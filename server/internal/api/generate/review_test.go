package generate_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/server"
)

func TestReviewC34AuthenticationMatrix(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{Auth: true})
	full, err := server.New(s.Context(), server.Options{Repo: s.Repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	*s.App = *full.HTTP
	err = s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		system.SystemConfig.ConfigToken = "synthetic-config"
		system.SystemConfig.RuleProxyToken = "synthetic-rule"
		system.SystemConfig.RetiredRuleProxyTokens = []string{"synthetic-retired"}
		var agent model.Agent
		if err := json.Unmarshal([]byte(`{"id":"agent","profile_id":"default","token":"synthetic-agent"}`), &agent); err != nil {
			return err
		}
		system.Agents = []model.Agent{agent}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Repo.ExportAllJSON(s.Context(), false)
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
			for _, token := range []string{"synthetic-config", "synthetic-rule", "synthetic-retired", "synthetic-agent", "wrong"} {
				s.Do(method, path, httpxtest.Bearer(token)).WantStatus(401)
				// Only the eleven live subscription paths accept a config token.
				if token != "synthetic-config" || !strings.HasPrefix(route.Endpoint, "config.get_") {
					s.Do(method, path, httpxtest.Query("token", token)).WantStatus(401)
				}
			}
			count++
		}
	}
	if count != 59 {
		t.Fatalf("matrix covered %d methods", count)
	}
	after, err := s.Repo.ExportAllJSON(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected authentication changed storage")
	}
	admin := s.Login()
	for _, route := range s.App.Router.Routes() {
		if !c34Owned(route) {
			continue
		}
		for _, method := range route.Methods {
			if method == "HEAD" || method == "OPTIONS" {
				continue
			}
			path := strings.NewReplacer("<profile_id>", "default", "<service>", "mihomo", "<preview_id>", "synthetic", "<action>", "download", "<filename>", "name.lcf").Replace(route.Path)
			response := s.Do(method, path, admin, httpxtest.JSON(map[string]any{}))
			if response.Status == 401 || response.Status == 403 {
				t.Fatalf("valid JWT rejected: %s %s: %s", method, path, response.Body)
			}
			if route.Endpoint == "generate.generate_artifact" || route.Endpoint == "profiles.profile_generate_artifact" {
				response.WantStatus(200)
			}
			if route.Endpoint == "generate.read_artifact" {
				response.WantStatus(409)
			}
		}
	}
}

func TestReviewC34ForeignAliasDispatch(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{})
	full, err := server.New(s.Context(), server.Options{Repo: s.Repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	full.HTTP.BeforeRequest(func(c *httpx.Ctx) (*httpx.Response, error) {
		return httpx.JSON(map[string]string{"endpoint": c.Endpoint, "profile": c.ProfileID}), nil
	})
	*s.App = *full.HTTP
	aliases := []struct{ methods, path, endpoint string }{
		{"GET", "subscriptions/item/proxies", "profile_subscription_proxies"},
		{"GET POST", "rules", "profile_rules"},
		{"GET", "rules/local/name", "profile_local_rule"},
		{"GET", "rule-library/content/item", "profile_rule_library_content"},
		{"GET POST", "proxy-groups", "profile_proxy_groups"},
		{"GET", "aggregations/item/provider", "profile_aggregation_provider"},
		{"GET", "mosdns/rule-proxy", "profile_mosdns_rule_proxy"},
		{"GET POST", "oxidns/settings", "profile_oxidns_settings"},
		{"POST", "oxidns/render", "profile_render_oxidns_draft"},
		{"POST", "oxidns/validate", "profile_validate_oxidns_draft"},
		{"POST", "oxidns/editor", "profile_prepare_oxidns_editor"},
		{"POST", "oxidns/template-preview", "profile_preview_oxidns_template"},
		{"POST", "oxidns/common-settings", "profile_read_oxidns_common_settings"},
		{"POST", "oxidns/common-settings/apply", "profile_apply_oxidns_common_settings"},
		{"POST", "oxidns/native-settings/apply", "profile_apply_oxidns_native_settings"},
	}
	count := 0
	for _, route := range aliases {
		for _, method := range strings.Fields(route.methods) {
			response := s.Do(method, "/api/profiles/default/"+route.path)
			response.WantStatus(200)
			obj := response.Object()
			if obj.GetString("endpoint") != "profiles."+route.endpoint || obj.GetString("profile") != "default" {
				t.Fatalf("%s %s dispatched to %s", method, route.path, response.Body)
			}
			count++
		}
	}
	if count != 18 {
		t.Fatalf("foreign alias methods=%d", count)
	}
}

func TestPreviewOxiDNSPreservesAuthObjectAndRedactsSystemSecrets(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{})
	full, err := server.New(s.Context(), server.Options{Repo: s.Repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	*s.App = *full.HTTP
	err = s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
		system.SystemConfig.RuleProxyToken = "synthetic-preview-secret"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, profile *model.Profile) error {
		return profile.SetJSON("oxidns", json.RawMessage(`{"config_mode":"native","native_config":"api:\n  http:\n    listen: 127.0.0.1:9199\n    auth:\n      type: basic\n      username: admin\n      password: synthetic-preview-secret\nplugins: []\n"}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	res := s.Do("POST", "/api/generate/oxidns/preview")
	res.WantStatus(200)
	var body struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(res.Body), &body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body.Content, "auth: \"[REDACTED]\"") || strings.Contains(body.Content, "auth: '[REDACTED]'") {
		t.Fatalf("preview collapsed api.http.auth mapping into scalar redaction:\n%s", body.Content)
	}
	if !strings.Contains(body.Content, "type: basic") || !strings.Contains(body.Content, "username: admin") {
		t.Fatalf("preview dropped api.http.auth mapping fields:\n%s", body.Content)
	}
	if strings.Contains(body.Content, "synthetic-preview-secret") || !strings.Contains(body.Content, "[REDACTED]") {
		t.Fatalf("preview did not redact system secret:\n%s", body.Content)
	}
}
