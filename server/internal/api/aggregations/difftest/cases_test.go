package difftest

import (
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"context"
	"fmt"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func seed(t *testing.T, used bool, pattern string) func(context.Context, *repo.Repository) {
	t.Helper()
	return func(ctx context.Context, r *repo.Repository) {
		_, err := r.SaveShared(ctx, jsonx.Obj("subscriptions", []any{jsonx.Obj("id", "s", "name", "Feed", "url", "https://feed.example/s", "enabled", true)}, "nodes", []any{node("manual"), node("relay")}, "subscription_aggregations", []any{
			jsonx.Obj("id", "a", "name", "Aggregate", "enabled", true, "subscriptions", []any{"s"}, "nodes", []any{"manual"}, "regex_filter", pattern),
			jsonx.Obj("id", "b", "name", "Second", "enabled", true, "subscriptions", []any{}, "nodes", []any{"relay"}),
			jsonx.Obj("id", "off", "name", "Disabled", "enabled", false, "subscriptions", []any{}, "nodes", []any{}),
		}), nil)
		must(t, err)
		if used {
			_, _, err = r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
				p.Set("proxy_groups", []any{jsonx.Obj("id", "g", "name", "Entry", "type", "select", "aggregations", []any{"a"})})
				return nil, nil
			})
			must(t, err)
		}
		_, err = substore.SaveSubscriptionNodes(ctx, "s", []any{node("cached")}, nil)
		must(t, err)
	}
}

const feed = "proxies: [{name: Remote, type: http, server: remote.example, port: 8080}]"

func TestCatalogAndReorder(t *testing.T) {
	w := newWorld(t, seed(t, false, ""), map[string]string{"s": feed})
	for _, p := range []string{"/api/aggregations", "/api/aggregations/a", "/api/aggregations/missing", "/api/aggregations/a/count", "/api/aggregations/missing/count", "/api/aggregations/a/health-impact", "/api/aggregations/missing/health-impact"} {
		w.call("GET", p, "", true)
	}
	for _, b := range []string{`null`, `[]`, `false`, `1`, `"s"`} {
		w.call("POST", "/api/aggregations", b, true)
		w.call("PUT", "/api/aggregations/a", b, true)
	}
	w.call("PUT", "/api/aggregations/missing", `{}`, true)
	w.call("PUT", "/api/aggregations/a", `{`, true)
	// Full-object replacement, path identity, unknown fields and statistics stripping.
	w.call("PUT", "/api/aggregations/a", `{"id":"forged","name":"Aggregate","enabled":true,"subscriptions":["s"],"nodes":["manual"],"custom":{"keep":[1,2]},"node_count":99,"total_node_count":99,"subscription_node_counts":{"s":99},"loading_count":99}`, true)
	for _, b := range []string{`{}`, `{"ids":["missing"]}`, `{"ids":["a","a"]}`, `{"ids":[1]}`, `{"ids":"a"}`, `null`, `[]`, `false`, `"yes"`, `{`, `{"ids":["b","a"]}`, `{"aggregations":[{"id":"off","name":"forged"},{"id":"b"}]}`, `{"ids":["a","b","off"]}`} {
		w.call("POST", "/api/aggregations/reorder", b, true)
	}
	w.call("POST", "/api/aggregations", `{"id":"forged","name":"Created","nodes":[],"subscriptions":[],"custom":{"x":1},"node_count":20,"total_node_count":20,"subscription_node_counts":{},"loading_count":3}`, true)
	w.call("GET", "/api/aggregations", "", true)
	w.call("DELETE", "/api/aggregations/missing", "", true)
	w.call("DELETE", "/api/aggregations/a", "", true)
	for _, p := range []string{"/api/aggregations", "/api/aggregations/b", "/api/aggregations/b/count", "/api/aggregations/b/preview", "/api/aggregations/b/health-impact", "/api/aggregations/b/provider?token=c31-pull"} {
		w.call("GET", p, "", false)
	}
	w.call("POST", "/api/aggregations/reorder", `{"ids":[]}`, false)
	w.call("DELETE", "/api/aggregations/b", "", false)
	w.call("POST", "/api/aggregations", `{}`, false)
	w.call("PUT", "/api/aggregations/b", `{}`, false)
}
func TestProviderAndPreview(t *testing.T) {
	w := newWorld(t, seed(t, true, ""), map[string]string{"s": feed})
	w.call("GET", "/api/aggregations/a/preview", "", true)
	w.call("GET", "/api/aggregations/missing/preview", "", true)
	w.call("GET", "/api/aggregations/a/provider", "", true)
	for _, q := range []string{"format=surge", "format=loon", "format=Mihomo", "regex=Remote", "include=Remote", "exclude=Remote", "include=[", "format=surge&include=manual", "format=loon&exclude=Remote"} {
		w.call("GET", "/api/aggregations/a/provider?"+q, "", true)
	}
	for _, id := range []string{"missing", "off", "a"} {
		for _, token := range []string{"", "bad", "c31-retired", "c31-pull", "c31-internal"} {
			w.call("GET", "/api/profiles/default/aggregations/"+id+"/provider?token="+token, "", false)
		}
	}
	w.call("GET", "/api/profiles/missing/aggregations/a/provider?token=c31-pull", "", false)
	w.call("DELETE", "/api/aggregations/a", "", true) // referenced resource must remain whole
	w.control.Offline = true
	w.call("GET", "/api/aggregations/a/preview", "", true)
}
func TestInvalidAggregateRegex(t *testing.T) {
	w := newWorld(t, seed(t, true, "["), map[string]string{"s": feed})
	w.call("GET", "/api/aggregations/a/preview", "", true)
	w.call("GET", "/api/profiles/default/aggregations/a/provider?token=c31-pull", "", false)
}

func TestDryGeneratorCorpus(t *testing.T) {
	sources := map[string]string{
		"s":  `proxies: [{name: Same, type: http, server: first.test, port: 80}, {name: One, type: ss, server: one.test, port: 443, cipher: aes-128-gcm, password: secret}]`,
		"s2": `proxies: [{name: Same, type: http, server: second.test, port: 81}, {name: Two, type: http, server: two.test, port: 80}]`,
	}
	w := newWorld(t, nil, sources)
	cfg := jsonx.Obj("profile_id", "default", "subscriptions", []any{jsonx.Obj("id", "s", "name", "First", "url", "https://feed.example/s"), jsonx.Obj("id", "s2", "name", "Second", "url", "https://feed.example/s2")}, "nodes", []any{node("manual"), jsonx.Obj("id", "same", "name", "Same", "type", "http", "server", "manual.test", "port", 90), jsonx.Obj("id", "off", "name", "Disabled", "enabled", false, "type", "http", "server", "off.test", "port", 80)}, "subscription_aggregations", []any{}, "proxy_groups", []any{}, "rule_configs", []any{})
	for _, selection := range [][]any{{}, {"s"}, {"s2", "s"}, {"s", "s2"}, {"missing"}} {
		for _, regex := range []any{"", "Same|manual", "(?<=O)ne", "[", nil, 123} {
			agg := jsonx.Obj("id", "a", "name", "Aggregate", "subscriptions", selection, "nodes", []any{"manual", "same", "off"}, "regex_filter", regex)
			raw, err := jsonx.Compact(jsonx.Obj("config", cfg, "aggregation", agg))
			must(t, err)
			w.call("GENERATE", "a", string(raw), true)
		}
	}
	// Immutable actual main decides activation and the valid chain target set.
	for _, main := range []string{`{"proxies":[],"proxy-groups":[]}`, `{"proxies":[{"name":"relay","type":"http","server":"x.test","port":80}],"proxy-groups":[]}`, `{"proxies":[{"name":"Exit","type":"http","server":"x.test","port":80,"dialer-proxy":"DIRECT"}],"proxy-groups":[]}`} {
		for _, text := range []string{`proxies: [{name: Chained, type: http, server: r.test, port: 80, dialer-proxy: relay}]`, `proxies: [{name: Same, type: http, server: a.test, port: 80}, {name: Same, type: http, server: b.test, port: 80}]`, `proxies: [{name: DIRECT, type: http, server: a.test, port: 80}]`, `proxies: [null]`, `proxies: [`} {
			w.control.Sources["s"] = text
			raw, err := jsonx.Compact(jsonx.Obj("config", cfg, "aggregation", jsonx.Obj("id", "a", "name", "Aggregate", "subscriptions", []any{"s"}, "nodes", []any{}), "main", parse(t, main), "prepared", true))
			must(t, err)
			w.call("GENERATE", "a", string(raw), true)
		}
	}
}

func parse(t *testing.T, text string) any {
	t.Helper()
	v, err := jsonx.Parse([]byte(text))
	must(t, err)
	return v
}

func TestGlobalRenderedBundle(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, chain := range []bool{false, true} {
			for sourceIndex, source := range []string{`proxies: [{name: Same, type: http, server: a.test, port: 80}, {name: Same, type: http, server: b.test, port: 80}]`, `proxies: [{name: Remote, type: http, server: good.test, port: 80, dialer-proxy: relay}]`, `proxies: [{name: Remote, type: http, server: good.test, port: 80, dialer-proxy: DIRECT}]`} {
				label := fmt.Sprintf("reverse=%t/chain=%t/source=%d", reverse, chain, sourceIndex)
				t.Run(label, func(t *testing.T) {
					setup := func(ctx context.Context, r *repo.Repository) {
						exit := node("exit")
						exit.Set("proxy_string", "opaque://exit")
						aggs := []any{jsonx.Obj("id", "chain", "name", "Chain", "nodes", []any{"exit"}, "subscriptions", []any{}), jsonx.Obj("id", "other", "name", "Other", "nodes", []any{}, "subscriptions", []any{"s"})}
						if reverse {
							aggs[0], aggs[1] = aggs[1], aggs[0]
						}
						_, err := r.SaveShared(ctx, jsonx.Obj("nodes", []any{node("relay"), exit}, "subscriptions", []any{jsonx.Obj("id", "s", "name", "Feed", "url", "https://feed.example/s")}, "subscription_aggregations", aggs), nil)
						must(t, err)
						_, _, err = r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
							p.Set("proxy_groups", []any{jsonx.Obj("id", "g", "name", "Entry", "type", "select", "manual_nodes", []any{"relay"}, "aggregations", []any{"chain", "other"})})
							return nil, nil
						})
						must(t, err)
					}
					w := newWorld(t, setup, map[string]string{"s": source})
					w.control.URIChain = chain
					w.call("GET", "/api/aggregations/chain/preview", "", true)
					w.call("GET", "/api/profiles/default/aggregations/other/provider?token=c31-pull", "", false)
					w.call("GET", "/api/aggregations/other/preview", "", true)
					w.call("GET", "/api/aggregations/chain/provider?format=surge", "", true)
					w.call("GET", "/api/aggregations/chain/provider?format=loon", "", true)
				})
			}
		}
	}

}

func TestSharedCatalogSelectionAndProtection(t *testing.T) {
	w := newWorld(t, func(ctx context.Context, r *repo.Repository) {
		seed(t, true, "")(ctx, r)
		_, err := r.CreateProfile(ctx, jsonx.Obj("id", "other", "name", "Other"), "default")
		must(t, err)
	}, map[string]string{"s": feed})
	// The request helper applies the same deliberately stale selector to both apps.
	w.extraHeaders = map[string]string{"X-ConfigFlow-Profile": "../missing"}
	w.call("GET", "/api/aggregations", "", true)
	w.call("GET", "/api/aggregations/a/count", "", true)
	w.call("POST", "/api/aggregations/reorder", `{"aggregations":[{"id":"b","name":"Forged"},{"id":"a"},{"id":"off"}]}`, true)
	w.call("DELETE", "/api/aggregations/a", "", true)
	w.extraHeaders = map[string]string{"X-ConfigFlow-Profile": "other"}
	w.call("GET", "/api/aggregations/a/preview", "", true)
}
