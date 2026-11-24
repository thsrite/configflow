package routers_test

import (
	"configflow/internal/routers/oplock"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/agents"
	apiagents "configflow/internal/api/agents"
	api "configflow/internal/api/routers"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	core "configflow/internal/routers"
	routerrulesync "configflow/internal/routers/rulesync"
)

func decodeJSON(t *testing.T, res *httpxtest.Result) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(res.Body, &out); err != nil {
		t.Fatalf("%v: %s", err, res.Body)
	}
	return out
}

func wireRouterTestHooks(t *testing.T) {
	t.Helper()
	previous := repo.SetHooks(repo.Hooks{RecordHooks: repo.RecordHooks{JobLockRecord: core.JobLockRecord}})
	t.Cleanup(func() { repo.SetHooks(previous) })
}

func fixture(t *testing.T) *httpxtest.Server {
	t.Helper()
	s := httpxtest.New(t, httpxtest.Options{Auth: true})
	m, err := agents.New(s.Context(), s.Repo, agents.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	nt := notifications.New(notifications.Options{Repo: s.Repo, Summarize: core.NotificationSummary})
	rtr := core.New(s.Repo, core.Options{Notifications: nt})
	rr := routerrulesync.New(s.Repo, routerrulesync.Options{Notifications: nt})
	api.New(api.Deps{Repo: s.Repo, Agents: m, Routers: rtr, RuleSync: rr, Notifications: nt}).Register(s.App.Router)
	wireRouterTestHooks(t)
	return s
}
func routerBody(endpoint string) map[string]any {
	return map[string]any{"name": "Synthetic", "type": "routeros", "endpoint": endpoint, "username": "synthetic-user", "password": "synthetic-password"}
}
func TestR15CRUDHeadAndAuth(t *testing.T) {
	s := fixture(t)
	login := s.Login()
	s.Get("/api/routers").WantStatus(401)
	before, _ := s.Repo.ConfigRevision(s.Context(), "default")
	head := s.Do("HEAD", "/api/routers", login, httpxtest.JSON(routerBody("http://192.0.2.1")))
	head.WantStatus(200)
	after, _ := s.Repo.ConfigRevision(s.Context(), "default")
	if len(head.Body) != 0 || before != after {
		t.Fatal("HEAD created a router")
	}
	created := s.Post("/api/routers", login, httpxtest.JSON(routerBody("http://192.0.2.1")))
	created.WantStatus(200)
	if strings.Contains(created.Text(), "synthetic-password") {
		t.Fatal("password exposed")
	}
	id := decodeJSON(t, created)["router"].(map[string]any)["id"].(string)
	s.Put("/api/routers/"+id, login, httpxtest.JSON(map[string]string{"name": "Renamed"})).WantStatus(200)
	s.Delete("/api/routers/"+id, login).WantStatus(200)
	s.Get("/api/routers").WantStatus(401)
	s.Post("/api/routers/missing/probe", login).WantStatus(404)
}
func TestR15JobLockPrecedesValidation(t *testing.T) {
	s := fixture(t)
	login := s.Login()
	created := s.Post("/api/routers", login, httpxtest.JSON(routerBody("http://192.0.2.1")))
	created.WantStatus(200)
	id := decodeJSON(t, created)["router"].(map[string]any)["id"].(string)
	cfg, _ := s.Repo.Config(s.Context(), "default", repo.ResourceRoots{})
	raw, _ := json.Marshal(cfg)
	r := core.ManagementRouters(raw)[0]
	_, release, lockErr := oplock.RouterJobLock(s.Context(), "default", id, r)
	if lockErr != nil {
		t.Fatal("lock")
	}
	defer release()
	for _, tc := range []struct{ method, path string }{{"PUT", ""}, {"DELETE", ""}, {"POST", "/probe"}, {"POST", "/apply"}, {"POST", "/routes/apply"}, {"PUT", "/policy-settings"}, {"PUT", "/rule-sync"}} {
		s.Do(tc.method, "/api/routers/"+id+tc.path, login, httpxtest.JSON([]any{})).WantStatus(409)
	}
}
func TestR15FlaskRoutePriorities(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{Setup: func(app *httpx.App) {
		app.BeforeRequest(func(c *httpx.Ctx) (*httpx.Response, error) {
			if c.Endpoint != "" {
				return httpx.JSON(map[string]string{"endpoint": c.Endpoint}), nil
			}
			return nil, nil
		})
	}})
	apiagents.NewHandler(apiagents.Deps{Repo: s.Repo}).Register(s.App.Router)
	api.New(api.Deps{Repo: s.Repo}).Register(s.App.Router)
	for _, tc := range []struct{ method, path, endpoint string }{{"GET", "/api/agents/gateway-installations/upstream-routers", "agents.gateway_installation_status"}, {"GET", "/api/agents/i/status", "agents.download_install_command"}, {"POST", "/api/agents/one/mihomo/rule-targets", "agents.mihomo_panel"}, {"GET", "/api/agents/one/mihomo/rule-targets", "agents.rule_targets"}, {"GET", "/api/agents/one/upstream-routers", "agents.upstream_router_pending"}} {
		result := s.Do(tc.method, tc.path)
		result.WantStatus(200)
		if decodeJSON(t, result)["endpoint"] != tc.endpoint {
			t.Fatalf("%s => %s", tc.path, result.Text())
		}
	}
}

func TestR15WorkspaceDraftIsOfflineAndAtomic(t *testing.T) {
	s := fixture(t)
	login := s.Login()
	created := s.Post("/api/routers", login, httpxtest.JSON(routerBody("http://192.0.2.1")))
	created.WantStatus(200)
	id := decodeJSON(t, created)["router"].(map[string]any)["id"].(string)
	path := "/api/routers/" + id + "/policy-settings"
	state := s.Get(path, login)
	state.WantStatus(200)
	workspace := decodeJSON(t, state)["workspace"].(map[string]any)
	request := map[string]any{"revision": workspace["revision"], "base_url": "", "router": map[string]any{"ip_rule_sets": []string{}, "ros_policy_rule_sets": []string{}, "proxy_gateway_agent_id": "gateway"}}
	saved := s.Put(path, login, httpxtest.JSON(request))
	saved.WantStatus(200)
	s.Put(path, login, httpxtest.JSON(request)).WantStatus(409)
	stored, _ := s.Repo.Profile(s.Context(), "default")
	if stored.Routers.Items[0].ProxyGatewayAgentID != "gateway" {
		t.Fatal("draft not saved")
	}
	before, _ := s.Repo.ConfigRevision(s.Context(), "default")
	s.Do("HEAD", path, login, httpxtest.JSON(request)).WantStatus(200)
	after, _ := s.Repo.ConfigRevision(s.Context(), "default")
	if before != after {
		t.Fatal("workspace HEAD wrote")
	}
}

func TestR15EveryRouteRequiresAuthentication(t *testing.T) {
	s := fixture(t)
	root := "/api/routers/missing"
	cases := []struct{ method, path string }{{"GET", "/api/routers"}, {"POST", "/api/routers"}, {"PUT", root}, {"DELETE", root}, {"POST", root + "/probe"}, {"GET", root + "/report"}, {"POST", root + "/plan"}, {"POST", root + "/apply"}, {"GET", root + "/routes"}, {"POST", root + "/routes/plan"}, {"POST", root + "/routes/apply"}, {"GET", root + "/policy"}, {"GET", root + "/policy-settings"}, {"PUT", root + "/policy-settings"}, {"POST", root + "/policy-settings/preview"}, {"GET", root + "/rule-sync"}, {"PUT", root + "/rule-sync"}, {"POST", root + "/rule-sync"}, {"GET", "/api/routers/gateway-forwarding"}, {"POST", "/api/routers/gateway-forwarding"}, {"GET", "/api/agents/missing/upstream-routers"}}
	before, _ := s.Repo.ConfigRevision(s.Context(), "default")
	for _, tc := range cases {
		s.Do(tc.method, tc.path, httpxtest.JSON([]any{})).WantStatus(401)
	}
	s.Get("/api/agents/missing/upstream-routers?profile=missing").WantStatus(401)
	after, _ := s.Repo.ConfigRevision(s.Context(), "default")
	if before != after {
		t.Fatal("unauthenticated request committed")
	}
}
