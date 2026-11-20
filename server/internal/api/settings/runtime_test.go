package settings_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
	"configflow/internal/webdav"
)

func TestSettingsRuntimeWriteLifetime(t *testing.T) {
	for _, stopped := range []bool{true, false} {
		mode := "disconnected"
		if stopped {
			mode = "shutdown"
		}
		for _, operation := range []string{"proxy", "domain", "token", "delete-token", "aggregation", "backup-config", "backup-test", "backup-now"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				s := newServer(t, false)
				var mu sync.Mutex
				dirs := map[string]bool{"/": true}
				calls := 0
				var uploaded []byte
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					calls++
					switch r.Method {
					case "HEAD":
						if !dirs[r.URL.Path] {
							w.WriteHeader(404)
						}
					case "MKCOL":
						dirs[r.URL.Path] = true
						w.WriteHeader(201)
					case "PUT":
						uploaded, _ = io.ReadAll(r.Body)
						w.WriteHeader(201)
					case "PROPFIND":
						w.WriteHeader(207)
						_, _ = io.WriteString(w, `<multistatus xmlns="DAV:"></multistatus>`)
					default:
						w.WriteHeader(405)
					}
				}))
				t.Cleanup(remote.Close)
				t.Cleanup(webdav.SetDefault(webdav.NewService(remote.Client())))
				err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, document *model.System) error {
					document.SystemConfig.ServerDomain = "https://before.example.com"
					document.SystemConfig.ConfigToken = "before-token"
					if err := document.SystemConfig.SetJSON("subscription_aggregation_enabled", json.RawMessage("false")); err != nil {
						return err
					}
					raw, err := json.Marshal(map[string]any{
						"webdav_url":      remote.URL,
						"webdav_username": "operator",
						"webdav_password": "synthetic-password",
						"webdav_path":     "/archive/",
						"auto_backup":     false,
						"keep_latest":     3,
					})
					if err != nil {
						return err
					}
					return document.SetJSON("backup", raw)
				})
				if err != nil {
					t.Fatal(err)
				}
				// Warm the real store and request baseline: a shutdown test must
				// not rely on a later cache miss or network call noticing cancel.
				if _, err := s.Repo.System(s.Context()); err != nil {
					t.Fatal(err)
				}
				before, err := s.Repo.Store().Fingerprint()
				if err != nil {
					t.Fatal(err)
				}
				files := storedFiles(t, s.DataDir)
				exported, err := s.Repo.ExportAllJSON(s.Context(), false)
				if err != nil {
					t.Fatal(err)
				}
				runtime := lifecycle.NewRuntime(lifecycle.Options{Grace: 2 * time.Second})
				runtime.Start(s.Context())
				t.Cleanup(func() { runtime.Shutdown(context.Background()) })
				requestCtx, cancel := context.WithCancel(runtime.BindRequest(s.Context()))
				defer cancel()
				if stopped {
					runtime.Shutdown(context.Background())
					if !errors.Is(lifecycle.Detach(requestCtx).Err(), context.Canceled) {
						t.Fatal("real Runtime did not cancel bound work")
					}
				} else {
					cancel()
					if requestCtx.Err() == nil || lifecycle.Detach(requestCtx).Err() != nil {
						t.Fatal("disconnect changed Runtime lifetime")
					}
				}
				method, path, body := "POST", "", ""
				switch operation {
				case "proxy":
					path, body = endpoint, `{"rule_fetch_proxy":"http://proxy.example.com:8080"}`
				case "domain":
					path, body = "/api/server-domain", `{"new_domain":"https://after.example.com"}`
				case "token":
					path, body = "/api/config-token", `{"token":"after-token"}`
				case "delete-token":
					method, path, body = "DELETE", "/api/config-token", "{}"
				case "aggregation":
					path, body = "/api/settings/subscription-aggregation", `{"enabled":true}`
				case "backup-config":
					path, body = "/api/backup/config", `{"keep_latest":9}`
				case "backup-test", "backup-now":
					path = "/api/backup/" + strings.TrimPrefix(operation, "backup-")
					raw, err := json.Marshal(map[string]string{"webdav_url": remote.URL, "webdav_username": "operator", "webdav_password": "******", "webdav_path": "/archive/"})
					if err != nil {
						t.Fatal(err)
					}
					body = string(raw)
				}
				request := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(requestCtx)
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				s.Handler().ServeHTTP(response, request)
				after, err := s.Repo.Store().Fingerprint()
				if err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				defer mu.Unlock()
				if stopped {
					if response.Code < 400 {
						t.Fatalf("stopped write succeeded: %d %s", response.Code, response.Body)
					}
					if before != after || !reflect.DeepEqual(files, storedFiles(t, s.DataDir)) || calls != 0 {
						t.Fatalf("stopped write reached storage or WebDAV: calls=%d", calls)
					}
					return
				}
				if response.Code != 200 {
					t.Fatalf("disconnected write failed: %d %s", response.Code, response.Body)
				}
				if strings.HasPrefix(operation, "backup-") && operation != "backup-config" {
					if before != after || calls == 0 || !dirs["/archive/"] {
						t.Fatal("backup did not finish remote directory work or changed config")
					}
					if operation == "backup-now" {
						if diff := testjson.Diff(exported, json.RawMessage(uploaded)); diff != "" {
							t.Fatalf("disconnected backup did not upload the complete document: %s", diff)
						}
						receipt, err := os.ReadFile(filepath.Join(s.DataDir, taskstatus.Filename))
						if err != nil || !bytes.Contains(receipt, []byte(`"last_status":"success"`)) {
							t.Fatalf("backup receipt missing: %s %v", receipt, err)
						}
					}
					return
				}
				if after.Generation != before.Generation+1 {
					t.Fatalf("commit generation before=%d after=%d", before.Generation, after.Generation)
				}
				doc := systemMap(t, s)
				settingsCfg := asMap(doc["system_config"])
				var got, want any
				switch operation {
				case "proxy":
					got, want = settingsCfg["rule_fetch_proxy"], "http://proxy.example.com:8080"
				case "domain":
					got, want = settingsCfg["server_domain"], "https://after.example.com"
				case "token":
					got, want = settingsCfg["config_token"], "after-token"
				case "delete-token":
					got, want = settingsCfg["config_token"], ""
				case "aggregation":
					got, want = settingsCfg["subscription_aggregation_enabled"], true
				case "backup-config":
					got, want = asInt(asMap(doc["backup"])["keep_latest"]), int64(9)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("write not complete: %v want %v", got, want)
				}
			})
		}
	}
}
