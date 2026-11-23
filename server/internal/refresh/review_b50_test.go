package refresh

import (
	"context"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/model"
)

// HR review of batch-50: scheduled-refresh sleeps until the next due
// resource, never longer than MaxPause, and a configuration commit (a new
// interval or source) wakes it to recompute.
func TestReviewB50RefreshPacesByNextDueAndWakesOnCommits(t *testing.T) {
	f := setup(t, 300)
	f.updateShared(func(shared *model.Shared) {
		disabled := false
		shared.RuleLibrary[0].Enabled = &disabled
	})
	f.download(proxiesYAML(t), "direct_yaml", nil)
	round(f.ctx)
	if entry := f.status(SourceTaskKey("s")); mapInt(entry, "run_count") != 1 {
		t.Fatalf("the due subscription did not run: %v", entry)
	}
	if pause := refreshPause(f.ctx); pause < time.Second || pause > 300*time.Second {
		t.Fatalf("pause %v, want the time until the next due run (≤ 300 s)", pause)
	}
	f.updateShared(func(shared *model.Shared) {
		interval := model.Int(7200)
		shared.Subscriptions[0].Interval = &interval
	})
	round(f.ctx)
	if pause := refreshPause(f.ctx); pause != MaxPause {
		t.Fatalf("pause %v after the interval became 2 h, want MaxPause", pause)
	}

	// In the loop: after a round the loop rests (MaxPause); a configuration
	// commit runs the next round at once, without the clock moving.
	clock := lifecycletest.NewClock(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	reg := lifecycle.NewRegistry()
	spec := periodic()
	original := spec.Round
	rounds := make(chan struct{}, 4)
	spec.Round = func(ctx context.Context) error {
		err := original(ctx)
		rounds <- struct{}{}
		return err
	}
	reg.RegisterPeriodic(spec)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: reg, Clock: clock})
	runtime.Start(f.ctx)
	defer runtime.Shutdown(context.Background())
	awaitRefresh(t, func() bool { return clock.Pending() == 1 })
	clock.Advance(FirstDelaySeconds * time.Second)
	<-rounds
	awaitRefresh(t, func() bool { return clock.Pending() == 1 })
	f.updateShared(func(shared *model.Shared) {
		interval := model.Int(3600)
		shared.Subscriptions[0].Interval = &interval
	})
	select {
	case <-rounds:
	case <-time.After(3 * time.Second):
		t.Fatal("a configuration commit did not wake scheduled-refresh")
	}
}

func TestO17FollowupImmediateDueResourceRetriesWithin30Seconds(t *testing.T) {
	f := setup(t, 3600)
	f.updateShared(func(shared *model.Shared) {
		disabled := false
		shared.RuleLibrary[0].Enabled = &disabled
	})
	// Hold the subscription's running lock during the round so it stays
	// immediately due (no receipt, no cache baseline) when pace computes
	// the next sleep.
	_, release, ok := running.TryLock(f.ctx, SourceTaskKey("s"))
	if !ok {
		t.Fatal("failed to claim running lock")
	}
	defer release()

	round(f.ctx)
	if pause := refreshPause(f.ctx); pause > TickSeconds*time.Second || pause < time.Second {
		t.Fatalf("pause %v with an immediately due resource, want <= %v", pause, TickSeconds*time.Second)
	}
}
