package repo

// Temporary legacy mutation adapter around the N8-3 model codec. N8-5 removes
// the legacy intent normalization as repository mutations become typed.
// The store itself has no dependency on jsonx or the document's schema.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/store"
)

type storedEntry struct {
	store.Entry
	Document *jsonx.Object
	Model    *model.Document
	Issues   []model.FieldIssue
}

func decodeEntry(raw store.Entry) (storedEntry, error) {
	document, issues, err := decodeModelRows(raw.Document)
	if err != nil {
		return storedEntry{}, err
	}
	legacy, err := legacyModelDocument(document)
	return storedEntry{Entry: raw, Document: legacy, Model: document, Issues: issues}, err
}

func decodeRows(raw *store.Document) (*jsonx.Object, error) {
	document, _, err := decodeModelRows(raw)
	if err != nil {
		return nil, err
	}
	return legacyModelDocument(document)
}

func legacyModelDocument(document *model.Document) (*jsonx.Object, error) {
	encoded, err := model.Marshal(document)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(encoded)
	if err != nil {
		return nil, err
	}
	object, ok := jsonx.AsObject(value)
	if !ok {
		return nil, fmt.Errorf("decode stored model: root must be an object")
	}
	return object, nil
}

// encodeRows translates legacy caller intent first, then makes the model codec
// the stored-document boundary. N8-5 removes legacyIntentRows with the legacy
// mutation API. In particular, two invalid legacy revisions (true/false) must
// remain distinguishable even though both read as model.Int(0) under R5.
func encodeRows(doc *jsonx.Object, base *store.Document) (*store.Document, error) {
	proposed, err := legacyIntentRows(doc, base)
	if err != nil {
		return nil, err
	}
	document, _, err := decodeModelRows(proposed)
	if err != nil {
		return nil, &store.Error{Kind: store.ErrDocument, Message: err.Error(), Cause: err}
	}
	return encodeModelRows(document, proposed)
}

func legacyIntentRows(doc *jsonx.Object, base *store.Document) (*store.Document, error) {
	if doc == nil {
		return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreDocumentObject}
	}
	result := &store.Document{Order: doc.Keys()}
	// Preserve the original bytes whenever the legacy value did not change.
	// Compare serialized ordered values, rather than Equal (which ignores key order).
	fields := func(object *jsonx.Object, original []store.Field) ([]store.Field, error) {
		previous := map[string]json.RawMessage{}
		for _, field := range original {
			previous[field.Name] = field.Value
		}
		out := make([]store.Field, 0, object.Len())
		for name, value := range object.All() {
			encoded, err := jsonx.Compact(value)
			if err != nil {
				return nil, &store.Error{Kind: store.ErrDocument, Message: err.Error(), Cause: err}
			}
			if raw, ok := previous[name]; ok {
				before, err := jsonx.Parse(raw)
				if err != nil {
					return nil, err
				}
				normalized, err := jsonx.Compact(before)
				if err != nil {
					return nil, err
				}
				if bytes.Equal(encoded, normalized) {
					encoded = raw
				}
			}
			out = append(out, store.Field{Name: name, Value: encoded})
		}
		return out, nil
	}
	if base == nil {
		base = &store.Document{}
	}
	root := jsonx.NewObject()
	for name, value := range doc.All() {
		switch name {
		case "system", "shared":
			object, ok := jsonx.AsObject(value)
			if !ok {
				return nil, &store.Error{Kind: store.ErrDocument, Message: fmt.Sprintf(msgStoreSectionObject, name)}
			}
			var old []store.Field
			prior := base.System
			if name == "shared" {
				prior = base.Shared
			}
			if prior != nil {
				old = prior.Fields
			}
			encoded, err := fields(object, old)
			if err != nil {
				return nil, err
			}
			if name == "system" {
				result.System = &store.Section{Fields: encoded}
			} else {
				result.Shared = &store.Section{Fields: encoded}
			}
		case "profiles":
			profiles, ok := jsonx.AsObject(value)
			if !ok {
				return nil, &store.Error{Kind: store.ErrDocument, Message: msgStoreProfilesObject}
			}
			previous := map[string][]store.Field{}
			for _, p := range base.Profiles {
				previous[p.ID] = p.Fields
			}
			for id, v := range profiles.All() {
				profile, ok := jsonx.AsObject(v)
				if !ok {
					return nil, &store.Error{Kind: store.ErrDocument, Message: fmt.Sprintf(msgStoreProfileObject, id)}
				}
				encoded, err := fields(profile, previous[id])
				if err != nil {
					return nil, err
				}
				result.Profiles = append(result.Profiles, store.ProfileRows{ID: id, Fields: encoded})
			}
		default:
			root.Set(name, value)
		}
	}
	var err error
	result.Root, err = fields(root, base.Root)
	return result, err
}

func encodeMeta(meta *jsonx.Object) (map[string]json.RawMessage, error) {
	result := map[string]json.RawMessage{}
	for key, value := range meta.All() {
		encoded, err := jsonx.Compact(value)
		if err != nil {
			return nil, &store.Error{Kind: store.ErrDocument, Message: err.Error(), Cause: err}
		}
		result[key] = encoded
	}
	return result, nil
}

func (r *Repository) createStore(doc, meta *jsonx.Object) (storedEntry, error) {
	raw, err := encodeRows(doc, nil)
	if err != nil {
		return storedEntry{}, err
	}
	extra, err := encodeMeta(meta)
	if err != nil {
		return storedEntry{}, err
	}
	entry, err := r.store.Create(raw, extra)
	if err != nil {
		return storedEntry{}, err
	}
	return decodeEntry(entry)
}

func (r *Repository) commitStore(ctx context.Context, base storedEntry, doc, meta *jsonx.Object) (storedEntry, bool, error) {
	raw, err := encodeRows(doc, base.Entry.Document)
	if err != nil {
		return storedEntry{}, false, err
	}
	extra, err := encodeMeta(meta)
	if err != nil {
		return storedEntry{}, false, err
	}
	entry, changed, err := r.store.CommitContext(ctx, base.Entry, raw, extra)
	if err != nil || !changed {
		return storedEntry{}, changed, err
	}
	result, err := decodeEntry(entry)
	return result, changed, err
}
