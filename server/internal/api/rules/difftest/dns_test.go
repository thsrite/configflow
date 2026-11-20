package difftest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
)

func nativeSeed(t *testing.T, custom bool) func(context.Context, *repo.Repository) {
	t.Helper()
	return func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		if _, err := r.UpdateProfile(ctx, "other", func(_ context.Context, p *model.Profile) error {
			config, err := r.Config(ctx, "other", repo.ResourceRoots{})
			if err != nil {
				return err
			}
			rawConfig, err := json.Marshal(config)
			if err != nil {
				return err
			}
			if custom {
				var cfg map[string]any
				if err := json.Unmarshal(rawConfig, &cfg); err != nil {
					return err
				}
				oxi, _ := cfg["oxidns"].(map[string]any)
				if oxi == nil {
					oxi = map[string]any{}
					cfg["oxidns"] = oxi
				}
				oxi["rule_actions"] = map[string]any{"proxied": map[string]any{"action": "blackhole"}}
				rawConfig, err = json.Marshal(cfg)
				if err != nil {
					return err
				}
			}
			view, err := oxidns.ProfileFromJSON(rawConfig)
			if err != nil {
				return err
			}
			settings, err := oxidns.PrepareEditor(view.Settings(), view, "http://localhost")
			if err != nil {
				return err
			}
			if custom {
				var raw map[string]any
				if err := json.Unmarshal(oxidns.ToJSON(settings), &raw); err != nil {
					return err
				}
				text, _ := raw["native_config"].(string)
				changed := strings.NewReplacer("mode: 'null'", "mode: nxdomain", `mode: "null"`, "mode: nxdomain").Replace(text)
				if text == changed {
					t.Fatal("fixture did not customize blackhole plugin")
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
		}); err != nil {
			t.Fatal(err)
		}
	}
}
func TestTemplateAndNativeDNSWrites(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "template"
		setup := seed(t)
		if native {
			name = "native"
			setup = nativeSeed(t, false)
		}
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, setup)
			for _, c := range []struct{ method, path, body string }{
				{"GET", "/api/rules/dns", ""},
				{"PUT", "/api/rules/dns", `{"routes":{"inline":"follow","set":"follow","off":"refused","proxied":"blackhole"}}`},
				{"PUT", "/api/rules/inline", `{"policy":"PROXY"}`},
				{"PUT", "/api/rules/policy", `{"ids":["inline","set"],"policy":"REJECT","group_name":" Home "}`},
				{"PUT", "/api/rules/proxied", `{"rule_type":"DST-PORT","value":"443"}`},
				{"PUT", "/api/rules/proxied", `{"rule_type":"DST-PORT","value":"443","dns":"default"}`},
				{"PUT", "/api/rules/inline", `{"enabled":false}`},
				{"PUT", "/api/rules/inline", `{"enabled":true}`},
				{"PUT", "/api/rules/dns", `{"routes":{"geo":"blackhole","inline":"direct"}}`},
				{"PUT", "/api/rules/dns", `{"routes":{"geo":"blackhole","inline":"direct"},"skip_unsupported":true}`},
				{"PUT", "/api/rules/dns", `{"routes":{"inline":"upstream:0123456789ab"}}`},
				{"POST", "/api/rules/reorder", `{"ids":["inline"],"position":"bottom"}`},
				{"POST", "/api/rules/compose", `{"source":{"kind":"values","text":"new.example\nIP-CIDR,192.0.2.0/24"},"rule":{"policy":"DIRECT"},"dns":"blackhole","dns_ip":"default"}`},
				{"POST", "/api/rules/compose", `{"source":{"kind":"library","library_rule_ids":["mixed"]},"rule":{"policy":"PROXY"},"dns":"refused"}`},
				{"DELETE", "/api/rules/set", ""}, {"DELETE", "/api/rules/off", ""}, {"DELETE", "/api/rules/inline", ""},
				{"GET", "/api/rules/dns", ""},
			} {
				w.call(c.method, c.path+"?profile=other", c.body, true)
			}
		})
	}
}
func TestCustomNativeRoutingPreservesCommittedMetadata(t *testing.T) {
	w := newWorld(t, nativeSeed(t, true))
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/rules/dns", ""},
		{"PUT", "/api/rules/inline", `{"policy":"PROXY"}`},
		{"PUT", "/api/rules/inline", `{"remark":"annotation"}`},
		{"PUT", "/api/rules/policy", `{"ids":["inline","off"],"policy":"PROXY"}`},
		{"PUT", "/api/rules/inline", `{"dns":"refused","policy":"DIRECT"}`},
		{"PUT", "/api/rules/dns", `{"routes":{"inline":"refused"}}`},
		{"POST", "/api/rules/compose", `{"source":{"kind":"values","text":"new.example"},"rule":{"policy":"DIRECT"},"dns":"direct"}`},
		{"POST", "/api/rules/compose", `{"source":{"kind":"values","text":"default.example"},"rule":{"policy":"DIRECT"},"dns":"default"}`},
	} {
		w.call(c.method, c.path+"?profile=other", c.body, true)
	}
}
func TestMosDNSGuardAndCleanup(t *testing.T) {
	setup := func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		if _, err := r.UpdateProfile(ctx, "other", func(_ context.Context, p *model.Profile) error {
			p.MosDNS.ProxyRules = []string{"inline"}
			p.MosDNS.DirectRulesets = []string{"set"}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	w := newWorld(t, setup)
	w.call("PUT", "/api/rules/inline?profile=other", `{"rule_type":"GEOIP","value":"CN"}`, true)
	w.call("PUT", "/api/rules/inline?profile=other", `{"rule_type":"IP-CIDR","value":"192.0.2.0/24"}`, true)
	w.call("GET", "/api/rules/dns?profile=other", "", true)
	w.call("DELETE", "/api/rules/inline?profile=other", "", true)
	w.call("DELETE", "/api/rules/set?profile=other", "", true)
}

func TestDNSAgentReadModelLegacyNames(t *testing.T) {
	for _, name := range []any{"OxiDNS 主路由", nil, 0, 42, []any{"legacy"}} {
		t.Run(body(name), func(t *testing.T) {
			setup := func(ctx context.Context, r *repo.Repository) {
				seed(t)(ctx, r)
				err := r.UpdateSystem(ctx, func(_ context.Context, p *model.System) error {
					raw, err := json.Marshal([]any{
						map[string]any{"id": "dns", "name": name, "service_type": "oxidns", "enabled": true, "profile_id": "other", "rule_sync": map[string]any{"settings": map[string]any{"library": true}}},
						map[string]any{"id": "off", "name": "Off", "service_type": "oxidns", "enabled": false, "profile_id": "other"},
						map[string]any{"id": "other-profile", "name": "Elsewhere", "service_type": "mihomo", "enabled": true, "profile_id": "default"},
					})
					if err != nil {
						return err
					}
					return p.SetJSON("agents", raw)
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			w := newWorld(t, setup)
			w.call("GET", "/api/rules/dns?profile=other", "", true)
		})
	}
}

func TestPinnedUpstreamChoicesAndLinkedRejection(t *testing.T) {
	for _, linked := range []bool{false, true} {
		name := "free"
		if linked {
			name = "linked"
		}
		t.Run(name, func(t *testing.T) {
			setup := func(ctx context.Context, r *repo.Repository) {
				seed(t)(ctx, r)
				if linked {
					err := r.UpdateSystem(ctx, func(_ context.Context, p *model.System) error {
						return p.SetJSON("agents", json.RawMessage(`[{"id":"dns","name":"DNS","enabled":true,"service_type":"oxidns","profile_id":"other","dns_follow":{"upstream_tag":"forward_local","upstream_index":0}}]`))
					})
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			w := newWorld(t, setup)
			state := w.call("GET", "/api/rules/dns?profile=other", "", true)
			key := ""
			for _, value := range state.Body.(map[string]any)["upstreams"].([]any) {
				row := value.(map[string]any)
				if row["source"] == "local" {
					key = row["key"].(string)
					if row["linked"] != linked {
						t.Fatal("wrong binding")
					}
					break
				}
			}
			if key == "" {
				t.Fatal("missing local upstream")
			}
			w.call("PUT", "/api/rules/dns?profile=other", body(map[string]any{"routes": map[string]string{"inline": "upstream:" + key}}), true)
			w.call("GET", "/api/rules/dns?profile=other", "", true)
		})
	}
}
