package clientdevices_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	api "configflow/internal/api/clientdevices"
	"configflow/internal/clients"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

type oracleRequest struct {
	Method   string `json:"method"`
	Path     string `json:"path"`
	Body     any    `json:"body,omitempty"`
	UseToken bool   `json:"use_token,omitempty"`
}
type oracleResponse struct {
	Status  int             `json:"status"`
	Payload json.RawMessage `json:"payload"`
	Cache   string          `json:"cache"`
}

func pythonHTTP(t *testing.T, dir string, config any, requests []oracleRequest) struct {
	Responses []oracleResponse  `json:"responses"`
	Stored    []json.RawMessage `json:"stored"`
} {
	t.Helper()
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	input, _ := json.Marshal(map[string]any{"config": config, "requests": requests})
	cmd := exec.CommandContext(t.Context(), filepath.Join(home, "cf-test/.venv/bin/python"), filepath.Join(wd, "testdata/r17_http_reference.py"))
	cmd.Dir = filepath.Clean(filepath.Join(wd, "../../../.."))
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R17_DATA="+dir)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Flask %v %s", err, stderr.String())
	}
	var result struct {
		Responses []oracleResponse  `json:"responses"`
		Stored    []json.RawMessage `json:"stored"`
	}
	if err = json.Unmarshal(output, &result); err != nil {
		t.Fatal(err, string(output))
	}
	return result
}

var markerPattern = regexp.MustCompile(`^(cf-client:.*):[0-9a-f]{32}$`)

func normalized(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			if key == "token" && v["actions"] != nil && v["success"] == true {
				continue
			}
			if key == "checked_at" && v["sources"] != nil && v["discovered"] != nil {
				if timestamp, ok := item.(string); ok {
					if _, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
						item = "<validated timestamp>"
					}
				}
			}
			if key == "comment" || key == "hostname" {
				if text, ok := item.(string); ok && markerPattern.MatchString(text) {
					item = markerPattern.ReplaceAllString(text, "${1}:<operation-id>")
				}
			}
			out[key] = normalized(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalized(item)
		}
		return out
	}
	return value
}
func compareOracle(t *testing.T, label string, left, right json.RawMessage) {
	t.Helper()
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		t.Fatalf("invalid %s JSON", label)
	}
	if !reflect.DeepEqual(normalized(a), normalized(b)) {
		t.Fatalf("%s differ: %s", label, firstDifference(normalized(a), normalized(b), "$"))
	}
}
func TestR17FlaskHTTPAndStoredState(t *testing.T) {
	f := routerfake.New(nil)
	seed := rosState()
	f.Reset(seed)
	httpServer := httptest.NewServer(f)
	defer httpServer.Close()
	cfg := map[string]any{"device_notes": map[string]string{"192.0.2.21": "Synthetic"}, "routers": map[string]any{"items": []any{map[string]any{"id": "router-one", "name": "Synthetic", "type": "routeros", "endpoint": httpServer.URL, "username": "synthetic-user", "password": "synthetic-pass", "verify_tls": false}}}}
	requests := []oracleRequest{{Method: "GET", Path: "/api/client-devices"}, {Method: "GET", Path: "/api/client-devices/discovery?refresh=1"}, {Method: "HEAD", Path: "/api/client-devices", Body: map[string]any{"name": "not saved"}}, {Method: "PUT", Path: "/api/client-devices", Body: map[string]any{"profile_id": "default", "ip": "192.0.2.21", "name": "Renamed", "mode": "", "previous_name": "Synthetic", "previous_mode": "", "base_url": ""}}, {Method: "PUT", Path: "/api/client-devices", Body: map[string]any{"profile_id": "default", "ip": "192.0.2.21", "name": "stale", "mode": "", "previous_name": "Synthetic", "previous_mode": "", "base_url": ""}}, {Method: "POST", Path: "/api/client-devices/fixed-ip/preview", Body: requestBody("add")}, {Method: "POST", Path: "/api/client-devices/fixed-ip/apply", Body: map[string]string{"profile_id": "default"}, UseToken: true}, {Method: "GET", Path: "/api/client-devices/discovery?refresh=1"}, {Method: "POST", Path: "/api/client-devices/fixed-ip/preview", Body: requestBody("delete")}, {Method: "POST", Path: "/api/client-devices/fixed-ip/apply", Body: map[string]string{"profile_id": "default"}, UseToken: true}, {Method: "POST", Path: "/api/client-devices/fixed-ip/apply", Body: map[string]string{"profile_id": "default", "token": "invalid"}}, {Method: "GET", Path: "/api/client-devices?agent=missing"}, {Method: "GET", Path: "/api/client-devices/discovery?agent=missing"}, {Method: "PUT", Path: "/api/client-devices", Body: []any{}}, {Method: "POST", Path: "/api/client-devices/fixed-ip/preview", Body: map[string]any{}}}
	dir := t.TempDir()
	expected := pythonHTTP(t, dir, cfg, requests)
	pythonState := f.Snapshot()
	f.Reset(seed)
	s := httpxtest.New(t, httpxtest.Options{DataDir: filepath.Join(dir, "go"), SecretKey: "synthetic-fixed-secret", Register: httpxtest.Routes(api.Register)})
	routers.Wire()
	token := ""
	for i, request := range requests {
		t.Run(request.Method+request.Path, func(t *testing.T) {
			body := request.Body
			if request.UseToken {
				body = map[string]string{"profile_id": "default", "token": token}
			}
			options := []httpxtest.Option{}
			if body != nil {
				options = append(options, httpxtest.JSON(body))
			}
			result := s.Do(request.Method, request.Path, options...)
			if result.Status != expected.Responses[i].Status || result.Header.Get("Cache-Control") != expected.Responses[i].Cache {
				t.Fatalf("HTTP Go=%d %s %s Python=%d %s %s", result.Status, result.Header.Get("Cache-Control"), result.Body, expected.Responses[i].Status, expected.Responses[i].Cache, expected.Responses[i].Payload)
			}
			if request.Method != "HEAD" {
				compareOracle(t, "response", result.Body, expected.Responses[i].Payload)
				if strings.HasSuffix(request.Path, "/fixed-ip/preview") && result.Status == 200 {
					token = result.Object().GetString("token")
					if token == "" {
						t.Fatal("preview omitted confirmation token")
					}
					signer := clients.FixedIPToken{Secret: "synthetic-fixed-secret"}
					var python struct {
						Token string `json:"token"`
					}
					_ = json.Unmarshal(expected.Responses[i].Payload, &python)
					if _, err := signer.Verify(python.Token); err != nil {
						t.Fatal("Python HTTP token incompatible", err)
					}
					if _, err := signer.Verify(token); err != nil {
						t.Fatal("Go HTTP token invalid", err)
					}
				}
			}
			stored, err := s.Repo.Profile(s.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(stored)
			compareStored(t, data, expected.Stored[i])
		})
	}
	left, _ := json.Marshal(f.Snapshot().Tables)
	right, _ := json.Marshal(pythonState.Tables)
	compareOracle(t, "device tables", left, right)
}

func firstDifference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	if x, ok := a.(map[string]any); ok {
		y, ok := b.(map[string]any)
		if !ok {
			return path + ": type"
		}
		for k, v := range x {
			if d := firstDifference(v, y[k], path+"."+k); d != "" {
				return d
			}
		}
		return path + ": keys"
	}
	if x, ok := a.([]any); ok {
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return path + ": length/type"
		}
		for i, v := range x {
			if d := firstDifference(v, y[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
	}
	return fmt.Sprintf("%s: Go=%v Python=%v", path, a, b)
}
func compareStored(t *testing.T, left, right json.RawMessage) {
	t.Helper()
	var a, b map[string]json.RawMessage
	_ = json.Unmarshal(left, &a)
	_ = json.Unmarshal(right, &b)
	for _, fields := range []map[string]json.RawMessage{a, b} {
		var timestamp string
		if json.Unmarshal(fields["updated_at"], &timestamp) != nil {
			t.Fatal("missing storage timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			t.Fatal(err)
		}
		delete(fields, "updated_at")
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	compareOracle(t, "stored profile", x, y)
}
