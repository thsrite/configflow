package upgrades

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
)

type addressClientFunc func(context.Context, agents.Request) (*agents.Response, error)

func (fn addressClientFunc) Do(ctx context.Context, request agents.Request) (*agents.Response, error) {
	return fn(ctx, request)
}

func addressIdle(t *testing.T, r *lifecycle.Runtime) {
	t.Helper()
	wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.WaitIdle(wait); err != nil {
		t.Fatal(err)
	}
}

func TestAddressFollowUsesProductionThrottleOnClock(t *testing.T) {
	if RetrySeconds != 120 || VerifyTimeout != 4 {
		t.Fatal("production address parameters changed", RetrySeconds, VerifyTimeout)
	}
	ResetAddressState()
	t.Cleanup(ResetAddressState)
	clock := lifecycletest.NewClock(time.Now())
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Clock: clock})
	runner.Start(context.Background())
	defer runner.Shutdown(context.Background())
	m := plainManager(t)
	agent := jsonx.Obj("id", "missing-agent", "host", "192.0.2.1")
	previous := startThread
	var count atomic.Int32
	startThread = func(ctx context.Context, name string, fn func(context.Context)) error {
		count.Add(1)
		return runner.Spawn(ctx, name, fn)
	}
	defer func() { startThread = previous }()
	call := func() {
		t.Helper()
		done := make(chan error, 1)
		if err := runner.Spawn(context.Background(), "follow-caller", func(ctx context.Context) { done <- Follow(ctx, m, agent, "192.0.2.2") }); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Follow blocked")
		}
		addressIdle(t, runner)
	}
	call()
	call()
	clock.Advance(119 * time.Second)
	call()
	if count.Load() != 1 {
		t.Fatal("throttle was bypassed", count.Load())
	}
	clock.Advance(time.Second)
	call()
	if count.Load() != 2 {
		t.Fatal("production120s deadline not respected", count.Load())
	}
}

func TestAddressRefusedSpawnRollsBackThrottle(t *testing.T) {
	ResetAddressState()
	t.Cleanup(ResetAddressState)
	m := plainManager(t)
	agent := jsonx.Obj("id", "missing-agent", "host", "192.0.2.1")
	previous := startThread
	startThread = func(context.Context, string, func(context.Context)) error { return lifecycle.ErrStopping }
	defer func() { startThread = previous }()
	if err := Follow(context.Background(), m, agent, "192.0.2.2"); !errors.Is(err, lifecycle.ErrStopping) {
		t.Fatal(err)
	}
	addressMu.Lock()
	remaining := len(attempts)
	addressMu.Unlock()
	if remaining != 0 {
		t.Fatal("refused worker retained throttle")
	}
}

func TestAddressJobCancellationReachesProductionIO(t *testing.T) {
	ResetAddressState()
	t.Cleanup(ResetAddressState)
	e := newUpgradeEnv(t)
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	runner.Start(e.ctx)
	defer runner.Shutdown(context.Background())
	previous := startThread
	startThread = runner.Spawn
	defer func() { startThread = previous }()
	entered, finished := make(chan struct{}), make(chan struct{})
	e.m.SetClient(addressClientFunc(func(ctx context.Context, request agents.Request) (*agents.Response, error) {
		if request.Timeout != agents.Seconds(4) || request.Method != "GET" || len(request.Body) != 0 {
			t.Error("production request parameters changed", request.Timeout, request.Method, len(request.Body))
		}
		close(entered)
		<-ctx.Done()
		close(finished)
		return nil, ctx.Err()
	}))
	if err := Follow(e.ctx, e.m, e.agent(), "192.0.2.2"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("address I/O not started")
	}
	runner.Shutdown(context.Background())
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("I/O lost shutdown context")
	}
	if e.agent().GetString("host") != "localhost" {
		t.Fatal("cancelled check updated host")
	}
	addressMu.Lock()
	notice := reported[e.ident]
	addressMu.Unlock()
	if notice {
		t.Fatal("shutdown cancellation produced an unreachable-address notice")
	}
}
