package main

import (
	"agent/routes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newMihomoPanelFixture(t *testing.T, core http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	_, handler := newMihomoPanelConfigFixture(t, core)
	return handler
}

func newMihomoPanelConfigFixture(t *testing.T, core http.HandlerFunc) (*Config, http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(core)
	t.Cleanup(server.Close)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(configPath, []byte("external-controller: "+strings.TrimPrefix(server.URL, "http://")+"\nsecret: s3cret\n"), 0600)
	cfg := &Config{ServiceType: "mihomo", ConfigPath: configPath}
	return cfg, mihomoPanelHandler(cfg)
}

func TestMihomoPanelForwardsAllowedRequests(t *testing.T) {
	var seen []string
	handler := newMihomoPanelFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Authorization")+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})

	cases := []struct{ method, target, body, want string }{
		{"GET", "/api/mihomo/proxies", "", "GET /proxies Bearer s3cret "},
		{"PUT", "/api/mihomo/proxies/%E8%8A%82%E7%82%B9%2FA", `{"name":"x"}`, `PUT /proxies/%E8%8A%82%E7%82%B9%2FA Bearer s3cret {"name":"x"}`},
		{"GET", "/api/mihomo/group/Auto/delay?url=http%3A%2F%2Fcp.cloudflare.com&timeout=5000&extra=1", "",
			"GET /group/Auto/delay?timeout=5000&url=http%3A%2F%2Fcp.cloudflare.com Bearer s3cret "},
		{"GET", "/api/mihomo/proxies/JP-05/delay?url=https%3A%2F%2Fexample.com%2F204&timeout=5000&extra=1", "",
			"GET /proxies/JP-05/delay?timeout=5000&url=https%3A%2F%2Fexample.com%2F204 Bearer s3cret "},
		{"GET", "/api/mihomo/providers/proxies/Subscription/healthcheck?url=https%3A%2F%2Fignored.test&timeout=5000&extra=1", "",
			"GET /providers/proxies/Subscription/healthcheck Bearer s3cret "},
		{"GET", "/api/mihomo/providers/proxies/Subscription/JP-05/healthcheck?url=https%3A%2F%2Fexample.com%2F204&timeout=5000&extra=1", "",
			"GET /providers/proxies/Subscription/JP-05/healthcheck?timeout=5000&url=https%3A%2F%2Fexample.com%2F204 Bearer s3cret "},
		{"DELETE", "/api/mihomo/connections/abc", "", "DELETE /connections/abc Bearer s3cret "},
	}
	for _, c := range cases {
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(c.method, c.target, strings.NewReader(c.body)))
		if recorder.Code != http.StatusOK || recorder.Body.String() != `{"ok":true}` {
			t.Fatalf("%s %s: got %d %s", c.method, c.target, recorder.Code, recorder.Body.String())
		}
		if got := seen[len(seen)-1]; got != c.want {
			t.Fatalf("forwarded %q, want %q", got, c.want)
		}
	}
}

func TestMihomoPanelRejectsOtherEndpoints(t *testing.T) {
	called := false
	handler := newMihomoPanelFixture(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	cases := []struct {
		method, target string
		status         int
	}{
		{"PUT", "/api/mihomo/rules", http.StatusMethodNotAllowed},
		{"POST", "/api/mihomo/upgrade/ui", http.StatusNotFound},
		{"POST", "/api/mihomo/restart", http.StatusNotFound},
		{"DELETE", "/api/mihomo/logs", http.StatusMethodNotAllowed},
		{"GET", "/api/mihomo/logs/stream", http.StatusNotFound},
		{"DELETE", "/api/mihomo/rules", http.StatusMethodNotAllowed},
		{"GET", "/api/mihomo/proxies/a/b", http.StatusNotFound},
		{"POST", "/api/mihomo/providers/proxies/sub/node/healthcheck", http.StatusMethodNotAllowed},
		{"PUT", "/api/mihomo/providers/proxies/sub/node/healthcheck", http.StatusMethodNotAllowed},
		{"PATCH", "/api/mihomo/providers/proxies/sub/node/healthcheck", http.StatusMethodNotAllowed},
		{"DELETE", "/api/mihomo/providers/proxies/sub/node/healthcheck", http.StatusMethodNotAllowed},
		{"HEAD", "/api/mihomo/providers/proxies/sub/node/healthcheck", http.StatusMethodNotAllowed},
		{"GET", "/api/mihomo/providers/proxies/sub/node/extra/healthcheck", http.StatusNotFound},
		{"GET", "/api/mihomo/providers/proxies//node/healthcheck", http.StatusNotFound},
	}
	for _, c := range cases {
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(c.method, c.target, nil))
		if recorder.Code != c.status {
			t.Fatalf("%s %s: got %d, want %d", c.method, c.target, recorder.Code, c.status)
		}
	}
	if called {
		t.Fatal("rejected request reached the controller")
	}
}

func TestMihomoPanelProviderNodeDelayPreservesEncodedNames(t *testing.T) {
	provider, node := "订阅 🇯🇵/50%?source#1", "🇯🇵 JP-05 · AWS2/节点%?name#2"
	probeURL := "https://example.com/generate_204?from=测速&percent=%25#fragment"
	path := "/providers/proxies/" + url.PathEscape(provider) + "/" + url.PathEscape(node) + "/healthcheck"
	query := url.Values{"url": {probeURL}, "timeout": {"7000"}}
	wantURI := path + "?" + query.Encode()
	seenURI, seenToken := "", ""
	handler := newMihomoPanelFixture(t, func(w http.ResponseWriter, r *http.Request) {
		seenURI, seenToken = r.URL.RequestURI(), r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delay":42}`))
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/api/mihomo"+wantURI+"&secret=ignored&force=true", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"delay":42}` {
		t.Fatalf("got %d %s", recorder.Code, recorder.Body.String())
	}
	if seenURI != wantURI || seenToken != "Bearer s3cret" {
		t.Fatalf("forwarded %q with token %q, want %q with controller token", seenURI, seenToken, wantURI)
	}
	if recorder.Header().Get("Content-Type") != "application/json" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected reply headers: %v", recorder.Header())
	}
}

func TestMihomoPanelProviderNodeDelayErrorResponses(t *testing.T) {
	cases := []struct {
		name       string
		coreStatus int
		coreBody   string
		wantStatus int
		wantBody   string
	}{
		{"missing node", http.StatusNotFound, `{"message":"Resource not found"}`, http.StatusNotFound, `{"message":"Resource not found"}`},
		{"probe failed", http.StatusGatewayTimeout, `{"message":"An error occurred in the delay test"}`, http.StatusGatewayTimeout, `{"message":"An error occurred in the delay test"}`},
		{"forbidden controller", http.StatusForbidden, `{"message":"secret internal diagnostic"}`, http.StatusBadGateway, "CORE_AUTH_FAILED"},
		{"unauthorized controller", http.StatusUnauthorized, `{"message":"secret internal diagnostic"}`, http.StatusBadGateway, "CORE_AUTH_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := newMihomoPanelFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.coreStatus)
				_, _ = w.Write([]byte(tc.coreBody))
			})
			recorder := httptest.NewRecorder()
			handler(recorder, httptest.NewRequest(http.MethodGet, "/api/mihomo/providers/proxies/sub/node/healthcheck?url=http%3A%2F%2Fexample.com&timeout=100", nil))
			if recorder.Code != tc.wantStatus || !strings.Contains(recorder.Body.String(), tc.wantBody) {
				t.Fatalf("got %d %s", recorder.Code, recorder.Body.String())
			}
			if tc.wantStatus == http.StatusBadGateway && strings.Contains(recorder.Body.String(), "secret internal diagnostic") {
				t.Fatal("controller auth details leaked")
			}
		})
	}
}

func TestMihomoPanelProviderNodeDelayCancelsControllerRequest(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	handler := newMihomoPanelFixture(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler(recorder, httptest.NewRequest(http.MethodGet, "/api/mihomo/providers/proxies/sub/node/healthcheck", nil).WithContext(ctx))
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach controller")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not stop after cancellation")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("controller request was not cancelled")
	}
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "CORE_UNREACHABLE") {
		t.Fatalf("got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMihomoPanelMasksControllerAuthFailure(t *testing.T) {
	handler := newMihomoPanelFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest("GET", "/api/mihomo/version", nil))
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "CORE_AUTH_FAILED") {
		t.Fatalf("got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMihomoPanelRequiresMihomoAgent(t *testing.T) {
	recorder := httptest.NewRecorder()
	mihomoPanelHandler(&Config{ServiceType: "oxidns"})(recorder, httptest.NewRequest("GET", "/api/mihomo/version", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("got %d", recorder.Code)
	}
}

func TestMihomoPanelReloadOnlyUsesCurrentConfigFile(t *testing.T) {
	var seen string
	handler := newMihomoPanelFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = r.Method + " " + r.URL.RequestURI() + " " + string(body)
		w.WriteHeader(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest("PUT", "/api/mihomo/configs?force=false",
		strings.NewReader(`{"path":"/tmp/evil.yaml","payload":"mode: global"}`)))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("got %d %s", recorder.Code, recorder.Body.String())
	}
	if want := `PUT /configs?force=true {"path":"","payload":""}`; seen != want {
		t.Fatalf("forwarded %q, want %q", seen, want)
	}
}

func TestMihomoPanelUpgradeDropsQueryAndBody(t *testing.T) {
	var seen string
	handler := newMihomoPanelFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = r.Method + " " + r.URL.RequestURI() + " " + string(body)
		w.Write([]byte(`{"status":"ok"}`))
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest("POST", "/api/mihomo/upgrade?channel=alpha&force=true", strings.NewReader(`{"path":"x"}`)))
	if recorder.Code != http.StatusOK || seen != "POST /upgrade " {
		t.Fatalf("got %d, forwarded %q", recorder.Code, seen)
	}
}

func TestMihomoPanelProviderUpdatesShareOperationLockAndReleaseOnFailure(t *testing.T) {
	for _, kind := range []string{"rules", "proxies"} {
		t.Run(kind, func(t *testing.T) {
			var cfg *Config
			var calls atomic.Int32
			var heldDuringRequest atomic.Bool
			var handler http.HandlerFunc
			cfg, handler = newMihomoPanelConfigFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if release, err := routes.AcquireServiceOperation(toRoutesConfig(cfg)); err != nil {
					heldDuringRequest.Store(true)
				} else {
					release()
				}
				w.WriteHeader(http.StatusInternalServerError)
			})
			release, err := routes.AcquireServiceOperation(toRoutesConfig(cfg))
			if err != nil {
				t.Fatal(err)
			}
			busy := httptest.NewRecorder()
			handler(busy, httptest.NewRequest(http.MethodPut, "/api/mihomo/providers/"+kind+"/test", strings.NewReader("{}")))
			release()
			if busy.Code != http.StatusConflict || !strings.Contains(busy.Body.String(), "SERVICE_BUSY") || calls.Load() != 0 {
				t.Fatalf("provider update overlapped an active operation: %d %s", busy.Code, busy.Body.String())
			}
			failed := httptest.NewRecorder()
			handler(failed, httptest.NewRequest(http.MethodPut, "/api/mihomo/providers/"+kind+"/test", strings.NewReader("{}")))
			if failed.Code != http.StatusInternalServerError || calls.Load() != 1 || !heldDuringRequest.Load() {
				t.Fatalf("provider update did not hold the service lock: %d %s", failed.Code, failed.Body.String())
			}
			release, err = routes.AcquireServiceOperation(toRoutesConfig(cfg))
			if err != nil {
				t.Fatalf("failed provider update leaked its operation lock: %v", err)
			}
			release()
		})
	}
}

func TestMihomoPanelProviderUnreachableReleasesOperationLock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("external-controller: "+strings.TrimPrefix(server.URL, "http://")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{ServiceType: "mihomo", ConfigPath: configPath}
	w := httptest.NewRecorder()
	mihomoPanelHandler(cfg)(w, httptest.NewRequest(http.MethodPut, "/api/mihomo/providers/rules/test", strings.NewReader("{}")))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "CORE_UNREACHABLE") {
		t.Fatalf("unexpected unreachable controller response: %d %s", w.Code, w.Body.String())
	}
	release, err := routes.AcquireServiceOperation(toRoutesConfig(cfg))
	if err != nil {
		t.Fatalf("unreachable controller leaked the operation lock: %v", err)
	}
	release()
}
