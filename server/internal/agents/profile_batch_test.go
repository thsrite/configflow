package agents

import (
	"testing"

	"configflow/internal/model"
)

func BenchmarkResolveAgentProfiles(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"empty", 0}, {"2MiB", 2 << 20}} {
		b.Run(size.name, func(b *testing.B) {
			m, ctx := newHeartbeatBenchmark(b, size.n)
			rows := make([]model.Agent, 100)
			for i := range rows {
				rows[i].ProfileID = "default"
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := m.ResolveAgentProfiles(ctx, rows); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestOptionalAgentProfilesIgnoreOversizedUnrelatedConfig(t *testing.T) {
	run := func(unused int) testing.BenchmarkResult {
		return testing.Benchmark(func(b *testing.B) {
			m, ctx := newHeartbeatBenchmark(b, unused)
			rows := make([]model.Agent, 100)
			if _, err := m.ResolveAgentProfiles(ctx, rows); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for b.Loop() {
				if got, err := m.ResolveAgentProfiles(ctx, rows); err != nil || len(got) != 100 || got[0] == "" {
					b.Fatal(got, err)
				}
			}
		})
	}
	small, large := run(0), run(16<<20)
	t.Logf("100 optional bindings empty: %d B/op %d allocs/op; 16MiB: %d B/op %d allocs/op", small.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocedBytesPerOp(), large.AllocsPerOp())
	if large.AllocedBytesPerOp() > small.AllocedBytesPerOp()+32<<10 || large.AllocsPerOp() > small.AllocsPerOp()+100 {
		t.Fatal("optional bindings decoded unrelated document")
	}
}
