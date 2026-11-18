package generate_test

import (
	"strings"
	"testing"

	api "configflow/internal/api/generate"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/server"
)

func TestArtifactRegistrationWaitsForReleaseOwner(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(api.Register)})
	for _, prefix := range []string{"/api/generate", "/api/profiles/default/generate"} {
		for _, engine := range []string{"mihomo", "surge", "loon", "mosdns", "oxidns"} {
			result := s.Post(prefix + "/" + engine + "/artifact")
			result.WantStatus(503)
			if !strings.Contains(string(result.Body), "等待 Agent 发布模块") || result.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("missing release-owner explanation")
			}
		}
		s.Post(prefix + "/unknown/artifact").WantStatus(400)
	}
	for _, action := range []string{"download", "file"} {
		s.Get("/api/generate/artifacts/synthetic/" + action).WantStatus(503)
	}
	s.Get("/api/generate/artifacts/synthetic/unknown").WantStatus(404)
}
func TestCompleteC34RegistrationHasOneOwner(t *testing.T) {
	app := server.NewApp()
	seen := map[string]string{}
	count := 0
	for _, r := range app.Router.Routes() {
		for _, method := range r.Methods {
			key := method + " " + r.Path
			if old, ok := seen[key]; ok {
				t.Fatalf("route conflict %s: %s %s", key, old, r.Endpoint)
			}
			seen[key] = r.Endpoint
			if method == "HEAD" || method == "OPTIONS" {
				continue
			}
			if strings.HasPrefix(r.Endpoint, "config.") || strings.HasPrefix(r.Endpoint, "custom_config.") || strings.HasPrefix(r.Endpoint, "generate.") {
				count++
				continue
			}
			switch r.Endpoint {
			case "profiles.handle_profiles", "profiles.handle_profile", "profiles.set_default_profile", "profiles.clone_profile", "profiles.profile_agent_bindings", "profiles.rebind_profile_agents", "profiles.export_profile", "profiles.import_profile", "profiles.import_client_profile":
				count++
				continue
			}
			if strings.Contains(r.Path, "/generate/") && (strings.HasPrefix(r.Endpoint, "profiles.profile_generate_") || strings.HasPrefix(r.Endpoint, "profiles.profile_preview_")) {
				count++
			}
		}
	}
	if count != 59 {
		t.Fatalf("C34 owns %d methods, want 59", count)
	}
}
