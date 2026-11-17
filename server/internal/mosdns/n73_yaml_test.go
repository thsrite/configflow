package mosdns

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
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
		out, err := GenerateMosDNSConfig(jsonx.Obj("mosdns", jsonx.Obj("custom_config", text)), "")
		if err == nil {
			if _, err := yamlx.Parse([]byte(out)); err != nil {
				t.Fatalf("invalid generated YAML: %v", err)
			}
		}
	})
}

// Plain nesting has no alias limit; test the native limit without extrapolating
// Python's recursion ceiling. Durations are logged, not compared to wall time.
func TestN73PlainDepthBoundary(t *testing.T) {
	for _, depth := range []int{1000, 4000, 10000, 10001} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			nested := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
			started := time.Now()
			source := "extra: " + nested
			out, err := GenerateMosDNSConfig(jsonx.Obj("mosdns", jsonx.Obj("custom_config", source)), "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = yamlx.Parse([]byte(out)); err != nil {
				t.Fatal(err)
			}
			_, err = ParseDNSUpstreams("addr: https://dns.example/dns-query\nextra: " + nested)
			if depth < 10000 && err != nil {
				t.Fatal(err)
			}
			_, err = ConvertRuleContentForMosDNS("extra: " + nested + "\npayload: [example.com]\n")
			if err != nil {
				t.Fatal(err)
			}
			err = checkReferences(source+"\nplugins: []\n", nil, false)
			if (err == nil) != (depth <= 10000) {
				t.Fatalf("archive reader: %v", err)
			}
			t.Logf("depth=%d bytes=%d elapsed=%s", depth, len(nested), time.Since(started))
		})
	}
}

func TestN73UpstreamTimestampSurvivesVerifiedGeneration(t *testing.T) {
	config := jsonx.Obj("mosdns", jsonx.Obj("custom_config", "extra: null\n", "local_dns", "- {addr: x, when: 2001-01-01 10:00:00}\n"))
	if _, err := GenerateMosDNSConfig(config, ""); err != nil {
		t.Fatal(err)
	}
}

func TestN73CompositeKeysFollowEachMosDNSSchema(t *testing.T) {
	if err := checkReferences("? [a,b]\n: ignored\nplugins: []\n", nil, false); err != nil {
		t.Fatal(err)
	}
	out, err := ConvertRuleContentForMosDNS("payload: {? [a,b]: ignored, example.com: null}\n")
	if err != nil || out != "full:example.com" {
		t.Fatalf("payload keys: %q %v", out, err)
	}
	upstreams, err := ParseDNSUpstreams("addr: https://dns.example/dns-query\n? [a,b]\n: preserved\n")
	if err != nil || len(upstreams) != 1 {
		t.Fatalf("upstream composite metadata: %v", err)
	}
	if _, ok := upstreams[0].(*yamlx.Node); !ok {
		t.Fatal("non-JSON metadata must remain a Node")
	}
}

func TestN73SharedUpstreamMetadataMatchesPython(t *testing.T) {
	row := loadRows(t, "testdata/corpus.jsonl.gz")[14912]
	if row.GetString("fn") != "generate" {
		t.Fatal("synthetic regression fixture changed")
	}
	output, err, _ := callRow("generate", row.GetList("args"))
	oracle := yamlxtest.NewOracle(t, "server/internal/mosdns/testdata/n73_reference.py")
	if !nativeMosDNSOutcome(t, oracle, row, output, err) {
		t.Fatalf("shared metadata output: %v", err)
	}
}
