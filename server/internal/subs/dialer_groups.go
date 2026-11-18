package subs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/model"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// DialerInputs provides source-owned cache and conversion operations.
type DialerInputs struct {
	LoadCache      func(context.Context, string) (*SubscriptionCache, error)
	Convert        func(context.Context, model.Node) (*yamlx.Node, error)
	DownloadedText func(context.Context, string) (string, bool)
}

// DialerGroup describes one generated source-owned relay group.
type DialerGroup struct {
	Subscription string   `json:"subscription"`
	Target       string   `json:"target"`
	Group        string   `json:"group"`
	Members      []string `json:"members"`
	Copied       bool     `json:"copied"`
}

// DialerSkip describes one source node withheld because its relay is unavailable.
type DialerSkip struct {
	Subscription string `json:"subscription"`
	Node         string `json:"node"`
	Target       string `json:"target"`
	Reason       string `json:"reason"`
}

// DialerReport contains generated groups and a bounded missing-relay preview.
type DialerReport struct {
	Groups       []DialerGroup `json:"groups"`
	Skipped      []DialerSkip  `json:"skipped"`
	SkippedCount int           `json:"skipped_count"`
}

// SourceDialers reads the cached record, including explicit null, before falling
// back to a previous downloaded body. No substitute cache state is fabricated.
func SourceDialers(ctx context.Context, sub model.Subscription, cache *SubscriptionCache, downloaded func(context.Context, string) (string, bool)) (*DialersData, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cache != nil && cache.Metadata != nil {
		data, err := model.Marshal(cache.Metadata)
		if err != nil {
			return nil, err
		}
		var presence struct {
			Dialers json.RawMessage `json:"dialers"`
		}
		if err := json.Unmarshal(data, &presence); err != nil {
			return nil, err
		}
		if presence.Dialers != nil {
			return cache.Metadata.Dialers, nil
		}
	}
	if sub.URL == "" || downloaded == nil {
		return nil, nil
	}
	body, ok := downloaded(ctx, sub.URL)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return ExtractDialers(body)
}
func dialerSettings(entry DialerTarget) *yamlx.Node {
	group := yamlx.Map()
	if entry.URL != nil {
		yamlx.Set(group, "url", yamlx.Str(*entry.URL))
	}
	if entry.Interval != nil {
		yamlx.Set(group, "interval", yamlx.Int(int64(*entry.Interval)))
	}
	if entry.Tolerance != nil {
		yamlx.Set(group, "tolerance", yamlx.Int(int64(*entry.Tolerance)))
	}
	if entry.Lazy != nil {
		yamlx.Set(group, "lazy", yamlx.Bool(*entry.Lazy))
	}
	if entry.Timeout != nil {
		yamlx.Set(group, "timeout", yamlx.Int(int64(*entry.Timeout)))
	}
	if entry.ExpectedStatus != nil {
		yamlx.Set(group, "expected-status", yamlx.Str(*entry.ExpectedStatus))
	}
	if entry.Strategy != nil {
		yamlx.Set(group, "strategy", yamlx.Str(*entry.Strategy))
	}
	if entry.MaxFailedTimes != nil {
		yamlx.Set(group, "max-failed-times", yamlx.Int(int64(*entry.MaxFailedTimes)))
	}
	return group
}
func ownedSequence(main *yamlx.Node, field string) (*yamlx.Node, error) {
	value := yamlx.Get(main, field)
	if value == nil {
		return yamlx.Seq(), nil
	}
	resolved := yamlx.Resolve(value)
	if resolved == nil || resolved.Kind != yaml.SequenceNode {
		return nil, &SubscriptionShapeError{Field: field}
	}
	validated, err := yamlx.Encode(value)
	if err != nil {
		return nil, err
	}
	return yamlx.Clone(validated), nil
}
func mentionsDialer(node model.Node) bool {
	if node.ProxyString != nil && strings.Contains(*node.ProxyString, "dialer-proxy") {
		return true
	}
	data, err := node.Params.MarshalJSON()
	return err == nil && bytes.Contains(data, []byte("dialer-proxy"))
}

// AddGroups supplies relay groups required by emitted source nodes. Catalogs
// are indexed once; cache records are loaded once per source and passed typed
// to conversion. Partial business failures retain the same generated prefix.
func AddGroups(ctx context.Context, main *yamlx.Node, config *model.Config, inputs DialerInputs) (*DialerReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, nil
	}
	subscriptions := map[string]model.Subscription{}
	aggregations := map[string]model.Aggregation{}
	for _, sub := range config.Subscriptions {
		if sub.ID != "" {
			subscriptions[sub.ID] = sub
		}
	}
	for _, agg := range config.Aggregations {
		if agg.ID != "" {
			aggregations[agg.ID] = agg
		}
	}
	direct, seen := map[string]bool{}, map[string]bool{}
	var emitted []string
	for _, source := range sourceProviders(main, config) {
		var members []string
		if source.kind == "subscription" {
			members = []string{source.id}
			direct[source.id] = true
		} else {
			members = aggregations[source.id].Subscriptions
		}
		for _, id := range members {
			if _, ok := subscriptions[id]; ok && !seen[id] {
				seen[id] = true
				emitted = append(emitted, id)
			}
		}
	}
	caches := map[string]*SubscriptionCache{}
	var order []string
	for _, id := range emitted {
		if resourceEnabled(subscriptions[id].Enabled) {
			if inputs.LoadCache == nil {
				return nil, &CollaborationError{Operation: "subscription dialer cache"}
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			cache, err := inputs.LoadCache(ctx, id)
			if err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if cache == nil {
				cache = &SubscriptionCache{}
			}
			caches[id] = cache
			order = append(order, id)
		}
	}
	if len(order) == 0 {
		return nil, nil
	}
	ops := sharedDialers()
	if ops.NodeDialer == nil {
		return nil, &CollaborationError{Operation: "node dialer"}
	}
	nodeNames := map[string]bool{}
	for _, id := range order {
		for _, node := range caches[id].Nodes {
			nodeNames[node.Name] = true
		}
	}
	proxies, err := ownedSequence(main, "proxies")
	if err != nil {
		return nil, err
	}
	groups, err := ownedSequence(main, "proxy-groups")
	if err != nil {
		return nil, err
	}
	// These two fields are the sole mutation boundary, including a failed prefix.
	defer func() {
		yamlx.Set(main, "proxies", proxies)
		yamlx.Set(main, "proxy-groups", groups)
		yamlx.MaterializeAliases(main)
	}()
	providers := yamlx.Get(main, "proxy-providers")
	taken, own := map[string]bool{}, map[string]bool{}
	for _, sequence := range []*yamlx.Node{proxies, groups} {
		for _, item := range sequence.Content {
			taken[proxyName(item)] = true
		}
	}
	created := []DialerGroup{}
	skipped := []DialerSkip{}
	for _, id := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cache, sub := caches[id], subscriptions[id]
		nodes := make([]model.Node, 0, len(cache.Nodes))
		for _, node := range cache.Nodes {
			if node.Name != "" {
				nodes = append(nodes, node)
			}
		}
		visibleNodes := FilterNodes(sub, nodes)
		type chainedNode struct {
			node   model.Node
			target string
		}
		var chained []chainedNode
		for _, node := range visibleNodes {
			if !mentionsDialer(node) {
				continue
			}
			raw, err := ops.NodeDialer(node)
			if err != nil {
				return nil, err
			}
			target, ok := dnsText(raw)
			if ok && !dialerBuiltins[target] && !taken[target] {
				chained = append(chained, chainedNode{node, target})
			}
		}
		if len(chained) == 0 {
			continue
		}
		record, err := SourceDialers(ctx, sub, cache, inputs.DownloadedText)
		if err != nil {
			return nil, err
		}
		airport := airportTargets(record)
		byName := map[string]model.Node{}
		visible := map[string]bool{}
		for _, node := range nodes {
			byName[node.Name] = node
		}
		for _, node := range visibleNodes {
			visible[node.Name] = true
		}
		resolved := map[string]*string{}
		claim := func(name string) error {
			if taken[name] {
				return dre(fmt.Sprintf(msgDialerNameConflict, name, sub.Name))
			}
			taken[name] = true
			own[name] = true
			return nil
		}
		var resolve func(string, []string) (*string, error)
		resolve = func(target string, path []string) (*string, error) {
			if value, ok := resolved[target]; ok {
				return value, nil
			}
			resolved[target] = nil
			if record == nil {
				return nil, nil
			}
			entry, ok := record.Targets.Get(target)
			if !ok || len(entry.Members) == 0 {
				return nil, nil
			}
			for _, previous := range path {
				if previous == target {
					return nil, nil
				}
			}
			for _, name := range entry.Members {
				if _, ok := byName[name]; !ok {
					return nil, nil
				}
			}
			typ := airportGroupType(yamlx.Str(entry.Type))
			group := yamlx.Map(yamlx.Str("name"), yamlx.Str(GroupName(sub.Name, target)), yamlx.Str("type"), yamlx.Str(typ), yamlx.Str("hidden"), yamlx.Bool(true))
			for key, value := range yamlx.Pairs(dialerSettings(entry)) {
				yamlx.Set(group, key, value)
			}
			allVisible := true
			for _, name := range entry.Members {
				if !visible[name] {
					allVisible = false
				}
			}
			copied := false
			if direct[id] && yamlx.Get(providers, sub.Name) != nil && allVisible {
				patterns := make([]string, 0, len(entry.Members))
				for _, name := range entry.Members {
					patterns = append(patterns, namePattern(name))
				}
				yamlx.Set(group, "use", yamlx.Seq(yamlx.Str(sub.Name)))
				yamlx.Set(group, "filter", yamlx.Str("^(?:"+strings.Join(patterns, "|")+")$"))
			} else {
				var pending []*yamlx.Node
				for _, name := range entry.Members {
					copyName := GroupName(sub.Name, name)
					if own[copyName] {
						continue
					}
					if inputs.Convert == nil {
						return nil, &CollaborationError{Operation: "subscription relay conversion"}
					}
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					base, err := inputs.Convert(ctx, byName[name])
					if err != nil {
						return nil, err
					}
					base = yamlx.Resolve(base)
					if base == nil || base.Kind != yaml.MappingNode || len(base.Content) == 0 {
						return nil, nil
					}
					validated, err := yamlx.Encode(base)
					if err != nil {
						return nil, err
					}
					proxy := yamlx.Clone(validated)
					yamlx.Set(proxy, "name", yamlx.Str(copyName))
					if nested, ok := dnsText(yamlx.Get(proxy, "dialer-proxy")); ok && airport[nested] {
						next, err := resolve(nested, append(append([]string(nil), path...), target))
						if err != nil {
							return nil, err
						}
						if next == nil {
							return nil, nil
						}
						yamlx.Set(proxy, "dialer-proxy", yamlx.Str(*next))
					}
					pending = append(pending, proxy)
				}
				for _, proxy := range pending {
					if err := claim(proxyName(proxy)); err != nil {
						return nil, err
					}
					proxies.Content = append(proxies.Content, proxy)
				}
				members := yamlx.Seq()
				for _, name := range entry.Members {
					members.Content = append(members.Content, yamlx.Str(GroupName(sub.Name, name)))
				}
				yamlx.Set(group, "proxies", members)
				copied = true
			}
			if err := claim(proxyName(group)); err != nil {
				return nil, err
			}
			groups.Content = append(groups.Content, group)
			members := append([]string{}, entry.Members...)
			created = append(created, DialerGroup{sub.Name, target, proxyName(group), members, copied})
			name := proxyName(group)
			resolved[target] = &name
			return &name, nil
		}
		for _, item := range chained {
			if airport[item.target] {
				result, err := resolve(item.target, nil)
				if err != nil {
					return nil, err
				}
				if result == nil {
					skipped = append(skipped, DialerSkip{sub.Name, item.node.Name, item.target, "missing"})
				}
			} else if record == nil && !nodeNames[item.target] {
				skipped = append(skipped, DialerSkip{sub.Name, item.node.Name, item.target, "refresh"})
			}
		}
	}
	if len(created) == 0 && len(skipped) == 0 {
		return nil, nil
	}
	shown := skipped
	if len(shown) > MaxReported {
		shown = shown[:MaxReported]
	}
	report := &DialerReport{Groups: created, Skipped: shown, SkippedCount: len(skipped)}
	if len(skipped) > 0 {
		var parts []string
		for i, item := range skipped {
			if i >= 5 {
				break
			}
			parts = append(parts, item.Subscription+"「"+item.Node+"」→ "+item.Target)
		}
		dialersLogger.Warningf(msgDialerMissing, len(skipped), strings.Join(parts, "；"))
	}
	return report, nil
}
