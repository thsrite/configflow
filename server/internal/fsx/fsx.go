// Package fsx replaces files atomically: the new content is written to a
// temporary file in the destination directory, flushed to disk, renamed over
// the destination and the directory entry is flushed too, so a crash leaves
// either the old or the new file, never a partial one. It does not lock:
// callers serialise writers of the same path themselves.
package fsx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// Options controls how the temporary file is created.
type Options struct {
	// Perm is the mode of the new file before the process umask, as for
	// os.OpenFile; zero means 0o600 (os.CreateTemp's mode).
	Perm fs.FileMode
	// ExactPerm applies Perm with Chmod, ignoring the umask.
	ExactPerm bool
	// Pattern names the temporary file as for os.CreateTemp; empty means
	// "."+base(path)+".*.tmp".
	Pattern string
}

// Temp is a written and flushed file next to its destination that is not
// visible under the destination name yet.
type Temp struct {
	name string
	done bool
}

// Name is the temporary file's path.
func (t *Temp) Name() string { return t.name }

func create(dir, pattern string, perm fs.FileMode) (*os.File, error) {
	prefix, suffix := pattern, ""
	for i := len(pattern) - 1; i >= 0; i-- {
		if pattern[i] == '*' {
			prefix, suffix = pattern[:i], pattern[i+1:]
			break
		}
	}
	for range 10000 {
		random := strconv.FormatUint(rand.Uint64(), 36)
		file, err := os.OpenFile(filepath.Join(dir, prefix+random+suffix), os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return file, err
	}
	return nil, &fs.PathError{Op: "createtemp", Path: filepath.Join(dir, prefix+"*"+suffix), Err: fs.ErrExist}
}

// Stage writes data to a new temporary file in the directory of path and
// flushes and closes it. The caller must Commit or Discard the result.
func Stage(ctx context.Context, path string, data []byte, opts Options) (*Temp, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	perm := opts.Perm
	if perm == 0 {
		perm = 0o600
	}
	pattern := opts.Pattern
	if pattern == "" {
		pattern = "." + filepath.Base(path) + ".*.tmp"
	}
	file, err := create(filepath.Dir(path), pattern, perm)
	if err != nil {
		return nil, fmt.Errorf("create temporary file: %w", err)
	}
	t := &Temp{name: file.Name()}
	fail := func(step string, err error) (*Temp, error) {
		file.Close()
		os.Remove(t.name)
		return nil, fmt.Errorf("%s temporary file: %w", step, err)
	}
	if opts.ExactPerm {
		if err := file.Chmod(perm); err != nil {
			return fail("chmod", err)
		}
	}
	if _, err := file.Write(data); err != nil {
		return fail("write", err)
	}
	if err := file.Sync(); err != nil {
		return fail("sync", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(t.name)
		return nil, fmt.Errorf("close temporary file: %w", err)
	}
	return t, nil
}

// Commit renames the temporary file over path and flushes the directory. On a
// cancelled ctx or a failed rename the temporary file is removed and path is
// unchanged.
func (t *Temp) Commit(ctx context.Context, path string) error {
	return t.CommitWith(ctx, path, os.Rename)
}

// CommitWith is Commit with a replaceable rename, for tests that simulate a
// failing disk.
func (t *Temp) CommitWith(ctx context.Context, path string, rename func(oldpath, newpath string) error) error {
	if t.done {
		return errors.New("fsx: temporary file already committed or discarded")
	}
	t.done = true
	if err := ctx.Err(); err != nil {
		os.Remove(t.name)
		return err
	}
	if err := rename(t.name, path); err != nil {
		os.Remove(t.name)
		return fmt.Errorf("replace file: %w", err)
	}
	return SyncDir(filepath.Dir(path))
}

// Discard removes an uncommitted temporary file.
func (t *Temp) Discard() {
	if !t.done {
		t.done = true
		os.Remove(t.name)
	}
}

// WriteFile atomically replaces path with data (Stage then Commit). The
// directory must exist.
func WriteFile(ctx context.Context, path string, data []byte, opts Options) error {
	t, err := Stage(ctx, path, data, opts)
	if err != nil {
		return err
	}
	return t.Commit(ctx, path)
}

// SyncDir flushes a directory so that a rename or removal in it survives a
// crash. File systems that cannot flush directories (EINVAL, ENOTSUP) are
// not an error.
func SyncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	err = handle.Sync()
	closeErr := handle.Close()
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		err = nil
	}
	if err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return closeErr
}
