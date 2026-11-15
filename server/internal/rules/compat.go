package rules

// Helpers that belong to packages not ported yet (see server/PORTING.md,
// "Helpers from a package that is not ported yet"). Each is a minimal private
// copy; the owner replaces it when its package lands.

import (
	"configflow/internal/pystr"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/jsonx"
)

// ErrClassicalLine is the ValueError of ruleset_content._classical_line.
var ErrClassicalLine = errors.New("无法确定此行的 Classical 语义，已保留原文；请改为明确的规则类型和值。")

// TODO(port): replace with package #12 (backend/utils/ruleset_content.py).
func validPattern(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !pyWordRune(r) && r != '.' && r != '*' && r != '+' && r != '-' {
			return false
		}
	}
	var hostname string
	if strings.HasPrefix(value, "+.") || strings.HasPrefix(value, "*.") {
		hostname = value[2:]
	} else {
		hostname = strings.TrimLeft(value, ".")
	}
	if LooksLikeIP(hostname) {
		return false
	}
	if _, ok := parseIPAddress(value); ok {
		return false
	}
	if value == "*" {
		return true
	}
	parts := strings.Split(value, ".")
	for _, part := range parts[1:] {
		if part == "" {
			return false
		}
	}
	if value == "." {
		return false
	}
	for index, part := range parts {
		if strings.Contains(part, "+") && !(index == 0 && part == "+" && len(parts) > 1) {
			return false
		}
		if strings.Contains(part, "*") && part != "*" {
			return false
		}
	}
	return true
}

// TODO(port): replace with package #12.
func validClassicalDomain(value string) bool {
	if len(value) > 253 || !validPattern(value) {
		return false
	}
	if i := strings.LastIndex(value, "."); pyIsASCIIDigits(value[i+1:]) {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if !validDNSLabel(part) {
			return false
		}
	}
	return true
}

// classicalLine is ruleset_content._classical_line.
// TODO(port): replace with package #12.
func classicalLine(value string) (string, error) {
	stripped := pystr.Strip(value)
	if stripped == "" || strings.HasPrefix(stripped, "#") || strings.HasPrefix(stripped, "//") || strings.Contains(stripped, ",") {
		return value, nil
	}
	if strings.HasPrefix(stripped, "+.") && validClassicalDomain(stripped[2:]) {
		return "DOMAIN-SUFFIX," + stripped[2:], nil
	}
	if !strings.Contains(stripped, "%") && !(strings.Contains(stripped, "/") && CIDRError(stripped) != "") {
		if network, ok := parseIPNetwork(stripped); ok {
			prefix := "IP-CIDR"
			if network.ver == 6 {
				prefix = "IP-CIDR6"
			}
			address := stripped
			if !strings.Contains(stripped, "/") {
				address = fmt.Sprintf("%s/%d", stripped, ipAddr{ver: network.ver}.maxPrefix())
			}
			return prefix + "," + address + ",no-resolve", nil
		}
	}
	if validClassicalDomain(stripped) {
		return "DOMAIN," + stripped, nil
	}
	return "", ErrClassicalLine
}

// classicalLineError is ruleset_content.classical_line_error ("" for None).
// TODO(port): replace with package #12.
func classicalLineError(value string) string {
	kind, payload, params := SplitRulePayload(value)
	if kind == "MATCH" || kind == "RULE-SET" || kind == "SUB-RULE" {
		return fmt.Sprintf("Classical 规则集不支持 %s；请放在策略规则中。", kind)
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

// hasYAMLPayload is ruleset_content._has_yaml_payload.
// TODO(port): replace with package #12.
func hasYAMLPayload(content string) bool {
	probe := strings.TrimPrefix(content, "\ufeff")
	if strings.HasPrefix(strings.TrimLeftFunc(probe, pystr.IsSpace), "{") {
		return true
	}
	skipSpace := func(s string) string { return strings.TrimLeftFunc(s, pystr.IsSpace) }
	for start := 0; start <= len(probe); {
		rest := skipSpace(probe[start:])
		for _, key := range []string{`payload`, `"payload"`, `'payload'`} {
			if strings.HasPrefix(rest, key) && strings.HasPrefix(skipSpace(rest[len(key):]), ":") {
				return true
			}
		}
		next := strings.IndexByte(probe[start:], '\n')
		if next < 0 {
			break
		}
		start += next + 1
	}
	return false
}

// urlPath is urlsplit(url).path.
func urlPath(url string) string {
	if i := strings.IndexByte(url, '#'); i >= 0 {
		url = url[:i]
	}
	if i := strings.IndexByte(url, '?'); i >= 0 {
		url = url[:i]
	}
	if i := strings.IndexByte(url, ':'); i > 0 {
		scheme := url[:i]
		valid := scheme[0] >= 'A' && (scheme[0] <= 'Z' || scheme[0] >= 'a' && scheme[0] <= 'z')
		for j := 1; valid && j < len(scheme); j++ {
			c := scheme[j]
			valid = c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'
		}
		if valid {
			url = url[i+1:]
		}
	}
	if strings.HasPrefix(url, "//") {
		rest := url[2:]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			return rest[i:]
		}
		return ""
	}
	return url
}

// RulesetFormat is converters.common.ruleset_format(rule, {'rule_library':
// library}): the declared format, or the one inferred from the payload or URL
// ("yaml", "text" or "mrs"). The port is convert.RulesetFormat (C12), which
// cannot be used here (convert imports rules); compat_drift_test.go checks
// this copy against convert's Python corpus.
func RulesetFormat(rule *jsonx.Object, library []any, sourceURL string) string {
	var found *jsonx.Object
	libraryID := rule.Value("library_rule_id")
	for _, entry := range library {
		item, ok := entry.(*jsonx.Object)
		if !ok {
			continue
		}
		var match bool
		if jsonx.Truthy(libraryID) {
			match = jsonx.Equal(item.Value("id"), libraryID)
		} else {
			match = jsonx.Equal(item.Value("name"), rule.Value("name"))
		}
		if match {
			found = item
			break
		}
	}
	if found == nil {
		found = jsonx.NewObject()
	}
	for _, source := range []*jsonx.Object{found, rule} {
		switch declared, _ := source.Value("format").(string); declared {
		case "yaml", "text", "mrs":
			return declared
		}
	}
	source := found
	if found.Len() == 0 {
		source = rule
	}
	if kind, _ := source.Value("source_type").(string); kind == "content" {
		if content, ok := source.Value("content").(string); ok && hasYAMLPayload(content) {
			return "yaml"
		}
		return "text"
	}
	url := ""
	if text, ok := found.Value("url").(string); ok && text != "" {
		url = text
	} else if sourceURL != "" {
		url = sourceURL
	} else if text, ok := rule.Value("url").(string); ok {
		url = text
	}
	path := strings.ToLower(urlPath(url))
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		return "yaml"
	}
	if strings.HasSuffix(path, ".mrs") {
		return "mrs"
	}
	return "text"
}

// ErrClientRange is OxidnsSettingsError for a bad client address or range.
// TODO(port): replace with package #11 / #2 (common/oxidns_device_policy).
var ErrClientRange = errors.New("请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。")

// NormalizeClientRange is oxidns_device_policy.normalize_client_range: it
// canonicalises hosts and CIDRs, including IPv4-mapped peers and host prefixes.
// TODO(port): replace with package #11.
func NormalizeClientRange(raw any) (string, error) {
	text, ok := raw.(string)
	if !ok {
		return "", ErrClientRange
	}
	parts := strings.Split(pystr.Strip(text), "/")
	if len(parts) > 2 || parts[0] == "" {
		return "", ErrClientRange
	}
	for _, c := range []byte(parts[0]) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' || c == ':' || c == '.') {
			return "", ErrClientRange
		}
	}
	address, ok := parseIPAddress(parts[0])
	if !ok {
		return "", ErrClientRange
	}
	prefix := address.maxPrefix()
	if len(parts) == 2 {
		if l := len(parts[1]); l < 1 || l > 3 || !pyIsASCIIDigits(parts[1]) {
			return "", ErrClientRange
		}
		prefix = 0
		for _, c := range parts[1] {
			prefix = prefix*10 + int(c-'0')
		}
	}
	if prefix > address.maxPrefix() {
		return "", ErrClientRange
	}
	if address.ver == 6 && address.v.hi == 0 && address.v.lo>>32 == 0xffff {
		if prefix < 96 {
			return "", ErrClientRange
		}
		address = ipAddr{ver: 4, v: u128{lo: address.v.lo & 0xffffffff}}
		prefix -= 96
	}
	network, _ := parseIPNetwork(fmt.Sprintf("%s/%d", formatIP(address.ver, address.v), prefix))
	mask := maskFor(network.ver, network.prefix)
	broadcast := u128{network.base.hi | ^mask.hi, network.base.lo | ^mask.lo}
	if network.ver == 4 {
		broadcast.lo &= 0xffffffff
	}
	isMulticast := func(v u128) bool {
		if network.ver == 4 {
			return v.lo>>28 == 0xe
		}
		return v.hi>>56 == 0xff
	}
	if isMulticast(network.base) && isMulticast(broadcast) {
		return "", ErrClientRange
	}
	if prefix == address.maxPrefix() {
		if address.v == (u128{}) {
			return "", ErrClientRange
		}
		return formatIP(address.ver, address.v), nil
	}
	return network.String(), nil
}

// safeExceptionDetails is url_utils.safe_exception_details for a Go error:
// bounded metadata without the message.
// TODO(port): replace with package #1 (utils/url_utils).
func safeExceptionDetails(err error) string {
	return fmt.Sprintf("exception_type=%T", err)
}

// Logger is the part of the application logger the rule engine writes to.
// TODO(port): replace with package #1 (utils/logger) once it exists.
type Logger interface {
	Debugf(format string, args ...any)
	Warningf(format string, args ...any)
	Errorf(format string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Debugf(string, ...any)   {}
func (nopLogger) Warningf(string, ...any) {}
func (nopLogger) Errorf(string, ...any)   {}

// ProfileValidationError is common.config_repository.ProfileValidationError:
// a request the user must fix. Error() is the Chinese message.
// TODO(port): replace with package #2 (repo).
type ProfileValidationError struct{ Message string }

func (e *ProfileValidationError) Error() string { return e.Message }
