package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

// BenchmarkBatch10Heartbeat measures the typed heartbeat owner with an actual
// warmed repository.
func BenchmarkBatch10Heartbeat(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"empty", 0}, {"2MiB", 2 << 20}} {
		b.Run(size.name, func(b *testing.B) {
			r, err := repo.OpenDefault(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			ctx := context.Background()
			m, err := New(ctx, r)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(m.Close)
			profile, err := r.DefaultProfileID(ctx)
			if err != nil {
				b.Fatal(err)
			}
			err = r.Update(ctx, func(_ context.Context, d *model.Document) error {
				d.System.Agents = []model.Agent{{ID: "device", ProfileID: profile, Version: "1.0.0", ConfigVersion: "0", LastHeartbeat: "2000-01-01T00:00:00"}}
				return d.Profiles[0].Fields.Set("unused", json.RawMessage(`"`+strings.Repeat("x", size.n)+`"`))
			})
			if err != nil {
				b.Fatal(err)
			}
			payload := &model.Agent{Version: "1.0.0", ConfigVersion: "0"}
			if ok, err := m.ObserveHeartbeat(ctx, "device", payload); err != nil || !ok {
				b.Fatal(ok, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if ok, err := m.ObserveHeartbeat(ctx, "device", payload); err != nil || !ok {
					b.Fatal(ok, err)
				}
			}
		})
	}
}
