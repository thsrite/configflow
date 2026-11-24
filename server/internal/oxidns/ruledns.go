package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/model"

	"gopkg.in/yaml.v3"
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

// RuleAction is one stored rule DNS action: its kind and, for a pinned
// upstream, the upstream list it was picked from and the native forward
// upstream entry, kept as written (unknown members included).
type RuleAction struct {
	Action   string
	Source   string
	Upstream *Node
	raw      *Node
}

// actionOf reads a stored action; members of the wrong type read as empty.
func actionOf(n *Node) RuleAction {
	return RuleAction{Action: str(get(n, "action")), Source: str(get(n, "source")), Upstream: get(n, "upstream")}
}

// Key is the route of the action.
func (a RuleAction) Key() string {
	if a.Action == "upstream" {
		return "upstream:" + EntryKey(a.Upstream, a.Source)
	}
	return a.Action
}

// toNode is the canonical stored form of a normalized action.
func (a RuleAction) toNode() *Node {
	if a.raw != nil {
		return clone(a.raw)
	}
	if a.Action != "upstream" {
		return M("action", a.Action)
	}
	return M("action", "upstream", "source", a.Source, "upstream", clone(a.Upstream))
}

// RouteKey is the route of a stored action.
func RouteKey(action *Node) string { return actionOf(action).Key() }

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
	action, err := normalizeAction(value, parked)
	if err != nil {
		return nil, err
	}
	return action.toNode(), nil
}

func normalizeAction(value *Node, parked bool) (RuleAction, error) {
	allowed := Actions
	if parked {
		allowed = append(append([]string{}, ListRoutes...), Actions...)
	}
	action := actionOf(value)
	if !isMap(value) || !slices.Contains(allowed, action.Action) {
		return RuleAction{}, fail("规则 DNS 处理无效：" + strings.Join(allowed, "、"))
	}
	if action.Action != "upstream" {
		if mapSize(value) != 1 {
			return RuleAction{}, fail("规则 DNS 处理「" + RouteLabels[action.Action] + "」不能包含其他参数")
		}
		return RuleAction{Action: action.Action}, nil
	}
	if mapSize(value) != 3 || !has(value, "source") || !has(value, "upstream") || !slices.Contains(UpstreamSources, action.Source) {
		return RuleAction{}, fail("指定上游需要 source（local、remote 或 fallback）和 upstream")
	}
	if !isMap(action.Upstream) || strings.TrimSpace(str(get(action.Upstream, "addr"))) == "" {
		return RuleAction{}, fail("指定上游需要非空的 addr 地址")
	}
	return action, nil
}

// chainFeatures are the china_dns response hooks and the cache and TTL
// settings a pinned upstream chain copies (from template settings, or read
// back from a native document's plugins).
type chainFeatures struct {
	hooks     map[string]bool // enabled RealWrappers
	cache     bool
	lazyTTL   *big.Int // lazy_cache_ttl when positive
	remoteTTL string   // positive remote TTL digits, "" for none
}

func (f chainFeatures) anyHook() bool {
	for _, name := range RealWrappers {
		if f.hooks[name] {
			return true
		}
	}
	return false
}

// features are the template's chain features.
func (s *templateSettings) features() chainFeatures {
	f := chainFeatures{hooks: map[string]bool{"ip_selector": s.IPSelector.Enabled, "ros_address_list": s.ROSAddressList.Enabled, "ros_route": s.ROSRoute.Enabled},
		cache: s.CacheEnabled}
	if s.CacheLazyTTL > 0 {
		f.lazyTTL = big.NewInt(s.CacheLazyTTL)
	}
	if s.RemoteTTL > 0 {
		f.remoteTTL = strconv.FormatInt(s.RemoteTTL, 10)
	}
	return f
}

// upstreamChain is forward + optional cache + sequence behind one pinned upstream.
func upstreamChain(action RuleAction, features chainFeatures) []*Node {
	source := action.Source
	key := action.Key()[len("upstream:"):]
	plugins := []*Node{M("tag", ForwardPrefix+key, "type", "forward", "args", M("upstreams", L(clone(action.Upstream))))}
	steps := L(M("exec", "drop_resp"))
	if source == "local" {
		for _, name := range RealWrappers {
			if features.hooks[name] {
				steps.Content = append(steps.Content, M("exec", "$"+name))
			}
		}
	}
	if features.cache {
		cache := M("size", upstreamCacheSize)
		if features.lazyTTL != nil {
			set(cache, "lazy_cache_ttl", &Node{Kind: yaml.ScalarNode, Tag: tagInt, Value: features.lazyTTL.String()})
		}
		set(cache, "short_circuit", B(true))
		plugins = append(plugins, M("tag", CachePrefix+key, "type", "cache", "args", cache))
		steps.Content = append(steps.Content, M("exec", "$"+CachePrefix+key))
	}
	steps.Content = append(steps.Content, M("exec", "$"+ForwardPrefix+key), M("matches", L("!has_resp"), "exec", "reject 3"))
	if source != "local" && features.remoteTTL != "" {
		steps.Content = append(steps.Content, M("exec", "ttl "+features.remoteTTL))
	}
	steps.Content = append(steps.Content, M("exec", "accept"))
	return append(plugins, M("tag", UpstreamPrefix+key, "type", "sequence", "args", steps))
}

func helperPlugins(routes []string, actions *actionMap, features chainFeatures) ([]*Node, error) {
	pinned := map[string]RuleAction{}
	if actions != nil {
		for _, id := range actions.order {
			action := actions.items[id]
			if action.Action == "upstream" {
				if _, seen := pinned[action.Key()]; !seen {
					pinned[action.Key()] = action
				}
			}
		}
	}
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
			action, ok := pinned[route]
			if !ok {
				return nil, fail("指定上游缺少上游地址，请重新选择")
			}
			result = append(result, upstreamChain(action, features)...)
		}
	}
	return result, nil
}

// pluginMap is tag → plugin of a document's plugins; nil when the list is
// not a clean list of uniquely tagged mappings (routing's _plugins).
func pluginMap(document *nativeDoc) map[string]*Node {
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

// documentFeatures reads the china_dns / remote_dns features a pinned chain
// copies from a native document's plugins.
func documentFeatures(plugins map[string]*Node) chainFeatures {
	args := func(tag string) *Node { return get(plugins[tag], "args") }
	steps := func(tag string) []*Node {
		return filter(items(args(tag)), isMap)
	}
	china := seqOf(steps("china_dns"))
	result := chainFeatures{hooks: map[string]bool{}}
	for _, name := range RealWrappers {
		result.hooks[name] = contains(china, M("exec", "$"+name))
	}
	cache := plugins["lazy_cache"]
	result.cache = contains(china, M("exec", "$lazy_cache")) && isMap(cache) && equal(get(cache, "type"), S("cache"))
	if lazy, ok := intOf(get(args("lazy_cache"), "lazy_cache_ttl")); ok && lazy.Sign() > 0 {
		result.lazyTTL = lazy
	}
	for _, item := range steps("remote_dns") {
		if exec := get(item, "exec"); isStr(exec) && ttlExecRE.MatchString(exec.Value) {
			result.remoteTTL = strings.TrimPrefix(exec.Value, "ttl ")
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
		features := documentFeatures(plugins)
		hooks := features.anyHook()
		var kept []string
		for _, item := range sources {
			if !(item == "local" && hooks || item != "local" && features.remoteTTL != "") {
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

// pinnedRuleAction is the typed action of a recognized goto cf_dns_up_<key>.
func pinnedRuleAction(key string, plugins map[string]*Node) (RuleAction, bool) {
	route := parseExec(S("goto "+UpstreamPrefix+key), "domain", plugins)
	if route == "" || isInvalid(route) {
		return RuleAction{}, false
	}
	entry, sources := pinnedChain(key, plugins)
	for _, item := range sources {
		if "upstream:"+EntryKey(entry, item) == route {
			return RuleAction{Action: "upstream", Source: item, Upstream: clone(entry)}, true
		}
	}
	return RuleAction{}, false
}

// pinnedAction is the stored action of a recognized goto cf_dns_up_<key>.
func pinnedAction(key string, plugins map[string]*Node) *Node {
	action, ok := pinnedRuleAction(key, plugins)
	if !ok {
		return nil
	}
	return action.toNode()
}

// policyDNSRoutes maps each proxy group to the DNS route of its configured
// default (manual select groups only; everything else stays proxy).
func policyDNSRoutes(p *Profile) map[string]string {
	groups := map[string]model.ProxyGroup{}
	byID := map[string]model.ProxyGroup{}
	var names []string
	for _, group := range p.source.Profile.ProxyGroups {
		if _, seen := groups[group.Name]; !seen {
			names = append(names, group.Name)
		}
		groups[group.Name] = group
	}
	for _, name := range names {
		if id := groups[name].ID; id != "" {
			byID[id] = groups[name]
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
		group, ok := groups[name]
		if !ok || !model.Flag(group.Enabled, true) || group.Type == "chain" || visiting[name] || len(visiting) >= 64 {
			return "proxy"
		}
		source := group
		if follow := model.TextValue(group.FollowGroup); follow != "" {
			if source, ok = byID[follow]; !ok || model.TextValue(source.FollowGroup) != "" {
				return "proxy"
			}
		}
		if source.Type != "select" {
			return "proxy"
		}
		emitted := p.renderMembers(source)
		if strings.Contains(str(get(emitted, "filter")), "`") || model.TextValue(source.ExcludeFilter) != "" || model.TextValue(source.ExcludeType) != "" {
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
func followRoute(item model.RuleConfig, routes map[string]string) string {
	if !convertible(item) {
		return ""
	}
	policy := model.TextValue(item.Policy)
	if policy == "DIRECT" {
		return "direct"
	}
	if policy == "" || containsString(nonProxyPolicies, policy) {
		return ""
	}
	if route, ok := routes[policy]; ok {
		return route
	}
	return "proxy"
}

// convertible: the rule can take part in DNS routing.
func convertible(item model.RuleConfig) bool {
	if item.ItemType == "ruleset" {
		return model.TextValue(item.Behavior) != "ipcidr"
	}
	switch item.RuleType {
	case "DOMAIN-SUFFIX", "DOMAIN", "DOMAIN-KEYWORD", "DOMAIN-REGEX":
		return true
	}
	return false
}

// hasIPPart: the source has IP entries; a classical set only when its known
// content has IP lines (p nil keeps the old assumption).
func hasIPPart(item model.RuleConfig, p *Profile) bool {
	if item.ItemType != "ruleset" {
		return IsIPRuleType(item.RuleType)
	}
	switch behavior := model.TextValue(item.Behavior); {
	case behavior == "classical" && p != nil:
		return !p.classicalWithoutIP(item)
	default:
		return behavior == "ipcidr" || behavior == "classical"
	}
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
