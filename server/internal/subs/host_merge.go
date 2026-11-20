package subs

import (
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/model"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// HostInline pairs an emitted proxy with its typed subscription provenance.
type HostInline struct {
	Node  model.Node
	Proxy *yamlx.Node
}

// HostItem is one bounded preview detail. Value is absent for source parse skips.
type HostItem struct {
	Subscription string          `json:"subscription"`
	Domain       string          `json:"domain"`
	Value        json.RawMessage `json:"value,omitempty"`
	Status       string          `json:"status"`
	Reason       string          `json:"reason,omitempty"`
	Detail       string          `json:"detail,omitempty"`
	Kind         string          `json:"kind,omitempty"`
}

// HostSubscription summarizes one participating source in catalog order.
type HostSubscription struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Total   int    `json:"total"`
	Merged  int    `json:"merged"`
	Ignored int    `json:"ignored"`
}

// HostReport is the bounded, user-visible subscription hosts preview.
type HostReport struct {
	Merged        int                `json:"merged"`
	Ignored       int                `json:"ignored"`
	DomainMapped  int                `json:"domain_mapped"`
	Subscriptions []HostSubscription `json:"subscriptions"`
	Items         []HostItem         `json:"items"`
}

type providerSource struct {
	kind, id string
	query    map[string][]string
}

func sourceProviders(main *yamlx.Node, config *model.Config) []providerSource {
	if config == nil {
		return nil
	}
	subscriptions, aggregations := map[string]bool{}, map[string]bool{}
	for _, sub := range config.Subscriptions {
		subscriptions[sub.ID] = true
	}
	for _, agg := range config.Aggregations {
		aggregations[agg.ID] = true
	}
	var out []providerSource
	for _, definition := range yamlx.AllPairs(yamlx.Get(main, "proxy-providers")) {
		text, ok := dnsText(yamlx.Get(definition, "url"))
		if !ok {
			continue
		}
		u, err := weburl.Parse(text)
		if err != nil {
			continue
		}
		_, query := ParseQueryOptions(u.RawQuery)
		if i := strings.LastIndex(u.Path, "/subscriptions/"); i >= 0 {
			ident := u.Path[i+len("/subscriptions/"):]
			ident, _, _ = strings.Cut(ident, "/")
			if subscriptions[ident] {
				out = append(out, providerSource{"subscription", ident, query})
			}
		} else if i := strings.LastIndex(u.Path, "/aggregations/"); i >= 0 {
			ident := u.Path[i+len("/aggregations/"):]
			ident, _, _ = strings.Cut(ident, "/")
			if aggregations[ident] {
				out = append(out, providerSource{"aggregation", ident, query})
			}
		}
	}
	return out
}

type nameCheck func(string) bool

func firstValue(query map[string][]string, key string) string {
	if list := query[key]; len(list) > 0 {
		return list[0]
	}
	return ""
}
func queryFilter(query map[string][]string) nameCheck {
	include, exclude := firstValue(query, "regex"), firstValue(query, "exclude_regex")
	if include == "" && exclude == "" {
		return nil
	}
	var keep, drop Matcher
	if strings.TrimSpace(include) != "" {
		re, err := compileRegex(include)
		if err != nil && !isUnsupported(err) {
			return nil
		}
		if err == nil {
			keep = re
		}
	}
	if strings.TrimSpace(exclude) != "" {
		re, err := compileRegex(exclude)
		if err != nil && !isUnsupported(err) {
			return nil
		}
		if err == nil {
			drop = re
		}
	}
	return func(name string) bool {
		return (keep == nil || keep.Search(name)) && (drop == nil || !drop.Search(name))
	}
}
func resourceEnabled(record model.SwitchMember, value *bool) bool {
	return model.SwitchEnabled(record, "enabled", value, true)
}

type usedDomains map[string]map[string]bool

func usedHostDomains(main *yamlx.Node, config *model.Config, inline []HostInline, load HostIndexLoader) (usedDomains, map[string]*HostIndex, error) {
	used, indexes, loaded := usedDomains{}, map[string]*HostIndex{}, map[string]bool{}
	if config == nil {
		return used, indexes, nil
	}
	subscriptions := map[string]model.Subscription{}
	nodes := map[string]model.Node{}
	aggregations := map[string]model.Aggregation{}
	for _, sub := range config.Subscriptions {
		if sub.ID != "" && resourceEnabled(&sub, sub.Enabled) {
			subscriptions[sub.ID] = sub
		}
	}
	for _, node := range config.Nodes {
		nodes[node.ID] = node
	}
	for _, agg := range config.Aggregations {
		if _, exists := aggregations[agg.ID]; !exists {
			aggregations[agg.ID] = agg
		}
	}
	index := func(id string) (*HostIndex, error) {
		if loaded[id] {
			return indexes[id], nil
		}
		loaded[id] = true
		sub, ok := subscriptions[id]
		if !ok {
			return nil, nil
		}
		if load == nil {
			return nil, &CollaborationError{Operation: "subscription hosts cache"}
		}
		value, err := load(sub)
		if err != nil {
			return nil, err
		}
		indexes[id] = value
		return value, nil
	}
	fromSub := func(id string, checks ...nameCheck) error {
		idx, err := index(id)
		if err != nil {
			return err
		}
		if idx == nil {
			return nil
		}
		if re := CompileFilter(subscriptions[id]); re != nil {
			checks = append([]nameCheck{re.Search}, checks...)
		}
		if used[id] == nil {
			used[id] = map[string]bool{}
		}
		for domain, names := range idx.Nodes {
			for _, name := range names {
				pass := true
				for _, check := range checks {
					if !check(name) {
						pass = false
						break
					}
				}
				if pass {
					used[id][domain] = true
					break
				}
			}
		}
		return nil
	}
	fromNode := func(node model.Node, proxy *yamlx.Node) {
		if _, ok := subscriptions[node.SubscriptionID]; !ok {
			return
		}
		server := node.Server
		if value := yamlx.Get(proxy, "server"); value != nil {
			var text model.String
			data, err := yamlx.ToJSON(value)
			if err != nil || json.Unmarshal(data, &text) != nil {
				server = ""
			} else {
				server = string(text)
			}
		}
		if domain := serverDomain(server); domain != "" {
			if used[node.SubscriptionID] == nil {
				used[node.SubscriptionID] = map[string]bool{}
			}
			used[node.SubscriptionID][domain] = true
		}
	}
	for _, source := range sourceProviders(main, config) {
		queryCheck := queryFilter(source.query)
		var checks []nameCheck
		if queryCheck != nil {
			checks = append(checks, queryCheck)
		}
		if source.kind == "subscription" {
			if _, ok := subscriptions[source.id]; ok {
				if err := fromSub(source.id, checks...); err != nil {
					return nil, nil, err
				}
			}
			continue
		}
		agg, ok := aggregations[source.id]
		if !ok || !resourceEnabled(&agg, agg.Enabled) {
			continue
		}
		if agg.RegexFilter != nil && strings.TrimSpace(*agg.RegexFilter) != "" {
			re, err := compileRegex(strings.TrimSpace(*agg.RegexFilter))
			if err != nil && !isUnsupported(err) {
				continue
			}
			if err == nil {
				checks = append([]nameCheck{re.Search}, checks...)
			}
		}
		for _, id := range agg.Subscriptions {
			if _, ok := subscriptions[id]; ok {
				if err := fromSub(id, checks...); err != nil {
					return nil, nil, err
				}
			}
		}
		for _, id := range agg.Nodes {
			node, ok := nodes[id]
			if !ok || !resourceEnabled(&node, node.Enabled) {
				continue
			}
			pass := true
			for _, check := range checks {
				if !check(node.Name) {
					pass = false
					break
				}
			}
			if pass {
				fromNode(node, nil)
			}
		}
	}
	for _, item := range inline {
		fromNode(item.Node, item.Proxy)
	}
	// Inline-only sources need their index only once their domains are known.
	for _, sub := range config.Subscriptions {
		if _, ok := used[sub.ID]; ok {
			if _, err := index(sub.ID); err != nil {
				return nil, nil, err
			}
		}
	}
	return used, indexes, nil
}

// CollaborationError reports a missing typed owner instead of silently skipping work.
type CollaborationError struct{ Operation string }

func (e *CollaborationError) Error() string {
	return fmt.Sprintf(msgSubscriptionCollaborator, e.Operation)
}
func hostCreatesCycle(hosts *yamlx.Node, key string, value *yamlx.Node) bool {
	target, ok := dnsText(value)
	if !ok {
		return false
	}
	pending := []string{target}
	seen := map[string]bool{}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if current == key {
			return true
		}
		if seen[current] || len(seen) > 256 {
			continue
		}
		seen[current] = true
		for pattern, target := range yamlx.AllPairs(hosts) {
			p, ok := dnsText(pattern)
			if !ok || !hostPatternMatches(hostPatternText(p), current) {
				continue
			}
			t, ok := dnsText(target)
			if !ok || usableAddress(t) != "" {
				continue
			}
			t = hostPatternText(t)
			if t == key {
				return true
			}
			pending = append(pending, t)
		}
	}
	return false
}
func emittedHostValue(value *yamlx.Node) *yamlx.Node {
	resolved := yamlx.Resolve(value)
	if resolved != nil && resolved.Kind == yaml.SequenceNode && len(resolved.Content) == 1 {
		return resolved.Content[0]
	}
	return value
}
func anyMatches(keys []string, domain string) bool {
	for _, key := range keys {
		if hostPatternMatches(hostPatternText(key), domain) {
			return true
		}
	}
	return false
}

// MergeHosts plans a catalog-ordered merge with user overrides and cycle checks.
// Only a successful nonempty plan replaces the owned hosts container; unknown
// source fields, inline nodes and the input configuration are never rewritten.
func MergeHosts(main *yamlx.Node, config *model.Config, userKeys, mosdnsKeys []string, inline []HostInline, load HostIndexLoader) (*HostReport, *yamlx.Node, error) {
	used, indexes, err := usedHostDomains(main, config, inline, load)
	if err != nil {
		return nil, nil, err
	}
	old := yamlx.Get(main, "hosts")
	hosts := yamlx.Map()
	if old != nil {
		if _, err := yamlx.Encode(old); err != nil {
			return nil, nil, err
		}
		if resolved := yamlx.Resolve(old); resolved != nil && resolved.Kind == yaml.MappingNode {
			hosts = yamlx.CopyGraph(old)
		}
	}
	report := &HostReport{Subscriptions: []HostSubscription{}, Items: []HostItem{}}
	merged := yamlx.Map()
	owners := map[string]string{}
	note := func(item HostItem) {
		if len(report.Items) < MaxReportItems {
			report.Items = append(report.Items, item)
		}
	}
	if config != nil {
		for _, sub := range config.Subscriptions {
			domains, inUsed := used[sub.ID]
			if !inUsed {
				continue
			}
			idx := indexes[sub.ID]
			if idx == nil {
				continue
			}
			name := sub.Name
			if name == "" {
				name = sub.ID
			}
			accepted := 0
			entryCount := 0
			for key, value := range yamlx.AllPairs(idx.Entries) {
				domain, ok := dnsText(key)
				if !ok {
					continue
				}
				entryCount++
				reason := ""
				owner, owned := owners[domain]
				switch {
				case !domains[domain]:
					reason = "not_node_domain"
				case anyMatches(userKeys, domain):
					reason = "custom_hosts"
				case anyMatches(mosdnsKeys, domain):
					reason = "mosdns_hosts"
				case owned:
					if !yamlx.Equal(yamlx.Get(merged, domain), value) {
						reason = "conflict"
					}
				case hostCreatesCycle(hosts, domain, value):
					reason = "cycle"
				}
				wire, err := yamlx.ToJSON(emittedHostValue(value))
				if err != nil {
					return nil, nil, err
				}
				item := HostItem{Subscription: name, Domain: domain, Value: wire}
				if reason != "" {
					report.Ignored++
					item.Status = "ignored"
					item.Reason = reason
					item.Detail = Reasons[reason]
					if reason == "conflict" {
						item.Detail = fmt.Sprintf(msgHostsConflict, owner, owner)
					}
					note(item)
					continue
				}
				accepted++
				if !owned {
					owners[domain] = name
					yamlx.Set(merged, domain, value)
					yamlx.Set(hosts, domain, emittedHostValue(value))
					item.Status = "merged"
					item.Kind = "address"
					if _, ok := dnsText(value); ok {
						item.Kind = "domain"
						report.DomainMapped++
					}
					note(item)
					report.Merged++
				}
			}
			for _, sample := range idx.Skipped {
				detail := Reasons[sample.Reason]
				if detail == "" {
					detail = msgHostsInvalidEntry
				}
				note(HostItem{Subscription: name, Domain: sample.Domain, Status: "ignored", Reason: sample.Reason, Detail: detail})
			}
			report.Ignored += idx.SkippedCount
			report.Subscriptions = append(report.Subscriptions, HostSubscription{sub.ID, name, idx.Total, accepted, entryCount - accepted + idx.SkippedCount})
		}
	}
	if report.Merged > 0 {
		yamlx.Set(main, "hosts", hosts)
		yamlx.MaterializeAliases(main)
	}
	return report, merged, nil
}
