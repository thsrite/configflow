//go:build configflow_proddiff

package agents

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
)

// TestProddiffContentWaits checks actual slow Checker work at every observed route.
func TestProddiffContentWaits(t *testing.T) {
	for _, path := range []string{"/api/agents", "/api/agents/content-agent", "/api/agents/content-agent/push-diff"} {
		t.Run(path, func(t *testing.T) {
			f := newContentHTTP(t, true, false)
			f.gate = make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(f.gate) }) }
			defer release()
			NewHandler(Deps{Repo: f.s.Repo, Agents: f.manager}).InstallProddiffContentObserver(f.s.App)
			done := make(chan *httpxtest.Result, 1)
			go func() { done <- f.get(path) }()
			deadline := time.Now().Add(5 * time.Second)
			for f.calls.Load() == 0 {
				if time.Now().After(deadline) {
					t.Fatal("observer never scheduled real preparation")
				}
				time.Sleep(time.Millisecond)
			}
			select {
			case <-done:
				t.Fatal("observer returned the transient checking response")
			case <-time.After(25 * time.Millisecond):
			}
			release()
			select {
			case reply := <-done:
				reply.WantStatus(200)
				if reply.Contains(`"checking"`) || !reply.Contains(`"synced"`) || reply.Header.Get(contentObserverHeader) != "" {
					t.Fatal("observer did not return the real stable result")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("observer did not return after preparation finished")
			}
		})
	}
}

// TestProddiffContentTimeout checks the real bound with a Runtime fake clock.
func TestProddiffContentTimeout(t *testing.T) {
	clock := lifecycletest.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	runtime := lifecycle.NewRuntime(lifecycle.Options{Clock: clock, Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	defer runtime.Shutdown(context.Background())
	checking := func() *httpx.Response { return httpx.JSON(map[string]string{"status": "checking"}) }
	done := make(chan error, 1)
	go func() {
		_, err := observeContent(runtime.BindRequest(context.Background()), checking(), contentObserverTimeout, func() (*httpx.Response, error) { return checking(), nil })
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for clock.Pending() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("observer did not arm its bounded timer")
		}
		time.Sleep(time.Millisecond)
	}
	clock.Advance(120 * time.Second)
	select {
	case err := <-done:
		if !errors.Is(err, errContentObservation) {
			t.Fatal("timeout became an ordinary comparison result")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("observer exceeded its deadline")
	}
	failure := contentObservationFailure()
	if failure.Status != 503 || failure.Header().Get(contentObserverHeader) != "content-sync-timeout" {
		t.Fatal("harness timeout marker was lost")
	}
}

// TestAuditProddiffObservationExactNames keeps status extensions from changing
// the tagged test observer's wait decision.
func TestAuditProddiffObservationExactNames(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		checking  bool
	}{
		{"exact", `{"status":"checking"}`, true},
		{"case-only", `{"Status":"checking"}`, false},
		{"mixed", `{"status":"complete","STATUS":"checking"}`, false},
		{"nested", `[{"content_sync":{"STATUS":"checking"}}]`, false},
		{"exact-nested", `[{"content_sync":{"status":"checking"}}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httpx.Bytes(200, "application/json", []byte(tc.raw))
			if got := contentChecking(response); got != tc.checking {
				t.Fatalf("checking=%v want %v", got, tc.checking)
			}
		})
	}
}
