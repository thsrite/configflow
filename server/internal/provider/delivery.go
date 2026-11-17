package provider

import (
	"context"
	"strings"
	"sync"

	"configflow/internal/agents/diag"
	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// CacheUpdate is one staged node-cache update (sub_id, nodes, metadata),
// written by CommitCacheUpdates only after the whole bundle validated.
type CacheUpdate struct {
	SubID    string
	Nodes    []any
	Metadata *jsonx.Object
}

// Rendered is one rendered provider: the dicts prepare_subscription_provider,
// generate_aggregation_provider and prepare_provider_bundle return.
type Rendered struct {
	// Name is the 'name' key; HasName is false where Python's dict has none
	// ({'content': '', 'cache_updates': []} of a transport fallback).
	Name    string
	HasName bool
	// Content is the provider YAML ('' = unverified URL fallback).
	Content      string
	CacheUpdates []CacheUpdate
	// AggregationID is 'aggregation_id' ("" = absent).
	AggregationID string
	// URL and LocalPath are the bundle's 'url' and 'local_path' (the
	// definition's url and path); LocalPath nil = absent.
	URL       string
	LocalPath any
	// Extra holds the other keys of the Python dict (e.g. the counts
	// generate_aggregation_provider returns); may be nil.
	Extra *jsonx.Object
}

func (r *Rendered) clone() *Rendered {
	c := *r
	c.CacheUpdates = append([]CacheUpdate(nil), r.CacheUpdates...)
	return &c
}

// nameValue is provider.get('name', def).
func (r *Rendered) nameValue(def any) any {
	if r.HasName {
		return r.Name
	}
	return def
}

func (r *Rendered) localPath() any {
	if r.LocalPath == nil {
		return ""
	}
	return r.LocalPath
}

// Snapshot is DeliverySnapshot: the main configuration and chain exits of
// one delivery, captured before any remote fetch.
type Snapshot struct {
	ProfileID string
	// MainJSON is json.dumps(main); every use parses it again (a fresh copy).
	MainJSON string
	// ChainNames is the frozenset of enabled chain group names.
	ChainNames []any
}

// Capture is DeliverySnapshot.capture(profile_id, main, profile=profile);
// profile may be nil.
func Capture(profileID string, main, profile *jsonx.Object) (*Snapshot, error) {
	var names []any
	if profile != nil && profile.Len() > 0 {
		groups, err := pyIter(profile.GetOr("proxy_groups", []any{}))
		if err != nil {
			return nil, err
		}
		for _, item := range groups {
			g, ok := item.(*jsonx.Object)
			if !ok {
				return nil, attributeError(item, "get")
			}
			if !jsonx.Equal(g.Value("type"), "chain") || !jsonx.Truthy(g.GetOr("enabled", true)) {
				continue
			}
			name, present := g.Get("name")
			if !present {
				return nil, keyError("name")
			}
			if !hashable(name) {
				return nil, typeError("unhashable type: '" + pyTypeName(name) + "'")
			}
			if !containsEqual(names, name) {
				names = append(names, name)
			}
		}
	}
	text, err := jsonx.Encode(mainValue(main), jsonx.Options{EnsureASCII: true})
	if err != nil {
		return nil, err
	}
	return &Snapshot{ProfileID: profileID, MainJSON: string(text), ChainNames: names}, nil
}

func mainValue(main *jsonx.Object) any {
	if main == nil {
		return nil
	}
	return main
}

// Main is json.loads(self.main_json): a fresh copy of the captured main.
func (s *Snapshot) Main() (*jsonx.Object, error) {
	value, err := jsonx.Parse([]byte(s.MainJSON))
	if err != nil {
		return nil, err
	}
	main, ok := value.(*jsonx.Object)
	if !ok {
		return nil, attributeError(value, "get")
	}
	return main, nil
}

// HasChains is the has_chains property.
func (s *Snapshot) HasChains() (bool, error) {
	if len(s.ChainNames) > 0 {
		return true, nil
	}
	main, err := s.Main()
	if err != nil {
		return false, err
	}
	proxies, err := pyIter(main.GetOr("proxies", []any{}))
	if err != nil {
		return false, err
	}
	for _, item := range proxies {
		p, ok := item.(*jsonx.Object)
		if !ok {
			return false, attributeError(item, "get")
		}
		if p.Value("dialer-proxy") != nil {
			return true, nil
		}
	}
	providersValue := main.GetOr("proxy-providers", jsonx.NewObject())
	providers, ok := providersValue.(*jsonx.Object)
	if !ok {
		return false, attributeError(providersValue, "values")
	}
	for _, item := range providers.Values() {
		p, ok := item.(*jsonx.Object)
		if !ok {
			return false, attributeError(item, "get")
		}
		overrideValue := p.GetOr("override", jsonx.NewObject())
		override, ok := overrideValue.(*jsonx.Object)
		if !ok {
			return false, attributeError(overrideValue, "get")
		}
		if jsonx.Truthy(override.Value("dialer-proxy")) {
			return true, nil
		}
	}
	return false, nil
}

// Adapt is snapshot.adapt(sub, proxies, chains=chains).
func (s *Snapshot) Adapt(ctx context.Context, sub *jsonx.Object, proxies []any, chains bool) ([]any, error) {
	return AdaptDelivered(ctx, s, sub, proxies, chains)
}

// ProviderName converts sub['name'] to the provider_name argument of
// Validate: nil (None) for anything but a string, which can never name a
// provider definition.
func ProviderName(name any) *string {
	if s, ok := name.(string); ok {
		return &s
	}
	return nil
}

// Validate is snapshot.validate(proxies, force_chains=, provider_name=):
// provider shapes, cross-artifact chain exit names and the emitted dialer
// topology with this provider's payload.
func (s *Snapshot) Validate(proxies any, forceChains bool, providerName *string) error {
	if err := ValidateProxyShapes(proxies); err != nil {
		return err
	}
	mainValue, err := jsonx.Parse([]byte(s.MainJSON))
	if err != nil {
		return err
	}
	main, isObject := mainValue.(*jsonx.Object)
	list := proxies.([]any)
	var names []any
	for _, p := range list {
		name, _ := providerMember(p, "name")
		names = append(names, name)
	}
	for _, name := range names {
		if containsEqual(s.ChainNames, name) {
			return dialerErr("Provider 节点名称与代理链出口冲突")
		}
	}
	if !isObject {
		if providerName != nil {
			return attributeError(mainValue, "get")
		}
		return typeError("'" + pyTypeName(mainValue) + "' object is not a mapping")
	}
	definitions, _ := main.GetOr("proxy-providers", jsonx.NewObject()).(*jsonx.Object)
	if providerName != nil && definitions.Has(*providerName) {
		sourceValue := definitions.GetOr(*providerName, jsonx.NewObject())
		payloads := jsonx.NewObject()
		for name, value := range definitions.All() {
			if name == *providerName {
				payloads.Set(name, list)
				continue
			}
			// source.get('url') is only evaluated for the other definitions.
			source, ok := sourceValue.(*jsonx.Object)
			if !ok {
				return attributeError(sourceValue, "get")
			}
			if !jsonx.Truthy(source.Value("url")) {
				continue
			}
			definition, ok := value.(*jsonx.Object)
			if !ok {
				return attributeError(value, "get")
			}
			if jsonx.Equal(definition.Value("url"), source.Value("url")) {
				payloads.Set(name, list)
			}
		}
		chains, err := s.HasChains()
		if err != nil {
			return err
		}
		return convert.ValidateEmitted(main, convert.EmitOptions{ForceChains: chains || forceChains, ProviderProxies: payloads})
	}
	mainProxies, err := pyIter(main.GetOr("proxies", []any{}))
	if err != nil {
		return err
	}
	combined := []any{}
	for _, item := range mainProxies {
		p, ok := item.(*jsonx.Object)
		if !ok {
			return subscriptError(item)
		}
		name, present := p.Get("name")
		if !present {
			return keyError("name")
		}
		if !containsEqual(names, name) {
			combined = append(combined, p)
		}
	}
	combined = append(combined, list...)
	emitted := main.Clone()
	emitted.Set("proxies", combined)
	chains, err := s.HasChains()
	if err != nil {
		return err
	}
	return convert.ValidateEmitted(emitted, convert.EmitOptions{ForceChains: chains || forceChains})
}

// AdaptDelivered is adapt_delivered(snapshot, sub, proxies, chains=): a
// subscription's own chained nodes as delivered with the snapshot's main
// configuration. A nil snapshot stands for an object without main_json.
func AdaptDelivered(ctx context.Context, snapshot *Snapshot, sub *jsonx.Object, proxies []any, chains bool) ([]any, error) {
	if snapshot == nil || !jsonx.Truthy(sub.Value("id")) {
		return proxies, nil
	}
	name := sub.GetOr("name", "")
	main, err := snapshot.Main()
	if err != nil {
		return nil, err
	}
	record, err := dialerRecord(ctx, sub)
	if err != nil {
		return nil, err
	}
	subscription, ok := name.(string)
	if !ok {
		subscription = jsonx.Str(name)
	}
	return subs.AdaptSubscriptionProxies(subscription, proxies, main, record, chains), nil
}

// dialerRecord is subscription_dialers.dialer_record(sub) with its cache
// and download-cache reads.
func dialerRecord(ctx context.Context, sub *jsonx.Object) (*jsonx.Object, error) {
	id, present := sub.Get("id")
	if !present {
		return nil, keyError("id")
	}
	cache, err := substore.LoadSubscriptionCache(ctx, idText(id))
	if err != nil {
		return nil, err
	}
	return subs.DialerRecord(sub, cache, func(url string) (string, bool) {
		r, err := repo.GetRepository(ctx)
		if err != nil {
			return "", false
		}
		downloaded, err := r.ReadSharedJSON(ctx, refresh.DownloadCachePath(url))
		if err != nil || downloaded == nil {
			return "", false
		}
		text, ok := downloaded.Value("text").(string)
		return text, ok
	})
}

// SubscriptionDialerRecord reads one subscription's cached airport topology so
// aggregation renderers can freeze it once for every proxy in the snapshot.
func SubscriptionDialerRecord(ctx context.Context, sub *jsonx.Object) (*jsonx.Object, error) {
	return dialerRecord(ctx, sub)
}

func idText(id any) string {
	if s, ok := id.(string); ok {
		return s
	}
	return jsonx.Str(id)
}

// ValidateProxyShapes is validate_proxy_shapes(proxies).
func ValidateProxyShapes(proxies any) error {
	list, ok := proxies.([]any)
	if !ok {
		return shapeError("Provider proxies 必须是数组", "proxies")
	}
	for index, proxy := range list {
		isDict := providerMapping(proxy)
		name, hasName := providerMember(proxy, "name")
		text, isText := name.(string)
		if !isDict || !isText || pystr.Strip(text) == "" {
			if isDict && hasName {
				return shapeError("Provider 代理必须是含非空 name 的对象", "proxies", index, "name")
			}
			return shapeError("Provider 代理必须是含非空 name 的对象", "proxies", index)
		}
		if refs, present := providerMember(proxy, "proxies"); present {
			refList, ok := refs.([]any)
			bad := !ok
			for _, item := range refList {
				s, ok := item.(string)
				if !ok || pystr.Strip(s) == "" {
					bad = true
					break
				}
			}
			if bad {
				return shapeError("Provider proxies 引用必须是字符串数组", "proxies", index, "proxies")
			}
		}
		if kind, present := providerMember(proxy, "type"); present {
			if _, ok := kind.(string); !ok {
				return shapeError("Provider type 必须是字符串", "proxies", index, "type")
			}
		}
	}
	return nil
}

// loadProviderProxies is _load_provider_proxies(text, name=, path=, kind=,
// source_label=). Content errors carry their source and owner; other load
// errors (Python: not yaml.YAMLError) are returned unchanged.
func loadProviderProxies(text string, name, path any, kind, sourceLabel string) ([]any, error) {
	data, err := yamlx.Parse([]byte(text))
	if err != nil {
		if cause := yamlCause(err, text); cause != nil {
			return nil, providerContext(providerSourceError(err, cause, text, sourceLabel), name, path, kind)
		}
		return nil, err
	}
	if data.Kind != yaml.MappingNode {
		e := dialerErr("Provider 必须是 YAML 对象\n字段：文档根节点")
		return nil, providerContext(providerSourceError(e, nil, text, sourceLabel), name, path, kind)
	}
	proxyNode := yamlx.Get(data, "proxies")
	if proxyNode == nil {
		return []any{}, nil
	}
	encoded, err := yamlx.ToJSON(proxyNode)
	if err != nil {
		return nil, providerContext(providerSourceError(err, yamlCause(err, text), text, sourceLabel), name, path, kind)
	}
	proxies, err := jsonx.Parse(encoded)
	if err != nil {
		return nil, err
	}
	if err := ValidateProxyShapes(proxies); err != nil {
		return nil, providerContext(providerSourceError(err, nil, text, sourceLabel), name, path, kind)
	}
	return proxies.([]any), nil
}

// ParseProviderProxies is parse_provider_proxies(text, name=, path=):
// reject malformed metadata before legacy field normalization can mask it,
// then substore.ParseProxiesFromYAML.
func ParseProviderProxies(text string, name, path any) ([]any, error) {
	if _, err := loadProviderProxies(text, orEmpty(name), orEmpty(path), "订阅", "本次订阅返回的内容"); err != nil {
		return nil, err
	}
	return substore.ParseProxiesFromYAML(text)
}

func orEmpty(v any) any {
	if v == nil {
		return ""
	}
	return v
}

// PrepareOptions are the keyword arguments of prepare_subscription_provider.
type PrepareOptions struct {
	// Fetch is fetch(sub_id, url); nil = Env.Fetch.
	Fetch                  refresh.SourceFetch
	AllowTransportFallback bool
	// SourcePath is source_path (nil = '').
	SourcePath any
	// SourcePrepared means Fetch already resolves the bundle's captured
	// source under its refresh policy.
	SourcePrepared bool
}

// PrepareSubscriptionProvider is prepare_subscription_provider(sub, config,
// snapshot, ...): prepare without publishing node caches, retaining the
// subscription's name on failure. The result's CacheUpdates are staged.
func PrepareSubscriptionProvider(ctx context.Context, sub, config *jsonx.Object, snapshot *Snapshot, opts PrepareOptions) (*Rendered, error) {
	// PORTING rule 4b: Python finishes the fetch and validation after the
	// client went away; only the fetches' own timeouts end them.
	ctx = context.WithoutCancel(ctx)
	opts.SourcePath = orEmpty(opts.SourcePath)
	result, err := prepareSubscriptionProvider(ctx, sub, snapshot, opts)
	if err != nil && isStrategyReference(err) {
		return nil, providerContext(err, sub.GetOr("name", ""), opts.SourcePath, "订阅")
	}
	return result, err
}

func requiredKey(o *jsonx.Object, key string) (any, error) {
	v, present := o.Get(key)
	if !present {
		return nil, keyError(key)
	}
	return v, nil
}

func prepareSubscriptionProvider(ctx context.Context, sub *jsonx.Object, snapshot *Snapshot, opts PrepareOptions) (*Rendered, error) {
	env := currentEnv()
	fetch := opts.Fetch
	if fetch == nil {
		fetch = env.Fetch
	}
	var update *CacheUpdate
	var proxies []any
	have := false
	var transportErr error
	if jsonx.Truthy(sub.Value("url")) {
		err := func() error {
			var result *substore.SubscriptionResult
			var err error
			if opts.SourcePrepared {
				id, err := requiredKey(sub, "id")
				if err != nil {
					return err
				}
				result, err = fetch(ctx, idText(id), idText(sub.Value("url")))
				if err != nil {
					return err
				}
			} else if result, err = refresh.FetchSubscriptionSource(ctx, sub, fetch); err != nil {
				return err
			}
			raw, err := ParseProviderProxies(result.Text, sub.GetOr("name", ""), opts.SourcePath)
			if err != nil {
				return err
			}
			adapted, err := AdaptDelivered(ctx, snapshot, sub, subs.FilterSubscriptionItems(sub, raw), true)
			if err != nil {
				return err
			}
			name, err := requiredKey(sub, "name")
			if err != nil {
				return err
			}
			if err := snapshot.Validate(adapted, false, ProviderName(name)); err != nil {
				return err
			}
			if len(raw) > 0 && result.Source != "cache" && result.Source != "prepared_yaml" {
				nodes, err := substore.ProxiesToNodes(raw)
				if err != nil {
					return err
				}
				id, err := requiredKey(sub, "id")
				if err != nil {
					return err
				}
				for _, item := range nodes {
					node := item.(*jsonx.Object)
					node.Set("subscription_id", id)
					node.Set("subscription_name", name)
					if !node.Has("id") {
						node.Set("id", env.NodeID())
					}
				}
				metadata, err := refresh.SourceCacheMetadata(sub, result)
				if err != nil {
					return err
				}
				update = &CacheUpdate{SubID: idText(id), Nodes: nodes, Metadata: metadata}
			}
			proxies, have = adapted, true
			return nil
		}()
		if err != nil {
			if isStrategyReference(err) {
				return nil, err
			}
			transportErr, proxies, have = err, nil, false
		}
	}
	if !have {
		id, err := requiredKey(sub, "id")
		if err != nil {
			return nil, err
		}
		cache, err := substore.LoadSubscriptionCache(ctx, idText(id))
		if err != nil {
			return nil, err
		}
		if cache == nil || cache.Len() == 0 || !jsonx.Truthy(cache.Value("nodes")) {
			if opts.AllowTransportFallback {
				chains, err := snapshot.HasChains()
				if err != nil {
					return nil, err
				}
				if !chains {
					return &Rendered{Content: "", CacheUpdates: []CacheUpdate{}}, nil
				}
			}
			detail := ""
			if transportErr != nil {
				detail = "\n原始诊断：" + diag.FailureText(transportErr.Error(), nil, 0)
			}
			return nil, dialerErr("订阅没有可用缓存或远程代理" + detail)
		}
		nodes := cache.Value("nodes")
		if err := convert.ValidateShapes(jsonx.Obj("nodes", nodes), false); err != nil {
			return nil, err
		}
		list, ok := nodes.([]any)
		if !ok {
			return nil, typeError("'" + pyTypeName(nodes) + "' object is not iterable")
		}
		converted := []any{}
		for _, item := range subs.FilterSubscriptionItems(sub, list) {
			node, ok := item.(*jsonx.Object)
			if !ok {
				return nil, attributeError(item, "get")
			}
			p, err := convert.ConvertNodeToMihomo(ctx, node, env.converter())
			if err != nil {
				return nil, err
			}
			if p != nil && p.Len() > 0 {
				converted = append(converted, p)
			}
		}
		if proxies, err = AdaptDelivered(ctx, snapshot, sub, converted, true); err != nil {
			return nil, err
		}
	}
	name, err := requiredKey(sub, "name")
	if err != nil {
		return nil, err
	}
	if err := snapshot.Validate(proxies, false, ProviderName(name)); err != nil {
		return nil, err
	}
	content, err := yamlx.IndentDump(jsonx.Obj("proxies", proxies))
	if err != nil {
		return nil, err
	}
	updates := []CacheUpdate{}
	if update != nil {
		updates = append(updates, *update)
	}
	return &Rendered{Name: idText(name), HasName: true, Content: content, CacheUpdates: updates}, nil
}

// ValidateRenderedBundle is validate_rendered_bundle(snapshot, providers):
// validate all exact provider collections with one monotonic activation;
// never fetch or render again. It returns whether dialer chains are active.
func ValidateRenderedBundle(snapshot *Snapshot, providers []*Rendered) (bool, error) {
	collections := make([][]any, len(providers))
	present := make([]bool, len(providers))
	for i, p := range providers {
		if p.Content == "" {
			continue
		}
		proxies, err := loadProviderProxies(p.Content, p.nameValue(""), p.localPath(), "节点来源", "本次准备的文件内容")
		if err != nil {
			return false, err
		}
		collections[i], present[i] = proxies, true
	}
	chains, err := snapshot.HasChains()
	if err != nil {
		return false, err
	}
	if !chains {
	outer:
		for i, proxies := range collections {
			if !present[i] {
				continue
			}
			for _, p := range proxies {
				if v, _ := providerMember(p, "dialer-proxy"); v != nil {
					chains = true
					break outer
				}
			}
		}
	}
	if chains {
		for i, p := range providers {
			if !present[i] {
				return false, providerContext(dialerErr("拨号拓扑不能使用未验证的 Provider URL fallback"),
					p.nameValue(""), p.localPath(), "节点来源")
			}
		}
	}
	if err := snapshot.Validate([]any{}, chains, nil); err != nil {
		return false, err
	}
	main, err := snapshot.Main()
	if err != nil {
		return false, err
	}
	payloads := jsonx.NewObject()
	for i, p := range providers {
		if !present[i] {
			continue
		}
		name := p.nameValue(nil)
		if err := snapshot.Validate(collections[i], chains, ProviderName(name)); err != nil {
			if isDialerReference(err) {
				return false, providerContext(err, name, p.localPath(), "节点来源")
			}
			return false, err
		}
		if jsonx.Truthy(name) {
			payloads.Set(jsonx.Str(name), collections[i])
		}
	}
	if payloads.Len() > 0 {
		definitionsValue := main.GetOr("proxy-providers", jsonx.NewObject())
		definitions, ok := definitionsValue.(*jsonx.Object)
		if !ok {
			return false, attributeError(definitionsValue, "items")
		}
		type urlPayload struct {
			url     any
			proxies any
		}
		var byURL []urlPayload
		for name, proxies := range payloads.All() {
			value, present := definitions.Get(name)
			if !present {
				continue
			}
			definition, ok := value.(*jsonx.Object)
			if !ok {
				return false, attributeError(value, "get")
			}
			url := definition.Value("url")
			if !jsonx.Truthy(url) {
				continue
			}
			replaced := false
			for i := range byURL {
				if jsonx.Equal(byURL[i].url, url) {
					byURL[i].proxies, replaced = proxies, true
					break
				}
			}
			if !replaced {
				byURL = append(byURL, urlPayload{url, proxies})
			}
		}
		for name, value := range definitions.All() {
			definition, ok := value.(*jsonx.Object)
			if !ok {
				return false, attributeError(value, "get")
			}
			url := definition.Value("url")
			for _, entry := range byURL {
				if jsonx.Equal(entry.url, url) {
					payloads.Set(name, entry.proxies)
					break
				}
			}
		}
	}
	if err := convert.ValidateEmitted(main, convert.EmitOptions{ForceChains: chains, ProviderProxies: payloads, RequireProviders: chains}); err != nil {
		return false, err
	}
	return chains, nil
}

// CommitCacheUpdates is commit_cache_updates(updates).
func CommitCacheUpdates(ctx context.Context, updates []CacheUpdate) error {
	ctx = context.WithoutCancel(ctx) // PORTING rule 4b: never commit half a bundle.
	for _, u := range updates {
		if _, err := substore.SaveSubscriptionNodes(ctx, u.SubID, u.Nodes, u.Metadata); err != nil {
			return err
		}
	}
	return nil
}

// Requested is the requested=(kind, resource) argument of
// prepare_provider_bundle: Kind "subscription" or "aggregation".
type Requested struct {
	Kind     string
	Resource *jsonx.Object
}

// BundleOptions are the keyword arguments of prepare_provider_bundle.
type BundleOptions struct {
	Requested              *Requested
	RenderAll              bool
	AllowTransportFallback bool
	// Prepared maps provider names to content prepared by the caller
	// ({'content', 'cache_updates'}).
	Prepared map[string]*Rendered
	// SubscriptionFetch replaces the fetch of fetch_subscription_source.
	SubscriptionFetch refresh.SourceFetch
	// Progress is progress(message) / progress(message, force=True).
	Progress func(message string, force bool)
}

type fetchFlight struct {
	done   chan struct{}
	result *substore.SubscriptionResult
	err    error
}

// PrepareProviderBundle is prepare_provider_bundle(config, main, ...):
// capture once, render each original source once, validate all copies,
// never commit. config and main are not modified.
func PrepareProviderBundle(ctx context.Context, config, main *jsonx.Object, opts BundleOptions) ([]*Rendered, error) {
	// PORTING rule 4b: the shared per-subscription fetches and the bundle
	// validation run to completion like Python's request thread.
	ctx = context.WithoutCancel(ctx)
	env := currentEnv()
	var requestedName any
	hasRequested := opts.Requested != nil
	if hasRequested {
		resource := opts.Requested.Resource
		name, err := requiredKey(resource, "name")
		if err != nil {
			return nil, err
		}
		requestedName = name
		catalog := "subscription_aggregations"
		if opts.Requested.Kind == "subscription" {
			catalog = "subscriptions"
		}
		items, err := pyIter(config.GetOr(catalog, []any{}))
		if err != nil {
			return nil, err
		}
		found := false
		for _, item := range items {
			o, ok := item.(*jsonx.Object)
			if !ok {
				return nil, subscriptError(item)
			}
			id, err := requiredKey(o, "id")
			if err != nil {
				return nil, err
			}
			// resource['id'] is evaluated per item (any() over a generator).
			resourceID, err := requiredKey(resource, "id")
			if err != nil {
				return nil, err
			}
			if jsonx.Equal(id, resourceID) {
				found = true
				break
			}
		}
		if !found {
			config = config.DeepCopy()
			list, _ := config.SetDefault(catalog, []any{}).([]any)
			config.Set(catalog, append(list, resource.DeepCopy()))
		}
	}
	prepared := opts.Prepared
	var mu sync.Mutex
	fetchResults := map[string]*fetchFlight{}
	subscriptions, err := pyIter(config.GetOr("subscriptions", []any{}))
	if err != nil {
		return nil, err
	}
	if len(prepared) > 0 {
		for _, item := range subscriptions {
			sub, ok := item.(*jsonx.Object)
			if !ok {
				return nil, subscriptError(item)
			}
			name, err := requiredKey(sub, "name")
			if err != nil {
				return nil, err
			}
			nameText, ok := name.(string)
			if !ok || prepared[nameText] == nil {
				continue
			}
			id, err := requiredKey(sub, "id")
			if err != nil {
				return nil, err
			}
			done := make(chan struct{})
			close(done)
			// The fetches look the key up with the str() of id and url.
			fetchResults[fetchKey(idText(id), idText(sub.Value("url")))] = &fetchFlight{done: done,
				result: &substore.SubscriptionResult{Text: prepared[nameText].Content, Source: "prepared_yaml"}}
		}
	}
	fetchSubscription := func(fetch refresh.SourceFetch) refresh.SourceFetch {
		return func(ctx context.Context, id, url string) (*substore.SubscriptionResult, error) {
			key := fetchKey(id, url)
			mu.Lock()
			flight, exists := fetchResults[key]
			if !exists {
				flight = &fetchFlight{done: make(chan struct{})}
				fetchResults[key] = flight
			}
			mu.Unlock()
			if !exists {
				flight.result, flight.err = func() (*substore.SubscriptionResult, error) {
					sub, err := findByID(config.GetOr("subscriptions", []any{}), id)
					if err != nil {
						return nil, err
					}
					use := opts.SubscriptionFetch
					if use == nil {
						use = fetch
					}
					return refresh.FetchSubscriptionSource(ctx, sub, use)
				}()
				close(flight.done)
			}
			<-flight.done
			return flight.result, flight.err
		}
	}

	profileID, err := bundleProfileID(ctx, config)
	if err != nil {
		return nil, err
	}
	snapshot, err := Capture(profileID, main, config)
	if err != nil {
		return nil, err
	}
	definitionsValue := main.GetOr("proxy-providers", jsonx.NewObject())
	definitions, ok := definitionsValue.(*jsonx.Object)
	if !ok {
		return nil, attributeError(definitionsValue, "items")
	}
	if hasRequested {
		requestedText, isText := requestedName.(string)
		if !isText || !definitions.Has(requestedText) {
			main = main.DeepCopy()
			value := main.SetDefault("proxy-providers", jsonx.NewObject())
			definitions, ok = value.(*jsonx.Object)
			if !ok {
				return nil, typeError("'" + pyTypeName(value) + "' object does not support item assignment")
			}
			resource := opts.Requested.Resource
			segment, suffix := "aggregations", "provider"
			if opts.Requested.Kind == "subscription" {
				segment, suffix = "subscriptions", "proxies"
			}
			id := jsonx.Str(resource.Value("id"))
			definitions.Set(jsonx.Str(requestedName), jsonx.Obj(
				"url", "/"+segment+"/"+id+"/"+suffix,
				"path", "./providers/"+id+".yaml"))
			if snapshot, err = Capture(profileID, main, config); err != nil {
				return nil, err
			}
		}
	}
	// sources: url -> first provider name using it.
	var sourceURLs []string
	sourceNames := map[string]string{}
	for name, value := range definitions.All() {
		definition, ok := value.(*jsonx.Object)
		if !ok {
			return nil, attributeError(value, "get")
		}
		urlValue := definition.GetOr("url", "")
		url, ok := urlValue.(string)
		if !ok {
			return nil, typeError("provider url must be str, not " + pyTypeName(urlValue))
		}
		if _, seen := sourceNames[url]; !seen {
			sourceNames[url] = name
			sourceURLs = append(sourceURLs, url)
		}
	}
	var renderedURLs []string
	rendered := map[string]*Rendered{}
	nodes, err := pyIter(config.GetOr("nodes", []any{}))
	if err != nil {
		return nil, err
	}
	var opaque []any
	for _, item := range nodes {
		n, ok := item.(*jsonx.Object)
		if !ok {
			return nil, attributeError(item, "get")
		}
		ps := n.Value("proxy_string")
		if !jsonx.Truthy(ps) {
			continue
		}
		text, ok := ps.(string)
		if !ok {
			return nil, attributeError(ps, "strip")
		}
		parsed, err := convert.InspectStructuredProxyString(text)
		if err != nil {
			return nil, err
		}
		if parsed == nil {
			id, err := requiredKey(n, "id")
			if err != nil {
				return nil, err
			}
			if !containsEqual(opaque, id) {
				opaque = append(opaque, id)
			}
		}
	}
	pathOf := func(name string) any {
		definition, _ := definitions.Value(name).(*jsonx.Object)
		return definition.GetOr("path", "")
	}

	materializeInner := func(url, name string) error {
		if p, ok := prepared[name]; ok && p != nil {
			value := p.clone()
			value.Name, value.HasName = name, true
			if _, seen := rendered[url]; !seen {
				renderedURLs = append(renderedURLs, url)
			}
			rendered[url] = value
			return nil
		}
		path, err := urlPath(url)
		if err != nil {
			return err
		}
		var value *Rendered
		switch {
		case strings.Contains(path, "/subscriptions/"):
			id := strings.Split(path[strings.LastIndex(path, "/subscriptions/")+len("/subscriptions/"):], "/")[0]
			sub, err := findByID(config.GetOr("subscriptions", []any{}), id)
			if err != nil {
				return err
			}
			value, err = PrepareSubscriptionProvider(ctx, sub, config, snapshot, PrepareOptions{
				Fetch:                  fetchSubscription(env.SubscriptionFetch),
				AllowTransportFallback: opts.AllowTransportFallback,
				SourcePrepared:         true,
				SourcePath:             pathOf(name),
			})
			if err != nil {
				return err
			}
		case strings.Contains(path, "/aggregations/"):
			id := strings.Split(path[strings.LastIndex(path, "/aggregations/")+len("/aggregations/"):], "/")[0]
			agg, err := findByID(config.GetOr("subscription_aggregations", []any{}), id)
			if err != nil {
				return err
			}
			value, err = currentAggregationProvider()(ctx, agg, config, main, fetchSubscription(env.AggregationFetch))
			if err != nil {
				return err
			}
			value = value.clone()
			value.AggregationID = id
		default:
			return dialerErr("拨号拓扑 Provider 必须使用当前配置空间的订阅或聚合来源")
		}
		value.Name, value.HasName = name, true
		if _, seen := rendered[url]; !seen {
			renderedURLs = append(renderedURLs, url)
		}
		rendered[url] = value
		return nil
	}
	materialize := func(url, name string) error {
		path, err := urlPath(url)
		if err != nil {
			return err
		}
		kind := "订阅"
		if strings.Contains(path, "/aggregations/") {
			kind = "订阅聚合"
		}
		if opts.Progress != nil {
			opts.Progress("正在准备"+kind+"「"+name+"」", false)
		}
		if err := materializeInner(url, name); err != nil {
			if isStrategyReference(err) {
				return providerContext(err, name, pathOf(name), kind)
			}
			return err
		}
		if opts.Progress != nil {
			opts.Progress(kind+"「"+name+"」已准备", false)
		}
		return nil
	}

	aggregations, err := pyIter(config.GetOr("subscription_aggregations", []any{}))
	if err != nil {
		return nil, err
	}
	for _, url := range sourceURLs {
		name := sourceNames[url]
		path, err := urlPath(url)
		if err != nil {
			return nil, err
		}
		var agg *jsonx.Object
		for _, item := range aggregations {
			a, ok := item.(*jsonx.Object)
			if !ok {
				return nil, subscriptError(item)
			}
			id, err := requiredKey(a, "id")
			if err != nil {
				return nil, err
			}
			if strings.Contains(path, "/aggregations/"+jsonx.Str(id)+"/") {
				agg = a
				break
			}
		}
		render := opts.RenderAll
		if !render {
			chains, err := snapshot.HasChains()
			if err != nil {
				return nil, err
			}
			render = chains || (hasRequested && jsonx.Equal(name, requestedName))
		}
		if !render && agg != nil && agg.Len() > 0 {
			members, err := pyIter(agg.GetOr("nodes", []any{}))
			if err != nil {
				return nil, err
			}
			for _, m := range members {
				if containsEqual(opaque, m) {
					render = true
					break
				}
			}
		}
		if render {
			if err := materialize(url, name); err != nil {
				return nil, err
			}
		}
	}
	active, err := snapshot.HasChains()
	if err != nil {
		return nil, err
	}
	if !active {
	scan:
		for _, url := range renderedURLs {
			value := rendered[url]
			if value.Content == "" {
				continue
			}
			name := sourceNames[url]
			proxies, err := loadProviderProxies(value.Content, name, pathOf(name), "节点来源", "本次准备的文件内容")
			if err != nil {
				return nil, err
			}
			for _, p := range proxies {
				if v, _ := providerMember(p, "dialer-proxy"); v != nil {
					active = true
					break scan
				}
			}
		}
	}
	if active {
		for _, url := range sourceURLs {
			if _, done := rendered[url]; !done {
				if err := materialize(url, sourceNames[url]); err != nil {
					return nil, err
				}
			}
		}
	}
	bundle := []*Rendered{}
	for name, value := range definitions.All() {
		definition := value.(*jsonx.Object)
		url, _ := definition.Value("url").(string)
		r, ok := rendered[url]
		if !ok || !definition.Has("url") {
			continue
		}
		path, present := definition.Get("path")
		if !present {
			return nil, keyError("path")
		}
		item := r.clone()
		item.Name, item.HasName, item.URL, item.LocalPath = name, true, url, path
		bundle = append(bundle, item)
	}
	if opts.Progress != nil {
		opts.Progress("正在校验节点来源与拨号引用关系（"+itoa(len(bundle))+" 个文件）", true)
	}
	if _, err := ValidateRenderedBundle(snapshot, bundle); err != nil {
		return nil, err
	}
	return bundle, nil
}

// CommitProviderBundle is commit_provider_bundle(profile_id, bundle): only
// call after the complete exact bundle passed validation.
func CommitProviderBundle(ctx context.Context, profileID string, bundle []*Rendered) error {
	ctx = context.WithoutCancel(ctx) // PORTING rule 4b: never commit half a bundle.
	var order []string
	updates := map[string]CacheUpdate{}
	var artifactOrder []string
	artifacts := map[string]string{}
	for _, item := range bundle {
		for _, u := range item.CacheUpdates {
			if _, seen := updates[u.SubID]; !seen {
				order = append(order, u.SubID)
			}
			updates[u.SubID] = u
		}
		if item.AggregationID != "" {
			if _, seen := artifacts[item.AggregationID]; !seen {
				artifactOrder = append(artifactOrder, item.AggregationID)
			}
			artifacts[item.AggregationID] = item.Content
		}
	}
	list := make([]CacheUpdate, 0, len(order))
	for _, id := range order {
		list = append(list, updates[id])
	}
	if err := CommitCacheUpdates(ctx, list); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	for _, id := range artifactOrder {
		if _, err := r.WriteProfileText(ctx, profileID, "providers/"+id+".yaml", []byte(artifacts[id])); err != nil {
			return err
		}
	}
	return nil
}

// bundleProfileID is config.get('profile_id') or get_default_profile_id().
func bundleProfileID(ctx context.Context, config *jsonx.Object) (string, error) {
	if value := config.Value("profile_id"); jsonx.Truthy(value) {
		return idText(value), nil
	}
	return repo.GetDefaultProfileID(ctx)
}

func fetchKey(id, url any) string { return jsonx.Repr(id) + "\x00" + jsonx.Repr(url) }

// findByID is next(s for s in items if s['id'] == id) (StopIteration when
// none matches).
func findByID(itemsValue any, id string) (*jsonx.Object, error) {
	items, err := pyIter(itemsValue)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		o, ok := item.(*jsonx.Object)
		if !ok {
			return nil, subscriptError(item)
		}
		value, err := requiredKey(o, "id")
		if err != nil {
			return nil, err
		}
		if jsonx.Equal(value, id) {
			return o, nil
		}
	}
	return nil, &pyError{"StopIteration", ""}
}

// urlPath is the percent-decoded path of url.
func urlPath(url string) (string, error) {
	parts, err := weburl.Parse(url)
	if err != nil {
		return "", &pyError{"ValueError", err.Error()}
	}
	return parts.Path, nil
}

func providerMember(value any, key string) (any, bool) {
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return nil, false
	}
	return object.Get(key)
}

func providerMapping(value any) bool {
	object, ok := value.(*jsonx.Object)
	return ok && object != nil
}
