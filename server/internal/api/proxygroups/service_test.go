package proxygroups

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/api/aggregations"
	apirules "configflow/internal/api/rules"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/testjson"
)

func fixture(t *testing.T) *httpxtest.Server {
	t.Helper()
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(aggregations.Register, apirules.Register, Register)})
	err := s.Repo.UpdateShared(s.Context(), func(_ context.Context, p *model.Shared) error {
		if err := p.SetJSON("subscriptions", json.RawMessage(`[{"id":"feed","name":"Feed","url":"https://source.example/feed","regex_filter":"HK|JP|广告"}]`)); err != nil {
			return err
		}
		if err := p.SetJSON("nodes", json.RawMessage(`[{"id":"manual","name":"HK manual","type":"http","server":"example.com","port":80}]`)); err != nil {
			return err
		}
		return p.SetJSON("subscription_aggregations", json.RawMessage(`[{"id":"agg","name":"Aggregate","subscriptions":["feed"],"nodes":["manual"]}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, p *model.Profile) error {
		if err := p.SetJSON("proxy_groups", json.RawMessage(`[{"id":"auto","name":"Auto","type":"url-test","manual_nodes":["DIRECT"],"url":"http://probe.example/old","interval":73,"future":{"keep":true}},{"id":"select","name":"Select","type":"select","manual_nodes":["DIRECT"]}]`)); err != nil {
			return err
		}
		return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"rule","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":"Auto"}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Repo.CreateProfileRecord(s.Context(), repo.ProfileCreation{ID: "other", Name: "Other"}, "default"); err != nil {
		t.Fatal(err)
	}
	var nodes []model.Node
	for _, name := range []string{"HK 01", "HK 02", "JP 01", "广告", "US hidden", "HK 01"} {
		nodes = append(nodes, model.Node{Name: name, Type: "ss"})
	}
	if _, err = substore.SaveCache(s.Context(), s.Repo, "feed", nodes, nil); err != nil {
		t.Fatal(err)
	}
	return s
}
func state(t *testing.T, s *httpxtest.Server) string {
	t.Helper()
	raw, err := s.Repo.ExportAllJSON(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func TestBatchRollbackAndProfileIsolation(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	before, err := s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	beforeRaw, _ := json.Marshal(before)
	s.Put("/api/proxy-groups/batch-health-url?profile=other", auth, httpxtest.JSON(map[string]any{"ids": []string{"auto"}, "url": "https://probe.example/new"})).WantStatus(200)
	after, err := s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	afterRaw, _ := json.Marshal(after)
	if !testjson.Equal(beforeRaw, afterRaw) {
		t.Fatal("other-profile edit changed default")
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
	p, err := s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	groupRaw, _ := json.Marshal(p.ProxyGroups[0])
	groupParsed, _ := testjson.Parse(groupRaw)
	// Undeclared group members are dropped (O18 T1c).
	_, kept := testjson.Path(groupParsed, "future", "keep")
	if kept || p.RuleConfigs[0].Policy == nil || *p.RuleConfigs[0].Policy != "Renamed" {
		t.Fatal("rename lost unknown data or policy reference")
	}
	s.Post("/api/proxy-groups/reorder", auth, httpxtest.JSON(map[string]any{"groups": []any{map[string]any{"id": "select", "name": "display value"}}})).WantStatus(200)
	p, err = s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if p.ProxyGroups[0].Name != "Select" {
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

func TestListenerRelinkingPreservesUserComments(t *testing.T) {
	s := fixture(t)
	auth := s.Login()
	_, err := s.Repo.UpdateProfile(s.Context(), "other", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("mihomo", json.RawMessage(`{"custom_config":"# user heading\nlisteners:\n  - name: local\n    type: mixed\n    port: 7890\n    proxy: Select # keep comment\n"}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, patch := range []map[string]any{{"name": "Renamed"}, {"enabled": false}} {
		r := s.Put("/api/proxy-groups/select?profile=other", auth, httpxtest.JSON(patch)).WantStatus(200)
		var out struct {
			ListenerChanges []any `json:"listener_changes"`
		}
		if err := json.Unmarshal(r.Body, &out); err != nil {
			t.Fatal(err)
		}
		if len(out.ListenerChanges) != 1 {
			t.Fatal("listener was not relinked", r.Text())
		}
		p, err := s.Repo.Profile(s.Context(), "other")
		if err != nil {
			t.Fatal(err)
		}
		content := p.Mihomo.CustomConfig
		if !strings.Contains(content, "# user heading") || !strings.Contains(content, "# keep comment") {
			t.Fatalf("listener editing lost comments: %q", content)
		}
	}
}
