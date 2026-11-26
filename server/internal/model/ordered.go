package model

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"sync"
)

// OrderedMap is a JSON object keyed by user data (deployment IDs,
// verification scopes, file paths) that keeps insertion order: history
// retention ("keep the latest N") and the UI rely on it. It holds plain
// values only, without original bytes (O18 T1b).
//
// Set, SetAll and Delete copy the entry list before changing it, so a shallow
// copy of a record that holds an OrderedMap (a borrowed snapshot) can be
// edited without writing through to the original.
type OrderedMap[V any] struct{ entries []orderedEntry[V] }

type orderedEntry[V any] struct {
	key   string
	value V
}

// Len returns the number of members.
func (m OrderedMap[V]) Len() int { return len(m.entries) }

// IsZero reports an empty map; omitzero members leave it out.
func (m OrderedMap[V]) IsZero() bool { return len(m.entries) == 0 }

// Keys returns the member names in order, owned by the caller.
func (m OrderedMap[V]) Keys() []string {
	keys := make([]string, 0, len(m.entries))
	for _, entry := range m.entries {
		keys = append(keys, entry.key)
	}
	return keys
}

// Get returns a value by name. References within V are shared with the map.
func (m OrderedMap[V]) Get(name string) (V, bool) {
	for _, entry := range m.entries {
		if entry.key == name {
			return entry.value, true
		}
	}
	var zero V
	return zero, false
}

// All visits entries in order.
func (m OrderedMap[V]) All() iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		for _, entry := range m.entries {
			if !yield(entry.key, entry.value) {
				return
			}
		}
	}
}

// Set replaces a value without moving its key, or appends a new member.
func (m *OrderedMap[V]) Set(name string, value V) {
	entries := slices.Clone(m.entries)
	for i := range entries {
		if entries[i].key == name {
			entries[i].value = value
			m.entries = entries
			return
		}
	}
	m.entries = append(entries, orderedEntry[V]{key: name, value: value})
}

// SetAll is Set for every pair in order, copying the entry list once.
func (m *OrderedMap[V]) SetAll(pairs iter.Seq2[string, V]) {
	var entries []orderedEntry[V]
	var index map[string]int
	for name, value := range pairs {
		if index == nil {
			entries = slices.Clone(m.entries)
			index = make(map[string]int, len(entries))
			for i, entry := range entries {
				if _, seen := index[entry.key]; !seen {
					index[entry.key] = i
				}
			}
		}
		if i, ok := index[name]; ok {
			entries[i].value = value
			continue
		}
		index[name] = len(entries)
		entries = append(entries, orderedEntry[V]{key: name, value: value})
	}
	if index != nil {
		m.entries = entries
	}
}

// Delete removes a key; re-adding it appends a new member.
func (m *OrderedMap[V]) Delete(name string) bool {
	for i, entry := range m.entries {
		if entry.key == name {
			entries := slices.Delete(slices.Clone(m.entries), i, i+1)
			if len(entries) == 0 {
				entries = nil
			}
			m.entries = entries
			return true
		}
	}
	return false
}

// MarshalJSON writes the members in order.
func (m OrderedMap[V]) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	for i, entry := range m.entries {
		if i > 0 {
			out.WriteByte(',')
		}
		key, err := scalarJSON(entry.key)
		if err != nil {
			return nil, err
		}
		value, err := encodeUnit(entry.value)
		if err != nil {
			return nil, err
		}
		out.Write(key)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// UnmarshalJSON decodes stored or captured data leniently (DecodeStored;
// issues are not reported). null is an empty map.
func (m *OrderedMap[V]) UnmarshalJSON(data []byte) error {
	var decoded OrderedMap[V]
	if _, err := DecodeStored(data, &decoded, ""); err != nil {
		return err
	}
	*m = decoded
	return nil
}

// UnmarshalJSONFrom decodes the members in order with the decoder's options,
// so the lenient stored-data rules reach the values. A repeated key keeps its
// first position and its last value. Another JSON kind than an object is
// handled by the caller (DecodeStored makes it the zero value plus an issue).
func (m *OrderedMap[V]) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	switch dec.PeekKind() {
	case 'n':
		_, err := dec.ReadToken()
		m.entries = nil
		return err
	case '{':
	default:
		return fmt.Errorf("%w: ordered map must be an object", ErrType)
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	var entries []orderedEntry[V]
	for dec.PeekKind() != '}' {
		token, err := dec.ReadToken()
		if err != nil {
			return err
		}
		key := token.String()
		var value V
		if err := jsonv2.UnmarshalDecode(dec, &value); err != nil {
			return err
		}
		clipSlices(reflect.ValueOf(&value).Elem())
		if i := slices.IndexFunc(entries, func(entry orderedEntry[V]) bool { return entry.key == key }); i >= 0 {
			entries[i].value = value
			continue
		}
		entries = append(entries, orderedEntry[V]{key: key, value: value})
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	m.entries = slices.Clip(entries)
	return nil
}

func (m OrderedMap[V]) orderedValueType() reflect.Type { return reflect.TypeFor[V]() }

// walkValues visits the values for the encoder's cycle check.
func (m OrderedMap[V]) walkValues(visit func(reflect.Value) error) error {
	for _, entry := range m.entries {
		if err := visit(reflect.ValueOf(entry.value)); err != nil {
			return err
		}
	}
	return nil
}

// orderedMap is implemented by every OrderedMap instantiation.
type orderedMap interface{ orderedValueType() reflect.Type }

var orderedMapType = reflect.TypeFor[orderedMap]()

// orderedElem returns the value type of an OrderedMap type, or nil.
func orderedElem(typ reflect.Type) reflect.Type {
	if typ.Kind() != reflect.Struct {
		return nil
	}
	if cached, ok := orderedElemCache.Load(typ); ok {
		elem, _ := cached.(reflect.Type)
		return elem
	}
	var elem reflect.Type
	if typ.Implements(orderedMapType) {
		elem = reflect.Zero(typ).Interface().(orderedMap).orderedValueType()
	}
	orderedElemCache.Store(typ, elem)
	return elem
}

var orderedElemCache sync.Map

// Clone returns a deep copy of v (typically a plain record or a pointer to
// one): its encoding decoded again with the stored-data rules.
func Clone[T any](v T) (T, error) {
	var out T
	data, err := encodeUnit(v)
	if err != nil {
		return out, err
	}
	if _, err := DecodeStored(data, &out, ""); err != nil {
		return out, err
	}
	return out, nil
}

// hasMembers reports whether a plain record (a pointer) holds any value,
// without encoding it.
func hasMembers(rec any) bool {
	value := reflect.ValueOf(rec).Elem()
	for _, field := range memberFields(value.Type()) {
		if !omitted(value.Field(field.index)) {
			return true
		}
	}
	return false
}
