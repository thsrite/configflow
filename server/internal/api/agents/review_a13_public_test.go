package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/logx"
	"configflow/internal/model"
)

// TestReviewA13PublicReadsUseSharedCredentialSanitizer checks all list/detail variants.
func TestReviewA13PublicReadsUseSharedCredentialSanitizer(t *testing.T) {
	t.Setenv("AGENT_REGISTRATION_KEY", "synthetic-env-registration")
	f := newContentHTTP(t, false, true)
	if err := f.s.Repo.UpdateSystem(f.s.Context(), func(_ context.Context, system *model.System) error {
		setAgentJSONFields(t, &system.Agents[0], map[string]any{
			"host":             "operator:synthetic-password@192.0.2.10",
			"password":         "synthetic-password",
			"registration_key": "synthetic-legacy-registration",
			"description":      "old synthetic-env-registration and synthetic-legacy-registration",
			"journal":          "old synthetic-api-key",
			"latest_upgrade":   map[string]any{"status": "failed", "message": "old stored-agent-token"},
			"deployments":      map[string]any{"old": map[string]any{"status": "failed", "api_key": "synthetic-api-key", "message": "password=synthetic-diagnostic"}},
			"future":           map[string]any{"keep": true},
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/agents?summary=1", "/api/agents?history=0", "/api/agents", "/api/agents/content-agent"} {
		reply := f.get(path).WantStatus(200)
		for _, secret := range []string{"synthetic-password", "synthetic-legacy-registration", "synthetic-env-registration", "stored-agent-token", "synthetic-api-key", "synthetic-diagnostic"} {
			if reply.Contains(secret) {
				t.Errorf("%s leaked %q", path, secret)
			}
		}
		if !reply.Contains(`"keep":true`) {
			t.Error("ordinary future field lost")
		}
	}
	record, err := f.manager.ReadAgent(f.s.Context(), "content-agent", true)
	if err != nil {
		t.Fatal(err)
	}
	stored := parseJSONMap(t, mustRawJSON(t, record))
	if mapStr(stored, "password") != "synthetic-password" || mapStr(stored, "registration_key") != "synthetic-legacy-registration" {
		t.Fatal("public reads mutated stored credentials")
	}
}

// TestReviewA13CollectedSecretsStayOutOfErrorsAndLogs exercises failure and panic paths.
func TestReviewA13CollectedSecretsStayOutOfErrorsAndLogs(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "app.log")
	root, err := logx.Setup(logx.Config{Level: logx.Debug, File: logFile})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		root.Close()
		if _, err := logx.Setup(logx.Config{Level: logx.LevelFromEnv(), Console: os.Stderr}); err != nil {
			t.Error(err)
		}
	})
	const secret = "synthetic-collected-secret"
	for _, panicWorker := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panicWorker], func(t *testing.T) {
			f := newContentHTTP(t, true, false)
			if err := f.s.Repo.UpdateSystem(f.s.Context(), func(_ context.Context, system *model.System) error {
				setAgentJSONFields(t, &system.Agents[0], map[string]any{
					"deployments": map[string]any{"old": map[string]any{"status": "failed", "api_key": secret}},
					"journal":     "old " + secret,
				})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			restore := SetContentPrepare(func(context.Context, deploy.Target, *deploy.Source, string, string) (*deploy.Bundle, error) {
				if panicWorker {
					panic(secret)
				}
				return nil, errors.New(secret)
			})
			defer restore()
			if f.get("/api/agents/content-agent").WantStatus(200).Contains(secret) {
				t.Error("collected credential escaped the public record")
			}
			failure := f.settle(t).WantStatus(200)
			if mapStr(parseBodyMap(t, failure), "reason") != "check_failed" || failure.Contains(secret) {
				t.Error("collected credential escaped a preparation failure")
			}
		})
	}
	data, err := os.ReadFile(logFile)
	if err != nil || strings.Contains(string(data), secret) {
		t.Error("collected credential escaped into application logs")
	}
	if !strings.Contains(string(data), "Content comparison worker panicked") {
		t.Error("panic path did not exercise the real logger")
	}
	_ = json.RawMessage(nil)
}
