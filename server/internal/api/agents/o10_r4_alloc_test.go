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
// Agent views allocate a fraction of the direct projection, and summary reads
// do not grow with the history, cached or not (no whole-record encoding).
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
		return model.SetStoredMember(&system.Agents[0], "deployments", mustRawJSON(t, history))
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
		// The history-free summary is cheap without the cache too.
		if direct > 1<<20 && cached*3 > direct || cached > 2<<20 {
			t.Fatalf("%s: cached %d bytes/request, direct %d", path, cached, direct)
		}
		t.Logf("%s: cached %d bytes/request, direct %d", path, cached, direct)
	}
	for _, cached := range []bool{true, false} {
		if summary := perRequest("/api/agents?summary=1", cached); summary > 256*1024 {
			t.Fatalf("summary (cache %v) allocates %d bytes/request", cached, summary)
		}
	}
	var probe []map[string]json.RawMessage
	if err := json.Unmarshal(f.get("/api/agents").WantStatus(200).Body, &probe); err != nil || len(probe) != 1 || len(probe[0]["deployments"]) < 10000 {
		t.Fatalf("fixture history missing: %v", err)
	}
}
