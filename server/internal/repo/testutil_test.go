package repo

// Helpers shared by the test files of this package. Add file-specific
// helpers to your own _test.go file with a distinctive prefix.

import (
	"context"
	"errors"
	"os"
	"testing"

	"configflow/internal/jsonx"
)

var bg = context.Background()

// openRepo is ProfileRepository(tmp_path): no factories.
func openRepo(t *testing.T, dataDir string) *Repository {
	t.Helper()
	r, err := Open(dataDir, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Every View callback in the tests is checked for read-only use.
	r.viewGuard = func() { t.Error("a View callback modified the shared snapshot") }
	return r
}

// newRepo opens a repository in a fresh temporary data directory.
func newRepo(t *testing.T) *Repository {
	t.Helper()
	return openRepo(t, t.TempDir())
}

// must returns a checker for the (value, err) pair of a call: must(f())(t).
func must[T any](value T, err error) func(testing.TB) T {
	return func(t testing.TB) T {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return value
	}
}

func noError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// wantError asserts pytest.raises(kind, match=message): err is of kind and,
// when message is not empty, its text contains message.
func wantError(t testing.TB, err error, kind error, message string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %v containing %q, got nil", kind, message)
	}
	if kind != nil && !errors.Is(err, kind) {
		t.Fatalf("expected error kind %v, got %T: %v", kind, err, err)
	}
	if message != "" && !containsText(err.Error(), message) {
		t.Fatalf("expected error containing %q, got %q", message, err.Error())
	}
}

func containsText(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

// wantEqual asserts Python == on jsonx values.
func wantEqual(t testing.TB, got, want any) {
	t.Helper()
	if !jsonx.Equal(got, want) {
		t.Fatalf("got  %s\nwant %s", jsonx.Repr(got), jsonx.Repr(want))
	}
}

// parse is json.loads of a literal.
func parse(t testing.TB, text string) *jsonx.Object {
	t.Helper()
	value, err := jsonx.ParseString(text)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return value.(*jsonx.Object)
}

func readFileString(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}
