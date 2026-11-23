// Package tasks is the logic of backend/routes/tasks.py (系统设置 → 定时任务):
// one row per background task with its last result (internal/taskstatus)
// and next run, "run now", and interval edits.
//
// The rows come from the packages that own the tasks, in Python's order:
//
//	SlotSubscriptions  (C15 internal/refresh)    SlotRuleSources (C15)
//	SlotRouters        (#37 router rule sync)    SlotAgents      (#20/#27/#28 Agent rule sync, DNS follow)
//	SlotDomains        (#19 domain discovery)    SlotBackup      (this package, WebDAV)
//	SlotMetrics        (#21 metrics flush)
//
// Each owner calls SetRows for its slot and SetRunner for the categories it
// can run. ListTasks refuses to answer (ErrIncomplete) while a slot is
// missing, so the page never shows a partial list as if it were complete.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/taskstatus"
)

// Slots in list order.
const (
	SlotSubscriptions = "subscription"
	SlotRuleSources   = "rule_source"
	SlotRouters       = "router_rule_sync"
	SlotAgents        = "agent"
	SlotDomains       = "domain_discovery"
	SlotBackup        = "webdav_backup"
	SlotMetrics       = "metrics_history"
)

var slotOrder = []string{SlotSubscriptions, SlotRuleSources, SlotRouters, SlotAgents, SlotDomains, SlotBackup, SlotMetrics}

// DomainDiscoverySeconds is DOMAIN_DISCOVERY_SECONDS.
const DomainDiscoverySeconds = 600

// BackupTaskKey is webdav_backup.BACKUP_TASK_KEY.
const BackupTaskKey = "webdav_backup"

// MetricsTaskKey is metrics_flush.TASK_KEY.
const MetricsTaskKey = "metrics_history_flush"

// BuildRows projects a slot from native saved receipts.
type BuildRows func(context.Context, taskstatus.Snapshot) ([]*Task, error)

// IntervalEditor edits a native integer interval; nil means invalid input.
type IntervalEditor func(context.Context, *int64) error

// RunFunc starts a task now. Return a *Busy, *NotFound, *Invalid or
// *Conflict error for the user-facing failures.
type RunFunc func(ctx context.Context, key, resource string) error

// Error kinds of run_task / set_interval.
type (
	// Busy is scheduled_refresh.TaskBusy (409).
	Busy struct{ Message string }
	// NotFound is a LookupError (404); Message is already stripped of quotes.
	NotFound struct{ Message string }
	// Invalid is a ValueError (409 for run, 400 for intervals).
	Invalid struct{ Message string }
	// Conflict is a RouterError of start_job (409).
	Conflict struct{ Message string }
)

func (e *Busy) Error() string     { return e.Message }
func (e *NotFound) Error() string { return e.Message }
func (e *Invalid) Error() string  { return e.Message }
func (e *Conflict) Error() string { return e.Message }

// SetRowBuilder installs the row builder of a slot on the default Registry.
func SetRowBuilder(slot string, f BuildRows) func() { return Default().SetRowBuilder(slot, f) }

// SetRunner installs the "run now" of a category on the default Registry.
func SetRunner(category string, f RunFunc) func() { return Default().SetRunner(category, f) }

// SetIntervalEditor installs an exact-key editor on the default Registry.
func SetIntervalEditor(key string, f IntervalEditor) func() {
	return Default().SetIntervalEditor(key, f)
}

// Missing lists the slots nobody provides yet on the default Registry.
func Missing() []string { return Default().Missing() }

// Complete reports whether every slot has rows on the default Registry.
func Complete() bool { return Default().Complete() }

// ErrIncomplete is returned by ListTasks while slots are missing.
var ErrIncomplete = errors.New("task rows are not wired: " + "missing slots")

// Timestamp formats seconds precision with Z for UTC.
func Timestamp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	text := t.Format("2006-01-02T15:04:05-07:00")
	return new(strings.Replace(text, "+00:00", "Z", 1))
}

// ErrInvalidHistory identifies malformed saved counters, distinct from I/O or
// readiness. Notification list reads translate this data failure to HTTP 503.
var ErrInvalidHistory = errors.New("invalid task history")

var background = lockx.NewKeyed[string](lockx.LevelJob, "tasks")

// IsBackground reports whether this process holds the task's busy key.
func IsBackground(key string) bool { return background.Held(key) }

// StartBackground is _start_background(key, work): run work once in the
// background; a second start while it runs is Busy. Failures are recorded
// by the work itself.
func StartBackground(ctx context.Context, key string, work func(ctx context.Context) error) error {
	return Default().StartBackground(ctx, key, work)
}

func startBackground(ctx context.Context, key string, work func(context.Context) error, spawner lockx.Spawner) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	held, release, ok := background.TryLock(ctx, key)
	if !ok {
		return &Busy{Message: msgBusy}
	}
	if err := spawner.Spawn(held, "task-"+key, func(worker context.Context) {
		defer release()
		// The work owns recording and logging ordinary errors. Runtime recovers
		// panics after this defer releases the busy key.
		_ = work(worker)
	}); err != nil {
		release()
		return fmt.Errorf("start background task: %w", err)
	}
	return nil
}

// List is list_tasks() on the default Registry.
func List(ctx context.Context) ([]*Task, error) { return Default().List(ctx) }

// Available projects receipts only for registered task owners on the default Registry.
func Available(ctx context.Context) ([]*Task, error) { return Default().Available(ctx) }

// ForSlot returns only the selected provided slot on the default Registry.
func ForSlot(ctx context.Context, slot string) ([]*Task, error) { return Default().ForSlot(ctx, slot) }

// Runner returns the run function of a category on the default Registry.
func Runner(category string) RunFunc { return Default().Runner(category) }

// IntervalEditorFor returns the interval edit of a key on the default Registry.
func IntervalEditorFor(key string) IntervalEditor { return Default().IntervalEditorFor(key) }
