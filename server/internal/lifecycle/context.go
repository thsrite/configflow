package lifecycle

import "context"

// ClockFrom returns the originating Runtime's clock, or the real clock for
// callers outside a Runtime. Owner business timestamps can retain their own
// existing clock injection; scheduling waits use this source.
func ClockFrom(ctx context.Context) Clock {
	if g, ok := ctx.Value(generationKey{}).(*generation); ok {
		return g.runtime.clock
	}
	return realClock{}
}

// cancelParentKey holds the context whose cancellation Detach keeps: a
// generation's job context for rounds, jobs, startup work and bound requests,
// or the overall shutdown deadline inside shutdown hooks and Drain.
type cancelParentKey struct{}

// Detach keeps values and drops request cancellation/deadlines. Inside a
// Runtime (rounds, jobs, startup work, requests bound with BindRequest) it
// remains cancellable by that generation's shutdown cancellation phase, even
// after a later generation starts. Inside shutdown hooks and Drain, which run
// after or across that phase, it is bounded by the overall shutdown grace
// instead. Outside it matches WithoutCancel. The returned view creates neither
// a goroutine nor a cancellation registration.
func Detach(ctx context.Context) context.Context {
	values := context.WithoutCancel(ctx)
	if parent, ok := ctx.Value(cancelParentKey{}).(context.Context); ok {
		return detachedContext{Context: parent, values: values}
	}
	return values
}

// BindRequest attaches the default Runtime's live generation to a request
// context (httpx does this for every request), so owners that Detach keep the
// generation's shutdown cancellation and Stopping/ClockFrom see it. A context
// already inside a Runtime, or a process without a live generation, is
// returned unchanged.
func BindRequest(ctx context.Context) context.Context { return currentRuntime().BindRequest(ctx) }

// BindRequest attaches this Runtime's live generation to a request context.
func (r *Runtime) BindRequest(ctx context.Context) context.Context {
	if _, ok := ctx.Value(generationKey{}).(*generation); ok {
		return ctx
	}
	r.mu.Lock()
	g := r.current
	live := g != nil && !g.closed
	r.mu.Unlock()
	if !live {
		return ctx
	}
	return withGeneration(ctx, g, g.jobs)
}

// withGeneration records g for Stopping/ClockFrom and the cancellation parent
// that Detach keeps.
func withGeneration(ctx context.Context, g *generation, cancelParent context.Context) context.Context {
	return context.WithValue(context.WithValue(ctx, generationKey{}, g), cancelParentKey{}, cancelParent)
}

// detachedContext is a Context view, not an owner retaining a request for I/O.
// The cancellation parent supplies Done/Err/Deadline and its internal values;
// WithoutCancel supplies only the caller's ordinary values.
type detachedContext struct {
	context.Context
	values context.Context
}

func (d detachedContext) Value(key any) any {
	if value := d.values.Value(key); value != nil {
		return value
	}
	return d.Context.Value(key)
}
