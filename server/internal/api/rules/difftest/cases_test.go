package difftest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func seed(t *testing.T) func(context.Context, *repo.Repository) {
	t.Helper()
	return func(ctx context.Context, r *repo.Repository) {
		if err := r.UpdateShared(ctx, func(_ context.Context, p *model.Shared) error {
			return p.SetJSON("rule_library", json.RawMessage(`[{"id":"ads","name":"Ads","source_type":"content","behavior":"domain","content":"+.ads.example\n","enabled":true},{"id":"mixed","name":"Mixed","source_type":"content","behavior":"classical","format":"text","content":"DOMAIN,Example.com\nDOMAIN,example.com\nIP-CIDR,192.0.2.0/24\nREGEX,^Foo$\n","enabled":true},{"id":"cn","name":"CN","source_type":"content","behavior":"domain","content":"cn.example\n","enabled":true},{"id":"empty","name":"Empty","source_type":"content","content":""}]`))
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
			if err := p.SetJSON("proxy_groups", json.RawMessage(`[{"id":"group","name":"PROXY","type":"select"}]`)); err != nil {
				return err
			}
			return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"inline","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":"DIRECT","enabled":true,"future":{"keep":true}},{"id":"set","itemType":"ruleset","library_rule_id":"cn","policy":"DIRECT","enabled":true},{"id":"proxied","itemType":"rule","rule_type":"DOMAIN-SUFFIX","value":"proxied.example","policy":"PROXY","enabled":true},{"id":"off","itemType":"rule","rule_type":"DOMAIN","value":"off.example","policy":"DIRECT","enabled":false},{"id":"geo","itemType":"rule","rule_type":"GEOIP","value":"CN","policy":"DIRECT","enabled":true}]`))
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "other", Name: "Other"}, "default"); err != nil {
			t.Fatal(err)
		}
		if err := r.UpdateSystem(ctx, func(_ context.Context, p *model.System) error {
			p.SystemConfig.ServerDomain = "http://panel.example"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
func body(value any) string { raw, _ := json.Marshal(value); return string(raw) }
func TestEveryRouteSuccessAndFailureWithStorage(t *testing.T) {
	w := newWorld(t, seed(t))
	calls := []struct{ method, path, body string }{
		{"GET", "/api/rules", ""}, {"GET", "/api/profiles/other/rules", ""}, {"GET", "/api/rule-sets", ""},
		{"POST", "/api/rules", `{"id":"new","rule_type":"DOMAIN","value":"new.example","policy":"DIRECT"}`},
		{"POST", "/api/profiles/other/rules", `{"id":"othernew","rule_type":"IP-CIDR","value":"192.0.2.0/24","policy":"DIRECT"}`},
		{"POST", "/api/rule-sets", `{"id":"mixedset","library_rule_id":"mixed","policy":"PROXY","name":"spoof","url":"http://ignored.example","content":"ignored"}`},
		{"PUT", "/api/rules/new", `{"remark":"标签","enabled":false}`},
		{"PUT", "/api/rule-sets/mixedset", `{"remark":"规则集"}`},
		{"POST", "/api/rules/batch", `{"rule_type":"DOMAIN","domains":["batch.example"," batch2.example "],"policy":"DIRECT"}`},
		{"POST", "/api/rules/group-name", `{"ids":["new","inline"],"group_name":" Group "}`},
		{"PUT", "/api/rules/policy", `{"ids":["new","inline"],"policy":"PROXY","group_name":""}`},
		{"POST", "/api/rules/reorder", `{"ids":["proxied","inline"],"position":"bottom"}`},
		{"POST", "/api/rules/reorder", `{"rule_configs":[{"id":"inline","policy":"ignored"}],"position":"top"}`},
		{"GET", "/api/rules/dns", ""},
		{"PUT", "/api/rules/dns", `{"routes":{"inline":"direct","proxied":"proxy","geo":"default"}}`},
		{"PUT", "/api/rules/inline", `{"dns":"follow","policy":"PROXY"}`},
		{"GET", "/api/rules/dns", ""},
		{"GET", "/api/rules/local/Ads", ""}, {"GET", "/api/profiles/other/rules/local/Mixed?target=mihomo", ""},
		{"POST", "/api/rules/match-test", `{"query":"Example.com."}`},
		{"POST", "/api/rules/find-duplicates", ""},
		{"DELETE", "/api/rule-sets/mixedset", ""}, {"DELETE", "/api/rules/new", ""},
		{"GET", "/api/rules/local/Absent", ""}, {"GET", "/api/profiles/other/rules/local/Absent", ""},
		{"PUT", "/api/rules/absent", `{}`}, {"DELETE", "/api/rules/absent", ""}, {"PUT", "/api/rule-sets/inline", `{}`}, {"DELETE", "/api/rule-sets/inline", ""},
		{"POST", "/api/rules", `[]`}, {"POST", "/api/profiles/other/rules", `{"rule_type":"DOMAIN","value":""}`}, {"POST", "/api/rule-sets", `{"id":"bad","library_rule_id":"absent"}`},
		{"POST", "/api/rules/batch", `{"domains":[]}`}, {"POST", "/api/rules/group-name", `{"ids":["absent"],"group_name":"x"}`}, {"PUT", "/api/rules/policy", `{"ids":["inline"],"policy":"absent"}`},
		{"POST", "/api/rules/reorder", `{"ids":["absent"]}`}, {"PUT", "/api/rules/dns", `{"routes":{"inline":"invalid"}}`},
		{"POST", "/api/rules/match-test", `{"query":"invalid name"}`},
	}
	for i, c := range calls {
		t.Run(fmt.Sprintf("%02d_%s_%s", i, c.method, c.path), func(t *testing.T) { w.t = t; w.call(c.method, c.path, c.body, true) })
	}
	for _, c := range []struct{ method, path string }{{"GET", "/api/rules"}, {"GET", "/api/rule-sets"}, {"GET", "/api/profiles/other/rules"}, {"POST", "/api/rules/find-duplicates"}, {"POST", "/api/rules/compose"}, {"GET", "/api/rules/dns"}} {
		w.t = t
		w.call(c.method, c.path, `{}`, false)
	}
}
func TestComposeSourcesDNSAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name         string
		source, rule any
		dns          string
		extra        map[string]any
	}{
		{"values", map[string]any{"kind": "values", "text": "# ignored\n+.new.example\n*.other.example\nfull:full.example\nDOMAIN,example.com\nDOMAIN,off.example"}, nil, "direct", nil},
		{"mixed", map[string]any{"kind": "values", "text": "DOMAIN,new.example\nIP-CIDR,192.0.2.0/24\nGEOIP,CN"}, nil, "default", nil},
		{"library", map[string]any{"kind": "library", "library_rule_ids": []string{"ads", "mixed"}}, nil, "follow", nil},
		{"existing", map[string]any{"kind": "library", "library_rule_ids": []string{"cn"}}, nil, "default", nil},
		{"content", map[string]any{"kind": "new_library", "library": map[string]any{"id": "spoof", "name": "New source", "source_type": "content", "behavior": "classical", "format": "text", "content": "full:new.example\n+.other.example\nDOMAIN,third.example"}}, nil, "direct", nil},
		{"regex", map[string]any{"kind": "values", "text": `DOMAIN-REGEX,^node[0-9]{1,3}\.example$`}, nil, "default", nil},
		{"match", map[string]any{"kind": "values", "text": "MATCH,REJECT"}, nil, "default", nil},
		{"unknown-policy", map[string]any{"kind": "new_library", "library": map[string]any{"name": "Never saved", "source_type": "content", "behavior": "domain", "content": "new.example"}}, map[string]any{"policy": "absent"}, "default", nil},
		{"invalid-kind", map[string]any{"kind": "invalid"}, nil, "default", nil},
		{"invalid-position", map[string]any{"kind": "values", "text": "new.example"}, nil, "default", map[string]any{"position": "invalid"}},
		{"limit", map[string]any{"kind": "values", "text": strings.Repeat("a.example\n", 201)}, nil, "default", nil},
		{"invalid-dns", map[string]any{"kind": "values", "text": "new.example"}, nil, "invalid", nil},
		{"unconvertible", map[string]any{"kind": "values", "text": "DST-PORT,443"}, nil, "direct", nil},
		{"field-errors", map[string]any{"kind": "values", "text": "DOMAIN-SHUFFIX,example.com\nIP-CIDR,invalid\nDOMAIN,"}, nil, "default", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, seed(t))
			r := tc.rule
			if r == nil {
				r = map[string]any{"policy": "DIRECT"}
			}
			request := map[string]any{"source": tc.source, "rule": r, "dns": tc.dns}
			for key, value := range tc.extra {
				request[key] = value
			}
			w.call("POST", "/api/rules/compose?profile=other", body(request), true)
			w.call("GET", "/api/rules/dns?profile=other", "", true)
		})
	}
}
func TestSyntaxValidationAcrossWriteEntries(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, tc := range []struct{ kind, value string }{{"DOMAIN-SHUFFIX", "example.com"}, {"domain-shuffix", "example.com"}, {"DOMAIN", ""}, {"DOMAIN-SUFFIX", "+.example.com"}, {"DOMAIN", "999.999.999.999"}, {"IP-CIDR", "+.example.com"}, {"IP-CIDR", "192.0.2.1/33"}, {"IP-CIDR6", ":::/64"}, {"DST-PORT", "abc"}, {"SRC-PORT", "65536"}, {"DST-PORT", "80,443"}, {"DOMAIN-KEYWORD", "foo,bar"}, {"PROCESS-NAME", "app,DIRECT"}} {
		for _, entry := range []string{"create", "edit", "compose", "batch"} {
			t.Run(tc.kind+tc.value+entry, func(t *testing.T) {
				w.t = t
				method, path := "POST", "/api/rules?profile=other"
				request := map[string]any{"id": "invalid", "rule_type": tc.kind, "value": tc.value, "policy": "DIRECT"}
				switch entry {
				case "edit":
					method, path = "PUT", "/api/rules/inline?profile=other"
				case "batch":
					path = "/api/rules/batch?profile=other"
					request["domains"] = []string{tc.value}
				case "compose":
					path = "/api/rules/compose?profile=other"
					request = map[string]any{"source": map[string]any{"kind": "values", "text": tc.kind + "," + tc.value}, "rule": map[string]string{"policy": "DIRECT"}}
				}
				w.call(method, path, body(request), true)
			})
		}
	}
}
func TestRemoteProviderAndLazyMatching(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.ContentLength > 0 {
			t.Errorf("unexpected request %s %d", r.Method, r.ContentLength)
		}
		switch r.URL.Path {
		case "/valid":
			fmt.Fprint(w, "+.remote.example\n")
		case "/invalid":
			fmt.Fprint(w, "IP-CIDR,192.0.2.0/24\n")
		default:
			http.Error(w, "synthetic unavailable", 503)
		}
	}))
	defer source.Close()
	for _, path := range []string{"valid", "invalid", "offline"} {
		t.Run(path, func(t *testing.T) {
			setup := func(ctx context.Context, r *repo.Repository) {
				seed(t)(ctx, r)
				err := r.UpdateShared(ctx, func(_ context.Context, p *model.Shared) error {
					raw, err := json.Marshal(p.RuleLibrary)
					if err != nil {
						return err
					}
					var items []any
					if err := json.Unmarshal(raw, &items); err != nil {
						return err
					}
					items = append(items, map[string]any{"id": "remote", "name": "Remote", "url": source.URL + "/" + path, "behavior": "domain", "format": "text", "enabled": true})
					merged, err := json.Marshal(items)
					if err != nil {
						return err
					}
					return p.SetJSON("rule_library", merged)
				})
				if err != nil {
					t.Fatal(err)
				}
				_, err = r.UpdateProfile(ctx, "other", func(_ context.Context, p *model.Profile) error {
					return p.SetJSON("rule_configs", json.RawMessage(`[{"id":"first","itemType":"rule","rule_type":"DOMAIN","value":"first.example","policy":"DIRECT"},{"id":"remote","itemType":"ruleset","library_rule_id":"remote","policy":"DIRECT"}]`))
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			w := newWorld(t, setup)
			w.call("POST", "/api/rules/match-test?profile=other", `{"query":"first.example"}`, true)
			w.call("POST", "/api/rules/match-test?profile=other", `{"query":"remote.example"}`, true)
			w.call("GET", "/api/profiles/other/rules/local/Remote?target=mihomo", "", false)
			w.call("GET", "/api/profiles/other/rules/local/Remote", "", false)
			w.call("POST", "/api/rules/find-duplicates?profile=other", "", true)
		})
	}
}

func TestReorderCompatibilityFailuresAndNoClobber(t *testing.T) {
	w := newWorld(t, seed(t))
	for _, request := range []string{`{}`, `[]`, `{"ids":"inline"}`, `{"ids":null}`, `{"rule_configs":[null]}`, `{"rule_configs":[{"id":"missing"}]}`, `{"ids":[42]}`, `{"ids":[null,"missing"]}`, `{"ids":["inline","inline"]}`, `{"ids":[]}`, `{"ids":["inline"],"position":"unexpected"}`} {
		w.call("POST", "/api/rules/reorder?profile=other", request, true)
	}
}
