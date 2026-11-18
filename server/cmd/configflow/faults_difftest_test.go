//go:build configflow_difftest && !configflow_proddiff

package main

import (
	"configflow/internal/faultprobe"
	"configflow/internal/httpx"
	"configflow/internal/repo"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFaultStateRequiresLoopbackAndPrivateToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(faultprobe.ControlEnv, dir)
	app := httpx.New()
	if err := writeTestManifest(app); err != nil {
		t.Fatal(err)
	}
	repository, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, remote, token string
		status              int
	}{
		{"ipv4", "127.0.0.1:1", token, 200}, {"ipv6", "[::1]:1", token, 200},
		{"missing", "127.0.0.1:1", "", 401}, {"wrong", "127.0.0.1:1", "wrong", 401},
		{"remote", "192.0.2.1:1", token, 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/__faults/state", nil).WithContext(repo.WithRepository(t.Context(), repository))
			req.RemoteAddr = tt.remote
			req.Header.Set(faultprobe.TokenHeader, tt.token)
			out := httptest.NewRecorder()
			app.ServeHTTP(out, req)
			if out.Code != tt.status {
				t.Fatalf("status %d: %s", out.Code, out.Body.String())
			}
			if strings.Contains(out.Body.String(), token) {
				t.Fatal("token leaked")
			}
			if tt.status == 200 {
				var state map[string]json.RawMessage
				if err := json.Unmarshal(out.Body.Bytes(), &state); err != nil {
					t.Fatal(err)
				}
				if len(state) != 2 || state["goroutines"] == nil || state["phases"] == nil {
					t.Fatalf("unexpected diagnostic fields: %s", out.Body.String())
				}
			}
		})
	}
}
