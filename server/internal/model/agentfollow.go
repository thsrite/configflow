package model

import ()

// DNSFollowBinding captures the selected source and target identities without normalizing its signatures.
type DNSFollowBinding struct {
	SourceAgentID     string  `json:"source_agent_id,omitzero"`
	UpstreamTag       string  `json:"upstream_tag,omitzero"`
	UpstreamIndex     Int     `json:"upstream_index"`
	Connection        string  `json:"connection,omitzero"`
	Address           *string `json:"address,omitzero"`
	ID                string  `json:"id,omitzero"`
	SourceProfileID   string  `json:"source_profile_id,omitzero"`
	TargetProfileID   string  `json:"target_profile_id,omitzero"`
	UpstreamSignature string  `json:"upstream_signature,omitzero"`
	AfterDeploymentID *string `json:"after_deployment_id,omitzero"`
}

// DNSFollowState holds the durable follow outcome and retry ownership.
type DNSFollowState struct {
	Status             string           `json:"status,omitzero"`
	Message            string           `json:"message,omitzero"`
	UpdatedAt          string           `json:"updated_at,omitzero"`
	Events             []OperationEvent `json:"events,omitzero"`
	SourceDeploymentID *string          `json:"source_deployment_id,omitzero"`
	TargetDeploymentID *string          `json:"target_deployment_id,omitzero"`
	Attempt            *Int             `json:"attempt,omitzero"`
	RetryRequested     *bool            `json:"retry_requested,omitzero" stored:"gate"`
	Address            *string          `json:"address,omitzero"`
	DraftSynced        *bool            `json:"draft_synced,omitzero"`
}

// DNSFollowJob retains an uncommitted OxiDNS candidate under its original binding and settings digest.
type DNSFollowJob struct {
	BindingID              string  `json:"binding_id,omitzero"`
	ProfileID              string  `json:"profile_id,omitzero"`
	OriginalSettingsDigest string  `json:"original_settings_digest,omitzero"`
	CandidateSettings      Dynamic `json:"candidate_settings,omitzero"`
	CandidateSignature     string  `json:"candidate_signature,omitzero"`
}

// Plain-record codecs (O18 T1b); see plainCodec.

type dNSFollowBindingWire DNSFollowBinding

func (*DNSFollowBinding) plainRecord() {}

// UnmarshalJSON decodes stored or captured DNSFollowBinding data leniently (DecodeStored).
func (r *DNSFollowBinding) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*dNSFollowBindingWire)(r))
}

func (r *DNSFollowBinding) plainWire() any { return (*dNSFollowBindingWire)(r) }

// MarshalJSON writes the DNSFollowBinding members in declaration order.
func (r DNSFollowBinding) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type dNSFollowStateWire DNSFollowState

func (*DNSFollowState) plainRecord() {}

// UnmarshalJSON decodes stored or captured DNSFollowState data leniently (DecodeStored).
func (r *DNSFollowState) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*dNSFollowStateWire)(r))
}

func (r *DNSFollowState) plainWire() any { return (*dNSFollowStateWire)(r) }

// failClosed: a stored state of another JSON kind is an empty state, which
// Busy counts as running (O18 T1b review).
func (*DNSFollowState) failClosed() {}

// Busy reports a follow run that locks its source and target Agents:
// preparing or publishing. A state without status is damaged data (every
// state the panel writes has one) and counts as busy too, so it keeps the
// lock (fail closed, like the HA publication journal); a missing state is
// not busy.
func (r *DNSFollowState) Busy() bool {
	return r != nil && (r.Status == "preparing" || r.Status == "publishing" || r.Status == "")
}

// MarshalJSON writes the DNSFollowState members in declaration order.
func (r DNSFollowState) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type dNSFollowJobWire DNSFollowJob

func (*DNSFollowJob) plainRecord() {}

// UnmarshalJSON decodes stored or captured DNSFollowJob data leniently (DecodeStored).
func (r *DNSFollowJob) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*dNSFollowJobWire)(r))
}

func (r *DNSFollowJob) plainWire() any { return (*dNSFollowJobWire)(r) }

// MarshalJSON writes the DNSFollowJob members in declaration order.
func (r DNSFollowJob) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }
