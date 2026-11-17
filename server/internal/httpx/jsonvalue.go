package httpx

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"

	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
)

// responseValue replaces non-finite numbers without modifying caller-owned
// values. It is used only after native encoding rejects a response. Struct
// mirrors retain JSON tags and embedding; encoding/json still selects fields,
// applies omission/string options and encodes custom marshalers.
func responseValue(value any) (result any, changed bool, err error) {
	// StructOf has restrictions on unusual reflected types. Return a normal
	// encoding error if a custom response type cannot be mirrored safely.
	defer func() {
		if recover() != nil {
			result = nil
			changed = false
			err = errors.New("normalize JSON response: unsupported reflected type")
		}
	}()
	v, changed, err := normalizeResponseValue(reflect.ValueOf(value), make(map[responseVisit]bool), 0)
	if err != nil {
		return nil, false, err
	}
	if !v.IsValid() {
		return nil, changed, nil
	}
	return v.Interface(), changed, nil
}

type responseVisit struct {
	kind    reflect.Kind
	pointer uintptr
	length  int
}

var (
	objectType        = reflect.TypeFor[*jsonx.Object]()
	numberType        = reflect.TypeFor[jsonx.Number]()
	marshalerType     = reflect.TypeFor[json.Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
)

func normalizeResponseValue(v reflect.Value, active map[responseVisit]bool, depth int) (reflect.Value, bool, error) {
	if !v.IsValid() {
		return reflect.Zero(reflect.TypeFor[any]()), false, nil
	}
	if depth > jsonx.MaxDepth {
		return reflect.Value{}, false, errors.New("normalize JSON response: nesting limit exceeded")
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v, false, nil
		}
		return normalizeResponseValue(v.Elem(), active, depth)
	}
	if v.Type() == reflect.TypeFor[json.RawMessage]() {
		data, changed := finiteJSON(v.Bytes())
		if changed && json.Valid(data) {
			return reflect.ValueOf(json.RawMessage(data)), true, nil
		}
		return v, false, nil
	}
	if v.Type() == numberType {
		switch v.String() {
		case "NaN", "Infinity", "-Infinity":
			return reflect.ValueOf(json.RawMessage("null")), true, nil
		}
		return v, false, nil
	}
	isObject := v.Type() == objectType
	bridgePointer := v.Kind() == reflect.Pointer && (v.Type().Elem() == numberType || v.Type().Elem() == reflect.TypeFor[json.RawMessage]())
	if !isObject && !bridgePointer && (v.Type().Implements(marshalerType) || v.Type().Implements(textMarshalerType)) {
		return v, false, nil
	}
	if v.Kind() == reflect.Float32 || v.Kind() == reflect.Float64 {
		if math.IsNaN(v.Float()) || math.IsInf(v.Float(), 0) {
			return reflect.ValueOf(json.RawMessage("null")), true, nil
		}
		return v, false, nil
	}
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
		return v, false, nil
	}
	var id responseVisit
	switch v.Kind() {
	case reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return v, false, nil
		}
		id = responseVisit{v.Kind(), v.Pointer(), 0}
		if v.Kind() == reflect.Slice {
			id.length = v.Len()
		}
	case reflect.Map:
		if v.IsNil() {
			return v, false, nil
		}
		id = responseVisit{v.Kind(), uintptr(v.UnsafePointer()), 0}
	}
	if id.pointer != 0 {
		if active[id] {
			return reflect.Value{}, false, errors.New("normalize JSON response: cyclic value")
		}
		active[id] = true
		defer delete(active, id)
	}
	changed := false
	if isObject {
		out := jsonx.NewObject()
		for key, item := range v.Interface().(*jsonx.Object).All() {
			next, altered, err := normalizeResponseValue(reflect.ValueOf(item), active, depth+1)
			if err != nil {
				return reflect.Value{}, false, err
			}
			changed = changed || altered
			out.Set(key, next.Interface())
		}
		return reflect.ValueOf(out), changed, nil
	}
	switch v.Kind() {
	case reflect.Pointer:
		next, altered, err := normalizeResponseValue(v.Elem(), active, depth+1)
		if err != nil {
			return reflect.Value{}, false, err
		}
		out := reflect.New(next.Type())
		out.Elem().Set(next)
		return out, altered, nil
	case reflect.Slice, reflect.Array:
		out := make([]any, v.Len())
		for i := range out {
			next, altered, err := normalizeResponseValue(v.Index(i), active, depth+1)
			if err != nil {
				return reflect.Value{}, false, err
			}
			changed = changed || altered
			out[i] = next.Interface()
		}
		return reflect.ValueOf(out), changed, nil
	case reflect.Map:
		out := reflect.MakeMapWithSize(reflect.MapOf(v.Type().Key(), reflect.TypeFor[any]()), v.Len())
		it := v.MapRange()
		for it.Next() {
			next, altered, err := normalizeResponseValue(it.Value(), active, depth+1)
			if err != nil {
				return reflect.Value{}, false, err
			}
			changed = changed || altered
			out.SetMapIndex(it.Key(), next)
		}
		return out, changed, nil
	case reflect.Struct:
		fields := []reflect.StructField{}
		values := []reflect.Value{}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if field.Tag.Get("json") == "-" || (field.PkgPath != "" && !field.Anonymous) {
				continue
			}
			next, altered, err := normalizeResponseValue(v.Field(i), active, depth+1)
			if err != nil {
				return reflect.Value{}, false, err
			}
			changed = changed || altered
			field.Type = next.Type()
			field.Offset = 0
			field.Index = nil
			if field.PkgPath != "" {
				field.Name = fmt.Sprintf("Anonymous%d", i)
				field.PkgPath = ""
			}
			kind := next.Kind()
			if kind == reflect.Pointer {
				kind = next.Type().Elem().Kind()
			}
			if kind != reflect.Struct {
				field.Anonymous = false
			}
			fields = append(fields, field)
			values = append(values, next)
		}
		out := reflect.New(reflect.StructOf(fields)).Elem()
		for i, next := range values {
			out.Field(i).Set(next)
		}
		return out, changed, nil
	}
	return v, false, nil
}

// finiteJSON is the response-only compatibility boundary. Requests remain strict.
func finiteJSON(data []byte) ([]byte, bool) { return jsonresp.NormalizeNonFinite(data) }
