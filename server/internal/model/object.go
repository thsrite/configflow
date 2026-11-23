package model

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"slices"
)

// Fields is a JSON object of opaque member values in their order: a device
// reply passed through to the frontend, a probe result, a member-by-member
// reader of input. Values are kept as given (validated JSON); the object is
// written with its members in order. Mutators copy the member list, so a
// copy of a Fields value never changes when another copy is changed.
type Fields struct{ entries []fieldEntry }

type fieldEntry struct {
	name  string
	value json.RawMessage
}

// Len returns the number of members.
func (f Fields) Len() int { return len(f.entries) }

// IsZero reports an object without members (omitzero leaves it out).
func (f Fields) IsZero() bool { return len(f.entries) == 0 }

// Keys returns the member names in order, owned by the caller.
func (f Fields) Keys() []string {
	keys := make([]string, len(f.entries))
	for i, entry := range f.entries {
		keys[i] = entry.name
	}
	return keys
}

// Get returns an independent copy of a member's JSON.
func (f Fields) Get(name string) (json.RawMessage, bool) {
	if i := f.index(name); i >= 0 {
		return bytes.Clone(f.entries[i].value), true
	}
	return nil, false
}

// Has reports whether a member is present, including an explicit null.
func (f Fields) Has(name string) bool { return f.index(name) >= 0 }

// All visits members in order. Returned byte slices belong to the caller.
func (f Fields) All() iter.Seq2[string, json.RawMessage] {
	return func(yield func(string, json.RawMessage) bool) {
		for _, entry := range f.entries {
			if !yield(entry.name, bytes.Clone(entry.value)) {
				return
			}
		}
	}
}

// Set replaces a member without moving it, or appends a new member. value
// must be one valid JSON value.
func (f *Fields) Set(name string, value json.RawMessage) error {
	if err := validJSON(value); err != nil {
		return err
	}
	entries := slices.Clone(f.entries)
	if i := f.index(name); i >= 0 {
		entries[i].value = bytes.Clone(value)
	} else {
		entries = append(entries, fieldEntry{name: name, value: bytes.Clone(value)})
	}
	f.entries = entries
	return nil
}

// set is Set for a value this package encoded (valid by construction); the
// value is kept without a copy.
func (f *Fields) set(name string, value json.RawMessage) {
	entries := slices.Clone(f.entries)
	if i := f.index(name); i >= 0 {
		entries[i].value = value
	} else {
		entries = append(entries, fieldEntry{name: name, value: value})
	}
	f.entries = entries
}

// Delete removes a member. Deleting and re-adding it appends it as a new member.
func (f *Fields) Delete(name string) bool {
	i := f.index(name)
	if i < 0 {
		return false
	}
	f.entries = slices.Delete(slices.Clone(f.entries), i, i+1)
	return true
}

func (f Fields) index(name string) int {
	return slices.IndexFunc(f.entries, func(entry fieldEntry) bool { return entry.name == name })
}

// UnmarshalJSON reads a JSON object (anything else is ErrType; a repeated
// member name is ErrJSON). Invalid UTF-8 in values reads as U+FFFD.
func (f *Fields) UnmarshalJSON(data []byte) error {
	data = validUTF8(data)
	if err := validJSON(data); err != nil {
		return err
	}
	dec := jsontext.NewDecoder(bytes.NewReader(data))
	if dec.PeekKind() != '{' {
		return fmt.Errorf("%w: expected a JSON object", ErrType)
	}
	if _, err := dec.ReadToken(); err != nil {
		return fmt.Errorf("%w: %v", ErrJSON, err)
	}
	var entries []fieldEntry
	for dec.PeekKind() != '}' {
		token, err := dec.ReadToken()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrJSON, err)
		}
		name := token.String()
		value, err := dec.ReadValue()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrJSON, err)
		}
		entries = append(entries, fieldEntry{name: name, value: bytes.Clone(value)})
	}
	f.entries = entries
	return nil
}

// MarshalJSON writes the members in order.
func (f Fields) MarshalJSON() ([]byte, error) {
	members := make([]StoredMember, len(f.entries))
	for i, entry := range f.entries {
		members[i] = StoredMember{Name: entry.name, Value: entry.value}
	}
	data, err := joinMembers(members)
	if err == nil && tooDeep(data) {
		return nil, ErrJSON
	}
	return data, err
}
