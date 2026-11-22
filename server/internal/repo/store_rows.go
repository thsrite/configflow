package repo

import (
	"context"
	"encoding/json"

	"configflow/internal/model"
	"configflow/internal/store"
)

// Native stored generations and row commits.

func decodeEntry(raw store.Entry) (storedEntry, error) {
	document, issues, err := decodeModelRows(raw.Document)
	if err != nil {
		return storedEntry{}, err
	}
	return storedEntry{Entry: raw, Model: document, Issues: issues}, nil
}

func (r *Repository) createStore(doc *model.Document, meta map[string]json.RawMessage) (storedEntry, error) {
	raw, err := encodeModelRows(doc, nil)
	if err != nil {
		return storedEntry{}, err
	}
	extra := meta
	entry, err := r.store.Create(raw, extra)
	if err != nil {
		return storedEntry{}, err
	}
	return decodeEntry(entry)
}

func (r *Repository) commitStore(ctx context.Context, base storedEntry, doc *model.Document, meta map[string]json.RawMessage) (storedEntry, bool, error) {
	raw, err := encodeModelRows(doc, base.Entry.Document)
	if err != nil {
		return storedEntry{}, false, err
	}
	if err := keepUnchangedRows(raw, base.Entry.Document, base.Model); err != nil {
		return storedEntry{}, false, err
	}
	if err := keepDamagedConfigToken(raw, base.Entry.Document, base.Issues, doc); err != nil {
		return storedEntry{}, false, err
	}
	extra := meta
	entry, changed, err := r.store.CommitContext(ctx, base.Entry, raw, extra)
	if err != nil || !changed {
		return storedEntry{}, changed, err
	}
	result, err := decodeEntry(entry)
	return result, changed, err
}
