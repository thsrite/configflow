package repo

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"syscall"

	"configflow/internal/ctxx"
)

// ReadSharedBytes reads an owned shared file using the existing file lease.
// Parsing belongs to the consumer, allowing typed records without a JSON bridge.
func (r *Repository) ReadSharedBytes(ctx context.Context, relativePath string) (data []byte, err error) {
	path, err := r.SharedPath(relativePath)
	if err != nil {
		return nil, err
	}
	err = withFile(ctx, path, func() error { var readErr error; data, readErr = os.ReadFile(path); return readErr })
	return data, err
}

// RemoveSharedFile deletes a shared file under the same leaf lease used by writers.
// Directories and legacy lock files are never removed implicitly.
func (r *Repository) RemoveSharedFile(ctx context.Context, relativePath string) (bool, error) {
	return r.removeSharedFileUsing(ctx, relativePath, withFile)
}
func (r *Repository) removeSharedFileUsing(ctx context.Context, relativePath string, lease func(context.Context, string, func() error) error) (removed bool, err error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	path, err := r.SharedPath(relativePath)
	if err != nil {
		return false, err
	}
	err = lease(ctx, path, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Unlink(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return &fs.PathError{Op: "remove", Path: path, Err: err}
		}
		removed = true
		return nil
	})
	return removed, err
}
