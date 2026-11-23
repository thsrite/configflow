package routes

import (
	"agent/internal/logx"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandForLogAlwaysRedactsNonURLCommand(t *testing.T) {
	if got := commandForLog("printf command-text-secret; false"); got != "[COMMAND REDACTED]" {
		t.Fatalf("commandForLog() = %q", got)
	}
}

func TestFailedSecretCommandDoesNotLeakLogsOutputOrError(t *testing.T) {
	const commandSecret = "command-text-secret"
	const outputSecret = "output-stream-secret"
	var logs bytes.Buffer
	previousWriter := logx.SetOutput(&logs)
	t.Cleanup(func() { logx.SetOutput(previousWriter) })

	output, err := executeURLCommand("printf 'invalid listen address; password=" + outputSecret + "'; false # " + commandSecret)
	if err == nil {
		t.Fatal("executeURLCommand unexpectedly succeeded")
	}
	if len(output) != 0 {
		t.Fatalf("failed command returned output: %q", output)
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("errors.As(err, *exec.ExitError) = false: %v", err)
	}
	if !errors.Is(err, exitError) {
		t.Fatalf("errors.Is(err, exitError) = false: %v", err)
	}
	for _, secret := range []string{commandSecret, outputSecret} {
		if strings.Contains(logs.String(), secret) || strings.Contains(err.Error(), secret) {
			t.Fatalf("failed command leaked %q: logs=%q err=%q", secret, logs.String(), err.Error())
		}
	}
	if !strings.Contains(err.Error(), "invalid listen address") {
		t.Fatalf("useful failure reason missing: %v", err)
	}
}

func TestRestartHandlerDoesNotReturnFailedCommandOutput(t *testing.T) {
	const commandSecret = "handler-command-secret"
	const outputSecret = "handler-output-secret"
	cfg := &Config{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), RestartCommand: "printf 'failed to bind port; token=" + outputSecret + "'; false # " + commandSecret}
	request := httptest.NewRequest(http.MethodPost, "/restart", nil)
	recorder := httptest.NewRecorder()

	RestartHandler(cfg).ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", recorder.Code, body)
	}
	for _, secret := range []string{commandSecret, outputSecret} {
		if strings.Contains(body, secret) {
			t.Fatalf("restart response leaked %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, "error_details") || !strings.Contains(body, "failed to bind port") {
		t.Fatalf("restart response lost useful diagnostic: %s", body)
	}
}
