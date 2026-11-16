package oxidns

import (
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/rules"
)

// Parts of backend/common/rule_dns.py (module C27) that the common routing
// editor (C25) needs: recognition of rule DNS execs and pinned upstream
// chains, chain features, policy DNS routes. C27 adds the rest of
// rule_dns.py to ruledns.go.

// NonProxyPolicies is NON_PROXY_POLICIES: built-in policies that never send
// traffic through a proxy group.
var NonProxyPolicies = []string{"REJECT", "REJECT-DROP", "PASS"}

var (
	ruleReject = map[string]string{"": "refused", "5": "refused", "refused": "refused",
		"3": "nxdomain", "nxdomain": "nxdomain", "0": "nodata", "noerror": "nodata"}
	blackholeReason = "黑洞插件 " + BlackholeTag + " 已被手动修改（应为 mode: \"null\"、short_circuit: true），" +
		"请在完整参数中恢复或改名"
)

func chainReason(tag string) string {
	return "指定上游处理链 " + tag + " 已被手动修改，请在完整参数中恢复"
}

// invalidRoute is rule_dns.Invalid: a recognizable rule DNS exec whose owned
// helper was edited by hand. Routes are any: a string, invalidRoute or nil
// (None).
type invalidRoute struct{ reason string }

// isGroupTag is GROUP_TAG_RE.fullmatch(tag): rules_(domain|ip)_\d+(?:_\d+)*
// with Unicode decimal digits.
func isGroupTag(tag string) bool {
	var rest string
	switch {
	case strings.HasPrefix(tag, "rules_domain_"):
		rest = tag[len("rules_domain_"):]
	case strings.HasPrefix(tag, "rules_ip_"):
		rest = tag[len("rules_ip_"):]
	default:
		return false
	}
	for _, piece := range strings.Split(rest, "_") {
		if piece == "" {
			return false
		}
		for _, r := range piece {
			if !isDecimalRune(r) {
				return false
			}
		}
	}
	return true
}

// ttlExec is _TTL.fullmatch(value): "ttl " + [1-9][0-9]*.
func ttlExec(value string) bool {
	if !strings.HasPrefix(value, "ttl ") || len(value) < 5 || value[4] < '1' || value[4] > '9' {
		return false
	}
	for i := 5; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// chainFeatures is chain_features(plugins): the china_dns / remote_dns
// features a pinned upstream chain copies, read from a tag -> plugin map.
func chainFeatures(plugins *jsonx.Object) *jsonx.Object {
	args := func(tag string) any {
		plugin, ok := plugins.Value(tag).(*jsonx.Object)
		if !ok || plugin == nil {
			return nil
		}
		return plugin.Value("args")
	}
	steps := func(tag string) []any {
		value, ok := args(tag).([]any)
		if !ok {
			return nil
		}
		var out []any
		for _, element := range value {
			if isDict(element) {
				out = append(out, element)
			}
		}
		return out
	}
	china := steps("china_dns")
	result := jsonx.NewObject()
	for _, name := range RealWrappers {
		result.Set(name, jsonx.Obj("enabled", listContains(china, jsonx.Obj("exec", "$"+name))))
	}
	cache, isCache := plugins.Value("lazy_cache").(*jsonx.Object)
	result.Set("cache_enabled", listContains(china, jsonx.Obj("exec", "$lazy_cache")) &&
		isCache && cache != nil && cache.Value("type") == "cache")
	var lazy any
	if lazyArgs, ok := args("lazy_cache").(*jsonx.Object); ok && lazyArgs != nil {
		lazy = lazyArgs.Value("lazy_cache_ttl")
	}
	if n, ok := lazy.(jsonx.Number); ok && n.IsInt() && bigOf(n).Sign() > 0 {
		result.Set("cache_lazy_ttl", n)
	} else {
		result.Set("cache_lazy_ttl", jsonx.Int(0))
	}
	ttl := ""
	for _, element := range steps("remote_dns") {
		if exec, ok := get(element, "exec", nil).(string); ok && ttlExec(exec) {
			ttl = exec
			break
		}
	}
	if ttl != "" {
		n, err := jsonx.ParseNumber(ttl[4:])
		must(err)
		result.Set("remote_ttl", n)
	} else {
		result.Set("remote_ttl", jsonx.Int(0))
	}
	return result
}

// pinnedChain is _pinned(key, plugins): the upstream entry and possible
// sources of a canonical pinned chain, ok false when it was edited.
func pinnedChain(key string, plugins *jsonx.Object) (entry *jsonx.Object, sources []string, ok bool) {
	chain, _ := plugins.Value(UpstreamPrefix + key).(*jsonx.Object)
	forward, _ := plugins.Value(ForwardPrefix + key).(*jsonx.Object)
	cache, _ := plugins.Value(CachePrefix + key).(*jsonx.Object)
	if chain == nil || chain.Value("type") != "sequence" {
		return nil, nil, false
	}
	list, isList := chain.Value("args").([]any)
	if !isList {
		return nil, nil, false
	}
	steps := append([]any{}, list...)
	pop0 := func() any { v := steps[0]; steps = steps[1:]; return v }
	popLast := func() any { v := steps[len(steps)-1]; steps = steps[:len(steps)-1]; return v }
	if len(steps) == 0 || !sameValue(pop0(), jsonx.Obj("exec", "drop_resp")) || len(steps) == 0 ||
		!sameValue(popLast(), jsonx.Obj("exec", "accept")) {
		return nil, nil, false
	}
	var wrappers []string
	var ttl any
	wrapperExecs := []any{}
	for _, name := range RealWrappers {
		wrapperExecs = append(wrapperExecs, "$"+name)
	}
	for len(steps) > 0 {
		first, isObj := steps[0].(*jsonx.Object)
		if !isObj || first == nil || !exactKeys(first, "exec") || !listContains(wrapperExecs, first.Value("exec")) {
			break
		}
		wrappers = append(wrappers, pop0().(*jsonx.Object).Value("exec").(string)[1:])
	}
	var ordered []string
	for _, name := range RealWrappers {
		for _, w := range wrappers {
			if w == name {
				ordered = append(ordered, name)
				break
			}
		}
	}
	if strings.Join(wrappers, "\x00") != strings.Join(ordered, "\x00") || len(wrappers) != len(ordered) {
		return nil, nil, false
	}
	if len(steps) > 0 && sameValue(steps[0], jsonx.Obj("exec", "$"+CachePrefix+key)) {
		pop0()
		var args *jsonx.Object
		if cache != nil && cache.Value("type") == "cache" {
			args, _ = cache.Value("args").(*jsonx.Object)
		}
		var lazy any
		if args != nil {
			lazy = args.Value("lazy_cache_ttl")
		}
		lazyN, lazyOK := lazy.(jsonx.Number)
		if args == nil || !(sameValue(args, jsonx.Obj("size", UpstreamCacheSize, "short_circuit", true)) ||
			(lazyOK && lazyN.IsInt() && bigOf(lazyN).Sign() > 0 &&
				sameValue(args, jsonx.Obj("size", UpstreamCacheSize, "lazy_cache_ttl", lazy, "short_circuit", true)))) {
			return nil, nil, false
		}
	}
	if len(steps) > 0 {
		last, isObj := steps[len(steps)-1].(*jsonx.Object)
		if isObj && last != nil && exactKeys(last, "exec") {
			if exec, isText := last.Value("exec").(string); isText && ttlExec(exec) {
				ttl = popLast()
			}
		}
	}
	if !sameValue(steps, []any{jsonx.Obj("exec", "$"+ForwardPrefix+key),
		jsonx.Obj("matches", []any{"!has_resp"}, "exec", "reject 3")}) {
		return nil, nil, false
	}
	var args *jsonx.Object
	if forward != nil && forward.Value("type") == "forward" {
		args, _ = forward.Value("args").(*jsonx.Object)
	}
	var upstreams []any
	if args != nil && exactKeys(args, "upstreams") {
		upstreams, _ = args.Value("upstreams").([]any)
	}
	if len(upstreams) != 1 {
		return nil, nil, false
	}
	first, isObj := upstreams[0].(*jsonx.Object)
	if !isObj || first == nil {
		return nil, nil, false
	}
	if addr, isText := first.Value("addr").(string); !isText || pystr.Strip(addr) == "" {
		return nil, nil, false
	}
	if len(wrappers) > 0 && ttl != nil {
		return nil, nil, false
	}
	switch {
	case len(wrappers) > 0:
		sources = []string{"local"}
	case ttl != nil:
		sources = []string{"remote", "fallback"}
	default:
		sources = UpstreamSources
	}
	return first, sources, true
}

// pinnedSource is _source(key, entry, sources, plugins): the source of a
// pinned chain ("" = None).
func pinnedSource(key string, entry *jsonx.Object, sources []string, plugins *jsonx.Object) string {
	for _, source := range sources {
		if entryKey(entry, source) == key {
			return source
		}
	}
	if len(sources) == 1 {
		return sources[0]
	}
	for _, source := range sources {
		if source == "local" {
			// A bare chain: no local hooks and no TTL rewrite.
			features := chainFeatures(plugins)
			hooks := false
			for _, name := range RealWrappers {
				if enabled(features, name) {
					hooks = true
				}
			}
			var kept []string
			for _, item := range sources {
				drop := truthy(features.Value("remote_ttl"))
				if item == "local" {
					drop = hooks
				}
				if !drop {
					kept = append(kept, item)
				}
			}
			sources = kept
			break
		}
	}
	if len(sources) > 0 {
		return sources[0]
	}
	return ""
}

// parseExec is parse_exec(value, part, plugins): the route of one
// sequence_main rule-group exec, nil when it is not a rule DNS action, or an
// invalidRoute when an owned helper was edited by hand.
func parseExec(value any, part string, plugins *jsonx.Object) any {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	targets, known := ruleGoto[part]
	if !known {
		raise(pyErr("KeyError", jsonx.Repr(part)))
	}
	for _, route := range ListRoutes {
		if text == targets[route] {
			return route
		}
	}
	words := pySplit(text)
	if len(words) > 0 && pystr.Lower(words[0]) == "reject" && len(words) <= 2 {
		word := ""
		if len(words) == 2 {
			word = pystr.Lower(words[1])
		}
		if route, found := ruleReject[word]; found {
			return route
		}
		return nil
	}
	if text == "$"+BlackholeTag {
		if sameValue(plugins.Value(BlackholeTag), BlackholePlugin()) {
			return "blackhole"
		}
		return invalidRoute{blackholeReason}
	}
	if strings.HasPrefix(text, "goto "+UpstreamPrefix) && ruleKey.MatchString(text[5+len(UpstreamPrefix):]) {
		key := text[5+len(UpstreamPrefix):]
		entry, sources, found := pinnedChain(key, plugins)
		if !found {
			return invalidRoute{chainReason(UpstreamPrefix + key)}
		}
		source := pinnedSource(key, entry, sources, plugins)
		if source == "" {
			return invalidRoute{chainReason(UpstreamPrefix + key)}
		}
		return "upstream:" + entryKey(entry, source)
	}
	return nil
}

// pinnedAction is pinned_action(key, plugins): the stored action of a
// recognized "goto cf_dns_up_<key>" chain, nil when there is none.
func pinnedAction(key string, plugins *jsonx.Object) *jsonx.Object {
	route, ok := parseExec("goto "+UpstreamPrefix+key, "domain", plugins).(string)
	if !ok {
		return nil
	}
	entry, sources, _ := pinnedChain(key, plugins)
	for _, source := range sources {
		if "upstream:"+entryKey(entry, source) == route {
			return jsonx.Obj("action", "upstream", "source", source, "upstream", copyValue(entry))
		}
	}
	raise(pyErr("StopIteration", ""))
	return nil
}

// defaultRoute is default_route(default_forward).
func defaultRoute(defaultForward any) string {
	if defaultForward == "forward_local" {
		return "direct"
	}
	return "proxy"
}

// probeNeeded is probe_needed(routes, default_forward): whether any IP route
// differs from the fallback (an invalidRoute always differs).
func probeNeeded(routes []any, defaultForward any) bool {
	fallback := defaultRoute(defaultForward)
	for _, route := range routes {
		if route != fallback {
			return true
		}
	}
	return false
}

// policyDNSRoutes is policy_dns_routes(config): the DNS routes of the
// configured group defaults ({group name: "direct" | "proxy" | None}).
func policyDNSRoutes(config *jsonx.Object) *jsonx.Object {
	groups := jsonx.NewObject()
	list := config.Value("proxy_groups")
	if !truthy(list) {
		list = []any{}
	}
	for _, element := range pyIter(list) {
		group, ok := element.(*jsonx.Object)
		if !ok || group == nil {
			continue
		}
		if name, isText := group.Value("name").(string); isText {
			groups.Set(name, group)
		}
	}
	byID := jsonx.NewObject()
	for _, element := range groups.Values() {
		group := element.(*jsonx.Object)
		if id, isText := group.Value("id").(string); isText {
			byID.Set(id, group)
		}
	}
	resolved := map[string]any{}
	var resolve func(name any, visiting map[string]bool) any
	resolve = func(name any, visiting map[string]bool) any {
		if name == "DIRECT" {
			return "direct"
		}
		if strIn(name, NonProxyPolicies...) {
			return nil
		}
		hashable(name)
		text, isText := name.(string)
		if isText {
			if route, done := resolved[text]; done {
				return route
			}
		}
		var group *jsonx.Object
		if isText {
			group, _ = groups.Value(text).(*jsonx.Object)
		}
		if group == nil || !truthy(group.GetOr("enabled", true)) || group.Value("type") == "chain" ||
			(isText && visiting[text]) || len(visiting) >= 64 {
			return "proxy"
		}
		source := group
		if follow := group.Value("follow_group"); truthy(follow) {
			hashable(follow)
			source = nil
			if id, ok := follow.(string); ok {
				source, _ = byID.Value(id).(*jsonx.Object)
			}
			if source == nil || truthy(source.Value("follow_group")) {
				return "proxy"
			}
		}
		if source.Value("type") != "select" {
			return "proxy"
		}
		emitted := rules.RenderMembers(source, config)
		if pyContains(emitted.GetOr("filter", ""), "`") {
			return "proxy"
		}
		if truthy(source.Value("exclude-filter")) || truthy(source.Value("exclude-type")) {
			return "proxy"
		}
		candidates := emitted.Value("proxies")
		var route any = "proxy"
		if truthy(candidates) {
			next := map[string]bool{text: true}
			for key := range visiting {
				next[key] = true
			}
			list, isList := candidates.([]any)
			if !isList {
				raise(pyErr("TypeError", "'"+typeName(candidates)+"' object is not subscriptable"))
			}
			route = resolve(list[0], next)
		}
		resolved[text] = route
		return route
	}
	result := jsonx.NewObject()
	for _, name := range groups.Keys() {
		result.Set(name, resolve(name, map[string]bool{}))
	}
	return result
}

// followRoute is follow_route(item, routes=routes): the DNS route implied by
// the rule's policy and configured group default (nil = None).
func followRoute(entry *jsonx.Object, routes *jsonx.Object) any {
	if !Convertible(entry) {
		return nil
	}
	policy := entry.Value("policy")
	if policy == "DIRECT" {
		return "direct"
	}
	text, ok := policy.(string)
	if !ok || text == "" || strIn(text, NonProxyPolicies...) {
		return nil
	}
	if value, found := routes.Get(text); found {
		return value
	}
	return "proxy"
}
