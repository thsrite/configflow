package substore

// Port of backend/test_subscription_cache_cleanup.py (cache level; the
// DELETE /api/subscriptions route halves wait for C30) and of
// test_subscription_hosts.py::test_hosts_change_invalidates_agent_content_checks_once.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func testCacheNode(t *testing.T) model.Node {
	t.Helper()
	var n model.Node
	raw := `{"id":"n","name":"HK","type":"ss","server":"192.0.2.1","port":443,"cipher":"aes-128-gcm","password":"secret"}`
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func sharedRepo(t *testing.T) (context.Context, *repo.Repository) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	enabled := true
	err = r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		shared.Subscriptions = []model.Subscription{
			{ID: "keep", Name: "Keep", URL: "https://example.test/keep", Enabled: &enabled},
			{ID: "drop", Name: "Drop", URL: "https://example.test/drop", Enabled: &enabled},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"keep", "drop"} {
		must[*Cache](t)(SaveCache(ctx, r, id, []model.Node{testCacheNode(t)}, nil))
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
	if !must[bool](t)(DeleteSubscriptionCache(ctx, r, "drop")) {
		t.Fatal("nothing removed")
	}
	if must[*Cache](t)(LoadCache(ctx, r, "drop")) != nil {
		t.Fatal("cache still readable")
	}
	if got := strings.Join(cacheFiles(t, r), ","); got != "keep.json" {
		t.Fatalf("files %s", got)
	}
	kept := must[*Cache](t)(LoadCache(ctx, r, "keep"))
	if !testjson.Equal([]model.Node{testCacheNode(t)}, kept.Nodes) {
		t.Fatal("kept cache changed")
	}
	if must[bool](t)(DeleteSubscriptionCache(ctx, r, "drop")) {
		t.Fatal("removed twice")
	}
}

// test_subscription_cache_cleanup.py::test_startup_prunes_only_caches_without_a_subscription
func TestStartupPrunesOnlyCachesWithoutASubscription(t *testing.T) {
	ctx, r := sharedRepo(t)
	must[*Cache](t)(SaveCache(ctx, r, "removed/earlier", []model.Node{testCacheNode(t)}, nil)) // path-safe name
	err := r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		var kept []model.Subscription
		for _, s := range shared.Subscriptions {
			if s.ID != "drop" {
				kept = append(kept, s)
			}
		}
		shared.Subscriptions = kept
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(must[string](t)(r.SharedCacheDir()), "notes.txt"), []byte("unrelated"), 0o644)
	removed := must[[]string](t)(PruneOrphanCaches(ctx, r))
	sort.Strings(removed)
	if strings.Join(removed, ",") != "drop.json,removed_earlier.json" {
		t.Fatalf("removed %v", removed)
	}
	if got := strings.Join(cacheFiles(t, r), ","); got != "keep.json,notes.txt" {
		t.Fatalf("files %s", got)
	}
	if again := must[[]string](t)(PruneOrphanCaches(ctx, r)); len(again) != 0 {
		t.Fatalf("second prune %v", again)
	}
}

// test_subscription_cache_cleanup.py::test_pruning_deletes_nothing_when_subscriptions_cannot_be_read
func TestPruningDeletesNothingWhenSubscriptionsCannotBeRead(t *testing.T) {
	ctx, r := sharedRepo(t)
	logs := captureLogs(t)
	previous := getShared
	getShared = func(context.Context, *repo.Repository) (*model.Shared, []model.FieldIssue, error) {
		return nil, nil, repo.NewValidationError("unreadable")
	}
	defer func() { getShared = previous }()
	if removed := must[[]string](t)(PruneOrphanCaches(ctx, r)); len(removed) != 0 {
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
			// The stored row's load issues, as the repository reports them.
			getShared = func(context.Context, *repo.Repository) (*model.Shared, []model.FieldIssue, error) {
				var shared model.Shared
				var rows map[string]json.RawMessage
				if err := json.Unmarshal([]byte(raw), &rows); err != nil {
					return nil, nil, err
				}
				var issues []model.FieldIssue
				for field, value := range rows {
					_, found, err := model.DecodeStoredMember(&shared, field, value, "shared."+field)
					if err != nil {
						return nil, nil, err
					}
					issues = append(issues, found...)
				}
				return &shared, issues, nil
			}
			defer func() { getShared = previous }()
			logs := captureLogs(t)
			removed, err := PruneOrphanCaches(ctx, r)
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
	payload := must[*Cache](t)(SaveCache(ctx, r, "keep", []model.Node{testCacheNode(t)},
		&CacheMetadata{SubscriptionName: "机场", URL: new("https://example.test/keep")}))
	data, err := os.ReadFile(filepath.Join(must[string](t)(r.SharedCacheDir()), "keep.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"subscription_id\": \"keep\",\n  \"updated_at\": \"2026-11-15T08:00:00Z\",\n  \"count\": 1,\n  \"nodes\": [\n    {\n" +
		"      \"id\": \"n\",\n      \"name\": \"HK\",\n      \"type\": \"ss\",\n      \"server\": \"192.0.2.1\",\n      \"port\": 443,\n" +
		"      \"cipher\": \"aes-128-gcm\",\n      \"password\": \"secret\"\n    }\n  ],\n  \"metadata\": {\n" +
		"    \"subscription_name\": \"机场\",\n    \"url\": \"https://example.test/keep\",\n    \"fetched_at\": null\n  }\n}\n"
	if string(data) != want {
		t.Fatalf("file:\n%s", data)
	}
	if payload.UpdatedAt != "2026-11-15T08:00:00Z" {
		t.Fatal("payload time")
	}
	// fetched_at replaces updated_at; an empty nodes list has count 0.
	payload = must[*Cache](t)(SaveCache(ctx, r, "keep", nil, &CacheMetadata{FetchedAt: new("2026-01-01T00:00:00.5+00:00")}))
	if payload.UpdatedAt != "2026-01-01T00:00:00.5+00:00" || payload.Count == nil || int64(*payload.Count) != 0 {
		t.Fatalf("payload %+v", payload)
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
	if got := must[*Cache](t)(LoadCache(ctx, r, "old")); got == nil || got.Count == nil || int64(*got.Count) != 2 {
		t.Fatalf("old %+v", got)
	}
	logs := captureLogs(t)
	os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{`), 0o644)
	if must[*Cache](t)(LoadCache(ctx, r, "broken")) != nil {
		t.Fatal("broken cache read")
	}
	if !strings.Contains(logs.text(), "读取订阅缓存失败 (broken): decode stored JSON:") {
		t.Fatalf("log:\n%s", logs.text())
	}
	os.WriteFile(filepath.Join(dir, "list.json"), []byte(`[1]`), 0o644)
	if must[*Cache](t)(LoadCache(ctx, r, "list")) != nil {
		t.Fatal("list cache read")
	}
}

// test_subscription_hosts.py::test_hosts_change_invalidates_agent_content_checks_once
func TestHostsChangeInvalidatesAgentContentChecksOnce(t *testing.T) {
	ctx, r := sharedRepo(t)
	svc := NewService(NewEnvFor(r))
	calls := 0
	t.Cleanup(svc.OnContentChange(func(context.Context) error { calls++; return nil }))
	airport := "proxies:\n  - {name: HK 01, type: socks5, server: jp-node.example.net, port: 1080}\n" +
		"hosts:\n  jp-node.example.net: jp-entry.example.org\n  google.com: 203.0.113.66\n"
	store := func(text string) {
		t.Helper()
		metadata := &CacheMetadata{SubscriptionName: "机场", Hosts: subs.ExtractHosts(text)}
		proxies := must[[]*yamlx.Node](t)(ParseProxyNodes(text))
		nodes := must[[]model.Node](t)(ProxyNodes(proxies))
		must[*Cache](t)(svc.SaveCache(ctx, r, "air", nodes, metadata))
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
	ctx, r := sharedRepo(t)
	logs := captureLogs(t)
	must[*Cache](t)(SaveCache(ctx, r, "keep", []model.Node{testCacheNode(t), testCacheNode(t)}, &CacheMetadata{SubscriptionName: "Keep\nX"}))
	if !strings.Contains(logs.text(), "订阅 Keep?X 节点数变化：1 → 2") {
		t.Fatalf("log:\n%s", logs.text())
	}
}

// O10 cache budget: LoadSubscriptionCacheShared matches LoadCacheJSON without
// populating the encoded JSON cache field.
func TestLoadSubscriptionCacheSharedMatchesJSON(t *testing.T) {
	ctx, r := sharedRepo(t)
	url := "https://feed.example.com/sub"
	airport := "proxies:\n  - {name: HK 01, type: socks5, server: jp-node.example.net, port: 1080}\n" +
		"hosts:\n  jp-node.example.net: jp-entry.example.org\n"
	metadata := &CacheMetadata{
		SubscriptionName: "Air",
		URL:              &url,
		Hosts:            subs.ExtractHosts(airport),
	}
	proxies := must[[]*yamlx.Node](t)(ParseProxyNodes(airport))
	nodes := must[[]model.Node](t)(ProxyNodes(proxies))
	must[*Cache](t)(SaveCache(ctx, r, "shared-view", nodes, metadata))

	path := must[string](t)(CachePath(ctx, r, "shared-view"))
	memoForget(path)

	shared := must[*subs.SubscriptionCache](t)(LoadSubscriptionCacheShared(ctx, r, "shared-view"))
	if shared == nil || len(shared.Nodes) != 1 || shared.Metadata == nil || shared.Metadata.URL != url || shared.Metadata.Hosts == nil {
		t.Fatalf("unexpected shared cache: %+v", shared)
	}
	key, ok := statCacheFile(path)
	if !ok {
		t.Fatal("cache file missing")
	}
	if entry := memoLookup(path, key); entry.encoded != nil {
		t.Fatal("LoadSubscriptionCacheShared unexpectedly populated encoded JSON bytes")
	}
	raw := must[json.RawMessage](t)(LoadCacheJSON(ctx, r, "shared-view"))
	var decoded subs.SubscriptionCache
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Nodes) != len(shared.Nodes) || decoded.Nodes[0].Name != shared.Nodes[0].Name ||
		decoded.Metadata.URL != shared.Metadata.URL || decoded.Metadata.DialersStored != shared.Metadata.DialersStored ||
		decoded.Metadata.Hosts.Entries.Len() != shared.Metadata.Hosts.Entries.Len() {
		t.Fatalf("shared %+v != decoded %+v", shared, decoded)
	}
}
