package yamlx

import "testing"

func TestValueHashUsesEffectiveTypedValues(t *testing.T) {
	for _, tc := range []struct {
		name, left, right string
		equal             bool
	}{
		{"boolean", "True", "true", true},
		{"null", "~", "null", true},
		{"integer", "0x10", "16", true},
		{"float", "1.00", "1.0", true},
		{"exponent", "1.0e+50", "10.0e+49", true},
		{"number types", "1", "1.0", false},
		{"key types", "{1: value}", "{'1': value}", false},
		{"key spelling", "{1.00: value}", "{1.0: value}", true},
		{"mapping order", "{a: 1, b: [2]}", "{b: [2], a: 1}", true},
		{"merge", "a: &a {x: 1}\nb: {<<: *a, y: 2}", "a: {x: 1}\nb: {y: 2, x: 1}", true},
		{"alias", "a: &a [1]\nb: *a", "a: [1]\nb: [1]", true},
		{"list order", "[1, 2]", "[2, 1]", false},
		{"nested types", "[{a: 1}]", "[{a: '1'}]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Parse([]byte(tc.left))
			if err != nil {
				t.Fatal(err)
			}
			b, err := Parse([]byte(tc.right))
			if err != nil {
				t.Fatal(err)
			}
			if Equal(a, b) != tc.equal || (ValueHash(a) == ValueHash(b)) != tc.equal {
				t.Fatal("hash disagrees with effective typed values")
			}
		})
	}
}

func TestMaterializeAliasesRenamesCopiedDefinitions(t *testing.T) {
	n, err := Parse([]byte("a: &a {path: ./rules/a.yaml}\ncopy: *a\ncopy2: *a\n"))
	if err != nil {
		t.Fatal(err)
	}
	changed := *Get(n, "a")
	changed.Content = append([]*Node(nil), changed.Content...)
	Set(&changed, "path", Str("rules/a.yaml"))
	Set(n, "a", &changed)
	before := ValueHash(n)
	MaterializeAliases(n)
	out, err := MarshalChecked(t, n)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if ValueHash(after) != before {
		t.Fatalf("copied anchor changed meaning: %s", out)
	}
	if Resolve(Get(after, "copy")) != Resolve(Get(after, "copy2")) {
		t.Fatal("lost alias sharing")
	}
}
