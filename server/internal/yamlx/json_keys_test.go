package yamlx

import (
	"errors"
	"testing"
)

func TestNativeKeyProjectionModes(t *testing.T) {
	source := "base: &base {keep: good, 1: one, false: no, null: zero, 1.5: ratio}\nuse: *base\n"
	tests := []struct {
		name    string
		project func(*Node) error
		want    string
		wantErr error
	}{
		{"parser rejects", func(n *Node) error { return ProjectJSONKeys(n, RejectNonStringKeys) }, "", ErrPolicy},
		{"dialers discard", func(n *Node) error { return ProjectJSONKeys(n, DiscardNonStringKeys) }, `{"base":{"keep":"good"},"use":{"keep":"good"}}`, nil},
		{"substore scalar keys", func(n *Node) error { return ProjectJSONKeys(n, StringifyScalarKeys) }, `{"base":{"keep":"good","1":"one","false":"no","null":"zero","1.5":"ratio"},"use":{"keep":"good","1":"one","false":"no","null":"zero","1.5":"ratio"}}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			base := Get(n, "base")
			alias := Get(n, "use")
			if err := tc.project(n); err != nil {
				if tc.wantErr != nil && errors.Is(err, tc.wantErr) {
					return
				}
				t.Fatal(err)
			}
			if alias.Alias != base || Get(n, "base") != base {
				t.Fatal("projection expanded or replaced shared graph")
			}
			data, err := ToJSON(n)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || string(data) != tc.want {
				t.Fatalf("%s: %v", data, err)
			}
		})
	}
}

func TestSubstoreProjectionCollisionAndUnsupportedKeys(t *testing.T) {
	n, err := Parse([]byte("0: old\n'0': last\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err = func(n *Node) error { return ProjectJSONKeys(n, StringifyScalarKeys) }(n); err != nil {
		t.Fatal(err)
	}
	data, err := ToJSON(n)
	if err != nil || string(data) != `{"0":"last"}` {
		t.Fatalf("%s: %v", data, err)
	}
	for _, source := range []string{"2001-01-01: value", "!!binary aGk=: value", "? [a]\n: value"} {
		n, err := Parse([]byte(source))
		if err != nil {
			t.Fatal(err)
		}
		if err = func(n *Node) error { return ProjectJSONKeys(n, StringifyScalarKeys) }(n); !errors.Is(err, ErrType) {
			t.Fatalf("%s: %v", source, err)
		}
	}
	n, err = Parse([]byte(".nan: value"))
	if err != nil {
		t.Fatal(err)
	}
	if err = func(n *Node) error { return ProjectJSONKeys(n, StringifyScalarKeys) }(n); err != nil {
		t.Fatal(err)
	}
	data, err = ToJSON(n)
	if err != nil || string(data) != `{".nan":"value"}` {
		t.Fatalf("%s: %v", data, err)
	}
	if _, err := Parse([]byte("1: first\n1: second")); !errors.Is(err, ErrDuplicateKey) {
		t.Fatal(err)
	}
}

func TestJSONKeyProjectionMergeOrder(t *testing.T) {
	n := parseNative(t, "base: &b {1: old, keep: yes}\nuse: {<<: *b, '1': last, local: value}")
	if err := ProjectJSONKeys(n, StringifyScalarKeys); err != nil {
		t.Fatal(err)
	}
	data, err := ToJSON(Get(n, "use"))
	if err != nil || string(data) != `{"1":"last","keep":"yes","local":"value"}` {
		t.Fatalf("%s: %v", data, err)
	}
}
