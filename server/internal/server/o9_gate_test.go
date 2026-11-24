package server

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/repo"
)

// O9: with login enabled, every route of the fully wired application that
// is not on the public allowlist (auth.IsPublicPath or a self-authenticating
// endpoint) answers an anonymous request with 401 before its handler runs.
// New routes that forget the gate, or a gate that drifts from the
// allowlist, fail here.
func TestO9EveryNonPublicRouteRequiresLogin(t *testing.T) {
	previous := auth.Configure(auth.Config{AdminUsername: "o9-admin", AdminPassword: "o9-password"})
	t.Cleanup(func() { auth.Configure(previous) })
	repository, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repository.Close)
	srv, err := New(t.Context(), Options{Repo: repository})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Agents.Close)
	app := srv.HTTP
	param := regexp.MustCompile(`<(?:[a-z]+:)?[A-Za-z_]+>`)
	selfAuthenticating := map[string]bool{
		"agents.download_agent_binary": true, "agents.download_install_command": true,
		"agents.gateway_installation_script": true, "agents.gateway_installation_enroll": true,
		"agents.docker_oxidns_ha_enroll": true, "agents.agent_heartbeat": true, "agents.agent_traffic_report": true,
		"agents.agent_traffic_history_report": true, "agents.get_agent_config": true, "agents.get_agent_upgrade": true,
		"agents.report_agent_upgrade": true, "custom_config.linux_gateway_rule_provider": true,
		"profiles.linux_gateway_rule_provider": true, "custom_config.linux_gateway_source_provider": true,
		"profiles.linux_gateway_source_provider": true,
	}
	checked := 0
	for _, route := range app.Router.Routes() {
		path := param.ReplaceAllString(strings.ReplaceAll(route.Path, "<profile_id>", "default"), "o9x")
		if selfAuthenticating[route.Endpoint] || auth.IsPublicPath(path) {
			continue
		}
		for _, method := range route.Methods {
			if method == "OPTIONS" || method == "HEAD" {
				continue
			}
			req := httptest.NewRequest(method, "http://config.example"+path, nil)
			req = req.WithContext(req.Context())
			w := httptest.NewRecorder()
			app.ServeHTTP(w, req)
			if w.Code != 401 {
				t.Errorf("%s %s (%s): anonymous request answered %d", method, route.Path, route.Endpoint, w.Code)
			}
			checked++
		}
	}
	if checked < 300 {
		t.Fatalf("only %d protected method/route pairs checked", checked)
	}
	t.Logf("%d protected method/route pairs answer 401 without login", checked)
}
