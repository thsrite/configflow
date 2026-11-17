package discovery

import (
	"context"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
)

func TestMatcherUsesEachPeriodicActivationClock(t *testing.T) {
	clearMatcherCache()
	t.Cleanup(func() {
		matcherMu.Lock()
		defer matcherMu.Unlock()
		if matcherCache != nil {
			matcherCache.Clear()
		}
		matcherCache = nil
	})
	for _, year := range []int{2025, 2026} {
		source := lifecycletest.NewClock(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC))
		reg := lifecycle.NewRegistry()
		spec := autoPeriodic()
		activated := make(chan struct{})
		original := spec.OnStart
		spec.OnStart = func(ctx context.Context) { original(ctx); close(activated) }
		reg.RegisterPeriodic(spec)
		reg.RegisterShutdown(matcherShutdown(clearMatcherCache))
		runner := lifecycle.NewRuntime(lifecycle.Options{Registry: reg, Clock: source})
		runner.Start(context.Background())
		select {
		case <-activated:
		case <-time.After(5 * time.Second):
			t.Fatal("matcher activation missing")
		}
		owner := cache(runner.BindRequest(context.Background()))
		owner.LocalMatcher(jsonx.Obj("rule_configs", []any{}, "rule_library", []any{}), "default")
		deadline := time.Now().Add(5 * time.Second)
		for source.Pending() != 2 {
			if time.Now().After(deadline) {
				t.Fatal("cache timer retained another generation's Clock", year, source.Pending())
			}
			time.Sleep(time.Millisecond)
		}
		runner.Shutdown(context.Background())
		if source.Pending() != 0 || owner.CleanupScheduled() {
			t.Fatal("activation timer leaked")
		}
	}
}
