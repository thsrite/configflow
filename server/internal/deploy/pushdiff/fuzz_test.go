package pushdiff

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"configflow/internal/agents/diag"
	"configflow/internal/yamlx"
)

func FuzzConfig(f *testing.F) {
	for _, s := range []string{"mode: rule\n", "a: &a [*a]", aliasChain(), scalarAliases(), "a: 1\na: 2", "secret: [a, b]", "proxies: [{name: 香港, uuid: value}]", "proxy-providers: &p {a: {path: ./rules/a.yaml}}\ncopy: *p\n"} {
		f.Add([]byte("mode: rule\n"), []byte(s))
	}
	f.Fuzz(func(t *testing.T, a, b []byte) {
		if len(a)+len(b) > 128*1024 {
			t.Skip()
		}
		result := Config(a, b, "config.yaml", "config.yaml")
		if len(result.Items) > MaxConfigRows {
			t.Fatal("unbounded rows")
		}
		if result.Raw != nil && len(result.Raw.Lines) > MaxRawLines {
			t.Fatal("unbounded raw")
		}
		if _, err := json.Marshal(result); err != nil {
			t.Fatal(err)
		}
		if result.Status != "unparsable" {
			for _, input := range [][]byte{a, b} {
				doc, err := yamlx.Parse(input)
				if err != nil {
					t.Fatal(err)
				}
				want := agentPaths(doc)
				canonical, err := canonicalYAML(want)
				if err != nil {
					t.Fatal(err)
				}
				got, err := yamlx.Parse(canonical)
				if err != nil || !yamlx.Equal(got, want) {
					t.Fatalf("raw YAML changed meaning: %q (%v)", canonical, err)
				}
			}
		}
	})
}
func FuzzRuleEntries(f *testing.F) {
	for _, s := range []string{"+.example.com\n", "payload: ['+.example.com']", `{"payload":["+.example.com"]}`, "payload: &a [*a]", aliasChain() + "payload: []", scalarAliases() + "payload: []"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 128*1024 {
			t.Skip()
		}
		_, _ = RuleEntries(data)
		s, d := ruleChange([]byte("+.example.com\n"), data, &detailBudget{left: MaxDetailBytes}, nil)
		if _, err := json.Marshal([]any{s, d}); err != nil {
			t.Fatal(err)
		}
	})
}
func FuzzNodeItems(f *testing.F) {
	for _, s := range []string{"proxies: []", "proxies: [{name: 日本語, password: synthetic}]", "proxies: &a [*a]", aliasChain() + "proxies: []", scalarAliases() + "proxies: []"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 128*1024 {
			t.Skip()
		}
		s, d := nodeChange([]byte("proxies: []"), data, &detailBudget{left: MaxDetailBytes}, nil)
		if _, err := json.Marshal([]any{s, d}); err != nil {
			t.Fatal(err)
		}
	})
}
func FuzzCredentialSpans(f *testing.F) {
	for _, s := range []string{"中文 https://u:p@rules.example.com/?key=value", "user:password@192.0.2.1:1080", "https://[::1/broken", "https://rules.example.com/abc1234567890123456789012345", `https://rules.example.com/path\part?token=synthetic\secret`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			t.Skip()
		}
		last := 0
		for _, r := range diag.CredentialSpans(s) {
			if r[0] < last || r[0] >= r[1] || r[1] > len(s) || !utf8.ValidString(s[r[0]:r[1]]) {
				t.Fatalf("invalid range %v", r)
			}
			last = r[1]
		}
		value := markedString(s, false, nil)
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var out Text
		if err = json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		text, _ := out.Unpack()
		if text != s {
			t.Fatal("changed display text")
		}
	})
}
