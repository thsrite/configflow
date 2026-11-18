package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

// Per-rule DNS handling (backend/common/rule_dns.py, the parts generation and
// native routing use). A route is "direct"/"proxy" (selector lists), an
// inline rejection, "blackhole" or "upstream:<h12>"; a recognizable exec whose
// owned helper was edited by hand is an invalid route ("!" + reason).

// Rule DNS constants.
const (
	BlackholeTag      = "cf_dns_blackhole"
	UpstreamPrefix    = "cf_dns_up_"
	ForwardPrefix     = "cf_dns_fwd_"
	CachePrefix       = "cf_dns_cache_"
	upstreamCacheSize = 1024
)

var (
	// Actions are the explicit rule_actions; ListRoutes the selector routes.
	Actions         = []string{"refused", "nxdomain", "nodata", "blackhole", "upstream"}
	ListRoutes      = []string{"direct", "proxy"}
	UpstreamSources = []string{"local", "remote", "fallback"}
	InlineExec      = map[string]string{"refused": "reject", "nxdomain": "reject 3", "nodata": "reject 0"}
	RouteLabels     = map[string]string{"direct": "直连 DNS", "proxy": "代理 DNS", "refused": "拒绝", "nxdomain": "不存在",
		"nodata": "空应答", "blackhole": "黑洞", "upstream": "指定上游"}
	// RealWrappers are china_dns's response hooks, in order.
	RealWrappers     = []string{"ip_selector", "ros_address_list", "ros_route"}
	nonProxyPolicies = []string{"REJECT", "REJECT-DROP", "PASS"}
	groupTagRE       = regexp.MustCompile(`^rules_(domain|ip)_[0-9]+(?:_[0-9]+)*$`)
	keyRE            = regexp.MustCompile(`^[0-9a-f]{12}$`)
	ttlExecRE        = regexp.MustCompile(`^ttl ([1-9][0-9]*)$`)
	gotoRoutes       = map[string]map[string]string{
		"domain": {"direct": "goto china_dns", "proxy": "goto proxy_dns_seq"},
		"ip":     {"direct": "goto direct_ip", "proxy": "goto ip_requery_proxy"},
	}
	rejectRoutes   = map[string]string{"": "refused", "5": "refused", "refused": "refused", "3": "nxdomain", "nxdomain": "nxdomain", "0": "nodata", "noerror": "nodata"}
	sourceTags     = map[string]string{"local": "forward_local", "remote": "forward_remote", "fallback": "forward_fallback"}
	blackholeNode  = M("tag", BlackholeTag, "type", "black_hole", "args", M("mode", "null", "short_circuit", true))
	blackholeError = "黑洞插件 " + BlackholeTag + ` 已被手动修改（应为 mode: "null"、short_circuit: true），请在完整参数中恢复或改名`
)

func invalidRoute(reason string) string { return "!" + reason }
func isInvalid(route string) bool       { return strings.HasPrefix(route, "!") }
func isListRoute(route string) bool     { return route == "direct" || route == "proxy" }

func chainReason(tag string) string {
	return "指定上游处理链 " + tag + " 已被手动修改，请在完整参数中恢复"
}

// EntryKey is the 12 hex digits naming one pinned upstream copy.
func EntryKey(upstream *Node, source string) string {
	sum := sha256.Sum256(canonicalJSON(M("source", source, "upstream", upstream)))
	return hex.EncodeToString(sum[:])[:12]
}

// RouteKey is the route of a stored action.
func RouteKey(action *Node) string {
	kind := str(get(action, "action"))
	if kind == "upstream" {
		return "upstream:" + EntryKey(get(action, "upstream"), str(get(action, "source")))
	}
	return kind
}

// RouteExec is the sequence_main exec of a rule group.
func RouteExec(route, part string) (string, error) {
	switch {
	case isListRoute(route):
		return gotoRoutes[part][route], nil
	case InlineExec[route] != "":
		return InlineExec[route], nil
	case route == "blackhole":
		return "$" + BlackholeTag, nil
	case strings.HasPrefix(route, "upstream:") && keyRE.MatchString(route[9:]):
		return "goto " + UpstreamPrefix + route[9:], nil
	}
	return "", fail("不支持的 DNS 处理：" + strings.TrimPrefix(route, "!"))
}

// NormalizeAction validates one rule_actions (or, parked, rule_dns_parked)
// value and returns its canonical copy.
func NormalizeAction(value *Node, parked bool) (*Node, error) {
	allowed := Actions
	if parked {
		allowed = append(append([]string{}, ListRoutes...), Actions...)
	}
	kind := get(value, "action")
	if !isMap(value) || !isOneOf(kind, allowed...) {
		return nil, fail("规则 DNS 处理无效：" + strings.Join(allowed, "、"))
	}
	if kind.Value != "upstream" {
		if mapSize(value) != 1 {
			return nil, fail("规则 DNS 处理「" + RouteLabels[kind.Value] + "」不能包含其他参数")
		}
		return M("action", kind.Value), nil
	}
	if mapSize(value) != 3 || !has(value, "source") || !has(value, "upstream") || !isOneOf(get(value, "source"), UpstreamSources...) {
		return nil, fail("指定上游需要 source（local、remote 或 fallback）和 upstream")
	}
	upstream := get(value, "upstream")
	if !isMap(upstream) || strings.TrimSpace(str(get(upstream, "addr"))) == "" {
		return nil, fail("指定上游需要非空的 addr 地址")
	}
	return M("action", "upstream", "source", get(value, "source").Value, "upstream", clone(upstream)), nil
}

func sectionEnabled(settings *Node, name string) bool {
	return isTrue(get(get(settings, name), "enabled"))
}

// upstreamChain is forward + optional cache + sequence behind one pinned
// upstream; features is a settings-shaped mapping (see chainFeatures).
func upstreamChain(action, features *Node) []*Node {
	source := str(get(action, "source"))
	key := EntryKey(get(action, "upstream"), source)
	plugins := []*Node{M("tag", ForwardPrefix+key, "type", "forward", "args", M("upstreams", L(clone(get(action, "upstream")))))}
	steps := L(M("exec", "drop_resp"))
	if source == "local" {
		for _, name := range RealWrappers {
			if sectionEnabled(features, name) {
				steps.Content = append(steps.Content, M("exec", "$"+name))
			}
		}
	}
	if isTrue(get(features, "cache_enabled")) {
		cache := M("size", upstreamCacheSize)
		if lazy, ok := ratOf(get(features, "cache_lazy_ttl")); ok && lazy.Sign() > 0 {
			set(cache, "lazy_cache_ttl", clone(get(features, "cache_lazy_ttl")))
		}
		set(cache, "short_circuit", B(true))
		plugins = append(plugins, M("tag", CachePrefix+key, "type", "cache", "args", cache))
		steps.Content = append(steps.Content, M("exec", "$"+CachePrefix+key))
	}
	steps.Content = append(steps.Content, M("exec", "$"+ForwardPrefix+key), M("matches", L("!has_resp"), "exec", "reject 3"))
	if ttl := get(features, "remote_ttl"); source != "local" && (positive(ttl)) {
		steps.Content = append(steps.Content, M("exec", "ttl "+ttl.Value))
	}
	steps.Content = append(steps.Content, M("exec", "accept"))
	return append(plugins, M("tag", UpstreamPrefix+key, "type", "sequence", "args", steps))
}

// HelperPlugins are the owned plugins the given group routes need, in
// first-use order; actions (rule_actions) supply pinned upstreams, features
// the china_dns hooks copied into local chains.
func HelperPlugins(routes []string, actions, features *Node) ([]*Node, error) {
	pinned := map[string]*Node{}
	pairs(actions, func(_ string, action *Node) bool {
		if str(get(action, "action")) == "upstream" {
			if key := RouteKey(action); pinned[key] == nil {
				pinned[key] = action
			}
		}
		return true
	})
	var result []*Node
	seen := map[string]bool{}
	for _, route := range routes {
		if seen[route] {
			continue
		}
		seen[route] = true
		if route == "blackhole" {
			result = append(result, clone(blackholeNode))
		} else if strings.HasPrefix(route, "upstream:") {
			if pinned[route] == nil {
				return nil, fail("指定上游缺少上游地址，请重新选择")
			}
			result = append(result, upstreamChain(pinned[route], features)...)
		}
	}
	return result, nil
}

// pluginMap is tag → plugin of a document's plugins; nil when the list is
// not a clean list of uniquely tagged mappings (routing's _plugins).
func pluginMap(document *Node) map[string]*Node {
	result := map[string]*Node{}
	for _, item := range items(get(document, "plugins")) {
		tag, ok := tagOf(item)
		if !isMap(item) || !ok || result[tag] != nil {
			return map[string]*Node{}
		}
		result[tag] = item
	}
	return result
}

// chainFeatures reads the china_dns / remote_dns features a pinned chain
// copies from a native document's plugins.
func chainFeatures(plugins map[string]*Node) *Node {
	args := func(tag string) *Node { return get(plugins[tag], "args") }
	steps := func(tag string) []*Node {
		return filter(items(args(tag)), isMap)
	}
	china := seqOf(steps("china_dns"))
	result := M()
	for _, name := range RealWrappers {
		set(result, name, M("enabled", contains(china, M("exec", "$"+name))))
	}
	cache := plugins["lazy_cache"]
	set(result, "cache_enabled", B(contains(china, M("exec", "$lazy_cache")) && isMap(cache) && equal(get(cache, "type"), S("cache"))))
	lazy := get(args("lazy_cache"), "lazy_cache_ttl")
	if v, ok := int64Of(lazy); ok && v > 0 {
		set(result, "cache_lazy_ttl", lazy)
	} else if big, ok := intOf(lazy); ok && big.Sign() > 0 {
		set(result, "cache_lazy_ttl", lazy)
	} else {
		set(result, "cache_lazy_ttl", I(0))
	}
	set(result, "remote_ttl", I(0))
	for _, item := range steps("remote_dns") {
		if exec := get(item, "exec"); isStr(exec) && ttlExecRE.MatchString(exec.Value) {
			set(result, "remote_ttl", &Node{Kind: exec.Kind, Tag: tagInt, Value: strings.TrimPrefix(exec.Value, "ttl ")})
			break
		}
	}
	return result
}

// pinnedChain is (upstream entry, possible sources) of a canonical pinned
// chain; nil when edited.
func pinnedChain(key string, plugins map[string]*Node) (*Node, []string) {
	chain, forward, cache := plugins[UpstreamPrefix+key], plugins[ForwardPrefix+key], plugins[CachePrefix+key]
	if !isMap(chain) || !equal(get(chain, "type"), S("sequence")) || !isSeq(get(chain, "args")) {
		return nil, nil
	}
	steps := append([]*Node{}, get(chain, "args").Content...)
	if len(steps) == 0 || !same(steps[0], M("exec", "drop_resp")) || len(steps) < 2 || !same(steps[len(steps)-1], M("exec", "accept")) {
		return nil, nil
	}
	steps = steps[1 : len(steps)-1]
	var wrappers []string
	for len(steps) > 0 && isMap(steps[0]) && len(steps[0].Content) == 2 && has(steps[0], "exec") &&
		isStr(get(steps[0], "exec")) && strings.HasPrefix(get(steps[0], "exec").Value, "$") &&
		containsString(RealWrappers, get(steps[0], "exec").Value[1:]) {
		wrappers = append(wrappers, get(steps[0], "exec").Value[1:])
		steps = steps[1:]
	}
	var ordered []string
	for _, name := range RealWrappers {
		if containsString(wrappers, name) {
			ordered = append(ordered, name)
		}
	}
	if strings.Join(wrappers, ",") != strings.Join(ordered, ",") {
		return nil, nil
	}
	if len(steps) > 0 && same(steps[0], M("exec", "$"+CachePrefix+key)) {
		steps = steps[1:]
		args := get(cache, "args")
		if !isMap(cache) || !equal(get(cache, "type"), S("cache")) {
			args = nil
		}
		lazy := get(args, "lazy_cache_ttl")
		lazyOK := false
		if v, ok := intOf(lazy); ok && v.Sign() > 0 {
			lazyOK = same(args, M("size", upstreamCacheSize, "lazy_cache_ttl", lazy, "short_circuit", true))
		}
		if !isMap(args) || !(same(args, M("size", upstreamCacheSize, "short_circuit", true)) || lazyOK) {
			return nil, nil
		}
	}
	ttl := false
	if n := len(steps); n > 0 && isMap(steps[n-1]) && len(steps[n-1].Content) == 2 && isStr(get(steps[n-1], "exec")) && ttlExecRE.MatchString(get(steps[n-1], "exec").Value) {
		ttl, steps = true, steps[:n-1]
	}
	if !same(seqOf(steps), L(M("exec", "$"+ForwardPrefix+key), M("matches", L("!has_resp"), "exec", "reject 3"))) {
		return nil, nil
	}
	args := get(forward, "args")
	if !isMap(forward) || !equal(get(forward, "type"), S("forward")) {
		args = nil
	}
	upstreams := get(args, "upstreams")
	if !isMap(args) || mapSize(args) != 1 || !has(args, "upstreams") || !isSeq(upstreams) || len(upstreams.Content) != 1 ||
		!isMap(upstreams.Content[0]) || strings.TrimSpace(str(get(upstreams.Content[0], "addr"))) == "" {
		return nil, nil
	}
	if len(wrappers) > 0 && ttl {
		return nil, nil
	}
	sources := UpstreamSources
	if len(wrappers) > 0 {
		sources = []string{"local"}
	} else if ttl {
		sources = []string{"remote", "fallback"}
	}
	return upstreams.Content[0], sources
}

// pinnedSource is _source: the source reproducing the tag's hash, else the
// first one whose chain the current features would generate in this shape.
func pinnedSource(key string, entry *Node, sources []string, plugins map[string]*Node) string {
	for _, item := range sources {
		if EntryKey(entry, item) == key {
			return item
		}
	}
	// Native JSON identities intentionally differ at the switch (design D2).
	// A legacy JS-separator escape can lose the original source's hash. Do
	// not infer a different source from a shape remote and fallback share.
	if hasJSONSeparator(entry) {
		return ""
	}
	if len(sources) == 1 {
		return sources[0]
	}
	if containsString(sources, "local") {
		features := chainFeatures(plugins)
		hooks := false
		for _, name := range RealWrappers {
			hooks = hooks || sectionEnabled(features, name)
		}
		var kept []string
		for _, item := range sources {
			if !(item == "local" && hooks || item != "local" && (positive(get(features, "remote_ttl")))) {
				kept = append(kept, item)
			}
		}
		sources = kept
	}
	if len(sources) == 0 {
		return ""
	}
	return sources[0]
}

func hasJSONSeparator(n *Node) bool {
	if n == nil {
		return false
	}
	if isStr(n) && strings.ContainsAny(n.Value, "\u2028\u2029") {
		return true
	}
	for _, child := range n.Content {
		if hasJSONSeparator(child) {
			return true
		}
	}
	return false
}

// parseExec is the route of one sequence_main rule-group exec ("" when it
// is not a rule DNS action; invalid when an owned helper was hand-edited).
func parseExec(value *Node, part string, plugins map[string]*Node) string {
	if !isStr(value) {
		return ""
	}
	for route, target := range gotoRoutes[part] {
		if value.Value == target {
			return route
		}
	}
	words := strings.Fields(value.Value)
	if len(words) > 0 && strings.ToLower(words[0]) == "reject" && len(words) <= 2 {
		code := ""
		if len(words) == 2 {
			code = strings.ToLower(words[1])
		}
		return rejectRoutes[code]
	}
	if value.Value == "$"+BlackholeTag {
		if same(plugins[BlackholeTag], blackholeNode) {
			return "blackhole"
		}
		return invalidRoute(blackholeError)
	}
	if rest := strings.TrimPrefix(value.Value, "goto "+UpstreamPrefix); rest != value.Value && keyRE.MatchString(rest) {
		entry, sources := pinnedChain(rest, plugins)
		if entry == nil {
			return invalidRoute(chainReason(UpstreamPrefix + rest))
		}
		source := pinnedSource(rest, entry, sources, plugins)
		if source == "" {
			if hasJSONSeparator(entry) {
				return invalidRoute(messageDNSUpstreamKeyChanged)
			}
			return invalidRoute(chainReason(UpstreamPrefix + rest))
		}
		return "upstream:" + EntryKey(entry, source)
	}
	return ""
}

// pinnedAction is the stored action of a recognized goto cf_dns_up_<key>.
func pinnedAction(key string, plugins map[string]*Node) *Node {
	route := parseExec(S("goto "+UpstreamPrefix+key), "domain", plugins)
	if route == "" || isInvalid(route) {
		return nil
	}
	entry, sources := pinnedChain(key, plugins)
	for _, item := range sources {
		if "upstream:"+EntryKey(entry, item) == route {
			return M("action", "upstream", "source", item, "upstream", clone(entry))
		}
	}
	return nil
}

// policyDNSRoutes maps each proxy group to the DNS route of its configured
// default (manual select groups only; everything else stays proxy).
func policyDNSRoutes(p *Profile) map[string]string {
	groups := map[string]*Node{}
	byID := map[string]*Node{}
	var names []string
	for _, group := range items(p.get("proxy_groups")) {
		if name, ok := get(group, "name"), isMap(group); ok && isStr(name) {
			if groups[name.Value] == nil {
				names = append(names, name.Value)
			}
			groups[name.Value] = group
		}
	}
	for _, name := range names {
		if id := get(groups[name], "id"); isStr(id) {
			byID[id.Value] = groups[name]
		}
	}
	resolved := map[string]string{}
	var resolve func(name string, visiting map[string]bool) string
	resolve = func(name string, visiting map[string]bool) string {
		if name == "DIRECT" {
			return "direct"
		}
		if containsString(nonProxyPolicies, name) {
			return ""
		}
		if route, ok := resolved[name]; ok {
			return route
		}
		group := groups[name]
		if group == nil || has(group, "enabled") && !(isTrue(get(group, "enabled"))) || equal(get(group, "type"), S("chain")) ||
			visiting[name] || len(visiting) >= 64 {
			return "proxy"
		}
		source := group
		if follow := get(group, "follow_group"); str(follow) != "" {
			source = byID[str(follow)]
			if !isStr(follow) || source == nil || (str(get(source, "follow_group")) != "") {
				return "proxy"
			}
		}
		if !equal(get(source, "type"), S("select")) {
			return "proxy"
		}
		emitted := p.renderMembers(source)
		if strings.Contains(str(get(emitted, "filter")), "`") || (str(get(source, "exclude-filter")) != "") || (str(get(source, "exclude-type")) != "") {
			return "proxy"
		}
		route := "proxy"
		if candidates := items(get(emitted, "proxies")); len(candidates) > 0 {
			next := map[string]bool{name: true}
			for k := range visiting {
				next[k] = true
			}
			route = resolve(str(candidates[0]), next)
		}
		resolved[name] = route
		return route
	}
	out := map[string]string{}
	for _, name := range names {
		out[name] = resolve(name, map[string]bool{})
	}
	return out
}

// followRoute is the DNS route implied by a rule's policy ("" = none).
func followRoute(item *Node, routes map[string]string) string {
	if !convertible(item) {
		return ""
	}
	policy := get(item, "policy")
	if equal(policy, S("DIRECT")) {
		return "direct"
	}
	if !isStr(policy) || policy.Value == "" || containsString(nonProxyPolicies, policy.Value) {
		return ""
	}
	if route, ok := routes[policy.Value]; ok {
		return route
	}
	return "proxy"
}

// convertible: the rule can take part in DNS routing.
func convertible(item *Node) bool {
	if isOneOf(get(item, "itemType"), "ruleset") {
		return !isOneOf(get(item, "behavior"), "ipcidr")
	}
	return isOneOf(get(item, "rule_type"), "DOMAIN-SUFFIX", "DOMAIN", "DOMAIN-KEYWORD", "DOMAIN-REGEX")
}

// hasIPPart: the source has IP entries; a classical set only when its known
// content has IP lines (p nil keeps the old assumption).
func hasIPPart(item *Node, p *Profile) bool {
	if isOneOf(get(item, "itemType"), "ruleset") {
		behavior := get(item, "behavior")
		if !isOneOf(behavior, "classical") || p == nil {
			return isOneOf(behavior, "ipcidr", "classical")
		}
		return !p.classicalWithoutIP(item)
	}
	return isOneOf(get(item, "rule_type"), "IP-CIDR", "IP-CIDR6")
}

func defaultRoute(defaultForward string) string {
	if defaultForward == "forward_local" {
		return "direct"
	}
	return "proxy"
}

// probeNeeded: an IP group routes away from the fallback.
func probeNeeded(routes []string, defaultForward string) bool {
	fallback := defaultRoute(defaultForward)
	for _, route := range routes {
		if route != fallback {
			return true
		}
	}
	return false
}

func itoa(i int) string { return strconv.Itoa(i) }
