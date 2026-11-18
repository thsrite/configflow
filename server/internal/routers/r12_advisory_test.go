package routers

import (
	"net/http/httptest"
	"testing"

	"configflow/internal/routers/routerfake"
)

func TestR12AdvisoryWholePlansPython(t *testing.T) {
	invalid := routerfake.Row{".id": "*F0", "chain": "forward", "action": "drop", "connection-state": "invalid", "comment": "synthetic state firewall"}
	established := routerfake.Row{".id": "*F1", "chain": "forward", "action": "accept", "connection-state": "established,related"}
	cases := []struct {
		name string
		rows []routerfake.Row
		sets map[string][]string
	}{{"empty", nil, nil}, {"explicit", []routerfake.Row{invalid}, nil}, {"remaining", []routerfake.Row{established, {".id": "*F2", "chain": "forward", "action": "drop"}}, nil}, {"fasttrack", []routerfake.Row{{"chain": "forward", "action": "fasttrack-connection", "connection-state": "established,related"}, established}, nil}, {"shadowed", []routerfake.Row{{"chain": "forward", "action": "accept"}, invalid}, nil}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := r08State()
			s.Tables[rosRoutes] = append(s.Tables[rosRoutes], routerfake.Row{".id": "*9", "dst-address": "198.51.100.0/25", "gateway": "192.0.2.4", "routing-table": "main", "active": "true", "disabled": "false", "comment": "cf"})
			s.Tables[rosFilter] = tc.rows
			s.Tables["/interface/list"] = []routerfake.Row{{"name": "LAN"}, {"name": "WAN"}}
			s.Tables["/interface/list/member"] = []routerfake.Row{{"list": "LAN", "interface": "bridge"}, {"list": "WAN", "interface": "wan"}}
			f := routerfake.New(nil)
			server := httptest.NewServer(f)
			defer server.Close()
			input := struct {
				r12BoundaryCase
				Sets map[string][]string `json:"address_sets"`
			}{r12BoundaryCase{Router: &Router{RouterSettings: RouterSettings{Type: "routeros"}}, State: s, Desired: r12Desired(), Return: &ReturnStatus{Status: "disabled"}}, tc.sets}
			py := r12Python(t, server.URL, input)
			f.Reset(s)
			d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			state, e := d.ReadState(t.Context())
			if e != nil {
				t.Fatal(e)
			}
			for k, v := range tc.sets {
				state.AddressSets[k] = v
			}
			before := mustPolicyJSON(state.NatContext)
			report, e := BuildReport(input.Desired, state)
			if e != nil {
				t.Fatal(e)
			}
			report.ReturnRouting = input.Return
			plan, e := BuildPlan(t.Context(), input.Router, &Inspection{Desired: input.Desired, State: state, Report: report}, d)
			if e != nil {
				t.Fatal(e)
			}
			r12Compare(t, "advisory plan", r12PlanComparable(t, py.Plan), r12PlanComparable(t, plan))
			if !jsonEqual(before, mustPolicyJSON(state.NatContext)) {
				t.Fatal("advisory mutated routing evidence")
			}
		})
	}
}
