package agents_test

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/httpx/httpxtest"
)

func TestN810InstallationTypedHooksKeepArgumentOrderAndRunOnce(t *testing.T) {
	s := newServer(t)
	profileCalls, registerCalls := 0, 0
	const id, token, existing = "installation-id-A", "installation-token-B", "existing-token-C"
	agents.RegisterHooks(agents.Hooks{
		InstallationProfileTyped: func(_ context.Context, data json.RawMessage, gotID, gotToken, fallback string) (string, json.RawMessage, error) {
			profileCalls++
			if gotID != id || gotToken != token || fallback == "" || fallback == id || fallback == token {
				t.Fatalf("arguments mixed: %q %q %q", gotID, gotToken, fallback)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if _, ok := fields["installation_token"]; ok {
				t.Fatal("capability leaked into registry fields")
			}
			fields["profile_id"] = json.RawMessage(`"default"`)
			updated, err := json.Marshal(fields)
			return "default", updated, err
		},
		InstallationRegisterTyped: func(_ context.Context, _ *agents.Manager, gotID, gotToken string, data json.RawMessage, gotExisting string) (json.RawMessage, error) {
			registerCalls++
			if gotID != id || gotToken != token || gotExisting != existing {
				t.Fatal("registration arguments mixed")
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["profile_id"]) != `"default"` {
				t.Fatal("profile result not propagated")
			}
			return json.RawMessage(`{"id":"registered-D","is_new":true,"status":"registered"}`), nil
		},
	})
	body := payload("installation_id", id, "installation_token", token)
	s.Post("/api/agents/register", httpxtest.JSON(body), httpxtest.Bearer(existing)).WantStatus(200)
	if profileCalls != 1 || registerCalls != 1 {
		t.Fatalf("hook calls: %d %d", profileCalls, registerCalls)
	}
}
