package repo

import (
	"configflow/internal/ctxx"
	"context"
	"crypto/subtle"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/rules"
)

// GetShared is get_shared(): a copy of the shared section (subscriptions,
// nodes, subscription_aggregations, rule_library, _revision).
func (r *Repository) GetShared(ctx context.Context) (result *jsonx.Object, err error) {
	err = r.read(ctx, func(document *model.Document) error {
		result, err = legacyObject(&document.Shared)
		return err
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
	ctx = ctxx.Detach(ctx)
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
	batch := false
	err = r.update(ctx, writeOptions{batch: &batch}, func(ctx context.Context, document *jsonx.Object) error {
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
		if err := beginCommitBatch(ctx, &batch); err != nil {
			return err
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

// UpdateSharedTransaction is update_shared_transaction: updater edits a copy
// of the shared section; the SharedFields it changed are stored and the
// revision advances by one. It returns a copy of the committed section.
func (r *Repository) UpdateSharedTransaction(ctx context.Context, updater Updater) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
	var proposed *jsonx.Object
	err := r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		shared, err := applySharedLegacy(ctx, document, updater)
		if err == nil {
			proposed, err = legacyObject(shared)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return proposed, nil
}

// SaveShared is save_shared(snapshot, baseline).
func (r *Repository) SaveShared(ctx context.Context, snapshot, baseline *jsonx.Object) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
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
				merged, err := threeWayMerge(current.Value(key), base, value)
				if err != nil {
					return nil, err
				}
				current.Set(key, merged)
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
	ctx = ctxx.Detach(ctx)
	err = r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		sharedView, err := legacyObject(&document.Shared)
		if err != nil {
			return err
		}
		profilesView := jsonx.NewObject()
		for _, profile := range document.Profiles {
			view, err := legacyObject(profile)
			if err != nil {
				return err
			}
			profilesView.Set(profile.Key(), view)
		}
		chosen, err := profileIDs(sharedView, profilesView)
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
			if before[profileID], err = legacyConfig(document, profileID, nil, true); err != nil {
				return err
			}
		}
		updatedShared, err := applySharedLegacy(ctx, document, sharedUpdater)
		if err != nil {
			return err
		}
		if shared, err = legacyObject(updatedShared); err != nil {
			return err
		}
		profiles = jsonx.NewObject()
		for _, profileID := range selected {
			profile, err := updateProfileLegacy(ctx, document, profileID, func(ctx context.Context, snapshot *jsonx.Object) (*jsonx.Object, error) {
				return profileUpdater(ctx, profileID, before[profileID], snapshot)
			})
			if err != nil {
				return err
			}
			view, err := legacyObject(profile)
			if err != nil {
				return err
			}
			profiles.Set(profileID, view)
		}
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
	err = r.read(ctx, func(document *model.Document) error {
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
	ctx = ctxx.Detach(ctx)
	if !confirmationPattern.MatchString(confirmation) {
		return nil, validationError("请先查看并确认删除影响")
	}
	err = r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		legacy, err := r.working(ctx)
		if err != nil {
			return err
		}
		var candidate *jsonx.Object
		if preview, candidate, err = r.nodeDeletionPlan(legacy, nodeIDs); err != nil {
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
			groups := candidate.GetObject("profiles").GetObject(profileID).Value("proxy_groups")
			_, err := updateProfileIn(document, profileID, func(profile *model.Profile) error {
				return setLegacy(profile.SetJSON, "proxy_groups", groups)
			})
			if err != nil {
				return err
			}
		}
		// Detach first inside the same transaction, then retain the normal
		// shared-resource deletion guard and the final document validation.
		candidateShared := candidate.GetObject("shared")
		if err := setLegacy(document.Shared.SetJSON, "subscription_aggregations", candidateShared.Value("subscription_aggregations")); err != nil {
			return err
		}
		_, err = applySharedIn(document, func(shared *model.Shared) error {
			return setLegacy(shared.SetJSON, "nodes", candidateShared.Value("nodes"))
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return preview, nil
}
