package testgolden

import (
	"os"
	"testing"
)

// Open opens a test fixture file.
func Open(t testing.TB, path string) (*os.File, error) {
	t.Helper()
	return os.Open(path)
}

// ReadFile reads a test fixture file.
func ReadFile(t testing.TB, path string) ([]byte, error) {
	t.Helper()
	return os.ReadFile(path)
}

// ReadFileOutside reads a file in a temporary directory.
func ReadFileOutside(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ReadDirOutside lists a directory in a temporary directory.
func ReadDirOutside(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}
