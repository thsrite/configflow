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
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"configflow/internal/ctxx"
	"configflow/internal/jsonresp"
	"configflow/internal/logx"
	"configflow/internal/model"
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

// ParseTimestamp reads an ISO timestamp, treating naive timestamps as UTC.
func ParseTimestamp(text string) (time.Time, bool) {
	t, _, ok := ParseISO(text)
	return t, ok
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

// Snapshot preserves unowned receipts and unknown JSON members without converting numbers.
type Snapshot map[string]json.RawMessage

// Receipt is one task's result (task_status.record). Every member is
// optional: record() writes the last-run members, the counters once a run
// counted, and the cache members only for failures. A stored member of the
// wrong type reads as absent and is reported; the counters stay opaque JSON
// (Count refuses a malformed one, so the receipt is not rewritten).
type Receipt struct {
	LastRunAt           *string         `json:"last_run_at,omitzero" text:"strict"`
	LastFinishedAt      *string         `json:"last_finished_at,omitzero"`
	LastStatus          *string         `json:"last_status,omitzero"`
	LastDurationMs      *model.Int      `json:"last_duration_ms,omitzero"`
	LastTrigger         *string         `json:"last_trigger,omitzero"`
	LastError           *string         `json:"last_error,omitzero"`
	RunCount            json.RawMessage `json:"run_count,omitzero"`
	FailureCount        json.RawMessage `json:"failure_count,omitzero"`
	ConsecutiveFailures json.RawMessage `json:"consecutive_failures,omitzero"`
	LastSuccessAt       *string         `json:"last_success_at,omitzero"`
	CacheRetained       *bool           `json:"cache_retained,omitzero"`
	CacheUpdatedAt      *string         `json:"cache_updated_at,omitzero"`
}

// UnmarshalJSON decodes a stored receipt leniently. Receipts are read on
// every task list refresh, so issues are not logged here.
func (r *Receipt) UnmarshalJSON(data []byte) error {
	type wire Receipt
	var decoded wire
	if _, err := model.DecodeStored(data, &decoded, ""); err != nil {
		return err
	}
	*r = Receipt(decoded)
	return nil
}

// Entry returns a private typed view of a receipt, or an empty receipt for non-objects.
func (s Snapshot) Entry(key string) *Receipt {
	var result Receipt
	if json.Unmarshal(s[key], &result) != nil {
		return &Receipt{}
	}
	return &result
}

func readSnapshot(path string) Snapshot {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			category := "io"
			if errors.Is(err, os.ErrPermission) {
				category = "permission"
			}
			logx.Get("backend.utils.task_status").Warningf("%s: %s", msgReadFailed, category)
		}
		return Snapshot{}
	}
	var result Snapshot
	if jsonresp.DecodeStored(data, &result) != nil || result == nil {
		return Snapshot{}
	}
	return result
}

func writeSnapshot(ctx context.Context, path string, value Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
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

// Read returns a private snapshot under the leaf lock.
func (s *Store) Read() (Snapshot, error) {
	var out Snapshot
	err := s.locked(func(path string) error { out = readSnapshot(path); return nil })
	return out, err
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

func increment(raw *json.RawMessage) error {
	count, err := Count(*raw)
	if err != nil {
		return err
	}
	*raw = json.RawMessage(count.Add(count, big.NewInt(1)).String())
	return nil
}

// SaveResult records a result independently of client disconnects, while
// respecting its work lifetime under the leaf lock and before replacement.
func (s *Store) SaveResult(ctx context.Context, key string, r Result) (*Receipt, error) {
	ctx = ctxx.Detach(ctx)
	var result *Receipt
	err := s.lockedContext(ctx, func(path string) error {
		data := readSnapshot(path)
		entry := data.Entry(key)
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
		entry.LastRunAt = new(started)
		entry.LastFinishedAt = new(finished)
		entry.LastStatus = new(status)
		entry.LastDurationMs = new(model.Int(duration))
		entry.LastTrigger = new(trigger)
		entry.LastError = new(message)
		if !r.Skipped {
			if err := increment(&entry.RunCount); err != nil {
				return err
			}
			if r.OK {
				entry.LastSuccessAt = new(finished)
				entry.ConsecutiveFailures = json.RawMessage("0")
			} else {
				if err := increment(&entry.FailureCount); err != nil {
					return err
				}
				if err := increment(&entry.ConsecutiveFailures); err != nil {
					return err
				}
			}
		}
		if r.OK || r.Skipped {
			entry.CacheRetained = nil
		} else if r.CacheRetained != nil {
			entry.CacheRetained = r.CacheRetained
		}
		if r.CacheUpdatedAt != "" {
			entry.CacheUpdatedAt = new(r.CacheUpdatedAt)
		}
		encoded, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		data[key] = encoded
		if err := writeSnapshot(ctx, path, data); err != nil {
			return err
		}
		result = entry
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
		data := readSnapshot(path)
		removed := false
		for _, key := range keys {
			if value, ok := data[key]; ok && string(value) != "null" {
				delete(data, key)
				removed = true
			}
		}
		if removed {
			return writeSnapshot(ctx, path, data)
		}
		return nil
	})
}
