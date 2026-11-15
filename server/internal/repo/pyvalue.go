package repo

import (
	"math"
	"math/big"
	"strconv"
	"sync/atomic"

	"configflow/internal/jsonx"
)

// pyHashKey is a map key with Python's hash/equality semantics for the
// scalar values of a document: None, strings, and numbers where
// 1 == 1.0 == True. ok is false for unhashable values (lists, dicts), on
// which Python raises TypeError ("unhashable type: 'list'") when they are put
// into a set or looked up in one.
func pyHashKey(value any) (key string, ok bool) {
	switch v := value.(type) {
	case nil:
		return "N", true
	case string:
		return "S" + v, true
	case bool:
		if v {
			return "I1", true
		}
		return "I0", true
	case jsonx.Number:
		if v.IsInt() {
			return "I" + v.String(), true
		}
		f := v.Float64()
		switch {
		case math.IsNaN(f):
			// NaN equals nothing, not even another NaN object.
			return "NaN" + strconv.FormatUint(nanCounter.Add(1), 10), true
		case math.IsInf(f, 0) || f != math.Trunc(f):
			return "F" + strconv.FormatFloat(f, 'g', -1, 64), true
		}
		integer, _ := big.NewFloat(f).Int(nil)
		return "I" + integer.String(), true
	}
	return "", false
}

var nanCounter atomic.Uint64

// unhashable is the TypeError Python raises for an unhashable set member or
// dict key.
func unhashable(value any) error {
	return helperError(ClassTypeError, "unhashable type: '"+pythonTypeName(value)+"'")
}

// pythonTypeName is type(value).__name__ for a document value.
func pythonTypeName(value any) string {
	switch v := value.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case *jsonx.Object:
		return "dict"
	case jsonx.Number:
		if v.IsInt() {
			return "int"
		}
		return "float"
	}
	return "object"
}
