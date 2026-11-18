package testgolden

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExactReplayDoesNotExecute(t *testing.T) {
	r := &Reference{old: document{Entries: map[string]entry{digest([]byte("input")): {Input: []byte("input"), Output: []byte("answer")}}}, seen: map[string]bool{}}
	got := r.Call(t, []byte("input"), func() ([]byte, error) { t.Fatal("ordinary replay executed reference"); return nil, nil })
	if !bytes.Equal(got, []byte("answer")) {
		t.Fatalf("got %q", got)
	}
	got[0] = 'X'
	if !bytes.Equal(r.old.Entries[digest([]byte("input"))].Output, []byte("answer")) {
		t.Fatal("replay aliases golden bytes")
	}
}
func TestCheckDetectsChangesWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "golden.json")
	original := []byte("original")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	r := &Reference{path: path, mode: "check", old: document{Entries: map[string]entry{digest([]byte("a")): {Input: []byte("a"), Output: []byte("old")}}}, next: document{Entries: map[string]entry{}}, seen: map[string]bool{}}
	got := r.Call(t, []byte("a"), func() ([]byte, error) { return []byte("new"), nil })
	if string(got) != "new" {
		t.Fatal("live callback was bypassed")
	}
	// Compare the exact document as finish does; do not deliberately fail a child
	// testing.T (that would fail the parent and hide this negative control).
	old, _ := json.Marshal(r.old)
	next, _ := json.Marshal(r.next)
	if bytes.Equal(old, next) {
		t.Fatal("changed reference was not detected")
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("check overwrote golden")
	}
}
func TestSourceHashDetectsChanges(t *testing.T) {
	if digest([]byte("backend-a")) == digest([]byte("backend-b")) {
		t.Fatal("source hash collision")
	}
	if digest([]byte("input-a")) == digest([]byte("input-b")) {
		t.Fatal("input hash collision")
	}
}
