package contentsync

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
)

// runtimeGate delays the callback, retaining the real Runtime's detached
// context semantics rather than forwarding the caller's context like deferred.
type runtimeGate struct {
	runtime *lifecycle.Runtime
	gate    <-chan struct{}
}

func (s runtimeGate) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	return s.runtime.Spawn(ctx, name, func(work context.Context) {
		<-s.gate
		fn(work)
	})
}

func TestReviewCloseCancelsRealRuntimeWorkers(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprintf("before-start=%t", queued), func(t *testing.T) {
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			gate := make(chan struct{})
			if !queued {
				close(gate)
			}
			f := newFixture(t, func(o *Options) { o.Spawner = runtimeGate{runtime, gate} })
			var calls atomic.Int32
			started, cancelled, escape := make(chan struct{}), make(chan struct{}), make(chan struct{})
			f.prepare = func(ctx context.Context, _ deploy.Target, _ *deploy.Source, _, _ string) (*deploy.Bundle, error) {
				calls.Add(1)
				close(started)
				select {
				case <-ctx.Done():
					close(cancelled)
					return nil, ctx.Err()
				case <-escape:
					return f.baseline, nil
				}
			}
			t.Cleanup(func() { close(escape); runtime.Shutdown(context.Background()) })
			f.ctx = runtime.BindRequest(context.Background())
			requireStatus(t, f.status(), "checking", "")
			if !queued {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("worker did not start")
				}
			}
			f.c.Close()
			if queued {
				close(gate)
			} else {
				select {
				case <-cancelled:
				case <-time.After(time.Second):
					t.Error("Close did not cancel real Runtime preparation")
				}
			}
			wait, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := runtime.WaitIdle(wait); err != nil {
				t.Error("closed Checker left Runtime busy")
			}
			if queued && calls.Load() != 0 {
				t.Error("closed Checker started preparation after admission")
			}
			f.c.mu.Lock()
			defer f.c.mu.Unlock()
			if len(f.c.running) != 0 || len(f.c.slots) != 0 || f.c.keys.Held("agent-test") || len(f.c.entries) != 0 || len(f.c.details) != 0 {
				t.Error("Close retained worker resources or cached results")
			}
		})
	}
}

func TestReviewProductionCacheSpec(t *testing.T) {
	c := New(Options{})
	defer c.Close()
	// Literal expectations pin the production spec, independently of the fake
	// fixtures and the constants that their boundary advances might also use.
	if c.opts.Workers != 2 || cap(c.slots) != 2 || c.opts.SuccessTTL != 900*time.Second || c.opts.ErrorTTL != 30*time.Second || c.opts.MaxEntries != 512 || c.opts.MaxDetails != 8 || c.opts.MaxListBytes != 48*1024*1024 {
		t.Fatalf("production cache spec changed: %+v", c.opts)
	}
	f := newFixture(t, nil)
	for i := 0; i < 514; i++ {
		f.agent.Set("id", fmt.Sprintf("status-%d", i))
		f.status()
		f.spawn.run(t)
	}
	if len(f.c.entries) != 512 || f.c.entries["status-0"] != nil || f.c.entries["status-2"] == nil {
		t.Fatal("production 512-status LRU not enforced")
	}
	f.cfg.Set("test_rule", "DOMAIN,changed.example.com\n")
	for i := 0; i < 9; i++ {
		f.agent.Set("id", fmt.Sprintf("detail-%d", i))
		f.diff()
		f.spawn.run(t)
	}
	if len(f.c.details) != 8 || f.c.details["detail-0"] != nil || f.c.details["detail-1"] == nil {
		t.Fatal("production eight-detail FIFO not enforced")
	}
	// Exercise the real 48 MiB accounting boundary without allocating opaque
	// compressed payloads; FileEntries.Size itself is covered by A12.
	for i, size := range []int{16 * 1024 * 1024, 16 * 1024 * 1024, 16 * 1024 * 1024, 1} {
		c.storeDetail(context.Background(), fmt.Sprintf("bytes-%d", i), &detail{size: size, ttl: 900 * time.Second})
		if i == 2 && (c.bytes != 48*1024*1024 || len(c.details) != 3) {
			t.Fatal("production byte quota evicts at its inclusive boundary")
		}
	}
	if c.details["bytes-0"] != nil || c.bytes != 32*1024*1024+1 || len(c.details) != 3 {
		t.Fatal("production byte quota did not evict the oldest detail")
	}
}

func TestReviewUnregisterWaitsForBroadcast(t *testing.T) {
	a, b := newFixture(t, nil), newFixture(t, nil)
	var dispatcher Dispatcher
	unregister := dispatcher.Register(a.c)
	defer dispatcher.Register(b.c)()
	a.c.mu.Lock()
	done := make(chan struct{})
	go func() { dispatcher.Invalidate(); close(done) }()
	// Hold the first Checker until the broadcaster holds the dispatcher guard.
	deadline := time.Now().Add(5 * time.Second)
	for dispatcher.mu.TryLock() {
		dispatcher.mu.Unlock()
		if time.Now().After(deadline) {
			a.c.mu.Unlock()
			t.Fatal("broadcast did not acquire its guard")
		}
		runtime.Gosched()
	}
	unregistered := make(chan struct{})
	go func() { unregister(); close(unregistered) }()
	select {
	case <-unregistered:
		t.Error("unregister returned before the blocked broadcast")
	default:
	}
	a.c.mu.Unlock()
	<-done
	<-unregistered
	before := a.c.generation
	for i := 0; i < 100; i++ {
		dispatcher.Invalidate()
	}
	if a.c.generation != before || b.c.generation != 101 || dispatcher.Count() != 1 {
		t.Fatal("unregistered Checker received another invalidation")
	}
}
