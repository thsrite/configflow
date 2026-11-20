package setupflow

import (
	"configflow/internal/repo"
	"configflow/internal/routersplit"
	"context"
	"encoding/json"
)

// Wire installs the shared read-only C36 adapter. ProfileSessions runs before
// callers acquire repository write locks; bound Agents come from repo's snapshot.
func Wire() {
	routersplit.SetSessionReader(func(ctx context.Context, r *repo.Repository, profile string) ([]routersplit.Session, error) {
		sessions, err := ProfileSessions(ctx, r.DataDir(), profile)
		if err != nil {
			return nil, err
		}
		out := make([]routersplit.Session, 0, len(sessions))
		for _, session := range sessions {
			var value routersplit.Session
			if err := json.Unmarshal(session.Raw, &value); err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	})
}
