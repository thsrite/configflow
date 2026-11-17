package lockx

import (
	"context"
	"sync"
	"time"
)

// RWMutex provides shared/exclusive in-process leases with cancellation and
// limits. Waiting writers prevent new readers from starving them. Its zero
// value is unusable; use NewRWMutex. It does not support lock upgrades.
type RWMutex struct {
	mu             sync.Mutex
	readers        int
	writer         bool
	waitingWriters int
	changed        chan struct{}
	ready          bool
	level          Level
	name           string
}

// NewRWMutex constructs a shared/exclusive lock at the supplied tracked level.
func NewRWMutex(level Level, name string) *RWMutex {
	return &RWMutex{changed: make(chan struct{}), ready: true, level: level, name: name}
}

// TryLock attempts an exclusive lease without waiting or order checking.
func (m *RWMutex) TryLock(ctx context.Context) (context.Context, func(), bool) {
	return m.try(ctx, false)
}

// TryRLock attempts a shared lease without waiting or order checking.
func (m *RWMutex) TryRLock(ctx context.Context) (context.Context, func(), bool) {
	return m.try(ctx, true)
}

func (m *RWMutex) try(ctx context.Context, read bool) (context.Context, func(), bool) {
	if m == nil {
		return ctx, nil, false
	}
	m.mu.Lock()
	if m.changed == nil || ctx.Err() != nil || !m.available(read) {
		m.mu.Unlock()
		return ctx, nil, false
	}
	m.take(read)
	m.mu.Unlock()
	held, release := hold(ctx, m.level, m.name, func() { m.unlock(read) })
	return held, release, true
}

// RLock waits for a shared lease. A positive limit bounds the wait; limit <= 0
// waits until ctx ends. LevelJob always tries and returns ErrBusy if occupied.
func (m *RWMutex) RLock(ctx context.Context, limit time.Duration) (context.Context, func(), error) {
	return m.lock(ctx, limit, true)
}

// Lock waits for an exclusive lease, with the same limit and LevelJob rules as RLock.
func (m *RWMutex) Lock(ctx context.Context, limit time.Duration) (context.Context, func(), error) {
	return m.lock(ctx, limit, false)
}

func (m *RWMutex) lock(ctx context.Context, limit time.Duration, read bool) (context.Context, func(), error) {
	if m == nil || !m.ready {
		return ctx, nil, errUninitialized
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	if m.level == LevelJob {
		held, release, ok := m.try(ctx, read)
		if ok {
			return held, release, nil
		}
		if err := ctx.Err(); err != nil {
			return ctx, nil, err
		}
		return ctx, nil, ErrBusy
	}
	checkOrder(ctx, m.level, m.name)
	timer, stop := waitLimit(limit)
	defer stop()
	registered := false
	for {
		m.mu.Lock()
		if err := ctx.Err(); err != nil {
			m.dropWriter(registered)
			m.mu.Unlock()
			return ctx, nil, err
		}
		if m.available(read) {
			if registered {
				m.waitingWriters--
			}
			m.take(read)
			m.mu.Unlock()
			held, release := hold(ctx, m.level, m.name, func() { m.unlock(read) })
			return held, release, nil
		}
		if !read && !registered {
			m.waitingWriters++
			registered = true
		}
		changed := m.changed
		m.mu.Unlock()
		var err error
		select {
		case <-changed:
			continue
		case <-ctx.Done():
			err = ctx.Err()
		case <-timer:
			err = ErrTimeout
		}
		m.mu.Lock()
		m.dropWriter(registered)
		m.mu.Unlock()
		return ctx, nil, err
	}
}

func (m *RWMutex) available(read bool) bool {
	if m.writer {
		return false
	}
	if read {
		return m.waitingWriters == 0
	}
	return m.readers == 0
}

func (m *RWMutex) take(read bool) {
	if read {
		m.readers++
	} else {
		m.writer = true
	}
}

func (m *RWMutex) notify() { close(m.changed); m.changed = make(chan struct{}) }

func (m *RWMutex) dropWriter(registered bool) {
	if registered {
		m.waitingWriters--
		m.notify()
	}
}

func (m *RWMutex) unlock(read bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if read {
		m.readers--
	} else {
		m.writer = false
	}
	m.notify()
}
