package difftest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
)

func nativeSeed(t *testing.T, custom bool) func(context.Context, *repo.Repository) {
	t.Helper()
	return func(ctx context.Context, r *repo.Repository) {
		seed(t)(ctx, r)
		if _, _, err := r.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			config, err := r.GetCompatConfig(ctx, "other", repo.ResourceRoots{})
			if err != nil {
				return nil, err
			}
			if custom {
				config.GetObject("oxidns").Set("rule_actions", jsonx.Obj("proxied", jsonx.Obj("action", "blackhole")))
			}
			view := oxidns.NewProfile(config)
			settings, err := oxidns.PrepareEditor(view.Settings(), view, "http://localhost")
			if err != nil {
				return nil, err
			}
			if custom {
				text := oxidns.ToJSONX(settings).(*jsonx.Object).GetString("native_config")
				changed := strings.NewReplacer("mode: 'null'", "mode: nxdomain", `mode: "null"`, "mode: nxdomain").Replace(text)
				if text == changed {
					t.Fatal("fixture did not customize blackhole plugin")
				}
				raw := oxidns.ToJSONX(settings).(*jsonx.Object)
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
		if _, _, err := r.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			p.GetObject("mosdns").Set("proxy_rules", []any{"inline"})
			p.GetObject("mosdns").Set("direct_rulesets", []any{"set"})
			return nil, nil
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
				_, err := r.UpdateSystemTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
					p.Set("agents", []any{jsonx.Obj("id", "dns", "name", name, "service_type", "oxidns", "enabled", true, "profile_id", "other", "rule_sync", jsonx.Obj("settings", jsonx.Obj("library", true))), jsonx.Obj("id", "off", "name", "Off", "service_type", "oxidns", "enabled", false, "profile_id", "other"), jsonx.Obj("id", "other-profile", "name", "Elsewhere", "service_type", "mihomo", "enabled", true, "profile_id", "default")})
					return nil, nil
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
					_, err := r.UpdateSystemTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
						p.Set("agents", []any{jsonx.Obj("id", "dns", "name", "DNS", "enabled", true, "service_type", "oxidns", "profile_id", "other", "dns_follow", jsonx.Obj("upstream_tag", "forward_local", "upstream_index", 0))})
						return nil, nil
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
