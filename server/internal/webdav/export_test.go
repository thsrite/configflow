package webdav

import (
	"context"
	"time"
)

// NewWorker creates an idle worker using the default tracked spawner. Explicit
// Close is terminal; Runtime startup activates the automatic worker each generation.
func NewWorker(delay time.Duration, upload UploadFunc) *Worker {
	return newWorker(delay, upload, workerOptions{})
}

// Close permanently refuses scheduling, discards pending work and waits within
// ctx. In-flight uploads retain their Runtime cancellation and own I/O timeouts.
func (w *Worker) Close(ctx context.Context) error {
	w.mu.Lock()
	w.closed = true
	w.stopLocked(w.generation)
	w.mu.Unlock()
	return w.WaitIdle(ctx)
}
