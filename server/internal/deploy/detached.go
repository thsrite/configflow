package deploy

import (
	"configflow/internal/lifecycle"
	"context"
	"fmt"
)

type publishJobKey struct{}

// RunDetached tracks a publication through shutdown while dropping browser cancellation.
// Nested preparation/publish calls reuse their owner's job and cancellation parent.
func RunDetached(ctx context.Context, name string, fn func(context.Context) error) error {
	if ctx.Value(publishJobKey{}) != nil || lifecycle.Tracked(ctx) {
		// This is an owner's task, not a browser request. Preserve explicit
		// owner cancellation (for example Checker.Close) as well as shutdown.
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(ctx)
	}
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	err := lifecycle.SpawnerFrom(ctx).Spawn(ctx, name, func(job context.Context) {
		// lifecycle recovers too, but this waiter must always be released.
		defer func() {
			if p := recover(); p != nil {
				logger.Error("Agent deployment job panicked")
				done <- fmt.Errorf("deployment job: %v", p)
			}
		}()
		done <- fn(context.WithValue(job, publishJobKey{}, true))
	})
	if err != nil {
		return err
	}
	return <-done
}
