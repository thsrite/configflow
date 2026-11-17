package jsonx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MarshalJSON bridges ordered legacy objects to encoding/json. Member order and
// number literals are retained; non-JSON numbers and cyclic trees are rejected.
func (o *Object) MarshalJSON() ([]byte, error) {
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
