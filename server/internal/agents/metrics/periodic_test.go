package metrics

import (
	"context"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/taskstatus"
)

func awaitFlush(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("flush condition timed out")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFlushPeriodicActivationWakeAndRestart(t *testing.T) {
	_, r, ctx := agentManager(t)
	clock := lifecycletest.NewClock(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	oldNow := utcNow
	utcNow = clock.Now
	defer func() { utcNow = oldNow }()
	reg := lifecycle.NewRegistry()
	spec := flushPeriodic()
	original := spec.Round
	rounds := make(chan struct{}, 8)
	spec.Round = func(ctx context.Context) error {
		err := original(ctx)
		clock.Advance(7 * time.Second)
		rounds <- struct{}{}
		return err
	}
	reg.RegisterPeriodic(spec)
	reg.RegisterShutdown(lifecycle.ShutdownHook{Name: "first", Phase: lifecycle.PhaseStopSignal, Run: func(ctx context.Context) { shutdownFlush(ctx, true) }})
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: reg, Clock: clock})
	defer runner.Shutdown(ctx)
	receive := func() {
		t.Helper()
		select {
		case <-rounds:
		case <-time.After(5 * time.Second):
			t.Fatal("missing flush round")
		}
		if err := runner.WaitIdle(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	runner.Start(ctx)
	if got := NextRunAt(jsonx.NewObject(), 1800); got == nil || !got.Equal(clock.Now().Add(1800*time.Second)) {
		t.Fatal("no activation baseline", got)
	}
	awaitFlush(t, func() bool { return clock.Pending() == 1 })
	clock.Advance(29 * time.Second)
	select {
	case <-rounds:
		t.Fatal("early first tick")
	default:
	}
	clock.Advance(time.Second)
	receive()
	if entry, err := taskstatus.New(r.DataDir()).Get(TaskKey); err != nil || entry.GetInt("run_count") != 0 {
		t.Fatal("saved before due", entry, err)
	}
	awaitFlush(t, func() bool { return clock.Pending() == 1 })
	clock.Advance(29 * time.Second)
	select {
	case <-rounds:
		t.Fatal("interval counted from round start")
	default:
	}
	clock.Advance(time.Second)
	receive()
	awaitFlush(t, func() bool { return clock.Pending() == 1 })
	clock.Advance(570 * time.Second)
	receive()
	awaitFlush(t, func() bool { return clock.Pending() == 1 })
	if err := SetInterval(ctx, jsonx.Int(300)); err != nil {
		t.Fatal(err)
	}
	receive() // Signal wakes without advancing the clock; the new interval is reread.
	entry, err := taskstatus.New(r.DataDir()).Get(TaskKey)
	if err != nil || entry.Value("last_trigger") != "schedule" || entry.GetInt("run_count") != 1 {
		t.Fatal("wake did not save", entry, err)
	}
	runner.Shutdown(ctx) // leaves a wake notification from the first save
	entry, err = taskstatus.New(r.DataDir()).Get(TaskKey)
	if err != nil || entry.Value("last_trigger") != "shutdown" || entry.GetInt("run_count") != 2 {
		t.Fatal("shutdown used the wrong repository", entry, err)
	}
	if !isStopping() {
		t.Fatal("shutdown did not stop owner")
	}
	clock.Advance(time.Hour)
	runner.Start(ctx)
	if isStopping() {
		t.Fatal("restart did not reset stopping")
	}
	if got := NextRunAt(jsonx.NewObject(), 300); got == nil || !got.Equal(clock.Now().Add(300*time.Second)) {
		t.Fatal("restart retained old baseline", got)
	}
	awaitFlush(t, func() bool { return clock.Pending() == 1 })
	select {
	case <-rounds:
		t.Fatal("old wake interrupted new first delay")
	default:
	}
	clock.Advance(29 * time.Second)
	select {
	case <-rounds:
		t.Fatal("restart tick early")
	default:
	}
	clock.Advance(time.Second)
	receive()
}

func TestManualFlushDoesNotInitializeSchedule(t *testing.T) {
	_, _, ctx := agentManager(t)
	clock := lifecycletest.NewClock(time.Now())
	reg := lifecycle.NewRegistry()
	reg.RegisterPeriodic(flushPeriodic())
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: reg, Clock: clock, Mode: lifecycle.ModeManual})
	runner.Start(ctx)
	defer runner.Shutdown(ctx)
	if err := runner.RunRound(ctx, "metrics-history-flush"); err != nil {
		t.Fatal(err)
	}
	if clock.Pending() != 0 || NextRunAt(jsonx.NewObject(), 300) != nil {
		t.Fatal("manual mode invented activation")
	}
	rows, err := Rows(ctx, jsonx.NewObject())
	if err != nil || rows[0].Value("next_run_at") != nil {
		t.Fatal("manual row invented next_run_at", rows, err)
	}
}
