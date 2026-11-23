package oxidns

import (
	"configflow/internal/textx"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Plugin references and quick-setup expressions, audited against the
// OxiDNS 1.6.1 factories. Unknown names are warnings; nothing is resolved.

var (
	domainProviders = []string{"domain_set", "dynamic_domain_set", "geosite", "adguard_rule"}
	ipProviders     = []string{"ip_set", "geoip"}
	ruleKinds       = []string{"qname", "cname", "client_ip", "resp_ip", "ptr_ip", "question", "reload_provider"}
	quickKinds      = strings.Fields(`_true _false has_resp has_wanted_ans qtype qclass rcode qname cname client_ip
resp_ip ptr_ip question mark random env string_exp time rate_limiter forward cache black_hole
ttl sleep ecs_handler forward_edns0opt client_ip_from_ecs ipset nftset ip_selector prefer_ipv4
prefer_ipv6 download upgrade reload reload_provider drop_resp query_summary debug_print metrics_collector`)
	ignoredQuick = []string{"drop_resp", "reload", "prefer_ipv4", "prefer_ipv6", "ecs_handler", "query_summary", "debug_print", "metrics_collector"}
)

// splitTokens is re.split(r'[\s,]+', s.strip()) without empty tokens.
func splitTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
}

// refTokens is _tokens of the reference module: strings of a value or list.
func refTokens(value *Node) []string {
	list := []*Node{value}
	if isSeq(value) {
		list = value.Content
	}
	var out []string
	for _, raw := range list {
		if isStr(raw) {
			out = append(out, splitTokens(raw.Value)...)
		}
	}
	return out
}

// splitFirst is s.split(None, 1) of a left-trimmed string.
func splitFirst(s string) (string, string, bool) {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, "", false
	}
	rest := strings.TrimLeftFunc(s[i:], unicode.IsSpace)
	return s[:i], rest, rest != ""
}

// edge is one parsed dependency: owner → target at path.
type edge struct {
	target string
	at     Path
}

// depGraph holds the dependency edges per tag in declaration order.
type depGraph struct {
	order []string
	edges map[string][]edge
}

func collectReferenceIssues(document *Node, graph *depGraph) []Issue {
	var issues []Issue
	plugins := get(mapOr(document), "plugins")
	if !has(mapOr(document), "plugins") || !isSeq(plugins) {
		return issues
	}
	_, catalog := schemas()
	issue := func(at Path, message, code, severity string) {
		issues = append(issues, Issue{Path: at, Message: message, Code: code, Severity: severity})
	}
	registry := map[string]*Node{}
	g := depGraph{edges: map[string][]edge{}}
	for index, plugin := range plugins.Content {
		tag, ok := tagOf(mapOr(plugin))
		if !ok {
			continue
		}
		if registry[tag] != nil {
			issue(P("plugins", index, "tag"), "插件 tag 重复，必须全局唯一", "duplicate-tag", "error")
		} else {
			registry[tag] = plugin
			g.order = append(g.order, tag)
			g.edges[tag] = []edge{}
		}
	}
	reference := func(owner string, raw *Node, at Path, kind, exact, capability string) {
		if !isStr(raw) || envRef(raw.Value) {
			return
		}
		tag := strings.TrimSpace(raw.Value)
		if tag == "" {
			issue(at, "插件引用不能为空", "reference", "error")
			return
		}
		target := registry[tag]
		if target == nil {
			issue(at, "引用的插件 tag 不存在", "reference", "error")
			return
		}
		targetType := get(target, "type")
		schema := catalog[str(targetType)]
		if !isStr(targetType) {
			schema = nil
		}
		if _, ok := g.edges[owner]; ok {
			g.edges[owner] = append(g.edges[owner], edge{tag, at})
		}
		switch {
		case schema == nil:
			issue(at, "目标是尚未识别的插件类型，由目标核心确认其能力", "unknown-reference", "warning")
		case schema.Kind != kind:
			issue(at, "引用目标必须为 "+kind+" 类型插件", "reference-kind", "error")
		case exact != "" && targetType.Value != exact:
			issue(at, "引用目标必须为 "+exact+" 插件", "reference-type", "error")
		case capability == "domain" && !containsString(domainProviders, targetType.Value):
			issue(at, "引用的 Provider 必须支持域名匹配", "reference-capability", "error")
		case capability == "ip" && !containsString(ipProviders, targetType.Value):
			issue(at, "引用的 Provider 必须支持 IP 匹配", "reference-capability", "error")
		}
	}
	providerRules := func(owner, kind string, args *Node, at Path) {
		for _, token := range refTokens(args) {
			if strings.HasPrefix(token, "$") && len(token) > 1 {
				capability := ""
				switch kind {
				case "qname", "cname":
					capability = "domain"
				case "client_ip", "resp_ip", "ptr_ip":
					capability = "ip"
				}
				reference(owner, S(token[1:]), at, "provider", "", capability)
			}
		}
	}
	converted := func(owner, kind string, args *Node, at Path) {
		if schema := catalog[kind]; schema != nil {
			for _, problem := range ValidateSchema(args, schema, at) {
				problem.Path = at
				issues = append(issues, problem)
			}
		}
		for _, problem := range collectPluginIssues(M("tag", owner, "type", kind, "args", args), 0) {
			problem.Path = at
			issues = append(issues, problem)
		}
		if containsString(ruleKinds, kind) {
			providerRules(owner, kind, args, at)
		}
	}
	shortFlags := func(tokens []string, at Path) ([]string, bool) {
		result := false
		for len(tokens) > 0 {
			last := tokens[len(tokens)-1]
			if last != "short_circuit" && !strings.HasPrefix(last, "short_circuit=") {
				break
			}
			tokens = tokens[:len(tokens)-1]
			switch value := strings.TrimPrefix(last, "short_circuit="); {
			case last == "short_circuit":
				result = true
			case value != "true" && value != "false":
				issue(at, "short_circuit 只能使用 true 或 false", "quick-setup", "error")
			default:
				result = value == "true"
			}
		}
		return tokens, result
	}
	quick := func(owner, kind, raw string, at Path, expected string) {
		schema := catalog[kind]
		if schema == nil {
			issue(at, "尚未识别的快捷表达式，请由目标核心确认", "unknown-expression", "warning")
			return
		}
		if schema.Kind != expected {
			issue(at, "此处需要 "+expected+" 表达式", "expression-kind", "error")
			return
		}
		if !containsString(quickKinds, kind) {
			issue(at, "该插件不支持快捷表达式，请定义插件后使用 $tag 引用", "quick-setup", "error")
			return
		}
		tokens := strings.Fields(raw)
		quickIssue := func(message string) { issue(at, message, "quick-setup", "error") }
		switch {
		case containsString(ignoredQuick, kind):
		case kind == "_true" || kind == "_false" || kind == "has_resp" || kind == "has_wanted_ans":
			if strings.TrimSpace(raw) != "" {
				quickIssue("此匹配器不接受参数")
			}
		case containsString(ruleKinds, kind) || containsString([]string{"qtype", "qclass", "rcode", "mark", "random", "string_exp", "ttl"}, kind):
			if strings.TrimSpace(raw) == "" {
				quickIssue("快捷表达式缺少必填参数")
				return
			}
			converted(owner, kind, S(raw), at)
		case kind == "env":
			converted(owner, kind, strs(tokens), at)
		case kind == "forward":
			tokens, _ = shortFlags(tokens, at)
			upstreams := L()
			for _, item := range tokens {
				upstreams.Content = append(upstreams.Content, M("addr", item))
			}
			converted(owner, kind, M("upstreams", upstreams), at)
		case kind == "cache":
			for _, item := range tokens {
				if item != "short_circuit" && item != "short_circuit=true" && item != "short_circuit=false" {
					quickIssue("cache 快捷参数仅支持 short_circuit、short_circuit=true/false")
					break
				}
			}
		case kind == "black_hole":
			tokens, short := shortFlags(tokens, at)
			var split []string
			for _, item := range tokens {
				split = append(split, splitTokens(item)...)
			}
			args := M()
			mode := ""
			if len(split) > 0 && containsString([]string{"nxdomain", "nodata", "null", "custom", "refused"}, strings.ToLower(split[0])) {
				mode, split = strings.ToLower(split[0]), split[1:]
			}
			set(args, "ips", strs(split))
			set(args, "short_circuit", B(short))
			if mode != "" {
				set(args, "mode", S(mode))
			}
			converted(owner, kind, args, at)
		case kind == "sleep":
			message := FormatError("duration", S(raw))
			if strings.TrimSpace(raw) == "" || message != "" {
				if message == "" {
					message = "sleep 必须填写时长"
				}
				quickIssue(message)
			}
		case kind == "forward_edns0opt":
			for _, token := range splitTokens(raw) {
				if !uintMax(token, 65535) {
					quickIssue("EDNS0 代码必须为 0–65535 的整数")
					break
				}
			}
		case kind == "client_ip_from_ecs":
			converted(owner, kind, strs(splitTokens(raw)), at)
		case kind == "time":
			if len(tokens) != 1 || !strings.Contains(tokens[0], "-") {
				quickIssue("time 快捷参数必须是单个 HH:MM-HH:MM 时间段")
				return
			}
			start, end, _ := strings.Cut(tokens[0], "-")
			converted(owner, kind, M("periods", L(M("start", start, "end", end))), at)
		case kind == "rate_limiter":
			args := M()
			for index, key := range []string{"qps", "burst", "mask4", "mask6"} {
				if len(tokens) <= index {
					break
				}
				if key == "qps" {
					f, err := strconv.ParseFloat(strings.ReplaceAll(tokens[index], "_", ""), 64)
					if err != nil && !errors.Is(err, strconv.ErrRange) {
						quickIssue("rate_limiter 快捷参数需为 qps burst mask4 mask6 数字")
						return
					}
					set(args, key, Float(f))
					continue
				}
				if !uintMax(tokens[index], 4294967295) {
					quickIssue("rate_limiter 快捷参数需为 qps burst mask4 mask6 数字")
					return
				}
				set(args, key, uintNode(tokens[index]))
			}
			converted(owner, kind, args, at)
		case kind == "ip_selector":
			args := M()
			if len(tokens) > 0 && containsString([]string{"first_success", "best_within_budget", "background"}, tokens[0]) {
				set(args, "selection_mode", S(tokens[0]))
				tokens = tokens[1:]
			}
			if len(tokens) > 0 {
				set(args, "probe_methods", strs(tokens))
			}
			converted(owner, kind, args, at)
		case kind == "ipset":
			args := M()
			for _, token := range tokens {
				parts := strings.Split(token, ",")
				if len(parts) != 3 || (parts[1] != "inet" && parts[1] != "inet6") || !uintMax(parts[2], 255) {
					quickIssue("ipset 快捷格式为 set,inet|inet6,mask")
					continue
				}
				suffix := map[string]string{"inet": "4", "inet6": "6"}[parts[1]]
				set(args, "set_name"+suffix, S(parts[0]))
				set(args, "mask"+suffix, uintNode(parts[2]))
			}
			converted(owner, kind, args, at)
		case kind == "nftset":
			args := M()
			for _, token := range tokens {
				parts := strings.Split(token, ",")
				if len(parts) != 5 || !containsString([]string{"ip", "ip6", "inet"}, parts[0]) || parts[1] == "" || parts[2] == "" || !uintMax(parts[4], 255) {
					quickIssue("nftset 快捷格式为 family,table,set,type,mask")
					continue
				}
				if parts[3] != "ipv4_addr" && parts[3] != "ipv6_addr" {
					issue(at, "未知 nftset 地址类型，核心会忽略此条目", "ignored-parameter", "warning")
					continue
				}
				key := map[string]string{"ipv4_addr": "ipv4", "ipv6_addr": "ipv6"}[parts[3]]
				set(args, key, M("table_family", parts[0], "table_name", parts[1], "set_name", parts[2], "mask", uintNode(parts[4])))
			}
			converted(owner, kind, args, at)
		case kind == "download":
			url, dir, ok := splitFirst(strings.TrimSpace(raw))
			if !ok || strings.TrimSpace(dir) == "" {
				quickIssue("download 快捷参数必须包含 URL 和目录")
				return
			}
			converted(owner, kind, M("downloads", L(M("url", url, "dir", strings.TrimSpace(dir)))), at)
		case kind == "upgrade":
			args := M()
			for _, token := range tokens {
				if token == "force" || token == "no_restart" {
					set(args, token, B(true))
					continue
				}
				key, value, sep := strings.Cut(token, "=")
				switch {
				case !sep || !containsString([]string{"force", "no_restart", "github_token", "bundle"}, key):
					quickIssue("upgrade 包含不支持的快捷参数")
				case key == "force" || key == "no_restart":
					if value != "true" && value != "false" {
						quickIssue("upgrade 开关必须为 true 或 false")
					} else {
						set(args, key, B(value == "true"))
					}
				case key == "bundle":
					set(args, key, S(strings.ToLower(value)))
				default:
					set(args, key, S(value))
				}
			}
			converted(owner, kind, args, at)
		}
	}
	expression := func(owner string, rawNode *Node, at Path, expected string, sequence, cron bool) {
		if !isStr(rawNode) || envRef(rawNode.Value) {
			return
		}
		raw := strings.TrimLeftFunc(rawNode.Value, unicode.IsSpace)
		if expected == "matcher" && strings.HasPrefix(raw, "!") {
			raw = strings.TrimLeftFunc(raw[1:], unicode.IsSpace)
		}
		if strings.TrimSpace(raw) == "" {
			issue(at, "表达式不能为空", "expression", "error")
			return
		}
		if strings.HasPrefix(raw, "$") {
			reference(owner, S(raw[1:]), at, expected, "", "")
			return
		}
		first, param, hasParam := splitFirst(raw)
		switch {
		case sequence && (first == "accept" || first == "return"):
			return
		case sequence && (first == "jump" || first == "goto"):
			if !hasParam || strings.HasPrefix(strings.TrimSpace(param), "$") {
				issue(at, "jump/goto 必须引用不带 $ 的 sequence tag", "expression", "error")
			} else {
				reference(owner, S(param), at, "executor", "sequence", "")
			}
			return
		case sequence && first == "reject":
			tokens := strings.Fields(param)
			if len(tokens) > 0 && (len(tokens) != 1 || !DNSCodeValid("base-rcode", S(tokens[0]))) {
				issue(at, "reject 只接受一个基础响应码 0–15 或对应名称", "expression", "error")
			}
			return
		case sequence && (first == "mark" || first == "set_mark"):
			tokens := splitTokens(param)
			bad := len(tokens) == 0
			for _, token := range tokens {
				bad = bad || !uintMax(token, 4294967295)
			}
			if bad {
				issue(at, "mark/set_mark 至少需要一个 0–4294967295 整数", "expression", "error")
			}
			return
		case cron && pluginKind(first) != "executor":
			reference(owner, S(raw), at, "executor", "", "")
			return
		}
		quick(owner, first, param, at, expected)
	}
	for index, plugin := range plugins.Content {
		if !isMap(plugin) {
			continue
		}
		owner, ownerOK := tagOf(plugin)
		kindNode, args := get(plugin, "type"), get(plugin, "args")
		if !ownerOK || !isStr(kindNode) {
			continue
		}
		kind, at, cfg := kindNode.Value, P("plugins", index, "args"), mapOr(args)
		switch kind {
		case "udp_server", "tcp_server", "quic_server":
			reference(owner, get(cfg, "entry"), at.with("entry"), "executor", "", "")
		case "http_server":
			for j, entry := range items(get(cfg, "entries")) {
				reference(owner, get(mapOr(entry), "exec"), at.with("entries", j, "exec"), "executor", "", "")
			}
		case "fallback":
			for _, key := range []string{"primary", "secondary"} {
				reference(owner, get(cfg, key), at.with(key), "executor", "", "")
			}
		case "prefer_ipv4", "prefer_ipv6":
			reference(owner, get(cfg, "probe_executor"), at.with("probe_executor"), "executor", "", "")
		case "learn_domain":
			reference(owner, get(cfg, "provider"), at.with("provider"), "provider", "dynamic_domain_set", "")
		case "domain_set", "ip_set":
			capability := map[string]string{"domain_set": "domain", "ip_set": "ip"}[kind]
			for j, value := range items(get(cfg, "sets")) {
				reference(owner, value, at.with("sets", j), "provider", "", capability)
			}
		}
		if containsString(ruleKinds, kind) {
			providerRules(owner, kind, args, at)
		}
		switch kind {
		case "sequence":
			for j, rule := range items(args) {
				rule = mapOr(rule)
				expression(owner, get(rule, "exec"), at.with(j, "exec"), "executor", true, false)
				matches := get(rule, "matches")
				if isStr(matches) {
					expression(owner, matches, at.with(j, "matches"), "matcher", false, false)
				}
				for k, match := range items(matches) {
					expression(owner, match, at.with(j, "matches", k), "matcher", false, false)
				}
			}
		case "any_match":
			values := items(args)
			if isStr(args) {
				for _, line := range textx.SplitLines(args.Value, false) {
					values = append(values, S(line))
				}
			}
			for j, value := range values {
				if isStr(value) && strings.TrimSpace(value.Value) != "" {
					where := at
					if isSeq(args) {
						where = at.with(j)
					}
					expression(owner, value, where, "matcher", false, false)
				}
			}
		case "cron":
			for j, job := range items(get(cfg, "jobs")) {
				for k, value := range items(get(mapOr(job), "executors")) {
					expression(owner, value, at.with("jobs", j, "executors", k), "executor", false, true)
				}
			}
		}
	}
	if graph != nil {
		*graph = g
	}
	// Iterative DFS for dependency cycles.
	done, active := map[string]bool{}, map[string]bool{}
	type frame struct {
		node string
		next int
	}
	for _, start := range g.order {
		if done[start] {
			continue
		}
		active[start] = true
		stack := []frame{{start, 0}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			list := g.edges[top.node]
			if top.next >= len(list) {
				delete(active, top.node)
				done[top.node] = true
				stack = stack[:len(stack)-1]
				continue
			}
			e := list[top.next]
			top.next++
			if active[e.target] {
				issue(e.at, "插件依赖形成循环，无法初始化", "dependency-cycle", "error")
			} else if !done[e.target] {
				active[e.target] = true
				stack = append(stack, frame{e.target, 0})
			}
		}
	}
	return issues
}

func uintNode(digits string) *Node {
	v, _ := new(big.Int).SetString(strings.TrimPrefix(digits, "+"), 10)
	return &Node{Kind: yaml.ScalarNode, Tag: tagInt, Value: v.String()}
}
