package repo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"configflow/internal/lockx"
)

// Lock settings (ProfileRepository class attributes).
const (
	LockTimeoutEnv            = "CONFIGFLOW_LOCK_TIMEOUT_SECONDS"
	DefaultLockTimeoutSeconds = 300.0
	MinLockTimeoutSeconds     = 0.1
	MaxLockTimeoutSeconds     = 3600.0
	LockPollInterval          = 50 * time.Millisecond
)

// LockTimeout is the value of CONFIGFLOW_LOCK_TIMEOUT_SECONDS (a decimal
// number, surrounding white space allowed) clamped to 0.1–3600 s, 300 s when
// it is unset, not a number or not finite. It is read on every use.
func LockTimeout() time.Duration {
	raw, ok := os.LookupEnv(LockTimeoutEnv)
	if !ok {
		return secondsDuration(DefaultLockTimeoutSeconds)
	}
	text := strings.TrimSpace(raw)
	timeout, err := strconv.ParseFloat(text, 64)
	// Decimal only: ParseFloat also reads hexadecimal floats.
	if err != nil || strings.ContainsAny(text, "xXpP") || math.IsNaN(timeout) || math.IsInf(timeout, 0) {
		return secondsDuration(DefaultLockTimeoutSeconds)
	}
	return secondsDuration(math.Min(MaxLockTimeoutSeconds, math.Max(MinLockTimeoutSeconds, timeout)))
}

func secondsDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

// configLocks serializes one physical configuration lock within this process.
// The file flock remains necessary for external tools and instance startup.
var configLocks = lockx.NewKeyed[string](lockx.LevelRepository, "repository")
var profileOperations = lockx.NewKeyed[[2]string](lockx.LevelDevice, "profile-operation")
var fileLocks = lockx.NewLeafKeyed[string]("repository-file")

// heldLocks is the chain of lock files held by the call chain a context
// belongs to (Python: the thread-local _HELD_LOCKS).
type heldLocks struct {
	path   string
	active atomic.Bool
	parent *heldLocks
}

type heldLocksKey struct{}

func holdsLock(ctx context.Context, path string) bool {
	key := resolvePath(path)
	for held, _ := ctx.Value(heldLocksKey{}).(*heldLocks); held != nil; held = held.parent {
		if held.path == key && held.active.Load() {
			return true
		}
	}
	return false
}

const lockTimeoutMessage = "Timed out waiting for profile repository lock"

func lockIsContended(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EDEADLK)
}

// withLock is ProfileRepository._lock(path): it runs fn while holding the
// in-process lock of path and an exclusive flock on the file, polled every
// 50 ms until LockTimeout. fn receives a context that records the lock; pass
// it on to everything called inside, so nested reads know .config.lock is
// held. Waiting stops early when ctx is cancelled.
func withLock(ctx context.Context, path string, fn func(ctx context.Context) error) error {
	locked, release, err := acquireLock(ctx, path)
	if err != nil {
		return err
	}
	defer release()
	return fn(locked)
}

// acquireLock takes the lock of withLock and returns the context that
// records it and the function that releases it (call it exactly once). It
// is for locks held beyond one call, like Python's ExitStack entries.
func acquireLock(ctx context.Context, path string) (locked context.Context, release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if readSnapshotActive(ctx) {
		return nil, nil, ErrReadOnlySnapshot
	}
	key, err := resolve(path)
	if err != nil {
		return nil, nil, err
	}
	deadline := time.Now().Add(LockTimeout())
	held, releaseLocal, err := configLocks.Lock(ctx, key, time.Until(deadline))
	if err != nil {
		if errors.Is(err, lockx.ErrTimeout) {
			return nil, nil, &Error{Kind: ErrRepository, Message: lockTimeoutMessage, Cause: err}
		}
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
		releaseLocal()
		return nil, nil, err
	}
	handle, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		releaseLocal()
		return nil, nil, err
	}
	fail := func(err error) (context.Context, func(), error) { handle.Close(); releaseLocal(); return nil, nil, err }
	if info, err := handle.Stat(); err != nil {
		return fail(err)
	} else if info.Size() == 0 {
		if _, err := handle.Write([]byte("0")); err != nil {
			return fail(err)
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !lockIsContended(err) {
			return fail(&os.PathError{Op: "flock", Path: path, Err: err})
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fail(repositoryError(lockTimeoutMessage))
		}
		timer := time.NewTimer(min(LockPollInterval, remaining))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return fail(ctx.Err())
		}
		timer.Stop()
	}
	parent, _ := ctx.Value(heldLocksKey{}).(*heldLocks)
	record := &heldLocks{path: key, parent: parent}
	record.active.Store(true)
	var once sync.Once
	release = func() {
		once.Do(func() {
			record.active.Store(false)
			syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
			handle.Close()
			releaseLocal()
		})
	}
	return context.WithValue(held, heldLocksKey{}, record), release, nil
}

func acquireFile(ctx context.Context, path string) (context.Context, func(), error) {
	if readSnapshotActive(ctx) {
		return nil, nil, ErrReadOnlySnapshot
	}
	key, err := resolve(path)
	if err != nil {
		return nil, nil, err
	}
	held, release, err := fileLocks.Lock(ctx, key, LockTimeout())
	if errors.Is(err, lockx.ErrTimeout) {
		return nil, nil, &Error{Kind: ErrRepository, Message: lockTimeoutMessage, Cause: err}
	}
	return held, release, err
}

func withFile(ctx context.Context, path string, fn func() error) error {
	_, release, err := acquireFile(ctx, path)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

func (r *Repository) withProfileOperation(ctx context.Context, id string, fn func(context.Context) error) error {
	if err := ValidateProfileID(id); err != nil {
		return err
	}
	if readSnapshotActive(ctx) {
		return ErrReadOnlySnapshot
	}
	held, release, err := profileOperations.Lock(ctx, [2]string{r.dataDir, id}, LockTimeout())
	if err != nil {
		if errors.Is(err, lockx.ErrTimeout) {
			return &Error{Kind: ErrRepository, Message: lockTimeoutMessage, Cause: err}
		}
		return err
	}
	defer release()
	return fn(held)
}

// resolvePath is resolve for callers that only need a lock or map key: on
// an error (a symlink loop) it falls back to the cleaned absolute path.
func resolvePath(path string) string {
	resolved, err := resolve(path)
	if err != nil {
		absolute, _ := filepath.Abs(path)
		return filepath.Clean(absolute)
	}
	return resolved
}

// resolve returns path absolute with its symbolic links followed
// (filepath.EvalSymlinks). A path that does not exist yet resolves its
// longest existing prefix and keeps the rest as given (cleaned), so a file
// about to be created resolves like its directory; a dangling link resolves
// to its target. A symlink loop or another read error is returned.
func resolve(path string) (string, error) {
	return resolveLinks(path, path, 0)
}

// maxDanglingLinks bounds dangling links followed by resolve (a loop of
// dangling links never ends otherwise).
const maxDanglingLinks = 40

func resolveLinks(original, path string, depth int) (string, error) {
	if depth > maxDanglingLinks {
		return "", fmt.Errorf("resolve %s: too many links", original)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", original, err)
	}
	existing, rest := absolute, ""
	for {
		info, err := os.Lstat(existing)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(existing)
			if err == nil {
				return filepath.Join(resolved, rest), nil
			}
			if info.Mode()&os.ModeSymlink != 0 && errors.Is(err, fs.ErrNotExist) {
				target, err := os.Readlink(existing)
				if err != nil {
					return "", fmt.Errorf("resolve %s: %w", original, err)
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(existing), target)
				}
				return resolveLinks(original, filepath.Join(target, rest), depth+1)
			}
			return "", fmt.Errorf("resolve %s: %w", original, err)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve %s: %w", original, err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return absolute, nil
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
}

// isRelativeTo is Path.relative_to() succeeding: child is root or below it.
func isRelativeTo(child, root string) bool {
	if child == root {
		return true
	}
	if root == "/" {
		return strings.HasPrefix(child, "/")
	}
	return strings.HasPrefix(child, root+"/")
}
