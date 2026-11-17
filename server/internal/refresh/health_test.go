package refresh

// Ports of backend/test_flow_ui_apis.py (test_parse_userinfo,
// test_fetch_userinfo_skips_non_http, the subscription_health half of
// test_subscription_health_history_and_derived_fields) and of
// backend/test_subscription_hosts.py
// (test_only_downloaded_bodies_carry_hosts_into_the_cache_metadata,
// test_scheduled_download_stores_hosts_next_to_nodes).

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

func TestParseUserinfo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		header string
		want   any
	}{
		{"upload=1024; download=2048; total=10737418240; expire=1798732800",
			jsonx.Obj("upload", jsonx.Int(1024), "download", jsonx.Int(2048), "total", jsonx.Int(10737418240), "expire", jsonx.Int(1798732800))},
		{"upload=1; total=5", jsonx.Obj("upload", jsonx.Int(1), "total", jsonx.Int(5))},
		{"", nil},
		{"garbage", nil},
	}
	for _, c := range cases {
		got, err := ParseUserinfo(c.header)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if got != nil {
			value = got
		}
		if !jsonx.Equal(value, c.want) {
			t.Errorf("%q: %s", c.header, jsonx.Repr(value))
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
	t.Cleanup(SetEnv(&Env{Clock: substore.SystemClock, HTTP: client}))
	for _, url := range []any{"file:///etc/passwd", nil, 5} {
		if got, err := FetchUserinfo(context.Background(), url); got != nil || err != nil {
			t.Errorf("%v: %v %v", url, got, err)
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
	t.Cleanup(SetEnv(&Env{Clock: substore.SystemClock, HTTP: client}))
	got, err := FetchUserinfo(context.Background(), "HTTPS://airport.test/sub")
	if err != nil || !jsonx.Equal(got, jsonx.Obj("upload", jsonx.Int(1), "download", jsonx.Int(2), "total", jsonx.Int(3), "expire", jsonx.Int(4))) {
		t.Fatal(got, err)
	}
	req := client.requests[0]
	if req.Header.Get("User-Agent") != UserinfoUserAgent || req.Timeout != substore.Seconds(UserinfoTimeout) || req.URL != "HTTPS://airport.test/sub" {
		t.Fatal(req)
	}
	status = 404
	if got, err := FetchUserinfo(context.Background(), "https://airport.test/sub"); got != nil || err != nil {
		t.Fatal(got, err)
	}
	failure = &substore.RequestError{Class: "ConnectTimeout", Message: "timeout"}
	if got, err := FetchUserinfo(context.Background(), "https://airport.test/sub"); got != nil || err != nil {
		t.Fatal(got, err)
	}
}

// test_flow_ui_apis.py::test_subscription_health_history_and_derived_fields
// (record_fetch / load_health / forget; the routes are C30).
func TestSubscriptionHealthHistory(t *testing.T) {
	f := setup(t, jsonx.Int(86400))
	for i := range HistorySize + 3 {
		if _, err := RecordFetch(f.ctx, "s1", FetchRecord{OK: true, Count: jsonx.Int(i)}); err != nil {
			t.Fatal(err)
		}
	}
	RecordFetch(f.ctx, "s1", FetchRecord{OK: false, FromCache: true, Count: jsonx.Int(9)})
	entry, _ := RecordFetch(f.ctx, "s1", FetchRecord{OK: true, Count: jsonx.Int(10),
		Traffic: jsonx.Obj("upload", jsonx.Int(1), "download", jsonx.Int(2), "total", jsonx.Int(100), "expire", jsonx.Int(2000000000))})
	health, _ := LoadHealth(f.ctx)
	item := health.GetObject("s1")
	history := item.GetList("history")
	if len(history) != HistorySize || history[len(history)-2].(*jsonx.Object).Value("status") != "cache" ||
		history[len(history)-1].(*jsonx.Object).Value("status") != "ok" || item.GetObject("traffic").GetInt("total") != 100 {
		t.Fatal(jsonx.Repr(item))
	}
	if history[0].(*jsonx.Object).GetInt("count") != 5 || !jsonx.Equal(entry, item) {
		t.Fatal(jsonx.Repr(history[0]))
	}
	if entry, _ := RecordFetch(f.ctx, "", FetchRecord{OK: true}); entry.Len() != 0 {
		t.Fatal(entry)
	}
	if err := Forget(f.ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	health, _ = LoadHealth(f.ctx)
	if health.Has("s1") {
		t.Fatal(jsonx.Repr(health))
	}
}

// load_health treats a missing, damaged or non-object file as {}; a
// hand-edited history that is a string or an object is list()-ed.
func TestHealthFileEdges(t *testing.T) {
	f := setup(t, jsonx.Int(86400))
	path, _ := f.repo.SharedPath(HealthFile)
	for _, text := range []string{"[1, 2]", "{broken", "\xff"} {
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(text), 0o644)
		if health, err := LoadHealth(f.ctx); err != nil || health.Len() != 0 {
			t.Fatal(text, health, err)
		}
	}
	os.WriteFile(path, []byte(`{"s": {"history": "ab"}, "t": {"history": {"x": 1}}, "u": 5}`), 0o644)
	entry, err := RecordFetch(f.ctx, "s", FetchRecord{Count: "3"})
	if err != nil || !jsonx.Equal(entry.GetList("history")[:2], []any{"a", "b"}) ||
		entry.GetList("history")[2].(*jsonx.Object).GetInt("count") != 3 ||
		entry.GetList("history")[2].(*jsonx.Object).Value("status") != "fail" {
		t.Fatal(jsonx.Repr(entry), err)
	}
	entry, _ = RecordFetch(f.ctx, "t", FetchRecord{})
	if entry.GetList("history")[0] != "x" {
		t.Fatal(jsonx.Repr(entry))
	}
	entry, _ = RecordFetch(f.ctx, "u", FetchRecord{OK: true, FromCache: true})
	if entry.GetList("history")[0].(*jsonx.Object).Value("status") != "cache" {
		t.Fatal(jsonx.Repr(entry))
	}
	os.WriteFile(path, []byte(`{"s": {"history": 7}}`), 0o644)
	if _, err := RecordFetch(f.ctx, "s", FetchRecord{}); err == nil || err.Error() != "'int' object is not iterable" {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte(`{"s": null}`), 0o644)
	Forget(f.ctx, "s")
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

func airSub() *jsonx.Object {
	return jsonx.Obj("id", "air", "name", "机场", "url", "https://sub.test/air", "enabled", true)
}

// test_subscription_hosts.py::test_only_downloaded_bodies_carry_hosts_into_the_cache_metadata
func TestOnlyDownloadedBodiesCarryHostsIntoTheCacheMetadata(t *testing.T) {
	// Sequential: missing fetched_at values use the process-wide Env clock.
	feed := airSub()
	metadata, err := SourceCacheMetadata(feed, &substore.SubscriptionResult{Text: airport, Source: "direct_yaml", FetchedAt: "x"})
	if err != nil || metadata.GetObject("hosts").GetInt("total") != 3 {
		t.Fatal(jsonx.Repr(metadata), err)
	}
	if metadata, _ := SourceCacheMetadata(feed, &substore.SubscriptionResult{Text: airport, Source: "cache"}); metadata.Has("hosts") {
		t.Fatal(jsonx.Repr(metadata))
	}
	doc, _ := yamlx.Parse([]byte(airport))
	conversionBytes, _ := yamlx.Marshal(yamlx.Map(yamlx.Str("proxies"), yamlx.Get(doc, "proxies")))
	conversion := string(conversionBytes)
	if metadata, _ := SourceCacheMetadata(feed, &substore.SubscriptionResult{Text: conversion, Source: "sub_store"}); metadata.Has("hosts") {
		t.Fatal(jsonx.Repr(metadata))
	}
	if !slices.Equal(metadata.Keys()[:3], []string{"subscription_name", "url", "fetched_at"}) {
		t.Fatal(metadata.Keys())
	}
}

// test_subscription_hosts.py::test_scheduled_download_stores_hosts_next_to_nodes
func TestScheduledDownloadStoresHostsNextToNodes(t *testing.T) {
	f := setup(t, jsonx.Int(86400))
	f.download(airport, "direct_yaml", nil)
	if _, _, _, err := DownloadSubscriptionNodes(f.ctx, airSub()); err != nil {
		t.Fatal(err)
	}
	cache, _ := substore.LoadSubscriptionCache(f.ctx, "air")
	entries := cache.GetObject("metadata").GetObject("hosts").GetObject("entries")
	if cache.GetInt("count") != 3 || !jsonx.Equal(entries.Value("google.com"), []any{"203.0.113.66"}) {
		t.Fatal(jsonx.Repr(cache))
	}
}
