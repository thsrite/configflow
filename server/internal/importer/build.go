package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"configflow/internal/rules"
)

func nodeSignature(node Node) json.RawMessage {
	fields := node.original
	if fields == nil {
		fields = map[string]json.RawMessage{"type": jsonValue(node.Type), "server": jsonValue(node.Server), "port": node.Port, "params": jsonValue(node.Params)}
	}
	signature := map[string]json.RawMessage{}
	for _, key := range []string{"type", "server", "port", "params"} {
		signature[key] = fields[key]
	}
	data, _ := json.Marshal(signature)
	return data
}
func sameNode(a, b Node) bool {
	var left, right any
	decode := func(node Node, value *any) error {
		decoder := json.NewDecoder(bytes.NewReader(nodeSignature(node)))
		decoder.UseNumber()
		return decoder.Decode(value)
	}
	if decode(a, &left) != nil || decode(b, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}
func regexMatches(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	compiled, err := rules.CompilePyRegex(pattern, false)
	return err != nil || compiled.Search(name)
}

// Build resolves imported names against an existing shared catalog without
// mutating it or the parsed plan. Unknown protocol options remain intact.
func Build(plan *Plan, shared Shared, options BuildOptions) (*Result, error) {
	if plan == nil {
		return nil, fmt.Errorf("import plan is nil")
	}
	newID := options.NewID
	if newID == nil {
		newID = randomID
	}
	result := &Result{Shared: Shared{Subscriptions: []Subscription{}, Nodes: []Node{}, RuleLibrary: []LibraryItem{}}, Profile: Profile{Groups: []ProxyGroup{}, Rules: []RuleConfig{}}}
	warn := warnings{}
	for _, bucket := range plan.warnings.buckets {
		warn.buckets = append(warn.buckets, warningBucket{bucket.category, append([]string{}, bucket.details...), maps.Clone(bucket.seen)})
	}
	defaults := []string{}
	for _, id := range options.DefaultSubscriptionIDs {
		for _, sub := range shared.Subscriptions {
			if sub.ID == id {
				defaults = append(defaults, id)
				break
			}
		}
	}
	subsByURL := map[string]Subscription{}
	subNames := map[string]bool{}
	for _, sub := range shared.Subscriptions {
		if sub.URL != "" {
			subsByURL[sub.URL] = sub
		}
		subNames[sub.Name] = true
	}
	providerIDs := map[string]string{}
	providerOrder := []string{}
	for _, provider := range plan.Providers {
		if _, exists := providerIDs[provider.Name]; !exists {
			providerOrder = append(providerOrder, provider.Name)
		}
		if existing, exists := subsByURL[provider.URL]; exists {
			providerIDs[provider.Name] = existing.ID
			result.Summary.SubscriptionsReused++
			continue
		}
		name := uniqueName(first(provider.Name, subscriptionName(provider.URL)), subNames, false)
		subNames[name] = true
		id, err := newID("sub")
		if err != nil {
			return nil, err
		}
		sub := Subscription{ID: id, Name: name, URL: provider.URL, Type: "universal", Interval: 86400, Enabled: true}
		result.Shared.Subscriptions = append(result.Shared.Subscriptions, sub)
		subsByURL[sub.URL] = sub
		providerIDs[provider.Name] = id
	}
	nodesByName := map[string]Node{}
	nodeNames := map[string]bool{}
	for _, node := range shared.Nodes {
		nodesByName[node.Name] = node
		nodeNames[node.Name] = true
	}
	nodeIDs, nodeFinalNames := map[string]string{}, map[string]string{}
	nodeOrder, dialers := []string{}, []string{}
	for _, proxy := range plan.Proxies {
		if _, exists := nodeIDs[proxy.Name]; exists {
			warn.add(msgDuplicateNode, proxy.Name)
			continue
		}
		if raw := proxy.Options["dialer-proxy"]; len(raw) > 0 && string(raw) != "null" && string(raw) != `""` && string(raw) != "false" && string(raw) != "0" {
			dialers = append(dialers, proxy.Name)
		}
		params := map[string]json.RawMessage{}
		for key, raw := range proxy.Options {
			if key != "dialer-proxy" {
				params[key] = append(json.RawMessage{}, raw...)
			}
		}
		node := Node{Name: proxy.Name, Type: strings.ToLower(proxy.Type), Server: proxy.Server, Port: append(json.RawMessage{}, proxy.Port...), Params: params, Enabled: true}
		if existing, exists := nodesByName[node.Name]; exists && sameNode(existing, node) {
			nodeIDs[proxy.Name] = existing.ID
			nodeFinalNames[proxy.Name] = existing.Name
			result.Summary.NodesReused++
		} else {
			node.Name = uniqueName(node.Name, nodeNames, false)
			id, err := newID("node")
			if err != nil {
				return nil, err
			}
			node.ID = id
			nodeNames[node.Name] = true
			result.Shared.Nodes = append(result.Shared.Nodes, node)
			nodeIDs[proxy.Name] = id
			nodeFinalNames[proxy.Name] = node.Name
			if node.Name != proxy.Name {
				warn.add(msgRenameNode, proxy.Name+" → "+node.Name)
			}
		}
		nodeOrder = append(nodeOrder, proxy.Name)
	}
	if len(dialers) > 0 {
		warn.add(msgDialer, strings.Join(dialers[:min(8, len(dialers))], "、"))
	}
	groupIDs := map[string]string{}
	for _, group := range plan.Groups {
		_, exists := groupIDs[group.Name]
		if _, builtin := builtinPolicies[group.Name]; exists || builtin {
			warn.add(msgDuplicateGroup, group.Name)
			continue
		}
		id, err := newID("group")
		if err != nil {
			return nil, err
		}
		groupIDs[group.Name] = id
	}
	usedNodes := map[string]bool{}
	builtGroups := map[string]bool{}
	for _, group := range plan.Groups {
		id := groupIDs[group.Name]
		if id == "" || builtGroups[id] {
			continue
		}
		builtGroups[id] = true
		out := ProxyGroup{ID: id, Name: group.Name, Type: group.Type, Enabled: true, Subscriptions: []string{}, ManualNodes: []string{}, Aggregations: []string{}, IncludeGroups: []string{}, Order: []OrderItem{}, Extra: group.Extra}
		addNode := func(id string) {
			if !slices.Contains(out.ManualNodes, id) {
				out.ManualNodes = append(out.ManualNodes, id)
				out.Order = append(out.Order, OrderItem{"node", id})
			}
		}
		for _, member := range group.Members {
			if target, exists := groupIDs[member]; exists && member != group.Name {
				if !slices.Contains(out.IncludeGroups, target) {
					out.IncludeGroups = append(out.IncludeGroups, target)
					out.Order = append(out.Order, OrderItem{"strategy", target})
				}
			} else if target, exists := nodeIDs[member]; exists {
				addNode(target)
				usedNodes[member] = true
			} else if target := builtinPolicies[member]; target != "" {
				addNode(target)
			} else if target := plan.Aliases[member]; target != "" {
				addNode(target)
			} else if target, exists := providerIDs[member]; exists {
				out.Subscriptions = append(out.Subscriptions, target)
			} else if member != group.Name {
				warn.add(msgMissingMember, group.Name+" → "+member)
			}
		}
		for _, provider := range group.Providers {
			if target, exists := providerIDs[provider]; exists {
				out.Subscriptions = appendUnique(out.Subscriptions, target)
			} else {
				warn.add(msgMissingProvider, group.Name+" → "+provider)
			}
		}
		if group.IncludeAllProviders {
			for _, provider := range providerOrder {
				out.Subscriptions = appendUnique(out.Subscriptions, providerIDs[provider])
			}
			for _, id := range defaults {
				out.Subscriptions = appendUnique(out.Subscriptions, id)
			}
		}
		if group.IncludeAllProxies {
			for _, name := range nodeOrder {
				if regexMatches(group.Filter, name) {
					addNode(nodeIDs[name])
					usedNodes[name] = true
				}
			}
			for _, id := range defaults {
				out.Subscriptions = appendUnique(out.Subscriptions, id)
			}
		}
		if len(out.Order) == 0 && len(out.Subscriptions) == 0 {
			warn.add(msgEmptyGroup, group.Name)
		}
		if len(out.Subscriptions) > 0 {
			out.Regex = group.Filter
		}
		if group.Type != "select" {
			out.URL = group.URL
			if group.Interval != nil && *group.Interval != 0 {
				interval := *group.Interval
				out.Interval = &interval
			}
		}
		result.Profile.Groups = append(result.Profile.Groups, out)
	}
	libraryByURL := map[string]LibraryItem{}
	libraryNames := map[string]bool{}
	for _, item := range shared.RuleLibrary {
		if item.URL != "" {
			libraryByURL[item.URL] = item
		}
		libraryNames[item.Name] = true
	}
	libraryIDs := map[string]string{}
	resolveRuleset := func(name string) (string, error) {
		if id, exists := libraryIDs[name]; exists {
			return id, nil
		}
		var provider *RuleProvider
		for i := range plan.RuleProviders {
			if plan.RuleProviders[i].Name == name {
				provider = &plan.RuleProviders[i]
				break
			}
		}
		if provider == nil {
			return "", nil
		}
		if provider.URL != "" {
			if existing, exists := libraryByURL[provider.URL]; exists {
				libraryIDs[name] = existing.ID
				result.Summary.RuleLibraryReused++
				return existing.ID, nil
			}
		}
		finalName := uniqueName(name, libraryNames, true)
		if finalName != name {
			warn.add(msgRenameLibrary, name+" → "+finalName)
		}
		libraryNames[finalName] = true
		id, err := newID("lib")
		if err != nil {
			return "", err
		}
		item := LibraryItem{ID: id, Name: finalName, Behavior: first(provider.Behavior, "classical"), Enabled: true}
		if provider.Content != nil {
			item.SourceType = "content"
			text := *provider.Content
			item.Content = &text
		} else {
			item.SourceType = "url"
			item.URL = provider.URL
			libraryByURL[provider.URL] = item
		}
		result.Shared.RuleLibrary = append(result.Shared.RuleLibrary, item)
		libraryIDs[name] = id
		return id, nil
	}
	resolvePolicy := func(policy string) string {
		if _, exists := groupIDs[policy]; exists {
			return policy
		}
		if target := builtinPolicies[policy]; target != "" {
			return target
		}
		if target := plan.Aliases[policy]; target != "" {
			return target
		}
		if usedNodes[policy] {
			return nodeFinalNames[policy]
		}
		return ""
	}
	for _, rule := range plan.Rules {
		policy := resolvePolicy(rule.Policy)
		if policy == "" {
			warn.add(msgMissingRulePolicy, strings.ReplaceAll(rule.Type+","+rule.Value+","+rule.Policy, ",,", ","))
			continue
		}
		item := RuleConfig{Policy: policy, Enabled: true, NoResolve: rule.NoResolve}
		prefix := "rule"
		if rule.Type == "RULE-SET" {
			id, err := resolveRuleset(rule.Value)
			if err != nil {
				return nil, err
			}
			if id == "" {
				warn.add(msgMissingRuleset, rule.Value)
				continue
			}
			prefix = "ruleset"
			item.ItemType = "ruleset"
			item.LibraryID = id
			result.Summary.Rulesets++
		} else {
			item.ItemType = "rule"
			item.RuleType = rule.Type
			value := rule.Value
			item.Value = &value
			result.Summary.Rules++
		}
		id, err := newID(prefix)
		if err != nil {
			return nil, err
		}
		item.ID = id
		result.Profile.Rules = append(result.Profile.Rules, item)
	}
	if plan.Custom != nil {
		custom := &EngineSettings{plan.Custom.Text}
		switch plan.Custom.Engine {
		case "mihomo":
			result.Profile.Mihomo = custom
		case "surge":
			result.Profile.Surge = custom
		case "loon":
			result.Profile.Loon = custom
		}
		engine := plan.Custom.Engine
		result.Summary.CustomConfig = &engine
	}
	result.Summary.Nodes = len(result.Shared.Nodes)
	result.Summary.Subscriptions = len(result.Shared.Subscriptions)
	result.Summary.RuleLibrary = len(result.Shared.RuleLibrary)
	result.Summary.Groups = len(result.Profile.Groups)
	result.Warnings = warn.list()
	return result, nil
}
