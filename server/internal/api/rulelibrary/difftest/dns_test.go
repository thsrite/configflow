package difftest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
)

func dnsSeed(t *testing.T, native, locked bool) func(context.Context, *repo.Repository) {
	t.Helper()
	return func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		if _, err := r.UpdateSystemTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.GetObject("system_config").Set("server_domain", "http://localhost")
			p.GetObject("system_config").Set("rule_proxy_token", "c33-proxy-token-0123456789abcdef")
			return nil, nil
		}); err != nil {
			t.Fatal(err)
		}
		_, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.Set("rule_library", []any{jsonx.Obj("id", "library", "name", "Domains", "source_type", "content", "content", "DOMAIN,service.example.com", "behavior", "classical", "enabled", true)})
			p.Set("nodes", append(p.GetList("nodes"), jsonx.Obj("id", "unused", "name", "Unused node", "type", "http", "server", "proxy.example.com", "port", 8080)))
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = r.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.Set("proxy_groups", []any{jsonx.Obj("id", "source", "name", "Source", "type", "select", "manual_nodes", []any{"DIRECT", "node"}, "proxies_order", []any{jsonx.Obj("type", "node", "id", "DIRECT"), jsonx.Obj("type", "node", "id", "node")}), jsonx.Obj("id", "proxy", "name", "Proxy", "type", "select", "manual_nodes", []any{"node"}), jsonx.Obj("id", "following", "name", "Following", "type", "select", "follow_group", "source"), jsonx.Obj("id", "nested", "name", "Nested", "type", "select", "include_groups", []any{"following"})})
			p.Set("rule_configs", []any{jsonx.Obj("id", "followed", "itemType", "rule", "rule_type", "DOMAIN", "value", "followed.example.com", "policy", "Following", "enabled", true), jsonx.Obj("id", "nested", "itemType", "ruleset", "library_rule_id", "library", "policy", "Nested", "enabled", true), jsonx.Obj("id", "fixed", "itemType", "rule", "rule_type", "DOMAIN", "value", "fixed.example.com", "policy", "Following", "enabled", true), jsonx.Obj("id", "blocked", "itemType", "rule", "rule_type", "DOMAIN", "value", "blocked.example.com", "policy", "REJECT", "enabled", true)})
			p.Set("oxidns", jsonx.Obj("direct_rules", []any{"followed", "fixed"}, "direct_rulesets", []any{"nested"}, "rule_dns_follow", []any{"followed", "nested"}, "rule_actions", jsonx.Obj("blocked", jsonx.Obj("action", "blackhole"))))
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if native {
			_, _, err = r.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
				config, err := r.GetCompatConfig(ctx, "other", repo.ResourceRoots{})
				if err != nil {
					return nil, err
				}
				view := oxidns.NewProfile(config)
				settings, err := oxidns.PrepareEditor(view.Settings(), view, "http://localhost")
				if err != nil {
					return nil, err
				}
				if locked {
					raw := oxidns.ToJSONX(settings).(*jsonx.Object)
					text := raw.GetString("native_config")
					changed := strings.NewReplacer("mode: 'null'", "mode: nxdomain", `mode: "null"`, "mode: nxdomain").Replace(text)
					if changed == text {
						t.Fatal("locked fixture did not modify native routing")
					}
					raw.Set("native_config", changed)
					data, err := json.Marshal(raw)
					if err != nil {
						return nil, err
					}
					settings, err = oxidns.FromJSON(data)
					if err != nil {
						return nil, err
					}
				}
				p.Set("oxidns", oxidns.ToJSONX(settings))
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
func TestProxyGroupFollowDNSHTTPParity(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "template"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, dnsSeed(t, native, false))
			for _, request := range []struct{ id, body string }{{"source", `{"proxies_order":[{"type":"node","id":"node"},{"type":"node","id":"DIRECT"}]}`}, {"following", `{"follow_group":"proxy"}`}, {"following", `{"follow_group":"source"}`}, {"source", `{"name":"Renamed source"}`}, {"source", `{"manual_nodes":["unused","DIRECT"],"proxies_order":[{"type":"node","id":"unused"},{"type":"node","id":"DIRECT"}]}`}} {
				w.call("PUT", "/api/proxy-groups/"+request.id+"?profile=other", request.body, true)
			}
		})
	}
	w := newWorld(t, dnsSeed(t, true, true))
	w.call("PUT", "/api/proxy-groups/source?profile=other", `{"name":"Renamed source"}`, true)
	w.call("PUT", "/api/proxy-groups/source?profile=other", `{"proxies_order":[{"type":"node","id":"node"},{"type":"node","id":"DIRECT"}]}`, true)
}
func TestLibraryNativeParkingHTTPParity(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("+.service.example.com\n")) }))
	defer origin.Close()
	w := newWorld(t, dnsSeed(t, true, false))
	for _, enabled := range []bool{false, true} {
		w.call("PUT", "/api/rule-library/library", body(map[string]any{"id": "library", "name": "Domains", "source_type": "content", "behavior": "classical", "content": "DOMAIN,service.example.com", "enabled": enabled}), true)
		w.call("GET", "/api/rules/dns?profile=other", "", true)
	}
	w.call("PUT", "/api/rule-library/library", `{"name":"Domain form","source_type":"content","behavior":"domain","format":"text","content":"+.service.example.com","enabled":true}`, true)
	for _, suffix := range []string{"/first", "/second"} {
		address := origin.URL + suffix
		w.call("PUT", "/api/rule-library/library", body(map[string]any{"name": "Remote form", "source_type": "url", "behavior": "domain", "format": "text", "url": address, "enabled": true}), true)
		profile, err := w.srv.Repo.GetProfile(w.srv.Context(), "other")
		if err != nil {
			t.Fatal(err)
		}
		draft := profile.GetObject("oxidns").GetString("native_config")
		if !strings.Contains(draft, address) && !strings.Contains(draft, url.QueryEscape(address)) {
			t.Fatal("native source rewrite was not observable")
		}
	}
	w.call("DELETE", "/api/rule-library/library", "", true)
}
func TestGroupListenerRelinkingHTTPParity(t *testing.T) {
	setup := func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		_, _, err := r.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.GetObject("mihomo").Set("custom_config", "# user heading\nlisteners:\n  - name: local\n    type: mixed\n    port: 7890\n    proxy: Group # keep comment\n")
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	w := newWorld(t, setup)
	w.call("PUT", "/api/proxy-groups/group?profile=other", `{"name":"Renamed"}`, true)
	w.call("PUT", "/api/proxy-groups/group?profile=other", `{"enabled":false}`, true)
}
