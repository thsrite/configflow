package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadTransportErrorPreservesIdentityWithoutLeakingURLCredential(t *testing.T) {
	want := errors.New("download transport failure")
	old := http.DefaultTransport
	http.DefaultTransport = updateRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, want
	})
	t.Cleanup(func() { http.DefaultTransport = old })

	err := downloadFile(filepath.Join(t.TempDir(), "agent"), "https://example.test/?token=download-secret")
	if !errors.Is(err, want) {
		t.Fatalf("errors.Is(err, want) = false: %v", err)
	}
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Fatalf("errors.As(err, *url.Error) = false: %v", err)
	}
	if got := err.Error(); strings.Contains(got, "download-secret") || strings.Contains(got, "token=download-secret") {
		t.Fatalf("download URL credential leaked in error: %s", got)
	}
}

type updateRoundTripFunc func(*http.Request) (*http.Response, error)

func (f updateRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUpdateBusyNamesRunningRuleUpdate(t *testing.T) {
	cfg := &Config{ConfigPath: filepath.Join(t.TempDir(), "config.yaml")}
	release, err := AcquireServiceOperation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	previousWait := updateLockWait
	updateLockWait = 200 * time.Millisecond
	defer func() { updateLockWait = previousWait }()
	submit := func() map[string]any {
		recorder := httptest.NewRecorder()
		HandleUpdate(cfg)(recorder, httptest.NewRequest(http.MethodPost, "/api/update", strings.NewReader(`{}`)))
		if recorder.Code != http.StatusConflict {
			t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	// Deployment retries match this exact text; other holders must keep it.
	if body := submit(); body["message"] != errDeploymentBusy.Error() {
		t.Fatalf("generic busy message changed: %v", body)
	}
	oxRuleUpdatesRunning.Add(1)
	defer oxRuleUpdatesRunning.Add(-1)
	if body := submit(); body["message"] != errRuleUpdateBusy.Error() {
		t.Fatalf("rule update not named: %v", body)
	}
}
