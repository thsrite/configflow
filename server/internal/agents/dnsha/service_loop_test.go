package dnsha

import (
	"context"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestServiceLoopWakesOnConfigStateAndPresence(t *testing.T) {
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)

	ctx := context.Background()
	m, err := agents.New(ctx, r, agents.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)

	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents = append(s.Agents, model.Agent{
			ID:          "dns-ha-wake-1",
			Name:        "dns-1",
			Host:        "192.0.2.10",
			Port:        model.Int(8080),
			Token:       "synthetic-ha-token",
			ServiceType: "oxidns",
			ProfileID:   "default",
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	svc := New(m, Options{})
	loopSpec := svc.Loop()
	rounds := make(chan struct{}, 16)
	loopSpec.Round = func(context.Context) error {
		rounds <- struct{}{}
		return nil
	}

	reg := lifecycle.NewRegistry()
	reg.RegisterPeriodic(loopSpec)
	clk := lifecycletest.NewClock(time.Unix(1_800_000_000, 0))
	rt := lifecycle.NewRuntime(lifecycle.Options{Clock: clk, Registry: reg})
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rt.Shutdown(stopCtx)
	})
	rt.Start(ctx)

	wantRound := func(what string) {
		t.Helper()
		select {
		case <-rounds:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not wake dnsha loop", what)
		}
	}
	noRound := func(what string) {
		t.Helper()
		select {
		case <-rounds:
			t.Fatalf("%s unexpectedly woke dnsha loop", what)
		case <-time.After(150 * time.Millisecond):
		}
	}

	wantRound("initial start")

	// Config commit wakes the loop.
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Name = "dns-1-renamed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantRound("config commit")

	// Agent state commit wakes the loop.
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Version = "1.2.3"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantRound("state commit")

	// Agent coming online wakes the loop.
	if _, err := m.ObserveHeartbeat(ctx, "dns-ha-wake-1", &model.Agent{}); err != nil {
		t.Fatal(err)
	}
	wantRound("agent coming online")

	// Closing the manager cancels the watch.
	m.Close()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Version = "1.2.4"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	noRound("commit after manager close")
}
