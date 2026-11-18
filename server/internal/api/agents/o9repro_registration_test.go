//go:build o9repro

package agents

// O9 round 1 reproduction (fails until fixed; excluded from the gate by the
// o9repro build tag): run with `go test -tags o9repro -run TestO9Repro ./internal/api/agents`.

import (
	"context"
	"os"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

// With panel login enabled and AGENT_REGISTRATION_KEY unset (the default),
// a client without any credential registers an Agent and then pulls the
// generated configuration of the default profile, including its secrets
// (here the Mihomo external-controller secret of the custom config).
func TestO9ReproAnonymousAgentRegistrationReadsProfileSecrets(t *testing.T) {
	t.Setenv("AGENT_REGISTRATION_KEY", "")
	os.Unsetenv("AGENT_REGISTRATION_KEY")
	s, _, _ := newDeployHTTP(t)
	node := jsonx.Obj("id", "o9-node", "name", "O9", "proxy_string", "ss://YWVzLTEyOC1nY206bzktc3ludGhldGljLW5vZGUtc2VjcmV0@192.0.2.10:8388#O9", "enabled", true)
	if _, err := s.Repo.UpdateSharedTransaction(s.Context(), func(_ context.Context, catalog *jsonx.Object) (*jsonx.Object, error) {
		catalog.Set("nodes", []any{node})
		return catalog, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Repo.UpdateProfileFields(s.Context(), "default", jsonx.Obj("mihomo", jsonx.Obj("custom_config", "mode: rule\nsecret: o9-synthetic-controller-secret\n"), "proxy_groups", []any{jsonx.Obj("id", "o9-group", "name", "Proxy", "type", "select", "subscriptions", []any{}, "manual_nodes", []any{"o9-node"})}), nil); err != nil {
		t.Fatal(err)
	}
	registered := s.Post("/api/agents/register", httpxtest.JSON(jsonx.Obj("name", "o9-rogue", "host", "192.0.2.66", "port", 8080,
		"service_type", "mihomo", "version", "1.8.60-go")))
	t.Logf("register %d %s", registered.Status, registered.Body)
	if registered.Status != 200 {
		return // registration refused without credentials: fixed
	}
	body := registered.Object()
	config := s.Get("/api/agents/" + body.GetString("id") + "/config?token=" + body.GetString("token"))
	t.Logf("config %d %.600s", config.Status, config.Body)
	if config.Status == 200 && (strings.Contains(string(config.Body), "o9-synthetic-node-secret") || strings.Contains(string(config.Body), "o9-synthetic-controller-secret")) {
		t.Fatalf("anonymous Agent registration returned the profile's secrets (register %d, config %d)", registered.Status, config.Status)
	}
}
