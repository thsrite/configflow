package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Run explicitly with OXIDNS_TEST_BINARY=/path/to/oxidns. This exercises the
// actual binary and Agent transaction worker without touching a system service.
func TestOxiDNSNativeTransactionalDeployment(t *testing.T) {
	binary := os.Getenv("OXIDNS_TEST_BINARY")
	if binary == "" {
		t.Skip("set OXIDNS_TEST_BINARY to run the real OxiDNS lifecycle regression")
	}
	if runtime.GOOS == "windows" {
		t.Skip("native lifecycle fixture uses a POSIX shell")
	}
	absolute, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(absolute); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	// Bind both protocols while choosing a DNS port, then release them before
	// starting OxiDNS. Every listener is restricted to loopback.
	dnsPort := 0
	for attempt := 0; attempt < 20; attempt++ {
		tcp, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := tcp.Addr().(*net.TCPAddr).Port
		udp, udpErr := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
		tcp.Close()
		if udpErr == nil {
			udp.Close()
			dnsPort = port
			break
		}
	}
	if dnsPort == 0 {
		t.Fatal("could not reserve loopback DNS port")
	}
	api, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	apiAddress := api.Addr().String()
	api.Close()
	// The fixture's lifecycle script is outside the deployment manifest. Only
	// the process recorded in this private temporary directory is stopped.
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	script := fmt.Sprintf(`#!/bin/sh
set -eu
cd %s
alive() {
    [ -s core.pid ] || return 1
    pid=$(cat core.pid)
    kill -0 "$pid" 2>/dev/null || return 1
    if [ -r "/proc/$pid/stat" ]; then
        state=$(awk '{print $3}' "/proc/$pid/stat")
        [ "$state" != Z ] || return 1
    fi
}
case "$1" in
start)
    if alive; then exit 0; fi
    nohup %s start -c config.yaml -d %s > core.log 2>&1 < /dev/null &
    echo $! > core.pid
    ;;
status) alive ;;
stop)
    if alive; then
        pid=$(cat core.pid)
        kill "$pid" 2>/dev/null || true
        count=0
        while alive && [ "$count" -lt 100 ]; do
            sleep 0.02
            count=$((count + 1))
        done
        if alive; then kill -9 "$pid" 2>/dev/null || true; fi
    fi
    rm -f core.pid
    ;;
*) exit 2 ;;
esac
`, quote(root), quote(absolute), quote(root))
	lifecycle := filepath.Join(root, "lifecycle.sh")
	if err := os.WriteFile(lifecycle, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{AgentID: "native-oxi-test", ServiceType: "oxidns", ServiceName: "oxidns",
		ConfigPath: filepath.Join(root, "config.yaml"), ServiceManager: "command", ServiceBinary: absolute,
		StopCommand: quote(lifecycle) + " stop", StartCommand: quote(lifecycle) + " start",
		StatusCommand: quote(lifecycle) + " status", HealthDNSName: "health.test", DeploymentHealthTimeout: 2}
	t.Cleanup(func() {
		serviceCommand(cfg, "stop")
		if t.Failed() {
			if logs, err := os.ReadFile(filepath.Join(root, "core.log")); err == nil {
				t.Logf("OxiDNS fixture log:\n%s", logs)
			}
		}
	})
	if err := os.MkdirAll(filepath.Join(root, "state"), 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state/learned.txt")
	preserved := []byte("full:keep.native.test\n")
	if err := os.WriteFile(state, preserved, 0600); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf(`runtime:
  worker_threads: 2
log:
  level: info
api:
  http:
    listen: "%s"
    auth: {type: basic, username: native-test, password: fixture-password}
plugins:
  - tag: learned
    type: dynamic_domain_set
    args: {path: './state/learned.txt'}
  - tag: answer
    type: black_hole
    args: {mode: custom, ips: [192.0.2.55], short_circuit: true}
  - tag: main
    type: sequence
    args:
      - exec: $answer
  - tag: dns_udp
    type: udp_server
    args: {entry: main, listen: '127.0.0.1:%d'}
  - tag: dns_tcp
    type: tcp_server
    args: {entry: main, listen: '127.0.0.1:%d'}
`, apiAddress, dnsPort, dnsPort)
	first := submitBundle(t, cfg, "native-good", map[string][]byte{
		"config.yaml": []byte(content), "rules/old.txt": []byte("full:old.test\n")}, true)
	if first.Status != "succeeded" {
		t.Fatalf("real native deployment: %+v", first)
	}
	before, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	// This configuration starts and exposes readyz, but gives no successful A
	// answer. Only the real DNS health probe should force transactional rollback.
	badHealth := strings.Replace(content, "mode: custom, ips: [192.0.2.55]", "mode: refused", 1)
	failed := submitBundle(t, cfg, "native-unhealthy", map[string][]byte{
		"config.yaml": []byte(badHealth), "rules/new.txt": []byte("full:new.test\n")}, true)
	if failed.Status != "rolled_back" || failed.FailedStage != "checking" {
		t.Fatalf("real health failure should roll back: %+v", failed)
	}
	restored, _ := os.ReadFile(cfg.ConfigPath)
	if !bytes.Equal(before, restored) {
		t.Fatal("rollback did not restore exact live config")
	}
	if value, _ := os.ReadFile(state); !bytes.Equal(value, preserved) {
		t.Fatal("runtime learned state was overwritten")
	}
	if value, _ := os.ReadFile(filepath.Join(root, "rules/old.txt")); string(value) != "full:old.test\n" {
		t.Fatal("rollback did not restore previous managed rule")
	}
	if _, err := os.Stat(filepath.Join(root, "rules/new.txt")); !os.IsNotExist(err) {
		t.Fatal("failed revision left new managed file")
	}
	if err := deploymentProbe(cfg); err != nil {
		t.Fatalf("restored native core is unhealthy: %v", err)
	}

	// A successful revision can also be explicitly undone by the operator. The
	// previous failed revision must not become the source of this rollback.
	secondContent := strings.Replace(content, "192.0.2.55", "192.0.2.77", 1)
	second := submitBundle(t, cfg, "native-manual", map[string][]byte{
		"config.yaml": []byte(secondContent), "rules/manual.txt": []byte("full:manual.test\n")}, true)
	if second.Status != "succeeded" {
		t.Fatalf("successful revision before manual rollback: %+v", second)
	}
	current, err := os.ReadFile(cfg.ConfigPath)
	if err != nil || bytes.Equal(before, current) {
		t.Fatal("second successful revision did not replace live configuration")
	}
	// An older successful deployment is not active and cannot roll back a newer
	// one. Exercise the public handler rather than calling rollback internals.
	stale := httptest.NewRecorder()
	DeploymentHandler(cfg)(stale, httptest.NewRequest(http.MethodPost, "/api/deployments/native-good/rollback", nil))
	if stale.Code != http.StatusConflict {
		t.Fatalf("inactive revision rollback was not rejected: %d %s", stale.Code, stale.Body)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/deployments/native-manual/rollback", nil)
	accepted := httptest.NewRecorder()
	DeploymentHandler(cfg)(accepted, request)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("manual rollback not accepted: %d %s", accepted.Code, accepted.Body)
	}
	var acknowledgement map[string]any
	if err := json.Unmarshal(accepted.Body.Bytes(), &acknowledgement); err != nil || acknowledgement["status"] != "rolling_back" {
		t.Fatalf("manual rollback acknowledgement: %s", accepted.Body)
	}
	deadline := time.Now().Add(8 * time.Second)
	var final map[string]any
	for time.Now().Before(deadline) {
		response := httptest.NewRecorder()
		DeploymentHandler(cfg)(response, httptest.NewRequest(http.MethodGet, "/api/deployments/native-manual", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("manual rollback status: %d %s", response.Code, response.Body)
		}
		if err := json.Unmarshal(response.Body.Bytes(), &final); err != nil {
			t.Fatal(err)
		}
		if final["status"] == "rolled_back" || final["status"] == "rollback_failed" {
			if release, err := AcquireServiceOperation(cfg); err == nil {
				release()
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final["status"] != "rolled_back" || final["success"] != true || final["manual_rollback"] != true {
		t.Fatalf("manual rollback did not complete successfully: %+v", final)
	}
	restored, _ = os.ReadFile(cfg.ConfigPath)
	if !bytes.Equal(before, restored) {
		t.Fatal("manual rollback did not restore the previous successful configuration")
	}
	if value, _ := os.ReadFile(state); !bytes.Equal(value, preserved) {
		t.Fatal("manual rollback overwrote runtime learned state")
	}
	if value, _ := os.ReadFile(filepath.Join(root, "rules/old.txt")); string(value) != "full:old.test\n" {
		t.Fatal("manual rollback did not restore previous managed rule")
	}
	if _, err := os.Stat(filepath.Join(root, "rules/manual.txt")); !os.IsNotExist(err) {
		t.Fatal("manual rollback left the undone revision's managed rule")
	}
	active, err := os.ReadFile(filepath.Join(deploymentRoot(cfg), "active-manifest.json"))
	if err != nil || !bytes.Contains(active, []byte(`"deployment_id":"native-good"`)) {
		t.Fatal("manual rollback did not restore previous active manifest")
	}
	if err := deploymentProbe(cfg); err != nil {
		t.Fatalf("native core after manual rollback is unhealthy: %v", err)
	}

	// Keep this rejection after the manual rollback case: retention keeps only
	// two older completed journals, and the stale-success guard above must test
	// a retained deployment rather than an intentionally pruned history entry.
	// YAML and manifest are valid; only the real core knows this plugin is invalid.
	invalid := strings.Replace(content, "type: black_hole", "type: not_a_real_oxidns_plugin", 1)
	rejected := submitBundle(t, cfg, "native-invalid", map[string][]byte{"config.yaml": []byte(invalid)}, true)
	if rejected.Status != "failed" {
		t.Fatalf("native check should reject: %+v", rejected)
	}
	unchanged, _ := os.ReadFile(cfg.ConfigPath)
	if !bytes.Equal(before, unchanged) {
		t.Fatal("failed native validation changed live config")
	}
	if err := deploymentProbe(cfg); err != nil {
		t.Fatalf("original service after validation rejection: %v", err)
	}
}
