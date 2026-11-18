package substore

import (
	"bytes"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
)

var cacheLog = logx.Get("backend.utils.subscription_cache")

// ContentSyncInvalidator invalidates cached Agent content observations.
type ContentSyncInvalidator func(context.Context) error

var contentSync atomic.Pointer[ContentSyncInvalidator]

// SetContentSyncInvalidator installs the Agent owner's callback.
func SetContentSyncInvalidator(fn ContentSyncInvalidator) {
	if fn == nil {
		contentSync.Store(nil)
	} else {
		contentSync.Store(&fn)
	}
}
func isOSError(err error) bool {
	var pe *fs.PathError
	var le *os.LinkError
	var errno syscall.Errno
	return errors.Is(err, store.ErrStorage) || errors.As(err, &pe) || errors.As(err, &le) || errors.As(err, &errno)
}
func osErrorText(err error) string { return err.Error() }
func ensureCacheDir(ctx context.Context) (string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
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
func CachePath(ctx context.Context, id string) (string, error) {
	dir, err := ensureCacheDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheName(id)), nil
}
func removeFile(path string) error {
	if err := syscall.Unlink(path); err != nil {
		return &fs.PathError{Op: "remove", Path: path, Err: err}
	}
	return nil
}

// DeleteSubscriptionCache removes a node cache with the canonical repository leaf lease.
func DeleteSubscriptionCache(ctx context.Context, id string) (bool, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return false, err
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

var getShared = func(ctx context.Context, r *repo.Repository) (*model.Shared, error) { return r.Shared(ctx) }

// PruneOrphanCaches uses one typed shared snapshot before deleting any orphan.
func PruneOrphanCaches(ctx context.Context) ([]string, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := getShared(ctx, r)
	if err != nil {
		cacheLog.Warning(fmt.Sprintf(msgCachePrune, err))
		return []string{}, nil
	}
	if shared == nil || shared.Subscriptions == nil || model.IssueUnder(model.Issues(shared), "subscriptions") {
		cacheLog.Warning(msgCacheSubscriptionsShape)
		return []string{}, nil
	}
	keep := map[string]bool{}
	for _, sub := range shared.Subscriptions {
		if sub.ID == "" || model.IssueUnder(model.Issues(sub), "id") {
			cacheLog.Warning(fmt.Sprintf(msgCachePrune, msgCacheSubscriptionID))
			return []string{}, nil
		}
		keep[cacheName(sub.ID)] = true
	}
	dir, err := ensureCacheDir(ctx)
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
			removed = append(removed, entry.Name())
		}
	}
	if len(removed) > 0 {
		cacheLog.Info(fmt.Sprintf(msgCachePruned, len(removed)))
	}
	return removed, nil
}

// SaveCache persists typed nodes and metadata in one atomic repository file write.
func SaveCache(ctx context.Context, id string, nodes []model.Node, metadata *CacheMetadata) (*Cache, error) {
	count := model.Int(len(nodes))
	return saveCache(ctx, id, &Cache{Count: &count, Nodes: nodes, Metadata: metadata})
}
func saveCache(ctx context.Context, id string, payload *Cache) (*Cache, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	metadata := payload.Metadata
	path, err := CachePath(ctx, id)
	if err != nil {
		return nil, err
	}
	if metadata != nil {
		data, err := model.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(data, []byte("{}")) {
			metadata = nil
		}
	}
	if metadata == nil {
		if err := model.ClearMember(payload, &payload.Raw, "metadata"); err != nil {
			return nil, err
		}
	}
	payload.SubscriptionID = id
	payload.UpdatedAt = strings.Replace(isoFormat(cacheClock.Now()), "+00:00", "Z", 1)
	payload.Metadata = metadata
	if payload.Count == nil {
		count := model.Int(len(payload.Nodes))
		payload.Count = &count
	}
	count := *payload.Count
	if metadata != nil && metadata.FetchedAt != "" {
		payload.UpdatedAt = metadata.FetchedAt
	}
	previous, err := LoadCache(ctx, id)
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
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := r.WriteSharedText(ctx, filepath.Join("subscribes", filepath.Base(path)), formatted.Bytes()); err != nil {
		if !isOSError(err) {
			return nil, err
		}
		cacheLog.Error(fmt.Sprintf(msgCacheWrite, id, err))
		return payload, nil
	}
	if previous != nil && previous.Count != nil && *previous.Count != count {
		cacheLog.Info(fmt.Sprintf(msgCacheCount, logx.LogText(subscriptionLabel(metadata, id), 120), *previous.Count, count))
	}
	if ctx.Err() == nil {
		noteHostsChange(ctx, id, previous, payload)
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
func noteHostsChange(ctx context.Context, id string, previous, current *Cache) {
	old, new := cacheHosts(previous), cacheHosts(current)
	var before, after model.Map[model.Dynamic]
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
	invalidate := contentSync.Load()
	if invalidate == nil {
		return
	}
	defer func() {
		if recover() != nil {
			cacheLog.Debug(msgCacheInvalidator)
		}
	}()
	if err := (*invalidate)(ctx); err != nil {
		cacheLog.Debug(msgCacheInvalidator)
	}
}

// LoadCache reads one owned typed record. Malformed files are ignored without discarding valid files elsewhere.
func LoadCache(ctx context.Context, id string) (*Cache, error) {
	path, err := CachePath(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
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
	var cache Cache
	if err := jsonresp.DecodeStored(data, &cache); err != nil {
		cacheLog.Warning(fmt.Sprintf(msgCacheRead, id, err))
		return nil, nil
	}
	if cache.Count == nil {
		var envelope struct {
			Nodes json.RawMessage `json:"nodes"`
			Count json.RawMessage `json:"count"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Count == nil && len(envelope.Nodes) > 0 && envelope.Nodes[0] == '[' {
			var nodes []json.RawMessage
			if json.Unmarshal(envelope.Nodes, &nodes) == nil {
				count := model.Int(len(nodes))
				cache.Count = &count
			}
		}
	}
	return &cache, nil
}

var cacheClock Clock = SystemClock
