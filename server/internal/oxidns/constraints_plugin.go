package oxidns

import (
	"configflow/internal/textx"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"configflow/internal/netipx"
	"configflow/internal/rules"
	"configflow/internal/weburl"
)

// Cross-field checks of OxiDNS 1.6.1 plugin arguments (official docs and
// factory parsers at bd183f69). Never normalizes or deletes user values.

var u32 = big.NewInt(4294967295)

func trimmed(n *Node) string { return strings.TrimSpace(str(n)) }

// deferred: a string with an unexpanded "${".
func deferred(n *Node) bool { return isStr(n) && strings.Contains(n.Value, "${") }

// argTokens is _tokens of the constraint module (strings and ints).
func argTokens(value *Node) []string {
	list := []*Node{value}
	if isSeq(value) {
		list = value.Content
	}
	var out []string
	for _, item := range list {
		if isStr(item) || isInt(item) {
			out = append(out, splitTokens(item.Value)...)
		}
	}
	return out
}

// plainUint is _uint of the constraint module: [0-9]+ up to maximum.
func plainUint(raw string, maximum *big.Int) (*big.Int, bool) {
	return boundedUint(raw, maximum, false)
}

var durationUnit = regexp.MustCompile(`(?i)^(\p{Nd}+)` + spaces + `*(ms|s|m|h|d)?$`)

// durationSeconds is _duration; ok false for invalid.
func durationSeconds(value *Node) (float64, bool) {
	if v, ok := intOf(value); ok {
		if v.Sign() < 0 || v.Cmp(u64) > 0 {
			return 0, false
		}
		f, _ := new(big.Float).SetInt(v).Float64()
		return f, true
	}
	if !isStr(value) {
		return 0, false
	}
	m := durationUnit.FindStringSubmatch(strings.TrimSpace(value.Value))
	if m == nil {
		return 0, false
	}
	n, ok := plainUint(m[1], u64)
	if !ok {
		return 0, false
	}
	unit, known := map[string]float64{"ms": .001, "s": 1, "m": 60, "h": 3600, "d": 86400, "": 1}[strings.ToLower(m[2])]
	if !known {
		return 0, false // a case-folded unit such as "ſ"
	}
	f, _ := new(big.Float).SetInt(n).Float64()
	return f * unit, true
}

var lookaround = regexp.MustCompile(`\(\?[=!]|\(\?<[=!]`)

// regexError is _regex_error: Rust regex has no backreferences/lookaround.
func regexError(raw string) string {
	for i := 0; i+1 < len(raw); i++ {
		if raw[i] == '\\' && raw[i+1] >= '1' && raw[i+1] <= '9' && (i == 0 || raw[i-1] != '\\') {
			return "OxiDNS 的正则不支持反向引用或前后查找"
		}
	}
	if lookaround.MatchString(raw) {
		return "OxiDNS 的正则不支持反向引用或前后查找"
	}
	for _, unicodeSyntax := range []string{`\p`, `\P`, "&&", "--", "~~"} {
		if strings.Contains(raw, unicodeSyntax) {
			return ""
		}
	}
	var unsupported *rules.UnsupportedRegexError
	if _, err := rules.CompilePyRegex(raw, false); err != nil && !errors.As(err, &unsupported) {
		return "正则表达式语法无效"
	}
	return ""
}

func hasPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// domainError is _domain_error.
func domainError(value *Node, dynamic bool) string {
	if !isStr(value) || deferred(value) {
		return ""
	}
	raw := strings.TrimSpace(value.Value)
	switch {
	case raw == "" || strings.HasPrefix(raw, "#"):
		if dynamic {
			return "域名规则不能为空"
		}
	case strings.HasPrefix(raw, "regexp:"):
		if strings.TrimSpace(raw[7:]) == "" {
			return "正则表达式不能为空"
		}
		return regexError(raw[7:])
	case dynamic || hasPrefix(raw, "full:", "domain:", "keyword:"):
		if hasPrefix(raw, "full:", "domain:", "keyword:") {
			raw = raw[strings.IndexByte(raw, ':')+1:]
		}
		if strings.TrimRight(strings.TrimSpace(raw), ".") == "" {
			return "域名规则不能为空"
		}
	}
	return ""
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) }

// replaceWord is re.sub(r'\b'+name+r'\b', value, s).
func replaceWord(s, name, value string) string {
	var b strings.Builder
	r := []rune(s)
	n := []rune(name)
	for i := 0; i < len(r); {
		if i+len(n) <= len(r) && string(r[i:i+len(n)]) == name &&
			(i == 0 || !isWordRune(r[i-1])) && (i+len(n) == len(r) || !isWordRune(r[i+len(n)])) {
			b.WriteString(value)
			i += len(n)
			continue
		}
		b.WriteRune(r[i])
		i++
	}
	return b.String()
}

var (
	monthDay = regexp.MustCompile(`^(?:L(?:-\p{Nd}+)?|LW|\p{Nd}+W)$`)
	weekDay  = regexp.MustCompile(`^[0-7](?:L|#[1-5])$`)
	numbers  = regexp.MustCompile(`\p{Nd}+`)
	months   = strings.Fields("JAN FEB MAR APR MAY JUN JUL AUG SEP OCT NOV DEC")
	weekdays = strings.Fields("SUN MON TUE WED THU FRI SAT")
)

// cronError is _cron_error: five fields with Croner's L/W/# extensions.
func cronError(value *Node) string {
	if !isStr(value) || deferred(value) {
		return ""
	}
	parts := strings.Fields(value.Value)
	if len(parts) != 5 {
		return "必须使用 5 段 Cron 表达式（分 时 日 月 星期）"
	}
	low, high := []int64{0, 0, 1, 1, 0}, []int64{59, 23, 31, 12, 7}
	for index, field := range parts {
		field = strings.ToUpper(field)
		if index == 3 {
			for i, name := range months {
				field = replaceWord(field, name, strconv.Itoa(i+1))
			}
		}
		if index == 4 {
			for i, name := range weekdays {
				field = replaceWord(field, name, strconv.Itoa(i))
			}
		}
		bound := func(x string) (int64, bool) {
			v, ok := plainUint(x, big.NewInt(high[index]))
			if !ok {
				return 0, false
			}
			return v.Int64(), true
		}
		for _, segment := range strings.Split(field, ",") {
			if segment == "" {
				return "Cron 字段中有空条目"
			}
			if index == 2 && monthDay.MatchString(segment) {
				minimum := int64(1)
				if strings.HasPrefix(segment, "L-") {
					minimum = 0
				}
				for _, n := range numbers.FindAllString(segment, -1) {
					if v, ok := plainUint(n, big.NewInt(31)); !ok || v.Int64() < minimum {
						return "Cron 月日字段超出 1–31"
					}
				}
				continue
			}
			if index == 4 && weekDay.MatchString(segment) {
				continue
			}
			fragments := strings.Split(segment, "/")
			if len(fragments) > 2 {
				return "Cron 步长必须是字段范围内的正整数"
			}
			if len(fragments) == 2 {
				if v, ok := plainUint(fragments[1], u64); !ok || v.Sign() == 0 {
					return "Cron 步长必须是字段范围内的正整数"
				}
			}
			base := fragments[0]
			if base == "*" || base == "?" {
				if base == "?" && index != 2 && index != 4 {
					return "Cron 的 ? 只能用于日或星期字段"
				}
				continue
			}
			bounds := strings.Split(base, "-")
			if len(bounds) > 2 {
				return "Cron 字段超出允许范围"
			}
			for _, x := range bounds {
				if v, ok := bound(x); !ok || v < low[index] {
					return "Cron 字段超出允许范围"
				}
			}
			if len(bounds) == 2 {
				a, _ := bound(bounds[0])
				b, _ := bound(bounds[1])
				if a > b {
					return "Cron 范围起点不能大于终点"
				}
			}
		}
	}
	return ""
}

var (
	runtimeZone = regexp.MustCompile(`(?im)^\s*\$(?:INCLUDE|GENERATE)\b`)
	placeholder = regexp.MustCompile(`\$\{([^}]*)\}`)
	timeOfDay   = regexp.MustCompile(`^(?:[01]\p{Nd}|2[0-3]):[0-5]\p{Nd}$`)
	tcpProbe    = regexp.MustCompile(`^tcp:([0-9]+)$`)
)

// collectPluginIssues returns the non-mutating constraint issues of one plugin.
func collectPluginIssues(plugin *Node, index int) []Issue {
	if !isMap(plugin) {
		return nil
	}
	kind := str(get(plugin, "type"))
	if !isStr(get(plugin, "type")) {
		kind = ""
	}
	args := get(plugin, "args")
	var issues []Issue
	prefix := P("plugins", index, "args")
	issue := func(at Path, message, code, severity string) {
		issues = append(issues, Issue{Path: prefix.with(at), Message: message, Code: code, Severity: severity})
	}
	errorAt := func(at Path, message, code string) { issue(at, message, code, "error") }
	domain := func(raw *Node, at Path, dynamic bool) {
		if message := domainError(raw, dynamic); message != "" {
			errorAt(at, message, "domain-rule")
		}
	}
	ip := func(raw *Node, at Path, network bool) {
		if !isStr(raw) || deferred(raw) {
			return
		}
		if network {
			if _, ok := netipx.ParseNetwork(raw.Value, false); !ok {
				errorAt(at, "需要有效的 IP 或 CIDR", "ip")
			}
		} else if _, err := netip.ParseAddr(raw.Value); err != nil {
			errorAt(at, "需要有效的 IP 地址", "ip")
		}
	}
	nullable := func(mapping *Node, name string, fallback *Node) *Node {
		if v := get(mapping, name); !isNone(v) {
			return v
		}
		return fallback
	}
	compare := func(lower, upper string, defaults [2]*Node) {
		low, high := nullable(args, lower, defaults[0]), nullable(args, upper, defaults[1])
		if isNum(low) && isNum(high) {
			a, _ := ratOf(low)
			b, _ := ratOf(high)
			if a.Cmp(b) > 0 {
				errorAt(P(lower), lower+" 不能大于 "+upper, "range-order")
			}
		}
	}
	template := func(raw *Node, at Path) {
		if !isStr(raw) {
			return
		}
		stripped := strings.ReplaceAll(raw.Value, "$$", "")
		for _, m := range placeholder.FindAllStringSubmatch(stripped, -1) {
			if strings.TrimSpace(m[1]) == "" {
				errorAt(at, "占位符名称不能为空", "template")
			}
		}
		if strings.Contains(placeholder.ReplaceAllString(stripped, ""), "${") {
			errorAt(at, "占位符缺少右花括号", "template")
		}
	}

	switch kind {
	case "qname", "cname", "client_ip", "ptr_ip", "resp_ip", "question", "qtype", "qclass", "rcode", "mark", "random", "reload_provider":
		tokens := argTokens(args)
		if len(tokens) == 0 {
			message := "至少配置一个匹配值"
			if kind == "reload_provider" {
				message = "至少引用一个 Provider"
			}
			errorAt(nil, message, "required")
		}
		anyDeferred := false
		for _, token := range tokens {
			if strings.Contains(token, "${") {
				anyDeferred = true
				continue
			}
			switch kind {
			case "qname", "cname":
				if !hasPrefix(token, "$", "&") {
					domain(S(token), nil, false)
				}
			case "client_ip", "ptr_ip", "resp_ip":
				if !hasPrefix(token, "$", "&") {
					ip(S(token), nil, true)
				}
			case "question", "reload_provider":
				if !strings.HasPrefix(token, "$") || strings.TrimSpace(token[1:]) == "" {
					errorAt(nil, "这里只接受 $tag 形式的 Provider 引用", "provider-reference")
				}
			case "qtype", "qclass", "rcode":
				if !DNSCodeValid(kind, S(token)) {
					errorAt(nil, "DNS 类型、类别或响应码无效", "dns-code")
				}
			case "mark":
				if _, ok := plainUint(token, u32); !ok {
					errorAt(nil, "标记必须是 0–4294967295 的整数", "range")
				}
			}
		}
		if kind == "random" && !anyDeferred {
			valid := false
			if len(tokens) == 1 {
				f, err := strconv.ParseFloat(strings.ReplaceAll(tokens[0], "_", ""), 64)
				valid = err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) && f >= 0 && f <= 1
			}
			if !valid {
				errorAt(nil, "概率必须且只能填写一个 0–1 的数字字符串", "range")
			}
		}
	case "env":
		entries := []*Node{args}
		if isSeq(args) {
			entries = args.Content
		}
		blank := true
		for _, x := range entries {
			blank = blank && trimmed(x) == ""
		}
		if len(entries) == 0 || blank {
			errorAt(nil, "至少配置一个环境变量条件", "required")
		}
		for j, entry := range entries {
			if !isStr(entry) {
				continue
			}
			name := entry.Value
			if i := strings.IndexAny(name, "=:"); i >= 0 {
				name = name[:i]
			}
			if strings.TrimSpace(entry.Value) == "" || strings.TrimSpace(name) == "" {
				at := Path(nil)
				if isSeq(args) {
					at = P(j)
				}
				errorAt(at, "环境变量名称不能为空", "required")
			}
		}
	case "any_match":
		entries := items(args)
		if isStr(args) {
			entries = strs(textx.SplitLines(args.Value, false)).Content
		}
		if len(filter(entries, func(x *Node) bool { return trimmed(x) != "" })) == 0 {
			errorAt(nil, "至少配置一个匹配表达式", "required")
		}
	case "string_exp":
		raw := args
		if isSeq(args) {
			var parts []string
			for _, x := range args.Content {
				if isStr(x) {
					parts = append(parts, x.Value)
				}
			}
			raw = S(strings.Join(parts, " "))
		}
		if isStr(raw) && !deferred(raw) {
			tokens := strings.Fields(raw.Value)
			var source, op string
			var operands []string
			if len(tokens) > 0 {
				matched := false
				for _, candidate := range []string{"contains", "prefix", "suffix", "regexp", "eq", "zl"} {
					if strings.HasSuffix(tokens[0], candidate) && len(tokens[0]) > len(candidate) {
						source, op, operands, matched = strings.TrimSuffix(tokens[0], candidate), candidate, tokens[1:], true
						break
					}
				}
				if !matched && len(tokens) >= 2 {
					source, op, operands = tokens[0], tokens[1], tokens[2:]
				}
			}
			sources := []string{"qname", "qtype", "qclass", "rcode", "resp_ip", "mark", "client_ip", "server_name", "url_path"}
			if !containsString(sources, source) && !(strings.HasPrefix(source, "$") && len(source) > 1) {
				errorAt(nil, "字符串表达式的来源字段无效", "expression")
			}
			if !containsString([]string{"eq", "prefix", "suffix", "contains", "regexp", "zl"}, op) {
				errorAt(nil, "字符串表达式的操作无效", "expression")
			} else if (op == "zl") == (len(operands) > 0) {
				errorAt(nil, "zl 不接受参数；其他操作至少需要一个参数", "expression")
			}
			if op == "regexp" {
				for _, operand := range operands {
					if message := regexError(operand); message != "" {
						errorAt(nil, message, "regexp")
					}
				}
			}
		}
	case "client_ip_from_ecs":
		for j, raw := range items(args) {
			if trimmed(raw) != "" {
				ip(S(strings.TrimSpace(raw.Value)), P(j), true)
			}
		}
	case "ttl":
		if isStr(args) && !deferred(args) {
			tokens := strings.Split(args.Value, "-")
			bad := len(tokens) > 2
			for _, x := range tokens {
				_, ok := plainUint(strings.TrimSpace(x), u32)
				bad = bad || !ok
			}
			if bad {
				errorAt(nil, "TTL 简写需为非负整数或 min-max，数值不得大于 4294967295", "ttl")
			}
		} else if isMap(args) && isNone(get(args, "fix")) && isNone(get(args, "min")) && isNone(get(args, "max")) {
			errorAt(nil, "TTL 至少需要 fix、min 或 max 中的一个非空值", "required")
		}
	case "forward_edns0opt":
		if isStr(args) && !deferred(args) {
			for _, token := range argTokens(args) {
				if _, ok := plainUint(token, big.NewInt(65535)); !ok {
					errorAt(nil, "EDNS0 代码必须为 0–65535 的整数", "range")
					break
				}
			}
		}
	case "black_hole":
		modes := []string{"nxdomain", "nodata", "null", "custom", "refused"}
		if isMap(args) {
			rawMode := get(args, "mode")
			mode := rawMode
			if isStr(rawMode) {
				mode = S(strings.ToLower(strings.TrimSpace(rawMode.Value)))
			}
			ips := get(args, "ips")
			if has(args, "mode") && isNone(rawMode) {
				issue(P("mode"), `mode 为空时 OxiDNS 会按 nxdomain 处理；黑洞请写 "null"（带引号）`, "null-mode", "warning")
			}
			if isStr(mode) && !containsString(modes, mode.Value) && !deferred(mode) {
				errorAt(P("mode"), "拦截模式无效", "enum")
			}
			if isSeq(ips) {
				for j, raw := range ips.Content {
					if trimmed(raw) != "" {
						ip(raw, P("ips", j), false)
					}
				}
				ips = seqOf(filter(ips.Content, func(x *Node) bool { return trimmed(x) != "" }))
			}
			switch {
			case equal(mode, S("custom")) && (isSeq(ips) || isNone(ips)) && len(items(ips)) == 0:
				errorAt(P("ips"), "custom 模式至少需要一个 IP", "required")
			case !isNone(mode) && !equal(mode, S("custom")) && isSeq(ips) && len(ips.Content) > 0:
				errorAt(P("ips"), "只有 custom 模式可以配置 ips", "mutually-exclusive")
			}
		} else if isStr(args) || isSeq(args) {
			tokens := argTokens(args)
			mode := ""
			if isStr(args) && len(tokens) > 0 && strings.HasPrefix(tokens[len(tokens)-1], "short_circuit=") {
				value := strings.SplitN(tokens[len(tokens)-1], "=", 2)[1]
				tokens = tokens[:len(tokens)-1]
				if value != "true" && value != "false" {
					errorAt(nil, "short_circuit 必须为 true 或 false", "boolean")
				}
			}
			if isStr(args) && len(tokens) > 0 && containsString(modes, strings.ToLower(tokens[0])) {
				mode, tokens = strings.ToLower(tokens[0]), tokens[1:]
			}
			if mode == "custom" && len(tokens) == 0 {
				errorAt(nil, "custom 模式至少需要一个 IP", "required")
			}
			if mode != "" && mode != "custom" && len(tokens) > 0 {
				errorAt(nil, "只有 custom 模式可以附加 IP", "mutually-exclusive")
			}
			for _, token := range tokens {
				ip(S(token), nil, false)
			}
		}
	}
	if !isMap(args) || kind == "sequence" {
		return issues
	}
	switch kind {
	case "cache":
		if !isNone(get(args, "dump_file")) && numericZero(get(args, "dump_interval")) {
			errorAt(P("dump_interval"), "配置持久化文件时，保存间隔必须大于 0", "range")
		}
		compare("min_positive_ttl", "max_positive_ttl", [2]*Node{})
	case "prefer_ipv4", "prefer_ipv6":
		if isTrue(nullable(args, "cache", B(true))) && numericZero(get(args, "cache_ttl")) {
			errorAt(P("cache_ttl"), "启用探测缓存时 TTL 必须大于 0", "range")
		}
	case "ip_selector":
		if methods := get(args, "probe_methods"); isStr(methods) || isSeq(methods) {
			list := []*Node{methods}
			if isSeq(methods) {
				list = methods.Content
			}
			var tokens []string
			for _, raw := range list {
				if isStr(raw) {
					for _, piece := range strings.Split(raw.Value, ",") {
						if piece = strings.TrimSpace(piece); piece != "" {
							tokens = append(tokens, piece)
						}
					}
				}
			}
			if len(tokens) == 0 {
				errorAt(P("probe_methods"), "至少配置一种探测方法", "required")
			}
			distinct := map[string]bool{}
			for _, token := range tokens {
				distinct[token] = true
				if strings.Contains(token, "${") {
					continue
				}
				m := tcpProbe.FindStringSubmatch(token)
				valid := token == "ping" || token == "none"
				if m != nil {
					v, ok := plainUint(m[1], big.NewInt(65535))
					valid = valid || ok && v.Sign() != 0
				}
				if !valid {
					errorAt(P("probe_methods"), "探测方法应为 ping、none 或 tcp:1–65535", "probe-method")
				}
			}
			if distinct["none"] && len(distinct) > 1 {
				errorAt(P("probe_methods"), "none 不能与其他探测方法同时使用", "mutually-exclusive")
			}
		}
		if cache := get(args, "cache"); isMap(cache) && isTrue(nullable(cache, "enabled", B(true))) {
			for _, key := range []string{"size", "ttl", "failure_ttl"} {
				if numericZero(get(cache, key)) {
					errorAt(P("cache", key), "启用缓存时此值必须大于 0", "range")
				}
			}
		}
	case "ecs_handler":
		if trimmed(get(args, "preset")) != "" {
			ip(get(args, "preset"), P("preset"), false)
		}
	case "hosts", "redirect":
		key := map[string]string{"hosts": "entries", "redirect": "rules"}[kind]
		for j, entry := range items(get(args, key)) {
			if !isStr(entry) || deferred(entry) || strings.TrimSpace(entry.Value) == "" || strings.HasPrefix(strings.TrimLeftFunc(entry.Value, unicode.IsSpace), "#") {
				continue
			}
			tokens := strings.Fields(entry.Value)
			if kind == "hosts" && len(tokens) < 2 {
				errorAt(P(key, j), "需填写域名规则及至少一个 IP", "rule")
				continue
			}
			if kind == "redirect" && len(tokens) != 2 {
				errorAt(P(key, j), "重定向规则需恰好填写域名规则和目标域名", "rule")
				continue
			}
			if strings.HasPrefix(tokens[0], "regexp:") {
				if message := regexError(tokens[0][7:]); message != "" {
					errorAt(P(key, j), message, "regexp")
				}
			}
			if kind == "hosts" {
				for _, token := range tokens[1:] {
					ip(S(token), P(key, j), false)
				}
				continue
			}
			target := strings.TrimRight(tokens[1], ".")
			bad := len(target) > 253
			for _, part := range strings.Split(target, ".") {
				bad = bad || part == "" || len(part) > 63
			}
			if bad {
				errorAt(P(key, j), "重定向的目标域名无效", "domain")
			}
		}
	case "domain_set", "dynamic_domain_set":
		key := map[string]string{"domain_set": "exps", "dynamic_domain_set": "bootstrap_rules"}[kind]
		for j, entry := range items(get(args, key)) {
			if !isStr(entry) {
				continue
			}
			if kind == "dynamic_domain_set" {
				domain(entry, P(key, j), true)
				continue
			}
			for _, line := range textx.SplitLines(entry.Value, false) {
				domain(S(line), P(key, j), false)
			}
		}
	case "arbitrary":
		for j, raw := range items(get(args, "rules")) {
			if message := zoneRecordsError(raw, false); message != "" {
				errorAt(P("rules", j), message, "dns-record")
			}
			if isStr(raw) && runtimeZone.MatchString(raw.Value) {
				issue(P("rules", j), "外部 zone 文件和生成记录需要目标核心在初始化时检查", "runtime-zone", "warning")
			}
		}
	case "response":
		for _, key := range []string{"answers", "authorities", "additionals"} {
			for j, raw := range items(get(args, key)) {
				if isStr(raw) && runtimeZone.MatchString(raw.Value) {
					issue(P(key, j), "外部 zone 文件和生成记录需要目标核心在初始化时检查", "runtime-zone", "warning")
				}
			}
		}
	case "adguard_rule":
		for j, raw := range items(get(args, "rules")) {
			if !isStr(raw) {
				continue
			}
			for _, line := range textx.SplitLines(raw.Value, false) {
				if message, severity := adguardError(line); message != "" {
					issue(P("rules", j), message, "adguard-rule", severity)
				}
			}
		}
	case "geosite":
		for j, selector := range items(get(args, "selectors")) {
			if isStr(selector) && strings.Contains(selector.Value, "@") && !deferred(selector) {
				code, attr, _ := strings.Cut(strings.TrimSpace(selector.Value), "@")
				if strings.TrimSpace(code) == "" || strings.TrimSpace(attr) == "" {
					errorAt(P("selectors", j), "分类名称和 @ 后的属性名不能为空", "selector")
				}
			}
		}
	case "ip_set":
		for j, entry := range items(get(args, "ips")) {
			if !isStr(entry) {
				continue
			}
			for _, line := range textx.SplitLines(entry.Value, false) {
				if token := strings.TrimSpace(strings.SplitN(line, "#", 2)[0]); token != "" {
					ip(S(token), P("ips", j), true)
				}
			}
		}
	case "nftset":
		for _, family := range []struct {
			suffix string
			limit  int64
		}{{"4", 32}, {"6", 128}} {
			s := family.suffix
			if !isNone(get(args, "ipv"+s)) || isNone(get(args, "table_family"+s)) || isNone(get(args, "table_name"+s)) || isNone(get(args, "set_name"+s)) {
				continue
			}
			if f := get(args, "table_family"+s); isStr(f) && !containsString([]string{"ip", "ip6", "inet"}, f.Value) {
				errorAt(P("table_family"+s), "地址族必须为 ip、ip6 或 inet", "enum")
			}
			for _, key := range []string{"table_name", "set_name"} {
				if v := get(args, key+s); isStr(v) && strings.TrimSpace(v.Value) == "" {
					errorAt(P(key+s), "名称不能为空", "required")
				}
			}
			if mask := get(args, "mask"+s); isNum(mask) {
				if r, _ := ratOf(mask); r.Cmp(big.NewRat(family.limit, 1)) > 0 {
					errorAt(P("mask"+s), "前缀长度不能超过 "+strconv.FormatInt(family.limit, 10), "range")
				}
			}
		}
	case "ros_route", "ros_address_list":
		compare("min_ttl", "max_ttl", [2]*Node{I(60), I(3600)})
		families := []string{"address_list4", "address_list6"}
		if kind == "ros_route" {
			families = []string{"gateway4", "gateway6"}
		}
		if trimmed(get(args, families[0])) == "" && trimmed(get(args, families[1])) == "" {
			errorAt(P(families[0]), "至少配置 "+strings.Join(families, " 或 "), "required")
		}
		if p := get(args, "comment_prefix"); isStr(p) && strings.ContainsAny(p.Value, ";=") {
			errorAt(P("comment_prefix"), "注释前缀不能包含 ; 或 =", "delimiter")
		}
		if tls := get(args, "tls"); isMap(tls) && isTrue(get(tls, "insecure")) && trimmed(get(tls, "ca")) != "" {
			errorAt(P("tls", "ca"), "跳过 TLS 校验时不能同时指定 CA", "mutually-exclusive")
		}
		if seconds, ok := durationSeconds(get(args, "wait_timeout")); ok && seconds == 0 {
			errorAt(P("wait_timeout"), "等待超时必须大于 0", "range")
		}
		if persistent := get(args, "persistent"); isMap(persistent) {
			for j, raw := range items(get(persistent, "ips")) {
				if !isStr(raw) || deferred(raw) {
					continue
				}
				for _, line := range textx.SplitLines(raw.Value, false) {
					line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
					if line == "" {
						continue
					}
					network, ok := netipx.ParseNetwork(line, false)
					if !ok {
						errorAt(P("persistent", "ips", j), "常驻地址需为有效 IP 或 CIDR", "ip")
						continue
					}
					target := families[0]
					if !network.Addr().Is4() {
						target = families[1]
					}
					if trimmed(get(args, target)) == "" {
						issue(P("persistent", "ips", j), "未设置该地址族的目标，OxiDNS 会忽略此常驻地址", "ignored-family", "warning")
					}
					if kind == "ros_route" && network.Bits() == 0 {
						issue(P("persistent", "ips", j), "OxiDNS 会忽略 /0 默认路由", "ignored-default-route", "warning")
					}
				}
			}
		}
	}
	if kind == "http_request" || kind == "learn_domain" || kind == "upgrade" {
		timeout := get(args, "timeout")
		if _, ok := durationSeconds(timeout); trimmed(timeout) != "" && !deferred(timeout) && !ok {
			errorAt(P("timeout"), "超时需为非负整数秒或带 ms/s/m/h/d 单位的时长", "duration")
		}
	}
	switch kind {
	case "http_request":
		count := 0
		for _, present := range []bool{!isNone(get(args, "body")), !isNone(get(args, "json")), (isMap(get(args, "form")) && mapSize(get(args, "form")) > 0)} {
			if present {
				count++
			}
		}
		if count > 1 {
			errorAt(P("body"), "body、json 和非空 form 不能同时配置", "mutually-exclusive")
		}
		if !isNone(get(args, "content_type")) && (!isNone(get(args, "json")) || (isMap(get(args, "form")) && mapSize(get(args, "form")) > 0)) {
			errorAt(P("content_type"), "content_type 仅用于原始 body", "dependency")
		}
		for _, key := range []string{"url", "body", "content_type"} {
			if value := get(args, key); isStr(value) {
				template(value, P(key))
				if key == "content_type" && strings.ContainsAny(value.Value, "\r\n") {
					errorAt(P(key), "HTTP 头值不能包含换行", "http-header")
				}
			}
		}
		if url := get(args, "url"); isStr(url) && !deferred(url) {
			u, err := weburl.Parse(url.Value)
			if err != nil || !containsString([]string{"http", "https"}, strings.ToLower(u.Scheme)) || weburl.Hostname(u) == "" {
				errorAt(P("url"), "需要有效的 HTTP / HTTPS URL", "url")
			}
		}
		for _, key := range []string{"headers", "query_params", "form"} {
			pairs(get(args, key), func(name string, value *Node) bool {
				at := P(key, name)
				if key != "query_params" && strings.TrimSpace(name) == "" {
					errorAt(at, "名称不能为空", "required")
				}
				if key == "headers" && !tokenRE.MatchString(strings.TrimSpace(name)) {
					errorAt(at, "HTTP 请求头名称无效", "http-header")
				}
				if isStr(value) && strings.TrimSpace(value.Value) == "" {
					errorAt(at, "请求模板值不能为空", "required")
				}
				template(value, at)
				return true
			})
		}
	case "script":
		for j, value := range items(get(args, "args")) {
			template(value, P("args", j))
		}
		pairs(get(args, "env"), func(key string, value *Node) bool {
			if strings.TrimSpace(key) == "" {
				errorAt(P("env", key), "环境变量名称不能为空", "required")
			}
			template(value, P("env", key))
			return true
		})
	case "download":
		seen := map[string]bool{}
		for j, item := range items(get(args, "downloads")) {
			if !isMap(item) || deferred(get(item, "url")) || deferred(get(item, "dir")) {
				continue
			}
			filename := trimmed(get(item, "filename"))
			if filename == "" {
				u, err := weburl.Parse(trimmed(get(item, "url")))
				if err != nil {
					continue
				}
				p := weburl.RawPath(u)
				filename = strings.TrimSpace(p[strings.LastIndexByte(p, '/')+1:])
			}
			if filename == "" {
				errorAt(P("downloads", j, "filename"), "URL 无法推导文件名，请填写 filename", "required")
			}
			target := filename
			if dir := trimmed(get(item, "dir")); dir != "" && !strings.HasPrefix(filename, "/") {
				target = strings.TrimSuffix(dir, "/") + "/" + filename
				if strings.HasSuffix(dir, "/") {
					target = dir + filename
				}
			}
			if seen[target] {
				errorAt(P("downloads", j), "下载目标文件重复", "duplicate")
			}
			seen[target] = true
		}
	case "cron":
		if tz := get(args, "timezone"); trimmed(tz) != "" && !deferred(tz) && !validZone(strings.TrimSpace(tz.Value)) {
			errorAt(P("timezone"), "需要有效的 IANA 时区，例如 Asia/Shanghai", "timezone")
		}
		names := map[string]bool{}
		for j, job := range items(get(args, "jobs")) {
			if !isMap(job) {
				continue
			}
			name := trimmed(get(job, "name"))
			if names[name] {
				errorAt(P("jobs", j, "name"), "任务名称重复", "duplicate")
			}
			names[name] = true
			schedule, interval := get(job, "schedule"), get(job, "interval")
			if (trimmed(schedule) != "") == (trimmed(interval) != "") {
				errorAt(P("jobs", j), "schedule 和 interval 必须且只能填写一个", "mutually-exclusive")
			}
			if trimmed(schedule) != "" {
				if message := cronError(schedule); message != "" {
					errorAt(P("jobs", j, "schedule"), message, "cron")
				}
			}
			if trimmed(interval) != "" && !deferred(interval) {
				if seconds, ok := durationSeconds(interval); !ok || seconds < 60 {
					errorAt(P("jobs", j, "interval"), "定时间隔至少为 1 分钟", "range")
				}
			}
		}
	case "time":
		for j, period := range items(get(args, "periods")) {
			if !isMap(period) {
				continue
			}
			start, end := get(period, "start"), get(period, "end")
			if isNone(start) != isNone(end) {
				errorAt(P("periods", j), "start 和 end 必须同时填写或同时省略", "dependency")
			}
			for _, field := range []struct {
				key   string
				value *Node
			}{{"start", start}, {"end", end}} {
				if isStr(field.value) && !deferred(field.value) && !timeOfDay.MatchString(strings.TrimSpace(field.value.Value)) {
					errorAt(P("periods", j, field.key), "时间需为 00:00–23:59 的 HH:MM", "time")
				}
			}
			if isStr(start) && isStr(end) && !deferred(start) && strings.TrimSpace(start.Value) == strings.TrimSpace(end.Value) {
				errorAt(P("periods", j, "end"), "开始和结束时间不能相同", "range")
			}
			weekdays, monthdays := get(period, "weekdays"), get(period, "monthdays")
			// Wrong container types already have a schema diagnostic. Only
			// meaningful day-list shapes participate in the cross-field rule.
			dayLists := (isNone(weekdays) || isSeq(weekdays)) && (isNone(monthdays) || isSeq(monthdays))
			if isNone(start) && dayLists && len(items(weekdays)) == 0 && len(items(monthdays)) == 0 {
				errorAt(P("periods", j), "省略时间段时至少设置星期或月日", "required")
			}
			for k, day := range items(get(period, "weekdays")) {
				if isStr(day) && !deferred(day) && !containsString([]string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}, strings.ToLower(strings.TrimSpace(day.Value))) {
					errorAt(P("periods", j, "weekdays", k), "星期为 mon–sun 或数字 1–7；数字字符串不支持", "weekday")
				}
			}
		}
	}
	return issues
}

// ValidatePluginFormat is validate_plugin_format: dns-record (one response
// record) and domain-expression.
func ValidatePluginFormat(name string, value *Node) string {
	switch name {
	case "dns-record":
		return zoneRecordsError(value, true)
	case "domain-expression":
		return domainError(value, false)
	}
	return fmt.Sprintf(messageUnsupportedFormat, name)
}

var (
	urlChars    = regexp.MustCompile(`[/?:#]|` + spaces)
	domainChars = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// adguardError is _adguard_error: (message, severity) of one filter line.
func adguardError(line string) (string, string) {
	raw := strings.TrimSpace(line)
	if raw == "" || hasPrefix(raw, "!", "#") || strings.Contains(raw, "${") {
		return "", "warning"
	}
	for _, marker := range []string{"##", "#@#", "#$#", "#@$#", "#%#", "#@%#", "#?#", "#@?#"} {
		if strings.Contains(raw, marker) {
			return "此为非 DNS 过滤规则，OxiDNS 会跳过", "warning"
		}
	}
	if words := strings.Fields(raw); len(words) > 1 {
		if _, err := netip.ParseAddr(words[0]); err == nil {
			return "Hosts 格式规则会被 AdGuard Provider 跳过", "warning"
		}
	}
	if strings.HasPrefix(raw, "@@") {
		raw = strings.TrimSpace(raw[2:])
	}
	var pattern string
	modifiers, hasModifiers := "", false
	if strings.HasPrefix(raw, "/") {
		switch {
		case strings.HasSuffix(raw, "/") && len(raw) > 1:
			pattern = raw
		case strings.Contains(raw, "/$"):
			i := strings.LastIndex(raw, "/$")
			pattern, modifiers, hasModifiers = raw[:i]+"/", raw[i+2:], true
		default:
			return "路径或 URL 过滤规则不用于 DNS，OxiDNS 会跳过", "warning"
		}
	} else {
		pattern, modifiers, hasModifiers = strings.Cut(raw, "$")
	}
	pattern = strings.TrimSpace(pattern)
	type modifier struct {
		name, value string
		sep         bool
	}
	var parsed []modifier
	if hasModifiers {
		for _, part := range strings.Split(modifiers, ",") {
			if part = strings.TrimSpace(part); part != "" {
				name, value, sep := strings.Cut(part, "=")
				parsed = append(parsed, modifier{strings.ToLower(name), value, sep})
			}
		}
	}
	known := func(name string) bool {
		return containsString([]string{"important", "badfilter", "denyallow", "dnstype"}, name)
	}
	if pattern == "" {
		required := !hasModifiers
		for _, m := range parsed {
			required = required || known(m.name)
		}
		if required {
			return "AdGuard 规则的匹配模式不能为空", "error"
		}
		return "规则仅含不支持的修饰符，OxiDNS 会跳过", "warning"
	}
	if !strings.HasPrefix(pattern, "/") && urlChars.MatchString(pattern) {
		return "路径或 URL 过滤规则不用于 DNS，OxiDNS 会跳过", "warning"
	}
	for _, m := range parsed {
		if !known(m.name) {
			return "规则含 OxiDNS 不支持的修饰符，整条规则会跳过", "warning"
		}
	}
	for _, m := range parsed {
		if (m.name == "denyallow" || m.name == "dnstype") && !m.sep {
			return "denyallow/dnstype 修饰符需要等号和值", "error"
		}
		for _, item := range strings.Split(m.value, "|") {
			switch m.name {
			case "denyallow":
				if strings.TrimSpace(item) != "" && !domainChars.MatchString(strings.TrimRight(strings.TrimSpace(item), ".")) {
					return "denyallow 中需要合法域名", "error"
				}
			case "dnstype":
				token := strings.TrimLeft(strings.TrimSpace(item), "~")
				if token != "" && (strings.Trim(token, "0123456789") == "" || !DNSCodeValid("qtype", S(token))) {
					return "dnstype 中需要有效的 DNS 类型名称", "error"
				}
			}
		}
	}
	if strings.HasPrefix(pattern, "/") {
		regex := ""
		if len(pattern) > 1 {
			regex = pattern[1 : len(pattern)-1]
		}
		if strings.TrimSpace(regex) == "" {
			return "AdGuard 正则不能为空", "error"
		}
		if message := regexError(regex); message != "" {
			return message, "error"
		}
		return "", "warning"
	}
	normalized := strings.ToLower(strings.TrimRight(pattern, "."))
	if normalized == "" {
		return "AdGuard 匹配域名不能为空", "error"
	}
	rest := strings.TrimPrefix(normalized, "||")
	if rest == normalized {
		rest = strings.TrimPrefix(normalized, "|")
	}
	rest = strings.TrimSuffix(rest, "|")
	if strings.Contains(rest, "|") {
		return "AdGuard 模式内部不能包含 |", "error"
	}
	return "", "warning"
}
