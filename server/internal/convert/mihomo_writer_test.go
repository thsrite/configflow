package convert

import (
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

func TestMihomoDocumentNodeLegacyValues(t *testing.T) {
	for _, source := range []string{
		"a: !!timestamp 2001-12-14 21:59:43.100000-05:00\nb: 2002-12-14\n",
		"binary: !!binary |\n  YWJj\n",
		"numbers: [!!int 100000000000000000000, 1.0, .inf, -.inf, .nan]\nstrings: ['yes', '0777', '2001-01-01', '1:30']\n",
		"{payload: [a.com], 1: x, null: y, false: z}",
	} {
		t.Run(source, func(t *testing.T) {
			value, err := yamlx.LoadWith(source, yamlx.LoadOptions{KeepNonStringKeys: true})
			if err != nil {
				t.Fatal(err)
			}
			n, err := mihomoDocumentNode(value)
			if err != nil {
				t.Fatal(err)
			}
			out, err := yamlx.Marshal(n)
			if err != nil {
				t.Fatal(err)
			}
			want, err := yamlx.Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			got, err := yamlx.Parse(out)
			if err != nil || !yamlx.Equal(want, got) {
				t.Fatalf("legacy value changed: %s: %v", out, err)
			}
		})
	}
}

func TestMihomoDocumentNodeSharedValues(t *testing.T) {
	shared := jsonx.Obj("name", "shared")
	list := []any{shared}
	n, err := mihomoDocumentNode(jsonx.Obj("one", list, "two", list, "three", shared))
	if err != nil {
		t.Fatal(err)
	}
	if first, second := yamlx.Get(n, "one"), yamlx.Get(n, "two"); second.Kind != yaml.AliasNode || second.Alias != first {
		t.Fatal("shared list was expanded")
	}
	if third := yamlx.Get(n, "three"); third.Kind != yaml.AliasNode || third.Alias != yamlx.Get(n, "one").Content[0] {
		t.Fatal("shared object was expanded")
	}
	for _, tc := range []struct {
		name  string
		value any
		want  error
	}{
		{"alias-depth-40", writerAliasChain(), yamlx.ErrAliasBudget},
		{"recursive-object", writerRecursiveObject(), yamlx.ErrAliasRecursion},
		{"recursive-list", writerRecursiveList(), yamlx.ErrAliasRecursion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := mihomoDocumentNode(tc.value); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func writerAliasChain() any {
	var value any = []any{"leaf"}
	for range 40 {
		value = []any{value, value}
	}
	return value
}

func writerRecursiveObject() any {
	v := jsonx.NewObject()
	v.Set("self", v)
	return v
}

func writerRecursiveList() any {
	v := make([]any, 1)
	v[0] = v
	return v
}

func FuzzMihomoWriter(f *testing.F) {
	for _, seed := range []string{`{"name":"香港","port":80}`, `{"large":100000000000000000000,"float":1.0}`, `{"a":[true,null,"yes","2001-01-01","line\nnext\t"]}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 64*1024 {
			t.Skip()
		}
		if _, err := yamlx.FromJSON([]byte(source)); err != nil {
			t.Skip()
		}
		value, err := jsonx.ParseString(source)
		if err != nil {
			t.Fatal(err)
		}
		// The builder receives legacy parsed values until N7-3d. Compare the
		// actual input value, not decimal precision already lost by that reader.
		data, err := json.Marshal(value)
		if err != nil {
			t.Skip() // Non-finite values are covered explicitly above.
		}
		want, err := yamlx.FromJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		n, err := mihomoDocumentNode(value)
		if err != nil {
			t.Fatal(err)
		}
		out, err := yamlx.Marshal(n)
		if err != nil {
			t.Fatal(err)
		}
		got, err := yamlx.Parse(out)
		if err != nil || !yamlx.Equal(want, got) {
			t.Fatalf("generated YAML changed JSON meaning: %s: %v", out, err)
		}
	})
}
