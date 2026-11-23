package rulelibrary

import (
	"bytes"
	"configflow/internal/model"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"configflow/internal/lifecycle"
)

type draftResult struct {
	Success  bool    `json:"success"`
	Revision string  `json:"revision"`
	Rule     *record `json:"rule,omitempty"`
	Usage    *int    `json:"usage_count,omitempty"`
}

func revision(item record) (string, error) {
	raw, err := json.Marshal(item)
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
func source(ctx context.Context, id string) (record, error) {
	catalog, err := sharedConfig(ctx)
	if err != nil {
		return record{}, err
	}
	library, err := libraryItems(catalog)
	if err != nil {
		return record{}, err
	}
	i := find(library, id)
	if i < 0 {
		return record{}, fail(404, msgDraftMissing)
	}
	return library[i], nil
}

// Draft reads the current shared declaration with a deterministic editor token.
func (s *Service) Draft(ctx context.Context, id string) (draftResult, error) {
	out := draftResult{Success: true}
	item, err := source(ctx, id)
	if err != nil {
		return out, err
	}
	out.Revision, err = revision(item)
	if err != nil {
		return out, err
	}
	var selected record
	for _, key := range []string{"id", "name", "source_type", "behavior", "format", "content", "url"} {
		raw, ok := item.Get(key)
		if !ok {
			raw = json.RawMessage(`""`)
		}
		if err = selected.Set(key, raw); err != nil {
			return out, err
		}
	}
	out.Rule = &selected
	count, err := usage(ctx, id)
	out.Usage = &count
	return out, err
}

// SaveDraft changes only handwritten content and rejects stale editor tokens.
func (s *Service) SaveDraft(ctx context.Context, id string, body record) (draftResult, error) {
	ctx = lifecycle.Detach(ctx)
	out := draftResult{Success: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	token, tokenOK := stringField(body, "revision")
	content, contentOK := stringField(body, "content")
	if body.Len() != 2 || !tokenOK || !contentOK {
		return out, fail(400, msgDraftBody)
	}
	identity := record{}
	if err := put(&identity, "id", id); err != nil {
		return out, err
	}
	if err := put(&identity, "source_type", "content"); err != nil {
		return out, err
	}
	held, release, err := editLease(ctx, identity)
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
	err = editShared(ctx, func(tx context.Context, catalog *model.Shared) error {
		library, err := libraryItems(catalog)
		if err != nil {
			return err
		}
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
		if sourceType(old) != "content" {
			return fail(400, msgDraftRemote)
		}
		next := old
		if err = put(&next, "content", content); err != nil {
			return err
		}
		if err = validateContent(next, &old); err != nil {
			return err
		}
		staged, err = stageCache(tx, next, []byte(content), s.replaceCache)
		if err != nil {
			return err
		}
		library[i] = next
		if err = setLibrary(catalog, library); err != nil {
			return err
		}
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
