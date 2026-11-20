package setup

import (
	"fmt"
	"slices"
	"strings"

	"configflow/internal/model"
	"configflow/internal/rules"
)

type ruleChoice struct{ Identity, Route string }

func stringsOf(values *[]string) []string {
	if values == nil {
		return nil
	}
	return *values
}

func filteredNodeIDs(nodes []model.Node, include, exclude string) ([]string, error) {
	matched, err := rules.Filter(nodes, &include, &exclude, func(node model.Node) string { return node.Name })
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, node := range matched {
		ids = append(ids, node.ID)
	}
	return ids, nil
}

func (c *catalog) strategy(choices []ruleChoice, fallback string, subscriptionIDs, nodeIDs []string, subscriptionNodes, manualNodes []model.Node) ([]model.ProxyGroup, []string, string, error) {
	groups := slices.Clone(c.Template.ProxyGroups)
	byName, byID := map[string]*model.ProxyGroup{}, map[string]*model.ProxyGroup{}
	for i := range groups {
		byName[groups[i].Name], byID[groups[i].ID] = &groups[i], &groups[i]
	}
	templateSubscriptions := map[string]bool{}
	for _, item := range c.Template.Subscriptions {
		templateSubscriptions[item.ID] = true
	}
	library := map[string]string{}
	for _, item := range c.Template.RuleLibrary {
		library[item.ID] = item.Name
	}
	policies := map[string]string{}
	for _, rule := range c.Template.RuleConfigs {
		if name, ok := library[rule.LibraryRuleID]; ok && rule.ItemType == "ruleset" {
			policies["builtin:"+identitySeparators.ReplaceAllString(strings.ToLower(name), "_")] = text(rule.Policy)
		}
	}
	removed := map[string]bool{}
	for i := range groups {
		group := &groups[i]
		bound := false
		for _, id := range stringsOf(group.Subscriptions) {
			bound = bound || templateSubscriptions[id]
		}
		if !bound {
			continue
		}
		include, exclude := text(group.Regex), text(group.ExcludeRegex)
		matched, err := filteredNodeIDs(subscriptionNodes, include, exclude)
		if err != nil {
			return nil, nil, "", err
		}
		manual, err := filteredNodeIDs(manualNodes, include, exclude)
		if err != nil {
			return nil, nil, "", err
		}
		if include == "" && exclude == "" {
			manual = append([]string{}, nodeIDs...)
		}
		if len(matched) == 0 && len(manual) == 0 {
			removed[group.ID] = true
		}
		boundIDs := []string{}
		if len(matched) != 0 {
			boundIDs = append(boundIDs, subscriptionIDs...)
		}
		group.Subscriptions, group.ManualNodes, group.Aggregations = &boundIDs, &manual, pointer([]string{})
	}
	for i := range groups {
		group := &groups[i]
		includes := []string{}
		for _, id := range stringsOf(group.IncludeGroups) {
			if !removed[id] {
				includes = append(includes, id)
			}
		}
		group.IncludeGroups = &includes
		order := []model.ResourceRef{}
		if group.ProxiesOrder != nil {
			for _, item := range *group.ProxiesOrder {
				if !removed[item.ID] {
					order = append(order, item)
				}
			}
		}
		group.ProxiesOrder = &order
	}
	direct, proxyFirst := byName[msgGlobalDirect], byName[msgProxyFirst]
	if direct == nil || proxyFirst == nil {
		return nil, nil, "", fmt.Errorf("setup template is missing required policy groups")
	}
	directFirst := func(group *model.ProxyGroup) bool {
		followed := byID[text(group.FollowGroup)]
		if followed == nil {
			followed = group
		}
		members := stringsOf(followed.IncludeGroups)
		return len(members) > 0 && members[0] == direct.ID || group.Name == msgGlobalDirect
	}
	result := []string{}
	proxied := map[string]bool{}
	for _, choice := range choices {
		group := byName[policies[choice.Identity]]
		name := msgGlobalDirect
		if choice.Route == "proxy" {
			name = msgNodeSelection
			if group != nil && text(group.FollowGroup) != "" {
				name = group.Name
			}
			proxied[name] = true
		} else if policies[choice.Identity] == "DIRECT" {
			name = "DIRECT"
		} else if group != nil && directFirst(group) {
			name = group.Name
		}
		result = append(result, name)
	}
	for i, choice := range choices {
		if choice.Route == "direct" && proxied[result[i]] {
			result[i] = msgGlobalDirect
		}
	}
	final := "DIRECT"
	if fallback == "proxy" {
		final = msgUnmatched
		proxied[final] = true
	}
	for name := range proxied {
		group := byName[name]
		if group != nil && text(group.FollowGroup) != "" && directFirst(group) {
			group.FollowGroup = pointer(proxyFirst.ID)
		}
	}
	kept := []model.ProxyGroup{}
	for _, group := range groups {
		if !removed[group.ID] {
			kept = append(kept, group)
		}
	}
	return kept, result, final, nil
}
