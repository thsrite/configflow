// Package testgolden records independent reference outputs and replays them without
// starting the reference runtime. Recording is opt-in and updates require acceptance.
package testgolden

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Spec identifies an oracle and all source/input material affecting its result.
// Sources are repository-relative files or directories (directories include .py
// files). Run is never called in ordinary tests. Names must be safe filenames.
type Spec struct {
	Name     string
	Sources  []string
	Input    []byte
	Run      func() ([]byte, error)
	Compress bool
}

type metadata struct {
	Sources      map[string]string `json:"sources"`
	Dependencies string            `json:"venv_dependencies_sha256"`
	Input        string            `json:"input_sha256"`
}
type entry struct {
	Input  []byte `json:"input"`
	Output []byte `json:"output"`
}
type document struct {
	Version  int              `json:"version"`
	Metadata metadata         `json:"metadata"`
	Entries  map[string]entry `json:"entries"`
}

// Reference holds one test-local reference transcript. It is safe for parallel calls.
type Reference struct {
	mu   sync.Mutex
	spec Spec
	path string
	old  document
	next document
	mode string
	seen map[string]bool
}

// Root finds the repository containing backend and server/go.mod.
func Root() (string, error) {
	here, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(here, "server/go.mod")); err == nil {
			return here, nil
		}
		parent := filepath.Dir(here)
		if parent == here {
			return "", fmt.Errorf("repository root not found")
		}
		here = parent
	}
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

var dependencyCache sync.Map

// Python returns the configured reference interpreter; it does not execute it.
func Python() string {
	if path := os.Getenv("CONFIGFLOW_DIFF_PYTHON"); path != "" {
		return path
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "cf-test/.venv/bin/python")
}
func dependencies() (string, error) {
	interpreter := Python()
	if value, ok := dependencyCache.Load(interpreter); ok {
		return value.(string), nil
	}
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(interpreter)), "lib/python*/site-packages/*.dist-info"))
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no venv dependency metadata for %s", interpreter)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, dir := range paths {
		for _, name := range []string{"METADATA", "RECORD", "WHEEL"} {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return "", err
			}
			fmt.Fprintf(h, "%s/%s\x00%s\x00", filepath.Base(dir), name, digest(data))
		}
	}
	sum := hex.EncodeToString(h.Sum(nil))
	dependencyCache.Store(interpreter, sum)
	return sum, nil
}
func sourceMetadata(spec Spec) (metadata, error) {
	root, err := Root()
	if err != nil {
		return metadata{}, err
	}
	sources := append([]string{}, spec.Sources...)
	if len(sources) == 0 {
		sources = []string{"backend"}
	}
	// The consuming test source and fixtures define inputs just as the oracle does.
	cwd, err := os.Getwd()
	if err != nil {
		return metadata{}, err
	}
	tests, err := filepath.Glob(filepath.Join(cwd, "*_test.go"))
	if err != nil {
		return metadata{}, err
	}
	sources = append(sources, tests...)
	files := map[string]string{}
	for _, source := range sources {
		if !filepath.IsAbs(source) {
			source = filepath.Join(root, source)
		}
		info, err := os.Stat(source)
		if err != nil {
			return metadata{}, err
		}
		paths := []string{source}
		if info.IsDir() {
			paths = nil
			err = filepath.WalkDir(source, func(path string, d fs.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if d.IsDir() {
					if d.Name() == "__pycache__" || d.Name() == "go-agent" {
						return filepath.SkipDir
					}
					return nil
				}
				if strings.HasSuffix(path, ".py") {
					paths = append(paths, path)
				}
				return nil
			})
			if err != nil {
				return metadata{}, err
			}
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return metadata{}, err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil || strings.HasPrefix(rel, "..") {
				return metadata{}, fmt.Errorf("source outside repository: %s", path)
			}
			files[filepath.ToSlash(rel)] = digest(data)
		}
	}
	dep, err := dependencies()
	return metadata{files, dep, digest(spec.Input)}, err
}

// New loads and validates a golden. PYREF_MODE=check reruns the real oracle and
// reports differences without writing; record additionally permits updates only
// with PYREF_ACCEPT=1. Failed tests never write golden files.
func New(t *testing.T, spec Spec) *Reference {
	t.Helper()
	if spec.Name == "" {
		spec.Name = t.Name()
	}
	name := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(spec.Name)
	if name == "." || name == ".." || strings.ContainsAny(name, "\x00\n\r") {
		t.Fatal("invalid golden name")
	}
	mode := os.Getenv("PYREF_MODE")
	if mode != "" && mode != "check" && mode != "record" {
		t.Fatalf("unknown PYREF_MODE %q", mode)
	}
	meta, err := sourceMetadata(spec)
	if err != nil {
		t.Fatal(err)
	}
	suffix := ".json"
	if spec.Compress {
		suffix += ".gz"
	}
	r := &Reference{spec: spec, path: filepath.Join("testdata", "pyref", name+suffix), mode: mode, seen: map[string]bool{}}
	r.next = document{Version: 1, Metadata: meta, Entries: map[string]entry{}}
	data, err := os.ReadFile(r.path)
	if err == nil {
		if spec.Compress {
			reader, e := gzip.NewReader(bytes.NewReader(data))
			if e != nil {
				t.Fatal(e)
			}
			data, err = io.ReadAll(reader)
			reader.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if err = json.Unmarshal(data, &r.old); err != nil {
			t.Fatal(err)
		}
		if r.old.Version != 1 {
			t.Fatal("unsupported golden version")
		}
	} else if !os.IsNotExist(err) || mode == "" {
		t.Fatalf("%s: %v; Python 参考已变化，请重新录制", r.path, err)
	}
	before, _ := json.Marshal(r.old.Metadata)
	after, _ := json.Marshal(meta)
	if mode == "" && !bytes.Equal(before, after) {
		t.Fatalf("%s: Python 参考已变化，请重新录制 (source/input/venv hash mismatch)", r.path)
	}
	t.Cleanup(func() { r.finish(t) })
	return r
}

// Output replays the single result configured by Spec.Run and Spec.Input.
func (r *Reference) Output(t *testing.T) []byte {
	t.Helper()
	return r.Call(t, r.spec.Input, r.spec.Run)
}

// Call replays an exact input, or calls run in explicit reference modes. Dynamic
// fixture addresses may be normalized by the caller before constructing input.
func (r *Reference) Call(t *testing.T, input []byte, run func() ([]byte, error)) []byte {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	key := digest(input)
	r.seen[key] = true
	old, exists := r.old.Entries[key]
	if r.mode == "" {
		if !exists || !bytes.Equal(old.Input, input) {
			t.Fatalf("%s input %s absent: Python 参考已变化，请重新录制", r.path, key)
		}
		return bytes.Clone(old.Output)
	}
	if run == nil {
		t.Fatal("live reference callback missing")
	}
	output, err := run()
	if err != nil {
		t.Fatalf("live reference: %v", err)
	}
	if previous, ok := r.next.Entries[key]; ok && !bytes.Equal(previous.Output, output) {
		t.Fatalf("nondeterministic reference output for %s input %s", r.path, key)
	}
	r.next.Entries[key] = entry{bytes.Clone(input), bytes.Clone(output)}
	if !exists || !bytes.Equal(old.Output, output) {
		t.Logf("PYREF DIFF %s input %s\n- %q\n+ %q", r.path, key, old.Output, output)
	}
	return output
}
func (r *Reference) finish(t *testing.T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mode == "" {
		return
	}
	old, _ := json.Marshal(r.old)
	next, _ := json.Marshal(r.next)
	if bytes.Equal(old, next) {
		t.Logf("PYREF equal: %s (%d exact inputs)", r.path, len(r.next.Entries))
		return
	}
	keys := make([]string, 0, len(r.next.Metadata.Sources))
	for path, hash := range r.next.Metadata.Sources {
		if r.old.Metadata.Sources[path] != hash {
			keys = append(keys, path)
		}
	}
	for path := range r.old.Metadata.Sources {
		if _, ok := r.next.Metadata.Sources[path]; !ok {
			keys = append(keys, path)
		}
	}
	sort.Strings(keys)
	for _, path := range keys {
		t.Logf("PYREF source %s: %s -> %s", path, r.old.Metadata.Sources[path], r.next.Metadata.Sources[path])
	}
	if r.old.Metadata.Dependencies != r.next.Metadata.Dependencies {
		t.Logf("PYREF venv dependencies: %s -> %s", r.old.Metadata.Dependencies, r.next.Metadata.Dependencies)
	}
	if r.old.Metadata.Input != r.next.Metadata.Input {
		t.Logf("PYREF declared input: %s -> %s", r.old.Metadata.Input, r.next.Metadata.Input)
	}
	t.Logf("PYREF metadata/entries changed: %s (old=%d new=%d)", r.path, len(r.old.Entries), len(r.next.Entries))
	if r.mode != "record" || os.Getenv("PYREF_ACCEPT") != "1" {
		t.Errorf("Python 参考已变化，请重新录制: review PYREF DIFF; explicit PYREF_MODE=record PYREF_ACCEPT=1 required")
		return
	}
	if t.Failed() {
		t.Log("PYREF: failed test; golden not overwritten")
		return
	}
	data, err := json.MarshalIndent(r.next, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		t.Fatal(err)
	}
	temp, err := os.CreateTemp(filepath.Dir(r.path), ".pyref-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(temp.Name())
	if r.spec.Compress {
		writer := gzip.NewWriter(temp)
		if _, err = writer.Write(append(data, '\n')); err != nil {
			temp.Close()
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			temp.Close()
			t.Fatal(err)
		}
	} else if _, err = temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		t.Fatal(err)
	}
	if err = temp.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(temp.Name(), r.path); err != nil {
		t.Fatal(err)
	}
	t.Logf("PYREF accepted: %s", r.path)
}
