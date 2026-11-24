package oxidns_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server/servertest"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
)

func normalizeOxiGolden(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			if k == "updated_at" {
				if text, ok := item.(string); ok {
					if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
						out[k] = "<time>"
						continue
					}
				}
			}
			out[k] = normalizeOxiGolden(item, k)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeOxiGolden(item, key)
		}
		return out
	case string:
		if key == "native_config" || key == "config" || key == "custom_config" || key == "template_base_config" || key == "legacy_native_config" {
			if node, err := yamlx.Parse([]byte(x)); err == nil {
				if data, err := yamlx.ToJSON(node); err == nil {
					var parsed any
					if json.Unmarshal(data, &parsed) == nil {
						return parsed
					}
				}
			}
		}
	}
	return v
}

func oxiHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"Content-Type", "Cache-Control", "ETag", "Allow"} {
		out[key] = h.Get(key)
	}
	if out["Allow"] != "" {
		items := strings.Split(out["Allow"], ",")
		for i := range items {
			items[i] = strings.TrimSpace(items[i])
		}
		sort.Strings(items)
		out["Allow"] = strings.Join(items, ", ")
	}
	return out
}

func TestOxiDNSAPIGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	srv, _ := servertest.New(t, httpxtest.Options{
		Auth: true,
	})
	ctx := srv.Context()
	if err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.ConfigToken = "c29-config-secret"
		s.SystemConfig.RuleProxyToken = "c29-rule-secret"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Repo.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		return shared.SetJSON("rule_library", json.RawMessage(`[{"id":"library","name":"Synthetic","source_type":"content","content":"DOMAIN-SUFFIX,example.com","behavior":"domain","format":"text"}]`))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"rs","itemType":"ruleset","library_rule_id":"library","policy":"DIRECT","enabled":true}]`))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Repo.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "office", Name: "Office"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Repo.UpdateProfile(ctx, "office", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"rs","itemType":"ruleset","library_rule_id":"library","policy":"DIRECT","enabled":true}]`))
	}); err != nil {
		t.Fatal(err)
	}

	admin := srv.Login()
	draft := func(text string) map[string]any {
		return map[string]any{"config_mode": "native", "native_config": text, "additional_files": []any{}}
	}

	record := func(label, method, path, body string, useAuth bool, hdrs map[string]string) (http.Header, any) {
		t.Helper()
		var opts []httpxtest.Option
		if useAuth {
			opts = append(opts, admin)
		}
		ct := "application/json"
		if v, ok := hdrs["Content-Type"]; ok {
			ct = v
		}
		for k, v := range hdrs {
			if strings.EqualFold(k, "Content-Type") {
				continue
			}
			opts = append(opts, httpxtest.Header(k, v))
		}
		if body != "" {
			opts = append(opts, httpxtest.Body([]byte(body), ct))
		}
		resp := srv.Do(method, path, opts...)
		var parsed any = string(resp.Body)
		if method == "HEAD" {
			parsed = ""
		} else if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			_ = json.Unmarshal(resp.Body, &parsed)
		}
		key, _ := json.Marshal(map[string]string{"case": label})
		actual, _ := json.Marshal(normalizeOxiGolden(map[string]any{
			"status":  resp.Status,
			"headers": oxiHeaders(resp.Header),
			"body":    parsed,
		}, ""))
		ref.Assert(t, key, actual)
		return resp.Header, parsed
	}

	mustJSON := func(v any) string { b, _ := json.Marshal(v); return string(b) }

	for _, prefix := range []string{"/api/oxidns", "/api/profiles/office/oxidns"} {
		tag := "global"
		if strings.Contains(prefix, "/office/") {
			tag = "office"
		}
		record(tag+"-get-settings", "GET", prefix+"/settings", "", true, nil)
		record(tag+"-head-settings", "HEAD", prefix+"/settings", `{"worker_threads":9}`, true, nil)
		saved, _ := record(tag+"-post-settings", "POST", prefix+"/settings", mustJSON(draft("# retained draft\nruntime: {worker_threads: 0}\nplugins: []\n")), true, nil)

		for _, action := range []string{"render", "validate", "editor", "template-preview", "common-settings", "common-settings/apply", "native-settings/apply"} {
			settings := draft("runtime: {worker_threads: 4}\nplugins: []\nfuture: {empty: [], nil_value: null, flag: false}\n")
			val := map[string]any{
				"settings":          settings,
				"base_url":          "http://config.example",
				"template_settings": map[string]any{"local_dns": "192.0.2.53", "remote_dns": "192.0.2.54"},
			}
			if action == "native-settings/apply" {
				val["native_config"] = "runtime: {worker_threads: 6}\nplugins: []\nfuture: {empty: [], nil_value: null, flag: false}\n"
			}
			if action == "common-settings/apply" {
				val["changes"] = map[string]any{"worker_threads": 6}
			}
			record(tag+"-"+action+"-ok", "POST", prefix+"/"+action, mustJSON(val), true, nil)
			record(tag+"-"+action+"-empty", "POST", prefix+"/"+action, `{}`, true, nil)
			record(tag+"-"+action+"-bad-baseurl", "POST", prefix+"/"+action, mustJSON(map[string]any{"settings": settings, "base_url": false}), true, nil)
			record(tag+"-"+action+"-malformed", "POST", prefix+"/"+action, `{"settings":`, true, nil)
			record(tag+"-"+action+"-unauth", "POST", prefix+"/"+action, `{}`, false, nil)
		}

		etag := saved.Get("ETag")
		record(tag+"-ifmatch-stale", "POST", prefix+"/settings", `{}`, true, map[string]string{"If-Match": `"stale"`})
		record(tag+"-ifmatch-weak", "POST", prefix+"/settings", `{}`, true, map[string]string{"If-Match": "W/" + etag})
		record(tag+"-ifmatch-star", "POST", prefix+"/settings", `{}`, true, map[string]string{"If-Match": "*"})
		for i, bad := range []string{`null`, `[]`, `true`, `"text"`, `{`} {
			record(tag+"-bad-body-"+string(rune('a'+i)), "POST", prefix+"/settings", bad, true, nil)
		}
		record(tag+"-text-plain", "POST", prefix+"/settings", `{}`, true, map[string]string{"Content-Type": "text/plain"})
		record(tag+"-unauth-post", "POST", prefix+"/settings", `{}`, false, nil)
		record(tag+"-unauth-get", "GET", prefix+"/settings", "", false, nil)
		record(tag+"-options", "OPTIONS", prefix+"/settings", "", true, nil)
	}

	record("missing-profile-get", "GET", "/api/profiles/missing/oxidns/settings", "", true, nil)
	record("missing-profile-post", "POST", "/api/profiles/missing/oxidns/settings", `{}`, true, nil)

	// Editor workflow round-trip.
	template := map[string]any{
		"direct_rulesets": []string{"rs"},
		"local_dns":       "192.0.2.53",
		"remote_dns":      "192.0.2.54",
		"fallback_dns":    "192.0.2.55",
		"api":             map[string]any{"enabled": true, "username": "editor", "password": "c29-rule-secret"},
		"rule_update":     map[string]any{"enabled": true},
	}
	_, prepared := record("workflow-editor", "POST", "/api/profiles/office/oxidns/editor",
		mustJSON(map[string]any{"settings": template, "base_url": "http://config.example"}), true, nil)
	settings := prepared.(map[string]any)["settings"]
	record("workflow-common-read", "POST", "/api/profiles/office/oxidns/common-settings",
		mustJSON(map[string]any{"settings": settings, "base_url": "http://config.example"}), true, nil)
	_, patched := record("workflow-common-apply", "POST", "/api/profiles/office/oxidns/common-settings/apply",
		mustJSON(map[string]any{"settings": settings, "changes": map[string]any{"worker_threads": 6}, "base_url": "http://config.example"}), true, nil)
	settings = patched.(map[string]any)["settings"]
	record("workflow-render", "POST", "/api/profiles/office/oxidns/render",
		mustJSON(map[string]any{"settings": settings, "base_url": "http://config.example"}), true, nil)
}
