package redact

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/testjson"
)

// TestReviewCollectCredentialsBeforeProjection checks all raw publication sources.
func TestReviewCollectCredentialsBeforeProjection(t *testing.T) {
	t.Setenv("AGENT_REGISTRATION_KEY", "synthetic-environment")
	raw := map[string]any{
		"deployments":           map[string]any{"old": map[string]any{"api_key": "synthetic-history"}},
		"latest_deployment":     map[string]any{"password": "synthetic-deployment"},
		"oxidns_ha_publication": map[string]any{"members": []any{map[string]any{"auth": "synthetic-ha"}}},
		"latest_upgrade":        map[string]any{"registration_key": "synthetic-upgrade"},
		"authorization":         map[string]any{"token": "synthetic-auth"},
	}
	before, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	seeds := NewSecrets([]string{"synthetic-seed"})
	collected := CollectCredentials(raw, seeds)
	want := NewSecrets(seeds, []string{"synthetic-environment", "synthetic-history", "synthetic-deployment", "synthetic-ha", "synthetic-upgrade", "synthetic-auth"})
	if !collected.Equal(want) {
		t.Fatal("raw publication credential snapshot incomplete")
	}
	if !testjson.Equal(raw, json.RawMessage(before)) || len(seeds) != 1 {
		t.Fatal("collection mutated input or caller seeds")
	}
	projected := map[string]any{"journal": strings.Join([]string(collected), " "), "future": true}
	public, err := json.Marshal(SanitizeCredentials(projected, collected))
	if err != nil || strings.Contains(string(public), "synthetic-") || !strings.Contains(string(public), `"future":true`) {
		t.Fatal("collected secrets escaped a projection or ordinary fields were lost")
	}
	// Cycles and deep input use the same bounded walk as public redaction.
	raw["self"] = raw
	if !CollectCredentials(raw, seeds).Equal(want) {
		t.Fatal("cyclic raw metadata changed the collected snapshot")
	}
	deep := raw
	for i := 0; i < maxDepth+1; i++ {
		deep = map[string]any{"child": deep}
	}
	if !CollectCredentials(deep, nil).Equal(NewSecrets([]string{"synthetic-environment"})) {
		t.Fatal("credential collector bypassed the shared depth bound")
	}
}
