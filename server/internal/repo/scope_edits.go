package repo

import (
	"context"

	"configflow/internal/ctxx"
	"configflow/internal/model"
)

// UpdateScopedConfig edits one resolved profile and invalidates only that
// profile's request snapshot and save baseline after a successful commit.
func (r *Repository) UpdateScopedConfig(ctx context.Context, profileID string, fn func(context.Context, *model.Config) error) error {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	id, err := r.ResolveProfileID(ctx, profileID)
	if err != nil {
		return err
	}
	if err := r.UpdateConfig(ctx, id, fn); err != nil {
		return err
	}
	if scope := ScopeOf(ctx); scope != nil {
		scope.mu.Lock()
		scope.bind(r)
		delete(scope.configs, id)
		scope.mu.Unlock()
	}
	return nil
}

// UpdateScopedSharedConfigs edits shared resources and selected resolved
// profiles, discarding request snapshots and baselines only after a successful
// commit.
func (r *Repository) UpdateScopedSharedConfigs(ctx context.Context, sharedEdit func(context.Context, *model.Shared) error, profileEdit func(context.Context, string, *model.Config, *model.Config) error, selector func(*model.Shared, []*model.Profile) ([]string, error)) error {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if selector == nil {
		id, err := r.ResolveProfileID(ctx, "")
		if err != nil {
			return err
		}
		selector = func(*model.Shared, []*model.Profile) ([]string, error) { return []string{id}, nil }
	}
	if err := r.UpdateSharedConfigs(ctx, sharedEdit, profileEdit, selector); err != nil {
		return err
	}
	ResetConfigContext(ctx)
	return nil
}

// UpdateConfig delegates to the context/default repository (transitional; removed in W11).
func UpdateConfig(ctx context.Context, profileID string, fn func(context.Context, *model.Config) error) error {
	r, err := GetRepository(ctx)
	if err != nil {
		return err
	}
	return r.UpdateScopedConfig(ctx, profileID, fn)
}

// UpdateSharedConfigs delegates to the context/default repository (transitional; removed in W11).
func UpdateSharedConfigs(ctx context.Context, sharedEdit func(context.Context, *model.Shared) error, profileEdit func(context.Context, string, *model.Config, *model.Config) error, selector func(*model.Shared, []*model.Profile) ([]string, error)) error {
	r, err := GetRepository(ctx)
	if err != nil {
		return err
	}
	return r.UpdateScopedSharedConfigs(ctx, sharedEdit, profileEdit, selector)
}
