package metrics

// Port of backend/agents/metrics_flush.py: 系统设置 → 定时任务「监控历史保存」.
//
// Agent heartbeats only append to the History in memory. This task writes
// the new points to disk on the interval chosen in system_config, and once
// more when the service stops, so a graceful stop loses nothing. Every run,
// including failures such as a full disk or a read-only directory, is
// recorded in task_status for the task list.
//
// Python's stop-signal handling (handle_stop_signal, exit_after_stop_signal,
// install_signal_handlers: save on SIGTERM/SIGINT, then exit without waiting
// for busy threads; a second Ctrl+C force-quits) is the Go server's shutdown
// path: main stops on SIGTERM/SIGINT and lifecycle.Stop runs the shutdown
// hook registered by Wire (FlushOnShutdown) after the HTTP servers stopped.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"configflow/internal/agents"
	"configflow/internal/faultprobe"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"encoding/json"
	"strconv"
)

var flushLogger = logx.Get("backend.agents.metrics_flush")

const (
	// TaskKey is TASK_KEY.
	TaskKey = tasks.MetricsTaskKey
	// Setting is SETTING (system_config field).
	Setting = "metrics_history_flush_interval"
	// DefaultInterval is DEFAULT_INTERVAL (seconds).
	DefaultInterval = 1800
	// TickSeconds is TICK_SECONDS.
	TickSeconds = 30
)

// IntervalChoices is INTERVAL_CHOICES.
var IntervalChoices = []int64{300, 600, 1800, 3600}

var (
	// runMu is the L2 save operation, spanning history I/O and its receipt.
	runMu   = lockx.NewMutex(lockx.LevelDevice, "metrics-history-save")
	running atomic.Bool
	// wake coalesces interval edits without launching overlapping rounds.
	wake lifecycle.Signal

	stateMu sync.Mutex
	// startedAt is _started_at: when the loop started (nil before).
	startedAt *time.Time
	// attemptedAt is _attempted_at: when this process last started a save,
	// so that a run whose record could not be written does not rerun every
	// tick.
	attemptedAt *time.Time
	// stopping is _stopping: the service is stopping.
	stopping bool
)

// utcNow is datetime.now(timezone.utc) (tests replace it).
var utcNow = func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// ResetFlushState restores the process-wide flush state of a fresh process
// (tests; Python's tests patched the module globals).
func ResetFlushState() {
	stateMu.Lock()
	startedAt, attemptedAt, stopping = nil, nil, false
	stateMu.Unlock()
	wake.Clear()
}

// SetStartedAt sets _started_at (tests).
func SetStartedAt(t *time.Time) {
	stateMu.Lock()
	startedAt = t
	stateMu.Unlock()
}

func setWake() { wake.Notify() }

var errInterval = &tasks.Invalid{Message: msgFlushInterval}

func allowedInterval(value int64) bool {
	for _, choice := range IntervalChoices {
		if value == choice {
			return true
		}
	}
	return false
}

// FlushInterval reads the exact stored setting, retaining the established default.
func FlushInterval(system *model.System) int64 {
	stored := system.SystemConfig.MetricsHistoryFlushInterval
	if stored == nil || !allowedInterval(int64(*stored)) {
		return DefaultInterval
	}
	return int64(*stored)
}
func currentIntervalSeconds(ctx context.Context) (int64, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return 0, err
	}
	settings, err := r.SystemConfig(ctx)
	if err != nil {
		return 0, err
	}
	return FlushInterval(&model.System{SystemConfig: *settings}), nil
}

// SetFlushInterval updates the native setting and wakes the scheduler.
func SetFlushInterval(ctx context.Context, seconds int64) error {
	if !allowedInterval(seconds) {
		return errInterval
	}
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	err = r.UpdateSystem(ctx, func(ctx context.Context, s *model.System) error {
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		f, err := jsondecode.Select(raw, "system_config")
		if err != nil {
			return err
		}
		if v, exists := f["system_config"]; exists && (len(v) == 0 || v[0] != '{') {
			return ErrTrafficSettingsShape
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return s.SystemConfig.SetJSON(Setting, json.RawMessage(strconv.FormatInt(seconds, 10)))
	})
	if err == nil {
		setWake()
	}
	return err
}

// IsRunning is is_running().
func IsRunning() bool { return running.Load() }

// parseTimeKeepingOffset reads a receipt time keeping its offset, so the
// next due time is shown in the zone of the last run.
func parseTimeKeepingOffset(value string) (time.Time, bool) {
	return taskstatus.ParseTimestamp(value)
}

// NextFlushAt is next_run_at(entry, interval): the due time of the next
// scheduled run; nil before the loop has started.
func NextFlushAt(entry *taskstatus.Receipt, interval int64) *time.Time {
	stateMu.Lock()
	candidates := []*time.Time{nil, startedAt, attemptedAt}
	stateMu.Unlock()
	text := ""
	if entry != nil && entry.LastRunAt != nil {
		text = *entry.LastRunAt
	}
	if last, ok := parseTimeKeepingOffset(text); ok {
		candidates[0] = &last
	}
	var base *time.Time
	for _, candidate := range candidates {
		if candidate != nil && (base == nil || candidate.After(*base)) {
			base = candidate
		}
	}
	if base == nil {
		return nil
	}
	due := base.Add(time.Duration(interval) * time.Second)
	return &due
}

// TrafficHistoryFlushError is TrafficHistoryFlushError(error).
type TrafficHistoryFlushError struct{ Err error }

func (e *TrafficHistoryFlushError) Error() string {
	if text := e.Err.Error(); text != "" {
		return "流量记录：" + text
	}
	return "流量记录：" + fmt.Sprintf("%T", e.Err)
}

func (e *TrafficHistoryFlushError) Unwrap() error { return e.Err }

// recordStatus is task_status.record (tests replace it).
var recordStatus = func(ctx context.Context, dataDir string, result taskstatus.Result) error {
	_, err := notifications.TaskStatus(ctx, dataDir).SaveResult(ctx, TaskKey, result)
	return err
}

// record is _record: a status that cannot be written is only logged.
func record(ctx context.Context, result taskstatus.Result) {
	err := func() error {
		repository, err := repo.GetRepository(ctx)
		if err != nil {
			return err
		}
		return recordStatus(ctx, repository.DataDir(), result)
	}()
	if err != nil {
		// 磁盘满时运行记录也可能写不进去；日志里仍能看到保存结果
		flushLogger.Warningf("监控历史保存结果记录失败：%s", err)
	}
}

func nativeAgentIDsFromRecords(agents []model.Agent) []string {
	ids := make([]string, 0, len(agents))
	for i := range agents {
		ids = append(ids, agents[i].ID)
	}
	return ids
}

func errorText(err error) string {
	if text := err.Error(); text != "" {
		return text
	}
	return fmt.Sprintf("%T", err)
}

// FlushNow saves (trigger, record_idle): save every Agent's new points and
// return the counts of History.Flush (plus "traffic_rows" once A05 wires
// the traffic history). The result, or the reason it failed, lands in
// task_status; a failed run keeps the points in memory for the next one and
// returns the first error.
func FlushNow(ctx context.Context, trigger string, recordIdle bool) (*SaveResult, error) {
	ctx = lifecycle.Detach(ctx)
	ctx, release, err := runMu.Lock(ctx, 0)
	if err != nil {
		return nil, err
	}
	defer release()
	running.Store(true)
	defer running.Store(false)
	attempt := utcNow()
	stateMu.Lock()
	attemptedAt = &attempt
	stateMu.Unlock()
	started, clock := taskstatus.NowISO(), time.Now()
	var failures []error
	result := &SaveResult{}
	manager, err := agents.Get(ctx)
	var agentIDs []string
	var minimalSystem model.System
	if err == nil {
		err = manager.Repository().View(ctx, func(doc *model.Document) error {
			agentIDs = nativeAgentIDsFromRecords(doc.System.Agents)
			raw, present, memberErr := repo.SystemMemberJSON(&doc.System, "system_config")
			if memberErr != nil {
				return memberErr
			}
			if present {
				return minimalSystem.SetJSON("system_config", raw)
			}
			return nil
		})
	}
	if err != nil {
		failures = append(failures, err)
	} else {
		// 导入、恢复或重置配置后已不存在的 Agent，不再保存它们的历史
		history, historyErr := historyContext(ctx, manager.Repository().DataDir())
		if historyErr != nil {
			return nil, historyErr
		}
		if _, err := history.RetainMetricsAgents(ctx, agentIDs); err != nil {
			return nil, err
		}
		faultprobe.Point(ctx, faultprobe.MetricsFlush)
		counts, err := history.FlushContext(ctx)
		if err != nil {
			failures = append(failures, err)
		} else {
			result.FlushResult = counts
		}
		// 流量记录与监控历史一起保存；一项失败不影响另一项
		rows, err := flushTraffic(ctx, manager, agentIDs, &minimalSystem)
		if err != nil {
			failures = append(failures, &TrafficHistoryFlushError{err})
		} else {
			result.TrafficRows = &rows
		}
	}
	duration := float64(time.Since(clock).Microseconds()) / 1000
	if len(failures) > 0 {
		reasons := make([]string, len(failures))
		for i, failure := range failures {
			reasons[i] = errorText(failure)
		}
		reason := strings.Join(reasons, "；")
		flushLogger.Warningf("监控历史保存失败：%s", reason)
		record(ctx, taskstatus.Result{OK: false, StartedAt: started, Trigger: trigger, Error: reason, DurationMs: duration})
		return nil, failures[0]
	}
	if recordIdle || result.Appended != 0 || result.Compacted != 0 || (result.TrafficRows != nil && *result.TrafficRows != 0) {
		record(ctx, taskstatus.Result{OK: true, StartedAt: started, Trigger: trigger, DurationMs: duration})
	}
	return result, nil
}

// RunDue is run_due(now): run once if the interval has passed; the interval
// is read every time. ran is true when a run started (err is its failure).
func RunDue(ctx context.Context, current time.Time) (ran bool, err error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return false, err
	}
	snapshot, err := taskstatus.New(repository.DataDir()).Read()
	if err != nil {
		return false, err
	}
	interval, err := currentIntervalSeconds(ctx)
	if err != nil {
		return false, err
	}
	due := NextFlushAt(snapshot.Entry(TaskKey), interval)
	if due == nil || due.After(current) {
		return false, nil
	}
	_, err = FlushNow(ctx, "schedule", true)
	return true, err
}

func isStopping() bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return stopping
}

func flushPeriodic() lifecycle.Periodic {
	return lifecycle.Periodic{
		Name: "metrics-history-flush", Start: lifecycle.AtBoot,
		FirstDelay: TickSeconds * time.Second, Wake: &wake,
		Interval: func(context.Context) time.Duration { return TickSeconds * time.Second },
		OnStart: func(context.Context) {
			wake.Clear()
			stateMu.Lock()
			t := utcNow()
			startedAt, attemptedAt, stopping = &t, nil, false
			stateMu.Unlock()
		},
		Round: func(ctx context.Context) error {
			if isStopping() {
				return nil
			}
			// Run owns recording and logging of ordinary save failures.
			_, _ = RunDue(ctx, utcNow())
			return nil
		},
	}
}

// SaveOnShutdown is flush_on_shutdown(record_idle): the final save when
// the service stops; never fails (nil result when the save failed).
func SaveOnShutdown(ctx context.Context, recordIdle bool) *SaveResult {
	stateMu.Lock()
	stopping = true
	stateMu.Unlock()
	setWake()
	result, err := FlushNow(ctx, "shutdown", recordIdle)
	if err != nil {
		return nil
	}
	if appended := result.Appended; appended != 0 {
		flushLogger.Infof("服务停止前已保存 %d 个监控点", appended)
	}
	return result
}

// LoadSavedHistory is load_history(): read the saved history back at service
// start (and migrate the old format); nil when it failed (logged).
func LoadSavedHistory(ctx context.Context, manager *agents.Manager) *LoadResult {
	ctx = lifecycle.Detach(ctx)
	if ctx.Err() != nil {
		return nil
	}
	var agentIDs []string
	err := manager.Repository().ViewAgents(ctx, func(records []model.Agent) error {
		agentIDs = nativeAgentIDsFromRecords(records)
		return nil
	})
	if err != nil {
		flushLogger.Warningf("监控历史加载失败：%s", err)
		return nil
	}
	history, err := historyContext(ctx, manager.Repository().DataDir())
	if err != nil {
		return nil
	}
	result, err := history.LoadContext(ctx, agentIDs)
	if err != nil {
		flushLogger.Warningf("监控历史加载失败：%s", err)
		return nil
	}
	return &result
}

// BuildRows is routes/tasks._metrics_row(statuses): the one system-level row of
// the task list.
func BuildRows(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	entry := statuses.Entry(TaskKey)
	interval, err := currentIntervalSeconds(ctx)
	if err != nil {
		return nil, err
	}
	history, err := tasks.ProjectHistory(entry)
	if err != nil {
		return nil, err
	}
	link := "/agents"
	return []*tasks.Task{{Key: TaskKey, Category: "metrics_history", Name: msgFlushTask, Detail: msgFlushDetail, Enabled: true, Editable: true, CanRun: true, Running: tasks.IsBackground(TaskKey) || IsRunning(), IntervalSeconds: json.RawMessage(strconv.FormatInt(interval, 10)), NextRunAt: flushISO(NextFlushAt(entry, interval)), Link: &link, HistoryData: history}}, nil
}

// runNow is the "run now" of routes/tasks.run_task for this task.
func runNow(ctx context.Context, key, _ string) error {
	return tasks.StartBackground(ctx, key, func(ctx context.Context) error {
		_, err := FlushNow(ctx, "manual", true)
		return err
	})
}

// SaveResult contains the native counts for one scheduled save.
type SaveResult struct {
	FlushResult
	TrafficRows *int64 `json:"traffic_rows,omitempty"`
}

func flushISO(at *time.Time) *string {
	if at == nil {
		return nil
	}
	text := at.Format(time.RFC3339)
	return &text
}
