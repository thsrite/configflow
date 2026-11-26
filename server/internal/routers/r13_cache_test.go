package routers

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/lockx"
)

func r13Eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		runtime.Gosched()
	}
}
func r13Runtime(t *testing.T) *lifecycle.Runtime {
	t.Helper()
	r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual, Grace: 50 * time.Millisecond})
	r.Start(t.Context())
	t.Cleanup(func() { r.Shutdown(t.Context()) })
	return r
}
func r13Known() []PendingEntry {
	return []PendingEntry{{ID: "synthetic", PendingSummary: PendingSummary{State: "synced", Items: []PendingItem{{Key: "routes", Label: "synthetic", Status: messagePendingAdd, Count: 1}}}}}
}
func TestR13CacheCoalescesRefreshAndCopies(t *testing.T) {
	rt := r13Runtime(t)
	clock := lifecycletest.NewClock(time.Unix(1700000000, 0))
	cache := NewPendingCache(rt, clock)
	gate := make(chan struct{})
	var calls atomic.Int32
	load := func(context.Context) ([]PendingEntry, error) { calls.Add(1); <-gate; return r13Known(), nil }
	const readers = 20
	results := make(chan PendingRead, readers)
	failures := make(chan error, readers)
	var wg sync.WaitGroup
	for range readers {
		wg.Go(func() {
			out, e := cache.Read(t.Context(), "agent", "signature", true, load)
			if e != nil {
				failures <- e
				return
			}
			results <- out
		})
	}
	r13Eventually(t, func() bool { return clock.Pending() == readers && calls.Load() == 1 })
	close(gate)
	wg.Wait()
	close(failures)
	for e := range failures {
		t.Fatal(e)
	}
	close(results)
	for out := range results {
		if out.Cached {
			t.Fatal("joined read marked cached")
		}
		out.Routers[0].Items[0].Label = "changed"
	}
	hit, e := cache.Read(t.Context(), "agent", "signature", false, load)
	if e != nil || !hit.Cached || hit.Routers[0].Items[0].Label != "synthetic" || calls.Load() != 1 {
		t.Fatalf("cache copy/reuse %+v %v calls%d", hit, e, calls.Load())
	}
}
func TestR13CacheUnknownRetryGenerationsAndBound(t *testing.T) {
	cache := NewPendingCache(r13Runtime(t), nil)
	var calls atomic.Int32
	load := func(context.Context) ([]PendingEntry, error) {
		n := calls.Add(1)
		v := r13Known()
		if n == 1 {
			v[0].State = "unknown"
		}
		return v, nil
	}
	for i := 0; i < 3; i++ {
		out, e := cache.Read(t.Context(), "a", "same", false, load)
		if e != nil || out.Cached != (i == 2) {
			t.Fatalf("unknown retention i%d %+v %v", i, out, e)
		}
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
	gate := make(chan struct{})
	started := make(chan struct{})
	old := make(chan PendingRead, 1)
	go func() {
		value, _ := cache.Read(t.Context(), "a", "old", false, func(context.Context) ([]PendingEntry, error) {
			close(started)
			<-gate
			v := r13Known()
			v[0].ID = "old"
			return v, nil
		})
		old <- value
	}()
	<-started
	newer, e := cache.Read(t.Context(), "a", "new", false, func(context.Context) ([]PendingEntry, error) { v := r13Known(); v[0].ID = "new"; return v, nil })
	if e != nil {
		t.Fatal(e)
	}
	close(gate)
	<-old
	hit, e := cache.Read(t.Context(), "a", "new", false, load)
	if e != nil || !hit.Cached || hit.Routers[0].ID != newer.Routers[0].ID {
		t.Fatal("old generation overwrote new", hit, e)
	}
	cache.Clear()
	for i := 0; i <= PendingCacheAgents; i++ {
		if _, e = cache.Read(t.Context(), fmt.Sprint(i), "s", false, func(context.Context) ([]PendingEntry, error) { return r13Known(), nil }); e != nil {
			t.Fatal(e)
		}
	}
	cache.mu.Lock()
	size := len(cache.entries)
	_, oldest := cache.entries["0"]
	cache.mu.Unlock()
	if size != PendingCacheAgents || oldest {
		t.Fatalf("LRU size=%d oldest=%v", size, oldest)
	}
}
func TestR13CacheDisconnectedWaiterAnd35SecondBudget(t *testing.T) {
	rt := r13Runtime(t)
	clock := lifecycletest.NewClock(time.Unix(1700000000, 0))
	cache := NewPendingCache(rt, clock)
	gate := make(chan struct{})
	entered := make(chan struct{})
	request, cancel := context.WithCancel(t.Context())
	ctx := rt.BindRequest(request)
	done := make(chan error, 1)
	go func() {
		_, e := cache.Read(ctx, "a", "s", false, func(work context.Context) ([]PendingEntry, error) {
			close(entered)
			<-gate
			if work.Err() != nil {
				return nil, work.Err()
			}
			return r13Known(), nil
		})
		done <- e
	}()
	<-entered
	cancel()
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	follower := make(chan error, 1)
	go func() {
		_, e := cache.Read(t.Context(), "a", "s", true, func(context.Context) ([]PendingEntry, error) {
			t.Error("refresh duplicated in-flight read")
			return nil, nil
		})
		follower <- e
	}()
	r13Eventually(t, func() bool { return clock.Pending() == 1 })
	clock.Advance(35 * time.Second)
	if e := <-follower; !errors.Is(e, ErrPendingTimeout) {
		t.Fatal(e)
	}
	close(gate)
	if e := rt.WaitIdle(t.Context()); e != nil {
		t.Fatal(e)
	}
	hit, e := cache.Read(t.Context(), "a", "s", false, func(context.Context) ([]PendingEntry, error) {
		t.Error("disconnected owner lost cache result")
		return nil, nil
	})
	if e != nil || !hit.Cached {
		t.Fatal(hit, e)
	}
}

type r13RejectSpawner struct{}

func (r13RejectSpawner) Spawn(context.Context, string, func(context.Context)) error {
	return lifecycle.ErrStopping
}
func TestR13CacheAdmissionFailureCanRetry(t *testing.T) {
	cache := NewPendingCache(r13RejectSpawner{}, nil)
	for range 2 {
		_, e := cache.Read(t.Context(), "a", "s", false, func(context.Context) ([]PendingEntry, error) { t.Fatal("refused worker ran"); return nil, nil })
		if !errors.Is(e, lifecycle.ErrStopping) {
			t.Fatal(e)
		}
	}
	_ = lockx.ErrPanic
}

func TestR13CacheShutdownRejectsBeforeMutation(t *testing.T) {
	rt := r13Runtime(t)
	ctx := rt.BindRequest(t.Context())
	cache := NewPendingCache(rt, nil)
	rt.Shutdown(t.Context())
	if _, e := cache.Read(ctx, "a", "s", false, func(context.Context) ([]PendingEntry, error) { t.Fatal("worker after shutdown"); return nil, nil }); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) != 0 {
		t.Fatal("cache mutated after shutdown")
	}
}
