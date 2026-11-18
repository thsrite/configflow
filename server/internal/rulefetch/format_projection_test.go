package rulefetch

import (
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"context"
	"strings"
	"testing"
)

func knownFormatInput(tb testing.TB, payload int) (context.Context, *jsonx.Object, *jsonx.Object) {
	tb.Helper()
	r, err := repo.Open(tb.TempDir(), repo.Options{})
	if err != nil {
		tb.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	rule := jsonx.Obj("id", "r1", "name", "Synthetic", "source_type", "url", "url", "https://example.com/rules", "format", "text", "behavior", "domain")
	if _, err := r.WriteSharedText(ctx, "rules/"+rules.CacheFilename(legacyRule(rule)), []byte("example.com\n")); err != nil {
		tb.Fatal(err)
	}
	config := jsonx.Obj("rule_library", []any{jsonx.Obj("id", "unused", "name", "Unused", "content", strings.Repeat("x", payload))})
	return ctx, rule, config
}
func TestKnownFormatReadAllocationsIgnoreUnrelatedLibrary(t *testing.T) {
	ctx, rule, small := knownFormatInput(t, 0)
	large := jsonx.Obj("rule_library", []any{jsonx.Obj("id", "unused", "content", strings.Repeat("x", 1<<20))})
	run := func(config *jsonx.Object) float64 {
		return testing.AllocsPerRun(10, func() {
			data, err := ReadValidatedRuleCacheWithFormat(ctx, rule, config, "text")
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
	// Known format is validation authority, never an instruction to accept a
	// cached file under any interpretation.
	data, err := ReadValidatedRuleCacheWithFormat(ctx, rule, large, "mrs")
	if err != nil || data != nil {
		t.Fatal("wrong explicit format accepted", err)
	}
}
func BenchmarkKnownFormatRead(b *testing.B) {
	for _, size := range []int{0, 1 << 20} {
		b.Run(map[int]string{0: "empty", 1 << 20: "1MiB"}[size], func(b *testing.B) {
			ctx, rule, config := knownFormatInput(b, size)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, err := ReadValidatedRuleCacheWithFormat(ctx, rule, config, "text")
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
