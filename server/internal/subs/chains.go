package subs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
)

func lookupByID(items []*jsonx.Object) (map[string]*jsonx.Object, []string) {
	m := map[string]*jsonx.Object{}
	var order []string
	for _, o := range items {
		id := jsonx.Str(o.Value("id"))
		if _, ok := m[id]; !ok {
			order = append(order, id)
		}
		m[id] = o
	}
	return m, order
}

// ValidateProxyChains is validate_proxy_chains.
func ValidateProxyChains(profile *jsonx.Object) error {
	groups, groupOrder := lookupByID(objList(profile, "proxy_groups"))
	nodes, _ := lookupByID(objList(profile, "nodes"))
	chains := map[string]*jsonx.Object{}
	var chainOrder []string
	for _, id := range groupOrder {
		if groups[id].Value("type") == "chain" {
			chains[id] = groups[id]
			chainOrder = append(chainOrder, id)
		}
	}
	for _, gid := range groupOrder {
		group := groups[gid]
		if f, ok := group.Value("follow_group").(string); ok && chains[f] != nil {
			return dre("跟随模式不能引用代理链，请使用策略组引用")
		}
		if len(chains) > 0 && enabled(group) && group.Value("type") != "chain" {
			for _, m := range groupMembers(group) {
				if m.kind == "group" {
					if g, ok := groups[m.id]; !ok || !enabled(g) {
						return dre("策略组引用不存在或未启用")
					}
				}
			}
		}
	}
	for _, cid := range chainOrder {
		group := chains[cid]
		chain, ok := group.Value("chain").(*jsonx.Object)
		if !ok || !onlyKeys(chain, "entry", "exit") {
			return dre("代理链 chain 必须包含 entry 和 exit")
		}
		for _, endpoint := range []string{"entry", "exit"} {
			r, ok := chain.Value(endpoint).(*jsonx.Object)
			if !ok || !onlyKeys(r, "type", "id") || !(r.Value("type") == "node" || r.Value("type") == "group") ||
				!nonEmptyString(r.Value("id")) {
				return dre("代理链端点必须为 {type: node|group, id: 非空字符串}")
			}
			source := groups
			if r.Value("type") == "node" {
				source = nodes
			}
			target := source[r.GetString("id")]
			if target == nil || ((endpoint == "entry" || enabled(group)) && !enabled(target)) {
				return dre("代理链依赖不存在或未启用")
			}
			if r.Value("type") == "node" && jsonx.Truthy(target.Value("subscription_id")) {
				return dre("代理链仅支持共享手动节点，订阅请通过策略组使用")
			}
		}
		entry, exit := chain.GetObject("entry"), chain.GetObject("exit")
		if entry.Value("type") == "node" && jsonx.Equal(entry, exit) {
			return dre("代理链前置与落地不能是同一资源")
		}
		for _, k := range []string{"manual_nodes", "include_groups", "proxies_order", "proxies", "subscriptions",
			"aggregations", "use", "follow_group", "include_all", "include-all"} {
			if jsonx.Truthy(group.Value(k)) {
				return dre("代理链不能同时配置普通策略组来源")
			}
		}
	}
	return nil
}

func refObject(r ref) *jsonx.Object { return jsonx.Obj("type", r.kind, "id", r.id) }

func objRef(v any) ref {
	o, _ := v.(*jsonx.Object)
	return ref{jsonx.Str(o.Value("type")), jsonx.Str(o.Value("id"))}
}

func keyError(key string) error { return pyErr("KeyError: " + jsonx.Repr(key)) }

// clearObject empties an object in place (dict.clear()).
func clearObject(o *jsonx.Object) {
	for _, k := range o.Keys() {
		o.Delete(k)
	}
}

// appendUnique is `if id not in group.setdefault(field, []): append`.
func appendUnique(group *jsonx.Object, field, id string) {
	list, _ := group.SetDefault(field, []any{}).([]any)
	if !containsStr(list, id) {
		group.Set(field, append(list, id))
	}
}

// LowerProxyChains is lower_proxy_chains: independent Mihomo nodes and
// groups for the profile's chains. The profile is returned as is when no
// chain or node dialer is active; otherwise a private copy.
func LowerProxyChains(profile *jsonx.Object) (*jsonx.Object, error) {
	if err := ValidateDialers(profile); err != nil {
		return nil, err
	}
	active := false
	for _, g := range objList(profile, "proxy_groups") {
		if g.Value("type") == "chain" {
			active = true
		}
	}
	for _, n := range objList(profile, "nodes") {
		if n.Value("dialer_ref") != nil {
			active = true
		}
	}
	if !active {
		return profile, nil
	}
	runtime := profile.DeepCopy()
	groups, groupOrder := lookupByID(objList(runtime, "proxy_groups"))
	var providers []*jsonx.Object
	providers = append(providers, objList(runtime, "subscriptions")...)
	providers = append(providers, objList(runtime, "subscription_aggregations")...)
	seenNames := map[string]bool{}
	for _, p := range providers {
		if enabled(p) {
			name := jsonx.Str(p.Value("name"))
			if seenNames[name] {
				return nil, dre("订阅与聚合 Provider 名称必须唯一")
			}
			seenNames[name] = true
		}
	}
	for _, group := range objList(runtime, "proxy_groups") {
		original := group
		seen := map[string]bool{}
		for jsonx.Truthy(original.Value("follow_group")) {
			id := jsonx.Str(original.Value("id"))
			next := groups[jsonx.Str(original.Value("follow_group"))]
			if seen[id] || next == nil {
				return nil, dre("跟随策略组依赖不存在或存在循环")
			}
			seen[id] = true
			original = next
		}
		if original != group {
			identity := jsonx.NewObject()
			for _, k := range []string{"id", "name", "enabled"} {
				if group.Has(k) {
					identity.Set(k, group.Value(k))
				}
			}
			copied := original.DeepCopy()
			clearObject(group)
			group.Update(copied)
			group.Update(identity)
			group.Delete("follow_group")
		}
		if names, ok := group.Pop("use"); ok {
			list, _ := names.([]any)
			for _, name := range list {
				for _, key := range []string{"subscriptions", "subscription_aggregations"} {
					var source *jsonx.Object
					for _, p := range objList(runtime, key) {
						if p.Value("name") == name {
							source = p
							break
						}
					}
					if source != nil {
						field := "subscriptions"
						if key == "subscription_aggregations" {
							field = "aggregations"
						}
						appendUnique(group, field, jsonx.Str(source.Value("id")))
					}
				}
			}
		}
		for _, item := range group.GetList("proxies_order") {
			o := item.(*jsonx.Object)
			field := ""
			switch o.Value("type") {
			case "aggregation":
				field = "aggregations"
			case "subscription":
				field = "subscriptions"
			}
			if field != "" {
				appendUnique(group, field, jsonx.Str(o.Value("id")))
			}
		}
	}
	chains := map[string]*jsonx.Object{}
	var chainOrder []string
	for _, id := range groupOrder {
		if groups[id].Value("type") == "chain" {
			chains[id] = groups[id]
			chainOrder = append(chainOrder, id)
		}
	}
	if len(chains) == 0 {
		return runtime, nil
	}
	nodes, _ := lookupByID(objList(runtime, "nodes"))
	runtimeNodes := listField(runtime, "nodes")
	var runtimeGroups []any
	for _, g := range objList(runtime, "proxy_groups") {
		if g.Value("type") != "chain" {
			runtimeGroups = append(runtimeGroups, g)
		}
	}
	occupiedIDs := map[string]bool{}
	occupiedNames := map[string]bool{}
	for id, n := range nodes {
		occupiedIDs[id] = true
		occupiedNames[jsonx.Str(n.Value("name"))] = true
	}
	for id, g := range groups {
		occupiedIDs[id] = true
		occupiedNames[jsonx.Str(g.Value("name"))] = true
	}
	results := map[string]ref{}

	privateID := func(seed string) string {
		sum := sha256.Sum256([]byte(seed))
		value := "__chain__" + hex.EncodeToString(sum[:])[:20]
		for occupiedIDs[value] {
			value += "_"
		}
		occupiedIDs[value] = true
		return value
	}
	privateName := func(base string) string {
		name, suffix := base, 2
		for occupiedNames[name] {
			name = fmt.Sprintf("%s (%d)", base, suffix)
			suffix++
		}
		occupiedNames[name] = true
		return name
	}

	var buildChain func(id string) (ref, error)
	reference := func(r ref) (ref, error) {
		if r.kind == "group" && chains[r.id] != nil {
			return buildChain(r.id)
		}
		return r, nil
	}
	var clone func(r ref, dialer ref, seed, scope, name string) (ref, error)
	clone = func(r ref, dialer ref, seed, scope, name string) (ref, error) {
		id := privateID(seed)
		source := groups
		if r.kind == "node" {
			source = nodes
		}
		original := source[r.id]
		if original == nil {
			return ref{}, keyError(r.id)
		}
		if name == "" {
			name = privateName(scope + "_" + jsonx.Str(original.Value("name")))
		}
		if r.kind == "node" {
			obj := original.DeepCopy()
			obj.Set("id", id)
			obj.Set("name", name)
			obj.Set("enabled", true)
			obj.Set("dialer_ref", refObject(dialer))
			obj.Set("_proxy_chain_id", seed)
			runtimeNodes = append(runtimeNodes, obj)
			return ref{"node", id}, nil
		}
		if original.Value("type") == "chain" {
			chain := original.GetObject("chain")
			entry, err := clone(objRef(chain.Value("entry")), dialer, seed+"/entry", scope, "")
			if err != nil {
				return ref{}, err
			}
			return clone(objRef(chain.Value("exit")), entry, seed+"/exit", scope, name)
		}
		seen := map[string]bool{}
		for jsonx.Truthy(original.Value("follow_group")) {
			oid := jsonx.Str(original.Value("id"))
			if seen[oid] {
				return ref{}, dre("跟随策略组依赖存在循环")
			}
			seen[oid] = true
			next := groups[jsonx.Str(original.Value("follow_group"))]
			if next == nil {
				return ref{}, keyError(jsonx.Str(original.Value("follow_group")))
			}
			original = next
		}
		obj := original.DeepCopy()
		obj.Set("id", id)
		obj.Set("name", name)
		obj.Set("enabled", true)
		obj.Set("_chain_dialer", refObject(dialer))
		obj.Delete("follow_group")
		members := groupMembers(original)
		if !jsonx.Truthy(original.Value("proxies_order")) && original.Value("proxy_order") == "strategies_first" {
			var first, rest []ref
			for _, m := range members {
				if m.kind == "group" {
					first = append(first, m)
				} else {
					rest = append(rest, m)
				}
			}
			members = append(first, rest...)
		}
		var order []any
		originals := jsonx.NewObject()
		var nodeIDs, strategyIDs []any
		for index, m := range members {
			if m.kind == "builtin" {
				return ref{}, dre("代理链落地组不能包含 DIRECT/REJECT 等非代理候选")
			}
			candidate, err := clone(ref{m.kind, m.id}, dialer, fmt.Sprintf("%s/%d", seed, index), scope, "")
			if err != nil {
				return ref{}, err
			}
			kind := "strategy"
			if candidate.kind == "node" {
				kind = "node"
				nodeIDs = append(nodeIDs, candidate.id)
			} else {
				strategyIDs = append(strategyIDs, candidate.id)
			}
			order = append(order, jsonx.Obj("type", kind, "id", candidate.id))
			src := groups
			if m.kind == "node" {
				src = nodes
			}
			oc := src[m.id]
			if oc == nil {
				return ref{}, keyError(m.id)
			}
			originals.Set(candidate.id, jsonx.Obj("name", oc.Value("name")))
		}
		obj.Set("manual_nodes", orEmpty(nodeIDs))
		obj.Set("include_groups", orEmpty(strategyIDs))
		extra := orEmpty(order)
		for _, i := range original.GetList("proxies_order") {
			o := i.(*jsonx.Object)
			if o.Value("type") != "node" && o.Value("type") != "strategy" {
				extra = append(extra, jsonx.DeepCopy(o))
			}
		}
		obj.Set("proxies_order", extra)
		obj.Set("_chain_original_candidates", originals)
		obj.Delete("proxies")
		runtimeGroups = append(runtimeGroups, obj)
		return ref{"group", id}, nil
	}
	buildChain = func(id string) (ref, error) {
		if r, ok := results[id]; ok {
			return r, nil
		}
		chain := chains[id]
		c := chain.GetObject("chain")
		entry, err := reference(objRef(c.Value("entry")))
		if err != nil {
			return ref{}, err
		}
		name := jsonx.Str(chain.Value("name"))
		r, err := clone(objRef(c.Value("exit")), entry, id, name, name)
		if err != nil {
			return ref{}, err
		}
		results[id] = r
		return r, nil
	}

	for _, id := range chainOrder {
		if enabled(chains[id]) {
			if _, err := buildChain(id); err != nil {
				return nil, err
			}
		}
	}
	for i := 0; i < len(runtimeNodes); i++ {
		node := runtimeNodes[i].(*jsonx.Object)
		if node.Value("dialer_ref") != nil {
			r, err := reference(objRef(node.Value("dialer_ref")))
			if err != nil {
				return nil, err
			}
			node.Set("dialer_ref", refObject(r))
		}
	}
	for i := 0; i < len(runtimeGroups); i++ {
		group := runtimeGroups[i].(*jsonx.Object)
		if jsonx.Truthy(group.Value("_chain_dialer")) {
			continue
		}
		includes := group.GetList("include_groups")
		legacy := true
		for _, k := range []string{"manual_nodes", "include_groups", "subscriptions", "aggregations"} {
			if jsonx.Truthy(group.Value(k)) {
				legacy = false
			}
		}
		if legacy && group.Value("source") == "strategy" {
			includes = group.GetList("proxies")
		}
		hasChain := false
		for _, id := range includes {
			if chains[jsonx.Str(id)] != nil {
				hasChain = true
			}
		}
		if !jsonx.Truthy(group.Value("proxies_order")) && hasChain {
			var nodeOrder, groupOrder []any
			for _, id := range group.GetList("manual_nodes") {
				nodeOrder = append(nodeOrder, jsonx.Obj("type", "node", "id", id))
			}
			for _, id := range includes {
				groupOrder = append(groupOrder, jsonx.Obj("type", "strategy", "id", id))
			}
			if group.Value("proxy_order") == "strategies_first" {
				group.Set("proxies_order", append(orEmpty(groupOrder), nodeOrder...))
			} else {
				group.Set("proxies_order", append(orEmpty(nodeOrder), groupOrder...))
			}
		}
		for _, item := range group.GetList("proxies_order") {
			o := item.(*jsonx.Object)
			if o.Value("type") == "strategy" {
				if r, ok := results[jsonx.Str(o.Value("id"))]; ok {
					kind := "strategy"
					if r.kind == "node" {
						kind = "node"
					}
					o.Set("type", kind)
					o.Set("id", r.id)
				}
			}
		}
		resolved := []any{}
		for _, idv := range includes {
			id := jsonx.Str(idv)
			r, inResults := results[id]
			if chains[id] != nil && !(inResults && r.kind == "group") {
				continue
			}
			if inResults && r.kind == "group" {
				resolved = append(resolved, r.id)
			} else {
				resolved = append(resolved, idv)
			}
		}
		group.Set("include_groups", resolved)
		if legacy && group.Value("source") == "strategy" {
			group.Set("proxies", append([]any{}, resolved...))
		}
	}
	runtime.Set("proxy_groups", orEmpty(runtimeGroups))
	runtime.Set("nodes", orEmpty(runtimeNodes))
	return runtime, nil
}

func orEmpty(l []any) []any {
	if l == nil {
		return []any{}
	}
	return l
}

// LowerChainProviders is lower_chain_providers: Mihomo filters original names
// before applying provider overrides. runtime is the lowered profile, main
// the generated Mihomo document (modified in place).
func LowerChainProviders(runtime, main *jsonx.Object) error {
	providers := main.GetObject("proxy-providers")
	if providers == nil {
		providers = jsonx.NewObject()
		main.Set("proxy-providers", providers)
	}
	groups := map[string]*jsonx.Object{}
	for _, g := range objList(main, "proxy-groups") {
		groups[jsonx.Str(g.Value("name"))] = g
	}
	proxies := map[string]*jsonx.Object{}
	for _, p := range objList(main, "proxies") {
		proxies[jsonx.Str(p.Value("name"))] = p
	}
	occupied := map[string]bool{}
	for _, k := range providers.Keys() {
		occupied[k] = true
	}
	for k := range groups {
		occupied[k] = true
	}
	for k := range proxies {
		occupied[k] = true
	}
	for _, source := range objList(runtime, "proxy_groups") {
		if !jsonx.Truthy(source.Value("_chain_dialer")) {
			continue
		}
		sname := jsonx.Str(source.Value("name"))
		group := groups[sname]
		if group == nil {
			return keyError(sname)
		}
		filterValueAny, _ := group.Pop("filter")
		filterValue := ""
		if filterValueAny != nil {
			s, ok := filterValueAny.(string)
			if !ok {
				return pyErr("filter is not a string")
			}
			filterValue = s
		}
		excluded := jsonx.NewObject()
		for _, key := range []string{"exclude-filter", "exclude-type"} {
			if v, ok := group.Pop(key); ok {
				excluded.Set(key, v)
			}
		}
		var use []any
		var patterns []string
		var captures []int
		if strings.Contains(filterValue, "`") {
			patterns = strings.Split(filterValue, "`")
		}
		const orderTag = "排序_"
		var static []Candidate
		originals := source.GetObject("_chain_original_candidates")
		for _, id := range originals.Keys() {
			original := originals.GetObject(id)
			refType := "group"
			for _, n := range objList(runtime, "nodes") {
				if n.Value("id") == any(id) {
					refType = "node"
					break
				}
			}
			emitted, err := resolveDialer(runtime, jsonx.Obj("dialer_ref", jsonx.Obj("type", refType, "id", id)))
			if err != nil {
				return err
			}
			table := groups
			if refType == "node" {
				table = proxies
			}
			converted := table[emitted]
			if converted == nil {
				return keyError(emitted)
			}
			static = append(static, Candidate{Name: jsonx.Str(original.Value("name")), Type: jsonx.Str(converted.Value("type")), Raw: emitted})
		}
		excludeText, _ := excluded.Value("exclude-filter").(string)
		excludeType, _ := excluded.Value("exclude-type").(string)
		var err error
		if static, err = filterProviderCandidates(static, "", excludeText, excludeType); err != nil {
			return err
		}
		if jsonx.Truthy(group.Value("proxies")) {
			allowed := map[string]bool{}
			for _, p := range static {
				allowed[p.Raw.(string)] = true
			}
			kept := []any{}
			for _, n := range group.GetList("proxies") {
				if allowed[jsonx.Str(n)] {
					kept = append(kept, n)
				}
			}
			group.Set("proxies", kept)
		}
		for index, originalName := range group.GetList("use") {
			base := sname + "_" + jsonx.Str(originalName)
			name, suffix := base, 2
			for occupied[name] {
				name = fmt.Sprintf("%s (%d)", base, suffix)
				suffix++
			}
			occupied[name] = true
			src := providers.GetObject(jsonx.Str(originalName))
			if src == nil {
				return keyError(jsonx.Str(originalName))
			}
			provider := src.DeepCopy()
			provider.Set("path", fmt.Sprintf("./providers/%s_provider_%d.yaml", jsonx.Str(source.Value("id")), index))
			dialerName, err := resolveDialer(runtime, jsonx.Obj("dialer_ref", source.Value("_chain_dialer")))
			if err != nil {
				return err
			}
			override := jsonx.Obj("additional-prefix", name+"_", "dialer-proxy", dialerName)
			provider.Set("override", override)
			if filterValue != "" {
				provider.Set("filter", filterValue)
			}
			if len(patterns) > 0 {
				parts := make([]string, len(patterns))
				for i, p := range patterns {
					parts[i] = "(?:" + p + ")"
				}
				provider.Set("filter", strings.Join(parts, "|"))
				if captures == nil {
					for _, pattern := range patterns {
						compiled, err := rules.CompilePyRegex(pattern, false)
						if err != nil {
							return fmt.Errorf("compile chain provider filter: %w", err)
						}
						captures = append(captures, compiled.Groups())
					}
				}
				rules := []any{}
				for rank, pattern := range patterns {
					rules = append(rules, jsonx.Obj(
						"pattern", fmt.Sprintf(`^(?!%s)(?=[\s\S]*?(?:%s))([\s\S]*)$`, pyReEscape(orderTag), pattern),
						"target", fmt.Sprintf("%s%d_$%d", orderTag, rank+1, captures[rank]+1)))
				}
				override.Set("proxy-name", rules)
			}
			for _, key := range excluded.Keys() {
				value := excluded.Value(key)
				if key == "exclude-filter" && jsonx.Truthy(provider.Value(key)) {
					if jsonx.Truthy(value) {
						provider.Set(key, jsonx.Str(provider.Value(key))+"`"+jsonx.Str(value))
					}
				} else {
					provider.Set(key, value)
				}
			}
			providers.Set(name, provider)
			use = append(use, name)
		}
		if len(use) > 0 {
			group.Set("use", use)
		}
		if len(patterns) > 0 && len(use) > 0 {
			ordered := make([]string, 0, len(patterns))
			for rank, pattern := range patterns {
				var dynamic []string
				for _, n := range use {
					dynamic = append(dynamic, "^"+namePattern(jsonx.Str(n)+"_"+orderTag+fmt.Sprint(rank+1)+"_"))
				}
				matching, err := filterProviderCandidates(static, pattern, "", "")
				if err != nil {
					return err
				}
				for _, p := range matching {
					dynamic = append(dynamic, "^"+namePattern(p.Raw.(string))+"$")
				}
				ordered = append(ordered, strings.Join(dynamic, "|"))
			}
			group.Set("filter", strings.Join(ordered, "`"))
		}
	}
	if providers.Len() == 0 {
		main.Delete("proxy-providers")
	}
	return nil
}
