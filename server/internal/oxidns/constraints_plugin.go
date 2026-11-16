package oxidns

import (
	"math"
	"math/big"
	"regexp"
	"strings"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/urlx"
)

// Port of backend/common/oxidns_plugin_constraints.py (module C23):
// cross-field checks of OxiDNS 1.6.1 plugin arguments.

// strOf is _str(value): value.strip() for a str, else "".
func strOf(v any) string {
	if s, ok := v.(string); ok {
		return pystr.Strip(s)
	}
	return ""
}

// isNumber is _number(value): an int (not bool) or a finite float.
func isNumber(v any) bool {
	n, ok := v.(jsonx.Number)
	return ok && (n.IsInt() || finite(n))
}

// deferredText is the plugin module's _deferred(value): a str with "${".
func deferredText(v any) bool {
	s, ok := v.(string)
	return ok && strings.Contains(s, "${")
}

// constraintTokens is the plugin module's _tokens(value): str and int (not
// bool) items, split on white space and commas.
func constraintTokens(value any) []string {
	values, isList := value.([]any)
	if !isList {
		values = []any{value}
	}
	var out []string
	for _, item := range values {
		var text string
		switch x := item.(type) {
		case string:
			text = x
		case jsonx.Number:
			if !x.IsInt() {
				continue
			}
			text = x.String()
		default:
			continue
		}
		out = append(out, splitSpaceComma(pystr.Strip(text))...)
	}
	return out
}

// constraintUint is the plugin module's _uint(raw, maximum).
func constraintUint(raw any, maximum *big.Int) *big.Int { return boundedUint(raw, maximum, false) }

// durationSeconds is _duration(value): the seconds as a float or int (nil
// = None). '5ſ' and other units only re.I matches make Python fail with a
// TypeError, raised here too.
func durationSeconds(value any) (seconds *big.Float, ok bool) {
	switch x := value.(type) {
	case jsonx.Number:
		if !x.IsInt() {
			return nil, false
		}
		b := bigOf(x)
		if b.Sign() < 0 || b.Cmp(bigU64) > 0 {
			return nil, false
		}
		return new(big.Float).SetInt(b), true
	case string:
		text := []rune(pystr.Strip(x))
		i := 0
		for i < len(text) && isDecimalRune(text[i]) {
			i++
		}
		if i == 0 {
			return nil, false
		}
		digits := string(text[:i])
		j := i
		for j < len(text) && pystr.IsSpace(text[j]) {
			j++
		}
		unit := string(text[j:])
		folded := strings.ToLower(strings.ReplaceAll(unit, "ſ", "s"))
		switch folded {
		case "", "ms", "s", "m", "h", "d":
		default:
			return nil, false
		}
		for _, r := range unit {
			if r > 0x7f && r != 0x17f {
				return nil, false
			}
		}
		number := constraintUint(digits, bigU64)
		if number == nil {
			return nil, false
		}
		key := strings.ToLower(unit)
		if key == "" {
			key = "s"
		}
		var factor int64
		switch key {
		case "ms":
			f, _ := new(big.Float).SetInt(number).Float64()
			return big.NewFloat(f * 0.001), true
		case "s":
			factor = 1
		case "m":
			factor = 60
		case "h":
			factor = 3600
		case "d":
			factor = 86400
		default:
			raise(pyErr("TypeError", "unsupported operand type(s) for *: 'int' and 'NoneType'"))
		}
		return new(big.Float).SetInt(new(big.Int).Mul(number, big.NewInt(factor))), true
	}
	return nil, false
}

var (
	backrefOrLookaround = regexp.MustCompile(`\(\?[=!]|\(\?<[=!]`)
	httpHeaderName      = httpToken
)

// regexError is _regex_error(raw).
func regexError(raw string) string {
	for i := 0; i+1 < len(raw); i++ {
		if raw[i] == '\\' && (i == 0 || raw[i-1] != '\\') && raw[i+1] >= '1' && raw[i+1] <= '9' {
			return "OxiDNS 的正则不支持反向引用或前后查找"
		}
	}
	if backrefOrLookaround.MatchString(raw) {
		return "OxiDNS 的正则不支持反向引用或前后查找"
	}
	if strings.Contains(raw, `\p`) || strings.Contains(raw, `\P`) || strings.Contains(raw, "&&") ||
		strings.Contains(raw, "--") || strings.Contains(raw, "~~") {
		return ""
	}
	if regexCompileError(raw) {
		return "正则表达式语法无效"
	}
	return ""
}

// domainError is _domain_error(raw, dynamic).
func domainError(value any, dynamic bool) string {
	raw, ok := value.(string)
	if !ok || deferredText(raw) {
		return ""
	}
	raw = pystr.Strip(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		if dynamic {
			return "域名规则不能为空"
		}
		return ""
	}
	if strings.HasPrefix(raw, "regexp:") {
		pattern := raw[7:]
		if pystr.Strip(pattern) == "" {
			return "正则表达式不能为空"
		}
		return regexError(pattern)
	}
	prefixed := strings.HasPrefix(raw, "full:") || strings.HasPrefix(raw, "domain:") || strings.HasPrefix(raw, "keyword:")
	if dynamic || prefixed {
		rest := raw
		if prefixed {
			rest = raw[strings.IndexByte(raw, ':')+1:]
		}
		if rstripChars(pystr.Strip(rest), ".") == "" {
			return "域名规则不能为空"
		}
	}
	return ""
}

var (
	cronMonths   = []string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"}
	cronWeekdays = []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}
	cronDayLast  = regexp.MustCompile(`^(?:[0-7](?:L|#[1-5]))$`)
)

// cronDaySpecial is re.fullmatch(r'L(?:-\d+)?|LW|\d+W', segment).
func cronDaySpecial(segment string) bool {
	switch {
	case segment == "L" || segment == "LW":
		return true
	case strings.HasPrefix(segment, "L-"):
		return decimalDigits(segment[2:])
	case strings.HasSuffix(segment, "W"):
		return decimalDigits(segment[:len(segment)-1])
	}
	return false
}

// findDecimalRuns is re.findall(r'\d+', s).
func findDecimalRuns(s string) []string {
	var out []string
	var current []rune
	for _, r := range s {
		if isDecimalRune(r) {
			current = append(current, r)
			continue
		}
		if len(current) > 0 {
			out = append(out, string(current))
			current = nil
		}
	}
	if len(current) > 0 {
		out = append(out, string(current))
	}
	return out
}

// cronError is _cron_error(raw).
func cronError(value any) string {
	raw, ok := value.(string)
	if !ok || deferredText(raw) {
		return ""
	}
	parts := pySplit(raw)
	if len(parts) != 5 {
		return "必须使用 5 段 Cron 表达式（分 时 日 月 星期）"
	}
	lows := []int64{0, 0, 1, 1, 0}
	highs := []int64{59, 23, 31, 12, 7}
	for index, field := range parts {
		lo, hi := big.NewInt(lows[index]), big.NewInt(highs[index])
		field = pystr.Upper(field)
		switch index {
		case 3:
			for i, name := range cronMonths {
				field = subWord(field, name, itoa(i+1))
			}
		case 4:
			for i, name := range cronWeekdays {
				field = subWord(field, name, itoa(i))
			}
		}
		for _, segment := range strings.Split(field, ",") {
			if segment == "" {
				return "Cron 字段中有空条目"
			}
			if index == 2 && cronDaySpecial(segment) {
				minimum := big.NewInt(1)
				if strings.HasPrefix(segment, "L-") {
					minimum = big.NewInt(0)
				}
				for _, n := range findDecimalRuns(segment) {
					value := constraintUint(n, big.NewInt(31))
					if value == nil || value.Cmp(minimum) < 0 {
						return "Cron 月日字段超出 1–31"
					}
				}
				continue
			}
			if index == 4 && cronDayLast.MatchString(segment) {
				continue
			}
			fragments := strings.Split(segment, "/")
			if len(fragments) > 2 {
				return "Cron 步长必须是字段范围内的正整数"
			}
			if len(fragments) == 2 {
				if step := constraintUint(fragments[1], bigU64); step == nil || step.Sign() == 0 {
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
				value := constraintUint(x, hi)
				if value == nil || value.Cmp(lo) < 0 {
					return "Cron 字段超出允许范围"
				}
			}
			if len(bounds) == 2 && constraintUint(bounds[0], hi).Cmp(constraintUint(bounds[1], hi)) > 0 {
				return "Cron 范围起点不能大于终点"
			}
		}
	}
	return ""
}

// pyEqZero is `value == 0` for loaded values (False == 0 in Python).
func pyEqZero(v any) bool {
	switch x := v.(type) {
	case bool:
		return !x
	case jsonx.Number:
		if x.IsInt() {
			return bigOf(x).Sign() == 0
		}
		return x.Float64() == 0
	}
	return false
}

// numberGreater is a > b for _number values.
func numberGreater(a, b any) bool {
	return compareNumbers(a.(jsonx.Number), b.(jsonx.Number)) > 0
}

// posixJoin is os.path.join(a, b).
func posixJoin(a, b string) string {
	if strings.HasPrefix(b, "/") {
		return b
	}
	if a == "" || strings.HasSuffix(a, "/") {
		return a + b
	}
	return a + "/" + b
}

// networkInfo is ipaddress.ip_network(text, strict=False): its version and
// whether the prefix length is 0 (ok=false is the ValueError).
func networkInfo(text string) (version int, zeroPrefix bool, ok bool) {
	if !convert.IsIPNetwork(text) {
		return 0, false, false
	}
	address, mask, hasMask := strings.Cut(text, "/")
	parsed, _ := convert.ParseIPAddress(address)
	version = 6
	if parsed.Is4() {
		version = 4
	}
	if hasMask {
		if asciiDigits(mask) && strings.Trim(mask, "0") == "" {
			zeroPrefix = true
		} else if version == 4 && mask == "0.0.0.0" {
			zeroPrefix = true
		}
	}
	return version, zeroPrefix, true
}

var timeOfDay = func(s string) bool {
	r := []rune(s)
	if len(r) != 5 || r[2] != ':' {
		return false
	}
	if !isDecimalRune(r[1]) || !isDecimalRune(r[4]) {
		return false
	}
	if !(r[0] == '0' || r[0] == '1' || (r[0] == '2' && r[1] >= '0' && r[1] <= '3')) {
		return false
	}
	return r[3] >= '0' && r[3] <= '5'
}

// CollectPluginIssues is collect_plugin_issues(plugin, index, document):
// non-mutating {path, message, code, severity} issues of one plugin.
func CollectPluginIssues(plugin any, index any, document any) (issues []any, err error) {
	defer catch(&err)
	return collectPluginIssues(plugin, index), nil
}

func collectPluginIssues(plugin any, index any) []any {
	if !isDict(plugin) {
		return []any{}
	}
	kind := get(plugin, "type", nil)
	args := get(plugin, "args", nil)
	issues := []any{}
	prefix := []any{"plugins", index, "args"}
	add := func(path []any, message, code, severity string) {
		issues = append(issues, jsonx.Obj("path", appendPath(prefix, path...), "message", message, "code", code, "severity", severity))
	}
	constraint := func(path []any, message, code string) { add(path, message, code, "error") }
	domain := func(raw any, path []any, dynamic bool) {
		if message := domainError(raw, dynamic); message != "" {
			constraint(path, message, "domain-rule")
		}
	}
	ip := func(raw any, path []any, network bool) {
		text, ok := raw.(string)
		if !ok || deferredText(text) {
			return
		}
		if network {
			if !convert.IsIPNetwork(text) {
				constraint(path, "需要有效的 IP 或 CIDR", "ip")
			}
		} else if _, valid := convert.ParseIPAddress(text); !valid {
			constraint(path, "需要有效的 IP 地址", "ip")
		}
	}
	nullableDefault := func(name string, def any, mapping any) any {
		if mapping == nil {
			mapping = args
		}
		value := get(mapping, name, nil)
		if value == nil {
			return def
		}
		return value
	}
	compare := func(lower, upper string, lowDefault, highDefault any) {
		low, high := nullableDefault(lower, lowDefault, nil), nullableDefault(upper, highDefault, nil)
		if isNumber(low) && isNumber(high) && numberGreater(low, high) {
			constraint([]any{lower}, lower+" 不能大于 "+upper, "range-order")
		}
	}
	template := func(raw any, path []any) {
		text, ok := raw.(string)
		if !ok {
			return
		}
		stripped := strings.ReplaceAll(text, "$$", "")
		runes := []rune(stripped)
		matches := findTemplates(stripped)
		for _, m := range matches {
			if pystr.Strip(string(runes[m[0]+2:m[1]-1])) == "" {
				constraint(path, "占位符名称不能为空", "template")
			}
		}
		var rest strings.Builder
		last := 0
		for _, m := range matches {
			rest.WriteString(string(runes[last:m[0]]))
			last = m[1]
		}
		rest.WriteString(string(runes[last:]))
		if strings.Contains(rest.String(), "${") {
			constraint(path, "占位符缺少右花括号", "template")
		}
	}

	if strIn(kind, "qname", "cname", "client_ip", "ptr_ip", "resp_ip", "question", "qtype", "qclass", "rcode", "mark", "random", "reload_provider") {
		tokens := constraintTokens(args)
		if len(tokens) == 0 {
			message := "至少配置一个匹配值"
			if kind == "reload_provider" {
				message = "至少引用一个 Provider"
			}
			constraint(nil, message, "required")
		}
		for _, token := range tokens {
			if deferredText(token) {
				continue
			}
			reference := strings.HasPrefix(token, "$") || strings.HasPrefix(token, "&")
			if strIn(kind, "qname", "cname") && !reference {
				domain(token, nil, false)
			}
			if strIn(kind, "client_ip", "ptr_ip", "resp_ip") && !reference {
				ip(token, nil, true)
			}
			if strIn(kind, "question", "reload_provider") && (!strings.HasPrefix(token, "$") || pystr.Strip(token[1:]) == "") {
				constraint(nil, "这里只接受 $tag 形式的 Provider 引用", "provider-reference")
			}
			if strIn(kind, "qtype", "qclass", "rcode") && !dnsCodeValid(kind.(string), token) {
				constraint(nil, "DNS 类型、类别或响应码无效", "dns-code")
			}
			if kind == "mark" && constraintUint(token, bigU32) == nil {
				constraint(nil, "标记必须是 0–4294967295 的整数", "range")
			}
		}
		if kind == "random" {
			anyDeferred := false
			for _, t := range tokens {
				if deferredText(t) {
					anyDeferred = true
				}
			}
			if !anyDeferred {
				valid := false
				if len(tokens) == 1 {
					if f, err := pystr.Float(tokens[0]); err == nil {
						valid = !math.IsInf(f, 0) && !math.IsNaN(f) && f >= 0 && f <= 1
					}
				}
				if !valid {
					constraint(nil, "概率必须且只能填写一个 0–1 的数字字符串", "range")
				}
			}
		}
	}
	if kind == "env" {
		entries, isList := args.([]any)
		if !isList {
			entries = []any{args}
		}
		allEmpty := true
		for _, x := range entries {
			if strOf(x) != "" {
				allEmpty = false
			}
		}
		if len(entries) == 0 || allEmpty {
			constraint(nil, "至少配置一个环境变量条件", "required")
		}
		for j, entry := range entries {
			text, ok := entry.(string)
			if !ok {
				continue
			}
			name := text
			if k := strings.IndexAny(text, "=:"); k >= 0 {
				name = text[:k]
			}
			if pystr.Strip(text) == "" || pystr.Strip(name) == "" {
				var path []any
				if isList {
					path = []any{jsonx.Int(j)}
				}
				constraint(path, "环境变量名称不能为空", "required")
			}
		}
	}
	if kind == "any_match" {
		var entries []any
		switch x := args.(type) {
		case []any:
			entries = x
		case string:
			for _, line := range convert.PySplitlines(x) {
				entries = append(entries, line)
			}
		}
		found := false
		for _, x := range entries {
			if strOf(x) != "" {
				found = true
			}
		}
		if !found {
			constraint(nil, "至少配置一个匹配表达式", "required")
		}
	}
	if kind == "string_exp" {
		raw := args
		if list, ok := args.([]any); ok {
			var parts []string
			for _, x := range list {
				if s, ok := x.(string); ok {
					parts = append(parts, s)
				}
			}
			raw = strings.Join(parts, " ")
		}
		if text, ok := raw.(string); ok && !deferredText(text) {
			tokens := pySplit(text)
			source, op := "", ""
			var values []string
			if len(tokens) > 0 {
				matched := false
				for _, candidate := range []string{"contains", "prefix", "suffix", "regexp", "eq", "zl"} {
					if strings.HasSuffix(tokens[0], candidate) && runeLen(tokens[0]) > len(candidate) {
						source, op, values = tokens[0][:len(tokens[0])-len(candidate)], candidate, tokens[1:]
						matched = true
						break
					}
				}
				if !matched && len(tokens) >= 2 {
					source, op, values = tokens[0], tokens[1], tokens[2:]
				}
			}
			if !strIn(source, "qname", "qtype", "qclass", "rcode", "resp_ip", "mark", "client_ip", "server_name", "url_path") &&
				!(strings.HasPrefix(source, "$") && runeLen(source) > 1) {
				constraint(nil, "字符串表达式的来源字段无效", "expression")
			}
			if !strIn(op, "eq", "prefix", "suffix", "contains", "regexp", "zl") {
				constraint(nil, "字符串表达式的操作无效", "expression")
			} else if (op == "zl" && len(values) > 0) || (op != "zl" && len(values) == 0) {
				constraint(nil, "zl 不接受参数；其他操作至少需要一个参数", "expression")
			}
			if op == "regexp" {
				for _, value := range values {
					if message := regexError(value); message != "" {
						constraint(nil, message, "regexp")
					}
				}
			}
		}
	}
	if list, ok := args.([]any); ok && kind == "client_ip_from_ecs" {
		for j, raw := range list {
			if strOf(raw) != "" {
				ip(strOf(raw), []any{jsonx.Int(j)}, true)
			}
		}
	}
	if kind == "ttl" {
		if text, ok := args.(string); ok && !deferredText(text) {
			tokens := strings.Split(text, "-")
			bad := len(tokens) > 2
			for _, x := range tokens {
				if constraintUint(pystr.Strip(x), bigU32) == nil {
					bad = true
				}
			}
			if bad {
				constraint(nil, "TTL 简写需为非负整数或 min-max，数值不得大于 4294967295", "ttl")
			}
		} else if isDict(args) {
			if get(args, "fix", nil) == nil && get(args, "min", nil) == nil && get(args, "max", nil) == nil {
				constraint(nil, "TTL 至少需要 fix、min 或 max 中的一个非空值", "required")
			}
		}
	}
	if text, ok := args.(string); ok && kind == "forward_edns0opt" && !deferredText(text) {
		for _, token := range constraintTokens(text) {
			if constraintUint(token, big.NewInt(65535)) == nil {
				constraint(nil, "EDNS0 代码必须为 0–65535 的整数", "range")
				break
			}
		}
	}
	if kind == "black_hole" {
		modes := []string{"nxdomain", "nodata", "null", "custom", "refused"}
		if isDict(args) {
			rawMode := get(args, "mode", nil)
			var mode any = rawMode
			if s, ok := rawMode.(string); ok {
				mode = pystr.Lower(pystr.Strip(s))
			}
			ips := get(args, "ips", nil)
			if has(args, "mode") && rawMode == nil {
				add([]any{"mode"}, `mode 为空时 OxiDNS 会按 nxdomain 处理；黑洞请写 "null"（带引号）`, "null-mode", "warning")
			}
			if s, ok := mode.(string); ok && !strIn(s, modes...) && !deferredText(s) {
				constraint([]any{"mode"}, "拦截模式无效", "enum")
			}
			if list, ok := ips.([]any); ok {
				var kept []any
				for j, raw := range list {
					if strOf(raw) != "" {
						ip(raw, []any{"ips", jsonx.Int(j)}, false)
						kept = append(kept, raw)
					}
				}
				ips = kept
				if kept == nil {
					ips = []any{}
				}
			}
			ipsList, ipsIsList := ips.([]any)
			if mode == "custom" && (ipsIsList || ips == nil) && len(ipsList) == 0 {
				constraint([]any{"ips"}, "custom 模式至少需要一个 IP", "required")
			} else if mode != nil && mode != "custom" && ipsIsList && len(ipsList) > 0 {
				constraint([]any{"ips"}, "只有 custom 模式可以配置 ips", "mutually-exclusive")
			}
		} else if _, isText := args.(string); isText || isList(args) {
			tokens := constraintTokens(args)
			mode := ""
			if isText && len(tokens) > 0 && strings.HasPrefix(tokens[len(tokens)-1], "short_circuit=") {
				last := tokens[len(tokens)-1]
				tokens = tokens[:len(tokens)-1]
				value := last[strings.IndexByte(last, '=')+1:]
				if value != "true" && value != "false" {
					constraint(nil, "short_circuit 必须为 true 或 false", "boolean")
				}
			}
			if isText && len(tokens) > 0 && strIn(pystr.Lower(tokens[0]), modes...) {
				mode = pystr.Lower(tokens[0])
				tokens = tokens[1:]
			}
			if mode == "custom" && len(tokens) == 0 {
				constraint(nil, "custom 模式至少需要一个 IP", "required")
			}
			if mode != "" && mode != "custom" && len(tokens) > 0 {
				constraint(nil, "只有 custom 模式可以附加 IP", "mutually-exclusive")
			}
			for _, token := range tokens {
				ip(token, nil, false)
			}
		}
	}
	if !isDict(args) || kind == "sequence" {
		return issues
	}
	if kind == "cache" {
		if get(args, "dump_file", nil) != nil && pyEqZero(get(args, "dump_interval", nil)) {
			constraint([]any{"dump_interval"}, "配置持久化文件时，保存间隔必须大于 0", "range")
		}
		compare("min_positive_ttl", "max_positive_ttl", nil, nil)
	}
	if strIn(kind, "prefer_ipv4", "prefer_ipv6") && nullableDefault("cache", true, nil) == true && pyEqZero(get(args, "cache_ttl", nil)) {
		constraint([]any{"cache_ttl"}, "启用探测缓存时 TTL 必须大于 0", "range")
	}
	if kind == "ip_selector" {
		methods := get(args, "probe_methods", nil)
		if _, isText := methods.(string); methods != nil && (isText || isList(methods)) {
			rawMethods, ok := methods.([]any)
			if !ok {
				rawMethods = []any{methods}
			}
			var tokens []string
			for _, raw := range rawMethods {
				s, ok := raw.(string)
				if !ok {
					continue
				}
				for _, piece := range strings.Split(s, ",") {
					if p := pystr.Strip(piece); p != "" {
						tokens = append(tokens, p)
					}
				}
			}
			if len(tokens) == 0 {
				constraint([]any{"probe_methods"}, "至少配置一种探测方法", "required")
			}
			distinct := map[string]bool{}
			hasNone := false
			for _, token := range tokens {
				distinct[token] = true
				if token == "none" {
					hasNone = true
				}
				if deferredText(token) {
					continue
				}
				valid := token == "ping" || token == "none"
				if !valid && strings.HasPrefix(token, "tcp:") && decimalDigits(token[4:]) {
					if port := constraintUint(token[4:], big.NewInt(65535)); port != nil && port.Sign() != 0 {
						valid = true
					}
				}
				if !valid {
					constraint([]any{"probe_methods"}, "探测方法应为 ping、none 或 tcp:1–65535", "probe-method")
				}
			}
			if hasNone && len(distinct) > 1 {
				constraint([]any{"probe_methods"}, "none 不能与其他探测方法同时使用", "mutually-exclusive")
			}
		}
		cache := get(args, "cache", nil)
		if isDict(cache) && nullableDefault("enabled", true, cache) == true {
			for _, key := range []string{"size", "ttl", "failure_ttl"} {
				if pyEqZero(get(cache, key, nil)) {
					constraint([]any{"cache", key}, "启用缓存时此值必须大于 0", "range")
				}
			}
		}
	}
	if kind == "ecs_handler" && strOf(get(args, "preset", nil)) != "" {
		ip(item(args, "preset"), []any{"preset"}, false)
	}
	if strIn(kind, "hosts", "redirect") {
		key := "entries"
		if kind == "redirect" {
			key = "rules"
		}
		if entries, ok := get(args, key, []any{}).([]any); ok {
			for j, entry := range entries {
				text, ok := entry.(string)
				if !ok || deferredText(text) || pystr.Strip(text) == "" || strings.HasPrefix(lstrip(text), "#") {
					continue
				}
				path := []any{key, jsonx.Int(j)}
				tokens := pySplit(text)
				if (kind == "hosts" && len(tokens) < 2) || (kind == "redirect" && len(tokens) != 2) {
					message := "需填写域名规则及至少一个 IP"
					if kind == "redirect" {
						message = "重定向规则需恰好填写域名规则和目标域名"
					}
					constraint(path, message, "rule")
					continue
				}
				if strings.HasPrefix(tokens[0], "regexp:") {
					if message := regexError(tokens[0][7:]); message != "" {
						constraint(path, message, "regexp")
					}
				}
				if kind == "hosts" {
					for _, token := range tokens[1:] {
						ip(token, path, false)
					}
				} else {
					target := rstripChars(tokens[1], ".")
					bad := len(target) > 253
					for _, part := range strings.Split(target, ".") {
						if part == "" || len(part) > 63 {
							bad = true
						}
					}
					if bad {
						constraint(path, "重定向的目标域名无效", "domain")
					}
				}
			}
		}
	}
	if strIn(kind, "domain_set", "dynamic_domain_set") {
		key := "exps"
		if kind == "dynamic_domain_set" {
			key = "bootstrap_rules"
		}
		if entries, ok := get(args, key, []any{}).([]any); ok {
			for j, entry := range entries {
				text, ok := entry.(string)
				if !ok {
					continue
				}
				path := []any{key, jsonx.Int(j)}
				if kind == "dynamic_domain_set" {
					domain(text, path, true)
				} else {
					for _, line := range convert.PySplitlines(text) {
						domain(line, path, false)
					}
				}
			}
		}
	}
	if rulesList, ok := get(args, "rules", nil).([]any); ok && kind == "arbitrary" {
		for j, raw := range rulesList {
			path := []any{"rules", jsonx.Int(j)}
			if message := zoneRecordsError(raw, false); message != "" {
				constraint(path, message, "dns-record")
			}
			if text, ok := raw.(string); ok && directiveLine(text) {
				add(path, "外部 zone 文件和生成记录需要目标核心在初始化时检查", "runtime-zone", "warning")
			}
		}
	}
	if kind == "response" {
		for _, key := range []string{"answers", "authorities", "additionals"} {
			if records, ok := get(args, key, nil).([]any); ok {
				for j, raw := range records {
					if text, ok := raw.(string); ok && directiveLine(text) {
						add([]any{key, jsonx.Int(j)}, "外部 zone 文件和生成记录需要目标核心在初始化时检查", "runtime-zone", "warning")
					}
				}
			}
		}
	}
	if rulesList, ok := get(args, "rules", nil).([]any); ok && kind == "adguard_rule" {
		for j, raw := range rulesList {
			text, ok := raw.(string)
			if !ok {
				continue
			}
			for _, line := range convert.PySplitlines(text) {
				if message, severity := adguardError(line); message != "" {
					add([]any{"rules", jsonx.Int(j)}, message, "adguard-rule", severity)
				}
			}
		}
	}
	if selectors, ok := get(args, "selectors", nil).([]any); ok && kind == "geosite" {
		for j, selector := range selectors {
			text, ok := selector.(string)
			if !ok || !strings.Contains(text, "@") || deferredText(text) {
				continue
			}
			code, attr, _ := strings.Cut(pystr.Strip(text), "@")
			if pystr.Strip(code) == "" || pystr.Strip(attr) == "" {
				constraint([]any{"selectors", jsonx.Int(j)}, "分类名称和 @ 后的属性名不能为空", "selector")
			}
		}
	}
	if ips, ok := get(args, "ips", nil).([]any); ok && kind == "ip_set" {
		for j, entry := range ips {
			text, ok := entry.(string)
			if !ok {
				continue
			}
			for _, line := range convert.PySplitlines(text) {
				token, _, _ := strings.Cut(line, "#")
				if token = pystr.Strip(token); token != "" {
					ip(token, []any{"ips", jsonx.Int(j)}, true)
				}
			}
		}
	}
	if kind == "nftset" {
		for _, family := range []struct {
			suffix string
			limit  int64
		}{{"4", 32}, {"6", 128}} {
			s := family.suffix
			if get(args, "ipv"+s, nil) != nil || get(args, "table_family"+s, nil) == nil ||
				get(args, "table_name"+s, nil) == nil || get(args, "set_name"+s, nil) == nil {
				continue
			}
			if name, ok := get(args, "table_family"+s, nil).(string); ok && !strIn(name, "ip", "ip6", "inet") {
				constraint([]any{"table_family" + s}, "地址族必须为 ip、ip6 或 inet", "enum")
			}
			for _, key := range []string{"table_name", "set_name"} {
				if text, ok := get(args, key+s, nil).(string); ok && pystr.Strip(text) == "" {
					constraint([]any{key + s}, "名称不能为空", "required")
				}
			}
			if mask := get(args, "mask"+s, nil); isNumber(mask) && compareNumbers(mask.(jsonx.Number), jsonx.Int(int(family.limit))) > 0 {
				constraint([]any{"mask" + s}, "前缀长度不能超过 "+itoa(int(family.limit)), "range")
			}
		}
	}
	if strIn(kind, "ros_route", "ros_address_list") {
		compare("min_ttl", "max_ttl", jsonx.Int(60), jsonx.Int(3600))
		families := []string{"gateway4", "gateway6"}
		if kind == "ros_address_list" {
			families = []string{"address_list4", "address_list6"}
		}
		if strOf(get(args, families[0], nil)) == "" && strOf(get(args, families[1], nil)) == "" {
			constraint([]any{families[0]}, "至少配置 "+families[0]+" 或 "+families[1], "required")
		}
		if prefix, ok := get(args, "comment_prefix", nil).(string); ok && strings.ContainsAny(prefix, ";=") {
			constraint([]any{"comment_prefix"}, "注释前缀不能包含 ; 或 =", "delimiter")
		}
		if tls := get(args, "tls", nil); isDict(tls) && get(tls, "insecure", nil) == true && strOf(get(tls, "ca", nil)) != "" {
			constraint([]any{"tls", "ca"}, "跳过 TLS 校验时不能同时指定 CA", "mutually-exclusive")
		}
		if seconds, ok := durationSeconds(get(args, "wait_timeout", nil)); ok && seconds.Sign() == 0 {
			constraint([]any{"wait_timeout"}, "等待超时必须大于 0", "range")
		}
		if persistent := get(args, "persistent", nil); isDict(persistent) {
			if ips, ok := get(persistent, "ips", nil).([]any); ok {
				for j, raw := range ips {
					text, ok := raw.(string)
					if !ok || deferredText(text) {
						continue
					}
					path := []any{"persistent", "ips", jsonx.Int(j)}
					for _, line := range convert.PySplitlines(text) {
						line, _, _ = strings.Cut(line, "#")
						line = pystr.Strip(line)
						if line == "" {
							continue
						}
						version, zero, valid := networkInfo(line)
						if !valid {
							constraint(path, "常驻地址需为有效 IP 或 CIDR", "ip")
							continue
						}
						target := families[0]
						if version != 4 {
							target = families[1]
						}
						if strOf(get(args, target, nil)) == "" {
							add(path, "未设置该地址族的目标，OxiDNS 会忽略此常驻地址", "ignored-family", "warning")
						}
						if kind == "ros_route" && zero {
							add(path, "OxiDNS 会忽略 /0 默认路由", "ignored-default-route", "warning")
						}
					}
				}
			}
		}
	}
	if strIn(kind, "http_request", "learn_domain", "upgrade") {
		timeout := get(args, "timeout", nil)
		if strOf(timeout) != "" && !deferredText(timeout) {
			if _, ok := durationSeconds(timeout); !ok {
				constraint([]any{"timeout"}, "超时需为非负整数秒或带 ms/s/m/h/d 单位的时长", "duration")
			}
		}
	}
	if kind == "http_request" {
		count := 0
		if get(args, "body", nil) != nil {
			count++
		}
		if get(args, "json", nil) != nil {
			count++
		}
		if truthy(get(args, "form", nil)) {
			count++
		}
		if count > 1 {
			constraint([]any{"body"}, "body、json 和非空 form 不能同时配置", "mutually-exclusive")
		}
		if get(args, "content_type", nil) != nil && (get(args, "json", nil) != nil || truthy(get(args, "form", nil))) {
			constraint([]any{"content_type"}, "content_type 仅用于原始 body", "dependency")
		}
		for _, key := range []string{"url", "body", "content_type"} {
			if value, ok := get(args, key, nil).(string); ok {
				template(value, []any{key})
				if key == "content_type" && strings.ContainsAny(value, "\r\n") {
					constraint([]any{key}, "HTTP 头值不能包含换行", "http-header")
				}
			}
		}
		if url, ok := get(args, "url", nil).(string); ok && !deferredText(url) {
			valid := false
			if parsed, err := urlx.Split(url); err == nil {
				valid = strIn(pystr.Lower(parsed.Scheme), "http", "https") && parsed.Hostname() != ""
			}
			if !valid {
				constraint([]any{"url"}, "需要有效的 HTTP / HTTPS URL", "url")
			}
		}
		for _, key := range []string{"headers", "query_params", "form"} {
			mapping, ok := get(args, key, nil).(*jsonx.Object)
			if !ok || mapping == nil {
				continue
			}
			for name, value := range mapping.All() {
				path := []any{key, name}
				if key != "query_params" && pystr.Strip(name) == "" {
					constraint(path, "名称不能为空", "required")
				}
				if key == "headers" && !httpHeaderName.MatchString(pystr.Strip(name)) {
					constraint(path, "HTTP 请求头名称无效", "http-header")
				}
				if text, ok := value.(string); ok && pystr.Strip(text) == "" {
					constraint(path, "请求模板值不能为空", "required")
				}
				template(value, path)
			}
		}
	}
	if kind == "script" {
		if list, ok := get(args, "args", nil).([]any); ok {
			for j, value := range list {
				template(value, []any{"args", jsonx.Int(j)})
			}
		}
		if env, ok := get(args, "env", nil).(*jsonx.Object); ok && env != nil {
			for key, value := range env.All() {
				if pystr.Strip(key) == "" {
					constraint([]any{"env", key}, "环境变量名称不能为空", "required")
				}
				template(value, []any{"env", key})
			}
		}
	}
	if downloads, ok := get(args, "downloads", nil).([]any); ok && kind == "download" {
		seen := map[string]bool{}
		for j, entry := range downloads {
			if !isDict(entry) || deferredText(get(entry, "url", nil)) || deferredText(get(entry, "dir", nil)) {
				continue
			}
			filename := strOf(get(entry, "filename", nil))
			if filename == "" {
				parsed, err := urlx.Split(strOf(get(entry, "url", nil)))
				if err != nil {
					continue
				}
				p := parsed.Path
				filename = pystr.Strip(p[strings.LastIndexByte(p, '/')+1:])
			}
			if filename == "" {
				constraint([]any{"downloads", jsonx.Int(j), "filename"}, "URL 无法推导文件名，请填写 filename", "required")
			}
			target := posixJoin(strOf(get(entry, "dir", nil)), filename)
			if seen[target] {
				constraint([]any{"downloads", jsonx.Int(j)}, "下载目标文件重复", "duplicate")
			}
			seen[target] = true
		}
	}
	if kind == "cron" {
		timezone := get(args, "timezone", nil)
		if strOf(timezone) != "" && !deferredText(timezone) && !zoneInfoValid(strOf(timezone)) {
			constraint([]any{"timezone"}, "需要有效的 IANA 时区，例如 Asia/Shanghai", "timezone")
		}
	}
	if periods, ok := get(args, "periods", nil).([]any); ok && kind == "time" {
		weekdays := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
		for j, period := range periods {
			if !isDict(period) {
				continue
			}
			at := func(parts ...any) []any { return append([]any{"periods", jsonx.Int(j)}, parts...) }
			start, end := get(period, "start", nil), get(period, "end", nil)
			if (start == nil) != (end == nil) {
				constraint(at(), "start 和 end 必须同时填写或同时省略", "dependency")
			}
			for _, pair := range []struct {
				key   string
				value any
			}{{"start", start}, {"end", end}} {
				if text, ok := pair.value.(string); ok && !deferredText(text) && !timeOfDay(pystr.Strip(text)) {
					constraint(at(pair.key), "时间需为 00:00–23:59 的 HH:MM", "time")
				}
			}
			startText, ok1 := start.(string)
			endText, ok2 := end.(string)
			if ok1 && ok2 && !deferredText(startText) && pystr.Strip(startText) == pystr.Strip(endText) {
				constraint(at("end"), "开始和结束时间不能相同", "range")
			}
			if start == nil && !truthy(get(period, "weekdays", nil)) && !truthy(get(period, "monthdays", nil)) {
				constraint(at(), "省略时间段时至少设置星期或月日", "required")
			}
			if days, ok := get(period, "weekdays", nil).([]any); ok {
				for k, day := range days {
					if text, ok := day.(string); ok && !deferredText(text) && !strIn(pystr.Lower(pystr.Strip(text)), weekdays...) {
						constraint(at("weekdays", jsonx.Int(k)), "星期为 mon–sun 或数字 1–7；数字字符串不支持", "weekday")
					}
				}
			}
		}
	}
	if jobs, ok := get(args, "jobs", nil).([]any); ok && kind == "cron" {
		names := map[string]bool{}
		for j, job := range jobs {
			if !isDict(job) {
				continue
			}
			at := func(parts ...any) []any { return append([]any{"jobs", jsonx.Int(j)}, parts...) }
			name := strOf(get(job, "name", nil))
			if names[name] {
				constraint(at("name"), "任务名称重复", "duplicate")
			}
			names[name] = true
			schedule, interval := get(job, "schedule", nil), get(job, "interval", nil)
			if (strOf(schedule) != "") == (strOf(interval) != "") {
				constraint(at(), "schedule 和 interval 必须且只能填写一个", "mutually-exclusive")
			}
			if strOf(schedule) != "" {
				if message := cronError(schedule); message != "" {
					constraint(at("schedule"), message, "cron")
				}
			}
			if strOf(interval) != "" && !deferredText(interval) {
				seconds, ok := durationSeconds(interval)
				if !ok || seconds.Cmp(big.NewFloat(60)) < 0 {
					constraint(at("interval"), "定时间隔至少为 1 分钟", "range")
				}
			}
		}
	}
	return issues
}

func isList(v any) bool {
	_, ok := v.([]any)
	return ok
}

// ValidatePluginFormat is validate_plugin_format(name, value): nil when
// the value has the format.
func ValidatePluginFormat(name any, value any) (message *string, err error) {
	defer catch(&err)
	if text := validatePluginFormat(name, value); text != "" {
		return &text, nil
	}
	return nil, nil
}

func validatePluginFormat(name any, value any) string {
	switch name {
	case "dns-record":
		return zoneRecordsError(value, true)
	case "domain-expression":
		return domainError(value, false)
	}
	raise(pyErr("ValueError", "Unsupported plugin validation format: "+str(name)))
	return ""
}

// adguardError is _adguard_error(raw): (message, severity).
func adguardError(raw string) (string, string) {
	raw = pystr.Strip(raw)
	if raw == "" || strings.HasPrefix(raw, "!") || strings.HasPrefix(raw, "#") || deferredText(raw) {
		return "", "warning"
	}
	for _, marker := range []string{"##", "#@#", "#$#", "#@$#", "#%#", "#@%#", "#?#", "#@?#"} {
		if strings.Contains(raw, marker) {
			return "此为非 DNS 过滤规则，OxiDNS 会跳过", "warning"
		}
	}
	if words := pySplit(raw); len(words) > 1 {
		if _, ok := convert.ParseIPAddress(words[0]); ok {
			return "Hosts 格式规则会被 AdGuard Provider 跳过", "warning"
		}
	}
	if strings.HasPrefix(raw, "@@") {
		raw = pystr.Strip(raw[2:])
	}
	var pattern string
	var modifiers *string
	if strings.HasPrefix(raw, "/") {
		if strings.HasSuffix(raw, "/") && runeLen(raw) > 1 {
			pattern = raw
		} else if k := strings.LastIndex(raw, "/$"); k >= 0 {
			rest := raw[k+2:]
			pattern, modifiers = raw[:k]+"/", &rest
		} else {
			return "路径或 URL 过滤规则不用于 DNS，OxiDNS 会跳过", "warning"
		}
	} else {
		before, after, found := strings.Cut(raw, "$")
		pattern = before
		if found {
			modifiers = &after
		}
	}
	pattern = pystr.Strip(pattern)
	type modifier struct{ name, separator, value string }
	var parsed []modifier
	if modifiers != nil {
		for _, part := range strings.Split(*modifiers, ",") {
			if p := pystr.Strip(part); p != "" {
				name, value, found := strings.Cut(p, "=")
				separator := ""
				if found {
					separator = "="
				}
				parsed = append(parsed, modifier{name, separator, value})
			}
		}
	}
	supported := func(name string) bool {
		return strIn(pystr.Lower(name), "important", "badfilter", "denyallow", "dnstype")
	}
	if pattern == "" {
		anySupported := false
		for _, m := range parsed {
			if supported(m.name) {
				anySupported = true
			}
		}
		if modifiers == nil || anySupported {
			return "AdGuard 规则的匹配模式不能为空", "error"
		}
		return "规则仅含不支持的修饰符，OxiDNS 会跳过", "warning"
	}
	if !strings.HasPrefix(pattern, "/") && strings.ContainsFunc(pattern, func(r rune) bool {
		return r == '/' || r == '?' || r == ':' || r == '#' || pystr.IsSpace(r)
	}) {
		return "路径或 URL 过滤规则不用于 DNS，OxiDNS 会跳过", "warning"
	}
	for _, m := range parsed {
		if !supported(m.name) {
			return "规则含 OxiDNS 不支持的修饰符，整条规则会跳过", "warning"
		}
	}
	for _, m := range parsed {
		name := pystr.Lower(m.name)
		if strIn(name, "denyallow", "dnstype") && m.separator == "" {
			return "denyallow/dnstype 修饰符需要等号和值", "error"
		}
		if name == "denyallow" {
			for _, domain := range strings.Split(m.value, "|") {
				if pystr.Strip(domain) == "" {
					continue
				}
				if !denyallowDomain.MatchString(rstripChars(pystr.Strip(domain), ".")) {
					return "denyallow 中需要合法域名", "error"
				}
			}
		}
		if name == "dnstype" {
			for _, token := range strings.Split(m.value, "|") {
				token = strings.TrimLeft(pystr.Strip(token), "~")
				if token != "" && (pystr.IsDigit(token) || !dnsCodeValid("qtype", token)) {
					return "dnstype 中需要有效的 DNS 类型名称", "error"
				}
			}
		}
	}
	if strings.HasPrefix(pattern, "/") {
		runes := []rune(pattern)
		regex := ""
		if len(runes) > 2 {
			regex = string(runes[1 : len(runes)-1])
		}
		if pystr.Strip(regex) == "" {
			return "AdGuard 正则不能为空", "error"
		}
		if message := regexError(regex); message != "" {
			return message, "error"
		}
		return "", "warning"
	}
	normalized := pystr.Lower(rstripChars(pattern, "."))
	if normalized == "" {
		return "AdGuard 匹配域名不能为空", "error"
	}
	rest := normalized
	if strings.HasPrefix(normalized, "||") {
		rest = normalized[2:]
	} else if strings.HasPrefix(normalized, "|") {
		rest = normalized[1:]
	}
	rest = strings.TrimSuffix(rest, "|")
	if strings.Contains(rest, "|") {
		return "AdGuard 模式内部不能包含 |", "error"
	}
	return "", "warning"
}

var denyallowDomain = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
