package yamlx

import "testing"

// TestDumpMappingWithNonStringKeys: a *Mapping from KeepNonStringKeys dumps
// its keys by their own type, as yaml.safe_dump(document, allow_unicode=True,
// sort_keys=False, default_flow_style=False) does (references from the C14
// review corpus, ruleset_content.normalize_mihomo_yaml on flow documents).
func TestDumpMappingWithNonStringKeys(t *testing.T) {
	cases := map[string]string{
		"{payload: [a.com], 1: x}":               "payload:\n- a.com\n1: x\n",
		"{payload: [a.com], true: x, ~: y}":      "payload:\n- a.com\ntrue: x\nnull: y\n",
		"{payload: [a.com], 1.5: x}":             "payload:\n- a.com\n1.5: x\n",
		"{payload: [a.com], meta: {1: a, 2: b}}": "payload:\n- a.com\nmeta:\n  1: a\n  2: b\n",
	}
	for text, want := range cases {
		value, err := LoadWith(text, LoadOptions{KeepNonStringKeys: true})
		if err != nil {
			t.Fatalf("%s: %v", text, err)
		}
		got, err := Dump(value, DumpOptions{AllowUnicode: true, FlowStyle: FlowBlock, Width: 1<<31 - 1})
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", text, got, err, want)
		}
	}
	value, _ := LoadWith("{1: x}", LoadOptions{KeepNonStringKeys: true})
	if _, err := Dump(value, DumpOptions{SortKeys: true}); err == nil {
		t.Error("sorting a mapping with non-string keys must fail (Python TypeError)")
	}
}
