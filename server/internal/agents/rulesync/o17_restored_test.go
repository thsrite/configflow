package rulesync

import (
	"context"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/model"
)

// A restored Agent (O17, design 2.5.1) has no baseline: rule sync shows
// blocked with the restore message and pushes nothing.
func TestO17RestoredAgentBlocksRuleSync(t *testing.T) {
	f := newWorkerFixture(t, "mihomo")
	if _, e := f.s.Configure(f.ctx, "device", []byte(`{"library":true,"rules":true}`)); e != nil {
		t.Fatal(e)
	}
	e := f.s.m.Repository().UpdateSystem(f.ctx, func(_ context.Context, system *model.System) error {
		for i := range system.Agents {
			if system.Agents[i].ID == "device" {
				system.Agents[i].StateRestoredAt = new("2026-01-01T00:00:00Z")
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	prepared := 0
	prepare := f.s.prepare
	f.s.prepare = func(ctx context.Context, target deploy.Target, source *deploy.Source, baseline Baseline, settings Settings, id string, force bool) (*deploy.Bundle, error) {
		prepared++
		return prepare(ctx, target, source, baseline, settings, id, force)
	}
	if e = f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if state := f.state(t); state.Status != "blocked" || state.Message != agents.StateRestoredMessage || prepared != 0 {
		t.Fatalf("state %+v, prepared %d", state, prepared)
	}
}
