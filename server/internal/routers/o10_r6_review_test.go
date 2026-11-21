package routers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// TestO10R6ReviewStoredRoutersMatchEncodedConfig compares the router records
// Records decodes from the stored routers member (model encoding: no HTML
// escaping, stored bytes kept) with routersOf on the encoded resolved view
// (encoding/json output: HTML and U+2028 escaped), for values that encode
// differently in the two: the decoded records, their public views, the
// release records and the workspace revisions must not differ.
func TestO10R6ReviewStoredRoutersMatchEncodedConfig(t *testing.T) {
	ctx := context.Background()
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	items := `{"items":[
		{"id":"r1","type":"routeros","endpoint":"http://192.0.2.1","username":"a&b","password":"p<w>&` + " " + `x","remark":"<b>中</b>","proxy_gateway_agent_id":"a<1>","x_unknown":{"k":"<&>","n":1.50}},
		{"id":"r2","type":"routeros","endpoint":"http://192.0.2.2","rule_sync_interval":"not a number","port":"bad","last_probe":{"ok":true,"message":"<ok>"}},
		{"id":"r3","type":"ikuai","name":"& escaped","endpoint":"http://192.0.2.3"}
	]}`
	if _, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("routers", json.RawMessage(items))
	}); err != nil {
		t.Fatal(err)
	}
	config, err := r.ConfigWithOxiDNSAgents(ctx, "default", repo.ResourceRoots{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	before := routersOf(encoded)
	s := &ManagementService{Repo: r}
	for _, read := range []struct {
		name string
		fn   func(context.Context, string) (*ConfigView, error)
	}{{"Records", s.Records}, {"ConfigRecords", s.ConfigRecords}} {
		t.Run(read.name, func(t *testing.T) {
			view, err := read.fn(ctx, "default")
			if err != nil {
				t.Fatal(err)
			}
			if len(view.Routers) != len(before) || len(before) != 3 {
				t.Fatalf("routers = %d, before %d", len(view.Routers), len(before))
			}
			for i, after := range view.Routers {
				old := before[i]
				if !jsonEqual(after, old) {
					t.Fatalf("%s: record differs", old.ID)
				}
				a, _ := json.Marshal(after.View())
				b, _ := json.Marshal(old.View())
				if string(a) != string(b) {
					t.Fatalf("%s: view %s, before %s", old.ID, a, b)
				}
				ra, err := releaseRouterRecord(after)
				if err != nil {
					t.Fatal(err)
				}
				rb, err := releaseRouterRecord(old)
				if err != nil {
					t.Fatal(err)
				}
				if !jsonEqual(ra, rb) || !sameReleaseRouter(after, old) {
					t.Fatalf("%s: release record %s, before %s", old.ID, ra, rb)
				}
				wa, err := WorkspaceRevision(encoded, after)
				if err != nil {
					t.Fatal(err)
				}
				wb, err := WorkspaceRevision(encoded, old)
				if err != nil {
					t.Fatal(err)
				}
				if wa != wb {
					t.Fatalf("%s: workspace revision %s, before %s", old.ID, wa, wb)
				}
			}
		})
	}
}

// TestO10R6ReviewWorkspaceRuleSetsMatchEncoded compares the typed ruleset rows
// of policy-settings with the strict decode of the encoded resolved view that
// R6 replaced, including hydrated library values and wrong-typed members (which
// must still fail the read).
func TestO10R6ReviewWorkspaceRuleSetsMatchEncoded(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name, library, rules string
	}{
		{"plain", `[{"id":"L1","name":"Lib","url":"https://example.com/a.list"},{"id":"L2","name":"Lib2"}]`,
			`[{"id":"x","itemType":"ruleset","library_rule_id":"L1","policy":"DIRECT","name":"own"},{"id":"y","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":null},{"id":"z","itemType":"ruleset","library_rule_id":"L2"}]`},
		{"null policy", `[{"id":"L1","name":"Lib"}]`, `[{"id":"x","itemType":"ruleset","library_rule_id":"L1","policy":null}]`},
		{"policy number", `[{"id":"L1","name":"Lib"}]`, `[{"id":"x","itemType":"ruleset","library_rule_id":"L1","policy":5}]`},
		{"policy bool", `[{"id":"L1","name":"Lib"}]`, `[{"id":"x","itemType":"ruleset","library_rule_id":"L1","policy":true}]`},
		{"other rule policy number", `[{"id":"L1","name":"Lib"}]`, `[{"id":"y","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":7},{"id":"x","itemType":"ruleset","library_rule_id":"L1","policy":"DIRECT"}]`},
		{"other rule name number", `[{"id":"L1","name":"Lib"}]`, `[{"id":"y","itemType":"rule","rule_type":"DOMAIN","value":"example.com","name":3},{"id":"x","itemType":"ruleset","library_rule_id":"L1"}]`},
		{"other rule enabled string", `[{"id":"L1","name":"Lib"}]`, `[{"id":"y","itemType":"rule","rule_type":"DOMAIN","value":"example.com","enabled":"yes"},{"id":"x","itemType":"ruleset","library_rule_id":"L1"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := repo.OpenDefault(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "second", Name: "Second"}, ""); err != nil {
				t.Fatal(err)
			}
			var library []model.RuleLibraryItem
			if err := json.Unmarshal([]byte(tt.library), &library); err != nil {
				t.Fatal(err)
			}
			if err := r.UpdateSharedAndProfiles(ctx, func(_ context.Context, s *model.Shared, profiles []*model.Profile) error {
				s.RuleLibrary = append(s.RuleLibrary, library...)
				for _, p := range profiles {
					if p.Key() == "second" {
						return p.SetJSON("rule_configs", json.RawMessage(tt.rules))
					}
				}
				return errors.New("no second profile")
			}); err != nil {
				t.Fatalf("not storable: %v", err)
			}
			config, err := r.ConfigWithOxiDNSAgents(ctx, "second", repo.ResourceRoots{})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Rules []struct {
					WorkspaceRule
					ItemType string `json:"itemType"`
				} `json:"rule_configs"`
			}
			var want []WorkspaceRule
			wantErr := jsondecode.Unmarshal(encoded, &doc)
			for _, rule := range doc.Rules {
				if rule.ItemType == "ruleset" {
					rule.Name = orText(rule.Name, rule.ID)
					want = append(want, rule.WorkspaceRule)
				}
			}
			got, err := workspaceRuleSets(config.Profile)
			if (err != nil) != (wantErr != nil) {
				t.Fatalf("error %v, before %v", err, wantErr)
			}
			if wantErr != nil {
				if err.Error() != wantErr.Error() {
					t.Fatalf("error %q, before %q", err, wantErr)
				}
				return
			}
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(want)
			if string(a) != string(b) {
				t.Fatalf("rows %s, before %s", a, b)
			}
		})
	}
}
