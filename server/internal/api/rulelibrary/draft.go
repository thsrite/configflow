package rulelibrary

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

type draftResult struct {
	Success  bool       `json:"success"`
	Revision string     `json:"revision"`
	Rule     *draftRule `json:"rule,omitempty"`
	Usage    *int       `json:"usage_count,omitempty"`
}

func revision(item model.RuleLibraryItem) (string, error) {
	raw, err := model.Marshal(item)
	if err != nil {
		return "", err
	}
	var members map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&members); err != nil {
		return "", err
	}
	raw, err = json.Marshal(members)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (s *Service) source(ctx context.Context, id string) (model.RuleLibraryItem, error) {
	catalog, err := s.sharedConfig(ctx)
	if err != nil {
		return model.RuleLibraryItem{}, err
	}
	i := find(catalog.RuleLibrary, id)
	if i < 0 {
		return model.RuleLibraryItem{}, fail(404, msgDraftMissing)
	}
	return catalog.RuleLibrary[i], nil
}

// Draft reads the current shared declaration with a deterministic editor token.
func (s *Service) Draft(ctx context.Context, id string) (draftResult, error) {
	out := draftResult{Success: true}
	item, err := s.source(ctx, id)
	if err != nil {
		return out, err
	}
	out.Revision, err = revision(item)
	if err != nil {
		return out, err
	}
	out.Rule = &draftRule{
		ID:         item.ID,
		Name:       item.Name,
		SourceType: model.TextValue(item.SourceType),
		Behavior:   model.TextValue(item.Behavior),
		Format:     model.TextValue(item.Format),
		Content:    model.TextValue(item.Content),
		URL:        model.TextValue(item.URL),
	}
	count, err := s.usage(ctx, id)
	out.Usage = &count
	return out, err
}

// SaveDraft changes only handwritten content and rejects stale editor tokens.
func (s *Service) SaveDraft(ctx context.Context, id string, body *draftRequest) (draftResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := draftResult{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if body == nil || !body.exact {
		return out, fail(400, msgDraftBody)
	}
	token, content := body.Revision.Value, body.Content.Value
	held, release, err := editLease(ctx, id)
	if err != nil {
		return out, err
	}
	defer release()
	ctx = held
	var staged *stagedCache
	committed := false
	defer func() {
		if !committed {
			staged.abort(ctx)
		}
	}()
	err = s.editShared(ctx, func(tx context.Context, catalog *model.Shared) error {
		library := slices.Clone(catalog.RuleLibrary)
		i := find(library, id)
		if i < 0 {
			return fail(404, msgDraftMissing)
		}
		old := library[i]
		current, err := revision(old)
		if err != nil {
			return err
		}
		if current != token {
			return fail(409, msgDraftConflict)
		}
		if sourceTypeOf(old) != "content" {
			return fail(400, msgDraftRemote)
		}
		next := old
		next.Content = new(content)
		if err = validateItemContent(next, &old); err != nil {
			return err
		}
		staged, err = stageCache(tx, next, []byte(content), s.replaceCache)
		if err != nil {
			return err
		}
		library[i] = next
		catalog.RuleLibrary = library
		out.Revision, err = revision(next)
		return err
	})
	if err != nil {
		if !rejectedBeforeCommit(err) {
			staged.failClosed(ctx)
			staged = nil
		}
		return out, err
	}
	committed = true
	if err = staged.commit(ctx); err != nil {
		if stopped := ctx.Err(); stopped != nil {
			return out, stopped
		}
		return out, fail(500, msgCacheFailed)
	}
	return out, nil
}
