package substore

import (
	"bytes"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/subs"
	"configflow/internal/timefmt"
	"configflow/internal/yamlx"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"
)

var cacheLog = logx.Get("backend.utils.subscription_cache")

// ContentSyncInvalidator invalidates cached Agent content observations.
type ContentSyncInvalidator func(context.Context) error

func isFileError(err error) bool {
	var pe *fs.PathError
	var le *os.LinkError
	var errno syscall.Errno
	return errors.Is(err, store.ErrStorage) || errors.As(err, &pe) || errors.As(err, &le) || errors.As(err, &errno)
}
func ensureCacheDir(ctx context.Context, r *repo.Repository) (string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r == nil {
		return "", errors.New("substore: repository is nil")
	}
	dir, err := r.SharedCacheDir()
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0777); err != nil {
		cacheLog.Warning(fmt.Sprintf(msgCacheDir, err))
	}
	return dir, nil
}
func cacheName(id string) string {
	return strings.NewReplacer("/", "_", `\`, "_").Replace(id) + ".json"
}

// CachePath resolves the safe shared cache path.
func CachePath(ctx context.Context, r *repo.Repository, id string) (string, error) {
	dir, err := ensureCacheDir(ctx, r)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheName(id)), nil
}

// DeleteSubscriptionCache removes a node cache with the canonical repository leaf lease.
func DeleteSubscriptionCache(ctx context.Context, r *repo.Repository, id string) (bool, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if r == nil {
		return false, errors.New("substore: repository is nil")
	}
	if path, err := r.SharedPath(filepath.Join("subscribes", cacheName(id))); err == nil {
		defer memoForget(path)
	}
	removed, err := r.RemoveSharedFile(ctx, filepath.Join("subscribes", cacheName(id)))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false, err
	}
	if err != nil {
		cacheLog.Warning(fmt.Sprintf(msgCacheDelete, id, err))
		return false, nil
	}
	return removed, nil
}

// getShared reads the shared resources with the load issues of their rows.
var getShared = func(ctx context.Context, r *repo.Repository) (*model.Shared, []model.FieldIssue, error) {
	shared, err := r.Shared(ctx)
	if err != nil {
		return nil, nil, err
	}
	issues, err := r.StoredIssues(ctx)
	return shared, issues, err
}

// PruneOrphanCaches uses one typed shared snapshot before deleting any orphan.
func PruneOrphanCaches(ctx context.Context, r *repo.Repository) ([]string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, errors.New("substore: repository is nil")
	}
	shared, issues, err := getShared(ctx, r)
	if err != nil {
		cacheLog.Warning(fmt.Sprintf(msgCachePrune, err))
		return []string{}, nil
	}
	// A stored subscription list that read with a type issue (not a list, an
	// element that is not an object, an ID of another type) is not proof of
	// which caches are orphans: nothing is deleted (fail closed).
	if shared == nil || shared.Subscriptions == nil || typeIssueUnder(issues, "shared.subscriptions") {
		cacheLog.Warning(msgCacheSubscriptionsShape)
		return []string{}, nil
	}
	keep := map[string]bool{}
	for _, sub := range shared.Subscriptions {
		if sub.ID == "" {
			cacheLog.Warning(fmt.Sprintf(msgCachePrune, msgCacheSubscriptionID))
			return []string{}, nil
		}
		keep[cacheName(sub.ID)] = true
	}
	dir, err := ensureCacheDir(ctx, r)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		cacheLog.Warning(fmt.Sprintf(msgCachePrune, err))
		return []string{}, nil
	}
	removed := []string{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || keep[entry.Name()] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		ok, err := r.RemoveSharedFile(ctx, filepath.Join("subscribes", entry.Name()))
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return removed, err
		}
		if err != nil {
			cacheLog.Warning(fmt.Sprintf(msgCacheOrphan, entry.Name(), err))
			continue
		}
		if ok {
			memoForget(filepath.Join(dir, entry.Name()))
			removed = append(removed, entry.Name())
		}
	}
	if len(removed) > 0 {
		cacheLog.Info(fmt.Sprintf(msgCachePruned, len(removed)))
	}
	return removed, nil
}

// SaveCache persists typed nodes and metadata in one atomic repository file write.
func SaveCache(ctx context.Context, r *repo.Repository, id string, nodes []model.Node, metadata *CacheMetadata) (*Cache, error) {
	count := model.Int(len(nodes))
	return saveCache(ctx, r, id, &Cache{Count: &count, Nodes: nodes, Metadata: metadata}, nil)
}
func saveCache(ctx context.Context, r *repo.Repository, id string, payload *Cache, onHostsChange func(context.Context)) (*Cache, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	metadata := payload.Metadata
	path, err := CachePath(ctx, r, id)
	if err != nil {
		return nil, err
	}
	// Python: `if metadata:` (an empty dict is no metadata).
	if metadata != nil && *metadata == (CacheMetadata{}) {
		metadata = nil
	}
	payload.SubscriptionID = id
	payload.UpdatedAt = timefmt.UTCZ(cacheClock.Now())
	payload.Metadata = metadata
	if payload.Count == nil {
		count := model.Int(len(payload.Nodes))
		payload.Count = &count
	}
	count := *payload.Count
	if metadata != nil && metadata.FetchedAt != nil && *metadata.FetchedAt != "" {
		payload.UpdatedAt = *metadata.FetchedAt
	}
	previous, err := LoadCache(ctx, r, id)
	if err != nil {
		return nil, err
	}
	data, err := model.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, data, "", "  "); err != nil {
		return nil, err
	}
	formatted.WriteByte('\n')
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := r.WriteSharedText(ctx, filepath.Join("subscribes", filepath.Base(path)), formatted.Bytes()); err != nil {
		if !isFileError(err) {
			return nil, err
		}
		cacheLog.Error(fmt.Sprintf(msgCacheWrite, id, err))
		return payload, nil
	}
	if previous != nil && previous.Count != nil && *previous.Count != count {
		cacheLog.Info(fmt.Sprintf(msgCacheCount, logx.LogText(subscriptionLabel(metadata, id), 120), *previous.Count, count))
	}
	if ctx.Err() == nil {
		noteHostsChange(ctx, id, previous, payload, onHostsChange)
	}
	return payload, nil
}
func subscriptionLabel(metadata *CacheMetadata, id string) string {
	if metadata != nil && metadata.SubscriptionName != "" {
		return metadata.SubscriptionName
	}
	return id
}
func cacheHosts(cache *Cache) *subs.HostsData {
	if cache != nil && cache.Metadata != nil {
		return cache.Metadata.Hosts
	}
	return nil
}
func noteHostsChange(ctx context.Context, id string, previous, current *Cache, onHostsChange func(context.Context)) {
	old, new := cacheHosts(previous), cacheHosts(current)
	var before, after model.OrderedMap[model.Dynamic]
	if old != nil {
		before = old.Entries
	}
	if new != nil {
		after = new.Entries
	}
	added, removed, changed := 0, 0, 0
	for key, value := range after.All() {
		prior, ok := before.Get(key)
		if !ok {
			added++
			continue
		}
		a, e1 := value.Node()
		b, e2 := prior.Node()
		if e1 != nil || e2 != nil || !yamlx.Equal(a, b) {
			changed++
		}
	}
	for key := range before.All() {
		if _, ok := after.Get(key); !ok {
			removed++
		}
	}
	if added+removed+changed == 0 {
		return
	}
	cacheLog.Info(fmt.Sprintf(msgCacheHosts, logx.LogText(subscriptionLabel(current.Metadata, id), 120), added, removed, changed))
	if onHostsChange != nil {
		onHostsChange(ctx)
	}
}

func invokeContentInvalidator(ctx context.Context, fn ContentSyncInvalidator) {
	if fn == nil {
		return
	}
	defer func() {
		if recover() != nil {
			cacheLog.Debug(msgCacheInvalidator)
		}
	}()
	if err := fn(ctx); err != nil {
		cacheLog.Debug(msgCacheInvalidator)
	}
}

// LoadCache reads one owned typed record. Malformed files are ignored without
// discarding valid files elsewhere. The decode is reused while the file is
// unchanged (cache_memo.go); the caller still gets its own copy.
func LoadCache(ctx context.Context, r *repo.Repository, id string) (*Cache, error) {
	path, err := CachePath(ctx, r, id)
	if err != nil {
		return nil, err
	}
	key, ok := statCacheFile(path)
	if !ok {
		memoForget(path)
		return nil, nil
	}
	if entry := memoLookup(path, key); entry.cache != nil {
		return cloneCache(entry.cache), nil
	}
	cache, err := decodeCacheFile(ctx, r, id, path)
	if err != nil || cache == nil {
		return nil, err
	}
	memoStore(path, key, func(entry *cacheMemoEntry) { entry.cache = cache })
	return cloneCache(cache), nil
}

// LoadCacheShared is LoadCache without the caller's copy: the record is
// shared by every reader of the unchanged file and must not be modified. The
// same pointer is returned while the file is unchanged, so derived values may
// be keyed by it.
func LoadCacheShared(ctx context.Context, r *repo.Repository, id string) (*Cache, error) {
	path, err := CachePath(ctx, r, id)
	if err != nil {
		return nil, err
	}
	key, ok := statCacheFile(path)
	if !ok {
		memoForget(path)
		return nil, nil
	}
	if entry := memoLookup(path, key); entry.cache != nil {
		return entry.cache, nil
	}
	cache, err := decodeCacheFile(ctx, r, id, path)
	if err != nil || cache == nil {
		return nil, err
	}
	memoStore(path, key, func(entry *cacheMemoEntry) { entry.cache = cache })
	return cache, nil
}

// LoadSubscriptionCacheShared returns a read-only subs.SubscriptionCache view
// sharing the cached file's decoded Nodes slice without JSON round-tripping.
// The returned value and its slices must not be modified by the caller.
func LoadSubscriptionCacheShared(ctx context.Context, r *repo.Repository, id string) (*subs.SubscriptionCache, error) {
	cache, err := LoadCacheShared(ctx, r, id)
	if err != nil || cache == nil {
		return nil, err
	}
	var metadata *subs.SourceMetadata
	if cache.Metadata != nil {
		metadata = &subs.SourceMetadata{
			Hosts:         cache.Metadata.Hosts,
			Dialers:       cache.Metadata.Dialers,
			DialersStored: cache.Metadata.Dialers != nil,
		}
		if cache.Metadata.URL != nil {
			metadata.URL = *cache.Metadata.URL
		}
	}
	return &subs.SubscriptionCache{
		Nodes:    cache.Nodes,
		Metadata: metadata,
	}, nil
}

// LoadCacheJSON is the model encoding of LoadCache's record (nil when there
// is none), reused while the file is unchanged; the caller owns the bytes.
func LoadCacheJSON(ctx context.Context, r *repo.Repository, id string) (json.RawMessage, error) {
	path, err := CachePath(ctx, r, id)
	if err != nil {
		return nil, err
	}
	key, ok := statCacheFile(path)
	if ok {
		if entry := memoLookup(path, key); entry.cache != nil && entry.encoded != nil {
			return bytes.Clone(entry.encoded), nil
		}
	}
	cache, err := LoadCacheShared(ctx, r, id)
	if err != nil || cache == nil {
		return nil, err
	}
	data, err := model.Marshal(cache)
	if err != nil {
		return nil, err
	}
	memoStore(path, key, func(entry *cacheMemoEntry) {
		if entry.cache == cache {
			entry.encoded = data
		}
	})
	return bytes.Clone(data), nil
}

func decodeCacheFile(ctx context.Context, r *repo.Repository, id, path string) (*Cache, error) {
	if r == nil {
		return nil, errors.New("substore: repository is nil")
	}
	data, err := r.ReadSharedBytes(ctx, filepath.Join("subscribes", filepath.Base(path)))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	if err != nil {
		cacheLog.Warning(fmt.Sprintf(msgCacheRead, id, err))
		return nil, nil
	}
	return decodeCacheData(id, data), nil
}

// decodeCacheData decodes a stored cache file leniently; nil (with a warning)
// when it is not a readable object.
func decodeCacheData(id string, data []byte) *Cache {
	if data = bytes.TrimSpace(data); len(data) == 0 || data[0] != '{' {
		cacheLog.Warning(fmt.Sprintf(msgCacheRead, id, msgCacheRootShape))
		return nil
	}
	if !utf8.Valid(data) {
		cacheLog.Warning(fmt.Sprintf(msgCacheRead, id, msgCacheRootShape))
		return nil
	}
	var cache Cache
	data, _ = jsonresp.NormalizeNonFinite(data)
	issues, err := model.DecodeStored(data, &cache, "")
	if err != nil {
		cacheLog.Warning(fmt.Sprintf(msgCacheRead, id, fmt.Errorf("decode stored JSON: %w", err)))
		return nil
	}
	var envelope struct {
		Count    json.RawMessage `json:"count"`
		Nodes    jsontext.Value  `json:"nodes"`
		Metadata jsontext.Value  `json:"metadata"`
	}
	if jsondecode.Unmarshal(data, &envelope) == nil {
		cache.StoredCount = envelope.Count
		issues = append(issues, nodeElementIssues(envelope.Nodes, issues)...)
		if cache.Metadata != nil {
			var metadata struct {
				URL json.RawMessage `json:"url"`
			}
			if jsondecode.Unmarshal(envelope.Metadata, &metadata) == nil && metadata.URL == nil {
				cache.Metadata.URLAbsent = true
			}
		}
	}
	cache.Issues = issues
	if cache.Count == nil {
		var envelope struct {
			Nodes json.RawMessage `json:"nodes"`
			Count json.RawMessage `json:"count"`
		}
		if jsondecode.Unmarshal(data, &envelope) == nil && envelope.Count == nil && len(envelope.Nodes) > 0 && envelope.Nodes[0] == '[' {
			var nodes []json.RawMessage
			if json.Unmarshal(envelope.Nodes, &nodes) == nil {
				count := model.Int(len(nodes))
				cache.Count = &count
			}
		}
	}
	return &cache
}

// CacheSummary holds the lightweight envelope fields of a subscription node
// cache without decoding individual nodes.
type CacheSummary struct {
	HasNodes  bool
	UpdatedAt *string
	URL       *string
	URLBad    bool
}

// LoadCacheSummary reads only the cache envelope (whether valid node objects
// are present, metadata.url, and updated_at) without decoding each node. It
// is reused while the file is unchanged, like LoadCache.
func LoadCacheSummary(ctx context.Context, r *repo.Repository, id string) (*CacheSummary, error) {
	path, err := CachePath(ctx, r, id)
	if err != nil {
		return nil, err
	}
	key, ok := statCacheFile(path)
	if !ok {
		memoForget(path)
		return nil, nil
	}
	if entry := memoLookup(path, key); entry.summary != nil {
		return cloneSummary(entry.summary), nil
	}
	summary, err := readCacheSummary(ctx, r, id, path)
	if err != nil || summary == nil {
		return nil, err
	}
	memoStore(path, key, func(entry *cacheMemoEntry) { entry.summary = summary })
	return cloneSummary(summary), nil
}

func readCacheSummary(ctx context.Context, r *repo.Repository, id, path string) (*CacheSummary, error) {
	if r == nil {
		return nil, errors.New("substore: repository is nil")
	}
	data, err := r.ReadSharedBytes(ctx, filepath.Join("subscribes", filepath.Base(path)))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	if err != nil {
		cacheLog.Warning(fmt.Sprintf(msgCacheRead, id, err))
		return nil, nil
	}
	if data = bytes.TrimSpace(data); len(data) == 0 || data[0] != '{' {
		cacheLog.Warning(fmt.Sprintf(msgCacheRead, id, msgCacheRootShape))
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := jsonresp.DecodeStored(data, &fields); err != nil {
		cacheLog.Warning(fmt.Sprintf(msgCacheRead, id, err))
		return nil, nil
	}
	summary := &CacheSummary{}
	if rawNodes, ok := fields["nodes"]; ok {
		var items []json.RawMessage
		if json.Unmarshal(rawNodes, &items) == nil && len(items) > 0 {
			allObjects := true
			for _, item := range items {
				trimmed := bytes.TrimSpace(item)
				if len(trimmed) == 0 || trimmed[0] != '{' {
					allObjects = false
					break
				}
			}
			summary.HasNodes = allObjects
		}
	}
	if rawMeta, ok := fields["metadata"]; ok {
		trimmed := bytes.TrimSpace(rawMeta)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			summary.URLBad = true
		} else {
			var meta map[string]json.RawMessage
			if json.Unmarshal(trimmed, &meta) != nil {
				summary.URLBad = true
			} else if rawURL, hasURL := meta["url"]; hasURL {
				trimmedURL := bytes.TrimSpace(rawURL)
				if string(trimmedURL) != "null" {
					var address string
					if len(trimmedURL) == 0 || trimmedURL[0] != '"' || json.Unmarshal(trimmedURL, &address) != nil {
						summary.URLBad = true
					} else {
						summary.URL = &address
					}
				}
			}
		}
	}
	if rawUpdated, ok := fields["updated_at"]; ok {
		trimmed := bytes.TrimSpace(rawUpdated)
		if len(trimmed) > 0 && trimmed[0] == '"' {
			var updated string
			if json.Unmarshal(trimmed, &updated) == nil {
				summary.UpdatedAt = &updated
			}
		}
	}
	return summary, nil
}

var cacheClock Clock = SystemClock

// typeIssueUnder reports a type issue at path or below it (undeclared members
// are dropped harmlessly and do not count).
func typeIssueUnder(issues []model.FieldIssue, path string) bool {
	for _, issue := range issues {
		if issue.Kind == model.IssueType && model.IssueUnder([]model.FieldIssue{issue}, path) {
			return true
		}
	}
	return false
}

// nodeElementIssues reports the elements of a stored node list that are not
// objects and have no issue yet (a null element decodes as an empty node
// without one); such a list is damaged (Cache.NodesDamaged), as before O18
// T1d and as in Python, whose publication failed on the element.
func nodeElementIssues(nodes jsontext.Value, issues []model.FieldIssue) []model.FieldIssue {
	if nodes.Kind() != '[' {
		return nil
	}
	var elements []jsontext.Value
	if jsondecode.Unmarshal(nodes, &elements) != nil {
		return nil
	}
	var out []model.FieldIssue
	for i, element := range elements {
		path := fmt.Sprintf("nodes[%d]", i)
		if element.Kind() == '{' || slices.ContainsFunc(issues, func(issue model.FieldIssue) bool { return issue.Path == path }) {
			continue
		}
		out = append(out, model.FieldIssue{Path: path, Kind: model.IssueType})
	}
	return out
}
