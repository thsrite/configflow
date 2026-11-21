package routers

import (
	"fmt"
	"net/http/httptest"
	"slices"
	"testing"

	"configflow/internal/routers/routerfake"
)

// TestReturnUpstreamOutsideGatewayNetworkBlocksPlan covers lab L5-D2: a
// management address outside the Mihomo gateway's connected network is
// refused at plan time instead of failing in the Agent's apply.
func TestReturnUpstreamOutsideGatewayNetworkBlocksPlan(t *testing.T) {
	blocker := fmt.Sprintf(messageReturnUpstreamNetwork, "203.0.113.1", "192.0.2.0/24", "192.0.2.0/24")
	tests := []struct {
		name     string
		endpoint string
		state    func(*routerfake.State)
		want     bool
	}{
		{"lan-address", "http://192.0.2.1", nil, false},
		{"wan-address", "http://203.0.113.1", nil, true},
		{"no-connected-route", "http://203.0.113.1", func(s *routerfake.State) {
			s.Tables[rosRoutes] = s.Tables[rosRoutes][1:]
		}, false},
		{"inactive-connected-route", "http://203.0.113.1", func(s *routerfake.State) {
			s.Tables[rosRoutes][0]["active"] = "false"
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := r08State()
			if tt.state != nil {
				tt.state(&state)
			}
			f := routerfake.New(nil)
			f.Reset(state)
			srv := httptest.NewServer(f)
			defer srv.Close()
			d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			s, e := d.ReadState(t.Context())
			if e != nil {
				t.Fatal(e)
			}
			desired := r12Desired()
			report, e := BuildReport(desired, s)
			if e != nil {
				t.Fatal(e)
			}
			report.ReturnRouting = r12SafeStatus()
			r := &Router{RouterSettings: RouterSettings{ID: "r1", Type: "routeros", Endpoint: tt.endpoint, ManageReturnRouting: true}}
			plan, e := BuildPlan(t.Context(), r, &Inspection{Desired: desired, State: s, Report: report}, d)
			if e != nil {
				t.Fatal(e)
			}
			if got := slices.ContainsFunc(plan.Blockers, func(b string) bool { return b == blocker }); got != tt.want {
				t.Fatalf("blockers %q, want upstream blocker %v", plan.Blockers, tt.want)
			}
			if tt.want && len(plan.Stages) != 0 {
				t.Fatalf("blocked plan has stages: %+v", plan.Stages)
			}
		})
	}
}
