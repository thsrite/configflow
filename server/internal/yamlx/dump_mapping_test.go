package yamlx

import "testing"

func TestMarshalMappingWithNonStringKeys(t *testing.T) {
	for _, source := range []string{
		"{payload: [a.com], 1: x}",
		"{payload: [a.com], true: x, null: y}",
		"{payload: [a.com], 1.5: x}",
		"{payload: [a.com], meta: {1: a, 2: b}}",
		"? [one, two] # key note\n: value\n",
	} {
		node, err := Parse([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		out, err := MarshalChecked(t, node)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Parse(out)
		if err != nil || !Equal(node, got) {
			t.Fatalf("non-string key meaning changed: %q: %v", out, err)
		}
	}
}
