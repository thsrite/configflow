package lifecycle_test

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

func TestDetachWithoutRuntime(t *testing.T) {
	type key struct{}
	request, cancel := context.WithTimeout(context.WithValue(context.Background(), key{}, "kept"), time.Second)
	cancel()
	work := lifecycle.Detach(request)
	if work.Value(key{}) != "kept" || work.Done() != nil || work.Err() != nil || context.Cause(work) != nil {
		t.Fatal("request cancellation leaked into detached context")
	}
	if _, ok := work.Deadline(); ok {
		t.Fatal("request deadline leaked")
	}
	before := time.Now()
	if now := lifecycle.ClockFrom(work).Now(); now.Before(before) || now.After(time.Now()) {
		t.Fatal("fallback is not real time")
	}
}

func TestDetachKeepsRuntimeCancellationAndValues(t *testing.T) {
	type key struct{}
	clock := newClock()
	r := setup(t, lifecycle.Options{Clock: clock})
	request, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "kept"))
	entered, cancelled := make(chan struct{}), make(chan struct{})
	var work context.Context
	if err := r.Spawn(request, "detach", func(ctx context.Context) {
		work = lifecycle.Detach(ctx)
		if lifecycle.ClockFrom(work) != clock || work.Value(key{}) != "kept" {
			t.Error("lost runtime or values")
		}
		// Child context cancellation should follow the job parent, without a
		// watcher goroutine for every detached wrapper or child.
		base := runtime.NumGoroutine()
		for range 1000 {
			child, stop := context.WithCancel(lifecycle.Detach(work))
			stop()
			if !errors.Is(child.Err(), context.Canceled) {
				t.Error(child.Err())
			}
		}
		if runtime.NumGoroutine() > base+3 {
			t.Error("detached children leaked watchers")
		}
		close(entered)
		<-work.Done()
		if !errors.Is(work.Err(), context.Canceled) || !errors.Is(context.Cause(work), context.Canceled) {
			t.Error("lost shutdown cancellation")
		}
		close(cancelled)
	}); err != nil {
		t.Fatal(err)
	}
	receive(t, entered)
	cancel()
	select {
	case <-cancelled:
		t.Fatal("request cancellation stopped I/O")
	default:
	}
	stopped := make(chan struct{})
	go func() { defer close(stopped); r.Shutdown(context.Background()) }()
	until(t, func() bool { return clock.Pending() == 2 })
	select {
	case <-cancelled:
		t.Fatal("cancelled before drain deadline")
	default:
	}
	clock.Advance(6 * time.Second)
	receive(t, cancelled)
	receive(t, stopped)
	// Detach remains bound to the old generation even after a new Start.
	r.Start(context.Background())
	if lifecycle.Detach(work).Err() == nil {
		t.Fatal("old job rebound to new generation")
	}
}

func TestPeriodicOnStartActivation(t *testing.T) {
	for _, tt := range []struct {
		name  string
		mode  lifecycle.Mode
		start lifecycle.StartMode
	}{
		{"boot", lifecycle.ModeRun, lifecycle.AtBoot},
		{"request", lifecycle.ModeRun, lifecycle.AfterFirstRequest},
		{"manual", lifecycle.ModeManual, lifecycle.AtBoot},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clock, reg := newClock(), lifecycle.NewRegistry()
			var activations atomic.Int64
			var activated time.Time
			rounds := make(chan time.Time, 3)
			reg.RegisterPeriodic(lifecycle.Periodic{Name: "activation", Start: tt.start, FirstDelay: 30 * time.Second,
				OnStart: func(ctx context.Context) { activated = lifecycle.ClockFrom(ctx).Now(); activations.Add(1) },
				Round:   func(ctx context.Context) error { rounds <- lifecycle.ClockFrom(ctx).Now(); return nil },
			})
			r := setup(t, lifecycle.Options{Registry: reg, Clock: clock, Mode: tt.mode})
			for generation := int64(1); generation <= 2; generation++ {
				r.Start(context.Background())
				r.Start(context.Background())
				if tt.mode == lifecycle.ModeManual {
					if err := r.RunRound(context.Background(), "activation"); err != nil {
						t.Fatal(err)
					}
					receive(t, rounds)
					if activations.Load() != 0 {
						t.Fatal("manual mode initialized owner")
					}
				} else {
					if tt.start == lifecycle.AfterFirstRequest {
						if activations.Load() != generation-1 {
							t.Fatal("initialized before request")
						}
						r.RequestSeen()
						r.RequestSeen()
					}
					if activations.Load() != generation || !activated.Equal(clock.Now()) {
						t.Fatal("activation is late or repeated")
					}
					until(t, func() bool { return clock.Pending() == 1 })
					clock.Advance(30 * time.Second)
					if at := receive(t, rounds); !at.Equal(activated.Add(30 * time.Second)) {
						t.Fatal("first delay drifted")
					}
					idle(t, r)
				}
				r.Shutdown(context.Background())
			}
		})
	}
}
