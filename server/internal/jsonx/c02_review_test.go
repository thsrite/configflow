package jsonx

import "testing"

// Regression (review C02): repr() escapes characters that are not printable
// in CPython 3.11's Unicode 14.0.0 tables, including code points Go's newer
// tables know (references from CPython 3.11.17).
func TestReprUsesCPythonPrintableTable(t *testing.T) {
	for in, want := range map[string]string{
		"\U00011F50":   `'\U00011f50'`,                      // KAWI DIGIT ZERO (Unicode 15)
		"\U0001E4F0":   `'\U0001e4f0'`,                      // NAG MUNDARI DIGIT ZERO (Unicode 15)
		"a\u0378b":     "'a" + string(rune(92)) + "u0378b'", // unassigned
		"­":            `'\xad'`,                            // soft hyphen (Cf)
		"\U0001F600 é": "'\U0001F600 é'",
	} {
		if got := Repr(in); got != want {
			t.Errorf("Repr(%q) = %s, want %s", in, got, want)
		}
	}
}
