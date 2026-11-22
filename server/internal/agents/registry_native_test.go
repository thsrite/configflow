package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestTypedRegistryOwnsCopies(t *testing.T) {
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
	// Stored values read leniently (O18 T1b): "8080" is the port; the text
	// "false" in the Agent's enabled switch (a device-write gate) is off.
	if light.Port != 8080 || light.Enabled == nil || *light.Enabled || light.Deployments.Len() != 0 || light.IPv6Verifications.Len() != 0 {
		t.Fatal("typed light projection", light)
	}
	before, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil || before.Deployments.Len() != 1 {
		t.Fatal(before, err)
	}
	before.Deployments.Set("new", model.Deployment{DeploymentID: "new"})
	reread, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil || reread.Deployments.Len() != 1 {
		t.Fatal("shared registry mutated", reread, err)
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
	entry, _ := runtimeEntryCopy(e.m.Runtime(), id)
	if ok, err := e.m.ObserveHeartbeat(bound, id, &model.Agent{Version: "3.0.0"}); ok || !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown accepted heartbeat", ok, err)
	}
	if err := e.m.EditAgents(bound, "", func(_ context.Context, rows *[]model.Agent) error { (*rows)[0].Version = "3.0.0"; return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown accepted edit", err)
	}
	after, err := e.repo.Head(e.ctx)
	afterEntry, _ := runtimeEntryCopy(e.m.Runtime(), id)
	if err != nil || after != head || afterEntry.HeartbeatAt != entry.HeartbeatAt {
		t.Fatal("shutdown wrote registry/runtime", err)
	}
}

func newHeartbeatBenchmark(b *testing.B, unused int) (*Manager, context.Context) {
	b.Helper()
	r, err := repo.OpenDefault(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	// Allocation measurements exclude the test-only view guard's encodings.
	r.SetViewGuard(nil)
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
			return d.Profiles[0].SetJSON("description", json.RawMessage(`"`+strings.Repeat("x", unused)+`"`))
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
	f.Add([]byte(`{"id":"device","oxidns_ha_publication":{"status":"ready","enable":{"future_source":{"opaque":2.00}}}}`))
	f.Add([]byte(`{"id":"device","oxidns_ha_publication":{}}`))
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
		checkPresence := func(journal *model.HAJournal) {
			t.Helper()
			raw, err := model.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			var members map[string]json.RawMessage
			if err := json.Unmarshal(raw, &members); err != nil {
				t.Fatal(err)
			}
			for name, value := range members {
				if string(value) == "null" {
					delete(members, name) // active_agent_id is always written
				}
			}
			if journal.HasMembers() != (len(members) > 0) {
				t.Fatal("HA presence differs from native JSON")
			}
		}
		if source.OxiDNSHAPublication != nil {
			journal := *source.OxiDNSHAPublication
			checkPresence(&journal)
			journal.Enable = nil
			checkPresence(&journal)
			if err := model.ZeroMember(&journal, "enable"); err != nil {
				t.Fatal(err)
			}
			checkPresence(&journal)
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
		_ = model.SetStoredMember(&records[0], "version", []byte(`"mutated"`))
		after, err = model.Marshal(source)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("runtime copy aliases source", err)
		}
	})
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
		record.LoggingConfig = &model.AgentLoggingConfig{Enabled: json.RawMessage(`{"password":"synthetic-private-proof","username":"operator"}`)}
		return nil
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

func TestTypedRuntimeLegacyImportHonorsSourceGeneration(t *testing.T) {
	e, id := setupRuntime(t)
	e.ageRegistration(5)
	path := filepath.Join(e.dir, LegacyDatabase)
	legacyDatabase(t, path, [][4]any{legacyRowOf(id, clock(), map[string]any{"version": "2.0.0"})})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(e.ctx)
	stopped := rt.BindRequest(e.ctx)
	rt.Shutdown(context.Background())
	ResetRuntime()
	state := NewRuntimeState()
	if err := importLegacyDatabase(stopped, e.repo, state, e.m.saveReportedModels); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown imported legacy runtime", err)
	}
	if _, err := New(stopped, e.repo); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown created manager", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || runtimeEntriesCount(state) != 0 {
		t.Fatal("shutdown changed legacy runtime", err)
	}
	active := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	active.Start(e.ctx)
	defer active.Shutdown(context.Background())
	request, disconnect := context.WithCancel(active.BindRequest(e.ctx))
	disconnect()
	if err := importLegacyDatabase(request, e.repo, state, e.m.saveReportedModels); err != nil {
		t.Fatal("disconnect stopped import", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) || runtimeEntriesCount(state) != 1 {
		t.Fatal("legacy import did not finish", err)
	}
}

func TestTypedHAEnablePresenceAndAllocationBound(t *testing.T) {
	for _, tt := range []struct {
		raw      string
		enabling bool
	}{
		{`{"oxidns_ha_publication":{"status":"ready","enable":null}}`, false},
		{`{"oxidns_ha_publication":{"status":"ready","enable":{}}}`, true},
		{`{"oxidns_ha_publication":{"status":"ready","enable":{"peer_id":null}}}`, true},
		{`{"oxidns_ha_publication":{"status":"ready","enable":7}}`, false},
		{`{"oxidns_ha_publication":{"status":"succeeded","enable":{"peer_id":"peer"}}}`, false},
	} {
		var record model.Agent
		if err := json.Unmarshal([]byte(tt.raw), &record); err != nil {
			t.Fatal(err)
		}
		if got := HAEnablingModel(&record); got != tt.enabling {
			t.Fatalf("input=%s enabling=%v want=%v", tt.raw, got, tt.enabling)
		}
	}
	measure := func(unused int) testing.BenchmarkResult {
		intent := &model.HAEnable{}
		record := &model.Agent{OxiDNSHAPublication: &model.HAJournal{Status: "ready", Enable: intent}}
		return testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if !HAActiveModel(record) || !HAEnablingModel(record) {
					b.Fatal("lost active intent")
				}
			}
		})
	}
	small, large := measure(0), measure(2<<20)
	t.Logf("HA guards unused=0: %d B/op %d allocs/op; unused=2MiB: %d B/op %d allocs/op", small.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocedBytesPerOp(), large.AllocsPerOp())
	if large.AllocedBytesPerOp() > small.AllocedBytesPerOp()+1024 || large.AllocedBytesPerOp() > 1024 || large.AllocsPerOp() > small.AllocsPerOp()+1 || large.AllocsPerOp() > 4 {
		t.Fatal("HA guard walks captured payload")
	}
}

// BenchmarkTypedHAGuardsUnrelatedPayload measures both allocation bytes and
// counts without including fixture decoding in the publication guard cost.
func BenchmarkTypedHAGuardsUnrelatedPayload(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"empty", 0}, {"2MiB", 2 << 20}} {
		b.Run(size.name, func(b *testing.B) {
			intent := &model.HAEnable{}
			record := &model.Agent{OxiDNSHAPublication: &model.HAJournal{Status: "ready", Enable: intent}}
			if !HAActiveModel(record) || !HAEnablingModel(record) {
				b.Fatal("lost active intent")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if !HAActiveModel(record) || !HAEnablingModel(record) {
					b.Fatal("lost active intent")
				}
			}
		})
	}
}
