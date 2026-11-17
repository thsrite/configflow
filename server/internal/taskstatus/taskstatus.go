// Package taskstatus is the port of backend/utils/task_status.py: the result
// of every background task, shown in 系统设置 → 定时任务.
//
// Runtime receipts live beside the configuration, not inside it: they are not
// part of revisions, exports or backups, and survive restarts so the page can
// still say why the last automatic run failed after a reload. The file is
// DATA_DIR/task-status.json, guarded by a process-wide leaf mutex. The server
// instance lock excludes another server; legacy lock files are left untouched.
package taskstatus

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"configflow/internal/ctxx"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

// Filename is the status file inside the data directory.
const Filename = "task-status.json"

// MaxError is the longest last_error kept (in characters).
const MaxError = 2000

// guard is the module-level threading.Lock of the Python module: one process
// holds one lock whatever the data directory.
var guard sync.Mutex

// Store reads and writes the status file of one data directory.
type Store struct {
	// DataDir is the repository data directory (get_repository().data_dir).
	DataDir string
	// Now is for tests; nil means time.Now.
	Now func() time.Time
	// OnRecord is called after Record wrote the file and released the lock
	// (the Python `safely(refresh_tasks, key)` of backend.notifications);
	// package #3 wires the notification refresh in here.
	OnRecord func(key string)
}

// New returns a store for dataDir.
func New(dataDir string) *Store { return &Store{DataDir: dataDir} }

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// NowISO is now_iso(): the UTC time at second resolution, "2026-11-14T12:00:00Z".
func NowISO() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

func (s *Store) nowISO() string { return s.now().UTC().Format("2006-01-02T15:04:05Z") }

// ParseTime is parse_time: the aware time for an ISO timestamp, or ok=false.
// Naive timestamps are taken as UTC. See FromISOFormat for the accepted grammar.
func ParseTime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || text == "" {
		return time.Time{}, false
	}
	return FromISOFormat(strings.ReplaceAll(text, "Z", "+00:00"))
}

func (s *Store) path() string { return filepath.Join(s.DataDir, Filename) }

// locked runs local file work under the process-wide leaf mutex.
func (s *Store) locked(fn func(path string) error) error {
	return s.lockedContext(context.Background(), fn)
}

func (s *Store) lockedContext(ctx context.Context, fn func(path string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	guard.Lock()
	defer guard.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	path := s.path()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	return fn(path)
}

// read is _read: the stored object, or an empty one when the file is missing,
// damaged or not an object.
func read(path string) *jsonx.Object {
	data, err := os.ReadFile(path)
	if err != nil {
		return jsonx.NewObject()
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return jsonx.NewObject()
	}
	if object, ok := value.(*jsonx.Object); ok {
		return object
	}
	return jsonx.NewObject()
}

// write is _write: temp file in the same directory (mode 0600 like
// tempfile.mkstemp), compact JSON, fsync, rename over the file.
func write(path string, value *jsonx.Object) error {
	return writeContext(context.Background(), path, value)
}

func writeContext(ctx context.Context, path string, value *jsonx.Object) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".task-status-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() {
		if _, statErr := os.Stat(name); statErr == nil {
			os.Remove(name)
		}
	}()
	if _, err := temp.Write(encoded); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Load is load(): the whole status object. The result is a private copy.
func (s *Store) Load() (*jsonx.Object, error) {
	var out *jsonx.Object
	err := s.locked(func(path string) error {
		out = read(path)
		return nil
	})
	return out, err
}

// Get is get(key): the entry of one task, an empty object when there is none.
func (s *Store) Get(key string) (*jsonx.Object, error) {
	data, err := s.Load()
	if err != nil {
		return nil, err
	}
	if entry, ok := data.Value(key).(*jsonx.Object); ok {
		return entry, nil
	}
	return jsonx.NewObject(), nil
}

// Result is the keyword arguments of record().
type Result struct {
	OK         bool
	StartedAt  string  // empty: now
	DurationMs float64 // int() of it, at least 0
	Error      string  // kept only for failures, cut to MaxError characters
	Trigger    string  // empty: "schedule"
	// CacheRetained is recorded for a failed (not skipped) run when non-nil.
	CacheRetained  *bool
	CacheUpdatedAt string
	// Skipped marks a run with nothing to do or a missing prerequisite: it
	// keeps the previous success and failure counts.
	Skipped bool
}

// countError is the Python exception record() raises on a counter it cannot
// convert (the file is left as it was).
type countError struct{ class, msg string }

func (e *countError) Error() string       { return e.msg }
func (e *countError) PythonClass() string { return e.class }

// pyInt is int(value or 0) for a counter read from the file: strings as
// int(str) (white space, Unicode digits, underscores), floats truncated,
// big integers kept; Python's TypeError/ValueError/OverflowError otherwise.
func pyInt(v any) (*big.Int, error) {
	switch x := v.(type) {
	case nil:
		return new(big.Int), nil
	case bool:
		if x {
			return big.NewInt(1), nil
		}
		return new(big.Int), nil
	case string:
		if x == "" {
			return new(big.Int), nil
		}
		n, err := pystr.Int(x, 10)
		if err != nil {
			return nil, &countError{"ValueError", err.Error()}
		}
		return n, nil
	case jsonx.Number:
		if x.IsInt() {
			n, _ := new(big.Int).SetString(string(x), 10)
			return n, nil
		}
		f := x.Float64()
		switch {
		case math.IsInf(f, 0):
			return nil, &countError{"OverflowError", "cannot convert float infinity to integer"}
		case math.IsNaN(f):
			return nil, &countError{"ValueError", "cannot convert float NaN to integer"}
		}
		n, _ := big.NewFloat(math.Trunc(f)).Int(nil)
		return n, nil
	case []any:
		if len(x) == 0 {
			return new(big.Int), nil
		}
		return nil, &countError{"TypeError", "int() argument must be a string, a bytes-like object or a real number, not 'list'"}
	case *jsonx.Object:
		if x.Len() == 0 {
			return new(big.Int), nil
		}
		return nil, &countError{"TypeError", "int() argument must be a string, a bytes-like object or a real number, not 'dict'"}
	}
	return nil, &countError{"TypeError", fmt.Sprintf("int() argument must be a string, a bytes-like object or a real number, not '%T'", v)}
}

// increment is int(entry.get(key) or 0) + 1 stored back under key.
func increment(entry *jsonx.Object, key string) error {
	n, err := pyInt(entry.Value(key))
	if err != nil {
		return err
	}
	entry.Set(key, jsonx.BigInt(n.Add(n, big.NewInt(1))))
	return nil
}

// Record is record(key, ...): append one finished run and return the updated
// entry (a copy).
func (s *Store) Record(key string, r Result) (*jsonx.Object, error) {
	return s.RecordContext(context.Background(), key, r)
}

// RecordContext records a result independently of client disconnects, while
// respecting its work lifetime under the leaf lock and before replacement.
func (s *Store) RecordContext(ctx context.Context, key string, r Result) (*jsonx.Object, error) {
	ctx = ctxx.Detach(ctx)
	var result *jsonx.Object
	err := s.lockedContext(ctx, func(path string) error {
		data := read(path)
		entry, ok := data.Value(key).(*jsonx.Object)
		if !ok {
			entry = jsonx.NewObject()
		}
		finished := s.nowISO()
		status := "failed"
		switch {
		case r.Skipped:
			status = "skipped"
		case r.OK:
			status = "success"
		}
		started := r.StartedAt
		if started == "" {
			started = finished
		}
		duration := int64(r.DurationMs)
		if duration < 0 {
			duration = 0
		}
		trigger := r.Trigger
		if trigger == "" {
			trigger = "schedule"
		}
		message := ""
		if !r.OK {
			message = r.Error
			if utf8.RuneCountInString(message) > MaxError {
				message = string([]rune(message)[:MaxError])
			}
		}
		entry.Set("last_run_at", started)
		entry.Set("last_finished_at", finished)
		entry.Set("last_status", status)
		entry.Set("last_duration_ms", duration)
		entry.Set("last_trigger", trigger)
		entry.Set("last_error", message)
		if !r.Skipped {
			if err := increment(entry, "run_count"); err != nil {
				return err
			}
			if r.OK {
				entry.Set("last_success_at", finished)
				entry.Set("consecutive_failures", 0)
			} else {
				if err := increment(entry, "failure_count"); err != nil {
					return err
				}
				if err := increment(entry, "consecutive_failures"); err != nil {
					return err
				}
			}
		}
		if r.OK || r.Skipped {
			entry.Delete("cache_retained")
		} else if r.CacheRetained != nil {
			entry.Set("cache_retained", *r.CacheRetained)
		}
		if r.CacheUpdatedAt != "" {
			entry.Set("cache_updated_at", r.CacheUpdatedAt)
		}
		data.Set(key, entry)
		if err := writeContext(ctx, path, data); err != nil {
			return err
		}
		result = entry.Clone()
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.OnRecord != nil {
		s.OnRecord(key)
	}
	return result, nil
}

// Forget is forget(*keys): drop the entries of the given tasks.
func (s *Store) Forget(keys ...string) error {
	return s.ForgetContext(context.Background(), keys...)
}

// ForgetContext removes results independently of client disconnects, while
// respecting its work lifetime under the leaf lock and before replacement.
func (s *Store) ForgetContext(ctx context.Context, keys ...string) error {
	ctx = ctxx.Detach(ctx)
	return s.lockedContext(ctx, func(path string) error {
		data := read(path)
		removed := false
		for _, key := range keys {
			if value, ok := data.Pop(key); ok && value != nil {
				removed = true
			}
		}
		if removed {
			return writeContext(ctx, path, data)
		}
		return nil
	})
}
