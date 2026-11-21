package model

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// Stored decoding (O18 T1a). Plain records (Document, Profile, Shared,
// System, and later the nested records) are ordinary structs decoded with
// encoding/json/v2. Data read from storage goes through DecodeStored, which
// is lenient per leaf: a value of the wrong type becomes the zero value plus
// a value-free FieldIssue, so one bad value never makes a row unreadable.
// Input from HTTP or imports is decoded strictly (SetMember, UnmarshalJSON).
//
// Records not yet migrated keep their own UnmarshalJSON (DecodeRecord with
// R5 fallback); DecodeStored treats them as leaves and collects their nested
// issues with Issues.

// IssueUnknown means a stored member is not declared by its record. Its value
// is dropped on load and is not written back.
const IssueUnknown IssueKind = "unknown"

// MemberTypeError reports input whose JSON type does not fit a declared
// member of a plain record. Record names the record ("profile", "shared",
// "system", "document"); Value is the rejected input, caller-owned.
type MemberTypeError struct {
	Record string
	Member string
	Value  json.RawMessage
	Err    error
}

func (e *MemberTypeError) Error() string {
	return fmt.Sprintf("%s.%s: %v", e.Record, e.Member, e.Err)
}

func (e *MemberTypeError) Unwrap() error { return ErrType }

// plainRecord marks the migrated records. Their UnmarshalJSON is strict, so
// the stored-data hooks below skip them and decode their members instead.
type plainRecord interface{ plainRecord() }

// issueSink collects the leaf issues of one DecodeStored call.
type issueSink struct {
	root   reflect.Type
	prefix string
	issues []FieldIssue
}

func (s *issueSink) add(dec *jsontext.Decoder, kind IssueKind) {
	s.issues = append(s.issues, FieldIssue{Path: pointerPath(s.root, s.prefix, dec.StackPointer()), Kind: kind})
}

// pointerPath turns a JSON pointer into a value-free schema path: struct
// members by name, list elements by index, map keys as {*}.
func pointerPath(typ reflect.Type, prefix string, pointer jsontext.Pointer) string {
	path := prefix
	for token := range pointer.Tokens() {
		for typ != nil && typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		switch {
		case typ != nil && typ.Kind() == reflect.Struct:
			field, ok := plainFields(typ)[token]
			if !ok {
				return joinPath(path, token)
			}
			path = joinPath(path, token)
			typ = field.typ
		case typ != nil && (typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array):
			path += "[" + token + "]"
			typ = typ.Elem()
		default:
			if typ != nil && typ.Kind() == reflect.Map {
				typ = typ.Elem()
			} else {
				typ = nil
			}
			path += "{*}"
		}
	}
	return path
}

type plainField struct {
	index []int
	typ   reflect.Type
}

var plainFieldCache sync.Map

// plainFields maps the JSON member names of a plain struct to its fields.
func plainFields(typ reflect.Type) map[string]plainField {
	if cached, ok := plainFieldCache.Load(typ); ok {
		return cached.(map[string]plainField)
	}
	fields := map[string]plainField{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = plainField{index: field.Index, typ: field.Type}
	}
	actual, _ := plainFieldCache.LoadOrStore(typ, fields)
	return actual.(map[string]plainField)
}

// storedUnmarshalers are the lenient leaf rules for stored data.
func storedUnmarshalers(sink *issueSink) *jsonv2.Unmarshalers {
	return jsonv2.JoinUnmarshalers(
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *string) error {
			switch dec.PeekKind() {
			case '"':
				token, err := dec.ReadToken()
				if err != nil {
					return err
				}
				*v = token.String()
				return nil
			case '0':
				// Python stored numbers in text members; keep their literal.
				value, err := dec.ReadValue()
				if err != nil {
					return err
				}
				*v = string(value)
				return nil
			}
			return skipInvalid(dec, sink, func() { *v = "" })
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *bool) error {
			switch dec.PeekKind() {
			case 't', 'f':
				token, err := dec.ReadToken()
				if err != nil {
					return err
				}
				*v = token.Bool()
				return nil
			}
			return skipInvalid(dec, sink, func() { *v = false })
		}),
		// A stored null switch reads as off: Python's get(key, True) returns
		// None, which is falsy (the flag-truthiness ruling).
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v **bool) error {
			if dec.PeekKind() != 'n' {
				return errors.ErrUnsupported
			}
			if _, err := dec.ReadToken(); err != nil {
				return err
			}
			off := false
			*v = &off
			return nil
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *int64) error {
			return storedInt(dec, sink, func(n int64) { *v = n })
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *int) error {
			return storedInt(dec, sink, func(n int64) { *v = int(n) })
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *float64) error {
			if dec.PeekKind() == '0' {
				token, err := dec.ReadToken()
				if err != nil {
					return err
				}
				value, err := token.Float()
				if err != nil {
					*v = 0
					sink.add(dec, IssueType)
					return nil
				}
				*v = value
				return nil
			}
			return skipInvalid(dec, sink, func() { *v = 0 })
		}),
		// A list, object or plain record stored with another JSON kind is the
		// zero value plus an issue; matching kinds decode normally.
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v any) error {
			typ := reflect.TypeOf(v).Elem()
			kind := dec.PeekKind()
			if kind == 'n' {
				return errors.ErrUnsupported
			}
			if typ.Kind() == reflect.Slice && kind == '[' && selfDecoding(typ.Elem()) {
				return decodeRecordList(dec, sink, reflect.ValueOf(v).Elem())
			}
			switch {
			case typ.Kind() == reflect.Slice && typ.Elem().Kind() != reflect.Uint8 && kind != '[',
				typ.Kind() == reflect.Map && kind != '{',
				typ.Kind() == reflect.Struct && isPlain(typ) && kind != '{':
				return skipInvalid(dec, sink, func() { reflect.ValueOf(v).Elem().SetZero() })
			}
			return errors.ErrUnsupported
		}),
		// Records not yet migrated decode themselves (R5 inside); a value they
		// refuse (not an object, for example) is zero plus an issue.
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v json.Unmarshaler) error {
			if _, plain := v.(plainRecord); plain {
				return errors.ErrUnsupported
			}
			value, err := dec.ReadValue()
			if err != nil {
				return err
			}
			if err := v.UnmarshalJSON(bytes.Clone(value)); err != nil {
				reflect.ValueOf(v).Elem().SetZero()
				sink.add(dec, IssueType)
			}
			return nil
		}),
	)
}

// skipInvalid consumes a value of the wrong type. Null is the zero value
// without an issue, like encoding/json.
func skipInvalid(dec *jsontext.Decoder, sink *issueSink, zero func()) error {
	kind := dec.PeekKind()
	if err := dec.SkipValue(); err != nil {
		return err
	}
	zero()
	if kind != 'n' {
		sink.add(dec, IssueType)
	}
	return nil
}

// storedInt accepts integral numbers in any notation (60, 60.0, 6e1) and
// decimal integer text ("60"), as Python-written data may hold.
func storedInt(dec *jsontext.Decoder, sink *issueSink, set func(int64)) error {
	switch dec.PeekKind() {
	case '0', '"':
		value, err := dec.ReadValue()
		if err != nil {
			return err
		}
		var n Int
		if n.UnmarshalJSON(value) != nil {
			set(0)
			sink.add(dec, IssueType)
			return nil
		}
		set(int64(n))
		return nil
	}
	return skipInvalid(dec, sink, func() { set(0) })
}

// DecodeStored decodes stored JSON into v (a non-nil pointer) with the lenient
// leaf rules. Path prefixes every issue. Only syntax errors (invalid JSON,
// duplicate member names) are returned as errors. Unknown members of plain
// records are dropped and reported as IssueUnknown; issues of records that are
// not yet migrated are included.
func DecodeStored(data []byte, v any, prefix string) ([]FieldIssue, error) {
	target := reflect.ValueOf(v)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return nil, fmt.Errorf("%w: stored target must be a non-nil pointer", ErrType)
	}
	sink := &issueSink{root: target.Type().Elem(), prefix: prefix}
	if err := jsonv2.Unmarshal(data, v, jsonv2.WithUnmarshalers(storedUnmarshalers(sink)), jsontext.AllowInvalidUTF8(true)); err != nil {
		var syntax *jsontext.SyntacticError
		if errors.As(err, &syntax) {
			return nil, fmt.Errorf("%w: %v", ErrJSON, err)
		}
		return nil, err
	}
	clipSlices(target.Elem())
	issues := sink.issues
	if hasPlain(sink.root) {
		unknown, err := unknownMembers(data, sink.root, prefix)
		if err != nil {
			return nil, err
		}
		issues = append(issues, unknown...)
	}
	issues = append(issues, legacyIssues(target.Elem(), prefix)...)
	return issues, nil
}

// legacyIssues collects the FieldIssues recorded by records that still decode
// themselves, walking through plain structs and lists.
func legacyIssues(value reflect.Value, prefix string) []FieldIssue {
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	typ := value.Type()
	if isPlain(typ) {
		var issues []FieldIssue
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if !field.IsExported() || name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			issues = append(issues, legacyIssues(value.Field(i), joinPath(prefix, name))...)
		}
		return issues
	}
	if (typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array) && !hasPlain(typ) {
		return valueIssues(value, prefix)
	}
	if typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		var issues []FieldIssue
		for i := 0; i < value.Len(); i++ {
			issues = append(issues, legacyIssues(value.Index(i), fmt.Sprintf("%s[%d]", prefix, i))...)
		}
		return issues
	}
	return valueIssues(value, prefix)
}

var plainRecordType = reflect.TypeFor[plainRecord]()

// isPlain reports a struct decoded by encoding/json/v2 member by member.
func isPlain(typ reflect.Type) bool {
	if typ.Kind() != reflect.Struct {
		return false
	}
	if reflect.PointerTo(typ).Implements(plainRecordType) {
		return true
	}
	return !reflect.PointerTo(typ).Implements(unmarshalerType)
}

var hasPlainCache sync.Map

// hasPlain reports whether a value of typ can contain a plain struct.
func hasPlain(typ reflect.Type) bool {
	if cached, ok := hasPlainCache.Load(typ); ok {
		return cached.(bool)
	}
	result := typeHasPlain(typ, map[reflect.Type]bool{})
	hasPlainCache.Store(typ, result)
	return result
}

func typeHasPlain(typ reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[typ] {
		return false
	}
	seen[typ] = true
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
			return false
		}
		return typeHasPlain(typ.Elem(), seen)
	case reflect.Struct:
		return isPlain(typ)
	}
	return false
}

// unknownMembers walks data along typ and reports members of plain structs
// that the struct does not declare. Records that decode themselves are skipped.
func unknownMembers(data []byte, typ reflect.Type, prefix string) ([]FieldIssue, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowInvalidUTF8(true))
	var issues []FieldIssue
	err := walkUnknown(dec, typ, prefix, &issues)
	return issues, err
}

func walkUnknown(dec *jsontext.Decoder, typ reflect.Type, path string, issues *[]FieldIssue) error {
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == nil || !hasPlain(typ) {
		return dec.SkipValue()
	}
	kind := dec.PeekKind()
	switch {
	case kind == '{' && typ.Kind() == reflect.Struct:
		fields := plainFields(typ)
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		for dec.PeekKind() != '}' {
			token, err := dec.ReadToken()
			if err != nil {
				return err
			}
			name := token.String()
			field, known := fields[name]
			if !known {
				*issues = append(*issues, FieldIssue{Path: joinPath(path, name), Kind: IssueUnknown})
				if err := dec.SkipValue(); err != nil {
					return err
				}
				continue
			}
			if err := walkUnknown(dec, field.typ, joinPath(path, name), issues); err != nil {
				return err
			}
		}
		_, err := dec.ReadToken()
		return err
	case kind == '{' && typ.Kind() == reflect.Map:
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		for dec.PeekKind() != '}' {
			if _, err := dec.ReadToken(); err != nil {
				return err
			}
			if err := walkUnknown(dec, typ.Elem(), path+"{*}", issues); err != nil {
				return err
			}
		}
		_, err := dec.ReadToken()
		return err
	case kind == '[' && (typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array):
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		for i := 0; dec.PeekKind() != ']'; i++ {
			if err := walkUnknown(dec, typ.Elem(), fmt.Sprintf("%s[%d]", path, i), issues); err != nil {
				return err
			}
		}
		_, err := dec.ReadToken()
		return err
	}
	return dec.SkipValue()
}

// strictOptions decode input from HTTP and imports: exact member names,
// unknown members dropped (records do not keep them), no leaf leniency.
var strictOptions = jsontext.AllowInvalidUTF8(true)

// decodeMember strictly decodes data into a declared member of a plain
// record. A type mismatch is a MemberTypeError; the field is not modified.
func decodeMember(record string, rec any, member string, data []byte) error {
	value := reflect.ValueOf(rec).Elem()
	field, known := plainFields(value.Type())[member]
	if !known {
		return fmt.Errorf("%w: set of an undeclared member %q", ErrType, member)
	}
	if err := validJSON(data); err != nil {
		return err
	}
	target := reflect.New(field.typ)
	if err := decodeStrict(data, target.Interface()); err != nil {
		return &MemberTypeError{Record: record, Member: member, Value: bytes.Clone(data), Err: err}
	}
	clipSlices(target.Elem())
	value.FieldByIndex(field.index).Set(target.Elem())
	return nil
}

// clearMember sets a declared member of a plain record to its zero value.
func clearMember(rec any, member string) error {
	value := reflect.ValueOf(rec).Elem()
	field, known := plainFields(value.Type())[member]
	if !known {
		return fmt.Errorf("%w: clear of an undeclared member %q", ErrType, member)
	}
	value.FieldByIndex(field.index).SetZero()
	return nil
}

// DecodeStoredMember decodes one stored member of a plain record (a storage
// row) leniently. known is false for a member the record does not declare;
// the value is then ignored and reported as IssueUnknown by the caller.
func DecodeStoredMember(rec any, member string, data []byte, path string) (known bool, issues []FieldIssue, err error) {
	value := reflect.ValueOf(rec)
	if value.Kind() != reflect.Pointer || value.IsNil() || !isPlain(value.Type().Elem()) {
		return false, nil, fmt.Errorf("%w: stored member target must be a plain record", ErrType)
	}
	field, known := plainFields(value.Type().Elem())[member]
	if !known {
		return false, nil, nil
	}
	target := reflect.New(field.typ)
	issues, err = DecodeStored(data, target.Interface(), path)
	if err != nil {
		return true, nil, err
	}
	value.Elem().FieldByIndex(field.index).Set(target.Elem())
	return true, issues, nil
}

// encodePlain writes a plain record (through an alias type without methods)
// with encoding/json/v2. Nested records keep their own codecs.
func encodePlain(v any) ([]byte, error) {
	data, err := jsonv2.Marshal(v, jsonv2.Deterministic(true))
	if err != nil {
		return nil, fmt.Errorf("encode model record: %w", err)
	}
	return data, nil
}


func plainFieldsOf(target any) map[string]plainField {
	return plainFields(reflect.TypeOf(target).Elem())
}

func plainFieldValue(target any, field plainField) reflect.Value {
	return reflect.ValueOf(target).Elem().FieldByIndex(field.index)
}

func newValue(typ reflect.Type) reflect.Value { return reflect.New(typ) }

// isPlainTarget reports a pointer to a plain struct.
func isPlainTarget(target any) bool {
	typ := reflect.TypeOf(target)
	return typ != nil && typ.Kind() == reflect.Pointer && typ.Elem().Kind() == reflect.Struct && isPlain(typ.Elem())
}

// StoredMember is one member of a plain record as written to storage.
type StoredMember struct {
	Name  string
	Value []byte
}

// EncodeMembers writes each member of a plain record (a pointer) as its own
// JSON value, in declaration order. omitzero members holding their zero value
// are left out, as MarshalJSON does; the values are those MarshalJSON writes.
func EncodeMembers(rec any) ([]StoredMember, error) {
	value := reflect.ValueOf(rec)
	if value.Kind() != reflect.Pointer || value.IsNil() || !isPlain(value.Type().Elem()) {
		return nil, fmt.Errorf("%w: stored members need a plain record", ErrType)
	}
	value = value.Elem()
	typ := value.Type()
	members := make([]StoredMember, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		current := value.Field(i)
		if strings.Contains(","+options+",", ",omitzero,") && current.IsZero() {
			continue
		}
		data, err := encodePlain(current.Addr().Interface())
		if err != nil {
			return nil, err
		}
		members = append(members, StoredMember{Name: name, Value: data})
	}
	return members, nil
}

// clipSlices gives every slice decoded by encoding/json/v2 (lists of plain
// records and their members) a capacity equal to its length. Snapshots lend
// shallow copies of records (repo, "Borrowed reads"): an append by one
// borrower must reallocate instead of writing a backing array that another
// borrower reads. Records that decode themselves already allocate exactly.
func clipSlices(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() {
			clipSlices(value.Elem())
		}
	case reflect.Slice:
		if value.Cap() > value.Len() && value.CanSet() {
			exact := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
			reflect.Copy(exact, value)
			value.Set(exact)
		}
		if hasPlain(value.Type().Elem()) {
			for i := 0; i < value.Len(); i++ {
				clipSlices(value.Index(i))
			}
		}
	case reflect.Struct:
		if isPlain(value.Type()) {
			for i := 0; i < value.NumField(); i++ {
				if value.Type().Field(i).IsExported() {
					clipSlices(value.Field(i))
				}
			}
		}
	}
}

// ValidJSON reports whether data is one valid JSON value without duplicate
// object member names at any depth (ErrJSON otherwise). Storage uses it for
// rows no record declares, which are dropped but must not be corrupt.
func ValidJSON(data []byte) error { return validJSON(data) }

// decodeStrict decodes input into target (a pointer). Null is accepted only
// where Go has a natural empty value for it: scalars and pointers. A list,
// object or record given as null is a type mismatch, as Python's validation
// refused it.
func decodeStrict(data []byte, target any) error {
	typ := reflect.TypeOf(target).Elem()
	// Int reads Python's integer text from storage; input must be a number.
	if typ == intType && len(bytes.TrimSpace(data)) > 0 && bytes.TrimSpace(data)[0] == '"' {
		return fmt.Errorf("%w: integer text", ErrType)
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		switch {
		case typ.Kind() == reflect.Slice && typ.Elem().Kind() != reflect.Uint8, typ.Kind() == reflect.Map, typ.Kind() == reflect.Struct:
			return fmt.Errorf("%w: null for %s", ErrType, typ)
		}
	}
	return jsonv2.Unmarshal(data, target, strictOptions)
}

// selfDecoding reports a type that is not yet migrated and decodes itself.
func selfDecoding(typ reflect.Type) bool {
	return reflect.PointerTo(typ).Implements(unmarshalerType) && !(typ.Kind() == reflect.Struct && isPlain(typ))
}

// decodeRecordList decodes a list of records that decode themselves. As for
// any member of such a record (R5), one element it refuses (for example a
// number where a record belongs) makes the whole list its zero value with one
// issue: a zero element would read as an empty record that was never stored.
func decodeRecordList(dec *jsontext.Decoder, sink *issueSink, target reflect.Value) error {
	value, err := dec.ReadValue()
	if err != nil {
		return err
	}
	elements := jsontext.NewDecoder(bytes.NewReader(value), jsontext.AllowInvalidUTF8(true))
	if _, err := elements.ReadToken(); err != nil {
		return err
	}
	var items []reflect.Value
	for elements.PeekKind() != ']' {
		item, err := elements.ReadValue()
		if err != nil {
			return err
		}
		element := reflect.New(target.Type().Elem())
		if err := element.Interface().(json.Unmarshaler).UnmarshalJSON(bytes.Clone(item)); err != nil {
			target.SetZero()
			sink.add(dec, IssueType)
			return nil
		}
		items = append(items, element.Elem())
	}
	list := reflect.MakeSlice(target.Type(), len(items), len(items))
	for i, item := range items {
		list.Index(i).Set(item)
	}
	target.Set(list)
	return nil
}
