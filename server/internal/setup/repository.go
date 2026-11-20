package setup

import (
	"context"
	"encoding/json"

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

// resourceEnabled retains the distinction between an absent legacy flag
// (enabled by default) and explicit null (disabled). Typed true/false takes the
// fast path; opaque members remain preserved by the shared model codec.
func resourceEnabled(item json.Marshaler, flag *bool) bool {
	if flag != nil {
		return *flag
	}
	raw, err := item.MarshalJSON()
	if err != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	value, present := fields["enabled"]
	return !present || string(value) != "null"
}
