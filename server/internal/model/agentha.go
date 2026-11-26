package model

import (
	"encoding/json"
)

// HASettings holds an explicit DNS HA member configuration.
type HASettings struct {
	Enabled *bool  `json:"enabled,omitzero" stored:"gate"`
	GroupID string `json:"group_id,omitzero"`
	PeerID  string `json:"peer_id,omitzero"`
	VIP     string `json:"vip,omitzero"`
	VRID    Int    `json:"vrid,omitzero"`
	Role    string `json:"role,omitzero"`
}

// HAState holds the latest HA setup outcome; UpdatedAt is an integral epoch.
type HAState struct {
	Status    string   `json:"status,omitzero"`
	Error     *string  `json:"error,omitzero"`
	UpdatedAt Int      `json:"updated_at,omitzero"`
	Members   []string `json:"members,omitzero"`
}

// HAMember is one publication member outcome.
type HAMember struct {
	AgentID      string `json:"agent_id,omitzero"`
	Name         string `json:"name,omitzero"`
	DeploymentID string `json:"deployment_id,omitzero"`
	Status       string `json:"status,omitzero"`
}

// HAIdentity pins a child target identity for restart-safe ownership checks.
type HAIdentity struct {
	Host        string `json:"host,omitzero"`
	Port        Int    `json:"port,omitzero"`
	ProfileID   string `json:"profile_id,omitzero"`
	ServiceType string `json:"service_type,omitzero"`
	Enabled     *bool  `json:"enabled,omitzero"`
}

// HAEnable is the keepalived enable request retained by a publication.
type HAEnable struct {
	PeerID string `json:"peer_id,omitzero"`
	VIP    string `json:"vip,omitzero"`
	VRID   Int    `json:"vrid,omitzero"`
}

// HARouter identifies a router affected by a publication.
type HARouter struct {
	ID        string `json:"id,omitzero"`
	Name      string `json:"name,omitzero"`
	ProfileID string `json:"profile_id,omitzero"`
}

// HAHandoff retains the release handshake rather than inferring it from the active member.
type HAHandoff struct {
	OperationID   string   `json:"operation_id,omitzero"`
	SourceAgentID string   `json:"source_agent_id,omitzero"`
	TargetAgentID string   `json:"target_agent_id,omitzero"`
	RequestedAt   float64  `json:"requested_at,omitzero"`
	Confirmed     *bool    `json:"confirmed,omitzero"`
	Released      *bool    `json:"released,omitzero"`
	Confirmations Int      `json:"confirmations"`
	ReleasedAt    *float64 `json:"released_at,omitzero"`
}

// HAHandoffFailure holds the persisted failure to finish a handoff.
type HAHandoffFailure struct {
	Message string `json:"message,omitzero"`
	Status  string `json:"status,omitzero"`
}

// HAJournal owns a multi-member publication; optional booleans retain presence-sensitive recovery decisions.
type HAJournal struct {
	ID                string                 `json:"id,omitzero"`
	OwnerAgentID      string                 `json:"owner_agent_id,omitzero"`
	OwnerDeploymentID string                 `json:"owner_deployment_id,omitzero"`
	Children          OrderedMap[string]     `json:"children,omitzero"`
	Identities        OrderedMap[HAIdentity] `json:"identities,omitzero"`
	Status            string                 `json:"status,omitzero"`
	Phase             string                 `json:"phase,omitzero"`
	Message           string                 `json:"message,omitzero"`
	ActiveAgentID     *string                `json:"active_agent_id"`
	Activate          *bool                  `json:"activate,omitzero" stored:"gate"`
	Order             []string               `json:"order,omitzero"`
	Enable            *HAEnable              `json:"enable,omitzero"`
	UpdatedAt         Int                    `json:"updated_at,omitzero"`
	HandoffProtocol   *Int                   `json:"handoff_protocol,omitzero"`
	Warnings          []string               `json:"warnings,omitzero"`
	Routers           []HARouter             `json:"routers,omitzero"`
	RuleSyncSettings  *RuleSyncSettings      `json:"rule_sync_settings,omitzero"`
	Kind              *string                `json:"kind,omitzero"`
	Members           []HAMember             `json:"members,omitzero"`
	Handoff           *HAHandoff             `json:"handoff,omitzero"`
	HandoffRequired   *bool                  `json:"handoff_required,omitzero"`
	HandoffFailure    *HAHandoffFailure      `json:"handoff_failure,omitzero"`
	// UnreachableSince is when an unfinished pair first lost a member's
	// connection (Unix seconds); progress clears it (lab O-1).
	UnreachableSince *float64 `json:"unreachable_since,omitzero"`
	// AutoComplete is the public automatic-completion state: on a pair
	// stopped by a lost member (waiting/running/blocked) or on the
	// publication that completes it (attempt).
	AutoComplete *HAAutoComplete `json:"auto_complete,omitzero"`
	// AutoCompleteWatch is the private recovery evidence for AutoComplete.
	AutoCompleteWatch *HAAutoCompleteWatch `json:"auto_complete_watch,omitzero"`
}

// HAAutoComplete is the automatic completion state shown with a pair publication.
type HAAutoComplete struct {
	// Status is waiting, running or blocked on a stopped pair; empty on the
	// completing publication, which sets Attempt.
	Status  string `json:"status,omitzero"`
	Message string `json:"message,omitzero"`
	Attempt bool   `json:"attempt,omitzero"`
}

// HAAutoCompleteWatch records which members were lost, when the publication
// stopped and the heartbeats observed since, so a restart keeps the decision.
type HAAutoCompleteWatch struct {
	Lost       []string           `json:"lost,omitzero"`
	StoppedAt  float64            `json:"stopped_at,omitzero"`
	Heartbeats OrderedMap[string] `json:"heartbeats,omitzero"`
	Healthy    OrderedMap[Int]    `json:"healthy,omitzero"`
}

// HAPublicationSummary is the task projection of a journal, with no child or source graph.
type HAPublicationSummary struct {
	ID                string     `json:"id,omitzero"`
	OwnerAgentID      string     `json:"owner_agent_id,omitzero"`
	OwnerDeploymentID string     `json:"owner_deployment_id,omitzero"`
	Status            string     `json:"status,omitzero"`
	Phase             string     `json:"phase,omitzero"`
	Message           string     `json:"message,omitzero"`
	ActiveAgentID     *string    `json:"active_agent_id"`
	UpdatedAt         Int        `json:"updated_at,omitzero"`
	Warnings          []string   `json:"warnings,omitzero"`
	Routers           []HARouter `json:"routers,omitzero"`
	Members           []HAMember `json:"members,omitzero"`
	// AutoComplete is the journal's automatic-completion state, when any.
	AutoComplete *HAAutoComplete `json:"auto_complete,omitzero"`
}

// HABundle is the external payload metadata for an archive kept by the existing file store.
type HABundle struct {
	Manifest         DeploymentManifest `json:"manifest,omitzero"`
	SHA256           string             `json:"sha256,omitzero"`
	ConfigVersion    string             `json:"config_version,omitzero"`
	ComparisonConfig *DeploymentFile    `json:"comparison_config,omitzero"`
}

// HAPayload is the fixed external envelope; captured source configurations remain opaque.
type HAPayload struct {
	ID             string          `json:"id,omitzero"`
	BaseURL        string          `json:"base_url,omitzero"`
	ConfigRevision *Int            `json:"config_revision,omitzero"`
	SourceSnapshot json.RawMessage `json:"source_snapshot,omitzero"`
	ResourceConfig json.RawMessage `json:"resource_config,omitzero"`
	Bundle         *HABundle       `json:"bundle,omitzero"`
}

// Plain-record codecs (O18 T1b); see plainCodec.

type hASettingsWire HASettings

func (*HASettings) plainRecord() {}

// UnmarshalJSON decodes stored or captured HASettings data leniently (DecodeStored).
func (r *HASettings) UnmarshalJSON(data []byte) error { return decodePlain(data, (*hASettingsWire)(r)) }

func (r *HASettings) plainWire() any { return (*hASettingsWire)(r) }

// MarshalJSON writes the HASettings members in declaration order.
func (r HASettings) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type hAStateWire HAState

func (*HAState) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAState data leniently (DecodeStored).
func (r *HAState) UnmarshalJSON(data []byte) error { return decodePlain(data, (*hAStateWire)(r)) }

func (r *HAState) plainWire() any { return (*hAStateWire)(r) }

// MarshalJSON writes the HAState members in declaration order.
func (r HAState) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type hAMemberWire HAMember

func (*HAMember) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAMember data leniently (DecodeStored).
func (r *HAMember) UnmarshalJSON(data []byte) error { return decodePlain(data, (*hAMemberWire)(r)) }

func (r *HAMember) plainWire() any { return (*hAMemberWire)(r) }

// MarshalJSON writes the HAMember members in declaration order.
func (r HAMember) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type hAIdentityWire HAIdentity

func (*HAIdentity) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAIdentity data leniently (DecodeStored).
func (r *HAIdentity) UnmarshalJSON(data []byte) error { return decodePlain(data, (*hAIdentityWire)(r)) }

func (r *HAIdentity) plainWire() any { return (*hAIdentityWire)(r) }

// MarshalJSON writes the HAIdentity members in declaration order.
func (r HAIdentity) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type hAEnableWire HAEnable

func (*HAEnable) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAEnable data leniently (DecodeStored).
func (r *HAEnable) UnmarshalJSON(data []byte) error { return decodePlain(data, (*hAEnableWire)(r)) }

func (r *HAEnable) plainWire() any { return (*hAEnableWire)(r) }

// MarshalJSON writes the HAEnable members in declaration order.
func (r HAEnable) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// HasMembers reports whether HAEnable holds any value.
func (r *HAEnable) HasMembers() bool { return r != nil && hasMembers(r) }

type hARouterWire HARouter

func (*HARouter) plainRecord() {}

// UnmarshalJSON decodes stored or captured HARouter data leniently (DecodeStored).
func (r *HARouter) UnmarshalJSON(data []byte) error { return decodePlain(data, (*hARouterWire)(r)) }

func (r *HARouter) plainWire() any { return (*hARouterWire)(r) }

// MarshalJSON writes the HARouter members in declaration order.
func (r HARouter) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type hAHandoffWire HAHandoff

func (*HAHandoff) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAHandoff data leniently (DecodeStored).
func (r *HAHandoff) UnmarshalJSON(data []byte) error { return decodePlain(data, (*hAHandoffWire)(r)) }

func (r *HAHandoff) plainWire() any { return (*hAHandoffWire)(r) }

// MarshalJSON writes the HAHandoff members in declaration order.
func (r HAHandoff) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type hAHandoffFailureWire HAHandoffFailure

func (*HAHandoffFailure) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAHandoffFailure data leniently (DecodeStored).
func (r *HAHandoffFailure) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*hAHandoffFailureWire)(r))
}

func (r *HAHandoffFailure) plainWire() any { return (*hAHandoffFailureWire)(r) }

// MarshalJSON writes the HAHandoffFailure members in declaration order.
func (r HAHandoffFailure) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type hAJournalWire HAJournal

func (*HAJournal) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAJournal data leniently (DecodeStored).
func (r *HAJournal) UnmarshalJSON(data []byte) error { return decodePlain(data, (*hAJournalWire)(r)) }

func (r *HAJournal) plainWire() any { return (*hAJournalWire)(r) }

// failClosed: a stored journal of another JSON kind still locks the pair.
func (*HAJournal) failClosed() {}

// MarshalJSON writes the HAJournal members in declaration order.
func (r HAJournal) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// HasMembers reports whether HAJournal holds any value.
func (r *HAJournal) HasMembers() bool { return r != nil && hasMembers(r) }

type hAAutoCompleteWire HAAutoComplete

func (*HAAutoComplete) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAAutoComplete data leniently (DecodeStored).
func (r *HAAutoComplete) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*hAAutoCompleteWire)(r))
}

func (r *HAAutoComplete) plainWire() any { return (*hAAutoCompleteWire)(r) }

// MarshalJSON writes the HAAutoComplete members in declaration order.
func (r HAAutoComplete) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type hAAutoCompleteWatchWire HAAutoCompleteWatch

func (*HAAutoCompleteWatch) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAAutoCompleteWatch data leniently (DecodeStored).
func (r *HAAutoCompleteWatch) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*hAAutoCompleteWatchWire)(r))
}

func (r *HAAutoCompleteWatch) plainWire() any { return (*hAAutoCompleteWatchWire)(r) }

// MarshalJSON writes the HAAutoCompleteWatch members in declaration order.
func (r HAAutoCompleteWatch) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type hAPublicationSummaryWire HAPublicationSummary

func (*HAPublicationSummary) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAPublicationSummary data leniently (DecodeStored).
func (r *HAPublicationSummary) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*hAPublicationSummaryWire)(r))
}

func (r *HAPublicationSummary) plainWire() any { return (*hAPublicationSummaryWire)(r) }

// MarshalJSON writes the HAPublicationSummary members in declaration order.
func (r HAPublicationSummary) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// HasMembers reports whether HAPublicationSummary holds any value.
func (r *HAPublicationSummary) HasMembers() bool { return r != nil && hasMembers(r) }

type hABundleWire HABundle

func (*HABundle) plainRecord() {}

// UnmarshalJSON decodes stored or captured HABundle data leniently (DecodeStored).
func (r *HABundle) UnmarshalJSON(data []byte) error { return decodePlain(data, (*hABundleWire)(r)) }

func (r *HABundle) plainWire() any { return (*hABundleWire)(r) }

// MarshalJSON writes the HABundle members in declaration order.
func (r HABundle) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type hAPayloadWire HAPayload

func (*HAPayload) plainRecord() {}

// UnmarshalJSON decodes stored or captured HAPayload data leniently (DecodeStored).
func (r *HAPayload) UnmarshalJSON(data []byte) error { return decodePlain(data, (*hAPayloadWire)(r)) }

func (r *HAPayload) plainWire() any { return (*hAPayloadWire)(r) }

// MarshalJSON writes the HAPayload members in declaration order.
func (r HAPayload) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }
