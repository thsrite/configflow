package ros

import "testing"

// The list rule is inspected once per index and read, never across reads.
func TestListRuleIsCachedPerRead(t *testing.T) {
	rule := Row{".id": "*1", "chain": "srcnat", "action": "accept", "comment": NATListComment,
		"src-address-list": NATSrcList, "dst-address-list": NATDstList}
	rows := []Row{rule}
	ix := NewRouteEgressIndex(nil)
	if own := listRule(rows, ix); own == nil || own.Rule.ID() != "*1" {
		t.Fatalf("own %+v", own)
	}
	rule["action"] = "masquerade" // same read: cached
	if listRule(rows, ix) == nil {
		t.Fatal("cache not used for the same rows")
	}
	if listRule([]Row{rule}, ix) != nil {
		t.Fatal("another read must be inspected again")
	}
}
