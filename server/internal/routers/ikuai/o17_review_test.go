package ikuai

import (
	"bytes"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"configflow/internal/devhttp"
)

// legacyIkuaiNewNAT, legacyIkuaiNewPolicy and legacyIkuaiNewRoute are the
// map forms the new-object writes sent before O17 1.8 (go@d8286168b),
// kept to prove the typed forms send the same bytes.
func legacyIkuaiNewNAT(src, dst []string, comment string, refs []any, useRefs bool) Row {
	custom := make([]any, len(src))
	for i, v := range src {
		custom[i] = v
	}
	return ikuaiNATPayload(Row{}, Row{"enabled": "yes", "tagname": IkuaiTagname("nat", strings.Join(dst, ",")),
		"action": "filter", "protocol": "any", "iinterface": "any", "ointerface": "any",
		"src_addr": map[string]any{"custom": custom, "object": []any{}}, "dst_addr": ikuaiTarget(dst, refs, useRefs),
		"nat_addr": "", "nat_port": "", "src_addr_inv": 0, "dst_addr_inv": 0, "comment": comment})
}

func legacyIkuaiNewPolicy(gateway string, dst []string, comment string, refs []any, useRefs bool) Row {
	changes := ikuaiPolicyTarget(gateway, dst, comment, refs, useRefs)
	maps.Copy(changes, Row{"tagname": IkuaiTagname("pr", comment), "interface": "", "iface_band": 0,
		"time": map[string]any{"custom": []any{map[string]any{"type": "weekly", "weekdays": "1234567",
			"start_time": "00:00", "end_time": "23:59"}}, "object": []any{}},
		"mode": 0, "area_code": "", "dst_type": 0})
	return ikuaiPolicyPayload(Row{}, changes)
}

func legacyIkuaiNewRoute(cidr, address, mask, gateway, comment string) Row {
	return Row{"enabled": "yes", "tagname": IkuaiTagname("rt", cidr), "ip_type": "4",
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
	var row Row
	if err := json.Unmarshal([]byte(`{"id":12,"tagname":"t"}`), &row); err != nil || row.formID() != "12" || row.formTag() != "t" {
		t.Errorf("record identity: %q %q %v", row.formID(), row.formTag(), err)
	}
}
