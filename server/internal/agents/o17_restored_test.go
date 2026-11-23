package agents

import (
	"errors"
	"testing"

	"configflow/internal/model"
)

// An Agent imported from a backup without its run records (O17, design
// 2.5.1) refuses guarded edits until a successful publication clears the
// mark; deleting it stays possible.
func TestO17RestoredAgentIsLockedUntilPublished(t *testing.T) {
	e := newEnv(t)
	e.editSystem(func(system *model.System) {
		registryAgent(system.Agents, e.id()).StateRestoredAt = new("2026-01-01T00:00:00Z")
	})
	edits, err := DecodeAgentEdits([]byte(`{"host":"192.0.2.99"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.m.EditAgent(e.ctx, e.id(), edits)
	var conflict *DeploymentConflict
	if !errors.As(err, &conflict) || conflict.Message != StateRestoredMessage {
		t.Fatalf("guarded edit of a restored Agent: %v", err)
	}
	rename, err := DecodeAgentEdits([]byte(`{"name":"renamed"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.EditAgent(e.ctx, e.id(), rename); err != nil {
		t.Fatalf("an unguarded edit was refused: %v", err)
	}
	e.begin("after-restore")
	e.record("after-restore", map[string]any{"success": true, "status": "succeeded", "config_version": "confirmed"})
	e.finish("after-restore")
	if a := e.stored(e.id()); a.StateRestoredAt != nil || a.ConfigVersion != "confirmed" {
		t.Fatalf("a successful publication kept the mark: %+v", a.StateRestoredAt)
	}
	if _, err := e.m.EditAgent(e.ctx, e.id(), edits); err != nil {
		t.Fatalf("edit after publishing: %v", err)
	}
}

func TestO17RestoredAgentCanBeDeleted(t *testing.T) {
	e := newEnv(t)
	e.editSystem(func(system *model.System) {
		registryAgent(system.Agents, e.id()).StateRestoredAt = new("2026-01-01T00:00:00Z")
	})
	if !must[bool](t)(e.m.DeleteAgent(e.ctx, e.id())) || e.stored(e.id()) != nil {
		t.Fatal("a restored Agent could not be deleted")
	}
}
