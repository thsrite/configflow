package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"

	"configflow/internal/ctxx"
	"configflow/internal/model"
)

// changedMembers stores only a callback's delta, leaving unchanged hydrated
// sources and opaque stored fragments with their original owner and bytes.
func changedMembers(keys []string, before, after any, set func(string, json.RawMessage) error) error {
	old, err := recordMembers(before)
	if err != nil {
		return err
	}
	next, err := recordMembers(after)
	if err != nil {
		return err
	}
	for _, key := range keys {
		raw, present := next.Get(key)
		if !present {
			continue
		}
		previous, had := old.Get(key)
		if had && bytes.Equal(previous, raw) {
			continue
		}
		if err := set(key, raw); err != nil {
			return err
		}
	}
	return nil
}

// UpdateConfig edits the resolved, hydrated configuration under one repository
// transaction. The view carries the OxiDNS Agent projection (see
// ConfigWithOxiDNSAgents) for rule-DNS rebuilds, never Agent histories. Only changed profile settings are stored; metadata and shared
// resources remain owned by their respective write APIs.
func (r *Repository) UpdateConfig(ctx context.Context, profileID string, fn func(context.Context, *model.Config) error) error {
	_, _, err := r.updateConfig(ctx, profileID, fn)
	return err
}

func (r *Repository) updateConfig(ctx context.Context, profileID string, fn func(context.Context, *model.Config) error) (*model.Profile, int64, error) {
	ctx = ctxx.Detach(ctx)
	if fn == nil {
		return nil, 0, validationError("Updater must be callable")
	}
	var saved *model.Profile
	var revision int64
	err := r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		var err error
		saved, err = updateProfileIn(document, profileID, func(profile *model.Profile) error {
			config, err := resolveConfig(document, profileID, nil, agentCopyOxiDNS, false)
			if err != nil {
				return err
			}
			raw, err := model.Marshal(config)
			if err != nil {
				return err
			}
			before, err := model.DecodeConfig(raw)
			if err != nil {
				return err
			}
			if err := fn(ctx, config); err != nil {
				return err
			}
			return changedMembers(ProfileFields, before, config, profile.SetJSON)
		})
		if err == nil {
			revision = int64(saved.Revision) + int64(document.Shared.Revision)
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return saved, revision, nil
}

// UpdateSharedConfigs edits shared resources and selected resolved profiles in
// one transaction. before is resolved against the old shared catalog, after
// against the edited catalog. Selection preserves order and suppresses duplicates.
func (r *Repository) UpdateSharedConfigs(ctx context.Context, sharedEdit func(context.Context, *model.Shared) error, profileEdit func(context.Context, string, *model.Config, *model.Config) error, selector func(*model.Shared, []*model.Profile) ([]string, error)) error {
	_, _, err := r.updateSharedConfigs(ctx, sharedEdit, profileEdit, selector)
	return err
}

func (r *Repository) updateSharedConfigs(ctx context.Context, sharedEdit func(context.Context, *model.Shared) error, profileEdit func(context.Context, string, *model.Config, *model.Config) error, selector func(*model.Shared, []*model.Profile) ([]string, error)) (*model.Shared, []*model.Profile, error) {
	ctx = ctxx.Detach(ctx)
	if sharedEdit == nil || profileEdit == nil || selector == nil {
		return nil, nil, validationError("Updater must be callable")
	}
	var savedShared *model.Shared
	var savedProfiles []*model.Profile
	err := r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		shared, err := copyRecord(&document.Shared)
		if err != nil {
			return err
		}
		profiles := make([]*model.Profile, len(document.Profiles))
		for i, profile := range document.Profiles {
			profiles[i], err = copyRecord(profile)
			if err != nil {
				return err
			}
		}
		ids, err := selector(shared, profiles)
		if err != nil {
			return err
		}
		selected := []string{}
		for _, id := range ids {
			if !slices.Contains(selected, id) {
				selected = append(selected, id)
			}
		}
		before := map[string]*model.Config{}
		for _, id := range selected {
			config, err := resolveConfig(document, id, nil, agentCopyOxiDNS, false)
			if err != nil {
				return err
			}
			before[id] = config
		}
		savedShared, err = applySharedIn(document, func(shared *model.Shared) error {
			baseline, err := copyRecord(shared)
			if err != nil {
				return err
			}
			next, err := copyRecord(shared)
			if err != nil {
				return err
			}
			if err := sharedEdit(ctx, next); err != nil {
				return err
			}
			return changedMembers(SharedFields, baseline, next, shared.SetJSON)
		})
		if err != nil {
			return err
		}
		for _, id := range selected {
			saved, err := updateProfileIn(document, id, func(profile *model.Profile) error {
				after, err := resolveConfig(document, id, nil, agentCopyOxiDNS, false)
				if err != nil {
					return err
				}
				raw, err := model.Marshal(after)
				if err != nil {
					return err
				}
				baseline, err := model.DecodeConfig(raw)
				if err != nil {
					return err
				}
				if err := profileEdit(ctx, id, before[id], after); err != nil {
					return err
				}
				return changedMembers(ProfileFields, baseline, after, profile.SetJSON)
			})
			if err != nil {
				return err
			}
			savedProfiles = append(savedProfiles, saved)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return savedShared, savedProfiles, nil
}

func recordMemberJSON(record any, name string) (json.RawMessage, bool, error) {
	fields, err := recordMembers(record)
	if err != nil {
		return nil, false, err
	}
	raw, present := fields.Get(name)
	return raw, present, nil
}

// setRecordMemberJSON uses the model's existing record codec for resource
// records that expose Clear but do not yet expose SetJSON. It performs no
// schema conversion and retains every other member as opaque stored JSON.
func setRecordMemberJSON(record any, name string, raw json.RawMessage) error {
	fields, err := recordMembers(record)
	if err != nil {
		return err
	}
	if err := fields.Set(name, raw); err != nil {
		return err
	}
	encoded, err := fields.MarshalJSON()
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, record)
}
