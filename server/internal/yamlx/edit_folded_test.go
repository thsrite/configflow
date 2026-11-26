package yamlx

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMarshalFoldedValuesKeepMeaning(t *testing.T) {
	for _, indicator := range []string{">", ">-", ">+", ">2-", ">2+"} {
		for _, body := range []string{"  with # hash\n      more\n    proxy: Pinned\n", "  first\n    deeper\n  last\n\n", "  # string content\n      deeper # content\n  last\n"} {
			source := "text: " + indicator + "\n" + body + "keep: value # note\n"
			t.Run(indicator+body, func(t *testing.T) {
				original := mustEditParse(t, source)
				out, err := MarshalChecked(t, original)
				if err != nil {
					t.Fatal(err)
				}
				parsed := mustEditParse(t, string(out))
				if !Equal(original, parsed) || Get(parsed, "text").Style&yaml.LiteralStyle == 0 {
					t.Fatalf("folded value changed: %q", out)
				}
			})
		}
	}
}

func TestEditKeepsUnattachedNotesWithoutCopyingScalarContents(t *testing.T) {
	source := "text: |\n  # inside string\n  data\n# loose note\nkeep: old # note\n"
	out, err := Edit([]byte(source), func(n *Node) error { Set(n, "added", Str("new")); return nil })
	if err != nil {
		t.Fatal(err)
	}
	parsed := mustEditParse(t, string(out))
	comments := captureEdit(parsed).comments
	for _, comment := range comments {
		if comment == "# inside string" {
			t.Fatal("scalar content copied into comments")
		}
	}
	found := false
	for _, comment := range comments {
		if comment == "# loose note" {
			found = true
		}
	}
	if !found {
		t.Fatalf("loose note lost: %s", out)
	}
}
