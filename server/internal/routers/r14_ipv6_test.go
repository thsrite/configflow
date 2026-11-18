package routers

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"configflow/internal/routers/routerfake"
)

func TestR14IPv6DeltaPython(t *testing.T) {
	for _, mode := range []string{"delta", "failed_write", "enrollment", "next_hop", "unclear"} {
		t.Run(mode, func(t *testing.T) {
			seed := r08State()
			for key, rows := range r10State().Tables {
				seed.Tables[key] = rows
			}
			seed.IPv6Wire = true
			f := routerfake.New(nil)
			f.Reset(seed)
			httpServer := httptest.NewServer(f)
			defer httpServer.Close()
			d := NewRouterOS(httpServer.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			r := r10Router()
			r.ID = "r-sync"
			config := r10Config("ipv6: true\ntun: {enable: true}\n")
			agent := r10Agent()
			host := r10Host()
			old := r08Desired(false)
			old.IPv6CIDRs = []ProxyCIDR{{"2001:db8:10::/48", []string{"Synthetic"}}, {"2001:db8:20::/48", []string{"Synthetic"}}}
			if mode == "enrollment" {
				old.IPv6CIDRs = []ProxyCIDR{}
			}
			plan := r14Plan(t, t.Context(), d, r, old)
			if result := Execute(t.Context(), plan, d, nil); !result.OK {
				t.Fatalf("initial IPv4 %+v", result)
			}
			if mode != "enrollment" {
				v := InspectIPv6Routes(t.Context(), config, r, old, agent, host, d)
				if len(v.Ops) != 1 {
					t.Fatalf("IPv6 enrollment %+v", v)
				}
				if _, err := d.Apply(t.Context(), mustPolicyJSON(v.Ops[0])); err != nil {
					t.Fatal(err)
				}
				r.Extra["ipv6_routes_state"] = slices.Clone(d.ipv6RoutesManifest)
			}
			static, err := syncStaticResources(t.Context(), d, old)
			if err != nil {
				t.Fatal(err)
			}
			base := &RuleSyncBaseline{Control: "synthetic", Desired: old, Rules: map[string]*RuleSyncRecord{}, StaticResources: static, Policy: json.RawMessage("null")}
			next := r08Desired(false)
			next.ProxyCIDRs[1].CIDR = "203.0.113.0/24"
			next.IPv6CIDRs = []ProxyCIDR{{"2001:db8:10::/48", []string{"Synthetic"}}, {"2001:db8:30::/48", []string{"Synthetic"}}}
			if mode == "next_hop" {
				host.Addresses[1].Address = "fe80::9"
			}
			if mode == "unclear" {
				host = IPv6Host{Error: "synthetic offline"}
			}
			seed = f.Snapshot()
			seed.Calls = nil
			if mode == "failed_write" {
				seed.Faults = []routerfake.Fault{{Method: "PUT", Path: rosIPv6Routes, Count: 1, Status: 403}}
			}
			input := map[string]any{"action": "delta", "ipv6": true, "config": config, "router": r, "desired": next, "agent": agent, "host": host, "baseline": base, "state": seed}
			var expected struct {
				Value    *RuleSyncDelta  `json:"value"`
				Error    string          `json:"error"`
				Note     string          `json:"note"`
				Manifest json.RawMessage `json:"ipv6_manifest"`
			}
			r14Python(t, httpServer.URL, input, &expected)
			python := f.Snapshot()
			f.Reset(seed)
			d.Close()
			d = NewRouterOS(httpServer.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			plan = r14Plan(t, t.Context(), d, r, next)
			v := InspectIPv6Routes(t.Context(), config, r, next, agent, host, d)
			plan.Stages = append(plan.Stages, Stage{Key: StageIPv6Routes, Title: StageTitles[StageIPv6Routes], Ops: v.Ops})
			plan.Checks.IPv6Routes = v.Check
			note := syncIPv6Delta(&RouterState{IPv6RoutesPlan: v}, plan)
			if note != expected.Note {
				t.Fatalf("IPv6 eligibility %s expected %s", note, expected.Note)
			}
			result, err := ApplyRuleSyncDelta(t.Context(), d, next, plan, base, nil)
			if expected.Error != "" {
				if err == nil || err.Error() != expected.Error {
					t.Fatalf("IPv6 error %v expected %s", err, expected.Error)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				r12Compare(t, "IPv6 delta", result, expected.Value)
			}
			r12Compare(t, "IPv6 and IPv4 state", f.Snapshot().Tables, python.Tables)
			r12Compare(t, "IPv6 ownership", syncNull(d.ipv6RoutesManifest), syncNull(expected.Manifest))
			r12Compare(t, "IPv6 request effects", r08Writes(t, f.Calls()), r08Writes(t, python.Calls))
		})
	}
}
