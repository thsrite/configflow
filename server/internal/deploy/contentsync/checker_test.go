package contentsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/repo"
)

func TestProductionTTLsAndNoQueue(t *testing.T) {
	f := newFixture(t, nil)
	for i := 0; i < 20; i++ {
		requireStatus(t, f.status(), "checking", "")
	}
	if f.spawn.count() != 1 || f.calls != 0 {
		t.Fatal("poll enqueued preparation")
	}
	f.spawn.run(t)
	first := f.status()
	requireStatus(t, first, "synced", "")
	if !strings.HasSuffix(first.CheckedAt, "Z") {
		t.Fatal(first)
	}
	f.clock.Advance(899 * time.Second)
	requireStatus(t, f.status(), "synced", "")
	f.clock.Advance(time.Second)
	requireStatus(t, f.status(), "checking", "")
	if f.spawn.count() != 1 || f.calls != 1 {
		t.Fatal("success TTL")
	}
	f.spawn.run(t)
	// The default two slots never leave jobs waiting in an unbounded queue.
	f.c.Invalidate()
	for i := 0; i < 10; i++ {
		f.agent["id"] = fmt.Sprintf("agent-%d", i)
		f.status()
	}
	if f.spawn.count() != Workers || len(f.c.slots) != Workers {
		t.Fatal("unbounded workers")
	}
}

func TestInputProjectionAndPreconditions(t *testing.T) {
	for _, tc := range []struct {
		name           string
		change         func(*fixture)
		status, reason string
		newJob         bool
	}{
		{"runtime metadata", func(f *fixture) {
			f.agent["last_heartbeat"] = "later"
			f.agent["version"] = "next"
			f.cfg["_revision"] = 9
			f.cfg["name"] = "renamed"
			f.cfg["backup"] = nil
		}, "synced", "", false},
		{"profile_name participates", func(f *fixture) { f.cfg["profile_name"] = "renamed" }, "checking", "", true},
		{"nested metadata participates", func(f *fixture) { f.cfg["plugin"] = map[string]any{"created_at": "later"} }, "checking", "", true},
		{"rule bytes", func(f *fixture) { f.cfg["test_rule"] = "DOMAIN,changed.example.test\n" }, "checking", "", true},
		{"pending latest", func(f *fixture) { f.agent["latest_deployment"] = map[string]any{"status": "ready"} }, "unknown", "deployment_pending", false},
		{"pending history", func(f *fixture) {
			f.agent["deployments"] = map[string]any{"next": map[string]any{"status": "uploading"}}
		}, "unknown", "deployment_pending", false},
		{"baseline missing", func(f *fixture) { delete(f.agent, "applied_content") }, "unknown", "baseline_missing", false},
		{"bad fingerprint", func(f *fixture) { f.agent["applied_content"].(map[string]any)["fingerprint"] = "v1:bad" }, "unknown", "baseline_invalid", false},
		{"profile mismatch", func(f *fixture) { f.agent["profile_id"] = "other" }, "unknown", "profile_mismatch", false},
		{"source profile mismatch", func(f *fixture) { f.cfg["profile_id"] = "other" }, "unknown", "profile_mismatch", false},
		{"service mismatch", func(f *fixture) { f.agent["service_type"] = "oxidns" }, "unknown", "service_mismatch", false},
		{"version mismatch", func(f *fixture) { f.agent["config_version"] = "next" }, "unknown", "version_mismatch", false},
		{"deployment id missing", func(f *fixture) { delete(f.agent["applied_content"].(map[string]any), "deployment_id") }, "unknown", "baseline_invalid", false},
		{"path missing", func(f *fixture) { delete(f.agent["applied_content"].(map[string]any), "config_path") }, "unknown", "baseline_invalid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, nil)
			f.status()
			f.spawn.run(t)
			tc.change(f)
			r := f.status()
			requireStatus(t, r, tc.status, tc.reason)
			if (f.spawn.count() > 0) != tc.newJob {
				t.Fatal("wrong job reservation")
			}
		})
	}
	for _, status := range []string{"failed", "rolled_back", "succeeded"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, nil)
			f.agent["latest_deployment"] = map[string]any{"status": status}
			requireStatus(t, f.status(), "checking", "")
		})
	}
	t.Run("never pushed", func(t *testing.T) {
		f := newFixture(t, nil)
		delete(f.agent, "applied_content")
		delete(f.agent, "config_version")
		requireStatus(t, f.status(), "never-pushed", "")
		if f.spawn.count() != 0 {
			t.Fatal("scheduled missing baseline")
		}
	})
}

func TestOldWorkersInvalidationAndReturnToOriginal(t *testing.T) {
	for _, kind := range []string{"changed input", "new baseline", "A to B to A", "invalidation", "pending", "close"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, nil)
			f.status()
			switch kind {
			case "changed input":
				f.cfg["test_rule"] = "different"
				f.status()
			case "new baseline":
				f.agent["applied_content"].(map[string]any)["deployment_id"] = "new"
				f.status()
			case "A to B to A":
				f.cfg["test_rule"] = "different"
				f.status()
				f.cfg["test_rule"] = "DOMAIN,example.test\n"
				f.status()
			case "invalidation":
				f.c.Invalidate()
			case "pending":
				f.agent["latest_deployment"] = map[string]any{"status": "preparing"}
				f.status()
			case "close":
				f.c.Close()
			}
			f.spawn.run(t)
			if len(f.c.running) != 0 || len(f.c.slots) != 0 {
				t.Fatal("reservation leak")
			}
			if kind == "close" {
				requireStatus(t, f.status(), "unknown", "check_failed")
				return
			}
			if kind == "pending" {
				delete(f.agent, "latest_deployment")
			}
			requireStatus(t, f.status(), "checking", "")
			if f.spawn.count() != 1 {
				t.Fatal("old worker was cached")
			}
		})
	}
}

func TestErrorTTLRejectPanicAndCancellation(t *testing.T) {
	for _, kind := range []string{"prepare error", "panic", "spawn rejection"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, nil)
			if kind == "spawn rejection" {
				f.spawn.reject = true
			} else {
				f.prepare = func(context.Context, deploy.Target, *deploy.Source, string, string) (*deploy.Bundle, error) {
					if kind == "panic" {
						panic("synthetic private token")
					}
					return nil, errors.New("synthetic private token")
				}
			}
			initial := f.status()
			if kind != "spawn rejection" {
				f.spawn.run(t)
			} else {
				requireStatus(t, initial, "unknown", "check_failed")
			}
			r := f.status()
			requireStatus(t, r, "unknown", "check_failed")
			raw, _ := json.Marshal(r)
			if strings.Contains(string(raw), "private") {
				t.Fatal(string(raw))
			}
			if len(f.c.slots) != 0 || len(f.c.running) != 0 {
				t.Fatal("leaked slot")
			}
			f.clock.Advance(29 * time.Second)
			requireStatus(t, f.status(), "unknown", "check_failed")
			f.clock.Advance(time.Second)
			requireStatus(t, f.status(), "checking", "")
		})
	}
	t.Run("request disconnect", func(t *testing.T) {
		f := newFixture(t, nil)
		ctx, cancel := context.WithCancel(context.Background())
		f.ctx = ctx
		f.status()
		cancel()
		f.spawn.run(t)
		requireStatus(t, f.status(), "synced", "")
	})
}

func TestSynchronousCompareCacheAndGeneration(t *testing.T) {
	f := newFixture(t, nil)
	r := f.c.CompareNow(f.ctx, f.target(), f.source(), "")
	requireStatus(t, r, "synced", "")
	if f.calls != 1 || f.spawn.count() != 0 {
		t.Fatal("did not run synchronously")
	}
	requireStatus(t, f.c.Status(f.ctx, f.target(), f.source(), ""), "synced", "")
	f.clock.Advance(SuccessTTL)
	base := f.prepare
	f.prepare = func(ctx context.Context, target deploy.Target, s *deploy.Source, url, id string) (*deploy.Bundle, error) {
		f.c.Invalidate()
		return base(ctx, target, s, url, id)
	}
	requireStatus(t, f.c.CompareNow(f.ctx, f.target(), f.source(), ""), "synced", "")
	if e := f.c.entries[f.agent["id"].(string)]; e != nil && e.result != nil {
		t.Fatal("cached across generation")
	}
}

func TestConcurrentPollsAndIsolatedScopes(t *testing.T) {
	f := newFixture(t, nil)
	target, source := f.target(), f.source()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f.c.Status(f.ctx, target, source, "") }()
	}
	wg.Wait()
	if f.spawn.count() != 1 {
		t.Fatal("duplicate preparation")
	}
	f.spawn.run(t)
	other := newFixture(t, nil)
	other.cfg["test_rule"] = "changed"
	other.status()
	other.spawn.run(t)
	requireStatus(t, other.status(), "changed", "")
	requireStatus(t, f.c.Status(f.ctx, target, source, ""), "synced", "")
}

func TestClockFromBoundRequestAndFreshPreparationScope(t *testing.T) {
	clock := lifecycletest.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	runtime := lifecycle.NewRuntime(lifecycle.Options{Clock: clock, Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	f := newFixture(t, func(o *Options) { o.Clock = nil })
	f.ctx = repo.WithScope(runtime.BindRequest(context.Background()))
	callerScope := repo.ScopeOf(f.ctx)
	f.prepare = func(ctx context.Context, _ deploy.Target, _ *deploy.Source, _, _ string) (*deploy.Bundle, error) {
		if lifecycle.ClockFrom(ctx) != clock {
			t.Error("lost runtime clock")
		}
		if repo.ScopeOf(ctx) == nil || repo.ScopeOf(ctx) == callerScope {
			t.Error("reused request scope")
		}
		return f.baseline, nil
	}
	f.status()
	f.spawn.run(t)
	clock.Advance(SuccessTTL - time.Nanosecond)
	requireStatus(t, f.status(), "synced", "")
	clock.Advance(time.Nanosecond)
	requireStatus(t, f.status(), "checking", "")
	f.c.Close()
	runtime.Shutdown(context.Background())
}

func TestCompareNowReusesListAndDoesNotCachePreparationErrors(t *testing.T) {
	f := newFixture(t, nil)
	f.status()
	f.spawn.run(t)
	requireStatus(t, f.c.CompareNow(f.ctx, f.target(), f.source(), "https://config.example.test"), "synced", "")
	if f.calls != 1 {
		t.Fatal("fresh list status not reused")
	}
	f.clock.Advance(SuccessTTL)
	f.prepare = func(context.Context, deploy.Target, *deploy.Source, string, string) (*deploy.Bundle, error) {
		return nil, errors.New("synthetic secret")
	}
	requireStatus(t, f.c.CompareNow(f.ctx, f.target(), f.source(), "https://config.example.test"), "unknown", "check_failed")
	if e := f.c.entries["agent-test"]; e != nil && e.result != nil && f.clock.Now().Before(e.expires) {
		t.Fatal("synchronous failure cached")
	}
}

func TestRuntimeShutdownCancelsAdmittedJobAndReleasesSlot(t *testing.T) {
	clock := lifecycletest.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	runtime := lifecycle.NewRuntime(lifecycle.Options{Clock: clock, Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	f := newFixture(t, func(o *Options) { o.Clock = clock; o.Spawner = runtime })
	started := make(chan context.Context, 1)
	f.prepare = func(ctx context.Context, _ deploy.Target, _ *deploy.Source, _, _ string) (*deploy.Bundle, error) {
		started <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f.ctx = runtime.BindRequest(context.Background())
	requireStatus(t, f.status(), "checking", "")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not start")
	}
	done := make(chan struct{})
	go func() { runtime.Shutdown(context.Background()); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for clock.Pending() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("shutdown timers not armed")
		}
		time.Sleep(time.Millisecond)
	}
	// Default grace is eight seconds: owner jobs are cancelled at grace - 2s.
	clock.Advance(6 * time.Second)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not release the admitted job")
	}
	f.c.mu.Lock()
	defer f.c.mu.Unlock()
	if len(f.c.slots) != 0 || len(f.c.running) != 0 {
		t.Fatal("shutdown leaked reservation")
	}
}
