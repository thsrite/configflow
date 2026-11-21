package tasks

import (
	"context"

	"configflow/internal/model"
)

type snapshotCacheKey struct{}

type snapshotCache struct {
	agentsLoaded bool
	agents       []model.Agent
}

// WithSnapshotCache attaches a per-snapshot read cache to ctx so notification
// collection and task row builders share one Agent snapshot.
func WithSnapshotCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, snapshotCacheKey{}, &snapshotCache{})
}

// CachedAgents returns Agents stored in the current snapshot cache, if any.
// Callers must treat the returned slice as read-only.
func CachedAgents(ctx context.Context) ([]model.Agent, bool) {
	c, ok := ctx.Value(snapshotCacheKey{}).(*snapshotCache)
	if !ok || c == nil || !c.agentsLoaded {
		return nil, false
	}
	return c.agents, true
}

// CacheAgents stores a read-only Agent slice in the current snapshot cache.
func CacheAgents(ctx context.Context, list []model.Agent) {
	if c, ok := ctx.Value(snapshotCacheKey{}).(*snapshotCache); ok && c != nil {
		c.agents = list
		c.agentsLoaded = true
	}
}
