package yamlx

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Regressions from the N7-2 Opus review (server/parity/review-n7-2.md).

// aliasChainDocument has a nested alias chain whose standalone expansion
// exceeds the alias budget although the whole document is within it.
func aliasChainDocument(consumer string) string {
	var b strings.Builder
	b.WriteString("pad: [")
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&b, "%d, ", i)
	}
	b.WriteString("0]\nx: &x [" + strings.Repeat("a, ", 50) + "a]\n")
	b.WriteString("big: &b [" + strings.Repeat("*x, ", 30) + "*x]\n")
	b.WriteString(consumer)
	return b.String()
}

func TestReviewN72DisplacedAnchorKeepsAliasChain(t *testing.T) {
	source := aliasChainDocument("use: *b # consumer\n")
	want := mustEditParse(t, source)
	Delete(want, "big")
	out, err := Edit([]byte(source), func(n *Node) error { Delete(n, "big"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	got := mustEditParse(t, string(out))
	if !Equal(got, want) {
		t.Fatalf("alias consumer changed: %s", out[bytes.Index(out, []byte("use:")):])
	}
	if len(out) > len(source) || !bytes.Contains(out, []byte("*x")) || !bytes.Contains(out, []byte("# consumer")) {
		t.Fatalf("alias chain expanded or comment lost: %d bytes from %d", len(out), len(source))
	}
}

func TestReviewN72SpliceFallsBackOrCoversIndentlessSequences(t *testing.T) {
	tests := []struct {
		name, source, want string
		edit               func(*Node)
	}{
		{"indentless value", "a:\n- x # c\n- y\nb: 1 # keep\n", "# c\nb: 1 # keep\n", func(n *Node) { Delete(n, "a") }},
		{"nested indentless value", "s:\n- k: 1\n  remove:\n  - a\n  # note\n  keep: 2\n", "s:\n- k: 1\n  # note\n  keep: 2\n", func(n *Node) { Delete(Get(n, "s").Content[0], "remove") }},
		{"implicit key over the length limit", "short: 1 # c\nother: 2\n", "", func(n *Node) { n.Content[0].Value = strings.Repeat("k", 1100) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := mustEditParse(t, tt.source)
			tt.edit(want)
			out, err := Edit([]byte(tt.source), func(n *Node) error { tt.edit(n); return nil })
			if err != nil {
				t.Fatal(err)
			}
			if tt.want != "" && string(out) != tt.want {
				t.Fatalf("splice output %q want %q", out, tt.want)
			}
			if got := mustEditParse(t, string(out)); !Equal(got, want) || !bytes.Contains(out, []byte("# ")) {
				t.Fatalf("wrong result: %s", out)
			}
		})
	}
}

func TestReviewN72MergeKeysStayImplicit(t *testing.T) {
	tests := []struct{ source, want string }{
		{"base: &b {a: 1}\nuse:\n  <<: *b\n  c: 2\n", "  <<: *b\n"},
		{"base: &b {a: 1}\nuse: {<<: *b, c: 2}\n", "{<<: *b, c: 2}"},
		{"base: &b {a: 1}\nuse:\n  !!merge <<: *b\n", "  !!merge <<: *b\n"},
	}
	for _, tt := range tests {
		n := mustEditParse(t, tt.source)
		out, err := MarshalChecked(t, n)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(out, []byte(tt.want)) || !Equal(mustEditParse(t, string(out)), n) {
			t.Fatalf("merge key written as %q", out)
		}
	}
}

func TestReviewN72RemovedPairCommentsStayNearTheirMapping(t *testing.T) {
	tests := []struct {
		name, source, before, after string
		edit                        func(*Node)
	}{
		{"sequence item", "# top\nkeep: 1\nlisteners:\n- proxy: Old # pinned\n  name: a\n- name: b\n", "# pinned", "- name: a", func(n *Node) {
			Delete(Get(n, "listeners").Content[0], "proxy")
		}},
		{"root key", "a: 1\n# about b\nb: 2 # bb\nc: 3\n", "# bb", "c: 3", func(n *Node) {
			Delete(n, "b")
			Set(n, "added", Seq(Str("x")))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Edit([]byte(tt.source), func(n *Node) error { tt.edit(n); return nil })
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(string(out), "\n")
			for i, line := range lines {
				if strings.TrimSpace(line) == tt.before {
					if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == tt.after {
						return
					}
				}
			}
			t.Fatalf("%q is not directly before %q:\n%s", tt.before, tt.after, out)
		})
	}
}

func TestReviewN72MultipleDocumentsDiagnostic(t *testing.T) {
	source := []byte("listeners: []\n---\nother: 1\n")
	out, err := Edit(source, func(*Node) error { t.Fatal("callback ran"); return nil })
	if !errors.Is(err, ErrMultipleDocuments) || err.Error() != fmt.Sprintf(msgMultipleDocuments, 2) || !bytes.Equal(out, source) {
		t.Fatalf("multi-document diagnostic: %v", err)
	}
}

// yaml.v3 parses `!%21` as the tag `!!` but writes it back as a bare `!!`,
// which is not YAML. PyYAML refused the tag too (no constructor).
func TestReviewN72EmptyYAMLTagRejected(t *testing.T) {
	for _, source := range []string{"!%21", "a: !%21 x\n", "a: !<tag:yaml.org,2002:> [b]\n"} {
		_, err := Parse([]byte(source))
		var syntax *SyntaxError
		if !errors.Is(err, ErrPolicy) || !errors.As(err, &syntax) || syntax.Line < 1 || err.Error() != fmt.Sprintf(msgEmptyTag, syntax.Line) {
			t.Errorf("%q: %v", source, err)
		}
	}
	if out, err := MarshalChecked(t, &Node{Kind: yaml.ScalarNode, Tag: "!!", Value: "x"}); !errors.Is(err, ErrPolicy) {
		t.Errorf("unwritable tag marshalled as %q: %v", out, err)
	}
}

// Every string scalar must read back as the same string after Marshal, in
// every position. yaml.v3's encoder decides quoting with its resolver, which
// does not know `<<`, while its parser reads a plain `<<` as a merge key.
func TestReviewN72StringsSurviveMarshal(t *testing.T) {
	special := []string{"", " ", "~", "<<", "<", "=", "-", "- a", "?", "? a", ":", "a: b", "#", "a #b",
		"!a", "&a", "*a", "%a", "@a", "`a", "|", ">", "'", "\"", "[", "]", "{", "}", ",", "a,b",
		"null", "Null", "NULL", "true", "True", "FALSE", "yes", "No", "on", "OFF", "y", "n",
		"0", "-0", "+1", "0x1F", "0o17", "017", "0b101", "1_000", "1e3", "1.5", ".5", "1.",
		".inf", "-.Inf", ".NaN", "1:30", "2001-12-14", "2001-12-14 21:59:43.10 -5", "=", "!!",
		"a\tb", "\t", "a\nb", "a\n", "\n", " a", "a ", "a  \nb", "\u0085", "\u2028", "\ufeffa"}
	alphabet := []rune("<>=~-?:#!&*%@`|'\",[]{}.01xeEyYnNoO_+ \tab")
	var values []string
	values = append(values, special...)
	for _, a := range alphabet {
		values = append(values, string(a))
		for _, b := range alphabet {
			values = append(values, string([]rune{a, b}))
		}
	}
	for _, value := range values {
		for _, build := range []func(*Node) *Node{
			func(s *Node) *Node { return s },
			func(s *Node) *Node { return Map(Str("k"), s) },
			func(s *Node) *Node { return Map(s, Str("v")) },
			func(s *Node) *Node { n := Seq(s); n.Style = yaml.FlowStyle; return n },
			func(s *Node) *Node { n := Map(s, Str("v")); n.Style = yaml.FlowStyle; return n },
		} {
			for _, style := range []yaml.Style{0, yaml.SingleQuotedStyle, yaml.LiteralStyle, yaml.FoldedStyle} {
				s := &Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: style}
				n := build(s)
				out, err := MarshalChecked(t, n)
				if err != nil {
					t.Errorf("%q style %v: %v", value, style, err)
					continue
				}
				if got, err := Parse(out); err != nil || !Equal(got, n) {
					t.Errorf("%q style %v changed: %q (%v)", value, style, out, err)
				}
			}
		}
	}
}
