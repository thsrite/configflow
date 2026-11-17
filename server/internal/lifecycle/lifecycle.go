// Package lifecycle owns startup work, periodic loops, tracked detached jobs
// and phased shutdown. Production always drives loops; test instrumentation is
// compiled only under dedicated build tags. Legacy entry points remain adapters.
package lifecycle

import (
	"context"
	"errors"
	"sync"
	"time"

	"configflow/internal/logx"
)

var logger = logx.Get("backend.app")

type loop struct {
	name string
	run  func(context.Context)
}

// ErrRunning rejects default runtime reconfiguration while work is active.
var ErrRunning = errors.New("lifecycle: runtime is running")

var defaultMu sync.Mutex
var defaultRuntime = NewRuntime(Options{Mode: defaultMode()})
var defaultStarting bool

func currentRuntime() *Runtime { defaultMu.Lock(); defer defaultMu.Unlock(); return defaultRuntime }

// Configure replaces an idle/stopped default runtime, preserving registrations
// when Registry is nil. Its mode is fixed by the build, never by environment or
// options. Active jobs or Start/Shutdown in progress return ErrRunning.
func Configure(opts Options) error {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	r := defaultRuntime
	r.mu.Lock()
	g := r.current
	running := defaultStarting || g != nil && (g.active > 0 || g.runners > 0 || !g.closed && (g.started || g.draining))
	r.mu.Unlock()
	if running {
		return ErrRunning
	}
	if opts.Registry == nil {
		opts.Registry = r.registry
	}
	opts.Mode = defaultMode()
	defaultRuntime = NewRuntime(opts)
	return nil
}

// RegisterPeriodic adds a default specification and starts a newly named loop if eligible.
func RegisterPeriodic(p Periodic) {
	r := currentRuntime()
	r.registry.RegisterPeriodic(p)
	r.startEligible()
}

// RegisterStartup registers synchronous once-per-start work on the default runtime.
func RegisterStartup(s Startup) { currentRuntime().registry.RegisterStartup(s) }

// RegisterShutdown registers a phased hook on the default runtime.
func RegisterShutdown(h ShutdownHook) { currentRuntime().registry.RegisterShutdown(h) }

// Loop keeps legacy long-running loops until O3-4 moves their timing to Periodic.
func Loop(name string, run func(context.Context)) {
	r := currentRuntime()
	r.registry.addLegacy(loop{name, run})
	r.startEligible()
}

// OnShutdown keeps legacy hooks in reverse registration order after drainage.
func OnShutdown(name string, run func(context.Context)) {
	r := currentRuntime()
	r.registry.mu.Lock()
	defer r.registry.mu.Unlock()
	h := ShutdownHook{Name: name, Phase: PhaseAfterDrain, Run: run}
	for i := range r.registry.hooks {
		if r.registry.hooks[i].Name == name && r.registry.hooks[i].Phase == PhaseAfterDrain {
			r.registry.hooks[i] = h
			return
		}
	}
	r.registry.hooks = append([]ShutdownHook{h}, r.registry.hooks...)
}

// Start runs startup work and starts a default generation; after Stop, another
// Start restarts all registered loops with fresh cancellation and counters.
func Start(ctx context.Context) {
	defaultMu.Lock()
	r := defaultRuntime
	defaultStarting = true
	defaultMu.Unlock()
	defer func() { defaultMu.Lock(); defaultStarting = false; defaultMu.Unlock() }()
	r.Start(ctx)
	reportReady(r)
}

// Stop shuts down the current generation, bounded by timeout and its grace.
func Stop(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	currentRuntime().Shutdown(ctx)
}

// Names lists registered loops and startup work.
func Names() []string { return currentRuntime().Names() }

// Spawn starts tracked detached work on the configured default runtime.
func Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	return defaultRuntime.Spawn(ctx, name, fn)
}

// Stopping returns ctx's original generation signal, or the current default's.
func Stopping(ctx context.Context) <-chan struct{} { return currentRuntime().Stopping(ctx) }

// RequestSeen admits the default generation's first-request loops.
func RequestSeen() { currentRuntime().RequestSeen() }

// DefaultSpawner adapts global Spawn to lockx.Spawner; its zero value is usable.
// A *Runtime implements the same interface for isolated shared calls.
type DefaultSpawner struct{}

// Spawn forwards to global Spawn.
func (DefaultSpawner) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	return Spawn(ctx, name, fn)
}
