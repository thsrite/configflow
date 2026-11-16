package oxidns

import (
	"math/big"
	"strings"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

// Port of backend/common/oxidns_reference_validation.py (module C23):
// plugin references and quick-setup syntax audited against OxiDNS 1.6.1.

var (
	refDomainProviders = []string{"domain_set", "dynamic_domain_set", "geosite", "adguard_rule"}
	refIPProviders     = []string{"ip_set", "geoip"}
	refRules           = []string{"qname", "cname", "client_ip", "resp_ip", "ptr_ip", "question", "reload_provider"}
	refQuick           = map[string]bool{}
	refIgnored         = []string{"drop_resp", "reload", "prefer_ipv4", "prefer_ipv6", "ecs_handler"}
)

func init() {
	for _, name := range strings.Fields(`_true _false has_resp has_wanted_ans qtype qclass rcode qname cname client_ip
resp_ip ptr_ip question mark random env string_exp time rate_limiter forward cache black_hole
ttl sleep ecs_handler forward_edns0opt client_ip_from_ecs ipset nftset ip_selector prefer_ipv4
prefer_ipv6 download upgrade reload reload_provider drop_resp query_summary debug_print metrics_collector`) {
		refQuick[name] = true
	}
}

// refTokens is the reference module's _tokens(value): str items only.
func refTokens(value any) []string {
	values, isList := value.([]any)
	if !isList {
		values = []any{value}
	}
	var out []string
	for _, raw := range values {
		if text, ok := raw.(string); ok {
			out = append(out, splitSpaceComma(pystr.Strip(text))...)
		}
	}
	return out
}

// refUint is the reference module's _uint(raw, maximum) (bool).
func refUint(raw any, maximum int64) bool {
	return boundedUint(raw, big.NewInt(maximum), true) != nil
}

// refUintValue is _uint_value(raw).
func refUintValue(raw string) jsonx.Number {
	digits := strings.TrimLeft(strings.TrimLeft(raw, "+"), "0")
	if digits == "" {
		digits = "0"
	}
	return jsonx.Number(digits)
}

type refEdge struct {
	target string
	path   []any
}

// CollectReferenceIssues is collect_reference_issues(document,
// plugin_schemas, dependency_graph=...): the reference diagnostics; when
// graph is not nil it receives tag -> referenced tags (and order the tags in
// the dict's insertion order).
func CollectReferenceIssues(document any, catalog *jsonx.Object, graph map[string][]string, order *[]string) (issues []any, err error) {
	defer catch(&err)
	return collectReferenceIssues(document, catalog, graph, order), nil
}

func collectReferenceIssues(document any, catalog *jsonx.Object, graph map[string][]string, order *[]string) []any {
	issues := []any{}
	plugins, ok := get(mapping(document), "plugins", []any{}).([]any)
	if !ok {
		return issues
	}
	addIssue := func(path []any, message, code, severity string) {
		issues = append(issues, jsonx.Obj("path", append([]any{}, path...), "message", message, "code", code, "severity", severity))
	}
	type entry struct {
		index  int
		plugin any
	}
	registry := map[string]entry{}
	edges := map[string][]refEdge{}
	var edgeOrder []string
	for index, plugin := range plugins {
		tag, ok := get(mapping(plugin), "tag", nil).(string)
		if !ok {
			continue
		}
		if _, seen := registry[tag]; seen {
			addIssue([]any{"plugins", jsonx.Int(index), "tag"}, "插件 tag 重复，必须全局唯一", "duplicate-tag", "error")
		} else {
			registry[tag] = entry{index, plugin}
			edges[tag] = nil
			edgeOrder = append(edgeOrder, tag)
		}
	}
	schemaOf := func(kind any) any {
		if name, ok := kind.(string); ok {
			return catalog.Value(name)
		}
		return nil
	}
	reference := func(owner string, raw any, path []any, kind, exact, capability string) {
		text, ok := raw.(string)
		if !ok || envSearch(text) {
			return
		}
		tag := pystr.Strip(text)
		if tag == "" {
			addIssue(path, "插件引用不能为空", "reference", "error")
			return
		}
		target, found := registry[tag]
		if !found {
			addIssue(path, "引用的插件 tag 不存在", "reference", "error")
			return
		}
		targetType := get(target.plugin, "type", nil)
		schema := schemaOf(targetType)
		if _, known := edges[owner]; known {
			edges[owner] = append(edges[owner], refEdge{tag, path})
		}
		if !truthy(schema) {
			addIssue(path, "目标是尚未识别的插件类型，由目标核心确认其能力", "unknown-reference", "warning")
			return
		}
		switch {
		case get(schema, "x-kind", nil) != kind:
			addIssue(path, "引用目标必须为 "+kind+" 类型插件", "reference-kind", "error")
		case exact != "" && targetType != exact:
			addIssue(path, "引用目标必须为 "+exact+" 插件", "reference-type", "error")
		case capability == "domain" && !strIn(targetType, refDomainProviders...):
			addIssue(path, "引用的 Provider 必须支持域名匹配", "reference-capability", "error")
		case capability == "ip" && !strIn(targetType, refIPProviders...):
			addIssue(path, "引用的 Provider 必须支持 IP 匹配", "reference-capability", "error")
		}
	}
	providerRules := func(owner, kind string, args any, path []any) {
		for _, token := range refTokens(args) {
			if strings.HasPrefix(token, "$") && runeLen(token) > 1 {
				capability := ""
				if kind == "qname" || kind == "cname" {
					capability = "domain"
				} else if strIn(kind, "client_ip", "resp_ip", "ptr_ip") {
					capability = "ip"
				}
				reference(owner, token[1:], path, "provider", "", capability)
			}
		}
	}
	converted := func(owner, kind string, args any, path []any) {
		if schema := catalog.Value(kind); truthy(schema) {
			for _, problem := range validateSchema(args, asObject(schema), path) {
				copied := asObject(problem).Clone()
				copied.Set("path", append([]any{}, path...))
				issues = append(issues, copied)
			}
		}
		pseudo := jsonx.Obj("tag", owner, "type", kind, "args", args)
		for _, problem := range collectPluginIssues(pseudo, jsonx.Int(0)) {
			copied := asObject(problem).Clone()
			copied.Set("path", append([]any{}, path...))
			issues = append(issues, copied)
		}
		if strIn(kind, refRules...) {
			providerRules(owner, kind, args, path)
		}
	}
	shortFlags := func(tokens []string, path []any) ([]string, bool) {
		result := false
		for len(tokens) > 0 {
			last := tokens[len(tokens)-1]
			if last != "short_circuit" && !strings.HasPrefix(last, "short_circuit=") {
				break
			}
			tokens = tokens[:len(tokens)-1]
			if last == "short_circuit" {
				result = true
			} else if value := last[strings.IndexByte(last, '=')+1:]; value != "true" && value != "false" {
				addIssue(path, "short_circuit 只能使用 true 或 false", "quick-setup", "error")
			} else {
				result = strings.HasSuffix(last, "=true")
			}
		}
		return tokens, result
	}
	quickSetup := func(path []any, message string) { addIssue(path, message, "quick-setup", "error") }
	quick := func(owner, kind string, param *string, path []any, expected string) {
		schema, present := catalog.Get(kind)
		if !present || schema == nil {
			addIssue(path, "尚未识别的快捷表达式，请由目标核心确认", "unknown-expression", "warning")
			return
		}
		if get(schema, "x-kind", nil) != expected {
			addIssue(path, "此处需要 "+expected+" 表达式", "expression-kind", "error")
			return
		}
		if !refQuick[kind] {
			quickSetup(path, "该插件不支持快捷表达式，请定义插件后使用 $tag 引用")
			return
		}
		raw := ""
		if param != nil {
			raw = *param
		}
		tokens := pySplit(raw)
		switch {
		case strIn(kind, refIgnored...) || strIn(kind, "query_summary", "debug_print", "metrics_collector"):
			return
		case strIn(kind, "_true", "_false", "has_resp", "has_wanted_ans"):
			if pystr.Strip(raw) != "" {
				quickSetup(path, "此匹配器不接受参数")
			}
			return
		case strIn(kind, refRules...) || strIn(kind, "qtype", "qclass", "rcode", "mark", "random", "string_exp", "ttl"):
			if pystr.Strip(raw) == "" {
				quickSetup(path, "快捷表达式缺少必填参数")
				return
			}
			converted(owner, kind, raw, path)
		case kind == "env":
			converted(owner, kind, jsonx.ListOf(tokens), path)
		case kind == "forward":
			tokens, _ = shortFlags(tokens, path)
			upstreams := []any{}
			for _, token := range tokens {
				upstreams = append(upstreams, jsonx.Obj("addr", token))
			}
			converted(owner, kind, jsonx.Obj("upstreams", upstreams), path)
		case kind == "cache":
			for _, token := range tokens {
				if !strIn(token, "short_circuit", "short_circuit=true", "short_circuit=false") {
					quickSetup(path, "cache 快捷参数仅支持 short_circuit、short_circuit=true/false")
					break
				}
			}
		case kind == "black_hole":
			rest, short := shortFlags(tokens, path)
			rest = refTokens(jsonx.ListOf(rest))
			var mode *string
			if len(rest) > 0 && strIn(pystr.Lower(rest[0]), "nxdomain", "nodata", "null", "custom", "refused") {
				lowered := pystr.Lower(rest[0])
				mode = &lowered
				rest = rest[1:]
			}
			args := jsonx.Obj("ips", jsonx.ListOf(rest), "short_circuit", short)
			if mode != nil {
				args.Set("mode", *mode)
			}
			converted(owner, kind, args, path)
		case kind == "sleep":
			message := formatError("duration", raw)
			if pystr.Strip(raw) == "" || message != "" {
				if message == "" {
					message = "sleep 必须填写时长"
				}
				quickSetup(path, message)
			}
		case kind == "forward_edns0opt":
			for _, token := range refTokens(raw) {
				if !refUint(token, 65535) {
					quickSetup(path, "EDNS0 代码必须为 0–65535 的整数")
					break
				}
			}
		case kind == "client_ip_from_ecs":
			converted(owner, kind, jsonx.ListOf(refTokens(raw)), path)
		case kind == "time":
			if len(tokens) != 1 || !strings.Contains(tokens[0], "-") {
				quickSetup(path, "time 快捷参数必须是单个 HH:MM-HH:MM 时间段")
				return
			}
			start, end, _ := strings.Cut(tokens[0], "-")
			converted(owner, kind, jsonx.Obj("periods", []any{jsonx.Obj("start", start, "end", end)}), path)
		case kind == "rate_limiter":
			args := jsonx.NewObject()
			for index, key := range []string{"qps", "burst", "mask4", "mask6"} {
				if len(tokens) <= index {
					break
				}
				if key == "qps" {
					f, err := pystr.Float(tokens[index])
					if err != nil {
						quickSetup(path, "rate_limiter 快捷参数需为 qps burst mask4 mask6 数字")
						return
					}
					args.Set(key, jsonx.Float(f))
					continue
				}
				if !refUint(tokens[index], 4294967295) {
					quickSetup(path, "rate_limiter 快捷参数需为 qps burst mask4 mask6 数字")
					return
				}
				args.Set(key, refUintValue(tokens[index]))
			}
			converted(owner, kind, args, path)
		case kind == "ip_selector":
			args := jsonx.NewObject()
			if len(tokens) > 0 && strIn(tokens[0], "first_success", "best_within_budget", "background") {
				args.Set("selection_mode", tokens[0])
				tokens = tokens[1:]
			}
			if len(tokens) > 0 {
				args.Set("probe_methods", jsonx.ListOf(tokens))
			}
			converted(owner, kind, args, path)
		case kind == "ipset":
			args := jsonx.NewObject()
			for _, token := range tokens {
				parts := strings.Split(token, ",")
				if len(parts) != 3 || !strIn(parts[1], "inet", "inet6") || !refUint(parts[2], 255) {
					quickSetup(path, "ipset 快捷格式为 set,inet|inet6,mask")
					continue
				}
				suffix := "4"
				if parts[1] != "inet" {
					suffix = "6"
				}
				args.Set("set_name"+suffix, parts[0])
				args.Set("mask"+suffix, refUintValue(parts[2]))
			}
			converted(owner, kind, args, path)
		case kind == "nftset":
			args := jsonx.NewObject()
			for _, token := range tokens {
				parts := strings.Split(token, ",")
				if len(parts) != 5 || !strIn(parts[0], "ip", "ip6", "inet") || parts[1] == "" || parts[2] == "" || !refUint(parts[4], 255) {
					quickSetup(path, "nftset 快捷格式为 family,table,set,type,mask")
					continue
				}
				if !strIn(parts[3], "ipv4_addr", "ipv6_addr") {
					addIssue(path, "未知 nftset 地址类型，核心会忽略此条目", "ignored-parameter", "warning")
					continue
				}
				key := "ipv6"
				if parts[3] == "ipv4_addr" {
					key = "ipv4"
				}
				args.Set(key, jsonx.Obj("table_family", parts[0], "table_name", parts[1], "set_name", parts[2], "mask", refUintValue(parts[4])))
			}
			converted(owner, kind, args, path)
		case kind == "download":
			parts := splitMax1(pystr.Strip(raw))
			if len(parts) < 2 || pystr.Strip(parts[1]) == "" {
				quickSetup(path, "download 快捷参数必须包含 URL 和目录")
			} else {
				converted(owner, kind, jsonx.Obj("downloads", []any{jsonx.Obj("url", parts[0], "dir", pystr.Strip(parts[1]))}), path)
			}
		case kind == "upgrade":
			args := jsonx.NewObject()
			for _, token := range tokens {
				if token == "force" || token == "no_restart" {
					args.Set(token, true)
					continue
				}
				key, value, found := strings.Cut(token, "=")
				switch {
				case !found || !strIn(key, "force", "no_restart", "github_token", "bundle"):
					quickSetup(path, "upgrade 包含不支持的快捷参数")
				case key == "force" || key == "no_restart":
					if value != "true" && value != "false" {
						quickSetup(path, "upgrade 开关必须为 true 或 false")
					} else {
						args.Set(key, value == "true")
					}
				case key == "bundle":
					args.Set(key, pystr.Lower(value))
				default:
					args.Set(key, value)
				}
			}
			converted(owner, kind, args, path)
		}
	}
	expression := func(owner string, value any, path []any, expected string, sequence, cron bool) {
		raw, ok := value.(string)
		if !ok || envSearch(raw) {
			return
		}
		raw = lstrip(raw)
		if expected == "matcher" && strings.HasPrefix(raw, "!") {
			raw = lstrip(raw[1:])
		}
		if pystr.Strip(raw) == "" {
			addIssue(path, "表达式不能为空", "expression", "error")
			return
		}
		if strings.HasPrefix(raw, "$") {
			reference(owner, raw[1:], path, expected, "", "")
			return
		}
		split := splitMax1(raw)
		first := split[0]
		var param *string
		if len(split) > 1 {
			param = &split[1]
		}
		paramText := ""
		if param != nil {
			paramText = *param
		}
		if sequence && (first == "accept" || first == "return") {
			return
		}
		if sequence && (first == "jump" || first == "goto") {
			if paramText == "" || strings.HasPrefix(pystr.Strip(paramText), "$") {
				addIssue(path, "jump/goto 必须引用不带 $ 的 sequence tag", "expression", "error")
			} else {
				reference(owner, paramText, path, "executor", "sequence", "")
			}
			return
		}
		if sequence && first == "reject" {
			tokens := pySplit(paramText)
			if len(tokens) > 0 && (len(tokens) != 1 || !dnsCodeValid("base-rcode", tokens[0])) {
				addIssue(path, "reject 只接受一个基础响应码 0–15 或对应名称", "expression", "error")
			}
			return
		}
		if sequence && (first == "mark" || first == "set_mark") {
			var tokens []string
			if param != nil {
				tokens = refTokens(paramText)
			}
			bad := len(tokens) == 0
			for _, token := range tokens {
				if !refUint(token, 4294967295) {
					bad = true
				}
			}
			if bad {
				addIssue(path, "mark/set_mark 至少需要一个 0–4294967295 整数", "expression", "error")
			}
			return
		}
		if cron && get(catalog.GetOr(first, jsonx.NewObject()), "x-kind", nil) != "executor" {
			reference(owner, raw, path, "executor", "", "")
			return
		}
		quick(owner, first, param, path, expected)
	}
	for index, plugin := range plugins {
		if !isDict(plugin) {
			continue
		}
		owner, ok1 := get(plugin, "tag", nil).(string)
		kind, ok2 := get(plugin, "type", nil).(string)
		args := get(plugin, "args", nil)
		if !ok1 || !ok2 {
			continue
		}
		path := []any{"plugins", jsonx.Int(index), "args"}
		cfg := mapping(args)
		if strIn(kind, "udp_server", "tcp_server", "quic_server") {
			reference(owner, get(cfg, "entry", nil), appendPath(path, "entry"), "executor", "", "")
		}
		if kind == "http_server" {
			entries, _ := get(cfg, "entries", nil).([]any)
			for j, entry := range entries {
				reference(owner, get(mapping(entry), "exec", nil), appendPath(path, "entries", jsonx.Int(j), "exec"), "executor", "", "")
			}
		}
		if kind == "fallback" {
			for _, key := range []string{"primary", "secondary"} {
				reference(owner, get(cfg, key, nil), appendPath(path, key), "executor", "", "")
			}
		}
		if strIn(kind, "prefer_ipv4", "prefer_ipv6") {
			reference(owner, get(cfg, "probe_executor", nil), appendPath(path, "probe_executor"), "executor", "", "")
		}
		if kind == "learn_domain" {
			reference(owner, get(cfg, "provider", nil), appendPath(path, "provider"), "provider", "dynamic_domain_set", "")
		}
		if strIn(kind, "domain_set", "ip_set") {
			sets, _ := get(cfg, "sets", nil).([]any)
			capability := "ip"
			if kind == "domain_set" {
				capability = "domain"
			}
			for j, value := range sets {
				reference(owner, value, appendPath(path, "sets", jsonx.Int(j)), "provider", "", capability)
			}
		}
		if strIn(kind, refRules...) {
			providerRules(owner, kind, args, path)
		}
		if rulesList, ok := args.([]any); ok && kind == "sequence" {
			for j, rule := range rulesList {
				rule = mapping(rule)
				expression(owner, get(rule, "exec", nil), appendPath(path, jsonx.Int(j), "exec"), "executor", true, false)
				switch matches := get(rule, "matches", nil).(type) {
				case string:
					expression(owner, matches, appendPath(path, jsonx.Int(j), "matches"), "matcher", false, false)
				case []any:
					for k, match := range matches {
						expression(owner, match, appendPath(path, jsonx.Int(j), "matches", jsonx.Int(k)), "matcher", false, false)
					}
				}
			}
		}
		if kind == "any_match" {
			var values []any
			list, isList := args.([]any)
			if isList {
				values = list
			} else if text, ok := args.(string); ok {
				for _, line := range convert.PySplitlines(text) {
					values = append(values, line)
				}
			}
			for j, value := range values {
				if text, ok := value.(string); ok && pystr.Strip(text) != "" {
					at := path
					if isList {
						at = appendPath(path, jsonx.Int(j))
					}
					expression(owner, value, at, "matcher", false, false)
				}
			}
		}
		if kind == "cron" {
			jobs, _ := get(cfg, "jobs", nil).([]any)
			for j, job := range jobs {
				values, _ := get(mapping(job), "executors", nil).([]any)
				for k, value := range values {
					expression(owner, value, appendPath(path, "jobs", jsonx.Int(j), "executors", jsonx.Int(k)), "executor", false, true)
				}
			}
		}
	}
	if graph != nil {
		for _, tag := range edgeOrder {
			targets := []string{}
			for _, e := range edges[tag] {
				targets = append(targets, e.target)
			}
			graph[tag] = targets
		}
		if order != nil {
			*order = append(*order, edgeOrder...)
		}
	}
	// Iterative DFS (cycle detection) in edge insertion order.
	done, active := map[string]bool{}, map[string]bool{}
	type frame struct {
		node string
		next int
	}
	for _, start := range edgeOrder {
		if done[start] {
			continue
		}
		active[start] = true
		stack := []frame{{start, 0}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			list := edges[top.node]
			if top.next >= len(list) {
				delete(active, top.node)
				done[top.node] = true
				stack = stack[:len(stack)-1]
				continue
			}
			e := list[top.next]
			top.next++
			if active[e.target] {
				addIssue(e.path, "插件依赖形成循环，无法初始化", "dependency-cycle", "error")
			} else if !done[e.target] {
				active[e.target] = true
				stack = append(stack, frame{e.target, 0})
			}
		}
	}
	return issues
}
