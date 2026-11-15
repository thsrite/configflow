package server

import (
	"slices"
	"testing"

	"configflow/internal/tasks"
)

// C15: NewApp provides the subscription and rule-source task rows with
// their "run now"; /api/tasks still waits for the slots listed here.
func TestNewAppWiresTheScheduledRefresh(t *testing.T) {
	NewApp()
	missing := tasks.Missing()
	for _, slot := range []string{tasks.SlotSubscriptions, tasks.SlotRuleSources} {
		if slices.Contains(missing, slot) {
			t.Errorf("%s task rows not provided", slot)
		}
	}
	for _, category := range []string{"subscription", "rule_source"} {
		if tasks.Runner(category) == nil {
			t.Errorf("%s runner not provided", category)
		}
	}
	t.Logf("task slots still missing: %v", missing)
}
