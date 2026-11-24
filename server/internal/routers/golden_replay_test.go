package routers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/routerfake"
	"configflow/internal/routers/spec"
	"configflow/internal/testgolden"
)

func goldenMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func r13Stage(key string, kinds ...string) device.Stage {
	s := stageOf(key, false, []device.Op{})
	for _, kind := range kinds {
		s.Ops = append(s.Ops, device.OpHead{Kind: kind, Summary: "synthetic"})
	}
	return s
}

// TestRoutersBoundaryGoldenReplay records and replays deterministic Go-native
// boundary outputs for R12 (whole-plan + return routing) and R13 (pending
// summary projection and cache).
func TestRoutersBoundaryGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	// R12: Whole-plan building & desired delta on RouterOS (with and without return routing).
	t.Run("R12_whole_plan", func(t *testing.T) {
		f := routerfake.New(nil)
		f.Reset(r08State())
		srv := httptest.NewServer(f)
		defer srv.Close()
		d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-pass", false)
		defer d.Close()
		r := &spec.Router{RouterSettings: spec.RouterSettings{ID: "r12-golden", Type: "routeros", Endpoint: srv.URL}}
		plan := r14Plan(t, context.Background(), d, r, r08Desired(true))
		ref.Assert(t, []byte(`{"module":"R12","case":"whole_plan"}`), goldenMarshal(t, r12PlanComparable(t, plan)))
	})

	// R12/R06: Return-routing plan & advisory reconciliation.
	t.Run("R12_return_routing_plan", func(t *testing.T) {
		f := routerfake.New(nil)
		f.Reset(r08State())
		srv := httptest.NewServer(f)
		defer srv.Close()
		d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-pass", false)
		defer d.Close()

		state, err := r08InspectState(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		desired := r12Desired()
		report, err := BuildReport(desired, state)
		if err != nil {
			t.Fatal(err)
		}
		report.ReturnRouting = r12SafeStatus()
		r := &spec.Router{RouterSettings: spec.RouterSettings{
			ID:                  "r12-return-golden",
			Type:                "routeros",
			Endpoint:            srv.URL,
			ManageReturnRouting: true,
		}}
		plan, err := BuildPlan(context.Background(), r, &Inspection{Desired: desired, State: state, Report: report}, d)
		if err != nil {
			t.Fatal(err)
		}
		ref.Assert(t, []byte(`{"module":"R12","case":"return_routing_plan"}`), goldenMarshal(t, r12PlanComparable(t, plan)))
	})

	// R13: Pending summary projection across all stage/report shapes.
	t.Run("R13_summaries", func(t *testing.T) {
		base := func() *Report { return &Report{InSync: true} }
		all := base()
		all.InSync = false
		all.Parts = []device.ReportPart{
			{Key: "ipv6_fakeip", Section: ipv6.IPv6FakeIPReport{Enabled: true, Status: "missing"}},
			{Key: "ipv6_routes", Section: ipv6.IPv6RoutesReport{Enabled: true, Items: []ipv6.IPv6RouteItem{{Status: "missing"}, {Status: "borrowed"}}}},
			{Key: "ipv6_pd_guard", Section: ipv6.IPv6PDGuardReport{Enabled: true, Items: []ipv6.IPv6PDItem{{Status: "missing"}, {Status: "synced"}}}},
		}
		withdraw := base()
		withdraw.Parts = []device.ReportPart{
			{Key: "ipv6_fakeip", Section: ipv6.IPv6FakeIPReport{Enabled: false}},
			{Key: "ipv6_routes", Section: ipv6.IPv6RoutesReport{Enabled: false}},
			{Key: "dns_guard", Section: dnsguard.Report{Enabled: false}},
			{Key: "ipv6_pd_guard", Section: ipv6.IPv6PDGuardReport{Enabled: false}},
		}
		skipped := base()
		skipped.InSync = false
		skipped.Parts = []device.ReportPart{{Key: "ipv6_routes", Section: ipv6.IPv6RoutesReport{Enabled: true, Skip: []string{"synthetic missing next hop"}}}}
		conflict := base()
		conflict.InSync = false
		conflict.Routes.Items = []device.RouteItem{{CIDR: "198.51.100.0/24", Status: "conflict", Detail: "synthetic conflict"}}
		pd := base()
		pd.InSync = false
		pd.Parts = []device.ReportPart{{Key: "ipv6_pd_guard", Section: ipv6.IPv6PDGuardReport{Enabled: true, Items: []ipv6.IPv6PDItem{{Status: "adopt"}}}}}

		cases := []struct {
			name   string
			report *Report
			plan   *Plan
		}{
			{"synced", base(), &Plan{Stages: []device.Stage{r13Stage(device.StageRoutes), r13Stage(device.StageNATBypass)}}},
			{"normal_stages", all, &Plan{Stages: []device.Stage{
				r13Stage(device.StageReturnRouting, "return_routing_apply"),
				r13Stage(device.StageRoutes, "route_add"),
				r13Stage(device.StageIPv6FakeIP, "ipv6_fakeip_replace"),
				r13Stage(device.StageIPv6Routes, "ipv6_routes_replace"),
				r13Stage(device.StageDHCPDNS, "dhcp_dns_update"),
				r13Stage(device.StageIPv6DNS, "ipv6_dns_update"),
				r13Stage(device.StageIPv6PDGuard, "ipv6_pd_guard_sync"),
				r13Stage(device.StageReturnCleanup, "return_routing_remove"),
			}}},
			{"withdraw", withdraw, &Plan{Stages: []device.Stage{
				r13Stage(device.StageIPv6FakeIP, "ipv6_fakeip_replace"),
				r13Stage(device.StageIPv6Routes, "ipv6_routes_replace"),
				r13Stage(device.StageDNSGuard, "dns_guard_replace"),
				r13Stage(device.StageIPv6PDGuard, "ipv6_pd_guard_sync"),
			}}},
			{"blocked", all, &Plan{Stages: []device.Stage{r13Stage(device.StageRoutes, "route_add")}, Blockers: []string{"synthetic blocker", "second blocker"}}},
			{"skipped", skipped, &Plan{Stages: []device.Stage{{Key: device.StageIPv6Routes, Title: StageTitles[device.StageIPv6Routes], Skipped: new(true), Ops: []device.Op{}}}}},
			{"conflict", conflict, &Plan{}},
			{"adopt", pd, &Plan{Stages: []device.Stage{r13Stage(device.StageRoutes, "route_comment"), r13Stage(device.StageIPv6PDGuard, "ipv6_pd_guard_sync")}}},
			{"generic", &Report{}, &Plan{}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				actual := Summarize(c.report, c.plan)
				fromJSON, err := SummarizeJSON(device.JSON(c.report), device.JSON(c.plan))
				if err != nil || !reflect.DeepEqual(actual, fromJSON) {
					t.Fatalf("SummarizeJSON mismatch: %+v vs %+v (err=%v)", fromJSON, actual, err)
				}
				ref.Assert(t, goldenMarshal(t, map[string]string{"module": "R13", "case": c.name}), goldenMarshal(t, actual))
			})
		}
	})
}
