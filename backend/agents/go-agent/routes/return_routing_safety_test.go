package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

func safetySpec(t *testing.T) resolvedSpec {
	t.Helper()
	spec, err := validateSpec(ReturnRoutingSpec{SafetyVersion: 1, RouterIP: "192.168.1.1", ProxyDst: []string{"198.18.0.0/16"}, Mark: "0x400000", Table: 104, Priority: 104, LanInterface: "eth0"})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func safetyObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	value, err := decodeResourceJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value.(map[string]any)
}

func safetyReadFake(t *testing.T, override map[string]string, failed string) *[]string {
	t.Helper()
	old := runCommand
	commands := []string{}
	outputs := map[string]string{
		"ip -V":                            "ip utility, iproute2-6.1.0",
		"nft --version":                    "nftables v1.0.6",
		"ip -j -N -4 route show table all": `[{"dst":"default","gateway":"192.168.1.1","dev":"eth0","table":"main"}]`,
		"ip -j -N -4 rule show":            `[{"priority":0,"src":"all","table":"local"},{"priority":32766,"src":"all","table":"main"}]`,
		"nft -j list ruleset":              `{"nftables":[{"metainfo":{"json_schema_version":1}}]}`,
		"ip -j -d link show":               `[]`,
	}
	for key, value := range override {
		outputs[key] = value
	}
	runCommand = func(name string, args ...string) (string, error) {
		command := name + " " + strings.Join(args, " ")
		commands = append(commands, command)
		if command == failed {
			return "", errors.New("read denied")
		}
		output, ok := outputs[command]
		if !ok {
			t.Fatalf("unexpected or mutating command: %s", command)
		}
		return output, nil
	}
	t.Cleanup(func() { runCommand = old })
	return &commands
}

func TestSafetyReadResourcesUsesCompleteNumericSnapshots(t *testing.T) {
	commands := safetyReadFake(t, nil, "")
	resources, err := readReturnResources()
	if err != nil {
		t.Fatal(err)
	}
	if len(*commands) != 6 || len(resources.NFT) != 0 || len(resources.Routes) != 1 {
		t.Fatalf("resources=%+v commands=%v", resources, *commands)
	}
	if err := checkReturnResources(safetySpec(t), nil, resources); err != nil {
		t.Fatal(err)
	}
}

func TestSafetyReadResourcesRejectsFailedOrIncompleteEvidence(t *testing.T) {
	cases := []struct {
		name, command, raw string
		fail               bool
	}{
		{"route command failure", "ip -j -N -4 route show table all", "", true},
		{"rule command failure", "ip -j -N -4 rule show", "", true},
		{"nft command failure", "nft -j list ruleset", "", true},
		{"vrf command failure", "ip -j -d link show", "", true},
		{"empty output", "ip -j -N -4 route show table all", "", false},
		{"null array", "ip -j -N -4 route show table all", `null`, false},
		{"object instead array", "ip -j -N -4 rule show", `{}`, false},
		{"empty route", "ip -j -N -4 route show table all", `[{}]`, false},
		{"route no destination", "ip -j -N -4 route show table all", `[{"table":104}]`, false},
		{"unresolved table alias", "ip -j -N -4 route show table all", `[{"dst":"default","table":"vpn"}]`, false},
		{"rule no priority", "ip -j -N -4 rule show", `[{"table":104}]`, false},
		{"rule missing action", "ip -j -N -4 rule show", `[{"priority":200}]`, false},
		{"rule unknown action", "ip -j -N -4 rule show", `[{"priority":200,"action":"future"}]`, false},
		{"goto missing target", "ip -j -N -4 rule show", `[{"priority":9001,"goto":"none"}]`, false},
		{"goto boolean target", "ip -j -N -4 rule show", `[{"priority":9001,"goto":true}]`, false},
		{"goto table alias", "ip -j -N -4 rule show", `[{"priority":9001,"goto":"main"}]`, false},
		{"goto out of range", "ip -j -N -4 rule show", `[{"priority":9001,"goto":4294967296}]`, false},
		{"goto unresolved", "ip -j -N -4 rule show", `[{"priority":9001,"goto":9010,"unresolved":null}]`, false},
		{"goto contradictory action", "ip -j -N -4 rule show", `[{"priority":9001,"goto":9010,"action":"blackhole"}]`, false},
		{"goto contradictory nop", "ip -j -N -4 rule show", `[{"priority":9001,"goto":9010,"nop":null}]`, false},
		{"nop malformed flag", "ip -j -N -4 rule show", `[{"priority":9001,"nop":true}]`, false},
		{"invalid rule mark", "ip -j -N -4 rule show", `[{"priority":100,"fwmark":true}]`, false},
		{"nft missing list", "nft -j list ruleset", `{}`, false},
		{"nft null list", "nft -j list ruleset", `{"nftables":null}`, false},
		{"nft missing family", "nft -j list ruleset", `{"nftables":[{"table":{"name":"foreign"}}]}`, false},
		{"nft rule missing expressions", "nft -j list ruleset", `{"nftables":[{"rule":{"family":"inet","table":"foreign"}}]}`, false},
		{"nft partial mangle", "nft -j list ruleset", `{"nftables":[{"rule":{"family":"inet","table":"foreign","chain":"input","expr":[{"mangle":{}}]}}]}`, false},
		{"nft partial mangle target", "nft -j list ruleset", `{"nftables":[{"rule":{"family":"inet","table":"foreign","chain":"input","expr":[{"mangle":{"key":{"meta":{}},"value":2}}]}}]}`, false},
		{"nft partial match", "nft -j list ruleset", `{"nftables":[{"rule":{"family":"inet","table":"foreign","chain":"input","expr":[{"match":{"left":{"ct":{"key":"mark"}}}}]}}]}`, false},
		{"nft trailing data", "nft -j list ruleset", `{"nftables":[]} {}`, false},
		{"vrf missing table", "ip -j -d link show", `[{"ifindex":5,"ifname":"vrf0","linkinfo":{"info_kind":"vrf","info_data":{}}}]`, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			failed := ""
			if test.fail {
				failed = test.command
			}
			safetyReadFake(t, map[string]string{test.command: test.raw}, failed)
			if _, err := readReturnResources(); err == nil {
				t.Fatal("incomplete evidence must fail closed")
			}
		})
	}
}

func TestSafetyNewOwnershipRejectsOccupiedResources(t *testing.T) {
	cases := map[string]*returnResources{
		"route with no policy rule":                 {Routes: []map[string]any{{"table": 104, "dst": "10.0.0.0/8"}}},
		"same priority without mark":                {Rules: []map[string]any{{"priority": 104, "table": 200, "src": "10.0.0.0/8"}}},
		"table referenced without routes":           {Rules: []map[string]any{{"priority": 50, "table": 104}}},
		"priority referenced by goto":               {Rules: []map[string]any{{"priority": 50, "goto": 104}}},
		"masked mark zero still reserves bit":       {Rules: []map[string]any{{"priority": 80, "table": 200, "fwmark": "0x0", "fwmask": "0x400000"}}},
		"unmasked mark uses full value":             {Rules: []map[string]any{{"priority": 80, "table": 200, "fwmark": "0x2"}}},
		"nft name alone cannot establish ownership": {NFT: []map[string]any{{"table": map[string]any{"family": "inet", "name": managedReturnTable}}}},
		"empty vrf table is reserved":               {VRFs: []map[string]any{{"linkinfo": map[string]any{"info_kind": "vrf", "info_data": map[string]any{"table": 104}}}}},
	}
	for name, resources := range cases {
		t.Run(name, func(t *testing.T) {
			if err := checkReturnResources(safetySpec(t), nil, resources); err == nil {
				t.Fatal("occupied resources accepted")
			}
		})
	}
	free := &returnResources{Rules: []map[string]any{{"priority": 80, "table": 200, "fwmark": "0x2", "fwmask": "0xff"}}}
	if err := checkReturnResources(safetySpec(t), nil, free); err != nil {
		t.Fatalf("disjoint explicit mask: %v", err)
	}
}

func TestSafetyReadAcceptsIPRouteGotoAndNopEncoding(t *testing.T) {
	// Matches upstream iproute2 print_rule(): goto is a JSON number and nop
	// is a null-valued flag. Neither has an action or lookup-table field.
	safetyReadFake(t, map[string]string{"ip -j -N -4 rule show": `[
		{"priority":0,"src":"all","table":"local"},
		{"priority":9001,"src":"all","iif":"Meta","goto":9010},
		{"priority":9002,"src":"all","nop":null},
		{"priority":9010,"src":"all","table":"2022"},
		{"priority":32766,"src":"all","table":"main"}
	]`}, "")
	resources, err := readReturnResources()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkReturnResources(safetySpec(t), nil, resources); err != nil {
		t.Fatalf("unrelated mihomo goto/nop must be preserved: %v", err)
	}
}

func TestSafetyExistingOwnershipAllowsOnlyExactSubset(t *testing.T) {
	spec := safetySpec(t)
	route := safetyObject(t, `{"table":104,"dst":"default","gateway":"192.168.1.1","dev":"eth0","metric":60104}`)
	rule := safetyObject(t, `{"priority":104,"table":104,"fwmark":"0x400000","fwmask":"0x400000"}`)
	table := safetyObject(t, `{"table":{"family":"inet","name":"configflow_return","comment":"owner-token","handle":10}}`)
	state := &returnRoutingState{Spec: spec.ReturnRoutingSpec, Ownership: &returnOwnership{Version: 1, Token: "owner-token", Metric: 60104, Routes: []map[string]any{route}, Rules: []map[string]any{rule}, NFT: []map[string]any{table}}}
	for _, resources := range []*returnResources{{}, {Routes: []map[string]any{route}}, {Routes: []map[string]any{route}, Rules: []map[string]any{rule}, NFT: []map[string]any{table}}} {
		if err := checkReturnResources(spec, state, resources); err != nil {
			t.Fatalf("owned exact subset must be reusable: %v", err)
		}
	}
	cases := map[string]*returnResources{
		"duplicate identical rule":  {Rules: []map[string]any{rule, rule}},
		"duplicate identical route": {Routes: []map[string]any{route, route}},
		"changed route":             {Routes: []map[string]any{safetyObject(t, `{"table":104,"dst":"default","gateway":"192.168.1.9","dev":"eth0","metric":60104}`)}},
		"extra route":               {Routes: []map[string]any{route, safetyObject(t, `{"table":104,"dst":"10.0.0.0/8","dev":"vpn0"}`)}},
		"changed owner comment":     {NFT: []map[string]any{safetyObject(t, `{"table":{"family":"inet","name":"configflow_return","comment":"somebody-else"}}`)}},
		"extra nft chain":           {NFT: []map[string]any{table, safetyObject(t, `{"chain":{"family":"inet","table":"configflow_return","name":"foreign"}}`)}},
	}
	for name, resources := range cases {
		t.Run(name, func(t *testing.T) {
			if err := checkReturnResources(spec, state, resources); err == nil {
				t.Fatal("drift was accepted")
			}
		})
	}
	newSpec := spec
	newSpec.Table, newSpec.Priority = 105, 105
	if err := checkReturnResources(newSpec, state, &returnResources{Routes: []map[string]any{safetyObject(t, `{"table":104,"dst":"default","dev":"foreign"}`)}}); err == nil {
		t.Fatal("changing requested table must not ignore drift in old owned table")
	}
}

func TestSafetyLegacyStateNeverConfersOwnership(t *testing.T) {
	spec := safetySpec(t)
	for _, ownership := range []*returnOwnership{nil, {Version: 0, Token: "x"}, {Version: 2, Token: "x"}, {Version: 1}} {
		if err := checkReturnResources(spec, &returnRoutingState{Spec: spec.ReturnRoutingSpec, Ownership: ownership}, &returnResources{}); err == nil {
			t.Fatal("legacy or invalid owner must not be accepted")
		}
	}
}

func TestSafetyOwnedHandlesMustRetainKernelIdentity(t *testing.T) {
	spec := safetySpec(t)
	stored := safetyObject(t, `{"table":{"family":"inet","name":"configflow_return","comment":"owner-token","handle":10}}`)
	replaced := safetyObject(t, `{"table":{"family":"inet","name":"configflow_return","comment":"owner-token","handle":11}}`)
	if !normalizedResourceEqual(stored, replaced) {
		t.Fatal("creation comparison must remain independent of handles")
	}
	if ownedResourceEqual(stored, replaced) {
		t.Fatal("a new handle cannot inherit persisted ownership")
	}
	state := &returnRoutingState{Spec: spec.ReturnRoutingSpec, Ownership: &returnOwnership{Version: 1, Token: "owner-token", NFT: []map[string]any{stored}}}
	if err := checkReturnResources(spec, state, &returnResources{NFT: []map[string]any{replaced}}); err == nil {
		t.Fatal("replacement must block at preflight, before deletion")
	}
	delete(replaced["table"].(map[string]any), "handle")
	if ownedResourceEqual(stored, replaced) {
		t.Fatal("missing recorded handle cannot prove identity")
	}
	if !normalizedResourceEqual(stored, replaced) {
		t.Fatal("handle-free creation plan must still match kernel dump")
	}
	if !ownedResourceEqual(stored, stored) {
		t.Fatal("unchanged owned identity should match")
	}
}

func TestSafetyNFTMarkExpressionsFailClosed(t *testing.T) {
	cases := []struct {
		name, expression string
		conflict         bool
	}{
		{"plain full comparison", `{"match":{"op":"==","left":{"meta":{"key":"mark"}},"right":2}}`, true},
		{"disjoint masked comparison", `{"match":{"op":"==","left":{"&":[{"meta":{"key":"mark"}},255]},"right":2}}`, false},
		{"overlapping masked comparison", `{"match":{"op":"==","left":{"&":[{"ct":{"key":"mark"}},4194304]},"right":0}}`, true},
		{"disjoint or preserves target", `{"mangle":{"key":{"ct":{"key":"mark"}},"value":{"|":[{"ct":{"key":"mark"}},8192]}}}`, false},
		{"overlapping or", `{"mangle":{"key":{"ct":{"key":"mark"}},"value":{"|":[{"ct":{"key":"mark"}},4194304]}}}`, true},
		{"and clears target", `{"mangle":{"key":{"ct":{"key":"mark"}},"value":{"&":[{"ct":{"key":"mark"}},255]}}}`, true},
		{"and preserves target", `{"mangle":{"key":{"ct":{"key":"mark"}},"value":{"&":[{"ct":{"key":"mark"}},4294967293]}}}`, false},
		{"constant assignment zero", `{"mangle":{"key":{"meta":{"key":"mark"}},"value":0}}`, true},
		{"constant assignment disjoint value still overwrites", `{"mangle":{"key":{"meta":{"key":"mark"}},"value":2}}`, true},
		{"cross mark copy", `{"mangle":{"key":{"meta":{"key":"mark"}},"value":{"ct":{"key":"mark"}}}}`, true},
		{"same mark copy", `{"mangle":{"key":{"ct":{"key":"mark"}},"value":{"ct":{"key":"mark"}}}}`, false},
		{"dynamic map", `{"mangle":{"key":{"meta":{"key":"mark"}},"value":{"map":{"key":{"payload":{"protocol":"ip","field":"saddr"}},"data":"@marks"}}}}`, true},
		{"future expression", `{"future":{"ct":{"key":"mark"}}}`, true},
		{"malformed reference cannot hide mark", `{"match":{"op":"==","left":{"ct":{"key":"mark"},"future":1},"right":0}}`, true},
		{"ordinary nonmark rule", `{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":"192.168.1.1"}}`, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			expr := safetyObject(t, test.expression)
			object := map[string]any{"rule": map[string]any{"family": "inet", "table": "other", "expr": []any{expr}}}
			err := checkReturnResources(safetySpec(t), nil, &returnResources{NFT: []map[string]any{object}})
			if (err != nil) != test.conflict {
				t.Fatalf("conflict=%v error=%v", test.conflict, err)
			}
		})
	}
}

func TestSafetyNormalizationPreservesAllSemanticChanges(t *testing.T) {
	a := safetyObject(t, `{"rule":{"family":"inet","table":"configflow_return","chain":"owned","handle":10,"comment":"owner-token","expr":[{"counter":{"packets":1,"bytes":10}},{"accept":null}]}}`)
	b := safetyObject(t, `{"rule":{"family":"inet","table":"configflow_return","chain":"owned","handle":20,"comment":"owner-token","expr":[{"counter":{"packets":300,"bytes":9999}},{"accept":null}]}}`)
	if !normalizedResourceEqual(a, b) {
		t.Fatal("runtime handles and counters must be ignored")
	}
	b["rule"].(map[string]any)["comment"] = "foreign"
	if normalizedResourceEqual(a, b) {
		t.Fatal("owner comment must remain semantic")
	}
	b = safetyObject(t, `{"rule":{"family":"inet","table":"configflow_return","chain":"owned","comment":"owner-token","expr":[{"counter":{}},{"drop":null}]}}`)
	if normalizedResourceEqual(a, b) {
		t.Fatal("verdict changes must remain semantic")
	}
	if !normalizedResourceEqual(map[string]any{"table": "104", "fwmark": "0x400000"}, map[string]any{"table": 104, "fwmark": json.Number("4194304")}) {
		t.Fatal("numeric aliases should compare canonically")
	}
	if !normalizedResourceEqual(map[string]any{"protocol": "242", "prio": -150}, map[string]any{"protocol": 242, "prio": json.Number("-150")}) {
		t.Fatal("protocol and signed nft priority must compare canonically")
	}
}

func TestSafetyResourceNumberRejectsAmbiguousTypes(t *testing.T) {
	for _, value := range []any{nil, true, false, -1, 1.5, math.NaN(), math.Inf(1), "vpn", "0x", "-1", json.Number("2.5")} {
		if _, ok := resourceNumber(value); ok {
			t.Fatalf("accepted %v", value)
		}
	}
	for _, value := range []any{104, uint64(104), float64(104), "104", "0x68", json.Number("104")} {
		if n, ok := resourceNumber(value); !ok || n != 104 {
			t.Fatalf("rejected %v", value)
		}
	}
}

func TestSafetyAnonymousSetOrderIsCanonicalButRuleOrderIsNot(t *testing.T) {
	first := safetyObject(t, `{"rule":{"family":"inet","table":"configflow_return","chain":"owned","expr":[{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":{"set":[{"prefix":{"addr":"198.18.0.0","len":16}},{"prefix":{"addr":"91.108.0.0","len":16}}]}}},{"accept":null}]}}`)
	reorderedSet := safetyObject(t, `{"rule":{"family":"inet","table":"configflow_return","chain":"owned","expr":[{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":{"set":[{"prefix":{"addr":"91.108.0.0","len":16}},{"prefix":{"addr":"198.18.0.0","len":16}}]}}},{"accept":null}]}}`)
	if !normalizedResourceEqual(first, reorderedSet) {
		t.Fatal("nft anonymous CIDR set ordering is not semantic")
	}
	if _, err := confirmResources([]map[string]any{first}, []map[string]any{reorderedSet}); err != nil {
		t.Fatalf("kernel-sorted anonymous set must confirm successfully: %v", err)
	}
	expressions := reorderedSet["rule"].(map[string]any)["expr"].([]any)
	expressions[0], expressions[1] = expressions[1], expressions[0]
	if normalizedResourceEqual(first, reorderedSet) {
		t.Fatal("accept before match must not equal match before accept")
	}
	if _, err := confirmResources([]map[string]any{first}, []map[string]any{reorderedSet}); err == nil {
		t.Fatal("changed rule expression sequence must fail confirmation")
	}
}

func TestSafetyReadAllLinksFiltersVRFWithoutEmptyPlaceholders(t *testing.T) {
	for _, withVRF := range []bool{false, true} {
		t.Run(fmt.Sprint(withVRF), func(t *testing.T) {
			links := []map[string]any{
				{"ifindex": 1, "ifname": "lo", "link_type": "loopback"},
				{"ifindex": 2, "ifname": "sit0", "linkinfo": map[string]any{"info_kind": "sit"}},
				{"ifindex": 3, "ifname": "eth0", "linkinfo": map[string]any{"info_kind": "veth"}},
				{"ifindex": 4, "ifname": "eth1", "linkinfo": map[string]any{"info_slave_kind": "vrf"}},
			}
			if withVRF {
				links = append(links, map[string]any{"ifindex": 5, "ifname": "vrf0", "linkinfo": map[string]any{"info_kind": "vrf", "info_data": map[string]any{"table": 104}}})
			}
			safetyReadFake(t, map[string]string{"ip -j -d link show": jsonText(links)}, "")
			resources, err := readReturnResources()
			if err != nil {
				t.Fatal(err)
			}
			if withVRF && len(resources.VRFs) != 1 || !withVRF && len(resources.VRFs) != 0 {
				t.Fatalf("VRFs = %v", resources.VRFs)
			}
			err = checkReturnResources(safetySpec(t), nil, resources)
			if (err != nil) != withVRF {
				t.Fatalf("VRF occupancy error = %v", err)
			}
		})
	}
}

func TestSafetyReadAllLinksRejectsIncompleteIdentity(t *testing.T) {
	for _, raw := range []string{`[{}]`, `[{"ifname":"eth0"}]`, `[{"ifindex":1}]`, `[{"ifindex":1,"ifname":"eth0","linkinfo":null}]`, `[{"ifindex":1,"ifname":"eth0","linkinfo":{"info_kind":5}}]`} {
		t.Run(raw, func(t *testing.T) {
			safetyReadFake(t, map[string]string{"ip -j -d link show": raw}, "")
			if _, err := readReturnResources(); err == nil {
				t.Fatal("incomplete links accepted")
			}
		})
	}
}

func TestReturnRoutingToolsNamesBusyBoxIPAndMissingNft(t *testing.T) {
	old := runCommand
	t.Cleanup(func() { runCommand = old })
	runCommand = func(name string, args ...string) (string, error) {
		if name == "ip" {
			return "", errors.New("exit status 1: BusyBox v1.37.0 (2025-11-23) multi-call binary.")
		}
		return "", errors.New(`exec: "nft": executable file not found in $PATH`)
	}
	_, err := readReturnResources()
	if err == nil || !strings.Contains(err.Error(), "缺少 iproute2（当前 ip 为 BusyBox 版本）、nftables") ||
		!strings.Contains(err.Error(), "apk add iproute2 nftables") {
		t.Fatalf("err=%v", err)
	}
}

func TestSafetyAnonymousSetMatchesNftMergedIntervals(t *testing.T) {
	// nftables 1.0.9 reads 91.108.4.0/22, 91.108.8.0/21 and 91.108.16.0/21 back as one range.
	written := safetyObject(t, `{"rule":{"family":"inet","table":"configflow_return","chain":"owned","expr":[{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":{"set":[{"prefix":{"addr":"91.108.4.0","len":22}},{"prefix":{"addr":"91.108.8.0","len":21}},{"prefix":{"addr":"91.108.16.0","len":21}},{"prefix":{"addr":"198.18.0.0","len":15}},"1.1.1.1"]}}},{"accept":null}]}}`)
	readBack := safetyObject(t, `{"rule":{"family":"inet","table":"configflow_return","chain":"owned","handle":7,"expr":[{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":{"set":["1.1.1.1",{"range":["91.108.4.0","91.108.23.255"]},{"prefix":{"addr":"198.18.0.0","len":15}}]}}},{"accept":null}]}}`)
	if _, err := confirmResources([]map[string]any{written}, []map[string]any{readBack}); err != nil {
		t.Fatalf("merged nft intervals must confirm the written CIDRs: %v", err)
	}
	narrower := safetyObject(t, `{"rule":{"family":"inet","table":"configflow_return","chain":"owned","expr":[{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":{"set":["1.1.1.1",{"range":["91.108.4.0","91.108.15.255"]},{"prefix":{"addr":"198.18.0.0","len":15}}]}}},{"accept":null}]}}`)
	if normalizedResourceEqual(written, narrower) {
		t.Fatal("a set matching fewer addresses must not be equal")
	}
}
