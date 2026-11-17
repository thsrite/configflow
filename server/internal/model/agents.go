package model

// Agent is a stored registry entry; liveness overlays and public projections are not synthesized here.
type Agent struct {
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
