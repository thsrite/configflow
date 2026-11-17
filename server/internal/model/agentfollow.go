package model

// DNSFollowBinding captures the selected source and target identities without normalizing its signatures.
type DNSFollowBinding struct {
	SourceAgentID     string  `json:"source_agent_id"`
	UpstreamTag       string  `json:"upstream_tag"`
	UpstreamIndex     Int     `json:"upstream_index"`
	Connection        string  `json:"connection"`
	Address           *string `json:"address"`
	ID                string  `json:"id"`
	SourceProfileID   string  `json:"source_profile_id"`
	TargetProfileID   string  `json:"target_profile_id"`
	UpstreamSignature string  `json:"upstream_signature"`
	AfterDeploymentID *string `json:"after_deployment_id"`
	Fields            Fields
	raw               Raw
}

// UnmarshalJSON decodes DNSFollowBinding with stored-member preservation and R5 fallback.
func (r *DNSFollowBinding) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged DNSFollowBinding members.
func (r DNSFollowBinding) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DNSFollowState holds the durable follow outcome and retry ownership.
type DNSFollowState struct {
	Status             string           `json:"status"`
	Message            string           `json:"message"`
	UpdatedAt          string           `json:"updated_at"`
	Events             []OperationEvent `json:"events"`
	SourceDeploymentID *string          `json:"source_deployment_id"`
	TargetDeploymentID *string          `json:"target_deployment_id"`
	Attempt            *Int             `json:"attempt"`
	RetryRequested     *bool            `json:"retry_requested"`
	Address            *string          `json:"address"`
	DraftSynced        *bool            `json:"draft_synced"`
	Fields             Fields
	raw                Raw
}

// UnmarshalJSON decodes DNSFollowState with stored-member preservation and R5 fallback.
func (r *DNSFollowState) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged DNSFollowState members.
func (r DNSFollowState) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DNSFollowJob retains an uncommitted OxiDNS candidate under its original binding and settings digest.
type DNSFollowJob struct {
	BindingID              string  `json:"binding_id"`
	ProfileID              string  `json:"profile_id"`
	OriginalSettingsDigest string  `json:"original_settings_digest"`
	CandidateSettings      Dynamic `json:"candidate_settings"`
	CandidateSignature     string  `json:"candidate_signature"`
	Fields                 Fields
	raw                    Raw
}

// UnmarshalJSON decodes DNSFollowJob with stored-member preservation and R5 fallback.
func (r *DNSFollowJob) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged DNSFollowJob members.
func (r DNSFollowJob) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }
