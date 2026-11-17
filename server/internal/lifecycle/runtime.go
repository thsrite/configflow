package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"configflow/internal/lockx"
	"configflow/internal/repo"
	"configflow/internal/weburl"
)

// ErrStopping rejects new jobs after the shutdown drain has ended.
var ErrStopping = errors.New("lifecycle: runtime is stopping")

// ErrPanic identifies a callback that panicked; panic payloads are not logged.
var ErrPanic = errors.New("lifecycle: task panicked")

type generationKey struct{}
type generation struct {
	runtime                              *Runtime
	root                                 context.Context
	loops                                context.Context
	stopLoops                            context.CancelFunc
	jobs                                 context.Context
	stopJobs                             context.CancelFunc
	stopping                             chan struct{}
	changed                              chan struct{}
	started, requested, draining, closed bool
	admit                                bool
	active, runners                      int
	drivers                              map[string]bool
	busy                                 map[string]bool
	startups                             map[string]bool
}

// Runtime owns independent generations of loops and jobs. Start and Shutdown
// can be repeated; a previous generation's cleanup cannot change its successor.
// Its zero value is unusable; construct it with NewRuntime.
type Runtime struct {
	opMu      sync.Mutex
	startupMu sync.Mutex
	mu        sync.Mutex
	clock     Clock
	mode      Mode
	grace     time.Duration
	registry  *Registry
	drain     func(context.Context) error
	current   *generation
}

// NewRuntime constructs an independent runtime without starting background work.
func NewRuntime(opts Options) *Runtime {
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	if opts.Registry == nil {
		opts.Registry = NewRegistry()
	}
	if opts.Grace <= 0 {
		opts.Grace = 8 * time.Second
	}
	return &Runtime{clock: opts.Clock, mode: opts.Mode, grace: opts.Grace, registry: opts.Registry, drain: opts.Drain}
}

func (r *Runtime) generation(ctx context.Context) *generation {
	if r.current == nil {
		loops, stopLoops := context.WithCancel(ctx)
		jobs, stopJobs := context.WithCancel(context.Background())
		r.current = &generation{runtime: r, root: ctx, loops: loops, stopLoops: stopLoops, jobs: jobs, stopJobs: stopJobs,
			stopping: make(chan struct{}), changed: make(chan struct{}), admit: true,
			drivers: map[string]bool{}, busy: map[string]bool{}, startups: map[string]bool{}}
	}
	return r.current
}

func (r *Runtime) notify(g *generation) { close(g.changed); g.changed = make(chan struct{}) }

// RunStartup runs each registered startup once per generation, synchronously
// in registration order. Errors and panics are logged and later steps still run.
func (r *Runtime) RunStartup(ctx context.Context) {
	r.startupMu.Lock()
	defer r.startupMu.Unlock()
	for _, s := range r.registry.snapshot().startups {
		r.mu.Lock()
		g := r.generation(ctx)
		if g.closed || g.draining || g.startups[s.Name] {
			r.mu.Unlock()
			continue
		}
		g.startups[s.Name] = true
		r.mu.Unlock()
		work, finish := r.workContext(ctx, g, false)
		func() {
			defer finish()
			defer func() {
				if recover() != nil {
					logger.Errorf(msgTaskPanic, s.Name)
				}
			}()
			if err := s.Run(work); err != nil {
				logger.Warningf(msgStartupFailed, s.Name, weburl.SafeErrorDetails(err))
			}
		}()
	}
}

// Start runs startup work and starts eligible loops. Repeated calls while
// running are idempotent; after Shutdown it creates a fresh generation.
func (r *Runtime) Start(ctx context.Context) {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	r.mu.Lock()
	if r.current != nil && r.current.closed {
		r.current = nil
	}
	g := r.generation(ctx)
	if g.started {
		r.mu.Unlock()
		return
	}
	g.stopLoops()
	g.loops, g.stopLoops = context.WithCancel(ctx)
	g.root = ctx
	r.mu.Unlock()
	r.RunStartup(ctx)
	r.mu.Lock()
	g.started = true
	r.mu.Unlock()
	r.startEligible()
}

func (r *Runtime) startEligible() {
	if r.mode == ModeManual {
		return
	}
	snapshot := r.registry.snapshot()
	r.mu.Lock()
	defer r.mu.Unlock()
	g := r.current
	if g == nil || !g.started || g.draining || g.closed {
		return
	}
	for _, p := range snapshot.periodics {
		if g.drivers[p.Name] || p.Start == AfterFirstRequest && !g.requested {
			continue
		}
		g.drivers[p.Name] = true
		g.runners++
		go r.drive(g, p)
	}
	for _, l := range snapshot.legacy {
		if g.drivers[l.name] {
			continue
		}
		g.drivers[l.name] = true
		g.runners++
		go r.runLegacy(g, l)
	}
}

func (r *Runtime) finishRunner(g *generation, name string) {
	if recover() != nil {
		logger.Errorf(msgTaskPanic, name)
	}
	r.mu.Lock()
	g.runners--
	r.notify(g)
	r.mu.Unlock()
}

func (r *Runtime) runLegacy(g *generation, l loop) {
	defer r.finishRunner(g, l.name)
	ctx := context.WithValue(g.loops, generationKey{}, g)
	l.run(ctx)
}

func (r *Runtime) pause(ctx context.Context, d time.Duration, wake *Signal) bool {
	if ctx.Err() != nil {
		return false
	}
	if d <= 0 {
		return true
	}
	timer := r.clock.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C():
		return ctx.Err() == nil
	case <-wake.channel():
		return ctx.Err() == nil
	}
}

func (r *Runtime) drive(g *generation, p Periodic) {
	defer r.finishRunner(g, p.Name)
	if !r.pause(g.loops, p.FirstDelay, p.Wake) {
		return
	}
	for {
		err := r.round(g.root, g, p)
		if errors.Is(err, ErrStopping) {
			return
		}
		if p.Interval == nil {
			return
		}
		d := p.Interval(context.WithValue(g.root, generationKey{}, g))
		if d <= 0 {
			logger.Errorf(msgInvalidInterval, p.Name)
			return
		}
		if !r.pause(g.loops, d, p.Wake) {
			return
		}
	}
}

// RequestSeen starts AfterFirstRequest loops once per runtime generation.
func (r *Runtime) RequestSeen() {
	r.mu.Lock()
	g := r.generation(context.Background())
	if !g.draining && !g.closed {
		g.requested = true
	}
	r.mu.Unlock()
	r.startEligible()
}

func (r *Runtime) workContext(ctx context.Context, g *generation, detach bool) (context.Context, func()) {
	if detach {
		ctx = context.WithoutCancel(ctx)
	}
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(g.jobs, cancel)
	work = repo.WithScope(context.WithValue(work, generationKey{}, g))
	return work, func() { stop(); cancel(); repo.ResetConfigContext(work) }
}

// RunRound executes one named periodic round synchronously, including in manual
// mode. An overlapping round returns lockx.ErrBusy; shutdown rejects new rounds.
func (r *Runtime) RunRound(ctx context.Context, name string) error {
	for _, p := range r.registry.snapshot().periodics {
		if p.Name == name {
			r.mu.Lock()
			g := r.generation(ctx)
			r.mu.Unlock()
			return r.round(ctx, g, p)
		}
	}
	return fmt.Errorf("lifecycle: unknown periodic task %q", name)
}

func (r *Runtime) round(ctx context.Context, g *generation, p Periodic) (err error) {
	r.mu.Lock()
	if g.draining || g.closed {
		r.mu.Unlock()
		return ErrStopping
	}
	if g.busy[p.Name] {
		r.mu.Unlock()
		return lockx.ErrBusy
	}
	g.busy[p.Name] = true
	g.active++
	r.notify(g)
	r.mu.Unlock()
	work, finish := r.workContext(ctx, g, true)
	emitEvent(r.clock.Now(), p.Name, "round-start")
	defer func() {
		if recover() != nil {
			err = ErrPanic
			logger.Errorf(msgTaskPanic, p.Name)
		}
		finish()
		emitEvent(r.clock.Now(), p.Name, "round-end")
		r.mu.Lock()
		delete(g.busy, p.Name)
		g.active--
		r.notify(g)
		r.mu.Unlock()
	}()
	err = p.Round(work)
	if err != nil {
		logger.Warningf(msgRoundFailed, p.Name, weburl.SafeErrorDetails(err))
	}
	return err
}

// Spawn starts tracked, detached work with a fresh repo Scope. Admission remains
// open while Drain and existing work finish; it closes at the drain deadline.
func (r *Runtime) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	r.mu.Lock()
	g := r.generation(context.Background())
	if !g.admit || g.closed {
		r.mu.Unlock()
		return ErrStopping
	}
	g.active++
	r.notify(g)
	r.mu.Unlock()
	work, finish := r.workContext(ctx, g, true)
	go func() {
		emitEvent(r.clock.Now(), name, "job-start")
		defer func() {
			if recover() != nil {
				logger.Errorf(msgTaskPanic, name)
			}
			finish()
			emitEvent(r.clock.Now(), name, "job-end")
			r.mu.Lock()
			g.active--
			r.notify(g)
			r.mu.Unlock()
		}()
		fn(work)
	}()
	return nil
}

// Stopping returns this generation's shutdown signal. Job contexts keep their
// original generation's signal even when the Runtime is later restarted.
func (r *Runtime) Stopping(ctx context.Context) <-chan struct{} {
	if g, ok := ctx.Value(generationKey{}).(*generation); ok {
		return g.stopping
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.generation(context.Background()).stopping
}

// WaitIdle waits for current jobs and rounds, excluding paused loop drivers.
func (r *Runtime) WaitIdle(ctx context.Context) error {
	r.mu.Lock()
	g := r.current
	r.mu.Unlock()
	if g == nil {
		return nil
	}
	return r.wait(ctx, g, false, nil)
}

func (r *Runtime) wait(ctx context.Context, g *generation, runners bool, drained <-chan struct{}) error {
	for {
		drainDone := drained == nil
		if drained != nil {
			select {
			case <-drained:
				drainDone = true
			default:
			}
		}
		r.mu.Lock()
		idle := g.active == 0 && (!runners || g.runners == 0)
		changed := g.changed
		r.mu.Unlock()
		if idle && drainDone {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		case <-drained:
			drained = nil
		}
	}
}

// Names lists registered loops and startup work (not shutdown hooks).
func (r *Runtime) Names() []string { return r.registry.names() }

// clockContext bounds a context using the injected clock. Its one controller
// goroutine is always joined by cancel; it never outlives the owning shutdown.
func (r *Runtime) clockContext(parent context.Context, d time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	timer := r.clock.NewTimer(d)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C():
			cancel()
		}
	}()
	return ctx, func() { cancel(); <-done }
}

func (r *Runtime) hook(ctx context.Context, h ShutdownHook) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if recover() != nil {
				logger.Errorf(msgTaskPanic, h.Name)
			}
		}()
		h.Run(ctx)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Shutdown signals stopping, drains handlers/jobs, cancels at grace−2s, waits at
// most another 0.5s, then runs after-drain and close hooks. Drain and hooks must
// honour ctx: ignored cancellation cannot be forcibly killed by Go, but overall
// grace bounds how long Shutdown waits. An idle runtime without Drain ends early.
func (r *Runtime) Shutdown(ctx context.Context) {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	r.mu.Lock()
	g := r.current
	if g == nil || g.closed {
		r.mu.Unlock()
		return
	}
	g.draining = true
	close(g.stopping)
	r.notify(g)
	r.mu.Unlock()
	start := r.clock.Now()
	g.stopLoops()
	overall, stopOverall := r.clockContext(ctx, r.grace)
	defer stopOverall()
	hooks := r.registry.snapshot().hooks
	for _, h := range hooks {
		if h.Phase == PhaseStopSignal {
			r.hook(overall, h)
		}
	}
	drainCtx, stopDrain := r.clockContext(overall, start.Add(r.grace-2*time.Second).Sub(r.clock.Now()))
	var drained chan struct{}
	if r.drain != nil {
		drained = make(chan struct{})
		go func() {
			defer close(drained)
			defer func() {
				if recover() != nil {
					logger.Errorf(msgTaskPanic, "http-drain")
				}
			}()
			if err := r.drain(drainCtx); err != nil && drainCtx.Err() == nil {
				logger.Warningf(msgDrainFailed, weburl.SafeErrorDetails(err))
			}
		}()
	}
	_ = r.wait(drainCtx, g, true, drained)
	stopDrain()
	r.mu.Lock()
	g.admit = false
	r.mu.Unlock()
	g.stopJobs()
	cancelCtx, stopCancel := r.clockContext(overall, 500*time.Millisecond)
	if err := r.wait(cancelCtx, g, true, nil); err != nil {
		logger.Warning(msgShutdownTimeout)
	}
	stopCancel()
	for _, phase := range []Phase{PhaseAfterDrain, PhaseClose} {
		for _, h := range hooks {
			if h.Phase == phase {
				r.hook(overall, h)
			}
		}
	}
	r.mu.Lock()
	g.closed = true
	r.notify(g)
	r.mu.Unlock()
}
