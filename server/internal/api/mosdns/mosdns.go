// Package mosdns holds the routes of backend/routes/mosdns.py (blueprint
// "mosdns", prefix /api/mosdns) and the profile wrapper
// /api/profiles/<profile_id>/mosdns/rule-proxy of routes/profiles.py. The
// rule proxy and the cache normalization live in internal/mosdns.
package mosdns

import (
	"context"
	"fmt"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/repo"
)

// Register adds the MosDNS routes.
func Register(r *httpx.Router) {
	r.POST("/api/mosdns/settings", "mosdns.save_mosdns_settings", auth.Require(saveSettings))
	r.Handle([]string{"GET", "POST"}, "/api/mosdns/rulesets", "mosdns.handle_mosdns_rulesets", auth.Require(rulesets))
	r.Handle([]string{"GET", "POST"}, "/api/mosdns/custom-matches", "mosdns.handle_mosdns_custom_matches", auth.Require(customMatches))
	r.Handle([]string{"GET", "POST"}, "/api/mosdns/dns-servers", "mosdns.handle_mosdns_dns_servers", auth.Require(dnsServers))
	r.Handle([]string{"GET", "POST"}, "/api/mosdns/log-settings", "mosdns.handle_mosdns_log_settings", auth.Require(logSettings))
	r.Handle([]string{"GET", "POST"}, "/api/mosdns/api-settings", "mosdns.handle_mosdns_api_settings", auth.Require(apiSettings))
	r.Handle([]string{"GET", "POST"}, "/api/mosdns/cache-settings", "mosdns.handle_mosdns_cache_settings", auth.Require(cacheSettings))
	// Public: the rule proxy checks its own token or JWT.
	r.GET("/api/mosdns/rule-proxy", "mosdns.mosdns_rule_proxy", ruleProxy)
	r.GET("/api/profiles/<profile_id>/mosdns/rule-proxy", "profiles.profile_mosdns_rule_proxy", ruleProxy)
}

// failure is `jsonify({'success': False, 'message': str(e)}), 500`.
func failure(err error) *httpx.Response {
	return httpx.Message(500, false, err.Error())
}

// errNoResponse is Flask's TypeError for a view that returned None (a HEAD
// request reaches neither the GET nor the POST branch).
func errNoResponse(endpoint string) error {
	return fmt.Errorf("The view function for %q did not return a valid response. The function either returned None or ended without a return statement.", endpoint)
}

func saveSettings(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := context.WithoutCancel(c.Context())
	data, ok := c.JSONSilent().(*jsonx.Object)
	if !ok {
		return nil, repo.NewValidationError("MosDNS 设置必须是 JSON 对象")
	}
	settings := jsonx.NewObject()
	defaults := []any{
		"custom_config", "", "direct_rulesets", []any{}, "proxy_rulesets", []any{},
		"direct_rules", []any{}, "proxy_rules", []any{}, "custom_matches", []any{},
		"custom_match_position", "tail", "local_dns", "", "remote_dns", "",
		"fallback_dns", "", "default_forward", "forward_remote", "custom_hosts", "",
		"log_enabled", true, "log_level", "info", "log_file", "",
		"api_enabled", true, "api_address", ":8080",
	}
	for i := 0; i < len(defaults); i += 2 {
		key := defaults[i].(string)
		settings.Set(key, data.GetOr(key, defaults[i+1]))
	}
	cache, err := mosdns.CacheSettings(data)
	if err != nil {
		return nil, err
	}
	settings.Update(cache)
	profileID, err := repo.ResolveProfileID(ctx, "")
	if err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	_, revision, err := r.UpdateProfileTransaction(ctx, profileID, func(_ context.Context, profile *jsonx.Object) (*jsonx.Object, error) {
		current, present := profile.Get("mosdns")
		if !present {
			current = jsonx.NewObject()
			profile.Set("mosdns", current)
		}
		object, ok := current.(*jsonx.Object)
		if !ok || object == nil {
			return nil, fmt.Errorf("'%s' object has no attribute 'update'", httpx.PyTypeName(current))
		}
		object.Update(settings)
		object.Delete("api_addr")
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	repo.ResetConfigContext(ctx)
	return httpx.JSON(jsonx.Obj("success", true, "publish_guard", jsonx.Obj(
		"expected_profile_id", profileID, "expected_service_type", "mosdns",
		"expected_config_revision", jsonx.Int(revision)))), nil
}

var baseDefaults = []any{
	"direct_rulesets", []any{}, "proxy_rulesets", []any{}, "direct_rules", []any{}, "proxy_rules", []any{},
	"local_dns", "", "remote_dns", "", "fallback_dns", "", "default_forward", "forward_remote",
	"custom_hosts", "", "custom_config", "",
}

// mosdnsSection is the shared preamble: get_config(), the mosdns section
// created with the route's defaults when missing.
func mosdnsSection(ctx context.Context, extra ...any) (*jsonx.Object, *jsonx.Object, error) {
	config, err := repo.GetConfig(ctx, "")
	if err != nil {
		return nil, nil, err
	}
	if !config.Has("mosdns") {
		section := jsonx.NewObject()
		defaults := append(append([]any{}, baseDefaults...), extra...)
		for i := 0; i < len(defaults); i += 2 {
			section.Set(defaults[i].(string), jsonx.DeepCopy(defaults[i+1]))
		}
		config.Set("mosdns", section)
	}
	section, ok := config.Value("mosdns").(*jsonx.Object)
	if !ok || section == nil {
		return nil, nil, fmt.Errorf("'%s' object has no attribute 'get'", httpx.PyTypeName(config.Value("mosdns")))
	}
	return config, section, nil
}

// postData is `data = request.json`, failing like Python when the body is
// not JSON or not an object (the caller turns it into the 500 JSON body).
func postData(c *httpx.Ctx, allowFalsy bool) (*jsonx.Object, error) {
	value, err := c.JSON()
	if err != nil {
		return nil, err
	}
	if allowFalsy && !jsonx.Truthy(value) {
		return jsonx.NewObject(), nil
	}
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return nil, fmt.Errorf("'%s' object has no attribute 'get'", httpx.PyTypeName(value))
	}
	return object, nil
}

// simpleRoute is the GET/POST pattern shared by the section routes: GET
// returns fields with defaults, POST stores them and saves the profile.
type field struct {
	key, arg string
	def      any
}

func simpleRoute(endpoint string, extraDefaults []any, fields []field, post func(*jsonx.Object, *jsonx.Object)) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		ctx := context.WithoutCancel(c.Context())
		config, section, err := mosdnsSection(ctx, extraDefaults...)
		if err != nil {
			return nil, err
		}
		switch c.Request.Method {
		case "GET":
			out := jsonx.NewObject()
			for _, f := range fields {
				out.Set(f.arg, section.GetOr(f.key, jsonx.DeepCopy(f.def)))
			}
			return httpx.JSON(out), nil
		case "POST":
			data, err := postData(c, false)
			if err != nil {
				return failure(err), nil
			}
			for _, f := range fields {
				section.Set(f.key, data.GetOr(f.arg, jsonx.DeepCopy(f.def)))
			}
			if post != nil {
				post(section, data)
			}
			if err := repo.SaveConfig(ctx, config, ""); err != nil {
				return failure(err), nil
			}
			return httpx.JSON(jsonx.Obj("success", true)), nil
		}
		return nil, errNoResponse(endpoint)
	}
}

var rulesets = simpleRoute("mosdns.handle_mosdns_rulesets", nil, []field{
	{"direct_rulesets", "direct_rulesets", []any{}}, {"proxy_rulesets", "proxy_rulesets", []any{}},
	{"direct_rules", "direct_rules", []any{}}, {"proxy_rules", "proxy_rules", []any{}},
}, nil)

var customMatches = simpleRoute("mosdns.handle_mosdns_custom_matches",
	[]any{"custom_matches", []any{}, "custom_match_position", "tail"}, []field{
		{"custom_matches", "custom_matches", []any{}}, {"custom_match_position", "position", "tail"},
	}, nil)

var dnsServers = simpleRoute("mosdns.handle_mosdns_dns_servers", nil, []field{
	{"local_dns", "local_dns", ""}, {"remote_dns", "remote_dns", ""}, {"fallback_dns", "fallback_dns", ""},
	{"default_forward", "default_forward", "forward_remote"}, {"custom_hosts", "custom_hosts", ""},
}, nil)

var logSettings = simpleRoute("mosdns.handle_mosdns_log_settings",
	[]any{"log_enabled", true, "log_level", "info", "log_file", ""}, []field{
		{"log_enabled", "log_enabled", true}, {"log_level", "log_level", "info"}, {"log_file", "log_file", ""},
	}, nil)

func apiSettings(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := context.WithoutCancel(c.Context())
	config, section, err := mosdnsSection(ctx, "api_enabled", true, "api_address", ":8080")
	if err != nil {
		return nil, err
	}
	switch c.Request.Method {
	case "GET":
		// api_addr: the legacy field name.
		return httpx.JSON(jsonx.Obj("api_enabled", section.GetOr("api_enabled", true),
			"api_address", section.GetOr("api_address", section.GetOr("api_addr", ":8080")))), nil
	case "POST":
		data, err := postData(c, false)
		if err != nil {
			return failure(err), nil
		}
		section.Set("api_enabled", data.GetOr("api_enabled", true))
		section.Set("api_address", data.GetOr("api_address", ":8080"))
		section.Delete("api_addr")
		if err := repo.SaveConfig(ctx, config, ""); err != nil {
			return failure(err), nil
		}
		return httpx.JSON(jsonx.Obj("success", true)), nil
	}
	return nil, errNoResponse("mosdns.handle_mosdns_api_settings")
}

var cacheDefaults = []any{
	"cache_enabled", true, "cache_size", jsonx.Int(10240), "cache_lazy_ttl", jsonx.Int(21600),
	"cache_dump_enabled", true, "cache_dump_file", "./cache.dump", "cache_dump_interval", jsonx.Int(300),
}

func cacheSettings(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := context.WithoutCancel(c.Context())
	config, section, err := mosdnsSection(ctx, cacheDefaults...)
	if err != nil {
		return nil, err
	}
	// Fields missing in older configurations.
	for i := 0; i < len(cacheDefaults); i += 2 {
		key := cacheDefaults[i].(string)
		if !section.Has(key) {
			section.Set(key, cacheDefaults[i+1])
		}
	}
	switch c.Request.Method {
	case "GET":
		out := jsonx.NewObject()
		for _, key := range []string{"cache_enabled", "cache_size", "cache_lazy_ttl", "cache_dump_enabled",
			"cache_dump_file", "cache_dump_interval"} {
			out.Set(key, section.Value(key))
		}
		return httpx.JSON(out), nil
	case "POST":
		data, err := postData(c, true)
		if err != nil {
			return failure(err), nil
		}
		settings, err := mosdns.CacheSettings(data)
		if err != nil {
			return failure(err), nil
		}
		section.Update(settings)
		guard, err := repo.SaveConfigWithPublishGuard(ctx, config, "mosdns", "")
		if err != nil {
			return failure(err), nil
		}
		return httpx.JSON(jsonx.Obj("success", true, "publish_guard", guard)), nil
	}
	return nil, errNoResponse("mosdns.handle_mosdns_cache_settings")
}

func ruleProxy(c *httpx.Ctx) (*httpx.Response, error) {
	authorization, present := c.Header("Authorization")
	request := mosdns.RuleProxyRequest{
		Auth: mosdns.AuthRequest{Internal: c.IsInternalCall(), Authorization: authorization,
			AuthorizationPresent: present, Token: c.ArgDefault("token", "")},
		URL:      c.ArgDefault("url", ""),
		Part:     c.ArgDefault("part", ""),
		SourceID: c.ArgDefault("source_id", ""),
		HostURL:  c.HostURL(),
	}
	result := mosdns.RuleProxy(c.Context(), request)
	var response *httpx.Response
	if result.JSON != nil {
		response = httpx.JSONStatus(result.Status, result.JSON)
	} else {
		response = httpx.Text(result.Status, result.Text)
	}
	if result.NoStore {
		response.NoStore()
	}
	if result.Stale {
		response.WithHeader("X-ConfigFlow-Rule-Cache", "stale")
	}
	return response, nil
}
