package repo

import (
	"context"
	"encoding/json"

	"configflow/internal/ctxx"
	"configflow/internal/model"
)

// SaveSharedRecord merges a shared snapshot against an optional read baseline.
// With a baseline only the caller's delta is applied; otherwise a supplied
// revision must still match. Unknown stored members are never replaced.
func (r *Repository) SaveSharedRecord(ctx context.Context, snapshot, baseline *model.Shared) (*model.Shared, error) {
	ctx = ctxx.Detach(ctx)
	if snapshot == nil {
		return nil, validationError("Shared data must be an object")
	}
	proposed, err := recordMembers(snapshot)
	if err != nil {
		return nil, err
	}
	var base model.Fields
	if baseline != nil {
		base, err = recordMembers(baseline)
		if err != nil {
			return nil, err
		}
	}
	var result *model.Shared
	err = r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		var err error
		result, err = applySharedIn(document, func(current *model.Shared) error {
			if baseline == nil {
				if revision, present := proposed.Get("_revision"); present {
					raw, _, err := recordMemberJSON(current, "_revision")
					if err != nil {
						return err
					}
					if !sameStored(revision, raw) {
						return inUse(msgSharedChanged)
					}
				}
			}
			return mergeSectionMembers(SharedFields, proposed, base, current, current.SetJSON, false)
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return copyRecord(result)
}

// SaveSystemRecord applies a system snapshot, merging concurrent edits against
// baseline when supplied and deep-merging object settings otherwise.
func (r *Repository) SaveSystemRecord(ctx context.Context, snapshot, baseline *model.System) (*model.System, error) {
	ctx = ctxx.Detach(ctx)
	if snapshot == nil {
		return nil, validationError("System data must be an object")
	}
	proposed, err := recordMembers(snapshot)
	if err != nil {
		return nil, err
	}
	var base model.Fields
	if baseline != nil {
		base, err = recordMembers(baseline)
		if err != nil {
			return nil, err
		}
	}
	var result *model.System
	err = r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		var err error
		result, err = applySystemIn(document, func(current *model.System) error {
			if baseline == nil {
				if revision, present := proposed.Get("_revision"); present {
					raw, _, err := recordMemberJSON(current, "_revision")
					if err != nil {
						return err
					}
					if !sameStored(revision, raw) {
						return inUse(msgSystemChanged)
					}
				}
			}
			return mergeSectionMembers(systemSections, proposed, base, current, current.SetJSON, true)
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return copyRecord(result)
}

// SaveSystemConfig merges system_config against an optional baseline under one
// system transaction without copying or encoding the Agent registry.
func (r *Repository) SaveSystemConfig(ctx context.Context, snapshot, baseline *model.SystemConfig) (*model.SystemConfig, error) {
	if snapshot == nil {
		return nil, validationError("System config must be an object")
	}
	saved, err := r.saveSystemMember(ctx, "system_config", snapshot, baseline)
	if err != nil {
		return nil, err
	}
	return copyRecord(&saved.SystemConfig)
}

// SaveBackup merges backup settings against an optional baseline under one
// system transaction without copying or encoding the Agent registry.
func (r *Repository) SaveBackup(ctx context.Context, snapshot, baseline *model.Backup) (*model.Backup, error) {
	if snapshot == nil {
		return nil, validationError("Backup settings must be an object")
	}
	saved, err := r.saveSystemMember(ctx, "backup", snapshot, baseline)
	if err != nil {
		return nil, err
	}
	return copyRecord(&saved.Backup)
}

func (r *Repository) saveSystemMember(ctx context.Context, key string, snapshot, baseline any) (*model.System, error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := model.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	var previous json.RawMessage
	hasBaseline := false
	if baseline != nil {
		encoded, err := model.Marshal(baseline)
		if err != nil {
			return nil, err
		}
		previous = encoded
		hasBaseline = true
	}
	var result *model.System
	err = r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		result, err = applySystemIn(document, func(current *model.System) error {
			existing, _, err := SystemMemberJSON(current, key)
			if err != nil {
				return err
			}
			merged := json.RawMessage(raw)
			if hasBaseline {
				merged, _, err = MergeJSON(existing, previous, merged)
				if err != nil {
					return err
				}
			} else if jsonKind(existing) == '{' && jsonKind(merged) == '{' {
				merged, err = mergeObjectsJSON(existing, merged)
				if err != nil {
					return err
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return current.SetJSON(key, merged)
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func mergeSectionMembers(keys []string, proposed, base model.Fields, current any, set func(string, json.RawMessage) error, deep bool) error {
	stored, err := recordMembers(current)
	if err != nil {
		return err
	}
	for _, key := range keys {
		raw, present := proposed.Get(key)
		if !present {
			continue
		}
		existing, _ := stored.Get(key)
		if previous, present := base.Get(key); present {
			raw, _, err = MergeJSON(existing, previous, raw)
			if err != nil {
				return err
			}
		} else if deep && jsonKind(existing) == '{' && jsonKind(raw) == '{' {
			raw, err = mergeObjectsJSON(existing, raw)
			if err != nil {
				return err
			}
		}
		if err := set(key, raw); err != nil {
			return err
		}
	}
	return nil
}

// SharedConfig loads an owned shared snapshot and records its immutable baseline
// in the request Scope, retaining three-way save semantics for catalog editors.
func SharedConfig(ctx context.Context) (*model.Shared, error) {
	repository, err := GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	result, err := repository.Shared(ctx)
	if err != nil {
		return nil, err
	}
	fields, err := recordMembers(result)
	if err != nil {
		return nil, err
	}
	if scope := ScopeOf(ctx); scope != nil {
		scope.mu.Lock()
		scope.bind(repository)
		if scope.recordBaselines == nil {
			scope.recordBaselines = map[string]model.Fields{}
		}
		scope.recordBaselines[sharedBaselineKey] = fields
		scope.mu.Unlock()
	}
	return result, nil
}

// SaveSharedRecord saves a shared catalog using the explicit baseline, or the
// baseline captured by SharedConfig in the same Scope. Successful saves reset it.
func SaveSharedRecord(ctx context.Context, snapshot, baseline *model.Shared) error {
	ctx = ctxx.Detach(ctx)
	repository, err := GetRepository(ctx)
	if err != nil {
		return err
	}
	if baseline == nil {
		if scope := ScopeOf(ctx); scope != nil {
			scope.mu.Lock()
			scope.bind(repository)
			fields, present := scope.recordBaselines[sharedBaselineKey]
			scope.mu.Unlock()
			if present {
				raw, err := fields.MarshalJSON()
				if err != nil {
					return err
				}
				baseline = &model.Shared{}
				if err := json.Unmarshal(raw, baseline); err != nil {
					return err
				}
			}
		}
	}
	if _, err := repository.SaveSharedRecord(ctx, snapshot, baseline); err != nil {
		return err
	}
	ResetConfigContext(ctx)
	return nil
}
