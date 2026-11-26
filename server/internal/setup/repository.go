package setup

import (
	"context"

	"configflow/internal/model"
	"configflow/internal/repo"
)

// Shared reads and edits use the repository's sole typed transaction owner.
func sharedSnapshot(ctx context.Context, r *repo.Repository) (model.Shared, error) {
	shared, err := r.Shared(ctx)
	if err != nil {
		return model.Shared{}, err
	}
	return *shared, nil
}
func updateShared(ctx context.Context, r *repo.Repository, update func(*model.Shared) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.UpdateShared(ctx, func(tx context.Context, current *model.Shared) error {
		if err := tx.Err(); err != nil {
			return err
		}
		if err := update(current); err != nil {
			return err
		}
		return tx.Err()
	})
}

// resourceEnabled evaluates a stored resource switch with the shared JSON
// truthiness rule, defaulting to enabled when absent.
func resourceEnabled(flag *bool) bool {
	return model.Flag(flag, true)
}
