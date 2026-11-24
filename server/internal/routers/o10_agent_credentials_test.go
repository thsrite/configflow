package routers

import (
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// TestO10RouterRedactionKeepsAgentRecordCredentials checks that router
// responses still hide credential values stored anywhere in an Agent record,
// including deployment history, although resolved views no longer embed the
// Agent registry.
func TestO10RouterRedactionKeepsAgentRecordCredentials(t *testing.T) {
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	agents := `[{"id":"a-oxi","name":"Oxi","host":"192.0.2.10","service_type":"oxidns","profile_id":"default","token":"synthetic-agent-token",` +
		`"deployments":{"d1":{"id":"d1","status":"success","password":"synthetic-history-pass"}},` +
		`"future":{"secret":"synthetic-unknown-secret"}}]`
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		return s.SetJSON("agents", json.RawMessage(agents))
	}); err != nil {
		t.Fatal(err)
	}
	// The history's undeclared "password" member is dropped on load (O18 T1b)
	// and is no credential any more.
	secrets := []string{"synthetic-agent-token"}

	t.Run("helper", func(t *testing.T) {
		seeds, err := redact.AgentRecordCredentials(ctx, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range secrets {
			got := redact.SanitizeWithSecrets("value "+secret, seeds, false)
			if data, err := json.Marshal(got); err != nil || strings.Contains(string(data), secret) {
				t.Fatalf("%q not redacted: %v", secret, got)
			}
		}
	})

	t.Run("management safeText", func(t *testing.T) {
		s := &ManagementService{Repo: r}
		for _, secret := range secrets {
			got := s.safeText(ctx, "default", &spec.Router{}, "diagnostic "+secret)
			if strings.Contains(got, secret) {
				t.Fatalf("%q leaked: %q", secret, got)
			}
		}
		// A later Agent write (new head) is seen despite the per-generation cache.
		if err := r.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
			sys.Agents[0].Token = "synthetic-later-pass"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if got := s.safeText(ctx, "default", &spec.Router{}, "diagnostic synthetic-later-pass"); strings.Contains(got, "synthetic-later-pass") {
			t.Fatalf("credential written after the first read leaked: %q", got)
		}
		// Control: an ordinary value passes through unchanged.
		if got := s.safeText(ctx, "default", &spec.Router{}, "diagnostic ok"); got != "diagnostic ok" {
			t.Fatalf("plain text changed: %q", got)
		}
	})
}
