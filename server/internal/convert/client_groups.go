package convert

import (
	"fmt"
	"strconv"
	"strings"

	"configflow/internal/model"
)

func clientGroupSource(group model.ProxyGroup, config *model.Config) (*model.ProxyGroup, error) {
	if config == nil {
		return nil, fmt.Errorf("%s", msgClientConfig)
	}
	for _, member := range []string{"name", "type", "follow_group"} {
		if err := storedText(&group, kindGroup, group.ID, member); err != nil {
			return nil, err
		}
	}
	if stringValue(group.FollowGroup) == "" {
		return &group, nil
	}
	source := followed(configGroups(config), *group.FollowGroup)
	if source == nil {
		return nil, nil
	}
	if err := storedText(source, kindGroup, source.ID, "type"); err != nil {
		return nil, err
	}
	return source, nil
}
func effectiveClientGroup(group model.ProxyGroup, config *model.Config) (*model.ProxyGroup, error) {
	source, err := clientGroupSource(group, config)
	if err != nil || source == nil {
		return nil, err
	}
	if stringValue(group.FollowGroup) == "" {
		return &group, nil
	}
	result := group
	result.Type = source.Type
	result.ManualNodes, result.Aggregations, result.IncludeGroups, result.Subscriptions = source.ManualNodes, source.Aggregations, source.IncludeGroups, source.Subscriptions
	result.Regex, result.ExcludeRegex, result.AggregationRegex, result.AggregationExcludeRegex = source.Regex, source.ExcludeRegex, source.AggregationRegex, source.AggregationExcludeRegex
	result.ProxiesOrder, result.ProxyOrder, result.URL, result.Interval, result.Strategy = source.ProxiesOrder, source.ProxyOrder, source.URL, source.Interval, source.Strategy
	return &result, nil
}
func clientGroupSources(group model.ProxyGroup) (manual, aggregations, groups, subscriptions []string) {
	if group.ManualNodes != nil {
		manual = *group.ManualNodes
	}
	if group.Aggregations != nil {
		aggregations = *group.Aggregations
	}
	if group.IncludeGroups != nil {
		groups = *group.IncludeGroups
	}
	if group.Subscriptions != nil {
		subscriptions = *group.Subscriptions
	}
	if len(manual)+len(aggregations)+len(groups)+len(subscriptions) == 0 && group.Proxies != nil {
		switch stringValue(group.Source) {
		case "node":
			manual = *group.Proxies
		case "strategy":
			groups = *group.Proxies
		}
	}
	return
}

type clientMemberRef struct{ kind, id string }

func clientNodeRef(id string) clientMemberRef {
	if id == "DIRECT" || id == "REJECT" {
		return clientMemberRef{"builtin", id}
	}
	return clientMemberRef{"node", id}
}
func clientMemberRefs(group model.ProxyGroup) []clientMemberRef {
	manual, _, groups, _ := clientGroupSources(group)
	var refs []clientMemberRef
	if group.ProxiesOrder != nil && len(*group.ProxiesOrder) > 0 {
		seen := map[string]bool{}
		for _, ref := range *group.ProxiesOrder {
			switch ref.Type {
			case "node":
				refs = append(refs, clientNodeRef(ref.ID))
				seen[ref.ID] = true
			case "strategy":
				refs = append(refs, clientMemberRef{"strategy", ref.ID})
			}
		}
		for _, id := range manual {
			if !seen[id] {
				refs = append(refs, clientNodeRef(id))
			}
		}
	} else {
		var nodes, strategies []clientMemberRef
		for _, id := range manual {
			nodes = append(nodes, clientNodeRef(id))
		}
		for _, id := range groups {
			strategies = append(strategies, clientMemberRef{"strategy", id})
		}
		if stringValue(group.ProxyOrder) == "strategies_first" {
			refs = append(strategies, nodes...)
		} else {
			refs = append(nodes, strategies...)
		}
	}
	return refs
}
func clientRemoteURL(config *model.Config, base, kind, id, target, regex, exclude string) (string, error) {
	path := "/subscriptions/" + id + "/proxies"
	if kind == "aggregation" {
		path = "/aggregations/" + id + "/provider"
	}
	query := [][2]string{{"format", target}}
	if regex != "" {
		query = append(query, [2]string{"regex", regex})
	}
	if exclude != "" {
		query = append(query, [2]string{"exclude_regex", exclude})
	}
	if token := config.SystemConfig.ConfigToken; token != "" {
		query = append(query, [2]string{"token", token})
	}
	return appendProfileQuery(base+profilePath(config, path), query)
}
func clientResourceName(config *model.Config, kind, id string) (string, bool, error) {
	if kind == "subscription" {
		for _, s := range config.Subscriptions {
			if s.ID == id && enabled(&s, s.Enabled) {
				if err := storedText(&s, kindSubscription, id, "name"); err != nil {
					return "", false, err
				}
				return s.Name, true, nil
			}
		}
	} else {
		for _, a := range config.Aggregations {
			if a.ID == id && enabled(&a, a.Enabled) {
				if err := storedText(&a, kindAggregation, id, "name"); err != nil {
					return "", false, err
				}
				return a.Name, true, nil
			}
		}
	}
	return "", false, nil
}

// SurgeGroup renders one policy group and its remote policy paths.
func SurgeGroup(group model.ProxyGroup, config *model.Config, base string, smart map[string]string) (string, error) {
	effective, err := effectiveClientGroup(group, config)
	if err != nil || effective == nil {
		return "", err
	}
	typ := effective.Type
	priority, isSmart := smart[group.ID]
	if isSmart {
		typ = "smart"
	}
	var members, paths []string
	for _, ref := range clientMemberRefs(*effective) {
		switch ref.kind {
		case "builtin":
			members = append(members, ref.id)
		case "node":
			for _, n := range config.Nodes {
				if n.ID == ref.id {
					members = append(members, n.Name)
					break
				}
			}
		case "strategy":
			if g := followed(configGroups(config), ref.id); g != nil {
				if err := storedText(g, kindGroup, g.ID, "name"); err != nil {
					return "", err
				}
				members = append(members, g.Name)
			}
		}
	}
	_, aggregations, _, subscriptions := clientGroupSources(*effective)
	for _, source := range []struct {
		kind           string
		ids            []string
		regex, exclude *string
	}{{"subscription", subscriptions, effective.Regex, effective.ExcludeRegex}, {"aggregation", aggregations, effective.AggregationRegex, effective.AggregationExcludeRegex}} {
		for _, id := range source.ids {
			_, found, err := clientResourceName(config, source.kind, id)
			if err != nil {
				return "", err
			}
			if found {
				path, err := clientRemoteURL(config, clientBaseURL(config, base), source.kind, id, "surge", strings.TrimSpace(stringValue(source.regex)), strings.TrimSpace(stringValue(source.exclude)))
				if err != nil {
					return "", err
				}
				paths = append(paths, path)
			}
		}
	}
	if len(members)+len(paths) == 0 {
		return "", nil
	}
	line := group.Name + " = " + typ
	switch typ {
	case "select", "url-test", "fallback", "load-balance", "smart":
	default:
		return "", nil
	}
	if len(members) > 0 {
		line += ", " + strings.Join(members, ", ")
	}
	if typ != "select" {
		if typ == "smart" && priority != "" {
			line += ", policy-priority=" + priority
		}
		url := defaultTestURL
		if effective.URL != nil {
			url = *effective.URL
		}
		interval := int64(300)
		if effective.Interval != nil {
			interval = int64(*effective.Interval)
		}
		line += ", url = " + url + ", interval = " + strconv.FormatInt(interval, 10)
	}
	for _, path := range paths {
		line += ", policy-path = " + path + ", update-interval = 86400"
	}
	surgeLog.Debug("Generated Surge proxy group")
	return line, nil
}

// LoonGroup renders a policy group whose members have already been resolved.
func LoonGroup(group model.ProxyGroup, members []string) (string, error) {
	for _, member := range []string{"name", "type"} {
		if err := storedText(&group, kindGroup, group.ID, member); err != nil {
			return "", err
		}
	}
	if len(members) == 0 {
		return "", nil
	}
	name := clientSanitizeName(group.Name)
	list := strings.Join(members, ",")
	url := stringValue(group.URL)
	if url == "" {
		url = LoonDefaultTestURL
	}
	interval := int64(600)
	if group.Interval != nil && *group.Interval != 0 {
		interval = int64(*group.Interval)
	}
	options := ",url=" + url + ",interval=" + strconv.FormatInt(interval, 10)
	switch group.Type {
	case "select":
		return name + " = select," + list, nil
	case "url-test":
		return name + " = url-test," + list + options + ",tolerance=100", nil
	case "fallback":
		return name + " = fallback," + list + options + ",max-timeout=3000", nil
	case "load-balance":
		algorithm := loadBalanceAlgorithms[stringValue(group.Strategy)]
		if algorithm == "" {
			algorithm = "random"
		}
		return name + " = load-balance," + list + options + ",algorithm=" + algorithm + ",max-timeout=3000", nil
	}
	return "", nil
}
