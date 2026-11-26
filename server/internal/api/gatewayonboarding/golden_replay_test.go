package gatewayonboarding_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/server/servertest"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
)

const onboardingValidForm = `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query"}`

func newOnboardingServer(t *testing.T) *httpxtest.Server {
	t.Helper()
	s, _ := servertest.New(t, httpxtest.Options{Auth: true})
	ctx := s.Context()
	if err := s.Repo.UpdateShared(ctx, func(_ context.Context, c *model.Shared) error {
		return c.SetJSON("nodes", json.RawMessage(`[{"id":"n","name":"Example","type":"ss","server":"192.0.2.2","port":443,"cipher":"aes-128-gcm","password":"synthetic-node-secret"}]`))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Repo.UpdateProfile(ctx, "default", func(_ context.Context, c *model.Profile) error {
		if err := c.SetJSON("proxy_groups", json.RawMessage(`[{"id":"g","name":"Proxy","type":"select","manual_nodes":["n"]}]`)); err != nil {
			return err
		}
		if err := c.SetJSON("rule_configs", json.RawMessage(`[{"id":"match","itemType":"rule","rule_type":"MATCH","policy":"Proxy"}]`)); err != nil {
			return err
		}
		return c.SetJSON("mihomo", json.RawMessage(`{"custom_config":"mixed-port: 7890\n"}`))
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func onboardingBodyOpt(text string) httpxtest.Option {
	return func(r *http.Request) {
		r.Body = io.NopCloser(strings.NewReader(text))
		r.ContentLength = int64(len(text))
		r.Header.Set("Content-Type", "application/json")
	}
}

func normalizeOnboardingGolden(t *testing.T, v any) any {
	t.Helper()
	var walk func(any, []string) any
	timestamp := func(v any) any {
		s, ok := v.(string)
		if !ok {
			return v
		}
		if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return "<time>"
		}
		return v
	}
	walk = func(v any, path []string) any {
		switch x := v.(type) {
		case map[string]any:
			out := make(map[string]any, len(x))
			for key, item := range x {
				if key == "created_at" || key == "updated_at" {
					out[key] = timestamp(item)
					continue
				}
				if key == "config_token" || key == "rule_proxy_token" || key == "jwt_secret_key" {
					if s, ok := item.(string); ok && s != "" {
						out[key] = "<token>"
						continue
					}
				}
				if text, ok := item.(string); ok && (key == "custom_config" || key == "native_config") && text != "" {
					if node, err := yamlx.Parse([]byte(text)); err == nil {
						if data, err := yamlx.ToJSON(node); err == nil {
							var parsed any
							if json.Unmarshal(data, &parsed) == nil {
								out[key] = walk(parsed, append(append([]string{}, path...), key))
								continue
							}
						}
					}
				}
				out[key] = walk(item, append(append([]string{}, path...), key))
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, item := range x {
				out[i] = walk(item, append(append([]string{}, path...), "[]"))
			}
			return out
		}
		return v
	}
	return walk(v, nil)
}

func TestGatewayOnboardingGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})
	s := newOnboardingServer(t)
	admin := s.Login()
	path := "/api/profiles/default/gateway-onboarding"

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"get-initial", "GET", path, ""},
		{"head-readonly", "HEAD", path, onboardingValidForm},
		{"preview-initial", "POST", path + "/preview", onboardingValidForm},
		{"save-initial", "POST", path, onboardingValidForm},
		{"preview-repeat", "POST", path + "/preview", onboardingValidForm},
		{"save-repeat", "POST", path, onboardingValidForm},
		{"save-bad-body", "POST", path, `{"bad":"body"}`},
		{"preview-malformed", "POST", path + "/preview", "{"},
		{"save-null", "POST", path, "null"},
		{"preview-array", "POST", path + "/preview", "[]"},
		{"preview-udp-dns", "POST", path + "/preview", `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"udp://192.0.2.53"}`},
		{"save-oversized", "POST", path, strings.Repeat("x", 262145)},
		{"save-stale-revision", "POST", path, `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query","expected_config_revision":999999}`},
		{"save-string-revision", "POST", path, `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query","expected_config_revision":"7"}`},
		{"save-missing-rule", "POST", path, `{"rule_set_ids":[],"direct_rule_set_ids":["missing-rule"],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query"}`},
		{"get-missing-profile", "GET", "/api/profiles/missing-profile/gateway-onboarding", ""},
		{"save-missing-profile", "POST", "/api/profiles/missing-profile/gateway-onboarding", onboardingValidForm},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.Do(tc.method, tc.path, admin, onboardingBodyOpt(tc.body))
			var parsed any
			if tc.method == "HEAD" {
				parsed = ""
			} else if err := json.Unmarshal(resp.Body, &parsed); err != nil {
				parsed = string(resp.Body)
			}
			docRaw, err := s.Repo.DiagnosticExportJSON(s.Context())
			if err != nil {
				t.Fatal(err)
			}
			var doc any
			if err := json.Unmarshal(docRaw, &doc); err != nil {
				t.Fatal(err)
			}
			key, _ := json.Marshal(map[string]string{"case": tc.name, "method": tc.method, "path": tc.path})
			actual, _ := json.Marshal(normalizeOnboardingGolden(t, map[string]any{
				"status":        resp.Status,
				"cache_control": resp.Header.Get("Cache-Control"),
				"content_type":  resp.Header.Get("Content-Type"),
				"body":          parsed,
				"document":      doc,
			}))
			ref.Assert(t, key, actual)
		})
	}
}
