package aggregations_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	generation "configflow/internal/aggregations"
	"configflow/internal/api/aggregations"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/substore"
	"configflow/internal/testgolden"
	"configflow/internal/testjson"
)

type aggFakeHTTP struct {
	sources map[string]string
	offline bool
}

func (f aggFakeHTTP) Do(_ context.Context, req *substore.Request) (*substore.Response, error) {
	text := "proxies: []"
	source := false
	if strings.HasPrefix(req.URL, "https://feed.example/") {
		id := strings.TrimPrefix(req.URL, "https://feed.example/")
		text = f.sources[id]
		source = true
	}
	if f.offline && (source || strings.Contains(req.URL, "/download/_cf_tmp_s")) {
		return nil, fmt.Errorf("synthetic offline")
	}
	return &substore.Response{
		StatusCode: 200,
		URL:        req.URL,
		Header:     http.Header{"Content-Type": []string{"text/yaml"}, "X-Powered-By": []string{"sub-store"}},
		Body:       []byte(text),
	}, nil
}

type aggFixedClock struct{}

func (aggFixedClock) Now() time.Time      { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
func (aggFixedClock) Monotonic() float64  { return 0 }
func (aggFixedClock) Sleep(time.Duration) {}

var (
	aggGenID = regexp.MustCompile(`^agg_[0-9a-f]{8,}$`)
	aggISO   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
)

func normalizeAggGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if k == "created_at" || k == "updated_at" || k == "last_updated" {
				if s, ok := val.(string); ok && s != "" {
					out[k] = "<time>"
					continue
				}
			}
			out[k] = normalizeAggGolden(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeAggGolden(item)
		}
		return out
	case string:
		if aggGenID.MatchString(x) {
			return "<agg_id>"
		}
		if aggISO.MatchString(x) {
			return "<time>"
		}
		return x
	}
	return v
}

func newAggWorld(t *testing.T, used bool, pattern string, sources map[string]string) (*httpxtest.Server, *aggFakeHTTP) {
	t.Helper()
	srv := httpxtest.New(t, httpxtest.Options{Auth: true})
	ctx := srv.Context()
	httpStub := &aggFakeHTTP{sources: sources}
	t.Setenv("SUB_STORE_BUILTIN_DIR", "/nonexistent")
	t.Setenv("SUB_STORE_URL", "http://127.0.0.1:9")
	env := substore.NewEnvFor(srv.Repo)
	env.HTTP = httpStub
	env.Clock = aggFixedClock{}
	sub := substore.NewService(env)
	refreshEnv := refresh.DefaultRefreshEnvFor(srv.Repo, sub)
	refreshEnv.HTTP = httpStub
	refreshEnv.Clock = aggFixedClock{}
	rf := refresh.New(refresh.Options{Repo: srv.Repo, SubStore: sub, Env: refreshEnv})
	pv := provider.New(provider.Options{Repo: srv.Repo, SubStore: sub, Refresh: rf, Locations: deploy.Locations{}})
	mihomoenv.OverrideForTest(t, srv.Repo, sub, pv)
	gen := generation.New(generation.ServiceOptions{Repo: srv.Repo, Provider: pv, SubStore: sub})
	if err := pv.SetAggregations(gen.RenderProvider); err != nil {
		t.Fatal(err)
	}
	aggregations.NewHandler(srv.Repo, gen, pv, sub).Register(srv.App.Router)
	if err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.ConfigToken = "c31-pull"
		s.SystemConfig.RuleProxyToken = "c31-internal"
		s.SystemConfig.RetiredRuleProxyTokens = []string{"c31-retired"}
		s.SystemConfig.ServerDomain = "http://127.0.0.1:5001"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	node := func(id string) map[string]any {
		return map[string]any{"id": id, "name": id, "type": "http", "server": "example.com", "port": 80, "enabled": true}
	}
	proposed := testjson.Decode[model.Shared](t, map[string]any{
		"subscriptions": []any{map[string]any{"id": "s", "name": "Feed", "url": "https://feed.example/s", "enabled": true}},
		"nodes":         []any{node("manual"), node("relay")},
		"subscription_aggregations": []any{
			map[string]any{"id": "a", "name": "Aggregate", "enabled": true, "subscriptions": []any{"s"}, "nodes": []any{"manual"}, "regex_filter": pattern},
			map[string]any{"id": "b", "name": "Second", "enabled": true, "subscriptions": []any{}, "nodes": []any{"relay"}},
			map[string]any{"id": "off", "name": "Disabled", "enabled": false, "subscriptions": []any{}, "nodes": []any{}},
		},
	})
	if _, err := srv.Repo.SaveSharedRecord(ctx, &proposed, nil); err != nil {
		t.Fatal(err)
	}
	if used {
		if _, err := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
			raw, _ := json.Marshal([]any{map[string]any{"id": "g", "name": "Entry", "type": "select", "aggregations": []any{"a"}}})
			return p.SetJSON("proxy_groups", raw)
		}); err != nil {
			t.Fatal(err)
		}
	}
	return srv, httpStub
}

func TestAggregationsGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})
	const feed = "proxies: [{name: Remote, type: http, server: remote.example, port: 8080}]"

	record := func(t *testing.T, srv *httpxtest.Server, admin httpxtest.Option, label, method, path, body string, useAuth bool) {
		t.Helper()
		var opts []httpxtest.Option
		if useAuth {
			opts = append(opts, admin)
		}
		if body != "" {
			opts = append(opts, httpxtest.Body([]byte(body), "application/json"))
		}
		resp := srv.Do(method, path, opts...)
		var parsed any
		if err := json.Unmarshal(resp.Body, &parsed); err != nil {
			parsed = string(resp.Body)
		}
		key, _ := json.Marshal(map[string]string{"case": label})
		val, _ := json.Marshal(normalizeAggGolden(map[string]any{
			"status": resp.Status,
			"body":   parsed,
		}))
		ref.Assert(t, key, val)
	}

	t.Run("catalog-and-reorder", func(t *testing.T) {
		srv, _ := newAggWorld(t, false, "", map[string]string{"s": feed})
		admin := srv.Login()
		for i, p := range []string{"/api/aggregations", "/api/aggregations/a", "/api/aggregations/missing", "/api/aggregations/a/count", "/api/aggregations/missing/count", "/api/aggregations/a/health-impact", "/api/aggregations/missing/health-impact"} {
			record(t, srv, admin, fmt.Sprintf("cat-get-%d", i), "GET", p, "", true)
		}
		for i, b := range []string{`null`, `[]`, `{}`, `{"name":""}`, `{"name":42}`, `{"name":"Bad","subscriptions":null}`, `{"name":"Bad","nodes":["missing"]}`} {
			record(t, srv, admin, fmt.Sprintf("cat-post-bad-%d", i), "POST", "/api/aggregations", b, true)
			record(t, srv, admin, fmt.Sprintf("cat-put-bad-%d", i), "PUT", "/api/aggregations/a", b, true)
		}
		record(t, srv, admin, "cat-put-missing", "PUT", "/api/aggregations/missing", `{}`, true)
		record(t, srv, admin, "cat-put-malformed", "PUT", "/api/aggregations/a", `{`, true)
		record(t, srv, admin, "cat-put-replace", "PUT", "/api/aggregations/a",
			`{"id":"forged","name":"Aggregate","enabled":true,"subscriptions":["s"],"nodes":["manual"],"node_count":99}`, true)
		for i, b := range []string{`{}`, `{"ids":["missing"]}`, `{"ids":["a","a"]}`, `null`, `[]`, `{"ids":["b","a"]}`, `{"ids":["a","b","off"]}`} {
			record(t, srv, admin, fmt.Sprintf("cat-reorder-%d", i), "POST", "/api/aggregations/reorder", b, true)
		}
		record(t, srv, admin, "cat-create", "POST", "/api/aggregations", `{"id":"forged","name":"Created","nodes":[],"subscriptions":[]}`, true)
		record(t, srv, admin, "cat-list", "GET", "/api/aggregations", "", true)
		record(t, srv, admin, "cat-del-missing", "DELETE", "/api/aggregations/missing", "", true)
		record(t, srv, admin, "cat-del-a", "DELETE", "/api/aggregations/a", "", true)
		record(t, srv, admin, "cat-unauth-list", "GET", "/api/aggregations", "", false)
	})

	t.Run("provider-and-preview", func(t *testing.T) {
		srv, httpStub := newAggWorld(t, true, "", map[string]string{"s": feed})
		admin := srv.Login()
		record(t, srv, admin, "prev-a", "GET", "/api/aggregations/a/preview", "", true)
		record(t, srv, admin, "prev-missing", "GET", "/api/aggregations/missing/preview", "", true)
		record(t, srv, admin, "prov-a", "GET", "/api/aggregations/a/provider", "", true)
		for i, q := range []string{"format=surge", "format=loon", "regex=Remote", "include=Remote", "exclude=Remote", "include=["} {
			record(t, srv, admin, fmt.Sprintf("prov-q-%d", i), "GET", "/api/aggregations/a/provider?"+q, "", true)
		}
		for _, id := range []string{"missing", "off", "a"} {
			for _, tok := range []string{"", "bad", "c31-retired", "c31-pull", "c31-internal"} {
				record(t, srv, admin, "prov-tok-"+id+"-"+tok, "GET", "/api/profiles/default/aggregations/"+id+"/provider?token="+tok, "", false)
			}
		}
		record(t, srv, admin, "prov-missing-profile", "GET", "/api/profiles/missing/aggregations/a/provider?token=c31-pull", "", false)
		record(t, srv, admin, "del-referenced", "DELETE", "/api/aggregations/a", "", true)
		httpStub.offline = true
		record(t, srv, admin, "prev-offline", "GET", "/api/aggregations/a/preview", "", true)
	})

	t.Run("invalid-regex", func(t *testing.T) {
		srv, _ := newAggWorld(t, true, "[", map[string]string{"s": feed})
		admin := srv.Login()
		record(t, srv, admin, "badre-prev", "GET", "/api/aggregations/a/preview", "", true)
		record(t, srv, admin, "badre-prov", "GET", "/api/profiles/default/aggregations/a/provider?token=c31-pull", "", false)
	})
}
