package rulelibrary

import (
	"context"
	"errors"
	"os"
	"slices"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/rulefetch"
)

type sourceChange struct{ Old, Next model.RuleLibraryItem }
type profileWarning struct {
	Profile string `json:"profile"`
	Message string `json:"message"`
}
type mutationResult struct {
	Success  bool                   `json:"success"`
	Data     *model.RuleLibraryItem `json:"data,omitempty"`
	Synced   *int                   `json:"synced_count,omitempty"`
	Warnings *[]profileWarning      `json:"warnings,omitempty"`
}

func same(a, b model.RuleLibraryItem) bool {
	x, err := revision(a)
	if err != nil {
		return false
	}
	y, err := revision(b)
	return err == nil && x == y
}

func equalOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func declarationChanged(req *libraryRequest, baseline model.RuleLibraryItem) bool {
	sameMember := func(sent member[string], stored *string) bool {
		if !sent.Present() || sent.Null() {
			return stored == nil
		}
		return sent.OK && stored != nil && sent.Value == *stored
	}
	return !sameMember(req.URL, baseline.URL) ||
		!sameMember(req.SourceType, baseline.SourceType) ||
		!sameMember(req.Behavior, baseline.Behavior) ||
		!sameMember(req.Format, baseline.Format) ||
		!sameMember(req.Content, baseline.Content)
}

func routingChanged(a, b model.RuleLibraryItem) bool {
	behaviorOf := func(item model.RuleLibraryItem) string {
		if item.Behavior == nil {
			return "domain"
		}
		return *item.Behavior
	}
	return model.Flag(a.Enabled, true) != model.Flag(b.Enabled, true) || behaviorOf(a) != behaviorOf(b)
}

func downloadChanged(a, b model.RuleLibraryItem) bool {
	return !equalOptionalString(a.URL, b.URL) || sourceTypeOf(a) != sourceTypeOf(b)
}

func affects(profile *model.Profile, changes []sourceChange) (routing, downloads bool, err error) {
	for _, item := range profile.RuleConfigs {
		for _, change := range changes {
			if item.LibraryRuleID == change.Old.ID {
				routing = routing || routingChanged(change.Old, change.Next)
				downloads = downloads || downloadChanged(change.Old, change.Next)
			}
		}
	}
	return routing, downloads, nil
}

// Mutate replaces or removes a shared source without overwriting an intervening
// edit, and reconstructs the affected native profiles in the same transaction.
func (s *Service) Mutate(ctx context.Context, id string, req *libraryRequest, remove bool, base string) (mutationResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := mutationResult{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	var baseline model.RuleLibraryItem
	var preview model.RuleLibraryItem
	var prepared []byte
	changed := false
	var err error
	if !remove {
		baseline, err = s.source(ctx, id)
		if err != nil {
			return out, itemMissing(err)
		}
		if err = validateDeclaration(req.declaration(), &baseline); err != nil {
			return out, err
		}
		changed = declarationChanged(req, baseline)
		preview = req.previewItem(id)
		if changed {
			prepared, err = s.readSource(ctx, preview)
			if err != nil {
				if rulefetch.IsRequestException(err) {
					return out, fail(502, msgDownloadEdit)
				}
				return out, err
			}
			if prepared == nil {
				return out, fail(400, msgSource)
			}
		} else {
			prepared, err = s.readCache(ctx, baseline)
			if err != nil {
				return out, err
			}
		}
	}
	ctx, release, err := s.editLease(ctx, id)
	if err != nil {
		return out, err
	}
	defer release()
	if !remove && prepared != nil && req.sourceType() == "url" {
		path, err := s.cachePath(ctx, preview)
		if err != nil {
			return out, err
		}
		_, statErr := os.Stat(path)
		if changed || errors.Is(statErr, os.ErrNotExist) {
			if err = s.writeCache(ctx, preview, prepared); err != nil {
				return out, err
			}
		}
	}
	var previous model.RuleLibraryItem
	var saved model.RuleLibraryItem
	var stage *stagedCache
	committed := false
	defer func() {
		if !committed {
			stage.abort(ctx)
		}
	}()
	changes := []sourceChange{}
	if !remove {
		changes = append(changes, sourceChange{baseline, preview})
	}
	warnings, err := s.editWithProfiles(ctx, changes, func(tx context.Context, catalog *model.Shared) error {
		library := slices.Clone(catalog.RuleLibrary)
		i := find(library, id)
		if i < 0 {
			return fail(404, "Rule not found")
		}
		previous = library[i]
		if remove {
			catalog.RuleLibrary = slices.Delete(library, i, i+1)
			return nil
		}
		if !same(previous, baseline) {
			return fail(409, msgDraftConflict)
		}
		cleaned := *req
		cleaned.cleanForUpdate()
		if err = validateDeclaration(cleaned.declaration(), &previous); err != nil {
			return err
		}
		next, err := cleaned.decodeItem(id, previous.UpdateInterval, true)
		if err != nil {
			return libraryTypeError(library, i, &cleaned, id, err)
		}
		if sourceTypeOf(next) == "content" && prepared != nil {
			stage, err = s.stageCache(tx, next, prepared, s.replaceCache)
			if err != nil {
				return err
			}
		}
		saved = next
		library[i] = next
		catalog.RuleLibrary = library
		return nil
	}, base)
	if err != nil {
		if !rejectedBeforeCommit(err) {
			stage.failClosed(ctx)
			stage = nil
		}
		return out, err
	}
	committed = true
	if remove {
		if err = s.removeCache(ctx, previous); err != nil {
			return out, err
		}
	} else if err = stage.commit(ctx); err != nil {
		if stopped := ctx.Err(); stopped != nil {
			return out, stopped
		}
		return out, fail(500, msgCacheFailed)
	}
	if remove || changed {
		if err = s.forget(ctx, id); err != nil {
			return out, err
		}
	}
	if previous.Name != "" && (remove || previous.Name != saved.Name) {
		if err = s.removeLegacy(ctx, previous.Name); err != nil {
			return out, err
		}
	}
	if !remove {
		count, err := s.usage(ctx, id)
		if err != nil {
			return out, err
		}
		out.Data = &saved
		out.Synced = &count
		out.Warnings = &warnings
	}
	return out, nil
}
func itemMissing(err error) error {
	var f *fault
	if errors.As(err, &f) && f.status == 404 {
		return fail(404, "Rule not found")
	}
	return err
}
