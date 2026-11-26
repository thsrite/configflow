package lifecycle_test

import (
	"context"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

// TestR14ReviewShutdownReachesJobAndDetachTogether is the root cause of the
// intermittent R13 "shutdown lost cancellation": a job's own context learned
// about shutdown through an asynchronous AfterFunc hop, while Detach(job)
// observed the generation directly. An owner whose detached leaf write had
// just failed with context.Canceled could still read job.Err() == nil and
// report success (or reach its next commit check) after shutdown.
func TestR14ReviewShutdownReachesJobAndDetachTogether(t *testing.T) {
	for _, kind := range []string{"spawn", "round"} {
		t.Run(kind, func(t *testing.T) {
			lagged := 0
			for range 200 {
				registry := lifecycle.NewRegistry()
				observed := make(chan error, 1)
				body := func(work context.Context) {
					<-lifecycle.Detach(work).Done()
					observed <- work.Err()
				}
				ready := make(chan struct{})
				if kind == "round" {
					registry.RegisterPeriodic(lifecycle.Periodic{Name: "r14-review-round", Start: lifecycle.AtBoot, FirstDelay: time.Hour, Interval: func(context.Context) time.Duration { return time.Hour }, Round: func(work context.Context) error {
						close(ready)
						body(work)
						return nil
					}})
				}
				rt := lifecycle.NewRuntime(lifecycle.Options{Registry: registry, Mode: lifecycle.ModeManual, Grace: 50 * time.Millisecond})
				rt.Start(t.Context())
				if kind == "spawn" {
					if err := rt.Spawn(t.Context(), "r14-review-job", func(work context.Context) {
						close(ready)
						body(work)
					}); err != nil {
						t.Fatal(err)
					}
				} else {
					go func() { _ = rt.RunRound(t.Context(), "r14-review-round") }()
				}
				<-ready
				rt.Shutdown(t.Context())
				if err := <-observed; err == nil {
					lagged++
				}
			}
			if lagged > 0 {
				t.Fatalf("job context still live after its detached child saw shutdown in %d/200 runs", lagged)
			}
		})
	}
}
