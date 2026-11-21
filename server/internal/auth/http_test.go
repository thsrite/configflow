package auth_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/testjson"
)

func wantJSON(t *testing.T, res *httpxtest.Result, want any) {
	t.Helper()
	if diff := testjson.Diff(json.RawMessage(res.Body), want); diff != "" {
		t.Fatalf("body %s\n%s", res.Body, diff)
	}
}

// protectedRoutes adds a require_auth route on a public path, so that
// require_auth itself (not the global gate) answers, as in the Python tests
// that build a bare Flask app.
func protectedRoutes(r *httpx.Router) {
	r.GET("/static/protected", "test.protected", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		return httpx.JSON(map[string]any{"success": true}), nil
	}))
}

func protectedServer(t *testing.T) *httpxtest.Server {
	return httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(protectedRoutes)})
}

// test_bearer_auth.py::test_require_auth_accepts_valid_jwt
func TestRequireAuthAcceptsValidJWT(t *testing.T) {
	srv := protectedServer(t)
	token, _ := auth.GenerateToken("admin")
	srv.Get("/static/protected", httpxtest.Bearer(token)).WantStatus(200)
}

// test_bearer_auth.py::test_require_auth_rejects_malformed_bearer
func TestRequireAuthRejectsMalformedBearer(t *testing.T) {
	srv := protectedServer(t)
	token, _ := auth.GenerateToken("admin")
	for name, header := range map[string]string{
		"trailing-field": "Bearer " + token + " extra", "double-space": "Bearer  " + token,
		"tab": "Bearer\t" + token, "unicode": "Bearer café",
		"oversized": "Bearer " + strings.Repeat("x", auth.MaxAuthTokenLength+1),
	} {
		if res := srv.Get("/static/protected", httpxtest.Header("Authorization", header)); res.Status != 401 {
			t.Errorf("%s: status %d", name, res.Status)
		}
	}
}

// test_login_messages.py::test_wrong_credentials_return_chinese_and_still_reject_login
func TestWrongCredentialsReturnChineseAndStillRejectLogin(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true})
	res := srv.Post("/api/auth/login", httpxtest.JSON(map[string]any{"username": "upgrade-admin", "password": "wrong"}))
	wantJSON(t, res.WantStatus(401), map[string]any{"success": false, "message": "用户名或密码错误，请重新输入。"})
}

// test_login_messages.py::test_login_validation_and_missing_session_messages_are_chinese
func TestLoginValidationAndMissingSessionMessagesAreChinese(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true})
	admin := srv.Login()
	verified := srv.Get("/api/auth/verify", admin).WantStatus(200)
	if verified.Object().GetString("message") != "登录状态有效。" {
		t.Fatal(verified.Text())
	}
	missing := srv.Get("/api/agents").WantStatus(401)
	if missing.Object().GetString("message") != "请先登录后再操作。" {
		t.Fatal(missing.Text())
	}
}

// test_login_messages.py::test_jwt_failure_codes_and_original_diagnostics_are_preserved
func TestJWTFailureCodesAndOriginalDiagnosticsArePreserved(t *testing.T) {
	srv := protectedServer(t)
	clock := time.Now()
	previous := auth.Current()
	auth.Configure(auth.Config{AdminUsername: previous.AdminUsername, AdminPassword: previous.AdminPassword,
		SecretKey: previous.SecretKey, Now: func() time.Time { return clock }})
	expired, _ := auth.GenerateToken("admin")
	clock = clock.Add(25 * time.Hour)
	res := srv.Get("/static/protected", httpxtest.Bearer(expired)).WantStatus(401)
	if res.Object().GetString("error") != "token_expired" || !strings.Contains(res.Object().GetString("message"), "登录已过期") {
		t.Fatal(res.Text())
	}
	forged, _ := auth.EncodeHS256(map[string]any{"username": "admin"}, "other-secret")
	res = srv.Get("/static/protected", httpxtest.Bearer(forged)).WantStatus(401)
	message := res.Object().GetString("message")
	if res.Object().GetString("error") != "token_invalid" || !strings.Contains(message, "登录凭据无效") ||
		!strings.HasSuffix(message, "Signature verification failed") {
		t.Fatal(res.Text())
	}
}

// test_security_regressions.py::test_auth_public_paths_are_exact_and_stats_is_protected
func TestAuthPublicPathsAreExactAndStatsIsProtected(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true})
	srv.Get("/").WantStatus(404) // public, no route
	srv.Get("/anything").WantStatus(401)
	srv.Get("/api/auth/status-extra").WantStatus(401)
	srv.Get("/api/stats/overview").WantStatus(401)
}

func TestPublicPathRules(t *testing.T) {
	public := []string{"/api/auth/status", "/api/config/mihomo", "/api/version", "/", "/mcp", "/mcp/x",
		"/api/rule-library/content/abc", "/api/config/alpha/surge", "/api/config/alpha/loon/x",
		"/api/profiles/p1/mosdns/rule-proxy", "/api/profiles/p1/subscriptions/s/proxies",
		"/api/profiles/p1/rule-library/content/r"}
	private := []string{"/api/config/-bad/mihomo", "/api/config/alpha/mihomo/x", "/api/configx", "/api/auth/statusx",
		"/api/profiles/p1/subscriptions/s/proxies/x", "/mcpx", "/api/config/alpha/mihomo\n", "/api/config/alpha/mihomo\n\n"}
	for _, path := range public {
		if !auth.IsPublicPath(path) {
			t.Errorf("%q should be public", path)
		}
	}
	for _, path := range private {
		if auth.IsPublicPath(path) {
			t.Errorf("%q should not be public", path)
		}
	}
}

// The gate exempts self-authenticating endpoints by name.
func TestGateExemptsEndpointsByName(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(func(r *httpx.Router) {
		r.POST("/api/agents/<agent_id>/heartbeat", "agents.agent_heartbeat", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.JSON(map[string]any{"agent": c.Param("agent_id")}), nil
		})
		r.POST("/api/agents/<agent_id>/other", "agents.other", func(c *httpx.Ctx) (*httpx.Response, error) {
			return httpx.JSON(map[string]any{"ok": true}), nil
		})
	})})
	wantJSON(t, srv.Post("/api/agents/a1/heartbeat").WantStatus(200), map[string]any{"agent": "a1"})
	srv.Post("/api/agents/a1/other").WantStatus(401)
}

func TestAuthDisabledLetsEverythingThroughAndLoginIsRefused(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(protectedRoutes)})
	srv.Get("/static/protected", httpxtest.Header("Authorization", "Bearer garbage")).WantStatus(200)
	wantJSON(t, srv.Get("/api/auth/status"), map[string]any{"authEnabled": false})
	wantJSON(t, srv.Post("/api/auth/login", httpxtest.JSON(map[string]any{"username": "a", "password": "b"})).
		WantStatus(400), map[string]any{"success": false, "message": "当前未开启登录验证。"})
}

func TestLoginBindingErrorsUseClientStatuses(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true})
	// N8-6 fixes Python's caught 500 responses to 400/415. The HTTP
	// differential separately asserts Python's full old body and this envelope.
	wantJSON(t, srv.Post("/api/auth/login", httpxtest.Body([]byte(`{"username":`), "application/json")).WantStatus(400),
		map[string]any{"success": false, "message": httpx.DescBadRequest})
	res := srv.Post("/api/auth/login", httpxtest.Body([]byte(`{}`), "text/plain")).WantStatus(415)
	if got := res.Object().GetString("message"); got != "Did not attempt to load JSON data because the request Content-Type was not 'application/json'." {
		t.Fatal(got)
	}
	wantJSON(t, srv.Post("/api/auth/login", httpxtest.JSON([]any{1})).WantStatus(400),
		map[string]any{"success": false, "message": httpx.DescBadRequest})
	ok := srv.Post("/api/auth/login", httpxtest.JSON(map[string]any{"username": httpxtest.Username, "password": httpxtest.Password}))
	body := ok.WantStatus(200).Object()
	if body.GetString("username") != httpxtest.Username || body.GetInt("expiresIn") != 86400 || body.GetString("token") == "" {
		t.Fatal(ok.Text())
	}
}

// A token whose own claims carry "error" is treated like verify_token's
// error dict (Python cannot tell them apart).
func TestClaimsWithErrorKeyAreRejected(t *testing.T) {
	srv := protectedServer(t)
	token, _ := auth.EncodeHS256(map[string]any{"error": "expired"}, auth.Current().SecretKey)
	res := srv.Get("/static/protected", httpxtest.Bearer(token)).WantStatus(401)
	if res.Object().GetString("error") != "token_expired" {
		t.Fatal(res.Text())
	}
	empty, _ := auth.EncodeHS256(map[string]any{}, auth.Current().SecretKey)
	res = srv.Get("/static/protected", httpxtest.Bearer(empty)).WantStatus(401)
	if res.Object().GetString("message") != "登录已失效，请重新登录。" {
		t.Fatal(res.Text())
	}
}
