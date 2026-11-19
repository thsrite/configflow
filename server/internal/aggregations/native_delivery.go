package aggregations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/convert"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

// typedDelivery shares one Config and main graph across all aggregation nodes.
type typedDelivery struct {
	config        *model.Config
	sourceFetch   refresh.FetchSourceFn
	main          *yamlx.Node
	snapshot      *provider.DeliverySnapshot
	subscriptions []subscription
	nodes         []node
	sources       map[string]model.Subscription
	records       map[string]*subs.DialersData
}

func (d *typedDelivery) catalogs() ([]subscription, []node) { return d.subscriptions, d.nodes }
func typedResource(id, name string, enabled *bool) resource {
	return resource{ID: id, Name: name, Enabled: enabled == nil || *enabled}
}
func typedNode(value model.Node) node {
	return node{resource: typedResource(value.ID, value.Name, value.Enabled), SubscriptionID: value.SubscriptionID, typed: &value}
}
func newTypedDelivery(config *model.Config, main *yamlx.Node, snapshot *provider.DeliverySnapshot) *typedDelivery {
	d := &typedDelivery{config: config, main: main, snapshot: snapshot, sources: map[string]model.Subscription{}}
	for _, sub := range config.Subscriptions {
		d.subscriptions = append(d.subscriptions, subscription{resource: typedResource(sub.ID, sub.Name, sub.Enabled), URL: sub.URL})
		d.sources[sub.ID] = sub
	}
	for _, value := range config.Nodes {
		d.nodes = append(d.nodes, typedNode(value))
	}
	return d
}
func proxyGraph(proxy Proxy) (*yamlx.Node, error) {
	raw, err := json.Marshal(proxy)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(raw)
}
func graphProxy(graph *yamlx.Node) (Proxy, error) {
	if graph == nil {
		return nil, nil
	}
	raw, err := yamlx.ToJSON(graph)
	if err != nil {
		return nil, err
	}
	var proxy Proxy
	err = json.Unmarshal(raw, &proxy)
	return proxy, err
}
func (d *typedDelivery) fetchSource(ctx context.Context, source subscription, options Options) sourceResult {
	sub := d.sources[source.ID]
	fetch := d.sourceFetch
	if fetch == nil {
		fetch = d.fetch(options.Fetch)
	}
	var fetched *refresh.SourceResult
	var err error
	if options.SourcePrepared {
		fetched, err = fetch(ctx, source.ID, sub.URL)
	} else {
		fetched, err = refresh.FetchSource(ctx, sub, fetch)
	}
	if canceled := ctx.Err(); canceled != nil {
		return sourceResult{err: canceled}
	}
	var proxies []*yamlx.Node
	var nodes []model.Node
	if err == nil {
		if fetched == nil {
			err = errors.New("subscription fetch returned no result")
		} else {
			proxies, err = provider.ParseNodes(fetched.Text, source.Name, "")
		}
		if err == nil {
			nodes, err = substore.ProxyNodes(proxies)
		}
		if err == nil {
			for i := range nodes {
				nodes[i].SubscriptionID = source.ID
				nodes[i].SubscriptionName = source.Name
				if nodes[i].ID == "" {
					nodes[i].ID = "node_" + substore.Default().Env.RandomHex()
				}
			}
		}
	}
	var reference *rules.StrategyReferenceError
	if errors.As(err, &reference) {
		return sourceResult{err: err}
	}
	var updates []provider.CacheWrite
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return sourceResult{err: err}
		}
		logx.Get("backend.routes.aggregations").Warningf(msgSourceFailed, weburl.SafeErrorDetails(err))
		proxies = nil
		cache, cacheErr := substore.LoadCache(ctx, source.ID)
		if cacheErr != nil {
			return sourceResult{err: cacheErr}
		}
		if cache != nil {
			if model.IssueUnder(model.Issues(cache), "nodes") {
				return sourceResult{err: errors.New("cached nodes must be an array")}
			}
			nodes = cache.Nodes
		} else {
			nodes = nil
		}
	} else if fetched.Source != "cache" && fetched.Source != "prepared_yaml" {
		metadata, err := refresh.CacheMetadata(ctx, sub, fetched)
		if err != nil {
			return sourceResult{err: err}
		}
		var typed substore.CacheMetadata
		if metadata != nil {
			data, err := model.Marshal(metadata)
			if err != nil {
				return sourceResult{err: err}
			}
			if err := json.Unmarshal(data, &typed); err != nil {
				return sourceResult{err: err}
			}
		}
		updates = []provider.CacheWrite{{SubID: source.ID, Nodes: nodes, Metadata: &typed}}
	}
	filtered := subs.FilterNodes(sub, nodes)
	if err := convert.ConfigShapes(&model.Config{Nodes: filtered}, false); err != nil {
		return sourceResult{err: convert.DialerFailure(err)}
	}
	// CacheWrite shares this owned node slice: preserve the aggregation owner's
	// established enabled/subscription_id updates for every selected record.
	selected := map[string]bool{}
	for _, value := range filtered {
		selected[value.ID+"\x00"+value.Name] = true
	}
	enabled := true
	for i := range nodes {
		if selected[nodes[i].ID+"\x00"+nodes[i].Name] {
			nodes[i].SubscriptionID = source.ID
			nodes[i].Enabled = &enabled
		}
	}
	projected := make([]node, 0, len(filtered))
	for _, value := range filtered {
		value.SubscriptionID = source.ID
		value.Enabled = &enabled
		projected = append(projected, typedNode(value))
	}
	var nativeProxies []Proxy
	for _, graph := range subs.FilterProxies(sub, proxies) {
		proxy, err := graphProxy(graph)
		if err != nil {
			return sourceResult{err: err}
		}
		nativeProxies = append(nativeProxies, proxy)
	}
	return sourceResult{nodes: projected, proxies: nativeProxies, updates: updates}
}
func (d *typedDelivery) fetch(fetch Fetch) refresh.FetchSourceFn {
	if fetch == nil {
		return provider.DefaultDeliveryEnv().Fetch
	}
	return sourceFetch(fetch)
}
func (d *typedDelivery) captureRecords(ctx context.Context, selected map[string]bool) error {
	d.records = map[string]*subs.DialersData{}
	for _, sub := range d.config.Subscriptions {
		if selected[sub.ID] {
			record, err := provider.DialerRecord(ctx, sub)
			if err != nil {
				return err
			}
			d.records[sub.ID] = record
		}
	}
	return ctx.Err()
}
func (d *typedDelivery) convertNode(ctx context.Context, value node) (Proxy, error) {
	var n model.Node
	if value.typed != nil {
		n = *value.typed
	} else if err := json.Unmarshal(value.raw, &n); err != nil {
		return nil, err
	}
	graph, err := convert.NodeProxy(ctx, n, convert.OptionalNodeConverter(substore.NativeConverter()))
	if err != nil {
		return nil, err
	}
	graph, err = convert.ApplyNodeDialer(d.config, n, graph)
	if err != nil {
		return nil, convert.DialerFailure(err)
	}
	return graphProxy(graph)
}
func (d *typedDelivery) adapt(id string, proxy Proxy, chains bool) (Proxy, error) {
	graph, err := proxyGraph(proxy)
	if err != nil {
		return nil, err
	}
	graphs, err := subs.AdaptProxies(d.sources[id].Name, []*yamlx.Node{graph}, d.main, d.records[id], chains)
	if err != nil {
		return nil, err
	}
	if len(graphs) == 0 {
		return nil, nil
	}
	return graphProxy(graphs[0])
}
func (d *typedDelivery) validate(proxies []Proxy, name, format string) error {
	graphs := make([]*yamlx.Node, 0, len(proxies))
	for _, proxy := range proxies {
		graph, err := proxyGraph(proxy)
		if err != nil {
			return err
		}
		graphs = append(graphs, graph)
	}
	if err := d.snapshot.Validate(graphs, false, &name); err != nil {
		return err
	}
	chains, err := d.snapshot.HasChains()
	if err != nil {
		return err
	}
	for _, graph := range graphs {
		if target := yamlx.Get(graph, "dialer-proxy"); target != nil && target.ShortTag() != "!!null" {
			chains = true
		}
	}
	if chains && (format == "surge" || format == "loon") {
		client := "Surge"
		if format == "loon" {
			client = "Loon"
		}
		return &rules.StrategyReferenceError{Message: fmt.Sprintf(msgUnsupportedChain, client)}
	}
	return nil
}

// RenderProvider binds the typed provider callback without re-decoding Config in
// each aggregation or each node. Existing HTTP adapters remain separate codecs.
func RenderProvider(ctx context.Context, agg model.Aggregation, config *model.Config, main *yamlx.Node, snapshot *provider.DeliverySnapshot, fetch refresh.FetchSourceFn) (*provider.Artifact, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := convert.ConfigDialers(config); err != nil {
		return nil, convert.DialerFailure(err)
	}
	var err error
	if snapshot == nil {
		snapshot, err = provider.CaptureDelivery(config.ProfileID, main, config.Profile)
		if err != nil {
			return nil, err
		}
	}
	d := newTypedDelivery(config, main, snapshot)
	r := typedResource(agg.ID, agg.Name, agg.Enabled)
	a := aggregation{resource: r, Subscriptions: agg.Subscriptions, Nodes: agg.Nodes}
	if agg.RegexFilter != nil {
		a.Regex, err = json.Marshal(*agg.RegexFilter)
		if err != nil {
			return nil, err
		}
	}
	// The render interface predates native fetch results. Carry its typed callback
	// directly in the delivery, rather than round-tripping source traffic.
	d.sourceFetch = fetch
	format, _ := ctx.Value(formatKey{}).(string)
	result, err := render(ctx, a, d, Options{SourcePrepared: true, Format: format})
	if err != nil {
		return nil, err
	}
	extra, err := json.Marshal(struct {
		FilePath *string        `json:"file_path"`
		Counts   map[string]int `json:"subscription_node_counts"`
		Total    int            `json:"total_count"`
	}{result.FilePath, result.SubscriptionNodeCounts, result.TotalCount})
	if err != nil {
		return nil, err
	}
	return &provider.Artifact{Name: result.Name, HasName: true, Content: result.Content, CacheUpdates: result.updates, Extra: extra}, nil
}

func logSourcePanic() { logx.Get("backend.routes.aggregations").Warning(msgSourcePanic) }
