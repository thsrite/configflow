package routes

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const activationCheckScript = `#!/bin/sh
[ "$1" = check ] && [ "$2" = -c ] && [ "$4" = -d ] && [ "$PWD" = "$5" ] || exit 91
root=$(dirname "$0")
printf 'check\n' >> "$root/native-checks"
[ ! -f "$root/reject-check" ] || exit 1
exit 0
`

func writeActivationFixture(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
}

func activateReadyCandidate(t *testing.T, cfg *Config, id string) *DeploymentState {
	t.Helper()
	response := httptest.NewRecorder()
	DeploymentHandler(cfg)(response, httptest.NewRequest(http.MethodPost, "/api/deployments/"+id+"/activate", nil))
	if response.Code != http.StatusAccepted {
		t.Fatalf("activation request: %d %s", response.Code, response.Body)
	}
	return waitDeploymentState(t, cfg, id, true, func(state *DeploymentState) bool {
		return state.Status == "failed" || state.Status == "succeeded" || state.Status == "rolled_back" || state.Status == "rollback_failed"
	}, "activation "+id)
}

func TestOxiDNSReadyActivationRechecksCurrentCoreBeforeTouchingLive(t *testing.T) {
	for _, change := range []string{"replaced-core", "changed-environment"} {
		t.Run(change, func(t *testing.T) {
			cfg, content := testOxiDeployment(t)
			root := filepath.Dir(cfg.ConfigPath)
			cfg.StopCommand = "printf 'stop\\n' >> lifecycle-actions; rm -f running"
			cfg.StartCommand = "printf 'start\\n' >> lifecycle-actions; touch running"
			writeActivationFixture(t, cfg.ServiceBinary, []byte(activationCheckScript), 0700)
			original := []byte(content + "\n# existing live configuration\n")
			writeActivationFixture(t, cfg.ConfigPath, original, 0600)
			writeActivationFixture(t, filepath.Join(root, "running"), nil, 0600)
			writeActivationFixture(t, filepath.Join(root, "live-rule.txt"), []byte("keep-live-rule"), 0600)
			state := submitBundle(t, cfg, "ready-current-core", map[string][]byte{
				"config.yaml": []byte(content), "candidate-rule.txt": []byte("new-rule"),
			}, false)
			if state.Status != "ready" {
				t.Fatalf("candidate did not pass prepare check: %+v", state)
			}
			before, err := os.ReadFile(filepath.Join(root, "native-checks"))
			if err != nil || string(before) != "check\n" {
				t.Fatalf("prepare should run one native check: %q %v", before, err)
			}
			if change == "replaced-core" {
				// Same staged bytes, newly installed core no longer accepts them.
				writeActivationFixture(t, cfg.ServiceBinary, []byte(strings.Replace(activationCheckScript, "exit 0\n", "exit 1\n", 1)), 0700)
			} else {
				writeActivationFixture(t, filepath.Join(root, "reject-check"), nil, 0600)
			}
			state = activateReadyCandidate(t, cfg, state.DeploymentID)
			if state.Status != "failed" || state.FailedStage != "verifying" || !strings.Contains(state.Error, "native configuration check failed") {
				t.Fatalf("activation did not reject changed core/environment: %+v", state)
			}
			if state.BackupComplete || len(state.Backups) != 0 || state.ConfigVersion != "" {
				t.Fatalf("failed precheck advanced activation: %+v", state)
			}
			checks, err := os.ReadFile(filepath.Join(root, "native-checks"))
			if err != nil || string(checks) != "check\ncheck\n" {
				t.Fatalf("activation did not rerun the native core: %q %v", checks, err)
			}
			got, err := os.ReadFile(cfg.ConfigPath)
			if err != nil || !bytes.Equal(got, original) {
				t.Fatal("precheck failure replaced the live config")
			}
			if _, err := os.Stat(filepath.Join(root, "running")); err != nil {
				t.Fatal("precheck failure stopped the existing service")
			}
			if _, err := os.Stat(filepath.Join(root, "lifecycle-actions")); !os.IsNotExist(err) {
				t.Fatalf("precheck failure ran a lifecycle action: %v", err)
			}
			if rule, err := os.ReadFile(filepath.Join(root, "live-rule.txt")); err != nil || string(rule) != "keep-live-rule" {
				t.Fatal("precheck failure changed the live rule")
			}
			if _, err := os.Stat(filepath.Join(root, "candidate-rule.txt")); !os.IsNotExist(err) {
				t.Fatal("precheck failure installed a candidate file")
			}
			if _, err := os.Stat(filepath.Join(deploymentRoot(cfg), "active-manifest.json")); !os.IsNotExist(err) {
				t.Fatal("precheck failure recorded a live deployment")
			}
		})
	}
}

func TestOxiDNSDirectPublishPassesCurrentCoreCheckAndApplies(t *testing.T) {
	cfg, content := testOxiDeployment(t)
	root := filepath.Dir(cfg.ConfigPath)
	cfg.StopCommand = "printf 'stop\\n' >> lifecycle-actions; rm -f running"
	cfg.StartCommand = "printf 'start\\n' >> lifecycle-actions; touch running"
	writeActivationFixture(t, cfg.ServiceBinary, []byte(activationCheckScript), 0700)
	files := map[string][]byte{"config.yaml": []byte(content), "candidate-rule.txt": []byte("new-rule")}
	state := submitBundle(t, cfg, "publish-current-core", files, true)
	if state.Status != "succeeded" || state.ConfigVersion == "" {
		t.Fatalf("valid direct publish failed: %+v", state)
	}
	checks, err := os.ReadFile(filepath.Join(root, "native-checks"))
	if err != nil || string(checks) != "check\ncheck\n" {
		t.Fatalf("direct publish should check prepare and activation: %q %v", checks, err)
	}
	for path, expected := range files {
		actual, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("valid direct publish did not install %s: %v", path, err)
		}
	}
	if actions, err := os.ReadFile(filepath.Join(root, "lifecycle-actions")); err != nil || string(actions) != "stop\nstart\n" {
		t.Fatalf("unexpected publish lifecycle: %q %v", actions, err)
	}
}
