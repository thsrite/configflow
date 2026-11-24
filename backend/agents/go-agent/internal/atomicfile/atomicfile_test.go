package atomicfile

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func assertOnlyEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	got := make(map[string]bool, len(entries))
	for _, entry := range entries {
		got[entry.Name()] = true
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected files in %s: got %v, want %v", dir, got, want)
	}
	for _, name := range want {
		if !got[name] {
			t.Fatalf("missing expected file %s in %s (got %v)", name, dir, got)
		}
	}
}

func TestWriteAndPermissions(t *testing.T) {
	tests := []struct {
		name     string
		initial  []byte
		initialM os.FileMode
		data     []byte
		perm     os.FileMode
		pattern  string
	}{
		{
			name:    "create 0600",
			data:    []byte("secret\n"),
			perm:    0o600,
			pattern: ".agent-config-*",
		},
		{
			name:     "overwrite and update mode",
			initial:  []byte("old"),
			initialM: 0o600,
			data:     []byte("new content\n"),
			perm:     0o644,
		},
		{
			name: "empty file 0755",
			data: nil,
			perm: 0o755,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target.conf")
			if tc.initial != nil {
				if err := os.WriteFile(target, tc.initial, tc.initialM); err != nil {
					t.Fatalf("seed file: %v", err)
				}
			}
			var syncedDirs []string
			err := WriteWith(target, tc.data, tc.perm, Options{
				Pattern: tc.pattern,
				SyncDir: func(d string) error {
					syncedDirs = append(syncedDirs, d)
					return SyncDir(d)
				},
			})
			if err != nil {
				t.Fatalf("WriteWith failed: %v", err)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("read target: %v", err)
			}
			if !bytes.Equal(got, tc.data) {
				t.Fatalf("content mismatch: got %q, want %q", got, tc.data)
			}
			info, err := os.Stat(target)
			if err != nil {
				t.Fatalf("stat target: %v", err)
			}
			if info.Mode().Perm() != tc.perm {
				t.Fatalf("perm mismatch: got %o, want %o", info.Mode().Perm(), tc.perm)
			}
			if len(syncedDirs) != 1 || syncedDirs[0] != dir {
				t.Fatalf("expected SyncDir(%s) once, got %v", dir, syncedDirs)
			}
			assertOnlyEntries(t, dir, "target.conf")
		})
	}
}

func TestWriteOptions(t *testing.T) {
	t.Run("exclusive creates new and rejects existing", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "id")
		if err := WriteWith(target, []byte("first\n"), 0o600, Options{Exclusive: true}); err != nil {
			t.Fatalf("first exclusive write: %v", err)
		}
		err := WriteWith(target, []byte("second\n"), 0o600, Options{Exclusive: true})
		if !errors.Is(err, os.ErrExist) {
			t.Fatalf("second exclusive write: got %v, want os.ErrExist", err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read target: %v", err)
		}
		if string(got) != "first\n" {
			t.Fatalf("existing file mutated: got %q", got)
		}
		assertOnlyEntries(t, dir, "id")
	})

	t.Run("before commit aborts and cleans temp", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "tun.conf")
		if err := WriteWith(target, []byte("original\n"), 0o644, Options{}); err != nil {
			t.Fatalf("seed write: %v", err)
		}
		sentinel := errors.New("changed concurrently")
		err := WriteWith(target, []byte("updated\n"), 0o644, Options{
			BeforeCommit: func() error { return sentinel },
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got %v", err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read target: %v", err)
		}
		if string(got) != "original\n" {
			t.Fatalf("target changed after aborted commit: got %q", got)
		}
		assertOnlyEntries(t, dir, "tun.conf")
	})

	t.Run("skip dir sync", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "batch.dat")
		called := false
		err := WriteWith(target, []byte("ok"), 0o600, Options{
			SkipDirSync: true,
			SyncDir: func(string) error {
				called = true
				return nil
			},
		})
		if err != nil {
			t.Fatalf("WriteWith failed: %v", err)
		}
		if called {
			t.Fatal("SyncDir should not be called when SkipDirSync is true")
		}
	})

	t.Run("set owner noop for current uid/gid", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "owned")
		err := WriteWith(target, []byte("owned"), 0o600, Options{
			SetOwner: true,
			UID:      os.Getuid(),
			GID:      os.Getgid(),
		})
		if err != nil {
			t.Fatalf("WriteWith SetOwner current uid/gid failed: %v", err)
		}
		assertOnlyEntries(t, dir, "owned")
	})
}

func TestWriteFailuresCleanTemp(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string) (string, io.Reader)
		entries []string
	}{
		{
			name: "reader error",
			setup: func(t *testing.T, dir string) (string, io.Reader) {
				t.Helper()
				return filepath.Join(dir, "out.txt"), errReader{err: errors.New("read boom")}
			},
		},
		{
			name: "rename over non-empty directory fails",
			setup: func(t *testing.T, dir string) (string, io.Reader) {
				t.Helper()
				sub := filepath.Join(dir, "occupied")
				if err := os.Mkdir(sub, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(sub, "child"), []byte("x"), 0o600); err != nil {
					t.Fatalf("write child: %v", err)
				}
				return sub, bytes.NewReader([]byte("data"))
			},
			entries: []string{"occupied"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target, r := tc.setup(t, dir)
			if err := WriteFrom(target, r, 0o600, Options{}); err == nil {
				t.Fatal("expected error, got nil")
			}
			assertOnlyEntries(t, dir, tc.entries...)
		})
	}

	t.Run("missing parent directory", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "missing", "file")
		if err := WriteWith(target, []byte("x"), 0o600, Options{}); err == nil {
			t.Fatal("expected error for missing parent directory")
		}
	})
}

func TestSyncUnsupportedOK(t *testing.T) {
	other := errors.New("disk failure")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"nil", nil, nil},
		{"EINVAL", &os.PathError{Op: "sync", Path: "d", Err: syscall.EINVAL}, nil},
		{"ENOTSUP", &os.PathError{Op: "sync", Path: "d", Err: syscall.ENOTSUP}, nil},
		{"EIO", syscall.EIO, syscall.EIO},
		{"other", other, other},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := syncUnsupportedOK(tc.err); !errors.Is(got, tc.want) || (got == nil) != (tc.want == nil) {
				t.Fatalf("syncUnsupportedOK(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
