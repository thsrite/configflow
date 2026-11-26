// Package testgolden replays committed outputs using exact input keys.
// Every recorded input must be consumed during replay.
package testgolden

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	digestx "configflow/internal/digest"
)

// Spec identifies a committed golden archive under testdata/pyref/.
type Spec struct {
	Name     string
	Input    []byte
	Compress bool
}

type entry struct {
	Input  []byte `json:"input"`
	Output []byte `json:"output"`
}

type document struct {
	Version int              `json:"version"`
	Entries map[string]entry `json:"entries"`
}

// Reference holds one test-local golden transcript. It is safe for parallel calls.
type Reference struct {
	mu   sync.Mutex
	spec Spec
	path string
	old  document
	seen map[string]bool
}

func digest(b []byte) string { return digestx.SHA256Hex(b) }

// New loads a committed golden file and checks on cleanup that every recorded
// entry was consumed.
func New(t *testing.T, spec Spec) *Reference {
	t.Helper()
	if spec.Name == "" {
		spec.Name = t.Name()
	}
	name := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(spec.Name)
	if name == "." || name == ".." || strings.ContainsAny(name, "\x00\n\r") {
		t.Fatal("invalid golden name")
	}
	suffix := ".json"
	if spec.Compress {
		suffix += ".gz"
	}
	r := &Reference{
		spec: spec,
		path: filepath.Join("testdata", "pyref", name+suffix),
		seen: map[string]bool{},
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Compress {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		data, err = io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := json.Unmarshal(data, &r.old); err != nil {
		t.Fatal(err)
	}
	if r.old.Version != 1 {
		t.Fatal("unsupported golden version")
	}
	t.Cleanup(func() { r.finish(t) })
	return r
}

// Output replays the single result configured by Spec.Input.
func (r *Reference) Output(t *testing.T) []byte {
	t.Helper()
	return r.Call(t, r.spec.Input)
}

// Call replays an exact recorded input.
func (r *Reference) Call(t *testing.T, input []byte) []byte {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	key := digest(input)
	r.seen[key] = true
	old, exists := r.old.Entries[key]
	if !exists || !bytes.Equal(old.Input, input) {
		t.Fatalf("%s: input %s absent from golden archive", r.path, key)
	}
	return bytes.Clone(old.Output)
}

// Assert compares actual against the recorded golden output for input.
func (r *Reference) Assert(t *testing.T, input, actual []byte) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	key := digest(input)
	r.seen[key] = true
	old, exists := r.old.Entries[key]
	if !exists || !bytes.Equal(old.Input, input) {
		t.Fatalf("%s: input %s absent from golden archive (input=%s)", r.path, key, string(input))
	}
	if !bytes.Equal(old.Output, actual) {
		t.Fatalf("%s: golden mismatch for input %s\nwant: %s\ngot:  %s", r.path, string(input), string(old.Output), string(actual))
	}
}

func (r *Reference) finish(t *testing.T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.old.Entries {
		if !r.seen[key] {
			t.Errorf("%s: unconsumed exact input %s", r.path, key)
		}
	}
}
