package stats

import (
	"bytes"
	"context"
	"errors"
	"os"
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
	return r, repo.WithScope(repo.WithRepository(context.Background(), r))
}

func TestHEADHasGETHeadersAndNeverCreatesOrReplacesHistory(t *testing.T) {
	r, ctx := statsRepository(t)
	app := httpx.New()
	Register(app.Router)
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
			previous := Now
			t.Cleanup(func() { Now = previous })
			Now = func() time.Time {
				if mode == "shutdown after observation" {
					runtime.Shutdown(context.Background())
				}
				return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
			}
			_, err := recordHistory(request, Counts{Nodes: 1}, true)
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
	previous := Now
	t.Cleanup(func() { Now = previous })
	clock := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	Now = func() time.Time { return clock }
	for range HistorySize + 5 {
		if _, err := recordHistory(ctx, Counts{Nodes: 1}, true); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(SampleInterval)
	}
	saved, err := r.ReadProfileJSON("default", HistoryFile)
	if err != nil || len(saved.GetList("samples")) != HistorySize {
		t.Fatal("history not bounded", err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := recordHistory(ctx, Counts{Nodes: 2}, true); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	saved, err = r.ReadProfileJSON("default", HistoryFile)
	if err != nil || len(saved.GetList("samples")) != HistorySize {
		t.Fatal("concurrent samples lost or grew", err)
	}
	path, _ := r.ProfilePath("default", HistoryFile)
	if err := os.WriteFile(path, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := recordHistory(ctx, Counts{Nodes: 3}, true); err != nil {
		t.Fatal("corrupt optional history prevented overview", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := recordHistory(ctx, Counts{Nodes: 4}, true); err != nil {
		t.Fatal("optional history write failure prevented overview", err)
	}
}
