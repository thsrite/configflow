package upgrades

// Port of backend/test_agent_upgrades.py: upgrade persistence,
// acceptance/health distinction and failure concurrency.

import (
	"configflow/internal/model"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

type upgradeEnv struct {
	t     *testing.T
	ctx   context.Context
	repo  *repo.Repository
	m     *agents.Manager
	ident string
}

func newUpgradeEnv(t *testing.T) *upgradeEnv {
	t.Helper()
	agents.ResetRuntime()
	agents.ResetHooks()
	t.Cleanup(func() { agents.ResetRuntime(); agents.ResetHooks() })
	r, err := repo.Open(filepath.Join(t.TempDir(), "config"), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	m, err := agents.New(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := m.RegisterAgent(ctx, jsonx.Obj("name", "upgrade", "host", "localhost", "port", 1234,
		"service_type", "mihomo", "deployment_method", "shell", "version", "1.1.0-go"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(setLegacyArtifactsForTest(func() (*jsonx.Object, error) {
		out := jsonx.NewObject()
		for _, arch := range Arches {
			out.Set(arch, jsonx.Obj("sha256", strings.Repeat("a", 64), "size", 100))
		}
		return out, nil
	}))
	t.Cleanup(func() { requestHook = nil; requestOverride = nil })
	return &upgradeEnv{t: t, ctx: ctx, repo: r, m: m, ident: agent.GetString("id")}
}

// update is manager._update_agents(lambda agents: agents[0].update(**fields)).
func (e *upgradeEnv) update(pairs ...any) {
	e.t.Helper()
	_, err := e.m.UpdateAgents(e.ctx, "", func(_ context.Context, list *[]any) (any, error) {
		(*list)[0].(*jsonx.Object).Update(jsonx.Obj(pairs...))
		return nil, nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *upgradeEnv) agent() *jsonx.Object {
	e.t.Helper()
	agent, err := e.m.GetAgentByID(e.ctx, e.ident, true)
	if err != nil {
		e.t.Fatal(err)
	}
	return agent
}

func (e *upgradeEnv) restarted() *agents.Manager {
	e.t.Helper()
	m, err := agents.New(e.ctx, e.repo)
	if err != nil {
		e.t.Fatal(err)
	}
	return m
}

func (e *upgradeEnv) heartbeat(version string) {
	e.t.Helper()
	if ok, err := e.m.ObserveHeartbeat(e.ctx, e.ident, agentModelForTest(e.t, jsonx.Obj("version", version))); !ok || err != nil {
		e.t.Fatalf("heartbeat %v %v", ok, err)
	}
}

func (e *upgradeEnv) poll(m *agents.Manager) *jsonx.Object {
	e.t.Helper()
	if m == nil {
		m = e.m
	}
	state, err := Poll(e.ctx, m, e.ident)
	if err != nil {
		e.t.Fatal(err)
	}
	return state
}

func respond(payload any, status int) *agents.Response { return AgentResponse(payload, status) }

func wantValueError(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil || !IsValueError(err) || !strings.Contains(err.Error(), fragment) {
		t.Fatalf("want ValueError %q, got %v", fragment, err)
	}
}

func upgradeState(pairs ...any) *jsonx.Object {
	state := jsonx.Obj("update_id", "job", "target_version", agents.GetLatestVersion(), "previous_version", "1.1.0-go")
	state.Update(jsonx.Obj(pairs...))
	return state
}

func statuses(events any) string {
	var out []string
	for _, event := range events.([]any) {
		out = append(out, event.(*jsonx.Object).GetString("status"))
	}
	return strings.Join(out, ",")
}

func TestAcceptanceIsNotSuccessAndReservesAcrossRestart(t *testing.T) {
	e := newUpgradeEnv(t)
	info := jsonx.Obj("agent_id", e.ident, "deployment_method", "shell", "architecture", "linux-amd64", "version", "1.1.0-go")
	requestHook = func(_ *jsonx.Object, _, path string, _ *jsonx.Object) (*agents.Response, error) {
		if strings.HasSuffix(path, "info") {
			return respond(info, 200), nil
		}
		return respond(jsonx.NewObject(), 200), nil
	}
	state, err := Start(e.ctx, e.m, e.ident)
	if err != nil {
		t.Fatal(err)
	}
	if state.Value("status") != "queued" { // Acceptance is not an observed worker stage.
		t.Fatalf("status %v", state.Value("status"))
	}
	restarted := e.restarted()
	_, err = Start(e.ctx, restarted, e.ident)
	wantValueError(t, err, "已有更新")
	agent, _ := restarted.GetAgentByID(e.ctx, e.ident, true)
	if agent.GetObject("latest_upgrade").Value("update_id") != state.Value("update_id") {
		t.Fatal("reservation lost")
	}
}

func TestWorkerSuccessStillRequiresVersionIdentityAndMigration(t *testing.T) {
	e := newUpgradeEnv(t)
	e.update("latest_upgrade", upgradeState("status", "checking", "protocol", 2, "started_at", jsonx.Float(now())))
	info := jsonx.Obj("agent_id", e.ident, "version", agents.GetLatestVersion(), "migration_ready", false)
	requestHook = func(_ *jsonx.Object, _, path string, _ *jsonx.Object) (*agents.Response, error) {
		if strings.HasSuffix(path, "info") {
			return respond(info, 200), nil
		}
		return respond(jsonx.Obj("update_id", "job", "agent_id", e.ident, "status", "succeeded"), 200), nil
	}
	if got := e.poll(nil).Value("status"); got != "checking" {
		t.Fatalf("got %v", got)
	}
	info.Set("migration_ready", true)
	info.Set("agent_id", "wrong-agent")
	if got := e.poll(nil).Value("status"); got != "checking" {
		t.Fatalf("got %v", got)
	}
	info.Set("agent_id", e.ident)
	if got := e.poll(nil).Value("status"); got != "succeeded" {
		t.Fatalf("got %v", got)
	}
}

func TestLegacyRollbackRequiresFreshAuthenticatedHeartbeat(t *testing.T) {
	e := newUpgradeEnv(t)
	e.update("latest_upgrade", upgradeState("status", "downloading", "protocol", 1, "started_at", jsonx.Float(now())))
	requestHook = func(_ *jsonx.Object, _, path string, _ *jsonx.Object) (*agents.Response, error) {
		if path == "/api/update" {
			t.Fatal("legacy GET endpoint is unsupported")
		}
		return respond(jsonx.NewObject(), 404), nil
	}
	reported, err := ReportLegacy(e.ctx, e.m, e.ident, jsonx.Obj("update_id", "job", "status", "rolling_back"))
	if err != nil || reported.Value("status") != "rolling_back" {
		t.Fatalf("%v %v", reported, err)
	}
	if got := e.poll(nil).Value("status"); got != "rolling_back" {
		t.Fatalf("got %v", got)
	}
	time.Sleep(2 * time.Millisecond)
	e.heartbeat("1.1.0-go")
	if got := e.poll(nil).Value("status"); got != "rolled_back" {
		t.Fatalf("got %v", got)
	}
}

// naiveISO is datetime.fromtimestamp(ts).isoformat().
func naiveISO(ts float64) string {
	at := time.Unix(0, int64(ts*1e9)).Local()
	text := at.Format("2006-01-02T15:04:05")
	if micro := at.Nanosecond() / 1000; micro != 0 {
		text += fmt.Sprintf(".%06d", micro)
	}
	return text
}

func TestUnknownTimeoutBlocksRetryAndDoesNotClaimRollback(t *testing.T) {
	e := newUpgradeEnv(t)
	started := now() - 601
	e.update("latest_upgrade", upgradeState("status", "checking", "protocol", 2, "started_at", jsonx.Float(started)),
		"last_heartbeat", naiveISO(started))
	requestHook = func(*jsonx.Object, string, string, *jsonx.Object) (*agents.Response, error) {
		return respond(jsonx.NewObject(), 404), nil
	}
	if got := e.poll(nil).Value("status"); got != "unknown" {
		t.Fatalf("got %v", got)
	}
	_, err := Start(e.ctx, e.m, e.ident)
	wantValueError(t, err, "已有更新")
}

func TestOldVersionHeartbeatAfterTimeoutFailsAndAllowsRetry(t *testing.T) {
	e := newUpgradeEnv(t)
	e.update("latest_upgrade", upgradeState("status", "unknown", "protocol", 2, "started_at", jsonx.Float(now()-601)))
	e.heartbeat("1.1.0-go")
	info := jsonx.Obj("agent_id", e.ident, "deployment_method", "shell", "architecture", "linux-amd64", "version", "1.1.0-go")
	requestHook = func(_ *jsonx.Object, method, path string, _ *jsonx.Object) (*agents.Response, error) {
		if strings.HasSuffix(path, "info") {
			return respond(info, 200), nil
		}
		if method == "GET" && path == "/api/update" {
			return respond(jsonx.NewObject(), 404), nil
		}
		return respond(jsonx.NewObject(), 200), nil
	}
	result := e.poll(nil)
	if result.Value("status") != "failed" || !strings.Contains(result.GetString("error"), "1.1.0-go") {
		t.Fatalf("result %v", result)
	}
	state, err := Start(e.ctx, e.m, e.ident)
	if err != nil || state.Value("status") != "queued" {
		t.Fatalf("%v %v", state, err)
	}
}

func TestDockerIsNotSentBinaryUpdate(t *testing.T) {
	e := newUpgradeEnv(t)
	e.update("deployment_method", "docker")
	requestHook = func(*jsonx.Object, string, string, *jsonx.Object) (*agents.Response, error) {
		t.Fatal("contacted Docker binary updater")
		return nil, nil
	}
	_, err := Start(e.ctx, e.m, e.ident)
	wantValueError(t, err, "Docker")
}

func TestFailedRollbackCanConfirmLaterRecovery(t *testing.T) {
	e := newUpgradeEnv(t)
	e.update("latest_upgrade", upgradeState("status", "rollback_failed", "protocol", 2, "started_at", jsonx.Float(now())))
	requestHook = func(*jsonx.Object, string, string, *jsonx.Object) (*agents.Response, error) {
		return respond(jsonx.Obj("update_id", "job", "agent_id", e.ident, "status", "rolled_back"), 200), nil
	}
	if got := e.poll(nil).Value("status"); got != "rolled_back" {
		t.Fatalf("got %v", got)
	}
}

func TestWorkerEventsSurviveRestartAndRepeatedPollsWithoutDuplicates(t *testing.T) {
	e := newUpgradeEnv(t)
	e.update("latest_upgrade", upgradeState("status", "queued", "protocol", 2, "started_at", jsonx.Float(now())))
	payload := jsonx.Obj("update_id", "job", "agent_id", e.ident, "status", "verifying",
		"message", "检查 Agent 候选程序与版本", "updated_at", "2026-10-18T00:00:02Z",
		"events", []any{jsonx.Obj("at", "2026-10-18T00:00:00Z", "status", "queued"),
			jsonx.Obj("at", "2026-10-18T00:00:01Z", "status", "downloading", "message", "下载 configflow-agent-linux-amd64"),
			jsonx.Obj("at", "2026-10-18T00:00:02Z", "status", "verifying", "message", "检查 Agent 候选程序与版本")})
	requestHook = func(*jsonx.Object, string, string, *jsonx.Object) (*agents.Response, error) {
		return respond(payload, 200), nil
	}
	first := e.poll(nil)
	second := e.poll(e.restarted())
	if !jsonx.Equal(first.Value("events"), second.Value("events")) {
		t.Fatalf("events differ:\n%s\n%s", jsonx.Repr(first.Value("events")), jsonx.Repr(second.Value("events")))
	}
	if got := statuses(first.Value("events")); got != "queued,downloading,verifying" {
		t.Fatalf("got %s", got)
	}
	if second.Value("message") != payload.Value("message") {
		t.Fatalf("message %v", second.Value("message"))
	}
}

func TestUnconfirmedWorkerSuccessDoesNotExposeSuccessEvent(t *testing.T) {
	e := newUpgradeEnv(t)
	e.update("latest_upgrade", upgradeState("status", "checking", "protocol", 2, "started_at", jsonx.Float(now())))
	worker := jsonx.Obj("update_id", "job", "agent_id", e.ident, "status", "succeeded", "updated_at", "2026-10-18T00:00:01Z",
		"events", []any{jsonx.Obj("at", "2026-10-18T00:00:00Z", "status", "checking"),
			jsonx.Obj("at", "2026-10-18T00:00:01Z", "status", "succeeded")})
	info := jsonx.Obj("agent_id", e.ident, "version", agents.GetLatestVersion(), "migration_ready", false)
	requestHook = func(_ *jsonx.Object, _, path string, _ *jsonx.Object) (*agents.Response, error) {
		if strings.HasSuffix(path, "info") {
			return respond(info, 200), nil
		}
		return respond(worker, 200), nil
	}
	result := e.poll(nil)
	if result.Value("status") != "checking" || strings.Contains(statuses(result.Value("events")), "succeeded") {
		t.Fatalf("result %s", jsonx.Repr(result))
	}
	info.Set("migration_ready", true)
	events := e.poll(nil).GetList("events")
	if events[len(events)-1].(*jsonx.Object).Value("status") != "succeeded" {
		t.Fatalf("events %s", jsonx.Repr(events))
	}
}

func TestLateStartAcceptanceKeepsStageAlreadyObservedByPoll(t *testing.T) {
	e := newUpgradeEnv(t)
	info := jsonx.Obj("agent_id", e.ident, "deployment_method", "shell", "architecture", "linux-amd64")
	requestHook = func(_ *jsonx.Object, _, path string, payload *jsonx.Object) (*agents.Response, error) {
		if strings.HasSuffix(path, "info") {
			return respond(info, 200), nil
		}
		if _, err := save(e.ctx, e.m, e.ident, payload.Value("update_id"), jsonx.Obj("status", "migrating", "message", "正在迁移 Agent 配置"), false); err != nil {
			t.Fatal(err)
		}
		return respond(jsonx.NewObject(), 200), nil
	}
	result, err := Start(e.ctx, e.m, e.ident)
	if err != nil {
		t.Fatal(err)
	}
	events := result.GetList("events")
	if result.Value("status") != "migrating" || events[len(events)-1].(*jsonx.Object).Value("status") != "migrating" {
		t.Fatalf("result %s", jsonx.Repr(result))
	}
}

func TestChangedUpgradeDuringPreflightPreventsASecondSubmission(t *testing.T) {
	e := newUpgradeEnv(t)
	requestHook = func(_ *jsonx.Object, method, path string, _ *jsonx.Object) (*agents.Response, error) {
		if method != "GET" || !strings.HasSuffix(path, "info") {
			t.Fatal("late request must not submit a second update")
		}
		e.update("latest_upgrade", jsonx.Obj("update_id", "other-completed", "status", "succeeded"))
		return respond(jsonx.Obj("agent_id", e.ident, "deployment_method", "shell", "architecture", "linux-amd64"), 200), nil
	}
	_, err := Start(e.ctx, e.m, e.ident)
	wantValueError(t, err, "更新任务已改变")
	if got := e.agent().GetObject("latest_upgrade").Value("update_id"); got != "other-completed" {
		t.Fatalf("got %v", got)
	}
}

func TestStaleWorkerResponseDoesNotUndoNewerStateAndOldWorkerHasObservedEvents(t *testing.T) {
	e := newUpgradeEnv(t)
	e.update("latest_upgrade", jsonx.Obj("update_id", "job", "status", "queued", "protocol", 2, "started_at", jsonx.Float(now())))
	current, err := save(e.ctx, e.m, e.ident, "job", jsonx.Obj("status", "checking", "agent_updated_at", "2026-10-18T00:00:00.1Z"), false)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := save(e.ctx, e.m, e.ident, "job", jsonx.Obj("status", "downloading", "agent_updated_at", "2026-10-18T00:00:00Z"), false)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Value("status") != "checking" || !jsonx.Equal(stale.Value("events"), current.Value("events")) || statuses(current.Value("events")) != "checking" {
		t.Fatalf("stale %s current %s", jsonx.Repr(stale), jsonx.Repr(current))
	}
}

func agentModelForTest(t testing.TB, value any) *model.Agent {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var a model.Agent
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}
