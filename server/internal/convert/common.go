package convert

// Port of backend/converters/common.py: small input conversions shared by the
// client configuration generators.

import (
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/rules"
	"configflow/internal/weburl"
)

// UDPFallbackPolicies are the values udp_fallback accepts.
var UDPFallbackPolicies = []string{"DIRECT", "REJECT"}

// LogicTypes are the logic rule types.
var LogicTypes = []string{"AND", "OR", "NOT"}

// PortRuleTypes are the rule types that take a port.
var PortRuleTypes = []string{"DST-PORT", "SRC-PORT", "IN-PORT"}

// The patterns run on rules.CompilePyRegex: with re.IGNORECASE Python also
// matches "ı" for I and "ſ" for S (IN-PORT, DST-PORT), which RE2 does not.
const (
	networkConditionPattern = `\(\s*NETWORK\s*,\s*(tcp|udp)\s*\)`
	portConditionPattern    = `\(\s*(?:DST|DEST|SRC|IN)-PORT\s*,\s*([^()]*)\)`
)

func mustPyRegex(pattern string, ignoreCase bool) *rules.PyRegex {
	re, err := rules.CompilePyRegex(pattern, ignoreCase)
	if err != nil {
		panic(err)
	}
	return re
}

// AfterMatchNote is AFTER_MATCH_NOTE.
const AfterMatchNote = "仅 Mihomo 生效（位于 MATCH 之后，只匹配节点不支持 UDP 而跳过的连接），已忽略"

// NTPDirectRule returns a fresh copy of NTP_DIRECT_RULE: NTP only needs the
// real time server address, so it goes first, outside proxy rules.
func NTPDirectRule() *jsonx.Object {
	return jsonx.Obj("id", "builtin-ntp-direct", "itemType", "rule", "rule_type", "AND",
		"value", "((NETWORK,udp),(DST-PORT,123))", "policy", "DIRECT", "enabled", true)
}

func contains(list []string, v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// iterList is `for x in value` over a list field; nil when the value is
// missing. Non-lists give the TypeError/AttributeError Python would raise.
func iterList(v any) ([]any, error) {
	switch value := v.(type) {
	case []any:
		return value, nil
	case string:
		out := make([]any, 0, len(value))
		for _, r := range value {
			out = append(out, string(r))
		}
		return out, nil
	case *jsonx.Object:
		return jsonx.ListOf(value.Keys()), nil
	}
	return nil, pyError("TypeError", "'"+pyTypeName(v)+"' object is not iterable")
}

// asObject is the receiver of a `.get`: anything but a dict raises
// AttributeError in Python.
func asObject(v any) (*jsonx.Object, error) {
	if o, ok := v.(*jsonx.Object); ok && o != nil {
		return o, nil
	}
	return nil, attributeError(v, "get")
}

// effectiveMatch is _effective_match: the first enabled MATCH rule.
func effectiveMatch(config *jsonx.Object) (*jsonx.Object, error) {
	items, err := iterList(config.GetOr("rule_configs", []any{}))
	if err != nil {
		return nil, err
	}
	for _, v := range items {
		item, err := asObject(v)
		if err != nil {
			return nil, err
		}
		if jsonx.Equal(item.Value("itemType"), "rule") && jsonx.Equal(item.Value("rule_type"), "MATCH") &&
			jsonx.Truthy(item.GetOr("enabled", true)) {
			return item, nil
		}
	}
	return jsonx.NewObject(), nil
}

// UDPFallback is udp_fallback: DIRECT or REJECT chosen on the effective MATCH
// rule; "" (None) follows the client default.
func UDPFallback(config *jsonx.Object) (string, error) {
	match, err := effectiveMatch(config)
	if err != nil {
		return "", err
	}
	if choice := match.Value("udp_fallback"); contains(UDPFallbackPolicies, choice) {
		return choice.(string), nil
	}
	return "", nil
}

// NTPDirect is ntp_direct.
func NTPDirect(config *jsonx.Object) (bool, error) {
	match, err := effectiveMatch(config)
	if err != nil {
		return false, err
	}
	return match.Value("ntp_direct") == true, nil
}

// EmittedRules is emitted_rules: rule items in output order, with the
// built-in NTP rule first when enabled. The items are the stored objects
// (list(...) is a shallow copy).
func EmittedRules(config *jsonx.Object) ([]any, error) {
	ntp, err := NTPDirect(config)
	if err != nil {
		return nil, err
	}
	var out []any
	if ntp {
		out = append(out, NTPDirectRule())
	}
	items, err := iterList(config.GetOr("rule_configs", []any{}))
	if err != nil {
		return nil, err
	}
	return append(out, items...), nil
}

// AfterMatchComment is after_match_comment: Surge and Loon have no rules past
// FINAL; keep the Mihomo-only line visible.
func AfterMatchComment(item *jsonx.Object) string {
	var line string
	if jsonx.Equal(item.Value("itemType"), "ruleset") {
		line = "RULE-SET," + jsonx.Str(item.GetOr("name", "")) + "," + jsonx.Str(item.GetOr("policy", ""))
	} else {
		line = jsonx.Str(item.GetOr("rule_type", "")) + "," + jsonx.Str(item.GetOr("value", "")) + "," +
			jsonx.Str(item.GetOr("policy", ""))
	}
	return "# " + AfterMatchNote + ": " + line
}

// NetworkAsProtocol is network_as_protocol: Surge and Loon match the
// transport with PROTOCOL,TCP/UDP instead of NETWORK.
func NetworkAsProtocol(ruleType, value string) (string, string) {
	if ruleType == "NETWORK" {
		return "PROTOCOL", pystr.Upper(pystr.Strip(value))
	}
	if contains(LogicTypes, ruleType) {
		out, err := mustPyRegex(networkConditionPattern, true).Sub(value, func(m *rules.Match) (string, error) {
			group, _ := m.Group(1)
			return "(PROTOCOL," + pystr.Upper(group) + ")", nil
		}, 0)
		if err != nil { // only a match timeout, which these patterns cannot reach
			return ruleType, value
		}
		return ruleType, out
	}
	return ruleType, value
}

// findAllGroup1 is pattern.findall(value) for a pattern with one group.
func findAllGroup1(re *rules.PyRegex, value string) []string {
	var found []string
	_, _ = re.Sub(value, func(m *rules.Match) (string, error) {
		group, _ := m.Group(1)
		found = append(found, group)
		return "", nil
	}, 0)
	return found
}

// HasPortList is has_port_list: Surge and Loon take one port, range or
// comparison per rule, not a / or , list.
func HasPortList(ruleType, value string) bool {
	var values []string
	switch {
	case contains(PortRuleTypes, ruleType):
		values = []string{value}
	case contains(LogicTypes, ruleType):
		values = findAllGroup1(mustPyRegex(portConditionPattern, true), value)
	default:
		return false
	}
	for _, item := range values {
		if strings.Contains(item, "/") || strings.Contains(item, ",") {
			return true
		}
	}
	return false
}

// SetGeneralOption is set_general_option: add "key = value" to a [General]
// section unless it already sets the key.
func SetGeneralOption(section, key, value string) string {
	if mustPyRegex(`(?m)^\s*`+pyReEscape(key)+`\s*=`, false).Search(section) {
		return section
	}
	return strings.TrimRightFunc(section, pystr.IsSpace) + "\n" + key + " = " + value
}

// pyReEscape is re.escape (Python 3.7+).
func pyReEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 && strings.ContainsRune("()[]{}?*+-|^$\\.&~# \t\n\r\v\f", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// RulesetFormat is ruleset_format: prefer the declared format; infer
// handwritten payloads or legacy URL paths. sourceURL "" is None.
func RulesetFormat(rule, config *jsonx.Object, sourceURL string) (string, error) {
	items, err := iterList(config.GetOr("rule_library", []any{}))
	if err != nil {
		return "", err
	}
	library := jsonx.NewObject()
	libraryID := rule.Value("library_rule_id")
	for _, v := range items {
		item, err := asObject(v)
		if err != nil {
			return "", err
		}
		var match bool
		if jsonx.Truthy(libraryID) {
			match = jsonx.Equal(item.Value("id"), libraryID)
		} else {
			match = jsonx.Equal(item.Value("name"), rule.Value("name"))
		}
		if match {
			library = item
			break
		}
	}
	for _, source := range []*jsonx.Object{library, rule} {
		if declared := source.Value("format"); contains([]string{"yaml", "text", "mrs"}, declared) {
			return declared.(string), nil
		}
	}
	source := library
	if library.Len() == 0 {
		source = rule
	}
	if jsonx.Equal(source.Value("source_type"), "content") {
		if content, ok := source.GetOr("content", "").(string); ok && hasYAMLPayload(content) {
			return "yaml", nil
		}
		return "text", nil
	}
	var url any = ""
	switch {
	case jsonx.Truthy(library.Value("url")):
		url = library.Value("url")
	case sourceURL != "":
		url = sourceURL
	case jsonx.Truthy(rule.Value("url")):
		url = rule.Value("url")
	}
	text, ok := url.(string)
	if !ok {
		// urlsplit is lru_cache'd: an unhashable list/dict fails hashing
		// (TypeError, which callers such as rule_ip_presence catch) before
		// the str coercion (AttributeError) does.
		switch url.(type) {
		case []any, *jsonx.Object:
			return "", pyError("TypeError", "unhashable type: '"+pyTypeName(url)+"'")
		}
		return "", attributeError(url, "decode")
	}
	parts, err := weburl.Parse(text)
	if err != nil {
		return "", pyError("ValueError", err.Error())
	}
	path := pystr.Lower(weburl.RawPath(parts))
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		return "yaml", nil
	}
	if strings.HasSuffix(path, ".mrs") {
		return "mrs", nil
	}
	return "text", nil
}

// HostsPair is one (domain, address) row of ParseHostsPairs.
type HostsPair struct{ Domain, Address string }

// ParseHostsPairs is parse_hosts_pairs: domain/address and legacy
// address/domain rows, for IPv4 and IPv6.
func ParseHostsPairs(text string) []HostsPair {
	isIP := func(value string) bool {
		if _, ok := parseIPv4(value); ok {
			return true
		}
		return validIPv6(value)
	}
	var pairs []HostsPair
	for _, line := range pySplitlines(text) {
		before, _, _ := strings.Cut(line, "#")
		fields := strings.FieldsFunc(before, pystr.IsSpace)
		if len(fields) < 2 {
			continue
		}
		first, second := fields[0], fields[1]
		if isIP(second) {
			pairs = append(pairs, HostsPair{first, second})
		} else if isIP(first) {
			pairs = append(pairs, HostsPair{second, first})
		}
	}
	return pairs
}

// pySplitlines is str.splitlines().
func pySplitlines(s string) []string {
	var lines []string
	runes := []rune(s)
	start := 0
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '\n', '\x0b', '\x0c', '\x1c', '\x1d', '\x1e', '\x85', ' ', ' ':
			lines = append(lines, string(runes[start:i]))
			start = i + 1
		case '\r':
			lines = append(lines, string(runes[start:i]))
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}
