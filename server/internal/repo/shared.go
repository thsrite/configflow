package repo

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"configflow/internal/ctxx"
	"configflow/internal/model"
	"configflow/internal/rules"
)

// CreateRuleLibraryRecord is create_rule_library_item(rule, content): commit a
// new rule source together with its prepared cache file; an existing id or
// cache is never replaced. The cache lock is kept through the document
// commit, so a failed commit removes only this creation's file. When the
// outcome of the commit is unknown the cache is kept and the error is
// ErrRuleLibraryCreationUncertain.
func (r *Repository) CreateRuleLibraryRecord(ctx context.Context, item model.RuleLibraryItem, content []byte) (*model.RuleLibraryItem, error) {
	ctx = ctxx.Detach(ctx)
	raw, err := model.Marshal(item)
	if err != nil {
		return nil, err
	}
	if _, err := itemsOf(append(append([]byte(`[`), raw...), ']'), "rule_library"); err != nil {
		return nil, err
	}
	ruleID := item.ID
	var source model.RuleConfig
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	path, err := r.SharedPath(filepath.Join("rules", rules.CacheFilename(source)))
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
	err = r.transact(ctx, writeOptions{batch: &batch}, func(ctx context.Context, tx *transaction) error {
		shared := &tx.document.Shared
		for _, existing := range shared.RuleLibrary {
			if existing.ID == ruleID {
				return exists(msgRuleLibraryExists)
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
			return exists(msgRuleCacheExists)
		}
		if err := beginCommitBatch(ctx, &batch); err != nil {
			return err
		}
		ownsCache = true
		shared.RuleLibrary = append(shared.RuleLibrary, item)
		shared.Revision++
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
				for _, existing := range current.Model.Shared.RuleLibrary {
					if existing.ID == ruleID {
						preserve = true
					}
				}
			}
		}
		if preserve {
			return nil, &Error{Kind: ErrRuleLibraryCreationUncertain, Message: msgRuleLibraryUncertain, Cause: err}
		}
		return nil, err
	}
	preserve = true
	return copyRecord(&item)
}

var confirmationPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (r *Repository) nodeDeletionRecords(document *model.Document, ids []string) (*NodeDeletionPreview, *model.Document, error) {
	plan := recordHooks().PlanNodeDeletionRecords
	if plan == nil {
		return nil, nil, repositoryError(msgDeletionHookMissing)
	}
	preview, candidate, err := plan(document, ids)
	if err != nil {
		return nil, nil, err
	}
	if preview == nil || candidate == nil {
		return nil, nil, validationError(msgStoreDocumentObject)
	}
	if len(preview.Blockers) == 0 {
		if err := validateDocument(candidate); err != nil {
			if !errors.Is(err, ErrRepository) {
				return nil, nil, err
			}
			preview.Blockers = append(slices.Clone(preview.Blockers), err.Error())
		}
	}
	return preview, candidate, nil
}

// PreviewNodeDeletionIDs computes the impact for validated request IDs.
func (r *Repository) PreviewNodeDeletionIDs(ctx context.Context, ids []string) (*NodeDeletionPreview, error) {
	var preview *NodeDeletionPreview
	err := r.View(ctx, func(document *model.Document) error {
		// The collaborator owns its candidate and cannot mutate a committed snapshot.
		raw, err := model.Marshal(document)
		if err != nil {
			return err
		}
		owned, err := model.DecodeDocument(raw)
		if err != nil {
			return err
		}
		preview, _, err = r.nodeDeletionRecords(owned, ids)
		return err
	})
	return preview, err
}

// DeleteNodesWithReferencesIDs deletes the confirmed nodes and their detachable
// references in one guarded transaction. IDs have already passed request binding.
func (r *Repository) DeleteNodesWithReferencesIDs(ctx context.Context, ids []string, confirmation string) (*NodeDeletionPreview, error) {
	ctx = ctxx.Detach(ctx)
	if !confirmationPattern.MatchString(confirmation) {
		return nil, validationError(msgDeletionConfirm)
	}
	var preview *NodeDeletionPreview
	err := r.Update(ctx, func(ctx context.Context, document *model.Document) error {
		raw, err := model.Marshal(document)
		if err != nil {
			return err
		}
		owned, err := model.DecodeDocument(raw)
		if err != nil {
			return err
		}
		var candidate *model.Document
		preview, candidate, err = r.nodeDeletionRecords(owned, ids)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(confirmation), []byte(preview.Confirmation)) != 1 {
			return newError(ErrNodeDeletionChanged, msgDeletionChanged)
		}
		if len(preview.Blockers) > 0 {
			usages := make([]any, len(preview.Usages))
			for i, item := range preview.Usages {
				usages[i] = item
			}
			return inUse(msgDeletionBlockedPrefix+strings.Join(preview.Blockers, "；"), usages...)
		}
		seen := map[string]bool{}
		for _, group := range preview.Groups {
			if seen[group.ProfileID] {
				continue
			}
			seen[group.ProfileID] = true
			next, err := profileFor(candidate, group.ProfileID)
			if err != nil {
				return err
			}
			raw, _, err := recordMemberJSON(next, "proxy_groups")
			if err != nil {
				return err
			}
			if _, err := updateProfileIn(document, group.ProfileID, func(profile *model.Profile) error { return profile.SetJSON("proxy_groups", raw) }); err != nil {
				return err
			}
		}
		raw, _, err = recordMemberJSON(candidate.Shared, "subscription_aggregations")
		if err != nil {
			return err
		}
		if err := document.Shared.SetJSON("subscription_aggregations", raw); err != nil {
			return err
		}
		raw, _, err = recordMemberJSON(candidate.Shared, "nodes")
		if err != nil {
			return err
		}
		_, err = applySharedIn(document, func(shared *model.Shared) error { return shared.SetJSON("nodes", raw) })
		return err
	})
	if err != nil {
		return nil, err
	}
	return preview, nil
}
