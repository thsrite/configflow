package yamlx

import (
	"errors"
	"fmt"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStandardTagShapes(t *testing.T) {
	tests := []struct {
		tags  []string
		kind  yaml.Kind
		shape string
	}{
		{[]string{"null", "bool", "int", "float", "str", "timestamp", "binary", "merge"}, yaml.ScalarNode, msgScalarShape},
		{[]string{"seq", "omap", "pairs"}, yaml.SequenceNode, msgSequenceShape},
		{[]string{"map", "set"}, yaml.MappingNode, msgMappingShape},
	}
	for _, tc := range tests {
		for _, tag := range tc.tags {
			for _, form := range []struct {
				text string
				kind yaml.Kind
			}{{"value", yaml.ScalarNode}, {"[a]", yaml.SequenceNode}, {"{a: 1}", yaml.MappingNode}} {
				t.Run(tag+form.text, func(t *testing.T) {
					text := form.text
					if form.kind == yaml.ScalarNode {
						switch tag {
						case "bool":
							text = "true"
						case "int":
							text = "1"
						case "float":
							text = "1.5"
						case "null":
							text = "null"
						}
					}
					source := "# header\n!!" + tag + " " + text
					_, err := Parse([]byte(source))
					if form.kind == tc.kind {
						if err != nil {
							t.Fatal(err)
						}
						return
					}
					want := fmt.Sprintf(msgTagShape, "!!"+tag, tc.shape, 2)
					var syntax *SyntaxError
					if !errors.Is(err, ErrPolicy) || !errors.As(err, &syntax) || syntax.Line != 2 || err.Error() != want {
						t.Fatalf("diagnostic %v want %s", err, want)
					}
					manual := &Node{Kind: form.kind, Tag: "!!" + tag, Line: 2}
					var value any
					if err := Decode(manual, &value); !errors.Is(err, ErrPolicy) {
						t.Fatalf("Decode: %v", err)
					}
					if _, err := MarshalChecked(t, manual); !errors.Is(err, ErrPolicy) {
						t.Fatalf("Marshal: %v", err)
					}
				})
			}
		}
	}
}

func TestTagShapeParsingDoesNotDecodeDestination(t *testing.T) {
	n, err := Parse([]byte("!!int 300"))
	if err != nil {
		t.Fatal(err)
	}
	var value uint8
	if err := Decode(n, &value); err == nil {
		t.Fatal("out-of-range integer decoded")
	}
	for _, source := range []string{"!custom [a]", "!custom {a: 1}", "!custom value"} {
		if _, err := Parse([]byte(source)); err != nil {
			t.Fatal(err)
		}
	}
}
