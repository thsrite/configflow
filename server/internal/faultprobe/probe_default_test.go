//go:build !configflow_difftest

package faultprobe

import (
	"context"
	"testing"
)

func TestProductionPointAllocatesNothing(t *testing.T) {
	if n := testing.AllocsPerRun(1000, func() { Point(context.Background(), StoreBeforeCommit) }); n != 0 {
		t.Fatalf("Point allocations: %v", n)
	}
	if len(Active()) != 0 {
		t.Fatal("production phases active")
	}
}
func BenchmarkProductionPoint(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		Point(ctx, StoreBeforeCommit)
	}
}
