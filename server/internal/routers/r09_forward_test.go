package routers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"configflow/internal/routers/routerfake"
)

type r09ForwardCase struct {
	Name       string             `json:"name"`
	State      routerfake.State   `json:"state"`
	Family     int                `json:"family"`
	LAN        string             `json:"lan"`
	Targets    []string           `json:"targets"`
	Router     *Router            `json:"router"`
	Desired    *Desired           `json:"desired"`
	Report     json.RawMessage    `json:"report"`
	PolicyPlan *ROSPolicyPlan     `json:"policy_plan,omitempty"`
	PolicyLive *ROSPolicySnapshot `json:"policy_live,omitempty"`
	GuardLive  *DNSGuardSnapshot  `json:"guard_live,omitempty"`
}

func r09ForwardBase() r09ForwardCase {
	base := r09Base("forward")
	base.State.Tables["/interface"] = []routerfake.Row{{".id": "*1", "name": "bridge"}, {".id": "*2", "name": "ether1"}}
	base.State.Tables["/interface/list"] = []routerfake.Row{{".id": "*1", "name": "LAN"}, {".id": "*2", "name": "WAN"}}
	base.State.Tables["/interface/list/member"] = []routerfake.Row{{".id": "*1", "list": "LAN", "interface": "bridge"}, {".id": "*2", "list": "WAN", "interface": "ether1"}}
	base.State.Tables[rosFilter] = []routerfake.Row{
		{".id": "*1", "chain": "forward", "action": "passthrough", "dynamic": "true"},
		{".id": "*2", "chain": "input", "action": "drop", "connection-state": "invalid"},
		{".id": "*3", "chain": "forward", "action": "accept", "ipsec-policy": "in,ipsec"},
		{".id": "*4", "chain": "forward", "action": "accept", "ipsec-policy": "out,ipsec"},
		{".id": "*5", "chain": "forward", "action": "fasttrack-connection", "connection-state": "established,related", "hw-offload": "true"},
		{".id": "*6", "chain": "forward", "action": "accept", "connection-state": "established,related,untracked"},
		{".id": "*7", "chain": "forward", "action": "drop", "connection-state": "invalid"},
		{".id": "*8", "chain": "forward", "action": "drop", "connection-state": "new", "connection-nat-state": "!dstnat", "in-interface-list": "WAN"},
	}
	return r09ForwardCase{Name: "default", State: base.State, Family: 4, LAN: "bridge", Targets: []string{"198.18.0.0/16", "198.51.100.0/24"}, Router: &Router{RouterSettings: RouterSettings{Type: "routeros"}}, Desired: base.Desired, Report: r09Raw(map[string]any{"routes": map[string]any{"items": []map[string]string{{"cidr": "198.18.0.0/16", "status": "missing"}, {"cidr": "198.51.100.0/24", "status": "synced"}}}})}
}
func r09ForwardCases() []r09ForwardCase {
	cases := []r09ForwardCase{}
	for _, variant := range []string{"default", "early_accept", "late_accept", "renamed", "custom_drop", "jump", "default_deny", "empty", "disabled", "input", "wan_only", "new_only", "unknown_accept", "target_only", "address_list", "quoted_lan", "escaped_lan", "unreadable", "no_lan", "ikuai", "return_routing", "no_targets", "bad_gateway", "policy_only", "policy_blocked", "policy_disabled", "dns_snapshot", "policy_snapshot", "policy_empty_snapshot", "policy_missing_filter", "ipv6"} {
		tc := r09ForwardBase()
		tc.Name = variant
		rows := tc.State.Tables[rosFilter]
		same := routerfake.Row{".id": "*F", "chain": "forward", "action": "accept", "in-interface": "bridge", "out-interface": "bridge"}
		switch variant {
		case "early_accept":
			rows = append([]routerfake.Row{same}, rows...)
		case "late_accept":
			rows = append(rows, same)
		case "renamed":
			rows[6]["comment"] = "synthetic 用户规则"
		case "custom_drop":
			rows = append([]routerfake.Row{{".id": "*C", "chain": "forward", "action": "drop", "connection-state": "invalid", "in-interface-list": "LAN"}}, rows...)
		case "jump":
			rows = []routerfake.Row{{".id": "*A", "chain": "forward", "action": "jump", "jump-target": "child"}, {".id": "*B", "chain": "child", "action": "drop", "connection-state": "invalid"}}
		case "default_deny":
			rows = []routerfake.Row{rows[5], {".id": "*D", "chain": "forward", "action": "drop"}}
		case "empty":
			rows = []routerfake.Row{}
		case "disabled":
			rows = []routerfake.Row{rows[6]}
			rows[0]["disabled"] = "true"
		case "input":
			rows = []routerfake.Row{rows[6]}
			rows[0]["chain"] = "input"
		case "wan_only":
			rows = []routerfake.Row{rows[6]}
			rows[0]["in-interface-list"] = "WAN"
		case "new_only":
			rows = []routerfake.Row{rows[6]}
			rows[0]["connection-state"] = "new"
		case "unknown_accept":
			rows = append([]routerfake.Row{{".id": "*A", "chain": "forward", "action": "accept", "packet-mark": "unknown"}}, rows...)
		case "target_only":
			rows = []routerfake.Row{{".id": "*A", "chain": "forward", "action": "drop", "dst-address": "198.51.100.0/24"}}
		case "address_list":
			rows = []routerfake.Row{{".id": "*A", "chain": "forward", "action": "drop", "dst-address-list": "proxied"}}
			tc.State.Tables[rosAddresses] = []routerfake.Row{{".id": "*A", "list": "proxied", "address": "198.18.0.0/15"}, {".id": "*B", "list": "other", "address": "203.0.113.1"}}
		case "quoted_lan":
			tc.LAN = "LAN bridge"
		case "escaped_lan":
			tc.LAN = `LAN "$bridge\`
		case "unreadable":
			tc.State.Faults = []routerfake.Fault{{Method: "GET", Path: rosFilter, Status: 403}}
		case "no_lan":
			tc.State.Tables[rosRoutes] = []routerfake.Row{}
		case "ikuai":
			tc.Router.Type = "ikuai"
		case "return_routing":
			tc.Router.ManageReturnRouting = true
		case "no_targets":
			tc.Report = r09Raw(map[string]any{"routes": map[string]any{"items": []map[string]string{{"cidr": "198.18.0.0/16", "status": "conflict"}}}})
		case "bad_gateway":
			tc.Desired.ProxyGateway = "not-an-ip"
		case "policy_only", "policy_blocked", "policy_disabled":
			tc.Desired.ROSPolicy = &ROSPolicyDesired{Selected: []string{"synthetic-policy"}}
			tc.PolicyPlan = &ROSPolicyPlan{Items: []ROSPolicyItem{{ProxyCIDR: ProxyCIDR{CIDR: "198.18.0.0/16"}}}, Blockers: []string{}}
			if variant == "policy_blocked" {
				tc.PolicyPlan.Blockers = []string{"synthetic blocker"}
			}
			if variant == "policy_disabled" {
				tc.Desired.ROSPolicy.Selected = nil
			}
			tc.Report = r09Raw(map[string]any{"routes": map[string]any{"items": []any{}}, "policy_routing": map[string]any{"enabled": len(tc.Desired.ROSPolicy.Selected) > 0, "blockers": tc.PolicyPlan.Blockers, "items": tc.PolicyPlan.Items}})
		case "dns_snapshot", "policy_snapshot", "policy_empty_snapshot", "policy_missing_filter":
			tc.GuardLive = &DNSGuardSnapshot{rosFilter: {Row(same)}}
			switch variant {
			case "policy_snapshot":
				tc.PolicyLive = &ROSPolicySnapshot{rosFilter: {{".id": "*D", "chain": "forward", "action": "drop"}}}
			case "policy_empty_snapshot":
				tc.PolicyLive = &ROSPolicySnapshot{rosFilter: {}}
			case "policy_missing_filter":
				tc.PolicyLive = &ROSPolicySnapshot{}
			}
		case "ipv6":
			tc.Family = 6
			tc.Targets = []string{"2001:db8::/32"}
			tc.State.Tables["/ipv6/firewall/filter"] = rows
		}
		tc.State.Tables[rosFilter] = rows
		cases = append(cases, tc)
	}
	return cases
}
func TestR09ForwardHintPython(t *testing.T) {
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, tc := range r09ForwardCases() {
		t.Run(tc.Name, func(t *testing.T) {
			var want map[string]any
			if err := json.Unmarshal(r09Python(t, "r09_forward_reference.py", tc, srv.URL), &want); err != nil {
				t.Fatal(err)
			}
			f.Reset(tc.State)
			d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			check := CheckForwardHint(context.Background(), d, tc.Family, tc.LAN, tc.Targets, nil)
			var report Report
			if err := json.Unmarshal(tc.Report, &report); err != nil {
				t.Fatal(err)
			}
			routes := []Row{}
			for _, row := range tc.State.Tables[rosRoutes] {
				routes = append(routes, Row(row))
			}
			state := &RouterState{NatContext: &NatContext{Routes: routes}, ROSPolicyPlan: tc.PolicyPlan, ROSPolicy: tc.PolicyLive, DNSGuard: tc.GuardLive}
			hint, err := ForwardHint(context.Background(), tc.Router, tc.Desired, state, &report, d)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := f.Snapshot()
			got := r09JSON(t, map[string]any{"warning4": ForwardDropWarning(4), "warning6": ForwardDropWarning(6), "check": check, "hint": hint, "calls": snapshot.Calls, "tables": snapshot.Tables})
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("hint boundary differs\nPython %s\nGo %s", r09Raw(want), r09Raw(got))
			}
			for _, call := range snapshot.Calls {
				if call.Method != "GET" {
					t.Fatalf("advisory wrote %s %s", call.Method, call.Path)
				}
			}
		})
	}
}
