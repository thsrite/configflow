// Package jsondecode decodes JSON DTOs with exact member names on
// encoding/json/v2. It keeps encoding/json's other decoding rules (null,
// merging into existing values, error types). A member repeated in an object
// bound for an ordinary struct or map keeps only its last complete value
// (earlier ones are neither merged nor checked); values owned by
// custom codecs (json.Unmarshaler and friends, json.RawMessage) receive their
// original bytes, repeats included, and apply their own rules to them.
// Unknown members are ignored.
package jsondecode

import (
	"bytes"
	"encoding"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
	"sync"
)

// options are encoding/json's own semantics with case-sensitive member
// names. Repeats stay allowed: structs and maps get them resolved by
// lastMemberHook, and distinct names that meet in one map key ("01" and "1"
// in a map[int]T) keep the later value, as in encoding/json.
var options = jsonv2.JoinOptions(json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false))

// numbers keeps numbers in interface values as json.Number
// (json.Decoder.UseNumber).
var numbers = jsonv2.UnmarshalFromFunc(rawNumber)

// Unmarshal is encoding/json.Unmarshal with case-sensitive struct field names.
// Unknown members are ignored. A member repeated in an object bound for an
// ordinary struct or map keeps only its last complete value.
func Unmarshal(data []byte, dst any) error {
	if repeats(data) {
		return jsonv2.Unmarshal(data, dst, options, jsonv2.WithUnmarshalers(lastMemberHook()))
	}
	return jsonv2.Unmarshal(data, dst, options)
}

// Decode is Unmarshal with numbers in interface values kept as json.Number.
// It accepts exactly one JSON value.
func Decode(data []byte, dst any) error {
	unmarshalers := numbers
	if repeats(data) {
		unmarshalers = jsonv2.JoinUnmarshalers(lastMemberHook(), numbers)
	}
	return jsonv2.Unmarshal(data, dst, options, jsonv2.WithUnmarshalers(unmarshalers))
}

// repeats reports whether data is valid JSON only with repeated member names.
func repeats(data []byte) bool {
	value := jsontext.Value(data)
	return !value.IsValid(jsontext.AllowInvalidUTF8(true)) &&
		value.IsValid(jsontext.AllowInvalidUTF8(true), jsontext.AllowDuplicateNames(true))
}

func rawNumber(d *jsontext.Decoder, v *any) error {
	if d.PeekKind() != '0' {
		return errors.ErrUnsupported
	}
	if held := reflect.ValueOf(*v); held.Kind() == reflect.Pointer && !held.IsNil() {
		return errors.ErrUnsupported // encoding/json decodes into the held pointer
	}
	value, err := d.ReadValue()
	if err != nil {
		return err
	}
	*v = json.Number(value)
	return nil
}

// lastMemberHook decodes every object bound for an ordinary struct or map
// with its repeated members reduced to their last occurrence, then lets the
// default decoder fill the value (and visit its members, where the hook
// applies again). It is built per call: entered is the value being decoded by
// the hook itself, which must take the default path.
func lastMemberHook() *jsonv2.Unmarshalers {
	var entered any
	return jsonv2.UnmarshalFromFunc(func(d *jsontext.Decoder, v any) error {
		if v == entered {
			entered = nil
			return errors.ErrUnsupported
		}
		if d.PeekKind() != '{' || !plainObject(reflect.TypeOf(v).Elem()) {
			return errors.ErrUnsupported
		}
		raw, err := d.ReadValue()
		if err != nil {
			return err
		}
		object, err := lastMembers(raw)
		if err != nil {
			return err
		}
		entered = v
		return jsonv2.Unmarshal(object, v, d.Options())
	})
}

var plainObjects sync.Map // reflect.Type -> bool

var codecTypes = []reflect.Type{
	reflect.TypeFor[json.Unmarshaler](),
	reflect.TypeFor[jsonv2.UnmarshalerFrom](),
	reflect.TypeFor[encoding.TextUnmarshaler](),
}

// plainObject reports whether the default decoder fills t member by member:
// a struct or map without a custom codec.
func plainObject(t reflect.Type) bool {
	if t.Kind() != reflect.Struct && t.Kind() != reflect.Map {
		return false
	}
	if known, ok := plainObjects.Load(t); ok {
		return known.(bool)
	}
	plain := true
	for _, codec := range codecTypes {
		if t.Implements(codec) || reflect.PointerTo(t).Implements(codec) {
			plain = false
		}
	}
	plainObjects.Store(t, plain)
	return plain
}

// lastMembers returns one valid JSON object with every repeated member name
// reduced to its last occurrence, kept in that occurrence's position. Member
// values keep their bytes (names are re-quoted); an object without repeats is
// returned unchanged.
func lastMembers(object []byte) ([]byte, error) {
	d := jsontext.NewDecoder(bytes.NewReader(object), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	if _, err := d.ReadToken(); err != nil {
		return nil, err
	}
	type member struct {
		key   string
		value []byte
	}
	var members []member
	last := map[string]int{}
	for d.PeekKind() != '}' {
		// Invalid UTF-8 in a name reads as U+FFFD, as encoding/json reads it.
		name, err := d.ReadToken()
		if err != nil {
			return nil, err
		}
		m := member{key: name.String()}
		value, err := d.ReadValue()
		if err != nil {
			return nil, err
		}
		m.value = bytes.Clone(value)
		last[m.key] = len(members)
		members = append(members, m)
	}
	if len(last) == len(members) {
		return object, nil
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, m := range members {
		if last[m.key] != i {
			continue
		}
		if out.Len() > 1 {
			out.WriteByte(',')
		}
		name, err := jsontext.AppendQuote(out.AvailableBuffer(), m.key)
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(m.value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
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
// unrelated configuration merely to discard an unknown member. Select copies
// each value before it can escape.
type borrowedJSON []byte

// UnmarshalJSON borrows a value only until the current scan finishes.
func (b *borrowedJSON) UnmarshalJSON(data []byte) error { *b = data; return nil }
