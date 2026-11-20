package difftest

import (
	"context"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestReviewBatchNullValues(t *testing.T) {
	for _, request := range []string{
		`{"rule_type":"MATCH","domains":[null],"policy":"DIRECT"}`,
		`{"rule_type":"DOMAIN","domains":["a.example",null],"policy":"DIRECT"}`,
	} {
		t.Run(request, func(t *testing.T) {
			w := newWorld(t, seed(t))
			w.call("POST", "/api/rules/batch?profile=other", request, true)
		})
	}
}

func TestReviewAuthenticationAndProviderTokens(t *testing.T) {
	w := newWorld(t, func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		err := r.UpdateSystem(ctx, func(_ context.Context, p *model.System) error {
			p.SystemConfig.ConfigToken = "synthetic-config-token"
			p.SystemConfig.RuleProxyToken = "synthetic-rule-proxy-token"
			p.SystemConfig.RetiredRuleProxyTokens = []string{"synthetic-retired-token"}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	// These two routes intentionally remain anonymous, including when an
	// invalid or retired capability is supplied. Compare the real auth gates.
	for _, path := range []string{"/api/rules/local/Ads", "/api/profiles/other/rules/local/Ads"} {
		for _, suffix := range []string{"", "?token=synthetic-config-token", "?token=wrong", "?token=synthetic-rule-proxy-token", "?token=synthetic-retired-token", "?token="} {
			for _, headers := range []map[string]string{{}, {"Authorization": "Bearer invalid-jwt"}, {"Authorization": "Bearer synthetic-rule-proxy-token"}, {"Authorization": "Bearer synthetic-retired-token"}} {
				if reply := w.callHeaders("GET", path+suffix, "", headers); reply.Status != 200 {
					t.Fatalf("provider %s: %d", path+suffix, reply.Status)
				}
			}
		}
	}
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/rules"}, {"POST", "/api/rules"},
		{"GET", "/api/profiles/other/rules"}, {"POST", "/api/profiles/other/rules"},
		{"GET", "/api/rule-sets"}, {"POST", "/api/rule-sets"},
		{"PUT", "/api/rules/inline"}, {"DELETE", "/api/rules/inline"},
		{"PUT", "/api/rule-sets/set"}, {"DELETE", "/api/rule-sets/set"},
		{"POST", "/api/rules/compose"}, {"POST", "/api/rules/group-name"},
		{"PUT", "/api/rules/policy"}, {"POST", "/api/rules/batch"}, {"POST", "/api/rules/reorder"},
		{"GET", "/api/rules/dns"}, {"PUT", "/api/rules/dns"},
		{"POST", "/api/rules/match-test"}, {"POST", "/api/rules/find-duplicates"},
	} {
		for _, suffix := range []string{"", "?token=synthetic-config-token", "?token=wrong", "?token=synthetic-rule-proxy-token", "?token=synthetic-retired-token"} {
			if reply := w.call(route.method, route.path+suffix, `{}`, false); reply.Status != 401 {
				t.Fatalf("unauthenticated %s %s: %d", route.method, route.path+suffix, reply.Status)
			}
		}
	}
}
