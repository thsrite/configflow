package lockx

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrPanic identifies a shared worker that panicked or exited without returning
// a result. The worker logs one diagnostic and wakes all callers with this error.
var ErrPanic = errors.New("lockx: shared work panicked")

var errMissingSpawner = errors.New(msgMissingSpawner)

// Spawner starts tracked background work and returns promptly. A nil error
// guarantees fn will run once in an independent goroutine; an error means fn
// will not run. The worker receives the spawner's context, allowing lifecycle
// to add shutdown cancellation without coupling work to request cancellation.
type Spawner interface {
	Spawn(ctx context.Context, name string, fn func(context.Context)) error
}

type flightCall[V any] struct {
	done     chan struct{}
	value    V
	err      error
	joiners  int
	finished bool
	channels []chan Result[V]
}

// Result is the result of shared work sent by DoChan. Shared reports whether
// another caller joined before completion.
type Result[V any] struct {
	// Value is fn's returned value, or its zero value when work did not return.
	Value V
	// Err is the work or admission error, if any.
	Err error
	// Shared is true when another caller joined before completion.
	Shared bool
}

// Flight coalesces overlapping calls per key. Callers can independently cancel
// their waits while work continues under an injected Spawner. Completed calls
// are not cached. The zero value returns an unconfigured-spawner error.
type Flight[K comparable, V any] struct {
	mu      sync.Mutex
	calls   map[K]*flightCall[V]
	spawner Spawner
	name    string
}

// NewFlight constructs shared work with a stable diagnostic name and a tracked
// spawner. O3-2 supplies an adapter for lifecycle.ContextSpawner; no unmanaged goroutine
// fallback is started when the spawner is missing.
func NewFlight[K comparable, V any](spawner Spawner, name string) *Flight[K, V] {
	return &Flight[K, V]{spawner: spawner, name: name, calls: make(map[K]*flightCall[V])}
}

// Do starts or joins work for key. fn runs with cancellation detached from the
// first caller (context values retained). Every caller, including the first,
// waits with its own ctx. Once another caller joins, the call's results are
// shared. Cancelling all waiters does not cancel or discard the running work.
func (f *Flight[K, V]) Do(ctx context.Context, key K, fn func(context.Context) (V, error)) (V, error, bool) {
	var zero V
	c, err := f.start(ctx, key, fn, nil)
	if err != nil {
		return zero, err, false
	}
	// Give a caller whose context already ended a deterministic cancellation
	// result, even when the worker also finished before the select.
	if err := ctx.Err(); err != nil {
		return zero, err, f.shared(c)
	}
	select {
	case <-ctx.Done():
		return zero, ctx.Err(), f.shared(c)
	case <-c.done:
		return c.value, c.err, f.shared(c)
	}
}

// DoChan starts or joins the same work as Do and returns a buffered channel
// receiving exactly one result. The channel is not closed. Cancellation of ctx
// does not discard the result or cancel the work: callers select on ctx.Done
// themselves. Registering a waiter starts no additional goroutine.
func (f *Flight[K, V]) DoChan(ctx context.Context, key K, fn func(context.Context) (V, error)) <-chan Result[V] {
	result := make(chan Result[V], 1)
	if _, err := f.start(ctx, key, fn, result); err != nil {
		result <- Result[V]{Err: err}
	}
	return result
}

func (f *Flight[K, V]) start(ctx context.Context, key K, fn func(context.Context) (V, error), result chan Result[V]) (*flightCall[V], error) {
	if f == nil || f.spawner == nil {
		return nil, errMissingSpawner
	}
	f.mu.Lock()
	c := f.calls[key]
	first := c == nil
	if first {
		c = &flightCall[V]{done: make(chan struct{})}
		f.calls[key] = c
	} else {
		c.joiners++
	}
	if result != nil {
		c.channels = append(c.channels, result)
	}
	f.mu.Unlock()
	if first {
		err := f.spawner.Spawn(context.WithoutCancel(ctx), f.name, func(workCtx context.Context) { f.run(workCtx, key, c, fn) })
		if err != nil {
			var zero V
			f.finish(key, c, zero, fmt.Errorf("start shared work: %w", err))
		}
	}
	return c, nil
}

func (f *Flight[K, V]) shared(c *flightCall[V]) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return c.joiners > 0
}

func (f *Flight[K, V]) run(ctx context.Context, key K, c *flightCall[V], fn func(context.Context) (V, error)) {
	var value V
	var err error
	returned := false
	defer func() {
		problem := recover()
		if problem != nil || !returned {
			err = ErrPanic
			// Panic payloads may contain credentials; log only the stable name.
			logger.Errorf(msgFlightPanic, f.name)
		}
		f.finish(key, c, value, err)
	}()
	value, err = fn(ctx)
	returned = true
}

func (f *Flight[K, V]) finish(key K, c *flightCall[V], value V, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.finished {
		return
	}
	c.value, c.err, c.finished = value, err, true
	if f.calls[key] == c {
		delete(f.calls, key)
	}
	close(c.done)
	for _, ch := range c.channels {
		ch <- Result[V]{Value: value, Err: err, Shared: c.joiners > 0}
	}
	c.channels = nil
}
