package substore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"configflow/internal/clock"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// IdleSeconds is IDLE_SECONDS: an on-demand Sub-Store stops after five idle
// minutes.
const IdleSeconds = 300

var runtimeLog = logx.Get("backend.utils.sub_store_runtime")

// RuntimeUnavailable is sub_store_runtime.RuntimeUnavailable (a
// RuntimeError): the message is shown to the user.
type RuntimeUnavailable struct{ Message string }

func (e *RuntimeUnavailable) Error() string { return e.Message }

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
	mode := "on_demand"
	err = r.View(ctx, func(document *model.Document) error {
		value := document.System.SystemConfig.SubStoreMode
		if value != nil && (*value == "on_demand" || *value == "always") {
			mode = *value
		}
		return nil
	})
	return mode, err
}

func saveMode(ctx context.Context, mode string) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	if err := r.UpdateSystem(ctx, func(ctx context.Context, system *model.System) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		system.SystemConfig.SubStoreMode = new(mode)
		return nil
	}); err != nil {
		return err
	}
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

// runtimeTempRoot retains explicit legacy temporary-directory settings so a
// valid existing state file stays at the same location. Selection is read-only:
// an unusable configured directory fails at the actual write, without probing
// files or silently starting a second runtime under a different directory.
func runtimeTempRoot() string { return selectedRuntimeTempDirectory().path }

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
	return filepath.Join(runtimeTempRoot(), "configflow-sub-store-"+hex.EncodeToString(sum[:])[:12])
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
	if err := r.checkTempDirectory(); err != nil {
		return "", nil, nil, err
	}
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
		if err := ctx.Err(); err != nil {
			return err
		}
		return unavailable(msgRuntimeBusy)
	}
	defer release()
	return r.runWork(held, root, true, fn)
}

func (r *Runtime) runWork(ctx context.Context, root string, exclusive bool, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return r.directoryFailure(osError(err, root))
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
	if err := held.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return r.directoryFailure(osError(err, root))
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

func (r *Runtime) monotonic() float64 { return r.env.Clock.Monotonic() }

// runCommand is _command(action): status returns the state word, start and
// stop control the supervised process.
func (r *Runtime) runCommand(ctx context.Context, action string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r.command != nil {
		return r.command(ctx, action)
	}
	process := r.env.Process
	if process == nil {
		return "", unavailable(msgManageUnavailable)
	}
	var err error
	switch action {
	case "status":
		return string(process.Status()), nil
	case "start":
		err = process.Start(ctx)
	case "stop":
		err = process.Stop(ctx)
	default:
		return "", fmt.Errorf("unknown Sub-Store action %q", action)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(err, ErrProcessUnavailable) {
		return "", unavailable(msgManageUnavailable)
	}
	if err != nil {
		runtimeLog.Warningf(msgProcessActionFailed, action, err)
		return "", unavailable(msgProcessFailed)
	}
	return "", nil
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
		return r.writeState(ctx, runtimePatch{State: new("running"), Error: new("")})
	}
	if err := r.writeState(ctx, runtimePatch{State: new("starting"), Error: new("")}); err != nil {
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
				return r.writeState(ctx, runtimePatch{State: new("running"), Error: new("")})
			}
			if err := r.wait(ctx, min(startupPoll, deadline.Sub(clock.Now()))); err != nil {
				return err
			}
		}
		return unavailable(msgStartNotReady)
	}()
	var u *RuntimeUnavailable
	if errors.As(err, &u) {
		if werr := r.writeState(ctx, runtimePatch{State: new("failed"), Error: new(u.Message)}); werr != nil {
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
	if err := ctx.Err(); err != nil {
		return err
	}
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
				if ferr := r.withControl(ctx, func(ctx context.Context) error { return r.writeState(ctx, runtimePatch{LastUsed: new(r.monotonic())}) }); ferr != nil {
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
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.withWork(ctx, true, maintenanceWait, func(ctx context.Context) error {
		if err := r.withControl(ctx, func(ctx context.Context) error {
			return r.writeState(ctx, runtimePatch{State: new("updating"), Error: new("")})
		}); err != nil {
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
					return r.writeState(ctx, runtimePatch{State: new(state), Error: new(message), LastUsed: new(r.monotonic())})
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if mode != "on_demand" && mode != "always" {
		return errors.New(msgRuntimeModeInvalid)
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
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := save(ctx, mode); err != nil {
				return err
			}
			return r.writeState(ctx, runtimePatch{LastUsed: new(r.monotonic())})
		})
	})
}

// Tick is tick(): the non-blocking idle sweep. Busy (conversions, updates)
// is normal; other RuntimeUnavailable failures are logged as warnings.
// Errors that are not RuntimeUnavailable are returned (the monitor logs
// them).
func (r *Runtime) Tick(ctx context.Context) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
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
			state, err := r.loadState()
			if err != nil {
				return err
			}
			now := r.env.Clock.Monotonic()
			if state.empty() {
				status, err := r.runCommand(ctx, "status")
				if err != nil {
					return err
				}
				current := "sleeping"
				if status == "RUNNING" || status == "STARTING" {
					current = "running"
				}
				return r.writeState(ctx, runtimePatch{State: new(current), LastUsed: new(now)})
			}
			if state.State != nil && *state.State == "sleeping" {
				return nil
			}
			if state.invalidLastUsed {
				return &RuntimeStateError{Field: "last_used"}
			}
			lastUsed := 0.0
			if state.LastUsed != nil {
				lastUsed = *state.LastUsed
			}
			if now-lastUsed >= r.IdleSeconds {
				status, err := r.runCommand(ctx, "status")
				if err != nil {
					return err
				}
				if status != "STOPPED" && status != "EXITED" && status != "FATAL" {
					if _, err := r.runCommand(ctx, "stop"); err != nil {
						return err
					}
				}
				current := "sleeping"
				if state.errorText() != "" {
					current = "failed"
				}
				return r.writeState(ctx, runtimePatch{State: new(current)})
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

// Snapshot observes lifecycle state without starting or touching the service.
func (r *Runtime) Snapshot(ctx context.Context, running bool) (*RuntimeStatus, error) {
	managed := r.env.IsBuiltinAvailable()
	state := &runtimeState{}
	if managed {
		var err error
		state, err = r.loadState()
		if err != nil {
			return nil, err
		}
	}
	current := ""
	if state.State != nil {
		current = *state.State
	}
	if current != "updating" {
		if running {
			current = "running"
		} else if current != "starting" && current != "failed" {
			sleeping := false
			if managed {
				mode, err := r.mode(ctx)
				if err != nil {
					return nil, err
				}
				sleeping = mode == "on_demand" && (state.State == nil || current == "sleeping")
			}
			current = "failed"
			if sleeping {
				current = "sleeping"
			}
		}
	}
	mode := "external"
	if managed {
		var err error
		mode, err = r.mode(ctx)
		if err != nil {
			return nil, err
		}
	}
	message := ""
	if current == "failed" {
		message = state.errorText()
	}
	return &RuntimeStatus{Managed: managed, Mode: mode, State: current, IdleSeconds: r.IdleSeconds, Error: message}, nil
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
