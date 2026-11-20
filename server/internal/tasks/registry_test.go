package tasks

import (
	"context"
	"reflect"
	"testing"

	"configflow/internal/taskstatus"
)

func TestProviderRestorationIsNestedAndDoesNotFakeCompleteness(t *testing.T) {
	before := Missing()
	t.Run("fake", func(t *testing.T) {
		for _, slot := range before {
			t.Cleanup(SetRowBuilder(slot, func(context.Context, taskstatus.Snapshot) ([]*Task, error) { return nil, nil }))
		}
		if !Complete() {
			t.Fatal(Missing())
		}
		t.Run("override", func(t *testing.T) {
			t.Cleanup(SetRowBuilder(SlotRouters, nil))
			if HasRows(SlotRouters) {
				t.Fatal("override not installed")
			}
		})
		if !HasRows(SlotRouters) {
			t.Fatal("nested cleanup did not restore outer provider")
		}
	})
	if !reflect.DeepEqual(Missing(), before) {
		t.Fatal("fake providers leaked", Missing(), before)
	}
}
