package mosdns

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

func TestN73YAMLReadGraphs(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("a0: &a0 [x]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	for _, graph := range []string{chain.String(), "a: &a [*a]\n"} {
		unsafeError := func(err error) {
			t.Helper()
			if !errors.Is(err, yamlx.ErrAliasBudget) && !errors.Is(err, yamlx.ErrAliasRecursion) {
				t.Fatalf("unsafe document not refused: %v", err)
			}
		}
		_, err := GenerateMosDNSConfig(jsonx.Obj("mosdns", jsonx.Obj("custom_config", graph)), "")
		unsafeError(err)
		_, err = ParseDNSUpstreams(graph + "addr: https://dns.example/dns-query\n")
		unsafeError(err)
		_, err = ConvertRuleContentForMosDNS(graph + "payload: [example.com]\n")
		unsafeError(err)
		unsafeError(checkReferences(graph+"plugins: []\n", nil, false))
	}
}

func TestN73PolicyDoesNotHideBehindDefaultConfig(t *testing.T) {
	for _, text := range []string{"a: 1\na: 2\n", "a: !!str [x]\n", "a: {<<: 3}\n", "a: !!bool yes\n"} {
		_, err := GenerateMosDNSConfig(jsonx.Obj("mosdns", jsonx.Obj("custom_config", text)), "")
		if !errors.Is(err, yamlx.ErrPolicy) {
			t.Fatalf("policy failure hidden: %v", err)
		}
	}
	if _, err := GenerateMosDNSConfig(jsonx.Obj("mosdns", jsonx.Obj("custom_config", "a: [\n")), ""); err != nil {
		t.Fatalf("ordinary syntax fallback changed: %v", err)
	}
}

func TestN73RemovedSectionsKeepSharedValues(t *testing.T) {
	input := "plugins: &old [original, value]\nkeep: *old\ncustom: !custom {value: preserved}\n"
	output, err := GenerateMosDNSConfig(jsonx.Obj("mosdns", jsonx.Obj("custom_config", input)), "")
	if err != nil {
		t.Fatal(err)
	}
	document, err := yamlx.Parse([]byte(output))
	if err != nil {
		t.Fatal(err)
	}
	keep := yamlx.Resolve(yamlx.Get(document, "keep"))
	if keep == nil || len(keep.Content) != 2 || keep.Content[0].Value != "original" {
		t.Fatalf("alias lost its value: %s", output)
	}
}

func FuzzMosDNSYAML(f *testing.F) {
	f.Add("payload: [example.com]")
	f.Add("x: &x [*x]\npayload: *x")
	f.Fuzz(func(t *testing.T, text string) {
		_, _ = ParseDNSUpstreams(text)
		_, _ = ConvertRuleContentForMosDNS(text)
		_ = checkReferences(text, nil, false)
	})
}
