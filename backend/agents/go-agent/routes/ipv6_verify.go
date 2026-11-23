package routes

// Verification deliberately separates a running core, DNS answers, proxy HTTP
// success and an observed IPv6 socket. None of these alone proves a LAN path.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ipv6VerifyDefaultURL = "https://www.gstatic.com/generate_204"

type ipv6VerifyCheck struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
	Scope   string   `json:"scope"`
}

type ipv6VerifyReport struct {
	Success    bool              `json:"success"`
	Scope      string            `json:"scope"`
	CheckedAt  string            `json:"checked_at"`
	Checks     []ipv6VerifyCheck `json:"checks"`
	NodeName   string            `json:"node_name,omitempty"`
	TargetHost string            `json:"target_host,omitempty"`
}

func (r *ipv6VerifyReport) add(id, label, status, message, scope string, details ...string) {
	r.Checks = append(r.Checks, ipv6VerifyCheck{ID: id, Label: label, Status: status, Message: message, Scope: scope, Details: details})
}

func ipv6VerificationURL(raw string) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\x00#") {
		return nil, fmt.Errorf("验收地址无效")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || u.Hostname() == "" || u.Opaque != "" {
		return nil, fmt.Errorf("验收地址必须是无账号、无片段的 HTTP(S) 地址")
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("验收地址端口无效")
		}
	}
	host := u.Hostname()
	if len(host) > 253 {
		return nil, fmt.Errorf("验收地址主机名过长")
	}
	if ip := net.ParseIP(host); ip == nil && !ipv6VerifyDomain.MatchString(host) {
		return nil, fmt.Errorf("验收地址主机名无效")
	}
	return u, nil
}

var ipv6VerifyDomain = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.?$`)
var ipv6VerifyRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
var ipv6VerifySHA = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

// The controller supplies only a saved verification target. In the absence of
// that selection, use an already deployed health-check URL or the fixed default.
func ipv6SavedVerificationURL(doc map[string]any) string {
	groups, _ := doc["proxy-groups"].([]any)
	for _, value := range groups {
		if group, ok := value.(map[string]any); ok {
			if raw, ok := group["url"].(string); ok {
				if _, err := ipv6VerificationURL(raw); err == nil {
					return raw
				}
			}
		}
	}
	providers, _ := doc["proxy-providers"].(map[string]any)
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if provider, ok := providers[name].(map[string]any); ok {
			if health, ok := provider["health-check"].(map[string]any); ok {
				if raw, ok := health["url"].(string); ok {
					if _, err := ipv6VerificationURL(raw); err == nil {
						return raw
					}
				}
			}
		}
	}
	return ipv6VerifyDefaultURL
}

func ipv6VerifyAPI(ctx context.Context, controller *mihomoReloader, path string, result any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, controller.endpoint+path, nil)
	if err != nil {
		return fmt.Errorf("控制接口请求无效")
	}
	if controller.secret != "" {
		request.Header.Set("Authorization", "Bearer "+controller.secret)
	}
	response, err := controller.client.Do(request)
	if err != nil {
		return fmt.Errorf("Mihomo 控制接口无法连接或超时")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Mihomo 控制接口返回 HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 || json.Unmarshal(data, result) != nil {
		return fmt.Errorf("Mihomo 控制接口响应无效")
	}
	return nil
}

func ipv6VerifyAPIStep(ctx context.Context, controller *mihomoReloader, path string, result any) error {
	step, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return ipv6VerifyAPI(step, controller, path, result)
}

type ipv6RuntimeProxy struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	ID          string   `json:"id"`
	DialerProxy string   `json:"dialer-proxy"`
	All         []string `json:"all"`
	Extra       map[string]struct {
		Alive   bool `json:"alive"`
		History []struct {
			Time  time.Time `json:"time"`
			Delay int       `json:"delay"`
		} `json:"history"`
	} `json:"extra"`
}

func ipv6ActualProxy(p ipv6RuntimeProxy) bool {
	if len(p.All) > 0 || p.Type == "" {
		return false
	}
	switch strings.ToLower(p.Type) {
	case "direct", "reject", "rejectdrop", "reject-drop", "pass", "compatible", "selector", "urltest", "url-test", "fallback", "loadbalance", "load-balance", "relay":
		return false
	}
	return true
}

func ipv6SelectVerifyNode(doc map[string]any, proxies map[string]ipv6RuntimeProxy, requested string) (string, map[string]any) {
	definitions := map[string]map[string]any{}
	items, _ := doc["proxies"].([]any)
	for _, item := range items {
		if node, ok := item.(map[string]any); ok {
			if name, ok := node["name"].(string); ok {
				definitions[name] = node
			}
		}
	}
	if requested != "" {
		if p, ok := proxies[requested]; ok && ipv6ActualProxy(p) {
			return requested, definitions[requested]
		}
		return "", nil
	}
	names := make([]string, 0, len(proxies))
	for name, p := range proxies {
		if name != "" && len(name) <= 256 && !strings.ContainsAny(name, "\r\n\x00") && ipv6ActualProxy(p) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		server, _ := definitions[name]["server"].(string)
		if ip := net.ParseIP(server); ip != nil && ip.To4() == nil {
			return name, definitions[name]
		}
	}
	for _, name := range names {
		server, _ := definitions[name]["server"].(string)
		if server != "" && net.ParseIP(server) == nil {
			return name, definitions[name]
		}
	}
	if len(names) > 0 {
		return names[0], definitions[names[0]]
	}
	return "", nil
}

func ipv6VerifyHandler(cfg *Config, w http.ResponseWriter, r *http.Request) {
	var request struct {
		TestURL              string `json:"test_url"`
		NodeName             string `json:"node_name"`
		ExpectedConfigSHA256 string `json:"expected_config_sha256"`
	}
	if err := decodeIPv6Request(w, r, &request); err != nil {
		JsonResponse(w, 400, map[string]any{"success": false, "message": "验收请求无效"})
		return
	}
	if !ipv6VerifySupported(cfg) || cfg.ServiceType != "mihomo" || len(request.NodeName) > 256 || strings.ContainsAny(request.NodeName, "\r\n\x00") || (request.ExpectedConfigSHA256 != "" && !ipv6VerifySHA.MatchString(request.ExpectedConfigSHA256)) {
		JsonResponse(w, 400, map[string]any{"success": false, "message": "需要 Linux Mihomo Agent 和有效验收参数"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 28*time.Second)
	defer cancel()
	report := ipv6VerifyReport{Success: true, Scope: "mihomo_runtime", CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), Checks: []ipv6VerifyCheck{}}
	finish := func() { JsonResponse(w, 200, report) }
	release, err := AcquireServiceOperation(cfg)
	if err != nil {
		JsonResponse(w, 409, map[string]any{"success": false, "message": "服务正在更新或执行其他操作，请稍后验收"})
		return
	}
	defer release()
	doc, file, err := readYAMLDocument(cfg.ConfigPath)
	if err != nil {
		report.add("deployed_config", "已发布配置", "error", "无法读取 Agent 管理的配置文件", "deployed_file")
		finish()
		return
	}
	if request.ExpectedConfigSHA256 == "" {
		report.add("deployed_config", "已发布配置", "unknown", "未提供本次发布内容的指纹，不能核对是否为预期版本", "deployed_file")
	} else if !strings.EqualFold(request.ExpectedConfigSHA256, sha256Hex(file)) {
		report.add("deployed_config", "已发布配置", "error", "设备上的配置与本次验收版本不同，请重新发布后验收", "deployed_file")
		finish()
		return
	} else {
		report.add("deployed_config", "已发布配置", "ok", "设备配置文件与本次发布内容一致；运行状态另外检查", "deployed_file")
	}
	if request.TestURL == "" {
		request.TestURL = ipv6SavedVerificationURL(doc)
	}
	target, err := ipv6VerificationURL(request.TestURL)
	if err != nil {
		JsonResponse(w, 400, map[string]any{"success": false, "message": err.Error()})
		return
	}
	report.TargetHost = target.Hostname()
	controller, err := newMihomoReloader(doc)
	if err != nil {
		report.add("runtime_core", "Mihomo 运行状态", "unknown", "无法确定可安全访问的本机控制接口", "mihomo_api")
		finish()
		return
	}
	defer controller.client.CloseIdleConnections()
	var version struct {
		Version string `json:"version"`
	}
	var current map[string]any
	if ipv6VerifyAPIStep(ctx, controller, "/version", &version) != nil || version.Version == "" || ipv6VerifyAPIStep(ctx, controller, "/configs", &current) != nil || current == nil {
		report.add("runtime_core", "Mihomo 运行状态", "error", "未能读取正在运行的 Mihomo 控制接口", "mihomo_api")
		finish()
		return
	}
	report.add("runtime_core", "Mihomo 运行状态", "ok", "已从当前 Mihomo 控制接口读取运行状态", "mihomo_api")
	expectedIPv6 := true
	if v, exists := doc["ipv6"]; exists {
		expectedIPv6 = v == true
	}
	actualIPv6, known := current["ipv6"].(bool)
	if !known {
		report.add("runtime_ipv6", "运行中的 IPv6 开关", "unknown", "当前核心未返回 IPv6 开关，不能只根据文件判断已生效", "mihomo_api")
	} else if actualIPv6 != expectedIPv6 {
		report.add("runtime_ipv6", "运行中的 IPv6 开关", "error", "运行中的 IPv6 开关与已发布文件不同", "mihomo_api")
	} else {
		report.add("runtime_ipv6", "运行中的 IPv6 开关", "ok", fmt.Sprintf("运行中的 IPv6 开关与文件一致：%t", actualIPv6), "mihomo_api")
	}
	configuredTUN, _ := doc["tun"].(map[string]any)
	runtimeTUN, _ := current["tun"].(map[string]any)
	tunEnabled, tunKnown := runtimeTUN["enable"].(bool)
	if !tunKnown {
		report.add("runtime_tun", "运行中的 TUN", "unknown", "当前核心未返回 TUN 状态", "mihomo_api")
	} else if tunEnabled != (configuredTUN["enable"] == true) {
		report.add("runtime_tun", "运行中的 TUN", "error", "运行中的 TUN 开关与发布文件不同", "mihomo_api")
	} else {
		report.add("runtime_tun", "运行中的 TUN", "ok", "已核对当前核心的 TUN 开关", "mihomo_api", "开关一致不等于客户端已经经过 TUN。")
	}
	ipv6VerifyDNS(ctx, &report, doc, target.Hostname())
	var live struct {
		Proxies map[string]ipv6RuntimeProxy `json:"proxies"`
	}
	if ipv6VerifyAPIStep(ctx, controller, "/proxies", &live) != nil {
		report.add("proxy_http", "代理访问网站", "unknown", "无法读取当前核心的实际节点，未执行代理访问", "mihomo_proxy_http")
		finish()
		return
	}
	name, node := ipv6SelectVerifyNode(doc, live.Proxies, request.NodeName)
	report.NodeName = name
	if name == "" {
		report.add("proxy_http", "代理访问网站", "unknown", "当前核心没有可明确选择的实际代理节点", "mihomo_proxy_http")
		finish()
		return
	}
	before := live.Proxies[name]
	started := time.Now()
	stopObserve := ipv6ObserveNodeSockets(ctx, cfg, node, before.DialerProxy)
	query := url.Values{"url": {request.TestURL}, "timeout": {"8000"}, "expected": {"200-299"}}
	step, stepCancel := context.WithTimeout(ctx, 10*time.Second)
	var delay struct {
		Delay int `json:"delay"`
	}
	err = ipv6VerifyAPI(step, controller, "/proxies/"+url.PathEscape(name)+"/delay?"+query.Encode(), &delay)
	stepCancel()
	socketCheck := stopObserve()
	var after ipv6RuntimeProxy
	if err != nil || delay.Delay <= 0 {
		report.add("proxy_http", "代理访问网站", "error", "没有通过这个节点成功访问测试网站", "mihomo_proxy_http", "端口能够连接，不代表代理协议和网站访问都正常。")
	} else if ipv6VerifyAPIStep(ctx, controller, "/proxies/"+url.PathEscape(name), &after) != nil || (before.ID != "" && after.ID != before.ID) {
		report.add("proxy_http", "代理访问网站", "unknown", "收到测速结果，但无法确认同一运行节点的本次 HTTP 结果", "mihomo_proxy_http")
	} else if ipv6FreshHTTPResult(after, request.TestURL, started) {
		report.add("proxy_http", "代理访问网站", "ok", "实际节点已通过代理访问测试网站并取得成功 HTTP 响应", "mihomo_proxy_http", "只验证所选节点和这次目标，不代表其他节点或客户端分流。")
	} else {
		report.add("proxy_http", "代理访问网站", "unknown", "已收到延迟值，但未取得本次成功 HTTP 状态的证据", "mihomo_proxy_http", "延迟值本身不能证明网站返回成功状态，也不能证明使用 IPv6。")
	}
	report.Checks = append(report.Checks, socketCheck)
	report.add("client_path", "客户端完整路径", "unknown", "本机验收不包含客户端经过 ROS、DNS 和 Mihomo 的完整路径", "client_path")
	if currentFile, e := os.ReadFile(cfg.ConfigPath); e != nil || sha256Hex(currentFile) != sha256Hex(file) {
		report.add("config_stability", "验收期间配置", "warning", "验收过程中配置发生变化，请重新验收", "deployed_file")
	}
	finish()
}

func ipv6FreshHTTPResult(proxy ipv6RuntimeProxy, target string, started time.Time) bool {
	state, exists := proxy.Extra[target]
	if !exists || !state.Alive || len(state.History) == 0 {
		return false
	}
	newRecords := 0
	for _, item := range state.History {
		if !item.Time.Before(started) {
			newRecords++
		}
	}
	last := state.History[len(state.History)-1]
	return newRecords == 1 && !last.Time.Before(started) && last.Delay > 0
}
