package aggregations

// This is the boundary to the repository and existing provider collaborators.
// Collection/rendering code uses native types and does not access legacy values.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/weburl"
)

// Fetch provides one subscription body without publishing a provider artifact.
type Fetch = refresh.SourceFetch

type cacheUpdate = provider.CacheUpdate

type delivery struct {
	config, main  *jsonx.Object
	profileID     string
	snapshot      *provider.Snapshot
	subscriptions []subscription
	nodes         []node
	records       map[string]*jsonx.Object
	subNames      map[string]string
}

func legacyObject(raw json.RawMessage) (*jsonx.Object, error) {
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return nil, errors.New("aggregation document must be an object")
	}
	return object, nil
}
func native(value any, target any) error {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
func proxyValues(proxies []Proxy) ([]any, error) {
	raw, err := json.Marshal(proxies)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	list, ok := value.([]any)
	if !ok {
		return nil, errors.New("provider proxies must be an array")
	}
	return list, nil
}
func projection(value *jsonx.Object) (resource, error) {
	raw, err := jsonx.Compact(value)
	return resource{ID: value.GetString("id"), Name: value.GetString("name"), Enabled: jsonx.Truthy(value.GetOr("enabled", true)), raw: raw}, err
}
func projections(list []any) ([]resource, error) {
	result := make([]resource, 0, len(list))
	for _, value := range list {
		object, ok := value.(*jsonx.Object)
		if !ok {
			return nil, errors.New("source node must be an object")
		}
		item, err := projection(object)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}
func subscriptionProjections(list []any) ([]subscription, error) {
	bases, err := projections(list)
	if err != nil {
		return nil, err
	}
	result := make([]subscription, 0, len(bases))
	for i, base := range bases {
		result = append(result, subscription{base, list[i].(*jsonx.Object).GetString("url")})
	}
	return result, nil
}
func nodeProjections(list []any) ([]node, error) {
	bases, err := projections(list)
	if err != nil {
		return nil, err
	}
	result := make([]node, 0, len(bases))
	for i, base := range bases {
		result = append(result, node{base, list[i].(*jsonx.Object).GetString("subscription_id")})
	}
	return result, nil
}

func selectedIDs(object *jsonx.Object, key string) ([]string, error) {
	value, present := object.Get(key)
	if !present || value == nil && key == "nodes" {
		return []string{}, nil
	}
	if value == nil {
		return nil, fmt.Errorf("aggregation %s must be an array", key)
	}
	var ids []string
	if err := native(value, &ids); err != nil {
		return nil, fmt.Errorf("decode aggregation %s: %w", key, err)
	}
	return ids, nil
}

// Generate validates a frozen main before fetching sources. Persist publishes
// through the shared all-or-nothing provider bundle; dry renders never commit.
func Generate(ctx context.Context, rawAggregation, rawConfig json.RawMessage, options Options) (*Result, error) {
	ctx = context.WithoutCancel(ctx)
	if options.Format != "" {
		ctx = WithFormat(ctx, options.Format)
	} else {
		options.Format, _ = ctx.Value(formatKey{}).(string)
	}
	agg, err := legacyObject(rawAggregation)
	if err != nil {
		return nil, err
	}
	config, err := legacyObject(rawConfig)
	if err != nil {
		return nil, err
	}
	if err := convert.ValidateDialers(config); err != nil {
		return nil, err
	}
	profileID := config.GetString("profile_id")
	if options.Persist {
		profileID, err = repo.ResolveProfileID(ctx, profileID)
	} else if profileID == "" {
		profileID, err = repo.GetDefaultProfileID(ctx)
	}
	if err != nil {
		return nil, err
	}
	ctx = convert.EnsureNodeConversions(ctx)
	var main *jsonx.Object
	if len(options.Main) > 0 && string(options.Main) != "null" {
		main, err = legacyObject(options.Main)
	} else {
		main, err = convert.GenerateMihomoDocument(ctx, config, convert.GenerateOptions{SkipProviderPreflight: true})
	}
	if err != nil {
		return nil, err
	}
	snapshot, err := provider.Capture(profileID, main, config)
	if err != nil {
		return nil, err
	}
	d := &delivery{config: config, main: main, profileID: profileID, snapshot: snapshot}
	if options.Persist {
		return d.publish(ctx, agg, options)
	}
	if _, ok := agg.Value("name").(string); !ok {
		return nil, errors.New("aggregation name must be a string")
	}
	base, err := projection(agg)
	if err != nil {
		return nil, err
	}
	a := aggregation{resource: base}
	a.Subscriptions, err = selectedIDs(agg, "subscriptions")
	if err != nil {
		return nil, err
	}
	a.Nodes, err = selectedIDs(agg, "nodes")
	if err != nil {
		return nil, err
	}
	if agg.Has("regex_filter") {
		a.Regex, err = jsonx.Compact(agg.Value("regex_filter"))
		if err != nil {
			return nil, err
		}
	}
	d.subscriptions, err = subscriptionProjections(config.GetList("subscriptions"))
	if err != nil {
		return nil, err
	}
	d.nodes, err = nodeProjections(config.GetList("nodes"))
	if err != nil {
		return nil, err
	}
	return render(ctx, a, d, options)
}

func defaultFetch(ctx context.Context, id, url string) (*substore.SubscriptionResult, error) {
	return substore.Default().Client.GetSubscriptionProxiesYAML(ctx, id, url, "", substore.FetchOptions{})
}

func (d *delivery) fetchSource(ctx context.Context, source subscription, options Options) sourceResult {
	sub, err := legacyObject(source.raw)
	if err != nil {
		return sourceResult{err: err}
	}
	fetch := options.Fetch
	if fetch == nil {
		fetch = defaultFetch
	}
	var fetched *substore.SubscriptionResult
	if options.SourcePrepared {
		fetched, err = fetch(ctx, source.ID, sub.GetString("url"))
	} else {
		fetched, err = refresh.FetchSubscriptionSource(ctx, sub, fetch)
	}
	var proxies, nodes []any
	if err == nil {
		if fetched == nil {
			err = errors.New("subscription fetch returned no result")
		} else {
			proxies, err = provider.ParseProviderProxies(fetched.Text, source.Name, "")
		}
		if err == nil {
			nodes, err = substore.ProxiesToNodes(proxies)
		}
		if err == nil {
			for _, value := range nodes {
				n := value.(*jsonx.Object)
				n.Set("subscription_id", source.ID)
				n.Set("subscription_name", source.Name)
				if !n.Has("id") {
					n.Set("id", "node_"+substore.Default().Env.RandomHex())
				}
			}
		}
	}
	var reference *rules.StrategyReferenceError
	if errors.As(err, &reference) {
		return sourceResult{err: err}
	}
	var updates []provider.CacheUpdate
	if err != nil {
		logx.Get("backend.routes.aggregations").Warningf(msgSourceFailed, weburl.SafeErrorDetails(err))
		proxies = nil
		cached, cacheErr := substore.LoadSubscriptionCache(ctx, source.ID)
		if cacheErr != nil {
			return sourceResult{err: cacheErr}
		}
		nodes = []any{}
		if cached != nil {
			value := cached.GetOr("nodes", []any{})
			if value != nil {
				var ok bool
				nodes, ok = value.([]any)
				if !ok {
					return sourceResult{err: errors.New("cached nodes must be an array")}
				}
			}
		}
	} else if fetched.Source != "cache" && fetched.Source != "prepared_yaml" {
		metadata, err := refresh.SourceCacheMetadata(sub, fetched)
		if err != nil {
			return sourceResult{err: err}
		}
		updates = []provider.CacheUpdate{{SubID: source.ID, Nodes: nodes, Metadata: metadata}}
	}
	filteredNodes := subs.FilterSubscriptionItems(sub, nodes)
	if err := convert.ValidateShapes(jsonx.Obj("nodes", filteredNodes), false); err != nil {
		return sourceResult{err: err}
	}
	for _, value := range filteredNodes {
		n := value.(*jsonx.Object)
		n.Set("subscription_id", source.ID)
		n.Set("enabled", true)
	}
	projected, err := nodeProjections(filteredNodes)
	if err != nil {
		return sourceResult{err: err}
	}
	var nativeProxies []Proxy
	if proxies != nil {
		if err := native(subs.FilterSubscriptionItems(sub, proxies), &nativeProxies); err != nil {
			return sourceResult{err: err}
		}
	}
	return sourceResult{nodes: projected, proxies: nativeProxies, updates: updates}
}

func (d *delivery) captureRecords(ctx context.Context, selected map[string]bool) error {
	d.records, d.subNames = map[string]*jsonx.Object{}, map[string]string{}
	for _, source := range d.subscriptions {
		d.subNames[source.ID] = source.Name
		if !selected[source.ID] {
			continue
		}
		sub, err := legacyObject(source.raw)
		if err != nil {
			return err
		}
		record, err := provider.SubscriptionDialerRecord(ctx, sub)
		if err != nil {
			return err
		}
		if record != nil {
			record = record.DeepCopy()
		}
		d.records[source.ID] = record
	}
	return nil
}

func (d *delivery) convertNode(ctx context.Context, n node) (Proxy, error) {
	node, err := legacyObject(n.raw)
	if err != nil {
		return nil, err
	}
	proxy, err := convert.ConvertNodeToMihomo(ctx, node, substore.Converter())
	if err != nil {
		return nil, err
	}
	proxy, err = convert.OverlayDialer(d.config, node, proxy)
	if err != nil {
		return nil, err
	}
	if proxy == nil {
		return nil, nil
	}
	var out Proxy
	err = native(proxy, &out)
	return out, err
}

func (d *delivery) adapt(id string, proxy Proxy, chains bool) (Proxy, error) {
	values, err := proxyValues([]Proxy{proxy})
	if err != nil {
		return nil, err
	}
	adapted := subs.AdaptSubscriptionProxies(d.subNames[id], values, d.main, d.records[id], chains)
	if len(adapted) == 0 {
		return nil, nil
	}
	var out Proxy
	err = native(adapted[0], &out)
	return out, err
}

func (d *delivery) validate(proxies []Proxy, name, format string) error {
	values, err := proxyValues(proxies)
	if err != nil {
		return err
	}
	if err := d.snapshot.Validate(values, false, &name); err != nil {
		return err
	}
	chained, err := d.snapshot.HasChains()
	if err != nil {
		return err
	}
	for _, value := range values {
		if value.(*jsonx.Object).Value("dialer-proxy") != nil {
			chained = true
		}
	}
	if chained && (format == "surge" || format == "loon") {
		client := "Surge"
		if format == "loon" {
			client = "Loon"
		}
		return &rules.StrategyReferenceError{Message: fmt.Sprintf(msgUnsupportedChain, client)}
	}
	return nil
}

func (d *delivery) publish(ctx context.Context, agg *jsonx.Object, options Options) (*Result, error) {
	bundle, err := provider.PrepareProviderBundle(ctx, d.config, d.main, provider.BundleOptions{Requested: &provider.Requested{Kind: "aggregation", Resource: agg}, SubscriptionFetch: options.Fetch})
	if err != nil {
		return nil, err
	}
	var found *provider.Rendered
	for _, item := range bundle {
		if item.Name == agg.GetString("name") {
			found = item
			break
		}
	}
	if found == nil {
		return nil, errors.New("aggregation absent from validated provider bundle")
	}
	if err := provider.CommitProviderBundle(ctx, d.profileID, bundle); err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	path, err := r.ProfilePath(d.profileID, "providers/"+agg.GetString("id")+".yaml")
	if err != nil {
		return nil, err
	}
	result := &Result{Name: found.Name, Content: found.Content, FilePath: &path, SubscriptionNodeCounts: map[string]int{}}
	if found.Extra != nil {
		if err := native(found.Extra, result); err != nil {
			return nil, err
		}
		result.FilePath = &path
		result.Content = found.Content
	}
	values, err := provider.ParseProviderProxies(found.Content, found.Name, path)
	if err != nil {
		return nil, err
	}
	if err := native(values, &result.Proxies); err != nil {
		return nil, err
	}
	result.TotalCount = len(result.Proxies)
	return result, nil
}

// Wire registers aggregation rendering for conversions and provider bundles.
func Wire() {
	provider.SetAggregationProvider(func(ctx context.Context, agg, config, main *jsonx.Object, fetch refresh.SourceFetch) (*provider.Rendered, error) {
		a, err := jsonx.Compact(agg)
		if err != nil {
			return nil, err
		}
		c, err := jsonx.Compact(config)
		if err != nil {
			return nil, err
		}
		m, err := jsonx.Compact(main)
		if err != nil {
			return nil, err
		}
		result, err := Generate(ctx, a, c, Options{Main: m, Fetch: fetch, SourcePrepared: true})
		if err != nil {
			return nil, err
		}
		counts := jsonx.NewObject()
		for id, count := range result.SubscriptionNodeCounts {
			counts.Set(id, jsonx.Int(int64(count)))
		}
		return &provider.Rendered{Name: result.Name, HasName: true, Content: result.Content, CacheUpdates: result.updates, Extra: jsonx.Obj("file_path", nil, "subscription_node_counts", counts, "total_count", jsonx.Int(int64(result.TotalCount)))}, nil
	})
}

func logSourcePanic() { logx.Get("backend.routes.aggregations").Warning(msgSourcePanic) }
