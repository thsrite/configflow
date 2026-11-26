// Package setup owns the resumable first-use wizard. Device installation,
// gateway publication and router I/O remain with their existing owners.
package setup

import (
	"encoding/json"
	"fmt"

	"configflow/internal/model"
)

// Record is a stored wizard session. Unknown members survive every update;
// optional fields retain presence rather than synthesizing completed steps.
type Record struct {
	ID                     string          `json:"id"`
	ProfileID              string          `json:"profile_id"`
	Name                   string          `json:"name"`
	Goal                   string          `json:"goal"`
	Step                   model.Int       `json:"step"`
	MaxStep                model.Int       `json:"max_step"`
	CreatedAt              string          `json:"created_at"`
	UpdatedAt              string          `json:"updated_at"`
	SubscriptionIDs        []string        `json:"subscription_ids"`
	NodeIDs                []string        `json:"node_ids"`
	Prepared               *bool           `json:"prepared,omitzero"`
	ProfileRevision        *model.Int      `json:"profile_revision,omitzero"`
	Completed              *bool           `json:"completed,omitzero"`
	Abandoned              *bool           `json:"abandoned,omitzero"`
	AbandonedAt            *string         `json:"abandoned_at,omitzero"`
	DigestVersion          *model.Int      `json:"digest_version,omitzero"`
	OwnedDigest            *string         `json:"owned_digest,omitzero"`
	OwnedDigestWithoutBase *string         `json:"owned_digest_without_base,omitzero"`
	DNSFallback            *string         `json:"dns_fallback,omitzero"`
	TrafficFallback        *string         `json:"traffic_fallback,omitzero"`
	LocalDNS               *string         `json:"local_dns,omitzero"`
	DomainRules            *Selections     `json:"domain_rules,omitzero"`
	QueryRecorder          *bool           `json:"query_recorder,omitzero"`
	NodeCount              *model.Int      `json:"node_count,omitzero"`
	ConfigRevision         *model.Int      `json:"config_revision,omitzero"`
	MihomoChanges          *[]string       `json:"mihomo_changes,omitzero"`
	ControllerSecretSaved  *bool           `json:"controller_secret_saved,omitzero"`
	ControllerAddress      *string         `json:"controller_address,omitzero"`
	LegacyTakeover         *bool           `json:"legacy_takeover,omitzero"`
	RouterMode             *string         `json:"router_mode,omitzero"`
	RouterID               *string         `json:"router_id,omitzero"`
	RouterDraft            json.RawMessage `json:"router_draft,omitzero"`
	Installation           json.RawMessage `json:"installation,omitzero"`
	RouteInstructions      json.RawMessage `json:"route_instructions,omitzero"`
	RouterVerification     *string         `json:"router_verification,omitzero"`
	PreviousDefault        *string         `json:"previous_default,omitzero"`
}

type recordWire Record

// UnmarshalJSON decodes a stored session and rejects one with a member of the
// wrong type (it is corrupt ownership evidence, never repaired). Members the
// session does not declare are dropped.
func (r *Record) UnmarshalJSON(data []byte) error {
	if err := requireObject(data); err != nil {
		return err
	}
	var next recordWire
	issues, err := model.DecodeStored(data, &next, "")
	if err != nil {
		return err
	}
	for _, issue := range issues {
		if issue.Kind != model.IssueUnknown {
			return fmt.Errorf("%w: invalid wizard members", ErrCorruptRecord)
		}
	}
	*r = Record(next)
	return nil
}

// MarshalJSON writes the session in declaration order; the always-written
// members (Python creates every session with them) include empty lists as
// [], optional members only when set.
func (r Record) MarshalJSON() ([]byte, error) { return model.Marshal(recordWire(r)) }

// Selections contains the two independently ordered domain-rule choices.
type Selections struct {
	Direct []string `json:"direct"`
	Proxy  []string `json:"proxy"`
}

type selectionsWire Selections

// UnmarshalJSON rejects selections with a member of the wrong type.
func (s *Selections) UnmarshalJSON(data []byte) error {
	if err := requireObject(data); err != nil {
		return err
	}
	var next selectionsWire
	issues, err := model.DecodeStored(data, &next, "")
	if err != nil {
		return err
	}
	for _, issue := range issues {
		if issue.Kind != model.IssueUnknown {
			return ErrCorruptRecord
		}
	}
	*s = Selections(next)
	return nil
}

// MarshalJSON writes both lists, empty ones as [].
func (s Selections) MarshalJSON() ([]byte, error) { return model.Marshal(selectionsWire(s)) }

// requireObject refuses stored data that is not a JSON object.
func requireObject(data []byte) error {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return err
	}
	if members == nil {
		return ErrCorruptRecord
	}
	return nil
}

func pointer[T any](value T) *T { return &value }
func enabled(value *bool) bool  { return value != nil && *value }
func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// clear removes members explicitly, including stored nulls. A nil pointer alone
// is not a clear intent in the shared stored-record codec.
func (r *Record) clear(names ...string) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, name := range names {
		delete(fields, name)
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, r)
}
