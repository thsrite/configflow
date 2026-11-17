// Python helpers shared by the repository's compat files, and the dialer
// reference checks, which now come from internal/convert (module C12:
// backend/utils/dialer_references.py, converters/mihomo.py
// _parse_structured_proxy_string). convert errors are mapped to HelperError
// classes so the repository converts them as before.

package repo

import (
	"errors"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
)

// ---- small Python helpers shared by the compat files ----

// pyIsSpace is str.isspace() for one character.
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// pyStrip is str.strip().
func pyStrip(text string) string {
	return strings.TrimFunc(text, pyIsSpace)
}

// nonBlankString is isinstance(v, str) and v.strip().
func nonBlankString(v any) bool {
	text, ok := v.(string)
	return ok && pyStrip(text) != ""
}

// compatEnabled is obj.get('enabled', True) as a condition.
func compatEnabled(obj *jsonx.Object) bool {
	return jsonx.Truthy(obj.GetOr("enabled", true))
}

// compatObject is the receiver of a `.get`: anything but a dict raises
// AttributeError in Python.
func compatObject(v any) (*jsonx.Object, error) {
	if obj, ok := v.(*jsonx.Object); ok && obj != nil {
		return obj, nil
	}
	return nil, helperError(ClassAttributeError, "'"+pyTypeName(v)+"' object has no attribute 'get'")
}

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

// compatList is the operand of a `for ... in value` over a list field.
func compatList(v any) ([]any, error) {
	if v == nil {
		return nil, helperError(ClassTypeError, "'NoneType' object is not iterable")
	}
	switch value := v.(type) {
	case []any:
		return value, nil
	case string:
		result := make([]any, 0, len(value))
		for _, r := range value {
			result = append(result, string(r))
		}
		return result, nil
	case *jsonx.Object:
		return jsonx.ListOf(value.Keys()), nil
	}
	return nil, helperError(ClassTypeError, "'"+pyTypeName(v)+"' object is not iterable")
}

// compatItem is obj[key].
func compatItem(v any, key string) (any, error) {
	obj, ok := v.(*jsonx.Object)
	if !ok || obj == nil {
		return nil, helperError(ClassTypeError, "'"+pyTypeName(v)+"' object is not subscriptable")
	}
	value, present := obj.Get(key)
	if !present {
		return nil, helperError(ClassKeyError, jsonx.Repr(key))
	}
	return value, nil
}

// hashID turns a value used as a dict key or set member into a comparable
// string: strings are themselves, anything else gets a prefix no id has.
func hashID(v any) (string, error) {
	switch value := v.(type) {
	case string:
		return value, nil
	case []any:
		return "", helperError(ClassTypeError, "unhashable type: 'list'")
	case *jsonx.Object:
		return "", helperError(ClassTypeError, "unhashable type: 'dict'")
	case jsonx.Number:
		if value.IsFloat() {
			if f := value.Float64(); f == float64(int64(f)) {
				return "\x00" + jsonx.Repr(jsonx.Int(int64(f))), nil
			}
		}
	case bool:
		if value {
			return "\x001", nil
		}
		return "\x000", nil
	}
	return "\x00" + jsonx.Repr(v), nil
}

// objectMap is a dict of resources keyed by id: insertion-ordered, a later
// duplicate replaces the value and keeps the first position.
type objectMap struct {
	keys   []string
	values map[string]*jsonx.Object
}

func newObjectMap() *objectMap { return &objectMap{values: map[string]*jsonx.Object{}} }

func (m *objectMap) set(key string, value *jsonx.Object) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// byID is {item['id']: item for item in items}.
func byID(items any) (*objectMap, error) {
	list, err := compatList(items)
	if err != nil {
		return nil, err
	}
	result := newObjectMap()
	for _, item := range list {
		id, err := compatItem(item, "id")
		if err != nil {
			return nil, err
		}
		key, err := hashID(id)
		if err != nil {
			return nil, err
		}
		result.set(key, item.(*jsonx.Object))
	}
	return result, nil
}

// pyFloat is float(text) for an already stripped string; overflow gives
// ±inf like Python.
func pyFloat(text string) (float64, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil && errors.Is(err, strconv.ErrRange) {
		return value, nil
	}
	return value, err
}

// parseStructuredProxyString is _parse_structured_proxy_string. It is a
// variable so SetStructuredProxyStringParser can still replace it.
var parseStructuredProxyString = convert.InspectStructuredProxyString

// fromConvert maps convert's exceptions to HelperError classes.
func fromConvert(err error) error {
	if err == nil {
		return nil
	}
	var d *convert.DialerReferenceError
	if errors.As(err, &d) {
		return helperError(ClassDialerReference, d.Message)
	}
	var p *convert.PyError
	if errors.As(err, &p) {
		return helperError(p.Class, p.Message)
	}
	return helperError(ClassTypeError, err.Error())
}

// validateShapes is dialer_references.validate_shapes.
func validateShapes(profile *jsonx.Object, requireIDs bool) error {
	return fromConvert(convert.ValidateShapes(profile, requireIDs))
}

// validateDialers is dialer_references.validate_dialers.
func validateDialers(profile *jsonx.Object) error {
	return fromConvert(convert.ValidateDialers(profile))
}

// rawDialer is dialer_references.raw_dialer. It reads proxy strings through
// parseStructuredProxyString, as Python reads them through the converter.
func rawDialer(node *jsonx.Object) (any, error) {
	if proxyString := node.Value("proxy_string"); jsonx.Truthy(proxyString) {
		text, ok := proxyString.(string)
		if !ok {
			return nil, helperError(ClassAttributeError, "'"+pyTypeName(proxyString)+"' object has no attribute 'strip'")
		}
		parsed, err := parseStructuredProxyString(text)
		if err != nil {
			return nil, fromConvert(err)
		}
		if parsed == nil || parsed.Len() == 0 {
			return nil, nil
		}
		return parsed.Value("dialer-proxy"), nil
	}
	value, err := convert.RawDialer(node)
	return value, fromConvert(err)
}
