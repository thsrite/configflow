package oxidns

import (
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// Parts of backend/common/oxidns_editor.py (module C26) that oxidns_common
// (C24) needs; C26 adds the rest of the module to this file.

// missingValue is the _MISSING sentinel of the OxiDNS form modules.
var missingValue any = &sentinel{"_MISSING"}

type sentinel struct{ name string }

// sameValue is oxidns_editor._same(left, right): equality that also
// compares types, so False is not 0 and 1 is not 1.0.
func sameValue(left, right any) bool {
	if left == missingValue || right == missingValue {
		return left == right
	}
	if typeName(left) != typeName(right) {
		return false
	}
	switch l := left.(type) {
	case *jsonx.Object:
		r, ok := right.(*jsonx.Object)
		if !ok || l.Len() != r.Len() {
			return false
		}
		for key := range l.All() {
			if !r.Has(key) {
				return false
			}
		}
		for key, value := range l.All() {
			if !sameValue(value, r.Value(key)) {
				return false
			}
		}
		return true
	case []any:
		r := right.([]any)
		if len(l) != len(r) {
			return false
		}
		for i := range l {
			if !sameValue(l[i], r[i]) {
				return false
			}
		}
		return true
	case yamlx.Timestamp:
		return l == right.(yamlx.Timestamp)
	case []byte:
		return string(l) == string(right.([]byte))
	}
	return jsonx.Equal(left, right)
}

// ---- the rest of oxidns_editor.py (C26): non-persistent preparation and
// three-way template tools for one native draft ----

// editorFields is _EDITOR_FIELDS.
var editorFields = map[string]bool{
	"config_mode": true, "native_config": true, "additional_files": true, "template_settings": true,
	"template_base_config": true, "legacy_native_config": true, "editor_version": true,
}

// resourceLists is _RESOURCE_LISTS.
var resourceLists = []string{"direct_rules", "proxy_rules", "direct_rulesets", "proxy_rulesets"}

// ruleIDs is _rule_ids(config): (every rule id -> item, ids of active
// rules) of a space.
func ruleIDs(configValue any) (*jsonx.Object, map[string]bool) {
	config := configOf(configValue)
	items := config.Value("rule_configs")
	if !truthy(items) {
		items = itemsOf(config)
	}
	known := jsonx.NewObject()
	for _, entry := range pyIter(items) {
		if obj, ok := entry.(*jsonx.Object); ok && obj != nil {
			if identity, ok := obj.Value("id").(string); ok && !known.Has(identity) {
				known.Set(identity, obj)
			}
		}
	}
	active := map[string]bool{}
	for _, entry := range itemsOf(config) {
		identity := get(entry, "id", nil)
		hashable(identity)
		if text, ok := identity.(string); ok {
			active[text] = true
		}
	}
	return known, active
}

// restoreParked is _restore_parked(inputs, config): template inputs route
// disabled rules again; config nil is None.
func restoreParked(inputs *jsonx.Object, config any) *jsonx.Object {
	parked, ok1 := inputs.Value("rule_dns_parked").(*jsonx.Object)
	actions, ok2 := inputs.Value("rule_actions").(*jsonx.Object)
	if !ok1 || parked == nil || !ok2 || actions == nil {
		return inputs // Normalization reports the malformed value.
	}
	for _, key := range resourceLists {
		if _, ok := inputs.GetOr(key, []any{}).([]any); !ok {
			return inputs
		}
	}
	known := jsonx.NewObject()
	if config != nil {
		known, _ = ruleIDs(config)
	}
	for identity, action := range parked.All() {
		var route any
		if isDict(action) {
			route = get(action, "action", nil)
		}
		if strIn(route, "direct", "proxy") {
			routeText := route.(string)
			var suffix string
			if entry, ok := known.Get(identity); ok {
				suffix = itemSuffix(entry)
			} else if inList(asList(inputs.GetOr(routeText+"_rulesets", []any{})), identity, true) {
				suffix = "rulesets"
			} else {
				suffix = "rules"
			}
			otherRoute := "direct"
			if routeText == "direct" {
				otherRoute = "proxy"
			}
			other := otherRoute + "_" + suffix
			inputs.Set(other, listWithout(asList(inputs.GetOr(other, []any{})), identity))
			target := routeText + "_" + suffix
			list := asList(inputs.SetDefault(target, []any{}))
			if !inList(list, identity, true) {
				inputs.Set(target, appendCopy(list, identity))
			}
		} else if route != nil {
			if !actions.Has(identity) {
				actions.Set(identity, action)
			}
		}
	}
	inputs.Set("rule_dns_parked", jsonx.NewObject())
	for _, key := range resourceLists {
		if list, ok := inputs.Get(key); ok {
			kept := []any{}
			for _, value := range asList(list) {
				if !pyContains(actions, value) {
					kept = append(kept, value)
				}
			}
			inputs.Set(key, kept)
		}
	}
	return inputs
}

// TemplateInputs is template_inputs(settings, config): copy template
// controls without recursively embedding previous editor data; config nil
// is None.
func TemplateInputs(settings *jsonx.Object, config any) (inputs *jsonx.Object, err error) {
	defer catch(&err)
	return templateInputs(settings, config), nil
}

func templateInputs(settings any, config any) *jsonx.Object {
	obj, ok := settings.(*jsonx.Object)
	if !ok || obj == nil {
		raise(attributeError(settings, "items"))
	}
	inputs := jsonx.NewObject()
	for key, value := range obj.All() {
		if !editorFields[key] && !strings.HasPrefix(key, "_editor_") {
			inputs.Set(key, copyValue(value))
		}
	}
	return restoreParked(inputs, config)
}

// parkInactive is _park_inactive(settings, config): native routing keeps
// only active rules; park the choices of the others.
func parkInactive(settings *jsonx.Object, config any) {
	known, active := ruleIDs(config)
	follow := map[string]bool{}
	for _, value := range pyIter(item(settings, "rule_dns_follow")) {
		hashable(value)
		if text, ok := value.(string); ok {
			follow[text] = true
		}
	}
	parked := item(settings, "rule_dns_parked")
	actions := asObject(item(settings, "rule_actions"))
	for _, identity := range actions.Keys() {
		action := actions.Value(identity)
		if active[identity] {
			continue
		}
		actions.Delete(identity)
		if known.Has(identity) && !follow[identity] {
			setDefault(parked, identity, action)
		}
	}
	for _, key := range resourceLists {
		for _, value := range pyIter(item(settings, key)) {
			identity, ok := value.(string)
			if ok && known.Has(identity) && !active[identity] && !follow[identity] {
				setDefault(parked, identity, jsonx.Obj("action", strings.SplitN(key, "_", 2)[0]))
			}
		}
	}
}

// renderTemplate is _template(settings, config, base_url): the normalized
// template settings, the generated text and its document.
func renderTemplate(settings *jsonx.Object, config any, baseURL string) (*jsonx.Object, string, *jsonx.Object) {
	source := copyValue(configOf(config)).(*jsonx.Object)
	values := templateInputs(settings, config)
	values.Set("config_mode", "template")
	values.Set("native_config", "")
	values.Set("additional_files", copyValue(settings.GetOr("additional_files", []any{})))
	source.Set("oxidns", normalizeSettings(values, true))
	text := generateConfig(source, baseURL)
	return source.GetObject("oxidns"), text, loadNativeYAML(text, DefaultNativeLabel)
}

// PrepareEditor is prepare_editor(settings, config, base_url): a unified
// native draft, without writing or normalizing native YAML text.
func PrepareEditor(settings, config any, baseURL string) (draft *jsonx.Object, err error) {
	defer catch(&err)
	return prepareEditor(settings, config, baseURL), nil
}

func prepareEditorImpl(settings, config any, baseURL string) *jsonx.Object {
	current := normalizeSettings(settings, false)
	if current.Value("config_mode") == "native" {
		// A former native user may have an invalid, inactive template. Opening
		// the editor must not attempt to regenerate it or replace their YAML.
		if !isDict(current.Value("template_settings")) {
			current.Set("template_settings", templateInputs(current, config))
		}
		return current
	}
	template, text, document := renderTemplate(current, config, baseURL)
	oldNative := item(current, "native_config")
	if strip(oldNative) != "" && !current.Has("legacy_native_config") {
		same := false
		if tryExcept(func() {
			same = sameValue(loadNativeYAML(oldNative, DefaultNativeLabel), document)
		}, ClassSettings) != nil {
			same = false
		}
		if !same {
			current.Set("legacy_native_config", oldNative)
		}
	}
	current.Set("config_mode", "native")
	current.Set("native_config", text)
	current.Set("template_settings", templateInputs(template, config))
	current.Set("template_base_config", text)
	parkInactive(current, config)
	return current
}

// copyOrMissing is _copy(value).
func copyOrMissing(value any) any {
	if value == missingValue {
		return value
	}
	return copyValue(value)
}

// pointer is _pointer(path): a JSON pointer.
func pointer(path []string) string {
	parts := make([]string, len(path))
	for i, part := range path {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
	}
	return "/" + strings.Join(parts, "/")
}

// unionKeys is _keys(*objects): the keys of the dicts among objects, in
// first-seen order.
func unionKeys(objects ...any) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range objects {
		if obj, ok := value.(*jsonx.Object); ok && obj != nil {
			for _, key := range obj.Keys() {
				if !seen[key] {
					seen[key] = true
					out = append(out, key)
				}
			}
		}
	}
	return out
}

// getOrMissing is obj.get(key, _MISSING) on a dict (or the sentinel).
func getOrMissing(value any, key string) any {
	if obj, ok := value.(*jsonx.Object); ok && obj != nil {
		if element, ok := obj.Get(key); ok {
			return element
		}
	}
	return missingValue
}

// pluginMap is _plugin_map(value): tag -> plugin, nil when value is not a
// list of objects with unique string tags.
func pluginMap(value any) *jsonx.Object {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	result := jsonx.NewObject()
	for _, element := range list {
		obj, ok := element.(*jsonx.Object)
		if !ok || obj == nil {
			return nil
		}
		tag, ok := obj.Value("tag").(string)
		if !ok || result.Has(tag) {
			return nil
		}
		result.Set(tag, obj)
	}
	return result
}

// label is _label(path).
func label(path []string) string {
	if len(path) > 1 && path[0] == "plugins" {
		text := "插件「" + path[1] + "」"
		if len(path) > 2 {
			text += " · " + strings.Join(path[2:], ".")
		}
		return text
	}
	switch strings.Join(path, "\x00") {
	case "resources\x00custom_hosts":
		return "Hosts 规则文件内容"
	case "resources\x00adguard\x00content":
		return "AdGuard 自定义规则文件内容"
	}
	return strings.Join(path, ".")
}

func childPath(path []string, key string) []string {
	return append(append([]string{}, path...), key)
}

// mergePlugins is _merge_plugins(base, current, proposed, path,
// resolutions, conflicts); ok false is None.
func mergePlugins(base, current, proposed any, path []string, resolutions *jsonx.Object, conflicts *[]any) ([]any, bool) {
	baseMap, currentMap, nextMap := pluginMap(base), pluginMap(current), pluginMap(proposed)
	if baseMap == nil || currentMap == nil || nextMap == nil {
		return nil, false
	}
	merged := jsonx.NewObject()
	for _, tag := range unionKeys(currentMap, nextMap, baseMap) {
		value := mergeValues(getOrMissing(baseMap, tag), getOrMissing(currentMap, tag), getOrMissing(nextMap, tag),
			childPath(path, tag), resolutions, conflicts)
		if value != missingValue {
			merged.Set(tag, value)
		}
	}
	order := []string{}
	for _, tag := range currentMap.Keys() {
		if merged.Has(tag) {
			order = append(order, tag)
		}
	}
	indexOf := func(tag string) int {
		for i, value := range order {
			if value == tag {
				return i
			}
		}
		return -1
	}
	templateOrder := nextMap.Keys()
	for index, tag := range templateOrder {
		if !merged.Has(tag) || indexOf(tag) >= 0 {
			continue
		}
		previous, following := "", ""
		for i := index - 1; i >= 0; i-- {
			if indexOf(templateOrder[i]) >= 0 {
				previous = templateOrder[i]
				break
			}
		}
		for _, candidate := range templateOrder[index+1:] {
			if indexOf(candidate) >= 0 {
				following = candidate
				break
			}
		}
		at := len(order)
		if previous != "" {
			at = indexOf(previous) + 1
		} else if following != "" {
			at = indexOf(following)
		}
		order = append(order[:at], append([]string{tag}, order[at:]...)...)
	}
	result := make([]any, len(order))
	for i, tag := range order {
		result[i] = merged.Value(tag)
	}
	return result, true
}

// mergeValues is _merge(base, current, proposed, path, resolutions,
// conflicts): the three-way merge of one value (missingValue = _MISSING).
func mergeValues(base, current, proposed any, path []string, resolutions *jsonx.Object, conflicts *[]any) any {
	if sameValue(proposed, base) || sameValue(current, proposed) {
		return copyOrMissing(current)
	}
	currentObj, ok1 := current.(*jsonx.Object)
	proposedObj, ok2 := proposed.(*jsonx.Object)
	baseObj, baseIsDict := base.(*jsonx.Object)
	if ok1 && currentObj != nil && ok2 && proposedObj != nil && (base == missingValue || baseIsDict && baseObj != nil) {
		var previous any = jsonx.NewObject()
		if base != missingValue {
			previous = base
		}
		result := jsonx.NewObject()
		for _, key := range unionKeys(current, proposed, previous) {
			value := mergeValues(getOrMissing(previous, key), getOrMissing(current, key), getOrMissing(proposed, key),
				childPath(path, key), resolutions, conflicts)
			if value != missingValue {
				result.Set(key, value)
			}
		}
		return result
	}
	if len(path) == 1 && path[0] == "plugins" {
		var baseList any = []any{}
		if base != missingValue {
			baseList = base
		}
		if result, ok := mergePlugins(baseList, current, proposed, path, resolutions, conflicts); ok {
			return result
		}
	}
	if sameValue(current, base) {
		return copyOrMissing(proposed)
	}
	location := pointer(path)
	choice := resolutions.GetOr(location, "current")
	*conflicts = append(*conflicts, jsonx.Obj("path", location, "label", label(path), "choice", choice))
	if choice == "template" {
		return copyOrMissing(proposed)
	}
	return copyOrMissing(current)
}

// changesOf is _changes(current, proposed, path).
func changesOf(current, proposed any, path []string) []any {
	if sameValue(current, proposed) {
		return []any{}
	}
	if current == missingValue || proposed == missingValue {
		kind := "remove"
		if current == missingValue {
			kind = "add"
		}
		return []any{jsonx.Obj("path", pointer(path), "kind", kind)}
	}
	currentObj, ok1 := current.(*jsonx.Object)
	proposedObj, ok2 := proposed.(*jsonx.Object)
	if ok1 && currentObj != nil && ok2 && proposedObj != nil {
		out := []any{}
		for _, key := range unionKeys(current, proposed) {
			out = append(out, changesOf(getOrMissing(current, key), getOrMissing(proposed, key), childPath(path, key))...)
		}
		return out
	}
	if len(path) == 1 && path[0] == "plugins" {
		left, right := pluginMap(current), pluginMap(proposed)
		if left != nil && right != nil {
			out := []any{}
			for _, tag := range unionKeys(left, right) {
				out = append(out, changesOf(getOrMissing(left, tag), getOrMissing(right, tag), childPath(path, tag))...)
			}
			if sameKeySet(left, right) && strings.Join(left.Keys(), "\x00") != strings.Join(right.Keys(), "\x00") {
				out = append(out, jsonx.Obj("path", pointer(path), "kind", "reorder"))
			}
			return out
		}
	}
	return []any{jsonx.Obj("path", pointer(path), "kind", "update")}
}

func sameKeySet(a, b *jsonx.Object) bool {
	if a.Len() != b.Len() {
		return false
	}
	for _, key := range a.Keys() {
		if !b.Has(key) {
			return false
		}
	}
	return true
}

// union is _union(left, right): left plus the items of right not already
// in it (by sameValue).
func union(left, right any) []any {
	result := []any{}
	if list, ok := left.([]any); ok {
		result = copyValue(list).([]any)
	}
	if list, ok := right.([]any); ok {
		for _, element := range list {
			found := false
			for _, existing := range result {
				if sameValue(element, existing) {
					found = true
					break
				}
			}
			if !found {
				result = append(result, copyValue(element))
			}
		}
	}
	return result
}

// keepReferencedContent is _keep_referenced_content(...): deleting template
// inputs cannot remove a file retained by a manual plugin.
func keepReferencedContent(current, merged any, resource string, path []string, referenced map[string]bool,
	resolutions *jsonx.Object, conflicts *[]any) any {
	currentText, ok := current.(string)
	mergedText, mergedIsText := merged.(string)
	if !referenced[resource] || !ok || strings.TrimSpace(currentText) == "" || (mergedIsText && strings.TrimSpace(mergedText) != "") {
		return merged
	}
	location := pointer(path)
	if resolutions.GetOr(location, "current") == "template" {
		fail(label(path) + "仍被完整配置引用，请先移除相关文件引用再清空内容")
	}
	for _, conflict := range *conflicts {
		if item(conflict, "path") == location {
			return current
		}
	}
	*conflicts = append(*conflicts, jsonx.Obj("path", location, "label", label(path), "choice", "current"))
	return current
}

// PreviewTemplate is preview_template(settings, proposed_inputs, config,
// base_url, resolutions): a merge preview {settings, changes, conflicts}
// that retains manual edits and all resource sources. resolutions nil is
// None.
func PreviewTemplate(settings, proposedInputs, config any, baseURL string, resolutions any) (preview *jsonx.Object, err error) {
	defer catch(&err)
	return previewTemplate(settings, proposedInputs, config, baseURL, resolutions), nil
}

func previewTemplate(settings, proposedInputs, config any, baseURL string, resolutionsValue any) *jsonx.Object {
	if !isDict(proposedInputs) {
		fail("template_settings 必须为模板参数对象")
	}
	if resolutionsValue == nil {
		resolutionsValue = jsonx.NewObject()
	}
	resolutions, ok := resolutionsValue.(*jsonx.Object)
	if ok && resolutions != nil {
		for _, choice := range resolutions.Values() {
			if !strIn(choice, "current", "template") {
				ok = false
			}
		}
	}
	if !ok || resolutions == nil {
		fail("resolutions 必须按参数路径选择 current 或 template")
	}
	current := prepareEditor(settings, config, baseURL)
	fallback := templateInputs(current, config)
	previousInputs := current.GetOr("template_settings", fallback)
	// Validate only on invoking this tool. Merely opening a native draft never
	// depends on whether its retained template controls are still renderable.
	oldTemplate := templateInputs(previousInputs, config)
	oldTemplate.Set("additional_files", item(current, "additional_files"))
	var base *jsonx.Object
	switch baseText := current.Value("template_base_config").(type) {
	case string:
		if strings.TrimSpace(baseText) != "" {
			base = loadNativeYAML(baseText, "模板基线")
		}
	case *jsonx.Object:
		if baseText != nil {
			// Accept early draft metadata stored as a parsed document as well.
			text, err := yamlx.SafeDump(baseText)
			must(err)
			base = loadNativeYAML(text, "模板基线")
		}
	}
	if base == nil {
		_, _, base = renderTemplate(oldTemplate, config, baseURL)
	}
	nextInputs := templateInputs(proposedInputs, config)
	nextInputs.Set("additional_files", item(current, "additional_files"))
	nextTemplate, nextText, proposed := renderTemplate(nextInputs, config, baseURL)
	document := loadNativeYAML(item(current, "native_config"), DefaultNativeLabel)
	conflicts := []any{}
	merged := mergeValues(base, document, proposed, nil, resolutions, &conflicts)
	changes := changesOf(document, merged, nil)
	result := copyValue(current).(*jsonx.Object)
	result.Set("config_mode", "native")
	result.Set("template_settings", templateInputs(nextTemplate, config))
	result.Set("template_base_config", nextText)
	// Keep comments and original formatting when the actual document is equal.
	if !sameValue(document, merged) {
		result.Set("native_config", dump(merged.(*jsonx.Object)))
	}
	referenced := map[string]bool{}
	for _, path := range resourceReferences(nativeDocument(result, false, nil)) {
		referenced[resourceKey(relativeResource(path))] = true
	}
	for _, key := range resourceLists {
		result.Set(key, union(current.Value(key), nextTemplate.Value(key)))
		changes = append(changes, changesOf(getOrMissing(current, key), result.Value(key), []string{"resources", key})...)
	}
	// The draft's own rule DNS choices win; follow ends where the template set an action.
	actions := asObject(item(nextTemplate, "rule_actions")).Clone()
	for key, value := range asObject(item(current, "rule_actions")).All() {
		actions.Set(key, value)
	}
	result.Set("rule_actions", actions)
	follow := []any{}
	for _, identity := range pyIter(item(current, "rule_dns_follow")) {
		if !pyContains(actions, identity) {
			follow = append(follow, identity)
		}
	}
	result.Set("rule_dns_follow", follow)
	parkInactive(result, config)
	hostsPath := []string{"resources", "custom_hosts"}
	hosts := mergeValues(get(previousInputs, "custom_hosts", ""), current.GetOr("custom_hosts", ""),
		item(nextTemplate, "custom_hosts"), hostsPath, resolutions, &conflicts)
	hosts = keepReferencedContent(current.GetOr("custom_hosts", ""), hosts, "rules/custom_hosts.txt",
		hostsPath, referenced, resolutions, &conflicts)
	result.Set("custom_hosts", hosts)
	changes = append(changes, changesOf(current.GetOr("custom_hosts", ""), hosts, hostsPath)...)
	oldAdguard, ok := get(previousInputs, "adguard", nil).(*jsonx.Object)
	if !ok || oldAdguard == nil {
		oldAdguard = jsonx.NewObject()
	}
	currentAdguard, ok := current.Value("adguard").(*jsonx.Object)
	if !ok || currentAdguard == nil {
		currentAdguard = jsonx.NewObject()
	}
	nextAdguard := item(nextTemplate, "adguard")
	adguard := copyValue(currentAdguard).(*jsonx.Object)
	adguard.Set("enabled", truthy(currentAdguard.Value("enabled")) || truthy(item(nextAdguard, "enabled")))
	adguard.Set("urls", union(currentAdguard.Value("urls"), item(nextAdguard, "urls")))
	contentPath := []string{"resources", "adguard", "content"}
	content := mergeValues(oldAdguard.GetOr("content", ""), currentAdguard.GetOr("content", ""),
		item(nextAdguard, "content"), contentPath, resolutions, &conflicts)
	content = keepReferencedContent(currentAdguard.GetOr("content", ""), content, "rules/adguard-custom.txt",
		contentPath, referenced, resolutions, &conflicts)
	adguard.Set("content", content)
	result.Set("adguard", adguard)
	changes = append(changes, changesOf(currentAdguard, adguard, []string{"resources", "adguard"})...)
	// Includes and attached files must still be structurally valid, while an
	// incomplete legacy plugin is allowed to remain editable in this preview.
	normalizeSettings(result, false)
	return jsonx.Obj("settings", result, "changes", changes, "conflicts", conflicts)
}
