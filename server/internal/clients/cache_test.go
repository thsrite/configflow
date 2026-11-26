package clients

import (
	"configflow/internal/routers/spec"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/routerdoc"
)

func snapshot(name string) Snapshot {
	r := routerdoc.NewReading()
	s := routerdoc.NewSighting("192.0.2.10")
	s.Hostname = name
	s.MAC = "AA:BB:CC:00:00:10"
	s.AddSource("arp")
	s.Presence = "online"
	r.Sightings[s.IP] = s
	return Snapshot{[]RouterReading{{Reading: r}}, name}
}
func cacheRouters(password string) []*spec.Router {
	return []*spec.Router{{RouterSettings: spec.RouterSettings{ID: "r1", Type: "routeros", Endpoint: "http://192.0.2.1"}, Password: password}}
}
func cacheRead(t *testing.T, c *DiscoveryCache, ctx context.Context, id string, rs []*spec.Router, load func(context.Context) (Snapshot, error), refresh bool) (Snapshot, CacheMetadata) {
	t.Helper()
	s, m, err := c.Read(ctx, id, rs, load, refresh)
	if err != nil {
		t.Fatal(err)
	}
	return s, m
}
func TestCacheTimingFailureIsolationAndCopies(t *testing.T) {
	clock := lifecycletest.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	runtime := lifecycle.NewRuntime(lifecycle.Options{Clock: clock, Mode: lifecycle.ModeManual})
	runtime.Start(context.Background())
	defer runtime.Shutdown(context.Background())
	ctx := runtime.BindRequest(context.Background())
	c := NewDiscoveryCache(runtime, clock)
	var calls int
	load := func(context.Context) (Snapshot, error) { calls++; return snapshot("old"), nil }
	rs := cacheRouters("synthetic-secret")
	first, meta := cacheRead(t, c, ctx, "office", rs, load, false)
	if meta.Cached || meta.Stale || calls != 1 {
		t.Fatalf("first: %+v %d", meta, calls)
	}
	first.Readings[0].Reading.Sightings["192.0.2.10"].Sources[0] = "corrupt"
	delete(first.Readings[0].Reading.Sightings, "192.0.2.10")
	clock.Advance(59 * time.Second)
	again, meta := cacheRead(t, c, ctx, "office", rs, load, false)
	if !meta.Cached || meta.Stale || calls != 1 || again.Readings[0].Reading.Sightings["192.0.2.10"].Sources[0] != "arp" {
		t.Fatal("fresh/copy failed")
	}
	clock.Advance(time.Second)
	_, meta = cacheRead(t, c, ctx, "office", rs, load, false)
	if !meta.Stale || calls != 1 {
		t.Fatal("stale must not automatically read")
	}
	_, meta = cacheRead(t, c, ctx, "office", rs, load, true)
	if meta.Cached || meta.Stale || calls != 2 {
		t.Fatal("refresh")
	}
	failed := func(context.Context) (Snapshot, error) {
		return Snapshot{[]RouterReading{{Error: "synthetic outage"}}, "failed"}, nil
	}
	kept, meta := cacheRead(t, c, ctx, "office", rs, failed, true)
	if kept.CheckedAt != "old" || !meta.Cached || meta.RefreshError != "synthetic outage" {
		t.Fatalf("last useful: %v %+v", kept, meta)
	}
	for _, tc := range []struct{ id, password string }{{"other", "synthetic-secret"}, {"office", "changed-secret"}} {
		_, meta := cacheRead(t, c, ctx, tc.id, cacheRouters(tc.password), load, false)
		if meta.Cached {
			t.Fatal("profile/connection isolation")
		}
	}
	c.Invalidate("office")
	_, meta = cacheRead(t, c, ctx, "office", rs, load, false)
	if meta.Cached {
		t.Fatal("invalidation")
	}
	for i := range 129 {
		cacheRead(t, c, ctx, fmt.Sprint(i), rs, load, false)
	}
	c.mu.Lock()
	size := len(c.entries)
	_, old := c.entries["0"]
	c.mu.Unlock()
	if size != 128 || old {
		t.Fatalf("LRU limit: %d %v", size, old)
	}
}
func TestCacheConcurrentReadsAndInvalidation(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(fmt.Sprint(refresh), func(t *testing.T) {
			c := NewDiscoveryCache(nil, nil)
			started := make(chan struct{})
			release := make(chan struct{})
			var calls atomic.Int32
			load := func(context.Context) (Snapshot, error) {
				if calls.Add(1) == 1 {
					close(started)
				}
				<-release
				return snapshot("old"), nil
			}
			const readers = 20
			results := make(chan Snapshot, readers)
			errorsCh := make(chan error, readers)
			var ready sync.WaitGroup
			ready.Add(readers)
			for range readers {
				go func() {
					ready.Done()
					ready.Wait()
					s, _, err := c.Read(context.Background(), "office", cacheRouters("synthetic"), load, refresh)
					results <- s
					errorsCh <- err
				}()
			}
			<-started
			// All callers start together. Cold readers scheduled after the load
			// finishes must reuse the installed snapshot; overlapping work shares Flight.
			close(release)
			for range readers {
				if err := <-errorsCh; err != nil {
					t.Fatal(err)
				}
				if (<-results).CheckedAt != "old" {
					t.Fatal("result")
				}
			}
			if !refresh && calls.Load() != 1 {
				t.Fatalf("cold reads: %d", calls.Load())
			}
		})
	}
	c := NewDiscoveryCache(nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, err := c.Read(context.Background(), "office", cacheRouters("synthetic"), func(context.Context) (Snapshot, error) { close(started); <-release; return snapshot("old"), nil }, false)
		done <- err
	}()
	<-started
	c.Invalidate("office")
	cacheRead(t, c, context.Background(), "office", cacheRouters("synthetic"), func(context.Context) (Snapshot, error) { return snapshot("new"), nil }, false)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s, _ := cacheRead(t, c, context.Background(), "office", cacheRouters("synthetic"), func(context.Context) (Snapshot, error) { t.Fatal("stale load revived"); return Snapshot{}, nil }, false)
	if s.CheckedAt != "new" {
		t.Fatal("inflight invalidation")
	}
}
func TestCacheLoadErrorReleasesFlight(t *testing.T) {
	c := NewDiscoveryCache(nil, nil)
	_, _, err := c.Read(context.Background(), "office", cacheRouters("synthetic"), func(context.Context) (Snapshot, error) { return Snapshot{}, errors.New("load failed") }, false)
	if err == nil {
		t.Fatal("lost failure")
	}
	s, _ := cacheRead(t, c, context.Background(), "office", cacheRouters("synthetic"), func(context.Context) (Snapshot, error) { return snapshot("new"), nil }, false)
	if s.CheckedAt != "new" {
		t.Fatal("flight stuck")
	}
}
