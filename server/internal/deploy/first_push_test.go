package deploy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents/agentfake"
	"configflow/internal/aggregations"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/provider"
	"configflow/internal/substore"
)

func TestFirstPushCachesAndStableContents(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		aggregation, cached, fail, moved, uri bool
	}{
		{name: "subscription-first"}, {name: "aggregation-first", aggregation: true}, {name: "subscription-current", cached: true}, {name: "aggregation-current", aggregation: true, cached: true}, {name: "missing-cache-failure", fail: true}, {name: "moved-fallback", moved: true}, {name: "uri-conversions", aggregation: true, uri: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mihomoenv.Wire()
			aggregations.Wire()
			Wire()
			f := newPublish(t, agentfake.Options{}, nil)
			feed := "hosts:\n  node.example.com: entry.example.org\nproxies:\n  - {name: Remote, type: socks5, server: node.example.com, port: 1080, username: user, password: synthetic-secret, udp: true}\n"
			var mu sync.Mutex
			fetches := map[string]int{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				fetches[r.URL.Path]++
				if content := r.URL.Query().Get("content"); content != "" {
					fetches[content]++
				}
				mu.Unlock()
				if tc.uri && r.Method == "POST" && r.URL.Path == "/api/subs" {
					w.WriteHeader(201)
					_, _ = io.WriteString(w, `{}`)
					return
				}
				if tc.uri && r.Method == "DELETE" {
					_, _ = io.WriteString(w, `{}`)
					return
				}
				if tc.uri && r.URL.Query().Get("content") != "" {
					u, _ := url.Parse(r.URL.Query().Get("content"))
					_, _ = fmt.Fprintf(w, "proxies: [{name: Converted, type: ss, server: %s, port: 8388, cipher: aes-128-gcm, password: synthetic-secret}]\n", u.Hostname())
					return
				}
				if (tc.fail && r.URL.Path == "/selected") || r.URL.Path == "/moved" || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/download/") {
					w.WriteHeader(503)
					return
				}
				_, _ = io.WriteString(w, feed)
			}))
			defer srv.Close()
			env := substore.NewEnv()
			env.DataDir = func() string { return f.m.Repository().DataDir() }
			env.LookupEnv = func(key string) (string, bool) {
				switch key {
				case "SUB_STORE_URL":
					return srv.URL, true
				case "SUB_STORE_BUILTIN_DIR":
					return "/nonexistent-a14-substore", true
				}
				return "", false
			}
			restoreService := substore.SetDefault(substore.NewService(env))
			defer restoreService()
			restore := provider.SetDeliveryEnv(nil)
			defer restore()
			rawSource, err := f.source.Raw()
			if err != nil {
				t.Fatal(err)
			}
			var cfg map[string]any
			if err := json.Unmarshal(rawSource, &cfg); err != nil {
				t.Fatal(err)
			}
			cfg["subscriptions"] = []any{map[string]any{"id": "selected-sub", "name": "Feed", "url": srv.URL + "/selected", "enabled": true}}
			group := map[string]any{"id": "group", "name": "Proxy", "type": "select", "subscriptions": []any{"selected-sub"}}
			cfg["proxy_groups"] = []any{group}
			cfg["mihomo"] = map[string]any{"custom_config": "mode: rule\ndns:\n  enable: true\n  listen: 0.0.0.0:1053\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\n  nameserver: [192.0.2.53]\n"}
			if tc.aggregation {
				cfg["subscription_aggregations"] = []any{map[string]any{"id": "aggregate", "name": "Aggregate", "enabled": true, "subscriptions": []any{"selected-sub"}, "nodes": []any{}}}
				group["subscriptions"] = []any{}
				group["aggregations"] = []any{"aggregate"}
			}
			if tc.uri {
				cfg["nodes"] = []any{map[string]any{"id": "inline-node", "name": "Inline", "proxy_string": "ss://inline@192.0.2.41:8388", "enabled": true}, map[string]any{"id": "member-node", "name": "Member", "proxy_string": "ss://member@192.0.2.42:8388", "enabled": true}}
				group["manual_nodes"] = []any{"inline-node"}
				cfg["subscription_aggregations"].([]any)[0].(map[string]any)["nodes"] = []any{"member-node"}
			}
			if tc.cached {
				pre, e := PrepareContent(f.ctx, f.m, mustTarget(t, f), testSource(t, cfg), PrepareOptions{BaseURL: "https://panel.example.com", DeploymentID: "warm", StoreDownloads: true})
				if e != nil {
					t.Fatal(e)
				}
				if e = provider.CommitArtifacts(f.ctx, "default", pre.Providers); e != nil {
					t.Fatal(e)
				}
				mu.Lock()
				fetches = map[string]int{}
				mu.Unlock()
			}
			if tc.moved {
				old := map[string]any{"id": "other-sub", "name": "Other", "url": srv.URL + "/old", "enabled": true}
				oldCfg := cloneMap(t, cfg)
				oldCfg["subscriptions"] = []any{old}
				oldCfg["proxy_groups"] = []any{map[string]any{"id": "g", "name": "Proxy", "type": "select", "subscriptions": []any{"other-sub"}}}
				_, e := PrepareContent(f.ctx, f.m, mustTarget(t, f), testSource(t, oldCfg), PrepareOptions{BaseURL: "https://panel.example.com", StoreDownloads: true})
				if e != nil {
					t.Fatal(e)
				}
				old["url"] = srv.URL + "/moved"
				cfg["subscriptions"] = append(cfg["subscriptions"].([]any), old)
				group["subscriptions"] = []any{"selected-sub", "other-sub"}
				mu.Lock()
				fetches = map[string]int{}
				mu.Unlock()
			}
			source := testSource(t, cfg)
			var progressMu sync.Mutex
			var unforced, forced []string
			progress := Progress{
				Phase:   func(m string) { progressMu.Lock(); forced = append(forced, m); progressMu.Unlock() },
				Message: func(m string) { progressMu.Lock(); unforced = append(unforced, m); progressMu.Unlock() },
			}
			p, e := PrepareContent(f.ctx, f.m, mustTarget(t, f), source, PrepareOptions{BaseURL: "https://panel.example.com", DeploymentID: "first", StoreDownloads: true, Progress: progress})
			// A14 review L1: per-provider details are throttled like Python's
			// unforced phase_progress; only the graph validation is forced.
			progressMu.Lock()
			for _, m := range forced {
				if strings.Contains(m, "已准备") || strings.HasPrefix(m, "正在准备") {
					t.Error("provider detail forced", m)
				}
			}
			if !tc.fail && len(unforced) == 0 {
				t.Error("provider details missing")
			}
			progressMu.Unlock()
			if tc.fail {
				if e == nil || !IsPreparationFailure(e) {
					t.Fatal("missing cache accepted", e)
				}
				if cache, _ := substore.LoadCache(f.ctx, "selected-sub"); cache != nil {
					t.Fatal("failed download committed cache")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = provider.CommitArtifacts(f.ctx, "default", p.Providers); e != nil {
				t.Fatal(e)
			}
			cache, e := substore.LoadCache(f.ctx, "selected-sub")
			if e != nil || cache == nil {
				t.Fatal("download not saved", e)
			}
			cacheRaw, _ := json.Marshal(cache)
			first, e := p.Bundle.Files()
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(string(first["config.yaml"]), "entry.example.org") {
				t.Fatal("saved subscription hosts missing from final main")
			}
			if tc.uri {
				mu.Lock()
				one, two := fetches["ss://inline@192.0.2.41:8388"], fetches["ss://member@192.0.2.42:8388"]
				mu.Unlock()
				if one != 1 || two != 1 {
					t.Fatalf("URI conversion was not reused through rerender: inline=%d member=%d", one, two)
				}
			}
			candidate, e := PrepareContentBundle(f.ctx, f.m, mustTarget(t, f), source, "https://panel.example.com", "check-first")
			if e != nil {
				t.Fatal(e)
			}
			after, e := candidate.Files()
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(first, after) {
				t.Fatal("first publication differs from immediate content check")
			}
			a, _ := Fingerprint(p.Bundle.Manifest())
			b, _ := Fingerprint(candidate.Manifest())
			if a != b {
				t.Fatal("first push is not synced")
			}
			cache, e = substore.LoadCache(f.ctx, "selected-sub")
			if e != nil {
				t.Fatal(e)
			}
			checkRaw, _ := json.Marshal(cache)
			if string(cacheRaw) != string(checkRaw) {
				t.Fatal("content comparison changed cache")
			}
			mu.Lock()
			counts := map[string]int{}
			for k, v := range fetches {
				counts[k] = v
			}
			mu.Unlock()
			want := 1
			if tc.cached {
				want = 0
			}
			if counts["/selected"] != want || counts["/moved"] > 1 {
				t.Fatal("subscription refetched during rerender/check", counts)
			}
			if tc.aggregation {
				path, _ := f.m.Repository().ProfilePath("default", "providers/aggregate.yaml")
				if _, e = os.Stat(path); e != nil {
					t.Fatal("aggregation provider not committed", e)
				}
			}
		})
	}
}
func mustTarget(t *testing.T, f *publishFixture) Target {
	t.Helper()
	target, ok, e := targetByID(f.ctx, f.m, "publish-agent")
	if e != nil || !ok {
		t.Fatal(e)
	}
	target.DeploymentMethod = "docker"
	return target
}
