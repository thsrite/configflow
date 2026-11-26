package lifecycle_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/lockx"
	"configflow/internal/repo"
)

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
		var zero T
		return zero
	}
}
func until(t *testing.T, ready func() bool) {
	t.Helper()
	limit := time.Now().Add(5 * time.Second)
	for !ready() {
		if time.Now().After(limit) {
			t.Fatal("condition not reached")
		}
		runtime.Gosched()
	}
}
func newClock() *lifecycletest.Clock {
	return lifecycletest.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}
func setup(t *testing.T, opts lifecycle.Options) *lifecycle.Runtime {
	t.Helper()
	r := lifecycle.NewRuntime(opts)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r.Shutdown(ctx)
	})
	return r
}
func idle(t *testing.T, r *lifecycle.Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}
func releaseGate(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	fn := func() { once.Do(func() { close(ch) }) }
	t.Cleanup(fn)
	return ch, fn
}

func TestStartupOrderAndFailures(t *testing.T) {
	reg := lifecycle.NewRegistry()
	var order []string
	reg.RegisterStartup(lifecycle.Startup{Name: "one", Run: func(context.Context) error {
		order = append(order, "one")
		return errors.New("synthetic startup error")
	}})
	reg.RegisterStartup(lifecycle.Startup{Name: "panic", Run: func(context.Context) error { order = append(order, "panic"); panic("synthetic payload") }})
	reg.RegisterStartup(lifecycle.Startup{Name: "two", Run: func(context.Context) error { order = append(order, "two"); return nil }})
	r := setup(t, lifecycle.Options{Registry: reg, Mode: lifecycle.ModeManual})
	r.RunStartup(context.Background())
	r.Start(context.Background())
	r.RunStartup(context.Background())
	if len(order) != 3 || order[0] != "one" || order[1] != "panic" || order[2] != "two" {
		t.Fatalf("startup order/once: %v", order)
	}
	r.Shutdown(context.Background())
	r.Start(context.Background())
	if len(order) != 6 {
		t.Fatalf("startup not repeated on restart: %v", order)
	}
}

func TestPeriodicFirstDelayIntervalAndWake(t *testing.T) {
	clock := newClock()
	reg := lifecycle.NewRegistry()
	wake := new(lifecycle.Signal)
	entered := make(chan time.Time, 10)
	gate, finish := releaseGate(t)
	var rounds atomic.Int64
	var interval atomic.Int64
	interval.Store(30)
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "refresh", FirstDelay: 30 * time.Second, Wake: wake,
		Interval: func(context.Context) time.Duration { return time.Duration(interval.Load()) * time.Second },
		Round: func(context.Context) error {
			n := rounds.Add(1)
			entered <- clock.Now()
			if n == 1 {
				<-gate
			}
			return nil
		}})
	r := setup(t, lifecycle.Options{Clock: clock, Registry: reg})
	r.Start(context.Background())
	until(t, func() bool { return clock.Pending() == 1 })
	start := clock.Now()
	clock.Advance(29 * time.Second)
	if rounds.Load() != 0 {
		t.Fatal("early first round")
	}
	clock.Advance(time.Second)
	if at := receive(t, entered); at != start.Add(30*time.Second) {
		t.Fatal(at)
	}
	clock.Advance(7 * time.Second)
	interval.Store(45)
	finish()
	idle(t, r)
	until(t, func() bool { return clock.Pending() == 1 })
	clock.Advance(44 * time.Second)
	if rounds.Load() != 1 {
		t.Fatal("interval measured from round start")
	}
	clock.Advance(time.Second)
	receive(t, entered)
	idle(t, r)
	until(t, func() bool { return clock.Pending() == 1 })
	wake.Notify()
	receive(t, entered)
	idle(t, r)
	r.Shutdown(context.Background())
	if clock.Pending() != 0 {
		t.Fatalf("timer leaked: %d", clock.Pending())
	}
}

func TestPythonTimingTable(t *testing.T) {
	for _, tt := range []struct {
		name            string
		first, interval time.Duration
	}{
		{"scheduled-refresh", 30 * time.Second, 30 * time.Second}, {"router-rule-sync", 30 * time.Second, 30 * time.Second},
		{"domain-discovery-auto", 120 * time.Second, 600 * time.Second}, {"oxidns-ranking-history", 60 * time.Second, 60 * time.Second},
		{"metrics-history-flush", 30 * time.Second, 30 * time.Second}, {"sub-store-idle", 0, 5 * time.Second},
		{"dns-follow", 0, 5 * time.Second}, {"oxidns-ha-publish", 0, 2 * time.Second}, {"ipv6-verification", 0, 5 * time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clock := newClock()
			reg := lifecycle.NewRegistry()
			times := make(chan time.Time, 2)
			reg.RegisterPeriodic(lifecycle.Periodic{Name: tt.name, FirstDelay: tt.first, Interval: func(context.Context) time.Duration { return tt.interval }, Round: func(context.Context) error { times <- clock.Now(); return nil }})
			r := setup(t, lifecycle.Options{Clock: clock, Registry: reg})
			start := clock.Now()
			r.Start(context.Background())
			if tt.first > 0 {
				until(t, func() bool { return clock.Pending() == 1 })
				clock.Advance(tt.first)
			}
			if at := receive(t, times); at != start.Add(tt.first) {
				t.Fatal(at)
			}
			idle(t, r)
			until(t, func() bool { return clock.Pending() == 1 })
			clock.Advance(tt.interval)
			if at := receive(t, times); at != start.Add(tt.first+tt.interval) {
				t.Fatal(at)
			}
		})
	}
}

func TestAfterFirstRequestAndRepeatedStartStop(t *testing.T) {
	clock := newClock()
	reg := lifecycle.NewRegistry()
	ran := make(chan struct{}, 5)
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "agent-rule-sync-loop", Start: lifecycle.AfterFirstRequest, Interval: func(context.Context) time.Duration { return 5 * time.Second }, Round: func(context.Context) error { ran <- struct{}{}; return nil }})
	r := setup(t, lifecycle.Options{Clock: clock, Registry: reg})
	for range 3 {
		r.Start(context.Background())
		r.Start(context.Background())
		if clock.Pending() != 0 {
			t.Fatal("loop started before request")
		}
		r.RequestSeen()
		r.RequestSeen()
		receive(t, ran)
		idle(t, r)
		r.Shutdown(context.Background())
		if clock.Pending() != 0 {
			t.Fatal("restart leaked timers")
		}
	}
}

func TestManualRoundNoOverlapAndPanicRecovery(t *testing.T) {
	clock := newClock()
	reg := lifecycle.NewRegistry()
	gate, finish := releaseGate(t)
	entered := make(chan struct{})
	var count atomic.Int64
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "manual", Interval: func(context.Context) time.Duration { return time.Second }, Round: func(context.Context) error {
		if count.Add(1) == 1 {
			close(entered)
			<-gate
			panic("private")
		}
		return nil
	}})
	r := setup(t, lifecycle.Options{Clock: clock, Registry: reg, Mode: lifecycle.ModeManual})
	r.Start(context.Background())
	clock.Advance(time.Hour)
	if count.Load() != 0 || clock.Pending() != 0 {
		t.Fatal("manual runtime ran automatically")
	}
	done := make(chan error, 1)
	go func() { done <- r.RunRound(context.Background(), "manual") }()
	receive(t, entered)
	if err := r.RunRound(context.Background(), "manual"); !errors.Is(err, lockx.ErrBusy) {
		t.Fatalf("overlap: %v", err)
	}
	finish()
	if err := receive(t, done); !errors.Is(err, lifecycle.ErrPanic) {
		t.Fatal(err)
	}
	if err := r.RunRound(context.Background(), "manual"); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 {
		t.Fatal(count.Load())
	}
}

func TestPeriodicContinuesAfterPanic(t *testing.T) {
	clock := newClock()
	reg := lifecycle.NewRegistry()
	ran := make(chan int, 3)
	var count atomic.Int64
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "recover", Interval: func(context.Context) time.Duration { return time.Second }, Round: func(context.Context) error {
		n := int(count.Add(1))
		ran <- n
		if n == 1 {
			panic("synthetic")
		}
		return nil
	}})
	r := setup(t, lifecycle.Options{Clock: clock, Registry: reg})
	r.Start(context.Background())
	if receive(t, ran) != 1 {
		t.Fatal("first")
	}
	idle(t, r)
	until(t, func() bool { return clock.Pending() == 1 })
	clock.Advance(time.Second)
	if receive(t, ran) != 2 {
		t.Fatal("panic stopped loop")
	}
}

type valueKey struct{}

func TestSpawnFreshScopeCancellationAndFlight(t *testing.T) {
	r := setup(t, lifecycle.Options{Mode: lifecycle.ModeManual})
	r.Start(context.Background())
	ctx, cancel := context.WithCancel(repo.WithScope(context.WithValue(context.Background(), valueKey{}, "kept")))
	defer cancel()
	parent := repo.ScopeOf(ctx)
	gate, finish := releaseGate(t)
	started := make(chan struct{})
	failure := make(chan error, 1)
	f := lockx.NewFlight[string, int](r, "render")
	go func() {
		_, err, _ := f.Do(ctx, "key", func(work context.Context) (int, error) {
			close(started)
			<-gate
			if work.Err() != nil || work.Value(valueKey{}) != "kept" || repo.ScopeOf(work) == nil || repo.ScopeOf(work) == parent {
				return 0, errors.New("scope or context lost")
			}
			return 7, nil
		})
		failure <- err
	}()
	receive(t, started)
	cancel()
	if err := receive(t, failure); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	if err := r.WaitIdle(wait); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("untracked flight: %v", err)
	}
	finish()
	idle(t, r)
	// A cancelled request still starts detached work through Spawn.
	done := make(chan error, 1)
	if err := r.Spawn(ctx, "cancelled", func(work context.Context) { done <- work.Err() }); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownDrainDeadlineAndPhases(t *testing.T) {
	clock := newClock()
	reg := lifecycle.NewRegistry()
	events := make(chan string, 20)
	for _, h := range []struct {
		name  string
		phase lifecycle.Phase
	}{{"stop", lifecycle.PhaseStopSignal}, {"after", lifecycle.PhaseAfterDrain}, {"close", lifecycle.PhaseClose}} {
		reg.RegisterShutdown(lifecycle.ShutdownHook{Name: h.name, Phase: h.phase, Run: func(ctx context.Context) {
			select {
			case <-lifecycle.Stopping(ctx):
			default:
				t.Error("hook lost generation shutdown signal")
			}
			events <- h.name
		}})
	}
	draining := make(chan struct{})
	r := setup(t, lifecycle.Options{Clock: clock, Registry: reg, Grace: 8 * time.Second, Drain: func(ctx context.Context) error { close(draining); <-ctx.Done(); return ctx.Err() }})
	r.Start(context.Background())
	started := make(chan struct{})
	cancelled := make(chan time.Time, 1)
	if err := r.Spawn(context.Background(), "slow", func(ctx context.Context) { close(started); <-r.Stopping(ctx); <-ctx.Done(); cancelled <- clock.Now() }); err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	stopped := make(chan struct{})
	go func() { r.Shutdown(context.Background()); close(stopped) }()
	if receive(t, events) != "stop" {
		t.Fatal("stop hook order")
	}
	receive(t, draining)
	until(t, func() bool { return clock.Pending() == 2 })
	if err := r.Spawn(context.Background(), "finishing-handler", func(context.Context) {}); err != nil {
		t.Fatalf("drain rejected admitted handler: %v", err)
	}
	start := clock.Now()
	clock.Advance(5 * time.Second)
	select {
	case <-cancelled:
		t.Fatal("early job cancellation")
	default:
	}
	clock.Advance(time.Second)
	if at := receive(t, cancelled); at != start.Add(6*time.Second) {
		t.Fatal(at)
	}
	receive(t, stopped)
	if receive(t, events) != "after" || receive(t, events) != "close" {
		t.Fatal("phase order")
	}
	if err := r.Spawn(context.Background(), "too-late", func(context.Context) {}); !errors.Is(err, lifecycle.ErrStopping) {
		t.Fatal(err)
	}
	if clock.Pending() != 0 {
		t.Fatalf("shutdown timers leaked: %d", clock.Pending())
	}
}

func TestIdleShutdownWaitsForHTTPDrainThenEndsEarly(t *testing.T) {
	clock := newClock()
	gate, finish := releaseGate(t)
	draining := make(chan struct{})
	r := setup(t, lifecycle.Options{Clock: clock, Drain: func(context.Context) error { close(draining); <-gate; return nil }})
	r.Start(context.Background())
	done := make(chan struct{})
	go func() { r.Shutdown(context.Background()); close(done) }()
	receive(t, draining)
	if err := r.Spawn(context.Background(), "late-handler", func(context.Context) {}); err != nil {
		t.Fatal(err)
	}
	finish()
	receive(t, done)
	if clock.Now() != time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatal("idle shutdown waited grace")
	}
}

func TestUncooperativeHookDoesNotBlockShutdown(t *testing.T) {
	clock := newClock()
	reg := lifecycle.NewRegistry()
	gate, finish := releaseGate(t)
	started := make(chan struct{})
	ended := make(chan struct{})
	reg.RegisterShutdown(lifecycle.ShutdownHook{Name: "slow-hook", Phase: lifecycle.PhaseStopSignal, Run: func(context.Context) { close(started); <-gate; close(ended) }})
	r := setup(t, lifecycle.Options{Clock: clock, Registry: reg})
	r.Start(context.Background())
	done := make(chan struct{})
	go func() { r.Shutdown(context.Background()); close(done) }()
	receive(t, started)
	clock.Advance(8 * time.Second)
	receive(t, done)
	finish()
	receive(t, ended)
}

func TestRuntimeNoGoroutineLeak(t *testing.T) {
	baseline := runtime.NumGoroutine()
	r := setup(t, lifecycle.Options{Mode: lifecycle.ModeManual})
	r.Start(context.Background())
	for range 100 {
		if err := r.Spawn(context.Background(), "short-job", func(context.Context) {}); err != nil {
			t.Fatal(err)
		}
	}
	idle(t, r)
	r.Shutdown(context.Background())
	until(t, func() bool { return runtime.NumGoroutine() <= baseline+2 })
}
