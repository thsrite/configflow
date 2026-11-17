package store

// Legacy fixture adapters only. Production store tests also exercise the raw
// API directly in raw_test.go. No decoding or schema logic lives in store.
import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"configflow/internal/jsonx"
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

// The old corpus is authored as whole ordered documents. This test-only codec
// constructs their JSON text directly; it does not copy repo's byte-preserving
// bridge or decide whether a field changed. The assertions compare real SQL rows.
func legacyDecode(raw *Document) (*jsonx.Object, error) {
	object := func(fields []Field) json.RawMessage {
		var out bytes.Buffer
		out.WriteByte('{')
		for i, field := range fields {
			if i > 0 {
				out.WriteByte(',')
			}
			key, _ := json.Marshal(field.Name)
			out.Write(key)
			out.WriteByte(':')
			out.Write(field.Value)
		}
		out.WriteByte('}')
		return out.Bytes()
	}
	byName := map[string]json.RawMessage{}
	for _, field := range raw.Root {
		byName[field.Name] = field.Value
	}
	if raw.System != nil {
		byName["system"] = object(raw.System.Fields)
	}
	if raw.Shared != nil {
		byName["shared"] = object(raw.Shared.Fields)
	}
	profiles := []Field{}
	for _, profile := range raw.Profiles {
		profiles = append(profiles, Field{profile.ID, object(profile.Fields)})
	}
	byName["profiles"] = object(profiles)
	fields := []Field{}
	for _, key := range raw.Order {
		fields = append(fields, Field{key, byName[key]})
	}
	value, err := jsonx.Parse(object(fields))
	if err != nil {
		return nil, err
	}
	return value.(*jsonx.Object), nil
}

func legacyEncode(doc *jsonx.Object, _ *Document) (*Document, error) {
	if doc == nil {
		return nil, newError(ErrDocument, msgDocumentObject)
	}
	fields := func(value any, scope, owner string) ([]Field, error) {
		section, ok := jsonx.AsObject(value)
		if !ok {
			return nil, newError(ErrDocument, fmt.Sprintf("配置节 %s/%s 必须是对象", scope, owner))
		}
		result := []Field{}
		for key, v := range section.All() {
			text, err := jsonx.Compact(v)
			if err != nil {
				return nil, err
			}
			result = append(result, Field{key, text})
		}
		return result, nil
	}
	raw := &Document{Order: doc.Keys()}
	for key, value := range doc.All() {
		switch key {
		case "system", "shared":
			f, err := fields(value, key, "")
			if err != nil {
				return nil, err
			}
			if key == "system" {
				raw.System = &Section{f}
			} else {
				raw.Shared = &Section{f}
			}
		case "profiles":
			profiles, ok := jsonx.AsObject(value)
			if !ok {
				return nil, newError(ErrDocument, "profiles 必须是对象")
			}
			for id, p := range profiles.All() {
				f, err := fields(p, "profile", id)
				if err != nil {
					return nil, err
				}
				raw.Profiles = append(raw.Profiles, ProfileRows{id, f})
			}
		default:
			text, err := jsonx.Compact(value)
			if err != nil {
				return nil, err
			}
			raw.Root = append(raw.Root, Field{key, text})
		}
	}
	return raw, nil
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
