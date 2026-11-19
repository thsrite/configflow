package aggregations_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	generation "configflow/internal/aggregations"
	"configflow/internal/api/aggregations"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/provider"
	"configflow/internal/refresh"
)

func startDeliveryRuntime(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 2 * time.Second})
		if err == nil {
			break
		}
		if !errors.Is(err, lifecycle.ErrRunning) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	lifecycle.Start(ctx)
	t.Cleanup(func() {
		lifecycle.Stop(3 * time.Second)
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error(err)
		}
	})
}

func TestRouteFallbackCoalescesAndFinishesAfterCancellation(t *testing.T) {
	generation.Wire()
	mihomoenv.Wire()
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(aggregations.Register)})
	ctx := srv.Context()
	_, err := srv.Repo.SaveShared(ctx, jsonx.Obj("nodes", []any{}, "subscriptions", []any{jsonx.Obj("id", "s", "name", "Feed", "url", "http://127.0.0.1:9/feed")}, "subscription_aggregations", []any{jsonx.Obj("id", "a", "name", "Aggregate", "nodes", []any{}, "subscriptions", []any{"s"})}), nil)
	if err != nil {
		t.Fatal(err)
	}
	startDeliveryRuntime(t, ctx)
	// Before any served copy exists, fallback must use the aggregation artifact.
	_, err = srv.Repo.WriteProfileText(ctx, "default", "providers/a.yaml", []byte("proxies: [{name: LastGood}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	started, secondStarted, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	var calls atomic.Int32
	fetch := func(ctx context.Context, id, url string) (*refresh.SourceResult, error) {
		if calls.Add(1) == 2 {
			close(secondStarted)
		}
		once.Do(func() { close(started) })
		<-release
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return &refresh.SourceResult{Text: "proxies: [{name: Remote, type: http, server: good.test, port: 80}]", Source: "rendered_yaml"}, nil
	}
	t.Cleanup(provider.SetDeliveryEnv(&provider.DeliveryEnv{AggregationFetch: fetch}))
	t.Cleanup(provider.SetResponseSeconds(0))
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	login := srv.Login()
	rejected := srv.Get("/api/profiles/default/aggregations/a/provider?token=bad")
	if rejected.Status != 401 || calls.Load() != 0 {
		t.Fatalf("auth reached render: %d calls=%d", rejected.Status, calls.Load())
	}
	// Zero response deadline makes the fallback deterministic, without sleeps.
	for range 2 {
		reply := srv.Get("/api/aggregations/a/provider", login)
		if reply.Status != 200 || reply.Header.Get("X-ConfigFlow-Provider") != "last-good" || !strings.Contains(reply.Text(), "LastGood") {
			t.Fatalf("fallback: status=%d headers=%v body=%s", reply.Status, reply.Header, reply.Body)
		}
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("render did not start")
	}
	if calls.Load() != 1 {
		t.Fatalf("same delivery fetched %d times", calls.Load())
	}
	// A disconnected waiter stops waiting without cancelling its render or
	// receiving a Mihomo-format last-good response for the Surge key.
	request := httptest.NewRequest(http.MethodGet, "/api/aggregations/a/provider?format=surge", nil)
	login(request)
	cancelled, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(cancelled)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); srv.Handler().ServeHTTP(response, request) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled waiter did not finish")
	}
	if response.Code != 500 || response.Header().Get("X-ConfigFlow-Provider") == "last-good" {
		t.Fatalf("cancelled Surge response: %d %s", response.Code, response.Body.String())
	}
	select {
	case <-secondStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled waiter prevented its render")
	}
	releaseOnce.Do(func() { close(release) })
	surge := srv.Get("/api/aggregations/a/provider?format=surge", login)
	if surge.Status != 200 || !strings.Contains(surge.Text(), "Remote") || surge.Header.Get("X-ConfigFlow-Provider") != "" {
		t.Fatalf("live Surge waiter: %d %s", surge.Status, surge.Body)
	}
	// Await background publication before restoring collaborators or removing data.
	path, err := srv.Repo.ProfilePath("default", "providers/served/aggregation-a.yaml")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "Remote") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background publication missing: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	// Render is now fast. Wait for the final provider flight to finish as well.
	provider.SetResponseSeconds(60)
	reply := srv.Get("/api/aggregations/a/provider", login)
	if reply.Status != 200 || !strings.Contains(reply.Text(), "Remote") {
		t.Fatalf("completed response: %d %s", reply.Status, reply.Body)
	}
}
