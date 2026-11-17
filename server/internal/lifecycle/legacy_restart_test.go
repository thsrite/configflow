package lifecycle

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLegacyRestartWaitsForOldLoop(t *testing.T) {
	reg := NewRegistry()
	gate := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(gate) }) }
	defer finish()
	entered := make(chan struct{}, 2)
	reg.addLegacy(loop{name: "slow-legacy", run: func(context.Context) {
		entered <- struct{}{}
		<-gate
	}})
	r := NewRuntime(Options{Registry: reg, Grace: time.Millisecond})
	t.Cleanup(func() { r.Shutdown(context.Background()) })
	await := func() {
		t.Helper()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("legacy loop did not start")
		}
	}
	r.Start(context.Background())
	await()
	r.Shutdown(context.Background())
	r.Start(context.Background())
	select {
	case <-entered:
		t.Fatal("legacy restart overlapped unfinished old loop")
	case <-time.After(20 * time.Millisecond):
	}
	finish()
	await()
	r.Shutdown(context.Background())
}
