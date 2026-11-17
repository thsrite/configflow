// Package lifecycletest supplies a deterministic lifecycle clock. It performs no
// sleeping or background work; tests advance time and synchronise with their rounds.
package lifecycletest

import (
	"sort"
	"sync"
	"time"

	"configflow/internal/clock"
)

// Clock is a manually advanced, concurrency-safe clock with one-shot timers.
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	next   uint64
	timers map[*timer]bool
}
type timer struct {
	clock *Clock
	at    time.Time
	id    uint64
	ch    chan time.Time
}

// NewClock starts an independent clock at the supplied time.
func NewClock(now time.Time) *Clock { return &Clock{now: now, timers: make(map[*timer]bool)} }

// Now returns the current simulated time.
func (c *Clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

// NewTimer creates a one-shot timer, firing immediately for nonpositive durations.
func (c *Clock) NewTimer(d time.Duration) clock.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	t := &timer{clock: c, at: c.now.Add(d), id: c.next, ch: make(chan time.Time, 1)}
	if d <= 0 {
		t.ch <- c.now
	} else {
		c.timers[t] = true
	}
	return t
}

func (t *timer) C() <-chan time.Time { return t.ch }
func (t *timer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	active := t.clock.timers[t]
	delete(t.clock.timers, t)
	return active
}

// Advance moves time forward and fires all due timers in deadline/creation order.
// Negative durations are ignored. It never runs a user's round synchronously.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d < 0 {
		return
	}
	c.now = c.now.Add(d)
	var due []*timer
	for t := range c.timers {
		if !t.at.After(c.now) {
			due = append(due, t)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		if due[i].at.Equal(due[j].at) {
			return due[i].id < due[j].id
		}
		return due[i].at.Before(due[j].at)
	})
	for _, t := range due {
		delete(c.timers, t)
		t.ch <- t.at
	}
}

// Pending returns how many active timers remain, for deterministic test barriers.
func (c *Clock) Pending() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.timers) }
