package routers

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"sort"
	"strings"

	"configflow/internal/mosdns"
	"configflow/internal/netipx"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// DefaultFakeIPRange is Mihomo's default fake-ip range.
const DefaultFakeIPRange = "198.18.0.1/16"

// BuiltinNonProxy are the built-in policies that never proxy.
var BuiltinNonProxy = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE"}

// RuleSet is a profile rule_configs entry of itemType "ruleset". Raw is the whole entry for content loaders.
type RuleSet struct {
	ID             string          `json:"id"`
	ItemType       string          `json:"itemType"`
	Name           string          `json:"name"`
	Policy         string          `json:"policy"`
	Enabled        *bool           `json:"enabled"`
	LibraryEnabled *bool           `json:"library_enabled"`
	LibraryRuleID  string          `json:"library_rule_id"`
	Raw            json.RawMessage `json:"-"`
}

// LibraryRule is a shared rule-library entry.
type LibraryRule struct {
	ID      string          `json:"id"`
	Enabled *bool           `json:"enabled"`
	Raw     json.RawMessage `json:"-"`
}

// RuleSourceIssue says why a selected rule set cannot be used ("" when it
// can). item nil: the rule set no longer exists.
func RuleSourceIssue(item *RuleSet, library *LibraryRule, requireLibrary bool) string {
	switch {
	case item == nil:
		return "所选规则集已不存在"
	case item.Enabled != nil && !*item.Enabled:
		return "规则集在策略规则中已停用"
	case (item.LibraryRuleID != "" || requireLibrary) && library == nil:
		return "共享规则库条目已不存在"
	case item.LibraryEnabled != nil && !*item.LibraryEnabled, library != nil && library.Enabled != nil && !*library.Enabled:
		return "共享规则库条目已停用"
	}
	return ""
}

// FakeIPNetwork reads the IPv4 fake-IP range of a Mihomo base configuration.
// Ordinary syntax errors and absent/invalid range values use the established
// default; facade policy and unsafe-alias errors are returned before planning.
func FakeIPNetwork(customConfig string) (netip.Prefix, error) {
	doc, err := yamlx.Parse([]byte(customConfig))
	if err != nil && !errors.Is(err, yamlx.ErrSyntax) {
		return netip.Prefix{}, err
	}
	value := yamlx.Resolve(yamlx.Get(yamlx.Get(doc, "dns"), "fake-ip-range"))
	var text string
	if value != nil && value.Kind == yaml.ScalarNode {
		switch value.ShortTag() {
		case "!!null", "!!bool", "!!int", "!!float":
		default:
			text = value.Value
		}
	}
	if network, ok := netipx.ParseNetwork4(text, false); ok && err == nil {
		return network, nil
	}
	network, _ := netipx.ParseNetwork4(DefaultFakeIPRange, false)
	return network, nil
}

// desiredYAMLError retains both the existing HTTP validation classification
// and the native reason so callers can inspect D1/alias errors without parsing text.
type desiredYAMLError struct {
	validation *ValidationError
	cause      error
}

func (e *desiredYAMLError) Error() string   { return e.validation.Error() }
func (e *desiredYAMLError) Unwrap() []error { return []error{e.validation, e.cause} }

func (d desiredProfile) label() string {
	id, _ := jsonString(d.ProfileID)
	if id == "" {
		id, _ = jsonString(d.ID)
	}
	name, _ := jsonString(d.Name)
	if name != "" && id != "" {
		return name + "（" + id + "）"
	}
	return cmp.Or(name, id, messageCurrentProfile)
}

// ProxyCIDR is a collapsed network and the sources inside it.
type ProxyCIDR struct {
	CIDR    string   `json:"cidr"`
	Sources []string `json:"sources"`
}

// MergeSources collapses source networks of one family; each result lists (sorted) every source name inside it.
func MergeSources(sources map[netip.Prefix][]string) []ProxyCIDR {
	collapsed, _ := netipx.Collapse(slices.Collect(maps.Keys(sources)))
	out := make([]ProxyCIDR, len(collapsed))
	for i, n := range collapsed {
		out[i] = ProxyCIDR{CIDR: n.String(), Sources: []string{}}
	}
	// Collapsed networks are disjoint and cover every source: each source lies
	// in the last one starting at or before it.
	for source, names := range sources {
		i := sort.Search(len(collapsed), func(i int) bool { return source.Addr().Less(collapsed[i].Addr()) }) - 1
		out[i].Sources = append(out[i].Sources, names...)
	}
	for i := range out {
		slices.Sort(out[i].Sources)
		out[i].Sources = slices.Compact(out[i].Sources)
	}
	return out
}

// Desired is what the upstream router should contain: proxy CIDRs are the
// fake-ip range plus the public IPv4 networks of the rule sets selected on
// the router (never inferred from policies); their public IPv6 networks are
// listed apart. DNSSettings is added by the inspection.
type Desired struct {
	FakeIPRange      string      `json:"fake_ip_range"`
	ProxyGateway     string      `json:"proxy_gateway"`
	ProxyCIDRs       []ProxyCIDR `json:"proxy_cidrs"`
	IPv6CIDRs        []ProxyCIDR `json:"ipv6_cidrs"`
	Warnings         []string    `json:"warnings"`
	BlockingWarnings []string    `json:"blocking_warnings"`
	*DNSSettings
}

// ContentLoader returns a rule set's text (library nil: not from the shared
// library). An error counts as "no local cache", except ErrNotPorted.
type ContentLoader func(item RuleSet, library *LibraryRule) (string, error)

// RulesetContent is the default loader: local cache or inline content, never
// the network (C32 get_ruleset_content; fails closed until C32 sets it).
var RulesetContent = func(profile json.RawMessage, item RuleSet, library *LibraryRule) (string, error) {
	return "", ErrNotPorted
}

type desiredProfile struct {
	ID        json.RawMessage `json:"id"`
	ProfileID json.RawMessage `json:"profile_id"`
	Name      json.RawMessage `json:"name"`
	Mihomo    struct {
		CustomConfig string `json:"custom_config"`
	} `json:"mihomo"`
	ProxyGroups []json.RawMessage `json:"proxy_groups"`
	RuleConfigs []json.RawMessage `json:"rule_configs"`
	RuleLibrary []json.RawMessage `json:"rule_library"`
}

// directOnlyGroups are strategy groups that can only resolve to built-in non-proxy policies.
func directOnlyGroups(groups []json.RawMessage) map[string]bool {
	names := map[string]bool{}
	for _, raw := range groups {
		var g map[string]json.RawMessage
		var nodes []string
		direct := json.Unmarshal(raw, &g) == nil && json.Unmarshal(g["manual_nodes"], &nodes) == nil && len(nodes) > 0
		for _, key := range []string{"include_groups", "subscriptions", "aggregations", "regex", "follow_group"} {
			direct = direct && !truthy(g[key])
		}
		for _, node := range nodes {
			direct = direct && slices.Contains(BuiltinNonProxy, node)
		}
		if name, ok := jsonString(g["name"]); direct && ok {
			names[name] = true
		}
	}
	return names
}

// ComputeDesired builds the desired state for a profile (stored JSON), the
// proxy gateway ("" when unlinked) and the router's static-route rule sets.
func ComputeDesired(profile json.RawMessage, gateway string, ruleSets []string, loader ContentLoader) (*Desired, error) {
	var doc desiredProfile
	if err := json.Unmarshal(profile, &doc); err != nil {
		return nil, err
	}
	fakeIP, err := FakeIPNetwork(doc.Mihomo.CustomConfig)
	if err != nil {
		return nil, &desiredYAMLError{validation: &ValidationError{Message: fmt.Sprintf(messageDesiredYAML, doc.label(), err)}, cause: err}
	}
	if loader == nil {
		loader = func(item RuleSet, library *LibraryRule) (string, error) {
			return RulesetContent(profile, item, library)
		}
	}
	d := &Desired{ProxyGateway: gateway, Warnings: []string{}, BlockingWarnings: []string{}}
	incomplete := func(message string) {
		d.Warnings = append(d.Warnings, message)
		d.BlockingWarnings = append(d.BlockingWarnings, message)
	}
	d.FakeIPRange = fakeIP.String()
	sources := map[netip.Prefix][]string{fakeIP: {"fake-ip"}}
	sources6 := map[netip.Prefix][]string{}
	direct := directOnlyGroups(doc.ProxyGroups)
	library := map[string]*LibraryRule{}
	for _, raw := range doc.RuleLibrary {
		rule := &LibraryRule{Raw: raw}
		if json.Unmarshal(raw, rule) == nil {
			library[rule.ID] = rule
		}
	}
	rulesets := map[string]*RuleSet{}
	for _, raw := range doc.RuleConfigs {
		item := &RuleSet{Raw: raw}
		if json.Unmarshal(raw, item) == nil && item.ItemType == "ruleset" {
			rulesets[item.ID] = item
		}
	}
	for _, id := range ruleSets {
		item := rulesets[id]
		if item == nil {
			incomplete("所选规则集 " + id + " 已不存在，已忽略")
			continue
		}
		name := cmp.Or(item.Name, item.ID, "规则集")
		var lib *LibraryRule
		if item.LibraryRuleID != "" {
			lib = library[item.LibraryRuleID]
		}
		if issue := RuleSourceIssue(item, lib, false); issue != "" {
			incomplete("规则集「" + name + "」：" + issue + "，未计入；取消对应路由选择后可预览撤回")
			continue
		}
		if slices.Contains(BuiltinNonProxy, item.Policy) || direct[item.Policy] {
			d.Warnings = append(d.Warnings, "规则集「"+name+"」的策略是 "+item.Policy+"（不走代理），仍按勾选计入，请确认")
		}
		content, err := loader(*item, lib)
		if errors.Is(err, ErrNotPorted) {
			return nil, err
		}
		if err != nil || content == "" {
			incomplete("规则集「" + name + "」没有本地缓存，未计入其中的 IP 段")
			continue
		}
		lines, err := mosdns.ConvertRuleContentForMosDNS(content, mosdns.PartIP)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(lines, "\n") {
			network, ok := netipx.ParseNetwork(strings.TrimSpace(line), false)
			if !ok || network.Bits() == 0 || !netipx.IsGlobal(network) { // also blank and # lines
				continue
			}
			if network.Addr().Is4() {
				sources[network] = append(sources[network], name)
			} else {
				sources6[network] = append(sources6[network], name)
			}
		}
	}
	d.ProxyCIDRs, d.IPv6CIDRs = MergeSources(sources), MergeSources(sources6)
	if gateway == "" {
		incomplete("未关联 mihomo Agent，无法确定代理路由的下一跳")
	}
	return d, nil
}
