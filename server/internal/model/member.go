package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

// recordHasMembers follows record presence rules without serializing member
// payloads. In particular, preserved null/R5 members count, while a pointer
// removed after loading and an explicitly cleared zero value do not.
func recordHasMembers(rec any, raw *Raw) bool {
	value, schema, err := recordValue(rec)
	if err != nil || ownRaw(value, schema, raw) != nil {
		return false
	}
	if schema.extra >= 0 && value.Field(schema.extra).Interface().(Fields).Len() > 0 {
		return true
	}
	for _, original := range raw.members {
		field, known := schema.byName[original.name]
		if !known {
			if schema.extra < 0 {
				return true
			}
			continue
		}
		current := value.Field(field.index)
		if original.cleared && isZero(current) {
			continue
		}
		if current.Kind() == reflect.Pointer && current.IsNil() && !original.invalid && !bytes.Equal(original.baseline, []byte("null")) {
			continue
		}
		return true
	}
	for _, field := range schema.fields {
		if !isZero(value.Field(field.index)) {
			return true
		}
	}
	return false
}

// MemberJSON reads one effective declared member without encoding the enclosing
// record. Pass that member's current typed field value. Unchanged nulls, wrong
// types and numeric notation retain their original JSON, exactly as EncodeRecord
// does; edited and explicitly cleared fields use their current value. The result
// is caller-owned. This is for evidence checks in hot readers, not mutations.
func (r Raw) MemberJSON(name string, current any) (json.RawMessage, bool, error) {
	value := reflect.ValueOf(current)
	if r.typ != nil {
		schema := schemaFor(r.typ)
		field, known := schema.byName[name]
		if !known || !value.IsValid() || value.Type() != r.typ.Field(field.index).Type {
			return nil, false, fmt.Errorf("%w: member field mismatch", ErrType)
		}
	}
	for _, original := range r.members {
		if original.name != name {
			continue
		}
		if original.cleared && isZero(value) {
			return nil, false, nil
		}
		encoded, err := Marshal(current)
		if err != nil {
			return nil, false, err
		}
		unchanged := !original.cleared && ((original.invalid && isZero(value)) || (!original.invalid && bytes.Equal(encoded, original.baseline)))
		if unchanged {
			return bytes.Clone(original.value), true, nil
		}
		if value.Kind() == reflect.Pointer && value.IsNil() {
			return nil, false, nil
		}
		return encoded, true, nil
	}
	if isZero(value) {
		return nil, false, nil
	}
	encoded, err := Marshal(current)
	return encoded, err == nil, err
}
