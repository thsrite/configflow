package check

import (
	"math"
	"math/big"
	"sort"
	"strings"

	"configflow/internal/jsonx"
)

// PyError is an exception Python raises on malformed input (a stored file
// edited by hand, an Agent answering unexpected shapes): Class is the Python
// exception class, Message is str(exc) (approximate where Python renders
// objects).
type PyError struct{ Class, Message string }

func (e *PyError) Error() string { return e.Message }

// PythonClass is the Python exception class (urlx convention).
func (e *PyError) PythonClass() string { return e.Class }

func raise(class, message string) { panic(&PyError{class, message}) }

// catch turns a raised *PyError into the function's error.
func catch(err *error) {
	if p := recover(); p != nil {
		if e, ok := p.(*PyError); ok {
			*err = e
			return
		}
		if e, ok := p.(*KeyError); ok {
			*err = e
			return
		}
		panic(p)
	}
}

// TypeName is type(v).__name__ for jsonx values.
func TypeName(v any) string {
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
		return "dict"
	case jsonx.Number:
		if value.IsInt() {
			return "int"
		}
		return "float"
	}
	return "object"
}

// get is v.get(key, fallback) on a dict; anything else raises the
// AttributeError Python would.
func get(v any, key string, fallback any) any {
	object, ok := v.(*jsonx.Object)
	if !ok || object == nil {
		raise("AttributeError", "'"+TypeName(v)+"' object has no attribute 'get'")
	}
	if value, ok := object.Get(key); ok {
		return value
	}
	return fallback
}

// item is v[key] on a dict (KeyError when missing, TypeError for other
// subscriptable types).
func item(v any, key string) any {
	switch value := v.(type) {
	case *jsonx.Object:
		if value != nil {
			if found, ok := value.Get(key); ok {
				return found
			}
			panic(&KeyError{Key: key})
		}
	case []any:
		raise("TypeError", "list indices must be integers or slices, not str")
	case string:
		raise("TypeError", "string indices must be integers, not 'str'")
	}
	raise("TypeError", "'"+TypeName(v)+"' object is not subscriptable")
	return nil
}

// hashKey is the dict/set key of a hashable value; unhashable values raise
// the TypeError Python would. Equal numbers and bools share a key (1 == 1.0
// == True).
func hashKey(v any) string {
	switch value := v.(type) {
	case []any:
		raise("TypeError", "unhashable type: 'list'")
	case *jsonx.Object:
		raise("TypeError", "unhashable type: 'dict'")
	case string:
		return "s" + value
	case nil:
		return "n"
	case bool:
		if value {
			return "i1"
		}
		return "i0"
	case jsonx.Number:
		if value.IsInt() {
			return "i" + strings.TrimPrefix(value.String(), "+")
		}
		f := value.Float64()
		if f == math.Trunc(f) && !math.IsInf(f, 0) {
			i, _ := new(big.Float).SetFloat64(f).Int(nil)
			return "i" + i.String()
		}
		return "f" + value.String()
	}
	return "?"
}

// iterate is iter(v) for the values Python can iterate: lists, the
// characters of a str, the keys of a dict. Anything else raises TypeError.
func iterate(v any) []any {
	switch value := v.(type) {
	case []any:
		return value
	case string:
		out := make([]any, 0, len(value))
		for _, r := range value {
			out = append(out, string(r))
		}
		return out
	case *jsonx.Object:
		out := make([]any, 0, value.Len())
		for key := range value.All() {
			out = append(out, key)
		}
		return out
	}
	raise("TypeError", "'"+TypeName(v)+"' object is not iterable")
	return nil
}

// contains is `needle in container` for a str needle.
func contains(container any, needle string) bool {
	switch value := container.(type) {
	case string:
		return strings.Contains(value, needle)
	case []any:
		for _, element := range value {
			if jsonx.Equal(element, needle) {
				return true
			}
		}
		return false
	case *jsonx.Object:
		return value.Has(needle)
	}
	raise("TypeError", "argument of type '"+TypeName(container)+"' is not iterable")
	return false
}

// sortedJoin is sep.join(sorted(v)).
func sortedJoin(sep string, v any) string {
	items := iterate(v)
	texts := make([]string, len(items))
	for i, element := range items {
		text, ok := element.(string)
		if !ok {
			for _, other := range items {
				if _, isText := other.(string); isText {
					raise("TypeError", "'<' not supported between instances of 'str' and '"+TypeName(element)+"'")
				}
			}
			raise("TypeError", "sequence item 0: expected str instance, "+TypeName(element)+" found")
		}
		texts[i] = text
	}
	sort.Strings(texts)
	return strings.Join(texts, sep)
}
