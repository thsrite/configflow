// Package deploy prepares immutable deployment files, private previews and
// publication receipts. Device publication is owned by the publishing chain.
package deploy

import (
	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"encoding/json"
)

// Role identifies a delivered file's purpose.
type Role string

// Deployment file roles, as stored and sent to Agents.
const (
	RoleConfig   Role = "config"
	RoleProvider Role = "provider"
	RoleRule     Role = "rule"
	RoleCustom   Role = "custom"
	RoleResource Role = "resource"
)

// File is one manifest entry.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Role   Role   `json:"role"`
	Name   string `json:"name,omitempty"`
}

// Manifest is the Agent protocol's manifest.json, in Python's field order.
type Manifest struct {
	ProtocolVersion int    `json:"protocol_version"`
	DeploymentID    string `json:"deployment_id"`
	AgentID         string `json:"agent_id"`
	ProfileID       string `json:"profile_id"`
	ServiceType     string `json:"service_type"`
	ConfigPath      string `json:"config_path"`
	Files           []File `json:"files"`
	ReloadOnly      bool   `json:"reload_only,omitempty"`
	FinalConfig     bool   `json:"final_config,omitempty"`
}

// Content holds one immutable file's bytes and role.
type Content struct {
	Data []byte
	Role Role
}

// Target is a captured Agent identity; Record is opaque to deploy logic.
type Target struct {
	ID, Name, ProfileID, ServiceType, DeploymentMethod, Version, Host string
	Port                                                              json.RawMessage
	Token                                                             string
	DomainDiscovery                                                   bool
	Record                                                            *model.Agent
}

// PreparedContent is the comparison baseline kept in an Agent record.
type PreparedContent struct {
	Fingerprint          string            `json:"fingerprint"`
	ProfileID            string            `json:"profile_id"`
	ServiceType          string            `json:"service_type"`
	ConfigPath           string            `json:"config_path"`
	BaseURL              string            `json:"base_url"`
	Files                map[string]string `json:"files,omitempty"`
	DeployedConfigSHA256 string            `json:"deployed_config_sha256,omitempty"`
	SettingsDigest       string            `json:"settings_digest,omitempty"`
}

// ValidationReceipt is server-owned evidence of a validated OxiDNS bundle.
type ValidationReceipt struct {
	Version      int    `json:"version"`
	RulesSHA256  string `json:"rules_sha256"`
	AgentID      string `json:"agent_id"`
	DeploymentID string `json:"deployment_id"`
	ProfileID    string `json:"profile_id"`
	ServiceType  string `json:"service_type"`
	SHA256       string `json:"sha256"`
	Fingerprint  string `json:"fingerprint"`
}

// Receipt is an immutable rule-sync baseline and its optional loaded bundle.
type Receipt struct {
	Source           json.RawMessage `json:"source"`
	Manifest         Manifest        `json:"manifest"`
	SHA256           string          `json:"sha256"`
	ConfigVersion    string          `json:"config_version"`
	BaseURL          string          `json:"base_url"`
	ComparisonConfig *File           `json:"comparison_config"`
	Bundle           *Bundle         `json:"-"`
}

// Revision distinguishes a missing revision from an explicit null.
type Revision struct {
	Value int64
	Null  bool
}

// Update holds only the panel-originated task fields present in an update.
type Update struct {
	Status, Message, Error, ErrorDetails, FailedStage *string
	Success, Submitted, ManualRollback                *bool
	HTTPStatus                                        int
	SHA256, SourceSHA256, ConfigVersion               *string
	ConfigRevision                                    *Revision
}

// Remedy identifies a download setting that may restore connectivity.
type Remedy struct {
	Kind   string `json:"kind"`
	Github bool   `json:"github"`
}

// PreparationError means a complete artifact cannot safely be prepared.
type PreparationError struct {
	Message string
	Issues  []oxidns.Issue
	Remedy  *Remedy
	cause   error
}

func (e *PreparationError) Error() string { return e.Message }

// Unwrap retains the underlying error for classification.
func (e *PreparationError) Unwrap() error { return e.cause }

// RuleDownloadError is a preparation error with optional network remedy.
type RuleDownloadError struct{ *PreparationError }

// Unwrap exposes the embedded preparation error (Python: RuleDownloadError is a
// DeploymentPreparationError), whose own Unwrap continues to the cause; without
// it errors.As(err, **PreparationError) would skip straight to the cause.
func (e *RuleDownloadError) Unwrap() error { return e.PreparationError }

// PreviewChanged requires generation of a new immutable preview.
type PreviewChanged struct{ Message string }

func (e *PreviewChanged) Error() string { return e.Message }

// ProviderFile is one pre-rendered subscription provider.
type ProviderFile struct {
	Name, LocalPath, URL string
	Content              []byte
}

// CustomFile is one explicit attachment.
type CustomFile struct {
	Path, Name string
	Content    []byte
	Native     bool
}

// PinnedFile preserves trusted bytes from a confirmed deployment.
type PinnedFile struct {
	Path    string
	Content []byte
	Role    Role
}

// Progress receives optional preparation notifications. Callbacks may run concurrently.
type Progress struct {
	Rules  func(done, total int)
	Detail func(done *int, total int, name, phase string)
	Phase  func(message string)
	// Message reports an unforced detail that the owner may throttle (Python's
	// phase_progress(message) without force). Nil falls back to Phase.
	Message func(message string)
}

// BuildInput captures all inputs to one immutable bundle.
type BuildInput struct {
	Target                Target
	Source                *Source
	ConfigContent         string
	ProviderDownloads     []ProviderFile
	RulesetDownloads      []convert.ResourceDownload
	CustomFiles           []CustomFile
	PinnedFiles           []PinnedFile
	BaseURL, DeploymentID string
	LocalDNSWatchdog      bool
	Progress              Progress
}

// Capabilities is the typed subset of an Agent capability response.
type Capabilities struct {
	DeploymentProtocols  []int    `json:"deployment_protocols"`
	FinalConfigProtocols []int    `json:"final_config_protocols"`
	ReloadOnlyProtocols  []int    `json:"reload_only_protocols"`
	ServiceType          string   `json:"service_type"`
	ConfigPath           string   `json:"config_path"`
	ConfigDirectory      string   `json:"config_directory"`
	MihomoResources      []string `json:"mihomo_resources"`
}

// UnmarshalJSON refuses null or missing file metadata rather than turning it
// into a valid zero-length rule. Zero-length files must carry an explicit size.
func (f *File) UnmarshalJSON(data []byte) error {
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var next File
	for _, item := range []struct {
		name     string
		target   any
		required bool
	}{
		{"path", &next.Path, true}, {"size", &next.Size, true}, {"sha256", &next.SHA256, true}, {"role", &next.Role, true}, {"name", &next.Name, false},
	} {
		raw, present := fields.Get(item.name)
		if item.required && (!present || string(raw) == "null") {
			return preparation("Invalid deployment manifest file metadata")
		}
		if present {
			if err := json.Unmarshal(raw, item.target); err != nil {
				return err
			}
		}
	}
	*f = next
	return nil
}

// UnmarshalJSON reads exact protocol member names and refuses null or nonboolean flags.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var next Manifest
	for _, item := range []struct {
		name   string
		target any
	}{
		{"protocol_version", &next.ProtocolVersion}, {"deployment_id", &next.DeploymentID}, {"agent_id", &next.AgentID},
		{"profile_id", &next.ProfileID}, {"service_type", &next.ServiceType}, {"config_path", &next.ConfigPath}, {"files", &next.Files},
		{"reload_only", &next.ReloadOnly}, {"final_config", &next.FinalConfig},
	} {
		raw, present := fields.Get(item.name)
		if !present {
			continue
		}
		if (item.name == "reload_only" || item.name == "final_config") && string(raw) != "true" && string(raw) != "false" {
			return preparation(item.name + " must be a boolean")
		}
		if err := json.Unmarshal(raw, item.target); err != nil {
			return err
		}
	}
	*m = next
	return nil
}
