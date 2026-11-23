package tasks

import (
	"context"
	"errors"
	"slices"
	"testing"

	"configflow/internal/repo"
	"configflow/internal/taskstatus"
)

func TestRegistryInstanceIsolationAndValidate(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)

	reg := NewRegistry(r, nil)
	// Missing all non-backup slots -> Validate fails closed.
	var unavail *Unavailable
	if err := reg.Validate(); !errors.As(err, &unavail) || len(unavail.Slots) != 6 {
		t.Fatalf("Validate() = %v, want 6 missing slots", err)
	}

	var order []string
	for _, slot := range []string{SlotSubscriptions, SlotRuleSources, SlotRouters, SlotAgents, SlotDomains, SlotMetrics} {
		s := slot
		reg.SetRowBuilder(s, func(context.Context, taskstatus.Snapshot) ([]*Task, error) {
			order = append(order, s)
			return []*Task{{Key: s, Category: s}}, nil
		})
	}
	if err := reg.Validate(); err != nil {
		t.Fatalf("Validate() after registering all slots: %v", err)
	}

	rows, err := reg.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	wantOrder := []string{SlotSubscriptions, SlotRuleSources, SlotRouters, SlotAgents, SlotDomains, SlotMetrics}
	if !slices.Equal(order, wantOrder) {
		t.Fatalf("row builder order = %v, want %v", order, wantOrder)
	}
	if len(rows) != len(wantOrder) {
		t.Fatalf("len(rows) = %d, want %d", len(rows), len(wantOrder))
	}
}
