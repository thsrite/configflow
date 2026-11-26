package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeMihomoReloadCore struct {
	mu              sync.Mutex
	cfg             *Config
	reloads         int
	running         string
	reject          bool
	preflightStatus int
}

func (c *fakeMihomoReloadCore) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.preflightStatus != 0 && r.Method == http.MethodGet {
		w.WriteHeader(c.preflightStatus)
		return
	}
	switch {
	case r.URL.Path == "/version" && r.Method == http.MethodGet:
		fmt.Fprint(w, `{"version":"test"}`)
	case r.URL.Path == "/configs" && r.Method == http.MethodGet:
		fmt.Fprint(w, `{"mode":"rule"}`)
	case r.URL.Path == "/configs" && r.Method == http.MethodPut:
		c.reloads++
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["path"] != c.cfg.ConfigPath || body["payload"] != "" || len(body) != 2 || r.URL.Query().Get("force") != "true" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if c.reject {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"message":"private failure details"}`)
			return
		}
		data, _ := os.ReadFile(c.cfg.ConfigPath)
		c.running = string(data)
		w.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(r.URL.Path, "/providers/"):
		fmt.Fprint(w, `{"providers":{"test-rules":{}}}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func testMihomoReloadDeployment(t *testing.T) (*Config, *fakeMihomoReloadCore, func(string) string) {
	t.Helper()
	cfg := testDeploymentConfig(t)
	core := &fakeMihomoReloadCore{cfg: cfg}
	server := httptest.NewServer(http.HandlerFunc(core.serve))
	t.Cleanup(server.Close)
	render := func(extra string) string {
		return fmt.Sprintf("external-controller: %s\nsecret: secret\nmode: rule\n%s", strings.TrimPrefix(server.URL, "http://"), extra)
	}
	cfg.StopCommand = "printf 'stop\\n' >> lifecycle-actions; rm -f running"
	cfg.StartCommand = "printf 'start\\n' >> lifecycle-actions; touch running"
	live := render("# live\n")
	writeActivationFixture(t, cfg.ConfigPath, []byte(live), 0600)
	writeActivationFixture(t, filepath.Join(filepath.Dir(cfg.ConfigPath), "running"), nil, 0600)
	core.running = live
	return cfg, core, render
}

func requireReloadOnly(m *DeploymentManifest) { m.ReloadOnly = true }

func TestMihomoPublishReloadsWithoutRestart(t *testing.T) {
	for _, reloadOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("reload_only=%v", reloadOnly), func(t *testing.T) {
			cfg, core, render := testMihomoReloadDeployment(t)
			candidate := []byte(render("rules: ['DOMAIN,new.test,DIRECT']\n"))
			state := submitBundle(t, cfg, "reload-ok", map[string][]byte{"config.yaml": candidate, "rules/new.txt": []byte("new.test")}, true, func(m *DeploymentManifest) { m.ReloadOnly = reloadOnly })
			if state.Status != "succeeded" || state.ConfigVersion == "" || !state.BackupComplete {
				t.Fatalf("reload publish failed: %+v", state)
			}
			if actions := lifecycle(t, cfg); actions != "" {
				t.Fatalf("native reload stopped/started service: %q", actions)
			}
			installed, _ := os.ReadFile(cfg.ConfigPath)
			if core.reloads != 1 || core.running != string(installed) || !bytes.Contains(installed, []byte("DOMAIN,new.test,DIRECT")) {
				t.Fatalf("candidate not loaded: reloads=%d running=%q", core.reloads, core.running)
			}
		})
	}
}

func TestReloadOnlyRefusesUnavailableNativeAPIWithoutLiveMutation(t *testing.T) {
	for _, scenario := range []string{"absent", "authentication", "unavailable", "stopped", "secret-change", "controller-change"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, core, render := testMihomoReloadDeployment(t)
			candidate := render("# candidate\n")
			switch scenario {
			case "absent":
				writeActivationFixture(t, cfg.ConfigPath, []byte("mode: rule\n"), 0600)
				candidate = "mode: global\n"
			case "authentication":
				core.preflightStatus = http.StatusUnauthorized
			case "unavailable":
				core.preflightStatus = http.StatusNotFound
			case "stopped":
				os.Remove(filepath.Join(filepath.Dir(cfg.ConfigPath), "running"))
			case "secret-change":
				candidate = strings.Replace(candidate, "secret: secret", "secret: new-secret", 1)
			case "controller-change":
				candidate = strings.Replace(candidate, "external-controller: ", "external-controller: 127.0.0.1:1 # ", 1)
			}
			live, _ := os.ReadFile(cfg.ConfigPath)
			state := submitBundle(t, cfg, "reload-unavailable", map[string][]byte{"config.yaml": []byte(candidate), "rules/new.txt": []byte("candidate")}, true, requireReloadOnly)
			if state.Status != "failed" || !strings.Contains(state.Error, "native hot reload is unavailable") || state.BackupComplete {
				t.Fatalf("unsafe activation: %+v", state)
			}
			got, _ := os.ReadFile(cfg.ConfigPath)
			if !bytes.Equal(got, live) || lifecycle(t, cfg) != "" || core.reloads != 0 {
				t.Fatalf("preflight refusal changed live state: %+v", state)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.ConfigPath), "rules/new.txt")); !os.IsNotExist(err) {
				t.Fatal("preflight refusal installed candidate resource")
			}
		})
	}
}

func TestMihomoManualPublishRestartsOnControllerChange(t *testing.T) {
	cfg, core, render := testMihomoReloadDeployment(t)
	candidate := strings.Replace(render("# candidate\n"), "secret: secret", "secret: other-secret", 1)
	state := submitBundle(t, cfg, "identity-changed", map[string][]byte{"config.yaml": []byte(candidate)}, true)
	if state.Status != "succeeded" || core.reloads != 0 || lifecycle(t, cfg) != "stop\nstart\n" {
		t.Fatalf("manual fallback changed: %+v actions=%q", state, lifecycle(t, cfg))
	}
}

func TestMihomoReloadFailureRespectsActivationPolicy(t *testing.T) {
	for _, reloadOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("reload_only=%v", reloadOnly), func(t *testing.T) {
			cfg, core, render := testMihomoReloadDeployment(t)
			core.reject = true
			cfg.StartCommand = "cat config.yaml > started-config; printf 'start\\n' >> lifecycle-actions; touch running"
			live, _ := os.ReadFile(cfg.ConfigPath)
			candidate := []byte(render("rules: ['DOMAIN,new.test,DIRECT']\n"))
			state := submitBundle(t, cfg, "reload-failure", map[string][]byte{"config.yaml": candidate, "rules/new.txt": []byte("candidate")}, true, func(m *DeploymentManifest) { m.ReloadOnly = reloadOnly })
			got, _ := os.ReadFile(cfg.ConfigPath)
			if reloadOnly {
				if state.Status != "rolled_back" || state.FailedStage != "reloading" || !bytes.Equal(got, live) || strings.Contains(state.Error, "private failure") {
					t.Fatalf("reload-only failure did not restore old state: %+v", state)
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.ConfigPath), "rules/new.txt")); !os.IsNotExist(err) {
					t.Fatal("rollback left a candidate resource")
				}
				started, _ := os.ReadFile(filepath.Join(filepath.Dir(cfg.ConfigPath), "started-config"))
				if !bytes.Equal(started, live) {
					t.Fatal("reload-only recovery started the candidate instead of the previous config")
				}
			} else if state.Status != "succeeded" || !bytes.Contains(got, []byte("DOMAIN,new.test,DIRECT")) {
				t.Fatalf("manual restart fallback failed: %+v", state)
			}
			if core.reloads != 1 || lifecycle(t, cfg) != "stop\nstart\n" {
				t.Fatalf("unexpected service actions: %q reloads=%d", lifecycle(t, cfg), core.reloads)
			}
		})
	}
}

func TestMihomoUnixOnlyPublishReloadsAndChecksProviders(t *testing.T) {
	cfg, core, _ := testMihomoReloadDeployment(t)
	cfg.HealthURL = ""
	dir, err := os.MkdirTemp("/tmp", "cf-mihomo-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "api.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(core.serve)}
	go server.Serve(listener)
	defer server.Close()
	live := fmt.Sprintf("external-controller-unix: %s\nsecret: secret\nmode: rule\n", socket)
	writeActivationFixture(t, cfg.ConfigPath, []byte(live), 0600)
	candidate := live + "rule-providers:\n  test-rules:\n    type: file\n    behavior: domain\n    path: rules/test.yaml\nrules: ['RULE-SET,test-rules,DIRECT']\n"
	state := submitBundle(t, cfg, "unix-reload", map[string][]byte{"config.yaml": []byte(candidate), "rules/test.yaml": []byte("payload: ['new.test']\n")}, true, requireReloadOnly)
	if state.Status != "succeeded" || core.reloads != 1 || lifecycle(t, cfg) != "" {
		t.Fatalf("Unix-only reload or provider verification failed: %+v", state)
	}
}

func TestMihomoHotReloadHealthFailureRestoresOldConfig(t *testing.T) {
	cfg, core, render := testMihomoReloadDeployment(t)
	live, _ := os.ReadFile(cfg.ConfigPath)
	state := submitBundle(t, cfg, "health-failure", map[string][]byte{"config.yaml": []byte(render("reject-start: true\nrules: ['DOMAIN,new.test,DIRECT']\n"))}, true, requireReloadOnly)
	installed, _ := os.ReadFile(cfg.ConfigPath)
	if state.Status != "rolled_back" || state.FailedStage != "checking" || !bytes.Equal(installed, live) || core.reloads != 1 || lifecycle(t, cfg) != "stop\nstart\n" {
		t.Fatalf("unhealthy native reload did not recover previous config: %+v", state)
	}
}

func TestOxiDNSReloadOnlyRefusesRestartAndRestoresRejectedCandidate(t *testing.T) {
	for _, startupChange := range []bool{false, true} {
		t.Run(fmt.Sprintf("startup_change=%v", startupChange), func(t *testing.T) {
			cfg, _, render := testOxiReloadDeployment(t)
			live, _ := os.ReadFile(cfg.ConfigPath)
			candidate := render("# reject-reload\n")
			if startupChange {
				candidate = strings.Replace(candidate, "worker_threads: 2", "worker_threads: 4", 1)
			}
			state := submitBundle(t, cfg, "oxi-reload-only", map[string][]byte{"config.yaml": []byte(candidate)}, true, requireReloadOnly)
			got, _ := os.ReadFile(cfg.ConfigPath)
			if !bytes.Equal(got, live) {
				t.Fatal("reload-only failure left candidate installed")
			}
			if startupChange {
				if state.Status != "failed" || lifecycle(t, cfg) != "" {
					t.Fatalf("startup change should fail without mutation: %+v", state)
				}
			} else if state.Status != "rolled_back" || lifecycle(t, cfg) != "stop\nstart\n" {
				t.Fatalf("native failure should recover previous service: %+v", state)
			}
		})
	}
}

func TestMihomoReloadControllerTransport(t *testing.T) {
	for _, address := range []string{"example.com:9090", "192.0.2.1:9090", "127.0.0.1:0", "127.0.0.1:65536", "http://127.0.0.1:9090"} {
		if _, err := newMihomoReloader(map[string]any{"external-controller": address}); err == nil {
			t.Errorf("accepted unsafe controller %q", address)
		}
	}
	for _, address := range []string{"0.0.0.0:9090", "[::]:9090", "localhost:9090", "[::1]:9090"} {
		if _, err := newMihomoReloader(map[string]any{"external-controller": address}); err != nil {
			t.Errorf("rejected local controller %q: %v", address, err)
		}
	}
	t.Run("redirects and proxy environment", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "http://192.0.2.1/private")
			w.WriteHeader(http.StatusTemporaryRedirect)
		}))
		defer server.Close()
		t.Setenv("HTTP_PROXY", "http://192.0.2.1:1")
		r, err := newMihomoReloader(map[string]any{"external-controller": strings.TrimPrefix(server.URL, "http://")})
		if err != nil {
			t.Fatal(err)
		}
		code, _, err := r.request(http.MethodGet, "/version", nil, time.Second)
		if err != nil || code != http.StatusTemporaryRedirect {
			t.Fatalf("redirect followed or environment proxy used: code=%d err=%v", code, err)
		}
	})
	t.Run("Unix socket", func(t *testing.T) {
		// Keep the socket path below macOS sockaddr_un's small length limit.
		dir, err := os.MkdirTemp("/tmp", "cf-mihomo-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		socket := filepath.Join(dir, "api.sock")
		listener, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer secret" {
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"version":"test"}`)
		})}
		go server.Serve(listener)
		defer server.Close()
		if err := mihomoUnixControllerProbe(map[string]any{"external-controller-unix": socket, "secret": "secret"}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReloadOnlyManifestContract(t *testing.T) {
	for _, flag := range []string{"null", `"true"`, "1", "[]", "{}"} {
		var manifest DeploymentManifest
		if err := json.Unmarshal([]byte(`{"reload_only":`+flag+`}`), &manifest); err == nil {
			t.Errorf("accepted invalid reload_only %s", flag)
		}
	}
	for _, body := range []string{`{}`, `{"reload_only":false}`, `{"reload_only":true}`} {
		var manifest DeploymentManifest
		if err := json.Unmarshal([]byte(body), &manifest); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	CapabilitiesHandler(&Config{ServiceType: "mihomo", ConfigPath: "/tmp/config.yaml"})(w, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	data, _ := io.ReadAll(w.Result().Body)
	if !bytes.Contains(data, []byte(`"reload_only_protocols":[1]`)) {
		t.Fatalf("no reload-only capability: %s", data)
	}
}
