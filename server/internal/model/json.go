package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"unicode/utf8"
)

// ErrJSON identifies invalid JSON, including duplicate object member names.
var ErrJSON = errors.New("model: invalid JSON")

// ErrType identifies a JSON value incompatible with its Go destination.
var ErrType = errors.New("model: incompatible JSON type")

type jsonMember struct {
	name                           string
	key, leading, separator, value []byte
}

type jsonObject struct {
	data, opening, closing []byte
	members                []jsonMember
}

// validJSON checks the complete value, UTF-8, and uniqueness at every depth.
// Decoder.UseNumber keeps arbitrarily large numeric literals out of float64.
func validJSON(data []byte) error {
	if !utf8.Valid(data) || !json.Valid(data) {
		return ErrJSON
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var visit func() error
	visit = func() error {
		token, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: token", ErrJSON)
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				token, err := dec.Token()
				if err != nil {
					return ErrJSON
				}
				name, ok := token.(string)
				if !ok || seen[name] {
					return fmt.Errorf("%w: duplicate member", ErrJSON)
				}
				seen[name] = true
				if err := visit(); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := visit(); err != nil {
					return err
				}
			}
		default:
			return ErrJSON
		}
		_, err = dec.Token()
		return err
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrJSON
	}
	return nil
}

func space(b byte) bool { return b == ' ' || b == '\n' || b == '\r' || b == '\t' }
func skipSpace(data []byte, offset int) int {
	for offset < len(data) && space(data[offset]) {
		offset++
	}
	return offset
}

func parseObject(data []byte) (jsonObject, error) {
	if err := validJSON(data); err != nil {
		return jsonObject{}, err
	}
	data = bytes.Clone(data)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return jsonObject{}, ErrType
	}
	obj := jsonObject{data: data, opening: data[:dec.InputOffset()]}
	previous := int(dec.InputOffset())
	for dec.More() {
		keyStart := skipSpace(data, previous)
		if data[keyStart] == ',' {
			keyStart = skipSpace(data, keyStart+1)
		}
		token, err := dec.Token()
		if err != nil {
			return jsonObject{}, ErrJSON
		}
		name, ok := token.(string)
		if !ok {
			return jsonObject{}, ErrJSON
		}
		keyEnd := int(dec.InputOffset())
		valueStart := skipSpace(data, skipSpace(data, keyEnd)+1)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return jsonObject{}, ErrJSON
		}
		end := int(dec.InputOffset())
		leading := data[previous:keyStart]
		if comma := bytes.IndexByte(leading, ','); comma >= 0 {
			leading = leading[comma+1:]
		}
		obj.members = append(obj.members, jsonMember{name: name, key: data[keyStart:keyEnd], leading: leading, separator: data[keyEnd:valueStart], value: data[valueStart:end]})
		previous = end
	}
	if _, err := dec.Token(); err != nil {
		return jsonObject{}, ErrJSON
	}
	obj.closing = data[previous:]
	return obj, nil
}

func objectJSON(source jsonObject, members []jsonMember) ([]byte, error) {
	if source.data != nil && len(members) == len(source.members) {
		equal := true
		for i, m := range members {
			if m.name != source.members[i].name || !bytes.Equal(m.value, source.members[i].value) {
				equal = false
				break
			}
		}
		if equal {
			return bytes.Clone(source.data), nil
		}
	}
	var out bytes.Buffer
	if source.opening != nil {
		out.Write(source.opening)
	} else {
		out.WriteByte('{')
	}
	seen := map[string]bool{}
	for i, m := range members {
		if seen[m.name] {
			return nil, fmt.Errorf("%w: duplicate member", ErrJSON)
		}
		seen[m.name] = true
		if i > 0 {
			out.WriteByte(',')
		}
		if m.key != nil {
			out.Write(m.leading)
			out.Write(m.key)
			out.Write(m.separator)
		} else {
			key, err := scalarJSON(m.name)
			if err != nil {
				return nil, err
			}
			out.Write(key)
			out.WriteByte(':')
		}
		out.Write(m.value)
	}
	if source.closing != nil {
		out.Write(source.closing)
	} else {
		out.WriteByte('}')
	}
	if err := validJSON(out.Bytes()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func scalarJSON(v any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, fmt.Errorf("encode model value: %w", err)
	}
	return bytes.TrimSuffix(out.Bytes(), []byte{'\n'}), nil
}

var marshalerType = reflect.TypeFor[json.Marshaler]()

// Marshal writes model records, raw JSON and containers without compacting
// preserved fragments. New scalar values use encoding/json without HTML escapes.
// The complete output must be valid UTF-8 JSON with no duplicate object keys.
func Marshal(v any) ([]byte, error) {
	if err := acyclic(reflect.ValueOf(v), map[visitKey]bool{}); err != nil {
		return nil, err
	}
	data, err := marshalValue(reflect.ValueOf(v), map[visitKey]bool{})
	if err != nil {
		return nil, err
	}
	if err := validJSON(data); err != nil {
		return nil, err
	}
	return data, nil
}

type visitKey struct {
	typ     reflect.Type
	pointer uintptr
}

func acyclic(v reflect.Value, active map[visitKey]bool) error {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		return acyclic(v.Elem(), active)
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if v.IsNil() {
			return nil
		}
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return nil
		}
		pointer := uintptr(0)
		if v.Kind() == reflect.Map {
			pointer = uintptr(v.UnsafePointer())
		} else {
			pointer = v.Pointer()
		}
		key := visitKey{v.Type(), pointer}
		if active[key] {
			return fmt.Errorf("%w: cyclic value", ErrJSON)
		}
		active[key] = true
		defer delete(active, key)
	}
	if v.CanInterface() {
		if children, ok := v.Interface().(interface {
			walkValues(func(reflect.Value) error) error
		}); ok {
			return children.walkValues(func(child reflect.Value) error { return acyclic(child, active) })
		}
	}
	switch v.Kind() {
	case reflect.Pointer:
		return acyclic(v.Elem(), active)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				if err := acyclic(v.Field(i), active); err != nil {
					return err
				}
			}
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := acyclic(v.Index(i), active); err != nil {
				return err
			}
		}
	case reflect.Map:
		iterator := v.MapRange()
		for iterator.Next() {
			if err := acyclic(iterator.Value(), active); err != nil {
				return err
			}
		}
	}
	return nil
}

func marshalValue(v reflect.Value, active map[visitKey]bool) ([]byte, error) {
	if !v.IsValid() {
		return []byte("null"), nil
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return []byte("null"), nil
		}
		return marshalValue(v.Elem(), active)
	}
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice) && v.IsNil() {
		return []byte("null"), nil
	}
	if v.CanInterface() && v.Type().Implements(marshalerType) {
		return v.Interface().(json.Marshaler).MarshalJSON()
	}
	if v.CanAddr() && v.Addr().CanInterface() && v.Addr().Type().Implements(marshalerType) {
		return v.Addr().Interface().(json.Marshaler).MarshalJSON()
	}
	switch v.Kind() {
	case reflect.Pointer:
		return marshalValue(v.Elem(), active)
	case reflect.Array, reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return scalarJSON(v.Interface())
		}
		var out bytes.Buffer
		out.WriteByte('[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				out.WriteByte(',')
			}
			data, err := marshalValue(v.Index(i), active)
			if err != nil {
				return nil, err
			}
			out.Write(data)
		}
		out.WriteByte(']')
		return out.Bytes(), nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("%w: map key must be a string", ErrType)
		}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		members := make([]jsonMember, 0, len(keys))
		for _, key := range keys {
			data, err := marshalValue(v.MapIndex(key), active)
			if err != nil {
				return nil, err
			}
			members = append(members, jsonMember{name: key.String(), value: data})
		}
		return objectJSON(jsonObject{}, members)
	default:
		return scalarJSON(v.Interface())
	}
}
