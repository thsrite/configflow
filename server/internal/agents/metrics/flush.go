package metrics

// Port of backend/agents/metrics_flush.py: 系统设置 → 定时任务「监控历史保存」.
//
// Agent heartbeats only append to the History in memory. This task writes
// the new points to disk on the interval chosen in system_config, and once
// more when the service stops, so a graceful stop loses nothing. Every run,
// including failures such as a full disk or a read-only directory, is
// recorded in task_status for the task list.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"configflow/internal/faultprobe"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
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

var errInterval = &tasks.Invalid{Message: msgFlushInterval}

func allowedInterval(value int64) bool {
	for _, choice := range IntervalChoices {
		if value == choice {
			return true
		}
	}
	return false
}

func (s *Service) nowUTC() time.Time {
	if s != nil && s.utcNow != nil {
		return s.utcNow()
	}
	return time.Now().UTC().Truncate(time.Microsecond)
}

func (s *Service) setWake() { s.wake.Notify() }

// FlushInterval reads the exact stored setting, retaining the established default.
func FlushInterval(system *model.System) int64 {
	stored := system.SystemConfig.MetricsHistoryFlushInterval
	if stored == nil || !allowedInterval(int64(*stored)) {
		return DefaultInterval
	}
	return int64(*stored)
}

func (s *Service) currentIntervalSeconds(ctx context.Context) (int64, error) {
	if s == nil || s.repo == nil {
		return 0, repo.ErrRepository
	}
	settings, err := s.repo.SystemConfig(ctx)
	if err != nil {
		return 0, err
	}
	return FlushInterval(&model.System{SystemConfig: *settings}), nil
}

// SetFlushInterval updates the native setting and wakes the scheduler.
func (s *Service) SetFlushInterval(ctx context.Context, seconds int64) error {
	if !allowedInterval(seconds) {
		return errInterval
	}
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.repo == nil {
		return repo.ErrRepository
	}
	err := s.repo.UpdateSystem(ctx, func(ctx context.Context, sys *model.System) error {
		raw, err := json.Marshal(sys)
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
		return sys.SystemConfig.SetJSON(Setting, json.RawMessage(strconv.FormatInt(seconds, 10)))
	})
	if err == nil {
		s.setWake()
	}
	return err
}

// IsRunning is is_running().
func (s *Service) IsRunning() bool { return s.running.Load() }

// SetStartedAt records the loop start time on s (for tests that inspect NextFlushAt without starting the loop).
func (s *Service) SetStartedAt(t *time.Time) {
	s.stateMu.Lock()
	s.startedAt = t
	s.stateMu.Unlock()
}

// parseTimeKeepingOffset reads a receipt time keeping its offset, so the
// next due time is shown in the zone of the last run.
func parseTimeKeepingOffset(value string) (time.Time, bool) {
	return taskstatus.ParseTimestamp(value)
}

// NextFlushAt is next_run_at(entry, interval): the due time of the next
// scheduled run; nil before the loop has started.
func (s *Service) NextFlushAt(entry *taskstatus.Receipt, interval int64) *time.Time {
	s.stateMu.Lock()
	candidates := []*time.Time{nil, s.startedAt, s.attemptedAt}
	s.stateMu.Unlock()
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

func (s *Service) saveTaskStatus(ctx context.Context, dataDir string, result taskstatus.Result) error {
	if s.recordStatus != nil {
		return s.recordStatus(ctx, dataDir, result)
	}
	var store *taskstatus.Store
	if s.notifications != nil {
		store = s.notifications.TaskStatus(ctx, dataDir)
	} else {
		store = taskstatus.New(dataDir)
	}
	_, err := store.SaveResult(ctx, TaskKey, result)
	return err
}

// record is _record: a status that cannot be written is only logged.
func (s *Service) record(ctx context.Context, result taskstatus.Result) {
	err := func() error {
		if s.dataDir == "" {
			return repo.ErrRepository
		}
		return s.saveTaskStatus(ctx, s.dataDir, result)
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

// FlushNow saves every Agent's new metrics and traffic points and records the
// result in task_status; a failed run keeps the points in memory for the next
// one and returns the first error.
func (s *Service) FlushNow(ctx context.Context, trigger string, recordIdle bool) (*SaveResult, error) {
	ctx = lifecycle.Detach(ctx)
	ctx, release, err := s.runMu.Lock(ctx, 0)
	if err != nil {
		return nil, err
	}
	defer release()
	s.running.Store(true)
	defer s.running.Store(false)
	attempt := s.nowUTC()
	s.stateMu.Lock()
	s.attemptedAt = &attempt
	s.stateMu.Unlock()
	started, clock := taskstatus.NowISO(), time.Now()
	var failures []error
	result := &SaveResult{}
	var agentIDs []string
	var minimalSystem model.System
	if s.repo == nil {
		err = repo.ErrRepository
	} else {
		err = s.repo.View(ctx, func(doc *model.Document) error {
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
		if _, err := s.history.RetainMetricsAgents(ctx, agentIDs); err != nil {
			return nil, err
		}
		faultprobe.Point(ctx, faultprobe.MetricsFlush)
		counts, err := s.history.Flush(ctx)
		if err != nil {
			failures = append(failures, err)
		} else {
			result.FlushResult = counts
		}
		// 流量记录与监控历史一起保存；一项失败不影响另一项
		rows, err := s.flushTraffic(ctx, agentIDs, &minimalSystem)
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
		s.record(ctx, taskstatus.Result{OK: false, StartedAt: started, Trigger: trigger, Error: reason, DurationMs: duration})
		return nil, failures[0]
	}
	if recordIdle || result.Appended != 0 || result.Compacted != 0 || (result.TrafficRows != nil && *result.TrafficRows != 0) {
		s.record(ctx, taskstatus.Result{OK: true, StartedAt: started, Trigger: trigger, DurationMs: duration})
	}
	return result, nil
}

// RunDue runs one flush if the interval has passed; the interval is read every
// time. ran is true when a run started (err is its failure).
func (s *Service) RunDue(ctx context.Context, current time.Time) (ran bool, err error) {
	if s.repo == nil {
		return false, repo.ErrRepository
	}
	snapshot, err := taskstatus.New(s.dataDir).Read()
	if err != nil {
		return false, err
	}
	interval, err := s.currentIntervalSeconds(ctx)
	if err != nil {
		return false, err
	}
	due := s.NextFlushAt(snapshot.Entry(TaskKey), interval)
	if due == nil || due.After(current) {
		return false, nil
	}
	_, err = s.FlushNow(ctx, "schedule", true)
	return true, err
}

func (s *Service) isStopping() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.stopping
}

func (s *Service) flushPeriodic() lifecycle.Periodic {
	return lifecycle.Periodic{
		Name: "metrics-history-flush", Start: lifecycle.AtBoot,
		FirstDelay: TickSeconds * time.Second, Wake: &s.wake,
		Interval: func(context.Context) time.Duration { return TickSeconds * time.Second },
		OnStart: func(context.Context) {
			s.wake.Clear()
			s.stateMu.Lock()
			t := s.nowUTC()
			s.startedAt, s.attemptedAt, s.stopping = &t, nil, false
			s.stateMu.Unlock()
		},
		Round: func(ctx context.Context) error {
			if s.isStopping() {
				return nil
			}
			// Run owns recording and logging of ordinary save failures.
			_, _ = s.RunDue(ctx, s.nowUTC())
			return nil
		},
	}
}

// SaveOnShutdown is the final save when the service stops; never fails (nil
// result when the save failed).
func (s *Service) SaveOnShutdown(ctx context.Context, recordIdle bool) *SaveResult {
	s.stateMu.Lock()
	s.stopping = true
	s.stateMu.Unlock()
	s.setWake()
	result, err := s.FlushNow(ctx, "shutdown", recordIdle)
	if err != nil {
		return nil
	}
	if appended := result.Appended; appended != 0 {
		flushLogger.Infof("服务停止前已保存 %d 个监控点", appended)
	}
	return result
}

// LoadSavedHistory reads the saved history back at service start (and migrates
// the old format); nil when it failed (logged).
func (s *Service) LoadSavedHistory(ctx context.Context) *LoadResult {
	ctx = lifecycle.Detach(ctx)
	if ctx.Err() != nil {
		return nil
	}
	if s.repo == nil {
		flushLogger.Warningf("监控历史加载失败：%s", repo.ErrRepository)
		return nil
	}
	var agentIDs []string
	err := s.repo.ViewAgents(ctx, func(records []model.Agent) error {
		agentIDs = nativeAgentIDsFromRecords(records)
		return nil
	})
	if err != nil {
		flushLogger.Warningf("监控历史加载失败：%s", err)
		return nil
	}
	result, err := s.history.Load(ctx, agentIDs)
	if err != nil {
		flushLogger.Warningf("监控历史加载失败：%s", err)
		return nil
	}
	return &result
}

// BuildRows builds the one system-level row of the task list.
func (s *Service) BuildRows(ctx context.Context, statuses taskstatus.Snapshot) ([]*tasks.Task, error) {
	entry := statuses.Entry(TaskKey)
	interval, err := s.currentIntervalSeconds(ctx)
	if err != nil {
		return nil, err
	}
	history, err := tasks.ProjectHistory(entry)
	if err != nil {
		return nil, err
	}
	link := "/agents"
	return []*tasks.Task{{Key: TaskKey, Category: "metrics_history", Name: msgFlushTask, Detail: msgFlushDetail, Enabled: true, Editable: true, CanRun: true, Running: tasks.IsBackground(TaskKey) || s.IsRunning(), IntervalSeconds: json.RawMessage(strconv.FormatInt(interval, 10)), NextRunAt: flushISO(s.NextFlushAt(entry, interval)), Link: &link, HistoryData: history}}, nil
}

// runNow is the "run now" of routes/tasks.run_task for this task.
func (s *Service) runNow(ctx context.Context, key, _ string) error {
	if s.tasks == nil {
		_, err := s.FlushNow(ctx, "manual", true)
		return err
	}
	return s.tasks.StartBackground(ctx, key, func(ctx context.Context) error {
		_, err := s.FlushNow(ctx, "manual", true)
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
