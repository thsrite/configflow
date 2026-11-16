package oxidns

import (
	"unsafe"

	"configflow/internal/jsonx"
)

// The _Context class of backend/common/oxidns_common.py (module C24) and the
// Python list/dict glue the common settings code needs.
//
// Python mutates lists in place (list.insert, list.remove, items[:] = ...);
// jsonx lists are Go slices. Every in-place mutation of a list inside the
// document goes through commonContext.replaceList, which also rebinds every
// other place of the document that holds the same list (YAML aliases), so
// the result is the one Python's shared list object gives.

// TagTypes is _TAG_TYPES: the plugin type each managed tag must have
// (anything else is a "sequence").
var TagTypes = map[string]string{
	"lazy_cache": "cache", "local_probe_cache": "cache",
	"forward_local": "forward", "forward_remote": "forward", "forward_fallback": "forward",
	"fallback": "fallback", "dynamic_domains": "dynamic_domain_set",
	"learn_classified_domain": "learn_domain", "adguard_rules": "adguard_rule",
	"adguard_block": "black_hole", "custom_hosts": "hosts",
	"server_udp": "udp_server", "server_tcp": "tcp_server",
	"rules_download": "download", "rules_reload": "reload_provider",
	"rules_cron":     "cron",
	"query_recorder": "query_recorder", "ip_selector": "ip_selector",
	"ros_address_list": "ros_address_list", "ros_route": "ros_route",
}

func tagType(tag string) string {
	if kind, ok := TagTypes[tag]; ok {
		return kind
	}
	return "sequence"
}

// commonContext is _Context(document, settings).
type commonContext struct {
	doc      *jsonx.Object
	settings *jsonx.Object
	// attached: the document had "plugins" when the context was made, so
	// self.plugins is the document's list (otherwise a detached empty list).
	attached     bool
	byTag        map[string]*jsonx.Object
	parked       *jsonx.Object
	includedTags map[string]bool
}

func newCommonContext(document, settings *jsonx.Object) *commonContext {
	ctx := &commonContext{doc: document, settings: settings}
	plugins := get(document, "plugins", missingValue)
	ctx.attached = plugins != missingValue
	list, isList := plugins.([]any)
	if !ctx.attached {
		list, isList = nil, true
	}
	valid := isList
	if valid {
		for _, element := range list {
			obj, ok := element.(*jsonx.Object)
			if !ok || obj == nil || !isStr(obj.Value("tag")) {
				valid = false
				break
			}
		}
	}
	if !valid {
		fail("插件结构尚未完成，请在完整参数或 YAML 中继续编辑")
	}
	ctx.rebuildByTag()
	parked, ok := get(settings, "_common_disabled", missingValue).(*jsonx.Object)
	if !ok || parked == nil {
		parked = jsonx.NewObject()
	}
	ctx.parked = parked
	ctx.includedTags = map[string]bool{}
	if truthy(document.Value("include")) {
		expanded := nativeDocument(settings, false, nil)
		for _, element := range iterate(get(expanded, "plugins", []any{})) {
			tag := item(element, "tag")
			hashable(tag)
			if text, ok := tag.(string); ok && ctx.byTag[text] == nil {
				ctx.includedTags[text] = true
			}
		}
	}
	return ctx
}

// plugins is self.plugins.
func (ctx *commonContext) plugins() []any {
	if !ctx.attached {
		return nil
	}
	list, _ := ctx.doc.Value("plugins").([]any)
	return list
}

// rebuildByTag is {item.get('tag'): item for item in plugins if dict}.
func (ctx *commonContext) rebuildByTag() {
	ctx.byTag = map[string]*jsonx.Object{}
	for _, element := range ctx.plugins() {
		if obj, ok := element.(*jsonx.Object); ok && obj != nil {
			if tag, ok := obj.Value("tag").(string); ok {
				ctx.byTag[tag] = obj
			}
		}
	}
}

func (ctx *commonContext) plugin(tag string) *jsonx.Object { return ctx.byTag[tag] }

// aliasCapability is alias_capability(tag).
func (ctx *commonContext) aliasCapability(tag string) *jsonx.Object {
	aliases := get(ctx.parked, "renamed_plugins", jsonx.NewObject())
	var alias any
	if isDict(aliases) {
		alias = get(aliases, tag, nil)
	}
	if text, ok := alias.(string); ok && text != tag && ctx.plugin(text) != nil {
		return capability(false, "受管插件已重命名为「"+text+"」，请在完整参数中编辑自定义配置", text, false)
	}
	return nil
}

// allowed is allowed(tag, kind); kind "" is the tag's managed type.
func (ctx *commonContext) allowed(tag, kind string) *jsonx.Object {
	if alias := ctx.aliasCapability(tag); alias != nil {
		return alias
	}
	if ctx.includedTags[tag] {
		return capability(false, "此插件来自 include 文件，请在完整参数或附加文件中编辑", tag, false)
	}
	plugin := ctx.plugin(tag)
	if kind == "" {
		kind = tagType(tag)
	}
	if plugin != nil && !strIn(plugin.Value("type"), kind) {
		return capability(false, "同名插件使用了其他类型，请在完整参数中编辑", tag, false)
	}
	if plugin != nil && plugin.Value("args") != nil && !isDict(plugin.Value("args")) {
		return capability(false, "插件参数使用了自定义值类型，请在完整参数中编辑", tag, false)
	}
	return capability(true, "", tag, plugin == nil)
}

// args is args(tag): the plugin's args ({} for none or null, not stored).
func (ctx *commonContext) args(tag string) any {
	plugin := ctx.plugin(tag)
	if plugin == nil {
		return jsonx.NewObject()
	}
	if value := plugin.Value("args"); value != nil {
		return value
	}
	return jsonx.NewObject()
}

// storedArgs is stored_args(tag): the live or parked managed plugin's args.
func (ctx *commonContext) storedArgs(tag string) any {
	alias := ctx.aliasCapability(tag)
	if alias != nil && ctx.plugin(tag) == nil {
		return ctx.args(alias.Value("target").(string))
	}
	var plugin any
	if p := ctx.plugin(tag); p != nil {
		plugin = p
	} else {
		plugin = get(ctx.parked, tag, nil)
	}
	if isDict(plugin) && strIn(get(plugin, "tag", nil), tag) && strIn(get(plugin, "type", nil), tagType(tag)) {
		if args := get(plugin, "args", nil); truthy(args) {
			return args
		}
		return jsonx.NewObject()
	}
	return ctx.args(tag)
}

// appendPlugin is self.doc.setdefault('plugins', []).append(plugin).
func (ctx *commonContext) appendPlugin(plugin *jsonx.Object) {
	current := ctx.doc.SetDefault("plugins", []any{})
	list, ok := current.([]any)
	if !ok {
		raise(attributeError(current, "append"))
	}
	ctx.replaceList(ctx.doc, "plugins", list, appendCopy(list, plugin))
}

// ensure is ensure(tag, args, kind); args nil is None ({}), kind "" the
// tag's managed type.
func (ctx *commonContext) ensure(tag string, args any, kind string) *jsonx.Object {
	if kind == "" {
		kind = tagType(tag)
	}
	plugin := ctx.plugin(tag)
	parked := get(ctx.parked, tag, nil)
	if plugin == nil && isDict(parked) && strIn(get(parked, "tag", nil), tag) && strIn(get(parked, "type", nil), kind) {
		plugin = copyValue(parked).(*jsonx.Object)
		ctx.appendPlugin(plugin)
		ctx.byTag[tag] = plugin
	}
	if args == nil {
		args = jsonx.NewObject()
	}
	if plugin == nil {
		plugin = jsonx.Obj("tag", tag, "type", kind, "args", copyValue(args))
		ctx.appendPlugin(plugin)
		ctx.byTag[tag] = plugin
	} else if !strIn(plugin.Value("type"), kind) {
		fail("插件「" + tag + "」已使用其他类型")
	}
	if plugin.Value("args") == nil {
		plugin.Set("args", copyValue(args))
	}
	return plugin
}

// sequence is sequence(tag): the args list of a sequence plugin, ok=false
// for None.
func (ctx *commonContext) sequence(tag string) ([]any, bool) {
	plugin := ctx.plugin(tag)
	if plugin == nil || !strIn(plugin.Value("type"), "sequence") {
		return nil, false
	}
	list, ok := plugin.Value("args").([]any)
	return list, ok
}

// setSequence replaces the items of sequence(tag) in place.
func (ctx *commonContext) setSequence(tag string, items []any) {
	plugin := ctx.plugin(tag)
	old, _ := plugin.Value("args").([]any)
	ctx.replaceList(plugin, "args", old, items)
}

// park is park(name, value).
func (ctx *commonContext) park(name string, value any) {
	holder := ctx.settings.SetDefault("_common_disabled", jsonx.NewObject())
	setItem(holder, name, copyValue(value))
	ctx.parked = ctx.settings.Value("_common_disabled").(*jsonx.Object)
}

// parkPlugin is park_plugin(tag).
func (ctx *commonContext) parkPlugin(tag string) {
	plugin := ctx.plugin(tag)
	if plugin != nil {
		ctx.park(tag, plugin)
		ctx.removeFromPlugins(plugin)
		delete(ctx.byTag, tag)
	}
}

// removeFromPlugins is self.doc['plugins'].remove(plugin).
func (ctx *commonContext) removeFromPlugins(plugin *jsonx.Object) {
	list := item(ctx.doc, "plugins")
	ctx.replaceList(ctx.doc, "plugins", asList(list), listRemove(list, plugin))
}

// resourcePaths is resource_paths(): the resources the (expanded) document
// reads, relative.
func (ctx *commonContext) resourcePaths() map[string]bool {
	document := ctx.doc
	if truthy(ctx.doc.Value("include")) {
		settings := ctx.settings.Clone()
		settings.Set("native_config", dump(ctx.doc))
		document = nativeDocument(settings, false, nil)
	}
	out := map[string]bool{}
	for _, path := range resourceReferences(document) {
		if relative := relativeResource(path); relative != nil {
			out[*relative] = true
		}
	}
	return out
}

// callState is call_state(tag, chains): whether the feature is called and
// its capability.
func (ctx *commonContext) callState(tag string, chains []string) (bool, *jsonx.Object) {
	cap := ctx.allowed(tag, "")
	if !cap.GetBool("editable") {
		alias := ctx.aliasCapability(tag)
		return alias != nil && len(referenceLocations(ctx.doc, []string{alias.Value("target").(string)}, false)) > 0, cap
	}
	if truthy(ctx.doc.Value("include")) {
		return false, capability(false, "配置包含外部执行链，请在完整参数中调整功能调用", tag, false)
	}
	expected := jsonx.Obj("exec", "$"+tag)
	counts := map[string]int{}
	isChain := map[string]bool{}
	for _, chain := range chains {
		counts[chain] = 0
		isChain[chain] = true
	}
	custom, anyCall := false, false
	owned := map[string]bool{}
	if strIn(tag, chainWrappers...) {
		owned = ownedChainSet(ctx.doc)
	}
	for _, element := range ctx.plugins() {
		plugin := element.(*jsonx.Object)
		list, ok := get(plugin, "args", nil).([]any)
		pluginTag, _ := get(plugin, "tag", nil).(string)
		if !ok || owned[pluginTag] {
			continue
		}
		for _, entry := range list {
			var action any
			if isDict(entry) {
				action = get(entry, "exec", nil)
			}
			if strIn(action, "$"+tag, "jump "+tag, "goto "+tag) {
				anyCall = true
				if isChain[pluginTag] && jsonx.Equal(entry, expected) {
					counts[pluginTag]++
				} else {
					custom = true
				}
			}
		}
	}
	remaining := copyValue(ctx.doc).(*jsonx.Object)
	for _, element := range iterate(get(remaining, "plugins", []any{})) {
		pluginTag := get(element, "tag", nil)
		text, _ := pluginTag.(string)
		if (isChain[text] || owned[text]) && isStr(pluginTag) {
			if list, ok := get(element, "args", nil).([]any); ok {
				setItem(element, "args", listWithout(list, expected))
			}
		}
	}
	found := referenceLocations(remaining, []string{tag}, true)
	if len(found) > 0 {
		called := anyCall
		for _, entry := range found {
			if item(entry, "tag") == tag {
				called = true
			}
		}
		return called, capability(false, referenceReason(found, DefaultReferencePrefix), tag, false)
	}
	partial := false
	for _, chain := range chains {
		if counts[chain] > 1 || (anyCall && counts[chain] != 1) {
			partial = true
		}
	}
	if custom || partial {
		return anyCall, capability(false, "功能使用了自定义或部分接入的执行链，请在完整参数中调整调用", tag, false)
	}
	for _, chain := range chains {
		if _, ok := ctx.sequence(chain); !ok {
			return anyCall, capability(false, "当前没有可识别的 ConfigFlow 处理链，请在完整参数中接入功能", tag, false)
		}
	}
	return anyCall, cap
}

// toggleCall is toggle_call(tag, chains, enabled, default_args).
func (ctx *commonContext) toggleCall(tag string, chains []string, enabled bool, defaultArgs any) {
	if enabled {
		ctx.ensure(tag, defaultArgs, "")
	}
	saved, ok := get(ctx.parked, "feature_calls", jsonx.NewObject()).(*jsonx.Object)
	if ok && saved != nil {
		saved = copyValue(saved).(*jsonx.Object)
	} else {
		saved = jsonx.NewObject()
	}
	expected := jsonx.Obj("exec", "$"+tag)
	for _, chain := range chains {
		items, isSequence := ctx.sequence(chain)
		position, ok := get(saved, chain, jsonx.NewObject()).(*jsonx.Object)
		if !ok || position == nil {
			position = jsonx.NewObject()
		}
		original, ok := get(position, "items", []any{}).([]any)
		if !ok {
			original = []any{}
		}
		disabled, disabledStored := get(position, "disabled", []any{}).([]any)
		if !disabledStored {
			disabled = []any{}
		}
		disabledStored = disabledStored && position.Has("disabled")
		if enabled && !inList(items, expected, isSequence) {
			index := 0
			if inList(disabled, tag, true) && inList(original, expected, true) {
				following := original[listIndex(original, expected)+1:]
				index = len(items)
				for _, element := range following {
					if inList(items, element, true) {
						index = listIndex(items, element)
						break
					}
				}
			} else if chain == "sequence_main" {
				index = len(items)
				for i, element := range items {
					if !strIn(get(element, "exec", nil), "metrics_collector metrics", "$query_recorder") {
						index = i
						break
					}
				}
			} else if tag == "lazy_cache" {
				for i, element := range items {
					if jsonx.Equal(get(element, "exec", nil), "$forward_local") {
						index = i
						break
					}
				}
			}
			ctx.setSequence(chain, insertCopy(items, index, expected))
			if inList(disabled, tag, true) {
				disabled = listRemove(disabled, tag)
				if disabledStored {
					position.Set("disabled", disabled)
				}
			}
			if len(disabled) == 0 {
				saved.Pop(chain)
			}
		} else if !enabled {
			if inList(items, expected, isSequence) {
				if len(disabled) == 0 {
					position = jsonx.Obj("items", copyValue(items), "disabled", []any{})
					saved.Set(chain, position)
				}
				current := asList(item(position, "disabled"))
				if !inList(current, tag, true) {
					position.Set("disabled", appendCopy(current, tag))
				}
			}
			ctx.setSequence(chain, listWithout(items, expected))
		}
	}
	if saved.Len() > 0 || ctx.parked.Has("feature_calls") {
		ctx.park("feature_calls", saved)
	}
}

// replaceList stores items under owner[key] (old was the list there) and
// rebinds every other holder of old in the document.
func (ctx *commonContext) replaceList(owner *jsonx.Object, key string, old, items []any) {
	owner.Set(key, items)
	if len(old) == 0 {
		return
	}
	relinkList(ctx.doc, old, items, map[unsafe.Pointer]bool{})
}

// relinkList replaces, below root, every list that is old (same backing
// array and length) by items.
func relinkList(root any, old, items []any, seen map[unsafe.Pointer]bool) {
	switch x := root.(type) {
	case *jsonx.Object:
		if x == nil || seen[unsafe.Pointer(x)] {
			return
		}
		seen[unsafe.Pointer(x)] = true
		for key, value := range x.All() {
			if list, ok := value.([]any); ok && sameList(list, old) {
				x.Set(key, items)
				continue
			}
			relinkList(value, old, items, seen)
		}
	case []any:
		if len(x) == 0 || seen[unsafe.Pointer(unsafe.SliceData(x))] {
			return
		}
		seen[unsafe.Pointer(unsafe.SliceData(x))] = true
		for i, value := range x {
			if list, ok := value.([]any); ok && sameList(list, old) {
				x[i] = items
				continue
			}
			relinkList(value, old, items, seen)
		}
	}
}

func sameList(a, b []any) bool {
	return len(a) == len(b) && len(a) > 0 && unsafe.SliceData(a) == unsafe.SliceData(b)
}

// List glue (fresh slices, never sharing a backing array).

func appendCopy(list []any, values ...any) []any {
	out := make([]any, 0, len(list)+len(values))
	out = append(out, list...)
	return append(out, values...)
}

// insertCopy is list.insert(index, value) (index clamped like Python).
func insertCopy(list []any, index int, values ...any) []any {
	if index < 0 {
		index += len(list)
		if index < 0 {
			index = 0
		}
	}
	if index > len(list) {
		index = len(list)
	}
	out := make([]any, 0, len(list)+len(values))
	out = append(out, list[:index]...)
	out = append(out, values...)
	return append(out, list[index:]...)
}

// popCopy is list.pop(index) for a valid index.
func popCopy(list []any, index int) []any {
	out := make([]any, 0, len(list))
	out = append(out, list[:index]...)
	return append(out, list[index+1:]...)
}

// listWithout is [item for item in list if item != value].
func listWithout(list []any, value any) []any {
	out := make([]any, 0, len(list))
	for _, element := range list {
		if !jsonx.Equal(element, value) {
			out = append(out, element)
		}
	}
	return out
}

// sameElement is the identity-or-equality test of list.remove/index/in.
func sameElement(a, b any) bool {
	if x, ok := a.(*jsonx.Object); ok {
		if y, ok := b.(*jsonx.Object); ok && x == y {
			return true
		}
	}
	return jsonx.Equal(a, b)
}

// listIndex is list.index(value) (-1 when absent).
func listIndex(list []any, value any) int {
	for i, element := range list {
		if sameElement(element, value) {
			return i
		}
	}
	return -1
}

// inList is `value in list`; isList=false is a None container
// (TypeError).
func inList(list []any, value any, isList bool) bool {
	if !isList {
		raise(pyErr("TypeError", "argument of type 'NoneType' is not iterable"))
	}
	return listIndex(list, value) >= 0
}

// listRemove is list.remove(value) on a copy (ValueError when absent;
// AttributeError when the container is not a list).
func listRemove(container any, value any) []any {
	list, ok := container.([]any)
	if !ok {
		raise(attributeError(container, "remove"))
	}
	index := listIndex(list, value)
	if index < 0 {
		raise(pyErr("ValueError", "list.remove(x): x not in list"))
	}
	return popCopy(list, index)
}

// pyIter is iter(value) for the containers documents hold: list items,
// dict keys, the characters of a str; TypeError otherwise.
func pyIter(value any) []any {
	switch x := value.(type) {
	case []any:
		return x
	case *jsonx.Object:
		if x != nil {
			return iterate(x)
		}
	case string:
		out := []any{}
		for _, r := range x {
			out = append(out, string(r))
		}
		return out
	}
	raise(pyErr("TypeError", "'"+typeName(value)+"' object is not iterable"))
	return nil
}

// pyContains is `value in container`.
func pyContains(container any, value any) bool {
	switch x := container.(type) {
	case []any:
		return listIndex(x, value) >= 0
	case *jsonx.Object:
		if x != nil {
			hashable(value)
			text, ok := value.(string)
			return ok && x.Has(text)
		}
	case string:
		text, ok := value.(string)
		if !ok {
			raise(pyErr("TypeError", "'in <string>' requires string as left operand, not "+typeName(value)))
		}
		return containsString(x, text)
	}
	raise(pyErr("TypeError", "argument of type '"+typeName(container)+"' is not iterable"))
	return false
}

func containsString(s, sub string) bool {
	return len(sub) == 0 || indexString(s, sub) >= 0
}

func indexString(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// setItem is obj[key] = value.
func setItem(obj any, key string, value any) {
	switch x := obj.(type) {
	case *jsonx.Object:
		if x != nil {
			x.Set(key, value)
			return
		}
	case []any:
		raise(pyErr("TypeError", "list indices must be integers or slices, not str"))
	}
	raise(pyErr("TypeError", "'"+typeName(obj)+"' object does not support item assignment"))
}

// popKey is obj.pop(key, None).
func popKey(obj any, key string) any {
	switch x := obj.(type) {
	case *jsonx.Object:
		if x != nil {
			value, _ := x.Pop(key)
			return value
		}
	case []any:
		raise(pyErr("TypeError", "pop expected at most 1 argument, got 2"))
	}
	raise(attributeError(obj, "pop"))
	return nil
}

// setDefault is obj.setdefault(key, value).
func setDefault(obj any, key string, value any) any {
	x, ok := obj.(*jsonx.Object)
	if !ok || x == nil {
		raise(attributeError(obj, "setdefault"))
	}
	return x.SetDefault(key, value)
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
