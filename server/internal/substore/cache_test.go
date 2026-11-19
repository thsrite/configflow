package substore

// Port of backend/test_subscription_cache_cleanup.py (cache level; the
// DELETE /api/subscriptions route halves wait for C30) and of
// test_subscription_hosts.py::test_hosts_change_invalidates_agent_content_checks_once.

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

var cacheNode = jsonx.Obj("id", "n", "name", "HK", "type", "ss", "server", "192.0.2.1", "port", jsonx.Int(443),
	"cipher", "aes-128-gcm", "password", "secret")

func sharedRepo(t *testing.T) (context.Context, *repo.Repository) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	_, err = r.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("subscriptions", []any{
			jsonx.Obj("id", "keep", "name", "Keep", "url", "https://example.test/keep", "enabled", true),
			jsonx.Obj("id", "drop", "name", "Drop", "url", "https://example.test/drop", "enabled", true)})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"keep", "drop"} {
		must[*jsonx.Object](t)(SaveSubscriptionNodes(ctx, id, []any{cacheNode.DeepCopy()}, nil))
	}
	return ctx, r
}

func cacheFiles(t *testing.T, r *repo.Repository) []string {
	dir := must[string](t)(r.SharedCacheDir())
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// test_subscription_cache_cleanup.py::test_deleting_a_subscription_removes_its_node_cache
// (the cache half of the DELETE route: delete_subscription_cache).
func TestDeletingASubscriptionRemovesItsNodeCache(t *testing.T) {
	ctx, r := sharedRepo(t)
	if !must[bool](t)(DeleteSubscriptionCache(ctx, "drop")) {
		t.Fatal("nothing removed")
	}
	if must[*jsonx.Object](t)(LoadSubscriptionCache(ctx, "drop")) != nil {
		t.Fatal("cache still readable")
	}
	if got := strings.Join(cacheFiles(t, r), ","); got != "keep.json" {
		t.Fatalf("files %s", got)
	}
	if !jsonx.Equal(must[*jsonx.Object](t)(LoadSubscriptionCache(ctx, "keep")).Value("nodes"), []any{cacheNode}) {
		t.Fatal("kept cache changed")
	}
	if must[bool](t)(DeleteSubscriptionCache(ctx, "drop")) {
		t.Fatal("removed twice")
	}
}

// test_subscription_cache_cleanup.py::test_startup_prunes_only_caches_without_a_subscription
func TestStartupPrunesOnlyCachesWithoutASubscription(t *testing.T) {
	ctx, r := sharedRepo(t)
	must[*jsonx.Object](t)(SaveSubscriptionNodes(ctx, "removed/earlier", []any{cacheNode.DeepCopy()}, nil)) // path-safe name
	_, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		var kept []any
		for _, s := range shared.GetList("subscriptions") {
			if s.(*jsonx.Object).GetString("id") != "drop" {
				kept = append(kept, s)
			}
		}
		shared.Set("subscriptions", kept)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(must[string](t)(r.SharedCacheDir()), "notes.txt"), []byte("unrelated"), 0o644)
	removed := must[[]string](t)(PruneOrphanCaches(ctx))
	sort.Strings(removed)
	if strings.Join(removed, ",") != "drop.json,removed_earlier.json" {
		t.Fatalf("removed %v", removed)
	}
	if got := strings.Join(cacheFiles(t, r), ","); got != "keep.json,notes.txt" {
		t.Fatalf("files %s", got)
	}
	if again := must[[]string](t)(PruneOrphanCaches(ctx)); len(again) != 0 {
		t.Fatalf("second prune %v", again)
	}
}

// test_subscription_cache_cleanup.py::test_pruning_deletes_nothing_when_subscriptions_cannot_be_read
func TestPruningDeletesNothingWhenSubscriptionsCannotBeRead(t *testing.T) {
	ctx, r := sharedRepo(t)
	logs := captureLogs(t)
	previous := getShared
	getShared = func(context.Context, *repo.Repository) (*model.Shared, error) {
		return nil, repo.NewValidationError("unreadable")
	}
	defer func() { getShared = previous }()
	if removed := must[[]string](t)(PruneOrphanCaches(ctx)); len(removed) != 0 {
		t.Fatalf("removed %v", removed)
	}
	files := strings.Join(cacheFiles(t, r), ",")
	if !strings.Contains(files, "drop.json") || !strings.Contains(files, "keep.json") {
		t.Fatalf("files %s", files)
	}
	if !strings.Contains(logs.text(), "检查订阅缓存失败，跳过清理: unreadable") {
		t.Fatalf("log:\n%s", logs.text())
	}
}

// Invalid model identities/list shapes must not prove that every cache is orphaned.
func TestPruneInvalidStoredIdentityKeepsFiles(t *testing.T) {
	for _, raw := range []string{`{"subscriptions":null}`, `{"subscriptions":"bad"}`, `{"subscriptions":["x"]}`, `{"subscriptions":[{"id":false}]}`, `{"subscriptions":[{"name":"x"}]}`} {
		t.Run(raw, func(t *testing.T) {
			ctx, r := sharedRepo(t)
			before := strings.Join(cacheFiles(t, r), ",")
			previous := getShared
			getShared = func(context.Context, *repo.Repository) (*model.Shared, error) {
				var shared model.Shared
				err := json.Unmarshal([]byte(raw), &shared)
				return &shared, err
			}
			defer func() { getShared = previous }()
			logs := captureLogs(t)
			removed, err := PruneOrphanCaches(ctx)
			if err != nil || len(removed) != 0 {
				t.Fatalf("removed %v err %v", removed, err)
			}
			if got := strings.Join(cacheFiles(t, r), ","); got != before {
				t.Fatalf("files changed: %s", got)
			}
			if !strings.Contains(logs.text(), "检查订阅缓存失败，跳过清理: ") {
				t.Fatalf("missing business prefix: %s", logs.text())
			}
		})
	}
}

// The cache file is the repository's shared JSON (indent 2, UTF-8, newline)
// with Python's key order and timestamp format.
func TestSavedCacheBytes(t *testing.T) {
	ctx, r := sharedRepo(t)
	previous := cacheClock
	cacheClock = fixedClock{time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)}
	defer func() { cacheClock = previous }()
	payload := must[*jsonx.Object](t)(SaveSubscriptionNodes(ctx, "keep", []any{cacheNode.DeepCopy()},
		jsonx.Obj("subscription_name", "机场", "url", "https://example.test/keep")))
	data, err := os.ReadFile(filepath.Join(must[string](t)(r.SharedCacheDir()), "keep.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"subscription_id\": \"keep\",\n  \"updated_at\": \"2026-11-15T08:00:00Z\",\n  \"count\": 1,\n  \"nodes\": [\n    {\n" +
		"      \"id\": \"n\",\n      \"name\": \"HK\",\n      \"type\": \"ss\",\n      \"server\": \"192.0.2.1\",\n      \"port\": 443,\n" +
		"      \"cipher\": \"aes-128-gcm\",\n      \"password\": \"secret\"\n    }\n  ],\n  \"metadata\": {\n" +
		"    \"subscription_name\": \"机场\",\n    \"url\": \"https://example.test/keep\"\n  }\n}\n"
	if string(data) != want {
		t.Fatalf("file:\n%s", data)
	}
	if payload.GetString("updated_at") != "2026-11-15T08:00:00Z" {
		t.Fatal("payload time")
	}
	// fetched_at replaces updated_at; a non-list count is 0.
	payload = must[*jsonx.Object](t)(SaveSubscriptionNodes(ctx, "keep", "x", jsonx.Obj("fetched_at", "2026-01-01T00:00:00.5+00:00")))
	if payload.GetString("updated_at") != "2026-01-01T00:00:00.5+00:00" || payload.GetInt("count") != 0 {
		t.Fatalf("payload %s", jsonx.Repr(payload))
	}
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time      { return c.t }
func (c fixedClock) Monotonic() float64  { return 0 }
func (c fixedClock) Sleep(time.Duration) {}

// Old caches without count get one; unreadable caches are None with a warning.
func TestLoadSubscriptionCacheLegacyAndBroken(t *testing.T) {
	ctx, r := sharedRepo(t)
	dir := must[string](t)(r.SharedCacheDir())
	os.WriteFile(filepath.Join(dir, "old.json"), []byte(`{"nodes": [1, 2]}`), 0o644)
	if got := must[*jsonx.Object](t)(LoadSubscriptionCache(ctx, "old")); got.GetInt("count") != 2 {
		t.Fatalf("old %s", jsonx.Repr(got))
	}
	logs := captureLogs(t)
	os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{`), 0o644)
	if must[*jsonx.Object](t)(LoadSubscriptionCache(ctx, "broken")) != nil {
		t.Fatal("broken cache read")
	}
	if !strings.Contains(logs.text(), "读取订阅缓存失败 (broken): decode stored JSON:") {
		t.Fatalf("log:\n%s", logs.text())
	}
	os.WriteFile(filepath.Join(dir, "list.json"), []byte(`[1]`), 0o644)
	if must[*jsonx.Object](t)(LoadSubscriptionCache(ctx, "list")) != nil {
		t.Fatal("list cache read")
	}
}

// test_subscription_hosts.py::test_hosts_change_invalidates_agent_content_checks_once
// (metadata as source_cache_metadata stores it: hosts extracted from the
// downloaded body).
func TestHostsChangeInvalidatesAgentContentChecksOnce(t *testing.T) {
	ctx, _ := sharedRepo(t)
	calls := 0
	SetContentSyncInvalidator(func(context.Context) error { calls++; return nil })
	defer SetContentSyncInvalidator(nil)
	airport := "proxies:\n  - {name: HK 01, type: socks5, server: jp-node.example.net, port: 1080}\n" +
		"hosts:\n  jp-node.example.net: jp-entry.example.org\n  google.com: 203.0.113.66\n"
	store := func(text string) {
		t.Helper()
		metadata := jsonx.Obj("subscription_name", "机场", "hosts", fixtureHosts(text))
		proxies := must[[]any](t)(ParseProxiesFromYAML(text))
		nodes := must[[]any](t)(ProxiesToNodes(proxies))
		must[*jsonx.Object](t)(SaveSubscriptionNodes(ctx, "air", nodes, metadata))
	}
	// C11's test_only_hosts_changes_invalidate_agent_content_checks: a node
	// address change without hosts does not invalidate the main config.
	noHosts := strings.Split(airport, "hosts:\n")[0]
	store(noHosts)
	store(strings.Replace(noHosts, "jp-node.example.net", "other.example.net", 1))
	if calls != 0 {
		t.Fatalf("node-only changes invalidated content: %d", calls)
	}
	store(airport)
	if calls != 1 { // none -> hosts
		t.Fatalf("calls %d", calls)
	}
	store(airport)
	if calls != 1 { // same entries, only nodes refreshed
		t.Fatalf("calls %d", calls)
	}
	store(strings.Replace(airport, "hosts:\n", "  - {name: HK 02, type: socks5, server: jp-node.example.net, port: 1081}\nhosts:\n", 1))
	if calls != 1 { // another node uses the same mapping
		t.Fatalf("new node on the same hosts mapping invalidated content: %d", calls)
	}
	logs := captureLogs(t)
	store(strings.Replace(airport, "203.0.113.66", "203.0.113.67", 1))
	if calls != 2 {
		t.Fatalf("calls %d", calls)
	}
	if !strings.Contains(logs.text(), "订阅 机场 的 hosts 有变化：新增 0 条、删除 0 条、修改 1 条；重新发布 Mihomo 配置后生效") {
		t.Fatalf("log:\n%s", logs.text())
	}
}

// Node count changes are logged with the subscription name.
func TestNodeCountChangeIsLogged(t *testing.T) {
	ctx, _ := sharedRepo(t)
	logs := captureLogs(t)
	must[*jsonx.Object](t)(SaveSubscriptionNodes(ctx, "keep", []any{cacheNode, cacheNode}, jsonx.Obj("subscription_name", "Keep\nX")))
	if !strings.Contains(logs.text(), "订阅 Keep?X 节点数变化：1 → 2") {
		t.Fatalf("log:\n%s", logs.text())
	}
}

// Archived JSON fixtures adapt only at this test boundary to the native owners.
// SaveSubscriptionNodes transcodes the old cache boundary once per batch.
func SaveSubscriptionNodes(ctx context.Context, id string, nodes any, metadata *jsonx.Object) (*jsonx.Object, error) {
	record, err := decodeLegacy[*Cache](jsonx.Obj("subscription_id", id, "updated_at", "", "count", jsonx.Int(0), "nodes", nodes, "metadata", metadata))
	if err != nil {
		return nil, err
	}
	var raw struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	data, err := json.Marshal(jsonx.Obj("nodes", nodes))
	if err != nil {
		return nil, err
	}
	if jsondecode.Unmarshal(data, &raw) == nil {
		count := model.Int(len(raw.Nodes))
		record.Count = &count
	}
	result, err := saveCache(ctx, id, record)
	if err != nil {
		return nil, err
	}
	return encodeLegacy(result)
}

// ParseProxiesFromYAML transcodes the complete native proxy batch once.
func ParseProxiesFromYAML(text string) ([]any, error) {
	values, err := ParseProxyNodes(text)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(values))
	for _, v := range values {
		data, err := yamlx.ToJSON(v)
		if err != nil {
			return nil, err
		}
		item, err := jsonx.Parse(data)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// ParseUserinfo adapts native subscription traffic for unported callers.
func ParseUserinfo(text string) (*jsonx.Object, error) {
	value, err := ReadUserinfo(text)
	if err != nil || value == nil {
		return nil, err
	}
	return encodeLegacy(value)
}

// ParseVersion adapts old dynamic input to the native version parser.
func ParseVersion(value any) []*big.Int {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	return VersionParts(text)
}

// IsNewer adapts old dynamic version inputs.
func IsNewer(candidate, current any) bool {
	a, ok := candidate.(string)
	if !ok {
		return false
	}
	b, _ := current.(string)
	return VersionNewer(a, b)
}

// FetchLatestRelease transcodes the selected release at the legacy response boundary.
func (d *Dependencies) FetchLatestRelease(ctx context.Context, force bool) (*jsonx.Object, string) {
	value, message := d.Latest(ctx, force)
	if value == nil {
		return nil, message
	}
	result, err := encodeLegacy(value)
	if err != nil {
		return nil, err.Error()
	}
	return result, message
}

// GetUpdateState transcodes the native observation for old HTTP owners.
func (d *Dependencies) GetUpdateState() *jsonx.Object {
	state := d.UpdateSnapshot()
	value, err := encodeLegacy(state)
	if err == nil && value != nil {
		value.Set("finished_at", jsonx.Float(state.FinishedAt))
	}
	return value
}

// GetSubStoreStatus transcodes one complete native snapshot.
func (d *Dependencies) GetSubStoreStatus(ctx context.Context, force bool) (*jsonx.Object, error) {
	value, err := d.Snapshot(ctx, force)
	if err != nil {
		return nil, err
	}
	return legacyDependencyStatus(value)
}

// Archived JSON fixtures adapt only at this test boundary to the native owners.
// read/write remain only for old test fixtures; native owners use state records.
func (r *Runtime) read() any {
	value, err := r.loadState()
	if err != nil {
		return nil
	}
	result, _ := encodeLegacy(value)
	return result
}

func (r *Runtime) write(pairs ...any) error {
	value := jsonx.Obj(pairs...)
	patch, err := decodeLegacy[runtimePatch](value)
	if err != nil {
		return err
	}
	return r.writeState(context.Background(), patch)
}

// JSON transcodes a native HTTP JSON payload for unported consumers.
func (r *Response) JSON() (any, error) {
	data, err := r.JSONData()
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}

func fixtureHosts(text string) *jsonx.Object {
	data, _ := json.Marshal(subs.ExtractHosts(text))
	value, _ := jsonx.Parse(data)
	object, _ := value.(*jsonx.Object)
	return object
}

// Archived JSON fixtures adapt only at this test boundary to the native owners.
// Status transcodes the lifecycle snapshot for unported response consumers.
func (r *Runtime) Status(ctx context.Context, running bool) (*jsonx.Object, error) {
	value, err := r.Snapshot(ctx, running)
	if err != nil {
		return nil, err
	}
	return encodeLegacy(value)
}
