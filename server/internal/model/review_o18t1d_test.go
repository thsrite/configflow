package model

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

// TestReviewO18T1dTruthMatchesPython pins Truthy/Switch against Python's
// bool() of json.loads(value) (the reference the deleted JSONHasContent /
// JSONSwitchEnabled followed), including numbers that underflow to 0.0.
func TestReviewO18T1dTruthMatchesPython(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"null", false}, {"false", false}, {"true", true},
		{"0", false}, {"-0", false}, {"0.0", false}, {"-0.0e5", false}, {"0e5", false}, {"0E-3", false},
		{"1", true}, {"-1", true}, {"0.5", true}, {"1e400", true}, {"-1e400", true},
		{"1e-400", false}, {"-2.5e-330", false}, {"4.9e-324", true},
		{"123456789012345678901234567890", true},
		{`""`, false}, {`" "`, true}, {`"0"`, true}, {`"\u0000"`, true},
		{"[]", false}, {" [ ] ", false}, {"[0]", true}, {"[[]]", true},
		{"{}", false}, {"{ }", false}, {`{"a":null}`, true},
	}
	for _, tt := range tests {
		got, err := Truthy(json.RawMessage(tt.raw))
		if err != nil || got != tt.want {
			t.Errorf("Truthy(%s) = %v, %v; want %v", tt.raw, got, err, tt.want)
		}
		if got := Switch(json.RawMessage(tt.raw), !tt.want); got != tt.want {
			t.Errorf("Switch(%s) = %v; want %v", tt.raw, got, tt.want)
		}
	}
	// Absent is the default; enabled:null is off whatever the default.
	if !Switch(nil, true) || Switch(nil, false) || Switch(json.RawMessage(" "), false) || Switch(json.RawMessage("null"), true) {
		t.Fatal("absent/null switch")
	}
	if ok, err := Truthy(nil); ok || err != nil {
		t.Fatal("empty Truthy")
	}
	for _, bad := range []string{"nul", "[1,", "{]", `"\xff"`, "[]x"} {
		if _, err := Truthy(json.RawMessage(bad)); !errors.Is(err, ErrJSON) {
			t.Errorf("Truthy(%q) err = %v", bad, err)
		}
	}
}

// TestReviewO18T1dEncoderBoundary checks the plain-record encoder's edges.
func TestReviewO18T1dEncoderBoundary(t *testing.T) {
	type rec struct {
		Text   string      `json:"text"`
		Number json.Number `json:"number"`
		Float  float64     `json:"float,omitzero"`
	}
	data, err := Marshal(rec{Text: "<a&b>é", Number: "123456789012345678901234567890"})
	if err != nil || string(data) != `{"text":"<a&b>é","number":123456789012345678901234567890}` {
		t.Fatalf("Marshal = %s, %v", data, err)
	}
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := Marshal(rec{Float: f}); err == nil {
			t.Errorf("Marshal(%v) succeeded", f)
		}
	}
	if _, err := Marshal(rec{Text: "\xff"}); err != nil {
		t.Fatalf("invalid UTF-8 text: %v", err)
	}
	deep := json.RawMessage(strings.Repeat("[", 10001) + strings.Repeat("]", 10001))
	if _, err := Marshal(struct {
		V json.RawMessage `json:"v"`
	}{deep}); !errors.Is(err, ErrJSON) {
		t.Fatalf("deep value err = %v", err)
	}
	var fields Fields
	if err := fields.Set("v", json.RawMessage(strings.Repeat("[", 9999)+strings.Repeat("]", 9999))); err != nil {
		t.Fatal(err)
	}
	if _, err := Marshal(struct {
		F Fields `json:"f"`
	}{fields}); !errors.Is(err, ErrJSON) {
		t.Fatalf("nesting past the limit through Fields err = %v", err)
	}
	// A self-referencing ordered map is an error, not a stack overflow.
	m := &OrderedMap[any]{}
	m.Set("self", m)
	if _, err := Marshal(m); !errors.Is(err, ErrJSON) {
		t.Fatalf("cyclic map err = %v", err)
	}
	// Fields refuse duplicate member names and non-objects.
	var f Fields
	if err := json.Unmarshal([]byte(`{"a":1,"a":2}`), &f); err == nil {
		t.Fatal("duplicate member accepted")
	}
	if err := f.UnmarshalJSON([]byte(`[1]`)); !errors.Is(err, ErrType) {
		t.Fatalf("non-object err = %v", err)
	}
	if err := f.UnmarshalJSON([]byte(`{"ab":1,"c":{"d":[2]}}`)); err != nil || strings.Join(f.Keys(), ",") != "ab,c" {
		t.Fatalf("keys %v, %v", f.Keys(), err)
	}
}

// TestReviewO18T1dLenientInvalidUTF8 pins that a member kept as JSON in a
// lenient read is writable (T1c rule: invalid UTF-8 reads as U+FFFD).
func TestReviewO18T1dLenientInvalidUTF8(t *testing.T) {
	type wire struct {
		F   Fields          `json:"f"`
		Raw json.RawMessage `json:"raw"`
		S   string          `json:"s"`
	}
	var out wire
	issues, err := DecodeStored([]byte("{\"f\":{\"x\":\"a\xffb\"},\"raw\":\"c\xffd\",\"s\":\"e\xfff\"}"), &out, "")
	t.Logf("issues=%v err=%v f=%v", issues, err, out.F.Keys())
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(out)
	if err != nil {
		t.Fatalf("read value not writable: %v", err)
	}
	t.Logf("written %s", data)
}
