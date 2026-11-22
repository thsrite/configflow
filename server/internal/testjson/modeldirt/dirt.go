package modeldirt

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Dirt kinds reported by Dirt. Each names a stored value that plain Go
// records (O18 T1c) read differently from Python on purpose: a number in a
// text member reads as its literal, null and "" read as a missing member, a
// member the record does not declare is dropped, and a value of another JSON
// kind reads as the zero value. Empty text is reported only for members that
// are not pointers (a *string keeps "" apart from a missing member).
const (
	DirtNumberText = "number-as-text"
	DirtNull       = "null"
	DirtEmptyText  = "empty-text"
	DirtUnknown    = "undeclared-member"
	DirtType       = "wrong-type"
	// DirtQuotedNumber is a number stored as text in an integer member
	// ("300"); plain records read its value, Python kept the text.
	DirtQuotedNumber = "quoted-number"
)

// Dirt walks data with the shape of sample (a value or pointer of a
// plain record type, list or map of them) and returns the sorted, distinct
// dirt kinds found. Only members with json tags are known; a field tagged
// json:",embed" accepts every undeclared member. Types that decode
// themselves without exported fields (model.Int, model.Dynamic, OrderedMap)
// are not inspected. Invalid JSON returns nil.
func Dirt(data []byte, sample any) []string {
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if dec.Decode(&value) != nil {
		return nil
	}
	found := map[string]bool{}
	walkDirt(value, reflect.TypeOf(sample), true, false, found)
	kinds := make([]string, 0, len(found))
	for kind := range found {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// HasDirt reports whether data holds any of the given dirt kinds (any kind
// when none is given) for the shape of sample.
func HasDirt(data []byte, sample any, kinds ...string) bool {
	found := Dirt(data, sample)
	if len(kinds) == 0 {
		return len(found) > 0
	}
	for _, f := range found {
		for _, k := range kinds {
			if f == k {
				return true
			}
		}
	}
	return false
}

var unmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

func walkDirt(value any, typ reflect.Type, root, pointer bool, found map[string]bool) {
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
		pointer = true
	}
	if typ == nil {
		return
	}
	if value == nil {
		if !root {
			found[DirtNull] = true
		}
		return
	}
	if typ == reflect.TypeOf(json.RawMessage(nil)) || typ.Kind() == reflect.Interface {
		return
	}
	custom := reflect.PointerTo(typ).Implements(unmarshalerType)
	switch typ.Kind() {
	case reflect.Struct:
		fields := taggedFields(typ)
		if fields == nil {
			return
		}
		object, ok := value.(map[string]any)
		if !ok {
			found[DirtType] = true
			return
		}
		for name, item := range object {
			field, known := fields.byName[name]
			if !known {
				if !fields.embed {
					found[DirtUnknown] = true
				}
				continue
			}
			walkDirt(item, field, false, false, found)
		}
	case reflect.String:
		if custom {
			return
		}
		switch v := value.(type) {
		case string:
			// "" is distinguishable from a missing member only behind
			// a pointer.
			if v == "" && !pointer {
				found[DirtEmptyText] = true
			}
		case json.Number:
			found[DirtNumberText] = true
		default:
			found[DirtType] = true
		}
	case reflect.Bool:
		if _, ok := value.(bool); !ok && !custom {
			found[DirtType] = true
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		switch v := value.(type) {
		case json.Number:
			if !fitsInt64(v) && typ.Kind() != reflect.Float32 && typ.Kind() != reflect.Float64 {
				found[DirtType] = true
			}
		case string:
			found[DirtQuotedNumber] = true
		default:
			found[DirtType] = true
		}
	case reflect.Slice, reflect.Array:
		if typ.Elem().Kind() == reflect.Uint8 || custom {
			return
		}
		list, ok := value.([]any)
		if !ok {
			found[DirtType] = true
			return
		}
		for _, item := range list {
			walkDirt(item, typ.Elem(), false, false, found)
		}
	case reflect.Map:
		if custom {
			return
		}
		object, ok := value.(map[string]any)
		if !ok {
			found[DirtType] = true
			return
		}
		for _, item := range object {
			walkDirt(item, typ.Elem(), false, false, found)
		}
	}
}

type dirtFields struct {
	byName map[string]reflect.Type
	embed  bool
}

// taggedFields returns the JSON members of a struct, or nil when it declares
// none (a self-decoding wrapper).
func taggedFields(typ reflect.Type) *dirtFields {
	out := &dirtFields{byName: map[string]reflect.Type{}}
	declared := false
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		tag, ok := field.Tag.Lookup("json")
		if field.Anonymous && !ok {
			if inner := taggedFields(derefType(field.Type)); inner != nil {
				for name, t := range inner.byName {
					out.byName[name] = t
				}
				out.embed = out.embed || inner.embed
				declared = true
			}
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" && opts == "" {
			continue
		}
		declared = true
		if strings.Contains(","+opts+",", ",embed,") || strings.Contains(","+opts+",", ",inline,") || strings.Contains(","+opts+",", ",unknown,") {
			out.embed = true
			continue
		}
		if name == "" {
			name = field.Name
		}
		if unquoted, err := strconv.Unquote(name); err == nil {
			name = unquoted
		}
		out.byName[name] = field.Type
	}
	if !declared {
		return nil
	}
	return out
}

// fitsInt64 reports whether a JSON number is an integer (in any notation)
// that an int64 holds.
func fitsInt64(n json.Number) bool {
	if _, err := strconv.ParseInt(string(n), 10, 64); err == nil {
		return true
	}
	f, err := strconv.ParseFloat(string(n), 64)
	return err == nil && f == math.Trunc(f) && f >= math.MinInt64 && f < math.MaxInt64
}

func derefType(typ reflect.Type) reflect.Type {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}
