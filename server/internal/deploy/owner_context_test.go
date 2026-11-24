package deploy

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestNestedOwnerKeepsTrackedCancellation(t *testing.T) {
	for _, before := range []bool{true, false} {
		name := "during"
		if before {
			name = "before-start"
		}
		t.Run(name, func(t *testing.T) {
			ctx := testContext(t)
			rt := startGlobalPublishRuntime(t, ctx)
			cancellation := make(chan context.CancelFunc, 1)
			entered := make(chan struct{})
			done := make(chan error, 1)
			var called atomic.Bool
			if err := rt.Spawn(rt.BindRequest(ctx), "owner-cancellation-test", func(job context.Context) {
				owner, cancel := context.WithCancel(job)
				defer cancel()
				cancellation <- cancel
				if before {
					cancel()
				}
				done <- RunDetached(owner, "nested-cancellation-proof", func(work context.Context) error {
					called.Store(true)
					close(entered)
					if before {
						return nil
					}
					<-work.Done()
					return work.Err()
				})
			}); err != nil {
				t.Fatal(err)
			}
			cancel := <-cancellation
			if !before {
				<-entered
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) || before && called.Load() {
					t.Fatal("nested owner discarded tracked cancellation", err, called.Load())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("tracked cancellation did not reach nested owner")
			}
		})
	}
}
