package mosdns

import (
	"strings"
	"unsafe"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// UnsupportedYAMLError reports a custom MosDNS base configuration
// (mosdns.custom_config) or an upstream definition with a loaded YAML value
// the jsonx model cannot hold that Python would copy into the generated
// configuration: a collection that contains itself through an alias, or a
// !!set/!!omap/!!pairs value. Python writes such values with yaml.Dumper;
// the Go server refuses instead of writing other bytes. Values only in
// sections Python drops do not matter; mappings with keys that are not
// strings are written like PyYAML.
type UnsupportedYAMLError struct {
	Msg   string
	cause error
}

func (e *UnsupportedYAMLError) Error() string { return e.Msg }

// Unwrap retains the facade policy or alias rejection.
func (e *UnsupportedYAMLError) Unwrap() error { return e.cause }

// effectiveBaseURL is `config['system_config'].get('server_domain',
// ”).strip() or base_url`.
func effectiveBaseURL(config *jsonx.Object, baseURL string) string {
	serverDomain := strip(get(get(config, "system_config", jsonx.NewObject()), "server_domain", ""))
	if serverDomain != "" {
		return serverDomain
	}
	return baseURL
}

// download is one entry of get_mosdns_ruleset_downloads.
func download(rs any, file RulesetFile) *jsonx.Object {
	return jsonx.Obj(
		"name", item(rs, "name"),
		"url", file.URL,
		"local_path", file.LocalPath,
		"behavior", get(rs, "behavior", "domain"),
	)
}

// GetMosDNSRulesetDownloads is get_mosdns_ruleset_downloads(config_data,
// base_url): the rule files MosDNS needs ({name, url, local_path,
// behavior}); rule-proxy converts every format to MosDNS text, classical
// sets are split into a domain and an IP file.
func GetMosDNSRulesetDownloads(config *jsonx.Object, baseURL string) (out []any, err error) {
	defer catch(&err)
	_, ruleSets := splitRulesAndRulesets(config)

	settings := get(config, "mosdns", jsonx.NewObject())
	directIDs := get(settings, "direct_rulesets", []any{})
	proxyIDs := get(settings, "proxy_rulesets", []any{})
	configured := newSet(add(directIDs, proxyIDs))

	sets := buildRulesetMap(ruleSets, true)
	order := rulesetExecutionOrder(config, ruleSets)
	_, stems := rulesetLayout(config, sets, order)

	base := effectiveBaseURL(config, baseURL)

	downloads := []any{}
	for _, id := range order {
		rs := sets.get(id, nil)
		if !truthy(rs) || !truthy(get(rs, "enabled", true)) {
			continue
		}
		if !configured.has(id) {
			continue
		}
		stem, _ := stems.lookup(id)
		for _, file := range rulesetFiles(config, base, rs, stem) {
			if truthy(file.URL) {
				downloads = append(downloads, download(rs, file))
			}
		}
	}
	return downloads, nil
}

// GetMosDNSCustomFiles is get_mosdns_custom_files: the files written next to
// the configuration ({path, content}): custom hosts and single-rule files.
func GetMosDNSCustomFiles(config *jsonx.Object) (out []any, err error) {
	defer catch(&err)
	settings := get(config, "mosdns", jsonx.NewObject())
	files := []any{}
	hosts := parseHostsPairs(get(settings, "custom_hosts", ""))
	if len(hosts) > 0 {
		lines := make([]any, len(hosts))
		for i, h := range hosts {
			lines[i] = h.Domain + " " + h.Address
		}
		files = append(files, jsonx.Obj("path", "./rules/custom_hosts.txt", "content", joinStrings(lines, "\n")))
	}
	for _, group := range singleRuleGroups(config) {
		files = append(files, jsonx.Obj("path", group.path, "content", joinStrings(group.lines, "\n")))
	}
	return files, nil
}

// parseHostsPairs is parse_hosts_pairs(text) for any value: `(text or
// ”).splitlines()` (AttributeError for a truthy non-string).
func parseHostsPairs(text any) []convert.HostsPair {
	if !truthy(text) {
		return nil
	}
	s, ok := text.(string)
	if !ok {
		raise(attributeError(text, "splitlines"))
	}
	return convert.ParseHostsPairs(s)
}

func seq(entries ...any) []any { return entries }

func execEntry(exec string) *jsonx.Object { return jsonx.Obj("exec", exec) }

func matchEntry(match, exec string) *jsonx.Object {
	return jsonx.Obj("matches", []any{match}, "exec", exec)
}

func plugin(tag, kind string, args any) *jsonx.Object {
	return jsonx.Obj("tag", tag, "type", kind, "args", args)
}

// GenerateMosDNSConfig is generate_mosdns_config(config_data, base_url): the
// MosDNS YAML configuration (log, api, plugins: rule sets, cache, forwarders,
// hosts, sequences, servers), dumped like
// yaml.dump(Dumper=IndentDumper, allow_unicode=True, sort_keys=False,
// indent=2). Errors are the exceptions Python raises on malformed settings
// (*convert.PyError) and *UnsupportedYAMLError.
func GenerateMosDNSConfig(config *jsonx.Object, baseURL string) (out string, err error) {
	defer catch(&err)
	document, err := generateDocument(config, baseURL)
	if err != nil {
		return "", err
	}
	node, err := yamlbridge.Encode(document)
	if err != nil {
		return "", err
	}
	// Edit's verified rewrite retains displaced anchor definitions without
	// expanding their aliases when generated sections replace user sections.
	settings, _ := config.Value("mosdns").(*jsonx.Object)
	custom, _ := settings.Value("custom_config").(string)
	if original, parseErr := yamlx.Parse([]byte(custom)); parseErr == nil && original.Kind == yaml.MappingNode {
		data, editErr := yamlx.Edit([]byte(custom), func(root *yamlx.Node) error { *root = *node; return nil })
		return string(data), editErr
	}
	data, err := yamlx.Marshal(node)
	return string(data), err
}

func generateDocument(config *jsonx.Object, baseURL string) (any, error) {
	_, ruleSets := splitRulesAndRulesets(config)
	sets := buildRulesetMap(ruleSets, true)
	order := rulesetExecutionOrder(config, ruleSets)
	tagMapping, stems := rulesetLayout(config, sets, order)

	settings := get(config, "mosdns", jsonx.NewObject())

	customMatchesConfig := get(settings, "custom_matches", []any{})
	customMatchPosition := get(settings, "custom_match_position", "tail")
	parsedCustomMatches := parseCustomMatches(customMatchesConfig)
	if customMatchPosition != "head" && customMatchPosition != "tail" {
		customMatchPosition = "tail"
	}

	directRulesetIDs := get(settings, "direct_rulesets", []any{})
	proxyRulesetIDs := get(settings, "proxy_rulesets", []any{})
	directRuleIDs := get(settings, "direct_rules", []any{})
	proxyRuleIDs := get(settings, "proxy_rules", []any{})

	customConfig := get(settings, "custom_config", "")
	var base *jsonx.Object
	var root *yamlx.Node
	if truthy(customConfig) && strip(customConfig) != "" {
		var err error
		root, err = loadCustomConfig(customConfig.(string))
		if err != nil {
			return nil, err
		}
	}
	base = jsonx.Obj("api", jsonx.Obj("http", "0.0.0.0:8338"), "plugins", []any{})

	if !base.Has("plugins") {
		base.Set("plugins", []any{})
	}

	logEnabled := get(settings, "log_enabled", true)
	logLevel := get(settings, "log_level", "info")
	logFile := get(settings, "log_file", "./mosdns.log")
	if truthy(logEnabled) {
		base.Set("log", jsonx.Obj("level", logLevel, "file", logFile))
	} else {
		base.Delete("log")
	}

	apiEnabled := get(settings, "api_enabled", true)
	apiAddress := get(settings, "api_address", "0.0.0.0:8338")
	if truthy(apiEnabled) {
		base.Set("api", jsonx.Obj("http", apiAddress))
	} else {
		base.Delete("api")
	}

	plugins := []any{}
	configured := newSet(add(directRulesetIDs, proxyRulesetIDs))
	effective := effectiveBaseURL(config, baseURL)

	// 1. Rule set plugins (the Agent downloads the files to local_path).
	for _, id := range order {
		if !configured.has(id) {
			continue
		}
		rs := sets.get(id, nil)
		if !truthy(rs) || !truthy(get(rs, "enabled", true)) {
			continue
		}
		tag := tagDefault(tagMapping, id, rs)
		stem, _ := stems.lookup(id)
		for _, file := range rulesetFiles(config, effective, rs, stem) {
			if truthy(file.URL) {
				_ = download(rs, file) // ruleset_downloads: evaluated (rule_set['name']) but unused
			}
			isIP := file.Part == PartIP
			pluginTag, kind := tag, "domain_set"
			if isIP {
				pluginTag, kind = rulesetIPTag(rs, tag), "ip_set"
			}
			plugins = append(plugins, plugin(pluginTag, kind, jsonx.Obj("files", []any{file.LocalPath})))
		}
	}

	// 2. Cache.
	cacheEnabled := get(settings, "cache_enabled", true)
	cacheSize := get(settings, "cache_size", jsonx.Int(10240))
	cacheLazyTTL := get(settings, "cache_lazy_ttl", jsonx.Int(21600))
	cacheDumpEnabled := get(settings, "cache_dump_enabled", true)
	cacheDumpFile := get(settings, "cache_dump_file", "./cache.dump")
	cacheDumpInterval := get(settings, "cache_dump_interval", jsonx.Int(300))
	if truthy(cacheEnabled) {
		cacheArgs := jsonx.NewObject()
		if cacheSize != nil {
			cacheArgs.Set("size", pyInt(cacheSize))
		} else {
			cacheArgs.Set("size", jsonx.Int(10240))
		}
		if cacheLazyTTL != nil {
			cacheArgs.Set("lazy_cache_ttl", pyInt(cacheLazyTTL))
		} else {
			cacheArgs.Set("lazy_cache_ttl", jsonx.Int(21600))
		}
		if truthy(cacheDumpEnabled) {
			interval := dumpInterval(cacheDumpInterval)
			dumpFile := "./cache.dump"
			if truthy(cacheDumpFile) {
				dumpFile = pystr.Strip(str(cacheDumpFile))
			}
			cacheArgs.Set("dump_file", dumpFile)
			cacheArgs.Set("dump_interval", interval)
		}
		plugins = append(plugins, plugin("lazy_cache", "cache", cacheArgs))
	}

	// 3-5. Forwarders.
	localUpstreams, err := parseDNSUpstreams(get(settings, "local_dns", ""))
	if err != nil {
		return nil, err
	}
	if len(localUpstreams) == 0 {
		localUpstreams = []any{jsonx.Obj("addr", "223.5.5.5"), jsonx.Obj("addr", "119.29.29.29")}
	}
	plugins = append(plugins, plugin("forward_local", "forward",
		jsonx.Obj("concurrent", jsonx.Int(10), "upstreams", localUpstreams)))

	remoteUpstreams, err := parseDNSUpstreams(get(settings, "remote_dns", ""))
	if err != nil {
		return nil, err
	}
	if len(remoteUpstreams) == 0 {
		remoteUpstreams = []any{jsonx.Obj("addr", "https://1.1.1.1/dns-query"), jsonx.Obj("addr", "https://dns.google/dns-query")}
	}
	plugins = append(plugins, plugin("forward_remote", "forward",
		jsonx.Obj("concurrent", jsonx.Int(5), "upstreams", remoteUpstreams)))

	fallbackUpstreams, err := parseDNSUpstreams(get(settings, "fallback_dns", ""))
	if err != nil {
		return nil, err
	}
	if len(fallbackUpstreams) == 0 {
		// A copy, not a reference: no YAML anchor between the two lists.
		fallbackUpstreams = deepCopy(localUpstreams, map[unsafe.Pointer]any{}).([]any)
	}
	plugins = append(plugins, plugin("forward_remote_fallback", "forward",
		jsonx.Obj("concurrent", jsonx.Int(10), "upstreams", fallbackUpstreams)))

	// 6. Hosts (parsed like the bundled file, so IPv6 is not taken for a domain).
	customHosts := parseHostsPairs(get(settings, "custom_hosts", ""))
	if len(customHosts) > 0 {
		plugins = append(plugins, plugin("custom_hosts", "hosts", jsonx.Obj("files", []any{"./rules/custom_hosts.txt"})))
	}

	// 7-10. Sequences.
	plugins = append(plugins, plugin("remote_dns", "sequence", seq(
		execEntry("query_summary forward_remote"),
		execEntry("$forward_remote"),
		matchEntry("!has_resp", "reject 3"),
		execEntry("ttl 1"),
	)))
	plugins = append(plugins, plugin("remote_dns_fallback", "sequence", seq(
		execEntry("query_summary forward_remote_fallback"),
		execEntry("$forward_remote_fallback"),
		matchEntry("!has_resp", "reject 3"),
	)))
	plugins = append(plugins, plugin("proxy_dns", "fallback", jsonx.Obj(
		"primary", "remote_dns",
		"secondary", "remote_dns_fallback",
		"threshold", jsonx.Int(500),
		"always_standby", false,
	)))
	plugins = append(plugins, plugin("proxy_dns_seq", "sequence", seq(
		execEntry("$proxy_dns"),
		execEntry("accept"),
	)))
	chinaArgs := []any{}
	if truthy(cacheEnabled) {
		chinaArgs = append(chinaArgs, execEntry("$lazy_cache"), matchEntry("has_resp", "accept"))
	}
	chinaArgs = append(chinaArgs,
		execEntry("query_summary forward_local"),
		execEntry("$forward_local"),
		matchEntry("!has_resp", "reject 3"),
	)
	plugins = append(plugins, plugin("china_dns", "sequence", chinaArgs))

	// 11. Single rules: adjacent rules with the same route share a file.
	ruleIDToTag := tagMap{}
	for _, group := range singleRuleGroups(config) {
		for _, ident := range group.ids {
			ruleIDToTag[hashKey(ident)] = group.tag
		}
		kind := "domain_set"
		if group.part == PartIP {
			kind = "ip_set"
		}
		plugins = append(plugins, plugin(group.tag, kind, jsonx.Obj("files", []any{group.path})))
	}

	// 12. Main sequence.
	sequence := []any{execEntry("metrics_collector metrics"), execEntry("prefer_ipv4")}
	if len(customHosts) > 0 {
		sequence = append(sequence, execEntry("$custom_hosts"), matchEntry("has_resp", "accept"))
	}

	allRules := get(config, "rule_configs", []any{})
	sets2 := buildRulesetMap(ruleSets, false)

	ruleMatchEntries := []any{}
	ipMatchEntries := []any{}
	ipMatchEntry := func(ipTag string, isDirect bool) *jsonx.Object {
		exec := "goto ip_requery_proxy"
		if isDirect {
			exec = "accept"
		}
		return matchEntry("resp_ip $"+ipTag, exec)
	}
	headInsertIndex := len(sequence)
	addedMergedTags := map[string]bool{}

	for _, entry := range iterate(allRules) {
		if !truthy(get(entry, "enabled", true)) || !truthy(get(entry, "library_enabled", true)) {
			continue
		}
		itemType := get(entry, "itemType", "")
		itemID := get(entry, "id", nil)
		if !truthy(itemID) {
			continue
		}
		if itemType == "ruleset" {
			rs := sets2.get(itemID, jsonx.NewObject())
			behavior := get(rs, "behavior", "domain")
			tag := tagDefault(tagMapping, itemID, rs)
			isDirect := contains(itemID, directRulesetIDs)
			if !(isDirect || contains(itemID, proxyRulesetIDs)) {
				continue
			}
			parts := RulesetParts(behavior)
			if inList(PartIP, parts) {
				ipMatchEntries = append(ipMatchEntries, ipMatchEntry(rulesetIPTag(rs, tag), isDirect))
			}
			if !inList(PartDomain, parts) {
				continue
			}
			match := "qname $" + tag
			if contains(itemID, directRulesetIDs) {
				ruleMatchEntries = append(ruleMatchEntries, matchEntry(match, "goto china_dns"))
			} else if contains(itemID, proxyRulesetIDs) {
				ruleMatchEntries = append(ruleMatchEntries, matchEntry(match, "goto proxy_dns_seq"))
			}
		} else if itemType == "rule" {
			ruleTag, ok := ruleIDToTag.lookup(itemID)
			if !ok || ruleTag == "" {
				continue
			}
			if addedMergedTags[ruleTag] {
				continue
			}
			addedMergedTags[ruleTag] = true
			if strings.Contains(ruleTag, "ip_rules") {
				ipMatchEntries = append(ipMatchEntries, ipMatchEntry(ruleTag, contains(itemID, directRuleIDs)))
				continue
			}
			match := "qname $" + ruleTag
			if contains(itemID, directRuleIDs) {
				ruleMatchEntries = append(ruleMatchEntries, matchEntry(match, "goto china_dns"))
			} else if contains(itemID, proxyRuleIDs) {
				ruleMatchEntries = append(ruleMatchEntries, matchEntry(match, "goto proxy_dns_seq"))
			}
		}
	}

	if customMatchPosition == "head" {
		if len(parsedCustomMatches) > 0 {
			head := append([]any{}, sequence[:headInsertIndex]...)
			head = append(head, parsedCustomMatches...)
			sequence = append(head, sequence[headInsertIndex:]...)
		}
		sequence = append(sequence, ruleMatchEntries...)
	} else {
		sequence = append(sequence, ruleMatchEntries...)
		sequence = append(sequence, parsedCustomMatches...)
	}

	defaultForward := get(settings, "default_forward", "forward_remote")

	if len(ipMatchEntries) > 0 {
		plugins = append(plugins, plugin("ip_requery_proxy", "sequence", seq(
			execEntry("drop_resp"),
			execEntry("goto proxy_dns_seq"),
		)))
		sequence = append(sequence, execEntry("query_summary forward_local"), execEntry("$forward_local"))
		sequence = append(sequence, ipMatchEntries...)
		if defaultForward == "forward_local" {
			sequence = append(sequence, matchEntry("has_resp", "accept"))
		} else {
			sequence = append(sequence, execEntry("drop_resp"))
		}
	}

	if defaultForward == "forward_local" {
		sequence = append(sequence, execEntry("goto china_dns"))
	} else {
		sequence = append(sequence, execEntry("goto proxy_dns_seq"))
	}

	plugins = append(plugins, plugin("sequence_main", "sequence", sequence))

	listen := get(settings, "listen_address", nil)
	if !truthy(listen) {
		listen = ":53"
	}
	plugins = append(plugins, jsonx.Obj("type", "udp_server", "args", jsonx.Obj("entry", "sequence_main", "listen", listen)))
	listen = get(settings, "listen_address", nil)
	if !truthy(listen) {
		listen = ":53"
	}
	plugins = append(plugins, jsonx.Obj("type", "tcp_server", "args", jsonx.Obj("entry", "sequence_main", "listen", listen)))
	base.Set("plugins", plugins)

	if root != nil {
		return orderedMapping(root, base)
	}
	ordered := jsonx.NewObject()
	for _, k := range []string{"log", "api", "plugins"} {
		if v, ok := base.Get(k); ok {
			ordered.Set(k, v)
		}
	}
	for k, v := range base.All() {
		if !ordered.Has(k) && k != "servers" && k != "data_providers" {
			ordered.Set(k, v)
		}
	}
	return ordered, nil
}

// orderedMapping is the ordered_config of a custom root that has keys other
// than strings: base holds its string keys after the log/api/plugins
// updates. Python's mosdns_config keeps the original key order (deleted keys
// gone, new keys appended); ordered_config puts log, api and plugins first
// and drops servers and data_providers.
func orderedMapping(root *yamlx.Node, base *jsonx.Object) (*yamlx.Node, error) {
	generated, err := yamlbridge.Encode(base)
	if err != nil {
		return nil, err
	}
	out := yamlx.Map()
	for _, key := range []string{"log", "api", "plugins"} {
		if value := yamlx.Get(generated, key); value != nil {
			yamlx.Set(out, key, value)
		}
	}
	// Generated sections have JSON configuration values. All other user nodes,
	// including non-string keys and aliases, remain YAML nodes.
	for key, value := range yamlx.AllPairs(root) {
		resolved := yamlx.Resolve(key)
		if resolved.Kind == yaml.ScalarNode && resolved.ShortTag() == "!!str" {
			switch resolved.Value {
			case "log", "api", "plugins", "servers", "data_providers":
				continue
			}
		}
		out.Content = append(out.Content, key, value)
	}
	return out, nil
}

// tagDefault is ruleset_tag_map.get(id, rule_set.get('name') or
// _normalize_ruleset_id(id)) with Python's eager default.
func tagDefault(tags tagMap, id, rs any) string {
	var def any = get(rs, "name", nil)
	if !truthy(def) {
		def = normalizeRulesetID(id)
	}
	if tag, ok := tags.lookup(id); ok {
		return tag
	}
	// Only reachable for ids missing from the map; the default is then used
	// as a tag text (an f-string later).
	return str(def)
}

// dumpInterval is the cache dump interval: int(value), 300 for a TypeError,
// ValueError or a value <= 0 (OverflowError is not caught).
func dumpInterval(value any) (n jsonx.Number) {
	defer func() {
		if r := recover(); r != nil {
			if p, ok := r.(raised); ok {
				if class := convert.PyErrorClass(p.err); class == "TypeError" || class == "ValueError" {
					n = jsonx.Int(300)
					return
				}
			}
			panic(r)
		}
	}()
	n = pyInt(value)
	if b, _ := n.BigInt(); b == nil || b.Sign() <= 0 {
		return jsonx.Int(300)
	}
	return n
}

// loadCustomConfig keeps free-form configuration as a graph. Ordinary syntax
// errors retain the established default-template fallback; policy and unsafe
// alias errors are never hidden by that fallback.
func loadCustomConfig(text string) (*yamlx.Node, error) {
	node, err := yamlx.Parse([]byte(text))
	if err != nil {
		if rejectYAML(err) {
			return nil, customYAMLError(err)
		}
		return nil, nil
	}
	if node.Kind != yaml.MappingNode || node.ShortTag() == "!!set" {
		return nil, nil
	}
	return node, nil
}
