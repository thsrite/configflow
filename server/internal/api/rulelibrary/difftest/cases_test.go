package difftest

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/substore"
)

func seed(t *testing.T) func(context.Context, *repo.Repository) {
	t.Helper()
	return func(ctx context.Context, r *repo.Repository) {
		if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
			t.Fatal(err)
		}
		err := r.UpdateShared(ctx, func(_ context.Context, p *model.Shared) error {
			if err := p.SetJSON("subscriptions", json.RawMessage(`[{"id":"feed","name":"Feed","url":"https://source.example/feed","regex_filter":"HK|JP|广告"}]`)); err != nil {
				return err
			}
			if err := p.SetJSON("nodes", json.RawMessage(`[{"id":"node","name":"HK manual","type":"http","server":"example.com","port":80}]`)); err != nil {
				return err
			}
			return p.SetJSON("subscription_aggregations", json.RawMessage(`[{"id":"agg","name":"Aggregation","subscriptions":["feed"],"nodes":["node"]}]`))
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"default", "other"} {
			_, err = r.UpdateProfile(ctx, id, func(_ context.Context, p *model.Profile) error {
				return p.SetJSON("proxy_groups", json.RawMessage(`[{"id":"group","name":"Group","type":"select","subscriptions":["feed"],"regex":"HK","future":{"keep":true}}]`))
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		nodes := []any{}
		for _, name := range []string{"HK 01", "HK 02", "JP 01", "广告", "US hidden", "HK 01"} {
			nodes = append(nodes, map[string]any{"name": name, "type": "ss"})
		}
		if err = fixtureSaveSubscriptionNodes(ctx, r, "feed", nodes, nil); err != nil {
			t.Fatal(err)
		}
	}
}
func TestProxyGroupHTTPParity(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, path := range []string{"/api/proxy-groups", "/api/profiles/other/proxy-groups", "/api/proxy-groups/health-defaults"} {
		w.call("GET", path, "", true)
		w.call("GET", path, "", false)
	}
	for _, path := range []string{"/api/proxy-groups", "/api/profiles/other/proxy-groups"} {
		w.call("POST", path, `{"id":"new","name":"New","type":"url-test","manual_nodes":["DIRECT"],"future":{"x":1}}`, true)
		w.call("POST", path, `[]`, true)
	}
	for _, body := range []string{`{"regex":"","exclude_regex":"广告|02","aggregation_regex":"HK","aggregation_exclude_regex":"manual"}`, `{"exclude_regex":""}`, `{"id":"wrong"}`, `{"exclude_regex":123}`, `{"delete_fields":["interval"]}`} {
		w.call("PUT", "/api/proxy-groups/group?profile=other", body, true)
	}
	for _, body := range []string{`{"ids":["new"],"url":"https://probe.example/new"}`, `{"ids":["new","group"],"url":"https://probe.example/new"}`, `{"ids":["new","missing"],"url":"https://probe.example/new"}`, `{"ids":["new","new"],"url":"https://probe.example/new"}`, `{"ids":["new"],"url":"https://u:p@probe.example/new"}`, `[]`} {
		w.call("PUT", "/api/proxy-groups/batch-health-url", body, true)
	}
	for _, body := range []string{`{"ids":["new"]}`, `{"groups":[{"id":"group","name":"display-only"}]}`, `{"ids":["missing"]}`} {
		w.call("POST", "/api/proxy-groups/reorder", body, true)
	}
	for _, source := range []string{"subscription", "aggregation"} {
		for _, filters := range []string{`"regex":"HK","exclude_regex":"02"`, `"regex":"JP` + "`" + `HK 02` + "`" + `HK"`, `"exclude_regex":"广告|02|manual"`, `"regex":"HK 02` + "`" + `"`, `"regex":false`, `"exclude_regex":[]`, `"regex":".*","deduplicate_by_source":true`} {
			w.call("POST", "/api/proxy-groups/preview-regex", `{"source":"`+source+`","subscriptions":["feed"],"aggregations":["agg"],`+filters+`}`, true)
		}
	}
	w.call("POST", "/api/proxy-groups/preview-regex", `{"source":"bad","regex":".*"}`, true)
	w.call("DELETE", "/api/proxy-groups/new", "", true)
	w.call("DELETE", "/api/proxy-groups/new", "", true)
	w.call("PUT", "/api/proxy-groups/group", `{"name":"Renamed"}`, true)
}
func TestRuleLibraryReadCreateHTTPParity(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, path := range []string{"/api/rule-library", "/api/rule-library/proxy-domains"} {
		w.call("GET", path, "", true)
		w.call("GET", path, "", false)
	}
	for _, body := range []string{`null`, `[]`, `{}`, `{"id":"manual","name":"Manual","source_type":"content","behavior":"domain","format":"text","content":"+.example.com\n","url":"https://unused.example/rules","future":{"keep":true},"has_ip_part":true}`, `{"id":"manual","source_type":"content","content":"other.example"}`, `{"id":"remote","name":"Remote","url":"","source_type":"url"}`} {
		w.call("POST", "/api/rule-library", body, true)
	}
	w.call("GET", "/api/rule-library", "", true)
	for _, path := range []string{"/api/rule-library/content/manual", "/api/profiles/default/rule-library/content/manual", "/api/rule-library/content/missing", "/api/profiles/other/rule-library/content/missing"} {
		w.call("GET", path, "", false)
	}
	for _, body := range []string{`{"content":"payload:\n  - +.a.example\n","behavior":"domain","format":"yaml","append_lines":["+.b.example"]}`, `{"content":"DOMAIN,a.example\n","behavior":"classical","format":"text"}`, `{"content":"192.0.2.0/24","behavior":"domain","format":"text"}`, `{"content":1}`, `{"content":"a.example","append_lines":[null]}`} {
		w.call("POST", "/api/rule-library/check-content", body, true)
	}
	w.call("POST", "/api/rule-library/proxy-domains", `{"proxy_domains":"  https://proxy.example/  "}`, true)
	w.call("POST", "/api/rule-library/proxy-domains", `{"proxy_domains":42}`, true)
	w.call("GET", "/api/rule-library/proxy-domains", "", true)
	w.call("POST", "/api/rule-library/reorder", `{"rules":[{"id":"manual","content":"display-only"}]}`, true)
	w.call("POST", "/api/rule-library/reorder", `{"ids":["missing"]}`, true)
}

func fixtureSaveSubscriptionNodes(ctx context.Context, r *repo.Repository, id string, value any, metadata any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var nodes []model.Node
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return err
	}
	var receipt *substore.CacheMetadata
	if metadata != nil {
		raw, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		receipt = &substore.CacheMetadata{}
		if err := json.Unmarshal(raw, receipt); err != nil {
			return err
		}
	}
	_, err = substore.SaveCache(ctx, r, id, nodes, receipt)
	return err
}
