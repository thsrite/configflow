package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

// Every A20 rule is registered in the fully merged application. Static routes
// must keep Flask precedence even when their spelling can be an Agent ID.
func TestGatewayRegistrationAndPrecedence(t *testing.T) {
	s := httpxtest.New(t, httpxtest.Options{})
	srv, err := New(s.Context(), Options{Repo: s.Repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Agents.Close)
	app := srv.HTTP
	previous := auth.Configure(auth.Config{})
	t.Cleanup(func() { auth.Configure(previous) })
	for _, endpoint := range []string{"agents.agent_gateway_forwarding", "agents.linux_gateway_operation", "custom_config.linux_gateway_settings", "profiles.linux_gateway_settings", "custom_config.linux_gateway_rule_provider", "profiles.linux_gateway_rule_provider", "custom_config.linux_gateway_source_provider", "profiles.linux_gateway_source_provider"} {
		if !app.Router.HasEndpoint(endpoint) {
			t.Fatal("not registered", endpoint)
		}
	}
	// Run just routing in the complete registration, avoiding any live networking.
	app.BeforeRequest(func(c *httpx.Ctx) (*httpx.Response, error) { return httpx.JSON(c.Params()), nil })
	for _, tc := range []struct{ method, path, param, want string }{{"GET", "/api/agents/gateway-forwarding", "agent_id", ""}, {"POST", "/api/agents/gateway-forwarding", "agent_id", ""}, {"GET", "/api/agents/gateway-forwarding/linux-gateway/status", "agent_id", "gateway-forwarding"}, {"GET", "/api/agents/i/status", "token", "status"}, {"POST", "/api/agents/gateway/mihomo/rule-targets", "resource", "rule-targets"}, {"GET", "/api/profiles/default/linux-gateway/rules/a/b", "rule_id", "a/b"}} {
		request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		request = request.WithContext(s.Context())
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, request)
		if rec.Code != 200 {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		if tc.want != "" && !strings.Contains(rec.Body.String(), `"`+tc.param+`":"`+tc.want+`"`) {
			t.Fatalf("wrong precedence %s: %s", tc.path, rec.Body.String())
		}
	}
}
