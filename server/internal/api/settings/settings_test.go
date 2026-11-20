package settings_test

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"configflow/internal/api/settings"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
	"configflow/internal/weblistener"
)

const (
	proxy    = "http://proxy-user:proxy-secret@192.168.0.3:7890"
	endpoint = "/api/settings/rule-fetch-proxy"
)

func newServer(t *testing.T, auth bool) *httpxtest.Server {
	return httpxtest.New(t, httpxtest.Options{Auth: auth, Register: httpxtest.Routes(settings.Register)})
}

func assertJSON(t *testing.T, res *httpxtest.Result, want any) *httpxtest.Result {
	t.Helper()
	if diff := testjson.Diff(want, json.RawMessage(res.Body)); diff != "" {
		t.Fatalf("response JSON diff: %s\nbody: %s", diff, res.Body)
	}
	return res
}

func decodeMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	return out
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

func systemMap(t *testing.T, srv *httpxtest.Server) map[string]any {
	t.Helper()
	s, err := srv.Repo.System(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return decodeMap(t, raw)
}

func updateSettings(t *testing.T, srv *httpxtest.Server, key string, value any) {
	t.Helper()
	err := srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, s *model.System) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return s.SystemConfig.SetJSON(key, raw)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// test_rule_fetch_proxy_settings.py::test_proxy_defaults_to_empty_for_new_and_existing_settings
func TestProxyDefaultsToEmptyForNewAndExistingSettings(t *testing.T) {
	srv := newServer(t, false)
	if asMap(systemMap(t, srv)["system_config"])["rule_fetch_proxy"] != "" {
		t.Fatal("default")
	}
	err := srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, s *model.System) error {
		s.SystemConfig.RuleFetchProxy = ""
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, srv.Get(endpoint), map[string]any{"rule_fetch_proxy": ""})
}

// test_rule_fetch_proxy_settings.py::test_save_proxy_is_global_persistent_and_can_be_cleared
func TestSaveProxyIsGlobalPersistentAndCanBeCleared(t *testing.T) {
	srv := newServer(t, false)
	if _, err := srv.Repo.CreateProfileRecord(srv.Context(), repo.ProfileCreation{ID: "another", Name: "Another"}, ""); err != nil {
		t.Fatal(err)
	}
	res := srv.Post(endpoint, httpxtest.JSON(map[string]any{"rule_fetch_proxy": " " + proxy + " "})).WantStatus(200)
	if asString(decodeMap(t, res.Body)["rule_fetch_proxy"]) != proxy {
		t.Fatal(res.Text())
	}
	reopened, err := repo.Open(srv.DataDir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := reopened.System(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if saved.SystemConfig.RuleFetchProxy != proxy {
		t.Fatal("not persisted")
	}
	assertJSON(t, srv.Get(endpoint, httpxtest.Header("X-ConfigFlow-Profile", "another")), map[string]any{"rule_fetch_proxy": proxy})
	srv.Post(endpoint, httpxtest.JSON(map[string]any{"rule_fetch_proxy": "  "})).WantStatus(200)
	assertJSON(t, srv.Get(endpoint), map[string]any{"rule_fetch_proxy": ""})
}

// test_rule_fetch_proxy_settings.py::test_invalid_proxy_does_not_overwrite_previous_or_echo_credentials
func TestInvalidProxyDoesNotOverwritePreviousOrEchoCredentials(t *testing.T) {
	values := []any{nil, false, 123, []any{}, map[string]any{}, "192.168.0.3:7890",
		"socks5://proxy-user:proxy-secret@proxy.test:1080",
		"http://proxy-user:proxy-secret@proxy.test:invalid",
		"http://proxy-user:proxy-secret@proxy.test:70000",
		"http://proxy-user:proxy-secret@proxy.test:7890/rules",
		"http://proxy-user:proxy-secret@proxy.test:7890?token=secret",
		"http://proxy-user:proxy-secret@proxy.test:7890#fragment"}
	for _, value := range values {
		srv := newServer(t, false)
		updateSettings(t, srv, "rule_fetch_proxy", proxy)
		before := systemMap(t, srv)
		res := srv.Post(endpoint, httpxtest.JSON(map[string]any{"rule_fetch_proxy": value})).WantStatus(400)
		if !testjson.Equal(systemMap(t, srv), before) || res.Contains("proxy-secret") || res.Contains("proxy-user") {
			t.Fatalf("%#v: %s", value, res.Text())
		}
	}
}

// test_rule_fetch_proxy_settings.py::test_missing_proxy_field_cannot_accidentally_clear_setting
func TestMissingProxyFieldCannotAccidentallyClearSetting(t *testing.T) {
	for _, body := range []any{map[string]any{}, []any{}, map[string]any{"unrelated": proxy}} {
		srv := newServer(t, false)
		updateSettings(t, srv, "rule_fetch_proxy", proxy)
		srv.Post(endpoint, httpxtest.JSON(body)).WantStatus(400)
		if asString(asMap(systemMap(t, srv)["system_config"])["rule_fetch_proxy"]) != proxy {
			t.Fatal("cleared")
		}
	}
}

// test_rule_fetch_proxy_settings.py::test_proxy_setting_requires_authentication
func TestProxySettingRequiresAuthentication(t *testing.T) {
	srv := newServer(t, true)
	srv.Get(endpoint).WantStatus(401)
	srv.Post(endpoint, httpxtest.JSON(map[string]any{"rule_fetch_proxy": proxy})).WantStatus(401)
}

// test_security_regressions.py::test_anonymous_config_token_response_does_not_expose_internal_rule_proxy_token
func TestAnonymousConfigTokenResponseDoesNotExposeInternalRuleProxyToken(t *testing.T) {
	srv := newServer(t, false)
	updateSettings(t, srv, "rule_proxy_token", "internal-rule-proxy-token")
	res := assertJSON(t, srv.Get("/api/config-token").WantStatus(200), map[string]any{"config_token": ""})
	if res.Contains("internal-rule-proxy-token") {
		t.Fatal("leak")
	}
}

// test_security_regressions.py::test_config_token_post_rejects_internal_rule_proxy_token
func TestConfigTokenPostRejectsInternalRuleProxyToken(t *testing.T) {
	srv := newServer(t, false)
	updateSettings(t, srv, "rule_proxy_token", "internal-rule-proxy-token")
	updateSettings(t, srv, "config_token", "public-token")
	before := systemMap(t, srv)["system_config"]
	res := srv.Post("/api/config-token", httpxtest.JSON(map[string]any{"token": "internal-rule-proxy-token"})).WantStatus(400)
	if decodeMap(t, res.Body)["success"] != false || !testjson.Equal(systemMap(t, srv)["system_config"], before) {
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
		before := systemMap(t, srv)["system_config"]
		for _, internal := range []string{"current-internal-token", "retired-internal-token"} {
			encoded := internal
			for i := 0; i < layers; i++ {
				if i%2 == 0 {
					encoded = strings.ReplaceAll(url.QueryEscape(encoded), "+", "%20")
				} else {
					encoded = url.QueryEscape(encoded)
				}
			}
			srv.Post("/api/config-token", httpxtest.JSON(map[string]any{"token": "managed::" + encoded + "::suffix"})).WantStatus(400)
			if !testjson.Equal(systemMap(t, srv)["system_config"], before) {
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
	res := assertJSON(t, srv.Get("/api/config-token").WantStatus(200), map[string]any{"config_token": "[REDACTED]"})
	if res.Contains("internal-token-x") {
		t.Fatal("leak")
	}
}

func TestConfigTokenLifecycle(t *testing.T) {
	srv := newServer(t, false)
	res := srv.Post("/api/config-token", httpxtest.JSON(map[string]any{"generate": true})).WantStatus(200)
	token := asString(decodeMap(t, res.Body)["config_token"])
	if len(token) != 32 {
		t.Fatal(res.Text())
	}
	assertJSON(t, srv.Get("/api/config-token"), map[string]any{"config_token": token})
	assertJSON(t, srv.Post("/api/config-token", httpxtest.JSON(map[string]any{"token": "  "})).WantStatus(400),
		map[string]any{"success": false, "message": "Token is required or set generate=true"})
	assertJSON(t, srv.Post("/api/config-token", httpxtest.JSON(map[string]any{"token": 5})).WantStatus(500),
		map[string]any{"success": false, "message": "setting must be a string"})
	assertJSON(t, srv.Delete("/api/config-token").WantStatus(200), map[string]any{"success": true})
	assertJSON(t, srv.Get("/api/config-token"), map[string]any{"config_token": ""})
}

func TestServerDomainAndAggregationAndVersion(t *testing.T) {
	srv := newServer(t, false)
	assertJSON(t, srv.Get("/api/server-domain"), map[string]any{"server_domain": ""})
	assertJSON(t, srv.Post("/api/server-domain", httpxtest.JSON(map[string]any{"new_domain": " https://cf.example "})),
		map[string]any{"success": true, "server_domain": "https://cf.example"})
	srv.Post("/api/server-domain", httpxtest.JSON(map[string]any{"new_domain": ""})).WantStatus(400)
	srv.Post("/api/server-domain", httpxtest.Body([]byte("x"), "text/plain")).WantStatus(415)
	srv.Do("HEAD", "/api/server-domain").WantStatus(200)
	assertJSON(t, srv.Get("/api/settings/subscription-aggregation"), map[string]any{"enabled": false})
	assertJSON(t, srv.Post("/api/settings/subscription-aggregation", httpxtest.JSON(map[string]any{"enabled": "yes"})),
		map[string]any{"success": true, "enabled": "yes"})
	assertJSON(t, srv.Get("/api/version"), map[string]any{"version": "dev"})
}

func TestBackupConfigMasksPasswordAndValidates(t *testing.T) {
	srv := newServer(t, false)
	assertJSON(t, srv.Get("/api/backup/config"), map[string]any{
		"webdav_url": "", "webdav_username": "", "webdav_password": "",
		"webdav_path": "/config-flow-backup/", "auto_backup": false, "keep_latest": 30, "keep_daily": 30,
	})
	assertJSON(t, srv.Post("/api/backup/config", httpxtest.JSON(map[string]any{
		"webdav_url": "https://dav.example", "webdav_password": "pw", "keep_latest": 5,
	})), map[string]any{"success": true, "message": "备份配置已保存"})
	srv.Post("/api/backup/config", httpxtest.JSON(map[string]any{"webdav_password": "******"})).WantStatus(200)
	got := decodeMap(t, srv.Get("/api/backup/config").Body)
	if got["webdav_password"] != "******" || asInt(got["keep_latest"]) != 5 {
		t.Fatalf("%#v", got)
	}
	backup := asMap(systemMap(t, srv)["backup"])
	if asString(backup["webdav_password"]) != "pw" {
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
		assertJSON(t, srv.Post("/api/backup/config", httpxtest.Body([]byte(body), "application/json")).WantStatus(400),
			map[string]any{"success": false, "message": message})
	}
}

func TestWebPortUnsupportedWithoutWebFrontEnd(t *testing.T) {
	settings.SetWebListener(nil)
	srv := newServer(t, false)
	assertJSON(t, srv.Get("/api/settings/web-port"), weblistener.UnsupportedInformation())
	assertJSON(t, srv.Post("/api/settings/web-port", httpxtest.JSON(map[string]any{})).WantStatus(400),
		map[string]any{"success": false, "message": "请提供 Web 监听端口"})
	srv.Post("/api/settings/web-port", httpxtest.JSON(map[string]any{"port": "80"})).WantStatus(400)
	assertJSON(t, srv.Post("/api/settings/web-port", httpxtest.JSON(map[string]any{"port": 8080})).WantStatus(503),
		map[string]any{"success": false, "message": "当前部署不支持在 Web 中修改监听端口"})
}
