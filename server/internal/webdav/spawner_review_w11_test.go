package webdav

import (
	"testing"

	"configflow/internal/lifecycle"
)

// A commit scheduled before webdav-worker-init must run as a job of the
// Runtime given to Register (tracked by shutdown), not as a bare goroutine.
func TestRegisterBindsPreStartupWorkerToRuntime(t *testing.T) {
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	s := NewService(nil)
	s.Register(rt, nil)
	w := s.automaticWorker()
	w.mu.Lock()
	spawner := w.generation.options.Spawner
	w.mu.Unlock()
	if spawner != rt {
		t.Fatalf("pre-startup spawner = %T, want the registered Runtime", spawner)
	}
}
