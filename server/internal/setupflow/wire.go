package setupflow

import (
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
	"context"
)

// Wire installs the shared read-only C36 adapter. ProfileSessions runs before
// callers acquire repository write locks; bound Agents come from repo's snapshot.
func Wire() {
	routersplit.SetProfileSessions(func(ctx context.Context, r *repo.Repository, profile string) ([]*jsonx.Object, error) {
		sessions, err := ProfileSessions(ctx, r.DataDir(), profile)
		if err != nil {
			return nil, err
		}
		out := make([]*jsonx.Object, 0, len(sessions))
		for _, session := range sessions {
			v, err := jsonx.Parse(session.Raw)
			if err != nil {
				return nil, err
			}
			out = append(out, v.(*jsonx.Object))
		}
		return out, nil
	})
}
