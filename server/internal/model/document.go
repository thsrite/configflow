package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
)

// Profile holds typed profile settings and router publication records.
// Unknown members are retained in Fields.
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
	Loon            LoonSettings    `json:"loon"`
	MosDNS          MosDNSSettings  `json:"mosdns"`
	OxiDNS          Dynamic         `json:"oxidns"`
	Routers         Routers         `json:"routers"`
	DomainDiscovery DomainDiscovery `json:"domain_discovery"`
	DeviceNotes     DeviceNotes     `json:"device_notes"`
	ClientFixedIPs  ClientFixedIPs  `json:"client_fixed_ips"`
	Fields          Fields
	raw             Raw
	// owner is the stored section key and ownerID the ID decoded with it. The
	// key follows a changed ID, so a copied profile with a new ID becomes its
	// own section; an untouched (possibly malformed) ID keeps the stored key.
	owner    string
	ownerID  string
	hasOwner bool
}

// UnmarshalJSON decodes a profile with R1–R9 preservation and type fallback.
func (p *Profile) UnmarshalJSON(data []byte) error { return DecodeRecord(data, p, &p.raw) }

// Clear drops a declared member (by JSON name) when Profile is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (p *Profile) Clear(member string) error { return ClearMember(p, &p.raw, member) }

// MarshalJSON preserves the profile's unknown and unchanged members.
func (p Profile) MarshalJSON() ([]byte, error) { return EncodeRecord(&p, &p.raw) }

// SetJSON stores a JSON value as a member of the profile; see SetMemberJSON.
func (p *Profile) SetJSON(member string, data json.RawMessage) error {
	return SetMemberJSON(p, &p.raw, member, data)
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
	Fields        Fields
	raw           Raw
}

// UnmarshalJSON decodes the shared record with type fallback.
func (s *Shared) UnmarshalJSON(data []byte) error { return DecodeRecord(data, s, &s.raw) }

// Clear drops a declared member (by JSON name) when Shared is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (s *Shared) Clear(member string) error { return ClearMember(s, &s.raw, member) }

// MarshalJSON preserves unknown and unchanged shared members.
func (s Shared) MarshalJSON() ([]byte, error) { return EncodeRecord(&s, &s.raw) }

// SetJSON stores a JSON value as a member of the shared; see SetMemberJSON.
func (s *Shared) SetJSON(member string, data json.RawMessage) error {
	return SetMemberJSON(s, &s.raw, member, data)
}

// System holds global settings, backup preferences and registered Agents.
type System struct {
	SystemConfig         SystemConfig               `json:"system_config"`
	Backup               Backup                     `json:"backup"`
	Agents               []Agent                    `json:"agents"`
	RegistrationSessions []AgentRegistrationSession `json:"agent_registration_sessions"`
	Revision             Int                        `json:"_revision"`
	Fields               Fields
	raw                  Raw
}

// UnmarshalJSON decodes the system record with type fallback.
func (s *System) UnmarshalJSON(data []byte) error { return DecodeRecord(data, s, &s.raw) }

// Clear drops a declared member (by JSON name) when System is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (s *System) Clear(member string) error { return ClearMember(s, &s.raw, member) }

// MarshalJSON preserves unknown and unchanged system members.
func (s System) MarshalJSON() ([]byte, error) { return EncodeRecord(&s, &s.raw) }

// SetJSON stores a JSON value as a member of the system; see SetMemberJSON.
func (s *System) SetJSON(member string, data json.RawMessage) error {
	return SetMemberJSON(s, &s.raw, member, data)
}

// Document is the stored root record. Profiles retains the stored section order;
// Root holds other root members, including schema_version. The root member order
// and original profile-owner keys are retained separately from editable values.
type Document struct {
	System   System
	Shared   Shared
	Profiles []*Profile
	Root     Fields
	order    []string
	raw      Raw
	profiles Map[*Profile]
}

type documentWire struct {
	System   System        `json:"system"`
	Shared   Shared        `json:"shared"`
	Profiles Map[*Profile] `json:"profiles"`
	Fields   Fields
	raw      Raw
}

// DecodeDocument decodes a stored document and returns value-free diagnostics.
// Invalid member types are tolerated; invalid JSON or a non-object root is not.
func DecodeDocument(data []byte) (*Document, []FieldIssue, error) {
	var wire documentWire
	if err := DecodeRecord(data, &wire, &wire.raw); err != nil {
		return nil, nil, err
	}
	issues := valueIssues(reflect.ValueOf(wire), "")
	document := &Document{System: wire.System, Shared: wire.Shared, Root: wire.Fields, raw: wire.raw, profiles: wire.Profiles}
	for _, member := range wire.raw.source.members {
		document.order = append(document.order, member.name)
	}
	malformed := false
	index := 0
	for name, profile := range wire.Profiles.All() {
		if profile == nil {
			malformed = true
			path := fmt.Sprintf("profiles[%d]", index)
			found := false
			for _, issue := range issues {
				if issue.Path == path {
					found = true
					break
				}
			}
			if !found {
				issues = append(issues, FieldIssue{path, IssueType})
			}
			index++
			continue
		}
		index++
		profile.owner = name
		profile.ownerID = profile.ID
		profile.hasOwner = true
		document.Profiles = append(document.Profiles, profile)
	}
	if malformed {
		// A profile section is a record, not a nullable map value. Preserve an
		// unusable profile collection as an invalid parent field until replaced.
		document.Profiles = nil
		document.profiles = Map[*Profile]{}

		for i := range document.raw.members {
			if document.raw.members[i].name == "profiles" {
				document.raw.members[i].invalid = true
				document.raw.members[i].baseline = []byte("null")
			}
		}
	}
	return document, issues, nil
}

// UnmarshalJSON decodes the document; callers that log diagnostics use
// DecodeDocument instead so no issue is lost at the I/O boundary.
func (d *Document) UnmarshalJSON(data []byte) error {
	document, _, err := DecodeDocument(data)
	if err != nil {
		return err
	}
	*d = *document
	return nil
}

// MarshalJSON preserves the root record and profile object layout. Source owner
// keys identify loaded sections even if their stored id member is malformed;
// a profile whose ID was changed (for example a copy) is keyed by its new ID.
func (d Document) MarshalJSON() ([]byte, error) {
	profiles := d.profiles
	if profiles.state != nil || d.Profiles != nil {
		state := profiles.copyState()
		state.null = false
		state.entries = nil
		if d.Profiles == nil && profiles.state != nil && profiles.state.null {
			state.null = true
		}
		original := map[string]mapEntry[*Profile]{}
		if profiles.state != nil {
			for _, entry := range profiles.state.entries {
				original[entry.name] = entry
			}
		}
		seen := map[string]bool{}
		for _, profile := range d.Profiles {
			if profile == nil {
				return nil, fmt.Errorf("%w: nil profile record", ErrType)
			}
			owner := profile.Key()
			if seen[owner] {
				return nil, fmt.Errorf("%w: duplicate profile owner", ErrJSON)
			}
			seen[owner] = true
			entry, known := original[owner]
			if !known {
				entry = mapEntry[*Profile]{name: owner}
			}
			entry.value = profile
			state.entries = append(state.entries, entry)
		}
		profiles.state = state
	}
	wire := documentWire{System: d.System, Shared: d.Shared, Profiles: profiles, Fields: d.Root, raw: d.raw}
	return EncodeRecord(&wire, &wire.raw)
}

// Order returns the original root member order, independently owned by caller.
// Marshal additionally appends newly present members according to R3.
func (d Document) Order() []string { return slices.Clone(d.order) }
