package model

// MihomoSettings stores the editable YAML text and profile-level provider/EasyTier settings; text is parsed only by consumers.
type MihomoSettings struct {
	CustomConfig           string            `json:"custom_config"`
	ProviderUpdateInterval *Int              `json:"provider_update_interval"`
	EasyTier               *EasyTierSettings `json:"easytier"`
	Fields                 Fields
	raw                    Raw
}

// UnmarshalJSON decodes MihomoSettings with stored-member preservation and R5 fallback.
func (r *MihomoSettings) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged MihomoSettings members.
func (r MihomoSettings) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// SurgeSettings stores the editable client text and explicit smart-group choices.
type SurgeSettings struct {
	CustomConfig string            `json:"custom_config"`
	SmartGroups  []SurgeSmartGroup `json:"smart_groups"`
	Fields       Fields
	raw          Raw
}

// UnmarshalJSON decodes SurgeSettings with stored-member preservation and R5 fallback.
func (r *SurgeSettings) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged SurgeSettings members.
func (r SurgeSettings) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// SurgeSmartGroup selects a policy group and its client priority expression.
type SurgeSmartGroup struct {
	GroupID        string `json:"group_id"`
	PolicyPriority string `json:"policy_priority"`
	Fields         Fields
	raw            Raw
}

// UnmarshalJSON decodes SurgeSmartGroup with stored-member preservation and R5 fallback.
func (r *SurgeSmartGroup) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged SurgeSmartGroup members.
func (r SurgeSmartGroup) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// LoonSettings stores the editable Loon client text.
type LoonSettings struct {
	CustomConfig string `json:"custom_config"`
	Fields       Fields
	raw          Raw
}

// UnmarshalJSON decodes LoonSettings with stored-member preservation and R5 fallback.
func (r *LoonSettings) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged LoonSettings members.
func (r LoonSettings) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// EasyTierSettings holds the profile overlay settings without applying defaults during decoding.
type EasyTierSettings struct {
	Enabled   *bool              `json:"enabled"`
	Name      *string            `json:"name"`
	ApplyTo   *string            `json:"apply_to"`
	Routes    []string           `json:"routes"`
	MagicDNS  *bool              `json:"magic_dns"`
	Keepalive *EasyTierKeepalive `json:"keepalive"`
	Options   EasyTierOptions    `json:"options"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes EasyTierSettings with stored-member preservation and R5 fallback.
func (r *EasyTierSettings) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged EasyTierSettings members.
func (r EasyTierSettings) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// EasyTierKeepalive holds the optional overlay health request.
type EasyTierKeepalive struct {
	Enabled  *bool  `json:"enabled"`
	URL      string `json:"url"`
	Interval *Int   `json:"interval"`
	Fields   Fields
	raw      Raw
}

// UnmarshalJSON decodes EasyTierKeepalive with stored-member preservation and R5 fallback.
func (r *EasyTierKeepalive) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged EasyTierKeepalive members.
func (r EasyTierKeepalive) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// EasyTierOptions declares every option accepted by Python mihomo_easytier.FIELDS; future keys remain in Raw.
type EasyTierOptions struct {
	NetworkName         string   `json:"network-name"`
	NetworkSecret       string   `json:"network-secret"`
	Hostname            string   `json:"hostname"`
	IPv4                string   `json:"ipv4"`
	DHCP                *bool    `json:"dhcp"`
	Peers               []string `json:"peers"`
	SecureMode          *bool    `json:"secure-mode"`
	LocalPrivateKey     string   `json:"local-private-key"`
	LocalPublicKey      string   `json:"local-public-key"`
	Listeners           []string `json:"listeners"`
	NoListener          *bool    `json:"no-listener"`
	MappedListeners     []string `json:"mapped-listeners"`
	ExitNodes           []string `json:"exit-nodes"`
	ProxyNetworks       []string `json:"proxy-networks"`
	InstanceName        string   `json:"instance-name"`
	StateDir            string   `json:"state-dir"`
	UDP                 *bool    `json:"udp"`
	AcceptDNS           *bool    `json:"accept-dns"`
	EnableExitNode      *bool    `json:"enable-exit-node"`
	EnableEncryption    *bool    `json:"enable-encryption"`
	EncryptionAlgorithm string   `json:"encryption-algorithm"`
	PrivateMode         *bool    `json:"private-mode"`
	LatencyFirst        *bool    `json:"latency-first"`
	DisableP2P          *bool    `json:"disable-p2p"`
	EnableKCPProxy      *bool    `json:"enable-kcp-proxy"`
	DisableKCPInput     *bool    `json:"disable-kcp-input"`
	EnableQUICProxy     *bool    `json:"enable-quic-proxy"`
	DisableQUICInput    *bool    `json:"disable-quic-input"`
	MTU                 *Int     `json:"mtu"`
	TLDDNSZone          string   `json:"tld-dns-zone"`
	DialerProxy         string   `json:"dialer-proxy"`
	InterfaceName       string   `json:"interface-name"`
	RoutingMark         *Int     `json:"routing-mark"`
	IPVersion           *string  `json:"ip-version"`
	Fields              Fields
	raw                 Raw
}

// UnmarshalJSON decodes EasyTierOptions with stored-member preservation and R5 fallback.
func (r *EasyTierOptions) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged EasyTierOptions members.
func (r EasyTierOptions) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// MosDNSSettings stores editor settings and raw YAML text without parsing or normalizing a draft.
type MosDNSSettings struct {
	CustomConfig        string        `json:"custom_config"`
	DirectRulesets      []string      `json:"direct_rulesets"`
	ProxyRulesets       []string      `json:"proxy_rulesets"`
	DirectRules         []string      `json:"direct_rules"`
	ProxyRules          []string      `json:"proxy_rules"`
	CustomMatches       []MosDNSMatch `json:"custom_matches"`
	CustomMatchPosition *string       `json:"custom_match_position"`
	LocalDNS            string        `json:"local_dns"`
	RemoteDNS           string        `json:"remote_dns"`
	FallbackDNS         string        `json:"fallback_dns"`
	DefaultForward      *string       `json:"default_forward"`
	CustomHosts         string        `json:"custom_hosts"`
	LogEnabled          *bool         `json:"log_enabled"`
	LogLevel            *string       `json:"log_level"`
	LogFile             *string       `json:"log_file"`
	APIEnabled          *bool         `json:"api_enabled"`
	APIAddress          *string       `json:"api_address"`
	APIAddr             *string       `json:"api_addr"`
	ListenAddress       *string       `json:"listen_address"`
	CacheEnabled        *bool         `json:"cache_enabled"`
	CacheSize           *Int          `json:"cache_size"`
	CacheLazyTTL        *Int          `json:"cache_lazy_ttl"`
	CacheDumpEnabled    *bool         `json:"cache_dump_enabled"`
	CacheDumpFile       *string       `json:"cache_dump_file"`
	CacheDumpInterval   *Int          `json:"cache_dump_interval"`
	Fields              Fields
	raw                 Raw
}

// UnmarshalJSON decodes MosDNSSettings with stored-member preservation and R5 fallback.
func (r *MosDNSSettings) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged MosDNSSettings members.
func (r MosDNSSettings) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// MosDNSMatch is one editable sequence entry; matches retains its string or list representation.
type MosDNSMatch struct {
	ID      string    `json:"id"`
	Enabled *bool     `json:"enabled"`
	Matches TextLines `json:"matches"`
	Exec    string    `json:"exec"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes MosDNSMatch with stored-member preservation and R5 fallback.
func (r *MosDNSMatch) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged MosDNSMatch members.
func (r MosDNSMatch) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DomainDiscovery holds profile discovery choices, user-keyed notes and applied-rule history.
type DomainDiscovery struct {
	DirectRuleset          *string            `json:"direct_ruleset"`
	ProxyRuleset           *string            `json:"proxy_ruleset"`
	Ignored                []string           `json:"ignored"`
	AutoProbe              *bool              `json:"auto_probe"`
	AutoApply              *bool              `json:"auto_apply"`
	AutoApplyMinConfidence *Int               `json:"auto_apply_min_confidence"`
	History                []DiscoveryHistory `json:"history"`
	RegionCheckEnabled     *bool              `json:"region_check_enabled"`
	RegionProbePort        *Int               `json:"region_probe_port"`
	RegionalRulesets       Map[string]        `json:"regional_rulesets"`
	WebsiteNotes           Map[string]        `json:"website_notes"`
	Fields                 Fields
	raw                    Raw
}

// UnmarshalJSON decodes DomainDiscovery with stored-member preservation and R5 fallback.
func (r *DomainDiscovery) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged DomainDiscovery members.
func (r DomainDiscovery) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DiscoveryHistory records one applied discovery result and its optional recheck.
type DiscoveryHistory struct {
	Value      string            `json:"value"`
	RuleType   string            `json:"rule_type"`
	Target     string            `json:"target"`
	RulesetID  string            `json:"ruleset_id"`
	Source     string            `json:"source"`
	AppliedAt  string            `json:"applied_at"`
	Verdict    *string           `json:"verdict"`
	Confidence *Int              `json:"confidence"`
	Recheck    *DiscoveryRecheck `json:"recheck"`
	Fields     Fields
	raw        Raw
}

// UnmarshalJSON decodes DiscoveryHistory with stored-member preservation and R5 fallback.
func (r *DiscoveryHistory) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged DiscoveryHistory members.
func (r DiscoveryHistory) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DiscoveryRecheck records a periodic check of an applied discovery rule.
type DiscoveryRecheck struct {
	At            string  `json:"at"`
	Verdict       *string `json:"verdict"`
	SuggestRemove *bool   `json:"suggest_remove"`
	Reason        string  `json:"reason"`
	Fields        Fields
	raw           Raw
}

// UnmarshalJSON decodes DiscoveryRecheck with stored-member preservation and R5 fallback.
func (r *DiscoveryRecheck) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged DiscoveryRecheck members.
func (r DiscoveryRecheck) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// ClientFixedIP is a tracked DHCP reservation keyed by client IP, with ownership text kept unchanged.
type ClientFixedIP struct {
	RouterID  string  `json:"router_id"`
	Vendor    string  `json:"vendor"`
	ID        string  `json:"id"`
	IP        string  `json:"ip"`
	MAC       string  `json:"mac"`
	Comment   string  `json:"comment"`
	Server    *string `json:"server"`
	Interface *string `json:"interface"`
	Gateway   *string `json:"gateway"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes ClientFixedIP with stored-member preservation and R5 fallback.
func (r *ClientFixedIP) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged ClientFixedIP members.
func (r ClientFixedIP) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// DeviceNotes maps client address/range text to display names in stored order.
type DeviceNotes = Map[string]

// ClientFixedIPs maps client IPv4 text to tracked reservations in stored order.
type ClientFixedIPs = Map[ClientFixedIP]
