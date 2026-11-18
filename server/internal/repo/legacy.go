package repo

// Legacy adapter (design n7-n8 2.5): the *jsonx.Object API that consumers
// still use until their own N8-8…N8-10 step migrates them. It holds no logic
// of its own: values cross between the typed core and jsonx by model.Marshal →
// jsonx.Parse and jsonx.Compact → the model codec. Deleted in N8-11.

import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/convert"
	"configflow/internal/health/check"

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

// legacyValue is a model value in its jsonx form.
func legacyValue(value any) (any, error) {
	data, err := model.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}

// legacyObject is a model record (or Config) as a *jsonx.Object.
func legacyObject(value any) (*jsonx.Object, error) {
	converted, err := legacyValue(value)
	if err != nil {
		return nil, err
	}
	object, ok := jsonx.AsObject(converted)
	if !ok {
		return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreDocumentObject}
	}
	return object, nil
}

// sameLegacy reports whether two jsonx values encode identically (member
// order and number text included).
func sameLegacy(a, b any) bool {
	left, err := jsonx.Compact(a)
	if err != nil {
		return false
	}
	right, err := jsonx.Compact(b)
	return err == nil && string(left) == string(right)
}

// setChanged stores the members of updated named in keys that differ from
// view (the value a legacy updater was handed) through set, as stored JSON
// (SetMemberJSON): members the updater did not change keep their stored bytes,
// and a value of the wrong shape is kept for write validation to refuse.
func setChanged(keys []string, view, updated *jsonx.Object, set func(member string, data json.RawMessage) error) error {
	for _, key := range keys {
		value, ok := updated.Get(key)
		if !ok {
			continue
		}
		if before, had := view.Get(key); had && sameLegacy(before, value) {
			continue
		}
		data, err := jsonx.Compact(value)
		if err != nil {
			return err
		}
		if err := set(key, data); err != nil {
			return err
		}
	}
	return nil
}

// setLegacy stores a jsonx value as a record member (SetMemberJSON).
func setLegacy(set func(member string, data json.RawMessage) error, member string, value any) error {
	data, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return set(member, data)
}

// legacyConfig is the jsonx compat view of a profile in a typed document.
func legacyConfig(document *model.Document, profileID string, roots ResourceRoots, agents bool) (*jsonx.Object, error) {
	config, err := resolveConfig(document, profileID, roots, agents)
	if err != nil {
		return nil, err
	}
	return legacyObject(config)
}

// updateProfileLegacy runs a legacy Updater, which edits the compat view, as
// a profile update: the ProfileFields it changed are stored.
func updateProfileLegacy(ctx context.Context, document *model.Document, profileID string, updater Updater) (*model.Profile, error) {
	return updateProfileIn(document, profileID, func(p *model.Profile) error {
		view, err := legacyConfig(document, profileID, nil, true)
		if err != nil {
			return err
		}
		original := view.DeepCopy()
		updated, err := applyUpdater(ctx, view, updater)
		if err != nil {
			return err
		}
		return setChanged(ProfileFields, original, updated, p.SetJSON)
	})
}

// applySharedLegacy runs a legacy Updater, which edits a copy of the shared
// section, as a shared update: the SharedFields it changed are stored.
func applySharedLegacy(ctx context.Context, document *model.Document, updater Updater) (*model.Shared, error) {
	return applySharedIn(document, func(s *model.Shared) error {
		view, err := legacyObject(s)
		if err != nil {
			return err
		}
		original := view.DeepCopy()
		updated, err := applyUpdater(ctx, view, updater)
		if err != nil {
			return err
		}
		return setChanged(SharedFields, original, updated, s.SetJSON)
	})
}

// nodeRawDialer is dialer_references.raw_dialer of a typed node, through
// the jsonx converter until convert reads typed nodes (N8-8a).
func nodeRawDialer(node *model.Node) (any, error) {
	object, err := legacyObject(node)
	if err != nil {
		return nil, err
	}
	return rawDialer(object)
}

// Bridges to the validators of packages that still take jsonx values
// (health/check, convert); they move to typed input with their owners
// (N8-8a/N8-9), and these bridges go with legacy.go.

// checkHealth is health_check.validate_health of a stored value: a
// ValueError is a validation error.
func checkHealth(raw json.RawMessage, provider bool) error {
	value, err := jsonx.Parse(raw)
	if err != nil {
		return err
	}
	err = check.ValidateHealth(value, provider)
	var invalid *check.ValueError
	if errors.As(err, &invalid) {
		return validationError(invalid.Message)
	}
	return err
}

// checkNodeShapes is dialer_references.validate_shapes of the shared nodes.
func checkNodeShapes(nodes json.RawMessage) error {
	value, err := jsonx.Parse(nodes)
	if err != nil {
		return err
	}
	return dialerValidation(convert.ValidateShapes(jsonx.Obj("nodes", value), true))
}

// checkProfileShapes is dialer_references.validate_shapes of a profile.
func checkProfileShapes(profile *model.Profile) error {
	object, err := legacyObject(profile)
	if err != nil {
		return err
	}
	return dialerValidation(convert.ValidateShapes(object, true))
}

// checkDialers is dialer_references.validate_dialers of a resolved profile:
// a broken reference is ErrInUse.
func checkDialers(config *model.Config) error {
	object, err := legacyObject(config)
	if err != nil {
		return err
	}
	err = convert.ValidateDialers(object)
	var reference *convert.DialerReferenceError
	if errors.As(err, &reference) {
		return &Error{Kind: ErrInUse, Message: reference.Message, Usages: []any{}, Cause: err}
	}
	return err
}

func dialerValidation(err error) error {
	var reference *convert.DialerReferenceError
	if errors.As(err, &reference) {
		return &Error{Kind: ErrValidation, Message: reference.Message, Cause: err}
	}
	return err
}
