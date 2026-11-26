package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type oxRuleTransportError struct{ timeout bool }

func (e oxRuleTransportError) Error() string   { return "core transport failed" }
func (e oxRuleTransportError) Timeout() bool   { return e.timeout }
func (e oxRuleTransportError) Temporary() bool { return false }

type oxRuleRoundTripFunc func(*http.Request) (*http.Response, error)

func (f oxRuleRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOxiDNSRuleRequestTransportErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"client_timeout_without_deadline_sentinel", oxRuleTransportError{timeout: true}, http.StatusGatewayTimeout, "RULE_UPDATE_UNKNOWN"},
		{"context_deadline", context.DeadlineExceeded, http.StatusGatewayTimeout, "RULE_UPDATE_UNKNOWN"},
		{"network_failure", oxRuleTransportError{}, http.StatusBadGateway, "CORE_UNREACHABLE"},
		{"other_failure", errors.New("connection refused"), http.StatusBadGateway, "CORE_UNREACHABLE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: oxRuleRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Body != nil {
					r.Body.Close()
				}
				return nil, test.err
			})}
			var result any
			err := oxRuleRequest(context.Background(), client, &oxMonitorConfig{endpoint: "http://127.0.0.1:1"}, http.MethodPost, "/api/plugins/rules_download/download", &result)
			if err == nil || err.status != test.status || err.code != test.code {
				t.Fatalf("transport failure classified incorrectly: %+v", err)
			}
		})
	}
}

// Manual updates need only a download -> reload sequence, without a cron plugin.
const oxRuleTestPlugins = `
  - {tag: domains, type: domain_set, args: {files: [rules/domains.txt]}}
  - {tag: addresses, type: ip_set, args: {files: [rules/addresses.txt]}}
  - tag: rules_download
    type: download
    args:
      downloads:
        - {url: 'https://example.test/private?token=secret', dir: rules, filename: domains.txt}
        - {url: 'https://example.test/addresses', dir: rules, filename: addresses.txt}
  - {tag: rules_reload, type: reload_provider, args: ['$domains', '$addresses']}
  - tag: rules_refresh
    type: sequence
    args:
      - {exec: '$rules_download'}
      - {exec: '$rules_reload'}
`

type oxRuleFixture struct {
	cfg     *Config
	updater *oxRuleUpdater
	handler http.HandlerFunc
	mu      sync.Mutex
	calls   []string
	bodies  []string
}

func newOxRuleFixture(t *testing.T, plugins string, override func(http.ResponseWriter, *http.Request) bool) *oxRuleFixture {
	t.Helper()
	fixture := &oxRuleFixture{cfg: &Config{ServiceType: "oxidns", Token: "agent-secret", ConfigPath: filepath.Join(t.TempDir(), "config.yaml")}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		fixture.calls = append(fixture.calls, r.Method+" "+r.URL.Path)
		fixture.mu.Unlock()
		if user, password, ok := r.BasicAuth(); !ok || user != "tester" || password != "core-secret" || r.Header.Get("Cookie") != "" {
			t.Error("core received incorrect authentication or forwarded cookie")
		}
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			fixture.mu.Lock()
			fixture.bodies = append(fixture.bodies, string(body))
			fixture.mu.Unlock()
			targeted := r.URL.Path == "/api/plugins/rules_download/download" && (string(body) == `{"index":0}` || string(body) == `{"index":1}`)
			if (string(body) != "{}" && !targeted) || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("unexpected core request body: %q", body)
			}
		}
		if override != nil && override(w, r) {
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/reload/status":
			data, _ := os.ReadFile(fixture.cfg.ConfigPath)
			fmt.Fprintf(w, `{"pending":false,"in_progress":false,"running_version":%q}`, sha256Hex(data))
		case "GET /api/plugins/rules_download/downloads":
			fmt.Fprint(w, `{"ok":true,"running":false,"downloads":[{"url":"secret"},{}]}`)
		case "POST /api/plugins/rules_download/download":
			fmt.Fprint(w, `{"ok":true,"total":2,"succeeded":2,"failed":0}`)
		case "POST /api/plugins/domains/reload", "POST /api/plugins/addresses/reload":
			fmt.Fprint(w, `{"ok":true,"action":"reload_provider","status":"reloaded"}`)
		default:
			t.Errorf("unexpected core operation: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 404)
		}
	}))
	t.Cleanup(server.Close)
	text := fmt.Sprintf("api:\n  http:\n    listen: %s\n    auth: {type: basic, username: tester, password: core-secret}\nplugins:\n%s", strings.TrimPrefix(server.URL, "http://"), plugins)
	if err := os.WriteFile(fixture.cfg.ConfigPath, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	fixture.updater = &oxRuleUpdater{task: oxRuleUpdateTask{Status: "idle", Phase: "idle"}, requestTimeout: time.Second, roundTimeout: 2 * time.Second}
	fixture.handler = oxidnsMonitorAuth(fixture.cfg, fixture.updater.handler(fixture.cfg))
	return fixture
}

func (f *oxRuleFixture) request(method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+f.cfg.Token)
	r.Header.Set("Cookie", "do-not-forward=secret")
	w := httptest.NewRecorder()
	f.handler(w, r)
	return w
}

func (f *oxRuleFixture) wait(t *testing.T) oxRuleUpdateTask {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		w := f.request("GET", "/api/oxidns/rules/update", "")
		var response struct {
			Task oxRuleUpdateTask `json:"task"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
			t.Fatalf("invalid task observation: %d %s", w.Code, w.Body.String())
		}
		if response.Task.Status != "running" {
			return response.Task
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("rule task did not complete")
	return oxRuleUpdateTask{}
}

func (f *oxRuleFixture) mutations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var calls []string
	for _, call := range f.calls {
		if strings.HasPrefix(call, "POST ") {
			calls = append(calls, call)
		}
	}
	return calls
}

func TestOxiDNSRuleUpdateManualWithoutCron(t *testing.T) {
	f := newOxRuleFixture(t, oxRuleTestPlugins, nil)
	if task := f.wait(t); task.Status != "idle" || task.Phase != "idle" || task.ID != "" {
		t.Fatalf("unexpected idle state: %+v", task)
	}
	w := f.request("POST", "/api/oxidns/rules/update", "{}")
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"status":"running"`) {
		t.Fatalf("not accepted: %d %s", w.Code, w.Body.String())
	}
	task := f.wait(t)
	if task.Status != "succeeded" || task.Phase != "done" || task.ID == "" || task.Downloaded != 2 || task.Reloaded != 2 {
		t.Fatalf("unexpected final state: %+v", task)
	}
	// A stale marker would blame later busy replies on a finished rule update.
	if oxRuleUpdateRunning() {
		t.Fatal("finished rule update still marked as running")
	}
	want := []string{
		"GET /api/reload/status",
		"GET /api/plugins/rules_download/downloads",
		"POST /api/plugins/rules_download/download",
		"GET /api/reload/status",
		"POST /api/plugins/domains/reload",
		"POST /api/plugins/addresses/reload",
		"GET /api/reload/status",
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("wrong manual update requests: %v", f.calls)
	}
}

func TestOxiDNSRuleUpdateFailuresStopChain(t *testing.T) {
	for _, test := range []struct {
		name, target, body, code  string
		status, downloaded, calls int
	}{
		{"partial", "/api/plugins/rules_download/download", `{"ok":false,"total":2,"succeeded":1,"failed":1}`, "DOWNLOAD_FAILED", 200, 1, 1},
		{"false_success", "/api/plugins/rules_download/download", `{"ok":true,"total":2,"succeeded":1,"failed":1}`, "DOWNLOAD_FAILED", 200, 1, 1},
		{"bad_counts", "/api/plugins/rules_download/download", `{"ok":true,"total":2,"succeeded":2}`, "INVALID_RESPONSE", 200, 0, 1},
		{"reload_failed", "/api/plugins/domains/reload", `{"error":"secret path /private/config"}`, "RELOAD_FAILED", 400, 2, 2},
		{"reload_false", "/api/plugins/domains/reload", `{"ok":false}`, "RELOAD_FAILED", 200, 2, 2},
		{"unsupported", "/api/plugins/rules_download/download", `{"error":"secret"}`, "CORE_UPDATE_UNSUPPORTED", 404, 0, 1},
		{"core_auth", "/api/plugins/rules_download/download", `{"error":"core-secret"}`, "CORE_AUTH_FAILED", 401, 0, 1},
		{"redirect", "/api/plugins/rules_download/download", `{"error":"secret"}`, "RULE_UPDATE_FAILED", 302, 0, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newOxRuleFixture(t, oxRuleTestPlugins, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != test.target {
					return false
				}
				w.Header().Set("Location", "http://127.0.0.1:1/private")
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
				return true
			})
			if w := f.request("POST", "/api/oxidns/rules/update", "{}"); w.Code != 202 {
				t.Fatalf("not accepted: %s", w.Body.String())
			}
			task := f.wait(t)
			if task.Status != "failed" || task.Code != test.code || task.Downloaded != test.downloaded || task.Reloaded != 0 || len(f.mutations()) != test.calls {
				t.Fatalf("incorrect failure: %+v, calls %v", task, f.mutations())
			}
			w := f.request("GET", "/api/oxidns/rules/update", "")
			if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "/private") {
				t.Fatal("task leaked upstream detail")
			}
		})
	}
}

func TestOxiDNSRuleUpdatePreflight(t *testing.T) {
	for _, test := range []struct{ name, plugins, target, body, code string }{
		{"unconfigured", "  []", "", "", "RULE_UPDATE_NOT_CONFIGURED"},
		{"drift", oxRuleTestPlugins, "/api/reload/status", `{"pending":false,"in_progress":false,"running_version":"old"}`, "SNAPSHOT_CHANGED"},
		{"reloading", oxRuleTestPlugins, "/api/reload/status", `{"pending":true,"in_progress":true,"running_version":"old"}`, "SNAPSHOT_CHANGED"},
		{"download_busy", oxRuleTestPlugins, "/api/plugins/rules_download/downloads", `{"ok":true,"running":true,"downloads":[{},{}]}`, "CORE_BUSY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newOxRuleFixture(t, test.plugins, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != test.target {
					return false
				}
				fmt.Fprint(w, test.body)
				return true
			})
			w := f.request("POST", "/api/oxidns/rules/update", "{}")
			if w.Code != 409 || !strings.Contains(w.Body.String(), test.code) || len(f.mutations()) != 0 {
				t.Fatalf("wrong preflight: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestOxiDNSRuleUpdateLocksAndTimeout(t *testing.T) {
	entered, stop := make(chan struct{}), make(chan struct{})
	var coreBusy atomic.Bool
	f := newOxRuleFixture(t, oxRuleTestPlugins, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/api/plugins/rules_download/download" {
			coreBusy.Store(true)
			close(entered)
			<-stop
			fmt.Fprint(w, `{"ok":true,"total":2,"succeeded":2,"failed":0}`)
			return true
		}
		if r.URL.Path == "/api/plugins/rules_download/downloads" && coreBusy.Load() {
			fmt.Fprint(w, `{"ok":true,"running":true,"downloads":[{},{}]}`)
			return true
		}
		return false
	})
	defer close(stop)
	f.updater.requestTimeout = 50 * time.Millisecond
	if w := f.request("POST", "/api/oxidns/rules/update", "{}"); w.Code != 202 {
		t.Fatalf("not accepted: %s", w.Body.String())
	}
	<-entered
	if release, err := AcquireServiceOperation(f.cfg); err == nil {
		release()
		t.Fatal("update did not hold service lock")
	}
	if w := f.request("POST", "/api/oxidns/rules/update", "{}"); w.Code != 409 {
		t.Fatalf("duplicate accepted: %d", w.Code)
	}
	if task := f.wait(t); task.Status != "failed" || task.Code != "RULE_UPDATE_UNKNOWN" || task.Reloaded != 0 {
		t.Fatalf("timeout reported incorrectly: %+v", task)
	}
	if w := f.request("POST", "/api/oxidns/rules/update", "{}"); w.Code != 409 || !strings.Contains(w.Body.String(), "CORE_BUSY") {
		t.Fatalf("timed-out native download was restarted: %d %s", w.Code, w.Body.String())
	}
	if len(f.mutations()) != 1 {
		t.Fatalf("timeout continued or repeated work: %v", f.mutations())
	}
}

func TestOxiDNSRuleUpdateRoundDeadlineDuringReload(t *testing.T) {
	stop := make(chan struct{})
	f := newOxRuleFixture(t, oxRuleTestPlugins, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/plugins/domains/reload" {
			return false
		}
		<-stop
		fmt.Fprint(w, `{"ok":true}`)
		return true
	})
	defer close(stop)
	f.updater.roundTimeout = 40 * time.Millisecond
	if w := f.request("POST", "/api/oxidns/rules/update", "{}"); w.Code != 202 {
		t.Fatalf("not accepted: %s", w.Body.String())
	}
	task := f.wait(t)
	if task.Status != "failed" || task.Code != "RULE_UPDATE_UNKNOWN" || task.Downloaded != 2 || task.Reloaded != 0 || len(f.mutations()) != 2 {
		t.Fatalf("round deadline reported success or continued reload: %+v %v", task, f.mutations())
	}
}

func TestOxiDNSRuleUpdateDoesNotReloadChangedConfiguration(t *testing.T) {
	var drift atomic.Bool
	f := newOxRuleFixture(t, oxRuleTestPlugins, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/api/plugins/rules_download/download" {
			drift.Store(true)
			fmt.Fprint(w, `{"ok":true,"total":2,"succeeded":2,"failed":0}`)
			return true
		}
		if r.URL.Path == "/api/reload/status" && drift.Load() {
			fmt.Fprint(w, `{"pending":false,"in_progress":false,"running_version":"external-change"}`)
			return true
		}
		return false
	})
	if w := f.request("POST", "/api/oxidns/rules/update", "{}"); w.Code != 202 {
		t.Fatalf("not accepted: %s", w.Body.String())
	}
	task := f.wait(t)
	if task.Status != "failed" || task.Code != "SNAPSHOT_CHANGED" || len(f.mutations()) != 1 {
		t.Fatalf("changed core received stale provider reload: %+v %v", task, f.mutations())
	}
}

func TestOxiDNSRuleUpdateRejectsInjectionAndRequiresAuth(t *testing.T) {
	f := newOxRuleFixture(t, oxRuleTestPlugins, nil)
	for _, test := range []struct{ method, target, body string }{
		{"POST", "/api/oxidns/rules/update", `{"url":"https://evil.test"}`},
		{"POST", "/api/oxidns/rules/update", `{"tag":"rules_download"}`},
		{"POST", "/api/oxidns/rules/update", `null`},
		{"POST", "/api/oxidns/rules/update", ``},
		{"POST", "/api/oxidns/rules/update", `{} {}`},
		{"POST", "/api/oxidns/rules/update?url=evil", `{}`},
		{"POST", "/api/oxidns/rules/%75pdate", `{}`},
		{"GET", "/api/oxidns/rules/update", `{}`},
		{"DELETE", "/api/oxidns/rules/update", ``},
	} {
		w := f.request(test.method, test.target, test.body)
		if w.Code != 400 && w.Code != 405 {
			t.Errorf("accepted unsafe request: %+v: %d", test, w.Code)
		}
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, f.cfg)
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, "/api/oxidns/rules/update", strings.NewReader("{}")))
		if w.Code != 401 {
			t.Errorf("unauthenticated route: %d", w.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("invalid request reached core")
	}
}

func TestOxiDNSRuleUpdateSharedOperationAndRecoveryBlock(t *testing.T) {
	f := newOxRuleFixture(t, oxRuleTestPlugins, nil)
	release, err := AcquireServiceOperation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := f.request("POST", "/api/oxidns/rules/update", "{}")
	release()
	if w.Code != 409 {
		t.Fatalf("ignored other service operation: %d", w.Code)
	}
	root := deploymentPath(f.cfg, "blocked")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"deployment_id":"blocked","status":"recovery_pending"}`), 0600); err != nil {
		t.Fatal(err)
	}
	w = f.request("POST", "/api/oxidns/rules/update", "{}")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "SERVICE_BUSY") || len(f.calls) != 0 {
		t.Fatalf("ignored recovery: %d %s", w.Code, w.Body.String())
	}
}

func TestOxiDNSRuleUpdateOnlyExplicitSafeChains(t *testing.T) {
	for _, test := range []struct {
		name, plugins string
		valid         bool
	}{
		{"standard", oxRuleTestPlugins, true},
		{"custom", strings.ReplaceAll(strings.ReplaceAll(oxRuleTestPlugins, "rules_download", "custom-download"), "rules_reload", "custom-reload"), true},
		{"conditional", strings.Replace(oxRuleTestPlugins, "{exec: '$rules_download'}", "{matches: has_resp, exec: '$rules_download'}", 1), false},
		{"wrong_provider", strings.Replace(oxRuleTestPlugins, "type: domain_set", "type: exec", 1), false},
		{"unsafe_tag", strings.ReplaceAll(oxRuleTestPlugins, "rules_download", "../../reload"), false},
		{"ambiguous", oxRuleTestPlugins + "  - {tag: another, type: sequence, args: [{exec: '$rules_download'}, {exec: '$rules_reload'}]}\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newOxRuleFixture(t, test.plugins, nil)
			_, err := oxRuleUpdateConfiguration(f.cfg)
			if (err == nil) != test.valid {
				t.Fatalf("chain validity: %v", err)
			}
		})
	}
}

func TestOxiDNSRuleUpdateTargetsRequestedFiles(t *testing.T) {
	f := newOxRuleFixture(t, oxRuleTestPlugins, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPost && r.URL.Path == "/api/plugins/rules_download/download" {
			fmt.Fprint(w, `{"ok":true,"total":1,"succeeded":1,"failed":0}`)
			return true
		}
		return false
	})
	w := f.request("POST", "/api/oxidns/rules/update", `{"paths":["./rules/addresses.txt"]}`)
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"download_total":1`) || !strings.Contains(w.Body.String(), `"reload_total":1`) {
		t.Fatalf("targeted update not accepted: %d %s", w.Code, w.Body.String())
	}
	task := f.wait(t)
	if task.Status != "succeeded" || task.Downloaded != 1 || task.DownloadTotal != 1 || task.Reloaded != 1 || task.ReloadTotal != 1 {
		t.Fatalf("unexpected targeted result: %+v", task)
	}
	want := []string{"POST /api/plugins/rules_download/download", "POST /api/plugins/addresses/reload"}
	if got := f.mutations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("targeted update touched other items: %v", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.bodies[0] != `{"index":1}` {
		t.Fatalf("wrong download index: %v", f.bodies)
	}
}

func TestOxiDNSRuleUpdateTargetedFailureKeepsGoingWithoutReload(t *testing.T) {
	var downloads atomic.Int32
	f := newOxRuleFixture(t, oxRuleTestPlugins, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPost || r.URL.Path != "/api/plugins/rules_download/download" {
			return false
		}
		if downloads.Add(1) == 1 {
			fmt.Fprint(w, `{"ok":false,"total":1,"succeeded":0,"failed":1}`)
		} else {
			fmt.Fprint(w, `{"ok":true,"total":1,"succeeded":1,"failed":0}`)
		}
		return true
	})
	if w := f.request("POST", "/api/oxidns/rules/update", `{"paths":["rules/domains.txt","rules/addresses.txt"]}`); w.Code != 202 {
		t.Fatalf("not accepted: %s", w.Body.String())
	}
	task := f.wait(t)
	if task.Status != "failed" || task.Code != "DOWNLOAD_FAILED" || task.Downloaded != 1 || task.DownloadTotal != 2 || task.Reloaded != 0 {
		t.Fatalf("incorrect targeted failure: %+v", task)
	}
	want := []string{"POST /api/plugins/rules_download/download", "POST /api/plugins/rules_download/download"}
	if got := f.mutations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("failed item stopped others or reloaded: %v", got)
	}
}

func TestOxiDNSRuleUpdateRejectsUnknownOrChangedTargets(t *testing.T) {
	f := newOxRuleFixture(t, oxRuleTestPlugins, nil)
	w := f.request("POST", "/api/oxidns/rules/update", `{"paths":["rules/other.txt"]}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "RULE_UPDATE_TARGET_UNKNOWN") || len(f.calls) != 0 {
		t.Fatalf("unknown target was accepted: %d %s %v", w.Code, w.Body.String(), f.calls)
	}
	f = newOxRuleFixture(t, oxRuleTestPlugins, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/plugins/rules_download/downloads" {
			return false
		}
		fmt.Fprint(w, `{"ok":true,"running":false,"downloads":[{"index":0,"path":"/etc/oxidns/rules/domains.txt"},{"index":1,"path":"/etc/oxidns/rules/replaced.txt"}]}`)
		return true
	})
	w = f.request("POST", "/api/oxidns/rules/update", `{"paths":["rules/addresses.txt"]}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "SNAPSHOT_CHANGED") || len(f.mutations()) != 0 {
		t.Fatalf("changed runtime target was accepted: %d %s", w.Code, w.Body.String())
	}
	if w := f.request("POST", "/api/oxidns/rules/update", `{"paths":["rules/domains.txt"]}`); w.Code != 202 {
		t.Fatalf("matching absolute runtime path rejected: %d %s", w.Code, w.Body.String())
	}
}

func TestOxiDNSRuleUpdateRejectsInvalidPathRequests(t *testing.T) {
	f := newOxRuleFixture(t, oxRuleTestPlugins, nil)
	for _, body := range []string{
		`{"paths":[]}`,
		`{"paths":"rules/domains.txt"}`,
		`{"paths":[""]}`,
		`{"paths":["/etc/oxidns/rules/domains.txt"]}`,
		`{"paths":["../rules/domains.txt"]}`,
		`{"paths":["rules\\domains.txt"]}`,
		`{"paths":["."]}`,
		`{"paths":[1]}`,
		`{"paths":["rules/domains.txt"],"url":"https://evil.test"}`,
		`{"index":0}`,
	} {
		if w := f.request("POST", "/api/oxidns/rules/update", body); w.Code != 400 {
			t.Errorf("accepted invalid targeted request %s: %d", body, w.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("invalid request reached core")
	}
}
