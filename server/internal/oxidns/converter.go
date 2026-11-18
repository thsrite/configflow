package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// OxiDNS configuration generation: ordered routing, isolated probe cache and
// opt-in features. Rule text conversion is shared with MosDNS.

var (
	LocalDNSDefaults  = []string{"udp://223.5.5.5", "udp://119.29.29.29"}
	RemoteDNSDefaults = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}
	ruleFileRE        = regexp.MustCompile(`^rules/(rule_[0-9a-f]{16})_(domain|ip)\.txt$`)
	dnsRuleTypes      = []string{"DOMAIN-SUFFIX", "DOMAIN", "DOMAIN-KEYWORD", "DOMAIN-REGEX", "IP-CIDR", "IP-CIDR6"}
)

// TagFor is _tag: prefix + 16 hex digits of sha256 of the resource identity.
func TagFor(prefix string, identity *Node) string {
	sum := sha256.Sum256([]byte(displayValue(identity)))
	return prefix + "_" + hex.EncodeToString(sum[:])[:16]
}

func tagForString(prefix, identity string) string { return TagFor(prefix, S(identity)) }

// RulePath is the generated resource of one ruleset part.
func RulePath(item *Node, part string) string {
	return "./rules/" + TagFor("rule", get(item, "id")) + "_" + part + ".txt"
}

// base is the effective panel origin: system_config.server_domain or baseURL.
func (p *Profile) base(baseURL string) string {
	domain := get(mapOr(p.get("system_config")), "server_domain")
	if str(domain) != "" {
		baseURL = str(domain)
	}
	return strings.TrimRight(baseURL, "/")
}

// activeItems is _items: enabled rule_configs, or the legacy rules/rule sets.
func (p *Profile) activeItems() []*Node {
	if configs := p.get("rule_configs"); len(items(configs)) > 0 {
		return filter(items(configs), func(item *Node) bool {
			return enabledOrDefault(item, "enabled") && enabledOrDefault(item, "library_enabled")
		})
	}
	ruleList, sets := p.splitRules()
	var out []*Node
	for _, pair := range []struct {
		kind string
		list *Node
	}{{"rule", ruleList}, {"ruleset", sets}} {
		for _, item := range items(pair.list) {
			c := shallow(mapOr(item))
			set(c, "itemType", S(pair.kind))
			out = append(out, c)
		}
	}
	return out
}

// enabledOrDefault defaults an absent enable flag to true.
func enabledOrDefault(item *Node, key string) bool {
	return !has(item, key) || (isTrue(get(item, key)))
}

func suffixOf(item *Node) string {
	if equal(get(item, "itemType"), S("ruleset")) {
		return "rulesets"
	}
	return "rules"
}

// inSelector is `identity in selector` for a selector list (or mapping).
func inSelector(selector, identity *Node) bool {
	if isMap(selector) {
		return isStr(identity) && has(selector, identity.Value)
	}
	return contains(selector, identity)
}

type selection struct {
	item  *Node
	route string
}

// selected is _selected: (item, route) for the routed candidates; a rule DNS
// action wins over the selector lists. candidates nil means every active rule.
func (p *Profile) selected(settings *Node, candidates []*Node) ([]selection, error) {
	actions := get(settings, "rule_actions")
	if candidates == nil {
		candidates = p.activeItems()
	}
	var out []selection
	for _, item := range candidates {
		suffix := suffixOf(item)
		identity := get(item, "id")
		switch {
		case isStr(identity) && has(actions, identity.Value):
			out = append(out, selection{item, RouteKey(get(actions, identity.Value))})
		case inSelector(get(settings, "direct_"+suffix), identity):
			out = append(out, selection{item, "direct"})
		case inSelector(get(settings, "proxy_"+suffix), identity):
			out = append(out, selection{item, "proxy"})
		}
	}
	return out, nil
}

// UnavailableRulesets explains, one line per ruleset, why referenced
// generated rule files cannot be produced.
func UnavailableRulesets(p *Profile, paths []string) []string {
	rulesets := map[string]*Node{}
	for _, item := range items(p.get("rule_configs")) {
		if equal(get(item, "itemType"), S("ruleset")) {
			rulesets[TagFor("rule", get(item, "id"))] = item
		}
	}
	lines := []string{}
	var deleted []string
	seen := map[string]bool{}
	for _, path := range paths {
		m := ruleFileRE.FindStringSubmatch(path)
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		item := rulesets[m[1]]
		if item == nil {
			deleted = append(deleted, m[1])
			continue
		}
		name := get(item, "name")
		if !(str(name) != "") {
			name = get(item, "id")
		}
		behavior := get(item, "behavior")
		if !has(item, "behavior") {
			behavior = S("domain")
		}
		switch label := displayValue(name); {
		case !enabledOrDefault(item, "enabled"):
			lines = append(lines, "「"+label+"」已停用：请在规则管理中重新启用")
		case !enabledOrDefault(item, "library_enabled"):
			lines = append(lines, "「"+label+"」的规则来源已停用：请在规则库中重新启用")
		case !containsString(rulesetParts(behavior), m[2]):
			if isNone(behavior) {
				lines = append(lines, fmt.Sprintf(messageNullRulesetType, label))
			} else {
				lines = append(lines, "「"+label+"」的类型被改成了 "+displayValue(behavior)+"：请改回原来的类型")
			}
		default:
			lines = append(lines, "「"+label+"」没有下载地址：请检查规则来源")
		}
	}
	if len(deleted) > 0 {
		lines = append(lines, fmt.Sprintf("%d 个已删除的规则集（%s）：请在 OxiDNS 设置的 YAML 中删除含这些名称的行", len(deleted), strings.Join(deleted, "、")))
	}
	return lines
}

// RulesetDownload is one rule file the Agent downloads.
type RulesetDownload struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
	Behavior  *Node  `json:"-"`
}

// managedRulesetParts is ruleset_parts: managed routing only uses names.
func managedRulesetParts(item *Node) []string {
	if containsString(rulesetParts(behaviorNode(item)), "domain") {
		return []string{"domain"}
	}
	return nil
}

func behaviorNode(item *Node) *Node {
	if has(item, "behavior") {
		return get(item, "behavior")
	}
	return S("domain")
}

// RulesetDownloads is get_oxidns_ruleset_downloads.
func RulesetDownloads(p *Profile, baseURL string) ([]RulesetDownload, error) {
	settings, err := NormalizeSettings(p.Settings(), true)
	if err != nil {
		return nil, err
	}
	return rulesetDownloads(settings, p, baseURL)
}

func rulesetDownloads(settings *Node, p *Profile, baseURL string) ([]RulesetDownload, error) {
	var native map[string]bool
	if str(get(settings, "config_mode")) == "native" {
		document, err := NativeDocument(settings, true, nil)
		if err != nil {
			return nil, err
		}
		if document, settings, err = MigrateDomainOnlyRouting(document, settings, p, baseURL); err != nil {
			return nil, err
		}
		if native, err = referencedPaths(document); err != nil {
			return nil, err
		}
	}
	effective := p.base(baseURL)
	var candidates []selection
	if native != nil {
		for _, item := range p.activeItems() {
			candidates = append(candidates, selection{item: item})
		}
	} else {
		selections := M()
		pairs(settings, func(key string, value *Node) bool {
			if !isSeq(value) && !isMap(value) {
				value = L()
			}
			set(selections, key, value)
			return true
		})
		var err error
		if candidates, err = p.selected(selections, nil); err != nil {
			return nil, err
		}
	}
	downloads := []RulesetDownload{}
	for _, c := range candidates {
		item := c.item
		if !equal(get(item, "itemType"), S("ruleset")) {
			continue
		}
		var parts []string
		if native != nil {
			for _, part := range rulesetParts(behaviorNode(item)) {
				if path, _, err := relativeResource(RulePath(item, part)); err == nil && native[path] {
					parts = append(parts, part)
				}
			}
		} else {
			parts = managedRulesetParts(item)
		}
		if len(parts) == 0 {
			continue
		}
		url := get(item, "url")
		if !(str(url) != "") {
			return nil, fail("已选择的规则集缺少下载来源")
		}
		if !isStr(url) {
			return nil, errMalformed
		}
		link := url.Value
		if strings.HasPrefix(link, "/") {
			link = effective + link
		}
		name := "规则集"
		if n := get(item, "name"); str(n) != "" {
			name = displayValue(n)
		}
		for _, part := range parts {
			proxied, err := p.ruleProxyURL(effective, link, part)
			if err != nil {
				return nil, err
			}
			downloads = append(downloads, RulesetDownload{name, proxied, RulePath(item, part), behaviorNode(item)})
		}
	}
	adguard := get(settings, "adguard")
	if isMap(adguard) && (isTrue(get(adguard, "enabled"))) {
		for _, url := range items(get(adguard, "urls")) {
			path := "./rules/" + TagFor("adguard", url) + ".txt"
			if native != nil {
				if rel, _, err := relativeResource(path); err != nil || !native[rel] {
					continue
				}
			}
			downloads = append(downloads, RulesetDownload{"AdGuard DNS 规则", displayValue(url), path, nil})
		}
	}
	return downloads, nil
}

// CustomFiles is get_oxidns_custom_files: hosts, AdGuard content and the
// managed attachments the configuration references.
func CustomFiles(p *Profile) ([]File, error) {
	settings, err := NormalizeSettings(p.Settings(), true)
	if err != nil {
		return nil, err
	}
	var native map[string]bool
	if str(get(settings, "config_mode")) == "native" {
		document, err := NativeDocument(settings, true, nil)
		if err != nil {
			return nil, err
		}
		if native, err = referencedPaths(document); err != nil {
			return nil, err
		}
	}
	files := []File{}
	if hosts := get(settings, "custom_hosts"); (native == nil || native["rules/custom_hosts.txt"]) && isStr(hosts) && strings.TrimSpace(hosts.Value) != "" {
		content, err := HostsFileContent(hosts.Value)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "./rules/custom_hosts.txt", Content: []byte(content)})
	}
	adguard := mapOr(get(settings, "adguard"))
	if content := get(adguard, "content"); (native == nil || native["rules/adguard-custom.txt"]) && (isTrue(get(adguard, "enabled"))) && isStr(content) && strings.TrimSpace(content.Value) != "" {
		files = append(files, File{Path: "./rules/adguard-custom.txt", Content: []byte(content.Value)})
	}
	managed, err := ManagedFiles(get(settings, "additional_files"))
	if err != nil {
		return nil, err
	}
	return append(files, managed...), nil
}

var (
	digitsDots = regexp.MustCompile(`^[0-9.]+$`)
	legacyName = regexp.MustCompile(`^[A-Za-z0-9_](?:[A-Za-z0-9_.-]*[A-Za-z0-9_])?\.?$`)
)

// HostsFileContent keeps native hosts rules (matcher ip...) and converts
// unambiguous legacy "IP name [alias...]" rows; line endings are kept.
func HostsFileContent(text string) (string, error) {
	isIP := func(value string) bool {
		_, err := netip.ParseAddr(value)
		return err == nil && !strings.Contains(value, "%")
	}
	isLegacyName := func(value string) bool {
		if hasPrefix(value, "full:", "domain:", "keyword:", "regexp:") {
			return value[strings.IndexByte(value, ':')+1:] != ""
		}
		return !digitsDots.MatchString(value) && legacyName.MatchString(value)
	}
	var out strings.Builder
	for number, raw := range splitLines(text, true) {
		line := strings.TrimRight(raw, "\r\n")
		ending := raw[len(line):]
		rule, comment, marker := strings.Cut(line, "#")
		fields := strings.Fields(rule)
		allIPs := len(fields) >= 2
		for _, v := range fields[min(1, len(fields)):] {
			allIPs = allIPs && isIP(v)
		}
		switch {
		case len(fields) == 0, allIPs:
			out.WriteString(raw)
			continue
		case len(fields) >= 2 && isIP(fields[0]):
			names := true
			for _, v := range fields[1:] {
				names = names && !isIP(v) && isLegacyName(v)
			}
			if names {
				converted := make([]string, len(fields)-1)
				for i, name := range fields[1:] {
					converted[i] = name + " " + fields[0]
				}
				if marker {
					converted[0] += " #" + comment
				}
				sep := ending
				if sep == "" {
					sep = "\n"
				}
				out.WriteString(strings.Join(converted, sep) + ending)
				continue
			}
		}
		return "", fail(fmt.Sprintf("自定义 Hosts 第 %d 行格式无效；请使用“域名规则 IPv4/IPv6 [更多 IP]”，或“IP 域名 [别名]”，不能混合地址与别名", number+1))
	}
	return out.String(), nil
}

// legacyMode selects historical routing shapes for native recognition.
type legacyMode int

const (
	legacyNone legacyMode = iota
	legacyAll
	legacyContentAware
)

// ruleGroup is one generated provider: route, args (files/exps/ips lists).
type ruleGroup struct {
	Route string
	Args  *Node
	Tag   string
}

type routingGroups struct{ Domain, IP []*ruleGroup }

func (g *routingGroups) part(name string) *[]*ruleGroup {
	if name == "ip" {
		return &g.IP
	}
	return &g.Domain
}

func (g *routingGroups) all() []*ruleGroup {
	return append(append([]*ruleGroup{}, g.Domain...), g.IP...)
}

// routingGroups is _routing_groups: managed domain groups, or the historical
// IP shapes (legacy) used to recognize native documents.
func (p *Profile) routingGroups(settings *Node, legacy legacyMode, candidates []*Node) (*routingGroups, error) {
	groups := &routingGroups{}
	selected, err := p.selected(settings, candidates)
	if err != nil {
		return nil, err
	}
	for _, s := range selected {
		item, route := s.item, s.route
		type entry struct {
			part string
			args *Node
		}
		var entries []entry
		if equal(get(item, "itemType"), S("ruleset")) {
			parts := managedRulesetParts(item)
			if legacy != legacyNone {
				parts = rulesetParts(behaviorNode(item))
			}
			for _, part := range parts {
				if part == "ip" && legacy == legacyContentAware && !hasIPPart(item, p) {
					continue
				}
				entries = append(entries, entry{part, M("files", L(RulePath(item, part)))})
			}
		} else {
			kind, value := get(item, "rule_type"), get(item, "value")
			kindText, valueText := "", ""
			if has(item, "rule_type") {
				kindText = displayValue(kind)
			}
			if has(item, "value") {
				valueText = displayValue(value)
			}
			part := "domain"
			if kindText == "IP-CIDR" || kindText == "IP-CIDR6" {
				part = "ip"
			}
			if part == "ip" && legacy == legacyNone {
				continue
			}
			if !isStr(kind) && has(item, "rule_type") || !containsString(dnsRuleTypes, kindText) {
				if isNone(kind) && has(item, "rule_type") {
					return nil, fail(messageNullRuleType)
				}
				return nil, fail("所选规则类型暂不能转换为 OxiDNS 规则：" + kindText)
			}
			converted, err := convertRule(kindText+","+valueText, part)
			if err != nil {
				return nil, err
			}
			converted = strings.TrimSpace(converted)
			if converted == "" || strings.HasPrefix(converted, "#") {
				return nil, fail("所选单条规则无法转换为 DNS 规则")
			}
			key := "exps"
			if part == "ip" {
				key = "ips"
			}
			entries = append(entries, entry{part, M(key, L(converted))})
		}
		for _, e := range entries {
			target := groups.part(e.part)
			if n := len(*target); (isTrue(get(settings, "aggregate_rules"))) && n > 0 && (*target)[n-1].Route == route {
				last := (*target)[n-1]
				pairs(e.args, func(key string, values *Node) bool {
					list := setDefault(last.Args, key, L())
					list.Content = append(list.Content, values.Content...)
					return true
				})
				continue
			}
			*target = append(*target, &ruleGroup{route, e.args, fmt.Sprintf("rules_%s_%d", e.part, len(*target)+1)})
		}
	}
	return groups, nil
}

// forwardUpstreams is _forward: upstream text (YAML list or lines) or defaults.
func forwardUpstreams(text string, defaults []string) (*Node, error) {
	if strings.TrimSpace(text) == "" {
		out := L()
		for _, addr := range defaults {
			out.Content = append(out.Content, M("addr", addr))
		}
		return out, nil
	}
	if hasPrefix(strings.TrimLeft(text, " \t\n\r\v\f"), "-", "[", "{", "addr:") {
		parsed, ok, err := safeLoad(text)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fail("DNS 上游 YAML 语法无效")
		}
		values := []*Node{parsed}
		if isSeq(parsed) {
			values = parsed.Content
		}
		bad := len(values) == 0
		for _, item := range values {
			bad = bad || !isStr(item) && !isMap(item) || isMap(item) && !(str(get(item, "addr")) != "")
		}
		if bad {
			return nil, fail("DNS 上游 YAML 每项需要 addr 地址")
		}
	}
	result, err := parseUpstreams(text)
	if err != nil {
		return nil, err
	}
	if len(result.Content) == 0 {
		return nil, fail("DNS 上游配置无效")
	}
	for _, item := range result.Content {
		if strings.TrimSpace(str(get(item, "addr"))) == "" {
			return nil, fail("DNS 上游配置无效")
		}
	}
	return result, nil
}

// fallbackUpstreams is _fallback_upstreams: empty disables the backup.
func fallbackUpstreams(text string) (*Node, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return L(), nil
	}
	if hasPrefix(strings.TrimLeft(text, " \t\n\r\v\f"), "-", "[", "{", "addr:") || trimmed == "null" || trimmed == "~" {
		value, ok, err := safeLoad(text)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fail("DNS 上游 YAML 语法无效")
		}
		if isNone(value) || isSeq(value) && len(value.Content) == 0 {
			return L(), nil
		}
	}
	return forwardUpstreams(text, nil)
}

// Generate is generate_oxidns_config for the profile's own settings.
func Generate(p *Profile, baseURL string) (string, error) {
	settings, err := NormalizeSettings(p.Settings(), true)
	if err != nil {
		return "", err
	}
	return generate(settings, p, baseURL)
}

func generate(s *Node, p *Profile, baseURL string) (string, error) {
	if str(get(s, "config_mode")) == "native" {
		document, err := NativeDocument(s, true, nil)
		if err != nil {
			return "", err
		}
		if document, err = PrepareNativeRuleUpdates(document, s, p, baseURL); err != nil {
			return "", err
		}
		if _, err := RequireValidDocument(document); err != nil {
			return "", err
		}
		return dump(deliveryNode(document))
	}
	result, err := templateDocument(s, p, baseURL)
	if err != nil {
		return "", err
	}
	return dump(deliveryNode(result))
}

func templateDocument(s *Node, p *Profile, baseURL string) (*Node, error) {
	custom := M()
	if text := str(get(s, "custom_config")); strings.TrimSpace(text) != "" {
		parsed, ok, err := safeLoad(text)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fail("OxiDNS 高级 YAML 语法无效")
		}
		custom = parsed
	}
	if !isMap(custom) {
		return nil, fail("OxiDNS 高级 YAML 顶层必须为对象")
	}
	if externalIncludes(custom) {
		return nil, fail("完整部署模板不支持外部 include；请将插件内联到高级 YAML")
	}
	extra := del(custom, "plugins")
	if extra == nil {
		extra = L()
	}
	if !isSeq(extra) {
		return nil, fail("高级 YAML 的 plugins 必须为数组")
	}
	result := clone(custom)
	runtime := get(result, "runtime")
	if !isNone(runtime) && !isMap(runtime) {
		return nil, fail("高级 YAML runtime 必须为对象")
	}
	merged := M()
	if isMap(runtime) {
		merged = shallow(runtime)
	}
	set(merged, "worker_threads", get(s, "worker_threads"))
	set(result, "runtime", merged)
	del(result, "api")
	if api := get(s, "api"); isTrue(get(api, "enabled")) {
		http := M("listen", get(api, "listen"))
		if str(get(api, "username")) != "" {
			set(http, "auth", M("type", "basic", "username", get(api, "username"), "password", get(api, "password")))
		}
		if str(get(api, "webui_root")) != "" {
			set(http, "webui", M("root", get(api, "webui_root"), "index", "index.html"))
		}
		if isMap(get(api, "ssl")) && mapSize(get(api, "ssl")) > 0 {
			set(http, "ssl", clone(get(api, "ssl")))
		}
		set(result, "api", M("http", http))
	}
	log := get(s, "log")
	logOut := M("level", get(log, "level"))
	if str(get(log, "file")) != "" {
		set(logOut, "file", get(log, "file"))
		set(logOut, "rotation", M("type", get(log, "rotation"), "max_files", get(log, "max_files")))
	}
	set(result, "log", logOut)
	plugins := L()
	add := func(tag, kind string, args *Node) {
		plugins.Content = append(plugins.Content, M("tag", tag, "type", kind, "args", args))
	}
	enabled := func(name string) bool { return isTrue(get(get(s, name), "enabled")) }
	groups, err := p.routingGroups(s, legacyNone, nil)
	if err != nil {
		return nil, err
	}
	for _, g := range groups.Domain {
		add(g.Tag, "domain_set", g.Args)
	}
	for _, g := range groups.IP {
		add(g.Tag, "ip_set", g.Args)
	}
	hosts := str(get(s, "custom_hosts"))
	if strings.TrimSpace(hosts) != "" {
		if _, err := HostsFileContent(hosts); err != nil {
			return nil, err
		}
		add("custom_hosts", "hosts", M("files", L("./rules/custom_hosts.txt")))
	}
	if isTrue(get(s, "cache_enabled")) {
		cache := M("size", get(s, "cache_size"))
		if lazy, _ := ratOf(get(s, "cache_lazy_ttl")); lazy.Sign() > 0 {
			set(cache, "lazy_cache_ttl", get(s, "cache_lazy_ttl"))
		}
		set(cache, "short_circuit", B(true))
		if isTrue(get(s, "cache_dump_enabled")) {
			set(cache, "dump_file", get(s, "cache_dump_file"))
			set(cache, "dump_interval", get(s, "cache_dump_interval"))
		}
		add("lazy_cache", "cache", cache)
	}
	if isTrue(get(s, "probe_cache_enabled")) {
		add("local_probe_cache", "cache", M("size", get(s, "probe_cache_size"), "short_circuit", false))
	}
	fallback, err := fallbackUpstreams(str(get(s, "fallback_dns")))
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"local", "remote"} {
		defaults := LocalDNSDefaults
		if name == "remote" {
			defaults = RemoteDNSDefaults
		}
		upstreams, err := forwardUpstreams(str(get(s, name+"_dns")), defaults)
		if err != nil {
			return nil, err
		}
		add("forward_"+name, "forward", M("concurrent", get(s, name+"_concurrent"), "upstreams", upstreams))
	}
	hasFallback := len(fallback.Content) > 0
	if hasFallback {
		add("forward_fallback", "forward", M("concurrent", get(s, "fallback_concurrent"), "upstreams", fallback))
	}
	dynamic := get(s, "dynamic_domain_set")
	dynamicOn, learn := isTrue(get(dynamic, "enabled")), (isTrue(get(dynamic, "learn")))
	if dynamicOn {
		add("dynamic_domains", "dynamic_domain_set", clone(get(dynamic, "args")))
		if learn {
			args := clone(get(dynamic, "learn_args"))
			set(args, "provider", S("dynamic_domains"))
			add("learn_classified_domain", "learn_domain", args)
		}
	}
	for _, name := range []string{"query_recorder", "ip_selector", "ros_address_list", "ros_route"} {
		if !enabled(name) {
			continue
		}
		args := clone(get(get(s, name), "args"))
		if name == "ros_route" {
			kept := M()
			pairs(args, func(key string, value *Node) bool {
				if !equal(value, S("")) {
					set(kept, key, value)
				}
				return true
			})
			args = kept
		}
		add(name, name, args)
	}
	adguard := get(s, "adguard")
	adguardOn := isTrue(get(adguard, "enabled"))
	if adguardOn {
		paths := L()
		if strings.TrimSpace(str(get(adguard, "content"))) != "" {
			paths.Content = append(paths.Content, S("./rules/adguard-custom.txt"))
		}
		for _, url := range items(get(adguard, "urls")) {
			paths.Content = append(paths.Content, S("./rules/"+TagFor("adguard", url)+".txt"))
		}
		add("adguard_rules", "adguard_rule", M("files", paths))
		add("adguard_block", "black_hole", M("mode", get(adguard, "mode")))
		add("adguard_reject", "sequence", L(M("exec", "$adguard_block"), M("exec", "accept")))
	}
	wrappers := func() *Node {
		out := L()
		for _, name := range RealWrappers {
			if enabled(name) {
				out.Content = append(out.Content, M("exec", "$"+name))
			}
		}
		return out
	}
	direct := wrappers()
	if isTrue(get(s, "cache_enabled")) {
		direct.Content = append(direct.Content, M("exec", "$lazy_cache"))
	}
	direct.Content = append(direct.Content, M("exec", "$forward_local"), M("matches", L("!has_resp"), "exec", "reject 3"))
	add("china_dns", "sequence", direct)
	probe := L()
	if isTrue(get(s, "probe_cache_enabled")) {
		probe.Content = append(probe.Content, M("exec", "$local_probe_cache"))
	}
	probe.Content = append(probe.Content, M("matches", L("!has_resp"), "exec", "$forward_local"), M("exec", "return"))
	add("local_probe", "sequence", probe)
	remote := L(M("exec", "$forward_remote"), M("matches", L("!has_resp"), "exec", "reject 3"))
	if ttl := get(s, "remote_ttl"); positive(ttl) {
		remote.Content = append(remote.Content, M("exec", "ttl "+ttl.Value))
	}
	add("remote_dns", "sequence", remote)
	if hasFallback {
		add("fallback_dns", "sequence", L(M("exec", "$forward_fallback"), M("matches", L("!has_resp"), "exec", "reject 3")))
		add("fallback", "fallback", M("primary", "remote_dns", "secondary", "fallback_dns",
			"threshold", get(s, "fallback_threshold"), "always_standby", get(s, "always_standby")))
		add("proxy_dns_seq", "sequence", L(M("exec", "$fallback"), M("exec", "accept")))
	} else {
		add("proxy_dns_seq", "sequence", L(M("exec", "goto remote_dns")))
	}
	requery := L(M("exec", "drop_resp"))
	directIP := wrappers()
	defaultDirect := wrappers()
	defaultDirect.Content = append(defaultDirect.Content, M("exec", "accept"))
	add("default_direct_ip", "sequence", defaultDirect)
	if dynamicOn && learn {
		if equal(get(dynamic, "route"), S("direct")) {
			directIP.Content = append(directIP.Content, M("exec", "$learn_classified_domain"))
		} else {
			requery.Content = append(requery.Content, M("exec", "$learn_classified_domain"))
		}
	}
	directIP.Content = append(directIP.Content, M("exec", "accept"))
	requery.Content = append(requery.Content, M("exec", "goto proxy_dns_seq"))
	add("direct_ip", "sequence", directIP)
	add("ip_requery_proxy", "sequence", requery)
	var routes []string
	for _, g := range groups.all() {
		routes = append(routes, g.Route)
	}
	helpers, err := HelperPlugins(routes, get(s, "rule_actions"), s)
	if err != nil {
		return nil, err
	}
	plugins.Content = append(plugins.Content, helpers...)

	main := L()
	if isTrue(get(s, "metrics_enabled")) {
		main.Content = append(main.Content, M("exec", "metrics_collector metrics"))
	}
	if enabled("query_recorder") {
		main.Content = append(main.Content, M("exec", "$query_recorder"))
	}
	if isTrue(get(s, "prefer_ipv4")) {
		main.Content = append(main.Content, M("exec", "prefer_ipv4"))
	}
	if blocked := items(get(s, "block_qtypes")); len(blocked) > 0 {
		codes := make([]string, len(blocked))
		for i, v := range blocked {
			codes[i] = v.Value
		}
		main.Content = append(main.Content, M("matches", L("qtype "+strings.Join(codes, " ")), "exec", BlockQtypesExec))
	}
	if strings.TrimSpace(hosts) != "" {
		main.Content = append(main.Content, M("exec", "$custom_hosts"), M("matches", L("has_resp"), "exec", "accept"))
	}
	if adguardOn {
		main.Content = append(main.Content, M("matches", L("question $adguard_rules"), "exec", "goto adguard_reject"))
	}
	matches, err := parseCustomMatches(get(s, "custom_matches"))
	if err != nil {
		return nil, err
	}
	head := str(get(s, "custom_match_position")) == "head"
	if head {
		main.Content = append(main.Content, matches.Content...)
	}
	for _, g := range groups.Domain {
		exec, err := RouteExec(g.Route, "domain")
		if err != nil {
			return nil, err
		}
		main.Content = append(main.Content, M("matches", L("qname $"+g.Tag), "exec", exec))
	}
	if str(get(s, "custom_match_position")) == "tail" {
		main.Content = append(main.Content, matches.Content...)
	}
	if dynamicOn {
		target := "goto proxy_dns_seq"
		if equal(get(dynamic, "route"), S("direct")) {
			target = "goto china_dns"
		}
		main.Content = append(main.Content, M("matches", L("qname $dynamic_domains"), "exec", target))
	}
	final := "goto proxy_dns_seq"
	if str(get(s, "default_forward")) == "forward_local" {
		final = "goto china_dns"
	}
	main.Content = append(main.Content, M("exec", final))
	add("sequence_main", "sequence", main)
	add("server_udp", "udp_server", M("listen", get(s, "listen"), "entry", "sequence_main"))
	if isTrue(get(s, "tcp_enabled")) {
		add("server_tcp", "tcp_server", M("listen", get(s, "listen"), "entry", "sequence_main"))
	}

	downloads, err := rulesetDownloads(s, p, baseURL)
	if err != nil {
		return nil, err
	}
	customUpdates := false
	for _, plugin := range extra.Content {
		customUpdates = customUpdates || isMap(plugin) && (isOneOf(get(plugin, "type"), "download", "reload_provider") ||
			isOneOf(get(plugin, "tag"), "rules_download", "rules_reload", "rules_refresh"))
	}
	update := get(s, "rule_update")
	updateOn := isTrue(get(update, "enabled"))
	if updateOn && customUpdates {
		return nil, fail("已有自定义下载或重载插件，请在高级 YAML 中配置规则定时更新，不能自动创建第二条维护链")
	}
	if updateOn && len(downloads) == 0 {
		return nil, fail("定时更新需要至少一个规则订阅")
	}
	if len(downloads) > 0 && !customUpdates {
		updateArgs := get(update, "args")
		args := M()
		_, catalog := schemas()
		var issues []Issue
		for _, key := range []string{"timeout", "outbound", "socks5"} {
			if value := get(updateArgs, key); !isNone(value) && !equal(value, S("")) {
				set(args, key, value)
				issues = append(issues, ValidateSchema(value, catalog["download"].Properties[key], P("plugins", len(plugins.Content), "args", key))...)
			}
		}
		if hasError(issues) {
			return nil, &ValidationError{issues}
		}
		entries := L()
		for _, d := range downloads {
			entries.Content = append(entries.Content, M("url", d.URL, "dir", posixParent(d.LocalPath), "filename", posixName(d.LocalPath)))
		}
		set(args, "downloads", entries)
		add("rules_download", "download", args)
		providers := L()
		for _, g := range groups.all() {
			providers.Content = append(providers.Content, S("$"+g.Tag))
		}
		if adguardOn {
			providers.Content = append(providers.Content, S("$adguard_rules"))
		}
		add("rules_reload", "reload_provider", providers)
		add("rules_refresh", "sequence", L(M("exec", "$rules_download"), M("exec", "$rules_reload")))
		if updateOn {
			add("rules_cron", "cron", M("timezone", get(updateArgs, "timezone"), "jobs", L(M(
				"name", "refresh_rules", "schedule", get(updateArgs, "schedule"), "executors", L("$rules_refresh")))))
		}
	}
	plugins.Content = append(plugins.Content, clone(extra).Content...)
	tags := map[string]bool{}
	for _, plugin := range plugins.Content {
		if !isMap(plugin) {
			return nil, fail("插件必须为对象")
		}
		tag := get(plugin, "tag")
		if !has(plugin, "tag") {
			tag = S("")
		}
		if !isStr(tag) || !validTag(tag.Value) {
			return nil, fail("OxiDNS 插件 tag 必须为有效的 1–64 位 ASCII 标识")
		}
		if tags[tag.Value] {
			return nil, fail("OxiDNS 插件 tag 重复：" + tag.Value)
		}
		tags[tag.Value] = true
	}
	set(result, "plugins", plugins)
	if policies := get(s, "device_policies"); len(items(policies)) > 0 {
		return PatchDocument(result, L(), policies, nil)
	}
	return result, nil
}
