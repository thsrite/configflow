package lifecycle

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPeriodicRestartWaitsForOldRound(t *testing.T) {
	reg := NewRegistry()
	gate := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(gate) }) }
	defer finish()
	entered := make(chan struct{}, 2)
	reg.RegisterPeriodic(Periodic{Name: "slow-periodic", Interval: func(context.Context) time.Duration { return time.Millisecond }, Round: func(context.Context) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-gate
		return nil
	}})
	r := NewRuntime(Options{Registry: reg, Grace: time.Millisecond})
	t.Cleanup(func() { r.Shutdown(context.Background()) })
	await := func() {
		t.Helper()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("periodic round did not start")
		}
	}
	r.Start(context.Background())
	await()
	r.Shutdown(context.Background())
	r.Start(context.Background())
	select {
	case <-entered:
		t.Fatal("periodic restart overlapped unfinished old round")
	case <-time.After(20 * time.Millisecond):
	}
	finish()
	await()
	r.Shutdown(context.Background())
}
