package redact

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// TestReviewCollectCredentialsBeforeProjection checks all raw publication sources.
func TestReviewCollectCredentialsBeforeProjection(t *testing.T) {
	t.Setenv("AGENT_REGISTRATION_KEY", "synthetic-environment")
	raw := jsonx.Obj(
		"deployments", jsonx.Obj("old", jsonx.Obj("api_key", "synthetic-history")),
		"latest_deployment", jsonx.Obj("password", "synthetic-deployment"),
		"oxidns_ha_publication", jsonx.Obj("members", []any{jsonx.Obj("auth", "synthetic-ha")}),
		"latest_upgrade", jsonx.Obj("registration_key", "synthetic-upgrade"),
		"authorization", jsonx.Obj("token", "synthetic-auth"),
	)
	before := raw.DeepCopy()
	seeds := NewSecrets([]string{"synthetic-seed"})
	collected := CollectCredentials(raw, seeds)
	want := NewSecrets(seeds, []string{"synthetic-environment", "synthetic-history", "synthetic-deployment", "synthetic-ha", "synthetic-upgrade", "synthetic-auth"})
	if !collected.Equal(want) {
		t.Fatal("raw publication credential snapshot incomplete")
	}
	if !jsonx.Equal(raw, before) || len(seeds) != 1 {
		t.Fatal("collection mutated input or caller seeds")
	}
	projected := jsonx.Obj("journal", strings.Join([]string(collected), " "), "future", true)
	public, err := json.Marshal(SanitizeCredentials(projected, collected))
	if err != nil || strings.Contains(string(public), "synthetic-") || !strings.Contains(string(public), `"future":true`) {
		t.Fatal("collected secrets escaped a projection or ordinary fields were lost")
	}
	// Cycles and deep input use the same bounded walk as public redaction.
	raw.Set("self", raw)
	if !CollectCredentials(raw, seeds).Equal(want) {
		t.Fatal("cyclic raw metadata changed the collected snapshot")
	}
	deep := raw
	for i := 0; i < maxDepth+1; i++ {
		deep = jsonx.Obj("child", deep)
	}
	if !CollectCredentials(deep, nil).Equal(NewSecrets([]string{"synthetic-environment"})) {
		t.Fatal("credential collector bypassed the shared depth bound")
	}
}
