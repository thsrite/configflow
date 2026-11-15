package urlx

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

type pyClassErr struct{}

func (pyClassErr) Error() string       { return "x" }
func (pyClassErr) PythonClass() string { return "ReadTimeout" }

// Regression (review C02): references from CPython 3.11.17 for host
// lower-casing on Python's Unicode tables (Final_Sigma context with
// Other_Uppercase letters, letters newer than Unicode 14), ipaddress scope-id
// rules, and requests' URL preparation (used by localhttp).
func TestReviewC02References(t *testing.T) {
	data, err := os.ReadFile("testdata/review.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		URLs    [][2]string `json:"urls"`
		Prepare [][2]any    `json:"prepare"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	for _, row := range ref.URLs {
		if got := SafeURLForLog(row[0]); got != row[1] {
			t.Errorf("SafeURLForLog(%q) = %q, want %q", row[0], got, row[1])
		}
	}
	for _, row := range ref.Prepare {
		in := row[0].(string)
		got, err := RequestsPrepareURL(in, nil)
		want, ok := row[1].(string)
		if !ok {
			if err == nil {
				t.Errorf("prepare(%q) = %q, want %v", in, got, row[1])
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("prepare(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// Regression (review C02): an error that knows its Python class is reported
// with it (type(error).__name__), also when wrapped.
func TestSafeExceptionDetailsUsesPythonClass(t *testing.T) {
	if got := SafeExceptionDetails(pyClassErr{}); got != "exception_type=ReadTimeout" {
		t.Errorf("got %q", got)
	}
	wrapped := errors.Join(pyClassErr{})
	if got := SafeExceptionDetails(wrapped); got != "exception_type=ReadTimeout" {
		t.Errorf("wrapped: got %q", got)
	}
}
