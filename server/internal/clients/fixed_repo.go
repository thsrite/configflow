package clients

import (
	"context"
	"encoding/json"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// updateFixedProfile is the legacy C03 transaction boundary. The business
// callback receives native JSON and the repository's held context, preserving
// unknown profile fields and cancellation at the actual commit point.
func updateFixedProfile(ctx context.Context, db *repo.Repository, profile string, update func(context.Context, json.RawMessage) (json.RawMessage, error)) error {
	_, _, err := db.UpdateProfileTransaction(ctx, profile, func(work context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		raw, err := json.Marshal(current)
		if err != nil {
			return nil, err
		}
		next, err := update(work, raw)
		if err != nil {
			return nil, err
		}
		var result jsonx.Object
		if err = json.Unmarshal(next, &result); err != nil {
			return nil, err
		}
		return &result, work.Err()
	})
	return err
}
