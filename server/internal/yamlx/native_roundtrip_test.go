package yamlx

import (
	"bytes"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMarshalCollectionKeyComments(t *testing.T) {
	for _, source := range []string{"A:\n[]: # note", "A:\n{}: # note", "? [one, two] # note\n: value\n"} {
		t.Run(source, func(t *testing.T) {
			n := mustEditParse(t, source)
			Delete(n, "A")
			out, err := Marshal(n)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := Parse(out)
			if err != nil || !Equal(n, parsed) || !bytes.Contains(out, []byte("# note")) {
				t.Fatalf("collection-key round trip: %q: %v", out, err)
			}
		})
	}
	n := mustEditParse(t, "key: value # scalar note\n")
	out, err := Marshal(n)
	if err != nil || string(out) != "key: value # scalar note\n" {
		t.Fatalf("scalar comment moved: %q: %v", out, err)
	}
}

func TestMarshalTabStringsUseDoubleQuotes(t *testing.T) {
	for _, value := range []string{"\t", "\ttext", "first\n\t\nlast\n", "first\n\ttext\n"} {
		for _, style := range []yaml.Style{yaml.LiteralStyle, yaml.FoldedStyle, yaml.TaggedStyle | yaml.LiteralStyle} {
			n := &Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: style}
			out, err := Marshal(n)
			if err != nil {
				t.Fatal(err)
			}
			parsed := mustEditParse(t, string(out))
			if !Equal(n, parsed) || parsed.Style&yaml.DoubleQuotedStyle == 0 || parsed.Style&yaml.TaggedStyle != style&yaml.TaggedStyle {
				t.Fatalf("tab round trip changed meaning/style: %q", out)
			}
		}
	}
}
