package clients

import (
	"context"
	"encoding/json"

	"configflow/internal/model"
	"configflow/internal/repo"
)

// updateFixedProfile is the C03 transaction boundary. The business
// callback receives native JSON and the repository's held context, preserving
// unknown profile fields and cancellation at the actual commit point.
func updateFixedProfile(ctx context.Context, db *repo.Repository, profile string, update func(context.Context, json.RawMessage) (json.RawMessage, error)) error {
	_, err := db.UpdateProfile(ctx, profile, func(work context.Context, current *model.Profile) error {
		raw, err := json.Marshal(current)
		if err != nil {
			return err
		}
		next, err := update(work, raw)
		if err != nil {
			return err
		}
		var result model.Profile
		if err = json.Unmarshal(next, &result); err != nil {
			return err
		}
		*current = result
		return work.Err()
	})
	return err
}
