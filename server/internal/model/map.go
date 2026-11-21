package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"iter"
	"reflect"
	"slices"
)

type mapEntry[V any] struct {
	name              string
	value             V
	baseline          []byte
	invalid, original bool
}
type mapState[V any] struct {
	source  jsonObject
	entries []mapEntry[V]
	null    bool
}

// Map is a JSON object keyed by user data. It retains member order and original
// bytes for unchanged values. Set/Delete use copy-on-write state.
type Map[V any] struct{ state *mapState[V] }

// Len returns the number of members.
func (m Map[V]) Len() int {
	if m.state == nil {
		return 0
	}
	return len(m.state.entries)
}

// Keys returns the member names in order, independently owned by caller.
func (m Map[V]) Keys() []string {
	keys := make([]string, 0, m.Len())
	if m.state != nil {
		for _, entry := range m.state.entries {
			keys = append(keys, entry.name)
		}
	}
	return keys
}

// Get returns a value by name. References within V retain ordinary Go semantics.
func (m Map[V]) Get(name string) (V, bool) {
	if m.state != nil {
		for _, entry := range m.state.entries {
			if entry.name == name {
				return entry.value, true
			}
		}
	}
	var zero V
	return zero, false
}

// All visits entries in stored order.
func (m Map[V]) All() iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		if m.state != nil {
			for _, entry := range m.state.entries {
				if !yield(entry.name, entry.value) {
					return
				}
			}
		}
	}
}

// Set replaces a value without moving its key, or appends a new member.
func (m *Map[V]) Set(name string, value V) {
	state := m.copyState()
	state.null = false
	for i := range state.entries {
		if state.entries[i].name == name {
			state.entries[i].value = value
			m.state = state
			return
		}
	}
	state.entries = append(state.entries, mapEntry[V]{name: name, value: value})
	m.state = state
}

// SetAll is Set for every pair of entries in order, copying the state once
// instead of once per pair. An empty sequence leaves m unchanged.
func (m *Map[V]) SetAll(entries iter.Seq2[string, V]) {
	var state *mapState[V]
	var index map[string]int
	for name, value := range entries {
		if state == nil {
			state = m.copyState()
			state.null = false
			index = make(map[string]int, len(state.entries))
			for i, entry := range state.entries {
				if _, seen := index[entry.name]; !seen {
					index[entry.name] = i
				}
			}
		}
		if i, ok := index[name]; ok {
			state.entries[i].value = value
			continue
		}
		index[name] = len(state.entries)
		state.entries = append(state.entries, mapEntry[V]{name: name, value: value})
	}
	if state != nil {
		m.state = state
	}
}

// Delete removes a key. Re-adding it appends a new member.
func (m *Map[V]) Delete(name string) bool {
	if m.state != nil {
		for i, entry := range m.state.entries {
			if entry.name == name {
				state := m.copyState()
				state.entries = slices.Delete(state.entries, i, i+1)
				state.null = false
				m.state = state
				return true
			}
		}
	}
	return false
}
func (m Map[V]) copyState() *mapState[V] {
	if m.state == nil {
		return &mapState[V]{}
	}
	return &mapState[V]{source: m.state.source, entries: slices.Clone(m.state.entries), null: m.state.null}
}

// UnmarshalJSON decodes ordered values, retaining invalid values as raw JSON
// with a zero typed value and a diagnostic available through document decoding.
func (m *Map[V]) UnmarshalJSON(data []byte) error {
	if err := storedType(reflect.TypeFor[V]()); err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		m.state = &mapState[V]{null: true}
		return nil
	}
	source, err := parseObject(data)
	if err != nil {
		return err
	}
	state := &mapState[V]{source: source}
	for _, member := range source.members {
		var value V
		err := decodeValue(member.value, &value)
		invalid := err != nil
		if invalid {
			var zero V
			value = zero
		}
		baseline, err := encodeUnit(value)
		if err != nil {
			return err
		}
		state.entries = append(state.entries, mapEntry[V]{member.name, value, baseline, invalid, true})
	}
	m.state = state
	return nil
}

// MarshalJSON writes keys in order and retains unchanged original fragments.
// A zero Map is an empty object; only a stored null that was not modified
// stays null.
func (m Map[V]) MarshalJSON() ([]byte, error) {
	if m.state == nil {
		return []byte("{}"), nil
	}
	if m.state.null {
		return []byte("null"), nil
	}
	originals := map[string]jsonMember{}
	for _, member := range m.state.source.members {
		originals[member.name] = member
	}
	members := make([]jsonMember, 0, m.Len())
	for _, entry := range m.state.entries {
		encoded, err := encodeUnit(entry.value)
		if err != nil {
			return nil, err
		}
		member := jsonMember{name: entry.name}
		if entry.original {
			member = originals[entry.name]
			if (entry.invalid && isZero(reflect.ValueOf(entry.value))) || (!entry.invalid && bytes.Equal(encoded, entry.baseline)) {
				encoded = member.value
			}
		}
		member.value = encoded
		members = append(members, member)
	}
	return objectJSON(m.state.source, members)
}
func (m Map[V]) fieldIssues(prefix string) []FieldIssue {
	var issues []FieldIssue
	if m.state != nil {
		for i, entry := range m.state.entries {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			if entry.invalid {
				issues = append(issues, FieldIssue{path, IssueType})
			} else {
				issues = append(issues, valueIssues(reflect.ValueOf(entry.value), path)...)
			}
		}
	}
	return issues
}
func (m Map[V]) walkValues(visit func(reflect.Value) error) error {
	if m.state != nil {
		for _, entry := range m.state.entries {
			if err := visit(reflect.ValueOf(entry.value)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m Map[V]) storedValueType() reflect.Type { return reflect.TypeFor[V]() }

// EntryValid reports whether a present map value has a valid top-level shape.
// Nested R5 fields still require original-member evidence. An unchanged invalid
// token remains invalid even when its typed fallback is zero; replacing it with
// a nonzero value follows MarshalJSON's repair rules.
func (m Map[V]) EntryValid(name string) bool {
	if m.state != nil {
		for _, entry := range m.state.entries {
			if entry.name == name {
				return !entry.invalid || !isZero(reflect.ValueOf(entry.value))
			}
		}
	}
	return false
}

// MemberJSON returns one owned effective value without encoding other entries.
// Compatibility text/number projections retain their original token types.
func (m Map[V]) MemberJSON(name string) (json.RawMessage, bool, error) {
	if m.state != nil {
		for _, entry := range m.state.entries {
			if entry.name != name {
				continue
			}
			encoded, err := Marshal(entry.value)
			if err != nil {
				return nil, false, err
			}
			if entry.original && (entry.invalid && isZero(reflect.ValueOf(entry.value)) || !entry.invalid && bytes.Equal(encoded, entry.baseline)) {
				for _, original := range m.state.source.members {
					if original.name == name {
						return bytes.Clone(original.value), true, nil
					}
				}
			}
			return encoded, true, nil
		}
	}
	return nil, false, nil
}
