package agents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRuntimeDeleteQueuedAtShutdownPreservesState(t *testing.T) {
	s := NewRuntimeState()
	s.entries["device"] = heartbeatEntry{HeartbeatAt: 3}
	s.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Delete(ctx, "device") }()
	select {
	case err := <-done:
		s.mu.Unlock()
		t.Fatalf("deleted through held lock: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	s.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if s.entries["device"].HeartbeatAt != 3 || s.deletions != 0 || len(s.deleted) != 0 {
		t.Fatal("shutdown deleted runtime or advanced tombstone")
	}
}
