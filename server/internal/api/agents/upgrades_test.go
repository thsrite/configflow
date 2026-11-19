package agents_test

// Ports of backend/test_agent_upgrade_auth.py (upgrade authentication
// through the application's request hooks) and the route cases of
// backend/test_agent_upgrade_messages.py. Deferred: the binary download
// cases (GET /api/agents/download/<filename>, A10), the GET /api/agents and
// GET /api/agents/<id> parts (A13) and push-config (A14).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/upgrades"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

type upgradeApp struct {
	t      *testing.T
	s      *httpxtest.Server
	m      *agents.Manager
	agent  *jsonx.Object
	other  *jsonx.Object
	admin  httpxtest.Option
	static string
}

// newUpgradeApp is the authenticated_app fixture.
func newUpgradeApp(t *testing.T) *upgradeApp {
	t.Helper()
	agents.ResetRuntime()
	agents.ResetHooks()
	t.Cleanup(func() { agents.ResetRuntime(); agents.ResetHooks() })
	t.Setenv("AGENT_REGISTRATION_KEY", "")
	os.Unsetenv("AGENT_REGISTRATION_KEY")
	static := t.TempDir()
	for _, arch := range upgrades.Arches {
		os.WriteFile(filepath.Join(static, "configflow-agent-"+arch), []byte("\x7fELF"+arch), 0o644)
	}
	t.Setenv("AGENTS_STATIC_DIR", static)
	upgrades.ResetArtifactCache()
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "upgrade-admin", Password: "upgrade-test-password",
		DefaultFactories: true, Register: httpxtest.Routes(apiagents.Register)})
	upgrades.Wire()
	app := &upgradeApp{t: t, s: s, static: static}
	app.admin = s.Login()
	m, err := agents.Get(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	app.m = m
	for _, name := range []string{"upgrading", "other-agent"} {
		agent, err := m.RegisterAgent(s.Context(), jsonx.Obj("name", name, "host", "127.0.0.1", "port", 1234,
			"service_type", "mihomo", "deployment_method", "shell", "version", "1.1.0-go"), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if app.agent == nil {
			app.agent = agent
		} else {
			app.other = agent
		}
	}
	t.Cleanup(legacyUpgradeSetRequestForTest(func(*jsonx.Object, string, string, *jsonx.Object) (*agents.Response, error) {
		t.Fatal("unexpected Agent call")
		return nil, nil
	}))
	return app
}

func (a *upgradeApp) id() string { return a.agent.GetString("id") }

func (a *upgradeApp) setUpgrade(state *jsonx.Object) {
	a.t.Helper()
	_, err := a.m.UpdateAgents(a.s.Context(), "", func(_ context.Context, list *[]any) (any, error) {
		for _, item := range *list {
			if agent := item.(*jsonx.Object); agent.Value("id") == a.agent.Value("id") {
				agent.Set("latest_upgrade", state.DeepCopy())
			}
		}
		return nil, nil
	})
	if err != nil {
		a.t.Fatal(err)
	}
}

// seedUpgrade is seed_upgrade(manager, agent): a protocol 1 update downloading.
func (a *upgradeApp) seedUpgrade() *jsonx.Object {
	now := agents.NowTimestamp()
	state := jsonx.Obj("update_id", strings.Repeat("a", 32), "agent_id", a.id(), "protocol", 1,
		"target_version", agents.GetLatestVersion(), "previous_version", "1.1.0-go",
		"status", "downloading", "started_at", jsonx.Float(now), "updated_at", jsonx.Float(now))
	a.setUpgrade(state)
	return state
}

func (a *upgradeApp) stored(agentID string) *jsonx.Object {
	a.t.Helper()
	agent, err := a.m.GetAgentByID(a.s.Context(), agentID, true)
	if err != nil {
		a.t.Fatal(err)
	}
	return agent
}

func (a *upgradeApp) system() *jsonx.Object { return system(a.t, a.s) }

func (a *upgradeApp) agentAuth() httpxtest.Option {
	return httpxtest.Bearer(a.agent.GetString("token"))
}

func (a *upgradeApp) respond(fn func(method, path string, payload *jsonx.Object) *agents.Response) {
	a.t.Cleanup(legacyUpgradeSetRequestForTest(func(_ *jsonx.Object, method, path string, payload *jsonx.Object) (*agents.Response, error) {
		return fn(method, path, payload), nil
	}))
}

func TestLegacyAgentCanReadAndReportOwnUpgradeThenAdminConfirmsRollback(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	path := "/api/agents/" + a.id() + "/upgrade"
	// A bootstrap read must not recursively poll the not-yet-started Agent.
	if got := a.s.Get(path, a.agentAuth()).WantStatus(200).Object(); got.Value("update_id") != state.Value("update_id") {
		t.Fatalf("got %v", got)
	}
	got := a.s.Post(path+"/report", a.agentAuth(), httpxtest.JSON(jsonx.Obj("update_id", state.Value("update_id"),
		"status", "rolling_back", "error", "migration failed"))).WantStatus(200).Object()
	if got.Value("status") != "rolling_back" || a.stored(a.other.GetString("id")).Has("latest_upgrade") {
		t.Fatalf("got %v", got)
	}
	a.s.Post("/api/agents/"+a.id()+"/heartbeat", a.agentAuth(), httpxtest.JSON(jsonx.Obj("version", "1.1.0-go"))).WantStatus(200)
	a.respond(func(string, string, *jsonx.Object) *agents.Response {
		return legacyUpgradeAgentResponse(jsonx.NewObject(), 404)
	})
	r := a.s.Get(path, a.admin).WantStatus(200)
	if r.Object().Value("status") != "rolled_back" || r.Contains(a.agent.GetString("token")) || r.Contains(a.other.GetString("token")) {
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
				options = append(options, httpxtest.Bearer(a.other.GetString("token")))
			case "malformed":
				options = append(options, httpxtest.Bearer(a.agent.GetString("token")+" extra"))
			}
			path := "/api/agents/" + a.id() + "/upgrade"
			if operation == "read" {
				a.s.Get(path, options...).WantStatus(401)
			} else {
				options = append(options, httpxtest.JSON(jsonx.Obj("update_id", state.Value("update_id"), "status", "rolling_back")))
				a.s.Post(path+"/report", options...).WantStatus(401)
			}
			if !jsonx.Equal(a.system(), before) {
				t.Fatalf("%s/%s mutated", credential, operation)
			}
		}
	}
}

func TestAdminJWTCannotImpersonateAgentRollbackReport(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	a.s.Post("/api/agents/"+a.id()+"/upgrade/report", a.admin,
		httpxtest.JSON(jsonx.Obj("update_id", state.Value("update_id"), "status", "rolling_back"))).WantStatus(401)
	if !jsonx.Equal(a.stored(a.id()).Value("latest_upgrade"), state) {
		t.Fatal("changed")
	}
}

func TestAuthenticatedReportStillRequiresMatchingLegacyTask(t *testing.T) {
	for _, payload := range []*jsonx.Object{
		jsonx.Obj("update_id", "wrong-task", "status", "rolling_back"),
		jsonx.Obj("update_id", strings.Repeat("a", 32), "status", "succeeded"),
	} {
		a := newUpgradeApp(t)
		state := a.seedUpgrade()
		a.s.Post("/api/agents/"+a.id()+"/upgrade/report", a.agentAuth(), httpxtest.JSON(payload)).WantStatus(400).
			WantJSON(jsonx.Obj("success", false, "message", "Agent 上报的更新结果格式无效。"))
		if !jsonx.Equal(a.stored(a.id()).Value("latest_upgrade"), state) {
			t.Fatal("changed")
		}
	}
}

func TestReportBodyLimitAndMalformedReports(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	path := "/api/agents/" + a.id() + "/upgrade/report"
	big := jsonx.Obj("update_id", state.Value("update_id"), "status", "rolling_back", "error", strings.Repeat("x", apiagents.ReportBodyLimit))
	a.s.Post(path, a.agentAuth(), httpxtest.JSON(big)).WantStatus(413).WantJSON(jsonx.Obj("success", false))
	for _, body := range []string{`null`, `[1]`, `"x"`, `5`} {
		a.s.Post(path, a.agentAuth(), httpxtest.Body([]byte(body), "application/json")).WantStatus(400)
	}
	a.s.Post(path, a.agentAuth(), httpxtest.Body([]byte(`{`), "application/json")).WantStatus(400)
	a.s.Post(path, a.agentAuth(), httpxtest.Body([]byte(`{}`), "text/plain")).WantStatus(415)
	if !jsonx.Equal(a.stored(a.id()).Value("latest_upgrade"), state) {
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
			if !jsonx.Equal(a.system(), before) {
				t.Fatalf("%s %s mutated", credential, c[1])
			}
		}
	}
}

func TestAdminCanStillReadUpgradeStatus(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	state.Set("status", "failed")
	a.setUpgrade(state)
	got := a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(200).Object()
	if got.Value("update_id") != state.Value("update_id") || got.Value("status") != "failed" {
		t.Fatalf("got %v", got)
	}
}

func TestAdminStartsUpgradeForBothAgentProtocols(t *testing.T) {
	for _, protocol := range []int{1, 2} {
		a := newUpgradeApp(t)
		var submitted []*jsonx.Object
		a.respond(func(method, path string, payload *jsonx.Object) *agents.Response {
			if method == "GET" && path == "/api/upgrade-info" {
				status := 200
				if protocol == 1 {
					status = 404
				}
				return legacyUpgradeAgentResponse(jsonx.Obj("agent_id", a.id(), "deployment_method", "shell", "architecture", "linux-amd64"), status)
			}
			if method != "POST" || path != "/api/update" {
				t.Fatalf("%s %s", method, path)
			}
			submitted = append(submitted, payload)
			if protocol == 2 {
				return legacyUpgradeAgentResponse(jsonx.NewObject(), 202)
			}
			return legacyUpgradeAgentResponse(jsonx.NewObject(), 200)
		})
		state := a.s.Post("/api/agents/"+a.id()+"/update", a.admin, httpxtest.JSON(jsonx.NewObject())).WantStatus(202).Object()
		if state.Value("status") != "queued" || !jsonx.Equal(state.Value("protocol"), protocol) ||
			submitted[0].Value("update_id") != state.Value("update_id") || submitted[0].Value("version") != agents.GetLatestVersion() {
			t.Fatalf("state %v submitted %v", state, submitted)
		}
		if protocol == 2 && submitted[0].Value("sha256") != state.GetObject("artifacts").GetObject("linux-amd64").Value("sha256") {
			t.Fatal("sha256")
		}
	}
}

func TestReregisterPreservesUpgradeRecordAndPendingOperationLock(t *testing.T) {
	for _, status := range []string{"checking", "unknown", "rollback_failed", "succeeded"} {
		a := newUpgradeApp(t)
		state := a.seedUpgrade()
		state.Set("status", status)
		a.setUpgrade(state)
		got := a.s.Post("/api/agents/register", a.agentAuth(), httpxtest.JSON(jsonx.Obj("name", "upgrading", "host", "127.0.0.1",
			"port", 1234, "service_type", "mihomo", "deployment_method", "shell", "version", agents.GetLatestVersion()))).WantStatus(200).Object()
		if got.Value("id") != a.agent.Value("id") {
			t.Fatalf("got %v", got)
		}
		registered := a.stored(a.id())
		if !jsonx.Equal(registered.Value("latest_upgrade"), state) || registered.Value("token") != a.agent.Value("token") ||
			registered.Value("version") != agents.GetLatestVersion() {
			t.Fatalf("%s: registered %v", status, registered)
		}
		if status != "succeeded" {
			// Re-registration cannot unlock a pending/unknown/failed-rollback task.
			a.s.Delete("/api/agents/"+a.id(), a.admin).WantStatus(409)
			if !jsonx.Equal(a.stored(a.id()).Value("latest_upgrade"), state) {
				t.Fatal("changed")
			}
		}
	}
}

func TestReregisteredNewAgentCanConfirmPreservedUpgrade(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	a.s.Post("/api/agents/register", a.agentAuth(), httpxtest.JSON(jsonx.Obj("name", "upgrading", "host", "127.0.0.1",
		"port", 1234, "service_type", "mihomo", "deployment_method", "shell", "version", agents.GetLatestVersion()))).WantStatus(200)
	a.respond(func(string, string, *jsonx.Object) *agents.Response {
		return legacyUpgradeAgentResponse(jsonx.Obj("agent_id", a.id(), "version", state.Value("target_version"), "migration_ready", true), 200)
	})
	got := a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(200).Object()
	if got.Value("status") != "succeeded" || got.Value("update_id") != state.Value("update_id") ||
		a.stored(a.id()).GetObject("latest_upgrade").Value("status") != "succeeded" {
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
		state.Set("status", status)
		state.Set("error", raw)
		a.setUpgrade(state)
		original := a.stored(a.id()).GetObject("latest_upgrade").DeepCopy()
		a.respond(func(string, string, *jsonx.Object) *agents.Response {
			return legacyUpgradeAgentResponse(jsonx.NewObject(), 404)
		})
		progress := a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(200).Object()
		if progress.Value("error") != agents.UpgradeErrorMessage(raw) || progress.Value("status") != status ||
			progress.Value("update_id") != original.Value("update_id") {
			t.Fatalf("progress %v", progress)
		}
		if !jsonx.Equal(a.stored(a.id()).Value("latest_upgrade"), original) {
			t.Fatal("rewritten")
		}
		// The old Agent's bootstrap still sees its original protocol response.
		if own := a.s.Get("/api/agents/"+a.id()+"/upgrade", a.agentAuth()).Object(); !jsonx.Equal(own, original) {
			t.Fatalf("own %v", own)
		}
	}
}

func TestRejectedUpdateResponseIsChineseButPersistedErrorIsOriginal(t *testing.T) {
	a := newUpgradeApp(t)
	raw := "another Agent update or service operation is active"
	a.respond(func(method, path string, _ *jsonx.Object) *agents.Response {
		if path == "/api/upgrade-info" {
			return legacyUpgradeAgentResponse(jsonx.Obj("agent_id", a.id(), "deployment_method", "shell", "architecture", "linux-amd64"), 200)
		}
		return legacyUpgradeAgentResponse(jsonx.Obj("success", false, "message", raw), 409)
	})
	result := a.s.Post("/api/agents/"+a.id()+"/update", a.admin, httpxtest.JSON(jsonx.NewObject())).WantStatus(409).Object()
	if result.Value("status") != "failed" || result.Value("error") != agents.UpgradeErrorMessage(raw) ||
		a.stored(a.id()).GetObject("latest_upgrade").Value("error") != raw {
		t.Fatalf("result %v", result)
	}
}

func TestLiveRollbackFailureIsLocalizedWithoutChangingResult(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	state.Set("protocol", 2)
	a.setUpgrade(state)
	raw := "Agent did not confirm expected version, identity and migration readiness; rollback: open /opt/agent/backup: no such file or directory"
	a.respond(func(string, string, *jsonx.Object) *agents.Response {
		return legacyUpgradeAgentResponse(jsonx.Obj("update_id", state.Value("update_id"), "agent_id", a.id(), "status", "rollback_failed", "error", raw), 200)
	})
	result := a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(200).Object()
	if result.Value("status") != "rollback_failed" || !strings.Contains(result.GetString("error"), "未能确认 Agent 的版本") ||
		!strings.Contains(result.GetString("error"), "恢复旧版本失败") || a.stored(a.id()).GetObject("latest_upgrade").Value("error") != raw {
		t.Fatalf("result %v", result)
	}
}

func TestLegacyMigrationFailureIsLocalizedWhenViewedByAdmin(t *testing.T) {
	a := newUpgradeApp(t)
	state := a.seedUpgrade()
	raw := "automatic migration failed: systemctl operation failed: failed to start"
	a.respond(func(string, string, *jsonx.Object) *agents.Response {
		return legacyUpgradeAgentResponse(jsonx.NewObject(), 404)
	})
	path := "/api/agents/" + a.id() + "/upgrade"
	a.s.Post(path+"/report", a.agentAuth(), httpxtest.JSON(jsonx.Obj("update_id", state.Value("update_id"), "status", "rolling_back", "error", raw))).WantStatus(200)
	viewed := a.s.Get(path, a.admin).Object()
	if viewed.Value("status") != "rolling_back" || !strings.Contains(viewed.GetString("error"), "自动迁移失败") ||
		!strings.Contains(viewed.GetString("error"), "服务管理命令执行失败") || a.stored(a.id()).GetObject("latest_upgrade").Value("error") != raw {
		t.Fatalf("viewed %v", viewed)
	}
}

func TestMissingAgentAndUpdateRecordErrorsAreChinese(t *testing.T) {
	a := newUpgradeApp(t)
	a.s.Post("/api/agents/missing/update", a.admin, httpxtest.JSON(jsonx.NewObject())).WantStatus(409).
		WantJSON(jsonx.Obj("success", false, "message", agents.UpgradeErrorMessage("Agent not found")))
	a.s.Get("/api/agents/"+a.id()+"/upgrade", a.admin).WantStatus(404).
		WantJSON(jsonx.Obj("success", false, "message", agents.UpgradeErrorMessage("No upgrade recorded")))
}

func TestLatestVersionRoute(t *testing.T) {
	a := newUpgradeApp(t)
	a.s.Get("/api/agents/latest-version", a.admin).WantStatus(200).WantJSON(jsonx.Obj("success", true, "version", agents.GetLatestVersion()))
	a.s.Get("/api/agents/latest-version").WantStatus(401)
}
