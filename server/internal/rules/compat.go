package rules

// Helpers owned by modules that import this package (rulefetch for rule-set
// content, convert for ruleset_format, oxidns for client ranges). The
// matcher and the connection rules need them too, and an import the other
// way round would be a cycle, so the implementation lives here: rulefetch
// and mosdns call these; convert.RulesetFormat and
// oxidns.NormalizeClientRange are still their own copies (to be switched to
// these in their S passes).

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/jsonx"
)

// ProfileValidationError is a request the user must fix; Error() is the
// Chinese message. (repo has its own; rules cannot import repo.)
type ProfileValidationError struct{ Message string }

func (e *ProfileValidationError) Error() string { return e.Message }

// ErrClassicalLine: a line of a classical rule set whose meaning is unclear.
var ErrClassicalLine = errors.New("无法确定此行的 Classical 语义，已保留原文；请改为明确的规则类型和值。")

// ValidPattern reports a domain pattern of a domain rule set: word
// characters, dots and the wildcards "*" (a whole label) and "+." (a leading
// any-depth suffix); IPs and networks are not patterns.
func ValidPattern(value string) bool {
	if value == "" || value == "." {
		return false
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !strings.ContainsRune("_.*+-", r) {
			return false
		}
	}
	hostname := strings.TrimLeft(value, ".")
	if strings.HasPrefix(value, "+.") || strings.HasPrefix(value, "*.") {
		hostname = value[2:]
	}
	if LooksLikeIP(hostname) || IsValidIP(value) {
		return false
	}
	if value == "*" {
		return true
	}
	parts := strings.Split(value, ".")
	for index, part := range parts {
		if index > 0 && part == "" {
			return false
		}
		if strings.Contains(part, "+") && !(index == 0 && part == "+" && len(parts) > 1) {
			return false
		}
		if strings.Contains(part, "*") && part != "*" {
			return false
		}
	}
	return true
}

func validClassicalDomain(value string) bool {
	if len(value) > 253 || !ValidPattern(value) || asciiDigits(value[strings.LastIndex(value, ".")+1:]) {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !validDNSLabel(label) {
			return false
		}
	}
	return true
}

// ClassicalLine gives a bare entry of a classical rule set its explicit form
// (+.suffix → DOMAIN-SUFFIX, an IP or CIDR → IP-CIDR/IP-CIDR6 with
// no-resolve, a host → DOMAIN); explicit, blank and comment lines are kept.
func ClassicalLine(value string) (string, error) {
	stripped := strings.TrimSpace(value)
	if stripped == "" || strings.HasPrefix(stripped, "#") || strings.HasPrefix(stripped, "//") || strings.Contains(stripped, ",") {
		return value, nil
	}
	if suffix, ok := strings.CutPrefix(stripped, "+."); ok && validClassicalDomain(suffix) {
		return "DOMAIN-SUFFIX," + suffix, nil
	}
	if !strings.Contains(stripped, "%") && !(strings.Contains(stripped, "/") && CIDRError(stripped) != "") {
		if network, ok := parseNetwork(stripped); ok {
			kind := "IP-CIDR"
			if network.Addr().Is6() {
				kind = "IP-CIDR6"
			}
			address := stripped
			if !strings.Contains(stripped, "/") {
				address += "/" + strconv.Itoa(network.Addr().BitLen())
			}
			return kind + "," + address + ",no-resolve", nil
		}
	}
	if validClassicalDomain(stripped) {
		return "DOMAIN," + stripped, nil
	}
	return "", ErrClassicalLine
}

// ClassicalLineError is the message for an explicit classical rule line
// that a rule set cannot hold; "" when valid.
func ClassicalLineError(value string) string {
	kind, payload, params := SplitRulePayload(value)
	if kind == "MATCH" || kind == "RULE-SET" || kind == "SUB-RULE" {
		return "Classical 规则集不支持 " + kind + "；请放在策略规则中。"
	}
	if err := ruleValueError(kind, payload, 0); err != "" {
		return err
	}
	for _, param := range params {
		if param == "" {
			return "规则包含空参数，请检查多余的逗号。"
		}
	}
	return ""
}

// HasYAMLPayload reports content that looks like a YAML rule set: a flow
// mapping, or a line starting with a (quoted) payload key.
func HasYAMLPayload(content string) bool {
	probe := strings.TrimPrefix(content, "\ufeff")
	if strings.HasPrefix(strings.TrimLeftFunc(probe, unicode.IsSpace), "{") {
		return true
	}
	for _, line := range strings.Split(probe, "\n") {
		rest := strings.TrimLeftFunc(line, unicode.IsSpace)
		for _, key := range []string{`payload`, `"payload"`, `'payload'`} {
			if after, ok := strings.CutPrefix(rest, key); ok && strings.HasPrefix(strings.TrimLeftFunc(after, unicode.IsSpace), ":") {
				return true
			}
		}
	}
	return false
}

// RulesetFormat is the format of a rule set reference ("yaml", "text" or
// "mrs"): the declared one, else inferred from inline content or the URL
// path. library is the rule_library list; sourceURL overrides the rule's
// URL when the library entry has none.
func RulesetFormat(rule *jsonx.Object, library []any, sourceURL string) string {
	found := jsonx.NewObject()
	libraryID := rule.GetString("library_rule_id")
	for _, entry := range library {
		item, ok := entry.(*jsonx.Object)
		if ok && (libraryID != "" && item.GetString("id") == libraryID || libraryID == "" && jsonx.Equal(item.Value("name"), rule.Value("name"))) {
			found = item
			break
		}
	}
	for _, source := range []*jsonx.Object{found, rule} {
		switch declared := source.GetString("format"); declared {
		case "yaml", "text", "mrs":
			return declared
		}
	}
	source := found
	if found.Len() == 0 {
		source = rule
	}
	if source.GetString("source_type") == "content" {
		if HasYAMLPayload(source.GetString("content")) {
			return "yaml"
		}
		return "text"
	}
	address := found.GetString("url")
	if address == "" {
		address = sourceURL
	}
	if address == "" {
		address = rule.GetString("url")
	}
	path := address
	if parsed, err := url.Parse(address); err == nil {
		path = parsed.Path + parsed.Opaque
	}
	switch path = strings.ToLower(path); {
	case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"):
		return "yaml"
	case strings.HasSuffix(path, ".mrs"):
		return "mrs"
	}
	return "text"
}

// ErrClientRange: not a usable client address or range.
var ErrClientRange = errors.New("请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。")

// NormalizeClientRange is a client host or CIDR in canonical text
// (IPv4-mapped peers as IPv4; host bits cleared; a full-length prefix as the
// bare address). Multicast networks and the unspecified address are refused.
func NormalizeClientRange(raw string) (string, error) {
	addressText, prefixText, hasPrefix := strings.Cut(strings.TrimSpace(raw), "/")
	if addressText == "" || strings.Trim(addressText, "0123456789abcdefABCDEF:.") != "" || strings.Contains(prefixText, "/") {
		return "", ErrClientRange
	}
	address, err := netip.ParseAddr(addressText)
	if err != nil {
		return "", ErrClientRange
	}
	prefix := address.BitLen()
	if hasPrefix {
		if len(prefixText) > 3 || !asciiDigits(prefixText) {
			return "", ErrClientRange
		}
		prefix, _ = strconv.Atoi(prefixText)
	}
	if prefix > address.BitLen() {
		return "", ErrClientRange
	}
	if address.Is4In6() { // OxiDNS sees IPv4-mapped peers as the IPv4 client
		if prefix < 96 {
			return "", ErrClientRange
		}
		address, prefix = address.Unmap(), prefix-96
	}
	network := netip.PrefixFrom(address, prefix).Masked()
	if network.Addr().IsMulticast() && lastAddress(network).IsMulticast() {
		return "", ErrClientRange
	}
	if prefix == address.BitLen() {
		if address.IsUnspecified() {
			return "", ErrClientRange
		}
		return address.String(), nil
	}
	return network.String(), nil
}

func lastAddress(network netip.Prefix) netip.Addr {
	bytes := network.Addr().AsSlice()
	for bit := network.Bits(); bit < len(bytes)*8; bit++ {
		bytes[bit/8] |= 0x80 >> (bit % 8)
	}
	last, _ := netip.AddrFromSlice(bytes)
	return last
}
