package profiles

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestReviewC34BindingCredentials(t *testing.T) {
	s := profileApp(t)
	if _, err := s.Repo.CreateProfileRecord(s.Context(), repo.ProfileCreation{ID: "target", Name: "Target"}, ""); err != nil {
		t.Fatal(err)
	}
	err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		return system.SetJSON("agents", json.RawMessage(`[{"id":"legacy","name":"Legacy","host":"operator:synthetic-binding-password@192.0.2.9","profile_id":"default","token":"synthetic-binding-token"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	before := state(t, s)
	for _, path := range []string{"/api/profiles", "/api/profiles/default/agents"} {
		response := s.Get(path)
		response.WantStatus(200)
		if strings.Contains(string(response.Body), "synthetic-binding-password") {
			t.Errorf("%s leaks host credentials: %s", path, response.Body)
		}
	}
	if before != state(t, s) {
		t.Fatal("metadata sanitization changed storage")
	}
	response := s.Post("/api/profiles/default/agents/rebind", httpxtest.JSON(map[string]any{"target_profile_id": "target", "agent_ids": []string{"legacy"}}))
	response.WantStatus(200)
	if strings.Contains(string(response.Body), "synthetic-binding-password") {
		t.Errorf("rebind leaks host credentials: %s", response.Body)
	}
	system, err := s.Repo.System(s.Context())
	if err != nil || system.Agents[0].Host != "operator:synthetic-binding-password@192.0.2.9" {
		t.Fatal("redaction changed restorable host credentials", err)
	}
	err = s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		system.Agents[0].Name = "password=synthetic-binding-error"
		return model.SetStoredMember(&system.Agents[0], "latest_upgrade", json.RawMessage(`{"status":"installing"}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	before = state(t, s)
	response = s.Post("/api/profiles/target/agents/rebind", httpxtest.JSON(map[string]any{"target_profile_id": "default", "agent_ids": []string{"legacy"}}))
	response.WantStatus(409)
	if strings.Contains(string(response.Body), "synthetic-binding-error") {
		t.Errorf("conflict diagnostic leaks a credential: %s", response.Body)
	}
	if before != state(t, s) {
		t.Fatal("rejected rebind changed storage")
	}
}
