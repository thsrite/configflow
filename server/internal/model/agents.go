package model

import (
	"encoding/json"
)

// Agent is a stored registry entry; liveness overlays and public projections are not synthesized here.
type Agent struct {
	LoggingConfig            *AgentLoggingConfig           `json:"logging_config,omitzero"`
	ID                       string                        `json:"id,omitzero"`
	Name                     string                        `json:"name,omitzero"`
	Host                     string                        `json:"host,omitzero"`
	Port                     Int                           `json:"port,omitzero"`
	Token                    string                        `json:"token,omitzero"`
	ServiceType              string                        `json:"service_type,omitzero"`
	ProfileID                string                        `json:"profile_id,omitzero"`
	DeploymentMethod         string                        `json:"deployment_method,omitzero"`
	Enabled                  *bool                         `json:"enabled,omitzero" stored:"gate"`
	CreatedAt                string                        `json:"created_at,omitzero"`
	UpdatedAt                *string                       `json:"updated_at,omitzero"`
	Status                   string                        `json:"status,omitzero"`
	LastHeartbeat            string                        `json:"last_heartbeat,omitzero"`
	Version                  string                        `json:"version,omitzero"`
	ConfigVersion            string                        `json:"config_version,omitzero"`
	ConfigRevision           *Int                          `json:"config_revision,omitzero"`
	PushedAt                 *string                       `json:"pushed_at,omitzero"`
	GatewayInstallationID    *string                       `json:"gateway_installation_id,omitzero"`
	DockerHAInstallationID   *string                       `json:"docker_ha_installation_id,omitzero"`
	DomainDiscoveryEnabled   *bool                         `json:"domain_discovery_enabled,omitzero" stored:"gate"`
	TrafficHistoryEnabled    *bool                         `json:"traffic_history_enabled,omitzero" stored:"gate"`
	TrafficHistorySince      *string                       `json:"traffic_history_since,omitzero"`
	Deployments              OrderedMap[Deployment]        `json:"deployments,omitzero"`
	LatestDeployment         *Deployment                   `json:"latest_deployment,omitzero"`
	AppliedContent           *AppliedContent               `json:"applied_content,omitzero"`
	LatestUpgrade            *AgentUpgrade                 `json:"latest_upgrade,omitzero"`
	RuleSync                 *AgentRuleSync                `json:"rule_sync,omitzero"`
	DNSFollow                *DNSFollowBinding             `json:"dns_follow,omitzero"`
	DNSFollowState           *DNSFollowState               `json:"dns_follow_state,omitzero"`
	DNSFollowJob             *DNSFollowJob                 `json:"dns_follow_job,omitzero"`
	OxiDNSHA                 *HASettings                   `json:"oxidns_ha,omitzero"`
	OxiDNSHAState            *HAState                      `json:"oxidns_ha_state,omitzero"`
	OxiDNSHAPublication      *HAJournal                    `json:"oxidns_ha_publication,omitzero"`
	IPv6VerificationSettings *IPv6VerificationSettings     `json:"ipv6_verification_settings,omitzero"`
	IPv6Verifications        OrderedMap[IPv6Verification]  `json:"ipv6_verifications,omitzero"`
	NodeTestDNSEndpoints     OrderedMap[LegacyDNSEndpoint] `json:"node_test_dns_endpoints,omitzero"`
	ServiceStatus            *string                       `json:"service_status,omitzero"`
	SystemMetrics            *AgentMetrics                 `json:"system_metrics,omitzero"`
	CPU                      *AgentCPU                     `json:"cpu,omitzero"`
	Memory                   *AgentMemory                  `json:"memory,omitzero"`
	Disk                     *AgentDisk                    `json:"disk,omitzero"`
	Network                  *AgentNetwork                 `json:"network,omitzero"`
}

// AgentMetrics preserves older embedded metrics without introducing persistent heartbeat writes.
type AgentMetrics struct {
	CollectedAt string       `json:"collected_at,omitzero"`
	CPU         AgentCPU     `json:"cpu,omitzero"`
	Memory      AgentMemory  `json:"memory,omitzero"`
	Disk        AgentDisk    `json:"disk,omitzero"`
	Network     AgentNetwork `json:"network,omitzero"`
}

// AgentCPU holds a legacy CPU sample.
type AgentCPU struct {
	CoreCount    Number `json:"core_count,omitzero"`
	UsagePercent Number `json:"usage_percent,omitzero"`
}

// AgentMemory holds a legacy memory sample.
type AgentMemory struct {
	Total       Number `json:"total,omitzero"`
	Used        Number `json:"used,omitzero"`
	Available   Number `json:"available,omitzero"`
	UsedPercent Number `json:"used_percent,omitzero"`
}

// AgentDisk holds a legacy disk sample.
type AgentDisk struct {
	Total       Number `json:"total,omitzero"`
	Used        Number `json:"used,omitzero"`
	Free        Number `json:"free,omitzero"`
	UsedPercent Number `json:"used_percent,omitzero"`
}

// AgentNetwork holds legacy byte counters and integral bytes-per-second rates.
type AgentNetwork struct {
	BytesSent Number `json:"bytes_sent,omitzero"`
	BytesRecv Number `json:"bytes_recv,omitzero"`
	SpeedSent Number `json:"speed_sent,omitzero"`
	SpeedRecv Number `json:"speed_recv,omitzero"`
}

// LegacyDNSEndpoint retains the removed managed-node-test listener proof until its consumer explicitly removes it.
type LegacyDNSEndpoint struct {
	Endpoint  string    `json:"endpoint,omitzero"`
	ProfileID string    `json:"profile_id,omitzero"`
	Status    string    `json:"status,omitzero"`
	Installed *bool     `json:"installed,omitzero"`
	Plugins   []Dynamic `json:"plugins,omitzero"`
}

// AgentLoggingConfig holds the Agent's opaque logging switch and future options.
type AgentLoggingConfig struct {
	Enabled json.RawMessage `json:"enabled,omitzero"`
}

// Plain-record codecs (O18 T1b); see plainCodec.

type agentWire Agent

func (*Agent) plainRecord() {}

// UnmarshalJSON decodes stored or captured Agent data leniently (DecodeStored).
func (r *Agent) UnmarshalJSON(data []byte) error { return decodePlain(data, (*agentWire)(r)) }

func (r *Agent) plainWire() any { return (*agentWire)(r) }

// MarshalJSON writes the Agent members in declaration order.
func (r Agent) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type agentMetricsWire AgentMetrics

func (*AgentMetrics) plainRecord() {}

// UnmarshalJSON decodes stored or captured AgentMetrics data leniently (DecodeStored).
func (r *AgentMetrics) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*agentMetricsWire)(r))
}

func (r *AgentMetrics) plainWire() any { return (*agentMetricsWire)(r) }

// MarshalJSON writes the AgentMetrics members in declaration order.
func (r AgentMetrics) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type agentCPUWire AgentCPU

func (*AgentCPU) plainRecord() {}

// UnmarshalJSON decodes stored or captured AgentCPU data leniently (DecodeStored).
func (r *AgentCPU) UnmarshalJSON(data []byte) error { return decodePlain(data, (*agentCPUWire)(r)) }

func (r *AgentCPU) plainWire() any { return (*agentCPUWire)(r) }

// MarshalJSON writes the AgentCPU members in declaration order.
func (r AgentCPU) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type agentMemoryWire AgentMemory

func (*AgentMemory) plainRecord() {}

// UnmarshalJSON decodes stored or captured AgentMemory data leniently (DecodeStored).
func (r *AgentMemory) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*agentMemoryWire)(r))
}

func (r *AgentMemory) plainWire() any { return (*agentMemoryWire)(r) }

// MarshalJSON writes the AgentMemory members in declaration order.
func (r AgentMemory) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type agentDiskWire AgentDisk

func (*AgentDisk) plainRecord() {}

// UnmarshalJSON decodes stored or captured AgentDisk data leniently (DecodeStored).
func (r *AgentDisk) UnmarshalJSON(data []byte) error { return decodePlain(data, (*agentDiskWire)(r)) }

func (r *AgentDisk) plainWire() any { return (*agentDiskWire)(r) }

// MarshalJSON writes the AgentDisk members in declaration order.
func (r AgentDisk) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type agentNetworkWire AgentNetwork

func (*AgentNetwork) plainRecord() {}

// UnmarshalJSON decodes stored or captured AgentNetwork data leniently (DecodeStored).
func (r *AgentNetwork) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*agentNetworkWire)(r))
}

func (r *AgentNetwork) plainWire() any { return (*agentNetworkWire)(r) }

// MarshalJSON writes the AgentNetwork members in declaration order.
func (r AgentNetwork) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type legacyDNSEndpointWire LegacyDNSEndpoint

func (*LegacyDNSEndpoint) plainRecord() {}

// UnmarshalJSON decodes stored or captured LegacyDNSEndpoint data leniently (DecodeStored).
func (r *LegacyDNSEndpoint) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*legacyDNSEndpointWire)(r))
}

func (r *LegacyDNSEndpoint) plainWire() any { return (*legacyDNSEndpointWire)(r) }

// MarshalJSON writes the LegacyDNSEndpoint members in declaration order.
func (r LegacyDNSEndpoint) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

type agentLoggingConfigWire AgentLoggingConfig

func (*AgentLoggingConfig) plainRecord() {}

// UnmarshalJSON decodes stored or captured AgentLoggingConfig data leniently (DecodeStored).
func (r *AgentLoggingConfig) UnmarshalJSON(data []byte) error {
	return decodePlain(data, (*agentLoggingConfigWire)(r))
}

func (r *AgentLoggingConfig) plainWire() any { return (*agentLoggingConfigWire)(r) }

// MarshalJSON writes the AgentLoggingConfig members in declaration order.
func (r AgentLoggingConfig) MarshalJSON() ([]byte, error) { return marshalPlain(&r) }

// IsEnabled reports the Agent's enabled switch; a missing switch is on.
func (r *Agent) IsEnabled() bool { return r.Enabled == nil || *r.Enabled }

// DropHistory clears the deployment and IPv6 verification histories, the two
// members reads without history leave out.
func (r *Agent) DropHistory() {
	r.Deployments = OrderedMap[Deployment]{}
	r.IPv6Verifications = OrderedMap[IPv6Verification]{}
}
