package testjson_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"configflow/internal/testjson"
)

func parse(t *testing.T, text string) any {
	t.Helper()
	value, err := testjson.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestOrderedObjectsAndDuplicateKeys(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"constructor", testjson.Obj(testjson.Field{Key: "z", Value: 1}, testjson.Field{Key: "a", Value: "中文"}, testjson.Field{Key: "z", Value: 2}), `{"z":2,"a":"中文"}`},
		{"parsed duplicate", parse(t, `{"z":1,"a":2,"z":3}`), `{"z":3,"a":2}`},
		{"empty", testjson.Obj(), `{}`},
		{"zero", &testjson.Object{}, `{}`},
		{"nil", (*testjson.Object)(nil), `null`},
		{"native encoding", testjson.Obj(testjson.Field{Key: "html", Value: "<中文>"}), `{"html":"\u003c中文\u003e"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.value)
			if err != nil || string(got) != tt.want {
				t.Fatalf("got %s, %v; want %s", got, err, tt.want)
			}
		})
	}
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	for _, text := range []string{"", "1 2", "{} garbage", "[1,]", `{"x":}`, `{"x":1`, "NaN", "Infinity", "01", "+1", "1e", "\xef\xbb\xbf{}"} {
		t.Run(text, func(t *testing.T) {
			if _, err := testjson.Parse([]byte(text)); err == nil {
				t.Fatalf("accepted %q", text)
			}
		})
	}
}

func TestParsePreservesNumbersAndPath(t *testing.T) {
	value := parse(t, `{"rows":[null,{"n":9007199254740993,"decimal":1.00,"exponent":1e+99}],"key":true}`)
	for _, tt := range []struct{ key, want string }{{"n", "9007199254740993"}, {"decimal", "1.00"}, {"exponent", "1e+99"}} {
		got, ok := testjson.Path(value, "rows", 1, tt.key)
		n, number := got.(json.Number)
		if !ok || !number || n.String() != tt.want {
			t.Fatalf("path %s: %v, %t", tt.key, got, ok)
		}
	}
	for _, steps := range [][]any{{"rows", -1}, {"rows", 2}, {"rows", "n"}, {"missing"}, {"key", 0}, {true}} {
		if got, ok := testjson.Path(value, steps...); ok {
			t.Fatalf("unexpected path %v = %v", steps, got)
		}
	}
	if got, ok := testjson.Path(value, "rows", 0); !ok || got != nil {
		t.Fatalf("null is present: %v, %t", got, ok)
	}
	if got, ok := testjson.Path(map[string]any{"n": 7}, "n"); !ok || got != json.Number("7") {
		t.Fatalf("native path: %v, %t", got, ok)
	}
}

func TestDepthAndCycles(t *testing.T) {
	for _, delimiters := range [][2]string{{"[", "]"}, {`{"a":`, "}"}} {
		atLimit := strings.Repeat(delimiters[0], testjson.MaxDepth) + "0" + strings.Repeat(delimiters[1], testjson.MaxDepth)
		value := parse(t, atLimit)
		if diff := testjson.Diff(value, value); diff != "" {
			t.Fatalf("at limit: %s", diff)
		}
		tooDeep := delimiters[0] + atLimit + delimiters[1]
		if _, err := testjson.Parse([]byte(tooDeep)); err == nil {
			t.Fatal("accepted excessive depth")
		}
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	fixture := testjson.Obj(testjson.Field{Key: "cycle", Value: cycle})
	if _, err := json.Marshal(fixture); err == nil {
		t.Fatal("encoded cyclic fixture")
	}
	if testjson.Equal(cycle, cycle) {
		t.Fatal("cyclic values compared equal")
	}
	if _, ok := testjson.Path(cycle, "self"); ok {
		t.Fatal("accepted cyclic path input")
	}
}

func TestStructuralEqualAndDiff(t *testing.T) {
	tests := []struct {
		name, want, got string
		equal, strict   bool
	}{
		{"object order", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true, true},
		{"array order", `[1,2]`, `[2,1]`, false, false},
		{"integer decimal", `1`, `1.0`, true, false},
		{"integer exponent", `1`, `1e0`, true, false},
		{"decimal exponent", `1.00`, `10e-1`, true, true},
		{"signed zero", `-0`, `0`, true, true},
		{"zero exponent", `0`, `-0.00e9999999999999999999999`, true, false},
		{"large integer exact", `9007199254740993`, `9007199254740992`, false, false},
		{"large integer decimal", `9007199254740993`, `9007199254740993.0`, true, false},
		{"huge exponent", `1e9999999999999999999999`, `10e9999999999999999999998`, true, true},
		{"tiny exact", `0.00000000000000000001`, `1e-20`, true, true},
		{"bool number", `true`, `1`, false, false},
		{"null object", `null`, `{}`, false, false},
		{"null array", `null`, `[]`, false, false},
		{"string number", `"1"`, `1`, false, false},
		{"duplicate last", `{"a":1,"a":2}`, `{"a":2}`, true, true},
		{"missing", `{"a":null}`, `{}`, false, false},
		{"extra", `{}`, `{"a":null}`, false, false},
		{"length", `[1]`, `[]`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, got := parse(t, tt.want), parse(t, tt.got)
			if equal := testjson.Equal(want, got); equal != tt.equal {
				t.Fatalf("Equal=%t; Diff=%s", equal, testjson.Diff(want, got))
			}
			if equal := testjson.Equal(want, got, testjson.CompareOptions{DistinguishNumberLiterals: true}); equal != tt.strict {
				t.Fatalf("strict Equal=%t", equal)
			}
		})
	}
	want, got := parse(t, `{"rows":[{"n":1}]}`), parse(t, `{"rows":[{"n":2}]}`)
	if diff := testjson.Diff(want, got); diff != `$["rows"][0]["n"]: want 1, got 2` {
		t.Fatalf("diff=%s", diff)
	}
	type payload struct {
		N int `json:"n"`
	}
	if !testjson.Equal(payload{N: 1}, map[string]any{"n": json.Number("1.0")}) {
		t.Fatal("native structs/maps rejected")
	}
	for _, invalid := range []any{math.NaN(), math.Inf(1), make(chan int), json.Number("invalid")} {
		if testjson.Equal(invalid, invalid) || testjson.Diff(nil, invalid) == "" {
			t.Fatalf("invalid value accepted: %T", invalid)
		}
	}
}
