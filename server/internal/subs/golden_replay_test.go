package subs_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
)

func TestSubsChainAndRelinkGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	t.Run("chain-capture-groups", func(t *testing.T) {
		cases := []struct{ Pattern, Name string }{
			{`(a)?(?(1)b|c)`, "ab"},
			{`(a)?(?(1)b|c)`, "c"},
			{"(?x:a # ignored ( parentheses\n(b))", "ab"},
			{"(?x:(a) # ignored ) (?P<ignored>\n(b))", "ab"},
			{`(?P<first>a)(b)`, "ab"},
			{`(?P<first>a)?(?(first)b|c)`, "ab"},
			{`(?P<first>a)?(?(first)b|c)`, "c"},
			{`\(a\)(b)`, "(a)b"},
			{`[()](a)`, "(a"},
			{`(?# ignored opening ()(a)`, "a"},
			{`(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)(l)`, "abcdefghijkl"},
			{`(a)`, "z"},
			{`(a)`, "abz"},
		}
		dollarRef := regexp.MustCompile(`\$[0-9]+`)
		for i, tc := range cases {
			compiled, err := rules.CompilePyRegex(tc.Pattern, false)
			if err != nil {
				t.Fatal(err)
			}
			runtime := map[string]any{
				"nodes": []any{map[string]any{"id": "entry", "name": "Entry"}},
				"proxy_groups": []any{
					map[string]any{"id": "chain", "name": "Chain", "_chain_dialer": map[string]any{"type": "node", "id": "entry"}},
				},
			}
			main := map[string]any{
				"proxy-providers": map[string]any{"Source": map[string]any{"type": "http", "url": "https://example.com/nodes"}},
				"proxy-groups":    []any{map[string]any{"name": "Chain", "type": "select", "use": []any{"Source"}, "filter": tc.Pattern + "`z"}},
			}
			rawConfig, _ := json.Marshal(runtime)
			var profile model.Profile
			var shared model.Shared
			_ = json.Unmarshal(rawConfig, &profile)
			_ = json.Unmarshal(rawConfig, &shared)
			rawMain, _ := json.Marshal(main)
			graph, err := yamlx.FromJSON(rawMain)
			if err != nil {
				t.Fatal(err)
			}
			if err := subs.ChainProviders(&model.Config{Profile: &profile, Nodes: shared.Nodes}, graph); err != nil {
				t.Fatal(err)
			}
			outJSON, err := yamlx.ToJSON(graph)
			if err != nil {
				t.Fatal(err)
			}
			var outMain map[string]any
			_ = json.Unmarshal(outJSON, &outMain)
			renamed := tc.Name
			providers := outMain["proxy-providers"].(map[string]any)
			chainProv := providers["Chain_Source"].(map[string]any)
			override := chainProv["override"].(map[string]any)
			for _, rawRule := range override["proxy-name"].([]any) {
				rule := rawRule.(map[string]any)
				re, err := rules.CompilePyRegex(rule["pattern"].(string), false)
				if err != nil {
					t.Fatal(err)
				}
				target := rule["target"].(string)
				renamed, err = re.Sub(renamed, func(m *rules.Match) (string, error) {
					return dollarRef.ReplaceAllStringFunc(target, func(s string) string {
						n, err := strconv.Atoi(s[1:])
						if err != nil {
							return s
						}
						if g, ok := m.Group(n); ok {
							return g
						}
						return ""
					}), nil
				}, 0)
				if err != nil {
					t.Fatal(err)
				}
			}
			key, _ := json.Marshal(map[string]string{"kind": "chain", "idx": strconv.Itoa(i), "pattern": tc.Pattern, "name": tc.Name})
			val, _ := json.Marshal(map[string]any{
				"groups":  compiled.Groups(),
				"main":    outMain,
				"renamed": renamed,
			})
			ref.Assert(t, key, val)
		}
	})

	t.Run("relink-transaction-boundary", func(t *testing.T) {
		renamed := "Renamed"
		quoted := "new # : \"quoted\""
		on := "on"
		same := "Pinned"
		aliasChain := "pad: [" + strings.Repeat("0, ", 3000) + "0]\nx: &x [" + strings.Repeat("a, ", 50) + "a]\nu: &u [" +
			strings.Repeat("*x, ", 30) + "*x]\nlisteners:\n- name: in\n  proxy: Pinned\n  users: *u\n"
		base := "# header\nfuture: {keep: false}\nlisteners:\n- name: first # name\n  proxy: Pinned # fixed\n  rule: local\n- {name: second, proxy: DIRECT}\n"
		type editCase struct {
			Name      string
			Source    string
			New       *string
			FailAfter bool
		}
		cases := []editCase{
			{Name: "rename", Source: base, New: &renamed},
			{Name: "unpin", Source: base},
			{Name: "quoted name", Source: base, New: &quoted},
			{Name: "YAML 1.1-looking name", Source: base, New: &on},
			{Name: "same name no-op", Source: base, New: &same},
			{Name: "CRLF comments and blank lines", Source: strings.ReplaceAll(strings.Replace(base, "- {name: second", "\n# gap\n- {name: second", 1), "\n", "\r\n"), New: &renamed},
			{Name: "shared listener", Source: "shared: &base {name: first, proxy: Pinned}\nlisteners: [*base]\n", New: &renamed},
			{Name: "shared listener sequence", Source: "listeners: &all [{name: first, proxy: Pinned}]\nother: *all\n", New: &renamed},
			{Name: "merge", Source: "shared: &base {name: first, proxy: Pinned}\nlisteners:\n- <<: *base\n  port: 17890\n", New: &renamed},
			{Name: "rollback after edit", Source: base, New: &renamed, FailAfter: true},
			{Name: "alias chain in listener", Source: aliasChain, New: &renamed},
			{Name: "alias chain in unpinned listener", Source: aliasChain},
			{Name: "merged listener comments", Source: "# top\nshared: &base {name: first, proxy: Pinned} # base\nlisteners:\n- <<: *base # merge\n  port: 17890\n"},
			{Name: "parse diagnostic", Source: "listeners: ["},
			{Name: "listeners wrong type", Source: "listeners: {proxy: Pinned}\n"},
			{Name: "listener wrong type", Source: "listeners: [false]\n"},
			{Name: "target wrong type", Source: "listeners: [{proxy: true}]\n"},
			{Name: "variable blocks whole transaction", Source: "listeners: [{proxy: Pinned}, {proxy: '${POLICY}'}]\n", New: &renamed},
			{Name: "D1 duplicate rollback", Source: "listeners:\n- proxy: Other\n  proxy: Pinned\n", New: &renamed},
			{Name: "D1 rejects duplicate even when no relink needed", Source: "listeners:\n- proxy: Pinned\n  proxy: DIRECT\n", New: &renamed},
			{Name: "scalar-alias recovery", Source: "pol: &pol Pinned\nlisteners:\n- name: x\n  proxy: *pol\n- name: y\n  proxy: Pinned\n"},
		}
		for _, tc := range cases {
			t.Run(tc.Name, func(t *testing.T) {
				var srv *httpxtest.Server
				srv = httpxtest.New(t, httpxtest.Options{
					Register: httpxtest.Routes(func(r *httpx.Router) {
						r.POST("/test/relink", "test.relink", func(c *httpx.Ctx) (*httpx.Response, error) {
							var changed []string
							_, err := srv.Repo.UpdateProfile(c.Context(), "default", func(_ context.Context, p *model.Profile) error {
								if tc.New != nil {
									p.ProxyGroups[0].Name = *tc.New
								} else {
									p.ProxyGroups[0].Name = "Disabled"
								}
								var err error
								changed, err = subs.RelinkListeners(p, "Pinned", tc.New)
								if err != nil {
									return err
								}
								if tc.FailAfter {
									return errors.New("forced rollback")
								}
								return nil
							})
							if err != nil {
								return httpx.JSONStatus(400, map[string]any{"success": false, "error": err.Error()}), nil
							}
							if changed == nil {
								changed = []string{}
							}
							return httpx.JSON(map[string]any{"success": true, "changed": changed}), nil
						})
					}),
				})
				if _, err := srv.Repo.UpdateProfile(srv.Context(), "default", func(_ context.Context, p *model.Profile) error {
					p.Mihomo.CustomConfig = tc.Source
					return json.Unmarshal([]byte(`[{"id":"g1","name":"Pinned","type":"select"}]`), &p.ProxyGroups)
				}); err != nil {
					t.Fatal(err)
				}
				resp := srv.Post("/test/relink")
				var body any
				_ = json.Unmarshal(resp.Body, &body)
				p, err := srv.Repo.Profile(srv.Context(), "default")
				if err != nil {
					t.Fatal(err)
				}
				var parsedSource any = p.Mihomo.CustomConfig
				if node, err := yamlx.Parse([]byte(p.Mihomo.CustomConfig)); err == nil {
					if j, err := yamlx.ToJSON(node); err == nil {
						_ = json.Unmarshal(j, &parsedSource)
					}
				}
				key, _ := json.Marshal(map[string]string{"kind": "relink", "name": tc.Name})
				val, _ := json.Marshal(map[string]any{
					"status": resp.Status,
					"body":   body,
					"source": parsedSource,
					"group":  p.ProxyGroups[0].Name,
				})
				ref.Assert(t, key, val)
			})
		}
	})
}
