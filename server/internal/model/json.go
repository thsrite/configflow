package model

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"unicode/utf8"
)

// ErrJSON identifies invalid JSON, including duplicate object member names.
var ErrJSON = errors.New("model: invalid JSON")

// ErrType identifies a JSON value incompatible with its Go destination.
var ErrType = errors.New("model: incompatible JSON type")

type jsonMember struct {
	name                                     string
	key, leading, separator, value, trailing []byte
}

type jsonObject struct {
	data, opening, closing []byte
	members                []jsonMember
}

// validJSON checks the complete value, UTF-8, and uniqueness at every depth in
// one jsontext pass. Lone surrogate escapes stay valid (and compare as U+FFFD),
// as they did with encoding/json; raw invalid UTF-8 is rejected by utf8.Valid.
// Only a failure is classified again: malformed input is ErrJSON, anything
// else that jsontext rejects is a duplicate member name.
func validJSON(data []byte) error {
	if utf8.Valid(data) && jsontext.Value(data).IsValid(jsontext.AllowInvalidUTF8(true)) {
		return nil
	}
	if !utf8.Valid(data) || !json.Valid(data) {
		return ErrJSON
	}
	return fmt.Errorf("%w: duplicate member", ErrJSON)
}

func space(b byte) bool { return b == ' ' || b == '\n' || b == '\r' || b == '\t' }
func skipSpace(data []byte, offset int) int {
	for offset < len(data) && space(data[offset]) {
		offset++
	}
	return offset
}

// parseObject validates data once and splits its top-level members, keeping
// every whitespace fragment so unchanged members can be written back as read.
func parseObject(data []byte) (jsonObject, error) {
	if err := validJSON(data); err != nil {
		return jsonObject{}, err
	}
	data = bytes.Clone(data)
	// Validated above; the decoder only reports offsets and reads the
	// bytes.Buffer in place.
	dec := jsontext.NewDecoder(bytes.NewBuffer(data), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	if dec.PeekKind() != '{' {
		return jsonObject{}, ErrType
	}
	if _, err := dec.ReadToken(); err != nil {
		return jsonObject{}, ErrJSON
	}
	obj := jsonObject{data: data, opening: data[:dec.InputOffset()]}
	previous := int(dec.InputOffset())
	for {
		if kind := dec.PeekKind(); kind == '}' {
			break
		} else if kind != '"' {
			return jsonObject{}, ErrJSON
		}
		keyStart := skipSpace(data, previous)
		if data[keyStart] == ',' {
			keyStart = skipSpace(data, keyStart+1)
		}
		token, err := dec.ReadToken()
		if err != nil {
			return jsonObject{}, ErrJSON
		}
		name := token.String()
		keyEnd := int(dec.InputOffset())
		valueStart := skipSpace(data, skipSpace(data, keyEnd)+1)
		if _, err := dec.ReadValue(); err != nil {
			return jsonObject{}, ErrJSON
		}
		end := int(dec.InputOffset())
		leading := data[previous:keyStart]
		if comma := bytes.IndexByte(leading, ','); comma >= 0 {
			if len(obj.members) > 0 {
				obj.members[len(obj.members)-1].trailing = leading[:comma]
			}
			leading = leading[comma+1:]
		}
		obj.members = append(obj.members, jsonMember{name: name, key: data[keyStart:keyEnd], leading: leading, separator: data[keyEnd:valueStart], value: data[valueStart:end]})
		previous = end
	}
	if _, err := dec.ReadToken(); err != nil {
		return jsonObject{}, ErrJSON
	}
	closingStart := skipSpace(data, previous)
	if len(obj.members) > 0 {
		obj.members[len(obj.members)-1].trailing = data[previous:closingStart]
		obj.closing = data[closingStart:]
	} else {
		obj.closing = data[previous:]
	}
	return obj, nil
}

// splitArray returns the elements of a validated JSON array as subslices of
// data, nil for null, and ErrType for any other value: what json.Unmarshal
// into []json.RawMessage gives, without copying or scanning twice.
func splitArray(data []byte) ([]json.RawMessage, error) {
	dec := jsontext.NewDecoder(bytes.NewBuffer(data), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	switch dec.PeekKind() {
	case 'n':
		return nil, nil
	case '[':
	default:
		return nil, ErrType
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, ErrJSON
	}
	items := []json.RawMessage{}
	for {
		switch dec.PeekKind() {
		case ']':
		case 0:
			return nil, ErrJSON
		default:
			value, err := dec.ReadValue()
			if err != nil {
				return nil, ErrJSON
			}
			end := int(dec.InputOffset())
			items = append(items, data[end-len(value):end:end])
			continue
		}
		break
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, ErrJSON
	}
	return items, nil
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
		out.Write(m.trailing)
	}
	if source.closing != nil {
		out.Write(source.closing)
	} else {
		out.WriteByte('}')
	}
	if tooDeep(out.Bytes()) {
		return nil, ErrJSON
	}
	return out.Bytes(), nil
}

// maxDepth is the nesting limit of encoding/json and jsontext.
const maxDepth = 10000

// tooDeep reports whether data, valid by construction except for its depth,
// nests deeper than maxDepth. Joining valid values adds one level, so the
// check points that used to run validJSON keep this one (it fails exactly
// where validJSON did, with the same ErrJSON). Data shorter than 2*maxDepth+2
// bytes cannot be that deep; longer data is scanned without allocating,
// skipping string contents with IndexByte.
func tooDeep(data []byte) bool {
	if len(data) <= 2*maxDepth+1 {
		return false
	}
	depth := 0
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '"':
			for {
				next := bytes.IndexByte(data[i+1:], '"')
				if next < 0 {
					return false
				}
				i += next + 1
				escapes := 0
				for k := i - 1; data[k] == '\\'; k-- {
					escapes++
				}
				if escapes%2 == 0 {
					break
				}
			}
		case '[', '{':
			depth++
			if depth > maxDepth {
				return true
			}
		case ']', '}':
			depth--
		}
	}
	return false
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

// modelPackage is this package's import path. Its JSON codecs build output
// only from validated or self-encoded bytes (see encodeUnit), so their
// MarshalJSON results need no second validation when nested.
var modelPackage = rawType.PkgPath()

// Marshal writes model records, raw JSON and containers without compacting
// preserved fragments. New scalar values use encoding/json without HTML escapes.
// Nil slices and zero Maps are written as [] and {} (never null), like
// encoding/json/v2; a nil pointer or RawMessage is null.
// The complete output must be valid UTF-8 JSON with no duplicate object keys;
// it is checked once, here, on the final bytes.
// validUTF8 replaces invalid UTF-8 with U+FFFD. Lenient reads accept it
// (jsontext.AllowInvalidUTF8) and string leaves read it as U+FFFD; values a
// plain record keeps as stored JSON (damaged manifests and guards, node
// protocol parameters, raw members) are read the same way, so what can be
// read can be written. In a value that decoded, invalid UTF-8 can only occur
// inside strings.
func validUTF8(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	return bytes.ToValidUTF8(data, []byte("\uFFFD"))
}

func Marshal(v any) ([]byte, error) {
	data, err := encodeUnit(v)
	if err != nil {
		return nil, err
	}
	if err := validJSON(data); err != nil {
		return nil, err
	}
	return data, nil
}

// encodeUnit is Marshal for the codec's own nested values (record members, Map
// values, typed baselines). It returns the same bytes and errors as Marshal
// did before O10-T2 without re-validating what is valid by construction:
//
//   - The cycle check runs only for types whose graph can contain a cycle.
//   - Output of this package's codecs is trusted: they only join bytes that
//     were validated on entry (decoded originals, Fields.Set, SetMemberJSON,
//     Dynamic), encoding/json scalars and other trusted output, and check
//     member names for uniqueness while joining objects.
//   - Output of any other json.Marshaler (json.RawMessage, other packages) is
//     foreign. A unit holding foreign bytes is validated as a whole, exactly
//     where Marshal used to validate it, so a fragment such as `1,2` is still
//     rejected even where it would happen to fit its surroundings.
//
// Code in this package must keep that invariant: a MarshalJSON here never
// returns bytes that were not validated or built by these rules.
func encodeUnit(v any) ([]byte, error) {
	value := reflect.ValueOf(v)
	if value.IsValid() && mayCycle(value.Type()) {
		if err := acyclic(value, map[visitKey]bool{}); err != nil {
			return nil, err
		}
	}
	var e encoder
	data, err := e.value(value)
	if err != nil {
		return nil, err
	}
	if e.foreign {
		if err := validJSON(data); err != nil {
			return nil, err
		}
	} else if tooDeep(data) {
		return nil, ErrJSON
	}
	return data, nil
}

type visitKey struct {
	typ     reflect.Type
	pointer uintptr
	length  int
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
		// Overlapping slices may share their first element without forming a
		// cycle: a later record can refer to an earlier, shorter prefix.
		key := visitKey{typ: v.Type(), pointer: pointer}
		if v.Kind() == reflect.Slice {
			key.length = v.Len()
		}
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

// encoder writes one encodeUnit; foreign records that it included bytes from
// a json.Marshaler outside this package that are not yet validated.
type encoder struct{ foreign bool }

func (e *encoder) value(v reflect.Value) ([]byte, error) {
	if !v.IsValid() {
		return []byte("null"), nil
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return []byte("null"), nil
		}
		return e.value(v.Elem())
	}
	// A nil list is an empty list in Go; stored lists are always written as
	// arrays (as Python did), so `var kept []T` filtered to nothing stays [].
	// R2 still keeps a stored null: its baseline encodes the same way.
	if v.Kind() == reflect.Slice && v.IsNil() && v.Type() != rawMessageType && v.Type().Elem().Kind() != reflect.Uint8 {
		return []byte("[]"), nil
	}
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice) && v.IsNil() {
		return []byte("null"), nil
	}
	if v.CanInterface() && v.Type().Implements(marshalerType) {
		return e.marshaler(v.Type(), v.Interface().(json.Marshaler))
	}
	if v.CanAddr() && v.Addr().CanInterface() && v.Addr().Type().Implements(marshalerType) {
		return e.marshaler(v.Type(), v.Addr().Interface().(json.Marshaler))
	}
	switch v.Kind() {
	case reflect.Pointer:
		return e.value(v.Elem())
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
			data, err := e.value(v.Index(i))
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
		// A Go map is joined by objectJSON, which validated its result
		// before O10-T2; keep that check point for foreign members.
		outer := e.foreign
		e.foreign = false
		members := make([]jsonMember, 0, len(keys))
		for _, key := range keys {
			data, err := e.value(v.MapIndex(key))
			if err != nil {
				return nil, err
			}
			members = append(members, jsonMember{name: key.String(), value: data})
		}
		data, err := objectJSON(jsonObject{}, members)
		if err != nil {
			return nil, err
		}
		if e.foreign {
			if err := validJSON(data); err != nil {
				return nil, err
			}
		}
		e.foreign = outer
		return data, nil
	case reflect.Struct:
		return nil, fmt.Errorf("%w: record must implement JSON codecs", ErrType)
	default:
		return scalarJSON(v.Interface())
	}
}

func (e *encoder) marshaler(typ reflect.Type, m json.Marshaler) ([]byte, error) {
	data, err := m.MarshalJSON()
	if err != nil {
		return nil, err
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.PkgPath() != modelPackage {
		e.foreign = true
	}
	return data, nil
}

// cycleTypes caches mayCycle per reflect.Type.
var cycleTypes sync.Map

// mayCycle reports whether a value of typ can reach itself along the edges
// acyclic follows (pointers, slices, arrays, map values, exported struct
// fields, interfaces and Map values). Without an interface or a recursive
// type in that graph no value can form a cycle, so acyclic would pass.
func mayCycle(typ reflect.Type) bool {
	if cached, ok := cycleTypes.Load(typ); ok {
		return cached.(bool)
	}
	result := typeMayCycle(typ, map[reflect.Type]bool{})
	cycleTypes.Store(typ, result)
	return result
}

var walkerType = reflect.TypeFor[interface {
	walkValues(func(reflect.Value) error) error
}]()
var storedValuerType = reflect.TypeFor[interface{ storedValueType() reflect.Type }]()

func typeMayCycle(typ reflect.Type, path map[reflect.Type]bool) bool {
	if path[typ] {
		return true
	}
	if cached, ok := cycleTypes.Load(typ); ok {
		return cached.(bool)
	}
	path[typ] = true
	defer delete(path, typ)
	if typ.Kind() == reflect.Pointer {
		return typeMayCycle(typ.Elem(), path)
	}
	if typ.Implements(walkerType) {
		if !typ.Implements(storedValuerType) {
			return true
		}
		return typeMayCycle(reflect.Zero(typ).Interface().(interface{ storedValueType() reflect.Type }).storedValueType(), path)
	}
	switch typ.Kind() {
	case reflect.Interface:
		return true
	case reflect.Slice, reflect.Array, reflect.Map:
		return typeMayCycle(typ.Elem(), path)
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			if typ.Field(i).IsExported() && typeMayCycle(typ.Field(i).Type, path) {
				return true
			}
		}
	}
	return false
}
