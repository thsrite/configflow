package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"configflow/internal/routers/routerfake"
)

// Tests for the fresh-Opus R08 review findings (parity/review-r08.md).

// F1: a fresh NAT-list member moved into another list during the release is an
// external edit. Rollback keeps it and reports an incomplete rollback instead of
// treating the ID missing from its own list as already deleted.
func TestR08ReviewNATListRollbackKeepsMovedMember(t *testing.T) {
	f := routerfake.New(r07FakeState().Tables)
	server := httptest.NewServer(f)
	defer server.Close()
	d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	ctx := context.Background()
	undo, err := rospolicy.ApplyNATLists(ctx, d.Client, r07NewSync())
	if err != nil {
		t.Fatal(err)
	}
	moved := ""
	for _, row := range f.Snapshot().Tables[ros.PathAddresses] {
		if row["list"] == ros.NATDstList {
			moved, _ = row[".id"].(string)
		}
	}
	if moved == "" {
		t.Fatal("no fresh destination member")
	}
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{ros.PathAddresses: {moved: {"list": "user-list"}}}}})
	err = rospolicy.RestoreNATLists(ctx, d.Client, *undo)
	if err == nil {
		t.Fatal("rollback reported success while a fresh member survived in another list")
	}
	if want := fmt.Sprintf(device.MessagePolicyChanged, ros.PathAddresses, moved); !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not name the moved member (%q)", err, want)
	}
	if row := findRowFake(f.Snapshot().Tables[ros.PathAddresses], moved); row == nil || row["list"] != "user-list" {
		t.Fatalf("moved member was not kept: %v", row)
	}
}

// F2: subtraction never turns an input error into "nothing remains".
func TestR08ReviewSubtractNetworksFamilies(t *testing.T) {
	p := netip.MustParsePrefix
	tests := []struct {
		name        string
		from, minus []netip.Prefix
		want        []string
	}{
		{"ipv4 sweep", []netip.Prefix{p("10.0.0.0/8")}, []netip.Prefix{p("10.0.0.0/9")}, []string{"10.128.0.0/9"}},
		{"ipv6 exclusion keeps ipv4", []netip.Prefix{p("10.0.0.0/8")}, []netip.Prefix{p("2001:db8::/32")}, []string{"10.0.0.0/8"}},
		{"mixed exclusions", []netip.Prefix{p("10.0.0.0/8")}, []netip.Prefix{p("10.0.0.0/9"), p("2001:db8::/32")}, []string{"10.128.0.0/9"}},
		{"mixed input per family", []netip.Prefix{p("10.0.0.0/8"), p("2001:db8::/32")}, []netip.Prefix{p("2001:db8::/33")}, []string{"10.0.0.0/8", "2001:db8:8000::/33"}},
		{"invalid ignored", []netip.Prefix{{}, p("192.0.2.0/24")}, []netip.Prefix{{}}, []string{"192.0.2.0/24"}},
		{"everything removed", []netip.Prefix{p("192.0.2.0/25")}, []netip.Prefix{p("192.0.2.0/24")}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			for _, prefix := range rospolicy.SubtractNetworks(tt.from, tt.minus) {
				got = append(got, prefix.String())
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// F3: prepare_static is present exactly when the planner set it, even empty.
func TestR08ReviewPrepareStaticPresence(t *testing.T) {
	base := rospolicy.ROSPolicyReplace{OpHead: device.OpHead{Kind: "ros_policy_replace", Summary: "s"}, Bundle: []rospolicy.ROSReplacement{}, Owned: []ros.Resource{}, Migrations: []ros.Resource{}, Replacements: []rospolicy.ROSReplacement{}, StaticChecks: []device.CIDRGateway{}}
	tests := []struct {
		name    string
		prepare []device.Op
		want    string // raw prepare_static, "" = absent
	}{
		{"unset", nil, ""},
		{"empty", []device.Op{}, `[]`},
		{"typed op", []device.Op{&rospolicy.NATListSync{OpHead: device.OpHead{Kind: "nat_list_sync", Summary: "x"}}}, "kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := base
			op.PrepareStatic = tt.prepare
			data, err := json.Marshal(&op)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			raw, present := fields["prepare_static"]
			if (tt.want == "") == present || present && !strings.Contains(string(raw), tt.want) {
				t.Fatalf("prepare_static=%s present=%v in %s", raw, present, data)
			}
			if fields["kind"] == nil || fields["summary"] == nil || fields["static_checks"] == nil {
				t.Fatalf("op head or static checks lost: %s", data)
			}
			var write rospolicy.PolicyWrite
			if err = json.Unmarshal(data, &write); err != nil || len(write.PrepareStatic) != len(tt.prepare) {
				t.Fatalf("driver contract decode: %v %d", err, len(write.PrepareStatic))
			}
		})
	}
}

// F4: editor roles come only from lists a saved jump rule actually names.
func TestR08ReviewViewRolesIgnoreEmptyLists(t *testing.T) {
	d, _ := r08Driver(t)
	manifest := json.RawMessage(`{"spec":null,"resources":[
		{"path":"/ip/firewall/mangle","id":"*A1","config":{"chain":"prerouting","action":"jump","src-address-list":"S"}},
		{"path":"/ip/firewall/address-list","id":"*A2","config":{"address":"192.0.2.1"}},
		{"path":"/ip/firewall/address-list","id":"*A3","config":{"list":"S","address":"192.0.2.2"}}]}`)
	objects, err := rospolicy.ReadPolicyObjects(context.Background(), d.Client, manifest)
	if err != nil {
		t.Fatal(err)
	}
	editors := map[string]string{}
	for _, object := range objects {
		editors[object.ID] = object.Editor
	}
	if editors["*A2"] != "settings" || editors["*A3"] != "source" {
		t.Fatalf("editors=%v", editors)
	}
}

// F5: a console-restored route that is gone on read-back reports Python's
// read-back mismatch, not a generic uncertain write.
func TestR08ReviewRestoredRouteVanished(t *testing.T) {
	d, f := r08Driver(t)
	steps := []ros.NATStep{}
	for i := range 4 {
		values := ros.Row{"dst-address": fmt.Sprintf("203.0.113.%d/32", i), "gateway": r08Gateway, "comment": "cf", "disabled": "false"}
		config := ros.CloneRow(values)
		config["routing-table"] = "main"
		steps = append(steps, ros.NATStep{Action: "restore", Path: ros.PathRoutes, ID: fmt.Sprintf("*B%d", i), Config: config, Values: values})
	}
	// The fake numbers new rows from *65: the second restored route vanishes
	// right after the console batch reported it.
	f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "POST", Path: "/execute", AfterCommit: true, RemoveIDs: map[string][]string{ros.PathRoutes: {"*66"}}}}})
	err := rospolicy.RestoreROSPolicy(context.Background(), d.Client, rospolicy.ROSPolicyUndo{Kind: "ros_policy_restore", Steps: steps, Previous: json.RawMessage(`null`)})
	if err == nil {
		t.Fatal("vanished route accepted")
	}
	want := rospolicy.MessageRollbackPrefix + fmt.Sprintf(rospolicy.MessageConfirm, ros.PathRoutes, rospolicy.MessageMissingDetail)
	if err.Error() != want {
		t.Fatalf("error %q, want %q", err, want)
	}
	routes := 0
	for _, row := range f.Snapshot().Tables[ros.PathRoutes] {
		if row["comment"] == "cf" && strings.HasPrefix(fmt.Sprint(row["dst-address"]), "203.0.113.") {
			routes++
		}
	}
	if routes != 3 {
		t.Fatalf("restored routes=%d, want 3", routes)
	}
}
