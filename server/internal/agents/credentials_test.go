package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

func buildHistoryAgent(t *testing.T, deployments int) *model.Agent {
	t.Helper()
	deps := make(map[string]any, deployments)
	for i := range deployments {
		id := fmt.Sprintf("dep-%04d", i)
		deps[id] = map[string]any{
			"success":       true,
			"deployment_id": id,
			"status":        "succeeded",
			"profile_id":    "default",
			"service_type":  "mihomo",
			"created_at":    "2026-01-01T00:00:00Z",
			"message":       fmt.Sprintf("synthetic deployment payload %04d padding-0123456789abcdef0123456789abcdef", i),
			"events": []map[string]any{
				{"at": "2026-01-01T00:00:00Z", "status": "succeeded", "message": "synthetic event 0123456789abcdef0123456789abcdef"},
			},
		}
	}
	payload := map[string]any{
		"id":            "agent-1",
		"name":          map[string]any{"label": "alice", "token": "synthetic-malformed-name-token"},
		"host":          "192.0.2.10",
		"port":          18080,
		"token":         "synthetic-agent-token",
		"service_type":  "oxidns",
		"profile_id":    "default",
		"enabled":       true,
		"has_password":  true,
		"legacy_secret": "synthetic-legacy-secret",
		"dns_follow_job": map[string]any{
			"binding_id": "bind-1",
			"password":   "synthetic-job-password",
		},
		"rule_sync": map[string]any{
			"settings": map[string]any{"library": true, "rules": false},
			"state":    map[string]any{"status": "failed", "message": "failed for alice"},
		},
		"deployments": deps,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var a model.Agent
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

func TestSystemSecretsMatchesExternalPayloadSecrets(t *testing.T) {
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	const proxy = "http://alice:synthetic-proxy-secret@proxy.example.com:8080"
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.RuleFetchProxy = proxy
		s.Agents = []model.Agent{{ID: "a1", Token: "synthetic-a1-token", ProfileID: "default"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := SystemSecrets(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := r.SystemConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := model.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	want, err := redact.ExternalPayloadSecrets(ctx, r, json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("SystemSecrets mismatch: got=%v want=%v", got, want)
	}
}
