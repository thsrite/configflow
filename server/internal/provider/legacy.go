package provider

// Compatibility codecs for callers awaiting their own typed migration. Business
// logic lives in delivery.go; this file never emulates Python scalar behavior.
import (
	"context"
	"sync"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

func decodeLegacy(value any, target any) error {
	data, err := jsonresp.Encode(value)
	if err != nil {
		return err
	}
	if err := jsondecode.Unmarshal(data, target); err != nil {
		return &InputError{Field: "record", Cause: err}
	}
	return nil
}
func legacyValue(value any) (any, error) {
	data, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}
func legacyGraph(value any) (*yamlx.Node, error) {
	data, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(data)
}
func legacyObject(value any) (*jsonx.Object, error) {
	out, err := legacyValue(value)
	if err != nil {
		return nil, err
	}
	object, ok := out.(*jsonx.Object)
	if !ok && out != nil {
		return nil, &InputError{Field: "object"}
	}
	return object, nil
}
func legacyNodes(nodes []*yamlx.Node) ([]any, error) {
	out := make([]any, 0, len(nodes))
	for _, node := range nodes {
		data, err := yamlx.ToJSON(node)
		if err != nil {
			return nil, err
		}
		value, err := jsonx.Parse(data)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}
func nodesFromLegacy(value any) ([]*yamlx.Node, error) {
	node, err := legacyGraph(value)
	if err != nil {
		return nil, err
	}
	if node.Kind != 2 {
		return nil, shapeError(msgProxyList, FieldPath{{Key: "proxies"}})
	}
	return node.Content, nil
}
func configFromLegacy(value *jsonx.Object) (*model.Config, error) {
	var profile model.Profile
	// Only consumed profile members cross this adapter; an unrelated megabyte of
	// settings must not be decoded during provider preparation.
	fields := jsonx.NewObject()
	for _, key := range []string{"proxy_groups"} {
		if v, ok := value.Get(key); ok {
			fields.Set(key, v)
		}
	}
	if err := decodeLegacy(fields, &profile); err != nil {
		return nil, err
	}
	var catalog struct {
		ProfileID     string               `json:"profile_id"`
		Subscriptions []model.Subscription `json:"subscriptions"`
		Nodes         []model.Node         `json:"nodes"`
		Aggregations  []model.Aggregation  `json:"subscription_aggregations"`
	}
	fields = jsonx.NewObject()
	for _, key := range []string{"profile_id", "subscriptions", "nodes", "subscription_aggregations"} {
		if v, ok := value.Get(key); ok {
			fields.Set(key, v)
		}
	}
	if err := decodeLegacy(fields, &catalog); err != nil {
		return nil, err
	}
	return &model.Config{Profile: &profile, ProfileID: catalog.ProfileID, Subscriptions: catalog.Subscriptions, Nodes: catalog.Nodes, Aggregations: catalog.Aggregations}, nil
}

// CacheUpdate is the legacy receipt boundary; native owners use CacheWrite.
type CacheUpdate struct {
	SubID    string
	Nodes    []any
	Metadata *jsonx.Object
}

// Rendered is the legacy provider boundary; native owners use Artifact.
type Rendered struct {
	Name          string
	HasName       bool
	Content       string
	CacheUpdates  []CacheUpdate
	AggregationID string
	URL           string
	LocalPath     any
	Extra         *jsonx.Object
}

func (r *Rendered) clone() *Rendered {
	copy := *r
	copy.CacheUpdates = append([]CacheUpdate(nil), r.CacheUpdates...)
	return &copy
}
func nativeArtifact(value *Rendered) (*Artifact, error) {
	if value == nil {
		return nil, &InputError{Field: "artifact"}
	}
	out := &Artifact{Name: value.Name, HasName: value.HasName, Content: value.Content, AggregationID: value.AggregationID, URL: value.URL, CacheUpdates: []CacheWrite{}}
	if value.LocalPath != nil {
		if err := decodeLegacy(value.LocalPath, &out.LocalPath); err != nil {
			return nil, err
		}
	}
	for _, update := range value.CacheUpdates {
		var u CacheWrite
		u.SubID = update.SubID
		if err := decodeLegacy(update.Nodes, &u.Nodes); err != nil {
			return nil, err
		}
		if update.Metadata != nil {
			u.Metadata = &substore.CacheMetadata{}
			if err := decodeLegacy(update.Metadata, u.Metadata); err != nil {
				return nil, err
			}
		}
		out.CacheUpdates = append(out.CacheUpdates, u)
	}
	if value.Extra != nil {
		data, err := jsonresp.Encode(value.Extra)
		if err != nil {
			return nil, err
		}
		out.Extra = data
	}
	return out, nil
}
func renderedArtifact(value *Artifact) (*Rendered, error) {
	if value == nil {
		return nil, nil
	}
	out := &Rendered{Name: value.Name, HasName: value.HasName, Content: value.Content, AggregationID: value.AggregationID, URL: value.URL, LocalPath: value.LocalPath, CacheUpdates: []CacheUpdate{}}
	for _, update := range value.CacheUpdates {
		nodes, err := legacyValue(update.Nodes)
		if err != nil {
			return nil, err
		}
		list, _ := nodes.([]any)
		metadata, err := legacyObject(update.Metadata)
		if err != nil {
			return nil, err
		}
		out.CacheUpdates = append(out.CacheUpdates, CacheUpdate{SubID: update.SubID, Nodes: list, Metadata: metadata})
	}
	if len(value.Extra) > 0 {
		extra, err := jsonx.Parse(value.Extra)
		if err != nil {
			return nil, err
		}
		out.Extra, _ = extra.(*jsonx.Object)
	}
	return out, nil
}

// Snapshot retains serialized fields only for older callers and archived replays.
// Its native graph is decoded once, even when providers validate repeatedly.
type Snapshot struct {
	ProfileID  string
	MainJSON   string
	ChainNames []any
	once       sync.Once
	delivery   *DeliverySnapshot
	err        error
}

// Capture transcodes a legacy delivery once; use CaptureDelivery natively.
func Capture(profileID string, main, profile *jsonx.Object) (*Snapshot, error) {
	graph, err := legacyGraph(main)
	if err != nil {
		return nil, err
	}
	var typed *model.Profile
	if profile != nil {
		typed = &model.Profile{}
		groups := jsonx.NewObject()
		if value, ok := profile.Get("proxy_groups"); ok {
			groups.Set("proxy_groups", value)
		}
		if err := decodeLegacy(groups, typed); err != nil {
			return nil, err
		}
	}
	delivery, err := CaptureDelivery(profileID, graph, typed)
	if err != nil {
		return nil, err
	}
	data, err := yamlx.ToJSON(graph)
	if err != nil {
		return nil, err
	}
	out := &Snapshot{ProfileID: profileID, MainJSON: string(data), delivery: delivery, ChainNames: []any{}}
	if typed != nil {
		for _, group := range typed.ProxyGroups {
			if delivery.chainNames[group.Name] {
				seen := false
				for _, name := range out.ChainNames {
					if name == group.Name {
						seen = true
					}
				}
				if !seen {
					out.ChainNames = append(out.ChainNames, group.Name)
				}
			}
		}
	}
	return out, nil
}
func (s *Snapshot) native() (*DeliverySnapshot, error) {
	if s == nil {
		return nil, &InputError{Field: "snapshot"}
	}
	s.once.Do(func() {
		if s.delivery != nil {
			return
		}
		main, err := yamlx.FromJSON([]byte(s.MainJSON))
		if err != nil {
			s.err = err
			return
		}
		names := map[string]bool{}
		for _, value := range s.ChainNames {
			var name string
			if err := decodeLegacy(value, &name); err != nil {
				s.err = err
				return
			}
			names[name] = true
		}
		s.delivery = &DeliverySnapshot{ProfileID: s.ProfileID, main: main, chainNames: names}
	})
	return s.delivery, s.err
}

// ParseProviderProxies retains the legacy result shape around ParseNodes.
func ParseProviderProxies(text string, name, path any) ([]any, error) {
	n, p := legacyLabel(name), legacyLabel(path)
	nodes, err := ParseNodes(text, n, p)
	if err != nil {
		return nil, err
	}
	return legacyNodes(nodes)
}

// PrepareOptions is the legacy source boundary; use SubscriptionOptions natively.
type PrepareOptions struct {
	Fetch                  refresh.SourceFetch
	AllowTransportFallback bool
	SourcePath             any
	SourcePrepared         bool
}

// PrepareSubscriptionProvider adapts legacy inputs to PrepareSubscription.
func PrepareSubscriptionProvider(ctx context.Context, sub, config *jsonx.Object, snapshot *Snapshot, opts PrepareOptions) (*Rendered, error) {
	var source model.Subscription
	if err := decodeLegacy(sub, &source); err != nil {
		return nil, err
	}
	delivery, err := snapshot.native()
	if err != nil {
		return nil, err
	}
	options := SubscriptionOptions{Fetch: nativeFetch(opts.Fetch), AllowTransportFallback: opts.AllowTransportFallback, SourcePrepared: opts.SourcePrepared}
	if opts.SourcePath != nil {
		if err := decodeLegacy(opts.SourcePath, &options.SourcePath); err != nil {
			return nil, err
		}
	}
	result, err := PrepareSubscription(ctx, source, delivery, options)
	if err != nil {
		return nil, err
	}
	return renderedArtifact(result)
}

// CommitCacheUpdates adapts staged legacy receipts to CommitWrites.
func CommitCacheUpdates(ctx context.Context, updates []CacheUpdate) error {
	artifact, err := nativeArtifact(&Rendered{CacheUpdates: updates})
	if err != nil {
		return err
	}
	return CommitWrites(ctx, artifact.CacheUpdates)
}

// Requested is the legacy resource boundary; native owners use BundleRequest.
type Requested struct {
	Kind     string
	Resource *jsonx.Object
}

// BundleOptions is the legacy bundle boundary; native owners use DeliveryOptions.
type BundleOptions struct {
	Requested                         *Requested
	RenderAll, AllowTransportFallback bool
	Prepared                          map[string]*Rendered
	SubscriptionFetch                 refresh.SourceFetch
	Progress                          func(string, bool)
}

// PrepareProviderBundle transcodes the consumed config once at entry.
func PrepareProviderBundle(ctx context.Context, config, main *jsonx.Object, opts BundleOptions) ([]*Rendered, error) {
	typed, err := configFromLegacy(config)
	if err != nil {
		return nil, err
	}
	graph, err := legacyGraph(main)
	if err != nil {
		return nil, err
	}
	options := DeliveryOptions{RenderAll: opts.RenderAll, AllowTransportFallback: opts.AllowTransportFallback, SubscriptionFetch: nativeFetch(opts.SubscriptionFetch), Progress: opts.Progress, Prepared: map[string]*Artifact{}}
	if opts.Requested != nil {
		options.Requested = &BundleRequest{Kind: opts.Requested.Kind}
		if opts.Requested.Kind == "subscription" {
			source := &model.Subscription{}
			if err := decodeLegacy(opts.Requested.Resource, source); err != nil {
				return nil, err
			}
			options.Requested.Subscription = source
		} else {
			source := &model.Aggregation{}
			if err := decodeLegacy(opts.Requested.Resource, source); err != nil {
				return nil, err
			}
			options.Requested.Aggregation = source
		}
	}
	for name, value := range opts.Prepared {
		artifact, err := nativeArtifact(value)
		if err != nil {
			return nil, err
		}
		options.Prepared[name] = artifact
	}
	bundle, err := PrepareBundle(ctx, typed, graph, options)
	if err != nil {
		return nil, err
	}
	out := make([]*Rendered, 0, len(bundle))
	for _, item := range bundle {
		value, err := renderedArtifact(item)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

// CommitProviderBundle adapts validated legacy artifacts to CommitArtifacts.
func CommitProviderBundle(ctx context.Context, profileID string, bundle []*Rendered) error {
	items := make([]*Artifact, 0, len(bundle))
	for _, item := range bundle {
		value, err := nativeArtifact(item)
		if err != nil {
			return err
		}
		items = append(items, value)
	}
	return CommitArtifacts(ctx, profileID, items)
}

func nativeFetch(fetch refresh.SourceFetch) refresh.FetchSourceFn {
	if fetch == nil {
		return nil
	}
	return func(ctx context.Context, id, address string) (*refresh.SourceResult, error) {
		result, err := fetch(ctx, id, address)
		if err != nil || result == nil {
			return nil, err
		}
		var traffic *refresh.Traffic
		if result.Traffic != nil {
			traffic = &refresh.Traffic{}
			if err := decodeLegacy(result.Traffic, traffic); err != nil {
				return nil, err
			}
		}
		return &refresh.SourceResult{Text: result.Text, Source: result.Source, FetchedAt: result.FetchedAt, Traffic: traffic}, nil
	}
}
func legacyFetch(fetch refresh.FetchSourceFn) refresh.SourceFetch {
	if fetch == nil {
		return nil
	}
	return func(ctx context.Context, id, address string) (*substore.SubscriptionResult, error) {
		result, err := fetch(ctx, id, address)
		if err != nil || result == nil {
			return nil, err
		}
		traffic, err := legacyObject(result.Traffic)
		if err != nil {
			return nil, err
		}
		return &substore.SubscriptionResult{Text: result.Text, Source: result.Source, FetchedAt: result.FetchedAt, Traffic: traffic}, nil
	}
}

func legacyLabel(value any) string {
	if value == nil {
		return ""
	}
	var text model.String
	if decodeLegacy(value, &text) != nil {
		return ""
	}
	if number, ok := value.(jsonx.Number); ok && number.String() == "0" {
		return ""
	}
	return string(text)
}
