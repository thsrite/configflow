package clientdevices_test

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	api "configflow/internal/api/clientdevices"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

// TestReviewR17ValidationOrderMatchesFlask sends requests that violate more
// than one rule. Flask reports the first rule in its own order (and compares a
// non-string profile_id as a profile switch); Go must report the same class
// and message, not the first JSON type error it meets.
func TestReviewR17ValidationOrderMatchesFlask(t *testing.T) {
	f := routerfake.New(nil)
	seed := rosState()
	f.Reset(seed)
	device := httptest.NewServer(f)
	defer device.Close()
	cfg := map[string]any{"device_notes": map[string]string{"192.0.2.21": "Synthetic"}, "routers": map[string]any{"items": []any{map[string]any{"id": "router-one", "name": "Synthetic", "type": "routeros", "endpoint": device.URL, "username": "synthetic-user", "password": "synthetic-pass", "verify_tls": false}}}}
	valid := map[string]any{"ip": "192.0.2.21", "mac": "02:00:00:00:00:21", "router_id": "router-one", "name": "Synthetic"}
	requests := []oracleRequest{
		// Address is checked before the mode's type.
		{Method: "PUT", Path: "/api/client-devices", Body: map[string]any{"profile_id": "default", "ip": "bad", "name": "x", "mode": 5, "previous_name": "Synthetic", "previous_mode": "", "base_url": ""}},
		// previous_name is checked before the mode's type.
		{Method: "PUT", Path: "/api/client-devices", Body: map[string]any{"profile_id": "default", "ip": "192.0.2.21", "name": "x", "mode": 5, "previous_name": 5, "previous_mode": "", "base_url": ""}},
		// A non-string profile_id is a profile switch (409), not a malformed batch.
		{Method: "POST", Path: "/api/client-devices/fixed-ip/preview", Body: map[string]any{"profile_id": 1, "operation": "add", "devices": []any{valid}}},
		// Identity is checked before the name's type.
		{Method: "POST", Path: "/api/client-devices/fixed-ip/preview", Body: map[string]any{"profile_id": "default", "operation": "add", "devices": []any{map[string]any{"ip": "bad", "mac": "02:00:00:00:00:21", "router_id": "router-one", "name": 5}}}},
		// A duplicate IP is reported before the second row's name type.
		{Method: "POST", Path: "/api/client-devices/fixed-ip/preview", Body: map[string]any{"profile_id": "default", "operation": "add", "devices": []any{valid, map[string]any{"ip": "192.0.2.21", "mac": "02:00:00:00:00:22", "router_id": "router-one", "name": nil}}}},
		// Control: both report the profile switch for a non-string apply profile.
		{Method: "POST", Path: "/api/client-devices/fixed-ip/apply", Body: map[string]any{"profile_id": 1, "token": "x"}},
	}
	dir := t.TempDir()
	expected := pythonHTTP(t, dir, cfg, requests)
	f.Reset(seed)
	s := httpxtest.New(t, httpxtest.Options{DataDir: filepath.Join(dir, "go"), SecretKey: "synthetic-fixed-secret", Register: httpxtest.Routes(api.Register)})
	previous := repo.SetHooks(repo.Hooks{RecordHooks: repo.RecordHooks{JobLockRecord: routers.JobLockRecord}})
	t.Cleanup(func() { repo.SetHooks(previous) })
	for i, request := range requests {
		t.Run(request.Method+request.Path, func(t *testing.T) {
			result := s.Do(request.Method, request.Path, httpxtest.JSON(request.Body))
			if result.Status != expected.Responses[i].Status {
				t.Fatalf("status Go=%d %s Python=%d %s", result.Status, result.Body, expected.Responses[i].Status, expected.Responses[i].Payload)
			}
			compareOracle(t, "response", result.Body, expected.Responses[i].Payload)
		})
	}
	if len(f.Calls()) != 0 {
		t.Fatal("validation failure contacted the router")
	}
}
