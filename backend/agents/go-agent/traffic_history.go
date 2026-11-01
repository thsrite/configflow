package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// 流量记录：订阅共用的连接轮询，把每次采样的字节增量按小时、来源设备、策略组和出口节点累加，
// 目标另按小时与来源设备累加，定期上报给服务端保存。只在内存里累加，不写本机磁盘。
// 内核总量的增量减去各连接增量之和记为「未归属」（采样间隔内开始又结束的短连接）。

const (
	trafficHistorySchema         = 1
	trafficHistoryReportInterval = 5 * time.Minute
	trafficHistoryFirstReport    = time.Minute
	// 上报失败时内存里最多保留的键数（两类合计），超出的新键计入 dropped
	trafficHistoryMaxKeys = 20000
	// 每小时每台设备最多记录的目标数，其余并入「其他」（空目标）
	trafficHistoryMaxDomains   = 200
	trafficHistoryServerTooOld = 30 * time.Minute
	// 来源为空的连接是 Mihomo 自己发起的（DNS 上游、测速），与面板一致记为 Inner
	trafficHistoryInnerSource = "Inner"
)

type trafficUsageKey struct {
	Hour     int64
	Source   string
	Group    string
	Outbound string
}

type trafficDomainKey struct {
	Hour   int64
	Source string
	Domain string
}

type trafficCounters struct {
	Up, Down, Conns int64
}

func (c *trafficCounters) add(other trafficCounters) {
	c.Up += other.Up
	c.Down += other.Down
	c.Conns += other.Conns
}

type trafficUsageItem struct {
	Hour     int64  `json:"hour"`
	Source   string `json:"source"`
	Group    string `json:"group"`
	Outbound string `json:"outbound"`
	Up       int64  `json:"up"`
	Down     int64  `json:"down"`
	Conns    int64  `json:"conns"`
}

type trafficDomainItem struct {
	Hour   int64  `json:"hour"`
	Source string `json:"source"`
	Domain string `json:"domain"`
	Up     int64  `json:"up"`
	Down   int64  `json:"down"`
	Conns  int64  `json:"conns"`
}

type trafficHistoryReport struct {
	Schema  int                 `json:"schema"`
	Usage   []trafficUsageItem  `json:"usage"`
	Domains []trafficDomainItem `json:"domains"`
	Dropped int64               `json:"dropped"`
}

type trafficHistoryCollector struct {
	cfg          *Config
	postClient   *http.Client
	disabledByUs func()

	mu          sync.Mutex
	usage       map[trafficUsageKey]*trafficCounters
	domains     map[trafficDomainKey]*trafficCounters
	hourDomains map[[2]string]int // 当前小时每台设备已记录的目标数，键为 {小时, 来源}
	dropped     int64
	pausedUntil time.Time

	// 只在采集协程里使用
	baseline   bool
	prev       map[string][2]int64
	prevTotals [2]int64
}

func newTrafficHistoryCollector(cfg *Config, onDisabled func()) *trafficHistoryCollector {
	return &trafficHistoryCollector{
		cfg:          cfg,
		postClient:   &http.Client{Timeout: 30 * time.Second},
		disabledByUs: onDisabled,
		usage:        map[trafficUsageKey]*trafficCounters{},
		domains:      map[trafficDomainKey]*trafficCounters{},
		hourDomains:  map[[2]string]int{},
	}
}

func (t *trafficHistoryCollector) run(ctx context.Context) {
	snapshots, unsubscribe := mihomoConns.subscribe(t.cfg)
	defer unsubscribe()
	timer := time.NewTimer(trafficHistoryFirstReport)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case snapshot := <-snapshots:
			t.observe(snapshot)
		case <-timer.C:
			t.reportOnce(ctx)
			timer.Reset(trafficHistoryReportInterval)
		}
	}
}

func connSource(conn mihomoConn) string {
	if conn.Metadata.SourceIP == "" {
		return trafficHistoryInnerSource
	}
	return conn.Metadata.SourceIP
}

// connTarget 优先用域名，没有域名时用目标 IP；服务端再归并到主域名
func connTarget(conn mihomoConn) string {
	if domain := conn.domain(); domain != "" {
		return domain
	}
	return conn.Metadata.DestinationIP
}

func positive(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

// observe 处理一次快照。首次采样、读取失败后或内核重启（总量变小）后只建立基线，
// 不把建立基线前的累计量当作本小时的流量。
func (t *trafficHistoryCollector) observe(snapshot mihomoConnSnapshot) {
	if snapshot.Err != nil {
		t.baseline = false
		return
	}
	current := make(map[string][2]int64, len(snapshot.Conns))
	for _, conn := range snapshot.Conns {
		current[conn.ID] = [2]int64{conn.Upload, conn.Download}
	}
	totals := [2]int64{snapshot.UploadTotal, snapshot.DownloadTotal}
	if !t.baseline || totals[0] < t.prevTotals[0] || totals[1] < t.prevTotals[1] {
		t.baseline, t.prev, t.prevTotals = true, current, totals
		return
	}
	hour := snapshot.At.Truncate(time.Hour).Unix()
	var attributed [2]int64
	t.mu.Lock()
	for _, conn := range snapshot.Conns {
		before, seen := t.prev[conn.ID]
		delta := trafficCounters{Up: positive(conn.Upload - before[0]), Down: positive(conn.Download - before[1])}
		if !seen {
			// 两次采样之间新建的连接，字节都发生在这段时间内
			delta.Conns = 1
		}
		if delta.Up == 0 && delta.Down == 0 && delta.Conns == 0 {
			continue
		}
		attributed[0] += delta.Up
		attributed[1] += delta.Down
		source := connSource(conn)
		t.addUsage(trafficUsageKey{Hour: hour, Source: source, Group: conn.policy(), Outbound: conn.outletName()}, delta)
		t.addDomain(hour, source, connTarget(conn), delta)
	}
	if rest := (trafficCounters{Up: positive(totals[0] - t.prevTotals[0] - attributed[0]), Down: positive(totals[1] - t.prevTotals[1] - attributed[1])}); rest.Up > 0 || rest.Down > 0 {
		t.addUsage(trafficUsageKey{Hour: hour}, rest)
	}
	t.mu.Unlock()
	t.prev, t.prevTotals = current, totals
}

// 调用方持有 t.mu
func (t *trafficHistoryCollector) addUsage(key trafficUsageKey, delta trafficCounters) {
	if counters, ok := t.usage[key]; ok {
		counters.add(delta)
		return
	}
	if len(t.usage)+len(t.domains) >= trafficHistoryMaxKeys {
		t.dropped++
		return
	}
	counters := delta
	t.usage[key] = &counters
}

// 调用方持有 t.mu
func (t *trafficHistoryCollector) addDomain(hour int64, source, domain string, delta trafficCounters) {
	key := trafficDomainKey{Hour: hour, Source: source, Domain: domain}
	if counters, ok := t.domains[key]; ok {
		counters.add(delta)
		return
	}
	slot := [2]string{fmt.Sprint(hour), source}
	if domain != "" && t.hourDomains[slot] >= trafficHistoryMaxDomains {
		key.Domain = ""
		if counters, ok := t.domains[key]; ok {
			counters.add(delta)
			return
		}
	}
	if len(t.usage)+len(t.domains) >= trafficHistoryMaxKeys {
		t.dropped++
		return
	}
	if key.Domain != "" {
		t.hourDomains[slot]++
	}
	counters := delta
	t.domains[key] = &counters
}

func (t *trafficHistoryCollector) drain() trafficHistoryReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	report := trafficHistoryReport{Schema: trafficHistorySchema, Usage: []trafficUsageItem{}, Domains: []trafficDomainItem{}, Dropped: t.dropped}
	for key, c := range t.usage {
		report.Usage = append(report.Usage, trafficUsageItem{Hour: key.Hour, Source: key.Source, Group: key.Group, Outbound: key.Outbound, Up: c.Up, Down: c.Down, Conns: c.Conns})
	}
	for key, c := range t.domains {
		report.Domains = append(report.Domains, trafficDomainItem{Hour: key.Hour, Source: key.Source, Domain: key.Domain, Up: c.Up, Down: c.Down, Conns: c.Conns})
	}
	t.usage = map[trafficUsageKey]*trafficCounters{}
	t.domains = map[trafficDomainKey]*trafficCounters{}
	t.hourDomains = map[[2]string]int{}
	t.dropped = 0
	return report
}

// restore 把没送达的数据放回去，与上报期间新采到的数据合并
func (t *trafficHistoryCollector) restore(report trafficHistoryReport) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, item := range report.Usage {
		t.addUsage(trafficUsageKey{Hour: item.Hour, Source: item.Source, Group: item.Group, Outbound: item.Outbound}, trafficCounters{Up: item.Up, Down: item.Down, Conns: item.Conns})
	}
	for _, item := range report.Domains {
		t.addDomain(item.Hour, item.Source, item.Domain, trafficCounters{Up: item.Up, Down: item.Down, Conns: item.Conns})
	}
	t.dropped += report.Dropped
}

func (t *trafficHistoryCollector) reportOnce(ctx context.Context) {
	t.mu.Lock()
	paused := time.Now().Before(t.pausedUntil)
	t.mu.Unlock()
	if paused {
		return
	}
	report := t.drain()
	if len(report.Usage) == 0 && len(report.Domains) == 0 && report.Dropped == 0 {
		return
	}
	code, err := t.postReport(ctx, report)
	switch {
	case err == nil && code == http.StatusOK:
		return
	case code == http.StatusConflict:
		// 服务端已关闭流量记录，停止采集并丢弃数据
		if t.disabledByUs != nil {
			t.disabledByUs()
		}
		return
	case code == http.StatusNotFound:
		log.Printf("Traffic history: server does not accept reports yet, pausing")
		t.mu.Lock()
		t.pausedUntil = time.Now().Add(trafficHistoryServerTooOld)
		t.mu.Unlock()
	case code == http.StatusBadRequest || code == http.StatusRequestEntityTooLarge:
		// 被拒收的数据重试也不会成功，直接丢弃
		log.Printf("Traffic history: report rejected with status %d", code)
		return
	default:
		if err != nil {
			log.Printf("Traffic history: report failed: %v", err)
		}
	}
	t.restore(report)
}

func (t *trafficHistoryCollector) postReport(ctx context.Context, report trafficHistoryReport) (int, error) {
	body, err := json.Marshal(report)
	if err != nil {
		return 0, err
	}
	reportURL := fmt.Sprintf("%s/api/agents/%s/traffic-history", t.cfg.ServerURL, t.cfg.AgentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reportURL, bytes.NewReader(body))
	if err != nil {
		return 0, safeURLFailure("failed to create traffic history report for", reportURL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.cfg.Token)
	resp, err := t.postClient.Do(req)
	if err != nil {
		return 0, safeURLFailure("failed to send traffic history report to", reportURL, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	return resp.StatusCode, nil
}

// ---------- 开关 ----------

type trafficHistoryController struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

var trafficHistory = &trafficHistoryController{}

// SetEnabled 由心跳响应驱动；只有 mihomo Agent 会真正启动采集。
func (c *trafficHistoryController) SetEnabled(cfg *Config, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	enabled = enabled && cfg.ServiceType == "mihomo"
	if enabled == (c.cancel != nil) {
		return
	}
	if !enabled {
		c.cancel()
		c.cancel = nil
		log.Printf("Traffic history stopped")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	collector := newTrafficHistoryCollector(cfg, func() { c.SetEnabled(cfg, false) })
	go collector.run(ctx)
	log.Printf("Traffic history started")
}
