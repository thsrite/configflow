package difftest

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/substore"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func seedGraph(t *testing.T, chain string, second bool, cached bool) func(context.Context, *repo.Repository) {
	return func(ctx context.Context, r *repo.Repository) {
		catalog := []any{node("relay"), node("exit")}
		if chain == "raw" {
			catalog[1].(*jsonx.Object).Set("params", jsonx.Obj("dialer-proxy", "relay"))
		}
		if chain == "uri" {
			catalog[1].(*jsonx.Object).Set("proxy_string", "opaque://exit")
		}
		feeds := []any{jsonx.Obj("id", "s", "name", "Feed", "url", "https://feed.example/s", "enabled", true)}
		ids := []any{"s"}
		if second {
			feeds = append(feeds, jsonx.Obj("id", "s2", "name", "Second", "url", "https://feed.example/s2"))
			ids = append(ids, "s2")
		}
		_, err := r.SaveShared(ctx, jsonx.Obj("nodes", catalog, "subscriptions", feeds, "subscription_aggregations", []any{}), nil)
		must(t, err)
		groups := []any{jsonx.Obj("id", "g", "name", "Entry", "type", "select", "manual_nodes", []any{"exit", "relay"}, "subscriptions", ids)}
		if chain == "managed" {
			groups[0].(*jsonx.Object).Set("include_groups", []any{"chain-exit"})
			groups = append(groups, jsonx.Obj("id", "chain-exit", "name", "Via exit", "type", "chain", "chain", jsonx.Obj("entry", jsonx.Obj("type", "node", "id", "relay"), "exit", jsonx.Obj("type", "node", "id", "exit"))))
		}
		_, _, err = r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.Set("proxy_groups", groups)
			return nil, nil
		})
		must(t, err)
		if cached {
			_, err := fixtureSaveSubscriptionNodes(ctx, "s", []any{node("old")}, nil)
			must(t, err)
		}
	}
}
func TestManagementWrites(t *testing.T) {
	w := newWorld(t, nil, nil)
	w.call("GET", "/api/nodes", "", true)
	for _, body := range []string{`null`, `[]`, `{"id":"bad","name":"Bad","dialer_ref":null}`} {
		w.call("POST", "/api/nodes", body, true)
	}
	w.call("POST", "/api/nodes", `{"id":"n","name":"Node","custom":{"keep":true}}`, true)
	w.call("PUT", "/api/nodes/n", `null`, true)
	w.call("PUT", "/api/nodes/n", `{"id":"spoof","name":"Node"}`, true)
	w.call("PUT", "/api/nodes/missing", `{"name":"Node"}`, true)
	w.call("PUT", "/api/nodes/n", `{"name":"Renamed","custom":{"keep":true}}`, true)
	w.call("POST", "/api/nodes/reorder", `{"nodes":[{"id":"n","name":"forged"}]}`, true)
	w.call("POST", "/api/nodes/reorder", `{"ids":["missing"]}`, true)
	w.call("POST", "/api/nodes/reorder", `{"ids":["n","n"]}`, true)
	w.call("POST", "/api/nodes/parse-lines", `{"lines":[" demo ","","unrecognized"]}`, true)
	for _, body := range []string{`{}`, `{"lines":[null]}`, `{"lines":[1]}`, `{"lines":"x"}`} {
		w.call("POST", "/api/nodes/parse-lines", body, true)
	}
	w.call("DELETE", "/api/nodes/missing", "", true)
	w.call("DELETE", "/api/nodes/n", "", true)
	w.call("GET", "/api/subscriptions", "", true)
	w.call("GET", "/api/subscriptions/test", "", true)
	for _, body := range []string{`null`, `[]`, `{}`, `{"name":42,"url":"https://feed.example/s"}`, `{"name":"Feed","url":" "}`, `{"name":"Feed","url":"https://feed.example/s","interval":1}`, `{"name":"Feed","url":"https://feed.example/s","regex_filter":"["}`} {
		w.call("POST", "/api/subscriptions", body, true)
	}
	w.call("POST", "/api/subscriptions", `{"id":"s","name":"Feed","url":"https://feed.example/s","cached_node_count":99,"traffic":{"discard":true},"custom":{"keep":true}}`, true)
	w.call("PUT", "/api/subscriptions/missing", `{}`, true)
	w.call("PUT", "/api/subscriptions/s", `{"name":"","url":"https://feed.example/s"}`, true)
	w.call("PUT", "/api/subscriptions/s", `{"id":"forged","name":"Changed","url":"https://feed.example/s","last_fetch":"discard","custom":{"next":true}}`, true)
	w.call("POST", "/api/subscriptions/reorder", `{"subscriptions":[{"id":"s","url":"forged"}]}`, true)
	w.call("POST", "/api/subscriptions/reorder", `{"ids":["missing"]}`, true)
	w.call("POST", "/api/subscriptions/reorder", `{"ids":["s","s"]}`, true)
	w.call("GET", "/api/subscriptions/health", "", true)
	w.call("GET", "/api/subscriptions/s/health-impact", "", true)
	w.call("GET", "/api/subscriptions/missing/health-impact", "", true)
	w.call("GET", "/api/subscriptions/s/nodes?source=cache", "", true)
	w.call("GET", "/api/subscriptions/s/nodes", "", true)
	w.call("GET", "/api/subscriptions/missing/nodes", "", true)
	w.call("POST", "/api/subscriptions/missing/fetch", `{}`, true)
	w.call("DELETE", "/api/subscriptions/s", "", true)
	w.call("DELETE", "/api/subscriptions/missing", "", true) // Missing DELETE intentionally succeeds in Python.
	for _, route := range []string{"/api/nodes", "/api/subscriptions", "/api/subscriptions/test", "/api/subscriptions/proxies?token=c30-pull", "/api/subscriptions/s/proxies?token=c30-pull"} {
		w.call("GET", route, "", false)
	}
}
func TestFetchPreviewAndImport(t *testing.T) {
	for _, preview := range []bool{true, false} {
		t.Run(fmt.Sprint(preview), func(t *testing.T) {
			w := newWorld(t, seedGraph(t, "none", false, false), map[string]string{"s": "proxies: [{name: Remote, type: http, server: example.com, port: 80}]"})
			w.call("POST", "/api/subscriptions/s/fetch", fmt.Sprintf(`{"preview":%t}`, preview), true)
			w.call("GET", "/api/subscriptions", "", true)
			w.call("GET", "/api/subscriptions/health", "", true)
			w.call("GET", "/api/subscriptions/s/nodes?source=cache", "", true)
			w.call("GET", "/api/subscriptions/s/nodes", "", true)
			w.call("GET", "/api/subscriptions", "", true)
		})
	}
}
func TestReviewedDeletion(t *testing.T) {
	for _, blocker := range []string{"none", "chain", "dialer", "dialer-alias", "rule"} {
		t.Run(blocker, func(t *testing.T) {
			setup := func(ctx context.Context, r *repo.Repository) {
				catalog := []any{node("remove"), node("keep")}
				catalog[0].(*jsonx.Object).Set("password", "synthetic-preview-secret")
				if blocker == "dialer" {
					catalog[1].(*jsonx.Object).Set("params", jsonx.Obj("dialer-proxy", "remove"))
				}
				if blocker == "dialer-alias" {
					catalog[1].(*jsonx.Object).Set("proxy_string", "name: keep\ntype: http\nserver: example.com\nport: 80\nshared_target: &target remove\ndialer-proxy: *target\n")
				}
				_, err := r.SaveShared(ctx, jsonx.Obj("nodes", catalog, "subscription_aggregations", []any{jsonx.Obj("id", "agg", "name", "Agg", "nodes", []any{"remove", "keep"}, "subscriptions", []any{})}), nil)
				must(t, err)
				groups := []any{jsonx.Obj("id", "g", "name", "Group", "type", "select", "manual_nodes", []any{"remove", "keep", "DIRECT"}, "aggregations", []any{"agg"}, "proxies_order", []any{jsonx.Obj("type", "node", "id", "remove")})}
				if blocker == "chain" {
					groups = append(groups, jsonx.Obj("id", "chain", "name", "Chain", "type", "chain", "enabled", false, "chain", jsonx.Obj("entry", jsonx.Obj("type", "node", "id", "remove"), "exit", jsonx.Obj("type", "node", "id", "keep"))))
				}
				_, _, err = r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
					p.Set("proxy_groups", groups)
					if blocker == "rule" {
						p.Set("rule_configs", []any{jsonx.Obj("id", "rule", "itemType", "rule", "rule_type", "MATCH", "policy", "remove")})
					}
					return nil, nil
				})
				must(t, err)
				_, err = r.CreateProfile(ctx, jsonx.Obj("id", "other", "name", "Other"), "")
				must(t, err)
				_, _, err = r.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
					p.Set("proxy_groups", []any{jsonx.Obj("id", "legacy", "name", "Legacy", "type", "select", "source", "node", "proxies", []any{"remove", "keep"})})
					return nil, nil
				})
				must(t, err)
			}
			w := newWorld(t, setup, nil)
			w.call("POST", "/api/nodes/delete-preview", `{"ids":["missing"]}`, true)
			for _, body := range []string{`null`, `[]`, `{}`, `{"ids":[]}`, `{"ids":[null]}`, `{"ids":"remove"}`} {
				w.call("POST", "/api/nodes/delete-preview", body, true)
			}
			w.call("POST", "/api/nodes/delete", `{"ids":["remove"]}`, true)
			plan := w.call("POST", "/api/nodes/delete-preview", `{"ids":["remove","remove"]}`, true)
			encoded, _ := json.Marshal(plan.Body)
			if strings.Contains(string(encoded), "synthetic-preview-secret") {
				t.Fatal("preview exposed node secret")
			}
			confirmation := plan.Body.(map[string]any)["confirmation"].(string)
			w.call("DELETE", "/api/nodes/remove", "", true)
			w.call("POST", "/api/nodes/delete", `{"ids":["remove"],"confirmation":"`+strings.Repeat("a", 64)+`"}`, true)
			w.call("POST", "/api/nodes/delete", `{"ids":["remove"],"confirmation":"`+confirmation+`"}`, true)
		})
	}
}
func TestProviderSecurity(t *testing.T) {
	w := newWorld(t, seedGraph(t, "none", false, true), map[string]string{"s": "proxies: [{name: Remote, type: http, server: example.com, port: 80}]"})
	for _, path := range []string{"/api/subscriptions/s/proxies?token=c30-pull", "/api/subscriptions/proxies?token=c30-pull"} {
		w.call("GET", path, "", false)
	}
	for _, token := range []string{"wrong", "", "c30-internal", "c30-retired"} {
		w.call("GET", "/api/profiles/default/subscriptions/s/proxies?token="+token, "", false)
	}
	w.call("GET", "/api/profiles/default/subscriptions/s/proxies?token=c30-pull", "", false)
	w.call("GET", "/api/profiles/default/subscriptions/missing/proxies?token=c30-pull", "", false)
	w.call("GET", "/api/profiles/default/subscriptions/s/proxies?token=c30-pull&regex=[", "", false)
	w.call("GET", "/api/subscriptions/s/proxies", "", true)
}
func TestProviderDeliveryGraphs(t *testing.T) {
	cases := []struct{ name, text string }{
		{"valid", "proxies: [{name: Remote, type: http, server: example.com, port: 80}]"},
		{"dangling", "proxies: [{name: Remote, type: http, server: example.com, port: 80, dialer-proxy: missing}]"},
		{"reserved", "proxies: [{name: Via exit, type: http, server: example.com, port: 80}, {name: DIRECT, type: http}]"},
		{"duplicate", "proxies: [{name: Via exit, type: http}, {name: Same, type: http}, {name: Same, type: http}]"},
		{"cycle", "proxies: [{name: Via exit, type: http}, {name: A, type: http, dialer-proxy: B}, {name: B, type: http, dialer-proxy: A}]"},
		{"malformed", "proxies: [{name: Remote, type: http, dialer-proxy: [relay]}]"},
		{"shape", "proxies: [null]"},
	}
	for _, chain := range []string{"none", "managed", "raw", "uri"} {
		for _, consumer := range []string{"single", "combined"} {
			for _, c := range cases {
				t.Run(chain+"/"+consumer+"/"+c.name, func(t *testing.T) {
					w := newWorld(t, seedGraph(t, chain, false, true), map[string]string{"s": c.text})
					w.control.URIChain = chain == "uri"
					path := "/api/subscriptions/s/proxies"
					if consumer == "combined" {
						path = "/api/subscriptions/proxies"
					}
					if consumer == "single" && c.name == "malformed" {
						expected := &nativeBodyExpectation{
							Python: map[string]any{"success": false, "message": `Provider 节点「Remote」的 dialer-proxy「['relay']」引用了不存在的代理或策略组；若它是机场订阅自带的中转分组，在「订阅来源」刷新该订阅后 ConfigFlow 会自动补上`},
							Native: map[string]any{"success": false, "message": `Provider 节点「Remote」的 dialer-proxy「["relay"]」引用了不存在的代理或策略组；若它是机场订阅自带的中转分组，在「订阅来源」刷新该订阅后 ConfigFlow 会自动补上`},
						}
						if chain != "none" {
							for _, body := range []map[string]any{expected.Python, expected.Native} {
								body["message"] = "订阅「Feed」准备失败：" + body["message"].(string) + "\n文件：./providers/Feed.yaml"
							}
						}
						w.callWithNativeBody("GET", path, "", true, expected)
					} else {
						w.call("GET", path, "", true)
					}
				})
			}
		}
	}
}
func TestCombinedCrossFeedAndStaging(t *testing.T) {
	for _, chain := range []string{"none", "managed"} {
		for _, kind := range []string{"valid", "cycle", "duplicate", "reserved", "bad-second"} {
			t.Run(chain+"/"+kind, func(t *testing.T) {
				first := "proxies: [{name: A, type: http, server: example.com, port: 80, dialer-proxy: B}]"
				second := "proxies: [{name: B, type: http, server: example.com, port: 80}]"
				switch kind {
				case "cycle":
					second = "proxies: [{name: B, type: http, dialer-proxy: A}]"
				case "duplicate":
					second = "proxies: [{name: A, type: http}]"
				case "reserved":
					second = "proxies: [{name: DIRECT, type: http}]"
				case "bad-second":
					second = "proxies: [null]"
				}
				w := newWorld(t, seedGraph(t, chain, true, true), map[string]string{"s": first, "s2": second})
				w.call("GET", "/api/subscriptions/proxies", "", true)
			})
		}
	}
}

func TestCachePreviewNeverRefreshes(t *testing.T) {
	for _, state := range []string{"legacy", "matching", "different-url", "empty", "invalid", "filtered", "non-list", "missing"} {
		t.Run(state, func(t *testing.T) {
			setup := func(ctx context.Context, r *repo.Repository) {
				_, err := r.SaveShared(ctx, jsonx.Obj("subscriptions", []any{jsonx.Obj("id", "s", "name", "Feed", "url", "https://feed.example/s", "interval", 0, "regex_filter", "^HK")}, "nodes", []any{jsonx.Obj("id", "imported", "name", "HK imported", "subscription_id", "s")}), nil)
				must(t, err)
				if state == "missing" {
					return
				}
				var list any = []any{jsonx.Obj("id", "hk", "name", "HK cached", "type", "http"), jsonx.Obj("id", "jp", "name", "JP cached", "type", "http")}
				if state == "empty" {
					list = []any{}
				}
				if state == "invalid" {
					list = nil
				}
				if state == "non-list" {
					list = jsonx.Obj("legacy", true)
				}
				if state == "filtered" {
					list = []any{jsonx.Obj("id", "jp", "name", "JP cached")}
				}
				metadata := jsonx.Obj("fetched_at", "2026-01-01T00:00:00+00:00")
				if state != "legacy" {
					metadata.Set("url", "https://feed.example/s")
				}
				if state == "different-url" {
					metadata.Set("url", "https://feed.example/previous")
				}
				if state == "non-list" {
					_, err = r.WriteSharedJSON(ctx, "subscribes/s.json", jsonx.Obj("subscription_id", "s", "updated_at", "2026-01-01T00:00:00+00:00", "count", 0, "nodes", list, "metadata", metadata))
				} else {
					_, err = fixtureSaveSubscriptionNodes(ctx, "s", list, metadata)
				}
				must(t, err)
			}
			w := newWorld(t, setup, nil)
			w.control.Offline = true
			before := normalize(snapshot(t, w.srv))
			for range 2 {
				w.call("GET", "/api/subscriptions/s/nodes?source=cache", "", true)
			}
			if !reflect.DeepEqual(before, normalize(snapshot(t, w.srv))) {
				t.Fatal("cache preview changed stored data")
			}
			w.call("GET", "/api/subscriptions/s/nodes", "", true)
			w.call("GET", "/api/subscriptions", "", true)
		})
	}
}
func TestTransportFallbackAndFormats(t *testing.T) {
	for _, chain := range []string{"none", "managed", "raw", "uri"} {
		for _, format := range []string{"", "surge", "loon"} {
			t.Run(chain+"/"+format, func(t *testing.T) {
				w := newWorld(t, seedGraph(t, chain, false, true), nil)
				w.control.Offline = true
				w.control.URIChain = chain == "uri"
				path := "/api/subscriptions/s/proxies"
				if format != "" {
					path += "?format=" + format
				}
				w.call("GET", path, "", true)
			})
		}
	}
	for _, cached := range []bool{true, false} {
		t.Run(fmt.Sprint("fetch/", cached), func(t *testing.T) {
			w := newWorld(t, seedGraph(t, "none", false, cached), nil)
			w.control.Offline = true
			w.call("POST", "/api/subscriptions/s/fetch", `{"preview":true}`, true)
		})
	}
}
func TestAlternateFormatFilters(t *testing.T) {
	for _, format := range []string{"surge", "loon"} {
		t.Run(format, func(t *testing.T) {
			w := newWorld(t, seedGraph(t, "none", false, false), map[string]string{"s": "proxies: [{name: Remote, type: http, server: example.com, port: 80}, {name: Other, type: http, server: example.com, port: 81}]"})
			w.call("GET", "/api/subscriptions/s/proxies?format="+format+"&regex=^Remote", "", true)
		})
	}
}

func TestDeliverySnapshotSurvivesConcurrentProfileChange(t *testing.T) {
	for _, chain := range []string{"none", "managed", "uri"} {
		for _, consumer := range []string{"single", "combined"} {
			t.Run(chain+"/"+consumer, func(t *testing.T) {
				w := newWorld(t, seedGraph(t, chain, false, true), map[string]string{"s": "proxies: [{name: Remote, type: http, server: example.com, port: 80, dialer-proxy: relay}]"})
				w.control.Mutate = true
				w.control.URIChain = chain == "uri"
				path := "/api/subscriptions/s/proxies"
				if consumer == "combined" {
					path = "/api/subscriptions/proxies"
				}
				w.call("GET", path, "", true)
			})
		}
	}
}
func TestDeletionConfirmationSurvivesOnlyUnrelatedWrites(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			setup := func(ctx context.Context, r *repo.Repository) {
				_, err := r.SaveShared(ctx, jsonx.Obj("nodes", []any{node("remove"), node("keep")}), nil)
				must(t, err)
			}
			w := newWorld(t, setup, nil)
			plan := w.call("POST", "/api/nodes/delete-preview", `{"ids":["remove"]}`, true)
			target := "keep"
			if changed {
				target = "remove"
			}
			w.call("PUT", "/api/nodes/"+target, `{"name":"Changed"}`, true)
			w.call("POST", "/api/nodes/delete", `{"ids":["remove"],"confirmation":"`+plan.Body.(map[string]any)["confirmation"].(string)+`"}`, true)
		})
	}
}

func TestDeletionDoesNotReviseUnaffectedOrderGroups(t *testing.T) {
	setup := func(ctx context.Context, r *repo.Repository) {
		_, err := r.SaveShared(ctx, jsonx.Obj("nodes", []any{node("remove"), node("keep")}), nil)
		must(t, err)
		_, _, err = r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.Set("proxy_groups", []any{jsonx.Obj("id", "keep-group", "name", "Keep Group", "type", "select", "manual_nodes", []any{"keep"}, "proxies_order", []any{jsonx.Obj("type", "node", "id", "keep")})})
			return nil, nil
		})
		must(t, err)
	}
	w := newWorld(t, setup, nil)
	plan := w.call("POST", "/api/nodes/delete-preview", `{"ids":["remove"]}`, true)
	if len(plan.Body.(map[string]any)["groups"].([]any)) != 0 {
		t.Fatal("unaffected group marked changed")
	}
	w.call("POST", "/api/nodes/delete", `{"ids":["remove"],"confirmation":"`+plan.Body.(map[string]any)["confirmation"].(string)+`"}`, true)
}
func TestSubscriptionDeletionProtectsSelectedSource(t *testing.T) {
	w := newWorld(t, seedGraph(t, "none", false, false), nil)
	w.call("DELETE", "/api/subscriptions/s", "", true)
}

func TestProfileSelectionIsolation(t *testing.T) {
	setup := func(ctx context.Context, r *repo.Repository) {
		seedGraph(t, "none", false, true)(ctx, r)
		_, err := r.CreateProfile(ctx, jsonx.Obj("id", "other", "name", "Other"), "default")
		must(t, err)
		_, _, err = r.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.Set("proxy_groups", append(p.GetList("proxy_groups"), jsonx.Obj("id", "other-target", "name", "OtherTarget", "type", "select", "manual_nodes", []any{"relay"})))
			return nil, nil
		})
		must(t, err)
	}
	w := newWorld(t, setup, map[string]string{"s": "proxies: [{name: Remote, type: http, server: example.com, port: 80, dialer-proxy: OtherTarget}]"})
	w.call("GET", "/api/subscriptions/proxies?profile=other", "", true)
	w.call("GET", "/api/subscriptions/proxies", "", true)
	w.call("GET", "/api/profiles/other/subscriptions/s/proxies?token=c30-pull", "", false)
	w.call("GET", "/api/profiles/default/subscriptions/s/proxies?token=c30-pull", "", false)
}
func TestNodeMutationsProtectChains(t *testing.T) {
	for _, chain := range []string{"managed", "raw"} {
		t.Run(chain, func(t *testing.T) {
			w := newWorld(t, seedGraph(t, chain, false, false), nil)
			w.call("PUT", "/api/nodes/relay", `{"id":"spoof","name":"relay"}`, true)
			w.call("PUT", "/api/nodes/relay", `{"name":"exit"}`, true)
			w.call("PUT", "/api/nodes/relay", `{"name":"DIRECT"}`, true)
			w.call("PUT", "/api/nodes/relay", `{"name":"relay","enabled":false}`, true)
			w.call("DELETE", "/api/nodes/relay", "", true)
			w.call("POST", "/api/nodes/reorder", `{"nodes":[{"id":"exit","name":"forged","params":{"dialer-proxy":"missing"}}]}`, true)
			w.call("PUT", "/api/nodes/relay", `{"name":"Renamed","type":"http","server":"example.com","port":80}`, true)
		})
	}
}
func TestDeletionWarnsOnEmptySources(t *testing.T) {
	setup := func(ctx context.Context, r *repo.Repository) {
		_, err := r.SaveShared(ctx, jsonx.Obj("nodes", []any{node("remove"), node("keep")}, "subscription_aggregations", []any{jsonx.Obj("id", "agg", "name", "Agg", "nodes", []any{"remove", "keep"}, "subscriptions", []any{})}), nil)
		must(t, err)
		_, _, err = r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.Set("proxy_groups", []any{jsonx.Obj("id", "g", "name", "Legacy", "type", "select", "source", "node", "proxies", []any{"remove", "keep"})})
			return nil, nil
		})
		must(t, err)
	}
	w := newWorld(t, setup, nil)
	plan := w.call("POST", "/api/nodes/delete-preview", `{"ids":["remove","keep"]}`, true)
	if len(plan.Body.(map[string]any)["empty_sources"].([]any)) != 2 {
		t.Fatal("missing empty-source warnings")
	}
	w.call("POST", "/api/nodes/delete", `{"ids":["remove","keep"],"confirmation":"`+plan.Body.(map[string]any)["confirmation"].(string)+`"}`, true)
}

// These cases derive from Flask's JWT allowlist and MultiDict credential lookup.
func TestAdversarialProviderURLs(t *testing.T) {
	w := newWorld(t, seedGraph(t, "none", false, true), map[string]string{"s": "proxies: [{name: Remote, type: http, server: example.com, port: 80}]"})
	prefix := "/api/profiles/default/subscriptions/s/proxies"
	for _, query := range []string{
		"", "?token=", "?TOKEN=c30-pull", "?token=C30-pull", "?token=c30-pull%00",
		"?token=c30-pull+", "?token=%256330-pull", "?token=wrong&token=c30-pull",
		"?token=&token=c30-pull", "?token=c30-pull&token=wrong", "?token=c30-pull&extra=unused",
		"?%74oken=%6330-pull", "?token=c30-pull&profile=missing", "?token=c30-pull&profile_id=missing",
	} {
		w.call("GET", prefix+query, "", false)
	}
	for _, path := range []string{
		"/api/%73ubscriptions/s/proxies?token=c30-pull",
		"/api/subscriptions/s/%70roxies?token=c30-pull",
		"/api/profiles/%64efault/subscriptions/s/proxies?token=c30-pull",
		"/api/profiles/default/subscriptions/%73/proxies?token=c30-pull",
		"/api/profiles/default/subscriptions/s/%70roxies?token=c30-pull",
		"/api/profiles/default/subscriptions/s%2f..%2fs/proxies?token=c30-pull",
		"/api/profiles/default/subscriptions/../s/proxies?token=c30-pull",
		"/api/profiles/default/subscriptions/s/proxies/?token=c30-pull",
		"/api/profiles/default/subscriptions/missing/proxies?token=wrong",
	} {
		w.call("GET", path, "", false)
	}
}

// C31 is deliberately absent: an activated graph requiring an aggregation
// must reject the complete delivery before any cache or served artifact changes.
func TestMissingAggregationDependencyFailsClosed(t *testing.T) {
	defer provider.SetAggregationRenderer(nil)()
	setup := func(ctx context.Context, r *repo.Repository) {
		seedGraph(t, "managed", false, true)(ctx, r)
		_, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
			shared.GetList("subscriptions")[0].(*jsonx.Object).Set("interval", jsonx.Int(0))
			shared.Set("subscription_aggregations", []any{jsonx.Obj("id", "agg", "name", "Agg", "nodes", []any{"relay"}, "subscriptions", []any{})})
			return nil, nil
		})
		must(t, err)
		_, _, err = r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.GetList("proxy_groups")[0].(*jsonx.Object).Set("aggregations", []any{"agg"})
			return nil, nil
		})
		must(t, err)
	}
	w := newWorld(t, setup, nil)
	before := normalize(snapshot(t, w.srv))
	for _, path := range []string{"/api/subscriptions/s/proxies", "/api/subscriptions/proxies"} {
		w.srv.Get(path, w.srv.Login()).WantStatus(500).WantJSON(jsonx.Obj("success", false, "message", "获取订阅代理列表失败"))
		if !reflect.DeepEqual(before, normalize(snapshot(t, w.srv))) {
			t.Fatal("missing aggregation dependency changed stored data")
		}
	}
}

func TestMalformedManagementBodies(t *testing.T) {
	w := newWorld(t, seedGraph(t, "none", false, true), nil)
	for _, path := range []string{"/api/nodes/reorder", "/api/subscriptions/reorder", "/api/subscriptions/s/fetch"} {
		for _, body := range []string{`[1]`, `"invalid"`, `true`, `1`} {
			w.call("POST", path, body, true)
		}
	}
}

func TestEveryGlobalRouteRequiresJWT(t *testing.T) {
	w := newWorld(t, seedGraph(t, "none", false, true), nil)
	tests := []struct{ method, path string }{
		{"GET", "/api/nodes"}, {"POST", "/api/nodes"},
		{"PUT", "/api/nodes/relay"}, {"DELETE", "/api/nodes/relay"},
		{"POST", "/api/nodes/delete"}, {"POST", "/api/nodes/delete-preview"},
		{"POST", "/api/nodes/parse-lines"}, {"POST", "/api/nodes/reorder"},
		{"GET", "/api/nodes/latency"}, {"POST", "/api/nodes/latency"},
		{"GET", "/api/nodes/latency/settings"}, {"POST", "/api/nodes/latency/settings"},
		{"POST", "/api/nodes/latency/dns/check"},
		{"GET", "/api/subscriptions"}, {"POST", "/api/subscriptions"},
		{"PUT", "/api/subscriptions/s"}, {"DELETE", "/api/subscriptions/s"},
		{"POST", "/api/subscriptions/reorder"}, {"GET", "/api/subscriptions/health"},
		{"GET", "/api/subscriptions/s/health-impact"}, {"GET", "/api/subscriptions/s/nodes"},
		{"POST", "/api/subscriptions/s/fetch"}, {"GET", "/api/subscriptions/test"},
		{"GET", "/api/subscriptions/proxies"}, {"GET", "/api/subscriptions/s/proxies"},
	}
	for _, tt := range tests {
		got := w.call(tt.method, tt.path+"?token=c30-pull", "", false)
		if got.Status != 401 {
			t.Fatalf("%s %s accepted URL capability without JWT", tt.method, tt.path)
		}
	}
}

func TestFetchWithNullCacheNodes(t *testing.T) {
	w := newWorld(t, func(ctx context.Context, r *repo.Repository) {
		seedGraph(t, "none", false, false)(ctx, r)
		_, err := fixtureSaveSubscriptionNodes(ctx, "s", nil, nil)
		must(t, err)
	}, nil)
	w.control.Offline = true
	w.call("POST", "/api/subscriptions/s/fetch", `{"preview":true}`, true)
	w.call("POST", "/api/subscriptions/s/fetch", `{}`, true)
}

func TestUTF8BOMRequestBodies(t *testing.T) {
	w := newWorld(t, seedGraph(t, "none", false, true), nil)
	w.control.Offline = true
	w.call("POST", "/api/nodes", "\ufeff"+`{"id":"bom","name":"BOM"}`, true)
	w.call("PUT", "/api/nodes/bom", "\ufeff"+`{"name":"Updated BOM"}`, true)
	w.call("POST", "/api/nodes/reorder", "\ufeff"+`{"ids":["bom"]}`, true)
	w.call("POST", "/api/nodes/parse-lines", "\ufeff"+`{"lines":[]}`, true)
	w.call("PUT", "/api/subscriptions/s", "\ufeff"+`{"name":"Feed","url":"https://feed.example/s"}`, true)
	w.call("POST", "/api/subscriptions/reorder", "\ufeff"+`{"ids":["s"]}`, true)
	w.call("POST", "/api/subscriptions/s/fetch", "\ufeff"+`{"preview":true}`, true)
	// Step B #1: null is now rejected before effects; Python treated it as {}.
	before := normalize(snapshot(t, w.srv))
	w.srv.Post("/api/subscriptions/s/fetch", httpxtest.Body([]byte("\ufeffnull"), "application/json"), httpxtest.Bearer(strings.TrimPrefix(w.token, "Bearer "))).WantStatus(400)
	if !reflect.DeepEqual(before, normalize(snapshot(t, w.srv))) {
		t.Fatal("BOM null fetch changed storage")
	}
}

func fixtureSaveSubscriptionNodes(ctx context.Context, id string, value any, metadata *jsonx.Object) (*jsonx.Object, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var nodes []model.Node
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return nil, err
	}
	var receipt *substore.CacheMetadata
	if metadata != nil {
		raw, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		receipt = &substore.CacheMetadata{}
		if err := json.Unmarshal(raw, receipt); err != nil {
			return nil, err
		}
	}
	cache, err := substore.SaveCache(ctx, id, nodes, receipt)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return nil, err
	}
	parsed, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	return parsed.(*jsonx.Object), nil
}
