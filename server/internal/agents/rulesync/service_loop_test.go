package rulesync

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestServiceLoopWakesOnConfigStatePresenceAndRefresh(t *testing.T) {
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
			ID:          "rulesync-wake-1",
			Name:        "rsync-1",
			Host:        "192.0.2.12",
			Port:        model.Int(8080),
			Token:       "synthetic-rsync-token",
			ServiceType: "oxidns",
			ProfileID:   "default",
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	svc := New(m, nil, Options{})
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
	rt.RequestSeen()

	wantRound := func(what string) {
		t.Helper()
		select {
		case <-rounds:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not wake rulesync loop", what)
		}
	}
	noRound := func(what string) {
		t.Helper()
		select {
		case <-rounds:
			t.Fatalf("%s unexpectedly woke rulesync loop", what)
		case <-time.After(150 * time.Millisecond):
		}
	}

	wantRound("first request start")

	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Name = "rsync-1-renamed"
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

	if _, err := m.ObserveHeartbeat(ctx, "rulesync-wake-1", &model.Agent{}); err != nil {
		t.Fatal(err)
	}
	wantRound("agent coming online")

	// A rule reader with its own refresh service still publishes through this repository.
	write := func() {
		t.Helper()
		if _, err := r.WriteSharedTextIfChanged(ctx, "rules/by-source/poll.list", []byte("example.com\n")); err != nil {
			t.Fatal(err)
		}
	}
	write()
	wantRound("rule file publication")
	write() // first replacement also installs the backup
	wantRound("rule cache backup installation")
	write() // identical bytes: refreshes mtime only
	noRound("unchanged file touch")

	// Rule-source refresh wakes the loop explicitly via WakeLoop.
	svc.WakeLoop()
	wantRound("WakeLoop after rule refresh")

	m.Close()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Version = "1.2.4"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	noRound("commit after manager close")
}

func TestPairHooksFailSafeAndWired(t *testing.T) {
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

	// Unwired PairHooks is safe (no-op advance, no failure translation).
	unwired := New(m, nil, Options{})
	if err := unwired.advancePair(ctx, "agent-1"); err != nil {
		t.Fatalf("unwired advancePair: %v", err)
	}
	if msg, ok := unwired.pairFailure(ctx, errors.New("boom")); ok || msg != "" {
		t.Fatalf("unwired pairFailure = (%q, %v), want (\"\", false)", msg, ok)
	}

	// Wired PairHooks invokes the injected callbacks directly.
	advanced := ""
	wired := New(m, nil, Options{
		Pair: PairHooks{
			Advance: func(_ context.Context, id string) { advanced = id },
			FailureMessage: func(_ context.Context, cause error) (string, bool) {
				return "translated: " + cause.Error(), true
			},
		},
	})
	if err := wired.advancePair(ctx, "agent-2"); err != nil {
		t.Fatalf("wired advancePair: %v", err)
	}
	if advanced != "agent-2" {
		t.Fatalf("advanced = %q, want agent-2", advanced)
	}
	if msg, ok := wired.pairFailure(ctx, errors.New("busy")); !ok || msg != "translated: busy" {
		t.Fatalf("wired pairFailure = (%q, %v), want (\"translated: busy\", true)", msg, ok)
	}
}

func TestRoundIdleAndRetryPacing(t *testing.T) {
	ctx, s := fixtureService(t)
	clk := lifecycletest.NewClock(s.now())
	s.now = clk.Now
	rt := lifecycle.NewRuntime(lifecycle.Options{Clock: clk, Registry: lifecycle.NewRegistry()})
	round := func() time.Duration {
		t.Helper()
		rt.Start(ctx)
		if err := s.Round(rt.BindRequest(ctx)); err != nil {
			t.Fatal(err)
		}
		rt.Stop(5 * time.Second)
		return s.Loop().Interval(ctx)
	}
	set := func(on bool, state model.RuleSyncState) {
		t.Helper()
		if err := persistSync(ctx, s.m, "device", &Settings{Library: on}, &state); err != nil {
			t.Fatal(err)
		}
	}
	for _, on := range []bool{true, false} {
		set(on, model.RuleSyncState{Status: "pending", Message: msgOffline})
		start := clk.Now()
		rounds := 0
		for clk.Now().Sub(start) < 10*time.Minute {
			delay := round()
			if delay != IdleFallback {
				t.Fatalf("library=%v: idle interval %v", on, delay)
			}
			clk.Advance(delay)
			rounds++
		}
		if rounds != 10 {
			t.Fatalf("library=%v: %d rounds in ten minutes", on, rounds)
		}
	}
	const retry = 17 * time.Second
	set(true, model.RuleSyncState{Status: "failed", RetryAt: new(nowSeconds(clk.Now().Add(retry)))})
	if delay := round(); delay != retry {
		t.Fatalf("retry interval %v, want %v", delay, retry)
	}
	clk.Advance(retry)
	round()
	record, err := s.m.ReadAgent(ctx, "device", false)
	if err != nil {
		t.Fatal(err)
	}
	if state := stateOf(record); state.Message != msgOffline || state.RetryAt != nil {
		t.Fatalf("retry did not run at its deadline: %+v", state)
	}
}
