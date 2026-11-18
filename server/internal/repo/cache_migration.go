package repo

import (
	"configflow/internal/ctxx"
	"context"
	"errors"
	"os"
	"time"
)

// WriteSharedTextIfMissing installs validated migration data only if the target
// is still absent under its file lock. The original cache timestamp is attached
// to the temporary file before rename, so a migration cannot extend freshness.
// The caller owns content/declaration validation. Existing files are untouched.
func (r *Repository) WriteSharedTextIfMissing(ctx context.Context, relativePath string, data []byte, modified time.Time) (bool, error) {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	path, err := r.SharedPath(relativePath)
	if err != nil {
		return false, err
	}
	installed := false
	err = withFile(ctx, path, func() error {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		stage := func(path string, data []byte) (string, error) {
			temporary, err := writeTemp(path, data)
			if err != nil {
				return "", err
			}
			if err = os.Chtimes(temporary, modified, modified); err != nil {
				os.Remove(temporary)
				return "", err
			}
			return temporary, nil
		}
		if err := writeAtomicUsing(ctx, path, data, stage, os.Rename); err != nil {
			return err
		}
		installed = true
		return nil
	})
	return installed, err
}
