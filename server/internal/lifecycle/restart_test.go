package lifecycle_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
)

// Keep the legacy restart regression isolated from the default registry, so
// repeated test runs cannot retain callbacks from an earlier testing.T.
func TestStartStopStartRunsTheLoopAgain(t *testing.T) {
	reg := lifecycle.NewRegistry()
	startedLoop := make(chan struct{}, 2)
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "restart-regression", Round: func(ctx context.Context) error {
		startedLoop <- struct{}{}
		<-ctx.Done()
		return nil
	}})
	r := setup(t, lifecycle.Options{Registry: reg, Grace: 10 * time.Millisecond})
	for generation := range 2 {
		r.Start(context.Background())
		select {
		case <-startedLoop:
		case <-time.After(time.Second):
			t.Fatalf("loop did not run for generation %d", generation+1)
		}
		r.Shutdown(context.Background())
	}
}

func TestRestartCannotOverlapAnUnfinishedOldRound(t *testing.T) {
	clock := newClock()
	reg := lifecycle.NewRegistry()
	gate, finish := releaseGate(t)
	entered := make(chan struct{}, 2)
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "slow", Round: func(context.Context) error { entered <- struct{}{}; <-gate; return nil }})
	r := setup(t, lifecycle.Options{Clock: clock, Registry: reg, Mode: lifecycle.ModeManual})
	r.Start(context.Background())
	old := make(chan error, 1)
	go func() { old <- r.RunRound(context.Background(), "slow") }()
	receive(t, entered)
	stopped := make(chan struct{})
	go func() { r.Shutdown(context.Background()); close(stopped) }()
	until(t, func() bool { return clock.Pending() == 2 })
	clock.Advance(6 * time.Second)
	// The cancellation window replaces the drain timer while the overall
	// grace timer remains. Old work intentionally ignores its context.
	until(t, func() bool { return clock.Pending() == 2 })
	clock.Advance(500 * time.Millisecond)
	receive(t, stopped)
	r.Start(context.Background())
	next := make(chan error, 1)
	go func() { next <- r.RunRound(context.Background(), "slow") }()
	select {
	case err := <-next:
		if !errors.Is(err, lockx.ErrBusy) {
			t.Fatalf("old round lost its gate: %v", err)
		}
	case <-entered:
		finish()
		receive(t, next)
		t.Fatal("restarted generation overlapped the old round")
	case <-time.After(5 * time.Second):
		t.Fatal("restart gate blocked instead of reporting busy")
	}
	finish()
	if err := receive(t, old); err != nil {
		t.Fatal(err)
	}
	if err := r.RunRound(context.Background(), "slow"); err != nil {
		t.Fatalf("old completion did not release gate: %v", err)
	}
}
