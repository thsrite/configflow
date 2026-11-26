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

// sharedNodeChain is a node whose level i is a list holding level i-1 twice:
// 2^depth paths through `depth` containers, like a YAML alias chain.
func sharedNodeChain(depth int, leaf *yaml.Node) *yaml.Node {
	n := leaf
	for i := 0; i < depth; i++ {
		n = yamlx.Seq(n, n)
	}
	return n
}

func parseNode(t *testing.T, text string) *yaml.Node {
	t.Helper()
	root, err := yamlx.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
}

// routingCycle constructs an intentionally invalid graph to exercise the
// defensive traversal independently of the validated public reader.
func routingCycle(mark bool) *yaml.Node {
	list := yamlx.Seq(yamlx.Str("x"))
	list.Content = append(list.Content, &yaml.Node{Kind: yaml.AliasNode, Alias: list})
	if mark {
		list.Content = append(list.Content, yamlx.Map(yamlx.Str("routing-mark"), yamlx.Int(3)))
	}
	return yamlx.Map(yamlx.Str("a"), list)
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
		{"cycle", func(t *testing.T) *yaml.Node { return routingCycle(false) }, ""},
		{"cycle with mark", func(t *testing.T) *yaml.Node {
			return routingCycle(true)
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
}

// F1: Apply preserves shared YAML alias anchors on untouched sections and on
// preserved TUN fields.
func TestSharedContainersStayShared(t *testing.T) {
	root, err := yamlx.Parse([]byte("mixed-port: 7890\n" +
		"x-shared: {l0: &l0 [a, b], l1: &l1 [*l0, *l0]}\n" +
		"tun: {mtu: 1400, include: &t [x], again: *t}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.NewFixtureConfig(snapshot(true)).Apply(root, ""); err != nil {
		t.Fatal(err)
	}
	l1 := yamlx.Resolve(yamlx.Get(yamlx.Get(root, "x-shared"), "l1"))
	if l1 == nil || len(l1.Content) != 2 || yamlx.Resolve(l1.Content[0]) != yamlx.Resolve(l1.Content[1]) {
		t.Error("x-shared.l1 lost its shared list")
	}
	tun := yamlx.Get(root, "tun")
	include, again := yamlx.Resolve(yamlx.Get(tun, "include")), yamlx.Resolve(yamlx.Get(tun, "again"))
	if include == nil || include != again || len(include.Content) != 1 || yamlx.Get(tun, "auto-redirect").Value != "true" {
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
			config["mihomo"].(map[string]any)["custom_config"] = b.String()
			var text string
			var err error
			within(t, 20*time.Second, func() {
				text, err = convert.MihomoConfig(context.Background(), gateway.NewFixtureModel(config), convert.MihomoOptions{SkipProviderPreflight: true})
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
			state, err := gateway.NewFixtureConfig(config).EditorState(false)
			if depth == 16 {
				if err != nil || state.FakeIPRange != "" || len(state.Warnings) == 0 || !strings.Contains(strings.Join(state.Warnings, "\n"), gateway.BaseYAMLInvalidMessage) {
					t.Fatalf("unsafe editor graph did not report its base YAML warning: %+v %v", state, err)
				}
				if _, err := gateway.NewFixtureConfig(config).ValidateSelection([]byte(`{"enabled": true, "rule_set_ids": ["r"]}`)); err == nil {
					t.Fatal("unsafe base graph enabled the gateway")
				}
			} else {
				if err != nil || state.FakeIPRange != "198.18.0.0/16" {
					t.Fatalf("editor: %+v %v", state, err)
				}
				if _, err := gateway.NewFixtureConfig(config).ValidateSelection([]byte(`{"enabled": true, "rule_set_ids": ["r"]}`)); err != nil {
					t.Fatal(err)
				}
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
	c := gateway.NewFixtureConfig(snapshot(true))
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
	if err := gateway.NewFixtureConfig(snapshot(false)).Apply(yamlx.Seq(), ""); err != nil {
		t.Fatalf("disabled gateway: %v", err)
	}
}
