package gateway_test

// Tests of the A19 HR review (server/parity/review-a19.md).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"configflow/internal/convert"
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// within fails the test when fn does not return in time (a regression to
// exponential or endless traversal must not hang the suite).
func within(t *testing.T, limit time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("still running after %v", limit)
	}
}

// sharedChain is a value whose level i is a list holding level i-1 twice:
// 2^depth paths through `depth` containers, like a YAML alias chain.
func sharedChain(depth int, leaf any) any {
	v := leaf
	for i := 0; i < depth; i++ {
		v = []any{v, v}
	}
	return v
}

// sharedNodeChain is sharedChain as yaml.Node values sharing their children.
func sharedNodeChain(depth int, leaf *yaml.Node) *yaml.Node {
	n := leaf
	for i := 0; i < depth; i++ {
		n = yamlx.Seq(n, n)
	}
	return n
}

func parseNode(t *testing.T, text string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	return &doc // a document wrapper, as yaml.v3 returns it
}

// F1/F2: Python's _routing_mark_path visits each container once (seen), so
// aliased, deeply shared and self-referencing documents (third-party provider
// files in the deployment bundle) are checked in linear time; a yaml.v3
// document wrapper must not hide the document.
func TestRoutingMarkPathGraphs(t *testing.T) {
	mark := yamlx.Map(yamlx.Str("routing-mark"), yamlx.Int(7))
	tests := []struct {
		name string
		node func(t *testing.T) *yaml.Node
		want string
	}{
		{"cycle", func(t *testing.T) *yaml.Node { return parseNode(t, "a: &a [x, *a]\n").Content[0] }, ""},
		{"cycle with mark", func(t *testing.T) *yaml.Node {
			return parseNode(t, "a: &a [x, *a, {routing-mark: 3}]\n").Content[0]
		}, "R.a.2.routing-mark"},
		{"document wrapper", func(t *testing.T) *yaml.Node {
			return parseNode(t, "proxies: [{name: p}, {name: q, routing-mark: 1}]\n")
		}, "R.proxies.1.routing-mark"},
		{"shared chain", func(*testing.T) *yaml.Node { return sharedNodeChain(64, yamlx.Str("x")) }, ""},
		{"shared chain with mark", func(*testing.T) *yaml.Node {
			return sharedNodeChain(64, mark)
		}, "R" + strings.Repeat(".0", 64) + ".routing-mark"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := tt.node(t)
			var got string
			within(t, 10*time.Second, func() { got = gateway.RoutingMarkPath(n, "R") })
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("jsonx shared chain", func(t *testing.T) {
		value := sharedChain(64, jsonx.Obj("name", "p", "routing-mark", jsonx.Int(5)))
		var got string
		within(t, 10*time.Second, func() { got = gateway.RoutingMarkPathJSONX(value, "节点") })
		if want := "节点" + strings.Repeat(".0", 64) + ".routing-mark"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// F1: shared containers stay shared through the jsonx boundary (one node per
// container, one rebuilt value per node), as Python's dicts stay shared.
func TestSharedContainersStayShared(t *testing.T) {
	n := gateway.JSONXNode(sharedChain(3, jsonx.Obj("a", "b")))
	if n.Content[0] != n.Content[1] || n.Content[0].Content[0] != n.Content[0].Content[1] {
		t.Fatal("a shared list became separate nodes")
	}
	names := []any{"a", "b", "c"}
	n = gateway.JSONXNode([]any{names, names[:2]})
	if len(n.Content[0].Content) != 3 || len(n.Content[1].Content) != 2 {
		t.Fatal("a list and its prefix were merged")
	}

	loaded, err := yamlx.Load("mixed-port: 7890\n" +
		"x-shared: {l0: &l0 [a, b], l1: &l1 [*l0, *l0]}\n" +
		"tun: {mtu: 1400, include: &t [x], again: *t}\n")
	if err != nil {
		t.Fatal(err)
	}
	doc := loaded.(*jsonx.Object)
	if err := gateway.ApplyJSONX(doc, snapshot(true), ""); err != nil {
		t.Fatal(err)
	}
	l1 := doc.GetObject("x-shared").GetList("l1")
	a, b := l1[0].([]any), l1[1].([]any)
	if &a[0] != &b[0] {
		t.Error("x-shared.l1 lost its shared list")
	}
	tun := doc.GetObject("tun")
	include, again := tun.GetList("include"), tun.GetList("again")
	if len(include) != 1 || &include[0] != &again[0] || !tun.GetBool("auto-redirect") {
		t.Errorf("patched TUN: %v", tun)
	}
}

// F1: a small shared graph stays compact. N7-4 applies the same alias
// expansion budget as Mihomo before emitting any generated configuration.
func TestGenerateWithAliasChain(t *testing.T) {
	generationEnv(t)
	for _, depth := range []int{6, 16} {
		t.Run(itoa(depth), func(t *testing.T) {
			var b strings.Builder
			b.WriteString("mixed-port: 7890\nx-chain:\n  l0: &l0 [a, a]\n")
			for i := 1; i <= depth; i++ {
				b.WriteString("  l" + itoa(i) + ": &l" + itoa(i) + " [*l" + itoa(i-1) + ", *l" + itoa(i-1) + "]\n")
			}
			config := snapshot(true)
			config.GetObject("mihomo").Set("custom_config", b.String())
			var text string
			var err error
			within(t, 20*time.Second, func() {
				text, err = convert.GenerateMihomoConfig(context.Background(), config, convert.GenerateOptions{SkipProviderPreflight: true})
			})
			if depth == 16 {
				if !errors.Is(err, yamlx.ErrAliasBudget) || text != "" {
					t.Fatalf("unsafe generated graph was not rejected: %d bytes, %v", len(text), err)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if len(text) > 64<<10 || !strings.Contains(text, "route-address-set") || !strings.Contains(text, "*id") {
				t.Fatalf("generated %d bytes (aliases expanded?)", len(text))
			}
			state, err := gateway.NewConfig(config).EditorState(false)
			if err != nil || state.FakeIPRange != "198.18.0.0/16" {
				t.Fatalf("editor: %+v %v", state, err)
			}
			if _, err := gateway.NewConfig(config).ValidateSelection([]byte(`{"enabled": true, "rule_set_ids": ["r"]}`)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return digits[i : i+1]
	}
	return itoa(i/10) + digits[i%10:i%10+1]
}

// F2: Apply never reports success for a document it did not patch.
func TestApplyDocumentShapes(t *testing.T) {
	c := gateway.NewConfig(snapshot(true))
	wrapped := parseNode(t, "mixed-port: 7890\n")
	if err := c.Apply(wrapped, ""); err != nil {
		t.Fatal(err)
	}
	if got := yamlx.Get(wrapped.Content[0], gateway.MarkerKey); got == nil {
		t.Fatal("the wrapped document was not patched")
	}
	for _, doc := range []*yaml.Node{yamlx.Seq(), yamlx.Str("x"), nil} {
		err := c.Apply(doc, "")
		var gwErr *gateway.Error
		if err == nil || errors.As(err, &gwErr) {
			t.Errorf("Apply(%v) = %v, want a caller error", doc, err)
		}
	}
	if err := gateway.NewConfig(snapshot(false)).Apply(yamlx.Seq(), ""); err != nil {
		t.Fatalf("disabled gateway: %v", err)
	}
}
