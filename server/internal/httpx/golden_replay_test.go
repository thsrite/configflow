package httpx_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/testgolden"
	"configflow/internal/version"
)

const (
	hxUser          = "diff-admin"
	hxPass          = "diff-password"
	hxSecret        = "diff-shared-jwt-secret"
	hxInternalToken = "diff-internal-rule-proxy-token"
	hxAgentToken    = "diff-agent-token-0123456789"
)

var (
	hxISO = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	hxHex = regexp.MustCompile(`^[0-9a-f]{32,}$`)
)

func normalizeHTTPXGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			switch k {
			case "token", "config_token":
				if s, ok := val.(string); ok && s != "" && k == "token" {
					out[k] = "<jwt>"
					continue
				}
			case "updated_at", "created_at":
				if s, ok := val.(string); ok && s != "" {
					out[k] = "<time>"
					continue
				}
			case "version":
				if s, ok := val.(string); ok && s == version.Version {
					out[k] = "<app-version>"
					continue
				}
			}
			out[k] = normalizeHTTPXGolden(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeHTTPXGolden(item)
		}
		return out
	case string:
		if hxISO.MatchString(x) {
			return "<time>"
		}
		if hxHex.MatchString(x) {
			return "<hex>"
		}
		return x
	}
	return v
}

func TestHTTPXAuthAndSettingsGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	prev := auth.Configure(auth.Config{AdminUsername: hxUser, AdminPassword: hxPass, SecretKey: hxSecret})
	t.Cleanup(func() { auth.Configure(prev) })

	r, err := repo.OpenDefaultWithOptions(t.TempDir(), server.RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	ctx := context.Background()
	if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		sys.SystemConfig.ServerDomain = "http://127.0.0.1:5001"
		sys.SystemConfig.RuleProxyToken = hxInternalToken
		sys.SystemConfig.RetiredRuleProxyTokens = []string{"diff-retired-token"}
		agentsRaw, _ := json.Marshal([]any{map[string]any{
			"id": "agent-1", "name": "diff agent", "token": hxAgentToken,
			"profile_id": "default", "service_type": "mihomo", "host": "127.0.0.1", "port": 9,
		}})
		if err := sys.SetJSON("agents", agentsRaw); err != nil {
			return err
		}
		return sys.SetJSON("backup", json.RawMessage(`{"webdav_url":"","webdav_username":"","webdav_password":"","auto_backup":false}`))
	}); err != nil {
		t.Fatal(err)
	}

	full, err := server.New(ctx, server.Options{Repo: r})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	app := full.HTTP
	adminTok, err := auth.EncodeHS256(map[string]any{"username": hxUser, "exp": time.Now().Add(time.Hour).Unix()}, hxSecret)
	if err != nil {
		t.Fatal(err)
	}
	expiredTok, _ := auth.EncodeHS256(map[string]any{"username": hxUser, "exp": time.Now().Add(-time.Hour).Unix()}, hxSecret)
	forgedTok, _ := auth.EncodeHS256(map[string]any{"username": hxUser, "exp": time.Now().Add(time.Hour).Unix()}, "wrong-secret")

	type req struct {
		name, method, path, body, contentType, authMode string
		headers                                         map[string]string
		maskConfigToken                                 bool
	}
	const J = "application/json"
	origin := map[string]string{"Origin": "https://client.example"}
	rows := []req{
		{"auth-status", "GET", "/api/auth/status", "", "", "", nil, false},
		{"auth-status-origin", "GET", "/api/auth/status", "", "", "", origin, false},
		{"login-wrong-pass", "POST", "/api/auth/login", `{"username":"diff-admin","password":"x"}`, J, "", nil, false},
		{"login-ok", "POST", "/api/auth/login", `{"username":"diff-admin","password":"diff-password"}`, J, "", nil, false},
		{"login-get-405", "GET", "/api/auth/login", "", "", "", nil, false},
		{"verify-no-token", "GET", "/api/auth/verify", "", "", "", nil, false},
		{"verify-admin", "GET", "/api/auth/verify", "", "", "admin", nil, false},
		{"verify-expired", "GET", "/api/auth/verify", "", "", "expired", nil, false},
		{"verify-forged", "GET", "/api/auth/verify", "", "", "forged", nil, false},
		{"verify-malformed", "GET", "/api/auth/verify", "", "", "malformed", nil, false},
		{"version-get", "GET", "/api/version", "", "", "", nil, false},
		{"version-head", "HEAD", "/api/version", "", "", "", nil, false},
		{"version-put-405", "PUT", "/api/version", "", "", "", nil, false},
		{"unknown-no-token", "GET", "/api/missing", "", "", "", nil, false},
		{"unknown-admin", "GET", "/api/missing", "", "", "admin", nil, false},
		{"preflight-public", "OPTIONS", "/api/auth/login", "", "", "", map[string]string{"Origin": "https://client.example", "Access-Control-Request-Method": "POST"}, false},
		{"preflight-protected", "OPTIONS", "/api/server-domain", "", "", "", map[string]string{"Origin": "https://client.example", "Access-Control-Request-Method": "GET"}, false},
		{"server-domain-get", "GET", "/api/server-domain", "", "", "admin", nil, false},
		{"server-domain-set", "POST", "/api/server-domain", `{"new_domain":" http://127.0.0.1:5002 "}`, J, "admin", nil, false},
		{"server-domain-empty", "POST", "/api/server-domain", `{"new_domain":""}`, J, "admin", nil, false},
		{"server-domain-number", "POST", "/api/server-domain", `{"new_domain":5}`, J, "admin", nil, false},
		{"config-token-empty", "GET", "/api/config-token", "", "", "admin", nil, false},
		{"config-token-internal", "POST", "/api/config-token", `{"token":"x-` + hxInternalToken + `"}`, J, "admin", nil, false},
		{"config-token-blank", "POST", "/api/config-token", `{"token":"  "}`, J, "admin", nil, false},
		{"config-token-set", "POST", "/api/config-token", `{"token":" pull-token "}`, J, "admin", nil, false},
		{"config-token-generate", "POST", "/api/config-token", `{"generate":true}`, J, "admin", nil, true},
		{"config-token-delete", "DELETE", "/api/config-token", "", "", "admin", nil, false},
		{"proxy-get", "GET", "/api/settings/rule-fetch-proxy", "", "", "admin", nil, false},
		{"proxy-set", "POST", "/api/settings/rule-fetch-proxy", `{"rule_fetch_proxy":" http://u:p@192.0.2.3:7890/ "}`, J, "admin", nil, false},
		{"proxy-invalid", "POST", "/api/settings/rule-fetch-proxy", `{"rule_fetch_proxy":"socks5://x:1"}`, J, "admin", nil, false},
		{"proxy-clear", "POST", "/api/settings/rule-fetch-proxy", `{"rule_fetch_proxy":""}`, J, "admin", nil, false},
		{"agg-get", "GET", "/api/settings/subscription-aggregation", "", "", "admin", nil, false},
		{"agg-set", "POST", "/api/settings/subscription-aggregation", `{"enabled":true}`, J, "admin", nil, false},
		{"agg-bad", "POST", "/api/settings/subscription-aggregation", `[]`, J, "admin", nil, false},
		{"web-port-get", "GET", "/api/settings/web-port", "", "", "admin", nil, false},
		{"web-port-reserved", "POST", "/api/settings/web-port", `{"port":3001}`, J, "admin", nil, false},
		{"web-port-bad", "POST", "/api/settings/web-port", `{"port":"80"}`, J, "admin", nil, false},
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			httpReq := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				httpReq.Header.Set("Content-Type", tc.contentType)
			}
			for k, v := range tc.headers {
				httpReq.Header.Set(k, v)
			}
			switch tc.authMode {
			case "admin":
				httpReq.Header.Set("Authorization", "Bearer "+adminTok)
			case "expired":
				httpReq.Header.Set("Authorization", "Bearer "+expiredTok)
			case "forged":
				httpReq.Header.Set("Authorization", "Bearer "+forgedTok)
			case "malformed":
				httpReq.Header.Set("Authorization", "Bearer")
			}
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, httpReq)
			var parsed any
			if tc.method == "HEAD" {
				parsed = ""
			} else if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
				parsed = rec.Body.String()
			}
			if tc.maskConfigToken {
				if m, ok := parsed.(map[string]any); ok {
					if _, has := m["config_token"]; has {
						m["config_token"] = "<generated>"
					}
				}
			}
			key, _ := json.Marshal(map[string]string{"case": tc.name})
			val, _ := json.Marshal(normalizeHTTPXGolden(map[string]any{
				"status":       rec.Code,
				"content_type": rec.Header().Get("Content-Type"),
				"acao":         rec.Header().Get("Access-Control-Allow-Origin"),
				"body":         parsed,
			}))
			ref.Assert(t, key, val)
		})
	}
}
