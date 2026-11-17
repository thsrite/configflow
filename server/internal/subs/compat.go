package subs

// Private copy of the parts of backend/utils/dialer_references.py that
// proxy_chains and subscription_dialers call, plus the structured
// proxy-string parser of converters/mihomo.py (_parse_structured_proxy_string).
// The real port is internal/convert (C12), which imports this package for
// validate_proxy_chains, so subs cannot use it (import cycle).
// compat_drift_test.go runs this copy over convert's Python corpus so the two
// cannot drift.

import (
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
)

// DialerReferenceError is dialer_references.DialerReferenceError, a
// StrategyReferenceError.
type DialerReferenceError struct{ Message string }

func (e *DialerReferenceError) Error() string { return e.Message }

// Unwrap lets errors.As find the StrategyReferenceError base class.
func (e *DialerReferenceError) Unwrap() error {
	return &rules.StrategyReferenceError{Message: e.Message}
}

func dre(msg string) error { return &DialerReferenceError{Message: msg} }

// parseStructuredProxyString is converters.mihomo._parse_structured_proxy_string.
func parseStructuredProxyString(proxyString string) *jsonx.Object {
	stripped := pyStrip(proxyString)
	firstLine, _, _ := strings.Cut(stripped, "\n")
	if strings.Contains(firstLine, "://") {
		scheme := stripped
		if i := strings.Index(stripped, "://"); i >= 0 {
			scheme = stripped[:i]
		}
		if !strings.HasSuffix(scheme, ":") || scheme == "" {
			first := pyStrip(firstLine)
			if !strings.HasSuffix(first, ":") && strings.Contains(first, "://") {
				sp := first[:strings.Index(first, "://")]
				if !strings.Contains(sp, " ") && !strings.Contains(sp, ":") {
					return nil
				}
			}
		}
	}
	if strings.HasPrefix(stripped, "{") {
		if v, err := jsonx.ParseString(stripped); err == nil {
			if o, ok := v.(*jsonx.Object); ok && jsonx.Truthy(o.Value("type")) {
				return o
			}
		}
	}
	if v, err := readSubscriptionYAML(stripped, false); err == nil {
		if o, ok := v.(*jsonx.Object); ok && bytesTruthy(o.Value("type")) {
			return o
		}
	}
	return nil
}

// bytesTruthy is bool(v) including bytes from !!binary (empty bytes are false).
func bytesTruthy(v any) bool {
	if b, ok := v.([]byte); ok {
		return len(b) > 0
	}
	return jsonx.Truthy(v)
}

// iterValue is `for x in v` over a list field: a str yields its characters,
// a dict its keys; anything else nothing.
func iterValue(v any) []any {
	switch value := v.(type) {
	case []any:
		return value
	case string:
		out := make([]any, 0, len(value))
		for _, r := range value {
			out = append(out, string(r))
		}
		return out
	case *jsonx.Object:
		return jsonx.ListOf(value.Keys())
	}
	return nil
}

// rawDialer is raw_dialer(node): the dialer-proxy value of a node or nil; an
// error stands for the exception Python would raise (node['params'] not a dict).
func rawDialer(node *jsonx.Object) (any, error) {
	if jsonx.Truthy(node.Value("proxy_string")) {
		ps, ok := node.Value("proxy_string").(string)
		if !ok {
			return nil, pyErr("AttributeError")
		}
		if parsed := parseStructuredProxyString(ps); parsed != nil {
			return parsed.Value("dialer-proxy"), nil
		}
		return nil, nil
	}
	params := any(jsonx.NewObject())
	if node.Has("params") {
		params = node.Value("params")
	}
	p, ok := params.(*jsonx.Object)
	if !ok {
		return nil, pyErr("'" + pyTypeName(params) + "' object has no attribute 'get'")
	}
	return p.Value("dialer-proxy"), nil
}

// ---------------------------------------------------------------- references

type ref struct{ kind, id string }

func objList(o *jsonx.Object, key string) []*jsonx.Object {
	var out []*jsonx.Object
	for _, v := range listField(o, key) {
		if x, ok := v.(*jsonx.Object); ok {
			out = append(out, x)
		}
	}
	return out
}

func enabled(o *jsonx.Object) bool { return jsonx.Truthy(getOr(o, "enabled", true)) }

func nonEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && pyStrip(s) != ""
}

func validateShapes(profile *jsonx.Object, requireIDs bool) error {
	for _, key := range []string{"nodes", "proxy_groups"} {
		var items []any
		if v, ok := profile.Get(key); ok {
			l, isList := v.([]any)
			if !isList {
				return dre(key + " 必须是数组")
			}
			items = l
		}
		for _, item := range items {
			obj, ok := item.(*jsonx.Object)
			if !ok {
				return dre("资源必须是 JSON 对象")
			}
			for _, field := range []string{"id", "name"} {
				if field == "id" && !requireIDs && !obj.Has("id") {
					continue
				}
				if !nonEmptyString(obj.Value(field)) {
					return dre("资源 " + field + " 必须是非空字符串")
				}
			}
			if key == "nodes" {
				if obj.Has("params") {
					if _, ok := obj.Value("params").(*jsonx.Object); !ok {
						return dre("节点 params 必须是对象，不能为 null")
					}
				}
				if ps := obj.Value("proxy_string"); ps != nil {
					if _, ok := ps.(string); !ok {
						return dre("节点 proxy_string 必须是字符串")
					}
				}
				continue
			}
			for _, field := range []string{"manual_nodes", "include_groups", "proxies", "subscriptions", "aggregations", "use"} {
				if !obj.Has(field) {
					continue
				}
				l, ok := obj.Value(field).([]any)
				if !ok {
					return dre("策略组 " + field + " 必须是字符串数组")
				}
				for _, v := range l {
					if !nonEmptyString(v) {
						return dre("策略组 " + field + " 必须是字符串数组")
					}
				}
			}
			var order []any
			if obj.Has("proxies_order") {
				l, ok := obj.Value("proxies_order").([]any)
				if !ok {
					return dre("策略组 proxies_order 必须是含 type/id 字符串的对象数组")
				}
				order = l
			}
			for _, i := range order {
				o, ok := i.(*jsonx.Object)
				if !ok || !isString(o.Value("type")) || !isString(o.Value("id")) {
					return dre("策略组 proxies_order 必须是含 type/id 字符串的对象数组")
				}
			}
		}
	}
	return nil
}

// groupMembers is group_members.
func groupMembers(group *jsonx.Object) []ref {
	var members []ref
	if jsonx.Truthy(group.Value("proxies_order")) {
		for _, i := range group.GetList("proxies_order") {
			o, _ := i.(*jsonx.Object)
			t := o.Value("type")
			if t == "node" || t == "strategy" {
				kind := "group"
				if t == "node" {
					kind = "node"
				}
				members = append(members, ref{kind, jsonx.Str(o.Value("id"))})
			}
		}
	} else {
		for _, id := range iterValue(group.Value("manual_nodes")) {
			members = append(members, ref{"node", jsonx.Str(id)})
		}
		for _, id := range iterValue(group.Value("include_groups")) {
			members = append(members, ref{"group", jsonx.Str(id)})
		}
		if len(members) == 0 {
			if src := group.Value("source"); src == "node" || src == "strategy" {
				kind := "group"
				if src == "node" {
					kind = "node"
				}
				for _, id := range iterValue(group.Value("proxies")) {
					members = append(members, ref{kind, jsonx.Str(id)})
				}
			}
		}
	}
	for i, m := range members {
		if m.kind == "node" && (m.id == "DIRECT" || m.id == "REJECT") {
			members[i].kind = "builtin"
		}
	}
	return members
}

func containsStr(list []any, s string) bool {
	for _, v := range list {
		if v == any(s) {
			return true
		}
	}
	return false
}

// groupDependencies is group_dependencies.
func groupDependencies(profile, group *jsonx.Object) ([]ref, error) {
	if jsonx.Truthy(group.Value("include_all")) || jsonx.Truthy(group.Value("include-all")) {
		return nil, dre("拨号拓扑不支持 include-all 隐式全局来源，请显式选择来源")
	}
	if jsonx.Truthy(group.Value("follow_group")) {
		return []ref{{"group", jsonx.Str(group.Value("follow_group"))}}, nil
	}
	children := groupMembers(group)
	subscriptions, aggregations := map[string]bool{}, map[string]bool{}
	for _, v := range group.GetList("subscriptions") {
		subscriptions[jsonx.Str(v)] = true
	}
	for _, v := range group.GetList("aggregations") {
		aggregations[jsonx.Str(v)] = true
	}
	for _, i := range group.GetList("proxies_order") {
		o := i.(*jsonx.Object)
		switch o.Value("type") {
		case "aggregation":
			aggregations[jsonx.Str(o.Value("id"))] = true
		case "subscription":
			subscriptions[jsonx.Str(o.Value("id"))] = true
		}
	}
	for _, n := range group.GetList("use") {
		var matches []ref
		for _, p := range [][2]string{{"subscription", "subscriptions"}, {"aggregation", "subscription_aggregations"}} {
			for _, obj := range objList(profile, p[1]) {
				if obj.Value("name") == n && enabled(obj) {
					matches = append(matches, ref{p[0], jsonx.Str(obj.Value("id"))})
				}
			}
		}
		if len(matches) != 1 {
			return nil, dre("策略组 use 引用不存在或名称不唯一")
		}
		if matches[0].kind == "subscription" {
			subscriptions[matches[0].id] = true
		} else {
			aggregations[matches[0].id] = true
		}
	}
	for _, p := range []struct {
		kind, key string
		ids       map[string]bool
	}{{"subscription", "subscriptions", subscriptions}, {"aggregation", "subscription_aggregations", aggregations}} {
		catalog := map[string]*jsonx.Object{}
		for _, o := range objList(profile, p.key) {
			catalog[jsonx.Str(o.Value("id"))] = o
		}
		for _, id := range sortedKeys(p.ids) {
			if c, ok := catalog[id]; !ok || !enabled(c) {
				return nil, dre("拨号策略组来源不存在或未启用")
			}
			children = append(children, ref{p.kind, id})
		}
	}
	if len(children) == 0 {
		return nil, dre("拨号策略组不能为空")
	}
	return children, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// dialerTarget is dialer_target.
func dialerTarget(profile, node *jsonx.Object) (*ref, error) {
	if r := node.Value("dialer_ref"); r != nil {
		o, _ := r.(*jsonx.Object)
		return &ref{jsonx.Str(o.Value("type")), jsonx.Str(o.Value("id"))}, nil
	}
	name, err := rawDialer(node)
	if err != nil {
		return nil, err
	}
	if name == nil {
		return nil, nil
	}
	s, ok := name.(string)
	if !ok || pyStrip(s) == "" {
		return nil, dre("原始 dialer-proxy 必须是非空名称")
	}
	if s == "DIRECT" || s == "REJECT" {
		return &ref{"builtin", s}, nil
	}
	var matches []ref
	for _, p := range [][2]string{{"node", "nodes"}, {"group", "proxy_groups"}} {
		for _, obj := range objList(profile, p[1]) {
			if obj.Value("name") == name && enabled(obj) {
				matches = append(matches, ref{p[0], jsonx.Str(obj.Value("id"))})
			}
		}
	}
	if len(matches) != 1 {
		return nil, dre("原始 dialer-proxy '" + s + "' 不存在、未启用或名称不唯一，请改用稳定引用")
	}
	return &matches[0], nil
}

type refObjects map[ref]*jsonx.Object

func profileObjects(profile *jsonx.Object) refObjects {
	objects := refObjects{}
	for _, p := range [][2]string{{"node", "nodes"}, {"group", "proxy_groups"}, {"subscription", "subscriptions"}, {"aggregation", "subscription_aggregations"}} {
		for _, o := range objList(profile, p[1]) {
			objects[ref{p[0], jsonx.Str(o.Value("id"))}] = o
		}
	}
	return objects
}

// chainExitDependencies is chain_exit_dependencies.
func chainExitDependencies(profile *jsonx.Object, exit ref, objects refObjects) ([]ref, error) {
	graph := newGraph()
	var dependencies []ref
	pending := []ref{exit}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if graph.has(key) {
			continue
		}
		if key.kind == "builtin" {
			return nil, dre("代理链落地组不能包含 DIRECT/REJECT 等无法施加前置代理的候选")
		}
		obj := objects[key]
		if obj == nil || !enabled(obj) {
			return nil, dre("代理链落地候选不存在或未启用")
		}
		var children []ref
		switch {
		case key.kind == "node":
			if jsonx.Truthy(obj.Value("subscription_id")) {
				return nil, dre("订阅节点请通过策略组 Provider 使用")
			}
		case key.kind == "group" && obj.Value("type") == "chain":
			dependencies = append(dependencies, key)
		case key.kind == "group":
			var err error
			if children, err = groupDependencies(profile, obj); err != nil {
				return nil, err
			}
		case key.kind == "aggregation":
			for _, id := range obj.GetList("nodes") {
				children = append(children, ref{"node", jsonx.Str(id)})
			}
			for _, id := range obj.GetList("subscriptions") {
				children = append(children, ref{"subscription", jsonx.Str(id)})
			}
			if len(children) == 0 {
				return nil, dre("代理链落地聚合不能为空")
			}
		}
		graph.set(key, children)
		pending = append(pending, children...)
	}
	if err := checkCycles(graph); err != nil {
		return nil, err
	}
	return dependencies, nil
}

// depGraph is an insertion-ordered dict of ref -> [ref].
type depGraph struct {
	order []ref
	m     map[ref][]ref
}

func newGraph() *depGraph { return &depGraph{m: map[ref][]ref{}} }
func (g *depGraph) has(k ref) bool {
	_, ok := g.m[k]
	return ok
}
func (g *depGraph) set(k ref, v []ref) {
	if !g.has(k) {
		g.order = append(g.order, k)
	}
	g.m[k] = v
}

// dependencyGraph is dependency_graph.
func dependencyGraph(profile *jsonx.Object) (*depGraph, error) {
	if err := validateShapes(profile, true); err != nil {
		return nil, err
	}
	if err := ValidateProxyChains(profile); err != nil {
		return nil, err
	}
	objects := profileObjects(profile)
	graph := newGraph()
	var pending []ref
	// objects iteration order: nodes, groups, subscriptions, aggregations.
	for _, p := range [][2]string{{"node", "nodes"}, {"group", "proxy_groups"}, {"subscription", "subscriptions"}, {"aggregation", "subscription_aggregations"}} {
		seen := map[ref]bool{}
		for _, o := range objList(profile, p[1]) {
			key := ref{p[0], jsonx.Str(o.Value("id"))}
			if seen[key] {
				continue
			}
			seen[key] = true
			obj := objects[key]
			if !enabled(obj) {
				continue
			}
			if key.kind == "node" {
				if obj.Value("dialer_ref") != nil {
					pending = append(pending, key)
				} else if d, err := rawDialer(obj); err != nil {
					return nil, err
				} else if d != nil {
					pending = append(pending, key)
				}
			} else if key.kind == "group" && obj.Value("type") == "chain" {
				pending = append(pending, key)
			}
		}
	}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if graph.has(key) || key.kind == "builtin" {
			continue
		}
		obj := objects[key]
		if obj == nil || !enabled(obj) {
			return nil, dre("拨号依赖不存在或未启用")
		}
		var children []ref
		var err error
		switch {
		case key.kind == "group" && obj.Value("type") == "chain":
			chain := obj.GetObject("chain")
			entry := chain.GetObject("entry")
			children = []ref{{jsonx.Str(entry.Value("type")), jsonx.Str(entry.Value("id"))}}
			exit := chain.GetObject("exit")
			deps, derr := chainExitDependencies(profile, ref{jsonx.Str(exit.Value("type")), jsonx.Str(exit.Value("id"))}, objects)
			if derr != nil {
				return nil, derr
			}
			children = append(children, deps...)
		case key.kind == "group":
			children, err = groupDependencies(profile, obj)
		case key.kind == "subscription":
		case key.kind == "aggregation":
			for _, id := range obj.GetList("nodes") {
				children = append(children, ref{"node", jsonx.Str(id)})
			}
			for _, id := range obj.GetList("subscriptions") {
				children = append(children, ref{"subscription", jsonx.Str(id)})
			}
			if len(children) == 0 {
				err = dre("拨号策略组聚合来源不能为空")
			}
		default:
			var target *ref
			target, err = dialerTarget(profile, obj)
			if target != nil {
				children = []ref{*target}
			}
		}
		if err != nil {
			return nil, err
		}
		graph.set(key, children)
		pending = append(pending, children...)
	}
	if err := checkCycles(graph); err != nil {
		return nil, err
	}
	return graph, nil
}

// checkCycles is check_cycles.
func checkCycles(graph *depGraph) error {
	type frame struct {
		key     ref
		exiting bool
	}
	visiting, done := map[ref]bool{}, map[ref]bool{}
	for _, root := range graph.order {
		pending := []frame{{root, false}}
		for len(pending) > 0 {
			f := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if f.exiting {
				delete(visiting, f.key)
				done[f.key] = true
				continue
			}
			if visiting[f.key] {
				return dre("拨号代理依赖存在循环")
			}
			if done[f.key] {
				continue
			}
			visiting[f.key] = true
			pending = append(pending, frame{f.key, true})
			children := graph.m[f.key]
			for i := len(children) - 1; i >= 0; i-- {
				pending = append(pending, frame{children[i], false})
			}
		}
	}
	return nil
}

// ValidateDialers is dialer_references.validate_dialers.
func ValidateDialers(profile *jsonx.Object) error {
	if err := validateShapes(profile, true); err != nil {
		return err
	}
	if err := ValidateProxyChains(profile); err != nil {
		return err
	}
	nodes := objList(profile, "nodes")
	groupsList := objList(profile, "proxy_groups")
	active := false
	for _, g := range groupsList {
		if g.Value("type") == "chain" {
			active = true
		}
	}
	for _, n := range nodes {
		if active {
			break
		}
		if n.Value("dialer_ref") != nil {
			active = true
			break
		}
		d, err := rawDialer(n)
		if err != nil {
			return err
		}
		if d != nil {
			active = true
		}
	}
	if active {
		var names []any
		for _, items := range [][]*jsonx.Object{nodes, groupsList} {
			seen := map[any]bool{}
			for _, o := range items {
				id := o.Value("id")
				if seen[id] {
					return dre("拨号引用要求资源 ID 唯一")
				}
				seen[id] = true
			}
			for _, o := range items {
				if enabled(o) {
					names = append(names, o.Value("name"))
				}
			}
		}
		uniq := map[any]bool{}
		bad := false
		for _, n := range names {
			if !nonEmptyString(n) {
				bad = true
				continue
			}
			if uniq[n] {
				bad = true
			}
			uniq[n] = true
			if dialerBuiltins[n.(string)] {
				bad = true
			}
		}
		if bad {
			return dre("拨号代理要求代理/策略组名称非空、唯一且不与内置策略冲突")
		}
	}
	for _, node := range nodes {
		r := node.Value("dialer_ref")
		if r == nil {
			continue
		}
		o, ok := r.(*jsonx.Object)
		if !ok || !onlyKeys(o, "type", "id") || !(o.Value("type") == "node" || o.Value("type") == "group") || !nonEmptyString(o.Value("id")) {
			return dre("dialer_ref 必须为 {type: node|group, id: 非空字符串}")
		}
		key := "proxy_groups"
		if o.Value("type") == "node" {
			key = "nodes"
		}
		var target *jsonx.Object
		for _, t := range objList(profile, key) {
			if t.Value("id") == o.Value("id") {
				target = t
				break
			}
		}
		if target == nil || !enabled(target) {
			return dre("拨号引用不存在、未启用或不在当前配置空间")
		}
		if o.Value("type") == "node" && jsonx.Equal(target.Value("id"), node.Value("id")) {
			return dre("节点不能引用自身作为拨号代理")
		}
	}

	graph, err := dependencyGraph(profile)
	if err != nil {
		return err
	}
	objects := profileObjects(profile)
	var pending []ref
	for _, n := range nodes {
		if n.Value("dialer_ref") != nil {
			pending = append(pending, ref{"node", jsonx.Str(n.Value("id"))})
		}
	}
	for _, g := range groupsList {
		if g.Value("type") == "chain" {
			pending = append(pending, ref{"group", jsonx.Str(g.Value("id"))})
		}
	}
	roots := map[ref]bool{}
	for _, p := range pending {
		roots[p] = true
	}
	visited := map[ref]bool{}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[key] || key.kind == "builtin" {
			continue
		}
		visited[key] = true
		obj := objects[key]
		if obj == nil {
			return dre("拨号依赖不存在")
		}
		if key.kind == "node" && jsonx.Truthy(obj.Value("subscription_id")) {
			return dre("稳定拨号引用仅支持手动节点，不能使用订阅节点")
		}
		if !enabled(obj) && !roots[key] {
			return dre("拨号依赖不存在或未启用")
		}
		switch {
		case graph.has(key):
			pending = append(pending, graph.m[key]...)
		case key.kind == "node":
			target, err := dialerTarget(profile, obj)
			if err != nil {
				return err
			}
			if target != nil {
				pending = append(pending, *target)
			}
		case obj.Value("type") == "chain":
			entry := obj.GetObject("chain").GetObject("entry")
			pending = append(pending, ref{jsonx.Str(entry.Value("type")), jsonx.Str(entry.Value("id"))})
		case key.kind == "group":
			deps, err := groupDependencies(profile, obj)
			if err != nil {
				return err
			}
			pending = append(pending, deps...)
		}
	}
	return nil
}

func onlyKeys(o *jsonx.Object, keys ...string) bool {
	if o.Len() != len(keys) {
		return false
	}
	for _, k := range keys {
		if !o.Has(k) {
			return false
		}
	}
	return true
}

// resolveDialer is resolve_dialer: the name of the node's dialer_ref target.
func resolveDialer(profile, node *jsonx.Object) (string, error) {
	r, ok := node.Value("dialer_ref").(*jsonx.Object)
	if !ok {
		return "", nil
	}
	key := "proxy_groups"
	if r.Value("type") == "node" {
		key = "nodes"
	}
	for _, t := range objList(profile, key) {
		if t.Value("id") == r.Value("id") {
			return jsonx.Str(t.Value("name")), nil
		}
	}
	return "", pyErr("StopIteration")
}

// Candidate is a proxy-like object with a name and type used by
// filter_provider_candidates; Raw carries the original value.
type Candidate struct {
	Name string
	Type string
	Raw  any
}

// filterProviderCandidates is filter_provider_candidates.
func filterProviderCandidates(proxies []Candidate, include, exclude, excludeType string) ([]Candidate, error) {
	var exclusions, filters []Matcher
	fail := func() ([]Candidate, error) { return nil, dre("Provider 正则过滤无效或不受支持") }
	for _, v := range strings.Split(exclude, "`") {
		if v == "" {
			continue
		}
		re, err := compileRegex(v)
		if err != nil {
			return fail()
		}
		exclusions = append(exclusions, re)
	}
	for _, v := range strings.Split(include, "`") {
		re, err := compileRegex(v)
		if err != nil {
			return fail()
		}
		filters = append(filters, re)
	}
	excludedTypes := map[string]bool{}
	for _, v := range strings.Split(excludeType, "|") {
		if v != "" {
			excludedTypes[pyLower(v)] = true
		}
	}
	var selected []Candidate
	seen := map[string]bool{}
	for _, pattern := range filters {
		for _, p := range proxies {
			if excludedTypes[pyLower(p.Type)] {
				continue
			}
			if seen[p.Name] || !pattern.Search(p.Name) {
				continue
			}
			excluded := false
			for _, e := range exclusions {
				if e.Search(p.Name) {
					excluded = true
					break
				}
			}
			if !excluded {
				selected = append(selected, p)
				seen[p.Name] = true
			}
		}
	}
	return selected, nil
}
