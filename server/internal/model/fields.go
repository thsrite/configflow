package model

import (
	"bytes"
	"encoding/json"
	"iter"
	"slices"
)

type fieldEntry struct {
	name     string
	value    json.RawMessage
	original bool
}
type fieldState struct {
	source  jsonObject
	entries []fieldEntry
}

// Fields is an ordered collection of opaque JSON members. Mutators copy its
// state, so copying a record never lets Fields mutate another record's state.
type Fields struct{ state *fieldState }

// Len returns the number of members.
func (f Fields) Len() int {
	if f.state == nil {
		return 0
	}
	return len(f.state.entries)
}

// Keys returns the member names in stored order, independently owned by caller.
func (f Fields) Keys() []string {
	keys := make([]string, 0, f.Len())
	if f.state != nil {
		for _, entry := range f.state.entries {
			keys = append(keys, entry.name)
		}
	}
	return keys
}

// Get returns an independent copy of a member's JSON.
func (f Fields) Get(name string) (json.RawMessage, bool) {
	entry, ok := f.entry(name)
	return bytes.Clone(entry.value), ok
}

// Has reports whether a member is present, including an explicit null.
func (f Fields) Has(name string) bool { _, ok := f.entry(name); return ok }

// All visits members in order. Returned byte slices belong to the caller.
func (f Fields) All() iter.Seq2[string, json.RawMessage] {
	return func(yield func(string, json.RawMessage) bool) {
		if f.state != nil {
			for _, entry := range f.state.entries {
				if !yield(entry.name, bytes.Clone(entry.value)) {
					return
				}
			}
		}
	}
}

// Set replaces a member without moving it, or appends a new member.
func (f *Fields) Set(name string, value json.RawMessage) error {
	if err := validJSON(value); err != nil {
		return err
	}
	f.set(name, bytes.Clone(value))
	return nil
}

// set is Set for a caller-owned value that is valid by construction
// (encodeUnit output); value is stored without a copy.
func (f *Fields) set(name string, value json.RawMessage) {
	state := f.copyState()
	for i := range state.entries {
		if state.entries[i].name == name {
			state.entries[i].value = value
			f.state = state
			return
		}
	}
	state.entries = append(state.entries, fieldEntry{name: name, value: value})
	f.state = state
}

// Delete removes a member. Deleting and re-adding it appends it as a new member.
func (f *Fields) Delete(name string) bool {
	if f.state == nil {
		return false
	}
	for i, entry := range f.state.entries {
		if entry.name == name {
			state := f.copyState()
			state.entries = slices.Delete(state.entries, i, i+1)
			f.state = state
			return true
		}
	}
	return false
}
func (f Fields) entry(name string) (fieldEntry, bool) {
	if f.state != nil {
		for _, entry := range f.state.entries {
			if entry.name == name {
				return entry, true
			}
		}
	}
	return fieldEntry{}, false
}
func (f Fields) copyState() *fieldState {
	if f.state == nil {
		return &fieldState{}
	}
	return &fieldState{source: f.state.source, entries: slices.Clone(f.state.entries)}
}

// UnmarshalJSON captures every member's original bytes and order.
func (f *Fields) UnmarshalJSON(data []byte) error {
	source, err := parseObject(data)
	if err != nil {
		return err
	}
	state := &fieldState{source: source}
	for _, member := range source.members {
		state.entries = append(state.entries, fieldEntry{member.name, member.value, true})
	}
	f.state = state
	return nil
}

// MarshalJSON preserves all existing member fragments and writes changes only.
func (f Fields) MarshalJSON() ([]byte, error) {
	if f.state == nil {
		return []byte("{}"), nil
	}
	originals := map[string]jsonMember{}
	for _, member := range f.state.source.members {
		originals[member.name] = member
	}
	members := make([]jsonMember, 0, f.Len())
	for _, entry := range f.state.entries {
		member := jsonMember{name: entry.name}
		if entry.original {
			member = originals[entry.name]
		}
		member.value = entry.value
		members = append(members, member)
	}
	return objectJSON(f.state.source, members)
}
