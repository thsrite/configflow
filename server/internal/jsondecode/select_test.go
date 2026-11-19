package jsondecode

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSelectExactMembers(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		want        map[string]string
		errKind     string
	}{
		{"aliases", `{"id":"real","ID":"forged","Id":"mixed","child":{"n":2.00}}`, map[string]string{"id": `"real"`, "child": `{"n":2.00}`}, ""},
		{"last complete", `{"id":{"old":1},"id":{"last":2}}`, map[string]string{"id": `{"last":2}`}, ""},
		{"escaped duplicate", `{"id":false,"i\u0064":null}`, map[string]string{"id": `null`}, ""},
		{"unknown only", `{"ID":"forged","other":5}`, map[string]string{}, ""},
		{"null", `null`, map[string]string{}, ""},
		{"array", `[]`, nil, "type"}, {"string", `"text"`, nil, "type"}, {"number", `1`, nil, "type"}, {"bool", `true`, nil, "type"},
		{"syntax", `{"id":`, nil, "syntax"}, {"trailing", `{} {}`, nil, "syntax"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Select([]byte(tt.input), "id", "child", "id")
			switch tt.errKind {
			case "type":
				var target *json.UnmarshalTypeError
				if !errors.As(err, &target) {
					t.Fatalf("type error wanted: %v", err)
				}
				return
			case "syntax":
				var target *json.SyntaxError
				if !errors.As(err, &target) {
					t.Fatalf("syntax error wanted: %v", err)
				}
				return
			}
			if err != nil || len(got) != len(tt.want) {
				t.Fatalf("got=%v err=%v want=%v", got, err, tt.want)
			}
			for name, want := range tt.want {
				if string(got[name]) != want {
					t.Fatalf("%s=%s want %s", name, got[name], want)
				}
			}
		})
	}
}

func TestSelectOwnsValues(t *testing.T) {
	input := []byte(`{"id":{"n":2.00},"other":"ignored"}`)
	before := bytes.Clone(input)
	first, err := Select(input, "id")
	if err != nil {
		t.Fatal(err)
	}
	first["id"][0] = '['
	if !bytes.Equal(input, before) {
		t.Fatal("selected value aliases input")
	}
	second, err := Select(input, "id")
	if err != nil {
		t.Fatal(err)
	}
	for i := range input {
		input[i] = ' '
	}
	if string(second["id"]) != `{"n":2.00}` {
		t.Fatal("input mutation changed selected value")
	}
}

func TestSelectAllocationsIgnoreUnselectedValue(t *testing.T) {
	measure := func(padding int) testing.BenchmarkResult {
		input, _ := json.Marshal(map[string]string{"id": "real", "unselected": strings.Repeat("x", padding)})
		return testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				got, err := Select(input, "id")
				if err != nil || string(got["id"]) != `"real"` || len(got) != 1 {
					b.Fatal(got, err)
				}
			}
		})
	}
	small, large := measure(0), measure(1<<20)
	if small.N == 0 || large.N == 0 {
		t.Fatal("allocation measurement did not execute")
	}
	t.Logf("padding=0/1MiB bytes=%d/%d allocs=%d/%d", small.AllocedBytesPerOp(), large.AllocedBytesPerOp(), small.AllocsPerOp(), large.AllocsPerOp())
	if large.AllocedBytesPerOp() > small.AllocedBytesPerOp()+4<<10 || large.AllocsPerOp() > small.AllocsPerOp()+4 || large.AllocedBytesPerOp() > 8<<10 || large.AllocsPerOp() > 64 {
		t.Fatal("Select copied unselected payload or allocation cost scaled with padding")
	}
}

func FuzzSelectExactMembers(f *testing.F) {
	for _, input := range []string{`{}`, `null`, `{"id":"real","ID":"forged"}`, `{"id":{"a":1},"id":{"b":2}}`, `{"id":false,"i\u0064":null}`, `{"child":{"n":2.00},"ignored":[1,2,3]}`, `[]`, `1`, `{"id":`} {
		f.Add([]byte(input))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 32<<10 {
			t.Skip()
		}
		before := bytes.Clone(input)
		var native map[string]json.RawMessage
		expectedErr := json.Unmarshal(input, &native)
		selected, err := Select(input, "id", "child")
		if (err == nil) != (expectedErr == nil) {
			t.Fatalf("error mismatch %v/%v", err, expectedErr)
		}
		if err != nil {
			return
		}
		count := 0
		for _, name := range []string{"id", "child"} {
			expected, exists := native[name]
			actual, found := selected[name]
			if exists != found || !bytes.Equal(expected, actual) {
				t.Fatalf("%s lost exact selected member", name)
			}
			if exists {
				count++
				if len(actual) > 0 {
					actual[0] ^= 1
				}
			}
		}
		if len(selected) != count || !bytes.Equal(before, input) {
			t.Fatal("selection leaked unknown members or aliases input")
		}
	})
}
