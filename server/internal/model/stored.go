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
	// tag: the stored tag of the root value itself (DecodeStoredMember of
	// a tagged field); see storedTag.
	tag    string
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
		case typ != nil && orderedElem(typ) != nil:
			path += "{*}"
			typ = orderedElem(typ)
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
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" || inlineOption(options) {
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
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *DeviceNotes) error {
			return decodeStoredNotes(dec, sink, v)
		}),
		// A credential list (stored:"secret") keeps its text elements only:
		// another element never becomes a credential, not even as "".
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *[]string) error {
			if dec.PeekKind() != '[' || storedTag(sink, dec.StackPointer()) != secretTag {
				return errors.ErrUnsupported
			}
			if _, err := dec.ReadToken(); err != nil {
				return err
			}
			list := []string{}
			for dec.PeekKind() != ']' {
				if dec.PeekKind() == '"' {
					token, err := dec.ReadToken()
					if err != nil {
						return err
					}
					list = append(list, token.String())
					continue
				}
				if err := skipInvalid(dec, sink, func() {}); err != nil {
					return err
				}
			}
			if _, err := dec.ReadToken(); err != nil {
				return err
			}
			*v = list
			return nil
		}),
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
				// Python stored numbers in text members; keep their literal,
				// except in credentials and restrictions (see secretTag).
				value, err := dec.ReadValue()
				if err != nil {
					return err
				}
				switch {
				case storedTag(sink, dec.StackPointer()) == secretTag || sink.strictText(dec):
					*v = ""
					sink.add(dec, IssueType)
				case storedTag(sink, dec.StackPointer()) == restrictTag:
					*v = DamagedText
					sink.add(dec, IssueType)
				default:
					*v = string(value)
				}
				return nil
			}
			kind := dec.PeekKind()
			// A required restriction (null:"damaged", a router's connection
			// identity) stored as null is damaged too, not unrestricted.
			if kind == 'n' && storedTag(sink, dec.StackPointer()) == restrictTag {
				if field, ok := leafField(sink.root, dec.StackPointer()); ok && field.Tag.Get("null") == "damaged" {
					if err := dec.SkipValue(); err != nil {
						return err
					}
					*v = DamagedText
					sink.add(dec, IssueType)
					return nil
				}
			}
			return skipInvalid(dec, sink, func() {
				*v = ""
				if kind != 'n' && storedTag(sink, dec.StackPointer()) == restrictTag {
					*v = DamagedText
				}
			})
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
			case 'n':
				return skipInvalid(dec, sink, func() { *v = false })
			}
			// A switch stored with another type keeps its truth value (the
			// flag-truthiness ruling: 1 and "yes" are on, 0, "" and [] off),
			// except a device-write gate, which is off (see gateTag).
			value, err := dec.ReadValue()
			if err != nil {
				return err
			}
			*v = storedTag(sink, dec.StackPointer()) != gateTag && JSONSwitchEnabled(value, false)
			if tagged, ok := sink.switchDefault(dec); ok {
				*v = tagged
			}
			sink.add(dec, IssueType)
			return nil
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
			if on, ok := sink.switchDefault(dec); ok && on {
				off = true
			}
			*v = &off
			return nil
		}),
		// An optional number or text stored with a wrong type is absent, so
		// readers apply their default (as R5 did); the issue is reported.
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v **Int) error {
			switch dec.PeekKind() {
			case 'n':
				return errors.ErrUnsupported
			case '0', '"':
				value, err := dec.ReadValue()
				if err != nil {
					return err
				}
				var n Int
				if n.UnmarshalJSON(value) != nil || !literalInt(sink, dec, value) {
					*v = nil
					sink.add(dec, IssueType)
					return nil
				}
				*v = &n
				return nil
			}
			return skipInvalid(dec, sink, func() { *v = nil })
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v **string) error {
			switch dec.PeekKind() {
			case 'n':
				return nullText(dec, sink, v)
			case '"', '0':
				return errors.ErrUnsupported
			}
			return skipInvalid(dec, sink, func() {
				*v = nil
				if field, ok := leafField(sink.root, dec.StackPointer()); ok && field.Tag.Get("null") == "empty" {
					empty := ""
					*v = &empty
				}
			})
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
			// A record stored as null is its zero value, reported: Python
			// refused null where a record belongs (O18 T1c; damaged settings).
			if kind == 'n' && typ.Kind() == reflect.Struct && isPlain(typ) && orderedElem(typ) == nil {
				if err := dec.SkipValue(); err != nil {
					return err
				}
				reflect.ValueOf(v).Elem().SetZero()
				sink.add(dec, IssueType)
				return nil
			}
			if kind == 'n' {
				return errors.ErrUnsupported
			}
			if typ.Kind() == reflect.Slice && kind == '[' && selfDecoding(typ.Elem()) {
				return decodeRecordList(dec, sink, reflect.ValueOf(v).Elem())
			}
			if typ.Kind() == reflect.Slice && kind == '[' && typ.Elem().Kind() == reflect.Struct && isPlain(typ.Elem()) && orderedElem(typ.Elem()) == nil {
				return decodePlainList(dec, sink, reflect.ValueOf(v).Elem())
			}
			// A manifest stored with another kind, or with members its record
			// does not declare, keeps its value (damagedKeeper).
			if typ.Kind() == reflect.Pointer {
				if keeper, ok := reflect.New(typ.Elem()).Interface().(damagedKeeper); ok {
					return decodeManifest(dec, sink, reflect.ValueOf(v).Elem(), keeper)
				}
			}
			// A record behind a pointer stored with another kind is nil, or an
			// empty record when its presence must fail closed (failClosed).
			if typ.Kind() == reflect.Pointer && typ.Elem().Kind() == reflect.Struct && isPlain(typ.Elem()) && kind != '{' {
				if typ.Implements(failClosedType) {
					return skipInvalid(dec, sink, func() { reflect.ValueOf(v).Elem().Set(reflect.New(typ.Elem())) })
				}
				return skipInvalid(dec, sink, func() { reflect.ValueOf(v).Elem().SetZero() })
			}
			// A device selection list stored as another JSON kind stays
			// damaged instead of reading as an empty selection (selectionTag).
			if typ == stringListType && kind != '[' && storedTag(sink, dec.StackPointer()) == selectionTag {
				return skipInvalid(dec, sink, func() { reflect.ValueOf(v).Elem().Set(reflect.ValueOf([]string{DamagedText})) })
			}
			switch {
			case orderedElem(typ) != nil && kind != '{',
				typ.Kind() == reflect.Slice && typ.Elem().Kind() != reflect.Uint8 && kind != '[',
				typ.Kind() == reflect.Map && kind != '{',
				typ.Kind() == reflect.Struct && isPlain(typ) && kind != '{':
				return skipInvalid(dec, sink, func() { reflect.ValueOf(v).Elem().SetZero() })
			}
			return errors.ErrUnsupported
		}),
		// Records not yet migrated decode themselves (R5 inside); a value they
		// refuse (not an object, for example) is zero plus an issue.
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v json.Unmarshaler) error {
			if wired, ok := v.(plainWirer); ok {
				return decodeWire(dec, v, wired)
			}
			if _, plain := v.(plainRecord); plain {
				return errors.ErrUnsupported
			}
			if _, from := v.(jsonv2.UnmarshalerFrom); from {
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

// gateTag marks a stored switch that makes the panel write to a device or
// push automatically (rule sync, HA activation, DNS follow retry, ...). Such a
// switch stored with another JSON type than a boolean reads as off (plus an
// issue) instead of by its truth value: damaged data must not start device
// writes the user never turned on (O18 T1b review ruling).
const gateTag = "gate"

// secretTag marks a stored credential (an Agent token, a registration
// credential digest): a value of another JSON type than a string, numbers
// included, reads as empty plus an issue, so it never authenticates and the
// panel sends no request with it (O18 T1b review).
const secretTag = "secret"

// restrictTag marks stored text that restricts something when present and
// allows anything when empty (a registration session's host). A value of
// another JSON type than a string (null aside) reads as DamagedText, which
// matches nothing and is refused (O18 T1b review); it is written back as
// that text, so the damage stays.
const restrictTag = "restrict"

// selectionTag marks a stored list of IDs that selects what the panel
// writes to a device (a router's rule-set selections). Stored as another JSON
// kind than a list (null aside: no selection) it reads as [DamagedText]
// instead of an empty selection, which would withdraw everything the
// selection put on the device at the next publication; consumers refuse the
// damaged ID (HR O18 T1c). Written back as that list, the damage stays.
const selectionTag = "selection"

var stringListType = reflect.TypeFor[[]string]()

// DamagedText is the value of a stored:"restrict" member whose stored JSON
// type was not a string. Consumers refuse it explicitly; it is not a valid
// host or name.
const DamagedText = "\x00damaged"

// storedTag returns the stored tag of the struct field at pointer (from the
// decoder's root type; the root's own tag when pointer is empty).
func storedTag(sink *issueSink, pointer jsontext.Pointer) string {
	typ, tag := sink.root, sink.tag
	for token := range pointer.Tokens() {
		for typ != nil && typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		tag = ""
		switch {
		case typ == nil:
			return ""
		case orderedElem(typ) != nil:
			typ = orderedElem(typ)
		case typ.Kind() == reflect.Struct:
			field, ok := plainFields(typ)[token]
			if !ok {
				return ""
			}
			tag = typ.FieldByIndex(field.index).Tag.Get("stored")
			typ = field.typ
		case typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array || typ.Kind() == reflect.Map:
			typ = typ.Elem()
		default:
			return ""
		}
	}
	return tag
}

// failClosed marks a record whose stored presence locks something (the
// OxiDNS HA publication journal locks its pair). Stored with another JSON
// kind than an object it reads as an empty record, not as absent, so damaged
// data keeps the lock (O18 T1b review).
type failClosed interface{ failClosed() }

var failClosedType = reflect.TypeFor[failClosed]()

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
	return decodeStored(data, v, prefix, "")
}

// decodeStored is DecodeStored; tag is the stored tag of v's own field.
func decodeStored(data []byte, v any, prefix string, tag string) ([]FieldIssue, error) {
	target := reflect.ValueOf(v)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return nil, fmt.Errorf("%w: stored target must be a non-nil pointer", ErrType)
	}
	sink := &issueSink{root: target.Type().Elem(), prefix: prefix, tag: tag}
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
		if elem := orderedElem(typ); elem != nil {
			return typeHasPlain(elem, seen)
		}
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
	case kind == '{' && typ.Kind() == reflect.Struct && orderedElem(typ) == nil:
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
			if !known && inlineIndex(typ) >= 0 {
				if err := dec.SkipValue(); err != nil {
					return err
				}
				continue
			}
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
	case kind == '{' && (typ.Kind() == reflect.Map || orderedElem(typ) != nil):
		elem := orderedElem(typ)
		if elem == nil {
			elem = typ.Elem()
		}
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		for dec.PeekKind() != '}' {
			if _, err := dec.ReadToken(); err != nil {
				return err
			}
			if err := walkUnknown(dec, elem, path+"{*}", issues); err != nil {
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
// Nested plain records (Agent and its journals) decode themselves with the
// stored-data rules: input carries them whole, as Python-written exports do.
var strictOptions = jsontext.AllowInvalidUTF8(true)

// plainWirer is a plain record nested in another: plainWire returns the
// record as its method-free wire type, so its members decode with the
// options of the enclosing decoder (lenient issues for storage, strict for
// input) instead of the record's own lenient UnmarshalJSON.
type plainWirer interface{ plainWire() any }

// decodeWire replaces v (a pointer to a plain record) by decoding the next
// value into its wire view; null is the zero record.
func decodeWire(dec *jsontext.Decoder, v any, wired plainWirer) error {
	reflect.ValueOf(v).Elem().SetZero()
	return jsonv2.UnmarshalDecode(dec, wired.plainWire())
}

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
	tag := value.Type().Elem().FieldByIndex(field.index).Tag.Get("stored")
	issues, err = decodeStored(data, target.Interface(), path, tag)
	if err != nil {
		return true, nil, err
	}
	value.Elem().FieldByIndex(field.index).Set(target.Elem())
	return true, issues, nil
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

// memberFields lists the JSON members of a plain struct in declaration order.
type memberField struct {
	name     string
	index    int
	omitzero bool
}

var memberFieldCache sync.Map

func memberFields(typ reflect.Type) []memberField {
	if cached, ok := memberFieldCache.Load(typ); ok {
		return cached.([]memberField)
	}
	var fields []memberField
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" || inlineOption(options) {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields = append(fields, memberField{name: name, index: i, omitzero: strings.Contains(","+options+",", ",omitzero,")})
	}
	actual, _ := memberFieldCache.LoadOrStore(typ, fields)
	return actual.([]memberField)
}

// EncodeMembers writes each member of a plain record (a pointer) as its own
// JSON value, in declaration order. omitzero members holding their zero value
// are left out. Values use this package's encoder (encodeUnit), so records not
// yet migrated are written by their own codecs without a second pass.
func EncodeMembers(rec any) ([]StoredMember, error) {
	value := reflect.ValueOf(rec)
	if value.Kind() != reflect.Pointer || value.IsNil() || !isPlain(value.Type().Elem()) {
		return nil, fmt.Errorf("%w: stored members need a plain record", ErrType)
	}
	value = value.Elem()
	fields := memberFields(value.Type())
	members := make([]StoredMember, 0, len(fields))
	for _, field := range fields {
		current := value.Field(field.index)
		if field.omitzero && omitted(current) {
			continue
		}
		data, err := encodeUnit(current.Interface())
		if err != nil {
			return nil, err
		}
		members = append(members, StoredMember{Name: field.name, Value: data})
	}
	return appendInlineMembers(value, members)
}

// EncodeMember is EncodeMembers for one member; present is false for an
// omitted (omitzero) or undeclared member.
func EncodeMember(rec any, name string) (data json.RawMessage, present bool, err error) {
	value := plainPointer(reflect.ValueOf(rec))
	if value.Kind() != reflect.Pointer || value.IsNil() || !isPlain(value.Type().Elem()) {
		return nil, false, fmt.Errorf("%w: stored members need a plain record", ErrType)
	}
	value = value.Elem()
	for _, field := range memberFields(value.Type()) {
		if field.name != name {
			continue
		}
		current := value.Field(field.index)
		if field.omitzero && omitted(current) {
			return nil, false, nil
		}
		data, err := encodeUnit(current.Interface())
		return data, err == nil, err
	}
	inline, err := appendInlineMembers(value, nil)
	if err != nil {
		return nil, false, err
	}
	for _, member := range inline {
		if member.Name == name {
			return member.Value, true, nil
		}
	}
	return nil, false, nil
}

// marshalPlain is MarshalJSON of a plain record: its members joined into one
// object in declaration order.
func marshalPlain(rec any) ([]byte, error) {
	members, err := EncodeMembers(rec)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, member := range members {
		if i > 0 {
			out.WriteByte(',')
		}
		key, err := scalarJSON(member.Name)
		if err != nil {
			return nil, err
		}
		out.Write(key)
		out.WriteByte(':')
		out.Write(member.Value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
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
	return jsonv2.Unmarshal(data, target, strictOptions, jsonv2.WithUnmarshalers(strictUnmarshalers))
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
	path := pointerPath(sink.root, sink.prefix, dec.StackPointer())
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
		if issuer, ok := element.Interface().(storedIssuer); ok {
			sink.issues = append(sink.issues, issuer.storedIssues(item, fmt.Sprintf("%s[%d]", path, len(items)))...)
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

// omitted reports a zero value for omitzero: its IsZero method if it has one
// (OrderedMap), otherwise the Go zero value.
func omitted(value reflect.Value) bool {
	if orderedElem(value.Type()) != nil {
		return value.Field(0).Len() == 0
	}
	return value.IsZero()
}

// decodePlain decodes stored or captured data into a plain record's wire type
// (the record without its methods) leniently; issues are not reported. The
// record must be a JSON object.
func decodePlain[W any](data []byte, out *W) error {
	if err := requireObject(data); err != nil {
		return err
	}
	var decoded W
	if _, err := DecodeStored(data, &decoded, ""); err != nil {
		return err
	}
	*out = decoded
	return nil
}

// DecodeInputMember strictly decodes input data into a declared member of a
// plain record (rec is a pointer; record names it in errors). A value of
// another JSON type is a *MemberTypeError and leaves the record unchanged.
func DecodeInputMember(record string, rec any, member string, data []byte) error {
	return decodeMember(record, rec, member, data)
}

// plainPointer follows a pointer to a pointer of a plain record.
func plainPointer(value reflect.Value) reflect.Value {
	for value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.Pointer {
		value = value.Elem()
	}
	return value
}

// SetStoredMember decodes data into a declared member of a plain record (rec
// is a pointer, or a pointer to one) with the stored-data rules: a value of
// the wrong type becomes the zero value.
func SetStoredMember(rec any, member string, data []byte) error {
	target := plainPointer(reflect.ValueOf(rec))
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		return fmt.Errorf("%w: stored member target must be a plain record", ErrType)
	}
	known, _, err := DecodeStoredMember(target.Interface(), member, data, member)
	if err == nil && !known {
		err = fmt.Errorf("%w: set of an undeclared member %q", ErrType, member)
	}
	return err
}

// ZeroMember sets a declared member of a plain record (rec is a pointer, or a
// pointer to one) to its zero value.
func ZeroMember(rec any, member string) error {
	target := plainPointer(reflect.ValueOf(rec))
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() || !isPlain(target.Type().Elem()) {
		return fmt.Errorf("%w: zero member target must be a plain record", ErrType)
	}
	return clearMember(target.Interface(), member)
}

// literalInt reports whether value may be read as the member's integer: a
// member tagged int:"literal" (backup retention, which Python accepted only
// as an int and otherwise replaced with its default) takes only an integer
// literal, not text or decimal/exponent notation.
func literalInt(sink *issueSink, dec *jsontext.Decoder, value jsontext.Value) bool {
	field, ok := leafField(sink.root, dec.StackPointer())
	if !ok || field.Tag.Get("int") != "literal" {
		return true
	}
	return value.Kind() == '0' && !bytes.ContainsAny(value, ".eE")
}

// storedIssuer is a self-decoding record that reports the load issues of a
// stored value it read leniently (wrong-typed and undeclared members), with
// paths under prefix.
type storedIssuer interface {
	storedIssues(raw []byte, prefix string) []FieldIssue
}
