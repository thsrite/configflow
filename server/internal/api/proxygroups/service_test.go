package proxygroups

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"configflow/internal/api/aggregations"
	apirules "configflow/internal/api/rules"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/substore"
)

func fixture(t *testing.T) *httpxtest.Server {
	t.Helper()
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(aggregations.Register, apirules.Register, Register)})
	_, err := s.Repo.UpdateSharedTransaction(s.Context(), func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.Set("subscriptions", []any{jsonx.Obj("id", "feed", "name", "Feed", "url", "https://source.example/feed", "regex_filter", "HK|JP|广告")})
		p.Set("nodes", []any{jsonx.Obj("id", "manual", "name", "HK manual", "type", "http", "server", "example.com", "port", 80)})
		p.Set("subscription_aggregations", []any{jsonx.Obj("id", "agg", "name", "Aggregate", "subscriptions", []any{"feed"}, "nodes", []any{"manual"})})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(s.Context(), "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.Set("proxy_groups", []any{jsonx.Obj("id", "auto", "name", "Auto", "type", "url-test", "manual_nodes", []any{"DIRECT"}, "url", "http://probe.example/old", "interval", 73, "future", jsonx.Obj("keep", true)), jsonx.Obj("id", "select", "name", "Select", "type", "select", "manual_nodes", []any{"DIRECT"})})
		p.Set("rule_configs", []any{jsonx.Obj("id", "rule", "itemType", "rule", "rule_type", "DOMAIN", "value", "example.com", "policy", "Auto")})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Repo.CreateProfile(s.Context(), jsonx.Obj("id", "other", "name", "Other"), "default"); err != nil {
		t.Fatal(err)
	}
	nodes := []any{}
	for _, name := range []string{"HK 01", "HK 02", "JP 01", "广告", "US hidden", "HK 01"} {
		nodes = append(nodes, jsonx.Obj("name", name, "type", "ss"))
	}
	if _, err = substore.SaveSubscriptionNodes(s.Context(), "feed", nodes, nil); err != nil {
		t.Fatal(err)
	}
	return s
}
func state(t *testing.T, s *httpxtest.Server) string {
	t.Helper()
	doc, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func TestBatchRollbackAndProfileIsolation(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	before, err := s.Repo.GetProfile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	s.Put("/api/proxy-groups/batch-health-url?profile=other", auth, httpxtest.JSON(map[string]any{"ids": []string{"auto"}, "url": "https://probe.example/new"})).WantStatus(200)
	after, err := s.Repo.GetProfile(s.Context(), "default")
	if err != nil || !jsonx.Equal(before, after) {
		t.Fatal("other-profile edit changed default", err)
	}
	for _, test := range []struct {
		body   any
		status int
	}{{nil, 400}, {[]any{}, 400}, {map[string]any{"ids": []any{"auto", nil}}, 400}, {map[string]any{"ids": []string{"auto", "auto"}}, 400}, {map[string]any{"ids": []string{"auto", "missing"}, "url": "https://probe.example/new"}, 404}, {map[string]any{"ids": []string{"auto", "select"}, "url": "https://probe.example/new"}, 400}, {map[string]any{"ids": []string{"auto"}, "url": "https://user:secret@probe.example/new"}, 400}} {
		t.Run("reject", func(t *testing.T) {
			before := state(t, s)
			s.Put("/api/proxy-groups/batch-health-url", auth, httpxtest.JSON(test.body)).WantStatus(test.status)
			if state(t, s) != before {
				t.Fatal("failed batch changed stored data")
			}
		})
	}
}
func TestGroupMutationReferencesAndUnknowns(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	before := state(t, s)
	s.Delete("/api/proxy-groups/auto", auth).WantStatus(409)
	s.Put("/api/proxy-groups/auto", auth, httpxtest.JSON(map[string]any{"id": "different"})).WantStatus(409)
	if state(t, s) != before {
		t.Fatal("rejected mutation changed data")
	}
	s.Put("/api/proxy-groups/auto", auth, httpxtest.JSON(map[string]any{"name": "Renamed"})).WantStatus(200)
	p, err := s.Repo.GetProfile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	group := p.GetList("proxy_groups")[0].(*jsonx.Object)
	if !group.GetObject("future").GetBool("keep") || p.GetList("rule_configs")[0].(*jsonx.Object).GetString("policy") != "Renamed" {
		t.Fatal("rename lost unknown data or policy reference")
	}
	s.Post("/api/proxy-groups/reorder", auth, httpxtest.JSON(map[string]any{"groups": []any{map[string]any{"id": "select", "name": "display value"}}})).WantStatus(200)
	p, err = s.Repo.GetProfile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if p.GetList("proxy_groups")[0].(*jsonx.Object).GetString("name") != "Select" {
		t.Fatal("legacy reorder persisted display-only fields")
	}
	before = state(t, s)
	s.Do("HEAD", "/api/proxy-groups", auth).WantStatus(200)
	if state(t, s) != before {
		t.Fatal("HEAD wrote data")
	}
	s.Post("/api/profiles/other/proxy-groups", auth, httpxtest.JSON(map[string]any{"id": "new", "name": "New", "type": "fallback", "manual_nodes": []string{"DIRECT"}})).WantStatus(200)
	s.Get("/api/proxy-groups/health-defaults", auth).WantStatus(200)
}
func TestPreviewBucketOrderingAndSourceFilters(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	before := state(t, s)
	tests := []struct {
		source, include, exclude string
		names                    []string
		total                    int
	}{{"subscription", "HK", "02", []string{"HK 01"}, 5}, {"aggregation", "JP`HK 02`HK", "", []string{"JP 01", "HK 02", "HK 01", "HK manual"}, 6}, {"aggregation", "HK 02`", "", []string{"HK 02", "HK 01", "JP 01", "广告", "HK manual"}, 6}, {"aggregation", "", ".*", []string{}, 6}}
	for _, test := range tests {
		t.Run(test.source+test.include+test.exclude, func(t *testing.T) {
			r := s.Post("/api/proxy-groups/preview-regex", auth, httpxtest.JSON(map[string]any{"source": test.source, "subscriptions": []string{"feed"}, "aggregations": []string{"agg"}, "regex": test.include, "exclude_regex": test.exclude})).WantStatus(200)
			var out previewResult
			if err := json.Unmarshal(r.Body, &out); err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, node := range out.Nodes {
				names = append(names, node.Name)
			}
			if !reflect.DeepEqual(names, test.names) || out.Total != test.total {
				t.Fatalf("got %+v, names %v", out, names)
			}
		})
	}
	for _, patch := range []map[string]any{{"regex": "("}, {"regex": false}, {"exclude_regex": []string{}}, {"exclude_regex": "("}} {
		patch["source"] = "subscription"
		s.Post("/api/proxy-groups/preview-regex", auth, httpxtest.JSON(patch)).WantStatus(400)
	}
	if state(t, s) != before {
		t.Fatal("preview changed database")
	}
}
