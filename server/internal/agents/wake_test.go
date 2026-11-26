package agents

import (
	"context"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestWakerPacesBusyAndIdleRounds(t *testing.T) {
	w := NewWaker(5*time.Second, IdleFallback)
	tests := []struct {
		busy bool
		want time.Duration
	}{{false, IdleFallback}, {true, 5 * time.Second}, {false, IdleFallback}}
	for _, tt := range tests {
		w.Done(tt.busy)
		if got := w.Interval(context.Background()); got != tt.want || w.Busy() != tt.busy {
			t.Fatalf("busy %v: interval %v", tt.busy, got)
		}
	}
}

// A change-driven loop runs at once after a commit of the kinds it watches
// or an Agent coming online, and rests otherwise (no clock advance needed).
func TestWakerRunsTheLoopOnCommitsAndPresence(t *testing.T) {
	e := newEnv(t)
	w := NewWaker(5*time.Second, IdleFallback)
	w.Watch(e.m, repo.ChangeState, true)
	rounds := make(chan struct{}, 16)
	reg := lifecycle.NewRegistry()
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "o17-waker-test", Interval: w.Interval, Wake: w.Signal(), Round: func(context.Context) error {
		rounds <- struct{}{}
		return nil
	}})
	clock := lifecycletest.NewClock(time.Unix(1_800_000_000, 0))
	r := lifecycle.NewRuntime(lifecycle.Options{Clock: clock, Registry: reg})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r.Shutdown(ctx)
	})
	r.Start(context.Background())
	wantRound := func(what string) {
		t.Helper()
		select {
		case <-rounds:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not run the loop", what)
		}
	}
	noRound := func(what string) {
		t.Helper()
		select {
		case <-rounds:
			t.Fatalf("%s ran the loop", what)
		case <-time.After(200 * time.Millisecond):
		}
	}
	wantRound("start")
	// A configuration-only commit is not watched here.
	e.editSystem(func(system *model.System) { registryAgent(system.Agents, e.id()).Name = "renamed" })
	noRound("a configuration commit")
	// An Agent state commit is.
	e.editSystem(func(system *model.System) { registryAgent(system.Agents, e.id()).Version = "9.9.9" })
	wantRound("an Agent state commit")
	// An Agent coming online is, a heartbeat of an online Agent is not.
	e.beat(e.id(), map[string]any{})
	wantRound("an Agent coming online")
	e.beat(e.id(), map[string]any{})
	noRound("a heartbeat of an online Agent")
	// Idle: nothing until the fallback.
	clock.Advance(IdleFallback - time.Second)
	noRound("time before the idle fallback")
	clock.Advance(time.Second)
	wantRound("the idle fallback")
	// Closing the Manager ends the watch.
	e.m.Close()
	e.editSystem(func(system *model.System) { registryAgent(system.Agents, e.id()).Version = "9.9.10" })
	noRound("a commit after the Manager closed")
}
