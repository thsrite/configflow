package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"configflow/internal/jsonx"
)

// The parts of backend/common/rule_dns.py (module C27) that the settings
// normalizer (C05) and the template converter (C22) call. C27 adds the rest
// of rule_dns.py to this file.

var (
	// Actions is ACTIONS: the explicit rule DNS actions.
	Actions = []string{"refused", "nxdomain", "nodata", "blackhole", "upstream"}
	// ListRoutes is LIST_ROUTES: the routes of the four selector lists.
	ListRoutes = []string{"direct", "proxy"}
	// UpstreamSources is UPSTREAM_SOURCES.
	UpstreamSources = []string{"local", "remote", "fallback"}
	// InlineExec is INLINE_EXEC: rejections written inline in sequence_main.
	InlineExec = map[string]string{"refused": "reject", "nxdomain": "reject 3", "nodata": "reject 0"}
	// RouteLabels is ROUTE_LABELS.
	RouteLabels = map[string]string{"direct": "直连 DNS", "proxy": "代理 DNS", "refused": "拒绝", "nxdomain": "不存在",
		"nodata": "空应答", "blackhole": "黑洞", "upstream": "指定上游"}
	// RealWrappers is REAL_WRAPPERS: response hooks in china_dns order.
	RealWrappers = []string{"ip_selector", "ros_address_list", "ros_route"}
	// IPRuleTypes is IP_RULE_TYPES.
	IPRuleTypes = []string{"IP-CIDR", "IP-CIDR6"}

	ruleGoto = map[string]map[string]string{
		"domain": {"direct": "goto china_dns", "proxy": "goto proxy_dns_seq"},
		"ip":     {"direct": "goto direct_ip", "proxy": "goto ip_requery_proxy"},
	}
	ruleKey = regexp.MustCompile(`^[0-9a-f]{12}$`)
)

const (
	// BlackholeTag is BLACKHOLE_TAG.
	BlackholeTag = "cf_dns_blackhole"
	// UpstreamPrefix is UPSTREAM_PREFIX.
	UpstreamPrefix = "cf_dns_up_"
	// ForwardPrefix is FORWARD_PREFIX.
	ForwardPrefix = "cf_dns_fwd_"
	// CachePrefix is CACHE_PREFIX.
	CachePrefix = "cf_dns_cache_"
	// UpstreamCacheSize is UPSTREAM_CACHE_SIZE.
	UpstreamCacheSize = 1024
)

// BlackholePlugin is BLACKHOLE_PLUGIN (a fresh copy). A bare YAML null is
// treated as nxdomain by OxiDNS; "null" must stay a string.
func BlackholePlugin() *jsonx.Object {
	return jsonx.Obj("tag", BlackholeTag, "type", "black_hole", "args", jsonx.Obj("mode", "null", "short_circuit", true))
}

// EntryKey is entry_key(upstream, source): 12 hex digits naming one pinned
// upstream copy as chosen from one list.
func EntryKey(upstream any, source any) (key string, err error) {
	defer catch(&err)
	return entryKey(upstream, source), nil
}

func entryKey(upstream any, source any) string {
	text, err := jsonx.Encode(jsonx.Obj("source", source, "upstream", upstream), jsonx.Options{SortKeys: true, Compact: true})
	if err != nil {
		raise(pyErr("TypeError", err.Error()))
	}
	sum := sha256.Sum256(text)
	return hex.EncodeToString(sum[:])[:12]
}

// RouteKey is route_key(action): the route string of a stored action object
// (also accepts parked direct/proxy).
func RouteKey(action any) (route string, err error) {
	defer catch(&err)
	return routeKey(action), nil
}

func routeKey(action any) string {
	name := item(action, "action")
	if name == "upstream" {
		return "upstream:" + entryKey(item(action, "upstream"), item(action, "source"))
	}
	text, ok := name.(string)
	if !ok {
		// Python returns the value itself; only strings reach here from
		// normalized settings.
		raise(pyErr("TypeError", "route is not a string: "+jsonx.Repr(name)))
	}
	return text
}

// RouteExec is route_exec(route, part): the sequence_main exec for a rule
// group.
func RouteExec(route any, part string) (exec string, err error) {
	defer catch(&err)
	return routeExec(route, part), nil
}

func routeExec(route any, part string) string {
	text, isText := route.(string)
	if isText && strIn(text, ListRoutes...) {
		target, ok := ruleGoto[part]
		if !ok {
			raise(pyErr("KeyError", jsonx.Repr(part)))
		}
		return target[text]
	}
	if exec, ok := InlineExec[text]; isText && ok {
		return exec
	}
	if text == "blackhole" {
		return "$" + BlackholeTag
	}
	if isText && strings.HasPrefix(text, "upstream:") && ruleKey.MatchString(text[9:]) {
		return "goto " + UpstreamPrefix + text[9:]
	}
	fail("不支持的 DNS 处理：" + str(route))
	return ""
}

// NormalizeAction is normalize_action(value, parked=parked): the validated
// canonical copy of one rule_actions (or rule_dns_parked) value.
func NormalizeAction(value any, parked bool) (action *jsonx.Object, err error) {
	defer catch(&err)
	return normalizeAction(value, parked), nil
}

func normalizeAction(value any, parked bool) *jsonx.Object {
	allowed := Actions
	if parked {
		allowed = append(append([]string{}, ListRoutes...), Actions...)
	}
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil || !strIn(object.Value("action"), allowed...) {
		fail("规则 DNS 处理无效：" + strings.Join(allowed, "、"))
	}
	name := object.GetString("action")
	if name != "upstream" {
		if !exactKeys(object, "action") {
			fail("规则 DNS 处理「" + RouteLabels[name] + "」不能包含其他参数")
		}
		return jsonx.Obj("action", name)
	}
	if !exactKeys(object, "action", "source", "upstream") || !strIn(object.Value("source"), UpstreamSources...) {
		fail("指定上游需要 source（local、remote 或 fallback）和 upstream")
	}
	upstream, ok := object.Value("upstream").(*jsonx.Object)
	if !ok || upstream == nil {
		fail("指定上游需要非空的 addr 地址")
	}
	addr, ok := upstream.Value("addr").(string)
	if !ok || strip(addr) == "" {
		fail("指定上游需要非空的 addr 地址")
	}
	return jsonx.Obj("action", "upstream", "source", object.Value("source"), "upstream", upstream.DeepCopy())
}

// exactKeys is set(obj) == {keys...}.
func exactKeys(obj *jsonx.Object, keys ...string) bool {
	if obj.Len() != len(keys) {
		return false
	}
	for _, key := range keys {
		if !obj.Has(key) {
			return false
		}
	}
	return true
}

// enabled is _enabled(settings, name).
func enabled(settings any, name string) bool {
	section := get(settings, name, nil)
	return isDict(section) && get(section, "enabled", nil) == true
}

// upstreamChain is upstream_chain(action, settings): forward + optional
// cache + sequence behind one pinned upstream.
func upstreamChain(action any, settings any) []any {
	upstream, source := item(action, "upstream"), item(action, "source")
	key := entryKey(upstream, source)
	forward := ForwardPrefix + key
	plugins := []any{jsonx.Obj("tag", forward, "type", "forward", "args", jsonx.Obj("upstreams", []any{copyValue(upstream)}))}
	steps := []any{jsonx.Obj("exec", "drop_resp")}
	if source == "local" {
		for _, name := range RealWrappers {
			if enabled(settings, name) {
				steps = append(steps, jsonx.Obj("exec", "$"+name))
			}
		}
	}
	if truthy(get(settings, "cache_enabled", nil)) {
		cache := jsonx.Obj("size", UpstreamCacheSize)
		// OxiDNS rejects an explicit zero; omitting the field disables lazy cache.
		if greaterThanZero(get(settings, "cache_lazy_ttl", jsonx.Int(0))) {
			cache.Set("lazy_cache_ttl", item(settings, "cache_lazy_ttl"))
		}
		cache.Set("short_circuit", true)
		plugins = append(plugins, jsonx.Obj("tag", CachePrefix+key, "type", "cache", "args", cache))
		steps = append(steps, jsonx.Obj("exec", "$"+CachePrefix+key))
	}
	steps = append(steps, jsonx.Obj("exec", "$"+forward), jsonx.Obj("matches", []any{"!has_resp"}, "exec", "reject 3"))
	if source != "local" && truthy(get(settings, "remote_ttl", nil)) {
		steps = append(steps, jsonx.Obj("exec", "ttl "+str(item(settings, "remote_ttl"))))
	}
	steps = append(steps, jsonx.Obj("exec", "accept"))
	plugins = append(plugins, jsonx.Obj("tag", UpstreamPrefix+key, "type", "sequence", "args", steps))
	return plugins
}

// greaterThanZero is `value > 0` for the normalized (int) setting.
func greaterThanZero(value any) bool {
	if b, isBool := value.(bool); isBool {
		return b
	}
	n, ok := value.(jsonx.Number)
	if !ok {
		raise(pyErr("TypeError", "'>' not supported between instances of '"+typeName(value)+"' and 'int'"))
	}
	return n.Float64() > 0
}

// HelperPlugins is helper_plugins(routes, actions, settings): the owned
// plugins needed by the given group routes, in first-use order. actions is
// rule_actions (id -> action, nil for None).
func HelperPlugins(routes []string, actions *jsonx.Object, settings *jsonx.Object) (plugins []any, err error) {
	defer catch(&err)
	return helperPlugins(routes, actions, settings), nil
}

func helperPlugins(routes []string, actions *jsonx.Object, settings any) []any {
	pinned := map[string]any{}
	for _, action := range actions.Values() {
		if get(action, "action", nil) == "upstream" {
			key := routeKey(action)
			if _, ok := pinned[key]; !ok {
				pinned[key] = action
			}
		}
	}
	result := []any{}
	seen := map[string]bool{}
	for _, route := range routes {
		if seen[route] {
			continue
		}
		seen[route] = true
		if route == "blackhole" {
			result = append(result, BlackholePlugin())
		} else if strings.HasPrefix(route, "upstream:") {
			action, ok := pinned[route]
			if !ok {
				fail("指定上游缺少上游地址，请重新选择")
			}
			result = append(result, upstreamChain(action, settings)...)
		}
	}
	return result
}

// Convertible is convertible(item): whether a rule can take part in DNS
// routing at all.
func Convertible(item *jsonx.Object) bool {
	if item.Value("itemType") == "ruleset" {
		return !jsonx.Equal(item.GetOr("behavior", "domain"), "ipcidr")
	}
	return strIn(item.Value("rule_type"), dnsRuleTypes()...)
}

// dnsRuleTypes is rule_dns.DNS_RULE_TYPES: MosDNS's single-rule types
// without the IP ones.
func dnsRuleTypes() []string {
	var out []string
	for _, kind := range mosdnsRuleTypes() {
		if !strIn(kind, IPRuleTypes...) {
			out = append(out, kind)
		}
	}
	return out
}

// hasIPPart is has_ip_part(item, config): whether the source has IP entries.
// config nil is None.
func hasIPPart(item any, config any) bool {
	if get(item, "itemType", nil) == "ruleset" {
		behavior := get(item, "behavior", "domain")
		if behavior != "classical" || config == nil {
			return strIn(behavior, "ipcidr", "classical")
		}
		return classicalIPLines(item, config) != false
	}
	return strIn(get(item, "rule_type", nil), IPRuleTypes...)
}
