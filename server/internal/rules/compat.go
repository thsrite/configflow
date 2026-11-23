package rules

// Helpers shared with rulefetch and mosdns. Rule-set format inference and
// client-range normalization live in cycle-free leaf packages so converters,
// OxiDNS and this package all use one implementation.

import (
	"configflow/internal/textx"
	"errors"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/clientrange"
	"configflow/internal/rulesetfmt"
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
	if len(value) > 253 || !ValidPattern(value) || textx.ASCIIDigits(value[strings.LastIndex(value, ".")+1:]) {
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
	return rulesetfmt.HasYAMLPayload(content)
}

// ErrClientRange: not a usable client address or range.
var ErrClientRange = errors.New("请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。")

// NormalizeClientRange is a client host or CIDR in canonical text
// (IPv4-mapped peers as IPv4; host bits cleared; a full-length prefix as the
// bare address). Multicast networks and the unspecified address are refused.
func NormalizeClientRange(raw string) (string, error) {
	value, ok := clientrange.Normalize(raw)
	if !ok {
		return "", ErrClientRange
	}
	return value, nil
}
