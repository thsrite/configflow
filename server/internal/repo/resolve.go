package repo

import (
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/model"
)

// Typed resolution of a profile's composition (N8-5b): which shared
// resources a profile selects, the resolved Config and resource usages.
//
// Reads use these functions; a member with a wrong JSON type reads as its
// zero value (R5), so a stored value that breaks a type rule does not prevent
// reading. References that point nowhere are still errors, with the messages
// write validation uses.

// Resource kinds selected by a profile's composition.
const (
	kindSubscriptions = "subscriptions"
	kindNodes         = "nodes"
	kindAggregations  = "subscription_aggregations"
	kindRuleLibrary   = "rule_library"
)

// catalog indexes records by id: first position, last value (like a dict).
type catalog[T any] struct {
	keys []string
	byID map[string]*T
}

func catalogOf[T any](items []T, id func(*T) string) catalog[T] {
	result := catalog[T]{byID: map[string]*T{}}
	for index := range items {
		item := &items[index]
		key := id(item)
		if _, ok := result.byID[key]; !ok {
			result.keys = append(result.keys, key)
		}
		result.byID[key] = item
	}
	return result
}

func (c catalog[T]) has(id string) bool { _, ok := c.byID[id]; return ok }

// catalogs are the shared resource catalogs a profile selects from.
type catalogs struct {
	subscriptions catalog[model.Subscription]
	nodes         catalog[model.Node]
	aggregations  catalog[model.Aggregation]
}

func catalogsOf(shared *model.Shared) catalogs {
	return catalogs{
		subscriptions: catalogOf(shared.Subscriptions, func(s *model.Subscription) string { return s.ID }),
		nodes:         catalogOf(shared.Nodes, func(n *model.Node) string { return n.ID }),
		aggregations:  catalogOf(shared.Aggregations, func(a *model.Aggregation) string { return a.ID }),
	}
}

func (c catalogs) has(kind, id string) bool {
	switch kind {
	case kindSubscriptions:
		return c.subscriptions.has(id)
	case kindNodes:
		return c.nodes.has(id)
	case kindAggregations:
		return c.aggregations.has(id)
	}
	return false
}

func (c catalogs) keys(kind string) []string {
	switch kind {
	case kindSubscriptions:
		return c.subscriptions.keys
	case kindNodes:
		return c.nodes.keys
	case kindAggregations:
		return c.aggregations.keys
	}
	return nil
}

// selection is the set of resources selected per kind, in selection order.
type selection map[string]*orderedSet

func builtinPolicy(name string) bool { return name == "DIRECT" || name == "REJECT" }

// orderSourceKinds maps a proxies_order entry type (and a group's legacy
// source) to the resource kind it names.
var orderSourceKinds = map[string]string{"node": kindNodes, "subscription": kindSubscriptions, "aggregation": kindAggregations}

func derefStrings(values *[]string) []string {
	if values == nil {
		return nil
	}
	return *values
}

// selectResources is the resource closure of a profile's composition plus
// roots: group members, chain nodes, legacy group sources, proxies_order
// entries, aggregation members, node subscriptions and raw dialer targets.
func selectResources(profile *model.Profile, c catalogs, roots ResourceRoots) (selection, error) {
	selected := selection{}
	for _, kind := range ResourceFields {
		selected[kind] = newOrderedSet()
	}
	groupIDs := map[string]bool{}
	for _, group := range profile.ProxyGroups {
		if group.ID == "" {
			return nil, validationError("策略组 中每一项必须有 ID")
		}
		if groupIDs[group.ID] {
			return nil, validationError("策略组 存在重复 ID：" + group.ID)
		}
		groupIDs[group.ID] = true
	}
	choose := func(kind, id string, allowBuiltin bool) error {
		if strings.TrimSpace(id) == "" {
			return validationError("资源引用必须是非空 ID")
		}
		if allowBuiltin && kind == kindNodes && builtinPolicy(id) {
			return nil
		}
		if !c.has(kind, id) {
			return inUse("引用的共享资源不存在：" + kind + "/" + id)
		}
		selected[kind].add(id)
		return nil
	}
	for _, group := range profile.ProxyGroups {
		if group.Type == "chain" {
			if group.Chain == nil {
				return nil, validationError("代理链必须指定前置和落地引用")
			}
			for _, reference := range []model.ResourceRef{group.Chain.Entry, group.Chain.Exit} {
				if (reference.Type != "node" && reference.Type != "group") || strings.TrimSpace(reference.ID) == "" {
					return nil, validationError("代理链引用必须是 node/group type 与非空 id")
				}
				if reference.Type == "node" {
					if err := choose(kindNodes, reference.ID, false); err != nil {
						return nil, err
					}
				} else if !groupIDs[reference.ID] {
					return nil, inUse("代理链引用的策略组不存在：" + reference.ID)
				}
			}
			continue
		}
		members := []struct {
			kind   string
			values []string
		}{
			{kindSubscriptions, derefStrings(group.Subscriptions)},
			{kindNodes, derefStrings(group.ManualNodes)},
			{kindAggregations, derefStrings(group.Aggregations)},
		}
		if group.Source != nil {
			if kind := orderSourceKinds[*group.Source]; kind != "" {
				members = append(members, struct {
					kind   string
					values []string
				}{kind, derefStrings(group.Proxies)})
			}
		}
		for _, member := range members {
			for _, id := range member.values {
				if err := choose(member.kind, id, member.kind == kindNodes); err != nil {
					return nil, err
				}
			}
		}
		if group.ProxiesOrder != nil {
			for _, entry := range *group.ProxiesOrder {
				if kind := orderSourceKinds[entry.Type]; kind != "" {
					if err := choose(kind, entry.ID, kind == kindNodes); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	for _, kind := range sortedRootKinds(roots) {
		ids := roots[kind]
		if selected[kind] == nil {
			return nil, helperError(ClassKeyError, fmt.Sprintf("%q", kind))
		}
		if ids == nil {
			ids = c.keys(kind)
		}
		for _, id := range ids {
			if c.has(kind, id) {
				if err := choose(kind, id, false); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, aggregationID := range append([]string(nil), selected[kindAggregations].keys...) {
		aggregation := c.aggregations.byID[aggregationID]
		for _, member := range []struct {
			kind   string
			values []string
		}{{kindNodes, aggregation.Nodes}, {kindSubscriptions, aggregation.Subscriptions}} {
			for _, id := range member.values {
				if err := choose(member.kind, id, false); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := selectDialerTargets(profile, c, selected, choose); err != nil {
		return nil, err
	}
	return selected, nil
}

// sortedRootKinds visits root kinds in ResourceFields order, so the
// selection does not depend on map iteration.
func sortedRootKinds(roots ResourceRoots) []string {
	var kinds []string
	for _, kind := range ResourceFields {
		if _, ok := roots[kind]; ok {
			kinds = append(kinds, kind)
		}
	}
	for kind := range roots {
		known := false
		for _, field := range ResourceFields {
			known = known || field == kind
		}
		if !known {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// selectDialerTargets follows the raw dialer-proxy of every selected enabled
// node to the node (selected too) or group it names, and selects the
// subscription of every selected node.
func selectDialerTargets(profile *model.Profile, c catalogs, selected selection, choose func(kind, id string, allowBuiltin bool) error) error {
	nodesByName := map[string][]string{}
	for _, id := range c.nodes.keys {
		name := c.nodes.byID[id].Name
		nodesByName[name] = append(nodesByName[name], id)
	}
	groupsByName := map[string]int{}
	for _, group := range profile.ProxyGroups {
		groupsByName[group.Name]++
	}
	pending := append([]string(nil), selected[kindNodes].keys...)
	visited := map[string]bool{}
	for len(pending) > 0 {
		nodeID := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[nodeID] {
			continue
		}
		visited[nodeID] = true
		node := c.nodes.byID[nodeID]
		if node.SubscriptionID != "" {
			if err := choose(kindSubscriptions, node.SubscriptionID, false); err != nil {
				return err
			}
		}
		if node.Enabled != nil && !*node.Enabled {
			continue
		}
		target, err := nodeRawDialer(node)
		if err != nil {
			return err
		}
		if target == nil {
			continue
		}
		name, ok := target.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return validationError("原始 dialer-proxy 必须是非空名称")
		}
		if builtinPolicy(name) {
			continue
		}
		nodeTargets := nodesByName[name]
		if len(nodeTargets)+groupsByName[name] != 1 {
			return inUse("原始拨号代理目标不存在或名称不唯一：" + name)
		}
		if len(nodeTargets) > 0 {
			if err := choose(kindNodes, nodeTargets[0], false); err != nil {
				return err
			}
			pending = append(pending, nodeTargets[0])
		}
	}
	return nil
}

// resolveConfig builds the Config of a profile from one document: roots
// adds resources (nil ids: all of a kind), agents includes the Agents.
func resolveConfig(document *model.Document, profileID string, roots ResourceRoots, agents bool) (*model.Config, error) {
	if err := ValidateProfileID(profileID); err != nil {
		return nil, err
	}
	stored := profileByKey(document, profileID)
	if stored == nil {
		return nil, notFound(profileID)
	}
	profile, err := copyRecord(stored)
	if err != nil {
		return nil, err
	}
	c := catalogsOf(&document.Shared)
	selected, err := selectResources(profile, c, roots)
	if err != nil {
		return nil, err
	}
	config := &model.Config{Profile: profile, ProfileID: profileID, HasAgents: agents}
	if config.SystemConfig, err = copyValue(document.System.SystemConfig); err != nil {
		return nil, err
	}
	if config.Backup, err = copyValue(document.System.Backup); err != nil {
		return nil, err
	}
	if agents {
		if config.Agents, err = copyValue(document.System.Agents); err != nil {
			return nil, err
		}
	}
	config.Subscriptions, err = pick(c.subscriptions, selected[kindSubscriptions])
	if err == nil {
		config.Nodes, err = pick(c.nodes, selected[kindNodes])
	}
	if err == nil {
		config.Aggregations, err = pick(c.aggregations, selected[kindAggregations])
	}
	if err == nil {
		config.RuleLibrary, err = copyValue(document.Shared.RuleLibrary)
	}
	if err != nil {
		return nil, err
	}
	library := map[string]*model.RuleLibraryItem{}
	for index := range config.RuleLibrary {
		library[config.RuleLibrary[index].ID] = &config.RuleLibrary[index]
	}
	for index := range profile.RuleConfigs {
		rule := &profile.RuleConfigs[index]
		if rule.ItemType != "ruleset" {
			continue
		}
		entry, ok := library[rule.LibraryRuleID]
		if !ok {
			return nil, helperError(ClassKeyError, fmt.Sprintf("'%s'", rule.LibraryRuleID))
		}
		if *rule, err = hydratedRuleset(rule, entry, profileID); err != nil {
			return nil, err
		}
	}
	return config, nil
}

// pick copies the selected records of a catalog in catalog order.
func pick[T any](c catalog[T], selected *orderedSet) ([]T, error) {
	result := []T{}
	for _, id := range c.keys {
		if selected.has(id) {
			item, err := copyRecord(c.byID[id])
			if err != nil {
				return nil, err
			}
			result = append(result, *item)
		}
	}
	return result, nil
}

// copyValue decodes a fresh copy of a model value (record, list or map).
func copyValue[T any](value T) (T, error) {
	var result T
	data, err := model.Marshal(value)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("copy model value: %w", err)
	}
	return result, nil
}

// rulesetSource are the members a ruleset takes from its rule library item.
var rulesetSource = []string{"name", "url", "behavior", "content", "source_type", "format"}

// hydratedRuleset returns rule with its source taken from the library item:
// the item's source members (stored values as they are), behavior "domain"
// when the item has none, library_enabled from the item's enabled (default
// true), and for content items the profile's content URL.
func hydratedRuleset(rule *model.RuleConfig, item *model.RuleLibraryItem, profileID string) (model.RuleConfig, error) {
	members, err := recordMembers(rule)
	if err != nil {
		return model.RuleConfig{}, err
	}
	source, err := recordMembers(item)
	if err != nil {
		return model.RuleConfig{}, err
	}
	for _, name := range append(append([]string(nil), rulesetSource...), "library_enabled") {
		members.Delete(name)
	}
	for _, name := range rulesetSource {
		if value, ok := source.Get(name); ok {
			if err := members.Set(name, value); err != nil {
				return model.RuleConfig{}, err
			}
		}
	}
	if !members.Has("behavior") {
		if err := members.Set("behavior", json.RawMessage(`"domain"`)); err != nil {
			return model.RuleConfig{}, err
		}
	}
	enabled, ok := source.Get("enabled")
	if !ok {
		enabled = json.RawMessage("true")
	}
	if err := members.Set("library_enabled", enabled); err != nil {
		return model.RuleConfig{}, err
	}
	if item.SourceType != nil && *item.SourceType == "content" {
		if profileID == "" {
			profileID = DefaultProfileID
		}
		url, err := model.Marshal("/api/profiles/" + profileID + "/rule-library/content/" + item.ID)
		if err != nil {
			return model.RuleConfig{}, err
		}
		if err := members.Set("url", url); err != nil {
			return model.RuleConfig{}, err
		}
	}
	encoded, err := members.MarshalJSON()
	if err != nil {
		return model.RuleConfig{}, err
	}
	var result model.RuleConfig
	err = json.Unmarshal(encoded, &result)
	return result, err
}

// recordMembers is a record's members in order as opaque JSON.
func recordMembers(record any) (model.Fields, error) {
	var members model.Fields
	encoded, err := model.Marshal(record)
	if err == nil {
		err = members.UnmarshalJSON(encoded)
	}
	return members, err
}
