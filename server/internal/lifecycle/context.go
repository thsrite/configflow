package lifecycle

import (
	"configflow/internal/ctxx"
	"context"
)

// ClockFrom returns the originating Runtime's clock, or the real clock for
// callers outside a Runtime. Owner business timestamps can retain their own
// existing clock injection; scheduling waits use this source.
func ClockFrom(ctx context.Context) Clock {
	if g, ok := ctx.Value(generationKey{}).(*generation); ok {
		return g.runtime.clock
	}
	return realClock{}
}

// Detach keeps values and drops request cancellation/deadlines. Inside a
// Runtime (rounds, jobs, startup work, requests bound with BindRequest) it
// remains cancellable by that generation's shutdown cancellation phase, even
// after a later generation starts. Inside shutdown hooks and Drain, which run
// after or across that phase, it is bounded by the overall shutdown grace
// instead. Outside it matches WithoutCancel. The returned view creates neither
// a goroutine nor a cancellation registration.
func Detach(ctx context.Context) context.Context {
	return ctxx.Detach(ctx)
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
	return withGeneration(ctx, g, g.detached)
}

// withGeneration records g for Stopping/ClockFrom and the cancellation parent
// that Detach keeps.
func withGeneration(ctx context.Context, g *generation, cancelParent context.Context) context.Context {
	return ctxx.WithParent(context.WithValue(ctx, generationKey{}, g), cancelParent)
}

// Tracked reports whether the caller is already an owned Runtime job or round.
// A bound HTTP request has a cancellation parent but is not a tracked worker.
func Tracked(ctx context.Context) bool {
	return ctx.Value(trackedKey{}) == true
}

type trackedKey struct{}
