package model

import "encoding/json"

// OperationEvent is one stored operation event; ordering and retention belong to consumers.
type OperationEvent struct {
	At      string  `json:"at"`
	Status  string  `json:"status"`
	Message *string `json:"message"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes OperationEvent with stored-member preservation and R5 fallback.
func (r *OperationEvent) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when OperationEvent is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *OperationEvent) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged OperationEvent members.
func (r OperationEvent) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// Deployment is the panel deployment journal, distinct from the remote Agent on-disk journal.
type Deployment struct {
	Success                   bool                     `json:"success"`
	DeploymentID              string                   `json:"deployment_id"`
	Status                    string                   `json:"status"`
	ProfileID                 string                   `json:"profile_id"`
	ServiceType               string                   `json:"service_type"`
	CreatedAt                 string                   `json:"created_at"`
	UpdatedAt                 *string                  `json:"updated_at"`
	AgentUpdatedAt            *string                  `json:"agent_updated_at"`
	Events                    []OperationEvent         `json:"events"`
	Message                   *string                  `json:"message"`
	Error                     *string                  `json:"error"`
	ErrorDetails              *string                  `json:"error_details"`
	FailedStage               *string                  `json:"failed_stage"`
	RollbackError             *string                  `json:"rollback_error"`
	RollbackErrorDetails      *string                  `json:"rollback_error_details"`
	ConfigVersion             *string                  `json:"config_version"`
	ConfigRevision            *Int                     `json:"config_revision"`
	SHA256                    *string                  `json:"sha256"`
	SourceSHA256              *string                  `json:"source_sha256"`
	Submitted                 *bool                    `json:"submitted"`
	PreviousRunning           *bool                    `json:"previous_running"`
	Health                    json.RawMessage          `json:"health"`
	HTTPStatus                *Int                     `json:"http_status"`
	ManualRollback            *bool                    `json:"manual_rollback"`
	RollbackRejected          *bool                    `json:"rollback_rejected"`
	RollbackMessage           *string                  `json:"rollback_message"`
	DNSChecks                 []DeploymentDNSCheck     `json:"dns_checks"`
	DNSCheckedAt              *string                  `json:"dns_checked_at"`
	PreparedContent           *PreparedContent         `json:"prepared_content"`
	OxiDNSValidationReceipt   *OxiDNSValidationReceipt `json:"oxidns_validation_receipt"`
	HAPublication             *HAPublicationSummary    `json:"ha_publication"`
	IPv6VerificationRequested *bool                    `json:"ipv6_verification_requested"`
	Fields                    Fields
	raw                       Raw
}

// UnmarshalJSON decodes Deployment with stored-member preservation and R5 fallback.
func (r *Deployment) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when Deployment is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *Deployment) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged Deployment members.
func (r Deployment) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DeploymentDNSCheck is a persisted bounded DNS report item.
type DeploymentDNSCheck struct {
	Name    string  `json:"name"`
	Level   string  `json:"level"`
	Message string  `json:"message"`
	Fix     *string `json:"fix"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes DeploymentDNSCheck with stored-member preservation and R5 fallback.
func (r *DeploymentDNSCheck) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when DeploymentDNSCheck is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *DeploymentDNSCheck) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged DeploymentDNSCheck members.
func (r DeploymentDNSCheck) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// PreparedContent records immutable prepared content identities without recalculating them.
type PreparedContent struct {
	Fingerprint          string       `json:"fingerprint"`
	ProfileID            string       `json:"profile_id"`
	ServiceType          string       `json:"service_type"`
	ConfigPath           string       `json:"config_path"`
	BaseURL              string       `json:"base_url"`
	Files                *Map[string] `json:"files"`
	DeployedConfigSHA256 *string      `json:"deployed_config_sha256"`
	SettingsDigest       *string      `json:"settings_digest"`
	Fields               Fields
	raw                  Raw
}

// UnmarshalJSON decodes PreparedContent with stored-member preservation and R5 fallback.
func (r *PreparedContent) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when PreparedContent is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *PreparedContent) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged PreparedContent members.
func (r PreparedContent) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// AppliedContent is the confirmed current content pointer, not an inferred deployment outcome.
type AppliedContent struct {
	Fingerprint          string       `json:"fingerprint"`
	ProfileID            string       `json:"profile_id"`
	ServiceType          string       `json:"service_type"`
	ConfigPath           string       `json:"config_path"`
	BaseURL              string       `json:"base_url"`
	Files                *Map[string] `json:"files"`
	DeployedConfigSHA256 *string      `json:"deployed_config_sha256"`
	SettingsDigest       *string      `json:"settings_digest"`
	ConfigVersion        string       `json:"config_version"`
	DeploymentID         string       `json:"deployment_id"`
	Fields               Fields
	raw                  Raw
}

// UnmarshalJSON decodes AppliedContent with stored-member preservation and R5 fallback.
func (r *AppliedContent) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when AppliedContent is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *AppliedContent) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged AppliedContent members.
func (r AppliedContent) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// OxiDNSValidationReceipt holds server-issued activation evidence; decoding does not validate or mint a receipt.
type OxiDNSValidationReceipt struct {
	Version      Int    `json:"version"`
	RulesSHA256  string `json:"rules_sha256"`
	AgentID      string `json:"agent_id"`
	DeploymentID string `json:"deployment_id"`
	ProfileID    string `json:"profile_id"`
	ServiceType  string `json:"service_type"`
	SHA256       string `json:"sha256"`
	Fingerprint  string `json:"fingerprint"`
	Fields       Fields
	raw          Raw
}

// UnmarshalJSON decodes OxiDNSValidationReceipt with stored-member preservation and R5 fallback.
func (r *OxiDNSValidationReceipt) UnmarshalJSON(data []byte) error {
	return DecodeRecord(data, r, &r.raw)
}

// Clear drops a declared member (by JSON name) when OxiDNSValidationReceipt is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *OxiDNSValidationReceipt) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged OxiDNSValidationReceipt members.
func (r OxiDNSValidationReceipt) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// AgentUpgrade is the durable latest upgrade, owned by UpdateID.
type AgentUpgrade struct {
	UpdateID           string               `json:"update_id"`
	TargetVersion      string               `json:"target_version"`
	PreviousVersion    string               `json:"previous_version"`
	Status             string               `json:"status"`
	AgentID            string               `json:"agent_id"`
	StartedAt          float64              `json:"started_at"`
	UpdatedAt          float64              `json:"updated_at"`
	Protocol           Int                  `json:"protocol"`
	Artifacts          Map[UpgradeArtifact] `json:"artifacts"`
	Events             []OperationEvent     `json:"events"`
	Message            *string              `json:"message"`
	Error              *string              `json:"error"`
	AgentUpdatedAt     *string              `json:"agent_updated_at"`
	RecoveryReportedAt *float64             `json:"recovery_reported_at"`
	Fields             Fields
	raw                Raw
}

// UnmarshalJSON decodes AgentUpgrade with stored-member preservation and R5 fallback.
func (r *AgentUpgrade) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when AgentUpgrade is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *AgentUpgrade) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged AgentUpgrade members.
func (r AgentUpgrade) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HasMembers reports effective upgrade journal presence without encoding artifacts.
func (r *AgentUpgrade) HasMembers() bool { return r != nil && recordHasMembers(r, &r.raw) }

// UpgradeArtifact identifies an upgrade artifact without accessing its file.
type UpgradeArtifact struct {
	SHA256 string `json:"sha256"`
	Size   Int    `json:"size"`
	Fields Fields
	raw    Raw
}

// UnmarshalJSON decodes UpgradeArtifact with stored-member preservation and R5 fallback.
func (r *UpgradeArtifact) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when UpgradeArtifact is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *UpgradeArtifact) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged UpgradeArtifact members.
func (r UpgradeArtifact) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// IPv6VerificationSettings selects the client and URL used by readiness checks.
type IPv6VerificationSettings struct {
	ClientAgentID string `json:"client_agent_id"`
	TestURL       string `json:"test_url"`
	Fields        Fields
	raw           Raw
}

// UnmarshalJSON decodes IPv6VerificationSettings with stored-member preservation and R5 fallback.
func (r *IPv6VerificationSettings) UnmarshalJSON(data []byte) error {
	return DecodeRecord(data, r, &r.raw)
}

// Clear drops a declared member (by JSON name) when IPv6VerificationSettings is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *IPv6VerificationSettings) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged IPv6VerificationSettings members.
func (r IPv6VerificationSettings) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// IPv6Verification retains a readiness result and the identities against which it was checked.
type IPv6Verification struct {
	Status              string                  `json:"status"`
	Scope               string                  `json:"scope"`
	Checks              []IPv6VerificationCheck `json:"checks"`
	StartedAt           string                  `json:"started_at"`
	StartedEpoch        float64                 `json:"started_epoch"`
	RunID               string                  `json:"run_id"`
	ProfileID           string                  `json:"profile_id"`
	ConfigRevision      *Int                    `json:"config_revision"`
	DeploymentID        *string                 `json:"deployment_id"`
	SettingsFingerprint string                  `json:"settings_fingerprint"`
	TargetFingerprint   string                  `json:"target_fingerprint"`
	AppliedFingerprint  string                  `json:"applied_fingerprint"`
	ClientFingerprint   *string                 `json:"client_fingerprint"`
	Message             *string                 `json:"message"`
	CheckedAt           *string                 `json:"checked_at"`
	ErrorCode           *string                 `json:"error_code"`
	Fields              Fields
	raw                 Raw
}

// UnmarshalJSON decodes IPv6Verification with stored-member preservation and R5 fallback.
func (r *IPv6Verification) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when IPv6Verification is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *IPv6Verification) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged IPv6Verification members.
func (r IPv6Verification) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// IPv6VerificationCheck is one stored readiness check.
type IPv6VerificationCheck struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Details []string `json:"details"`
	Scope   *string  `json:"scope"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes IPv6VerificationCheck with stored-member preservation and R5 fallback.
func (r *IPv6VerificationCheck) UnmarshalJSON(data []byte) error {
	return DecodeRecord(data, r, &r.raw)
}

// Clear drops a declared member (by JSON name) when IPv6VerificationCheck is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *IPv6VerificationCheck) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged IPv6VerificationCheck members.
func (r IPv6VerificationCheck) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DeploymentFile describes one immutable archive entry; paths and digests are opaque to the codec.
type DeploymentFile struct {
	Path   string  `json:"path"`
	Size   Int     `json:"size"`
	SHA256 string  `json:"sha256"`
	Role   string  `json:"role"`
	Name   *string `json:"name"`
	Fields Fields
	raw    Raw
}

// UnmarshalJSON decodes DeploymentFile with stored-member preservation and R5 fallback.
func (r *DeploymentFile) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when DeploymentFile is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *DeploymentFile) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged DeploymentFile members.
func (r DeploymentFile) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DeploymentManifest is a stored manifest counterpart independent of runtime deploy DTOs.
type DeploymentManifest struct {
	ProtocolVersion Int              `json:"protocol_version"`
	DeploymentID    string           `json:"deployment_id"`
	AgentID         string           `json:"agent_id"`
	ProfileID       string           `json:"profile_id"`
	ServiceType     string           `json:"service_type"`
	ConfigPath      string           `json:"config_path"`
	Files           []DeploymentFile `json:"files"`
	ReloadOnly      *bool            `json:"reload_only"`
	FinalConfig     *bool            `json:"final_config"`
	Fields          Fields
	raw             Raw
}

// UnmarshalJSON decodes DeploymentManifest with stored-member preservation and R5 fallback.
func (r *DeploymentManifest) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when DeploymentManifest is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *DeploymentManifest) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged DeploymentManifest members.
func (r DeploymentManifest) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// AgentRuleSyncReceipt is the fixed external rule-sync envelope; Source is a captured configuration, not a live model graph.
type AgentRuleSyncReceipt struct {
	Source           json.RawMessage    `json:"source"`
	Manifest         DeploymentManifest `json:"manifest"`
	SHA256           string             `json:"sha256"`
	ConfigVersion    string             `json:"config_version"`
	BaseURL          string             `json:"base_url"`
	ComparisonConfig *DeploymentFile    `json:"comparison_config"`
	Fields           Fields
	raw              Raw
}

// UnmarshalJSON decodes AgentRuleSyncReceipt with stored-member preservation and R5 fallback.
func (r *AgentRuleSyncReceipt) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when AgentRuleSyncReceipt is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *AgentRuleSyncReceipt) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged AgentRuleSyncReceipt members.
func (r AgentRuleSyncReceipt) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }
