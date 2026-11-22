package repo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"configflow/internal/model"
	"configflow/internal/store"
)

func agentViewRepo(t *testing.T) *Repository {
	t.Helper()
	r := newRepo(t)
	r.cacheMaxBytes = 1024
	r.dropCache()
	if err := r.Update(t.Context(), func(_ context.Context, document *model.Document) error {
		document.System.Agents = []model.Agent{{ID: "device", Name: "committed", ProfileID: "default", Host: "192.0.2.40", Port: 8080, Token: "synthetic-token", ServiceType: "mihomo"}}
		return document.Profiles[0].SetJSON("description", []byte(`"`+strings.Repeat("x", 4096)+`"`))
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func agentViewName(t *testing.T, r *Repository, ctx context.Context) string {
	t.Helper()
	var name string
	if err := r.ViewAgents(ctx, func(records []model.Agent) error {
		if len(records) != 1 {
			t.Fatalf("registry size=%d", len(records))
		}
		name = records[0].Name
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestAgentViewCachesRegistryWithoutLoadingOversizedDocument(t *testing.T) {
	r := agentViewRepo(t)
	before := r.loads.Load()
	for range 8 {
		if got := agentViewName(t, r, t.Context()); got != "committed" {
			t.Fatal(got)
		}
	}
	if r.loads.Load() != before || r.agentLoads.Load() != 1 {
		t.Fatalf("full loads=%d registry loads=%d", r.loads.Load()-before, r.agentLoads.Load())
	}
	if r.cached() != nil && r.cached().Head == r.agentsCache.head {
		t.Fatal("test did not exceed document cache budget")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.ViewAgents(t.Context(), func(records []model.Agent) error {
				if len(records) != 1 || records[0].Name != "committed" {
					t.Error("shared registry changed")
				}
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if r.agentLoads.Load() != 1 {
		t.Fatal("cache hit redecoded registry")
	}
	if err := r.UpdateSystem(t.Context(), func(_ context.Context, system *model.System) error { system.Agents[0].Name = "next"; return nil }); err != nil {
		t.Fatal(err)
	}
	before = r.loads.Load()
	if got := agentViewName(t, r, t.Context()); got != "next" {
		t.Fatal(got)
	}
	if r.loads.Load() != before || r.agentLoads.Load() != 2 {
		t.Fatal("generation refresh loaded unrelated document")
	}
}

func TestAgentViewUsesWorkingAndPinnedSnapshotsWithoutPublishingThem(t *testing.T) {
	r := agentViewRepo(t)
	if got := agentViewName(t, r, t.Context()); got != "committed" {
		t.Fatal(got)
	}
	cached := r.agentsCache
	refuse := errors.New("synthetic abort")
	err := r.UpdateSystem(t.Context(), func(tx context.Context, system *model.System) error {
		system.Agents[0].Name = "uncommitted"
		if got := agentViewName(t, r, tx); got != "uncommitted" {
			t.Fatal(got)
		}
		typed, err := r.System(tx)
		if err != nil || len(typed.Agents) == 0 || typed.Agents[0].Name != "uncommitted" {
			t.Fatal("working view unavailable", err)
		}
		if r.agentsCache != cached {
			t.Fatal("uncommitted view was published")
		}
		return refuse
	})
	if !errors.Is(err, refuse) || agentViewName(t, r, t.Context()) != "committed" {
		t.Fatal("working snapshot leaked", err)
	}
	if err := r.WithReadSnapshot(t.Context(), func(held context.Context) error {
		base, err := r.store.Load()
		if err != nil {
			return err
		}
		data, err := json.Marshal(base.Document)
		if err != nil {
			return err
		}
		var changed store.Document
		if err := json.Unmarshal(data, &changed); err != nil {
			return err
		}
		for i := range changed.System.Fields {
			if changed.System.Fields[i].Name == "agents" {
				changed.System.Fields[i].Value = json.RawMessage(`[{"id":"device","name":"foreign","profile_id":"default"}]`)
			}
		}
		if _, committed, err := r.store.Commit(base, &changed, nil); err != nil || !committed {
			t.Fatalf("external fixture commit=%v err=%v", committed, err)
		}
		if got := agentViewName(t, r, held); got != "committed" {
			t.Fatal("pinned view moved", got)
		}
		if r.agentsCache != cached {
			t.Fatal("pinned view changed live cache")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := agentViewName(t, r, t.Context()); got != "foreign" {
		t.Fatal("live view remained pinned", got)
	}
}

func TestAgentViewRetainsOneOversizedRegistryGeneration(t *testing.T) {
	r := agentViewRepo(t)
	if err := r.UpdateSystem(t.Context(), func(_ context.Context, system *model.System) error {
		system.Agents[0].LoggingConfig = &model.AgentLoggingConfig{Enabled: json.RawMessage(`"` + strings.Repeat("x", 4096) + `"`)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := r.loads.Load()
	for range 4 {
		if name := agentViewName(t, r, t.Context()); name != "committed" {
			t.Fatal(name)
		}
	}
	if r.loads.Load() != before || r.agentLoads.Load() != 1 {
		t.Fatal("oversized registry was decoded on each read")
	}
	if _, err := r.AgentTokensForSanitization(t.Context()); err != nil {
		t.Fatal(err)
	}
	before = r.loads.Load()
	for range 4 {
		tokens, err := r.AgentTokensForSanitization(t.Context())
		if err != nil || !tokens.Has("synthetic-token") {
			t.Fatal("missing original token", err)
		}
	}
	if r.loads.Load() != before {
		t.Fatal("redaction cache hit decoded oversized document")
	}
}

func TestAgentViewOriginalTypesCancellationAndReadGuard(t *testing.T) {
	r := agentViewRepo(t)
	base, err := r.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(base.Document)
	if err != nil {
		t.Fatal(err)
	}
	var changed store.Document
	if err := json.Unmarshal(data, &changed); err != nil {
		t.Fatal(err)
	}
	const original = `[{"id":"device","profile_id":"default","port":"8080","token":7,"enabled":"false","future":{"x":2.00}}]`
	for i := range changed.System.Fields {
		if changed.System.Fields[i].Name == "agents" {
			changed.System.Fields[i].Value = json.RawMessage(original)
		}
	}
	if _, committed, err := r.store.Commit(base, &changed, nil); err != nil || !committed {
		t.Fatal(committed, err)
	}
	before, err := r.store.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ViewAgents(t.Context(), func(records []model.Agent) error {
		// Stored values read leniently (O18 T1b): integer text is a number,
		// a number is text, the Agent's enabled switch (a device-write gate,
		// O18 T1b review) of another type is off, unknown members drop.
		for _, tt := range []struct{ member, want string }{{"port", `8080`}, {"token", `"7"`}, {"enabled", `false`}} {
			raw, present, err := model.EncodeMember(&records[0], tt.member)
			if err != nil || !present || string(raw) != tt.want {
				t.Fatal(tt.member, string(raw), err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if after, err := r.store.Fingerprint(); err != nil || after != before {
		t.Fatal("native view wrote rows", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	if err := r.ViewAgents(cancelled, func([]model.Agent) error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled view entered callback", err)
	}
	guarded := false
	r.SetViewGuard(func() { guarded = true })
	if err := r.ViewAgents(t.Context(), func(records []model.Agent) error { records[0].Name = "forbidden"; return nil }); err != nil || !guarded {
		t.Fatal("read guard missed mutation", err)
	}
	// The mutation was deliberate: forget the remembered generations.
	r.SetViewGuard(nil)
}
