package agents

// Ports of the manager-level cases of backend/test_agent_deployments.py,
// test_agent_failure_details.py and test_agent_dns_reports.py (the cases
// that build bundles or call routes of #24/#26 stay with those packages).

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"configflow/internal/model"
)

func (e *env) begin(id string) *model.Deployment {
	e.t.Helper()
	return must[*model.Deployment](e.t)(e.m.ReserveDeployment(e.ctx, e.id(), id))
}

func (e *env) record(id string, updates map[string]any, options ...RecordOptions) *model.Deployment {
	e.t.Helper()
	var o RecordOptions
	if len(options) > 0 {
		o = options[0]
	}
	patch, err := DecodeDeploymentUpdate(mustJSON(e.t, updates))
	if err != nil {
		e.t.Fatal(err)
	}
	return must[*model.Deployment](e.t)(e.m.RecordDeploymentModel(e.ctx, e.id(), id, patch, o))
}

func (e *env) poll(id string) *model.Deployment {
	e.t.Helper()
	return must[*model.Deployment](e.t)(e.m.GetDeploymentModel(e.ctx, e.id(), id))
}

func (e *env) finish(id string) { e.m.FinishDeploymentPreparation(e.id(), id) }

func (e *env) latest() *model.Deployment { return e.get(e.id()).LatestDeployment }

func httpStatusOf(d *model.Deployment) int {
	if d == nil || d.HTTPStatus == nil {
		return 0
	}
	return int(*d.HTTPStatus)
}

func TestPreparationInterruptionAndProfileBinding(t *testing.T) {
	e := newEnv(t)
	e.begin("interrupted")
	if got := e.poll("interrupted").Status; got != "preparing" {
		t.Fatalf("status %v", got)
	}
	restarted := newManager(t, e.repo)
	if got := must[*model.Deployment](t)(restarted.GetDeploymentModel(e.ctx, e.id(), "interrupted")).Status; got != "preparing" {
		t.Fatalf("restarted sees %v while the lock is held", got)
	}
	e.finish("interrupted")
	if got := must[*model.Deployment](t)(restarted.GetDeploymentModel(e.ctx, e.id(), "interrupted")).Status; got != "failed" {
		t.Fatalf("after release %v", got)
	}
	e.createProfile("other")
	must[*model.Deployment](t)(restarted.ReserveDeployment(e.ctx, e.id(), "bound"))
	e.editSystem(func(system *model.System) { e.firstAgent(system).ProfileID = "other" })
	if got := httpStatusOf(must[*model.Deployment](t)(restarted.GetDeploymentModel(e.ctx, e.id(), "bound"))); got != 409 {
		t.Fatalf("get %v", got)
	}
	if got := httpStatusOf(must[*model.Deployment](t)(restarted.ActivateDeploymentModel(e.ctx, e.id(), "bound"))); got != 409 {
		t.Fatalf("activate %v", got)
	}
	restarted.FinishDeploymentPreparation(e.id(), "bound")
}

func TestFailedRollbackIsBlockingAndCanReconcileAfterAgentRecovery(t *testing.T) {
	e := newEnv(t)
	e.begin("broken")
	e.record("broken", map[string]any{"success": false, "status": "rollback_failed", "submitted": true})
	if got := httpStatusOf(e.begin("next")); got != 409 {
		t.Fatalf("begin %v", got)
	}
	e.fake.set(always(map[string]any{"success": false, "status": "rolled_back"}, 200))
	if got := e.poll("broken").Status; got != "rolled_back" {
		t.Fatalf("poll %v", got)
	}
	if !deploymentFlagTrue(e.begin("next"), "success") {
		t.Fatal("next must start")
	}
	e.finish("broken")
	e.finish("next")
}

func TestLatePendingResponseNeverRegressesTerminalResult(t *testing.T) {
	e := newEnv(t)
	e.begin("late-response")
	e.record("late-response", map[string]any{"status": "unknown", "message": "Awaiting upload"})
	result := e.record("late-response", map[string]any{"success": true, "status": "succeeded", "config_version": "confirmed"})
	if memberPresent(result, "message") {
		t.Fatal("message must be cleared")
	}
	late := e.record("late-response", map[string]any{"success": true, "status": "verifying"})
	if late.Status != "succeeded" || e.get(e.id()).ConfigVersion != "confirmed" {
		t.Fatalf("late %+v", late)
	}
	e.finish("late-response")
}

func TestPollBeforeUploadRegistrationDoesNotFailPublication(t *testing.T) {
	for _, finishDuringGet := range []bool{false, true} {
		e := newEnv(t)
		id := "upload-registering"
		e.begin(id)
		e.record(id, map[string]any{"status": "uploading", "submitted": true, "success": true})
		e.fake.set(func(Request) (*Response, error) {
			if finishDuringGet {
				e.record(id, map[string]any{"status": "checking"})
				e.finish(id)
			}
			return reply(map[string]any{"success": false}, 404), nil
		})
		pending := e.poll(id)
		if !deploymentFlagTrue(pending, "pending") || pending.Status != "uploading" {
			t.Fatalf("finish=%v pending %+v", finishDuringGet, pending)
		}
		if d, _ := e.get(e.id()).Deployments.Get(id); d.Status == "failed" {
			t.Fatal("must not fail")
		}
		e.finish(id)
		e.fake.set(always(map[string]any{"success": true, "status": "succeeded", "config_version": "confirmed"}, 200))
		if got := e.poll(id).Status; got != "succeeded" {
			t.Fatalf("final %v", got)
		}
	}
}

func TestMissingAgentJournalAfterUploadStillFails(t *testing.T) {
	e := newEnv(t)
	e.begin("missing-upload")
	e.record("missing-upload", map[string]any{"status": "uploading", "submitted": true})
	e.finish("missing-upload")
	e.fake.set(always(map[string]any{"success": false}, 404))
	if got := e.poll("missing-upload").Status; got != "failed" {
		t.Fatalf("status %v", got)
	}
}

func TestPreparationFinishingDuringPollIsNotMarkedInterrupted(t *testing.T) {
	e := newEnv(t)
	e.begin("preparation-race")
	e.m.preparationRunning = func(string, string) (bool, error) {
		e.record("preparation-race", map[string]any{"status": "uploading", "submitted": true})
		e.finish("preparation-race")
		return false, nil
	}
	e.fake.set(always(map[string]any{"status": "checking", "success": true}, 200))
	if got := e.poll("preparation-race").Status; got != "checking" {
		t.Fatalf("status %v", got)
	}
	if got := e.record("preparation-race", map[string]any{"status": "failed"}, RecordOptions{OnlyUnsubmitted: true}).Status; got != "checking" {
		t.Fatalf("only_unsubmitted %v", got)
	}
}

func TestLateTransportTimeoutDoesNotReplaceObservedCheckingOnActivate(t *testing.T) {
	e := newEnv(t)
	e.begin("late-timeout")
	e.record("late-timeout", map[string]any{"status": "ready", "submitted": true})
	e.finish("late-timeout")
	e.fake.set(func(req Request) (*Response, error) {
		if req.Method == "GET" {
			return reply(map[string]any{"status": "ready"}, 200), nil
		}
		e.record("late-timeout", map[string]any{"status": "checking"})
		return nil, &TransportError{Err: errString("late upload/activation response")}
	})
	state := must[*model.Deployment](t)(e.m.ActivateDeploymentModel(e.ctx, e.id(), "late-timeout"))
	events := state.Events
	if state.Status != "checking" || len(events) == 0 || events[len(events)-1].Status != "checking" {
		t.Fatalf("state %+v", state)
	}
}

func TestDeploymentHistoryPersistsActualChangesAndRedactsEvents(t *testing.T) {
	e := newEnv(t)
	e.begin("history")
	message := "规则 Feed： https://user:password@rules.test/private-token?token=hidden " + e.agent.Token
	for i := 0; i < 3; i++ {
		e.record("history", map[string]any{"status": "preparing", "message": message})
	}
	e.record("history", map[string]any{"status": "uploading", "submitted": true})
	e.record("history", map[string]any{"status": "succeeded", "success": true})
	state := newManager(t, e.repo)
	latest := must[*model.Agent](t)(state.ReadAgent(e.ctx, e.id(), true)).LatestDeployment
	var statuses []string
	for _, event := range latest.Events {
		statuses = append(statuses, event.Status)
	}
	if strings.Join(statuses, ",") != "preparing,preparing,uploading,succeeded" {
		t.Fatalf("statuses %v", statuses)
	}
	text := string(mustJSON(t, latest.Events))
	for _, secret := range []string{"password", "private-token", "hidden", e.agent.Token} {
		if strings.Contains(text, secret) {
			t.Fatalf("%q leaked: %s", secret, text)
		}
	}
	e.finish("history")
}

func TestOlderAgentStatusTimestampCannotRegressProgressButNewRollbackCan(t *testing.T) {
	e := newEnv(t)
	e.begin("timestamp-order")
	e.record("timestamp-order", map[string]any{"status": "checking", "updated_at": "2026-10-18T00:00:00.1Z"})
	if got := e.record("timestamp-order", map[string]any{"status": "verifying", "updated_at": "2026-10-18T00:00:00Z"}).Status; got != "checking" {
		t.Fatalf("regressed to %v", got)
	}
	if got := e.record("timestamp-order", map[string]any{"status": "rolling_back", "updated_at": "2026-10-18T00:00:01Z"}).Status; got != "rolling_back" {
		t.Fatalf("got %v", got)
	}
	e.finish("timestamp-order")
}

func TestProfileRebindingRejectedForEveryUnfinishedDeployment(t *testing.T) {
	for _, status := range []string{"preparing", "uploading", "checking", "ready", "rollback_failed"} {
		e := newEnv(t)
		e.createProfile("other")
		e.begin("bound-publish")
		e.record("bound-publish", map[string]any{"status": status})
		_, err := e.editAgentJSON(e.id(), map[string]any{"profile_id": "other"})
		var conflict *DeploymentConflict
		if !errors.As(err, &conflict) {
			t.Fatalf("%s: err %v", status, err)
		}
		if e.get(e.id()).ProfileID != "default" {
			t.Fatal("profile changed")
		}
		if !must[bool](t)(e.editAgentJSON(e.id(), map[string]any{"profile_id": "default", "name": "renamed"})) {
			t.Fatal("same-profile edit must pass")
		}
		e.finish("bound-publish")
	}
}

func TestPostRetriesOnlyExactBusyWithIdenticalArchiveAndIdentity(t *testing.T) {
	for _, suffix := range []string{"deployments", "deployments/test/activate", "deployments/test/rollback"} {
		e := newEnv(t)
		var sleeps []float64
		e.m.sleep = func(d time.Duration) { sleeps = append(sleeps, d.Seconds()) }
		var calls []Request
		e.fake.set(func(req Request) (*Response, error) {
			calls = append(calls, req)
			if len(calls) == 1 {
				return reply(map[string]any{"success": false, "message": busyMessage}, 409), nil
			}
			return reply(map[string]any{"success": true, "status": "verifying"}, 202), nil
		})
		body := []byte("archive-original-bytes")
		header := map[string][]string{"X-Deployment-Id": {"same-id"}, "X-Content-Sha256": {"same-sha"}}
		result := must[*model.Deployment](t)(e.m.DeploymentRequestModel(e.ctx, e.agent, "POST", suffix, header, body, nil))
		if result.Status != "verifying" || len(calls) != 2 || len(sleeps) != 1 || sleeps[0] < 0.0999 || sleeps[0] > 0.1001 {
			t.Fatalf("%s: %+v calls %d sleeps %v", suffix, result, len(calls), sleeps)
		}
		if calls[0].URL != calls[1].URL || string(calls[1].Body) != string(body) || calls[1].Header.Get("X-Deployment-Id") != "same-id" {
			t.Fatal("retry must repeat the identical request")
		}
	}
}

func TestBusyRetriesBoundedOtherConflictsAndNetworkErrorsNeverRetried(t *testing.T) {
	e := newEnv(t)
	e.m.monotonic = func() float64 { return 0 }
	calls := 0
	e.fake.set(func(Request) (*Response, error) {
		calls++
		return reply(map[string]any{"message": busyMessage}, 409), nil
	})
	if got := httpStatusOf(must[*model.Deployment](t)(e.m.DeploymentRequestModel(e.ctx, e.agent, "POST", "deployments", nil, nil, nil))); got != 409 || calls != 21 {
		t.Fatalf("busy: %v after %d calls", got, calls)
	}
	calls = 0
	must[*model.Deployment](t)(e.m.DeploymentRequestModel(e.ctx, e.agent, "POST", "restart", nil, nil, nil))
	if calls != 1 {
		t.Fatalf("restart retried %d", calls)
	}
	calls = 0
	e.fake.set(func(Request) (*Response, error) {
		calls++
		return reply(map[string]any{"message": "deployment ID already belongs to different content"}, 409), nil
	})
	must[*model.Deployment](t)(e.m.DeploymentRequestModel(e.ctx, e.agent, "POST", "deployments", nil, nil, nil))
	if calls != 1 {
		t.Fatalf("conflict retried %d", calls)
	}
	calls = 0
	e.fake.set(func(Request) (*Response, error) {
		calls++
		return nil, &TransportError{Err: errString("ambiguous outcome")}
	})
	if _, err := e.m.DeploymentRequestModel(e.ctx, e.agent, "POST", "deployments", nil, nil, nil); !IsTransportError(err) || calls != 1 {
		t.Fatalf("timeout: %v after %d", err, calls)
	}
}

func successfulDeployment(e *env, id string) string {
	e.begin(id)
	e.record(id, map[string]any{"status": "succeeded", "success": true, "submitted": true, "config_version": "current", "config_revision": 9})
	e.finish(id)
	return id
}

func TestManualRollbackReservesBeforePostAndInvalidatesAppliedBaseline(t *testing.T) {
	e := newEnv(t)
	id := successfulDeployment(e, "manual-test")
	e.editSystem(func(system *model.System) {
		_ = e.firstAgent(system).SetJSON("applied_content", []byte(`{"fingerprint":"current"}`))
	})
	e.fake.set(func(req Request) (*Response, error) {
		if !strings.HasSuffix(req.URL, "/deployments/"+id+"/rollback") {
			t.Fatalf("url %s", req.URL)
		}
		reserved := e.latest()
		if !deploymentFlagTrue(reserved, "manual_rollback") || reserved.Status != "rolling_back" {
			t.Fatalf("not reserved: %+v", reserved)
		}
		if got := httpStatusOf(e.begin("raced-publish")); got != 409 {
			t.Fatalf("raced %v", got)
		}
		if got := e.record(id, map[string]any{"status": "succeeded"}).Status; got != "rolling_back" {
			t.Fatalf("old poll completed the rollback: %v", got)
		}
		return reply(map[string]any{"success": true, "status": "rolling_back", "manual_rollback": true}, 202), nil
	})
	if got := must[*model.Deployment](t)(e.m.RollbackDeploymentModel(e.ctx, e.id(), id)).Status; got != "rolling_back" {
		t.Fatalf("rollback %v", got)
	}
	restarted := newManager(t, e.repo)
	restarted.SetClient(e.fake)
	e.fake.set(always(map[string]any{"success": true, "status": "rolled_back", "manual_rollback": true, "config_version": "previous"}, 200))
	result := must[*model.Deployment](t)(restarted.GetDeploymentModel(e.ctx, e.id(), id))
	if !deploymentFlagTrue(result, "success") || !deploymentFlagTrue(result, "manual_rollback") || result.Status != "rolled_back" {
		t.Fatalf("result %+v", result)
	}
	current := must[*model.Agent](t)(restarted.ReadAgent(e.ctx, e.id(), true))
	if current.ConfigVersion != "previous" || memberPresent(current, "applied_content") || memberPresent(current, "pushed_at") || memberPresent(current, "config_revision") {
		t.Fatalf("current %+v", current)
	}
	if !deploymentFlagTrue(current.LatestDeployment, "manual_rollback") {
		t.Fatal("latest lost the flag")
	}
	again := must[*model.Deployment](t)(restarted.RollbackDeploymentModel(e.ctx, e.id(), id))
	if diff := diffJSONValues(t, result, again); diff != "" {
		t.Fatalf("repeat differs: %s", diff)
	}
}

func TestManualRollbackDefiniteRejectionPreservesSuccessfulPublication(t *testing.T) {
	for _, rejection := range []int{400, 404, 405, 409} {
		e := newEnv(t)
		id := successfulDeployment(e, "manual-test")
		e.fake.set(always(map[string]any{"message": "active managed files changed since deployment; refusing rollback"}, rejection))
		if got := httpStatusOf(must[*model.Deployment](t)(e.m.RollbackDeploymentModel(e.ctx, e.id(), id))); got != rejection {
			t.Fatalf("%d: %v", rejection, got)
		}
		current := e.get(e.id())
		latest := current.LatestDeployment
		events := latest.Events
		last := events[len(events)-1]
		if current.ConfigVersion != "current" || latest.Status != "succeeded" || !deploymentFlagTrue(latest, "rollback_rejected") ||
			deploymentFlagTrue(latest, "manual_rollback") || last.Status != "succeeded" || last.Message == nil || !strings.Contains(*last.Message, "回滚未执行，原配置继续运行") {
			t.Fatalf("%d: %+v", rejection, current)
		}
		if !deploymentFlagTrue(e.begin("after-rejection"), "success") {
			t.Fatal("next publication blocked")
		}
		e.finish("after-rejection")
	}
}

func TestManualRollbackTransportTimeoutReconcilesSameIdentity(t *testing.T) {
	for _, retryRejected := range []bool{false, true} {
		e := newEnv(t)
		id := successfulDeployment(e, "manual-test")
		e.fake.set(transportFailure("secret URL must not leak"))
		result := must[*model.Deployment](t)(e.m.RollbackDeploymentModel(e.ctx, e.id(), id))
		if result.Status != "unknown" || !deploymentFlagTrue(result, "manual_rollback") || strings.Contains(string(mustJSON(t, result)), "secret") {
			t.Fatalf("result %+v", result)
		}
		var posts []string
		e.fake.set(func(req Request) (*Response, error) {
			if req.Method == "GET" {
				return reply(map[string]any{"success": true, "status": "succeeded"}, 200), nil
			}
			posts = append(posts, req.URL)
			if retryRejected {
				return reply(map[string]any{"message": "rollback backup checksum mismatch"}, 409), nil
			}
			return reply(map[string]any{"success": true, "status": "rolled_back", "manual_rollback": true, "config_version": "old"}, 200), nil
		})
		result = e.poll(id)
		if len(posts) != 1 || posts[0] != "http://agent.test:8080/api/deployments/"+id+"/rollback" {
			t.Fatalf("posts %v", posts)
		}
		if retryRejected {
			events := result.Events
			last := events[len(events)-1]
			if result.Status != "succeeded" || !deploymentFlagTrue(result, "rollback_rejected") ||
				derefStr(result.RollbackMessage) != "rollback backup checksum mismatch" || deploymentFlagTrue(result, "manual_rollback") ||
				last.Message == nil || !strings.Contains(*last.Message, "回滚未执行，原配置继续运行") {
				t.Fatalf("rejected %+v", result)
			}
		} else if result.Status != "rolled_back" || !deploymentFlagTrue(result, "manual_rollback") {
			t.Fatalf("rolled back %+v", result)
		}
	}
}

func TestManualRollbackOwnershipAndOperationGuards(t *testing.T) {
	for _, change := range []string{"profile", "latest", "upgrade", "status"} {
		e := newEnv(t)
		id := successfulDeployment(e, "manual-test")
		e.createProfile("another-profile")
		e.editSystem(func(system *model.System) {
			saved := e.firstAgent(system)
			switch change {
			case "profile":
				saved.ProfileID = "another-profile"
			case "latest":
				_ = saved.SetJSON("latest_deployment", []byte(`{"deployment_id":"another-deployment"}`))
			case "upgrade":
				_ = saved.SetJSON("latest_upgrade", []byte(`{"status":"installing"}`))
			default:
				d, _ := saved.Deployments.Get(id)
				d.Status = "failed"
				saved.Deployments.Set(id, d)
			}
		})
		if got := httpStatusOf(must[*model.Deployment](t)(e.m.RollbackDeploymentModel(e.ctx, e.id(), id))); got != 409 {
			t.Fatalf("%s: %v", change, got)
		}
		if len(e.fake.urls()) != 0 {
			t.Fatalf("%s: guard must precede network", change)
		}
	}
}

func TestManualRollbackBusyAfterAmbiguousRequestRemainsPending(t *testing.T) {
	e := newEnv(t)
	id := successfulDeployment(e, "manual-test")
	e.fake.set(transportFailure("ambiguous"))
	if got := must[*model.Deployment](t)(e.m.RollbackDeploymentModel(e.ctx, e.id(), id)).Status; got != "unknown" {
		t.Fatalf("status %v", got)
	}
	e.m.monotonic = func() float64 { return 0 }
	e.fake.set(func(req Request) (*Response, error) {
		if req.Method == "GET" {
			return reply(map[string]any{"status": "succeeded"}, 200), nil
		}
		return reply(map[string]any{"message": busyMessage}, 409), nil
	})
	result := e.poll(id)
	if result.Status != "unknown" || !deploymentFlagTrue(result, "manual_rollback") || !deploymentFlagTrue(result, "pending") {
		t.Fatalf("result %+v", result)
	}
	if got := httpStatusOf(e.begin("unsafe-next")); got != 409 {
		t.Fatalf("begin %v", got)
	}
}

func failedRollback(e *env, id string) string {
	e.begin(id)
	e.record(id, map[string]any{
		"success": false, "status": "rollback_failed", "submitted": true,
		"error": "service remains running after stop", "rollback_error": "rollback cannot stop service",
		"rollback_error_details": "1 process refused to stop",
	})
	return id
}

func TestRecoverFailedRollbackRequestsAgentRecoveryAndReconciles(t *testing.T) {
	e := newEnv(t)
	id := failedRollback(e, "broken")
	e.fake.set(func(req Request) (*Response, error) {
		if !strings.HasSuffix(req.URL, "/deployments/"+id+"/recover") {
			t.Fatalf("url %s", req.URL)
		}
		reserved := e.latest()
		if reserved.Status != "rolling_back" || memberPresent(reserved, "rollback_error") {
			t.Fatalf("reserved %+v", reserved)
		}
		if got := httpStatusOf(e.begin("raced-publish")); got != 409 {
			t.Fatalf("raced %v", got)
		}
		return reply(map[string]any{"success": true, "deployment_id": id, "status": "rolling_back", "recovering": true}, 202), nil
	})
	result := must[*model.Deployment](t)(e.m.RecoverDeploymentModel(e.ctx, e.id(), id))
	if result.Status != "rolling_back" || httpStatusOf(result) != 0 {
		t.Fatalf("result %+v", result)
	}
	found := false
	for _, event := range result.Events {
		if event.Message != nil && *event.Message == "正在请求 Agent 重新恢复原配置" {
			found = true
		}
	}
	if !found {
		t.Fatal("recovery event missing")
	}
	e.fake.set(always(map[string]any{"success": false, "status": "rolled_back", "error": "service remains running after stop"}, 200))
	recovered := e.poll(id)
	if recovered.Status != "rolled_back" || memberPresent(recovered, "rollback_error") {
		t.Fatalf("recovered %+v", recovered)
	}
	if !deploymentFlagTrue(e.begin("next"), "success") {
		t.Fatal("next blocked")
	}
	e.finish("next")
	e.finish(id)
}

func TestRecoverOnAgentWithoutEndpointKeepsFailureAndNamesTheCommand(t *testing.T) {
	for _, status := range []int{404, 405} {
		e := newEnv(t)
		id := failedRollback(e, "broken")
		e.fake.set(always(map[string]any{}, status))
		if got := httpStatusOf(must[*model.Deployment](t)(e.m.RecoverDeploymentModel(e.ctx, e.id(), id))); got != status {
			t.Fatalf("%d: %v", status, got)
		}
		current := e.latest()
		message := derefStr(current.Message)
		if current.Status != "rollback_failed" || derefStr(current.RollbackError) != "rollback cannot stop service" ||
			derefStr(current.RollbackErrorDetails) != "1 process refused to stop" || !strings.Contains(message, "1.8.57-go") ||
			!strings.Contains(message, "-config /opt/configflow-agent/config-mihomo.json -recover-and-exit") {
			t.Fatalf("%d: %+v", status, current)
		}
		if got := httpStatusOf(e.begin("blocked")); got != 409 {
			t.Fatalf("begin %v", got)
		}
		e.finish(id)
	}
}

func TestRecoverRejectedByAgentKeepsFailureWithReason(t *testing.T) {
	e := newEnv(t)
	id := failedRollback(e, "broken")
	e.fake.set(always(map[string]any{"success": false, "message": "deployment broken does not need recovery (status rolled_back)"}, 409))
	if got := httpStatusOf(must[*model.Deployment](t)(e.m.RecoverDeploymentModel(e.ctx, e.id(), id))); got != 409 {
		t.Fatalf("got %v", got)
	}
	current := e.latest()
	events := current.Events
	if current.Status != "rollback_failed" || !strings.Contains(derefStr(current.Message), "does not need recovery") ||
		events[len(events)-1].Status != "rollback_failed" {
		t.Fatalf("current %+v", current)
	}
	e.finish(id)
}

func TestRecoverTransportErrorWaitsForAgentStatus(t *testing.T) {
	e := newEnv(t)
	id := failedRollback(e, "broken")
	e.fake.set(transportFailure("reset"))
	if got := must[*model.Deployment](t)(e.m.RecoverDeploymentModel(e.ctx, e.id(), id)).Status; got != "unknown" {
		t.Fatalf("got %v", got)
	}
	e.fake.set(always(map[string]any{"success": false, "status": "rolled_back"}, 200))
	if got := e.poll(id).Status; got != "rolled_back" {
		t.Fatalf("poll %v", got)
	}
	e.finish(id)
}

func TestRecoverOnlyAppliesToFailedRollback(t *testing.T) {
	e := newEnv(t)
	id := successfulDeployment(e, "manual-test")
	if got := httpStatusOf(must[*model.Deployment](t)(e.m.RecoverDeploymentModel(e.ctx, e.id(), id))); got != 409 {
		t.Fatalf("got %v", got)
	}
	if e.latest().Status != "succeeded" || len(e.fake.urls()) != 0 {
		t.Fatal("must not touch the Agent")
	}
	if got := httpStatusOf(must[*model.Deployment](t)(e.m.RecoverDeploymentModel(e.ctx, e.id(), "missing"))); got != 404 {
		t.Fatalf("missing %v", got)
	}
}

// test_agent_failure_details.py

func TestFailureAndRollbackDetailsAreIndependentAndRecoveryClearsOnlyRollback(t *testing.T) {
	e := newEnv(t)
	e.begin("rollback")
	failed := e.record("rollback", map[string]any{
		"status": "rollback_failed", "error": "start failed",
		"error_details": "new service: address already in use", "rollback_error": "old service failed",
		"rollback_error_details": "old service: permission denied",
	})
	if derefStr(failed.ErrorDetails) == derefStr(failed.RollbackErrorDetails) {
		t.Fatal("details merged")
	}
	recovered := e.record("rollback", map[string]any{"status": "rolled_back", "success": false})
	if derefStr(recovered.ErrorDetails) != "new service: address already in use" || derefStr(recovered.RollbackErrorDetails) != "" ||
		derefStr(recovered.RollbackError) != "" {
		t.Fatalf("recovered %+v", recovered)
	}
	e.finish("rollback")
}

func TestDetailsAreBoundedRedactedBeforePersistenceAndSuccessClearsThem(t *testing.T) {
	e := newEnv(t)
	e.begin("transport")
	token := e.agent.Token
	raw := "connection refused\nAuthorization: Bearer " + token + "\npassword: \"sensitive password\"\n" + strings.Repeat("内容", 20000)
	state := e.record("transport", map[string]any{"status": "unknown", "error_details": raw})
	detail := derefStr(state.ErrorDetails)
	if !strings.Contains(detail, "connection refused") || !strings.Contains(detail, "已截断") ||
		strings.Contains(detail, "sensitive password") || strings.Contains(detail, token) || len(detail) > DetailLimit {
		t.Fatalf("detail %d bytes", len(detail))
	}
	disk := string(mustJSON(t, e.firstAgent(e.system()).Deployments))
	if strings.Contains(disk, "sensitive password") || strings.Contains(disk, token) {
		t.Fatal("secret persisted")
	}
	if memberPresent(e.record("transport", map[string]any{"success": true, "status": "succeeded"}), "error_details") {
		t.Fatal("success must clear details")
	}
	e.finish("transport")
}

func TestLegacyErrorsKeepAgentReasonOnAllHTTPStatuses(t *testing.T) {
	for _, method := range []string{"push", "restart"} {
		for _, status := range []int{200, 400, 500} {
			e := newEnv(t)
			e.editSystem(func(system *model.System) { e.firstAgent(system).ServiceType = "surge" })
			e.fake.set(always(map[string]any{
				"success": false, "message": "Invalid configuration",
				"error_details": "line 7: proxy group does not exist\npassword=do-not-echo",
			}, status))
			var raw json.RawMessage
			if method == "push" {
				raw = must[json.RawMessage](t)(e.m.PushLegacyConfigModel(e.ctx, e.id(), "[General]", LegacyConfigOptions{}))
			} else {
				raw = must[json.RawMessage](t)(e.m.RunDirectOperation(e.ctx, e.id(), RestartService, nil, nil))
			}
			var result struct {
				Success      bool   `json:"success"`
				Message      string `json:"message"`
				ErrorDetails string `json:"error_details"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			if result.Success != false || result.Message != "Invalid configuration" ||
				!strings.Contains(result.ErrorDetails, "line 7") || strings.Contains(result.ErrorDetails, "do-not-echo") {
				t.Fatalf("%s %d: %s", method, status, raw)
			}
		}
	}
}

func TestPlainTextLegacyErrorIsRetainedButHTMLIsNot(t *testing.T) {
	response := &Response{
		StatusCode: 502,
		Header:     map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}},
		Body:       []byte("connection refused on port 9090"),
	}
	if got := derefStr(must[*model.Deployment](t)(ResponseFailureModel(response, "HTTP 502", nil)).ErrorDetails); got != "connection refused on port 9090" {
		t.Fatalf("got %q", got)
	}
	response.Header.Set("Content-Type", "text/html")
	if memberPresent(must[*model.Deployment](t)(ResponseFailureModel(response, "HTTP 502", nil)), "error_details") {
		t.Fatal("html kept")
	}
}

// test_agent_dns_reports.py (manager parts)

var dnsWarning = map[string]any{"name": "dns_hijack", "level": "warn", "message": "DNS 劫持可能绕过 OxiDNS。", "fix": "检查 TUN 中的 DNS 劫持范围。"}
var dnsConflict = map[string]any{"name": "dns_listener", "level": "error", "message": "UDP 53 已被其他程序占用。", "fix": "调整 DNS 监听端口。"}

const checkedAt = "2026-10-18T10:00:00.123456789Z"

func TestWarningReportSurvivesReadyPollActivationAndSuccess(t *testing.T) {
	e := newEnv(t)
	e.begin("dns-warning")
	e.record("dns-warning", map[string]any{
		"success": true, "status": "ready", "submitted": true,
		"dns_checks": []any{dnsWarning}, "dns_checked_at": checkedAt,
	})
	e.finish("dns-warning")
	e.fake.set(func(req Request) (*Response, error) {
		if req.Method == "GET" {
			return reply(map[string]any{"success": true, "status": "ready"}, 200), nil
		}
		return reply(map[string]any{"success": true, "status": "succeeded", "config_version": "dns-approved"}, 200), nil
	})
	if diff := diffJSONValues(t, []any{dnsWarning}, e.poll("dns-warning").DNSChecks); diff != "" {
		t.Fatalf("poll: %s", diff)
	}
	activated := must[*model.Deployment](t)(e.m.ActivateDeploymentModel(e.ctx, e.id(), "dns-warning"))
	if !deploymentFlagTrue(activated, "success") || activated.Status != "succeeded" ||
		diffJSONValues(t, []any{dnsWarning}, activated.DNSChecks) != "" || derefStr(activated.DNSCheckedAt) != checkedAt {
		t.Fatalf("activated %+v", activated)
	}
	restarted := newManager(t, e.repo)
	final := must[*model.Deployment](t)(restarted.GetDeploymentModel(e.ctx, e.id(), "dns-warning"))
	if diffJSONValues(t, []any{dnsWarning}, final.DNSChecks) != "" ||
		must[*model.Agent](t)(restarted.ReadAgent(e.ctx, e.id(), true)).ConfigVersion != "dns-approved" {
		t.Fatalf("final %+v", final)
	}
	e.begin("next-candidate")
	if latest := e.latest(); memberPresent(latest, "dns_checks") || memberPresent(latest, "dns_checked_at") {
		t.Fatalf("latest %+v", latest)
	}
	e.finish("next-candidate")
}

func TestPollSavesDNSFailureAndKeepsPreviousVersion(t *testing.T) {
	e := newEnv(t)
	e.begin("dns-poll")
	e.record("dns-poll", map[string]any{"success": true, "status": "verifying", "submitted": true})
	e.finish("dns-poll")
	e.fake.set(always(map[string]any{
		"success": false, "status": "failed", "failed_stage": "verifying",
		"dns_checks": []any{dnsConflict}, "dns_checked_at": checkedAt,
	}, 200))
	result := e.poll("dns-poll")
	if result.Status != "failed" || diffJSONValues(t, []any{dnsConflict}, result.DNSChecks) != "" {
		t.Fatalf("result %+v", result)
	}
	saved := must[*model.Agent](t)(newManager(t, e.repo).ReadAgent(e.ctx, e.id(), true))
	if diffJSONValues(t, []any{dnsConflict}, saved.LatestDeployment.DNSChecks) != "" || saved.ConfigVersion != "0" {
		t.Fatalf("saved %+v", saved)
	}
}

func TestDNSReportFieldsBoundsAndIncompleteness(t *testing.T) {
	var checks []any
	for i := 0; i < DNSCheckLimit+3; i++ {
		checks = append(checks, map[string]any{"name": strings.Repeat("n", DNSNameLimit*2), "level": "pass", "message": "ok"})
	}
	checks = append(checks, "bad")
	var dep model.Deployment
	if err := json.Unmarshal(mustJSON(t, map[string]any{"dns_checks": checks, "dns_checked_at": "2026-10-18T10:00:00"}), &dep); err != nil {
		t.Fatal(err)
	}
	report, err := SanitizeDNSReport(&dep, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Checks == nil || len(*report.Checks) != DNSCheckLimit || (*report.Checks)[len(*report.Checks)-1].Name != "DNS 报告完整性" {
		t.Fatalf("checks %v", report.Checks)
	}
	if report.CheckedAt != nil {
		t.Fatal("naive time accepted")
	}
	var dep2 model.Deployment
	if err := json.Unmarshal(mustJSON(t, map[string]any{"dns_checked_at": checkedAt}), &dep2); err != nil {
		t.Fatal(err)
	}
	if report2, err := SanitizeDNSReport(&dep2, nil); err != nil || report2.CheckedAt == nil {
		t.Fatalf("aware time refused: %+v %v", report2, err)
	}
}
