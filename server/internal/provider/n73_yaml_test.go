package provider

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func providerAliasGraphs() []string {
	var chain strings.Builder
	chain.WriteString("a0: &a0 [x]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	chain.WriteString("proxies: [{name: Synthetic, type: socks5, options: *a40}]\n")
	return []string{chain.String(), "proxies: &p [{name: Synthetic, options: *p}]\n"}
}

func TestN73ProviderYAMLGraphs(t *testing.T) {
	repoContext(t)
	for i, text := range providerAliasGraphs() {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if _, err := readProviderNodes(text, "Synthetic", "", "订阅", "本次订阅返回的内容"); err == nil {
				t.Fatal("unsafe graph materialized")
			}
			if _, err := ParseNodes(text, "Synthetic", ""); err == nil {
				t.Fatal("unsafe graph normalized")
			}
			if _, err := ValidateArtifacts(emptySnapshot(t, nil), []*Artifact{{Content: text}}); err == nil {
				t.Fatal("unsafe graph entered delivery snapshot")
			}
			if _, valid := nodeAtPath(text, FieldPath{{Key: "proxies"}, {Index: 0, Indexed: true}}); valid {
				t.Fatal("unsafe document used for source lookup")
			}
		})
	}
}

func TestN73ProviderDuplicateDiagnostic(t *testing.T) {
	repoContext(t)
	_, err := ParseNodes("proxies: []\nproxies: []\n", "Synthetic", "")
	if err == nil || !strings.Contains(err.Error(), "YAML 包含重复键") || !strings.Contains(err.Error(), "第 2 行") {
		t.Fatalf("duplicate policy lost: %v", err)
	}
}

func FuzzProviderYAML(f *testing.F) {
	f.Add("proxies: [{name: Synthetic, type: http}]")
	f.Add("proxies: &p [*p]")
	f.Fuzz(func(t *testing.T, text string) {
		_, _ = readProviderNodes(text, "Synthetic", "", "订阅", "本次订阅返回的内容")
		document, err := yamlx.Parse([]byte(text))
		if err != nil {
			return
		}
		if proxies := yamlx.Get(document, "proxies"); proxies != nil {
			_, _ = yamlx.ToJSON(proxies)
		}
		_, _ = nodeAtPath(text, FieldPath{{Key: "proxies"}, {Index: 0, Indexed: true}, {Key: "name"}})
	})
}

func readProviderTestValue(text string) (any, error) {
	node, err := yamlx.Parse([]byte(text))
	if err != nil {
		return nil, err
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return nil, err
	}
	return decodeAny(data)
}

// Plain nesting has no alias limit; test the native limit without extrapolating
// Python's recursion ceiling. Durations are logged, not compared to wall time.
func TestN73PlainDepthBoundary(t *testing.T) {
	for _, depth := range []int{1000, 4000, 10000, 10001} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			nested := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
			started := time.Now()
			source := "proxies: []\nextra: " + nested
			_, err := readProviderNodes(source, "Synthetic", "", "订阅", "本次订阅返回的内容")
			if (err == nil) != (depth <= 10000) {
				t.Fatalf("provider loader: %v", err)
			}
			_, _ = nodeAtPath(source, FieldPath{{Key: "extra"}})
			_, err = readProviderNodes("proxies: "+nested, "Synthetic", "", "订阅", "本次订阅返回的内容")
			if err == nil {
				t.Fatal("invalid proxy shape materialized")
			}
			t.Logf("depth=%d bytes=%d elapsed=%s", depth, len(nested), time.Since(started))
		})
	}
}

func TestN73ProviderScalarAndCompositeKeys(t *testing.T) {
	repoContext(t)
	nodes, err := readProviderNodes("proxies: [{1: a, name: A}]", "Synthetic", "", "订阅", "本次订阅返回的内容")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("scalar key changed: %v %v", nodes, err)
	}
	proxies, err := nodesToAny(nodes)
	if err != nil || !testjson.Equal(proxies[0], map[string]any{"1": "a", "name": "A"}) {
		t.Fatalf("scalar key changed: %v %v", proxies, err)
	}
	if _, err := readProviderNodes("proxies: [{? [a,b]: c, name: A}]", "Synthetic", "", "订阅", "本次订阅返回的内容"); err == nil || !strings.Contains(err.Error(), "Provider YAML 内容无效") {
		t.Fatalf("composite key refusal lost context: %v", err)
	}
	nodes, err = readProviderNodes("? [a,b]\n: ignored\nproxies: [{name: A}]", "Synthetic", "", "订阅", "本次订阅返回的内容")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("opaque root metadata changed proxies: %v %v", nodes, err)
	}
	proxies, err = nodesToAny(nodes)
	if err != nil || !testjson.Equal(proxies[0], map[string]any{"name": "A"}) {
		t.Fatalf("opaque root metadata changed proxies: %v %v", proxies, err)
	}
}

func TestN73ProviderScalarKeyProjectionAfterMerges(t *testing.T) {
	repoContext(t)
	source := "base: &b {1: inherited, name: Base}\nproxies: [{<<: *b, '1': explicit, name: A}]\n"
	nodes, err := readProviderNodes(source, "Synthetic", "", "订阅", "本次订阅返回的内容")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("merged projection changed winner: %v %v", nodes, err)
	}
	proxies, err := nodesToAny(nodes)
	if err != nil || !testjson.Equal(proxies[0], map[string]any{"1": "explicit", "name": "A"}) {
		t.Fatalf("merged projection changed winner: %v %v", proxies, err)
	}
	nodes, err = ParseNodes("proxies: [{1: a, name: A}]", "Synthetic", "")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("public scalar-key normalization: %v %v", nodes, err)
	}
	proxies, err = nodesToAny(nodes)
	if err != nil || !testjson.Equal(proxies[0], map[string]any{"1": "a", "name": "A"}) {
		t.Fatalf("public scalar-key normalization: %v %v", proxies, err)
	}
}
