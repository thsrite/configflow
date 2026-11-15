package agents_test

// Route cases of backend/test_oxidns_monitor.py
// (test_routes_require_admin_auth_and_never_accept_agent_token,
// test_clients_route_auth_no_store_and_agent_isolation) and
// backend/test_oxidns_dashboard.py
// (test_reset_route_auth_method_no_store_and_unchanged_config). A fake
// Agent client answers instead of the patched requests.Session / _request.

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

const oxidnsToken = "agent-private-token-0123456789abcdef"

type oxidnsAgent struct {
	mu    sync.Mutex
	calls []string
}

func (a *oxidnsAgent) Do(_ context.Context, r agents.Request) (*agents.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, _ := url.Parse(r.URL)
	a.calls = append(a.calls, u.Path)
	section := func() *jsonx.Object { return jsonx.Obj("sample_size", 0, "rows", []any{}, "at_limit", false) }
	var data any
	switch u.Path {
	case "/api/oxidns/overview":
		data = jsonx.Obj("success", true, "health", jsonx.Obj("version", "1.6.1", "status", "ok", "uptime_ms", 1234,
			"instance_id", "instance"), "capabilities", jsonx.Obj("query_recorders", []any{"records"}),
			"collected_at_ms", 1000, "source_revision", strings.Repeat("a", 64), "runtime_revision", strings.Repeat("d", 64),
			"metrics", "server_request_total{plugin_tag=\"udp\",protocol=\"udp\"} 5\n",
			"config", "private configuration must not be forwarded")
	case "/api/oxidns/rankings":
		blocked := jsonx.Obj("available", true)
		blocked.Update(section())
		data = jsonx.Obj("success", true, "recorder", "records", "collected_at_ms", 1000,
			"source_revision", strings.Repeat("a", 64), "runtime_revision", strings.Repeat("d", 64), "instance_id", "instance",
			"retention_days", 7, "sample_limit", 10000, "clients", section(), "domains", section(), "blocked", blocked)
	case "/api/oxidns/clients":
		data = jsonx.Obj("ok", true, "recorder", "records", "sample_size", 3,
			"rows", []any{jsonx.Obj("key", "192.168.0.2", "count", 2), jsonx.Obj("key", "2001:db8::2", "count", 1)},
			"token", "core-private-token")
	default:
		data = jsonx.Obj("ok", true, "records", []any{}, "next_cursor", nil)
	}
	body, _ := jsonx.Default(data)
	return &agents.Response{StatusCode: 200, Header: http.Header{}, Body: body}, nil
}

func (a *oxidnsAgent) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

func oxidnsServer(t *testing.T, version string) (*httpxtest.Server, *oxidnsAgent, httpxtest.Option) {
	t.Helper()
	reset := func() { agents.ResetRuntime(); agents.ResetHooks(); monitor.ResetRankingsCache() }
	reset()
	t.Cleanup(reset)
	fake := &oxidnsAgent{}
	t.Cleanup(agents.SetDefaultClient(fake))
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(apiagents.Register)})
	_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{jsonx.Obj("id", "oxi", "name", "DNS", "service_type", "oxidns", "profile_id", "default",
			"token", oxidnsToken, "host", "127.0.0.1", "port", 8082, "version", version, "status", "online")})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, fake, s.Login()
}

func TestOxidnsRoutesRequireAdminAuthAndNeverAcceptAgentToken(t *testing.T) {
	s, fake, admin := oxidnsServer(t, "1.7.0-go")
	for _, suffix := range []string{"overview", "queries", "queries/42"} {
		path := "/api/agents/oxi/oxidns/" + suffix
		s.Get(path).WantStatus(401)
		s.Get(path, httpxtest.Bearer(oxidnsToken)).WantStatus(401)
	}
	if fake.count() != 0 {
		t.Fatal(fake.calls)
	}
	r := s.Get("/api/agents/oxi/oxidns/overview", admin).WantStatus(200)
	if r.Header.Get("Cache-Control") != "no-store" || r.Contains(oxidnsToken) || r.Contains("private configuration") {
		t.Fatal(r.Header, r.Text())
	}
	before := fake.count()
	s.Post("/api/agents/oxi/oxidns/overview", admin).WantStatus(405)
	if fake.count() != before {
		t.Fatal(fake.calls)
	}
}

func TestOxidnsClientsRouteAuthNoStoreAndAgentIsolation(t *testing.T) {
	s, fake, admin := oxidnsServer(t, "1.7.1-go")
	endpoint := "/api/agents/oxi/oxidns/clients"
	s.Get(endpoint).WantStatus(401)
	s.Get(endpoint, httpxtest.Bearer(oxidnsToken)).WantStatus(401)
	s.Post(endpoint, admin).WantStatus(405)
	if fake.count() != 0 {
		t.Fatal(fake.calls)
	}
	r := s.Get(endpoint, admin).WantStatus(200)
	if r.Header.Get("Cache-Control") != "no-store" || r.Contains(oxidnsToken) || r.Contains("core-private-token") {
		t.Fatal(r.Header, r.Text())
	}
	before := fake.count()
	s.Get("/api/agents/missing/oxidns/clients", admin).WantStatus(404).WantJSON(jsonx.Obj("success", false,
		"code", "AGENT_NOT_FOUND", "message", "未找到这台 Agent。"))
	r = s.Get(endpoint+"?agent_id=elsewhere", admin).WantStatus(400)
	if r.Header.Get("Cache-Control") != "no-store" || fake.count() != before {
		t.Fatal(r.Header, fake.calls)
	}
}

func TestOxidnsResetRouteAuthMethodNoStoreAndUnchangedConfig(t *testing.T) {
	s, fake, admin := oxidnsServer(t, "1.8.14-go")
	url := "/api/agents/oxi/oxidns/dashboard/reset"
	s.Post(url, httpxtest.JSON(jsonx.NewObject())).WantStatus(401)
	s.Post(url, httpxtest.JSON(jsonx.NewObject()), httpxtest.Bearer(oxidnsToken)).WantStatus(401)
	s.Get(url, admin).WantStatus(405)
	if fake.count() != 0 {
		t.Fatal(fake.calls)
	}
	before, err := s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	r := s.Post(url, httpxtest.JSON(jsonx.NewObject()), admin).WantStatus(200)
	if r.Header.Get("Cache-Control") != "no-store" || r.Object().GetObject("statistics").Value("reason") != "manual" || r.Contains(oxidnsToken) {
		t.Fatal(r.Header, r.Text())
	}
	after, _ := s.Repo.Store().Fingerprint()
	if before != after {
		t.Fatal("configuration changed", before, after)
	}
	invalid := jsonx.Obj("success", false, "code", "INVALID_QUERY", "message", "重置请求格式无效。")
	s.Post(url+"?agent_id=elsewhere", httpxtest.JSON(jsonx.NewObject()), admin).WantStatus(400).WantJSON(invalid)
	s.Post(url, httpxtest.JSON(jsonx.Obj("recorder", strings.Repeat("x", 3000))), admin).WantStatus(400).WantJSON(invalid)
	s.Post(url, httpxtest.Body([]byte("{"), "application/json"), admin).WantStatus(400).WantJSON(invalid)
	s.Post(url, httpxtest.Body([]byte("{}"), "text/plain"), admin).WantStatus(400).WantJSON(invalid)
	s.Post(url, httpxtest.Body([]byte(`{"recorder": "../x"}`), "application/json"), admin).WantStatus(400).
		WantJSON(jsonx.Obj("success", false, "code", "INVALID_QUERY", "message", "查询条件无效，请检查筛选内容。"))
	s.Get("/api/agents/oxi/oxidns/overview?recorder=records", admin).WantStatus(200)
	s.Get("/api/agents/oxi/oxidns/overview?unknown=1", admin).WantStatus(400)
}
