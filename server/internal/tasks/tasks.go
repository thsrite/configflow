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
	"sync"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/repo"
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

var (
	mu        sync.Mutex
	rows      = map[string]BuildRows{SlotBackup: backupRows}
	runners   = map[string]RunFunc{}
	intervals = map[string]IntervalEditor{}
)

// SetRowBuilder installs the row builder of a slot and returns its restoration.
func SetRowBuilder(slot string, f BuildRows) func() {
	mu.Lock()
	previous := rows[slot]
	rows[slot] = f
	mu.Unlock()
	return func() { mu.Lock(); rows[slot] = previous; mu.Unlock() }
}

// SetRunner installs the "run now" of a category (or of the exact key
// "metrics_history_flush") and returns its restoration.
func SetRunner(category string, f RunFunc) func() {
	mu.Lock()
	previous := runners[category]
	runners[category] = f
	mu.Unlock()
	return func() { mu.Lock(); runners[category] = previous; mu.Unlock() }
}

// SetIntervalEditor installs an exact-key editor and returns its restoration.
func SetIntervalEditor(key string, f IntervalEditor) func() {
	mu.Lock()
	previous := intervals[key]
	intervals[key] = f
	mu.Unlock()
	return func() { mu.Lock(); intervals[key] = previous; mu.Unlock() }
}

// Missing lists the slots nobody provides yet.
func Missing() []string {
	mu.Lock()
	defer mu.Unlock()
	var missing []string
	for _, slot := range slotOrder {
		if rows[slot] == nil {
			missing = append(missing, slot)
		}
	}
	return missing
}

// Complete reports whether every slot has rows.
func Complete() bool { return len(Missing()) == 0 }

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
	return startBackground(ctx, key, work, lifecycle.DefaultSpawner{})
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

func backupRows(ctx context.Context, statuses taskstatus.Snapshot) ([]*Task, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	system, err := repository.System(ctx)
	if err != nil {
		return nil, err
	}
	backup := system.Backup
	configured := backup.WebDAVURL != "" && backup.WebDAVUsername != "" && backup.WebDAVPassword != ""
	if _, present := statuses[BackupTaskKey]; !configured && !present {
		return nil, nil
	}
	history, err := ProjectHistory(statuses.Entry(BackupTaskKey))
	if err != nil {
		return nil, err
	}
	automatic := backup.AutoBackup != nil && *backup.AutoBackup
	hint := ""
	if !configured {
		hint = msgBackupConfigure
	}
	manual := history.LastTrigger != nil && *history.LastTrigger == "manual"
	return []*Task{{Key: BackupTaskKey, Category: SlotBackup, Name: msgBackupName, Enabled: automatic,
		NotificationsEnabled: new(automatic || (configured && manual)), ScheduleText: msgBackupSchedule,
		CanRun: configured, RunHint: hint, Running: IsBackground(BackupTaskKey), Link: new("/system-settings"), HistoryData: history}}, nil
}

// List is list_tasks().
func List(ctx context.Context) ([]*Task, error) {
	if !Complete() {
		return nil, ErrIncomplete
	}
	return Available(ctx)
}

// Available projects receipts only for registered task owners. Callers
// requiring a complete observation must check readiness or use ListTasks.
func Available(ctx context.Context) ([]*Task, error) {
	return availableRows(ctx, "")
}

// ForSlot returns only the selected provided slot, never a fake empty provider.
func ForSlot(ctx context.Context, slot string) ([]*Task, error) {
	if !HasRows(slot) {
		return nil, &Unavailable{Slots: []string{slot}}
	}
	return availableRows(ctx, slot)
}

func availableRows(ctx context.Context, selected string) ([]*Task, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	statuses, err := taskstatus.New(repository.DataDir()).Read()
	if err != nil {
		return nil, err
	}
	mu.Lock()
	builders := make([]BuildRows, 0, len(slotOrder))
	for _, slot := range slotOrder {
		if selected != "" && slot != selected {
			continue
		}
		if build := rows[slot]; build != nil {
			builders = append(builders, build)
		}
	}
	mu.Unlock()
	result := []*Task{}
	for _, build := range builders {
		part, err := build(ctx, statuses)
		if err != nil {
			return nil, err
		}
		result = append(result, part...)
	}
	return result, nil
}

// Runner returns the run function of a category.
func Runner(category string) RunFunc {
	mu.Lock()
	defer mu.Unlock()
	return runners[category]
}

// IntervalEditorFor returns the interval edit of a key.
func IntervalEditorFor(key string) IntervalEditor {
	mu.Lock()
	defer mu.Unlock()
	return intervals[key]
}
