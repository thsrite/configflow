package routers_test

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	api "configflow/internal/api/routers"
	"configflow/internal/httpx/httpxtest"
	core "configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

// Review R15 F1: Python stores str(RouterError) in last_probe. The iKuai
// redirect message names the management origin the user must enter; the
// diagnostic "download address" filter must not replace it.
func TestReviewR15ProbeRedirectKeepsManagementOrigin(t *testing.T) {
	seed := routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{{Func: "login", Status: 302, Location: "https://198.51.100.7:8443/Action/login?token=synthetic-token"}}}
	fake := routerfake.NewIkuai(seed)
	server := httptest.NewServer(fake)
	defer server.Close()
	cfg := inputConfig(server.URL)
	r := cfg["routers"].(map[string]any)["items"].([]any)[0].(map[string]any)
	r["type"] = "ikuai"
	r["username"] = routerfake.IkuaiUser
	r["password"] = routerfake.IkuaiPassword
	dir := t.TempDir()
	oracle := r15Python(t, dir, map[string]any{"config": cfg, "system": map[string]any{}, "requests": []boundaryRequest{{Method: "POST", Path: "/api/routers/router-one/probe"}}})
	var want struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(oracle.Responses[0].Payload, &want); err != nil || oracle.Responses[0].Status != 502 {
		t.Fatalf("reference: %d %s %v", oracle.Responses[0].Status, oracle.Responses[0].Payload, err)
	}
	const origin = "跳转到 https://198.51.100.7:8443（HTTP 302）"
	if !strings.Contains(want.Message, origin) {
		t.Fatalf("reference message changed: %s", want.Message)
	}
	fake.Reset(seed)
	s := httpxtest.New(t, httpxtest.Options{DataDir: filepath.Join(dir, "go")})
	api.New(api.Deps{Repo: s.Repo, Routers: core.New(s.Repo, core.Options{})}).Register(s.App.Router)
	result := s.Post("/api/routers/router-one/probe")
	result.WantStatus(502)
	got, _ := decodeJSON(t, result)["message"].(string)
	if got != want.Message || strings.Contains(got, "synthetic-token") {
		t.Fatalf("message=%q want %q", got, want.Message)
	}
	profile, err := s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	stored := profile.Routers.Items[0].LastProbe
	if stored == nil || stored.Message == nil || *stored.Message != want.Message {
		t.Fatalf("stored message=%+v", stored)
	}
}
