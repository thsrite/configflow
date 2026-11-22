package model

// SystemConfig holds global settings, secrets and retention choices; nil fields leave consumer defaults intact.
type SystemConfig struct {
	ServerDomain                   string             `json:"server_domain" text:"strict"`
	GitHubProxyDomain              string             `json:"github_proxy_domain"`
	RuleFetchProxy                 string             `json:"rule_fetch_proxy"`
	ConfigToken                    string             `json:"config_token,omitzero" stored:"secret"`
	RuleProxyToken                 string             `json:"rule_proxy_token,omitzero" stored:"secret"`
	RetiredRuleProxyTokens         []string           `json:"retired_rule_proxy_tokens,omitzero" stored:"secret"`
	DefaultProfileID               *string            `json:"default_profile_id,omitzero"`
	SubscriptionAggregationEnabled *bool              `json:"subscription_aggregation_enabled,omitzero"`
	SubStoreMode                   *string            `json:"sub_store_mode,omitzero"`
	SubStoreURL                    *string            `json:"sub_store_url,omitzero"`
	NodeTestDNS                    []string           `json:"node_test_dns,omitzero"`
	NodeTestDNSConfig              *NodeTestDNSConfig `json:"node_test_dns_config,omitzero"`
	MetricsHistoryFlushInterval    *Int               `json:"metrics_history_flush_interval,omitzero"`
	// TrafficHistoryRetentionDays decides how much traffic history is deleted:
	// stored as anything but an integer literal it reads as absent (the
	// default), never as a smaller count.
	TrafficHistoryRetentionDays *Int  `json:"traffic_history_retention_days,omitzero" int:"literal"`
	SetupSuggestionDismissed    *bool `json:"setup_suggestion_dismissed,omitzero"`
}

// NodeTestDNSConfig stores the current probe DNS settings and preserves the removed managed mode for startup cleanup.
type NodeTestDNSConfig struct {
	Mode      string             `json:"mode"`
	Servers   []string           `json:"servers"`
	Bootstrap []string           `json:"bootstrap"`
	Managed   *ManagedDNSBinding `json:"managed,omitzero"`
}

// ManagedDNSBinding identifies a legacy managed DNS endpoint; it does not carry Agent state.
type ManagedDNSBinding struct {
	AgentID    string `json:"agent_id,omitzero"`
	EndpointID string `json:"endpoint_id,omitzero"`
}

// Backup holds WebDAV credentials and optional retention settings, without
// applying response redaction. A stored URL that is not text reads as empty
// (text:"strict"); retention stored as anything but an integer literal reads
// as absent (int:"literal"), so the default applies as in Python.
type Backup struct {
	WebDAVURL      string  `json:"webdav_url,omitzero" text:"strict"`
	WebDAVUsername string  `json:"webdav_username,omitzero"`
	WebDAVPassword string  `json:"webdav_password,omitzero"`
	WebDAVPath     *string `json:"webdav_path,omitzero"`
	AutoBackup     *bool   `json:"auto_backup,omitzero" stored:"gate"`
	KeepLatest     *Int    `json:"keep_latest,omitzero" int:"literal"`
	KeepDaily      *Int    `json:"keep_daily,omitzero" int:"literal"`
}
