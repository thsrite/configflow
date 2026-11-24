package metrics

import (
	"configflow/internal/timefmt"
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"configflow/internal/agents"
	apitasks "configflow/internal/api/tasks"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
)

func bigOf(i int64) *big.Int { return big.NewInt(i) }

func seedAgent(t *testing.T, ctx context.Context, r *repo.Repository, heartbeat bool) {
	t.Helper()
	agentMap := map[string]any{
		"id": "agent_1", "name": "Edge", "service_type": "mihomo", "profile_id": "default",
		"token": "agent-token-0123456789abcdef", "host": "h", "port": 8080,
	}
	if heartbeat {
		agentMap["last_heartbeat"] = timefmt.Naive(naiveNow())
	}
	agent := agentModelForTest(t, agentMap)
	if err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{*agent}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// agentManager builds an isolated repository, metrics Service, and Agent Manager.
func agentManager(t *testing.T) (*Service, *agents.Manager, *repo.Repository, context.Context) {
	t.Helper()
	r, err := repo.Open(filepath.Join(t.TempDir(), "config"), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(context.Background())
	t.Cleanup(func() { repo.ResetConfigContext(ctx) })
	seedAgent(t, ctx, r, true)
	svc := New(Options{Repo: r})
	m, err := agents.New(ctx, r, agents.Options{Metrics: svc})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return svc, m, r, ctx
}

func heartbeat(t *testing.T, ctx context.Context, m *agents.Manager, metrics map[string]any) {
	t.Helper()
	ok, err := m.ObserveHeartbeat(ctx, "agent_1", agentModelForTest(t, map[string]any{"system_metrics": metrics}))
	if !ok || err != nil {
		t.Fatalf("heartbeat %v %v", ok, err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestHeartbeatsStayInMemoryAndDeletingAnAgentDropsItsHistory(t *testing.T) {
	svc, m, _, ctx := agentManager(t)
	heartbeat(t, ctx, m, sample(1))
	h := svc.History()
	path := filepath.Join(h.DataDir, "agent_1_metrics.jsonl")
	if exists(path) || len(get(t, h, "agent_1", nil)) != 1 {
		t.Fatal("heartbeat")
	}
	h.Flush()
	if !exists(path) {
		t.Fatal("not saved")
	}
	if ok, err := m.DeleteAgent(ctx, "agent_1"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if exists(path) || len(get(t, h, "agent_1", nil)) != 0 {
		t.Fatal("history kept")
	}
	wantFlush(t, h, 0, 0, 0)
}

func TestAgentManagerKeepsHistoryInItsRepositoryDataDirectory(t *testing.T) {
	svc, m, r, ctx := agentManager(t)
	if svc.History().DataDir != filepath.Join(r.DataDir(), "metrics") {
		t.Fatal(svc.History().DataDir)
	}
	heartbeat(t, ctx, m, sample(1))
	svc.History().Flush()
	if !exists(filepath.Join(r.DataDir(), "metrics", "agent_1_metrics.jsonl")) {
		t.Fatal("not in the data directory")
	}
}

func TestBuildingAnAgentManagerDoesNotTouchSavedHistory(t *testing.T) {
	// Only service start loads and cleans; building a manager or its history does not.
	dataDir := filepath.Join(t.TempDir(), "config")
	metricsDir := filepath.Join(dataDir, "metrics")
	os.MkdirAll(metricsDir, 0o755)
	os.WriteFile(filepath.Join(metricsDir, "someone_else_metrics.jsonl"), []byte(`{"timestamp":"2026-10-24T12:00:00"}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(metricsDir, "agent_1_metrics.json"), []byte(`{"metrics":[]}`), 0o644)
	os.WriteFile(filepath.Join(metricsDir, ".agent_1_compacting.tmp"), []byte("in progress"), 0o644)
	r, err := repo.Open(dataDir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seedAgent(t, ctx, r, true)
	svc := New(Options{Repo: r})
	m, err := agents.New(ctx, r, agents.Options{Metrics: svc})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	if svc.History().DataDir != metricsDir {
		t.Fatal(svc.History().DataDir)
	}
	if got := strings.Join(names(t, metricsDir), ","); got != ".agent_1_compacting.tmp,agent_1_metrics.json,someone_else_metrics.jsonl" {
		t.Fatal(got)
	}
}

// Startup loads and migrates the saved history, once.
func TestServiceStartupLoadsAndMigratesSavedHistory(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seedAgent(t, ctx, r, false)
	metricsDir := filepath.Join(r.DataDir(), "metrics")
	os.MkdirAll(metricsDir, 0o755)
	recent := pointAt(naiveNow().Add(-time.Minute), 4)
	text, _ := json.Marshal(map[string]any{"metrics": []any{recent}})
	os.WriteFile(filepath.Join(metricsDir, "agent_1_metrics.json"), text, 0o644)
	svc := New(Options{Repo: r})
	reg := lifecycle.NewRegistry()
	reg.RegisterStartup(svc.Startup())
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: reg})
	runtime.RunStartup(ctx)
	wantEqual(t, "loaded", get(t, svc.History(), "agent_1", nil), []any{recent})
	if got := strings.Join(names(t, metricsDir), ","); got != "agent_1_metrics.jsonl" {
		t.Fatal(got)
	}
	// Startup runs once per generation.
	runtime.RunStartup(ctx)
	if len(get(t, svc.History(), "agent_1", nil)) != 1 {
		t.Fatal("loaded twice")
	}
	if got := svc.LoadSavedHistory(ctx); got == nil || got.Loaded != 1 || got.Migrated != 0 {
		t.Fatal(got)
	}
}

// tasksApp is the tasks_app fixture: the task routes with this package's
// row and the other slots empty.
func tasksApp(t *testing.T) (*httpxtest.Server, *Service, *agents.Manager, *tasks.Registry) {
	t.Helper()
	srv := httpxtest.New(t, httpxtest.Options{})
	seedAgent(t, srv.Context(), srv.Repo, false)
	tk := tasks.NewRegistry(srv.Repo, nil)
	none := func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }
	for _, slot := range []string{tasks.SlotSubscriptions, tasks.SlotRuleSources, tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains} {
		tk.SetRowBuilder(slot, none)
	}
	svc := New(Options{Repo: srv.Repo, Tasks: tk})
	svc.RegisterTasks(tk)
	m, err := agents.New(srv.Context(), srv.Repo, agents.Options{Metrics: svc})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	apitasks.NewHandlerWithRepo(tk, srv.Repo).Register(srv.App.Router)
	return srv, svc, m, tk
}

func taskRow(t *testing.T, srv *httpxtest.Server) map[string]any {
	t.Helper()
	res := srv.Get("/api/tasks").WantStatus(200)
	var body struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		t.Fatal(err)
	}
	for _, row := range body.Tasks {
		if row["key"] == TaskKey {
			return row
		}
	}
	t.Fatal("no metrics_history_flush row")
	return nil
}

func waitBackground(t *testing.T, _ *tasks.Registry) {
	t.Helper()
	for i := 0; i < 1000 && tasks.IsBackground(TaskKey); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if tasks.IsBackground(TaskKey) {
		t.Fatal("background run did not finish")
	}
}

func TestTaskRowRunsNowAndSurfacesFailures(t *testing.T) {
	srv, svc, m, tk := tasksApp(t)
	ctx := srv.Context()
	row := taskRow(t, srv)
	wantEqual(t, "row", map[string]any{
		"category": row["category"], "name": row["name"],
		"interval_seconds": row["interval_seconds"], "editable": row["editable"], "can_run": row["can_run"],
		"enabled": row["enabled"],
	}, map[string]any{
		"category": "metrics_history", "name": "监控历史保存",
		"interval_seconds": 1800, "editable": true, "can_run": true, "enabled": true,
	})
	for key, want := range map[string]any{
		"last_run_at": nil, "next_run_at": nil, "link": "/agents", "profile_id": nil,
		"detail": "Agent 系统监控（CPU、内存、网速） · Mihomo 流量记录",
	} {
		v, present := row[key]
		if !present || !testjson.Equal(want, v) {
			t.Fatalf("%s = %v", key, v)
		}
	}

	heartbeat(t, ctx, m, sample(1))
	srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{"key": TaskKey})).WantStatus(202)
	waitBackground(t, tk)
	row = taskRow(t, srv)
	if row["last_status"] != "success" || row["last_trigger"] != "manual" || !testjson.Equal(1, row["run_count"]) {
		t.Fatal(row)
	}
	if len(readLines(t, filepath.Join(svc.History().DataDir, "agent_1_metrics.jsonl"))) != 1 {
		t.Fatal("not saved")
	}

	previous := appendLines
	t.Cleanup(func() { appendLines = previous })
	appendLines = func(ctx context.Context, path string, _ []string) error {
		return &os.PathError{Op: "open", Path: path, Err: syscall.EROFS}
	}
	heartbeat(t, ctx, m, sample(2))
	srv.Post("/api/tasks/run", httpxtest.JSON(map[string]any{"key": TaskKey})).WantStatus(202)
	waitBackground(t, tk)
	row = taskRow(t, srv)
	if row["last_status"] != "failed" || !testjson.Equal(1, row["failure_count"]) {
		t.Fatal(row)
	}
	if text, _ := row["last_error"].(string); !strings.Contains(text, "文件系统只读") || !strings.Contains(text, "下次保存时重试") {
		t.Fatal(text)
	}
	if svc.History().PendingCount() != 1 {
		t.Fatal(svc.History().PendingCount())
	}
}

func TestIntervalAcceptsOnlyTheOfferedChoicesAndReschedules(t *testing.T) {
	srv, svc, _, _ := tasksApp(t)
	ctx := srv.Context()
	for _, value := range []any{120, 900, "600", true, nil, 0} {
		res := srv.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": TaskKey, "interval_seconds": value})).WantStatus(400)
		msg, _ := testjson.Path(json.RawMessage(res.Body), "message")
		if msg != "保存间隔只能是 5、10、30 或 60 分钟" {
			t.Fatalf("%s", res.Body)
		}
	}
	system, _ := srv.Repo.System(ctx)
	if _, ok, _ := model.EncodeMember(&system.SystemConfig, Setting); ok {
		t.Fatal("stored")
	}

	started := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	svc.stateMu.Lock()
	svc.startedAt = &started
	svc.stateMu.Unlock()
	if got := taskRow(t, srv)["next_run_at"]; got != "2026-10-24T00:30:00Z" {
		t.Fatal(got)
	}
	if ran, _ := svc.RunDue(ctx, started.Add(10*time.Minute)); ran {
		t.Fatal("ran early")
	}
	res := srv.Put("/api/tasks/interval", httpxtest.JSON(map[string]any{"key": TaskKey, "interval_seconds": 300})).WantStatus(200)
	if got, _ := testjson.Path(json.RawMessage(res.Body), "task", "interval_seconds"); !testjson.Equal(300, got) {
		t.Fatalf("%s", res.Body)
	}
	system, _ = srv.Repo.System(ctx)
	if raw, ok, _ := model.EncodeMember(&system.SystemConfig, Setting); !ok || !testjson.Equal(300, raw) {
		t.Fatalf("%s", raw)
	}
	if ran, err := svc.RunDue(ctx, started.Add(10*time.Minute)); !ran || err != nil {
		t.Fatal(ran, err)
	}
	entry := readReceipt(t, srv.DataDir)
	if entry.LastTrigger == nil || *entry.LastTrigger != "schedule" {
		t.Fatal(entry)
	}
	// An unexpected stored value falls back to the default instead of breaking the loop.
	_ = srv.Repo.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		return system.SystemConfig.SetJSON(Setting, json.RawMessage(`7`))
	})
	if got, _ := svc.currentIntervalSeconds(ctx); got != 1800 {
		t.Fatal(got)
	}
}

func TestFailedStatusRecordDoesNotRerunEveryTick(t *testing.T) {
	srv, svc, m, _ := tasksApp(t)
	ctx := srv.Context()
	svc.recordStatus = func(context.Context, string, taskstatus.Result) error {
		return &os.PathError{Op: "open", Err: syscall.ENOSPC}
	}
	current := time.Now().UTC()
	before := current.Add(-31 * time.Minute)
	svc.stateMu.Lock()
	svc.startedAt = &before
	svc.stateMu.Unlock()
	heartbeat(t, ctx, m, sample(1))
	if ran, err := svc.RunDue(ctx, current); !ran || err != nil {
		t.Fatal(ran, err)
	}
	entry := readReceipt(t, srv.DataDir)
	if entry.LastRunAt != nil {
		t.Fatal(entry)
	}
	// The next tick waits a full interval from this attempt, not from startup.
	if ran, _ := svc.RunDue(ctx, current.Add(TickSeconds*time.Second)); ran {
		t.Fatal("reran")
	}
	if ran, _ := svc.RunDue(ctx, current.Add(31*time.Minute)); !ran {
		t.Fatal("did not run")
	}
}

func TestHistoryOfAgentsRemovedByImportIsDroppedOnTheNextSave(t *testing.T) {
	srv, svc, m, _ := tasksApp(t)
	ctx := srv.Context()
	h := svc.History()
	heartbeat(t, ctx, m, sample(1))
	if _, err := svc.FlushNow(ctx, "manual", true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.DataDir, "agent_1_metrics.jsonl")
	if !exists(path) {
		t.Fatal("not saved")
	}
	// Backup restore / reset replace the Agent list without deleting Agents.
	_ = srv.Repo.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{}
		return nil
	})
	if _, err := svc.FlushNow(ctx, "manual", true); err != nil {
		t.Fatal(err)
	}
	if exists(path) || len(get(t, h, "agent_1", nil)) != 0 || h.PendingCount() != 0 {
		t.Fatal("kept")
	}
	wantFlush(t, h, 0, 0, 0)
}

func TestShutdownSavesAndRecordsTheRun(t *testing.T) {
	srv, svc, m, _ := tasksApp(t)
	ctx := srv.Context()
	heartbeat(t, ctx, m, sample(1))
	if result := svc.SaveOnShutdown(ctx, true); result == nil || result.Appended != 1 {
		t.Fatal(result)
	}
	if svc.History().PendingCount() != 0 {
		t.Fatal("pending")
	}
	entry := readReceipt(t, srv.DataDir)
	if entry.LastTrigger == nil || *entry.LastTrigger != "shutdown" || entry.LastStatus == nil || *entry.LastStatus != "success" {
		t.Fatal(entry)
	}
	// record_idle=False records nothing without new data; True always does.
	svc.SaveOnShutdown(ctx, false)
	if again := readReceipt(t, srv.DataDir); receiptCount(again.RunCount) != 1 {
		t.Fatal(again)
	}
	svc.SaveOnShutdown(ctx, true)
	if again := readReceipt(t, srv.DataDir); receiptCount(again.RunCount) != 2 {
		t.Fatal(again)
	}
}

func TestShutdownSaveDuringARunningSaveDoesNotDeadlock(t *testing.T) {
	srv, svc, m, _ := tasksApp(t)
	ctx := srv.Context()
	h := svc.History()
	heartbeat(t, ctx, m, sample(1))
	h.Flush()
	heartbeat(t, ctx, m, sample(2))
	entered, release := make(chan struct{}), make(chan struct{})
	previous := appendLines
	t.Cleanup(func() { appendLines = previous })
	var once sync.Once
	appendLines = func(ctx context.Context, path string, lines []string) error {
		once.Do(func() { close(entered); <-release })
		return previous(ctx, path, lines)
	}
	done := make(chan struct{}, 2)
	go func() { svc.FlushNow(ctx, "schedule", true); done <- struct{}{} }()
	<-entered
	go func() { svc.SaveOnShutdown(ctx, false); done <- struct{}{} }()
	heartbeat(t, ctx, m, sample(3))
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("shutdown save deadlocked")
		}
	}
	if h.PendingCount() != 0 {
		t.Fatal(h.PendingCount())
	}
}

func TestStartAndStopPathLoadsSavesAndRecords(t *testing.T) {
	dataDir := t.TempDir()
	r, err := repo.Open(dataDir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seedAgent(t, ctx, r, false)
	metricsDir := filepath.Join(dataDir, "metrics")
	os.MkdirAll(metricsDir, 0o755)
	oldPoint := pointAt(naiveNow().Add(-time.Minute), 3)
	text, _ := json.Marshal(map[string]any{"agent_id": "agent_1", "metrics": []any{oldPoint}})
	os.WriteFile(filepath.Join(metricsDir, "agent_1_metrics.json"), text, 0o644)
	os.WriteFile(filepath.Join(metricsDir, "deleted_metrics.jsonl"), []byte(dumps(t, oldPoint)+"\n"), 0o644)

	svc := New(Options{Repo: r})
	m, err := agents.New(ctx, r, agents.Options{Metrics: svc})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.RegisterStartup(svc.Startup())
	rt.RegisterPeriodic(svc.Loop())
	for _, hook := range svc.ShutdownHooks() {
		rt.RegisterShutdown(hook)
	}
	if !strings.Contains(strings.Join(rt.Names(), ","), "metrics-history-flush") {
		t.Fatal(rt.Names())
	}
	rt.RunStartup(ctx)
	rt.Start(ctx)
	heartbeat(t, ctx, m, map[string]any{"cpu": map[string]any{"usage_percent": 7}})
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rt.Shutdown(stopCtx)

	if got := strings.Join(names(t, metricsDir), ","); got != "agent_1_metrics.jsonl" {
		t.Fatal(got)
	}
	var saved []any
	for _, line := range parsedLines(t, filepath.Join(metricsDir, "agent_1_metrics.jsonl")) {
		cpu, _ := testjson.Path(line, "cpu")
		saved = append(saved, cpu)
	}
	wantEqual(t, "saved", saved, []any{map[string]any{"usage_percent": 3}, map[string]any{"usage_percent": 7}})
	entry := readReceipt(t, dataDir)
	if entry.LastStatus == nil || *entry.LastStatus != "success" || entry.LastTrigger == nil || *entry.LastTrigger != "shutdown" || receiptCount(entry.RunCount) != 1 {
		t.Fatal(entry)
	}
}

func TestShutdownFailureIsTriedAndRecordedTwiceLikeTheSignalPath(t *testing.T) {
	srv, svc, m, _ := tasksApp(t)
	ctx := srv.Context()
	heartbeat(t, ctx, m, sample(1))
	previous, previousReplace := appendLines, replaceFile
	t.Cleanup(func() { appendLines, replaceFile = previous, previousReplace })
	full := &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}
	appendLines = func(context.Context, string, []string) error { return full }
	// the second save rewrites the file (the failed append is not trusted)
	replaceFile = func(string, string) error { return full }
	svc.shutdownFlush(ctx, true)
	svc.shutdownFlush(ctx, false)
	entry := readReceipt(t, srv.DataDir)
	if entry.LastStatus == nil || *entry.LastStatus != "failed" || entry.LastTrigger == nil || *entry.LastTrigger != "shutdown" ||
		receiptCount(entry.RunCount) != 2 || receiptCount(entry.FailureCount) != 2 || receiptCount(entry.ConsecutiveFailures) != 2 {
		t.Fatal(entry)
	}
	if svc.History().PendingCount() != 1 {
		t.Fatal(svc.History().PendingCount())
	}
	appendLines, replaceFile = previous, previousReplace
	svc.shutdownFlush(ctx, true)
	svc.shutdownFlush(ctx, false)
	entry = readReceipt(t, srv.DataDir)
	if entry.LastStatus == nil || *entry.LastStatus != "success" || receiptCount(entry.RunCount) != 3 || receiptCount(entry.ConsecutiveFailures) != 0 {
		t.Fatal(entry)
	}
}

func agentModelForTest(t testing.TB, value any) *model.Agent {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var a model.Agent
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}
