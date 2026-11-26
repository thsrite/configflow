package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
