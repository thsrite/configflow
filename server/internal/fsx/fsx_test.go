package fsx

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteFile(t *testing.T) {
	umask := syscall.Umask(0o022)
	defer syscall.Umask(umask)
	tests := []struct {
		name     string
		opts     Options
		existing bool
		want     fs.FileMode
	}{
		{"default private", Options{}, false, 0o600},
		{"umask applies", Options{Perm: 0o666}, false, 0o644},
		{"exact ignores umask", Options{Perm: 0o664, ExactPerm: true}, false, 0o664},
		{"replace keeps new mode", Options{Perm: 0o600}, true, 0o600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			if tt.existing {
				if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := WriteFile(t.Context(), path, []byte("new"), tt.opts); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "new" {
				t.Fatalf("content %q %v", data, err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != tt.want {
				t.Fatalf("mode %v, want %v (%v)", info.Mode().Perm(), tt.want, err)
			}
			if names := entries(t, dir); len(names) != 1 {
				t.Fatalf("leftover files %v", names)
			}
		})
	}
}

func TestStagePatternAndDiscard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	temp, err := Stage(t.Context(), path, []byte("x"), Options{Pattern: ".write-*"})
	if err != nil {
		t.Fatal(err)
	}
	if base := filepath.Base(temp.Name()); !strings.HasPrefix(base, ".write-") || filepath.Dir(temp.Name()) != dir {
		t.Fatalf("temporary %q", temp.Name())
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("staged file visible before commit")
	}
	temp.Discard()
	if names := entries(t, dir); len(names) != 0 {
		t.Fatalf("leftover files %v", names)
	}
	if err := temp.Commit(t.Context(), path); err == nil {
		t.Fatal("commit after discard")
	}
}

func TestCommitFailureKeepsOldFile(t *testing.T) {
	tests := []struct {
		name   string
		cancel bool
		target func(dir string) string
	}{
		{"cancelled", true, func(dir string) string {
			path := filepath.Join(dir, "a.json")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{"rename onto directory", false, func(dir string) string {
			sub := filepath.Join(dir, "a.json")
			if err := os.Mkdir(sub, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "keep"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			return sub
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := tt.target(dir)
			ctx, cancel := context.WithCancel(t.Context())
			temp, err := Stage(ctx, path, []byte("new"), Options{})
			if err != nil {
				t.Fatal(err)
			}
			if tt.cancel {
				cancel()
			}
			if err := temp.Commit(ctx, path); err == nil {
				t.Fatal("commit succeeded")
			}
			cancel()
			if names := entries(t, dir); len(names) != 1 {
				t.Fatalf("leftover files %v", names)
			}
			if tt.cancel {
				if data, _ := os.ReadFile(path); string(data) != "old" {
					t.Fatalf("old file changed: %q", data)
				}
			}
		})
	}
}

func TestStageCancelledAndMissingDirectory(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Stage(ctx, filepath.Join(t.TempDir(), "a"), nil, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled stage: %v", err)
	}
	if err := WriteFile(t.Context(), filepath.Join(t.TempDir(), "missing", "a"), nil, Options{}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing directory: %v", err)
	}
}

func TestSyncDir(t *testing.T) {
	if err := SyncDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := SyncDir(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
}
