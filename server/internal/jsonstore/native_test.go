package jsonstore

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFilesRoundTripBothDirectionsWithPython(t *testing.T) {
	home, _ := os.UserHomeDir()
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	store := New(t.TempDir())
	input := struct {
		Text     string      `json:"text"`
		Number   json.Number `json:"number"`
		Optional *int        `json:"optional"`
	}{"中文 <>&\u2028\u0085", json.Number("100000000000000000001"), nil}
	if _, err := store.Write("go", input, true); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-c", `import json,sys; p=json.load(open(sys.argv[1])); assert p=={'text':'中文 <>&\u2028\u0085','number':100000000000000000001,'optional':None}; json.dump(p,open(sys.argv[2],'w'),ensure_ascii=False)`, store.Path("go"), store.Path("python"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var got struct {
		Text     string      `json:"text"`
		Number   json.Number `json:"number"`
		Optional *int        `json:"optional"`
	}
	if !store.Read("python", &got) || got.Text != input.Text || got.Number != input.Number || got.Optional != nil {
		t.Fatalf("%+v", got)
	}
}

func TestLegacyNonFiniteFileRetainsFiniteSiblings(t *testing.T) {
	s := New(t.TempDir())
	if err := os.WriteFile(s.Path("a"), []byte(`{"status":null,"days":{"2026-11-18":{"good":42,"bad":NaN,"items":[Infinity,-Infinity]}},"text":"NaN"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var data map[string]json.RawMessage
	if !s.Read("a", &data) {
		t.Fatal("lost entire legacy file")
	}
	if _, err := s.Write("a", data, true); err != nil {
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
