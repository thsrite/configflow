// Package jsondecode decodes JSON DTOs with exact member names. It leaves
// custom codecs and opaque JSON in charge of their own storage/protocol rules.
package jsondecode

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

var unmarshalerType = reflect.TypeFor[json.Unmarshaler]()
var schemas sync.Map // reflect.Type -> map[string]field (immutable)

// Unmarshal is encoding/json.Unmarshal with case-sensitive struct field names.
// Unknown members are ignored. Repeated members of ordinary DTO objects use the
// last complete value, rather than merging earlier objects into the last one.
// Custom UnmarshalJSON methods receive the original bytes, including duplicates;
// their implementations must use this package when decoding ordinary DTOs.
func Unmarshal(data []byte, dst any) error {
	if !json.Valid(data) {
		return json.Unmarshal(data, dst)
	}
	data, err := prepare(data, reflect.TypeOf(dst), reflect.ValueOf(dst))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// Decode is Unmarshal with numbers in interface values kept as json.Number.
// It accepts exactly one JSON value, including when dst uses a custom codec.
func Decode(data []byte, dst any) error {
	if !json.Valid(data) {
		var raw json.RawMessage
		return json.Unmarshal(data, &raw)
	}
	data, err := prepare(data, reflect.TypeOf(dst), reflect.ValueOf(dst))
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(dst)
}

// Select returns owned JSON values for exact top-level member names. Unknown
// members are scanned by encoding/json without copying their value bytes.
// Duplicate names select the last complete value. Null yields an empty result;
// other non-object roots return encoding/json's native type or syntax error.
func Select(data []byte, exactNames ...string) (map[string]json.RawMessage, error) {
	var fields map[string]borrowedJSON
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	selected := make(map[string]json.RawMessage, len(exactNames))
	for _, name := range exactNames {
		if _, already := selected[name]; already {
			continue
		}
		if raw, present := fields[name]; present {
			selected[name] = bytes.Clone(raw)
		}
	}
	return selected, nil
}

// borrowedJSON is confined to input scanning: encoding/json passes each value's
// input span to custom codecs. Keeping that span avoids copying megabytes of
// unrelated configuration merely to discard an unknown member. The destination
// decoder or Select copies each value before it can escape.
type borrowedJSON []byte

// UnmarshalJSON borrows a value only until the current scan finishes.
func (b *borrowedJSON) UnmarshalJSON(data []byte) error { *b = data; return nil }

func prepare(data []byte, typ reflect.Type, value reflect.Value) ([]byte, error) {
	if typ == nil {
		return data, nil
	}
	// An interface can hold a non-nil concrete pointer that encoding/json reuses.
	// Follow that value as well as the declared type; a nil interface (or one
	// holding a non-pointer) instead receives native map/slice/scalar values.
	for {
		if typ.Implements(unmarshalerType) || typ.Kind() != reflect.Pointer && reflect.PointerTo(typ).Implements(unmarshalerType) {
			return data, nil
		}
		switch typ.Kind() {
		case reflect.Pointer:
			typ = typ.Elem()
			if value.IsValid() && !value.IsNil() {
				value = value.Elem()
			} else {
				value = reflect.Value{}
			}
		case reflect.Interface:
			if !value.IsValid() || value.IsNil() {
				return data, nil
			}
			next := value.Elem()
			if next.Kind() != reflect.Pointer || next.IsNil() || next.Elem() == value {
				return data, nil
			}
			typ, value = next.Type(), next
		default:
			goto concrete
		}
	}
concrete:
	text := bytes.TrimSpace(data)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) {
		return data, nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		if text[0] != '{' {
			return data, nil
		}
		members, changed, err := objectMembers(data)
		if err != nil {
			return nil, err
		}
		fields := fieldsFor(typ)
		// Do not re-marshal RawMessage: that would compact opaque values and change
		// the bytes custom codecs use as provenance, signatures or number tokens.
		var out bytes.Buffer
		out.WriteByte('{')
		first := true
		for _, member := range members {
			f, ok := fields[member.key]
			if !ok {
				changed = true
				continue
			}
			next, err := prepare(member.raw, f.typ, fieldValue(value, f.index))
			if err != nil {
				return nil, err
			}
			changed = changed || !bytes.Equal(next, member.raw)
			if !first {
				out.WriteByte(',')
			}
			first = false
			k, _ := json.Marshal(member.key)
			out.Write(k)
			out.WriteByte(':')
			out.Write(next)
		}
		out.WriteByte('}')
		if !changed {
			return data, nil
		}
		return out.Bytes(), nil
	case reflect.Slice, reflect.Array:
		if text[0] != '[' || !containsStruct(typ.Elem(), map[reflect.Type]bool{}) {
			return data, nil
		}
		var values []borrowedJSON
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		var out bytes.Buffer
		out.WriteByte('[')
		changed := false
		for i, raw := range values {
			if i > 0 {
				out.WriteByte(',')
			}
			var element reflect.Value
			if value.IsValid() {
				v := value
				if v.Kind() == reflect.Slice {
					v = v.Slice(0, v.Cap())
				}
				if i < v.Len() {
					element = v.Index(i)
				}
			}
			next, err := prepare(raw, typ.Elem(), element)
			if err != nil {
				return nil, err
			}
			changed = changed || !bytes.Equal(next, raw)
			out.Write(next)
		}
		out.WriteByte(']')
		if !changed {
			return data, nil
		}
		return out.Bytes(), nil
	case reflect.Map:
		if text[0] != '{' || !containsStruct(typ.Elem(), map[reflect.Type]bool{}) {
			return data, nil
		}
		members, changed, err := objectMembers(data)
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		out.WriteByte('{')
		for i, member := range members {
			if i > 0 {
				out.WriteByte(',')
			}
			k, _ := json.Marshal(member.key)
			out.Write(k)
			out.WriteByte(':')
			// Native map decoding always starts a fresh element, even when
			// the key already exists in the destination.
			next, err := prepare(member.raw, typ.Elem(), reflect.Value{})
			if err != nil {
				return nil, err
			}
			changed = changed || !bytes.Equal(next, member.raw)
			out.Write(next)
		}
		out.WriteByte('}')
		if !changed {
			return data, nil
		}
		return out.Bytes(), nil
	}
	return data, nil
}

// objectMembers preserves source order for custom codec calls and native error
// recovery. Only the last occurrence of a repeated ordinary member is decoded.
func objectMembers(data []byte) ([]member, bool, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	if _, err := d.Token(); err != nil {
		return nil, false, err
	}
	var members []member
	last := map[string]int{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, false, err
		}
		start := d.InputOffset()
		var raw borrowedJSON
		if err := d.Decode(&raw); err != nil {
			return nil, false, err
		}
		// Decoder owns a reusable buffer; retain the span in the original input,
		// rather than borrowing that buffer across the next Decode call.
		raw = bytes.TrimSpace(data[start:d.InputOffset()])
		if len(raw) > 0 && raw[0] == ':' {
			raw = bytes.TrimSpace(raw[1:])
		}
		name := key.(string)
		last[name] = len(members)
		members = append(members, member{key: name, raw: raw})
	}
	if _, err := d.Token(); err != nil {
		return nil, false, err
	}
	out := members[:0]
	for i, m := range members {
		if last[m.key] == i {
			out = append(out, m)
		}
	}
	return out, len(out) != len(members), nil
}

type member struct {
	key string
	raw borrowedJSON
}

func fieldValue(value reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if value.IsValid() && value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return reflect.Value{}
			}
			value = value.Elem()
		}
		if !value.IsValid() {
			return reflect.Value{}
		}
		value = value.Field(i)
	}
	return value
}

func containsStruct(typ reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[typ] {
		return false
	}
	seen[typ] = true
	if typ.Implements(unmarshalerType) {
		return false
	}
	if typ.Kind() != reflect.Pointer && reflect.PointerTo(typ).Implements(unmarshalerType) {
		return false
	}
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return containsStruct(typ.Elem(), seen)
	case reflect.Struct, reflect.Interface:
		return true
	}
	return false
}

type field struct {
	typ   reflect.Type
	index []int
}

type candidate struct {
	index  []int
	typ    reflect.Type
	depth  int
	tagged bool
}

// fieldsFor follows encoding/json's embedded-field dominance rules, retaining
// only exact declared names. Equal-depth conflicts disappear unless one tagged
// field wins. Types are visited per path so repeated embeddings remain ambiguous.
func fieldsFor(typ reflect.Type) map[string]field {
	if cached, ok := schemas.Load(typ); ok {
		return cached.(map[string]field)
	}
	candidates := map[string][]candidate{}
	var visit func(reflect.Type, []int, map[reflect.Type]bool)
	visit = func(t reflect.Type, path []int, active map[reflect.Type]bool) {
		if active[t] {
			return
		}
		active[t] = true
		defer delete(active, t)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if !f.IsExported() && (!f.Anonymous || ft.Kind() != reflect.Struct) {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name := strings.Split(tag, ",")[0]
			if !validTag(name) {
				name = ""
			}
			if f.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				visit(ft, append(append([]int(nil), path...), i), active)
				continue
			}
			tagged := name != ""
			if name == "" {
				name = f.Name
			}
			candidates[name] = append(candidates[name], candidate{index: append(append([]int(nil), path...), i), typ: f.Type, depth: len(path), tagged: tagged})
		}
	}
	visit(typ, nil, map[reflect.Type]bool{})
	fields := map[string]field{}
	for name, items := range candidates {
		best := items[0]
		count := 1
		for _, item := range items[1:] {
			if item.depth < best.depth || item.depth == best.depth && item.tagged && !best.tagged {
				best = item
				count = 1
			} else if item.depth == best.depth && item.tagged == best.tagged {
				count++
			}
		}
		if count == 1 {
			fields[name] = field{typ: best.typ, index: best.index}
		}
	}
	actual, _ := schemas.LoadOrStore(typ, fields)
	return actual.(map[string]field)
}

func validTag(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", r) && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
