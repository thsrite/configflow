package agents

import (
	"bytes"
	"configflow/internal/logx"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestTypedRegistryOwnsCopiesAndPreservesEvidence(t *testing.T) {
	e, id := setupRuntime(t)
	original := []byte(`{"future":{"x":2.00},"enabled":"false","port":"8080","deployments":{"old":{"deployment_id":"old","status":"succeeded","future":3e0}},"ipv6_verifications":{"old":{"status":"passed"}}}`)
	var imported model.Agent
	if err := json.Unmarshal(original, &imported); err != nil {
		t.Fatal(err)
	}
	imported.ID = id
	imported.ProfileID, _ = e.m.defaultProfileID(e.ctx)
	if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, list *[]model.Agent) error { *list = []model.Agent{imported}; return nil }); err != nil {
		t.Fatal(err)
	}
	light, err := e.m.ReadAgent(e.ctx, id, false)
	if err != nil || light == nil {
		t.Fatal(light, err)
	}
	if light.Port != 8080 || light.Enabled != nil || light.Deployments.Len() != 0 || light.IPv6Verifications.Len() != 0 {
		t.Fatal("typed light projection", light)
	}
	before, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil || before.Deployments.Len() != 1 {
		t.Fatal(before, err)
	}
	data, _ := model.Marshal(before)
	if !bytes.Contains(data, []byte(`"enabled":"false"`)) || !bytes.Contains(data, []byte(`"x":2.00`)) || !bytes.Contains(data, []byte(`"future":3e0`)) {
		t.Fatalf("lost stored evidence: %s", data)
	}
	before.Deployments.Set("new", model.Deployment{DeploymentID: "new"})
	if err := light.Fields.Set("future", []byte(`null`)); err != nil {
		t.Fatal(err)
	}
	reread, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil || reread.Deployments.Len() != 1 {
		t.Fatal("shared registry mutated", reread, err)
	}
	future, _, _ := reread.MemberJSON("future")
	if string(future) != `{"x":2.00}` {
		t.Fatalf("shared raw mutated: %s", future)
	}
}

func TestTypedHeartbeatRuntimeShutdownAndDisconnect(t *testing.T) {
	e, id := setupRuntime(t)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(e.ctx)
	defer rt.Shutdown(context.Background())
	request, disconnect := context.WithCancel(rt.BindRequest(e.ctx))
	disconnect()
	payload := &model.Agent{Version: "2.0.0", ConfigVersion: "changed", ServiceStatus: new("running")}
	if ok, err := e.m.ObserveHeartbeat(request, id, payload); err != nil || !ok {
		t.Fatal("disconnect stopped heartbeat", ok, err)
	}
	read, err := e.m.ReadAgent(e.ctx, id, false)
	if err != nil || read.Version != "2.0.0" || read.ConfigVersion != "changed" {
		t.Fatal(read, err)
	}
	bound := rt.BindRequest(e.ctx)
	rt.Shutdown(context.Background())
	head, err := e.repo.Head(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	entry := e.m.Runtime().Entries()[id]
	if ok, err := e.m.ObserveHeartbeat(bound, id, &model.Agent{Version: "3.0.0"}); ok || !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown accepted heartbeat", ok, err)
	}
	if err := e.m.EditAgents(bound, "", func(_ context.Context, rows *[]model.Agent) error { (*rows)[0].Version = "3.0.0"; return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown accepted edit", err)
	}
	after, err := e.repo.Head(e.ctx)
	if err != nil || after != head || e.m.Runtime().Entries()[id].HeartbeatAt != entry.HeartbeatAt {
		t.Fatal("shutdown wrote registry/runtime", err)
	}
}

func newHeartbeatBenchmark(b *testing.B, unused int) (*Manager, context.Context) {
	b.Helper()
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
		if unused != 0 {
			return d.Profiles[0].Fields.Set("unused", json.RawMessage(`"`+strings.Repeat("x", unused)+`"`))
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	// Warm the actual cached generation, not a fake registry or direct engine.
	if _, err := m.ObserveHeartbeat(ctx, "device", &model.Agent{Version: "1.0.0"}); err != nil {
		b.Fatal(err)
	}
	return m, ctx
}

func BenchmarkTypedHeartbeatUnrelatedConfig(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"empty", 0}, {"2MiB", 2 << 20}} {
		b.Run(size.name, func(b *testing.B) {
			m, ctx := newHeartbeatBenchmark(b, size.n)
			payload := &model.Agent{Version: "1.0.0", ConfigVersion: "0"}
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

func TestTypedHeartbeatAllocationsIgnoreUnrelatedConfig(t *testing.T) {
	run := func(size int) testing.BenchmarkResult {
		return testing.Benchmark(func(b *testing.B) {
			m, ctx := newHeartbeatBenchmark(b, size)
			payload := &model.Agent{Version: "1.0.0", ConfigVersion: "0"}
			b.ResetTimer()
			for b.Loop() {
				if ok, err := m.ObserveHeartbeat(ctx, "device", payload); err != nil || !ok {
					b.Fatal(ok, err)
				}
			}
		})
	}
	small, large := run(0), run(2<<20)
	if small.N == 0 || large.N == 0 {
		t.Fatal("benchmark setup/run failed")
	}
	t.Logf("unused=0: %d B/op %d allocs/op; unused=2MiB: %d B/op %d allocs/op", small.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocedBytesPerOp(), large.AllocsPerOp())
	if large.AllocedBytesPerOp() > small.AllocedBytesPerOp()+32<<10 || large.AllocsPerOp() > small.AllocsPerOp()+100 {
		t.Fatal("heartbeat cost scales with unrelated configuration")
	}
}

func FuzzTypedRuntimeProjection(f *testing.F) {
	f.Add([]byte(`{"id":"device","version":"1.0.0","system_metrics":{"cpu":{"usage_percent":20}},"future":{"n":2.00}}`))
	f.Add([]byte(`{"id":"device","version":0,"system_metrics":null,"deployments":{"old":{"status":"succeeded"}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 32<<10 {
			t.Skip()
		}
		var source model.Agent
		if json.Unmarshal(data, &source) != nil {
			t.Skip()
		}
		before, err := model.Marshal(source)
		if err != nil {
			t.Skip()
		}
		state := NewRuntimeState()
		if _, err := state.recordModel(context.Background(), &source, &source, state.Mark()); err != nil {
			t.Fatal(err)
		}
		records := state.overlayModels([]model.Agent{source})
		if len(records) != 1 {
			t.Fatal("lost record")
		}
		after, err := model.Marshal(source)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("runtime changed source evidence", err)
		}
		_ = records[0].SetJSON("version", []byte(`"mutated"`))
		after, err = model.Marshal(source)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("runtime copy aliases source", err)
		}
	})
}

func TestTypedRegistryNumericIDIsNotAnIdentityProof(t *testing.T) {
	var malformed model.Agent
	if err := json.Unmarshal([]byte(`{"id":7,"version":"1.0.0"}`), &malformed); err != nil {
		t.Fatal(err)
	}
	if registryAgent([]model.Agent{malformed}, "7") != nil {
		t.Fatal("stored numeric id became an identity proof")
	}
	if registryAgent([]model.Agent{{ID: "7"}}, "7") == nil {
		t.Fatal("native string identity refused")
	}
}

func TestTypedRegistryShutdownAtCommitPoint(t *testing.T) {
	e, id := setupRuntime(t)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(e.ctx)
	defer rt.Shutdown(context.Background())
	bound := rt.BindRequest(e.ctx)
	before, err := e.repo.Head(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = e.m.EditAgents(bound, "", func(_ context.Context, rows *[]model.Agent) error {
		registryAgent(*rows, id).Version = "shutdown-write"
		rt.Shutdown(context.Background())
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("late shutdown accepted commit", err)
	}
	after, err := e.repo.Head(e.ctx)
	if err != nil || after != before {
		t.Fatal("late shutdown committed registry", err)
	}
}

func TestPreparedOwnershipRequiresOriginalTextIdentity(t *testing.T) {
	e := newEnv(t)
	id := "typed-proof"
	e.begin(id)
	defer e.finish(id)
	before, err := e.repo.Head(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"profile_id":7,"service_type":"mihomo"}`,
		`{"profile_id":"default","service_type":7}`,
		`{"profile_id":null,"service_type":"mihomo"}`,
	} {
		if err := e.m.RememberPreparedContent(e.ctx, e.id(), id, []byte(raw), nil); err == nil {
			t.Fatalf("coerced proof accepted: %s", raw)
		}
	}
	after, err := e.repo.Head(e.ctx)
	if err != nil || after != before {
		t.Fatal("invalid proof wrote registry", err)
	}
}

func TestTypedHeartbeatFailureLogUsesCredentialOwner(t *testing.T) {
	e, id := setupRuntime(t)
	secret := "synthetic-private-proof"
	err := e.m.EditAgents(e.ctx, "", func(_ context.Context, rows *[]model.Agent) error {
		record := registryAgent(*rows, id)
		record.Name = "operator"
		return record.Fields.Set("future_proof", []byte(`{"password":"synthetic-private-proof","username":"operator"}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "metrics.log")
	root, err := logx.Setup(logx.Config{Level: logx.Info, File: file})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	RegisterHooks(Hooks{AddMetricsJSON: func(context.Context, *Manager, string, json.RawMessage) error {
		return errors.New("operator " + secret)
	}})
	if ok, err := e.m.ObserveHeartbeat(e.ctx, id, &model.Agent{SystemMetrics: &model.AgentMetrics{}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if strings.Contains(output, secret) || !strings.Contains(output, "operator") || !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("unsafe or over-redacted warning: %s", output)
	}
}

func TestTypedAutomationEnabledR5Rows(t *testing.T) {
	tests := []struct {
		input   string
		enabled bool
	}{
		{`{}`, true},
		{`{"enabled":true}`, true},
		{`{"enabled":false}`, false},
		{`{"enabled":null}`, false},
		{`{"enabled":0}`, false},
		{`{"enabled":1}`, false},
		{`{"enabled":"false"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var record model.Agent
			if err := json.Unmarshal([]byte(tt.input), &record); err != nil {
				t.Fatal(err)
			}
			if got := AgentEnabledModel(&record); got != tt.enabled {
				t.Fatalf("input=%s enabled=%v want=%v", tt.input, got, tt.enabled)
			}
			after, err := model.Marshal(record)
			if err != nil || string(after) != tt.input {
				t.Fatalf("read rewrote row: %s %v", after, err)
			}
		})
	}
}
