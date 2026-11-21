package yamlbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"strings"
	"testing"
	"unicode/utf8"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

type testObj struct {
	keys   []string
	values map[string]any
}

func newTestObj(kv ...any) *testObj {
	o := &testObj{values: map[string]any{}}
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

func (o *testObj) Set(key string, value any) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *testObj) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for _, k := range o.keys {
			if !yield(k, o.values[k]) {
				return
			}
		}
	}
}

func (o *testObj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		vb, err := json.Marshal(o.values[k])
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func parseTestJSON(source string) (any, error) {
	if !utf8.ValidString(source) {
		return nil, errors.New("invalid utf-8")
	}
	dec := json.NewDecoder(strings.NewReader(source))
	dec.UseNumber()
	val, err := readTestJSON(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing token")
	}
	return val, nil
}

func readTestJSON(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		o := newTestObj()
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := readTestJSON(dec)
			if err != nil {
				return nil, err
			}
			o.Set(k.(string), v)
		}
		_, err := dec.Token()
		return o, err
	case '[':
		list := []any{}
		for dec.More() {
			v, err := readTestJSON(dec)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		_, err := dec.Token()
		return list, err
	}
	return nil, errors.New("unexpected delimiter")
}

func TestEncodeNativeDocuments(t *testing.T) {
	for _, source := range []string{
		"a: !!timestamp 2001-12-14 21:59:43.100000-05:00\nb: 2002-12-14\n",
		"binary: !!binary |\n  YWJj\n",
		"numbers: [!!int 100000000000000000000, 1.0, .inf, -.inf, .nan]\nstrings: ['yes', '0777', '2001-01-01', '1:30']\n",
		"{payload: [a.com], 1: x, null: y, false: z}",
	} {
		t.Run(source, func(t *testing.T) {
			value, err := yamlx.Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			n, err := Encode(value)
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
				t.Fatalf("native graph changed: %s: %v", out, err)
			}
		})
	}
}

func TestEncodeSharedValues(t *testing.T) {
	shared := newTestObj("name", "shared")
	list := []any{shared}
	n, err := Encode(newTestObj("one", list, "two", list, "three", shared))
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
			if _, err := Encode(tc.value); !errors.Is(err, tc.want) {
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
	v := newTestObj()
	v.Set("self", v)
	return v
}

func writerRecursiveList() any {
	v := make([]any, 1)
	v[0] = v
	return v
}

func TestEncodeNativeGraphs(t *testing.T) {
	source := "outside: &same {value: one}\nselected: {first: *same, second: *same} # selected\n"
	doc, err := yamlx.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	selected := yamlx.Get(doc, "selected")
	before, err := yamlx.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	n, err := Encode(selected)
	if err != nil {
		t.Fatal(err)
	}
	first, second := yamlx.Get(n, "first"), yamlx.Get(n, "second")
	if first.Kind != yaml.MappingNode || second.Kind != yaml.AliasNode || second.Alias != first {
		t.Fatal("orphan anchor was not defined once and shared")
	}
	out, err := yamlx.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	again, err := yamlx.Parse(out)
	if err != nil || !yamlx.Equal(selected, again) || !strings.Contains(string(out), "# selected") {
		t.Fatalf("selected graph changed: %s: %v", out, err)
	}
	after, err := yamlx.Marshal(doc)
	if err != nil || string(before) != string(after) {
		t.Fatal("encoding mutated its source")
	}
	other, err := yamlx.Parse([]byte("outside: &same {value: two}\nselected: *same\n"))
	if err != nil {
		t.Fatal(err)
	}
	n, err = Encode(newTestObj("one", selected, "two", yamlx.Get(other, "selected")))
	if err != nil {
		t.Fatal(err)
	}
	out, err = yamlx.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	again, err = yamlx.Parse(out)
	want := yamlx.Map(yamlx.Str("one"), selected, yamlx.Str("two"), yamlx.Get(other, "selected"))
	if err != nil || !yamlx.Equal(want, again) || strings.Count(string(out), "&same") != 1 {
		t.Fatalf("colliding source anchors changed values: %s: %v", out, err)
	}
}

func TestEncodeNativeGraphLimits(t *testing.T) {
	shared := yamlx.Seq(yamlx.Str("leaf"))
	for range 40 {
		shared = yamlx.Seq(shared, shared)
	}
	recursive := yamlx.Seq()
	recursive.Content = append(recursive.Content, recursive)
	alias := &yamlx.Node{Kind: yaml.AliasNode, Value: "self"}
	alias.Alias = alias
	for _, tc := range []struct {
		name string
		node *yamlx.Node
		want error
	}{
		{"shared-physical-depth-40", shared, yamlx.ErrAliasBudget},
		{"container-recursion", recursive, yamlx.ErrAliasRecursion},
		{"alias-recursion", alias, yamlx.ErrAliasRecursion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if n, err := Encode(tc.node); !errors.Is(err, tc.want) || n != nil {
				t.Fatalf("unsafe graph accepted: %v", err)
			}
		})
	}
}

func FuzzEncode(f *testing.F) {
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
		value, err := parseTestJSON(source)
		if err != nil {
			t.Skip()
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Skip()
		}
		want, err := yamlx.FromJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		n, err := Encode(value)
		if err != nil {
			t.Fatal(err)
		}
		if !yamlx.Equal(want, n) {
			t.Fatalf("encoded graph differs from the native JSON graph: %s", data)
		}
		out, err := yamlx.Marshal(n)
		if err != nil {
			// Exponents outside float64 (1e400) keep their JSON text and the
			// shared writer refuses them with ErrPolicy (behaviour-changes
			// "N7 shared writer"); the native graph must be refused the same way.
			if _, wantErr := yamlx.Marshal(want); !errors.Is(err, yamlx.ErrPolicy) || !errors.Is(wantErr, yamlx.ErrPolicy) {
				t.Fatalf("unexpected writer failure: %v (native graph: %v)", err, wantErr)
			}
			return
		}
		got, err := yamlx.Parse(out)
		if err != nil || !yamlx.Equal(want, got) {
			t.Fatalf("generated YAML changed JSON meaning: %s: %v", out, err)
		}
	})
}
