package routers

import (
	"configflow/internal/jsonx"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"context"
	"encoding/json"
)

// managementTransaction is the legacy C03 boundary; the business callback gets
// native JSON and the held transaction context, including its cancellation.
func managementTransaction(ctx context.Context, db *repo.Repository, profile string, fn func(context.Context, json.RawMessage) (json.RawMessage, error)) error {
	_, _, err := db.UpdateProfileTransaction(ctx, profile, func(work context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		raw, err := json.Marshal(current)
		if err != nil {
			return nil, err
		}
		next, err := fn(work, raw)
		if err != nil {
			return nil, err
		}
		var out jsonx.Object
		if err = json.Unmarshal(next, &out); err != nil {
			return nil, err
		}
		return &out, work.Err()
	})
	return err
}
func saveManagementSettings(ctx context.Context, db *repo.Repository, profile string, old, next *Router) error {
	raw, err := releaseRouterRecord(old)
	if err != nil {
		return err
	}
	var expected jsonx.Object
	if err = json.Unmarshal(raw, &expected); err != nil {
		return err
	}
	var replacement *jsonx.Object
	if next != nil {
		encoded, err := json.Marshal(next)
		if err != nil {
			return err
		}
		replacement = jsonx.NewObject()
		if err = json.Unmarshal(encoded, replacement); err != nil {
			return err
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return db.SaveRouterSettings(ctx, profile, &expected, replacement)
}

// saveWorkspacePolicies adapts C22/C31's existing profile mutation boundary.
func saveWorkspacePolicies(config json.RawMessage, rows json.RawMessage, base string) (json.RawMessage, error) {
	if rows == nil {
		return config, nil
	}
	var doc jsonx.Object
	if err := json.Unmarshal(config, &doc); err != nil {
		return nil, err
	}
	p := oxidns.NewProfile(&doc)
	view, err := oxidns.ReadPolicies(p.Settings(), p)
	if err != nil {
		return nil, err
	}
	if jsonEqual(rows, oxidns.ToJSON(view.Policies)) {
		return config, nil
	}
	node, err := oxidns.FromJSON(rows)
	if err != nil {
		return nil, err
	}
	settings, err := oxidns.SavePolicyRows(p, p.Settings(), node, base)
	if err != nil {
		return nil, err
	}
	doc.Set("oxidns", oxidns.ToJSONX(settings))
	return json.Marshal(&doc)
}
