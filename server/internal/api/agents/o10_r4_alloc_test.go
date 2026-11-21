package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"

	"configflow/internal/model"
)

// TestO10AgentViewCacheAllocations: with a long deployment history, cached
// Agent views allocate a fraction of the direct projection; summary reads do
// not grow with the history at all once cached. The direct path (cache off)
// is the negative control.
func TestO10AgentViewCacheAllocations(t *testing.T) {
	f := newContentHTTP(t, false, false)
	if err := f.s.Repo.UpdateSystem(f.s.Context(), func(_ context.Context, system *model.System) error {
		history := map[string]any{}
		for i := range 300 {
			history[fmt.Sprintf("task-%03d", i)] = map[string]any{
				"status": "failed", "message": "Connection refused", "prepared_content": map[string]any{"fingerprint": "private"},
				"events": []any{map[string]any{"at": "2026-01-01T00:00:00", "status": "failed", "message": "timeout"}},
			}
		}
		return system.Agents[0].SetJSON("deployments", mustRawJSON(t, history))
	}); err != nil {
		t.Fatal(err)
	}
	f.s.Repo.SetViewGuard(nil)
	perRequest := func(path string, cached bool) uint64 {
		t.Helper()
		restore := setAgentViewCache(cached)
		defer restore()
		f.get(path).WantStatus(200)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for range 5 {
			f.get(path).WantStatus(200)
		}
		runtime.ReadMemStats(&after)
		return (after.TotalAlloc - before.TotalAlloc) / 5
	}
	for _, path := range []string{"/api/agents", "/api/agents?history=0", "/api/agents?summary=1", "/api/agents/content-agent"} {
		direct, cached := perRequest(path, false), perRequest(path, true)
		if cached*3 > direct {
			t.Fatalf("%s: cached %d bytes/request, direct %d", path, cached, direct)
		}
		t.Logf("%s: cached %d bytes/request, direct %d", path, cached, direct)
	}
	if summary := perRequest("/api/agents?summary=1", true); summary > 256*1024 {
		t.Fatalf("cached summary allocates %d bytes/request", summary)
	}
	var probe []map[string]json.RawMessage
	if err := json.Unmarshal(f.get("/api/agents").WantStatus(200).Body, &probe); err != nil || len(probe) != 1 || len(probe[0]["deployments"]) < 10000 {
		t.Fatalf("fixture history missing: %v", err)
	}
}
