package ipv6

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

func TestServiceLoopWakesOnConfigAndState(t *testing.T) {
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
			ID:          "ipv6-wake-1",
			Name:        "gw-1",
			Host:        "192.0.2.13",
			Port:        model.Int(8080),
			Token:       "synthetic-ipv6-token",
			ServiceType: "mihomo",
			ProfileID:   "default",
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(m, nil)
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
			t.Fatalf("%s did not wake ipv6 loop", what)
		}
	}
	noRound := func(what string) {
		t.Helper()
		select {
		case <-rounds:
			t.Fatalf("%s unexpectedly woke ipv6 loop", what)
		case <-time.After(150 * time.Millisecond):
		}
	}

	wantRound("initial start")

	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Name = "gw-1-renamed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantRound("config commit")

	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Version = "1.2.3"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantRound("state commit")

	// IPv6 scheduler does not watch heartbeat presence.
	if _, err := m.ObserveHeartbeat(ctx, "ipv6-wake-1", &model.Agent{}); err != nil {
		t.Fatal(err)
	}
	noRound("agent coming online")

	m.Close()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Version = "1.2.4"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	noRound("commit after manager close")
}
