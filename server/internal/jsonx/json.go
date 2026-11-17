package jsonx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
)

// MarshalJSON bridges ordered legacy objects to encoding/json. Member order and
// number literals are retained; non-JSON numbers and cyclic trees are rejected.
func (o *Object) MarshalJSON() ([]byte, error) {
	if err := checkBridgeCycles(reflect.ValueOf(o), make(map[bridgeID]bool), 0); err != nil {
		return nil, err
	}
	b := bridgeEncoder{objects: make(map[*Object]bool), lists: make(map[*any]bool)}
	if err := b.value(o, 0); err != nil {
		return nil, err
	}
	return b.buf.Bytes(), nil
}

// UnmarshalJSON replaces o with an ordered object decoded by encoding/json.
// Duplicate members keep their first position and last value. Numbers keep
// their literal text, including exponent and fractional notation.
func (o *Object) UnmarshalJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := bridgeDecode(d)
	if err != nil {
		return fmt.Errorf("decode ordered object: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("decode ordered object: trailing JSON data")
	}
	if v == nil {
		*o = *NewObject()
		return nil
	}
	obj, ok := v.(*Object)
	if !ok {
		return errors.New("decode ordered object: expected an object")
	}
	*o = *obj
	return nil
}

// MarshalJSON writes n as a JSON number without changing its literal text.
func (n Number) MarshalJSON() ([]byte, error) {
	if n == "" {
		return nil, errors.New("encode legacy number: empty number")
	}
	return json.Marshal(json.Number(n))
}

// UnmarshalJSON reads a JSON number without changing its literal text.
func (n *Number) UnmarshalJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := d.Token()
	if err != nil {
		return fmt.Errorf("decode legacy number: %w", err)
	}
	num, ok := v.(json.Number)
	if !ok {
		return errors.New("decode legacy number: expected a number")
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("decode legacy number: trailing JSON data")
	}
	*n = Number(num)
	return nil
}

type bridgeEncoder struct {
	buf     bytes.Buffer
	objects map[*Object]bool
	lists   map[*any]bool
}

func (b *bridgeEncoder) value(v any, depth int) error {
	if depth > MaxDepth {
		return errors.New("encode ordered object: nesting limit exceeded")
	}
	switch v := v.(type) {
	case *Object:
		if v == nil {
			b.buf.WriteString("null")
			return nil
		}
		if b.objects[v] {
			return errors.New("encode ordered object: cyclic object")
		}
		b.objects[v] = true
		defer delete(b.objects, v)
		b.buf.WriteByte('{')
		first := true
		for key, item := range v.All() {
			if !first {
				b.buf.WriteByte(',')
			}
			first = false
			if err := b.native(key); err != nil {
				return err
			}
			b.buf.WriteByte(':')
			if err := b.value(item, depth+1); err != nil {
				return err
			}
		}
		b.buf.WriteByte('}')
		return nil
	case []any:
		if v == nil {
			b.buf.WriteString("null")
			return nil
		}
		if len(v) > 0 {
			id := &v[0]
			if b.lists[id] {
				return errors.New("encode ordered object: cyclic list")
			}
			b.lists[id] = true
			defer delete(b.lists, id)
		}
		b.buf.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.buf.WriteByte(',')
			}
			if err := b.value(item, depth+1); err != nil {
				return err
			}
		}
		b.buf.WriteByte(']')
		return nil
	default:
		return b.native(v)
	}
}

func (b *bridgeEncoder) native(v any) error {
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return fmt.Errorf("encode ordered member: %w", err)
	}
	b.buf.Write(bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}))
	return nil
}

func bridgeDecode(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		o := NewObject()
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			v, err := bridgeDecode(d)
			if err != nil {
				return nil, err
			}
			o.Set(key.(string), v)
		}
		_, err := d.Token()
		return o, err
	case json.Delim('['):
		list := []any{}
		for d.More() {
			v, err := bridgeDecode(d)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		_, err := d.Token()
		return list, err
	}
	if n, ok := token.(json.Number); ok {
		return Number(n), nil
	}
	return token, nil
}

// Native containers can point back through an Object's custom marshaler. Check
// that mixed graph before invoking encoding/json, which cannot detect cycles
// spanning separate MarshalJSON calls. Ignore fields the native encoder ignores.
type bridgeID struct {
	kind    reflect.Kind
	pointer uintptr
	length  int
}

func checkBridgeCycles(v reflect.Value, active map[bridgeID]bool, depth int) error {
	if !v.IsValid() {
		return nil
	}
	if depth > MaxDepth {
		return errors.New("encode ordered object: nesting limit exceeded")
	}
	if v.Kind() == reflect.Interface {
		return checkBridgeCycles(v.Elem(), active, depth)
	}
	if v.Type() == reflect.TypeFor[*Object]() {
		if v.IsNil() {
			return nil
		}
		id := bridgeID{reflect.Pointer, v.Pointer(), 0}
		if active[id] {
			return errors.New("encode ordered object: cyclic value")
		}
		active[id] = true
		defer delete(active, id)
		for _, item := range v.Interface().(*Object).All() {
			if err := checkBridgeCycles(reflect.ValueOf(item), active, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	// Other custom marshalers own their representation (e.g. time.Time).
	if v.Type().Implements(reflect.TypeFor[json.Marshaler]()) {
		return nil
	}
	var id bridgeID
	switch v.Kind() {
	case reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return nil
		}
		id = bridgeID{v.Kind(), v.Pointer(), 0}
		if v.Kind() == reflect.Slice {
			id.length = v.Len()
		}
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		id = bridgeID{v.Kind(), uintptr(v.UnsafePointer()), 0}
	}
	if id.pointer != 0 {
		if active[id] {
			return errors.New("encode ordered object: cyclic value")
		}
		active[id] = true
		defer delete(active, id)
	}
	switch v.Kind() {
	case reflect.Pointer:
		return checkBridgeCycles(v.Elem(), active, depth+1)
	case reflect.Map:
		it := v.MapRange()
		for it.Next() {
			if err := checkBridgeCycles(it.Value(), active, depth+1); err != nil {
				return err
			}
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := checkBridgeCycles(v.Index(i), active, depth+1); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if field.PkgPath != "" || field.Tag.Get("json") == "-" {
				continue
			}
			if err := checkBridgeCycles(v.Field(i), active, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
