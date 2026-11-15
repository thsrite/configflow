package jsonx

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"testing"
)

// corpusCase is one line of testdata/corpus.jsonl (see testdata/gen.py).
// encoding/json is used here on purpose: the corpus reader must not depend
// on the code under test. It decodes a lone surrogate escape as U+FFFD,
// which is also what Parse does.
type corpusCase struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Input string `json:"input"`
	Lone  bool   `json:"lone"`

	Compact      *string `json:"compact"`
	Indent2      *string `json:"indent2"`
	Sorted       *string `json:"sorted"`
	Response     *string `json:"response"`
	Default      *string `json:"default"`
	SortedSpaced *string `json:"sorted_spaced"`
	Indent1      *string `json:"indent1"`
	Repr         *string `json:"repr"`
	Str          *string `json:"str"`
	Truthy       *bool   `json:"truthy"`

	Error string `json:"error"`
	Type  string `json:"type"`

	Items  [][2]string `json:"items"`
	Values []string    `json:"values"`
	Matrix []string    `json:"matrix"`
}

func loadCorpus(t *testing.T) []corpusCase {
	t.Helper()
	return loadCorpusFile(t, "testdata/corpus.jsonl")
}

func loadCorpusFile(t *testing.T, path string) []corpusCase {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var cases []corpusCase
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 16<<20)
	for scanner.Scan() {
		var item corpusCase
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatalf("corpus line %d: %v", len(cases)+1, err)
		}
		cases = append(cases, item)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

func clip(text string) string {
	if len(text) > 300 {
		return text[:300] + "…"
	}
	return text
}

// TestCorpusEncoders: every encoder is byte-identical to CPython 3.11 (and
// Flask 3.0 for Response) for every value of the corpus, and the values
// survive parse → encode → parse unchanged.
func TestCorpusEncoders(t *testing.T) {
	count := 0
	for _, item := range loadCorpus(t) {
		if item.Kind != "value" {
			continue
		}
		count++
		checkValueCase(t, item)
	}
	if count < 500 {
		t.Fatalf("only %d value cases in the corpus", count)
	}
}

// TestCorpusErrors: what json.loads rejects is rejected with the same
// message and position.
func TestCorpusErrors(t *testing.T) {
	count := 0
	for _, item := range loadCorpus(t) {
		if item.Kind != "error" {
			continue
		}
		count++
		checkErrorCase(t, item)
	}
	if count < 500 {
		t.Fatalf("only %d error cases in the corpus", count)
	}
}

// TestCorpusFloats: Float(f) is json.dumps(f) for floats made in Go, and a
// parsed float is written back the same way.
func TestCorpusFloats(t *testing.T) {
	count := 0
	for _, item := range loadCorpus(t) {
		if item.Kind != "floats" {
			continue
		}
		for _, pair := range item.Items {
			count++
			raw, err := hex.DecodeString(pair[0])
			if err != nil || len(raw) != 8 {
				t.Fatalf("bad float bits %q", pair[0])
			}
			var bits uint64
			for _, b := range raw {
				bits = bits<<8 | uint64(b)
			}
			f := math.Float64frombits(bits)
			if got := Float(f); string(got) != pair[1] {
				t.Errorf("Float(%s) = %s, CPython %s", pair[0], got, pair[1])
			}
			text, err := Compact([]any{f, Float(f)})
			if want := "[" + pair[1] + "," + pair[1] + "]"; err != nil || string(text) != want {
				t.Errorf("Compact(float %s) = %s (%v), want %s", pair[0], text, err, want)
			}
			number, err := ParseNumber(pair[1])
			if err != nil || string(number) != pair[1] {
				t.Errorf("ParseNumber(%s) = %s (%v)", pair[1], number, err)
			}
			if back := number.Float64(); math.Float64bits(back) != bits && !(math.IsNaN(back) && math.IsNaN(f)) {
				t.Errorf("%s does not round-trip: %x", pair[1], math.Float64bits(back))
			}
		}
	}
	if count < 2000 {
		t.Fatalf("only %d floats in the corpus", count)
	}
}

// TestCorpusEqual: Equal is Python's == for every pair of the matrix.
func TestCorpusEqual(t *testing.T) {
	for _, item := range loadCorpus(t) {
		if item.Kind != "equal" {
			continue
		}
		for i, left := range item.Values {
			a, err := ParseString(left)
			if err != nil {
				t.Fatal(err)
			}
			for j, right := range item.Values {
				b, _ := ParseString(right)
				if want := item.Matrix[i][j] == '1'; Equal(a, b) != want {
					t.Errorf("Equal(%s, %s) = %v, Python %v", left, right, !want, want)
				}
			}
		}
		return
	}
	t.Fatal("no equality matrix in the corpus")
}

// checkValueCase runs every check of TestCorpusEncoders on one value case.
func checkValueCase(t *testing.T, item corpusCase) {
	t.Helper()
	value, err := ParseString(item.Input)
	if err != nil {
		t.Errorf("%s: Parse(%q): %v", item.Name, clip(item.Input), err)
		return
	}
	check := func(what string, want *string, options Options, suffix string) {
		if want == nil {
			return
		}
		got, err := Encode(value, options)
		if err != nil {
			t.Errorf("%s: %s: %v", item.Name, what, err)
		} else if string(got)+suffix != *want {
			t.Errorf("%s: %s of %q\n got %q\nwant %q", item.Name, what, clip(item.Input), clip(string(got)), clip(*want))
		}
	}
	check("compact", item.Compact, Options{Compact: true}, "")
	check("indent2", item.Indent2, Options{Indent: 2}, "")
	check("sorted", item.Sorted, Options{SortKeys: true, Compact: true}, "")
	check("sorted_spaced", item.SortedSpaced, Options{SortKeys: true}, "")
	if !item.Lone { // \ud800 cannot be reproduced; see the package comment
		check("response", item.Response, Options{EnsureASCII: true, SortKeys: true, Compact: true}, "\n")
		check("default", item.Default, Options{EnsureASCII: true}, "")
		check("indent1", item.Indent1, Options{EnsureASCII: true, SortKeys: true, Compact: true, Indent: 1}, "")
		if item.Repr != nil && Repr(value) != *item.Repr {
			t.Errorf("%s: Repr\n got %q\nwant %q", item.Name, clip(Repr(value)), clip(*item.Repr))
		}
		if item.Str != nil && Str(value) != *item.Str {
			t.Errorf("%s: Str\n got %q\nwant %q", item.Name, clip(Str(value)), clip(*item.Str))
		}
	}
	if item.Truthy != nil && Truthy(value) != *item.Truthy {
		t.Errorf("%s: Truthy = %v, want %v", item.Name, Truthy(value), *item.Truthy)
	}

	// The named encoders are the same option sets.
	named := []struct {
		encode func(any) ([]byte, error)
		want   *string
	}{{Compact, item.Compact}, {Indent2, item.Indent2}, {Sorted, item.Sorted}}
	if !item.Lone {
		named = append(named, struct {
			encode func(any) ([]byte, error)
			want   *string
		}{Response, item.Response}, struct {
			encode func(any) ([]byte, error)
			want   *string
		}{Default, item.Default})
	}
	for i, pair := range named {
		if pair.want == nil {
			continue
		}
		if got, err := pair.encode(value); err != nil || string(got) != *pair.want {
			t.Errorf("%s: named encoder %d differs from CPython (%v)", item.Name, i, err)
		}
	}

	// Round trips: what was written reads back as the same value and
	// is written the same again, from every layout.
	compact, err := Compact(value)
	if err != nil {
		return
	}
	for _, options := range []Options{{Compact: true}, {Indent: 2}, {EnsureASCII: true}, {SortKeys: true}} {
		text, _ := Encode(value, options)
		again, err := Parse(text)
		if err != nil {
			t.Errorf("%s: reparse %+v: %v", item.Name, options, err)
			continue
		}
		if !options.SortKeys {
			if second, _ := Compact(again); string(second) != string(compact) {
				t.Errorf("%s: round trip through %+v changed the value", item.Name, options)
			}
		}
		if string(compact) != "NaN" && !Equal(value, again) {
			t.Errorf("%s: round trip through %+v is not Equal", item.Name, options)
		}
	}
	if copied, _ := Compact(DeepCopy(value)); string(copied) != string(compact) {
		t.Errorf("%s: DeepCopy changed the value", item.Name)
	}
}

// checkErrorCase runs the checks of TestCorpusErrors on one error case.
func checkErrorCase(t *testing.T, item corpusCase) {
	t.Helper()
	value, err := ParseString(item.Input)
	if err == nil {
		text, _ := Compact(value)
		t.Errorf("Parse(%q) = %s, CPython: %s", clip(item.Input), clip(string(text)), item.Error)
		return
	}
	if err.Error() != item.Error {
		t.Errorf("Parse(%q)\n got %s\nwant %s", clip(item.Input), err, item.Error)
	}
	if _, ok := err.(*SyntaxError); ok != (item.Type == "JSONDecodeError") {
		t.Errorf("Parse(%q): error type %T, CPython %s", clip(item.Input), err, item.Type)
	}
}
