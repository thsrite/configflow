package convert

// Port of backend/converters/surge.py (C19): the Surge configuration of a
// profile, byte-identical to Python's. Manual node strings are parsed with
// the built-in subs.ParseURIList exactly as Python's parse_uri_list
// (unknown schemes are dropped silently; switching Surge/Loon to Sub-Store
// is a follow-up after parity, packages.md).

import (
	"context"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/rules"
)

var surgeLog = logx.Get("backend.converters.surge")

// SurgeBuiltinPolicies are validate_rule_policies' Surge built-ins.
var SurgeBuiltinPolicies = []string{"DIRECT", "REJECT", "REJECT-DROP", "REJECT-TINYGIF", "REJECT-NO-DROP",
	"CELLULAR", "CELLULAR-ONLY", "HYBRID", "NO-HYBRID"}

// SurgeDefaultGeneral is the [General] section used without a custom one.
var SurgeDefaultGeneral = []string{
	"[General]",
	"loglevel = notify",
	"internet-test-url = https://www.gstatic.com/generate_204",
	"proxy-test-url = https://www.gstatic.com/generate_204",
	"test-timeout = 3",
	"skip-proxy = localhost, *.local, injections.adguard.org, local.adguard.org, captive.apple.com, guzzoni.apple.com, 0.0.0.0/8, 10.0.0.0/8, 17.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.0.0.0/24, 192.0.2.0/24, 192.168.0.0/16, 192.88.99.0/24, 198.18.0.0/15, 198.51.100.0/24, 203.0.113.0/24, 224.0.0.0/4, 240.0.0.0/4, 255.255.255.255/32",
	"dns-server = 223.5.5.5, 119.29.29.29, system",
	"ipv6 = true",
	"allow-wifi-access = true",
	"wifi-access-http-port = 6152",
	"wifi-access-socks5-port = 6153",
	"http-listen = 0.0.0.0:6152",
	"socks5-listen = 0.0.0.0:6153",
	"exclude-simple-hostnames = true",
}

const defaultTestURL = "https://www.gstatic.com/generate_204"

// surgeRuleTypeMap is surge_rule_type_map (mihomo/Clash → Surge), in order.
var surgeRuleTypeMap = [][2]string{{"SRC-IP-CIDR", "SRC-IP"}, {"DST-PORT", "DEST-PORT"}}

// GenerateSurgeConfig is generate_surge_config(config_data, base_url). Errors
// are the exceptions Python raises: *DialerReferenceError,
// StrategyReferenceError, *HookMissingError (Sub-Store not wired) and
// *PyError for crashes on malformed values. Sub-Store conversions (made
// while rejecting dialer topologies) reuse the scope installed by
// WithNodeConversions.
func GenerateSurgeConfig(ctx context.Context, config *jsonx.Object, baseURL string) (out string, err error) {
	defer catch(&err)
	return generateSurge(ctx, config, baseURL), nil
}

// usedSources is the follow-mode source selection of the collection loops.
func surgeGroupSources(config *jsonx.Object, group any, ownOrder bool) (manual, aggregations, subscriptions, order any, ok bool) {
	follow := dictGet(group, "follow_group", nil)
	source := group
	if truthy(follow) {
		followed := nextMatch(dictGet(config, "proxy_groups", []any{}), "id", follow)
		if !truthy(followed) {
			return nil, nil, nil, nil, false
		}
		source = followed
	}
	manual = dictGet(source, "manual_nodes", []any{})
	aggregations = dictGet(source, "aggregations", []any{})
	subscriptions = dictGet(source, "subscriptions", []any{})
	if ownOrder {
		order = group
	} else {
		order = source
	}
	return manual, aggregations, subscriptions, order, true
}

func generateSurge(ctx context.Context, config *jsonx.Object, baseURL string) string {
	must(ValidateDialers(config))
	must(RejectSurgeDialers(ctx, config, "Surge", clientConverter()))
	splitRulesAndRulesets(config)

	effective := effectiveBaseURL(config, baseURL)
	var sections []string
	var wireguardSections []string

	surgeSettings := dictGet(config, "surge", jsonx.NewObject())
	custom := dictGet(surgeSettings, "custom_config", "")

	smartGroups := dictGet(surgeSettings, "smart_groups", []any{})
	smartMap := newDict()
	for _, sg := range iterate(smartGroups) {
		groupID := dictItem(sg, "group_id")
		priority := dictGet(sg, "policy_priority", "")
		smartMap.set(groupID, priority)
	}

	customSections := parseCustomSections(custom)
	general, _ := sectionText(customSections, "General")
	if general == "" {
		general = strings.Join(SurgeDefaultGeneral, "\n")
	}
	if fallback := mustValue(UDPFallback(config)); fallback != "" {
		general = SetGeneralOption(general, "udp-policy-not-supported-behaviour", fallback)
	}
	sections = append(sections, general)

	usedNodeIDs, usedSubscriptionIDs, usedAggregationIDs := newSet(), newSet(), newSet()
	surgeLog.Debug("开始收集被策略组使用的节点、订阅和聚合...")
	for _, group := range iterate(dictGet(config, "proxy_groups", []any{})) {
		if !truthy(dictGet(group, "enabled", true)) {
			continue
		}
		manual, aggregations, subscriptions, _, ok := surgeGroupSources(config, group, true)
		if !ok {
			continue
		}
		for _, nodeID := range iterate(manual) {
			if !inStrings(nodeID, "DIRECT", "REJECT") {
				usedNodeIDs.add(nodeID)
			}
		}
		for _, subID := range iterate(subscriptions) {
			usedSubscriptionIDs.add(subID)
		}
		if truthy(aggregations) {
			for _, aggID := range iterate(aggregations) {
				usedAggregationIDs.add(aggID)
			}
		}
		if order := dictGet(group, "proxies_order", []any{}); truthy(order) {
			for _, item := range iterate(order) {
				if eq(dictGet(item, "type", nil), "node") {
					nodeID := dictGet(item, "id", nil)
					if !inStrings(nodeID, "DIRECT", "REJECT") {
						usedNodeIDs.add(nodeID)
					}
				}
			}
		}
	}

	directlySelected := newSet()
	for _, group := range iterate(dictGet(config, "proxy_groups", []any{})) {
		if !truthy(dictGet(group, "enabled", true)) {
			continue
		}
		manual, _, _, source, ok := surgeGroupSources(config, group, false)
		if !ok {
			continue
		}
		order := dictGet(source, "proxies_order", []any{})
		for _, nodeID := range iterate(manual) {
			if !inStrings(nodeID, "DIRECT", "REJECT") {
				directlySelected.add(nodeID)
			}
		}
		if truthy(order) {
			for _, item := range iterate(order) {
				if eq(dictGet(item, "type", nil), "node") {
					nodeID := dictGet(item, "id", nil)
					if !inStrings(nodeID, "DIRECT", "REJECT") {
						directlySelected.add(nodeID)
					}
				}
			}
		}
	}
	surgeLog.Debug("策略组直接选择的节点ID: " + directlySelected.repr())

	onlyInAggregations := newSet()
	for _, agg := range iterate(dictGet(config, "subscription_aggregations", []any{})) {
		if truthy(dictGet(agg, "enabled", true)) && usedAggregationIDs.has(dictGet(agg, "id", nil)) {
			aggNodes := dictGet(agg, "nodes", []any{})
			for _, nodeID := range iterate(aggNodes) {
				if !directlySelected.has(nodeID) {
					onlyInAggregations.add(nodeID)
				}
			}
			surgeLog.Debug("聚合 '" + fstr(dictGet(agg, "name", nil)) + "' 包含节点: " + fstr(aggNodes))
		}
	}
	surgeLog.Debug("仅通过聚合使用的节点ID（将被排除）: " + onlyInAggregations.repr())

	proxies := []string{"[Proxy]"}
	surgeLog.Debug("收集到的被使用节点ID: " + usedNodeIDs.repr())
	nodeCount := pyLen(dictGet(config, "nodes", []any{}))
	surgeLog.Debug("开始生成 Surge proxies，总节点数: " + jsonx.Repr(jsonx.Int(int64(nodeCount))))
	for _, node := range iterate(dictGet(config, "nodes", []any{})) {
		if !truthy(dictGet(node, "enabled", true)) {
			surgeLog.Debug("跳过禁用节点: " + fstr(dictGet(node, "name", nil)))
			continue
		}
		if !usedNodeIDs.has(dictGet(node, "id", nil)) {
			surgeLog.Debug("跳过未被策略组使用的节点: " + fstr(dictGet(node, "name", nil)) + " (id: " + fstr(dictGet(node, "id", nil)) + ")")
			continue
		}
		if onlyInAggregations.has(dictGet(node, "id", nil)) {
			surgeLog.Debug("跳过仅通过聚合使用的节点: " + fstr(dictGet(node, "name", nil)) + " (id: " + fstr(dictGet(node, "id", nil)) + ")")
			continue
		}
		line, wireguard := convertNodeToSurge(node.(*jsonx.Object))
		if line != "" {
			proxies = append(proxies, line)
			surgeLog.Debug("添加节点到 Surge proxies: " + fstr(dictGet(node, "name", nil)))
		}
		if wireguard != "" {
			wireguardSections = append(wireguardSections, wireguard)
		}
	}
	surgeLog.Debug("最终生成的 Surge proxies 数量: " + jsonx.Repr(jsonx.Int(int64(len(proxies)-1))))
	sections = append(sections, strings.Join(proxies, "\n"))

	proxyGroups := []string{"[Proxy Group]"}
	for _, group := range iterate(dictGet(config, "proxy_groups", []any{})) {
		if !truthy(dictGet(group, "enabled", true)) {
			continue
		}
		if line := convertProxyGroupToSurge(group, config, baseURL, smartMap); line != "" {
			proxyGroups = append(proxyGroups, line)
		}
	}
	if len(proxyGroups) == 1 {
		var names []any
		for _, node := range iterate(dictGet(config, "nodes", []any{})) {
			if truthy(dictGet(node, "enabled", true)) {
				names = append(names, dictItem(node, "name"))
			}
		}
		if len(names) > 0 {
			joined := joinAny(", ", names)
			proxyGroups = append(proxyGroups, "Proxy = select, "+joined)
			joined = joinAny(", ", names)
			proxyGroups = append(proxyGroups, "Auto = url-test, "+joined+", url = https://www.gstatic.com/generate_204, interval = 300")
		}
	}
	sections = append(sections, strings.Join(proxyGroups, "\n"))

	sections = append(sections, strings.Join(surgeRules(config, effective), "\n"))
	sections = append(sections, wireguardSections...)
	for _, s := range customSections {
		switch s.name {
		case "General", "Proxy", "Proxy Group", "Rule":
		default:
			sections = append(sections, s.text)
		}
	}

	validateRulePolicies(config, policyNames(append(append([]string{}, proxies[1:]...), proxyGroups[1:]...)), "surge",
		SurgeBuiltinPolicies)

	output := strings.Join(sections, "\n\n")
	token := dictGet(dictGet(config, "system_config", jsonx.NewObject()), "config_token", "")
	surgeURL := effective + configAPIPath(config, "surge")
	if truthy(token) {
		surgeURL = appendURLQuery(surgeURL, [][2]any{{"token", token}})
	}
	return "#!MANAGED-CONFIG " + surgeURL + " interval=86400 strict=true\n\n" + output
}

// surgeRules is the [Rule] section of generate_surge_config.
func surgeRules(config *jsonx.Object, effective string) []string {
	lines := []string{"[Rule]"}
	behaviors, urls := newDict(), newDict()
	for _, lib := range iterate(dictGet(config, "rule_library", []any{})) {
		if truthy(dictGet(lib, "enabled", true)) {
			name := dictGet(lib, "name", "")
			behavior := dictGet(lib, "behavior", "")
			behaviors.set(name, behavior)
			url := dictGet(lib, "url", "")
			urls.set(name, url)
		}
	}

	afterMatch := false
	for _, item := range mustValue(EmittedRules(config)) {
		if !truthy(dictGet(item, "enabled", true)) || !truthy(dictGet(item, "library_enabled", true)) {
			continue
		}
		itemType := dictGet(item, "itemType", "")
		if afterMatch {
			lines = append(lines, AfterMatchComment(item.(*jsonx.Object)))
			continue
		}
		if eq(itemType, "rule") {
			ruleType := dictItem(item, "rule_type")
			value := dictGet(item, "value", "")
			policy := dictItem(item, "policy")
			switch {
			case eq(ruleType, "MATCH"):
				lines = append(lines, "FINAL,"+fstr(policy))
				afterMatch = true
			case eq(ruleType, "RULE-SET"):
				lines = append(lines, "RULE-SET,"+fstr(value)+","+fstr(policy))
			case hasPortListAny(ruleType, value):
				lines = append(lines, "# Surge 不支持一条规则写多个端口，已忽略: "+fstr(ruleType)+","+fstr(value)+","+fstr(policy))
			case inStrings(ruleType, "AND", "OR", "NOT"):
				surgeValue := pyReplace(value, "),(", "), (")
				for _, m := range surgeRuleTypeMap {
					surgeValue = strings.ReplaceAll(surgeValue, m[0], m[1])
				}
				_, converted := networkAsProtocolAny(ruleType, surgeValue)
				lines = append(lines, fstr(ruleType)+","+fstr(converted)+","+fstr(policy))
			default:
				surgeType, converted := networkAsProtocolAny(ruleType, value)
				key(surgeType) // dict.get hashes the key
				if s, ok := surgeType.(string); ok {
					for _, m := range surgeRuleTypeMap {
						if s == m[0] {
							surgeType = m[1]
						}
					}
				}
				line := fstr(surgeType) + "," + fstr(converted) + "," + fstr(policy)
				if truthy(dictGet(item, "no_resolve", false)) {
					line += ",no-resolve"
				}
				lines = append(lines, line)
			}
		} else if eq(itemType, "ruleset") {
			policy := dictGet(item, "policy", "Proxy")
			url := dictGet(item, "url", "")
			name := dictGet(item, "name", "")
			originalURL := urls.get(name, url)
			if truthy(url) && startsWith(url, "/") && effective != "" {
				url = effective + fstr(url)
			}
			if !truthy(url) && urls.has(name) {
				url = urls.item(name)
				if truthy(url) && startsWith(url, "/") && effective != "" {
					url = effective + fstr(url)
				}
			}
			var options []string
			if truthy(dictGet(item, "no_resolve", false)) {
				options = append(options, "no-resolve")
			}
			if rulesetFormatAny(item.(*jsonx.Object), config, originalURL) == "yaml" {
				options = append(options, "rule-set-format=yaml")
			}
			if len(options) > 0 {
				lines = append(lines, "RULE-SET,"+fstr(url)+","+fstr(policy)+","+strings.Join(options, ","))
			} else {
				lines = append(lines, "RULE-SET,"+fstr(url)+","+fstr(policy))
			}
		}
	}
	return lines
}

// ConvertNodeToSurge is convert_node_to_surge(node): the [Proxy] line and,
// for WireGuard, its [WireGuard …] section; "" stands for None.
func ConvertNodeToSurge(node *jsonx.Object) (line, wireguard string, err error) {
	defer catch(&err)
	line, wireguard = convertNodeToSurge(node)
	return line, wireguard, nil
}

// surgeParts collects the comma separated fields of a Surge proxy line:
// `', '.join(parts)` needs str items (server may be anything).
type surgeParts []any

func (p *surgeParts) add(v any) { *p = append(*p, v) }

func (p surgeParts) join() string { return joinAny(", ", p) }

func convertNodeToSurge(node *jsonx.Object) (string, string) {
	isRaw := false
	if node.Has("proxy_string") {
		parsed, raw, ok := resolveProxyString(node)
		if !ok {
			return "", ""
		}
		node, isRaw = parsed, raw
	}
	nodeType := pyLower(dictGet(node, "type", ""))
	if nodeType == "" {
		return "", ""
	}
	name := dictItem(node, "name")
	server := dictGet(node, "server", "")
	port := dictGet(node, "port", jsonx.Int(0))
	var params any = node
	if !isRaw {
		params = dictGet(node, "params", jsonx.NewObject())
	}
	get := func(k string, def any) any { return dictGet(params, k, def) }
	udp := func() bool { return truthy(get("udp", nil)) || truthy(get("udp-relay", nil)) }

	switch nodeType {
	case "ss":
		cipher := get("cipher", "aes-256-gcm")
		password := get("password", "")
		parts := surgeParts{fstr(name) + " = ss", server, fstr(port), "encrypt-method=" + fstr(cipher), "password=" + fstr(password)}
		if udp() {
			parts.add("udp-relay=true")
		}
		if truthy(get("test-url", nil)) {
			parts.add("test-url=" + fstr(dictItem(params, "test-url")))
		}
		return parts.join(), ""

	case "vmess":
		uuid := get("uuid", "")
		tls := truthy(get("tls", false))
		network := get("network", "tcp")
		parts := surgeParts{fstr(name) + " = vmess", server, fstr(port), "username=" + fstr(uuid)}
		if tls {
			parts.add("tls=true")
		}
		if eq(network, "ws") {
			wsOpts := get("ws-opts", jsonx.NewObject())
			path := dictGet(wsOpts, "path", "/")
			host := dictGet(dictGet(wsOpts, "headers", jsonx.NewObject()), "Host", "")
			parts.add("ws=true")
			parts.add("ws-path=" + fstr(path))
			if truthy(host) {
				parts.add("ws-headers=Host:" + fstr(host))
			}
		}
		if udp() {
			parts.add("udp-relay=true")
		}
		return parts.join(), ""

	case "trojan", "hysteria2", "anytls":
		password := get("password", "")
		sni := get("sni", "")
		skipCert := get("skip-cert-verify", false)
		parts := surgeParts{fstr(name) + " = " + nodeType, server, fstr(port), "password=" + fstr(password)}
		if truthy(sni) {
			parts.add("sni=" + fstr(sni))
		}
		if truthy(skipCert) {
			parts.add("skip-cert-verify=true")
		}
		if udp() {
			parts.add("udp-relay=true")
		}
		return parts.join(), ""

	case "http", "https":
		username := get("username", "")
		password := get("password", "")
		if truthy(username) && truthy(password) {
			return fstr(name) + " = " + nodeType + ", " + fstr(server) + ", " + fstr(port) + ", " + fstr(username) + ", " + fstr(password), ""
		}
		return fstr(name) + " = " + nodeType + ", " + fstr(server) + ", " + fstr(port), ""

	case "snell":
		psk := or2(get("psk", ""), get("password", ""))
		version := get("version", jsonx.Int(4))
		parts := surgeParts{fstr(name) + " = snell", server, fstr(port), "psk=" + fstr(psk)}
		if truthy(version) {
			parts.add("version=" + fstr(version))
		}
		if truthy(get("reuse", nil)) {
			parts.add("reuse=true")
		}
		if truthy(get("udp-relay", nil)) || truthy(get("udp", nil)) {
			parts.add("udp-relay=true")
		}
		var obfsOpts any = jsonx.NewObject()
		if isDict(get("obfs-opts", nil)) {
			obfsOpts = get("obfs-opts", nil)
		}
		obfs := or2(dictGet(obfsOpts, "mode", nil), get("obfs", ""))
		obfsHost := or2(dictGet(obfsOpts, "host", nil), get("obfs-host", ""))
		if eq(obfs, "shadow-tls") {
			parts.add("shadow-tls-password=" + fstr(dictGet(obfsOpts, "password", "")))
			if truthy(obfsHost) {
				parts.add("shadow-tls-sni=" + fstr(obfsHost))
			}
			if truthy(dictGet(obfsOpts, "version", nil)) {
				parts.add("shadow-tls-version=" + fstr(dictItem(obfsOpts, "version")))
			}
		} else if inStrings(obfs, "http", "tls") {
			parts.add("obfs=" + fstr(obfs))
			if truthy(obfsHost) {
				parts.add("obfs-host=" + fstr(obfsHost))
			}
		}
		return parts.join(), ""

	case "tuic", "tuic-v5":
		uuid := get("uuid", "")
		password := get("password", "")
		token := get("token", "")
		parts := surgeParts{fstr(name) + " = tuic-v5", server, fstr(port)}
		if truthy(uuid) {
			parts.add("uuid=" + fstr(uuid))
		}
		if truthy(password) {
			parts.add("password=" + fstr(password))
		} else if truthy(token) {
			parts.add("token=" + fstr(token))
		}
		if alpn := get("alpn", "h3"); truthy(alpn) {
			parts.add("alpn=" + fstr(alpn))
		}
		if sni := get("sni", ""); truthy(sni) {
			parts.add("sni=" + fstr(sni))
		}
		if truthy(get("skip-cert-verify", false)) {
			parts.add("skip-cert-verify=true")
		}
		if truthy(get("udp-relay", nil)) || truthy(get("udp", nil)) {
			parts.add("udp-relay=true")
		}
		return parts.join(), ""

	case "vless":
		uuid := get("uuid", "")
		get("flow", "")
		tls := truthy(get("tls", false)) || eq(get("security", ""), "tls")
		network := get("network", "tcp")
		parts := surgeParts{fstr(name) + " = vmess", server, fstr(port), "username=" + fstr(uuid)}
		if tls {
			parts.add("tls=true")
			if sni := or2(get("sni", ""), get("servername", "")); truthy(sni) {
				parts.add("sni=" + fstr(sni))
			}
		}
		if eq(network, "ws") {
			wsOpts := or2(get("ws-opts", jsonx.NewObject()), get("ws-config", jsonx.NewObject()))
			path := or2(dictGet(wsOpts, "path", "/"), get("path", "/"))
			host := or2(dictGet(dictGet(wsOpts, "headers", jsonx.NewObject()), "Host", ""), get("host", ""))
			parts.add("ws=true")
			parts.add("ws-path=" + fstr(path))
			if truthy(host) {
				parts.add("ws-headers=Host:" + fstr(host))
			}
		} else if eq(network, "grpc") {
			service := or2(dictGet(get("grpc-opts", jsonx.NewObject()), "grpc-service-name", ""), get("serviceName", ""))
			if truthy(service) {
				parts.add("grpc-service-name=" + fstr(service))
			}
		}
		if truthy(get("skip-cert-verify", false)) {
			parts.add("skip-cert-verify=true")
		}
		if udp() {
			parts.add("udp-relay=true")
		}
		return parts.join(), ""

	case "wireguard", "wg":
		defaultSection := strings.ReplaceAll(pyReplace(name, " ", "-"), "_", "-")
		sectionName := get("section-name", defaultSection)
		lineParts := []string{fstr(name) + " = wireguard", "section-name = " + fstr(sectionName)}
		mtu := get("mtu", nil)
		if truthy(mtu) {
			lineParts = append(lineParts, "mtu="+fstr(mtu))
		}
		proxyLine := strings.Join(lineParts, ", ")

		section := []string{"[WireGuard " + fstr(sectionName) + "]"}
		if privateKey := or2(get("private-key", ""), get("privateKey", "")); truthy(privateKey) {
			section = append(section, "private-key = "+fstr(privateKey))
		}
		selfIP := or2(or2(get("self-ip", ""), get("ip", "")), get("address", ""))
		if truthy(selfIP) {
			section = append(section, "self-ip = "+fstr(selfIP))
		}
		if dns := or2(get("dns", ""), get("dns-server", "")); truthy(dns) {
			if isList(dns) {
				dns = joinAny(", ", dns.([]any))
			}
			section = append(section, "dns-server = "+fstr(dns))
		}
		if truthy(mtu) {
			section = append(section, "mtu = "+fstr(mtu))
		}
		peerKey := or2(or2(get("public-key", ""), get("publicKey", "")), get("peer-public-key", ""))
		endpoint := get("endpoint", "")
		if !truthy(endpoint) {
			endpoint = fstr(server) + ":" + fstr(port)
		}
		allowedIPs := or2(get("allowed-ips", ""), get("allowed_ips", "0.0.0.0/0, ::/0"))
		if truthy(peerKey) {
			peer := []string{"public-key = " + fstr(peerKey), "endpoint = " + fstr(endpoint)}
			if isList(allowedIPs) {
				allowedIPs = joinAny(", ", allowedIPs.([]any))
			}
			peer = append(peer, `allowed-ips = "`+fstr(allowedIPs)+`"`)
			if keepalive := or2(get("keepalive", ""), get("persistent-keepalive", "")); truthy(keepalive) {
				peer = append(peer, "keepalive = "+fstr(keepalive))
			}
			if preshared := or2(get("preshared-key", ""), get("presharedKey", "")); truthy(preshared) {
				peer = append(peer, "preshared-key = "+fstr(preshared))
			}
			if clientID := or2(get("client-id", ""), get("reserved", "")); truthy(clientID) {
				peer = append(peer, "client-id = "+fstr(clientID))
			}
			section = append(section, "peer = ("+strings.Join(peer, ", ")+")")
		}
		return proxyLine, strings.Join(section, "\n")
	}
	return "", ""
}

// ConvertProxyGroupToSurge is convert_proxy_group_to_surge(group,
// config_data, base_url, smart_group_map); "" stands for None.
// smartGroups maps group ids to their policy priority (nil = None).
func ConvertProxyGroupToSurge(group any, config *jsonx.Object, baseURL string, smartGroups *jsonx.Object) (line string, err error) {
	defer catch(&err)
	var smart *pyDict
	if smartGroups != nil {
		smart = newDict()
		for k, v := range smartGroups.All() {
			smart.set(k, v)
		}
	}
	return convertProxyGroupToSurge(group, config, baseURL, smart), nil
}

// followedGroupView is the merged group of follow mode (Surge and Loon).
func followedGroupView(group, followed any, withStrategy bool) *jsonx.Object {
	merged := dictItemObject(group).Clone()
	merged.Set("type", dictItem(followed, "type"))
	for _, field := range []struct {
		key string
		def any
	}{{"manual_nodes", []any{}}, {"aggregations", []any{}}, {"include_groups", []any{}}, {"subscriptions", []any{}},
		{"regex", ""}, {"exclude_regex", ""}, {"aggregation_regex", ""}, {"aggregation_exclude_regex", ""},
		{"proxies_order", []any{}}, {"proxy_order", "nodes_first"}, {"url", nil}, {"interval", nil}} {
		merged.Set(field.key, dictGet(followed, field.key, field.def))
	}
	if withStrategy {
		merged.Set("strategy", dictGet(followed, "strategy", nil))
	}
	return merged
}

// dictItemObject is the `**group` of a dict display.
func dictItemObject(v any) *jsonx.Object {
	obj, ok := v.(*jsonx.Object)
	if !ok || obj == nil {
		raise(pyError("TypeError", "'"+typeName(v)+"' object is not a mapping"))
	}
	return obj
}

func convertProxyGroupToSurge(group any, config *jsonx.Object, baseURL string, smart *pyDict) string {
	effective := effectiveBaseURL(config, baseURL)
	name := dictItem(group, "name")
	groupType := dictItem(group, "type")

	var policyPriority any
	if smart != nil && smart.len() > 0 && smart.has(dictGet(group, "id", nil)) {
		policyPriority = smart.item(dictItem(group, "id"))
		groupType = "smart"
	}

	var manual, aggregations, includeGroups, subscriptions any
	if follow := dictGet(group, "follow_group", nil); truthy(follow) {
		followed := nextMatch(dictGet(config, "proxy_groups", []any{}), "id", follow)
		if !truthy(followed) {
			return ""
		}
		manual = dictGet(followed, "manual_nodes", []any{})
		aggregations = dictGet(followed, "aggregations", []any{})
		includeGroups = dictGet(followed, "include_groups", []any{})
		subscriptions = dictGet(followed, "subscriptions", []any{})
		group = followedGroupView(group, followed, false)
		if !eq(groupType, "smart") {
			groupType = dictItem(followed, "type")
		}
	} else {
		manual = dictGet(group, "manual_nodes", []any{})
		aggregations = dictGet(group, "aggregations", []any{})
		includeGroups = dictGet(group, "include_groups", []any{})
		subscriptions = dictGet(group, "subscriptions", []any{})
	}

	if !truthy(manual) && !truthy(aggregations) && !truthy(includeGroups) && !truthy(subscriptions) {
		source := dictGet(group, "source", "subscription")
		proxiesOld := dictGet(group, "proxies", []any{})
		switch {
		case eq(source, "subscription"):
			subscriptions = dictGet(group, "subscriptions", []any{})
		case eq(source, "node"):
			manual = proxiesOld
		case eq(source, "strategy"):
			includeGroups = proxiesOld
		}
	}

	var all []any
	nodeName := func(nodeID any) (any, bool) {
		node := nextMatch(dictGet(config, "nodes", []any{}), "id", nodeID)
		if truthy(node) {
			return dictItem(node, "name"), true
		}
		return nil, false
	}
	groupName := func(groupID any) (any, bool) {
		ref := nextMatch(dictGet(config, "proxy_groups", []any{}), "id", groupID)
		if truthy(ref) {
			return dictItem(ref, "name"), true
		}
		return nil, false
	}

	if order := dictGet(group, "proxies_order", []any{}); truthy(order) {
		for _, item := range iterate(order) {
			if eq(dictGet(item, "type", nil), "node") {
				nodeID := dictGet(item, "id", nil)
				if inStrings(nodeID, "DIRECT", "REJECT") {
					all = append(all, nodeID)
				} else if n, ok := nodeName(nodeID); ok {
					all = append(all, n)
				}
			} else if eq(dictGet(item, "type", nil), "strategy") {
				if g, ok := groupName(dictGet(item, "id", nil)); ok {
					all = append(all, g)
				}
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
				if inStrings(nodeID, "DIRECT", "REJECT") {
					all = append(all, nodeID)
				} else if n, ok := nodeName(nodeID); ok {
					all = append(all, n)
					surgeLog.Debug("Added aggregation node '" + fstr(n) + "' not in proxies_order")
				}
			}
		}
	} else {
		var nodesList, strategiesList []any
		if truthy(manual) {
			for _, nodeID := range iterate(manual) {
				if inStrings(nodeID, "DIRECT", "REJECT") {
					nodesList = append(nodesList, nodeID)
				} else if n, ok := nodeName(nodeID); ok {
					nodesList = append(nodesList, n)
				}
			}
		}
		if truthy(includeGroups) {
			for _, groupID := range iterate(includeGroups) {
				if g, ok := groupName(groupID); ok {
					strategiesList = append(strategiesList, g)
				}
			}
		}
		if eq(dictGet(group, "proxy_order", "nodes_first"), "strategies_first") {
			all = append(append(all, strategiesList...), nodesList...)
		} else {
			all = append(append(all, nodesList...), strategiesList...)
		}
	}

	var policyPaths []string
	token := dictGet(dictGet(config, "system_config", jsonx.NewObject()), "config_token", "")
	addPaths := func(ids, resources any, path func(id any) string, prefix string) {
		for _, id := range iterate(ids) {
			var found any
			for _, r := range iterate(resources) {
				if eq(dictItem(r, "id"), id) && truthy(dictGet(r, "enabled", true)) {
					found = r
					break
				}
			}
			if !truthy(found) {
				continue
			}
			url := effective + profileAPIPath(config, path(id))
			query := [][2]any{{"format", "surge"}}
			for _, field := range []string{"regex", "exclude_regex"} {
				if pattern := pyStrip(or2(dictGet(group, prefix+field, nil), "")); pattern != "" {
					query = append(query, [2]any{field, pattern})
				}
			}
			if truthy(token) {
				query = append(query, [2]any{"token", token})
			}
			policyPaths = append(policyPaths, appendURLQuery(url, query))
		}
	}
	if truthy(subscriptions) {
		addPaths(subscriptions, dictGet(config, "subscriptions", []any{}),
			func(id any) string { return "/subscriptions/" + fstr(id) + "/proxies" }, "")
	}
	if truthy(aggregations) {
		addPaths(aggregations, dictGet(config, "subscription_aggregations", []any{}),
			func(id any) string { return "/aggregations/" + fstr(id) + "/provider" }, "aggregation_")
	}

	if len(all) == 0 && len(policyPaths) == 0 {
		surgeLog.Debug("Group '" + fstr(name) + "' has no proxies and no policy-paths, skipping")
		return ""
	}
	proxyList := ""
	if len(all) > 0 {
		proxyList = joinAny(", ", all)
	}

	var line string
	testOptions := func() string {
		url := dictGet(group, "url", defaultTestURL)
		interval := dictGet(group, "interval", jsonx.Int(300))
		return ", url = " + fstr(url) + ", interval = " + fstr(interval)
	}
	typeText, _ := groupType.(string)
	switch {
	case eq(groupType, "select"):
		line = fstr(name) + " = select"
		if proxyList != "" {
			line += ", " + proxyList
		}
	case inStrings(groupType, "url-test", "fallback", "load-balance"):
		options := testOptions()
		line = fstr(name) + " = " + typeText
		if proxyList != "" {
			line += ", " + proxyList
		}
		line += options
	case eq(groupType, "smart"):
		options := testOptions()
		line = fstr(name) + " = smart"
		if proxyList != "" {
			line += ", " + proxyList
		}
		if truthy(policyPriority) {
			line += ", policy-priority=" + fstr(policyPriority)
		}
		line += options
	default:
		return ""
	}
	for _, path := range policyPaths {
		line += ", policy-path = " + path + ", update-interval = 86400"
	}
	surgeLog.Debug("Group '" + fstr(name) + "' final line: " + line)
	return line
}

// ConvertProxiesToSurgeText is convert_proxies_to_surge_text(proxies): one
// Surge line per Mihomo proxy for policy-path responses. toNodes is
// sub_store_client.proxies_to_nodes (substore.ProxiesToNodes; substore
// imports this package). WireGuard nodes and nodes that fail to convert are
// skipped (the latter with a warning).
func ConvertProxiesToSurgeText(proxies []any, toNodes func([]any) ([]any, error)) (text string, err error) {
	defer catch(&err)
	for _, proxy := range proxies {
		if dictGet(proxy, "dialer-proxy", nil) != nil {
			raise(dialerError("Surge policy-path 暂不支持 dialer-proxy，不能静默移除拨号配置"))
		}
	}
	nodes := mustValue(toNodes(proxies))
	var lines []string
	for _, item := range nodes {
		node := item.(*jsonx.Object)
		line, wireguard, convErr := ConvertNodeToSurge(node)
		if convErr != nil {
			surgeLog.Warning("转换节点到 Surge 格式失败: " + fstr(node.GetOr("name", "?")) + ", 错误: " + convErr.Error())
			continue
		}
		if wireguard != "" {
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// GetProxyGroupNodes is get_proxy_group_nodes(group, config_data): the
// legacy member names of a group (subscription nodes after the group's
// filters, then manual names), deduplicated in order. No production caller
// remains; kept for parity (test_surge_loon_group_exclusion.py).
func GetProxyGroupNodes(group any, config *jsonx.Object) (names []any, err error) {
	defer catch(&err)
	var result []any
	allNodes := dictGet(config, "nodes", []any{})
	if subscriptions := dictGet(group, "subscriptions", []any{}); truthy(subscriptions) {
		surgeLog.Debug("get_proxy_group_nodes: Looking for nodes with subscription_id in " + fstr(subscriptions))
		surgeLog.Debug("Total nodes in config: " + jsonx.Repr(jsonx.Int(int64(pyLen(allNodes)))))
		var matching []any
		for _, node := range iterate(allNodes) {
			if pyContainsAny(subscriptions, dictGet(node, "subscription_id", nil)) && truthy(dictGet(node, "enabled", true)) {
				matching = append(matching, node)
			}
		}
		surgeLog.Debug("Found " + jsonx.Repr(jsonx.Int(int64(len(matching)))) + " matching nodes")
		if matching == nil {
			matching = []any{}
		}
		filtered := mustValue(rules.FilterCandidates(matching, dictGet(group, "regex", nil), dictGet(group, "exclude_regex", nil)))
		for _, node := range filtered {
			result = append(result, dictItem(node, "name"))
		}
	}
	if manual := dictGet(group, "proxies", []any{}); truthy(manual) {
		for _, name := range iterate(manual) {
			if !listContains(result, name) {
				result = append(result, name)
			}
		}
	}
	seen := newSet()
	names = []any{}
	for _, name := range result {
		if !seen.has(name) {
			seen.add(name)
			names = append(names, name)
		}
	}
	return names, nil
}

// pyContainsAny is `needle in haystack` for any container.
func pyContainsAny(haystack, needle any) bool {
	switch h := haystack.(type) {
	case []any:
		return listContains(h, needle)
	case *jsonx.Object:
		k := key(needle)
		if h == nil {
			break
		}
		for _, existing := range h.Keys() {
			if existing == k {
				return true
			}
		}
		return false
	case string:
		s, ok := needle.(string)
		if !ok {
			raise(pyError("TypeError", "'in <string>' requires string as left operand, not "+typeName(needle)))
		}
		return strings.Contains(h, s)
	}
	raise(pyError("TypeError", "argument of type '"+typeName(haystack)+"' is not iterable"))
	return false
}

// listContains is `needle in list` (==).
func listContains(list []any, needle any) bool {
	for _, item := range list {
		if eq(item, needle) {
			return true
		}
	}
	return false
}
