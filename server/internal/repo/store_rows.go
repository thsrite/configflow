package repo

// Temporary N8-2 boundary. N8-3 replaces this with model's record codec.
// The store itself has no dependency on jsonx or the document's schema.

import (
	"bytes"
	"encoding/json"
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/store"
)

type storedEntry struct {
	store.Entry
	Document *jsonx.Object
}

func decodeEntry(raw store.Entry) (storedEntry, error) {
	doc, err := decodeRows(raw.Document)
	return storedEntry{Entry: raw, Document: doc}, err
}

func decodeRows(raw *store.Document) (*jsonx.Object, error) {
	if raw == nil {
		return nil, fmt.Errorf("decode store rows: missing document")
	}
	section := func(fields []store.Field) (*jsonx.Object, error) {
		out := jsonx.NewObject()
		for _, field := range fields {
			value, err := jsonx.Parse(field.Value)
			if err != nil {
				return nil, fmt.Errorf("decode store field %s: %w", field.Name, err)
			}
			out.Set(field.Name, value)
		}
		return out, nil
	}
	root, err := section(raw.Root)
	if err != nil {
		return nil, err
	}
	out := jsonx.NewObject()
	for _, key := range raw.Order {
		switch key {
		case "system", "shared":
			sec := raw.System
			if key == "shared" {
				sec = raw.Shared
			}
			if sec == nil {
				return nil, fmt.Errorf("decode store section %s: missing section", key)
			}
			value, err := section(sec.Fields)
			if err != nil {
				return nil, err
			}
			out.Set(key, value)
		case "profiles":
			profiles := jsonx.NewObject()
			for _, p := range raw.Profiles {
				value, err := section(p.Fields)
				if err != nil {
					return nil, err
				}
				profiles.Set(p.ID, value)
			}
			out.Set(key, profiles)
		default:
			out.Set(key, root.Value(key))
		}
	}
	return out, nil
}

func encodeRows(doc *jsonx.Object, base *store.Document) (*store.Document, error) {
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
				return nil, err
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
			return nil, err
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

func (r *Repository) commitStore(base storedEntry, doc, meta *jsonx.Object) (storedEntry, bool, error) {
	raw, err := encodeRows(doc, base.Entry.Document)
	if err != nil {
		return storedEntry{}, false, err
	}
	extra, err := encodeMeta(meta)
	if err != nil {
		return storedEntry{}, false, err
	}
	entry, changed, err := r.store.Commit(base.Entry, raw, extra)
	if err != nil || !changed {
		return storedEntry{}, changed, err
	}
	result, err := decodeEntry(entry)
	return result, changed, err
}
