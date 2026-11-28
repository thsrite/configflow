package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/taskstatus"
)

func pollRoute(t testing.TB, f *o10Fixture, path string) {
	t.Helper()
	response := f.do(http.MethodGet, path, "", nil)
	if response.Code != http.StatusOK || response.Body.Len() == 0 {
		t.Fatalf("%s: %d %.300s", path, response.Code, response.Body.String())
	}
}

func measurePollingRoute(t *testing.T, f *o10Fixture, path string) float64 {
	t.Helper()
	pollRoute(t, f, path)
	pollRoute(t, f, path)
	runtime.GC()
	var before, after runtime.MemStats
	t.Log("poll-start", path)
	runtime.ReadMemStats(&before)
	for range 20 {
		pollRoute(t, f, path)
	}
	runtime.ReadMemStats(&after)
	mib := float64(after.TotalAlloc-before.TotalAlloc) / (20 << 20)
	t.Logf("poll-end %s %.4f MiB/request", path, mib)
	return mib
}

func benchmarkPollingRoute(b *testing.B, f *o10Fixture, path string) {
	b.Helper()
	b.Run(path, func(b *testing.B) {
		pollRoute(b, f, path)
		b.ReportAllocs()
		for b.Loop() {
			pollRoute(b, f, path)
		}
	})
}

func pollingTaskFixture(t testing.TB) *o10Fixture {
	t.Helper()
	f := newO10Fixture(t)
	f.srv.Repo.SetViewGuard(nil)
	statuses := taskstatus.Snapshot{}
	var sources []model.RuleLibraryItem
	err := f.srv.Repo.UpdateShared(t.Context(), func(_ context.Context, shared *model.Shared) error {
		for _, rule := range shared.RuleLibrary {
			if rule.URL != nil && *rule.URL != "" && (rule.SourceType == nil || *rule.SourceType == "url") {
				sources = append(sources, rule)
			}
		}
		for len(sources) < 31 {
			id := fmt.Sprintf("poll-%d", len(sources))
			rule := model.RuleLibraryItem{ID: id, Name: id, URL: new("https://example.com/" + id)}
			shared.RuleLibrary = append(shared.RuleLibrary, rule)
			sources = append(sources, rule)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := f.srv.Repo.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	for i, rule := range sources {
		path := filepath.Join(root, rules.CacheFilename(rules.LibraryRule(rule)))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, []byte("example.com\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			statuses["rule_source:"+rule.ID] = json.RawMessage(`{"last_status":"success","last_run_at":"2026-01-01T00:00:00Z","cache_updated_at":"2026-01-01T00:00:00Z"}`)
		}
	}
	data, err := json.Marshal(statuses)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.srv.Repo.DataDir(), taskstatus.Filename), data, 0600); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPollingTaskAllocations(t *testing.T) {
	f := pollingTaskFixture(t)
	for _, path := range []string{"/api/notifications", "/api/tasks"} {
		measurePollingRoute(t, f, path)
	}
}

func BenchmarkPollingTasks(b *testing.B) {
	f := pollingTaskFixture(b)
	for _, path := range []string{"/api/notifications", "/api/tasks"} {
		benchmarkPollingRoute(b, f, path)
	}
}
