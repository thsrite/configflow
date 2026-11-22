package model

// ProxyChain connects two stable resource references without recursive stored objects.
type ProxyChain struct {
	Entry ResourceRef `json:"entry"`
	Exit  ResourceRef `json:"exit"`
}

// ProxyGroup defines profile-local policy candidates, ordering and optional engine health settings.
type ProxyGroup struct {
	ID                        string         `json:"id,omitzero"`
	Name                      string         `json:"name"`
	Type                      string         `json:"type,omitzero"`
	Enabled                   *bool          `json:"enabled,omitzero"`
	Subscriptions             *[]string      `json:"subscriptions,omitzero" input:"strict"`
	ManualNodes               *[]string      `json:"manual_nodes,omitzero" input:"strict"`
	Aggregations              *[]string      `json:"aggregations,omitzero" input:"strict"`
	IncludeGroups             *[]string      `json:"include_groups,omitzero" input:"strict"`
	FollowGroup               *string        `json:"follow_group,omitzero"`
	ProxiesOrder              *[]ResourceRef `json:"proxies_order,omitzero" input:"strict" elements:"all"`
	ProxyOrder                *string        `json:"proxy_order,omitzero"`
	Regex                     *string        `json:"regex,omitzero"`
	ExcludeRegex              *string        `json:"exclude_regex,omitzero"`
	AggregationRegex          *string        `json:"aggregation_regex,omitzero"`
	AggregationExcludeRegex   *string        `json:"aggregation_exclude_regex,omitzero"`
	URL                       *string        `json:"url,omitzero" input:"strict"`
	Interval                  *Int           `json:"interval,omitzero" input:"strict"`
	Timeout                   *Int           `json:"timeout,omitzero" input:"strict"`
	Tolerance                 *Int           `json:"tolerance,omitzero" input:"strict"`
	Lazy                      *bool          `json:"lazy,omitzero" input:"strict"`
	ExpectedStatus            *string        `json:"expected-status,omitzero" input:"strict"`
	Strategy                  *string        `json:"strategy,omitzero"`
	Chain                     *ProxyChain    `json:"chain,omitzero"`
	Source                    *string        `json:"source,omitzero"`
	SourceType                *string        `json:"source_type,omitzero"`
	Proxies                   *[]string      `json:"proxies,omitzero" input:"strict"`
	Use                       *[]string      `json:"use,omitzero" input:"strict"`
	IncludeAll                *bool          `json:"include_all,omitzero"`
	IncludeAllNative          *bool          `json:"include-all,omitzero"`
	IncludeAllProviders       *bool          `json:"include_all_providers,omitzero"`
	IncludeAllProvidersNative *bool          `json:"include-all-providers,omitzero"`
	IncludeAllProxies         *bool          `json:"include_all_proxies,omitzero"`
	IncludeAllProxiesNative   *bool          `json:"include-all-proxies,omitzero"`
	MaxFailedTimes            *Int           `json:"max-failed-times,omitzero"`
	DisableUDP                *bool          `json:"disable-udp,omitzero"`
	Hidden                    *bool          `json:"hidden,omitzero"`
	Icon                      *string        `json:"icon,omitzero"`
	ExcludeFilter             *string        `json:"exclude-filter,omitzero"`
	ExcludeType               *string        `json:"exclude-type,omitzero"`
	// ChainDialer and ChainOriginalCandidates are set by proxy-chain lowering
	// on a resolved configuration (generator runtime only; editors never
	// write them): the dialer of a cloned group and its original candidates.
	ChainDialer             *ResourceRef `json:"_chain_dialer,omitzero"`
	ChainOriginalCandidates Dynamic      `json:"_chain_original_candidates,omitzero"`
}

// RuleConfig is one profile rule or shared-library reference, including stored editor grouping metadata and legacy source fields.
type RuleConfig struct {
	ID                  string  `json:"id,omitzero"`
	ItemType            string  `json:"itemType,omitzero"`
	Enabled             *bool   `json:"enabled,omitzero"`
	LibraryRuleID       string  `json:"library_rule_id,omitzero"`
	RuleType            string  `json:"rule_type,omitzero"`
	Value               string  `json:"value,omitzero"`
	Policy              *string `json:"policy,omitzero" null:"empty" text:"strict"`
	NoResolve           *bool   `json:"no_resolve,omitzero"`
	UDPFallback         *string `json:"udp_fallback,omitzero"`
	NTPDirect           *bool   `json:"ntp_direct,omitzero"`
	Remark              string  `json:"remark,omitzero"`
	GroupName           string  `json:"group_name,omitzero"`
	GroupID             string  `json:"groupId,omitzero"`
	GroupLabel          string  `json:"groupLabel,omitzero"`
	GroupPolicy         string  `json:"groupPolicy,omitzero"`
	IsExpandedGroupItem *bool   `json:"isExpandedGroupItem,omitzero"`
	IsFirstInGroup      *bool   `json:"isFirstInGroup,omitzero"`
	IsLastInGroup       *bool   `json:"isLastInGroup,omitzero"`
	UniqueID            string  `json:"uniqueId,omitzero"`
	Name                string  `json:"name,omitzero"`
	URL                 *string `json:"url,omitzero"`
	Behavior            *string `json:"behavior,omitzero" null:"empty"` // null is not the default (see RuleLibraryItem)
	Content             *string `json:"content,omitzero"`
	SourceType          *string `json:"source_type,omitzero"`
	Format              *string `json:"format,omitzero"`
	LibraryEnabled      *bool   `json:"library_enabled,omitzero"`
}
