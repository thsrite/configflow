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

	"configflow/internal/jsonx"
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

// RowsFunc builds the rows of one slot from the loaded task statuses.
type RowsFunc func(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error)

// RunFunc starts a task now. Return a *Busy, *NotFound, *Invalid or
// *Conflict error for the user-facing failures.
type RunFunc func(ctx context.Context, key, resource string) error

// IntervalFunc sets the interval of a task (metrics history).
type IntervalFunc func(ctx context.Context, value any) error

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
	rows      = map[string]RowsFunc{SlotBackup: backupRows}
	runners   = map[string]RunFunc{}
	intervals = map[string]IntervalFunc{}
)

// SetRows installs the row builder of a slot and returns its restoration.
func SetRows(slot string, f RowsFunc) func() {
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

// SetIntervalSetter installs an exact-key editor and returns its restoration.
func SetIntervalSetter(key string, f IntervalFunc) func() {
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

// ISO is _iso(value): seconds precision, "Z" for UTC, nil for none.
func ISO(t *time.Time) any {
	if t == nil {
		return nil
	}
	text := t.Format("2006-01-02T15:04:05-07:00")
	return strings.Replace(text, "+00:00", "Z", 1)
}

var historyKeys = []string{"last_run_at", "last_status", "last_error", "last_duration_ms", "last_trigger",
	"last_success_at", "run_count", "failure_count", "consecutive_failures", "cache_retained", "cache_updated_at"}

// History is _history(entry).
func History(entry *jsonx.Object) (*jsonx.Object, error) {
	row := jsonx.NewObject()
	for _, key := range historyKeys {
		row.Set(key, entry.Value(key))
	}
	for _, key := range []string{"run_count", "failure_count"} {
		raw, err := jsonx.Compact(row.Value(key))
		if err != nil {
			return nil, err
		}
		count, err := historyCount(raw)
		if err != nil {
			return nil, fmt.Errorf("task history %s: %w", key, err)
		}
		row.Set(key, jsonx.BigInt(count))
	}
	return row, nil
}

// Row is _row(key, category, name, **fields): the defaults, then fields
// (pairs of key, value) in order, overriding defaults in place.
func Row(key, category, name string, fields ...any) *jsonx.Object {
	row := jsonx.Obj("key", key, "category", category, "name", name, "detail", "", "resource_id", "",
		"profile_id", nil, "profile_name", nil, "enabled", true, "interval_seconds", nil,
		"schedule_text", "", "editable", false, "can_run", false, "run_hint", "", "running", false,
		"next_run_at", nil, "state_message", "", "link", nil)
	for i := 0; i+1 < len(fields); i += 2 {
		if object, ok := fields[i].(*jsonx.Object); ok && fields[i+1] == nil {
			row.Update(object)
			continue
		}
		row.Set(fields[i].(string), fields[i+1])
	}
	return row
}

// Spread marks a *jsonx.Object to merge into a Row (Python's **history).
func Spread(o *jsonx.Object) []any { return []any{o, nil} }

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

func backupRows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	system, err := repository.GetSystem(ctx)
	if err != nil {
		return nil, err
	}
	backup, _ := system.Value("backup").(*jsonx.Object)
	configured := true
	for _, key := range []string{"webdav_url", "webdav_username", "webdav_password"} {
		if !jsonx.Truthy(backup.Value(key)) {
			configured = false
		}
	}
	entry, present := statuses.Get(BackupTaskKey)
	if !configured && !present {
		return nil, nil
	}
	entryObject, _ := entry.(*jsonx.Object)
	history, err := History(entryObject)
	if err != nil {
		return nil, err
	}
	autoBackup := jsonx.Truthy(backup.Value("auto_backup"))
	hint := ""
	if !configured {
		hint = "请先填写 WebDAV 配置"
	}
	fields := []any{"enabled", autoBackup,
		"notifications_enabled", autoBackup || (configured && history.Value("last_trigger") == "manual"),
		"schedule_text", "配置变更后自动备份", "can_run", configured, "run_hint", hint,
		"running", IsBackground(BackupTaskKey), "link", "/system-settings"}
	fields = append(fields, Spread(history)...)
	return []*jsonx.Object{Row(BackupTaskKey, "webdav_backup", "WebDAV 自动备份", fields...)}, nil
}

// ListTasks is list_tasks().
func ListTasks(ctx context.Context) ([]*jsonx.Object, error) {
	if !Complete() {
		return nil, ErrIncomplete
	}
	return AvailableTasks(ctx)
}

// AvailableTasks projects receipts only for registered task owners. Callers
// requiring a complete observation must check readiness or use ListTasks.
func AvailableTasks(ctx context.Context) ([]*jsonx.Object, error) {
	return availableRows(ctx, "")
}

// RowsFor returns only the selected provided slot, never a fake empty provider.
func RowsFor(ctx context.Context, slot string) ([]*jsonx.Object, error) {
	if !HasRows(slot) {
		return nil, &Unavailable{Slots: []string{slot}}
	}
	return availableRows(ctx, slot)
}

func availableRows(ctx context.Context, selected string) ([]*jsonx.Object, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	statuses, err := taskstatus.New(repository.DataDir()).Load()
	if err != nil {
		return nil, err
	}
	mu.Lock()
	builders := make([]RowsFunc, 0, len(slotOrder))
	for _, slot := range slotOrder {
		if selected != "" && slot != selected {
			continue
		}
		if build := rows[slot]; build != nil {
			builders = append(builders, build)
		}
	}
	mu.Unlock()
	result := []*jsonx.Object{}
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

// IntervalSetter returns the interval edit of a key.
func IntervalSetter(key string) IntervalFunc {
	mu.Lock()
	defer mu.Unlock()
	return intervals[key]
}
