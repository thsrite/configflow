package generate_test

import (
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/server"
)

func c34Owned(route *httpx.Route) bool {
	// A20 shares the custom_config blueprint; its providers have their own auth contract.
	if strings.Contains(route.Endpoint, ".linux_gateway_") {
		return false
	}
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
	s := httpxtest.New(t, httpxtest.Options{Auth: true})
	full, err := server.New(s.Context(), server.Options{Repo: s.Repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	*s.App = *full.HTTP
	original, err := s.Repo.ExportAllJSON(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
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
	current, err := s.Repo.ExportAllJSON(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != string(current) {
		t.Fatal("unauthenticated C34 request wrote storage")
	}
}

func TestC34OwnershipKeepsExternalBlueprintRoutesSeparate(t *testing.T) {
	for _, test := range []struct {
		endpoint string
		want     bool
	}{
		{"custom_config.linux_gateway_settings", false}, {"custom_config.linux_gateway_rule_provider", false}, {"custom_config.linux_gateway_source_provider", false},
		{"custom_config.handle_custom_mihomo_config", true}, {"generate.generate_mihomo", true}, {"profiles.handle_profiles", true},
	} {
		t.Run(test.endpoint, func(t *testing.T) {
			if got := c34Owned(&httpx.Route{Endpoint: test.endpoint}); got != test.want {
				t.Fatalf("owned=%v want=%v", got, test.want)
			}
		})
	}
}
