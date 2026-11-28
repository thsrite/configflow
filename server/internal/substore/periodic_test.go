package substore

import (
	"context"
	"os"
	"path/filepath"
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
			rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			h.service.Register(rt)
			if slices.Contains(rt.Names(), "sub-store-idle") != builtin {
				t.Fatal("conditional idle registration", rt.Names())
			}
			h.service.RegisterStartup(rt)
			if !slices.Contains(rt.Names(), "subscription-cache-prune") {
				t.Fatal("prune Startup missing")
			}
		})
	}
}

func TestSleepingPaceResumesOnUseModeAndExternalState(t *testing.T) {
	h, r, _ := lifecycleService(t)
	ctx := context.Background()
	spec := r.idlePeriodic()
	tick := func() time.Duration {
		t.Helper()
		if err := spec.Round(ctx); err != nil {
			t.Fatal(err)
		}
		return spec.Interval(ctx)
	}
	active := 5 * time.Second
	tick()
	if pause := tick(); pause != time.Minute {
		t.Fatalf("sleeping pause %v", pause)
	}
	if err := r.Use(ctx, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if spec.Interval(ctx) != active {
		t.Fatal("use did not restore active interval")
	}
	tick()
	if running, _, stops := process(h); running || stops != 1 {
		t.Fatalf("idle sweep after use: running=%v stops=%d", running, stops)
	}
	tick()
	if err := r.SetMode(ctx, "always"); err != nil {
		t.Fatal(err)
	}
	if tick() != active || !h.store.running() {
		t.Fatal("always did not stay running")
	}
	if err := r.SetMode(ctx, "on_demand"); err != nil {
		t.Fatal(err)
	}
	tick()
	tick()
	if err := os.WriteFile(filepath.Join(r.Root(), "state.json"), []byte(`{"state":"running","last_used":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	tick()
	if state := stateOf(r); state.State == nil || *state.State != "sleeping" {
		t.Fatalf("external state not reconciled: %+v", state)
	}
}
