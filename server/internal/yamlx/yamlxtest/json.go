// Package yamlxtest supplies structural comparison for YAML reader migration
// tests. It is not a runtime YAML or JSON adapter.
package yamlxtest

import (
	"bytes"
	"encoding/json"
	"iter"
	"math/big"
	"reflect"

	"configflow/internal/testjson"
)

type jsonObject interface {
	Len() int
	Get(string) (any, bool)
	All() iter.Seq2[string, any]
}

type legacyNumber interface {
	Int64() (int64, bool)
}

func numberText(v any) (string, bool) {
	switch n := v.(type) {
	case json.Number:
		return string(n), true
	case legacyNumber:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.String {
			return rv.String(), true
		}
	}
	return "", false
}

func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

func asObjectMap(v any) (map[string]any, bool) {
	switch obj := v.(type) {
	case map[string]any:
		return obj, true
	case *testjson.Object:
		if obj == nil {
			return nil, false
		}
		raw, err := json.Marshal(obj)
		if err != nil {
			return nil, false
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return nil, false
		}
		return m, m != nil
	}
	return nil, false
}

// EqualJSON compares JSON values structurally. Object order and number spelling
// are immaterial, but strings, booleans, nulls and numbers never coerce to one
// another. Array order and every object member remain significant.
func EqualJSON(a, b any) bool {
	if jsonNull(a) || jsonNull(b) {
		return jsonNull(a) && jsonNull(b)
	}
	if ax, ok := numberText(a); ok {
		by, ok := numberText(b)
		if !ok {
			return false
		}
		xr, xok := new(big.Rat).SetString(ax)
		yr, yok := new(big.Rat).SetString(by)
		if xok && yok {
			return xr.Cmp(yr) == 0
		}
		return ax == by
	}
	if x, ok := a.(jsonObject); ok {
		if y, ok := b.(jsonObject); ok {
			if x.Len() != y.Len() {
				return false
			}
			for key, value := range x.All() {
				other, ok := y.Get(key)
				if !ok || !EqualJSON(value, other) {
					return false
				}
			}
			return true
		}
		if y, ok := asObjectMap(b); ok {
			if x.Len() != len(y) {
				return false
			}
			for key, value := range x.All() {
				other, ok := y[key]
				if !ok || !EqualJSON(value, other) {
					return false
				}
			}
			return true
		}
		return false
	}
	if x, ok := asObjectMap(a); ok {
		if y, ok := b.(jsonObject); ok {
			return EqualJSON(y, x)
		}
		y, ok := asObjectMap(b)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			other, ok := y[key]
			if !ok || !EqualJSON(value, other) {
				return false
			}
		}
		return true
	}
	switch x := a.(type) {
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !EqualJSON(x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

func jsonNull(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case []any:
		return x == nil
	case map[string]any:
		return x == nil
	case *testjson.Object:
		return x == nil
	case jsonObject:
		return isNilValue(x)
	}
	return false
}
