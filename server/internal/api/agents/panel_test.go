package agents_test

// Route cases of backend/test_mihomo_panel.py
// (test_route_requires_admin_auth_and_forwards_name,
// test_provider_node_route_requires_admin_and_keeps_provider_healthcheck),
// backend/test_oxidns_cache.py (test_route_*) and
// backend/test_oxidns_rule_update.py (test_route_*). The Python tests patch
// requests.Session.request or the bridge's call(); here a fake Agent client
// answers and records what reached it.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

type panelSeen struct {
	method, path, rawQuery, authorization string
	body                                  string
}

type panelFake struct {
	mu      sync.Mutex
	seen    []panelSeen
	answers map[string]*agents.Response // by path; default {"success": true, ...}
}

func (f *panelFake) Do(_ context.Context, r agents.Request) (*agents.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, _ := url.Parse(r.URL)
	f.seen = append(f.seen, panelSeen{r.Method, u.EscapedPath(), u.RawQuery, r.Header.Get("Authorization"), string(r.Body)})
	if answer, ok := f.answers[u.Path]; ok {
		copied := *answer
		return &copied, nil
	}
	var data any = jsonx.Obj("ok", true)
	switch u.Path {
	case "/api/oxidns/cache":
		data = jsonx.Obj("success", true, "caches", []any{})
	case "/api/oxidns/cache/flush":
		data = jsonx.Obj("success", true, "flushed", []any{"probe"})
	case "/api/oxidns/rules/update":
		data = jsonx.Obj("success", true, "task", jsonx.Obj("id", strings.Repeat("a", 32), "status", "running",
			"phase", "downloading", "downloaded", 0, "download_total", 2, "reloaded", 0, "reload_total", 1))
		body, _ := jsonx.Default(data)
		return &agents.Response{StatusCode: 202, Header: http.Header{}, Body: body}, nil
	}
	body, _ := jsonx.Default(data)
	return &agents.Response{StatusCode: 200, Header: http.Header{}, Body: body}, nil
}

func (f *panelFake) calls() []panelSeen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]panelSeen(nil), f.seen...)
}

const (
	panelToken = "agent-private-token-for-the-panel-0123"
	dnsToken   = "agent-private-token-for-oxidns-01234567"
)

func panelServer(t *testing.T, mihomoVersion string) (*httpxtest.Server, *panelFake, httpxtest.Option) {
	t.Helper()
	reset := func() { agents.ResetRuntime(); agents.ResetHooks() }
	reset()
	t.Cleanup(reset)
	fake := &panelFake{answers: map[string]*agents.Response{}}
	t.Cleanup(agents.SetDefaultClient(fake))
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(apiagents.Register)})
	_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{
			jsonx.Obj("id", "gw", "name", "Gateway", "service_type", "mihomo", "profile_id", "default", "token", panelToken,
				"host", "127.0.0.1", "port", 8080, "version", mihomoVersion, "status", "online"),
			jsonx.Obj("id", "dns", "name", "DNS", "service_type", "oxidns", "profile_id", "default", "token", dnsToken,
				"host", "127.0.0.1", "port", 8082, "version", "1.8.36-go", "status", "online"),
		})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, fake, s.Login()
}

func TestPanelRouteRequiresAdminAuthAndForwardsName(t *testing.T) {
	s, fake, admin := panelServer(t, "1.8.15-go")
	path := "/api/agents/gw/mihomo/proxy?name=%E8%8A%82%E7%82%B9%2FA"
	s.Put(path, httpxtest.JSON(jsonx.Obj("name", "HK"))).WantStatus(401)
	s.Put(path, httpxtest.JSON(jsonx.Obj("name", "HK")), httpxtest.Bearer(panelToken)).WantStatus(401)
	if len(fake.calls()) != 0 {
		t.Fatal(fake.calls())
	}
	r := s.Put(path, httpxtest.JSON(jsonx.Obj("name", "HK")), admin).WantStatus(200)
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(r.Header)
	}
	if c := fake.calls()[0]; !strings.HasSuffix(c.path, "/api/mihomo/proxies/%E8%8A%82%E7%82%B9%2FA") || c.body != `{"name": "HK"}` {
		t.Fatal(c)
	}
	r = s.Get("/api/agents/gw/mihomo/restart", admin).WantStatus(404).WantJSON(jsonx.Obj("success", false,
		"code", "NOT_ALLOWED", "message", "面板不支持这个操作。"))
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(r.Header)
	}
}

func TestProviderNodeRouteRequiresAdminAndKeepsProviderHealthcheck(t *testing.T) {
	s, fake, admin := panelServer(t, "1.8.18-go")
	path := "/api/agents/gw/mihomo/providers/proxy/delay"
	params := httpxtest.Query("provider", "订阅/A", "name", "节点/B")
	s.Get(path, params).WantStatus(401)
	if len(fake.calls()) != 0 {
		t.Fatal(fake.calls())
	}
	body, _ := jsonx.Default(jsonx.Obj("delay", 19))
	fake.answers["/api/mihomo/providers/proxies/订阅/A/节点/B/healthcheck"] = &agents.Response{StatusCode: 200, Body: body}
	r := s.Get(path, params, admin).WantStatus(200)
	if !jsonx.Equal(r.Object().Value("data"), jsonx.Obj("delay", 19)) || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(r.Text())
	}
	s.Post(path, params, admin).WantStatus(404)
	if calls := fake.calls(); len(calls) != 1 ||
		!strings.HasSuffix(calls[0].path, "/providers/proxies/%E8%AE%A2%E9%98%85%2FA/%E8%8A%82%E7%82%B9%2FB/healthcheck") {
		t.Fatal(calls)
	}
}

func TestPanelRouteBodyIsReadOnlyForPutAndPatch(t *testing.T) {
	s, fake, admin := panelServer(t, "1.8.16-go")
	// A PATCH body that is not JSON is None: the mode check refuses it.
	s.Do("PATCH", "/api/agents/gw/mihomo/configs", httpxtest.Body([]byte(`{"mode":"rule"}`), "text/plain"), admin).
		WantStatus(400).WantJSON(jsonx.Obj("success", false, "code", "INVALID_REQUEST", "message", "代理模式只能是 rule、global 或 direct。"))
	s.Do("PATCH", "/api/agents/gw/mihomo/configs", httpxtest.Body([]byte(`{"mode":"rule"}`), "application/json"), admin).WantStatus(200)
	// POST bodies are never read or forwarded.
	s.Post("/api/agents/gw/mihomo/upgrade", httpxtest.JSON(jsonx.Obj("path", "/tmp/x")), admin).WantStatus(200)
	calls := fake.calls()
	if len(calls) != 2 || calls[0].body != `{"mode": "rule"}` || calls[1].body != "" || calls[1].method != "POST" {
		t.Fatal(calls)
	}
	s.Get("/api/agents/missing/mihomo/version", admin).WantStatus(404).WantJSON(jsonx.Obj("success", false,
		"code", "AGENT_NOT_FOUND", "message", "未找到这台 Agent。"))
}

func TestCacheRouteRequiresAdminAndPassesOnlyThePathAgent(t *testing.T) {
	s, fake, admin := panelServer(t, "1.8.15-go")
	path := "/api/agents/dns/oxidns/cache/flush"
	for _, option := range []httpxtest.Option{httpxtest.Header("X-Nothing", "1"), httpxtest.Bearer(dnsToken)} {
		s.Post(path, httpxtest.JSON(jsonx.Obj("tags", []any{"probe"})), option).WantStatus(401)
	}
	if len(fake.calls()) != 0 {
		t.Fatal(fake.calls())
	}
	r := s.Post(path, httpxtest.JSON(jsonx.Obj("tags", []any{"probe"})), admin, httpxtest.Header("X-ConfigFlow-Profile", "unrelated")).WantStatus(200)
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(r.Header)
	}
	r.WantJSON(jsonx.Obj("success", true, "flushed", []any{"probe"}))
	if c := fake.calls()[0]; c.method != "POST" || c.path != "/api/oxidns/cache/flush" || c.body != `{"tags": ["probe"]}` ||
		c.authorization != "Bearer "+dnsToken {
		t.Fatal(c)
	}
	s.Get("/api/agents/dns/oxidns/cache", admin).WantStatus(200).WantJSON(jsonx.Obj("success", true, "caches", []any{}))
	if c := fake.calls()[1]; c.method != "GET" || c.path != "/api/oxidns/cache" || c.body != "" {
		t.Fatal(c)
	}
}

func TestCacheRouteReturnsPartialFlushDetails(t *testing.T) {
	s, fake, admin := panelServer(t, "1.8.15-go")
	body, _ := jsonx.Default(jsonx.Obj("success", false, "code", "CACHE_FLUSH_FAILED", "message", "core secret", "flushed", []any{"main_cache"}))
	fake.answers["/api/oxidns/cache/flush"] = &agents.Response{StatusCode: 502, Body: body}
	s.Post("/api/agents/dns/oxidns/cache/flush", httpxtest.JSON(jsonx.Obj("tags", []any{"main_cache", "probe"})), admin).
		WantStatus(502).WantJSON(jsonx.Obj("success", false, "code", "CACHE_FLUSH_FAILED",
		"message", monitor.CacheMessages["CACHE_FLUSH_FAILED"], "flushed", []any{"main_cache"}))
}

func TestCacheRouteRejectsInjectedOrInvalidPayloads(t *testing.T) {
	s, fake, admin := panelServer(t, "1.8.15-go")
	for _, tc := range []struct{ suffix, data, contentType string }{
		{"?tag=probe", `{"tags":["probe"]}`, "application/json"},
		{"", `{}`, "application/json"},
		{"", `{"tags":["probe"],"agent_id":"other"}`, "application/json"},
		{"", `{"tags":["../reload"]}`, "application/json"},
		{"", `{"tags":["probe"]}`, "text/plain"},
		{"", `{`, "application/json"},
		{"", strings.Repeat(" ", 16*1024+1), "application/json"},
	} {
		r := s.Post("/api/agents/dns/oxidns/cache/flush"+tc.suffix, httpxtest.Body([]byte(tc.data), tc.contentType), admin).WantStatus(400)
		if r.Object().Value("code") != "INVALID_REQUEST" || r.Header.Get("Cache-Control") != "no-store" {
			t.Fatal(tc, r.Text())
		}
		s.Get("/api/agents/dns/oxidns/cache", httpxtest.Body([]byte("{}"), "application/json"), admin).WantStatus(400)
	}
	// set() of a tag list holding a dict raises (500), as in Python.
	s.Post("/api/agents/dns/oxidns/cache/flush", httpxtest.Body([]byte(`{"tags":[{}]}`), "application/json"), admin).WantStatus(500)
	if len(fake.calls()) != 0 {
		t.Fatal("unexpected Agent request", fake.calls())
	}
}

func TestRuleUpdateRouteRequiresAdminAndNeverAcceptsAgentToken(t *testing.T) {
	s, fake, admin := panelServer(t, "1.8.15-go")
	path := "/api/agents/dns/oxidns/rules/update"
	for _, option := range []httpxtest.Option{httpxtest.Header("X-Nothing", "1"), httpxtest.Bearer(dnsToken)} {
		s.Post(path, httpxtest.JSON(jsonx.NewObject()), option).WantStatus(401)
		s.Get(path, option).WantStatus(401)
	}
	if len(fake.calls()) != 0 {
		t.Fatal(fake.calls())
	}
	r := s.Post(path, httpxtest.JSON(jsonx.NewObject()), admin, httpxtest.Header("X-ConfigFlow-Profile", "unrelated")).WantStatus(202)
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(r.Header)
	}
	if c := fake.calls()[0]; c.method != "POST" || c.path != "/api/oxidns/rules/update" || c.body != "{}" {
		t.Fatal(c)
	}
}

func TestRuleUpdateRouteRejectsInjectedOrInvalidPayloads(t *testing.T) {
	s, fake, admin := panelServer(t, "1.8.15-go")
	for _, tc := range []struct{ method, suffix, data, contentType string }{
		{"POST", "?url=http://example.test", `{}`, "application/json"},
		{"POST", "", `{"tag":"arbitrary"}`, "application/json"},
		{"POST", "", `[]`, "application/json"},
		{"POST", "", `null`, "application/json"},
		{"POST", "", `{}`, "text/plain"},
		{"POST", "", `{`, "application/json"},
		{"POST", "", strings.Repeat(" ", 1025), "application/json"},
		{"GET", "", `{}`, "application/json"},
	} {
		r := s.Do(tc.method, "/api/agents/dns/oxidns/rules/update"+tc.suffix, httpxtest.Body([]byte(tc.data), tc.contentType), admin).WantStatus(400)
		if r.Object().Value("code") != "INVALID_REQUEST" || r.Header.Get("Cache-Control") != "no-store" {
			t.Fatal(tc, r.Text())
		}
	}
	if len(fake.calls()) != 0 {
		t.Fatal("unexpected Agent request", fake.calls())
	}
	s.Get("/api/agents/dns/oxidns/rules/update", admin).WantStatus(202)
	s.Get("/api/agents/gw/oxidns/rules/update", admin).WantStatus(400).WantJSON(jsonx.Obj("success", false,
		"code", "WRONG_SERVICE", "message", "更新规则仅适用于 OxiDNS Agent。"))
}
