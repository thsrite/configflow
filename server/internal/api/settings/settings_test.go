package settings_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"configflow/internal/api/settings"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/weblistener"
)

const (
	proxy    = "http://proxy-user:proxy-secret@192.168.0.3:7890"
	endpoint = "/api/settings/rule-fetch-proxy"
)

func newServer(t *testing.T, auth bool) *httpxtest.Server {
	return httpxtest.New(t, httpxtest.Options{Auth: auth, Register: httpxtest.Routes(settings.Register)})
}

func system(t *testing.T, srv *httpxtest.Server) *jsonx.Object {
	t.Helper()
	s, err := srv.Repo.GetSystem(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func updateSettings(t *testing.T, srv *httpxtest.Server, key string, value any) {
	t.Helper()
	_, err := srv.Repo.UpdateSystemTransaction(srv.Context(), func(_ context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.GetObject("system_config").Set(key, value)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// test_rule_fetch_proxy_settings.py::test_proxy_defaults_to_empty_for_new_and_existing_settings
func TestProxyDefaultsToEmptyForNewAndExistingSettings(t *testing.T) {
	srv := newServer(t, false)
	if system(t, srv).GetObject("system_config").Value("rule_fetch_proxy") != "" {
		t.Fatal("default")
	}
	_, err := srv.Repo.UpdateSystemTransaction(srv.Context(), func(_ context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.GetObject("system_config").Delete("rule_fetch_proxy")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Get(endpoint).WantJSON(jsonx.Obj("rule_fetch_proxy", ""))
}

// test_rule_fetch_proxy_settings.py::test_save_proxy_is_global_persistent_and_can_be_cleared
func TestSaveProxyIsGlobalPersistentAndCanBeCleared(t *testing.T) {
	srv := newServer(t, false)
	if _, err := srv.Repo.CreateProfile(srv.Context(), jsonx.Obj("id", "another", "name", "Another"), ""); err != nil {
		t.Fatal(err)
	}
	res := srv.Post(endpoint, httpxtest.JSON(jsonx.Obj("rule_fetch_proxy", " "+proxy+" "))).WantStatus(200)
	if res.Object().GetString("rule_fetch_proxy") != proxy {
		t.Fatal(res.Text())
	}
	reopened, err := repo.Open(srv.DataDir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := reopened.GetSystem(context.Background())
	if saved.GetObject("system_config").GetString("rule_fetch_proxy") != proxy {
		t.Fatal("not persisted")
	}
	srv.Get(endpoint, httpxtest.Header("X-ConfigFlow-Profile", "another")).WantJSON(jsonx.Obj("rule_fetch_proxy", proxy))
	srv.Post(endpoint, httpxtest.JSON(jsonx.Obj("rule_fetch_proxy", "  "))).WantStatus(200)
	srv.Get(endpoint).WantJSON(jsonx.Obj("rule_fetch_proxy", ""))
}

// test_rule_fetch_proxy_settings.py::test_invalid_proxy_does_not_overwrite_previous_or_echo_credentials
func TestInvalidProxyDoesNotOverwritePreviousOrEchoCredentials(t *testing.T) {
	values := []any{nil, false, 123, []any{}, jsonx.NewObject(), "192.168.0.3:7890",
		"socks5://proxy-user:proxy-secret@proxy.test:1080",
		"http://proxy-user:proxy-secret@proxy.test:invalid",
		"http://proxy-user:proxy-secret@proxy.test:70000",
		"http://proxy-user:proxy-secret@proxy.test:7890/rules",
		"http://proxy-user:proxy-secret@proxy.test:7890?token=secret",
		"http://proxy-user:proxy-secret@proxy.test:7890#fragment"}
	for _, value := range values {
		srv := newServer(t, false)
		updateSettings(t, srv, "rule_fetch_proxy", proxy)
		before := system(t, srv)
		res := srv.Post(endpoint, httpxtest.JSON(jsonx.Obj("rule_fetch_proxy", value))).WantStatus(400)
		if !jsonx.Equal(system(t, srv), before) || res.Contains("proxy-secret") || res.Contains("proxy-user") {
			t.Fatalf("%v: %s", jsonx.Repr(value), res.Text())
		}
	}
}

// test_rule_fetch_proxy_settings.py::test_missing_proxy_field_cannot_accidentally_clear_setting
func TestMissingProxyFieldCannotAccidentallyClearSetting(t *testing.T) {
	for _, body := range []any{jsonx.NewObject(), []any{}, jsonx.Obj("unrelated", proxy)} {
		srv := newServer(t, false)
		updateSettings(t, srv, "rule_fetch_proxy", proxy)
		srv.Post(endpoint, httpxtest.JSON(body)).WantStatus(400)
		if system(t, srv).GetObject("system_config").GetString("rule_fetch_proxy") != proxy {
			t.Fatal("cleared")
		}
	}
}

// test_rule_fetch_proxy_settings.py::test_proxy_setting_requires_authentication
func TestProxySettingRequiresAuthentication(t *testing.T) {
	srv := newServer(t, true)
	srv.Get(endpoint).WantStatus(401)
	srv.Post(endpoint, httpxtest.JSON(jsonx.Obj("rule_fetch_proxy", proxy))).WantStatus(401)
}

// test_security_regressions.py::test_anonymous_config_token_response_does_not_expose_internal_rule_proxy_token
func TestAnonymousConfigTokenResponseDoesNotExposeInternalRuleProxyToken(t *testing.T) {
	srv := newServer(t, false)
	updateSettings(t, srv, "rule_proxy_token", "internal-rule-proxy-token")
	res := srv.Get("/api/config-token").WantStatus(200).WantJSON(jsonx.Obj("config_token", ""))
	if res.Contains("internal-rule-proxy-token") {
		t.Fatal("leak")
	}
}

// test_security_regressions.py::test_config_token_post_rejects_internal_rule_proxy_token
func TestConfigTokenPostRejectsInternalRuleProxyToken(t *testing.T) {
	srv := newServer(t, false)
	updateSettings(t, srv, "rule_proxy_token", "internal-rule-proxy-token")
	updateSettings(t, srv, "config_token", "public-token")
	before := system(t, srv).GetObject("system_config")
	res := srv.Post("/api/config-token", httpxtest.JSON(jsonx.Obj("token", "internal-rule-proxy-token"))).WantStatus(400)
	if res.Object().Value("success") != false || !jsonx.Equal(system(t, srv).GetObject("system_config"), before) {
		t.Fatal(res.Text())
	}
}

// test_security_regressions.py::test_config_token_post_rejects_embedded_encoded_current_or_retired_internal_token
func TestConfigTokenPostRejectsEmbeddedEncodedCurrentOrRetiredInternalToken(t *testing.T) {
	for _, layers := range []int{0, 1, 2, 8, 20} {
		srv := newServer(t, false)
		updateSettings(t, srv, "rule_proxy_token", "current-internal-token")
		updateSettings(t, srv, "retired_rule_proxy_tokens", []any{"retired-internal-token"})
		updateSettings(t, srv, "config_token", "public-token")
		before := system(t, srv).GetObject("system_config")
		for _, internal := range []string{"current-internal-token", "retired-internal-token"} {
			encoded := internal
			for i := 0; i < layers; i++ {
				if i%2 == 0 {
					encoded = strings.ReplaceAll(url.QueryEscape(encoded), "+", "%20")
				} else {
					encoded = url.QueryEscape(encoded)
				}
			}
			srv.Post("/api/config-token", httpxtest.JSON(jsonx.Obj("token", "managed::"+encoded+"::suffix"))).WantStatus(400)
			if !jsonx.Equal(system(t, srv).GetObject("system_config"), before) {
				t.Fatal("changed")
			}
		}
	}
}

// test_security_regressions.py::test_config_token_get_scrubs_legacy_embedded_internal_token
func TestConfigTokenGetScrubsLegacyEmbeddedInternalToken(t *testing.T) {
	srv := newServer(t, false)
	updateSettings(t, srv, "rule_proxy_token", "internal-token-x")
	updateSettings(t, srv, "config_token", "legacy::internal-token-x::embedded")
	res := srv.Get("/api/config-token").WantStatus(200).WantJSON(jsonx.Obj("config_token", "[REDACTED]"))
	if res.Contains("internal-token-x") {
		t.Fatal("leak")
	}
}

func TestConfigTokenLifecycle(t *testing.T) {
	srv := newServer(t, false)
	res := srv.Post("/api/config-token", httpxtest.JSON(jsonx.Obj("generate", true))).WantStatus(200)
	token := res.Object().GetString("config_token")
	if len(token) != 32 {
		t.Fatal(res.Text())
	}
	srv.Get("/api/config-token").WantJSON(jsonx.Obj("config_token", token))
	srv.Post("/api/config-token", httpxtest.JSON(jsonx.Obj("token", "  "))).WantStatus(400).
		WantJSON(jsonx.Obj("success", false, "message", "Token is required or set generate=true"))
	srv.Post("/api/config-token", httpxtest.JSON(jsonx.Obj("token", 5))).WantStatus(500).
		WantJSON(jsonx.Obj("success", false, "message", "setting must be a string"))
	srv.Delete("/api/config-token").WantStatus(200).WantJSON(jsonx.Obj("success", true))
	srv.Get("/api/config-token").WantJSON(jsonx.Obj("config_token", ""))
}

func TestServerDomainAndAggregationAndVersion(t *testing.T) {
	srv := newServer(t, false)
	srv.Get("/api/server-domain").WantJSON(jsonx.Obj("server_domain", ""))
	srv.Post("/api/server-domain", httpxtest.JSON(jsonx.Obj("new_domain", " https://cf.example "))).
		WantJSON(jsonx.Obj("success", true, "server_domain", "https://cf.example"))
	srv.Post("/api/server-domain", httpxtest.JSON(jsonx.Obj("new_domain", ""))).WantStatus(400)
	srv.Post("/api/server-domain", httpxtest.Body([]byte("x"), "text/plain")).WantStatus(415)
	srv.Do("HEAD", "/api/server-domain").WantStatus(200)
	srv.Get("/api/settings/subscription-aggregation").WantJSON(jsonx.Obj("enabled", false))
	srv.Post("/api/settings/subscription-aggregation", httpxtest.JSON(jsonx.Obj("enabled", "yes"))).
		WantJSON(jsonx.Obj("success", true, "enabled", "yes"))
	srv.Get("/api/version").WantJSON(jsonx.Obj("version", "dev"))
}

func TestBackupConfigMasksPasswordAndValidates(t *testing.T) {
	srv := newServer(t, false)
	srv.Get("/api/backup/config").WantJSON(jsonx.Obj("webdav_url", "", "webdav_username", "", "webdav_password", "",
		"webdav_path", "/config-flow-backup/", "auto_backup", false, "keep_latest", 30, "keep_daily", 30))
	srv.Post("/api/backup/config", httpxtest.JSON(jsonx.Obj("webdav_url", "https://dav.example", "webdav_password", "pw",
		"keep_latest", 5))).WantJSON(jsonx.Obj("success", true, "message", "备份配置已保存"))
	srv.Post("/api/backup/config", httpxtest.JSON(jsonx.Obj("webdav_password", "******"))).WantStatus(200)
	got := srv.Get("/api/backup/config").Object()
	if got.Value("webdav_password") != "******" || got.GetInt("keep_latest") != 5 {
		t.Fatal(jsonx.Repr(got))
	}
	backup := system(t, srv).GetObject("backup")
	if backup.GetString("webdav_password") != "pw" {
		t.Fatal("the mask keeps the saved password")
	}
	for body, message := range map[string]string{
		`{}`:                    "请求体必须是非空 JSON 对象，请使用 Content-Type: application/json",
		`{"x": 1}`:              "请提供 WebDAV 备份配置字段",
		`{"webdav_url": 1}`:     "WebDAV 地址、用户名、密码和路径必须是字符串",
		`{"auto_backup": 1}`:    "auto_backup 必须是布尔值",
		`{"keep_latest": 1001}`: "保留份数须为 0–1000 的整数，每日保留天数须为 0–3650 的整数",
		`{"keep_daily": true}`:  "保留份数须为 0–1000 的整数，每日保留天数须为 0–3650 的整数",
		`{"keep_daily": 5.0}`:   "保留份数须为 0–1000 的整数，每日保留天数须为 0–3650 的整数",
	} {
		srv.Post("/api/backup/config", httpxtest.Body([]byte(body), "application/json")).WantStatus(400).
			WantJSON(jsonx.Obj("success", false, "message", message))
	}
}

func TestWebPortUnsupportedWithoutWebFrontEnd(t *testing.T) {
	settings.SetWebListener(nil)
	srv := newServer(t, false)
	srv.Get("/api/settings/web-port").WantJSON(weblistener.Unsupported())
	srv.Post("/api/settings/web-port", httpxtest.JSON(jsonx.NewObject())).WantStatus(400).
		WantJSON(jsonx.Obj("success", false, "message", "请提供 Web 监听端口"))
	srv.Post("/api/settings/web-port", httpxtest.JSON(jsonx.Obj("port", "80"))).WantStatus(400)
	srv.Post("/api/settings/web-port", httpxtest.JSON(jsonx.Obj("port", 8080))).WantStatus(503).
		WantJSON(jsonx.Obj("success", false, "message", "当前部署不支持在 Web 中修改监听端口"))
}
