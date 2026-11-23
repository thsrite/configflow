package routes

import (
	"encoding/json"

	"agent/netfilter"
)

// All HTTP JSON response structs declare their exported fields in strict
// lexicographical order of their JSON tag names so encoding/json emits
// byte-for-byte identical output (field names, types, null/omission semantics,
// and member order) to the previous map[string]any / map[string]string payloads.

// MessageResponse is the standard {message, success} JSON response.
type MessageResponse struct {
	Message string `json:"message"`
	Success bool   `json:"success"`
}

// StringStatusMessageResponse preserves legacy endpoints whose "success" field
// is encoded as the string "true" or "false" (auth, restart, uninstall).
type StringStatusMessageResponse struct {
	Message string `json:"message"`
	Success string `json:"success"`
}

// HealthResponse is the response for GET /health.
type HealthResponse struct {
	Status string `json:"status"`
}

// CodedErrorResponse is the standard {code, message, success} error payload.
type CodedErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Success bool   `json:"success"`
}

// DeploymentFailureResponse is returned when a deployment or upgrade route fails.
type DeploymentFailureResponse struct {
	ErrorDetails string `json:"error_details,omitempty"`
	Message      string `json:"message"`
	Success      bool   `json:"success"`
}

// DeploymentAcceptedResponse is returned for 202 Accepted deployment transitions.
type DeploymentAcceptedResponse struct {
	DeploymentID   string `json:"deployment_id"`
	ManualRollback bool   `json:"manual_rollback,omitempty"`
	Recovering     bool   `json:"recovering,omitempty"`
	Status         string `json:"status"`
	Success        bool   `json:"success"`
}

// CapabilitiesResponse is returned by GET /api/capabilities.
type CapabilitiesResponse struct {
	AIOForwardingProtocols       []int              `json:"aio_forwarding_protocols,omitempty"`
	ConfigDirectory              *string            `json:"config_directory,omitempty"`
	ConfigPath                   string             `json:"config_path"`
	DeploymentProtocols          []int              `json:"deployment_protocols"`
	FinalConfigProtocols         []int              `json:"final_config_protocols"`
	IPv6ClientProbeProtocols     []int              `json:"ipv6_client_probe_protocols,omitempty"`
	IPv6ReadinessProtocols       []int              `json:"ipv6_readiness_protocols,omitempty"`
	IPv6VerificationProtocols    []int              `json:"ipv6_verification_protocols,omitempty"`
	MihomoResources              *[]string          `json:"mihomo_resources,omitempty"`
	OxiDNSHAContainerProtocols   []int              `json:"oxidns_ha_container_protocols,omitempty"`
	OxiDNSHAEnvironment          *haEnvironmentInfo `json:"oxidns_ha_environment,omitempty"`
	OxiDNSHAHandoffProtocols     []int              `json:"oxidns_ha_handoff_protocols,omitempty"`
	OxiDNSHAProtocols            []int              `json:"oxidns_ha_protocols,omitempty"`
	Platform                     string             `json:"platform"`
	ReloadOnlyProtocols          []int              `json:"reload_only_protocols"`
	ReturnRoutingSafetyProtocols []int              `json:"return_routing_safety_protocols"`
	RuleSyncSnapshotProtocols    []int              `json:"rule_sync_snapshot_protocols"`
	ServiceType                  string             `json:"service_type"`
	Success                      bool               `json:"success"`
}

// DNSActiveResponse is returned by GET /api/dns/active.
type DNSActiveResponse struct {
	AgentID      string `json:"agent_id"`
	DeploymentID string `json:"deployment_id"`
	Success      bool   `json:"success"`
}

// DNSListenerResponse is returned by GET /api/dns/listener.
type DNSListenerResponse struct {
	AgentID      string `json:"agent_id"`
	DeploymentID string `json:"deployment_id"`
	Enabled      bool   `json:"enabled"`
	Host         string `json:"host"`
	Listen       string `json:"listen"`
	Port         int    `json:"port"`
	Success      bool   `json:"success"`
}

// DNSPeerResponse is returned by GET /api/dns/peer.
type DNSPeerResponse struct {
	PeerAgentID string `json:"peer_agent_id"`
	ServiceType string `json:"service_type"`
	Success     bool   `json:"success"`
}

// FinalConfigPreviewResponse is returned by POST /api/config/final-preview.
type FinalConfigPreviewResponse struct {
	Content string `json:"content"`
	Success bool   `json:"success"`
}

// IPv6ConnectivityResponse is returned by POST /api/ipv6-readiness/probe.
type IPv6ConnectivityResponse struct {
	Connectivity ipv6Connectivity `json:"connectivity"`
	Success      bool             `json:"success"`
}

// LogSourceErrorResponse is returned by GET /api/logs?source=... on read error.
type LogSourceErrorResponse struct {
	Message string    `json:"message"`
	Source  LogSource `json:"source"`
	Success bool      `json:"success"`
}

// LogSourceContentResponse is returned by GET /api/logs?source=... on success.
type LogSourceContentResponse struct {
	Logs        string    `json:"logs"`
	ServiceName string    `json:"service_name"`
	Source      LogSource `json:"source"`
	Success     bool      `json:"success"`
}

// LogPathContentResponse is returned by GET /api/logs?log_path=... and /logs?log_path=...
type LogPathContentResponse struct {
	Logs        string `json:"logs"`
	ServiceName string `json:"service_name"`
	Success     bool   `json:"success"`
}

// DefaultLogsResponse is returned by GET /api/logs and /logs without source/log_path.
type DefaultLogsResponse struct {
	ErrorLogs   string `json:"error_logs"`
	Logs        string `json:"logs"`
	ServiceName string `json:"service_name"`
	Success     bool   `json:"success"`
}

// LogSourcesResponse is returned by GET /api/logs/sources.
type LogSourcesResponse struct {
	Sources []LogSource `json:"sources"`
	Success bool        `json:"success"`
}

// OxiDNSCacheListResponse is returned by GET /api/oxidns/cache.
type OxiDNSCacheListResponse struct {
	Caches  []string `json:"caches"`
	Success bool     `json:"success"`
}

// OxiDNSCacheFlushResponse is returned by POST /api/oxidns/cache/flush on success.
type OxiDNSCacheFlushResponse struct {
	Flushed []string `json:"flushed"`
	Success bool     `json:"success"`
}

// OxiDNSCacheFlushErrorResponse is returned by POST /api/oxidns/cache/flush on failure.
type OxiDNSCacheFlushErrorResponse struct {
	Code    string   `json:"code"`
	Flushed []string `json:"flushed"`
	Message string   `json:"message"`
	Success bool     `json:"success"`
}

// OxiDNSHAErrorResponse is returned by /api/oxidns-ha* on failure.
type OxiDNSHAErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Success bool   `json:"success"`
}

// OxiDNSHARemoveResponse is returned by DELETE /api/oxidns-ha.
type OxiDNSHARemoveResponse struct {
	Removed bool `json:"removed"`
	Success bool `json:"success"`
}

// OxiDNSHAPreviewResponse is returned by POST /api/oxidns-ha/preview.
type OxiDNSHAPreviewResponse struct {
	Blockers    []string          `json:"blockers"`
	CIDR        string            `json:"cidr"`
	Environment haEnvironmentInfo `json:"environment"`
	Interface   string            `json:"interface"`
	OK          bool              `json:"ok"`
	Success     bool              `json:"success"`
}

// OxiDNSHAManagedStatus is the "managed" object inside OxiDNSHAStatusResponse.
type OxiDNSHAManagedStatus struct {
	Active    bool         `json:"active"`
	AppliedAt string       `json:"applied_at"`
	CIDR      string       `json:"cidr"`
	Interface string       `json:"interface"`
	Pending   bool         `json:"pending"`
	Spec      OxiDNSHASpec `json:"spec"`
}

// OxiDNSHARuntimeStatus is the "runtime" object inside OxiDNSHAStatusResponse.
type OxiDNSHARuntimeStatus struct {
	Healthy           bool   `json:"healthy"`
	KeepalivedRunning bool   `json:"keepalived_running"`
	Role              string `json:"role"`
	VIPHealthy        bool   `json:"vip_healthy"`
	VIPPresent        bool   `json:"vip_present"`
}

// OxiDNSHAStatusResponse is returned by GET/PUT /api/oxidns-ha and /api/oxidns-ha/handoff.
type OxiDNSHAStatusResponse struct {
	Environment haEnvironmentInfo      `json:"environment"`
	Error       string                 `json:"error"`
	Handoff     any                    `json:"handoff"`
	Managed     *OxiDNSHAManagedStatus `json:"managed"`
	Protocol    int                    `json:"protocol"`
	Runtime     OxiDNSHARuntimeStatus  `json:"runtime"`
	Success     bool                   `json:"success"`
}

// OxiDNSOverviewResponse is returned by GET /api/oxidns/overview.
type OxiDNSOverviewResponse struct {
	Capabilities    oxMonitorCapabilities `json:"capabilities"`
	CollectedAtMS   int64                 `json:"collected_at_ms"`
	Health          json.RawMessage       `json:"health"`
	Metrics         string                `json:"metrics"`
	RuntimeRevision string                `json:"runtime_revision"`
	SourceRevision  string                `json:"source_revision"`
	Success         bool                  `json:"success"`
	UpstreamLabels  []oxUpstreamLabel     `json:"upstream_labels"`
}

// OxiDNSRankingsResponse is returned by GET /api/oxidns/rankings.
type OxiDNSRankingsResponse struct {
	Blocked         map[string]any `json:"blocked"`
	Clients         oxRankingGroup `json:"clients"`
	CollectedAtMS   int64          `json:"collected_at_ms"`
	Domains         oxRankingGroup `json:"domains"`
	InstanceID      string         `json:"instance_id"`
	Recorder        string         `json:"recorder"`
	RetentionDays   *uint64        `json:"retention_days"`
	RowsLimit       int            `json:"rows_limit"`
	RuntimeRevision string         `json:"runtime_revision"`
	SampleLimit     int            `json:"sample_limit"`
	SourceRevision  string         `json:"source_revision"`
	Success         bool           `json:"success"`
}

// OxiDNSRuleUpdateResponse is returned by GET/POST /api/oxidns/rules/update.
type OxiDNSRuleUpdateResponse struct {
	Success bool             `json:"success"`
	Task    oxRuleUpdateTask `json:"task"`
}

// LinuxGatewayManagedStatus is the "managed" object in /api/linux-gateway/* responses.
type LinuxGatewayManagedStatus struct {
	Active              bool              `json:"active"`
	AppliedAt           string            `json:"applied_at,omitempty"`
	Error               string            `json:"error,omitempty"`
	Healthy             bool              `json:"healthy"`
	Pending             *bool             `json:"pending,omitempty"`
	PreservesForwarding bool              `json:"preserves_forwarding"`
	Spec                *LinuxGatewaySpec `json:"spec,omitempty"`
}

// LinuxGatewayStatusResponse is returned by GET /api/linux-gateway/status.
type LinuxGatewayStatusResponse struct {
	ConfigError       string                    `json:"config_error,omitempty"`
	Detection         gatewayDetection          `json:"detection"`
	DetectionError    string                    `json:"detection_error,omitempty"`
	EnabledInConfig   bool                      `json:"enabled_in_config"`
	ForwardFilter     netfilter.Result          `json:"forward_filter"`
	IPv4Forwarding    any                       `json:"ipv4_forwarding"`
	Managed           LinuxGatewayManagedStatus `json:"managed"`
	Success           bool                      `json:"success"`
	Supported         bool                      `json:"supported"`
	TUN               any                       `json:"tun"`
	UnsupportedReason string                    `json:"unsupported_reason,omitempty"`
}

// LinuxGatewayPreviewResponse is returned by POST /api/linux-gateway/preview.
type LinuxGatewayPreviewResponse struct {
	Blockers  []string                  `json:"blockers"`
	Commands  []string                  `json:"commands"`
	Detection gatewayDetection          `json:"detection"`
	Managed   LinuxGatewayManagedStatus `json:"managed"`
	Ready     bool                      `json:"ready"`
	Spec      LinuxGatewaySpec          `json:"spec"`
	Success   bool                      `json:"success"`
	Warnings  []string                  `json:"warnings"`
}

// LinuxGatewayActionResponse is returned by POST /api/linux-gateway/apply and /remove.
type LinuxGatewayActionResponse struct {
	Managed LinuxGatewayManagedStatus `json:"managed"`
	Message string                    `json:"message"`
	Success bool                      `json:"success"`
}

// LinuxGatewayForwardingErrorResponse is returned by POST /api/linux-gateway/forwarding on 409.
type LinuxGatewayForwardingErrorResponse struct {
	IPv4Forwarding any    `json:"ipv4_forwarding"`
	Message        string `json:"message"`
	Success        bool   `json:"success"`
}

// LinuxGatewayForwardingSuccessResponse is returned by POST /api/linux-gateway/forwarding on 200.
type LinuxGatewayForwardingSuccessResponse struct {
	IPv4Forwarding any                       `json:"ipv4_forwarding"`
	Managed        LinuxGatewayManagedStatus `json:"managed"`
	Message        string                    `json:"message"`
	Success        bool                      `json:"success"`
	TUNPrepare     any                       `json:"tun_prepare"`
}

// ReturnRoutingManagedError is the "managed" value when the return-routing state file cannot be loaded.
type ReturnRoutingManagedError struct {
	Error string `json:"error"`
}

// ReturnRoutingManagedDetail is the "managed" value when return-routing state exists.
type ReturnRoutingManagedDetail struct {
	Active        bool              `json:"active"`
	AppliedAt     string            `json:"applied_at"`
	Legacy        *legacyBackup     `json:"legacy"`
	SafetyVersion int               `json:"safety_version"`
	Spec          ReturnRoutingSpec `json:"spec"`
	TablePresent  bool              `json:"table_present"`
}

// ReturnRoutingSafetyStatus is the "safety" object inside ReturnRoutingGetResponse.
type ReturnRoutingSafetyStatus struct {
	Allocation *returnAllocation `json:"allocation,omitempty"`
	Blockers   []string          `json:"blockers"`
	OK         bool              `json:"ok"`
	Protocols  []int             `json:"protocols"`
	Version    int               `json:"version"`
}

// ReturnRoutingGetResponse is returned by GET /api/return-routing.
type ReturnRoutingGetResponse struct {
	FwmarkRules      []fwmarkRule              `json:"fwmark_rules"`
	IPRouteAvailable bool                      `json:"iproute_available"`
	Managed          any                       `json:"managed"`
	NFTAvailable     bool                      `json:"nft_available"`
	NFTTables        []string                  `json:"nft_tables"`
	ReturnTables     []returnTable             `json:"return_tables"`
	Safety           ReturnRoutingSafetyStatus `json:"safety"`
	Success          bool                      `json:"success"`
	TableRoutes      map[string][]string       `json:"table_routes"`
	TunDevices       []string                  `json:"tun_devices"`
}

// ReturnRoutingApplyResponse is returned by PUT /api/return-routing.
type ReturnRoutingApplyResponse struct {
	LanCIDR       string `json:"lan_cidr"`
	LanInterface  string `json:"lan_interface"`
	RouterMAC     string `json:"router_mac"`
	SafetyVersion int    `json:"safety_version"`
	Success       bool   `json:"success"`
	TunDevice     string `json:"tun_device"`
}

// ReturnRoutingRemoveResponse is returned by DELETE /api/return-routing.
type ReturnRoutingRemoveResponse struct {
	Removed            bool `json:"removed"`
	RetainedEmptyTable bool `json:"retained_empty_table,omitempty"`
	Success            bool `json:"success"`
}

// UpgradeInfoResponse is returned by GET /api/upgrade-info.
type UpgradeInfoResponse struct {
	AgentID          string `json:"agent_id"`
	Architecture     string `json:"architecture"`
	DeploymentMethod string `json:"deployment_method"`
	MigrationReady   bool   `json:"migration_ready"`
	UpdateProtocol   int    `json:"update_protocol"`
	Version          string `json:"version"`
}
