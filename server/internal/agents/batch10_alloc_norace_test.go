//go:build !race

package agents

import (
 "testing"
 "configflow/internal/jsonx"
 "configflow/internal/model"
)

// Quantitative thresholds use the same non-race build as the allocation
// benchmarks. Race builds verify ownership separately: instrumentation changes
// encoder and SQLite allocation costs, so these limits do not apply to them.
func TestBatch10HeartbeatAllocationCeilings(t *testing.T) {
	for _, owner := range []string{"public", "native"} {
		t.Run(owner, func(t *testing.T) {
			for _, size := range []int{0, 2 << 20} {
				measured := testing.Benchmark(func(b *testing.B) {
					m, ctx := newHeartbeatBenchmark(b, size)
					legacy := jsonx.Obj("version", "1.0.0", "config_version", "0")
					native := &model.Agent{Version: "1.0.0", ConfigVersion: "0"}
					b.ResetTimer()
					for b.Loop() {
						var ok bool
						var err error
						if owner == "public" {
							ok, err = m.UpdateHeartbeat(ctx, "device", legacy)
						} else {
							ok, err = m.ObserveHeartbeat(ctx, "device", native)
						}
						if err != nil || !ok {
							b.Fatal(ok, err)
						}
					}
				})
				if measured.N == 0 {
					t.Fatal("heartbeat benchmark did not execute")
				}
				// Approved batch-10 candidate maxima plus approximately 20% headroom:
				// public 9.0 KB/180 allocations; native 8.0 KB/166 allocations.
				maxBytes, maxAllocs := int64(10840), int64(216)
				if owner == "native" {
					maxBytes, maxAllocs = 9600, 200
				}
				t.Logf("unused=%d: %d B/op %d allocs/op", size, measured.AllocedBytesPerOp(), measured.AllocsPerOp())
				if measured.AllocedBytesPerOp() > maxBytes || measured.AllocsPerOp() > maxAllocs {
					t.Fatalf("heartbeat allocation regression: %d B/op %d allocs/op (max %d/%d)", measured.AllocedBytesPerOp(), measured.AllocsPerOp(), maxBytes, maxAllocs)
				}
			}
		})
	}
}
