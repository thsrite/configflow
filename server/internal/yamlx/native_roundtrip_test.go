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
			out, err := MarshalChecked(t, n)
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
	out, err := MarshalChecked(t, n)
	if err != nil || string(out) != "key: value # scalar note\n" {
		t.Fatalf("scalar comment moved: %q: %v", out, err)
	}
}

func TestMarshalTabStringsUseDoubleQuotes(t *testing.T) {
	for _, value := range []string{"\t", "\ttext", "first\n\t\nlast\n", "first\n\ttext\n"} {
		for _, style := range []yaml.Style{yaml.LiteralStyle, yaml.FoldedStyle, yaml.TaggedStyle | yaml.LiteralStyle} {
			n := &Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: style}
			out, err := MarshalChecked(t, n)
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

func TestMarshalPreservesScalarTagsInAllLayouts(t *testing.T) {
	for _, source := range []string{
		"{addr: x, when: 2001-01-01 10:00:00}",
		"[2001-01-01 10:00:00, 2001-01-01, .inf, -.inf, .nan]",
		"{large: !!int 100000000000000000000, binary: !!binary YQ==, custom: !custom value}",
		"['100000000000000000000', 'true', '2001-01-01', '.inf', '<<']",
		"!!timestamp '2001-01-01 10:00:00'",
	} {
		t.Run(source, func(t *testing.T) {
			n := parseNative(t, source)
			out, err := MarshalChecked(t, n)
			if err != nil {
				t.Fatal(err)
			}
			again, err := Parse(out)
			if err != nil || !Equal(n, again) {
				t.Fatalf("%q -> %s: %v", source, out, err)
			}
		})
	}
}
