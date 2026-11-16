package convert

// The rest of backend/utils/dialer_references.py (incoming_dialers,
// resolve_dialer, overlay_dialer, filter_provider_candidates,
// provider_proxy_name, validate_emitted, reject_surge_dialers,
// dependency_node_ids) and the exported entry points.

import (
	"context"
	"errors"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/rules"
	"configflow/internal/subs"
)

// IDValue is the id as Python holds it: ids that are not strings are kept
// in Ref.ID as "\x00" + repr(id) (so that 1, 1.0 and True collide as in a
// dict); IDValue turns numbers and None back into values (a bool id comes
// back as 1 or 0).
func (r Ref) IDValue() any {
	text, found := strings.CutPrefix(r.ID, "\x00")
	if !found {
		return r.ID
	}
	if text == "None" {
		return nil
	}
	if n, err := jsonx.ParseNumber(text); err == nil {
		return n
	}
	return text
}

// Keys lists the graph's keys in insertion order (dict order).
func (g *Graph) Keys() []Ref { return append([]Ref(nil), g.keys...) }

// Children returns graph[key]; ok is false when key is not in the graph.
func (g *Graph) Children(key Ref) ([]Ref, bool) {
	children, ok := g.children[key]
	return children, ok
}

// Has is `key in graph`.
func (g *Graph) Has(key Ref) bool { return g.has(key) }

// validateProxyChains is proxy_chains.validate_proxy_chains (owned by C11,
// subs.ValidateProxyChains). Python's first loop runs `group.get(
// 'follow_group') in chains` over the groups by id, which raises TypeError
// for an unhashable follow_group; subs does not check that, so the loop is
// walked here up to the first group that raises either way. Callers have
// passed validate_shapes (ids are strings).
func validateProxyChains(profile *jsonx.Object) error {
	list, _ := profile.GetOr("proxy_groups", []any{}).([]any)
	groups := newObjectMap()
	for _, item := range list {
		if group, ok := item.(*jsonx.Object); ok && group != nil {
			groups.set(group.GetString("id"), group)
		}
	}
	chains := map[string]bool{}
	for _, id := range groups.keys {
		if jsonx.Equal(groups.values[id].Value("type"), "chain") {
			chains[id] = true
		}
	}
walk:
	for _, id := range groups.keys {
		group := groups.values[id]
		follow := group.Value("follow_group")
		if err := hashable(follow); err != nil {
			return err
		}
		if text, ok := follow.(string); ok && chains[text] {
			break
		}
		if len(chains) > 0 && compatEnabled(group) && !jsonx.Equal(group.Value("type"), "chain") {
			members, err := groupMembers(group)
			if err != nil {
				break
			}
			for _, member := range members {
				if member.Kind != "group" {
					continue
				}
				target := groups.values[member.ID]
				if target == nil || !compatEnabled(target) {
					break walk
				}
			}
		}
	}
	return fromSubs(subs.ValidateProxyChains(profile))
}

// ValidateShapes is validate_shapes(profile, require_ids=...): metadata is
// checked before attribute access, hashing or string parsing.
func ValidateShapes(profile *jsonx.Object, requireIDs bool) error {
	return validateShapes(profile, requireIDs)
}

// ValidateDialers is validate_dialers.
func ValidateDialers(profile *jsonx.Object) error { return validateDialers(profile) }

// DependencyGraph is dependency_graph.
func DependencyGraph(profile *jsonx.Object) (*Graph, error) { return dependencyGraph(profile) }

// RawDialer is raw_dialer: the dialer-proxy a node names itself (from a
// structured proxy string or its params), never asking Sub-Store.
func RawDialer(node *jsonx.Object) (any, error) { return rawDialer(node) }

// DialerTarget is dialer_target; nil is None.
func DialerTarget(profile, node *jsonx.Object) (*Ref, error) { return dialerTarget(profile, node) }

// GroupMembers is group_members.
func GroupMembers(group *jsonx.Object) ([]Ref, error) { return groupMembers(group) }

// GroupDependencies is group_dependencies.
func GroupDependencies(profile, group *jsonx.Object) ([]Ref, error) {
	return groupDependencies(profile, group)
}

// CheckCycles is check_cycles.
func CheckCycles(graph *Graph) error { return checkCycles(graph) }

// pyContains is `needle in haystack` for the containers a profile holds.
func pyContains(haystack, needle any) (bool, error) {
	switch value := haystack.(type) {
	case []any:
		for _, item := range value {
			if jsonx.Equal(item, needle) {
				return true, nil
			}
		}
		return false, nil
	case string:
		text, ok := needle.(string)
		if !ok {
			return false, pyError("TypeError", "'in <string>' requires string as left operand, not "+pyTypeName(needle))
		}
		return strings.Contains(value, text), nil
	case *jsonx.Object:
		if err := hashable(needle); err != nil {
			return false, err
		}
		text, ok := needle.(string)
		return ok && value.Has(text), nil
	}
	return false, pyError("TypeError", "argument of type '"+pyTypeName(haystack)+"' is not iterable")
}

func refObject(kind, id string) *jsonx.Object { return jsonx.Obj("type", kind, "id", id) }

// IncomingDialers is incoming_dialers: logical dependents of (kind, id),
// including inactive metadata retained by users, without duplicates.
func IncomingDialers(profile *jsonx.Object, kind, id string) ([]Ref, error) {
	target := Ref{kind, id}
	want := refObject(kind, id)
	nodes, err := profileList(profile, "nodes")
	if err != nil {
		return nil, err
	}
	// Ids are hashed only by dict.fromkeys at the end, after
	// dependency_graph has validated the shapes.
	type rawRef struct {
		kind string
		id   any
	}
	var direct []rawRef
	for _, node := range nodes {
		if !jsonx.Equal(node.Value("dialer_ref"), want) {
			continue
		}
		value, err := compatItem(node, "id")
		if err != nil {
			return nil, err
		}
		direct = append(direct, rawRef{"node", value})
	}
	groups, err := profileList(profile, "proxy_groups")
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		var matches bool
		if jsonx.Equal(group.Value("type"), "chain") {
			value, err := compatItem(group, "chain")
			if err != nil {
				return nil, err
			}
			chain, ok := value.(*jsonx.Object)
			if !ok {
				return nil, attributeError(value, "values")
			}
			for _, ref := range chain.Values() {
				if jsonx.Equal(ref, want) {
					matches = true
					break
				}
			}
		} else {
			members, err := groupMembers(group)
			if err != nil {
				return nil, err
			}
			for _, member := range members {
				if member == target {
					matches = true
					break
				}
			}
			if !matches && kind == "group" {
				if matches, err = pyContains(group.GetOr("include_groups", []any{}), id); err != nil {
					return nil, err
				}
				if !matches {
					matches = jsonx.Equal(group.Value("follow_group"), id)
				}
			}
		}
		if matches {
			value, err := compatItem(group, "id")
			if err != nil {
				return nil, err
			}
			direct = append(direct, rawRef{"group", value})
		}
	}
	graph, err := dependencyGraph(profile)
	if err != nil {
		return nil, err
	}
	var keys []Ref
	for _, raw := range direct {
		key, err := hashID(raw.id)
		if err != nil {
			return nil, err
		}
		keys = append(keys, Ref{raw.kind, key})
	}
	for _, key := range graph.keys {
		for _, child := range graph.children[key] {
			if child == target {
				keys = append(keys, key)
				break
			}
		}
	}
	seen := map[Ref]bool{}
	result := []Ref{}
	for _, key := range keys {
		if !seen[key] {
			seen[key] = true
			result = append(result, key)
		}
	}
	return result, nil
}

// ResolveDialer is resolve_dialer: the name of the node's dialer_ref target
// (nil when the node has none). A dangling reference is Python's
// StopIteration; ref['id'] is read per candidate, as the generator does.
func ResolveDialer(profile, node *jsonx.Object) (any, error) {
	ref := node.Value("dialer_ref")
	if ref == nil {
		return nil, nil
	}
	kind, err := compatItem(ref, "type")
	if err != nil {
		return nil, err
	}
	key := "proxy_groups"
	if jsonx.Equal(kind, "node") {
		key = "nodes"
	}
	targets, err := compatList(profile.GetOr(key, []any{}))
	if err != nil {
		return nil, err
	}
	for _, item := range targets {
		target, err := compatObject(item)
		if err != nil {
			return nil, err
		}
		id, err := compatItem(ref, "id")
		if err != nil {
			return nil, err
		}
		if jsonx.Equal(target.Value("id"), id) {
			return compatItem(target, "name")
		}
	}
	return nil, pyError("StopIteration", "")
}

// OverlayDialer is overlay_dialer: put the resolved dialer on a converted
// proxy, refusing to drop a node that takes part in the dialer topology.
func OverlayDialer(profile, node, proxy *jsonx.Object) (*jsonx.Object, error) {
	if proxy == nil || proxy.Len() == 0 {
		graph, err := dependencyGraph(profile)
		if err != nil {
			return nil, err
		}
		id, err := hashID(node.Value("id"))
		if err != nil {
			return nil, err
		}
		if graph.has(Ref{"node", id}) {
			return nil, dialerError("拨号拓扑参与节点转换失败，不能省略节点")
		}
		return proxy, nil
	}
	if node.Value("dialer_ref") != nil {
		if jsonx.Truthy(node.Value("_proxy_chain_id")) {
			name, err := compatItem(node, "name")
			if err != nil {
				return nil, err
			}
			proxy.Set("name", name)
		}
		name, err := ResolveDialer(profile, node)
		if err != nil {
			return nil, err
		}
		proxy.Set("dialer-proxy", name)
	}
	return proxy, nil
}

// splitText is value.split(sep) on a str argument; anything else is the
// AttributeError Python raises.
func splitText(value any, sep string) ([]string, error) {
	text, ok := value.(string)
	if !ok {
		return nil, attributeError(value, "split")
	}
	return strings.Split(text, sep), nil
}

// regexCrash is the Python class of a compile error that is not re.error
// (RecursionError, OverflowError for a repeat count >= MAXREPEAT); "" for
// re.error and patterns only this implementation cannot run.
func regexCrash(err error) string {
	switch err.Error() {
	case "the repetition number is too large":
		return "OverflowError"
	case "maximum recursion depth exceeded":
		return "RecursionError"
	}
	return ""
}

// compileFilter is re.compile(value) inside filter_provider_candidates'
// `except re.error`. A pattern only this implementation cannot run (\N{...},
// repeat counts beyond int32) is refused with the same message (domain
// decision: such patterns are not applied).
func compileFilter(value string) (*rules.PyRegex, error) {
	re, err := rules.CompilePyRegex(value, false)
	if err == nil {
		return re, nil
	}
	if class := regexCrash(err); class != "" {
		return nil, pyError(class, err.Error())
	}
	return nil, dialerError("Provider 正则过滤无效或不受支持")
}

// FilterProviderCandidates is filter_provider_candidates(proxies, include,
// exclude, exclude_type): match original names, preserving Mihomo's
// backtick filter ordering. The arguments are the values Python receives
// (normally strings; "" is the default).
func FilterProviderCandidates(proxies []*jsonx.Object, include, exclude, excludeType any) ([]*jsonx.Object, error) {
	excludeParts, err := splitText(exclude, "`")
	if err != nil {
		return nil, err
	}
	var exclusions, filters []*rules.PyRegex
	for _, value := range excludeParts {
		if value == "" {
			continue
		}
		re, err := compileFilter(value)
		if err != nil {
			return nil, err
		}
		exclusions = append(exclusions, re)
	}
	includeParts, err := splitText(include, "`")
	if err != nil {
		return nil, err
	}
	for _, value := range includeParts {
		re, err := compileFilter(value)
		if err != nil {
			return nil, err
		}
		filters = append(filters, re)
	}
	typeParts, err := splitText(excludeType, "|")
	if err != nil {
		return nil, err
	}
	excludedTypes := map[string]bool{}
	for _, value := range typeParts {
		if value != "" {
			excludedTypes[pystr.Lower(value)] = true
		}
	}
	selected := []*jsonx.Object{}
	seen := map[string]bool{}
	for _, pattern := range filters {
		for _, proxy := range proxies {
			nameValue, err := compatItem(proxy, "name")
			if err != nil {
				return nil, err
			}
			typeValue := proxy.GetOr("type", "")
			typeText, ok := typeValue.(string)
			if !ok {
				return nil, attributeError(typeValue, "lower")
			}
			if excludedTypes[pystr.Lower(typeText)] {
				continue
			}
			key, err := hashID(nameValue)
			if err != nil {
				return nil, err
			}
			if seen[key] {
				continue
			}
			name, ok := nameValue.(string)
			if !ok {
				return nil, pyError("TypeError", "expected string or bytes-like object, got '"+pyTypeName(nameValue)+"'")
			}
			if !pattern.Search(name) {
				continue
			}
			excluded := false
			for _, e := range exclusions {
				if e.Search(name) {
					excluded = true
					break
				}
			}
			if !excluded {
				selected = append(selected, proxy)
				seen[key] = true
			}
		}
	}
	return selected, nil
}

// expandTarget is re.sub(r'\$(\d+)', repl, target) with repl returning
// match.group(int(part[1])) or the empty string.
func expandTarget(target string, groups []string) (string, error) {
	var b strings.Builder
	runes := []rune(target)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '$' {
			b.WriteRune(runes[i])
			continue
		}
		j, number, overflow := i+1, 0, false
		for j < len(runes) {
			d := pystr.Decimal(runes[j])
			if d < 0 {
				break
			}
			if number > 1<<40 {
				overflow = true
			} else {
				number = number*10 + d
			}
			j++
		}
		if j == i+1 {
			b.WriteRune('$')
			continue
		}
		if overflow || number >= len(groups) {
			return "", pyError("IndexError", "no such group")
		}
		b.WriteString(groups[number])
		i = j - 1
	}
	return b.String(), nil
}

// pyFirstItem is value[0] for a truthy value: a list's first item, a str's
// first character, KeyError for a dict, TypeError for anything else.
func pyFirstItem(value any) (any, error) {
	switch v := value.(type) {
	case []any:
		return v[0], nil
	case string:
		for _, r := range v {
			return string(r), nil
		}
	case *jsonx.Object:
		return nil, pyError("KeyError", "0")
	}
	return nil, pyError("TypeError", "'"+pyTypeName(value)+"' object is not subscriptable")
}

// ProviderProxyName is provider_proxy_name: project the native name
// overrides emitted by the chain compiler onto an original provider name.
func ProviderProxyName(proxy, override *jsonx.Object) (string, error) {
	nameValue, err := compatItem(proxy, "name")
	if err != nil {
		return "", err
	}
	rulesValue := override.GetOr("proxy-name", []any{})
	name, isName := nameValue.(string)
	if jsonx.Truthy(rulesValue) {
		first, err := pyFirstItem(rulesValue)
		if err != nil {
			return "", err
		}
		targetValue, err := compatItem(first, "target")
		if err != nil {
			return "", err
		}
		target, isText := targetValue.(string)
		if !isText {
			return "", attributeError(targetValue, "rsplit")
		}
		if !isName {
			return "", attributeError(nameValue, "startswith")
		}
		if strings.HasPrefix(name, pyRsplitFirst(target, "_", 2)+"_") {
			return "", dialerError("Provider 原始节点名称与代理链私有排序标记冲突")
		}
	}
	ruleList, err := compatList(override.GetOr("proxy-name", []any{}))
	if err != nil {
		return "", err
	}
	invalid := dialerError("Provider 名称转换规则无效")
	for _, ruleValue := range ruleList {
		patternValue, err := compatItem(ruleValue, "pattern")
		if err != nil {
			return "", err
		}
		pattern, isText := patternValue.(string)
		if !isText {
			return "", pyError("TypeError", "first argument must be string or compiled pattern")
		}
		// re.sub compiles the pattern before it looks at the string.
		re, err := rules.CompilePyRegex(pattern, false)
		if err != nil {
			if class := regexCrash(err); class != "" {
				return "", pyError(class, err.Error())
			}
			return "", invalid
		}
		if !isName {
			return "", pyError("TypeError", "expected string or bytes-like object, got '"+pyTypeName(nameValue)+"'")
		}
		rule := ruleValue.(*jsonx.Object)
		name, err = re.Sub(name, func(m *rules.Match) (string, error) {
			targetValue, err := compatItem(rule, "target")
			if err != nil {
				return "", err
			}
			target, isText := targetValue.(string)
			if !isText {
				return "", pyError("TypeError", "expected string or bytes-like object, got '"+pyTypeName(targetValue)+"'")
			}
			groups := make([]string, m.Groups()+1)
			for i := range groups {
				groups[i], _ = m.Group(i)
			}
			return expandTarget(target, groups)
		}, 0)
		if err != nil {
			if errors.Is(err, rules.ErrMatchTimeout) || PyErrorClass(err) == "IndexError" {
				return "", invalid
			}
			return "", err
		}
	}
	prefix, suffix := override.GetOr("additional-prefix", ""), override.GetOr("additional-suffix", "")
	prefixText, ok1 := prefix.(string)
	suffixText, ok2 := suffix.(string)
	switch {
	case !ok1:
		return "", pyError("TypeError", "unsupported operand type(s) for +: '"+pyTypeName(prefix)+"' and '"+pyTypeName(nameValue)+"'")
	case !isName:
		return "", pyError("TypeError", "can only concatenate str (not \""+pyTypeName(nameValue)+"\") to str")
	case !ok2:
		return "", pyError("TypeError", "can only concatenate str (not \""+pyTypeName(suffix)+"\") to str")
	}
	return prefixText + name + suffixText, nil
}

// pyRsplitFirst is s.rsplit(sep, maxsplit)[0].
func pyRsplitFirst(s, sep string, maxsplit int) string {
	for ; maxsplit > 0; maxsplit-- {
		i := strings.LastIndex(s, sep)
		if i < 0 {
			break
		}
		s = s[:i]
	}
	return s
}

// EmitOptions are the keyword arguments of validate_emitted.
type EmitOptions struct {
	ForceChains bool
	// ProviderProxies is provider_proxies: {provider name: [proxy, ...]}.
	ProviderProxies  *jsonx.Object
	RequireProviders bool
}

func nameKey(v any) (Ref, error) {
	key, err := hashID(v)
	return Ref{"name", key}, err
}

func objectsOf(v any) ([]*jsonx.Object, error) {
	list, err := compatList(v)
	if err != nil {
		return nil, err
	}
	out := make([]*jsonx.Object, 0, len(list))
	for _, item := range list {
		obj, err := compatObject(item)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, nil
}

var builtinNames = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "PASS-RULE", "COMPATIBLE"}

// ValidateEmitted is validate_emitted: the dialer topology of a generated
// Mihomo document (with provider payloads when known) has no dangling
// names, duplicates, cycles or empty dialer groups. Values are read in
// Python's order, so a malformed provider definition or payload entry raises
// where Python first touches it.
func ValidateEmitted(config *jsonx.Object, opts EmitOptions) error {
	proxiesValue, groupsValue := config.GetOr("proxies", []any{}), config.GetOr("proxy-groups", []any{})
	proxyList, ok1 := proxiesValue.([]any)
	groupList, ok2 := groupsValue.([]any)
	if !ok1 || !ok2 {
		return pyError("TypeError", "can only concatenate list to list")
	}
	all := append(append([]any{}, proxyList...), groupList...)
	definitionsValue := config.GetOr("proxy-providers", jsonx.NewObject())
	definitions, definitionsOK := definitionsValue.(*jsonx.Object)
	if definitions == nil {
		definitionsOK = false
	}
	definitionsError := func(method string) error { return attributeError(definitionsValue, method) }
	payloads := opts.ProviderProxies
	if payloads == nil {
		payloads = jsonx.NewObject()
	}

	active := opts.ForceChains
	for _, item := range all {
		if active {
			break
		}
		obj, err := compatObject(item)
		if err != nil {
			return err
		}
		active = obj.Value("dialer-proxy") != nil
	}
	if !active {
		if !definitionsOK {
			return definitionsError("values")
		}
		for _, value := range definitions.Values() {
			definition, err := compatObject(value)
			if err != nil {
				return err
			}
			override, err := compatObject(definition.GetOr("override", jsonx.NewObject()))
			if err != nil {
				return err
			}
			if active = jsonx.Truthy(override.Value("dialer-proxy")); active {
				break
			}
		}
	}
	if !active {
	payloadScan:
		for _, value := range payloads.Values() {
			list, err := compatList(value)
			if err != nil {
				return err
			}
			for _, item := range list {
				p, err := compatObject(item)
				if err != nil {
					return err
				}
				if p.Value("dialer-proxy") != nil {
					active = true
					break payloadScan
				}
			}
		}
	}
	if !active {
		return nil
	}

	objects, err := objectsOf(all)
	if err != nil {
		return err
	}
	builtins := map[Ref]bool{}
	for _, name := range builtinNames {
		builtins[Ref{"name", name}] = true
	}
	available := map[Ref]bool{}
	invalidNames := false
	for _, obj := range objects {
		key, err := nameKey(obj.Value("name"))
		if err != nil {
			return err
		}
		if available[key] {
			invalidNames = true
		}
		available[key] = true
	}
	for _, obj := range objects {
		name := obj.Value("name")
		key, _ := nameKey(name)
		if !jsonx.Truthy(name) || builtins[key] {
			invalidNames = true
		}
	}
	if invalidNames {
		return dialerError("生成的代理/策略组名称重复或与内置策略冲突")
	}
	for key := range builtins {
		available[key] = true
	}
	if !definitionsOK {
		return definitionsError("items")
	}
	graph := newRefGraph()
	candidates := map[string][]*jsonx.Object{}
	for _, name := range definitions.Keys() {
		definitionValue := definitions.Value(name)
		if !payloads.Has(name) {
			if opts.RequireProviders {
				return dialerError("拨号拓扑缺少已验证的 Provider 内容")
			}
			continue
		}
		payload, err := compatList(payloads.Value(name))
		if err != nil {
			return err
		}
		rawNames := make([]any, 0, len(payload))
		for _, p := range payload {
			value, err := compatItem(p, "name")
			if err != nil {
				return err
			}
			rawNames = append(rawNames, value)
		}
		seenNames := map[Ref]bool{}
		for _, value := range rawNames {
			key, err := nameKey(value)
			if err != nil {
				return err
			}
			if seenNames[key] {
				return dialerError("Provider 节点名称重复")
			}
			seenNames[key] = true
		}
		definition, err := compatObject(definitionValue)
		if err != nil {
			return err
		}
		proxies, err := objectsOf(payload)
		if err != nil {
			return err
		}
		proxies, err = FilterProviderCandidates(proxies, definition.GetOr("filter", ""),
			definition.GetOr("exclude-filter", ""), definition.GetOr("exclude-type", ""))
		if err != nil {
			return err
		}
		if len(proxies) == 0 {
			return dialerError("拨号拓扑 Provider 过滤后不能为空")
		}
		override, err := compatObject(definition.GetOr("override", jsonx.NewObject()))
		if err != nil {
			return err
		}
		candidates[name] = []*jsonx.Object{}
		for _, proxy := range proxies {
			emitted, err := ProviderProxyName(proxy, override)
			if err != nil {
				return err
			}
			key := Ref{"provider", name + "\x00" + emitted}
			if builtins[Ref{"name", emitted}] ||
				(available[Ref{"name", emitted}] && jsonx.Truthy(override.Value("additional-prefix"))) {
				return dialerError("Provider 节点名称与代理拓扑冲突")
			}
			var target any
			if v, ok := override.Get("dialer-proxy"); ok {
				target = v
			} else if v, ok := definition.Get("dialer-proxy"); ok {
				target = v
			} else {
				target = proxy.Value("dialer-proxy")
			}
			var children []Ref
			if target != nil {
				text, isText := target.(string)
				if !isText || !available[Ref{"name", text}] {
					return dialerError("Provider 节点「" + emitted + "」的 dialer-proxy「" + jsonx.Str(target) +
						"」引用了不存在的代理或策略组；若它是机场订阅自带的中转分组，在「订阅来源」刷新该订阅后 ConfigFlow 会自动补上")
				}
				children = []Ref{{"name", text}}
			}
			graph.set(key, children)
			typeValue := proxy.GetOr("type", "")
			candidates[name] = append(candidates[name], jsonx.Obj("name", emitted, "key", key, "type", typeValue))
		}
	}
	groups := map[Ref]*jsonx.Object{}
	var groupOrder []Ref
	groupObjects, _ := objectsOf(groupList)
	for _, g := range groupObjects {
		value, err := compatItem(g, "name")
		if err != nil {
			return err
		}
		key, _ := nameKey(value)
		if _, ok := groups[key]; !ok {
			groupOrder = append(groupOrder, key)
		}
		groups[key] = g
	}
	byName := map[Ref]*jsonx.Object{}
	for _, o := range objects {
		value, err := compatItem(o, "name")
		if err != nil {
			return err
		}
		key, _ := nameKey(value)
		byName[key] = o
	}
	for _, obj := range objects {
		childValue := obj.GetOr("proxies", []any{})
		childList, ok := childValue.([]any)
		if !ok {
			return pyError("TypeError", "'"+pyTypeName(childValue)+"' object is not a list")
		}
		var children []Ref
		var childNames []any
		seen := map[Ref]bool{}
		duplicate := false
		for _, n := range childList {
			key, err := nameKey(n)
			if err != nil {
				return err
			}
			if seen[key] {
				duplicate = true
			}
			seen[key] = true
			children = append(children, key)
			childNames = append(childNames, n)
		}
		if duplicate {
			return dialerError("策略组候选名称重复")
		}
		for _, key := range children {
			if !available[key] {
				return dialerError("拨号拓扑中的策略组引用了未生成的代理或策略组")
			}
		}
		objKey, _ := nameKey(obj.Value("name"))
		if _, isGroup := groups[objKey]; isGroup {
			static := make([]*jsonx.Object, 0, len(childNames))
			for i, n := range childNames {
				if o, ok := byName[children[i]]; ok {
					static = append(static, o)
				} else {
					static = append(static, jsonx.Obj("name", n, "type", n))
				}
			}
			kept, err := FilterProviderCandidates(static, "", obj.GetOr("exclude-filter", ""), obj.GetOr("exclude-type", ""))
			if err != nil {
				return err
			}
			children = children[:0:0]
			for _, p := range kept {
				key, _ := nameKey(p.Value("name"))
				children = append(children, key)
			}
		}
		candidateNames := map[Ref]bool{}
		for _, key := range children {
			candidateNames[key] = true
		}
		if target := obj.Value("dialer-proxy"); target != nil {
			text, isText := target.(string)
			if !isText || !available[Ref{"name", text}] {
				return dialerError("生成的 dialer-proxy 引用了不存在的代理或策略组")
			}
			children = append(children, Ref{"name", text})
		}
		useList, err := compatList(obj.GetOr("use", []any{}))
		if err != nil {
			return err
		}
		for _, providerValue := range useList {
			provider, isText := providerValue.(string)
			if !isText || !definitions.Has(provider) {
				if err := hashable(providerValue); err != nil {
					return err
				}
				return dialerError("策略组引用了未生成的 Provider")
			}
			selected, err := FilterProviderCandidates(candidates[provider], obj.GetOr("filter", ""),
				obj.GetOr("exclude-filter", ""), obj.GetOr("exclude-type", ""))
			if err != nil {
				return err
			}
			for _, candidate := range selected {
				key := Ref{"name", candidate.GetString("name")}
				if candidateNames[key] {
					return dialerError("策略组候选名称重复")
				}
				candidateNames[key] = true
				children = append(children, candidate.Value("key").(Ref))
			}
		}
		graph.set(objKey, children)
	}
	if err := checkCycles(graph); err != nil {
		return err
	}
	// Empty group auto-DIRECT must never be a dialer or an independently lowered exit.
	var pending []Ref
	for _, o := range objects {
		if target := o.Value("dialer-proxy"); jsonx.Truthy(target) {
			key, err := nameKey(target)
			if err != nil {
				return err
			}
			pending = append(pending, key)
		}
	}
	for _, value := range definitions.Values() {
		definition, err := compatObject(value)
		if err != nil {
			return err
		}
		override, err := compatObject(definition.GetOr("override", jsonx.NewObject()))
		if err != nil {
			return err
		}
		if target := override.Value("dialer-proxy"); jsonx.Truthy(target) {
			key, err := nameKey(target)
			if err != nil {
				return err
			}
			pending = append(pending, key)
		}
	}
	if opts.RequireProviders {
		pending = append(pending, groupOrder...)
	}
	visited := map[Ref]bool{}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[key] {
			continue
		}
		visited[key] = true
		if group, ok := groups[key]; ok && len(graph.children[key]) == 0 &&
			(opts.RequireProviders || !jsonx.Truthy(group.Value("use"))) {
			return dialerError("拨号策略组过滤后没有可用候选")
		}
		pending = append(pending, graph.children[key]...)
	}
	return nil
}

// RejectSurgeDialers is reject_surge_dialers(profile, client): Surge and
// Loon cannot express proxy chains or dialer-proxy. Groups and nodes are
// read one at a time, as Python's any() and loop do.
func RejectSurgeDialers(ctx context.Context, profile *jsonx.Object, client string, converter ProxyStringConverter) error {
	groups, err := compatList(profile.GetOr("proxy_groups", []any{}))
	if err != nil {
		return err
	}
	for _, item := range groups {
		g, err := compatObject(item)
		if err != nil {
			return err
		}
		if jsonx.Equal(g.Value("type"), "chain") && compatEnabled(g) {
			return dialerError(client + " 暂不支持代理链，请使用 Mihomo 或禁用代理链")
		}
	}
	nodes, err := compatList(profile.GetOr("nodes", []any{}))
	if err != nil {
		return err
	}
	for _, item := range nodes {
		node, err := compatObject(item)
		if err != nil {
			return err
		}
		if !compatEnabled(node) {
			continue
		}
		raw := any(nil)
		if node.Value("dialer_ref") == nil {
			if raw, err = rawDialer(node); err != nil {
				return err
			}
		}
		if node.Value("dialer_ref") != nil || raw != nil {
			return dialerError(client + " 暂不支持 Mihomo 拨号代理拓扑，请使用 Mihomo 或显式移除拨号配置")
		}
		if jsonx.Truthy(node.Value("proxy_string")) {
			proxy, err := ConvertNodeToMihomo(ctx, node, converter)
			if err != nil {
				return err
			}
			if proxy != nil && proxy.Len() > 0 && proxy.Value("dialer-proxy") != nil {
				return dialerError(client + " 暂不支持转换结果中的 dialer-proxy")
			}
		}
	}
	return nil
}

// DependencyNodeIDs is dependency_node_ids: the node ids in the dependency
// closure of the selected manual nodes.
func DependencyNodeIDs(profile *jsonx.Object, selected []string, includeRoots bool) (map[string]bool, error) {
	graph, err := dependencyGraph(profile)
	if err != nil {
		return nil, err
	}
	var pending []Ref
	result := map[string]bool{}
	for _, id := range selected {
		root := Ref{"node", id}
		if includeRoots {
			pending = append(pending, root)
			result[id] = true
		} else {
			pending = append(pending, graph.children[root]...)
		}
	}
	visited := map[Ref]bool{}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[key] {
			continue
		}
		visited[key] = true
		if key.Kind == "node" {
			result[key.ID] = true
		}
		pending = append(pending, graph.children[key]...)
	}
	return result, nil
}
