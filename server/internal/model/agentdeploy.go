package model

import (
	"encoding/json"
)

// OperationEvent is one stored operation event; ordering and retention belong to consumers.
type OperationEvent struct {
	At      string  `json:"at,omitzero"`
	Status  string  `json:"status,omitzero"`
	Message *string `json:"message,omitzero"`
}

// Deployment is the panel deployment journal, distinct from the remote Agent on-disk journal.
type Deployment struct {
	Success                   *bool                    `json:"success,omitzero"`
	DeploymentID              string                   `json:"deployment_id,omitzero"`
	Status                    string                   `json:"status,omitzero"`
	ProfileID                 string                   `json:"profile_id,omitzero"`
	ServiceType               string                   `json:"service_type,omitzero"`
	CreatedAt                 string                   `json:"created_at,omitzero"`
	UpdatedAt                 *string                  `json:"updated_at,omitzero"`
	AgentUpdatedAt            *string                  `json:"agent_updated_at,omitzero"`
	Events                    []OperationEvent         `json:"events,omitzero"`
	Message                   *string                  `json:"message,omitzero"`
	Error                     *string                  `json:"error,omitzero"`
	ErrorDetails              *string                  `json:"error_details,omitzero"`
	FailedStage               *string                  `json:"failed_stage,omitzero"`
	RollbackError             *string                  `json:"rollback_error,omitzero"`
	RollbackErrorDetails      *string                  `json:"rollback_error_details,omitzero"`
	ConfigVersion             *string                  `json:"config_version,omitzero"`
	ConfigRevision            *Int                     `json:"config_revision,omitzero"`
	SHA256                    *string                  `json:"sha256,omitzero"`
	SourceSHA256              *string                  `json:"source_sha256,omitzero"`
	Submitted                 *bool                    `json:"submitted,omitzero"`
	PreviousRunning           *bool                    `json:"previous_running,omitzero"`
	Health                    json.RawMessage          `json:"health,omitzero"`
	Pending                   *bool                    `json:"pending,omitzero"`
	HTTPStatus                *Int                     `json:"http_status,omitzero"`
	ManualRollback            *bool                    `json:"manual_rollback,omitzero"`
	RollbackRejected          *bool                    `json:"rollback_rejected,omitzero"`
	RollbackMessage           *string                  `json:"rollback_message,omitzero"`
	DNSChecks                 []DeploymentDNSCheck     `json:"dns_checks,omitzero" elements:"keep"`
	DNSCheckedAt              *string                  `json:"dns_checked_at,omitzero"`
	PreparedContent           *PreparedContent         `json:"prepared_content,omitzero"`
	OxiDNSValidationReceipt   *OxiDNSValidationReceipt `json:"oxidns_validation_receipt,omitzero"`
	HAPublication             *HAPublicationSummary    `json:"ha_publication,omitzero"`
	IPv6VerificationRequested *bool                    `json:"ipv6_verification_requested,omitzero"`
	// Restart, FileCount, ProviderCount and RuleCount are response-only
	// members of a publish result; journals in storage never hold them.
	Restart       json.RawMessage `json:"restart,omitzero"`
	FileCount     *Int            `json:"file_count,omitzero"`
	ProviderCount *Int            `json:"provider_count,omitzero"`
	RuleCount     *Int            `json:"rule_count,omitzero"`
}

// DeploymentDNSCheck is a persisted bounded DNS report item.
type DeploymentDNSCheck struct {
	Name    string  `json:"name,omitzero"`
	Level   string  `json:"level,omitzero"`
	Message string  `json:"message,omitzero"`
	Fix     *string `json:"fix,omitzero"`
}

// PreparedContent records immutable prepared content identities without recalculating them.
type PreparedContent struct {
	Fingerprint          string              `json:"fingerprint,omitzero"`
	ProfileID            string              `json:"profile_id,omitzero"`
	ServiceType          string              `json:"service_type,omitzero"`
	ConfigPath           string              `json:"config_path,omitzero"`
	BaseURL              string              `json:"base_url,omitzero"`
	Files                *OrderedMap[string] `json:"files,omitzero"`
	DeployedConfigSHA256 *string             `json:"deployed_config_sha256,omitzero"`
	SettingsDigest       *string             `json:"settings_digest,omitzero"`
}

// AppliedContent is the confirmed current content pointer, not an inferred deployment outcome.
type AppliedContent struct {
	Fingerprint          string              `json:"fingerprint,omitzero"`
	ProfileID            string              `json:"profile_id,omitzero"`
	ServiceType          string              `json:"service_type,omitzero"`
	ConfigPath           string              `json:"config_path,omitzero"`
	BaseURL              string              `json:"base_url,omitzero"`
	Files                *OrderedMap[string] `json:"files,omitzero"`
	DeployedConfigSHA256 *string             `json:"deployed_config_sha256,omitzero"`
	SettingsDigest       *string             `json:"settings_digest,omitzero"`
	ConfigVersion        string              `json:"config_version,omitzero"`
	DeploymentID         string              `json:"deployment_id,omitzero"`
}

// OxiDNSValidationReceipt holds server-issued activation evidence; decoding does not validate or mint a receipt.
type OxiDNSValidationReceipt struct {
	Version      Int    `json:"version,omitzero"`
	RulesSHA256  string `json:"rules_sha256,omitzero"`
	AgentID      string `json:"agent_id,omitzero"`
	DeploymentID string `json:"deployment_id,omitzero"`
	ProfileID    string `json:"profile_id,omitzero"`
	ServiceType  string `json:"service_type,omitzero"`
	SHA256       string `json:"sha256,omitzero"`
	Fingerprint  string `json:"fingerprint,omitzero"`
}

// AgentUpgrade is the durable latest upgrade, owned by UpdateID.
type AgentUpgrade struct {
	UpdateID           string                      `json:"update_id,omitzero"`
	TargetVersion      string                      `json:"target_version,omitzero"`
	PreviousVersion    string                      `json:"previous_version,omitzero"`
	Status             string                      `json:"status,omitzero"`
	AgentID            string                      `json:"agent_id,omitzero"`
	StartedAt          float64                     `json:"started_at"`
	UpdatedAt          float64                     `json:"updated_at"`
	Protocol           Int                         `json:"protocol,omitzero"`
	Artifacts          OrderedMap[UpgradeArtifact] `json:"artifacts,omitzero"`
	Events             []OperationEvent            `json:"events,omitzero"`
	Message            *string                     `json:"message,omitzero"`
	Error              *string                     `json:"error,omitzero"`
	AgentUpdatedAt     *string                     `json:"agent_updated_at,omitzero"`
	RecoveryReportedAt *float64                    `json:"recovery_reported_at,omitzero"`
}

// UpgradeArtifact identifies an upgrade artifact without accessing its file.
type UpgradeArtifact struct {
	SHA256 string `json:"sha256,omitzero"`
	Size   Int    `json:"size"`
}

// IPv6VerificationSettings selects the client and URL used by readiness checks.
type IPv6VerificationSettings struct {
	ClientAgentID string `json:"client_agent_id,omitzero"`
	TestURL       string `json:"test_url,omitzero"`
}

// IPv6Verification retains a readiness result and the identities against which it was checked.
type IPv6Verification struct {
	Status              string                  `json:"status,omitzero"`
	Scope               string                  `json:"scope,omitzero"`
	Checks              []IPv6VerificationCheck `json:"checks,omitzero"`
	StartedAt           string                  `json:"started_at,omitzero"`
	StartedEpoch        float64                 `json:"started_epoch,omitzero"`
	RunID               string                  `json:"run_id,omitzero"`
	ProfileID           string                  `json:"profile_id,omitzero"`
	ConfigRevision      *Int                    `json:"config_revision,omitzero"`
	DeploymentID        *string                 `json:"deployment_id,omitzero"`
	SettingsFingerprint string                  `json:"settings_fingerprint,omitzero"`
	TargetFingerprint   string                  `json:"target_fingerprint,omitzero"`
	AppliedFingerprint  string                  `json:"applied_fingerprint,omitzero"`
	ClientFingerprint   *string                 `json:"client_fingerprint,omitzero"`
	Message             *string                 `json:"message,omitzero"`
	CheckedAt           *string                 `json:"checked_at,omitzero"`
	ErrorCode           *string                 `json:"error_code,omitzero"`
}

// IPv6VerificationCheck is one stored readiness check.
type IPv6VerificationCheck struct {
	ID      string   `json:"id,omitzero"`
	Label   string   `json:"label,omitzero"`
	Status  string   `json:"status,omitzero"`
	Message string   `json:"message,omitzero"`
	Details []string `json:"details,omitzero"`
	Scope   *string  `json:"scope,omitzero"`
}

// DeploymentFile describes one immutable archive entry; paths and digests are opaque to the codec.
type DeploymentFile struct {
	Path   string  `json:"path,omitzero"`
	Size   Int     `json:"size,omitzero"`
	SHA256 string  `json:"sha256,omitzero"`
	Role   string  `json:"role,omitzero"`
	Name   *string `json:"name,omitzero"`
}

// DeploymentManifest is a stored manifest counterpart independent of runtime deploy DTOs.
type DeploymentManifest struct {
	ProtocolVersion Int              `json:"protocol_version,omitzero"`
	DeploymentID    string           `json:"deployment_id,omitzero"`
	AgentID         string           `json:"agent_id,omitzero"`
	ProfileID       string           `json:"profile_id,omitzero"`
	ServiceType     string           `json:"service_type,omitzero"`
	ConfigPath      string           `json:"config_path,omitzero"`
	Files           []DeploymentFile `json:"files,omitzero"`
	ReloadOnly      *bool            `json:"reload_only,omitzero"`
	FinalConfig     *bool            `json:"final_config,omitzero"`
}

// AgentRuleSyncReceipt is the fixed external rule-sync envelope; Source is a captured configuration, not a live model graph.
type AgentRuleSyncReceipt struct {
	Source           json.RawMessage    `json:"source,omitzero"`
	Manifest         DeploymentManifest `json:"manifest,omitzero"`
	SHA256           string             `json:"sha256,omitzero"`
	ConfigVersion    string             `json:"config_version,omitzero"`
	BaseURL          string             `json:"base_url,omitzero"`
	ComparisonConfig *DeploymentFile    `json:"comparison_config,omitzero"`
}

// Plain-record codecs (O18 T1b); see plainCodec.

type operationEventWire OperationEvent

func (*OperationEvent) plainRecord() {}

// UnmarshalJSON decodes stored or captured OperationEvent data leniently (DecodeStored).
func (r *OperationEvent) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*operationEventWire)(r))
}

func (r *OperationEvent) plainWire() any { return (*operationEventWire)(r) }

// MarshalJSON writes the OperationEvent members in declaration order.
func (r OperationEvent) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type deploymentWire Deployment

func (*Deployment) plainRecord() {}

// UnmarshalJSON decodes stored or captured Deployment data leniently (DecodeStored).
func (r *Deployment) UnmarshalJSON(data []byte) error { return decodePlain(data, (*deploymentWire)(r)) }

func (r *Deployment) plainWire() any { return (*deploymentWire)(r) }

// MarshalJSON writes the Deployment members in declaration order.
func (r Deployment) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type deploymentDNSCheckWire DeploymentDNSCheck

func (*DeploymentDNSCheck) plainRecord() {}

// UnmarshalJSON decodes stored or captured DeploymentDNSCheck data leniently (DecodeStored).
func (r *DeploymentDNSCheck) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*deploymentDNSCheckWire)(r))
}

func (r *DeploymentDNSCheck) plainWire() any { return (*deploymentDNSCheckWire)(r) }

// MarshalJSON writes the DeploymentDNSCheck members in declaration order.
func (r DeploymentDNSCheck) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type preparedContentWire PreparedContent

func (*PreparedContent) plainRecord() {}

// UnmarshalJSON decodes stored or captured PreparedContent data leniently (DecodeStored).
func (r *PreparedContent) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*preparedContentWire)(r))
}

func (r *PreparedContent) plainWire() any { return (*preparedContentWire)(r) }

// MarshalJSON writes the PreparedContent members in declaration order.
func (r PreparedContent) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type appliedContentWire AppliedContent

func (*AppliedContent) plainRecord() {}

// UnmarshalJSON decodes stored or captured AppliedContent data leniently (DecodeStored).
func (r *AppliedContent) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*appliedContentWire)(r))
}

func (r *AppliedContent) plainWire() any { return (*appliedContentWire)(r) }

// MarshalJSON writes the AppliedContent members in declaration order.
func (r AppliedContent) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type oxiDNSValidationReceiptWire OxiDNSValidationReceipt

func (*OxiDNSValidationReceipt) plainRecord() {}

// UnmarshalJSON decodes stored or captured OxiDNSValidationReceipt data leniently (DecodeStored).
func (r *OxiDNSValidationReceipt) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*oxiDNSValidationReceiptWire)(r))
}

func (r *OxiDNSValidationReceipt) plainWire() any { return (*oxiDNSValidationReceiptWire)(r) }

// MarshalJSON writes the OxiDNSValidationReceipt members in declaration order.
func (r OxiDNSValidationReceipt) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type agentUpgradeWire AgentUpgrade

func (*AgentUpgrade) plainRecord() {}

// UnmarshalJSON decodes stored or captured AgentUpgrade data leniently (DecodeStored).
func (r *AgentUpgrade) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*agentUpgradeWire)(r))
}

func (r *AgentUpgrade) plainWire() any { return (*agentUpgradeWire)(r) }

// MarshalJSON writes the AgentUpgrade members in declaration order.
func (r AgentUpgrade) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// HasMembers reports whether AgentUpgrade holds any value.
func (r *AgentUpgrade) HasMembers() bool { return r != nil && hasMembers(r) }

type upgradeArtifactWire UpgradeArtifact

func (*UpgradeArtifact) plainRecord() {}

// UnmarshalJSON decodes stored or captured UpgradeArtifact data leniently (DecodeStored).
func (r *UpgradeArtifact) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*upgradeArtifactWire)(r))
}

func (r *UpgradeArtifact) plainWire() any { return (*upgradeArtifactWire)(r) }

// MarshalJSON writes the UpgradeArtifact members in declaration order.
func (r UpgradeArtifact) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type iPv6VerificationSettingsWire IPv6VerificationSettings

func (*IPv6VerificationSettings) plainRecord() {}

// UnmarshalJSON decodes stored or captured IPv6VerificationSettings data leniently (DecodeStored).
func (r *IPv6VerificationSettings) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*iPv6VerificationSettingsWire)(r))
}

func (r *IPv6VerificationSettings) plainWire() any { return (*iPv6VerificationSettingsWire)(r) }

// MarshalJSON writes the IPv6VerificationSettings members in declaration order.
func (r IPv6VerificationSettings) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type iPv6VerificationWire IPv6Verification

func (*IPv6Verification) plainRecord() {}

// UnmarshalJSON decodes stored or captured IPv6Verification data leniently (DecodeStored).
func (r *IPv6Verification) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*iPv6VerificationWire)(r))
}

func (r *IPv6Verification) plainWire() any { return (*iPv6VerificationWire)(r) }

// MarshalJSON writes the IPv6Verification members in declaration order.
func (r IPv6Verification) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type iPv6VerificationCheckWire IPv6VerificationCheck

func (*IPv6VerificationCheck) plainRecord() {}

// UnmarshalJSON decodes stored or captured IPv6VerificationCheck data leniently (DecodeStored).
func (r *IPv6VerificationCheck) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*iPv6VerificationCheckWire)(r))
}

func (r *IPv6VerificationCheck) plainWire() any { return (*iPv6VerificationCheckWire)(r) }

// MarshalJSON writes the IPv6VerificationCheck members in declaration order.
func (r IPv6VerificationCheck) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type deploymentFileWire DeploymentFile

func (*DeploymentFile) plainRecord() {}

// UnmarshalJSON decodes stored or captured DeploymentFile data leniently (DecodeStored).
func (r *DeploymentFile) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*deploymentFileWire)(r))
}

func (r *DeploymentFile) plainWire() any { return (*deploymentFileWire)(r) }

// MarshalJSON writes the DeploymentFile members in declaration order.
func (r DeploymentFile) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type deploymentManifestWire DeploymentManifest

func (*DeploymentManifest) plainRecord() {}

// UnmarshalJSON decodes stored or captured DeploymentManifest data leniently (DecodeStored).
func (r *DeploymentManifest) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*deploymentManifestWire)(r))
}

func (r *DeploymentManifest) plainWire() any { return (*deploymentManifestWire)(r) }

// MarshalJSON writes the DeploymentManifest members in declaration order.
func (r DeploymentManifest) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type agentRuleSyncReceiptWire AgentRuleSyncReceipt

func (*AgentRuleSyncReceipt) plainRecord() {}

// UnmarshalJSON decodes stored or captured AgentRuleSyncReceipt data leniently (DecodeStored).
func (r *AgentRuleSyncReceipt) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*agentRuleSyncReceiptWire)(r))
}

func (r *AgentRuleSyncReceipt) plainWire() any { return (*agentRuleSyncReceiptWire)(r) }

// MarshalJSON writes the AgentRuleSyncReceipt members in declaration order.
func (r AgentRuleSyncReceipt) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// HasMembers reports whether Deployment holds any value.
func (r *Deployment) HasMembers() bool { return r != nil && hasMembers(r) }
