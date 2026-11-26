// Package proxygroups manages profile policy groups. Membership, health,
// listener editing and DNS routing remain owned by their shared packages.
package proxygroups

import (
	"encoding/json"
	"strconv"

	"configflow/internal/health/check"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	shared "configflow/internal/rules"
)

// member is one request member; see jsondecode.Member.
type member[T any] = jsondecode.Member[T]

// groupRequest is a policy group create/update request body decoded with exact
// member matching. Every member uses jsondecode.Member so a wrong-typed member
// that kind-specific cleanup drops never fails decoding, and validation sees
// the exact sent values before strict decoding into model.ProxyGroup.
type groupRequest struct {
	DeleteFields member[[]string] `json:"delete_fields"`

	ID      member[string] `json:"id"`
	Name    member[string] `json:"name"`
	Type    member[string] `json:"type"`
	Enabled member[bool]   `json:"enabled"`

	// Candidate and filter members (matching rules.GroupMembers).
	Subscriptions           member[[]member[string]]    `json:"subscriptions"`
	ManualNodes             member[[]member[string]]    `json:"manual_nodes"`
	Aggregations            member[[]member[string]]    `json:"aggregations"`
	IncludeGroups           member[[]member[string]]    `json:"include_groups"`
	Proxies                 member[[]member[string]]    `json:"proxies"`
	ProxiesOrder            member[[]shared.OrderEntry] `json:"proxies_order"`
	FollowGroup             member[string]              `json:"follow_group"`
	Source                  member[string]              `json:"source"`
	Regex                   member[string]              `json:"regex"`
	ExcludeRegex            member[string]              `json:"exclude_regex"`
	AggregationRegex        member[string]              `json:"aggregation_regex"`
	AggregationExcludeRegex member[string]              `json:"aggregation_exclude_regex"`

	// Health-check members.
	URL            member[json.RawMessage] `json:"url"`
	Interval       member[json.RawMessage] `json:"interval"`
	Timeout        member[json.RawMessage] `json:"timeout"`
	Tolerance      member[json.RawMessage] `json:"tolerance"`
	Lazy           member[json.RawMessage] `json:"lazy"`
	ExpectedStatus member[json.RawMessage] `json:"expected-status"`

	// Kind-specific and additional ProxyGroup members.
	Strategy                  member[json.RawMessage] `json:"strategy"`
	Chain                     member[json.RawMessage] `json:"chain"`
	SourceType                member[json.RawMessage] `json:"source_type"`
	ProxyOrder                member[json.RawMessage] `json:"proxy_order"`
	Use                       member[json.RawMessage] `json:"use"`
	IncludeAll                member[json.RawMessage] `json:"include_all"`
	IncludeAllNative          member[json.RawMessage] `json:"include-all"`
	IncludeAllProviders       member[json.RawMessage] `json:"include_all_providers"`
	IncludeAllProvidersNative member[json.RawMessage] `json:"include-all-providers"`
	IncludeAllProxies         member[json.RawMessage] `json:"include_all_proxies"`
	IncludeAllProxiesNative   member[json.RawMessage] `json:"include-all-proxies"`
	MaxFailedTimes            member[json.RawMessage] `json:"max-failed-times"`
	DisableUDP                member[json.RawMessage] `json:"disable-udp"`
	Hidden                    member[json.RawMessage] `json:"hidden"`
	Icon                      member[json.RawMessage] `json:"icon"`
	ExcludeFilter             member[json.RawMessage] `json:"exclude-filter"`
	ExcludeType               member[json.RawMessage] `json:"exclude-type"`
}

func decodeGroupRequest(raw json.RawMessage) (*groupRequest, error) {
	out := new(groupRequest)
	if len(raw) == 0 {
		return out, nil
	}
	if err := jsondecode.Unmarshal(raw, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *groupRequest) groupMembers() shared.GroupMembers {
	return shared.GroupMembers{
		ManualNodes:             r.ManualNodes,
		Subscriptions:           r.Subscriptions,
		Aggregations:            r.Aggregations,
		IncludeGroups:           r.IncludeGroups,
		Proxies:                 r.Proxies,
		ProxiesOrder:            r.ProxiesOrder,
		FollowGroup:             r.FollowGroup,
		Source:                  r.Source,
		Regex:                   r.Regex,
		AggregationRegex:        r.AggregationRegex,
		ExcludeRegex:            r.ExcludeRegex,
		AggregationExcludeRegex: r.AggregationExcludeRegex,
	}
}

func (r *groupRequest) hasHealthField(name string) bool {
	switch name {
	case "url":
		return r.URL.Present()
	case "interval":
		return r.Interval.Present()
	case "timeout":
		return r.Timeout.Present()
	case "tolerance":
		return r.Tolerance.Present()
	case "lazy":
		return r.Lazy.Present()
	case "expected-status":
		return r.ExpectedStatus.Present()
	default:
		return false
	}
}

func (r *groupRequest) clearHealthField(name string) {
	switch name {
	case "url":
		r.URL = member[json.RawMessage]{}
	case "interval":
		r.Interval = member[json.RawMessage]{}
	case "timeout":
		r.Timeout = member[json.RawMessage]{}
	case "tolerance":
		r.Tolerance = member[json.RawMessage]{}
	case "lazy":
		r.Lazy = member[json.RawMessage]{}
	case "expected-status":
		r.ExpectedStatus = member[json.RawMessage]{}
	}
}

func rawMember(raw json.RawMessage) member[json.RawMessage] {
	return member[json.RawMessage]{Raw: raw, Value: raw, OK: true}
}

func (r *groupRequest) initHealthDefaults(kind string) {
	if !check.IsGroupHealthType(kind) {
		return
	}
	d := check.DefaultGroupHealth()
	if !r.URL.Present() {
		raw, _ := json.Marshal(d.URL)
		r.URL = rawMember(raw)
	}
	if !r.Interval.Present() {
		r.Interval = rawMember(json.RawMessage(strconv.FormatInt(d.Interval, 10)))
	}
	if !r.Timeout.Present() {
		r.Timeout = rawMember(json.RawMessage(strconv.FormatInt(d.Timeout, 10)))
	}
	if !r.Lazy.Present() {
		r.Lazy = rawMember(json.RawMessage(strconv.FormatBool(d.Lazy)))
	}
	if !r.ExpectedStatus.Present() {
		raw, _ := json.Marshal(d.ExpectedStatus)
		r.ExpectedStatus = rawMember(raw)
	}
	if kind == "url-test" && !r.Tolerance.Present() {
		r.Tolerance = rawMember(json.RawMessage("50"))
	}
}

func (r *groupRequest) clean(kind string) {
	if !check.IsGroupHealthType(kind) {
		for _, key := range shared.HealthFields {
			r.clearHealthField(key)
		}
	}
	if kind != "url-test" {
		r.Tolerance = member[json.RawMessage]{}
	}
	if kind != "load-balance" {
		r.Strategy = member[json.RawMessage]{}
	}
	if kind == "chain" {
		r.Subscriptions = member[[]member[string]]{}
		r.Regex = member[string]{}
		r.ExcludeRegex = member[string]{}
		r.ManualNodes = member[[]member[string]]{}
		r.Aggregations = member[[]member[string]]{}
		r.AggregationRegex = member[string]{}
		r.AggregationExcludeRegex = member[string]{}
		r.IncludeGroups = member[[]member[string]]{}
		r.FollowGroup = member[string]{}
		r.Proxies = member[[]member[string]]{}
		r.Source = member[string]{}
		r.SourceType = member[json.RawMessage]{}
		r.ProxiesOrder = member[[]shared.OrderEntry]{}
		r.URL = member[json.RawMessage]{}
		r.Interval = member[json.RawMessage]{}
		r.Strategy = member[json.RawMessage]{}
		r.Lazy = member[json.RawMessage]{}
		r.Use = member[json.RawMessage]{}
		r.IncludeAll = member[json.RawMessage]{}
		r.IncludeAllNative = member[json.RawMessage]{}
	} else {
		r.Chain = member[json.RawMessage]{}
	}
}

func (r *groupRequest) enabled() bool {
	if !r.Enabled.Present() {
		return true
	}
	return shared.EnabledField(r.Enabled.Raw, true)
}

func (r *groupRequest) rawEntries() []struct {
	name string
	raw  json.RawMessage
} {
	return []struct {
		name string
		raw  json.RawMessage
	}{
		{"id", r.ID.Raw},
		{"name", r.Name.Raw},
		{"type", r.Type.Raw},
		{"enabled", r.Enabled.Raw},
		{"subscriptions", r.Subscriptions.Raw},
		{"manual_nodes", r.ManualNodes.Raw},
		{"aggregations", r.Aggregations.Raw},
		{"include_groups", r.IncludeGroups.Raw},
		{"follow_group", r.FollowGroup.Raw},
		{"proxies_order", r.ProxiesOrder.Raw},
		{"proxy_order", r.ProxyOrder.Raw},
		{"regex", r.Regex.Raw},
		{"exclude_regex", r.ExcludeRegex.Raw},
		{"aggregation_regex", r.AggregationRegex.Raw},
		{"aggregation_exclude_regex", r.AggregationExcludeRegex.Raw},
		{"url", r.URL.Raw},
		{"interval", r.Interval.Raw},
		{"timeout", r.Timeout.Raw},
		{"tolerance", r.Tolerance.Raw},
		{"lazy", r.Lazy.Raw},
		{"expected-status", r.ExpectedStatus.Raw},
		{"strategy", r.Strategy.Raw},
		{"chain", r.Chain.Raw},
		{"source", r.Source.Raw},
		{"source_type", r.SourceType.Raw},
		{"proxies", r.Proxies.Raw},
		{"use", r.Use.Raw},
		{"include_all", r.IncludeAll.Raw},
		{"include-all", r.IncludeAllNative.Raw},
		{"include_all_providers", r.IncludeAllProviders.Raw},
		{"include-all-providers", r.IncludeAllProvidersNative.Raw},
		{"include_all_proxies", r.IncludeAllProxies.Raw},
		{"include-all-proxies", r.IncludeAllProxiesNative.Raw},
		{"max-failed-times", r.MaxFailedTimes.Raw},
		{"disable-udp", r.DisableUDP.Raw},
		{"hidden", r.Hidden.Raw},
		{"icon", r.Icon.Raw},
		{"exclude-filter", r.ExcludeFilter.Raw},
		{"exclude-type", r.ExcludeType.Raw},
	}
}

// buildGroup decodes all present members into a fresh model.ProxyGroup.
// firstErr is the first strict type error, if any; valid members are still
// populated so candidate validation can run before storage reports type errors.
func (r *groupRequest) buildGroup() (model.ProxyGroup, error) {
	var out model.ProxyGroup
	var firstErr error
	for _, entry := range r.rawEntries() {
		if entry.raw == nil {
			continue
		}
		if err := model.DecodeInputMember("proxy_group", &out, entry.name, entry.raw); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return out, firstErr
}

// marshalSent encodes all present members as a JSON object for repository
// input-error shape reporting when buildGroup encounters a type mismatch.
func (r *groupRequest) marshalSent() (json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	for _, entry := range r.rawEntries() {
		if entry.raw != nil {
			m[entry.name] = entry.raw
		}
	}
	return json.Marshal(m)
}

func requestFromGroup(group model.ProxyGroup) (*groupRequest, error) {
	data, err := model.Marshal(group)
	if err != nil {
		return nil, err
	}
	return decodeGroupRequest(data)
}

func overlayGroup(base, patch *groupRequest, deletes []string) *groupRequest {
	merged := *base
	if patch.ID.Present() {
		merged.ID = patch.ID
	}
	if patch.Name.Present() {
		merged.Name = patch.Name
	}
	if patch.Type.Present() {
		merged.Type = patch.Type
	}
	if patch.Enabled.Present() {
		merged.Enabled = patch.Enabled
	}
	if patch.Subscriptions.Present() {
		merged.Subscriptions = patch.Subscriptions
	}
	if patch.ManualNodes.Present() {
		merged.ManualNodes = patch.ManualNodes
	}
	if patch.Aggregations.Present() {
		merged.Aggregations = patch.Aggregations
	}
	if patch.IncludeGroups.Present() {
		merged.IncludeGroups = patch.IncludeGroups
	}
	if patch.Proxies.Present() {
		merged.Proxies = patch.Proxies
	}
	if patch.ProxiesOrder.Present() {
		merged.ProxiesOrder = patch.ProxiesOrder
	}
	if patch.FollowGroup.Present() {
		merged.FollowGroup = patch.FollowGroup
	}
	if patch.Source.Present() {
		merged.Source = patch.Source
	}
	if patch.Regex.Present() {
		merged.Regex = patch.Regex
	}
	if patch.ExcludeRegex.Present() {
		merged.ExcludeRegex = patch.ExcludeRegex
	}
	if patch.AggregationRegex.Present() {
		merged.AggregationRegex = patch.AggregationRegex
	}
	if patch.AggregationExcludeRegex.Present() {
		merged.AggregationExcludeRegex = patch.AggregationExcludeRegex
	}
	if patch.URL.Present() {
		merged.URL = patch.URL
	}
	if patch.Interval.Present() {
		merged.Interval = patch.Interval
	}
	if patch.Timeout.Present() {
		merged.Timeout = patch.Timeout
	}
	if patch.Tolerance.Present() {
		merged.Tolerance = patch.Tolerance
	}
	if patch.Lazy.Present() {
		merged.Lazy = patch.Lazy
	}
	if patch.ExpectedStatus.Present() {
		merged.ExpectedStatus = patch.ExpectedStatus
	}
	if patch.Strategy.Present() {
		merged.Strategy = patch.Strategy
	}
	if patch.Chain.Present() {
		merged.Chain = patch.Chain
	}
	if patch.SourceType.Present() {
		merged.SourceType = patch.SourceType
	}
	if patch.ProxyOrder.Present() {
		merged.ProxyOrder = patch.ProxyOrder
	}
	if patch.Use.Present() {
		merged.Use = patch.Use
	}
	if patch.IncludeAll.Present() {
		merged.IncludeAll = patch.IncludeAll
	}
	if patch.IncludeAllNative.Present() {
		merged.IncludeAllNative = patch.IncludeAllNative
	}
	if patch.IncludeAllProviders.Present() {
		merged.IncludeAllProviders = patch.IncludeAllProviders
	}
	if patch.IncludeAllProvidersNative.Present() {
		merged.IncludeAllProvidersNative = patch.IncludeAllProvidersNative
	}
	if patch.IncludeAllProxies.Present() {
		merged.IncludeAllProxies = patch.IncludeAllProxies
	}
	if patch.IncludeAllProxiesNative.Present() {
		merged.IncludeAllProxiesNative = patch.IncludeAllProxiesNative
	}
	if patch.MaxFailedTimes.Present() {
		merged.MaxFailedTimes = patch.MaxFailedTimes
	}
	if patch.DisableUDP.Present() {
		merged.DisableUDP = patch.DisableUDP
	}
	if patch.Hidden.Present() {
		merged.Hidden = patch.Hidden
	}
	if patch.Icon.Present() {
		merged.Icon = patch.Icon
	}
	if patch.ExcludeFilter.Present() {
		merged.ExcludeFilter = patch.ExcludeFilter
	}
	if patch.ExcludeType.Present() {
		merged.ExcludeType = patch.ExcludeType
	}
	for _, field := range deletes {
		merged.clearHealthField(field)
	}
	return &merged
}

func find(groups []model.ProxyGroup, id string) int {
	for i := range groups {
		if groups[i].ID == id {
			return i
		}
	}
	return -1
}

type fault struct {
	status  int
	message string
}

func (e *fault) Error() string      { return e.message }
func bad(message string) error      { return &fault{400, message} }
func conflict(message string) error { return &fault{409, message} }
func absent(message string) error   { return &fault{404, message} }

type result struct {
	Success         bool              `json:"success"`
	Data            *model.ProxyGroup `json:"data,omitempty"`
	Warnings        []string          `json:"warnings,omitempty"`
	ListenerChanges []string          `json:"listener_changes,omitempty"`
	Message         string            `json:"message,omitempty"`
}
