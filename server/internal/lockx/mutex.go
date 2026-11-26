package lockx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrTimeout means a positive lock wait limit elapsed before acquisition.
	ErrTimeout = errors.New("lock wait timed out")
	// ErrBusy means a try-only LevelJob acquisition found the lock occupied.
	ErrBusy          = errors.New("lock is busy")
	errUninitialized = errors.New("lockx: lock is not initialized")
)

// Mutex is a non-reentrant in-process lock with cancellable, bounded waits.
// Its zero value is unusable; construct it with NewMutex.
type Mutex struct {
	token   chan struct{}
	level   Level
	name    string
	tracked bool
	held    atomic.Bool
}

// NewMutex constructs a lock at one of the server's tracked levels. Names are
// stable diagnostic labels, not resource keys or other user-supplied values.
func NewMutex(level Level, name string) *Mutex {
	return newMutex(level, name, true)
}

func newMutex(level Level, name string, tracked bool) *Mutex {
	m := &Mutex{token: make(chan struct{}, 1), level: level, name: name, tracked: tracked}
	m.token <- struct{}{}
	return m
}

// TryLock attempts acquisition without waiting or order violations. A cancelled
// context fails. On failure the original context and a nil release are returned.
func (m *Mutex) TryLock(ctx context.Context) (context.Context, func(), bool) {
	if m == nil || m.token == nil || ctx.Err() != nil {
		return ctx, nil, false
	}
	select {
	case <-m.token:
		if ctx.Err() != nil {
			m.token <- struct{}{}
			return ctx, nil, false
		}
		m.held.Store(true)
		held, release := m.record(ctx)
		return held, release, true
	default:
		return ctx, nil, false
	}
}

// Lock acquires the lock, waiting at most limit when positive, otherwise until
// ctx ends. Context cancellation returns ctx.Err; the wait limit returns
// ErrTimeout. At LevelJob this is always a try, returning ErrBusy on contention.
func (m *Mutex) Lock(ctx context.Context, limit time.Duration) (context.Context, func(), error) {
	if m == nil || m.token == nil {
		return ctx, nil, errUninitialized
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	if m.level == LevelJob {
		held, release, ok := m.TryLock(ctx)
		if ok {
			return held, release, nil
		}
		if err := ctx.Err(); err != nil {
			return ctx, nil, err
		}
		return ctx, nil, ErrBusy
	}
	if m.tracked {
		checkOrder(ctx, m.level, m.name)
	}
	timer, stop := waitLimit(limit)
	defer stop()
	select {
	case <-ctx.Done():
		return ctx, nil, ctx.Err()
	case <-timer:
		return ctx, nil, ErrTimeout
	case <-m.token:
		if err := ctx.Err(); err != nil {
			m.token <- struct{}{}
			return ctx, nil, err
		}
		m.held.Store(true)
		held, release := m.record(ctx)
		return held, release, nil
	}
}

func (m *Mutex) record(ctx context.Context) (context.Context, func()) {
	if m.tracked {
		return hold(ctx, m.level, m.name, m.unlock)
	}
	var once sync.Once
	return ctx, func() { once.Do(m.unlock) }
}

func (m *Mutex) unlock() { m.held.Store(false); m.token <- struct{}{} }

func waitLimit(limit time.Duration) (<-chan time.Time, func()) {
	if limit <= 0 {
		return nil, func() {}
	}
	t := time.NewTimer(limit)
	return t.C, func() { t.Stop() }
}
