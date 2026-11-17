package discovery

import (
	"context"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/rules"
)

func TestProductionMatcherWiringStopsBeforeDrain(t *testing.T) {
	opts := matcherOptions()
	if opts.Spawner == nil || opts.Clock == nil || opts.MaxMatchers != 4 || opts.MaxHostResults != 1024 || opts.IdleSeconds != 300 {
		t.Fatal("production matcher wiring lost", opts)
	}
	clock := lifecycletest.NewClock(time.Now())
	reg := lifecycle.NewRegistry()
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: reg, Clock: clock})
	runner.Start(context.Background())
	defer runner.Shutdown(context.Background())
	opts.Clock = lifecycle.ClockFrom(runner.BindRequest(context.Background()))
	opts.Spawner = runner
	dir := t.TempDir()
	opts.RulesDir = func() string { return dir }
	opts.StoredLibrary = nil
	bounded := rules.NewMatcherCache(opts)
	reg.RegisterShutdown(matcherShutdown(bounded.Clear))
	bounded.LocalMatcher(jsonx.Obj("rule_configs", []any{}, "rule_library", []any{}), "default")
	if clock.Pending() != 1 || !bounded.CleanupScheduled() {
		t.Fatal("cleanup not owned")
	}
	before := clock.Now()
	done := make(chan struct{})
	go func() { runner.Shutdown(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("idle shutdown waited for housekeeping grace")
	}
	if !clock.Now().Equal(before) || clock.Pending() != 0 || bounded.CleanupScheduled() {
		t.Fatal("cleanup did not stop at T0")
	}
}
