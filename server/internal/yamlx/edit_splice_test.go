package yamlx

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEditScalarSplicePreservesOtherBytes(t *testing.T) {
	tests := []struct{ name, source, oldToken, newValue, wantToken string }{
		{"CJK CRLF", "# note\r\nkey: 旧组   # keep\r\n\r\nother: 'unchanged'\r\n", "旧组", "新组", "新组"},
		{"single quoted", "key: 'it''s' # note\nother: [one,two]\n", "'it''s'", "new", "new"},
		{"escaped", "key: \"old\\u0020value\" # note\n", "\"old\\u0020value\"", "false", "\"false\""},
		{"flow punctuation", "key: {a: Old, b: unchanged}  # note\n", "Old", "new, [value]", "\"new, [value]\""},
		{"BOM", "\ufeffkey: old # note\n", "old", "new", "new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Edit([]byte(tt.source), func(n *Node) error {
				target := Get(n, "key")
				if target.Kind != yaml.ScalarNode {
					target = Get(target, "a")
				}
				target.Value = tt.newValue
				target.Tag = "!!str"
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(tt.source, tt.oldToken, tt.wantToken, 1)
			if string(out) != want {
				t.Fatalf("splice changed untouched bytes:\n got %q\nwant %q", out, want)
			}
		})
	}
}

func TestEditPairSplicePreservesCommentsAndOtherLines(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"scalar", "# before\nremove: old # note\n\nkeep:  'same' # keep\n", "# before\n# note\n\nkeep:  'same' # keep\n"},
		{"nested block", "remove:\n  values:\n  - {key: 'x # data'} # nested\n  # explanatory note\n\nkeep:  'same'\n", "  # nested\n  # explanatory note\n\nkeep:  'same'\n"},
		{"plain quotes", "remove: foo - \"bar # note\nkeep:  'same'\n", "# note\nkeep:  'same'\n"},
		{"CRLF", "remove: old # note\r\n\r\nkeep:  'same'\r\n", "# note\r\n\r\nkeep:  'same'\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Edit([]byte(tt.source), func(n *Node) error { Delete(n, "remove"); return nil })
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, []byte(tt.want)) {
				t.Fatalf("got %q want %q", out, tt.want)
			}
		})
	}
}

func TestEditRewritesUnsafeSpliceShapes(t *testing.T) {
	tests := []struct {
		name, source string
		edit         func(*Node)
	}{
		{"first key after dash", "items:\n- remove: old # note\n  keep: value\n", func(n *Node) { Delete(Get(n, "items").Content[0], "remove") }},
		{"flow removal", "items: {remove: old, keep: value} # note\n", func(n *Node) { Delete(Get(n, "items"), "remove") }},
		{"multiline flow value", "remove: [\n] # note\nkeep: value\n", func(n *Node) { Delete(n, "remove") }},
		{"exposed complex key", "remove:\n[]: # note", func(n *Node) { Delete(n, "remove") }},
		{"new node", "key: 'old' # note\n", func(n *Node) { Set(n, "added", Seq(Str("new"))) }},
		{"block scalar", "key: |\n  old\nkeep: value # note\n", func(n *Node) { Get(n, "key").Value = "new\n" }},
		{"anchor consumer", "key: &value old\nother: *value # note\n", func(n *Node) { Get(n, "key").Value = "new" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := mustEditParse(t, tt.source)
			before := captureEdit(n)
			tt.edit(n)
			if _, ok := spliceEdit([]byte(tt.source), n, before); ok {
				t.Fatal("unsafe shape selected splice")
			}
			out, err := Edit([]byte(tt.source), func(n *Node) error { tt.edit(n); return nil })
			if err != nil {
				t.Fatal(err)
			}
			parsed := mustEditParse(t, string(out))
			if !Equal(n, parsed) || !bytes.Contains(out, []byte("# note")) {
				t.Fatalf("bad rewrite: %s", out)
			}
		})
	}
}

func TestEditPlainScalarWithTabOnlyContinuation(t *testing.T) {
	source := []byte("A\r\t")
	out, err := Edit(source, func(n *Node) error { n.Value = "0"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	n := mustEditParse(t, string(out))
	if n.ShortTag() != "!!str" || n.Value != "0" {
		t.Fatalf("wrong edited scalar: %q", out)
	}
}
