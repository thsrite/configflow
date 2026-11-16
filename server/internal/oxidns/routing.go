package oxidns

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
	"unsafe"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/pystr"
)

// Port of backend/common/oxidns_common_routing.py (module C25): read and
// edit the recognizable routing portion of an OxiDNS document.
//
// The native providers and sequence are the source of truth. Historical
// template inputs are only retained as resource sources; they never supply
// displayed values. Unrecognized routing is left for the complete editor
// instead of being regenerated. Per-rule DNS actions are rule groups too,
// but only on generated providers that nothing else uses; their owned helper
// plugins are created and removed with the groups and never overwritten when
// edited by hand. The constants and the helpers C24 needed early are in
// common_routing.go; the rule_dns parts in ruledns_routing.go.

const (
	routingReason = "当前分流执行链包含自定义结构，请在完整参数中编辑。"
	missingRule   = "所选规则不在当前空间的可用规则库中，请刷新后重试。"
)

// routingFieldOrder is ROUTING_FIELDS in a fixed order (Python's frozenset
// order is hash dependent; the capabilities only reach jsonify, which sorts).
var routingFieldOrder = append(append(append([]string{}, Selectors...), RuleDNSFields...),
	"aggregate_rules", "default_forward", "custom_matches", "custom_match_position")

var customFields = []string{"custom_matches", "custom_match_position"}

// routingTargets is _TARGETS['domain'].
var routingTargets = map[string]string{"goto china_dns": "direct", "goto proxy_dns_seq": "proxy"}

// ---- Python glue -------------------------------------------------------

// pyBases maps a Python exception class to its base for except clauses.
var pyBases = map[string]string{
	ClassSettings: "ValueError", ClassReference: ClassSettings, ClassValidation: ClassSettings,
	"UnicodeError": "ValueError", "UnicodeDecodeError": "UnicodeError", "UnicodeEncodeError": "UnicodeError",
	"JSONDecodeError": "ValueError", "KeyError": "LookupError", "IndexError": "LookupError",
}

// pyClassOf is type(err).__name__ ("" for Go-only errors such as pending).
func pyClassOf(err error) string {
	if IsPending(err) {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Class
	}
	if class := convert.PyErrorClass(err); class != "" {
		return class
	}
	var p interface{ PythonClass() string }
	if errors.As(err, &p) {
		return p.PythonClass()
	}
	return ""
}

// pyIsInstance is isinstance(err, classes).
func pyIsInstance(err error, classes ...string) bool {
	for class := pyClassOf(err); class != ""; class = pyBases[class] {
		for _, want := range classes {
			if class == want {
				return true
			}
		}
	}
	return false
}

// tryExcept runs fn; a raised error of one of classes is returned, any other
// raise continues.
func tryExcept(fn func(), classes ...string) (caught error) {
	defer func() {
		if r := recover(); r != nil {
			if p, ok := r.(raised); ok && pyIsInstance(p.err, classes...) {
				caught = p.err
				return
			}
			panic(r)
		}
	}()
	fn()
	return nil
}

// configOf is the config dict (AttributeError for anything else, where
// Python's first config.get would fail).
func configOf(config any) *jsonx.Object {
	obj, ok := config.(*jsonx.Object)
	if !ok || obj == nil {
		raise(attributeError(config, "get"))
	}
	return obj
}

// setPluginsInPlace is document['plugins'][:] = items: every other holder
// of the old list (YAML aliases) sees the new contents too.
func setPluginsInPlace(document *jsonx.Object, old, items []any) {
	document.Set("plugins", items)
	if len(old) > 0 {
		relinkList(document, old, items, map[unsafe.Pointer]bool{})
	}
}

// capFor is _cap(editable, reason).
func capFor(editable bool, reason string) *jsonx.Object {
	return jsonx.Obj("editable", editable, "reason", reason, "target", "sequence_main")
}

func capEditable(caps *jsonx.Object, field string) bool {
	return caps.GetObject(field).Value("editable") == true
}

func capReason(caps *jsonx.Object, field string) string {
	return str(caps.GetObject(field).Value("reason"))
}

// routingPlugins is _plugins(document): tag -> plugin, empty when any plugin
// is not an object with a unique string tag.
func routingPlugins(document any) *jsonx.Object {
	result := jsonx.NewObject()
	list, ok := get(document, "plugins", nil).([]any)
	if !ok {
		return result
	}
	for _, element := range list {
		plugin, isObj := element.(*jsonx.Object)
		if !isObj || plugin == nil {
			return jsonx.NewObject()
		}
		tag, isText := plugin.Value("tag").(string)
		if !isText || result.Has(tag) {
			return jsonx.NewObject()
		}
		result.Set(tag, plugin)
	}
	return result
}

func pluginAt(plugins *jsonx.Object, tag string) *jsonx.Object {
	plugin, _ := plugins.Value(tag).(*jsonx.Object)
	return plugin
}

// routingMatches is _matches(entry).
func routingMatches(entry any) any {
	value := get(entry, "matches", nil)
	if text, ok := value.(string); ok {
		return []any{text}
	}
	return value
}

// singleMatch is _single_match(entry) (ok false = None).
func singleMatch(entry any) (string, bool) {
	values, ok := routingMatches(entry).([]any)
	if !ok || len(values) != 1 {
		return "", false
	}
	text, ok := values[0].(string)
	return text, ok
}

func singleMatchIs(entry any, want string) bool {
	match, ok := singleMatch(entry)
	return ok && match == want
}

// qtypeMatch is re.fullmatch(r'qtype(?:\s+\d+)+', value).
func qtypeMatch(value string) bool {
	if !strings.HasPrefix(value, "qtype") {
		return false
	}
	runes := []rune(value[len("qtype"):])
	if len(runes) == 0 {
		return false
	}
	i := 0
	for i < len(runes) {
		spaces := 0
		for i < len(runes) && pystr.IsSpace(runes[i]) {
			i++
			spaces++
		}
		digits := 0
		for i < len(runes) && isDecimalRune(runes[i]) {
			i++
			digits++
		}
		if spaces == 0 || digits == 0 {
			return false
		}
	}
	return true
}

// PrefixEnd is _prefix_end(args): the end of the recognized built-ins at
// the head of sequence_main, including imported deployment probe rules.
// The device policy (C26) uses it too.
func PrefixEnd(args []any) (end int, err error) {
	defer catch(&err)
	return prefixEnd(args), nil
}

func prefixEnd(args []any) int {
	index := 0
	for index < len(args) {
		entry := args[index]
		action := get(entry, "exec", nil)
		match, matched := singleMatch(entry)
		actionText, actionIsText := action.(string)
		switch {
		case jsonx.Equal(entry, watchdogRule):
			index++
		case !truthy(get(entry, "matches", nil)) && (strIn(action, "prefer_ipv4", "$query_recorder") ||
			actionIsText && strings.HasPrefix(actionText, "metrics_collector ")):
			index++
		case strIn(action, BlockQtypesExecs...) && matched && qtypeMatch(match):
			index++
		case action == "$custom_hosts" && !truthy(get(entry, "matches", nil)) && index+1 < len(args) &&
			get(args[index+1], "exec", nil) == "accept" && singleMatchIs(args[index+1], "has_resp"):
			index += 2
		case action == "goto adguard_reject" && matched && match == "question $adguard_rules":
			index++
		case jsonx.Equal(entry, jsonx.Obj("exec", "jump cf_device_policy")):
			// Device policy owns this hook; routing edits keep it ahead of
			// domain rules and never expose it as a user custom match.
			index++
		default:
			return index
		}
	}
	return index
}

// atomKey is the (part, key, value) tuple of _atom.
type atomKey struct{ part, key, value string }

// atomOf is _atom(part, key, value) (ok false = None).
func atomOf(part, key string, value any) (atomKey, bool) {
	text, ok := value.(string)
	if !ok {
		return atomKey{}, false
	}
	if key == "files" {
		text = strings.TrimPrefix(text, "./")
	}
	return atomKey{part, key, text}, true
}

type catalogRecord struct {
	suffix string
	parts  map[string]bool
	shapes map[string]bool // frozensets as sorted, comma-joined parts
	item   *jsonx.Object
}

// catalogData is the result of _catalog(config): records in profile order
// and atom -> owning rule ids.
type catalogData struct {
	order   []string
	records map[string]*catalogRecord
	atoms   map[atomKey]map[string]bool
}

func (c *catalogData) has(identity string) bool {
	_, ok := c.records[identity]
	return ok
}

func shapeKey(parts map[string]bool) string {
	keys := make([]string, 0, len(parts))
	for part := range parts {
		keys = append(keys, part)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func onlyElement(set map[string]bool) string {
	for key := range set {
		return key
	}
	return ""
}

// itemSuffix is _suffix(item).
func itemSuffix(entry any) string {
	if get(entry, "itemType", nil) == "ruleset" {
		return "rulesets"
	}
	return "rules"
}

// buildCatalog is _catalog(config): each rule's exact inline expression or
// generated resource path.
func buildCatalog(config *jsonx.Object) *catalogData {
	result := &catalogData{records: map[string]*catalogRecord{}, atoms: map[atomKey]map[string]bool{}}
	items := itemsOf(config)
	type sameKey struct{ identity, suffix string }
	same := map[sameKey][]any{}
	for _, entry := range items {
		if identity, ok := get(entry, "id", nil).(string); ok {
			key := sameKey{identity, itemSuffix(entry)}
			same[key] = append(same[key], entry)
		}
	}
	for _, entry := range items {
		identity, ok := get(entry, "id", nil).(string)
		if !ok || result.has(identity) {
			continue
		}
		suffix := itemSuffix(entry)
		settings := jsonx.NewObject()
		for _, key := range Selectors {
			settings.Set(key, []any{})
		}
		settings.Set("aggregate_rules", false)
		settings.Set("direct_"+suffix, []any{identity})
		var groups *jsonx.Object
		// Legacy shape: documents written before content-aware IP parts still
		// reference the IP file of a classical set without IP lines.
		if tryExcept(func() {
			groups = routingGroups(config, settings, LegacyAll, same[sameKey{identity, suffix}], true)
		}, ClassSettings, "KeyError", "TypeError", "ValueError") != nil {
			continue
		}
		parts := map[string]bool{}
		for _, part := range groups.Keys() {
			for _, group := range groups.GetList(part) {
				args := group.(*jsonx.Object).GetObject("args")
				for key, values := range args.All() {
					for _, value := range values.([]any) {
						if atom, ok := atomOf(part, key, value); ok {
							if result.atoms[atom] == nil {
								result.atoms[atom] = map[string]bool{}
							}
							result.atoms[atom][identity] = true
							parts[part] = true
						}
					}
				}
			}
		}
		current := map[string]bool{}
		if get(entry, "itemType", nil) == "ruleset" {
			for _, part := range rulesetPartsOf(entry) {
				current[part] = true
			}
		} else {
			for part := range parts {
				if part != "ip" {
					current[part] = true
				}
			}
		}
		contentAware := map[string]bool{}
		withIP := hasIPPart(entry, config)
		for part := range parts {
			if withIP || part != "ip" {
				contentAware[part] = true
			}
		}
		object, _ := entry.(*jsonx.Object)
		result.order = append(result.order, identity)
		result.records[identity] = &catalogRecord{suffix: suffix, parts: parts,
			shapes: map[string]bool{shapeKey(parts): true, shapeKey(contentAware): true, shapeKey(current): true},
			item:   object}
	}
	return result
}

// ipSuffix is _ip_suffix(entries, default_forward, probe): the IP routing
// clauses before the final fallback.
func ipSuffix(entries []any, defaultForward any, probe bool) []any {
	if len(entries) == 0 {
		return []any{}
	}
	if !probe {
		return append([]any{}, entries...)
	}
	out := append([]any{jsonx.Obj("exec", "jump local_probe")}, entries...)
	if defaultForward == "forward_local" {
		return append(out, jsonx.Obj("matches", []any{"has_resp"}, "exec", "goto default_direct_ip"))
	}
	return append(out, jsonx.Obj("exec", "drop_resp"))
}

// knownRules is _known(config): every rule of the space by id, disabled
// ones included.
func knownRules(config *jsonx.Object) *jsonx.Object {
	result := jsonx.NewObject()
	source := config.Value("rule_configs")
	var list []any
	if truthy(source) {
		list = pyIter(source)
	} else {
		list = itemsOf(config)
	}
	for _, element := range list {
		entry, ok := element.(*jsonx.Object)
		if !ok || entry == nil {
			continue
		}
		if identity, ok := entry.Value("id").(string); ok {
			result.SetDefault(identity, entry)
		}
	}
	return result
}

// storedFollow is _stored_follow(settings, config).
func storedFollow(settings *jsonx.Object, config *jsonx.Object) []any {
	follow := settings.Value("rule_dns_follow")
	known := knownRules(config)
	list, ok := follow.([]any)
	if !ok {
		return []any{}
	}
	out := []any{}
	for _, identity := range dictFromKeys(list) {
		if text, isText := identity.(string); isText && known.Has(text) {
			out = append(out, text)
		}
	}
	return out
}

// dictFromKeys is list(dict.fromkeys(values)) (TypeError for unhashable
// values; 1, 1.0 and True are one key).
func dictFromKeys(values []any) []any {
	out := []any{}
	for _, value := range values {
		hashable(value)
		if !listContains(out, value) {
			out = append(out, value)
		}
	}
	return out
}

// routeOf is _route_of(values, identity) (nil = None).
func routeOf(values *jsonx.Object, identity string) any {
	if action, ok := values.GetObject("rule_actions").Get(identity); ok && action != nil {
		return routeKey(action)
	}
	for _, route := range ListRoutes {
		if pyContains(values.Value(route+"_rules"), identity) || pyContains(values.Value(route+"_rulesets"), identity) {
			return route
		}
	}
	return nil
}

// recognizedGroup is one rule group of _recognize.
type recognizedGroup struct {
	index    int
	tag      string
	route    any // string or invalidRoute
	args     any
	entry    *jsonx.Object
	provider *jsonx.Object
	action   *jsonx.Object // nil: absent or None
}

// routingState is the state dict of _recognize / _analyze. full is false
// for the early returns that only carry values and capabilities.
type routingState struct {
	values, caps  *jsonx.Object
	full          bool
	plugins       *jsonx.Object
	main          *jsonx.Object
	groups        map[string][]*recognizedGroup
	prefix        []any
	dynamic       []any
	custom        []any
	final         *jsonx.Object
	records       *catalogData
	claimed       bool
	customIndices []int
	actualMainTag string
}

func (s *routingState) allGroups() []*recognizedGroup {
	return append(append([]*recognizedGroup{}, s.groups["domain"]...), s.groups["ip"]...)
}

// ownedGroup is _owned_group(mentions, entry, tag): a generated provider
// that only this one sequence_main entry uses.
func ownedGroup(mentionsOf func() [3]*mentions, entry *jsonx.Object, tag string) bool {
	if !isGroupTag(tag) || !exactKeys(entry, "matches", "exec") {
		return false
	}
	m := mentionsOf()
	args, top, plugins := m[0], m[1], m[2]
	if len(args.positions(tag)) != 1 || len(top.positions(tag)) > 0 {
		return false
	}
	for _, position := range plugins.positions(tag) {
		if !strIn(plugins.values[position].(*jsonx.Object).Value("tag"), "sequence_main", "rules_reload", tag) {
			return false
		}
	}
	return true
}

func partArgKey(part string) string {
	if part == "domain" {
		return "exps"
	}
	return "ips"
}

// onlyKeys is not (set(obj) - set(allowed)).
func onlyKeys(obj *jsonx.Object, allowed ...string) bool {
	for _, key := range obj.Keys() {
		if !strIn(key, allowed...) {
			return false
		}
	}
	return true
}

// uniqueAtoms is _unique_atoms(part, provider, atoms): every provider value
// belongs to exactly one catalog rule.
func uniqueAtoms(part string, provider *jsonx.Object, atoms map[atomKey]map[string]bool) bool {
	args, ok := provider.Value("args").(*jsonx.Object)
	if !ok || args == nil || args.Len() == 0 || !onlyKeys(args, "files", partArgKey(part)) {
		return false
	}
	for key, values := range args.All() {
		list, ok := values.([]any)
		if !ok || len(list) == 0 {
			return false
		}
		for _, value := range list {
			atom, ok := atomOf(part, key, value)
			if !ok || len(atoms[atom]) != 1 {
				return false
			}
		}
	}
	return true
}

func withoutKey(document *jsonx.Object, skip string) *jsonx.Object {
	out := jsonx.NewObject()
	for key, value := range document.All() {
		if key != skip {
			out.Set(key, value)
		}
	}
	return out
}

// helperExec is _HELPER_EXEC.match(value).
func helperExec(value string) bool {
	return strings.HasPrefix(value, "$cf_dns_") || strings.HasPrefix(value, "goto cf_dns_") ||
		strings.HasPrefix(value, "jump cf_dns_")
}

// helperTag is _HELPER_TAG.fullmatch(tag): matched, and group 1 (the key,
// "" for the blackhole).
func helperTag(tag string) (bool, string) {
	if tag == BlackholeTag {
		return true, ""
	}
	for _, prefix := range []string{UpstreamPrefix, ForwardPrefix, CachePrefix} {
		if strings.HasPrefix(tag, prefix) && ruleKey.MatchString(tag[len(prefix):]) {
			return true, tag[len(prefix):]
		}
	}
	return false, ""
}

func isHelperTag(tag any) bool {
	ok, _ := helperTag(str(tag))
	return ok
}

// analyzeRouting is _analyze(document, settings, config): the recognized
// routing state. Upgrading never locks a document that was editable before
// rule DNS actions existed: when claiming action entries breaks recognition
// and no helper is in use, those entries are read as custom matches.
func analyzeRouting(document, settings *jsonx.Object, config *jsonx.Object) *routingState {
	var built *catalogData
	catalog := func() *catalogData {
		if built == nil {
			built = buildCatalog(config)
		}
		return built
	}
	state := recognizeRouting(document, settings, config, true, catalog)
	if state.full && state.claimed {
		helper := false
		for _, entry := range asList(state.main.Value("args")) {
			if helperExec(entry.(*jsonx.Object).Value("exec").(string)) {
				helper = true
				break
			}
		}
		if !helper {
			legacy := recognizeRouting(document, settings, config, false, catalog)
			if !capEditable(state.caps, "direct_rules") || (!capEditable(state.caps, "custom_matches") &&
				capEditable(legacy.caps, "custom_matches")) {
				return legacy
			}
		}
	}
	return state
}

// observedLegacyGroups is _observed_legacy_groups(config, values, assigned,
// atoms, aggregate): old per-rule parts validated against the current
// catalog.
func observedLegacyGroups(config, values *jsonx.Object, assigned map[string]map[string]any,
	atoms map[atomKey]map[string]bool, aggregate bool) map[string][]*jsonx.Object {
	settings := values.Clone()
	settings.Set("aggregate_rules", false)
	raw := routingGroups(config, settings, LegacyAll, nil, false)
	expected := map[string][]*jsonx.Object{"domain": {}, "ip": {}}
	for _, part := range raw.Keys() {
		for _, element := range raw.GetList(part) {
			group := element.(*jsonx.Object)
			owners := map[string]bool{}
			for key, entries := range group.GetObject("args").All() {
				for _, value := range entries.([]any) {
					if atom, ok := atomOf(part, key, value); ok {
						for identity := range atoms[atom] {
							owners[identity] = true
						}
					}
				}
			}
			if len(owners) != 1 {
				continue
			}
			if _, ok := assigned[onlyElement(owners)][part]; !ok {
				continue
			}
			target := expected[part]
			if aggregate && len(target) > 0 && jsonx.Equal(target[len(target)-1].Value("route"), group.Value("route")) {
				last := target[len(target)-1].GetObject("args")
				for key, entries := range group.GetObject("args").All() {
					existing, _ := last.SetDefault(key, []any{}).([]any)
					last.Set(key, appendCopy(existing, entries.([]any)...))
				}
			} else {
				expected[part] = append(target, copyValue(group).(*jsonx.Object))
			}
		}
	}
	return expected
}

// parseGroupMatch is re.fullmatch(r'(qname|resp_ip) \$([^\s]+)', match or
// ”): the part and the provider tag.
func parseGroupMatch(match string, ok bool) (string, string, bool) {
	if !ok {
		return "", "", false
	}
	var part, rest string
	switch {
	case strings.HasPrefix(match, "qname $"):
		part, rest = "domain", match[len("qname $"):]
	case strings.HasPrefix(match, "resp_ip $"):
		part, rest = "ip", match[len("resp_ip $"):]
	default:
		return "", "", false
	}
	if rest == "" || strings.IndexFunc(rest, pystr.IsSpace) >= 0 {
		return "", "", false
	}
	return part, rest, true
}

func listOrEmpty(list []any) []any {
	if list == nil {
		return []any{}
	}
	return list
}

// recognizeRouting is _recognize(document, settings, config, claim,
// catalog).
func recognizeRouting(document, settings *jsonx.Object, config *jsonx.Object, claim bool,
	catalog func() *catalogData) *routingState {
	values := jsonx.NewObject()
	for _, key := range Selectors {
		values.Set(key, []any{})
	}
	values.Set("aggregate_rules", nil)
	values.Set("default_forward", nil)
	values.Set("custom_matches", []any{})
	values.Set("custom_match_position", nil)
	values.Set("rule_actions", jsonx.NewObject())
	values.Set("rule_dns_follow", storedFollow(settings, config))
	caps := jsonx.NewObject()
	for _, key := range routingFieldOrder {
		caps.Set(key, capFor(false, routingReason))
	}
	state := &routingState{values: values, caps: caps}
	plugins := routingPlugins(document)
	main := pluginAt(plugins, "sequence_main")
	if main == nil {
		main = jsonx.NewObject()
	}
	args, isList := main.Value("args").([]any)
	if main.Value("type") != "sequence" || !isList || len(args) == 0 {
		return state
	}
	for _, element := range args {
		entry, ok := element.(*jsonx.Object)
		if !ok || entry == nil {
			return state
		}
		if _, ok := entry.Value("exec").(string); !ok {
			return state
		}
	}
	final := args[len(args)-1].(*jsonx.Object)
	finalExec := final.Value("exec").(string)
	if _, target := routingTargets[finalExec]; !truthy(final.Value("matches")) && target {
		if finalExec == "goto china_dns" {
			values.Set("default_forward", "forward_local")
		} else {
			values.Set("default_forward", "forward_remote")
		}
		caps.Set("default_forward", capFor(true, ""))
	} else {
		return state
	}
	var records *catalogData
	if catalog != nil {
		records = catalog()
	} else {
		records = buildCatalog(config)
	}
	atoms := records.atoms

	var built *[3]*mentions
	mentionsOf := func() [3]*mentions {
		if built == nil {
			m := [3]*mentions{newMentions(args), newMentions([]any{withoutKey(document, "plugins")}),
				newMentions(plugins.Values())}
			built = &m
		}
		return *built
	}
	groups := map[string][]*recognizedGroup{"domain": {}, "ip": {}}
	type indexed struct {
		index int
		entry *jsonx.Object
	}
	var dynamic []indexed
	middleEnd := len(args) - 1
	recognized := true
	invalid, claimed := "", false
	for index, element := range args[:len(args)-1] {
		entry := element.(*jsonx.Object)
		exec := entry.Value("exec").(string)
		match, hasMatch := singleMatch(entry)
		if _, target := routingTargets[exec]; hasMatch && match == "qname $dynamic_domains" && target {
			dynamic = append(dynamic, indexed{index, entry})
			middleEnd = min(middleEnd, index)
			continue
		}
		if part, tag, ok := parseGroupMatch(match, hasMatch); ok {
			provider := pluginAt(plugins, tag)
			if provider == nil {
				provider = jsonx.NewObject()
			}
			var route any
			kind := "ip_set"
			if part == "domain" {
				kind = "domain_set"
			}
			if provider.Value("type") == kind {
				route = parseExec(exec, part, plugins)
			}
			if route != nil && !strIn(route, ListRoutes...) {
				// A user's own match (or one reusing a generated provider) stays a custom match.
				_, isInvalid := route.(invalidRoute)
				if claim && ownedGroup(mentionsOf, entry, tag) && (isInvalid || uniqueAtoms(part, provider, atoms)) {
					claimed = true
				} else {
					route = nil
				}
			}
			if route != nil {
				group := &recognizedGroup{index: index, tag: tag, route: route, args: provider.Value("args"),
					entry: entry, provider: provider}
				if bad, isInvalid := route.(invalidRoute); isInvalid {
					if invalid == "" {
						invalid = bad.reason
					}
				} else if strings.HasPrefix(route.(string), "upstream:") {
					group.action = pinnedAction(exec[len("goto "+UpstreamPrefix):], plugins)
				}
				groups[part] = append(groups[part], group)
				if part == "ip" {
					middleEnd = min(middleEnd, index)
				}
				continue
			}
		}
		if !truthy(entry.Value("matches")) && exec == "jump local_probe" {
			middleEnd = min(middleEnd, index)
		}
	}
	if len(dynamic) > 1 {
		return state
	}
	// The generated suffix has a single probe, ordered IP routes, and one final fallback.
	var entries, ipRoutes []any
	for _, group := range groups["ip"] {
		entries = append(entries, group.entry)
		ipRoutes = append(ipRoutes, group.route)
	}
	defaultForward := values.Value("default_forward")
	candidates := [][]any{ipSuffix(entries, defaultForward, true)}
	if !probeNeeded(ipRoutes, defaultForward) {
		// Written without the probe; older documents still carry it.
		candidates = append([][]any{ipSuffix(entries, defaultForward, false)}, candidates...)
	}
	var head []any
	if len(dynamic) > 0 {
		head = []any{dynamic[0].entry}
	}
	matched := false
	for _, candidate := range candidates {
		want := append(append(append([]any{}, head...), candidate...), final)
		if sameValue(append([]any{}, args[middleEnd:]...), want) {
			matched = true
			break
		}
	}
	if !matched {
		caps.Set("default_forward", capFor(false, routingReason))
		return state
	}
	prefixEndAt := prefixEnd(args[:middleEnd])
	domainIndices := map[int]bool{}
	minDomain, maxDomain := -1, -1
	for _, group := range groups["domain"] {
		domainIndices[group.index] = true
		if minDomain < 0 || group.index < minDomain {
			minDomain = group.index
		}
		maxDomain = max(maxDomain, group.index)
	}
	for index := range domainIndices {
		if index < prefixEndAt || index >= middleEnd {
			return state
		}
	}
	var customIndices []int
	var custom []any
	for i := prefixEndAt; i < middleEnd; i++ {
		if !domainIndices[i] {
			customIndices = append(customIndices, i)
			custom = append(custom, args[i])
		}
	}
	customValid := true
	for _, element := range custom {
		entry := element.(*jsonx.Object)
		list, ok := routingMatches(entry).([]any)
		valid := ok && len(list) > 0
		for _, match := range list {
			if text, isText := match.(string); !isText || pystr.Strip(text) == "" {
				valid = false
			}
		}
		if !valid || pystr.Strip(entry.Value("exec").(string)) == "" {
			customValid = false
			break
		}
	}
	position := "tail"
	if len(customIndices) > 0 && len(domainIndices) > 0 {
		if customIndices[len(customIndices)-1] < minDomain {
			position = "head"
		} else if customIndices[0] <= maxDomain {
			customValid = false
		}
	}
	values.Set("custom_matches", copyValue(listOrEmpty(custom)))
	if customValid && len(custom) > 0 {
		values.Set("custom_match_position", position)
	} else {
		values.Set("custom_match_position", nil)
	}
	if customValid {
		caps.Set("custom_matches", capFor(true, ""))
		reason := ""
		switch {
		case len(custom) == 0:
			reason = "请先添加自定义匹配，再设置执行顺序。"
		case len(domainIndices) == 0:
			reason = "当前没有域名分流规则，两种执行顺序效果相同。"
		}
		caps.Set("custom_match_position", capFor(len(custom) > 0, reason))
	} else {
		recognized = false
	}
	if invalid != "" {
		// Rebuilding the chain for a custom match edit would silently drop the
		// entry whose helper was edited by hand, so custom matches lock too.
		recognized = false
		locked := capFor(false, invalid)
		caps.Set("custom_matches", locked)
		caps.Set("custom_match_position", locked)
	}
	assigned := map[string]map[string]any{}
	var assignedOrder []string
	type orphan struct{ key, value string }
	var orphans []orphan
	actions := map[string]*jsonx.Object{}
	for _, part := range []string{"domain", "ip"} {
		for _, group := range groups[part] {
			route, isText := group.route.(string)
			if !isText {
				continue
			}
			if !strIn(route, ListRoutes...) {
				if group.action != nil {
					actions[route] = group.action
				} else {
					actions[route] = jsonx.Obj("action", route)
				}
			}
			argsValue, ok := group.args.(*jsonx.Object)
			if !ok || argsValue == nil || argsValue.Len() == 0 || !onlyKeys(argsValue, "files", partArgKey(part)) ||
				!exactKeys(group.entry, "matches", "exec") {
				recognized = false
				continue
			}
			for key, element := range argsValue.All() {
				list, ok := element.([]any)
				if !ok || len(list) == 0 {
					recognized = false
					continue
				}
				for _, value := range list {
					atom, hasAtom := atomOf(part, key, value)
					var owners map[string]bool
					if hasAtom {
						owners = atoms[atom]
					}
					if len(owners) != 1 {
						recognized = false
						if len(owners) == 0 {
							if !hasAtom {
								raise(pyErr("TypeError", "'NoneType' object is not subscriptable"))
							}
							orphans = append(orphans, orphan{atom.key, atom.value})
						}
						continue
					}
					identity := onlyElement(owners)
					previous, seen := assigned[identity]
					if !seen {
						previous = map[string]any{}
						assigned[identity] = previous
						assignedOrder = append(assignedOrder, identity)
					}
					if _, dup := previous[part]; dup {
						recognized = false
					}
					previous[part] = route
				}
			}
		}
	}
	ruleActions := values.GetObject("rule_actions")
	for _, identity := range assignedOrder {
		parts := assigned[identity]
		record := records.records[identity]
		routes := map[string]bool{}
		shape := map[string]bool{}
		for part, route := range parts {
			routes[route.(string)] = true
			shape[part] = true
		}
		if !record.shapes[shapeKey(shape)] || len(routes) != 1 {
			recognized = false
			continue
		}
		route := onlyElement(routes)
		if strIn(route, ListRoutes...) {
			key := route + "_" + record.suffix
			values.Set(key, append(values.GetList(key), identity))
		} else {
			ruleActions.Set(identity, copyValue(actions[route]))
		}
	}
	// Reconstruct profile order instead of deriving it from stale selection metadata.
	for _, key := range Selectors {
		selected := map[string]bool{}
		for _, identity := range values.GetList(key) {
			selected[identity.(string)] = true
		}
		ordered := []any{}
		for _, identity := range records.order {
			if selected[identity] {
				ordered = append(ordered, identity)
			}
		}
		values.Set(key, ordered)
	}
	orderedActions := jsonx.NewObject()
	for _, identity := range records.order {
		if action, ok := ruleActions.Get(identity); ok {
			orderedActions.Set(identity, action)
		}
	}
	values.Set("rule_actions", orderedActions)
	var matches []bool
	if recognized {
		for _, aggregate := range []bool{true, false} {
			expected := observedLegacyGroups(config, values, assigned, atoms, aggregate)
			equal := true
			for _, part := range []string{"domain", "ip"} {
				got, want := []any{}, []any{}
				for _, group := range groups[part] {
					got = append(got, jsonx.Obj("route", group.route, "args", group.args))
				}
				for _, group := range expected[part] {
					want = append(want, jsonx.Obj("route", group.Value("route"), "args", group.Value("args")))
				}
				if !sameValue(got, want) {
					equal = false
				}
			}
			if equal {
				matches = append(matches, aggregate)
			}
		}
		recognized = len(matches) > 0
	}
	if recognized {
		if len(matches) == 1 {
			values.Set("aggregate_rules", matches[0])
		} else {
			values.Set("aggregate_rules", nil)
		}
		for _, key := range append(append([]string{}, Selectors...), RuleDNSFields...) {
			caps.Set(key, capFor(true, ""))
		}
		reason := ""
		if len(matches) == 2 {
			reason = "当前没有相邻且同策略的规则，无需合并。"
		}
		caps.Set("aggregate_rules", capFor(len(matches) == 1, reason))
	} else {
		reason := invalid
		if reason == "" {
			reason = "规则资源或顺序无法唯一对应当前规则库，请在完整参数中编辑。"
		}
		var hints []string
		if invalid == "" {
			paths := []any{}
			for _, o := range orphans {
				if o.key == "files" {
					paths = append(paths, o.value)
				}
			}
			hints = unavailableRulesets(config, paths)
		}
		if len(hints) > 0 {
			reason = "分流里有不可用的规则集，暂时不能在这里编辑。" + strings.Join(hints, "；") + "。"
		} else if len(orphans) > 0 && invalid == "" {
			reason = "分流里有规则库中不存在的单条规则（" + orphans[0].value + "），请在完整参数中删除后再编辑。"
		}
		for _, key := range append(append(append([]string{}, Selectors...), RuleDNSFields...), "aggregate_rules") {
			caps.Set(key, capFor(false, reason))
		}
	}
	state.full = true
	state.plugins = plugins
	state.main = main
	state.groups = groups
	state.prefix = args[:prefixEndAt]
	for _, d := range dynamic {
		state.dynamic = append(state.dynamic, d.entry)
	}
	state.custom = custom
	state.final = final
	state.records = records
	state.claimed = claimed
	state.customIndices = customIndices
	return state
}

// aliasedCustomState is _aliased_custom_state(document, settings, config):
// recognize recorded managed renames without changing their native names
// (custom-match controls only). nil is None.
func aliasedCustomState(document, settings *jsonx.Object, config *jsonx.Object) *routingState {
	parked, _ := settings.Value("_common_disabled").(*jsonx.Object)
	var aliasesValue any
	if parked != nil {
		aliasesValue = parked.Value("renamed_plugins")
	}
	all, ok := aliasesValue.(*jsonx.Object)
	if !ok || all == nil || all.Len() == 0 {
		return nil
	}
	aliases := jsonx.NewObject()
	for old, new := range all.All() {
		if managedTags[old] {
			aliases.Set(old, new)
		}
	}
	if aliases.Len() == 0 {
		return nil
	}
	for _, value := range aliases.Values() {
		new, isText := value.(string)
		if !isText {
			return nil
		}
		length := utf8.RuneCountInString(new)
		if length == 0 || length > 64 || !Tag.MatchString(new) || hasAnyPrefix(new, "qs.exec.", "qs.match.", "qs.cron.") {
			return nil
		}
	}
	plugins := routingPlugins(document)
	expanded := plugins
	if truthy(document.Value("include")) {
		expanded = routingPlugins(nativeDocument(settings, false, nil))
	}
	distinct := map[string]bool{}
	for _, value := range aliases.Values() {
		distinct[value.(string)] = true
	}
	if expanded.Len() == 0 || len(distinct) != aliases.Len() {
		return nil
	}
	for old, value := range aliases.All() {
		target := pluginAt(plugins, value.(string))
		if expanded.Has(old) || target == nil || target.Value("type") != tagType(old) {
			return nil
		}
	}
	canonical := copyValue(document).(*jsonx.Object)
	byTag := routingPlugins(canonical)
	for old, value := range aliases.All() {
		new := value.(string)
		pluginAt(byTag, new).Set("tag", old)
		renameReferences(canonical, new, old)
	}
	state := analyzeRouting(canonical, settings, config)
	mainTag := "sequence_main"
	if value, ok := aliases.Get("sequence_main"); ok {
		mainTag = value.(string)
	}
	main := pluginAt(plugins, mainTag)
	if main == nil {
		main = jsonx.NewObject()
	}
	if state.full {
		custom := []any{}
		for _, index := range state.customIndices {
			custom = append(custom, copyValue(listItem(item(main, "args"), index)))
		}
		state.values.Set("custom_matches", custom)
	}
	for _, field := range customFields {
		state.caps.GetObject(field).Set("target", mainTag)
	}
	state.actualMainTag = mainTag
	return state
}

// listItem is value[index] for a list (TypeError / IndexError otherwise).
func listItem(value any, index int) any {
	list, ok := value.([]any)
	if !ok {
		raise(pyErr("TypeError", "'"+typeName(value)+"' object is not subscriptable"))
	}
	if index >= len(list) {
		raise(pyErr("IndexError", "list index out of range"))
	}
	return list[index]
}

// ReadRouting is read_routing(document, settings, config): {"values",
// "capabilities"} of the routing controls.
func ReadRouting(document, settings, config *jsonx.Object) (result *jsonx.Object, err error) {
	defer catch(&err)
	return readRoutingImpl(document, settings, config), nil
}

func readRoutingImpl(document, settings *jsonx.Object, configValue any) *jsonx.Object {
	config := configOf(configValue)
	state := analyzeRouting(document, settings, config)
	if aliased := aliasedCustomState(document, settings, config); aliased != nil {
		for _, field := range customFields {
			state.values.Set(field, aliased.values.Value(field))
			state.caps.Set(field, aliased.caps.Value(field))
		}
	}
	return jsonx.Obj("values", copyValue(state.values), "capabilities", copyValue(state.caps))
}

// MigrateDomainOnlyRouting is migrate_domain_only_routing(document,
// settings, config, base_url): old managed IP routing rendered as
// domain-only without changing the draft. Only a fully recognizable owned
// suffix is migrated; otherwise document and settings are returned as they
// are (the same objects).
func MigrateDomainOnlyRouting(document, settings, config *jsonx.Object, baseURL string) (doc, values *jsonx.Object, err error) {
	defer catch(&err)
	doc, values = migrateDomainOnlyRouting(document, settings, config, baseURL)
	return doc, values, nil
}

func migrateDomainOnlyRouting(document, settings, config *jsonx.Object, baseURL string) (*jsonx.Object, *jsonx.Object) {
	// Publication expands includes first. Check the source too so an expanded
	// custom document is not mistaken for a wholly managed draft.
	source := jsonx.NewObject()
	if strip(settings.GetOr("native_config", "")) != "" {
		source = loadNativeYAML(item(settings, "native_config"), DefaultNativeLabel)
	}
	if truthy(document.Value("include")) || truthy(source.Value("include")) {
		return document, settings
	}
	current := analyzeRouting(document, settings, config)
	if !current.full || len(current.groups["ip"]) == 0 {
		return document, settings
	}
	for _, key := range append(append([]string{}, Selectors...), "rule_actions") {
		if !capEditable(current.caps, key) {
			return document, settings
		}
	}
	// A recognizable routing suffix can still share a custom update pipeline:
	// automatic export then preserves the draft atomically.
	var rendered, values *jsonx.Object
	if tryExcept(func() {
		rendered, values = applyRoutingImpl(document, settings, config, jsonx.NewObject(), baseURL, config, true, nil)
		// Empty owned executors cannot be emitted with an empty download list.
		values.Set("native_config", dump(rendered))
		finishRuleUpdates(newCommonContext(rendered, values), jsonx.Obj("direct_rulesets", []any{}), config, baseURL)
		values.Set("native_config", dump(rendered))
	}, ClassSettings) != nil {
		return document, settings
	}
	return rendered, values
}

// usesRule is _uses_rule(document, config, identity): whether any
// domain/IP provider still holds a resource generated for identity.
func usesRule(document *jsonx.Object, config *jsonx.Object, identity string) bool {
	atoms := buildCatalog(config).atoms
	list := document.Value("plugins")
	if !truthy(list) {
		list = []any{}
	}
	for _, element := range pyIter(list) {
		plugin, ok := element.(*jsonx.Object)
		if !ok || plugin == nil {
			continue
		}
		kind := plugin.Value("type")
		hashable(kind)
		part := ""
		switch kind {
		case "domain_set":
			part = "domain"
		case "ip_set":
			part = "ip"
		}
		if part == "" {
			continue
		}
		args, ok := plugin.Value("args").(*jsonx.Object)
		if !ok || args == nil {
			continue
		}
		for key, values := range args.All() {
			for _, value := range asList(values) {
				if atom, ok := atomOf(part, key, value); ok && atoms[atom][identity] {
					return true
				}
			}
		}
	}
	return false
}

// RemoveRule is remove_rule(settings, config, identity, base_url): native
// settings without a rule that is about to be deleted, or nil if unused.
func RemoveRule(settings, config *jsonx.Object, identity, baseURL string) (result *jsonx.Object, err error) {
	defer catch(&err)
	return removeRule(settings, config, identity, baseURL), nil
}

func removeRule(settings, config *jsonx.Object, identity, baseURL string) *jsonx.Object {
	document := loadNativeYAML(item(settings, "native_config"), DefaultNativeLabel)
	state := analyzeRouting(document, settings, config)
	values := state.values
	// A rule is routed by a selector list or by its own DNS action.
	changes := jsonx.NewObject()
	for _, key := range Selectors {
		list := values.GetList(key)
		if !pyContains(list, identity) {
			continue
		}
		kept := []any{}
		for _, value := range list {
			if value != identity {
				kept = append(kept, value)
			}
		}
		changes.Set(key, kept)
	}
	if actions := values.GetObject("rule_actions"); actions.Has(identity) {
		kept := jsonx.NewObject()
		for key, value := range actions.All() {
			if key != identity {
				kept.Set(key, value)
			}
		}
		changes.Set("rule_actions", kept)
	}
	editable := true
	for _, key := range changes.Keys() {
		if !capEditable(state.caps, key) {
			editable = false
			break
		}
	}
	var result *jsonx.Object
	if changes.Len() > 0 && editable {
		// Same path as unchecking it in the editor, which also reconciles rule
		// updates and drops helpers no other rule uses.
		result = asObject(item(applyCommon(settings, changes, config, baseURL), "settings"))
	} else if changes.Len() == 0 && !usesRule(document, config, identity) {
		return nil
	}
	// Hand-written providers outside the recognized routing are never rewritten.
	if result == nil || usesRule(loadNativeYAML(item(result, "native_config"), DefaultNativeLabel), config, identity) {
		fail("OxiDNS 还在用这条规则，而且那部分配置是手动写的，没法自动移除。" +
			"请先在 OxiDNS 设置的 YAML 中删掉对它的引用，再删除。")
	}
	return result
}

// newName is _new_name(preferred, occupied).
func newName(preferred string, occupied map[string]bool) string {
	result, index := preferred, 2
	for occupied[result] {
		result = preferred + "_" + itoa(index)
		index++
	}
	occupied[result] = true
	return result
}

// oldGroup is what place_routing_groups reads of a recognized group.
type oldGroup struct {
	tag          string
	providerType any
	args         any
}

// PlaceRoutingGroups is place_routing_groups(plugins, old, expected,
// external): tag and insert routing providers; returns the new plugin list
// and the placed groups ({domain, ip} of {route, args, tag}). old are
// recognized groups (objects with "tag", "provider" and "args"). Rule sync
// replays draft routing edits with this too.
func PlaceRoutingGroups(plugins []any, old []any, expected *jsonx.Object, external []string) (placed []any, groups *jsonx.Object, err error) {
	defer catch(&err)
	var olds []oldGroup
	for _, element := range old {
		tag, ok := item(element, "tag").(string)
		if !ok {
			raise(pyErr("TypeError", "group tag is not a string"))
		}
		olds = append(olds, oldGroup{tag, item(item(element, "provider"), "type"), item(element, "args")})
	}
	set := map[string]bool{}
	for _, tag := range external {
		set[tag] = true
	}
	placed, groups = placeRoutingGroups(plugins, olds, expected, set)
	return placed, groups, nil
}

func placeRoutingGroups(plugins []any, old []oldGroup, expected *jsonx.Object, external map[string]bool) ([]any, *jsonx.Object) {
	occupied := map[string]bool{}
	for _, plugin := range plugins {
		tag := get(plugin, "tag", nil)
		hashable(tag)
		if text, ok := tag.(string); ok {
			occupied[text] = true
		}
	}
	retained := map[string]bool{}
	additions := []any{}
	result := jsonx.Obj("domain", []any{}, "ip", []any{})
	for _, part := range expected.Keys() {
		if !result.Has(part) {
			raise(pyErr("KeyError", jsonx.Repr(part)))
		}
		for _, element := range pyIter(expected.Value(part)) {
			group := element.(*jsonx.Object)
			kind := "ip_set"
			if part == "domain" {
				kind = "domain_set"
			}
			var tag string
			found := false
			for _, candidate := range old {
				if !retained[candidate.tag] && candidate.providerType == kind && sameValue(candidate.args, group.Value("args")) {
					tag = candidate.tag
					retained[tag] = true
					found = true
					break
				}
			}
			if !found {
				tag = newName(str(item(group, "tag")), occupied)
				additions = append(additions, jsonx.Obj("tag", tag, "type", kind, "args", copyValue(group.Value("args"))))
			}
			placedGroup := copyValue(group).(*jsonx.Object)
			placedGroup.Set("tag", tag)
			result.Set(part, append(result.GetList(part), placedGroup))
		}
	}
	removable := map[string]bool{}
	for _, group := range old {
		if !retained[group.tag] && !external[group.tag] {
			removable[group.tag] = true
		}
	}
	// min(..., default=next(...)): the default is evaluated first.
	insertion := -1
	for index, plugin := range plugins {
		if get(plugin, "tag", nil) == "sequence_main" {
			insertion = index
			break
		}
	}
	if insertion < 0 {
		raise(pyErr("StopIteration", ""))
	}
	for index, plugin := range plugins {
		if text, ok := get(plugin, "tag", nil).(string); ok && removable[text] {
			insertion = index
			break
		}
	}
	placed := []any{}
	for index, plugin := range plugins {
		if index == insertion {
			placed = append(placed, additions...)
		}
		if text, ok := get(plugin, "tag", nil).(string); !ok || !removable[text] {
			placed = append(placed, plugin)
		}
	}
	return placed, result
}

// ReloadReferences is reload_references(existing, old_tags, new_tags,
// plugins): the edited reload list, where retained entries keep their order
// and new providers are appended. plugins is the tag -> plugin map.
func ReloadReferences(existing []any, oldTags map[string]bool, newTags []string, plugins *jsonx.Object) []any {
	references := []any{}
	values := append([]any{}, existing...)
	for _, tag := range newTags {
		values = append(values, "$"+tag)
	}
	inNew := func(tag string) bool {
		for _, t := range newTags {
			if t == tag {
				return true
			}
		}
		return false
	}
	for _, value := range values {
		if listContains(references, value) {
			continue
		}
		text, isText := value.(string)
		if !isText {
			references = append(references, value)
			continue
		}
		tag := strings.TrimPrefix(text, "$")
		if inNew(tag) || !oldTags[tag] || plugins.Has(tag) {
			references = append(references, value)
		}
	}
	return references
}

// resourceKey is relative_resource(path) as a set key.
func resourceKey(p *string) string {
	if p == nil {
		return "\x00None"
	}
	return *p
}

// ResourceDownloads is resource_downloads(document, settings, config,
// base_url): download descriptors for actual provider references, without
// core validation.
func ResourceDownloads(document, settings, config *jsonx.Object, baseURL string) (downloads []any, err error) {
	defer catch(&err)
	return resourceDownloadsImpl(document, settings, config, baseURL, nil), nil
}

func resourceDownloadsImpl(document, settings *jsonx.Object, configValue any, baseURL string, records *catalogData) []any {
	referenced := map[string]bool{}
	for _, p := range resourceReferences(document) {
		referenced[resourceKey(relativeResource(p))] = true
	}
	config := configOf(configValue)
	effective := baseOf(config, baseURL)
	desired := []any{}
	if records == nil {
		records = buildCatalog(config)
	}
	for _, identity := range records.order {
		record := records.records[identity]
		if record.suffix != "rulesets" {
			continue
		}
		parts := make([]string, 0, len(record.parts))
		for part := range record.parts {
			parts = append(parts, part)
		}
		sort.Strings(parts)
		for _, part := range parts {
			localPath := rulePath(record.item, part)
			if !referenced[resourceKey(relativeResource(localPath))] {
				continue
			}
			url, ok := record.item.GetOr("url", "").(string)
			if !ok || url == "" {
				fail("已选择的规则集缺少下载来源。")
			}
			if strings.HasPrefix(url, "/") {
				url = effective + url
			}
			proxied, err := mosdns.BuildRuleProxyURL(config, effective, url, part, nil)
			must(err)
			dir, name := purePosixParentName(localPath)
			desired = append(desired, jsonx.Obj("url", proxied, "dir", dir, "filename", name))
		}
	}
	adguard, _ := settings.Value("adguard").(*jsonx.Object)
	var urls any
	if adguard != nil {
		urls = adguard.Value("urls")
	}
	for _, value := range asList(urls) {
		url, ok := value.(string)
		if !ok {
			continue
		}
		localPath := "./rules/" + TagFor("adguard", url) + ".txt"
		if referenced[resourceKey(relativeResource(localPath))] {
			dir, name := purePosixParentName(localPath)
			desired = append(desired, jsonx.Obj("url", url, "dir", dir, "filename", name))
		}
	}
	return desired
}

// ResourceReaders is resource_readers(document): conservative resource
// consumers, including unknown plugin fields.
func ResourceReaders(document *jsonx.Object) (readers []string, err error) {
	defer catch(&err)
	return resourceReaders(document), nil
}

func resourceReaders(document *jsonx.Object) []string {
	out := []string{}
	var walk func(value any)
	walk = func(value any) {
		switch x := value.(type) {
		case string:
			if strings.Contains(x, "/") || strings.Contains(x, "${") || strings.ContainsAny(x, "*?[") {
				out = append(out, x)
			}
		case *jsonx.Object:
			for _, element := range x.Values() {
				walk(element)
			}
		case []any:
			for _, element := range x {
				walk(element)
			}
		}
	}
	root := withoutKey(document, "plugins")
	list, present := document.Get("plugins")
	if !present {
		list = []any{}
	}
	plugins := []any{}
	for _, plugin := range pyIter(list) {
		if get(plugin, "type", nil) != "download" {
			plugins = append(plugins, plugin)
		}
	}
	root.Set("plugins", plugins)
	walk(root)
	return out
}

// ResourceIsRead is resource_is_read(path, readers).
func ResourceIsRead(path string, readers []string) bool {
	for _, reference := range readers {
		if strings.Contains(reference, "${") || fnmatchCase(path, strings.TrimPrefix(reference, "./")) ||
			strings.HasSuffix(reference, "/"+path) {
			return true
		}
		// Absolute patterns may refer to the same resource under the target's
		// installation directory, whose prefix is not known while editing.
		if strings.HasPrefix(reference, "/") {
			for index, r := range reference {
				if r == '/' && fnmatchCase(path, reference[index+1:]) {
					return true
				}
			}
		}
	}
	return false
}

// ownHelpers is _own_helpers(document, routes, actions, entries): create,
// keep, refresh or remove the owned helper plugins in place. entries are the
// rule group entries just written, which are not "elsewhere".
func ownHelpers(document *jsonx.Object, routes []string, actions *jsonx.Object, entries []any) {
	plugins := asList(item(document, "plugins"))
	byTag := routingPlugins(document)
	desired := helperPlugins(routes, actions, chainFeatures(byTag))
	wanted := jsonx.NewObject()
	for _, plugin := range desired {
		wanted.Set(plugin.(*jsonx.Object).GetString("tag"), plugin)
	}
	owned := map[*jsonx.Object]bool{}
	for _, entry := range entries {
		owned[entry.(*jsonx.Object)] = true
	}
	outside := []any{}
	for _, element := range plugins {
		plugin := element.(*jsonx.Object)
		if isHelperTag(plugin.Value("tag")) {
			continue
		}
		args, isList := plugin.Value("args").([]any)
		if plugin.Value("tag") == "sequence_main" && isList {
			copied := plugin.Clone()
			kept := []any{}
			for _, entry := range args {
				if object, ok := entry.(*jsonx.Object); !ok || !owned[object] {
					kept = append(kept, entry)
				}
			}
			copied.Set("args", kept)
			outside = append(outside, copied)
		} else {
			outside = append(outside, plugin)
		}
	}
	top := withoutKey(document, "plugins")
	external := func(tag string) bool {
		if references(top, tag) {
			return true
		}
		for _, plugin := range outside {
			if references(plugin, tag) {
				return true
			}
		}
		return false
	}
	replaceable := func(tag string) bool {
		_, key := helperTag(tag)
		if key == "" {
			return false
		}
		action := pinnedAction(key, byTag)
		if action == nil {
			return false
		}
		if routeKey(action) == "upstream:"+key {
			return true
		}
		// Another address under this tag (edited by hand) is replaced only if unused elsewhere.
		for _, prefix := range []string{UpstreamPrefix, ForwardPrefix, CachePrefix} {
			if external(prefix + key) {
				return false
			}
		}
		return true
	}
	for tag, plugin := range wanted.All() {
		if existing, ok := byTag.Get(tag); ok && !sameValue(existing, plugin) && !replaceable(tag) {
			fail("已有同名插件 " + tag + "，无法创建 DNS 处理所需插件，请先在完整参数中改名")
		}
	}
	kept := map[string]bool{}
	for _, tag := range wanted.Keys() {
		kept[tag] = true
	}
	for _, tag := range byTag.Keys() {
		if ok, _ := helperTag(tag); ok && external(tag) {
			kept[tag] = true
		}
	}
	// A kept chain keeps the forward and cache it calls (a closure: the
	// processing order does not matter).
	pendingTags := make([]string, 0, len(kept))
	for tag := range kept {
		pendingTags = append(pendingTags, tag)
	}
	sort.Strings(pendingTags)
	for len(pendingTags) > 0 {
		tag := pendingTags[len(pendingTags)-1]
		pendingTags = pendingTags[:len(pendingTags)-1]
		source, ok := wanted.Get(tag)
		if !ok {
			source = byTag.Value(tag)
		}
		for _, other := range byTag.Keys() {
			if ok, _ := helperTag(other); ok && !kept[other] && references(source, other) {
				kept[other] = true
				pendingTags = append(pendingTags, other)
			}
		}
	}
	result := []any{}
	for _, element := range plugins {
		plugin := element.(*jsonx.Object)
		tag := plugin.Value("tag")
		text, _ := tag.(string)
		if want, ok := wanted.Get(text); ok {
			result = append(result, copyValue(want))
		} else if !isHelperTag(tag) || kept[text] {
			result = append(result, plugin)
		}
	}
	for _, element := range desired {
		plugin := element.(*jsonx.Object)
		tag := plugin.GetString("tag")
		if byTag.Has(tag) {
			continue
		}
		// Before the first helper that uses it, else before the main sequence.
		index := len(result)
		for i, existing := range result {
			if existing.(*jsonx.Object).Value("tag") == "sequence_main" {
				index = i
				break
			}
		}
		for i, existing := range result {
			other, _ := existing.(*jsonx.Object).Value("tag").(string)
			if want, ok := wanted.Get(other); ok && other != tag && references(want, tag) {
				index = i
				break
			}
		}
		result = insertCopy(result, index, copyValue(plugin))
	}
	setPluginsInPlace(document, plugins, result)
}

// OwnedChains is owned_chains(document): {chain tag: action} of the pinned
// upstream chains whose tag still names their content.
func OwnedChains(document *jsonx.Object) (chains *jsonx.Object, err error) {
	defer catch(&err)
	return ownedChainsImpl(document), nil
}

func ownedChainsImpl(document *jsonx.Object) *jsonx.Object {
	plugins := routingPlugins(document)
	result := jsonx.NewObject()
	for _, tag := range plugins.Keys() {
		if !strings.HasPrefix(tag, UpstreamPrefix) || tag == UpstreamPrefix {
			continue
		}
		key := tag[len(UpstreamPrefix):]
		if action := pinnedAction(key, plugins); action != nil && routeKey(action) == "upstream:"+key {
			result.Set(tag, action)
		}
	}
	return result
}

// RefreshRuleHelpers is refresh_rule_helpers(document): rebuild owned
// pinned upstream chains for the YAML's current IP selection, RouterOS,
// cache and TTL features, in place. Chains edited by hand are left alone.
func RefreshRuleHelpers(document *jsonx.Object) (err error) {
	defer catch(&err)
	refreshRuleHelpersImpl(document)
	return nil
}

func refreshRuleHelpersImpl(document *jsonx.Object) {
	actions := ownedChainsImpl(document)
	if actions.Len() == 0 {
		return
	}
	routes := []string{}
	for _, action := range actions.Values() {
		routes = append(routes, routeKey(action))
	}
	ownHelpers(document, routes, actions, nil)
}

// syncUpdates is _sync_updates(document, settings, config, old_groups,
// new_groups, base_url, records, before_records): records are the catalog
// of config, beforeRecords those of the rule space the document was written
// for (nil = None).
func syncUpdates(document, settings, config *jsonx.Object, oldGroups []*recognizedGroup, newTags []string,
	baseURL string, records, beforeRecords *catalogData) {
	plugins := routingPlugins(document)
	download := pluginAt(plugins, "rules_download")
	reload := pluginAt(plugins, "rules_reload")
	if download == nil && reload == nil {
		return
	}
	var parkedReload *jsonx.Object
	if parked, ok := settings.GetOr("_common_disabled", jsonx.NewObject()).(*jsonx.Object); ok && parked != nil {
		parkedReload, _ = parked.Value("rules_reload").(*jsonx.Object)
	}
	_, parkedArgsList := parkedReload.Value("args").([]any)
	reloadParked := reload == nil && parkedReload != nil && parkedReload.Value("tag") == "rules_reload" &&
		parkedReload.Value("type") == "reload_provider" && parkedArgsList
	downloadOK := download != nil && download.Value("type") == "download"
	if downloadOK {
		args, isObj := download.Value("args").(*jsonx.Object)
		_, isList := args.Value("downloads").([]any)
		downloadOK = isObj && args != nil && isList
	}
	reloadOK := reloadParked
	if !reloadParked {
		_, isList := reload.Value("args").([]any)
		reloadOK = reload != nil && reload.Value("type") == "reload_provider" && isList
	}
	if !downloadOK || !reloadOK {
		fail("规则定时更新包含自定义结构，请先在完整参数中调整下载与重载任务。")
	}
	oldTags := map[string]bool{}
	for _, group := range oldGroups {
		oldTags[group.tag] = true
	}
	// Old providers retained for another caller also remain reloadable.
	if reload != nil {
		reload.Set("args", ReloadReferences(asList(reload.Value("args")), oldTags, newTags, plugins))
	}
	// Downloads of rulesets that left the catalog (deleted or disabled) are owned too.
	knownPaths := map[string]bool{}
	for _, catalog := range []*catalogData{records, beforeRecords} {
		if catalog == nil {
			continue
		}
		for _, identity := range catalog.order {
			record := catalog.records[identity]
			if record.suffix != "rulesets" {
				continue
			}
			for part := range record.parts {
				knownPaths[strings.TrimPrefix(rulePath(record.item, part), "./")] = true
			}
		}
	}
	desired := resourceDownloadsImpl(document, settings, config, baseURL, records)
	entryPath := func(entry any) (string, bool) {
		object, ok := entry.(*jsonx.Object)
		if !ok || object == nil {
			return "", false
		}
		dir, dirOK := object.Value("dir").(string)
		name, nameOK := object.Value("filename").(string)
		if !dirOK || !nameOK {
			return "", false
		}
		return strings.TrimPrefix(strings.TrimRight(dir, "/")+"/"+name, "./"), true
	}
	desiredEntries := jsonx.NewObject()
	for _, entry := range desired {
		if local, ok := entryPath(entry); ok && knownPaths[local] {
			desiredEntries.Set(local, entry)
		}
	}
	output := []any{}
	readers := resourceReaders(document)
	for _, entry := range download.GetObject("args").GetList("downloads") {
		location, ok := entryPath(entry)
		switch {
		case !ok || !knownPaths[location]:
			output = append(output, entry)
		case desiredEntries.Has(location):
			// Preserve per-download extensions and explicit URL overrides.
			output = append(output, entry)
			desiredEntries.Delete(location)
		case ResourceIsRead(location, readers):
			output = append(output, entry)
		}
	}
	output = append(output, desiredEntries.Values()...)
	replaceDownloadEntries(download, output)
}

// routeLabel is _route_label(route).
func routeLabel(route string) string {
	name := strings.SplitN(route, ":", 2)[0]
	label, ok := RouteLabels[name]
	if !ok {
		raise(pyErr("KeyError", jsonx.Repr(name)))
	}
	return label
}

// ruleName is _name(item).
func ruleName(entry *jsonx.Object) any {
	for _, key := range []string{"name", "value"} {
		if value := entry.Value(key); truthy(value) {
			return value
		}
	}
	return entry.Value("id")
}

// assignRoute is _assign(values, item, action): route one active rule by a
// stored choice; direct/proxy live in the lists.
func assignRoute(values *jsonx.Object, entry *jsonx.Object, action any) {
	normalized := normalizeAction(action, true)
	name := normalized.GetString("action")
	identity := item(entry, "id")
	if strIn(name, ListRoutes...) {
		key := name + "_" + itemSuffix(entry)
		values.Set(key, appendCopy(values.GetList(key), identity))
	} else {
		values.GetObject("rule_actions").Set(identity.(string), normalized)
	}
}

// validateSelections is _validate_selections(values, changes, known): shape
// and existence of changed selections.
func validateSelections(values, changes, known *jsonx.Object) {
	allStrings := func(value any) bool {
		list, ok := value.([]any)
		if !ok {
			return false
		}
		for _, element := range list {
			if !isStr(element) {
				return false
			}
		}
		return true
	}
	for _, key := range Selectors {
		if !allStrings(values.Value(key)) {
			fail(key + " 必须为规则 ID 列表")
		}
		list := dictFromKeys(values.GetList(key))
		values.Set(key, list)
		suffix := "rules"
		if strings.HasSuffix(key, "rulesets") {
			suffix = "rulesets"
		}
		if changes.Has(key) {
			for _, identity := range list {
				entry, found := known.Get(identity.(string))
				if !found || itemSuffix(entry) != suffix {
					fail(missingRule)
				}
			}
		}
	}
	if changes.Has("rule_actions") {
		actions, ok := values.Value("rule_actions").(*jsonx.Object)
		if !ok || actions == nil {
			fail("rule_actions 必须为以规则 ID 为键的对象")
		}
		for _, identity := range actions.Keys() {
			if !known.Has(identity) {
				fail(missingRule)
			}
		}
		normalized := jsonx.NewObject()
		for identity, action := range actions.All() {
			if action != nil {
				normalized.Set(identity, normalizeAction(action, false))
			}
		}
		values.Set("rule_actions", normalized)
	}
	if changes.Has("rule_dns_follow") {
		if !allStrings(values.Value("rule_dns_follow")) {
			fail("rule_dns_follow 必须为规则 ID 数组")
		}
		follow := values.GetList("rule_dns_follow")
		for _, identity := range follow {
			if !known.Has(identity.(string)) {
				fail(missingRule)
			}
		}
		values.Set("rule_dns_follow", dictFromKeys(follow))
	}
	for _, suffix := range []string{"rules", "rulesets"} {
		direct := map[string]bool{}
		for _, identity := range values.GetList("direct_" + suffix) {
			direct[identity.(string)] = true
		}
		for _, identity := range values.GetList("proxy_" + suffix) {
			if direct[identity.(string)] {
				fail("同一规则不能同时选择直连和代理。")
			}
		}
	}
}

// takeOver is _take_over(values, current, changes, known, notices): a
// changed key wins over the other kind of choice it newly takes a rule from.
func takeOver(values *jsonx.Object, current *routingState, changes, known *jsonx.Object, notices *[]any) {
	old := current.values
	type taking struct{ identity, before, after string }
	var taken []taking
	if changes.Has("rule_actions") {
		for identity, action := range values.GetObject("rule_actions").All() {
			if old.GetObject("rule_actions").Has(identity) {
				continue
			}
			for _, key := range Selectors {
				if pyContains(values.Value(key), identity) {
					if changes.Has(key) {
						fail("同一规则不能同时选择多种 DNS 处理。")
					}
					values.Set(key, listRemove(values.Value(key), identity))
					taken = append(taken, taking{identity, strings.SplitN(key, "_", 2)[0], routeKey(action)})
				}
			}
		}
	}
	for _, key := range Selectors {
		if !changes.Has(key) {
			continue
		}
		for _, element := range values.GetList(key) {
			identity := element.(string)
			actions := values.GetObject("rule_actions")
			if pyContains(old.Value(key), identity) || !actions.Has(identity) {
				continue
			}
			if changes.Has("rule_actions") {
				fail("同一规则不能同时选择多种 DNS 处理。")
			}
			action := actions.Value(identity)
			actions.Delete(identity)
			taken = append(taken, taking{identity, routeKey(action), strings.SplitN(key, "_", 2)[0]})
		}
	}
	if notices != nil {
		for _, t := range taken {
			*notices = append(*notices, "规则「"+str(ruleName(known.GetObject(t.identity)))+"」的 DNS 处理已从「"+
				routeLabel(t.before)+"」改为「"+routeLabel(t.after)+"」")
		}
	}
}

// settle is _settle(values, settings, changes, current, known, active,
// routes): keep only active rules in the selections; return (parked,
// follow). Follow rules are never parked: the policy decides again.
func settle(values, settings, changes *jsonx.Object, current *routingState, known *jsonx.Object,
	active *catalogData, routes *jsonx.Object) (*jsonx.Object, []any) {
	followGiven := changes.Has("rule_dns_follow")
	follow := []any{}
	for _, identity := range values.GetList("rule_dns_follow") {
		if known.Has(identity.(string)) {
			follow = append(follow, identity)
		}
	}
	parked := jsonx.NewObject()
	if stored, ok := settings.Value("rule_dns_parked").(*jsonx.Object); ok && stored != nil {
		for identity, action := range stored.All() {
			if known.Has(identity) {
				parked.Set(identity, copyValue(action))
			}
		}
	}
	assigned := map[string]bool{}
	for _, key := range Selectors {
		if changes.Has(key) {
			for _, identity := range values.GetList(key) {
				assigned[identity.(string)] = true
			}
		}
	}
	if changes.Has("rule_actions") {
		for _, identity := range values.GetObject("rule_actions").Keys() {
			assigned[identity] = true
		}
	}
	leaving := jsonx.NewObject()
	for _, key := range Selectors {
		kept := []any{}
		for _, element := range values.GetList(key) {
			identity := element.(string)
			if !active.has(identity) {
				leaving.Set(identity, jsonx.Obj("action", strings.SplitN(key, "_", 2)[0]))
			} else {
				kept = append(kept, identity)
			}
		}
		values.Set(key, kept)
	}
	keptActions := jsonx.NewObject()
	for identity, action := range values.GetObject("rule_actions").All() {
		if !active.has(identity) {
			leaving.Set(identity, action)
		} else {
			keptActions.Set(identity, action)
		}
	}
	values.Set("rule_actions", keptActions)
	for identity, action := range leaving.All() {
		if known.Has(identity) && (!listContains(follow, identity) || assigned[identity]) {
			parked.Set(identity, action)
			if listContains(follow, identity) && !followGiven {
				follow = listRemove(follow, identity)
			}
		}
	}
	if !current.full {
		raise(pyErr("KeyError", "'records'"))
	}
	before := current.records
	for _, identity := range active.order {
		record := active.records[identity]
		if routeOf(values, identity) != nil {
			parked.Delete(identity)
		} else if parked.Has(identity) {
			action := parked.Value(identity)
			parked.Delete(identity)
			assignRoute(values, record.item, action)
		} else if listContains(follow, identity) && !before.has(identity) {
			if route := followRoute(record.item, routes); route != nil {
				assignRoute(values, record.item, jsonx.Obj("action", route))
			}
		}
	}
	if !followGiven {
		// Follow intent ends for a rule active before and after whose actual
		// route this write changed, and for a rule with an explicit action.
		kept := []any{}
		for _, element := range follow {
			identity := element.(string)
			if values.GetObject("rule_actions").Has(identity) {
				continue
			}
			if before.has(identity) && active.has(identity) &&
				!jsonx.Equal(routeOf(current.values, identity), routeOf(values, identity)) {
				continue
			}
			kept = append(kept, identity)
		}
		follow = kept
	}
	for _, element := range follow {
		identity := element.(string)
		if action, ok := values.GetObject("rule_actions").Get(identity); ok {
			label := RouteLabels[str(item(action, "action"))]
			fail("规则 " + identity + " 已设置 DNS 处理「" + label + "」，不能同时跟随策略")
		}
		if parked.Has(identity) {
			fail("规则 " + identity + " 已暂存停用前的 DNS 处理，不能同时跟随策略")
		}
	}
	return parked, follow
}

// normalizeCustomMatches is _normalize_custom_matches(value).
func normalizeCustomMatches(value any) []any {
	list, ok := value.([]any)
	if !ok {
		fail("自定义匹配必须为列表。")
	}
	normalized := []any{}
	for _, element := range list {
		entry, ok := element.(*jsonx.Object)
		if !ok || entry == nil {
			fail("每条自定义匹配必须为对象。")
		}
		if entry.Value("enabled") == false {
			fail("常用设置直接编辑执行中的匹配条目，不支持 enabled: false；请移除该条目或改为启用。")
		}
		matches, ok := routingMatches(entry).([]any)
		valid := ok && len(matches) > 0
		for _, match := range matches {
			if text, isText := match.(string); !isText || pystr.Strip(text) == "" {
				valid = false
			}
		}
		exec, isText := entry.Value("exec").(string)
		if !valid || !isText || pystr.Strip(exec) == "" {
			fail("自定义匹配需填写非空条件与执行动作。")
		}
		copied := jsonx.NewObject()
		for key, value := range entry.All() {
			if key != "enabled" {
				copied.Set(key, copyValue(value))
			}
		}
		normalized = append(normalized, copied)
	}
	return normalized
}

// applyAliasedCustom is _apply_aliased_custom(document, settings, state,
// changes).
func applyAliasedCustom(document, settings *jsonx.Object, state *routingState, changes *jsonx.Object) (*jsonx.Object, *jsonx.Object) {
	for _, field := range changes.Keys() {
		if !capEditable(state.caps, field) {
			fail(capReason(state.caps, field))
		}
	}
	position, ok := changes.Get("custom_match_position")
	if !ok {
		position = state.values.Value("custom_match_position")
		if !truthy(position) {
			position = "tail"
		}
	}
	if !strIn(position, "head", "tail") {
		fail("自定义匹配位置必须为 head 或 tail。")
	}
	var custom []any
	if changes.Has("custom_matches") {
		custom = normalizeCustomMatches(changes.Value("custom_matches"))
	} else {
		custom = asList(copyValue(state.values.Value("custom_matches")))
	}
	result := copyValue(document).(*jsonx.Object)
	main := pluginAt(routingPlugins(result), state.actualMainTag)
	if main == nil {
		raise(pyErr("KeyError", jsonx.Repr(state.actualMainTag)))
	}
	args := asList(item(main, "args"))
	if !state.full {
		raise(pyErr("KeyError", "'prefix'"))
	}
	prefixLen := min(len(state.prefix), len(args))
	middleEnd := min(len(state.prefix)+len(state.customIndices)+len(state.groups["domain"]), len(args))
	// Indexes come from a read-only recognition copy. All retained entries
	// come from the original document.
	domain := []any{}
	for _, group := range state.groups["domain"] {
		domain = append(domain, listItem(args, group.index))
	}
	updated := append([]any{}, args[:prefixLen]...)
	if position == "head" {
		updated = append(updated, custom...)
	}
	updated = append(updated, domain...)
	if position == "tail" {
		updated = append(updated, custom...)
	}
	main.Set("args", append(updated, args[middleEnd:]...))
	return result, copyValue(settings).(*jsonx.Object)
}

// ApplyRoutingOptions are apply_routing's keyword arguments.
type ApplyRoutingOptions struct {
	// BeforeConfig is before_config: the rule space the document was
	// written for (nil = None). Passing it rebuilds the owned routing for
	// config even without changes.
	BeforeConfig *jsonx.Object
	// Notices is notices (nil = None): receives a line for each rule a
	// changed key took from another kind of DNS choice.
	Notices *[]any
}

// ApplyRouting is apply_routing(document, settings, config, changes,
// base_url, before_config=..., notices=...): one atomic routing edit applied
// to copies of document and settings; never persists or regenerates all
// YAML.
func ApplyRouting(document, settings, config, changes *jsonx.Object, baseURL string, options ApplyRoutingOptions) (doc, values *jsonx.Object, err error) {
	defer catch(&err)
	doc, values = applyRoutingImpl(document, settings, config, changes, baseURL, options.BeforeConfig,
		options.BeforeConfig != nil, options.Notices)
	return doc, values, nil
}

func applyRoutingImpl(document, settings *jsonx.Object, configValue any, changesValue *jsonx.Object, baseURL string,
	beforeConfig *jsonx.Object, rebuild bool, notices *[]any) (*jsonx.Object, *jsonx.Object) {
	changes := jsonx.NewObject()
	for key, value := range changesValue.All() {
		if RoutingFields[key] {
			changes.Set(key, value)
		}
	}
	if changes.Len() == 0 && !rebuild {
		return copyValue(document).(*jsonx.Object), copyValue(settings).(*jsonx.Object)
	}
	config := configOf(configValue)
	onlyCustom, anyCustom := true, false
	for _, key := range changes.Keys() {
		if strIn(key, customFields...) {
			anyCustom = true
		} else {
			onlyCustom = false
		}
	}
	if !rebuild && anyCustom {
		if aliased := aliasedCustomState(document, settings, config); aliased != nil {
			if onlyCustom {
				return applyAliasedCustom(document, settings, aliased, changes)
			}
			fail("执行链已重命名，请单独调整自定义匹配，再编辑其他分流设置。")
		}
	}
	analyzed := config
	if rebuild {
		analyzed = beforeConfig
	}
	current := analyzeRouting(document, settings, analyzed)
	for _, key := range changes.Keys() {
		if !capEditable(current.caps, key) {
			fail(capReason(current.caps, key))
		}
	}
	if rebuild && !capEditable(current.caps, "direct_rules") {
		fail(capReason(current.caps, "direct_rules"))
	}
	values := copyValue(current.values).(*jsonx.Object)
	for key, value := range copyValue(changes).(*jsonx.Object).All() {
		values.Set(key, value)
	}
	if !strIn(values.Value("default_forward"), "forward_local", "forward_remote") {
		fail("默认转发目标无效。")
	}
	migrateIP := current.full && len(current.groups["ip"]) > 0 && capEditable(current.caps, "direct_rules")
	onlyDefault := true
	for _, key := range changes.Keys() {
		if key != "default_forward" {
			onlyDefault = false
		}
	}
	if !rebuild && !migrateIP && onlyDefault {
		return applyDefaultForward(document, settings, current, values)
	}
	known := knownRules(config)
	// current was read against before_config when rebuilding.
	var records *catalogData
	if rebuild {
		records = buildCatalog(config)
	} else {
		if !current.full {
			raise(pyErr("KeyError", "'records'"))
		}
		records = current.records
	}
	active := &catalogData{records: map[string]*catalogRecord{}}
	for _, identity := range records.order {
		if record := records.records[identity]; Convertible(record.item) {
			active.order = append(active.order, identity)
			active.records[identity] = record
		}
	}
	validateSelections(values, changes, known)
	takeOver(values, current, changes, known, notices)
	routes := policyDNSRoutes(config)
	parked, follow := settle(values, settings, changes, current, known, active, routes)
	if changes.Has("aggregate_rules") {
		if _, isBool := values.Value("aggregate_rules").(bool); !isBool {
			fail("合并相邻规则必须为布尔值。")
		}
	}
	if changes.Has("custom_match_position") && !strIn(values.Value("custom_match_position"), "head", "tail") {
		fail("自定义匹配执行顺序必须为 head 或 tail。")
	}
	if changes.Has("custom_matches") {
		values.Set("custom_matches", normalizeCustomMatches(values.Value("custom_matches")))
	}
	result, resultSettings := copyValue(document).(*jsonx.Object), copyValue(settings).(*jsonx.Object)
	main := pluginAt(routingPlugins(result), "sequence_main")
	if main == nil {
		raise(pyErr("KeyError", "'sequence_main'"))
	}
	routingChanged := rebuild || migrateIP
	for _, key := range append(append([]string{}, Selectors...), "aggregate_rules", "rule_actions") {
		if changes.Has(key) {
			routingChanged = true
		}
	}
	type outGroup struct {
		tag   string
		route any // a string; an invalidRoute only on unchanged groups
		entry *jsonx.Object
	}
	groups := map[string][]outGroup{}
	var newTags []string
	if routingChanged {
		// There is no actual aggregate setting until adjacent same-route
		// rules exist. New selections use the existing grouped representation,
		// or the compact equivalent when the document could mean either.
		if values.Value("aggregate_rules") == nil {
			values.Set("aggregate_rules", true)
		}
		expected := routingGroups(config, values, LegacyNone, nil, false)
		old := current.allGroups()
		outside := newMentions([]any{withoutKey(document, "plugins"), values.Value("custom_matches")})
		resultPlugins := asList(result.Value("plugins"))
		inside := newMentions(resultPlugins)
		external := map[string]bool{}
		for _, group := range old {
			isExternal := !onlyKeys(group.provider, "tag", "type", "args") || truthy(document.Value("include")) ||
				len(outside.positions(group.tag)) > 0
			for _, position := range inside.positions(group.tag) {
				if !strIn(inside.values[position].(*jsonx.Object).Value("tag"), "sequence_main", "rules_reload", group.tag) {
					isExternal = true
				}
			}
			if isExternal {
				external[group.tag] = true
			}
		}
		olds := []oldGroup{}
		for _, group := range old {
			olds = append(olds, oldGroup{group.tag, group.provider.Value("type"), group.args})
		}
		placed, placedGroups := placeRoutingGroups(resultPlugins, olds, expected, external)
		result.Set("plugins", placed)
		for _, part := range []string{"domain", "ip"} {
			for _, element := range placedGroups.GetList(part) {
				group := element.(*jsonx.Object)
				tag := group.GetString("tag")
				groups[part] = append(groups[part], outGroup{tag: tag, route: group.Value("route")})
				newTags = append(newTags, tag)
			}
		}
		for _, key := range Selectors {
			updated := []any{}
			if list, isList := resultSettings.Value(key).([]any); isList {
				updated = asList(copyValue(list))
			}
			for _, identity := range values.GetList(key) {
				if !listContains(updated, identity) {
					updated = append(updated, identity)
				}
			}
			resultSettings.Set(key, updated)
		}
		// Unlike the retained list catalog, actions map exactly the active rules.
		resultSettings.Set("rule_actions", copyValue(values.Value("rule_actions")))
		resultSettings.Set("rule_dns_parked", parked)
	} else {
		for _, part := range []string{"domain", "ip"} {
			for _, group := range current.groups[part] {
				groups[part] = append(groups[part], outGroup{tag: group.tag, route: group.route, entry: group.entry})
			}
		}
	}
	if routingChanged || changes.Has("rule_dns_follow") {
		resultSettings.Set("rule_dns_follow", follow)
	}
	// Build only the identified sequence regions, retaining all surrounding nodes.
	prefix := asList(copyValue(listOrEmpty(current.prefix)))
	custom := asList(copyValue(values.Value("custom_matches")))
	position := values.Value("custom_match_position")
	if !truthy(position) {
		position = "tail"
	}
	build := func(part, verb string) []any {
		out := []any{}
		for _, group := range groups[part] {
			if !routingChanged {
				out = append(out, copyValue(group.entry))
			} else {
				out = append(out, jsonx.Obj("matches", []any{verb + " $" + group.tag}, "exec", routeExec(group.route, part)))
			}
		}
		return out
	}
	domain := build("domain", "qname")
	ip := build("ip", "resp_ip")
	final := copyValue(current.final).(*jsonx.Object)
	if values.Value("default_forward") == "forward_local" {
		final.Set("exec", "goto china_dns")
	} else {
		final.Set("exec", "goto proxy_dns_seq")
	}
	suffix := asList(copyValue(listOrEmpty(current.dynamic)))
	var ipRoutes []any
	for _, group := range groups["ip"] {
		ipRoutes = append(ipRoutes, group.route)
	}
	probe := probeNeeded(ipRoutes, values.Value("default_forward"))
	suffix = append(suffix, ipSuffix(ip, values.Value("default_forward"), probe)...)
	// Do not create a new jump into an absent helper sequence in a partial
	// native document. Existing unrelated custom references remain untouched.
	var affected []any
	if changes.Has("default_forward") {
		affected = append(affected, final)
	}
	if routingChanged {
		affected = append(append(affected, domain...), ip...)
	}
	if len(ip) > 0 && probe && (routingChanged || changes.Has("default_forward")) {
		affected = append(affected, jsonx.Obj("exec", "jump local_probe"))
		if values.Value("default_forward") == "forward_local" {
			affected = append(affected, jsonx.Obj("exec", "goto default_direct_ip"))
		}
	}
	available := routingPlugins(result)
	for _, entry := range affected {
		verb, target, _ := strings.Cut(entry.(*jsonx.Object).GetString("exec"), " ")
		// Rejections need no target; owned helpers are created below.
		if (verb != "goto" && verb != "jump") || strings.HasPrefix(target, UpstreamPrefix) {
			continue
		}
		if plugin := pluginAt(available, target); plugin == nil || plugin.Value("type") != "sequence" {
			fail("分流执行链 " + target + " 未配置，请在完整参数中添加后再调整。")
		}
	}
	args := append([]any{}, prefix...)
	if position == "head" {
		args = append(args, custom...)
	}
	args = append(args, domain...)
	if position == "tail" {
		args = append(args, custom...)
	}
	main.Set("args", append(append(args, suffix...), final))
	if routingChanged {
		routes := []string{}
		for _, part := range []string{"domain", "ip"} {
			for _, group := range groups[part] {
				routes = append(routes, str(group.route))
			}
		}
		ownHelpers(result, routes, values.GetObject("rule_actions"), append(append([]any{}, domain...), ip...))
		var before *catalogData
		if rebuild {
			before = current.records
		}
		syncUpdates(result, resultSettings, config, current.allGroups(), newTags, baseURL, records, before)
	}
	return result, resultSettings
}

// applyDefaultForward is apply_routing's path for a default_forward-only
// change without IP migration: only the final fallback and the IP block are
// rewritten, so an unrecognized custom middle stays untouched.
func applyDefaultForward(document, settings *jsonx.Object, current *routingState, values *jsonx.Object) (*jsonx.Object, *jsonx.Object) {
	result := copyValue(document).(*jsonx.Object)
	main := pluginAt(routingPlugins(result), "sequence_main")
	if main == nil {
		raise(pyErr("KeyError", "'sequence_main'"))
	}
	args := asList(item(main, "args"))
	direct := values.Value("default_forward") == "forward_local"
	target := "proxy_dns_seq"
	if direct {
		target = "china_dns"
	}
	if plugin := pluginAt(routingPlugins(result), target); plugin == nil || plugin.Value("type") != "sequence" {
		fail("目标分流执行链未配置，请在完整参数中添加后再切换。")
	}
	args[len(args)-1].(*jsonx.Object).Set("exec", "goto "+target)
	if !current.full {
		raise(pyErr("KeyError", "'groups'"))
	}
	ipGroups := current.groups["ip"]
	if len(ipGroups) > 0 {
		first := ipGroups[0].index
		var routes, entries []any
		for _, group := range ipGroups {
			first = min(first, group.index)
			routes = append(routes, group.route)
		}
		start := first
		if first > 0 && jsonx.Equal(args[first-1], jsonx.Obj("exec", "jump local_probe")) {
			start = first - 1
		}
		probe := probeNeeded(routes, values.Value("default_forward"))
		if probe {
			helpers := []string{"local_probe"}
			if direct {
				helpers = append(helpers, "default_direct_ip")
			}
			for _, helper := range helpers {
				if plugin := pluginAt(routingPlugins(result), helper); plugin == nil || plugin.Value("type") != "sequence" {
					fail("分流执行链 " + helper + " 未配置，请在完整参数中添加后再调整。")
				}
			}
		}
		for _, group := range ipGroups {
			entries = append(entries, args[group.index])
		}
		replacement := ipSuffix(entries, values.Value("default_forward"), probe)
		updated := append(append(append([]any{}, args[:start]...), replacement...), args[len(args)-1])
		main.Set("args", updated)
		relinkList(result, args, updated, map[unsafe.Pointer]bool{})
	}
	return result, copyValue(settings).(*jsonx.Object)
}
