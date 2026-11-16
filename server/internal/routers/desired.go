package routers

import (
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"configflow/internal/mosdns"
	"configflow/internal/netipx"
)

// The desired router state: proxy CIDRs are the Mihomo fake-ip range plus
// the public IPv4 networks of the rule sets explicitly selected on the
// router (typically Telegram, whose clients connect by IP). Their public
// IPv6 networks are listed separately (only RouterOS routes them). Rule sets
// are never inferred from policies.

// DefaultFakeIPRange is Mihomo's default fake-ip range.
const DefaultFakeIPRange = "198.18.0.1/16"

// BuiltinNonProxy are the built-in policies that never proxy.
var BuiltinNonProxy = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE"}

// RuleSet is a profile rule_configs entry of itemType "ruleset". Raw is the
// whole entry for content loaders.
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

// FakeIPNetwork is the fake-ip network of the Mihomo base config
// (dns.fake-ip-range of the custom YAML), 198.18.0.0/16 by default.
func FakeIPNetwork(customConfig string) netip.Prefix {
	fallback, _ := netipx.ParseNetwork4(DefaultFakeIPRange, false)
	var doc struct {
		DNS map[string]any `yaml:"dns"`
	}
	if yaml.Unmarshal([]byte(customConfig), &doc) != nil {
		return fallback
	}
	value, _ := doc.DNS["fake-ip-range"].(string)
	if network, ok := netipx.ParseNetwork4(value, false); ok {
		return network
	}
	return fallback
}

// ProxyCIDR is a collapsed network and the sources inside it.
type ProxyCIDR struct {
	CIDR    string   `json:"cidr"`
	Sources []string `json:"sources"`
}

// MergeSources collapses source networks of one family; each result lists
// (sorted) every source name inside it.
func MergeSources(sources map[netip.Prefix][]string) []ProxyCIDR {
	networks := make([]netip.Prefix, 0, len(sources))
	for n := range sources {
		networks = append(networks, n)
	}
	collapsed, _ := netipx.Collapse(networks)
	out := make([]ProxyCIDR, len(collapsed))
	labels := make([]map[string]bool, len(collapsed))
	for i, n := range collapsed {
		out[i].CIDR, labels[i] = n.String(), map[string]bool{}
	}
	// Collapsed networks are disjoint and cover every source: each source lies
	// in the last one starting at or before it.
	for n, names := range sources {
		i := sort.Search(len(collapsed), func(i int) bool { return n.Addr().Less(collapsed[i].Addr()) }) - 1
		for _, name := range names {
			labels[i][name] = true
		}
	}
	for i := range out {
		out[i].Sources = make([]string, 0, len(labels[i]))
		for name := range labels[i] {
			out[i].Sources = append(out[i].Sources, name)
		}
		sort.Strings(out[i].Sources)
	}
	return out
}

// Desired is what the upstream router should contain. DNS, when set, is
// the DHCP DNS target (added by the inspection).
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

// RulesetContent is the default content loader: the rule set's local cache
// or inline content, never the network (routes.rules.get_ruleset_content,
// C32). Fails closed until C32 provides it.
var RulesetContent = func(profile json.RawMessage, item RuleSet, library *LibraryRule) (string, error) {
	return "", ErrNotPorted
}

type desiredProfile struct {
	Mihomo struct {
		CustomConfig string `json:"custom_config"`
	} `json:"mihomo"`
	ProxyGroups []json.RawMessage `json:"proxy_groups"`
	RuleConfigs []json.RawMessage `json:"rule_configs"`
	RuleLibrary []json.RawMessage `json:"rule_library"`
}

type proxyGroup struct {
	Name        string            `json:"name"`
	ManualNodes []string          `json:"manual_nodes"`
	Sources     []json.RawMessage `json:"-"`
}

// directOnlyGroups are strategy groups that can only resolve to built-in
// non-proxy policies.
func directOnlyGroups(groups []json.RawMessage) map[string]bool {
	names := map[string]bool{}
	for _, raw := range groups {
		var g proxyGroup
		var other map[string]json.RawMessage
		if json.Unmarshal(raw, &g) != nil || json.Unmarshal(raw, &other) != nil || len(g.ManualNodes) == 0 {
			continue
		}
		direct := true
		for _, key := range []string{"include_groups", "subscriptions", "aggregations", "regex", "follow_group"} {
			direct = direct && !truthy(other[key])
		}
		for _, node := range g.ManualNodes {
			direct = direct && contains(BuiltinNonProxy, node)
		}
		if direct {
			names[g.Name] = true
		}
	}
	return names
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// ComputeDesired builds the desired state for a profile (its stored JSON),
// the proxy gateway ("" when no Mihomo Agent is linked) and the static-route
// rule sets selected on the router. loader nil uses RulesetContent.
func ComputeDesired(profile json.RawMessage, gateway string, ruleSets []string, loader ContentLoader) (*Desired, error) {
	var doc desiredProfile
	if err := json.Unmarshal(profile, &doc); err != nil {
		return nil, err
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
	fakeIP := FakeIPNetwork(strings.TrimSpace(doc.Mihomo.CustomConfig))
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
		name := item.Name
		if name == "" {
			name = item.ID
		}
		if name == "" {
			name = "规则集"
		}
		var lib *LibraryRule
		if item.LibraryRuleID != "" {
			lib = library[item.LibraryRuleID]
		}
		if issue := RuleSourceIssue(item, lib, false); issue != "" {
			incomplete("规则集「" + name + "」：" + issue + "，未计入；取消对应路由选择后可预览撤回")
			continue
		}
		if contains(BuiltinNonProxy, item.Policy) || direct[item.Policy] {
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
			line = strings.TrimSpace(line)
			network, ok := netipx.ParseNetwork(line, false)
			if line == "" || strings.HasPrefix(line, "#") || !ok || network.Bits() == 0 || !netipx.IsGlobal(network) {
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
