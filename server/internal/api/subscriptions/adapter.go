package subscriptions

// All calls to packages that still expose jsonx documents are confined here.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/health/check"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/notifications"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/weburl"
)

func legacy(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}
func legacyObject(value object) (*jsonx.Object, error) {
	v, err := legacy(value)
	if err != nil {
		return nil, err
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		return nil, errors.New("expected resource object")
	}
	return o, nil
}
func native(value any, target any) error {
	data, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
func nativeObject(value *jsonx.Object) (object, error) {
	var out object
	err := native(value, &out)
	return out, err
}
func nativeObjects(value []any) ([]object, error) {
	out := []object{}
	err := native(value, &out)
	return out, err
}
func listShared(ctx context.Context) ([]object, []object, error) {
	shared, err := repo.GetSharedConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	subs, err := nativeObjects(shared.GetList("subscriptions"))
	if err != nil {
		return nil, nil, err
	}
	nodes, err := nativeObjects(shared.GetList("nodes"))
	return subs, nodes, err
}
func editSubscriptions(ctx context.Context, mutate func([]object) ([]object, error)) error {
	_, err := repo.UpdateSharedConfigTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		list, err := nativeObjects(shared.GetList("subscriptions"))
		if err != nil {
			return nil, err
		}
		list, err = mutate(list)
		if err != nil {
			return nil, err
		}
		v, err := legacy(list)
		if err != nil {
			return nil, err
		}
		shared.Set("subscriptions", v)
		return nil, nil
	})
	return err
}
func appendImported(ctx context.Context, nodes []object) (int, error) {
	added := 0
	_, err := repo.UpdateSharedConfigTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		existing := shared.GetList("nodes")
		names := map[string]bool{}
		for _, item := range existing {
			names[item.(*jsonx.Object).GetString("name")] = true
		}
		for _, node := range nodes {
			if names[node.text("name")] {
				continue
			}
			o, err := legacyObject(node)
			if err != nil {
				return nil, err
			}
			existing = append(existing, o)
			names[node.text("name")] = true
			added++
		}
		shared.Set("nodes", existing)
		return nil, nil
	})
	return added, err
}
func validateOptions(sub object) error {
	o, err := legacyObject(sub)
	if err != nil {
		return err
	}
	if message := subs.ValidateRegexFilter(o.Value("regex_filter")); message != "" {
		return errors.New(message)
	}
	if o.Has("interval") {
		_, err = refresh.ValidateRefreshInterval(o.Value("interval"), "")
	}
	return err
}
func initialize(sub object) error {
	o, err := legacyObject(sub)
	if err != nil {
		return err
	}
	check.InitializeProviderHealth(o)
	return native(o, &sub)
}
func filter(sub object, nodes []object) ([]object, error) {
	// Optional cache nodes decode to a nil slice. Keep an empty JSON array
	// when crossing the storage adapter so the legacy API receives a list.
	if nodes == nil {
		nodes = []object{}
	}
	o, err := legacyObject(sub)
	if err != nil {
		return nil, err
	}
	v, err := legacy(nodes)
	if err != nil {
		return nil, err
	}
	return nativeObjects(subs.FilterSubscriptionItems(o, v.([]any)))
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
	v, err := substore.LoadSubscriptionCache(ctx, id)
	if err != nil || v == nil {
		return nil, err
	}
	if summary {
		if _, ok := v.Value("nodes").([]any); !ok {
			v = v.DeepCopy()
			v.Delete("nodes")
		}
	}
	var out cache
	err = native(v, &out)
	return &out, err
}
func loadHealth(ctx context.Context) (map[string]healthEntry, error) {
	v, err := refresh.LoadHealth(ctx)
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
	v, err := r.GetResourceUsage(ctx, "subscriptions", id)
	return httpx.JSON(v), err
}
func forgetTask(ctx context.Context, id string) error {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	return notifications.TaskStatus(ctx, r.DataDir()).Forget(refresh.SubscriptionKey(id))
}
func forgetSubscription(ctx context.Context, id string) error {
	if err := refresh.Forget(ctx, id); err != nil {
		return err
	}
	if err := forgetTask(ctx, id); err != nil {
		return err
	}
	_, err := substore.DeleteSubscriptionCache(ctx, id)
	return err
}
func refreshSubscription(ctx context.Context, sub object) ([]object, *cache, error) {
	o, err := legacyObject(sub)
	if err != nil {
		return nil, nil, err
	}
	nodes, _, payload, err := refresh.RefreshSubscription(ctx, o, "manual")
	if err != nil {
		return nil, nil, err
	}
	list, err := nativeObjects(nodes)
	if err != nil {
		return nil, nil, err
	}
	var c cache
	err = native(payload, &c)
	return list, &c, err
}
func reorder(current []object, body object) ([]object, []string, error) {
	if _, err := rules.OrderIDs(body, "subscriptions"); err != nil {
		return nil, nil, httpx.BadRequest(err.Error())
	}
	v, err := legacy(current)
	if err != nil {
		return nil, nil, err
	}
	b, err := legacyObject(body)
	if err != nil {
		return nil, nil, err
	}
	out, missing := rules.ResolveNewOrder(v.([]any), b, "subscriptions")
	list, err := nativeObjects(out)
	return list, missing, err
}
func authorize(c *httpx.Ctx) (*httpx.Response, error) {
	v, err := auth.ValidateTokenOrJWT(c, nil)
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
	config    *jsonx.Object
	main      *jsonx.Object
	snapshot  *provider.Snapshot
	profileID string
}
type staged struct{ updates []provider.CacheUpdate }

func resource(ctx context.Context, id *string) (*delivery, []object, error) {
	config, err := repo.GetResourceConfig(ctx, "subscriptions", id, "")
	if err != nil {
		return nil, nil, err
	}
	list, err := nativeObjects(config.GetList("subscriptions"))
	if err != nil {
		return nil, nil, err
	}
	return &delivery{config: config, profileID: config.GetString("profile_id")}, list, nil
}
func (d *delivery) capture(ctx context.Context) error {
	main, err := convert.GenerateMihomoDocument(ctx, d.config, convert.GenerateOptions{SkipProviderPreflight: true})
	if err != nil {
		return err
	}
	d.main = main
	d.snapshot, err = provider.Capture(d.profileID, main, d.config)
	return err
}
func (d *delivery) hasChains() (bool, error) { return d.snapshot.HasChains() }
func (d *delivery) validate(proxies []object, name *string) error {
	v, err := legacy(proxies)
	if err != nil {
		return err
	}
	return d.snapshot.Validate(v, false, name)
}
func (d *delivery) adapt(ctx context.Context, sub object, proxies []object, chains bool) ([]object, error) {
	o, err := legacyObject(sub)
	if err != nil {
		return nil, err
	}
	v, err := legacy(proxies)
	if err != nil {
		return nil, err
	}
	out, err := d.snapshot.Adapt(ctx, o, v.([]any), chains)
	if err != nil {
		return nil, err
	}
	return nativeObjects(out)
}
func fetchRaw(ctx context.Context, sub object) ([]object, *substore.SubscriptionResult, error) {
	o, err := legacyObject(sub)
	if err != nil {
		return nil, nil, err
	}
	result, err := refresh.FetchSubscriptionSource(ctx, o, func(ctx context.Context, id, url string) (*substore.SubscriptionResult, error) {
		return substore.Default().Client.GetSubscriptionProxiesYAML(ctx, id, url, "", substore.FetchOptions{})
	})
	if err != nil {
		return nil, nil, err
	}
	proxies, err := provider.ParseProviderProxies(result.Text, sub.text("name"), "")
	if err != nil {
		return nil, nil, err
	}
	list, err := nativeObjects(proxies)
	return list, result, err
}
func stageNodes(sub object, proxies []object, result *substore.SubscriptionResult, withIdentity bool) (staged, error) {
	if len(proxies) == 0 || result.Source == "cache" {
		return staged{}, nil
	}
	o, err := legacyObject(sub)
	if err != nil {
		return staged{}, err
	}
	v, err := legacy(proxies)
	if err != nil {
		return staged{}, err
	}
	nodes, err := substore.ProxiesToNodes(v.([]any))
	if err != nil {
		return staged{}, err
	}
	if withIdentity {
		for _, item := range nodes {
			n := item.(*jsonx.Object)
			n.Set("subscription_id", sub.text("id"))
			n.Set("subscription_name", sub.text("name"))
			if !n.Has("id") {
				n.Set("id", "node_"+substore.Default().Env.RandomHex())
			}
		}
	}
	metadata, err := refresh.SourceCacheMetadata(o, result)
	if err != nil {
		return staged{}, err
	}
	return staged{[]provider.CacheUpdate{{SubID: sub.text("id"), Nodes: nodes, Metadata: metadata}}}, nil
}
func commitStaged(ctx context.Context, updates []staged) error {
	var all []provider.CacheUpdate
	for _, update := range updates {
		all = append(all, update.updates...)
	}
	return provider.CommitCacheUpdates(ctx, all)
}
func cachedProxies(ctx context.Context, sub object, nodes []object, strict bool) ([]object, error) {
	v, err := legacy(nodes)
	if err != nil {
		return nil, err
	}
	if err := convert.ValidateShapes(jsonx.Obj("nodes", v), false); err != nil {
		return nil, err
	}
	o, err := legacyObject(sub)
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, item := range subs.FilterSubscriptionItems(o, v.([]any)) {
		p, err := convert.ConvertNodeToMihomo(ctx, item.(*jsonx.Object), substore.Converter())
		if err != nil {
			if strict && referenceError(err) {
				return nil, err
			}
			continue
		}
		if p != nil {
			out = append(out, p)
		}
	}
	return nativeObjects(out)
}
func alternative(format string, proxies []object) (string, error) {
	v, err := legacy(proxies)
	if err != nil {
		return "", err
	}
	if format == "surge" {
		return convert.ConvertProxiesToSurgeText(v.([]any), substore.ProxiesToNodes)
	}
	return convert.ConvertProxiesToLoonText(v.([]any), substore.ProxiesToNodes)
}
func requestedFilters(c *httpx.Ctx) (*[2]string, error) { return provider.RequestedFilters(c) }
func filterResponse(proxies []object, filters *[2]string) ([]object, error) {
	if filters == nil {
		return proxies, nil
	}
	v, err := legacy(proxies)
	if err != nil {
		return nil, err
	}
	out, err := rules.FilterCandidates(v.([]any), filters[0], filters[1])
	if err != nil {
		return nil, err
	}
	return nativeObjects(out)
}
func (d *delivery) validateBundle(ctx context.Context, sub object, prepared map[string][]object, combined []object) error {
	opts := provider.BundleOptions{Prepared: map[string]*provider.Rendered{}}
	if sub != nil {
		o, err := legacyObject(sub)
		if err != nil {
			return err
		}
		opts.Requested = &provider.Requested{Kind: "subscription", Resource: o}
	}
	for name, proxies := range prepared {
		content, err := yamlContent(proxies)
		if err != nil {
			return err
		}
		opts.Prepared[name] = &provider.Rendered{Content: content, CacheUpdates: []provider.CacheUpdate{}}
	}
	main := d.main.DeepCopy()
	if combined != nil {
		names := map[string]bool{}
		for _, p := range combined {
			names[p.text("name")] = true
		}
		all := []any{}
		for _, p := range main.GetList("proxies") {
			if !names[p.(*jsonx.Object).GetString("name")] {
				all = append(all, p)
			}
		}
		v, err := legacy(combined)
		if err != nil {
			return err
		}
		main.Set("proxies", append(all, v.([]any)...))
	}
	_, err := provider.PrepareProviderBundle(ctx, d.config, main, opts)
	return err
}
func (d *delivery) chainedContent(ctx context.Context, sub object, format string) (string, error) {
	o, err := legacyObject(sub)
	if err != nil {
		return "", err
	}
	bundle, err := provider.PrepareProviderBundle(ctx, d.config, d.main, provider.BundleOptions{Requested: &provider.Requested{Kind: "subscription", Resource: o}})
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
		if item.Name == sub.text("name") {
			content = item.Content
			found = true
			break
		}
	}
	if !found {
		return "", errors.New("provider missing from bundle")
	}
	if err := provider.CommitProviderBundle(ctx, d.profileID, bundle); err != nil {
		return "", err
	}
	return content, nil
}
func (d *delivery) serve(c *httpx.Ctx, id string, render provider.Render) (*httpx.Response, error) {
	return provider.ServeProvider(c, "subscription", id, d.profileID, d.config, render)
}
func recordFetch(ctx context.Context, id string, ok, fromCache bool, count int) error {
	_, err := refresh.RecordFetch(ctx, id, refresh.FetchRecord{OK: ok, FromCache: fromCache, Count: jsonx.Int(int64(count))})
	return err
}

func materializeContext(ctx context.Context) context.Context {
	return convert.WithNodeConversions(ctx, convert.NewConversionResults(), true)
}

func logProviderFailure(err error) {
	logx.Get("backend.routes.subscriptions").Error(msgProviderFailed + ": " + weburl.SafeErrorDetails(err))
}
