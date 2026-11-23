package oxidns

import (
	"configflow/internal/model"
	"errors"
	"slices"
	"strings"
)

// The editor prepares one native draft per profile (without writing it) and
// merges template changes three-way into it. The native YAML is the
// executable source; template inputs and their last generated baseline are
// editing metadata.

var (
	editorFields  = []string{"config_mode", "native_config", "additional_files", "template_settings", "template_base_config", "legacy_native_config", "editor_version"}
	resourceLists = []string{"direct_rules", "proxy_rules", "direct_rulesets", "proxy_rulesets"}
)

// ruleIDs is (every rule id → item, ids of active rules) of a profile.
func (p *Profile) ruleIDs() (map[string]*model.RuleConfig, map[string]bool) {
	known, _ := p.knownRules()
	active := map[string]bool{}
	for _, item := range p.activeItems() {
		active[item.ID] = true
	}
	return known, active
}

// restoreParked: template inputs route disabled rules again (generation
// skips them itself); rule_actions ids leave the selector lists.
func restoreParked(inputs *Node, p *Profile) *Node {
	parked, actions := get(inputs, "rule_dns_parked"), get(inputs, "rule_actions")
	if !isMap(parked) || !isMap(actions) {
		return inputs
	}
	for _, key := range resourceLists {
		if has(inputs, key) && !isSeq(get(inputs, key)) {
			return inputs // normalization reports the malformed value
		}
	}
	known := map[string]*model.RuleConfig{}
	if p != nil {
		known, _ = p.ruleIDs()
	}
	pairs(parked, func(identity string, action *Node) bool {
		route := get(action, "action")
		switch {
		case isOneOf(route, "direct", "proxy"):
			suffix := "rules"
			if item := known[identity]; item != nil {
				suffix = suffixOf(*item)
			} else if containsStr(get(inputs, route.Value+"_rulesets"), identity) {
				suffix = "rulesets"
			}
			other := map[string]string{"direct": "proxy", "proxy": "direct"}[route.Value] + "_" + suffix
			set(inputs, other, seqOf(filter(items(get(inputs, other)), func(v *Node) bool { return !equal(v, S(identity)) })))
			list := setDefault(inputs, route.Value+"_"+suffix, L())
			if !containsStr(list, identity) {
				list.Content = append(list.Content, S(identity))
			}
		case !isNone(route) && isMap(action):
			setDefault(actions, identity, action)
		}
		return true
	})
	set(inputs, "rule_dns_parked", M())
	for _, key := range resourceLists {
		if has(inputs, key) {
			set(inputs, key, seqOf(filter(items(get(inputs, key)), func(v *Node) bool { return !isStr(v) || !has(actions, v.Value) })))
		}
	}
	return inputs
}

// TemplateInputs copies the template controls of settings (no editor data)
// with parked choices restored.
func TemplateInputs(settings *Node, p *Profile) *Node {
	inputs := M()
	pairs(settings, func(key string, value *Node) bool {
		if !containsString(editorFields, key) && !strings.HasPrefix(key, "_editor_") {
			set(inputs, key, clone(value))
		}
		return true
	})
	return restoreParked(inputs, p)
}

// parkInactive keeps only active rules routed; choices of the others are
// parked (selector lists stay a retained catalog).
func parkInactive(settings *Node, p *Profile) {
	known, active := p.ruleIDs()
	values := ruleDNSFromSettings(settings)
	for _, identity := range values.Actions.keys() {
		if active[identity] {
			continue
		}
		action, _ := values.Actions.delete(identity)
		if known[identity] != nil && !slices.Contains(values.Follow, identity) {
			values.Parked.setDefault(identity, action)
		}
	}
	for _, key := range resourceLists {
		route := routePrefix(key)
		for _, identity := range values.selector(key) {
			if known[identity] != nil && !active[identity] && !slices.Contains(values.Follow, identity) {
				values.Parked.setDefault(identity, RuleAction{Action: route})
			}
		}
	}
	set(settings, "rule_actions", values.Actions.toNode())
	set(settings, "rule_dns_parked", values.Parked.toNode())
}

// renderTemplate is _template: normalized template settings, generated
// text and its document.
func renderTemplate(settings *Node, p *Profile, baseURL string) (*Node, string, *Node, error) {
	inputs := TemplateInputs(settings, p)
	set(inputs, "config_mode", S("template"))
	set(inputs, "native_config", S(""))
	files := get(settings, "additional_files")
	if !has(settings, "additional_files") {
		files = L()
	}
	set(inputs, "additional_files", clone(files))
	template, err := NormalizeSettings(inputs, true)
	if err != nil {
		return nil, "", nil, err
	}
	text, err := generate(template, p, baseURL)
	if err != nil {
		return nil, "", nil, err
	}
	document, err := LoadNativeYAML(text, NativeLabel)
	return template, text, document, err
}

// PrepareProfileEditor prepares the profile's unified native draft and parses
// its native YAML document.
func PrepareProfileEditor(p *Profile, baseURL string) (*Node, *nativeDoc, error) {
	if p == nil {
		p = emptyProfile()
	}
	settings, err := PrepareEditor(p.Settings(), p, baseURL)
	if err != nil {
		return nil, nil, err
	}
	doc, err := readSettingsView(settings).loadNative(NativeLabel)
	if err != nil {
		return nil, nil, err
	}
	return settings, doc, nil
}

// PrepareEditor returns the unified native draft of settings without
// writing it: template settings are rendered into a native draft.
func PrepareEditor(settings *Node, p *Profile, baseURL string) (*Node, error) {
	if p == nil {
		p = emptyProfile()
	}
	current, err := NormalizeSettings(settings, false)
	if err != nil {
		return nil, err
	}
	view := readSettingsView(current)
	if view.isNative() {
		if !view.TemplateSettings.OK {
			set(current, "template_settings", TemplateInputs(current, p))
		}
		return current, nil
	}
	template, text, document, err := renderTemplate(current, p, baseURL)
	if err != nil {
		return nil, err
	}
	if view.NativeConfig.OK && strings.TrimSpace(view.NativeConfig.Value) != "" && !view.LegacyNativeConfig.Present() {
		previous, err := LoadNativeYAML(view.NativeConfig.Value, NativeLabel)
		if err != nil || !same(previous, document) {
			set(current, "legacy_native_config", S(view.NativeConfig.Value))
		}
	}
	set(current, "config_mode", S("native"))
	set(current, "native_config", S(text))
	set(current, "template_settings", TemplateInputs(template, p))
	set(current, "template_base_config", S(text))
	parkInactive(current, p)
	return current, nil
}

// Change is one difference a template preview would make.
type Change struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// Conflict is a value both the draft and the template changed.
type Conflict struct {
	Path   string `json:"path"`
	Label  string `json:"label"`
	Choice string `json:"choice"`
}

// pointer is a JSON pointer of path.
func pointer(path []string) string {
	escaped := make([]string, len(path))
	for i, part := range path {
		escaped[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
	}
	return "/" + strings.Join(escaped, "/")
}

func label(path []string) string {
	switch {
	case len(path) > 1 && path[0] == "plugins":
		text := "插件「" + path[1] + "」"
		if len(path) > 2 {
			text += " · " + strings.Join(path[2:], ".")
		}
		return text
	case strings.Join(path, "\x00") == "resources\x00custom_hosts":
		return "Hosts 规则文件内容"
	case strings.Join(path, "\x00") == "resources\x00adguard\x00content":
		return "AdGuard 自定义规则文件内容"
	}
	return strings.Join(path, ".")
}

func with(path []string, part string) []string { return append(append([]string{}, path...), part) }

// orderedKeys is dict.fromkeys over the keys of several mappings.
func orderedKeys(objects ...*Node) []string {
	var out []string
	seen := map[string]bool{}
	for _, obj := range objects {
		for _, key := range keys(obj) {
			if !seen[key] {
				seen[key] = true
				out = append(out, key)
			}
		}
	}
	return out
}

// pluginsByTag is the editor's _plugin_map: nil unless a clean list.
func pluginsByTag(value *Node) (map[string]*Node, []string) {
	if !isSeq(value) {
		return nil, nil
	}
	result := map[string]*Node{}
	var order []string
	for _, item := range value.Content {
		tag, ok := tagOf(item)
		if !isMap(item) || !ok || result[tag] != nil {
			return nil, nil
		}
		result[tag] = item
		order = append(order, tag)
	}
	return result, order
}

// merger is one three-way merge; resolutions choose "current" or
// "template" per pointer.
type merger struct {
	resolutions map[string]string
	conflicts   []Conflict
}

func (m *merger) choice(location string) string {
	if c, ok := m.resolutions[location]; ok {
		return c
	}
	return "current"
}

func (m *merger) mergePlugins(base, current, proposed *Node, path []string) *Node {
	baseMap, baseOrder := pluginsByTag(base)
	currentMap, currentOrder := pluginsByTag(current)
	nextMap, nextOrder := pluginsByTag(proposed)
	if baseMap == nil || currentMap == nil || nextMap == nil {
		return nil
	}
	merged := map[string]*Node{}
	var tags []string
	for _, list := range [][]string{currentOrder, nextOrder, baseOrder} {
		for _, tag := range list {
			if !containsString(tags, tag) {
				tags = append(tags, tag)
			}
		}
	}
	for _, tag := range tags {
		if value := m.merge(baseMap[tag], currentMap[tag], nextMap[tag], with(path, tag)); value != nil {
			merged[tag] = value
		}
	}
	var order []string
	for _, tag := range currentOrder {
		if merged[tag] != nil {
			order = append(order, tag)
		}
	}
	indexOf := func(tag string) int {
		for i, t := range order {
			if t == tag {
				return i
			}
		}
		return -1
	}
	for index, tag := range nextOrder {
		if merged[tag] == nil || indexOf(tag) >= 0 {
			continue
		}
		position := -1
		for i := index - 1; i >= 0 && position < 0; i-- {
			if at := indexOf(nextOrder[i]); at >= 0 {
				position = at + 1
			}
		}
		for _, following := range nextOrder[index+1:] {
			if at := indexOf(following); position < 0 && at >= 0 {
				position = at
			}
		}
		if position < 0 {
			position = len(order)
		}
		order = append(order[:position], append([]string{tag}, order[position:]...)...)
	}
	result := presentation(L(), current)
	for _, tag := range order {
		result.Content = append(result.Content, merged[tag])
	}
	return result
}

// merge is _merge; nil stands for a missing value.
func (m *merger) merge(base, current, proposed *Node, path []string) *Node {
	if same(proposed, base) || same(current, proposed) {
		return clone(current)
	}
	if isMap(current) && isMap(proposed) && (base == nil || isMap(base)) {
		previous := base
		if previous == nil {
			previous = M()
		}
		result := shallow(current)
		for _, key := range orderedKeys(current, proposed, previous) {
			if value := m.merge(get(previous, key), get(current, key), get(proposed, key), with(path, key)); value != nil {
				set(result, key, value)
			} else {
				del(result, key)
			}
		}
		return result
	}
	if len(path) == 1 && path[0] == "plugins" {
		previous := base
		if previous == nil {
			previous = L()
		}
		if result := m.mergePlugins(previous, current, proposed, path); result != nil {
			return result
		}
	}
	if same(current, base) {
		return presentation(clone(proposed), current)
	}
	location := pointer(path)
	choice := m.choice(location)
	m.conflicts = append(m.conflicts, Conflict{location, label(path), choice})
	if choice == "template" {
		return clone(proposed)
	}
	return clone(current)
}

// changesOf is _changes: what turning current into proposed changes.
func changesOf(current, proposed *Node, path []string) []Change {
	if same(current, proposed) {
		return nil
	}
	if current == nil || proposed == nil {
		kind := "remove"
		if current == nil {
			kind = "add"
		}
		return []Change{{pointer(path), kind}}
	}
	if isMap(current) && isMap(proposed) {
		var out []Change
		for _, key := range orderedKeys(current, proposed) {
			out = append(out, changesOf(get(current, key), get(proposed, key), with(path, key))...)
		}
		return out
	}
	if len(path) == 1 && path[0] == "plugins" {
		left, leftOrder := pluginsByTag(current)
		right, rightOrder := pluginsByTag(proposed)
		if left != nil && right != nil {
			var out []Change
			tags := append([]string{}, leftOrder...)
			for _, tag := range rightOrder {
				if left[tag] == nil {
					tags = append(tags, tag)
				}
			}
			for _, tag := range tags {
				out = append(out, changesOf(left[tag], right[tag], with(path, tag))...)
			}
			sameSet := len(leftOrder) == len(rightOrder)
			for _, tag := range leftOrder {
				sameSet = sameSet && right[tag] != nil
			}
			if sameSet && strings.Join(leftOrder, "\x00") != strings.Join(rightOrder, "\x00") {
				out = append(out, Change{pointer(path), "reorder"})
			}
			return out
		}
	}
	return []Change{{pointer(path), "update"}}
}

func union(left, right *Node) *Node {
	result := L()
	if isSeq(left) {
		result = clone(left)
	}
	for _, item := range items(right) {
		found := false
		for _, existing := range result.Content {
			found = found || same(item, existing)
		}
		if !found {
			result.Content = append(result.Content, clone(item))
		}
	}
	return result
}

// keepReferencedContent: removing a template input cannot drop a file a
// manual plugin still reads.
func (m *merger) keepReferencedContent(current, merged *Node, resource string, path []string, referenced map[string]bool) (*Node, error) {
	if !referenced[resource] || !isStr(current) || strings.TrimSpace(current.Value) == "" || isStr(merged) && strings.TrimSpace(merged.Value) != "" {
		return merged, nil
	}
	location := pointer(path)
	if m.choice(location) == "template" {
		return nil, fail(label(path) + "仍被完整配置引用，请先移除相关文件引用再清空内容")
	}
	for _, c := range m.conflicts {
		if c.Path == location {
			return current, nil
		}
	}
	m.conflicts = append(m.conflicts, Conflict{location, label(path), "current"})
	return current, nil
}

// Preview is a template preview: merged settings, changes and conflicts.
type Preview struct {
	Settings  *Node
	Changes   []Change
	Conflicts []Conflict
}

// PreviewTemplate merges proposed template inputs into the draft, keeping
// manual edits and every resource source. resolutions (pointer → "current"
// or "template") may be nil.
func PreviewTemplate(settings, proposedInputs *Node, p *Profile, baseURL string, resolutions *Node) (Preview, error) {
	if !isMap(proposedInputs) {
		return Preview{}, fail("template_settings 必须为模板参数对象")
	}
	m := &merger{resolutions: map[string]string{}}
	if resolutions != nil {
		if !isMap(resolutions) {
			return Preview{}, fail("resolutions 必须按参数路径选择 current 或 template")
		}
		bad := false
		pairs(resolutions, func(path string, choice *Node) bool {
			bad = bad || !isOneOf(choice, "current", "template")
			m.resolutions[path] = str(choice)
			return true
		})
		if bad {
			return Preview{}, fail("resolutions 必须按参数路径选择 current 或 template")
		}
	}
	current, err := PrepareEditor(settings, p, baseURL)
	if err != nil {
		return Preview{}, err
	}
	previousInputs := get(current, "template_settings")
	if !has(current, "template_settings") {
		previousInputs = TemplateInputs(current, p)
	}
	if !isMap(previousInputs) {
		return Preview{}, errMalformed
	}
	withFiles := func(inputs *Node) *Node {
		t := TemplateInputs(inputs, p)
		set(t, "additional_files", get(current, "additional_files"))
		return t
	}
	var base *Node
	switch baseText := get(current, "template_base_config"); {
	case isStr(baseText) && strings.TrimSpace(baseText.Value) != "":
		base, err = LoadNativeYAML(baseText.Value, "模板基线")
	case isMap(baseText):
		var text string
		text, err = dump(baseText)
		if err == nil {
			base, err = LoadNativeYAML(text, "模板基线")
		}
	default:
		_, _, base, err = renderTemplate(withFiles(previousInputs), p, baseURL)
	}
	if err != nil {
		return Preview{}, err
	}
	nextTemplate, nextText, proposed, err := renderTemplate(withFiles(proposedInputs), p, baseURL)
	if err != nil {
		return Preview{}, err
	}
	document, err := readSettingsView(current).loadNative(NativeLabel)
	if err != nil {
		return Preview{}, err
	}
	merged := m.merge(base, document, proposed, nil)
	changes := changesOf(document, merged, nil)
	result := clone(current)
	set(result, "config_mode", S("native"))
	set(result, "template_settings", TemplateInputs(nextTemplate, p))
	set(result, "template_base_config", S(nextText))
	if !same(document, merged) {
		if err := setNativeConfig(result, merged); err != nil {
			return Preview{}, err
		}
	}
	expanded, err := NativeDocument(result, false, nil)
	if err != nil {
		return Preview{}, err
	}
	referenced, err := referencedPaths(expanded)
	if err != nil {
		return Preview{}, err
	}
	for _, key := range resourceLists {
		set(result, key, union(get(current, key), get(nextTemplate, key)))
		changes = append(changes, changesOf(get(current, key), get(result, key), []string{"resources", key})...)
	}
	actions := clone(get(nextTemplate, "rule_actions"))
	pairs(get(current, "rule_actions"), func(id string, action *Node) bool { set(actions, id, clone(action)); return true })
	set(result, "rule_actions", actions)
	set(result, "rule_dns_follow", seqOf(filter(items(get(current, "rule_dns_follow")), func(v *Node) bool { return !has(actions, v.Value) })))
	parkInactive(result, p)
	hostsPath := []string{"resources", "custom_hosts"}
	currentHosts := get(current, "custom_hosts")
	if !has(current, "custom_hosts") {
		currentHosts = S("")
	}
	previousHosts := get(previousInputs, "custom_hosts")
	if !has(previousInputs, "custom_hosts") {
		previousHosts = S("")
	}
	hosts := m.merge(previousHosts, currentHosts, get(nextTemplate, "custom_hosts"), hostsPath)
	if hosts, err = m.keepReferencedContent(currentHosts, hosts, "rules/custom_hosts.txt", hostsPath, referenced); err != nil {
		return Preview{}, err
	}
	set(result, "custom_hosts", orNull(hosts))
	changes = append(changes, changesOf(currentHosts, hosts, hostsPath)...)
	oldAdguard := mapOr(get(previousInputs, "adguard"))
	currentAdguard := mapOr(get(current, "adguard"))
	nextAdguard := get(nextTemplate, "adguard")
	adguard := clone(currentAdguard)
	set(adguard, "enabled", B(isTrue(get(currentAdguard, "enabled")) || isTrue(get(nextAdguard, "enabled"))))
	set(adguard, "urls", union(get(currentAdguard, "urls"), get(nextAdguard, "urls")))
	contentPath := []string{"resources", "adguard", "content"}
	textOr := func(m *Node) *Node {
		if has(m, "content") {
			return get(m, "content")
		}
		return S("")
	}
	content := m.merge(textOr(oldAdguard), textOr(currentAdguard), get(nextAdguard, "content"), contentPath)
	if content, err = m.keepReferencedContent(textOr(currentAdguard), content, "rules/adguard-custom.txt", contentPath, referenced); err != nil {
		return Preview{}, err
	}
	set(adguard, "content", orNull(content))
	set(result, "adguard", adguard)
	changes = append(changes, changesOf(currentAdguard, adguard, []string{"resources", "adguard"})...)
	if _, err := NormalizeSettings(result, false); err != nil {
		return Preview{}, err
	}
	if changes == nil {
		changes = []Change{}
	}
	if m.conflicts == nil {
		m.conflicts = []Conflict{}
	}
	return Preview{Settings: result, Changes: changes, Conflicts: m.conflicts}, nil
}

func orNull(n *Node) *Node {
	if n == nil {
		return Null()
	}
	return n
}

// Validation is the editor diagnostics of a draft (publication rules).
type Validation struct {
	Valid             bool    `json:"valid"`
	Issues            []Issue `json:"issues"`
	SchemaVersion     string  `json:"schema_version"`
	CoreCheckRequired bool    `json:"core_check_required"`
}

// ValidateSettings returns non-persistent editor diagnostics.
func ValidateSettings(settings *Node, p *Profile, baseURL string) (Validation, error) {
	issues, err := func() ([]Issue, error) {
		draft, err := PrepareEditor(settings, p, baseURL)
		if err != nil {
			return nil, err
		}
		sources := &Sources{}
		document, err := NativeDocument(draft, false, sources)
		if err != nil {
			return nil, err
		}
		return ValidateExpandedDocument(document, sources), nil
	}()
	var validation *ValidationError
	var settingsErr *SettingsError
	switch {
	case err == nil:
	case errors.As(err, &validation):
		issues = append([]Issue{}, validation.Issues...)
	case errors.As(err, &settingsErr) && settingsErr.Issues != nil:
		issues = settingsErr.Issues
	default:
		message, ok := IsSettingsError(err)
		if !ok {
			return Validation{}, err
		}
		issues = []Issue{{Path: Path{}, Message: message, Code: "structure", Severity: "error", Source: "config.yaml"}}
	}
	if issues == nil {
		issues = []Issue{}
	}
	return Validation{Valid: !hasError(issues), Issues: issues, SchemaVersion: "1.6.1", CoreCheckRequired: true}, nil
}
