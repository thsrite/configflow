package yamlx

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestEditRewriteKeepsCommentsAndSemantics(t *testing.T) {
	tests := []struct{ name, source string }{
		{"flow", "# head\nitems: [one, two] # items\nkeep: &base {key: value} # anchor\nother: *base # alias\n"},
		{"CRLF", "# head\r\nitems:\r\n  # first\r\n  - one # one\r\n\r\nkeep: value # tail\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var want *Node
			out, err := Edit([]byte(tt.source), func(n *Node) error {
				Set(n, "items", Seq(Str("changed")))
				want = copyGraph(n, map[*Node]*Node{})
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := Parse(out)
			if err != nil || !Equal(got, want) {
				t.Fatalf("wrong result: %v\n%s", err, out)
			}
			for _, comment := range captureEdit(mustEditParse(t, tt.source)).comments {
				if !strings.Contains(string(out), comment) {
					t.Errorf("lost %s", comment)
				}
			}
			if strings.Contains(tt.source, "\r\n") && bytes.Contains(bytes.ReplaceAll(out, []byte("\r\n"), nil), []byte("\n")) {
				t.Fatal("bare LF in CRLF rewrite")
			}
		})
	}
}

func mustEditParse(t *testing.T, source string) *Node {
	t.Helper()
	n, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEditFailureRetainsOriginal(t *testing.T) {
	source := []byte("# note\nvalue: original\n")
	callbackErr := errors.New("callback failed")
	tests := []struct {
		name string
		edit func(*Node) error
		want error
	}{
		{"callback", func(n *Node) error { Set(n, "value", Str("changed")); return callbackErr }, callbackErr},
		{"cycle", func(n *Node) error { Set(n, "self", n); return nil }, ErrUnsafeEdit},
		{"malformed node", func(n *Node) error { n.Content = append(n.Content, Str("missing-value")); return nil }, ErrUnsafeEdit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Edit(source, tt.edit)
			if !errors.Is(err, tt.want) || !bytes.Equal(out, source) {
				t.Fatalf("out %q err %v", out, err)
			}
		})
	}
	out, err := Edit(source, func(*Node) error { return nil })
	if err != nil || !bytes.Equal(out, source) {
		t.Fatal("no-op changed source", err)
	}
}

func TestEditDuplicateKeyDiagnostics(t *testing.T) {
	source := []byte("name: first\nname: second\n")
	out, err := Edit(source, func(*Node) error { t.Fatal("callback ran for duplicate keys"); return nil })
	var syntax *SyntaxError
	var duplicate *DuplicateKeyError
	if !errors.Is(err, ErrSyntax) || !errors.Is(err, ErrDuplicateKey) || !errors.As(err, &syntax) || !errors.As(err, &duplicate) || duplicate.Key != "name" || duplicate.Line != 2 || syntax.Line != 2 || !bytes.Equal(out, source) {
		t.Fatalf("wrong duplicate diagnostic: %v", err)
	}
}

func TestEditRetainsCommentOnlyInputAndDisplacedAliases(t *testing.T) {
	out, err := Edit([]byte("# only comment\n"), func(n *Node) error { *n = *Map(Str("added"), Str("yes")); return nil })
	if err != nil || !bytes.Contains(out, []byte("# only comment")) {
		t.Fatalf("%s: %v", out, err)
	}
	source := "listeners: &all [{proxy: Old}] # original\nshared: *all # alias consumer\n"
	out, err = Edit([]byte(source), func(n *Node) error {
		listeners := Clone(Get(n, "listeners"))
		Set(n, "listeners", listeners)
		Set(listeners.Content[0], "proxy", Str("New"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := mustEditParse(t, string(out))
	if Get(Get(doc, "listeners").Content[0], "proxy").Value != "New" || Get(Resolve(Get(doc, "shared")).Content[0], "proxy").Value != "Old" {
		t.Fatalf("aliases leaked: %s", out)
	}
	for _, comment := range []string{"# original", "# alias consumer"} {
		if !bytes.Contains(out, []byte(comment)) {
			t.Fatal("comment lost", comment)
		}
	}
}
