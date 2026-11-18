package routers

import (
	"bytes"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
)

type noSyncEnvironment struct{ context.Context }

func (c noSyncEnvironment) Value(key any) any {
	if _, ok := key.(syncEnvironmentKey); ok {
		return nil
	}
	return c.Context.Value(key)
}

type syncAllocationFixture struct {
	ctx      context.Context
	config   json.RawMessage
	router   *Router
	download RuleSyncDownload
}

func syncAllocationInput(t testing.TB, n, padding int, remote, dropEnvironment bool) syncAllocationFixture {
	t.Helper()
	db := projectionRepository(t, 0, 0)
	// A legacy GetShared/JSON bridge in each cache miss would copy this unrelated
	// shared extension once per selected rule. The typed View path must not.
	err := db.UpdateShared(context.Background(), func(_ context.Context, s *model.Shared) error {
		raw, _ := json.Marshal(strings.Repeat("x", padding))
		return s.Fields.Set("unrelated", raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceURL := ""
	if remote {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil || r.Method != "GET" || len(body) != 0 {
				t.Errorf("unexpected raw download request: %s %q %v", r.Method, body, err)
			}
			_, _ = io.WriteString(w, "IP-CIDR,203.0.113.0/24")
		}))
		t.Cleanup(server.Close)
		sourceURL = server.URL
	}
	rules, library := []map[string]any{}, []map[string]any{}
	r := &Router{RouterSettings: RouterSettings{Type: "routeros", IPRuleSets: []string{}}}
	for i := 0; i < n; i++ {
		id, lib := fmt.Sprintf("r%d", i), fmt.Sprintf("lib%d", i)
		r.IPRuleSets = append(r.IPRuleSets, id)
		rules = append(rules, map[string]any{"id": id, "itemType": "ruleset", "library_rule_id": lib, "policy": "PROXY", "enabled": true})
		source := map[string]any{"id": lib, "name": lib, "source_type": "content", "content": "IP-CIDR,203.0.113.0/24", "behavior": "classical", "format": "text", "enabled": true}
		if remote {
			source["source_type"] = "url"
			source["url"] = sourceURL
		}
		library = append(library, source)
	}
	settings := map[string]any{}
	// The operation consumes settings once. Deliberately hiding the context
	// environment below must multiply the cost of preserving its opaque member.
	if remote {
		settings["unrelated"] = strings.Repeat("x", padding)
	}
	input := map[string]any{"rule_configs": rules, "rule_library": library, "system_config": settings, "mihomo": map[string]string{"custom_config": strings.Repeat("x", padding)}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), db)
	out := syncAllocationFixture{ctx: ctx, config: raw, router: r}
	if remote {
		downloader := &RuleSyncDownloads{Client: &rulefetch.Client{Transport: rulefetch.HTTPTransport{}}}
		out.download = func(ctx context.Context, rule, config json.RawMessage) (string, error) {
			if dropEnvironment {
				ctx = noSyncEnvironment{ctx}
			}
			return downloader.Download(ctx, rule, config)
		}
	}
	if _, records, err := loadSyncSources(out.ctx, out.config, out.router, out.download); err != nil || len(records) != n {
		t.Fatalf("pipeline fixture: records=%d err=%v", len(records), err)
	}
	return out
}
func measureSyncPipeline(t testing.TB, fixture syncAllocationFixture) testing.BenchmarkResult {
	t.Helper()
	return testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, records, err := loadSyncSources(fixture.ctx, fixture.config, fixture.router, fixture.download)
			if err != nil || len(records) != len(fixture.router.IPRuleSets) {
				b.Fatal("pipeline failed", err)
			}
		}
	})
}

func TestN89SyncPipelineAllocations(t *testing.T) {
	for _, n := range []int{1, 16} {
		small := measureSyncPipeline(t, syncAllocationInput(t, n, 0, false, false))
		large := measureSyncPipeline(t, syncAllocationInput(t, n, 1<<20, false, false))
		t.Logf("cache rules=%d padding=0/1MiB B/op=%d/%d allocs/op=%d/%d", n, small.AllocedBytesPerOp(), large.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocsPerOp())
		if delta := large.AllocedBytesPerOp() - small.AllocedBytesPerOp(); delta > 64<<10 {
			t.Fatalf("unrelated shared/config copied per source: %d", delta)
		}
	}
	const budget = 6 << 20
	nativeSmall := measureSyncPipeline(t, syncAllocationInput(t, 16, 0, true, false))
	nativeLarge := measureSyncPipeline(t, syncAllocationInput(t, 16, 256<<10, true, false))
	brokenSmall := measureSyncPipeline(t, syncAllocationInput(t, 16, 0, true, true))
	brokenLarge := measureSyncPipeline(t, syncAllocationInput(t, 16, 256<<10, true, true))
	nativeDelta := nativeLarge.AllocedBytesPerOp() - nativeSmall.AllocedBytesPerOp()
	brokenDelta := brokenLarge.AllocedBytesPerOp() - brokenSmall.AllocedBytesPerOp()
	t.Logf("download rules=16 native B/op=%d/%d delta=%d; environment-hidden B/op=%d/%d delta=%d", nativeSmall.AllocedBytesPerOp(), nativeLarge.AllocedBytesPerOp(), nativeDelta, brokenSmall.AllocedBytesPerOp(), brokenLarge.AllocedBytesPerOp(), brokenDelta)
	if nativeDelta > budget {
		t.Fatalf("native pipeline repeatedly decodes config: delta=%d", nativeDelta)
	}
	if brokenDelta <= budget {
		t.Fatalf("negative control did not exceed same budget: delta=%d", brokenDelta)
	}
}

func BenchmarkSyncSourcePipeline(b *testing.B) {
	for _, n := range []int{1, 16, 64} {
		for _, padding := range []int{0, 1 << 20} {
			b.Run(fmt.Sprintf("rules%d/padding%d", n, padding), func(b *testing.B) {
				f := syncAllocationInput(b, n, padding, false, false)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, _, err := loadSyncSources(f.ctx, f.config, f.router, f.download); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestN89SyncCachePeekDoesNotMigrate(t *testing.T) {
	db := projectionRepository(t, 0, 0)
	var source model.RuleLibraryItem
	if err := json.Unmarshal([]byte(`{"id":"lib","name":"Synthetic","source_type":"url","url":"https://example.com/ips","behavior":"classical","format":"text","enabled":true}`), &source); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateShared(t.Context(), func(_ context.Context, s *model.Shared) error {
		s.RuleLibrary = []model.RuleLibraryItem{source}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rule := rules.LibraryRule(source)
	oldName, ok := rules.LegacyCacheFilename(rule)
	if !ok {
		t.Fatal("legacy fixture not eligible")
	}
	root, err := db.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, oldName)
	nativePath := filepath.Join(root, rules.CacheFilename(rule))
	content := []byte("IP-CIDR,203.0.113.0/24")
	if err = os.MkdirAll(filepath.Dir(oldPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(oldPath, content, 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err = os.Chtimes(oldPath, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"rule_library": []model.RuleLibraryItem{source}, "rule_configs": []map[string]any{{"id": "r", "itemType": "ruleset", "library_rule_id": "lib", "policy": "PROXY"}}})
	r := &Router{RouterSettings: RouterSettings{Type: "routeros", IPRuleSets: []string{"r"}}}
	before, err := db.ConfigRevision(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(t.Context(), db)
	contents, records, err := loadSyncSources(ctx, config, r, nil)
	if err != nil || records["r"] == nil || contents["r"] != string(content) {
		t.Fatalf("legacy peek failed: %v %#v", err, records)
	}
	if _, err = os.Stat(nativePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read migrated cache: %v", err)
	}
	info, err := os.Stat(oldPath)
	if err != nil || !info.ModTime().Equal(stamp) {
		t.Fatal("read touched old mtime", err)
	}
	after, err := db.ConfigRevision(t.Context(), "default")
	if err != nil || before != after {
		t.Fatal("peek changed DB revision", before, after, err)
	}
	bytesAfter, err := os.ReadFile(oldPath)
	if err != nil || !bytes.Equal(content, bytesAfter) {
		t.Fatal("read rewrote old bytes", err)
	}
}

func TestN89SyncWritePipelineAllocations(t *testing.T) {
	measure := func(padding int) testing.BenchmarkResult {
		f := syncAllocationInput(t, 16, padding, true, false)
		sources, err := selectedSyncSources(f.config, f.router)
		if err != nil {
			t.Fatal(err)
		}
		contents := map[string]string{}
		for _, source := range sources {
			contents[source.Item.ID] = "IP-CIDR,203.0.113.0/24"
		}
		if err = writeSyncSources(f.ctx, sources, contents, f.config); err != nil {
			t.Fatal(err)
		}
		return testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := writeSyncSources(f.ctx, sources, contents, f.config); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	small, large := measure(0), measure(256<<10)
	delta := large.AllocedBytesPerOp() - small.AllocedBytesPerOp()
	t.Logf("write rules=16 B/op=%d/%d delta=%d allocs/op=%d/%d", small.AllocedBytesPerOp(), large.AllocedBytesPerOp(), delta, small.AllocsPerOp(), large.AllocsPerOp())
	if delta > 6<<20 {
		t.Fatalf("write loop repeatedly decoded settings: delta=%d", delta)
	}
}
