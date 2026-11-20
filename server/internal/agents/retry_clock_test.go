package agents

import (
	"context"
	"testing"
	"time"

	sharedclock "configflow/internal/clock"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
)

type busyTestClock struct {
	sharedclock.Clock
	delays chan time.Duration
}

func (c busyTestClock) NewTimer(d time.Duration) sharedclock.Timer {
	timer := c.Clock.NewTimer(d)
	c.delays <- d
	return timer
}

func TestDeploymentBusyRetryUsesRuntimeClock(t *testing.T) {
	e := newEnv(t)
	e.m.sleep = nil
	var calls int
	e.fake.set(func(Request) (*Response, error) {
		calls++
		if calls == 1 {
			return reply(map[string]any{"success": false, "message": busyMessage}, 409), nil
		}
		return reply(map[string]any{"success": true}, 202), nil
	})
	fake := lifecycletest.NewClock(time.Unix(0, 0))
	delays := make(chan time.Duration, 4)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Clock: busyTestClock{fake, delays}})
	rt.Start(e.ctx)
	defer rt.Shutdown(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := e.m.DeploymentRequestModel(rt.BindRequest(e.ctx), e.agent, "POST", "deployments", nil, nil, nil)
		done <- err
	}()
	// This is the production busy delay, not an overridden sleep/timeout.
	if d := <-delays; d != 100*time.Millisecond {
		t.Fatalf("production busy interval=%s", d)
	}
	fake.Advance(100 * time.Millisecond)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls != 2 || fake.Pending() != 0 {
		t.Fatalf("calls=%d timers=%d", calls, fake.Pending())
	}
}
