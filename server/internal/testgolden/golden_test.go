package testgolden

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExactReplayDoesNotAlias(t *testing.T) {
	r := &Reference{
		old: document{
			Version: 1,
			Entries: map[string]entry{
				digest([]byte("input")): {Input: []byte("input"), Output: []byte("answer")},
			},
		},
		seen: map[string]bool{},
	}
	got := r.Call(t, []byte("input"))
	if !bytes.Equal(got, []byte("answer")) {
		t.Fatalf("got %q", got)
	}
	got[0] = 'X'
	if !bytes.Equal(r.old.Entries[digest([]byte("input"))].Output, []byte("answer")) {
		t.Fatal("replay aliases golden bytes")
	}
}

func TestCompressedGoldenReplay(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	doc := document{
		Version: 1,
		Entries: map[string]entry{
			digest([]byte("hello")): {Input: []byte("hello"), Output: []byte("world")},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	goldenDir := filepath.Join(dir, "testdata", "pyref")
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goldenDir, "sample.json.gz"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	ref := New(t, Spec{Name: "sample", Input: []byte("hello"), Compress: true})
	ref.Assert(t, []byte("hello"), []byte("world"))
	if got := string(ref.Output(t)); got != "world" {
		t.Fatalf("got %q, want %q", got, "world")
	}
}
