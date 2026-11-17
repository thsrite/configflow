package repo

import (
	"context"
	"crypto/subtle"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
)

// GetShared is get_shared(): a copy of the shared section (subscriptions,
// nodes, subscription_aggregations, rule_library, _revision).
func (r *Repository) GetShared(ctx context.Context) (result *jsonx.Object, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		result = document.GetObject("shared").DeepCopy()
		return nil
	})
	return result, err
}

// GetSharedWithSource is get_shared_with_source(): a copy of the shared
// section and the committed section it was read from (nil inside a
// transaction). The source is shared and must never be modified.
func (r *Repository) GetSharedWithSource(ctx context.Context) (result, source *jsonx.Object, err error) {
	document, committed, err := r.committedSource(ctx)
	if err != nil {
		return nil, nil, err
	}
	shared := document.GetObject("shared")
	if committed {
		source = shared
	}
	return shared.DeepCopy(), source, nil
}

// CreateRuleLibraryItem is create_rule_library_item(rule, content): commit a
// new rule source together with its prepared cache file; an existing id or
// cache is never replaced. The cache lock is kept through the document
// commit, so a failed commit removes only this creation's file. When the
// outcome of the commit is unknown the cache is kept and the error is
// ErrRuleLibraryCreationUncertain.
func (r *Repository) CreateRuleLibraryItem(ctx context.Context, rule *jsonx.Object, content []byte) (*jsonx.Object, error) {
	if _, err := itemsByID([]any{rule}, "rule_library"); err != nil {
		return nil, err
	}
	ruleID := rule.GetString("id")
	path, err := r.SharedPath(filepath.Join("rules", rules.RuleCacheFilename(rule)))
	if err != nil {
		return nil, err
	}
	ownsCache, preserve := false, false
	// The cache lock is taken inside the transaction and kept through the
	// commit and the clean-up (Python: an ExitStack around both).
	var releaseCache func()
	defer func() {
		if ownsCache && !preserve {
			os.Remove(path)
		}
		if releaseCache != nil {
			releaseCache()
		}
	}()
	err = r.update(ctx, writeOptions{}, func(ctx context.Context, document *jsonx.Object) error {
		shared := document.GetObject("shared")
		for _, item := range shared.GetList("rule_library") {
			if object, _ := jsonx.AsObject(item); object.Value("id") == ruleID {
				return exists("规则库 ID 已存在，请刷新后重试")
			}
		}
		_, release, err := acquireFile(ctx, path)
		if err != nil {
			return err
		}
		releaseCache = release
		// A previous/orphaned cache also belongs to an existing identity;
		// importing a new source must not overwrite its custom bytes.
		if pathExists(path) || pathExists(path+".bak") {
			return exists("规则缓存已存在，请使用新的规则 ID")
		}
		ownsCache = true
		shared.Set("rule_library", append(append([]any{}, shared.GetList("rule_library")...), rule.DeepCopy()))
		bumpRevision(shared)
		return writeAtomic(path, content)
	})
	if err != nil {
		if !ownsCache {
			return nil, err
		}
		// A failed commit normally rolls back. If the outcome is unknown
		// (COMMIT reported an error), retain the cache for any possibly
		// committed source. Reads need no document lock.
		preserve = true
		if raw, failure := r.store.Load(); failure == nil {
			if current, failure := decodeEntry(raw); failure == nil {
				preserve = false
				for _, item := range current.Document.GetObject("shared").GetList("rule_library") {
					if object, _ := jsonx.AsObject(item); object.Value("id") == ruleID {
						preserve = true
					}
				}
			}
		}
		if preserve {
			return nil, &Error{Kind: ErrRuleLibraryCreationUncertain, Message: "规则保存结果待确认，请刷新列表核对，勿重复导入", Cause: err}
		}
		return nil, err
	}
	preserve = true
	return rule.DeepCopy(), nil
}

// applySharedLocked is _apply_shared_locked: apply one shared updater inside
// an open write transaction. Removing or disabling a resource that is still
// referenced fails with ErrInUse.
func (r *Repository) applySharedLocked(ctx context.Context, document *jsonx.Object, updater Updater) (*jsonx.Object, error) {
	current := document.GetObject("shared")
	updated, err := applyUpdater(ctx, current.DeepCopy(), updater)
	if err != nil {
		return nil, err
	}
	proposed := jsonx.NewObject()
	for _, key := range SharedFields {
		proposed.Set(key, jsonx.DeepCopy(updated.GetOr(key, current.Value(key))))
	}
	for _, kind := range SharedFields {
		remaining, err := itemsByID(proposed.Value(kind), kind)
		if err != nil {
			return nil, err
		}
		for _, item := range current.GetList(kind) {
			resource, _ := jsonx.AsObject(item)
			id := resource.GetString("id")
			kept, found := remaining.values[id]
			disabled := kind != "rule_library" && truthyOr(resource, "enabled", true) && !truthyOr(kept, "enabled", true)
			if found && !disabled {
				continue
			}
			usages, err := resourceUsage(document, kind, id)
			if err != nil {
				return nil, err
			}
			if len(usages) > 0 {
				names := make([]string, len(usages))
				for index, usage := range usages {
					names[index] = jsonx.Str(usage.(*jsonx.Object).Value("name"))
				}
				return nil, inUse("资源 "+jsonx.Str(resource.GetOr("name", id))+" 仍被引用："+strings.Join(names, "、"), usages...)
			}
		}
	}
	proposed.Set("_revision", jsonx.Int(revisionOf(current)+1))
	document.Set("shared", proposed)
	return proposed, nil
}

// UpdateSharedTransaction is update_shared_transaction: updater edits a copy
// of the shared section; the SharedFields of its result are stored and the
// revision advances by one. It returns a copy of the committed section.
func (r *Repository) UpdateSharedTransaction(ctx context.Context, updater Updater) (*jsonx.Object, error) {
	var proposed *jsonx.Object
	err := r.update(ctx, writeOptions{}, func(ctx context.Context, document *jsonx.Object) error {
		var err error
		proposed, err = r.applySharedLocked(ctx, document, updater)
		return err
	})
	if err != nil {
		return nil, err
	}
	return proposed.DeepCopy(), nil
}

// SaveShared is save_shared(snapshot, baseline).
func (r *Repository) SaveShared(ctx context.Context, snapshot, baseline *jsonx.Object) (*jsonx.Object, error) {
	if snapshot == nil {
		return nil, validationError("Shared data must be an object")
	}
	return r.UpdateSharedTransaction(ctx, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		if revisionConflict(snapshot, current, baseline) {
			return nil, inUse("共享资源已更新，请刷新后重试")
		}
		for _, key := range SharedFields {
			value, ok := snapshot.Get(key)
			if !ok {
				continue
			}
			if base, ok := baseline.Get(key); ok {
				current.Set(key, ThreeWayMerge(current.Value(key), base, value))
			} else {
				current.Set(key, jsonx.DeepCopy(value))
			}
		}
		return nil, nil
	})
}

// ProfileUpdater is the profile_updater of
// UpdateSharedAndProfilesTransaction: before is the profile resolved against
// the shared data as it was, after against the shared updater's result;
// after is edited (or replaced by the returned object) like in any profile
// updater.
type ProfileUpdater func(ctx context.Context, profileID string, before, after *jsonx.Object) (replacement *jsonx.Object, err error)

// ProfileSelector chooses the profiles of UpdateSharedAndProfilesTransaction
// from the current shared and profiles sections, which it must only read.
type ProfileSelector func(shared, profiles *jsonx.Object) ([]string, error)

// Profiles is a ProfileSelector for a fixed list of ids.
func Profiles(ids ...string) ProfileSelector {
	return func(*jsonx.Object, *jsonx.Object) ([]string, error) { return ids, nil }
}

// UpdateSharedAndProfilesTransaction is
// update_shared_and_profiles_transaction: update shared resources and
// profiles in one commit; any failure writes nothing. Each section's
// revision advances once. It returns copies of the shared section and of
// the updated profiles (an object keyed by profile id, in selection order).
func (r *Repository) UpdateSharedAndProfilesTransaction(ctx context.Context, sharedUpdater Updater,
	profileUpdater ProfileUpdater, profileIDs ProfileSelector) (shared, profiles *jsonx.Object, err error) {
	err = r.update(ctx, writeOptions{}, func(ctx context.Context, document *jsonx.Object) error {
		chosen, err := profileIDs(document.GetObject("shared"), document.GetObject("profiles"))
		if err != nil {
			return err
		}
		var selected []string
		seen := map[string]bool{}
		for _, profileID := range chosen {
			if !seen[profileID] {
				seen[profileID] = true
				selected = append(selected, profileID)
			}
		}
		before := map[string]*jsonx.Object{}
		for _, profileID := range selected {
			if before[profileID], err = resolveProfile(document, profileID, nil, true); err != nil {
				return err
			}
		}
		updatedShared, err := r.applySharedLocked(ctx, document, sharedUpdater)
		if err != nil {
			return err
		}
		updatedProfiles := jsonx.NewObject()
		for _, profileID := range selected {
			profile, err := r.updateProfileLocked(ctx, document, profileID, func(ctx context.Context, snapshot *jsonx.Object) (*jsonx.Object, error) {
				return profileUpdater(ctx, profileID, before[profileID], snapshot)
			})
			if err != nil {
				return err
			}
			updatedProfiles.Set(profileID, profile)
		}
		shared, profiles = updatedShared.DeepCopy(), updatedProfiles.DeepCopy()
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return shared, profiles, nil
}

// GetResourceUsage is get_resource_usage(resource_type, resource_id).
func (r *Repository) GetResourceUsage(ctx context.Context, resourceType, resourceID string) (result []any, err error) {
	known := false
	for _, field := range SharedFields {
		known = known || field == resourceType
	}
	if !known {
		return nil, validationError("未知资源类型")
	}
	err = r.view(ctx, func(document *jsonx.Object) error {
		usages, err := resourceUsage(document, resourceType, resourceID)
		if err != nil {
			return err
		}
		result = jsonx.DeepCopy(usages).([]any)
		return nil
	})
	return result, err
}

// nodeDeletionPlan is _node_deletion_plan.
func (r *Repository) nodeDeletionPlan(document *jsonx.Object, nodeIDs any) (preview, candidate *jsonx.Object, err error) {
	plan := currentHooks().PlanNodeDeletion
	if plan == nil {
		return nil, nil, repositoryError("节点删除功能尚未接入（PlanNodeDeletion 未注册）")
	}
	if preview, candidate, err = plan(document, nodeIDs); err != nil {
		return nil, nil, err
	}
	if !jsonx.Truthy(preview.Value("blockers")) {
		if failure := ValidateDocument(candidate); failure != nil {
			if !errors.Is(failure, ErrRepository) {
				return nil, nil, failure
			}
			preview.Set("blockers", append(append([]any{}, preview.GetList("blockers")...), failure.Error()))
		}
	}
	return preview, candidate, nil
}

// PreviewNodeDeletion is preview_node_deletion(node_ids).
func (r *Repository) PreviewNodeDeletion(ctx context.Context, nodeIDs any) (preview *jsonx.Object, err error) {
	err = r.view(ctx, func(document *jsonx.Object) error {
		preview, _, err = r.nodeDeletionPlan(document, nodeIDs)
		return err
	})
	if err != nil {
		return nil, err
	}
	return preview, nil
}

var confirmationPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// DeleteNodesWithReferences is delete_nodes_with_references(node_ids,
// confirmation): delete nodes and detach them from groups and aggregations
// in one transaction, provided the plan still has the reviewed
// confirmation digest.
func (r *Repository) DeleteNodesWithReferences(ctx context.Context, nodeIDs any, confirmation string) (preview *jsonx.Object, err error) {
	if !confirmationPattern.MatchString(confirmation) {
		return nil, validationError("请先查看并确认删除影响")
	}
	err = r.update(ctx, writeOptions{}, func(ctx context.Context, document *jsonx.Object) error {
		var candidate *jsonx.Object
		var err error
		if preview, candidate, err = r.nodeDeletionPlan(document, nodeIDs); err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(confirmation), []byte(preview.GetString("confirmation"))) != 1 {
			return newError(ErrNodeDeletionChanged, "节点或引用关系已变化，请重新查看删除影响并确认")
		}
		if blockers := preview.GetList("blockers"); len(blockers) > 0 {
			return inUse("无法删除："+strings.Join(jsonx.Strings(blockers), "；"), preview.GetList("usages")...)
		}
		var changedProfiles []string
		seen := map[string]bool{}
		for _, item := range preview.GetList("groups") {
			group, _ := jsonx.AsObject(item)
			if profileID := group.GetString("profile_id"); !seen[profileID] {
				seen[profileID] = true
				changedProfiles = append(changedProfiles, profileID)
			}
		}
		for _, profileID := range changedProfiles {
			_, err := r.updateProfileLocked(ctx, document, profileID, func(_ context.Context, profile *jsonx.Object) (*jsonx.Object, error) {
				profile.Set("proxy_groups", candidate.GetObject("profiles").GetObject(profileID).Value("proxy_groups"))
				return nil, nil
			})
			if err != nil {
				return err
			}
		}
		// Detach first inside the same transaction, then retain the normal
		// shared-resource deletion guard and the final document validation.
		document.GetObject("shared").Set("subscription_aggregations",
			candidate.GetObject("shared").Value("subscription_aggregations"))
		_, err = r.applySharedLocked(ctx, document, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
			shared.Set("nodes", candidate.GetObject("shared").Value("nodes"))
			return nil, nil
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return preview, nil
}
