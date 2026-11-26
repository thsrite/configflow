package repo

import (
	"encoding/json"

	"configflow/internal/model"
)

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeObjectsJSON overlays object members recursively, retaining opaque member
// bytes and the base order. Arrays and scalars are replaced as complete values.
func mergeObjectsJSON(base, override json.RawMessage) (json.RawMessage, error) {
	var left, right model.Fields
	if err := left.UnmarshalJSON(base); err != nil {
		return nil, err
	}
	if err := right.UnmarshalJSON(override); err != nil {
		return nil, err
	}
	for key, incoming := range right.All() {
		current, present := left.Get(key)
		if present && jsonKind(current) == '{' && jsonKind(incoming) == '{' {
			var err error
			incoming, err = mergeObjectsJSON(current, incoming)
			if err != nil {
				return nil, err
			}
		}
		if err := left.Set(key, incoming); err != nil {
			return nil, err
		}
	}
	return left.MarshalJSON()
}
