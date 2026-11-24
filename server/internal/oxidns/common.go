package oxidns

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Common controls are a projection of the current native draft: only the
// requested fields are patched; ambiguous or custom execution paths stay
// editable in the complete editor and are reported as not editable here.

var (
	features      = []string{"query_recorder", "ip_selector", "ros_address_list", "ros_route"}
	wrapperChains = []string{"china_dns", "direct_ip", "default_direct_ip"}
	chainFields   = []string{"cache_enabled", "cache_lazy_ttl", "remote_ttl", "ip_selector.enabled", "ros_address_list.enabled", "ros_route.enabled"}
	// tagTypes are the plugin types of the managed tags (others are sequences).
	tagTypes = map[string]string{"lazy_cache": "cache", "local_probe_cache": "cache",
		"forward_local": "forward", "forward_remote": "forward", "forward_fallback": "forward",
		"fallback": "fallback", "dynamic_domains": "dynamic_domain_set",
		"learn_classified_domain": "learn_domain", "adguard_rules": "adguard_rule",
		"adguard_block": "black_hole", "custom_hosts": "hosts",
		"server_udp": "udp_server", "server_tcp": "tcp_server",
		"rules_download": "download", "rules_reload": "reload_provider", "rules_cron": "cron",
		"query_recorder": "query_recorder", "ip_selector": "ip_selector", "ros_address_list": "ros_address_list", "ros_route": "ros_route"}
)

// directField maps a common field to a value inside a plugin's args (tag
// "" = the document itself).
type directField struct {
	field string
	tag   string
	path  []string
}

var directFields, apiFields, commonFields = func() ([]directField, map[string][]string, map[string]bool) {
	direct := []directField{
		{"worker_threads", "", []string{"runtime", "worker_threads"}},
		{"log.level", "", []string{"log", "level"}}, {"log.file", "", []string{"log", "file"}},
		{"log.rotation", "", []string{"log", "rotation", "type"}},
		{"log.max_files", "", []string{"log", "rotation", "max_files"}},
		{"cache_size", "lazy_cache", []string{"size"}}, {"cache_lazy_ttl", "lazy_cache", []string{"lazy_cache_ttl"}},
		{"cache_dump_file", "lazy_cache", []string{"dump_file"}}, {"cache_dump_interval", "lazy_cache", []string{"dump_interval"}},
		{"probe_cache_size", "local_probe_cache", []string{"size"}},
		{"fallback_threshold", "fallback", []string{"threshold"}}, {"always_standby", "fallback", []string{"always_standby"}},
	}
	for _, name := range []string{"local", "remote", "fallback"} {
		direct = append(direct, directField{name + "_concurrent", "forward_" + name, []string{"concurrent"}})
	}
	var leaves func(prefix []string, value *Node, emit func([]string))
	leaves = func(prefix []string, value *Node, emit func([]string)) {
		pairs(value, func(key string, item *Node) bool {
			p := append(append([]string{}, prefix...), key)
			if isMap(item) {
				leaves(p, item, emit)
			} else {
				emit(p)
			}
			return true
		})
	}
	for _, feature := range features {
		leaves(nil, at(defaultSettingsNode, feature, "args"), func(p []string) {
			direct = append(direct, directField{feature + ".args." + strings.Join(p, "."), feature, p})
		})
	}
	for _, pair := range [][2]string{{"args", "dynamic_domains"}, {"learn_args", "learn_classified_domain"}} {
		leaves(nil, at(defaultSettingsNode, "dynamic_domain_set", pair[0]), func(p []string) {
			direct = append(direct, directField{"dynamic_domain_set." + pair[0] + "." + strings.Join(p, "."), pair[1], p})
		})
	}
	for _, feature := range []string{"ros_address_list", "ros_route"} {
		for _, field := range []string{"tls", "tls.server_name", "tls.ca", "tls.insecure"} {
			direct = append(direct, directField{feature + ".args." + field, feature, strings.Split(field, ".")})
		}
	}
	for _, field := range []string{"timeout", "outbound", "socks5"} {
		direct = append(direct, directField{"rule_update.args." + field, "rules_download", []string{field}})
	}
	api := map[string][]string{"api.listen": {"listen"}, "api.username": {"auth", "username"},
		"api.password": {"auth", "password"}, "api.webui_root": {"webui", "root"},
		"api.ssl.cert": {"ssl", "cert"}, "api.ssl.key": {"ssl", "key"}}
	all := map[string]bool{}
	for _, d := range direct {
		all[d.field] = true
	}
	for f := range api {
		all[f] = true
	}
	for _, f := range strings.Fields(`listen tcp_enabled prefer_ipv4 metrics_enabled block_qtypes remote_ttl
local_dns remote_dns fallback_dns cache_enabled cache_dump_enabled probe_cache_enabled api.enabled custom_hosts
dynamic_domain_set.enabled dynamic_domain_set.route dynamic_domain_set.learn adguard.enabled adguard.content
adguard.urls adguard.mode rule_update.enabled rule_update.args.schedule rule_update.args.timezone`) {
		all[f] = true
	}
	for _, name := range features {
		all[name+".enabled"] = true
	}
	return direct, api, all
}()

func findDirect(field string) (directField, bool) {
	for _, d := range directFields {
		if d.field == field {
			return d, true
		}
	}
	return directField{}, false
}

// lookup is _get: the value at path, ok false when missing (or a parent is
// not a mapping).
func lookup(obj *Node, path []string) (*Node, bool) {
	for _, key := range path {
		if !isMap(obj) || !has(obj, key) {
			return nil, false
		}
		obj = get(obj, key)
	}
	return obj, true
}

// put is _put: nil/null removes the value; missing parents are created.
func put(obj *Node, path []string, value *Node) error {
	for _, key := range path[:len(path)-1] {
		child := get(obj, key)
		switch {
		case isNone(child):
			if isNone(value) {
				return nil
			}
			child = M()
			set(obj, key, child)
		case !isMap(child):
			return fail("此参数包含自定义值类型，请在完整参数中编辑")
		}
		obj = child
	}
	if isNone(value) {
		del(obj, path[len(path)-1])
	} else {
		set(obj, path[len(path)-1], clone(value))
	}
	return nil
}

// assign is _value: a projection assignment (null stays null).
func assign(obj *Node, field string, value *Node) {
	path := strings.Split(field, ".")
	for _, key := range path[:len(path)-1] {
		if !isMap(get(obj, key)) {
			set(obj, key, M())
		}
		obj = get(obj, key)
	}
	set(obj, path[len(path)-1], orNull(clone(value)))
}

func matchList(item *Node) []*Node {
	value := get(item, "matches")
	if isStr(value) {
		return []*Node{value}
	}
	return items(value)
}

func referenceCount(value *Node, tag string) int {
	switch {
	case isMap(value), isSeq(value):
		n := 0
		for i, child := range value.Content {
			if value.Kind == 4 && i%2 == 0 {
				continue
			}
			n += referenceCount(child, tag)
		}
		return n
	case isOneOf(value, "$"+tag, "jump "+tag, "goto "+tag):
		return 1
	}
	return 0
}

func tagMentions(value *Node, tag string) int {
	switch {
	case isMap(value), isSeq(value):
		n := 0
		for i, child := range value.Content {
			if value.Kind == 4 && i%2 == 0 {
				continue
			}
			n += tagMentions(child, tag)
		}
		return n
	case isStr(value):
		for _, field := range strings.FieldsFunc(value.Value, func(r rune) bool { return isPySpace(string(r)) }) {
			if field == "$"+tag {
				return 1
			}
		}
		if value.Value == "jump "+tag || value.Value == "goto "+tag {
			return 1
		}
	}
	return 0
}

func safeParent(obj *Node, path []string) bool {
	for _, key := range path[:len(path)-1] {
		if isNone(obj) {
			return true
		}
		if !isMap(obj) {
			return false
		}
		obj = get(obj, key)
	}
	return isNone(obj) || isMap(obj)
}

func capability(editable bool, reason, target string) Capability {
	return Capability{Editable: editable, Reason: reason, Target: target}
}

func neutral(value *Node) *Node {
	switch {
	case isMap(value):
		out := M()
		pairs(value, func(k string, v *Node) bool { set(out, k, neutral(v)); return true })
		return out
	case isSeq(value):
		return L()
	case isBool(value):
		return B(false)
	case isStr(value):
		return S("")
	}
	return Null()
}

// commonContext is one edit of a native draft and its settings.
type commonContext struct {
	doc, settings *Node
	view          settingsView
	plugins       *Node
	byTag         map[string]*Node
	parked        *Node
	includedTags  map[string]bool
}

func newContext(document *nativeDoc, settings *Node) (*commonContext, error) {
	plugins := get(document, "plugins")
	if !has(document, "plugins") {
		plugins = L()
	}
	if !isSeq(plugins) {
		return nil, fail("插件结构尚未完成，请在完整参数或 YAML 中继续编辑")
	}
	ctx := &commonContext{doc: document, settings: settings, view: readSettingsView(settings), plugins: plugins, byTag: map[string]*Node{}, includedTags: map[string]bool{}}
	for _, item := range plugins.Content {
		tag, ok := tagOf(item)
		if !isMap(item) || !ok {
			return nil, fail("插件结构尚未完成，请在完整参数或 YAML 中继续编辑")
		}
		ctx.byTag[tag] = item
	}
	ctx.parked = get(settings, "_common_disabled")
	if !isMap(ctx.parked) {
		ctx.parked = M()
	}
	if externalIncludes(document) {
		expanded, err := NativeDocument(settings, false, nil)
		if err != nil {
			return nil, err
		}
		for _, item := range items(get(expanded, "plugins")) {
			if tag := str(get(item, "tag")); ctx.byTag[tag] == nil {
				ctx.includedTags[tag] = true
			}
		}
	}
	return ctx, nil
}

func (c *commonContext) plugin(tag string) *Node { return c.byTag[tag] }

func (c *commonContext) aliasCapability(tag string) *Capability {
	if alias, ok := c.view.renamedPlugin(tag); ok && alias != tag && c.plugin(alias) != nil {
		cap := capability(false, "受管插件已重命名为「"+alias+"」，请在完整参数中编辑自定义配置", alias)
		return &cap
	}
	return nil
}

func (c *commonContext) allowed(tag, kind string) Capability {
	if alias := c.aliasCapability(tag); alias != nil {
		return *alias
	}
	if c.includedTags[tag] {
		return capability(false, "此插件来自 include 文件，请在完整参数或附加文件中编辑", tag)
	}
	if kind == "" {
		kind = tagType(tag)
	}
	plugin := c.plugin(tag)
	if plugin != nil && !equal(get(plugin, "type"), S(kind)) {
		return capability(false, "同名插件使用了其他类型，请在完整参数中编辑", tag)
	}
	if plugin != nil && !isNone(get(plugin, "args")) && !isMap(get(plugin, "args")) {
		return capability(false, "插件参数使用了自定义值类型，请在完整参数中编辑", tag)
	}
	return Capability{Editable: true, Target: tag, Absent: plugin == nil}
}

// args is the plugin's args ({} when missing or null).
func (c *commonContext) args(tag string) *Node {
	plugin := c.plugin(tag)
	if plugin == nil {
		return M()
	}
	if args := get(plugin, "args"); !isNone(args) {
		return args
	}
	return M()
}

func (c *commonContext) storedArgs(tag string) *Node {
	if alias := c.aliasCapability(tag); alias != nil && c.plugin(tag) == nil {
		return c.args(alias.Target)
	}
	plugin := c.plugin(tag)
	if plugin == nil {
		plugin = get(c.parked, tag)
	}
	if isMap(plugin) && equal(get(plugin, "tag"), S(tag)) && equal(get(plugin, "type"), S(tagType(tag))) {
		if args := get(plugin, "args"); !isNone(args) {
			return args
		}
		return M()
	}
	return c.args(tag)
}

func (c *commonContext) appendPlugin(plugin *Node) {
	list := setDefault(c.doc, "plugins", L())
	list.Content = append(list.Content, plugin)
	c.byTag[str(get(plugin, "tag"))] = plugin
}

// ensure returns the plugin, restoring a parked one or creating it.
func (c *commonContext) ensure(tag string, args *Node, kind string) (*Node, error) {
	if kind == "" {
		kind = tagType(tag)
	}
	if args == nil {
		args = M()
	}
	plugin := c.plugin(tag)
	parked := get(c.parked, tag)
	if plugin == nil && isMap(parked) && equal(get(parked, "tag"), S(tag)) && equal(get(parked, "type"), S(kind)) {
		plugin = clone(parked)
		c.appendPlugin(plugin)
	}
	if plugin == nil {
		plugin = M("tag", tag, "type", kind, "args", clone(args))
		c.appendPlugin(plugin)
	} else if !equal(get(plugin, "type"), S(kind)) {
		return nil, fail("插件「" + tag + "」已使用其他类型")
	}
	if isNone(get(plugin, "args")) {
		set(plugin, "args", clone(args))
	}
	return plugin, nil
}

func (c *commonContext) sequence(tag string) *Node {
	plugin := c.plugin(tag)
	if args := get(plugin, "args"); equal(get(plugin, "type"), S("sequence")) && isSeq(args) {
		return args
	}
	return nil
}

func (c *commonContext) park(name string, value *Node) {
	parked := setDefault(c.settings, "_common_disabled", M())
	if isMap(parked) {
		set(parked, name, clone(value))
	}
	c.parked = parked
}

// removePlugin is list.remove: the first plugin equal to plugin.
func (c *commonContext) removePlugin(plugin *Node) {
	list := get(c.doc, "plugins")
	for i, item := range items(list) {
		if equal(item, plugin) {
			list.Content = append(list.Content[:i:i], list.Content[i+1:]...)
			return
		}
	}
}

func (c *commonContext) parkPlugin(tag string) {
	if plugin := c.plugin(tag); plugin != nil {
		c.park(tag, plugin)
		c.removePlugin(plugin)
		delete(c.byTag, tag)
	}
}

// expanded is the document with includes expanded (from the current draft).
func (c *commonContext) expanded() (*Node, error) {
	if !(externalIncludes(c.doc)) {
		return c.doc, nil
	}
	settings := shallow(c.settings)
	if err := setNativeConfig(settings, c.doc); err != nil {
		return nil, err
	}
	return NativeDocument(settings, false, nil)
}

func (c *commonContext) resourcePaths() (map[string]bool, error) {
	document, err := c.expanded()
	if err != nil {
		return nil, err
	}
	return referencedPaths(document)
}

// callState: whether tag is called from chains, and whether that call can
// be toggled here.
func (c *commonContext) callState(tag string, chains []string) (bool, Capability) {
	cap := c.allowed(tag, "")
	if !cap.Editable {
		alias := c.aliasCapability(tag)
		return alias != nil && len(ReferenceLocations(c.doc, []string{alias.Target}, false)) > 0, cap
	}
	if externalIncludes(c.doc) {
		return false, capability(false, "配置包含外部执行链，请在完整参数中调整功能调用", tag)
	}
	expected := M("exec", "$"+tag)
	counts := map[string]int{}
	for _, chain := range chains {
		counts[chain] = 0
	}
	owned := map[string]bool{}
	if containsString(RealWrappers, tag) {
		for chain := range OwnedChains(c.doc) {
			owned[chain] = true
		}
	}
	custom, anyCall := false, false
	for _, plugin := range c.plugins.Content {
		ptag := str(get(plugin, "tag"))
		if !isSeq(get(plugin, "args")) || owned[ptag] {
			continue
		}
		for _, item := range get(plugin, "args").Content {
			if action := get(item, "exec"); isMap(item) && isOneOf(action, "$"+tag, "jump "+tag, "goto "+tag) {
				anyCall = true
				if _, ok := counts[ptag]; ok && equal(item, expected) {
					counts[ptag]++
				} else {
					custom = true
				}
			}
		}
	}
	remaining := clone(c.doc)
	for _, plugin := range items(get(remaining, "plugins")) {
		ptag := str(get(plugin, "tag"))
		if _, ok := counts[ptag]; (ok || owned[ptag]) && isSeq(get(plugin, "args")) {
			set(plugin, "args", seqOf(filter(get(plugin, "args").Content, func(item *Node) bool { return !equal(item, expected) })))
		}
	}
	if refs := ReferenceLocations(remaining, []string{tag}, true); len(refs) > 0 {
		called := anyCall
		for _, r := range refs {
			called = called || r.Tag == tag
		}
		return called, capability(false, ReferenceReason(refs, DefaultReferencePrefix), tag)
	}
	partial := false
	for _, n := range counts {
		partial = partial || n > 1 || anyCall && n != 1
	}
	if custom || partial {
		return anyCall, capability(false, "功能使用了自定义或部分接入的执行链，请在完整参数中调整调用", tag)
	}
	for _, chain := range chains {
		if c.sequence(chain) == nil {
			return anyCall, capability(false, "当前没有可识别的 ConfigFlow 处理链，请在完整参数中接入功能", tag)
		}
	}
	return anyCall, cap
}

func insertAt(list *Node, index int, items ...*Node) {
	list.Content = append(list.Content[:index:index], append(items, list.Content[index:]...)...)
}

func (c *commonContext) toggleCall(tag string, chains []string, enabled bool, defaultArgs *Node) error {
	if enabled {
		if _, err := c.ensure(tag, defaultArgs, ""); err != nil {
			return err
		}
	}
	saved := get(c.parked, "feature_calls")
	if isMap(saved) {
		saved = clone(saved)
	} else {
		saved = M()
	}
	expected := M("exec", "$"+tag)
	for _, chain := range chains {
		list := c.sequence(chain)
		if list == nil {
			return errMalformed
		}
		position := mapOr(get(saved, chain))
		original, disabled := get(position, "items"), get(position, "disabled")
		if !isSeq(original) {
			original = L()
		}
		if !isSeq(disabled) {
			disabled = L()
		}
		switch {
		case enabled && !contains(list, expected):
			index := 0
			if contains(disabled, S(tag)) && contains(original, expected) {
				index = len(list.Content)
				for _, item := range original.Content[indexOf(original, expected)+1:] {
					if at := indexOf(list, item); at >= 0 {
						index = at
						break
					}
				}
			} else if chain == "sequence_main" {
				index = len(list.Content)
				for i, item := range list.Content {
					if !isOneOf(get(item, "exec"), "metrics_collector metrics", "$query_recorder") {
						index = i
						break
					}
				}
			} else if tag == "lazy_cache" {
				for i, item := range list.Content {
					if equal(get(item, "exec"), S("$forward_local")) {
						index = i
						break
					}
				}
			}
			insertAt(list, index, expected)
			if at := indexOf(disabled, S(tag)); at >= 0 {
				disabled.Content = append(disabled.Content[:at:at], disabled.Content[at+1:]...)
			}
			if len(disabled.Content) == 0 {
				del(saved, chain)
			}
		case !enabled && contains(list, expected):
			if len(disabled.Content) == 0 {
				position = M("items", clone(list), "disabled", L())
				set(saved, chain, position)
				disabled = get(position, "disabled")
			}
			if !contains(disabled, S(tag)) {
				disabled.Content = append(disabled.Content, S(tag))
			}
			list.Content = filter(list.Content, func(item *Node) bool { return !equal(item, expected) })
		case !enabled:
			list.Content = filter(list.Content, func(item *Node) bool { return !equal(item, expected) })
		}
	}
	if len(saved.Content) > 0 || has(c.parked, "feature_calls") {
		c.park("feature_calls", saved)
	}
	return nil
}

// ---- proxy fallback ----

var (
	fallbackPlugins   = []string{"forward_fallback", "fallback_dns", "fallback"}
	fallbackFields    = []string{"fallback_dns", "fallback_concurrent", "fallback_threshold", "always_standby"}
	fallbackOn        = func() *Node { return L(M("exec", "$fallback"), M("exec", "accept")) }
	fallbackOff       = func() *Node { return L(M("exec", "goto remote_dns")) }
	fallbackSecondary = func() *Node {
		return L(M("exec", "$forward_fallback"), M("matches", L("!has_resp"), "exec", "reject 3"))
	}
)

const (
	fallbackReason        = "代理回退使用了自定义执行链、额外引用或 include，请在完整参数中调整，不能自动切换"
	fallbackDynamicReason = "执行链包含未展开的环境变量引用，请在完整参数中确认引用后调整备用 DNS"
)

// fallbackState recognizes only the owned primary/secondary graph.
func fallbackState(c *commonContext) (bool, Capability) {
	calls := c.sequence("proxy_dns_seq")
	enabled := same(calls, fallbackOn())
	for _, tag := range append(append([]string{}, fallbackPlugins...), "proxy_dns_seq", "remote_dns") {
		if alias := c.aliasCapability(tag); alias != nil {
			return enabled, *alias
		}
	}
	denied := capability(false, fallbackReason, "proxy_dns_seq")
	if len(ReferenceLocations(c.doc, nil, true)) > 0 {
		return enabled, capability(false, fallbackDynamicReason, "proxy_dns_seq")
	}
	if (externalIncludes(c.doc)) || c.sequence("remote_dns") == nil ||
		!same(calls, fallbackOn()) && !same(calls, fallbackOff()) && !same(calls, L(M("exec", "$remote_dns"), M("exec", "accept"))) {
		return enabled, denied
	}
	for _, tag := range append(append([]string{}, fallbackPlugins...), "proxy_dns_seq", "remote_dns") {
		if len(filter(c.plugins.Content, func(p *Node) bool { return equal(get(p, "tag"), S(tag)) })) > 1 {
			return enabled, denied
		}
	}
	kinds := map[string]string{"forward_fallback": "forward", "fallback_dns": "sequence", "fallback": "fallback"}
	for _, tag := range fallbackPlugins {
		plugin := c.plugin(tag)
		if enabled && plugin == nil {
			return enabled, denied
		}
		if plugin == nil {
			plugin = get(c.parked, tag)
		}
		if plugin == nil {
			continue
		}
		if !isMap(plugin) || !equal(get(plugin, "tag"), S(tag)) || !equal(get(plugin, "type"), S(kinds[tag])) {
			return enabled, denied
		}
		args := get(plugin, "args")
		switch {
		case tag == "fallback_dns":
			if !same(args, fallbackSecondary()) {
				return enabled, denied
			}
		case !isMap(args):
			return enabled, denied
		case tag == "fallback" && (!equal(get(args, "primary"), S("remote_dns")) || !equal(get(args, "secondary"), S("fallback_dns"))):
			return enabled, denied
		case tag == "forward_fallback" && has(args, "upstreams") && !isSeq(get(args, "upstreams")):
			return enabled, denied
		}
	}
	// Remove the known owned edges; any remaining reference blocks parking.
	remaining := clone(c.doc)
	for _, plugin := range items(get(remaining, "plugins")) {
		tag := del(plugin, "tag")
		del(plugin, "type")
		switch {
		case equal(tag, S("proxy_dns_seq")), equal(tag, S("fallback_dns")):
			del(plugin, "args")
		case equal(tag, S("fallback")) && isMap(get(plugin, "args")):
			del(get(plugin, "args"), "primary")
			del(get(plugin, "args"), "secondary")
		}
	}
	for _, tag := range fallbackPlugins {
		if refersTo(remaining, tag) {
			return enabled, denied
		}
	}
	return enabled, capability(true, "", "proxy_dns_seq")
}

func touches(changes *Node, fields []string) bool {
	for _, f := range fields {
		if has(changes, f) {
			return true
		}
	}
	return false
}

// prepareFallback restores the fallback plugins before parameter patches;
// it returns the requested state (nil when the fallback is not touched).
func prepareFallback(c *commonContext, changes *Node) (*bool, error) {
	if !touches(changes, fallbackFields) {
		return nil, nil
	}
	enabled, cap := fallbackState(c)
	if !cap.Editable {
		return nil, fail(fallbackReason)
	}
	requested := enabled
	if has(changes, "fallback_dns") {
		upstreams := L()
		if text := str(get(changes, "fallback_dns")); strings.TrimSpace(text) != "" {
			parsed, ok, err := safeLoad(text)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fail("DNS 上游 YAML 语法无效")
			}
			if !isNone(parsed) {
				upstreams = parsed
			}
		}
		if !isSeq(upstreams) || len(filter(upstreams.Content, isMap)) != len(upstreams.Content) {
			return nil, fail("DNS 上游必须为参数对象数组")
		}
		requested = len(upstreams.Content) > 0
	}
	for _, step := range []struct {
		tag  string
		args *Node
	}{{"forward_fallback", M("upstreams", L(), "concurrent", 1)}, {"fallback_dns", fallbackSecondary()},
		{"fallback", M("primary", "remote_dns", "secondary", "fallback_dns", "threshold", 500, "always_standby", false)}} {
		if _, err := c.ensure(step.tag, step.args, ""); err != nil {
			return nil, err
		}
	}
	return &requested, nil
}

func finishFallback(c *commonContext, enabled *bool) error {
	if enabled == nil {
		return nil
	}
	upstreams := get(c.args("forward_fallback"), "upstreams")
	if !has(c.args("forward_fallback"), "upstreams") {
		upstreams = L()
	}
	if *enabled {
		bad := !isSeq(upstreams) || len(upstreams.Content) == 0
		for _, item := range items(upstreams) {
			bad = bad || strings.TrimSpace(str(get(item, "addr"))) == ""
		}
		if bad {
			return fail("启用代理回退前请先填写备用 DNS 地址")
		}
		set(c.plugin("proxy_dns_seq"), "args", fallbackOn())
		return nil
	}
	set(c.plugin("proxy_dns_seq"), "args", fallbackOff())
	for _, tag := range fallbackPlugins {
		c.parkPlugin(tag)
	}
	return nil
}

// ---- reads ----

var (
	qtypeList    = regexp.MustCompile(`^qtype(?: [0-9]+)+$`)
	remoteTTLRE  = regexp.MustCompile(`^ttl [0-9]+$`)
	featureCalls = func() []struct {
		field, tag string
		chains     []string
	} {
		out := []struct {
			field, tag string
			chains     []string
		}{{"cache_enabled", "lazy_cache", []string{"china_dns"}}, {"probe_cache_enabled", "local_probe_cache", []string{"local_probe"}}}
		for _, name := range features {
			chains := wrapperChains
			if name == "query_recorder" {
				chains = []string{"sequence_main"}
			}
			out = append(out, struct {
				field, tag string
				chains     []string
			}{name + ".enabled", name, chains})
		}
		return out
	}()
)

func (c *commonContext) directRead(values *Node, caps map[string]Capability) {
	for _, d := range directFields {
		holder := c.doc
		cap := capability(true, "", strings.Join(d.path, "."))
		if d.tag != "" {
			holder = c.storedArgs(d.tag)
			cap = c.allowed(d.tag, "")
		}
		value, ok := lookup(holder, d.path)
		if !safeParent(holder, d.path) {
			cap = capability(false, "父参数使用了自定义值类型，请在完整参数中编辑", cap.Target)
		}
		assign(values, d.field, value)
		if !ok {
			cap.Absent = true
		}
		caps[d.field] = cap
	}
}

// mainControl reads prefer_ipv4 / metrics_enabled / block_qtypes from
// sequence_main: value, capability, index of the owned entry (-1 = none).
func (c *commonContext) mainControl(field string) (*Node, Capability, int) {
	sequence := c.sequence("sequence_main")
	locked := capability(false, "当前使用了条件化、重复或自定义指令，请在完整参数中编辑", "sequence_main")
	if sequence == nil || (externalIncludes(c.doc)) {
		return Null(), capability(false, "当前没有可识别的主处理链，请在完整参数中编辑", "sequence_main"), -1
	}
	editable := capability(true, "", "sequence_main")
	var possible []int
	if field == "block_qtypes" {
		for i, item := range sequence.Content {
			if !isMap(item) || !isOneOf(get(item, "exec"), blockQtypesExecs...) {
				continue
			}
			for _, m := range matchList(item) {
				if strings.HasPrefix(displayValue(m), "qtype ") {
					possible = append(possible, i)
					break
				}
			}
		}
		if len(possible) == 0 {
			return L(), editable, -1
		}
		if len(possible) == 1 {
			item := sequence.Content[possible[0]]
			matches := get(item, "matches")
			if mapSize(item) == 2 && has(item, "matches") && isSeq(matches) && len(matches.Content) == 1 &&
				isStr(matches.Content[0]) && qtypeList.MatchString(matches.Content[0].Value) {
				codes := L()
				for _, code := range strings.Fields(matches.Content[0].Value)[1:] {
					codes.Content = append(codes.Content, uintNode(code))
				}
				return codes, editable, possible[0]
			}
		}
		return Null(), locked, -1
	}
	command, prefix := "metrics_collector metrics", "metrics_collector"
	if field == "prefer_ipv4" {
		command, prefix = "prefer_ipv4", "prefer_ipv4"
	}
	for i, item := range sequence.Content {
		exec := get(item, "exec")
		if !has(item, "exec") {
			exec = S("")
		}
		if isMap(item) && strings.HasPrefix(displayValue(exec), prefix) {
			possible = append(possible, i)
		}
	}
	if len(possible) == 0 {
		return B(false), editable, -1
	}
	if len(possible) == 1 && equal(sequence.Content[possible[0]], M("exec", command)) {
		return B(true), editable, possible[0]
	}
	return Null(), locked, -1
}

// apiHolder is the api.http mapping (parked one when restore) and whether
// the API is enabled.
func (c *commonContext) apiHolder(restore bool) (*Node, bool) {
	api := get(c.doc, "api")
	if !has(c.doc, "api") {
		api = M()
	}
	if !isMap(api) {
		return api, false
	}
	current := get(api, "http")
	if !isNone(current) {
		return current, true
	}
	if restore {
		if parked := get(c.parked, "api_http"); has(c.parked, "api_http") {
			return parked, false
		}
	}
	return M(), false
}

var (
	dynamicDirect = M("matches", L("qname $dynamic_domains"), "exec", "goto china_dns")
	dynamicProxy  = M("matches", L("qname $dynamic_domains"), "exec", "goto proxy_dns_seq")
)

// dynamicState: enabled, route ("" unknown) and capability of the dynamic
// domain set.
func (c *commonContext) dynamicState() (bool, string, Capability) {
	list := c.sequence("sequence_main")
	if list == nil || (externalIncludes(c.doc)) {
		return false, "", capability(false, "当前没有可识别的主处理链，请在完整参数中编辑", "dynamic_domains")
	}
	for _, plugin := range c.plugins.Content {
		if equal(get(plugin, "type"), S("learn_domain")) && !equal(get(plugin, "tag"), S("learn_classified_domain")) &&
			equal(at(plugin, "args", "provider"), S("dynamic_domains")) {
			return true, "", capability(false, "动态域名集还被自定义学习器引用，请在完整参数中调整", "dynamic_domains")
		}
	}
	var matches []*Node
	for _, item := range list.Content {
		for _, m := range matchList(item) {
			if isMap(item) && strings.Contains(displayValue(m), "$dynamic_domains") {
				matches = append(matches, item)
				break
			}
		}
	}
	remaining := clone(c.doc)
	for _, plugin := range items(get(remaining, "plugins")) {
		if equal(get(plugin, "tag"), S("sequence_main")) && isSeq(get(plugin, "args")) {
			set(plugin, "args", seqOf(filter(get(plugin, "args").Content, func(item *Node) bool { return !contains(seqOf(matches), item) })))
		}
		if equal(get(plugin, "tag"), S("learn_classified_domain")) && equal(get(plugin, "type"), S("learn_domain")) && isMap(get(plugin, "args")) {
			del(get(plugin, "args"), "provider")
		}
	}
	if extra := ReferenceLocations(remaining, []string{"dynamic_domains"}, true); len(extra) > 0 {
		called := len(matches) > 0
		for _, r := range extra {
			called = called || r.Tag == "dynamic_domains"
		}
		return called, "", capability(false, ReferenceReason(extra, DefaultReferencePrefix), "dynamic_domains")
	}
	references := 0
	for _, plugin := range c.plugins.Content {
		references += tagMentions(get(plugin, "args"), "dynamic_domains")
	}
	if references != len(matches) {
		return references > 0, "", capability(false, "动态域名集还被其他执行链引用，请在完整参数中调整", "dynamic_domains")
	}
	if len(matches) == 0 {
		return false, "", c.allowed("dynamic_domains", "")
	}
	if len(matches) == 1 && (equal(matches[0], dynamicDirect) || equal(matches[0], dynamicProxy)) {
		route := "proxy"
		if equal(matches[0], dynamicDirect) {
			route = "direct"
		}
		return true, route, c.allowed("dynamic_domains", "")
	}
	return true, "", capability(false, "动态域名使用了自定义匹配，请在完整参数中编辑", "dynamic_domains")
}

// cronJob is the owned refresh job (nil when absent) and its capability.
func (c *commonContext) cronJob() (*Node, Capability) {
	var alias *Capability
	for _, tag := range []string{"rules_cron", "rules_download", "rules_reload", "rules_refresh"} {
		if alias = c.aliasCapability(tag); alias != nil {
			break
		}
	}
	args := c.args("rules_cron")
	jobs := get(args, "jobs")
	if !has(args, "jobs") {
		jobs = L()
	}
	if !isMap(args) || !isSeq(jobs) {
		return nil, capability(false, "定时任务使用了自定义值类型，请在完整参数中编辑", "rules_cron")
	}
	var matching []*Node
	for _, job := range jobs.Content {
		if isMap(job) && (equal(get(job, "name"), S("refresh_rules")) || containsStr(get(job, "executors"), "$rules_refresh")) {
			matching = append(matching, job)
		}
	}
	cap := c.allowed("rules_cron", "")
	if alias != nil {
		cap = *alias
	}
	if len(matching) == 0 {
		return nil, cap
	}
	if len(matching) != 1 || !equal(get(matching[0], "name"), S("refresh_rules")) || !equal(get(matching[0], "executors"), L("$rules_refresh")) {
		return nil, capability(false, "规则更新任务使用了自定义执行器，请在完整参数中编辑", "rules_cron")
	}
	return matching[0], cap
}

// resources maps adguard_rules' files back to subscription URLs: urls,
// whether the custom content file is attached, capability.
func (c *commonContext) resources() ([]*Node, bool, Capability) {
	args := c.storedArgs("adguard_rules")
	files := get(args, "files")
	if !has(args, "files") {
		files = L()
	}
	known := map[string]*Node{}
	for _, url := range c.view.adguardURLs() {
		known["rules/"+tagForString("adguard", url)+".txt"] = S(url)
	}
	if !isMap(args) || !allStrings(files) {
		return nil, false, capability(false, "规则文件使用了自定义格式，请在完整参数中编辑", "adguard_rules")
	}
	cap := c.allowed("adguard_rules", "")
	var urls []*Node
	custom := false
	for _, f := range files.Content {
		p, _, _ := RelativeResource(f)
		switch {
		case p == "rules/adguard-custom.txt":
			custom = true
		case known[p] != nil:
			urls = append(urls, known[p])
		default:
			cap = capability(false, "存在无法映射到订阅地址的自定义规则文件，请在完整参数中编辑", "adguard_rules")
		}
	}
	return urls, custom, cap
}

var adguardCall = M("matches", L("question $adguard_rules"), "exec", "goto adguard_reject")

func (c *commonContext) adguardState() (bool, Capability) {
	main := c.sequence("sequence_main")
	enabled := contains(main, adguardCall)
	for _, tag := range []string{"adguard_rules", "adguard_reject", "adguard_block"} {
		if alias := c.aliasCapability(tag); alias != nil {
			return len(ReferenceLocations(c.doc, []string{alias.Target}, false)) > 0, *alias
		}
	}
	denied := capability(false, "自定义过滤链请在完整参数中调整", "adguard_rules")
	reject := c.plugin("adguard_reject")
	if main == nil || (externalIncludes(c.doc)) ||
		reject != nil && (!equal(get(reject, "type"), S("sequence")) || !equal(get(reject, "args"), L(M("exec", "$adguard_block"), M("exec", "accept")))) ||
		!c.allowed("adguard_block", "").Editable || !c.allowed("adguard_rules", "").Editable {
		return enabled, denied
	}
	if len(filter(main.Content, func(item *Node) bool { return equal(item, adguardCall) })) > 1 {
		return enabled, denied
	}
	remaining := clone(c.doc)
	for _, item := range items(get(remaining, "plugins")) {
		switch {
		case equal(get(item, "tag"), S("sequence_main")):
			set(item, "args", seqOf(filter(items(get(item, "args")), func(e *Node) bool { return !equal(e, adguardCall) })))
		case equal(get(item, "tag"), S("adguard_reject")):
			del(item, "args")
		case equal(get(item, "tag"), S("rules_reload")) && equal(get(item, "type"), S("reload_provider")) && isSeq(get(item, "args")):
			set(item, "args", seqOf(filter(get(item, "args").Content, func(e *Node) bool { return !equal(e, S("$adguard_rules")) })))
		}
	}
	if refs := ReferenceLocations(remaining, []string{"adguard_rules", "adguard_reject", "adguard_block"}, true); len(refs) > 0 {
		called := enabled
		for _, r := range refs {
			called = called || r.Tag != ""
		}
		return called, capability(false, ReferenceReason(refs, DefaultReferencePrefix), "adguard_rules")
	}
	return enabled, capability(true, "", "adguard_rules")
}

// CommonView is read_common_settings' answer.
type CommonView struct {
	Values       *Node
	Capabilities map[string]Capability
	MatchTargets MatchTargets
}

func readCommon(document *nativeDoc, settings *Node, p *Profile) (CommonView, error) {
	c, err := newContext(document, settings)
	if err != nil {
		return CommonView{}, err
	}
	values := neutral(defaultSettingsNode)
	for _, field := range []string{"config_mode", "native_config", "additional_files", "custom_config", "rule_dns_parked"} {
		del(values, field)
	}
	caps := map[string]Capability{}
	c.directRead(values, caps)
	for _, feature := range []string{"ros_address_list", "ros_route"} {
		if tls, ok := lookup(c.storedArgs(feature), []string{"tls"}); !ok || isNone(tls) {
			set(at(values, feature, "args"), "tls", Null())
		}
	}
	for _, name := range []string{"local", "remote", "fallback"} {
		tag := "forward_" + name
		holder := c.args(tag)
		if name == "fallback" {
			holder = c.storedArgs(tag)
		}
		upstreams, ok := lookup(holder, []string{"upstreams"})
		cap := c.allowed(tag, "")
		if ok && !isSeq(upstreams) {
			cap = capability(false, "上游使用了自定义值类型，请在完整参数中编辑", tag)
		}
		text := ""
		if ok {
			text, err = dump(upstreams)
			if err != nil {
				return CommonView{}, err
			}
		}
		set(values, name+"_dns", S(text))
		caps[name+"_dns"] = cap
	}
	fallbackEnabled, fallbackCap := fallbackState(c)
	if !fallbackEnabled && fallbackCap.Editable {
		set(values, "fallback_dns", S(""))
	}
	if !fallbackCap.Editable {
		for _, field := range fallbackFields {
			caps[field] = fallbackCap
		}
	}
	udp, tcp := c.args("server_udp"), c.plugin("server_tcp")
	listen := get(udp, "listen")
	set(values, "listen", orNull(listen))
	caps["listen"] = c.allowed("server_udp", "")
	set(values, "tcp_enabled", B(tcp != nil))
	caps["tcp_enabled"] = c.allowed("server_tcp", "")
	parkedTCP := get(c.parked, "server_tcp")
	restorable := isMap(parkedTCP) && equal(get(parkedTCP, "type"), S("tcp_server")) && isMap(get(parkedTCP, "args")) &&
		(str(get(get(parkedTCP, "args"), "listen")) != "") && (str(get(get(parkedTCP, "args"), "entry")) != "")
	if tcp == nil && !restorable && (c.plugin("server_udp") == nil || !c.allowed("server_udp", "").Editable ||
		!isMap(udp) || str(get(udp, "listen")) == "" || str(get(udp, "entry")) == "") {
		caps["tcp_enabled"] = capability(false, messageTCPPrerequisites, "server_tcp")
	}
	if tcp != nil && c.plugin("server_udp") != nil && (!isMap(udp) || !isMap(get(tcp, "args")) ||
		!equal(get(get(tcp, "args"), "listen"), get(values, "listen")) || !equal(get(get(tcp, "args"), "entry"), get(udp, "entry"))) {
		separate := capability(false, "UDP 与 TCP 使用独立配置，请在完整参数或监听服务中分别编辑", "server_tcp")
		caps["listen"], caps["tcp_enabled"] = separate, separate
	}
	for _, field := range []string{"prefer_ipv4", "metrics_enabled", "block_qtypes"} {
		value, cap, _ := c.mainControl(field)
		set(values, field, value)
		caps[field] = cap
	}
	for _, f := range featureCalls {
		enabled, cap := c.callState(f.tag, f.chains)
		if err := put(values, strings.Split(f.field, "."), B(enabled)); err != nil {
			return CommonView{}, err
		}
		caps[f.field] = cap
	}
	cache := c.storedArgs("lazy_cache")
	dumpOn := isMap(cache) && str(get(cache, "dump_file")) != ""
	set(values, "cache_dump_enabled", B(dumpOn))
	caps["cache_dump_enabled"] = c.allowed("lazy_cache", "")
	if !dumpOn && isMap(cache) {
		for _, field := range []string{"cache_dump_file", "cache_dump_interval"} {
			if has(c.parked, field) && !has(cache, strings.TrimPrefix(field, "cache_")) {
				set(values, field, clone(get(c.parked, field)))
			}
		}
	}
	remote := c.sequence("remote_dns")
	var ttlItems []*Node
	for _, item := range items(remote) {
		if exec := get(item, "exec"); isMap(item) && strings.HasPrefix(displayValue(orEmpty(item, "exec")), "ttl ") {
			ttlItems = append(ttlItems, item)
			_ = exec
		}
	}
	set(values, "remote_ttl", I(0))
	caps["remote_ttl"] = capability(true, "", "remote_dns")
	if remote == nil || len(ttlItems) > 1 || len(ttlItems) == 1 && (len(ttlItems[0].Content) != 2 || !isStr(get(ttlItems[0], "exec")) || !remoteTTLRE.MatchString(get(ttlItems[0], "exec").Value)) {
		set(values, "remote_ttl", Null())
		caps["remote_ttl"] = capability(false, "代理响应 TTL 使用了自定义执行链，请在完整参数中编辑", "remote_dns")
	} else if len(ttlItems) == 1 {
		set(values, "remote_ttl", uintNode(strings.Fields(get(ttlItems[0], "exec").Value)[1]))
	}
	api, apiOn := c.apiHolder(false)
	set(get(values, "api"), "enabled", B(apiOn))
	if isMap(api) {
		caps["api.enabled"] = capability(true, "", "api.http")
	} else {
		caps["api.enabled"] = capability(false, "管理 API 使用原生简写，请在完整参数中编辑", "api.http")
	}
	for field, path := range apiFields {
		value, ok := lookup(api, path)
		editable := apiOn && isMap(api) && safeParent(api, path)
		if auth := get(api, "auth"); path[0] == "auth" && isMap(api) && isMap(auth) && !equal(orDefault(auth, "type", S("basic")), S("basic")) {
			editable = false
		}
		if !ok {
			value = S("")
		}
		assign(values, field, value)
		reason := ""
		if !editable {
			reason = "管理 API 使用自定义认证或原生值类型，请在完整参数中编辑"
			if !apiOn {
				reason = "请先启用 API / WebUI，再编辑其参数"
			}
		}
		caps[field] = Capability{Editable: editable, Reason: reason, Target: "api.http", Absent: !ok}
	}
	dynamicOn, route, dynCap := c.dynamicState()
	dynamic := get(values, "dynamic_domain_set")
	set(dynamic, "enabled", B(dynamicOn))
	set(dynamic, "route", orNull(strOrNil(route)))
	caps["dynamic_domain_set.enabled"], caps["dynamic_domain_set.route"] = dynCap, dynCap
	learnPlugin := c.plugin("learn_classified_domain")
	learnRoute := "ip_requery_proxy"
	if route == "direct" {
		learnRoute = "direct_ip"
	}
	learning, learnCap := c.callState("learn_classified_domain", []string{learnRoute})
	if learnPlugin != nil && !equal(at(learnPlugin, "args", "provider"), S("dynamic_domains")) {
		learnCap = capability(false, "学习器引用了其他域名集，请在完整参数中编辑", "learn_classified_domain")
	}
	set(dynamic, "learn", B(learning))
	caps["dynamic_domain_set.learn"] = learnCap
	learnRefs := 0
	for _, plugin := range c.plugins.Content {
		learnRefs += referenceCount(get(plugin, "args"), "learn_classified_domain")
	}
	if dynCap.Editable && learnRefs > 0 && !learnCap.Editable {
		caps["dynamic_domain_set.enabled"], caps["dynamic_domain_set.route"] = learnCap, learnCap
	}
	job, jobCap := c.cronJob()
	update := get(values, "rule_update")
	set(update, "enabled", B(job != nil))
	caps["rule_update.enabled"] = jobCap
	schedule := Null()
	if job != nil {
		schedule = orNull(get(job, "schedule"))
	}
	set(get(update, "args"), "schedule", schedule)
	timezone, ok := lookup(c.args("rules_cron"), []string{"timezone"})
	if !ok {
		timezone = Null()
	}
	set(get(update, "args"), "timezone", timezone)
	caps["rule_update.args.schedule"], caps["rule_update.args.timezone"] = jobCap, jobCap
	urls, hasContent, resourceCap := c.resources()
	view := c.view
	adguard := get(values, "adguard")
	set(adguard, "urls", seqOf(urls))
	content := S("")
	if hasContent {
		content = memberTextNodeOr(view.AdGuard.Value.Content, "")
	}
	set(adguard, "content", content)
	mode, ok := lookup(c.storedArgs("adguard_block"), []string{"mode"})
	if !ok {
		mode = Null()
	}
	set(adguard, "mode", mode)
	caps["adguard.urls"], caps["adguard.content"] = resourceCap, resourceCap
	caps["adguard.mode"] = c.allowed("adguard_block", "")
	adguardOn, adguardCap := c.adguardState()
	set(adguard, "enabled", B(adguardOn))
	caps["adguard.enabled"] = adguardCap
	hosts := c.args("custom_hosts")
	files := get(hosts, "files")
	if !has(hosts, "files") {
		files = L()
	}
	hostPaths := items(files)
	hostsText := S("")
	if containsStr(seqOf(hostPaths), "./rules/custom_hosts.txt") || containsStr(seqOf(hostPaths), "rules/custom_hosts.txt") {
		hostsText = memberTextNodeOr(view.CustomHosts, "")
	}
	set(values, "custom_hosts", hostsText)
	_, caps["custom_hosts"] = c.callState("custom_hosts", []string{"sequence_main"})
	if c.sequence("sequence_main") == nil || !isMap(hosts) || !isSeq(files) ||
		len(hostPaths) > 0 && !equal(files, L("./rules/custom_hosts.txt")) && !equal(files, L("rules/custom_hosts.txt")) {
		caps["custom_hosts"] = capability(false, "Hosts 使用自定义文件或处理链，请在完整参数中编辑", "custom_hosts")
	}
	routing, err := ReadRouting(document, settings, p)
	if err != nil {
		return CommonView{}, err
	}
	pairs(routing.Values, func(k string, v *Node) bool { set(values, k, v); return true })
	for k, v := range routing.Capabilities {
		caps[k] = v
	}
	targets, err := ReadMatchTargets(document, settings)
	if err != nil {
		return CommonView{}, err
	}
	return CommonView{values, caps, targets}, nil
}

func orDefault(m *Node, key string, fallback *Node) *Node {
	if has(m, key) {
		return get(m, key)
	}
	return fallback
}

func orEmpty(m *Node, key string) *Node { return orDefault(m, key, S("")) }

func strOrNil(s string) *Node {
	if s == "" {
		return nil
	}
	return S(s)
}

// ReadCommonSettings projects the prepared draft onto the common controls.
func ReadCommonSettings(settings *Node, p *Profile, baseURL string) (CommonView, error) {
	prepared, err := PrepareEditor(settings, p, baseURL)
	if err != nil {
		return CommonView{}, err
	}
	document, err := loadNative(get(prepared, "native_config"), NativeLabel)
	if err != nil {
		return CommonView{}, err
	}
	return readCommon(document, prepared, p)
}

// ---- writes ----

func validateValue(field string, value *Node) error {
	if isNone(value) {
		return nil
	}
	example, _ := lookup(defaultSettingsNode, strings.Split(field, "."))
	switch {
	case strings.HasSuffix(field, ".tls"):
		if !isMap(value) {
			return fail(field + " 必须是 TLS 参数对象或 null")
		}
	case strings.HasSuffix(field, ".tls.insecure") || isBool(example):
		if !isBool(value) {
			return fail(field + " 必须为布尔值")
		}
	case isInt(example):
		if !isInt(value) {
			return fail(field + " 必须为整数")
		}
	case isSeq(example):
		if !isSeq(value) {
			return fail(field + " 必须为数组")
		}
		if field == "block_qtypes" && len(filter(value.Content, isInt)) != len(value.Content) {
			return fail("屏蔽查询类型必须为整数数组")
		}
	case isStr(example) || strings.Contains(field, ".tls.") || field == "api.ssl.cert" || field == "api.ssl.key":
		if !isStr(value) {
			return fail(field + " 必须为文本")
		}
	}
	return nil
}

func defaultArgs(tag string) *Node {
	switch {
	case containsString(features, tag):
		return clone(at(defaultSettingsNode, tag, "args"))
	case tag == "lazy_cache":
		return M("size", 10240, "lazy_cache_ttl", 3600, "short_circuit", true)
	case tag == "local_probe_cache":
		return M("size", 2048, "short_circuit", false)
	case tag == "dynamic_domains":
		return clone(at(defaultSettingsNode, "dynamic_domain_set", "args"))
	case tag == "learn_classified_domain":
		args := clone(at(defaultSettingsNode, "dynamic_domain_set", "learn_args"))
		set(args, "provider", S("dynamic_domains"))
		return args
	}
	return M()
}

func (c *commonContext) patchDirect(field string, value *Node) error {
	d, _ := findDirect(field)
	holder := c.doc
	if d.tag != "" {
		plugin, err := c.ensure(d.tag, defaultArgs(d.tag), "")
		if err != nil {
			return err
		}
		holder = get(plugin, "args")
	}
	if field == "cache_lazy_ttl" && numericZero(value) {
		value = nil
	}
	if containsString([]string{"log.file", "rule_update.args.timeout", "rule_update.args.outbound", "rule_update.args.socks5"}, field) && equal(value, S("")) {
		value = nil
	}
	if field == "log.max_files" && !isNone(value) {
		if current, ok := lookup(holder, []string{"log", "rotation", "type"}); !ok || isNone(current) {
			if err := put(holder, []string{"log", "rotation", "type"}, S("never")); err != nil {
				return err
			}
		}
	}
	if err := put(holder, d.path, value); err != nil {
		return err
	}
	if field == "log.rotation" && isNone(value) {
		if rotation, _ := lookup(holder, []string{"log", "rotation"}); isMap(rotation) {
			if len(rotation.Content) > 0 {
				set(rotation, "type", S("never"))
			} else {
				del(get(holder, "log"), "rotation")
			}
		}
	}
	return nil
}

func (c *commonContext) patchAPI(field string, value *Node) error {
	current, enabled := c.apiHolder(true)
	if field == "api.enabled" {
		if isTrue(value) || len(items(value)) > 0 {
			http := M("listen", at(defaultSettingsNode, "api", "listen"))
			if isMap(current) && mapSize(current) > 0 {
				http = clone(current)
			}
			set(setDefault(c.doc, "api", M()), "http", http)
		} else if enabled {
			c.park("api_http", current)
			api := get(c.doc, "api")
			del(api, "http")
			if len(api.Content) == 0 {
				del(c.doc, "api")
			}
		}
		return nil
	}
	var holder *Node
	if enabled {
		holder = at(c.doc, "api", "http")
	} else {
		parked := setDefault(c.settings, "_common_disabled", M())
		holder = setDefault(parked, "api_http", clone(current))
		c.parked = parked
	}
	path := apiFields[field]
	if (path[0] == "auth" || path[0] == "webui") && !isNone(value) && !equal(value, S("")) && !has(holder, path[0]) {
		if saved := get(c.parked, "api_"+path[0]); isMap(saved) {
			set(holder, path[0], clone(saved))
		}
	}
	stored := value
	if equal(value, S("")) && field != "api.listen" {
		stored = nil
	}
	if err := put(holder, path, stored); err != nil {
		return err
	}
	if auth := get(holder, "auth"); path[0] == "auth" && isMap(auth) {
		if len(auth.Content) > 0 {
			setDefault(auth, "type", S("basic"))
		}
		if str(get(auth, "username")) == "" && str(get(auth, "password")) == "" {
			c.park("api_auth", auth)
			del(holder, "auth")
		}
	}
	if path[0] == "webui" {
		if webui := get(holder, "webui"); str(get(mapOr(webui), "root")) != "" {
			setDefault(webui, "index", S("index.html"))
		} else if isMap(webui) {
			c.park("api_webui", del(holder, "webui"))
		}
	}
	return nil
}

func (c *commonContext) patchCacheDump(changes *Node) error {
	if !touches(changes, []string{"cache_dump_enabled", "cache_dump_file", "cache_dump_interval"}) {
		return nil
	}
	plugin, err := c.ensure("lazy_cache", defaultArgs("lazy_cache"), "")
	if err != nil {
		return err
	}
	args := get(plugin, "args")
	enabled := str(get(args, "dump_file")) != ""
	if has(changes, "cache_dump_enabled") {
		enabled = isTrue(get(changes, "cache_dump_enabled"))
	}
	if str(get(args, "dump_file")) != "" {
		c.park("cache_dump_file", get(args, "dump_file"))
	}
	if str(get(args, "dump_file")) != "" || has(args, "dump_interval") {
		c.park("cache_dump_interval", orNull(get(args, "dump_interval")))
	}
	for _, field := range []string{"cache_dump_file", "cache_dump_interval"} {
		if has(changes, field) {
			c.park(field, get(changes, field))
		}
	}
	if enabled {
		path := get(changes, "cache_dump_file")
		if !has(changes, "cache_dump_file") {
			path = get(args, "dump_file")
			if str(path) == "" {
				path = get(c.parked, "cache_dump_file")
			}
			if str(path) == "" {
				path = get(defaultSettingsNode, "cache_dump_file")
			}
		}
		enabled = isStr(path) && strings.TrimSpace(path.Value) != ""
		if enabled {
			set(args, "dump_file", path)
			interval := orDefault(c.parked, "cache_dump_interval", get(defaultSettingsNode, "cache_dump_interval"))
			if err := put(args, []string{"dump_interval"}, interval); err != nil {
				return err
			}
		}
	}
	if !enabled {
		del(args, "dump_file")
		del(args, "dump_interval")
	}
	return nil
}

func (c *commonContext) patchTLS(changes *Node) error {
	for _, feature := range []string{"ros_address_list", "ros_route"} {
		field := feature + ".args.tls"
		touched := false
		for _, key := range keys(changes) {
			touched = touched || key == field || strings.HasPrefix(key, field+".")
		}
		if !touched {
			continue
		}
		plugin, err := c.ensure(feature, defaultArgs(feature), "")
		if err != nil {
			return err
		}
		args := get(plugin, "args")
		current := get(args, "tls")
		enabled := isMap(current)
		if has(changes, field) {
			enabled = isMap(get(changes, field))
		}
		saved := current
		if !isMap(current) {
			saved = get(c.parked, field)
		}
		value := M()
		if isMap(saved) {
			value = clone(saved)
		}
		if update := get(changes, field); isMap(update) {
			pairs(clone(update), func(k string, v *Node) bool { set(value, k, v); return true })
		}
		for _, key := range keys(changes) {
			if strings.HasPrefix(key, field+".") {
				if err := put(value, strings.Split(key[len(field)+1:], "."), get(changes, key)); err != nil {
					return err
				}
			}
		}
		c.park(field, value)
		var stored *Node
		if enabled {
			stored = value
		}
		if err := put(args, []string{"tls"}, stored); err != nil {
			return err
		}
	}
	return nil
}

func (c *commonContext) patchMain(field string, value *Node) {
	_, _, index := c.mainControl(field)
	list := c.sequence("sequence_main")
	var replacement *Node
	if isTrue(value) || len(items(value)) > 0 {
		switch field {
		case "block_qtypes":
			codes := make([]string, len(value.Content))
			for i, v := range value.Content {
				codes[i] = v.Value
			}
			replacement = M("matches", L("qtype "+strings.Join(codes, " ")), "exec", BlockQtypesExec)
		case "prefer_ipv4":
			replacement = M("exec", "prefer_ipv4")
		default:
			replacement = M("exec", "metrics_collector metrics")
		}
	}
	switch {
	case index >= 0 && replacement != nil:
		list.Content[index] = replacement
	case index >= 0:
		list.Content = append(list.Content[:index:index], list.Content[index+1:]...)
	case replacement != nil:
		at := 0
		if field != "metrics_enabled" {
			at = len(list.Content)
			for i, item := range list.Content {
				if !isOneOf(get(item, "exec"), "metrics_collector metrics", "$query_recorder", "prefer_ipv4") {
					at = i
					break
				}
			}
		}
		insertAt(list, at, replacement)
	}
}

var learnCall = M("exec", "$learn_classified_domain")

func (c *commonContext) patchDynamic(changes *Node) error {
	enabled, route, _ := c.dynamicState()
	parked := mapOr(get(c.parked, "dynamic_domain_set_state"))
	nextEnabled := enabled
	if has(changes, "dynamic_domain_set.enabled") {
		nextEnabled = isTrue(get(changes, "dynamic_domain_set.enabled"))
	}
	nextRoute := S(route)
	if route == "" {
		nextRoute = orDefault(parked, "route", S("direct"))
	}
	if has(changes, "dynamic_domain_set.route") {
		nextRoute = get(changes, "dynamic_domain_set.route")
	}
	if !isOneOf(nextRoute, "direct", "proxy") {
		return fail("动态域名路由必须为 direct 或 proxy")
	}
	var oldChains []string
	for _, name := range []string{"direct_ip", "ip_requery_proxy"} {
		if contains(c.sequence(name), learnCall) {
			oldChains = append(oldChains, name)
		}
	}
	learn := orDefault(parked, "learn", B(false))
	if enabled {
		learn = B(len(oldChains) > 0)
	}
	if has(changes, "dynamic_domain_set.learn") {
		learn = get(changes, "dynamic_domain_set.learn")
	}
	stateFields := []string{"dynamic_domain_set.enabled", "dynamic_domain_set.route", "dynamic_domain_set.learn"}
	if touches(changes, stateFields) {
		c.park("dynamic_domain_set_state", M("route", nextRoute, "learn", learn))
	}
	if nextEnabled {
		if _, err := c.ensure("dynamic_domains", defaultArgs("dynamic_domains"), ""); err != nil {
			return err
		}
	}
	if touches(changes, stateFields[:2]) {
		main := c.sequence("sequence_main")
		previous := -1
		for i, item := range main.Content {
			if equal(get(item, "matches"), L("qname $dynamic_domains")) {
				previous = i
				break
			}
		}
		if previous >= 0 {
			main.Content = append(main.Content[:previous:previous], main.Content[previous+1:]...)
		}
		if nextEnabled {
			index := previous
			if index < 0 {
				index = max(0, len(main.Content)-1)
				for i, item := range main.Content {
					if equal(get(item, "exec"), S("jump local_probe")) {
						index = i
						break
					}
				}
			}
			target := "goto proxy_dns_seq"
			if nextRoute.Value == "direct" {
				target = "goto china_dns"
			}
			insertAt(main, index, M("matches", L("qname $dynamic_domains"), "exec", target))
		}
	}
	if touches(changes, stateFields) {
		for _, chain := range oldChains {
			list := c.sequence(chain)
			list.Content = filter(list.Content, func(item *Node) bool { return !equal(item, learnCall) })
		}
		if nextEnabled && isTrue(learn) {
			name := "ip_requery_proxy"
			if nextRoute.Value == "direct" {
				name = "direct_ip"
			}
			chain := c.sequence(name)
			if chain == nil {
				return fail("缺少动态学习的目标执行链，请在完整参数中配置")
			}
			if _, err := c.ensure("learn_classified_domain", defaultArgs("learn_classified_domain"), ""); err != nil {
				return err
			}
			insertAt(chain, max(0, len(chain.Content)-1), clone(learnCall))
		}
	}
	return nil
}

func (c *commonContext) parkDisabledFeatures(changes *Node) {
	controls := []struct {
		prefix, tag string
		chains      []string
	}{{"cache_", "lazy_cache", []string{"china_dns"}}, {"probe_cache_", "local_probe_cache", []string{"local_probe"}}}
	for _, name := range features {
		chains := wrapperChains
		if name == "query_recorder" {
			chains = []string{"sequence_main"}
		}
		controls = append(controls, struct {
			prefix, tag string
			chains      []string
		}{name + ".", name, chains})
	}
	changed := func(prefix string) bool {
		for _, key := range keys(changes) {
			if strings.HasPrefix(key, prefix) {
				return true
			}
		}
		return false
	}
	for _, control := range controls {
		if changed(control.prefix) {
			if enabled, cap := c.callState(control.tag, control.chains); !enabled && cap.Editable {
				c.parkPlugin(control.tag)
			}
		}
	}
	if changed("dynamic_domain_set.") {
		if _, _, cap := c.dynamicState(); !cap.Editable {
			return
		}
		if len(ReferenceLocations(c.doc, []string{"learn_classified_domain"}, true)) == 0 {
			c.parkPlugin("learn_classified_domain")
		}
		if enabled, _, cap := c.dynamicState(); !enabled && cap.Editable {
			c.parkPlugin("dynamic_domains")
		}
	}
}

func (c *commonContext) patchRuleUpdate(changes *Node, p *Profile, baseURL string) error {
	enable := get(changes, "rule_update.enabled")
	if c.plugin("rules_cron") == nil && (isTrue(enable) || has(changes, "rule_update.args.timezone")) {
		parked := get(c.parked, "rules_cron")
		if isMap(parked) && equal(get(parked, "tag"), S("rules_cron")) && equal(get(parked, "type"), S("cron")) &&
			isMap(get(parked, "args")) && equal(get(get(parked, "args"), "jobs"), L()) {
			c.appendPlugin(clone(parked))
		}
	}
	job, _ := c.cronJob()
	defaults := at(defaultSettingsNode, "rule_update", "args")
	if isTrue(enable) && job == nil {
		source := shallow(c.settings)
		if err := setNativeConfig(source, c.doc); err != nil {
			return err
		}
		downloads, err := p.resourceDownloads(c.doc, source, baseURL, nil)
		if err != nil {
			return err
		}
		if len(downloads) == 0 {
			return fail("规则定时更新需要当前配置引用至少一个可下载订阅")
		}
		downloader, err := c.ensure("rules_download", M("timeout", get(defaults, "timeout"), "downloads", seqOf(downloads)), "")
		if err != nil {
			return err
		}
		if !isMap(get(downloader, "args")) {
			return fail("下载插件使用自定义参数，请在完整参数中编辑")
		}
		setDefault(get(downloader, "args"), "downloads", seqOf(downloads))
		providers := L()
		for _, item := range items(get(c.doc, "plugins")) {
			if isOneOf(get(item, "type"), "domain_set", "ip_set", "adguard_rule") && isMap(get(item, "args")) && (len(items(get(get(item, "args"), "files"))) > 0) {
				providers.Content = append(providers.Content, S("$"+str(get(item, "tag"))))
			}
		}
		if _, err := c.ensure("rules_reload", providers, "reload_provider"); err != nil {
			return err
		}
		if _, err := c.ensure("rules_refresh", L(M("exec", "$rules_download"), M("exec", "$rules_reload")), "sequence"); err != nil {
			return err
		}
		cron, err := c.ensure("rules_cron", M("timezone", get(defaults, "timezone"), "jobs", L()), "")
		if err != nil {
			return err
		}
		template := orDefault(c.parked, "rules_cron_job", M("name", "refresh_rules", "schedule", get(defaults, "schedule"), "executors", L("$rules_refresh")))
		job = clone(template)
		jobs := setDefault(get(cron, "args"), "jobs", L())
		jobs.Content = append(jobs.Content, job)
	}
	if equal(enable, B(false)) && isBool(enable) && job != nil {
		c.park("rules_cron_job", job)
		jobs := get(c.args("rules_cron"), "jobs")
		for i, item := range jobs.Content {
			if equal(item, job) {
				jobs.Content = append(jobs.Content[:i:i], jobs.Content[i+1:]...)
				break
			}
		}
		job = nil
	}
	if has(changes, "rule_update.args.schedule") {
		schedule := get(changes, "rule_update.args.schedule")
		if job != nil {
			set(job, "schedule", schedule)
		} else {
			parked := setDefault(c.settings, "_common_disabled", M())
			c.parked = parked
			holder := setDefault(parked, "rules_cron_job", M("name", "refresh_rules", "executors", L("$rules_refresh")))
			set(holder, "schedule", schedule)
		}
	}
	if has(changes, "rule_update.args.timezone") {
		cron, err := c.ensure("rules_cron", M("jobs", L()), "")
		if err != nil {
			return err
		}
		if err := put(get(cron, "args"), []string{"timezone"}, get(changes, "rule_update.args.timezone")); err != nil {
			return err
		}
	}
	// The core needs at least one cron job: park an empty owned scheduler.
	if cron := c.plugin("rules_cron"); cron != nil && equal(get(cron, "type"), S("cron")) && isMap(get(cron, "args")) && equal(get(get(cron, "args"), "jobs"), L()) {
		c.park("rules_cron", cron)
		c.removePlugin(cron)
		delete(c.byTag, "rules_cron")
	}
	return nil
}

func (c *commonContext) patchResources(changes *Node, p *Profile, baseURL string) error {
	if has(changes, "custom_hosts") {
		// A direct edit supersedes content parked by a native deletion.
		del(c.parked, "native_hosts_source")
		content := str(get(changes, "custom_hosts"))
		previous := orDefault(c.settings, "custom_hosts", S(""))
		if strings.TrimSpace(content) != "" {
			if _, err := HostsFileContent(content); err != nil {
				return err
			}
			plugin, err := c.ensure("custom_hosts", M("files", L("./rules/custom_hosts.txt")), "")
			if err != nil {
				return err
			}
			set(get(plugin, "args"), "files", L("./rules/custom_hosts.txt"))
			main := c.sequence("sequence_main")
			if !contains(main, M("exec", "$custom_hosts")) {
				index := 0
				for i, item := range main.Content {
					first := ""
					if m := get(item, "matches"); len(items(m)) > 0 || str(m) != "" {
						first = displayValue(items(m)[0])
						if isStr(m) {
							first = string([]rune(m.Value)[0])
						}
					}
					if !isOneOf(get(item, "exec"), "metrics_collector metrics", "$query_recorder", "prefer_ipv4") && !strings.HasPrefix(first, "qtype ") {
						index = i
						break
					}
				}
				insertAt(main, index, M("exec", "$custom_hosts"), M("matches", L("has_resp"), "exec", "accept"))
			}
		} else if c.plugin("custom_hosts") != nil {
			args := c.args("custom_hosts")
			set(args, "files", L())
			// The common field owns the managed file, not native inline rules.
			if entries := get(args, "entries"); isNone(entries) || equal(entries, L()) {
				if isNone(entries) {
					del(args, "entries")
				}
				main := c.sequence("sequence_main")
				if index := indexOf(main, M("exec", "$custom_hosts")); index >= 0 {
					main.Content = append(main.Content[:index:index], main.Content[index+1:]...)
					if index < len(main.Content) && equal(main.Content[index], M("matches", L("has_resp"), "exec", "accept")) {
						main.Content = append(main.Content[:index:index], main.Content[index+1:]...)
					}
				}
				if len(ReferenceLocations(c.doc, []string{"custom_hosts"}, true)) == 0 {
					c.parkPlugin("custom_hosts")
				}
			}
		}
		referenced, err := c.resourcePaths()
		if err != nil {
			return err
		}
		if strings.TrimSpace(content) == "" && referenced["rules/custom_hosts.txt"] {
			set(c.settings, "custom_hosts", previous)
		} else {
			set(c.settings, "custom_hosts", S(content))
		}
	}
	changed := false
	for _, key := range keys(changes) {
		changed = changed || strings.HasPrefix(key, "adguard.")
	}
	if !changed {
		return nil
	}
	urls, hasContent, _ := c.resources()
	resource := setDefault(c.settings, "adguard", M())
	if !isMap(resource) {
		resource = M()
		set(c.settings, "adguard", resource)
	}
	oldResource := clone(resource)
	urlList := orDefault(changes, "adguard.urls", seqOf(urls))
	content := S("")
	if hasContent {
		content = orDefault(resource, "content", S(""))
	}
	content = orDefault(changes, "adguard.content", content)
	if str(content) == "" {
		content = S("")
	}
	if isTrue(get(changes, "adguard.enabled")) && isMap(get(c.parked, "adguard_rules")) {
		if _, err := c.ensure("adguard_rules", nil, ""); err != nil {
			return err
		}
	}
	for _, url := range items(urlList) {
		if !isStr(url) || !httpURL.MatchString(url.Value) {
			return fail("AdGuard 订阅必须为 HTTP(S) URL")
		}
	}
	if !isSeq(urlList) {
		return errMalformed
	}
	if touches(changes, []string{"adguard.urls", "adguard.content"}) || isTrue(get(changes, "adguard.enabled")) && c.plugin("adguard_rules") == nil {
		provider, err := c.ensure("adguard_rules", M("files", L()), "")
		if err != nil {
			return err
		}
		files := L()
		if strings.TrimSpace(str(content)) != "" {
			files.Content = append(files.Content, S("./rules/adguard-custom.txt"))
		}
		for _, url := range urlList.Content {
			files.Content = append(files.Content, S("./rules/"+TagFor("adguard", url)+".txt"))
		}
		set(get(provider, "args"), "files", files)
		set(resource, "urls", urlList)
		set(resource, "content", content)
		set(resource, "enabled", B(true))
		// A separately wired provider may still read an old source.
		referenced, err := c.resourcePaths()
		if err != nil {
			return err
		}
		for _, previous := range items(get(oldResource, "urls")) {
			if isStr(previous) && referenced["rules/"+TagFor("adguard", previous)+".txt"] && !contains(get(resource, "urls"), previous) {
				list := get(resource, "urls")
				list.Content = append(list.Content, previous)
			}
		}
		if strings.TrimSpace(str(content)) == "" && referenced["rules/adguard-custom.txt"] {
			set(resource, "content", orDefault(oldResource, "content", S("")))
		}
	}
	if has(changes, "adguard.mode") {
		mode := get(changes, "adguard.mode")
		if !isOneOf(mode, "nxdomain", "nodata", "null", "refused") {
			return fail("AdGuard 响应模式无效")
		}
		plugin, err := c.ensure("adguard_block", M("mode", "nxdomain"), "")
		if err != nil {
			return err
		}
		set(get(plugin, "args"), "mode", mode)
	}
	if has(changes, "adguard.enabled") {
		main := c.sequence("sequence_main")
		if isTrue(get(changes, "adguard.enabled")) {
			if _, err := c.ensure("adguard_block", M("mode", "nxdomain"), ""); err != nil {
				return err
			}
			if _, err := c.ensure("adguard_reject", L(M("exec", "$adguard_block"), M("exec", "accept")), "sequence"); err != nil {
				return err
			}
			if !contains(main, adguardCall) {
				index := max(0, len(main.Content)-1)
				for i, item := range main.Content {
					if exec := displayValue(orEmpty(item, "exec")); strings.HasPrefix(exec, "goto ") || strings.HasPrefix(exec, "jump ") {
						index = i
						break
					}
				}
				insertAt(main, index, clone(adguardCall))
			}
		} else {
			main.Content = filter(main.Content, func(item *Node) bool { return !equal(item, adguardCall) })
		}
	}
	if active, cap := c.adguardState(); !active && cap.Editable {
		for _, tag := range []string{"adguard_rules", "adguard_reject", "adguard_block"} {
			c.parkPlugin(tag)
		}
		if reload := c.plugin("rules_reload"); reload != nil && equal(get(reload, "type"), S("reload_provider")) && isSeq(get(reload, "args")) {
			set(reload, "args", seqOf(filter(get(reload, "args").Content, func(e *Node) bool { return !equal(e, S("$adguard_rules")) })))
		}
	}
	downloader := c.plugin("rules_download")
	if !touches(changes, []string{"adguard.urls", "adguard.content", "adguard.enabled"}) || downloader == nil {
		return nil
	}
	reload := c.plugin("rules_reload")
	if !equal(get(downloader, "type"), S("download")) || !isMap(get(downloader, "args")) || !isSeq(get(get(downloader, "args"), "downloads")) ||
		reload != nil && (!equal(get(reload, "type"), S("reload_provider")) || !isSeq(get(reload, "args"))) {
		return fail("规则更新使用自定义下载/重载结构，请在完整参数中调整")
	}
	known := map[string]bool{}
	for _, url := range append(items(get(oldResource, "urls")), items(get(resource, "urls"))...) {
		if isStr(url) {
			known["rules/"+TagFor("adguard", url)+".txt"] = true
		}
	}
	desired, err := p.resourceDownloads(c.doc, c.settings, baseURL, nil)
	if err != nil {
		return err
	}
	wanted := map[string]*Node{}
	var order []string
	for _, entry := range desired {
		if location := downloadPath(entry); known[location] {
			if wanted[location] == nil {
				order = append(order, location)
			}
			wanted[location] = entry
		}
	}
	var output []*Node
	for _, entry := range get(get(downloader, "args"), "downloads").Content {
		location := downloadPath(entry)
		if !known[location] {
			output = append(output, entry)
		} else if wanted[location] != nil {
			output = append(output, entry)
			delete(wanted, location)
		}
	}
	for _, location := range order {
		if wanted[location] != nil {
			output = append(output, wanted[location])
		}
	}
	replaceDownloadEntries(downloader, output)
	if reload != nil && c.plugin("adguard_rules") != nil && !containsStr(get(reload, "args"), "$adguard_rules") {
		list := get(reload, "args")
		list.Content = append(list.Content, S("$adguard_rules"))
	}
	return nil
}

// Applied is apply_common_settings' answer: the new settings and the
// projection of the result.
type Applied struct {
	Settings *Node
	View     CommonView
}

// ApplyCommonSettings applies changes (field → value) to the prepared draft.
func ApplyCommonSettings(settings, changes *Node, p *Profile, baseURL string) (Applied, error) {
	if !isMap(changes) || len(changes.Content) == 0 {
		return Applied{}, fail("changes 必须为非空参数对象")
	}
	var unknown []string
	for _, field := range keys(changes) {
		if !commonFields[field] && !containsString(RoutingFields, field) {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return Applied{}, fail("不支持的常用参数：" + strings.Join(unknown, ", "))
	}
	current, err := PrepareEditor(settings, p, baseURL)
	if err != nil {
		return Applied{}, err
	}
	document, err := readSettingsView(current).loadNative(NativeLabel)
	if err != nil {
		return Applied{}, err
	}
	projection, err := readCommon(document, current, p)
	if err != nil {
		return Applied{}, err
	}
	for _, field := range keys(changes) {
		cap, ok := projection.Capabilities[field]
		_, isAPI := apiFields[field]
		enablingAPI := isAPI && isTrue(get(changes, "api.enabled")) && projection.Capabilities["api.enabled"].Editable
		if (!ok || !cap.Editable) && !enablingAPI {
			reason := cap.Reason
			if !ok || reason == "" {
				reason = "请在完整参数中编辑"
			}
			return Applied{}, fail(field + "：" + reason)
		}
		if err := validateValue(field, get(changes, field)); err != nil {
			return Applied{}, err
		}
	}
	before := clone(document)
	routingChanges := M()
	pairs(changes, func(k string, v *Node) bool {
		if containsString(RoutingFields, k) {
			set(routingChanges, k, v)
		}
		return true
	})
	if len(routingChanges.Content) > 0 {
		if document, current, err = applyRoutingNode(document, current, p, routingChanges, baseURL, RoutingOptions{}); err != nil {
			return Applied{}, err
		}
	}
	c, err := newContext(document, current)
	if err != nil {
		return Applied{}, err
	}
	if err := c.applyCommon(changes, p, baseURL); err != nil {
		return Applied{}, err
	}
	if !same(document, before) {
		if err := setNativeConfig(current, document); err != nil {
			return Applied{}, err
		}
	}
	if _, err := NormalizeSettings(current, false); err != nil {
		return Applied{}, err
	}
	view, err := readCommon(document, current, p)
	if err != nil {
		return Applied{}, err
	}
	return Applied{current, view}, nil
}

func (c *commonContext) applyCommon(changes *Node, p *Profile, baseURL string) error {
	fallback, err := prepareFallback(c, changes)
	if err != nil {
		return err
	}
	if has(changes, "api.enabled") {
		if err := c.patchAPI("api.enabled", get(changes, "api.enabled")); err != nil {
			return err
		}
	}
	// Enable first so simultaneous argument edits modify the new node.
	for _, f := range featureCalls {
		if has(changes, f.field) {
			if err := c.toggleCall(f.tag, f.chains, isTrue(get(changes, f.field)), defaultArgs(f.tag)); err != nil {
				return err
			}
		}
	}
	for _, key := range keys(changes) {
		if strings.HasPrefix(key, "dynamic_domain_set.") {
			if err := c.patchDynamic(changes); err != nil {
				return err
			}
			break
		}
	}
	if err := c.patchResources(changes, p, baseURL); err != nil {
		return err
	}
	if err := prepareRuleUpdate(c, changes, p, baseURL); err != nil {
		return err
	}
	for _, key := range keys(changes) {
		if strings.HasPrefix(key, "rule_update.") {
			if err := c.patchRuleUpdate(changes, p, baseURL); err != nil {
				return err
			}
			break
		}
	}
	if err := c.patchCacheDump(changes); err != nil {
		return err
	}
	if err := c.patchTLS(changes); err != nil {
		return err
	}
	for _, field := range keys(changes) {
		value := get(changes, field)
		if containsString([]string{"cache_dump_enabled", "cache_dump_file", "cache_dump_interval"}, field) ||
			strings.HasPrefix(field, "ros_address_list.args.tls") || strings.HasPrefix(field, "ros_route.args.tls") {
			continue
		}
		var err error
		if _, ok := findDirect(field); ok {
			err = c.patchDirect(field, value)
		} else if _, ok := apiFields[field]; ok {
			err = c.patchAPI(field, value)
		} else {
			err = c.patchField(field, value)
		}
		if err != nil {
			return err
		}
	}
	if touches(changes, chainFields) {
		if err := RefreshRuleHelpers(c.doc); err != nil {
			return err
		}
		c.byTag = map[string]*Node{}
		for _, item := range items(get(c.doc, "plugins")) {
			if isMap(item) {
				c.byTag[displayValue(get(item, "tag"))] = item
			}
		}
	}
	if err := finishFallback(c, fallback); err != nil {
		return err
	}
	c.parkDisabledFeatures(changes)
	return finishRuleUpdates(c, changes, p, baseURL)
}

func (c *commonContext) patchField(field string, value *Node) error {
	switch field {
	case "prefer_ipv4", "metrics_enabled", "block_qtypes":
		c.patchMain(field, value)
	case "local_dns", "remote_dns", "fallback_dns":
		upstreams := L()
		if text := str(value); strings.TrimSpace(text) != "" {
			parsed, ok, err := safeLoad(text)
			if err != nil {
				return err
			}
			if !ok {
				return fail("DNS 上游 YAML 语法无效")
			}
			upstreams = parsed
			if field == "fallback_dns" && isNone(parsed) {
				upstreams = L()
			}
		}
		if !isSeq(upstreams) || len(filter(upstreams.Content, isMap)) != len(upstreams.Content) {
			return fail("DNS 上游必须为参数对象数组")
		}
		plugin, err := c.ensure("forward_"+strings.Split(field, "_")[0], nil, "")
		if err != nil {
			return err
		}
		set(get(plugin, "args"), "upstreams", upstreams)
	case "listen":
		plugin, err := c.ensure("server_udp", M("entry", "sequence_main"), "")
		if err != nil {
			return err
		}
		set(get(plugin, "args"), "listen", value)
		if c.plugin("server_tcp") != nil {
			set(c.args("server_tcp"), "listen", value)
		}
	case "tcp_enabled":
		if isTrue(value) || len(items(value)) > 0 {
			args := clone(c.args("server_udp"))
			parked := get(c.parked, "server_tcp")
			if c.plugin("server_tcp") == nil && isMap(parked) && equal(get(parked, "type"), S("tcp_server")) {
				restored := clone(parked)
				for _, key := range []string{"listen", "entry"} {
					if has(args, key) {
						set(get(restored, "args"), key, get(args, key))
					}
				}
				c.appendPlugin(restored)
			} else if _, err := c.ensure("server_tcp", args, ""); err != nil {
				return err
			}
		} else if tcp := c.plugin("server_tcp"); tcp != nil {
			c.park("server_tcp", tcp)
			c.removePlugin(tcp)
			delete(c.byTag, "server_tcp")
		}
	case "remote_ttl":
		list := c.sequence("remote_dns")
		index := -1
		for i, item := range list.Content {
			if strings.HasPrefix(displayValue(orEmpty(item, "exec")), "ttl ") {
				index = i
				break
			}
		}
		switch {
		case index >= 0 && (positive(value)):
			list.Content[index] = M("exec", "ttl "+value.Value)
		case index >= 0:
			list.Content = append(list.Content[:index:index], list.Content[index+1:]...)
		case (positive(value)):
			list.Content = append(list.Content, M("exec", "ttl "+value.Value))
		}
	}
	return nil
}

var _ = strconv.Itoa
