package substore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"configflow/internal/clock"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/repo"
)

// IdleSeconds is IDLE_SECONDS: an on-demand Sub-Store stops after five idle
// minutes.
const IdleSeconds = 300

var runtimeLog = logx.Get("backend.utils.sub_store_runtime")

// RuntimeUnavailable is sub_store_runtime.RuntimeUnavailable (a
// RuntimeError): the message is shown to the user.
type RuntimeUnavailable struct{ Message string }

func (e *RuntimeUnavailable) Error() string       { return e.Message }
func (e *RuntimeUnavailable) PythonClass() string { return "RuntimeUnavailable" }

func unavailable(message string) error { return &RuntimeUnavailable{Message: message} }

// IsRuntimeUnavailable reports whether err is a RuntimeUnavailable.
func IsRuntimeUnavailable(err error) bool {
	var u *RuntimeUnavailable
	return errors.As(err, &u)
}

// GetMode is sub_store_runtime.get_mode(): system_config.sub_store_mode,
// read from the settings alone ("on_demand" unless it is "always").
func GetMode(ctx context.Context) (string, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	settings, err := r.GetSystemSettings(ctx)
	if err != nil {
		return "", err
	}
	if mode, ok := settings.Value("sub_store_mode").(string); ok && (mode == "on_demand" || mode == "always") {
		return mode, nil
	}
	return "on_demand", nil
}

// saveMode is _save_mode: one system transaction setting sub_store_mode.
func saveMode(ctx context.Context, mode string) error {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	_, err = r.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		value, present := system.Get("system_config")
		if !present {
			return nil, pyErr("KeyError", "'system_config'")
		}
		settings, ok := value.(*jsonx.Object)
		if !ok {
			return nil, attributeError(value, "update")
		}
		settings.Set("sub_store_mode", mode)
		return nil, nil
	})
	if err != nil {
		return err
	}
	// reset_config_context() runs only after a successful transaction.
	repo.ResetConfigContext(ctx)
	return nil
}

// Runtime is SubStoreRuntime: the lifecycle of the bundled Sub-Store.
// Conversions hold a shared in-process work lease through subscription cleanup;
// stopping/updating take it exclusively. A second control lease serializes
// state/startup. Runtime copies of one physical root share both locks.
// state.json keeps Python's location; legacy lock files remain untouched. External Sub-Store instances are never
// started or stopped.
type Runtime struct {
	env         *Env
	Directory   string  // "" = the default root
	IdleSeconds float64 // IDLE_SECONDS

	// Replaceable steps (tests): get_mode, _save_mode, _ready, _command.
	getMode  func(ctx context.Context) (string, error)
	saveMode func(ctx context.Context, mode string) error
	ready    func(ctx context.Context) (bool, error)
	command  func(ctx context.Context, action string) (string, error)
	// clock preserves the owner test clock seam; production uses ClockFrom(ctx).
	clock clock.Clock
}

// NewRuntime returns a runtime on env (directory "" = default root).
func NewRuntime(env *Env, directory string, idleSeconds float64) *Runtime {
	return &Runtime{env: env, Directory: directory, IdleSeconds: idleSeconds}
}

var (
	tempDirOnce sync.Once
	tempDir     string
)

// gettempdir is tempfile.gettempdir(): the first usable of $TMPDIR, $TEMP,
// $TMP, /tmp, /var/tmp, /usr/tmp, the working directory (absolute).
func gettempdir() string {
	tempDirOnce.Do(func() {
		var candidates []string
		for _, key := range []string{"TMPDIR", "TEMP", "TMP"} {
			if value := os.Getenv(key); value != "" {
				candidates = append(candidates, value)
			}
		}
		candidates = append(candidates, "/tmp", "/var/tmp", "/usr/tmp")
		if wd, err := os.Getwd(); err == nil {
			candidates = append(candidates, wd)
		} else {
			candidates = append(candidates, ".")
		}
		for _, dir := range candidates {
			if dir != "." {
				if abs, err := filepath.Abs(dir); err == nil {
					dir = abs
				}
			}
			f, err := os.CreateTemp(dir, "tmp")
			if err != nil {
				continue
			}
			name := f.Name()
			_, werr := f.Write([]byte("blat"))
			f.Close()
			os.Remove(name)
			if werr == nil {
				tempDir = dir
				return
			}
		}
		tempDir = os.TempDir()
	})
	return tempDir
}

// Root is the runtime's directory: Directory, or
// <tempdir>/configflow-sub-store-<sha256(abspath(DATA_DIR))[:12]>.
func (r *Runtime) Root() string {
	if r.Directory != "" {
		return r.Directory
	}
	abs, err := filepath.Abs(r.env.DataDir())
	if err != nil {
		abs = r.env.DataDir()
	}
	sum := sha256.Sum256([]byte(abs))
	return pyJoin(gettempdir(), "configflow-sub-store-"+hex.EncodeToString(sum[:])[:12])
}

const (
	workWait        = 45 * time.Second
	maintenanceWait = 90 * time.Second
	controlWait     = 45 * time.Second
	startupWait     = 20 * time.Second
	startupPoll     = 200 * time.Millisecond
)

type runtimeLeases struct {
	work    *lockx.RWMutex
	control *lockx.Mutex
	refs    int
}

var leaseOwners = struct {
	sync.Mutex
	entries map[string]*runtimeLeases
}{entries: make(map[string]*runtimeLeases)}

func (r *Runtime) pinLeases() (string, *runtimeLeases, func(), error) {
	root, err := repo.ResolveDataDir(r.Root())
	if err != nil {
		return "", nil, nil, err
	}
	leaseOwners.Lock()
	leases := leaseOwners.entries[root]
	if leases == nil {
		leases = &runtimeLeases{work: lockx.NewRWMutex(lockx.LevelDevice, "sub-store-work"), control: lockx.NewMutex(lockx.LevelDeviceControl, "sub-store-control")}
		leaseOwners.entries[root] = leases
	}
	leases.refs++
	leaseOwners.Unlock()
	return root, leases, func() {
		leaseOwners.Lock()
		leases.refs--
		if leases.refs == 0 {
			delete(leaseOwners.entries, root)
		}
		leaseOwners.Unlock()
	}, nil
}

type workLeaseKey struct{}
type workLease struct {
	root      string
	exclusive bool
	active    atomic.Bool
}

func (r *Runtime) heldWork(ctx context.Context) *workLease {
	lease, _ := ctx.Value(workLeaseKey{}).(*workLease)
	if lease == nil || !lease.active.Load() {
		return nil
	}
	root, err := repo.ResolveDataDir(r.Root())
	if err != nil || lease.root != root {
		return nil
	}
	return lease
}

func runtimeLockError(err error) error {
	if errors.Is(err, lockx.ErrTimeout) || errors.Is(err, lockx.ErrBusy) {
		return unavailable(msgRuntimeBusy)
	}
	return err
}

func (r *Runtime) withWork(ctx context.Context, exclusive bool, limit time.Duration, fn func(context.Context) error) error {
	if lease := r.heldWork(ctx); lease != nil {
		if exclusive && !lease.exclusive {
			return unavailable(msgRuntimeBusy)
		}
		return fn(ctx)
	}
	root, leases, unpin, err := r.pinLeases()
	if err != nil {
		return err
	}
	defer unpin()
	var held context.Context
	var release func()
	if exclusive {
		held, release, err = leases.work.Lock(ctx, limit)
	} else {
		held, release, err = leases.work.RLock(ctx, limit)
	}
	if err != nil {
		return runtimeLockError(err)
	}
	defer release()
	return r.runWork(held, root, exclusive, fn)
}

func (r *Runtime) tryWork(ctx context.Context, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, leases, unpin, err := r.pinLeases()
	if err != nil {
		return err
	}
	defer unpin()
	held, release, ok := leases.work.TryLock(ctx)
	if !ok {
		return unavailable(msgRuntimeBusy)
	}
	defer release()
	return r.runWork(held, root, true, fn)
}

func (r *Runtime) runWork(ctx context.Context, root string, exclusive bool, fn func(context.Context) error) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return osError(err, root)
	}
	lease := &workLease{root: root, exclusive: exclusive}
	lease.active.Store(true)
	defer lease.active.Store(false)
	return fn(context.WithValue(ctx, workLeaseKey{}, lease))
}

func (r *Runtime) withControl(ctx context.Context, fn func(context.Context) error) error {
	root, leases, unpin, err := r.pinLeases()
	if err != nil {
		return err
	}
	defer unpin()
	held, release, err := leases.control.Lock(ctx, controlWait)
	if err != nil {
		return runtimeLockError(err)
	}
	defer release()
	if err := os.MkdirAll(root, 0700); err != nil {
		return osError(err, root)
	}
	return fn(held)
}

func (r *Runtime) scheduler(ctx context.Context) clock.Clock {
	if r.clock != nil {
		return r.clock
	}
	return lifecycle.ClockFrom(ctx)
}

func (r *Runtime) wait(ctx context.Context, d time.Duration) error {
	timer := r.scheduler(ctx).NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C():
		return nil
	}
}

// read is _read: the JSON value of state.json, or an empty state when it
// cannot be read or is not JSON (OSError, ValueError). A value that is not
// an object is returned as it is: Python then fails where it uses it
// (TypeError in _write, AttributeError at state.get), and so does Go.
func (r *Runtime) read() any {
	data, err := os.ReadFile(pyJoin(r.Root(), "state.json"))
	if err != nil || utf8DecodeError(data) != "" {
		return jsonx.NewObject()
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return jsonx.NewObject()
	}
	return value
}

// stateGet is state.get(key, fallback) on what read returned.
func stateGet(state any, key string, fallback any) (any, error) {
	object, ok := state.(*jsonx.Object)
	if !ok {
		return nil, attributeError(state, "get")
	}
	return object.GetOr(key, fallback), nil
}

// write is _write(**changes): merge and replace state.json atomically
// (callers hold the control lock). Pairs are key, value.
func (r *Runtime) write(pairs ...any) error {
	state, ok := r.read().(*jsonx.Object)
	if !ok {
		return pyErr("TypeError", fmt.Sprintf("'%s' object is not a mapping", typeName(r.read())))
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		state.Set(pairs[i].(string), pairs[i+1])
	}
	encoded, err := jsonx.Default(state)
	if err != nil {
		return err
	}
	temp := pyJoin(r.Root(), "state.tmp")
	if err := os.WriteFile(temp, encoded, 0o666); err != nil {
		return osError(err, temp)
	}
	target := pyJoin(r.Root(), "state.json")
	if err := os.Rename(temp, target); err != nil {
		return osError(err, temp)
	}
	return nil
}

func (r *Runtime) monotonic() any { return jsonx.Float(r.env.Clock.Monotonic()) }

// runCommand is _command(action): supervisorctl for the sub-store program.
// status returns the state word; other actions their output.
func (r *Runtime) runCommand(ctx context.Context, action string) (string, error) {
	if r.command != nil {
		return r.command(ctx, action)
	}
	result, err := r.env.Commands.Run(ctx, []string{"supervisorctl", "-c", SupervisorConfig, action, SubStoreProgram},
		20*time.Second)
	if err != nil {
		var py *PyError
		if errors.As(err, &py) && (py.Class == "TimeoutExpired" ||
			(strings.HasSuffix(py.Class, "Error") && py.Class != "UnicodeDecodeError")) {
			return "", unavailable("无法管理内置 Sub-Store，请检查容器服务状态")
		}
		return "", err
	}
	if action == "status" {
		parts := pyFields(result.Stdout)
		if len(parts) >= 2 && parts[0] == SubStoreProgram {
			return parts[1], nil
		}
	} else if result.ReturnCode == 0 {
		return result.Stdout, nil
	}
	return "", unavailable("Sub-Store 服务操作失败，请检查容器日志")
}

// isReady is _ready(): the bundled Sub-Store answers its version.
func (r *Runtime) isReady(ctx context.Context) (bool, error) {
	if r.ready != nil {
		return r.ready(ctx)
	}
	base, err := r.env.BuiltinSubStoreURL()
	if err != nil {
		return false, err
	}
	return r.env.FetchRunningVersion(ctx, base, 1) != "", nil
}

func (r *Runtime) mode(ctx context.Context) (string, error) {
	if r.getMode != nil {
		return r.getMode(ctx)
	}
	return GetMode(ctx)
}

// ensureRunning is _ensure_running (caller holds the control lock).
func (r *Runtime) ensureRunning(ctx context.Context) error {
	ready, err := r.isReady(ctx)
	if err != nil {
		return err
	}
	if ready {
		return r.write("state", "running", "error", "")
	}
	if err := r.write("state", "starting", "error", ""); err != nil {
		return err
	}
	err = func() error {
		status, err := r.runCommand(ctx, "status")
		if err != nil {
			return err
		}
		if status != "RUNNING" && status != "STARTING" {
			if _, err := r.runCommand(ctx, "start"); err != nil {
				return err
			}
		}
		clock := r.scheduler(ctx)
		deadline := clock.Now().Add(startupWait)
		for clock.Now().Before(deadline) {
			ready, err := r.isReady(ctx)
			if err != nil {
				return err
			}
			if ready {
				return r.write("state", "running", "error", "")
			}
			if err := r.wait(ctx, min(startupPoll, deadline.Sub(clock.Now()))); err != nil {
				return err
			}
		}
		return unavailable("Sub-Store 启动后未就绪，请稍后重试或检查容器日志")
	}()
	var u *RuntimeUnavailable
	if errors.As(err, &u) {
		if werr := r.write("state", "failed", "error", u.Message); werr != nil {
			return werr
		}
	}
	return err
}

// Use is the use() context manager: fn runs while the bundled Sub-Store is
// running and cannot be stopped; idle time counts from fn's completion
// (also after a failure). Without the bundle fn just runs.
func (r *Runtime) Use(ctx context.Context, fn func(context.Context) error) error {
	ctx = lifecycle.Detach(ctx)
	if lease := r.heldWork(ctx); lease != nil {
		return fn(ctx)
	}
	if !r.env.IsBuiltinAvailable() {
		return fn(ctx)
	}
	return r.withWork(ctx, false, workWait, func(ctx context.Context) error {
		if err := r.withControl(ctx, func(ctx context.Context) error { return r.ensureRunning(ctx) }); err != nil {
			return err
		}
		return func() (err error) {
			defer func() {
				if ferr := r.withControl(ctx, func(ctx context.Context) error { return r.write("last_used", r.monotonic()) }); ferr != nil {
					err = ferr
				}
			}()
			return fn(ctx)
		}()
	})
}

// Maintenance is maintenance(): fn (an update) runs with the work lock held
// exclusively; afterwards the state says whether Sub-Store answers.
func (r *Runtime) Maintenance(ctx context.Context, fn func(context.Context) error) error {
	ctx = lifecycle.Detach(ctx)
	return r.withWork(ctx, true, maintenanceWait, func(ctx context.Context) error {
		if err := r.withControl(ctx, func(ctx context.Context) error { return r.write("state", "updating", "error", "") }); err != nil {
			return err
		}
		return func() (err error) {
			defer func() {
				ferr := r.withControl(ctx, func(ctx context.Context) error {
					ready, err := r.isReady(ctx)
					if err != nil {
						return err
					}
					state, message := "failed", "Sub-Store 更新后无法连接，请检查容器日志"
					if ready {
						state, message = "running", ""
					}
					return r.write("state", state, "error", message, "last_used", r.monotonic())
				})
				if ferr != nil {
					err = ferr
				}
			}()
			return fn(ctx)
		}()
	})
}

// SetMode is set_mode(mode): "always" starts Sub-Store now; the mode is
// saved in system_config.
func (r *Runtime) SetMode(ctx context.Context, mode string) error {
	ctx = lifecycle.Detach(ctx)
	if mode != "on_demand" && mode != "always" {
		return pyErr("ValueError", "运行模式必须是 on_demand 或 always")
	}
	if !r.env.IsBuiltinAvailable() {
		return unavailable("外部 Sub-Store 的运行模式由其部署环境管理")
	}
	return r.tryWork(ctx, func(ctx context.Context) error {
		return r.withControl(ctx, func(ctx context.Context) error {
			if mode == "always" {
				if err := r.ensureRunning(ctx); err != nil {
					return err
				}
			}
			save := r.saveMode
			if save == nil {
				save = saveMode
			}
			if err := save(ctx, mode); err != nil {
				return err
			}
			return r.write("last_used", r.monotonic())
		})
	})
}

// Tick is tick(): the non-blocking idle sweep. Busy (conversions, updates)
// is normal; other RuntimeUnavailable failures are logged as warnings.
// Errors that are not RuntimeUnavailable are returned (the monitor logs
// them).
func (r *Runtime) Tick(ctx context.Context) error {
	ctx = lifecycle.Detach(ctx)
	if !r.env.IsBuiltinAvailable() {
		return nil
	}
	err := r.tryWork(ctx, func(ctx context.Context) error {
		return r.withControl(ctx, func(ctx context.Context) error {
			mode, err := r.mode(ctx)
			if err != nil {
				return err
			}
			if mode == "always" {
				return r.ensureRunning(ctx)
			}
			state := r.read()
			now := r.env.Clock.Monotonic()
			if !jsonx.Truthy(state) {
				// Also handles upgrades from the former autostart image.
				status, err := r.runCommand(ctx, "status")
				if err != nil {
					return err
				}
				current := "sleeping"
				if status == "RUNNING" || status == "STARTING" {
					current = "running"
				}
				return r.write("state", current, "last_used", jsonx.Float(now))
			}
			current, err := stateGet(state, "state", nil)
			if err != nil {
				return err
			}
			if jsonx.Equal(current, "sleeping") {
				return nil
			}
			used, err := stateGet(state, "last_used", jsonx.Int(0))
			if err != nil {
				return err
			}
			elapsed, err := floatMinus(now, used)
			if err != nil {
				return err
			}
			if elapsed >= r.IdleSeconds {
				status, err := r.runCommand(ctx, "status")
				if err != nil {
					return err
				}
				if status != "STOPPED" && status != "EXITED" && status != "FATAL" {
					if _, err := r.runCommand(ctx, "stop"); err != nil {
						return err
					}
				}
				// Retain a startup/update failure for the UI and retry.
				current := "sleeping"
				if failure, _ := stateGet(state, "error", nil); jsonx.Truthy(failure) {
					current = "failed"
				}
				return r.write("state", current)
			}
			return nil
		})
	})
	var u *RuntimeUnavailable
	if errors.As(err, &u) {
		if !strings.Contains(u.Message, "正忙") {
			runtimeLog.Warning(u.Message)
		}
		return nil
	}
	return err
}

// floatMinus is `now - value` for a float now and a JSON value: numbers
// (and bools) subtract, a huge int is OverflowError, anything else
// TypeError, as in Python.
func floatMinus(now float64, value any) (float64, error) {
	switch v := value.(type) {
	case bool:
		if v {
			return now - 1, nil
		}
		return now, nil
	case jsonx.Number:
		f := v.Float64()
		if v.IsInt() && math.IsInf(f, 0) {
			return 0, pyErr("OverflowError", "int too large to convert to float")
		}
		return now - f, nil
	}
	return 0, pyErr("TypeError", fmt.Sprintf("unsupported operand type(s) for -: 'float' and '%s'", typeName(value)))
}

// Status is status(running): what the dependency page shows. Observing
// never starts Sub-Store or extends its idle time.
func (r *Runtime) Status(ctx context.Context, running bool) (*jsonx.Object, error) {
	managed := r.env.IsBuiltinAvailable()
	var state any = jsonx.NewObject()
	if managed {
		state = r.read()
	}
	current, err := stateGet(state, "state", nil)
	if err != nil {
		return nil, err
	}
	if !jsonx.Equal(current, "updating") {
		if running {
			current = "running"
		} else if !jsonx.Equal(current, "starting") && !jsonx.Equal(current, "failed") {
			sleeping := false
			if managed {
				mode, err := r.mode(ctx)
				if err != nil {
					return nil, err
				}
				sleeping = mode == "on_demand" && (current == nil || jsonx.Equal(current, "sleeping"))
			}
			if sleeping {
				current = "sleeping"
			} else {
				current = "failed"
			}
		}
	}
	mode := "external"
	if managed {
		var err error
		if mode, err = r.mode(ctx); err != nil {
			return nil, err
		}
	}
	var idle any = jsonx.Float(r.IdleSeconds)
	if r.IdleSeconds == math.Trunc(r.IdleSeconds) {
		idle = jsonx.Int(int64(r.IdleSeconds))
	}
	var errorText any = ""
	if jsonx.Equal(current, "failed") {
		errorText, _ = stateGet(state, "error", "")
	}
	return jsonx.Obj("managed", managed, "mode", mode, "state", current, "idle_seconds", idle, "error", errorText), nil
}

// idlePeriodic only drives Tick; work/control leases keep their existing
// implementation until O3-6. Tick's persisted clock stays kernel-monotonic.
func (r *Runtime) idlePeriodic() lifecycle.Periodic {
	return lifecycle.Periodic{
		Name: "sub-store-idle", Start: lifecycle.AtBoot, FirstDelay: 0,
		Interval: func(context.Context) time.Duration { return 5 * time.Second },
		Round:    func(ctx context.Context) error { return r.Tick(ctx) },
	}
}
