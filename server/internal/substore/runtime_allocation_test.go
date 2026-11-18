package substore

import (
	"configflow/internal/model"
	"configflow/internal/repo"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func modeReadFixture(tb testing.TB, size int) context.Context {
	tb.Helper()
	r, err := repo.Open(tb.TempDir(), repo.Options{})
	if err != nil {
		tb.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	data, _ := json.Marshal(strings.Repeat("x", size))
	if err := r.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		mode := "always"
		sys.SystemConfig.SubStoreMode = &mode
		return sys.SystemConfig.Fields.Set("n89-unrelated", data)
	}); err != nil {
		tb.Fatal(err)
	}
	if mode, err := GetMode(ctx); err != nil || mode != "always" {
		tb.Fatalf("mode fixture: %s %v", mode, err)
	}
	return ctx
}
func BenchmarkN89RuntimeMode(b *testing.B) {
	for _, size := range []int{0, 64 << 10, 1 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			ctx := modeReadFixture(b, size)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if mode, err := GetMode(ctx); err != nil || mode != "always" {
					b.Fatalf("mode: %s %v", mode, err)
				}
			}
		})
	}
}
func TestN89RuntimeModeAllocationBudget(t *testing.T) {
	measure := func(size int) int64 {
		ctx := modeReadFixture(t, size)
		result := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				if mode, err := GetMode(ctx); err != nil || mode != "always" {
					b.Fatalf("mode: %s %v", mode, err)
				}
			}
		})
		return result.AllocedBytesPerOp()
	}
	small, large := measure(0), measure(1<<20)
	if large > small+(64<<10) {
		t.Fatalf("mode getter copied unrelated configuration: empty=%d B/op, 1MiB=%d B/op", small, large)
	}
}

func TestN89RuntimeTempRootDoesNotProbeOrFallback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	t.Setenv("TMPDIR", root)
	if got := runtimeTempRoot(); got != root {
		t.Fatalf("temporary root changed %q", got)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("root selection wrote filesystem: %v", err)
	}
}
