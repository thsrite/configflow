package proxygroups

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/rules"
)

type previewNode struct {
	ID               json.RawMessage `json:"id"`
	Name             string          `json:"name"`
	Type             string          `json:"type"`
	SubscriptionID   json.RawMessage `json:"subscription_id"`
	SubscriptionName json.RawMessage `json:"subscription_name"`
	SourceType       json.RawMessage `json:"source_type"`
	SourceID         json.RawMessage `json:"source_id"`
	SourceName       json.RawMessage `json:"source_name"`
}
type previewResult struct {
	Success  bool               `json:"success"`
	Count    int                `json:"count"`
	Total    int                `json:"total_candidates"`
	Nodes    []previewNode      `json:"nodes"`
	Imported *[]json.RawMessage `json:"imported_node_ids,omitempty"`
}

func raw(o object, key string) json.RawMessage {
	v, ok := o.Get(key)
	if !ok {
		return json.RawMessage("null")
	}
	return v
}
func pattern(o object, key, message string) (string, error) {
	v := raw(o, key)
	if string(v) == "null" {
		return "", nil
	}
	var result string
	if json.Unmarshal(v, &result) != nil {
		return "", bad(message)
	}
	return strings.TrimSpace(result), nil
}
func metadata(nodes []object, kind, id, name string) ([]object, error) {
	out := make([]object, len(nodes))
	for i, node := range nodes {
		out[i] = node
		for _, field := range []struct{ k, v string }{{"source_type", kind}, {"source_id", id}, {"source_name", name}} {
			if err := set(&out[i], field.k, field.v); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
func fallbackName(o object) string {
	if o.Has("name") {
		return text(o, "name")
	}
	return text(o, "id")
}
func subscriptionNodes(ctx context.Context, profile, sub object) ([]object, error) {
	nodes, err := cachedNodes(ctx, text(sub, "id"))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, node := range nodes {
		if name := text(node, "name"); name != "" {
			seen[name] = true
		}
	}
	imported, err := list(profile, "nodes")
	if err != nil {
		return nil, err
	}
	for _, node := range imported {
		if text(node, "subscription_id") == text(sub, "id") && enabled(node) && !seen[text(node, "name")] {
			nodes = append(nodes, node)
			seen[text(node, "name")] = true
		}
	}
	return filterSubscription(sub, nodes)
}

// Preview evaluates candidate filters against shared snapshots and downloaded
// caches. It never fetches or mutates subscription sources.
func (s *Service) Preview(ctx context.Context, body object) (previewResult, error) {
	out := previewResult{Success: true, Nodes: []previewNode{}}
	include, err := pattern(body, "regex", msgRegexType)
	if err != nil {
		return out, err
	}
	exclude, err := pattern(body, "exclude_regex", msgExcludeType)
	if err != nil {
		return out, err
	}
	source := text(body, "source")
	if source != "subscription" && source != "aggregation" {
		return out, bad("Invalid preview source")
	}
	if include == "" && exclude == "" {
		return out, bad(msgRegexRequired)
	}
	buckets := []*rules.Regex{nil}
	if include != "" {
		buckets = nil
		for _, part := range strings.Split(include, "`") {
			re, err := rules.CompilePyRegex(part, false)
			if err != nil {
				return out, bad(fmt.Sprintf(msgRegexInvalid, err))
			}
			buckets = append(buckets, re)
		}
	}
	var rejection *rules.Regex
	if exclude != "" {
		rejection, err = rules.CompilePyRegex(exclude, false)
		if err != nil {
			return out, bad(fmt.Sprintf(msgExcludeInvalid, err))
		}
	}
	profile, err := sharedConfig(ctx)
	if err != nil {
		return out, err
	}
	subscriptions, err := list(profile, "subscriptions")
	if err != nil {
		return out, err
	}
	nodes, err := list(profile, "nodes")
	if err != nil {
		return out, err
	}
	candidates := []object{}
	dedup := string(raw(body, "deduplicate_by_source")) == "true"
	imported := []json.RawMessage{}
	if source == "subscription" {
		ids, err := names(body, "subscriptions")
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			i := find(subscriptions, id)
			if i < 0 || !enabled(subscriptions[i]) {
				continue
			}
			sub := subscriptions[i]
			rows, err := subscriptionNodes(ctx, profile, sub)
			if err != nil {
				return out, err
			}
			rows, err = metadata(rows, "subscription", id, fallbackName(sub))
			if err != nil {
				return out, err
			}
			candidates = append(candidates, rows...)
			if dedup {
				selected := []object{}
				for _, node := range nodes {
					if text(node, "subscription_id") == id {
						selected = append(selected, node)
					}
				}
				selected, err = filterSubscription(sub, selected)
				if err != nil {
					return out, err
				}
				for _, node := range selected {
					if text(node, "id") != "" {
						imported = append(imported, raw(node, "id"))
					}
				}
			}
		}
		if dedup {
			out.Imported = &imported
		}
	}
	if source == "aggregation" {
		ids, err := names(body, "aggregations")
		if err != nil {
			return out, err
		}
		aggregations, err := list(profile, "subscription_aggregations")
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			i := find(aggregations, id)
			if i < 0 || !enabled(aggregations[i]) {
				continue
			}
			agg := aggregations[i]
			rows := []object{}
			subIDs, err := names(agg, "subscriptions")
			if err != nil {
				return out, err
			}
			for _, subID := range subIDs {
				j := find(subscriptions, subID)
				if j < 0 || !enabled(subscriptions[j]) {
					continue
				}
				items, err := subscriptionNodes(ctx, profile, subscriptions[j])
				if err != nil {
					return out, err
				}
				rows = append(rows, items...)
			}
			manual, err := names(agg, "nodes")
			if err != nil {
				return out, err
			}
			for _, nodeID := range manual {
				if nodeID == "DIRECT" || nodeID == "REJECT" {
					var node object
					if err = set(&node, "id", nodeID); err != nil {
						return out, err
					}
					if err = set(&node, "name", nodeID); err != nil {
						return out, err
					}
					if err = set(&node, "type", "builtin"); err != nil {
						return out, err
					}
					rows = append(rows, node)
					continue
				}
				j := find(nodes, nodeID)
				if j >= 0 && enabled(nodes[j]) {
					rows = append(rows, nodes[j])
				}
			}
			rows, err = metadata(rows, "aggregation", id, fallbackName(agg))
			if err != nil {
				return out, err
			}
			rows, err = filterAggregation(rows, text(agg, "regex_filter"))
			if err != nil {
				return out, err
			}
			candidates = append(candidates, rows...)
		}
	}
	out.Total = len(candidates)
	seen := map[struct{ source, name string }]bool{}
	for _, bucket := range buckets {
		for _, node := range candidates {
			name := text(node, "name")
			if name == "" || (bucket != nil && !bucket.Search(name)) || (rejection != nil && rejection.Search(name)) {
				continue
			}
			key := struct{ source, name string }{name: name}
			if dedup {
				key.source = text(node, "source_id")
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			id := raw(node, "id")
			if !node.Has("id") {
				id, _ = json.Marshal(name)
			}
			kind := text(node, "type")
			if kind == "" {
				kind = text(node, "protocol")
			}
			if kind == "" {
				kind = "unknown"
			}
			out.Nodes = append(out.Nodes, previewNode{id, name, kind, raw(node, "subscription_id"), raw(node, "subscription_name"), raw(node, "source_type"), raw(node, "source_id"), raw(node, "source_name")})
		}
	}
	out.Count = len(out.Nodes)
	return out, nil
}
