package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"net/http/httptest"
	"slices"
	"testing"

	"configflow/internal/routers/routerfake"
)

func TestR12PolicyReplacementPython(t *testing.T) {
	for _, mode := range []string{"install", "withdraw", "withdraw_nat_failure", "withdraw_route_failure", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			f := routerfake.New(nil)
			f.Reset(r08State())
			server := httptest.NewServer(f)
			defer server.Close()
			r := r10Router()
			r.Endpoint = "http://192.0.2.1"
			desired := r08Desired(true)
			if mode != "install" && mode != "blocked" {
				seed := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
				plan, e := r08Plan(t.Context(), seed, desired)
				if e != nil || len(plan.Blockers) > 0 {
					t.Fatal(e, plan)
				}
				if _, e = seed.Apply(t.Context(), device.JSON(plan.Ops[0])); e != nil {
					t.Fatal(e)
				}
				r.Extra["ros_policy_state"] = slices.Clone(seed.PolicyManifest())
				seed.Close()
				desired = r08Desired(false)
				desired.ROSPolicy.Previous = r.Extra["ros_policy_state"]
			}
			s := f.Snapshot()
			s.Calls = nil
			if mode == "withdraw_nat_failure" {
				s.Faults = []routerfake.Fault{{Method: "PUT", Path: ros.PathNAT, Count: 1, Status: 403}}
				s.Tables[ros.PathNAT][0]["out-interface"] = "bridge"
			}
			if mode == "withdraw_route_failure" {
				s.Faults = []routerfake.Fault{{Method: "PUT", Path: ros.PathRoutes, Count: 1, Status: 403}}
			}
			if mode == "blocked" {
				s.Tables[ros.PathFilter] = []routerfake.Row{{".id": "*F0", "chain": "forward", "action": "fasttrack-connection"}}
			}
			tc := struct {
				r12BoundaryCase
				Policy bool `json:"policy"`
			}{r12BoundaryCase{Router: r, State: s, Desired: desired, Return: &ReturnStatus{Status: "disabled"}, Execute: true}, true}
			python := r12Python(t, server.URL, tc)
			pythonState := f.Snapshot()
			f.Reset(s)
			d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			d.SetPolicyManifest(r.State("ros_policy_state"))
			state, e := r08InspectState(t.Context(), d)
			if e != nil {
				t.Fatal(e)
			}
			owned, e := rospolicy.OwnedROSPolicyRows(r.State("ros_policy_state"), state.ROSPolicy)
			if e != nil {
				owned = nil
			}
			ids := map[string]bool{}
			for _, v := range owned {
				if v.Path == ros.PathMangle {
					ids[v.ID] = true
				}
			}
			state.NatContext.Mangle = []Row{}
			for _, row := range (*state.ROSPolicy)[ros.PathMangle] {
				if !ids[row.ID()] {
					copy := ros.CloneRow(row)
					delete(copy, "bytes")
					delete(copy, "packets")
					state.NatContext.Mangle = append(state.NatContext.Mangle, copy)
				}
			}
			report, e := BuildReport(desired, state)
			if e != nil {
				t.Fatal(e)
			}
			report.ReturnRouting = tc.Return
			plan, e := BuildPlan(t.Context(), r, &Inspection{Desired: desired, State: state, Report: report}, d)
			if e != nil {
				t.Fatal(e)
			}
			// Running BuildPlan twice must not append preparation summaries or move ops
			// out of the shared feature plan retained in the inspection.
			again, e := BuildPlan(t.Context(), r, &Inspection{Desired: desired, State: state, Report: report}, d)
			if e != nil {
				t.Fatal(e)
			}
			r12Compare(t, "idempotent build", plan, again)
			r12Compare(t, "policy plan", r12PlanComparable(t, python.Plan), r12PlanComparable(t, plan))
			result := Execute(t.Context(), plan, d, nil)
			r12Compare(t, "policy result", python.Result, result)
			r12Compare(t, "policy device tables", pythonState.Tables, f.Snapshot().Tables)
			r12Compare(t, "policy writes", r08Writes(t, pythonState.Calls), r08Writes(t, f.Calls()))
		})
	}
}
