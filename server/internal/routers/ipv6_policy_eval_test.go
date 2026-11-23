package routers

import (
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"encoding/json"
	"fmt"
	"testing"
)

type r09EvalCase struct {
	Name     string          `json:"name"`
	Snapshot *PolicySnapshot `json:"snapshot"`
	Packet   PolicyPacket    `json:"packet"`
	Row      Row             `json:"row"`
	Family   int             `json:"family"`
}

func r09EvalCases() []r09EvalCase {
	base := PolicySnapshot{Filter: []Row{}, AddressLists: []Row{{"list": "proxied", "address": "198.51.100.0/24"}}, Interfaces: []Row{{"name": "bridge", "dynamic": "false"}}, InterfaceLists: []Row{{"name": "LAN"}, {"name": "WAN"}, {"name": "nested", "include": "LAN", "exclude": "WAN"}, {"name": "cycle", "include": "cycle"}}, InterfaceMembers: []Row{{"list": "LAN", "interface": "bridge"}}}
	packet := ipv6.SamePortPacket("bridge", "198.51.100.0/25")
	cases := []r09EvalCase{}
	for key, values := range map[string][]string{
		"dst-address":      {"198.51.100.0/24", "198.51.100.0/26", "203.0.113.0/24", "!198.51.100.0/24", "198.51.100.0-198.51.100.255", "198.51.100.0-198.51.100.2", "203.0.113.0-203.0.113.255", "2001:db8::/32", "example.com"},
		"src-address":      {"192.0.2.1", "!192.0.2.0/24"},
		"dst-address-list": {"proxied", "missing", "!proxied"}, "src-address-list": {"proxied"},
		"in-interface": {"bridge", "other", "!bridge", "bridge,other"}, "out-interface": {"bridge", "other"},
		"in-interface-list":  {"LAN", "WAN", "all", "none", "static", "dynamic", "nested", "cycle", "!WAN", "absent"},
		"out-interface-list": {"LAN", "!LAN"}, "protocol": {"tcp", "6", "udp", "!17"},
		"connection-state": {"invalid", "established,related", "!new"}, "connection-nat-state": {"dstnat", "!dstnat"},
		"ipsec-policy": {"in,ipsec", "out,none", "in,none", "bad,none", "in,bad"}, "packet-mark": {"unknown"},
		"dst-port": {"53", "!443", "50-60", "bad"}, "port": {"53"}, "tcp-flags": {"syn,!ack"}, "hop-limit": {"equal:64"}, "icmp-options": {"128:0"},
	} {
		for _, value := range values {
			for _, action := range []string{"accept", "drop", "jump"} {
				s := r09Copy(base)
				row := Row{"chain": "forward", "action": action, key: value, "jump-target": "child"}
				s.Filter = []Row{row, {"chain": "forward", "action": "drop"}, {"chain": "child", "action": "return"}}
				cases = append(cases, r09EvalCase{Name: key + "/" + value + "/" + action, Snapshot: &s, Packet: packet, Row: row, Family: 4})
			}
		}
	}
	for _, variant := range []string{"all_unknown_accept_drop", "both_unknown_drop", "return", "jump_missing", "jump_loop", "disabled", "invalid", "passthrough", "log", "action_unknown", "action_empty", "action_bool", "unreadable", "branch_budget"} {
		s := r09Copy(base)
		row := Row{"chain": "forward", "action": "drop"}
		s.Filter = []Row{row}
		p := packet
		switch variant {
		case "all_unknown_accept_drop":
			s.Filter = []Row{{"chain": "forward", "action": "accept", "packet-mark": "unknown"}, row}
		case "both_unknown_drop":
			s.Filter = []Row{{"chain": "forward", "action": "drop", "packet-mark": "unknown"}, row}
		case "return":
			s.Filter = []Row{{"chain": "forward", "action": "jump", "jump-target": "child"}, row, {"chain": "child", "action": "return"}}
		case "jump_missing":
			row["action"] = "jump"
			row["jump-target"] = "absent"
		case "jump_loop":
			row["action"] = "jump"
			row["jump-target"] = "child"
			s.Filter = append(s.Filter, Row{"chain": "child", "action": "jump", "jump-target": "forward"})
		case "disabled":
			row["disabled"] = "yes"
		case "invalid":
			row["invalid"] = "1"
		case "passthrough", "log":
			row["action"] = variant
		case "action_unknown":
			row["action"] = "fasttrack-connection"
		case "action_empty":
			row["action"] = ""
		case "action_bool":
			row["action"] = "true"
		case "unreadable":
			s.Filter = nil
		case "branch_budget":
			s.Filter = []Row{}
			for range 300 {
				s.Filter = append(s.Filter, Row{"chain": "forward", "action": "log"})
			}
		}
		cases = append(cases, r09EvalCase{Name: variant, Snapshot: &s, Packet: p, Row: row, Family: 4})
	}
	// Known numeric/flags/ICMP fields and the IPv6 address path use the same matcher.
	for _, row := range []Row{{"dst-port": "50-60"}, {"port": "50,53"}, {"src-port": "!1-1024"}, {"icmp-options": "128:0"}, {"tcp-flags": "syn,!ack"}, {"hop-limit": "equal:64"}, {"hop-limit": "less-than:65"}, {"hop-limit": "greater-than:65"}, {"dst-address": "2001:db8::/32"}} {
		s := r09Copy(base)
		p := packet
		p.SrcPort = new(2000)
		p.DstPort = new(53)
		p.ICMPType = new(128)
		p.ICMPCode = new(0)
		p.HopLimit = new(64)
		flags := []string{"syn"}
		p.TCPFlags = &flags
		p.DstAddress = new("2001:db8:1::/64")
		s.Filter = []Row{ros.MergeRows(row, Row{"chain": "forward", "action": "drop"})}
		cases = append(cases, r09EvalCase{Name: fmt.Sprint(row), Snapshot: &s, Packet: p, Row: row, Family: 6})
	}
	// Non-text device values arrive as their text (Row decoding).
	for _, bad := range []string{"", "true", "7", `["LAN"]`} {
		s := r09Copy(base)
		s.InterfaceLists = append(s.InterfaceLists, Row{"name": "malformed", "include": bad})
		row := Row{"chain": "forward", "action": "accept", "in-interface-list": "malformed"}
		s.Filter = []Row{row, {"chain": "forward", "action": "drop"}}
		cases = append(cases, r09EvalCase{Name: fmt.Sprintf("malformed_include_%q", bad), Snapshot: &s, Packet: packet, Row: row, Family: 4})
	}
	return cases
}
func TestR09SharedEvaluatorPython(t *testing.T) {
	cases := r09EvalCases()
	out := r09Python(t, "r09_eval_reference.py", cases, "")
	var reference []struct {
		Match    *bool  `json:"match"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(out, &reference); err != nil {
		t.Fatalf("decode %v %s", err, out)
	}
	if len(reference) != len(cases) {
		t.Fatal("reference rows missing")
	}
	for i, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			want := PolicyUnknown
			if reference[i].Match != nil {
				want = ipv6.PolicyBool(*reference[i].Match)
			}
			if got := ipv6.MatchPolicyRule(tc.Snapshot, tc.Row, tc.Packet); got != want {
				t.Fatalf("match %v want %v", got, want)
			}
			if got := ipv6.EvaluatePolicyChain(tc.Snapshot, "filter", "forward", tc.Packet).Decision; got != reference[i].Decision {
				t.Fatalf("decision %s want %s", got, reference[i].Decision)
			}
		})
	}
	t.Logf("%d real Python matcher/chain cases", len(cases))
}
func FuzzR09PolicyEvaluator(f *testing.F) {
	for _, tc := range r09EvalCases() {
		f.Add([]byte(r09Raw(tc)))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		var tc r09EvalCase
		if json.Unmarshal(data, &tc) != nil {
			return
		}
		_ = ipv6.MatchPolicyRule(tc.Snapshot, tc.Row, tc.Packet)
		_ = ipv6.EvaluatePolicyChain(tc.Snapshot, "filter", "forward", tc.Packet).Decision
	})
}
func FuzzR09DNSGuardManifest(f *testing.F) {
	f.Add([]byte(`null`))
	f.Add([]byte(`{"version":1,"resources":[]}`))
	f.Add([]byte(`{"resources":[{"path":"/ip/firewall/nat","id":"*1","role":"hijack-udp","config":{"disabled":"true"}}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		m, err := dnsguard.ParseManifest(data)
		if err == nil && m != nil {
			live := DNSGuardSnapshot{}
			for _, path := range dnsguard.Paths() {
				live[path] = []Row{}
			}
			_, _ = dnsguard.Owned(m, &live)
		}
	})
}

func FuzzR09DNSGuardOperation(f *testing.F) {
	spec := &DNSGuardSpec{Agent: "192.0.2.2", Interface: "bridge", Network: "192.0.2.0/24", Bypass: []string{"192.0.2.2/32"}, Failover: true}
	f.Add([]byte(r09Raw(DNSGuardReplace{OpHead: OpHead{Kind: "dns_guard_replace"}, Spec: spec, Previous: json.RawMessage("null"), Owned: []DNSGuardResource{}, Create: dnsguard.Resources(spec)})))
	f.Add([]byte(`{"kind":"dns_guard_replace","spec":null,"owned":[],"create":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		var op DNSGuardReplace
		if json.Unmarshal(data, &op) != nil {
			return
		}
		_ = dnsguard.ValidateOp(op)
		_, _ = dnsguard.ParseManifest(op.Previous)
	})
}
