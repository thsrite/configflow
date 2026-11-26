// Package ctxx preserves an explicitly recorded work lifetime while detaching
// request cancellation. It does not own jobs, clocks, shutdown or goroutines.
package ctxx

import "context"

type parentKey struct{}
type cancellationParent struct{ ctx context.Context }

// WithParent records the cancellation lifetime used by Detach. Ordinary values
// still come from ctx. A nil parent explicitly removes that lifetime.
func WithParent(ctx, parent context.Context) context.Context {
	return context.WithValue(ctx, parentKey{}, cancellationParent{parent})
}

// Detach ignores request cancellation and deadlines, keeping ctx's values and
// its recorded work cancellation parent. Without a parent it is WithoutCancel.
func Detach(ctx context.Context) context.Context {
	values := context.WithoutCancel(ctx)
	if parent, ok := ctx.Value(parentKey{}).(cancellationParent); ok && parent.ctx != nil {
		return detachedContext{Context: parent.ctx, values: values}
	}
	return values
}

// Uninterruptible starts a local atomic or batch commit. It drops cancellation
// and masks the recorded parent, so nested Detach calls cannot restore it.
// Do not use it for network I/O, subprocesses or general background work.
func Uninterruptible(ctx context.Context) context.Context {
	return WithParent(context.WithoutCancel(ctx), nil)
}

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
