package tasks

import (
	"context"
	"reflect"
	"testing"

	"configflow/internal/jsonx"
)

func TestProviderRestorationIsNestedAndDoesNotFakeCompleteness(t *testing.T) {
	before := Missing()
	t.Run("fake", func(t *testing.T) {
		for _, slot := range before {
			t.Cleanup(SetRows(slot, func(context.Context, *jsonx.Object) ([]*jsonx.Object, error) { return nil, nil }))
		}
		if !Complete() {
			t.Fatal(Missing())
		}
		t.Run("override", func(t *testing.T) {
			t.Cleanup(SetRows(SlotRouters, nil))
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
