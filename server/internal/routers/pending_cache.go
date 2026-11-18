package routers

import (
	"container/list"
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"configflow/internal/clock"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
)

const (
	// PendingRouterTimeout is the shared read budget of all routers of one Agent.
	PendingRouterTimeout = 25 * time.Second
	// PendingWorkers bounds concurrent device reads per Agent query.
	PendingWorkers = 8
	// PendingCacheAgents bounds the per-Agent result cache; there is no TTL.
	PendingCacheAgents = 64
	pendingWaitTimeout = PendingRouterTimeout + 10*time.Second
)

// ErrPendingTimeout means the shared read did not finish within the follower
// budget. The HTTP owner maps this to the existing 504 message.
var ErrPendingTimeout = errors.New(messagePendingWaitTimeout)

// PendingEntry is the reminder of one linked router, with its explicit profile.
type PendingEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	PendingSummary
}

// PendingRead contains an isolated cached value and the start time of its read.
type PendingRead struct {
	Routers   []PendingEntry
	CheckedAt string
	Cached    bool
}
type pendingLoad struct {
	routers   []PendingEntry
	checkedAt string
}
type pendingCacheEntry struct {
	agent, signature string
	serial           uint64
	pending          bool
	value            *pendingLoad
	element          *list.Element
}

// PendingCache reuses known results while signatures hold. Shared loads use
// tracked Flight workers; cancelling a waiter does not cancel the owner.
type PendingCache struct {
	mu      sync.Mutex
	entries map[string]*pendingCacheEntry
	order   *list.List
	serial  uint64
	clock   clock.Clock
	spawner lockx.Spawner
	flight  *lockx.Flight[uint64, pendingLoad]
}

// NewPendingCache constructs an isolated cache. Nil collaborators select the
// production clock and lifecycle; injected clocks advance the real 25/35s budgets.
func NewPendingCache(spawner lockx.Spawner, c clock.Clock) *PendingCache {
	if spawner == nil {
		spawner = lifecycle.DefaultSpawner{}
	}
	if c == nil {
		c = clock.Real{}
	}
	return &PendingCache{entries: map[string]*pendingCacheEntry{}, order: list.New(), clock: c, spawner: spawner, flight: lockx.NewFlight[uint64, pendingLoad](spawner, "upstream-pending")}
}

// Clear invalidates cached and in-flight generations without abandoning their
// tracked work. Late completion cannot repopulate or overwrite newer entries.
func (c *PendingCache) Clear() { c.mu.Lock(); defer c.mu.Unlock(); clear(c.entries); c.order.Init() }
func clonePendingEntries(entries []PendingEntry) []PendingEntry {
	out := append([]PendingEntry{}, entries...)
	for i := range out {
		out[i].Items = append([]PendingItem{}, entries[i].Items...)
	}
	return out
}
func pendingKnown(entries []PendingEntry) bool {
	return !slices.ContainsFunc(entries, func(e PendingEntry) bool { return e.State == "unknown" })
}
func pendingStamp(c clock.Clock) string { return c.Now().Local().Format("2006-01-02T15:04:05-07:00") }

// Read returns a copy and coalesces overlapping reads even with refresh. A new
// signature replaces only its cache generation, never an in-flight caller's result.
func (c *PendingCache) Read(ctx context.Context, agent, signature string, refresh bool, load func(context.Context) ([]PendingEntry, error)) (PendingRead, error) {
	owner := lifecycle.Detach(ctx)
	if err := owner.Err(); err != nil {
		return PendingRead{}, err
	}
	c.mu.Lock()
	if err := owner.Err(); err != nil {
		c.mu.Unlock()
		return PendingRead{}, err
	}
	entry := c.entries[agent]
	if entry == nil || entry.signature != signature {
		if entry != nil {
			c.order.Remove(entry.element)
		}
		entry = &pendingCacheEntry{agent: agent, signature: signature}
		entry.element = c.order.PushBack(entry)
		c.entries[agent] = entry
	} else {
		c.order.MoveToBack(entry.element)
	}
	for len(c.entries) > PendingCacheAgents {
		old := c.order.Front()
		victim := old.Value.(*pendingCacheEntry)
		delete(c.entries, victim.agent)
		c.order.Remove(old)
	}
	if entry.value != nil && !refresh && !entry.pending && pendingKnown(entry.value.routers) {
		value := entry.value
		out := PendingRead{Routers: clonePendingEntries(value.routers), CheckedAt: value.checkedAt, Cached: true}
		c.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return PendingRead{}, err
		}
		return out, nil
	}
	if !entry.pending {
		c.serial++
		entry.serial = c.serial
		entry.pending = true
	}
	serial := entry.serial
	// Register while the cache guard still proves this generation is pending;
	// completion cannot race registration into a second load for the same serial.
	result := c.flight.DoChan(ctx, serial, func(work context.Context) (value pendingLoad, err error) {
		defer func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if entry.serial == serial {
				entry.pending = false
			}
		}()
		if err = work.Err(); err != nil {
			return value, err
		}
		value.checkedAt = pendingStamp(c.clock)
		value.routers, err = load(work)
		if err != nil {
			return value, err
		}
		if err = work.Err(); err != nil {
			return value, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if err = work.Err(); err != nil {
			return value, err
		}
		if c.entries[agent] == entry && entry.serial == serial {
			copy := pendingLoad{clonePendingEntries(value.routers), value.checkedAt}
			entry.value = &copy
		}
		return value, nil
	})
	c.mu.Unlock()
	timer := c.clock.NewTimer(pendingWaitTimeout)
	defer timer.Stop()
	if err := ctx.Err(); err != nil {
		return PendingRead{}, err
	}
	select {
	case <-ctx.Done():
		return PendingRead{}, ctx.Err()
	case <-timer.C():
		return PendingRead{}, ErrPendingTimeout
	case got := <-result:
		if got.Err != nil {
			c.mu.Lock()
			if entry.serial == serial {
				entry.pending = false
			}
			c.mu.Unlock()
			return PendingRead{}, got.Err
		}
		return PendingRead{Routers: clonePendingEntries(got.Value.routers), CheckedAt: got.Value.checkedAt}, nil
	}
}
