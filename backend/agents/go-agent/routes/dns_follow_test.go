package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func confirmedDNSFixture(t *testing.T, cfg *Config, content string, files map[string]string) {
	t.Helper()
	if files == nil {
		files = make(map[string]string)
	}
	files[filepath.Base(cfg.ConfigPath)] = content
	manifest := DeploymentManifest{ProtocolVersion: 1, DeploymentID: "dns-source", AgentID: cfg.AgentID,
		ServiceType: cfg.ServiceType, ConfigPath: filepath.Base(cfg.ConfigPath)}
	for name, value := range files {
		path := filepath.Join(filepath.Dir(cfg.ConfigPath), name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		digest, size, err := hashFile(path)
		if err != nil {
			t.Fatal(err)
		}
		role := "custom"
		if name == filepath.Base(cfg.ConfigPath) {
			role = "config"
		} else if strings.HasPrefix(name, "rules/") {
			role = "rule"
		}
		manifest.Files = append(manifest.Files, DeploymentFile{Path: name, Size: size, SHA256: digest, Role: role})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := durableWrite(filepath.Join(deploymentRoot(cfg), "active-manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveDeployment(cfg, &DeploymentState{DeploymentID: "dns-source"}, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(cfg.ConfigPath), "running"), nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func callDNSRoute(t *testing.T, cfg *Config, route, token string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	RegisterRoutes(mux, cfg)
	req := httptest.NewRequest(http.MethodGet, route, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestDNSFollowRoutesRequireAuthentication(t *testing.T) {
	cfg := &Config{Token: "private-agent-token"}
	for _, endpoint := range []string{"listener?deployment_id=dns-source", "active?deployment_id=dns-source", "peer"} {
		for _, token := range []string{"", "wrong-token"} {
			w := callDNSRoute(t, cfg, "/api/dns/"+endpoint, token)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s accepted unauthenticated request: %d", endpoint, w.Code)
			}
		}
	}
}

func TestDNSFollowListenerUsesConfirmedLiveDNSListen(t *testing.T) {
	cfg := testDeploymentConfig(t)
	cfg.Token = "private-agent-token"
	confirmedDNSFixture(t, cfg, "secret: PRIVATE_CONTROLLER_TOKEN\ndns: {enable: true, listen: '[::1]:7874'}\ntun: {dns-hijack: ['any:53', 'tcp://any:5353']}\n", nil)
	w := callDNSRoute(t, cfg, "/api/dns/listener?deployment_id=dns-source", cfg.Token)
	var summary map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &summary) != nil || summary["success"] != true ||
		summary["agent_id"] != cfg.AgentID || summary["deployment_id"] != "dns-source" ||
		summary["enabled"] != true || summary["listen"] != "[::1]:7874" || summary["host"] != "::1" || summary["port"] != float64(7874) {
		t.Fatalf("unexpected summary: %d %s", w.Code, w.Body.String())
	}
	for _, secret := range []string{cfg.Token, "PRIVATE_CONTROLLER_TOKEN", "dns-hijack", "any:53"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("summary leaked configuration content %q", secret)
		}
	}
}

func TestDNSFollowListenerRejectsUnconfirmedOrUnavailableConfiguration(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		change  func(*testing.T, *Config)
		query   string
		status  int
		message string
	}{
		{name: "disabled", content: "dns: {enable: false, listen: '127.0.0.1:1053'}", status: 409, message: "未开启"},
		{name: "missing", content: "mode: rule", status: 409, message: "未开启"},
		{name: "invalid-port", content: "dns: {enable: true, listen: '127.0.0.1:+1053'}", status: 409, message: "有效的监听地址"},
		{name: "wrong-id", query: "other-deployment", status: 409, message: "尚未确认成功"},
		{name: "bad-id", query: "../other", status: 400, message: "发布编号"},
		{name: "missing-id", query: "EMPTY", status: 400, message: "发布编号"},
		{name: "stopped", status: 503, message: "未运行", change: func(t *testing.T, cfg *Config) {
			cfg.StatusCommand = "exit 1"
		}},
		{name: "unhealthy", status: 503, message: "健康检查", content: "dns: {enable: true, listen: '127.0.0.1:1053'}\nreject-start: true"},
		{name: "modified", status: 409, message: "发布后被修改", change: func(t *testing.T, cfg *Config) {
			if err := os.WriteFile(cfg.ConfigPath, []byte("dns: {enable: true, listen: '127.0.0.1:5553'}"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "ready-only", status: 409, message: "尚未确认成功", change: func(t *testing.T, cfg *Config) {
			if err := saveDeployment(cfg, &DeploymentState{DeploymentID: "dns-source"}, "ready"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "rolled-back", status: 409, message: "已回滚", change: func(t *testing.T, cfg *Config) {
			if err := saveDeployment(cfg, &DeploymentState{DeploymentID: "dns-source", ManualRollback: true}, "rolled_back"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "not-active", status: 409, message: "当前运行配置", change: func(t *testing.T, cfg *Config) {
			if err := saveDeployment(cfg, &DeploymentState{DeploymentID: "older-success"}, "succeeded"); err != nil {
				t.Fatal(err)
			}
		}, query: "older-success"},
		{name: "wrong-agent", status: 409, message: "当前运行配置", change: func(t *testing.T, cfg *Config) { cfg.AgentID = "different-agent" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			cfg.Token = "private-agent-token"
			content := test.content
			if content == "" {
				content = "dns: {enable: true, listen: '127.0.0.1:1053'}"
			}
			confirmedDNSFixture(t, cfg, content, nil)
			if test.change != nil {
				test.change(t, cfg)
			}
			id := test.query
			if id == "" {
				id = "dns-source"
			} else if id == "EMPTY" {
				id = ""
			}
			w := callDNSRoute(t, cfg, "/api/dns/listener?deployment_id="+id, cfg.Token)
			if w.Code != test.status || !strings.Contains(w.Body.String(), test.message) {
				t.Fatalf("expected %d with %s: %d %s", test.status, test.message, w.Code, w.Body.String())
			}
		})
	}
}

func TestDNSFollowListenerBlocksConcurrentServiceOperation(t *testing.T) {
	cfg := testDeploymentConfig(t)
	cfg.Token = "private-agent-token"
	confirmedDNSFixture(t, cfg, "dns: {enable: true, listen: ':1053'}", nil)
	release, err := AcquireServiceOperation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	w := callDNSRoute(t, cfg, "/api/dns/listener?deployment_id=dns-source", cfg.Token)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "其他操作") {
		t.Fatalf("concurrent mutation was not blocked: %d %s", w.Code, w.Body.String())
	}
}

func TestDNSFollowListenerAcceptsLargePublishedConfiguration(t *testing.T) {
	cfg := testDeploymentConfig(t)
	cfg.Token = "private-agent-token"
	content := "dns: {enable: true, listen: ':5353'}\n# " + strings.Repeat("x", 4<<20) + "\n"
	confirmedDNSFixture(t, cfg, content, nil)
	w := callDNSRoute(t, cfg, "/api/dns/listener?deployment_id=dns-source", cfg.Token)
	if w.Code != 200 {
		t.Fatalf("valid published configuration was rejected by an inspection-only size bound: %d %s", w.Code, w.Body.String())
	}
}

func TestDNSFollowListenerParsesPortsStrictly(t *testing.T) {
	for _, listen := range []string{":53", "0.0.0.0:5353", "127.0.0.1:65535", "[::]:7874", "[::1]:1053"} {
		got, host, _, err := parseFollowListener([]byte(fmt.Sprintf("dns: {enable: true, listen: '%s'}", listen)))
		if err != nil || got != listen {
			t.Errorf("rejected listener %s: %v", listen, err)
		}
		if strings.HasPrefix(listen, ":") && host != "0.0.0.0" {
			t.Errorf("wildcard host was not normalized: %s", host)
		}
	}
	for _, listen := range []string{"127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:-1", "127.0.0.1:+53", "127.0.0.1:dns", "127.0.0.1:53.0", "[::1]", "udp://127.0.0.1:53", "private@host:53", "127.0.0.1: 53"} {
		if _, _, _, err := parseFollowListener([]byte(fmt.Sprintf("dns: {enable: true, listen: '%s'}", listen))); err == nil {
			t.Errorf("accepted malformed listener %s", listen)
		}
	}
}

func TestDNSFollowActiveVerifiesEveryManagedFile(t *testing.T) {
	for _, changed := range []string{"", "rules/domains.txt", "config.yaml", "wrong-id"} {
		t.Run(changed, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			cfg.ServiceType, cfg.Token = "oxidns", "private-agent-token"
			// The old DNS upstream and HTTP health endpoint may be unavailable
			// while the new source port is being propagated. Only the target
			// process must be alive so this check cannot block its own repair.
			cfg.HealthURL, cfg.HealthDNSAddress = "http://127.0.0.1:1", "127.0.0.1:1"
			confirmedDNSFixture(t, cfg, "plugins: []", map[string]string{"rules/domains.txt": "example.org\n"})
			id := "dns-source"
			if changed == "wrong-id" {
				id = "another"
			} else if changed != "" {
				if err := os.WriteFile(filepath.Join(filepath.Dir(cfg.ConfigPath), changed), []byte("manual edit"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			w := callDNSRoute(t, cfg, "/api/dns/active?deployment_id="+id, cfg.Token)
			want := 200
			if changed != "" {
				want = 409
			}
			if w.Code != want {
				t.Fatalf("active verification: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

// OxiDNS rewrites its download targets on scheduled, manual and rule-sync
// refreshes without a new publication. Only those rule files may change.
func TestDNSFollowActiveAcceptsCoreRefreshedRuleFiles(t *testing.T) {
	config := `plugins:
- tag: rules_download
  type: download
  args:
    downloads:
    - {url: "https://config.test/a", dir: rules, filename: refreshed.txt}
    - {url: "https://config.test/b", dir: ./rules, filename: attachment.txt}
- tag: rules_reload
  type: reload_provider
  args: [$refreshed]
`
	for _, test := range []struct {
		name string
		edit func(t *testing.T, root string)
		want int
	}{
		{"refreshed rule bytes", func(t *testing.T, root string) {
			writeTestFile(t, filepath.Join(root, "rules/refreshed.txt"), "# no ip rules")
		}, 200},
		{"rule not downloaded by the core", func(t *testing.T, root string) {
			writeTestFile(t, filepath.Join(root, "rules/inline.txt"), "manual edit")
		}, 409},
		{"download target published as an attachment", func(t *testing.T, root string) {
			writeTestFile(t, filepath.Join(root, "rules/attachment.txt"), "manual edit")
		}, 409},
		{"refreshed rule missing", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "rules/refreshed.txt")); err != nil {
				t.Fatal(err)
			}
		}, 409},
		{"refreshed rule replaced by a symlink", func(t *testing.T, root string) {
			target := filepath.Join(t.TempDir(), "outside.txt")
			writeTestFile(t, target, "outside")
			name := filepath.Join(root, "rules/refreshed.txt")
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, name); err != nil {
				t.Fatal(err)
			}
		}, 409},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			cfg.ServiceType, cfg.Token = "oxidns", "private-agent-token"
			cfg.HealthURL, cfg.HealthDNSAddress = "http://127.0.0.1:1", "127.0.0.1:1"
			confirmedDNSFixture(t, cfg, config, map[string]string{
				"rules/refreshed.txt": "", "rules/inline.txt": "full:inline.test", "rules/attachment.txt": "full:a.test"})
			// The attachment shares a download target but was published as a
			// custom file; its bytes remain owned by the publication.
			markManifestRole(t, cfg, "rules/attachment.txt", "custom")
			test.edit(t, filepath.Dir(cfg.ConfigPath))
			w := callDNSRoute(t, cfg, "/api/dns/active?deployment_id=dns-source", cfg.Token)
			if w.Code != test.want {
				t.Fatalf("active verification: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func writeTestFile(t *testing.T, name, value string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func markManifestRole(t *testing.T, cfg *Config, name, role string) {
	t.Helper()
	path := filepath.Join(deploymentRoot(cfg), "active-manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest DeploymentManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Files {
		if manifest.Files[i].Path == name {
			manifest.Files[i].Role = role
		}
	}
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	if err := durableWrite(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDNSFollowActiveRequiresRunningTarget(t *testing.T) {
	cfg := testDeploymentConfig(t)
	cfg.ServiceType, cfg.Token, cfg.StatusCommand = "oxidns", "private-agent-token", "exit 1"
	confirmedDNSFixture(t, cfg, "plugins: []", nil)
	w := callDNSRoute(t, cfg, "/api/dns/active?deployment_id=dns-source", cfg.Token)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "OxiDNS 未运行") {
		t.Fatalf("stopped target accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestDNSFollowPeerRequiresManagedSameNamespaceEvidence(t *testing.T) {
	for _, native := range []bool{true, false} {
		for _, scenario := range []string{"valid", "no-aio", "wrong-path", "wrong-service", "unregistered", "same-id", "other-panel", "unreadable", "wrong-manager", "legacy-manager"} {
			t.Run(fmt.Sprintf("native=%v/%s", native, scenario), func(t *testing.T) {
				t.Setenv("ENABLE_MIHOMO", "true")
				t.Setenv("ENABLE_OXIDNS", "true")
				cfg := &Config{ServiceType: "oxidns", AgentID: "target-agent", NativeAIO: native, ConfigPath: "/etc/oxidns/config.yaml", ServerURL: "https://panel.example"}
				peer := Config{ServiceType: "mihomo", AgentID: "source-agent", NativeAIO: native, ConfigPath: "/etc/mihomo/config.yaml", ServerURL: cfg.ServerURL}
				peerPath := "/opt/configflow-agent/config-mihomo.json"
				if native {
					cfg.AgentConfigFile, cfg.AgentServiceUnit, cfg.ServiceManager = "/opt/configflow-agent/oxidns/config.json", "configflow-agent-oxidns", "systemd"
					peer.AgentServiceUnit, peer.ServiceManager = "configflow-agent-mihomo", "systemd"
					peerPath = "/opt/configflow-agent/mihomo/config.json"
				} else {
					cfg.AgentConfigFile, cfg.ServiceManager, cfg.ServiceUnit = "/opt/configflow-agent/config-oxidns.json", "s6", "oxidns"
					peer.ServiceManager, peer.ServiceUnit = "s6", "mihomo"
				}
				switch scenario {
				case "no-aio":
					cfg.NativeAIO = false
					t.Setenv("ENABLE_MIHOMO", "false")
				case "wrong-path":
					cfg.AgentConfigFile = "/tmp/config-oxidns.json"
				case "wrong-service":
					peer.ServiceType = "oxidns"
				case "unregistered":
					peer.AgentID = ""
				case "same-id":
					peer.AgentID = cfg.AgentID
				case "other-panel":
					peer.ServerURL = "https://other.example"
				case "wrong-manager":
					peer.ServiceManager = "command"
				case "legacy-manager":
					// A peer configuration written by an image before 1.9.3-go.
					if !native {
						peer.ServiceManager = "supervisor"
					}
				}
				read := func(path string) ([]byte, error) {
					if path != peerPath {
						t.Fatalf("attempted unexpected metadata path %s", path)
					}
					if scenario == "unreadable" {
						return nil, os.ErrNotExist
					}
					return json.Marshal(peer)
				}
				id, err := sameNamespaceDNSPeer(cfg, read)
				if (scenario == "valid" || scenario == "legacy-manager") && (err != nil || id != peer.AgentID) {
					t.Fatalf("valid peer not detected: %s %v", id, err)
				}
				if scenario != "valid" && scenario != "legacy-manager" && (err == nil || id != "") {
					t.Fatalf("unverified peer accepted: %s %v", id, err)
				}
			})
		}
	}
}
