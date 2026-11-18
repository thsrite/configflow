package routers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/routers/routerfake"
	"configflow/internal/rulefetch"
)

type r14Transport struct{ requests []rulefetch.TransportRequest }

func (s *r14Transport) Do(ctx context.Context, r rulefetch.TransportRequest) (*rulefetch.Response, error) {
	s.requests = append(s.requests, r)
	return (rulefetch.HTTPTransport{}).Do(ctx, r)
}
func TestR14DownloadsProductionParameters(t *testing.T) {
	calls := 0
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != "GET" || len(body) > 0 {
			t.Errorf("raw download %s %q", r.Method, body)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing client user agent")
		}
		_, _ = io.WriteString(w, "DOMAIN,example.com\n")
	}))
	defer httpServer.Close()
	spy := &r14Transport{}
	downloads := RuleSyncDownloads{Client: &rulefetch.Client{Transport: spy, Sleep: func(time.Duration) { t.Error("unexpected retry") }}}
	source := mustPolicyJSON(map[string]string{"id": "one", "url": httpServer.URL, "behavior": "classical", "format": "text", "name": "Synthetic"})
	config := json.RawMessage(`{"system_config":{}}`)
	for i := 0; i < 2; i++ {
		if _, err := downloads.Download(t.Context(), source, config); err != nil {
			t.Fatal(err)
		}
	}
	other := mustPolicyJSON(map[string]string{"id": "two", "url": httpServer.URL, "behavior": "ipcidr", "format": "text", "name": "Different declaration"})
	if _, err := downloads.Download(t.Context(), other, config); err == nil {
		t.Fatal("shared cached transport bypassed declaration validation")
	}
	if calls != 1 || len(spy.requests) != 1 {
		t.Fatalf("download cache misses %d %+v", calls, spy.requests)
	}
	r := spy.requests[0]
	if r.ConnectTimeout != 5*time.Second || r.ReadTimeout != 10*time.Second || r.Proxy != "" {
		t.Fatalf("production options changed %+v", r)
	}
	if _, err := downloads.Download(t.Context(), source, json.RawMessage(`{"system_config":{"unused":true}}`)); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("config-scoped cache key did not change")
	}
	fresh := RuleSyncDownloads{Client: downloads.Client}
	if _, err := fresh.Download(t.Context(), source, config); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatal("download cache escaped its round")
	}
}
func TestR14RawDeviceDelta(t *testing.T) {
	f := routerfake.New(nil)
	f.Reset(r08State())
	writes := 0
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		user, password, ok := r.BasicAuth()
		if !ok || user != "synthetic-user" || password != "synthetic-password" {
			t.Error("wrong raw authorization")
		}
		if r.Method == "GET" || r.Method == "DELETE" {
			if len(body) != 0 {
				t.Errorf("raw %s body %q", r.Method, body)
			}
		}
		if r.Method == "PUT" {
			writes++
			want := `{"comment":"cf-Synthetic","disabled":"false","dst-address":"198.51.100.0/25","gateway":"192.0.2.3"}`
			if string(body) != want || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("raw request %s %+v", body, r.Header)
			}
		}
		f.ServeHTTP(w, r)
	}))
	defer httpServer.Close()
	d := NewRouterOS(httpServer.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	old := r08Desired(false)
	old.ProxyCIDRs = old.ProxyCIDRs[:1]
	static, err := syncStaticResources(t.Context(), d, old)
	if err != nil {
		t.Fatal(err)
	}
	next := r08Desired(false)
	plan := r14Plan(t, t.Context(), d, r10Router(), next)
	outcome, err := ApplyRuleSyncDelta(t.Context(), d, next, plan, &RuleSyncBaseline{Desired: old, StaticResources: static}, nil)
	if err != nil || outcome.Added != 1 || writes != 1 {
		t.Fatalf("raw delta %+v writes=%d %v", outcome, writes, err)
	}
}
func TestR14RuntimeCancelsDeviceIO(t *testing.T) {
	service, f, router := r14Fixture(t)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/rest/ip/route" {
			once.Do(func() { close(entered) })
			<-r.Context().Done()
			close(cancelled)
			return
		}
		f.ServeHTTP(w, r)
	}))
	defer server.Close()
	service.NewDriver = func(r *Router) *RouterOS { return NewRouterOS(server.URL, r.Username, r.Password, r.VerifyTLS) }
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
	runtime.Start(t.Context())
	defer runtime.Shutdown(t.Context())
	result := make(chan error, 1)
	if err := runtime.Spawn(runtime.BindRequest(t.Context()), "r14-device", func(ctx context.Context) { _, err := service.Sync(ctx, "default", router.ID, "manual"); result <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("device read never started")
	}
	shutdown, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	runtime.Shutdown(shutdown)
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("Runtime did not cancel device HTTP")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("sync cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("device owner did not finish")
	}
	receipt, err := service.store().Read(t.Context(), "default", router.ID)
	if err != nil || receipt.Status != "running" {
		t.Fatalf("shutdown committed local completion %+v %v", receipt, err)
	}
	if len(r08Writes(t, f.Calls()).([]any)) > 0 {
		t.Fatal("cancelled inspection wrote device")
	}
}

func TestR14RuntimeReceiptLeafWait(t *testing.T) {
	for _, mode := range []string{"shutdown", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			store := RuleSyncStore{DataDir: t.TempDir()}
			if err := store.Write(t.Context(), "plain", "r", RuleSyncReceipt{Status: "waiting"}); err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
			runtime.Start(t.Context())
			defer runtime.Shutdown(t.Context())
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			_, unlock, err := syncReceiptLocks.Lock(t.Context(), store.path("plain", "r"), 0)
			if err != nil {
				t.Fatal(err)
			}
			held := true
			defer func() {
				if held {
					unlock()
				}
			}()
			entered, result := make(chan struct{}), make(chan error, 1)
			if err = runtime.Spawn(runtime.BindRequest(request), "r14-leaf", func(ctx context.Context) {
				close(entered)
				result <- store.Write(ctx, "plain", "r", RuleSyncReceipt{Status: "synced"})
			}); err != nil {
				t.Fatal(err)
			}
			<-entered
			if mode == "shutdown" {
				shutdown, stop := context.WithTimeout(t.Context(), 50*time.Millisecond)
				runtime.Shutdown(shutdown)
				stop()
			} else {
				cancel()
			}
			unlock()
			held = false
			select {
			case err = <-result:
			case <-time.After(5 * time.Second):
				t.Fatal("receipt wait did not finish")
			}
			got, readErr := store.Read(t.Context(), "plain", "r")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "shutdown" {
				if !errors.Is(err, context.Canceled) || got.Status != "waiting" {
					t.Fatalf("post-shutdown commit %+v %v", got, err)
				}
			} else if err != nil || got.Status != "synced" {
				t.Fatalf("disconnect dropped receipt %+v %v", got, err)
			}
		})
	}
}
func TestR14JobAndScheduler(t *testing.T) {
	s, f, r := r14Fixture(t)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
	runtime.Start(t.Context())
	defer runtime.Shutdown(t.Context())
	s.Spawner = runtime
	request, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := runtime.BindRequest(request)
	entered, finish := make(chan struct{}), make(chan struct{})
	s.Download = func(ctx context.Context, _ json.RawMessage, _ json.RawMessage) (string, error) {
		close(entered)
		select {
		case <-finish:
			return r14New, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if err := s.StartJob(ctx, "default", r.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("queued owner did not start")
	}
	cancel()
	if err := s.StartJob(t.Context(), "missing", "missing"); err == nil || err.Error() != messageSyncWorkerBusy {
		t.Fatalf("worker lock must precede lookup %v", err)
	}
	_, release, lockErr := RouterJobLock(t.Context(), "other-profile", "clone", r)
	if lockErr == nil {
		release()
		t.Fatal("connection clone escaped job lock")
	}
	if err := s.RunRound(t.Context()); err != nil {
		t.Fatal(err)
	}
	close(finish)
	if err := runtime.WaitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.store().Read(t.Context(), "default", r.ID)
	if err != nil || receipt.Status != "synced" {
		t.Fatalf("queued owner %+v %v", receipt, err)
	}
	downloads := 0
	s.Download = func(context.Context, json.RawMessage, json.RawMessage) (string, error) {
		downloads++
		return r14New, nil
	}
	if err = s.RunRound(t.Context()); err != nil {
		t.Fatal(err)
	}
	if downloads != 0 {
		t.Fatal("not-due round downloaded")
	}
	orphan := receipt
	orphan.Status = "queued"
	if err = s.store().Write(t.Context(), "default", r.ID, orphan); err != nil {
		t.Fatal(err)
	}
	if err = s.RunRound(t.Context()); err != nil {
		t.Fatal(err)
	}
	if downloads != 1 {
		t.Fatal("orphan queued receipt not resumed")
	}
	_, release, lockErr = RouterJobLock(t.Context(), "default", r.ID, r)
	if lockErr != nil {
		t.Fatal("job lock leaked")
	}
	release()
	if len(r08Writes(t, f.Calls()).([]any)) > 0 {
		t.Fatal("scheduler no-op wrote")
	}
	if entries, err := os.ReadDir(s.Repo.DataDir() + "/router-rule-sync"); err != nil || len(entries) != 1 {
		t.Fatalf("legacy lock files created: %+v %v", entries, err)
	}
}

func TestR14DownloadSafetyBounds(t *testing.T) {
	for _, mode := range []string{"size", "utf8"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "size" {
					_, _ = io.WriteString(w, strings.Repeat("x", syncMaxBytes+1))
				} else {
					_, _ = w.Write([]byte{0xff})
				}
			}))
			defer server.Close()
			downloads := RuleSyncDownloads{}
			source := mustPolicyJSON(map[string]string{"id": "synthetic", "url": server.URL, "behavior": "classical", "format": "text"})
			_, err := downloads.Download(t.Context(), source, json.RawMessage(`{"system_config":{}}`))
			if err == nil {
				t.Fatal("unsafe downloaded body accepted")
			}
			want := messageSyncDownloadLimit
			if mode == "utf8" {
				want = messageSyncDownloadUTF8
			}
			if err.Error() != want {
				t.Fatalf("download bound %v", err)
			}
			if len(downloads.items) > 0 {
				t.Fatal("unsafe response cached")
			}
		})
	}
}

func TestR14StoreWriteDetachesRequest(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
	runtime.Start(t.Context())
	defer runtime.Shutdown(t.Context())
	request, cancel := context.WithCancel(t.Context())
	ctx := runtime.BindRequest(request)
	cancel()
	store := RuleSyncStore{DataDir: t.TempDir()}
	if err := store.Write(ctx, "plain", "r", RuleSyncReceipt{Status: "synced"}); err != nil {
		t.Fatalf("request disconnect cancelled owner: %v", err)
	}
	runtime.Shutdown(t.Context())
	if err := store.Write(ctx, "plain", "r", RuleSyncReceipt{Status: "failed"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown owner: %v", err)
	}
}
