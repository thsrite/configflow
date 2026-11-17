package routers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

func runR07Helper(tc boundaryCase, endpoint string) boundaryOutcome {
	out := boundaryOutcome{Records: []map[string]any{}}
	raw, _ := json.Marshal(tc.op)
	var p struct {
		Function                           string
		NATOps                             []json.RawMessage `json:"nat_ops"`
		RouteOps                           []json.RawMessage `json:"route_ops"`
		Gateway                            string
		Sync                               routers.NATListSync
		Check                              routers.NATListCheck
		Interface                          string
		Stages                             []routers.NATStage
		Baseline                           *routers.NATBaseline
		Row                                routers.Row
		Addresses                          []routers.Row
		Exclusions, IDs, Patterns, Exclude []string
	}
	_ = json.Unmarshal(raw, &p)
	ctx := context.Background()
	d := routers.NewRouterOS(endpoint, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	var err error
	switch p.Function {
	case "removals":
		nat, route := routers.NATRemovalsFirst(p.NATOps, p.RouteOps, p.Gateway)
		out.Result = []any{nat, route}
	case "restrict":
		blocker := routers.RestrictNATEgress(&p.Sync, &p.Check, p.Interface)
		out.Result = map[string]any{"blocker": blocker, "sync": p.Sync, "check": p.Check}
	case "member":
		out.Result = routers.NATMemberUpdate(p.Stages, p.Baseline)
	case "migration":
		merged, stale := routers.NATMigration(p.Stages)
		out.Result = []int{merged, stale}
	case "policy_migrations":
		out.Result, err = routers.NATPolicyMigrations(p.Row, p.Addresses, p.Exclusions)
	case "baseline":
		out.Result, err = d.NATResources(ctx)
	case "verify":
		var state *routers.RouterState
		state, err = d.ReadState(ctx)
		if err == nil {
			err = routers.VerifyNATLists(ctx, &p.Check, state)
		}
	case "locate":
		out.Result, err = d.LocateAddresses(ctx, p.IDs)
	case "occupied":
		var names []string
		names, err = d.OccupiedAddressNames(ctx, p.Patterns, p.Exclude)
		slices.Sort(names)
		out.Result = names
	}
	if err != nil {
		out.Result = nil
		out.Error = &boundaryError{goErrorClass(err), err.Error()}
	}
	return out
}
func rawValue(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }
func TestNATListHelperBoundary(t *testing.T) {
	initial := r07State("list", 5)
	raw := r07PlannedOperation(t, initial, 6)
	var op routers.NATListSync
	_ = json.Unmarshal(rawValue(raw), &op)
	op.DstRemove = []routers.ROSResource{{Path: "/ip/firewall/address-list", ID: "*BBA", Config: routers.Row{"address": "198.51.100.2/32", "list": routers.NATDstList, "comment": routers.NATListComment, "disabled": "false"}}}
	op.Stale = []routers.ROSResource{{Path: "/ip/firewall/nat", ID: "*FA", Config: routers.Row{"dst-address": "203.0.113.0/24"}}}
	cases := []boundaryCase{}
	add := func(name string, p map[string]any) {
		cases = append(cases, boundaryCase{name: name, action: "helper", state: initial, op: p})
	}
	for _, replace := range []bool{false, true} {
		for _, withdraw := range []bool{false, true} {
			for _, hasRule := range []bool{false, true} {
				for _, routeDelete := range []bool{false, true} {
					copy := op
					copy.ReplaceRule, copy.Withdraw = replace, withdraw
					if !hasRule {
						copy.Rule = nil
					}
					routeOps := []json.RawMessage{}
					if routeDelete {
						routeOps = append(routeOps, rawValue(map[string]any{"kind": "route_delete", "id": "*1"}))
					}
					add(fmt.Sprintf("removals/%v/%v/%v/%v", replace, withdraw, hasRule, routeDelete), map[string]any{"function": "removals", "nat_ops": []json.RawMessage{rawValue(copy)}, "route_ops": routeOps, "gateway": "192.0.2.3"})
				}
			}
		}
	}
	for _, egress := range []string{"", "bridge", "ether9"} {
		for _, iface := range []string{"bridge", "ether9"} {
			for _, withdraw := range []bool{false, true} {
				copy := op
				copy.OutInterface, copy.Withdraw = egress, withdraw
				check := routers.NATListCheck{OutInterface: egress, Removed: []string{}, Sources: []string{}, Destinations: []string{}, Members: routers.NATListMembers{Src: []string{}, Dst: []string{}}}
				add(fmt.Sprintf("restrict/%s/%s/%v", egress, iface, withdraw), map[string]any{"function": "restrict", "sync": copy, "check": check, "interface": iface})
			}
		}
	}
	for _, change := range []string{"none", "rule", "source", "withdraw", "legacy", "stale", "replace", "adopt", "no_baseline"} {
		copy := op
		copy.Stale = []routers.ROSResource{}
		baseline := &routers.NATBaseline{Rule: *op.Rule, Src: []routers.ROSResource{}}
		ops := []json.RawMessage{}
		switch change {
		case "rule":
			r := *copy.Rule
			r.ID = "*FF"
			copy.Rule = &r
		case "source":
			copy.SrcAdd = []string{"192.0.2.9/32"}
		case "withdraw":
			copy.Withdraw = true
		case "legacy":
			copy.Legacy = op.Stale
		case "stale":
			copy.Stale = op.Stale
		case "replace":
			copy.ReplaceRule = true
		case "adopt":
			ops = append(ops, rawValue(map[string]any{"kind": "nat_update"}))
		case "no_baseline":
			baseline = nil
		}
		ops = append(ops, rawValue(copy))
		stages := []routers.NATStage{{Key: "routes", Ops: []json.RawMessage{rawValue(copy)}}, {Key: "nat_bypass", Ops: ops}}
		add("member/"+change, map[string]any{"function": "member", "stages": stages, "baseline": baseline})
	}
	add("migration/nested", map[string]any{"function": "migration", "stages": []routers.NATStage{{Key: "routes", Ops: []json.RawMessage{rawValue(map[string]any{"kind": "ros_policy_replace", "prepare_static": []routers.NATListSync{op}})}}, {Key: "nat_bypass", Ops: []json.RawMessage{rawValue(op)}}}})
	for _, change := range []string{"owned", "foreign", "dynamic", "disabled", "invalid", "no_overlap"} {
		address := "198.51.100.0/24"
		comment := routers.NATListComment
		dynamic, disabled := "false", "false"
		switch change {
		case "foreign":
			comment = "mine"
		case "dynamic":
			dynamic = "true"
		case "disabled":
			disabled = "true"
		case "invalid":
			address = "example.com"
		case "no_overlap":
			address = "203.0.113.0/24"
		}
		add("policy/"+change, map[string]any{"function": "policy_migrations", "row": routers.Row{"dst-address-list": routers.NATDstList}, "addresses": []routers.Row{{".id": "*A", "list": routers.NATDstList, "address": address, "comment": comment, "dynamic": dynamic, "disabled": disabled}}, "exclusions": []string{"198.51.100.0/25"}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runR07Boundary(t, tc) })
	}
}

func TestNATListReadHelpersBoundary(t *testing.T) {
	for _, variant := range []string{"new", "list", "modified", "ref"} {
		t.Run("baseline/"+variant, func(t *testing.T) {
			runR07Boundary(t, boundaryCase{action: "helper", state: r07State(variant, 3), op: map[string]any{"function": "baseline"}})
		})
	}
	for _, count := range []int{0, 1, 20, 21} {
		t.Run(fmt.Sprintf("locate/%d", count), func(t *testing.T) {
			state := r07State("list", 40)
			ids := []string{}
			for i := 0; i < count; i++ {
				ids = append(ids, fmt.Sprintf("*%X", 3000+i))
			}
			ids = append(ids, "*FFFFFF")
			runR07Boundary(t, boundaryCase{action: "helper", state: state, op: map[string]any{"function": "locate", "ids": ids}})
		})
	}
	t.Run("occupied", func(t *testing.T) {
		state := r07State("list", 3)
		state.Tables["/ip/firewall/address-list"] = append(state.Tables["/ip/firewall/address-list"], routerfake.Row{".id": "*FFFF", "list": "CF_Source_Proxy_IP_01", "address": "203.0.113.1"})
		runR07Boundary(t, boundaryCase{action: "helper", state: state, op: map[string]any{"function": "occupied", "patterns": []string{"CF_Source_Proxy_IP_[0-9]+", "cf-mihomo-nat-.*"}, "exclude": []string{routers.NATSrcList}}})
	})
}

func TestNATListVerifyBoundary(t *testing.T) {
	initial := r07State("list", 5)
	src, dst := []string{}, []string{}
	for _, row := range initial.Tables["/ip/firewall/address-list"] {
		if row["list"] == routers.NATSrcList {
			src = append(src, fmt.Sprint(row["address"]))
		} else {
			dst = append(dst, fmt.Sprint(row["address"]))
		}
	}
	for _, variant := range []string{"valid", "extra_owned", "missing_member", "withdraw", "egress", "rule_missing", "legacy_present", "mangle_unreadable", "filter_unreadable", "raw_unreadable"} {
		t.Run(variant, func(t *testing.T) {
			state := r07State("list", 5)
			check := routers.NATListCheck{Sources: src, Destinations: dst, Removed: []string{}, Members: routers.NATListMembers{Src: src, Dst: dst}}
			switch variant {
			case "extra_owned":
				state.Tables["/ip/firewall/address-list"] = append(state.Tables["/ip/firewall/address-list"], routerfake.Row{".id": "*FFFF", "list": routers.NATDstList, "address": "203.0.113.1", "comment": routers.NATListComment, "disabled": "false"})
			case "missing_member":
				state.Tables["/ip/firewall/address-list"] = state.Tables["/ip/firewall/address-list"][1:]
			case "withdraw":
				check.Withdraw = true
			case "egress":
				check.OutInterface = "bridge"
			case "rule_missing":
				state.Tables["/ip/firewall/nat"] = state.Tables["/ip/firewall/nat"][1:]
			case "legacy_present":
				check.Removed = []string{"*3"}
			case "mangle_unreadable":
				state.Faults = []routerfake.Fault{{Method: "GET", Path: "/ip/firewall/mangle"}}
			case "filter_unreadable":
				state.Faults = []routerfake.Fault{{Method: "GET", Path: "/ip/firewall/filter"}}
			case "raw_unreadable":
				state.Faults = []routerfake.Fault{{Method: "GET", Path: "/ip/firewall/raw"}}
			}
			runR07Boundary(t, boundaryCase{action: "helper", state: state, op: map[string]any{"function": "verify", "check": check}})
		})
	}
}
