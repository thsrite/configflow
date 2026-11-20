package model

import "encoding/json"

// Agent is a stored registry entry; liveness overlays and public projections are not synthesized here.
type Agent struct {
	LoggingConfig            *AgentLoggingConfig       `json:"logging_config"`
	ID                       string                    `json:"id"`
	Name                     string                    `json:"name"`
	Host                     string                    `json:"host"`
	Port                     Int                       `json:"port"`
	Token                    string                    `json:"token"`
	ServiceType              string                    `json:"service_type"`
	ProfileID                string                    `json:"profile_id"`
	DeploymentMethod         string                    `json:"deployment_method"`
	Enabled                  *bool                     `json:"enabled"`
	CreatedAt                string                    `json:"created_at"`
	UpdatedAt                *string                   `json:"updated_at"`
	Status                   string                    `json:"status"`
	LastHeartbeat            string                    `json:"last_heartbeat"`
	Version                  string                    `json:"version"`
	ConfigVersion            string                    `json:"config_version"`
	ConfigRevision           *Int                      `json:"config_revision"`
	PushedAt                 *string                   `json:"pushed_at"`
	GatewayInstallationID    *string                   `json:"gateway_installation_id"`
	DockerHAInstallationID   *string                   `json:"docker_ha_installation_id"`
	DomainDiscoveryEnabled   *bool                     `json:"domain_discovery_enabled"`
	TrafficHistoryEnabled    *bool                     `json:"traffic_history_enabled"`
	TrafficHistorySince      *string                   `json:"traffic_history_since"`
	Deployments              Map[Deployment]           `json:"deployments"`
	LatestDeployment         *Deployment               `json:"latest_deployment"`
	AppliedContent           *AppliedContent           `json:"applied_content"`
	LatestUpgrade            *AgentUpgrade             `json:"latest_upgrade"`
	RuleSync                 *AgentRuleSync            `json:"rule_sync"`
	DNSFollow                *DNSFollowBinding         `json:"dns_follow"`
	DNSFollowState           *DNSFollowState           `json:"dns_follow_state"`
	DNSFollowJob             *DNSFollowJob             `json:"dns_follow_job"`
	OxiDNSHA                 *HASettings               `json:"oxidns_ha"`
	OxiDNSHAState            *HAState                  `json:"oxidns_ha_state"`
	OxiDNSHAPublication      *HAJournal                `json:"oxidns_ha_publication"`
	IPv6VerificationSettings *IPv6VerificationSettings `json:"ipv6_verification_settings"`
	IPv6Verifications        Map[IPv6Verification]     `json:"ipv6_verifications"`
	NodeTestDNSEndpoints     Map[LegacyDNSEndpoint]    `json:"node_test_dns_endpoints"`
	ServiceStatus            *string                   `json:"service_status"`
	SystemMetrics            *AgentMetrics             `json:"system_metrics"`
	CPU                      *AgentCPU                 `json:"cpu"`
	Memory                   *AgentMemory              `json:"memory"`
	Disk                     *AgentDisk                `json:"disk"`
	Network                  *AgentNetwork             `json:"network"`
	Fields                   Fields
	raw                      Raw
}

// UnmarshalJSON decodes Agent with stored-member preservation and R5 fallback.
func (r *Agent) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when Agent is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *Agent) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged Agent members.
func (r Agent) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// AgentMetrics preserves older embedded metrics without introducing persistent heartbeat writes.
type AgentMetrics struct {
	CollectedAt string       `json:"collected_at"`
	CPU         AgentCPU     `json:"cpu"`
	Memory      AgentMemory  `json:"memory"`
	Disk        AgentDisk    `json:"disk"`
	Network     AgentNetwork `json:"network"`
	Fields      Fields
	raw         Raw
}

// UnmarshalJSON decodes AgentMetrics with stored-member preservation and R5 fallback.
func (r *AgentMetrics) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when AgentMetrics is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *AgentMetrics) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged AgentMetrics members.
func (r AgentMetrics) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// AgentCPU holds a legacy CPU sample.
type AgentCPU struct {
	CoreCount    Int     `json:"core_count"`
	UsagePercent float64 `json:"usage_percent"`
	Fields       Fields
	raw          Raw
}

// UnmarshalJSON decodes AgentCPU with stored-member preservation and R5 fallback.
func (r *AgentCPU) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when AgentCPU is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *AgentCPU) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged AgentCPU members.
func (r AgentCPU) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// AgentMemory holds a legacy memory sample.
type AgentMemory struct {
	Total       Int     `json:"total"`
	Used        Int     `json:"used"`
	Available   Int     `json:"available"`
	UsedPercent float64 `json:"used_percent"`
	Fields      Fields
	raw         Raw
}

// UnmarshalJSON decodes AgentMemory with stored-member preservation and R5 fallback.
func (r *AgentMemory) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when AgentMemory is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *AgentMemory) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged AgentMemory members.
func (r AgentMemory) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// AgentDisk holds a legacy disk sample.
type AgentDisk struct {
	Total       Int     `json:"total"`
	Used        Int     `json:"used"`
	Free        Int     `json:"free"`
	UsedPercent float64 `json:"used_percent"`
	Fields      Fields
	raw         Raw
}

// UnmarshalJSON decodes AgentDisk with stored-member preservation and R5 fallback.
func (r *AgentDisk) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when AgentDisk is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *AgentDisk) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged AgentDisk members.
func (r AgentDisk) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// AgentNetwork holds legacy byte counters and integral bytes-per-second rates.
type AgentNetwork struct {
	BytesSent Int `json:"bytes_sent"`
	BytesRecv Int `json:"bytes_recv"`
	SpeedSent Int `json:"speed_sent"`
	SpeedRecv Int `json:"speed_recv"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes AgentNetwork with stored-member preservation and R5 fallback.
func (r *AgentNetwork) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when AgentNetwork is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *AgentNetwork) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged AgentNetwork members.
func (r AgentNetwork) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// LegacyDNSEndpoint retains the removed managed-node-test listener proof until its consumer explicitly removes it.
type LegacyDNSEndpoint struct {
	Endpoint  string    `json:"endpoint"`
	ProfileID string    `json:"profile_id"`
	Status    string    `json:"status"`
	Installed *bool     `json:"installed"`
	Plugins   []Dynamic `json:"plugins"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes LegacyDNSEndpoint with stored-member preservation and R5 fallback.
func (r *LegacyDNSEndpoint) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when LegacyDNSEndpoint is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *LegacyDNSEndpoint) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged LegacyDNSEndpoint members.
func (r LegacyDNSEndpoint) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// SetJSON replaces one Agent member through the shared model codec. It retains
// explicit null/zero assignments and clears the previous member's R5 diagnostic.
func (r *Agent) SetJSON(member string, data []byte) error {
	return SetMemberJSON(r, &r.raw, member, data)
}

// MemberJSON returns an owned effective member, including original null/type
// evidence, without encoding the rest of this Agent or its history.
func (r *Agent) MemberJSON(member string) (json.RawMessage, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	switch member {
	case "logging_config":
		return r.raw.MemberJSON(member, r.LoggingConfig)
	case "id":
		return r.raw.MemberJSON(member, r.ID)
	case "name":
		return r.raw.MemberJSON(member, r.Name)
	case "host":
		return r.raw.MemberJSON(member, r.Host)
	case "port":
		return r.raw.MemberJSON(member, r.Port)
	case "token":
		return r.raw.MemberJSON(member, r.Token)
	case "service_type":
		return r.raw.MemberJSON(member, r.ServiceType)
	case "profile_id":
		return r.raw.MemberJSON(member, r.ProfileID)
	case "deployment_method":
		return r.raw.MemberJSON(member, r.DeploymentMethod)
	case "enabled":
		return r.raw.MemberJSON(member, r.Enabled)
	case "created_at":
		return r.raw.MemberJSON(member, r.CreatedAt)
	case "updated_at":
		return r.raw.MemberJSON(member, r.UpdatedAt)
	case "status":
		return r.raw.MemberJSON(member, r.Status)
	case "last_heartbeat":
		return r.raw.MemberJSON(member, r.LastHeartbeat)
	case "version":
		return r.raw.MemberJSON(member, r.Version)
	case "config_version":
		return r.raw.MemberJSON(member, r.ConfigVersion)
	case "config_revision":
		return r.raw.MemberJSON(member, r.ConfigRevision)
	case "pushed_at":
		return r.raw.MemberJSON(member, r.PushedAt)
	case "gateway_installation_id":
		return r.raw.MemberJSON(member, r.GatewayInstallationID)
	case "docker_ha_installation_id":
		return r.raw.MemberJSON(member, r.DockerHAInstallationID)
	case "domain_discovery_enabled":
		return r.raw.MemberJSON(member, r.DomainDiscoveryEnabled)
	case "traffic_history_enabled":
		return r.raw.MemberJSON(member, r.TrafficHistoryEnabled)
	case "traffic_history_since":
		return r.raw.MemberJSON(member, r.TrafficHistorySince)
	case "deployments":
		return r.raw.MemberJSON(member, r.Deployments)
	case "latest_deployment":
		return r.raw.MemberJSON(member, r.LatestDeployment)
	case "applied_content":
		return r.raw.MemberJSON(member, r.AppliedContent)
	case "latest_upgrade":
		return r.raw.MemberJSON(member, r.LatestUpgrade)
	case "rule_sync":
		return r.raw.MemberJSON(member, r.RuleSync)
	case "dns_follow":
		return r.raw.MemberJSON(member, r.DNSFollow)
	case "dns_follow_state":
		return r.raw.MemberJSON(member, r.DNSFollowState)
	case "dns_follow_job":
		return r.raw.MemberJSON(member, r.DNSFollowJob)
	case "oxidns_ha":
		return r.raw.MemberJSON(member, r.OxiDNSHA)
	case "oxidns_ha_state":
		return r.raw.MemberJSON(member, r.OxiDNSHAState)
	case "oxidns_ha_publication":
		return r.raw.MemberJSON(member, r.OxiDNSHAPublication)
	case "ipv6_verification_settings":
		return r.raw.MemberJSON(member, r.IPv6VerificationSettings)
	case "ipv6_verifications":
		return r.raw.MemberJSON(member, r.IPv6Verifications)
	case "node_test_dns_endpoints":
		return r.raw.MemberJSON(member, r.NodeTestDNSEndpoints)
	case "service_status":
		return r.raw.MemberJSON(member, r.ServiceStatus)
	case "system_metrics":
		return r.raw.MemberJSON(member, r.SystemMetrics)
	case "cpu":
		return r.raw.MemberJSON(member, r.CPU)
	case "memory":
		return r.raw.MemberJSON(member, r.Memory)
	case "disk":
		return r.raw.MemberJSON(member, r.Disk)
	case "network":
		return r.raw.MemberJSON(member, r.Network)
	}
	raw, ok := r.Fields.Get(member)
	return raw, ok, nil
}

// AgentLoggingConfig holds the Agent's opaque logging switch and future options.
type AgentLoggingConfig struct {
	Enabled json.RawMessage `json:"enabled"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON reads exact member names with stored provenance.
func (r *AgentLoggingConfig) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged logging options.
func (r AgentLoggingConfig) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }
