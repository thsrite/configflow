package repo

// Legacy adapter (design n7-n8 2.5): the *jsonx.Object API that consumers
// still use until their own N8-8…N8-10 step migrates them. It holds no logic
// of its own: values cross between the typed core and jsonx by model.Marshal →
// jsonx.Parse and jsonx.Compact → the model codec. Deleted in N8-11.

import (
	"context"
	"encoding/json"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/store"
)

// workingModel returns the typed working document of the write transaction
// ctx belongs to (a fresh conversion while a legacy updater runs); ok is false
// outside a transaction.
func (r *Repository) workingModel(ctx context.Context) (document *model.Document, ok bool, err error) {
	tx := r.transactionOf(ctx)
	if tx == nil {
		return nil, false, nil
	}
	if tx.legacy == nil {
		return tx.document, true, nil
	}
	document, err = modelFromLegacy(tx.legacy, tx.base.Entry.Document)
	return document, true, err
}

// working returns the legacy view of the working document of the write
// transaction ctx belongs to, or nil: the legacy updater's own copy while one
// runs, else a fresh conversion of the typed working copy.
func (r *Repository) working(ctx context.Context) (*jsonx.Object, error) {
	tx := r.transactionOf(ctx)
	if tx == nil {
		return nil, nil
	}
	if tx.legacy != nil {
		return tx.legacy, nil
	}
	return legacyModelDocument(tx.document)
}

// modelFromLegacy converts a legacy document into the typed one. Rows whose
// legacy value is unchanged keep the bytes of base (legacyIntentRows).
func modelFromLegacy(legacy *jsonx.Object, base *store.Document) (*model.Document, error) {
	rows, err := legacyIntentRows(legacy, base)
	if err != nil {
		return nil, err
	}
	document, _, err := decodeModelRows(rows)
	if err != nil {
		return nil, &store.Error{Kind: store.ErrDocument, Message: err.Error(), Cause: err}
	}
	return document, nil
}

// update is the legacy write transaction: fn mutates a *jsonx.Object view of
// the working document, which is converted back afterwards. Members fn did
// not change keep their stored bytes (row level, legacyIntentRows).
func (r *Repository) update(ctx context.Context, options writeOptions, fn func(ctx context.Context, document *jsonx.Object) error) error {
	return r.transact(ctx, options, func(ctx context.Context, tx *transaction) error {
		// A legacy updater always starts from the base generation, so the
		// base rows are the rows of its starting point.
		legacy, err := legacyModelDocument(tx.document)
		if err != nil {
			return err
		}
		tx.legacy = legacy
		err = fn(ctx, legacy)
		tx.legacy = nil
		if err != nil {
			return err
		}
		if tx.document, err = modelFromLegacy(legacy, tx.base.Entry.Document); err != nil {
			return err
		}
		// legacy is exactly the converted working copy: validation reuses it.
		tx.proposed = legacy
		return nil
	})
}

// view is `with self._document() as document`: fn reads the transaction's
// working copy when ctx is inside one, the committed generation otherwise.
// fn must not modify the document or keep references to it.
func (r *Repository) view(ctx context.Context, fn func(document *jsonx.Object) error) error {
	document, _, err := r.committedSource(ctx)
	if err != nil {
		return err
	}
	return fn(document)
}

// committedSource is _committed_source: the document and whether it is a
// committed generation (which is never mutated and may be kept as the source
// of a three-way merge baseline) rather than a transaction's working copy.
func (r *Repository) committedSource(ctx context.Context) (document *jsonx.Object, committed bool, err error) {
	if r.InTransaction(ctx) {
		document, err := r.working(ctx)
		return document, false, err
	}
	entry, err := r.readEntry(ctx, false)
	if err != nil {
		return nil, false, err
	}
	return entry.Document, true, nil
}

// ensureLegacyRuleProxyToken runs ensureRuleProxyToken on the legacy view of
// the proposed document and, when it changed the settings, stores them in the
// typed working copy too (N8-5c migrates it to the typed system section).
func ensureLegacyRuleProxyToken(tx *transaction, legacy *jsonx.Object) error {
	system, err := compatObject(legacy.Value("system"))
	if err != nil {
		return err
	}
	changed, err := ensureRuleProxyToken(system)
	if err != nil || !changed {
		return err
	}
	encoded, err := jsonx.Compact(system.Value("system_config"))
	if err != nil {
		return err
	}
	var settings model.SystemConfig
	if err := json.Unmarshal(encoded, &settings); err != nil {
		return err
	}
	tx.document.System.SystemConfig = settings
	return nil
}

// threeWayMerge is MergeJSON for jsonx values (nil = absent).
func threeWayMerge(current, baseline, proposed any) (any, error) {
	var encoded [3][]byte
	for index, value := range []any{current, baseline, proposed} {
		data, err := jsonx.Compact(value)
		if err != nil {
			return nil, err
		}
		encoded[index] = data
	}
	merged, changed, err := MergeJSON(encoded[0], encoded[1], encoded[2])
	if err != nil {
		return nil, err
	}
	if !changed {
		return jsonx.DeepCopy(current), nil
	}
	return jsonx.Parse(merged)
}
