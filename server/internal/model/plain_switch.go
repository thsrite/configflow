package model

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// Switch members whose JSON type is wrong (or null) in storage normally keep
// their Python truth value (T1b). A member tagged stored:"gate" makes a
// publication write to a device or push automatically (coordinator ruling,
// O18 T1c): a wrong type reads as off. A member tagged switch:"on" is a safety
// default Python read as `is not False` (verify_tls): a wrong type or null
// reads as on. Both still report the issue.

// switchDefault returns the value a tagged switch at the decoder's current
// position takes when it is stored with a wrong type or null.
func (s *issueSink) switchDefault(dec *jsontext.Decoder) (value, tagged bool) {
	field, ok := leafField(s.root, dec.StackPointer())
	if !ok {
		return false, false
	}
	switch {
	case field.Tag.Get("stored") == gateTag:
		return false, true
	case field.Tag.Get("switch") == "on":
		return true, true
	}
	return false, false
}

// damagedKeeper is a record that keeps a stored value of another JSON type
// instead of reading as absent (router ownership manifests).
type damagedKeeper interface{ keepDamaged(raw []byte) }

// innerKeeper is a damagedKeeper that also keeps its stored value when a
// member inside it reads with an issue and the decoded record says so
// (router guard settings: an enabled guard with a malformed member). raw is
// the stored object.
type innerKeeper interface {
	keepOnIssues(issues []FieldIssue, raw []byte) bool
}

func hasTypeIssue(issues []FieldIssue) bool {
	for _, issue := range issues {
		if issue.Kind == IssueType {
			return true
		}
	}
	return false
}

func isInnerKeeper(v any) bool {
	_, ok := v.(innerKeeper)
	return ok
}

// decodeManifest decodes a stored manifest into target (a nil pointer to a
// damagedKeeper record). An object of declared members of the right types
// decodes normally; any other value is kept whole as damaged, with issues. The object is decoded
// with a sink rooted at the record's own type, so member tags (switch rules)
// apply and issue paths keep the enclosing path. For guard settings
// (innerKeeper) undeclared members are dropped like in any plain record, and
// the record decides whether issues inside it keep the stored value.
func decodeManifest(dec *jsontext.Decoder, sink *issueSink, target reflect.Value, keeper damagedKeeper) error {
	path := pointerPath(sink.root, sink.prefix, dec.StackPointer())
	raw, err := dec.ReadValue()
	if err != nil {
		return err
	}
	inner, isInner := keeper.(innerKeeper)
	kind := IssueType
	if raw.Kind() == '{' {
		var unknown []FieldIssue
		if !isInner {
			if unknown, err = unknownMembers(raw, target.Type().Elem(), ""); err != nil {
				return err
			}
		}
		if len(unknown) == 0 {
			wired := keeper.(plainWirer)
			child := &issueSink{root: target.Type().Elem(), prefix: path}
			if err := jsonv2.Unmarshal(raw, wired.plainWire(), jsonv2.WithUnmarshalers(storedUnmarshalers(child)), jsontext.AllowInvalidUTF8(true)); err != nil {
				return err
			}
			sink.issues = append(sink.issues, child.issues...)
			switch {
			case isInner:
				if len(child.issues) > 0 && inner.keepOnIssues(child.issues, raw) {
					keeper.keepDamaged(raw)
				}
			case hasTypeIssue(child.issues):
				// A manifest member of the wrong type (resources "x") is lost
				// ownership evidence: the stored manifest is kept whole and
				// guards judge it as Python would (fail closed).
				keeper.keepDamaged(raw)
			}
			target.Set(reflect.ValueOf(keeper))
			return nil
		}
		kind = IssueUnknown
	}
	keeper.keepDamaged(raw)
	target.Set(reflect.ValueOf(keeper))
	sink.add(dec, kind)
	return nil
}

// strictText reports a text member tagged stored:"secret" or text:"strict"
// a number stored there is a wrong type, not text, so it fails closed.
// An element of a text list tagged stored:"secret" (retired credentials) is
// strict too.
func (s *issueSink) strictText(dec *jsontext.Decoder) bool {
	pointer := dec.StackPointer()
	if field, ok := leafField(s.root, pointer); ok {
		return strictTextTag(field.Tag)
	}
	parent, last := pointer.Parent(), pointer.LastToken()
	if _, err := strconv.Atoi(last); err != nil || parent == pointer {
		return false
	}
	field, ok := leafField(s.root, parent)
	return ok && field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.String && strictTextTag(field.Tag)
}

// leafField is the struct field a JSON pointer ends at, following plain
// structs, lists and ordered maps from typ.
func leafField(typ reflect.Type, pointer jsontext.Pointer) (reflect.StructField, bool) {
	var last reflect.StructField
	found := false
	for token := range pointer.Tokens() {
		for typ != nil && typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		found = false
		switch {
		case typ == nil:
			return last, false
		case orderedElem(typ) != nil:
			typ = orderedElem(typ)
		case typ.Kind() == reflect.Struct:
			field, ok := plainFields(typ)[token]
			if !ok {
				return last, false
			}
			last, found = typ.FieldByIndex(field.index), true
			typ = field.typ
		case typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array || typ.Kind() == reflect.Map:
			typ = typ.Elem()
		default:
			return last, false
		}
	}
	return last, found
}

// strictInputer marks the plain records of O18 T1c: as input they decode
// member by member with the strict rules (a wrong JSON type is an error)
// instead of their lenient UnmarshalJSON.
type strictInputer interface{ strictInput() }

// closedInputer is a record whose input must have exactly the members
// Python required: checkInput refuses anything else before it is decoded.
type closedInputer interface {
	checkInput(value jsontext.Value) error
}

// checkInput requires {"entry": ref, "exit": ref} with refs of exactly
// {"type", "id"}: Python refused a chain or a reference with other keys.
func (*ProxyChain) checkInput(value jsontext.Value) error {
	var chain map[string]jsontext.Value
	if jsonv2.Unmarshal(value, &chain) != nil || len(chain) != 2 {
		return fmt.Errorf("%w: proxy chain members", ErrType)
	}
	for _, name := range []string{"entry", "exit"} {
		var ref map[string]jsontext.Value
		if jsonv2.Unmarshal(chain[name], &ref) != nil || len(ref) != 2 || ref["type"] == nil || ref["id"] == nil {
			return fmt.Errorf("%w: proxy chain reference members", ErrType)
		}
	}
	return nil
}

// checkInput refuses members a provider health check does not declare
// (Python: 健康检查包含未知字段).
func (*ProviderHealthCheck) checkInput(value jsontext.Value) error {
	var members map[string]jsontext.Value
	if jsonv2.Unmarshal(value, &members) != nil {
		return fmt.Errorf("%w: health check must be an object", ErrType)
	}
	fields := plainFields(reflect.TypeFor[ProviderHealthCheck]())
	for name := range members {
		if _, known := fields[name]; !known {
			return fmt.Errorf("%w: undeclared health check member", ErrType)
		}
	}
	return nil
}

// strictUnmarshalers decode the plain records nested in strict input through
// their wire view (decodeStrictInput).
var strictUnmarshalers = jsonv2.UnmarshalFromFunc(decodeStrictInput)

// strictUnmarshalersFor are the input rules for the members of nested
// plain records (root is the decoded type, for member tags): every member is
// strict (coordinator ruling Q2-A), an Int must be an integer literal (one
// tagged int:"text", read by Python with int(), follows Int's own rules), a
// switch given as null is off (on for switch:"on"), as Python's falsy None,
// and a member tagged input:"strict" (a health-check setting) refuses null.
func strictUnmarshalersFor(root reflect.Type) *jsonv2.Unmarshalers {
	sink := &issueSink{root: root}
	return jsonv2.JoinUnmarshalers(
		jsonv2.UnmarshalFromFunc(decodeStrictInput),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v **string) error {
			if dec.PeekKind() == 'n' && !sink.strictLeaf(dec) {
				return nullText(dec, sink, v)
			}
			return strictNull(dec, sink)
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v **Int) error {
			return strictNull(dec, sink)
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v **[]string) error {
			return strictNull(dec, sink)
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *[]string) error {
			return strictNull(dec, sink)
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v **[]ResourceRef) error {
			return strictNull(dec, sink)
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v **bool) error {
			if dec.PeekKind() != 'n' {
				return errors.ErrUnsupported
			}
			if _, err := dec.ReadToken(); err != nil {
				return err
			}
			if sink.strictLeaf(dec) {
				return fmt.Errorf("%w: boolean expected", ErrType)
			}
			on, _ := sink.switchDefault(dec)
			*v = &on
			return nil
		}),
		jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *Int) error {
			if field, ok := leafField(sink.root, dec.StackPointer()); ok && field.Tag.Get("int") == "text" {
				// Python read the member with int(): Int's own rules.
				value, err := dec.ReadValue()
				if err != nil {
					return err
				}
				return v.UnmarshalJSON(value)
			}
			if dec.PeekKind() != '0' {
				return fmt.Errorf("%w: integer expected", ErrType)
			}
			value, err := dec.ReadValue()
			if err != nil {
				return err
			}
			if bytes.ContainsAny(value, ".eE") {
				return fmt.Errorf("%w: integer literal expected", ErrType)
			}
			return v.UnmarshalJSON(value)
		}),
	)
}

// strictNull refuses null for a member tagged input:"strict" and otherwise
// leaves it to the default pointer rule (nil).
func strictNull(dec *jsontext.Decoder, sink *issueSink) error {
	if dec.PeekKind() != 'n' {
		return errors.ErrUnsupported
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	if sink.strictLeaf(dec) {
		return fmt.Errorf("%w: null for a validated member", ErrType)
	}
	return nil
}

// strictLeaf reports a member tagged input:"strict": Python refused null
// there (health-check settings, a proxy group's id lists).
func (s *issueSink) strictLeaf(dec *jsontext.Decoder) bool {
	field, ok := leafField(s.root, dec.StackPointer())
	return ok && field.Tag.Get("input") == "strict"
}

func decodeStrictInput(dec *jsontext.Decoder, v strictInputer) error {
	wired, ok := v.(plainWirer)
	if !ok {
		return errors.ErrUnsupported
	}
	reflect.ValueOf(v).Elem().SetZero()
	// The record is decoded on its own so member paths (tags) are relative
	// to it.
	leaves := jsonv2.WithUnmarshalers(strictUnmarshalersFor(reflect.TypeOf(v).Elem()))
	value, err := dec.ReadValue()
	if err != nil {
		return err
	}
	// An ownership manifest is a proof: input that its record cannot hold
	// whole (another JSON type, undeclared members) is kept as given.
	// Manifests keep input they cannot hold; guard settings (innerKeeper)
	// are user input and decode strictly.
	if keeper, ok := v.(damagedKeeper); ok && !isInnerKeeper(v) && value.Kind() != 'n' {
		unknown := value.Kind() != '{'
		if !unknown {
			issues, err := unknownMembers(value, reflect.TypeOf(v).Elem(), "")
			if err != nil {
				return err
			}
			unknown = len(issues) > 0
		}
		if unknown {
			keeper.keepDamaged(value)
			return nil
		}
	}
	if closed, ok := v.(closedInputer); ok && value.Kind() != 'n' {
		if err := closed.checkInput(value); err != nil {
			return err
		}
	}
	return jsonv2.Unmarshal(value, wired.plainWire(), strictOptions, leaves)
}

// Declares reports whether a plain record (a pointer) declares the member.
func Declares(rec any, member string) bool {
	value := plainPointer(reflect.ValueOf(rec))
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() || !isPlain(value.Type().Elem()) {
		return false
	}
	_, known := plainFields(value.Type().Elem())[member]
	return known
}

// Node protocol parameters (O18 T1c, coordinator ruling): a flat node, one
// without proxy_string or params, holds its proxy protocol settings as
// top-level members whose set is open (Python used the node dict as the
// proxy). They are kept in a field tagged json:",embed" (an encoding/json/v2
// embedded fallback, in stored order); no other record has such a field.

func inlineOption(options string) bool { return strings.Contains(","+options+",", ",embed,") }

var inlineIndexCache sync.Map

// inlineIndex is the index of typ's json:",embed" fallback field, or -1.
func inlineIndex(typ reflect.Type) int {
	if cached, ok := inlineIndexCache.Load(typ); ok {
		return cached.(int)
	}
	index := -1
	if typ.Kind() == reflect.Struct {
		for i := 0; i < typ.NumField(); i++ {
			_, options, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
			if typ.Field(i).IsExported() && inlineOption(options) {
				index = i
			}
		}
	}
	inlineIndexCache.Store(typ, index)
	return index
}

// appendInlineMembers adds the members of value's inline field after its
// declared members.
func appendInlineMembers(value reflect.Value, members []StoredMember) ([]StoredMember, error) {
	index := inlineIndex(value.Type())
	if index < 0 {
		return members, nil
	}
	raw, _ := value.Field(index).Interface().(jsontext.Value)
	if len(raw) == 0 {
		return members, nil
	}
	dec := jsontext.NewDecoder(bytes.NewReader(raw), jsontext.AllowInvalidUTF8(true))
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("%w: inline members: %v", ErrJSON, err)
	}
	for dec.PeekKind() != '}' {
		token, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("%w: inline members: %v", ErrJSON, err)
		}
		name := token.String()
		item, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("%w: inline members: %v", ErrJSON, err)
		}
		members = append(members, StoredMember{Name: name, Value: bytes.Clone(item)})
	}
	return members, nil
}

// decodePlainList decodes a stored list of plain records. An element that is
// not an object is dropped (null silently, anything else with an issue) and
// the other elements are kept, at any depth: zeroing the whole list would let
// the next write lose every record of it (all routers of routers.items, the
// whole discovery history); most Python readers skip such elements too.
//
// A member tagged elements:"all" (a group's proxies_order, which Python's
// readers ignore as a whole when an element is not an object) reads as
// absent instead.
//
// A member tagged elements:"keep" (an Agent's DNS checks, whose report marks
// itself incomplete for an invalid check as Python did) reads such an element
// as a zero element instead, so the reader still sees it was there.
func decodePlainList(dec *jsontext.Decoder, sink *issueSink, target reflect.Value) error {
	field, ok := leafField(sink.root, dec.StackPointer())
	keep := ok && field.Tag.Get("elements") == "keep"
	all := ok && field.Tag.Get("elements") == "all"
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	list := reflect.MakeSlice(target.Type(), 0, 0)
	invalid := false
	for dec.PeekKind() != ']' {
		kind := dec.PeekKind()
		if kind != '{' {
			if err := dec.SkipValue(); err != nil {
				return err
			}
			if kind != 'n' {
				sink.add(dec, IssueType)
			}
			if keep {
				list = reflect.Append(list, reflect.Zero(target.Type().Elem()))
			}
			if all && kind != 'n' {
				invalid = true
			}
			continue
		}
		element := reflect.New(target.Type().Elem())
		if err := jsonv2.UnmarshalDecode(dec, element.Interface()); err != nil {
			return err
		}
		list = reflect.Append(list, element.Elem())
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	if invalid {
		target.SetZero()
		return nil
	}
	target.Set(list)
	return nil
}

// nullText reads a null text member. A member tagged null:"empty" (a rule's
// policy) keeps an explicit null, or a stored value of another type, as an
// empty reference: Python did not apply the default policy to such a value,
// so generation still refuses it instead of falling back to PROXY. Other
// members read null as absent.
func nullText(dec *jsontext.Decoder, sink *issueSink, v **string) error {
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	*v = nil
	if field, ok := leafField(sink.root, dec.StackPointer()); ok && field.Tag.Get("null") == "empty" {
		empty := ""
		*v = &empty
	}
	return nil
}

// strictTextTag reports text that must be stored as text: a credential
// (stored:"secret") or text whose number spelling must not become a value
// (text:"strict": the panel's domain, the backup URL, a rule's policy).
func strictTextTag(tag reflect.StructTag) bool {
	return tag.Get("stored") == secretTag || tag.Get("text") == "strict"
}
