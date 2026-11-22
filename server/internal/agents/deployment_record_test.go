package agents

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"configflow/internal/model"
)

func newRecordPreviewBenchmark(b *testing.B, unused int) (*Manager, context.Context, DeploymentUpdate) {
	b.Helper()
	m, ctx := newHeartbeatBenchmark(b, unused)
	if err := m.EditAgents(ctx, "", func(_ context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, "device")
		a.Name, a.Host, a.Port, a.Token, a.ServiceType = "device", "192.0.2.40", 8080, "synthetic-device-token", "mihomo"
		a.OxiDNSHAPublication = &model.HAJournal{Status: "succeeded"}
		other := model.Agent{ID: "other", ProfileID: a.ProfileID, Name: "other", Host: "192.0.2.41", Port: 8080, Token: "synthetic-other-token", ServiceType: "mihomo"}
		*records = append(*records, other)
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	if _, err := m.ReserveDeployment(ctx, "device", "preview"); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { m.FinishDeploymentPreparation("device", "preview") })
	patch, err := DecodeDeploymentUpdate([]byte(`{"status":"checking","success":true}`))
	if err != nil {
		b.Fatal(err)
	}
	if result, err := m.RecordDeploymentModel(ctx, "device", "preview", patch, RecordOptions{}); err != nil || result == nil || result.Status != "checking" || result.DeploymentID != "preview" {
		b.Fatal(result, err)
	}
	return m, ctx, patch
}

func BenchmarkTypedRecordUnchangedPreview(b *testing.B) {
	for _, tt := range []struct {
		name string
		size int
	}{{"empty", 0}, {"2MiB", 2 << 20}} {
		b.Run(tt.name, func(b *testing.B) {
			m, ctx, patch := newRecordPreviewBenchmark(b, tt.size)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if result, err := m.RecordDeploymentModel(ctx, "device", "preview", patch, RecordOptions{SkipUnchanged: true}); err != nil || result.Status != "checking" {
					b.Fatal(result, err)
				}
			}
		})
	}
}

func TestTypedRecordPreviewAllocationIgnoresUnrelatedConfig(t *testing.T) {
	measure := func(size int) testing.BenchmarkResult {
		return testing.Benchmark(func(b *testing.B) {
			m, ctx, patch := newRecordPreviewBenchmark(b, size)
			before, err := m.Repository().Head(ctx)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if result, err := m.RecordDeploymentModel(ctx, "device", "preview", patch, RecordOptions{SkipUnchanged: true}); err != nil || result == nil || result.Status != "checking" {
					b.Fatal(result, err)
				}
			}
			b.StopTimer()
			if after, err := m.Repository().Head(ctx); err != nil || after != before {
				b.Fatal("unchanged preview committed", err)
			}
		})
	}
	small, large := measure(0), measure(2<<20)
	if small.N == 0 || large.N == 0 {
		t.Fatal("benchmark setup/run failed")
	}
	t.Logf("record preview empty: %d B/op %d allocs/op; 2MiB per unrelated source: %d B/op %d allocs/op", small.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocedBytesPerOp(), large.AllocsPerOp())
	if large.AllocedBytesPerOp() > small.AllocedBytesPerOp()+64<<10 || large.AllocsPerOp() > small.AllocsPerOp()+200 {
		t.Fatal("record preview traversed unrelated configuration")
	}
}

func FuzzTypedDeploymentPatchOwnership(f *testing.F) {
	f.Add([]byte(`{"status":"checking","message":"token=synthetic-device-token","profile_id":"forged"}`))
	f.Add([]byte(`{"status":["bad"],"submitted":true,"error":"synthetic-device-token"}`))
	f.Add([]byte(`{"status":true,"manual_rollback":null,"dns_checks":[{"name":7}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32<<10 {
			t.Skip()
		}
		patch, err := DecodeDeploymentUpdate(data)
		if err != nil {
			return
		}
		patchBefore, err := model.Marshal(patch.record)
		if err != nil {
			return
		}
		state := model.Deployment{DeploymentID: "job", ProfileID: "default", ServiceType: "mihomo", Status: "checking"}
		a := model.Agent{ID: "device", ProfileID: "default", ServiceType: "mihomo", Token: "synthetic-device-token", ConfigVersion: "previous", LatestDeployment: &model.Deployment{DeploymentID: "job"}}
		before, _ := model.Marshal(state)
		_, err = (&Manager{}).applyDeploymentUpdate(context.Background(), &a, "job", &state, patch, RecordOptions{})
		if state.DeploymentID != "job" || state.ProfileID != "default" || state.ServiceType != "mihomo" || a.ID != "device" || a.ProfileID != "default" || a.ServiceType != "mihomo" || a.Token != "synthetic-device-token" {
			t.Fatal("untrusted patch changed identity")
		}
		if errors.Is(err, ErrInvalidDeploymentStatus) {
			after, _ := model.Marshal(state)
			if !bytes.Equal(before, after) {
				t.Fatal("invalid status mutated journal")
			}
		}
		patchAfter, err := model.Marshal(patch.record)
		if err != nil || !bytes.Equal(patchBefore, patchAfter) {
			t.Fatal("patch source mutated", err)
		}
		for _, key := range []string{"message", "error", "error_details", "rollback_error", "rollback_error_details"} {
			text := originalDeploymentText(&state, key)
			if strings.Contains(text, "synthetic-device-token") || len(text) > DetailLimit {
				t.Fatalf("unsafe %s", key)
			}
		}
	})
}
