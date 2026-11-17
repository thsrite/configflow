package substore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
)

type refusedUpdateSpawner struct{}

func (refusedUpdateSpawner) Spawn(context.Context, string, func(context.Context)) error {
	return lifecycle.ErrStopping
}

type updateClientFunc func(context.Context, *Request) (*Response, error)

func (fn updateClientFunc) Do(ctx context.Context, request *Request) (*Response, error) {
	return fn(ctx, request)
}

func TestUpdateRefusedSpawnReleasesBusyState(t *testing.T) {
	h := newDepsHarness(t)
	h.store.setVersion("2.40.0")
	h.release = `{"tag_name":"v2.41.0","assets":[{"name":"sub-store.bundle.js","browser_download_url":"https://example.com/bundle.js"}]}`
	h.d.Spawner = refusedUpdateSpawner{}
	if ok, message := h.d.StartSubStoreUpdate(h.ctx); ok || message != msgUpdateUnavailable {
		t.Fatal(ok, message)
	}
	if state := h.d.GetUpdateState(); state.GetString("state") != "failed" {
		t.Fatal(state)
	}
	h.unlocked(t)
}

func TestTrackedUpdateIgnoresDisconnectAndCancelsHTTPOnShutdown(t *testing.T) {
	h := newDepsHarness(t)
	h.store.setVersion("2.40.0")
	entered, cancelled, force := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var enterOnce, cancelOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		enterOnce.Do(func() { close(entered) })
		select {
		case <-req.Context().Done():
			cancelOnce.Do(func() { close(cancelled) })
		case <-force:
		}
	}))
	defer server.Close()
	defer close(force)
	asset, _ := json.Marshal(server.URL + "/bundle.js")
	h.release = fmt.Sprintf(`{"tag_name":"v2.41.0","assets":[{"name":"sub-store.bundle.js","browser_download_url":%s}]}`, asset)
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	runner.Start(h.ctx)
	defer runner.Shutdown(context.Background())
	h.d.Spawner = runner
	request, cancel := context.WithCancel(h.ctx)
	cancel()
	if ok, message := h.d.StartSubStoreUpdate(request); !ok {
		t.Fatal(message)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("download not started")
	}
	if ok, message := h.d.StartSubStoreUpdate(h.ctx); ok || message != msgUpdateBusy {
		t.Fatal("update not exclusive", ok, message)
	}
	runner.Shutdown(context.Background())
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("job cancellation did not reach HTTP")
	}
	wait, stop := context.WithTimeout(h.ctx, 5*time.Second)
	defer stop()
	if err := runner.WaitIdle(wait); err != nil {
		t.Fatal(err)
	}
	if state := h.d.GetUpdateState(); state.GetString("state") == "running" {
		t.Fatal("cancelled update still running", state)
	}
	h.unlocked(t)
	if lockx.Violations() != 0 {
		t.Fatal("lock order violation")
	}
}

func TestUpdateProductionSizeAndTimeoutsAreNotOverridden(t *testing.T) {
	h := newDepsHarness(t)
	if h.d.MinBundleSize != 500*1024 {
		t.Fatal("production minimum size changed", h.d.MinBundleSize)
	}
	var observed Timeout
	h.d.env.HTTP = updateClientFunc(func(_ context.Context, request *Request) (*Response, error) {
		observed = request.Timeout
		return &Response{StatusCode: 200, Body: []byte("small")}, nil
	})
	if err := h.d.downloadBundle(h.ctx, jsonx.Obj("download_url", "https://example.com/bundle.js"), filepath.Join(t.TempDir(), "bundle.js")); err == nil {
		t.Fatal("production size bound bypassed")
	}
	if observed != (Timeout{Connect: 15 * time.Second, Read: 120 * time.Second}) {
		t.Fatal("production download timeout changed", observed)
	}
}

func TestExecRunnerKeepsRuntimeCancellation(t *testing.T) {
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	runner.Start(context.Background())
	defer runner.Shutdown(context.Background())
	entered, done := make(chan struct{}), make(chan struct{})
	if err := runner.Spawn(context.Background(), "exec-cancel", func(ctx context.Context) {
		defer close(done)
		close(entered)
		result, err := (ExecRunner{}).Run(ctx, []string{"sh", "-c", "exec sleep 30"}, 5*time.Second)
		if err == nil && result.ReturnCode == 0 {
			t.Error("cancelled child succeeded")
		}
	}); err != nil {
		t.Fatal(err)
	}
	<-entered
	runner.Shutdown(context.Background())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("exec ignored Runtime cancellation")
	}
}
