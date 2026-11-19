package metrics

import (
	"configflow/internal/model"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"configflow/internal/agents"
	apitasks "configflow/internal/api/tasks"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

func bigOf(i int64) *big.Int { return big.NewInt(i) }

// fresh is a fresh process: no histories, no flush state, the registry
// hooks of this package only.
func fresh(t *testing.T) {
	t.Helper()
	reset := func() {
		// A tracked manual job may have released its key just before Runtime
		// finishes scope cleanup. Reconfigure only after that work is idle.
		deadline := time.Now().Add(5 * time.Second)
		for {
			err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			if err == nil {
				break
			}
			if !errors.Is(err, lifecycle.ErrRunning) || time.Now().After(deadline) {
				t.Fatal(err)
			}
			time.Sleep(time.Millisecond)
		}
		Reset()
		agents.ResetRuntime()
		agents.ResetHooks()
	}
	reset()
	t.Cleanup(reset)
	Wire()
}

func seedAgent(t *testing.T, ctx context.Context, r *repo.Repository, heartbeat bool) {
	t.Helper()
	agent := jsonx.Obj("id", "agent_1", "name", "Edge", "service_type", "mihomo", "profile_id", "default",
		"token", "agent-token-0123456789abcdef", "host", "h", "port", 8080)
	if heartbeat {
		agent.Set("last_heartbeat", isoformat(naiveNow()))
	}
	_, err := r.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{agent})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// agentManager is the agent_manager(tmp_path) helper: a repository with one
// Agent and its manager.
func agentManager(t *testing.T) (*agents.Manager, *repo.Repository, context.Context) {
	t.Helper()
	fresh(t)
	r, err := repo.Open(filepath.Join(t.TempDir(), "config"), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(repo.WithRepository(context.Background(), r))
	t.Cleanup(func() { repo.ResetConfigContext(ctx) })
	seedAgent(t, ctx, r, true)
	m, err := agents.New(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	return m, r, ctx
}

func heartbeat(t *testing.T, ctx context.Context, m *agents.Manager, metrics *jsonx.Object) {
	t.Helper()
	ok, err := m.ObserveHeartbeat(ctx, "agent_1", agentModelForTest(t, jsonx.Obj("system_metrics", metrics)))
	if !ok || err != nil {
		t.Fatalf("heartbeat %v %v", ok, err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestHeartbeatsStayInMemoryAndDeletingAnAgentDropsItsHistory(t *testing.T) {
	m, _, ctx := agentManager(t)
	heartbeat(t, ctx, m, sample(1))
	h := ForManager(m)
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
	m, r, ctx := agentManager(t)
	if ForManager(m).DataDir != filepath.Join(r.DataDir(), "metrics") {
		t.Fatal(ForManager(m).DataDir)
	}
	heartbeat(t, ctx, m, sample(1))
	ForManager(m).Flush()
	if !exists(filepath.Join(r.DataDir(), "metrics", "agent_1_metrics.jsonl")) {
		t.Fatal("not in the data directory")
	}
}

func TestBuildingAnAgentManagerDoesNotTouchSavedHistory(t *testing.T) {
	// Only service start (init_agent_manager) loads and cleans; building a
	// manager or its history does not. (The LEGACY_DIRS half is not ported.)
	fresh(t)
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
	ctx := repo.WithRepository(context.Background(), r)
	seedAgent(t, ctx, r, true)
	m, err := agents.New(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if ForManager(m).DataDir != metricsDir {
		t.Fatal(ForManager(m).DataDir)
	}
	if got := strings.Join(names(t, metricsDir), ","); got != ".agent_1_compacting.tmp,agent_1_metrics.json,someone_else_metrics.jsonl" {
		t.Fatal(got)
	}
}

// test_service_startup_adopts_old_files_before_loading without the
// Startup loads and
// migrates the saved history, once.
func TestServiceStartupLoadsAndMigratesSavedHistory(t *testing.T) {
	fresh(t)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	seedAgent(t, ctx, r, false)
	metricsDir := filepath.Join(r.DataDir(), "metrics")
	os.MkdirAll(metricsDir, 0o755)
	recent := pointAt(naiveNow().Add(-time.Minute), 4)
	text, _ := jsonx.Default(jsonx.Obj("metrics", []any{recent}))
	os.WriteFile(filepath.Join(metricsDir, "agent_1_metrics.json"), text, 0o644)
	m, err := agents.Init(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: historyStartupRegistry()})
	runtime.RunStartup(ctx)
	wantEqual(t, "loaded", get(t, ForManager(m), "agent_1", nil), []any{recent})
	if got := strings.Join(names(t, metricsDir), ","); got != "agent_1_metrics.jsonl" {
		t.Fatal(got)
	}
	// Startup runs once per generation.
	runtime.RunStartup(ctx)
	if len(get(t, ForManager(m), "agent_1", nil)) != 1 {
		t.Fatal("loaded twice")
	}
	if got := LoadHistory(ctx, m); got == nil || got.GetInt("loaded") != 1 || got.GetInt("migrated") != 0 {
		t.Fatal(got)
	}
}

// tasksApp is the tasks_app fixture: the task routes with this package's
// row and the other slots empty.
func tasksApp(t *testing.T) (*httpxtest.Server, *agents.Manager) {
	t.Helper()
	fresh(t)
	none := func(context.Context, *jsonx.Object) ([]*jsonx.Object, error) { return nil, nil }
	for _, slot := range []string{tasks.SlotSubscriptions, tasks.SlotRuleSources, tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains} {
		t.Cleanup(tasks.SetRows(slot, none))
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(apitasks.RegisterAlways)})
	seedAgent(t, srv.Context(), srv.Repo, false)
	m, err := agents.Get(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	return srv, m
}

func TestReviewTaskFixtureRestoresMissingSlots(t *testing.T) {
	fresh(t)
	before := tasks.Missing()
	t.Run("fixture", func(t *testing.T) { tasksApp(t) })
	if after := tasks.Missing(); !reflect.DeepEqual(before, after) {
		t.Fatal("fake task providers leaked", before, after)
	}
}

func taskRow(t *testing.T, srv *httpxtest.Server) *jsonx.Object {
	t.Helper()
	body := srv.Get("/api/tasks").WantStatus(200).Object()
	for _, item := range body.GetList("tasks") {
		if row := item.(*jsonx.Object); row.GetString("key") == TaskKey {
			return row
		}
	}
	t.Fatal("no metrics_history_flush row")
	return nil
}

func waitBackground(t *testing.T) {
	t.Helper()
	for i := 0; i < 1000 && tasks.IsBackground(TaskKey); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if tasks.IsBackground(TaskKey) {
		t.Fatal("background run did not finish")
	}
}

func TestTaskRowRunsNowAndSurfacesFailures(t *testing.T) {
	srv, m := tasksApp(t)
	ctx := srv.Context()
	row := taskRow(t, srv)
	wantEqual(t, "row", jsonx.Obj("category", row.Value("category"), "name", row.Value("name"),
		"interval_seconds", row.Value("interval_seconds"), "editable", row.Value("editable"), "can_run", row.Value("can_run"),
		"enabled", row.Value("enabled")), jsonx.Obj("category", "metrics_history", "name", "监控历史保存",
		"interval_seconds", 1800, "editable", true, "can_run", true, "enabled", true))
	for key, want := range map[string]any{"last_run_at": nil, "next_run_at": nil, "link": "/agents", "profile_id": nil,
		"detail": "Agent 系统监控（CPU、内存、网速） · Mihomo 流量记录"} {
		if v, present := row.Get(key); !present || !jsonx.Equal(v, want) {
			t.Fatalf("%s = %v", key, v)
		}
	}

	heartbeat(t, ctx, m, sample(1))
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.Obj("key", TaskKey))).WantStatus(202)
	waitBackground(t)
	row = taskRow(t, srv)
	if row.Value("last_status") != "success" || row.Value("last_trigger") != "manual" || row.GetInt("run_count") != 1 {
		t.Fatal(row)
	}
	if len(readLines(t, filepath.Join(ForManager(m).DataDir, "agent_1_metrics.jsonl"))) != 1 {
		t.Fatal("not saved")
	}

	previous := appendLines
	t.Cleanup(func() { appendLines = previous })
	appendLines = func(ctx context.Context, path string, _ []string) error {
		return &os.PathError{Op: "open", Path: path, Err: syscall.EROFS}
	}
	heartbeat(t, ctx, m, sample(2))
	srv.Post("/api/tasks/run", httpxtest.JSON(jsonx.Obj("key", TaskKey))).WantStatus(202)
	waitBackground(t)
	row = taskRow(t, srv)
	if row.Value("last_status") != "failed" || row.GetInt("failure_count") != 1 {
		t.Fatal(row)
	}
	if text := row.GetString("last_error"); !strings.Contains(text, "文件系统只读") || !strings.Contains(text, "下次保存时重试") {
		t.Fatal(text)
	}
	if ForManager(m).PendingCount() != 1 {
		t.Fatal(ForManager(m).PendingCount())
	}
}

func TestIntervalAcceptsOnlyTheOfferedChoicesAndReschedules(t *testing.T) {
	srv, _ := tasksApp(t)
	ctx := srv.Context()
	for _, value := range []any{jsonx.Int(120), jsonx.Int(900), "600", true, nil, jsonx.Int(0)} {
		body := srv.Put("/api/tasks/interval", httpxtest.JSON(jsonx.Obj("key", TaskKey, "interval_seconds", value))).
			WantStatus(400).Object()
		if body.GetString("message") != "保存间隔只能是 5、10、30 或 60 分钟" {
			t.Fatal(body)
		}
	}
	system, _ := srv.Repo.GetSystem(ctx)
	if system.GetObject("system_config").Has(Setting) {
		t.Fatal("stored")
	}

	started := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	SetStartedAt(&started)
	if got := taskRow(t, srv).Value("next_run_at"); got != "2026-10-24T00:30:00Z" {
		t.Fatal(got)
	}
	if ran, _ := RunDue(ctx, started.Add(10*time.Minute)); ran {
		t.Fatal("ran early")
	}
	body := srv.Put("/api/tasks/interval", httpxtest.JSON(jsonx.Obj("key", TaskKey, "interval_seconds", 300))).WantStatus(200).Object()
	if jsonx.Path(body, "task", "interval_seconds") != jsonx.Int(300) {
		t.Fatal(body)
	}
	system, _ = srv.Repo.GetSystem(ctx)
	if system.GetObject("system_config").Value(Setting) != jsonx.Int(300) {
		t.Fatal(system)
	}
	if ran, err := RunDue(ctx, started.Add(10*time.Minute)); !ran || err != nil {
		t.Fatal(ran, err)
	}
	entry, _ := taskstatus.New(srv.DataDir).Get(TaskKey)
	if entry.Value("last_trigger") != "schedule" {
		t.Fatal(entry)
	}
	// An unexpected stored value falls back to the default instead of breaking the loop.
	srv.Repo.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("system_config").Set(Setting, 7)
		return nil, nil
	})
	if got, _ := currentIntervalSeconds(ctx); got != 1800 {
		t.Fatal(got)
	}
}

func TestFailedStatusRecordDoesNotRerunEveryTick(t *testing.T) {
	srv, m := tasksApp(t)
	ctx := srv.Context()
	previous := recordStatus
	t.Cleanup(func() { recordStatus = previous })
	recordStatus = func(context.Context, string, taskstatus.Result) error {
		return &os.PathError{Op: "open", Err: syscall.ENOSPC}
	}
	current := time.Now().UTC()
	before := current.Add(-31 * time.Minute)
	SetStartedAt(&before)
	heartbeat(t, ctx, m, sample(1))
	if ran, err := RunDue(ctx, current); !ran || err != nil {
		t.Fatal(ran, err)
	}
	entry, _ := taskstatus.New(srv.DataDir).Get(TaskKey)
	if entry.Has("last_run_at") {
		t.Fatal(entry)
	}
	// The next tick waits a full interval from this attempt, not from startup.
	if ran, _ := RunDue(ctx, current.Add(TickSeconds*time.Second)); ran {
		t.Fatal("reran")
	}
	if ran, _ := RunDue(ctx, current.Add(31*time.Minute)); !ran {
		t.Fatal("did not run")
	}
}

func TestHistoryOfAgentsRemovedByImportIsDroppedOnTheNextSave(t *testing.T) {
	srv, m := tasksApp(t)
	ctx := srv.Context()
	h := ForManager(m)
	heartbeat(t, ctx, m, sample(1))
	if _, err := Run(ctx, "manual", true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.DataDir, "agent_1_metrics.jsonl")
	if !exists(path) {
		t.Fatal("not saved")
	}
	// Backup restore / reset replace the Agent list without deleting Agents.
	srv.Repo.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{})
		return nil, nil
	})
	if _, err := Run(ctx, "manual", true); err != nil {
		t.Fatal(err)
	}
	if exists(path) || len(get(t, h, "agent_1", nil)) != 0 || h.PendingCount() != 0 {
		t.Fatal("kept")
	}
	wantFlush(t, h, 0, 0, 0)
}

// test_stop_signal_handler_saves_then_exits: the stop path saves and
// records a "shutdown" run. (The repeated-signal default action is
// Python's signal handling; main owns signals in Go.)
func TestShutdownSavesAndRecordsTheRun(t *testing.T) {
	srv, m := tasksApp(t)
	ctx := srv.Context()
	heartbeat(t, ctx, m, sample(1))
	if result := FlushOnShutdown(ctx, true); result == nil || result.GetInt("appended") != 1 {
		t.Fatal(result)
	}
	if ForManager(m).PendingCount() != 0 {
		t.Fatal("pending")
	}
	entry, _ := taskstatus.New(srv.DataDir).Get(TaskKey)
	if entry.Value("last_trigger") != "shutdown" || entry.Value("last_status") != "success" {
		t.Fatal(entry)
	}
	// record_idle=False records nothing without new data; True always does.
	FlushOnShutdown(ctx, false)
	if again, _ := taskstatus.New(srv.DataDir).Get(TaskKey); again.GetInt("run_count") != 1 {
		t.Fatal(again)
	}
	FlushOnShutdown(ctx, true)
	if again, _ := taskstatus.New(srv.DataDir).Get(TaskKey); again.GetInt("run_count") != 2 {
		t.Fatal(again)
	}
}

// test_stop_signal_during_a_shutdown_save_is_ignored_instead_of_deadlocking:
// in Go a shutdown save that meets a running save waits for it; neither
// deadlocks and nothing is left pending.
func TestShutdownSaveDuringARunningSaveDoesNotDeadlock(t *testing.T) {
	srv, m := tasksApp(t)
	ctx := srv.Context()
	h := ForManager(m)
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
	go func() { Run(ctx, "schedule", true); done <- struct{}{} }()
	<-entered
	go func() { FlushOnShutdown(ctx, false); done <- struct{}{} }()
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

// test_real_startup_path_saves_on_sigterm_without_waiting_for_busy_threads
// in process: service start (init_agent_manager loads and migrates), the
// loops, a heartbeat, then the stop path main runs on SIGTERM
// (lifecycle.Stop) saves and records the run. The Ctrl+C and debug
// reloader variants are Python process mechanics (signal handlers, the
// Werkzeug reloader) with no Go counterpart.
func TestStartAndStopPathLoadsSavesAndRecords(t *testing.T) {
	fresh(t)
	dataDir := t.TempDir()
	r, err := repo.Open(dataDir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repo.SetRepository(r)
	t.Cleanup(func() { repo.SetRepository(nil) })
	ctx := repo.WithRepository(context.Background(), r)
	seedAgent(t, ctx, r, false)
	metricsDir := filepath.Join(dataDir, "metrics")
	os.MkdirAll(metricsDir, 0o755)
	oldPoint := pointAt(naiveNow().Add(-time.Minute), 3)
	text, _ := jsonx.Default(jsonx.Obj("agent_id", "agent_1", "metrics", []any{oldPoint}))
	os.WriteFile(filepath.Join(metricsDir, "agent_1_metrics.json"), text, 0o644)
	os.WriteFile(filepath.Join(metricsDir, "deleted_metrics.jsonl"), []byte(dumps(t, oldPoint)+"\n"), 0o644)

	m, err := agents.Init(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(lifecycle.Names(), ","), "metrics-history-flush") {
		t.Fatal(lifecycle.Names())
	}
	lifecycle.Start(ctx)
	heartbeat(t, ctx, m, jsonx.Obj("cpu", jsonx.Obj("usage_percent", 7)))
	lifecycle.Stop(10 * time.Second)

	if got := strings.Join(names(t, metricsDir), ","); got != "agent_1_metrics.jsonl" {
		t.Fatal(got)
	}
	var saved []any
	for _, line := range parsedLines(t, filepath.Join(metricsDir, "agent_1_metrics.jsonl")) {
		saved = append(saved, line.(*jsonx.Object).Value("cpu"))
	}
	wantEqual(t, "saved", saved, []any{jsonx.Obj("usage_percent", 3), jsonx.Obj("usage_percent", 7)})
	entry, _ := taskstatus.New(dataDir).Get(TaskKey)
	if entry.Value("last_status") != "success" || entry.Value("last_trigger") != "shutdown" || entry.GetInt("run_count") != 1 {
		t.Fatal(entry)
	}
}

// app.py saved twice on a stop signal (the handler with record_idle=True,
// then after serve() returned with record_idle=False): a save that keeps
// failing is recorded twice, a successful one once.
func TestShutdownFailureIsTriedAndRecordedTwiceLikeTheSignalPath(t *testing.T) {
	srv, m := tasksApp(t)
	ctx := srv.Context()
	heartbeat(t, ctx, m, sample(1))
	previous, previousReplace := appendLines, replaceFile
	t.Cleanup(func() { appendLines, replaceFile = previous, previousReplace })
	full := &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}
	appendLines = func(context.Context, string, []string) error { return full }
	// the second save rewrites the file (the failed append is not trusted)
	replaceFile = func(string, string) error { return full }
	shutdownFlush(ctx, true)
	shutdownFlush(ctx, false)
	entry, _ := taskstatus.New(srv.DataDir).Get(TaskKey)
	if entry.Value("last_status") != "failed" || entry.Value("last_trigger") != "shutdown" ||
		entry.GetInt("run_count") != 2 || entry.GetInt("failure_count") != 2 || entry.GetInt("consecutive_failures") != 2 {
		t.Fatal(entry)
	}
	if ForManager(m).PendingCount() != 1 {
		t.Fatal(ForManager(m).PendingCount())
	}
	appendLines, replaceFile = previous, previousReplace
	shutdownFlush(ctx, true)
	shutdownFlush(ctx, false)
	entry, _ = taskstatus.New(srv.DataDir).Get(TaskKey)
	if entry.Value("last_status") != "success" || entry.GetInt("run_count") != 3 || entry.GetInt("consecutive_failures") != 0 {
		t.Fatal(entry)
	}
}

func historyStartupRegistry() *lifecycle.Registry {
	reg := lifecycle.NewRegistry()
	reg.RegisterStartup(historyStartup())
	return reg
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
