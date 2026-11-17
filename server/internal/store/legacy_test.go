package store

// Legacy fixture adapters only. Production store tests also exercise the raw
// API directly in raw_test.go. No decoding or schema logic lives in store.
import (
	"bytes"
	"configflow/internal/jsonx"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

type legacyEntry struct {
	Entry
	Document *jsonx.Object
}
type legacyStore struct{ *Store }

func legacyNew(path string, timeout func() time.Duration) *legacyStore {
	return &legacyStore{New(path, timeout)}
}
func legacyEntryOf(raw Entry, err error) (legacyEntry, error) {
	if err != nil {
		return legacyEntry{}, err
	}
	doc, err := legacyDecode(raw.Document)
	return legacyEntry{raw, doc}, err
}
func (s *legacyStore) Load() (legacyEntry, error) { return legacyEntryOf(s.Store.Load()) }
func (s *legacyStore) Create(doc, meta *jsonx.Object) (legacyEntry, error) {
	raw, err := legacyEncode(doc, nil)
	if err != nil {
		return legacyEntry{}, err
	}
	extra, err := legacyMeta(meta)
	if err != nil {
		return legacyEntry{}, err
	}
	return legacyEntryOf(s.Store.Create(raw, extra))
}
func (s *legacyStore) Commit(base legacyEntry, doc, meta *jsonx.Object) (legacyEntry, bool, error) {
	raw, err := legacyEncode(doc, base.Entry.Document)
	if err != nil {
		return legacyEntry{}, false, err
	}
	extra, err := legacyMeta(meta)
	if err != nil {
		return legacyEntry{}, false, err
	}
	entry, changed, err := s.Store.Commit(base.Entry, raw, extra)
	if err != nil || !changed {
		return legacyEntry{}, changed, err
	}
	result, err := legacyEntryOf(entry, nil)
	return result, changed, err
}
func (s *legacyStore) Meta(key string) (any, error) {
	raw, err := s.Store.Meta(key)
	if err != nil || raw == nil {
		return nil, err
	}
	return jsonx.Parse(raw)
}
func (s *legacyStore) SalvageMeta() *jsonx.Object {
	raw := s.Store.SalvageMeta()
	if raw == nil {
		return nil
	}
	out := jsonx.NewObject()
	keys := []string{}
	for key := range raw {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := raw[key]
		v, err := jsonx.Parse(value)
		if err != nil {
			return nil
		}
		out.Set(key, v)
	}
	return out
}
func (s *legacyStore) BuildTemporary(doc, meta *jsonx.Object) (string, legacyEntry, error) {
	raw, err := legacyEncode(doc, nil)
	if err != nil {
		return "", legacyEntry{}, err
	}
	extra, err := legacyMeta(meta)
	if err != nil {
		return "", legacyEntry{}, err
	}
	path, entry, err := s.Store.BuildTemporary(raw, extra)
	result, err := legacyEntryOf(entry, err)
	return path, result, err
}
func legacyDecode(raw *Document) (*jsonx.Object, error) {
	if raw == nil {
		return nil, fmt.Errorf("decode store rows: missing document")
	}
	section := func(fields []Field) (*jsonx.Object, error) {
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

func legacyEncode(doc *jsonx.Object, base *Document) (*Document, error) {
	if doc == nil {
		return nil, &Error{Kind: ErrDocument, Message: "配置文档必须是对象"}
	}
	result := &Document{Order: doc.Keys()}
	// Preserve the original bytes whenever the legacy value did not change.
	// Compare serialized ordered values, rather than Equal (which ignores key order).
	fields := func(object *jsonx.Object, original []Field) ([]Field, error) {
		previous := map[string]json.RawMessage{}
		for _, field := range original {
			previous[field.Name] = field.Value
		}
		out := make([]Field, 0, object.Len())
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
			out = append(out, Field{Name: name, Value: encoded})
		}
		return out, nil
	}
	if base == nil {
		base = &Document{}
	}
	root := jsonx.NewObject()
	for name, value := range doc.All() {
		switch name {
		case "system", "shared":
			object, ok := jsonx.AsObject(value)
			if !ok {
				return nil, &Error{Kind: ErrDocument, Message: fmt.Sprintf("配置节 %s/ 必须是对象", name)}
			}
			var old []Field
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
				result.System = &Section{Fields: encoded}
			} else {
				result.Shared = &Section{Fields: encoded}
			}
		case "profiles":
			profiles, ok := jsonx.AsObject(value)
			if !ok {
				return nil, &Error{Kind: ErrDocument, Message: "profiles 必须是对象"}
			}
			previous := map[string][]Field{}
			for _, p := range base.Profiles {
				previous[p.ID] = p.Fields
			}
			for id, v := range profiles.All() {
				profile, ok := jsonx.AsObject(v)
				if !ok {
					return nil, &Error{Kind: ErrDocument, Message: fmt.Sprintf("配置节 profile/%s 必须是对象", id)}
				}
				encoded, err := fields(profile, previous[id])
				if err != nil {
					return nil, err
				}
				result.Profiles = append(result.Profiles, ProfileRows{ID: id, Fields: encoded})
			}
		default:
			root.Set(name, value)
		}
	}
	var err error
	result.Root, err = fields(root, base.Root)
	return result, err
}

func legacyMeta(meta *jsonx.Object) (map[string]json.RawMessage, error) {
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
