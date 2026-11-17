package model

// RouterRuleSyncReceipt is an external runtime receipt, never part of configuration revisions or exports.
type RouterRuleSyncReceipt struct {
	Status    string                  `json:"status"`
	Message   string                  `json:"message"`
	CheckedAt *string                 `json:"checked_at"`
	SyncedAt  *string                 `json:"synced_at"`
	UpdatedAt *string                 `json:"updated_at"`
	Added     *Int                    `json:"added"`
	Removed   *Int                    `json:"removed"`
	Log       []RouterSyncLog         `json:"log"`
	Rules     Map[RouterSyncRule]     `json:"rules"`
	Baseline  *RouterRuleSyncBaseline `json:"baseline"`
	Fields    Fields
	raw       Raw
}

// UnmarshalJSON decodes RouterRuleSyncReceipt with stored-member preservation and R5 fallback.
func (r *RouterRuleSyncReceipt) UnmarshalJSON(data []byte) error {
	return DecodeRecord(data, r, &r.raw)
}

// MarshalJSON preserves unknown and unchanged RouterRuleSyncReceipt members.
func (r RouterRuleSyncReceipt) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RouterSyncLog is one retained automatic routing update log item.
type RouterSyncLog struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes RouterSyncLog with stored-member preservation and R5 fallback.
func (r *RouterSyncLog) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged RouterSyncLog members.
func (r RouterSyncLog) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

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
	Fields       Fields
	raw          Raw
}

// UnmarshalJSON decodes RouterSyncRule with stored-member preservation and R5 fallback.
func (r *RouterSyncRule) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged RouterSyncRule members.
func (r RouterSyncRule) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RouterRuleSyncBaseline is the verified manual enrollment baseline; no ownership is inferred during decoding.
type RouterRuleSyncBaseline struct {
	Control         string              `json:"control"`
	Rules           Map[RouterSyncRule] `json:"rules"`
	Desired         RouterSyncDesired   `json:"desired"`
	StaticResources []RouterResource    `json:"static_resources"`
	Policy          *ROSPolicyState     `json:"policy"`
	NATList         *RouterSyncNAT      `json:"nat_list"`
	Fields          Fields
	raw             Raw
}

// UnmarshalJSON decodes RouterRuleSyncBaseline with stored-member preservation and R5 fallback.
func (r *RouterRuleSyncBaseline) UnmarshalJSON(data []byte) error {
	return DecodeRecord(data, r, &r.raw)
}

// MarshalJSON preserves unknown and unchanged RouterRuleSyncBaseline members.
func (r RouterRuleSyncBaseline) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RouterSyncDesired holds the frozen desired networks captured by a routing baseline.
type RouterSyncDesired struct {
	FakeIPRange      string       `json:"fake_ip_range"`
	ProxyGateway     string       `json:"proxy_gateway"`
	ProxyCIDRs       []RouterCIDR `json:"proxy_cidrs"`
	IPv6CIDRs        []RouterCIDR `json:"ipv6_cidrs"`
	Warnings         []string     `json:"warnings"`
	BlockingWarnings []string     `json:"blocking_warnings"`
	Fields           Fields
	raw              Raw
}

// UnmarshalJSON decodes RouterSyncDesired with stored-member preservation and R5 fallback.
func (r *RouterSyncDesired) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged RouterSyncDesired members.
func (r RouterSyncDesired) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RouterCIDR retains a network and ordered source labels without network normalization.
type RouterCIDR struct {
	CIDR    string   `json:"cidr"`
	Sources []string `json:"sources"`
	Fields  Fields
	raw     Raw
}

// UnmarshalJSON decodes RouterCIDR with stored-member preservation and R5 fallback.
func (r *RouterCIDR) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged RouterCIDR members.
func (r RouterCIDR) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// RouterSyncNAT holds the enrolled NAT rule and its owned source-list members.
type RouterSyncNAT struct {
	Rule   RouterResource   `json:"rule"`
	Src    []RouterResource `json:"src"`
	Fields Fields
	raw    Raw
}

// UnmarshalJSON decodes RouterSyncNAT with stored-member preservation and R5 fallback.
func (r *RouterSyncNAT) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// MarshalJSON preserves unknown and unchanged RouterSyncNAT members.
func (r RouterSyncNAT) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }
