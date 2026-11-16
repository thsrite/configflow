package oxidns

import (
	"math/big"
	"regexp"
	"sort"
	"strings"
	"sync"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
)

// Port of backend/common/oxidns_common.py (module C24): the common OxiDNS
// controls, a projection of the current native draft. Only the requested
// fields are patched; ambiguous or custom execution paths stay editable in
// the complete editor and are reported as not editable here.
//
// The functions of later modules it calls (prepare_editor, read_routing,
// apply_routing, owned_chains, resource_downloads, refresh_rule_helpers,
// prepare_rule_update, finish_rule_updates) are the variables of
// pending.go.

var (
	features      = []string{"query_recorder", "ip_selector", "ros_address_list", "ros_route"}
	wrappers      = []string{"china_dns", "direct_ip", "default_direct_ip"}
	chainWrappers = []string{"ip_selector", "ros_address_list", "ros_route"}
)

// chainFields is _CHAIN_FIELDS: changes that rebuild the pinned upstream
// chains of rule DNS.
var chainFields = map[string]bool{
	"cache_enabled": true, "cache_lazy_ttl": true, "remote_ttl": true,
	"ip_selector.enabled": true, "ros_address_list.enabled": true, "ros_route.enabled": true,
}

// directField is one entry of _DIRECT: tag "" (hasTag false) is the
// document itself.
type directField struct {
	field  string
	tag    string
	hasTag bool
	path   []string
}

type apiField struct {
	field string
	path  []string
}

var apiFields = []apiField{
	{"api.listen", []string{"listen"}}, {"api.username", []string{"auth", "username"}},
	{"api.password", []string{"auth", "password"}}, {"api.webui_root", []string{"webui", "root"}},
	{"api.ssl.cert", []string{"ssl", "cert"}}, {"api.ssl.key", []string{"ssl", "key"}},
}

var commonTables struct {
	once     sync.Once
	defaults *jsonx.Object
	direct   []directField
	byField  map[string]directField
	api      map[string][]string
	fields   map[string]bool
}

// commonInit builds _DEFAULTS, _DIRECT, _API and COMMON_FIELDS.
func commonInit() {
	commonTables.once.Do(func() {
		defaults := DefaultSettings()
		commonTables.defaults = defaults
		var direct []directField
		add := func(field, tag string, path ...string) {
			direct = append(direct, directField{field, tag, tag != "", path})
		}
		add("worker_threads", "", "runtime", "worker_threads")
		add("log.level", "", "log", "level")
		add("log.file", "", "log", "file")
		add("log.rotation", "", "log", "rotation", "type")
		add("log.max_files", "", "log", "rotation", "max_files")
		add("cache_size", "lazy_cache", "size")
		add("cache_lazy_ttl", "lazy_cache", "lazy_cache_ttl")
		add("cache_dump_file", "lazy_cache", "dump_file")
		add("cache_dump_interval", "lazy_cache", "dump_interval")
		add("probe_cache_size", "local_probe_cache", "size")
		add("fallback_threshold", "fallback", "threshold")
		add("always_standby", "fallback", "always_standby")
		for _, name := range []string{"local", "remote", "fallback"} {
			add(name+"_concurrent", "forward_"+name, "concurrent")
		}
		for _, feature := range features {
			for _, field := range leaves(defaults.GetObject(feature).GetObject("args"), "") {
				add(feature+".args."+field, feature, strings.Split(field, ".")...)
			}
		}
		for _, pair := range [][2]string{{"args", "dynamic_domains"}, {"learn_args", "learn_classified_domain"}} {
			for _, field := range leaves(defaults.GetObject("dynamic_domain_set").GetObject(pair[0]), "") {
				add("dynamic_domain_set."+pair[0]+"."+field, pair[1], strings.Split(field, ".")...)
			}
		}
		for _, feature := range []string{"ros_address_list", "ros_route"} {
			for _, field := range []string{"tls", "tls.server_name", "tls.ca", "tls.insecure"} {
				add(feature+".args."+field, feature, strings.Split(field, ".")...)
			}
		}
		for _, field := range []string{"timeout", "outbound", "socks5"} {
			add("rule_update.args."+field, "rules_download", field)
		}
		// A later add of the same field replaces the earlier one in place
		// (dict assignment).
		byField := map[string]directField{}
		var ordered []directField
		for _, entry := range direct {
			if _, ok := byField[entry.field]; ok {
				for i := range ordered {
					if ordered[i].field == entry.field {
						ordered[i] = entry
					}
				}
			} else {
				ordered = append(ordered, entry)
			}
			byField[entry.field] = entry
		}
		commonTables.direct = ordered
		commonTables.byField = byField
		commonTables.api = map[string][]string{}
		for _, entry := range apiFields {
			commonTables.api[entry.field] = entry.path
		}
		fields := map[string]bool{}
		for field := range byField {
			fields[field] = true
		}
		for field := range commonTables.api {
			fields[field] = true
		}
		for _, field := range []string{"listen", "tcp_enabled", "prefer_ipv4", "metrics_enabled", "block_qtypes", "remote_ttl",
			"local_dns", "remote_dns", "fallback_dns", "cache_enabled", "cache_dump_enabled",
			"probe_cache_enabled", "api.enabled", "custom_hosts",
			"dynamic_domain_set.enabled", "dynamic_domain_set.route", "dynamic_domain_set.learn",
			"adguard.enabled", "adguard.content", "adguard.urls", "adguard.mode",
			"rule_update.enabled", "rule_update.args.schedule", "rule_update.args.timezone"} {
			fields[field] = true
		}
		for _, name := range features {
			fields[name+".enabled"] = true
		}
		commonTables.fields = fields
	})
}

// CommonFields is COMMON_FIELDS: every field of the common settings form
// (routing fields are RoutingFields).
func CommonFields() map[string]bool {
	commonInit()
	out := make(map[string]bool, len(commonTables.fields))
	for field := range commonTables.fields {
		out[field] = true
	}
	return out
}

func commonDefaults() *jsonx.Object {
	commonInit()
	return commonTables.defaults
}

// leaves is _leaves(value): the dotted paths of the non-dict values.
func leaves(value *jsonx.Object, prefix string) []string {
	var out []string
	for key, element := range value.All() {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if obj, ok := element.(*jsonx.Object); ok {
			out = append(out, leaves(obj, path)...)
		} else {
			out = append(out, path)
		}
	}
	return out
}

// getPath is _get(obj, path): missingValue when a step is missing or not a
// dict.
func getPath(obj any, path []string) any {
	for _, key := range path {
		if !isDict(obj) {
			return missingValue
		}
		obj = get(obj, key, missingValue)
	}
	return obj
}

// putPath is _put(obj, path, value): nil (None) deletes, missing parents
// are created only for a value.
func putPath(obj any, path []string, value any) {
	for _, key := range path[:len(path)-1] {
		child := get(obj, key, missingValue)
		if child == missingValue || child == nil {
			if value == nil {
				return
			}
			setItem(obj, key, jsonx.NewObject())
		} else if !isDict(child) {
			fail("此参数包含自定义值类型，请在完整参数中编辑")
		}
		obj = item(obj, key)
	}
	last := path[len(path)-1]
	if value == nil {
		popKey(obj, last)
	} else {
		setItem(obj, last, copyValue(value))
	}
}

// setValue is _value(obj, field, value): projection assignment that keeps
// None.
func setValue(obj *jsonx.Object, field string, value any) {
	path := strings.Split(field, ".")
	for _, key := range path[:len(path)-1] {
		if !isDict(obj.Value(key)) {
			obj.Set(key, jsonx.NewObject())
		}
		obj = obj.Value(key).(*jsonx.Object)
	}
	obj.Set(path[len(path)-1], copyValue(value))
}

// matchesOf is _matches(item).
func matchesOf(entry any) []any {
	var value any = []any{}
	if isDict(entry) {
		value = get(entry, "matches", []any{})
	}
	switch x := value.(type) {
	case string:
		return []any{x}
	case []any:
		return x
	}
	return []any{}
}

// referenceCount is _reference_count(value, tag).
func referenceCount(value any, tag string) int {
	switch x := value.(type) {
	case *jsonx.Object:
		n := 0
		for _, element := range x.Values() {
			n += referenceCount(element, tag)
		}
		return n
	case []any:
		n := 0
		for _, element := range x {
			n += referenceCount(element, tag)
		}
		return n
	}
	if strIn(value, "$"+tag, "jump "+tag, "goto "+tag) {
		return 1
	}
	return 0
}

// tagMentions is _tag_mentions(value, tag).
func tagMentions(value any, tag string) int {
	switch x := value.(type) {
	case *jsonx.Object:
		n := 0
		for _, element := range x.Values() {
			n += tagMentions(element, tag)
		}
		return n
	case []any:
		n := 0
		for _, element := range x {
			n += tagMentions(element, tag)
		}
		return n
	case string:
		pattern, err := rules.CompilePyRegex(`(?<!\S)\$`+convert.PyReEscape(tag)+`(?!\S)`, false)
		must(err)
		if pattern.Search(x) || x == "jump "+tag || x == "goto "+tag {
			return 1
		}
	}
	return 0
}

// safeParent is _safe_parent(obj, path).
func safeParent(obj any, path []string) bool {
	for _, key := range path[:len(path)-1] {
		if obj == missingValue || obj == nil {
			return true
		}
		if !isDict(obj) {
			return false
		}
		obj = get(obj, key, missingValue)
	}
	return obj == missingValue || obj == nil || isDict(obj)
}

// capability is _cap(editable, reason, target, absent); reason "" and a
// falsy target are omitted.
func capability(editable bool, reason string, target any, absent bool) *jsonx.Object {
	out := jsonx.Obj("editable", editable)
	if reason != "" {
		out.Set("reason", reason)
	}
	if truthy(target) {
		out.Set("target", target)
	}
	if absent {
		out.Set("absent", true)
	}
	return out
}

// neutral is _neutral(value).
func neutral(value any) any {
	switch x := value.(type) {
	case *jsonx.Object:
		out := jsonx.NewObject()
		for key, element := range x.All() {
			out.Set(key, neutral(element))
		}
		return out
	case []any:
		return []any{}
	case bool:
		return false
	case string:
		return ""
	}
	return nil
}

// ownedChainSet is _owned_chains(document).
func ownedChainSet(document *jsonx.Object) map[string]bool {
	out := map[string]bool{}
	for _, tag := range ownedChains(document).Keys() {
		out[tag] = true
	}
	return out
}

// Fallback (proxy DNS backup) controls.

var (
	fallbackPlugins = []string{"forward_fallback", "fallback_dns", "fallback"}
	fallbackFields  = []string{"fallback_dns", "fallback_concurrent", "fallback_threshold", "always_standby"}
)

const (
	fallbackReason                 = "代理回退使用了自定义执行链、额外引用或 include，请在完整参数中调整，不能自动切换"
	fallbackDynamicReferenceReason = "执行链包含未展开的环境变量引用，请在完整参数中确认引用后调整备用 DNS"
)

func fallbackOn() []any {
	return []any{jsonx.Obj("exec", "$fallback"), jsonx.Obj("exec", "accept")}
}

func fallbackOff() []any { return []any{jsonx.Obj("exec", "goto remote_dns")} }

func fallbackSecondary() []any {
	return []any{jsonx.Obj("exec", "$forward_fallback"), jsonx.Obj("matches", []any{"!has_resp"}, "exec", "reject 3")}
}

// sequenceValue is ctx.sequence(tag) as a Python value (nil for None).
func (ctx *commonContext) sequenceValue(tag string) any {
	if list, ok := ctx.sequence(tag); ok {
		return list
	}
	return nil
}

// fallbackState is _fallback_state(ctx).
func fallbackState(ctx *commonContext) (bool, *jsonx.Object) {
	calls := ctx.sequenceValue("proxy_dns_seq")
	enabled := sameValue(calls, fallbackOn())
	for _, tag := range append(append([]string{}, fallbackPlugins...), "proxy_dns_seq", "remote_dns") {
		if alias := ctx.aliasCapability(tag); alias != nil {
			return enabled, alias
		}
	}
	denied := capability(false, fallbackReason, "proxy_dns_seq", false)
	if len(referenceLocations(ctx.doc, nil, true)) > 0 {
		return enabled, capability(false, fallbackDynamicReferenceReason, "proxy_dns_seq", false)
	}
	if truthy(ctx.doc.Value("include")) || ctx.sequenceValue("remote_dns") == nil {
		return enabled, denied
	}
	known := false
	for _, expected := range [][]any{fallbackOn(), fallbackOff(), {jsonx.Obj("exec", "$remote_dns"), jsonx.Obj("exec", "accept")}} {
		if sameValue(calls, expected) {
			known = true
			break
		}
	}
	if !known {
		return enabled, denied
	}
	for _, tag := range append(append([]string{}, fallbackPlugins...), "proxy_dns_seq", "remote_dns") {
		n := 0
		for _, element := range ctx.plugins() {
			if strIn(get(element, "tag", nil), tag) {
				n++
			}
		}
		if n > 1 {
			return enabled, denied
		}
	}
	kinds := map[string]string{"forward_fallback": "forward", "fallback_dns": "sequence", "fallback": "fallback"}
	for _, tag := range fallbackPlugins {
		var plugin any
		if p := ctx.plugin(tag); p != nil {
			plugin = p
		} else if enabled {
			return enabled, denied
		} else {
			plugin = get(ctx.parked, tag, nil)
		}
		if plugin == nil {
			continue
		}
		if !isDict(plugin) || !strIn(get(plugin, "tag", nil), tag) || !strIn(get(plugin, "type", nil), kinds[tag]) {
			return enabled, denied
		}
		args := get(plugin, "args", nil)
		if tag == "fallback_dns" {
			if !sameValue(args, fallbackSecondary()) {
				return enabled, denied
			}
		} else if !isDict(args) {
			return enabled, denied
		} else if tag == "fallback" && (!strIn(get(args, "primary", nil), "remote_dns") || !strIn(get(args, "secondary", nil), "fallback_dns")) {
			return enabled, denied
		} else if tag == "forward_fallback" {
			if _, ok := get(args, "upstreams", []any{}).([]any); !ok {
				return enabled, denied
			}
		}
	}
	remaining := copyValue(ctx.doc).(*jsonx.Object)
	for _, element := range iterate(get(remaining, "plugins", []any{})) {
		tag := popKey(element, "tag")
		popKey(element, "type")
		switch {
		case strIn(tag, "proxy_dns_seq"), strIn(tag, "fallback_dns"):
			popKey(element, "args")
		case strIn(tag, "fallback") && isDict(get(element, "args", nil)):
			popKey(get(element, "args", nil), "primary")
			popKey(get(element, "args", nil), "secondary")
		}
	}
	for _, tag := range fallbackPlugins {
		if references(remaining, tag) {
			return enabled, denied
		}
	}
	return enabled, capability(true, "", "proxy_dns_seq", false)
}

// loadUpstreams is the upstream YAML of a DNS field: yaml.safe_load(value)
// if value and value.strip() else [].
func loadUpstreams(value any) any {
	if !truthy(value) || strip(value) == "" {
		return []any{}
	}
	parsed, ok := safeLoadKeep(value.(string), "DNS 上游")
	if !ok {
		fail("DNS 上游 YAML 语法无效")
	}
	return parsed
}

// checkUpstreams raises unless upstreams is a list of mappings.
func checkUpstreams(upstreams any) {
	list, ok := upstreams.([]any)
	if ok {
		for _, element := range list {
			if !isDict(element) {
				ok = false
				break
			}
		}
	}
	if !ok {
		fail("DNS 上游必须为参数对象数组")
	}
}

// prepareFallback is _prepare_fallback(ctx, changes): nil (None) when no
// fallback field changes, else the requested state.
func prepareFallback(ctx *commonContext, changes *jsonx.Object) *bool {
	touched := false
	for _, field := range fallbackFields {
		if changes.Has(field) {
			touched = true
		}
	}
	if !touched {
		return nil
	}
	enabled, cap := fallbackState(ctx)
	if !cap.GetBool("editable") {
		fail(fallbackReason)
	}
	requested := enabled
	if changes.Has("fallback_dns") {
		upstreams := loadUpstreams(changes.Value("fallback_dns"))
		if upstreams == nil {
			upstreams = []any{}
		}
		checkUpstreams(upstreams)
		requested = len(upstreams.([]any)) > 0
	}
	ctx.ensure("forward_fallback", jsonx.Obj("upstreams", []any{}, "concurrent", 1), "")
	ctx.ensure("fallback_dns", fallbackSecondary(), "")
	ctx.ensure("fallback", jsonx.Obj("primary", "remote_dns", "secondary", "fallback_dns",
		"threshold", 500, "always_standby", false), "")
	return &requested
}

// finishFallback is _finish_fallback(ctx, enabled).
func finishFallback(ctx *commonContext, enabled *bool) {
	if enabled == nil {
		return
	}
	upstreams := get(ctx.args("forward_fallback"), "upstreams", []any{})
	if *enabled {
		list, ok := upstreams.([]any)
		valid := ok && len(list) > 0
		for _, element := range list {
			if !valid {
				break
			}
			addr, isText := get2(element, "addr")
			if !isDict(element) || !isText || pystr.Strip(addr) == "" {
				valid = false
			}
		}
		if !valid {
			fail("启用代理回退前请先填写备用 DNS 地址")
		}
	}
	calls := fallbackOff()
	if *enabled {
		calls = fallbackOn()
	}
	var holder any
	if p := ctx.plugin("proxy_dns_seq"); p != nil {
		holder = p
	}
	setItem(holder, "args", calls)
	if !*enabled {
		for _, tag := range fallbackPlugins {
			ctx.parkPlugin(tag)
		}
	}
}

// get2 returns entry['addr'] as a string when entry is a dict holding one.
func get2(entry any, key string) (string, bool) {
	if !isDict(entry) {
		return "", false
	}
	text, ok := get(entry, key, nil).(string)
	return text, ok
}

// directRead is _direct_read(ctx, values, capabilities).
func directRead(ctx *commonContext, values, capabilities *jsonx.Object) {
	commonInit()
	for _, entry := range commonTables.direct {
		var holder any
		var cap *jsonx.Object
		if !entry.hasTag {
			holder = ctx.doc
			cap = capability(true, "", strings.Join(entry.path, "."), false)
		} else {
			holder = ctx.storedArgs(entry.tag)
			cap = ctx.allowed(entry.tag, "")
		}
		value := getPath(holder, entry.path)
		if !safeParent(holder, entry.path) {
			cap = capability(false, "父参数使用了自定义值类型，请在完整参数中编辑", cap.Value("target"), false)
		}
		if value == missingValue {
			setValue(values, entry.field, nil)
			path := strings.Split(entry.field, ".")
			holderValue := values
			for _, key := range path[:len(path)-1] {
				holderValue = asObject(holderValue.SetDefault(key, jsonx.NewObject()))
			}
			holderValue.Set(path[len(path)-1], nil)
			cap.Set("absent", true)
		} else {
			setValue(values, entry.field, value)
		}
		capabilities.Set(entry.field, cap)
	}
}

var (
	qtypePattern = regexp.MustCompile(`^qtype(?: [0-9]+)+$`)
	ttlPattern   = regexp.MustCompile(`^ttl [0-9]+$`)
)

// mainControl is _main_control(ctx, field): value, capability and index
// (-1 for None) of prefer_ipv4 / metrics_enabled / block_qtypes.
func mainControl(ctx *commonContext, field string) (any, *jsonx.Object, int) {
	sequence, ok := ctx.sequence("sequence_main")
	if !ok || truthy(ctx.doc.Value("include")) {
		return nil, capability(false, "当前没有可识别的主处理链，请在完整参数中编辑", "sequence_main", false), -1
	}
	type candidate struct {
		index int
		entry *jsonx.Object
	}
	var possible []candidate
	if field == "block_qtypes" {
		for index, element := range sequence {
			entry, isObj := element.(*jsonx.Object)
			if !isObj || entry == nil || !strIn(entry.Value("exec"), BlockQtypesExecs...) {
				continue
			}
			for _, match := range matchesOf(entry) {
				if strings.HasPrefix(str(match), "qtype ") {
					possible = append(possible, candidate{index, entry})
					break
				}
			}
		}
		if len(possible) == 0 {
			return []any{}, capability(true, "", "sequence_main", false), -1
		}
		if len(possible) == 1 {
			entry := possible[0].entry
			matches := entry.Value("matches")
			list, isList := matches.([]any)
			keys := entry.Keys()
			exactKeys := len(keys) == 2 && entry.Has("matches") && entry.Has("exec")
			if exactKeys && pyLen(matches) == 1 && isList {
				if text, ok := list[0].(string); ok && qtypePattern.MatchString(text) {
					out := []any{}
					for _, part := range pySplit(text)[1:] {
						n, _ := new(big.Int).SetString(part, 10)
						out = append(out, jsonx.BigInt(n))
					}
					return out, capability(true, "", "sequence_main", false), possible[0].index
				}
			}
		}
	} else {
		command, prefix := "metrics_collector metrics", "metrics_collector"
		if field == "prefer_ipv4" {
			command, prefix = "prefer_ipv4", "prefer_ipv4"
		}
		for index, element := range sequence {
			entry, isObj := element.(*jsonx.Object)
			if isObj && entry != nil && strings.HasPrefix(str(entry.GetOr("exec", "")), prefix) {
				possible = append(possible, candidate{index, entry})
			}
		}
		if len(possible) == 0 {
			return false, capability(true, "", "sequence_main", false), -1
		}
		if len(possible) == 1 && jsonx.Equal(possible[0].entry, jsonx.Obj("exec", command)) {
			return true, capability(true, "", "sequence_main", false), possible[0].index
		}
	}
	return nil, capability(false, "当前使用了条件化、重复或自定义指令，请在完整参数中编辑", "sequence_main", false), -1
}

// pyLen is len(value) for the values a match list can hold.
func pyLen(value any) int {
	switch x := value.(type) {
	case string:
		return len([]rune(x))
	case []any:
		return len(x)
	case *jsonx.Object:
		return x.Len()
	}
	raise(pyErr("TypeError", "object of type '"+typeName(value)+"' has no len()"))
	return 0
}

// apiHolder is _api_holder(ctx, restore): the http section (or its parked
// copy) and whether it is live.
func apiHolder(ctx *commonContext, restore bool) (any, bool) {
	api := ctx.doc.GetOr("api", jsonx.NewObject())
	if !isDict(api) {
		return api, false
	}
	current := get(api, "http", nil)
	if current != nil {
		return current, true
	}
	if restore {
		return get(ctx.parked, "api_http", jsonx.NewObject()), false
	}
	return jsonx.NewObject(), false
}

func dynamicCall(route string) *jsonx.Object {
	return jsonx.Obj("matches", []any{"qname $dynamic_domains"}, "exec", route)
}

// dynamicState is _dynamic_state(ctx): enabled, route (nil, "direct",
// "proxy") and capability.
func dynamicState(ctx *commonContext) (bool, any, *jsonx.Object) {
	items, ok := ctx.sequence("sequence_main")
	if !ok || truthy(ctx.doc.Value("include")) {
		return false, nil, capability(false, "当前没有可识别的主处理链，请在完整参数中编辑", "dynamic_domains", false)
	}
	for _, element := range ctx.plugins() {
		if strIn(get(element, "type", nil), "learn_domain") && !strIn(get(element, "tag", nil), "learn_classified_domain") &&
			jsonx.Equal(getPath(element, []string{"args", "provider"}), "dynamic_domains") {
			return true, nil, capability(false, "动态域名集还被自定义学习器引用，请在完整参数中调整", "dynamic_domains", false)
		}
	}
	var matches []any
	for _, element := range items {
		if !isDict(element) {
			continue
		}
		for _, match := range matchesOf(element) {
			if strings.Contains(str(match), "$dynamic_domains") {
				matches = append(matches, element)
				break
			}
		}
	}
	remaining := copyValue(ctx.doc).(*jsonx.Object)
	for _, element := range iterate(get(remaining, "plugins", []any{})) {
		if strIn(get(element, "tag", nil), "sequence_main") {
			if list, ok := get(element, "args", nil).([]any); ok {
				kept := []any{}
				for _, entry := range list {
					if listIndex(matches, entry) < 0 {
						kept = append(kept, entry)
					}
				}
				setItem(element, "args", kept)
			}
		}
		if strIn(get(element, "tag", nil), "learn_classified_domain") && strIn(get(element, "type", nil), "learn_domain") && isDict(get(element, "args", nil)) {
			popKey(get(element, "args", nil), "provider")
		}
	}
	extra := referenceLocations(remaining, []string{"dynamic_domains"}, true)
	if len(extra) > 0 {
		called := len(matches) > 0
		for _, entry := range extra {
			if item(entry, "tag") == "dynamic_domains" {
				called = true
			}
		}
		return called, nil, capability(false, referenceReason(extra, DefaultReferencePrefix), "dynamic_domains", false)
	}
	mentions := 0
	for _, element := range ctx.plugins() {
		mentions += tagMentions(get(element, "args", nil), "dynamic_domains")
	}
	if mentions != len(matches) {
		return mentions > 0, nil, capability(false, "动态域名集还被其他执行链引用，请在完整参数中调整", "dynamic_domains", false)
	}
	if len(matches) == 0 {
		return false, nil, ctx.allowed("dynamic_domains", "")
	}
	if len(matches) == 1 {
		if jsonx.Equal(matches[0], dynamicCall("goto china_dns")) {
			return true, "direct", ctx.allowed("dynamic_domains", "")
		}
		if jsonx.Equal(matches[0], dynamicCall("goto proxy_dns_seq")) {
			return true, "proxy", ctx.allowed("dynamic_domains", "")
		}
	}
	return true, nil, capability(false, "动态域名使用了自定义匹配，请在完整参数中编辑", "dynamic_domains", false)
}

// cronJob is _cron_job(ctx): the managed refresh job (nil for None) and the
// capability.
func cronJob(ctx *commonContext) (*jsonx.Object, *jsonx.Object) {
	var alias *jsonx.Object
	for _, tag := range []string{"rules_cron", "rules_download", "rules_reload", "rules_refresh"} {
		if alias = ctx.aliasCapability(tag); alias != nil {
			break
		}
	}
	or := func() *jsonx.Object {
		if alias != nil {
			return alias
		}
		return ctx.allowed("rules_cron", "")
	}
	args := ctx.args("rules_cron")
	var jobs any
	if isDict(args) {
		jobs = get(args, "jobs", []any{})
	}
	list, ok := jobs.([]any)
	if !ok {
		return nil, capability(false, "定时任务使用了自定义值类型，请在完整参数中编辑", "rules_cron", false)
	}
	var matching []*jsonx.Object
	for _, element := range list {
		job, isObj := element.(*jsonx.Object)
		if !isObj || job == nil {
			continue
		}
		executors, isList := job.Value("executors").([]any)
		if jsonx.Equal(job.Value("name"), "refresh_rules") || (isList && listIndex(executors, "$rules_refresh") >= 0) {
			matching = append(matching, job)
		}
	}
	if len(matching) == 0 {
		return nil, or()
	}
	if len(matching) != 1 || !jsonx.Equal(matching[0].Value("name"), "refresh_rules") ||
		!jsonx.Equal(matching[0].Value("executors"), []any{"$rules_refresh"}) {
		return nil, capability(false, "规则更新任务使用了自定义执行器，请在完整参数中编辑", "rules_cron", false)
	}
	return matching[0], or()
}

// adguardPath is f'rules/{_tag("adguard", url)}.txt'.
func adguardPath(url any) string { return "rules/" + TagFor("adguard", url) + ".txt" }

// resources is _resources(ctx): the subscription URLs and custom content
// adguard_rules reads, and the capability.
func resources(ctx *commonContext) ([]any, bool, *jsonx.Object) {
	source, ok := ctx.settings.Value("adguard").(*jsonx.Object)
	if !ok || source == nil {
		source = jsonx.NewObject()
	}
	args := ctx.storedArgs("adguard_rules")
	var files any
	if isDict(args) {
		files = get(args, "files", []any{})
	}
	urls, ok := source.GetOr("urls", []any{}).([]any)
	if !ok {
		urls = []any{}
	}
	known := map[string]any{}
	for _, url := range urls {
		if isStr(url) {
			known[adguardPath(url)] = url
		}
	}
	list, ok := files.([]any)
	if ok {
		for _, path := range list {
			if !isStr(path) {
				ok = false
				break
			}
		}
	}
	if !ok {
		return []any{}, false, capability(false, "规则文件使用了自定义格式，请在完整参数中编辑", "adguard_rules", false)
	}
	var paths []*string
	for _, path := range list {
		paths = append(paths, relativeResource(path))
	}
	unknown := false
	for _, path := range paths {
		if path == nil {
			unknown = true
			continue
		}
		if _, isKnown := known[*path]; *path != "rules/adguard-custom.txt" && !isKnown {
			unknown = true
		}
	}
	cap := ctx.allowed("adguard_rules", "")
	if unknown {
		cap = capability(false, "存在无法映射到订阅地址的自定义规则文件，请在完整参数中编辑", "adguard_rules", false)
	}
	out := []any{}
	hasContent := false
	for _, path := range paths {
		if path == nil {
			continue
		}
		if url, isKnown := known[*path]; isKnown {
			out = append(out, url)
		}
		if *path == "rules/adguard-custom.txt" {
			hasContent = true
		}
	}
	return out, hasContent, cap
}

func adguardCall() *jsonx.Object {
	return jsonx.Obj("matches", []any{"question $adguard_rules"}, "exec", "goto adguard_reject")
}

func adguardReject() []any {
	return []any{jsonx.Obj("exec", "$adguard_block"), jsonx.Obj("exec", "accept")}
}

// adguardState is _adguard_state(ctx).
func adguardState(ctx *commonContext) (bool, *jsonx.Object) {
	call := adguardCall()
	main, hasMain := ctx.sequence("sequence_main")
	enabled := listIndex(main, call) >= 0
	for _, tag := range []string{"adguard_rules", "adguard_reject", "adguard_block"} {
		if alias := ctx.aliasCapability(tag); alias != nil {
			return len(referenceLocations(ctx.doc, []string{alias.Value("target").(string)}, false)) > 0, alias
		}
	}
	denied := capability(false, "自定义过滤链请在完整参数中调整", "adguard_rules", false)
	reject := ctx.plugin("adguard_reject")
	if !hasMain || truthy(ctx.doc.Value("include")) ||
		(reject != nil && (!strIn(reject.Value("type"), "sequence") || !jsonx.Equal(reject.Value("args"), adguardReject()))) ||
		!ctx.allowed("adguard_block", "").GetBool("editable") || !ctx.allowed("adguard_rules", "").GetBool("editable") {
		return enabled, denied
	}
	count := 0
	for _, element := range main {
		if jsonx.Equal(element, call) {
			count++
		}
	}
	if count > 1 {
		return enabled, denied
	}
	remaining := copyValue(ctx.doc).(*jsonx.Object)
	for _, element := range iterate(get(remaining, "plugins", []any{})) {
		switch {
		case strIn(get(element, "tag", nil), "sequence_main"):
			kept := []any{}
			for _, entry := range pyIter(item(element, "args")) {
				if !jsonx.Equal(entry, call) {
					kept = append(kept, entry)
				}
			}
			setItem(element, "args", kept)
		case strIn(get(element, "tag", nil), "adguard_reject"):
			popKey(element, "args")
		case strIn(get(element, "tag", nil), "rules_reload") && strIn(get(element, "type", nil), "reload_provider"):
			if list, ok := get(element, "args", nil).([]any); ok {
				setItem(element, "args", listWithout(list, "$adguard_rules"))
			}
		}
	}
	refs := referenceLocations(remaining, []string{"adguard_rules", "adguard_reject", "adguard_block"}, true)
	if len(refs) > 0 {
		active := enabled
		for _, entry := range refs {
			if item(entry, "tag") != nil {
				active = true
			}
		}
		return active, capability(false, referenceReason(refs, DefaultReferencePrefix), "adguard_rules", false)
	}
	return enabled, capability(true, "", "adguard_rules", false)
}

// featureControl is one (field, tag, chains) row of the feature switches.
type featureControl struct {
	field, prefix, tag string
	chains             []string
}

func featureControls() []featureControl {
	out := []featureControl{
		{"cache_enabled", "cache_", "lazy_cache", []string{"china_dns"}},
		{"probe_cache_enabled", "probe_cache_", "local_probe_cache", []string{"local_probe"}},
	}
	for _, name := range features {
		chains := wrappers
		if name == "query_recorder" {
			chains = []string{"sequence_main"}
		}
		out = append(out, featureControl{name + ".enabled", name + ".", name, chains})
	}
	return out
}

func dumpValue(value any) string {
	if holdsMapping(value) {
		unsupportedYAML("配置", &yamlx.NonStringKeyError{Msg: "mapping with a non-string key"})
	}
	text, err := yamlx.SafeDumpUnicode(value)
	must(err)
	return text
}

// readCommon is _read(document, settings, config).
func readCommon(document, settings *jsonx.Object, config any) *jsonx.Object {
	ctx := newCommonContext(document, settings)
	values := neutral(commonDefaults()).(*jsonx.Object)
	for _, field := range []string{"config_mode", "native_config", "additional_files", "custom_config", "rule_dns_parked"} {
		values.Delete(field)
	}
	capabilities := jsonx.NewObject()
	directRead(ctx, values, capabilities)
	for _, feature := range []string{"ros_address_list", "ros_route"} {
		if tls := getPath(ctx.storedArgs(feature), []string{"tls"}); tls == missingValue || tls == nil {
			values.GetObject(feature).GetObject("args").Set("tls", nil)
		}
	}
	for _, name := range []string{"local", "remote", "fallback"} {
		tag := "forward_" + name
		var holder any
		if name == "fallback" {
			holder = ctx.storedArgs(tag)
		} else {
			holder = ctx.args(tag)
		}
		upstreams := getPath(holder, []string{"upstreams"})
		cap := ctx.allowed(tag, "")
		if _, isList := upstreams.([]any); upstreams != missingValue && !isList {
			cap = capability(false, "上游使用了自定义值类型，请在完整参数中编辑", tag, false)
		}
		if upstreams == missingValue {
			values.Set(name+"_dns", "")
		} else {
			values.Set(name+"_dns", dumpValue(upstreams))
		}
		capabilities.Set(name+"_dns", cap)
	}
	fallbackEnabled, fallbackCap := fallbackState(ctx)
	if !fallbackEnabled && fallbackCap.GetBool("editable") {
		values.Set("fallback_dns", "")
	}
	if !fallbackCap.GetBool("editable") {
		for _, field := range fallbackFields {
			capabilities.Set(field, fallbackCap)
		}
	}
	udp := ctx.args("server_udp")
	tcp := ctx.plugin("server_tcp")
	if isDict(udp) {
		values.Set("listen", get(udp, "listen", nil))
	} else {
		values.Set("listen", nil)
	}
	capabilities.Set("listen", ctx.allowed("server_udp", ""))
	values.Set("tcp_enabled", tcp != nil)
	capabilities.Set("tcp_enabled", ctx.allowed("server_tcp", ""))
	parkedTCP := get(ctx.parked, "server_tcp", jsonx.NewObject())
	restorable := isDict(parkedTCP) && strIn(get(parkedTCP, "type", nil), "tcp_server") &&
		isDict(get(parkedTCP, "args", nil)) && truthy(get(item(parkedTCP, "args"), "listen", nil)) &&
		truthy(get(item(parkedTCP, "args"), "entry", nil))
	if tcp == nil && !restorable && (ctx.plugin("server_udp") == nil || !ctx.allowed("server_udp", "").GetBool("editable") ||
		!isDict(udp) || !truthy(get(udp, "listen", nil)) || !truthy(get(udp, "entry", nil))) {
		capabilities.Set("tcp_enabled", capability(false, "缺少可复用的 UDP 监听地址和处理入口，请在监听服务中配置 TCP", "server_tcp", false))
	}
	if tcp != nil && ctx.plugin("server_udp") != nil && (!isDict(udp) || !isDict(tcp.Value("args")) ||
		!jsonx.Equal(get(tcp.Value("args"), "listen", nil), values.Value("listen")) ||
		!jsonx.Equal(get(tcp.Value("args"), "entry", nil), get(udp, "entry", nil))) {
		cap := capability(false, "UDP 与 TCP 使用独立配置，请在完整参数或监听服务中分别编辑", "server_tcp", false)
		capabilities.Set("listen", cap)
		capabilities.Set("tcp_enabled", cap)
	}
	for _, field := range []string{"prefer_ipv4", "metrics_enabled", "block_qtypes"} {
		value, cap, _ := mainControl(ctx, field)
		values.Set(field, value)
		capabilities.Set(field, cap)
	}
	for _, control := range featureControls() {
		enabled, cap := ctx.callState(control.tag, control.chains)
		putPath(values, strings.Split(control.field, "."), enabled)
		capabilities.Set(control.field, cap)
	}
	cache := ctx.storedArgs("lazy_cache")
	dumpEnabled := isDict(cache) && truthy(get(cache, "dump_file", nil))
	values.Set("cache_dump_enabled", dumpEnabled)
	capabilities.Set("cache_dump_enabled", ctx.allowed("lazy_cache", ""))
	if !dumpEnabled && isDict(cache) {
		for _, field := range []string{"cache_dump_file", "cache_dump_interval"} {
			if ctx.parked.Has(field) && !has(cache, strings.TrimPrefix(field, "cache_")) {
				values.Set(field, copyValue(ctx.parked.Value(field)))
			}
		}
	}
	remote, hasRemote := ctx.sequence("remote_dns")
	var ttlItems []*jsonx.Object
	for _, element := range remote {
		entry, isObj := element.(*jsonx.Object)
		if isObj && entry != nil && strings.HasPrefix(str(entry.GetOr("exec", "")), "ttl ") {
			ttlItems = append(ttlItems, entry)
		}
	}
	values.Set("remote_ttl", 0)
	capabilities.Set("remote_ttl", capability(true, "", "remote_dns", false))
	customTTL := false
	if len(ttlItems) > 0 {
		keys := ttlItems[0].Keys()
		text, _ := ttlItems[0].Value("exec").(string)
		customTTL = len(keys) != 1 || keys[0] != "exec" || !ttlPattern.MatchString(text)
	}
	if !hasRemote || len(ttlItems) > 1 || customTTL {
		values.Set("remote_ttl", nil)
		capabilities.Set("remote_ttl", capability(false, "代理响应 TTL 使用了自定义执行链，请在完整参数中编辑", "remote_dns", false))
	} else if len(ttlItems) > 0 {
		n, _ := new(big.Int).SetString(pySplit(ttlItems[0].Value("exec").(string))[1], 10)
		values.Set("remote_ttl", jsonx.BigInt(n))
	}
	api, apiEnabled := apiHolder(ctx, false)
	values.GetObject("api").Set("enabled", apiEnabled)
	if isDict(api) {
		capabilities.Set("api.enabled", capability(true, "", "api.http", false))
	} else {
		capabilities.Set("api.enabled", capability(false, "管理 API 使用原生简写，请在完整参数中编辑", "api.http", false))
	}
	for _, entry := range apiFields {
		value := getPath(api, entry.path)
		editable := apiEnabled && isDict(api) && safeParent(api, entry.path)
		if entry.path[0] == "auth" && isDict(api) && isDict(get(api, "auth", nil)) &&
			!jsonx.Equal(get(item(api, "auth"), "type", "basic"), "basic") {
			editable = false
		}
		if value == missingValue {
			setValue(values, entry.field, "")
		} else {
			setValue(values, entry.field, value)
		}
		reason := ""
		if !editable {
			reason = "管理 API 使用自定义认证或原生值类型，请在完整参数中编辑"
			if !apiEnabled {
				reason = "请先启用 API / WebUI，再编辑其参数"
			}
		}
		capabilities.Set(entry.field, capability(editable, reason, "api.http", value == missingValue))
	}
	dynEnabled, route, dynCap := dynamicState(ctx)
	values.GetObject("dynamic_domain_set").Set("enabled", dynEnabled)
	values.GetObject("dynamic_domain_set").Set("route", route)
	capabilities.Set("dynamic_domain_set.enabled", dynCap)
	capabilities.Set("dynamic_domain_set.route", dynCap)
	learnPlugin := ctx.plugin("learn_classified_domain")
	learnRoute := "ip_requery_proxy"
	if route == "direct" {
		learnRoute = "direct_ip"
	}
	learning, learningCap := ctx.callState("learn_classified_domain", []string{learnRoute})
	if learnPlugin != nil && !jsonx.Equal(getPath(learnPlugin, []string{"args", "provider"}), "dynamic_domains") {
		learningCap = capability(false, "学习器引用了其他域名集，请在完整参数中编辑", "learn_classified_domain", false)
	}
	values.GetObject("dynamic_domain_set").Set("learn", learning)
	capabilities.Set("dynamic_domain_set.learn", learningCap)
	if dynCap.GetBool("editable") {
		count := 0
		for _, element := range ctx.plugins() {
			count += referenceCount(get(element, "args", nil), "learn_classified_domain")
		}
		if count > 0 && !learningCap.GetBool("editable") {
			capabilities.Set("dynamic_domain_set.enabled", learningCap)
			capabilities.Set("dynamic_domain_set.route", learningCap)
		}
	}
	job, cronCap := cronJob(ctx)
	values.GetObject("rule_update").Set("enabled", job != nil)
	capabilities.Set("rule_update.enabled", cronCap)
	for _, key := range []string{"schedule", "timezone"} {
		var value any
		if key == "schedule" {
			if job != nil {
				value = job.Value("schedule")
			}
		} else {
			value = getPath(ctx.args("rules_cron"), []string{"timezone"})
		}
		if value == missingValue {
			value = nil
		}
		values.GetObject("rule_update").GetObject("args").Set(key, value)
		capabilities.Set("rule_update.args."+key, cronCap)
	}
	urls, hasContent, resourcesCap := resources(ctx)
	source, ok := settings.Value("adguard").(*jsonx.Object)
	if !ok || source == nil {
		source = jsonx.NewObject()
	}
	adguard := values.GetObject("adguard")
	adguard.Set("urls", urls)
	if hasContent {
		adguard.Set("content", source.GetOr("content", ""))
	} else {
		adguard.Set("content", "")
	}
	mode := getPath(ctx.storedArgs("adguard_block"), []string{"mode"})
	if mode == missingValue {
		mode = nil
	}
	adguard.Set("mode", mode)
	capabilities.Set("adguard.urls", resourcesCap)
	capabilities.Set("adguard.content", resourcesCap)
	capabilities.Set("adguard.mode", ctx.allowed("adguard_block", ""))
	adguardEnabled, adguardCap := adguardState(ctx)
	adguard.Set("enabled", adguardEnabled)
	capabilities.Set("adguard.enabled", adguardCap)
	hosts := ctx.args("custom_hosts")
	var files any
	if isDict(hosts) {
		files = get(hosts, "files", []any{})
	}
	paths, filesList := files.([]any)
	if listIndex(paths, "./rules/custom_hosts.txt") >= 0 || listIndex(paths, "rules/custom_hosts.txt") >= 0 {
		values.Set("custom_hosts", settings.GetOr("custom_hosts", ""))
	} else {
		values.Set("custom_hosts", "")
	}
	_, hostsCap := ctx.callState("custom_hosts", []string{"sequence_main"})
	capabilities.Set("custom_hosts", hostsCap)
	if _, ok := ctx.sequence("sequence_main"); !ok || !filesList ||
		(len(paths) > 0 && !jsonx.Equal(paths, []any{"./rules/custom_hosts.txt"}) && !jsonx.Equal(paths, []any{"rules/custom_hosts.txt"})) {
		capabilities.Set("custom_hosts", capability(false, "Hosts 使用自定义文件或处理链，请在完整参数中编辑", "custom_hosts", false))
	}
	routing := readRouting(document, settings, config)
	if routed, ok := item(routing, "values").(*jsonx.Object); ok {
		values.Update(routed)
	} else {
		raise(pyErr("TypeError", "'"+typeName(item(routing, "values"))+"' object is not iterable"))
	}
	if routed, ok := item(routing, "capabilities").(*jsonx.Object); ok {
		capabilities.Update(routed)
	} else {
		raise(pyErr("TypeError", "'"+typeName(item(routing, "capabilities"))+"' object is not iterable"))
	}
	out := jsonx.Obj("values", values, "capabilities", capabilities)
	out.Update(readMatchTargets(document, settings))
	return out
}

// ReadCommonSettings is read_common_settings(settings, config, base_url):
// values and capabilities of the common controls for the current draft.
func ReadCommonSettings(settings, config any, baseURL string) (result *jsonx.Object, err error) {
	defer catch(&err)
	prepared := prepareEditor(settings, config, baseURL)
	return readCommon(loadNativeYAML(item(prepared, "native_config"), DefaultNativeLabel), prepared, config), nil
}

// validateValue is _validate_value(field, value).
func validateValue(field string, value any) {
	if value == nil {
		return
	}
	example := getPath(commonDefaults(), strings.Split(field, "."))
	_, exampleBool := example.(bool)
	exampleNumber, isNumber := example.(jsonx.Number)
	_, exampleList := example.([]any)
	_, exampleText := example.(string)
	switch {
	case strings.HasSuffix(field, ".tls"):
		if !isDict(value) {
			fail(field + " 必须是 TLS 参数对象或 null")
		}
	case strings.HasSuffix(field, ".tls.insecure") || exampleBool:
		if _, ok := value.(bool); !ok {
			fail(field + " 必须为布尔值")
		}
	case isNumber && exampleNumber.IsInt():
		if !isInt(value) {
			fail(field + " 必须为整数")
		}
	case exampleList:
		list, ok := value.([]any)
		if !ok {
			fail(field + " 必须为数组")
		}
		if field == "block_qtypes" {
			for _, element := range list {
				if !isInt(element) {
					fail("屏蔽查询类型必须为整数数组")
				}
			}
		}
	case exampleText || strings.Contains(field, ".tls.") || field == "api.ssl.cert" || field == "api.ssl.key":
		if !isStr(value) {
			fail(field + " 必须为文本")
		}
	}
}

// defaultArgs is _default_args(tag).
func defaultArgs(tag string) any {
	defaults := commonDefaults()
	switch {
	case strIn(tag, features...):
		return copyValue(defaults.GetObject(tag).Value("args"))
	case tag == "lazy_cache":
		return jsonx.Obj("size", 10240, "lazy_cache_ttl", 3600, "short_circuit", true)
	case tag == "local_probe_cache":
		return jsonx.Obj("size", 2048, "short_circuit", false)
	case tag == "dynamic_domains":
		return copyValue(defaults.GetObject("dynamic_domain_set").Value("args"))
	case tag == "learn_classified_domain":
		out := copyValue(defaults.GetObject("dynamic_domain_set").Value("learn_args")).(*jsonx.Object)
		out.Set("provider", "dynamic_domains")
		return out
	}
	return jsonx.NewObject()
}

// patchDirect is _patch_direct(ctx, field, value).
func patchDirect(ctx *commonContext, field string, value any) {
	commonInit()
	entry := commonTables.byField[field]
	var holder any = ctx.doc
	if entry.hasTag {
		holder = item(ctx.ensure(entry.tag, defaultArgs(entry.tag), ""), "args")
	}
	if field == "cache_lazy_ttl" && jsonx.Equal(value, jsonx.Int(0)) {
		value = nil
	}
	if strIn(field, "log.file", "rule_update.args.timeout", "rule_update.args.outbound", "rule_update.args.socks5") && jsonx.Equal(value, "") {
		value = nil
	}
	if field == "log.max_files" && value != nil {
		if current := getPath(holder, []string{"log", "rotation", "type"}); current == missingValue || current == nil {
			putPath(holder, []string{"log", "rotation", "type"}, "never")
		}
	}
	putPath(holder, entry.path, value)
	if field == "log.rotation" && value == nil {
		rotation := getPath(holder, []string{"log", "rotation"})
		if obj, ok := rotation.(*jsonx.Object); ok && obj != nil {
			if obj.Len() > 0 {
				obj.Set("type", "never")
			} else {
				popKey(item(holder, "log"), "rotation")
			}
		}
	}
}

// patchAPI is _patch_api(ctx, field, value).
func patchAPI(ctx *commonContext, field string, value any) {
	current, enabled := apiHolder(ctx, true)
	if field == "api.enabled" {
		if truthy(value) {
			var http any
			if truthy(current) {
				http = copyValue(current)
			} else {
				http = jsonx.Obj("listen", commonDefaults().GetObject("api").Value("listen"))
			}
			setItem(ctx.doc.SetDefault("api", jsonx.NewObject()), "http", http)
		} else if enabled {
			ctx.park("api_http", current)
			popKey(ctx.doc.Value("api"), "http")
			if !truthy(ctx.doc.Value("api")) {
				ctx.doc.Delete("api")
			}
		}
		return
	}
	var holder any
	if enabled {
		holder = item(item(ctx.doc, "api"), "http")
	} else {
		disabled := ctx.settings.SetDefault("_common_disabled", jsonx.NewObject())
		setDefault(disabled, "api_http", copyValue(current))
		holder = item(item(ctx.settings, "_common_disabled"), "api_http")
	}
	path := commonTables.api[field]
	if strIn(path[0], "auth", "webui") && !jsonx.Equal(value, "") && value != nil && !pyContains(holder, path[0]) {
		saved := get(ctx.parked, "api_"+path[0], nil)
		if isDict(saved) {
			setItem(holder, path[0], copyValue(saved))
		}
	}
	if jsonx.Equal(value, "") && field != "api.listen" {
		putPath(holder, path, nil)
	} else {
		putPath(holder, path, value)
	}
	if path[0] == "auth" {
		if auth, ok := get(holder, "auth", nil).(*jsonx.Object); ok && auth != nil {
			if auth.Len() > 0 {
				auth.SetDefault("type", "basic")
			}
			if !truthy(auth.Value("username")) && !truthy(auth.Value("password")) {
				ctx.park("api_auth", auth)
				popKey(holder, "auth")
			}
		}
	}
	if path[0] == "webui" {
		webui := get(holder, "webui", jsonx.NewObject())
		if truthy(get(webui, "root", nil)) {
			setDefault(item(holder, "webui"), "index", "index.html")
		} else if isDict(get(holder, "webui", nil)) {
			ctx.park("api_webui", popKey(holder, "webui"))
		}
	}
}

// patchCacheDump is _patch_cache_dump(ctx, changes).
func patchCacheDump(ctx *commonContext, changes *jsonx.Object) {
	if !changes.Has("cache_dump_enabled") && !changes.Has("cache_dump_file") && !changes.Has("cache_dump_interval") {
		return
	}
	args := item(ctx.ensure("lazy_cache", defaultArgs("lazy_cache"), ""), "args")
	var enabled any = truthy(get(args, "dump_file", nil))
	if value, ok := changes.Get("cache_dump_enabled"); ok {
		enabled = value
	}
	if truthy(get(args, "dump_file", nil)) {
		ctx.park("cache_dump_file", item(args, "dump_file"))
	}
	if truthy(get(args, "dump_file", nil)) || has(args, "dump_interval") {
		ctx.park("cache_dump_interval", get(args, "dump_interval", nil))
	}
	for _, field := range []string{"cache_dump_file", "cache_dump_interval"} {
		if value, ok := changes.Get(field); ok {
			ctx.park(field, value)
		}
	}
	if truthy(enabled) {
		var path any
		if value, ok := changes.Get("cache_dump_file"); ok {
			path = value
		} else if value := get(args, "dump_file", nil); truthy(value) {
			path = value
		} else if value := get(ctx.parked, "cache_dump_file", nil); truthy(value) {
			path = value
		} else {
			path = commonDefaults().Value("cache_dump_file")
		}
		text, isText := path.(string)
		enabled = isText && pystr.Strip(text) != ""
		if enabled == true {
			setItem(args, "dump_file", path)
			interval := get(ctx.parked, "cache_dump_interval", commonDefaults().Value("cache_dump_interval"))
			putPath(args, []string{"dump_interval"}, interval)
		}
	}
	if !truthy(enabled) {
		popKey(args, "dump_file")
		popKey(args, "dump_interval")
	}
}

// patchTLS is _patch_tls(ctx, changes).
func patchTLS(ctx *commonContext, changes *jsonx.Object) {
	for _, feature := range []string{"ros_address_list", "ros_route"} {
		field := feature + ".args.tls"
		touched := false
		for _, key := range changes.Keys() {
			if key == field || strings.HasPrefix(key, field+".") {
				touched = true
			}
		}
		if !touched {
			continue
		}
		args := item(ctx.ensure(feature, defaultArgs(feature), ""), "args")
		current := get(args, "tls", nil)
		var enabled bool
		if value, ok := changes.Get(field); ok {
			enabled = isDict(value)
		} else {
			enabled = isDict(current)
		}
		saved := current
		if !isDict(current) {
			saved = get(ctx.parked, field, jsonx.NewObject())
		}
		value := jsonx.NewObject()
		if isDict(saved) {
			value = copyValue(saved).(*jsonx.Object)
		}
		if change, ok := changes.Value(field).(*jsonx.Object); ok && change != nil {
			value.Update(copyValue(change).(*jsonx.Object))
		}
		for key, element := range changes.All() {
			if strings.HasPrefix(key, field+".") {
				putPath(value, strings.Split(key[len(field)+1:], "."), element)
			}
		}
		ctx.park(field, value)
		if enabled {
			putPath(args, []string{"tls"}, value)
		} else {
			putPath(args, []string{"tls"}, nil)
		}
	}
}

// patchMain is _patch_main(ctx, field, value).
func patchMain(ctx *commonContext, field string, value any) {
	_, _, index := mainControl(ctx, field)
	items, isSequence := ctx.sequence("sequence_main")
	var replacement *jsonx.Object
	if truthy(value) {
		switch field {
		case "block_qtypes":
			var parts []string
			for _, element := range pyIter(value) {
				parts = append(parts, str(element))
			}
			replacement = jsonx.Obj("matches", []any{"qtype " + strings.Join(parts, " ")}, "exec", BlockQtypesExec)
		case "prefer_ipv4":
			replacement = jsonx.Obj("exec", "prefer_ipv4")
		default:
			replacement = jsonx.Obj("exec", "metrics_collector metrics")
		}
	}
	if index >= 0 {
		if replacement != nil {
			items[index] = replacement
		} else {
			ctx.setSequence("sequence_main", popCopy(items, index))
		}
	} else if replacement != nil {
		if !isSequence {
			if field == "metrics_enabled" {
				raise(attributeError(nil, "insert"))
			}
			raise(pyErr("TypeError", "'NoneType' object is not iterable"))
		}
		index = 0
		if field != "metrics_enabled" {
			index = len(items)
			for i, element := range items {
				if !strIn(get(element, "exec", nil), "metrics_collector metrics", "$query_recorder", "prefer_ipv4") {
					index = i
					break
				}
			}
		}
		ctx.setSequence("sequence_main", insertCopy(items, index, replacement))
	}
}

// patchDynamic is _patch_dynamic(ctx, changes).
func patchDynamic(ctx *commonContext, changes *jsonx.Object) {
	enabled, route, _ := dynamicState(ctx)
	parked, ok := get(ctx.parked, "dynamic_domain_set_state", jsonx.NewObject()).(*jsonx.Object)
	if !ok || parked == nil {
		parked = jsonx.NewObject()
	}
	nextEnabled := changes.GetOr("dynamic_domain_set.enabled", enabled)
	var defaultRoute any = route
	if !truthy(route) {
		defaultRoute = parked.GetOr("route", "direct")
	}
	nextRoute := changes.GetOr("dynamic_domain_set.route", defaultRoute)
	if !strIn(nextRoute, "direct", "proxy") {
		fail("动态域名路由必须为 direct 或 proxy")
	}
	learnCall := jsonx.Obj("exec", "$learn_classified_domain")
	var oldChains []string
	for _, name := range []string{"direct_ip", "ip_requery_proxy"} {
		if list, _ := ctx.sequence(name); listIndex(list, learnCall) >= 0 {
			oldChains = append(oldChains, name)
		}
	}
	var defaultLearn any
	if enabled {
		defaultLearn = len(oldChains) > 0
	} else {
		defaultLearn = parked.GetOr("learn", false)
	}
	learn := changes.GetOr("dynamic_domain_set.learn", defaultLearn)
	routeOrEnabled := changes.Has("dynamic_domain_set.enabled") || changes.Has("dynamic_domain_set.route")
	anyField := routeOrEnabled || changes.Has("dynamic_domain_set.learn")
	if anyField {
		ctx.park("dynamic_domain_set_state", jsonx.Obj("route", nextRoute, "learn", learn))
	}
	if truthy(nextEnabled) {
		ctx.ensure("dynamic_domains", defaultArgs("dynamic_domains"), "")
	}
	if routeOrEnabled {
		main, isSequence := ctx.sequence("sequence_main")
		if !isSequence {
			raise(pyErr("TypeError", "'NoneType' object is not iterable"))
		}
		previous := -1
		for index, element := range main {
			if jsonx.Equal(get(element, "matches", nil), []any{"qname $dynamic_domains"}) {
				previous = index
				break
			}
		}
		if previous >= 0 {
			main = popCopy(main, previous)
			ctx.setSequence("sequence_main", main)
		}
		if truthy(nextEnabled) {
			index := previous
			if previous < 0 {
				index = len(main) - 1
				if index < 0 {
					index = 0
				}
				for i, element := range main {
					if jsonx.Equal(get(element, "exec", nil), "jump local_probe") {
						index = i
						break
					}
				}
			}
			target := "goto proxy_dns_seq"
			if nextRoute == "direct" {
				target = "goto china_dns"
			}
			ctx.setSequence("sequence_main", insertCopy(main, index, dynamicCall(target)))
		}
	}
	if anyField {
		for _, chain := range oldChains {
			list, _ := ctx.sequence(chain)
			ctx.setSequence(chain, listWithout(list, learnCall))
		}
		if truthy(nextEnabled) && truthy(learn) {
			name := "ip_requery_proxy"
			if nextRoute == "direct" {
				name = "direct_ip"
			}
			chain, ok := ctx.sequence(name)
			if !ok {
				fail("缺少动态学习的目标执行链，请在完整参数中配置")
			}
			ctx.ensure("learn_classified_domain", defaultArgs("learn_classified_domain"), "")
			index := len(chain) - 1
			if index < 0 {
				index = 0
			}
			owner := ctx.plugin(name)
			ctx.replaceList(owner, "args", chain, insertCopy(chain, index, learnCall))
		}
	}
}

// parkDisabledFeatures is _park_disabled_features(ctx, changes).
func parkDisabledFeatures(ctx *commonContext, changes *jsonx.Object) {
	keys := changes.Keys()
	anyPrefix := func(prefix string) bool {
		for _, key := range keys {
			if strings.HasPrefix(key, prefix) {
				return true
			}
		}
		return false
	}
	for _, control := range featureControls() {
		if anyPrefix(control.prefix) {
			enabled, cap := ctx.callState(control.tag, control.chains)
			if !enabled && cap.GetBool("editable") {
				ctx.parkPlugin(control.tag)
			}
		}
	}
	if anyPrefix("dynamic_domain_set.") {
		if _, _, cap := dynamicState(ctx); !cap.GetBool("editable") {
			return
		}
		if len(referenceLocations(ctx.doc, []string{"learn_classified_domain"}, true)) == 0 {
			ctx.parkPlugin("learn_classified_domain")
		}
		if enabled, _, cap := dynamicState(ctx); !enabled && cap.GetBool("editable") {
			ctx.parkPlugin("dynamic_domains")
		}
	}
}

// patchRuleUpdate is _patch_rule_update(ctx, changes, config, base_url).
func patchRuleUpdate(ctx *commonContext, changes *jsonx.Object, config any, baseURL string) {
	if ctx.plugin("rules_cron") == nil && (changes.Value("rule_update.enabled") == true || changes.Has("rule_update.args.timezone")) {
		parked := get(ctx.parked, "rules_cron", nil)
		if isDict(parked) && strIn(get(parked, "tag", nil), "rules_cron") && strIn(get(parked, "type", nil), "cron") &&
			isDict(get(parked, "args", nil)) && jsonx.Equal(get(item(parked, "args"), "jobs", nil), []any{}) {
			restored := copyValue(parked).(*jsonx.Object)
			ctx.appendPlugin(restored)
			ctx.byTag["rules_cron"] = restored
		}
	}
	cron0, _ := cronJob(ctx)
	var job any
	if cron0 != nil {
		job = cron0
	}
	defaults := commonDefaults().GetObject("rule_update").GetObject("args")
	if changes.Value("rule_update.enabled") == true && job == nil {
		source := mergedCopy(ctx.settings)
		source.Set("native_config", dump(ctx.doc))
		setItem(copyValue(config), "oxidns", source)
		downloads := resourceDownloads(ctx.doc, source, config, baseURL)
		if len(downloads) == 0 {
			fail("规则定时更新需要当前配置引用至少一个可下载订阅")
		}
		downloader := ctx.ensure("rules_download", jsonx.Obj("timeout", defaults.Value("timeout"), "downloads", downloads), "")
		if !isDict(downloader.Value("args")) {
			fail("下载插件使用自定义参数，请在完整参数中编辑")
		}
		setDefault(downloader.Value("args"), "downloads", downloads)
		providers := []any{}
		for _, element := range iterate(ctx.doc.GetOr("plugins", []any{})) {
			args := get(element, "args", nil)
			if strIn(get(element, "type", nil), "domain_set", "ip_set", "adguard_rule") && isDict(args) && truthy(get(item(element, "args"), "files", nil)) {
				providers = append(providers, "$"+str(item(element, "tag")))
			}
		}
		ctx.ensure("rules_reload", providers, "reload_provider")
		ctx.ensure("rules_refresh", []any{jsonx.Obj("exec", "$rules_download"), jsonx.Obj("exec", "$rules_reload")}, "sequence")
		cron := ctx.ensure("rules_cron", jsonx.Obj("timezone", defaults.Value("timezone"), "jobs", []any{}), "")
		job = copyValue(get(ctx.parked, "rules_cron_job",
			jsonx.Obj("name", "refresh_rules", "schedule", defaults.Value("schedule"), "executors", []any{"$rules_refresh"})))
		cronArgs := cron.Value("args")
		jobs := setDefault(cronArgs, "jobs", []any{})
		list, ok := jobs.([]any)
		if !ok {
			raise(attributeError(jobs, "append"))
		}
		ctx.replaceList(cronArgs.(*jsonx.Object), "jobs", list, appendCopy(list, job))
	}
	if changes.Value("rule_update.enabled") == false && job != nil {
		ctx.park("rules_cron_job", job)
		args := ctx.args("rules_cron")
		jobs := item(args, "jobs")
		removed := listRemove(jobs, job)
		ctx.replaceList(args.(*jsonx.Object), "jobs", asList(jobs), removed)
		job = nil
	}
	if value, ok := changes.Get("rule_update.args.schedule"); ok {
		if job != nil {
			setItem(job, "schedule", value)
		} else {
			disabled := ctx.settings.SetDefault("_common_disabled", jsonx.NewObject())
			holder := setDefault(disabled, "rules_cron_job", jsonx.Obj("name", "refresh_rules", "executors", []any{"$rules_refresh"}))
			setItem(holder, "schedule", value)
		}
	}
	if value, ok := changes.Get("rule_update.args.timezone"); ok {
		putPath(item(ctx.ensure("rules_cron", jsonx.Obj("jobs", []any{}), ""), "args"), []string{"timezone"}, value)
	}
	if cron := ctx.plugin("rules_cron"); cron != nil && strIn(cron.Value("type"), "cron") && isDict(cron.Value("args")) &&
		jsonx.Equal(get(cron.Value("args"), "jobs", nil), []any{}) {
		ctx.park("rules_cron", cron)
		ctx.removeFromPlugins(cron)
		delete(ctx.byTag, "rules_cron")
	}
}

// patchResources is _patch_resources(ctx, changes, config, base_url).
func patchResources(ctx *commonContext, changes *jsonx.Object, config any, baseURL string) {
	if changes.Has("custom_hosts") {
		ctx.parked.Pop("native_hosts_source")
		content := ""
		if value := changes.Value("custom_hosts"); truthy(value) {
			content = value.(string)
		}
		previous := ctx.settings.GetOr("custom_hosts", "")
		if pystr.Strip(content) != "" {
			hostsFileContent(content)
			setItem(item(ctx.ensure("custom_hosts", jsonx.Obj("files", []any{"./rules/custom_hosts.txt"}), ""), "args"),
				"files", []any{"./rules/custom_hosts.txt"})
			main, isSequence := ctx.sequence("sequence_main")
			call := jsonx.Obj("exec", "$custom_hosts")
			if !inList(main, call, isSequence) {
				index := 0
				for i, element := range main {
					if strIn(get(element, "exec", nil), "metrics_collector metrics", "$query_recorder", "prefer_ipv4") {
						continue
					}
					first := ""
					if matches := get(element, "matches", nil); truthy(matches) {
						first = str(index0(matches))
					}
					if !strings.HasPrefix(first, "qtype ") {
						index = i
						break
					}
				}
				ctx.setSequence("sequence_main", insertCopy(main, index, call, jsonx.Obj("matches", []any{"has_resp"}, "exec", "accept")))
			}
		} else if ctx.plugin("custom_hosts") != nil {
			args := ctx.args("custom_hosts")
			setItem(args, "files", []any{})
			entries := get(args, "entries", nil)
			if entries == nil || jsonx.Equal(entries, []any{}) {
				if entries == nil {
					popKey(args, "entries")
				}
				main, isSequence := ctx.sequence("sequence_main")
				if !isSequence {
					raise(pyErr("TypeError", "'NoneType' object is not iterable"))
				}
				index := -1
				for i, element := range main {
					if jsonx.Equal(element, jsonx.Obj("exec", "$custom_hosts")) {
						index = i
						break
					}
				}
				if index >= 0 {
					main = popCopy(main, index)
					if index < len(main) && jsonx.Equal(main[index], jsonx.Obj("matches", []any{"has_resp"}, "exec", "accept")) {
						main = popCopy(main, index)
					}
					ctx.setSequence("sequence_main", main)
				}
				if len(referenceLocations(ctx.doc, []string{"custom_hosts"}, true)) == 0 {
					ctx.parkPlugin("custom_hosts")
				}
			}
		}
		referenced := ctx.resourcePaths()
		if pystr.Strip(content) == "" && referenced["rules/custom_hosts.txt"] {
			ctx.settings.Set("custom_hosts", previous)
		} else {
			ctx.settings.Set("custom_hosts", content)
		}
	}
	touched := false
	for _, key := range changes.Keys() {
		if strings.HasPrefix(key, "adguard.") {
			touched = true
		}
	}
	if !touched {
		return
	}
	currentURLs, hasContent, _ := resources(ctx)
	resource := ctx.settings.SetDefault("adguard", jsonx.NewObject())
	resourceObj, ok := resource.(*jsonx.Object)
	if !ok || resourceObj == nil {
		resourceObj = jsonx.NewObject()
		ctx.settings.Set("adguard", resourceObj)
	}
	oldResource := copyValue(resourceObj).(*jsonx.Object)
	var urls any = currentURLs
	urlsFromChanges := changes.Has("adguard.urls")
	if urlsFromChanges {
		urls = changes.Value("adguard.urls")
	}
	var defaultContent any = ""
	if hasContent {
		defaultContent = resourceObj.GetOr("content", "")
	}
	contentValue := changes.GetOr("adguard.content", defaultContent)
	if !truthy(contentValue) {
		contentValue = ""
	}
	if truthy(changes.Value("adguard.enabled")) && isDict(get(ctx.parked, "adguard_rules", nil)) {
		ctx.ensure("adguard_rules", nil, "")
	}
	for _, url := range pyIter(urls) {
		text, isText := url.(string)
		if !isText || !(strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://")) {
			fail("AdGuard 订阅必须为 HTTP(S) URL")
		}
	}
	if changes.Has("adguard.urls") || changes.Has("adguard.content") || (truthy(changes.Value("adguard.enabled")) && ctx.plugin("adguard_rules") == nil) {
		provider := ctx.ensure("adguard_rules", jsonx.Obj("files", []any{}), "")
		content := strip(contentValue)
		files := []any{}
		if content != "" {
			files = append(files, "./rules/adguard-custom.txt")
		}
		for _, url := range pyIter(urls) {
			files = append(files, "./"+adguardPath(url))
		}
		setItem(item(provider, "args"), "files", files)
		resourceObj.Set("urls", urls)
		resourceObj.Set("content", contentValue)
		resourceObj.Set("enabled", true)
		referenced := ctx.resourcePaths()
		for _, previous := range pyIter(oldResource.GetOr("urls", []any{})) {
			current := asList(resourceObj.Value("urls"))
			if isStr(previous) && referenced[adguardPath(previous)] && !inList(current, previous, true) {
				grown := appendCopy(current, previous)
				resourceObj.Set("urls", grown)
				if urlsFromChanges {
					changes.Set("adguard.urls", grown)
				}
			}
		}
		if content == "" && referenced["rules/adguard-custom.txt"] {
			resourceObj.Set("content", oldResource.GetOr("content", ""))
		}
	}
	if mode, ok := changes.Get("adguard.mode"); ok {
		if !strIn(mode, "nxdomain", "nodata", "null", "refused") {
			fail("AdGuard 响应模式无效")
		}
		setItem(item(ctx.ensure("adguard_block", jsonx.Obj("mode", "nxdomain"), ""), "args"), "mode", mode)
	}
	if value, ok := changes.Get("adguard.enabled"); ok {
		main, isSequence := ctx.sequence("sequence_main")
		call := adguardCall()
		if truthy(value) {
			ctx.ensure("adguard_block", jsonx.Obj("mode", "nxdomain"), "")
			ctx.ensure("adguard_reject", adguardReject(), "sequence")
			main, isSequence = ctx.sequence("sequence_main")
			if !inList(main, call, isSequence) {
				index := len(main) - 1
				if index < 0 {
					index = 0
				}
				for i, element := range main {
					text := str(get(element, "exec", ""))
					if strings.HasPrefix(text, "goto ") || strings.HasPrefix(text, "jump ") {
						index = i
						break
					}
				}
				ctx.setSequence("sequence_main", insertCopy(main, index, call))
			}
		} else {
			if !isSequence {
				raise(pyErr("TypeError", "'NoneType' object is not iterable"))
			}
			ctx.setSequence("sequence_main", listWithout(main, call))
		}
	}
	if active, cap := adguardState(ctx); !active && cap.GetBool("editable") {
		for _, tag := range []string{"adguard_rules", "adguard_reject", "adguard_block"} {
			ctx.parkPlugin(tag)
		}
		if reload := ctx.plugin("rules_reload"); reload != nil && strIn(reload.Value("type"), "reload_provider") {
			if list, ok := reload.Value("args").([]any); ok {
				reload.Set("args", listWithout(list, "$adguard_rules"))
			}
		}
	}
	if (changes.Has("adguard.urls") || changes.Has("adguard.content") || changes.Has("adguard.enabled")) && ctx.plugin("rules_download") != nil {
		downloader := ctx.plugin("rules_download")
		reload := ctx.plugin("rules_reload")
		downloaderArgs, argsOK := downloader.Value("args").(*jsonx.Object)
		if !strIn(downloader.Value("type"), "download") || !argsOK || downloaderArgs == nil {
			fail("规则更新使用自定义下载/重载结构，请在完整参数中调整")
		}
		if _, ok := downloaderArgs.Value("downloads").([]any); !ok ||
			(reload != nil && (!strIn(reload.Value("type"), "reload_provider") || !isList(reload.Value("args")))) {
			fail("规则更新使用自定义下载/重载结构，请在完整参数中调整")
		}
		known := map[string]bool{}
		for _, url := range append(append([]any{}, pyIter(oldResource.GetOr("urls", []any{}))...), pyIter(resourceObj.GetOr("urls", []any{}))...) {
			if isStr(url) {
				known[adguardPath(url)] = true
			}
		}
		localPath := func(entry any) (string, bool) {
			if !isDict(entry) {
				return "", false
			}
			dir, ok1 := get(entry, "dir", nil).(string)
			filename, ok2 := get(entry, "filename", nil).(string)
			if !ok1 || !ok2 {
				return "", false
			}
			return strings.TrimPrefix(strings.TrimRight(dir, "/")+"/"+filename, "./"), true
		}
		desired := jsonx.NewObject()
		for _, entry := range resourceDownloads(ctx.doc, ctx.settings, config, baseURL) {
			if location, ok := localPath(entry); ok && known[location] {
				desired.Set(location, entry)
			}
		}
		output := []any{}
		for _, entry := range item(downloaderArgs, "downloads").([]any) {
			location, ok := localPath(entry)
			if !ok || !known[location] {
				output = append(output, entry)
			} else if desired.Has(location) {
				output = append(output, entry)
				desired.Delete(location)
			}
		}
		replaceDownloadEntries(downloader, append(output, desired.Values()...))
		if reload != nil && ctx.plugin("adguard_rules") != nil {
			list := reload.Value("args").([]any)
			if listIndex(list, "$adguard_rules") < 0 {
				ctx.replaceList(reload, "args", list, appendCopy(list, "$adguard_rules"))
			}
		}
	}
}

// index0 is value[0] for a truthy matches value.
func index0(value any) any {
	switch x := value.(type) {
	case []any:
		return x[0]
	case string:
		for _, r := range x {
			return string(r)
		}
	case *jsonx.Object:
		raise(pyErr("KeyError", "0"))
	}
	raise(pyErr("TypeError", "'"+typeName(value)+"' object is not subscriptable"))
	return nil
}

// ApplyCommonSettings is apply_common_settings(settings, changes, config,
// base_url): patches the requested common fields into the native draft and
// returns {"settings", "values", "capabilities", ...}.
func ApplyCommonSettings(settings, changes, config any, baseURL string) (result *jsonx.Object, err error) {
	defer catch(&err)
	return applyCommon(settings, changes, config, baseURL), nil
}

func applyCommon(settings, changesValue, config any, baseURL string) *jsonx.Object {
	changes, ok := changesValue.(*jsonx.Object)
	if !ok || changes == nil || changes.Len() == 0 {
		fail("changes 必须为非空参数对象")
	}
	commonInit()
	var unknown []string
	for _, field := range changes.Keys() {
		if !commonTables.fields[field] && !RoutingFields[field] {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		fail("不支持的常用参数：" + strings.Join(unknown, ", "))
	}
	current := prepareEditor(settings, config, baseURL)
	document := loadNativeYAML(item(current, "native_config"), DefaultNativeLabel)
	projection := readCommon(document, current, config)
	projected := projection.GetObject("capabilities")
	for field, value := range changes.All() {
		capability := get(projected, field, jsonx.NewObject())
		_, isAPI := commonTables.api[field]
		enablingAPI := isAPI && changes.Value("api.enabled") == true && truthy(get(item(projected, "api.enabled"), "editable", nil))
		if !truthy(get(capability, "editable", nil)) && !enablingAPI {
			reason := get(capability, "reason", "请在完整参数中编辑")
			text, isText := reason.(string)
			if !isText {
				raise(pyErr("TypeError", `can only concatenate str (not "`+typeName(reason)+`") to str`))
			}
			fail(field + "：" + text)
		}
		validateValue(field, value)
	}
	before := copyValue(document)
	routing := jsonx.NewObject()
	for field, value := range changes.All() {
		if RoutingFields[field] {
			routing.Set(field, value)
		}
	}
	if routing.Len() > 0 {
		document, current = applyRouting(document, current, config, routing, baseURL)
	}
	ctx := newCommonContext(document, current)
	fallbackEnabled := prepareFallback(ctx, changes)
	if value, ok := changes.Get("api.enabled"); ok {
		patchAPI(ctx, "api.enabled", value)
	}
	for _, control := range featureControls() {
		if value, ok := changes.Get(control.field); ok {
			ctx.toggleCall(control.tag, control.chains, truthy(value), defaultArgs(control.tag))
		}
	}
	keys := changes.Keys()
	for _, key := range keys {
		if strings.HasPrefix(key, "dynamic_domain_set.") {
			patchDynamic(ctx, changes)
			break
		}
	}
	patchResources(ctx, changes, config, baseURL)
	prepareRuleUpdate(ctx, changes, config, baseURL)
	for _, key := range keys {
		if strings.HasPrefix(key, "rule_update.") {
			patchRuleUpdate(ctx, changes, config, baseURL)
			break
		}
	}
	patchCacheDump(ctx, changes)
	patchTLS(ctx, changes)
	for field, value := range changes.All() {
		if strIn(field, "cache_dump_enabled", "cache_dump_file", "cache_dump_interval") {
			continue
		}
		tls := false
		for _, name := range []string{"ros_address_list", "ros_route"} {
			if field == name+".args.tls" || strings.HasPrefix(field, name+".args.tls.") {
				tls = true
			}
		}
		if tls {
			continue
		}
		_, isDirect := commonTables.byField[field]
		_, isAPI := commonTables.api[field]
		switch {
		case isDirect:
			patchDirect(ctx, field, value)
		case isAPI:
			patchAPI(ctx, field, value)
		case strIn(field, "prefer_ipv4", "metrics_enabled", "block_qtypes"):
			patchMain(ctx, field, value)
		case strIn(field, "local_dns", "remote_dns", "fallback_dns"):
			upstreams := loadUpstreams(value)
			if field == "fallback_dns" && upstreams == nil {
				upstreams = []any{}
			}
			checkUpstreams(upstreams)
			setItem(item(ctx.ensure("forward_"+strings.SplitN(field, "_", 2)[0], nil, ""), "args"), "upstreams", upstreams)
		case field == "listen":
			setItem(item(ctx.ensure("server_udp", jsonx.Obj("entry", "sequence_main"), ""), "args"), "listen", value)
			if ctx.plugin("server_tcp") != nil {
				setItem(ctx.args("server_tcp"), "listen", value)
			}
		case field == "tcp_enabled":
			if truthy(value) {
				args := copyValue(ctx.args("server_udp"))
				parked := get(ctx.parked, "server_tcp", nil)
				if ctx.plugin("server_tcp") == nil && isDict(parked) && strIn(get(parked, "type", nil), "tcp_server") {
					restored := copyValue(parked).(*jsonx.Object)
					target := item(restored, "args")
					targetObj, ok := target.(*jsonx.Object)
					if !ok || targetObj == nil {
						raise(attributeError(target, "update"))
					}
					update := jsonx.NewObject()
					for _, key := range []string{"listen", "entry"} {
						if pyContains(args, key) {
							update.Set(key, item(args, key))
						}
					}
					targetObj.Update(update)
					ctx.appendPlugin(restored)
					ctx.byTag["server_tcp"] = restored
				} else {
					ctx.ensure("server_tcp", args, "")
				}
			} else if tcp := ctx.plugin("server_tcp"); tcp != nil {
				ctx.park("server_tcp", tcp)
				ctx.removeFromPlugins(tcp)
				delete(ctx.byTag, "server_tcp")
			}
		case field == "remote_ttl":
			items, isSequence := ctx.sequence("remote_dns")
			if !isSequence {
				raise(pyErr("TypeError", "'NoneType' object is not iterable"))
			}
			index := -1
			for i, element := range items {
				if strings.HasPrefix(str(get(element, "exec", "")), "ttl ") {
					index = i
					break
				}
			}
			if index >= 0 {
				if truthy(value) {
					items[index] = jsonx.Obj("exec", "ttl "+str(value))
				} else {
					ctx.setSequence("remote_dns", popCopy(items, index))
				}
			} else if truthy(value) {
				ctx.setSequence("remote_dns", appendCopy(items, jsonx.Obj("exec", "ttl "+str(value))))
			}
		}
	}
	for _, key := range keys {
		if chainFields[key] {
			refreshRuleHelpers(ctx.doc)
			ctx.byTag = map[string]*jsonx.Object{}
			for _, element := range iterate(item(ctx.doc, "plugins")) {
				if obj, ok := element.(*jsonx.Object); ok && obj != nil {
					if tag, ok := obj.Value("tag").(string); ok {
						ctx.byTag[tag] = obj
					}
				}
			}
			break
		}
	}
	finishFallback(ctx, fallbackEnabled)
	parkDisabledFeatures(ctx, changes)
	finishRuleUpdates(ctx, changes, config, baseURL)
	if !sameValue(document, before) {
		current.Set("native_config", dump(document))
	}
	normalizeSettings(current, false)
	out := jsonx.Obj("settings", current)
	out.Update(readCommon(document, current, config))
	return out
}
