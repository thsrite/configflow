package agents_test

// Routes of A05: ports of the route cases of backend/test_traffic_history.py
// and of the traffic-report cases of backend/test_domain_discovery.py (C37's
// file; these run with A05), the traffic-report half of
// test_agent_light_reads.py::test_heartbeat_and_agent_reports_never_copy_the_history
// (deferred from A01), and the traffic stats/trend routes (Python has no
// route tests for them; expectations follow the handlers).

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/trafficstore"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/api/domaindiscovery"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/rules"
)

const trafficToken = "agent-token-0123456789abcdef"

type trafficEnv struct {
	s       *httpxtest.Server
	manager *agents.Manager
	admin   []httpxtest.Option
}

// newTrafficEnv is the env fixture of test_traffic_history.py: agents gw
// (mihomo, 1.8.51-go) and dns (oxidns). With login, admin holds the login.
func newTrafficEnv(t *testing.T, login bool) trafficEnv {
	t.Helper()
	reset := func() {
		metrics.Reset()
		metrics.ResetTraffic()
		metrics.SetNativeTrafficFlush(nil)
		trafficstore.ResetCache()
		agents.ResetRuntime()
		agents.ResetHooks()
	}
	reset()
	t.Cleanup(reset)
	metrics.Wire()
	metrics.WireTraffic()
	s := httpxtest.New(t, httpxtest.Options{Auth: login, Register: httpxtest.Routes(apiagents.Register)})
	_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{
			jsonx.Obj("id", "gw", "name", "Edge", "service_type", "mihomo", "profile_id", "default", "token", trafficToken,
				"host", "h", "port", 8080, "version", "1.8.51-go"),
			jsonx.Obj("id", "dns", "name", "DNS", "service_type", "oxidns", "profile_id", "default", "token", trafficToken+"x",
				"host", "h", "port", 8080),
		})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := agents.Get(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	env := trafficEnv{s: s, manager: m}
	if login {
		env.admin = []httpxtest.Option{s.Login()}
	}
	return env
}

func agentAuth(token string) httpxtest.Option { return httpxtest.Bearer(token) }

func currentHour() int64 { return time.Now().Unix() / 3600 * 3600 }

func historyReport(down int64) *jsonx.Object {
	return jsonx.Obj("schema", 2, "usage", []any{jsonx.Obj("hour", currentHour(), "source", "10.0.0.2",
		"chain", []any{"人工智能", "美国自建", "IEPL专线"}, "up", 1, "down", down, "conns", 1)}, "domains", []any{}, "dropped", 0)
}

func (e trafficEnv) with(options ...httpxtest.Option) []httpxtest.Option {
	return append(append([]httpxtest.Option{}, e.admin...), options...)
}

func (e trafficEnv) storedGW(t *testing.T) *jsonx.Object {
	return storedAgents(t, e.s)[0].(*jsonx.Object)
}

func TestSwitchReportsAndHeartbeat(t *testing.T) {
	e := newTrafficEnv(t, false)
	s := e.s
	url := "/api/agents/gw/traffic-history"
	body := httpxtest.JSON(historyReport(10))
	s.Post(url, body).WantStatus(401)
	s.Post(url, body, agentAuth(trafficToken)).WantStatus(409).WantJSON(jsonx.Obj("success", false, "enabled", false, "message", "Traffic history disabled"))
	if s.Post("/api/agents/gw/heartbeat", httpxtest.JSON(jsonx.NewObject()), agentAuth(trafficToken)).WantStatus(200).Object().Value("traffic_history_enabled") != false {
		t.Fatal("heartbeat enabled")
	}

	response := s.Put(url, httpxtest.JSON(jsonx.Obj("enabled", true))).WantStatus(200).Object()
	if response.GetObject("data").Value("enabled") != true {
		t.Fatal(jsonx.Repr(response))
	}
	since := e.storedGW(t).Value("traffic_history_since")
	if s.Post("/api/agents/gw/heartbeat", httpxtest.JSON(jsonx.NewObject()), agentAuth(trafficToken)).Object().Value("traffic_history_enabled") != true {
		t.Fatal("heartbeat not enabled")
	}
	s.Post(url, body, agentAuth(trafficToken)).WantStatus(200).WantJSON(jsonx.Obj("success", true))
	s.Post(url, httpxtest.JSON(jsonx.Obj("schema", 1, "usage", "x", "domains", []any{})), agentAuth(trafficToken)).WantStatus(400).
		WantJSON(jsonx.Obj("success", false, "message", "Invalid traffic history report"))

	data := s.Get(url + "?range=24h&by=source").WantStatus(200).Object().GetObject("data")
	if data.Value("enabled") != true || !jsonx.Equal(data.Value("since"), since) || data.Value("supported") != true {
		t.Fatal(jsonx.Repr(data))
	}
	if !jsonx.Equal(data.Value("retention_days"), 90) || data.GetList("rows")[0].(*jsonx.Object).Value("key") != "10.0.0.2" {
		t.Fatal(jsonx.Repr(data))
	}
	// 1.8.51-go 只有两层链路，提示更新到能上报完整链路的版本
	if data.Value("full_chain") != false || data.Value("full_chain_version") != "1.8.52-go" {
		t.Fatal(jsonx.Repr(data))
	}
	s.Get(url + "?range=1y").WantStatus(400).WantJSON(jsonx.Obj("success", false, "message", "range 或 by 参数无效"))
	s.Get(url + "?by=nope").WantStatus(400)
	s.Put("/api/agents/dns/traffic-history", httpxtest.JSON(jsonx.Obj("enabled", true))).WantStatus(400).
		WantJSON(jsonx.Obj("success", false, "message", "流量记录只支持 mihomo Agent"))

	// 再次开启不改开始时间
	s.Put(url, httpxtest.JSON(jsonx.Obj("enabled", true))).WantStatus(200)
	if !jsonx.Equal(e.storedGW(t).Value("traffic_history_since"), since) {
		t.Fatal("since changed")
	}

	// 关闭并删除数据
	s.Put(url, httpxtest.JSON(jsonx.Obj("enabled", false, "clear", true))).WantStatus(200)
	result, err := metrics.TrafficForManager(e.manager).QueryTraffic(s.Context(), "gw", "24h", "source", nil)
	if err != nil || len(result.Rows) != 0 {
		t.Fatal("not cleared", err)
	}
	s.Post(url, body, agentAuth(trafficToken)).WantStatus(409)
}

func TestOldAgentIsReportedUnsupportedAndRetentionIsValidated(t *testing.T) {
	e := newTrafficEnv(t, false)
	s := e.s
	if _, err := e.manager.UpdateAgents(s.Context(), "", func(_ context.Context, list *[]any) (any, error) {
		(*list)[0].(*jsonx.Object).Set("version", "1.8.50-go")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if s.Get("/api/agents/gw/traffic-history").Object().GetObject("data").Value("supported") != false {
		t.Fatal("supported")
	}
	for _, value := range []any{7, "90", nil, true, 90.0} {
		s.Put("/api/agents/traffic-history/retention", httpxtest.JSON(jsonx.Obj("days", value))).WantStatus(400).
			WantJSON(jsonx.Obj("success", false, "message", "保留时长只能是 30、90、180 或 365 天"))
	}
	s.Put("/api/agents/traffic-history/retention", httpxtest.Body([]byte("[]"), "application/json")).WantStatus(400)
	s.Put("/api/agents/traffic-history/retention", httpxtest.JSON(jsonx.Obj("days", 180))).WantStatus(200).
		WantJSON(jsonx.Obj("success", true, "data", jsonx.Obj("retention_days", 180)))
	if !jsonx.Equal(s.Get("/api/agents/gw/traffic-history").Object().GetObject("data").Value("retention_days"), 180) {
		t.Fatal("retention not stored")
	}
}

func TestTrafficHistoryRoutesAnswerUnknownAndBadPayloads(t *testing.T) {
	e := newTrafficEnv(t, false)
	s := e.s
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		options := []httpxtest.Option{}
		if method == "PUT" {
			options = append(options, httpxtest.JSON(jsonx.Obj("enabled", true)))
		}
		s.Do(method, "/api/agents/missing/traffic-history", options...).WantStatus(404).WantJSON(jsonx.Obj("success", false, "message", "Agent not found"))
	}
	s.Get("/api/agents/dns/traffic-history").WantStatus(400)
	s.Delete("/api/agents/dns/traffic-history").WantStatus(400)
	for _, payload := range []any{jsonx.Obj("enabled", "yes"), jsonx.Obj("enabled", true, "clear", nil), []any{}, nil} {
		s.Put("/api/agents/missing/traffic-history", httpxtest.JSON(payload)).WantStatus(400).
			WantJSON(jsonx.Obj("success", false, "message", "enabled 必须是布尔值"))
	}
	raw, err := json.Marshal(historyReport(5))
	if err != nil {
		t.Fatal(err)
	}
	if err := metrics.TrafficForManager(e.manager).AddTraffic(s.Context(), "gw", metrics.DecodeTrafficReport(raw, nil)); err != nil {
		t.Fatal(err)
	}
	s.Delete("/api/agents/gw/traffic-history").WantStatus(200).WantJSON(jsonx.Obj("success", true))
	if metrics.TrafficForManager(e.manager).PendingCount() != 0 {
		t.Fatal("not cleared")
	}
	// The Agent body limit: 4 MiB.
	s.Post("/api/agents/gw/traffic-history", httpxtest.Body([]byte(strings.Repeat(" ", metrics.TrafficReportMaxContentLength+1)), "application/json"),
		agentAuth(trafficToken)).WantStatus(413).WantJSON(jsonx.Obj("success", false, "message", "Request body too large"))
}

func TestAgentsCanReportWhenPanelLoginIsEnabled(t *testing.T) {
	e := newTrafficEnv(t, true)
	s := e.s
	if _, err := e.manager.UpdateAgents(s.Context(), "", func(_ context.Context, list *[]any) (any, error) {
		agent := (*list)[0].(*jsonx.Object)
		agent.Set("version", "1.8.52-go")
		agent.Set("traffic_history_enabled", true)
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	url := "/api/agents/gw/traffic-history"
	body := httpxtest.JSON(historyReport(10))
	s.Post(url, body, agentAuth(trafficToken)).WantStatus(200)
	s.Post(url, body, agentAuth("wrong-token")).WantStatus(401)
	// 查询、开关和清空是网页端接口：Agent 令牌不行，要网页登录
	s.Get(url, agentAuth(trafficToken)).WantStatus(401)
	s.Put(url, agentAuth(trafficToken), httpxtest.JSON(jsonx.Obj("enabled", false))).WantStatus(401)
	s.Delete(url, agentAuth(trafficToken)).WantStatus(401)
	data := s.Get(url+"?range=24h&by=source", e.admin...).WantStatus(200).Object().GetObject("data")
	if data.GetObject("total").GetInt("down") != 10 {
		t.Fatal(jsonx.Repr(data))
	}
}

// test_every_agent_token_route_bypasses_the_panel_login: every route that
// authenticates with the Agent's own token passes the panel login.
func TestEveryAgentTokenRouteBypassesThePanelLogin(t *testing.T) {
	e := newTrafficEnv(t, true)
	s := e.s
	for _, path := range []string{"/api/agents/gw/heartbeat", "/api/agents/gw/traffic-report", "/api/agents/gw/traffic-history"} {
		result := s.Post(path, httpxtest.JSON(jsonx.NewObject()), agentAuth(trafficToken))
		if result.Status == 401 {
			t.Errorf("%s: blocked by the panel login", path)
		}
	}
}

// ---------- traffic-report (test_domain_discovery.py) ----------

func discoveryItem(host string) *jsonx.Object {
	return jsonx.Obj("host", host, "port", 443, "network", "tcp", "rule", "Match", "rule_payload", "",
		"policy", "漏网之鱼", "outlet", "direct", "conns", 1, "fails", 0, "fail_kinds", jsonx.NewObject(),
		"zero_dl", 0, "up", 10, "down", 20)
}

func discoveryReport(items ...any) *jsonx.Object {
	if items == nil {
		items = []any{}
	}
	return jsonx.Obj("schema", 1, "status", "running", "mihomo_version", "v1.19.32",
		"window_start", "2026-10-10T10:00:00Z", "window_end", "2026-10-10T10:05:00Z",
		"ip_only_conns", 0, "dropped", 0, "items", items)
}

func (e trafficEnv) setDiscovery(t *testing.T, enabled bool) {
	t.Helper()
	if _, err := e.manager.SetDomainDiscovery(e.s.Context(), "gw", enabled); err != nil {
		t.Fatal(err)
	}
}

func (e trafficEnv) trafficFile(t *testing.T) string {
	data, err := os.ReadFile(filepath.Join(e.s.DataDir, "traffic", "gw.json"))
	if err != nil {
		return ""
	}
	return string(data)
}

func TestReportRequiresTokenAndEnabledSwitch(t *testing.T) {
	e := newTrafficEnv(t, false)
	s := e.s
	url := "/api/agents/gw/traffic-report"
	s.Post(url, httpxtest.JSON(discoveryReport())).WantStatus(401)
	s.Post(url, httpxtest.JSON(discoveryReport()), agentAuth("wrong")).WantStatus(401)
	s.Post(url, httpxtest.JSON(discoveryReport()), agentAuth(trafficToken)).WantStatus(409).
		WantJSON(jsonx.Obj("success", false, "enabled", false, "message", "Domain discovery disabled"))
	if s.Post("/api/agents/gw/heartbeat", httpxtest.JSON(jsonx.NewObject()), agentAuth(trafficToken)).Object().Value("domain_discovery_enabled") != false {
		t.Fatal("heartbeat")
	}
	e.setDiscovery(t, true)
	if s.Post("/api/agents/gw/heartbeat", httpxtest.JSON(jsonx.NewObject()), agentAuth(trafficToken)).Object().Value("domain_discovery_enabled") != true {
		t.Fatal("heartbeat")
	}
	bad := discoveryReport()
	bad.Set("schema", 9)
	s.Post(url, httpxtest.JSON(bad), agentAuth(trafficToken)).WantStatus(400).WantJSON(jsonx.Obj("success", false, "message", "Invalid traffic report"))
	item := discoveryItem("www.blocked.com")
	item.Set("fails", 5)
	s.Post(url, httpxtest.JSON(discoveryReport(item)), agentAuth(trafficToken)).WantStatus(200).
		WantJSON(jsonx.Obj("success", true, "enabled", true, "rule_providers", jsonx.NewObject()))
	stored := trafficstore.New(filepath.Join(s.DataDir, "traffic")).LoadStatus("gw").(*jsonx.Object)
	if stored.Value("value") != "running" || stored.Value("mihomo_version") != "v1.19.32" {
		t.Fatal(jsonx.Repr(stored))
	}
	// Unhashable status: TypeError in clean_report → 500 page; nothing stored.
	before := e.trafficFile(t)
	listStatus := discoveryReport()
	listStatus.Set("status", []any{})
	s.Post(url, httpxtest.JSON(listStatus), agentAuth(trafficToken)).WantStatus(500)
	if e.trafficFile(t) != before {
		t.Fatal("stored")
	}
	s.Post(url, httpxtest.Body([]byte(strings.Repeat(" ", trafficstore.ReportMaxContentLength+1)), "application/json"),
		agentAuth(trafficToken)).WantStatus(413)
}

func TestReportWithLoginEnabledRejectsInvalidAgentCredentials(t *testing.T) {
	for name, authorization := range map[string]string{
		"missing": "", "wrong-scheme": "Basic " + trafficToken, "wrong-token": "Bearer wrong-token",
		"malformed": "Bearer  " + trafficToken, "non-ascii": "Bearer café", "other-agent": "Bearer " + trafficToken + "x",
		"admin-jwt": "admin",
	} {
		e := newTrafficEnv(t, true)
		e.setDiscovery(t, true)
		before := system(t, e.s)
		options := []httpxtest.Option{httpxtest.JSON(discoveryReport(discoveryItem("www.blocked.com")))}
		switch authorization {
		case "":
		case "admin":
			options = append(options, e.admin...)
		default:
			options = append(options, httpxtest.Header("Authorization", authorization))
		}
		e.s.Post("/api/agents/gw/traffic-report", options...).WantStatus(401)
		if !jsonx.Equal(system(t, e.s), before) || e.trafficFile(t) != "" {
			t.Errorf("%s: state changed", name)
		}
	}
}

func TestReportWithLoginEnabledRetainsSwitchAndPayloadChecks(t *testing.T) {
	bad := discoveryReport()
	bad.Set("schema", 9)
	for _, c := range []struct {
		enabled bool
		payload *jsonx.Object
		status  int
	}{{false, discoveryReport(), 409}, {true, bad, 400}} {
		e := newTrafficEnv(t, true)
		e.setDiscovery(t, c.enabled)
		e.s.Post("/api/agents/gw/traffic-report", httpxtest.JSON(c.payload), agentAuth(trafficToken)).WantStatus(c.status)
		if e.trafficFile(t) != "" {
			t.Errorf("enabled=%v stored", c.enabled)
		}
	}
}

func TestReportWithLoginEnabledRejectsUnknownAgent(t *testing.T) {
	e := newTrafficEnv(t, true)
	e.s.Post("/api/agents/unknown/traffic-report", httpxtest.JSON(discoveryReport()), agentAuth(trafficToken)).WantStatus(401)
}

// The traffic-report half of test_heartbeat_and_agent_reports_never_copy_the_history:
// the Agent is authenticated from the light record and its publication
// history survives untouched.
func TestTrafficReportKeepsThePublicationHistory(t *testing.T) {
	e := newTrafficEnv(t, false)
	if _, err := e.manager.UpdateAgents(e.s.Context(), "", func(_ context.Context, list *[]any) (any, error) {
		deployments := jsonx.NewObject()
		for _, key := range []string{"release-0", "release-1", "release-2", "release-3", "release-4"} {
			deployments.Set(key, jsonx.Obj("deployment_id", key, "status", "succeeded"))
		}
		(*list)[0].(*jsonx.Object).Set("deployments", deployments)
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	status := e.s.Post("/api/agents/gw/traffic-report", httpxtest.JSON(jsonx.NewObject()), agentAuth(trafficToken)).Status
	if status != 400 && status != 409 {
		t.Fatal(status)
	}
	if e.storedGW(t).GetObject("deployments").Len() != 5 {
		t.Fatal("history lost")
	}
}

// provider_revisions: the MD5 of what /rules/local serves for each active
// default discovery ruleset (cache file first, else the stored content).
func TestTrafficReportReturnsProviderRevisions(t *testing.T) {
	e := newTrafficEnv(t, false)
	s := e.s
	ctx := s.Context()
	cached := jsonx.Obj("id", "rs-proxy", "name", "域名发现-代理", "source_type", "content", "behavior", "classical", "content", "DOMAIN,a.example")
	library := []any{
		jsonx.Obj("id", "rs-direct", "name", "域名发现-直连", "source_type", "content", "behavior", "domain", "content", "+.example.com"),
		cached,
		jsonx.Obj("id", "rs-us", "name", "域名发现-🇺🇸 美国", "source_type", "content", "behavior", "domain", "content", ""),
		jsonx.Obj("id", "rs-late", "name", "after match", "source_type", "content", "behavior", "domain", "content", "x"),
	}
	if _, err := s.Repo.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("rule_library", library)
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, profile *jsonx.Object) (*jsonx.Object, error) {
		profile.Set("rule_configs", []any{
			jsonx.Obj("id", "r1", "itemType", "ruleset", "library_rule_id", "rs-direct", "policy", "DIRECT", "enabled", true),
			jsonx.Obj("id", "r2", "itemType", "ruleset", "library_rule_id", "rs-proxy", "policy", "PROXY", "enabled", true),
			jsonx.Obj("id", "r3", "itemType", "ruleset", "library_rule_id", "rs-us", "policy", "US", "enabled", true),
			jsonx.Obj("id", "r4", "itemType", "rule", "rule_type", "MATCH", "policy", "DIRECT", "enabled", true),
			jsonx.Obj("id", "r5", "itemType", "ruleset", "library_rule_id", "rs-late", "policy", "DIRECT", "enabled", true),
		})
		profile.Set("domain_discovery", jsonx.Obj("direct_ruleset", "rs-direct", "proxy_ruleset", "rs-proxy",
			"regional_rulesets", jsonx.Obj("US", "rs-us", "late", "rs-late", "gone", "rs-missing")))
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	rulesDir, _ := s.Repo.SharedRulesDir()
	cachePath := filepath.Join(rulesDir, rules.RuleCacheFilename(cached))
	os.MkdirAll(filepath.Dir(cachePath), 0o755)
	if err := os.WriteFile(cachePath, []byte("DOMAIN,a.example\nDOMAIN,b.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.setDiscovery(t, true)
	sum := func(text string) string { digest := md5.Sum([]byte(text)); return hex.EncodeToString(digest[:]) }
	s.Post("/api/agents/gw/traffic-report", httpxtest.JSON(discoveryReport()), agentAuth(trafficToken)).WantStatus(200).
		WantJSON(jsonx.Obj("success", true, "enabled", true, "rule_providers", jsonx.Obj(
			"域名发现-直连", sum("+.example.com"),
			"域名发现-代理", sum("DOMAIN,a.example\nDOMAIN,b.example\n"),
			"域名发现-🇺🇸 美国", sum(""))))
}

func TestTrafficStatsAndTrendRoutes(t *testing.T) {
	e := newTrafficEnv(t, true)
	s := e.s
	for _, path := range []string{"/traffic/stats", "/traffic/trend"} {
		s.Get("/api/agents/gw" + path).WantStatus(401)
		s.Get("/api/agents/missing"+path, e.admin...).WantStatus(404).WantJSON(jsonx.Obj("success", false, "message", "Agent not found"))
	}
	stats := s.Get("/api/agents/gw/traffic/stats?period=today", e.admin...).WantStatus(200).Object().GetObject("data")
	if stats.Value("agent_id") != "gw" || stats.GetObject("stats").Value("period") != "today" {
		t.Fatal(jsonx.Repr(stats))
	}
	trend := s.Get("/api/agents/gw/traffic/trend?hours=abc&interval=10", e.admin...).WantStatus(200).Object().GetObject("data")
	if !jsonx.Equal(trend, jsonx.Obj("agent_id", "gw", "trend", []any{}, "hours", 24, "interval_minutes", 10, "data_points", 0)) {
		t.Fatal(jsonx.Repr(trend))
	}
}

func TestReviewN810CorruptDiscoveryFileHTTPRefusalAndReset(t *testing.T) {
	e := newTrafficEnv(t, false)
	e.setDiscovery(t, true)
	s := e.s
	domaindiscovery.Register(s.App.Router)
	path := filepath.Join(s.DataDir, "traffic", "gw.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	var baseline any
	readBaseline := map[string]any{}
	for _, content := range []string{`{"days":[]}`, "{", "", "[]", "null", `"old"`, "42", `{"status":{"value":"running"},"days":{},"broken":NaN}`, "{\"status\":{\"value\":\"running\"},\"days\":{},\"broken\":\"\xff\"}"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		answer := s.Post("/api/agents/gw/traffic-report", httpxtest.JSON(discoveryReport()), agentAuth(trafficToken)).WantStatus(500).Object()
		if baseline == nil {
			baseline = answer
		} else if !jsonx.Equal(baseline, answer) {
			t.Fatalf("error differs from days corruption: %s", jsonx.Repr(answer))
		}
		for _, route := range []string{"/api/domain-discovery/agents", "/api/domain-discovery/domains?agent_id=gw"} {
			value := s.Get(route).WantStatus(200).Object()
			if previous, exists := readBaseline[route]; exists {
				if !jsonx.Equal(previous, value) {
					t.Fatalf("read %s differs from corrupt days: %s", route, jsonx.Repr(value))
				}
			} else {
				readBaseline[route] = value
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != content {
			t.Fatalf("HTTP refusal changed original: %q %v", raw, err)
		}
		s.Post("/api/agents/gw/domain-discovery/clear").WantStatus(200)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("reset kept corrupt file", err)
		}
		// Negative control: existing reset leaves a missing file, which must accept reports.
		s.Post("/api/agents/gw/traffic-report", httpxtest.JSON(discoveryReport()), agentAuth(trafficToken)).WantStatus(200)
	}
}
