package notifications

import (
	"context"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestO17FollowupRouterSignaturesCacheUsesSameConfig(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{DefaultSettings: repo.DefaultConfig})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(t.Context(), r)
	if err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{{
			ID:          "a1",
			Name:        "Agent 1",
			Host:        "192.0.2.1",
			Port:        model.Int(8080),
			Token:       "tok-1",
			ProfileID:   repo.DefaultProfileID,
			ServiceType: "oxidns",
			Version:     "1.0.0",
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	routerSigMu.Lock()
	delete(routerSigCache, r.DataDir())
	routerSigMu.Unlock()

	sigs1, err := routerSignatures(ctx, r, []string{repo.DefaultProfileID})
	if err != nil {
		t.Fatal(err)
	}
	routerSigMu.Lock()
	head1 := routerSigCache[r.DataDir()].head
	routerSigMu.Unlock()

	// State-only commit must keep routerSigCache hit (cached head unchanged).
	if err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents[0].Version = "2.0.0"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sigs2, err := routerSignatures(ctx, r, []string{repo.DefaultProfileID})
	if err != nil {
		t.Fatal(err)
	}
	routerSigMu.Lock()
	head2 := routerSigCache[r.DataDir()].head
	routerSigMu.Unlock()
	if head2 != head1 || len(sigs2) != len(sigs1) {
		t.Fatalf("routerSigCache invalidated by state-only commit: head %+v -> %+v", head1, head2)
	}

	// Configuration commit must invalidate routerSigCache.
	if err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents[0].Name = "Agent 1 Renamed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := routerSignatures(ctx, r, []string{repo.DefaultProfileID}); err != nil {
		t.Fatal(err)
	}
	routerSigMu.Lock()
	head3 := routerSigCache[r.DataDir()].head
	routerSigMu.Unlock()
	if head3.SameConfig(head1) {
		t.Fatalf("routerSigCache not invalidated by config commit: head %+v -> %+v", head1, head3)
	}
}
