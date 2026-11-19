package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"configflow/internal/agents/diag"
	"configflow/internal/convert"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// CacheWrite is a validated subscription receipt staged until the bundle passes.
type CacheWrite struct {
	SubID    string
	Nodes    []model.Node
	Metadata *substore.CacheMetadata
}

// Artifact contains prepared provider bytes and their staged side effects.
type Artifact struct {
	Name          string
	HasName       bool
	Content       string
	CacheUpdates  []CacheWrite
	AggregationID string
	URL           string
	LocalPath     string
	Extra         json.RawMessage
}

func (a *Artifact) clone() *Artifact {
	c := *a
	c.CacheUpdates = slices.Clone(a.CacheUpdates)
	return &c
}

// DeliverySnapshot owns one immutable main graph and enabled chain exit index.
// Graph operations share untouched subtrees; no validation reparses the document.
type DeliverySnapshot struct {
	ProfileID  string
	main       *yamlx.Node
	chainNames map[string]bool
}

// CaptureDelivery freezes a validated graph once before fetching any source.
func CaptureDelivery(profileID string, main *yamlx.Node, profile *model.Profile) (*DeliverySnapshot, error) {
	if main == nil {
		main = yamlx.Null()
	}
	checked, err := yamlx.Encode(main)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	if profile != nil {
		for _, group := range profile.ProxyGroups {
			if group.Type == "chain" && (group.Enabled == nil || *group.Enabled) {
				if group.Name == "" || model.IssueUnder(model.Issues(group), "name") {
					return nil, &InputError{Field: "proxy_groups.name"}
				}
				names[group.Name] = true
			}
		}
	}
	return &DeliverySnapshot{ProfileID: profileID, main: checked, chainNames: names}, nil
}

// MainNode returns an independent graph for callers that need to edit it.
func (s *DeliverySnapshot) MainNode() *yamlx.Node { return yamlx.CopyGraph(s.main) }
func mapping(n *yamlx.Node) bool                  { n = yamlx.Resolve(n); return n != nil && n.Kind == yaml.MappingNode }
func textValue(n *yamlx.Node) (string, bool) {
	n = yamlx.Resolve(n)
	if n == nil || n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
		return "", false
	}
	return n.Value, true
}
func memberText(n *yamlx.Node, key string) string {
	text, _ := textValue(yamlx.Get(n, key))
	return text
}
func nonNull(n *yamlx.Node) bool { n = yamlx.Resolve(n); return n != nil && n.ShortTag() != "!!null" }
func sequence(n *yamlx.Node) ([]*yamlx.Node, error) {
	n = yamlx.Resolve(n)
	if n == nil {
		return nil, nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil, dialerErr(msgTopologyShape)
	}
	return n.Content, nil
}

// HasChains detects both managed chain exits and emitted dialer references.
func (s *DeliverySnapshot) HasChains() (bool, error) {
	if len(s.chainNames) > 0 {
		return true, nil
	}
	if !mapping(s.main) {
		return false, &InputError{Field: "main"}
	}
	proxies, err := sequence(yamlx.Get(s.main, "proxies"))
	if err != nil {
		return false, err
	}
	for _, proxy := range proxies {
		if !mapping(proxy) {
			return false, &InputError{Field: "proxies"}
		}
		if nonNull(yamlx.Get(proxy, "dialer-proxy")) {
			return true, nil
		}
	}
	definitions := yamlx.Get(s.main, "proxy-providers")
	if definitions != nil && !mapping(definitions) {
		return false, &InputError{Field: "proxy-providers"}
	}
	for _, definition := range yamlx.Pairs(definitions) {
		if !mapping(definition) {
			return false, &InputError{Field: "proxy-providers"}
		}
		override := yamlx.Get(definition, "override")
		if override != nil && !mapping(override) {
			return false, &InputError{Field: "proxy-providers.override"}
		}
		if target := yamlx.Get(override, "dialer-proxy"); nonNull(target) {
			value, ok := textValue(target)
			if !ok {
				return false, dialerErr(msgTopologyShape)
			}
			if value != "" {
				return true, nil
			}
		}
	}
	return false, nil
}

// Validate checks shapes, chain exits and the full emitted topology without mutation.
func (s *DeliverySnapshot) Validate(proxies []*yamlx.Node, force bool, providerName *string) error {
	if err := ValidateProxyNodes(proxies); err != nil {
		return err
	}
	names := make(map[string]bool, len(proxies))
	for _, proxy := range proxies {
		name := memberText(proxy, "name")
		names[name] = true
		if s.chainNames[name] {
			return dialerErr(msgChainConflict)
		}
	}
	if !mapping(s.main) {
		return &InputError{Field: "main"}
	}
	chains, err := s.HasChains()
	if err != nil {
		return err
	}
	opts := convert.TopologyOptions{ForceChains: chains || force}
	definitions := yamlx.Get(s.main, "proxy-providers")
	if providerName != nil && yamlx.Get(definitions, *providerName) != nil {
		source := yamlx.Get(definitions, *providerName)
		opts.ProviderProxies = map[string][]*yamlx.Node{*providerName: proxies}
		address := memberText(source, "url")
		if address != "" {
			for name, def := range yamlx.Pairs(definitions) {
				if memberText(def, "url") == address {
					opts.ProviderProxies[name] = proxies
				}
			}
		}
		return topologyError(convert.ValidateTopology(s.main, opts))
	}
	originals, err := sequence(yamlx.Get(s.main, "proxies"))
	if err != nil {
		return err
	}
	combined := make([]*yamlx.Node, 0, len(originals)+len(proxies))
	for _, proxy := range originals {
		name, ok := textValue(yamlx.Get(proxy, "name"))
		if !mapping(proxy) || !ok {
			return dialerErr(msgTopologyShape)
		}
		if !names[name] {
			combined = append(combined, proxy)
		}
	}
	combined = append(combined, proxies...)
	// Only own the root container. ValidateTopology is read-only.
	root := *yamlx.Resolve(s.main)
	root.Content = slices.Clone(root.Content)
	yamlx.Set(&root, "proxies", yamlx.Seq(combined...))
	return topologyError(convert.ValidateTopology(&root, opts))
}
func topologyError(err error) error {
	if err == nil {
		return nil
	}
	if isStrategyReference(err) {
		return err
	}
	result := dialerErr(err.Error())
	result.cause = err
	return result
}

// ValidateProxyNodes checks provider metadata before engine normalization.
func ValidateProxyNodes(proxies []*yamlx.Node) error {
	for index, proxy := range proxies {
		name, ok := textValue(yamlx.Get(proxy, "name"))
		if !mapping(proxy) || !ok || strings.TrimSpace(name) == "" {
			if mapping(proxy) && yamlx.Get(proxy, "name") != nil {
				return shapeError(msgProxyName, proxyField(index, "name"))
			}
			return shapeError(msgProxyName, proxyField(index, ""))
		}
		if refs := yamlx.Get(proxy, "proxies"); refs != nil {
			list, err := sequence(refs)
			bad := err != nil
			for _, ref := range list {
				text, ok := textValue(ref)
				if !ok || strings.TrimSpace(text) == "" {
					bad = true
					break
				}
			}
			if bad {
				return shapeError(msgProxyRefs, proxyField(index, "proxies"))
			}
		}
		if kind := yamlx.Get(proxy, "type"); kind != nil {
			if _, ok := textValue(kind); !ok {
				return shapeError(msgProxyType, proxyField(index, "type"))
			}
		}
	}
	return nil
}
func readProviderNodes(text, name, path, kind, sourceLabel string) ([]*yamlx.Node, error) {
	data, err := yamlx.Parse([]byte(text))
	wrap := func(err, errorCause error) error {
		return providerContext(providerSourceError(err, errorCause, text, sourceLabel), name, path, kind)
	}
	if err != nil {
		return nil, wrap(err, yamlCause(err))
	}
	if !mapping(data) {
		return nil, wrap(dialerErr(msgProviderRoot), nil)
	}
	list := yamlx.Get(data, "proxies")
	if list == nil {
		return []*yamlx.Node{}, nil
	}
	if err := yamlx.ProjectJSONKeys(list, yamlx.StringifyScalarKeys); err != nil {
		return nil, wrap(err, yamlCause(err))
	}
	if _, err := yamlx.ToJSON(list); err != nil {
		return nil, wrap(err, yamlCause(err))
	}
	list = yamlx.Resolve(list)
	if list.Kind != yaml.SequenceNode {
		return nil, wrap(shapeError(msgProxyList, FieldPath{{Key: "proxies"}}), nil)
	}
	if err := ValidateProxyNodes(list.Content); err != nil {
		return nil, wrap(err, nil)
	}
	return list.Content, nil
}

// ParseNodes validates source metadata before applying shared engine normalization.
func ParseNodes(text, name, path string) ([]*yamlx.Node, error) {
	nodes, err := readProviderNodes(text, name, path, msgSubscription, msgSubscriptionSource)
	if err != nil {
		return nil, err
	}
	result := make([]*yamlx.Node, 0, len(nodes))
	for _, node := range nodes {
		checked, err := yamlx.Encode(node)
		if err != nil {
			return nil, err
		}
		owned := checked
		convert.FixProxyNode(owned)
		result = append(result, owned)
	}
	return result, nil
}

// DialerRecord freezes a source's cached or downloaded airport topology.
func DialerRecord(ctx context.Context, sub model.Subscription) (*subs.DialersData, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sub.ID == "" || model.IssueUnder(model.Issues(sub), "id") {
		return nil, &InputError{Field: "subscription.id"}
	}
	cache, err := substore.LoadCache(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	var view *subs.SubscriptionCache
	if cache != nil {
		view = &subs.SubscriptionCache{Nodes: cache.Nodes}
		if cache.Metadata != nil {
			raw, err := model.Marshal(cache.Metadata)
			if err != nil {
				return nil, err
			}
			var metadata subs.SourceMetadata
			if err := json.Unmarshal(raw, &metadata); err != nil {
				return nil, err
			}
			view.Metadata = &metadata
		}
	}
	return subs.SourceDialers(ctx, sub, view, func(ctx context.Context, address string) (string, bool) {
		r, err := repo.GetRepository(ctx)
		if err != nil {
			return "", false
		}
		raw, err := r.ReadSharedBytes(ctx, refresh.DownloadCachePath(address))
		if err != nil {
			return "", false
		}
		var body struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return "", false
		}
		return body.Text, true
	})
}

// AdaptNodes applies airport dialers against this delivery's immutable graph.
func (s *DeliverySnapshot) AdaptNodes(ctx context.Context, sub model.Subscription, proxies []*yamlx.Node, chains bool) ([]*yamlx.Node, error) {
	if s == nil || sub.ID == "" {
		return proxies, nil
	}
	record, err := DialerRecord(ctx, sub)
	if err != nil {
		return nil, err
	}
	return subs.AdaptProxies(sub.Name, proxies, s.main, record, chains)
}

// SubscriptionOptions controls source reuse and transport fallback.
type SubscriptionOptions struct {
	Fetch                  refresh.FetchSourceFn
	AllowTransportFallback bool
	SourcePath             string
	SourcePrepared         bool
}

// PrepareSubscription stages one provider without publishing its cache.
func PrepareSubscription(ctx context.Context, sub model.Subscription, snapshot *DeliverySnapshot, opts SubscriptionOptions) (*Artifact, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := prepareSubscription(ctx, sub, snapshot, opts)
	if err != nil && isStrategyReference(err) {
		return nil, providerContext(err, sub.Name, opts.SourcePath, msgSubscription)
	}
	return result, err
}
func prepareSubscription(ctx context.Context, sub model.Subscription, snapshot *DeliverySnapshot, opts SubscriptionOptions) (*Artifact, error) {
	if sub.ID == "" || model.IssueUnder(model.Issues(sub), "id") {
		return nil, &InputError{Field: "subscription.id"}
	}
	env := currentDeliveryEnv()
	fetch := opts.Fetch
	if fetch == nil {
		fetch = env.Fetch
	}
	var update *CacheWrite
	var proxies []*yamlx.Node
	var transportErr error
	have := false
	if sub.URL != "" {
		err := func() error {
			var result *refresh.SourceResult
			var err error
			if opts.SourcePrepared {
				result, err = fetch(ctx, sub.ID, sub.URL)
			} else {
				result, err = refresh.FetchSource(ctx, sub, fetch)
			}
			if canceled := ctx.Err(); canceled != nil {
				return canceled
			}
			if err != nil {
				return err
			}
			if result == nil {
				return &InputError{Field: "subscription response"}
			}
			raw, err := ParseNodes(result.Text, sub.Name, opts.SourcePath)
			if err != nil {
				return err
			}
			adapted, err := snapshot.AdaptNodes(ctx, sub, subs.FilterProxies(sub, raw), true)
			if err != nil {
				return err
			}
			if err := snapshot.Validate(adapted, false, &sub.Name); err != nil {
				return err
			}
			if len(raw) > 0 && result.Source != "cache" && result.Source != "prepared_yaml" {
				nodes, err := substore.ProxyNodes(raw)
				if err != nil {
					return err
				}
				for i := range nodes {
					if err := nodeIdentity(&nodes[i], sub, env.NodeID); err != nil {
						return err
					}
				}
				meta, err := refresh.CacheMetadata(ctx, sub, result)
				if err != nil {
					return err
				}
				var metadata substore.CacheMetadata
				if meta != nil {
					data, err := model.Marshal(meta)
					if err != nil {
						return err
					}
					if err := json.Unmarshal(data, &metadata); err != nil {
						return err
					}
				}
				update = &CacheWrite{SubID: sub.ID, Nodes: nodes, Metadata: &metadata}
			}
			proxies, have = adapted, true
			return nil
		}()
		if err != nil {
			if isStrategyReference(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			transportErr = err
		}
	}
	if !have {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cache, err := substore.LoadCache(ctx, sub.ID)
		if err != nil {
			return nil, err
		}
		if cache != nil {
			for _, issue := range model.Issues(cache) {
				if issue.Path == "nodes" {
					return nil, dialerErr(msgNodesList)
				}
			}
		}
		if cache == nil || len(cache.Nodes) == 0 {
			if opts.AllowTransportFallback {
				chains, err := snapshot.HasChains()
				if err != nil {
					return nil, err
				}
				if !chains {
					return &Artifact{CacheUpdates: []CacheWrite{}}, nil
				}
			}
			detail := ""
			if transportErr != nil {
				detail = fmt.Sprintf(msgOriginalDiagnostic, diag.FailureText(transportErr.Error(), nil, 0))
			}
			return nil, dialerErr(msgNoSource + detail)
		}
		if err := convert.ConfigShapes(&model.Config{Nodes: cache.Nodes}, false); err != nil {
			return nil, topologyError(err)
		}
		proxies = []*yamlx.Node{}
		for _, node := range subs.FilterNodes(sub, cache.Nodes) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			proxy, err := convert.NodeProxy(ctx, node, env.converter())
			if err != nil {
				return nil, err
			}
			if proxy != nil && len(proxy.Content) > 0 {
				proxies = append(proxies, proxy)
			}
		}
		proxies, err = snapshot.AdaptNodes(ctx, sub, proxies, true)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var identity model.Fields
	identityJSON, err := model.Marshal(sub)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(identityJSON, &identity); err != nil {
		return nil, err
	}
	if !identity.Has("name") {
		return nil, &InputError{Field: "subscription.name"}
	}
	if err := snapshot.Validate(proxies, false, &sub.Name); err != nil {
		return nil, err
	}
	data, err := yamlx.Marshal(yamlx.Map(yamlx.Str("proxies"), yamlx.Seq(proxies...)))
	if err != nil {
		return nil, err
	}
	updates := []CacheWrite{}
	if update != nil {
		updates = append(updates, *update)
	}
	return &Artifact{Name: sub.Name, HasName: true, Content: string(data), CacheUpdates: updates}, nil
}

// ValidateArtifacts checks exact prepared content and all provider URL aliases.
func ValidateArtifacts(snapshot *DeliverySnapshot, providers []*Artifact) (bool, error) {
	collections := make([][]*yamlx.Node, len(providers))
	present := make([]bool, len(providers))
	chains, err := snapshot.HasChains()
	if err != nil {
		return false, err
	}
	for i, p := range providers {
		if p.Content == "" {
			continue
		}
		nodes, err := readProviderNodes(p.Content, p.Name, p.LocalPath, msgNodeSource, msgPreparedSource)
		if err != nil {
			return false, err
		}
		collections[i], present[i] = nodes, true
		for _, node := range nodes {
			if nonNull(yamlx.Get(node, "dialer-proxy")) {
				chains = true
			}
		}
	}
	if chains {
		for i, p := range providers {
			if !present[i] {
				return false, providerContext(dialerErr(msgUnverifiedFallback), p.Name, p.LocalPath, msgNodeSource)
			}
		}
	}
	if err := snapshot.Validate(nil, chains, nil); err != nil {
		return false, err
	}
	payloads := map[string][]*yamlx.Node{}
	for i, p := range providers {
		if !present[i] {
			continue
		}
		var name *string
		if p.HasName {
			name = &p.Name
		}
		if err := snapshot.Validate(collections[i], chains, name); err != nil {
			return false, providerContext(err, p.Name, p.LocalPath, msgNodeSource)
		}
		if p.HasName && p.Name != "" {
			payloads[p.Name] = collections[i]
		}
	}
	definitions := yamlx.Get(snapshot.main, "proxy-providers")
	byURL := map[string][]*yamlx.Node{}
	// Provider order, including last-wins URL payloads, is deterministic.
	for _, p := range providers {
		if nodes, ok := payloads[p.Name]; ok {
			if address := memberText(yamlx.Get(definitions, p.Name), "url"); address != "" {
				byURL[address] = nodes
			}
		}
	}
	for name, definition := range yamlx.Pairs(definitions) {
		if nodes, ok := byURL[memberText(definition, "url")]; ok {
			payloads[name] = nodes
		}
	}
	if err := convert.ValidateTopology(snapshot.main, convert.TopologyOptions{ForceChains: chains, RequireProviders: chains, ProviderProxies: payloads}); err != nil {
		return false, topologyError(err)
	}
	return chains, nil
}

// BundleRequest adds an explicitly requested source missing from the current view.
type BundleRequest struct {
	Kind         string
	Subscription *model.Subscription
	Aggregation  *model.Aggregation
}

func (r *BundleRequest) identity() (id, name string) {
	if r.Subscription != nil {
		return r.Subscription.ID, r.Subscription.Name
	}
	if r.Aggregation != nil {
		return r.Aggregation.ID, r.Aggregation.Name
	}
	return "", ""
}

// DeliveryOptions controls bundle preparation; it never commits staged writes.
type DeliveryOptions struct {
	Requested                         *BundleRequest
	RenderAll, AllowTransportFallback bool
	Prepared                          map[string]*Artifact
	SubscriptionFetch                 refresh.FetchSourceFn
	Progress                          func(string, bool)
}
type sourceKey struct{ id, address string }
type sourceFlight struct {
	done   chan struct{}
	result *refresh.SourceResult
	err    error
}

// PrepareBundle freezes configuration once, fetches each source once and validates
// every emitted copy before returning staged cache and artifact writes.
func PrepareBundle(ctx context.Context, config *model.Config, main *yamlx.Node, opts DeliveryOptions) ([]*Artifact, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, &InputError{Field: "config"}
	}
	env := currentDeliveryEnv()
	requestedID, requestedName := "", ""
	if opts.Requested != nil {
		requestedID, requestedName = opts.Requested.identity()
		if requestedID == "" {
			return nil, &InputError{Field: "requested.id"}
		}
		owned := *config
		config = &owned
		if opts.Requested.Subscription != nil {
			if !slices.ContainsFunc(config.Subscriptions, func(s model.Subscription) bool { return s.ID == requestedID }) {
				config.Subscriptions = append(slices.Clone(config.Subscriptions), *opts.Requested.Subscription)
			}
		}
		if opts.Requested.Aggregation != nil {
			if !slices.ContainsFunc(config.Aggregations, func(s model.Aggregation) bool { return s.ID == requestedID }) {
				config.Aggregations = append(slices.Clone(config.Aggregations), *opts.Requested.Aggregation)
			}
		}
	}
	profileID := config.ProfileID
	if profileID == "" {
		var err error
		profileID, err = repo.GetDefaultProfileID(ctx)
		if err != nil {
			return nil, err
		}
	}
	snapshot, err := CaptureDelivery(profileID, main, config.Profile)
	if err != nil {
		return nil, err
	}
	main = snapshot.main
	definitions := yamlx.Get(main, "proxy-providers")
	if definitions != nil && !mapping(definitions) {
		return nil, &InputError{Field: "proxy-providers"}
	}
	if opts.Requested != nil && yamlx.Get(definitions, requestedName) == nil {
		main = snapshot.MainNode()
		definitions = yamlx.Get(main, "proxy-providers")
		if definitions == nil {
			definitions = yamlx.Map()
			yamlx.Set(main, "proxy-providers", definitions)
		}
		segment, suffix := "aggregations", "provider"
		if opts.Requested.Subscription != nil {
			segment, suffix = "subscriptions", "proxies"
		}
		yamlx.Set(definitions, requestedName, yamlx.Map(yamlx.Str("url"), yamlx.Str("/"+segment+"/"+requestedID+"/"+suffix), yamlx.Str("path"), yamlx.Str("./providers/"+requestedID+".yaml")))
		snapshot, err = CaptureDelivery(profileID, main, config.Profile)
		if err != nil {
			return nil, err
		}
		main = snapshot.main
		definitions = yamlx.Get(main, "proxy-providers")
	}
	subscriptions := make(map[string]model.Subscription, len(config.Subscriptions))
	for _, sub := range config.Subscriptions {
		subscriptions[sub.ID] = sub
	}
	var mu sync.Mutex
	flights := map[sourceKey]*sourceFlight{}
	for _, sub := range config.Subscriptions {
		if prepared := opts.Prepared[sub.Name]; prepared != nil {
			done := make(chan struct{})
			close(done)
			flights[sourceKey{sub.ID, sub.URL}] = &sourceFlight{done: done, result: &refresh.SourceResult{Text: prepared.Content, Source: "prepared_yaml"}}
		}
	}
	fetchSource := func(fetch refresh.FetchSourceFn) refresh.FetchSourceFn {
		return func(ctx context.Context, id, address string) (*refresh.SourceResult, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			key := sourceKey{id, address}
			mu.Lock()
			flight, exists := flights[key]
			if !exists {
				flight = &sourceFlight{done: make(chan struct{})}
				flights[key] = flight
			}
			mu.Unlock()
			if !exists {
				sub, ok := subscriptions[id]
				if !ok {
					flight.err = &InputError{Field: "subscription.id"}
				} else {
					use := opts.SubscriptionFetch
					if use == nil {
						use = fetch
					}
					flight.result, flight.err = refresh.FetchSource(ctx, sub, use)
				}
				if err := ctx.Err(); err != nil {
					flight.result, flight.err = nil, err
				}
				close(flight.done)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-flight.done:
				return flight.result, flight.err
			}
		}
	}
	type source struct{ url, name, path string }
	sources := []source{}
	seen := map[string]bool{}
	for name, definition := range yamlx.Pairs(definitions) {
		if !mapping(definition) {
			return nil, &InputError{Field: "proxy-providers"}
		}
		address := ""
		if n := yamlx.Get(definition, "url"); n != nil {
			var ok bool
			address, ok = textValue(n)
			if !ok {
				return nil, &InputError{Field: "proxy-providers.url"}
			}
		}
		if !seen[address] {
			seen[address] = true
			sources = append(sources, source{address, name, memberText(definition, "path")})
		}
	}
	opaque := map[string]bool{}
	for _, node := range config.Nodes {
		if node.ProxyString != nil && *node.ProxyString != "" {
			parsed, err := convert.StructuredProxy(*node.ProxyString)
			if err != nil {
				return nil, err
			}
			if parsed == nil {
				opaque[node.ID] = true
			}
		}
	}
	rendered := map[string]*Artifact{}
	renderOrder := []string{}
	pathOf := func(address string) (string, error) {
		parts, err := weburl.Parse(address)
		if err != nil {
			return "", err
		}
		return parts.Path, nil
	}
	materialize := func(source source) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := pathOf(source.url)
		if err != nil {
			return err
		}
		kind := msgSubscription
		if strings.Contains(path, "/aggregations/") {
			kind = msgAggregation
		}
		if opts.Progress != nil {
			opts.Progress(fmt.Sprintf(msgPreparing, kind, source.name), false)
		}
		result, err := func() (*Artifact, error) {
			if prepared := opts.Prepared[source.name]; prepared != nil {
				return prepared.clone(), nil
			}
			switch {
			case strings.Contains(path, "/subscriptions/"):
				id := strings.Split(path[strings.LastIndex(path, "/subscriptions/")+len("/subscriptions/"):], "/")[0]
				sub, ok := subscriptions[id]
				if !ok {
					return nil, &InputError{Field: "subscription.id"}
				}
				return PrepareSubscription(ctx, sub, snapshot, SubscriptionOptions{Fetch: fetchSource(env.SubscriptionFetch), AllowTransportFallback: opts.AllowTransportFallback, SourcePrepared: true, SourcePath: source.path})
			case strings.Contains(path, "/aggregations/"):
				id := strings.Split(path[strings.LastIndex(path, "/aggregations/")+len("/aggregations/"):], "/")[0]
				for _, agg := range config.Aggregations {
					if agg.ID == id {
						result, err := currentAggregationRenderer()(ctx, agg, config, main, snapshot, fetchSource(env.AggregationFetch))
						if err != nil {
							return nil, err
						}
						if result == nil {
							return nil, &InputError{Field: "aggregation response"}
						}
						result = result.clone()
						result.AggregationID = id
						return result, nil
					}
				}
				return nil, &InputError{Field: "aggregation.id"}
			default:
				return nil, dialerErr(msgProviderOrigin)
			}
		}()
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if err != nil {
			if isStrategyReference(err) {
				return providerContext(err, source.name, source.path, kind)
			}
			return err
		}
		result.Name, result.HasName = source.name, true
		rendered[source.url] = result
		renderOrder = append(renderOrder, source.url)
		if opts.Progress != nil {
			opts.Progress(fmt.Sprintf(msgPrepared, kind, source.name), false)
		}
		return nil
	}
	chains, err := snapshot.HasChains()
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		path, err := pathOf(source.url)
		if err != nil {
			return nil, err
		}
		render := opts.RenderAll || chains || opts.Requested != nil && source.name == requestedName
		if !render {
			for _, agg := range config.Aggregations {
				if strings.Contains(path, "/aggregations/"+agg.ID+"/") {
					for _, id := range agg.Nodes {
						if opaque[id] {
							render = true
							break
						}
					}
					break
				}
			}
		}
		if render {
			if err := materialize(source); err != nil {
				return nil, err
			}
		}
	}
	if !chains {
		for _, address := range renderOrder {
			result := rendered[address]
			if result.Content == "" {
				continue
			}
			path := memberText(yamlx.Get(definitions, result.Name), "path")
			nodes, err := readProviderNodes(result.Content, result.Name, path, msgNodeSource, msgPreparedSource)
			if err != nil {
				return nil, err
			}
			for _, node := range nodes {
				if nonNull(yamlx.Get(node, "dialer-proxy")) {
					chains = true
					break
				}
			}
			if chains {
				break
			}
		}
	}
	if chains {
		for _, source := range sources {
			if rendered[source.url] == nil {
				if err := materialize(source); err != nil {
					return nil, err
				}
			}
		}
	}
	bundle := []*Artifact{}
	for name, definition := range yamlx.Pairs(definitions) {
		result := rendered[memberText(definition, "url")]
		if result == nil || yamlx.Get(definition, "url") == nil {
			continue
		}
		path, ok := textValue(yamlx.Get(definition, "path"))
		if !ok {
			return nil, &InputError{Field: "proxy-providers.path"}
		}
		item := result.clone()
		item.Name, item.HasName, item.URL, item.LocalPath = name, true, memberText(definition, "url"), path
		bundle = append(bundle, item)
	}
	if opts.Progress != nil {
		opts.Progress(fmt.Sprintf(msgValidating, len(bundle)), true)
	}
	if _, err := ValidateArtifacts(snapshot, bundle); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return bundle, nil
}

// CommitWrites publishes only while the originating Runtime remains live.
func CommitWrites(ctx context.Context, updates []CacheWrite) error {
	ctx = lifecycle.Detach(ctx)
	for _, update := range updates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := substore.SaveCache(ctx, update.SubID, update.Nodes, update.Metadata); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// CommitArtifacts deduplicates staged writes and retains shutdown cancellation
// through each repository leaf lock and atomic file commit.
func CommitArtifacts(ctx context.Context, profileID string, bundle []*Artifact) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	var order, artifactOrder []string
	updates := map[string]CacheWrite{}
	artifacts := map[string]string{}
	for _, item := range bundle {
		for _, update := range item.CacheUpdates {
			if _, ok := updates[update.SubID]; !ok {
				order = append(order, update.SubID)
			}
			updates[update.SubID] = update
		}
		if item.AggregationID != "" {
			if _, ok := artifacts[item.AggregationID]; !ok {
				artifactOrder = append(artifactOrder, item.AggregationID)
			}
			artifacts[item.AggregationID] = item.Content
		}
	}
	list := make([]CacheWrite, 0, len(order))
	for _, id := range order {
		list = append(list, updates[id])
	}
	if err := CommitWrites(ctx, list); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	for _, id := range artifactOrder {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := r.WriteProfileText(ctx, profileID, "providers/"+id+".yaml", []byte(artifacts[id])); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// nodeIdentity adds receipt metadata with the shared ordered record codec. This
// preserves the original cache-file framing while native callers use structs.
func nodeIdentity(node *model.Node, sub model.Subscription, nextID func() string) error {
	data, err := model.Marshal(node)
	if err != nil {
		return err
	}
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, pair := range [][2]string{{"subscription_id", sub.ID}, {"subscription_name", sub.Name}} {
		data, err := model.Marshal(pair[1])
		if err != nil {
			return err
		}
		if err := fields.Set(pair[0], data); err != nil {
			return err
		}
	}
	if node.ID == "" {
		data, err := model.Marshal(nextID())
		if err != nil {
			return err
		}
		if err := fields.Set("id", data); err != nil {
			return err
		}
	}
	data, err = fields.MarshalJSON()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, node)
}
