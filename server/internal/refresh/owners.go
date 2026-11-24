package refresh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"

	"configflow/internal/convert"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/subs"
	"configflow/internal/substore"
)

func readSharedData(ctx context.Context, r *repo.Repository, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errors.New("refresh: repository is nil")
	}
	return r.ReadSharedBytes(ctx, path)
}
func writeSharedData(ctx context.Context, r *repo.Repository, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return errors.New("refresh: repository is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, data, "", "  "); err != nil {
		return err
	}
	_, err := r.WriteSharedText(ctx, path, append(formatted.Bytes(), '\n'))
	return err
}
func parseTraffic(header string) (*Traffic, error) {
	value, err := substore.ReadUserinfo(header)
	if err != nil || value == nil {
		return nil, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result Traffic
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DefaultRefreshEnvFor wires production source owners bound to explicit
// Repository and Sub-Store service instances.
func DefaultRefreshEnvFor(r *repo.Repository, sub *substore.Service) *RefreshEnv {
	var conv convert.NodeConverter
	if sub != nil {
		conv = sub.Client
	}
	return &RefreshEnv{Clock: substore.SystemClock, RuleClient: rulefetch.DefaultClient, HTTP: substore.RequestsClient{}, RandomHex: func() string {
		if sub != nil && sub.Env != nil && sub.Env.RandomHex != nil {
			return sub.Env.RandomHex()
		}
		return substore.NewEnv().RandomHex()
	}, Sources: SourceOwners{
		Fetch: func(ctx context.Context, id, url string, options substore.FetchOptions) (*SourceResult, error) {
			if sub == nil {
				return nil, errors.New("refresh: substore service is nil")
			}
			result, err := sub.Client.Fetch(ctx, id, url, "", options)
			if err != nil || result == nil {
				return nil, err
			}
			var traffic *Traffic
			if result.Traffic != nil {
				data, err := json.Marshal(result.Traffic)
				if err != nil {
					return nil, err
				}
				traffic = &Traffic{}
				if err := json.Unmarshal(data, traffic); err != nil {
					return nil, err
				}
			}
			return &SourceResult{Text: result.Text, Source: result.Source, FetchedAt: result.FetchedAt, Traffic: traffic}, nil
		},
		ParseNodes: func(ctx context.Context, text string) ([]model.Node, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			proxies, err := substore.ParseProxyNodes(text)
			if err != nil {
				return nil, err
			}
			return substore.ProxyNodes(proxies)
		},
		Metadata: func(ctx context.Context, text string) (json.RawMessage, json.RawMessage, error) {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			var hosts, dialers json.RawMessage
			if value := subs.ExtractHosts(text); value != nil {
				var err error
				hosts, err = json.Marshal(value)
				if err != nil {
					return nil, nil, err
				}
			}
			value, err := subs.ExtractDialers(text)
			if err != nil {
				return nil, nil, err
			}
			if value != nil {
				dialers, err = json.Marshal(value)
			}
			return hosts, dialers, err
		},
		SaveNodes: func(ctx context.Context, id string, nodes []model.Node, metadata *SourceMetadata) (*NodeCache, error) {
			var receipt *substore.CacheMetadata
			if metadata != nil {
				raw, err := json.Marshal(metadata)
				if err != nil {
					return nil, err
				}
				receipt = &substore.CacheMetadata{}
				if err := json.Unmarshal(raw, receipt); err != nil {
					return nil, err
				}
			}
			if r == nil {
				return nil, errors.New("refresh: repository is nil")
			}
			var cache *substore.Cache
			var err error
			if sub != nil {
				cache, err = sub.SaveCache(ctx, r, id, nodes, receipt)
			} else {
				cache, err = substore.SaveCache(ctx, r, id, nodes, receipt)
			}
			return sourceCache(cache, err)
		},
		LoadNodes: func(ctx context.Context, id string) (*NodeCache, error) {
			if r == nil {
				return nil, errors.New("refresh: repository is nil")
			}
			return loadNodeCache(ctx, r, id)
		},
		SharedLoader: true,
		LoadCacheSummary: func(ctx context.Context, id string) (*substore.CacheSummary, error) {
			if r == nil {
				return nil, errors.New("refresh: repository is nil")
			}
			return substore.LoadCacheSummary(ctx, r, id)
		},
		FilterNodes: subs.FilterNodes,
		Converter:   conv,
	}}
}

// nodeCacheViews keeps, per subscription, the NodeCache view of the shared
// decoded cache file it was made from (O10 R8): the view is rebuilt only when
// substore hands out a different record, i.e. when the file changed.
var nodeCacheViews = struct {
	sync.Mutex
	views map[string]nodeCacheView
}{views: map[string]nodeCacheView{}}

type nodeCacheView struct {
	source *substore.Cache
	view   *NodeCache
}

// loadNodeCache returns the subscription's stored nodes, shared by every
// reader of the unchanged file: callers must not modify the result.
func loadNodeCache(ctx context.Context, r *repo.Repository, id string) (*NodeCache, error) {
	source, err := substore.LoadCacheShared(ctx, r, id)
	if err != nil || source == nil {
		nodeCacheViews.Lock()
		delete(nodeCacheViews.views, id)
		nodeCacheViews.Unlock()
		return nil, err
	}
	nodeCacheViews.Lock()
	cached := nodeCacheViews.views[id]
	nodeCacheViews.Unlock()
	if cached.source == source {
		return cached.view, nil
	}
	view, err := sourceCache(source, nil)
	if err != nil || view == nil {
		return view, err
	}
	nodeCacheViews.Lock()
	nodeCacheViews.views[id] = nodeCacheView{source: source, view: view}
	nodeCacheViews.Unlock()
	return view, nil
}

func sourceCache(cache *substore.Cache, err error) (*NodeCache, error) {
	if err != nil || cache == nil {
		return nil, err
	}
	// A missing or null updated_at reads as "" and is no timestamp.
	out := &NodeCache{SubscriptionID: cache.SubscriptionID, Nodes: cache.Nodes}
	if cache.UpdatedAt != "" {
		out.UpdatedAt = new(cache.UpdatedAt)
	}
	if cache.Count != nil {
		out.Count = *cache.Count
	}
	for _, issue := range cache.Issues {
		switch issue.Path {
		case "metadata", "metadata.url":
			out.MetadataDamaged = true
		case "updated_at":
			out.UpdatedAtDamaged = true
		}
	}
	if cache.Metadata != nil && !out.MetadataDamaged {
		raw, err := model.Marshal(cache.Metadata)
		if err != nil {
			return nil, err
		}
		out.Metadata = &SourceMetadata{}
		if _, err := model.DecodeStored(raw, out.Metadata, ""); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// detached keeps the admitted Runtime generation while completing request work.
func detached(ctx context.Context) context.Context { return lifecycle.Detach(ctx) }
