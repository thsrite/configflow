package rules

import (
	"configflow/internal/textx"
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strconv"
	"strings"
)

// Rule syntax (backend/utils/rule_syntax.py): explicit Mihomo rule types and
// clearly invalid values. These checks do not replace the core: geo data,
// provider references and complex regex semantics are validated there.

// ruleTypes is the type list of Mihomo's rules/parser.go, sorted.
var ruleTypes = []string{
	"AND", "DOMAIN", "DOMAIN-KEYWORD", "DOMAIN-REGEX", "DOMAIN-SUFFIX", "DOMAIN-WILDCARD",
	"DSCP", "DST-PORT", "GEOIP", "GEOSITE", "IN-NAME", "IN-PORT", "IN-TYPE", "IN-USER",
	"IP-ASN", "IP-CIDR", "IP-CIDR6", "IP-SUFFIX", "MATCH", "NETWORK", "NOT", "OR",
	"PROCESS-NAME", "PROCESS-NAME-REGEX", "PROCESS-NAME-WILDCARD", "PROCESS-PATH",
	"PROCESS-PATH-REGEX", "PROCESS-PATH-WILDCARD", "REMATCH-NAME", "RULE-SET",
	"SRC-GEOIP", "SRC-IP-ASN", "SRC-IP-CIDR", "SRC-IP-SUFFIX", "SRC-PORT", "SUB-RULE", "UID",
}

// IsRuleType reports whether kind is a Mihomo rule type.
func IsRuleType(kind string) bool {
	_, found := slices.BinarySearch(ruleTypes, kind)
	return found
}

// AllowsCommaPayload reports whether a rule type keeps commas inside its payload.
func AllowsCommaPayload(kind string) bool { return commaPayload(kind) }

// commaPayload reports the types whose payload may contain commas.
func commaPayload(kind string) bool {
	switch kind {
	case "AND", "OR", "NOT", "SUB-RULE", "DOMAIN-REGEX", "PROCESS-NAME-REGEX", "PROCESS-PATH-REGEX":
		return true
	}
	return false
}

// LooksLikeIP keeps a malformed address or network from being read as a
// host name.
func LooksLikeIP(value string) bool {
	return strings.ContainsAny(value, ":/") || value != "" && strings.Trim(value, "0123456789.") == ""
}

// validDNSLabel is one LDH label: letters, digits, inner hyphens, 1–63 long.
func validDNSLabel(label string) bool {
	alnum := func(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	if label == "" || len(label) > 63 || !alnum(label[0]) || !alnum(label[len(label)-1]) {
		return false
	}
	for i := 1; i < len(label)-1; i++ {
		if !alnum(label[i]) && label[i] != '-' {
			return false
		}
	}
	return true
}

// DomainValueError is the message for an invalid DOMAIN/DOMAIN-SUFFIX value;
// "" when valid. Internationalised names are checked in their ASCII form.
func DomainValueError(value string) string {
	if value == "" || LooksLikeIP(value) {
		return "不是有效域名；IP 地址或网段请使用 IP-CIDR。"
	}
	ascii, ok := IDNAEncode(value)
	if !ok || len(ascii) > 253 || textx.ASCIIDigits(ascii[strings.LastIndex(ascii, ".")+1:]) {
		return "不是有效域名。"
	}
	for _, label := range strings.Split(ascii, ".") {
		if !validDNSLabel(label) {
			return "不是有效域名。"
		}
	}
	return ""
}

// CIDRError is the message for an invalid CIDR value; "" when valid. Like
// Mihomo (netip.ParsePrefix), either family is accepted.
func CIDRError(value string) string {
	if value == "" {
		return "请填写有效 CIDR，例如 192.0.2.0/24 或 2001:db8::/32。"
	}
	cut := strings.LastIndex(value, "/")
	if cut < 0 {
		addr, ok := parseAddr(value)
		if !ok {
			return "不是有效 IP 网段；IPCIDR 只接受带前缀长度的 CIDR。"
		}
		return fmt.Sprintf("单个 IP 也需要前缀长度，请填写 %s/%d。", value, addr.BitLen())
	}
	address, prefix := value[:cut], value[cut+1:]
	if !textx.ASCIIDigits(prefix) || prefix != "0" && prefix[0] == '0' || strings.Contains(address, "%") {
		return "不是有效 IP 网段；请使用数字前缀长度。"
	}
	if _, ok := parseNetwork(value); !ok {
		return "不是有效 IP 网段；请检查地址与前缀长度。"
	}
	return ""
}

// SplitRulePayload splits a rule line into type, payload and parameters
// like Mihomo's ParseRulePayload(..., false): regex and logic payloads keep
// their commas.
func SplitRulePayload(line string) (kind, payload string, params []string) {
	fields := strings.Split(line, ",")
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	kind = strings.ToUpper(fields[0])
	params = []string{}
	switch {
	case commaPayload(kind):
		payload = strings.Join(fields[1:], ",")
	case len(fields) > 1:
		payload, params = fields[1], append(params, fields[2:]...)
	}
	return kind, payload, params
}

var (
	rangeSeparator = regexp.MustCompile(`[/,]`)
	rangeSegment   = regexp.MustCompile(`^([0-9]+)(?:-([0-9]+))?$`)
)

func rangesError(value string, maximum uint64, label string, wildcard bool) string {
	if value == "*" && wildcard {
		return ""
	}
	segments := rangeSeparator.Split(value, -1)
	if len(segments) > 28 {
		return label + "最多支持 28 组范围。"
	}
	digits := len(strconv.FormatUint(maximum, 10))
	for _, segment := range segments {
		m := rangeSegment.FindStringSubmatch(segment)
		if m == nil {
			return label + "请填写数字或范围，以 / 分隔，例如 80/443/8000-9000。"
		}
		first, last := trimZeros(m[1]), trimZeros(m[1])
		if m[2] != "" {
			last = trimZeros(m[2])
		}
		if len(first) > digits || len(last) > digits {
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

var (
	verboseFlag  = regexp.MustCompile(`\(\?[A-Za-z-]*x[A-Za-z-]*(?::|\))`)
	quotedRegex  = regexp.MustCompile(`(?s)\\Q(.*?)(?:\\E|$)`)
	controlRegex = regexp.MustCompile(`\\c.`)
	posixClass   = regexp.MustCompile(`\[:\^?[A-Za-z]+:\]`)
)

// regexError reports clear structural errors only: Python, Mihomo (regexp2)
// and other cores do not share one dialect, so advanced constructs are left
// to the destination core.
func regexError(value string) string {
	// In verbose mode and inline comments apparent delimiters can be comment
	// text; leave those expressions to the core.
	if strings.Contains(value, "(?#") || verboseFlag.MatchString(value) {
		return ""
	}
	// Mask literal quoting, control escapes (they consume the next character)
	// and POSIX classes (they can contain brackets).
	probe := quotedRegex.ReplaceAllStringFunc(value, func(m string) string {
		return regexp.QuoteMeta(quotedRegex.FindStringSubmatch(m)[1])
	})
	probe = controlRegex.ReplaceAllString(probe, "a")
	probe = posixClass.ReplaceAllString(probe, "a")
	depth, inClass, classItems, escaped := 0, false, 0, false
	for _, char := range probe {
		switch {
		case escaped:
			escaped = false
			if inClass {
				classItems++
			}
		case char == '\\':
			escaped = true
		case inClass:
			if char == ']' && classItems > 0 {
				inClass = false
			} else if char != '^' || classItems > 0 {
				classItems++
			}
		case char == '[':
			inClass, classItems = true, 0
		case char == '(':
			depth++
		case char == ')':
			if depth--; depth < 0 {
				return "正则表达式的括号不匹配。"
			}
		}
	}
	if escaped || inClass || depth != 0 {
		return "正则表达式含未闭合括号、字符组或末尾转义。"
	}
	// The RE2 parser sees the pattern's structure (look-around and back
	// references are plain groups in this spelling) and names the error.
	// Unknown escapes are left to the core, like other dialect questions.
	tr := translate(probe, false)
	if tr.tooLarge {
		return "正则表达式格式无效：" + ErrRepeatTooLarge.Error()
	}
	if tr.badRange && tr.badEscape == "" {
		return "正则表达式格式无效：bad character range: a class escape cannot be a range end"
	}
	_, err := syntax.Parse(tr.source(false), syntax.Perl)
	var se *syntax.Error
	if tr.unsupported != "" || tr.badEscape != "" || !errors.As(err, &se) {
		return ""
	}
	switch se.Code {
	case syntax.ErrNestingDepth:
		return "正则表达式嵌套过深，请简化表达式。"
	case syntax.ErrInvalidRepeatSize: // RE2 also limits nested counts; only m > n is an error
		if tr.badRepeat {
			return "正则表达式格式无效：" + err.Error()
		}
	case syntax.ErrMissingRepeatArgument, syntax.ErrInvalidRepeatOp,
		syntax.ErrInvalidCharRange, syntax.ErrMissingBracket, syntax.ErrMissingParen,
		syntax.ErrUnexpectedParen, syntax.ErrTrailingBackslash:
		return "正则表达式格式无效：" + err.Error()
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
	// Follow the core's literal parenthesis nesting: commas inside a regex or
	// a nested condition do not separate outer rules.
	nesting, start := 0, 0
	var children []string
	for index, char := range value {
		switch char {
		case '(':
			if nesting++; nesting == 2 {
				start = index + 1
			}
		case ')':
			if nesting == 2 {
				children = append(children, value[start:index])
			}
			if nesting--; nesting < 0 {
				return "逻辑规则的括号不匹配。"
			}
		}
	}
	if nesting != 0 {
		return "逻辑规则的括号不匹配。"
	}
	if len(children) == 0 || kind == "NOT" && len(children) != 1 {
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

func ruleValueError(kind, value string, depth int) string {
	if !IsRuleType(kind) {
		message := "未知规则类型 " + kind + "。"
		if suggestion, ok := closestMatch(kind, ruleTypes, 0.75); ok {
			message += "是否要填写 " + suggestion + "？"
		}
		return message
	}
	if strings.ContainsAny(value, "\n\r") {
		return "规则值必须是单行文本。"
	}
	blank := strings.TrimSpace(value) == ""
	switch {
	case kind == "MATCH" && blank:
		return ""
	case kind == "MATCH":
		return "MATCH 不接受规则值。"
	case blank:
		return "规则值为空。"
	case strings.Contains(value, ",") && !commaPayload(kind):
		return "此规则值不能包含逗号；端口列表请使用 / 分隔，策略与参数应单独填写。"
	case strings.HasSuffix(kind, "-REGEX"):
		return regexError(value)
	}
	switch kind {
	case "DOMAIN", "DOMAIN-SUFFIX":
		return DomainValueError(value)
	case "IP-CIDR", "IP-CIDR6", "SRC-IP-CIDR", "IP-SUFFIX", "SRC-IP-SUFFIX":
		return CIDRError(value)
	case "SRC-PORT", "DST-PORT", "IN-PORT":
		return rangesError(value, 65535, "端口", false)
	case "DSCP":
		return rangesError(value, 63, "DSCP", true)
	case "UID":
		return rangesError(value, 1<<32-1, "UID", false)
	case "IP-ASN", "SRC-IP-ASN":
		if !textx.ASCIIDigits(value) {
			return "ASN 应为非负整数。"
		}
	case "NETWORK":
		if lower := strings.ToLower(value); lower != "tcp" && lower != "udp" {
			return "NETWORK 只接受 tcp 或 udp。"
		}
	case "AND", "OR", "NOT":
		return logicError(kind, value, depth)
	}
	// SUB-RULE and RULE-SET reference other configuration; the owning
	// configuration or the core validates them.
	return ""
}

// RuleValueError is a human-readable message for an invalid rule type or
// value, "" when valid; neither field is rewritten.
func RuleValueError(kind, value string) string {
	return ruleValueError(kind, value, 0)
}
