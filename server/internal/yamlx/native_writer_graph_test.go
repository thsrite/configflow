package yamlx

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// MarshalChecked verifies the writer's closure contract in tests only. Production
// emission validates the graph statically without parsing the output again.
func MarshalChecked(t testing.TB, value any) ([]byte, error) {
	t.Helper()
	output, err := Marshal(value)
	if err != nil {
		return output, err
	}
	want, err := Encode(value)
	if err != nil {
		t.Fatalf("Marshal accepted a value Encode rejects: %v", err)
	}
	again, err := Parse(output)
	if err != nil || !Equal(want, again) {
		t.Fatalf("Marshal output is not closed: %q: %v; want %#v; got %#v", output, err, want, again)
	}
	return output, nil
}

func TestMarshalManualNodeBindings(t *testing.T) {
	anchor := Str("value")
	anchor.Anchor = "defined"
	alias := func(name string, target *Node) *Node {
		return &Node{Kind: yaml.AliasNode, Value: name, Alias: target, Line: 3}
	}
	rebound := Str("other")
	rebound.Anchor = anchor.Anchor
	for _, tt := range []struct {
		name string
		node *Node
	}{
		{"zero root", &Node{}},
		{"zero child", Seq(&Node{})},
		{"wrong name", Seq(anchor, alias("missing", anchor))},
		{"forward reference", Seq(alias("defined", anchor), anchor)},
		{"missing definition", Seq(alias("defined", anchor))},
		{"rebound target", Seq(anchor, rebound, alias("defined", anchor))},
		{"missing target", Seq(alias("defined", nil))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := MarshalChecked(t, tt.node)
			if !errors.Is(err, ErrPolicy) || len(output) != 0 || !strings.Contains(err.Error(), "YAML 输出") {
				t.Fatalf("output=%q error=%v", output, err)
			}
		})
	}
	for _, node := range []*Node{
		Seq(anchor, alias("defined", anchor)),
		Seq(anchor, rebound, alias("defined", rebound)),
		// Redefinition after a reference does not change its earlier binding.
		Seq(anchor, alias("defined", anchor), rebound),
	} {
		if _, err := MarshalChecked(t, node); err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzNativeNodeGraph(f *testing.F) {
	for _, seed := range [][]byte{{}, {0}, {1, 2, 3}, {4, 5, 6}, {6, 1, 2, 4, 9}, {8, 8, 8, 8, 8}, {2, 0, 6, 1, 5, 4}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Construct nodes directly, including unset kinds, mismatched bindings,
		// forward aliases and redefined anchors. No parser constructs this input.
		if len(data) > 24 {
			data = data[:24]
		}
		nodes := make([]*Node, len(data)+1)
		for i := range nodes {
			nodes[i] = Str("plain")
		}
		root := Seq(nodes...)
		for i, choice := range data {
			n := nodes[i]
			switch choice % 9 {
			case 0:
				*n = Node{}
			case 1:
				*n = *Str("100000000000000000000")
			case 2:
				*n = *Int(int64(choice))
			case 3:
				*n = *Seq(Str("first\n\tsecond\n"), Bool(true))
			case 4:
				*n = *Map(Str("date"), &Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: "2001-01-01 10:00:00"})
			case 5:
				*n = *Null()
			case 6:
				*n = Node{Kind: yaml.AliasNode, Value: "a", Alias: nodes[int(choice)/9%len(nodes)]}
			case 7:
				*n = *Str("!!str\nline")
				n.Style = yaml.FoldedStyle
			case 8:
				*n = Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".inf"}
			}
			if choice&16 != 0 && n.Kind != yaml.AliasNode {
				n.Anchor = "a"
			}
		}
		_, _ = MarshalChecked(t, root)
	})
}
