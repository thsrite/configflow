package provider

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
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
			if _, err := loadProviderProxies(text, "Synthetic", "", "订阅", "本次订阅返回的内容"); err == nil {
				t.Fatal("unsafe graph materialized")
			}
			if _, err := ParseProviderProxies(text, "Synthetic", ""); err == nil {
				t.Fatal("unsafe graph normalized")
			}
			if _, err := ValidateRenderedBundle(emptySnapshot(t, nil), []*Rendered{{Content: text}}); err == nil {
				t.Fatal("unsafe graph entered delivery snapshot")
			}
			if _, valid := nodeAt(text, []any{"proxies", 0}); valid {
				t.Fatal("unsafe document used for source lookup")
			}
		})
	}
}

func TestN73ProviderDuplicateDiagnostic(t *testing.T) {
	repoContext(t)
	_, err := ParseProviderProxies("proxies: []\nproxies: []\n", "Synthetic", "")
	if err == nil || !strings.Contains(err.Error(), "YAML 包含重复键") || !strings.Contains(err.Error(), "第 2 行") {
		t.Fatalf("duplicate policy lost: %v", err)
	}
}

func FuzzProviderYAML(f *testing.F) {
	f.Add("proxies: [{name: Synthetic, type: http}]")
	f.Add("proxies: &p [*p]")
	f.Fuzz(func(t *testing.T, text string) {
		document, err := yamlx.Parse([]byte(text))
		if err != nil {
			return
		}
		if proxies := yamlx.Get(document, "proxies"); proxies != nil {
			_, _ = yamlx.ToJSON(proxies)
		}
		_, _ = nodeAt(text, []any{"proxies", 0, "name"})
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
	return jsonx.Parse(data)
}

// Plain nesting has no alias limit; test the native limit without extrapolating
// Python's recursion ceiling. Durations are logged, not compared to wall time.
func TestN73PlainDepthBoundary(t *testing.T) {
	for _, depth := range []int{1000, 4000, 10000, 10001} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			nested := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
			started := time.Now()
			source := "proxies: []\nextra: " + nested
			_, err := loadProviderProxies(source, "Synthetic", "", "订阅", "本次订阅返回的内容")
			if (err == nil) != (depth <= 10000) {
				t.Fatalf("provider loader: %v", err)
			}
			_, _ = nodeAt(source, []any{"extra"})
			_, err = loadProviderProxies("proxies: "+nested, "Synthetic", "", "订阅", "本次订阅返回的内容")
			if err == nil {
				t.Fatal("invalid proxy shape materialized")
			}
			t.Logf("depth=%d bytes=%d elapsed=%s", depth, len(nested), time.Since(started))
		})
	}
}

func TestN73ProviderScalarAndCompositeKeys(t *testing.T) {
	repoContext(t)
	proxies, err := loadProviderProxies("proxies: [{1: a, name: A}]", "Synthetic", "", "订阅", "本次订阅返回的内容")
	if err != nil || len(proxies) != 1 || !jsonx.Equal(proxies[0], jsonx.Obj("1", "a", "name", "A")) {
		t.Fatalf("scalar key changed: %v %v", proxies, err)
	}
	if _, err := loadProviderProxies("proxies: [{? [a,b]: c, name: A}]", "Synthetic", "", "订阅", "本次订阅返回的内容"); err == nil || !strings.Contains(err.Error(), "Provider YAML 内容无效") {
		t.Fatalf("composite key refusal lost context: %v", err)
	}
	proxies, err = loadProviderProxies("? [a,b]\n: ignored\nproxies: [{name: A}]", "Synthetic", "", "订阅", "本次订阅返回的内容")
	if err != nil || len(proxies) != 1 || !jsonx.Equal(proxies[0], jsonx.Obj("name", "A")) {
		t.Fatalf("opaque root metadata changed proxies: %v %v", proxies, err)
	}
}

func TestN73ProviderScalarKeyProjectionAfterMerges(t *testing.T) {
	repoContext(t)
	source := "base: &b {1: inherited, name: Base}\nproxies: [{<<: *b, '1': explicit, name: A}]\n"
	proxies, err := loadProviderProxies(source, "Synthetic", "", "订阅", "本次订阅返回的内容")
	if err != nil || len(proxies) != 1 || !jsonx.Equal(proxies[0], jsonx.Obj("1", "explicit", "name", "A")) {
		t.Fatalf("merged projection changed winner: %v %v", proxies, err)
	}
	proxies, err = ParseProviderProxies("proxies: [{1: a, name: A}]", "Synthetic", "")
	if err != nil || len(proxies) != 1 || !jsonx.Equal(proxies[0], jsonx.Obj("1", "a", "name", "A")) {
		t.Fatalf("public scalar-key normalization: %v %v", proxies, err)
	}
}
