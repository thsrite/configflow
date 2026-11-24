package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/oplock"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routers/routerfake"
)

// r12ReviewSetup is one RouterOS router with a pending IPv6 Fake-IP release,
// previewed on its own driver; the returned request publishes that preview.
func r12ReviewSetup(t *testing.T, wrap func(*RouterOS)) (*routerfake.Fake, *repo.Repository, PublishRequest, model.Profile) {
	t.Helper()
	ipv6Hosts := func(context.Context, *GatewayAgent) (ipv6.IPv6Host, error) { return r10Host(), nil }
	f := routerfake.New(nil)
	f.Reset(r11State())
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	r := r10Router()
	r.Endpoint = server.URL
	r.Username = "synthetic-user"
	r.Password = "synthetic-password"
	r.ProxyGatewayAgentID = "gateway"
	r.VerifyTLS = false
	db := r12Repository(t, r, r10YAML)
	cfg, r, before := r12Config(t, db)
	source := r12Source{gateway: r12Gateway()}
	preview := NewRouterOS(server.URL, r.Username, r.Password, false)
	inspection, e := Inspect(t.Context(), InspectRequest{ProfileID: "default", Config: cfg, Router: r, Driver: preview, IPv6Hosts: ipv6Hosts, Agents: source, Repo: db})
	if e != nil {
		t.Fatal(e)
	}
	plan, e := BuildPlan(t.Context(), r, inspection, preview)
	preview.Close()
	if e != nil || len(plan.Blockers) > 0 || plan.Empty {
		t.Fatalf("preview %v %+v", e, plan)
	}
	return f, db, PublishRequest{ProfileID: "default", RouterID: r.ID, Expected: plan.Fingerprint, Router: r, Repo: db, Agents: source, IPv6Hosts: ipv6Hosts,
		NewDriver: func(r *spec.Router) Driver {
			d := NewRouterOS(server.URL, r.Username, r.Password, false)
			if wrap != nil {
				wrap(d)
			}
			return d
		}}, before
}

// Python _capture_sync hands capture_manual the router with the IPv6
// manifests this release published; rule sync compares that record's control
// signature with the stored one and refuses a baseline when they differ.
func TestR12ReviewCaptureSeesPublishedIPv6Manifest(t *testing.T) {
	_, db, req, _ := r12ReviewSetup(t, nil)
	var captured *spec.Router
	req.ManualBaseline = func(_ context.Context, _ string, _ json.RawMessage, r *spec.Router, _ Driver, _ *spec.Desired) error {
		captured = r
		return nil
	}
	outcome := Publish(t.Context(), req)
	if outcome.Err != nil || outcome.Result == nil || !outcome.Result.OK {
		t.Fatalf("publish %+v", outcome)
	}
	_, stored, _ := r12Config(t, db)
	if !effectful(stored.State("ipv6_fakeip_state")) {
		t.Fatal("control: release did not publish an IPv6 Fake-IP manifest")
	}
	if captured == nil || !device.JSONEqual(captured.State("ipv6_fakeip_state"), stored.State("ipv6_fakeip_state")) {
		t.Fatalf("capture saw the pre-release manifest: %s", captured.State("ipv6_fakeip_state"))
	}
	if req.Router.State("ipv6_fakeip_state") != nil {
		t.Fatal("capture overlay mutated the caller's router")
	}
}

// D-R12-3 applies once a device write has started, not only once one is
// proven: a shutdown inside the first write still records the aborted release.
func TestR12ReviewShutdownDuringFirstWriteRecords(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
	runtime.Start(t.Context())
	defer runtime.Shutdown(t.Context())
	var f *routerfake.Fake
	triggered, callsAtShutdown := false, 0
	f, db, req, before := r12ReviewSetup(t, func(d *RouterOS) {
		d.SetHTTP(&r09AfterReadHTTP{DeviceHTTP: d.HTTP(), after: func(method, _ string) {
			if !triggered && method == "PUT" {
				triggered = true
				runtime.Shutdown(t.Context())
				callsAtShutdown = len(f.Calls())
			}
		}})
	})
	outcome := Publish(runtime.BindRequest(t.Context()), req)
	if !triggered {
		t.Fatal("no write reached the device")
	}
	if outcome.Err != nil || outcome.Result == nil || !outcome.Result.Aborted || outcome.Status != PublishFailed || len(outcome.Result.Rollback) > 0 {
		t.Fatalf("aborted write lost its record %+v result %+v", outcome, outcome.Result)
	}
	_, stored, after := r12Config(t, db)
	var last LastRelease
	if e := json.Unmarshal(stored.Extra["last_release"], &last); e != nil || last.OK || !strings.Contains(last.Message, messageReleaseAborted) || after.Revision != before.Revision {
		t.Fatalf("failure record %+v revision %d -> %d", last, before.Revision, after.Revision)
	}
	if len(f.Calls()) != callsAtShutdown {
		t.Fatal("device I/O after shutdown")
	}
}

// Another router sharing the Agent may apply or remove the managed spec after
// this preview; the forward write must not act on a spec it never previewed.
func TestR12ReviewReturnOpsRequirePreviewedAgentSpec(t *testing.T) {
	previewed := json.RawMessage(`{"router_ip":"192.0.2.1","table":104,"priority":104,"mark":"0x400000","safety_version":1,"proxy_dst":["198.18.0.0/16","198.51.100.0/24"]}`)
	reordered := json.RawMessage(`{"router_ip":"192.0.2.1","table":104,"priority":104,"mark":"0x400000","safety_version":1,"proxy_dst":["198.51.100.0/24","198.18.0.0/16"]}`)
	other := json.RawMessage(`{"router_ip":"192.0.2.9","table":104,"priority":104,"mark":"0x400000","safety_version":1,"proxy_dst":["198.18.0.0/16","198.51.100.0/24"]}`)
	spec := &ReturnSpec{RouterIP: "192.0.2.1", Table: 104, Priority: 104, Mark: "0x400000", SafetyVersion: 1, ProxyDst: &[]string{"198.18.0.0/16"}}
	remove := device.JSON(ReturnRemoveOp{device.OpHead{Kind: "return_routing_remove", Summary: "synthetic"}, false, previewed})
	tests := []struct {
		name     string
		op       json.RawMessage
		live     json.RawMessage // nil: the Agent manages nothing
		accepted bool
	}{
		{"remove_previewed", remove, previewed, true},
		{"remove_same_set_other_order", remove, reordered, true},
		{"remove_other_router_spec", remove, other, false},
		{"remove_already_gone", remove, nil, false},
		{"apply_new_previewed_absent", device.JSON(ReturnApplyOp{device.OpHead{Kind: "return_routing_apply", Summary: "synthetic"}, spec, json.RawMessage("null")}), nil, true},
		{"apply_new_but_other_applied", device.JSON(ReturnApplyOp{device.OpHead{Kind: "return_routing_apply", Summary: "synthetic"}, spec, json.RawMessage("null")}), other, false},
		{"apply_replace_previewed", device.JSON(ReturnApplyOp{device.OpHead{Kind: "return_routing_apply", Summary: "synthetic"}, spec, previewed}), previewed, true},
		{"apply_replace_changed", device.JSON(ReturnApplyOp{device.OpHead{Kind: "return_routing_apply", Summary: "synthetic"}, spec, previewed}), other, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &r12Agent{s: r12SafeStatus()}
			if tt.live != nil {
				a.s.Managed = &ReturnManaged{Active: true, TablePresent: true, Spec: tt.live}
			}
			_, err := applyRelease(t.Context(), tt.op, nil, a, false)
			if tt.accepted != (err == nil) || tt.accepted != (len(a.calls) == 1) {
				t.Fatalf("accepted=%v err=%v calls=%s", tt.accepted, err, a.calls)
			}
			if !tt.accepted && err.Error() != messageReturnChanged {
				t.Fatalf("message %q", err)
			}
		})
	}
	// Rollback restores what this release changed; the live spec is its own.
	a := &r12Agent{s: r12SafeStatus()}
	a.s.Managed = &ReturnManaged{Active: true, TablePresent: true, Spec: other}
	undo := json.RawMessage(`{"kind":"return_routing_apply","spec":` + string(previewed) + `,"previous":null}`)
	if _, err := applyRelease(t.Context(), undo, nil, a, true); err != nil || len(a.calls) != 1 {
		t.Fatalf("compensation refused %v %s", err, a.calls)
	}
}

// A panicking owner still delivers one outcome, releases L1/L2 and logs it.
func TestR12ReviewStartPublishPanicIsLogged(t *testing.T) {
	if os.Getenv("R12_REVIEW_PANIC_CHILD") != "1" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestR12ReviewStartPublishPanicIsLogged$", "-test.count=1")
		cmd.Env = append(os.Environ(), "R12_REVIEW_PANIC_CHILD=1")
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated runtime: %v %s", err, raw)
		}
		return
	}
	var logs bytes.Buffer
	if _, e := logx.Setup(logx.Config{Level: logx.Info, Console: &logs}); e != nil {
		t.Fatal(e)
	}
	if e := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); e != nil {
		t.Fatal(e)
	}
	defer lifecycle.Stop(time.Second)
	r := r10Router()
	r.Endpoint = "http://192.0.2.1"
	db := r12Repository(t, r, "{}")
	_, r, _ = r12Config(t, db)
	released := 0
	ch, e := StartPublish(t.Context(), PublishRequest{ProfileID: "default", RouterID: r.ID, Router: r, Repo: db,
		NewDriver: func(*spec.Router) Driver { panic("synthetic-password") }}, func() { released++ })
	if e != nil {
		t.Fatal(e)
	}
	select {
	case outcome := <-ch:
		if !errors.Is(outcome.Err, lifecycle.ErrPanic) || released != 1 {
			t.Fatalf("outcome %+v released=%d", outcome, released)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not finish")
	}
	if _, release, e := oplock.RouterOperationLock(t.Context(), *r, false); e != nil {
		t.Fatal("L2 leaked", e)
	} else {
		release()
	}
	text := logs.String()
	if !strings.Contains(text, "发布任务异常终止") || strings.Contains(text, "synthetic-password") {
		t.Fatalf("panic not logged safely: %q", text)
	}
}

// The explicit null id of a newly planned NAT rule is a release-plan fact
// (Python _grouped_nat_ops); the shared planner check echoes its input rules,
// so GroupRuleCheck's own JSON keeps an absent id absent (R03 corpus).
func TestR12ReviewNewNATRuleNullIDOnlyInPlanView(t *testing.T) {
	rules := []ikuai.GroupRuleCheck{
		{Func: "nat_rule", Comment: "cf-mihomo", Groups: []string{"cf_nat_1"}, KeepForeign: true},
		{Func: "nat_rule", ID: "11", Comment: "cf-mihomo", Groups: []string{"cf_nat_1"}, KeepForeign: true},
		{Func: "stream_ipport", Comment: "cf-mihomo", Groups: []string{"cf_pa_1"}},
	}
	shared := r08Comparable(t, ikuai.GroupStageCheck{Groups: map[string]string{}, Rules: rules, Removed: []string{}}).(map[string]any)
	for i, rule := range shared["rules"].([]any) {
		if _, present := rule.(map[string]any)["id"]; present != (i == 1) {
			t.Fatalf("shared check rule %d id presence %v: %v", i, present, rule)
		}
	}
	checks := Checks{Routes: []device.CIDRGateway{}, RemovedRoutes: []string{}, IkuaiGroups: map[string]ikuai.GroupStageCheck{
		device.StageNATBypass: {Groups: map[string]string{}, Rules: rules[:2], Removed: []string{}},
		device.StageRoutes:    {Groups: map[string]string{}, Rules: rules[2:], Removed: []string{}},
	}}
	view := r08Comparable(t, checks).(map[string]any)["ikuai_groups"].(map[string]any)
	nat := view[device.StageNATBypass].(map[string]any)["rules"].([]any)
	if id, present := nat[0].(map[string]any)["id"]; !present || id != nil {
		t.Fatalf("new NAT rule lost explicit null id: %v", nat[0])
	}
	if nat[1].(map[string]any)["id"] != "11" {
		t.Fatalf("existing NAT rule id changed: %v", nat[1])
	}
	if _, present := view[device.StageRoutes].(map[string]any)["rules"].([]any)[0].(map[string]any)["id"]; present {
		t.Fatal("policy check gained an id")
	}
}
