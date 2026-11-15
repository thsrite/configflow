package agents

// Ports of backend/test_agent_failure_response_states.py (the manager cases;
// test_immediate_rejections_are_redacted_like_recorded_history calls the
// deployment routes and is deferred to A14), the remaining pure case of
// test_agent_failure_details.py and backend/test_agent_deployment_writes.py.

import (
	"net/http"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// recordTask is test_agent_failure_response_states._record_task.
func recordTask(e *env, status string) string {
	id := "diagnostic-state"
	e.begin(id)
	e.record(id, jsonx.Obj("status", status, "success", true, "submitted", true))
	e.finish(id)
	return id
}

func legacyCall(e *env, method string) *jsonx.Object {
	e.t.Helper()
	if method == "push" {
		return must[*jsonx.Object](e.t)(e.m.PushConfigToAgent(e.ctx, e.id(), "[General]", PushConfig{}))
	}
	return must[*jsonx.Object](e.t)(e.m.RestartAgentService(e.ctx, e.id()))
}

func TestLegacySuccessHTTPRequiresAJSONObject(t *testing.T) {
	for _, method := range []string{"push", "restart"} {
		for _, payload := range []any{nil, []any{}, "unexpected text", jsonx.Int(17)} {
			e := newEnv(t)
			e.editSystem(func(system *jsonx.Object) { e.firstAgent(system).Set("service_type", "surge") })
			before := e.get(e.id())
			e.fake.set(always(payload, 200))
			result := legacyCall(e, method)
			if !jsonx.Equal(result, jsonx.Obj("success", false, "message", "Invalid Agent response")) {
				t.Fatalf("%s %v: %v", method, payload, result)
			}
			saved := e.get(e.id())
			if !jsonx.Equal(saved.Value("config_version"), before.Value("config_version")) ||
				!jsonx.Equal(saved.Value("pushed_at"), before.Value("pushed_at")) {
				t.Fatalf("%s %v: saved %v", method, payload, saved)
			}
		}
	}
}

func TestLegacyInvalidJSONKeepsSafePlainTextExplanation(t *testing.T) {
	for _, method := range []string{"push", "restart"} {
		e := newEnv(t)
		e.editSystem(func(system *jsonx.Object) { e.firstAgent(system).Set("service_type", "surge") })
		e.fake.set(func(Request) (*Response, error) {
			return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/plain"}},
				Body: []byte("upstream service unavailable\npassword=do-not-display")}, nil
		})
		result := legacyCall(e, method)
		details := result.GetString("error_details")
		if result.Value("success") != false || !strings.Contains(details, "upstream service unavailable") || strings.Contains(details, "do-not-display") {
			t.Fatalf("%s: %v", method, result)
		}
	}
}

func TestConfirmedAgentStageDiscardsOldUnknownTransportDetails(t *testing.T) {
	for _, newStatus := range []string{"verifying", "checking", "failed", "rolling_back"} {
		e := newEnv(t)
		id := recordTask(e, "unknown")
		e.record(id, jsonx.Obj("status", "unknown", "success", true, "error_details", "previous connection refused",
			"rollback_error_details", "previous connection reset"))
		update := jsonx.Obj("status", newStatus, "success", newStatus != "failed")
		if newStatus == "failed" {
			update.Set("error", "configuration rejected by older Agent")
		}
		confirmed := e.record(id, update)
		if confirmed.Value("status") != newStatus || confirmed.Has("error_details") || confirmed.Has("rollback_error_details") {
			t.Fatalf("%s: %v", newStatus, confirmed)
		}
		if newStatus == "failed" && confirmed.Value("error") != update.Value("error") {
			t.Fatalf("%s: error %v", newStatus, confirmed.Value("error"))
		}
	}
}

func TestConfirmedFailureReplacesTransportDetailsAndRetainsOnSameErrorPoll(t *testing.T) {
	e := newEnv(t)
	id := recordTask(e, "unknown")
	e.record(id, jsonx.Obj("status", "unknown", "error_details", "previous connection refused"))
	details := "config.yaml:18: duplicate plugin"
	failed := e.record(id, jsonx.Obj("status", "failed", "success", false, "error", "Configuration rejected", "error_details", details))
	if failed.Value("error_details") != details {
		t.Fatalf("failed %v", failed)
	}
	same := e.record(id, jsonx.Obj("status", "failed", "success", false, "error", "Configuration rejected"))
	if same.Value("error_details") != details {
		t.Fatalf("same %v", same)
	}
}

func TestRollbackRetryClearsOldRejectionDetailsBeforeRequest(t *testing.T) {
	e := newEnv(t)
	id := recordTask(e, "succeeded")
	calls := 0
	e.fake.set(func(Request) (*Response, error) {
		calls++
		pending := e.get(e.id()).GetObject("deployments").GetObject(id)
		if pending.Value("status") != "rolling_back" || pending.Value("manual_rollback") != true ||
			jsonx.Truthy(pending.Value("rollback_error_details")) || jsonx.Truthy(pending.Value("rollback_message")) {
			t.Errorf("call %d: pending %v", calls, pending)
		}
		result := jsonx.Obj("success", false, "message", "Second rejection")
		if calls == 1 {
			result = jsonx.Obj("success", false, "message", "First rejection", "error_details", "first checksum mismatch")
		}
		return reply(result, 409), nil
	})
	must[*jsonx.Object](t)(e.m.RollbackDeployment(e.ctx, e.id(), id))
	first := e.get(e.id()).GetObject("deployments").GetObject(id)
	if first.Value("rollback_error_details") != "first checksum mismatch" {
		t.Fatalf("first %v", first)
	}
	must[*jsonx.Object](t)(e.m.RollbackDeployment(e.ctx, e.id(), id))
	second := e.get(e.id()).GetObject("deployments").GetObject(id)
	if second.Value("status") != "succeeded" || second.Value("rollback_message") != "Second rejection" ||
		second.Has("rollback_error_details") || calls != 2 {
		t.Fatalf("second %v (calls %d)", second, calls)
	}
}

func TestDeploymentBusyRetryIsUnchangedByDiagnosticSanitization(t *testing.T) {
	e := newEnv(t)
	answers := []*Response{
		reply(jsonx.Obj("success", false, "message", "another service operation is in progress"), 409),
		reply(jsonx.Obj("success", true, "status", "rolling_back", "manual_rollback", true), 202),
	}
	calls := 0
	e.fake.set(func(Request) (*Response, error) {
		calls++
		return answers[calls-1], nil
	})
	result := must[*jsonx.Object](t)(e.m.DeploymentRequest(e.ctx, e.agent, "POST", "deployments/id/rollback", nil, nil, nil))
	if !jsonx.Equal(result, jsonx.Obj("success", true, "status", "rolling_back", "manual_rollback", true)) || calls != 2 {
		t.Fatalf("result %v calls %d", result, calls)
	}
}

// test_agent_failure_details.py::test_yaml_secret_block_does_not_hide_sibling_diagnostics
func TestYAMLSecretBlockDoesNotHideSiblingDiagnostics(t *testing.T) {
	detail := FailureText("  password: |\n    private-first\n  error: bad proxy name\n", nil, 0)
	if strings.Contains(detail, "private-first") || !strings.Contains(detail, "error: bad proxy name") {
		t.Fatalf("detail %q", detail)
	}
}

// test_agent_deployment_writes.py

// submitted is test_agent_deployment_writes.submitted.
func submitted(e *env, id string) {
	e.begin(id)
	e.record(id, jsonx.Obj("status", "uploading", "submitted", true, "success", true))
	e.finish(id)
}

func TestRepeatedStatusPollDoesNotRewriteConfiguration(t *testing.T) {
	e := newEnv(t)
	submitted(e, "polled")
	answer := jsonx.Obj("success", true, "status", "activating", "message", "正在启动", "updated_at", "2026-10-25T01:00:00Z")
	e.fake.set(func(Request) (*Response, error) { return reply(answer, 200), nil })
	first := e.poll("polled")
	stored := jsonx.DeepCopy(e.system().GetList("agents"))
	revision := e.revision()
	for i := 0; i < 3; i++ {
		if again := e.poll("polled"); !jsonx.Equal(again, first) {
			t.Fatalf("poll %d: %v != %v", i, again, first)
		}
	}
	// Before, each 2-second poll rewrote the whole document to move updated_at.
	if e.revision() != revision || !jsonx.Equal(e.system().GetList("agents"), stored) {
		t.Fatal("an unchanged poll wrote the configuration")
	}
	answer.Update(jsonx.Obj("status", "checking", "message", "正在检查", "updated_at", "2026-10-25T01:00:05Z"))
	moved := e.poll("polled")
	if e.revision() != revision+1 || moved.Value("status") != "checking" || !(moved.GetString("updated_at") > first.GetString("updated_at")) {
		t.Fatalf("moved %v (revision %d → %d)", moved, revision, e.revision())
	}
	events := moved.GetList("events")
	if len(events) < 2 || events[len(events)-2].(*jsonx.Object).Value("status") != "activating" ||
		events[len(events)-1].(*jsonx.Object).Value("status") != "checking" {
		t.Fatalf("events %v", events)
	}
	answer.Update(jsonx.Obj("status", "succeeded", "message", "", "config_version", "confirmed"))
	if got := e.poll("polled").Value("status"); got != "succeeded" {
		t.Fatalf("final %v", got)
	}
	if got := e.get(e.id()).Value("config_version"); got != "confirmed" {
		t.Fatalf("config_version %v", got)
	}
}

func TestSupersededHistoryKeepsFingerprintButNotPerFileDigests(t *testing.T) {
	e := newEnv(t)
	files := jsonx.Obj("config.yaml", strings.Repeat("a", 64), "r.list", strings.Repeat("b", 64))
	prepared := jsonx.Obj("fingerprint", "v1:"+strings.Repeat("0", 64), "profile_id", "default", "service_type", "mihomo",
		"config_path", "config.yaml", "base_url", "", "files", files)
	install := func(id string) {
		submitted(e, id)
		e.editSystem(func(system *jsonx.Object) {
			agent, _ := findAgent(system.GetList("agents"), e.id())
			agent.GetObject("deployments").GetObject(id).Set("prepared_content", prepared.DeepCopy())
		})
		e.record(id, jsonx.Obj("status", "succeeded", "config_version", id))
	}
	install("first")
	stored := e.get(e.id())
	if !jsonx.Equal(stored.GetObject("applied_content").Value("files"), files) ||
		!jsonx.Equal(stored.GetObject("deployments").GetObject("first").GetObject("prepared_content").Value("files"), files) {
		t.Fatalf("stored %v", stored)
	}
	e.begin("second")
	stored = e.get(e.id())
	// The latest task and applied baseline keep the digests that explain drift.
	first := stored.GetObject("deployments").GetObject("first").GetObject("prepared_content")
	if first.Has("files") || first.Value("fingerprint") != prepared.Value("fingerprint") ||
		!jsonx.Equal(stored.GetObject("applied_content").Value("files"), files) {
		t.Fatalf("after second %v", stored)
	}
	e.finish("second")
}

// test_agent_dns_reports.py::test_dns_report_is_bounded_filtered_and_redacted_at_each_boundary
func TestDNSReportIsBoundedFilteredAndRedactedAtEachBoundary(t *testing.T) {
	for _, boundary := range []string{"record", "transport"} {
		e := newEnv(t)
		secret := e.agent.GetString("token")
		checks := []any{
			jsonx.Obj("name", "dns_"+secret+strings.Repeat(" 名称", 100), "level", "warn",
				"message", "https://name:password@dns.test/query?token=private-token\n"+secret+strings.Repeat("内容", 2000),
				"fix", "password=\"private-fix\"\n"+strings.Repeat("修复", 1000), "unexpected", secret),
			jsonx.Obj("name", "invalid-level", "level", "unknown", "message", secret),
			"invalid-item",
		}
		for i := 0; i < DNSCheckLimit; i++ {
			checks = append(checks, dnsWarning.DeepCopy())
		}
		raw := jsonx.Obj("status", "ready", "success", true, "dns_checked_at", "not-a-timestamp "+secret, "dns_checks", checks)
		var result *jsonx.Object
		if boundary == "record" {
			e.begin("safe-report")
			result = e.record("safe-report", raw)
			e.finish("safe-report")
			if saved := e.stored(e.id()).GetObject("latest_deployment"); !jsonx.Equal(saved.Value("dns_checks"), result.Value("dns_checks")) {
				t.Fatalf("saved %v", saved.Value("dns_checks"))
			}
		} else {
			e.fake.set(always(raw, 409))
			result = must[*jsonx.Object](t)(e.m.DeploymentRequest(e.ctx, e.agent, "POST", "deployments/safe-report/activate", nil, nil, nil))
			if !jsonx.Equal(result.Value("http_status"), 409) {
				t.Fatalf("http_status %v", result.Value("http_status"))
			}
		}
		list := result.GetList("dns_checks")
		last := list[len(list)-1].(*jsonx.Object)
		if len(list) > DNSCheckLimit || last.Value("name") != "DNS 报告完整性" || last.Value("level") != "warn" {
			t.Fatalf("%s: %d checks, last %v", boundary, len(list), last)
		}
		for _, item := range list {
			for _, key := range item.(*jsonx.Object).Keys() {
				if key != "name" && key != "level" && key != "message" && key != "fix" {
					t.Fatalf("%s: key %s", boundary, key)
				}
			}
		}
		first := list[0].(*jsonx.Object)
		if len(first.GetString("name")) > DNSNameLimit || len(first.GetString("message")) > DNSMessageLimit || len(first.GetString("fix")) > DNSFixLimit {
			t.Fatalf("%s: unbounded %v", boundary, first)
		}
		if result.Has("dns_checked_at") {
			t.Fatalf("%s: dns_checked_at kept", boundary)
		}
		serialized, _ := jsonx.Default(result)
		for _, private := range []string{secret, "name:password", "private-token", "private-fix"} {
			if strings.Contains(string(serialized), private) {
				t.Fatalf("%s: %q leaked", boundary, private)
			}
		}
	}
}

// test_agent_dns_reports.py::test_malformed_or_absent_report_does_not_erase_previous_result
func TestMalformedOrAbsentReportDoesNotErasePreviousResult(t *testing.T) {
	e := newEnv(t)
	e.begin("keep-report")
	e.record("keep-report", jsonx.Obj("status", "ready", "dns_checks", []any{dnsWarning}, "dns_checked_at", checkedAt))
	result := e.record("keep-report", jsonx.Obj("status", "succeeded", "success", true, "dns_checks", "invalid", "dns_checked_at", "invalid"))
	e.finish("keep-report")
	if !jsonx.Equal(result.Value("dns_checks"), []any{dnsWarning}) || result.Value("dns_checked_at") != checkedAt {
		t.Fatalf("result %v", result)
	}
}
