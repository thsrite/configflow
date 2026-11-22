package subs

import (
	"configflow/internal/jsondecode"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func groupList(config *model.Config) []model.ProxyGroup {
	if config == nil || config.Profile == nil {
		return nil
	}
	return config.Profile.ProxyGroups
}
func textValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func textList(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}
func orderedRefs(value *[]model.ResourceRef) []model.ResourceRef {
	if value == nil {
		return nil
	}
	return *value
}
func setGroupStrings(group *model.ProxyGroup, field string, values []string) error {
	data, err := jsonresp.Encode(values)
	if err != nil {
		return err
	}
	return group.SetJSON(field, data)
}
func setGroupOrder(group *model.ProxyGroup, values []model.ResourceRef) error {
	data, err := model.Marshal(values)
	if err != nil {
		return err
	}
	return group.SetJSON("proxies_order", data)
}
func appendGroupID(group *model.ProxyGroup, name string, field *[]string, id string) error {
	if field != nil && slices.Contains(*field, id) {
		return nil
	}
	list := []string{}
	if field != nil {
		list = append(list, (*field)...)
	}
	list = append(list, id)
	return setGroupStrings(group, name, list)
}
func typedRef(ref DependencyRef) *model.ResourceRef {
	return &model.ResourceRef{Type: ref.Kind, ID: ref.ID}
}
func dependencyRef(ref model.ResourceRef) DependencyRef {
	return DependencyRef{Kind: ref.Type, ID: ref.ID}
}

// ChainResourceError identifies an unresolved generated resource, with native text.
type ChainResourceError struct{ ID string }

func (e *ChainResourceError) Error() string { return fmt.Sprintf(msgChainMissing, e.ID) }

// ValidateChains delegates every shape and dependency decision to the shared owner.
func ValidateChains(config *model.Config) error {
	ops := sharedDialers()
	if ops.ValidateChains == nil {
		return &CollaborationError{Operation: "chain validation"}
	}
	return ops.ValidateChains(config)
}

// LowerChains returns a copy-on-write runtime view of stable chains. Unknown
// records stay immutable; only cloned groups/nodes and owned private metadata
// change. It never serializes or decodes the configuration inside resource loops.
func LowerChains(config *model.Config) (*model.Config, error) {
	ops := sharedDialers()
	if ops.Validate == nil || ops.Members == nil {
		return nil, &CollaborationError{Operation: "chain lowering"}
	}
	if err := ops.Validate(config); err != nil {
		return nil, referenceFailure(err)
	}
	active := false
	for _, group := range groupList(config) {
		active = active || group.Type == "chain"
	}
	if config != nil {
		for _, node := range config.Nodes {
			active = active || node.DialerRef != nil
		}
	}
	if !active {
		return config, nil
	}
	runtime := *config
	profile := model.Profile{}
	if config.Profile != nil {
		profile = *config.Profile
	}
	profile.ProxyGroups = slices.Clone(profile.ProxyGroups)
	runtime.Profile = &profile
	runtime.Nodes = slices.Clone(config.Nodes)
	groups := map[string]*model.ProxyGroup{}
	var groupOrder []string
	for i := range profile.ProxyGroups {
		group := &profile.ProxyGroups[i]
		if _, ok := groups[group.ID]; !ok {
			groupOrder = append(groupOrder, group.ID)
		}
		groups[group.ID] = group
	}
	names := map[string]bool{}
	for _, sub := range config.Subscriptions {
		if resourceEnabled(sub.Enabled) {
			if names[sub.Name] {
				return nil, dre(msgChainProviderNames)
			}
			names[sub.Name] = true
		}
	}
	for _, agg := range config.Aggregations {
		if resourceEnabled(agg.Enabled) {
			if names[agg.Name] {
				return nil, dre(msgChainProviderNames)
			}
			names[agg.Name] = true
		}
	}
	for i := range profile.ProxyGroups {
		group := &profile.ProxyGroups[i]
		original := group
		seen := map[string]bool{}
		for textValue(original.FollowGroup) != "" {
			next := groups[textValue(original.FollowGroup)]
			if seen[original.ID] || next == nil {
				return nil, dre(msgChainFollowMissing)
			}
			seen[original.ID] = true
			original = next
		}
		if original != group {
			// Preserve only the follower's owned identity, including stored null/zero
			// presence. This is one group-record projection, never a config round trip.
			identityJSON, err := model.Marshal(group)
			if err != nil {
				return nil, err
			}
			var identity struct {
				ID      json.RawMessage `json:"id"`
				Name    json.RawMessage `json:"name"`
				Enabled json.RawMessage `json:"enabled"`
			}
			if err := jsondecode.Unmarshal(identityJSON, &identity); err != nil {
				return nil, err
			}
			*group = *original
			for _, member := range []struct {
				name  string
				value json.RawMessage
			}{{"id", identity.ID}, {"name", identity.Name}, {"enabled", identity.Enabled}} {
				if member.value != nil {
					if err := group.SetJSON(member.name, member.value); err != nil {
						return nil, err
					}
				}
			}
			if err := group.Clear("follow_group"); err != nil {
				return nil, err
			}
		}
		for _, name := range textList(group.Use) {
			for _, sub := range config.Subscriptions {
				if sub.Name == name {
					if err := appendGroupID(group, "subscriptions", group.Subscriptions, sub.ID); err != nil {
						return nil, err
					}
					break
				}
			}
			for _, agg := range config.Aggregations {
				if agg.Name == name {
					if err := appendGroupID(group, "aggregations", group.Aggregations, agg.ID); err != nil {
						return nil, err
					}
					break
				}
			}
		}
		if err := group.Clear("use"); err != nil {
			return nil, err
		}
		for _, ref := range orderedRefs(group.ProxiesOrder) {
			switch ref.Type {
			case "aggregation":
				if err := appendGroupID(group, "aggregations", group.Aggregations, ref.ID); err != nil {
					return nil, err
				}
			case "subscription":
				if err := appendGroupID(group, "subscriptions", group.Subscriptions, ref.ID); err != nil {
					return nil, err
				}
			}
		}
	}
	chains := map[string]*model.ProxyGroup{}
	var chainOrder []string
	for _, id := range groupOrder {
		if groups[id].Type == "chain" {
			chains[id] = groups[id]
			chainOrder = append(chainOrder, id)
		}
	}
	if len(chains) == 0 {
		return &runtime, nil
	}
	nodes := map[string]*model.Node{}
	for i := range runtime.Nodes {
		node := &runtime.Nodes[i]
		nodes[node.ID] = node
	}
	runtimeNodes := append([]model.Node{}, runtime.Nodes...)
	runtimeGroups := []model.ProxyGroup{}
	for _, group := range profile.ProxyGroups {
		if group.Type != "chain" {
			runtimeGroups = append(runtimeGroups, group)
		}
	}
	occupiedIDs, occupiedNames := map[string]bool{}, map[string]bool{}
	for id, node := range nodes {
		occupiedIDs[id] = true
		occupiedNames[node.Name] = true
	}
	for id, group := range groups {
		occupiedIDs[id] = true
		occupiedNames[group.Name] = true
	}
	privateID := func(seed string) string {
		sum := sha256.Sum256([]byte(seed))
		id := "__chain__" + hex.EncodeToString(sum[:])[:20]
		for occupiedIDs[id] {
			id += "_"
		}
		occupiedIDs[id] = true
		return id
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
	results := map[string]DependencyRef{}
	var build func(string) (DependencyRef, error)
	reference := func(ref DependencyRef) (DependencyRef, error) {
		if ref.Kind == "group" && chains[ref.ID] != nil {
			return build(ref.ID)
		}
		return ref, nil
	}
	var clone func(DependencyRef, DependencyRef, string, string, string) (DependencyRef, error)
	clone = func(ref, dialer DependencyRef, seed, scope, name string) (DependencyRef, error) {
		id := privateID(seed)
		if ref.Kind == "node" {
			original := nodes[ref.ID]
			if original == nil {
				return DependencyRef{}, &ChainResourceError{ID: ref.ID}
			}
			if name == "" {
				name = privateName(scope + "_" + original.Name)
			}
			node := *original
			node.ID = id
			node.Name = name
			yes := true
			node.Enabled = &yes
			node.DialerRef = typedRef(dialer)
			node.ProxyChainID = seed
			runtimeNodes = append(runtimeNodes, node)
			return DependencyRef{Kind: "node", ID: id}, nil
		}
		original := groups[ref.ID]
		if original == nil {
			return DependencyRef{}, &ChainResourceError{ID: ref.ID}
		}
		if name == "" {
			name = privateName(scope + "_" + original.Name)
		}
		if original.Type == "chain" {
			if !chainEndpoints(original.Chain) {
				return DependencyRef{}, dre(msgChainEndpoints)
			}
			entry, err := clone(dependencyRef(original.Chain.Entry), dialer, seed+"/entry", scope, "")
			if err != nil {
				return DependencyRef{}, err
			}
			return clone(dependencyRef(original.Chain.Exit), entry, seed+"/exit", scope, name)
		}
		seen := map[string]bool{}
		for textValue(original.FollowGroup) != "" {
			if seen[original.ID] {
				return DependencyRef{}, dre(msgChainFollowCycle)
			}
			seen[original.ID] = true
			nextID := textValue(original.FollowGroup)
			original = groups[nextID]
			if original == nil {
				return DependencyRef{}, &ChainResourceError{ID: nextID}
			}
		}
		group := *original
		group.ID = id
		group.Name = name
		yes := true
		group.Enabled = &yes
		group.ChainDialer = typedRef(dialer)
		if err := group.Clear("follow_group"); err != nil {
			return DependencyRef{}, err
		}
		// Seal this cloned record before appending declared candidate fields. The
		// shared codec appends new opaque fields after declared ones; this bounded
		// group checkpoint retains the historical _chain_dialer member position.
		checkpoint, err := model.Marshal(group)
		if err != nil {
			return DependencyRef{}, err
		}
		if err := json.Unmarshal(checkpoint, &group); err != nil {
			return DependencyRef{}, err
		}
		members := ops.Members(*original)
		if len(orderedRefs(original.ProxiesOrder)) == 0 && textValue(original.ProxyOrder) == "strategies_first" {
			var first, rest []DependencyRef
			for _, ref := range members {
				if ref.Kind == "group" {
					first = append(first, ref)
				} else {
					rest = append(rest, ref)
				}
			}
			members = append(first, rest...)
		}
		order := []model.ResourceRef{}
		originals := yamlx.Map()
		nodeIDs, strategyIDs := []string{}, []string{}
		for index, member := range members {
			if member.Kind == "builtin" {
				return DependencyRef{}, dre(msgChainBuiltinExit)
			}
			candidate, err := clone(member, dialer, fmt.Sprintf("%s/%d", seed, index), scope, "")
			if err != nil {
				return DependencyRef{}, err
			}
			kind := "strategy"
			if candidate.Kind == "node" {
				kind = "node"
				nodeIDs = append(nodeIDs, candidate.ID)
			} else {
				strategyIDs = append(strategyIDs, candidate.ID)
			}
			order = append(order, model.ResourceRef{Type: kind, ID: candidate.ID})
			originalName := ""
			if member.Kind == "node" {
				if nodes[member.ID] == nil {
					return DependencyRef{}, &ChainResourceError{ID: member.ID}
				}
				originalName = nodes[member.ID].Name
			} else {
				if groups[member.ID] == nil {
					return DependencyRef{}, &ChainResourceError{ID: member.ID}
				}
				originalName = groups[member.ID].Name
			}
			originals.Content = append(originals.Content, yamlx.Str(candidate.ID), yamlx.Map(yamlx.Str("name"), yamlx.Str(originalName)))
		}
		if err := setGroupStrings(&group, "manual_nodes", nodeIDs); err != nil {
			return DependencyRef{}, err
		}
		if err := setGroupStrings(&group, "include_groups", strategyIDs); err != nil {
			return DependencyRef{}, err
		}
		for _, item := range orderedRefs(original.ProxiesOrder) {
			if item.Type != "node" && item.Type != "strategy" {
				order = append(order, item)
			}
		}
		if err := setGroupOrder(&group, order); err != nil {
			return DependencyRef{}, err
		}
		data, err := yamlx.ToJSON(originals)
		if err != nil {
			return DependencyRef{}, err
		}
		if err := group.ChainOriginalCandidates.UnmarshalJSON(data); err != nil {
			return DependencyRef{}, err
		}
		if err := group.Clear("proxies"); err != nil {
			return DependencyRef{}, err
		}
		runtimeGroups = append(runtimeGroups, group)
		return DependencyRef{Kind: "group", ID: id}, nil
	}
	build = func(id string) (DependencyRef, error) {
		if result, ok := results[id]; ok {
			return result, nil
		}
		chain := chains[id]
		if chain == nil || !chainEndpoints(chain.Chain) {
			return DependencyRef{}, dre(msgChainEndpoints)
		}
		entry, err := reference(dependencyRef(chain.Chain.Entry))
		if err != nil {
			return DependencyRef{}, err
		}
		result, err := clone(dependencyRef(chain.Chain.Exit), entry, id, chain.Name, chain.Name)
		if err != nil {
			return DependencyRef{}, err
		}
		results[id] = result
		return result, nil
	}
	for _, id := range chainOrder {
		if c := chains[id]; resourceEnabled(c.Enabled) {
			if _, err := build(id); err != nil {
				return nil, err
			}
		}
	}
	for i := range runtimeNodes {
		node := &runtimeNodes[i]
		if node.DialerRef != nil {
			ref, err := reference(dependencyRef(*node.DialerRef))
			if err != nil {
				return nil, err
			}
			node.DialerRef = typedRef(ref)
		}
	}
	for i := range runtimeGroups {
		group := &runtimeGroups[i]
		if group.ChainDialer != nil {
			continue
		}
		includes := textList(group.IncludeGroups)
		legacy := len(textList(group.ManualNodes))+len(textList(group.IncludeGroups))+len(textList(group.Subscriptions))+len(textList(group.Aggregations)) == 0
		if legacy && textValue(group.Source) == "strategy" {
			includes = textList(group.Proxies)
		}
		hasChain := false
		for _, id := range includes {
			hasChain = hasChain || chains[id] != nil
		}
		if len(orderedRefs(group.ProxiesOrder)) == 0 && hasChain {
			nodeOrder, groupOrder := []model.ResourceRef{}, []model.ResourceRef{}
			for _, id := range textList(group.ManualNodes) {
				nodeOrder = append(nodeOrder, model.ResourceRef{Type: "node", ID: id})
			}
			for _, id := range includes {
				groupOrder = append(groupOrder, model.ResourceRef{Type: "strategy", ID: id})
			}
			order := []model.ResourceRef{}
			if textValue(group.ProxyOrder) == "strategies_first" {
				order = append(append(order, groupOrder...), nodeOrder...)
			} else {
				order = append(append(order, nodeOrder...), groupOrder...)
			}
			if err := setGroupOrder(group, order); err != nil {
				return nil, err
			}
		}
		if group.ProxiesOrder != nil {
			order := slices.Clone(*group.ProxiesOrder)
			for j := range order {
				if order[j].Type == "strategy" {
					if result, ok := results[order[j].ID]; ok {
						order[j].Type = "strategy"
						if result.Kind == "node" {
							order[j].Type = "node"
						}
						order[j].ID = result.ID
					}
				}
			}
			if err := setGroupOrder(group, order); err != nil {
				return nil, err
			}
		}
		resolved := []string{}
		for _, id := range includes {
			result, ok := results[id]
			if chains[id] != nil && !(ok && result.Kind == "group") {
				continue
			}
			if ok && result.Kind == "group" {
				resolved = append(resolved, result.ID)
			} else {
				resolved = append(resolved, id)
			}
		}
		if err := setGroupStrings(group, "include_groups", resolved); err != nil {
			return nil, err
		}
		if legacy && textValue(group.Source) == "strategy" {
			copy := slices.Clone(resolved)
			if copy == nil {
				copy = []string{}
			}
			if err := setGroupStrings(group, "proxies", copy); err != nil {
				return nil, err
			}
		}
	}
	runtime.Nodes = runtimeNodes
	profile.ProxyGroups = runtimeGroups
	return &runtime, nil
}

// chainEndpoints reports a chain with both endpoints. The chain is a plain
// record (O18 T1c): a stored chain without entry or exit reads it as the zero
// reference, which Python refused as missing.
func chainEndpoints(chain *model.ProxyChain) bool {
	return chain != nil && chain.Entry != (model.ResourceRef{}) && chain.Exit != (model.ResourceRef{})
}
