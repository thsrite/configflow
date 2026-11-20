package difftest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
)

func dnsSeed(t *testing.T, native, locked bool) func(context.Context, *repo.Repository) {
	t.Helper()
	return func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		if err := r.UpdateSystem(ctx, func(_ context.Context, p *model.System) error {
			p.SystemConfig.ServerDomain = "http://localhost"
			p.SystemConfig.RuleProxyToken = "c33-proxy-token-0123456789abcdef"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		err := r.UpdateShared(ctx, func(_ context.Context, p *model.Shared) error {
			if err := p.SetJSON("rule_library", json.RawMessage(`[{"id":"library","name":"Domains","source_type":"content","content":"DOMAIN,service.example.com","behavior":"classical","enabled":true}]`)); err != nil {
				return err
			}
			rawNodes, err := json.Marshal(p.Nodes)
			if err != nil {
				return err
			}
			var nodes []any
			if err := json.Unmarshal(rawNodes, &nodes); err != nil {
				return err
			}
			nodes = append(nodes, map[string]any{"id": "unused", "name": "Unused node", "type": "http", "server": "proxy.example.com", "port": 8080})
			merged, err := json.Marshal(nodes)
			if err != nil {
				return err
			}
			return p.SetJSON("nodes", merged)
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = r.UpdateProfile(ctx, "other", func(_ context.Context, p *model.Profile) error {
			if err := p.SetJSON("proxy_groups", json.RawMessage(`[{"id":"source","name":"Source","type":"select","manual_nodes":["DIRECT","node"],"proxies_order":[{"type":"node","id":"DIRECT"},{"type":"node","id":"node"}]},{"id":"proxy","name":"Proxy","type":"select","manual_nodes":["node"]},{"id":"following","name":"Following","type":"select","follow_group":"source"},{"id":"nested","name":"Nested","type":"select","include_groups":["following"]}]`)); err != nil {
				return err
			}
			if err := p.SetJSON("rule_configs", json.RawMessage(`[{"id":"followed","itemType":"rule","rule_type":"DOMAIN","value":"followed.example.com","policy":"Following","enabled":true},{"id":"nested","itemType":"ruleset","library_rule_id":"library","policy":"Nested","enabled":true},{"id":"fixed","itemType":"rule","rule_type":"DOMAIN","value":"fixed.example.com","policy":"Following","enabled":true},{"id":"blocked","itemType":"rule","rule_type":"DOMAIN","value":"blocked.example.com","policy":"REJECT","enabled":true}]`)); err != nil {
				return err
			}
			return p.SetJSON("oxidns", json.RawMessage(`{"direct_rules":["followed","fixed"],"direct_rulesets":["nested"],"rule_dns_follow":["followed","nested"],"rule_actions":{"blocked":{"action":"blackhole"}}}`))
		})
		if err != nil {
			t.Fatal(err)
		}
		if native {
			_, err = r.UpdateProfile(ctx, "other", func(_ context.Context, p *model.Profile) error {
				config, err := r.Config(ctx, "other", repo.ResourceRoots{})
				if err != nil {
					return err
				}
				rawConfig, err := json.Marshal(config)
				if err != nil {
					return err
				}
				view, err := oxidns.ProfileFromJSON(rawConfig)
				if err != nil {
					return err
				}
				settings, err := oxidns.PrepareEditor(view.Settings(), view, "http://localhost")
				if err != nil {
					return err
				}
				if locked {
					var raw map[string]any
					if err := json.Unmarshal(oxidns.ToJSON(settings), &raw); err != nil {
						return err
					}
					text, _ := raw["native_config"].(string)
					changed := strings.NewReplacer("mode: 'null'", "mode: nxdomain", `mode: "null"`, "mode: nxdomain").Replace(text)
					if changed == text {
						t.Fatal("locked fixture did not modify native routing")
					}
					raw["native_config"] = changed
					data, err := json.Marshal(raw)
					if err != nil {
						return err
					}
					settings, err = oxidns.FromJSON(data)
					if err != nil {
						return err
					}
				}
				return p.SetJSON("oxidns", oxidns.ToJSON(settings))
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
		profile, err := w.srv.Repo.Profile(w.srv.Context(), "other")
		if err != nil {
			t.Fatal(err)
		}
		rawOxi, err := json.Marshal(profile.OxiDNS)
		if err != nil {
			t.Fatal(err)
		}
		var oxi struct {
			NativeConfig string `json:"native_config"`
		}
		if err := json.Unmarshal(rawOxi, &oxi); err != nil {
			t.Fatal(err)
		}
		draft := oxi.NativeConfig
		if !strings.Contains(draft, address) && !strings.Contains(draft, url.QueryEscape(address)) {
			t.Fatal("native source rewrite was not observable")
		}
	}
	w.call("DELETE", "/api/rule-library/library", "", true)
}
func TestGroupListenerRelinkingHTTPParity(t *testing.T) {
	setup := func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		_, err := r.UpdateProfile(ctx, "other", func(_ context.Context, p *model.Profile) error {
			p.Mihomo.CustomConfig = "# user heading\nlisteners:\n  - name: local\n    type: mixed\n    port: 7890\n    proxy: Group # keep comment\n"
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	w := newWorld(t, setup)
	w.call("PUT", "/api/proxy-groups/group?profile=other", `{"name":"Renamed"}`, true)
	w.call("PUT", "/api/proxy-groups/group?profile=other", `{"enabled":false}`, true)
}
