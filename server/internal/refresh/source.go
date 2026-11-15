package refresh

// Port of backend/utils/subscription_refresh.py: upstream subscription
// refresh kept apart from client/provider polling.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/taskstatus"
	"configflow/internal/yamlx"
)

// requiredString is sub[key] for a key the code indexes directly: KeyError
// when missing; a value that is not a string cannot be used as an id or an
// address (Python fails later with a TypeError/AttributeError of its own).
func requiredString(item *jsonx.Object, key string) (string, error) {
	value, present := item.Get(key)
	if !present {
		return "", pyErr("KeyError", "%s", jsonx.Repr(key))
	}
	text, ok := value.(string)
	if !ok {
		return "", pyErr("TypeError", "%s must be str, not %s", key, pyTypeName(value))
	}
	return text, nil
}

// newResult is SubscriptionResult(text, source, traffic, fetched_at) with
// the environment's clock for a missing fetched_at.
func newResult(text, source string, traffic *jsonx.Object, fetchedAt string) *substore.SubscriptionResult {
	if fetchedAt == "" {
		fetchedAt = isoMicro(currentEnv().Clock.Now())
	}
	return &substore.SubscriptionResult{Text: text, Source: source, Traffic: traffic, FetchedAt: fetchedAt}
}

// fetchedAtValue is getattr(result, 'fetched_at', None).
func fetchedAtValue(result *substore.SubscriptionResult) any {
	if result.FetchedAt == "" {
		return nil
	}
	return result.FetchedAt
}

// SourceCacheMetadata is source_cache_metadata(sub, result). Only a
// downloaded body can carry the subscription's own top-level hosts and the
// groups its chained nodes need; Sub-Store conversions and cache renders
// contain proxies alone.
func SourceCacheMetadata(sub *jsonx.Object, result *substore.SubscriptionResult) (*jsonx.Object, error) {
	metadata := jsonx.Obj("subscription_name", sub.GetOr("name", ""), "url", sub.Value("url"),
		"fetched_at", fetchedAtValue(result))
	if result.Source != "cache" && result.Source != "prepared_yaml" {
		if hosts := subs.ExtractSubscriptionHosts(result.Text); hosts != nil {
			metadata.Set("hosts", hosts)
		}
		dialers, err := subs.ExtractSubscriptionDialers(result.Text)
		if err != nil {
			return nil, err
		}
		if dialers != nil {
			metadata.Set("dialers", dialers)
		}
	}
	return metadata, nil
}

// isFresh is _is_fresh(timestamp, interval): fetched less than interval
// seconds ago (and not in the future).
func isFresh(timestamp any, interval int64) bool {
	updated, ok := taskstatus.ParseTime(timestamp)
	if !ok {
		return false
	}
	now := currentEnv().Clock.Now()
	micros := (now.Unix()-updated.Unix())*1_000_000 + int64(now.Nanosecond()/1000) - int64(updated.Nanosecond()/1000)
	return micros >= 0 && micros < interval*1_000_000
}

// DownloadCachePath is _download_cache_path(url): the transport cache of a
// subscription address (shared-relative). It is separate from validated
// node caches and bounds upstream requests of dry previews and checks.
func DownloadCachePath(url string) string {
	sum := sha256.Sum256([]byte(url))
	return "subscription-downloads/" + hex.EncodeToString(sum[:]) + ".json"
}

// RememberSourceDownload is remember_source_download(sub, result): store
// the downloaded text with its time and traffic in the transport cache and
// return the result with its fetched_at fixed.
func RememberSourceDownload(ctx context.Context, sub *jsonx.Object, result *substore.SubscriptionResult) (*substore.SubscriptionResult, error) {
	result = newResult(result.Text, result.Source, result.Traffic, result.FetchedAt)
	url, err := requiredString(sub, "url")
	if err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	var traffic any
	if result.Traffic != nil {
		traffic = result.Traffic
	}
	if _, err := r.WriteSharedJSON(ctx, DownloadCachePath(url), jsonx.Obj(
		"text", result.Text, "fetched_at", result.FetchedAt, "traffic", traffic)); err != nil {
		return nil, err
	}
	return result, nil
}

// errNoNodes is the ValueError of download_subscription_nodes.
func errNoNodes() error {
	return pyErr("ValueError", "订阅没有返回任何节点，已保留原有缓存")
}

// DownloadSubscriptionNodes is download_subscription_nodes(sub): download,
// parse and store a subscription; it returns (nodes, result, cache). A
// failure returns before anything is stored, so the previous nodes stay.
func DownloadSubscriptionNodes(ctx context.Context, sub *jsonx.Object) ([]any, *substore.SubscriptionResult, *jsonx.Object, error) {
	id, err := requiredString(sub, "id")
	if err != nil {
		return nil, nil, nil, err
	}
	url, err := requiredString(sub, "url")
	if err != nil {
		return nil, nil, nil, err
	}
	e := currentEnv()
	fetched, err := e.FetchSubscription(ctx, id, url, substore.FetchOptions{Refresh: true})
	if err != nil {
		return nil, nil, nil, err
	}
	result, err := RememberSourceDownload(ctx, sub, fetched)
	if err != nil {
		return nil, nil, nil, err
	}
	proxies, err := substore.ParseProxiesFromYAML(result.Text)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(proxies) == 0 {
		return nil, nil, nil, errNoNodes()
	}
	nodes, err := substore.ProxiesToNodes(proxies)
	if err != nil {
		return nil, nil, nil, err
	}
	name, present := sub.Get("name")
	if !present && len(nodes) > 0 {
		return nil, nil, nil, pyErr("KeyError", "'name'")
	}
	for _, item := range nodes {
		node := item.(*jsonx.Object)
		node.Set("subscription_id", id)
		node.Set("subscription_name", name)
		if !node.Has("id") {
			node.Set("id", "node_"+e.RandomHex())
		}
	}
	metadata, err := SourceCacheMetadata(sub, result)
	if err != nil {
		return nil, nil, nil, err
	}
	cache, err := substore.SaveSubscriptionNodes(ctx, id, nodes, metadata)
	if err != nil {
		return nil, nil, nil, err
	}
	return nodes, result, cache, nil
}

// SourceFetch is the fetch= argument of fetch_subscription_source:
// fetch(sub_id, url).
type SourceFetch func(ctx context.Context, subID, url string) (*substore.SubscriptionResult, error)

// cacheMetadataURL is cache.get('metadata', {}).get('url'): a metadata
// value that is not an object is Python's AttributeError.
func cacheMetadataURL(cache *jsonx.Object) (any, error) {
	value, present := cache.Get("metadata")
	if !present {
		return nil, nil
	}
	metadata, ok := value.(*jsonx.Object)
	if !ok {
		return nil, pyErr("AttributeError", "'%s' object has no attribute 'get'", pyTypeName(value))
	}
	return metadata.Value("url"), nil
}

// FetchSubscriptionSource is fetch_subscription_source(sub, fetch=):
// automatic delivery uses the stored nodes of the current address (the
// scheduled task refreshes them); in manual mode (interval 0) only those.
// Otherwise a fresh transport cache is reused before fetch is called.
// Returned cached content still goes through each consumer's filtering and
// validation; a poll never rewrites a timestamp.
func FetchSubscriptionSource(ctx context.Context, sub *jsonx.Object, fetch SourceFetch) (*substore.SubscriptionResult, error) {
	interval, err := ValidateRefreshInterval(sub.GetOr("interval", jsonx.Int(DefaultSourceInterval)), "")
	if err != nil {
		return nil, &rules.StrategyReferenceError{Message: err.Error()}
	}
	id, err := requiredString(sub, "id")
	if err != nil {
		return nil, err
	}
	cache, err := substore.LoadSubscriptionCache(ctx, id)
	if err != nil {
		return nil, err
	}
	if cache != nil && cache.Len() > 0 {
		url, err := cacheMetadataURL(cache)
		if err != nil {
			return nil, err
		}
		if url != nil && !jsonx.Equal(url, sub.Value("url")) {
			cache = nil
		}
	}
	var cachedNodes []any
	usable := false
	if cache != nil && cache.Len() > 0 {
		cachedNodes, usable = cache.Value("nodes").([]any)
		usable = usable && len(cachedNodes) > 0
	}
	current := false
	if usable {
		url, err := cacheMetadataURL(cache)
		if err != nil {
			return nil, err
		}
		current = jsonx.Equal(url, sub.Value("url"))
	}
	if interval == 0 || current {
		if !usable {
			name := sub.GetOr("name", id)
			return nil, &rules.StrategyReferenceError{Message: "订阅「" + jsonx.Str(name) +
				"」已关闭自动更新，且没有当前订阅地址的可用缓存。请先在「订阅来源」中手动更新，再重新发布或同步。"}
		}
		if err := convert.ValidateShapes(jsonx.Obj("nodes", cachedNodes), false); err != nil {
			return nil, err
		}
		proxies := []any{}
		for _, item := range cachedNodes {
			proxy, err := convert.ConvertNodeToMihomo(ctx, item.(*jsonx.Object), currentEnv().converter())
			if err != nil {
				return nil, err
			}
			if proxy != nil && proxy.Len() > 0 {
				proxies = append(proxies, proxy)
			}
		}
		text, err := yamlx.Dump(jsonx.Obj("proxies", proxies), yamlx.DumpOptions{AllowUnicode: true, SortKeys: true})
		if err != nil {
			return nil, err
		}
		return newResult(text, "cache", nil, ""), nil
	}
	url, err := requiredString(sub, "url")
	if err != nil {
		return nil, err
	}
	if r, err := repo.GetRepository(ctx); err != nil {
		return nil, err
	} else if downloaded, err := r.ReadSharedJSON(ctx, DownloadCachePath(url)); err == nil {
		text, isText := downloaded.Value("text").(string)
		if isText && isFresh(downloaded.Value("fetched_at"), interval) {
			traffic, _ := downloaded.Value("traffic").(*jsonx.Object)
			return newResult(text, "source_cache", traffic, downloaded.Value("fetched_at").(string)), nil
		}
	} else if !isRepositoryOrOSError(err) {
		return nil, err
	}
	fetched, err := fetch(ctx, id, url)
	if err != nil {
		return nil, err
	}
	return RememberSourceDownload(ctx, sub, fetched)
}
