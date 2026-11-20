package repo

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"

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

// LockTimeout is ProfileRepository._lock_timeout_seconds(): the value of
// CONFIGFLOW_LOCK_TIMEOUT_SECONDS clamped to 0.1–3600 s, 300 s when it is
// unset, not a number or not finite. It is read on every use.
func LockTimeout() time.Duration {
	raw, ok := os.LookupEnv(LockTimeoutEnv)
	if !ok {
		return secondsDuration(DefaultLockTimeoutSeconds)
	}
	timeout, ok := pythonFloat(raw)
	if !ok || math.IsNaN(timeout) || math.IsInf(timeout, 0) {
		return secondsDuration(DefaultLockTimeoutSeconds)
	}
	return secondsDuration(math.Min(MaxLockTimeoutSeconds, math.Max(MinLockTimeoutSeconds, timeout)))
}

// pythonFloatSyntax is the grammar of CPython's float(str) after the
// whitespace strip: decimal digits with single underscores between them, an
// optional fraction and exponent, or inf/infinity/nan (no hexadecimal).
var pythonFloatSyntax = regexp.MustCompile(`^[+-]?(?:(?:[0-9](?:_?[0-9])*(?:\.(?:[0-9](?:_?[0-9])*)?)?|\.[0-9](?:_?[0-9])*)(?:[eE][+-]?[0-9](?:_?[0-9])*)?|(?i:inf|infinity|nan))$`)

// pythonFloat is float(text) for a str: like CPython's PyFloat_FromString,
// Unicode white space becomes a space and Unicode decimal digits (full-width,
// Arabic-Indic, ...) become ASCII digits before ASCII white space is
// stripped and the number is parsed. ok is false where Python raises
// ValueError.
func pythonFloat(text string) (float64, bool) {
	var ascii strings.Builder
	for _, r := range text {
		switch {
		case r < 127:
			ascii.WriteRune(r)
		case pythonUnicodeSpace(r):
			ascii.WriteByte(' ')
		default:
			digit, ok := unicodeDecimal(r)
			if !ok {
				return 0, false
			}
			ascii.WriteByte(byte('0' + digit))
		}
	}
	number := strings.Trim(ascii.String(), " \t\n\r\v\f")
	if !pythonFloatSyntax.MatchString(number) {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(number, "_", ""), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return value, true
}

// pythonUnicodeSpace is Py_UNICODE_ISSPACE for characters from U+007F up.
func pythonUnicodeSpace(r rune) bool {
	switch r {
	case 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// unicodeDecimal is Py_UNICODE_TODECIMAL: the value of a character of
// category Nd. Every Nd run in Unicode is made of whole 0–9 sequences.
func unicodeDecimal(r rune) (int, bool) {
	if !unicode.Is(unicode.Nd, r) {
		return 0, false
	}
	start := r
	for unicode.Is(unicode.Nd, start-1) {
		start--
	}
	return int(r-start) % 10, true
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

// resolvePath is pathlib.Path.resolve() (non-strict) without its symlink
// loop check; see resolve.
func resolvePath(path string) string {
	resolved, _ := realpath(path)
	return resolved
}

// resolve is pathlib.Path.resolve() of CPython 3.11 (non-strict): absolute,
// symbolic links followed for as long as the path exists, "." and ".."
// removed after the links before them were followed. A symlink loop is
// Python's RuntimeError("Symlink loop from '<path>'").
func resolve(path string) (string, error) {
	resolved, _ := realpath(path)
	if _, err := os.Stat(resolved); errors.Is(err, syscall.ELOOP) {
		return "", errors.New("Symlink loop from " + pyQuote(resolved))
	}
	return resolved, nil
}

// realpath is posixpath.realpath(path, strict=False) of CPython 3.11 for an
// absolute path (a relative one is made absolute first): a link seen again
// while it is being resolved ends the walk at that link, with the rest of
// the path appended unresolved (ok is then false).
func realpath(path string) (resolved string, ok bool) {
	if !filepath.IsAbs(path) {
		if absolute, err := filepath.Abs(path); err == nil {
			path = absolute
		}
	}
	resolved, ok = joinRealpath("/", path, map[string]*string{})
	return filepath.Clean(resolved), ok
}

func joinRealpath(path, rest string, seen map[string]*string) (string, bool) {
	if strings.HasPrefix(rest, "/") {
		path, rest = "/", rest[1:]
	}
	for rest != "" {
		var name string
		name, rest, _ = strings.Cut(rest, "/")
		switch name {
		case "", ".":
			continue
		case "..":
			path = filepath.Dir(path)
			continue
		}
		newpath := filepath.Join(path, name)
		info, err := os.Lstat(newpath)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			path = newpath
			continue
		}
		if cached, found := seen[newpath]; found {
			if cached != nil {
				path = *cached
				continue
			}
			// The symlink is not resolved, so this is a symlink loop.
			return joinRest(newpath, rest), false
		}
		target, err := os.Readlink(newpath)
		if err != nil {
			path = newpath
			continue
		}
		seen[newpath] = nil
		resolved, ok := joinRealpath(path, target, seen)
		if !ok {
			return joinRest(resolved, rest), false
		}
		path = resolved
		seen[newpath] = &resolved
	}
	return path, true
}

func joinRest(path, rest string) string {
	if rest == "" {
		return path
	}
	return path + "/" + rest
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
