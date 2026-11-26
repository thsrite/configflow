package model

// RouterRuleSyncReceipt is an external runtime receipt, never part of configuration revisions or exports.
type RouterRuleSyncReceipt struct {
	Status    string                     `json:"status"`
	Message   string                     `json:"message"`
	CheckedAt *string                    `json:"checked_at,omitzero"`
	SyncedAt  *string                    `json:"synced_at,omitzero"`
	UpdatedAt *string                    `json:"updated_at,omitzero"`
	Added     *Int                       `json:"added,omitzero"`
	Removed   *Int                       `json:"removed,omitzero"`
	Log       []RouterSyncLog            `json:"log"`
	Rules     OrderedMap[RouterSyncRule] `json:"rules"`
	Baseline  *RouterRuleSyncBaseline    `json:"baseline,omitzero"`
}

// RouterSyncLog is one retained automatic routing update log item.
type RouterSyncLog struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// RouterSyncRule retains source versions and strict classification results used by automatic routing.
type RouterSyncRule struct {
	Name         string   `json:"name"`
	SourceType   string   `json:"source_type"`
	LibraryID    string   `json:"library_id"`
	Version      string   `json:"version"`
	IPv4         []string `json:"ipv4"`
	IPv6         []string `json:"ipv6"`
	IPCount      Int      `json:"ip_count"`
	IP6Count     Int      `json:"ip6_count"`
	DomainCount  Int      `json:"domain_count"`
	IgnoredCount Int      `json:"ignored_count"`
	InvalidCount Int      `json:"invalid_count"`
}

// RouterRuleSyncBaseline is the verified manual enrollment baseline; no ownership is inferred during decoding.
type RouterRuleSyncBaseline struct {
	Control         string                     `json:"control"`
	Rules           OrderedMap[RouterSyncRule] `json:"rules"`
	Desired         RouterSyncDesired          `json:"desired"`
	StaticResources []RouterResource           `json:"static_resources"`
	Policy          *ROSPolicyState            `json:"policy,omitzero"`
	NATList         *RouterSyncNAT             `json:"nat_list,omitzero"`
}

// RouterSyncDesired holds the frozen desired networks captured by a routing baseline.
type RouterSyncDesired struct {
	FakeIPRange      string       `json:"fake_ip_range"`
	ProxyGateway     string       `json:"proxy_gateway"`
	ProxyCIDRs       []RouterCIDR `json:"proxy_cidrs"`
	IPv6CIDRs        []RouterCIDR `json:"ipv6_cidrs"`
	Warnings         []string     `json:"warnings"`
	BlockingWarnings []string     `json:"blocking_warnings"`
}

// RouterCIDR retains a network and ordered source labels without network normalization.
type RouterCIDR struct {
	CIDR    string   `json:"cidr"`
	Sources []string `json:"sources"`
}

// RouterSyncNAT holds the enrolled NAT rule and its owned source-list members.
type RouterSyncNAT struct {
	Rule RouterResource   `json:"rule"`
	Src  []RouterResource `json:"src"`
}
