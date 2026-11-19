package refresh

// Existing substore/subs owners still expose JSON records on this baseline.
// These adapters translate one owned record at a time; they do not implement
// their cache declarations, node parsing, filters, hosts or dialer schemas.
import (
	"configflow/internal/convert"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"context"
	"encoding/json"
	"errors"
)

func decodeLegacy(value, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(data, target)
}
func legacyObject(value any) *jsonx.Object {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	parsed, _ := jsonx.Parse(data)
	object, _ := parsed.(*jsonx.Object)
	return object
}
func legacyNodes(nodes []model.Node) []any {
	data, _ := model.Marshal(nodes)
	value, _ := jsonx.Parse(data)
	result, _ := value.([]any)
	return result
}
func typedResult(result *substore.SubscriptionResult) *SourceResult {
	if result == nil {
		return nil
	}
	var traffic *Traffic
	if result.Traffic != nil {
		traffic = &Traffic{}
		_ = decodeLegacy(result.Traffic, traffic)
	}
	return &SourceResult{Text: result.Text, Source: result.Source, FetchedAt: result.FetchedAt, Traffic: traffic}
}
func oldResult(result *SourceResult) *substore.SubscriptionResult {
	if result == nil {
		return nil
	}
	var traffic *jsonx.Object
	if result.Traffic != nil {
		traffic = legacyObject(result.Traffic)
	}
	return &substore.SubscriptionResult{Text: result.Text, Source: result.Source, FetchedAt: result.FetchedAt, Traffic: traffic}
}
func readSharedData(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	value, err := r.ReadSharedJSON(ctx, path)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
func writeSharedData(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = r.WriteSharedJSON(ctx, path, value)
	return err
}
func parseTraffic(header string) (*Traffic, error) {
	value, err := substore.ParseUserinfo(header)
	if err != nil || value == nil {
		return nil, err
	}
	var result Traffic
	if err := decodeLegacy(value, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

type nodeConverter struct{ old convert.ProxyStringConverter }

func (c nodeConverter) ConvertProxy(ctx context.Context, text string) (*yamlx.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.old == nil {
		return nil, nil
	}
	value := c.old.ConvertProxyString(ctx, text)
	if value == nil {
		return nil, nil
	}
	return yamlbridge.Encode(value)
}
func refreshEnvFromLegacy(e *Env) *RefreshEnv {
	return &RefreshEnv{Clock: e.Clock, RuleClient: e.RuleClient, HTTP: e.HTTP, RandomHex: e.RandomHex, Sources: SourceOwners{
		Fetch: func(ctx context.Context, id, url string, options substore.FetchOptions) (*SourceResult, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if e.FetchSubscription == nil {
				return nil, errors.New("subscription fetch owner unavailable")
			}
			result, err := e.FetchSubscription(ctx, id, url, options)
			return typedResult(result), errorFromLegacy(err)
		},
		ParseNodes: func(ctx context.Context, text string) ([]model.Node, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			proxies, err := substore.ParseProxiesFromYAML(text)
			if err != nil {
				return nil, err
			}
			nodes, err := substore.ProxiesToNodes(proxies)
			if err != nil {
				return nil, err
			}
			var result []model.Node
			err = decodeLegacy(nodes, &result)
			return result, err
		},
		Metadata: func(ctx context.Context, text string) (json.RawMessage, json.RawMessage, error) {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			var hosts, dialers json.RawMessage
			if value := subs.ExtractSubscriptionHosts(text); value != nil {
				var err error
				hosts, err = json.Marshal(value)
				if err != nil {
					return nil, nil, err
				}
			}
			value, err := subs.ExtractSubscriptionDialers(text)
			if err != nil {
				return nil, nil, err
			}
			if value != nil {
				dialers, err = json.Marshal(value)
			}
			return hosts, dialers, err
		},
		SaveNodes: func(ctx context.Context, id string, nodes []model.Node, metadata *SourceMetadata) (*NodeCache, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			value, err := substore.SaveSubscriptionNodes(ctx, id, legacyNodes(nodes), legacyObject(metadata))
			if err != nil || value == nil {
				return nil, err
			}
			var result NodeCache
			err = decodeLegacy(value, &result)
			return &result, err
		},
		LoadNodes: func(ctx context.Context, id string) (*NodeCache, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			value, err := substore.LoadSubscriptionCache(ctx, id)
			if err != nil || value == nil {
				return nil, err
			}
			var result NodeCache
			err = decodeLegacy(value, &result)
			return &result, err
		},
		FilterNodes: func(sub model.Subscription, nodes []model.Node) []model.Node {
			filtered := subs.FilterSubscriptionItems(legacyObject(sub), legacyNodes(nodes))
			var result []model.Node
			_ = decodeLegacy(filtered, &result)
			return result
		},
		Converter: nodeConverter{e.converter()},
	}}
}

// ParseUserinfo adapts the existing quota parser to the legacy API.
func ParseUserinfo(header string) (*jsonx.Object, error) { return substore.ParseUserinfo(header) }

// FetchUserinfo validates the legacy URL shape before native quota fetching.
func FetchUserinfo(ctx context.Context, value any) (*jsonx.Object, error) {
	url, ok := value.(string)
	if !ok {
		return nil, nil
	}
	traffic, err := FetchTraffic(ctx, url)
	if traffic == nil {
		return nil, err
	}
	return legacyObject(traffic), err
}

// LoadHealth adapts raw snapshot members without interpreting unowned entries.
func LoadHealth(ctx context.Context) (*jsonx.Object, error) {
	data, err := ReadHealth(detached(ctx))
	if err != nil {
		return nil, err
	}
	return legacyObject(data), nil
}

// FetchRecord is the old boundary for a fetch outcome. Native owners use FetchEventInput.
type FetchRecord struct {
	OK        bool
	Count     any
	FromCache bool
	Traffic   *jsonx.Object
}

// RecordFetch adapts one outcome to the native health writer.
func RecordFetch(ctx context.Context, id string, value FetchRecord) (*jsonx.Object, error) {
	var count model.Int
	_ = decodeLegacy(value.Count, &count)
	var traffic *Traffic
	if value.Traffic != nil {
		traffic = &Traffic{}
		_ = decodeLegacy(value.Traffic, traffic)
	}
	result, err := RecordHealth(ctx, id, FetchEventInput{OK: value.OK, Count: count, FromCache: value.FromCache, Traffic: traffic})
	if err != nil {
		return nil, err
	}
	return legacyObject(result), nil
}

// Forget adapts the legacy health delete entry point.
func Forget(ctx context.Context, id string) error { return ForgetHealth(ctx, id) }

func sourceRecord(value *jsonx.Object) model.Subscription {
	var result model.Subscription
	_ = decodeLegacy(value, &result)
	return result
}
func libraryRecord(value *jsonx.Object) model.RuleLibraryItem {
	var result model.RuleLibraryItem
	_ = decodeLegacy(value, &result)
	return result
}
func newResult(text, source string, traffic *jsonx.Object, at string) *substore.SubscriptionResult {
	var quota *Traffic
	if traffic != nil {
		quota = &Traffic{}
		_ = decodeLegacy(traffic, quota)
	}
	return oldResult(makeResult(text, source, quota, at))
}
func fetchedAtValue(result *substore.SubscriptionResult) any {
	if result.FetchedAt == "" {
		return nil
	}
	return result.FetchedAt
}

// SourceCacheMetadata adapts one source/result record to the native metadata owner.
func SourceCacheMetadata(sub *jsonx.Object, result *substore.SubscriptionResult) (*jsonx.Object, error) {
	metadata, err := CacheMetadata(context.Background(), sourceRecord(sub), typedResult(result))
	if err != nil {
		return nil, err
	}
	return legacyObject(metadata), nil
}

// RememberSourceDownload adapts the source transport receipt write.
func RememberSourceDownload(ctx context.Context, sub *jsonx.Object, result *substore.SubscriptionResult) (*substore.SubscriptionResult, error) {
	value, err := RememberDownload(ctx, sourceRecord(sub), typedResult(result))
	return oldResult(value), err
}

// DownloadSubscriptionNodes adapts nodes and receipts for existing source callers.
func DownloadSubscriptionNodes(ctx context.Context, sub *jsonx.Object) ([]any, *substore.SubscriptionResult, *jsonx.Object, error) {
	nodes, result, cache, err := DownloadNodes(ctx, sourceRecord(sub))
	if err != nil {
		return nil, nil, nil, err
	}
	return legacyNodes(nodes), oldResult(result), legacyObject(cache), nil
}

// SourceFetch is the current baseline's subscription callback shape.
type SourceFetch func(context.Context, string, string) (*substore.SubscriptionResult, error)

// FetchSubscriptionSource adapts an unported fetch callback to the native flow.
func FetchSubscriptionSource(ctx context.Context, sub *jsonx.Object, fetch SourceFetch) (*substore.SubscriptionResult, error) {
	var native FetchSourceFn
	if fetch != nil {
		native = func(ctx context.Context, id, url string) (*SourceResult, error) {
			result, err := fetch(ctx, id, url)
			return typedResult(result), errorFromLegacy(err)
		}
	}
	result, err := FetchSource(ctx, sourceRecord(sub), native)
	return oldResult(result), err
}
func isFresh(timestamp any, interval int64) bool {
	text, _ := timestamp.(string)
	return fresh(text, interval)
}
