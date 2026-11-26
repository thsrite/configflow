package discovery

import (
	"context"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/repo"
)

func TestSynchronousJobKeepsShutdownContextWithFreshScope(t *testing.T) {
	svc, ctx := jobFixture(t)
	clock := lifecycletest.NewClock(time.Now())
	runner := lifecycle.NewRuntime(lifecycle.Options{Clock: clock})
	runner.Start(ctx)
	defer runner.Shutdown(context.Background())
	request, cancel := context.WithCancel(repo.WithScope(ctx))
	original := repo.ScopeOf(request)
	cancel()
	entered, cancelled, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	if err := runner.Spawn(request, "automatic-probe", func(work context.Context) {
		defer close(done)
		_, _, _ = svc.startJob(work, "default", "probe", true, func(ioCtx context.Context, _ func(int, int)) (outcome, error) {
			if ioCtx.Err() != nil || repo.ScopeOf(ioCtx) == original || lifecycle.ClockFrom(ioCtx) != clock {
				t.Error("synchronous job lost shutdown context or reused request scope")
			}
			close(entered)
			<-ioCtx.Done()
			close(cancelled)
			return outcome{}, ioCtx.Err()
		})
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("job not started")
	}
	select {
	case <-cancelled:
		t.Fatal("request cancelled I/O")
	default:
	}
	stopped := make(chan struct{})
	go func() { defer close(stopped); runner.Shutdown(context.Background()) }()
	deadline := time.Now().Add(5 * time.Second)
	for clock.Pending() != 2 {
		if time.Now().After(deadline) {
			t.Fatal("shutdown not draining")
		}
		time.Sleep(time.Millisecond)
	}
	clock.Advance(6 * time.Second)
	for _, ch := range []chan struct{}{cancelled, done, stopped} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled work leaked")
		}
	}
	if j := svc.runningJob(""); j != nil {
		t.Fatal("synchronous job left running")
	}
}
