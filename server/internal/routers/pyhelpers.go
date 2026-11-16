package routers

import (
	"math"
	"math/big"
	"strings"

	"configflow/internal/jsonx"
)

// Python semantics on jsonx values used by the router modules.

// typeName is type(v).__name__.
func typeName(v any) string {
	switch value := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case *jsonx.Object:
		if value == nil {
			return "NoneType"
		}
		return "dict"
	case jsonx.Number:
		if value.IsInt() {
			return "int"
		}
		return "float"
	}
	return "object"
}

// orEmpty is `v or ''` followed by str(): "" for falsy values.
func orEmptyStr(v any) string {
	if !jsonx.Truthy(v) {
		return ""
	}
	return jsonx.Str(v)
}

// pyIter is iter(v) for a truthy v as `for x in v` sees it: list items, dict
// keys, the characters of a str; other values raise TypeError.
func pyIter(v any) ([]any, error) {
	switch value := v.(type) {
	case []any:
		return value, nil
	case *jsonx.Object:
		keys := []any{}
		for _, k := range value.Keys() {
			keys = append(keys, k)
		}
		return keys, nil
	case string:
		out := []any{}
		for _, r := range value {
			out = append(out, string(r))
		}
		return out, nil
	}
	return nil, pyErr("TypeError", "'"+typeName(v)+"' object is not iterable")
}

// iterOr is `v or []` iterated.
func iterOr(v any) ([]any, error) {
	if !jsonx.Truthy(v) {
		return nil, nil
	}
	return pyIter(v)
}

// hashKey is a map key with Python's hash/eq for hashable jsonx values (1,
// 1.0 and True are one key); lists and dicts raise TypeError.
func hashKey(v any) (string, error) {
	switch value := v.(type) {
	case nil:
		return "N", nil
	case bool:
		if value {
			return "n:1", nil
		}
		return "n:0", nil
	case string:
		return "s:" + value, nil
	case jsonx.Number:
		if value.IsInt() {
			i, _ := value.BigInt()
			return "n:" + i.String(), nil
		}
		f := value.Float64()
		if !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f) {
			i, _ := new(big.Float).SetFloat64(f).Int(nil)
			return "n:" + i.String(), nil
		}
		return "f:" + value.String(), nil
	case []any:
		return "", pyErr("TypeError", "unhashable type: 'list'")
	case *jsonx.Object:
		if value == nil {
			return "N", nil
		}
		return "", pyErr("TypeError", "unhashable type: 'dict'")
	}
	return "", pyErr("TypeError", "unhashable type")
}

// pySet is a Python set of hashable values (insertion order kept).
type pySet struct {
	keys  map[string]bool
	items []any
}

func newPySet() *pySet { return &pySet{keys: map[string]bool{}} }

func (s *pySet) add(v any) error {
	k, err := hashKey(v)
	if err != nil {
		return err
	}
	if !s.keys[k] {
		s.keys[k] = true
		s.items = append(s.items, v)
	}
	return nil
}

func (s *pySet) has(v any) (bool, error) {
	k, err := hashKey(v)
	if err != nil {
		return false, err
	}
	return s.keys[k], nil
}

// setOf is set(iterable) over `v or []`.
func setOf(v any) (*pySet, error) {
	items, err := iterOr(v)
	if err != nil {
		return nil, err
	}
	s := newPySet()
	for _, item := range items {
		if err := s.add(item); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// mappingOf is `**value` for a dict merge: a dict or TypeError.
func mappingOf(v any) (*jsonx.Object, error) {
	if o, ok := v.(*jsonx.Object); ok && o != nil {
		return o, nil
	}
	return nil, pyErr("TypeError", "'"+typeName(v)+"' object is not a mapping")
}

// getAttr is `value.get(key)` where Python would raise AttributeError for a
// non-dict.
func getOf(v any, key string) (any, error) {
	if o, ok := v.(*jsonx.Object); ok && o != nil {
		return o.Value(key), nil
	}
	return nil, pyErr("AttributeError", "'"+typeName(v)+"' object has no attribute 'get'")
}

// isTrue is `value is True`.
func isTrue(v any) bool { b, ok := v.(bool); return ok && b }

// isFalse is `value is False`.
func isFalse(v any) bool { b, ok := v.(bool); return ok && !b }

// stringsOf returns []any of strings.
func anyStrings(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

func joinStrings(list []string, sep string) string { return strings.Join(list, sep) }
