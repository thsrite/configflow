package generate_test

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/server"
)

func c34Owned(route *httpx.Route) bool {
	if strings.HasPrefix(route.Endpoint, "config.") || strings.HasPrefix(route.Endpoint, "custom_config.") || strings.HasPrefix(route.Endpoint, "generate.") {
		return true
	}
	switch route.Endpoint {
	case "profiles.handle_profiles", "profiles.handle_profile", "profiles.set_default_profile", "profiles.clone_profile", "profiles.profile_agent_bindings", "profiles.rebind_profile_agents", "profiles.export_profile", "profiles.import_profile", "profiles.import_client_profile":
		return true
	}
	return strings.Contains(route.Path, "/generate/") && (strings.HasPrefix(route.Endpoint, "profiles.profile_generate_") || strings.HasPrefix(route.Endpoint, "profiles.profile_preview_"))
}
func TestEveryC34RegisteredMethodRequiresTheRightAuthentication(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Setup: func(app *httpx.App) { *app = *server.NewApp() }})
	before, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(before)
	count := 0
	for _, route := range s.App.Router.Routes() {
		if !c34Owned(route) {
			continue
		}
		for _, method := range route.Methods {
			if method == "HEAD" || method == "OPTIONS" {
				continue
			}
			path := strings.NewReplacer("<profile_id>", "default", "<service>", "mihomo", "<preview_id>", "synthetic", "<action>", "download", "<filename>", "name.lcf").Replace(route.Path)
			response := s.Do(method, path)
			response.WantStatus(401)
			if response.Header.Get("Content-Type") != "application/json" {
				t.Fatal("non-JSON auth failure")
			}
			count++
		}
	}
	if count != 59 {
		t.Fatalf("authentication matrix covers %d methods", count)
	}
	after, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := json.Marshal(after)
	if string(original) != string(current) {
		t.Fatal("unauthenticated C34 request wrote storage")
	}
}
