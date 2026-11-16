package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/pystr"
	"configflow/internal/yamlx"
)

// Port of backend/converters/oxidns.py (module C22): OxiDNS templates with
// ordered routing, an isolated probe cache and opt-in features. Rule text
// conversion is shared with MosDNS (internal/mosdns).

var (
	// LocalDNSDefaults is LOCAL_DNS_DEFAULTS.
	LocalDNSDefaults = []string{"udp://223.5.5.5", "udp://119.29.29.29"}
	// RemoteDNSDefaults is REMOTE_DNS_DEFAULTS.
	RemoteDNSDefaults = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}

	ruleFile = regexp.MustCompile(`^rules/(rule_[0-9a-f]{16})_(domain|ip)\.txt$`)
)

// UnsupportedYAMLError reports YAML in the OxiDNS settings (custom_config,
// DNS upstream definitions) that PyYAML loads into values the jsonx model
// cannot hold (mappings with non-string keys, !!set/!!omap/!!pairs,
// collections that contain themselves) and that Python would copy into the
// generated configuration. The Go server refuses instead of writing other
// bytes (same rule as mosdns.UnsupportedYAMLError).
type UnsupportedYAMLError = mosdns.UnsupportedYAMLError

func unsupportedYAML(where string, err error) {
	raise(&UnsupportedYAMLError{Msg: "OxiDNS " + where + " 含有无法转换的 YAML 值：" + err.Error()})
}

func mosdnsRuleTypes() []string { return mosdns.DNSRuleTypes }

// TagFor is _tag(prefix, identity): prefix + "_" + the first 16 hex digits
// of sha256(str(identity)).
func TagFor(prefix string, identity any) string {
	sum := sha256.Sum256([]byte(str(identity)))
	return prefix + "_" + hex.EncodeToString(sum[:])[:16]
}

// settingsOf is _settings(config).
func settingsOf(config *jsonx.Object) *jsonx.Object {
	return normalizeSettings(config.GetOr("oxidns", jsonx.NewObject()), true)
}

// Base is _base(config, base_url): system_config.server_domain or base_url,
// without trailing slashes.
func Base(config *jsonx.Object, baseURL string) (base string, err error) {
	defer catch(&err)
	return baseOf(config, baseURL), nil
}

func baseOf(config *jsonx.Object, baseURL string) string {
	var value any = baseURL
	if domain := get(get(config, "system_config", jsonx.NewObject()), "server_domain", nil); truthy(domain) {
		value = domain
	}
	text, ok := value.(string)
	if !ok {
		raise(attributeError(value, "rstrip"))
	}
	return strings.TrimRight(text, "/")
}

// Items is _items(config): the active rule_configs entries, or (for a profile
// without them) the split rules and rule sets marked with itemType.
func Items(config *jsonx.Object) (items []any, err error) {
	defer catch(&err)
	return itemsOf(config), nil
}

func itemsOf(config *jsonx.Object) []any {
	if truthy(config.Value("rule_configs")) {
		var out []any
		for _, entry := range iterate(config.Value("rule_configs")) {
			if truthy(get(entry, "enabled", true)) && truthy(get(entry, "library_enabled", true)) {
				out = append(out, entry)
			}
		}
		return out
	}
	rules, sets, err := mosdns.SplitRulesAndRulesets(config)
	must(err)
	var out []any
	for _, pair := range []struct {
		list any
		kind string
	}{{rules, "rule"}, {sets, "ruleset"}} {
		for _, entry := range iterate(pair.list) {
			object, ok := entry.(*jsonx.Object)
			if !ok || object == nil {
				raise(pyErr("TypeError", "'"+typeName(entry)+"' object is not iterable"))
			}
			clone := object.Clone()
			clone.Set("itemType", pair.kind)
			out = append(out, clone)
		}
	}
	return out
}

// containerHas is `identity in container` for a list or dict selector.
func containerHas(container any, identity any) bool {
	switch x := container.(type) {
	case []any:
		return listContains(x, identity)
	case *jsonx.Object:
		hashable(identity)
		text, ok := identity.(string)
		return ok && x.Has(text)
	case string:
		text, ok := identity.(string)
		if !ok {
			raise(pyErr("TypeError", "'in <string>' requires string as left operand, not "+typeName(identity)))
		}
		return strings.Contains(x, text)
	}
	raise(pyErr("TypeError", "argument of type '"+typeName(container)+"' is not iterable"))
	return false
}

// selected is the _selected(config, settings, items) generator: yield is
// called for each routed item, lazily as in Python (an error of a later item
// comes after the work done for the earlier ones). items nil is None.
func selected(config *jsonx.Object, settings any, items []any, itemsGiven bool, yield func(item any, route string)) {
	actions := orEmpty(get(settings, "rule_actions", nil))
	if !itemsGiven {
		items = itemsOf(config)
	}
	for _, entry := range items {
		suffix := "rules"
		if get(entry, "itemType", nil) == "ruleset" {
			suffix = "rulesets"
		}
		identity := get(entry, "id", nil)
		if text, ok := identity.(string); ok && has(actions, text) {
			yield(entry, routeKey(item(actions, text)))
		} else if containerHas(item(settings, "direct_"+suffix), identity) {
			yield(entry, "direct")
		} else if containerHas(item(settings, "proxy_"+suffix), identity) {
			yield(entry, "proxy")
		}
	}
}

// RulePath is _rule_path(item, part): ./rules/<tag>_<part>.txt.
func RulePath(item *jsonx.Object, part string) (path string, err error) {
	defer catch(&err)
	return rulePath(item, part), nil
}

func rulePath(entry any, part string) string {
	return "./rules/" + TagFor("rule", item(entry, "id")) + "_" + part + ".txt"
}

// UnavailableRulesets is unavailable_rulesets(config, paths): one short
// "what and where to fix" line per ruleset behind unusable rule files; paths
// that are not generated rule files are ignored.
func UnavailableRulesets(config *jsonx.Object, paths []any) (lines []string, err error) {
	defer catch(&err)
	return unavailableRulesets(config, paths), nil
}

func unavailableRulesets(config *jsonx.Object, paths []any) []string {
	rulesets := map[string]any{}
	for _, entry := range iterate(config.GetOr("rule_configs", []any{})) {
		if get(entry, "itemType", nil) == "ruleset" {
			rulesets[TagFor("rule", get(entry, "id", nil))] = entry
		}
	}
	lines := []string{}
	var deleted []string
	seen := map[string]bool{}
	for _, raw := range paths {
		text, ok := raw.(string)
		if !ok {
			raise(pyErr("TypeError", "expected string or bytes-like object, got '"+typeName(raw)+"'"))
		}
		match := ruleFile.FindStringSubmatch(text)
		if match == nil || seen[match[1]] {
			continue
		}
		tag, part := match[1], match[2]
		seen[tag] = true
		entry, ok := rulesets[tag]
		if !ok {
			deleted = append(deleted, tag)
			continue
		}
		name := get(entry, "name", nil)
		if !truthy(name) {
			name = get(entry, "id", nil)
		}
		behavior := get(entry, "behavior", "domain")
		switch {
		case !truthy(get(entry, "enabled", true)):
			lines = append(lines, "「"+str(name)+"」已停用：请在规则管理中重新启用")
		case !truthy(get(entry, "library_enabled", true)):
			lines = append(lines, "「"+str(name)+"」的规则来源已停用：请在规则库中重新启用")
		case !strIn(part, mosdns.RulesetParts(behavior)...):
			lines = append(lines, "「"+str(name)+"」的类型被改成了 "+str(behavior)+"：请改回原来的类型")
		default:
			lines = append(lines, "「"+str(name)+"」没有下载地址：请检查规则来源")
		}
	}
	if len(deleted) > 0 {
		lines = append(lines, itoa(len(deleted))+" 个已删除的规则集（"+strings.Join(deleted, "、")+"）："+
			"请在 OxiDNS 设置的 YAML 中删除含这些名称的行")
	}
	return lines
}

// GetOxidnsRulesetDownloads is get_oxidns_ruleset_downloads(config_data,
// base_url): the rule files the deployment downloads ({name, url,
// local_path, behavior}; AdGuard lists {name, url, local_path}).
func GetOxidnsRulesetDownloads(config *jsonx.Object, baseURL string) (downloads []any, err error) {
	defer catch(&err)
	return rulesetDownloads(config, baseURL), nil
}

func rulesetDownloads(config *jsonx.Object, baseURL string) []any {
	settings := settingsOf(config)
	var nativePaths map[string]bool
	if settings.Value("config_mode") == "native" {
		var document *jsonx.Object
		document, settings = migrateDomainOnlyRouting(nativeDocument(settings, true, nil), settings, config, baseURL)
		nativePaths = map[string]bool{}
		for _, reference := range resourceReferences(document) {
			if p := relativeResource(reference); p != nil {
				nativePaths[*p] = true
			}
		}
	}
	effective := baseOf(config, baseURL)
	downloads := []any{}
	selections := jsonx.NewObject()
	for key, value := range settings.All() {
		if _, isList := value.([]any); isList || isDict(value) {
			selections.Set(key, value)
		} else {
			selections.Set(key, []any{})
		}
	}
	// Native YAML is the routing source of truth; its stored selectors are only
	// a retained catalog, so every referenced active ruleset is downloaded.
	handle := func(entry any, _ string) {
		if get(entry, "itemType", nil) != "ruleset" {
			return
		}
		var parts []string
		if nativePaths != nil {
			// Native YAML decides; an older document may still reference an IP part.
			for _, part := range mosdns.RulesetParts(get(entry, "behavior", "domain")) {
				if p := relativeResource(rulePath(entry, part)); p != nil && nativePaths[*p] {
					parts = append(parts, part)
				}
			}
		} else {
			parts = rulesetPartsOf(entry)
		}
		if len(parts) == 0 {
			return
		}
		url := get(entry, "url", "")
		if !truthy(url) {
			fail("已选择的规则集缺少下载来源")
		}
		text, ok := url.(string)
		if !ok {
			raise(attributeError(url, "startswith"))
		}
		if strings.HasPrefix(text, "/") {
			text = effective + text
		}
		for _, part := range parts {
			name := get(entry, "name", nil)
			if !truthy(name) {
				name = "规则集"
			}
			proxyURL, err := mosdns.BuildRuleProxyURL(config, effective, text, part, nil)
			must(err)
			downloads = append(downloads, jsonx.Obj("name", name, "url", proxyURL,
				"local_path", rulePath(entry, part), "behavior", get(entry, "behavior", "domain")))
		}
	}
	if nativePaths != nil {
		for _, entry := range itemsOf(config) {
			handle(entry, "")
		}
	} else {
		selected(config, selections, nil, false, handle)
	}
	adguard := settings.Value("adguard")
	if !isDict(adguard) {
		adguard = jsonx.NewObject()
	}
	if urls, ok := get(adguard, "urls", nil).([]any); truthy(get(adguard, "enabled", nil)) && ok {
		for _, url := range urls {
			localPath := "./rules/" + TagFor("adguard", url) + ".txt"
			if nativePaths != nil {
				if p := relativeResource(localPath); p == nil || !nativePaths[*p] {
					continue
				}
			}
			downloads = append(downloads, jsonx.Obj("name", "AdGuard DNS 规则", "url", url, "local_path", localPath))
		}
	}
	return downloads
}

// GetOxidnsCustomFiles is get_oxidns_custom_files(config_data): the files
// written next to the configuration ({path, content}; managed attachments
// {path, content: []byte, native: true}).
func GetOxidnsCustomFiles(config *jsonx.Object) (files []any, err error) {
	defer catch(&err)
	return customFiles(config), nil
}

func customFiles(config *jsonx.Object) []any {
	settings := settingsOf(config)
	var nativePaths map[string]bool
	if settings.Value("config_mode") == "native" {
		nativePaths = map[string]bool{}
		for _, reference := range resourceReferences(nativeDocument(settings, true, nil)) {
			if p := relativeResource(reference); p != nil {
				nativePaths[*p] = true
			}
		}
	}
	files := []any{}
	if hosts, ok := settings.Value("custom_hosts").(string); (nativePaths == nil || nativePaths["rules/custom_hosts.txt"]) && ok && strip(hosts) != "" {
		files = append(files, jsonx.Obj("path", "./rules/custom_hosts.txt", "content", hostsFileContent(hosts)))
	}
	adguard := settings.Value("adguard")
	if !isDict(adguard) {
		adguard = jsonx.NewObject()
	}
	if nativePaths == nil || nativePaths["rules/adguard-custom.txt"] {
		if content, ok := get(adguard, "content", nil).(string); truthy(get(adguard, "enabled", nil)) && ok && strip(content) != "" {
			files = append(files, jsonx.Obj("path", "./rules/adguard-custom.txt", "content", content))
		}
	}
	for _, file := range managedFiles(item(settings, "additional_files")) {
		files = append(files, file.Object())
	}
	return files
}

// HostsFileContent is _hosts_file_content(text): native OxiDNS hosts rows
// kept intact, unambiguous IP-first rows converted (one full-match rule per
// name), anything else OxidnsSettingsError with the line number.
func HostsFileContent(text string) (content string, err error) {
	defer catch(&err)
	return hostsFileContent(text), nil
}

var (
	legacyName = regexp.MustCompile(`^[A-Za-z0-9_](?:[A-Za-z0-9_.-]*[A-Za-z0-9_])?\.?$`)
)

// holdsMapping reports a *yamlx.Mapping (a mapping with non-string keys)
// anywhere in v.
func holdsMapping(v any) bool {
	switch x := v.(type) {
	case *yamlx.Mapping:
		return true
	case *jsonx.Object:
		for _, value := range x.Values() {
			if holdsMapping(value) {
				return true
			}
		}
	case []any:
		for _, value := range x {
			if holdsMapping(value) {
				return true
			}
		}
	}
	return false
}

func parseIP(value string) (any, bool) { return convert.ParseIPAddress(value) }

func isDecimal(r rune) bool { return pystr.Decimal(r) >= 0 }

func isSpaceRune(r rune) bool { return pystr.IsSpace(r) }

func hostsIsIP(value string) bool {
	// Python accepts scoped IPv6, but Rust's IpAddr used by OxiDNS does not.
	if strings.Contains(value, "%") {
		return false
	}
	_, ok := parseIP(value)
	return ok
}

// digitsAndDots is re.fullmatch(r'[\d.]+', value) (\d: Unicode decimal
// digits).
func digitsAndDots(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r != '.' && !isDecimal(r) {
			return false
		}
	}
	return true
}

func hostsLegacyName(value string) bool {
	for _, prefix := range []string{"full:", "domain:", "keyword:", "regexp:"} {
		if strings.HasPrefix(value, prefix) {
			_, rest, _ := strings.Cut(value, ":")
			return rest != ""
		}
	}
	if digitsAndDots(value) {
		return false // Do not turn a mistyped IPv4 address into an alias.
	}
	return legacyName.MatchString(value)
}

func hostsFileContent(text string) string {
	var output strings.Builder
	for number, raw := range splitlinesKeepEnds(text) {
		line := strings.TrimRight(raw, "\r\n")
		ending := raw[len(line):]
		rule, comment, marker := strings.Cut(line, "#")
		fields := pySplit(rule)
		if len(fields) == 0 {
			output.WriteString(raw)
			continue
		}
		allIP := len(fields) >= 2
		for _, value := range fields[1:] {
			if !hostsIsIP(value) {
				allIP = false
				break
			}
		}
		if allIP {
			// Keep spelling, whitespace, address order, duplicates and comments.
			output.WriteString(raw)
			continue
		}
		convertible := len(fields) >= 2 && hostsIsIP(fields[0])
		if convertible {
			for _, value := range fields[1:] {
				if hostsIsIP(value) || !hostsLegacyName(value) {
					convertible = false
					break
				}
			}
		}
		if convertible {
			converted := make([]string, 0, len(fields)-1)
			for _, name := range fields[1:] {
				converted = append(converted, name+" "+fields[0])
			}
			if marker {
				converted[0] += " #" + comment
			}
			separator := ending
			if separator == "" {
				separator = "\n"
			}
			output.WriteString(strings.Join(converted, separator) + ending)
			continue
		}
		fail("自定义 Hosts 第 " + itoa(number+1) + " 行格式无效；请使用“域名规则 IPv4/IPv6 [更多 IP]”" +
			"，或“IP 域名 [别名]”，不能混合地址与别名")
	}
	return output.String()
}

// RulesetParts is ruleset_parts(item, config): the parts managed DNS routing
// uses for a rule set — names only, never addresses returned by an upstream.
func RulesetParts(item *jsonx.Object) []string { return rulesetPartsOf(item) }

func rulesetPartsOf(entry any) []string {
	var parts []string
	for _, part := range mosdns.RulesetParts(get(entry, "behavior", "domain")) {
		if part == "domain" {
			parts = append(parts, part)
		}
	}
	return parts
}

// Legacy selects the historical routing shapes of _routing_groups.
type Legacy int

const (
	// LegacyNone is legacy=False: the managed domain groups.
	LegacyNone Legacy = iota
	// LegacyAll is legacy=True: every part, from before content-aware IP parts.
	LegacyAll
	// LegacyContentAware is legacy='content-aware': the intermediate format.
	LegacyContentAware
)

// RoutingGroups is _routing_groups(config, settings, legacy=legacy,
// items=items): {domain: [...], ip: [...]} of groups {route, args, tag}.
// items nil (with itemsGiven false) is None: every active rule of config.
func RoutingGroups(config, settings *jsonx.Object, legacy Legacy, items []any, itemsGiven bool) (groups *jsonx.Object, err error) {
	defer catch(&err)
	return routingGroups(config, settings, legacy, items, itemsGiven), nil
}

func routingGroups(config *jsonx.Object, settings any, legacy Legacy, items []any, itemsGiven bool) *jsonx.Object {
	domain, ip := []any{}, []any{}
	groups := map[string]*[]any{"domain": &domain, "ip": &ip}
	selected(config, settings, items, itemsGiven, func(entry any, route string) {
		type part struct {
			name string
			args *jsonx.Object
		}
		var entries []part
		if get(entry, "itemType", nil) == "ruleset" {
			var parts []string
			if legacy != LegacyNone {
				parts = mosdns.RulesetParts(get(entry, "behavior", "domain"))
			} else {
				parts = rulesetPartsOf(entry)
			}
			if legacy == LegacyContentAware && !hasIPPart(entry, config) {
				var kept []string
				for _, p := range parts {
					if p != "ip" {
						kept = append(kept, p)
					}
				}
				parts = kept
			}
			for _, p := range parts {
				entries = append(entries, part{p, jsonx.Obj("files", []any{rulePath(entry, p)})})
			}
		} else {
			kind, value := get(entry, "rule_type", ""), get(entry, "value", "")
			name := "domain"
			if strIn(kind, IPRuleTypes...) {
				name = "ip"
			}
			if name == "ip" && legacy == LegacyNone {
				return
			}
			if !strIn(kind, mosdns.DNSRuleTypes...) {
				fail("所选规则类型暂不能转换为 OxiDNS 规则：" + str(kind))
			}
			out, err := mosdns.ConvertRuleContentForMosDNS(kind.(string)+","+str(value), name)
			must(err)
			converted := strip(out)
			if converted == "" || strings.HasPrefix(converted, "#") {
				fail("所选单条规则无法转换为 DNS 规则")
			}
			key := "exps"
			if name == "ip" {
				key = "ips"
			}
			entries = append(entries, part{name, jsonx.Obj(key, []any{converted})})
		}
		for _, e := range entries {
			target := groups[e.name]
			if truthy(item(settings, "aggregate_rules")) && len(*target) > 0 && get((*target)[len(*target)-1], "route", nil) == route {
				group := (*target)[len(*target)-1].(*jsonx.Object)
				args := group.GetObject("args")
				for key, values := range e.args.All() {
					existing, _ := args.SetDefault(key, []any{}).([]any)
					args.Set(key, append(existing, values.([]any)...))
				}
			} else {
				*target = append(*target, jsonx.Obj("route", route, "args", e.args, "tag", "rules_"+e.name+"_"+itoa(len(*target)+1)))
			}
		}
	})
	return jsonx.Obj("domain", domain, "ip", ip)
}

// textStartsWithYAML is text.lstrip().startswith(('-', '[', '{', 'addr:')).
func textStartsWithYAML(text string) bool {
	trimmed := strings.TrimLeftFunc(text, isSpaceRune)
	for _, prefix := range []string{"-", "[", "{", "addr:"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// safeLoadKeep is yaml.safe_load(text) for the upstream checks: mappings
// with non-string keys are kept (*yamlx.Mapping). ok=false is a
// yaml.YAMLError; other PyYAML exceptions are raised as Python's.
func safeLoadKeep(text, where string) (any, bool) {
	value, err := yamlx.LoadWith(text, yamlx.LoadOptions{KeepNonStringKeys: true})
	if err != nil {
		switch e := err.(type) {
		case *yamlx.Error:
			if e.IsYAMLError() {
				return nil, false
			}
			raise(pyErr(e.Class, e.Msg))
		case *yamlx.UnrepresentableError:
			unsupportedYAML(where, err)
		}
		raise(err)
	}
	return value, true
}

// Forward is _forward(text, defaults): the upstreams of one forward plugin;
// an empty text gives the defaults.
func Forward(text string, defaults []string) (upstreams []any, err error) {
	defer catch(&err)
	return forward(text, defaults), nil
}

func forward(text string, defaults []string) []any {
	if strip(text) == "" {
		out := []any{}
		for _, value := range defaults {
			out = append(out, jsonx.Obj("addr", value))
		}
		return out
	}
	if textStartsWithYAML(text) {
		parsed, ok := safeLoadKeep(text, "DNS 上游")
		if !ok {
			fail("DNS 上游 YAML 语法无效")
		}
		values, isList := parsed.([]any)
		if !isList {
			values = []any{parsed}
		}
		valid := len(values) > 0
		for _, value := range values {
			if isDict(value) {
				if !truthy(get(value, "addr", nil)) {
					valid = false
				}
			} else if !isStr(value) {
				valid = false
			}
		}
		if !valid {
			fail("DNS 上游 YAML 每项需要 addr 地址")
		}
	}
	result, err := mosdns.ParseDNSUpstreams(text)
	must(err)
	valid := len(result) > 0
	for _, upstream := range result {
		addr, ok := get(upstream, "addr", nil).(string)
		if !ok || strip(addr) == "" {
			valid = false
			break
		}
	}
	if !valid {
		fail("DNS 上游配置无效")
	}
	return result
}

// FallbackUpstreams is _fallback_upstreams(text): an empty backup is
// disabled, never an implicit copy of local DNS.
func FallbackUpstreams(text string) (upstreams []any, err error) {
	defer catch(&err)
	return fallbackUpstreams(text), nil
}

func fallbackUpstreams(text string) []any {
	if strip(text) == "" {
		return []any{}
	}
	if textStartsWithYAML(text) || strIn(strip(text), "null", "~") {
		value, ok := safeLoadKeep(text, "DNS 上游")
		if !ok {
			fail("DNS 上游 YAML 语法无效")
		}
		if list, isList := value.([]any); value == nil || isList && len(list) == 0 {
			return []any{}
		}
	}
	return forward(text, nil)
}

// GenerateOxidnsConfig is generate_oxidns_config(config_data, base_url):
// the OxiDNS configuration YAML (yaml.safe_dump(allow_unicode=True,
// sort_keys=False)) pushed to the DNS servers. Errors: *Error, Python's
// crashes (*convert.PyError), *UnsupportedYAMLError, *PendingError (native
// mode and device policies until C23/C26 land).
func GenerateOxidnsConfig(config *jsonx.Object, baseURL string) (text string, err error) {
	defer catch(&err)
	return generateConfig(config, baseURL), nil
}

func dump(document *jsonx.Object) string {
	if holdsMapping(document) {
		unsupportedYAML("配置", &yamlx.NonStringKeyError{Msg: "mapping with a non-string key"})
	}
	text, err := yamlx.SafeDumpUnicode(document)
	must(err)
	return text
}

func generateConfig(config *jsonx.Object, baseURL string) string {
	s := settingsOf(config)
	if s.Value("config_mode") == "native" {
		document := prepareNativeRuleUpdates(nativeDocument(s, true, nil), s, config, baseURL)
		requireValidDocument(document)
		return dump(document)
	}
	var custom any = jsonx.NewObject()
	if customText := s.GetString("custom_config"); strip(customText) != "" {
		value, ok := safeLoadKeep(customText, "高级 YAML")
		if !ok {
			fail("OxiDNS 高级 YAML 语法无效")
		}
		if _, isMapping := value.(*yamlx.Mapping); isMapping {
			unsupportedYAML("高级 YAML", &yamlx.NonStringKeyError{Msg: "top-level mapping with a non-string key"})
		}
		custom = value
	}
	customObject, ok := custom.(*jsonx.Object)
	if !ok || customObject == nil {
		fail("OxiDNS 高级 YAML 顶层必须为对象")
	}
	if truthy(customObject.Value("include")) {
		fail("完整部署模板不支持外部 include；请将插件内联到高级 YAML")
	}
	var extraValue any = []any{}
	if value, present := customObject.Pop("plugins"); present {
		extraValue = value
	}
	extraPlugins, ok := extraValue.([]any)
	if !ok {
		fail("高级 YAML 的 plugins 必须为数组")
	}
	result := copyValue(customObject).(*jsonx.Object)
	if runtime := result.Value("runtime"); runtime != nil && !isDict(runtime) {
		fail("高级 YAML runtime 必须为对象")
	}
	runtime := jsonx.NewObject()
	if previous := result.Value("runtime"); truthy(previous) {
		for key, value := range previous.(*jsonx.Object).All() {
			runtime.Set(key, value)
		}
	}
	runtime.Set("worker_threads", s.Value("worker_threads"))
	result.Set("runtime", runtime)
	result.Delete("api")
	api := s.GetObject("api")
	if api.Value("enabled") == true {
		http := jsonx.Obj("listen", api.Value("listen"))
		if truthy(api.Value("username")) {
			http.Set("auth", jsonx.Obj("type", "basic", "username", api.Value("username"), "password", api.Value("password")))
		}
		if truthy(api.Value("webui_root")) {
			http.Set("webui", jsonx.Obj("root", api.Value("webui_root"), "index", "index.html"))
		}
		if truthy(api.Value("ssl")) {
			http.Set("ssl", copyValue(api.Value("ssl")))
		}
		result.Set("api", jsonx.Obj("http", http))
	}
	log := s.GetObject("log")
	resultLog := jsonx.Obj("level", log.Value("level"))
	result.Set("log", resultLog)
	if truthy(log.Value("file")) {
		resultLog.Set("file", log.Value("file"))
		resultLog.Set("rotation", jsonx.Obj("type", log.Value("rotation"), "max_files", log.Value("max_files")))
	}
	plugins := []any{}
	add := func(tag, kind string, args any) {
		plugins = append(plugins, jsonx.Obj("tag", tag, "type", kind, "args", args))
	}

	groups := routingGroups(config, s, LegacyNone, nil, false)
	for _, part := range []string{"domain", "ip"} {
		for _, group := range groups.GetList(part) {
			g := group.(*jsonx.Object)
			kind := "ip_set"
			if part == "domain" {
				kind = "domain_set"
			}
			add(g.GetString("tag"), kind, g.Value("args"))
		}
	}
	hosts := s.GetString("custom_hosts")
	if strip(hosts) != "" {
		hostsFileContent(hosts)
		add("custom_hosts", "hosts", jsonx.Obj("files", []any{"./rules/custom_hosts.txt"}))
	}
	if s.Value("cache_enabled") == true {
		cache := jsonx.Obj("size", s.Value("cache_size"))
		// OxiDNS rejects an explicit zero; omitting the field disables lazy cache.
		if greaterThanZero(s.Value("cache_lazy_ttl")) {
			cache.Set("lazy_cache_ttl", s.Value("cache_lazy_ttl"))
		}
		cache.Set("short_circuit", true)
		if s.Value("cache_dump_enabled") == true {
			cache.Set("dump_file", s.Value("cache_dump_file"))
			cache.Set("dump_interval", s.Value("cache_dump_interval"))
		}
		add("lazy_cache", "cache", cache)
	}
	if s.Value("probe_cache_enabled") == true {
		// This cache ends with the child sequence. It must capture the domestic
		// probe, never the final proxy/Fake-IP answer from its parent sequence.
		add("local_probe_cache", "cache", jsonx.Obj("size", s.Value("probe_cache_size"), "short_circuit", false))
	}
	fallback := fallbackUpstreams(s.GetString("fallback_dns"))
	for _, upstream := range []struct {
		name     string
		defaults []string
	}{{"local", LocalDNSDefaults}, {"remote", RemoteDNSDefaults}} {
		concurrent := s.Value(upstream.name + "_concurrent")
		add("forward_"+upstream.name, "forward", jsonx.Obj("concurrent", concurrent,
			"upstreams", forward(s.GetString(upstream.name+"_dns"), upstream.defaults)))
	}
	if len(fallback) > 0 {
		add("forward_fallback", "forward", jsonx.Obj("concurrent", s.Value("fallback_concurrent"), "upstreams", fallback))
	}
	dynamic := s.GetObject("dynamic_domain_set")
	dynamicOn := dynamic.Value("enabled") == true
	learn := dynamicOn && truthy(dynamic.Value("learn"))
	if dynamicOn {
		add("dynamic_domains", "dynamic_domain_set", copyValue(dynamic.Value("args")))
		if truthy(dynamic.Value("learn")) {
			args := copyValue(dynamic.Value("learn_args")).(*jsonx.Object)
			args.Set("provider", "dynamic_domains")
			add("learn_classified_domain", "learn_domain", args)
		}
	}
	for _, name := range []string{"query_recorder", "ip_selector", "ros_address_list", "ros_route"} {
		section := s.GetObject(name)
		if section.Value("enabled") != true {
			continue
		}
		args := copyValue(section.Value("args")).(*jsonx.Object)
		// Empty optional gateway fields are absent in native configuration.
		if name == "ros_route" {
			kept := jsonx.NewObject()
			for key, value := range args.All() {
				if value != "" {
					kept.Set(key, value)
				}
			}
			args = kept
		}
		add(name, name, args)
	}
	adguard := s.GetObject("adguard")
	adguardOn := adguard.Value("enabled") == true
	if adguardOn {
		paths := []any{}
		for _, url := range adguard.GetList("urls") {
			paths = append(paths, "./rules/"+TagFor("adguard", url)+".txt")
		}
		if strip(adguard.GetString("content")) != "" {
			paths = append([]any{"./rules/adguard-custom.txt"}, paths...)
		}
		add("adguard_rules", "adguard_rule", jsonx.Obj("files", paths))
		add("adguard_block", "black_hole", jsonx.Obj("mode", adguard.Value("mode")))
		add("adguard_reject", "sequence", []any{jsonx.Obj("exec", "$adguard_block"), jsonx.Obj("exec", "accept")})
	}

	realWrappers := func() []any {
		out := []any{}
		for _, name := range RealWrappers {
			if s.GetObject(name).Value("enabled") == true {
				out = append(out, jsonx.Obj("exec", "$"+name))
			}
		}
		return out
	}
	direct := realWrappers()
	if s.Value("cache_enabled") == true {
		direct = append(direct, jsonx.Obj("exec", "$lazy_cache"))
	}
	direct = append(direct, jsonx.Obj("exec", "$forward_local"), jsonx.Obj("matches", []any{"!has_resp"}, "exec", "reject 3"))
	add("china_dns", "sequence", direct)
	probe := []any{}
	if s.Value("probe_cache_enabled") == true {
		probe = append(probe, jsonx.Obj("exec", "$local_probe_cache"))
	}
	probe = append(probe, jsonx.Obj("matches", []any{"!has_resp"}, "exec", "$forward_local"), jsonx.Obj("exec", "return"))
	add("local_probe", "sequence", probe)
	remote := []any{jsonx.Obj("exec", "$forward_remote"), jsonx.Obj("matches", []any{"!has_resp"}, "exec", "reject 3")}
	if truthy(s.Value("remote_ttl")) {
		remote = append(remote, jsonx.Obj("exec", "ttl "+str(s.Value("remote_ttl"))))
	}
	add("remote_dns", "sequence", remote)
	if len(fallback) > 0 {
		add("fallback_dns", "sequence", []any{jsonx.Obj("exec", "$forward_fallback"), jsonx.Obj("matches", []any{"!has_resp"}, "exec", "reject 3")})
		add("fallback", "fallback", jsonx.Obj("primary", "remote_dns", "secondary", "fallback_dns",
			"threshold", s.Value("fallback_threshold"), "always_standby", s.Value("always_standby")))
		add("proxy_dns_seq", "sequence", []any{jsonx.Obj("exec", "$fallback"), jsonx.Obj("exec", "accept")})
	} else {
		add("proxy_dns_seq", "sequence", []any{jsonx.Obj("exec", "goto remote_dns")})
	}
	requery := []any{jsonx.Obj("exec", "drop_resp")}
	directIP := realWrappers()
	add("default_direct_ip", "sequence", append(realWrappers(), jsonx.Obj("exec", "accept")))
	if learn {
		if dynamic.Value("route") == "direct" {
			directIP = append(directIP, jsonx.Obj("exec", "$learn_classified_domain"))
		} else {
			requery = append(requery, jsonx.Obj("exec", "$learn_classified_domain"))
		}
	}
	directIP = append(directIP, jsonx.Obj("exec", "accept"))
	requery = append(requery, jsonx.Obj("exec", "goto proxy_dns_seq"))
	add("direct_ip", "sequence", directIP)
	add("ip_requery_proxy", "sequence", requery)
	// Blackhole and pinned upstream helpers exist only while a rule uses them.
	var routes []string
	for _, part := range []string{"domain", "ip"} {
		for _, group := range groups.GetList(part) {
			routes = append(routes, group.(*jsonx.Object).GetString("route"))
		}
	}
	plugins = append(plugins, helperPlugins(routes, s.GetObject("rule_actions"), s)...)

	main := []any{}
	if s.Value("metrics_enabled") == true {
		main = append(main, jsonx.Obj("exec", "metrics_collector metrics"))
	}
	if s.GetObject("query_recorder").Value("enabled") == true {
		main = append(main, jsonx.Obj("exec", "$query_recorder"))
	}
	if s.Value("prefer_ipv4") == true {
		main = append(main, jsonx.Obj("exec", "prefer_ipv4"))
	}
	if qtypes := s.GetList("block_qtypes"); len(qtypes) > 0 {
		names := make([]string, len(qtypes))
		for i, t := range qtypes {
			names[i] = str(t)
		}
		main = append(main, jsonx.Obj("matches", []any{"qtype " + strings.Join(names, " ")}, "exec", BlockQtypesExec))
	}
	if strip(hosts) != "" {
		main = append(main, jsonx.Obj("exec", "$custom_hosts"), jsonx.Obj("matches", []any{"has_resp"}, "exec", "accept"))
	}
	if adguardOn {
		main = append(main, jsonx.Obj("matches", []any{"question $adguard_rules"}, "exec", "goto adguard_reject"))
	}
	matches, err := mosdns.ParseCustomMatches(s.Value("custom_matches"))
	must(err)
	if s.Value("custom_match_position") == "head" {
		main = append(main, matches...)
	}
	for _, group := range groups.GetList("domain") {
		g := group.(*jsonx.Object)
		main = append(main, jsonx.Obj("matches", []any{"qname $" + g.GetString("tag")}, "exec", routeExec(g.GetString("route"), "domain")))
	}
	if s.Value("custom_match_position") == "tail" {
		main = append(main, matches...)
	}
	// Explicit rules override previously learned classifications.
	if dynamicOn {
		target := "goto proxy_dns_seq"
		if dynamic.Value("route") == "direct" {
			target = "goto china_dns"
		}
		main = append(main, jsonx.Obj("matches", []any{"qname $dynamic_domains"}, "exec", target))
	}
	// A DNS response must not decide where an otherwise unmatched name is sent.
	// IP rules remain available to connection routing; DNS falls back directly.
	if s.Value("default_forward") == "forward_local" {
		main = append(main, jsonx.Obj("exec", "goto china_dns"))
	} else {
		main = append(main, jsonx.Obj("exec", "goto proxy_dns_seq"))
	}
	add("sequence_main", "sequence", main)
	add("server_udp", "udp_server", jsonx.Obj("listen", s.Value("listen"), "entry", "sequence_main"))
	if s.Value("tcp_enabled") == true {
		add("server_tcp", "tcp_server", jsonx.Obj("listen", s.Value("listen"), "entry", "sequence_main"))
	}

	downloads := rulesetDownloads(config, baseURL)
	customUpdates := false
	for _, plugin := range extraPlugins {
		if isDict(plugin) && (strIn(get(plugin, "type", nil), "download", "reload_provider") ||
			strIn(get(plugin, "tag", nil), "rules_download", "rules_reload", "rules_refresh")) {
			customUpdates = true
			break
		}
	}
	ruleUpdate := s.GetObject("rule_update")
	if ruleUpdate.Value("enabled") == true && customUpdates {
		fail("已有自定义下载或重载插件，请在高级 YAML 中配置规则定时更新，不能自动创建第二条维护链")
	}
	if ruleUpdate.Value("enabled") == true && len(downloads) == 0 {
		fail("定时更新需要至少一个规则订阅")
	}
	if len(downloads) > 0 && !customUpdates {
		update := ruleUpdate.GetObject("args")
		args := jsonx.NewObject()
		for _, key := range []string{"timeout", "outbound", "socks5"} {
			if value := update.Value(key); value != nil && value != "" {
				args.Set(key, item(update, key))
			}
		}
		// The downloader remains callable when cron is off, so its live options
		// must be valid independently of the inactive scheduling fields.
		loadSchemas()
		fields := asObject(asObject(schemaPlugins.Value("download")).Value("properties"))
		issues := []any{}
		for key, value := range args.All() {
			field, ok := fields.Get(key)
			if !ok {
				raise(pyErr("KeyError", jsonx.Repr(key)))
			}
			issues = append(issues, validateSchema(value, asObject(field), []any{"plugins", jsonx.Int(len(plugins)), "args", key})...)
		}
		if errorCount(issues) > 0 {
			raise(validationError(issues))
		}
		list := []any{}
		for _, download := range downloads {
			d := download.(*jsonx.Object)
			parent, name := purePosixParentName(d.GetString("local_path"))
			list = append(list, jsonx.Obj("url", d.Value("url"), "dir", parent, "filename", name))
		}
		args.Set("downloads", list)
		add("rules_download", "download", args)
		providers := []any{}
		for _, part := range []string{"domain", "ip"} {
			for _, group := range groups.GetList(part) {
				providers = append(providers, "$"+group.(*jsonx.Object).GetString("tag"))
			}
		}
		if adguardOn {
			providers = append(providers, "$adguard_rules")
		}
		add("rules_reload", "reload_provider", providers)
		// Native downloads are atomic per file, not across the whole ruleset.
		// A failed item retains its old file; provider reload then reads the
		// successful updates plus retained files. Only scheduling is opt-in;
		// the maintenance chain is also used by explicit manual updates.
		add("rules_refresh", "sequence", []any{jsonx.Obj("exec", "$rules_download"), jsonx.Obj("exec", "$rules_reload")})
		if ruleUpdate.Value("enabled") == true {
			add("rules_cron", "cron", jsonx.Obj("timezone", item(update, "timezone"), "jobs", []any{jsonx.Obj(
				"name", "refresh_rules", "schedule", item(update, "schedule"), "executors", []any{"$rules_refresh"})}))
		}
	}
	plugins = append(plugins, copyValue(extraPlugins).([]any)...)
	tags := map[string]bool{}
	for _, plugin := range plugins {
		if !isDict(plugin) {
			fail("插件必须为对象")
		}
		tag, ok := get(plugin, "tag", "").(string)
		if !ok || !validPluginTag(tag) {
			fail("OxiDNS 插件 tag 必须为有效的 1–64 位 ASCII 标识")
		}
		if tags[tag] {
			fail("OxiDNS 插件 tag 重复：" + tag)
		}
		tags[tag] = true
	}
	result.Set("plugins", plugins)
	if policies := s.GetList("device_policies"); len(policies) > 0 {
		result = patchDocument(result, []any{}, policies)
	}
	return dump(result)
}
