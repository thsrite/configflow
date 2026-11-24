package discovery

import (
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/rules"
	"context"
	"path/filepath"
)

func (s *Service) activateMatcherCache(ctx context.Context) {
	s.matcherMu.Lock()
	defer s.matcherMu.Unlock()
	if s.matcherCache != nil {
		s.matcherCache.Clear()
	}
	opts := s.matcherOptions(ctx)
	opts.Clock = lifecycle.ClockFrom(ctx)
	s.matcherCache = rules.NewCache(opts)
}

func (s *Service) cache(ctx context.Context) *rules.Cache {
	s.matcherMu.Lock()
	defer s.matcherMu.Unlock()
	if s.matcherCache == nil {
		opts := s.matcherOptions(ctx)
		opts.Clock = lifecycle.ClockFrom(ctx)
		s.matcherCache = rules.NewCache(opts)
	}
	return s.matcherCache
}

func (s *Service) clearMatcherCache() {
	s.matcherMu.Lock()
	defer s.matcherMu.Unlock()
	if s.matcherCache != nil {
		s.matcherCache.Clear()
	}
}

func matcherShutdown(clear func()) lifecycle.ShutdownHook {
	return lifecycle.ShutdownHook{Name: "domain-discovery-cache", Phase: lifecycle.PhaseStopSignal, Run: func(context.Context) { clear() }}
}

// Cleanup is housekeeping: no new timer job is needed once shutdown starts.
// The hook clears existing waits at T0, including jobs admitted just before it.
type matcherSpawner struct{ inner lockx.Spawner }

func (m matcherSpawner) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	select {
	case <-lifecycle.Stopping(ctx):
		return lifecycle.ErrStopping
	default:
	}
	return m.inner.Spawn(ctx, name, fn)
}

func (s *Service) matcherOptions(ctx context.Context) rules.CacheOptions {
	options := s.nativeMatcherOptions()
	options.Clock = lifecycle.ClockFrom(ctx)
	options.Spawner = matcherSpawner{inner: s.spawner(ctx)}
	options.RulesDir = func() string {
		if s != nil && s.Repo != nil {
			return filepath.Join(s.Repo.DataDir(), "shared", "rules")
		}
		return ""
	}
	options.MaxMatchers, options.MaxHostResults, options.IdleSeconds = 4, 1024, 300
	return options
}
