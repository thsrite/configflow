package substore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type runtimeTempDirectory struct{ name, path string }

func selectedRuntimeTempDirectory() runtimeTempDirectory {
	for _, name := range []string{"TMPDIR", "TEMP", "TMP"} {
		if value := os.Getenv(name); value != "" {
			if absolute, err := filepath.Abs(value); err == nil {
				value = absolute
			}
			return runtimeTempDirectory{name: name, path: value}
		}
	}
	return runtimeTempDirectory{path: os.TempDir()}
}

// RuntimeDirectoryError identifies the explicit environment setting that
// prevents use of the bundled runtime, retaining the filesystem cause.
type RuntimeDirectoryError struct {
	Variable, Path string
	Err            error
}

// Error names the setting and directory without hiding the original IO cause.
func (e *RuntimeDirectoryError) Error() string {
	detail := msgRuntimeDirectoryIO
	if e.Err != nil {
		detail = e.Err.Error()
	}
	if errors.Is(e.Err, os.ErrNotExist) {
		detail = msgRuntimeDirectoryMissing
	} else if errors.Is(e.Err, syscall.EROFS) {
		detail = msgRuntimeDirectoryReadOnly
	} else if errors.Is(e.Err, os.ErrPermission) {
		detail = msgRuntimeDirectoryPermission
	}
	return fmt.Sprintf(msgRuntimeDirectory, e.Variable, e.Path, detail)
}

// Unwrap preserves errors.Is/As for permission and missing-path errors.
func (e *RuntimeDirectoryError) Unwrap() error { return e.Err }

func (r *Runtime) checkTempDirectory() error {
	if r.Directory != "" {
		return nil
	}
	selected := selectedRuntimeTempDirectory()
	if selected.name == "" {
		return nil
	}
	info, err := os.Stat(selected.path)
	if err == nil && !info.IsDir() {
		err = errors.New(msgRuntimeDirectoryNotDirectory)
	}
	if err == nil && info.Mode().Perm()&0222 == 0 {
		err = os.ErrPermission
	}
	// The kernel check also covers ACLs and read-only mounts, without creating
	// probe files. Traversal is needed to reach the hashed runtime child.
	if err == nil {
		err = syscall.Access(selected.path, 2|1)
	}
	if err != nil {
		return &RuntimeDirectoryError{Variable: selected.name, Path: selected.path, Err: err}
	}
	return nil
}
func (r *Runtime) directoryFailure(err error) error {
	if err == nil || r.Directory != "" {
		return err
	}
	var owned *RuntimeDirectoryError
	if errors.As(err, &owned) {
		return err
	}
	selected := selectedRuntimeTempDirectory()
	var path *os.PathError
	if selected.name != "" && errors.As(err, &path) {
		return &RuntimeDirectoryError{Variable: selected.name, Path: selected.path, Err: err}
	}
	return err
}
func (r *Runtime) warnTempDirectory() {
	if err := r.checkTempDirectory(); err != nil {
		runtimeLog.Warning(err.Error())
	}
}
