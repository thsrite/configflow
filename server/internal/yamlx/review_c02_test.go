package yamlx

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

type reviewLine struct {
	Text   string      `json:"text"`
	Safe   loadOutcome `json:"safe"`
	Native loadOutcome `json:"native"`
	CSafe  loadOutcome `json:"csafe"`
}

// normalizeKept is enc() of gen_review.py: like normalize, plus *Mapping as
// {"$pairs": [[key, value], ...]} and NaN as "NaN-float".
func normalizeKept(v any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		out := jsonx.NewObject()
		for k, item := range x.All() {
			out.Set(k, normalizeKept(item))
		}
		return out
	case *Mapping:
		pairs := make([]any, len(x.Pairs))
		for i, p := range x.Pairs {
			pairs[i] = []any{normalizeKept(p.Key), normalizeKept(p.Value)}
		}
		return jsonx.Obj("$pairs", pairs)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeKept(item)
		}
		return out
	case Timestamp:
		return jsonx.Obj("$ts", x.String())
	case []byte:
		return jsonx.Obj("$bytes", hex.EncodeToString(x))
	case jsonx.Number:
		if x.IsFloat() && math.IsNaN(x.Float64()) {
			return "NaN-float"
		}
	}
	return v
}

func checkReview(t *testing.T, name, text string, want loadOutcome, got any, err error) bool {
	t.Helper()
	switch {
	case want.Value != nil:
		if err != nil {
			t.Errorf("%s %q: Python loaded %s, Go failed: %v", name, text, *want.Value, err)
			return false
		}
		expected, perr := jsonx.ParseString(*want.Value)
		if perr != nil {
			t.Fatal(perr)
		}
		wantBytes, _ := jsonx.Compact(expected)
		gotBytes, cerr := jsonx.Compact(normalizeKept(got))
		if cerr != nil || string(gotBytes) != string(wantBytes) {
			t.Errorf("%s %q:\n got %s %v\nwant %s", name, text, gotBytes, cerr, wantBytes)
			return false
		}
	case want.Special != "" && err == nil && (strings.HasPrefix(want.Special, "Exceeds the limit") || strings.Contains(want.Special, "recursion depth")):
		// Generator artefacts: Python loaded the value and only json.dumps or
		// enc() failed on it (an int over 4300 digits, 600 nested lists).
	case want.Special != "":
		var unrepresentable *UnrepresentableError
		if !errors.As(err, &unrepresentable) {
			t.Errorf("%s %q: Python gave a value jsonx cannot hold (%s), Go: %T %v", name, text, want.Special, got, err)
			return false
		}
	default:
		var pyErr *Error
		if !errors.As(err, &pyErr) || pyErr.Class != want.Error || pyErr.Msg != want.Message {
			t.Errorf("%s %q:\n got %T %v\nwant %s: %s", name, text, got, err, want.Error, want.Message)
			return false
		}
	}
	return true
}

// csafeKnown are texts on which gopkg.in/yaml.v3 and libyaml differ in a
// way the conversion cannot see (accepted difference of LoadCSafeFirst): an
// empty node with the non-specific tag "!" is "!!null" in yaml.v3's tree,
// libyaml's composer makes it a str.
var csafeKnown = map[string]bool{"! \n": true, "k: ! \n": true}

// TestReviewC02References: PyYAML 6.0.1 (CPython 3.11.17) on constructor edge
// cases and the former yaml.v3 gaps (testdata/gen_review.py), for
// safe_load, NativeLoader and the CSafeLoader-first pattern; mappings with
// non-string keys are compared through KeepNonStringKeys.
func TestReviewC02References(t *testing.T) {
	file, err := os.Open("testdata/review.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	counts := map[string]int{}
	total := 0
	for scanner.Scan() {
		var line reviewLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		total++
		got, err := LoadWith(line.Text, LoadOptions{KeepNonStringKeys: true})
		if checkReview(t, "safe_load", line.Text, line.Safe, got, err) {
			counts["safe"]++
		}
		got, err = LoadWith(line.Text, LoadOptions{Schema: SchemaNative, Key: nativeKeyCheck, KeepNonStringKeys: true})
		if checkReview(t, "native", line.Text, line.Native, got, err) {
			counts["native"]++
		}
		got, err = LoadCSafeFirst(line.Text, LoadOptions{KeepNonStringKeys: true})
		if csafeKnown[line.Text] {
			counts["csafe known"]++
		} else if checkReview(t, "csafe", line.Text, line.CSafe, got, err) {
			counts["csafe"]++
		}
	}
	t.Logf("%d texts; equal to Python: safe_load %d, NativeLoader %d, CSafeLoader-first %d (+%d known)", total, counts["safe"], counts["native"], counts["csafe"], counts["csafe known"])
}
