package agents_test

// Ports of backend/test_agent_upgrade_auth.py (upgrade authentication
// through the application's request hooks) and the route cases of
// backend/test_agent_upgrade_messages.py. Deferred: the binary download
// cases (GET /api/agents/download/<filename>, A10), the GET /api/agents and
// GET /api/agents/<id> parts (A13) and push-config (A14).

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/upgrades"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

type upgradeApp struct {
	t          *testing.T
	s          *httpxtest.Server
	m          *agents.Manager
	agentID    string
	agentToken string
	otherID    string
	otherToken string
	admin      httpxtest.Option
	static     string
}

// newUpgradeApp is the authenticated_app fixture.
func newUpgradeApp(t *testing.T) *upgradeApp {
	t.Helper()
	agents.ResetRuntime()
	t.Cleanup(agents.ResetRuntime)
	t.Setenv("AGENT_REGISTRATION_KEY", "")
	os.Unsetenv("AGENT_REGISTRATION_KEY")
	static := t.TempDir()
	for _, arch := range upgrades.Arches {
		os.WriteFile(filepath.Join(static, "configflow-agent-"+arch), []byte("\x7fELF"+arch), 0o644)
	}
	t.Setenv("AGENTS_STATIC_DIR", static)
	upgrades.ResetArtifactCache()
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "upgrade-admin", Password: "upgrade-test-password",
		DefaultFactories: true})
	app := &upgradeApp{t: t, s: s, static: static}
	app.admin = s.Login()
	m, err := agents.New(s.Context(), s.Repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	apiagents.NewHandler(apiagents.Deps{Repo: s.Repo, Agents: m}).Register(s.App.Router)
	app.m = m
	for _, name := range []string{"upgrading", "other-agent"} {
		id, token := registerAgentForTest(t, s.Context(), m, map[string]any{
			"name": name, "host": "127.0.0.1", "port": 1234,
			"service_type": "mihomo", "deployment_method": "shell", "version": "1.1.0-go",
		})
		if app.agentID == "" {
			app.agentID, app.agentToken = id, token
		} else {
			app.otherID, app.otherToken = id, token
		}
	}
	t.Cleanup(upgrades.SetRequestForTest(func(context.Context, *model.Agent, string, string, []byte) (*agents.Response, error) {
		t.Fatal("unexpected Agent call")
		return nil, nil
	}))
	return app
}

func (a *upgradeApp) id() string { return a.agentID }

func (a *upgradeApp) setUpgrade(state map[string]any) {
	a.t.Helper()
	if err := a.m.EditAgents(a.s.Context(), "", func(_ context.Context, rows *[]model.Agent) error {
		for i := range *rows {
			if (*rows)[i].ID == a.agentID {
				return model.SetStoredMember(&(*rows)[i], "latest_upgrade", mustRawJSON(a.t, state))
			}
		}
		return nil
	}); err != nil {
		a.t.Fatal(err)
	}
}

// seedUpgrade is seed_upgrade(manager, agent): a protocol 1 update downloading.
func (a *upgradeApp) seedUpgrade() map[string]any {
	now := agents.NowTimestamp()
	state := map[string]any{
		"update_id": strings.Repeat("a", 32), "agent_id": a.id(), "protocol": 1,
		"target_version": agents.GetLatestVersion(), "previous_version": "1.1.0-go",
		"status": "downloading", "started_at": now, "updated_at": now,
	}
	a.setUpgrade(state)
	return state
}

func (a *upgradeApp) stored(agentID string) map[string]any {
	a.t.Helper()
	agent, err := a.m.ReadAgent(a.s.Context(), agentID, true)
	if err != nil {
		a.t.Fatal(err)
	}
	return parseJSONMap(a.t, mustRawJSON(a.t, agent))
}

func (a *upgradeApp) system() map[string]any { return system(a.t, a.s) }

func (a *upgradeApp) agentAuth() httpxtest.Option {
	return httpxtest.Bearer(a.agentToken)
}

func (a *upgradeApp) respond(fn func(method, path string, payload map[string]any) *agents.Response) {
	a.t.Cleanup(upgrades.SetRequestForTest(func(_ context.Context, _ *model.Agent, method, path string, raw []byte) (*agents.Response, error) {
		var payload map[string]any
		if len(raw) > 0 {
			payload = parseJSONMap(a.t, raw)
		}
		return fn(method, path, payload), nil
	}))
}

func TestLegacyAgentCanReadAndReportOwnUpgradeThenAdminConfirmsRollback(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	path := "/api/agents/" + a.id() + "/upgrade"
	// A bootstrap read must not recursively poll the not-yet-started Agent.
	if got := parseBodyMap(t, a.s.Get(path, a.agentAuth()).WantStatus(200)); got["update_id"] != state["update_id"] {
		t.Fatalf("got %v", got)
	}
	got := parseBodyMap(t, a.s.Post(path+"/report", a.agentAuth(), httpxtest.JSON(map[string]any{
		"update_id": state["update_id"], "status": "rolling_back", "error": "migration failed",
	})).WantStatus(200))
	if got["status"] != "rolling_back" || mapHas(a.stored(a.otherID), "latest_upgrade") {
		t.Fatalf("got %v", got)
	}
	a.s.Post("/api/agents/"+a.id()+"/heartbeat", a.agentAuth(), httpxtest.JSON(map[string]any{"version": "1.1.0-go"})).WantStatus(200)
	a.respond(func(string, string, map[string]any) *agents.Response {
		return upgradeAgentResponse(map[string]any{}, 404)
	})
	r := a.s.Get(path, a.admin).WantStatus(200)
	if parseBodyMap(t, r)["status"] != "rolled_back" || r.Contains(a.agentToken) || r.Contains(a.otherToken) {
		t.Fatal(r.Text())
	}
}

func TestUpgradeRoutesRejectMissingOrForeignAgentCredentials(t *testing.T) {
	for _, credential := range []string{"missing", "wrong", "other", "malformed"} {
		for _, operation := range []string{"read", "report"} {
			a := newUpgradeApp(t)
			state := a.seedUpgrade()
			before := a.system()
			var options []httpxtest.Option
			switch credential {
			case "wrong":
				options = append(options, httpxtest.Bearer("wrong-token"))
			case "other":
				options = append(options, httpxtest.Bearer(a.otherToken))
			case "malformed":
				options = append(options, httpxtest.Bearer(a.agentToken+" extra"))
			}
			path := "/api/agents/" + a.id() + "/upgrade"
			if operation == "read" {
				a.s.Get(path, options...).WantStatus(401)
			} else {
				options = append(options, httpxtest.JSON(map[string]any{"update_id": state["update_id"], "status": "rolling_back"}))
				a.s.Post(path+"/report", options...).WantStatus(401)
			}
			if !testjson.Equal(a.system(), before) {
				t.Fatalf("%s/%s mutated", credential, operation)
			}
		}
	}
}

func TestAdminJWTCannotImpersonateAgentRollbackReport(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	a.s.Post("/api/agents/"+a.id()+"/upgrade/report", a.admin,
		httpxtest.JSON(map[string]any{"update_id": state["update_id"], "status": "rolling_back"})).WantStatus(401)
	if !testjson.Equal(a.stored(a.id())["latest_upgrade"], state) {
		t.Fatal("changed")
	}
}

func TestAuthenticatedReportStillRequiresMatchingLegacyTask(t *testing.T) {
	for _, payload := range []map[string]any{
		{"update_id": "wrong-task", "status": "rolling_back"},
		{"update_id": strings.Repeat("a", 32), "status": "succeeded"},
	} {
		a := newUpgradeApp(t)
		state := a.seedUpgrade()
		wantBodyJSON(t, a.s.Post("/api/agents/"+a.id()+"/upgrade/report", a.agentAuth(), httpxtest.JSON(payload)).WantStatus(400),
			map[string]any{"success": false, "message": "Agent 上报的更新结果格式无效。"})
		if !testjson.Equal(a.stored(a.id())["latest_upgrade"], state) {
			t.Fatal("changed")
		}
	}
}

func TestReportBodyLimitAndMalformedReports(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	path := "/api/agents/" + a.id() + "/upgrade/report"
	big := map[string]any{"update_id": state["update_id"], "status": "rolling_back", "error": strings.Repeat("x", apiagents.ReportBodyLimit)}
	wantBodyJSON(t, a.s.Post(path, a.agentAuth(), httpxtest.JSON(big)).WantStatus(413), map[string]any{"success": false})
	for _, body := range []string{`null`, `[1]`, `"x"`, `5`} {
		a.s.Post(path, a.agentAuth(), httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
	}
	a.s.Post(path, a.agentAuth(), httpxtest.Body([]byte(`{`), "application/json")).WantStatus(400)
	a.s.Post(path, a.agentAuth(), httpxtest.Body([]byte(`{}`), "text/plain")).WantStatus(415)
	if !testjson.Equal(a.stored(a.id())["latest_upgrade"], state) {
		t.Fatal("changed")
	}
}

func TestUpgradeExemptionsDoNotOpenAgentAdministration(t *testing.T) {
	// GET /api/agents, GET /api/agents/<id> (A13), push-config (A14) and the
	// download route (A10) are checked when those routes exist.
	for _, credential := range []string{"missing", "agent"} {
		for _, c := range [][2]string{{"POST", "/{id}/update"}, {"POST", "/{id}/restart"}, {"POST", "/{id}/uninstall"},
			{"DELETE", "/{id}"}, {"GET", "/{id}/upgrade-extra"}, {"GET", "/download-extra/configflow-agent-linux-amd64"}} {
			a := newUpgradeApp(t)
			before := a.system()
			var options []httpxtest.Option
			if credential == "agent" {
				options = append(options, a.agentAuth())
			}
			a.s.Do(c[0], "/api/agents"+strings.ReplaceAll(c[1], "{id}", a.id()), options...).WantStatus(401)
			if !testjson.Equal(a.system(), before) {
				t.Fatalf("%s %s mutated", credential, c[1])
			}
		}
	}
}

func TestAdminCanStillReadUpgradeStatus(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	state["status"] = "failed"
	a.setUpgrade(state)
	got := parseBodyMap(t, a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(200))
	if got["update_id"] != state["update_id"] || got["status"] != "failed" {
		t.Fatalf("got %v", got)
	}
}

func TestAdminStartsUpgradeForBothAgentProtocols(t *testing.T) {
	for _, protocol := range []int{1, 2} {
		a := newUpgradeApp(t)
		var submitted []map[string]any
		a.respond(func(method, path string, payload map[string]any) *agents.Response {
			if method == "GET" && path == "/api/upgrade-info" {
				status := 200
				if protocol == 1 {
					status = 404
				}
				return upgradeAgentResponse(map[string]any{"agent_id": a.id(), "deployment_method": "shell", "architecture": "linux-amd64"}, status)
			}
			if method != "POST" || path != "/api/update" {
				t.Fatalf("%s %s", method, path)
			}
			submitted = append(submitted, payload)
			if protocol == 2 {
				return upgradeAgentResponse(map[string]any{}, 202)
			}
			return upgradeAgentResponse(map[string]any{}, 200)
		})
		state := parseBodyMap(t, a.s.Post("/api/agents/"+a.id()+"/update", a.admin, httpxtest.JSON(map[string]any{})).WantStatus(202))
		if state["status"] != "queued" || !testjson.Equal(state["protocol"], protocol) ||
			submitted[0]["update_id"] != state["update_id"] || submitted[0]["version"] != agents.GetLatestVersion() {
			t.Fatalf("state %v submitted %v", state, submitted)
		}
		if protocol == 2 && submitted[0]["sha256"] != mapObj(mapObj(state, "artifacts"), "linux-amd64")["sha256"] {
			t.Fatal("sha256")
		}
	}
}

func TestReregisterPreservesUpgradeRecordAndPendingOperationLock(t *testing.T) {
	for _, status := range []string{"checking", "unknown", "rollback_failed", "succeeded"} {
		a := newUpgradeApp(t)
		state := a.seedUpgrade()
		state["status"] = status
		a.setUpgrade(state)
		got := parseBodyMap(t, a.s.Post("/api/agents/register", a.agentAuth(), httpxtest.JSON(map[string]any{
			"name": "upgrading", "host": "127.0.0.1", "port": 1234, "service_type": "mihomo",
			"deployment_method": "shell", "version": agents.GetLatestVersion(),
		})).WantStatus(200))
		if got["id"] != a.agentID {
			t.Fatalf("got %v", got)
		}
		registered := a.stored(a.id())
		if !testjson.Equal(registered["latest_upgrade"], state) || registered["token"] != a.agentToken ||
			registered["version"] != agents.GetLatestVersion() {
			t.Fatalf("%s: registered %v", status, registered)
		}
		if status != "succeeded" {
			// Re-registration cannot unlock a pending/unknown/failed-rollback task.
			a.s.Delete("/api/agents/"+a.id(), a.admin).WantStatus(409)
			if !testjson.Equal(a.stored(a.id())["latest_upgrade"], state) {
				t.Fatal("changed")
			}
		}
	}
}

func TestReregisteredNewAgentCanConfirmPreservedUpgrade(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	a.s.Post("/api/agents/register", a.agentAuth(), httpxtest.JSON(map[string]any{
		"name": "upgrading", "host": "127.0.0.1", "port": 1234, "service_type": "mihomo",
		"deployment_method": "shell", "version": agents.GetLatestVersion(),
	})).WantStatus(200)
	a.respond(func(string, string, map[string]any) *agents.Response {
		return upgradeAgentResponse(map[string]any{"agent_id": a.id(), "version": state["target_version"], "migration_ready": true}, 200)
	})
	got := parseBodyMap(t, a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(200))
	if got["status"] != "succeeded" || got["update_id"] != state["update_id"] ||
		mapObj(a.stored(a.id()), "latest_upgrade")["status"] != "succeeded" {
		t.Fatalf("got %v", got)
	}
}

// --- test_agent_upgrade_messages.py route cases ---

func TestStatusLocalizesOldRecordsWithoutRewritingThem(t *testing.T) {
	// The list and detail parts need GET /api/agents and /api/agents/<id> (A13).
	for _, status := range []string{"failed", "rolled_back", "rollback_failed"} {
		a := newUpgradeApp(t)
		state := a.seedUpgrade()
		raw := "Agent download size or SHA-256 mismatch"
		state["status"] = status
		state["error"] = raw
		a.setUpgrade(state)
		original := cloneMap(t, mapObj(a.stored(a.id()), "latest_upgrade"))
		a.respond(func(string, string, map[string]any) *agents.Response {
			return upgradeAgentResponse(map[string]any{}, 404)
		})
		progress := parseBodyMap(t, a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(200))
		if progress["error"] != agents.UpgradeErrorMessage(raw) || progress["status"] != status ||
			progress["update_id"] != original["update_id"] {
			t.Fatalf("progress %v", progress)
		}
		if !testjson.Equal(a.stored(a.id())["latest_upgrade"], original) {
			t.Fatal("rewritten")
		}
		// The old Agent's bootstrap still sees its original protocol response.
		if own := parseBodyMap(t, a.s.Get("/api/agents/"+a.id()+"/upgrade", a.agentAuth())); !testjson.Equal(own, original) {
			t.Fatalf("own %v", own)
		}
	}
}

func TestRejectedUpdateResponseIsChineseButPersistedErrorIsOriginal(t *testing.T) {
	a := newUpgradeApp(t)
	raw := "another Agent update or service operation is active"
	a.respond(func(method, path string, _ map[string]any) *agents.Response {
		if path == "/api/upgrade-info" {
			return upgradeAgentResponse(map[string]any{"agent_id": a.id(), "deployment_method": "shell", "architecture": "linux-amd64"}, 200)
		}
		return upgradeAgentResponse(map[string]any{"success": false, "message": raw}, 409)
	})
	result := parseBodyMap(t, a.s.Post("/api/agents/"+a.id()+"/update", a.admin, httpxtest.JSON(map[string]any{})).WantStatus(409))
	if result["status"] != "failed" || result["error"] != agents.UpgradeErrorMessage(raw) ||
		mapObj(a.stored(a.id()), "latest_upgrade")["error"] != raw {
		t.Fatalf("result %v", result)
	}
}

func TestLiveRollbackFailureIsLocalizedWithoutChangingResult(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	state["protocol"] = 2
	a.setUpgrade(state)
	raw := "Agent did not confirm expected version, identity and migration readiness; rollback: open /opt/agent/backup: no such file or directory"
	a.respond(func(string, string, map[string]any) *agents.Response {
		return upgradeAgentResponse(map[string]any{"update_id": state["update_id"], "agent_id": a.id(), "status": "rollback_failed", "error": raw}, 200)
	})
	result := parseBodyMap(t, a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(200))
	if result["status"] != "rollback_failed" || !strings.Contains(mapStr(result, "error"), "未能确认 Agent 的版本") ||
		!strings.Contains(mapStr(result, "error"), "恢复旧版本失败") || mapObj(a.stored(a.id()), "latest_upgrade")["error"] != raw {
		t.Fatalf("result %v", result)
	}
}

func TestLegacyMigrationFailureIsLocalizedWhenViewedByAdmin(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	raw := "automatic migration failed: systemctl operation failed: failed to start"
	a.respond(func(string, string, map[string]any) *agents.Response {
		return upgradeAgentResponse(map[string]any{}, 404)
	})
	path := "/api/agents/" + a.id() + "/upgrade"
	a.s.Post(path+"/report", a.agentAuth(), httpxtest.JSON(map[string]any{"update_id": state["update_id"], "status": "rolling_back", "error": raw})).WantStatus(200)
	viewed := parseBodyMap(t, a.s.Get(path, a.admin))
	if viewed["status"] != "rolling_back" || !strings.Contains(mapStr(viewed, "error"), "自动迁移失败") ||
		!strings.Contains(mapStr(viewed, "error"), "服务管理命令执行失败") || mapObj(a.stored(a.id()), "latest_upgrade")["error"] != raw {
		t.Fatalf("viewed %v", viewed)
	}
}

func TestMissingAgentAndUpdateRecordErrorsAreChinese(t *testing.T) {
	a := newUpgradeApp(t)
	wantBodyJSON(t, a.s.Post("/api/agents/missing/update", a.admin, httpxtest.JSON(map[string]any{})).WantStatus(409),
		map[string]any{"success": false, "message": agents.UpgradeErrorMessage("Agent not found")})
	wantBodyJSON(t, a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(404),
		map[string]any{"success": false, "message": agents.UpgradeErrorMessage("No upgrade recorded")})
}

func TestLatestVersionRoute(t *testing.T) {
	a := newUpgradeApp(t)
	wantBodyJSON(t, a.s.Get("/api/agents/latest-version", a.admin).WantStatus(200),
		map[string]any{"success": true, "version": agents.GetLatestVersion()})
	a.s.Get("/api/agents/latest-version").WantStatus(401)
}

type fakeOpClient struct {
	fn func(context.Context, agents.Request) (*agents.Response, error)
}

func (f fakeOpClient) Do(ctx context.Context, req agents.Request) (*agents.Response, error) {
	return f.fn(ctx, req)
}

func TestMissingAgentOperationRoutesReturn404(t *testing.T) {
	// Approved Python bug fix (O13 #2): all eight operation routes return 404
	// instead of 500 when the Agent ID does not exist, keeping the same body.
	tests := []struct {
		name   string
		method string
		path   string
		body   map[string]any
	}{
		{"logs", http.MethodGet, "/api/agents/missing/logs", nil},
		{"log sources", http.MethodGet, "/api/agents/missing/logs/sources", nil},
		{"log clear", http.MethodPost, "/api/agents/missing/logs/clear", map[string]any{"log_path": "/var/log/x.log"}},
		{"log validate", http.MethodPost, "/api/agents/missing/logs/validate", map[string]any{"path": "/var/log/x.log"}},
		{"logging config get", http.MethodGet, "/api/agents/missing/config/logging", nil},
		{"logging config set", http.MethodPost, "/api/agents/missing/config/logging", map[string]any{"enabled": true}},
		{"restart", http.MethodPost, "/api/agents/missing/restart", nil},
		{"uninstall", http.MethodPost, "/api/agents/missing/uninstall", nil},
	}
	a := newUpgradeApp(t)
	a.m.SetClient(fakeOpClient{fn: func(_ context.Context, req agents.Request) (*agents.Response, error) {
		t.Fatalf("unexpected Agent call for missing agent: %s %s", req.Method, req.URL)
		return nil, nil
	}})
	wantMissing := map[string]any{"success": false, "message": "Agent not found"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := []httpxtest.Option{a.admin}
			if tc.body != nil {
				opts = append(opts, httpxtest.JSON(tc.body))
			}
			wantBodyJSON(t, a.s.Do(tc.method, tc.path, opts...).WantStatus(404), wantMissing)
		})
	}

	// Input validation before Agent lookup retains its 400 status.
	wantEmptyPath := map[string]any{"success": false, "message": "日志路径不能为空"}
	wantBodyJSON(t, a.s.Post("/api/agents/missing/logs/clear", a.admin, httpxtest.JSON(map[string]any{})).WantStatus(400), wantEmptyPath)
	wantBodyJSON(t, a.s.Post("/api/agents/missing/logs/validate", a.admin, httpxtest.JSON(map[string]any{"path": ""})).WantStatus(400), wantEmptyPath)

	// Negative control: an existing Agent whose reply happens to contain the
	// same error message still returns 500, not 404.
	a.m.SetClient(fakeOpClient{fn: func(_ context.Context, _ agents.Request) (*agents.Response, error) {
		return &agents.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       []byte(`{"success":false,"message":"Agent not found"}`),
		}, nil
	}})
	wantBodyJSON(t, a.s.Get("/api/agents/"+a.id()+"/logs", a.admin).WantStatus(500), wantMissing)
	wantBodyJSON(t, a.s.Post("/api/agents/"+a.id()+"/restart", a.admin).WantStatus(500), wantMissing)
}
