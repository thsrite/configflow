package discovery

import (
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"context"
	"path/filepath"
	"sync"
)

var matcherMu sync.Mutex
var matcherCache *rules.Cache

func activateMatcherCache(ctx context.Context) {
	matcherMu.Lock()
	defer matcherMu.Unlock()
	if matcherCache != nil {
		matcherCache.Clear()
	}
	opts := matcherOptions()
	opts.Clock = lifecycle.ClockFrom(ctx)
	matcherCache = rules.NewCache(opts)
}

func cache(ctx context.Context) *rules.Cache {
	matcherMu.Lock()
	defer matcherMu.Unlock()
	if matcherCache == nil {
		opts := matcherOptions()
		opts.Clock = lifecycle.ClockFrom(ctx)
		matcherCache = rules.NewCache(opts)
	}
	return matcherCache
}

func clearMatcherCache() {
	matcherMu.Lock()
	defer matcherMu.Unlock()
	if matcherCache != nil {
		matcherCache.Clear()
	}
}

func matcherShutdown(clear func()) lifecycle.ShutdownHook {
	return lifecycle.ShutdownHook{Name: "domain-discovery-cache", Phase: lifecycle.PhaseStopSignal, Run: func(context.Context) { clear() }}
}

// Cleanup is housekeeping: no new timer job is needed once shutdown starts.
// The hook clears existing waits at T0, including jobs admitted just before it.
type matcherSpawner struct{}

func (matcherSpawner) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	select {
	case <-lifecycle.Stopping(ctx):
		return lifecycle.ErrStopping
	default:
	}
	return lifecycle.Spawn(ctx, name, fn)
}

func matcherOptions() rules.CacheOptions {
	options := nativeMatcherOptions()
	options.Clock = lifecycle.ClockFrom(context.Background())
	options.Spawner = matcherSpawner{}
	options.RulesDir = func() string { return filepath.Join(repo.DataDir(), "shared", "rules") }
	options.MaxMatchers, options.MaxHostResults, options.IdleSeconds = 4, 1024, 300
	return options
}
