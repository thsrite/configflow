package yamlx

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestN88ReviewMaterializeMovedDefinition(t *testing.T) {
	for _, reorder := range []bool{false, true} {
		name := "removed"
		if reorder {
			name = "moved later"
		}
		t.Run(name, func(t *testing.T) {
			source := []byte("definition: &value {key: example}\nfirst: *value\nlast: *value\n")
			doc, err := Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			change := func(root *Node) error {
				root = Resolve(root)
				if root.Kind == yaml.DocumentNode {
					root = root.Content[0]
				}
				old := root.Content
				root.Content = append([]*Node{}, old[2:4]...)
				if reorder {
					root.Content = append(root.Content, old[:2]...)
				}
				root.Content = append(root.Content, old[4:]...)
				return nil
			}
			_ = change(doc)
			want := Clone(doc)
			MaterializeAliases(doc)
			output, err := Marshal(doc)
			if err != nil {
				t.Fatalf("materialization rejected acyclic source after edit: %v", err)
			}
			parsed, err := Parse(output)
			if err != nil || !Equal(parsed, want) {
				t.Fatalf("materialization changed meaning: %s %v", output, err)
			}
			output, err = Edit(source, change)
			if err != nil {
				t.Fatalf("public edit rejected acyclic reordering: %v", err)
			}
			parsed, err = Parse(output)
			if err != nil || !Equal(parsed, want) {
				t.Fatalf("public edit changed meaning: %s %v", output, err)
			}
		})
	}
}
