package provider

import (
	"strconv"

	"configflow/internal/httpx"
	"configflow/internal/jsonx"
)

// pyTypeName is type(v).__name__ for a JSON value.
func pyTypeName(v any) string { return httpx.PyTypeName(v) }

func itoa(n int) string { return strconv.Itoa(n) }

// pyIter is iter(v) for a JSON value: a list, the keys of a dict, the
// characters of a string; anything else is Python's TypeError.
func pyIter(v any) ([]any, error) {
	switch x := v.(type) {
	case []any:
		return x, nil
	case *jsonx.Object:
		keys := make([]any, 0, x.Len())
		for _, k := range x.Keys() {
			keys = append(keys, k)
		}
		return keys, nil
	case string:
		out := []any{}
		for _, r := range x {
			out = append(out, string(r))
		}
		return out, nil
	}
	return nil, typeError("'" + pyTypeName(v) + "' object is not iterable")
}

// hashable reports whether a JSON value can be a set member.
func hashable(v any) bool {
	switch v.(type) {
	case []any, *jsonx.Object:
		return false
	}
	return true
}

// containsEqual is `value in collection` with Python equality.
func containsEqual(collection []any, value any) bool {
	for _, item := range collection {
		if jsonx.Equal(item, value) {
			return true
		}
	}
	return false
}
