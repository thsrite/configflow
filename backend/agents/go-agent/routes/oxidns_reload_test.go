package routes

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeReloadCore models the OxiDNS control API: it runs a config version and
// re-reads the on-disk file when asked to reload.
type fakeReloadCore struct {
	mu       sync.Mutex
	cfg      *Config
	running  string
	lastErr  string
	reloads  int
	unhealth bool
	// slowPolls keeps a "slow-reload" reload in progress for that many status
	// polls, like a core still re-assembling large rule sets.
	slowPolls int
}

func (c *fakeReloadCore) serve(w http.ResponseWriter, r *http.Request) {
	if u, p, ok := r.BasicAuth(); !ok || u != "tester" || p != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case r.URL.Path == "/api/readyz":
		data, _ := os.ReadFile(c.cfg.ConfigPath)
		if c.unhealth && bytes.Contains(data, []byte("unhealthy-after-reload")) && c.running == sha256Hex(data) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"status":"ready"}`)
	case r.URL.Path == "/api/reload/status" && r.Method == http.MethodGet:
		if c.slowPolls > 0 {
			c.slowPolls--
			if c.slowPolls > 0 {
				fmt.Fprintf(w, `{"status":"reloading","pending":false,"in_progress":true,"last_error":null,"running_version":%q}`, c.running)
				return
			}
			data, _ := os.ReadFile(c.cfg.ConfigPath)
			c.running = sha256Hex(data)
			appendLifecycle(c.cfg, "reload-finished")
		}
		lastErr := "null"
		if c.lastErr != "" {
			lastErr = fmt.Sprintf("%q", c.lastErr)
		}
		fmt.Fprintf(w, `{"status":"ok","pending":false,"in_progress":false,"last_error":%s,"running_version":%q}`, lastErr, c.running)
	case r.URL.Path == "/api/reload" && r.Method == http.MethodPost:
		c.reloads++
		data, _ := os.ReadFile(c.cfg.ConfigPath)
		if bytes.Contains(data, []byte("reject-reload")) {
			c.lastErr = "plugin assembly failed"
		} else if bytes.Contains(data, []byte("slow-reload")) {
			c.lastErr = ""
			c.slowPolls = 8
		} else {
			c.lastErr = ""
			c.running = sha256Hex(data)
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"ok":true,"action":"reload","status":"accepted"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// testOxiReloadDeployment installs a running live config and returns a
// candidate with the same API/runtime sections plus a different plugin list.
func testOxiReloadDeployment(t *testing.T) (*Config, *fakeReloadCore, func(extra string) string) {
	t.Helper()
	cfg := testDeploymentConfig(t)
	cfg.ServiceType = "oxidns"
	cfg.HealthURL = ""
	core := &fakeReloadCore{cfg: cfg}
	server := httptest.NewServer(http.HandlerFunc(core.serve))
	t.Cleanup(server.Close)
	dns := oxidnsTestDNS(t)
	render := func(extra string) string {
		return fmt.Sprintf("runtime:\n  worker_threads: 2\napi:\n  http:\n    listen: %s\n    auth: {type: basic, username: tester, password: secret}\nplugins:\n  - tag: dns\n    type: udp_server\n    args: {listen: '%s', entry: main}\n  - tag: main\n    type: sequence\n    args: []\n%s",
			strings.TrimPrefix(server.URL, "http://"), dns, extra)
	}
	root := filepath.Dir(cfg.ConfigPath)
	cfg.StopCommand = "printf 'stop\\n' >> lifecycle-actions; rm -f running"
	cfg.StartCommand = "printf 'start\\n' >> lifecycle-actions; touch running"
	cfg.StatusCommand = "test -f running"
	writeActivationFixture(t, cfg.ServiceBinary, []byte(activationCheckScript), 0700)
	live := []byte(render("# live\n"))
	writeActivationFixture(t, cfg.ConfigPath, live, 0600)
	writeActivationFixture(t, filepath.Join(root, "running"), nil, 0600)
	core.running = sha256Hex(live)
	return cfg, core, render
}

func lifecycle(t *testing.T, cfg *Config) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(cfg.ConfigPath), "lifecycle-actions"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOxiDNSPublishReloadsWithoutRestart(t *testing.T) {
	cfg, core, render := testOxiReloadDeployment(t)
	candidate := []byte(render("# candidate\n"))
	state := submitBundle(t, cfg, "reload-ok", map[string][]byte{"config.yaml": candidate, "rules/new.txt": []byte("full:new.test")}, true)
	if state.Status != "succeeded" || state.ConfigVersion == "" {
		t.Fatalf("reload publish failed: %+v", state)
	}
	if actions := lifecycle(t, cfg); actions != "" {
		t.Fatalf("reload publish must not stop/start the service: %q", actions)
	}
	if core.reloads != 1 || core.running != sha256Hex(candidate) {
		t.Fatalf("core not reloaded onto the candidate: reloads=%d", core.reloads)
	}
	got, _ := os.ReadFile(cfg.ConfigPath)
	if !bytes.Equal(got, candidate) {
		t.Fatal("candidate config not installed")
	}
	if !state.BackupComplete {
		t.Fatal("reload publish skipped the backup")
	}
}

func TestOxiDNSPublishRestartsWhenStartupSectionsChange(t *testing.T) {
	cfg, core, render := testOxiReloadDeployment(t)
	candidate := strings.Replace(render("# candidate\n"), "worker_threads: 2", "worker_threads: 4", 1)
	state := submitBundle(t, cfg, "runtime-change", map[string][]byte{"config.yaml": []byte(candidate)}, true)
	if state.Status != "succeeded" {
		t.Fatalf("restart publish failed: %+v", state)
	}
	if actions := lifecycle(t, cfg); actions != "stop\nstart\n" {
		t.Fatalf("runtime change must restart: %q", actions)
	}
	if core.reloads != 0 {
		t.Fatal("runtime change must not try a reload")
	}
}

func TestOxiDNSPublishFallsBackToRestartWhenReloadRejected(t *testing.T) {
	cfg, core, render := testOxiReloadDeployment(t)
	candidate := []byte(render("# reject-reload\n"))
	state := submitBundle(t, cfg, "reload-rejected", map[string][]byte{"config.yaml": candidate}, true)
	if state.Status != "succeeded" {
		t.Fatalf("fallback publish failed: %+v", state)
	}
	if core.reloads != 1 {
		t.Fatalf("reload should be attempted once, got %d", core.reloads)
	}
	if actions := lifecycle(t, cfg); actions != "stop\nstart\n" {
		t.Fatalf("rejected reload must fall back to restart: %q", actions)
	}
}

func TestOxiDNSPublishRestartsWhenRunningVersionIsStale(t *testing.T) {
	cfg, core, render := testOxiReloadDeployment(t)
	core.running = "stale"
	state := submitBundle(t, cfg, "stale-version", map[string][]byte{"config.yaml": []byte(render("# candidate\n"))}, true)
	if state.Status != "succeeded" || core.reloads != 0 || lifecycle(t, cfg) != "stop\nstart\n" {
		t.Fatalf("stale running version must restart: %+v reloads=%d actions=%q", state, core.reloads, lifecycle(t, cfg))
	}
}

func TestOxiDNSReloadHealthFailureRollsBack(t *testing.T) {
	cfg, core, render := testOxiReloadDeployment(t)
	cfg.DeploymentHealthTimeout = 2
	core.unhealth = true
	live, _ := os.ReadFile(cfg.ConfigPath)
	state := submitBundle(t, cfg, "reload-unhealthy", map[string][]byte{"config.yaml": []byte(render("# unhealthy-after-reload\n"))}, true)
	if state.Status != "rolled_back" || state.FailedStage != "checking" {
		t.Fatalf("unhealthy reload must roll back: %+v", state)
	}
	got, _ := os.ReadFile(cfg.ConfigPath)
	if !bytes.Equal(got, live) {
		t.Fatal("rollback did not restore the previous config")
	}
	if actions := lifecycle(t, cfg); actions != "stop\nstart\n" {
		t.Fatalf("rollback restarts onto the restored files: %q", actions)
	}
}

func appendLifecycle(cfg *Config, action string) {
	f, err := os.OpenFile(filepath.Join(filepath.Dir(cfg.ConfigPath), "lifecycle-actions"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err == nil {
		fmt.Fprintln(f, action)
		f.Close()
	}
}

// A reload that outlives the Agent's wait is still running, and OxiDNS handles
// SIGTERM only after it. The fallback restart must not stop the core mid-reload.
func TestOxiDNSFallbackRestartWaitsForUnfinishedReload(t *testing.T) {
	cfg, core, render := testOxiReloadDeployment(t)
	oldTimeout, oldSettle := oxidnsReloadTimeout, oxidnsReloadSettleTimeout
	oxidnsReloadTimeout, oxidnsReloadSettleTimeout = 300*time.Millisecond, 10*time.Second
	t.Cleanup(func() { oxidnsReloadTimeout, oxidnsReloadSettleTimeout = oldTimeout, oldSettle })
	state := submitBundle(t, cfg, "slow-reload", map[string][]byte{"config.yaml": []byte(render("# slow-reload\n"))}, true)
	if state.Status != "succeeded" {
		t.Fatalf("fallback publish failed: %+v", state)
	}
	if core.reloads != 1 {
		t.Fatalf("reload should be attempted once, got %d", core.reloads)
	}
	if actions := lifecycle(t, cfg); actions != "reload-finished\nstop\nstart\n" {
		t.Fatalf("stop must wait for the unfinished reload: %q", actions)
	}
}

func TestOxiDNSReloadSettleGivesUpAfterTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"reloading","pending":false,"in_progress":true,"last_error":null,"running_version":null}`)
	}))
	defer server.Close()
	r := &oxidnsReloader{endpoint: server.URL, client: server.Client()}
	start := time.Now()
	if r.settle(300 * time.Millisecond) {
		t.Fatal("a reload that never ends must not count as settled")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("settle ignored its timeout: %s", elapsed)
	}
	unreachable := &oxidnsReloader{endpoint: "http://127.0.0.1:1", client: server.Client()}
	if unreachable.settle(300 * time.Millisecond) {
		t.Fatal("an unreachable core must not count as settled")
	}
}
