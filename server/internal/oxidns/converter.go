package oxidns

import (
	"configflow/internal/textx"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/yamlx"
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

// RulePath is the generated resource of one ruleset part, for a stored rule
// object whose id may be of any JSON type.
func RulePath(item *Node, part string) string {
	return "./rules/" + TagFor("rule", get(item, "id")) + "_" + part + ".txt"
}

// rulePath is the generated resource of one part of the ruleset with this id.
func rulePath(id, part string) string {
	return "./rules/" + tagForString("rule", id) + "_" + part + ".txt"
}

// base is the effective panel origin: system_config.server_domain or baseURL.
func (p *Profile) base(baseURL string) string {
	if domain := p.source.SystemConfig.ServerDomain; domain != "" {
		baseURL = domain
	}
	return strings.TrimRight(baseURL, "/")
}

// activeItems is _items: enabled rule_configs, or the legacy rules/rule sets.
func (p *Profile) activeItems() []model.RuleConfig {
	out := []model.RuleConfig{}
	if configs := p.ruleConfigs(); len(configs) > 0 {
		for _, item := range configs {
			if ruleActive(item) {
				out = append(out, item)
			}
		}
		return out
	}
	rules, sets, err := mosdns.SplitRules(p.source)
	if err != nil {
		return out
	}
	for _, pair := range []struct {
		kind string
		list []model.RuleConfig
	}{{"rule", rules}, {"ruleset", sets}} {
		for _, item := range pair.list {
			item.ItemType = pair.kind
			out = append(out, item)
		}
	}
	return out
}

// ruleActive: the rule and its library source are enabled (absent means on).
func ruleActive(item model.RuleConfig) bool {
	return model.Flag(item.Enabled, true) && model.Flag(item.LibraryEnabled, true)
}

// enabledOrDefault defaults an absent enable flag to true and evaluates stored
// non-boolean values through the shared JSON switch rule.
func enabledOrDefault(item *Node, key string) bool {
	if !has(item, key) {
		return true
	}
	value := yamlx.Resolve(get(item, key))
	if isBool(value) {
		return value.Value == "true"
	}
	raw, err := yamlx.ToJSON(value)
	if err != nil {
		return false
	}
	return model.Switch(raw, true)
}

func suffixOf(item model.RuleConfig) string {
	if item.ItemType == "ruleset" {
		return "rulesets"
	}
	return "rules"
}

// inSelector is `identity in selector` for a selector list (or mapping).
func inSelector(selector *Node, identity string) bool {
	if isMap(selector) {
		return has(selector, identity)
	}
	return containsStr(selector, identity)
}

type selection struct {
	item  model.RuleConfig
	route string
}

// selected is _selected: (item, route) for the routed candidates; a rule DNS
// action wins over the selector lists. candidates nil means every active rule.
func (p *Profile) selected(settings *Node, candidates []model.RuleConfig) ([]selection, error) {
	actions := get(settings, "rule_actions")
	if candidates == nil {
		candidates = p.activeItems()
	}
	var out []selection
	for _, item := range candidates {
		suffix := suffixOf(item)
		switch {
		case has(actions, item.ID):
			out = append(out, selection{item, RouteKey(get(actions, item.ID))})
		case inSelector(get(settings, "direct_"+suffix), item.ID):
			out = append(out, selection{item, "direct"})
		case inSelector(get(settings, "proxy_"+suffix), item.ID):
			out = append(out, selection{item, "proxy"})
		}
	}
	return out, nil
}

// UnavailableRulesets explains, one line per ruleset, why referenced
// generated rule files cannot be produced.
func UnavailableRulesets(p *Profile, paths []string) []string {
	rulesets := map[string]model.RuleConfig{}
	for _, item := range p.ruleConfigs() {
		if item.ItemType == "ruleset" {
			rulesets[tagForString("rule", item.ID)] = item
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
		item, ok := rulesets[m[1]]
		if !ok {
			deleted = append(deleted, m[1])
			continue
		}
		label := item.Name
		if label == "" {
			label = item.ID
		}
		switch behavior := behaviorOf(item); {
		case !model.Flag(item.Enabled, true):
			lines = append(lines, "「"+label+"」已停用：请在规则管理中重新启用")
		case !model.Flag(item.LibraryEnabled, true):
			lines = append(lines, "「"+label+"」的规则来源已停用：请在规则库中重新启用")
		case !containsString(rulesetParts(behavior), m[2]):
			lines = append(lines, "「"+label+"」的类型被改成了 "+behavior+"：请改回原来的类型")
		default:
			lines = append(lines, "「"+label+"」没有下载地址：请检查规则来源")
		}
	}
	if len(deleted) > 0 {
		lines = append(lines, fmt.Sprintf("%d 个已删除的规则集（%s）：请在 OxiDNS 设置的 YAML 中删除含这些名称的行", len(deleted), strings.Join(deleted, "、")))
	}
	return lines
}

// RulesetDownload is one rule file the Agent downloads. Behavior is the
// ruleset's behavior ("" for AdGuard subscriptions).
type RulesetDownload struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
	Behavior  string `json:"-"`
}

// managedRulesetParts is ruleset_parts: managed routing only uses names.
func managedRulesetParts(behavior string) []string {
	if containsString(rulesetParts(behavior), "domain") {
		return []string{"domain"}
	}
	return nil
}

// behaviorOf is a ruleset's behavior; an absent one means domain.
func behaviorOf(item model.RuleConfig) string {
	if item.Behavior != nil {
		return *item.Behavior
	}
	return "domain"
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
		if item.ItemType != "ruleset" {
			continue
		}
		var parts []string
		if native != nil {
			for _, part := range rulesetParts(behaviorOf(item)) {
				if path, _, err := relativeResource(rulePath(item.ID, part)); err == nil && native[path] {
					parts = append(parts, part)
				}
			}
		} else {
			parts = managedRulesetParts(behaviorOf(item))
		}
		if len(parts) == 0 {
			continue
		}
		link := model.TextValue(item.URL)
		if link == "" {
			return nil, fail("已选择的规则集缺少下载来源")
		}
		if strings.HasPrefix(link, "/") {
			link = effective + link
		}
		name := "规则集"
		if item.Name != "" {
			name = item.Name
		}
		for _, part := range parts {
			proxied, err := p.ruleProxyURL(effective, link, part)
			if err != nil {
				return nil, err
			}
			downloads = append(downloads, RulesetDownload{name, proxied, rulePath(item.ID, part), behaviorOf(item)})
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
			downloads = append(downloads, RulesetDownload{"AdGuard DNS 规则", displayValue(url), path, ""})
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
	for number, raw := range textx.SplitLines(text, true) {
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
func (p *Profile) routingGroups(settings *Node, legacy legacyMode, candidates []model.RuleConfig) (*routingGroups, error) {
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
		if item.ItemType == "ruleset" {
			parts := managedRulesetParts(behaviorOf(item))
			if legacy != legacyNone {
				parts = rulesetParts(behaviorOf(item))
			}
			for _, part := range parts {
				if part == "ip" && legacy == legacyContentAware && !hasIPPart(item, p) {
					continue
				}
				entries = append(entries, entry{part, M("files", L(rulePath(item.ID, part)))})
			}
		} else {
			kind, value := item.RuleType, model.TextValue(item.Value)
			part := "domain"
			if IsIPRuleType(kind) {
				part = "ip"
			}
			if part == "ip" && legacy == legacyNone {
				continue
			}
			if !containsString(dnsRuleTypes, kind) {
				return nil, fail("所选规则类型暂不能转换为 OxiDNS 规则：" + kind)
			}
			converted, err := convertRule(kind+","+value, part)
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
	t, err := decodeTemplate(s)
	if err != nil {
		return "", err
	}
	result, err := templateDocument(t, p, baseURL)
	if err != nil {
		return "", err
	}
	return dump(deliveryNode(result))
}

func templateDocument(s *templateSettings, p *Profile, baseURL string) (*Node, error) {
	custom := M()
	if text := s.CustomConfig; strings.TrimSpace(text) != "" {
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
	set(merged, "worker_threads", I(s.WorkerThreads))
	set(result, "runtime", merged)
	del(result, "api")
	if api := s.API; api.Enabled {
		http := M("listen", api.Listen)
		if api.Username != "" {
			set(http, "auth", M("type", "basic", "username", api.Username, "password", api.Password))
		}
		if api.WebUIRoot != "" {
			set(http, "webui", M("root", api.WebUIRoot, "index", "index.html"))
		}
		if isMap(api.SSL) && mapSize(api.SSL) > 0 {
			set(http, "ssl", clone(api.SSL))
		}
		set(result, "api", M("http", http))
	}
	logOut := M("level", s.Log.Level)
	if s.Log.File != "" {
		set(logOut, "file", S(s.Log.File))
		set(logOut, "rotation", M("type", s.Log.Rotation, "max_files", s.Log.MaxFiles))
	}
	set(result, "log", logOut)
	plugins := L()
	add := func(tag, kind string, args *Node) {
		plugins.Content = append(plugins.Content, M("tag", tag, "type", kind, "args", args))
	}
	groups, err := p.routingGroups(s.node, legacyNone, nil)
	if err != nil {
		return nil, err
	}
	for _, g := range groups.Domain {
		add(g.Tag, "domain_set", g.Args)
	}
	for _, g := range groups.IP {
		add(g.Tag, "ip_set", g.Args)
	}
	hosts := s.CustomHosts
	if strings.TrimSpace(hosts) != "" {
		if _, err := HostsFileContent(hosts); err != nil {
			return nil, err
		}
		add("custom_hosts", "hosts", M("files", L("./rules/custom_hosts.txt")))
	}
	if s.CacheEnabled {
		cache := M("size", s.CacheSize)
		if s.CacheLazyTTL > 0 {
			set(cache, "lazy_cache_ttl", I(s.CacheLazyTTL))
		}
		set(cache, "short_circuit", B(true))
		if s.CacheDumpEnabled {
			set(cache, "dump_file", S(s.CacheDumpFile))
			set(cache, "dump_interval", I(s.CacheDumpInterval))
		}
		add("lazy_cache", "cache", cache)
	}
	if s.ProbeCacheEnabled {
		add("local_probe_cache", "cache", M("size", s.ProbeCacheSize, "short_circuit", false))
	}
	fallback, err := fallbackUpstreams(s.FallbackDNS)
	if err != nil {
		return nil, err
	}
	for _, forward := range []struct {
		name       string
		text       string
		defaults   []string
		concurrent int64
	}{{"local", s.LocalDNS, LocalDNSDefaults, s.LocalConcurrent}, {"remote", s.RemoteDNS, RemoteDNSDefaults, s.RemoteConcurrent}} {
		upstreams, err := forwardUpstreams(forward.text, forward.defaults)
		if err != nil {
			return nil, err
		}
		add("forward_"+forward.name, "forward", M("concurrent", forward.concurrent, "upstreams", upstreams))
	}
	hasFallback := len(fallback.Content) > 0
	if hasFallback {
		add("forward_fallback", "forward", M("concurrent", s.FallbackConcurrent, "upstreams", fallback))
	}
	dynamic := s.DynamicDomainSet
	dynamicOn, learn := dynamic.Enabled, dynamic.Learn
	if dynamicOn {
		add("dynamic_domains", "dynamic_domain_set", clone(dynamic.Args))
		if learn {
			args := clone(dynamic.LearnArgs)
			set(args, "provider", S("dynamic_domains"))
			add("learn_classified_domain", "learn_domain", args)
		}
	}
	sections := map[string]pluginSection{"query_recorder": s.QueryRecorder, "ip_selector": s.IPSelector, "ros_address_list": s.ROSAddressList, "ros_route": s.ROSRoute}
	enabled := func(name string) bool { return sections[name].Enabled }
	for _, name := range []string{"query_recorder", "ip_selector", "ros_address_list", "ros_route"} {
		if !enabled(name) {
			continue
		}
		args := clone(sections[name].Args)
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
	adguard := s.AdGuard
	adguardOn := adguard.Enabled
	if adguardOn {
		paths := L()
		if strings.TrimSpace(adguard.Content) != "" {
			paths.Content = append(paths.Content, S("./rules/adguard-custom.txt"))
		}
		for _, url := range adguard.URLs {
			paths.Content = append(paths.Content, S("./rules/"+tagForString("adguard", url)+".txt"))
		}
		add("adguard_rules", "adguard_rule", M("files", paths))
		add("adguard_block", "black_hole", M("mode", adguard.Mode))
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
	if s.CacheEnabled {
		direct.Content = append(direct.Content, M("exec", "$lazy_cache"))
	}
	direct.Content = append(direct.Content, M("exec", "$forward_local"), M("matches", L("!has_resp"), "exec", "reject 3"))
	add("china_dns", "sequence", direct)
	probe := L()
	if s.ProbeCacheEnabled {
		probe.Content = append(probe.Content, M("exec", "$local_probe_cache"))
	}
	probe.Content = append(probe.Content, M("matches", L("!has_resp"), "exec", "$forward_local"), M("exec", "return"))
	add("local_probe", "sequence", probe)
	remote := L(M("exec", "$forward_remote"), M("matches", L("!has_resp"), "exec", "reject 3"))
	if s.RemoteTTL > 0 {
		remote.Content = append(remote.Content, M("exec", "ttl "+strconv.FormatInt(s.RemoteTTL, 10)))
	}
	add("remote_dns", "sequence", remote)
	if hasFallback {
		add("fallback_dns", "sequence", L(M("exec", "$forward_fallback"), M("matches", L("!has_resp"), "exec", "reject 3")))
		add("fallback", "fallback", M("primary", "remote_dns", "secondary", "fallback_dns",
			"threshold", s.FallbackThreshold, "always_standby", s.AlwaysStandby))
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
		if dynamic.Route == "direct" {
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
	helpers, err := HelperPlugins(routes, get(s.node, "rule_actions"), s.features())
	if err != nil {
		return nil, err
	}
	plugins.Content = append(plugins.Content, helpers...)

	main := L()
	if s.MetricsEnabled {
		main.Content = append(main.Content, M("exec", "metrics_collector metrics"))
	}
	if enabled("query_recorder") {
		main.Content = append(main.Content, M("exec", "$query_recorder"))
	}
	if s.PreferIPv4 {
		main.Content = append(main.Content, M("exec", "prefer_ipv4"))
	}
	if blocked := s.BlockQtypes; len(blocked) > 0 {
		codes := make([]string, len(blocked))
		for i, code := range blocked {
			codes[i] = strconv.FormatInt(code, 10)
		}
		main.Content = append(main.Content, M("matches", L("qtype "+strings.Join(codes, " ")), "exec", BlockQtypesExec))
	}
	if strings.TrimSpace(hosts) != "" {
		main.Content = append(main.Content, M("exec", "$custom_hosts"), M("matches", L("has_resp"), "exec", "accept"))
	}
	if adguardOn {
		main.Content = append(main.Content, M("matches", L("question $adguard_rules"), "exec", "goto adguard_reject"))
	}
	matches, err := parseCustomMatches(s.CustomMatches)
	if err != nil {
		return nil, err
	}
	head := s.CustomMatchPosition == "head"
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
	if s.CustomMatchPosition == "tail" {
		main.Content = append(main.Content, matches.Content...)
	}
	if dynamicOn {
		target := "goto proxy_dns_seq"
		if dynamic.Route == "direct" {
			target = "goto china_dns"
		}
		main.Content = append(main.Content, M("matches", L("qname $dynamic_domains"), "exec", target))
	}
	final := "goto proxy_dns_seq"
	if s.DefaultForward == "forward_local" {
		final = "goto china_dns"
	}
	main.Content = append(main.Content, M("exec", final))
	add("sequence_main", "sequence", main)
	add("server_udp", "udp_server", M("listen", s.Listen, "entry", "sequence_main"))
	if s.TCPEnabled {
		add("server_tcp", "tcp_server", M("listen", s.Listen, "entry", "sequence_main"))
	}

	downloads, err := rulesetDownloads(s.node, p, baseURL)
	if err != nil {
		return nil, err
	}
	customUpdates := false
	for _, plugin := range extra.Content {
		customUpdates = customUpdates || isMap(plugin) && (isOneOf(get(plugin, "type"), "download", "reload_provider") ||
			isOneOf(get(plugin, "tag"), "rules_download", "rules_reload", "rules_refresh"))
	}
	update := s.RuleUpdate
	updateOn := update.Enabled
	if updateOn && customUpdates {
		return nil, fail("已有自定义下载或重载插件，请在高级 YAML 中配置规则定时更新，不能自动创建第二条维护链")
	}
	if updateOn && len(downloads) == 0 {
		return nil, fail("定时更新需要至少一个规则订阅")
	}
	if len(downloads) > 0 && !customUpdates {
		updateArgs := update.Args
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
	policies, err := parsePolicies(get(s.node, "device_policies"))
	if err != nil {
		return nil, err
	}
	if len(policies) > 0 {
		return PatchDocument(result, nil, policies, nil)
	}
	return result, nil
}
