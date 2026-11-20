package substore

import (
	"configflow/internal/yamlx"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestProxyYAMLEntrypointsRejectAliasAmplification(t *testing.T) {
	var b strings.Builder
	b.WriteString("x0: &x0 [leaf]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "x%d: &x%d [*x%d, *x%d]\n", i, i, i-1, i-1)
	}
	for _, source := range []string{b.String() + "proxies: *x40\n", "proxies: &self [*self]\n"} {
		if _, err := ParseProxyNodes(source); err == nil || !strings.Contains(err.Error(), "引用") {
			t.Fatalf("missing rejection: %v", err)
		}
		// Detection returns only a boolean and never materialises YAML values; the
		// actual proxy parser must still reject even when the fast detector says yes.
		_ = isYAMLResponse(source)
	}
}

func FuzzProxyYAMLReaders(f *testing.F) {
	for _, source := range []string{"proxies: [{name: n, type: ss, server: n.example.com, port: 443}]", "proxies: &self [*self]", "x: &x {a: 1}\nproxies: [*x]", "proxies: [{1: a, true: b, null: c}]"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 65536 {
			t.Skip()
		}
		_ = isYAMLResponse(source)
		_, _ = ParseProxyNodes(source)
	})
}

func TestProxyYAMLSubtreeBudgetCannotFillNull(t *testing.T) {
	source := "x: &x [" + strings.Repeat("leaf,", 50) + "leaf]\nb: &b [" + strings.Repeat("*x,", 30) + "*x]\npadding: [" + strings.Repeat("p,", 3000) + "p]\nproxies: *b\n"
	if _, err := yamlx.Parse([]byte(source)); err != nil {
		t.Fatalf("full source should fit budget: %v", err)
	}
	if proxies, err := ParseProxyNodes(source); !errors.Is(err, yamlx.ErrAliasBudget) || proxies != nil {
		t.Fatalf("subtree must refuse explicitly, not fill null: %v", err)
	}
}
