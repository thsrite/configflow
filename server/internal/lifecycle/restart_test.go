package lifecycle

import (
	"context"
	"testing"
	"time"
)

func TestStartStopStartRunsTheLoopAgain(t *testing.T) {
	startedLoop := make(chan struct{}, 2)
	Loop("restart-regression", func(ctx context.Context) { startedLoop <- struct{}{}; <-ctx.Done() })
	t.Cleanup(func() { Stop(time.Second) })
	for generation := range 2 {
		Start(context.Background())
		select {
		case <-startedLoop:
		case <-time.After(time.Second):
			t.Fatalf("loop did not run for generation %d", generation+1)
		}
		Stop(time.Second)
	}
}
