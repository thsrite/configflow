package jsondecode

import (
	"bytes"
	"encoding/json"
)

// Member is one member of a request DTO decoded as T. Raw is the JSON that
// was sent (nil when the member is absent) and OK reports that it held a
// non-null T. A member of the wrong type never fails the enclosing decode:
// handlers check members in their own order and answer with their own
// message, and a message may quote the rejected value (Raw).
type Member[T any] struct {
	Raw   json.RawMessage
	Value T
	OK    bool
}

// UnmarshalJSON records the sent value; it never returns an error.
func (m *Member[T]) UnmarshalJSON(data []byte) error {
	var value T
	*m = Member[T]{Raw: bytes.Clone(data)}
	if !bytes.Equal(bytes.TrimSpace(data), []byte("null")) && Unmarshal(data, &value) == nil {
		m.Value, m.OK = value, true
	}
	return nil
}

// Present reports whether the member was sent, null included.
func (m Member[T]) Present() bool { return m.Raw != nil }

// Null reports an absent member or an explicit null.
func (m Member[T]) Null() bool {
	return m.Raw == nil || bytes.Equal(bytes.TrimSpace(m.Raw), []byte("null"))
}

// Or returns the value, or fallback when the member is absent. A present
// member of the wrong type yields T's zero value.
func (m Member[T]) Or(fallback T) T {
	if !m.Present() {
		return fallback
	}
	return m.Value
}
