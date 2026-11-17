package lockx

import (
	"context"
	"sync"
	"time"
)

type keyedEntry struct {
	mutex *Mutex
	refs  int
}

// Keyed owns independent mutexes for resource keys. Entries are retained while
// holders or waiters reference them, then removed, so misses cannot grow a map.
// Use NewKeyed to assign the level and diagnostic name.
type Keyed[K comparable] struct {
	mu      sync.Mutex
	entries map[K]*keyedEntry
	level   Level
	name    string
}

// NewKeyed constructs a set of locks sharing a level and stable diagnostic name.
func NewKeyed[K comparable](level Level, name string) *Keyed[K] {
	return &Keyed[K]{entries: make(map[K]*keyedEntry), level: level, name: name}
}

func (k *Keyed[K]) pin(key K) *keyedEntry {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.entries == nil {
		k.entries = make(map[K]*keyedEntry)
	}
	e := k.entries[key]
	if e == nil {
		e = &keyedEntry{mutex: NewMutex(k.level, k.name)}
		k.entries[key] = e
	}
	e.refs++
	return e
}

func (k *Keyed[K]) unpin(key K, e *keyedEntry) {
	k.mu.Lock()
	defer k.mu.Unlock()
	e.refs--
	if e.refs == 0 {
		delete(k.entries, key)
	}
}

func (k *Keyed[K]) release(key K, e *keyedEntry, unlock func()) func() {
	var once sync.Once
	return func() { once.Do(func() { unlock(); k.unpin(key, e) }) }
}

// TryLock tries a key without waiting. Failed attempts leave no unused entry.
func (k *Keyed[K]) TryLock(ctx context.Context, key K) (context.Context, func(), bool) {
	e := k.pin(key)
	held, release, ok := e.mutex.TryLock(ctx)
	if !ok {
		k.unpin(key, e)
		return ctx, nil, false
	}
	return held, k.release(key, e, release), true
}

// Lock waits for a key using Mutex.Lock's cancellation, limit and LevelJob rules.
func (k *Keyed[K]) Lock(ctx context.Context, key K, limit time.Duration) (context.Context, func(), error) {
	e := k.pin(key)
	held, release, err := e.mutex.Lock(ctx, limit)
	if err != nil {
		k.unpin(key, e)
		return ctx, nil, err
	}
	return held, k.release(key, e, release), nil
}

// Held reports whether a key is currently held; querying a missing key allocates
// no entry. This describes current process state, not persisted receipts.
func (k *Keyed[K]) Held(key K) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	e := k.entries[key]
	return e != nil && e.mutex.held.Load()
}
