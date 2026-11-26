// Package pathx provides small filesystem path existence checks built on
// os.Stat. It does not perform file I/O or directory traversal.
package pathx

import (
	"errors"
	"io/fs"
	"os"
)

// Exists reports whether path exists (following symbolic links). Missing paths
// return (false, nil); other stat errors such as permission errors are returned
// without being swallowed.
func Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Accessible reports whether path can be stat'd (following symbolic links).
// Missing paths, dangling symlinks, invalid paths (e.g. NUL bytes) and
// permission errors all return false.
func Accessible(path string) bool {
	ok, err := Exists(path)
	return err == nil && ok
}
