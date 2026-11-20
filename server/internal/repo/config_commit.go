package repo

import (
	"configflow/internal/model"
	"context"
)

// CommitConfig edits a resolved configuration and returns the exact committed
// profile and combined revision. Reads of shared resources remain in the same
// transaction; failed callbacks leave the stored document unchanged.
func (r *Repository) CommitConfig(ctx context.Context, profileID string, fn func(context.Context, *model.Config) error) (*model.Profile, int64, error) {
	return r.updateConfig(ctx, profileID, fn)
}
