package substore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"

	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/subs"
)

var cacheLog = logx.Get("backend.utils.subscription_cache")

// ContentSyncInvalidator is agents.content_sync.invalidate_content_sync
// (provided by the agents domain, A13): drop cached Agent content checks so
// a subscription hosts change shows as 「有变更，待推送」 on the next poll.
type ContentSyncInvalidator func(ctx context.Context) error

var contentSync atomic.Pointer[ContentSyncInvalidator]

// SetContentSyncInvalidator registers the agents domain's invalidation
// (nil: none; the hosts change is then only logged).
func SetContentSyncInvalidator(fn ContentSyncInvalidator) {
	if fn == nil {
		contentSync.Store(nil)
		return
	}
	contentSync.Store(&fn)
}

// isOSError reports whether err is a Python OSError (a failed file
// operation) rather than a repository error.
func isOSError(err error) bool {
	if errors.Is(err, store.ErrStorage) {
		return true
	}
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	var errno syscall.Errno
	return errors.As(err, &pathErr) || errors.As(err, &linkErr) || errors.As(err, &errno)
}

// osErrorText is str(OSError) for a raw Go file error.
func osErrorText(err error) string {
	var storeErr *store.Error
	if errors.As(err, &storeErr) {
		return storeErr.Error()
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pyOSError(pathErr.Err, pathErr.Path)
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return pyOSError(linkErr.Err, linkErr.Old) + " -> " + jsonx.Repr(linkErr.New)
	}
	return err.Error()
}

// ensureCacheDir is _ensure_cache_dir: DATA_DIR/shared/subscribes, created
// when missing (a failure is only logged).
func ensureCacheDir(ctx context.Context) (string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	dir, err := r.SharedCacheDir()
	if err != nil {
		return "", err
	}
	if err := pyMakedirs(dir); err != nil {
		cacheLog.Warning("创建订阅缓存目录失败: " + osErrorText(err))
	}
	return dir, nil
}

// pyMakedirs is os.makedirs(name, exist_ok=True): missing parents first
// (their FileExistsError ignored), then mkdir(name); the error names the
// directory whose mkdir failed, as Python's does.
func pyMakedirs(name string) error {
	head, tail := filepath.Split(name)
	if tail == "" {
		head, tail = filepath.Split(strings.TrimRight(head, "/"))
	}
	head = strings.TrimRight(head, "/")
	if head == "" && strings.HasPrefix(name, "/") {
		head = "/"
	}
	if head != "" && tail != "" {
		if _, err := os.Stat(head); err != nil {
			if err := pyMakedirs(head); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			if tail == "." {
				return nil
			}
		}
	}
	if err := os.Mkdir(name, 0o777); err != nil {
		if info, serr := os.Stat(name); serr == nil && info.IsDir() {
			return nil
		}
		return err
	}
	return nil
}

// cacheName is _cache_name: "<id>.json" with "/" and "\" made safe.
func cacheName(subID string) string {
	return strings.NewReplacer("/", "_", `\`, "_").Replace(subID) + ".json"
}

// CachePath is _get_cache_path(sub_id).
func CachePath(ctx context.Context, subID string) (string, error) {
	dir, err := ensureCacheDir(ctx)
	if err != nil {
		return "", err
	}
	return pyJoin(dir, cacheName(subID)), nil
}

// removeFile is os.remove (unlink: never a directory).
func removeFile(path string) error {
	if err := syscall.Unlink(path); err != nil {
		return &fs.PathError{Op: "remove", Path: path, Err: err}
	}
	return nil
}

// removeCacheFile is _remove_cache_file: the cache and its lock file;
// true when the cache itself was removed.
func removeCacheFile(path string) (bool, error) {
	removed := false
	for _, target := range []string{path, path + ".lock"} {
		err := removeFile(target)
		if err == nil {
			removed = removed || target == path
			continue
		}
		if !errors.Is(err, syscall.ENOENT) {
			return removed, err
		}
	}
	return removed, nil
}

// DeleteSubscriptionCache is delete_subscription_cache: remove the node
// cache (node addresses and credentials) of a deleted subscription.
func DeleteSubscriptionCache(ctx context.Context, subID string) (bool, error) {
	path, err := CachePath(ctx, subID)
	if err != nil {
		return false, err
	}
	removed, err := removeCacheFile(path)
	if err != nil {
		cacheLog.Warning(fmt.Sprintf("删除订阅缓存失败 (%s): %s", subID, osErrorText(err)))
		return false, nil
	}
	return removed, nil
}

// PruneOrphanCaches is prune_orphan_caches: delete the caches of
// subscriptions that no longer exist and return their file names. Nothing
// is deleted unless the subscription list was read. A subscription id that
// is not a string fails like Python (AttributeError of str.replace); the
// startup caller logs it.
func PruneOrphanCaches(ctx context.Context) ([]string, error) {
	cacheDir, names, err := func() (string, []string, error) {
		r, err := repo.GetRepository(ctx)
		if err != nil {
			return "", nil, err
		}
		shared, err := getShared(ctx, r)
		if err != nil {
			return "", nil, err
		}
		keep := map[string]bool{}
		value, present := shared.Get("subscriptions")
		if !present {
			return "", nil, pyErr("KeyError", "'subscriptions'")
		}
		items, err := iterate(value)
		if err != nil {
			return "", nil, err
		}
		for _, item := range items {
			object, ok := item.(*jsonx.Object)
			if !ok {
				return "", nil, notSubscriptable(item)
			}
			id, present := object.Get("id")
			if !present {
				return "", nil, pyErr("KeyError", "'id'")
			}
			text, ok := id.(string)
			if !ok {
				return "", nil, attributeError(id, "replace")
			}
			keep[cacheName(text)] = true
		}
		cacheDir, err := ensureCacheDir(ctx)
		if err != nil {
			return "", nil, err
		}
		entries, err := os.ReadDir(cacheDir)
		if err != nil {
			return "", nil, err
		}
		var names []string
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".json") && !keep[entry.Name()] {
				names = append(names, entry.Name())
			}
		}
		return cacheDir, names, nil
	}()
	if err != nil {
		var py *PyError
		if errors.As(err, &py) && py.Class == "AttributeError" {
			return nil, err
		}
		if !isOSError(err) && !errors.Is(err, repo.ErrRepository) && !(errors.As(err, &py) &&
			(py.Class == "KeyError" || py.Class == "TypeError")) {
			return nil, err
		}
		text := err.Error()
		if isOSError(err) {
			text = osErrorText(err)
		}
		cacheLog.Warning("检查订阅缓存失败，跳过清理: " + text)
		return []string{}, nil
	}
	removed := []string{}
	for _, name := range names {
		ok, err := removeCacheFile(pyJoin(cacheDir, name))
		if err != nil {
			cacheLog.Warning(fmt.Sprintf("删除孤立订阅缓存失败 (%s): %s", name, osErrorText(err)))
			continue
		}
		if ok {
			removed = append(removed, name)
		}
	}
	if len(removed) > 0 {
		cacheLog.Info(fmt.Sprintf("已清理 %d 个已删除订阅的节点缓存", len(removed)))
	}
	return removed, nil
}

// notSubscriptable is the TypeError of value['id'] on a non-mapping.
func notSubscriptable(value any) error {
	switch value.(type) {
	case string:
		return pyErr("TypeError", "string indices must be integers, not 'str'")
	case []any:
		return pyErr("TypeError", "list indices must be integers or slices, not str")
	}
	return pyErr("TypeError", fmt.Sprintf("'%s' object is not subscriptable", typeName(value)))
}

// getShared is repository.get_shared() (tests replace it).
var getShared = func(ctx context.Context, r *repo.Repository) (*jsonx.Object, error) { return r.GetShared(ctx) }

// iterate is Python iteration over a JSON value for the subscription list:
// a list, the keys of a mapping, the characters of a string; None and
// numbers are a TypeError.
func iterate(value any) ([]any, error) {
	switch v := value.(type) {
	case []any:
		return v, nil
	case *jsonx.Object:
		keys := make([]any, 0, v.Len())
		for key := range v.All() {
			keys = append(keys, key)
		}
		return keys, nil
	case string:
		items := []any{}
		for _, r := range v {
			items = append(items, string(r))
		}
		return items, nil
	}
	return nil, pyErr("TypeError", fmt.Sprintf("'%s' object is not iterable", typeName(value)))
}

// SaveSubscriptionNodes is save_subscription_nodes(sub_id, nodes, metadata):
// write the shared node cache and return the payload. A write failure of
// the file system is logged and the payload returned anyway; repository
// errors are returned.
func SaveSubscriptionNodes(ctx context.Context, subID string, nodes any, metadata *jsonx.Object) (*jsonx.Object, error) {
	cachePath, err := CachePath(ctx, subID)
	if err != nil {
		return nil, err
	}
	count := 0
	if list, ok := nodes.([]any); ok {
		count = len(list)
	}
	now := cacheClock.Now()
	payload := jsonx.Obj("subscription_id", subID,
		"updated_at", strings.Replace(isoFormat(now), "+00:00", "Z", 1),
		"count", jsonx.Int(count), "nodes", nodes)
	if metadata != nil && metadata.Len() > 0 {
		payload.Set("metadata", metadata)
		if fetched := metadata.Value("fetched_at"); jsonx.Truthy(fetched) {
			payload.Set("updated_at", fetched)
		}
	}
	previous, err := LoadSubscriptionCache(ctx, subID)
	if err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := r.WriteSharedJSON(ctx, "subscribes/"+filepath.Base(cachePath), payload); err != nil {
		if !isOSError(err) {
			return nil, err
		}
		cacheLog.Error(fmt.Sprintf("写入订阅缓存失败 (%s): %s", subID, osErrorText(err)))
		return payload, nil
	}
	if previous != nil && !jsonx.Equal(previous.Value("count"), payload.Value("count")) {
		cacheLog.Info(fmt.Sprintf("订阅 %s 节点数变化：%s → %s", logx.LogText(subscriptionLabel(metadata, subID), 120),
			jsonx.Str(previous.Value("count")), jsonx.Str(payload.Value("count"))))
	}
	noteHostsChange(ctx, subID, previous, payload)
	return payload, nil
}

func subscriptionLabel(metadata *jsonx.Object, subID string) any {
	if name := metadata.Value("subscription_name"); jsonx.Truthy(name) {
		return name
	}
	return subID
}

// objectOr is `value or {}` for a value read with .get().
func objectOr(value any) *jsonx.Object {
	if object, ok := value.(*jsonx.Object); ok && object != nil {
		return object
	}
	return jsonx.NewObject()
}

// noteHostsChange is _note_hosts_change: hosts live in the main Mihomo
// file, which only a publication replaces; a change is logged and the Agent
// content checks are invalidated.
func noteHostsChange(ctx context.Context, subID string, previous, payload *jsonx.Object) {
	oldHosts, _ := objectOr(objectOr(previous).Value("metadata")).Value("hosts").(*jsonx.Object)
	newHosts, _ := objectOr(payload.Value("metadata")).Value("hosts").(*jsonx.Object)
	if subs.HostsDigest(oldHosts) == subs.HostsDigest(newHosts) {
		return
	}
	change := subs.DescribeChange(oldHosts, newHosts)
	cacheLog.Info(fmt.Sprintf("订阅 %s 的 hosts 有变化：新增 %d 条、删除 %d 条、修改 %d 条；重新发布 Mihomo 配置后生效",
		logx.LogText(subscriptionLabel(objectOr(payload.Value("metadata")), subID), 120),
		change.GetInt("added"), change.GetInt("removed"), change.GetInt("changed")))
	invalidate := contentSync.Load()
	if invalidate == nil {
		return
	}
	func() {
		defer func() {
			if p := recover(); p != nil {
				cacheLog.Debug(fmt.Sprintf("作废 Agent 内容核对缓存失败\n%v", p))
			}
		}()
		if err := (*invalidate)(ctx); err != nil {
			cacheLog.Debug("作废 Agent 内容核对缓存失败\n" + err.Error())
		}
	}()
}

// LoadSubscriptionCache is load_subscription_cache: the shared raw cache,
// nil (None) when it does not exist or cannot be read (logged). Old caches
// get a count. Errors are what Python raises outside its try block
// (opening the repository, resolving the cache directory).
func LoadSubscriptionCache(ctx context.Context, subID string) (*jsonx.Object, error) {
	cachePath, err := CachePath(ctx, subID)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(cachePath); err != nil {
		return nil, nil
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	data, err := r.ReadSharedJSON(ctx, "subscribes/"+filepath.Base(cachePath))
	if err != nil {
		text := err.Error()
		if isOSError(err) {
			text = osErrorText(err)
		} else if !errors.Is(err, repo.ErrRepository) {
			return nil, err
		}
		cacheLog.Warning(fmt.Sprintf("读取订阅缓存失败 (%s): %s", subID, text))
		return nil, nil
	}
	if nodes, ok := data.Value("nodes").([]any); ok && !data.Has("count") {
		data.Set("count", jsonx.Int(len(nodes)))
	}
	return data, nil
}

// cacheClock dates the caches (tests replace it).
var cacheClock Clock = SystemClock
