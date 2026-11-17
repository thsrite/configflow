package lifecycle_test

import (
	"context"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

// Keep the legacy restart regression isolated from the default registry, so
// repeated test runs cannot retain callbacks from an earlier testing.T.
func TestStartStopStartRunsTheLoopAgain(t *testing.T) {
	reg := lifecycle.NewRegistry()
	startedLoop := make(chan struct{}, 2)
	reg.RegisterPeriodic(lifecycle.Periodic{Name: "restart-regression", Round: func(ctx context.Context) error {
		startedLoop <- struct{}{}
		<-ctx.Done()
		return nil
	}})
	r := setup(t, lifecycle.Options{Registry: reg, Grace: 10 * time.Millisecond})
	for generation := range 2 {
		r.Start(context.Background())
		select {
		case <-startedLoop:
		case <-time.After(time.Second):
			t.Fatalf("loop did not run for generation %d", generation+1)
		}
		r.Shutdown(context.Background())
	}
}
