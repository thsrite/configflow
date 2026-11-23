package proxygroups

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/subs"
)

type previewNode struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Type             string  `json:"type"`
	SubscriptionID   *string `json:"subscription_id"`
	SubscriptionName *string `json:"subscription_name"`
	SourceType       string  `json:"source_type"`
	SourceID         string  `json:"source_id"`
	SourceName       string  `json:"source_name"`
}
type previewResult struct {
	Success  bool          `json:"success"`
	Count    int           `json:"count"`
	Total    int           `json:"total_candidates"`
	Nodes    []previewNode `json:"nodes"`
	Imported *[]string     `json:"imported_node_ids,omitempty"`
}

// previewRequest is a candidate filter to preview against one source kind.
type previewRequest struct {
	Regex         member[string]          `json:"regex"`
	ExcludeRegex  member[string]          `json:"exclude_regex"`
	Source        member[string]          `json:"source"`
	Deduplicate   member[bool]            `json:"deduplicate_by_source"`
	Subscriptions member[json.RawMessage] `json:"subscriptions"`
	Aggregations  member[json.RawMessage] `json:"aggregations"`
}

// candidate is a node offered by a preview source.
type candidate struct {
	node                             model.Node
	sourceType, sourceID, sourceName string
}

func pattern(sent member[string], message string) (string, error) {
	if sent.Null() {
		return "", nil
	}
	if !sent.OK {
		return "", bad(message)
	}
	return strings.TrimSpace(sent.Value), nil
}

// selection reads a preview's source IDs: a falsy value selects nothing;
// entries that are not text are skipped.
func selection(sent member[json.RawMessage]) ([]string, error) {
	if !rules.EnabledField(sent.Raw, false) {
		return nil, nil
	}
	var entries []member[string]
	if json.Unmarshal(sent.Raw, &entries) != nil {
		return nil, bad(msgPreviewSelection)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.OK {
			ids = append(ids, entry.Value)
		}
	}
	return ids, nil
}

// nameOr is a record's display name: its name, or its ID when it has none.
func nameOr(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

func tagged(nodes []model.Node, kind, id, name string) []candidate {
	out := make([]candidate, len(nodes))
	for i, node := range nodes {
		out[i] = candidate{node, kind, id, name}
	}
	return out
}

func filterSubscription(sub model.Subscription, nodes []model.Node) []model.Node {
	// Filtering is name-based; the original node records are kept.
	matcher := subs.CompileFilter(sub)
	out := make([]model.Node, 0, len(nodes))
	for _, node := range nodes {
		if matcher == nil || matcher.Search(node.Name) {
			out = append(out, node)
		}
	}
	return out
}

func subscriptionNodes(ctx context.Context, catalog *model.Shared, sub model.Subscription) ([]model.Node, error) {
	nodes, err := cachedNodes(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	nodes = append([]model.Node{}, nodes...)
	seen := map[string]bool{}
	for _, node := range nodes {
		if node.Name != "" {
			seen[node.Name] = true
		}
	}
	for _, node := range catalog.Nodes {
		if node.SubscriptionID == sub.ID && model.Flag(node.Enabled, true) && !seen[node.Name] {
			nodes = append(nodes, node)
			seen[node.Name] = true
		}
	}
	return filterSubscription(sub, nodes), nil
}

func findByID[T any](items []T, id string, idOf func(T) string) int {
	for i, item := range items {
		if idOf(item) == id {
			return i
		}
	}
	return -1
}

// Preview evaluates candidate filters against shared snapshots and downloaded
// caches. It never fetches or mutates subscription sources.
func (s *Service) Preview(ctx context.Context, body *previewRequest) (previewResult, error) {
	out := previewResult{Success: true, Nodes: []previewNode{}}
	include, err := pattern(body.Regex, msgRegexType)
	if err != nil {
		return out, err
	}
	exclude, err := pattern(body.ExcludeRegex, msgExcludeType)
	if err != nil {
		return out, err
	}
	source := body.Source.Value
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
	catalog, err := sharedCatalog(ctx)
	if err != nil {
		return out, err
	}
	subscriptionID := func(sub model.Subscription) string { return sub.ID }
	candidates := []candidate{}
	dedup := body.Deduplicate.OK && body.Deduplicate.Value
	imported := []string{}
	if source == "subscription" {
		ids, err := selection(body.Subscriptions)
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			i := findByID(catalog.Subscriptions, id, subscriptionID)
			if i < 0 || !model.Flag(catalog.Subscriptions[i].Enabled, true) {
				continue
			}
			sub := catalog.Subscriptions[i]
			rows, err := subscriptionNodes(ctx, catalog, sub)
			if err != nil {
				return out, err
			}
			candidates = append(candidates, tagged(rows, "subscription", id, nameOr(sub.Name, sub.ID))...)
			if dedup {
				selected := []model.Node{}
				for _, node := range catalog.Nodes {
					if node.SubscriptionID == id {
						selected = append(selected, node)
					}
				}
				for _, node := range filterSubscription(sub, selected) {
					if node.ID != "" {
						imported = append(imported, node.ID)
					}
				}
			}
		}
		if dedup {
			out.Imported = &imported
		}
	}
	if source == "aggregation" {
		ids, err := selection(body.Aggregations)
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			i := findByID(catalog.Aggregations, id, func(agg model.Aggregation) string { return agg.ID })
			if i < 0 || !model.Flag(catalog.Aggregations[i].Enabled, true) {
				continue
			}
			agg := catalog.Aggregations[i]
			rows := []model.Node{}
			for _, subID := range agg.Subscriptions {
				j := findByID(catalog.Subscriptions, subID, subscriptionID)
				if j < 0 || !model.Flag(catalog.Subscriptions[j].Enabled, true) {
					continue
				}
				items, err := subscriptionNodes(ctx, catalog, catalog.Subscriptions[j])
				if err != nil {
					return out, err
				}
				rows = append(rows, items...)
			}
			for _, nodeID := range agg.Nodes {
				if nodeID == "DIRECT" || nodeID == "REJECT" {
					rows = append(rows, model.Node{ID: nodeID, Name: nodeID, Type: "builtin"})
					continue
				}
				j := findByID(catalog.Nodes, nodeID, func(node model.Node) string { return node.ID })
				if j >= 0 && model.Flag(catalog.Nodes[j].Enabled, true) {
					rows = append(rows, catalog.Nodes[j])
				}
			}
			empty := ""
			rows, err = rules.Filter(rows, new(model.TextValue(agg.RegexFilter)), &empty, func(node model.Node) string { return node.Name })
			if err != nil {
				return out, err
			}
			candidates = append(candidates, tagged(rows, "aggregation", id, nameOr(agg.Name, agg.ID))...)
		}
	}
	out.Total = len(candidates)
	seen := map[struct{ source, name string }]bool{}
	for _, bucket := range buckets {
		for _, item := range candidates {
			node := item.node
			name := node.Name
			if name == "" || (bucket != nil && !bucket.Search(name)) || (rejection != nil && rejection.Search(name)) {
				continue
			}
			key := struct{ source, name string }{name: name}
			if dedup {
				key.source = item.sourceID
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			kind := node.Type
			if kind == "" {
				kind = node.Protocol
			}
			if kind == "" {
				kind = "unknown"
			}
			out.Nodes = append(out.Nodes, previewNode{nameOr(node.ID, name), name, kind, optional(node.SubscriptionID), optional(node.SubscriptionName), item.sourceType, item.sourceID, item.sourceName})
		}
	}
	out.Count = len(out.Nodes)
	return out, nil
}

// optional is a member the record omits when empty: null in the preview.
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
