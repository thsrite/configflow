package rules

import (
	"configflow/internal/pystr"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/jsonx"
)

// Port of backend/utils/rule_syntax.py: shared checks for explicit Mihomo rule
// types and clearly invalid values. Messages are byte-identical to Python.

// RuleTypes is RULE_TYPES: the rule types Mihomo's parser knows.
var RuleTypes = map[string]struct{}{}

var sortedRuleTypes []string

func init() {
	for _, kind := range []string{
		"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-REGEX", "DOMAIN-WILDCARD",
		"GEOSITE", "GEOIP", "SRC-GEOIP", "IP-ASN", "SRC-IP-ASN",
		"IP-CIDR", "IP-CIDR6", "SRC-IP-CIDR", "IP-SUFFIX", "SRC-IP-SUFFIX",
		"SRC-PORT", "DST-PORT", "IN-PORT", "DSCP",
		"PROCESS-NAME", "PROCESS-PATH", "PROCESS-NAME-REGEX", "PROCESS-PATH-REGEX",
		"PROCESS-NAME-WILDCARD", "PROCESS-PATH-WILDCARD",
		"NETWORK", "UID", "IN-TYPE", "IN-USER", "IN-NAME", "REMATCH-NAME",
		"SUB-RULE", "AND", "OR", "NOT", "RULE-SET", "MATCH",
	} {
		RuleTypes[kind] = struct{}{}
		sortedRuleTypes = append(sortedRuleTypes, kind)
	}
	sort.Strings(sortedRuleTypes)
}

// IsRuleType reports whether kind is in RULE_TYPES.
func IsRuleType(kind string) bool {
	_, ok := RuleTypes[kind]
	return ok
}

var commaPayloadTypes = map[string]bool{
	"AND": true, "OR": true, "NOT": true, "SUB-RULE": true, "DOMAIN-REGEX": true,
	"PROCESS-NAME-REGEX": true, "PROCESS-PATH-REGEX": true,
}

func isDigitsAndDots(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// LooksLikeIP is looks_like_ip: do not reinterpret a malformed address or
// network as a DNS hostname.
func LooksLikeIP(value string) bool {
	return strings.Contains(value, ":") || strings.Contains(value, "/") || isDigitsAndDots(value)
}

func validDNSLabel(part string) bool {
	if len(part) == 0 || len(part) > 63 {
		return false
	}
	alnum := func(c byte) bool {
		return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	if !alnum(part[0]) || !alnum(part[len(part)-1]) {
		return false
	}
	for i := 1; i < len(part)-1; i++ {
		if !alnum(part[i]) && part[i] != '-' {
			return false
		}
	}
	return true
}

// DomainValueError is domain_value_error; "" means valid (Python's None).
func DomainValueError(value string) string {
	if value == "" || LooksLikeIP(value) {
		return "不是有效域名；IP 地址或网段请使用 IP-CIDR。"
	}
	ascii, ok := pyIDNAEncode(value)
	if !ok {
		return "不是有效域名。"
	}
	if len(ascii) > 253 {
		return "不是有效域名。"
	}
	if i := strings.LastIndex(ascii, "."); pyIsASCIIDigits(ascii[i+1:]) {
		return "不是有效域名。"
	}
	for _, part := range strings.Split(ascii, ".") {
		if !validDNSLabel(part) {
			return "不是有效域名。"
		}
	}
	return ""
}

func isCanonicalPrefix(prefix string) bool {
	if prefix == "" {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if prefix[i] < '0' || prefix[i] > '9' {
			return false
		}
	}
	return prefix == "0" || prefix[0] != '0'
}

// CIDRError is cidr_error; "" means valid.
func CIDRError(value string) string {
	if value == "" {
		return "请填写有效 CIDR，例如 192.0.2.0/24 或 2001:db8::/32。"
	}
	if !strings.Contains(value, "/") {
		address, ok := parseIPAddress(value)
		if !ok {
			return "不是有效 IP 网段；IPCIDR 只接受带前缀长度的 CIDR。"
		}
		return fmt.Sprintf("单个 IP 也需要前缀长度，请填写 %s/%d。", value, address.maxPrefix())
	}
	cut := strings.LastIndex(value, "/")
	address, prefix := value[:cut], value[cut+1:]
	if !isCanonicalPrefix(prefix) || strings.Contains(address, "%") {
		return "不是有效 IP 网段；请使用数字前缀长度。"
	}
	if _, ok := parseIPNetwork(value); !ok {
		return "不是有效 IP 网段；请检查地址与前缀长度。"
	}
	return ""
}

// SplitRulePayload is split_rule_payload: kind, payload and extra parameters
// of a rule line, matching Mihomo's ParseRulePayload(..., false).
func SplitRulePayload(line string) (kind, payload string, params []string) {
	fields := strings.Split(line, ",")
	for i := range fields {
		fields[i] = pystr.Strip(fields[i])
	}
	kind = pystr.Upper(fields[0])
	if commaPayloadTypes[kind] {
		return kind, strings.Join(fields[1:], ","), []string{}
	}
	if len(fields) > 1 {
		payload = fields[1]
	}
	if len(fields) > 2 {
		params = fields[2:]
	} else {
		params = []string{}
	}
	return kind, payload, params
}

func rangesError(value string, maximum uint64, label string, wildcard bool) string {
	if value == "*" && wildcard {
		return ""
	}
	segments := splitOn(value, "/,")
	if len(segments) > 28 {
		return label + "最多支持 28 组范围。"
	}
	maxDigits := len(strconv.FormatUint(maximum, 10))
	for _, segment := range segments {
		first, last, ok := parseRangeSegment(segment)
		if !ok {
			return label + "请填写数字或范围，以 / 分隔，例如 80/443/8000-9000。"
		}
		first, last = trimZeros(first), trimZeros(last)
		if len(first) > maxDigits || len(last) > maxDigits {
			return fmt.Sprintf("%s范围必须在 0–%d 内。", label, maximum)
		}
		f, _ := strconv.ParseUint(first, 10, 64)
		l, _ := strconv.ParseUint(last, 10, 64)
		if f > l || l > maximum {
			return fmt.Sprintf("%s范围必须在 0–%d 内，且起点不大于终点。", label, maximum)
		}
	}
	return ""
}

func trimZeros(text string) string {
	if t := strings.TrimLeft(text, "0"); t != "" {
		return t
	}
	return "0"
}

// splitOn is re.split on a character class.
func splitOn(value, separators string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(value); i++ {
		if strings.IndexByte(separators, value[i]) >= 0 {
			parts = append(parts, value[start:i])
			start = i + 1
		}
	}
	return append(parts, value[start:])
}

// parseRangeSegment is re.fullmatch(r'([0-9]+)(?:-([0-9]+))?').
func parseRangeSegment(segment string) (first, last string, ok bool) {
	digits := func(s string) bool {
		if s == "" {
			return false
		}
		for i := 0; i < len(s); i++ {
			if s[i] < '0' || s[i] > '9' {
				return false
			}
		}
		return true
	}
	if i := strings.IndexByte(segment, '-'); i >= 0 {
		first, last = segment[:i], segment[i+1:]
		return first, last, digits(first) && digits(last)
	}
	return segment, segment, digits(segment)
}

// replaceQuoted is re.sub(r'\\Q(.*?)(?:\\E|$)', re.escape(group), value, re.S).
func replaceQuoted(value string) string {
	var out strings.Builder
	for {
		i := strings.Index(value, `\Q`)
		if i < 0 {
			out.WriteString(value)
			return out.String()
		}
		out.WriteString(value[:i])
		rest := value[i+2:]
		end := strings.Index(rest, `\E`)
		if end < 0 {
			out.WriteString(pyReEscape(rest))
			return out.String()
		}
		out.WriteString(pyReEscape(rest[:end]))
		value = rest[end+2:]
	}
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

// replaceControlEscapes is re.sub(r'\\c.', 'a', value) ('.' excludes "\n").
func replaceControlEscapes(value string) string {
	runes := []rune(value)
	var out []rune
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+2 < len(runes) && runes[i+1] == 'c' && runes[i+2] != '\n' {
			out = append(out, 'a')
			i += 2
			continue
		}
		out = append(out, runes[i])
	}
	return string(out)
}

// replacePOSIXClasses is re.sub(r'\[:\^?[A-Za-z]+:\]', 'a', value).
func replacePOSIXClasses(value string) string {
	runes := []rune(value)
	var out []rune
	for i := 0; i < len(runes); i++ {
		if runes[i] == '[' && i+1 < len(runes) && runes[i+1] == ':' {
			j := i + 2
			if j < len(runes) && runes[j] == '^' {
				j++
			}
			start := j
			for j < len(runes) && isASCIILetter(runes[j]) {
				j++
			}
			if j > start && j+1 < len(runes) && runes[j] == ':' && runes[j+1] == ']' {
				out = append(out, 'a')
				i = j + 1
				continue
			}
		}
		out = append(out, runes[i])
	}
	return string(out)
}

// hasInlineExtendedFlag is re.search(r'\(\?[A-Za-z-]*x[A-Za-z-]*(?::|\))', value).
func hasInlineExtendedFlag(value string) bool {
	runes := []rune(value)
	for i := 0; i+2 < len(runes); i++ {
		if runes[i] != '(' || runes[i+1] != '?' {
			continue
		}
		j := i + 2
		for j < len(runes) && (isASCIILetter(runes[j]) || runes[j] == '-') {
			j++
		}
		// The class run may be followed by ':' or ')'; the run must contain an
		// x with the terminator right after the run (regex backtracking can
		// only shorten the run, but then the next char would be a class char,
		// not a terminator).
		if j < len(runes) && (runes[j] == ':' || runes[j] == ')') {
			for _, r := range runes[i+2 : j] {
				if r == 'x' {
					return true
				}
			}
		}
	}
	return false
}

var regexSyntaxMessages = []string{
	"unterminated character set", "unbalanced parenthesis",
	"missing ),", "bad character range", "min repeat greater than max repeat",
	"multiple repeat", "nothing to repeat", "bad escape (end of pattern)",
}

func regexError(value string) string {
	// Python and destination cores do not share a regex dialect; only clear
	// structural errors are reported (see rule_syntax._regex_error).
	if strings.Contains(value, "(?#") || hasInlineExtendedFlag(value) {
		return ""
	}
	probe := replaceQuoted(value)
	probe = replaceControlEscapes(probe)
	probe = replacePOSIXClasses(probe)
	depth, inClass, classItems, escaped := 0, false, 0, false
	for _, char := range probe {
		if escaped {
			escaped = false
			if inClass {
				classItems++
			}
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if inClass {
			if char == ']' && classItems > 0 {
				inClass = false
			} else if char != '^' || classItems > 0 {
				classItems++
			}
			continue
		}
		switch char {
		case '[':
			inClass, classItems = true, 0
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return "正则表达式的括号不匹配。"
			}
		}
	}
	if escaped || inClass || depth != 0 {
		return "正则表达式含未闭合括号、字符组或末尾转义。"
	}
	_, err := parsePyRegex(probe, 0)
	switch e := err.(type) {
	case nil:
	case errRecursion:
		return "正则表达式嵌套过深，请简化表达式。"
	case errOverflow:
		// Python lets OverflowError escape (an HTTP 500); report it instead.
		return "正则表达式格式无效：" + e.Error()
	case *reError:
		text := e.Error()
		for _, message := range regexSyntaxMessages {
			if strings.Contains(text, message) {
				return "正则表达式格式无效：" + text
			}
		}
	}
	return ""
}

func logicError(kind, value string, depth int) string {
	if depth > 32 {
		return "逻辑规则嵌套过深，请拆分规则。"
	}
	if !strings.HasPrefix(value, "(") || !strings.HasSuffix(value, ")") {
		return "逻辑规则应使用括号包裹条件，例如 ((DOMAIN,example.com),(NETWORK,tcp))。"
	}
	nesting, start := 0, 0
	var children []string
	for index, char := range value {
		switch char {
		case '(':
			nesting++
			if nesting == 2 {
				start = index + 1
			}
		case ')':
			if nesting == 2 {
				children = append(children, value[start:index])
			}
			nesting--
			if nesting < 0 {
				return "逻辑规则的括号不匹配。"
			}
		}
	}
	if nesting != 0 {
		return "逻辑规则的括号不匹配。"
	}
	if len(children) == 0 || (kind == "NOT" && len(children) != 1) {
		return "逻辑规则需要有效的子条件；NOT 必须恰好包含一个条件。"
	}
	for _, child := range children {
		childKind, payload, _ := SplitRulePayload(child)
		if childKind == "MATCH" || childKind == "SUB-RULE" {
			return fmt.Sprintf("逻辑规则不能包含 %s。", childKind)
		}
		if err := ruleValueError(childKind, payload, depth+1); err != "" {
			return err
		}
	}
	return ""
}

// ruleValueError is _rule_value_error. kind and value are dynamic because
// callers pass whatever a stored rule holds; Python checks their types.
func ruleValueError(kindAny, valueAny any, depth int) string {
	kind, isStr := kindAny.(string)
	if !isStr || !IsRuleType(kind) {
		message := "未知规则类型 " + jsonx.Str(kindAny) + "。"
		if isStr {
			if suggestion, ok := getCloseMatch(kind, sortedRuleTypes, 0.75); ok {
				message += "是否要填写 " + suggestion + "？"
			}
		}
		return message
	}
	value, isStr := valueAny.(string)
	if !isStr || strings.ContainsAny(value, "\n\r") {
		return "规则值必须是单行文本。"
	}
	if kind == "MATCH" {
		if pystr.Strip(value) == "" {
			return ""
		}
		return "MATCH 不接受规则值。"
	}
	if pystr.Strip(value) == "" {
		return "规则值为空。"
	}
	if strings.Contains(value, ",") && !commaPayloadTypes[kind] {
		return "此规则值不能包含逗号；端口列表请使用 / 分隔，策略与参数应单独填写。"
	}
	switch kind {
	case "DOMAIN", "DOMAIN-SUFFIX":
		return DomainValueError(value)
	case "IP-CIDR", "IP-CIDR6", "SRC-IP-CIDR", "IP-SUFFIX", "SRC-IP-SUFFIX":
		return CIDRError(value)
	}
	if strings.HasSuffix(kind, "-REGEX") {
		return regexError(value)
	}
	switch kind {
	case "SRC-PORT", "DST-PORT", "IN-PORT":
		return rangesError(value, 65535, "端口", false)
	case "DSCP":
		return rangesError(value, 63, "DSCP", true)
	case "UID":
		return rangesError(value, 1<<32-1, "UID", false)
	case "IP-ASN", "SRC-IP-ASN":
		if !isAllASCIIDigits(value) {
			return "ASN 应为非负整数。"
		}
	case "NETWORK":
		if lower := pystr.Lower(value); lower != "tcp" && lower != "udp" {
			return "NETWORK 只接受 tcp 或 udp。"
		}
	case "AND", "OR", "NOT":
		return logicError(kind, value, depth)
	}
	// SUB-RULE and RULE-SET reference other configuration; their names are
	// validated by the owning configuration or the core.
	return ""
}

func isAllASCIIDigits(s string) bool { return pyIsASCIIDigits(s) }

// RuleValueError is rule_value_error: a human-readable error or "" (Python's
// None), without rewriting either field. kind and value may be any JSON value.
func RuleValueError(kind, value any) string {
	return ruleValueError(kind, value, 0)
}
