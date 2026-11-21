package model

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"reflect"
)

// The stored root, its three sections and the profiles are plain records
// (O18 T1a): ordinary structs written by encoding/json/v2. Members Python
// always wrote when it created a section are always written, so a stored
// zero value ("_revision": 0, "description": "") stays present; members that
// may be absent are omitzero. Unknown members are not kept: storage reports
// them once on load (DecodeStored), input decoding drops them.

// Profile holds typed profile settings and router publication records.
type Profile struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
	Revision        Int             `json:"_revision"`
	ProxyGroups     []ProxyGroup    `json:"proxy_groups"`
	RuleConfigs     []RuleConfig    `json:"rule_configs"`
	Mihomo          MihomoSettings  `json:"mihomo"`
	Surge           SurgeSettings   `json:"surge"`
	Loon            LoonSettings    `json:"loon,omitzero"`
	MosDNS          MosDNSSettings  `json:"mosdns"`
	OxiDNS          Dynamic         `json:"oxidns,omitzero"`
	Routers         Routers         `json:"routers,omitzero"`
	DomainDiscovery DomainDiscovery `json:"domain_discovery,omitzero"`
	DeviceNotes     DeviceNotes     `json:"device_notes,omitzero"`
	ClientFixedIPs  ClientFixedIPs  `json:"client_fixed_ips,omitzero"`
	// Rules and RuleSets are the rule lists of profiles written before
	// rule_configs existed, kept as stored JSON. Generators read them only
	// while RuleConfigs is empty; see LegacyRuleList.
	Rules    json.RawMessage `json:"rules,omitzero"`
	RuleSets json.RawMessage `json:"rule_sets,omitzero"`
	// owner is the stored section key and ownerID the ID decoded with it. The
	// key follows a changed ID, so a copied profile with a new ID becomes its
	// own section; an untouched (possibly malformed) ID keeps the stored key.
	owner    string
	ownerID  string
	hasOwner bool
}

type profileWire Profile

func (*Profile) plainRecord() {}

// UnmarshalJSON decodes stored or captured profile data leniently (see
// DecodeStored; issues are not reported). Input from HTTP and imports goes
// through SetJSON or DecodeDocument, which are strict.
func (p *Profile) UnmarshalJSON(data []byte) error {
	var decoded profileWire
	if err := requireObject(data); err != nil {
		return err
	}
	if _, err := DecodeStored(data, &decoded, ""); err != nil {
		return err
	}
	*p = Profile(decoded)
	return nil
}

// MarshalJSON writes the profile's members in declaration order.
func (p Profile) MarshalJSON() ([]byte, error) { return marshalPlain(&p) }

// Clear sets a declared member (by JSON name) to its zero value.
func (p *Profile) Clear(member string) error { return clearMember(p, member) }

// SetJSON strictly decodes data into a declared member (by JSON name). A value
// of another JSON type is a *MemberTypeError and leaves the profile unchanged.
func (p *Profile) SetJSON(member string, data json.RawMessage) error {
	return decodeMember("profile", p, member, data)
}

// LegacyRuleList returns the stored legacy list "rules" or "rule_sets".
func (p *Profile) LegacyRuleList(name string) (json.RawMessage, bool) {
	var raw json.RawMessage
	switch name {
	case "rules":
		raw = p.Rules
	case "rule_sets":
		raw = p.RuleSets
	}
	return bytes.Clone(raw), raw != nil
}

// SetStoredKey records the section key a profile was stored under; Key keeps
// returning it while ID is unchanged.
func (p *Profile) SetStoredKey(key string) {
	p.owner, p.ownerID, p.hasOwner = key, p.ID, true
}

// Key returns the profile's section key in the stored document: the key it
// was decoded under while ID is unchanged (even if the stored id member is
// malformed), otherwise ID.
func (p *Profile) Key() string {
	if p.hasOwner && p.ID == p.ownerID {
		return p.owner
	}
	return p.ID
}

// Shared holds the resources shared by all profiles.
type Shared struct {
	Subscriptions []Subscription    `json:"subscriptions"`
	Nodes         []Node            `json:"nodes"`
	Aggregations  []Aggregation     `json:"subscription_aggregations"`
	RuleLibrary   []RuleLibraryItem `json:"rule_library"`
	Revision      Int               `json:"_revision"`
}

type sharedWire Shared

func (*Shared) plainRecord() {}

// UnmarshalJSON decodes stored or captured shared data leniently (see
// DecodeStored; issues are not reported). Input from HTTP and imports goes
// through SetJSON or DecodeDocument, which are strict.
func (s *Shared) UnmarshalJSON(data []byte) error {
	var decoded sharedWire
	if err := requireObject(data); err != nil {
		return err
	}
	if _, err := DecodeStored(data, &decoded, ""); err != nil {
		return err
	}
	*s = Shared(decoded)
	return nil
}

// MarshalJSON writes the shared members in declaration order.
func (s Shared) MarshalJSON() ([]byte, error) { return marshalPlain(&s) }

// Clear sets a declared member (by JSON name) to its zero value.
func (s *Shared) Clear(member string) error { return clearMember(s, member) }

// SetJSON strictly decodes data into a declared member; see Profile.SetJSON.
func (s *Shared) SetJSON(member string, data json.RawMessage) error {
	return decodeMember("shared", s, member, data)
}

// System holds global settings, backup preferences and registered Agents.
type System struct {
	SystemConfig         SystemConfig               `json:"system_config"`
	Backup               Backup                     `json:"backup"`
	Agents               []Agent                    `json:"agents"`
	RegistrationSessions []AgentRegistrationSession `json:"agent_registration_sessions,omitzero"`
	Revision             Int                        `json:"_revision"`
}

type systemWire System

func (*System) plainRecord() {}

// UnmarshalJSON decodes stored or captured system data leniently (see
// DecodeStored; issues are not reported). Input from HTTP and imports goes
// through SetJSON or DecodeDocument, which are strict.
func (s *System) UnmarshalJSON(data []byte) error {
	var decoded systemWire
	if err := requireObject(data); err != nil {
		return err
	}
	if _, err := DecodeStored(data, &decoded, ""); err != nil {
		return err
	}
	*s = System(decoded)
	return nil
}

// MarshalJSON writes the system members in declaration order.
func (s System) MarshalJSON() ([]byte, error) { return marshalPlain(&s) }

// Clear sets a declared member (by JSON name) to its zero value.
func (s *System) Clear(member string) error { return clearMember(s, member) }

// SetJSON strictly decodes data into a declared member; see Profile.SetJSON.
func (s *System) SetJSON(member string, data json.RawMessage) error {
	return decodeMember("system", s, member, data)
}

// Document is the stored root record: the storage version, the system and
// shared sections and the profiles in their stored order. In JSON (exports,
// imports) profiles is an object keyed by Profile.Key.
type Document struct {
	SchemaVersion Int
	System        System
	Shared        Shared
	Profiles      []*Profile
}

// requireObject refuses a record that is not a JSON object; its members are
// decoded leniently, the record itself is not.
func requireObject(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("%w: record must be an object", ErrType)
	}
	return nil
}

// MissingSectionError reports document input without one of the sections
// system, shared or profiles.
type MissingSectionError struct{ Section string }

func (e *MissingSectionError) Error() string { return "document without section " + e.Section }

// DocumentSections are the members a stored document must have.
var DocumentSections = []string{"system", "shared", "profiles"}

// DecodeDocument strictly decodes a whole document (exports, imports, owned
// copies). A member of the wrong type is a *MemberTypeError naming its
// section member, a missing section a *MissingSectionError. Unknown members
// are dropped. Issues are those of records that still decode themselves.
func DecodeDocument(data []byte) (*Document, []FieldIssue, error) {
	if err := validJSON(data); err != nil {
		return nil, nil, err
	}
	dec := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowInvalidUTF8(true))
	if dec.PeekKind() != '{' {
		return nil, nil, fmt.Errorf("%w: document must be an object", ErrType)
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrJSON, err)
	}
	document := &Document{}
	seen := map[string]bool{}
	for dec.PeekKind() != '}' {
		token, err := dec.ReadToken()
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrJSON, err)
		}
		name := token.String()
		value, err := dec.ReadValue()
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrJSON, err)
		}
		seen[name] = true
		switch name {
		case "schema_version":
			if err := decodeStrict(value, &document.SchemaVersion); err != nil {
				return nil, nil, memberError("document", name, value, err)
			}
		case "system":
			if err := decodeSection("system", value, (*systemWire)(&document.System)); err != nil {
				return nil, nil, err
			}
		case "shared":
			if err := decodeSection("shared", value, (*sharedWire)(&document.Shared)); err != nil {
				return nil, nil, err
			}
		case "profiles":
			profiles, err := decodeProfiles(value)
			if err != nil {
				return nil, nil, err
			}
			document.Profiles = profiles
		}
	}
	for _, section := range DocumentSections {
		if !seen[section] {
			return nil, nil, &MissingSectionError{Section: section}
		}
	}
	issues := legacyIssues(reflect.ValueOf(&document.System), "system")
	issues = append(issues, legacyIssues(reflect.ValueOf(&document.Shared), "shared")...)
	for i, profile := range document.Profiles {
		issues = append(issues, legacyIssues(reflect.ValueOf(profile), fmt.Sprintf("profiles[%d]", i))...)
	}
	return document, issues, nil
}

func decodeProfiles(data []byte) ([]*Profile, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowInvalidUTF8(true))
	if dec.PeekKind() != '{' {
		return nil, &MemberTypeError{Record: "document", Member: "profiles", Value: bytes.Clone(data), Err: ErrType}
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJSON, err)
	}
	profiles := []*Profile{}
	for dec.PeekKind() != '}' {
		token, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrJSON, err)
		}
		key := token.String()
		value, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrJSON, err)
		}
		profile := &Profile{}
		if err := decodeSection("profile", value, (*profileWire)(profile)); err != nil {
			return nil, err
		}
		profile.SetStoredKey(key)
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

// DecodeInput strictly decodes input for a plain record (rec is a *Profile,
// *Shared or *System; record names it in errors): a non-object or a member of
// the wrong type is a *MemberTypeError, unknown members are dropped.
func DecodeInput(record string, data []byte, rec any) error {
	if err := validJSON(data); err != nil {
		return err
	}
	if !isPlainTarget(rec) {
		return fmt.Errorf("%w: input target must be a plain record", ErrType)
	}
	return decodeSection(record, data, rec)
}

// decodeSection strictly decodes one section object member by member, so a
// type mismatch names the member it belongs to.
func decodeSection(record string, data []byte, target any) error {
	dec := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowInvalidUTF8(true))
	if dec.PeekKind() != '{' {
		return &MemberTypeError{Record: "document", Member: record, Value: bytes.Clone(data), Err: ErrType}
	}
	if _, err := dec.ReadToken(); err != nil {
		return fmt.Errorf("%w: %v", ErrJSON, err)
	}
	fields := plainFieldsOf(target)
	for dec.PeekKind() != '}' {
		token, err := dec.ReadToken()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrJSON, err)
		}
		name := token.String()
		value, err := dec.ReadValue()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrJSON, err)
		}
		field, known := fields[name]
		if !known {
			continue
		}
		destination := plainFieldValue(target, field)
		decoded := newValue(field.typ)
		if err := decodeStrict(value, decoded.Interface()); err != nil {
			return memberError(record, name, value, err)
		}
		destination.Set(decoded.Elem())
	}
	return nil
}

func memberError(record, member string, value []byte, err error) error {
	var typed *MemberTypeError
	if errors.As(err, &typed) {
		return err
	}
	return &MemberTypeError{Record: record, Member: member, Value: bytes.Clone(value), Err: err}
}

// MarshalJSON writes schema_version, system, shared and profiles (an object
// keyed by Profile.Key, in slice order).
func (d Document) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteString(`{"schema_version":`)
	version, err := scalarJSON(d.SchemaVersion)
	if err != nil {
		return nil, err
	}
	out.Write(version)
	system, err := d.System.MarshalJSON()
	if err != nil {
		return nil, err
	}
	out.WriteString(`,"system":`)
	out.Write(system)
	shared, err := d.Shared.MarshalJSON()
	if err != nil {
		return nil, err
	}
	out.WriteString(`,"shared":`)
	out.Write(shared)
	out.WriteString(`,"profiles":{`)
	seen := map[string]bool{}
	for i, profile := range d.Profiles {
		if profile == nil {
			return nil, fmt.Errorf("%w: nil profile record", ErrType)
		}
		owner := profile.Key()
		if seen[owner] {
			return nil, fmt.Errorf("%w: duplicate profile owner", ErrJSON)
		}
		seen[owner] = true
		if i > 0 {
			out.WriteByte(',')
		}
		key, err := scalarJSON(owner)
		if err != nil {
			return nil, err
		}
		out.Write(key)
		out.WriteByte(':')
		data, err := profile.MarshalJSON()
		if err != nil {
			return nil, err
		}
		out.Write(data)
	}
	out.WriteString("}}")
	return out.Bytes(), nil
}

// UnmarshalJSON is DecodeDocument without the issues.
func (d *Document) UnmarshalJSON(data []byte) error {
	document, _, err := DecodeDocument(data)
	if err != nil {
		return err
	}
	*d = *document
	return nil
}
