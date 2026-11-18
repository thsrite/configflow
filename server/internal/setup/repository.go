package setup

import (
	"context"
	"encoding/json"
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// The current repository still exposes legacy objects. These adapters contain
// only serialization; wizard state and business decisions use native records.
func sharedSnapshot(ctx context.Context, r *repo.Repository) (model.Shared, error) {
	var shared model.Shared
	value, err := r.GetShared(ctx)
	if err != nil {
		return shared, err
	}
	raw, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(raw, &shared)
	}
	if err != nil {
		return shared, fmt.Errorf("decode setup shared resources: %w", err)
	}
	return shared, nil
}

func updateShared(ctx context.Context, r *repo.Repository, update func(*model.Shared) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := r.UpdateSharedTransaction(ctx, func(held context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		raw, err := json.Marshal(current)
		if err != nil {
			return nil, err
		}
		var shared model.Shared
		if err := json.Unmarshal(raw, &shared); err != nil {
			return nil, err
		}
		if err := update(&shared); err != nil {
			return nil, err
		}
		raw, err = json.Marshal(shared)
		if err != nil {
			return nil, err
		}
		var next jsonx.Object
		if err := json.Unmarshal(raw, &next); err != nil {
			return nil, err
		}
		if err := held.Err(); err != nil {
			return nil, err
		}
		return &next, nil
	})
	return err
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
