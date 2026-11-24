package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	apisubscriptions "configflow/internal/api/subscriptions"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestToolWriteUsesOriginalRuntime(t *testing.T) {
	previous := auth.Configure(auth.Config{})
	defer auth.Configure(previous)
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			base := repo.WithRepository(context.Background(), r)
			if err := r.UpdateShared(base, func(_ context.Context, s *model.Shared) error {
				return json.Unmarshal([]byte(`[{"id":"synthetic","name":"Before","url":"https://example.test/sub"}]`), &s.Subscriptions)
			}); err != nil {
				t.Fatal(err)
			}
			before, err := r.Shared(base)
			if err != nil {
				t.Fatal(err)
			}
			beforeJSON, err := model.Marshal(before)
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, disconnect := context.WithCancel(runtime.BindRequest(base))
			defer disconnect()
			app := httpx.New()
			app.Repo = r
			NewHandler(r).Register(app.Router)
			apisubscriptions.Register(app.Router)
			auth.Install(app)
			reached, release := make(chan struct{}), make(chan struct{})
			once := sync.OnceFunc(func() { close(release) })
			app.AfterRequest(func(c *httpx.Ctx, _ *httpx.Response) {
				if c.Path() == "/api/subscriptions" && c.Request.Method == "GET" {
					close(reached)
					<-release
				}
			})
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"manage_subscription","arguments":{"action":"update","id":"synthetic","data":{"name":"After"}}}}`))
				req = req.WithContext(request)
				w := httptest.NewRecorder()
				app.ServeHTTP(w, req)
				done <- w
			}()
			finished := false
			t.Cleanup(func() {
				once()
				runtime.Shutdown(context.Background())
				if !finished {
					select {
					case <-done:
					case <-time.After(10 * time.Second):
						t.Error("MCP cleanup did not finish")
					}
				}
			})
			select {
			case <-reached:
			case <-time.After(10 * time.Second):
				t.Fatal("REST read barrier not reached")
			}
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				disconnect()
			}
			once()
			var w *httptest.ResponseRecorder
			select {
			case w = <-done:
				finished = true
			case <-time.After(10 * time.Second):
				t.Fatal("MCP did not finish")
			}
			after, err := r.Shared(base)
			if err != nil {
				t.Fatal(err)
			}
			afterJSON, err := model.Marshal(after)
			if err != nil {
				t.Fatal(err)
			}
			if shutdown {
				if !bytes.Equal(beforeJSON, afterJSON) {
					t.Fatalf("shutdown allowed a later commit: %s", afterJSON)
				}
				var envelope rpcResponse
				if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope.Error == nil {
					t.Fatalf("shutdown reported success: %s", w.Body.Bytes())
				}
			} else {
				if len(after.Subscriptions) != 1 || after.Subscriptions[0].Name != "After" || !strings.Contains(w.Body.String(), `"isError":false`) {
					t.Fatalf("disconnect lost accepted write: %s / %s", afterJSON, w.Body.Bytes())
				}
			}
		})
	}
}

func TestTextContentUsesSharedCredentialRedaction(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.ConfigToken = "synthetic-panel-secret"
		s.SystemConfig.RuleProxyToken = "synthetic-rule-secret"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := json.RawMessage(`{"username":"operator","config_token":"synthetic-panel-secret","url":"https://example.test/r?token=synthetic-rule-secret","message":"synthetic-rule-secret","rule_proxy_token":"synthetic-rule-secret"}`)
	content, err := NewHandler(r).publicText(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "synthetic-rule-secret") || !strings.Contains(content, "synthetic-panel-secret") || !strings.Contains(content, "operator") {
		t.Fatalf("credential projection: %s", content)
	}
	if !bytes.Contains(before, []byte("synthetic-panel-secret")) {
		t.Fatal("redaction mutated original")
	}
}
func FuzzTransportJSON(f *testing.F) {
	for _, seed := range []string{`{"jsonrpc":"2.0","id":1,"method":"ping"}`, `[1,null,{},[]]`, `{"method":[1],"id":{},"jsonrpc":"2.0"}`, `{"params":{"arguments":false}}`} {
		f.Add(seed)
	}
	previous := auth.Configure(auth.Config{})
	f.Cleanup(func() { auth.Configure(previous) })
	r, err := repo.Open(f.TempDir(), repo.Options{})
	if err != nil {
		f.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	app := httpx.New()
	app.Repo = r
	NewHandler(r).Register(app.Router)
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 65536 {
			return
		}
		// The fuzz app registers no REST routes, so tool adapters cannot perform business writes or network I/O.
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(raw)).WithContext(ctx)
		w := httptest.NewRecorder()
		app.ServeHTTP(w, req)
		if w.Code != 202 && !json.Valid(w.Body.Bytes()) {
			t.Fatalf("invalid RPC envelope: %s", w.Body.Bytes())
		}
	})
}

func TestMalformedCredentialNeverBecomesAnonymousAccess(t *testing.T) {
	previous := auth.Configure(auth.Config{})
	defer auth.Configure(previous)
	for _, token := range []string{`true`, `123`, `{"unexpected":"value"}`, `["value"]`, `"synthetic-internal"`} {
		t.Run(token, func(t *testing.T) {
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", r.DatabasePath())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			settings := `{"config_token":` + token + `,"rule_proxy_token":"synthetic-internal"}`
			if _, err := db.Exec("UPDATE fields SET value=?, revision=revision+1 WHERE scope='system' AND owner='' AND field='system_config'", settings); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("UPDATE meta SET value=CAST(value AS INTEGER)+1 WHERE key='generation'"); err != nil {
				t.Fatal(err)
			}
			app := httpx.New()
			app.Repo = r
			NewHandler(r).Register(app.Router)
			for _, credential := range []string{"", "123", "synthetic-internal"} {
				req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)).WithContext(repo.WithRepository(context.Background(), r))
				if credential != "" {
					req.Header.Set("Authorization", "Bearer "+credential)
				}
				w := httptest.NewRecorder()
				app.ServeHTTP(w, req)
				if w.Code != 401 {
					t.Errorf("malformed credential %s authorized %q: %d", token, credential, w.Code)
				}
			}
		})
	}
}

func TestResourceMergeKeepsExplicitEmptyHealth(t *testing.T) {
	current := asObject(json.RawMessage(`{"id":"synthetic","health_check":{},"health_check_url":"https://old.example.test/probe"}`))
	data := asObject(json.RawMessage(`{"health_check":{"enable":false}}`))
	got := mergeResource(current, data, "synthetic")
	if !nativeEqual(got["health_check"], json.RawMessage(`{"enable":false}`)) {
		t.Fatalf("empty health became legacy URL: %s", got["health_check"])
	}
	delete(current, "health_check")
	got = mergeResource(current, data, "synthetic")
	if !nativeEqual(got["health_check"], json.RawMessage(`{"enable":false,"url":"https://old.example.test/probe"}`)) {
		t.Fatalf("missing health lost legacy URL: %s", got["health_check"])
	}
}

func TestMalformedSystemConfigCannotAuthorizeMCP(t *testing.T) {
	previous := auth.Configure(auth.Config{})
	defer auth.Configure(previous)
	for _, settings := range []string{`true`, `123`, `["unexpected"]`} {
		t.Run(settings, func(t *testing.T) {
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", r.DatabasePath())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec("UPDATE fields SET value=?,revision=revision+1 WHERE scope='system' AND owner='' AND field='system_config'", settings); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("UPDATE meta SET value=CAST(value AS INTEGER)+1 WHERE key='generation'"); err != nil {
				t.Fatal(err)
			}
			app := httpx.New()
			app.Repo = r
			NewHandler(r).Register(app.Router)
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)).WithContext(repo.WithRepository(context.Background(), r))
			w := httptest.NewRecorder()
			app.ServeHTTP(w, req)
			if w.Code != 401 {
				t.Fatalf("malformed system_config authorized MCP: %d %s", w.Code, w.Body.Bytes())
			}
		})
	}
}
