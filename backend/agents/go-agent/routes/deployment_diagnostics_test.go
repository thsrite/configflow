package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"agent/svc"
)

func TestDiagnosticCommandCapturesReasonAndUnwrapsExitStatus(t *testing.T) {
	_, err := runDiagnosticCommand(context.Background(), exec.Command("sh", "-c", "printf 'INFO beginning check\n'; printf 'ERROR rule 12: proxy group Missing not found\n' >&2; exit 7"), "Mihomo configuration check")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("exit cause lost: %v", err)
	}
	summary, details := deploymentErrorFields(fmt.Errorf("activation failed: %w", err))
	if !strings.Contains(summary, "rule 12") || !strings.Contains(details, "INFO beginning check") || !strings.Contains(details, "exit status 7") {
		t.Fatalf("diagnostics missing: %q / %q", summary, details)
	}
}

func TestDiagnosticCommandMissingBinaryAndTimeout(t *testing.T) {
	_, err := runDiagnosticCommand(context.Background(), exec.Command(filepath.Join(t.TempDir(), "missing-core")), "Mihomo configuration check")
	summary, details := deploymentErrorFields(err)
	if !strings.Contains(summary, "no such file") || !strings.Contains(details, "missing-core") {
		t.Fatalf("missing binary: %q %q", summary, details)
	}
	// An already-expired deadline is deterministic even when the runner cannot
	// schedule a new process promptly. Output retention is exercised below,
	// where cancellation starts only after the child has written its message.
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	_, err = runDiagnosticCommand(ctx, exec.CommandContext(ctx, "sh", "-c", "exec sleep 5"), "Mihomo configuration check")
	summary, details = deploymentErrorFields(err)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(summary, "timed out") || !strings.Contains(details, "context deadline exceeded") {
		t.Fatalf("timeout diagnostics: %q %q", summary, details)
	}
}

func TestDiagnosticPipesDoNotExtendTimeoutOrRejectSuccessfulParent(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint(timeout), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready, signal, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer ready.Close()
			defer signal.Close()
			// fd 3 acknowledges that stdout was written. Process startup time
			// must not consume a 100ms deadline on a busy CI runner. The long
			// lived child still holds stdout open to exercise WaitDelay.
			command := exec.CommandContext(ctx, "sh", "-c", "sleep 5 & printf 'check completed\\n'; printf x >&3; exit 0")
			var runContext context.Context
			if timeout {
				runContext = ctx
				command = exec.CommandContext(ctx, "sh", "-c", "sleep 5 & printf 'waiting for child\\n'; printf x >&3; wait")
			}
			command.ExtraFiles = []*os.File{signal}
			done := make(chan error, 1)
			go func() {
				_, err := runDiagnosticCommand(runContext, command, "native check")
				done <- err
			}()
			if err := ready.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var acknowledgement [1]byte
			if _, err := ready.Read(acknowledgement[:]); err != nil {
				t.Fatalf("child did not acknowledge output: %v", err)
			}
			if timeout {
				cancel()
			}
			select {
			case err = <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("inherited pipe extended command timeout")
			}
			if timeout {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
				_, detail := deploymentErrorFields(err)
				if !strings.Contains(detail, "waiting for child") {
					t.Fatal("timeout output lost")
				}
			} else if err != nil {
				t.Fatalf("successful parent treated as failure: %v", err)
			}
		})
	}
}

func TestKnownAgentTokenRedactedAtStatusAndRestartBoundaries(t *testing.T) {
	cfg := testDeploymentConfig(t)
	cfg.Token = "opaque-known-agent-credential"
	cfg.StatusCommand = "printf 'ERROR denied opaque-known-agent-credential\\n'; exit 4"
	_, err := serviceRunning(cfg)
	summary, detail := deploymentErrorFields(err)
	if strings.Contains(summary+detail, cfg.Token) || !strings.Contains(detail, "denied") {
		t.Fatalf("status leaked or lost reason: %s %s", summary, detail)
	}
	cfg.RestartCommand = cfg.StatusCommand
	response := httptest.NewRecorder()
	RestartHandler(cfg)(response, httptest.NewRequest(http.MethodPost, "/restart", nil))
	if strings.Contains(response.Body.String(), cfg.Token) || !strings.Contains(response.Body.String(), "denied") {
		t.Fatalf("restart leaked or lost reason: %s", response.Body)
	}
}

func TestDiagnosticOutputBoundedAndRetainsFinalError(t *testing.T) {
	var output diagnosticOutput
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&output, "INFO loading resource %04d\n", i)
	}
	fmt.Fprintln(&output, "FATAL invalid provider: 最后一条错误")
	_, details := deploymentErrorFields(diagnosticFailure("check failed", errors.New("exit status 1"), output.Bytes()))
	if len(output.data) > maxCommandDiagnosticOutput || len(details) > maxDeploymentErrorDetails || !utf8.ValidString(details) || !strings.Contains(details, "最后一条错误") {
		t.Fatalf("unbounded or missing terminal error: buffer=%d details=%d tail=%q", len(output.data), len(details), details[len(details)-100:])
	}
	var partial diagnosticOutput
	partial.Write([]byte("password=" + strings.Repeat("secret", 10000)))
	if strings.Contains(string(partial.Bytes()), "secret") {
		t.Fatal("partial oversized credential line retained")
	}
}

func TestTruncatedMultilineCredentialsDoNotLoseRedactionContext(t *testing.T) {
	for _, value := range []string{
		"password: |\n" + strings.Repeat("  dummy-multiline-secret\n", 2000),
		"-----BEGIN PRIVATE KEY-----\n" + strings.Repeat("dummy-multiline-secret\n", 2000) + "-----END PRIVATE KEY-----\n",
	} {
		var output diagnosticOutput
		output.Write([]byte(value + "ERROR final reason\n"))
		summary, details := deploymentErrorFields(diagnosticFailure("check failed", nil, output.Bytes()))
		if strings.Contains(summary+details, "dummy-multiline-secret") || !strings.Contains(summary+details, "final reason") {
			t.Fatalf("truncated credential leaked or reason lost: %q / %q", summary, details)
		}
	}
	err := diagnosticFailure("check failed", nil, []byte("ERROR dummy-multiline-secret"), "first-private-line\ndummy-multiline-secret\n")
	summary, details := deploymentErrorFields(err)
	if strings.Contains(summary+details, "dummy-multiline-secret") {
		t.Fatal("known secret continuation leaked")
	}
}

func TestDiagnosticRedactsCredentialsAndKnownBareValues(t *testing.T) {
	raw := `ERROR invalid provider https://username:url-password@example.com/rules?token=query-secret&label=unknown-secret
{"password":"json secret", "api_key":"key-secret", "secret": "quoted secret"}
Authorization: Bearer bearer-secret
token=plain-secret password='space secret'
password: |
  first-block-secret
  second-block-secret
next: legitimate
private-key: >-
  folded-block-secret
upstream trojan://share-secret@example.com:443
-----BEGIN PRIVATE KEY-----
private-data
-----END PRIVATE KEY-----
`
	details := sanitizeDiagnostic(raw)
	for _, secret := range []string{"username", "url-password", "query-secret", "unknown-secret", "json secret", "key-secret", "quoted secret", "bearer-secret", "plain-secret", "space secret", "share-secret", "private-data", "first-block-secret", "second-block-secret", "folded-block-secret"} {
		if strings.Contains(details, secret) {
			t.Fatalf("leaked %q: %s", secret, details)
		}
	}
	if !strings.Contains(details, "invalid provider") || !strings.Contains(details, "example.com/rules") {
		t.Fatal("non-sensitive diagnostics lost")
	}
	_, err := runDiagnosticCommand(context.Background(), exec.Command("sh", "-c", "printf 'invalid opaque-agent-credential'; exit 1"), "check", "opaque-agent-credential")
	summary, details := deploymentErrorFields(err)
	if strings.Contains(summary+details, "opaque-agent-credential") {
		t.Fatal("known unlabelled credential leaked")
	}
}

func TestNativeDeploymentFailureDetailsPersistAndLeaveLiveUntouched(t *testing.T) {
	for _, kind := range []string{"mihomo", "oxidns"} {
		t.Run(kind, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			content := "mode: rule\nsecret: private-core-secret\n"
			if kind == "oxidns" {
				cfg, content = testOxiDeployment(t)
				content += "secret: private-core-secret\n"
			}
			cfg.Token = "private-agent-token"
			writeActivationFixture(t, cfg.ServiceBinary, []byte("#!/bin/sh\nprintf 'ERROR unknown plugin invalid_plugin private-core-secret private-agent-token\\n' >&2\nexit 1\n"), 0700)
			writeActivationFixture(t, cfg.ConfigPath, []byte("old: configuration\n"), 0600)
			state := submitBundle(t, cfg, "native-failure", map[string][]byte{"config.yaml": []byte(content)}, true)
			if state.Status != "failed" || state.FailedStage != "verifying" || !strings.Contains(state.ErrorDetails, "invalid_plugin") {
				t.Fatalf("state %+v", state)
			}
			if state.BackupComplete {
				t.Fatal("native rejection reached activation")
			}
			live, _ := os.ReadFile(cfg.ConfigPath)
			if string(live) != "old: configuration\n" {
				t.Fatal("live configuration changed")
			}
			response := httptest.NewRecorder()
			DeploymentHandler(cfg)(response, httptest.NewRequest(http.MethodGet, "/api/deployments/native-failure", nil))
			for _, secret := range []string{"private-core-secret", "private-agent-token"} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("persisted credential: %s", response.Body)
				}
			}
			if !strings.Contains(response.Body.String(), "error_details") {
				t.Fatal("API lost persisted details")
			}
		})
	}
}

func TestMosDNSValidationAndYAMLReasonsAreSpecific(t *testing.T) {
	cfg := testDeploymentConfig(t)
	cfg.ServiceType = "mosdns"
	state := submitBundle(t, cfg, "bad-yaml", map[string][]byte{"config.yaml": []byte("plugins:\n - tag: [unterminated\n")}, true)
	if state.Status != "failed" || !strings.Contains(state.ErrorDetails, "line") {
		t.Fatalf("YAML location lost: %+v", state)
	}
	state = submitBundle(t, cfg, "duplicate-plugin", map[string][]byte{"config.yaml": []byte("plugins:\n - {tag: duplicate_name, type: sequence}\n - {tag: duplicate_name, type: sequence}\n")}, true)
	if state.Status != "failed" || !strings.Contains(state.Error, "duplicate_name") {
		t.Fatalf("plugin name lost: %+v", state)
	}
}

func TestAllServiceStartFailureDetailsAndRollback(t *testing.T) {
	for _, kind := range []string{"mihomo", "mosdns", "oxidns"} {
		t.Run(kind, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			content := "mode: rule\n"
			if kind == "oxidns" {
				cfg, content = testOxiDeployment(t)
			}
			if kind == "mosdns" {
				cfg.ServiceType = kind
				content = "plugins:\n - {tag: main, type: sequence, args: []}\n"
			}
			cfg.StartCommand = "printf 'ERROR listen 127.0.0.1:53: address already in use; password=start-secret\\n' >&2; exit 1"
			state := submitBundle(t, cfg, "start-failed", map[string][]byte{"config.yaml": []byte(content)}, true)
			if state.Status != "rolled_back" || state.FailedStage != "starting" || !strings.Contains(state.ErrorDetails, "address already in use") || strings.Contains(state.Error+state.ErrorDetails, "start-secret") {
				t.Fatalf("state %+v", state)
			}
			if state.RollbackErrorDetails != "" {
				t.Fatal("successful rollback left stale diagnostic")
			}
		})
	}
}

func TestRollbackDiagnosticsSurviveJournalAndClearAfterRecovery(t *testing.T) {
	cfg := testDeploymentConfig(t)
	cfg.StopCommand = "printf 'ERROR permission denied stopping service; token=stop-secret\\n'; exit 2"
	state := submitBundle(t, cfg, "stop-failed", map[string][]byte{"config.yaml": []byte("mode: rule\n")}, true)
	if state.Status != "rollback_failed" || !strings.Contains(state.ErrorDetails, "permission denied") || !strings.Contains(state.RollbackErrorDetails, "permission denied") {
		t.Fatalf("state %+v", state)
	}
	original := state.ErrorDetails
	cfg.StopCommand = "true"
	if err := RecoverDeployments(cfg, true); err != nil {
		t.Fatal(err)
	}
	state, err := loadDeployment(cfg, state.DeploymentID)
	if err != nil || state.Status != "rolled_back" || state.RollbackError != "" || state.RollbackErrorDetails != "" || state.ErrorDetails != original {
		t.Fatalf("recovery lost original error or retained stale rollback error: %+v %v", state, err)
	}
	state.Error, state.ErrorDetails, state.RollbackError, state.RollbackErrorDetails = "old", "old detail", "old rollback", "old rollback detail"
	if err := saveDeployment(cfg, state, "succeeded"); err != nil {
		t.Fatal(err)
	}
	state, _ = loadDeployment(cfg, state.DeploymentID)
	if state.Error+state.ErrorDetails+state.RollbackError+state.RollbackErrorDetails != "" {
		t.Fatal("success retained failure fields")
	}
}

func TestServiceManagerExitCodesStillRecognizedWithDiagnostics(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc", "command"} {
		t.Run(manager, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			cfg.ServiceManager, cfg.ServiceUnit = manager, "test-core"
			bin := t.TempDir()
			name := map[string]string{"systemd": "systemctl", "openrc": "rc-service"}[manager]
			if manager == "command" {
				cfg.StatusCommand = "printf 'inactive\\n'; exit 3"
			} else {
				writeActivationFixture(t, filepath.Join(bin, name), []byte("#!/bin/sh\nprintf 'inactive\\n'\nexit 3\n"), 0700)
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			if running, err := serviceRunning(cfg); err != nil || running {
				t.Fatalf("expected inactive status preserved: %t %v", running, err)
			}
			if manager == "command" {
				cfg.StatusCommand = "printf 'ERROR manager permission denied\\n'; exit 4"
			} else {
				writeActivationFixture(t, filepath.Join(bin, name), []byte("#!/bin/sh\nprintf 'ERROR manager permission denied\\n'\nexit 4\n"), 0700)
			}
			_, err := serviceRunning(cfg)
			_, details := deploymentErrorFields(err)
			if !strings.Contains(details, "manager permission denied") {
				t.Fatalf("manager diagnostics lost: %v %q", err, details)
			}
		})
	}
}

// fakeContainer is a scripted container service manager.
type fakeContainer struct {
	states map[string]svc.State
	err    error
	calls  []string
}

func (c *fakeContainer) act(name, action string, state svc.State) error {
	c.calls = append(c.calls, action+" "+name)
	if c.err != nil {
		return c.err
	}
	c.states[name] = state
	return nil
}
func (c *fakeContainer) Start(_ context.Context, name string) error {
	return c.act(name, "start", svc.StateRunning)
}
func (c *fakeContainer) Stop(_ context.Context, name string) error {
	return c.act(name, "stop", svc.StateStopped)
}
func (c *fakeContainer) Restart(_ context.Context, name string) error {
	return c.act(name, "restart", svc.StateRunning)
}
func (c *fakeContainer) Status(_ context.Context, name string) (svc.Status, error) {
	if c.err != nil {
		return svc.Status{}, c.err
	}
	state, ok := c.states[name]
	if !ok {
		return svc.Status{}, svc.ErrUnknown
	}
	return svc.Status{Name: name, State: state}, nil
}

func useFakeContainer(t *testing.T, states map[string]svc.State) *fakeContainer {
	t.Helper()
	fake := &fakeContainer{states: states}
	previous := containerServices
	t.Cleanup(func() { containerServices = previous })
	containerServices = fake
	return fake
}

func TestContainerServiceStates(t *testing.T) {
	tests := []struct {
		state   svc.State
		running bool
		err     error
	}{
		{svc.StateRunning, true, nil},
		{svc.StateStarting, false, errServiceTransition},
		{svc.StateBackoff, false, errServiceTransition},
		{svc.StateStopping, false, errServiceTransition},
		{svc.StateStopped, false, nil},
		{svc.StateFatal, false, nil},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			cfg.ServiceManager, cfg.ServiceUnit = "s6", "test-core"
			useFakeContainer(t, map[string]svc.State{"test-core": tt.state})
			running, err := serviceRunning(cfg)
			if running != tt.running || !errors.Is(err, tt.err) {
				t.Fatalf("%t %v", running, err)
			}
		})
	}
	cfg := testDeploymentConfig(t)
	cfg.ServiceManager, cfg.ServiceUnit = "s6", "test-core"
	fake := useFakeContainer(t, map[string]svc.State{"test-core": svc.StateRunning})
	if err := stopService(cfg); err != nil || fake.states["test-core"] != svc.StateStopped {
		t.Fatalf("stop: %v %v", err, fake.states)
	}
	// Stopping a stopped program is not an error.
	if err := stopService(cfg); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	fake.err = fmt.Errorf("manager permission denied")
	_, err := serviceRunning(cfg)
	if _, details := deploymentErrorFields(err); !strings.Contains(details, "manager permission denied") {
		t.Fatalf("manager diagnostics lost: %v %q", err, details)
	}
}

func TestOxiDNSReloadAndFallbackFailureDetails(t *testing.T) {
	cfg, _, render := testOxiReloadDeployment(t)
	cfg.StartCommand = "printf 'ERROR fallback startup rejected\\n'; exit 1"
	state := submitBundle(t, cfg, "reload-and-start-fail", map[string][]byte{"config.yaml": []byte(render("# reject-reload\n"))}, true)
	if state.Status != "rollback_failed" || !strings.Contains(state.ErrorDetails, "plugin assembly failed") || !strings.Contains(state.ErrorDetails, "fallback startup rejected") {
		t.Fatalf("combined cause missing: %+v", state)
	}
	encoded, _ := json.Marshal(state)
	if !strings.Contains(string(encoded), "rollback_error_details") {
		t.Fatal("rollback details not serialized")
	}
}

func TestRestartURLFailureResponseShowsSanitizedReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid module", "password":"http-password"}`)
	}))
	defer server.Close()
	cfg := &Config{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), RestartCommand: server.URL + "?token=url-token"}
	response := httptest.NewRecorder()
	RestartHandler(cfg)(response, httptest.NewRequest(http.MethodPost, "/restart", nil))
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "invalid module") || strings.Contains(response.Body.String(), "http-password") || strings.Contains(response.Body.String(), "url-token") {
		t.Fatalf("unsafe/missing response diagnostic: %s", response.Body)
	}
}
