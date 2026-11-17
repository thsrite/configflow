// Package yamlxtest supplies structural comparison for YAML reader migration
// tests. It is not a runtime YAML or JSON adapter.
package yamlxtest

import (
	"math/big"
	"reflect"

	"configflow/internal/jsonx"
)

// EqualJSON compares JSON values structurally. Object order and number spelling
// are immaterial, but strings, booleans, nulls and numbers never coerce to one
// another. Array order and every object member remain significant.
func EqualJSON(a, b any) bool {
	if jsonNull(a) || jsonNull(b) {
		return jsonNull(a) && jsonNull(b)
	}
	switch x := a.(type) {
	case *jsonx.Object:
		y, ok := b.(*jsonx.Object)
		if !ok {
			return false
		}
		if x == nil || y == nil {
			return x == nil && y == nil
		}
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
	case jsonx.Number:
		y, ok := b.(jsonx.Number)
		if !ok {
			return false
		}
		xr, xok := new(big.Rat).SetString(string(x))
		yr, yok := new(big.Rat).SetString(string(y))
		if xok && yok {
			return xr.Cmp(yr) == 0
		}
		return x == y
	default:
		return reflect.DeepEqual(a, b)
	}
}

func jsonNull(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case *jsonx.Object:
		return x == nil
	case []any:
		return x == nil
	}
	return false
}
