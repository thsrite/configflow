package installer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"configflow/internal/yamlx"
)

func l1Docker(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", append([]string{"-n", "docker"}, args...)...)
	// Let sudo forward termination to Docker, and bound pipe draining if a
	// daemonized fixture inherited descriptors. Cleanup removes owned containers.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	return cmd.CombinedOutput()
}

func TestL1CompactMountsWithRealCompose(t *testing.T) {
	if os.Getenv("CONFIGFLOW_L1_CONTAINERS") != "1" {
		t.Skip("set CONFIGFLOW_L1_CONTAINERS=1 on the test server")
	}
	version, err := l1Docker(t, "compose", "version", "--short")
	if err != nil {
		t.Fatalf("Compose version: %v: %s", err, version)
	}
	var major, minor, patch int
	if _, err := fmt.Sscanf(strings.TrimPrefix(strings.TrimSpace(string(version)), "v"), "%d.%d.%d", &major, &minor, &patch); err != nil ||
		major < 2 || major == 2 && (minor < 20 || minor == 20 && patch < 3) {
		t.Fatalf("Compose >= 2.20.3 required, got %q (%v)", version, err)
	}
	t.Logf("real Docker Compose %s", strings.TrimSpace(string(version)))
	tmp := t.TempDir()
	data := filepath.Join(tmp, "data $literal")
	if err := os.Mkdir(data, 0o755); err != nil {
		t.Fatal(err)
	}
	request := composeParams(tmp, "install_method", "compose_compact", "data_dir", data)
	guide := composeGuide(t, request, composeCapability("https://config.example"))
	document, err := yamlx.Parse([]byte(guide.GetString("compose")))
	if err != nil {
		t.Fatal(err)
	}
	var generated struct {
		Services map[string]struct {
			Volumes []any `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yamlx.Decode(document, &generated); err != nil {
		t.Fatal(err)
	}
	// Keep the generated mount specifications, but run only a harmless image
	// with no network/capabilities. This isolates the Docker bind-format boundary
	// from enrollment, VIP setup, host namespaces and core startup.
	probe := func(name string, volumes []any, wantSuccess bool) {
		t.Helper()
		project := "cfl1d1-" + strings.ToLower(rand.Text()[:12]) + "-" + name
		file := filepath.Join(tmp, name+".json")
		body := map[string]any{"services": map[string]any{"probe": map[string]any{
			"image": "alpine:3", "network_mode": "none", "cap_drop": []string{"ALL"},
			"command": []string{"sh", "-ec", "test -d /opt/configflow-agent; test -d /etc/oxidns; test -d /var/lib/configflow-ha"},
			"volumes": volumes,
		}}}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, file, string(raw), 0o644)
		t.Cleanup(func() {
			if out, err := l1Docker(t, "compose", "-p", project, "-f", file, "down", "--remove-orphans"); err != nil {
				t.Errorf("cleanup %s: %v: %s", project, err, out)
			}
		})
		out, err := l1Docker(t, "compose", "-p", project, "-f", file, "up", "--abort-on-container-exit", "--exit-code-from", "probe")
		if wantSuccess && err != nil {
			t.Fatalf("generated %s mounts: %v: %s", name, err, out)
		}
		if !wantSuccess && (err == nil || !strings.Contains(string(out), "invalid volume specification")) {
			t.Fatalf("old colon mount must fail at Docker's bind parser: %v: %s", err, out)
		}
	}
	for _, role := range []string{"agent", "ha-companion"} {
		volumes := generated.Services[role].Volumes
		if len(volumes) < 3 {
			t.Fatalf("missing %s mounts", role)
		}
		probe(role, volumes[:3], true)
	}
	colon := filepath.Join(tmp, "data:old")
	if err := os.Mkdir(colon, 0o755); err != nil {
		t.Fatal(err)
	}
	var old []any
	for _, item := range [][2]string{{"agent", "/opt/configflow-agent"}, {"oxidns", "/etc/oxidns"}, {"ha", "/var/lib/configflow-ha"}} {
		old = append(old, map[string]any{"type": "bind", "source": colon + "/" + item[0], "target": item[1], "bind": map[string]bool{"create_host_path": true}})
	}
	probe("old-colon", old, false)
	t.Log("L1 D1: generated normal mounts ran; legacy colon/create_host_path mounts failed as expected")
}
