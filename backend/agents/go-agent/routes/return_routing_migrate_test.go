package routes

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Fixtures mirror real `ip -j -N` / `nft -j` output from a host running the
// 1.8.2-1.8.4 return routing (routes/rule without protocol, table as string).
func legacyHost() *fakeHost {
	var host fakeHost
	host.routes = jsonMaps([]map[string]any{
		{"dst": "default", "gateway": "192.168.1.1", "dev": "eth0", "flags": []any{}},
		{"dst": "default", "gateway": "192.168.1.1", "dev": "eth0", "table": "104", "flags": []any{}},
		{"dst": "192.168.1.0/24", "gateway": "192.168.1.1", "dev": "eth0", "table": "104", "flags": []any{}},
	})
	host.rules = jsonMaps([]map[string]any{
		{"priority": 0, "src": "all", "table": "255"},
		{"priority": 104, "src": "all", "fwmark": "0x400000", "fwmask": "0x400000", "table": "104"},
		{"priority": 32766, "src": "all", "table": "254"},
	})
	var nft struct {
		NFTables []map[string]any `json:"nftables"`
	}
	_ = json.Unmarshal([]byte(`{"nftables":[
	 {"table":{"family":"inet","name":"configflow_return","handle":39}},
	 {"chain":{"family":"inet","table":"configflow_return","name":"prerouting","handle":1,"type":"filter","hook":"prerouting","prio":-150,"policy":"accept"}},
	 {"chain":{"family":"inet","table":"configflow_return","name":"output","handle":2,"type":"route","hook":"output","prio":-150,"policy":"accept"}},
	 {"set":{"family":"inet","name":"proxy_dst","table":"configflow_return","type":"ipv4_addr","handle":3,"flags":["interval"],"elem":[{"prefix":{"addr":"91.108.0.0","len":16}},{"prefix":{"addr":"198.18.0.0","len":16}}]}},
	 {"rule":{"family":"inet","table":"configflow_return","chain":"prerouting","handle":4,"comment":"configflow-return-mark","expr":[{"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"eth0"}},{"match":{"op":"==","left":{"payload":{"protocol":"ether","field":"saddr"}},"right":"00:e0:b4:66:d5:f9"}},{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":"@proxy_dst"}},{"match":{"op":"in","left":{"ct":{"key":"state"}},"right":"new"}},{"mangle":{"key":{"ct":{"key":"mark"}},"value":{"|":[{"ct":{"key":"mark"}},4194304]}}}]}},
	 {"rule":{"family":"inet","table":"configflow_return","chain":"prerouting","handle":5,"comment":"configflow-return-tun","expr":[{"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"Meta"}},{"match":{"op":"==","left":{"&":[{"ct":{"key":"mark"}},4194304]},"right":4194304}},{"mangle":{"key":{"meta":{"key":"mark"}},"value":{"|":[{"meta":{"key":"mark"}},4194304]}}}]}},
	 {"rule":{"family":"inet","table":"configflow_return","chain":"output","handle":6,"comment":"configflow-return-local","expr":[{"match":{"op":"==","left":{"&":[{"ct":{"key":"mark"}},4194304]},"right":4194304}},{"mangle":{"key":{"meta":{"key":"mark"}},"value":{"|":[{"meta":{"key":"mark"}},4194304]}}}]}}
	]}`), &nft)
	host.nft = nft.NFTables
	host.nextHandle = 100
	return &host
}

func writeLegacyState(t *testing.T, cfg *Config) []byte {
	t.Helper()
	data := []byte(`{"spec":{"router_ip":"192.168.1.1","proxy_dst":["198.18.0.0/16","91.108.0.0/16"],"mark":"0x400000","table":104,"priority":104},` +
		`"legacy":{"tables":["inet mihomo_return"],"dump":"/opt/configflow-agent/return-routing-legacy.nft","scripts":["/etc/local.d/mihomo-return.start"]},` +
		`"applied_at":"2026-10-17T10:52:23+08:00"}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(cfg.AgentConfigFile), "return-routing.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

// Boot replay must not infer ownership from an old spec, recognizable names,
// or comments that another program can retain while changing an object.
func TestLegacyBootReplayPreservesUnownedResources(t *testing.T) {
	cases := map[string]func(*fakeHost){
		"matching_legacy_snapshot": func(*fakeHost) {},
		"foreign_expression_with_legacy_comment": func(host *fakeHost) {
			host.nft[4]["rule"].(map[string]any)["expr"] = []any{map[string]any{"drop": nil}}
		},
		"foreign_chain_hook_with_legacy_name": func(host *fakeHost) {
			chain := host.nft[1]["chain"].(map[string]any)
			chain["hook"], chain["prio"], chain["policy"] = "input", float64(0), "drop"
		},
		"foreign_route_attributes": func(host *fakeHost) {
			host.routes[1]["protocol"] = float64(186)
			host.routes[1]["prefsrc"] = "192.168.1.9"
		},
		"foreign_rule_selector": func(host *fakeHost) {
			host.rules[1]["iif"] = "eth1"
		},
		"extra_foreign_rule": func(host *fakeHost) {
			host.nft = append(host.nft, map[string]any{"rule": map[string]any{
				"family": "inet", "table": managedReturnTable, "chain": "output", "handle": 7,
				"comment": "another-program", "expr": []any{map[string]any{"accept": nil}},
			}})
		},
		"inactive_without_owner":   func(*fakeHost) {},
		"active_without_owner":     func(*fakeHost) {},
		"missing_kernel_resources": func(host *fakeHost) { host.routes, host.rules, host.nft = nil, nil, nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			host := legacyHost()
			mutate(host)
			installFakeHost(t, host)
			cfg := testConfig(t, tunConfig)
			original := writeLegacyState(t, cfg)
			path := filepath.Join(filepath.Dir(cfg.AgentConfigFile), "return-routing.json")
			if name == "inactive_without_owner" || name == "active_without_owner" {
				var state map[string]any
				if err := json.Unmarshal(original, &state); err != nil {
					t.Fatal(err)
				}
				delete(state, "legacy")
				state["active"] = name == "active_without_owner"
				original, _ = json.Marshal(state)
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := jsonText([]any{host.routes, host.rules, host.nft, host.vrfs})
			reapplyReturnRouting(cfg)
			assertNoWrites(t, host)
			if len(host.commands) != 0 {
				t.Fatalf("unowned boot replay should stop before probing or changing kernel resources: %v", host.commands)
			}
			if before != jsonText([]any{host.routes, host.rules, host.nft, host.vrfs}) {
				t.Fatal("unowned boot replay changed host resources")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatalf("unowned boot replay changed the original journal: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Fatalf("boot refusal must not create migration artifacts: %v", entries)
			}
		})
	}
}
