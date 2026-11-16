package routers

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/netipx"
	"configflow/internal/pystr"
	"configflow/internal/yamlx"
)

// Port of backend/routers/desired.py: what the upstream router should
// contain for the side-gateway setup (proxy CIDRs = the Mihomo fake-ip range
// plus the public IP-CIDR entries of the rule sets selected on the router).

// DefaultFakeIPRange is DEFAULT_FAKE_IP_RANGE.
const DefaultFakeIPRange = "198.18.0.1/16"

// BuiltinNonProxy is BUILTIN_NON_PROXY.
var BuiltinNonProxy = map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true}

// RuleSourceIssue is rule_source_issue(item, library_rule, require_library):
// "" when the rule set may be used, else why not. A nil item is None.
func RuleSourceIssue(item, libraryRule *jsonx.Object, requireLibrary bool) string {
	if item == nil {
		return "所选规则集已不存在"
	}
	if isFalse(item.Value("enabled")) {
		return "规则集在策略规则中已停用"
	}
	if (jsonx.Truthy(item.Value("library_rule_id")) || requireLibrary) && libraryRule == nil {
		return "共享规则库条目已不存在"
	}
	if isFalse(item.Value("library_enabled")) || (libraryRule != nil && isFalse(libraryRule.Value("enabled"))) {
		return "共享规则库条目已停用"
	}
	return ""
}

func defaultFakeIP() netip.Prefix {
	n, _ := netipx.ParseNetwork4(DefaultFakeIPRange, false)
	return n
}

// FakeIPNetwork is fake_ip_network(config): the fake-ip network of the
// Mihomo base config (default 198.18.0.0/16). Errors are the Python
// exceptions a malformed profile raises (AttributeError, RecursionError
// from yaml), or *yamlx.UnrepresentableError for a document Python loads
// but jsonx cannot hold (then the answer is unknown).
func FakeIPNetwork(config *jsonx.Object) (netip.Prefix, error) {
	mihomo := config.Value("mihomo")
	if !jsonx.Truthy(mihomo) {
		mihomo = jsonx.NewObject()
	}
	customRaw, err := getOf(mihomo, "custom_config")
	if err != nil {
		return netip.Prefix{}, err
	}
	custom := ""
	if jsonx.Truthy(customRaw) {
		text, ok := customRaw.(string)
		if !ok {
			return netip.Prefix{}, pyErr("AttributeError", "'"+typeName(customRaw)+"' object has no attribute 'strip'")
		}
		custom = pystr.Strip(text)
	}
	value := ""
	if custom != "" {
		parsed, err := yamlx.LoadWith(custom, yamlx.LoadOptions{KeepNonStringKeys: true})
		if err != nil {
			var yerr *yamlx.Error
			if errors.As(err, &yerr) && yerr.IsYAMLError() {
				parsed = nil
			} else {
				return netip.Prefix{}, err
			}
		}
		if dns, ok := yamlx.Lookup(parsed, "dns"); ok && yamlx.IsMapping(dns) {
			if raw, ok := yamlx.Lookup(dns, "fake-ip-range"); ok && jsonx.Truthy(raw) {
				// str(value): only a str can form a network.
				if text, ok := raw.(string); ok {
					value = text
				} else {
					value = "\x00"
				}
			}
		}
	}
	if value == "" {
		return defaultFakeIP(), nil
	}
	network, ok := netipx.ParseNetwork4(value, false)
	if !ok {
		return defaultFakeIP(), nil
	}
	return network, nil
}

// directOnlyGroups is _direct_only_groups(config): the set of names of
// strategy groups that can only resolve to DIRECT/REJECT.
func directOnlyGroups(config *jsonx.Object) (*pySet, error) {
	names := newPySet()
	groups, err := iterOr(config.Value("proxy_groups"))
	if err != nil {
		return nil, err
	}
	for _, raw := range groups {
		group, ok := raw.(*jsonx.Object)
		if !ok || group == nil {
			continue
		}
		nodes, err := iterOr(group.Value("manual_nodes"))
		if err != nil {
			return nil, err
		}
		other := false
		for _, key := range []string{"include_groups", "subscriptions", "aggregations", "regex", "follow_group"} {
			if jsonx.Truthy(group.Value(key)) {
				other = true
				break
			}
		}
		allBuiltin := true
		for _, node := range nodes {
			if !BuiltinNonProxy[jsonx.Str(node)] {
				allBuiltin = false
				break
			}
		}
		if len(nodes) > 0 && !other && allBuiltin {
			if err := names.add(group.Value("name")); err != nil {
				return nil, err
			}
		}
	}
	return names, nil
}

// publicNetworks is _public_networks(lines): the public IPv4 and IPv6
// networks of converted IP lines.
func publicNetworks(lines string) (v4, v6 []netip.Prefix) {
	for _, raw := range convert.PySplitlines(lines) {
		value := pystr.Strip(raw)
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		network, ok := netipx.ParseNetwork(value, false)
		if !ok {
			continue
		}
		if network.Bits() > 0 && netipx.IsGlobal(network) {
			if network.Addr().Is4() {
				v4 = append(v4, network)
			} else {
				v6 = append(v6, network)
			}
		}
	}
	return v4, v6
}

// Sources is the {network: [source names]} dict merge_sources takes
// (insertion-ordered; equal networks are one key, like Python's).
type Sources struct {
	order []netip.Prefix
	names map[netip.Prefix][]string
}

// NewSources returns an empty Sources.
func NewSources() *Sources { return &Sources{names: map[netip.Prefix][]string{}} }

// Set is sources[network] = names.
func (s *Sources) Set(network netip.Prefix, names []string) {
	if _, ok := s.names[network]; !ok {
		s.order = append(s.order, network)
	}
	s.names[network] = names
}

// Add is sources.setdefault(network, []).append(name).
func (s *Sources) Add(network netip.Prefix, name string) {
	if _, ok := s.names[network]; !ok {
		s.order = append(s.order, network)
	}
	s.names[network] = append(s.names[network], name)
}

// Len is len(sources).
func (s *Sources) Len() int { return len(s.order) }

// MergeSources is merge_sources(sources): the collapsed networks (one
// address family), each listing every source name inside it (sorted).
func MergeSources(sources *Sources) ([]any, error) {
	out := []any{}
	if sources == nil || sources.Len() == 0 {
		return out, nil
	}
	collapsed, err := netipx.Collapse(sources.order)
	if err != nil {
		return nil, pyErr("TypeError", err.Error())
	}
	starts := make([]netip.Addr, len(collapsed))
	for i, network := range collapsed {
		starts[i] = network.Addr()
	}
	labels := make([]map[string]bool, len(collapsed))
	for i := range labels {
		labels[i] = map[string]bool{}
	}
	for _, original := range sources.order {
		value := original.Addr()
		index := sort.Search(len(starts), func(i int) bool { return value.Less(starts[i]) }) - 1
		if index < 0 {
			index = len(labels) - 1 // Python's labels[-1]
		}
		for _, name := range sources.names[original] {
			labels[index][name] = true
		}
	}
	for i, network := range collapsed {
		names := make([]string, 0, len(labels[i]))
		for name := range labels[i] {
			names = append(names, name)
		}
		sort.Strings(names)
		out = append(out, jsonx.Obj("cidr", network.String(), "sources", anyStrings(names)))
	}
	return out, nil
}

// ContentLoader is compute_desired's content_loader(rule_item,
// library_rule): rule set text. libraryRule nil is None. An error is an
// exception the loader raised (counted as "no local cache"), except
// *PendingError, which is returned to the caller.
type ContentLoader func(item, libraryRule *jsonx.Object) (string, error)

// RulesetContent is routes.rules.get_ruleset_content(item, library_rule,
// allow_network=False, config_data=config) (C32, to be exported from a
// non-API package). Until then it fails closed.
var RulesetContent = func(ctx context.Context, item, libraryRule, config *jsonx.Object) (string, error) {
	return "", pending("C32", "routes.rules.get_ruleset_content")
}

// hashedObjects is {key: item} built with Python dict semantics.
func hashedObjects(list any, keep func(*jsonx.Object) bool) (map[string]*jsonx.Object, error) {
	items, err := iterOr(list)
	if err != nil {
		return nil, err
	}
	out := map[string]*jsonx.Object{}
	for _, raw := range items {
		item, ok := raw.(*jsonx.Object)
		if !ok || item == nil || !keep(item) {
			continue
		}
		key, err := hashKey(item.Value("id"))
		if err != nil {
			return nil, err
		}
		out[key] = item
	}
	return out, nil
}

// ComputeDesired is compute_desired(config, proxy_gateway, ip_rule_sets,
// content_loader). loader nil is the default (local caches/inline content,
// RulesetContent). proxyGateway "" is None/empty.
func ComputeDesired(ctx context.Context, config *jsonx.Object, proxyGateway string, ipRuleSets []any, loader ContentLoader) (*jsonx.Object, error) {
	if loader == nil {
		loader = func(item, libraryRule *jsonx.Object) (string, error) {
			return RulesetContent(ctx, item, libraryRule, config)
		}
	}
	warnings, blocking := []any{}, []any{}
	incomplete := func(message string) {
		warnings = append(warnings, message)
		blocking = append(blocking, message)
	}
	sources, sources6 := NewSources(), NewSources()
	fakeIP, err := FakeIPNetwork(config)
	if err != nil {
		return nil, err
	}
	sources.Set(fakeIP, []string{"fake-ip"})
	directGroups, err := directOnlyGroups(config)
	if err != nil {
		return nil, err
	}
	library, err := hashedObjects(config.Value("rule_library"), func(*jsonx.Object) bool { return true })
	if err != nil {
		return nil, err
	}
	rulesets, err := hashedObjects(config.Value("rule_configs"), func(item *jsonx.Object) bool {
		return jsonx.Equal(item.Value("itemType"), "ruleset")
	})
	if err != nil {
		return nil, err
	}
	for _, ruleID := range ipRuleSets {
		key, err := hashKey(ruleID)
		if err != nil {
			return nil, err
		}
		item := rulesets[key]
		if item == nil {
			incomplete("所选规则集 " + jsonx.Str(ruleID) + " 已不存在，已忽略")
			continue
		}
		nameValue := item.Value("name")
		if !jsonx.Truthy(nameValue) {
			nameValue = item.Value("id")
		}
		if !jsonx.Truthy(nameValue) {
			nameValue = "规则集"
		}
		name := jsonx.Str(nameValue)
		libraryKey, err := hashKey(item.Value("library_rule_id"))
		if err != nil {
			return nil, err
		}
		libraryRule := library[libraryKey]
		if issue := RuleSourceIssue(item, libraryRule, false); issue != "" {
			incomplete("规则集「" + name + "」：" + issue + "，未计入；取消对应路由选择后可预览撤回")
			continue
		}
		policy := orEmptyStr(item.Value("policy"))
		inDirect, _ := directGroups.has(policy)
		if BuiltinNonProxy[policy] || inDirect {
			warnings = append(warnings, "规则集「"+name+"」的策略是 "+policy+"（不走代理），仍按勾选计入，请确认")
		}
		content, err := loader(item, libraryRule)
		if err != nil {
			if IsPending(err) {
				return nil, err
			}
			content = ""
		}
		if content == "" {
			incomplete("规则集「" + name + "」没有本地缓存，未计入其中的 IP 段")
			continue
		}
		lines, err := mosdns.ConvertRuleContentForMosDNS(content, mosdns.PartIP)
		if err != nil {
			return nil, err
		}
		v4, v6 := publicNetworks(lines)
		for _, network := range v4 {
			sources.Add(network, name)
		}
		for _, network := range v6 {
			sources6.Add(network, name)
		}
	}
	proxyCIDRs, err := MergeSources(sources)
	if err != nil {
		return nil, err
	}
	if proxyGateway == "" {
		incomplete("未关联 mihomo Agent，无法确定代理路由的下一跳")
	}
	ipv6CIDRs, err := MergeSources(sources6)
	if err != nil {
		return nil, err
	}
	return jsonx.Obj("fake_ip_range", fakeIP.String(), "proxy_gateway", proxyGateway, "proxy_cidrs", proxyCIDRs,
		"ipv6_cidrs", ipv6CIDRs, "warnings", warnings, "blocking_warnings", blocking), nil
}
