package routers

import (
	"configflow/internal/routers/ipv6"
	"encoding/json"
	"reflect"
	"testing"
)

// TestR10ReviewPolicyPythonProbes adds review-chosen policy_checks inputs to the
// port's matrix: NAT exemption and jumps, per-table read errors, conditional
// drops, interface lists and malformed supplied endpoints.
func TestR10ReviewPolicyPythonProbes(t *testing.T) {
	cases := []r10PolicyCase{}
	for _, variant := range []string{"nat_exempt_first", "nat_jump", "ipv4_nat_unread", "routes_unread", "established_drop", "input_icmp_drop", "interface_list_unread", "interface_list_member", "fake6_disabled", "client_prefix", "node_port_zero", "dns_name_list", "dstnat_input", "mangle_postrouting_unknown", "address_list_unread"} {
		s := r10PolicySnapshot()
		tc := r10PolicyCase{Name: "review/" + variant, Action: "checks", Snapshot: s, Agent: r10Agent(), Host: r10Host(), Configuration: PolicyConfiguration{FakeIPv6Enabled: true, FakeIPv6Range: "fd00:face::/64"}, LAN: "bridge", Context: PolicyContext{NodeTargets: []PolicyNodeTarget{{Address: "2001:db8:2::10", Port: 443}}, Clients: []PolicyClient{{Address: "2001:db8:1::20"}, {Address: "192.0.2.20"}}, FakeIPv4Range: "198.18.0.0/16"}}
		tc.Host.Addresses = append(tc.Host.Addresses, IPv6HostAddress{Address: "2001:db8:1::4", Usable: true})
		switch variant {
		case "nat_exempt_first":
			s["nat"] = []Row{{".id": "*1", "chain": "srcnat", "action": "accept", "dst-address": "2001:db8:2::/64"}, {".id": "*2", "chain": "srcnat", "action": "masquerade"}}
			s["ipv4_nat"] = []Row{{".id": "*3", "chain": "srcnat", "action": "accept", "src-address": "192.0.2.0/24", "dst-address": "198.18.0.0/16"}, {".id": "*4", "chain": "srcnat", "action": "masquerade", "out-interface": "wan"}}
		case "nat_jump":
			s["nat"] = []Row{{".id": "*1", "chain": "srcnat", "action": "jump", "jump-target": "cf"}, {".id": "*2", "chain": "cf", "action": "masquerade", "out-interface": "wan"}}
		case "ipv4_nat_unread":
			tc.Errors = []string{"ipv4_nat"}
		case "routes_unread":
			tc.Errors = []string{"routes"}
		case "established_drop":
			s["filter"] = []Row{{".id": "*1", "chain": "forward", "action": "drop", "connection-state": "established"}}
			s["ipv4_filter"] = []Row{{".id": "*2", "chain": "forward", "action": "drop", "connection-state": "established"}}
		case "input_icmp_drop":
			s["filter"] = []Row{{".id": "*1", "chain": "input", "action": "drop", "protocol": "icmpv6"}}
		case "interface_list_unread":
			tc.Errors = []string{"interface_members"}
			s["filter"] = []Row{{".id": "*1", "chain": "forward", "action": "drop", "in-interface-list": "LAN"}}
		case "interface_list_member":
			s["interface_lists"] = []Row{{"name": "LAN"}}
			s["interface_members"] = []Row{{"list": "LAN", "interface": "bridge"}}
			s["filter"] = []Row{{".id": "*1", "chain": "forward", "action": "drop", "in-interface-list": "!LAN"}}
		case "fake6_disabled":
			tc.Configuration.FakeIPv6Enabled = false
			tc.Context.FakeIPv4Range = ""
		case "client_prefix":
			tc.Context.Clients = []PolicyClient{{Address: "2001:db8:1::/64"}, {Address: "192.0.2.0/24"}, {Address: "not-an-address"}}
		case "node_port_zero":
			tc.Context.NodeTargets = []PolicyNodeTarget{{Address: "2001:db8:2::10", Port: 0}, {Address: "2001:db8:2::/64", Port: 443}, {Address: "192.0.2.9", Port: 443}}
		case "dns_name_list":
			s["address_lists"] = []Row{{"list": "nodes", "address": "node.example.com"}}
			s["filter"] = []Row{{".id": "*1", "chain": "forward", "action": "drop", "dst-address-list": "nodes"}}
		case "dstnat_input":
			s["nat"] = []Row{{".id": "*1", "chain": "dstnat", "action": "redirect", "protocol": "udp"}}
		case "mangle_postrouting_unknown":
			s["mangle"] = []Row{{".id": "*1", "chain": "postrouting", "action": "change-mss", "tcp-flags": "syn"}}
		case "address_list_unread":
			tc.Errors = []string{"ipv4_address_lists"}
			s["ipv4_filter"] = []Row{{".id": "*1", "chain": "forward", "action": "accept", "src-address-list": "lan"}, {".id": "*2", "chain": "forward", "action": "drop"}}
		}
		cases = append(cases, tc)
	}
	raw := r09Python(t, "r10_policy_reference.py", cases, "")
	var expected []json.RawMessage
	if err := json.Unmarshal(raw, &expected); err != nil || len(expected) != len(cases) {
		t.Fatal(err, string(raw))
	}
	for i, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			env := ipv6.PolicyEnvironmentFromSnapshot(tc.Snapshot, tc.Errors)
			got := ipv6.PolicyChecks(env, r10Agent(), tc.Host, tc.Configuration, tc.LAN, tc.Context)
			a, b := r08Comparable(t, expected[i]), r08Comparable(t, got)
			if !reflect.DeepEqual(a, b) {
				t.Fatal(r08Difference(a, b, "$"))
			}
		})
	}
}
