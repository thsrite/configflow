package routers

import (
	"configflow/internal/routers/ipv6"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type r10PolicyCase struct {
	Name          string              `json:"name"`
	Action        string              `json:"action"`
	Snapshot      map[string][]Row    `json:"snapshot"`
	Errors        []string            `json:"errors"`
	Family        int                 `json:"family"`
	Table         string              `json:"table"`
	Chain         string              `json:"chain"`
	Packet        PolicyPacket        `json:"packet"`
	Agent         any                 `json:"agent"`
	Host          IPv6Host            `json:"host"`
	Configuration PolicyConfiguration `json:"configuration"`
	LAN           string              `json:"lan"`
	Context       PolicyContext       `json:"context"`
}

func r10PolicySnapshot() map[string][]Row {
	out := map[string][]Row{}
	for key := range ipv6.PolicyTablePaths() {
		out[key] = []Row{}
	}
	for _, key := range []string{"routes", "nat", "filter"} {
		out[key] = []Row{}
	}
	out["routes"] = []Row{{".id": "*D", "dst-address": "::/0", "gateway": "fe80::1%wan", "active": "true"}, {".id": "*L", "dst-address": "2001:db8:1::/64", "gateway": "bridge", "active": "true"}, {".id": "*F", "dst-address": "fd00:face::/64", "gateway": "fd00:1::4%bridge", "active": "true"}}
	out["ipv4_routes"] = []Row{{".id": "*D4", "dst-address": "0.0.0.0/0", "gateway": "wan", "active": "true"}, {".id": "*L4", "dst-address": "192.0.2.0/24", "gateway": "bridge", "active": "true"}, {".id": "*F4", "dst-address": "198.18.0.0/16", "gateway": "192.0.2.4%bridge", "active": "true"}}
	out["interfaces"] = []Row{{"name": "bridge", "dynamic": "false"}, {"name": "wan", "dynamic": "true"}}
	return out
}
func r10PolicyCases() []r10PolicyCase {
	cases := []r10PolicyCase{}
	for _, family := range []int{4, 6} {
		for _, table := range []string{"raw", "mangle", "nat", "filter"} {
			for _, action := range []string{"accept", "drop", "reject", "return", "log", "passthrough", "jump", "mark-routing", "masquerade", "src-nat", "dst-nat", "netmap", "redirect"} {
				for _, selector := range []string{"known", "unknown", "false"} {
					s := r10PolicySnapshot()
					chain := "forward"
					if table == "nat" {
						chain = "srcnat"
					}
					key := table
					if family == 4 {
						key = "ipv4_" + key
					}
					row := Row{".id": "*1", "chain": chain, "action": action, "comment": "证据\n" + strings.Repeat("字", 185), "jump-target": "child"}
					if selector == "unknown" {
						row["layer7-protocol"] = "unknown"
					}
					if selector == "false" {
						row["protocol"] = "udp"
					}
					s[key] = []Row{row, {"chain": chain, "action": "drop"}, {"chain": "child", "action": "return"}}
					p := PolicyPacket{Protocol: new("tcp"), DstAddress: new("fd00:face::/64"), ConnectionState: new("new")}
					cases = append(cases, r10PolicyCase{Name: fmt.Sprintf("%d/%s/%s/%s", family, table, action, selector), Action: "chain", Snapshot: s, Family: family, Table: table, Chain: chain, Packet: p})
				}
			}
		}
	}
	for _, variant := range []string{"baseline", "raw_drop", "mangle_mark", "routing_rule", "routing_main", "raw_unread", "no_endpoints", "nat_src", "nat_dst", "candidate_drop", "wrong_gateway", "partial_route", "blackhole", "multiple_exits", "unresolved_exit", "scope_limit", "ipv4_only", "policy_rule_nomatch"} {
		s := r10PolicySnapshot()
		tc := r10PolicyCase{Name: "checks/" + variant, Action: "checks", Snapshot: s, Agent: r10Agent(), Host: r10Host(), Configuration: PolicyConfiguration{FakeIPv6Enabled: true, FakeIPv6Range: "fd00:face::/64"}, LAN: "bridge", Context: PolicyContext{NodeTargets: []PolicyNodeTarget{{Address: "2001:db8:2::10", Port: 443}}, Clients: []PolicyClient{{Address: "2001:db8:1::20"}, {Address: "192.0.2.20"}}, FakeIPv4Range: "198.18.0.0/16"}}
		tc.Host.Addresses = append(tc.Host.Addresses, IPv6HostAddress{Address: "2001:db8:1::4", Usable: true})
		switch variant {
		case "raw_drop":
			s["raw"] = []Row{{".id": "*1", "chain": "prerouting", "action": "drop", "src-address": "2001:db8:1::4"}}
		case "mangle_mark":
			s["mangle"] = []Row{{"chain": "prerouting", "action": "mark-routing", "new-routing-mark": "vpn"}}
		case "routing_rule":
			s["routing_rules"] = []Row{{"src-address": "2001:db8:1::/64", "action": "lookup", "table": "vpn"}}
		case "routing_main":
			s["routing_rules"] = []Row{{"table": "main"}}
		case "raw_unread":
			tc.Errors = []string{"raw"}
		case "no_endpoints":
			tc.Context = PolicyContext{FakeIPv4Range: "198.18.0.0/16"}
		case "nat_src":
			s["nat"] = []Row{{"chain": "srcnat", "action": "masquerade", "out-interface": "wan"}}
		case "nat_dst":
			s["nat"] = []Row{{"chain": "dstnat", "action": "dst-nat"}}
		case "candidate_drop":
			tc.Context.Clients[1].SourceCandidate = true
			tc.Context.Clients[1].SourceNote = "合成登记候选，尚未确认实际源地址"
			s["ipv4_filter"] = []Row{{".id": "*C", "chain": "forward", "action": "drop", "src-address": "192.0.2.20"}}
		case "wrong_gateway":
			s["ipv4_routes"][2]["gateway"] = "192.0.2.99%bridge"
		case "partial_route":
			s["routes"] = append(s["routes"], Row{"dst-address": "fd00:face::/80", "gateway": "other", "active": "true"})
		case "blackhole":
			s["routes"][0]["blackhole"] = "true"
		case "multiple_exits":
			s["routes"] = append(s["routes"], Row{"dst-address": "::/0", "gateway": "fe80::2%wan2", "active": "true"})
		case "unresolved_exit":
			s["routes"][0]["gateway"] = "2001:db8::1"
		case "scope_limit":
			for i := 0; i < 4; i++ {
				tc.Host.Addresses = append(tc.Host.Addresses, IPv6HostAddress{Address: fmt.Sprintf("2001:db8:1::%x", i+30), Usable: true})
				tc.Context.NodeTargets = append(tc.Context.NodeTargets, PolicyNodeTarget{Address: fmt.Sprintf("2001:db8:2::%x", i+30), Port: 443})
				tc.Context.Clients = append(tc.Context.Clients, PolicyClient{Address: fmt.Sprintf("192.0.2.%d", i+30)})
			}
		case "ipv4_only":
			tc.Configuration.FakeIPv6Enabled = false
		case "policy_rule_nomatch":
			s["routing_rules"] = []Row{{"src-address": "203.0.113.0/24", "table": "other"}}
		}
		cases = append(cases, tc)
	}
	return cases
}
func TestR10PolicyPythonBoundary(t *testing.T) {
	cases := r10PolicyCases()
	raw := r09Python(t, "r10_policy_reference.py", cases, "")
	var expected []json.RawMessage
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err, string(raw))
	}
	if len(expected) != len(cases) {
		t.Fatal("missing reference rows")
	}
	for i, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			env := ipv6.PolicyEnvironmentFromSnapshot(tc.Snapshot, tc.Errors)
			var got any
			if tc.Action == "chain" {
				s := &env.IPv6
				if tc.Family == 4 {
					s = &env.IPv4
				}
				got = ipv6.EvaluatePolicyChain(s, tc.Table, tc.Chain, tc.Packet)
			} else {
				got = ipv6.PolicyChecks(env, r10Agent(), tc.Host, tc.Configuration, tc.LAN, tc.Context)
			}
			a, b := r08Comparable(t, expected[i]), r08Comparable(t, got)
			if !reflect.DeepEqual(a, b) {
				t.Fatal(r08Difference(a, b, "$"))
			}
		})
	}
	t.Logf("%d real Python full-evidence policy cases", len(cases))
}
