package subscriptions

// Typed resource owners and provider delivery are wired here.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/store"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

func native(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(data, target)
}
func objects(value any) ([]proxy, error) {
	out := []proxy{}
	err := native(value, &out)
	return out, err
}
func proxyNodes(value []proxy) ([]*yamlx.Node, error) {
	if value == nil {
		value = []proxy{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	graph, err := yamlx.FromJSON(data)
	if err != nil {
		return nil, err
	}
	return graph.Content, nil
}
func proxyObjects(value []*yamlx.Node) ([]proxy, error) {
	out := make([]proxy, 0, len(value))
	for _, node := range value {
		data, err := yamlx.ToJSON(node)
		if err != nil {
			return nil, err
		}
		var item proxy
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}
func listShared(ctx context.Context) ([]model.Subscription, []model.Node, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, nil, err
	}
	shared, err := r.Shared(ctx)
	if err != nil {
		return nil, nil, err
	}
	return shared.Subscriptions, shared.Nodes, nil
}
func editSubscriptions(ctx context.Context, mutate func([]model.Subscription) ([]model.Subscription, error)) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	return r.UpdateShared(ctx, func(ctx context.Context, shared *model.Shared) error {
		list, err := mutate(slices.Clone(shared.Subscriptions))
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		shared.Subscriptions = list
		return nil
	})
}
func appendImported(ctx context.Context, nodes []proxy) (int, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return 0, err
	}
	added := 0
	err = r.UpdateShared(ctx, func(ctx context.Context, shared *model.Shared) error {
		names := map[string]bool{}
		for _, node := range shared.Nodes {
			names[node.Name] = true
		}
		for _, node := range nodes {
			if names[node.text("name")] {
				continue
			}
			var typed model.Node
			if err := native(node, &typed); err != nil {
				return err
			}
			shared.Nodes = append(shared.Nodes, typed)
			names[node.text("name")] = true
			added++
		}
		return ctx.Err()
	})
	return added, err
}

func validateOptions(sent *subscriptionRequest) error {
	if !sent.RegexFilter.Null() {
		if !sent.RegexFilter.OK {
			return errors.New(msgFilterType)
		}
		if message := subs.ValidateFilter(sent.RegexFilter.Value); message != "" {
			return errors.New(message)
		}
	}
	if sent.Interval.Present() {
		_, err := refresh.ParseInterval(sent.Interval.Raw, "")
		return err
	}
	return nil
}

type subscriptionErrorItem struct {
	ID          json.RawMessage `json:"id,omitempty"`
	Interval    json.RawMessage `json:"interval,omitempty"`
	HealthCheck json.RawMessage `json:"health_check,omitempty"`
}

func subscriptionTypeError(req *subscriptionRequest, overrideID string, cause error) error {
	idRaw := req.ID.Raw
	if overrideID != "" {
		idRaw, _ = json.Marshal(overrideID)
	}
	list, err := json.Marshal([]subscriptionErrorItem{{
		ID:          idRaw,
		Interval:    req.Interval.Raw,
		HealthCheck: req.HealthCheck.Raw,
	}})
	if err != nil {
		return err
	}
	return repo.InputError(&model.MemberTypeError{Record: "shared", Member: "subscriptions", Value: list, Err: cause})
}
func filter(sub model.Subscription, nodes []proxy) ([]proxy, error) {
	source := sub
	graphs, err := proxyNodes(nodes)
	if err != nil {
		return nil, err
	}
	return proxyObjects(subs.FilterProxies(source, graphs))
}
func loadCache(ctx context.Context, id string) (*cache, error) {
	return readCache(ctx, id, false)
}

// loadCacheSummary tolerates legacy non-array nodes only for read-only cache
// metadata/preview routes. Publication continues to use strict decoding.
func loadCacheSummary(ctx context.Context, id string) (*cache, error) {
	return readCache(ctx, id, true)
}
func readCache(ctx context.Context, id string, summary bool) (*cache, error) {
	v, err := substore.LoadCache(ctx, id)
	if err != nil || v == nil {
		return nil, err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	// Summary routes tolerate a malformed legacy node collection, while a
	// publication still rejects it before conversion or cache writes.
	if !summary && v.NodesDamaged() {
		return nil, errors.New("cached nodes must be an array of node objects")
	}
	if summary && v.NodesDamaged() {
		var fields proxy
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, err
		}
		delete(fields, "nodes")
		data, err = json.Marshal(fields)
		if err != nil {
			return nil, err
		}
	}
	var out cache
	err = json.Unmarshal(data, &out)
	return &out, err
}
func loadHealth(ctx context.Context) (map[string]healthEntry, error) {
	v, err := refresh.ReadHealth(lifecycle.Detach(ctx))
	if err != nil {
		return nil, err
	}
	var out map[string]healthEntry
	err = native(v, &out)
	return out, err
}
func healthSize() int { return refresh.HistorySize }
func usage(ctx context.Context, id string) (*httpx.Response, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	v, err := r.ResourceUsage(ctx, "subscriptions", id)
	return httpx.JSON(v), err
}
func forgetTask(ctx context.Context, id string) error {
	ctx = lifecycle.Detach(ctx)
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	return notifications.TaskStatus(ctx, r.DataDir()).ForgetContext(ctx, refresh.SourceTaskKey(id))
}
func forgetSubscription(ctx context.Context, id string) error {
	ctx = lifecycle.Detach(ctx)
	if err := refresh.ForgetHealth(ctx, id); err != nil {
		return err
	}
	if err := forgetTask(ctx, id); err != nil {
		return err
	}
	_, err := substore.DeleteSubscriptionCache(ctx, id)
	return err
}
func refreshSubscription(ctx context.Context, sub model.Subscription) ([]proxy, *cache, error) {
	source := sub
	nodes, _, payload, err := refresh.RefreshSource(ctx, source, "manual")
	if err != nil {
		return nil, nil, err
	}
	list, err := objects(nodes)
	if err != nil {
		return nil, nil, err
	}
	var c cache
	err = native(payload, &c)
	return list, &c, err
}
func reorder(current []model.Subscription, body json.RawMessage) ([]model.Subscription, []string, error) {
	request, err := rules.DecodeOrder(body, "subscriptions")
	if err != nil {
		return nil, nil, httpx.BadRequest(err.Error())
	}
	out, missing := rules.Reorder(current, request.IDs, request.Position, func(item model.Subscription) string { return item.ID })
	return out, missing, nil
}
func authorize(c *httpx.Ctx) (*httpx.Response, error) {
	v, err := auth.ValidateSystemTokenOrJWT(c, nil)
	if err != nil {
		return nil, err
	}
	if !v.Valid {
		return httpx.Message(401, false, v.Message), nil
	}
	return nil, nil
}
func failureDetail(err error) string { return "request_failed (" + weburl.SafeErrorDetails(err) + ")" }
func referenceError(err error) bool  { var e *rules.StrategyReferenceError; return errors.As(err, &e) }

type delivery struct {
	config    *model.Config
	head      *store.Head // committed head config was resolved from (provider flight key)
	main      *yamlx.Node
	snapshot  *provider.DeliverySnapshot
	profileID string
}
type staged struct{ updates []provider.CacheWrite }

func resource(ctx context.Context, id *string) (*delivery, []model.Subscription, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, nil, err
	}
	profileID, err := repo.ResolveProfileID(ctx, "")
	if err != nil {
		return nil, nil, err
	}
	roots := repo.ResourceRoots{"subscriptions": nil}
	if id != nil {
		roots["subscriptions"] = []string{*id}
	}
	config, head, err := r.ConfigAt(ctx, profileID, roots)
	if err != nil {
		return nil, nil, err
	}
	return &delivery{config: config, head: head, profileID: profileID}, config.Subscriptions, nil
}
func (d *delivery) capture(ctx context.Context) error {
	main, err := convert.MihomoDocument(ctx, d.config, convert.MihomoOptions{SkipProviderPreflight: true})
	if err != nil {
		return err
	}
	d.main = main
	d.snapshot, err = provider.CaptureDelivery(d.profileID, main, d.config.Profile)
	return err
}
func (d *delivery) hasChains() (bool, error) { return d.snapshot.HasChains() }
func (d *delivery) validate(proxies []proxy, name *string) error {
	nodes, err := proxyNodes(proxies)
	if err != nil {
		return err
	}
	return d.snapshot.Validate(nodes, false, name)
}
func (d *delivery) adapt(ctx context.Context, sub model.Subscription, proxies []proxy, chains bool) ([]proxy, error) {
	source := sub
	nodes, err := proxyNodes(proxies)
	if err != nil {
		return nil, err
	}
	out, err := d.snapshot.AdaptNodes(ctx, source, nodes, chains)
	if err != nil {
		return nil, err
	}
	return proxyObjects(out)
}
func fetchRaw(ctx context.Context, sub model.Subscription) ([]proxy, *refresh.SourceResult, error) {
	source := sub
	result, err := refresh.FetchSource(ctx, source, provider.DefaultDeliveryEnv().Fetch)
	if err != nil {
		return nil, nil, err
	}
	proxies, err := provider.ParseNodes(result.Text, sub.Name, "")
	if err != nil {
		return nil, nil, err
	}
	list, err := proxyObjects(proxies)
	return list, result, err
}
func stageNodes(sub model.Subscription, proxies []proxy, result *refresh.SourceResult, withIdentity bool) (staged, error) {
	if len(proxies) == 0 || result.Source == "cache" {
		return staged{}, nil
	}
	source := sub
	graphs, err := proxyNodes(proxies)
	if err != nil {
		return staged{}, err
	}
	nodes, err := substore.ProxyNodes(graphs)
	if err != nil {
		return staged{}, err
	}
	if withIdentity {
		for i := range nodes {
			nodes[i].SubscriptionID = source.ID
			nodes[i].SubscriptionName = source.Name
			if nodes[i].ID == "" {
				nodes[i].ID = "node_" + substore.Default().Env.RandomHex()
			}
		}
	}
	metadata, err := refresh.CacheMetadata(context.Background(), source, result)
	if err != nil {
		return staged{}, err
	}
	var receipt substore.CacheMetadata
	if err := native(metadata, &receipt); err != nil {
		return staged{}, err
	}
	return staged{[]provider.CacheWrite{{SubID: source.ID, Nodes: nodes, Metadata: &receipt}}}, nil
}
func commitStaged(ctx context.Context, updates []staged) error {
	var all []provider.CacheWrite
	for _, update := range updates {
		all = append(all, update.updates...)
	}
	return provider.CommitWrites(ctx, all)
}
func cachedProxies(ctx context.Context, sub model.Subscription, nodes []proxy, strict bool) ([]proxy, error) {
	var typed []model.Node
	if err := native(nodes, &typed); err != nil {
		return nil, err
	}
	if err := convert.ConfigShapes(&model.Config{Nodes: typed}, false); err != nil {
		return nil, convert.DialerFailure(err)
	}
	source := sub
	out := []*yamlx.Node{}
	skipped := 0
	for index, item := range subs.FilterNodes(source, typed) {
		p, err := convert.NodeProxy(ctx, item, convert.OptionalNodeConverter(substore.NativeConverter()))
		if err != nil {
			if strict && referenceError(err) {
				return nil, err
			}
			var yamlError *convert.ProxyYAMLError
			if !strict && errors.As(err, &yamlError) {
				kind := "yaml_value"
				switch {
				case errors.Is(err, yamlx.ErrAliasRecursion):
					kind = "alias_recursion"
				case errors.Is(err, yamlx.ErrAliasBudget):
					kind = "alias_budget"
				case errors.Is(err, yamlx.ErrPolicy):
					kind = "yaml_policy"
				}
				skipped++
				logx.Get("backend.routes.subscriptions").Warningf(msgProxyYAMLSkipped, index+1, kind)
			}
			continue
		}
		if p != nil {
			out = append(out, p)
		}
	}
	if skipped > 0 {
		logx.Get("backend.routes.subscriptions").Warningf(msgProxyYAMLSkipSummary, skipped)
	}
	return proxyObjects(out)
}
func alternative(format string, proxies []proxy) (string, error) {
	nodes, err := proxyNodes(proxies)
	if err != nil {
		return "", err
	}
	return convert.ClientProxyText(nodes, substore.ProxyNodes, format)
}
func requestedFilters(c *httpx.Ctx) (*[2]string, error) { return provider.RequestedFilters(c) }
func filterResponse(proxies []proxy, filters *[2]string) ([]proxy, error) {
	if filters == nil {
		return proxies, nil
	}
	return rules.Filter(proxies, &filters[0], &filters[1], func(item proxy) string { return item.text("name") })
}
func (d *delivery) validateBundle(ctx context.Context, sub *model.Subscription, prepared map[string][]proxy, combined []proxy) error {
	opts := provider.DeliveryOptions{Prepared: map[string]*provider.Artifact{}}
	if sub != nil {
		opts.Requested = &provider.BundleRequest{Kind: "subscription", Subscription: sub}
	}
	for name, proxies := range prepared {
		content, err := yamlContent(proxies)
		if err != nil {
			return err
		}
		opts.Prepared[name] = &provider.Artifact{Content: content, CacheUpdates: []provider.CacheWrite{}}
	}
	main := yamlx.CopyGraph(d.main)
	if combined != nil {
		names := map[string]bool{}
		for _, p := range combined {
			names[p.text("name")] = true
		}
		all := []*yamlx.Node{}
		current := yamlx.Get(main, "proxies")
		if current != nil {
			for _, p := range current.Content {
				data, err := yamlx.ToJSON(yamlx.Get(p, "name"))
				if err != nil {
					return err
				}
				var name string
				_ = json.Unmarshal(data, &name)
				if !names[name] {
					all = append(all, p)
				}
			}
		}
		graphs, err := proxyNodes(combined)
		if err != nil {
			return err
		}
		yamlx.Set(main, "proxies", &yamlx.Node{Kind: 2, Tag: "!!seq", Content: append(all, graphs...)})
	}
	_, err := provider.PrepareBundle(ctx, d.config, main, opts)
	return err
}
func (d *delivery) chainedContent(ctx context.Context, sub model.Subscription, format string) (string, error) {
	bundle, err := provider.PrepareBundle(ctx, d.config, d.main, provider.DeliveryOptions{Requested: &provider.BundleRequest{Kind: "subscription", Subscription: &sub}})
	if err != nil {
		return "", err
	}
	if format == "surge" || format == "loon" {
		client := "Surge"
		if format == "loon" {
			client = "Loon"
		}
		return "", &rules.StrategyReferenceError{Message: fmt.Sprintf(msgUnsupportedChain, client)}
	}
	content := ""
	found := false
	for _, item := range bundle {
		if item.Name == sub.Name {
			content = item.Content
			found = true
			break
		}
	}
	if !found {
		return "", errors.New("provider missing from bundle")
	}
	if err := provider.CommitArtifacts(ctx, d.profileID, bundle); err != nil {
		return "", err
	}
	return content, nil
}
func (d *delivery) serve(c *httpx.Ctx, id string, render provider.Render) (*httpx.Response, error) {
	return provider.Serve(c, "subscription", id, d.profileID, d.head, render)
}
func recordFetch(ctx context.Context, id string, ok, fromCache bool, count int) error {
	ctx = lifecycle.Detach(ctx)
	_, err := refresh.RecordHealth(ctx, id, refresh.FetchEventInput{OK: ok, FromCache: fromCache, Count: model.Int(count)})
	return err
}

func materializeContext(ctx context.Context) context.Context {
	return convert.WithConversions(ctx, convert.NewConversions(), true)
}

func logProviderFailure(ctx context.Context, err error) {
	if cause := ctx.Err(); cause != nil && errors.Is(err, cause) {
		return
	}
	logx.Get("backend.routes.subscriptions").Error(msgProviderFailed + ": " + weburl.SafeErrorDetails(err))
}
