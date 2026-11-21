package routes

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func allocationRouting() mihomoRouting {
	return mihomoRouting{marks: map[string]uint32{}, tableIndex: 2022, ruleIndex: 9000}
}

func allocationRoute(table int) map[string]interface{} {
	return map[string]interface{}{"dst": "default", "table": table, "dev": "other0"}
}

func allocationRule(priority int) map[string]interface{} {
	return map[string]interface{}{"priority": priority, "table": 254, "src": "all"}
}

func allocationMarkNFT(expression interface{}) map[string]interface{} {
	return map[string]interface{}{"rule": map[string]interface{}{
		"family": "inet", "table": "other", "chain": "prerouting",
		"expr": []interface{}{expression},
	}}
}

func allocationMarkOR(mark uint32) map[string]interface{} {
	reference := map[string]interface{}{"meta": map[string]interface{}{"key": "mark"}}
	return allocationMarkNFT(map[string]interface{}{"mangle": map[string]interface{}{
		"key": reference, "value": map[string]interface{}{"|": []interface{}{reference, mark}},
	}})
}

func TestReturnAllocationPrefersDefaultsAndDoesNotWrite(t *testing.T) {
	oldCommand, oldInput := runCommand, runCommandInput
	runCommand = func(string, ...string) (string, error) {
		t.Fatal("allocation must not execute commands")
		return "", nil
	}
	runCommandInput = func(string, string, ...string) (string, error) {
		t.Fatal("allocation must not execute mutating commands")
		return "", nil
	}
	t.Cleanup(func() { runCommand, runCommandInput = oldCommand, oldInput })
	resources := &returnResources{
		Routes: []map[string]interface{}{{"dst": "default", "gateway": "192.168.1.1", "dev": "eth0"}},
		Rules:  []map[string]interface{}{allocationRule(0), allocationRule(32766)},
	}
	before, _ := json.Marshal(resources)
	for i := 0; i < 2; i++ {
		got, err := selectReturnAllocation(nil, resources, allocationRouting())
		want := returnAllocation{Version: 1, Table: 104, Priority: 104, Mark: "0x400000"}
		if err != nil || got != want {
			t.Fatalf("got %+v, %v; want %+v", got, err, want)
		}
		encoded, _ := json.Marshal(got)
		if string(encoded) != `{"version":1,"table":104,"priority":104,"mark":"0x400000","reused":false}` {
			t.Fatalf("allocation JSON contract: %s", encoded)
		}
	}
	after, _ := json.Marshal(resources)
	if string(before) != string(after) {
		t.Fatal("allocation changed its resource snapshot")
	}
}

func TestReturnAllocationSelectsIndependentDimensions(t *testing.T) {
	cases := []struct {
		name      string
		resources *returnResources
		configure func(*mihomoRouting)
		table     int
		priority  int
		mark      string
	}{
		{"route table occupied", &returnResources{Routes: []map[string]interface{}{allocationRoute(104)}}, nil, 105, 104, "0x400000"},
		{"rule table referenced", &returnResources{Rules: []map[string]interface{}{{"priority": 5, "table": "104"}}}, nil, 105, 104, "0x400000"},
		{"priority occupied", &returnResources{Rules: []map[string]interface{}{allocationRule(104)}}, nil, 104, 105, "0x400000"},
		{"goto target referenced", &returnResources{Rules: []map[string]interface{}{{"priority": 5, "goto": 104}}}, nil, 104, 105, "0x400000"},
		{"VRF table occupied", &returnResources{VRFs: []map[string]interface{}{{"ifindex": 7, "ifname": "vrf0", "linkinfo": map[string]interface{}{"info_kind": "vrf", "info_data": map[string]interface{}{"table": 104}}}}}, nil, 105, 104, "0x400000"},
		{"mihomo table", &returnResources{}, func(r *mihomoRouting) { r.tableIndex = 104 }, 105, 104, "0x400000"},
		{"mihomo priority bound", &returnResources{}, func(r *mihomoRouting) { r.ruleIndex = 104 }, 104, 1, "0x400000"},
		{"mihomo mark", &returnResources{}, func(r *mihomoRouting) { r.marks["routing-mark"] = 0x400000 }, 104, 104, "0x800000"},
		{"zero mark still reserves mask", &returnResources{Rules: []map[string]interface{}{{"priority": 5, "table": 254, "fwmark": "0x0/0x400000"}}}, nil, 104, 104, "0x800000"},
		{"explicit mask reserves bit", &returnResources{Rules: []map[string]interface{}{{"priority": 5, "table": 254, "fwmark": "0", "fwmask": "0x400000"}}}, nil, 104, 104, "0x800000"},
		{"external NFT bit assignment", &returnResources{NFT: []map[string]interface{}{allocationMarkOR(0x400000)}}, nil, 104, 104, "0x800000"},
		{"unrelated NFT bit assignment", &returnResources{NFT: []map[string]interface{}{allocationMarkOR(0x1)}}, nil, 104, 104, "0x400000"},
		{"high mark bits exhausted", &returnResources{}, func(r *mihomoRouting) { r.marks["routing-mark"] = 0xffc00000 }, 104, 104, "0x1"},
		{"highest mark bit available", &returnResources{}, func(r *mihomoRouting) { r.marks["routing-mark"] = 0x7fffffff }, 104, 104, "0x80000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routing := allocationRouting()
			if tc.configure != nil {
				tc.configure(&routing)
			}
			got, err := selectReturnAllocation(nil, tc.resources, routing)
			want := returnAllocation{Version: 1, Table: tc.table, Priority: tc.priority, Mark: tc.mark}
			if err != nil || got != want {
				t.Fatalf("got %+v, %v; want %+v", got, err, want)
			}
		})
	}
}

func TestReturnAllocationFallsBackToLowNumbers(t *testing.T) {
	resources := &returnResources{}
	for table := 104; table <= 252; table++ {
		resources.Routes = append(resources.Routes, allocationRoute(table))
	}
	routing := allocationRouting()
	routing.ruleIndex = 110
	for priority := 104; priority < routing.ruleIndex; priority++ {
		resources.Rules = append(resources.Rules, allocationRule(priority))
	}
	resources.Routes = append(resources.Routes, allocationRoute(1))
	resources.Rules = append(resources.Rules, map[string]interface{}{"priority": 1, "goto": 2})
	got, err := selectReturnAllocation(nil, resources, routing)
	if err != nil || got.Table != 2 || got.Priority != 3 {
		t.Fatalf("got %+v, %v; want table 2 and priority 3", got, err)
	}
}

func TestReturnAllocationRejectsExhaustedOrUnknownResources(t *testing.T) {
	fullTables, fullPriorities := &returnResources{}, &returnResources{}
	for i := 1; i <= 252; i++ {
		fullTables.Routes = append(fullTables.Routes, allocationRoute(i))
	}
	for i := 1; i < 106; i++ {
		fullPriorities.Rules = append(fullPriorities.Rules, allocationRule(i))
	}
	cases := []struct {
		name      string
		resources *returnResources
		configure func(*mihomoRouting)
	}{
		{"missing snapshot", nil, nil},
		{"all tables occupied", fullTables, nil},
		{"all earlier priorities occupied", fullPriorities, func(r *mihomoRouting) { r.ruleIndex = 106 }},
		{"no valid priority bound", &returnResources{}, func(r *mihomoRouting) { r.ruleIndex = 1 }},
		{"all marks occupied", &returnResources{}, func(r *mihomoRouting) { r.marks["routing-mark"] = ^uint32(0) }},
		{"implicit full rule mask", &returnResources{Rules: []map[string]interface{}{{"priority": 5, "table": 254, "fwmark": "0x1"}}}, nil},
		{"unexplained NFT mark operation", &returnResources{NFT: []map[string]interface{}{allocationMarkNFT(map[string]interface{}{"mangle": map[string]interface{}{"key": map[string]interface{}{"meta": map[string]interface{}{"key": "mark"}}, "value": map[string]interface{}{"map": "dynamic_marks"}}})}}, nil},
		{"unknown fixed NFT table", &returnResources{NFT: []map[string]interface{}{{"table": map[string]interface{}{"family": "inet", "name": managedReturnTable}}}}, nil},
		{"incomplete route", &returnResources{Routes: []map[string]interface{}{{}}}, nil},
		{"unknown route table", &returnResources{Routes: []map[string]interface{}{{"dst": "default", "table": "custom_alias"}}}, nil},
		{"unknown rule priority", &returnResources{Rules: []map[string]interface{}{{"priority": "bad", "table": 254}}}, nil},
		{"unknown rule table", &returnResources{Rules: []map[string]interface{}{{"priority": 5, "table": "custom_alias"}}}, nil},
		{"unknown rule mask", &returnResources{Rules: []map[string]interface{}{{"priority": 5, "table": 254, "fwmark": "1/bad"}}}, nil},
		{"unknown goto target", &returnResources{Rules: []map[string]interface{}{{"priority": 5, "goto": "104"}}}, nil},
		{"unresolved goto target", &returnResources{Rules: []map[string]interface{}{{"priority": 5, "goto": 104, "unresolved": nil}}}, nil},
		{"unknown VRF table", &returnResources{VRFs: []map[string]interface{}{{"linkinfo": map[string]interface{}{"info_kind": "vrf", "info_data": map[string]interface{}{}}}}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routing := allocationRouting()
			if tc.configure != nil {
				tc.configure(&routing)
			}
			if got, err := selectReturnAllocation(nil, tc.resources, routing); err == nil || got != (returnAllocation{}) {
				t.Fatalf("unsafe allocation accepted: %+v, %v", got, err)
			}
		})
	}
}

func allocationOwnedState() (*returnRoutingState, *returnResources) {
	owner := &returnOwnership{Version: 1, Token: "0123456789abcdef0123456789abcdef", Metric: 60104}
	owner.Routes = []map[string]interface{}{{"dst": "default", "table": 105, "protocol": returnProtocol, "metric": owner.Metric, "gateway": "192.168.1.1", "dev": "eth0"}}
	owner.Rules = []map[string]interface{}{{"priority": 205, "table": 105, "protocol": returnProtocol, "fwmark": "0x800000", "fwmask": "0x800000", "src": "all"}}
	owner.NFT = []map[string]interface{}{{"table": map[string]interface{}{"family": "inet", "name": managedReturnTable, "comment": ownerTag(owner), "handle": 10}}}
	state := &returnRoutingState{Active: true, Ownership: owner, Spec: ReturnRoutingSpec{
		SafetyVersion: 1, RouterIP: "192.168.1.1", ProxyDst: []string{"198.18.0.0/16"},
		Mark: "0x800000", Table: 105, Priority: 205, LanInterface: "eth0",
	}}
	// Round-trip ensures mutations of the live snapshot cannot change the
	// expected ownership manifest through shared map references.
	encoded, _ := json.Marshal(&returnResources{Routes: owner.Routes, Rules: owner.Rules, NFT: owner.NFT})
	resources := &returnResources{}
	_ = json.Unmarshal(encoded, resources)
	return state, resources
}

func TestReturnAllocationReusesOwnedResourcesIncludingMissingObjects(t *testing.T) {
	for _, missing := range []string{"none", "route", "rule", "nft", "all"} {
		t.Run(missing, func(t *testing.T) {
			state, resources := allocationOwnedState()
			switch missing {
			case "route":
				resources.Routes = nil
			case "rule":
				resources.Rules = nil
			case "nft":
				resources.NFT = nil
			case "all":
				resources = &returnResources{}
			}
			before, _ := json.Marshal(state)
			got, err := selectReturnAllocation(state, resources, allocationRouting())
			want := returnAllocation{Version: 1, Table: 105, Priority: 205, Mark: "0x800000", Reused: true}
			if err != nil || got != want {
				t.Fatalf("got %+v, %v; want %+v", got, err, want)
			}
			after, _ := json.Marshal(state)
			if string(before) != string(after) {
				t.Fatal("allocation modified the persisted state")
			}
		})
	}
}

func TestReturnAllocationRefusesToMoveChangedOrUnownedState(t *testing.T) {
	cases := []struct {
		name   string
		change func(*returnRoutingState, *returnResources, *mihomoRouting)
	}{
		{"legacy state", func(s *returnRoutingState, _ *returnResources, _ *mihomoRouting) { s.Ownership = nil }},
		{"legacy backup", func(s *returnRoutingState, _ *returnResources, _ *mihomoRouting) { s.Legacy = &legacyBackup{} }},
		{"pending transaction with unprovable chain", func(s *returnRoutingState, r *returnResources, _ *mihomoRouting) {
			s.Ownership.Pending = true
			r.NFT = append(r.NFT, map[string]interface{}{"chain": map[string]interface{}{"family": "inet", "table": managedReturnTable, "name": "cf_ffffffffffffffffffffffffffffffff_pre"}})
		}},
		{"pending transaction with foreign route", func(s *returnRoutingState, r *returnResources, _ *mihomoRouting) {
			s.Ownership.Pending = true
			r.Routes = append(r.Routes, allocationRoute(105))
		}},
		{"unknown ownership version", func(s *returnRoutingState, _ *returnResources, _ *mihomoRouting) { s.Ownership.Version = 2 }},
		{"invalid token", func(s *returnRoutingState, _ *returnResources, _ *mihomoRouting) { s.Ownership.Token = "not-an-owner" }},
		{"invalid persisted spec", func(s *returnRoutingState, _ *returnResources, _ *mihomoRouting) { s.Spec.SafetyVersion = 0 }},
		{"changed route", func(_ *returnRoutingState, r *returnResources, _ *mihomoRouting) {
			r.Routes[0]["gateway"] = "192.168.1.254"
		}},
		{"additional route", func(_ *returnRoutingState, r *returnResources, _ *mihomoRouting) {
			r.Routes = append(r.Routes, allocationRoute(105))
		}},
		{"changed rule", func(_ *returnRoutingState, r *returnResources, _ *mihomoRouting) {
			r.Rules[0]["src"] = "192.168.2.0/24"
		}},
		{"duplicate rule", func(_ *returnRoutingState, r *returnResources, _ *mihomoRouting) {
			r.Rules = append(r.Rules, r.Rules[0])
		}},
		{"changed NFT handle", func(_ *returnRoutingState, r *returnResources, _ *mihomoRouting) {
			r.NFT[0]["table"].(map[string]interface{})["handle"] = 11
		}},
		{"additional NFT object", func(_ *returnRoutingState, r *returnResources, _ *mihomoRouting) {
			r.NFT = append(r.NFT, map[string]interface{}{"chain": map[string]interface{}{"family": "inet", "table": managedReturnTable, "name": "foreign"}})
		}},
		{"new mihomo mark overlap", func(_ *returnRoutingState, _ *returnResources, r *mihomoRouting) { r.marks["routing-mark"] = 0x800000 }},
		{"new mihomo table overlap", func(_ *returnRoutingState, _ *returnResources, r *mihomoRouting) { r.tableIndex = 105 }},
		{"new priority bound", func(_ *returnRoutingState, _ *returnResources, r *mihomoRouting) { r.ruleIndex = 205 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, resources := allocationOwnedState()
			routing := allocationRouting()
			tc.change(state, resources, &routing)
			if got, err := selectReturnAllocation(state, resources, routing); err == nil || got != (returnAllocation{}) {
				t.Fatalf("unsafe state was reused or migrated: %+v, %v", got, err)
			}
		})
	}
}

func TestReturnAllocationFinalCombinationPassesBothSafetyChecks(t *testing.T) {
	resources := &returnResources{Routes: []map[string]interface{}{allocationRoute(104)},
		Rules: []map[string]interface{}{{"priority": 104, "goto": 105}, {"priority": 12, "table": 106, "fwmark": "0/0xc00000"}},
		NFT:   []map[string]interface{}{allocationMarkOR(0x1000000)},
	}
	routing := allocationRouting()
	routing.tableIndex = 105
	routing.marks["routing-mark"] = 0x2000000
	got, err := selectReturnAllocation(nil, resources, routing)
	if err != nil {
		t.Fatal(err)
	}
	want := returnAllocation{Version: 1, Table: 107, Priority: 106, Mark: "0x4000000"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	mark, _ := parseMark(got.Mark)
	spec := resolvedSpec{ReturnRoutingSpec: ReturnRoutingSpec{Table: got.Table, Priority: got.Priority, Mark: got.Mark}, mark: mark}
	if err := checkMihomoConflicts(spec, routing); err != nil {
		t.Fatal(err)
	}
	if err := checkReturnResources(spec, nil, resources); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("0x%x", mark) != got.Mark || mark&(mark-1) != 0 {
		t.Fatalf("mark must be canonical and single-bit: %s", got.Mark)
	}
}
