// Package lifecycle owns startup work, periodic loops, tracked detached jobs
// and phased shutdown. Production always drives loops; test instrumentation is
// compiled only under dedicated build tags.
package lifecycle

import (
	"context"

	"configflow/internal/logx"
	"configflow/internal/repo"
)

var logger = logx.Get("backend.app")

// NewProcessRuntime constructs a Runtime configured with the build's mode and
// a fresh Registry when opts.Registry is nil.
func NewProcessRuntime(opts Options) *Runtime {
	if opts.Registry == nil {
		opts.Registry = NewRegistry()
	}
	opts.Mode = defaultMode()
	return NewRuntime(opts)
}

// Stopping returns ctx's originating generation shutdown signal, or nil outside
// a Runtime.
func Stopping(ctx context.Context) <-chan struct{} {
	if g, ok := ctx.Value(generationKey{}).(*generation); ok {
		return g.stopping
	}
	return nil
}

// ContextSpawner adapts lockx.Spawner to the originating Runtime attached to
// ctx. Inside a Runtime it delegates to that Runtime (preserving generation
// admission checks); outside any Runtime it runs detached work with a fresh
// repo Scope and panic recovery without touching any process-wide Runtime.
type ContextSpawner struct{}

// Spawn dispatches to ctx's originating Runtime when present, or runs detached.
func (ContextSpawner) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	if g, ok := ctx.Value(generationKey{}).(*generation); ok {
		return g.runtime.Spawn(ctx, name, fn)
	}
	work := repo.WithScope(Detach(ctx))
	go func() {
		defer func() {
			if recover() != nil {
				logger.Errorf(msgTaskPanic, name)
			}
			repo.ResetConfigContext(work)
		}()
		fn(work)
	}()
	return nil
}
