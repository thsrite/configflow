package profiles

import (
	"context"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

func TestReviewC34BindingCredentials(t *testing.T) {
	s := profileApp(t)
	if _, err := s.Repo.CreateProfile(s.Context(), jsonx.Obj("id", "target", "name", "Target"), ""); err != nil {
		t.Fatal(err)
	}
	_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{jsonx.Obj("id", "legacy", "name", "Legacy", "host", "operator:synthetic-binding-password@192.0.2.9", "profile_id", "default", "token", "synthetic-binding-token")})
		return nil, nil
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
	response := s.Post("/api/profiles/default/agents/rebind", httpxtest.JSON(jsonx.Obj("target_profile_id", "target", "agent_ids", []any{"legacy"})))
	response.WantStatus(200)
	if strings.Contains(string(response.Body), "synthetic-binding-password") {
		t.Errorf("rebind leaks host credentials: %s", response.Body)
	}
	system, err := s.Repo.GetSystem(s.Context())
	if err != nil || system.GetList("agents")[0].(*jsonx.Object).GetString("host") != "operator:synthetic-binding-password@192.0.2.9" {
		t.Fatal("redaction changed restorable host credentials", err)
	}
	_, err = s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		agent := system.GetList("agents")[0].(*jsonx.Object)
		agent.Set("name", "password=synthetic-binding-error")
		agent.Set("latest_upgrade", jsonx.Obj("status", "installing"))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before = state(t, s)
	response = s.Post("/api/profiles/target/agents/rebind", httpxtest.JSON(jsonx.Obj("target_profile_id", "default", "agent_ids", []any{"legacy"})))
	response.WantStatus(409)
	if strings.Contains(string(response.Body), "synthetic-binding-error") {
		t.Errorf("conflict diagnostic leaks a credential: %s", response.Body)
	}
	if before != state(t, s) {
		t.Fatal("rejected rebind changed storage")
	}
}
