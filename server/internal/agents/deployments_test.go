package agents

// Ports of the manager-level cases of backend/test_agent_deployments.py,
// test_agent_failure_details.py and test_agent_dns_reports.py (the cases
// that build bundles or call routes of #24/#26 stay with those packages).

import (
	"errors"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func (e *env) begin(id string) *jsonx.Object {
	e.t.Helper()
	return must[*jsonx.Object](e.t)(e.m.BeginDeployment(e.ctx, e.id(), id))
}

func (e *env) record(id string, updates *jsonx.Object, options ...RecordOptions) *jsonx.Object {
	e.t.Helper()
	var o RecordOptions
	if len(options) > 0 {
		o = options[0]
	}
	return must[*jsonx.Object](e.t)(e.m.RecordDeployment(e.ctx, e.id(), id, updates, o))
}

func (e *env) poll(id string) *jsonx.Object {
	e.t.Helper()
	return must[*jsonx.Object](e.t)(e.m.GetDeployment(e.ctx, e.id(), id))
}

func (e *env) finish(id string) { e.m.FinishDeploymentPreparation(e.id(), id) }

func (e *env) latest() *jsonx.Object { return e.get(e.id()).GetObject("latest_deployment") }

func TestPreparationInterruptionAndProfileBinding(t *testing.T) {
	e := newEnv(t)
	e.begin("interrupted")
	if got := e.poll("interrupted").Value("status"); got != "preparing" {
		t.Fatalf("status %v", got)
	}
	restarted := newManager(t, e.repo)
	if got := must[*jsonx.Object](t)(restarted.GetDeployment(e.ctx, e.id(), "interrupted")).Value("status"); got != "preparing" {
		t.Fatalf("restarted sees %v while the lock is held", got)
	}
	e.finish("interrupted")
	if got := must[*jsonx.Object](t)(restarted.GetDeployment(e.ctx, e.id(), "interrupted")).Value("status"); got != "failed" {
		t.Fatalf("after release %v", got)
	}
	e.createProfile("other")
	must[*jsonx.Object](t)(restarted.BeginDeployment(e.ctx, e.id(), "bound"))
	e.editSystem(func(system *jsonx.Object) { e.firstAgent(system).Set("profile_id", "other") })
	if got := must[*jsonx.Object](t)(restarted.GetDeployment(e.ctx, e.id(), "bound")).Value("http_status"); !jsonx.Equal(got, 409) {
		t.Fatalf("get %v", got)
	}
	if got := must[*jsonx.Object](t)(restarted.ActivateDeployment(e.ctx, e.id(), "bound")).Value("http_status"); !jsonx.Equal(got, 409) {
		t.Fatalf("activate %v", got)
	}
	restarted.FinishDeploymentPreparation(e.id(), "bound")
}

func TestFailedRollbackIsBlockingAndCanReconcileAfterAgentRecovery(t *testing.T) {
	e := newEnv(t)
	e.begin("broken")
	e.record("broken", jsonx.Obj("success", false, "status", "rollback_failed", "submitted", true))
	if got := e.begin("next").Value("http_status"); !jsonx.Equal(got, 409) {
		t.Fatalf("begin %v", got)
	}
	e.fake.set(always(jsonx.Obj("success", false, "status", "rolled_back"), 200))
	if got := e.poll("broken").Value("status"); got != "rolled_back" {
		t.Fatalf("poll %v", got)
	}
	if !e.begin("next").GetBool("success") {
		t.Fatal("next must start")
	}
	e.finish("broken")
	e.finish("next")
}

func TestLatePendingResponseNeverRegressesTerminalResult(t *testing.T) {
	e := newEnv(t)
	e.begin("late-response")
	e.record("late-response", jsonx.Obj("status", "unknown", "message", "Awaiting upload"))
	result := e.record("late-response", jsonx.Obj("success", true, "status", "succeeded", "config_version", "confirmed"))
	if result.Has("message") {
		t.Fatal("message must be cleared")
	}
	late := e.record("late-response", jsonx.Obj("success", true, "status", "verifying"))
	if late.Value("status") != "succeeded" || e.get(e.id()).Value("config_version") != "confirmed" {
		t.Fatalf("late %v", late)
	}
	e.finish("late-response")
}

func TestPollBeforeUploadRegistrationDoesNotFailPublication(t *testing.T) {
	for _, finishDuringGet := range []bool{false, true} {
		e := newEnv(t)
		id := "upload-registering"
		e.begin(id)
		e.record(id, jsonx.Obj("status", "uploading", "submitted", true, "success", true))
		e.fake.set(func(Request) (*Response, error) {
			if finishDuringGet {
				e.record(id, jsonx.Obj("status", "checking"))
				e.finish(id)
			}
			return reply(jsonx.Obj("success", false), 404), nil
		})
		pending := e.poll(id)
		if pending.Value("pending") != true || pending.Value("status") != "uploading" {
			t.Fatalf("finish=%v pending %v", finishDuringGet, pending)
		}
		if e.get(e.id()).GetObject("deployments").GetObject(id).Value("status") == "failed" {
			t.Fatal("must not fail")
		}
		e.finish(id)
		e.fake.set(always(jsonx.Obj("success", true, "status", "succeeded", "config_version", "confirmed"), 200))
		if got := e.poll(id).Value("status"); got != "succeeded" {
			t.Fatalf("final %v", got)
		}
	}
}

func TestMissingAgentJournalAfterUploadStillFails(t *testing.T) {
	e := newEnv(t)
	e.begin("missing-upload")
	e.record("missing-upload", jsonx.Obj("status", "uploading", "submitted", true))
	e.finish("missing-upload")
	e.fake.set(always(jsonx.Obj("success", false), 404))
	if got := e.poll("missing-upload").Value("status"); got != "failed" {
		t.Fatalf("status %v", got)
	}
}

func TestPreparationFinishingDuringPollIsNotMarkedInterrupted(t *testing.T) {
	e := newEnv(t)
	e.begin("preparation-race")
	e.m.preparationRunning = func(string, string) (bool, error) {
		e.record("preparation-race", jsonx.Obj("status", "uploading", "submitted", true))
		e.finish("preparation-race")
		return false, nil
	}
	e.fake.set(always(jsonx.Obj("status", "checking", "success", true), 200))
	if got := e.poll("preparation-race").Value("status"); got != "checking" {
		t.Fatalf("status %v", got)
	}
	if got := e.record("preparation-race", jsonx.Obj("status", "failed"), RecordOptions{OnlyUnsubmitted: true}).Value("status"); got != "checking" {
		t.Fatalf("only_unsubmitted %v", got)
	}
}

func TestLateTransportTimeoutDoesNotReplaceObservedCheckingOnActivate(t *testing.T) {
	e := newEnv(t)
	e.begin("late-timeout")
	e.record("late-timeout", jsonx.Obj("status", "ready", "submitted", true))
	e.finish("late-timeout")
	e.fake.set(func(req Request) (*Response, error) {
		if req.Method == "GET" {
			return reply(jsonx.Obj("status", "ready"), 200), nil
		}
		e.record("late-timeout", jsonx.Obj("status", "checking"))
		return nil, &TransportError{Err: errString("late upload/activation response")}
	})
	state := must[*jsonx.Object](t)(e.m.ActivateDeployment(e.ctx, e.id(), "late-timeout"))
	events := state.GetList("events")
	if state.Value("status") != "checking" || events[len(events)-1].(*jsonx.Object).Value("status") != "checking" {
		t.Fatalf("state %v", state)
	}
}

func TestDeploymentHistoryPersistsActualChangesAndRedactsEvents(t *testing.T) {
	e := newEnv(t)
	e.begin("history")
	message := "规则 Feed： https://user:password@rules.test/private-token?token=hidden " + e.agent.GetString("token")
	for i := 0; i < 3; i++ {
		e.record("history", jsonx.Obj("status", "preparing", "message", message))
	}
	e.record("history", jsonx.Obj("status", "uploading", "submitted", true))
	e.record("history", jsonx.Obj("status", "succeeded", "success", true))
	state := newManager(t, e.repo)
	latest := must[*jsonx.Object](t)(state.GetAgentByID(e.ctx, e.id(), true)).GetObject("latest_deployment")
	var statuses []string
	for _, event := range latest.GetList("events") {
		statuses = append(statuses, event.(*jsonx.Object).GetString("status"))
	}
	if strings.Join(statuses, ",") != "preparing,preparing,uploading,succeeded" {
		t.Fatalf("statuses %v", statuses)
	}
	text := jsonx.Repr(latest.Value("events"))
	for _, secret := range []string{"password", "private-token", "hidden", e.agent.GetString("token")} {
		if strings.Contains(text, secret) {
			t.Fatalf("%q leaked: %s", secret, text)
		}
	}
	e.finish("history")
}

func TestOlderAgentStatusTimestampCannotRegressProgressButNewRollbackCan(t *testing.T) {
	e := newEnv(t)
	e.begin("timestamp-order")
	e.record("timestamp-order", jsonx.Obj("status", "checking", "updated_at", "2026-10-18T00:00:00.1Z"))
	if got := e.record("timestamp-order", jsonx.Obj("status", "verifying", "updated_at", "2026-10-18T00:00:00Z")).Value("status"); got != "checking" {
		t.Fatalf("regressed to %v", got)
	}
	if got := e.record("timestamp-order", jsonx.Obj("status", "rolling_back", "updated_at", "2026-10-18T00:00:01Z")).Value("status"); got != "rolling_back" {
		t.Fatalf("got %v", got)
	}
	e.finish("timestamp-order")
}

func TestProfileRebindingRejectedForEveryUnfinishedDeployment(t *testing.T) {
	for _, status := range []string{"preparing", "uploading", "checking", "ready", "rollback_failed"} {
		e := newEnv(t)
		e.createProfile("other")
		e.begin("bound-publish")
		e.record("bound-publish", jsonx.Obj("status", status))
		_, err := e.m.UpdateAgent(e.ctx, e.id(), jsonx.Obj("profile_id", "other"))
		var conflict *DeploymentConflict
		if !errors.As(err, &conflict) {
			t.Fatalf("%s: err %v", status, err)
		}
		if e.get(e.id()).Value("profile_id") != "default" {
			t.Fatal("profile changed")
		}
		if !must[bool](t)(e.m.UpdateAgent(e.ctx, e.id(), jsonx.Obj("profile_id", "default", "name", "renamed"))) {
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
				return reply(jsonx.Obj("success", false, "message", busyMessage), 409), nil
			}
			return reply(jsonx.Obj("success", true, "status", "verifying"), 202), nil
		})
		body := []byte("archive-original-bytes")
		header := map[string][]string{"X-Deployment-Id": {"same-id"}, "X-Content-Sha256": {"same-sha"}}
		result := must[*jsonx.Object](t)(e.m.DeploymentRequest(e.ctx, e.agent, "POST", suffix, header, body, nil))
		if result.Value("status") != "verifying" || len(calls) != 2 || len(sleeps) != 1 || sleeps[0] < 0.0999 || sleeps[0] > 0.1001 {
			t.Fatalf("%s: %v calls %d sleeps %v", suffix, result, len(calls), sleeps)
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
		return reply(jsonx.Obj("message", busyMessage), 409), nil
	})
	if got := must[*jsonx.Object](t)(e.m.DeploymentRequest(e.ctx, e.agent, "POST", "deployments", nil, nil, nil)).Value("http_status"); !jsonx.Equal(got, 409) || calls != 21 {
		t.Fatalf("busy: %v after %d calls", got, calls)
	}
	calls = 0
	must[*jsonx.Object](t)(e.m.DeploymentRequest(e.ctx, e.agent, "POST", "restart", nil, nil, nil))
	if calls != 1 {
		t.Fatalf("restart retried %d", calls)
	}
	calls = 0
	e.fake.set(func(Request) (*Response, error) {
		calls++
		return reply(jsonx.Obj("message", "deployment ID already belongs to different content"), 409), nil
	})
	must[*jsonx.Object](t)(e.m.DeploymentRequest(e.ctx, e.agent, "POST", "deployments", nil, nil, nil))
	if calls != 1 {
		t.Fatalf("conflict retried %d", calls)
	}
	calls = 0
	e.fake.set(func(Request) (*Response, error) {
		calls++
		return nil, &TransportError{Err: errString("ambiguous outcome")}
	})
	if _, err := e.m.DeploymentRequest(e.ctx, e.agent, "POST", "deployments", nil, nil, nil); !IsTransportError(err) || calls != 1 {
		t.Fatalf("timeout: %v after %d", err, calls)
	}
}

func successfulDeployment(e *env, id string) string {
	e.begin(id)
	e.record(id, jsonx.Obj("status", "succeeded", "success", true, "submitted", true, "config_version", "current", "config_revision", 9))
	e.finish(id)
	return id
}

func TestManualRollbackReservesBeforePostAndInvalidatesAppliedBaseline(t *testing.T) {
	e := newEnv(t)
	id := successfulDeployment(e, "manual-test")
	e.editSystem(func(system *jsonx.Object) {
		e.firstAgent(system).Set("applied_content", jsonx.Obj("fingerprint", "current"))
	})
	e.fake.set(func(req Request) (*Response, error) {
		if !strings.HasSuffix(req.URL, "/deployments/"+id+"/rollback") {
			t.Fatalf("url %s", req.URL)
		}
		reserved := e.latest()
		if !reserved.GetBool("manual_rollback") || reserved.Value("status") != "rolling_back" {
			t.Fatalf("not reserved: %v", reserved)
		}
		if got := e.begin("raced-publish").Value("http_status"); !jsonx.Equal(got, 409) {
			t.Fatalf("raced %v", got)
		}
		if got := e.record(id, jsonx.Obj("status", "succeeded")).Value("status"); got != "rolling_back" {
			t.Fatalf("old poll completed the rollback: %v", got)
		}
		return reply(jsonx.Obj("success", true, "status", "rolling_back", "manual_rollback", true), 202), nil
	})
	if got := must[*jsonx.Object](t)(e.m.RollbackDeployment(e.ctx, e.id(), id)).Value("status"); got != "rolling_back" {
		t.Fatalf("rollback %v", got)
	}
	restarted := newManager(t, e.repo)
	restarted.SetClient(e.fake)
	e.fake.set(always(jsonx.Obj("success", true, "status", "rolled_back", "manual_rollback", true, "config_version", "previous"), 200))
	result := must[*jsonx.Object](t)(restarted.GetDeployment(e.ctx, e.id(), id))
	if !result.GetBool("success") || !result.GetBool("manual_rollback") || result.Value("status") != "rolled_back" {
		t.Fatalf("result %v", result)
	}
	current := must[*jsonx.Object](t)(restarted.GetAgentByID(e.ctx, e.id(), true))
	if current.Value("config_version") != "previous" || current.Has("applied_content") || current.Has("pushed_at") || current.Has("config_revision") {
		t.Fatalf("current %v", current)
	}
	if !current.GetObject("latest_deployment").GetBool("manual_rollback") {
		t.Fatal("latest lost the flag")
	}
	again := must[*jsonx.Object](t)(restarted.RollbackDeployment(e.ctx, e.id(), id))
	if !jsonx.Equal(again, result) {
		t.Fatalf("repeat %v != %v", again, result)
	}
}

func TestManualRollbackDefiniteRejectionPreservesSuccessfulPublication(t *testing.T) {
	for _, rejection := range []int{400, 404, 405, 409} {
		e := newEnv(t)
		id := successfulDeployment(e, "manual-test")
		e.fake.set(always(jsonx.Obj("message", "active managed files changed since deployment; refusing rollback"), rejection))
		if got := must[*jsonx.Object](t)(e.m.RollbackDeployment(e.ctx, e.id(), id)).Value("http_status"); !jsonx.Equal(got, rejection) {
			t.Fatalf("%d: %v", rejection, got)
		}
		current := e.get(e.id())
		latest := current.GetObject("latest_deployment")
		events := latest.GetList("events")
		last := events[len(events)-1].(*jsonx.Object)
		if current.Value("config_version") != "current" || latest.Value("status") != "succeeded" || !latest.GetBool("rollback_rejected") ||
			latest.GetBool("manual_rollback") || last.Value("status") != "succeeded" || !strings.Contains(last.GetString("message"), "回滚未执行，原配置继续运行") {
			t.Fatalf("%d: %v", rejection, current)
		}
		if !e.begin("after-rejection").GetBool("success") {
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
		result := must[*jsonx.Object](t)(e.m.RollbackDeployment(e.ctx, e.id(), id))
		if result.Value("status") != "unknown" || !result.GetBool("manual_rollback") || strings.Contains(jsonx.Repr(result), "secret") {
			t.Fatalf("result %v", result)
		}
		var posts []string
		e.fake.set(func(req Request) (*Response, error) {
			if req.Method == "GET" {
				return reply(jsonx.Obj("success", true, "status", "succeeded"), 200), nil
			}
			posts = append(posts, req.URL)
			if retryRejected {
				return reply(jsonx.Obj("message", "rollback backup checksum mismatch"), 409), nil
			}
			return reply(jsonx.Obj("success", true, "status", "rolled_back", "manual_rollback", true, "config_version", "old"), 200), nil
		})
		result = e.poll(id)
		if len(posts) != 1 || posts[0] != "http://agent.test:8080/api/deployments/"+id+"/rollback" {
			t.Fatalf("posts %v", posts)
		}
		if retryRejected {
			events := result.GetList("events")
			if result.Value("status") != "succeeded" || !result.GetBool("rollback_rejected") ||
				result.Value("rollback_message") != "rollback backup checksum mismatch" || result.GetBool("manual_rollback") ||
				!strings.Contains(events[len(events)-1].(*jsonx.Object).GetString("message"), "回滚未执行，原配置继续运行") {
				t.Fatalf("rejected %v", result)
			}
		} else if result.Value("status") != "rolled_back" || !result.GetBool("manual_rollback") {
			t.Fatalf("rolled back %v", result)
		}
	}
}

func TestManualRollbackOwnershipAndOperationGuards(t *testing.T) {
	for _, change := range []string{"profile", "latest", "upgrade", "status"} {
		e := newEnv(t)
		id := successfulDeployment(e, "manual-test")
		e.createProfile("another-profile")
		e.editSystem(func(system *jsonx.Object) {
			saved := e.firstAgent(system)
			switch change {
			case "profile":
				saved.Set("profile_id", "another-profile")
			case "latest":
				saved.Set("latest_deployment", jsonx.Obj("deployment_id", "another-deployment"))
			case "upgrade":
				saved.Set("latest_upgrade", jsonx.Obj("status", "installing"))
			default:
				saved.GetObject("deployments").GetObject(id).Set("status", "failed")
			}
		})
		if got := must[*jsonx.Object](t)(e.m.RollbackDeployment(e.ctx, e.id(), id)).Value("http_status"); !jsonx.Equal(got, 409) {
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
	if got := must[*jsonx.Object](t)(e.m.RollbackDeployment(e.ctx, e.id(), id)).Value("status"); got != "unknown" {
		t.Fatalf("status %v", got)
	}
	e.m.monotonic = func() float64 { return 0 }
	e.fake.set(func(req Request) (*Response, error) {
		if req.Method == "GET" {
			return reply(jsonx.Obj("status", "succeeded"), 200), nil
		}
		return reply(jsonx.Obj("message", busyMessage), 409), nil
	})
	result := e.poll(id)
	if result.Value("status") != "unknown" || !result.GetBool("manual_rollback") || !result.GetBool("pending") {
		t.Fatalf("result %v", result)
	}
	if got := e.begin("unsafe-next").Value("http_status"); !jsonx.Equal(got, 409) {
		t.Fatalf("begin %v", got)
	}
}

func failedRollback(e *env, id string) string {
	e.begin(id)
	e.record(id, jsonx.Obj("success", false, "status", "rollback_failed", "submitted", true,
		"error", "service remains running after stop", "rollback_error", "rollback cannot stop service",
		"rollback_error_details", "1 process refused to stop"))
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
		if reserved.Value("status") != "rolling_back" || reserved.Has("rollback_error") {
			t.Fatalf("reserved %v", reserved)
		}
		if got := e.begin("raced-publish").Value("http_status"); !jsonx.Equal(got, 409) {
			t.Fatalf("raced %v", got)
		}
		return reply(jsonx.Obj("success", true, "deployment_id", id, "status", "rolling_back", "recovering", true), 202), nil
	})
	result := must[*jsonx.Object](t)(e.m.RecoverDeployment(e.ctx, e.id(), id))
	if result.Value("status") != "rolling_back" || jsonx.Truthy(result.Value("http_status")) {
		t.Fatalf("result %v", result)
	}
	found := false
	for _, event := range result.GetList("events") {
		found = found || event.(*jsonx.Object).Value("message") == "正在请求 Agent 重新恢复原配置"
	}
	if !found {
		t.Fatal("recovery event missing")
	}
	e.fake.set(always(jsonx.Obj("success", false, "status", "rolled_back", "error", "service remains running after stop"), 200))
	recovered := e.poll(id)
	if recovered.Value("status") != "rolled_back" || recovered.Has("rollback_error") {
		t.Fatalf("recovered %v", recovered)
	}
	if !e.begin("next").GetBool("success") {
		t.Fatal("next blocked")
	}
	e.finish("next")
	e.finish(id)
}

func TestRecoverOnAgentWithoutEndpointKeepsFailureAndNamesTheCommand(t *testing.T) {
	for _, status := range []int{404, 405} {
		e := newEnv(t)
		id := failedRollback(e, "broken")
		e.fake.set(always(jsonx.NewObject(), status))
		if got := must[*jsonx.Object](t)(e.m.RecoverDeployment(e.ctx, e.id(), id)).Value("http_status"); !jsonx.Equal(got, status) {
			t.Fatalf("%d: %v", status, got)
		}
		current := e.latest()
		message := current.GetString("message")
		if current.Value("status") != "rollback_failed" || current.Value("rollback_error") != "rollback cannot stop service" ||
			current.Value("rollback_error_details") != "1 process refused to stop" || !strings.Contains(message, "1.8.57-go") ||
			!strings.Contains(message, "-config /opt/configflow-agent/config-mihomo.json -recover-and-exit") {
			t.Fatalf("%d: %v", status, current)
		}
		if got := e.begin("blocked").Value("http_status"); !jsonx.Equal(got, 409) {
			t.Fatalf("begin %v", got)
		}
		e.finish(id)
	}
}

func TestRecoverRejectedByAgentKeepsFailureWithReason(t *testing.T) {
	e := newEnv(t)
	id := failedRollback(e, "broken")
	e.fake.set(always(jsonx.Obj("success", false, "message", "deployment broken does not need recovery (status rolled_back)"), 409))
	if got := must[*jsonx.Object](t)(e.m.RecoverDeployment(e.ctx, e.id(), id)).Value("http_status"); !jsonx.Equal(got, 409) {
		t.Fatalf("got %v", got)
	}
	current := e.latest()
	events := current.GetList("events")
	if current.Value("status") != "rollback_failed" || !strings.Contains(current.GetString("message"), "does not need recovery") ||
		events[len(events)-1].(*jsonx.Object).Value("status") != "rollback_failed" {
		t.Fatalf("current %v", current)
	}
	e.finish(id)
}

func TestRecoverTransportErrorWaitsForAgentStatus(t *testing.T) {
	e := newEnv(t)
	id := failedRollback(e, "broken")
	e.fake.set(transportFailure("reset"))
	if got := must[*jsonx.Object](t)(e.m.RecoverDeployment(e.ctx, e.id(), id)).Value("status"); got != "unknown" {
		t.Fatalf("got %v", got)
	}
	e.fake.set(always(jsonx.Obj("success", false, "status", "rolled_back"), 200))
	if got := e.poll(id).Value("status"); got != "rolled_back" {
		t.Fatalf("poll %v", got)
	}
	e.finish(id)
}

func TestRecoverOnlyAppliesToFailedRollback(t *testing.T) {
	e := newEnv(t)
	id := successfulDeployment(e, "manual-test")
	if got := must[*jsonx.Object](t)(e.m.RecoverDeployment(e.ctx, e.id(), id)).Value("http_status"); !jsonx.Equal(got, 409) {
		t.Fatalf("got %v", got)
	}
	if e.latest().Value("status") != "succeeded" || len(e.fake.urls()) != 0 {
		t.Fatal("must not touch the Agent")
	}
	if got := must[*jsonx.Object](t)(e.m.RecoverDeployment(e.ctx, e.id(), "missing")).Value("http_status"); !jsonx.Equal(got, 404) {
		t.Fatalf("missing %v", got)
	}
}

// test_agent_failure_details.py

func TestFailureAndRollbackDetailsAreIndependentAndRecoveryClearsOnlyRollback(t *testing.T) {
	e := newEnv(t)
	e.begin("rollback")
	failed := e.record("rollback", jsonx.Obj("status", "rollback_failed", "error", "start failed",
		"error_details", "new service: address already in use", "rollback_error", "old service failed",
		"rollback_error_details", "old service: permission denied"))
	if jsonx.Equal(failed.Value("error_details"), failed.Value("rollback_error_details")) {
		t.Fatal("details merged")
	}
	recovered := e.record("rollback", jsonx.Obj("status", "rolled_back", "success", false))
	if recovered.Value("error_details") != "new service: address already in use" || jsonx.Truthy(recovered.Value("rollback_error_details")) ||
		jsonx.Truthy(recovered.Value("rollback_error")) {
		t.Fatalf("recovered %v", recovered)
	}
	e.finish("rollback")
}

func TestDetailsAreBoundedRedactedBeforePersistenceAndSuccessClearsThem(t *testing.T) {
	e := newEnv(t)
	e.begin("transport")
	token := e.agent.GetString("token")
	raw := "connection refused\nAuthorization: Bearer " + token + "\npassword: \"sensitive password\"\n" + strings.Repeat("内容", 20000)
	state := e.record("transport", jsonx.Obj("status", "unknown", "error_details", raw))
	detail := state.GetString("error_details")
	if !strings.Contains(detail, "connection refused") || !strings.Contains(detail, "已截断") ||
		strings.Contains(detail, "sensitive password") || strings.Contains(detail, token) || len(detail) > DetailLimit {
		t.Fatalf("detail %d bytes", len(detail))
	}
	disk := string(must[[]byte](t)(jsonx.Default(e.firstAgent(e.system()).Value("deployments"))))
	if strings.Contains(disk, "sensitive password") || strings.Contains(disk, token) {
		t.Fatal("secret persisted")
	}
	if e.record("transport", jsonx.Obj("success", true, "status", "succeeded")).Has("error_details") {
		t.Fatal("success must clear details")
	}
	e.finish("transport")
}

func TestLegacyErrorsKeepAgentReasonOnAllHTTPStatuses(t *testing.T) {
	for _, method := range []string{"push", "restart"} {
		for _, status := range []int{200, 400, 500} {
			e := newEnv(t)
			e.editSystem(func(system *jsonx.Object) { e.firstAgent(system).Set("service_type", "surge") })
			e.fake.set(always(jsonx.Obj("success", false, "message", "Invalid configuration",
				"error_details", "line 7: proxy group does not exist\npassword=do-not-echo"), status))
			var result *jsonx.Object
			if method == "push" {
				result = must[*jsonx.Object](t)(e.m.PushLegacyConfig(e.ctx, e.id(), "[General]", PushConfig{}))
			} else {
				result = must[*jsonx.Object](t)(e.m.RestartAgentService(e.ctx, e.id()))
			}
			details := result.GetString("error_details")
			if result.Value("success") != false || result.Value("message") != "Invalid configuration" ||
				!strings.Contains(details, "line 7") || strings.Contains(details, "do-not-echo") {
				t.Fatalf("%s %d: %v", method, status, result)
			}
		}
	}
}

func TestPlainTextLegacyErrorIsRetainedButHTMLIsNot(t *testing.T) {
	response := &Response{StatusCode: 502, Header: map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}},
		Body: []byte("connection refused on port 9090")}
	if got := ResponseFailure(response, "HTTP 502", nil).Value("error_details"); got != "connection refused on port 9090" {
		t.Fatalf("got %v", got)
	}
	response.Header.Set("Content-Type", "text/html")
	if ResponseFailure(response, "HTTP 502", nil).Has("error_details") {
		t.Fatal("html kept")
	}
}

// test_agent_dns_reports.py (manager parts)

var dnsWarning = jsonx.Obj("name", "dns_hijack", "level", "warn", "message", "DNS 劫持可能绕过 OxiDNS。", "fix", "检查 TUN 中的 DNS 劫持范围。")
var dnsConflict = jsonx.Obj("name", "dns_listener", "level", "error", "message", "UDP 53 已被其他程序占用。", "fix", "调整 DNS 监听端口。")

const checkedAt = "2026-10-18T10:00:00.123456789Z"

func TestWarningReportSurvivesReadyPollActivationAndSuccess(t *testing.T) {
	e := newEnv(t)
	e.begin("dns-warning")
	e.record("dns-warning", jsonx.Obj("success", true, "status", "ready", "submitted", true,
		"dns_checks", []any{dnsWarning}, "dns_checked_at", checkedAt))
	e.finish("dns-warning")
	e.fake.set(func(req Request) (*Response, error) {
		if req.Method == "GET" {
			return reply(jsonx.Obj("success", true, "status", "ready"), 200), nil
		}
		return reply(jsonx.Obj("success", true, "status", "succeeded", "config_version", "dns-approved"), 200), nil
	})
	if got := e.poll("dns-warning").Value("dns_checks"); !jsonx.Equal(got, []any{dnsWarning}) {
		t.Fatalf("poll %v", got)
	}
	activated := must[*jsonx.Object](t)(e.m.ActivateDeployment(e.ctx, e.id(), "dns-warning"))
	if !activated.GetBool("success") || activated.Value("status") != "succeeded" ||
		!jsonx.Equal(activated.Value("dns_checks"), []any{dnsWarning}) || activated.Value("dns_checked_at") != checkedAt {
		t.Fatalf("activated %v", activated)
	}
	restarted := newManager(t, e.repo)
	final := must[*jsonx.Object](t)(restarted.GetDeployment(e.ctx, e.id(), "dns-warning"))
	if !jsonx.Equal(final.Value("dns_checks"), []any{dnsWarning}) ||
		must[*jsonx.Object](t)(restarted.GetAgentByID(e.ctx, e.id(), true)).Value("config_version") != "dns-approved" {
		t.Fatalf("final %v", final)
	}
	e.begin("next-candidate")
	if latest := e.latest(); latest.Has("dns_checks") || latest.Has("dns_checked_at") {
		t.Fatalf("latest %v", latest)
	}
	e.finish("next-candidate")
}

func TestPollSavesDNSFailureAndKeepsPreviousVersion(t *testing.T) {
	e := newEnv(t)
	e.begin("dns-poll")
	e.record("dns-poll", jsonx.Obj("success", true, "status", "verifying", "submitted", true))
	e.finish("dns-poll")
	e.fake.set(always(jsonx.Obj("success", false, "status", "failed", "failed_stage", "verifying",
		"dns_checks", []any{dnsConflict}, "dns_checked_at", checkedAt), 200))
	result := e.poll("dns-poll")
	if result.Value("status") != "failed" || !jsonx.Equal(result.Value("dns_checks"), []any{dnsConflict}) {
		t.Fatalf("result %v", result)
	}
	saved := must[*jsonx.Object](t)(newManager(t, e.repo).GetAgentByID(e.ctx, e.id(), true))
	if !jsonx.Equal(saved.GetObject("latest_deployment").Value("dns_checks"), []any{dnsConflict}) || saved.Value("config_version") != "0" {
		t.Fatalf("saved %v", saved)
	}
}

func TestDNSReportFieldsBoundsAndIncompleteness(t *testing.T) {
	var checks []any
	for i := 0; i < DNSCheckLimit+3; i++ {
		checks = append(checks, jsonx.Obj("name", strings.Repeat("n", DNSNameLimit*2), "level", "pass", "message", "ok"))
	}
	checks = append(checks, "bad")
	report := DNSReportFields(jsonx.Obj("dns_checks", checks, "dns_checked_at", "2026-10-18T10:00:00"), nil)
	list := report.GetList("dns_checks")
	if len(list) != DNSCheckLimit || list[len(list)-1].(*jsonx.Object).Value("name") != "DNS 报告完整性" {
		t.Fatalf("checks %d", len(list))
	}
	if report.Has("dns_checked_at") {
		t.Fatal("naive time accepted")
	}
	if !DNSReportFields(jsonx.Obj("dns_checked_at", checkedAt), nil).Has("dns_checked_at") {
		t.Fatal("aware time refused")
	}
}
