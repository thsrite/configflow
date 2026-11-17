package routers_test

// Tests from the O4 HR review of R05 (server/parity/review-r05.md). The
// Python-compared cases are in ikuai_cases_test.go (ikuaiReviewCases); these
// cover what the corpus cannot compare: malformed records Python crashed on
// and the error type a release needs to roll back.

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

// writes lists the add/edit/del requests the fake received.
func writes(state routerfake.IkuaiState) []string {
	var out []string
	for _, call := range state.Calls {
		body, _ := call.Body.(map[string]any)
		if action, _ := body["action"].(string); action != "" && action != "show" {
			out = append(out, body["func_name"].(string)+"/"+action)
		}
	}
	return out
}

func wantRouterError(t *testing.T, err error, message string) {
	t.Helper()
	routerErr, ok := errors.AsType[*routers.RouterError](err)
	if !ok || routerErr.Message != message {
		t.Fatalf("error = %#v, want RouterError %q", err, message)
	}
}

// F3: an object ConfigFlow adds is found again only by its unique name. An
// add without a name is refused before it is written, and an undo without a
// name never takes a nameless row (here: the user's) for ours.
func TestIkuaiAddAndUndoNeedAName(t *testing.T) {
	nameless := ikNAT(7, "user", addressField([]string{"198.51.100.0/24"}))
	delete(nameless, "tagname")
	record := ikNAT(3, "cf-old", addressField([]string{"203.0.113.0/24"}))
	delete(record, "tagname")
	tests := []struct {
		name string
		op   map[string]any
	}{
		{"nat_readd without tagname", map[string]any{"kind": "nat_readd", "record": record}},
		{"nat_readd without record", map[string]any{"kind": "nat_readd"}},
		{"policy_route_readd null record", map[string]any{"kind": "policy_route_readd", "record": nil}},
		{"add_undo without tagname", map[string]any{"kind": "add_undo", "func": "nat_rule", "confirmed": false,
			"match": map[string]any{"action": "filter", "comment": "user"}}},
		{"add_undo with numeric tagname", map[string]any{"kind": "add_undo", "func": "nat_rule", "confirmed": true,
			"match": map[string]any{"tagname": 0, "comment": "user"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, fake := ikuaiDriver(t, routerfake.IkuaiState{Tables: tables("nat_rule", rows{nameless},
				"stream_ipport", rows{})})
			_, err := driver.Apply(context.Background(), mustJSON(t, tt.op))
			wantRouterError(t, err, "iKuai 操作格式无法识别")
			state := fake.Snapshot()
			if w := writes(state); len(w) != 0 || len(state.Tables["nat_rule"]) != 1 {
				t.Fatalf("written %v, NAT rows %d", w, len(state.Tables["nat_rule"]))
			}
		})
	}
}

// F4: a restore writes its before-image only onto the object it was read
// from. When the id now holds another object (renamed, or the id was reused
// after a delete) the restore is refused and nothing is written.
func TestIkuaiRestoreRefusesReplacedObject(t *testing.T) {
	route := ikRoute(1, "198.51.100.0/24", ikGateway, "user", "NEW_1")
	oldRoute := ikRoute(1, "198.51.100.0/24", ikGateway, "cf-user", "OLD_1")
	nat := ikNAT(1, "user", addressField([]string{"198.51.100.0/24"}))
	oldNAT := ikNAT(1, "cf-mihomo-nat", addressField([]string{"203.0.113.0/24"}))
	oldNAT["tagname"] = "NAT_OLD"
	policy := ikPolicy(1, "user", addressField([]string{"198.51.100.0/24"}))
	oldPolicy := ikPolicy(1, "cf-mihomo", addressField([]string{"203.0.113.0/24"}))
	oldPolicy["tagname"] = "PR_OLD"
	tests := []struct {
		name, kind, table, message string
		current, record            routerfake.Row
	}{
		{"route", "route_restore", "static_rt", "iKuai static_rt 中 id=1 的对象已被替换或改名，未自动恢复，请检查", route, oldRoute},
		{"nat", "nat_restore", "nat_rule", "iKuai nat_rule 中 id=1 的对象已被替换或改名，未自动恢复，请检查", nat, oldNAT},
		{"policy", "policy_route_restore", "stream_ipport", "iKuai stream_ipport 中 id=1 的对象已被替换或改名，未自动恢复，请检查", policy, oldPolicy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, fake := ikuaiDriver(t, routerfake.IkuaiState{Tables: tables(tt.table, rows{tt.current})})
			_, err := driver.Apply(context.Background(), mustJSON(t, map[string]any{"kind": tt.kind, "record": tt.record}))
			wantRouterError(t, err, tt.message)
			state := fake.Snapshot()
			if w := writes(state); len(w) != 0 || state.Tables[tt.table][0]["comment"] != "user" {
				t.Fatalf("written %v, row %v", w, state.Tables[tt.table][0])
			}
		})
	}
}

// F2: refusals on the release path are RouterErrors with Chinese messages
// (a release rolls back on RouterError; Python crashed here with ValueError).
func TestIkuaiRouteRefusalsAreRouterErrors(t *testing.T) {
	bad := ikRoute(12, "203.0.113.0/24", ikGateway, "cf-lab", "")
	bad["netmask"] = "255.0.255.0"
	tests := []struct {
		name, message string
		op            map[string]any
	}{
		{"route_delete unreadable network", "iKuai static_rt 中 id=12 的目标网段无法识别，未删除，请检查", byID("route_delete", 12)},
		{"route_add invalid network", "iKuai 操作格式无法识别", routeAdd("198.51.100.1/24", "cf-x")},
		{"route_add IPv6", "iKuai 操作格式无法识别", routeAdd("2001:db8::/32", "cf-x")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, fake := ikuaiDriver(t, routerfake.IkuaiState{Tables: tables("static_rt", rows{bad})})
			_, err := driver.Apply(context.Background(), mustJSON(t, tt.op))
			wantRouterError(t, err, tt.message)
			if w := writes(fake.Snapshot()); len(w) != 0 {
				t.Fatalf("written %v", w)
			}
		})
	}
}
