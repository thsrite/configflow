package config

import (
	"encoding/json"
	"regexp"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/testgolden"
)

var (
	goldenISO = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	goldenHex = regexp.MustCompile(`^[0-9a-f]{16,}$`)
)

func normalizeGoldenJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			switch k {
			case "created_at", "updated_at", "export_time", "timestamp":
				if s, ok := val.(string); ok && s != "" {
					out[k] = "<time>"
					continue
				}
			case "config_token", "rule_proxy_token", "jwt_secret_key":
				if s, ok := val.(string); ok && s != "" {
					out[k] = "<token>"
					continue
				}
			case "retired_rule_proxy_tokens":
				if list, ok := val.([]any); ok {
					masked := make([]any, len(list))
					for i, item := range list {
						if s, ok := item.(string); ok && s != "" {
							masked[i] = "<token>"
						} else {
							masked[i] = item
						}
					}
					out[k] = masked
					continue
				}
			}
			out[k] = normalizeGoldenJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeGoldenJSON(item)
		}
		return out
	case string:
		if goldenISO.MatchString(x) {
			return "<time>"
		}
		if goldenHex.MatchString(x) {
			return "<hex>"
		}
		return x
	default:
		return v
	}
}

func goldenResponse(t *testing.T, status int, body []byte) []byte {
	t.Helper()
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		parsed = string(body)
	}
	out, err := json.Marshal(map[string]any{
		"status": status,
		"body":   normalizeGoldenJSON(parsed),
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestConfigWriteRoutesGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})
	s := exportApp(t)

	// Capture a valid export payload before mutations for round-trip import testing.
	exported := s.Get("/api/config/export").WantStatus(200).Body

	steps := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"custom-mihomo-valid", "POST", "/api/custom-config/mihomo", `{"custom_config":"mode: rule\ndns:\n  enable: true\n"}`},
		{"custom-mihomo-invalid-yaml", "POST", "/api/custom-config/mihomo", `{"custom_config":"dns: ["}`},
		{"custom-mihomo-non-mapping", "POST", "/api/custom-config/mihomo", `{"custom_config":"- item1\n- item2\n"}`},
		{"custom-mihomo-clear", "POST", "/api/custom-config/mihomo", `{"custom_config":""}`},
		{"custom-mihomo-non-object", "POST", "/api/custom-config/mihomo", `[1,2]`},
		{"custom-mihomo-malformed-json", "POST", "/api/custom-config/mihomo", `{`},
		{"custom-surge-valid", "POST", "/api/custom-config/surge", `{"custom_config":"[General]\nloglevel = notify\n"}`},
		{"custom-surge-non-string", "POST", "/api/custom-config/surge", `{"custom_config":123}`},
		{"custom-loon-valid", "POST", "/api/custom-config/loon", `{"custom_config":"[General]\n"}`},
		{"import-malformed-json", "POST", "/api/config/import", `{`},
		{"import-non-object", "POST", "/api/config/import", `[]`},
		{"import-empty-object", "POST", "/api/config/import", `{}`},
		{"import-valid-export", "POST", "/api/config/import", string(exported)},
		{"reset-default", "POST", "/api/config/reset", `{}`},
		{"export-after-reset", "GET", "/api/config/export", ``},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			var opts []httpxtest.Option
			if step.body != "" {
				opts = append(opts, httpxtest.Body([]byte(step.body), "application/json"))
			}
			resp := s.Do(step.method, step.path, opts...)
			key, err := json.Marshal(map[string]string{
				"case":   step.name,
				"method": step.method,
				"path":   step.path,
			})
			if err != nil {
				t.Fatal(err)
			}
			ref.Assert(t, key, goldenResponse(t, resp.Status, resp.Body))
		})
	}
}
