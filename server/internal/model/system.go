package model

// SystemConfig holds global settings, secrets and retention choices; nil fields leave consumer defaults intact.
type SystemConfig struct {
	ServerDomain                   string             `json:"server_domain"`
	GitHubProxyDomain              string             `json:"github_proxy_domain"`
	RuleFetchProxy                 string             `json:"rule_fetch_proxy"`
	ConfigToken                    string             `json:"config_token"`
	RuleProxyToken                 string             `json:"rule_proxy_token"`
	RetiredRuleProxyTokens         []string           `json:"retired_rule_proxy_tokens"`
	DefaultProfileID               *string            `json:"default_profile_id"`
	SubscriptionAggregationEnabled *bool              `json:"subscription_aggregation_enabled"`
	SubStoreMode                   *string            `json:"sub_store_mode"`
	SubStoreURL                    *string            `json:"sub_store_url"`
	NodeTestDNS                    []string           `json:"node_test_dns"`
	NodeTestDNSConfig              *NodeTestDNSConfig `json:"node_test_dns_config"`
	MetricsHistoryFlushInterval    *Int               `json:"metrics_history_flush_interval"`
	TrafficHistoryRetentionDays    *Int               `json:"traffic_history_retention_days"`
	SetupSuggestionDismissed       *bool              `json:"setup_suggestion_dismissed"`
	Fields                         Fields
	raw                            Raw
}

// UnmarshalJSON decodes SystemConfig with stored-member preservation and R5 fallback.
func (r *SystemConfig) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when SystemConfig is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *SystemConfig) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged SystemConfig members.
func (r SystemConfig) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// NodeTestDNSConfig stores the current probe DNS settings and preserves the removed managed mode for startup cleanup.
type NodeTestDNSConfig struct {
	Mode      string             `json:"mode"`
	Servers   []string           `json:"servers"`
	Bootstrap []string           `json:"bootstrap"`
	Managed   *ManagedDNSBinding `json:"managed"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes NodeTestDNSConfig with stored-member preservation and R5 fallback.
func (r *NodeTestDNSConfig) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when NodeTestDNSConfig is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *NodeTestDNSConfig) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged NodeTestDNSConfig members.
func (r NodeTestDNSConfig) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// ManagedDNSBinding identifies a legacy managed DNS endpoint; it does not carry Agent state.
type ManagedDNSBinding struct {
	AgentID    string `json:"agent_id"`
	EndpointID string `json:"endpoint_id"`
	Fields     Fields
	raw        Raw
}

// UnmarshalJSON decodes ManagedDNSBinding with stored-member preservation and R5 fallback.
func (r *ManagedDNSBinding) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when ManagedDNSBinding is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *ManagedDNSBinding) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged ManagedDNSBinding members.
func (r ManagedDNSBinding) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// Backup holds WebDAV credentials and optional retention settings, without applying response redaction.
type Backup struct {
	WebDAVURL      string  `json:"webdav_url"`
	WebDAVUsername string  `json:"webdav_username"`
	WebDAVPassword string  `json:"webdav_password"`
	WebDAVPath     *string `json:"webdav_path"`
	AutoBackup     *bool   `json:"auto_backup"`
	KeepLatest     *Int    `json:"keep_latest"`
	KeepDaily      *Int    `json:"keep_daily"`
	Fields         Fields
	raw            Raw
}

// UnmarshalJSON decodes Backup with stored-member preservation and R5 fallback.
func (r *Backup) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when Backup is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *Backup) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged Backup members.
func (r Backup) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }
