package convert

// Port of backend/utils/dialer_references.py: profile-local Mihomo dialer
// metadata; names are resolved only at emission. Python's unintended
// exceptions on malformed values (TypeError for unhashable ids, KeyError,
// AttributeError) are *PyError values.

import (
	"math/big"
	"sort"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

// nonBlankString is isinstance(v, str) and v.strip().
func nonBlankString(v any) bool {
	text, ok := v.(string)
	return ok && pystr.Strip(text) != ""
}

// compatEnabled is obj.get('enabled', True) as a condition.
func compatEnabled(obj *jsonx.Object) bool {
	return jsonx.Truthy(obj.GetOr("enabled", true))
}

// compatObject is the receiver of a `.get`: anything but a dict raises
// AttributeError in Python.
func compatObject(v any) (*jsonx.Object, error) {
	if obj, ok := v.(*jsonx.Object); ok && obj != nil {
		return obj, nil
	}
	return nil, pyError("AttributeError", "'"+pyTypeName(v)+"' object has no attribute 'get'")
}

// compatList is the operand of a `for ... in value` over a list field.
func compatList(v any) ([]any, error) {
	if v == nil {
		return nil, pyError("TypeError", "'NoneType' object is not iterable")
	}
	switch value := v.(type) {
	case []any:
		return value, nil
	case string:
		result := make([]any, 0, len(value))
		for _, r := range value {
			result = append(result, string(r))
		}
		return result, nil
	case *jsonx.Object:
		return jsonx.ListOf(value.Keys()), nil
	}
	return nil, pyError("TypeError", "'"+pyTypeName(v)+"' object is not iterable")
}

// compatItem is obj[key].
func compatItem(v any, key string) (any, error) {
	obj, ok := v.(*jsonx.Object)
	if !ok || obj == nil {
		return nil, pyError("TypeError", "'"+pyTypeName(v)+"' object is not subscriptable")
	}
	value, present := obj.Get(key)
	if !present {
		return nil, pyError("KeyError", jsonx.Repr(key))
	}
	return value, nil
}

// hashID turns a value used as a dict key or set member into a comparable
// string: strings are themselves, anything else gets a prefix no id has.
func hashID(v any) (string, error) {
	switch value := v.(type) {
	case string:
		return value, nil
	case []any:
		return "", pyError("TypeError", "unhashable type: 'list'")
	case *jsonx.Object:
		return "", pyError("TypeError", "unhashable type: 'dict'")
	case jsonx.Number:
		if value.IsFloat() {
			if f := value.Float64(); f == float64(int64(f)) {
				return "\x00" + jsonx.Repr(jsonx.Int(int64(f))), nil
			}
		}
	case bool:
		if value {
			return "\x001", nil
		}
		return "\x000", nil
	}
	return "\x00" + jsonx.Repr(v), nil
}

// Ref is a (kind, id) tuple of the dependency graphs.
type Ref struct{ Kind, ID string }

// objectMap is a dict of resources keyed by id: insertion-ordered, a later
// duplicate replaces the value and keeps the first position.
type objectMap struct {
	keys   []string
	values map[string]*jsonx.Object
}

func newObjectMap() *objectMap { return &objectMap{values: map[string]*jsonx.Object{}} }

func (m *objectMap) set(key string, value *jsonx.Object) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// byID is {item['id']: item for item in items}.
func byID(items any) (*objectMap, error) {
	list, err := compatList(items)
	if err != nil {
		return nil, err
	}
	result := newObjectMap()
	for _, item := range list {
		id, err := compatItem(item, "id")
		if err != nil {
			return nil, err
		}
		key, err := hashID(id)
		if err != nil {
			return nil, err
		}
		result.set(key, item.(*jsonx.Object))
	}
	return result, nil
}

// refObjects is the `objects` dict of validate_dialers and dependency_graph.
type refObjects struct {
	keys   []Ref
	values map[Ref]*jsonx.Object
}

func (m *refObjects) set(key Ref, value *jsonx.Object) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

func profileObjects(profile *jsonx.Object) (*refObjects, error) {
	objects := &refObjects{values: map[Ref]*jsonx.Object{}}
	for _, source := range [][2]string{{"node", "nodes"}, {"group", "proxy_groups"},
		{"subscription", "subscriptions"}, {"aggregation", "subscription_aggregations"}} {
		list, err := compatList(profile.GetOr(source[1], []any{}))
		if err != nil {
			return nil, err
		}
		for _, item := range list {
			id, err := compatItem(item, "id")
			if err != nil {
				return nil, err
			}
			key, err := hashID(id)
			if err != nil {
				return nil, err
			}
			objects.set(Ref{source[0], key}, item.(*jsonx.Object))
		}
	}
	return objects, nil
}

// Graph is an insertion-ordered dict of key -> children.
type Graph struct {
	keys     []Ref
	children map[Ref][]Ref
}

func newRefGraph() *Graph { return &Graph{children: map[Ref][]Ref{}} }

func (g *Graph) has(key Ref) bool { _, ok := g.children[key]; return ok }

func (g *Graph) set(key Ref, children []Ref) {
	if !g.has(key) {
		g.keys = append(g.keys, key)
	}
	g.children[key] = children
}

// ---- dialer_references.py ----

// validateShapes is validate_shapes: metadata is checked before attribute
// access, hashing or string parsing.
func validateShapes(profile *jsonx.Object, requireIDs bool) error {
	for _, key := range []string{"nodes", "proxy_groups"} {
		items, ok := profile.GetOr(key, []any{}).([]any)
		if !ok {
			return dialerError(key + " 必须是数组")
		}
		for _, item := range items {
			obj, ok := item.(*jsonx.Object)
			if !ok || obj == nil {
				return dialerError("资源必须是 JSON 对象")
			}
			for _, field := range []string{"id", "name"} {
				// Legacy subscription caches may omit generated resource IDs.
				if field == "id" && !requireIDs && !obj.Has(field) {
					continue
				}
				if !nonBlankString(obj.Value(field)) {
					return dialerError("资源 " + field + " 必须是非空字符串")
				}
			}
			if key == "nodes" {
				if _, ok := obj.GetOr("params", jsonx.NewObject()).(*jsonx.Object); !ok {
					return dialerError("节点 params 必须是对象，不能为 null")
				}
				if value := obj.Value("proxy_string"); value != nil {
					if _, ok := value.(string); !ok {
						return dialerError("节点 proxy_string 必须是字符串")
					}
				}
				continue
			}
			for _, field := range []string{"manual_nodes", "include_groups", "proxies", "subscriptions", "aggregations", "use"} {
				value, present := obj.Get(field)
				if !present {
					continue
				}
				list, ok := value.([]any)
				valid := ok
				for _, v := range list {
					if !nonBlankString(v) {
						valid = false
						break
					}
				}
				if !valid {
					return dialerError("策略组 " + field + " 必须是字符串数组")
				}
			}
			order, ok := obj.GetOr("proxies_order", []any{}).([]any)
			valid := ok
			for _, v := range order {
				entry, ok := v.(*jsonx.Object)
				if !ok || entry == nil {
					valid = false
					break
				}
				_, typeOK := entry.Value("type").(string)
				_, idOK := entry.Value("id").(string)
				if !typeOK || !idOK {
					valid = false
					break
				}
			}
			if !valid {
				return dialerError("策略组 proxies_order 必须是含 type/id 字符串的对象数组")
			}
		}
	}
	return nil
}

var dialerBuiltinNames = map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true,
	"PASS": true, "PASS-RULE": true, "COMPATIBLE": true}

// validReference is the shape check shared by dialer_ref and chain endpoints:
// a dict with exactly type (node|group) and a non-blank string id.
func validReference(v any) bool {
	ref, ok := v.(*jsonx.Object)
	if !ok || ref == nil || ref.Len() != 2 || !ref.Has("type") || !ref.Has("id") {
		return false
	}
	kind, _ := ref.Value("type").(string)
	return (kind == "node" || kind == "group") && nonBlankString(ref.Value("id"))
}

// profileList is profile.get(key, []) as a list of objects.
func profileList(profile *jsonx.Object, key string) ([]*jsonx.Object, error) {
	list, err := compatList(profile.GetOr(key, []any{}))
	if err != nil {
		return nil, err
	}
	result := make([]*jsonx.Object, 0, len(list))
	for _, item := range list {
		obj, err := compatObject(item)
		if err != nil {
			return nil, err
		}
		result = append(result, obj)
	}
	return result, nil
}

// validateDialers is validate_dialers.
func validateDialers(profile *jsonx.Object) error {
	if err := validateShapes(profile, true); err != nil {
		return err
	}
	if err := validateProxyChains(profile); err != nil {
		return err
	}
	nodes, err := profileList(profile, "nodes")
	if err != nil {
		return err
	}
	groups, err := profileList(profile, "proxy_groups")
	if err != nil {
		return err
	}
	active := false
	for _, group := range groups {
		if jsonx.Equal(group.Value("type"), "chain") {
			active = true
			break
		}
	}
	if !active {
		for _, node := range nodes {
			if node.Value("dialer_ref") != nil {
				active = true
				break
			}
			target, err := rawDialer(node)
			if err != nil {
				return err
			}
			if target != nil {
				active = true
				break
			}
		}
	}
	if active {
		var names []any
		for _, items := range [][]*jsonx.Object{nodes, groups} {
			seen := map[string]bool{}
			for _, obj := range items {
				key, err := hashID(obj.Value("id"))
				if err != nil {
					return err
				}
				if seen[key] {
					return dialerError("拨号引用要求资源 ID 唯一")
				}
				seen[key] = true
			}
			for _, obj := range items {
				if compatEnabled(obj) {
					names = append(names, obj.Value("name"))
				}
			}
		}
		invalid := false
		for _, name := range names {
			if !nonBlankString(name) {
				invalid = true
				break
			}
		}
		if !invalid {
			seen := map[string]bool{}
			for _, name := range names {
				text := name.(string)
				if seen[text] {
					invalid = true
				}
				seen[text] = true
			}
			for name := range seen {
				if dialerBuiltinNames[name] {
					invalid = true
				}
			}
		}
		if invalid {
			return dialerError("拨号代理要求代理/策略组名称非空、唯一且不与内置策略冲突")
		}
	}
	for _, node := range nodes {
		refValue := node.Value("dialer_ref")
		if refValue == nil {
			continue
		}
		if !validReference(refValue) {
			return dialerError("dialer_ref 必须为 {type: node|group, id: 非空字符串}")
		}
		ref := refValue.(*jsonx.Object)
		targets := groups
		if ref.GetString("type") == "node" {
			targets = nodes
		}
		var target *jsonx.Object
		for _, candidate := range targets {
			if jsonx.Equal(candidate.Value("id"), ref.Value("id")) {
				target = candidate
				break
			}
		}
		if target == nil || !compatEnabled(target) {
			return dialerError("拨号引用不存在、未启用或不在当前配置空间")
		}
		if ref.GetString("type") == "node" && jsonx.Equal(target.Value("id"), node.Value("id")) {
			return dialerError("节点不能引用自身作为拨号代理")
		}
	}

	graph, err := dependencyGraphChecked(profile)
	if err != nil {
		return err
	}
	// Managed references have a manual-only contract. Legacy raw name references
	// remain a separate compatibility mode and are not rewritten into managed refs.
	objects, err := profileObjects(profile)
	if err != nil {
		return err
	}
	var pending []Ref
	for _, node := range nodes {
		if node.Value("dialer_ref") != nil {
			pending = append(pending, Ref{"node", node.GetString("id")})
		}
	}
	for _, group := range groups {
		if jsonx.Equal(group.Value("type"), "chain") {
			pending = append(pending, Ref{"group", group.GetString("id")})
		}
	}
	visited := map[Ref]bool{}
	roots := map[Ref]bool{}
	for _, key := range pending {
		roots[key] = true
	}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[key] || key.Kind == "builtin" {
			continue
		}
		visited[key] = true
		obj := objects.values[key]
		if obj == nil {
			return dialerError("拨号依赖不存在")
		}
		if key.Kind == "node" && jsonx.Truthy(obj.Value("subscription_id")) {
			return dialerError("稳定拨号引用仅支持手动节点，不能使用订阅节点")
		}
		if !compatEnabled(obj) && !roots[key] {
			return dialerError("拨号依赖不存在或未启用")
		}
		switch {
		case graph.has(key):
			pending = append(pending, graph.children[key]...)
		case key.Kind == "node":
			// Disabled managed sources still retain the manual-only metadata
			// contract, although they are not roots of the emitted topology.
			target, err := dialerTarget(profile, obj)
			if err != nil {
				return err
			}
			if target != nil {
				pending = append(pending, *target)
			}
		case jsonx.Equal(obj.Value("type"), "chain"):
			entry, err := chainEndpoint(obj, "entry")
			if err != nil {
				return err
			}
			pending = append(pending, entry)
		case key.Kind == "group":
			children, err := groupDependencies(profile, obj)
			if err != nil {
				return err
			}
			pending = append(pending, children...)
		}
	}
	return nil
}

// chainEndpoint is (ref['type'], ref['id']) for ref = obj['chain'][endpoint].
func chainEndpoint(obj *jsonx.Object, endpoint string) (Ref, error) {
	chain, err := compatItem(obj, "chain")
	if err != nil {
		return Ref{}, err
	}
	ref, err := compatItem(chain, endpoint)
	if err != nil {
		return Ref{}, err
	}
	return referenceKey(ref)
}

func referenceKey(ref any) (Ref, error) {
	kind, err := compatItem(ref, "type")
	if err != nil {
		return Ref{}, err
	}
	id, err := compatItem(ref, "id")
	if err != nil {
		return Ref{}, err
	}
	kindKey, err := hashID(kind)
	if err != nil {
		return Ref{}, err
	}
	idKey, err := hashID(id)
	if err != nil {
		return Ref{}, err
	}
	return Ref{kindKey, idKey}, nil
}

// groupMembers is group_members.
func groupMembers(group *jsonx.Object) ([]Ref, error) {
	var members []Ref
	add := func(kind string, id any) error {
		key, err := hashID(id)
		if err != nil {
			return err
		}
		members = append(members, Ref{kind, key})
		return nil
	}
	addAll := func(kind string, value any) error {
		list, err := compatList(value)
		if err != nil {
			return err
		}
		for _, id := range list {
			if err := add(kind, id); err != nil {
				return err
			}
		}
		return nil
	}
	if order := group.Value("proxies_order"); jsonx.Truthy(order) {
		list, err := compatList(order)
		if err != nil {
			return nil, err
		}
		for _, item := range list {
			entry, err := compatObject(item)
			if err != nil {
				return nil, err
			}
			kind, _ := entry.Value("type").(string)
			if kind != "node" && kind != "strategy" {
				continue
			}
			if kind == "strategy" {
				kind = "group"
			}
			if err := add(kind, entry.Value("id")); err != nil {
				return nil, err
			}
		}
	} else {
		if err := addAll("node", group.GetOr("manual_nodes", []any{})); err != nil {
			return nil, err
		}
		if err := addAll("group", group.GetOr("include_groups", []any{})); err != nil {
			return nil, err
		}
		if source, _ := group.Value("source").(string); len(members) == 0 && (source == "node" || source == "strategy") {
			kind := "group"
			if source == "node" {
				kind = "node"
			}
			if err := addAll(kind, group.GetOr("proxies", []any{})); err != nil {
				return nil, err
			}
		}
	}
	for i, member := range members {
		if member.Kind == "node" && (member.ID == "DIRECT" || member.ID == "REJECT") {
			members[i].Kind = "builtin"
		}
	}
	return members, nil
}

// idSet is a set of ids (hashed key -> the first id value) with sorted()
// iteration.
type idSet map[string]any

func (s idSet) add(id any) error {
	key, err := hashID(id)
	if err != nil {
		return err
	}
	if _, ok := s[key]; !ok {
		s[key] = id
	}
	return nil
}

func (s idSet) update(value any) error {
	list, err := compatList(value)
	if err != nil {
		return err
	}
	for _, id := range list {
		if err := s.add(id); err != nil {
			return err
		}
	}
	return nil
}

// sorted is sorted(ids) as hashed keys: strings in code point order,
// numbers (bools included) by value; comparing a str with a number, or None
// with anything, is Python's TypeError.
func (s idSet) sorted() ([]string, error) {
	keys := make([]string, 0, len(s))
	kinds := map[string]bool{}
	for key, id := range s {
		keys = append(keys, key)
		switch id.(type) {
		case string:
			kinds["str"] = true
		case bool, jsonx.Number:
			kinds["number"] = true
		default:
			kinds[pyTypeName(id)] = true
		}
	}
	if len(keys) < 2 {
		return keys, nil
	}
	if len(kinds) > 1 || kinds["NoneType"] {
		names := make([]string, 0, len(kinds))
		for kind := range kinds {
			if kind == "number" {
				kind = "int"
			}
			names = append(names, kind)
		}
		sort.Strings(names)
		if len(names) == 1 {
			names = append(names, names[0])
		}
		return nil, pyError("TypeError", "'<' not supported between instances of '"+names[0]+"' and '"+names[1]+"'")
	}
	if kinds["number"] {
		sort.Slice(keys, func(i, j int) bool { return pyNumberLess(s[keys[i]], s[keys[j]]) })
		return keys, nil
	}
	sort.Strings(keys)
	return keys, nil
}

// pyNumberLess is a < b for Python numbers (bools are 0 and 1).
func pyNumberLess(a, b any) bool {
	toRat := func(v any) *big.Rat {
		switch value := v.(type) {
		case bool:
			if value {
				return big.NewRat(1, 1)
			}
			return new(big.Rat)
		case jsonx.Number:
			if value.IsInt() {
				n, _ := value.BigInt()
				return new(big.Rat).SetInt(n)
			}
			if r := new(big.Rat); r.SetFloat64(value.Float64()) != nil {
				return r
			}
		}
		return nil
	}
	x, y := toRat(a), toRat(b)
	if x == nil || y == nil {
		fa, _ := jsonx.AsFloat(a)
		fb, _ := jsonx.AsFloat(b)
		return fa < fb
	}
	return x.Cmp(y) < 0
}

// groupDependencies is group_dependencies: logical sources include
// providers; actual remote edges are checked at delivery.
func groupDependencies(profile, group *jsonx.Object) ([]Ref, error) {
	if jsonx.Truthy(group.Value("include_all")) || jsonx.Truthy(group.Value("include-all")) {
		return nil, dialerError("拨号拓扑不支持 include-all 隐式全局来源，请显式选择来源")
	}
	if follow := group.Value("follow_group"); jsonx.Truthy(follow) {
		key, err := hashID(follow)
		if err != nil {
			return nil, err
		}
		return []Ref{{"group", key}}, nil
	}
	children, err := groupMembers(group)
	if err != nil {
		return nil, err
	}
	subscriptions, aggregations := idSet{}, idSet{}
	if err := subscriptions.update(group.GetOr("subscriptions", []any{})); err != nil {
		return nil, err
	}
	if err := aggregations.update(group.GetOr("aggregations", []any{})); err != nil {
		return nil, err
	}
	order, err := compatList(group.GetOr("proxies_order", []any{}))
	if err != nil {
		return nil, err
	}
	for _, pass := range []struct {
		Kind string
		ids  idSet
	}{{"aggregation", aggregations}, {"subscription", subscriptions}} {
		for _, item := range order {
			kind, err := compatItem(item, "type")
			if err != nil {
				return nil, err
			}
			if !jsonx.Equal(kind, pass.Kind) {
				continue
			}
			id, err := compatItem(item, "id")
			if err != nil {
				return nil, err
			}
			if err := pass.ids.add(id); err != nil {
				return nil, err
			}
		}
	}
	use, err := compatList(group.GetOr("use", []any{}))
	if err != nil {
		return nil, err
	}
	sources := []struct{ Kind, key string }{{"subscription", "subscriptions"}, {"aggregation", "subscription_aggregations"}}
	for _, name := range use {
		var matches []Ref
		var matchIDs []any
		for _, source := range sources {
			items, err := compatList(profile.GetOr(source.key, []any{}))
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				obj, err := compatObject(item)
				if err != nil {
					return nil, err
				}
				if !jsonx.Equal(obj.Value("name"), name) || !compatEnabled(obj) {
					continue
				}
				id, err := compatItem(obj, "id")
				if err != nil {
					return nil, err
				}
				// The tuple is hashed only when its id is added to the set.
				matches = append(matches, Ref{source.Kind, ""})
				matchIDs = append(matchIDs, id)
			}
		}
		if len(matches) != 1 {
			return nil, dialerError("策略组 use 引用不存在或名称不唯一")
		}
		target := aggregations
		if matches[0].Kind == "subscription" {
			target = subscriptions
		}
		if err := target.add(matchIDs[0]); err != nil {
			return nil, err
		}
	}
	for _, pass := range []struct {
		Kind string
		ids  idSet
		key  string
	}{{"subscription", subscriptions, "subscriptions"}, {"aggregation", aggregations, "subscription_aggregations"}} {
		catalog, err := byID(profile.GetOr(pass.key, []any{}))
		if err != nil {
			return nil, err
		}
		ids, err := pass.ids.sorted()
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			obj := catalog.values[id]
			if obj == nil || !compatEnabled(obj) {
				return nil, dialerError("拨号策略组来源不存在或未启用")
			}
			children = append(children, Ref{pass.Kind, id})
		}
	}
	if len(children) == 0 {
		return nil, dialerError("拨号策略组不能为空")
	}
	return children, nil
}

// rawDialer is raw_dialer: the dialer-proxy a node names itself, read from
// its structured proxy string or its params. It never contacts Sub-Store.
func rawDialer(node *jsonx.Object) (any, error) {
	if proxyString := node.Value("proxy_string"); jsonx.Truthy(proxyString) {
		text, ok := proxyString.(string)
		if !ok {
			return nil, pyError("AttributeError", "'"+pyTypeName(proxyString)+"' object has no attribute 'strip'")
		}
		parsed := InspectStructuredProxyString(text)
		if parsed == nil || parsed.Len() == 0 {
			return nil, nil
		}
		return parsed.Value("dialer-proxy"), nil
	}
	params, err := compatObject(node.GetOr("params", jsonx.NewObject()))
	if err != nil {
		return nil, err
	}
	return params.Value("dialer-proxy"), nil
}

// dialerTarget is dialer_target; nil means None.
func dialerTarget(profile, node *jsonx.Object) (*Ref, error) {
	if ref := node.Value("dialer_ref"); ref != nil {
		key, err := referenceKey(ref)
		if err != nil {
			return nil, err
		}
		return &key, nil
	}
	value, err := rawDialer(node)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	name, ok := value.(string)
	if !ok || pystr.Strip(name) == "" {
		return nil, dialerError("原始 dialer-proxy 必须是非空名称")
	}
	if name == "DIRECT" || name == "REJECT" {
		return &Ref{"builtin", name}, nil
	}
	var matches []Ref
	for _, source := range [][2]string{{"node", "nodes"}, {"group", "proxy_groups"}} {
		items, err := compatList(profile.GetOr(source[1], []any{}))
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			obj, err := compatObject(item)
			if err != nil {
				return nil, err
			}
			if !jsonx.Equal(obj.Value("name"), name) || !compatEnabled(obj) {
				continue
			}
			id, err := compatItem(obj, "id")
			if err != nil {
				return nil, err
			}
			key, err := hashID(id)
			if err != nil {
				return nil, err
			}
			matches = append(matches, Ref{source[0], key})
		}
	}
	if len(matches) != 1 {
		return nil, dialerError("原始 dialer-proxy '" + name + "' 不存在、未启用或名称不唯一，请改用稳定引用")
	}
	return &matches[0], nil
}

// aggregationChildren is [('node', id) ...] + [('subscription', id) ...].
func aggregationChildren(obj *jsonx.Object) ([]Ref, error) {
	var children []Ref
	for _, source := range [][2]string{{"node", "nodes"}, {"subscription", "subscriptions"}} {
		list, err := compatList(obj.GetOr(source[1], []any{}))
		if err != nil {
			return nil, err
		}
		for _, id := range list {
			key, err := hashID(id)
			if err != nil {
				return nil, err
			}
			children = append(children, Ref{source[0], key})
		}
	}
	return children, nil
}

// chainExitDependencies is chain_exit_dependencies: exit candidate dialers
// are replaced; only nested chain structure is shared.
func chainExitDependencies(profile *jsonx.Object, ref any, objects *refObjects) ([]Ref, error) {
	graph := newRefGraph()
	var dependencies []Ref
	start, err := referenceKey(ref)
	if err != nil {
		return nil, err
	}
	pending := []Ref{start}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if graph.has(key) {
			continue
		}
		if key.Kind == "builtin" {
			return nil, dialerError("代理链落地组不能包含 DIRECT/REJECT 等无法施加前置代理的候选")
		}
		obj := objects.values[key]
		if obj == nil || obj.Len() == 0 || !compatEnabled(obj) {
			return nil, dialerError("代理链落地候选不存在或未启用")
		}
		var children []Ref
		switch {
		case key.Kind == "node":
			if jsonx.Truthy(obj.Value("subscription_id")) {
				return nil, dialerError("订阅节点请通过策略组 Provider 使用")
			}
		case key.Kind == "group" && jsonx.Equal(obj.Value("type"), "chain"):
			dependencies = append(dependencies, key)
		case key.Kind == "group":
			children, err = groupDependencies(profile, obj)
			if err != nil {
				return nil, err
			}
		case key.Kind == "aggregation":
			children, err = aggregationChildren(obj)
			if err != nil {
				return nil, err
			}
			if len(children) == 0 {
				return nil, dialerError("代理链落地聚合不能为空")
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

// dependencyGraph is dependency_graph.
func dependencyGraph(profile *jsonx.Object) (*Graph, error) {
	if err := validateShapes(profile, true); err != nil {
		return nil, err
	}
	if err := validateProxyChains(profile); err != nil {
		return nil, err
	}
	return dependencyGraphChecked(profile)
}

// dependencyGraphChecked is dependency_graph after validate_shapes and
// validate_proxy_chains, which are pure and have just passed in the caller.
func dependencyGraphChecked(profile *jsonx.Object) (*Graph, error) {
	objects, err := profileObjects(profile)
	if err != nil {
		return nil, err
	}
	graph := newRefGraph()
	var pending []Ref
	for _, key := range objects.keys {
		obj := objects.values[key]
		if !compatEnabled(obj) {
			continue
		}
		switch key.Kind {
		case "node":
			if obj.Value("dialer_ref") != nil {
				pending = append(pending, key)
				continue
			}
			target, err := rawDialer(obj)
			if err != nil {
				return nil, err
			}
			if target != nil {
				pending = append(pending, key)
			}
		case "group":
			if jsonx.Equal(obj.Value("type"), "chain") {
				pending = append(pending, key)
			}
		}
	}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if graph.has(key) || key.Kind == "builtin" {
			continue
		}
		obj := objects.values[key]
		if obj == nil || obj.Len() == 0 || !compatEnabled(obj) {
			return nil, dialerError("拨号依赖不存在或未启用")
		}
		var children []Ref
		switch {
		case key.Kind == "group" && jsonx.Equal(obj.Value("type"), "chain"):
			entry, err := chainEndpoint(obj, "entry")
			if err != nil {
				return nil, err
			}
			children = []Ref{entry}
			chain, _ := compatItem(obj, "chain")
			exit, err := compatItem(chain, "exit")
			if err != nil {
				return nil, err
			}
			dependencies, err := chainExitDependencies(profile, exit, objects)
			if err != nil {
				return nil, err
			}
			children = append(children, dependencies...)
		case key.Kind == "group":
			children, err = groupDependencies(profile, obj)
			if err != nil {
				return nil, err
			}
		case key.Kind == "subscription":
		case key.Kind == "aggregation":
			children, err = aggregationChildren(obj)
			if err != nil {
				return nil, err
			}
			if len(children) == 0 {
				return nil, dialerError("拨号策略组聚合来源不能为空")
			}
		default:
			target, err := dialerTarget(profile, obj)
			if err != nil {
				return nil, err
			}
			if target != nil {
				children = []Ref{*target}
			}
		}
		graph.set(key, children)
		pending = append(pending, children...)
	}
	if err := checkCycles(graph); err != nil {
		return nil, err
	}
	return graph, nil
}

// checkCycles is check_cycles: an iterative DFS with explicit enter/exit
// frames.
func checkCycles(graph *Graph) error {
	type frame struct {
		key     Ref
		exiting bool
	}
	visiting, done := map[Ref]bool{}, map[Ref]bool{}
	for _, root := range graph.keys {
		pending := []frame{{root, false}}
		for len(pending) > 0 {
			current := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if current.exiting {
				delete(visiting, current.key)
				done[current.key] = true
				continue
			}
			if visiting[current.key] {
				return dialerError("拨号代理依赖存在循环")
			}
			if done[current.key] {
				continue
			}
			visiting[current.key] = true
			pending = append(pending, frame{current.key, true})
			children := graph.children[current.key]
			for i := len(children) - 1; i >= 0; i-- {
				pending = append(pending, frame{children[i], false})
			}
		}
	}
	return nil
}
