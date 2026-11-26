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
	"configflow/internal/lockx"
)

// O3 review: owner code that detaches inside a shutdown hook (the final
// metrics save, close-phase owners following design 2.5) must not receive the
// job context that the drain deadline already cancelled.
func TestDetachInsideShutdownHooksIsBoundedByGrace(t *testing.T) {
	clock, reg := newClock(), lifecycle.NewRegistry()
	type observation struct {
		phase string
		err   error
		work  context.Context
	}
	seen := make(chan observation, 4)
	for _, h := range []struct {
		name  string
		phase lifecycle.Phase
	}{{"stop", lifecycle.PhaseStopSignal}, {"after", lifecycle.PhaseAfterDrain}, {"close", lifecycle.PhaseClose}} {
		reg.RegisterShutdown(lifecycle.ShutdownHook{Name: h.name, Phase: h.phase, Run: func(ctx context.Context) {
			work := lifecycle.Detach(ctx)
			seen <- observation{h.name, work.Err(), work}
		}})
	}
	drained := make(chan observation, 1)
	r := setup(t, lifecycle.Options{Clock: clock, Registry: reg, Drain: func(ctx context.Context) error {
		work := lifecycle.Detach(ctx)
		drained <- observation{"drain", work.Err(), work}
		return nil
	}})
	r.Start(context.Background())
	// A job that keeps running until the drain deadline cancels it, so the
	// after-drain hooks run after the job cancellation phase.
	if err := r.Spawn(context.Background(), "slow", func(ctx context.Context) { <-ctx.Done() }); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() { defer close(stopped); r.Shutdown(context.Background()) }()
	until(t, func() bool { return clock.Pending() == 2 }) // overall grace + drain deadline
	seen <- receive(t, drained)
	clock.Advance(6 * time.Second)
	receive(t, stopped)
	close(seen)
	var held []context.Context
	for o := range seen {
		if o.err != nil {
			t.Fatalf("%s hook detached into a cancelled context: %v", o.phase, o.err)
		}
		held = append(held, o.work)
	}
	if len(held) != 4 {
		t.Fatalf("observed %d hooks", len(held))
	}
	for _, work := range held {
		if work.Err() == nil {
			t.Fatal("hook detachment outlived the overall shutdown grace")
		}
	}
}

// O3 review: requests are not jobs, but owner entry points detach from them
// (PORTING 4b). Binding keeps the drop of request cancellation and adds the
// generation's shutdown cancellation, which a plain WithoutCancel lost.
func TestBindRequestKeepsShutdownCancellation(t *testing.T) {
	type key struct{}
	clock := newClock()
	r := setup(t, lifecycle.Options{Clock: clock})
	unbound := context.WithValue(context.Background(), key{}, "kept")
	if r.BindRequest(unbound) != unbound {
		t.Fatal("bound without a live generation")
	}
	r.Start(context.Background())
	request, cancel := context.WithCancel(unbound)
	bound := r.BindRequest(request)
	if r.BindRequest(bound) != bound {
		t.Fatal("rebound an already bound context")
	}
	work := lifecycle.Detach(bound)
	if work.Value(key{}) != "kept" || lifecycle.ClockFrom(bound) != clock {
		t.Fatal("binding lost values or the runtime clock")
	}
	select {
	case <-lifecycle.Stopping(bound):
		t.Fatal("stopping before shutdown")
	default:
	}
	cancel()
	if work.Err() != nil {
		t.Fatal("client disconnect reached detached work")
	}
	jobBound := make(chan context.Context, 1)
	if err := r.Spawn(context.Background(), "job", func(ctx context.Context) { jobBound <- r.BindRequest(ctx) }); err != nil {
		t.Fatal(err)
	}
	if ctx := receive(t, jobBound); lifecycle.Detach(ctx).Err() != nil {
		t.Fatal("job context changed by binding")
	}
	idle(t, r)
	// A job still running keeps shutdown in its drain until the deadline.
	if err := r.Spawn(context.Background(), "slow", func(ctx context.Context) { <-ctx.Done() }); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() { defer close(stopped); r.Shutdown(context.Background()) }()
	until(t, func() bool { return clock.Pending() == 2 })
	select {
	case <-lifecycle.Stopping(bound):
	default:
		t.Fatal("bound request lost the stopping signal")
	}
	if work.Err() != nil {
		t.Fatal("cancelled before the drain deadline")
	}
	clock.Advance(6 * time.Second)
	receive(t, stopped)
	if !errors.Is(work.Err(), context.Canceled) {
		t.Fatal("shutdown cancellation did not reach request-detached work")
	}
	if after := context.Background(); r.BindRequest(after) != after {
		t.Fatal("bound to a closed generation")
	}
}

// O3 review: one request after another must not re-run eligibility work, but
// the first request of every generation still starts its loops.
func TestRequestSeenStartsOncePerGeneration(t *testing.T) {
	clock, reg := newClock(), lifecycle.NewRegistry()
	var activations atomic.Int64
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "agent-rule-sync", Start: lifecycle.AfterFirstRequest,
		OnStart:  func(context.Context) { activations.Add(1) },
		Interval: func(context.Context) time.Duration { return 5 * time.Second },
		Round:    func(context.Context) error { return nil }})
	r := setup(t, lifecycle.Options{Clock: clock, Registry: reg})
	// A request admitted between listen and Start counts for that generation.
	r.RequestSeen()
	r.Start(context.Background())
	if activations.Load() != 1 {
		t.Fatal("early request did not start the loop at Start")
	}
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() { defer wg.Done(); r.RequestSeen() }()
	}
	wg.Wait()
	if activations.Load() != 1 {
		t.Fatal("repeated activation")
	}
	r.Shutdown(context.Background())
	r.Start(context.Background())
	if activations.Load() != 1 {
		t.Fatal("restarted before a request")
	}
	r.RequestSeen()
	if activations.Load() != 2 {
		t.Fatal("first request of the next generation ignored")
	}
}

// O3 review, cross-step: loops, rounds, jobs, shared flights, bound requests
// and detached children all end by shutdown; nothing outlives the runtime.
func TestShutdownLeavesNoGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()
	clock, reg := newClock(), lifecycle.NewRegistry()
	rounds := make(chan struct{}, 100)
	var wake lifecycle.Signal
	for _, name := range []string{"loop-a", "loop-b"} {
		reg.RegisterPeriodic(lifecycle.Periodic{Name: name, FirstDelay: time.Second, Wake: &wake,
			Interval: func(context.Context) time.Duration { return time.Second },
			Round:    func(context.Context) error { rounds <- struct{}{}; return nil }})
	}
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "request-loop", Start: lifecycle.AfterFirstRequest,
		Interval: func(context.Context) time.Duration { return time.Second },
		Round:    func(context.Context) error { return nil }})
	r := lifecycle.NewRuntime(lifecycle.Options{Clock: clock, Registry: reg})
	for generation := 0; generation < 2; generation++ {
		r.Start(context.Background())
		r.RequestSeen()
		flight := lockx.NewFlight[int, int](r, "flight")
		var wg sync.WaitGroup
		for i := range 200 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				request, cancel := context.WithCancel(r.BindRequest(context.Background()))
				defer cancel()
				child, stop := context.WithCancel(lifecycle.Detach(request))
				defer stop()
				if i%2 == 0 {
					_, _, _ = flight.Do(child, i%7, func(ctx context.Context) (int, error) { return i, nil })
					return
				}
				_ = r.Spawn(child, "job", func(ctx context.Context) { <-lifecycle.Detach(ctx).Done() })
			}()
		}
		wg.Wait()
		wake.Notify()
		receive(t, rounds)
		// Remaining spawned jobs wait for the shutdown cancellation phase.
		stopped := make(chan struct{})
		go func() { defer close(stopped); r.Shutdown(context.Background()) }()
		// Drive the fake clock until shutdown ends; jobs end at the drain deadline.
		limit := time.Now().Add(5 * time.Second)
	drive:
		for {
			select {
			case <-stopped:
				break drive
			case <-time.After(5 * time.Millisecond):
				if time.Now().After(limit) {
					t.Fatal("shutdown did not finish")
				}
				clock.Advance(500 * time.Millisecond)
			}
		}
	}
	limit := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > baseline+2 {
		if time.Now().After(limit) {
			buf := make([]byte, 1<<20)
			t.Fatalf("goroutines %d > baseline %d:\n%s", runtime.NumGoroutine(), baseline, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(time.Millisecond)
	}
	if clock.Pending() != 0 {
		t.Fatalf("timers leaked: %d", clock.Pending())
	}
}
