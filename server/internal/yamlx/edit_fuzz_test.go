package yamlx

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

func FuzzEditScalar(f *testing.F) {
	for _, source := range []string{
		"key: old # note\nkeep: same\n",
		"# note\r\nkey: 'old' # inline\r\n\r\nkeep: same\r\n",
		"key: &shared old\nother: *shared # note\n",
		"items: [{key: old}] # flow\n",
		"key: |\n  old\nkeep: same\n",
		"key: >-\n  with # hash\n      more\n    proxy: Pinned\nkeep: same\n",
	} {
		f.Add([]byte(source), "new, # value")
	}
	f.Fuzz(func(t *testing.T, source []byte, value string) {
		if len(source) > 65536 || len(value) > 4096 || !utf8.ValidString(value) {
			t.Skip()
		}
		doc, err := Parse(source)
		if err != nil {
			t.Skip()
		}
		path, ok := editableScalar(doc, nil)
		if !ok {
			t.Skip()
		}
		want := copyGraph(doc, map[*Node]*Node{})
		editNodeAt(want, path).Value = value
		out, err := Edit(source, func(n *Node) error { editNodeAt(n, path).Value = value; return nil })
		if err != nil {
			t.Fatalf("valid scalar edit refused: %v", err)
		}
		got, err := Parse(out)
		if err != nil || !Equal(got, want) {
			t.Fatalf("wrong edited value: %v", err)
		}
		for _, comment := range captureEdit(doc).comments {
			if !bytes.Contains(out, []byte(comment)) {
				t.Fatalf("lost comment %q", comment)
			}
		}
	})
}

func editableScalar(n *Node, path []int) ([]int, bool) {
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
		return path, true
	}
	for i, child := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 {
			continue
		}
		if found, ok := editableScalar(child, append(append([]int(nil), path...), i)); ok {
			return found, true
		}
	}
	return nil, false
}

func editNodeAt(n *Node, path []int) *Node {
	for _, index := range path {
		n = n.Content[index]
	}
	return n
}

func FuzzEditDeletePair(f *testing.F) {
	for _, source := range []string{
		"remove: old # note\nkeep: same\n",
		"remove:\n  list: [one, two] # note\nkeep: same\n",
		"remove: &shared {key: old}\nkeep: *shared # note\n",
		"remove: |\n  # string content\n  text\nkeep: same # note\n",
	} {
		f.Add([]byte(source), uint(0))
	}
	f.Fuzz(func(t *testing.T, source []byte, index uint) {
		if len(source) > 65536 {
			t.Skip()
		}
		doc, err := Parse(source)
		if err != nil || doc.Kind != yaml.MappingNode || len(doc.Content) < 2 {
			t.Skip()
		}
		i := int(index%uint(len(doc.Content)/2)) * 2
		key, ok := stringKey(doc.Content[i])
		if !ok {
			t.Skip()
		}
		want := copyGraph(doc, map[*Node]*Node{})
		Delete(want, key)
		out, err := Edit(source, func(n *Node) error { Delete(n, key); return nil })
		if err != nil {
			t.Fatalf("valid pair deletion refused: %v", err)
		}
		got, err := Parse(out)
		if err != nil || !Equal(got, want) {
			t.Fatalf("wrong deletion result: %v", err)
		}
		for _, comment := range captureEdit(doc).comments {
			if !bytes.Contains(out, []byte(comment)) {
				t.Fatalf("lost comment %q", comment)
			}
		}
		if bytes.Equal(out, source) {
			t.Fatal("deletion did not change the source")
		}
	})
}
