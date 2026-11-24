package rulelibrary

import (
	"context"

	"configflow/internal/model"
)

func cachePath(ctx context.Context, item model.RuleLibraryItem) (string, error) {
	return (&Service{}).cachePath(ctx, item)
}

func editShared(ctx context.Context, fn func(context.Context, *model.Shared) error) error {
	return (&Service{}).editShared(ctx, fn)
}

func editSystem(ctx context.Context, fn func(*model.System) error) error {
	return (&Service{}).editSystem(ctx, fn)
}

func forget(ctx context.Context, id string) error {
	return (&Service{}).forget(ctx, id)
}

func source(ctx context.Context, id string) (model.RuleLibraryItem, error) {
	return (&Service{}).source(ctx, id)
}
