package agents

// Ports of backend/test_agent_failure_response_states.py (the manager cases;
// test_immediate_rejections_are_redacted_like_recorded_history calls the
// deployment routes and is deferred to A14), the remaining pure case of
// test_agent_failure_details.py and backend/test_agent_deployment_writes.py.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/redact"
)

// recordTask is test_agent_failure_response_states._record_task.
func recordTask(e *env, status string) string {
	id := "diagnostic-state"
	e.begin(id)
	e.record(id, map[string]any{"status": status, "success": true, "submitted": true})
	e.finish(id)
	return id
}

func legacyCall(e *env, method string) (json.RawMessage, error) {
	e.t.Helper()
	if method == "push" {
		return e.m.PushLegacyConfigModel(e.ctx, e.id(), "[General]", LegacyConfigOptions{})
	}
	return e.m.RunDirectOperation(e.ctx, e.id(), RestartService, nil, nil)
}

func TestLegacySuccessHTTPRequiresAJSONObject(t *testing.T) {
	for _, method := range []string{"push", "restart"} {
		for _, payload := range []any{nil, []any{}, "unexpected text", 17} {
			e := newEnv(t)
			e.editSystem(func(system *model.System) { e.firstAgent(system).ServiceType = "surge" })
			before := e.get(e.id())
			e.fake.set(always(payload, 200))
			head := remoteReferenceHead(t, e)
			result, callErr := legacyCall(e, method)
			if verifyNativeLegacyReply(t, e, method, string(rawLegacyReply(payload)), map[string]any{"success": false, "message": "Invalid Agent response"}, callErr, head) {
				continue
			}
			if callErr != nil {
				t.Fatal(callErr)
			}
			if diff := diffJSONValues(t, map[string]any{"success": false, "message": "Invalid Agent response"}, result); diff != "" {
				t.Fatalf("%s %v: %s", method, payload, diff)
			}
			saved := e.get(e.id())
			if saved.ConfigVersion != before.ConfigVersion || diffJSONValues(t, before.PushedAt, saved.PushedAt) != "" {
				t.Fatalf("%s %v: saved %+v", method, payload, saved)
			}
		}
	}
}

func TestLegacyInvalidJSONKeepsSafePlainTextExplanation(t *testing.T) {
	for _, method := range []string{"push", "restart"} {
		e := newEnv(t)
		e.editSystem(func(system *model.System) { e.firstAgent(system).ServiceType = "surge" })
		e.fake.set(func(Request) (*Response, error) {
			return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/plain"}},
				Body: []byte("upstream service unavailable\npassword=do-not-display")}, nil
		})
		head := remoteReferenceHead(t, e)
		result, callErr := legacyCall(e, method)
		if verifyNativeLegacyReply(t, e, method, "upstream service unavailable\npassword=do-not-display", nil, callErr, head) {
			continue
		}
		if callErr != nil {
			t.Fatal(callErr)
		}
		var parsed struct {
			Success      bool   `json:"success"`
			ErrorDetails string `json:"error_details"`
		}
		if err := json.Unmarshal(result, &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Success != false || !strings.Contains(parsed.ErrorDetails, "upstream service unavailable") || strings.Contains(parsed.ErrorDetails, "do-not-display") {
			t.Fatalf("%s: %s", method, result)
		}
	}
}

func TestConfirmedAgentStageDiscardsOldUnknownTransportDetails(t *testing.T) {
	for _, newStatus := range []string{"verifying", "checking", "failed", "rolling_back"} {
		e := newEnv(t)
		id := recordTask(e, "unknown")
		e.record(id, map[string]any{
			"status": "unknown", "success": true,
			"error_details":          "previous connection refused",
			"rollback_error_details": "previous connection reset",
		})
		update := map[string]any{"status": newStatus, "success": newStatus != "failed"}
		if newStatus == "failed" {
			update["error"] = "configuration rejected by older Agent"
		}
		confirmed := e.record(id, update)
		if confirmed.Status != newStatus || memberPresent(confirmed, "error_details") || memberPresent(confirmed, "rollback_error_details") {
			t.Fatalf("%s: %+v", newStatus, confirmed)
		}
		if newStatus == "failed" && derefStr(confirmed.Error) != update["error"] {
			t.Fatalf("%s: error %q", newStatus, derefStr(confirmed.Error))
		}
	}
}

func TestConfirmedFailureReplacesTransportDetailsAndRetainsOnSameErrorPoll(t *testing.T) {
	e := newEnv(t)
	id := recordTask(e, "unknown")
	e.record(id, map[string]any{"status": "unknown", "error_details": "previous connection refused"})
	details := "config.yaml:18: duplicate plugin"
	failed := e.record(id, map[string]any{"status": "failed", "success": false, "error": "Configuration rejected", "error_details": details})
	if derefStr(failed.ErrorDetails) != details {
		t.Fatalf("failed %+v", failed)
	}
	same := e.record(id, map[string]any{"status": "failed", "success": false, "error": "Configuration rejected"})
	if derefStr(same.ErrorDetails) != details {
		t.Fatalf("same %+v", same)
	}
}

func TestRollbackRetryClearsOldRejectionDetailsBeforeRequest(t *testing.T) {
	e := newEnv(t)
	id := recordTask(e, "succeeded")
	calls := 0
	e.fake.set(func(Request) (*Response, error) {
		calls++
		pending, _ := e.get(e.id()).Deployments.Get(id)
		if pending.Status != "rolling_back" || !deploymentFlagTrue(&pending, "manual_rollback") ||
			derefStr(pending.RollbackErrorDetails) != "" || derefStr(pending.RollbackMessage) != "" {
			t.Errorf("call %d: pending %+v", calls, pending)
		}
		result := map[string]any{"success": false, "message": "Second rejection"}
		if calls == 1 {
			result = map[string]any{"success": false, "message": "First rejection", "error_details": "first checksum mismatch"}
		}
		return reply(result, 409), nil
	})
	must[*model.Deployment](t)(e.m.RollbackDeploymentModel(e.ctx, e.id(), id))
	first, _ := e.get(e.id()).Deployments.Get(id)
	if derefStr(first.RollbackErrorDetails) != "first checksum mismatch" {
		t.Fatalf("first %+v", first)
	}
	must[*model.Deployment](t)(e.m.RollbackDeploymentModel(e.ctx, e.id(), id))
	second, _ := e.get(e.id()).Deployments.Get(id)
	if second.Status != "succeeded" || derefStr(second.RollbackMessage) != "Second rejection" ||
		memberPresent(&second, "rollback_error_details") || calls != 2 {
		t.Fatalf("second %+v (calls %d)", second, calls)
	}
}

func TestDeploymentBusyRetryIsUnchangedByDiagnosticSanitization(t *testing.T) {
	e := newEnv(t)
	answers := []*Response{
		reply(map[string]any{"success": false, "message": "another service operation is in progress"}, 409),
		reply(map[string]any{"success": true, "status": "rolling_back", "manual_rollback": true}, 202),
	}
	calls := 0
	e.fake.set(func(Request) (*Response, error) {
		calls++
		return answers[calls-1], nil
	})
	result := must[*model.Deployment](t)(e.m.DeploymentRequestModel(e.ctx, e.agent, "POST", "deployments/id/rollback", nil, nil, nil))
	if diff := diffJSONValues(t, map[string]any{"success": true, "status": "rolling_back", "manual_rollback": true}, result); diff != "" || calls != 2 {
		t.Fatalf("result %s calls %d", diff, calls)
	}
}

// test_agent_failure_details.py::test_yaml_secret_block_does_not_hide_sibling_diagnostics
func TestYAMLSecretBlockDoesNotHideSiblingDiagnostics(t *testing.T) {
	detail := redact.DiagnosticText("  password: |\n    private-first\n  error: bad proxy name\n", nil, 0)
	if strings.Contains(detail, "private-first") || !strings.Contains(detail, "error: bad proxy name") {
		t.Fatalf("detail %q", detail)
	}
}

// test_agent_deployment_writes.py

// submitted is test_agent_deployment_writes.submitted.
func submitted(e *env, id string) {
	e.begin(id)
	e.record(id, map[string]any{"status": "uploading", "submitted": true, "success": true})
	e.finish(id)
}

func TestRepeatedStatusPollDoesNotRewriteConfiguration(t *testing.T) {
	e := newEnv(t)
	submitted(e, "polled")
	answer := map[string]any{"success": true, "status": "activating", "message": "正在启动", "updated_at": "2026-10-25T01:00:00Z"}
	e.fake.set(func(Request) (*Response, error) { return reply(answer, 200), nil })
	first := e.poll("polled")
	stored := e.system().Agents
	revision := e.revision()
	for i := 0; i < 3; i++ {
		if again := e.poll("polled"); diffJSONValues(t, first, again) != "" {
			t.Fatalf("poll %d: %+v != %+v", i, again, first)
		}
	}
	// Before, each 2-second poll rewrote the whole document to move updated_at.
	if e.revision() != revision || diffJSONValues(t, stored, e.system().Agents) != "" {
		t.Fatal("an unchanged poll wrote the configuration")
	}
	answer["status"] = "checking"
	answer["message"] = "正在检查"
	answer["updated_at"] = "2026-10-25T01:00:05Z"
	moved := e.poll("polled")
	if e.revision() != revision+1 || moved.Status != "checking" || !(derefStr(moved.UpdatedAt) > derefStr(first.UpdatedAt)) {
		t.Fatalf("moved %+v (revision %d → %d)", moved, revision, e.revision())
	}
	events := moved.Events
	if len(events) < 2 || events[len(events)-2].Status != "activating" || events[len(events)-1].Status != "checking" {
		t.Fatalf("events %v", events)
	}
	answer["status"] = "succeeded"
	answer["message"] = ""
	answer["config_version"] = "confirmed"
	if got := e.poll("polled").Status; got != "succeeded" {
		t.Fatalf("final %v", got)
	}
	if got := e.get(e.id()).ConfigVersion; got != "confirmed" {
		t.Fatalf("config_version %v", got)
	}
}

func TestSupersededHistoryKeepsFingerprintButNotPerFileDigests(t *testing.T) {
	e := newEnv(t)
	files := map[string]any{"config.yaml": strings.Repeat("a", 64), "r.list": strings.Repeat("b", 64)}
	prepared := map[string]any{
		"fingerprint": "v1:" + strings.Repeat("0", 64), "profile_id": "default", "service_type": "mihomo",
		"config_path": "config.yaml", "base_url": "", "files": files,
	}
	install := func(id string) {
		submitted(e, id)
		e.editSystem(func(system *model.System) {
			agent := registryAgent(system.Agents, e.id())
			d, _ := agent.Deployments.Get(id)
			_ = d.SetJSON("prepared_content", mustJSON(t, prepared))
			agent.Deployments.Set(id, d)
		})
		e.record(id, map[string]any{"status": "succeeded", "config_version": id})
	}
	install("first")
	stored := e.get(e.id())
	firstDep, _ := stored.Deployments.Get("first")
	if diffJSONValues(t, files, stored.AppliedContent.Files) != "" ||
		diffJSONValues(t, files, firstDep.PreparedContent.Files) != "" {
		t.Fatalf("stored %+v", stored)
	}
	e.begin("second")
	stored = e.get(e.id())
	// The latest task and applied baseline keep the digests that explain drift.
	firstDep, _ = stored.Deployments.Get("first")
	if memberPresent(firstDep.PreparedContent, "files") || firstDep.PreparedContent.Fingerprint != prepared["fingerprint"] ||
		diffJSONValues(t, files, stored.AppliedContent.Files) != "" {
		t.Fatalf("after second %+v", stored)
	}
	e.finish("second")
}

// test_agent_dns_reports.py::test_dns_report_is_bounded_filtered_and_redacted_at_each_boundary
func TestDNSReportIsBoundedFilteredAndRedactedAtEachBoundary(t *testing.T) {
	for _, boundary := range []string{"record", "transport"} {
		e := newEnv(t)
		secret := e.agent.Token
		checks := []any{
			map[string]any{
				"name":       "dns_" + secret + strings.Repeat(" 名称", 100),
				"level":      "warn",
				"message":    "https://name:password@dns.test/query?token=private-token\n" + secret + strings.Repeat("内容", 2000),
				"fix":        "password=\"private-fix\"\n" + strings.Repeat("修复", 1000),
				"unexpected": secret,
			},
			map[string]any{"name": "invalid-level", "level": "unknown", "message": secret},
			"invalid-item",
		}
		for i := 0; i < DNSCheckLimit; i++ {
			checks = append(checks, dnsWarning)
		}
		raw := map[string]any{"status": "ready", "success": true, "dns_checked_at": "not-a-timestamp " + secret, "dns_checks": checks}
		var result *model.Deployment
		if boundary == "record" {
			e.begin("safe-report")
			result = e.record("safe-report", raw)
			e.finish("safe-report")
			if saved := e.stored(e.id()).LatestDeployment; diffJSONValues(t, result.DNSChecks, saved.DNSChecks) != "" {
				t.Fatalf("saved %+v", saved.DNSChecks)
			}
		} else {
			e.fake.set(always(raw, 409))
			result = must[*model.Deployment](t)(e.m.DeploymentRequestModel(e.ctx, e.agent, "POST", "deployments/safe-report/activate", nil, nil, nil))
			if httpStatusOf(result) != 409 {
				t.Fatalf("http_status %v", httpStatusOf(result))
			}
		}
		list := result.DNSChecks
		last := list[len(list)-1]
		if len(list) > DNSCheckLimit || last.Name != "DNS 报告完整性" || last.Level != "warn" {
			t.Fatalf("%s: %d checks, last %+v", boundary, len(list), last)
		}
		var rawChecks []map[string]any
		_ = json.Unmarshal(mustJSON(t, list), &rawChecks)
		for _, item := range rawChecks {
			for key := range item {
				if key != "name" && key != "level" && key != "message" && key != "fix" {
					t.Fatalf("%s: key %s", boundary, key)
				}
			}
		}
		first := list[0]
		if len(first.Name) > DNSNameLimit || len(first.Message) > DNSMessageLimit || len(derefStr(first.Fix)) > DNSFixLimit {
			t.Fatalf("%s: unbounded %+v", boundary, first)
		}
		if memberPresent(result, "dns_checked_at") {
			t.Fatalf("%s: dns_checked_at kept", boundary)
		}
		serialized := string(mustJSON(t, result))
		for _, private := range []string{secret, "name:password", "private-token", "private-fix"} {
			if strings.Contains(serialized, private) {
				t.Fatalf("%s: %q leaked", boundary, private)
			}
		}
	}
}

// test_agent_dns_reports.py::test_malformed_or_absent_report_does_not_erase_previous_result
func TestMalformedOrAbsentReportDoesNotErasePreviousResult(t *testing.T) {
	e := newEnv(t)
	e.begin("keep-report")
	e.record("keep-report", map[string]any{"status": "ready", "dns_checks": []any{dnsWarning}, "dns_checked_at": checkedAt})
	result := e.record("keep-report", map[string]any{"status": "succeeded", "success": true, "dns_checks": "invalid", "dns_checked_at": "invalid"})
	e.finish("keep-report")
	if diffJSONValues(t, []any{dnsWarning}, result.DNSChecks) != "" || derefStr(result.DNSCheckedAt) != checkedAt {
		t.Fatalf("result %+v", result)
	}
}
