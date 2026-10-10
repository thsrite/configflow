package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// 域名发现：读取本机 mihomo 控制接口的连接列表与拨号失败日志，
// 按域名聚合后定期上报给服务端。只上报域名级统计，不含 URL、客户端 IP 或进程。

const (
	discoveryReportSchema   = 1
	discoveryReportInterval = 5 * time.Minute
	discoveryFirstReport    = 15 * time.Second
	discoveryMaxItems       = 2000
	discoveryMaxKeys        = 2 * discoveryMaxItems
	discoveryMaxTracked     = 10000
	discoveryRetryDedupe    = 30 * time.Second
	discoveryZeroDLAfter    = 3 * time.Second
	discoveryServerTooOld   = 30 * time.Minute
)

var errControllerAuth = errors.New("mihomo controller rejected the secret")

// 连接轮询间隔；存活不到一个间隔的连接可能漏采，失败的连接由日志兜底
var discoveryPollInterval = 2 * time.Second

// ---------- 控制接口定位 ----------

type mihomoController struct {
	BaseURL string
	Secret  string
}

// resolveMihomoController 从 mihomo 配置读取 external-controller 与 secret。
// 监听在全部地址时改为访问回环地址。
func resolveMihomoController(configPath string) (*mihomoController, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Controller string `yaml:"external-controller"`
		Secret     string `yaml:"secret"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	address := strings.TrimSpace(parsed.Controller)
	if address == "" {
		return nil, errors.New("external-controller is not configured")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid external-controller %q: %w", address, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return &mihomoController{BaseURL: "http://" + net.JoinHostPort(host, port), Secret: parsed.Secret}, nil
}

func (m *mihomoController) request(ctx context.Context, method, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, m.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if m.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+m.Secret)
	}
	return req, nil
}

// ---------- 聚合 ----------

type trafficItem struct {
	Host        string           `json:"host"`
	Port        int              `json:"port"`
	Network     string           `json:"network"`
	Rule        string           `json:"rule"`
	RulePayload string           `json:"rule_payload"`
	Policy      string           `json:"policy"`
	Outlet      string           `json:"outlet"`
	Conns       int64            `json:"conns"`
	Fails       int64            `json:"fails"`
	FailKinds   map[string]int64 `json:"fail_kinds"`
	ZeroDL      int64            `json:"zero_dl"`
	Up          int64            `json:"up"`
	Down        int64            `json:"down"`
}

type trafficKey struct {
	host, rule, payload, outlet, policy string
}

type discoveryAggregator struct {
	mu      sync.Mutex
	items   map[trafficKey]*trafficItem
	ipOnly  int64
	dropped int64
}

func newDiscoveryAggregator() *discoveryAggregator {
	return &discoveryAggregator{items: map[trafficKey]*trafficItem{}}
}

// truncateUTF8 按字节上限截断且不切断多字节字符，与服务端的长度校验对齐
func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

func (a *discoveryAggregator) entry(host string, port int, network, rule, payload, policy, outlet string) *trafficItem {
	// 服务端按字符数限制 rule 64 / rule_payload 256 / policy 128，按字节截断一定不会超
	rule, payload, policy = truncateUTF8(rule, 64), truncateUTF8(payload, 256), truncateUTF8(policy, 128)
	key := trafficKey{host, rule, payload, outlet, policy}
	item := a.items[key]
	if item == nil {
		// 上报持续失败时窗口会不断并回，键数量必须有上限
		if len(a.items) >= discoveryMaxKeys {
			a.dropped++
			return nil
		}
		item = &trafficItem{Host: host, Network: network, Rule: rule, RulePayload: payload, Policy: policy,
			Outlet: outlet, FailKinds: map[string]int64{}}
		a.items[key] = item
	}
	item.Port = port
	return item
}

func (a *discoveryAggregator) addConn(conn mihomoConn, now time.Time) {
	// 区域检测自己发出的请求不是用户流量，不能计入统计
	if conn.Metadata.InboundName == regionProbeListener {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	host := conn.domain()
	if host == "" {
		a.ipOnly++
		return
	}
	port, _ := strconv.Atoi(conn.Metadata.DestinationPort)
	item := a.entry(host, port, conn.Metadata.Network, conn.Rule, conn.RulePayload, conn.policy(), outletOf(conn.outletName()))
	if item == nil {
		return
	}
	item.Conns++
	item.Up += conn.Upload
	item.Down += conn.Download
	if conn.Download == 0 && !conn.Start.IsZero() && now.Sub(conn.Start) >= discoveryZeroDLAfter {
		item.ZeroDL++
	}
}

func (a *discoveryAggregator) addFail(fail dialFailure) {
	a.mu.Lock()
	defer a.mu.Unlock()
	item := a.entry(fail.Host, fail.Port, fail.Network, fail.Rule, fail.RulePayload, fail.Policy, outletOf(fail.Policy))
	if item == nil {
		return
	}
	item.Fails++
	item.FailKinds[fail.Kind]++
}

// drain 取出当前窗口；超过上限时保留连接与失败最多的条目，其余计入 dropped。
func (a *discoveryAggregator) drain(limit int) ([]trafficItem, int64, int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items := make([]trafficItem, 0, len(a.items))
	for _, item := range a.items {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Conns+items[i].Fails > items[j].Conns+items[j].Fails
	})
	dropped := a.dropped
	if len(items) > limit {
		dropped += int64(len(items) - limit)
		items = items[:limit]
	}
	ipOnly := a.ipOnly
	a.items = map[trafficKey]*trafficItem{}
	a.ipOnly, a.dropped = 0, 0
	return items, ipOnly, dropped
}

// restore 把上报失败的窗口并回去，下个窗口一起重试。
func (a *discoveryAggregator) restore(items []trafficItem, ipOnly, dropped int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ipOnly += ipOnly
	a.dropped += dropped
	for _, old := range items {
		item := a.entry(old.Host, old.Port, old.Network, old.Rule, old.RulePayload, old.Policy, old.Outlet)
		if item == nil {
			continue
		}
		item.Conns += old.Conns
		item.Fails += old.Fails
		item.ZeroDL += old.ZeroDL
		item.Up += old.Up
		item.Down += old.Down
		for kind, value := range old.FailKinds {
			item.FailKinds[kind] += value
		}
	}
}

// ---------- 连接列表 ----------

type mihomoConn struct {
	ID       string `json:"id"`
	Metadata struct {
		Network         string `json:"network"`
		InboundName     string `json:"inboundName"`
		Host            string `json:"host"`
		SniffHost       string `json:"sniffHost"`
		DestinationPort string `json:"destinationPort"`
	} `json:"metadata"`
	Upload      int64     `json:"upload"`
	Download    int64     `json:"download"`
	Start       time.Time `json:"start"`
	Chains      []string  `json:"chains"`
	Rule        string    `json:"rule"`
	RulePayload string    `json:"rulePayload"`
}

func (c mihomoConn) domain() string {
	host := c.Metadata.Host
	if host == "" {
		host = c.Metadata.SniffHost
	}
	return normalizeDiscoveryHost(host)
}

// chains 从实际出口排到规则指向的策略：[节点, …, 策略组]
func (c mihomoConn) outletName() string {
	if len(c.Chains) == 0 {
		return ""
	}
	return c.Chains[0]
}

func (c mihomoConn) policy() string {
	if len(c.Chains) == 0 {
		return "DIRECT"
	}
	return c.Chains[len(c.Chains)-1]
}

func outletOf(name string) string {
	switch {
	case name == "DIRECT" || name == "":
		return "direct"
	case strings.HasPrefix(name, "REJECT"):
		return "reject"
	default:
		return "proxy"
	}
}

var localSuffixes = []string{".lan", ".local", ".arpa", ".localhost", ".home.arpa", ".internal"}

// normalizeDiscoveryHost 返回可上报的域名；IP、内网名称和非法值返回空串。
func normalizeDiscoveryHost(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || len(host) > 253 || !strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return ""
	}
	for _, suffix := range localSuffixes {
		if strings.HasSuffix(host, suffix) {
			return ""
		}
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return ""
			}
		}
	}
	return host
}

// connTracker 记住上一轮快照里的连接；从快照中消失即视为结束。
type connTracker struct {
	tracked map[string]mihomoConn
	limit   int
}

func newConnTracker(limit int) *connTracker {
	return &connTracker{tracked: map[string]mihomoConn{}, limit: limit}
}

func (t *connTracker) observe(conns []mihomoConn) (finished []mihomoConn, overflow int) {
	seen := make(map[string]bool, len(conns))
	for _, conn := range conns {
		seen[conn.ID] = true
		if _, ok := t.tracked[conn.ID]; !ok && len(t.tracked) >= t.limit {
			overflow++
			continue
		}
		t.tracked[conn.ID] = conn
	}
	for id, conn := range t.tracked {
		if !seen[id] {
			finished = append(finished, conn)
			delete(t.tracked, id)
		}
	}
	return finished, overflow
}

// ---------- 拨号失败日志 ----------

type dialFailure struct {
	Network, Policy, Rule, RulePayload, Source, Host, Kind string
	Port                                                   int
}

// 例：[TCP] dial DIRECT (match Match/) 127.0.0.1:47434 --> example.com:443 error: dial tcp ...: i/o timeout
var dialFailurePattern = regexp.MustCompile(`^\[(TCP|UDP)\] dial (.+?) \(match ([^/()]+)/(.*?)\) (.+?) --> (\S+) error: (.*)$`)

func parseDialFailure(line string) (dialFailure, bool) {
	match := dialFailurePattern.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil {
		return dialFailure{}, false
	}
	hostPart, portPart, err := net.SplitHostPort(match[6])
	if err != nil {
		return dialFailure{}, false
	}
	host := normalizeDiscoveryHost(hostPart)
	if host == "" {
		return dialFailure{}, false
	}
	port, _ := strconv.Atoi(portPart)
	return dialFailure{
		Network: strings.ToLower(match[1]), Policy: match[2], Rule: match[3], RulePayload: match[4],
		Source: match[5], Host: host, Port: port, Kind: classifyDialError(match[7]),
	}, true
}

func classifyDialError(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "timeout"
	case strings.Contains(lower, "connection reset"):
		return "reset"
	case strings.Contains(lower, "connection refused"):
		return "refused"
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "dns") || strings.Contains(lower, "resolve"):
		return "dns"
	case strings.Contains(lower, "eof"):
		return "eof"
	default:
		return "other"
	}
}

// failureDeduper 合并 mihomo 对同一连接的重复拨号重试。
type failureDeduper struct {
	seen map[string]time.Time
}

func (d *failureDeduper) first(fail dialFailure, now time.Time) bool {
	if d.seen == nil {
		d.seen = map[string]time.Time{}
	}
	key := fail.Source + "|" + fail.Host + "|" + strconv.Itoa(fail.Port)
	if last, ok := d.seen[key]; ok && now.Sub(last) < discoveryRetryDedupe {
		return false
	}
	d.seen[key] = now
	if len(d.seen) > 4096 {
		for k, v := range d.seen {
			if now.Sub(v) >= discoveryRetryDedupe {
				delete(d.seen, k)
			}
		}
	}
	return true
}

// ---------- 采集与上报 ----------

type discoveryCollector struct {
	cfg        *Config
	aggregator *discoveryAggregator
	pollClient *http.Client
	postClient *http.Client

	mu           sync.Mutex
	controller   *mihomoController
	status       string
	version      string
	revisions    map[string]string
	pausedUntil  time.Time
	disabledByUs func()
}

func newDiscoveryCollector(cfg *Config, onDisabled func()) *discoveryCollector {
	return &discoveryCollector{
		cfg:          cfg,
		aggregator:   newDiscoveryAggregator(),
		pollClient:   &http.Client{Timeout: 5 * time.Second},
		postClient:   &http.Client{Timeout: 15 * time.Second},
		status:       "unreachable",
		revisions:    map[string]string{},
		disabledByUs: onDisabled,
	}
}

func (d *discoveryCollector) currentController() (*mihomoController, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.controller != nil {
		return d.controller, nil
	}
	controller, err := resolveMihomoController(d.cfg.ConfigPath)
	if err != nil {
		d.status = "no_controller"
		return nil, err
	}
	d.controller = controller
	return controller, nil
}

func (d *discoveryCollector) setStatus(status string, resetController bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status = status
	if resetController {
		// 配置可能被重新部署，下次重新读取地址与 secret
		d.controller = nil
	}
}

func (d *discoveryCollector) run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); d.pollLoop(ctx) }()
	go func() { defer wg.Done(); d.logLoop(ctx) }()
	go func() { defer wg.Done(); d.reportLoop(ctx) }()
	wg.Wait()
}

func (d *discoveryCollector) fetchConnections(ctx context.Context, controller *mihomoController) ([]mihomoConn, error) {
	req, err := controller.request(ctx, http.MethodGet, "/connections")
	if err != nil {
		return nil, err
	}
	resp, err := d.pollClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errControllerAuth
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var body struct {
		Connections []mihomoConn `json:"connections"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&body); err != nil {
		return nil, err
	}
	return body.Connections, nil
}

func (d *discoveryCollector) fetchVersion(ctx context.Context, controller *mihomoController) {
	req, err := controller.request(ctx, http.MethodGet, "/version")
	if err != nil {
		return
	}
	resp, err := d.pollClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var body struct {
		Version string `json:"version"`
	}
	if resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body) == nil {
		d.mu.Lock()
		d.version = body.Version
		d.mu.Unlock()
	}
}

func (d *discoveryCollector) pollLoop(ctx context.Context) {
	tracker := newConnTracker(discoveryMaxTracked)
	ticker := time.NewTicker(discoveryPollInterval)
	defer ticker.Stop()
	connected := false
	for {
		if controller, err := d.currentController(); err == nil {
			conns, err := d.fetchConnections(ctx, controller)
			switch {
			case err == nil:
				if !connected {
					d.fetchVersion(ctx, controller)
					connected = true
				}
				d.setStatus("running", false)
				finished, overflow := tracker.observe(conns)
				now := time.Now()
				for _, conn := range finished {
					d.aggregator.addConn(conn, now)
				}
				if overflow > 0 {
					d.aggregator.mu.Lock()
					d.aggregator.dropped += int64(overflow)
					d.aggregator.mu.Unlock()
				}
			case errors.Is(err, errControllerAuth):
				connected = false
				d.setStatus("auth_failed", true)
			case ctx.Err() != nil:
				return
			default:
				connected = false
				d.setStatus("unreachable", true)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *discoveryCollector) logLoop(ctx context.Context) {
	backoff := time.Second
	deduper := &failureDeduper{}
	stream := &http.Client{} // 长连接流，靠 ctx 结束
	for ctx.Err() == nil {
		controller, err := d.currentController()
		if err == nil {
			err = d.streamLogs(ctx, stream, controller, deduper)
			if err == nil {
				backoff = time.Second
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}

func (d *discoveryCollector) streamLogs(ctx context.Context, client *http.Client, controller *mihomoController, deduper *failureDeduper) error {
	req, err := controller.request(ctx, http.MethodGet, "/logs?level=warning")
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			Payload string `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if fail, ok := parseDialFailure(event.Payload); ok && deduper.first(fail, time.Now()) {
			d.aggregator.addFail(fail)
		}
	}
	return scanner.Err()
}

type discoveryReport struct {
	Schema        int           `json:"schema"`
	Status        string        `json:"status"`
	MihomoVersion string        `json:"mihomo_version,omitempty"`
	WindowStart   string        `json:"window_start"`
	WindowEnd     string        `json:"window_end"`
	IPOnlyConns   int64         `json:"ip_only_conns"`
	Dropped       int64         `json:"dropped"`
	Items         []trafficItem `json:"items"`
}

func (d *discoveryCollector) reportLoop(ctx context.Context) {
	windowStart := time.Now()
	timer := time.NewTimer(discoveryFirstReport)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		windowStart = d.reportOnce(ctx, windowStart)
		timer.Reset(discoveryReportInterval)
	}
}

func (d *discoveryCollector) reportOnce(ctx context.Context, windowStart time.Time) time.Time {
	d.mu.Lock()
	paused := time.Now().Before(d.pausedUntil)
	status, version := d.status, d.version
	d.mu.Unlock()
	if paused {
		return windowStart
	}
	now := time.Now()
	items, ipOnly, dropped := d.aggregator.drain(discoveryMaxItems)
	if status != "running" {
		// 采集不可用时只报告状态，但保留已采到的数据
		d.aggregator.restore(items, ipOnly, dropped)
		items, ipOnly, dropped = []trafficItem{}, 0, 0
	}
	report := discoveryReport{
		Schema: discoveryReportSchema, Status: status, MihomoVersion: version,
		WindowStart: windowStart.UTC().Format(time.RFC3339), WindowEnd: now.UTC().Format(time.RFC3339),
		IPOnlyConns: ipOnly, Dropped: dropped, Items: items,
	}
	revisions, code, err := d.postReport(ctx, report)
	switch {
	case err == nil && code == http.StatusOK:
		d.refreshProviders(ctx, revisions)
		return now
	case code == http.StatusConflict:
		// 服务端已关闭域名发现，停止采集并丢弃数据
		if d.disabledByUs != nil {
			d.disabledByUs()
		}
		return now
	case code == http.StatusNotFound:
		log.Printf("Domain discovery: server does not support traffic reports yet, pausing")
		d.mu.Lock()
		d.pausedUntil = time.Now().Add(discoveryServerTooOld)
		d.mu.Unlock()
	case code == http.StatusBadRequest || code == http.StatusRequestEntityTooLarge:
		// 被拒收的数据重试也不会成功，直接丢弃
		log.Printf("Domain discovery: report rejected with status %d", code)
		return now
	default:
		if err != nil {
			log.Printf("Domain discovery: report failed: %v", err)
		}
	}
	d.aggregator.restore(items, ipOnly, dropped)
	return windowStart
}

func (d *discoveryCollector) postReport(ctx context.Context, report discoveryReport) (map[string]string, int, error) {
	body, err := json.Marshal(report)
	if err != nil {
		return nil, 0, err
	}
	reportURL := fmt.Sprintf("%s/api/agents/%s/traffic-report", d.cfg.ServerURL, d.cfg.AgentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reportURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, safeURLFailure("failed to create traffic report for", reportURL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.cfg.Token)
	resp, err := d.postClient.Do(req)
	if err != nil {
		return nil, 0, safeURLFailure("failed to send traffic report to", reportURL, err)
	}
	defer resp.Body.Close()
	var result struct {
		RuleProviders map[string]string `json:"rule_providers"`
	}
	if resp.StatusCode == http.StatusOK {
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result)
	}
	return result.RuleProviders, resp.StatusCode, nil
}

// refreshProviders 在默认规则集内容变化后让 mihomo 立即重新拉取，免去等待 interval。
func (d *discoveryCollector) refreshProviders(ctx context.Context, revisions map[string]string) {
	controller, err := d.currentController()
	if err != nil {
		return
	}
	for name, revision := range revisions {
		d.mu.Lock()
		unchanged := d.revisions[name] == revision
		d.mu.Unlock()
		if unchanged {
			continue
		}
		req, err := controller.request(ctx, http.MethodPut, "/providers/rules/"+url.PathEscape(name))
		if err != nil {
			continue
		}
		resp, err := d.pollClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		// 404 表示当前部署的配置里还没有这个规则集，等下次部署后再刷新
		if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
			d.mu.Lock()
			d.revisions[name] = revision
			d.mu.Unlock()
			log.Printf("Domain discovery: refreshed rule provider %q", name)
		}
	}
}

// ---------- 开关 ----------

type discoveryController struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

var domainDiscovery = &discoveryController{}

// SetEnabled 由心跳响应驱动；只有 mihomo Agent 会真正启动采集。
func (c *discoveryController) SetEnabled(cfg *Config, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	enabled = enabled && cfg.ServiceType == "mihomo"
	if enabled == (c.cancel != nil) {
		return
	}
	if !enabled {
		c.cancel()
		c.cancel = nil
		log.Printf("Domain discovery stopped")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	collector := newDiscoveryCollector(cfg, func() { c.SetEnabled(cfg, false) })
	go collector.run(ctx)
	log.Printf("Domain discovery started")
}
