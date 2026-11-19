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
var schemas sync.Map // reflect.Type -> map[string]reflect.Type (immutable)

// Unmarshal is encoding/json.Unmarshal with case-sensitive struct field names.
// Unknown members are ignored. Repeated members of ordinary DTO objects use the
// last complete value, rather than merging earlier objects into the last one.
// Custom UnmarshalJSON methods receive the original bytes, including duplicates;
// their implementations must use this package when decoding ordinary DTOs.
func Unmarshal(data []byte, dst any) error {
	data, err := prepare(data, reflect.TypeOf(dst))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// Decode is Unmarshal with numbers in interface values kept as json.Number.
// It accepts exactly one JSON value, including when dst uses a custom codec.
func Decode(data []byte, dst any) error {
	data, err := prepare(data, reflect.TypeOf(dst))
	if err != nil {
		return err
	}
	if !json.Valid(data) {
		var raw json.RawMessage
		return json.Unmarshal(data, &raw)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(dst)
}

// borrowedJSON is confined to preparation: encoding/json passes each value's
// input span to custom codecs. Keeping that span avoids copying megabytes of
// unrelated configuration merely to discard an unknown member. Nothing escapes
// into the destination until its own native/custom decoder takes ownership.
type borrowedJSON []byte

// UnmarshalJSON borrows a value only until preparation finishes.
func (b *borrowedJSON) UnmarshalJSON(data []byte) error { *b = data; return nil }

func prepare(data []byte, typ reflect.Type) ([]byte, error) {
	if typ == nil {
		return data, nil
	}
	// Match encoding/json's custom-codec dispatch before examining its fields.
	if typ.Implements(unmarshalerType) || typ.Kind() != reflect.Pointer && reflect.PointerTo(typ).Implements(unmarshalerType) {
		return data, nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
		if reflect.PointerTo(typ).Implements(unmarshalerType) {
			return data, nil
		}
	}
	text := bytes.TrimSpace(data)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) {
		return data, nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		if text[0] != '{' {
			return data, nil
		}
		var members map[string]borrowedJSON
		if err := json.Unmarshal(data, &members); err != nil {
			return nil, err
		}
		fields := fieldsFor(typ)
		for key, raw := range members {
			field, ok := fields[key]
			if !ok {
				delete(members, key)
				continue
			}
			next, err := prepare(raw, field)
			if err != nil {
				return nil, err
			}
			members[key] = borrowedJSON(next)
		}
		// Do not re-marshal RawMessage: that would compact opaque values and change
		// the bytes custom codecs use as provenance, signatures or number tokens.
		var out bytes.Buffer
		out.WriteByte('{')
		first := true
		for key, raw := range members {
			if !first {
				out.WriteByte(',')
			}
			first = false
			k, _ := json.Marshal(key)
			out.Write(k)
			out.WriteByte(':')
			out.Write(raw)
		}
		out.WriteByte('}')
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
		for i, raw := range values {
			if i > 0 {
				out.WriteByte(',')
			}
			next, err := prepare(raw, typ.Elem())
			if err != nil {
				return nil, err
			}
			out.Write(next)
		}
		out.WriteByte(']')
		return out.Bytes(), nil
	case reflect.Map:
		if text[0] != '{' || !containsStruct(typ.Elem(), map[reflect.Type]bool{}) {
			return data, nil
		}
		var members map[string]borrowedJSON
		if err := json.Unmarshal(data, &members); err != nil {
			return nil, err
		}
		var out bytes.Buffer
		out.WriteByte('{')
		first := true
		for key, raw := range members {
			if !first {
				out.WriteByte(',')
			}
			first = false
			k, _ := json.Marshal(key)
			out.Write(k)
			out.WriteByte(':')
			next, err := prepare(raw, typ.Elem())
			if err != nil {
				return nil, err
			}
			out.Write(next)
		}
		out.WriteByte('}')
		return out.Bytes(), nil
	}
	return data, nil
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
	case reflect.Struct:
		return true
	}
	return false
}

type candidate struct {
	typ    reflect.Type
	depth  int
	tagged bool
}

// fieldsFor follows encoding/json's embedded-field dominance rules, retaining
// only exact declared names. Equal-depth conflicts disappear unless one tagged
// field wins. Types are visited per path so repeated embeddings remain ambiguous.
func fieldsFor(typ reflect.Type) map[string]reflect.Type {
	if cached, ok := schemas.Load(typ); ok {
		return cached.(map[string]reflect.Type)
	}
	candidates := map[string][]candidate{}
	var visit func(reflect.Type, int, map[reflect.Type]bool)
	visit = func(t reflect.Type, depth int, active map[reflect.Type]bool) {
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
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if !validTag(name) {
				name = ""
			}
			if f.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				visit(ft, depth+1, active)
				continue
			}
			tagged := name != ""
			if name == "" {
				name = f.Name
			}
			candidates[name] = append(candidates[name], candidate{f.Type, depth, tagged})
		}
	}
	visit(typ, 0, map[reflect.Type]bool{})
	fields := map[string]reflect.Type{}
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
			fields[name] = best.typ
		}
	}
	actual, _ := schemas.LoadOrStore(typ, fields)
	return actual.(map[string]reflect.Type)
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
