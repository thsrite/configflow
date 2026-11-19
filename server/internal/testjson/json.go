// Package testjson constructs ordered JSON fixtures and compares JSON structure.
// It is for tests and test tools only, not application documents or Python emulation.
package testjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

// MaxDepth bounds nested JSON objects and arrays accepted by Parse.
const MaxDepth = 256

// Field is one named value in an ordered object fixture.
type Field struct {
	Key   string
	Value any
}

// Object is an ordered JSON object. Duplicate keys retain their first position
// and last value, just as they do when Parse reads duplicate JSON members.
// Use Obj to construct an Object; the zero value is an empty object.
type Object struct {
	fields []Field
	index  map[string]int
}

// Obj constructs an ordered object from typed fields, without dynamic key/value
// argument pairs. For example: Obj(Field{Key: "id", Value: 42}).
func Obj(fields ...Field) *Object {
	o := &Object{index: make(map[string]int, len(fields))}
	for _, f := range fields {
		o.set(f.Key, f.Value)
	}
	return o
}

func (o *Object) set(key string, value any) {
	if i, ok := o.index[key]; ok {
		o.fields[i].Value = value
		return
	}
	o.index[key] = len(o.fields)
	o.fields = append(o.fields, Field{Key: key, Value: value})
}

// MarshalJSON encodes members in insertion order using encoding/json's native
// string and number encoding. It rejects cyclic or excessively nested fixtures.
func (o *Object) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	if err := checkDepth(reflect.ValueOf(o), 0); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o.fields {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(f.Key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(f.Value)
		if err != nil {
			return nil, fmt.Errorf("encode member %q: %w", f.Key, err)
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// checkDepth bounds fixture traversal before custom Object marshalers recurse.
func checkDepth(v reflect.Value, depth int) error {
	return checkValueDepth(v, depth, make(map[depthPointer]bool))
}

type depthPointer struct {
	typ reflect.Type
	ptr uintptr
}

func checkValueDepth(v reflect.Value, depth int, active map[depthPointer]bool) error {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		return checkValueDepth(v.Elem(), depth, active)
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		key := depthPointer{v.Type(), v.Pointer()}
		if active[key] {
			return fmt.Errorf("cyclic JSON fixture")
		}
		active[key] = true
		defer delete(active, key)
		return checkValueDepth(v.Elem(), depth, active)
	}
	composite := v.Kind() == reflect.Map || v.Kind() == reflect.Slice || v.Kind() == reflect.Array || v.Kind() == reflect.Struct
	if composite && depth >= MaxDepth {
		return fmt.Errorf("JSON fixture exceeds maximum depth %d", MaxDepth)
	}
	if v.Type() == reflect.TypeFor[Object]() {
		// Object's private bookkeeping is not part of the JSON value graph.
		o := v.Interface().(Object)
		for _, f := range o.fields {
			if err := checkValueDepth(reflect.ValueOf(f.Value), depth+1, active); err != nil {
				return err
			}
		}
		return nil
	}
	switch v.Kind() {
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if err := checkValueDepth(iter.Value(), depth+1, active); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := checkValueDepth(v.Index(i), depth+1, active); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				if err := checkValueDepth(v.Field(i), depth+1, active); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Parse reads exactly one JSON value using encoding/json and UseNumber. Objects
// preserve member order, duplicate keys keep the last value, and numbers retain
// their literal spelling. Nested objects/arrays deeper than MaxDepth are rejected.
func Parse(data []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := readValue(d, 0)
	if err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parse JSON: trailing value")
		}
		return nil, fmt.Errorf("parse trailing JSON: %w", err)
	}
	return v, nil
}

func readValue(d *json.Decoder, depth int) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return token, nil
	}
	if depth >= MaxDepth {
		return nil, fmt.Errorf("maximum JSON depth %d exceeded", MaxDepth)
	}
	switch delimiter {
	case '{':
		o := Obj()
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("object member name is not a string")
			}
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			o.set(name, value)
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return o, nil
	case '[':
		values := []any{}
		for d.More() {
			value, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

// Path looks up object members (string steps) or array elements (int steps).
// It accepts values from Parse, ordered fixtures and native JSON-marshalable
// values. Missing members, bad indices and invalid JSON return false.
func Path(value any, steps ...any) (any, bool) {
	value, err := normalize(value)
	if err != nil {
		return nil, false
	}
	for _, step := range steps {
		switch key := step.(type) {
		case string:
			o, ok := value.(*Object)
			if !ok {
				return nil, false
			}
			i, ok := o.index[key]
			if !ok {
				return nil, false
			}
			value = o.fields[i].Value
		case int:
			list, ok := value.([]any)
			if !ok || key < 0 || key >= len(list) {
				return nil, false
			}
			value = list[key]
		default:
			return nil, false
		}
	}
	return value, true
}

func normalize(v any) (any, error) {
	if err := checkDepth(reflect.ValueOf(v), 0); err != nil {
		return nil, err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}
