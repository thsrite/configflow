package refresh

// Ports of backend/test_flow_ui_apis.py (test_parse_userinfo,
// test_fetch_userinfo_skips_non_http, the subscription_health half of
// test_subscription_health_history_and_derived_fields) and of
// backend/test_subscription_hosts.py
// (test_only_downloaded_bodies_carry_hosts_into_the_cache_metadata,
// test_scheduled_download_stores_hosts_next_to_nodes).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/substore"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func TestParseUserinfo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		header string
		want   any
	}{
		{"upload=1024; download=2048; total=10737418240; expire=1798732800",
			map[string]any{"upload": 1024, "download": 2048, "total": 10737418240, "expire": 1798732800}},
		{"upload=1; total=5", map[string]any{"upload": 1, "total": 5}},
		{"", nil},
		{"garbage", nil},
	}
	for _, c := range cases {
		got, err := parseTraffic(c.header)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if got != nil {
			value = got
		}
		if !testjson.Equal(value, c.want) {
			t.Errorf("%q: %s", c.header, compactJSON(value))
		}
	}
}

type fakeHTTP struct {
	requests []*substore.Request
	reply    func(*substore.Request) (*substore.Response, error)
}

func (f *fakeHTTP) Do(_ context.Context, req *substore.Request) (*substore.Response, error) {
	f.requests = append(f.requests, req)
	return f.reply(req)
}

func TestFetchUserinfoSkipsNonHTTP(t *testing.T) {
	// Sequential: replaces the process-wide HTTP Env.
	client := &fakeHTTP{reply: func(*substore.Request) (*substore.Response, error) {
		t.Error("unexpected request")
		return nil, errors.New("no")
	}}
	env := DefaultRefreshEnv()
	env.HTTP = client
	t.Cleanup(SetRefreshEnv(env))
	for _, url := range []string{"file:///etc/passwd", "", "ftp://example.test/sub"} {
		if got, err := FetchTraffic(context.Background(), url); got != nil || err != nil {
			t.Errorf("%q: %v %v", url, got, err)
		}
	}
}

// fetch_userinfo reads the header with the Clash user agent; HTTP errors
// and request failures are nil.
func TestFetchUserinfoReadsTheHeader(t *testing.T) {
	// Sequential: replaces the process-wide HTTP Env.
	status := 200
	var failure error
	client := &fakeHTTP{reply: func(req *substore.Request) (*substore.Response, error) {
		if failure != nil {
			return nil, failure
		}
		return &substore.Response{StatusCode: status, Header: http.Header{"Subscription-Userinfo": {"upload=1; download=2; total=3; expire=4"}}}, nil
	}}
	env := DefaultRefreshEnv()
	env.HTTP = client
	t.Cleanup(SetRefreshEnv(env))
	got, err := FetchTraffic(context.Background(), "HTTPS://airport.test/sub")
	if err != nil || !testjson.Equal(got, map[string]any{"upload": 1, "download": 2, "total": 3, "expire": 4}) {
		t.Fatal(got, err)
	}
	req := client.requests[0]
	if req.Header.Get("User-Agent") != UserinfoUserAgent || req.Timeout != substore.Seconds(UserinfoTimeout) || req.URL != "HTTPS://airport.test/sub" {
		t.Fatal(req)
	}
	status = 404
	if got, err := FetchTraffic(context.Background(), "https://airport.test/sub"); got != nil || err != nil {
		t.Fatal(got, err)
	}
	failure = &substore.RequestError{Class: "ConnectTimeout", Message: "timeout"}
	if got, err := FetchTraffic(context.Background(), "https://airport.test/sub"); got != nil || err != nil {
		t.Fatal(got, err)
	}
}

// test_flow_ui_apis.py::test_subscription_health_history_and_derived_fields
// (record_fetch / load_health / forget; the routes are C30).
func TestSubscriptionHealthHistory(t *testing.T) {
	f := setup(t, 86400)
	for i := range HistorySize + 3 {
		if _, err := RecordHealth(f.ctx, "s1", FetchEventInput{OK: true, Count: model.Int(i)}); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = RecordHealth(f.ctx, "s1", FetchEventInput{OK: false, FromCache: true, Count: 9})
	traffic := testjson.Decode[Traffic](t, map[string]any{"upload": 1, "download": 2, "total": 100, "expire": 2000000000})
	entry, _ := RecordHealth(f.ctx, "s1", FetchEventInput{OK: true, Count: 10, Traffic: &traffic})
	health, _ := ReadHealth(f.ctx)
	item := health.Entry("s1")
	if item == nil || len(item.History) != HistorySize ||
		item.History[len(item.History)-2].Status != "cache" ||
		item.History[len(item.History)-1].Status != "ok" ||
		item.Traffic == nil || item.Traffic.Total == nil || item.Traffic.Total.String() != "100" {
		t.Fatalf("unexpected item: %s", compactJSON(item))
	}
	if item.History[0].Count == nil || int64(*item.History[0].Count) != 5 || !testjson.Equal(entry, item) {
		t.Fatalf("unexpected history[0]: %s", compactJSON(item.History[0]))
	}
	if empty, _ := RecordHealth(f.ctx, "", FetchEventInput{OK: true}); empty != nil && len(empty.History) != 0 {
		t.Fatal(empty)
	}
	if err := ForgetHealth(f.ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	health, _ = ReadHealth(f.ctx)
	if _, exists := health["s1"]; exists {
		t.Fatalf("entry still present: %s", compactJSON(health))
	}
}

// load_health treats a missing, damaged or non-object file as {}; a
// hand-edited history that is a string or an object is list()-ed.
func TestHealthFileEdges(t *testing.T) {
	f := setup(t, 86400)
	path, _ := f.repo.SharedPath(HealthFile)
	for _, text := range []string{"[1, 2]", "{broken", "\xff"} {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, []byte(text), 0o644)
		if health, err := ReadHealth(f.ctx); err != nil || len(health) != 0 {
			t.Fatal(text, health, err)
		}
	}
	// R5: the real-Python archive remains unchanged; the independent fixture
	// runs these same writes on typed history/count projections.
	refs := loadNativeReferences(t)
	edge := mapObj(refs, "edge")
	_ = os.WriteFile(path, []byte(mapStr(mapObj(edge, "files"), "health")), 0o644)
	env := DefaultRefreshEnv()
	env.Clock = &fakeClock{now: corpusNow}
	t.Cleanup(SetRefreshEnv(env))
	edgeNative := mapList(refs, "edge_native")
	for i, item := range mapList(edge, "output") {
		call := item.(map[string]any)
		parsed, err := decodeAny([]byte(mapStr(call, "call")))
		if err != nil {
			t.Fatal(err)
		}
		args := parsed.([]any)
		options := args[1].(map[string]any)
		var count model.Int
		_ = decodeInto(options["count"], &count)
		_, err = RecordHealth(f.ctx, args[0].(string), FetchEventInput{
			OK:        options["ok"] == true,
			FromCache: options["from_cache"] == true,
			Count:     count,
		})
		if err != nil {
			t.Fatal(err)
		}
		want := edgeNative[i].(map[string]any)["output"]
		got := readHealth(t, f.repo)
		if !testjson.Equal(got, want) {
			t.Fatalf("edge %d: %s, want %s", i, compactJSON(got), compactJSON(want))
		}
	}
	_ = os.WriteFile(path, []byte(`{"s": null}`), 0o644)
	_ = ForgetHealth(f.ctx, "s")
	data, _ := os.ReadFile(path)
	if string(data) == "{}\n" || string(data) == "{}" {
		t.Fatal("a null entry is popped without saving")
	}
}

const airport = `mixed-port: 7890
proxies:
  - {name: HK 01, type: socks5, server: jp-node.example.net, port: 1080}
  - name: JP 01
    type: socks5
    server: HK-NODE.example.net.
    port: 1080
    # an indented key named hosts must not be read as the top-level section
    hosts: ignored.example
  - {name: SG 01, type: socks5, server: 203.0.113.9, port: 1080}
hosts:
  jp-node.example.net: jp-entry.example.org
  hk-node.example.net: [198.51.100.12, '2001:db8::12']
  google.com: 203.0.113.66
proxy-groups:
  - {name: PROXY, type: select, proxies: [HK 01, JP 01, SG 01]}
`

func airSub(t *testing.T) model.Subscription {
	t.Helper()
	return testjson.Decode[model.Subscription](t, map[string]any{
		"id": "air", "name": "机场", "url": "https://sub.test/air", "enabled": true,
	})
}

// test_subscription_hosts.py::test_only_downloaded_bodies_carry_hosts_into_the_cache_metadata
func TestOnlyDownloadedBodiesCarryHostsIntoTheCacheMetadata(t *testing.T) {
	// Sequential: missing fetched_at values use the process-wide Env clock.
	feed := airSub(t)
	metadata, err := CacheMetadata(context.Background(), feed, &SourceResult{Text: airport, Source: "direct_yaml", FetchedAt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	metaParsed, err := testjson.Parse(mustMarshalBytes(t, metadata))
	if err != nil {
		t.Fatal(err)
	}
	if total, _ := testjson.Path(metaParsed, "hosts", "total"); !testjson.Equal(total, 3) {
		t.Fatalf("unexpected metadata: %s", compactJSON(metadata))
	}
	if metadata, _ := CacheMetadata(context.Background(), feed, &SourceResult{Text: airport, Source: "cache"}); len(metadata.Hosts) != 0 {
		t.Fatalf("unexpected hosts on cache source: %s", compactJSON(metadata))
	}
	doc, _ := yamlx.Parse([]byte(airport))
	conversionBytes, _ := yamlx.Marshal(yamlx.Map(yamlx.Str("proxies"), yamlx.Get(doc, "proxies")))
	conversion := string(conversionBytes)
	if metadata, _ := CacheMetadata(context.Background(), feed, &SourceResult{Text: conversion, Source: "sub_store"}); len(metadata.Hosts) != 0 {
		t.Fatalf("unexpected hosts on sub_store source: %s", compactJSON(metadata))
	}
	rawMeta := string(mustMarshalBytes(t, metadata))
	i1 := strings.Index(rawMeta, `"subscription_name"`)
	i2 := strings.Index(rawMeta, `"url"`)
	i3 := strings.Index(rawMeta, `"fetched_at"`)
	if i1 < 0 || i2 <= i1 || i3 <= i2 {
		t.Fatalf("unexpected key order: %s", rawMeta)
	}
}

// test_subscription_hosts.py::test_scheduled_download_stores_hosts_next_to_nodes
func TestScheduledDownloadStoresHostsNextToNodes(t *testing.T) {
	f := setup(t, 86400)
	f.download(airport, "direct_yaml", nil)
	if _, _, _, err := DownloadNodes(f.ctx, airSub(t)); err != nil {
		t.Fatal(err)
	}
	cache, err := substore.LoadCache(f.ctx, f.repo, "air")
	if err != nil || cache == nil {
		t.Fatal(cache, err)
	}
	parsed, err := testjson.Parse(mustMarshalBytes(t, cache))
	if err != nil {
		t.Fatal(err)
	}
	google, _ := testjson.Path(parsed, "metadata", "hosts", "entries", "google.com")
	if cache.Count == nil || int64(*cache.Count) != 3 || !testjson.Equal(google, []any{"203.0.113.66"}) {
		t.Fatalf("unexpected cache: %s", compactJSON(cache))
	}
}

func mustMarshalBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
