package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/lockx"
)

func TestSaveOperationWaitKeepsShutdownCancellation(t *testing.T) {
	_, release, err := runMu.Lock(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	request, disconnect := context.WithCancel(context.Background())
	job, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	bound := ctxx.WithParent(request, job)
	disconnect()
	done := make(chan error, 1)
	go func() { _, err := FlushNow(bound, "manual", false); done <- err }()
	select {
	case err := <-done:
		t.Fatal("request disconnect aborted save operation", err)
	case <-time.After(20 * time.Millisecond):
	}
	shutdown()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel save admission")
	}
	if lockx.Violations() != 0 {
		t.Fatal("normal save order violation", lockx.Violations())
	}
}
