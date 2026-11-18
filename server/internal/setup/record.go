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
	Prepared               *bool           `json:"prepared"`
	ProfileRevision        *model.Int      `json:"profile_revision"`
	Completed              *bool           `json:"completed"`
	Abandoned              *bool           `json:"abandoned"`
	AbandonedAt            *string         `json:"abandoned_at"`
	OwnedDigest            *string         `json:"owned_digest"`
	OwnedDigestWithoutBase *string         `json:"owned_digest_without_base"`
	DNSFallback            *string         `json:"dns_fallback"`
	TrafficFallback        *string         `json:"traffic_fallback"`
	LocalDNS               *string         `json:"local_dns"`
	DomainRules            *Selections     `json:"domain_rules"`
	QueryRecorder          *bool           `json:"query_recorder"`
	NodeCount              *model.Int      `json:"node_count"`
	ConfigRevision         *model.Int      `json:"config_revision"`
	MihomoChanges          *[]string       `json:"mihomo_changes"`
	ControllerSecretSaved  *bool           `json:"controller_secret_saved"`
	ControllerAddress      *string         `json:"controller_address"`
	LegacyTakeover         *bool           `json:"legacy_takeover"`
	RouterMode             *string         `json:"router_mode"`
	RouterID               *string         `json:"router_id"`
	RouterDraft            json.RawMessage `json:"router_draft"`
	Installation           json.RawMessage `json:"installation"`
	RouteInstructions      json.RawMessage `json:"route_instructions"`
	RouterVerification     *string         `json:"router_verification"`
	PreviousDefault        *string         `json:"previous_default"`
	Fields                 model.Fields
	raw                    model.Raw
}

// UnmarshalJSON preserves stored fields and rejects invalid ownership evidence.
func (r *Record) UnmarshalJSON(data []byte) error {
	var next Record
	if err := model.DecodeRecord(data, &next, &next.raw); err != nil {
		return err
	}
	if len(next.raw.Issues()) != 0 {
		return fmt.Errorf("%w: invalid wizard members", ErrCorruptRecord)
	}
	*r = next
	return nil
}

// MarshalJSON preserves unknown and unchanged wizard members.
func (r Record) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.raw) }

// Selections contains the two independently ordered domain-rule choices.
type Selections struct {
	Direct []string `json:"direct"`
	Proxy  []string `json:"proxy"`
	Fields model.Fields
	raw    model.Raw
}

// UnmarshalJSON preserves legacy selection members and rejects wrong types.
func (s *Selections) UnmarshalJSON(data []byte) error {
	var next Selections
	if err := model.DecodeRecord(data, &next, &next.raw); err != nil {
		return err
	}
	if len(next.raw.Issues()) != 0 {
		return ErrCorruptRecord
	}
	*s = next
	return nil
}

// MarshalJSON preserves unknown selection members.
func (s Selections) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&s, &s.raw) }

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
