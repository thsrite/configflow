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

// Detach keeps values and drops request cancellation/deadlines. Inside a
// Runtime it remains cancellable by that generation's shutdown cancellation
// phase, even after a later generation starts. Outside it matches WithoutCancel.
// The returned view creates neither a goroutine nor a cancellation registration.
func Detach(ctx context.Context) context.Context {
	values := context.WithoutCancel(ctx)
	if g, ok := ctx.Value(generationKey{}).(*generation); ok {
		return detachedContext{Context: g.jobs, values: values}
	}
	return values
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
