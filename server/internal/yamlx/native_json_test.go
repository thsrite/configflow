package yamlx

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestNativeJSONRoundTrip(t *testing.T) {
	tests := []struct{ src, want string }{
		{`{"z":1,"a":2,"z":3}`, `{"z":3,"a":2}`},
		{`[null,true,false,-0,1.0,6e1,6E+001,1e1000000000,123456789012345678901234567890]`, `[null,true,false,-0,1.0,6e1,6E+001,1e1000000000,123456789012345678901234567890]`},
		{`{"中文":"<>&😀","pair":"\ud83d\ude00","lone":"\ud800"}`, `{"中文":"<>&😀","pair":"😀","lone":"�"}`},
		{`{"x":[{"a":0,"b":2,"a":3},[],{}],"y":"1:30"}`, `{"x":[{"a":3,"b":2},[],{}],"y":"1:30"}`},
		{`"\u2028\u2029"`, `"\u2028\u2029"`},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			n, err := FromJSON([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			got, err := ToJSON(n)
			if err != nil || string(got) != tt.want {
				t.Fatalf("got %s %v; want %s", got, err, tt.want)
			}
			again, err := FromJSON(got)
			if err != nil || !Equal(n, again) {
				t.Fatalf("node JSON round trip: %v", err)
			}
		})
	}
	for _, src := range []string{"", "{}{}", "null true", "[1,]", "{", "NaN", "Infinity", "\ufeff{}", `{"x": 01}`, `{"x":`, `{"x" 1}`, `]`, `{"a":1} garbage`} {
		if _, err := FromJSON([]byte(src)); err == nil {
			t.Fatalf("invalid JSON accepted: %q", src)
		}
	}
	_, err := FromJSON([]byte("[1,]"))
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("lost JSON error chain: %v", err)
	}
}

func TestNativeYAMLToJSON(t *testing.T) {
	src := "base: &b {a: 0777, b: 0x10}\nuse: {<<: *b, b: 0b11, c: 1_000}\ndate: 2001-12-14\nbinary: !!binary aGVsbG8=\nfloat: .5\ntrue: text\n"
	// YAML bool keys have no lossless JSON-object representation.
	n := parseNative(t, src)
	if _, err := ToJSON(n); err == nil {
		t.Fatal("non-string key silently lost")
	}
	n = parseNative(t, "base: &b {a: 0777, b: 0x10}\nuse: {<<: *b, b: 0b11, c: 1_000}\ndate: 2001-12-14\nbinary: !!binary aGVsbG8=\nfloat: .5\n")
	data, err := ToJSON(n)
	want := `{"base":{"a":511,"b":16},"use":{"a":511,"b":3,"c":1000},"date":"2001-12-14","binary":"aGVsbG8=","float":0.5}`
	if err != nil || string(data) != want {
		t.Fatalf("got %s %v; want %s", data, err, want)
	}
	for _, src := range []string{".inf", "-.inf", ".nan"} {
		if _, err := ToJSON(parseNative(t, src)); err == nil {
			t.Fatalf("nonfinite %s accepted", src)
		}
	}
	if data, err := ToJSON(nil); err != nil || string(data) != "null" {
		t.Fatalf("nil: %s %v", data, err)
	}
	if data, err := ToJSON(&Node{}); err != nil || string(data) != "null" {
		t.Fatalf("zero: %s %v", data, err)
	}
}

func TestNativePassThroughTags(t *testing.T) {
	for _, src := range []string{"!!binary aGVsbG8=", "!!set {a: null, b: null}", "!!omap [{a: 1}, {b: 2}]", "!!pairs [{a: 1}, {a: 2}]", "!!map {a: 1}", "!!seq [a, b]", "!custom text"} {
		n := parseNative(t, src)
		data, err := Marshal(n)
		if err != nil || !Equal(n, parseNative(t, string(data))) {
			t.Fatalf("tag %q: %s %v", src, data, err)
		}
	}
}

func FuzzNativeRoundTrip(f *testing.F) {
	for _, src := range []string{"", "# 中文\r\na: yes # comment\r\n", "base: &b {a: 0777}\nuse: {<<: *b, b: on}\n", "[1:30, 1_000, .nan, 2001-12-14]", "[!!str '\nline', null, true]", "a: [b, {c: d}]", "!!set {a: null}", "&a [*a]"} {
		f.Add([]byte(src))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		n, err := Parse(src)
		if err != nil {
			return
		}
		data, err := Marshal(n)
		if err != nil {
			t.Fatalf("parsed node cannot marshal: %v", err)
		}
		again, err := Parse(data)
		if err != nil {
			t.Fatalf("output cannot parse: %v\n%s", err, data)
		}
		if !Equal(n, again) {
			t.Fatalf("round trip changed semantics:\n%q\n%s", src, data)
		}
	})
}

func FuzzNativeNoPanic(f *testing.F) {
	for _, src := range []string{"", "a: [", "&a [*a]", "a: 1\na: 2", "---\n---", `{"a":[1,6e1,"中文"]}`, `{"dup":1,"dup":2}`, `"\ud83d\ude00"`, "a: &a [b, c]\nb: [*a, *a]", "[!!binary Zm9v, 2001-12-14]"} {
		f.Add([]byte(src))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		if n, err := Parse(src); err == nil {
			var v any
			_ = Decode(n, &v)
			_, _ = Marshal(n)
			_, _ = ToJSON(n)
			c := Clone(n)
			_ = Equal(n, c)
			for range Pairs(n) {
				break
			}
		}
		if n, err := FromJSON(src); err == nil {
			data, err := ToJSON(n)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(data) {
				t.Fatalf("invalid JSON output: %q", data)
			}
			again, err := FromJSON(data)
			if err != nil || !Equal(n, again) {
				t.Fatalf("JSON changed values: %v", err)
			}
			var before, after any
			d1, d2 := json.NewDecoder(bytes.NewReader(src)), json.NewDecoder(bytes.NewReader(data))
			d1.UseNumber()
			d2.UseNumber()
			if err := d1.Decode(&before); err != nil {
				t.Fatal(err)
			}
			if err := d2.Decode(&after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("JSON native semantics changed")
			}
		}
	})
}
