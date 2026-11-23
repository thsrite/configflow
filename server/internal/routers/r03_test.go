package routers

import (
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
	"net/netip"
	"testing"
)

func TestAddressExcludeCoversTheRest(t *testing.T) {
	a, b := netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("10.1.2.0/24")
	parts := ros.AddressExclude(a, b)
	if len(parts) != 16 {
		t.Fatalf("%d parts", len(parts))
	}
	size := uint64(1) << (32 - b.Bits())
	for i, p := range parts {
		if p.Overlaps(b) || !ros.SubnetOf(p, a) {
			t.Fatalf("part %s", p)
		}
		for _, q := range parts[i+1:] {
			if p.Overlaps(q) {
				t.Fatalf("%s overlaps %s", p, q)
			}
		}
		size += uint64(1) << (32 - p.Bits())
	}
	if size != 1<<24 {
		t.Fatalf("parts cover %d addresses", size)
	}
	if ros.AddressExclude(a, a) != nil {
		t.Fatal("a without a is empty")
	}
}

func TestNATTargetPrefersGroupedThenMostCustom(t *testing.T) {
	s := ikuai.ReadGroupRules(ikuai.GroupsFromRows([]ikuai.Row{{"id": 1.0, "group_id": "G1", "group_name": "cf_nat_1",
		"group_value": []any{map[string]any{"ip": "1.1.1.1", "comment": "cf-mihomo-nat"}}}}),
		[]ikuai.Row{
			{"id": 1.0, "action": "filter", "enabled": "yes", "comment": "cf-a", "dst_addr": map[string]any{"custom": []any{"1.1.1.1", "2.2.2.2"}}},
			{"id": 2.0, "action": "filter", "enabled": "yes", "comment": "cf-b", "dst_addr": map[string]any{"custom": []any{"3.3.3.3", "4.4.4.4"}}},
			{"id": 3.0, "action": "filter", "enabled": "yes", "comment": "cf-c", "dst_addr": map[string]any{"object": []any{map[string]any{"gid": "G1"}}}},
		}, nil)
	a, b, c := &NatBypassEntry{ID: "1"}, &NatBypassEntry{ID: "2"}, &NatBypassEntry{ID: "3"}
	if got := ikuai.NATTarget(s, []*NatBypassEntry{a, b}); got != a {
		t.Fatalf("tie: %v", got.ID)
	}
	if got := ikuai.NATTarget(s, []*NatBypassEntry{a, b, c}); got != c {
		t.Fatalf("grouped: %v", got.ID)
	}
	if ikuai.NATTarget(s, nil) != nil {
		t.Fatal("no managed rule")
	}
}
