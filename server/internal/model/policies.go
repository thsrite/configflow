package model

import "encoding/json"

// ProxyChain connects two stable resource references without recursive stored objects.
type ProxyChain struct {
	Entry  ResourceRef `json:"entry"`
	Exit   ResourceRef `json:"exit"`
	Fields Fields
	raw    Raw
}

// UnmarshalJSON decodes ProxyChain with stored-member preservation and R5 fallback.
func (r *ProxyChain) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when ProxyChain is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *ProxyChain) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged ProxyChain members.
func (r ProxyChain) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// ProxyGroup defines profile-local policy candidates, ordering and optional engine health settings.
type ProxyGroup struct {
	ID                        string         `json:"id"`
	Name                      string         `json:"name"`
	Type                      string         `json:"type"`
	Enabled                   *bool          `json:"enabled"`
	Subscriptions             *[]string      `json:"subscriptions"`
	ManualNodes               *[]string      `json:"manual_nodes"`
	Aggregations              *[]string      `json:"aggregations"`
	IncludeGroups             *[]string      `json:"include_groups"`
	FollowGroup               *string        `json:"follow_group"`
	ProxiesOrder              *[]ResourceRef `json:"proxies_order"`
	ProxyOrder                *string        `json:"proxy_order"`
	Regex                     *string        `json:"regex"`
	ExcludeRegex              *string        `json:"exclude_regex"`
	AggregationRegex          *string        `json:"aggregation_regex"`
	AggregationExcludeRegex   *string        `json:"aggregation_exclude_regex"`
	URL                       *string        `json:"url"`
	Interval                  *Int           `json:"interval"`
	Timeout                   *Int           `json:"timeout"`
	Tolerance                 *Int           `json:"tolerance"`
	Lazy                      *bool          `json:"lazy"`
	ExpectedStatus            *string        `json:"expected-status"`
	Strategy                  *string        `json:"strategy"`
	Chain                     *ProxyChain    `json:"chain"`
	Source                    *string        `json:"source"`
	SourceType                *string        `json:"source_type"`
	Proxies                   *[]string      `json:"proxies"`
	Use                       *[]string      `json:"use"`
	IncludeAll                *bool          `json:"include_all"`
	IncludeAllNative          *bool          `json:"include-all"`
	IncludeAllProviders       *bool          `json:"include_all_providers"`
	IncludeAllProvidersNative *bool          `json:"include-all-providers"`
	IncludeAllProxies         *bool          `json:"include_all_proxies"`
	IncludeAllProxiesNative   *bool          `json:"include-all-proxies"`
	MaxFailedTimes            *Int           `json:"max-failed-times"`
	DisableUDP                *bool          `json:"disable-udp"`
	Hidden                    *bool          `json:"hidden"`
	Icon                      *string        `json:"icon"`
	ExcludeFilter             *string        `json:"exclude-filter"`
	ExcludeType               *string        `json:"exclude-type"`
	Fields                    Fields
	raw                       Raw
}

// UnmarshalJSON decodes ProxyGroup with stored-member preservation and R5 fallback.
func (r *ProxyGroup) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when ProxyGroup is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *ProxyGroup) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// SetJSON explicitly replaces a declared group member, including owned zero/null values.
// It uses the same shared record codec as Profile.SetJSON.
func (r *ProxyGroup) SetJSON(member string, data json.RawMessage) error {
	return SetMemberJSON(r, &r.raw, member, data)
}

// MarshalJSON preserves unknown and unchanged ProxyGroup members.
func (r ProxyGroup) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// MemberJSON returns one effective declared ProxyGroup member without encoding siblings.
func (r *ProxyGroup) MemberJSON(name string) (json.RawMessage, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	if name == "enabled" {
		return r.raw.MemberJSON(name, r.Enabled)
	}
	return recordMemberJSON(r, &r.raw, name)
}

// RuleConfig is one profile rule or shared-library reference, including stored editor grouping metadata and legacy source fields.
type RuleConfig struct {
	ID                  string  `json:"id"`
	ItemType            string  `json:"itemType"`
	Enabled             *bool   `json:"enabled"`
	LibraryRuleID       string  `json:"library_rule_id"`
	RuleType            string  `json:"rule_type"`
	Value               string  `json:"value"`
	Policy              *string `json:"policy"`
	NoResolve           *bool   `json:"no_resolve"`
	UDPFallback         *string `json:"udp_fallback"`
	NTPDirect           *bool   `json:"ntp_direct"`
	Remark              string  `json:"remark"`
	GroupName           string  `json:"group_name"`
	GroupID             string  `json:"groupId"`
	GroupLabel          string  `json:"groupLabel"`
	GroupPolicy         string  `json:"groupPolicy"`
	IsExpandedGroupItem *bool   `json:"isExpandedGroupItem"`
	IsFirstInGroup      *bool   `json:"isFirstInGroup"`
	IsLastInGroup       *bool   `json:"isLastInGroup"`
	UniqueID            string  `json:"uniqueId"`
	Name                string  `json:"name"`
	URL                 *string `json:"url"`
	Behavior            *string `json:"behavior"`
	Content             *string `json:"content"`
	SourceType          *string `json:"source_type"`
	Format              *string `json:"format"`
	LibraryEnabled      *bool   `json:"library_enabled"`
	Fields              Fields
	raw                 Raw
}

// UnmarshalJSON decodes RuleConfig with stored-member preservation and R5 fallback.
func (r *RuleConfig) UnmarshalJSON(data []byte) error { return DecodeRecord(data, r, &r.raw) }

// Clear drops a declared member (by JSON name) when RuleConfig is written, even if
// it is stored as null or with a wrong type; see ClearMember.
func (r *RuleConfig) Clear(member string) error { return ClearMember(r, &r.raw, member) }

// MarshalJSON preserves unknown and unchanged RuleConfig members.
func (r RuleConfig) MarshalJSON() ([]byte, error) { return EncodeRecord(&r, &r.raw) }

// MemberJSON returns one effective declared RuleConfig member without encoding siblings.
func (r *RuleConfig) MemberJSON(name string) (json.RawMessage, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	switch name {
	case "enabled":
		return r.raw.MemberJSON(name, r.Enabled)
	case "library_enabled":
		return r.raw.MemberJSON(name, r.LibraryEnabled)
	default:
		return recordMemberJSON(r, &r.raw, name)
	}
}
