package routers

import (
	"fmt"
	"slices"
	"testing"
)

// ikuaiGroupRow is a decoded route_object row of a ConfigFlow group of role
// holding values (the first entry carries the marker).
func ikuaiGroupRow(role string, index int, values []string) Row {
	entries := []any{}
	for i, v := range values {
		comment := ""
		if i == 0 {
			comment = GroupMarkers[role]
		}
		entries = append(entries, map[string]any{"ip": v, "comment": comment})
	}
	return Row{"id": float64(index), "group_id": fmt.Sprintf("IPGP%d", index), "group_name": GroupName(role, index),
		"type": 0.0, "group_value": entries}
}

// Review R03 (Python bug fixed in Go): repacking a fragmented family must not
// queue the new CIDRs the first pass already placed a second time. Python
// wrote cf_pa_1 with 52 entries here (the new CIDR twice).
func TestLayoutRepackQueuesNewEntriesOnce(t *testing.T) {
	var rows []Row
	var want []string
	for index := 1; index <= 5; index++ {
		var values []string
		for i := range 10 {
			values = append(values, fmt.Sprintf("10.%d.%d.0/24", index, i))
		}
		rows = append(rows, ikuaiGroupRow("pa", index, values))
		want = append(want, values...)
	}
	want = append(want, "198.51.100.0/24")
	state := ReadGroupRules(GroupsFromRows(rows), nil, nil)
	var warnings, blockers []string
	p := NewGroupPlanner(state, &warnings, &blockers)
	final, ops := p.Family("pa", want, "")
	if !slices.Equal(final, []string{"cf_pa_1"}) || len(blockers) > 0 {
		t.Fatalf("final %v blockers %v", final, blockers)
	}
	// cf_pa_1 keeps its entries, takes the new CIDR, then everything moved down.
	expected := slices.Concat(want[:10], want[50:], want[10:50])
	got := p.Contents["pa"]["cf_pa_1"]
	if !slices.Equal(got, expected) {
		t.Fatalf("cf_pa_1 holds %d entries: %v", len(got), got)
	}
	if len(ops) != 1 || ops[0].Kind != OpGroupUpdate || len(ops[0].Added) != 41 || len(ops[0].Removed) != 0 ||
		ops[0].Summary != "更新 IP 分组「cf_pa_1」：加入 41 个网段（现 51 个）：加入 198.51.100.0/24、10.2.0.0/24、10.2.1.0/24、10.2.2.0/24、10.2.3.0/24…" {
		t.Fatalf("ops %+v", ops)
	}
	deletes := p.Cleanup("pa")
	if len(deletes) != 4 || deletes[0].Name != "cf_pa_2" || deletes[3].Name != "cf_pa_5" {
		t.Fatalf("cleanup %+v", deletes)
	}
}
