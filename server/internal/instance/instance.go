// Package instance gives one ConfigFlow server exclusive ownership of a data
// directory. It also holds Python's scheduler lifetime locks during the native
// transition; repository transactions remain the repository package's concern.
package instance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"configflow/internal/version"
)

// ErrInUse identifies a data directory held by another Go or Python server.
var ErrInUse = errors.New("data directory in use")

var lockNames = [...]string{
	".configflow-server.lock",
	".task-scheduler.lock",
	"router-rule-sync/.scheduler.lock",
	".oxidns-rankings.lock",
	"probes/.auto.lock",
}

type inUseError struct {
	directory string
	cause     error
}

func (e *inUseError) Error() string   { return fmt.Sprintf(msgDirectoryInUse, e.directory) }
func (e *inUseError) Unwrap() []error { return []error{ErrInUse, e.cause} }

// Instance owns five lifetime locks. Release is safe to call repeatedly or
// concurrently. The zero value is harmless but does not own a directory.
type Instance struct {
	once  sync.Once
	files []*os.File
}

// Acquire locks the data directory until Release. Busy locks are retried every
// 500ms until ctx ends (the server supplies a 30s deadline). Partial acquisition
// is rolled back on every error. Lock files persist; Python lock contents are
// untouched and the server lock's diagnostic text never controls ownership.
func Acquire(ctx context.Context, dataDir string) (_ *Instance, err error) {
	i := &Instance{}
	defer func() {
		if err != nil {
			i.Release()
		}
	}()
	for _, name := range lockNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(dataDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create instance lock directory: %w", err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open instance lock: %w", err)
		}
		i.files = append(i.files, file)
		if err := acquireFile(ctx, file); err != nil {
			if errors.Is(err, ErrInUse) {
				return nil, &inUseError{directory: dataDir, cause: err}
			}
			return nil, fmt.Errorf("acquire instance lock: %w", err)
		}
	}
	file := i.files[0]
	if err := file.Truncate(0); err != nil {
		return nil, fmt.Errorf("truncate instance diagnostics: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d %s %s\n", os.Getpid(), version.Version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, fmt.Errorf("write instance diagnostics: %w", err)
	}
	return i, nil
}

func acquireFile(ctx context.Context, file *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrInUse, err)
		}
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(ErrInUse, ctx.Err())
		case <-timer.C:
		}
	}
}

// Release closes all held locks without deleting or replacing their files.
func (i *Instance) Release() {
	if i == nil {
		return
	}
	i.once.Do(func() {
		for n := len(i.files) - 1; n >= 0; n-- {
			_ = i.files[n].Close()
		}
	})
}
