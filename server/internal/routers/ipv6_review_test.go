package routers

import (
	"testing"

	"configflow/internal/routers/device"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/routerfake"
)

// A compensating DELETE whose reply is lost may still have removed the new
// route. Fake-IP rollback must then restore the previous route instead of
// leaving the Fake-IP range unrouted.
func TestR10ReviewFakeRollbackRestoresAfterLostDelete(t *testing.T) {
	d, f := r10Driver(t)
	p := r10FakePlan(t, d)
	if _, err := ipv6.ApplyIPv6FakeIP(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6FakeIPReplace)); err != nil {
		t.Fatal(err)
	}
	f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{
		{Method: "PUT", Path: ros.PathIPv6Routes, Count: 1, AfterCommit: true, PatchRows: map[string]map[string]routerfake.Row{ros.PathIPv6Routes: {"*66": {"active": "false"}}}},
		{Method: "DELETE", Path: ros.PathIPv6Routes + "/*66", Count: 1, AfterCommit: true, LoseReply: true},
	}})
	host := r10Host()
	host.Addresses = []ipv6.IPv6HostAddress{{Address: "fe80::5"}}
	r := r10Router()
	r.Extra["ipv6_fakeip_state"] = ros.GuardRaw(d.IPv6FakeIPManifest())
	p = ipv6.InspectIPv6FakeIP(t.Context(), r10Config(r10YAML), r, r10Agent(), host, d.Client)
	if len(p.Ops) != 1 {
		t.Fatalf("no replacement planned: %s", device.JSON(p))
	}
	if _, err := ipv6.ApplyIPv6FakeIP(t.Context(), d.Client, *p.Ops[0].(*ipv6.IPv6FakeIPReplace)); err == nil {
		t.Fatal("inactive replacement accepted")
	}
	live, err := ipv6.SnapshotIPv6(t.Context(), d.Client, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(live["routes"]) != 1 || live["routes"][0].Text("gateway") != "fe80::4%bridge" {
		t.Fatalf("previous Fake-IP route not restored: %v", live["routes"])
	}
	owned, err := ipv6.Owned(d.IPv6FakeIPManifest(), live, true)
	if err != nil || len(owned) != 1 || owned[0].ID != live["routes"][0].ID() {
		t.Fatalf("restored route not owned: %s %v", d.IPv6FakeIPManifest(), err)
	}
}
