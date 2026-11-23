package routers

import (
	"bytes"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/devhttp"
	"configflow/internal/routers/routerfake"
)

// TestO17NullFieldsStayRefused: a null that a reader refused before rows
// became text (O17 1.8) is still refused by every list read, including the
// write path (payload) that takes over or restores an object.
func TestO17NullFieldsStayRefused(t *testing.T) {
	f := routerfake.New(map[string][]routerfake.Row{
		ros.PathIPv6ND: {
			{".id": "*1", "interface": "bridge", "disabled": nil, "dynamic": "false", "invalid": "false"},
			{".id": "*2", "interface": "bridge", "disabled": "false", "dynamic": nil, "invalid": "false"},
			{".id": "*3", "interface": "bridge", "disabled": "false", "dynamic": "false", "invalid": nil},
			{".id": "*4", "interface": "bridge", "disabled": "false", "dynamic": "false", "invalid": "false"},
		},
		ros.PathDHCPNetworks: {
			{".id": "*5", "address": "192.0.2.0/24", "dns-none": nil},
			{".id": "*6", "address": "198.51.100.0/24", "dns-none": "false"},
		},
		"/interface/list": {{".id": "*7", "name": "LAN", "include": nil}},
	})
	server := httptest.NewServer(f)
	defer server.Close()
	d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	ctx := context.Background()

	nd, err := d.List(ctx, ros.PathIPv6ND)
	if err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{"disabled", "dynamic", "invalid"} {
		if ros.Payload(ros.PathIPv6ND, nd[i]) != nil {
			t.Errorf("ND row with null %s was writable", key)
		}
		if got, want := ros.PayloadError(ros.PathIPv6ND, nd[i]).Error(), fmt.Sprintf(ros.MessageIPv6DNSFlag, key); !strings.Contains(got, want) {
			t.Errorf("ND null %s: %q, want %q", key, got, want)
		}
	}
	if ros.Payload(ros.PathIPv6ND, nd[3]) == nil {
		t.Error("plain ND row refused")
	}

	networks, err := d.List(ctx, ros.PathDHCPNetworks)
	if err != nil {
		t.Fatal(err)
	}
	if ros.Payload(ros.PathDHCPNetworks, networks[0]) != nil {
		t.Error("DHCP network with null dns-none was writable")
	}
	if ros.Payload(ros.PathDHCPNetworks, networks[1]) == nil {
		t.Error("plain DHCP network refused")
	}

	lists, err := d.List(ctx, "/interface/list")
	if err != nil {
		t.Fatal(err)
	}
	s := &PolicySnapshot{InterfaceLists: lists, InterfaceMembers: []Row{}}
	if got := policyInterface(s, new("ether1"), "LAN", map[string]bool{}); got != PolicyUnknown {
		t.Errorf("interface list with null include: %v, want unknown", got)
	}
}

// TestO17StructuredSelectorUnknown: a firewall selector that is a JSON array
// or object (held as its JSON text) is unknown, as before rows were text.
func TestO17StructuredSelectorUnknown(t *testing.T) {
	s := &PolicySnapshot{AddressLists: []Row{}, InterfaceLists: []Row{}, InterfaceMembers: []Row{}}
	packet := PolicyPacket{Protocol: new("tcp"), InInterface: new("ether1"), SrcAddress: new("192.0.2.1"),
		DstPort: new(443)}
	for _, raw := range []string{
		`{"chain":"forward","protocol":["udp"]}`,
		`{"chain":"forward","in-interface":{"name":"ether2"}}`,
		`{"chain":"forward","src-address":["198.51.100.0/24"]}`,
		`{"chain":"forward","dst-port":[80]}`,
	} {
		if got := MatchPolicyRule(s, decodedRow(t, raw), packet); got != PolicyUnknown {
			t.Errorf("%s: %v, want unknown", raw, got)
		}
	}
	if got := MatchPolicyRule(s, decodedRow(t, `{"chain":"forward","protocol":"udp"}`), packet); got != PolicyNoMatch {
		t.Errorf("text selector: %v, want no match", got)
	}
}

// legacyIkuaiNewNAT, legacyIkuaiNewPolicy and legacyIkuaiNewRoute are the
// map forms the new-object writes sent before O17 1.8 (go@d8286168b),
// kept to prove the typed forms send the same bytes.
func legacyIkuaiNewNAT(src, dst []string, comment string, refs []any, useRefs bool) ikuaiRow {
	custom := make([]any, len(src))
	for i, v := range src {
		custom[i] = v
	}
	return ikuaiNATPayload(ikuaiRow{}, ikuaiRow{"enabled": "yes", "tagname": IkuaiTagname("nat", strings.Join(dst, ",")),
		"action": "filter", "protocol": "any", "iinterface": "any", "ointerface": "any",
		"src_addr": map[string]any{"custom": custom, "object": []any{}}, "dst_addr": ikuaiTarget(dst, refs, useRefs),
		"nat_addr": "", "nat_port": "", "src_addr_inv": 0, "dst_addr_inv": 0, "comment": comment})
}

func legacyIkuaiNewPolicy(gateway string, dst []string, comment string, refs []any, useRefs bool) ikuaiRow {
	changes := ikuaiPolicyTarget(gateway, dst, comment, refs, useRefs)
	maps.Copy(changes, ikuaiRow{"tagname": IkuaiTagname("pr", comment), "interface": "", "iface_band": 0,
		"time": map[string]any{"custom": []any{map[string]any{"type": "weekly", "weekdays": "1234567",
			"start_time": "00:00", "end_time": "23:59"}}, "object": []any{}},
		"mode": 0, "area_code": "", "dst_type": 0})
	return ikuaiPolicyPayload(ikuaiRow{}, changes)
}

func legacyIkuaiNewRoute(cidr, address, mask, gateway, comment string) ikuaiRow {
	return ikuaiRow{"enabled": "yes", "tagname": IkuaiTagname("rt", cidr), "ip_type": "4",
		"interface": "auto", "dst_addr": address, "gateway": gateway, "netmask": mask, "prio": 1, "comment": comment}
}

// ikuaiWireBody is the request body a write of param sends.
func ikuaiWireBody(t *testing.T, funcName string, param any) []byte {
	t.Helper()
	encoded, err := ikuaiEncodeParam(param)
	if err != nil {
		t.Fatal(err)
	}
	body, err := devhttp.EncodeJSON(map[string]any{"func_name": funcName, "action": "add", "param": encoded})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// TestO17IkuaiNewFormsSameBytes: the typed new-object forms send exactly
// the bytes the map forms sent (same members, integer and empty values).
func TestO17IkuaiNewFormsSameBytes(t *testing.T) {
	refs := []any{map[string]any{"type": 1, "gp_name": "cf_group_a"}, map[string]any{"type": 1, "gp_name": "用户组"}}
	tests := []struct {
		name     string
		src, dst []string
		comment  string
		refs     []any
		useRefs  bool
	}{
		{"empty", nil, nil, "", nil, false},
		{"literal", []string{"192.0.2.0/24", "198.51.100.7"}, []string{"203.0.113.0/24"}, "ConfigFlow 规则 <&>", nil, false},
		{"empty-lists", []string{}, []string{}, "c", []any{}, false},
		{"refs", []string{"192.0.2.0/24"}, []string{"203.0.113.0/24", "2001:db8::/32"}, "cf", refs, true},
		{"nil-refs", []string{"192.0.2.0/24"}, []string{"203.0.113.0/24"}, "cf", nil, true},
		{"refs-unused", []string{"192.0.2.0/24"}, []string{"203.0.113.0/24"}, "\"quote\"\n", refs, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, update := range []bool{false, true} {
				legacyNAT, nat := legacyIkuaiNewNAT(tc.src, tc.dst, tc.comment, tc.refs, tc.useRefs), ikuaiNewNAT(tc.src, tc.dst, tc.comment, tc.refs, tc.useRefs)
				legacyPolicy, policy := legacyIkuaiNewPolicy("192.0.2.1", tc.dst, tc.comment, tc.refs, tc.useRefs), ikuaiNewPolicy("192.0.2.1", tc.dst, tc.comment, tc.refs, tc.useRefs)
				if update { // IkuaiOversizedRequests sizes an update with id 0
					legacyNAT["id"], nat.ID = 0, new(int64)
					legacyPolicy["id"], policy.ID = 0, new(int64)
				}
				for _, pair := range []struct {
					funcName    string
					legacy, now any
				}{{"nat_rule", legacyNAT, nat}, {"stream_ipport", legacyPolicy, policy}} {
					want, got := ikuaiWireBody(t, pair.funcName, pair.legacy), ikuaiWireBody(t, pair.funcName, pair.now)
					if !bytes.Equal(want, got) {
						t.Errorf("%s update=%v:\n got %s\nwant %s", pair.funcName, update, got, want)
					}
					if a, b := IkuaiRequestBytes(pair.funcName, "add", pair.legacy), IkuaiRequestBytes(pair.funcName, "add", pair.now); a != b {
						t.Errorf("%s update=%v: request bytes %d, want %d", pair.funcName, update, b, a)
					}
				}
			}
			route := &ikuaiRouteForm{Enabled: "yes", Tagname: IkuaiTagname("rt", "203.0.113.0/24"), IPType: "4",
				Interface: "auto", DstAddr: "203.0.113.0", Gateway: "192.0.2.1", Netmask: "255.255.255.0", Prio: 1, Comment: tc.comment}
			want := ikuaiWireBody(t, "static_rt", legacyIkuaiNewRoute("203.0.113.0/24", "203.0.113.0", "255.255.255.0", "192.0.2.1", tc.comment))
			if got := ikuaiWireBody(t, "static_rt", route); !bytes.Equal(want, got) {
				t.Errorf("static_rt:\n got %s\nwant %s", got, want)
			}
		})
	}
	// The forms carry their tag and no id.
	form := ikuaiNewNAT(nil, []string{"203.0.113.0/24"}, "c", nil, false)
	if form.formTag() == "" || form.formID() != "" {
		t.Errorf("form identity: %q %q", form.formTag(), form.formID())
	}
	var row ikuaiRow
	if err := json.Unmarshal([]byte(`{"id":12,"tagname":"t"}`), &row); err != nil || row.formID() != "12" || row.formTag() != "t" {
		t.Errorf("record identity: %q %q %v", row.formID(), row.formTag(), err)
	}
}
