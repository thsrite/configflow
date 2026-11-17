package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
)

// Profile is the N8-3 profile skeleton. Complex members remain opaque until
// their owning N8-4 type step; unknown members are retained in Fields.
type Profile struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
	Revision        Int             `json:"_revision"`
	ProxyGroups     json.RawMessage `json:"proxy_groups"`
	RuleConfigs     json.RawMessage `json:"rule_configs"`
	Mihomo          json.RawMessage `json:"mihomo"`
	Surge           json.RawMessage `json:"surge"`
	Loon            json.RawMessage `json:"loon"`
	MosDNS          json.RawMessage `json:"mosdns"`
	OxiDNS          json.RawMessage `json:"oxidns"`
	Routers         json.RawMessage `json:"routers"`
	DomainDiscovery json.RawMessage `json:"domain_discovery"`
	DeviceNotes     json.RawMessage `json:"device_notes"`
	ClientFixedIPs  json.RawMessage `json:"client_fixed_ips"`
	Fields          Fields
	raw             Raw
	owner           string
	hasOwner        bool
}

// UnmarshalJSON decodes a profile with R1–R9 preservation and type fallback.
func (p *Profile) UnmarshalJSON(data []byte) error { return DecodeRecord(data, p, &p.raw) }

// MarshalJSON preserves the profile's unknown and unchanged members.
func (p Profile) MarshalJSON() ([]byte, error) { return EncodeRecord(&p, &p.raw) }

// Shared is the shared-resource skeleton; resource lists are opaque in N8-3.
type Shared struct {
	Subscriptions json.RawMessage `json:"subscriptions"`
	Nodes         json.RawMessage `json:"nodes"`
	Aggregations  json.RawMessage `json:"subscription_aggregations"`
	RuleLibrary   json.RawMessage `json:"rule_library"`
	Revision      Int             `json:"_revision"`
	Fields        Fields
	raw           Raw
}

// UnmarshalJSON decodes the shared record with type fallback.
func (s *Shared) UnmarshalJSON(data []byte) error { return DecodeRecord(data, s, &s.raw) }

// MarshalJSON preserves unknown and unchanged shared members.
func (s Shared) MarshalJSON() ([]byte, error) { return EncodeRecord(&s, &s.raw) }

// System is the global-settings skeleton; settings/backup/agents remain opaque.
type System struct {
	SystemConfig json.RawMessage `json:"system_config"`
	Backup       json.RawMessage `json:"backup"`
	Agents       json.RawMessage `json:"agents"`
	Revision     Int             `json:"_revision"`
	Fields       Fields
	raw          Raw
}

// UnmarshalJSON decodes the system record with type fallback.
func (s *System) UnmarshalJSON(data []byte) error { return DecodeRecord(data, s, &s.raw) }

// MarshalJSON preserves unknown and unchanged system members.
func (s System) MarshalJSON() ([]byte, error) { return EncodeRecord(&s, &s.raw) }

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
// keys identify loaded sections even if their stored id member is malformed.
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
			owner := profile.ID
			if profile.hasOwner {
				owner = profile.owner
			}
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
