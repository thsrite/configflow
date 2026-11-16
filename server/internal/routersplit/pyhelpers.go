package routersplit

import (
	"unsafe"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// PyError is a Python exception raised where router_split.py does not catch
// it (AttributeError, TypeError, ValueError, ...): Class is the exception
// class, Message str(exc).
type PyError struct{ Class, Message string }

func (e *PyError) Error() string { return e.Message }

// PythonClass is the Python exception class name.
func (e *PyError) PythonClass() string { return e.Class }

func pyTypeName(v any) string {
	switch value := v.(type) {
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
		if value.IsInt() {
			return "int"
		}
		return "float"
	case []byte:
		return "bytes"
	case yamlx.Timestamp:
		if value.HasTime {
			return "datetime"
		}
		return "date"
	}
	return "object"
}

func attributeError(v any, name string) error {
	return &PyError{"AttributeError", "'" + pyTypeName(v) + "' object has no attribute '" + name + "'"}
}

// truthy is Python bool(v) for loaded YAML/JSON values.
func truthy(v any) bool {
	switch value := v.(type) {
	case *yamlx.Mapping:
		return value != nil && value.Len() > 0
	case []byte:
		return len(value) > 0
	case yamlx.Timestamp:
		return true
	}
	return jsonx.Truthy(v)
}

// isDict is isinstance(v, dict).
func isDict(v any) bool {
	switch m := v.(type) {
	case *jsonx.Object:
		return m != nil
	case *yamlx.Mapping:
		return m != nil
	}
	return false
}

// dictGet is d.get(key) on a dict of either kind (ok false when missing).
func dictGet(d any, key string) (any, bool) {
	switch m := d.(type) {
	case *jsonx.Object:
		if m == nil {
			return nil, false
		}
		return m.Get(key)
	case *yamlx.Mapping:
		if m == nil {
			return nil, false
		}
		return m.Get(key)
	}
	return nil, false
}

// dictValue is d.get(key) with None for a missing key.
func dictValue(d any, key string) any {
	v, _ := dictGet(d, key)
	return v
}

func dictHas(d any, key string) bool {
	_, ok := dictGet(d, key)
	return ok
}

// dictSet is d[key] = value (an existing key keeps its position).
func dictSet(d any, key string, value any) {
	switch m := d.(type) {
	case *jsonx.Object:
		m.Set(key, value)
	case *yamlx.Mapping:
		for i, p := range m.Pairs {
			if k, ok := p.Key.(string); ok && k == key {
				m.Pairs[i].Value = value
				return
			}
		}
		m.Pairs = append(m.Pairs, yamlx.Pair{Key: key, Value: value})
	}
}

// dictPop is d.pop(key, None).
func dictPop(d any, key string) {
	switch m := d.(type) {
	case *jsonx.Object:
		m.Delete(key)
	case *yamlx.Mapping:
		for i, p := range m.Pairs {
			if k, ok := p.Key.(string); ok && k == key {
				m.Pairs = append(m.Pairs[:i], m.Pairs[i+1:]...)
				return
			}
		}
	}
}

// getter is `value.get` on a value Python expects to be a dict: an
// AttributeError for anything else (None included).
func getter(v any) (any, error) {
	if isDict(v) {
		return v, nil
	}
	return nil, attributeError(v, "get")
}

// orEmpty is `value or {}`.
func orEmpty(v any) any {
	if truthy(v) {
		return v
	}
	return jsonx.NewObject()
}

// chainGet is `(config.get(outer) or {}).get(key)` with Python's
// AttributeErrors for a config or an outer value that is not a dict.
func chainGet(config any, outer, key string) (any, error) {
	c, err := getter(config)
	if err != nil {
		return nil, err
	}
	inner, err := getter(orEmpty(dictValue(c, outer)))
	if err != nil {
		return nil, err
	}
	return dictValue(inner, key), nil
}

// pyList is list(value) for a JSON value: lists are copied, strings give
// their characters, dicts their keys; anything else is not iterable.
func pyList(v any) ([]any, error) {
	switch value := v.(type) {
	case []any:
		return append([]any{}, value...), nil
	case string:
		out := []any{}
		for _, r := range value {
			out = append(out, string(r))
		}
		return out, nil
	case *jsonx.Object:
		return jsonx.ListOf(value.Keys()), nil
	case *yamlx.Mapping:
		out := []any{}
		for _, p := range value.Pairs {
			out = append(out, p.Key)
		}
		return out, nil
	}
	return nil, &PyError{"TypeError", "'" + pyTypeName(v) + "' object is not iterable"}
}

// contains is `needle in values` for a list (Python ==).
func contains(values []any, needle any) bool {
	for _, v := range values {
		if jsonx.Equal(v, needle) {
			return true
		}
	}
	return false
}

func hashable(v any) error {
	switch v.(type) {
	case []any, *jsonx.Object, *yamlx.Mapping:
		return &PyError{"TypeError", "unhashable type: '" + pyTypeName(v) + "'"}
	}
	return nil
}

type listKey struct {
	ptr unsafe.Pointer
	n   int
}

// copier is copy.deepcopy with its memo: a value reached twice (a YAML
// alias) stays one shared value, so yaml.safe_dump writes the same anchors.
type copier struct {
	objects  map[*jsonx.Object]*jsonx.Object
	mappings map[*yamlx.Mapping]*yamlx.Mapping
	lists    map[listKey][]any
}

func deepcopy(v any) any {
	c := &copier{map[*jsonx.Object]*jsonx.Object{}, map[*yamlx.Mapping]*yamlx.Mapping{}, map[listKey][]any{}}
	return c.copy(v)
}

func (c *copier) copy(v any) any {
	switch value := v.(type) {
	case *jsonx.Object:
		if value == nil {
			return value
		}
		if out, ok := c.objects[value]; ok {
			return out
		}
		out := jsonx.NewObject()
		c.objects[value] = out
		for k, item := range value.All() {
			out.Set(k, c.copy(item))
		}
		return out
	case *yamlx.Mapping:
		if value == nil {
			return value
		}
		if out, ok := c.mappings[value]; ok {
			return out
		}
		out := &yamlx.Mapping{}
		c.mappings[value] = out
		for _, p := range value.Pairs {
			out.Pairs = append(out.Pairs, yamlx.Pair{Key: p.Key, Value: c.copy(p.Value)})
		}
		return out
	case []any:
		if value == nil {
			return value
		}
		var key listKey
		if len(value) > 0 {
			key = listKey{unsafe.Pointer(unsafe.SliceData(value)), len(value)}
			if out, ok := c.lists[key]; ok {
				return out
			}
		}
		out := make([]any, len(value))
		if key.ptr != nil {
			c.lists[key] = out
		}
		for i, item := range value {
			out[i] = c.copy(item)
		}
		return out
	case []byte:
		return append([]byte{}, value...)
	}
	return v
}
