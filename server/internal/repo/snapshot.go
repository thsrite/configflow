package repo

import (
	"context"
	"errors"
	"sync/atomic"
)

// ErrReadOnlySnapshot rejects writes and resource-lock acquisition inside a
// locked configuration snapshot. Its callback may acquire a leaf CAS lock.
var ErrReadOnlySnapshot = errors.New("configuration snapshot is read-only")

type readSnapshotKey struct{}
type readSnapshot struct {
	repository *Repository
	entry      storedEntry
	active     atomic.Bool
}

func readSnapshotActive(ctx context.Context) bool {
	s, _ := ctx.Value(readSnapshotKey{}).(*readSnapshot)
	return s != nil && s.active.Load()
}

func (r *Repository) lockedSnapshot(ctx context.Context) *readSnapshot {
	s, _ := ctx.Value(readSnapshotKey{}).(*readSnapshot)
	if s != nil && s.repository == r && s.active.Load() {
		return s
	}
	return nil
}

// WithReadSnapshot runs fn with a coherent read-only configuration generation
// while L3 excludes configuration writers. Pass held to repository readers and
// only then acquire the leaf CAS lock; perform no network or device I/O here.
// Pure readers retain their context cancellation; write owners detach first.
func (r *Repository) WithReadSnapshot(ctx context.Context, fn func(held context.Context) error) error {
	if r.lockedSnapshot(ctx) != nil {
		return fn(ctx)
	}
	return withLock(ctx, r.lockFile, func(held context.Context) error {
		entry, err := r.readEntry(held, true)
		if err != nil {
			return err
		}
		s := &readSnapshot{repository: r, entry: entry}
		s.active.Store(true)
		defer s.active.Store(false)
		return fn(context.WithValue(held, readSnapshotKey{}, s))
	})
}
