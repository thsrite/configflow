package difftest

import (
	"context"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func TestReviewDraftRevisionShape(t *testing.T) {
	for _, test := range []struct {
		value any
		valid bool
	}{
		{strings.Repeat("a", 64), true}, {strings.Repeat("0", 64), true},
		{strings.Repeat("A", 64), false}, {strings.Repeat("a", 63), false},
		{strings.Repeat("a", 65), false}, {strings.Repeat("a", 64) + "\n", false},
		{42, false}, {nil, false},
	} {
		if validDraftRevision(test.value) != test.valid {
			t.Errorf("revision shape %v: want %t", test.value, test.valid)
		}
	}
}

func TestReviewReorderMalformedHTTPParity(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, path := range []string{"/api/rule-library/reorder", "/api/proxy-groups/reorder"} {
		for _, body := range []string{`null`, `false`, `0`, `""`, `[]`, `[1]`, `"bad"`, `42`, `true`} {
			w.call("POST", path, body, true)
		}
	}
}

func TestReviewStrictAndSilentBinding(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/rule-library"}, {"PUT", "/api/rule-library/missing"},
		{"PUT", "/api/rule-library/missing/draft-content"}, {"POST", "/api/rule-library/check-content"},
		{"POST", "/api/rule-library/cache"}, {"POST", "/api/rule-library/test-single"},
		{"POST", "/api/rule-library/reorder"}, {"POST", "/api/rule-library/proxy-domains"},
		{"POST", "/api/proxy-groups"}, {"POST", "/api/profiles/other/proxy-groups"},
		{"PUT", "/api/proxy-groups/group"}, {"PUT", "/api/proxy-groups/batch-health-url"},
		{"POST", "/api/proxy-groups/preview-regex"}, {"POST", "/api/proxy-groups/reorder"},
	} {
		for _, contentType := range []string{"application/json", "text/plain", "application/problem+json"} {
			for _, body := range []string{`{`, `{}`} {
				w.callHeaders(route.method, route.path, body, body, map[string]string{"Authorization": "Bearer " + w.token, "Content-Type": contentType})
			}
		}
	}
}

func TestReviewAuthenticationAllowlist(t *testing.T) {
	w := newWorld(t, func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		_, err := r.UpdateSystemTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			system := p.GetObject("system_config")
			system.Set("config_token", "synthetic-config-token")
			system.Set("rule_proxy_token", "synthetic-rule-proxy-token")
			system.Set("retired_rule_proxy_tokens", []any{"synthetic-retired-token"})
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	w.call("POST", "/api/rule-library", `{"id":"manual","name":"Manual","source_type":"content","behavior":"domain","format":"text","content":"+.manual.example\n"}`, true)
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/proxy-groups"}, {"POST", "/api/proxy-groups"},
		{"GET", "/api/profiles/other/proxy-groups"}, {"POST", "/api/profiles/other/proxy-groups"},
		{"PUT", "/api/proxy-groups/group"}, {"DELETE", "/api/proxy-groups/group"},
		{"PUT", "/api/proxy-groups/batch-health-url"}, {"GET", "/api/proxy-groups/health-defaults"},
		{"POST", "/api/proxy-groups/preview-regex"}, {"POST", "/api/proxy-groups/reorder"},
		{"GET", "/api/rule-library"}, {"POST", "/api/rule-library"},
		{"PUT", "/api/rule-library/manual"}, {"DELETE", "/api/rule-library/manual"},
		{"GET", "/api/rule-library/manual/draft-content"}, {"PUT", "/api/rule-library/manual/draft-content"},
		{"POST", "/api/rule-library/cache"}, {"POST", "/api/rule-library/check-content"},
		{"GET", "/api/rule-library/proxy-domains"}, {"POST", "/api/rule-library/proxy-domains"},
		{"POST", "/api/rule-library/reorder"}, {"POST", "/api/rule-library/test"}, {"POST", "/api/rule-library/test-single"},
	} {
		for _, token := range []string{"", "synthetic-config-token", "synthetic-rule-proxy-token", "synthetic-retired-token", "wrong"} {
			path := route.path + "?token=" + token
			// A capability in either channel must not grant management rights.
			headers := map[string]string{}
			if token != "" {
				headers["Authorization"] = "Bearer " + token
			}
			if reply := w.callHeaders(route.method, path, `{}`, `{}`, headers); reply.Status != 401 {
				t.Fatalf("management %s %s: %d", route.method, route.path, reply.Status)
			}
		}
	}
	for _, path := range []string{"/api/rule-library/content/manual", "/api/profiles/other/rule-library/content/manual"} {
		for _, token := range []string{"", "synthetic-config-token", "synthetic-rule-proxy-token", "synthetic-retired-token", "wrong"} {
			for _, headers := range []map[string]string{{}, {"Authorization": "Bearer " + token}} {
				if reply := w.callHeaders("GET", path+"?token="+token, "", "", headers); reply.Status != 200 {
					t.Fatalf("public content %s: %d", path, reply.Status)
				}
			}
		}
	}
}
