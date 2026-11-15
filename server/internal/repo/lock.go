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
	"syscall"
	"time"
	"unicode"
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

// One in-process lock per resolved path (Python: _THREAD_LOCKS). Python's is
// an RLock, but re-entering the same path in one thread then waits on its own
// flock until the timeout; heldLocks reproduces that without a second
// descriptor.
var (
	processLocks      = map[string]chan struct{}{}
	processLocksGuard sync.Mutex
)

func processLock(key string) chan struct{} {
	processLocksGuard.Lock()
	defer processLocksGuard.Unlock()
	lock, ok := processLocks[key]
	if !ok {
		lock = make(chan struct{}, 1)
		processLocks[key] = lock
	}
	return lock
}

// heldLocks is the chain of lock files held by the call chain a context
// belongs to (Python: the thread-local _HELD_LOCKS).
type heldLocks struct {
	path   string
	parent *heldLocks
}

type heldLocksKey struct{}

func holdsLock(ctx context.Context, path string) bool {
	key := resolvePath(path)
	for held, _ := ctx.Value(heldLocksKey{}).(*heldLocks); held != nil; held = held.parent {
		if held.path == key {
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
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	key := resolvePath(path)
	if holdsLock(ctx, key) {
		// Python: the RLock is re-entered, then flock on a second descriptor
		// of the same file waits for this thread itself until the timeout.
		select {
		case <-time.After(LockTimeout()):
			return repositoryError(lockTimeoutMessage)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	lock := processLock(key)
	select {
	case lock <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-lock }()

	handle, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	defer handle.Close()
	if info, err := handle.Stat(); err != nil {
		return err
	} else if info.Size() == 0 {
		if _, err := handle.Write([]byte("0")); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(LockTimeout())
	for {
		err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !lockIsContended(err) {
			return &os.PathError{Op: "flock", Path: path, Err: err}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return repositoryError(lockTimeoutMessage)
		}
		select {
		case <-time.After(min(LockPollInterval, remaining)):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
	held, _ := ctx.Value(heldLocksKey{}).(*heldLocks)
	return fn(context.WithValue(ctx, heldLocksKey{}, &heldLocks{path: key, parent: held}))
}

// resolvePath is pathlib.Path.resolve() (non-strict): absolute, symbolic
// links followed for as long as the path exists, "." and ".." removed after
// the links before them were followed.
func resolvePath(path string) string {
	if !filepath.IsAbs(path) {
		if absolute, err := filepath.Abs(path); err == nil {
			path = absolute
		}
	}
	return realpath("/", strings.Split(path, "/"), 0)
}

func realpath(current string, parts []string, depth int) string {
	for index, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			current = filepath.Dir(current)
			continue
		}
		next := filepath.Join(current, part)
		info, err := os.Lstat(next)
		if err != nil || info.Mode()&os.ModeSymlink == 0 || depth > 40 {
			current = next
			continue
		}
		target, err := os.Readlink(next)
		if err != nil {
			current = next
			continue
		}
		rest := append(strings.Split(target, "/"), parts[index+1:]...)
		if filepath.IsAbs(target) {
			return realpath("/", rest, depth+1)
		}
		return realpath(current, rest, depth+1)
	}
	return current
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
