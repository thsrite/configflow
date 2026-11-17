package yamlx

import (
	"bytes"
	"testing"
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
