package yamlbridge

import (
	"errors"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

func TestValueRetainsPhysicalSharingAndScalarPolicy(t *testing.T) {
	root, err := yamlx.Parse([]byte("base: &base {items: &items [a, b]}\ncopy: *base\nitems: *items\nwhen: 2001-01-01\nbin: !!binary YWJj\nnumber: .inf\nlarge: !!int 100000000000000000001\n"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := Value(root)
	if err != nil {
		t.Fatal(err)
	}
	object := value.(*jsonx.Object)
	if object.Value("base") != object.Value("copy") {
		t.Fatal("mapping alias expanded")
	}
	first, second := object.GetObject("base").GetList("items"), object.GetList("items")
	if &first[0] != &second[0] {
		t.Fatal("sequence alias expanded")
	}
	for key, want := range map[string]any{"when": "2001-01-01", "bin": "YWJj", "number": ".inf", "large": jsonx.Number("100000000000000000001")} {
		if object.Value(key) != want {
			t.Errorf("%s: got %#v, want %#v", key, object.Value(key), want)
		}
	}
}
func TestValueRejectsUnvalidatedUnsafeGraphs(t *testing.T) {
	recursive := yamlx.Seq()
	recursive.Content = []*yamlx.Node{{Kind: yaml.AliasNode, Alias: recursive}}
	chain := yamlx.Seq(yamlx.Str("leaf"))
	for range 40 {
		chain = yamlx.Seq(&yamlx.Node{Kind: yaml.AliasNode, Alias: chain}, &yamlx.Node{Kind: yaml.AliasNode, Alias: chain})
	}
	for _, test := range []struct {
		name string
		node *yamlx.Node
		want error
	}{{"recursion", recursive, yamlx.ErrAliasRecursion}, {"depth40", chain, yamlx.ErrAliasBudget}} {
		t.Run(test.name, func(t *testing.T) {
			value, err := Value(test.node)
			if value != nil || !errors.Is(err, test.want) {
				t.Fatalf("got %v %v", value, err)
			}
		})
	}
}
