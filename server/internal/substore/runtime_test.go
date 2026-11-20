package substore

// Port of backend/test_sub_store_runtime.py (lifecycle regressions: no
// idle shutdown during work, polling or updates). Python patched _ready and
// _command; here the runtime talks to a fake Sub-Store through the real
// client and supervisorctl is a fake CommandRunner driving it.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

// lifecycleService is the Python fixture `service`: idle_seconds=0, mode in
// memory.
func lifecycleService(t *testing.T) (*harness, *Runtime, *string) {
	h := newHarness(t, harnessOptions{builtin: true, realClock: true})
	rt := h.service.Runtime
	rt.IdleSeconds = 0
	mode := "on_demand"
	var mu sync.Mutex
	rt.getMode = func(context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return mode, nil
	}
	rt.saveMode = func(_ context.Context, value string) error {
		mu.Lock()
		defer mu.Unlock()
		mode = value
		return nil
	}
	return h, rt, &mode
}

func waitFor(t *testing.T, ch <-chan struct{}, d time.Duration) bool {
	t.Helper()
	select {
	case <-ch:
		return true
	case <-time.After(d):
		return false
	}
}

func process(h *harness) (bool, int, int) {
	starts, stops := h.commands.counts()
	return h.store.running(), starts, stops
}

// test_sub_store_runtime.py::test_concurrent_conversions_share_start_and_cannot_be_stopped
func TestConcurrentConversionsShareStartAndCannotBeStopped(t *testing.T) {
	h, rt, _ := lifecycleService(t)
	ctx := context.Background()
	ready, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	convert := func() error {
		return rt.Use(ctx, func(ctx context.Context) error {
			once.Do(func() { close(ready) })
			if !waitFor(t, release, 3*time.Second) {
				return errors.New("not released")
			}
			return nil
		})
	}
	errs := make(chan error, 2)
	go func() { errs <- convert() }()
	if !waitFor(t, ready, 3*time.Second) {
		t.Fatal("first conversion did not start")
	}
	go func() { errs <- convert() }()
	time.Sleep(100 * time.Millisecond)
	if err := rt.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if running, starts, stops := process(h); !running || starts != 1 || stops != 0 {
		t.Fatalf("process running=%v starts=%d stops=%d", running, starts, stops)
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	rt.Tick(ctx)
	if _, _, stops := process(h); stops != 1 {
		t.Fatalf("stops = %d", stops)
	}
	rt.Use(ctx, func(ctx context.Context) error {
		if _, starts, _ := process(h); starts != 2 {
			t.Errorf("starts = %d", starts)
		}
		return nil
	})
}

// test_sub_store_runtime.py::test_polling_never_starts_or_extends_idle_time
func TestPollingNeverStartsOrExtendsIdleTime(t *testing.T) {
	h, rt, _ := lifecycleService(t)
	ctx := context.Background()
	if status := must[*RuntimeStatus](t)(rt.Snapshot(ctx, false)); status.State != "sleeping" {
		t.Fatalf("status %+v", status)
	}
	if _, starts, _ := process(h); starts != 0 {
		t.Fatal("polling started Sub-Store")
	}
	rt.Use(ctx, func(ctx context.Context) error { return nil })
	completed := lastUsed(stateOf(rt))
	for i := 0; i < 5; i++ {
		if must[*RuntimeStatus](t)(rt.Snapshot(ctx, true)).State != "running" {
			t.Fatal("not running")
		}
	}
	if lastUsed(stateOf(rt)) != completed {
		t.Fatal("polling extended the idle time")
	}
	rt.Tick(ctx)
	if h.store.running() {
		t.Fatal("still running")
	}
	if must[*RuntimeStatus](t)(rt.Snapshot(ctx, false)).State != "sleeping" {
		t.Fatal("not sleeping")
	}
}

// test_sub_store_runtime.py::test_idle_time_begins_after_failed_task_finishes
func TestIdleTimeBeginsAfterFailedTaskFinishes(t *testing.T) {
	h, rt, _ := lifecycleService(t)
	ctx := context.Background()
	rt.IdleSeconds = 300
	failure := errors.New("conversion failed")
	err := rt.Use(ctx, func(ctx context.Context) error {
		rt.withControl(ctx, func(ctx context.Context) error {
			ts := SystemClock.Monotonic() - 400
			return rt.writeState(ctx, runtimePatch{LastUsed: &ts})
		})
		rt.Tick(ctx)
		if !h.store.running() {
			t.Error("stopped during work")
		}
		return failure
	})
	if err != failure {
		t.Fatalf("err = %v", err)
	}
	rt.Tick(ctx)
	if !h.store.running() {
		t.Fatal("stopped right after the failed task")
	}
	if SystemClock.Monotonic()-lastUsed(stateOf(rt)) >= 1 {
		t.Fatal("idle time not counted from completion")
	}
}

// test_sub_store_runtime.py::test_update_waits_for_conversion_and_blocks_new_work_and_stop
func TestUpdateWaitsForConversionAndBlocksNewWorkAndStop(t *testing.T) {
	_, rt, _ := lifecycleService(t)
	ctx := context.Background()
	updating, finish, converted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	updateDone, convertDone := make(chan error, 1), make(chan error, 1)
	rt.Use(ctx, func(ctx context.Context) error {
		go func() {
			updateDone <- rt.Maintenance(context.Background(), func(ctx context.Context) error {
				close(updating)
				if !waitFor(t, finish, 3*time.Second) {
					return errors.New("not finished")
				}
				return nil
			})
		}()
		if waitFor(t, updating, 150*time.Millisecond) {
			t.Error("update ran during a conversion")
		}
		return nil
	})
	if !waitFor(t, updating, 3*time.Second) {
		t.Fatal("update did not start")
	}
	if must[*RuntimeStatus](t)(rt.Snapshot(ctx, true)).State != "updating" {
		t.Fatal("state is not updating")
	}
	go func() { convertDone <- rt.Use(ctx, func(ctx context.Context) error { close(converted); return nil }) }()
	if waitFor(t, converted, 150*time.Millisecond) {
		t.Fatal("new work ran during the update")
	}
	rt.Tick(ctx)
	h := rt.env.Commands.(*fakeCommands)
	if _, stops := h.counts(); stops != 0 {
		t.Fatal("stopped during the update")
	}
	close(finish)
	if err := <-updateDone; err != nil {
		t.Fatal(err)
	}
	if err := <-convertDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-converted:
	default:
		t.Fatal("conversion did not run")
	}
}

// test_sub_store_runtime.py::test_start_failure_can_retry_and_is_not_shown_as_sleeping
func TestStartFailureCanRetryAndIsNotShownAsSleeping(t *testing.T) {
	h, rt, _ := lifecycleService(t)
	ctx := context.Background()
	rt.command = func(context.Context, string) (string, error) { return "", unavailable("启动失败") }
	ran := false
	err := rt.Use(ctx, func(ctx context.Context) error { ran = true; return nil })
	if !IsRuntimeUnavailable(err) || ran {
		t.Fatalf("err=%v ran=%v (work must not run before readiness)", err, ran)
	}
	if must[*RuntimeStatus](t)(rt.Snapshot(ctx, false)).State != "failed" {
		t.Fatal("not failed")
	}
	rt.command = nil
	rt.Use(ctx, func(ctx context.Context) error {
		if !h.store.running() {
			t.Error("not running")
		}
		return nil
	})
	if must[*RuntimeStatus](t)(rt.Snapshot(ctx, true)).Error != "" {
		t.Fatal("error kept")
	}
}

// test_sub_store_runtime.py::test_mode_switch_and_boot_respect_always_on
func TestModeSwitchAndBootRespectAlwaysOn(t *testing.T) {
	h, rt, mode := lifecycleService(t)
	ctx := context.Background()
	rt.Tick(ctx)
	if _, starts, _ := process(h); starts != 0 {
		t.Fatal("started on boot in on-demand mode")
	}
	if err := rt.SetMode(ctx, "always"); err != nil {
		t.Fatal(err)
	}
	rt.Tick(ctx)
	if !h.store.running() || *mode != "always" {
		t.Fatal("always mode not running")
	}
	h.store.setVersion("") // backend restart / process exit in always-on mode
	rt.Tick(ctx)
	if _, starts, _ := process(h); starts != 2 {
		t.Fatalf("starts = %d", starts)
	}
	if err := rt.SetMode(ctx, "on_demand"); err != nil {
		t.Fatal(err)
	}
	rt.Tick(ctx)
	if h.store.running() {
		t.Fatal("still running in on-demand mode")
	}
}

// test_sub_store_runtime.py::test_external_server_is_never_managed
func TestExternalServerIsNeverManaged(t *testing.T) {
	h, rt, _ := lifecycleService(t)
	ctx := context.Background()
	os.Remove(filepath.Join(h.builtin, SubStoreAsset))
	rt.Use(ctx, func(ctx context.Context) error { return nil })
	rt.Tick(ctx)
	if _, starts, stops := process(h); starts != 0 || stops != 0 {
		t.Fatal("external Sub-Store managed")
	}
	if must[*RuntimeStatus](t)(rt.Snapshot(ctx, true)).Mode != "external" {
		t.Fatal("mode is not external")
	}
	if err := rt.SetMode(ctx, "always"); !IsRuntimeUnavailable(err) || err.Error() != "外部 Sub-Store 的运行模式由其部署环境管理" {
		t.Fatalf("err = %v", err)
	}
}

// test_sub_store_runtime.py::test_lock_protects_work_in_another_process
func TestRuntimeCopiesShareWorkLease(t *testing.T) {
	h, rt, _ := lifecycleService(t)
	other := NewRuntime(rt.env, rt.Root(), 0)
	other.ready = func(context.Context) (bool, error) { return true, nil }
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	done := make(chan error, 1)
	go func() {
		done <- other.Use(context.Background(), func(context.Context) error { close(entered); <-release; return nil })
	}()
	if !waitFor(t, entered, 5*time.Second) {
		t.Fatal("conversion did not enter")
	}
	if err := rt.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, stops := h.commands.counts(); stops != 0 {
		t.Fatal("stopped while another runtime works")
	}
	if err := rt.tryWork(context.Background(), func(context.Context) error { t.Error("exclusive admission during conversion"); return nil }); !IsRuntimeUnavailable(err) {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// test_sub_store_runtime.py::test_mode_is_read_from_the_settings_alone
func TestModeIsReadFromTheSettingsAlone(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = append(system.Agents, model.Agent{
			ID: "agent", Name: "Agent", Token: "agent-token", ProfileID: "default",
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	setMode := func(value string) {
		err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
			system.SystemConfig.SubStoreMode = new(value)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if mode := must[string](t)(GetMode(ctx)); mode != "on_demand" {
		t.Fatalf("mode %q", mode)
	}
	setMode("always")
	if mode := must[string](t)(GetMode(ctx)); mode != "always" {
		t.Fatalf("mode %q", mode)
	}
	setMode("bogus")
	if mode := must[string](t)(GetMode(ctx)); mode != "on_demand" {
		t.Fatalf("mode %q", mode)
	}
	// The settings are a copy: editing them does not reach the stored document.
	settings := must[*model.System](t)(r.System(ctx))
	settings.SystemConfig.SubStoreMode = new("always")
	if got := must[*model.System](t)(r.System(ctx)).SystemConfig.SubStoreMode; got == nil || *got != "bogus" {
		t.Fatal("settings are not a copy")
	}
}

// saveMode (the real _save_mode) changes only system_config.sub_store_mode.
func TestSaveModeChangesOnlyTheMode(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(repo.WithRepository(context.Background(), r))
	before := must[*model.System](t)(r.System(ctx))
	if err := saveMode(ctx, "always"); err != nil {
		t.Fatal(err)
	}
	after := must[*model.System](t)(r.System(ctx))
	if after.SystemConfig.SubStoreMode == nil || *after.SystemConfig.SubStoreMode != "always" {
		t.Fatal("mode not saved")
	}
	if !testjson.Equal(before.Agents, after.Agents) || !testjson.Equal(before.Backup, after.Backup) {
		t.Fatal("other sections changed")
	}
	expectedCfg := before.SystemConfig
	expectedCfg.SubStoreMode = new("always")
	if !testjson.Equal(expectedCfg, after.SystemConfig) {
		t.Fatalf("other system_config fields changed:\n%s", testjson.Diff(expectedCfg, after.SystemConfig))
	}
}

// The state file is json.dumps of the merged state (Python's _write).
func TestStateFileBytes(t *testing.T) {
	_, rt, _ := lifecycleService(t)
	rt.withControl(context.Background(), func(ctx context.Context) error {
		running, empty := "running", ""
		rt.writeState(ctx, runtimePatch{State: &running, Error: &empty})
		sleeping, ts := "sleeping", 12.5
		return rt.writeState(ctx, runtimePatch{LastUsed: &ts, State: &sleeping})
	})
	data, _ := os.ReadFile(filepath.Join(rt.Root(), "state.json"))
	if string(data) != `{"state":"sleeping","error":"","last_used":12.5}` {
		t.Fatalf("state.json %s", data)
	}
	for _, name := range []string{"work.lock", "control.lock"} {
		if _, err := os.Stat(filepath.Join(rt.Root(), name)); !os.IsNotExist(err) {
			t.Fatal("created legacy lock", err)
		}
	}
}

// The default root is <tempdir>/configflow-sub-store-<sha256(abspath(DATA_DIR))[:12]>.
func TestDefaultRoot(t *testing.T) {
	env := &Env{DataDir: func() string { return "/data" }}
	rt := NewRuntime(env, "", IdleSeconds)
	want := filepath.Join(runtimeTempRoot(), "configflow-sub-store-"+sha12("/data"))
	if rt.Root() != want {
		t.Fatalf("root %s want %s", rt.Root(), want)
	}
}

// The supervisorctl protocol of _command.
func TestCommandProtocol(t *testing.T) {
	h, rt, _ := lifecycleService(t)
	ctx := context.Background()
	if status := must[string](t)(rt.runCommand(ctx, "status")); status != "STOPPED" {
		t.Fatalf("status %q", status)
	}
	if out := must[string](t)(rt.runCommand(ctx, "start")); out != "sub-store: started\n" {
		t.Fatalf("start %q", out)
	}
	want := []string{"supervisorctl", "-c", "/etc/supervisor/conf.d/supervisord.conf", "start", "sub-store"}
	if got := h.commands.calls[len(h.commands.calls)-1]; !slices.Equal(got, want) {
		t.Fatalf("argv %v", got)
	}
	h.commands.fail = os.ErrNotExist
	if _, err := rt.runCommand(ctx, "status"); err == nil || err.Error() != "无法管理内置 Sub-Store，请检查容器服务状态" {
		t.Fatalf("err %v", err)
	}
	h.commands.fail = context.DeadlineExceeded
	if _, err := rt.runCommand(ctx, "stop"); err == nil || err.Error() != "无法管理内置 Sub-Store，请检查容器服务状态" {
		t.Fatalf("err %v", err)
	}
	h.commands.fail = nil
	rt.env.Commands = commandFunc(func(argv []string) (CommandResult, error) {
		return CommandResult{Stdout: "garbage\n", ReturnCode: 1}, nil
	})
	for _, action := range []string{"status", "start"} {
		if _, err := rt.runCommand(ctx, action); err == nil || err.Error() != "Sub-Store 服务操作失败，请检查容器日志" {
			t.Fatalf("%s err %v", action, err)
		}
	}
}

type commandFunc func(argv []string) (CommandResult, error)

func (f commandFunc) Run(_ context.Context, argv []string, _ time.Duration) (CommandResult, error) {
	return f(argv)
}

// A Sub-Store that never becomes ready fails after 20 s with Python's message.
func TestStartTimesOutWhenNeverReady(t *testing.T) {
	h := newHarness(t, harnessOptions{builtin: true})
	rt := h.service.Runtime
	h.commands.runVersion = ""
	h.store.setVersion("")
	started := rt.scheduler(context.Background()).Now()
	err := rt.Use(context.Background(), func(ctx context.Context) error { t.Error("work ran"); return nil })
	if !IsRuntimeUnavailable(err) || err.Error() != "Sub-Store 启动后未就绪，请稍后重试或检查容器日志" {
		t.Fatalf("err %v", err)
	}
	state := stateOf(rt)
	if state == nil || state.State == nil || *state.State != "failed" || state.Error != err.Error() {
		t.Fatalf("state %+v", state)
	}
	if elapsed := rt.scheduler(context.Background()).Now().Sub(started); elapsed != startupWait {
		t.Fatal("did not wait 20 s")
	}
}

func sha12(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
