package rules

import (
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx/yamlxtest"
)

func TestN73MatcherReaderMigration(t *testing.T) {
	classes := map[string]int{}
	for row, record := range corpus(t)["match"] {
		contents := record.GetObject("contents")
		matcher := NewRuleConfigMatcher(record.GetList("configs"), record.GetList("library"), nil, nil, nil)
		for _, item := range matcher.items {
			if matcher.formats[item.position] != "yaml" {
				continue
			}
			source := contents.GetString(item.item.ID)
			category, err := yamlxtest.CompareNativeReader(source, yamlxtest.NativeAST)
			if err != nil {
				t.Fatalf("real matcher source row %d: %v", row, err)
			}
			classes[category]++
		}
	}
	if classes["equal"] == 0 {
		t.Fatal("no real YAML matcher inputs compared")
	}
	t.Logf("native reader classes=%v unknown=0", classes)
}

func TestN73MatcherRejectsUnsafeAliases(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("l0: &l0 [entry]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	chain.WriteString("payload: ['DOMAIN,example.com']\n")
	for name, source := range map[string]string{
		"depth40": chain.String(),
		"self":    "loop: &loop [*loop]\npayload: ['DOMAIN,example.com']\n",
	} {
		t.Run(name, func(t *testing.T) {
			if entries, _ := yamlPayloadLines(source); len(entries) != 0 {
				t.Fatal("unsafe alias graph produced rule entries")
			}
			if got := matcherFor(source, "classical", "format", "yaml").Match("example.com"); got != nil {
				t.Fatalf("unsafe alias graph matched: %s", jsonx.Repr(got))
			}
		})
	}
}

func FuzzMatcherYAML(f *testing.F) {
	for _, source := range []string{"payload: ['DOMAIN,example.com']", "payload: &p [*p]", "x: &x [*x]\npayload: [example.com]", "payload: [yes, 'on']"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		entries, _ := yamlPayloadLines(source)
		for _, entry := range entries {
			if strings.ContainsAny(entry.value, "\r\n") {
				t.Fatal("multiline rule escaped validation")
			}
		}
	})
}
