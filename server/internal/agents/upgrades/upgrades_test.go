package upgrades

// Port of backend/test_agent_upgrades.py: upgrade persistence,
// acceptance/health distinction and failure concurrency.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
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
	input, err := agents.DecodeRegistrationInput(json.RawMessage(`{"name":"upgrade","host":"localhost","port":1234,"service_type":"mihomo","deployment_method":"shell","version":"1.1.0-go"}`))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := m.RegisterIdentity(ctx, input, agents.RegistrationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(SetArtifactsForTest(func(context.Context) (model.OrderedMap[model.UpgradeArtifact], error) {
		var out model.OrderedMap[model.UpgradeArtifact]
		for _, arch := range Arches {
			out.Set(arch, model.UpgradeArtifact{SHA256: strings.Repeat("a", 64), Size: 100})
		}
		return out, nil
	}))
	t.Cleanup(func() { requestOverride = nil })
	return &upgradeEnv{t: t, ctx: ctx, repo: r, m: m, ident: reg.ID}
}

func (e *upgradeEnv) updateMap(fields map[string]any) {
	e.t.Helper()
	err := e.m.EditAgents(e.ctx, "", func(_ context.Context, list *[]model.Agent) error {
		for k, v := range fields {
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if err := model.SetStoredMember(&(*list)[0], k, raw); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *upgradeEnv) agent() *model.Agent {
	e.t.Helper()
	agent, err := e.m.ReadAgent(e.ctx, e.ident, true)
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
	if ok, err := e.m.ObserveHeartbeat(e.ctx, e.ident, agentModelForTest(e.t, map[string]any{"version": version})); !ok || err != nil {
		e.t.Fatalf("heartbeat %v %v", ok, err)
	}
}

func (e *upgradeEnv) poll(m *agents.Manager) *model.AgentUpgrade {
	e.t.Helper()
	if m == nil {
		m = e.m
	}
	state, err := PollModel(e.ctx, m, e.ident)
	if err != nil {
		e.t.Fatal(err)
	}
	return state
}

func ErrorClass(err error) string {
	var corrupt *CorruptStateError
	if errors.As(err, &corrupt) {
		return "CorruptStateError"
	}
	if errors.Is(err, agents.ErrHeaderEncoding) {
		return "UnicodeEncodeError"
	}
	if errors.Is(err, agents.ErrAgentEndpoint) {
		return "EndpointError"
	}
	if errors.Is(err, ErrInvalidReport) || IsValueError(err) {
		return "ValueError"
	}
	return ""
}

func AgentResponse(payload any, status int) *agents.Response {
	body, _ := json.Marshal(payload)
	return &agents.Response{StatusCode: status, Header: map[string][]string{"Content-Type": {"application/json"}}, Body: body}
}

func respond(payload any, status int) *agents.Response { return AgentResponse(payload, status) }

func wantValueError(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil || !IsValueError(err) || !strings.Contains(err.Error(), fragment) {
		t.Fatalf("want ValueError %q, got %v", fragment, err)
	}
}

func upgradeState(overrides map[string]any) map[string]any {
	state := map[string]any{
		"update_id": "job", "target_version": agents.GetLatestVersion(), "previous_version": "1.1.0-go",
	}
	for k, v := range overrides {
		state[k] = v
	}
	return state
}

func statuses(events []model.OperationEvent) string {
	var out []string
	for _, event := range events {
		out = append(out, event.Status)
	}
	return strings.Join(out, ",")
}

func strPtr(s string) *string { return &s }

func TestAcceptanceIsNotSuccessAndReservesAcrossRestart(t *testing.T) {
	e := newUpgradeEnv(t)
	info := map[string]any{"agent_id": e.ident, "deployment_method": "shell", "architecture": "linux-amd64", "version": "1.1.0-go"}
	t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, _, path string, _ []byte) (*agents.Response, error) {
		if strings.HasSuffix(path, "info") {
			return respond(info, 200), nil
		}
		return respond(map[string]any{}, 200), nil
	}))
	state, err := StartModel(e.ctx, e.m, e.ident)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "queued" { // Acceptance is not an observed worker stage.
		t.Fatalf("status %v", state.Status)
	}
	restarted := e.restarted()
	_, err = StartModel(e.ctx, restarted, e.ident)
	wantValueError(t, err, "已有更新")
	agent, _ := restarted.ReadAgent(e.ctx, e.ident, true)
	if agent.LatestUpgrade == nil || agent.LatestUpgrade.UpdateID != state.UpdateID {
		t.Fatal("reservation lost")
	}
}

func TestWorkerSuccessStillRequiresVersionIdentityAndMigration(t *testing.T) {
	e := newUpgradeEnv(t)
	e.updateMap(map[string]any{"latest_upgrade": upgradeState(map[string]any{"status": "checking", "protocol": 2, "started_at": now()})})
	info := map[string]any{"agent_id": e.ident, "version": agents.GetLatestVersion(), "migration_ready": false}
	t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, _, path string, _ []byte) (*agents.Response, error) {
		if strings.HasSuffix(path, "info") {
			return respond(info, 200), nil
		}
		return respond(map[string]any{"update_id": "job", "agent_id": e.ident, "status": "succeeded"}, 200), nil
	}))
	if got := e.poll(nil).Status; got != "checking" {
		t.Fatalf("got %v", got)
	}
	info["migration_ready"] = true
	info["agent_id"] = "wrong-agent"
	if got := e.poll(nil).Status; got != "checking" {
		t.Fatalf("got %v", got)
	}
	info["agent_id"] = e.ident
	if got := e.poll(nil).Status; got != "succeeded" {
		t.Fatalf("got %v", got)
	}
}

func TestLegacyRollbackRequiresFreshAuthenticatedHeartbeat(t *testing.T) {
	e := newUpgradeEnv(t)
	e.updateMap(map[string]any{"latest_upgrade": upgradeState(map[string]any{"status": "downloading", "protocol": 1, "started_at": now()})})
	t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, _, path string, _ []byte) (*agents.Response, error) {
		if path == "/api/update" {
			t.Fatal("legacy GET endpoint is unsupported")
		}
		return respond(map[string]any{}, 404), nil
	}))
	reported, err := ReportLegacyJSON(e.ctx, e.m, e.ident, json.RawMessage(`{"update_id":"job","status":"rolling_back"}`))
	if err != nil || reported.Status != "rolling_back" {
		t.Fatalf("%+v %v", reported, err)
	}
	if got := e.poll(nil).Status; got != "rolling_back" {
		t.Fatalf("got %v", got)
	}
	time.Sleep(2 * time.Millisecond)
	e.heartbeat("1.1.0-go")
	if got := e.poll(nil).Status; got != "rolled_back" {
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
	e.updateMap(map[string]any{
		"latest_upgrade": upgradeState(map[string]any{"status": "checking", "protocol": 2, "started_at": started}),
		"last_heartbeat": naiveISO(started),
	})
	t.Cleanup(SetRequestForTest(func(context.Context, *model.Agent, string, string, []byte) (*agents.Response, error) {
		return respond(map[string]any{}, 404), nil
	}))
	if got := e.poll(nil).Status; got != "unknown" {
		t.Fatalf("got %v", got)
	}
	_, err := StartModel(e.ctx, e.m, e.ident)
	wantValueError(t, err, "已有更新")
}

func TestOldVersionHeartbeatAfterTimeoutFailsAndAllowsRetry(t *testing.T) {
	e := newUpgradeEnv(t)
	e.updateMap(map[string]any{"latest_upgrade": upgradeState(map[string]any{"status": "unknown", "protocol": 2, "started_at": now() - 601})})
	e.heartbeat("1.1.0-go")
	info := map[string]any{"agent_id": e.ident, "deployment_method": "shell", "architecture": "linux-amd64", "version": "1.1.0-go"}
	t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, method, path string, _ []byte) (*agents.Response, error) {
		if strings.HasSuffix(path, "info") {
			return respond(info, 200), nil
		}
		if method == "GET" && path == "/api/update" {
			return respond(map[string]any{}, 404), nil
		}
		return respond(map[string]any{}, 200), nil
	}))
	result := e.poll(nil)
	if result.Status != "failed" || result.Error == nil || !strings.Contains(*result.Error, "1.1.0-go") {
		t.Fatalf("result %+v", result)
	}
	state, err := StartModel(e.ctx, e.m, e.ident)
	if err != nil || state.Status != "queued" {
		t.Fatalf("%+v %v", state, err)
	}
}

func TestDockerIsNotSentBinaryUpdate(t *testing.T) {
	e := newUpgradeEnv(t)
	e.updateMap(map[string]any{"deployment_method": "docker"})
	t.Cleanup(SetRequestForTest(func(context.Context, *model.Agent, string, string, []byte) (*agents.Response, error) {
		t.Fatal("contacted Docker binary updater")
		return nil, nil
	}))
	_, err := StartModel(e.ctx, e.m, e.ident)
	wantValueError(t, err, "Docker")
}

func TestFailedRollbackCanConfirmLaterRecovery(t *testing.T) {
	e := newUpgradeEnv(t)
	e.updateMap(map[string]any{"latest_upgrade": upgradeState(map[string]any{"status": "rollback_failed", "protocol": 2, "started_at": now()})})
	t.Cleanup(SetRequestForTest(func(context.Context, *model.Agent, string, string, []byte) (*agents.Response, error) {
		return respond(map[string]any{"update_id": "job", "agent_id": e.ident, "status": "rolled_back"}, 200), nil
	}))
	if got := e.poll(nil).Status; got != "rolled_back" {
		t.Fatalf("got %v", got)
	}
}

func TestWorkerEventsSurviveRestartAndRepeatedPollsWithoutDuplicates(t *testing.T) {
	e := newUpgradeEnv(t)
	e.updateMap(map[string]any{"latest_upgrade": upgradeState(map[string]any{"status": "queued", "protocol": 2, "started_at": now()})})
	payload := map[string]any{
		"update_id": "job", "agent_id": e.ident, "status": "verifying",
		"message": "检查 Agent 候选程序与版本", "updated_at": "2026-10-18T00:00:02Z",
		"events": []any{
			map[string]any{"at": "2026-10-18T00:00:00Z", "status": "queued"},
			map[string]any{"at": "2026-10-18T00:00:01Z", "status": "downloading", "message": "下载 configflow-agent-linux-amd64"},
			map[string]any{"at": "2026-10-18T00:00:02Z", "status": "verifying", "message": "检查 Agent 候选程序与版本"},
		},
	}
	t.Cleanup(SetRequestForTest(func(context.Context, *model.Agent, string, string, []byte) (*agents.Response, error) {
		return respond(payload, 200), nil
	}))
	first := e.poll(nil)
	second := e.poll(e.restarted())
	if diff := testjson.Diff(first.Events, second.Events); diff != "" {
		t.Fatalf("events differ: %s", diff)
	}
	if got := statuses(first.Events); got != "queued,downloading,verifying" {
		t.Fatalf("got %s", got)
	}
	if second.Message == nil || *second.Message != payload["message"] {
		t.Fatalf("message %v", second.Message)
	}
}

func TestUnconfirmedWorkerSuccessDoesNotExposeSuccessEvent(t *testing.T) {
	e := newUpgradeEnv(t)
	e.updateMap(map[string]any{"latest_upgrade": upgradeState(map[string]any{"status": "checking", "protocol": 2, "started_at": now()})})
	worker := map[string]any{
		"update_id": "job", "agent_id": e.ident, "status": "succeeded", "updated_at": "2026-10-18T00:00:01Z",
		"events": []any{
			map[string]any{"at": "2026-10-18T00:00:00Z", "status": "checking"},
			map[string]any{"at": "2026-10-18T00:00:01Z", "status": "succeeded"},
		},
	}
	info := map[string]any{"agent_id": e.ident, "version": agents.GetLatestVersion(), "migration_ready": false}
	t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, _, path string, _ []byte) (*agents.Response, error) {
		if strings.HasSuffix(path, "info") {
			return respond(info, 200), nil
		}
		return respond(worker, 200), nil
	}))
	result := e.poll(nil)
	if result.Status != "checking" || strings.Contains(statuses(result.Events), "succeeded") {
		t.Fatalf("result %+v", result)
	}
	info["migration_ready"] = true
	events := e.poll(nil).Events
	if events[len(events)-1].Status != "succeeded" {
		t.Fatalf("events %+v", events)
	}
}

func TestLateStartAcceptanceKeepsStageAlreadyObservedByPoll(t *testing.T) {
	e := newUpgradeEnv(t)
	info := map[string]any{"agent_id": e.ident, "deployment_method": "shell", "architecture": "linux-amd64"}
	t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, _, path string, body []byte) (*agents.Response, error) {
		if strings.HasSuffix(path, "info") {
			return respond(info, 200), nil
		}
		var payload struct {
			UpdateID string `json:"update_id"`
		}
		_ = json.Unmarshal(body, &payload)
		if _, err := saveModel(e.ctx, e.m, e.ident, payload.UpdateID, upgradePatch{Status: "migrating", Message: strPtr("正在迁移 Agent 配置")}, false); err != nil {
			t.Fatal(err)
		}
		return respond(map[string]any{}, 200), nil
	}))
	result, err := StartModel(e.ctx, e.m, e.ident)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "migrating" || len(result.Events) == 0 || result.Events[len(result.Events)-1].Status != "migrating" {
		t.Fatalf("result %+v", result)
	}
}

func TestChangedUpgradeDuringPreflightPreventsASecondSubmission(t *testing.T) {
	e := newUpgradeEnv(t)
	t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, method, path string, _ []byte) (*agents.Response, error) {
		if method != "GET" || !strings.HasSuffix(path, "info") {
			t.Fatal("late request must not submit a second update")
		}
		e.updateMap(map[string]any{"latest_upgrade": map[string]any{"update_id": "other-completed", "status": "succeeded"}})
		return respond(map[string]any{"agent_id": e.ident, "deployment_method": "shell", "architecture": "linux-amd64"}, 200), nil
	}))
	_, err := StartModel(e.ctx, e.m, e.ident)
	wantValueError(t, err, "更新任务已改变")
	if a := e.agent(); a.LatestUpgrade == nil || a.LatestUpgrade.UpdateID != "other-completed" {
		t.Fatalf("got %+v", a.LatestUpgrade)
	}
}

func TestStaleWorkerResponseDoesNotUndoNewerStateAndOldWorkerHasObservedEvents(t *testing.T) {
	e := newUpgradeEnv(t)
	e.updateMap(map[string]any{"latest_upgrade": map[string]any{"update_id": "job", "status": "queued", "protocol": 2, "started_at": now()}})
	current, err := saveModel(e.ctx, e.m, e.ident, "job", upgradePatch{Status: "checking", AgentUpdatedAt: strPtr("2026-10-18T00:00:00.1Z")}, false)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := saveModel(e.ctx, e.m, e.ident, "job", upgradePatch{Status: "downloading", AgentUpdatedAt: strPtr("2026-10-18T00:00:00Z")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Status != "checking" || !testjson.Equal(current.Events, stale.Events) || statuses(current.Events) != "checking" {
		t.Fatalf("stale %+v current %+v", stale, current)
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
