package main

import (
	"math"
	"reflect"
	"testing"
)

func TestPythonFloatRepr(t *testing.T) {
	tests := map[float64]string{1: "1.0", 0.5: "0.5", 1e-4: "0.0001", 1e-5: "1e-05", 1.5e-7: "1.5e-07", 1e15: "1000000000000000.0",
		1e16: "1e+16", 1.2345e20: "1.2345e+20", -2.5: "-2.5", 123.456: "123.456", math.Inf(1): "inf"}
	for in, want := range tests {
		if got := pyFloatRepr(in); got != want {
			t.Fatalf("%v: %s, want %s", in, got, want)
		}
	}
}

func TestJSONParsingKeepsNumberKindsAndRejectsDuplicates(t *testing.T) {
	v, err := loadJSON([]byte(`{"i":1,"f":1.0,"e":1e2,"big":123456789012345678901234567890,"nan":NaN,"s":"é😀"}`))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(*Map)
	for key, kind := range map[string]string{"i": "int", "f": "float", "e": "float", "big": "int", "nan": "float", "s": "str"} {
		value, _ := m.Get(key)
		if pyType(value) != kind {
			t.Fatalf("%s: %s", key, pyType(value))
		}
	}
	if s, _ := m.Get("s"); s != "é😀" {
		t.Fatalf("string %q", s)
	}
	for _, bad := range []string{`{"a":1,"a":1}`, `{`, `01`, `1.`, `{"a":1} x`, "\"a\x01\"", `[1,]`} {
		if _, err := loadJSON([]byte(bad)); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if v, err := parseJSON(`{"a":1,"a":2}`, true); err != nil || dumpJSON(v, dumpOptions{indent: -1}) != `{"a": 2}` {
		t.Fatal(v, err)
	}
}

func TestJSONDumpMatchesPythonLayout(t *testing.T) {
	v, _ := loadJSON([]byte(`{"b":[1,2.5,{}],"a":{"x":null,"y":"é\n"},"c":[]}`))
	if got := dumpJSON(v, dumpOptions{indent: -1}); got != `{"b": [1, 2.5, {}], "a": {"x": null, "y": "é\n"}, "c": []}` {
		t.Fatal(got)
	}
	want := "{\n  \"a\": {\n    \"x\": null,\n    \"y\": \"\\u00e9\\n\"\n  },\n  \"b\": [\n    1,\n    2.5,\n    {}\n  ],\n  \"c\": []\n}"
	if got := dumpJSON(v, dumpOptions{indent: 2, sortKeys: true, ensureASCII: true}); got != want {
		t.Fatal(got)
	}
}

func TestPythonEqualityAndKeys(t *testing.T) {
	if !pyEqual(int64(1), 1.0) || !pyEqual(true, int64(1)) || pyEqual(math.NaN(), math.NaN()) || pyEqual("1", int64(1)) {
		t.Fatal("numeric equality")
	}
	m := newMap()
	m.Set(int64(1), "a")
	if v, ok := m.Get(1.0); !ok || v != "a" {
		t.Fatal("float key lookup")
	}
	a, _ := loadJSON([]byte(`{"x":[1,{"y":2}]}`))
	b, _ := loadJSON([]byte(`{"x":[1.0,{"y":2}]}`))
	if !pyEqual(a, b) {
		t.Fatal("deep equality")
	}
}

func TestSplitLinesFollowsStrSplitlines(t *testing.T) {
	got := splitLines("a\nb\r\nc\rd\x0be\x0cf\x1cg h\u0085i\n")
	want := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
	if len(splitLines("")) != 0 || !reflect.DeepEqual(splitLines("x\n\n"), []string{"x", ""}) {
		t.Fatal("edges")
	}
}

func TestYAMLLoaderKeepsTypesOrderAndMerges(t *testing.T) {
	v, err := loadYAML([]byte("b: 1\na: 1.5\nc: [true, null, '1']\nbase: &x {k: 1, j: 2}\nitem:\n  <<: *x\n  k: 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(*Map)
	if got := dumpJSON(m, dumpOptions{indent: -1}); got != `{"b": 1, "a": 1.5, "c": [true, null, "1"], "base": {"k": 1, "j": 2}, "item": {"k": 3, "j": 2}}` {
		t.Fatal(got)
	}
	for _, bad := range []string{"a: 1\na: 2\n", "a: !custom x\n", "a: [1\n", "- a\n---\n- b\n"} {
		if _, err := loadYAML([]byte(bad)); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	out, err := dumpYAML(m, true)
	if err != nil {
		t.Fatal(err)
	}
	back, err := loadYAML(out)
	if err != nil || !pyEqual(back, m) {
		t.Fatalf("round trip %s %v", out, err)
	}
}

func TestISOTimestampsFollowFromISOFormat(t *testing.T) {
	for in, ok := range map[string]bool{"2026-01-01": true, "2026-01-01T00:00:00": true, "2026-01-01T00:00:00.000001Z": true,
		"2026-01-01 00:00:00+08:00": true, "2026-01-01T00:00": true, "20260101T000000": true, "not a time": false,
		"2026-13-01": false, "2026-02-30T00:00:00": false, "2026-01-0100:00": false} {
		if _, got := fromISOFormat(in); got != ok {
			t.Fatalf("%s: %v", in, got)
		}
	}
	a, _ := fromISOFormat("2026-01-01T08:00:00+08:00")
	b, _ := fromISOFormat("2026-01-01T00:00:00Z")
	if !a.Equal(b) {
		t.Fatal("offsets")
	}
}

func TestGoSourceKeysReadLiteralsAndTagsButNotTests(t *testing.T) {
	root := t.TempDir()
	write := func(path, text string) {
		full := root + "/server/internal/x/" + path
		mkdirAll(t, full)
		writeFile(t, full, text)
	}
	write("x.go", "package x\ntype T struct {\n\tA int `json:\"alpha_key,omitempty\" yaml:\"yaml-key\"`\n}\nvar m = map[string]int{\"literal_key\": 1, \"not a key\": 2}\n")
	write("x_test.go", "package x\nvar t = \"test_only_key\"\n")
	write("testdata/fixture.go", "package x\nvar f = \"fixture_key\"\n")
	keys, err := goSourceKeys(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"alpha_key", "yaml-key", "literal_key"} {
		if !keys[k] {
			t.Fatalf("%s missing", k)
		}
	}
	for _, k := range []string{"not a key", "test_only_key", "fixture_key"} {
		if keys[k] {
			t.Fatalf("%s present", k)
		}
	}
}
