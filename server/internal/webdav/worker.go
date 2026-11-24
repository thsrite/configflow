package webdav

import (
	"context"
	"sync"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
)

// Source loads a snapshot after the debounce period, outside repository locks.
type Source interface {
	Snapshot(context.Context) (Snapshot, error)
}

// UploadFunc performs and records one automatic upload.
type UploadFunc func(context.Context, Snapshot) error

type workerOptions struct {
	Clock   lifecycle.Clock
	Spawner lockx.Spawner
}
type workerRun struct{ finished bool }
type workerGeneration struct {
	options    workerOptions
	pending    Source
	deadline   time.Time
	wake, stop chan struct{}
	stopped    bool
	active     *workerRun
}

// Worker coalesces pending sources and serializes uploads across all activation
// generations. It stores no request context or per-save configuration document.
type Worker struct {
	mu         sync.Mutex
	delay      time.Duration
	upload     UploadFunc
	generation *workerGeneration
	gate       chan struct{}
	idle       chan struct{}
	workers    int
	closed     bool
}

func newWorker(delay time.Duration, upload UploadFunc, options workerOptions) *Worker {
	idle := make(chan struct{})
	close(idle)
	w := &Worker{delay: delay, upload: upload, gate: make(chan struct{}, 1), idle: idle}
	w.activate(options)
	return w
}
func (w *Worker) activate(options workerOptions) *workerGeneration {
	if options.Clock == nil {
		options.Clock = lifecycle.ClockFrom(context.Background())
	}
	if options.Spawner == nil {
		options.Spawner = lifecycle.ContextSpawner{}
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	var pending Source
	if old := w.generation; old != nil {
		if !old.stopped {
			pending = old.pending
		}
		w.stopLocked(old)
	}
	g := &workerGeneration{options: options, wake: make(chan struct{}, 1), stop: make(chan struct{}), pending: pending}
	w.generation = g
	var run *workerRun
	if pending != nil {
		g.deadline = options.Clock.Now().Add(w.delay)
		run = w.startLocked(g)
	}
	w.mu.Unlock()
	if run != nil {
		w.spawn(g, run)
	}
	return g
}
func (w *Worker) startLocked(g *workerGeneration) *workerRun {
	run := new(workerRun)
	g.active = run
	if w.workers == 0 {
		w.idle = make(chan struct{})
	}
	w.workers++
	return run
}
func (w *Worker) finishLocked(g *workerGeneration, run *workerRun) {
	if run.finished {
		return
	}
	run.finished = true
	if g.active == run {
		g.active = nil
	}
	w.workers--
	if w.workers == 0 {
		close(w.idle)
	}
}
func (w *Worker) stopLocked(g *workerGeneration) {
	if !g.stopped {
		g.stopped = true
		close(g.stop)
	}
	g.pending = nil
}
func (w *Worker) spawn(g *workerGeneration, run *workerRun) bool {
	if err := g.options.Spawner.Spawn(context.Background(), "webdav-auto-backup", func(ctx context.Context) { w.run(ctx, g, run) }); err != nil {
		w.mu.Lock()
		g.pending = nil
		w.finishLocked(g, run)
		w.mu.Unlock()
		return false
	}
	return true
}

// Schedule retains only the latest pending source and restarts its debounce.
// False means the worker is stopped/closed or the Runtime refused admission.
func (w *Worker) Schedule(source Source) bool {
	w.mu.Lock()
	g := w.generation
	if w.closed || g.stopped {
		w.mu.Unlock()
		return false
	}
	g.pending = source
	g.deadline = g.options.Clock.Now().Add(w.delay)
	var run *workerRun
	if g.active == nil {
		run = w.startLocked(g)
	} else {
		select {
		case g.wake <- struct{}{}:
		default:
		}
	}
	w.mu.Unlock()
	if run != nil {
		return w.spawn(g, run)
	}
	return true
}
func (w *Worker) run(ctx context.Context, g *workerGeneration, run *workerRun) {
	// Runtime logs panics. This defer repairs admission before that recovery;
	// gate release belongs to each upload attempt, including panic paths.
	defer func() {
		w.mu.Lock()
		if !run.finished {
			g.pending = nil
			w.finishLocked(g, run)
		}
		w.mu.Unlock()
	}()
	for {
		w.mu.Lock()
		if g.stopped || g.pending == nil || ctx.Err() != nil {
			g.pending = nil
			w.finishLocked(g, run)
			w.mu.Unlock()
			return
		}
		select {
		case <-g.wake:
		default:
		}
		delay := g.deadline.Sub(g.options.Clock.Now())
		w.mu.Unlock()
		if delay > 0 {
			timer := g.options.Clock.NewTimer(delay)
			select {
			case <-timer.C():
			case <-g.wake:
			case <-g.stop:
			case <-ctx.Done():
			}
			timer.Stop()
			continue
		}
		select {
		case w.gate <- struct{}{}:
		case <-g.stop:
			return
		case <-ctx.Done():
			return
		}
		func() {
			defer func() { <-w.gate }()
			// Pending work may have changed while an older generation held the gate.
			// Re-read its source/deadline before taking a fresh configuration snapshot.
			w.mu.Lock()
			if g.stopped || ctx.Err() != nil || g.pending == nil || g.deadline.After(g.options.Clock.Now()) {
				w.mu.Unlock()
				return
			}
			source := g.pending
			g.pending = nil
			w.mu.Unlock()
			snapshot, err := source.Snapshot(ctx)
			if err == nil && snapshot.Settings.Auto {
				err = w.upload(ctx, snapshot)
			}
			if err != nil && ctx.Err() == nil {
				logger.Warningf(msgAutoFailed, DescribeError(err))
			}
		}()
	}
}

// WaitIdle waits only for this owner's jobs, across activation generations.
// Cancelling the wait never cancels a committed upload.
func (w *Worker) WaitIdle(ctx context.Context) error {
	w.mu.Lock()
	idle := w.idle
	w.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (w *Worker) stopGeneration(ctx context.Context, g *workerGeneration) error {
	w.mu.Lock()
	w.stopLocked(g)
	w.mu.Unlock()
	return w.WaitIdle(ctx)
}
