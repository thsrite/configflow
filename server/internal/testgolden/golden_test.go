package testgolden

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestGoldenHelperProcess exercises real failure/cleanup paths in a child Go
// test, so expected Fatal/Error calls cannot be confused with passing controls.
func TestGoldenHelperProcess(t *testing.T) {
	dir := os.Getenv("PYREF_SELFTEST_DIR")
	if dir == "" {
		t.Skip("child-only negative control")
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	value := New(t, Spec{Name: "control", Sources: []string{filepath.Join(dir, "source.py")}, Input: []byte(os.Getenv("PYREF_SELFTEST_INPUT")), Compress: true, Run: func() ([]byte, error) {
		if err := os.WriteFile("executed", []byte("yes"), 0600); err != nil {
			return nil, err
		}
		return []byte(os.Getenv("PYREF_SELFTEST_OUTPUT")), nil
	}}).Output(t)
	if string(value) != os.Getenv("PYREF_SELFTEST_WANT") {
		t.Fatalf("original assertion: %q", value)
	}
}

func TestRecordingAndReplayNegativeControls(t *testing.T) {
	dir, err := os.MkdirTemp(".", "pyref-control-")
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	source := filepath.Join(dir, "source.py")
	if err = os.WriteFile(source, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(dir, "venv/lib/python3.11/site-packages/example.dist-info/METADATA")
	if err = os.MkdirAll(filepath.Dir(metadataPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(metadataPath, []byte("Name: example\nVersion: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(mode, accept, input, output, want string, success bool) string {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestGoldenHelperProcess$", "-test.v")
		cmd.Env = append(os.Environ(), "PYREF_SELFTEST_DIR="+dir, "CONFIGFLOW_DIFF_PYTHON="+filepath.Join(dir, "venv/bin/python"), "PYREF_MODE="+mode, "PYREF_ACCEPT="+accept, "PYREF_SELFTEST_INPUT="+input, "PYREF_SELFTEST_OUTPUT="+output, "PYREF_SELFTEST_WANT="+want)
		data, err := cmd.CombinedOutput()
		if (err == nil) != success {
			t.Fatalf("mode=%s success=%v err=%v\n%s", mode, success, err, data)
		}
		return string(data)
	}
	path := filepath.Join(dir, "testdata/pyref/control.json.gz")
	run("record", "", "input", "answer", "answer", false)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unaccepted new recording was written")
	}
	run("record", "1", "input", "answer", "answer", true)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, "executed")); err != nil {
		t.Fatal(err)
	}
	run("", "", "input", "unreachable", "answer", true)
	if _, err := os.Stat(filepath.Join(dir, "executed")); !os.IsNotExist(err) {
		t.Fatal("ordinary mode executed callback")
	}
	run("check", "", "input", "answer", "answer", true)
	for _, mode := range []string{"check", "record"} {
		text := run(mode, "", "input", "changed", "changed", false)
		if !strings.Contains(text, "PYREF DIFF") {
			t.Fatal("output mutation did not list diff")
		}
		data, _ := os.ReadFile(path)
		if !bytes.Equal(data, original) {
			t.Fatal("unaccepted diff overwrote golden")
		}
	}
	run("record", "1", "input", "changed", "assertion fails", false)
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, original) {
		t.Fatal("failed original assertion overwrote golden")
	}
	text := run("", "", "different-input", "answer", "answer", false)
	if !strings.Contains(text, "Python 参考已变化，请重新录制") {
		t.Fatal("input drift did not fail")
	}
	if err = os.WriteFile(source, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	text = run("", "", "input", "answer", "answer", false)
	if !strings.Contains(text, "Python 参考已变化，请重新录制") {
		t.Fatal("source drift did not fail")
	}
	if err = os.WriteFile(source, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(metadataPath, []byte("Name: example\nVersion: 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	text = run("", "", "input", "answer", "answer", false)
	if !strings.Contains(text, "Python 参考已变化，请重新录制") {
		t.Fatal("venv drift did not fail")
	}
}
