package tasks

import (
	"context"
	"reflect"
	"testing"

	"configflow/internal/taskstatus"
)

func TestProviderRestorationIsNestedAndDoesNotFakeCompleteness(t *testing.T) {
	reg := NewRegistry(nil, nil)
	before := reg.Missing()
	t.Run("fake", func(t *testing.T) {
		for _, slot := range before {
			t.Cleanup(reg.SetRowBuilder(slot, func(context.Context, taskstatus.Snapshot) ([]*Task, error) { return nil, nil }))
		}
		if !reg.Complete() {
			t.Fatal(reg.Missing())
		}
		t.Run("override", func(t *testing.T) {
			t.Cleanup(reg.SetRowBuilder(SlotRouters, nil))
			if reg.HasRows(SlotRouters) {
				t.Fatal("override not installed")
			}
		})
		if !reg.HasRows(SlotRouters) {
			t.Fatal("nested cleanup did not restore outer provider")
		}
	})
	if !reflect.DeepEqual(reg.Missing(), before) {
		t.Fatal("fake providers leaked", reg.Missing(), before)
	}
}
