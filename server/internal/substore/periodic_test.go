package substore

import (
	"context"
	"slices"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
)

func TestIdlePeriodicUsesSchedulerClockAndKeepsStoredMonotonicTime(t *testing.T) {
	h := newHarness(t, harnessOptions{builtin: true})
	owner := h.service.Runtime
	owner.getMode = func(context.Context) (string, error) { return "on_demand", nil }
	h.clock.set(1234.5)
	clock := lifecycletest.NewClock(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	reg := lifecycle.NewRegistry()
	spec := owner.idlePeriodic()
	round := spec.Round
	rounds := make(chan struct{}, 3)
	spec.Round = func(ctx context.Context) error {
		err := round(ctx)
		clock.Advance(2 * time.Second)
		rounds <- struct{}{}
		return err
	}
	reg.RegisterPeriodic(spec)
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: reg, Clock: clock})
	runner.Start(context.Background())
	defer runner.Shutdown(context.Background())
	select {
	case <-rounds:
	case <-time.After(5 * time.Second):
		t.Fatal("missing immediate first tick")
	}
	if lastUsed(stateOf(owner)) != 1234.5 {
		t.Fatal("persisted monotonic value changed domain", stateOf(owner))
	}
	deadline := time.Now().Add(5 * time.Second)
	for clock.Pending() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("no idle timer")
		}
		time.Sleep(time.Millisecond)
	}
	clock.Advance(4 * time.Second)
	select {
	case <-rounds:
		t.Fatal("early idle tick")
	default:
	}
	clock.Advance(time.Second)
	select {
	case <-rounds:
	case <-time.After(5 * time.Second):
		t.Fatal("missing second tick")
	}
	if err := runner.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lastUsed(stateOf(owner)) != 1234.5 {
		t.Fatal("scheduler altered stored monotonic baseline")
	}
}

func TestWireRegistersIdleOnlyWithBuiltin(t *testing.T) {
	for _, builtin := range []bool{false, true} {
		t.Run(map[bool]string{false: "external", true: "builtin"}[builtin], func(t *testing.T) {
			h := newHarness(t, harnessOptions{builtin: builtin})
			t.Cleanup(SetDefault(h.service))
			if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
					t.Error(err)
				}
			})
			Wire()
			if slices.Contains(lifecycle.Names(), "sub-store-idle") != builtin {
				t.Fatal("conditional idle registration", lifecycle.Names())
			}
			if !slices.Contains(lifecycle.Names(), "subscription-cache-prune") {
				t.Fatal("prune Startup missing")
			}
		})
	}
}
