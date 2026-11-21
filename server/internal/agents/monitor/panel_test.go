package monitor

// Port of backend/test_mihomo_panel.py: the Mihomo panel only forwards
// whitelisted controller calls through the Agent. The Python tests patch
// requests.Session.request; here a queue of fake Agent answers
// (replyClient). The two route cases are in
// internal/api/agents/panel_test.go; request_rule / _latest_core_version
// are replaced through requestRule / latestCoreVersion.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

func panelAgentRecord(changes ...any) *model.Agent {
	a := map[string]any{
		"id": "mihomo", "name": "Gateway", "host": "127.0.0.1", "port": 8080,
		"service_type": "mihomo", "version": "1.8.15-go", "status": "online", "token": "agent-private-token",
	}
	for i := 0; i+1 < len(changes); i += 2 {
		a[changes[i].(string)] = changes[i+1]
	}
	return mustAgentMap(a)
}

// replyClient answers queued responses (or errors), then {"ok": true}.
type replyClient struct {
	mu      sync.Mutex
	replies []any
	calls   []call
}

func (c *replyClient) Do(_ context.Context, r agents.Request) (*agents.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	u, _ := url.Parse(r.URL)
	header := map[string]string{}
	for k := range r.Header {
		header[k] = r.Header.Get(k)
	}
	c.calls = append(c.calls, call{url: u.Scheme + "://" + u.Host + u.EscapedPath(), query: u.Query(), rawQ: u.RawQuery, header: header, req: r})
	var reply any = jsonResponse(map[string]any{"ok": true}, 200)
	if len(c.replies) > 0 {
		reply, c.replies = c.replies[0], c.replies[1:]
	}
	if err, ok := reply.(error); ok {
		return nil, err
	}
	resp := *reply.(*agents.Response)
	if r.MaxBody > 0 && int64(len(resp.Body)) > r.MaxBody+1 {
		resp.Body = resp.Body[:r.MaxBody+1]
	}
	return &resp, nil
}

func (c *replyClient) add(replies ...any) {
	c.mu.Lock()
	c.replies = append(c.replies, replies...)
	c.mu.Unlock()
}

func (c *replyClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func panelManager(t *testing.T, agent *model.Agent) (*fakeManager, *replyClient) {
	if agent == nil {
		agent = panelAgentRecord()
	}
	client := &replyClient{}
	return &fakeManager{agents: map[string]*model.Agent{"mihomo": agent}, dataDir: t.TempDir(), client: client}, client
}

func panelCall(m Registry, resource, method string, a [][2]string, body any) (*PanelResult, error) {
	var raw json.RawMessage
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	return CallPanel(ctx, m, "mihomo", resource, method, a, raw, "")
}

func mustPanel(t *testing.T, m Registry, resource, method string, a [][2]string, body any) map[string]any {
	t.Helper()
	result, err := panelCall(m, resource, method, a, body)
	if err != nil {
		t.Fatal(err)
	}
	return toJSONMap(t, result)
}

func queryOf(c call) map[string]string {
	out := map[string]string{}
	for k, v := range c.query {
		out[k] = v[0]
	}
	return out
}

func TestForwardsNamedResourcesAsSingleEncodedSegment(t *testing.T) {
	m, client := panelManager(t, nil)
	result := mustPanel(t, m, "proxy", "PUT", args("name", "节点/香港 01"), map[string]any{"name": "HK"})
	wantJSON(t, result, map[string]any{"success": true, "data": map[string]any{"ok": true}})
	c := client.calls[0]
	if c.req.Method != "PUT" || c.url != "http://127.0.0.1:8080/api/mihomo/proxies/%E8%8A%82%E7%82%B9%2F%E9%A6%99%E6%B8%AF%2001" {
		t.Fatal(c.req.Method, c.url)
	}
	if string(c.req.Body) != `{"name":"HK"}` || c.header["Authorization"] != "Bearer agent-private-token" ||
		!c.req.NoRedirects || !c.req.NoProxy || c.header["Content-Type"] != "application/json" {
		t.Fatal(string(c.req.Body), c.header, c.req)
	}
}

func TestDelayQueryIsValidatedAndDefaulted(t *testing.T) {
	m, client := panelManager(t, nil)
	mustPanel(t, m, "group/delay", "GET", args("name", "Auto", "extra", "x"), nil)
	if client.calls[0].rawQ != "url=https%3A%2F%2Fwww.gstatic.com%2Fgenerate_204&timeout=5000" {
		t.Fatal(client.calls[0].rawQ)
	}
	_, err := panelCall(m, "group/delay", "GET", args("name", "Auto", "url", "file:///etc/passwd"), nil)
	wantMonitorError(t, err, "INVALID_REQUEST", 400)
}

func TestRejectsOperationsOutsideThePanel(t *testing.T) {
	cases := []struct {
		resource, method string
		body             any
	}{
		{"configs", "PUT", map[string]any{"mode": "rule"}},
		{"restart", "POST", nil},
		{"configs", "PATCH", map[string]any{"mode": "rule", "tun": map[string]any{"enable": false}}},
		{"configs", "PATCH", map[string]any{"external-controller": "0.0.0.0:9090"}},
		{"proxy", "PUT", map[string]any{"name": ""}},
	}
	for _, tc := range cases {
		m, client := panelManager(t, nil)
		if _, err := panelCall(m, tc.resource, tc.method, args("name", "G"), tc.body); err == nil {
			t.Fatal(tc)
		}
		if client.count() != 0 {
			t.Fatal(tc, client.calls)
		}
	}
}

func TestModeSwitchOnlySendsMode(t *testing.T) {
	m, client := panelManager(t, nil)
	mustPanel(t, m, "configs", "PATCH", nil, map[string]any{"mode": "global"})
	if string(client.calls[0].req.Body) != `{"mode":"global"}` {
		t.Fatal(string(client.calls[0].req.Body))
	}
}

func TestRequiresNewMihomoAgent(t *testing.T) {
	m, client := panelManager(t, panelAgentRecord("version", "1.8.14-go"))
	_, err := panelCall(m, "version", "GET", nil, nil)
	wantMonitorError(t, err, "UPGRADE_REQUIRED", 409)
	m.agents["mihomo"] = panelAgentRecord("service_type", "oxidns")
	_, err = panelCall(m, "version", "GET", nil, nil)
	wantMonitorError(t, err, "WRONG_SERVICE", 400)
	if client.count() != 0 {
		t.Fatal(client.calls)
	}
}

func TestEmptyReplyAndCoreErrors(t *testing.T) {
	m, client := panelManager(t, nil)
	client.add(&agents.Response{StatusCode: 204}, jsonResponse(map[string]any{"message": "Timeout"}, 504),
		jsonResponse(map[string]any{"success": false, "code": "CONTROLLER_UNAVAILABLE"}, 409))
	wantJSON(t, mustPanel(t, m, "connection", "DELETE", args("name", "abc"), nil), map[string]any{"success": true, "data": nil})
	_, err := panelCall(m, "proxy/delay", "GET", args("name", "HK"), nil)
	if e := wantMonitorError(t, err, "CORE_ERROR", 504); e.Message != "Timeout" {
		t.Fatal(e)
	}
	_, err = panelCall(m, "version", "GET", nil, nil)
	wantMonitorError(t, err, "CONTROLLER_UNAVAILABLE", 409)
}

func TestReloadAndUpgradeNeedNewerAgentAndSendNoBody(t *testing.T) {
	old, client := panelManager(t, nil)
	for _, rm := range [][2]string{{"configs/reload", "PUT"}, {"upgrade", "POST"}} {
		_, err := panelCall(old, rm[0], rm[1], nil, map[string]any{"path": "/tmp/evil.yaml"})
		if e := wantMonitorError(t, err, "UPGRADE_REQUIRED", 409); !strings.Contains(e.Message, "1.8.16-go") {
			t.Fatal(e)
		}
	}
	if client.count() != 0 {
		t.Fatal(client.calls)
	}
	m, client := panelManager(t, panelAgentRecord("version", "1.8.16-go"))
	mustPanel(t, m, "configs/reload", "PUT", nil, map[string]any{"path": "/tmp/evil.yaml", "payload": "x"})
	mustPanel(t, m, "upgrade", "POST", args("channel", "alpha"), nil)
	for i, want := range [][2]string{{"PUT", "configs"}, {"POST", "upgrade"}} {
		c := client.calls[i]
		if c.req.Method != want[0] || !strings.HasSuffix(c.url, "/api/mihomo/"+want[1]) || c.req.Body != nil || c.rawQ != "" {
			t.Fatal(i, c)
		}
	}
	if client.calls[1].req.Timeout != agents.Pair(3, 200) || client.calls[0].req.Timeout != agents.Pair(3, 70) {
		t.Fatal(client.calls[1].req.Timeout)
	}
}

func TestProviderNodeDelayEncodesBothNamesAndKeepsTestOptions(t *testing.T) {
	provider := "订阅 / A%?#"
	name := "🇯🇵 JP-05 · AWS2 / 50%?#"
	testURL := "http://127.0.0.1:18080/generate_204?value=a&other=b"
	m, client := panelManager(t, panelAgentRecord("version", "1.8.18-go"))
	client.add(jsonResponse(map[string]any{"delay": 123}, 200))
	result := mustPanel(t, m, "providers/proxy/delay", "GET", args("provider", provider, "name", name, "url", testURL,
		"timeout", "7300", "unrelated", "discard"), nil)
	wantJSON(t, result, map[string]any{"success": true, "data": map[string]any{"delay": 123}})
	c := client.calls[0]
	if !strings.HasSuffix(c.url, "/api/mihomo/providers/proxies/%E8%AE%A2%E9%98%85%20%2F%20A%25%3F%23/"+
		"%F0%9F%87%AF%F0%9F%87%B5%20JP-05%20%C2%B7%20AWS2%20%2F%2050%25%3F%23/healthcheck") {
		t.Fatal(c.url)
	}
	if q := queryOf(c); len(q) != 2 || q["url"] != testURL || q["timeout"] != "7300" {
		t.Fatal(q)
	}
	if c.req.Method != "GET" || c.req.Body != nil || c.req.Timeout != agents.Pair(3, 40) {
		t.Fatal(c.req)
	}
}

func TestOrdinaryNodeStillWorksOnOlderAgentAndProviderRequiresUpdate(t *testing.T) {
	m, client := panelManager(t, nil)
	client.add(jsonResponse(map[string]any{"delay": 81}, 200))
	result := mustPanel(t, m, "proxy/delay", "GET", args("name", "普通节点"), nil)
	if asInt64(asMap(result["data"])["delay"]) != 81 {
		t.Fatal(result)
	}
	c := client.calls[0]
	if !strings.Contains(c.url, "/proxies/") || strings.Contains(c.url, "/providers/") {
		t.Fatal(c.url)
	}
	if q := queryOf(c); q["url"] != PanelDefaultTestURL || q["timeout"] != "5000" {
		t.Fatal(q)
	}
	m.agents["mihomo"] = panelAgentRecord("version", "1.8.17-go")
	_, err := panelCall(m, "providers/proxy/delay", "GET", args("provider", "Airport", "name", "HK"), nil)
	e := wantMonitorError(t, err, "UPGRADE_REQUIRED", 409)
	if !strings.Contains(e.Message, "1.8.18-go") || !strings.Contains(e.Message, "订阅节点测速") || client.count() != 1 {
		t.Fatal(e, client.calls)
	}
}

func TestProviderNodeUsesExistingNameAndDelayValidation(t *testing.T) {
	cases := [][]string{
		{"name", "HK"}, {"provider", "Airport"}, {"provider", "", "name", "HK"},
		{"provider", "bad\nname", "name", "HK"}, {"provider", "Airport", "name", "bad\x00name"},
		{"provider", strings.Repeat("x", 513), "name", "HK"},
		{"provider", "Airport", "name", "HK", "url", "file:///tmp/probe"},
		{"provider", "Airport", "name", "HK", "timeout", "invalid"},
		{"provider", "Airport", "name", "HK", "timeout", "30001"},
		{"provider", "Airport", "name", "HK", "timeout", "499"},
	}
	for _, params := range cases {
		m, client := panelManager(t, panelAgentRecord("version", "1.8.18-go"))
		_, err := panelCall(m, "providers/proxy/delay", "GET", args(params...), nil)
		wantMonitorError(t, err, "INVALID_REQUEST", 400)
		if client.count() != 0 {
			t.Fatal(params, client.calls)
		}
	}
}

func TestProviderNodeCoreFailureIsReportedWithoutTestingAnotherNode(t *testing.T) {
	for _, tc := range []struct {
		status   int
		message  string
		expected int
	}{{404, "Resource not found", 400}, {504, "Timeout", 504}, {503, "An error occurred in the delay test", 502}} {
		m, client := panelManager(t, panelAgentRecord("version", "1.8.18-go"))
		client.add(jsonResponse(map[string]any{"message": tc.message}, tc.status))
		_, err := panelCall(m, "providers/proxy/delay", "GET", args("provider", "Airport", "name", "HK"), nil)
		if e := wantMonitorError(t, err, "CORE_ERROR", tc.expected); e.Message != tc.message || client.count() != 1 {
			t.Fatal(e, client.calls)
		}
	}
}

func TestProviderHealthcheckKeepsNoParams(t *testing.T) {
	// The direct-call half of test_provider_node_route_requires_admin_and_keeps_provider_healthcheck.
	m, client := panelManager(t, nil)
	mustPanel(t, m, "providers/proxy/healthcheck", "GET", args("name", "Airport"), nil)
	if c := client.calls[0]; !strings.HasSuffix(c.url, "/providers/proxies/Airport/healthcheck") || c.rawQ != "" {
		t.Fatal(c)
	}
}

func TestCoreUpgradeErrorsKeepOriginalText(t *testing.T) {
	for _, message := range []string{"download failed: dial tcp: i/o timeout", "Agent download connection failed"} {
		m, client := panelManager(t, panelAgentRecord("version", "1.8.16-go"))
		client.add(jsonResponse(map[string]any{"message": message}, 500))
		_, err := panelCall(m, "upgrade", "POST", nil, nil)
		if e := wantMonitorError(t, err, "CORE_ERROR", 502); e.Message != message {
			t.Fatal(e)
		}
	}
}

func TestLiveFlowNeedsNewerAgentAndForwardsWithoutParams(t *testing.T) {
	m, client := panelManager(t, panelAgentRecord("version", "1.8.25-go"))
	_, err := panelCall(m, "flow", "GET", nil, nil)
	e := wantMonitorError(t, err, "UPGRADE_REQUIRED", 409)
	if !strings.Contains(e.Message, "1.8.26-go") || !strings.Contains(e.Message, "实时流量") || client.count() != 0 {
		t.Fatal(e)
	}
	m.agents["mihomo"] = panelAgentRecord("version", "1.8.26-go")
	client.add(jsonResponse(map[string]any{"interval": 2.0, "paths": []any{}}, 200))
	result := mustPanel(t, m, "flow", "GET", args("url", "http://ignored"), nil)
	wantJSON(t, result["data"], map[string]any{"interval": 2.0, "paths": []any{}})
	if c := client.calls[0]; c.req.Method != "GET" || !strings.HasSuffix(c.url, "/api/mihomo/flow") || c.rawQ != "" || c.req.Body != nil {
		t.Fatal(c)
	}
}

func TestLiveLogsNeedNewerAgentAndForwardOnlyLevelAndCursor(t *testing.T) {
	m, client := panelManager(t, panelAgentRecord("version", "1.8.55-go"))
	_, err := panelCall(m, "logs", "GET", nil, nil)
	e := wantMonitorError(t, err, "UPGRADE_REQUIRED", 409)
	if !strings.Contains(e.Message, "1.8.56-go") || !strings.Contains(e.Message, "实时日志") || client.count() != 0 {
		t.Fatal(e)
	}
	m.agents["mihomo"] = panelAgentRecord("version", "1.8.56-go")
	client.add(jsonResponse(map[string]any{"stream": "debug", "entries": []any{}, "next": 7}, 200))
	result := mustPanel(t, m, "logs", "GET", args("level", "debug", "after", "7", "path", "/etc"), nil)
	wantJSON(t, result["data"], map[string]any{"stream": "debug", "entries": []any{}, "next": 7})
	if c := client.calls[0]; !strings.HasSuffix(c.url, "/api/mihomo/logs") || c.rawQ != "level=debug&after=7" {
		t.Fatal(c)
	}
	mustPanel(t, m, "logs", "GET", nil, nil)
	if client.calls[1].rawQ != "level=info&after=0" {
		t.Fatal(client.calls[1].rawQ)
	}
}

func TestLiveLogsRejectInvalidLevelOrCursor(t *testing.T) {
	for _, a := range [][]string{{"level", "trace"}, {"level", "silent"}, {"after", "-1"}, {"after", "1e3"},
		{"after", strings.Repeat("9", 19)}} {
		m, client := panelManager(t, panelAgentRecord("version", "1.8.56-go"))
		_, err := panelCall(m, "logs", "GET", args(a...), nil)
		wantMonitorError(t, err, "INVALID_REQUEST", 400)
		if client.count() != 0 {
			t.Fatal(a)
		}
	}
}

func node(name string, extra ...any) map[string]any {
	n := map[string]any{
		"name": name, "type": "Trojan", "udp": true, "xudp": true, "tfo": false, "mptcp": false, "smux": false,
		"interface": "", "routing-mark": 0, "dialer-proxy": "", "provider-name": "sub", "id": strings.Repeat("x", 36), "alive": true,
		"history": []any{map[string]any{"time": "t1", "delay": 300}, map[string]any{"time": "t2", "delay": 120}},
		"extra": map[string]any{"https://cp.cloudflare.com/generate_204": map[string]any{"alive": false,
			"history": []any{map[string]any{"time": "t1", "delay": 90}, map[string]any{"time": "t2", "delay": 0}}}},
	}
	for i := 0; i+1 < len(extra); i += 2 {
		n[extra[i].(string)] = extra[i+1]
	}
	return n
}

func TestNodeListingsKeepOnlyPanelFieldsAndTheNewestDelay(t *testing.T) {
	m, client := panelManager(t, nil)
	group := node("Proxy", "type", "Selector", "now", "HK", "all", []any{"HK", "JP"}, "testUrl", "https://cp.cloudflare.com/generate_204",
		"hidden", false, "icon", "i", "fixed", "")
	client.add(jsonResponse(map[string]any{"proxies": map[string]any{"Proxy": group, "HK": node("HK"), "odd": "not a dict"}}, 200))
	proxies := asMap(asMap(mustPanel(t, m, "proxies", "GET", nil, nil)["data"])["proxies"])
	wantJSON(t, proxies["HK"], map[string]any{
		"name": "HK", "type": "Trojan", "udp": true, "alive": true,
		"history": []any{map[string]any{"time": "t2", "delay": 120}},
		"extra":   map[string]any{"https://cp.cloudflare.com/generate_204": map[string]any{"alive": false, "history": []any{map[string]any{"time": "t2", "delay": 0}}}},
	})
	p := asMap(proxies["Proxy"])
	got := map[string]any{}
	for _, key := range []string{"now", "all", "testUrl", "hidden", "icon", "fixed"} {
		got[key] = p[key]
	}
	wantJSON(t, got, map[string]any{
		"now": "HK", "all": []any{"HK", "JP"}, "testUrl": "https://cp.cloudflare.com/generate_204",
		"hidden": false, "icon": "i", "fixed": "",
	})
	if proxies["odd"] != "not a dict" {
		t.Fatal(proxies)
	}
}

func TestProviderListingSlimsNodesAndKeepsOnlyNamesOfCompatibleMembers(t *testing.T) {
	m, client := panelManager(t, nil)
	subscription := map[string]any{
		"name": "sub", "type": "Proxy", "vehicleType": "HTTP", "updatedAt": "now", "testUrl": "u",
		"subscriptionInfo": map[string]any{"Total": 1}, "proxies": []any{node("HK"), node("JP", "history", []any{})},
	}
	all := make([]any, 500)
	for i := range all {
		all[i] = "HK"
	}
	compatible := map[string]any{
		"name": "Proxy", "type": "Proxy", "vehicleType": "Compatible",
		"proxies": []any{node("DIRECT", "type", "Direct"), node("Auto", "type", "URLTest", "all", all, "now", "HK")},
	}
	client.add(jsonResponse(map[string]any{"providers": map[string]any{"sub": subscription, "Proxy": compatible,
		"empty": map[string]any{"name": "empty", "vehicleType": "File"}}}, 200))
	providers := asMap(asMap(mustPanel(t, m, "providers/proxies", "GET", nil, nil)["data"])["providers"])
	sub := cloneMap(asMap(providers["sub"]))
	delete(sub, "proxies")
	want := cloneMap(subscription)
	delete(want, "proxies")
	wantJSON(t, sub, want)
	nodes := asList(asMap(providers["sub"])["proxies"])
	wantJSON(t, asMap(nodes[0])["history"], []any{map[string]any{"time": "t2", "delay": 120}})
	if _, ok := asMap(nodes[0])["xudp"]; ok {
		t.Fatal(nodes[0])
	}
	wantJSON(t, asMap(nodes[1])["history"], []any{})
	wantJSON(t, asMap(providers["Proxy"])["proxies"], []any{map[string]any{"name": "DIRECT"}, map[string]any{"name": "Auto"}})
	wantJSON(t, providers["empty"], map[string]any{"name": "empty", "vehicleType": "File"})
}

func TestOtherRepliesPassThroughUnchanged(t *testing.T) {
	m, client := panelManager(t, nil)
	reply := map[string]any{"proxies": map[string]any{"HK": node("HK")}}
	client.add(jsonResponse(reply, 200), jsonResponse([]any{"unexpected"}, 200))
	wantJSON(t, mustPanel(t, m, "proxy", "PUT", args("name", "Proxy"), map[string]any{"name": "HK"})["data"], reply)
	wantJSON(t, mustPanel(t, m, "proxies", "GET", nil, nil)["data"], []any{"unexpected"})
}

// --- core update check ---------------------------------------------------------

type ruleCall struct {
	url     string
	request rulefetch.DownloadRequest
}

// latestFake replaces request_rule and get_repository().get_system_settings.
type latestFake struct {
	mu       sync.Mutex
	calls    []ruleCall
	replies  []any // string body or error
	settings map[string]any
}

func (f *latestFake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func installLatest(t *testing.T) *latestFake {
	f := &latestFake{settings: map[string]any{
		"rule_fetch_proxy": "", "github_proxy_domain": "", "backup": map[string]any{"large": "not needed"},
	}}
	previousRule, previousSettings, previousClock := requestRule, downloadSettings, monotonic
	requestRule = func(_ context.Context, u string, r rulefetch.DownloadRequest) (*rulefetch.Response, error) {
		f.mu.Lock()
		f.calls = append(f.calls, ruleCall{u, r})
		reply := f.replies[0]
		f.replies = f.replies[1:]
		f.mu.Unlock()
		if err, ok := reply.(error); ok {
			return nil, err
		}
		return &rulefetch.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(reply.(string)))}, nil
	}
	downloadSettings = func(context.Context) (*model.SystemConfig, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		result := map[string]any{}
		for _, key := range []string{"rule_fetch_proxy", "github_proxy_domain", "server_domain"} {
			if val, ok := f.settings[key]; ok {
				result[key] = val
			} else {
				result[key] = ""
			}
		}
		return testDownloadSettings(t, result), nil
	}
	ResetCoreLatest()
	t.Cleanup(func() {
		requestRule, downloadSettings, monotonic = previousRule, previousSettings, previousClock
		ResetCoreLatest()
	})
	return f
}

func (f *latestFake) add(replies ...any) {
	f.mu.Lock()
	f.replies = append(f.replies, replies...)
	f.mu.Unlock()
}

func TestCoreUpdateCheckFollowsTheCoresOwnChannelAndComparison(t *testing.T) {
	for _, tc := range []struct {
		current, upstream, channel string
		hasUpdate                  bool
	}{
		{"v1.19.32", "v1.19.33\n", "release", true},
		{"v1.19.32", "v1.19.32\n", "release", false},
		{"alpha-0a1b2c3", "alpha-4d5e6f7", "alpha", true},
		{"alpha-0a1b2c3", "alpha-0a1b2c3", "alpha", false},
	} {
		latest := installLatest(t)
		m, client := panelManager(t, panelAgentRecord("version", "1.8.16-go"))
		client.add(jsonResponse(map[string]any{"meta": true, "version": tc.current}, 200))
		latest.add(tc.upstream)
		data := asMap(mustPanel(t, m, "core/update", "GET", nil, nil)["data"])
		if data["current"] != tc.current || data["latest"] != strings.TrimSpace(tc.upstream) ||
			data["channel"] != tc.channel || data["has_update"] != tc.hasUpdate || data["error"] != "" {
			t.Fatal(tc, data)
		}
		r := latest.calls[0]
		if r.url != CoreVersionURLs[tc.channel] || !r.request.Stream || r.request.MaxBytes != 256 {
			t.Fatal(r)
		}
		if r.request.Config == nil || len(r.request.Config.Library) != 0 || len(r.request.Config.Rules) != 0 || r.request.Config.Settings.Fields.Has("backup") {
			t.Fatal("unrelated settings leaked", r.request.Config)
		}
		if r.request.ConnectTimeout != 5*time.Second || r.request.ReadTimeout != 10*time.Second {
			t.Fatal(r.request)
		}
		if len(client.calls) != 1 || client.calls[0].req.Method != "GET" || !strings.HasSuffix(client.calls[0].url, "/api/mihomo/version") {
			t.Fatal(client.calls)
		}
	}
}

func TestCoreUpdateCheckNeedsTheUpgradeAgentVersion(t *testing.T) {
	latest := installLatest(t)
	m, client := panelManager(t, nil)
	_, err := panelCall(m, "core/update", "GET", nil, nil)
	if e := wantMonitorError(t, err, "UPGRADE_REQUIRED", 409); !strings.Contains(e.Message, "1.8.16-go") {
		t.Fatal(e)
	}
	if client.count() != 0 || latest.count() != 0 {
		t.Fatal("contacted")
	}
}

func TestCoreUpdateCheckFailureStillReportsTheRunningVersion(t *testing.T) {
	for _, tc := range []struct {
		failure any
		reason  string
	}{
		{&rulefetch.FetchError{Class: "ConnectTimeout", Message: "https://user:secret@proxy.invalid"}, "连接 GitHub 超时"},
		{&rulefetch.FetchError{Class: "HTTPError", Message: "404", Status: 404}, "GitHub 返回 HTTP 404"},
		{&rulefetch.FetchError{Class: "ConnectionError", Message: "https://user:secret@proxy.invalid"}, "无法连接 GitHub"},
		{"<html>blocked</html>", "version.txt 内容不是版本号"},
	} {
		latest := installLatest(t)
		m, client := panelManager(t, panelAgentRecord("version", "1.8.16-go"))
		client.add(jsonResponse(map[string]any{"meta": true, "version": "v1.19.32"}, 200))
		latest.add(tc.failure)
		data := asMap(mustPanel(t, m, "core/update", "GET", nil, nil)["data"])
		failure := asStr(data["error"])
		if data["current"] != "v1.19.32" || data["latest"] != "" || data["has_update"] != false ||
			!strings.Contains(failure, tc.reason) || strings.Contains(failure, "secret") {
			t.Fatal(tc.reason, data)
		}
	}
}

func TestCoreUpdateCheckFailureReportsTheLastTryOfSourceAndMirror(t *testing.T) {
	err := &rulefetch.RuleFetchError{LastError: &rulefetch.FetchError{Class: "HTTPError", Status: 503}}
	if got := checkFailure(err); got != "GitHub 返回 HTTP 503" {
		t.Fatal(got)
	}
	if got := checkFailure(&rulefetch.RuleFetchError{LastError: rulefetch.ErrSizeLimit}); got != "version.txt 内容不是版本号" {
		t.Fatal(got)
	}
}

func TestCoreUpdateCheckReportsAnUnreachableCore(t *testing.T) {
	latest := installLatest(t)
	m, client := panelManager(t, panelAgentRecord("version", "1.8.16-go"))
	client.add(jsonResponse(map[string]any{"success": false, "code": "CORE_UNREACHABLE"}, 502))
	_, err := panelCall(m, "core/update", "GET", nil, nil)
	wantMonitorError(t, err, "CORE_UNREACHABLE", 502)
	if latest.count() != 0 {
		t.Fatal(latest.calls)
	}
}

func fakeClock(t *testing.T, start float64) *float64 {
	clock := start
	var mu sync.Mutex
	monotonic = func() float64 { mu.Lock(); defer mu.Unlock(); return clock }
	return &clock
}

func TestCoreUpdateCheckCachesGithubAndAManualCheckRefreshes(t *testing.T) {
	latest := installLatest(t)
	clock := fakeClock(t, 1000)
	m, client := panelManager(t, panelAgentRecord("version", "1.8.16-go"))
	check := func(force bool) map[string]any {
		client.add(jsonResponse(map[string]any{"meta": true, "version": "v1.19.32"}, 200))
		a := [][2]string{}
		if force {
			a = args("force", "1")
		}
		return asMap(mustPanel(t, m, "core/update", "GET", a, nil)["data"])
	}
	latest.add(&rulefetch.FetchError{Class: "ConnectTimeout"})
	if asStr(check(false)["error"]) == "" {
		t.Fatal("no error")
	}
	*clock += CoreCheckFailureTTL - 1
	if asStr(check(false)["error"]) == "" || latest.count() != 1 {
		t.Fatal("failure retried")
	}
	*clock += 2
	latest.add("v1.19.32")
	if check(false)["has_update"] != false || latest.count() != 2 {
		t.Fatal("not refreshed")
	}
	*clock += CoreCheckTTL - 1
	if check(false)["latest"] != "v1.19.32" || latest.count() != 2 {
		t.Fatal("cache not used")
	}
	latest.add("v1.19.33")
	data := check(true)
	if data["has_update"] != true || data["latest"] != "v1.19.33" || latest.count() != 3 ||
		!strings.HasSuffix(asStr(data["release_url"]), "/releases/tag/v1.19.33") {
		t.Fatal(data)
	}
}

func TestACoreNewerThanTheReleaseIsNotOfferedADowngrade(t *testing.T) {
	latest := installLatest(t)
	clock := fakeClock(t, 1000)
	m, client := panelManager(t, panelAgentRecord("version", "1.8.16-go"))
	check := func(current string) map[string]any {
		client.add(jsonResponse(map[string]any{"meta": true, "version": current}, 200))
		return asMap(mustPanel(t, m, "core/update", "GET", nil, nil)["data"])
	}
	latest.add("v1.19.33")
	if check("v1.19.32")["has_update"] != true {
		t.Fatal("no update")
	}
	data := check("v1.19.34")
	if data["has_update"] != false || data["latest"] != "v1.19.33" || latest.count() != 1 {
		t.Fatal(data)
	}
	*clock += CoreCheckFailureTTL - 1
	if check("v1.19.34")["has_update"] != false || latest.count() != 1 {
		t.Fatal("refetched early")
	}
	*clock += 2
	latest.add("v1.19.34")
	data = check("v1.19.34")
	if data["has_update"] != false || data["latest"] != "v1.19.34" || latest.count() != 2 {
		t.Fatal(data)
	}
	*clock += CoreCheckFailureTTL + 1
	if check("v1.19.34")["latest"] != "v1.19.34" || latest.count() != 2 {
		t.Fatal("long TTL not used")
	}
}

func TestCoreUpdateCheckFetchesGithubOnceForConcurrentRequests(t *testing.T) {
	installLatest(t)
	release := make(chan struct{})
	var mu sync.Mutex
	var fetches []string
	previous := latestCoreVersion
	t.Cleanup(func() { latestCoreVersion = previous })
	latestCoreVersion = func(_ context.Context, channel string, _ *model.SystemConfig) (string, error) {
		mu.Lock()
		fetches = append(fetches, channel)
		mu.Unlock()
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
		return "v1.19.33", nil
	}
	results := make(chan [2]string, 6)
	for range 6 {
		go func() {
			latest, failure, err := cachedLatest(ctx, "release", "v1.19.32", false)
			if err != nil {
				t.Error(err)
			}
			results <- [2]string{latest, failure}
		}()
	}
	for {
		mu.Lock()
		n := len(fetches)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	for range 6 {
		if r := <-results; r != [2]string{"v1.19.33", ""} {
			t.Fatal(r)
		}
	}
	if len(fetches) != 1 || fetches[0] != "release" {
		t.Fatal(fetches)
	}
	// A manual check started after that fetch finished asks GitHub again.
	if latest, failure, _ := cachedLatest(ctx, "release", "v1.19.32", true); latest != "v1.19.33" || failure != "" || len(fetches) != 2 {
		t.Fatal(latest, failure, fetches)
	}
}

func TestAnExpiredCacheAnswersAtOnceWhileAnotherRequestRefreshes(t *testing.T) {
	installLatest(t)
	settings, _ := downloadSettings(ctx)
	coreLatest["release"] = &coreCached{finishedAt: 0, latest: "v1.19.33", settings: settings}
	monotonic = func() float64 { return CoreCheckTTL + 10 }
	previous := latestCoreVersion
	t.Cleanup(func() { latestCoreVersion = previous })
	latestCoreVersion = func(context.Context, string, *model.SystemConfig) (string, error) {
		t.Fatal("must not fetch")
		return "", nil
	}
	lock := coreLocks["release"]
	if !lock.TryLock() {
		t.Fatal("locked")
	}
	defer lock.Unlock()
	if latest, failure, _ := cachedLatest(ctx, "release", "v1.19.32", false); latest != "v1.19.33" || failure != "" {
		t.Fatal(latest, failure)
	}
}

func TestFailureCacheCountsFromWhenTheFetchEnded(t *testing.T) {
	installLatest(t)
	clock := fakeClock(t, 1000)
	previous := latestCoreVersion
	t.Cleanup(func() { latestCoreVersion = previous })
	latestCoreVersion = func(context.Context, string, *model.SystemConfig) (string, error) {
		*clock += 30
		return "", &rulefetch.FetchError{Class: "ConnectTimeout"}
	}
	if _, failure, _ := cachedLatest(ctx, "release", "v1.19.32", false); failure == "" {
		t.Fatal("no failure")
	}
	if coreLatest["release"].finishedAt != 1030 {
		t.Fatal(coreLatest["release"].finishedAt)
	}
}

func TestChangingTheDownloadProxyRetriesACachedFailureAtOnce(t *testing.T) {
	latest := installLatest(t)
	m, client := panelManager(t, panelAgentRecord("version", "1.8.16-go"))
	check := func() map[string]any {
		client.add(jsonResponse(map[string]any{"meta": true, "version": "v1.19.32"}, 200))
		return asMap(mustPanel(t, m, "core/update", "GET", nil, nil)["data"])
	}
	latest.add(&rulefetch.FetchError{Class: "ConnectionError"})
	if asStr(check()["error"]) == "" || asStr(check()["error"]) == "" || latest.count() != 1 {
		t.Fatal("failure not cached")
	}
	latest.mu.Lock()
	latest.settings["rule_fetch_proxy"] = "http://127.0.0.1:7890"
	latest.mu.Unlock()
	latest.add("v1.19.33")
	data := check()
	if data["error"] != "" || data["has_update"] != true || latest.count() != 2 {
		t.Fatal(data)
	}
	if latest.calls[1].request.Config.Settings.RuleFetchProxy != "http://127.0.0.1:7890" {
		t.Fatal(latest.calls[1].request.Config)
	}
}

// --- deferred case of test_agent_light_reads.py (A01) ---------------------------

type historyFlagManager struct {
	*fakeManager
	reads []bool
}

func (m *historyFlagManager) ReadAgent(c context.Context, id string, history bool) (*model.Agent, error) {
	m.reads = append(m.reads, history)
	return m.fakeManager.ReadAgent(c, id, history)
}

// TestPanelLookupsDoNotCopyTheHistoryButPublicationStatisticsDo is the Go
// equivalent of test_panel_lookups_do_not_copy_the_history_but_publication_statistics_do:
// the panel, monitor and rule update lookups read the Agent without its
// publication history; the publication statistics read it with history.
func TestPanelLookupsDoNotCopyTheHistoryButPublicationStatisticsDo(t *testing.T) {
	base := &fakeManager{agents: map[string]*model.Agent{
		"mihomo": panelAgentRecord("version", "1.8.54-go"),
		"dns": mustAgentMap(map[string]any{
			"id": "dns", "service_type": "oxidns", "version": "1.8.54-go", "host": "dns.test", "port": 8080, "token": "t",
		}),
	}, dataDir: t.TempDir(), client: &replyClient{}}
	m := &historyFlagManager{fakeManager: base}
	if _, err := readPanelAgent(ctx, m, "mihomo", PanelMinAgentVersion, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := readDNSAgent(ctx, m, "dns", MinAgentVersion, false); err != nil {
		t.Fatal(err)
	}
	if _, err := readDNSAgent(ctx, m, "dns", RuleUpdateMinAgentVersion, false); err != nil {
		t.Fatal(err)
	}
	if len(m.reads) != 3 || m.reads[0] || m.reads[1] || m.reads[2] {
		t.Fatal(m.reads)
	}
	if _, err := readDNSAgent(ctx, m, "dns", MinAgentVersion, true); err != nil || !m.reads[3] {
		t.Fatal(err, m.reads)
	}
	// checkpoint reads the history (its records decide the pending task).
	m.reads = nil
	transport(t, nil, nil)
	if err := CheckpointStatistics(ctx, m, "dns", "publish-one", strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	for _, history := range m.reads {
		if !history {
			t.Fatal(m.reads)
		}
	}
}

// --- Python details beyond the ported cases ----------------------------------------

func TestPanelASCIIIntegerRules(t *testing.T) {
	for text, want := range map[string]int64{"5000": 5000, " 7300 ": 7300, "+600": 600, "1_000": 1000,
		"　5000　": 5000} {
		got, ok := decimalInteger(text)
		if !ok || got.Int64() != want {
			t.Fatal(text, got, ok)
		}
	}
	for _, text := range []string{"", "1__0", "_1", "1_", "²", "٣٠٠٠", "０５０００", "1e3", "0x10", "\x1c5", "5 0", strings.Repeat("1", 4301)} {
		if _, ok := decimalInteger(text); ok {
			t.Fatal(text)
		}
	}
	// The bound counts digits; a sign and valid separators do not consume it.
	for _, text := range []string{"+" + strings.Repeat("1", 4300), "-1" + strings.Repeat("_1", 4299)} {
		if _, ok := decimalInteger(text); !ok {
			t.Fatal("valid ASCII decimal rejected at the digit limit")
		}
	}
	for text, want := range map[string]bool{"7": true, "²": false, "١٢": false, "①": false, "": false, "-1": false, "1e3": false, "½": false} {
		if asciiDigits(text) != want {
			t.Fatal(text)
		}
	}
	// 4301 digits fail with the generic message (int() ValueError), 0 with the range message.
	m, _ := panelManager(t, nil)
	_, err := panelCall(m, "proxy/delay", "GET", args("name", "HK", "timeout", strings.Repeat("1", 4301)), nil)
	if e := wantMonitorError(t, err, "INVALID_REQUEST", 400); e.Message != PanelMessages["INVALID_REQUEST"] {
		t.Fatal(e)
	}
}

func TestPanelAnswersThatAreNotJSONOrTooLarge(t *testing.T) {
	m, client := panelManager(t, nil)
	client.add(&agents.Response{StatusCode: 404, Body: []byte("404 page not found")},
		&agents.Response{StatusCode: 200, Body: []byte("not json")},
		&agents.Response{StatusCode: 500, Body: []byte("oops")},
		&agents.Response{StatusCode: 200, Body: make([]byte, PanelMaxResponseBytes+1)},
		&agents.Response{StatusCode: 401, Body: []byte("{}")},
		errors.New("refused"),
		jsonResponse(map[string]any{"success": false, "code": []any{"x"}}, 502),
		&agents.Response{StatusCode: 200, Body: []byte(" \r\n")},
		jsonResponse(map[string]any{"message": strings.Repeat("长", 400)}, 418))
	_, err := panelCall(m, "version", "GET", nil, nil)
	if e := wantMonitorError(t, err, "UPGRADE_REQUIRED", 409); e.Message != "请将此 Agent 升级到 1.8.15-go 或更新版本后使用 Mihomo 面板。" {
		t.Fatal(e)
	}
	_, err = panelCall(m, "version", "GET", nil, nil)
	wantMonitorError(t, err, "INVALID_RESPONSE", 502)
	_, err = panelCall(m, "version", "GET", nil, nil)
	if e := wantMonitorError(t, err, "CORE_ERROR", 502); e.Message != "Mihomo 返回 HTTP 500" {
		t.Fatal(e)
	}
	_, err = panelCall(m, "version", "GET", nil, nil)
	wantMonitorError(t, err, "INVALID_RESPONSE", 502)
	_, err = panelCall(m, "version", "GET", nil, nil)
	wantMonitorError(t, err, "AGENT_AUTH_FAILED", 502)
	_, err = panelCall(m, "version", "GET", nil, nil)
	wantMonitorError(t, err, "AGENT_UNREACHABLE", 502)
	_, err = panelCall(m, "version", "GET", nil, nil)
	var monitorErr *Error
	if err == nil || errors.As(err, &monitorErr) {
		t.Fatal("an unhashable code is a TypeError (500)", err)
	}
	wantJSON(t, mustPanel(t, m, "version", "GET", nil, nil), map[string]any{"success": true, "data": nil})
	_, err = panelCall(m, "version", "GET", nil, nil)
	if e := wantMonitorError(t, err, "CORE_ERROR", 400); runeLen(e.Message) != 300 {
		t.Fatal(e)
	}
	_, err = panelCall(m, "nothing", "GET", nil, nil)
	if e := wantMonitorError(t, err, "NOT_ALLOWED", 404); e.Message != "面板不支持这个操作。" {
		t.Fatal(e)
	}
}

func testDownloadSettings(t *testing.T, o map[string]any) *model.SystemConfig {
	t.Helper()
	var settings model.SystemConfig
	if err := json.Unmarshal(mustMarshal(t, o), &settings); err != nil {
		t.Fatal(err)
	}
	return &settings
}
