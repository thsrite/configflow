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
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
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

var errInterval = &tasks.Invalid{Message: "保存间隔只能是 5、10、30 或 60 分钟"}

func choice(value any) (int64, bool) {
	number, ok := value.(jsonx.Number)
	if !ok {
		return 0, false
	}
	seconds, ok := number.Int64()
	if !ok {
		return 0, false
	}
	for _, allowed := range IntervalChoices {
		if seconds == allowed {
			return seconds, true
		}
	}
	return 0, false
}

// ValidateInterval is validate_interval: an int among IntervalChoices, else
// a *tasks.Invalid (ValueError).
func ValidateInterval(value any) (int64, error) {
	seconds, ok := choice(value)
	if !ok {
		return 0, errInterval
	}
	return seconds, nil
}

// IntervalSeconds is interval_seconds(system): the configured interval;
// anything unexpected falls back to 30 minutes.
func IntervalSeconds(system *jsonx.Object) int64 {
	settings := system.GetObject("system_config")
	value, present := settings.Get(Setting)
	if !present {
		return DefaultInterval
	}
	if seconds, ok := choice(value); ok {
		return seconds
	}
	return DefaultInterval
}

func currentIntervalSeconds(ctx context.Context) (int64, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return 0, err
	}
	system, err := repository.GetSystem(ctx)
	if err != nil {
		return 0, err
	}
	return IntervalSeconds(system), nil
}

// SetInterval is set_interval(seconds): store the interval and wake the
// loop so that it recomputes the next run without a restart.
func SetInterval(ctx context.Context, value any) error {
	seconds, err := ValidateInterval(value)
	if err != nil {
		return err
	}
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	_, err = repository.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		settings, present := system.Get("system_config")
		if !present {
			settings = jsonx.NewObject()
			system.Set("system_config", settings)
		}
		object, ok := settings.(*jsonx.Object)
		if !ok || object == nil {
			return nil, fmt.Errorf("'%s' object has no attribute 'update'", pyTypeName(settings))
		}
		object.Set(Setting, seconds)
		return nil, nil
	})
	if err != nil {
		return err
	}
	setWake()
	return nil
}

// IsRunning is is_running().
func IsRunning() bool { return running.Load() }

// parseTimeKeepingOffset is task_status.parse_time keeping the parsed
// offset (Python returns base + interval in base's zone).
func parseTimeKeepingOffset(value any) (time.Time, bool) {
	t, ok := taskstatus.ParseTime(value)
	if !ok {
		return t, false
	}
	text := strings.ReplaceAll(value.(string), "Z", "+00:00")
	// The offset begins at the last '+' or '-' after the date part.
	if index := strings.LastIndexAny(text, "+-"); index > 10 {
		if wall, ok := taskstatus.FromISOFormat(text[:index]); ok {
			offset := int(wall.Sub(t) / time.Second)
			return t.In(time.FixedZone("", offset)), true
		}
	}
	return t, true
}

// NextRunAt is next_run_at(entry, interval): the due time of the next
// scheduled run; nil before the loop has started.
func NextRunAt(entry *jsonx.Object, interval int64) *time.Time {
	stateMu.Lock()
	candidates := []*time.Time{nil, startedAt, attemptedAt}
	stateMu.Unlock()
	if last, ok := parseTimeKeepingOffset(entry.Value("last_run_at")); ok {
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

// TrafficFlush is the traffic-history half of run(): retain_agents and
// flush of manager.traffic_history (A05), returning flush()['rows']. A05
// registers it with SetTrafficFlush; until then run() saves only the
// monitoring history and its result has no "traffic_rows".
type TrafficFlush func(ctx context.Context, m *agents.Manager, agentIDs []string, system *jsonx.Object) (int64, error)

var (
	trafficMu    sync.Mutex
	trafficFlush TrafficFlush
)

// SetTrafficFlush installs the traffic-history save of run() (A05).
func SetTrafficFlush(fn TrafficFlush) {
	trafficMu.Lock()
	trafficFlush = fn
	trafficMu.Unlock()
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
	_, err := notifications.TaskStatus(ctx, dataDir).Record(TaskKey, result)
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

func agentIDsOf(system *jsonx.Object) []string {
	var ids []string
	for _, item := range system.GetList("agents") {
		object, _ := item.(*jsonx.Object)
		ids = append(ids, object.GetString("id"))
	}
	return ids
}

func errorText(err error) string {
	if text := err.Error(); text != "" {
		return text
	}
	return fmt.Sprintf("%T", err)
}

// Run is run(trigger, record_idle): save every Agent's new points and
// return the counts of History.Flush (plus "traffic_rows" once A05 wires
// the traffic history). The result, or the reason it failed, lands in
// task_status; a failed run keeps the points in memory for the next one and
// returns the first error.
func Run(ctx context.Context, trigger string, recordIdle bool) (*jsonx.Object, error) {
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
	result := FlushResult{}.Object()
	manager, err := agents.Get(ctx)
	var system *jsonx.Object
	if err == nil {
		system, err = manager.Repository().GetSystem(ctx)
	}
	if err != nil {
		failures = append(failures, err)
	} else {
		agentIDs := agentIDsOf(system)
		// 导入、恢复或重置配置后已不存在的 Agent，不再保存它们的历史
		history := ForManager(manager)
		history.RetainAgents(agentIDs)
		counts, err := history.Flush()
		if err != nil {
			failures = append(failures, err)
		} else {
			result = counts.Object()
		}
		// 流量记录与监控历史一起保存；一项失败不影响另一项
		trafficMu.Lock()
		traffic := trafficFlush
		trafficMu.Unlock()
		if traffic != nil {
			rows, err := traffic(ctx, manager, agentIDs, system)
			if err != nil {
				failures = append(failures, &TrafficHistoryFlushError{err})
			} else {
				result.Set("traffic_rows", rows)
			}
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
	if recordIdle || result.GetInt("appended") != 0 || result.GetInt("compacted") != 0 || jsonx.Truthy(result.Value("traffic_rows")) {
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
	entry, err := taskstatus.New(repository.DataDir()).Get(TaskKey)
	if err != nil {
		return false, err
	}
	interval, err := currentIntervalSeconds(ctx)
	if err != nil {
		return false, err
	}
	due := NextRunAt(entry, interval)
	if due == nil || due.After(current) {
		return false, nil
	}
	_, err = Run(ctx, "schedule", true)
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

// FlushOnShutdown is flush_on_shutdown(record_idle): the final save when
// the service stops; never fails (nil result when the save failed).
func FlushOnShutdown(ctx context.Context, recordIdle bool) *jsonx.Object {
	stateMu.Lock()
	stopping = true
	stateMu.Unlock()
	setWake()
	result, err := Run(ctx, "shutdown", recordIdle)
	if err != nil {
		return nil
	}
	if appended := result.GetInt("appended"); appended != 0 {
		flushLogger.Infof("服务停止前已保存 %d 个监控点", appended)
	}
	return result
}

// LoadHistory is load_history(): read the saved history back at service
// start (and migrate the old format); nil when it failed (logged).
func LoadHistory(ctx context.Context, manager *agents.Manager) *jsonx.Object {
	system, err := manager.Repository().GetSystem(ctx)
	if err != nil {
		flushLogger.Warningf("监控历史加载失败：%s", err)
		return nil
	}
	result, err := ForManager(manager).Load(agentIDsOf(system))
	if err != nil {
		flushLogger.Warningf("监控历史加载失败：%s", err)
		return nil
	}
	return result.Object()
}

// Rows is routes/tasks._metrics_row(statuses): the one system-level row of
// the task list.
func Rows(ctx context.Context, statuses *jsonx.Object) ([]*jsonx.Object, error) {
	entry, _ := statuses.Value(TaskKey).(*jsonx.Object)
	if entry == nil {
		entry = jsonx.NewObject()
	}
	interval, err := currentIntervalSeconds(ctx)
	if err != nil {
		return nil, err
	}
	fields := []any{"interval_seconds", interval,
		// 一个任务保存两样数据，写明各属于哪个软件
		"detail", "Agent 系统监控（CPU、内存、网速） · Mihomo 流量记录",
		"editable", true, "can_run", true, "running", tasks.IsBackground(TaskKey) || IsRunning(),
		"next_run_at", tasks.ISO(NextRunAt(entry, interval)), "link", "/agents"}
	fields = append(fields, tasks.Spread(tasks.History(entry))...)
	return []*jsonx.Object{tasks.Row(TaskKey, "metrics_history", "监控历史保存", fields...)}, nil
}

// runNow is the "run now" of routes/tasks.run_task for this task.
func runNow(ctx context.Context, key, _ string) error {
	return tasks.StartBackground(ctx, key, func(ctx context.Context) error {
		_, err := Run(ctx, "manual", true)
		return err
	})
}
