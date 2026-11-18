package substore

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/yamlx"
)

func TestReviewN89ProxySubtreeBudgetNeverTruncates(t *testing.T) {
	// The harmless prefix gives the whole list enough ordinary nodes to pass
	// the shared ratio budget. The last proxy alone exceeds that budget.
	var source strings.Builder
	source.WriteString("proxies:\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&source, "- {name: normal-%d, type: http, server: example.com, port: 443}\n", i)
	}
	source.WriteString("- name: aliased\n  type: http\n  server: example.com\n  port: 443\n  opaque:\n    a0: &a0 [value]\n")
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&source, "    a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	graph, err := yamlx.Parse([]byte(source.String()))
	if err != nil {
		t.Fatal("fixture whole graph must pass shared policy:", err)
	}
	list := yamlx.Get(graph, "proxies")
	if _, err := yamlx.ToJSON(list); err != nil {
		t.Fatal("fixture complete list must pass shared policy:", err)
	}
	if _, err := yamlx.ToJSON(list.Content[200]); !errors.Is(err, yamlx.ErrAliasBudget) {
		t.Fatal("fixture isolated proxy must exceed alias budget:", err)
	}
	proxies, err := ParseProxyNodes(source.String())
	if !errors.Is(err, yamlx.ErrAliasBudget) {
		t.Fatalf("expected explicit subtree budget rejection, got %d proxies, err=%v", len(proxies), err)
	}
}
