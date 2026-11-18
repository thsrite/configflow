package yamlx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type nativeReference struct {
	Value    json.RawMessage `json:"value"`
	Rejected bool            `json:"rejected"`
}

func pythonNativeReference(t *testing.T, sources []string) []nativeReference {
	t.Helper()
	python := os.Getenv("CONFIGFLOW_YAML_PYTHON")
	if python == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		python = filepath.Join(home, "cf-test", ".venv", "bin", "python")
	}
	if _, err := os.Stat(python); err != nil {
		t.Skipf("PyYAML reference interpreter unavailable: %v", err)
	}
	input, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-I", "testdata/native_reference.py")
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("PyYAML reference: %v %s", err, stderr.String())
	}
	var rows []nativeReference
	if err := json.Unmarshal(output, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(sources) {
		t.Fatal("incomplete PyYAML reference output")
	}
	return rows
}

func semanticJSON(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestNativeSyntheticPyYAML(t *testing.T) {
	sources := []string{"", "# empty\r\n", "{a: [], b: {}, c: null}", "a: &a {x: 1, y: [a, b]}\nb: {<<: *a, x: 2}\n", "a: [", "x: *missing", "a\n---\nb"}
	for i := range 250 {
		// Booleans, numbers, text, flow/block collections, anchors and comments
		// use the same semantics in both libraries without a schema shim.
		src := fmt.Sprintf("# synthetic %d\nname: '中文-%d'\nport: %d\nenabled: %t\nbase: &base {mode: select, members: [DIRECT, 'on', '1:30']}\nuse:\n  <<: *base\n  name: 节点\nrules:\n  - DOMAIN,host-%d.example.com,DIRECT\n", i, i, 1000+i, i%2 == 0, i)
		if i%3 == 0 {
			src = strings.ReplaceAll(src, "\n", "\r\n")
		}
		sources = append(sources, src)
	}
	rows := pythonNativeReference(t, sources)
	for i, row := range rows {
		n, err := Parse([]byte(sources[i]))
		if row.Rejected {
			if err == nil {
				t.Fatalf("case %d accepted invalid Python YAML", i)
			}
			continue
		}
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		data, err := ToJSON(n)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(semanticJSON(t, data), semanticJSON(t, row.Value)) {
			t.Fatalf("case %d: got %s; Python %s", i, data, row.Value)
		}
	}
	t.Logf("%d synthetic documents agree with PyYAML on semantic values or rejection", len(rows))
}

func TestNativeAmbiguousStringsReadByPyYAML(t *testing.T) {
	values := []string{"yes", "on", "no", "off", "0777", "1:30", "1_000", "2001-12-14", "中文", "\nhello\n", "a\r\nb", "a\u0085b", "a\u2028b", "a\u2029b", strings.Repeat("word ", 200)}
	var sources []string
	for _, s := range values {
		data, err := MarshalChecked(t, Str(s))
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, string(data))
	}
	for i, row := range pythonNativeReference(t, sources) {
		var got string
		if err := json.Unmarshal(row.Value, &got); row.Rejected || err != nil || got != values[i] {
			t.Fatalf("%q emitted %q loaded as %s", values[i], sources[i], row.Value)
		}
	}
}

func TestNativeDocumentedPyYAMLDifferences(t *testing.T) {
	tests := []struct{ src, py, native string }{
		{"yes", `true`, `"yes"`}, {"on", `true`, `"on"`}, {"no", `false`, `"no"`}, {"off", `false`, `"off"`},
		{"1:30", `90`, `"1:30"`}, {"2001-12-14", `"2001-12-14"`, `"2001-12-14"`},
		{"a: 1\na: 2\n", `{"a":2}`, "reject"},
	}
	var sources []string
	for _, tt := range tests {
		sources = append(sources, tt.src)
	}
	for i, row := range pythonNativeReference(t, sources) {
		tt := tests[i]
		if row.Rejected || !reflect.DeepEqual(semanticJSON(t, row.Value), semanticJSON(t, []byte(tt.py))) {
			t.Fatalf("oracle assumption changed for %q", tt.src)
		}
		n, err := Parse([]byte(tt.src))
		if tt.native == "reject" {
			if err == nil {
				t.Fatal("duplicate accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		got, err := ToJSON(n)
		if err != nil || !reflect.DeepEqual(semanticJSON(t, got), semanticJSON(t, []byte(tt.native))) {
			t.Fatalf("%q: %s %v", tt.src, got, err)
		}
	}
}
