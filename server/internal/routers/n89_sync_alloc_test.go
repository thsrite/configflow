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
	"runtime"
	"runtime/debug"
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
	// // Allocation measurements exclude the test-only view guard's encodings.
	db.SetViewGuard(nil)
	// A legacy GetShared/JSON bridge in each cache miss would copy this unrelated
	// shared extension once per selected rule. The typed View path must not.
	err := db.UpdateShared(context.Background(), func(_ context.Context, s *model.Shared) error {
		s.Subscriptions = append(s.Subscriptions, model.Subscription{ID: "padding", Name: strings.Repeat("x", padding)})
		return nil
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

type syncAllocationStats struct {
	bytesPerOp  int64
	allocsPerOp int64
}

// measureSyncAllocations measures per-operation heap allocations over a fixed
// iteration count instead of testing.Benchmark's wall-clock loop. Under -race
// and CPU contention, wall-clock benchmarking can collapse to very small b.N
// and amplify one-off pool/goroutine allocations from sql.Open. Pinning
// GOMAXPROCS(1), pausing GC during the short measurement window so small and
// large fixtures do not evict sync.Pool at different live-heap thresholds, and
// taking the minimum across samples keeps the floor stable without hiding real
// per-source copies.
func measureSyncAllocations(t testing.TB, samples, runs int, fn func()) syncAllocationStats {
	t.Helper()
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	var best syncAllocationStats
	for i := range samples {
		runtime.GC()
		runtime.Gosched()
		cur := func() syncAllocationStats {
			prevGC := debug.SetGCPercent(-1)
			defer debug.SetGCPercent(prevGC)
			// Re-warm sync.Pool caches cleared by GC before reading MemStats.
			fn()
			runtime.Gosched()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			for range runs {
				fn()
			}
			runtime.ReadMemStats(&after)
			return syncAllocationStats{
				bytesPerOp:  int64(after.TotalAlloc-before.TotalAlloc) / int64(runs),
				allocsPerOp: int64(after.Mallocs-before.Mallocs) / int64(runs),
			}
		}()
		if i == 0 || cur.bytesPerOp < best.bytesPerOp {
			best = cur
		}
	}
	runtime.GC()
	return best
}

func measureSyncPipeline(t testing.TB, fixture syncAllocationFixture, samples, runs int) syncAllocationStats {
	t.Helper()
	return measureSyncAllocations(t, samples, runs, func() {
		_, records, err := loadSyncSources(fixture.ctx, fixture.config, fixture.router, fixture.download)
		if err != nil || len(records) != len(fixture.router.IPRuleSets) {
			t.Fatal("pipeline failed", err)
		}
	})
}

func TestN89SyncPipelineAllocations(t *testing.T) {
	// With 1 MiB unrelated padding, copying it even once per call adds >= 1 MiB
	// (>= 16 MiB at rules=16). A 256 KiB delta ceiling still catches a single
	// copy by 4x while leaving room above residual SQLite/race jitter on the
	// ~2.1 MiB rules=16 baseline.
	const cacheDeltaBudget = 256 << 10
	for _, n := range []int{1, 16} {
		small := measureSyncPipeline(t, syncAllocationInput(t, n, 0, false, false), 3, 2)
		large := measureSyncPipeline(t, syncAllocationInput(t, n, 1<<20, false, false), 3, 2)
		delta := large.bytesPerOp - small.bytesPerOp
		t.Logf("cache rules=%d padding=0/1MiB B/op=%d/%d delta=%d allocs/op=%d/%d", n, small.bytesPerOp, large.bytesPerOp, delta, small.allocsPerOp, large.allocsPerOp)
		if delta > cacheDeltaBudget {
			t.Fatalf("unrelated shared/config copied per source: delta=%d (max %d)", delta, cacheDeltaBudget)
		}
	}
	const budget = 6 << 20
	nativeSmall := measureSyncPipeline(t, syncAllocationInput(t, 16, 0, true, false), 2, 2)
	nativeLarge := measureSyncPipeline(t, syncAllocationInput(t, 16, 256<<10, true, false), 2, 2)
	brokenSmall := measureSyncPipeline(t, syncAllocationInput(t, 16, 0, true, true), 2, 1)
	brokenLarge := measureSyncPipeline(t, syncAllocationInput(t, 16, 256<<10, true, true), 2, 1)
	nativeDelta := nativeLarge.bytesPerOp - nativeSmall.bytesPerOp
	brokenDelta := brokenLarge.bytesPerOp - brokenSmall.bytesPerOp
	t.Logf("download rules=16 native B/op=%d/%d delta=%d; environment-hidden B/op=%d/%d delta=%d", nativeSmall.bytesPerOp, nativeLarge.bytesPerOp, nativeDelta, brokenSmall.bytesPerOp, brokenLarge.bytesPerOp, brokenDelta)
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
	measure := func(padding int) syncAllocationStats {
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
		return measureSyncAllocations(t, 2, 2, func() {
			if err := writeSyncSources(f.ctx, sources, contents, f.config); err != nil {
				t.Fatal(err)
			}
		})
	}
	small, large := measure(0), measure(256<<10)
	delta := large.bytesPerOp - small.bytesPerOp
	t.Logf("write rules=16 B/op=%d/%d delta=%d allocs/op=%d/%d", small.bytesPerOp, large.bytesPerOp, delta, small.allocsPerOp, large.allocsPerOp)
	if delta > 6<<20 {
		t.Fatalf("write loop repeatedly decoded settings: delta=%d", delta)
	}
}
