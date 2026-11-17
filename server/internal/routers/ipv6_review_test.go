package routers

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"configflow/internal/routers/routerfake"
)

// r10ReviewCases are the independent R10 review's extra Python boundary
// sequences: route flags and tables the port's own matrix did not vary, and
// external edits/deletions of owned routes between publications.
func r10ReviewCases() []r10Case {
	out := []r10Case{}
	for _, feature := range []string{"fake", "routes"} {
		base := func(name string, steps ...r10Step) r10Case {
			return r10Case{feature + "/review_" + name, feature, r10State(), r10Router(), steps}
		}
		cidr := "fd00:face::/64"
		if feature == "routes" {
			cidr = r10Wanted(5).IPv6CIDRs[0].CIDR
		}
		for _, variant := range []string{"dynamic", "connect", "other_table", "more_specific", "more_specific_disabled", "blackhole"} {
			tc := base("foreign_"+variant, r10StepFor("plan", 5))
			row := r10Route("*50", cidr, "fe80::4%bridge")
			switch variant {
			case "dynamic", "connect", "blackhole":
				row[variant] = "true"
			case "other_table":
				row["routing-table"] = "other"
			case "more_specific", "more_specific_disabled":
				if feature == "fake" {
					row["dst-address"] = "fd00:face::/96"
				} else {
					row["dst-address"] = "2001:db8:a:1::/64"
				}
				if variant == "more_specific_disabled" {
					row["disabled"] = "true"
				}
			}
			tc.State.Tables[rosIPv6Routes] = []routerfake.Row{row}
			out = append(out, tc)
		}
		// Owned routes edited or deleted outside ConfigFlow after a publication.
		edit := r10StepFor("publish", 5)
		edit.Mutation = &routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{rosIPv6Routes: {"*65": {"distance": "5"}}}}}
		out = append(out, base("owned_edited", r10StepFor("publish", 5), edit))
		gone := r10StepFor("publish", 5)
		gone.Mutation = &routerfake.ExternalWrite{Fault: routerfake.Fault{RemoveIDs: map[string][]string{rosIPv6Routes: {"*65"}}}}
		out = append(out, base("owned_deleted", r10StepFor("publish", 5), gone, r10StepFor("plan", 5)))
		if feature == "routes" {
			out = append(out, base("shrink", r10StepFor("publish", 5), r10StepFor("publish", 2), r10StepFor("plan", 2)))
		}
		// A disabled foreign route at the destination, enabled after the preview.
		late := base("late_foreign", r10StepFor("plan", 5), r10StepFor("apply", 5))
		late.Steps[1].Mutation = &routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{rosIPv6Routes: {r10Route("*50", cidr, "fe80::9%bridge")}}}}
		out = append(out, late)
	}
	return out
}

func TestR10ReviewPythonProbes(t *testing.T) {
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, tc := range r10ReviewCases() {
		t.Run(tc.Name, func(t *testing.T) {
			py := r10Python(t, srv.URL, tc)
			pyState := f.Snapshot()
			got := r10Run(t, f, srv.URL, tc)
			gotState := f.Snapshot()
			a, b := r08Comparable(t, py), r08Comparable(t, got)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("R10 result differs: %s", r08Difference(a, b, "$"))
			}
			a, b = r08Comparable(t, pyState.Tables), r08Comparable(t, gotState.Tables)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("R10 device tables differ: %s", r08Difference(a, b, "$"))
			}
			a, b = r10Writes(t, pyState.Calls), r10Writes(t, gotState.Calls)
			if !reflect.DeepEqual(a, b) {
				t.Errorf("R10 write sequence differs: %s", r08Difference(a, b, "$"))
			}
		})
	}
}

// A compensating DELETE whose reply is lost may still have removed the new
// route. Fake-IP rollback must then restore the previous route instead of
// leaving the Fake-IP range unrouted.
func TestR10ReviewFakeRollbackRestoresAfterLostDelete(t *testing.T) {
	d, f := r10Driver(t)
	p := r10FakePlan(t, d)
	if _, err := d.ApplyIPv6FakeIP(t.Context(), *p.Ops[0].(*IPv6FakeIPReplace)); err != nil {
		t.Fatal(err)
	}
	f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{
		{Method: "PUT", Path: rosIPv6Routes, Count: 1, AfterCommit: true, PatchRows: map[string]map[string]routerfake.Row{rosIPv6Routes: {"*66": {"active": "false"}}}},
		{Method: "DELETE", Path: rosIPv6Routes + "/*66", Count: 1, AfterCommit: true, LoseReply: true},
	}})
	host := r10Host()
	host.Addresses = []IPv6HostAddress{{Address: "fe80::5"}}
	r := r10Router()
	r.Extra["ipv6_fakeip_state"] = guardRaw(d.ipv6FakeIPManifest)
	p = InspectIPv6FakeIP(t.Context(), r10Config(r10YAML), r, r10Agent(), host, d)
	if len(p.Ops) != 1 {
		t.Fatalf("no replacement planned: %s", mustPolicyJSON(p))
	}
	if _, err := d.ApplyIPv6FakeIP(t.Context(), *p.Ops[0].(*IPv6FakeIPReplace)); err == nil {
		t.Fatal("inactive replacement accepted")
	}
	live, err := SnapshotIPv6(t.Context(), d, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(live["routes"]) != 1 || live["routes"][0].text("gateway") != "fe80::4%bridge" {
		t.Fatalf("previous Fake-IP route not restored: %v", live["routes"])
	}
	owned, err := ipv6Owned(d.ipv6FakeIPManifest, live, true)
	if err != nil || len(owned) != 1 || owned[0].ID != live["routes"][0].id() {
		t.Fatalf("restored route not owned: %s %v", d.ipv6FakeIPManifest, err)
	}
}

// TestR10ReviewLostDeleteDeviceMatchesPython compares only the device: the
// Python manifest after an incomplete rollback is its known stale-ID bug.
func TestR10ReviewLostDeleteDeviceMatchesPython(t *testing.T) {
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	replace := r10StepFor("publish", 5)
	replace.Host.Addresses = []IPv6HostAddress{{Address: "fe80::5"}}
	replace.Mutation = &routerfake.ExternalWrite{Faults: []routerfake.Fault{
		{Method: "PUT", Path: rosIPv6Routes, Count: 1, AfterCommit: true, PatchRows: map[string]map[string]routerfake.Row{rosIPv6Routes: {"*66": {"active": "false"}}}},
		{Method: "DELETE", Path: rosIPv6Routes + "/*66", Count: 1, AfterCommit: true, LoseReply: true},
	}}
	tc := r10Case{"fake/review_lost_delete", "fake", r10State(), r10Router(), []r10Step{r10StepFor("publish", 5), replace}}
	py := r10Python(t, srv.URL, tc)
	pyState := f.Snapshot()
	got := r10Run(t, f, srv.URL, tc)
	gotState := f.Snapshot()
	if py.Error == nil || got.Error == nil {
		t.Fatalf("expected both to fail: %+v %+v", py.Error, got.Error)
	}
	a, b := r08Comparable(t, pyState.Tables), r08Comparable(t, gotState.Tables)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("R10 device tables differ: %s", r08Difference(a, b, "$"))
	}
}
