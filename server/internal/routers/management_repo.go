package routers

import (
	"context"
	"encoding/json"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
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
	profile, err := oxidns.ProfileFromJSON(config)
	if err != nil {
		return nil, err
	}
	view, err := oxidns.ReadPolicies(profile.Settings(), profile)
	if err != nil {
		return nil, err
	}
	current, err := yamlx.ToJSON(view.Policies)
	if err != nil {
		return nil, err
	}
	if jsonEqual(rows, current) {
		return config, nil
	}
	node, err := yamlx.FromJSON(rows)
	if err != nil {
		return nil, err
	}
	_, notes, err := oxidns.PreparePolicyNotes(profile, node)
	if err != nil {
		return nil, err
	}
	settings, err := oxidns.SavePolicyRows(profile, profile.Settings(), node, base)
	if err != nil {
		return nil, err
	}
	replacement, err := yamlx.ToJSON(settings)
	if err != nil {
		return nil, err
	}
	var doc model.Fields
	if err = json.Unmarshal(config, &doc); err != nil {
		return nil, err
	}
	if err = doc.Set("oxidns", json.RawMessage(replacement)); err != nil {
		return nil, err
	}
	names, err := yamlx.ToJSON(notes)
	if err != nil {
		return nil, err
	}
	if err = doc.Set("device_notes", json.RawMessage(names)); err != nil {
		return nil, err
	}
	return doc.MarshalJSON()
}
