package rules

import (
	"context"
	"testing"
	"time"

	"configflow/internal/clock"
)

func cacheEventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !fn() {
		if time.Now().After(deadline) {
			t.Fatal("cache condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMatcherCleanupUsesProductionClockBounds(t *testing.T) {
	f := newFixture(t, nil)
	if f.cache.opt.IdleSeconds != DefaultIdleSeconds || DefaultIdleSeconds != 1800 || f.cache.opt.CleanupInterval != DefaultCleanupInterval || DefaultCleanupInterval != 60 || f.cache.opt.ReadRetrySeconds != 30 {
		t.Fatal("production cache parameters changed")
	}
	f.matcher(f.config, "default").Match("matched.example")
	for range 29 {
		f.clock.advance(60)
		cacheEventually(t, func() bool { return f.clock.Pending() == 1 })
		if f.cache.Size() != 1 {
			t.Fatal("evicted before1800s")
		}
	}
	f.clock.advance(59)
	if f.cache.Size() != 1 {
		t.Fatal("evicted before1800s")
	}
	f.clock.advance(1)
	cacheEventually(t, func() bool { return f.cache.Size() == 0 && !f.cache.CleanupScheduled() })
	if f.clock.Pending() != 0 {
		t.Fatal("timer leaked")
	}
}

type rejectedCacheSpawner struct{}

func (rejectedCacheSpawner) Spawn(context.Context, string, func(context.Context)) error {
	return context.Canceled
}

func TestMatcherCleanupRefusalAndNilSpawner(t *testing.T) {
	for _, name := range []string{"nil", "refused"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, func(o *MatcherCacheOptions) {
				o.Spawner = nil
				if name == "refused" {
					o.Spawner = rejectedCacheSpawner{}
				}
			})
			f.matcher(f.config, "default")
			if f.cache.CleanupScheduled() || f.clock.Pending() != 0 {
				t.Fatal("unowned cleanup scheduled")
			}
			f.clock.advance(DefaultIdleSeconds)
			if f.cache.Size() != 1 {
				t.Fatal("nil/refused spawner ran work")
			}
			f.cache.EvictIdle(f.clock.get())
			if f.cache.Size() != 0 {
				t.Fatal("synchronous eviction missing")
			}
		})
	}
}

var _ clock.Clock = (*testClock)(nil)
