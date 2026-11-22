package upgrades

import (
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/model"
)

// TestReviewN810CorruptFollowWarningInterval: the refused-follow warning
// repeats once the interval has passed, separately per damaged field.
func TestReviewN810CorruptFollowWarningInterval(t *testing.T) {
	ResetAddressState()
	t.Cleanup(ResetAddressState)
	a := &model.Agent{}
	start := time.Unix(1_800_000_000, 0)
	logged := func(field string, at time.Time) bool {
		t.Helper()
		key := corruptKey{"agent-1", field}
		previous, seen := corruptWarned[key]
		warnCorruptFollow(a, "agent-1", "192.0.2.77", &CorruptStateError{Field: field}, at)
		return !seen || !corruptWarned[key].Equal(previous)
	}
	tests := []struct {
		name  string
		field string
		at    time.Time
		want  bool
	}{
		{"first", "oxidns_ha.enabled", start, true},
		{"within interval", "oxidns_ha.enabled", start.Add(CorruptWarnInterval - time.Second), false},
		{"other field", agents.HAField + ".status", start.Add(time.Second), true},
		{"after interval", "oxidns_ha.enabled", start.Add(CorruptWarnInterval), true},
	}
	for _, test := range tests {
		if got := logged(test.field, test.at); got != test.want {
			t.Fatalf("%s: warned=%v, want %v", test.name, got, test.want)
		}
	}
}
