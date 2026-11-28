package stats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

func statsRepository(t *testing.T) (*repo.Repository, context.Context) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r, repo.WithScope(context.Background())
}

func TestHEADHasGETHeadersAndNeverCreatesOrReplacesHistory(t *testing.T) {
	r, ctx := statsRepository(t)
	app := httpx.New()
	app.Repo = r
	NewHandler(r).Register(app.Router)
	path, err := r.ProfilePath("default", HistoryFile)
	if err != nil {
		t.Fatal(err)
	}
	head, err := app.Invoke(ctx, httpx.InvokeRequest{Method: "HEAD", Path: "/api/stats/overview", JSON: map[string]int{"nodes": 999}})
	if err != nil || head.Status != 200 || len(head.Body) != 0 {
		t.Fatal(head, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("HEAD created history", err)
	}
	get, err := app.Invoke(ctx, httpx.InvokeRequest{Method: "GET", Path: "/api/stats/overview"})
	if err != nil || get.Status != head.Status {
		t.Fatal(get, err)
	}
	for _, key := range []string{"Content-Type", "Cache-Control"} {
		if get.Header.Get(key) != head.Header.Get(key) {
			t.Fatal(key)
		}
	}
	before, _ := os.ReadFile(path)
	stat, _ := os.Stat(path)
	head, err = app.Invoke(ctx, httpx.InvokeRequest{Method: "HEAD", Path: "/api/stats/overview"})
	after, _ := os.ReadFile(path)
	next, _ := os.Stat(path)
	if err != nil || head.Status != 200 || !bytes.Equal(before, after) || !os.SameFile(stat, next) || !stat.ModTime().Equal(next.ModTime()) {
		t.Fatal("HEAD replaced history", err)
	}
}

func TestSamplingOwnerRuntimeAndCancellationBeforeCommit(t *testing.T) {
	for _, mode := range []string{"disconnect", "shutdown", "shutdown after observation"} {
		t.Run(mode, func(t *testing.T) {
			r, ctx := statsRepository(t)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond})
			runtime.Start(context.Background())
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, cancel := context.WithCancel(runtime.BindRequest(ctx))
			defer cancel()
			if mode == "shutdown" {
				runtime.Shutdown(context.Background())
			} else {
				cancel()
			}
			h := NewHandler(r)
			h.Now = func() time.Time {
				if mode == "shutdown after observation" {
					runtime.Shutdown(context.Background())
				}
				return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
			}
			_, err := h.recordHistory(request, "default", Counts{Nodes: 1}, true)
			path, _ := r.ProfilePath("default", HistoryFile)
			_, statErr := os.Stat(path)
			if mode == "disconnect" {
				if err != nil || statErr != nil {
					t.Fatal("disconnect aborted sample", err, statErr)
				}
			} else if !errors.Is(err, context.Canceled) || !os.IsNotExist(statErr) {
				t.Fatal("shutdown wrote sample", err, statErr)
			}
		})
	}
}

func TestSamplingBoundsCorruptionAndConcurrentReaders(t *testing.T) {
	r, ctx := statsRepository(t)
	clock := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	h := NewHandler(r)
	h.Now = func() time.Time { return clock }
	for range HistorySize + 5 {
		if _, err := h.recordHistory(ctx, "default", Counts{Nodes: 1}, true); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(SampleInterval)
	}
	path, err := r.ProfilePath("default", HistoryFile)
	if err != nil {
		t.Fatal(err)
	}
	readSamples := func() []any {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Samples []any `json:"samples"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Samples
	}
	if len(readSamples()) != HistorySize {
		t.Fatal("history not bounded")
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := h.recordHistory(ctx, "default", Counts{Nodes: 2}, true); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(readSamples()) != HistorySize {
		t.Fatal("concurrent samples lost or grew")
	}
	if err := os.WriteFile(path, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.recordHistory(ctx, "default", Counts{Nodes: 3}, true); err != nil {
		t.Fatal("corrupt optional history prevented overview", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := h.recordHistory(ctx, "default", Counts{Nodes: 4}, true); err != nil {
		t.Fatal("optional history write failure prevented overview", err)
	}
}

func TestHistoryViewObeysFileReplacementAndSamplingTime(t *testing.T) {
	r, ctx := statsRepository(t)
	defer r.Close()
	h := NewHandler(r)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h.Now = func() time.Time { return now }
	path, err := r.ProfilePath("default", HistoryFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	replace := func(n string) {
		t.Helper()
		data := []byte(`{"samples":[{"nodes":` + n + `},{"at":"2026-01-01T00:00:00Z","nodes":2}]}`)
		if err := os.WriteFile(path+".new", data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".new", path); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want, tail string, length int, persist bool) {
		t.Helper()
		series, err := h.recordHistory(ctx, "default", Counts{Nodes: 5}, persist)
		if err != nil {
			t.Fatal(err)
		}
		got := series["nodes"]
		if len(got) != length || got[0].String() != want || got[length-1].String() != tail {
			t.Fatalf("unexpected series: %v", got)
		}
	}
	replace("1")
	check("1", "5", 2, true)
	check("1", "5", 2, true)
	replace("9")
	check("9", "5", 2, true)
	now = now.Add(SampleInterval)
	check("9", "5", 3, false)
	check("9", "5", 3, true)
	if got, err := os.ReadFile(path); err != nil || !bytes.Contains(got, []byte("00:30:00")) {
		t.Fatal("due GET failed to persist", err)
	}
}
