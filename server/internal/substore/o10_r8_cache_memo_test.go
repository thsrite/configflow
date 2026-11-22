package substore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func manyCacheNodes(t *testing.T, n int, tag string) []model.Node {
	t.Helper()
	var b strings.Builder
	b.WriteString("proxies:\n")
	for i := range n {
		fmt.Fprintf(&b, "  - {name: %s-%03d, type: ss, server: node%03d.example.com, port: %d, cipher: aes-128-gcm, password: synthetic}\n", tag, i, i, 10000+i)
	}
	proxies := must[[]*yamlx.Node](t)(ParseProxyNodes(b.String()))
	return must[[]model.Node](t)(ProxyNodes(proxies))
}

// O10 R8: a node cache is decoded once per file version; every reader still
// gets its own record, and a rewritten or removed file is seen at once.
func TestO10CacheDecodeReusedWhileFileUnchanged(t *testing.T) {
	ctx, r := sharedRepo(t)
	must[*Cache](t)(SaveCache(ctx, "keep", manyCacheNodes(t, 200, "a"), &CacheMetadata{URL: "https://example.test/keep", FetchedAt: "2026-01-01T00:00:00Z"}))

	first := must[*Cache](t)(LoadCache(ctx, "keep"))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	second := must[*Cache](t)(LoadCache(ctx, "keep"))
	runtime.ReadMemStats(&after)
	if len(second.Nodes) != 200 {
		t.Fatalf("nodes = %d", len(second.Nodes))
	}
	// A fresh decode of 200 nodes allocates several MB; the copy a few
	// hundred KB at most.
	if got := after.TotalAlloc - before.TotalAlloc; got > 512<<10 {
		t.Fatalf("second load allocated %d bytes; decode not reused", got)
	}

	// Copies are independent of each other and of the kept decode.
	first.Nodes[0].Name = "changed"
	first.Nodes = append(first.Nodes, model.Node{Name: "extra"})
	first.Metadata.URL = "https://changed.example.test"
	*first.Count = 1
	third := must[*Cache](t)(LoadCache(ctx, "keep"))
	if third.Nodes[0].Name != "a-000" || len(third.Nodes) != 200 || third.Metadata.URL != "https://example.test/keep" || *third.Count != 200 {
		t.Fatalf("kept decode modified through a copy: %q %d %q %d", third.Nodes[0].Name, len(third.Nodes), third.Metadata.URL, *third.Count)
	}
	shared := must[*Cache](t)(LoadCacheShared(ctx, "keep"))
	if again := must[*Cache](t)(LoadCacheShared(ctx, "keep")); again != shared {
		t.Fatal("shared record not stable while the file is unchanged")
	}
	encoded := must[json.RawMessage](t)(LoadCacheJSON(ctx, "keep"))
	direct := must[[]byte](t)(model.Marshal(third))
	if !bytes.Equal(encoded, direct) {
		t.Fatalf("encoded cache differs from the record's encoding")
	}
	summary := must[*CacheSummary](t)(LoadCacheSummary(ctx, "keep"))
	if !summary.HasNodes || summary.URL == nil || *summary.URL != "https://example.test/keep" {
		t.Fatalf("summary: %+v", summary)
	}

	// A rewrite (same node count, so the same size is likely) is seen at once.
	must[*Cache](t)(SaveCache(ctx, "keep", manyCacheNodes(t, 200, "b"), &CacheMetadata{URL: "https://example.test/moved", FetchedAt: "2026-01-01T00:00:00Z"}))
	if got := must[*Cache](t)(LoadCache(ctx, "keep")); got.Nodes[0].Name != "b-000" || got.Metadata.URL != "https://example.test/moved" {
		t.Fatalf("rewrite not seen: %q %q", got.Nodes[0].Name, got.Metadata.URL)
	}
	if got := must[*Cache](t)(LoadCacheShared(ctx, "keep")); got == shared || got.Nodes[0].Name != "b-000" {
		t.Fatal("shared record not replaced after a rewrite")
	}
	if got := must[*CacheSummary](t)(LoadCacheSummary(ctx, "keep")); got.URL == nil || *got.URL != "https://example.test/moved" {
		t.Fatalf("summary not refreshed: %+v", got)
	}
	if got := must[json.RawMessage](t)(LoadCacheJSON(ctx, "keep")); !bytes.Contains(got, []byte("b-000")) {
		t.Fatal("encoded cache not refreshed")
	}

	// A malformed file is never kept: it is reported and ignored every time.
	if _, err := r.WriteSharedText(ctx, filepath.Join("subscribes", cacheName("keep")), []byte("[1]")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if got := must[*Cache](t)(LoadCache(ctx, "keep")); got != nil {
			t.Fatal("malformed cache returned")
		}
		if got := must[*CacheSummary](t)(LoadCacheSummary(ctx, "keep")); got != nil {
			t.Fatal("malformed summary returned")
		}
	}

	must[*Cache](t)(SaveCache(ctx, "keep", manyCacheNodes(t, 3, "c"), nil))
	if got := must[*Cache](t)(LoadCache(ctx, "keep")); len(got.Nodes) != 3 {
		t.Fatal("valid file after a malformed one not read")
	}
	if removed := must[bool](t)(DeleteSubscriptionCache(ctx, "keep")); !removed {
		t.Fatal("not removed")
	}
	if got := must[*Cache](t)(LoadCache(ctx, "keep")); got != nil {
		t.Fatal("removed cache still returned")
	}
	if got := must[json.RawMessage](t)(LoadCacheJSON(ctx, "keep")); got != nil {
		t.Fatal("removed cache still encoded")
	}
}
