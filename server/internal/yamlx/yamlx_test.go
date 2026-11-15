package yamlx

import (
	"errors"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// Ported from backend/test_fast_yaml.py: the Python tests are about the libyaml
// verification cache, which has no counterpart here. What they pin down
// observably is kept: documents load to the same values on every read, invalid
// text always reports the same error, and the NativeLoader documents/rejections.

func loadNative(text string) (any, error) {
	value, err := LoadWith(text, LoadOptions{Schema: SchemaNative, Key: nativeKeyCheck})
	if err != nil {
		return nil, err
	}
	var visit func(item any) error
	visit = func(item any) error {
		switch v := item.(type) {
		case *jsonx.Object:
			for _, child := range v.All() {
				if err := visit(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range v {
				if err := visit(child); err != nil {
					return err
				}
			}
		case nil, string, bool, jsonx.Number:
			if n, ok := v.(jsonx.Number); ok && n.IsFloat() && (n == "NaN" || n == "Infinity" || n == "-Infinity") {
				return errors.New("OxiDNS YAML 数字必须为有限值")
			}
		default:
			return errors.New("OxiDNS YAML 只支持普通对象、数组和标量")
		}
		return nil
	}
	if err := visit(value); err != nil {
		return nil, err
	}
	if _, ok := value.(*jsonx.Object); !ok {
		return nil, errors.New("OxiDNS 完整配置顶层必须为对象")
	}
	return value, nil
}

var nativeCases = []string{
	"plugins: []\n",
	"on: off\nyes: no\ndate: 2026-10-16\ndecimal: 0123\noctal: 0o17\nclock: 1:20\nexponent: 1e3\nflag: false\nplugins: []",
	"plugins:\n  - tag: forward_local\n    type: forward\n    args:\n      upstreams:\n        - addr: udp://192.0.2.1\n      concurrent: 1_0\n      hex: 0x10\n",
	"api: {http: {listen: \"127.0.0.1:9199\"}}\nplugins: []\n",
	"a: &x [1, 2]\nb: *x\nplugins: []\n",
	"name: 🇭🇰 香港\nplugins: []\n",
	"ratio: 1.5\nneg: -3\nbool: TRUE\nnone: ~\nempty:\nplugins: []\n",
}

var nativeRejected = []string{
	"plugins: []\nplugins: []", "plugins: []\nconstructor: bad", "__proto__: 1\nplugins: []", "1: a\nplugins: []",
	"plugins: !!python/object:dict {}", "plugins: &cycle [*cycle]", "plugins: [\n", "x: &x {a: 1}\ny:\n  <<: *x\n",
	"plugins:\n\t- a\n", "a: !!binary AAAA\n", "? [a, b]\n: 1\n",
}

func TestNativeDocumentsKeepTheirValues(t *testing.T) {
	for _, text := range nativeCases {
		first, err := loadNative(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		again, _ := loadNative(text)
		if !jsonx.Equal(first, again) {
			t.Errorf("%q: reads differ", text)
		}
		// Callers edit the returned document; reads must not share containers.
		for key, v := range first.(*jsonx.Object).All() {
			if o, ok := v.(*jsonx.Object); ok && o == again.(*jsonx.Object).Value(key) {
				t.Errorf("%q: %s shared between reads", text, key)
			}
		}
	}
	value, _ := loadNative(nativeCases[1])
	got, _ := jsonx.Compact(value)
	want := `{"on":"off","yes":"no","date":"2026-10-16","decimal":123,"octal":15,"clock":"1:20","exponent":1000.0,"flag":false,"plugins":[]}`
	if string(got) != want {
		t.Errorf("native YAML 1.2 style scalars:\n got %s\nwant %s", got, want)
	}
}

func TestNativeRejectionsAreIdenticalOnEveryRead(t *testing.T) {
	for _, text := range nativeRejected {
		_, first := loadNative(text)
		if first == nil {
			t.Errorf("%q was accepted", text)
			continue
		}
		for i := 0; i < 3; i++ {
			if _, again := loadNative(text); again == nil || again.Error() != first.Error() {
				t.Errorf("%q: error changed: %v vs %v", text, first, again)
			}
		}
	}
}

func TestInvalidTextReportsTheSameErrorEveryTime(t *testing.T) {
	text := "a: [1, 2\nb: }\n"
	_, first := Load(text)
	if first == nil {
		t.Fatal("invalid text was accepted")
	}
	var loadErr *Error
	if !errors.As(first, &loadErr) || !loadErr.IsYAMLError() {
		t.Errorf("want a syntax *Error, got %T", first)
	}
	for i := 0; i < 3; i++ {
		if _, again := Load(text); again == nil || again.Error() != first.Error() {
			t.Errorf("error changed: %v", again)
		}
	}
}

func TestSafeLoadEqualsLoad(t *testing.T) {
	text := "a: 1\nb: [x, {c: 2.5}]\n"
	got, err := SafeLoad(text)
	if err != nil {
		t.Fatal(err)
	}
	want := jsonx.Obj("a", 1, "b", []any{"x", jsonx.Obj("c", jsonx.Float(2.5))})
	if !jsonx.Equal(got, want) {
		t.Errorf("got %s", jsonx.Repr(got))
	}
	for i := 0; i < 3; i++ {
		again, _ := SafeLoad(text)
		if !jsonx.Equal(again, want) {
			t.Fatal("read changed")
		}
	}
}

func TestYAML11Quirks(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"[yes, No, ON, off, y, n]", `[true,false,true,false,"y","n"]`},
		{"[0o17, 017, 0x1f, 0b11, 1_000, 12:30:45, 1:30.5, .5, 1e3, 1.0e+3]", `["0o17",15,31,3,1000,45045,90.5,0.5,"1e3",1000.0]`},
		{"[~, null, Null, '']", `[null,null,null,""]`},
		{"[2001-12-14]", `[{"$ts":"2001-12-14"}]`},
		{"{a: &x [1], b: *x}", `{"a":[1],"b":[1]}`},
		{"base: &b {x: 1, y: 2}\nd:\n  <<: *b\n  y: 3\n", `{"base":{"x":1,"y":2},"d":{"x":1,"y":3}}`},
		{"- !!binary aGVsbG8=", `[{"$bytes":"68656c6c6f"}]`},
	}
	for _, c := range cases {
		v, err := Load(c.text)
		if err != nil {
			t.Errorf("%q: %v", c.text, err)
			continue
		}
		got, _ := jsonx.Compact(normalize(v))
		if string(got) != c.want {
			t.Errorf("%q:\n got %s\nwant %s", c.text, got, c.want)
		}
	}
}

func TestNonStringKeysAreReported(t *testing.T) {
	for _, text := range []string{"1: a", "yes: a", "~: a", "1.5: a", "2001-01-01: a"} {
		_, err := Load(text)
		var nonString *NonStringKeyError
		if !errors.As(err, &nonString) {
			t.Errorf("%q: want NonStringKeyError, got %v", text, err)
		}
	}
}

func TestLoadAndDumpTimestamps(t *testing.T) {
	v, err := Load("a: 2001-12-14 21:59:43.10 -5\nb: 2002-12-14\n")
	if err != nil {
		t.Fatal(err)
	}
	out, err := SafeDumpUnicode(v)
	if err != nil {
		t.Fatal(err)
	}
	if out != "a: 2001-12-14 21:59:43.100000-05:00\nb: 2002-12-14\n" {
		t.Errorf("got %q", out)
	}
	if _, err := jsonx.Compact(v); err == nil {
		t.Error("jsonx encoded a Timestamp; json.dumps raises TypeError for datetime")
	}
}

func TestOnMarkedReportsScalarRanges(t *testing.T) {
	doc := jsonx.Obj("a", Marked{Value: "secret", Mark: 1}, "b", []any{Marked{Value: "x y", Mark: 2}})
	text, err := Dump(doc, DumpOptions{})
	if err != nil || text != "a: secret\nb:\n- x y\n" {
		t.Fatalf("%q %v", text, err)
	}
	var ranges [][3]int
	_, _ = Dump(doc, DumpOptions{OnMarked: func(m Marked, style byte, start, end int) {
		ranges = append(ranges, [3]int{int(style), start, end})
	}})
	rs := []rune(text)
	for i, r := range ranges {
		piece := strings.TrimSpace(string(rs[r[1]:r[2]])) // the range starts at the separating space, as in the Python marking dumper
		want := []string{"secret", "x y"}[i]
		if piece != want {
			t.Errorf("range %d covers %q, want %q", i, piece, want)
		}
	}
}

func TestDumpErrorsOnForeignTypes(t *testing.T) {
	for _, v := range []any{map[string]any{"a": 1}, struct{}{}, []string{"a"}, make(chan int)} {
		if _, err := SafeDump(v); err == nil {
			t.Errorf("%T was written", v)
		}
	}
}
