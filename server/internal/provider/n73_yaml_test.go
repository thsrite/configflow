package provider

import (
	"fmt"
	"strings"
	"testing"

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
