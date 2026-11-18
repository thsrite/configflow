package convert

// Port of backend/converters/loon.py (C20): the Loon configuration of a
// profile (https://nsloon.app/docs/intro).
//   - manual nodes go to [Proxy]
//   - subscriptions / aggregations are [Remote Proxy] entries pointing at this
//     server's ?format=loon endpoints
//   - a group's node-name regex becomes a [Remote Filter] NameRegex filter
//   - rule sets go to [Remote Rule]
// Manual node strings use subs.ParseURIList exactly as Python (see surge.go).

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

var loonLog = logx.Get("backend.converters.loon")

// LoonDefaultTestURL is DEFAULT_TEST_URL.
const LoonDefaultTestURL = "https://www.gstatic.com/generate_204"

// LoonBuiltinPolicies is LOON_BUILTIN_POLICIES (https://nsloon.app/docs/Policy/).
var LoonBuiltinPolicies = []string{"DIRECT", "REJECT", "REJECT-DROP", "REJECT-IMG", "REJECT-DICT", "REJECT-ARRAY"}

// LoonRuleTypes is LOON_RULE_TYPES: the local rule types Loon documents;
// others are commented out.
var LoonRuleTypes = []string{"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "IP-CIDR", "IP-CIDR6", "GEOIP", "IP-ASN",
	"SRC-PORT", "DEST-PORT", "URL-REGEX", "USER-AGENT", "PROTOCOL", "AND", "OR", "NOT"}

// loonRuleTypeMap is LOON_RULE_TYPE_MAP (mihomo/Clash → Loon).
var loonRuleTypeMap = [][2]string{{"DST-PORT", "DEST-PORT"}}

// loadBalanceAlgorithms is LOAD_BALANCE_ALGORITHMS.
var loadBalanceAlgorithms = map[string]string{"consistent-hashing": "pcc", "sticky-sessions": "pcc", "round-robin": "round-robin"}

// LoonVmessCiphers is VMESS_CIPHERS.
var LoonVmessCiphers = []string{"none", "auto", "aes-128-cfb", "aes-128-gcm", "chacha20-ietf-poly1305"}

// LoonDefaultGeneral is DEFAULT_GENERAL. bypass-tun must not contain
// 198.18.0.0/15, Loon's fake IP range.
var LoonDefaultGeneral = []string{
	"[General]",
	"ip-mode = dual",
	"dns-server = system,223.5.5.5,119.29.29.29",
	"doh-server = https://dns.alidns.com/dns-query",
	"hijack-dns = *:53",
	"proxy-test-url = https://www.gstatic.com/generate_204",
	"internet-test-url = http://wifi.vivo.com.cn/generate_204",
	"test-timeout = 5",
	"interface-mode = auto",
	"sni-sniffing = true",
	"udp-fallback-mode = DIRECT",
	"allow-wifi-access = false",
	"wifi-access-http-port = 7222",
	"wifi-access-socks5-port = 7221",
	"skip-proxy = 192.168.0.0/16,10.0.0.0/8,172.16.0.0/12,localhost,*.local,captive.apple.com",
	"bypass-tun = 10.0.0.0/8,100.64.0.0/10,127.0.0.0/8,169.254.0.0/16,172.16.0.0/12,192.0.0.0/24,192.0.2.0/24,192.88.99.0/24,192.168.0.0/16,198.51.100.0/24,203.0.113.0/24,224.0.0.0/4,255.255.255.255/32",
}

var (
	loonSubTypeRe = regexp.MustCompile(`\([\s\x{0085}\p{Z}]*([A-Z0-9-]+)[\s\x{0085}\p{Z}]*,`)
	loonDownRe    = regexp.MustCompile(`^(\p{Nd}+)`)
)

// effectiveLoonGroup is _effective_group: follow mode merged into the
// group (own name kept); nil when the followed group does not exist.
func effectiveLoonGroup(group any, config *jsonx.Object) any {
	follow := dictGet(group, "follow_group", nil)
	if !truthy(follow) {
		return group
	}
	followed := nextMatch(dictGet(config, "proxy_groups", []any{}), "id", follow)
	if !truthy(followed) {
		return nil
	}
	return followedGroupView(group, followed, true)
}

// loonGroupSources is _group_sources: (manual_nodes, aggregation_ids,
// include_groups, subscriptions) with the legacy format.
func loonGroupSources(group any) (manual, aggregations, includeGroups, subscriptions any) {
	manual = dictGet(group, "manual_nodes", []any{})
	aggregations = dictGet(group, "aggregations", []any{})
	includeGroups = dictGet(group, "include_groups", []any{})
	subscriptions = dictGet(group, "subscriptions", []any{})
	if !truthy(manual) && !truthy(aggregations) && !truthy(includeGroups) && !truthy(subscriptions) {
		source := dictGet(group, "source", "subscription")
		proxiesOld := dictGet(group, "proxies", []any{})
		if eq(source, "node") {
			manual = proxiesOld
		} else if eq(source, "strategy") {
			includeGroups = proxiesOld
		}
	}
	return
}

// memberRef is a ('node' | 'strategy' | 'builtin', id) tuple.
type memberRef struct {
	kind string
	id   any
}

func nodeRef(id any) memberRef {
	if inStrings(id, "DIRECT", "REJECT") {
		return memberRef{"builtin", id}
	}
	return memberRef{"node", id}
}

// loonGroupMemberRefs is _group_member_refs: member references in the
// user's order.
func loonGroupMemberRefs(group any) []memberRef {
	manual, _, includeGroups, _ := loonGroupSources(group)
	var refs []memberRef
	if order := dictGet(group, "proxies_order", []any{}); truthy(order) {
		for _, item := range iterate(order) {
			itemID := dictGet(item, "id", nil)
			if eq(dictGet(item, "type", nil), "node") {
				refs = append(refs, nodeRef(itemID))
			} else if eq(dictGet(item, "type", nil), "strategy") {
				refs = append(refs, memberRef{"strategy", itemID})
			}
		}
		orderIDs := newSet()
		for _, item := range iterate(order) {
			if eq(dictGet(item, "type", nil), "node") {
				orderIDs.add(dictGet(item, "id", nil))
			}
		}
		for _, nodeID := range iterate(manual) {
			if !orderIDs.has(nodeID) {
				refs = append(refs, nodeRef(nodeID))
			}
		}
		return refs
	}
	var nodes, strategies []memberRef
	for _, n := range iterate(manual) {
		nodes = append(nodes, nodeRef(n))
	}
	for _, g := range iterate(includeGroups) {
		strategies = append(strategies, memberRef{"strategy", g})
	}
	if eq(dictGet(group, "proxy_order", "nodes_first"), "strategies_first") {
		return append(strategies, nodes...)
	}
	return append(nodes, strategies...)
}

// nameAllocator is _NameAllocator: Remote Proxy / Remote Filter names that
// do not collide with nodes, groups or built-in policies.
type nameAllocator struct{ used map[string]bool }

func newNameAllocator(reserved []string) *nameAllocator {
	a := &nameAllocator{used: map[string]bool{}}
	for _, name := range reserved {
		a.used[name] = true
	}
	for _, name := range LoonBuiltinPolicies {
		a.used[name] = true
	}
	return a
}

func (a *nameAllocator) allocate(preferred any, suffix string) string {
	name := sanitizeLoonName(preferred)
	if name == "" {
		name = suffix
	}
	if !a.used[name] {
		a.used[name] = true
		return name
	}
	candidate := name + "-" + suffix
	for index := 2; a.used[candidate]; index++ {
		candidate = name + "-" + suffix + strconv.Itoa(index)
	}
	a.used[candidate] = true
	return candidate
}

// sanitizeLoonName is _sanitize_name: names appear in "name = ..." and comma
// separated lists.
func sanitizeLoonName(name any) string {
	text := pyReplace(or2(name, ""), ",", " ")
	return strings.TrimSpace(strings.ReplaceAll(text, "=", " "))
}

// SanitizeLoonName is _sanitize_name for callers outside the generator.
func SanitizeLoonName(name any) (out string, err error) {
	defer catch(&err)
	return sanitizeLoonName(name), nil
}

// loonMember is a resolved member: a name, or a ('strategy', id) reference.
type loonMember struct {
	name  string
	ref   any
	isRef bool
}

type loonGroupEntry struct {
	group   any
	members []loonMember
}

// GenerateLoonConfig is generate_loon_config(config_data, base_url). Errors
// are the exceptions Python raises: *DialerReferenceError,
// StrategyReferenceError, *HookMissingError (Sub-Store not wired) and
// *PyError for crashes on malformed values.
func GenerateLoonConfig(ctx context.Context, config *jsonx.Object, baseURL string) (out string, err error) {
	defer catch(&err)
	return generateLoon(ctx, config, baseURL), nil
}

func generateLoon(ctx context.Context, config *jsonx.Object, baseURL string) string {
	must(validateGenerationRules(config))
	must(ValidateDialers(config))
	must(RejectSurgeDialers(ctx, config, "Loon", clientConverter()))

	effective := effectiveBaseURL(config, baseURL)
	token := dictGet(dictGet(config, "system_config", jsonx.NewObject()), "config_token", "")

	customSections := parseCustomSections(dictGet(dictGet(config, "loon", jsonx.NewObject()), "custom_config", ""))
	fallback := mustValue(UDPFallback(config))
	general, _ := sectionText(customSections, "General")
	if general != "" {
		if fallback != "" {
			general = SetGeneralOption(general, "udp-fallback-mode", fallback)
		}
	} else {
		lines := make([]string, len(LoonDefaultGeneral))
		for i, line := range LoonDefaultGeneral {
			if fallback != "" && strings.HasPrefix(line, "udp-fallback-mode") {
				line = "udp-fallback-mode = " + fallback
			}
			lines[i] = line
		}
		general = strings.Join(lines, "\n")
	}
	sections := []string{general}

	allGroups := dictGet(config, "proxy_groups", []any{})
	groupsByID := newDict()
	for _, g := range iterate(allGroups) {
		groupsByID.set(dictGet(g, "id", nil), g)
	}

	var effectiveGroups []any
	for _, group := range iterate(allGroups) {
		if !truthy(dictGet(group, "enabled", true)) {
			continue
		}
		if e := effectiveLoonGroup(group, config); e != nil {
			effectiveGroups = append(effectiveGroups, e)
		}
	}

	var usedNodeIDs []any
	for _, group := range effectiveGroups {
		for _, ref := range loonGroupMemberRefs(group) {
			if ref.kind == "node" && !listContains(usedNodeIDs, ref.id) {
				usedNodeIDs = append(usedNodeIDs, ref.id)
			}
		}
	}

	proxies := []string{"[Proxy]"}
	nodeNames := newDict()
	for _, node := range iterate(dictGet(config, "nodes", []any{})) {
		if !truthy(dictGet(node, "enabled", true)) || !listContains(usedNodeIDs, dictGet(node, "id", nil)) {
			continue
		}
		line := convertNodeToLoon(node.(*jsonx.Object))
		if line != "" {
			proxies = append(proxies, line)
			before, _, _ := strings.Cut(line, " = ")
			nodeNames.set(dictItem(node, "id"), before)
		} else {
			loonLog.Warning("Loon 不支持该节点类型，已跳过: " + fstr(dictGet(node, "name", nil)))
		}
	}
	sections = append(sections, strings.Join(proxies, "\n"))

	var reserved []string
	for _, name := range nodeNames.list() {
		reserved = append(reserved, name.(string))
	}
	for _, g := range iterate(allGroups) {
		reserved = append(reserved, sanitizeLoonName(dictGet(g, "name", nil)))
	}
	allocator := newNameAllocator(reserved)

	// Keep each dimension separate: resource IDs and regexes may themselves
	// contain any delimiter chosen for a concatenated cache key.
	type remoteKey struct{ kind, resourceID, excludeRegex string }
	remoteNames := make(map[remoteKey]string)
	remoteLines := []string{"[Remote Proxy]"}
	remoteName := func(kind string, resourceID any, excludeRegex string) string {
		cacheKey := remoteKey{kind, key(resourceID), excludeRegex}
		if name, ok := remoteNames[cacheKey]; ok {
			return name
		}
		var resources any
		var path, suffix string
		if kind == "subscription" {
			resources = dictGet(config, "subscriptions", []any{})
			path = "/subscriptions/" + fstr(resourceID) + "/proxies"
			suffix = "订阅"
		} else {
			resources = dictGet(config, "subscription_aggregations", []any{})
			path = "/aggregations/" + fstr(resourceID) + "/provider"
			suffix = "聚合"
		}
		var resource any
		for _, r := range iterate(resources) {
			if eq(dictGet(r, "id", nil), resourceID) && truthy(dictGet(r, "enabled", true)) {
				resource = r
				break
			}
		}
		if !truthy(resource) {
			remoteNames[cacheKey] = ""
			return ""
		}
		name := allocator.allocate(dictGet(resource, "name", ""), suffix)
		query := [][2]any{{"format", "loon"}}
		if excludeRegex != "" {
			query = append(query, [2]any{"exclude_regex", excludeRegex})
		}
		if truthy(token) {
			query = append(query, [2]any{"token", token})
		}
		url := appendURLQuery(effective+profileAPIPath(config, path), query)
		remoteLines = append(remoteLines, name+" = "+url+",udp=default,block-quic=default,fast-open=default,"+
			"vmess-aead=default,skip-cert-verify=default,enabled=true")
		remoteNames[cacheKey] = name
		return name
	}

	filterLines := []string{"[Remote Filter]"}
	groupMembers := newDict()
	for _, group := range effectiveGroups {
		var members []loonMember
		for _, ref := range loonGroupMemberRefs(group) {
			switch {
			case ref.kind == "builtin":
				members = append(members, loonMember{name: ref.id.(string)})
			case ref.kind == "node" && nodeNames.has(ref.id):
				members = append(members, loonMember{name: nodeNames.item(ref.id).(string)})
			case ref.kind == "strategy" && groupsByID.has(ref.id) && !eq(ref.id, dictGet(group, "id", nil)):
				members = append(members, loonMember{ref: ref.id, isRef: true})
			}
		}
		_, aggregations, _, subscriptions := loonGroupSources(group)
		for _, source := range []struct {
			kind   string
			ids    any
			prefix string
		}{{"subscription", subscriptions, ""}, {"aggregation", aggregations, "aggregation_"}} {
			regex := pyStrip(or2(dictGet(group, source.prefix+"regex", nil), ""))
			exclude := pyStrip(or2(dictGet(group, source.prefix+"exclude_regex", nil), ""))
			var remotes []any
			for _, id := range iterate(source.ids) {
				if name := remoteName(source.kind, id, exclude); name != "" {
					remotes = append(remotes, name)
				}
			}
			if len(remotes) == 0 {
				continue
			}
			if regex != "" {
				suffix := "筛选"
				if source.kind != "subscription" {
					suffix = "聚合筛选"
				}
				filterName := allocator.allocate(fstr(dictItem(group, "name"))+"-"+suffix, suffix)
				escaped := strings.ReplaceAll(regex, `"`, `\"`)
				filterLines = append(filterLines, filterName+" = NameRegex,"+joinAny(",", remotes)+`,FilterKey="`+escaped+`"`)
				members = append(members, loonMember{name: filterName})
			} else {
				for _, r := range remotes {
					members = append(members, loonMember{name: r.(string)})
				}
			}
		}
		groupMembers.set(dictItem(group, "id"), loonGroupEntry{group, members})
	}

	// Remove groups without usable members until nothing changes (the
	// greatest fixed point; Python's set order only changes the log order).
	emitted := newSet()
	for _, id := range groupMembers.keys {
		emitted.add(groupMembers.orig[id])
	}
	for changed := true; changed; {
		changed = false
		for _, k := range append([]string(nil), emitted.keys...) {
			groupID := emitted.values[k]
			entry := groupMembers.item(groupID).(loonGroupEntry)
			usable := false
			for _, m := range entry.members {
				if !m.isRef || emitted.has(m.ref) {
					usable = true
					break
				}
			}
			if !usable {
				loonLog.Debug("Loon 策略组没有可用成员，跳过: " + fstr(dictGet(entry.group, "name", nil)))
				emitted.remove(groupID)
				changed = true
			}
		}
	}

	proxyGroups := []string{"[Proxy Group]"}
	for _, group := range effectiveGroups {
		if !emitted.has(dictItem(group, "id")) {
			continue
		}
		entry := groupMembers.item(dictItem(group, "id")).(loonGroupEntry)
		var names []string
		for _, m := range entry.members {
			if m.isRef {
				if emitted.has(m.ref) {
					names = append(names, sanitizeLoonName(dictItem(groupsByID.item(m.ref), "name")))
				}
			} else {
				names = append(names, m.name)
			}
		}
		if line := convertProxyGroupToLoon(group, names); line != "" {
			proxyGroups = append(proxyGroups, line)
		}
	}
	if len(proxyGroups) == 1 && nodeNames.len() > 0 {
		var all []string
		for _, name := range nodeNames.list() {
			all = append(all, name.(string))
		}
		joined := strings.Join(all, ",")
		proxyGroups = append(proxyGroups, "Proxy = select,"+joined)
		proxyGroups = append(proxyGroups, "Auto = url-test,"+joined+",url="+LoonDefaultTestURL+",interval=600")
	}

	sections = append(sections, strings.Join(remoteLines, "\n"), strings.Join(filterLines, "\n"),
		strings.Join(proxyGroups, "\n"))

	ruleLines, remoteRules := buildLoonRules(config, effective)
	sections = append(sections, strings.Join(ruleLines, "\n"), strings.Join(remoteRules, "\n"))

	for _, s := range customSections {
		switch s.name {
		case "General", "Proxy", "Remote Proxy", "Remote Filter", "Proxy Group", "Rule", "Remote Rule":
		default:
			sections = append(sections, s.text)
		}
	}

	validateRulePolicies(config, policyNames(append(append([]string{}, proxies[1:]...), proxyGroups[1:]...)), "loon",
		LoonBuiltinPolicies)

	return "# Generated by ConfigFlow for Loon\n\n" + strings.Join(sections, "\n\n") + "\n"
}

// BuildLoonRules is _build_rules(config_data, effective_base_url): the
// [Rule] and [Remote Rule] sections (with their header lines).
func BuildLoonRules(config *jsonx.Object, effectiveBaseURL string) (ruleLines, remoteRules []string, err error) {
	defer catch(&err)
	must(validateGenerationRules(config))
	ruleLines, remoteRules = buildLoonRules(config, effectiveBaseURL)
	return
}

func buildLoonRules(config *jsonx.Object, effective string) ([]string, []string) {
	lines := []string{"[Rule]"}
	remote := []string{"[Remote Rule]"}
	finalRule := ""

	urls := newDict()
	for _, lib := range iterate(dictGet(config, "rule_library", []any{})) {
		if truthy(dictGet(lib, "enabled", true)) {
			name := dictGet(lib, "name", "")
			urls.set(name, or2(dictGet(lib, "url", ""), ""))
		}
	}

	for _, item := range mustValue(EmittedRules(config)) {
		if !truthy(dictGet(item, "enabled", true)) || !truthy(dictGet(item, "library_enabled", true)) {
			continue
		}
		itemType := dictGet(item, "itemType", "")
		if finalRule != "" {
			comment := AfterMatchComment(item.(*jsonx.Object))
			if eq(itemType, "ruleset") {
				remote = append(remote, comment)
			} else {
				lines = append(lines, comment)
			}
			continue
		}
		if eq(itemType, "rule") {
			ruleType := dictItem(item, "rule_type")
			value := dictGet(item, "value", "")
			policy := dictItem(item, "policy")
			if eq(ruleType, "MATCH") {
				finalRule = "FINAL," + fstr(policy)
				continue
			}
			if hasPortListAny(ruleType, value) {
				lines = append(lines, "# Loon 不支持一条规则写多个端口，已忽略: "+fstr(ruleType)+","+fstr(value)+","+fstr(policy))
				continue
			}
			ruleType, value = networkAsProtocolAny(ruleType, value)
			if inStrings(ruleType, "AND", "OR", "NOT") {
				loonValue := value.(string)
				for _, m := range loonRuleTypeMap {
					loonValue = strings.ReplaceAll(loonValue, m[0], m[1])
				}
				line := ruleType.(string) + "," + loonValue + "," + fstr(policy)
				unsupported := false
				for _, sub := range loonSubTypeRe.FindAllStringSubmatch(loonValue, -1) {
					if !containsString(LoonRuleTypes, sub[1]) {
						unsupported = true
					}
				}
				if unsupported {
					lines = append(lines, "# Loon 不支持的规则类型，已忽略: "+line)
				} else {
					lines = append(lines, line)
				}
				continue
			}
			key(ruleType) // dict.get hashes the key
			loonType := ruleType
			if s, ok := ruleType.(string); ok {
				for _, m := range loonRuleTypeMap {
					if s == m[0] {
						loonType = m[1]
					}
				}
			}
			line := fstr(loonType) + "," + fstr(value) + "," + fstr(policy)
			if truthy(dictGet(item, "no_resolve", false)) {
				line += ",no-resolve"
			}
			if s, ok := loonType.(string); ok && containsString(LoonRuleTypes, s) {
				lines = append(lines, line)
			} else {
				lines = append(lines, "# Loon 不支持的规则类型，已忽略: "+line)
			}
		} else if eq(itemType, "ruleset") {
			policy := dictGet(item, "policy", "Proxy")
			name := dictGet(item, "name", "")
			url := dictGet(item, "url", "")
			if !truthy(url) {
				url = urls.get(name, "")
			}
			originalURL := or2(urls.get(name, url), url)
			if truthy(url) && startsWith(url, "/") && effective != "" {
				url = effective + fstr(url)
			}
			if !truthy(url) {
				continue
			}
			if format := rulesetFormatAny(item.(*jsonx.Object), config, originalURL); format == "yaml" || format == "mrs" {
				remote = append(remote, "# Loon 不支持 YAML/MRS 规则集，已忽略: "+fstr(name)+" "+fstr(url))
				continue
			}
			tag := sanitizeLoonName(name)
			if tag == "" {
				tag = "rule"
			}
			remote = append(remote, loonRulesetURL(url.(string))+",policy="+fstr(policy)+",tag="+tag+",enabled=true")
		}
	}
	if finalRule != "" {
		lines = append(lines, finalRule)
	}
	return lines, remote
}

// loonRulesetURL is _loon_ruleset_url: blackmatrix7/ios_rule_script's Clash
// rules contain types Loon does not support; use its Loon version.
func loonRulesetURL(url string) string {
	if strings.Contains(url, "blackmatrix7/ios_rule_script") && strings.Contains(url, "/rule/Clash/") {
		return strings.ReplaceAll(url, "/rule/Clash/", "/rule/Loon/")
	}
	return url
}

// ConvertProxyGroupToLoon is convert_proxy_group_to_loon(group, members);
// "" stands for None.
func ConvertProxyGroupToLoon(group any, members []string) (line string, err error) {
	defer catch(&err)
	return convertProxyGroupToLoon(group, members), nil
}

func convertProxyGroupToLoon(group any, members []string) string {
	if len(members) == 0 {
		return ""
	}
	name := sanitizeLoonName(dictItem(group, "name"))
	groupType := dictGet(group, "type", nil)
	memberList := strings.Join(members, ",")
	url := or2(dictGet(group, "url", nil), LoonDefaultTestURL)
	interval := or2(dictGet(group, "interval", nil), jsonx.Int(600))
	options := ",url=" + fstr(url) + ",interval=" + fstr(interval)
	switch {
	case eq(groupType, "select"):
		return name + " = select," + memberList
	case eq(groupType, "url-test"):
		return name + " = url-test," + memberList + options + ",tolerance=100"
	case eq(groupType, "fallback"):
		return name + " = fallback," + memberList + options + ",max-timeout=3000"
	case eq(groupType, "load-balance"):
		strategy := or2(dictGet(group, "strategy", nil), "")
		key(strategy)
		algorithm := "random"
		if s, ok := strategy.(string); ok {
			if a, found := loadBalanceAlgorithms[s]; found {
				algorithm = a
			}
		}
		return name + " = load-balance," + memberList + options + ",algorithm=" + algorithm + ",max-timeout=3000"
	}
	return ""
}

// ConvertProxiesToLoonText is convert_proxies_to_loon_text(proxies): one Loon
// line per Mihomo proxy for [Remote Proxy] subscriptions. toNodes is
// sub_store_client.proxies_to_nodes (substore.ProxiesToNodes). Unsupported
// nodes are skipped; nodes that fail to convert are logged and skipped.
func ConvertProxiesToLoonText(proxies []any, toNodes func([]any) ([]any, error)) (text string, err error) {
	defer catch(&err)
	for _, proxy := range proxies {
		if dictGet(proxy, "dialer-proxy", nil) != nil {
			raise(dialerError("Loon 订阅暂不支持 dialer-proxy，不能静默移除拨号配置"))
		}
	}
	var lines []string
	for _, item := range mustValue(toNodes(proxies)) {
		node := item.(*jsonx.Object)
		line, convErr := ConvertNodeToLoon(node)
		if convErr != nil {
			loonLog.Warning("转换节点到 Loon 格式失败: " + fstr(node.GetOr("name", "?")) + ", 错误: " + convErr.Error())
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), nil
}
