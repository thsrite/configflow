package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func pythonSetup(t *testing.T, code string, input []byte) []byte {
	t.Helper()
	home, _ := os.UserHomeDir()
	cmd := exec.Command(filepath.Join(home, "cf-test/.venv/bin/python"), "-c", code)
	cmd.Dir = "../../.."
	cmd.Stdin = bytes.NewReader(input)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python setup reference: %v %s", err, raw)
	}
	return raw
}

func equalJSON(t *testing.T, got, want []byte) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("Go=%s\nPython=%s", got, want)
	}
}

func TestSetupCatalogOptionsMatchPython(t *testing.T) {
	c, err := loadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Items) != 25 {
		t.Fatalf("template catalog size=%d", len(c.Items))
	}
	for _, existing := range [][]model.RuleLibraryItem{
		{},
		{{ID: "synthetic", Name: "Synthetic domains", SourceType: pointer("content"), Content: pointer("+.example.com"), Behavior: pointer("domain"), Format: pointer("text")}},
		{{ID: "disabled", Name: c.Items[0].Item.Name, SourceType: c.Items[0].Item.SourceType, Content: c.Items[0].Item.Content, URL: c.Items[0].Item.URL, Behavior: c.Items[0].Item.Behavior, Format: c.Items[0].Item.Format, Enabled: pointer(false)}},
		{{ID: "namesake", Name: c.Items[0].Item.Name, SourceType: pointer("content"), Content: pointer("+.example.com"), Behavior: pointer("domain"), Format: pointer("text")}},
	} {
		input, _ := json.Marshal(existing)
		want := pythonSetup(t, `import json,sys,tempfile;from backend.common.config_repository import ProfileRepository;from backend.common.setup_presets import domain_rule_options,recommended_domain_rules
with tempfile.TemporaryDirectory() as root:
 r=ProfileRepository(root);r.update_shared_transaction(lambda s:s.update(rule_library=json.load(sys.stdin)));json.dump({'options':domain_rule_options(r),'recommended':recommended_domain_rules(r)},sys.stdout,ensure_ascii=False)`, input)
		options := c.options(model.Shared{RuleLibrary: existing})
		got, err := json.Marshal(struct {
			Options     []DomainOption `json:"options"`
			Recommended Selections     `json:"recommended"`
		}{options, c.recommended(options)})
		if err != nil {
			t.Fatal(err)
		}
		equalJSON(t, got, want)
	}
}

func TestHandwrittenRuleEqualityMatchesPython(t *testing.T) {
	entry := func(content, behavior, format string) model.RuleLibraryItem {
		return model.RuleLibraryItem{SourceType: pointer("content"), Content: &content, Behavior: &behavior, Format: &format}
	}
	tests := [][2]model.RuleLibraryItem{
		{entry("DOMAIN-SUFFIX,example.com", "classical", "text"), entry("#. note\n+.EXAMPLE.com\n+.example.com", "domain", "text")},
		{entry("DOMAIN,example.com", "classical", "text"), entry("example.com", "domain", "yaml")},
		{entry("192.0.2.3/24", "classical", "text"), entry("IP-CIDR,192.0.2.0/24,no-resolve", "classical", "text")},
		{entry("IP-CIDR,192.0.2.0/24,no-resolve", "classical", "text"), entry("IP-CIDR,192.0.2.0/24,NO-RESOLVE", "classical", "text")},
		{entry(".example.com", "domain", "text"), entry("DOMAIN-SUFFIX,example.com", "classical", "text")},
		{entry("payload:\n  - >-\n    DOMAIN-SUFFIX,example.com", "classical", "yaml"), entry("DOMAIN-SUFFIX,example.com", "classical", "text")},
		{entry("payload:\n  - ' +.example.com'", "domain", "yaml"), entry("+.example.com", "domain", "text")},
		{entry("", "domain", "text"), entry("# comment", "domain", "text")},
	}
	input, _ := json.Marshal(tests)
	want := pythonSetup(t, `import json,sys;from backend.common.setup_presets import _same_rules;json.dump([_same_rules(*pair) for pair in json.load(sys.stdin)],sys.stdout)`, input)
	got := []bool{}
	matcher := &ruleMatcher{}
	for _, pair := range tests {
		got = append(got, matcher.same(pair[0], pair[1]))
	}
	raw, _ := json.Marshal(got)
	equalJSON(t, raw, want)
}

func TestSetupBuiltinResolutionIsIdempotentAndKeepsNamesakes(t *testing.T) {
	r, ctx := setupRepository(t)
	c := &catalog{Items: []builtin{
		{"builtin:synthetic", model.RuleLibraryItem{Name: "Synthetic", SourceType: pointer("content"), Content: pointer("+.example.com"), Behavior: pointer("domain"), Format: pointer("text"), Enabled: pointer(true)}, "proxy"},
	}}
	_, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("rule_library", []any{jsonx.Obj("id", "existing", "name", "Synthetic", "source_type", "content", "content", "DOMAIN,example.com", "behavior", "classical", "format", "text")})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := r.CommitCount()
	items, err := c.ensureLibrary(ctx, r, nil)
	if err != nil || len(items) != 0 || r.CommitCount() != before {
		t.Fatal("unselected conflicting template was copied", err)
	}
	items, err = c.ensureLibrary(ctx, r, map[string]bool{"builtin:synthetic": true})
	if err != nil || items["builtin:synthetic"].Name != "Synthetic（内置）" {
		t.Fatal("selected namesake not copied safely", err)
	}
	before = r.CommitCount()
	again, err := c.ensureLibrary(ctx, r, nil)
	if err != nil || r.CommitCount() != before || again["builtin:synthetic"].ID != items["builtin:synthetic"].ID {
		t.Fatal("resolved template was duplicated", err)
	}
}

func TestTemplateStrategyMatchesPython(t *testing.T) {
	c, err := loadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Synthetic node", "🇭🇰 Synthetic node"} {
		for _, fallback := range []string{"proxy", "direct"} {
			nodes := []model.Node{{ID: "synthetic-node", Name: name, Type: "ss"}}
			choices := []ruleChoice{}
			pairs := [][2]string{}
			for _, item := range c.Items {
				if item.Route != "" {
					choices = append(choices, ruleChoice{item.Identity, item.Route})
					pairs = append(pairs, [2]string{item.Identity, item.Route})
				}
			}
			input, _ := json.Marshal(struct {
				Rules    [][2]string  `json:"rules"`
				Fallback string       `json:"fallback"`
				Nodes    []model.Node `json:"nodes"`
			}{pairs, fallback, nodes})
			want := pythonSetup(t, `import json,sys;from backend.common.setup_presets import template_strategy;v=json.load(sys.stdin);json.dump(template_strategy(v['rules'],v['fallback'],[],['synthetic-node'],[],v['nodes']),sys.stdout,ensure_ascii=False)`, input)
			groups, policies, final, err := c.strategy(choices, fallback, []string{}, []string{"synthetic-node"}, nil, nodes)
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal([]any{groups, policies, final})
			if err != nil {
				t.Fatal(err)
			}
			equalJSON(t, got, want)
		}
	}
}
