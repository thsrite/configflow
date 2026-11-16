package mosdns

import (
	"encoding/json"
	"testing"
)

// Review C28 #4: integer cache settings of any size are kept like Python's
// int() (references from CPython 3.11); only invalid values fall back.
func TestReviewIntOfMatchesPythonInt(t *testing.T) {
	for raw, want := range map[string]string{
		"true": "1", "false": "0", "60.9": "60", "-0.5": "0", "1e+20": "100000000000000000000",
		"1e+30": "1000000000000000019884624838656", "100000000000000000000": "100000000000000000000",
		`"128"`: "128", `" 1_000 "`: "1000", `"1__0"`: "7", `"_1"`: "7", `"+7"`: "7", `"-3"`: "-3",
		`"12.5"`: "7", `"x"`: "7", "[1]": "7", "null": "7", "{}": "7", "1e400": "7",
	} {
		if got := intOf(json.RawMessage(raw), 7).String(); got != want {
			t.Errorf("int(%s) = %s, want %s", raw, got, want)
		}
	}
	if got := intOf(nil, 7).String(); got != "7" {
		t.Errorf("missing: %s", got)
	}
}
