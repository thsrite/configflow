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
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
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
		return rt.Use(ctx, func() error {
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
	rt.Use(ctx, func() error {
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
	if status := must[*jsonx.Object](t)(rt.Status(ctx, false)); status.GetString("state") != "sleeping" {
		t.Fatalf("status %s", jsonx.Repr(status))
	}
	if _, starts, _ := process(h); starts != 0 {
		t.Fatal("polling started Sub-Store")
	}
	rt.Use(ctx, func() error { return nil })
	completed := stateOf(rt).Value("last_used")
	for i := 0; i < 5; i++ {
		if must[*jsonx.Object](t)(rt.Status(ctx, true)).GetString("state") != "running" {
			t.Fatal("not running")
		}
	}
	if !jsonx.Equal(stateOf(rt).Value("last_used"), completed) {
		t.Fatal("polling extended the idle time")
	}
	rt.Tick(ctx)
	if h.store.running() {
		t.Fatal("still running")
	}
	if must[*jsonx.Object](t)(rt.Status(ctx, false)).GetString("state") != "sleeping" {
		t.Fatal("not sleeping")
	}
}

// test_sub_store_runtime.py::test_idle_time_begins_after_failed_task_finishes
func TestIdleTimeBeginsAfterFailedTaskFinishes(t *testing.T) {
	h, rt, _ := lifecycleService(t)
	ctx := context.Background()
	rt.IdleSeconds = 300
	failure := errors.New("conversion failed")
	err := rt.Use(ctx, func() error {
		rt.lock("control", true, 45, func() error { return rt.write("last_used", jsonx.Float(SystemClock.Monotonic()-400)) })
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
	rt.Use(ctx, func() error {
		go func() {
			updateDone <- rt.Maintenance(ctx, func() error {
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
	if must[*jsonx.Object](t)(rt.Status(ctx, true)).GetString("state") != "updating" {
		t.Fatal("state is not updating")
	}
	go func() { convertDone <- rt.Use(ctx, func() error { close(converted); return nil }) }()
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
	err := rt.Use(ctx, func() error { ran = true; return nil })
	if !IsRuntimeUnavailable(err) || ran {
		t.Fatalf("err=%v ran=%v (work must not run before readiness)", err, ran)
	}
	if must[*jsonx.Object](t)(rt.Status(ctx, false)).GetString("state") != "failed" {
		t.Fatal("not failed")
	}
	rt.command = nil
	rt.Use(ctx, func() error {
		if !h.store.running() {
			t.Error("not running")
		}
		return nil
	})
	if must[*jsonx.Object](t)(rt.Status(ctx, true)).GetString("error") != "" {
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
	rt.Use(ctx, func() error { return nil })
	rt.Tick(ctx)
	if _, starts, stops := process(h); starts != 0 || stops != 0 {
		t.Fatal("external Sub-Store managed")
	}
	if must[*jsonx.Object](t)(rt.Status(ctx, true)).GetString("mode") != "external" {
		t.Fatal("mode is not external")
	}
	if err := rt.SetMode(ctx, "always"); !IsRuntimeUnavailable(err) || err.Error() != "外部 Sub-Store 的运行模式由其部署环境管理" {
		t.Fatalf("err = %v", err)
	}
}

// test_sub_store_runtime.py::test_lock_protects_work_in_another_process: a
// second process (this test binary) holds the shared work lock.
func TestLockProtectsWorkInAnotherProcess(t *testing.T) {
	h, rt, _ := lifecycleService(t)
	ctx := context.Background()
	rt.Use(ctx, func() error { return nil }) // started, idle time 0
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessHoldsWorkLock$")
	cmd.Env = append(os.Environ(), "SUBSTORE_HELPER=1", "SUBSTORE_HELPER_ROOT="+rt.Root(),
		"SUBSTORE_HELPER_BUILTIN="+h.builtin, "SUBSTORE_HELPER_DIR="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "entered")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not enter")
		}
		time.Sleep(20 * time.Millisecond)
	}
	rt.Tick(ctx)
	if _, stops := h.commands.counts(); stops != 0 {
		t.Fatal("stopped while another process works")
	}
	err := rt.lock("work", true, 0, func() error { t.Error("exclusive lock acquired while child works"); return nil })
	if !IsRuntimeUnavailable(err) {
		t.Fatalf("err = %v", err)
	}
	os.WriteFile(filepath.Join(dir, "finish"), nil, 0o644)
	err = cmd.Wait()
	finished = true
	if err != nil {
		t.Fatalf("child: %v", err)
	}
}

// TestHelperProcessHoldsWorkLock is the child of
// TestLockProtectsWorkInAnotherProcess.
func TestHelperProcessHoldsWorkLock(t *testing.T) {
	if os.Getenv("SUBSTORE_HELPER") != "1" {
		t.Skip("helper process")
	}
	builtin := os.Getenv("SUBSTORE_HELPER_BUILTIN")
	env := &Env{LookupEnv: func(key string) (string, bool) {
		if key == "SUB_STORE_BUILTIN_DIR" {
			return builtin, true
		}
		return "", false
	}, DataDir: func() string { return "." }, HTTP: RequestsClient{}, Commands: &fakeCommands{}, Clock: SystemClock}
	rt := NewRuntime(env, os.Getenv("SUBSTORE_HELPER_ROOT"), 0)
	rt.ready = func(context.Context) (bool, error) { return true, nil }
	dir := os.Getenv("SUBSTORE_HELPER_DIR")
	err := rt.Use(context.Background(), func() error {
		os.WriteFile(filepath.Join(dir, "entered"), nil, 0o644)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(dir, "finish")); err == nil {
				return nil
			}
			time.Sleep(20 * time.Millisecond)
		}
		return errors.New("not finished")
	})
	if err != nil {
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
	_, err = r.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", append(system.GetList("agents"), jsonx.Obj("id", "agent", "name", "Agent", "token", "agent-token",
			"profile_id", "default", "deployments", jsonx.Obj("d", jsonx.Obj("status", "succeeded")))))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	setMode := func(value string) {
		_, err := r.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
			system.GetObject("system_config").Set("sub_store_mode", value)
			return nil, nil
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
	settings := must[*jsonx.Object](t)(r.GetSystemSettings(ctx))
	settings.Set("sub_store_mode", "always")
	if must[*jsonx.Object](t)(r.GetSystemSettings(ctx)).GetString("sub_store_mode") != "bogus" {
		t.Fatal("settings are not a copy")
	}
	system := must[*jsonx.Object](t)(r.UpdateSystemTransaction(ctx, func(context.Context, *jsonx.Object) (*jsonx.Object, error) {
		return nil, nil
	}))
	if len(settings.Keys()) != len(system.GetObject("system_config").Keys()) {
		t.Fatal("settings keys differ from system_config")
	}
}

// saveMode (the real _save_mode) changes only system_config.sub_store_mode.
func TestSaveModeChangesOnlyTheMode(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(repo.WithRepository(context.Background(), r))
	before := must[*jsonx.Object](t)(r.GetSystem(ctx))
	if err := saveMode(ctx, "always"); err != nil {
		t.Fatal(err)
	}
	after := must[*jsonx.Object](t)(r.GetSystem(ctx))
	if after.GetObject("system_config").GetString("sub_store_mode") != "always" {
		t.Fatal("mode not saved")
	}
	if !jsonx.Equal(after.Value("agents"), before.Value("agents")) || !jsonx.Equal(after.Value("backup"), before.Value("backup")) {
		t.Fatal("other sections changed")
	}
	for key, value := range before.GetObject("system_config").All() {
		if !jsonx.Equal(after.GetObject("system_config").Value(key), value) {
			t.Fatalf("%s changed", key)
		}
	}
}

// The state file is json.dumps of the merged state (Python's _write).
func TestStateFileBytes(t *testing.T) {
	_, rt, _ := lifecycleService(t)
	rt.lock("control", true, 45, func() error {
		rt.write("state", "running", "error", "")
		return rt.write("last_used", jsonx.Float(12.5), "state", "sleeping")
	})
	data, _ := os.ReadFile(filepath.Join(rt.Root(), "state.json"))
	if string(data) != `{"state": "sleeping", "error": "", "last_used": 12.5}` {
		t.Fatalf("state.json %s", data)
	}
	for _, name := range []string{"work.lock", "control.lock"} {
		if _, err := os.Stat(filepath.Join(rt.Root(), name)); name == "control.lock" && err != nil {
			t.Fatal(err)
		}
	}
}

// The default root is <tempdir>/configflow-sub-store-<sha256(abspath(DATA_DIR))[:12]>.
func TestDefaultRoot(t *testing.T) {
	env := &Env{DataDir: func() string { return "/data" }}
	rt := NewRuntime(env, "", IdleSeconds)
	want := pyJoin(gettempdir(), "configflow-sub-store-"+sha12("/data"))
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
	if got := h.commands.calls[len(h.commands.calls)-1]; jsonx.Repr(pyArgv(got)) != jsonx.Repr(pyArgv(want)) {
		t.Fatalf("argv %v", got)
	}
	h.commands.fail = &PyError{Class: "FileNotFoundError", Message: "[Errno 2] No such file or directory: 'supervisorctl'"}
	if _, err := rt.runCommand(ctx, "status"); err == nil || err.Error() != "无法管理内置 Sub-Store，请检查容器服务状态" {
		t.Fatalf("err %v", err)
	}
	h.commands.fail = pyErr("TimeoutExpired", "timed out")
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
	h.commands.runVersion = "" // the process starts but never answers
	h.store.setVersion("")
	err := rt.Use(context.Background(), func() error { t.Error("work ran"); return nil })
	if !IsRuntimeUnavailable(err) || err.Error() != "Sub-Store 启动后未就绪，请稍后重试或检查容器日志" {
		t.Fatalf("err %v", err)
	}
	state := stateOf(rt)
	if state.GetString("state") != "failed" || state.GetString("error") != err.Error() {
		t.Fatalf("state %s", jsonx.Repr(state))
	}
	if h.clock.Monotonic() < 20 {
		t.Fatal("did not wait 20 s")
	}
}

func sha12(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
