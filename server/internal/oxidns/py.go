package oxidns

import (
	"math/big"
	"strconv"
	"strings"
	"unsafe"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/yamlx"
)

// Python-semantics glue. The package is written like the Python modules it
// ports: an exception Python raises (OxidnsSettingsError, or a crash such as
// AttributeError/TypeError/KeyError on a malformed value) is raised as
// panic(raised{err}) and turned back into the returned error by catch in the
// exported functions. Crashes are *convert.PyError with the Python class and
// message.

type raised struct{ err error }

func raise(err error) { panic(raised{err}) }

// fail raises OxidnsSettingsError(message).
func fail(message string) { raise(settingsError(message)) }

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

// must raises err when it is not nil.
func must(err error) {
	if err != nil {
		raise(err)
	}
}

func pyErr(class, message string) error { return &convert.PyError{Class: class, Message: message} }

// typeName is the type name Python's messages show.
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

// isDict is isinstance(v, dict).
func isDict(v any) bool {
	switch x := v.(type) {
	case *jsonx.Object:
		return x != nil
	case *yamlx.Mapping:
		return x != nil
	}
	return false
}

// asObject returns v as a dict with string keys (nil when v is not one).
func asObject(v any) *jsonx.Object {
	o, _ := v.(*jsonx.Object)
	return o
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
		if x != nil {
			if value, ok := x.Get(key); ok {
				return value
			}
			return def
		}
	}
	raise(attributeError(v, "get"))
	return nil
}

// has is `key in v` for a dict v (TypeError otherwise is not needed here:
// callers check the type first).
func has(v any, key string) bool {
	switch x := v.(type) {
	case *jsonx.Object:
		return x.Has(key)
	case *yamlx.Mapping:
		_, ok := x.Get(key)
		return ok
	}
	return false
}

// item is v[key] on a dict.
func item(v any, key string) any {
	switch x := v.(type) {
	case *jsonx.Object:
		if x != nil {
			value, ok := x.Get(key)
			if !ok {
				raise(pyErr("KeyError", jsonx.Repr(key)))
			}
			return value
		}
	case *yamlx.Mapping:
		value, ok := x.Get(key)
		if !ok {
			raise(pyErr("KeyError", jsonx.Repr(key)))
		}
		return value
	case []any:
		raise(pyErr("TypeError", "list indices must be integers or slices, not str"))
	case string:
		raise(pyErr("TypeError", "string indices must be integers, not 'str'"))
	}
	raise(pyErr("TypeError", "'"+typeName(v)+"' object is not subscriptable"))
	return nil
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

// isStr is isinstance(v, str).
func isStr(v any) bool {
	_, ok := v.(string)
	return ok
}

// isInt is isinstance(v, int) and not a bool (type(v) is int for the jsonx
// model).
func isInt(v any) bool {
	n, ok := v.(jsonx.Number)
	return ok && n.IsInt()
}

// bigOf returns the value of an int Number.
func bigOf(n jsonx.Number) *big.Int {
	b, _ := n.BigInt()
	return b
}

// strIn is `v in (choices...)` for string choices (== never matches another
// type).
func strIn(v any, choices ...string) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, c := range choices {
		if c == s {
			return true
		}
	}
	return false
}

// strip is v.strip() (AttributeError for a non-string).
func strip(v any) string {
	s, ok := v.(string)
	if !ok {
		raise(attributeError(v, "strip"))
	}
	return pystr.Strip(s)
}

// pySplit is str.split() (runs of Unicode whitespace).
func pySplit(s string) []string { return strings.FieldsFunc(s, pystr.IsSpace) }

// splitlinesKeepEnds is str.splitlines(keepends=True).
func splitlinesKeepEnds(s string) []string {
	var lines []string
	runes := []rune(s)
	start := 0
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			end := i + 1
			if runes[i] == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
				end = i + 2
				i++
			}
			lines = append(lines, string(runes[start:end]))
			start = end
		}
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

// hasControl reports any(ord(c) < 32 for c in s).
func hasControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 32 {
			return true
		}
	}
	return false
}

func itoa(i int) string { return strconv.Itoa(i) }

// dedupe is list(dict.fromkeys(items)) for strings.
func dedupe(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, s := range items {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// stringList returns v as []string when v is a list of strings.
func stringList(v any) ([]string, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(list))
	for i, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

// deepCopy is copy.deepcopy for loaded values: objects referenced twice
// stay shared inside the copy (the memo), as in Python, so yaml.safe_dump
// still writes the anchor.
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
	}
	return v
}

// copyValue is copy.deepcopy(v) as one call.
func copyValue(v any) any { return deepCopy(v, map[unsafe.Pointer]any{}) }
