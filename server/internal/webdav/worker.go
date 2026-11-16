package webdav

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Snapshot is a fresh, complete committed document and its backup settings.
type Snapshot struct {
	Document json.RawMessage
	Settings Settings
	DataDir  string
}

// Source loads a snapshot after the debounce period, outside repository locks.
type Source interface {
	Snapshot(context.Context) (Snapshot, error)
}

// UploadFunc performs and records one automatic upload.
type UploadFunc func(context.Context, Snapshot) error

// Worker serializes uploads and retains only the most recently scheduled source.
// It never retains a request context or one document per configuration save.
type Worker struct {
	mu             sync.Mutex
	delay          time.Duration
	upload         UploadFunc
	pending        Source
	deadline       time.Time
	wake           chan struct{}
	idle           chan struct{}
	active, closed bool
}

// NewWorker creates an idle worker; the first Schedule starts its goroutine.
func NewWorker(delay time.Duration, upload UploadFunc) *Worker {
	idle := make(chan struct{})
	close(idle)
	return &Worker{delay: delay, upload: upload, wake: make(chan struct{}, 1), idle: idle}
}

// Schedule replaces pending work and restarts its debounce deadline. It never
// waits for network I/O and refuses new work after Close.
func (w *Worker) Schedule(source Source) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	w.pending = source
	w.deadline = time.Now().Add(w.delay)
	if !w.active {
		w.active = true
		w.idle = make(chan struct{})
		go w.run()
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return true
}
func (w *Worker) run() {
	defer func() {
		if recover() != nil {
			w.mu.Lock()
			w.active = false
			w.pending = nil
			close(w.idle)
			w.mu.Unlock()
			logger.Warning(msgUnexpected)
		}
	}()
	for {
		w.mu.Lock()
		if w.closed || w.pending == nil {
			w.active = false
			close(w.idle)
			w.mu.Unlock()
			return
		}
		delay := time.Until(w.deadline)
		if delay > 0 {
			w.mu.Unlock()
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-w.wake:
			}
			timer.Stop()
			continue
		}
		source := w.pending
		w.pending = nil
		w.mu.Unlock()
		snapshot, err := source.Snapshot(context.Background())
		if err == nil && snapshot.Settings.Auto {
			err = w.upload(context.Background(), snapshot)
		}
		if err != nil {
			logger.Warningf(msgAutoFailed, DescribeError(err))
		}
		// No retries. Changes committed during upload are represented by one pending
		// source/deadline and read afresh on the next loop iteration.
	}
}

// WaitIdle waits for the current worker generation; it does not cancel uploads.
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

// Close discards pending work and waits within ctx for an in-flight upload to
// finish under its own I/O timeouts. Request cancellation never aborts a backup.
func (w *Worker) Close(ctx context.Context) error {
	w.mu.Lock()
	w.closed = true
	w.pending = nil
	select {
	case w.wake <- struct{}{}:
	default:
	}
	w.mu.Unlock()
	return w.WaitIdle(ctx)
}
