package model

import "encoding/json"

// HASettings holds an explicit DNS HA member configuration.
type HASettings struct {
	Enabled *bool  `json:"enabled"`
	GroupID string `json:"group_id"`
	PeerID  string `json:"peer_id"`
	VIP     string `json:"vip"`
	VRID    Int    `json:"vrid"`
	Role    string `json:"role"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes HASettings with stored-member preservation and R5 fallback.
func (r *HASettings) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HASettings is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HASettings) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HASettings members.
func (r HASettings) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HAState holds the latest HA setup outcome; UpdatedAt is an integral epoch.
type HAState struct {
	Status    string   `json:"status"`
	Error     *string  `json:"error"`
	UpdatedAt Int      `json:"updated_at"`
	Members   []string `json:"members"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes HAState with stored-member preservation and R5 fallback.
func (r *HAState) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HAState is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HAState) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HAState members.
func (r HAState) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HAMember is one publication member outcome.
type HAMember struct {
	AgentID      string `json:"agent_id"`
	Name         string `json:"name"`
	DeploymentID string `json:"deployment_id"`
	Status       string `json:"status"`
	Fields       Fields
	raw          Raw
}

// UnmarshalJSON decodes HAMember with stored-member preservation and R5 fallback.
func (r *HAMember) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HAMember is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HAMember) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HAMember members.
func (r HAMember) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HAIdentity pins a child target identity for restart-safe ownership checks.
type HAIdentity struct {
	Host        string `json:"host"`
	Port        Int    `json:"port"`
	ProfileID   string `json:"profile_id"`
	ServiceType string `json:"service_type"`
	Enabled     *bool  `json:"enabled"`
	Fields      Fields
	raw         Raw
}

// UnmarshalJSON decodes HAIdentity with stored-member preservation and R5 fallback.
func (r *HAIdentity) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HAIdentity is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HAIdentity) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HAIdentity members.
func (r HAIdentity) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HAEnable is the keepalived enable request retained by a publication.
type HAEnable struct {
	PeerID string `json:"peer_id"`
	VIP    string `json:"vip"`
	VRID   Int    `json:"vrid"`
	Fields Fields
	raw    Raw
}

// UnmarshalJSON decodes HAEnable with stored-member preservation and R5 fallback.
func (r *HAEnable) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HAEnable is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HAEnable) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HAEnable members.
func (r HAEnable) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HasMembers reports effective JSON object presence without encoding payloads.
func (r *HAEnable) HasMembers() bool {
	return r != nil && recordHasMembers(r, &r.raw)
}

// HARouter identifies a router affected by a publication.
type HARouter struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ProfileID string `json:"profile_id"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes HARouter with stored-member preservation and R5 fallback.
func (r *HARouter) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HARouter is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HARouter) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HARouter members.
func (r HARouter) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HAHandoff retains the release handshake rather than inferring it from the active member.
type HAHandoff struct {
	OperationID   string   `json:"operation_id"`
	SourceAgentID string   `json:"source_agent_id"`
	TargetAgentID string   `json:"target_agent_id"`
	RequestedAt   float64  `json:"requested_at"`
	Confirmed     *bool    `json:"confirmed"`
	Released      *bool    `json:"released"`
	Confirmations Int      `json:"confirmations"`
	ReleasedAt    *float64 `json:"released_at"`
	Fields        Fields
	raw           Raw
}

// UnmarshalJSON decodes HAHandoff with stored-member preservation and R5 fallback.
func (r *HAHandoff) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HAHandoff is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HAHandoff) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HAHandoff members.
func (r HAHandoff) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HAHandoffFailure holds the persisted failure to finish a handoff.
type HAHandoffFailure struct {
	Message string `json:"message"`
	Status  string `json:"status"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes HAHandoffFailure with stored-member preservation and R5 fallback.
func (r *HAHandoffFailure) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HAHandoffFailure is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HAHandoffFailure) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HAHandoffFailure members.
func (r HAHandoffFailure) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HAJournal owns a multi-member publication; optional booleans retain presence-sensitive recovery decisions.
type HAJournal struct {
	ID                string            `json:"id"`
	OwnerAgentID      string            `json:"owner_agent_id"`
	OwnerDeploymentID string            `json:"owner_deployment_id"`
	Children          Map[string]       `json:"children"`
	Identities        Map[HAIdentity]   `json:"identities"`
	Status            string            `json:"status"`
	Phase             string            `json:"phase"`
	Message           string            `json:"message"`
	ActiveAgentID     *string           `json:"active_agent_id"`
	Activate          *bool             `json:"activate"`
	Order             []string          `json:"order"`
	Enable            *HAEnable         `json:"enable"`
	UpdatedAt         Int               `json:"updated_at"`
	HandoffProtocol   *Int              `json:"handoff_protocol"`
	Warnings          []string          `json:"warnings"`
	Routers           []HARouter        `json:"routers"`
	RuleSyncSettings  *RuleSyncSettings `json:"rule_sync_settings"`
	Kind              *string           `json:"kind"`
	Members           []HAMember        `json:"members"`
	Handoff           *HAHandoff        `json:"handoff"`
	HandoffRequired   *bool             `json:"handoff_required"`
	HandoffFailure    *HAHandoffFailure `json:"handoff_failure"`
	Fields            Fields
	raw               Raw
}

// UnmarshalJSON decodes HAJournal with stored-member preservation and R5 fallback.
func (r *HAJournal) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HAJournal is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HAJournal) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HAJournal members.
func (r HAJournal) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HasMembers reports effective JSON object presence without encoding payloads.
func (r *HAJournal) HasMembers() bool {
	return r != nil && recordHasMembers(r, &r.raw)
}

// HAPublicationSummary is the task projection of a journal, with no child or source graph.
type HAPublicationSummary struct {
	ID                string     `json:"id"`
	OwnerAgentID      string     `json:"owner_agent_id"`
	OwnerDeploymentID string     `json:"owner_deployment_id"`
	Status            string     `json:"status"`
	Phase             string     `json:"phase"`
	Message           string     `json:"message"`
	ActiveAgentID     *string    `json:"active_agent_id"`
	UpdatedAt         Int        `json:"updated_at"`
	Warnings          []string   `json:"warnings"`
	Routers           []HARouter `json:"routers"`
	Members           []HAMember `json:"members"`
	Fields            Fields
	raw               Raw
}

// UnmarshalJSON decodes HAPublicationSummary with stored-member preservation and R5 fallback.
func (r *HAPublicationSummary) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HAPublicationSummary is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HAPublicationSummary) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HAPublicationSummary members.
func (r HAPublicationSummary) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HABundle is the external payload metadata for an archive kept by the existing file store.
type HABundle struct {
	Manifest         DeploymentManifest `json:"manifest"`
	SHA256           string             `json:"sha256"`
	ConfigVersion    string             `json:"config_version"`
	ComparisonConfig *DeploymentFile    `json:"comparison_config"`
	Fields           Fields
	raw              Raw
}

// UnmarshalJSON decodes HABundle with stored-member preservation and R5 fallback.
func (r *HABundle) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HABundle is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HABundle) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HABundle members.
func (r HABundle) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// HAPayload is the fixed external envelope; captured source configurations remain opaque.
type HAPayload struct {
	ID             string          `json:"id"`
	BaseURL        string          `json:"base_url"`
	ConfigRevision *Int            `json:"config_revision"`
	SourceSnapshot json.RawMessage `json:"source_snapshot"`
	ResourceConfig json.RawMessage `json:"resource_config"`
	Bundle         *HABundle       `json:"bundle"`
	Fields         Fields
	raw            Raw
}

// UnmarshalJSON decodes HAPayload with stored-member preservation and R5 fallback.
func (r *HAPayload) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when HAPayload is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *HAPayload) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged HAPayload members.
func (r HAPayload) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// MemberJSON returns one effective HA identity member by its exact stored key.
func (r *HASettings) MemberJSON(name string) (json.RawMessage, bool, error) {
	return recordMemberJSON(r, &r.raw, name)
}

// MemberJSON returns one publication member without encoding captured payloads.
func (r *HAJournal) MemberJSON(name string) (json.RawMessage, bool, error) {
	return recordMemberJSON(r, &r.raw, name)
}

// SetJSON replaces one public HA summary member through the shared codec.
func (r *HAPublicationSummary) SetJSON(name string, value json.RawMessage) error {
	return SetMemberJSON(r, &r.raw, name, value)
}

// HasMembers reports effective JSON object presence without encoding payloads.
func (r *HAPublicationSummary) HasMembers() bool {
	return r != nil && recordHasMembers(r, &r.raw)
}

// MemberJSON returns one original summary member without encoding its siblings.
func (r *HAPublicationSummary) MemberJSON(member string) (json.RawMessage, bool, error) {
	return recordMemberJSON(r, &r.raw, member)
}

// MemberJSON reads one original HA identity field without scalar projection.
func (r *HAIdentity) MemberJSON(name string) (json.RawMessage, bool, error) {
	return recordMemberJSON(r, &r.raw, name)
}

// MemberJSON reads one original HA enable field without scalar projection.
func (r *HAEnable) MemberJSON(name string) (json.RawMessage, bool, error) {
	return recordMemberJSON(r, &r.raw, name)
}
