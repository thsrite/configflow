package notifications

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

func TestServiceSetAgentsTwoPhaseAndValidate(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)

	reg := tasks.NewRegistry(r, nil)
	svc := New(Options{Repo: r, Tasks: reg})

	// Unwired agents source fails Validate.
	if err := svc.Validate(); !errors.Is(err, ErrSourcesMissing) {
		t.Fatalf("Validate without agents = %v, want ErrSourcesMissing", err)
	}

	// First SetAgents succeeds.
	if err := svc.SetAgents(func(context.Context) ([]model.Agent, error) { return nil, nil }); err != nil {
		t.Fatalf("first SetAgents: %v", err)
	}
	// Second SetAgents fails (D6: set once).
	if err := svc.SetAgents(func(context.Context) ([]model.Agent, error) { return nil, nil }); !errors.Is(err, ErrAgentsAlreadySet) {
		t.Fatalf("second SetAgents = %v, want ErrAgentsAlreadySet", err)
	}

	// Missing task slots still fails Validate.
	var unavail *tasks.Unavailable
	if err := svc.Validate(); !errors.As(err, &unavail) {
		t.Fatalf("Validate with missing tasks = %v, want *tasks.Unavailable", err)
	}

	for _, slot := range []string{tasks.SlotSubscriptions, tasks.SlotRuleSources, tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains, tasks.SlotMetrics} {
		reg.SetRowBuilder(slot, func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil })
	}
	if err := svc.Validate(); err != nil {
		t.Fatalf("Validate when complete: %v", err)
	}

	// SetAgents after MarkStarted fails.
	svc2 := New(Options{Repo: r, Tasks: reg})
	svc2.MarkStarted()
	if err := svc2.SetAgents(func(context.Context) ([]model.Agent, error) { return nil, nil }); !errors.Is(err, ErrServiceStarted) {
		t.Fatalf("SetAgents after MarkStarted = %v, want ErrServiceStarted", err)
	}
}
