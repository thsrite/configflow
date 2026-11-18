package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

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
