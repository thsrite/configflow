package routers

import (
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestR12AllocationStrictness(t *testing.T) {
	for _, field := range []string{"version", "table", "priority", "reused", "mark"} {
		for _, value := range []string{"null", "true", "false", "\"1\"", "1.0", "-1", "{}", "[]"} {
			t.Run(field+"="+value, func(t *testing.T) {
				s := r12SafeStatus()
				v := returnSafety(s)
				var allocation map[string]json.RawMessage
				if e := json.Unmarshal(v["allocation"], &allocation); e != nil {
					t.Fatal(e)
				}
				allocation[field] = json.RawMessage(value)
				v["allocation"] = device.JSON(allocation)
				s.Safety = device.JSON(v)
				valid := field == "reused" && (value == "true" || value == "false")
				if got := ReturnAllocationOf(s) != nil; got != valid {
					t.Fatalf("allocation accepted=%v want %v", got, valid)
				}
			})
		}
	}
	for _, mark := range []string{"0x0", "0x3", "0x100000000", "4194304", " 0x4", "0x4/0x4"} {
		s := r12SafeStatus()
		s.Safety = json.RawMessage(strings.ReplaceAll(string(s.Safety), "0x400000", mark))
		if ReturnAllocationOf(s) != nil {
			t.Fatal(mark)
		}
	}
	for _, protocols := range []string{`["2"]`, `[true]`, `{}`, `null`, `[]`} {
		s := r12SafeStatus()
		v := returnSafety(s)
		v["protocols"] = json.RawMessage(protocols)
		s.Safety = device.JSON(v)
		if ReturnProtocol(s) != 1 {
			t.Fatal("invented protocol", protocols)
		}
	}
	s := r12SafeStatus()
	s.Safety = json.RawMessage(strings.ReplaceAll(string(s.Safety), "0x400000", "0X00400000"))
	if a := ReturnAllocationOf(s); a == nil || a.Mark != "0x400000" {
		t.Fatalf("mark normalization %+v", a)
	}
}
func TestR12LostAgentRepliesAreCompensatedAndConfirmed(t *testing.T) {
	for _, previous := range []bool{false, true} {
		t.Run(fmt.Sprint(previous), func(t *testing.T) {
			a := &r12Agent{s: r12SafeStatus(), errorAfterApply: true, errorAfterRemove: true}
			want := &ReturnSpec{RouterIP: "192.0.2.1", Table: 104, Priority: 104, Mark: "0x400000", SafetyVersion: 2}
			v := returnSafety(a.s)
			v["protocols"] = json.RawMessage(`[1,2]`)
			a.s.Safety = device.JSON(v)
			old := json.RawMessage("null")
			if previous {
				old = device.JSON(map[string]any{"router_ip": "192.0.2.1", "table": 104, "priority": 104, "mark": "0x400000", "safety_version": 1, "proxy_dst": []string{"198.18.0.0/16"}, "lan_interface": "synthetic-lan", "tun_device": "synthetic-tun"})
				a.s.Managed = &ReturnManaged{TablePresent: true, Spec: old}
			}
			undo, err := applyRelease(t.Context(), device.JSON(ReturnApplyOp{OpHead: device.OpHead{Kind: "return_routing_apply"}, Spec: want, Previous: old}), nil, a, false)
			if err == nil || effectful(undo) || !effectful(device.UndoOf(err)) {
				t.Fatalf("lost reply missing compensation %s %v", undo, err)
			}
			_, err = applyRelease(t.Context(), device.UndoOf(err), nil, a, true)
			if err != nil {
				t.Fatal("compensation GET did not prove success", err)
			}
			if previous {
				if a.s.Managed == nil || !device.JSONEqual(a.s.Managed.Spec, old) {
					t.Fatal("previous raw spec lost")
				}
			} else if a.s.Managed != nil {
				t.Fatal("owned create not cleaned")
			}
		})
	}
}
func TestR12ReturnStatusPreservesUnknownAndPresence(t *testing.T) {
	raw := json.RawMessage(`{"success":true,"managed":{"table_present":true,"spec":{"safety_version":2,"proxy_dst":null},"unknown":{"x":9007199254740993}},"safety":{},"fwmark_rules":[],"future":[null,1.0,"x"]}`)
	var s ReturnStatus
	if e := json.Unmarshal(raw, &s); e != nil {
		t.Fatal(e)
	}
	s.Status = "ok"
	out := r08Comparable(t, s).(map[string]any)
	original := r08Comparable(t, raw).(map[string]any)
	delete(original, "success")
	original["status"] = "ok"
	r12Compare(t, "Agent body", original, out)
}
func TestR12FingerprintCanonicalEvidence(t *testing.T) {
	stages := []device.Stage{stageOf(device.StageRoutes, false, []device.Op{RouteAddOp{device.OpHead{Kind: "route_add", Summary: "synthetic"}, "198.18.0.0/16", "192.0.2.4", "cf"}})}
	base, err := Fingerprint(stages, json.RawMessage(`{"z":2,"a":{"b":3,"a":1}}`), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	equivalent, err := Fingerprint(stages, json.RawMessage(`{"a":{"a":1,"b":3},"z":2}`), nil, nil)
	if err != nil || base != equivalent {
		t.Fatal("map order unstable")
	}
	for _, leaf := range []string{`{"z":3,"a":{"b":3,"a":1}}`, `{"z":2,"a":{"b":4,"a":1}}`, `{"z":2,"a":{"b":3,"a":2}}`} {
		next, e := Fingerprint(stages, json.RawMessage(leaf), nil, nil)
		if e != nil || base == next {
			t.Fatal("fingerprint missed evidence leaf", leaf)
		}
	}
	if _, err = Fingerprint(stages, json.RawMessage(`{broken`), nil, nil); err == nil {
		t.Fatal("invalid evidence accepted")
	}
}
func TestR12ExecuteDisabledAndEmptyContracts(t *testing.T) {
	d, _ := r11Driver(t)
	disabled := stageOf(device.StageRoutes, false, []device.Op{RouteAddOp{device.OpHead{Kind: "route_add", Summary: "synthetic"}, "198.18.0.0/16", "192.0.2.4", "cf"}})
	disabled.Enabled = pointer(false)
	got := Execute(t.Context(), &Plan{Stages: []device.Stage{disabled}}, d, nil)
	if got.OK || got.Log[0].Summary != messageDisabledWrites {
		t.Fatalf("disabled writes %+v", got)
	}
	failed := Execute(t.Context(), &Plan{Blockers: []string{"synthetic"}}, d, nil)
	if failed.OK || failed.FailedStage == nil || *failed.FailedStage != "" {
		t.Fatal("early failure presence")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := Execute(ctx, &Plan{Stages: []device.Stage{}}, d, nil); !got.OK {
		t.Fatal("outside Runtime detach should finish")
	}
}

func TestR12ReturnStatusClearDoesNotResurrectSnapshot(t *testing.T) {
	var s ReturnStatus
	if e := json.Unmarshal([]byte(`{"managed":{"table_present":true,"spec":{"table":104}},"fwmark_rules":[{"table":104}]}`), &s); e != nil {
		t.Fatal(e)
	}
	s.Managed = nil
	s.FWMarkRules = []json.RawMessage{}
	out := r08Comparable(t, s).(map[string]any)
	if out["managed"] != nil || len(out["fwmark_rules"].([]any)) != 0 {
		t.Fatalf("resurrected retired state %+v", out)
	}
}

func TestR12LostReplyProofRejectsUnknownSpecChanges(t *testing.T) {
	expected := json.RawMessage(`{"router_ip":"192.0.2.1","table":104,"lan_interface":"synthetic-lan"}`)
	if !returnSpecProof(expected, expected) {
		t.Fatal("proof control did not match")
	}
	changed := json.RawMessage(`{"router_ip":"192.0.2.1","table":104,"lan_interface":"other-lan"}`)
	extra := json.RawMessage(`{"router_ip":"192.0.2.1","table":104,"lan_interface":"synthetic-lan","future":true}`)
	if returnSpecProof(changed, expected) || returnSpecProof(extra, expected) {
		t.Fatal("GET accepted external/unknown spec change as a successful compensation")
	}
}
