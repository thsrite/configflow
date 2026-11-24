package rulefetch

import (
	"context"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
)

func knownFormatInput(tb testing.TB, payload int) (context.Context, *repo.Repository, model.RuleConfig, *SourceConfig) {
	tb.Helper()
	r, err := repo.Open(tb.TempDir(), repo.Options{})
	if err != nil {
		tb.Fatal(err)
	}
	ctx := context.Background()
	rule := model.RuleConfig{
		ID:         "r1",
		Name:       "Synthetic",
		SourceType: strp("url"),
		URL:        strp("https://example.com/rules"),
		Format:     strp("text"),
		Behavior:   strp("domain"),
	}
	if _, err := r.WriteSharedText(ctx, "rules/"+rules.CacheFilename(rule), []byte("example.com\n")); err != nil {
		tb.Fatal(err)
	}
	config := &SourceConfig{
		Library: []model.RuleLibraryItem{{ID: "unused", Name: "Unused", Content: strp(strings.Repeat("x", payload))}},
	}
	return ctx, r, rule, config
}

func TestKnownFormatReadAllocationsIgnoreUnrelatedLibrary(t *testing.T) {
	ctx, r, rule, small := knownFormatInput(t, 0)
	large := &SourceConfig{
		Library: []model.RuleLibraryItem{{ID: "unused", Content: strp(strings.Repeat("x", 1<<20))}},
	}
	run := func(config *SourceConfig) float64 {
		return testing.AllocsPerRun(10, func() {
			data, err := ReadCacheWithFormat(ctx, r, rule, config, "text")
			if err != nil || string(data) != "example.com\n" {
				t.Fatal(err)
			}
		})
	}
	a, b := run(small), run(large)
	if b > a+2 {
		t.Fatalf("unrelated library allocations: small=%g large=%g", a, b)
	}
	t.Logf("small=%g large=%g allocs/read", a, b)
	// Allocation counts alone can miss a copied megabyte when the number of
	// buffers stays constant. Guard bytes/op as well as reporting the benchmark.
	bytesPerRead := func(config *SourceConfig) int64 {
		result := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				data, err := ReadCacheWithFormat(ctx, r, rule, config, "text")
				if err != nil || string(data) != "example.com\n" {
					b.Fatal("known-format read changed", err)
				}
			}
		})
		if result.N == 0 {
			t.Fatal("allocation benchmark failed")
		}
		return result.AllocedBytesPerOp()
	}
	smallBytes, largeBytes := bytesPerRead(small), bytesPerRead(large)
	if largeBytes > smallBytes+(64<<10) {
		t.Fatalf("unrelated library copied: small=%d large=%d bytes/read", smallBytes, largeBytes)
	}
	t.Logf("small=%d large=%d bytes/read", smallBytes, largeBytes)

	// Known format is validation authority, never an instruction to accept a
	// cached file under any interpretation.
	data, err := ReadCacheWithFormat(ctx, r, rule, large, "mrs")
	if err != nil || data != nil {
		t.Fatal("wrong explicit format accepted", err)
	}
}

func BenchmarkKnownFormatRead(b *testing.B) {
	for _, size := range []int{0, 1 << 20} {
		b.Run(map[int]string{0: "empty", 1 << 20: "1MiB"}[size], func(b *testing.B) {
			ctx, r, rule, config := knownFormatInput(b, size)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, err := ReadCacheWithFormat(ctx, r, rule, config, "text")
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
