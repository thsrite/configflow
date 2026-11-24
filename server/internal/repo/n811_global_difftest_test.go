package repo_test

import (
	"bufio"
	"bytes"
	"configflow/internal/testjson"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/repo/repotest"
	"configflow/internal/server"
)

type globalRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Body    *string           `json:"body"`
	Headers map[string]string `json:"headers"`
}
type globalAnswer struct {
	Status int `json:"status"`
	Body   any `json:"body"`
	State  struct {
		Document any `json:"document"`
	} `json:"state"`
	Error string `json:"oracle_error"`
}

// TestN811GlobalFlaskBoundaries reuses the existing real Flask oracle without
// altering its fixtures. It covers the three global routes and their stored data.
func TestN811GlobalFlaskBoundaries(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint("ownership-conflict=", conflict), func(t *testing.T) {
			const secret = "n811-synthetic-jwt-key"
			srv := httpxtest.New(t, httpxtest.Options{RepoOptions: server.RepoOptions(), Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: secret})
			full, err := server.New(srv.Context(), server.Options{Repo: srv.Repo})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(full.Agents.Close)
			*srv.App = *full.HTTP
			ctx := srv.Context()
			if err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
				s.SystemConfig.ConfigToken = "n811-config-secret"
				s.SystemConfig.RuleProxyToken = "n811-rule-secret"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if conflict {
				connection := json.RawMessage(`{"id":"router","name":"Synthetic","type":"routeros","endpoint":"http://192.0.2.1","username":"fixture","password":"secret","verify_tls":true}`)
				var router model.Router
				if err := json.Unmarshal(connection, &router); err != nil {
					t.Fatal(err)
				}
				if _, err := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error { p.Routers.Items = []model.Router{router}; return nil }); err != nil {
					t.Fatal(err)
				}
				if err := srv.Repo.SaveRouterPublicationJSON(ctx, "default", connection, repo.RouterPublicationJSON{Manifest: repo.JSONUpdate{Set: true, Value: json.RawMessage(`{"owner":"configflow:fixture","resources":[]}`)}}); err != nil {
					t.Fatal(err)
				}
			}
			pyDir := t.TempDir()
			if output, err := exec.Command("cp", "-a", srv.DataDir+"/.", pyDir).CombinedOutput(); err != nil {
				t.Fatalf("copy fixture: %v %s", err, output)
			}
			repotest.PythonLayout(t, pyDir) // Python reads the version-1 layout (O17)
			root, err := filepath.Abs("../../..")
			if err != nil {
				t.Fatal(err)
			}
			interpreter := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
			if interpreter == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					t.Fatal(err)
				}
				interpreter = filepath.Join(home, "cf-test/.venv/bin/python")
			}
			script := filepath.Join(root, "server/internal/api/config/difftest/testdata/reference.py")
			command := exec.Command(interpreter, script)
			command.Dir = root
			command.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+pyDir, "ADMIN_USERNAME=diff-admin", "ADMIN_PASSWORD=diff-password", "JWT_SECRET_KEY="+secret, "PYTHONDONTWRITEBYTECODE=1", "LOG_FILE="+filepath.Join(pyDir, "app.log"))
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var diagnostics bytes.Buffer
			command.Stderr = &diagnostics
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				input.Close()
				command.Process.Kill()
				command.Wait()
				if t.Failed() {
					t.Log(diagnostics.String())
				}
			})
			scanner := bufio.NewScanner(output)
			scanner.Buffer(make([]byte, 65536), 16<<20)
			token, err := auth.EncodeHS256(map[string]any{"username": "diff-admin", "exp": time.Now().Add(time.Hour).Unix()}, secret)
			if err != nil {
				t.Fatal(err)
			}
			call := func(method, path string, body *string, malformed bool) {
				t.Helper()
				request := globalRequest{method, path, body, map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}}
				encoded, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := fmt.Fprintln(input, string(encoded)); err != nil {
					t.Fatal(err)
				}
				if !scanner.Scan() {
					t.Fatalf("Flask oracle ended: %v %s", scanner.Err(), diagnostics.String())
				}
				var expected globalAnswer
				if err := json.Unmarshal(scanner.Bytes(), &expected); err != nil {
					t.Fatal(err)
				}
				if expected.Error != "" {
					t.Fatal(expected.Error)
				}
				text := ""
				if body != nil {
					text = *body
				}
				req := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(text))
				for key, value := range request.Headers {
					req.Header.Set(key, value)
				}
				response := httptest.NewRecorder()
				srv.Handler().ServeHTTP(response, req)
				if malformed {
					if response.Code != 400 || expected.Status != 500 {
						t.Fatalf("approved malformed JSON boundary changed: Go=%d Flask=%d", response.Code, expected.Status)
					}
				} else {
					if response.Code != expected.Status {
						t.Fatalf("%s status Go=%d Flask=%d; %s", path, response.Code, expected.Status, response.Body.String())
					}
					var actual any
					if err := json.Unmarshal(response.Body.Bytes(), &actual); err != nil {
						t.Fatal(err)
					}

					// Exports show plain records (O18 T1c): empty optional
					// members are compared as absent on both sides.
					if !reflect.DeepEqual(testjson.WithoutEmptyMembers(normalizeGlobalAnswer(actual, "")), testjson.WithoutEmptyMembers(normalizeGlobalAnswer(expected.Body, ""))) {
						t.Fatalf("%s response differs: Go=%v Flask=%v", path, actual, expected.Body)
					}
				}
				raw, err := srv.Repo.ExportAllJSON(ctx, false)
				if err != nil {
					t.Fatal(err)
				}
				var actual any
				if err := json.Unmarshal(raw, &actual); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(normalizeGlobalAnswer(actual, ""), normalizeGlobalAnswer(expected.State.Document, "")) {
					t.Fatalf("%s stored document differs", path)
				}
			}
			body := func(raw string) *string { return &raw }
			call("GET", "/api/config/export", nil, false)
			if conflict {
				call("POST", "/api/config/import", body(`{}`), false)
				call("POST", "/api/config/reset", nil, false)
				return
			}
			for _, raw := range []string{`[]`, `{"system_config":[]}`, `{"schema_version":6}`, `{"schema_version":5,"system":{},"shared":{},"profiles":{}}`} {
				call("POST", "/api/config/import", body(raw), false)
			}
			call("POST", "/api/config/import", body(`{`), true)
			raw, err := srv.Repo.ExportAllJSON(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			var document model.Document
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatal(err)
			}
			document.Profiles[0].Mihomo.CustomConfig = "mixed-port: 17890"
			raw, err = model.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			call("POST", "/api/config/import", body(string(raw)), false)
			call("GET", "/api/config/export?desensitize=true", nil, false)
			call("POST", "/api/config/reset", nil, false)
			call("GET", "/api/config/export", nil, false)
		})
	}
}
func normalizeGlobalAnswer(value any, key string) any {
	switch v := value.(type) {
	case map[string]any:
		for name, item := range v {
			v[name] = normalizeGlobalAnswer(item, name)
		}
	case []any:
		for i, item := range v {
			v[i] = normalizeGlobalAnswer(item, key)
		}
	case string:
		if key == "created_at" || key == "updated_at" {
			if _, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return "<timestamp>"
			}
		}
		if (key == "config_token" || key == "rule_proxy_token" || key == "retired_rule_proxy_tokens") && v != "" {
			return "<nonempty-secret>"
		}
	}
	return value
}
