package mosdns

import (
	"math"
	"math/big"
	"strings"
	"unsafe"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/yamlx"
)

// Python-semantics helpers. The converter is written like the Python module
// it ports: an exception Python raises on a malformed value (AttributeError,
// TypeError, KeyError, ValueError ...) is raised as panic(raised{err}) and
// turned back into the returned error by catch in the exported functions.
// The errors are *convert.PyError with the Python class and message.

type raised struct{ err error }

func raise(err error) { panic(raised{err}) }

// catch converts a raised error into *errp; other panics continue.
func catch(errp *error) {
	if r := recover(); r != nil {
		if p, ok := r.(raised); ok {
			*errp = p.err
			return
		}
		panic(r)
	}
}

func pyErr(class, message string) error { return &convert.PyError{Class: class, Message: message} }

// typeName is the type name Python's messages show (tp_name).
func typeName(v any) string {
	switch x := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case *jsonx.Object, *yamlx.Mapping:
		return "dict"
	case jsonx.Number:
		if x.IsInt() {
			return "int"
		}
		return "float"
	case yamlx.Timestamp:
		if !x.HasTime {
			return "datetime.date"
		}
		return "datetime.datetime"
	case []byte:
		return "bytes"
	}
	return "object"
}

func attributeError(v any, attribute string) error {
	return pyErr("AttributeError", "'"+typeName(v)+"' object has no attribute '"+attribute+"'")
}

// get is v.get(key, def): AttributeError when v is not a dict.
func get(v any, key string, def any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		if x != nil {
			if value, ok := x.Get(key); ok {
				return value
			}
			return def
		}
	case *yamlx.Mapping:
		if value, ok := x.Get(key); ok {
			return value
		}
		return def
	}
	raise(attributeError(v, "get"))
	return nil
}

// item is v[key] on a dict.
func item(v any, key string) any {
	obj, ok := v.(*jsonx.Object)
	if !ok || obj == nil {
		raise(pyErr("TypeError", "'"+typeName(v)+"' object is not subscriptable"))
	}
	value, present := obj.Get(key)
	if !present {
		raise(pyErr("KeyError", jsonx.Repr(key)))
	}
	return value
}

// iterate is `for x in v`: list items, dict keys, string characters, bytes.
func iterate(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case *jsonx.Object:
		if x != nil {
			out := make([]any, 0, x.Len())
			for _, k := range x.Keys() {
				out = append(out, k)
			}
			return out
		}
	case *yamlx.Mapping:
		out := make([]any, 0, x.Len())
		for _, pair := range x.Pairs {
			out = append(out, pair.Key)
		}
		return out
	case string:
		out := []any{}
		for _, r := range x {
			out = append(out, string(r))
		}
		return out
	case []byte:
		out := []any{}
		for _, b := range x {
			out = append(out, jsonx.Int(int64(b)))
		}
		return out
	}
	raise(pyErr("TypeError", "'"+typeName(v)+"' object is not iterable"))
	return nil
}

// strip is v.strip() (AttributeError for a non-string).
func strip(v any) string {
	s, ok := v.(string)
	if !ok {
		raise(attributeError(v, "strip"))
	}
	return pystr.Strip(s)
}

// truthy is bool(v), including the YAML-only types.
func truthy(v any) bool {
	switch x := v.(type) {
	case []byte:
		return len(x) > 0
	case *yamlx.Mapping:
		return x.Len() > 0
	case yamlx.Timestamp:
		return true
	}
	return jsonx.Truthy(v)
}

// str is str(v) (also an f-string field).
func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case yamlx.Timestamp:
		return x.String()
	}
	return jsonx.Str(v)
}

// hashKey identifies v as a dict key or set member: values Python considers
// equal (1, 1.0, True) get the same key; unhashable values raise TypeError.
func hashKey(v any) string {
	switch x := v.(type) {
	case nil:
		return "N"
	case bool:
		if x {
			return "n:1"
		}
		return "n:0"
	case string:
		return "s:" + x
	case jsonx.Number:
		if x.IsInt() {
			if n, ok := new(big.Int).SetString(x.String(), 10); ok {
				return "n:" + n.String()
			}
			return "n:" + x.String()
		}
		f := x.Float64()
		if f == math.Trunc(f) && !math.IsInf(f, 0) {
			n, _ := new(big.Float).SetFloat64(f).Int(nil)
			return "n:" + n.String()
		}
		return "f:" + x.String()
	case yamlx.Timestamp:
		return "t:" + x.String()
	case []byte:
		return "b:" + string(x)
	}
	raise(pyErr("TypeError", "unhashable type: '"+typeName(v)+"'"))
	return ""
}

// pySet is a Python set built from an iterable (membership only).
type pySet map[string]bool

func newSet(v any) pySet {
	s := pySet{}
	for _, member := range iterate(v) {
		s[hashKey(member)] = true
	}
	return s
}

func (s pySet) has(v any) bool { return s[hashKey(v)] }

// contains is `needle in container`.
func contains(needle, container any) bool {
	switch c := container.(type) {
	case []any:
		for _, element := range c {
			if sameObject(element, needle) || jsonx.Equal(element, needle) {
				return true
			}
		}
		return false
	case string:
		s, ok := needle.(string)
		if !ok {
			raise(pyErr("TypeError", "'in <string>' requires string as left operand, not "+typeName(needle)))
		}
		return strings.Contains(c, s)
	case *jsonx.Object:
		if c != nil {
			k := hashKey(needle)
			s, ok := needle.(string)
			return ok && strings.HasPrefix(k, "s:") && c.Has(s)
		}
	case *yamlx.Mapping:
		k := hashKey(needle)
		for _, pair := range c.Pairs {
			if hashKey(pair.Key) == k {
				return true
			}
		}
		return false
	}
	raise(pyErr("TypeError", "argument of type '"+typeName(container)+"' is not iterable"))
	return false
}

// sameObject is Python's identity check `a is b` for containers.
func sameObject(a, b any) bool {
	switch x := a.(type) {
	case *jsonx.Object:
		y, ok := b.(*jsonx.Object)
		return ok && x == y && x != nil
	case []any:
		y, ok := b.([]any)
		return ok && len(x) > 0 && len(y) > 0 && unsafe.SliceData(x) == unsafe.SliceData(y) && len(x) == len(y)
	}
	return false
}

// add is `a + b` for the values `direct_rulesets + proxy_rulesets` can hold.
func add(a, b any) any {
	switch x := a.(type) {
	case []any:
		y, ok := b.([]any)
		if !ok {
			raise(pyErr("TypeError", `can only concatenate list (not "`+typeName(b)+`") to list`))
		}
		out := make([]any, 0, len(x)+len(y))
		return append(append(out, x...), y...)
	case string:
		y, ok := b.(string)
		if !ok {
			raise(pyErr("TypeError", `can only concatenate str (not "`+typeName(b)+`") to str`))
		}
		return x + y
	}
	if isNumber(a) && isNumber(b) {
		// The sum is never used except to be iterated (TypeError there).
		if typeName(a) == "float" || typeName(b) == "float" {
			return jsonx.Float(0.5)
		}
		return jsonx.Int(0)
	}
	raise(pyErr("TypeError", "unsupported operand type(s) for +: '"+typeName(a)+"' and '"+typeName(b)+"'"))
	return nil
}

func isNumber(v any) bool {
	switch v.(type) {
	case bool, jsonx.Number:
		return true
	}
	return false
}

// pyInt is int(v).
func pyInt(v any) jsonx.Number {
	switch x := v.(type) {
	case bool:
		if x {
			return jsonx.Int(1)
		}
		return jsonx.Int(0)
	case jsonx.Number:
		if x.IsInt() {
			if n, ok := new(big.Int).SetString(x.String(), 10); ok {
				return jsonx.BigInt(n)
			}
			return x
		}
		f := x.Float64()
		if math.IsNaN(f) {
			raise(pyErr("ValueError", "cannot convert float NaN to integer"))
		}
		if math.IsInf(f, 0) {
			raise(pyErr("OverflowError", "cannot convert float infinity to integer"))
		}
		n, _ := new(big.Float).SetFloat64(math.Trunc(f)).Int(nil)
		return jsonx.BigInt(n)
	case string:
		n, err := pystr.Int(x, 10)
		if err != nil {
			raise(pyErr("ValueError", "invalid literal for int() with base 10: "+jsonx.Repr(x)))
		}
		return jsonx.BigInt(n)
	}
	raise(pyErr("TypeError", "int() argument must be a string, a bytes-like object or a real number, not '"+typeName(v)+"'"))
	return jsonx.Int(0)
}

// joinStrings is '\n'.join(items) (TypeError for a non-string item).
func joinStrings(items []any, sep string) string {
	parts := make([]string, len(items))
	for i, it := range items {
		s, ok := it.(string)
		if !ok {
			raise(pyErr("TypeError", "sequence item "+itoa(i)+": expected str instance, "+typeName(it)+" found"))
		}
		parts[i] = s
	}
	return strings.Join(parts, sep)
}

func itoa(i int) string { return big.NewInt(int64(i)).String() }

// deepCopy is copy.deepcopy for loaded values: objects referenced twice
// stay shared inside the copy (the memo), as in Python.
func deepCopy(v any, memo map[unsafe.Pointer]any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		if x == nil {
			return x
		}
		if done, ok := memo[unsafe.Pointer(x)]; ok {
			return done
		}
		clone := jsonx.NewObject()
		memo[unsafe.Pointer(x)] = clone
		for k, value := range x.All() {
			clone.Set(k, deepCopy(value, memo))
		}
		return clone
	case []any:
		if len(x) == 0 {
			if x == nil {
				return x
			}
			return []any{}
		}
		p := unsafe.Pointer(unsafe.SliceData(x))
		if done, ok := memo[p]; ok {
			if list, ok := done.([]any); ok && len(list) == len(x) {
				return done
			}
		}
		clone := make([]any, len(x))
		memo[p] = clone
		for i, value := range x {
			clone[i] = deepCopy(value, memo)
		}
		return clone
	case *yamlx.Mapping:
		// A dict with keys that are not strings is copied like any dict, so
		// the copy is not an alias of the original in the dump.
		if x == nil {
			return x
		}
		if done, ok := memo[unsafe.Pointer(x)]; ok {
			return done
		}
		clone := &yamlx.Mapping{Pairs: make([]yamlx.Pair, len(x.Pairs))}
		memo[unsafe.Pointer(x)] = clone
		for i, pair := range x.Pairs {
			clone.Pairs[i] = yamlx.Pair{Key: pair.Key, Value: deepCopy(pair.Value, memo)}
		}
		return clone
	case []byte:
		return x
	}
	return v
}
