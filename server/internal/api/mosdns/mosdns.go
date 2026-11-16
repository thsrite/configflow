// Package mosdns holds the routes of backend/routes/mosdns.py (MosDNS
// editor settings and the public rule proxy) and the profile wrapper
// /api/profiles/<profile_id>/mosdns/rule-proxy of routes/profiles.py.
package mosdns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/repo"
)

// Register adds the MosDNS routes.
func Register(r *httpx.Router) {
	r.POST("/api/mosdns/settings", "mosdns.save_mosdns_settings", auth.Require(saveSettings))
	for _, s := range sections {
		r.Handle([]string{"GET", "POST"}, "/api/mosdns/"+s.path, "mosdns."+s.endpoint, auth.Require(s.handle))
	}
	// Public: the rule proxy checks its own token or JWT.
	r.GET("/api/mosdns/rule-proxy", "mosdns.mosdns_rule_proxy", ruleProxy)
	r.GET("/api/profiles/<profile_id>/mosdns/rule-proxy", "profiles.profile_mosdns_rule_proxy", ruleProxy)
}

// field is a stored setting, its name in the request/response and its
// default (JSON).
type field struct{ key, name, def string }

// section is one editor route: GET returns its fields, POST stores them.
type section struct {
	path, endpoint string
	fields         []field
	cache          bool // cache-settings: normalized, answers a publish guard
}

var sections = []section{
	{"rulesets", "handle_mosdns_rulesets", []field{{"direct_rulesets", "direct_rulesets", "[]"},
		{"proxy_rulesets", "proxy_rulesets", "[]"}, {"direct_rules", "direct_rules", "[]"}, {"proxy_rules", "proxy_rules", "[]"}}, false},
	{"custom-matches", "handle_mosdns_custom_matches", []field{{"custom_matches", "custom_matches", "[]"},
		{"custom_match_position", "position", `"tail"`}}, false},
	{"dns-servers", "handle_mosdns_dns_servers", []field{{"local_dns", "local_dns", `""`}, {"remote_dns", "remote_dns", `""`},
		{"fallback_dns", "fallback_dns", `""`}, {"default_forward", "default_forward", `"forward_remote"`},
		{"custom_hosts", "custom_hosts", `""`}}, false},
	{"log-settings", "handle_mosdns_log_settings", []field{{"log_enabled", "log_enabled", "true"},
		{"log_level", "log_level", `"info"`}, {"log_file", "log_file", `""`}}, false},
	{"api-settings", "handle_mosdns_api_settings", []field{{"api_enabled", "api_enabled", "true"},
		{"api_address", "api_address", `":8080"`}}, false},
	{"cache-settings", "handle_mosdns_cache_settings", []field{{"cache_enabled", "cache_enabled", "true"},
		{"cache_size", "cache_size", "10240"}, {"cache_lazy_ttl", "cache_lazy_ttl", "21600"},
		{"cache_dump_enabled", "cache_dump_enabled", "true"}, {"cache_dump_file", "cache_dump_file", `"./cache.dump"`},
		{"cache_dump_interval", "cache_dump_interval", "300"}}, true},
}

// baseDefaults seed a profile without MosDNS settings (plus the route's
// own fields).
var baseDefaults = append(append([]field{}, sections[0].fields...), field{"local_dns", "", `""`}, field{"remote_dns", "", `""`},
	field{"fallback_dns", "", `""`}, field{"default_forward", "", `"forward_remote"`}, field{"custom_hosts", "", `""`},
	field{"custom_config", "", `""`})

func value(raw string) any {
	v, _ := jsonx.Parse([]byte(raw))
	return v
}

// body is the request's JSON object; empty for a falsy body when
// allowEmpty (`request.json or {}`).
func body(c *httpx.Ctx, allowEmpty bool) (map[string]json.RawMessage, error) {
	parsed, err := c.JSON()
	if err != nil {
		return nil, err
	}
	if allowEmpty && !jsonx.Truthy(parsed) {
		return map[string]json.RawMessage{}, nil
	}
	data, _ := c.Data()
	var out map[string]json.RawMessage
	if json.Unmarshal(data, &out) != nil || out == nil {
		return nil, errors.New("request body is not a JSON object")
	}
	return out, nil
}

func (s section) handle(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := context.WithoutCancel(c.Context())
	config, err := repo.GetConfig(ctx, "")
	if err != nil {
		return nil, err
	}
	if !config.Has("mosdns") {
		defaults := jsonx.NewObject()
		for _, f := range append(append([]field{}, baseDefaults...), s.fields...) {
			defaults.Set(f.key, value(f.def))
		}
		config.Set("mosdns", defaults)
	}
	settings, ok := config.Value("mosdns").(*jsonx.Object)
	if !ok || settings == nil {
		return nil, errors.New("mosdns settings are not an object")
	}
	for _, f := range s.fields {
		if s.cache && !settings.Has(f.key) { // older configurations lack them
			settings.Set(f.key, value(f.def))
		}
	}
	if c.Request.Method == "GET" {
		out := jsonx.NewObject()
		for _, f := range s.fields {
			def := value(f.def)
			if f.key == "api_address" { // legacy field name
				def = settings.GetOr("api_addr", def)
			}
			out.Set(f.name, settings.GetOr(f.key, def))
		}
		return httpx.JSON(out), nil
	}
	if c.Request.Method != "POST" {
		return nil, nil // HEAD: Flask's view returns None (500)
	}
	data, err := body(c, s.cache)
	if err != nil {
		return httpx.Message(500, false, err.Error()), nil
	}
	if s.cache {
		normalizeCache(settings, data)
		guard, err := repo.SaveConfigWithPublishGuard(ctx, config, "mosdns", "")
		if err != nil {
			return httpx.Message(500, false, err.Error()), nil
		}
		return httpx.JSON(jsonx.Obj("success", true, "publish_guard", guard)), nil
	}
	for _, f := range s.fields {
		settings.Set(f.key, fromBody(data, f.name, f.def))
	}
	if s.path == "api-settings" {
		settings.Delete("api_addr")
	}
	if err := repo.SaveConfig(ctx, config, ""); err != nil {
		return httpx.Message(500, false, err.Error()), nil
	}
	return httpx.JSON(jsonx.Obj("success", true)), nil
}

func fromBody(data map[string]json.RawMessage, name, def string) any {
	if raw, ok := data[name]; ok {
		return value(string(raw))
	}
	return value(def)
}

// normalizeCache stores the cache fields of a save, normalized the same
// way for whole-editor and cache-only saves.
func normalizeCache(settings *jsonx.Object, data map[string]json.RawMessage) {
	interval := intOf(data["cache_dump_interval"], 300)
	if interval.Sign() <= 0 {
		interval = big.NewInt(300)
	}
	dumpFile := "./cache.dump"
	if raw, ok := data["cache_dump_file"]; ok && string(raw) != "null" {
		if json.Unmarshal(raw, &dumpFile) != nil {
			dumpFile = string(raw)
		}
	}
	settings.Set("cache_enabled", truthy(data["cache_enabled"]))
	settings.Set("cache_dump_enabled", truthy(data["cache_dump_enabled"]))
	settings.Set("cache_size", jsonx.BigInt(intOf(data["cache_size"], 10240)))
	settings.Set("cache_lazy_ttl", jsonx.BigInt(intOf(data["cache_lazy_ttl"], 21600)))
	settings.Set("cache_dump_interval", jsonx.BigInt(interval))
	settings.Set("cache_dump_file", dumpFile)
}

// truthy is the truth value of a JSON value (true when absent).
func truthy(raw json.RawMessage) bool {
	if raw == nil {
		return true
	}
	v, _ := jsonx.Parse(raw)
	return jsonx.Truthy(v)
}

var integerText = regexp.MustCompile(`^[+-]?[0-9]+(_[0-9]+)*$`)

// intOf is an integer setting (any size): a number (truncated), a boolean
// or a decimal string; anything else is the default.
func intOf(raw json.RawMessage, def int64) *big.Int {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var v any
	if raw == nil || decoder.Decode(&v) != nil {
		return big.NewInt(def)
	}
	switch x := v.(type) {
	case bool:
		if x {
			return big.NewInt(1)
		}
		return big.NewInt(0)
	case json.Number:
		if n, ok := new(big.Int).SetString(string(x), 10); ok {
			return n
		}
		if f, err := strconv.ParseFloat(string(x), 64); err == nil {
			n, _ := big.NewFloat(f).Int(nil)
			return n
		}
	case string:
		if text := strings.TrimSpace(x); integerText.MatchString(text) {
			n, _ := new(big.Int).SetString(strings.ReplaceAll(text, "_", ""), 10)
			return n
		}
	}
	return big.NewInt(def)
}

func saveSettings(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := context.WithoutCancel(c.Context())
	var data map[string]json.RawMessage
	if raw, err := c.Data(); err != nil || !c.IsJSON() || json.Unmarshal(raw, &data) != nil || data == nil {
		return nil, repo.NewValidationError("MosDNS 设置必须是 JSON 对象")
	}
	profileID, err := repo.ResolveProfileID(ctx, "")
	if err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	// The whole editor draft in one transaction.
	_, revision, err := r.UpdateProfileTransaction(ctx, profileID, func(_ context.Context, profile *jsonx.Object) (*jsonx.Object, error) {
		settings, ok := profile.Value("mosdns").(*jsonx.Object)
		if !ok || settings == nil {
			if profile.Has("mosdns") {
				return nil, errors.New("mosdns settings are not an object")
			}
			settings = jsonx.NewObject()
			profile.Set("mosdns", settings)
		}
		settings.Set("custom_config", fromBody(data, "custom_config", `""`))
		for _, s := range sections[:5] {
			for _, f := range s.fields {
				settings.Set(f.key, fromBody(data, f.key, f.def))
			}
		}
		normalizeCache(settings, data)
		settings.Delete("api_addr")
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	repo.ResetConfigContext(ctx)
	return httpx.JSON(jsonx.Obj("success", true, "publish_guard", jsonx.Obj("expected_profile_id", profileID,
		"expected_service_type", "mosdns", "expected_config_revision", jsonx.Int(revision)))), nil
}

func ruleProxy(c *httpx.Ctx) (*httpx.Response, error) {
	header, _ := c.Header("Authorization")
	result := mosdns.RuleProxy(c.Context(), mosdns.RuleProxyRequest{
		Auth: mosdns.AuthRequest{Internal: c.IsInternalCall(), Authorization: header, Token: c.ArgDefault("token", "")},
		URL:  c.ArgDefault("url", ""), Part: c.ArgDefault("part", ""), SourceID: c.ArgDefault("source_id", ""),
		HostURL: c.HostURL(),
	})
	response := httpx.Text(result.Status, result.Text)
	if result.JSON != nil {
		object := jsonx.NewObject()
		for key, v := range result.JSON {
			object.Set(key, v)
		}
		response = httpx.JSONStatus(result.Status, object)
	}
	if result.NoStore {
		response.NoStore()
	}
	if result.Stale {
		response.WithHeader("X-ConfigFlow-Rule-Cache", "stale")
	}
	return response, nil
}
