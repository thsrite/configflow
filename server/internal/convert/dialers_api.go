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
// subs.ValidateProxyChains). Python's `follow_group in chains` raises
// TypeError for an unhashable follow_group, which subs does not check; that
// is reproduced here first. Callers have passed validate_shapes.
func validateProxyChains(profile *jsonx.Object) error {
	groups, _ := profile.GetOr("proxy_groups", []any{}).([]any)
	for _, item := range groups {
		if group, ok := item.(*jsonx.Object); ok {
			if err := hashable(group.Value("follow_group")); err != nil {
				return err
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
// StopIteration.
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
	id, err := compatItem(ref, "id")
	if err != nil {
		return nil, err
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

// FilterProviderCandidates is filter_provider_candidates(proxies, include,
// exclude, exclude_type): match original names, preserving Mihomo's
// backtick filter ordering. The arguments are the values Python receives
// (normally strings; "" is the default).
func FilterProviderCandidates(proxies []*jsonx.Object, include, exclude, excludeType any) ([]*jsonx.Object, error) {
	invalid := func() ([]*jsonx.Object, error) { return nil, dialerError("Provider 正则过滤无效或不受支持") }
	excludeParts, err := splitText(exclude, "`")
	if err != nil {
		return nil, err
	}
	var exclusions, filters []*rules.PyRegex
	for _, value := range excludeParts {
		if value == "" {
			continue
		}
		re, err := rules.CompilePyRegex(value, false)
		if err != nil {
			return invalid()
		}
		exclusions = append(exclusions, re)
	}
	includeParts, err := splitText(include, "`")
	if err != nil {
		return nil, err
	}
	for _, value := range includeParts {
		re, err := rules.CompilePyRegex(value, false)
		if err != nil {
			return invalid()
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
			excludedTypes[pyLower(value)] = true
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
			if excludedTypes[pyLower(typeText)] {
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

// pySub is re.sub(pattern, repl, text) with a function replacement: the
// pattern is compiled by rules.CompilePyRegex (Python's parser and error
// texts) and substituted by PyRegex.Sub (CPython's match loop); repl gets
// the group texts ("" for a group that did not take part) and may fail with
// an IndexError.
func pySub(pattern, text string, repl func(groups []string, participated []bool) (string, error)) (string, error) {
	re, err := rules.CompilePyRegex(pattern, false)
	if err != nil {
		return "", err
	}
	out, err := re.Sub(text, func(m *rules.Match) (string, error) {
		texts := make([]string, m.Groups()+1)
		participated := make([]bool, m.Groups()+1)
		for i := range texts {
			texts[i], participated[i] = m.Group(i)
		}
		return repl(texts, participated)
	}, 0)
	if errors.Is(err, rules.ErrMatchTimeout) {
		return "", pyError("error", err.Error())
	}
	return out, err
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
			d, ok := decimalValue(runes[j])
			if !ok {
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

// ProviderProxyName is provider_proxy_name: project the native name
// overrides emitted by the chain compiler onto an original provider name.
func ProviderProxyName(proxy, override *jsonx.Object) (string, error) {
	nameValue, err := compatItem(proxy, "name")
	if err != nil {
		return "", err
	}
	rulesValue := override.GetOr("proxy-name", []any{})
	ruleList, err := compatList(rulesValue)
	if err != nil {
		return "", err
	}
	name, ok := nameValue.(string)
	if jsonx.Truthy(rulesValue) && len(ruleList) > 0 {
		targetValue, err := compatItem(ruleList[0], "target")
		if err != nil {
			return "", err
		}
		target, isText := targetValue.(string)
		if !isText {
			return "", attributeError(targetValue, "rsplit")
		}
		if !ok {
			return "", attributeError(nameValue, "startswith")
		}
		if strings.HasPrefix(name, pyRsplitFirst(target, "_", 2)+"_") {
			return "", dialerError("Provider 原始节点名称与代理链私有排序标记冲突")
		}
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
		if !ok {
			return "", pyError("TypeError", "expected string or bytes-like object, got '"+pyTypeName(nameValue)+"'")
		}
		rule := ruleValue.(*jsonx.Object)
		name, err = pySub(pattern, name, func(groups []string, _ []bool) (string, error) {
			targetValue, err := compatItem(rule, "target")
			if err != nil {
				return "", err
			}
			target, isText := targetValue.(string)
			if !isText {
				return "", pyError("TypeError", "expected string or bytes-like object, got '"+pyTypeName(targetValue)+"'")
			}
			return expandTarget(target, groups)
		})
		if err != nil {
			if rules.IsPyRegexError(err) || PyErrorClass(err) == "IndexError" || PyErrorClass(err) == "error" {
				return "", invalid
			}
			var unsupported *rules.UnsupportedRegexError
			if errors.As(err, &unsupported) {
				return "", invalid
			}
			return "", err
		}
	}
	prefix, suffix := override.GetOr("additional-prefix", ""), override.GetOr("additional-suffix", "")
	prefixText, ok1 := prefix.(string)
	suffixText, ok2 := suffix.(string)
	if !ok1 || !ok2 {
		return "", pyError("TypeError", "can only concatenate str (not \""+pyTypeName(prefix)+"\") to str")
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
// names, duplicates, cycles or empty dialer groups.
func ValidateEmitted(config *jsonx.Object, opts EmitOptions) error {
	proxiesValue, groupsValue := config.GetOr("proxies", []any{}), config.GetOr("proxy-groups", []any{})
	proxyList, ok1 := proxiesValue.([]any)
	groupList, ok2 := groupsValue.([]any)
	if !ok1 || !ok2 {
		return pyError("TypeError", "can only concatenate list to list")
	}
	objects, err := objectsOf(append(append([]any{}, proxyList...), groupList...))
	if err != nil {
		return err
	}
	definitions, err := compatObject(config.GetOr("proxy-providers", jsonx.NewObject()))
	if err != nil {
		return err
	}
	payloads := opts.ProviderProxies
	if payloads == nil {
		payloads = jsonx.NewObject()
	}
	active := opts.ForceChains
	for _, obj := range objects {
		if !active && obj.Value("dialer-proxy") != nil {
			active = true
		}
	}
	for _, value := range definitions.Values() {
		if active {
			break
		}
		definition, err := compatObject(value)
		if err != nil {
			return err
		}
		override, err := compatObject(definition.GetOr("override", jsonx.NewObject()))
		if err != nil {
			return err
		}
		active = jsonx.Truthy(override.Value("dialer-proxy"))
	}
	for _, value := range payloads.Values() {
		if active {
			break
		}
		list, err := objectsOf(value)
		if err != nil {
			return err
		}
		for _, p := range list {
			if p.Value("dialer-proxy") != nil {
				active = true
				break
			}
		}
	}
	if !active {
		return nil
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
	graph := newRefGraph()
	candidates := map[string][]*jsonx.Object{}
	for _, name := range definitions.Keys() {
		definition, err := compatObject(definitions.Value(name))
		if err != nil {
			return err
		}
		if !payloads.Has(name) {
			if opts.RequireProviders {
				return dialerError("拨号拓扑缺少已验证的 Provider 内容")
			}
			continue
		}
		proxies, err := objectsOf(payloads.Value(name))
		if err != nil {
			return err
		}
		rawNames := map[Ref]bool{}
		for _, p := range proxies {
			value, err := compatItem(p, "name")
			if err != nil {
				return err
			}
			key, err := nameKey(value)
			if err != nil {
				return err
			}
			if rawNames[key] {
				return dialerError("Provider 节点名称重复")
			}
			rawNames[key] = true
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
// Loon cannot express proxy chains or dialer-proxy.
func RejectSurgeDialers(ctx context.Context, profile *jsonx.Object, client string, converter ProxyStringConverter) error {
	groups, err := profileList(profile, "proxy_groups")
	if err != nil {
		return err
	}
	for _, g := range groups {
		if jsonx.Equal(g.Value("type"), "chain") && compatEnabled(g) {
			return dialerError(client + " 暂不支持代理链，请使用 Mihomo 或禁用代理链")
		}
	}
	nodes, err := profileList(profile, "nodes")
	if err != nil {
		return err
	}
	for _, node := range nodes {
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
