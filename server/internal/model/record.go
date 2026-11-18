package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
)

type rawMember struct {
	jsonMember
	baseline       []byte
	known, invalid bool
	// cleared records ClearMember: written only if the field is non-zero.
	cleared bool
}

// Raw is immutable record provenance: original bytes, order, typed baselines
// and invalid-field diagnostics. Copying a record may safely share its Raw:
// nothing it references is modified in place (ClearMember gives the record
// a new member list).
type Raw struct {
	typ     reflect.Type
	source  jsonObject
	members []rawMember
	issues  []FieldIssue
}

// Issues returns this record's direct invalid-field diagnostics, independently
// owned by caller. DecodeDocument additionally includes nested record issues.
func (r Raw) Issues() []FieldIssue { return slices.Clone(r.issues) }

type recordField struct {
	name  string
	index int
}
type recordSchema struct {
	fields     []recordField
	byName     map[string]recordField
	extra, raw int
	err        error
}

var schemas sync.Map
var rawType = reflect.TypeFor[Raw]()
var fieldsType = reflect.TypeFor[Fields]()
var jsonNumberType = reflect.TypeFor[json.Number]()
var intType = reflect.TypeFor[Int]()
var rawMessageType = reflect.TypeFor[json.RawMessage]()
var unmarshalerType = reflect.TypeFor[json.Unmarshaler]()

func schemaFor(typ reflect.Type) *recordSchema {
	if cached, ok := schemas.Load(typ); ok {
		return cached.(*recordSchema)
	}
	schema := &recordSchema{byName: map[string]recordField{}, extra: -1, raw: -1}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type == rawType {
			if schema.raw >= 0 {
				schema.err = fmt.Errorf("%w: multiple Raw members", ErrType)
				break
			}
			schema.raw = i
			continue
		}
		if !field.IsExported() {
			continue
		}
		if field.Type == fieldsType {
			if schema.extra >= 0 {
				schema.err = fmt.Errorf("%w: multiple Fields members", ErrType)
				break
			}
			schema.extra = i
			continue
		}
		options := strings.Split(field.Tag.Get("json"), ",")
		tag := options[0]
		// R3 decides presence; any other option (",string") would be ignored
		// silently, so it is a declaration error.
		for _, option := range options[1:] {
			if option != "omitempty" && option != "omitzero" {
				schema.err = fmt.Errorf("%w: unsupported json tag option", ErrType)
				break
			}
		}
		if schema.err != nil {
			break
		}
		if tag == "-" {
			continue
		}
		if tag == "" {
			tag = field.Name
		}
		if field.Anonymous {
			schema.err = fmt.Errorf("%w: embedded record fields require an explicit record type", ErrType)
			break
		}
		if _, exists := schema.byName[tag]; exists {
			schema.err = fmt.Errorf("%w: duplicate record tag", ErrJSON)
			break
		}
		if err := storedType(field.Type); err != nil {
			schema.err = err
			break
		}
		item := recordField{tag, i}
		schema.fields = append(schema.fields, item)
		schema.byName[tag] = item
	}
	actual, _ := schemas.LoadOrStore(typ, schema)
	return actual.(*recordSchema)
}

func recordValue(rec any) (reflect.Value, *recordSchema, error) {
	value := reflect.ValueOf(rec)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return reflect.Value{}, nil, fmt.Errorf("%w: record must be a non-nil struct pointer", ErrType)
	}
	value = value.Elem()
	schema := schemaFor(value.Type())
	if schema.err != nil {
		return reflect.Value{}, nil, schema.err
	}
	if schema.raw < 0 {
		return reflect.Value{}, nil, fmt.Errorf("%w: record must declare a Raw member", ErrType)
	}
	return value, schema, nil
}

// ownRaw requires the record's own Raw member, so diagnostics stay reachable
// for DecodeDocument and provenance cannot be borrowed from another value.
func ownRaw(value reflect.Value, schema *recordSchema, raw *Raw) error {
	if raw == nil {
		return fmt.Errorf("%w: missing record provenance", ErrType)
	}
	if value.Field(schema.raw).Addr().Pointer() != reflect.ValueOf(raw).Pointer() {
		return fmt.Errorf("%w: provenance must be the record's own Raw member", ErrType)
	}
	return nil
}

func isZero(value reflect.Value) bool { return !value.IsValid() || value.IsZero() }

// DecodeRecord decodes into a fresh record. Bad member types become zero values
// plus FieldIssues; their original bytes remain in Raw. Syntax/shape errors are
// fatal and leave rec and raw untouched. It performs no logging or I/O.
func DecodeRecord(data []byte, rec any, raw *Raw) error {
	destination, schema, err := recordValue(rec)
	if err != nil {
		return err
	}
	if err := ownRaw(destination, schema, raw); err != nil {
		return err
	}
	source, err := parseObject(data)
	if err != nil {
		return err
	}
	working := reflect.New(destination.Type()).Elem()
	state := Raw{source: source, typ: destination.Type()}
	extra := Fields{state: &fieldState{source: source}}
	for _, member := range source.members {
		saved := rawMember{jsonMember: member}
		if field, known := schema.byName[member.name]; known {
			saved.known = true
			target := reflect.New(working.Field(field.index).Type())
			err := decodeValue(member.value, target.Interface())
			if err != nil {
				saved.invalid = true
				state.issues = append(state.issues, FieldIssue{member.name, IssueType})
			} else {
				working.Field(field.index).Set(target.Elem())
			}
			saved.baseline, err = Marshal(working.Field(field.index).Interface())
			if err != nil {
				return err
			}
		} else if schema.extra >= 0 {
			extra.state.entries = append(extra.state.entries, fieldEntry{member.name, member.value, true})
		}
		state.members = append(state.members, saved)
	}
	if schema.extra >= 0 {
		working.Field(schema.extra).Set(reflect.ValueOf(extra))
	}
	destination.Set(working)
	*raw = state
	return nil
}

// ClearMember records the explicit intent to drop a declared member of rec
// (its JSON name) and sets the field to its zero value: Python's pop. Assigning a Go zero value or nil cannot
// express it (design n7-n8 2.3, Clarified in N8-4f): an unchanged stored null
// stays null under R2 and a wrong-typed member keeps its bytes under R5. After
// ClearMember the member is omitted when rec is written while the field still
// holds its zero value; a non-zero value assigned later is written in the
// member's stored position (replace). The member's FieldIssues are dropped. A member
// that is not stored needs no clearing. Opaque members use Fields.Delete.
// Copies of rec made earlier are not affected.
func ClearMember(rec any, raw *Raw, member string) error {
	value, schema, err := recordValue(rec)
	if err != nil {
		return err
	}
	if err := ownRaw(value, schema, raw); err != nil {
		return err
	}
	field, known := schema.byName[member]
	if !known {
		return fmt.Errorf("%w: clear of an undeclared member %q", ErrType, member)
	}
	value.Field(field.index).SetZero()
	for index := range raw.members {
		if raw.members[index].name != member {
			continue
		}
		members := slices.Clone(raw.members)
		members[index].cleared = true
		raw.members = members
		issues := make([]FieldIssue, 0, len(raw.issues))
		for _, issue := range raw.issues {
			if issue.Path != member {
				issues = append(issues, issue)
			}
		}
		raw.issues = issues
		return nil
	}
	return nil
}

// SetMemberJSON stores data as the value of a declared member of rec, as if
// it had been read that way: a value of the member's Go type is decoded into
// the field and written back as given while the field is unchanged (R2); a
// value of another type leaves the field at its zero value, is written back as
// given (R5) and adds a FieldIssue. The member keeps its stored position or is
// appended. It is for values that arrive as JSON (imports, legacy callers);
// Go code assigns the field instead.
func SetMemberJSON(rec any, raw *Raw, member string, data json.RawMessage) error {
	value, schema, err := recordValue(rec)
	if err != nil {
		return err
	}
	if err := ownRaw(value, schema, raw); err != nil {
		return err
	}
	field, known := schema.byName[member]
	if !known {
		return fmt.Errorf("%w: set of an undeclared member %q", ErrType, member)
	}
	if err := validJSON(data); err != nil {
		return err
	}
	destination := value.Field(field.index)
	target := reflect.New(destination.Type())
	invalid := decodeValue(data, target.Interface()) != nil
	if invalid {
		destination.SetZero()
	} else {
		destination.Set(target.Elem())
	}
	baseline, err := Marshal(destination.Interface())
	if err != nil {
		return err
	}
	if raw.typ == nil {
		raw.typ = value.Type()
	}
	entry := rawMember{jsonMember: jsonMember{name: member, value: bytes.Clone(data)}, baseline: baseline, known: true, invalid: invalid}
	members := slices.Clone(raw.members)
	replaced := false
	for index := range members {
		if members[index].name == member {
			entry.jsonMember.key = members[index].key
			entry.jsonMember.leading = members[index].leading
			entry.jsonMember.separator = members[index].separator
			entry.jsonMember.trailing = members[index].trailing
			members[index] = entry
			replaced = true
		}
	}
	if !replaced {
		members = append(members, entry)
	}
	raw.members = members
	issues := make([]FieldIssue, 0, len(raw.issues)+1)
	for _, issue := range raw.issues {
		if issue.Path != member {
			issues = append(issues, issue)
		}
	}
	if invalid {
		issues = append(issues, FieldIssue{member, IssueType})
	}
	raw.issues = issues
	return nil
}

// EncodeRecord preserves existing member positions and original bytes whenever
// their typed value is unchanged. Existing non-pointer zero values stay present;
// changed pointers set to nil are removed. New non-zero fields append in struct
// order, followed by new opaque Fields. Raw itself is never mutated.
func EncodeRecord(rec any, raw *Raw) ([]byte, error) {
	value, schema, err := recordValue(rec)
	if err != nil {
		return nil, err
	}
	if err := ownRaw(value, schema, raw); err != nil {
		return nil, err
	}
	if raw.typ != nil && raw.typ != value.Type() {
		return nil, fmt.Errorf("%w: record provenance type mismatch", ErrType)
	}
	extra := Fields{}
	if schema.extra >= 0 {
		extra = value.Field(schema.extra).Interface().(Fields)
	}
	for _, name := range extra.Keys() {
		if _, known := schema.byName[name]; known {
			return nil, fmt.Errorf("%w: opaque member shadows a declared member", ErrJSON)
		}
	}
	members := make([]jsonMember, 0, len(raw.members)+len(schema.fields)+extra.Len())
	seen := map[string]bool{}
	for _, original := range raw.members {
		member := original.jsonMember
		if field, known := schema.byName[member.name]; known {
			current := value.Field(field.index)
			if original.cleared && isZero(current) {
				continue
			}
			encoded, err := Marshal(current.Interface())
			if err != nil {
				return nil, err
			}
			unchanged := !original.cleared && ((original.invalid && isZero(current)) || (!original.invalid && bytes.Equal(encoded, original.baseline)))
			if !unchanged {
				if current.Kind() == reflect.Pointer && current.IsNil() {
					continue
				}
				member.value = encoded
			}
		} else if schema.extra >= 0 {
			entry, present := extra.entry(member.name)
			if !present || !entry.original {
				continue
			}
			member.value = entry.value
		}
		members = append(members, member)
		seen[member.name] = true
	}
	for _, field := range schema.fields {
		if seen[field.name] {
			continue
		}
		// A removed original pointer must not be appended again as a new member.
		original := false
		for _, member := range raw.members {
			if member.name == field.name {
				original = true
				break
			}
		}
		if original {
			continue
		}
		current := value.Field(field.index)
		if isZero(current) {
			continue
		}
		encoded, err := Marshal(current.Interface())
		if err != nil {
			return nil, err
		}
		members = append(members, jsonMember{name: field.name, value: encoded})
		seen[field.name] = true
	}
	for _, name := range extra.Keys() {
		if !seen[name] {
			entry, _ := extra.entry(name)
			members = append(members, jsonMember{name: name, value: entry.value})
		}
	}
	return objectJSON(raw.source, members)
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// valueIssues reads only codec metadata and declared schema paths. Reading
// primitive fields of an unexported Raw uses reflect getters, never unsafe
// memory access or Interface on an unexported field.
func valueIssues(value reflect.Value, prefix string) []FieldIssue {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.CanInterface() {
		if collector, ok := value.Interface().(interface{ fieldIssues(string) []FieldIssue }); ok {
			return collector.fieldIssues(prefix)
		}
	}
	var issues []FieldIssue
	switch value.Kind() {
	case reflect.Struct:
		schema := schemaFor(value.Type())
		if schema.err != nil {
			return nil
		}
		if schema.raw >= 0 {
			raw := value.Field(schema.raw)
			local := raw.FieldByName("issues")
			for i := 0; i < local.Len(); i++ {
				issue := local.Index(i)
				issues = append(issues, FieldIssue{joinPath(prefix, issue.FieldByName("Path").String()), IssueKind(issue.FieldByName("Kind").String())})
			}
		}
		for _, field := range schema.fields {
			issues = append(issues, valueIssues(value.Field(field.index), joinPath(prefix, field.name))...)
		}
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return nil
		}
		for i := 0; i < value.Len(); i++ {
			issues = append(issues, valueIssues(value.Index(i), fmt.Sprintf("%s[%d]", prefix, i))...)
		}
	}
	return issues
}

// storedType enforces R6/R8/R9 at record declarations, before data is decoded.
// Fixed-size arrays are not stored types: encoding/json silently drops extra
// stored elements, so a later edit would lose them.
func storedType(typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == intType || typ == jsonNumberType || typ == rawMessageType {
		return nil
	}
	switch typ.Kind() {
	case reflect.Bool, reflect.String, reflect.Float64:
		return nil
	case reflect.Slice:
		return storedType(typ.Elem())
	case reflect.Struct:
		if !typ.Implements(marshalerType) || !reflect.PointerTo(typ).Implements(unmarshalerType) {
			return fmt.Errorf("%w: nested records must implement JSON codecs", ErrType)
		}
		if container, ok := reflect.Zero(typ).Interface().(interface{ storedValueType() reflect.Type }); ok {
			return storedType(container.storedValueType())
		}
		return nil
	default:
		return fmt.Errorf("%w: stored fields require explicit model types", ErrType)
	}
}

func decodeValue(data []byte, target any) error {
	return decodeInto(data, reflect.ValueOf(target).Elem())
}

// decodeInto decodes into an addressable destination. encoding/json accepts a
// quoted numeric string for json.Number, so exact-number members remain strict.
// String and Int use the shared R5 scalar rules, also within pointers/lists.
func decodeInto(data []byte, destination reflect.Value) error {
	typ := destination.Type()
	pointers := 0
	for typ.Kind() == reflect.Pointer {
		pointers++
		typ = typ.Elem()
	}
	text := bytes.TrimSpace(data)
	if pointers > 0 && bytes.Equal(text, []byte("null")) {
		destination.SetZero()
		return nil
	}
	switch {
	case typ == jsonNumberType:
		if len(text) == 0 || (text[0] != '-' && (text[0] < '0' || text[0] > '9')) {
			return ErrType
		}
	case typ.Kind() == reflect.String:
		var value String
		if err := value.UnmarshalJSON(data); err != nil {
			return err
		}
		decoded := reflect.New(typ).Elem()
		decoded.SetString(string(value))
		for range pointers {
			wrapped := reflect.New(decoded.Type())
			wrapped.Elem().Set(decoded)
			decoded = wrapped
		}
		destination.Set(decoded)
		return nil
	case typ.Kind() == reflect.Slice && typ != rawMessageType:
		var items []json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			return err
		}
		if items == nil {
			destination.SetZero()
			return nil
		}
		value := reflect.MakeSlice(typ, len(items), len(items))
		for i, item := range items {
			if err := decodeInto(item, value.Index(i)); err != nil {
				return err
			}
		}
		for range pointers {
			wrapped := reflect.New(value.Type())
			wrapped.Elem().Set(value)
			value = wrapped
		}
		destination.Set(value)
		return nil
	}
	return json.Unmarshal(data, destination.Addr().Interface())
}
