package jsonstore

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLegacyNonFiniteFileRetainsFiniteSiblings(t *testing.T) {
	s := New(t.TempDir())
	if err := os.WriteFile(s.Path("a"), []byte(`{"status":null,"days":{"2026-11-18":{"good":42,"bad":NaN,"items":[Infinity,-Infinity]}},"text":"NaN"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var data map[string]json.RawMessage
	if !s.Read("a", &data) {
		t.Fatal("lost entire legacy file")
	}
	if _, err := s.Write(t.Context(), "a", data, true); err != nil {
		t.Fatal(err)
	}
	output, _ := os.ReadFile(s.Path("a"))
	if !json.Valid(output) {
		t.Fatalf("%s", output)
	}
	var round map[string]json.RawMessage
	if !s.Read("a", &round) || string(round["text"]) != `"NaN"` {
		t.Fatalf("%s", output)
	}
}
