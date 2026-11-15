package convert

// Port of backend/converters/common.py: small input conversions shared by the
// client configuration generators.

import (
	"regexp"
	"strings"

	"configflow/internal/jsonx"
)

// UDPFallbackPolicies are the values udp_fallback accepts.
var UDPFallbackPolicies = []string{"DIRECT", "REJECT"}

// LogicTypes are the logic rule types.
var LogicTypes = []string{"AND", "OR", "NOT"}

// PortRuleTypes are the rule types that take a port.
var PortRuleTypes = []string{"DST-PORT", "SRC-PORT", "IN-PORT"}

// pySpaceRE is Python's \s for str patterns (str.isspace characters).
const pySpaceRE = `[\t\n\x0b\f\r\x1c-\x1f \x85\p{Z}]`

// Both patterns only use RE2-compatible syntax; (?i) matches Python's
// re.IGNORECASE for these ASCII words (the [^()] class has no letters).
var (
	networkCondition = regexp.MustCompile(`(?i)\(` + pySpaceRE + `*NETWORK` + pySpaceRE + `*,` + pySpaceRE + `*(tcp|udp)` + pySpaceRE + `*\)`)
	portCondition    = regexp.MustCompile(`(?i)\(` + pySpaceRE + `*(?:DST|DEST|SRC|IN)-PORT` + pySpaceRE + `*,` + pySpaceRE + `*([^()]*)\)`)
)

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
		return "PROTOCOL", pyUpper(pyStrip(value))
	}
	if contains(LogicTypes, ruleType) {
		return ruleType, networkCondition.ReplaceAllStringFunc(value, func(m string) string {
			sub := networkCondition.FindStringSubmatch(m)
			return "(PROTOCOL," + strings.ToUpper(sub[1]) + ")"
		})
	}
	return ruleType, value
}

// pyUpper is str.upper() (Go's ToUpper; the full-case expansions Python
// applies, such as ß -> SS, are reproduced for the common ones).
func pyUpper(s string) string {
	if isASCII(s) {
		return strings.ToUpper(s)
	}
	var b strings.Builder
	for _, r := range s {
		if exp, ok := upperSpecial[r]; ok {
			b.WriteString(exp)
			continue
		}
		b.WriteString(strings.ToUpper(string(r)))
	}
	return b.String()
}

// upperSpecial holds Unicode SpecialCasing.txt unconditional upper mappings
// that expand to several characters.
var upperSpecial = map[rune]string{
	0x00DF: "SS", 0x0149: "ʼN", 0x01F0: "J̌", 0x0390: "Ϊ́", 0x03B0: "Ϋ́",
	0x0587: "ԵՒ", 0x1E96: "H̱", 0x1E97: "T̈", 0x1E98: "W̊", 0x1E99: "Y̊",
	0x1E9A: "Aʾ", 0xFB00: "FF", 0xFB01: "FI", 0xFB02: "FL", 0xFB03: "FFI", 0xFB04: "FFL", 0xFB05: "ST",
	0xFB06: "ST",
}

// HasPortList is has_port_list: Surge and Loon take one port, range or
// comparison per rule, not a / or , list.
func HasPortList(ruleType, value string) bool {
	var values []string
	switch {
	case contains(PortRuleTypes, ruleType):
		values = []string{value}
	case contains(LogicTypes, ruleType):
		for _, m := range portCondition.FindAllStringSubmatch(value, -1) {
			values = append(values, m[1])
		}
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
	pattern := regexp.MustCompile(`(?m)^` + pySpaceRE + `*` + regexp.QuoteMeta(key) + pySpaceRE + `*=`)
	if pattern.MatchString(section) {
		return section
	}
	return strings.TrimRightFunc(section, pyIsSpace) + "\n" + key + " = " + value
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
		return "", attributeError(url, "decode")
	}
	parts, err := urlsplit(text)
	if err != nil {
		return "", err
	}
	path := pyLower(parts.path)
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
		fields := strings.FieldsFunc(before, pyIsSpace)
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
