// Package lifecycle is the registry of background loops and shutdown hooks
// of the server process (Python started its loops from backend/app.py and
// saved state from SIGTERM handlers).
//
// A package that owns a loop registers it from its wiring function:
//
//	lifecycle.Loop("scheduled-refresh", func(ctx context.Context) {
//	    ticker := time.NewTicker(time.Minute)
//	    defer ticker.Stop()
//	    for {
//	        select {
//	        case <-ctx.Done():
//	            return
//	        case <-ticker.C:
//	            runOnce(repo.WithScope(ctx))
//	        }
//	    }
//	})
//	lifecycle.OnShutdown("metrics-flush", func(ctx context.Context) { flush(ctx) })
//
// main calls Start once the HTTP listeners are up and Stop on SIGTERM/SIGINT:
// loops are cancelled and awaited, then the shutdown hooks run in reverse
// registration order (each within the remaining timeout).
package lifecycle

import (
	"context"
	"sync"
	"time"

	"configflow/internal/logx"
)

var logger = logx.Get("backend.app")

type loop struct {
	name string
	run  func(ctx context.Context)
}

type hook struct {
	name string
	run  func(ctx context.Context)
}

var (
	mu      sync.Mutex
	loops   []loop
	hooks   []hook
	started bool
	cancel  context.CancelFunc
	running sync.WaitGroup
	baseCtx context.Context
)

// Loop registers a background loop. Loops registered after Start start
// immediately.
func Loop(name string, run func(ctx context.Context)) {
	mu.Lock()
	defer mu.Unlock()
	l := loop{name, run}
	loops = append(loops, l)
	if started {
		launch(baseCtx, l)
	}
}

// OnShutdown registers a hook that runs after the loops stopped.
func OnShutdown(name string, run func(ctx context.Context)) {
	mu.Lock()
	hooks = append(hooks, hook{name, run})
	mu.Unlock()
}

func launch(ctx context.Context, l loop) {
	running.Add(1)
	go func() {
		defer running.Done()
		defer func() {
			if p := recover(); p != nil {
				logger.Errorf("background loop %s stopped: %v", l.name, p)
			}
		}()
		l.run(ctx)
	}()
}

// Start starts every registered loop under ctx.
func Start(ctx context.Context) {
	mu.Lock()
	defer mu.Unlock()
	if started {
		return
	}
	baseCtx, cancel = context.WithCancel(ctx)
	started = true
	for _, l := range loops {
		launch(baseCtx, l)
	}
}

// Stop cancels the loops, waits for them (up to timeout) and runs the
// shutdown hooks with the rest of the timeout.
func Stop(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	mu.Lock()
	stop := cancel
	registered := append([]hook(nil), hooks...)
	mu.Unlock()
	if stop != nil {
		stop()
	}
	done := make(chan struct{})
	go func() { running.Wait(); close(done) }()
	stopped := false
	select {
	case <-done:
		stopped = true
	case <-time.After(time.Until(deadline)):
		logger.Warning("background loops did not stop in time")
	}
	ctx, cancelHooks := context.WithDeadline(context.Background(), deadline)
	defer cancelHooks()
	for i := len(registered) - 1; i >= 0; i-- {
		func() {
			defer func() {
				if p := recover(); p != nil {
					logger.Errorf("shutdown hook %s failed: %v", registered[i].name, p)
				}
			}()
			registered[i].run(ctx)
		}()
	}
	if stopped {
		// The wait group may be reused only after every previous loop exited.
		// Keep started true on timeout so a new generation cannot overlap it.
		mu.Lock()
		started, cancel, baseCtx = false, nil, nil
		mu.Unlock()
	}
}

// Names lists the registered loops (diagnostics, tests).
func Names() []string {
	mu.Lock()
	defer mu.Unlock()
	names := make([]string, 0, len(loops))
	for _, l := range loops {
		names = append(names, l.name)
	}
	return names
}
