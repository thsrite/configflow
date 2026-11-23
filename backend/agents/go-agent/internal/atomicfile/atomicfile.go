// Package atomicfile writes files atomically through a same-directory temporary
// file, fsyncing the file before publishing and fsyncing the parent directory
// afterwards. It does not create parent directories or manage file locks.
package atomicfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Options configures optional atomic write behaviour. Zero values select
// default rename semantics, os.CreateTemp with pattern ".tmp-*", no ownership
// changes, and SyncDir on the parent directory.
type Options struct {
	// Pattern is passed to os.CreateTemp in the destination directory.
	// Defaults to ".tmp-*". Ignored when CreateTemp is set.
	Pattern string

	// CreateTemp overrides temporary file creation when callers require a
	// specific filename convention (for example, crash-recovery prefixes).
	CreateTemp func(dir string) (*os.File, error)

	// SetOwner enables applying UID and GID to the temporary file before
	// publishing when they differ from the temporary file's current owner.
	SetOwner bool
	UID      int
	GID      int

	// Exclusive publishes with os.Link instead of os.Rename so an existing
	// target (or symlink) is never overwritten.
	Exclusive bool

	// BeforeCommit, if non-nil, runs after the temporary file is synced and
	// closed and immediately before Rename/Link. Returning an error aborts
	// the write and removes the temporary file.
	BeforeCommit func() error

	// SkipDirSync skips fsyncing the parent directory after publishing (used
	// by callers that batch-sync directories once after multiple writes).
	SkipDirSync bool

	// SyncDir overrides directory fsync when non-nil (for example, test
	// instrumentation). Defaults to SyncDir.
	SyncDir func(dir string) error
}

// Write atomically writes data to path with perm: it creates a temporary file
// in path's directory, sets perm, writes data, fsyncs and closes the file,
// renames it over path, and fsyncs the parent directory. On any failure the
// temporary file is removed.
func Write(path string, data []byte, perm os.FileMode) error {
	return WriteWith(path, data, perm, Options{})
}

// WriteWith atomically writes data to path with perm and opts.
func WriteWith(path string, data []byte, perm os.FileMode, opts Options) error {
	return WriteFrom(path, bytes.NewReader(data), perm, opts)
}

// WriteFrom atomically streams r into path with perm and opts.
func WriteFrom(path string, r io.Reader, perm os.FileMode, opts Options) error {
	dir := filepath.Dir(path)
	f, err := createTemp(dir, opts)
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()

	if opts.SetOwner && opts.UID >= 0 && opts.GID >= 0 {
		if info, statErr := f.Stat(); statErr != nil {
			err = statErr
		} else if oldUID, oldGID := fileOwnership(info); oldUID != opts.UID || oldGID != opts.GID {
			err = f.Chown(opts.UID, opts.GID)
		}
	}
	if err == nil {
		err = f.Chmod(perm)
	}
	if err == nil {
		_, err = io.Copy(f, r)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	if opts.BeforeCommit != nil {
		if err = opts.BeforeCommit(); err != nil {
			return err
		}
	}

	if opts.Exclusive {
		if err = os.Link(tmp, path); err != nil {
			return err
		}
	} else {
		if err = os.Rename(tmp, path); err != nil {
			return err
		}
		cleanup = false
	}

	if opts.SkipDirSync {
		return nil
	}
	syncFn := opts.SyncDir
	if syncFn == nil {
		syncFn = SyncDir
	}
	return syncFn(dir)
}

// SyncDir opens dir and calls Sync on it to persist directory entry updates.
// File systems that cannot sync a directory (EINVAL, ENOTSUP) count as
// synced, as in the server's fsx.SyncDir.
func SyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	defer f.Close()
	if err := syncUnsupportedOK(f.Sync()); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

func syncUnsupportedOK(err error) error {
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return nil
	}
	return err
}

func createTemp(dir string, opts Options) (*os.File, error) {
	if opts.CreateTemp != nil {
		return opts.CreateTemp(dir)
	}
	pattern := opts.Pattern
	if pattern == "" {
		pattern = ".tmp-*"
	}
	return os.CreateTemp(dir, pattern)
}

func fileOwnership(info os.FileInfo) (int, int) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(stat.Uid), int(stat.Gid)
	}
	return -1, -1
}
