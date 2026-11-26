package rulelibrary

import (
	"context"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func cachePath(r *repo.Repository, ctx context.Context, item model.RuleLibraryItem) (string, error) {
	return (&Service{repo: r}).cachePath(ctx, item)
}

func editShared(r *repo.Repository, ctx context.Context, fn func(context.Context, *model.Shared) error) error {
	return (&Service{repo: r}).editShared(ctx, fn)
}

func editSystem(r *repo.Repository, ctx context.Context, fn func(*model.System) error) error {
	return (&Service{repo: r}).editSystem(ctx, fn)
}

func forget(r *repo.Repository, ctx context.Context, id string) error {
	return (&Service{repo: r}).forget(ctx, id)
}

func source(r *repo.Repository, ctx context.Context, id string) (model.RuleLibraryItem, error) {
	return (&Service{repo: r}).source(ctx, id)
}
