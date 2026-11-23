package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

// managementTransaction is the legacy C03 boundary; the business callback gets
// native JSON and the held transaction context, including its cancellation.
func managementTransaction(ctx context.Context, db *repo.Repository, profile string, fn func(context.Context, json.RawMessage) (json.RawMessage, error)) error {
	_, err := db.UpdateProfile(ctx, profile, func(work context.Context, current *model.Profile) error {
		if err := work.Err(); err != nil {
			return err
		}
		raw, err := model.Marshal(current)
		if err != nil {
			return err
		}
		next, err := fn(work, raw)
		if err != nil {
			return err
		}
		var updated model.Profile
		if err = json.Unmarshal(next, &updated); err != nil {
			return err
		}
		if err = work.Err(); err != nil {
			return err
		}
		*current = updated
		return nil
	})
	return err
}
func saveManagementSettings(ctx context.Context, db *repo.Repository, profile string, old, next *Router) error {
	expected, err := spec.ReleaseRouterRecord(old)
	if err != nil {
		return err
	}
	var replacement json.RawMessage
	if next != nil {
		replacement, err = json.Marshal(next)
		if err != nil {
			return err
		}
	}
	return db.SaveRouterSettingsJSON(ctx, profile, expected, replacement)
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
	if device.JSONEqual(rows, current) {
		return config, nil
	}
	parsed, err := oxidns.ParsePolicies(rows)
	if err != nil {
		return nil, err
	}
	_, notes, err := oxidns.PreparePolicyNotes(profile, parsed)
	if err != nil {
		return nil, err
	}
	settings, err := oxidns.SavePolicyRows(profile, profile.Settings(), parsed, base)
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
	names, err := jsonresp.Encode(notes)
	if err != nil {
		return nil, err
	}
	if err = doc.Set("device_notes", json.RawMessage(names)); err != nil {
		return nil, err
	}
	return doc.MarshalJSON()
}
