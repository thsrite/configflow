package routes

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func oxidnsTestDNS(t *testing.T) string {
	t.Helper()
	socket, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { socket.Close() })
	go func() {
		for {
			buf := make([]byte, 2048)
			n, addr, err := socket.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}
			end := 12
			for end < n && buf[end] != 0 {
				end += int(buf[end]) + 1
			}
			end += 5
			if end > n {
				continue
			}
			response := append([]byte(nil), buf[:end]...)
			response[2], response[3], response[6], response[7], response[10], response[11] = 0x81, 0x80, 0, 1, 0, 0
			response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 192, 0, 2, 1)
			socket.WriteTo(response, addr)
		}
	}()
	return socket.LocalAddr().String()
}

func testOxiDeployment(t *testing.T) (*Config, string) {
	cfg := testDeploymentConfig(t)
	cfg.ServiceType = "oxidns"
	cfg.HealthURL = ""
	dns := oxidnsTestDNS(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "tester" || p != "secret" || r.URL.Path != "/api/readyz" {
			w.WriteHeader(401)
			return
		}
		data, _ := os.ReadFile(cfg.ConfigPath)
		if bytes.Contains(data, []byte("reject-start: true")) {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"status":"ready"}`)
	}))
	t.Cleanup(server.Close)
	config := fmt.Sprintf("api:\n  http:\n    listen: %s\n    auth: {type: basic, username: tester, password: secret}\nplugins:\n  - tag: dns\n    type: udp_server\n    args: {listen: '%s', entry: main}\n  - tag: main\n    type: sequence\n    args: []\n  - tag: learned\n    type: dynamic_domain_set\n    args: {path: './state/learned.txt'}\n", strings.TrimPrefix(server.URL, "http://"), dns)
	return cfg, config
}

func TestOxiDNSDeploymentNativeCheckAndHealthRollbackKeepState(t *testing.T) {
	cfg, content := testOxiDeployment(t)
	root := filepath.Dir(cfg.ConfigPath)
	os.MkdirAll(filepath.Join(root, "state"), 0700)
	state := filepath.Join(root, "state/learned.txt")
	os.WriteFile(state, []byte("domain:keep.test"), 0600)
	// Verify the native validator's exact CLI and working directory.
	script := "#!/bin/sh\n[ \"$1\" = check ] && [ \"$2\" = -c ] && [ \"$4\" = -d ] && [ \"$PWD\" = \"$5\" ] || exit 91\n[ -d state ] || exit 92\nif grep -q reject-check: \"$3\"; then exit 1; fi\n"
	os.WriteFile(cfg.ServiceBinary, []byte(script), 0700)
	first := submitBundle(t, cfg, "oxi-good", map[string][]byte{"config.yaml": []byte(content), "rules/old.txt": []byte("full:old.test")}, true)
	if first.Status != "succeeded" {
		t.Fatalf("good: %+v", first)
	}
	before, _ := os.ReadFile(cfg.ConfigPath)
	invalid := submitBundle(t, cfg, "oxi-invalid", map[string][]byte{"config.yaml": []byte(content + "reject-check: true\n")}, true)
	if invalid.Status != "failed" {
		t.Fatalf("invalid: %+v", invalid)
	}
	unchanged, _ := os.ReadFile(cfg.ConfigPath)
	if !bytes.Equal(before, unchanged) {
		t.Fatal("native check failure changed live configuration")
	}
	failed := submitBundle(t, cfg, "oxi-health-fail", map[string][]byte{"config.yaml": []byte(content + "reject-start: true\n"), "rules/new.txt": []byte("full:new.test")}, true)
	if failed.Status != "rolled_back" {
		t.Fatalf("health: %+v", failed)
	}
	restored, _ := os.ReadFile(cfg.ConfigPath)
	if !bytes.Equal(before, restored) {
		t.Fatal("rollback did not restore configuration")
	}
	if data, _ := os.ReadFile(state); string(data) != "domain:keep.test" {
		t.Fatal("runtime state overwritten")
	}
	if _, err := os.Stat(filepath.Join(root, "rules/new.txt")); !os.IsNotExist(err) {
		t.Fatal("new managed file survived rollback")
	}
	if _, err := os.Stat(filepath.Join(root, "rules/old.txt")); err != nil {
		t.Fatal("old managed file missing after rollback")
	}
}

func TestOxiDNSRejectManagedState(t *testing.T) {
	cfg, content := testOxiDeployment(t)
	result := submitBundle(t, cfg, "oxi-state", map[string][]byte{"config.yaml": []byte(content), "state/learned.txt": []byte("overwrite")}, false)
	if result.Status != "failed" {
		t.Fatalf("managed state accepted: %+v", result)
	}
}

func TestOxiDNSHealthCredentialsStayOnConfiguredEndpoint(t *testing.T) {
	doc := map[string]any{"api": map[string]any{"http": map[string]any{"listen": "0.0.0.0:9199", "auth": map[string]any{"type": "basic", "username": "tester", "password": "secret"}}}}
	target, u, p, err := oxidnsHealthEndpoint(doc, "")
	if err != nil || target != "http://127.0.0.1:9199/api/readyz" || u != "tester" || p != "secret" {
		t.Fatalf("bad derived health: %s %v", target, err)
	}
	_, u, p, err = oxidnsHealthEndpoint(doc, "http://other.invalid:9199/api/readyz")
	if err != nil || u != "" || p != "" {
		t.Fatal("credentials leaked to health override")
	}
}

func TestOxiDNSFirstStartCreatesRuntimeParentsWithoutOverwritingFiles(t *testing.T) {
	root := t.TempDir()
	cfg := &Config{ServiceType: "oxidns", ConfigPath: filepath.Join(root, "config.yaml")}
	os.WriteFile(cfg.ConfigPath, []byte("plugins:\n- type: query_recorder\n  args: {path: './state/queries.db'}\n- type: dynamic_domain_set\n  args: {path: './state/learned.txt'}\n"), 0600)
	if err := prepareOxiDNSRuntimeDirectories(cfg); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "state/learned.txt"), []byte("keep"), 0600)
	if err := prepareOxiDNSRuntimeDirectories(cfg); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "state/learned.txt")); string(data) != "keep" {
		t.Fatal("runtime data changed")
	}
}

func TestOxiDNSSystemdDirectoryMustMatchManagedRoot(t *testing.T) {
	for _, tc := range []struct {
		properties string
		valid      bool
	}{
		{"ExecStart={ path=/usr/bin/oxidns ; argv[]=/usr/bin/oxidns start -c /etc/oxidns/config.yaml -d /etc/oxidns ; ignore_errors=no ; }\nWorkingDirectory=/", true},
		{"ExecStart={ argv[]=/usr/bin/oxidns start -c /etc/oxidns/config.yaml -d /var/lib/oxidns ; }\nWorkingDirectory=/", false},
		{"ExecStart={ argv[]=/usr/bin/oxidns start -c /etc/oxidns/config.yaml ; }\nWorkingDirectory=/etc/oxidns", true},
		{"ExecStart={ argv[]=/usr/bin/oxidns start -c /etc/oxidns/other.yaml -d /etc/oxidns ; }\nWorkingDirectory=/", false},
		{"ExecStart={ argv[]=/usr/bin/oxidns start --config=./config.yaml --working-dir=/etc/oxidns ; }\nWorkingDirectory=/", true},
		{"ExecStart={ argv[]=/usr/bin/oxidns start --config=../other/config.yaml -d /etc/oxidns ; }\nWorkingDirectory=/", false},
		{"ExecStart={ argv[]=/usr/bin/oxidns start -cother.yaml -d/etc/oxidns ; }\nWorkingDirectory=/", false},
		{"ExecStart={ argv[]=/usr/bin/oxidns start -cconfig.yaml -d/etc/oxidns ; }\nWorkingDirectory=/", true},
		{"ExecStart={ argv[]=/usr/bin/oxidns start -d /etc/oxidns ; }\nWorkingDirectory=/", true},
		{"ExecStart=\nWorkingDirectory=", false},
	} {
		err := validateOxiDNSSystemdDirectory(tc.properties, "/etc/oxidns/config.yaml")
		if (err == nil) != tc.valid {
			t.Fatalf("properties=%s error=%v", tc.properties, err)
		}
	}
	if err := validateOxiDNSSystemdDirectory("ExecStart={ argv[]=/usr/bin/oxidns start -d /etc/oxidns ; }\nWorkingDirectory=/", "/etc/oxidns/other.yaml"); err == nil {
		t.Fatal("default config.yaml accepted for a different Agent filename")
	}
}

func TestOxiDNSTLSHealthDoesNotDisableCertificateVerification(t *testing.T) {
	doc := map[string]any{"api": map[string]any{"http": map[string]any{"listen": "0.0.0.0:9199", "ssl": map[string]any{"cert": "server.crt", "key": "server.key"}}}}
	target, _, _, err := oxidnsHealthEndpoint(doc, "")
	if err != nil || target != "" {
		t.Fatal("TLS auto health must use DNS/service rather than insecure loopback HTTPS")
	}
	target, _, _, err = oxidnsHealthEndpoint(doc, "https://dns.example.test:9199/api/readyz")
	if err != nil || target != "https://dns.example.test:9199/api/readyz" {
		t.Fatal("explicit certificate-valid endpoint lost")
	}
}

func TestOxiDNSNativeManagedTLSAndWebUIResources(t *testing.T) {
	cfg, content := testOxiDeployment(t)
	content += "extra: retained\n"
	// Add read-only resources without depending on a live TLS listener. The
	// fixture core validates transaction plumbing; a real-core suite covers PEM.
	content = strings.Replace(content, "    auth:", "    webui: {root: './webui'}\n    auth:", 1)
	content += "# managed resources\n"
	content = strings.Replace(content, "plugins:\n", "plugins:\n  - tag: dot\n    type: tcp_server\n    args: {cert: './tls/server.crt', key: './tls/server.key'}\n  - tag: ros\n    type: ros_address_list\n    args: {tls: {ca: './tls/ca.pem'}}\n", 1)
	resources := map[string][]byte{
		"config.yaml": []byte(content), "tls/server.crt": []byte("certificate"),
		"tls/server.key": []byte("private key"), "tls/ca.pem": []byte("CA"),
		"webui/index.html": []byte("<html>test</html>"),
	}
	for _, missing := range []string{"tls/server.crt", "tls/server.key", "tls/ca.pem", "webui/index.html"} {
		value := resources[missing]
		delete(resources, missing)
		result := submitBundle(t, cfg, "missing-"+strings.NewReplacer("/", "-", ".", "-").Replace(missing), resources, false)
		if result.Status != "failed" {
			t.Fatalf("missing %s accepted: %+v", missing, result)
		}
		resources[missing] = value
	}
	result := submitBundle(t, cfg, "all-native-assets", resources, false)
	if result.Status != "ready" {
		t.Fatalf("managed native resources: %+v", result)
	}
}

func TestOxiDNSAbsoluteAndEnvironmentStateAreCheckedInIsolation(t *testing.T) {
	cfg, content := testOxiDeployment(t)
	live := filepath.Join(t.TempDir(), "learned.txt")
	if err := os.WriteFile(live, []byte("live state untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	// Runtime paths are isolated, but their original expressions remain in inert
	// metadata for the official core's environment expansion. Only the original
	// document is eventually committed to the service.
	t.Setenv("EXTERNAL_DB", filepath.Join(t.TempDir(), "records.sqlite"))
	script := "#!/bin/sh\n[ \"$1\" = check ] || exit 91\ngrep -q '.configflow-check-state/' \"$3\" || exit 92\ngrep -Fq '${EXTERNAL_DB}' \"$3\" || exit 93\ngrep -q '^__configflow_check_runtime_expressions:' \"$3\" || exit 94\n"
	if err := os.WriteFile(cfg.ServiceBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	content = strings.Replace(content, "'./state/learned.txt'", "'"+live+"'", 1)
	content += "  - tag: records\n    type: query_recorder\n    args: {path: '${EXTERNAL_DB}'}\n"
	result := submitBundle(t, cfg, "external-runtime", map[string][]byte{"config.yaml": []byte(content)}, true)
	if result.Status != "succeeded" {
		t.Fatalf("isolated external runtime: %+v", result)
	}
	actual, err := os.ReadFile(cfg.ConfigPath)
	if err != nil || string(actual) != content {
		t.Fatal("shadow configuration leaked into installed config")
	}
	if actual, err := os.ReadFile(live); err != nil || string(actual) != "live state untouched" {
		t.Fatal("live external state changed")
	}
}

func TestOxiDNSAllowsExplicitTargetResourcesAndRejectsAbsoluteManagedCollision(t *testing.T) {
	cfg, content := testOxiDeployment(t)
	content = strings.Replace(content, "plugins:\n", "plugins:\n  - tag: installed\n    type: domain_set\n    args: {files: ['/etc/oxidns/shared.txt', '${INSTALLED_RULES}']}\n", 1)
	result := submitBundle(t, cfg, "external-rules", map[string][]byte{"config.yaml": []byte(content)}, false)
	if result.Status != "ready" {
		t.Fatalf("target-owned resources: %+v", result)
	}
	collision := filepath.Join(filepath.Dir(cfg.ConfigPath), "rules/collision.txt")
	content = strings.Replace(content, "'./state/learned.txt'", "'"+collision+"'", 1)
	result = submitBundle(t, cfg, "absolute-collision", map[string][]byte{"config.yaml": []byte(content), "rules/collision.txt": []byte("managed")}, false)
	if result.Status != "failed" {
		t.Fatalf("absolute runtime collision accepted: %+v", result)
	}
}

func TestOxiDNSActivatedAssetsArePrivateAndExistingRuleModeIsPreserved(t *testing.T) {
	cfg, content := testOxiDeployment(t)
	root := filepath.Dir(cfg.ConfigPath)
	for _, directory := range []string{"tls", "rules"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "tls/existing.key"), []byte("old key"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rules/existing.txt"), []byte("old rule"), 0640); err != nil {
		t.Fatal(err)
	}
	assets := map[string][]byte{
		"config.yaml": []byte(content), "tls/new.key": []byte("new private key"),
		"tls/existing.key": []byte("replacement private key"), "plugins/credentials.yaml": []byte("password: private"),
		"rules/existing.txt": []byte("full:example.test"), "rules/new.txt": []byte("full:new.test"),
	}
	roles := func(manifest *DeploymentManifest) {
		for index := range manifest.Files {
			if strings.HasPrefix(manifest.Files[index].Path, "tls/") || strings.HasPrefix(manifest.Files[index].Path, "plugins/") {
				manifest.Files[index].Role = "custom"
			}
		}
	}
	result := submitBundle(t, cfg, "private-native-assets", assets, true, roles)
	if result.Status != "succeeded" {
		t.Fatalf("activation failed: %+v", result)
	}
	configInfo, err := os.Stat(cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	uid, gid := fileOwnership(configInfo)
	for path := range assets {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		expected := os.FileMode(0600)
		if path == "rules/existing.txt" {
			expected = 0640
		}
		if info.Mode().Perm() != expected {
			t.Fatalf("%s installed as %o; want %o", path, info.Mode().Perm(), expected)
		}
		owner, group := fileOwnership(info)
		if owner != uid || group != gid {
			t.Fatalf("%s does not belong to the core owner", path)
		}
	}
}
