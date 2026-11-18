# O3 — Storage concurrency, locks and background tasks

Status: design, 2026-11-17 (Opus O3). Codex implements the steps in section 4. Changes to the contract in section 2
need the designer's or coordinator's agreement. Template: WORKFLOW.md, "Design docs, hand-offs and escalation".
This design builds on `server/design/n7-n8.md` §3 (repository transactions) and does not change it: one writer at
a time under `.config.lock` plus an in-process lock; Python's lock timeout; `ErrInUse` for a nested write;
`ConfigurationCommitted` runs after the lock is released; write entry points detach cancellation; lock waits are
bounded by the lock timeout, not by ctx.

Inputs: the O6 reviews in `parity/difftest-{config,agents,routers}.md` (config X08/T27; agents X07/X08/X10/X12/X15/
T26; routers X04/X07/X10/X14/T25/T26), PORTING rules 4b and 7, and the survey below (Python on this branch, Go at
`68c86a01`).

## 0. Survey

### 0.1 Python background threads and loops

"Start" is the condition Python starts the thread under. `-m` means "only when started as `python -m backend.app`"
(the production process). "import" means it runs at module import (`initialize_app` / `init_agent_manager`), so it
also runs under Gunicorn or a test client. Rounds always run back to back: a loop sleeps *after* a round, so the
interval is measured from the end of the previous round.

| # | Thread (name) | File | Start | First round | Then | Exclusion | Writes | Go today |
|---|---|---|---|---|---|---|---|---|
| P1 | `scheduled-refresh` | utils/scheduled_refresh.py:319 | `-m` | 30 s | every 30 s after the round | flock `.task-scheduler.lock` held for the process lifetime; per-key claim set `_running` shared with "run now"; 3 workers | subscription and rule caches, task-status, shared config | `refresh.Loop`, own flock |
| P2 | `router-rule-sync` | routers/rule_sync.py:462 | `-m` | 30 s | every 30 s | flock `router-rule-sync/.scheduler.lock` held for life; `_worker` semaphore(1) shared with manual `start_job` (round running → manual 409); per-connection `job_lock` | receipts, devices, rule caches, task-status, notifications | missing (R14) |
| P3 | `domain-discovery-auto` | utils/domain_discovery_service.py:1050 | `-m` | 120 s | every 600 s | flock `probes/.auto.lock` held for life; `ProbeBusy` (one running job globally) | probe stores, rule library, profile history | `discovery.autoLoop`, own flock |
| P4 | `oxidns-ranking-history` | agents/oxidns_ranking_history.py:374 | `-m` | 60 s | every 60 s | flock `.oxidns-rankings.lock` held for life | ranking SQLite | `monitor.rankingLoop`, own flock |
| P5 | `metrics-history-flush` | agents/metrics_flush.py:186 | `-m` (+ `load_history()` before serving, SIGTERM handlers) | 30 s tick | tick 30 s; runs when the configured interval (300/600/1800/3600 s) is due; `_wake` cuts the wait short | `_run_lock` | metrics files, traffic SQLite, task-status | `metrics` loop + `OnShutdown` |
| P6 | `sub-store-idle` | utils/sub_store_runtime.py:214 | import, if built-in Sub-Store exists | 0 | 5 s | `work`/`control` lease locks (flock, shared/exclusive, timeouts) | Sub-Store state file, process | `substore` loop |
| P7 | `dns-follow` | agents/dns_follow.py:715 | import | 0 | 5 s; `IdleRounds` (skip while the config head is unchanged, ≤600 s) | flock `.dns-follow.lock`, non-blocking per round and per route edit (409 `DNS 联动正在处理，请稍后重试`) | Agent `dns_follow_state`, deployments | missing (A17) |
| P8 | `oxidns-ha-publish` | agents/oxidns_ha_publish.py:664 | import | 0 | 2 s; `IdleRounds` | `.oxidns-ha.lock` (enable/disable, 409 `DNS 高可用正在处理，请稍后重试`); pair lock then member locks in sorted order, all non-blocking | pair publication jobs on disk, Agent records | missing (A18) |
| P9 | `ipv6-verification-scheduler` | agents/ipv6_verification.py:599 | import | 0 | 5 s | executor (2 workers, 8 slots); per-Agent RLock in ipv6_readiness | verification reports | missing (A23) |
| P10 | `agent-rule-sync-loop` | agents/rule_sync.py:605 | first request that passes profile resolution and the auth gate (`before_request`; public heartbeats count) | 0 | 5 s; ≤2 Agents in flight, round-robin cursor | per-Agent flock `<root>/worker.lock`, non-blocking | Agent rule-sync state, deployments, task-status, presence log lines | missing (A16) |
| P11 | `webdav-auto-backup` | utils/webdav_backup.py:170 | after a commit, auto_backup on | 1 s debounce | one upload at a time, latest snapshot only | Condition | WebDAV, task-status | `webdav.Worker` (own goroutine) |
| P12 | `agent-address` | agents/address_follow.py:64 | heartbeat with a new address | – | throttle 120 s per (Agent, host, HA member) | `_attempts` dict | Agent host, notifications | `upgrades.startThread` (plain `go`) |
| P13 | `gateway-onboarding-<id>` | agents/gateway_installation.py:723 | publish request | – | one worker per session | `<session>.publish.lock` flock held for the worker's lifetime (busy → return current state, not 409); `<session>.lock` blocking; `.create.lock` blocking | session files, deployments | missing (A22) |
| P14 | `router-rule-sync-now` | routers/rule_sync.py:442 | manual sync | – | – | `_worker`, `job_lock` | as P2 | missing (R14) |
| P15 | `task-<key>` | routes/tasks.py:245, utils/scheduled_refresh.py:312 | `/api/tasks/run` | – | – | `_background` / `_running` key sets → 409 `这个任务正在运行，请稍后查看结果` | per task | `tasks.StartBackground`, `refresh.RunNow` (plain `go`) |
| P16 | `domain-<kind>-<id>` | utils/domain_discovery_service.py:908 | probe/website/region start | – | – | one running job globally (`_JOBS_LOCK`), finished jobs kept in memory | probe stores | `discovery/jobs.go` (plain `go`) |
| P17 | `sub-store-update` | utils/dependencies.py:349 | update request | – | – | `_update_lock` non-blocking (`已有更新任务在进行中`) | runtime dir, in-memory state | `substore.Dependencies.Go` |
| P18 | `configflow-queue-watch` | http_queue.py:35 | waitress dispatcher | – | – | – | log `请求排队已等待 …` | not needed: net/http has no fixed worker pool (O12 ledger) |
| P19 | matcher cleanup timer | utils/domain_matcher_cache.py:180 | first cached matcher | 60 s | 60 s while cached; idle 1800 s | `_LOCK` | memory | `rules/matchercache` (`time.AfterFunc`) |
| P20 | load-with-timeout | common/setup_completion.py:89 | setup check | – | – | – | – | per call |
| P21 | fan-out pools inside one call | provider_response (8 + single flight + 12 s last-good), content_sync (2 + TTL cache), ros_clients, pending (25 s), client_discovery, mosdns_archive, node_latency, rule_library (10), aggregations (8), deployment_bundle (8) | per request/round | – | – | – | – | mostly ported; provider flights outlive the request |

Startup work Python does synchronously before serving (import time or `__main__` before `run_http_server`):
`load_config()` (rule-proxy token rotation), Sub-Store monitor start, `prune_orphan_caches()`, `init_agent_manager()`
(incl. legacy `runtime.sqlite3` import), `node_test_dns_cleanup.migrate()`, `metrics_flush.load_history()`.

### 0.2 Python locks

| Lock | File | Kind | Who takes it | Protects | Contended → |
|---|---|---|---|---|---|
| `.config.lock` + per-path RLock | common/config_repository.py:1947 | blocking flock, polled, timeout `CONFIGFLOW_LOCK_TIMEOUT_SECONDS` (300 s) | every config write; startup migration | `configflow.sqlite3` (with `BEGIN IMMEDIATE`, busy_timeout, commit-meta conflict check) | `Timed out waiting for profile repository lock` |
| router `job_lock` | routers/rule_sync_store.py:52 | in-process Lock + flock `router-rule-sync/<digest>.lock`, **non-blocking** | apply/probe/manual sync (`_manual_sync_lock`), settings PUT/DELETE (`_router_settings_lock`), auto round, client fixed IP, and **inside `.config.lock`** in `_router_commit_guard` | one device connection (type + endpoint) | 409 `此路由器正在发布或同步，请稍后重试` (taken *before* 404/400 checks, routers X04); in the commit guard 409 `路由器正在发布或同步，不能替换连接或删除记录，请稍后重试` |
| `router_operation_lock` | routers/route_editor.py:30 | in-process RLock per connection, **blocking, no limit** | publish, route edits, R013 GET, R021 preview, rule sync, fixed IP | device read-modify-write sequences | waits (R013 waits for a running apply) |
| rule-sync `_worker` | routers/rule_sync.py:20 | Semaphore(1), non-blocking | auto round, manual `start_job` | one rule-sync job at a time | 409 `已有规则同步任务正在运行，请稍后重试`; a round is skipped |
| `.scheduler.lock`, `.task-scheduler.lock`, `.oxidns-rankings.lock`, `probes/.auto.lock` | P1–P4 | non-blocking flock held for the process lifetime | the loop thread | one loop per DATA_DIR across processes | loop silently not started |
| deployment preparation | agents/manager.py:465 | RLock + in-memory set + flock `.agent-deployment-locks/<sha256>`, non-blocking | `begin_deployment` (inside the config transaction), `_preparation_running` (polls) | one preparation per Agent deployment; process death = not running | 409 `Deployment preparation already running` |
| Agent rule-sync worker | agents/rule_sync.py:342 | flock per Agent, non-blocking | P10 workers | one device operation per Agent | skip |
| `.dns-follow.lock` | agents/dns_follow.py:57 | flock, non-blocking | P7 rounds, binding edits, retry | DNS follow preparation/submission | 409 `DNS 联动正在处理，请稍后重试` |
| `.oxidns-ha.lock`; pair/member locks | agents/oxidns_ha.py:47, oxidns_ha_publish.py:90 | flock, non-blocking; pair first, then members sorted | HA enable/disable; P8 | HA state of a pair | 409 `DNS 高可用正在处理，请稍后重试`; round skips |
| gateway installation `<id>.lock`, `.create.lock`, `<id>.publish.lock` | agents/gateway_installation.py:84/211/682 | flock: blocking, blocking, non-blocking lease | session read-modify-write; session creation; publish worker | one session / one capability per installation / one worker | lease busy → current state (200) |
| `.generated-previews/lock` | agents/generated_preview.py:91 | blocking flock | preview store + eviction | artifact folders | waits |
| OxiDNS dashboard state | agents/oxidns_dashboard_state.py:121 | 64 striped RLocks + flock, both with 5 s timeout | dashboard state read-modify-write | per-device state file | `DNS statistics state is busy` |
| `.compose-enrollment.lock`, `.issue.lock` | agents/docker_ha_compose.py:110, docker_ha_enrollment.py:45 | blocking flock | Docker HA enrollment | enrollment files | waits |
| `web-listener.lock` | utils/web_listener.py:46 | non-blocking flock | web port change | listener state, Nginx | 409 `Web 端口正在更新，请稍后重试` |
| `task-status.json.lock` | utils/task_status.py:43 | Lock + blocking flock | every receipt | `task-status.json` | waits |
| `notifications.lock` | utils/notification_store.py:42 | RLock + blocking flock | every notification | `notifications.json` | waits |
| Sub-Store `work`/`control` | utils/sub_store_runtime.py:60 | flock shared/exclusive with timeouts (0, 45, 90 s) | conversions (shared), stop/maintenance (exclusive) | the on-demand Node process | `正忙` errors |
| `_update_lock` | utils/dependencies.py:338 | Lock, non-blocking | Sub-Store update | one update | `已有更新任务在进行中` |
| ipv6_readiness per Agent | agents/ipv6_readiness.py:40 | RLock, blocking | readiness prepare/verify | one Agent's host checks | waits |
| `_core_locks` | agents/mihomo_panel.py:216 | Lock per channel, blocking only without a cached answer | Mihomo core release lookup | one GitHub fetch per channel | cached answer |
| memory guards | metrics_history, traffic_history, traffic_store, runtime_state, presence_log, address_follow, content_sync, provider flights, idle_rounds, domain_probe, ruleset_content, fast_yaml, oxidns_monitor, metrics_flush | Lock/RLock/Event | in-memory maps | – | – |

### 0.3 Go today (`68c86a01`)

- `internal/lifecycle` (143 lines): global `Loop(name, fn)`, `OnShutdown`, `Start(ctx)`, `Stop(timeout)`, `Names()`.
  Each loop implements its own first delay, tick and flock. Registered loops: `scheduled-refresh`,
  `domain-discovery-auto`, `oxidns-ranking-history`, `metrics-history-flush`, `sub-store-idle`,
  `subscription-cache-prune` (a one-shot startup job registered as a loop). Shutdown hooks: metrics flush, WebDAV
  worker close, discovery matcher cache clear.
- `cmd/configflow/main.go`: SIGTERM/SIGINT → web and API `Shutdown(30 s)` → `lifecycle.Stop(30 s)`. Metrics are
  flushed only at the end (Python flushes first). No instance exclusion beyond the per-loop flocks.
- Detached goroutines started with a plain `go`: `tasks.StartBackground`, `refresh.RunNow` and `RunRound` workers,
  `discovery/jobs.go`, `provider.runFlight`, `webdav.Worker.run`, `upgrades.startThread`, `substore` update,
  `weblistener.serve`; per-call fan-outs in `mosdns/archive.go`, `health/latency.go`, `health/runner.go`,
  `localhttp`, `rulefetch/transport.go`. Nothing tracks them at shutdown; panics are recovered ad hoc (some not).
- `syscall.Flock`/`unix.Flock` in 12 files: repo (`.config.lock`), refresh, discovery, monitor rankings and
  dashboard state, agents deployments, taskstatus, notifications, weblistener, substore runtime, installer (2).
- `context.WithoutCancel` in 29 files, mostly in handlers (api/domaindiscovery 10, api/nodes 5, …) — rule 4b is
  applied call site by call site.
- `time.Sleep` in 11 non-test files (lock polling, retries, loops) — not driven by a test clock.
- SQLite: `store` (MaxOpenConns 1, busy_timeout = lock timeout, journal DELETE), traffic history (1 conn),
  rankings (1 conn, WAL), installer store (1 conn), legacy runtime import.
- Most loop/lock owners of the agents and routers domains are not ported yet (A11–A14, A16–A18, A22, A23,
  R05–R17), so the model below lands before them.

## 1. Scope

In scope:
1. One task model for every background activity: periodic loops, one-shot startup work, detached jobs started by
   requests, and the busy/409 semantics of "run now".
2. One lock model: what still needs a file lock, the in-process primitives, lock levels and ordering, lock-before-
   validation, timeouts.
3. Cancellation (request vs detached work), the shutdown sequence on SIGTERM, recovery of "running" states after
   a restart or crash.
4. Shared in-memory state (online status, caches, single flight) and how it meets persistence.
5. Storage versioning for future data changes (WORKFLOW O3 row).
6. Test hooks so Go unit tests, proddiff and the Flask↔Go difftests can disable or drive the loops deterministically.

Not in scope: repository transaction internals and typed documents (n7-n8); business logic of the loops (owned by
the module ports); HTTP server limits such as body size (O9/O12); performance tuning (O10).

## 2. Interface contract

### 2.1 One process per data directory (decision D1)

Python coordinated several processes on one DATA_DIR (Gunicorn workers, a debug reloader, CLI tools) with flocks.
Go runs exactly one server process per DATA_DIR and **enforces** it, so every other cross-process lock can become
an in-process lock.

```go
// Package instance makes sure one ConfigFlow server owns a data directory.
package instance

// ErrInUse: another ConfigFlow server (Go, or a Python one still running) holds the data directory.
var ErrInUse = errors.New("data directory in use")

// Acquire takes DATA_DIR/.configflow-server.lock (non-blocking flock, retried every 500 ms until ctx ends;
// main passes a 30 s deadline so a restarting container's predecessor can exit) and, in the same way, the four
// lifetime locks a Python server holds while it runs (`.task-scheduler.lock`, `router-rule-sync/.scheduler.lock`,
// `.oxidns-rankings.lock`, `probes/.auto.lock`). All five stay held until Release. The server lock file gets
// "pid version start-time" for diagnostics (never read for decisions).
func Acquire(ctx context.Context, dataDir string) (*Instance, error)
func (i *Instance) Release()
```

- main calls `Acquire` before `repo.OpenDefault`. `ErrInUse` → log (`messages.go` constant
  `数据目录 %s 正被另一个 ConfigFlow 服务使用，本进程退出`) and exit status 1; the container's restart policy retries.
- Holding Python's lifetime locks also means a Python server started *after* Go cannot start its loops (it would
  still serve requests; PORTING already forbids running both).
- **Kept file locks:** `.config.lock` (repo; n7-n8 contract, cheap, still serialises against any tool that follows
  Python's protocol) and the instance locks. **Removed:** every other flock listed in 0.2. Go creates no new
  per-resource lock files; Python's leftovers stay on disk untouched (the difftests already exclude `*.lock` by
  glob). Orphan-cache pruning still deletes a cache's `.lock` sibling, as Python did.
- PORTING rule 7 is replaced by a pointer to this section (done in this commit).

### 2.2 `internal/lockx` — in-process locks

```go
package lockx

// Level orders the blocking locks of the server (section 3.2). Acquire in increasing order only.
type Level int

const (
	LevelJob        Level = 10 // L1 operation locks: try-only (non-blocking), the 409 locks
	LevelDevice     Level = 20 // L2 device/session locks: blocking, around device read-modify-write
	LevelRepository Level = 30 // L3 .config.lock (taken by repo only)
	// L4 leaf stores and L5 memory guards use sync.Mutex directly (rules in 3.2).
)

// ErrTimeout is returned by Lock when the wait limit passed.
var ErrTimeout = errors.New("lock wait timed out")
// ErrBusy is returned when a LevelJob Lock/RLock attempt finds the lock busy.
var ErrBusy = errors.New("lock is busy")
// ErrPanic identifies a shared worker that panicked.
var ErrPanic = errors.New("lockx: shared work panicked")

// Mutex is a lock that can be tried, waited for with a context, or waited for with a limit. Zero value unusable;
// use NewMutex. Not reentrant.
type Mutex struct{ /* chan struct{} + level + name */ }

func NewMutex(level Level, name string) *Mutex
func (m *Mutex) TryLock(ctx context.Context) (held context.Context, release func(), ok bool)
func (m *Mutex) Lock(ctx context.Context, limit time.Duration) (held context.Context, release func(), err error) // limit <= 0 = until ctx ends

// Keyed is a set of Mutexes created on demand per key and dropped when released and unused (a 404 flood leaves
// nothing behind). Held reports whether a key is locked now (Python's "is running" checks after process death).
type Keyed[K comparable] struct{ /* … */ }

func NewKeyed[K comparable](level Level, name string) *Keyed[K]
func (k *Keyed[K]) TryLock(ctx context.Context, key K) (context.Context, func(), bool)
func (k *Keyed[K]) Lock(ctx context.Context, key K, limit time.Duration) (context.Context, func(), error)
func (k *Keyed[K]) Held(key K) bool

// RWMutex with try/limit for shared leases (Sub-Store work/control).
type RWMutex struct{ /* … */ }

func NewRWMutex(level Level, name string) *RWMutex
func (m *RWMutex) TryLock(ctx context.Context) (context.Context, func(), bool)
func (m *RWMutex) TryRLock(ctx context.Context) (context.Context, func(), bool)
func (m *RWMutex) RLock(ctx context.Context, limit time.Duration) (context.Context, func(), error)
func (m *RWMutex) Lock(ctx context.Context, limit time.Duration) (context.Context, func(), error)

// Flight runs one call per key at a time; later callers wait for its result (or their own ctx).
type Flight[K comparable, V any] struct{ /* … */ }

// Spawner supplies a tracked background worker; O3-2 adapts lifecycle.Spawn.
type Spawner interface {
    Spawn(ctx context.Context, name string, fn func(context.Context)) error
}
func NewFlight[K comparable, V any](spawner Spawner, name string) *Flight[K, V]

func (f *Flight[K, V]) Do(ctx context.Context, key K, fn func(ctx context.Context) (V, error)) (V, error, bool /*shared*/)

// Order checking: every Mutex/Keyed/RWMutex acquisition records its level in the returned context. A *blocking*
// acquisition while the context already holds a level >= the new one is a violation: logged once per lock name
// pair through logx (backend.lockx, ERROR) and counted. TryLock never violates (it cannot deadlock).
func Violations() int64
```

**Clarified in O3-1 (coordinator, 2026-11-17):**
- `LevelJob` is always try-only, including calls through `Lock`/`RLock`; contention returns `ErrBusy` for
  the owner to map to 409. `TryLock`/`TryRLock` explicitly express non-blocking acquisition. On other
  levels, both Mutex and RWMutex wait until ctx ends when `limit <= 0`, or at most the positive limit.
  Context cancellation returns `ctx.Err()`; expiration of the explicit wait limit returns `ErrTimeout`.
- A blocking order violation is logged through `backend.lockx` at ERROR once per held/waiting name pair;
  each offending acquisition attempt increments `Violations` once and still proceeds. It never panics or
  returns an order error. Released records in an old held context are ignored. Try acquisitions cannot
  violate order, even inside L3; active ancestors still matter for subsequent blocking acquisitions.
- L4 leaf stores and L5 memory guards use `sync.Mutex` directly, with no extra exported level constants.
  lockx checks L1–L3 and never performs file I/O or creates/removes old per-resource lock files.
- Flight uses the injected `Spawner` above and generic `NewFlight`. Before spawning it passes
  `context.WithoutCancel(ctx)`, preserving values; the worker receives the spawner's context (which a
  later lifecycle adapter may cancel at shutdown). Every caller, including the first, can cancel its
  own wait without cancelling the work. A zero-value Flight returns the `messages.go` diagnostic
  `未配置 spawner` rather than starting an unmanaged goroutine.
- Spawner rejection or a worker panic completes the call and wakes every waiter. Panic is logged once
  through logx and returned as `ErrPanic`; ordinary fn errors are returned without duplicate logging.
  `shared` follows singleflight: once a call has a joiner, all of its results are shared. Completed calls
  are removed, so a later call for the same key starts fresh work.

- Call every `release` exactly once (defer it); a second call is a no-op.
- `held` contexts must be passed on to everything called while the lock is held, so the checker and repo's
  `holdsLock` see it. repo records `LevelRepository` in the context it already returns from `acquireLock`.
- The router job lock (`repo.Hooks.JobLock`) becomes `Keyed[string]` at `LevelJob`. Keys keep Python's
  equivalence classes: `type + "\n" + strings.TrimRight(endpoint, "/")` when the router exists, otherwise
  `profileID + "\n" + routerID`.
- No `lockx` call takes a lock across processes; `instance` and `repo` are the only packages that may call
  `Flock` (gate in step O3-7).

### 2.3 `internal/lifecycle` v2 — tasks, jobs, shutdown

```go
package lifecycle

// Clock is the time source of loops, jobs and TTL caches; tests use a fake.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

// Start modes of a periodic loop.
type StartMode int

const (
	AtBoot            StartMode = iota // Python: import time or `python -m backend.app` — Go is always the service process
	AfterFirstRequest                  // Python: started by before_request (Agent rule sync, P10)
)

// Periodic is one background loop. Rounds of one loop never overlap.
type Periodic struct {
 OnStart func(ctx context.Context) // optional, synchronous memory setup once per ModeRun activation, before first wait
	Name       string                                  // stable kebab-case id = Python thread name; logs, tests, task rows
	Start      StartMode
	FirstDelay time.Duration                           // before the first round
	Interval   func(ctx context.Context) time.Duration // pause after each round, read every time (settings apply without restart)
	Round      func(ctx context.Context) error          // logged at WARNING with Name on error; panics recovered + logged; never stops the loop
	Wake       *Signal                                  // optional: Notify ends the current pause early (metrics flush `_wake`)
}

// Startup is one-shot work done synchronously, in registration order, before the listeners accept requests.
type Startup struct {
	Name string
	Run  func(ctx context.Context) error // an error is logged; it never stops the server (Python logged and continued)
}

// ShutdownHook runs in phase order (section 3.4); Phase lets metrics flush run first and last.
type ShutdownHook struct {
	Name  string
	Phase Phase // PhaseStopSignal (right after SIGTERM), PhaseAfterDrain, PhaseClose
	Run   func(ctx context.Context)
}

func RegisterPeriodic(p Periodic)
func RegisterStartup(s Startup)
func RegisterShutdown(h ShutdownHook)

// Spawn runs fn in a goroutine that may outlive ctx's request: cancellation of ctx is dropped (values kept), a fresh
// repo Scope is installed and reset afterwards, panics are recovered and logged with name, and the goroutine is
// tracked for the shutdown drain. fn's context is cancelled only at the drain deadline; it should poll Stopping
// between units of work. Returns ErrStopping once the drain deadline passed.
func Spawn(ctx context.Context, name string, fn func(ctx context.Context)) error

// ClockFrom returns the originating Runtime clock, otherwise real time.
func ClockFrom(ctx context.Context) Clock

// Detach drops request cancellation but keeps the originating Runtime job cancellation.
func Detach(ctx context.Context) context.Context

// Stopping is closed when shutdown starts (SIGTERM). Loops and jobs check it between units of work.
func Stopping(ctx context.Context) <-chan struct{}

// RequestSeen is called by httpx after profile resolution and the auth gate let a request through; the first call
// starts the AfterFirstRequest loops.
func RequestSeen()

// Runtime runs the registered specs. main uses the default; tests build their own.
type Runtime struct{ /* … */ }

type Mode int

const (
	ModeRun    Mode = iota // production: timers drive the loops
	ModeManual             // loops never run by themselves; tests and proddiff call RunRound
)

func NewRuntime(opts Options) *Runtime // Options{Clock, Mode, Grace, Registry, Drain}
func (r *Runtime) RunStartup(ctx context.Context)
func (r *Runtime) Start(ctx context.Context)
func (r *Runtime) RunRound(ctx context.Context, name string) error // exactly one round, synchronously
func (r *Runtime) WaitIdle(ctx context.Context) error            // all spawned jobs and running rounds finished
func (r *Runtime) Shutdown(ctx context.Context)                   // section 3.4
func (r *Runtime) Names() []string
```

**Clarified in O3-2 (coordinator, 2026-11-17):**
- Timer exposes `C() <-chan time.Time` and `Stop() bool`; Signal has coalescing `Notify`. Registry owns
  ordered specifications with registration methods and a `NewRegistry` constructor. Independent Runtime
  methods include Spawn, Stopping and RequestSeen; it implements lockx.Spawner, with an adapter for the
  default global Spawn as well. Runtime natively supports repeated Start → Shutdown → Start.
- `Options.Drain func(context.Context) error` is the O3-3 HTTP shutdown adapter. During drain, Runtime
  waits concurrently for Drain and jobs/rounds; only when both are idle can it finish early. Until then,
  handlers may still Spawn, at most through grace−2s. With no Drain, idle standalone runtimes (including
  repeated-Start/Stop difftests) return immediately instead of imposing a fixed six-second pause.
- Hooks execute in controlled goroutines; overall grace bounds how long Shutdown waits. Go cannot kill
  a callback that ignores its context: all normal hooks and Drain must honour ctx.
- Migrate only the one-shot subscription-cache-prune to RegisterStartup in O3-2; keep the five periodic
  consumers on legacy Loop until O3-4. No test-mode legacy-name whitelist is introduced.
- Proddiff builds use tagged main helpers for the route manifest and tagged lifecycle mode for manual
  scheduling, Names and readiness after Startup. Normal helpers are no-ops, with no production env or
  flag capable of disabling loops. Delete source overlays/build.py when no longer needed and update
  the proddiff README. Difftest builds append events under their own build tag only.
- `Configure(opts Options) error` changes the idle/stopped default Runtime, returning `ErrRunning`
  while active. A nil Registry retains all existing Wire registrations; Clock/Grace/Drain are replaced.
  The default mode stays fixed by its build (manual in proddiff, run in production). DefaultSpawner's
  zero value forwards global Spawn to the newly configured instance; *Runtime is also a Spawner.

- `Loop`/`OnShutdown` were temporary adapters through O3-4 and are removed in O3-5.
- **Busy keys stay with their owner** (different messages and sharing rules): a `lockx.Keyed[string]` at
  `LevelJob` per owner. "Run now" = `TryLock(key)` → owner's `*tasks.Busy{Message}` (409) when held, else
  `Spawn` with the release deferred inside the job. A loop that shares a worker with "run now" (P1 per key, P2
  `_worker`) takes the same key for its round and skips the round, or the item, when it is held. The task list's
  `running` field reads `Held(key)`.
- `taskstatus` stays the receipt store; lifecycle records nothing itself (triggers `schedule`/`manual`/`event`/
  `shutdown` and record-idle rules differ per owner).
- **Build-tag modes replace the proddiff source overlay:** `lifecycle/mode_proddiff.go` (`//go:build
  configflow_proddiff`) sets the default Mode to `ModeManual` (Startup work still runs, as Python's import-time
  work does) and writes `Names()` plus a ready marker; `lifecycle/events_difftest.go` (`//go:build
  configflow_difftest`) appends one JSON line per round start/end and job start/end
  (`{"t":…,"loop":…,"event":…}`) to `$CONFIGFLOW_LIFECYCLE_EVENTS`, so the black-box difftest can wait for "round
  N of X finished" instead of sleeping. Production builds have neither; no environment variable can switch loops off.

**Clarified in O3-3 (coordinator, 2026-11-17):**

Implementation notes: [startup and shutdown](o3-startup-shutdown.md).
- Remove the old lifetime flocks from refresh.Loop, discovery.autoLoop and monitor.rankingLoop in
  O3-3, before Periodic migration. Otherwise the instance's separately opened descriptors exclude
  its own loops. Preserve first delays, intervals and round behaviour. Cross-process exclusion is
  tested at instance.Acquire; each Go loop must execute while the instance owns the directory.
- repo.ResolveDataDir(path) shares Open's existing home-expansion and path resolution. Main resolves
  before instance.Acquire, without opening storage; absolute, home and symlink aliases must share locks.
- repo.PrepareConfig(ctx) performs configuration initialization without the manager hook;
  LoadConfig retains its existing callers' behaviour. Main explicitly orders configuration preparation,
  the existing prune, agents.Init and metrics history loading as Startup work before listening.
  The metrics manager-init load hook moves to Startup now; that part of O3-4 is already complete.
- Omit node-test-dns-migrate. Go refuses legacy storage; users first upgrade to the final Python
  release, whose startup already performed this retired migration. Do not resurrect it in Go.

**Clarified in O3-4 (coordinator, 2026-11-17):**
- Signal.Clear() is nonblocking and discards at most one pending notification. Notify and normal
  waits keep their semantics. Only metrics OnStart and ResetFlushState call it, preventing an old
  shutdown wake from shortening the next generation's first 30-second wait.
- Export ClockFrom(ctx) Clock: use the originating Runtime's clock, otherwise the real clock.
  Owner scheduling waits use it; existing owner business timestamp injection stays unchanged.
- Periodic.OnStart(ctx) is optional, once per actual ModeRun activation before the first wait.
  Metrics sets its started-at baseline and resets stopping/started state there on each generation.
  Manual mode never invokes OnStart and must not invent a next-run-at value.
- Export Detach(ctx) context.Context: retain values, drop request cancellation, retain Runtime job
  cancellation at grace−2s. Without a Runtime it is context.WithoutCancel. In section 2.5 and future
  owner migrations use lifecycle.Detach, keeping PORTING 4b request-disconnect semantics unchanged.
- In O3-4 change refresh.detached, discovery RunAutoTask/automatic rounds and the synchronous
  startJob(wait=true) Background path only. Async discovery jobs, tasks.StartBackground and metrics
  manual jobs remain O3-5; remaining handler detach moves remain O3-6. Held rows here mean refresh.
- Sub-Store work/control leases and lock polling remain O3-6. Migrate only its 0/5 Monitor scheduling;
  retain the persisted state.json kernel-monotonic clock domain unchanged.

**Clarified in O3 review (Opus, 2026-11-17; findings in `parity/review-o3.md`):**
- Detach keeps a recorded cancellation parent: the generation's job context for rounds, jobs, Startup work,
  OnStart and bound requests; the overall shutdown grace inside shutdown hooks and Drain (they run across
  or after the job cancellation phase, so the job context would already be cancelled there).
- `BindRequest(ctx) context.Context` (and `(*Runtime).BindRequest`): httpx binds every request to the live
  generation, so 2.5 owner entry points that detach from a request keep the grace−2s shutdown cancellation.
  A context already inside a Runtime, or a process without a live generation, is returned unchanged.
- RequestSeen does eligibility work only for the first admitted request of a generation; later
  registrations and Start start newly eligible loops themselves.
- main: a second SIGTERM/SIGINT while the stop-signal hooks (the first metrics save) run is ignored and
  logged, as Python did; afterwards it exits at once. A stop signal during the ≤30 s instance wait ends the
  wait and exits 0 before storage is opened.

**Clarified in O3-5 (coordinator, 2026-11-17):**
- Retire the now-unused Loop/OnShutdown entry points, legacy Registry entries and legacy runner.
  Keep the restart regressions on Periodic, including unfinished-round exclusion across generations.
- Move Clock, Timer and the real implementation to internal/clock. Lifecycle keeps type aliases and
  ClockFrom; fake clocks stay in lifecycletest. MatcherCacheOptions receives Clock/Spawner from discovery.
  Do not duplicate clock interfaces or retain time.AfterFunc (lifecycle → repo → rules forbids a reverse import).
- Flight.DoChan(ctx, key, fn) returns a buffered channel of Result[V]{Value, Err, Shared}, sharing the same
  underlying call with Do and creating no goroutine per waiter. Provider selects request cancellation,
  the 12-second Clock timer and that same result channel; missing/unreadable fallback never starts another render.
- Activate WebDAV each generation through Startup, with injected Clock/Spawner. Explicit Close stays terminal.
  Preserve the upload gate across generations: old and new uploads must not overlap. Worker/webdav.WaitIdle
  remains owner-scoped; Runtime also tracks the spawned work without replacing owner waits with global waits.
  Matcher timer waits end at Stopping, so housekeeping cannot force an idle drain to wait until grace−2s.
- Matcher cleanup needs no extra Stopping interface. Discovery clears its owner through a
  PhaseStopSignal hook, closing the private timer-wait stop channel at shutdown's start. A nil
  MatcherCacheOptions.Spawner starts no background work; synchronous eviction remains available.
  Pin the production discovery wiring in a test: its options must always inject a non-nil Spawner.
- Migrated job I/O (WebDAV Run/Upload/request, provider preparation, Sub-Store HTTP/exec, upgrades I/O)
  uses Detach or passes the job context, preserving request-disconnect behavior and existing timeouts.
  Local batch commits such as CommitCacheUpdates and repo transactions remain uninterruptible.
  Handler-level WithoutCancel remains O3-6.
- Implement tasks/metrics-manual/discovery/WebDAV/upgrades/Sub-Store/listener first. Clock extraction,
  Flight.DoChan and dependent provider/matcher changes wait until the joint O3 review is merged, then rebase.

**Clarified in O3-6 (coordinator, 2026-11-18):**
- Move the recorded cancellation parent and Detach implementation into internal/ctxx, depending only
  on context. lifecycle.Detach remains a forwarding API; repo owners use the same implementation,
  avoiding the lifecycle → repo import cycle. Explicit ctxx.Uninterruptible marks local atomic/batch
  work and masks the recorded parent: a downstream Detach must not restore shutdown cancellation.
  CommitCacheUpdates, CommitProviderBundle and local batches use this entry. Test disconnects,
  ordinary shutdown cancellation and Detach nested inside an uninterruptible batch.
- NewLeafKeyed is for bounded, cancellable leaf-lock waits and releases unused entries. It neither
  records nor checks L1–L3 levels; it never erases an ancestor's recorded level. Do not add an L4
  constant or mislabel leaf locks as device locks. Ordinary L4 stores still use sync.Mutex.
- Sub-Store's fixed work → control nesting has an explicit L2 suborder; the reverse remains a
  violation. Pass held contexts rather than dropping them or suppressing checking. Use/Maintenance
  callbacks become func(context.Context) error. JobLock receives ctx; StateStore and weblistener
  blocking entry points receive ctx. Never recursively RLock while a writer is waiting.
- .compose-enrollment.lock belongs to bootstrap.py on the target Agent, outside the server's
  instance-lock boundary. Keep that cross-process flock and generated script unchanged. Only the
  server-side installer .issue.lock is replaced here. O3-7 checks Go AST calls, not generated script
  strings, for server-side Flock.
- Notifications collect external observations outside the leaf lock, then merge under it with a
  version guard against stale observations. Monitor uses L3 read/verification → L4 CAS, preserving
  the R06 atomic target/publication check without an unlocked verification window. A minimal repo
  locked-read snapshot interface is allowed. Add concurrent regressions for both boundaries.
- JobLock missing its routers implementation fails closed; add the rejection regression before
  wiring the real routers-owned Keyed. The old nil-hook implementation actually allowed commits,
  so O3-6 corrects it. Routers writes and that wiring wait until R08/R09 have merged, then rebase.
- As noted in review-o3, metrics history serialization spans disk I/O and task receipts; record it
  as an L2 operation lock with cancellation, rather than a short L5 memory mutex.
- Split delivery (coordinator, 2026-11-18): O3-6a includes the non-routers locks, ctxx, owners and
  missing-JobLock fail-closed default. Deliver it on current go without waiting for R08/R09, so N8-5
  can build on the repository lock boundary. O3-6b separately wires the routers-owned JobLock and
  completes R04–R07/R09 write/rollback Detach after those ports merge. Do not weaken fail-closed to
  make a differential pass; report any affected router write path before proceeding.
- Leave every Python-written lock file untouched. Final acceptance uses server/scripts/gate.sh
  (-p 6 on .64); it includes both tags, full race, all enabled difftests and proddiff.
- Repo's other resource locks are included: profile operations use L2 Keyed by profile id, shared
  caches/generated files use bounded NewLeafKeyed, and only .config.lock keeps the cross-process
  flock with L3 recording. Writes follow operation L2 → reads/necessary L3 → file leaf;
  deletion follows operation L2 → config L3. Do not touch or create the old .profile-operations
  locks or per-file .lock paths. Test same-profile serialization, different-profile parallelism,
  delete/write races, cancellation/limits, normal order and unchanged legacy-file sentinels.

**Clarified in O3-6c (coordinator, 2026-11-18):**
- Ordinary repo transactions retain their owner's work context after lock admission. Remove the
  automatic Uninterruptible around the whole transaction. Store.CommitContext checks cancellation
  after all BeforeCommit hooks, immediately before SQLite COMMIT; cancellation rolls back all rows.
  Ordinary file writes check immediately before the primary rename and remove abandoned temporary files.
- Explicit caller-owned Uninterruptible batches (provider cache/bundle) keep the O3-6a contract.
  Three repo paths also span filesystem and database effects: DeleteProfile (move/export plus DB),
  ImportAll/ResetAll (backup snapshot plus DB), and CreateRuleLibraryItem (cache plus DB). Their
  callbacks check the work context immediately before the first filesystem effect and mark a
  request-local batch. After that marker the final database commit must complete despite shutdown.
  All other transactions remain cancellable. An AST test restricts markers to these three paths;
  adding a path requires updating this design. Test cancellation before and after each marker.
- writeAtomic has two replacements: the primary file and its best-effort previous-content .bak.
  Check cancellation before the primary rename. After that rename succeeds, finish the existing
  backup logic without another cancellation failure: a committed primary is reported as success.
  Test shutdown after primary replacement, including the resulting backup.
- Repo.WriteSharedTextIfChanged(ctx, relative, content) shares WriteSharedText's implementation and
  leaf lock. Touch only when both the primary and .bak match; otherwise atomically replace.
  Check ctx before Chtimes and preserve Python's mtime freshness semantics. Rulefetch uses this
  owner instead of an unlocked TouchUnchangedCache; remove the unused pure helper.
- taskstatus adds RecordContext/ForgetContext while retaining old entry points. Check the detached
  work context under the leaf mutex and immediately before replacement; a blocked writer must not
  commit after Runtime shutdown. Migrate production receipt callers to the context entries.
- Every shared window listed by C29/A14/C33/C06 gets real Runtime Start/BindRequest/Shutdown tests:
  shutdown while waiting or before commit preserves DB/files/revisions; client disconnect alone
  still completes. Preserve red/green evidence. Coordinate repo signatures with N8-5 before edits;
  O3-6c merges first, then N8-5 rebases. Activate A14's skipped regression once A14 is merged.
- C33's staged-cache protocol is replayable, not another uninterruptible batch: its durable invalid
  marker keeps readers fail-closed between database commit and guarded rename/Sync/marker removal.
  Shutdown may retain the new declaration plus marker; reads serve the current database inline bytes,
  never the old cache. After reopening, the next edit repairs the cache and clears the marker. Test
  that sequence on the merged C33 tree. Preserve F7's L3 declaration guard and F8's owner-context
  restoration; do not duplicate checks at the same action. Migrate C33 Forget to ForgetContext.

### 2.4 Mapping table (every Python loop/lock → Go construct)

Porters of the missing modules use these names, timings and levels; reviewers check them.

| Python | Go | Start / timing | Lock or key (level) | Busy → |
|---|---|---|---|---|
| P1 scheduled-refresh | `Periodic{"scheduled-refresh", AtBoot, 30s, 30s}`; workers fan out with a WaitGroup inside the round (≤3) | as Python | `refresh` Keyed per task key (L1), shared with run now | 409 `这个任务正在运行，请稍后查看结果` (after the 404/ValueError lookup, as Python) |
| P2 router-rule-sync | `Periodic{"router-rule-sync", AtBoot, 30s, 30s}` | as Python | worker key `routers:rule-sync` (L1) shared with start_job; job lock (L1); op lock (L2) | 409 `已有规则同步任务正在运行，请稍后重试` before any lookup; round skipped |
| P3 domain-discovery-auto | `Periodic{…, 120s, 600s}` | as Python | discovery single-job lock (L1) | `ProbeBusy` skipped in rounds |
| P4 oxidns-ranking-history | `Periodic{…, 60s, 60s}` | as Python | – | – |
| P5 metrics-history-flush | `Periodic{…, 30s, 30s, Wake}`; `Startup{"metrics-load-history"}`; shutdown hooks at PhaseStopSignal and PhaseAfterDrain | as Python | save operation Mutex (L2; spans history I/O and receipts) | run now uses the task key |
| P6 sub-store-idle | `Periodic{…, 0, 5s}` (registered only when built-in Sub-Store exists) | as Python | `work` RWMutex (L2; idle stop = explicit TryLock; conversions shared with 45 s; maintenance exclusive with 90 s), `control` Mutex (L2) | `正忙` texts |
| P7 dns-follow | `Periodic{…, 0, 5s}` + `agents.IdleRounds` | as Python | `dnsfollow` Mutex (L1, try) for rounds and edits | 409 `DNS 联动正在处理，请稍后重试` |
| P8 oxidns-ha-publish | `Periodic{…, 0, 2s}` + IdleRounds | as Python | HA Mutex (L1, try); pair then sorted member keys (L1, try) | 409 `DNS 高可用正在处理，请稍后重试`; round skips |
| P9 ipv6-verification | `Periodic{…, 0, 5s}`; verification jobs via `Spawn` limited by a semaphore of 2 (8 slots) | as Python | per-Agent Keyed (L2) | – |
| P10 agent-rule-sync | `Periodic{…, AfterFirstRequest, 0, 5s}`; ≤2 Agent jobs via `Spawn`, round-robin cursor | parity: started by `RequestSeen` | per-Agent Keyed (L1, try) | skip |
| P11 webdav-auto-backup | `webdav.Worker` goroutine started with `Spawn`; `WaitIdle` delegates to the Runtime | 1 s debounce | worker mutex (L5) | – |
| P12 agent-address | `Spawn("agent-address", …)` | 120 s throttle (Clock) | `_attempts` (L5) | – |
| P13 gateway-onboarding | `Spawn("gateway-onboarding-"+id[:8], …)` holding the publish key until it ends | – | publish lease Keyed (L1, try → return current state 200); session Keyed (L2); create Mutex (L2) | as Python |
| P14 router-rule-sync-now | `Spawn` | – | worker key + job lock (L1) | as P2 |
| P15 task-<key> | `Spawn` + owner's Keyed | – | L1 | 409 |
| P16 domain jobs | `Spawn` | – | single-job lock (L1) | as Python |
| P17 sub-store-update | `Spawn` | – | update Mutex (L1, try) | `已有更新任务在进行中` |
| P18 queue-watch | none (O12 ledger: no worker pool, so no queue warning) | – | – | – |
| P19 matcher cleanup | `time.AfterFunc` replaced by Clock timer in `rules/matchercache` | 60 s / 1800 s | L5 | – |
| P21 provider flights | `lockx.Flight` per key; the render runs under `Spawn` so it survives the request and the 12 s last-good fallback | – | – | – |
| router `job_lock` | `Keyed[string]` (L1) behind `repo.Hooks.JobLock` | – | L1, try only (also inside L3) | as Python |
| `router_operation_lock` | `Keyed[string]` (L2) per connection | blocking; read paths (R013, R021) wait with the request ctx, write paths with detached ctx and a 10 min limit (> 300 s apply) | L2 | limit reached → 409 `此路由器正在发布或同步，请稍后重试` (O12: Python waited forever) |
| deployment preparation | `Keyed[[2]string]` (L1, try) inside `begin_deployment`; `preparationRunning` = `Held` | – | L1 | 409 `Deployment preparation already running` |
| `.generated-previews/lock` | Mutex (L4) | – | L4 | – |
| dashboard state | Keyed per state file (L4) with 5 s limit | – | L4 | `DNS statistics state is busy` |
| installer `.compose-enrollment.lock`, `.issue.lock` | Mutex each (L4) | – | L4 | – |
| `web-listener.lock` | Mutex (L1, try) | – | L1 | 409 `Web 端口正在更新，请稍后重试` |
| `task-status.json.lock`, `notifications.lock` | `sync.Mutex` in the store (L4) | – | L4 | – |
| ipv6_readiness per Agent | Keyed (L2) | – | L2 | – |
| `_core_locks` | `lockx.Flight` per channel + cached answer | – | – | – |

### 2.5 Request handlers (PORTING 4b, made structural)

- **The owner detaches, not the handler.** Every exported entry point that writes state, takes an L1–L3 lock,
  records task status, starts a job or joins a shared download starts with `ctx = lifecycle.Detach(ctx)`.
  Repo's matching owner detachment is added in O3-6 through the shared ctxx implementation; it was not
  already present after n7-n8. Handlers pass `c.Context()` unchanged. Existing handler-level
  `WithoutCancel` calls (29 files) move into the owners in step O3-6; a handler may keep one only where the owner
  is a pure function.
- External I/O always has its own timeout from Python (`context.WithTimeout(detached, …)`); the request ctx is used
  only for pure reads, and for waiting on an L2 lock in a read-only handler.
- Go has no proxy in front of it: no 300 s `proxy_read_timeout`, and `http.Server` has no `WriteTimeout`, so long
  handlers run to completion after the client gives up — the same end state as Python behind Nginx (routers X10,
  agents X15). The browser's own timeout replaces Nginx's 504 page (O12 ledger).
- Lock-before-validation (routers X04): where Python takes an L1 lock before its 404/400 checks, Go takes it at the
  same point, so a busy resource answers 409 even for a malformed request. Every port lists, per L1 lock, which
  checks run before and after it (from the Python source) in its PROGRESS section; the reviewer checks the order.
  Known cases: routers `_manual_sync_lock` (R011, R017, R020) and `_router_settings_lock` (R009, R010, R015 PUT,
  R024 PUT); rule-sync `start_job` (`_worker` before lookup). Counter-examples where validation comes first:
  `/api/tasks/run` for subscriptions/rule sources (lookup 404/409 before the claim), gateway publish (body check,
  then lease), Sub-Store update (built-in check, then lock).

## 3. State and concurrency

### 3.1 Who owns what

| State | Owner | Mutated by | Persistence |
|---|---|---|---|
| `configflow.sqlite3` | repo/store (n7-n8) | repo transactions under L3 | SQLite, atomic commit |
| receipts, task-status, notifications, session files, probe stores, preview store | their package (one owner each) | owner, under its L4 mutex, atomic write (temp + fsync + rename) | JSON files |
| other SQLite files (traffic, rankings, installer store) | their package, one `*sql.DB` with `MaxOpenConns(1)` and busy_timeout | owner only | SQLite |
| Agent online status, heartbeat metrics | `agents` runtime state | heartbeats (memory only); version change → repo write *after* releasing the runtime mutex | memory (120 s freshness; restart = offline until the next heartbeat, X07) |
| caches (matcher, provider last-good + flights, content sync, rankings reuse, core release, discovery, upstream reminders, idle rounds, `View` snapshot) | their package | owner, L5 mutex | memory |
| busy keys, running jobs | owner's `Keyed`; Runtime tracks goroutines | owner | memory |

Rules for shared memory: one owner per value; an L5 mutex is never held across I/O, a callback, a channel send or
another lock; readers get copies (or immutable values, as `View`). Caches derived from configuration are keyed by
the repository head (`store` generation / commit id) instead of being invalidated by hooks; TTL caches read time
from the lifecycle Clock. A cache miss storm goes through `lockx.Flight`.

### 3.2 Lock levels and ordering

Acquire in this order only: **L1 job (try) → L2 device/session → L3 repository → L4 leaf store → L5 memory.**

1. L1 locks are only ever *tried*. A held L1 lock produces the owner's 409 / skip, never a wait. Several L1 locks
   are taken in a fixed order (HA: pair, then members sorted). Because nothing waits for an L1 lock, L1 can never
   be part of a deadlock; that is why the commit guard may try router job locks while holding L3.
2. L2 locks wait. Inside L2 only L3 and below. Typical publisher: L1 job → L2 operation → device I/O → L3 repo write
   of the readback → release.
3. L3 is held only for the transaction function and the commit: **no network, device, or subprocess I/O under L3**
   (Python: "Never hold the repository lock over I/O"); the `ConfigurationCommitted` hook and every follow-up
   (WebDAV, notifications of commit results, pushes) run after release.
4. L4 stores take no other lock and call nothing outside their file inside; hooks (`taskstatus.OnRecord`) run after
   release (already true).
5. L5: see 3.1.

The checker (2.2) enforces 1–3 at run time in every build; tests assert `lockx.Violations() == 0`. Rules 3–5 are
checked by review and by the fault tests.

Timeouts: L1 none (try). L2 as in 2.4. L3 `repo.LockTimeout()` (`CONFIGFLOW_LOCK_TIMEOUT_SECONDS`, 0.1–3600 s,
default 300 s) → `Timed out waiting for profile repository lock`, independent of ctx (n7-n8). L4 unbounded except
the dashboard state (5 s). SQLite busy_timeout of `store` = the lock timeout (unchanged).

### 3.3 Task states

Periodic loop: `registered → waiting(first delay) → running(round) → pausing(interval | wake) → running …`;
`Stopping` closed → no new round; a running round finishes its current item (or is cancelled at the drain
deadline) → `stopped`. `AfterFirstRequest` loops stay `registered` until the first `RequestSeen`.

Job: `Spawn → running → done`; panics → logged, done. After the drain deadline the job's ctx is cancelled.

Busy key: `free → held (TryLock ok) → free (release in the job's defer)`. A request that fails between TryLock and
Spawn releases the key itself (as Python did around `Thread.start`).

### 3.4 Shutdown (SIGTERM / SIGINT)

Python: the signal handler flushed metrics (`shutdown` run), waitress stopped, app.py flushed again (only when there
was new data or a failure), then `os._exit(0)` **without waiting for any thread** — in-flight publishes, refreshes
and syncs were killed. Go keeps both flushes and does better within the container's grace period:

| t | Step |
|---|---|
| 0 | Close `Stopping`; stop all loop timers (no new rounds); run `PhaseStopSignal` hooks: metrics flush #1 (`record_idle=true`, trigger `shutdown`). |
| 0 | `http.Server.Shutdown` for web and API (listeners close; in-flight handlers continue) — concurrently with the drain. |
| ≤ grace−2 s | Drain: wait for in-flight handlers, rounds and spawned jobs. Spawn still works for handlers that are finishing. |
| grace−2 s | Cancel every job/round context (device ops stop at their next ctx check — Python's kill, but later and cleaner); wait ≤0.5 s. |
| then | `PhaseAfterDrain` hooks: metrics flush #2 (`record_idle=false`), WebDAV worker close (no new upload; an in-flight upload is cancelled). |
| then | `PhaseClose` hooks: close SQLite handles, flush logx, release the instance lock; exit 0. |

`grace` = `CONFIGFLOW_SHUTDOWN_GRACE_SECONDS` (default 8, so the whole sequence fits Docker's and supervisord's
default 10 s). A second SIGTERM/SIGINT during shutdown exits at once (Python: second Ctrl+C force-quits), except
while the first metrics save runs, when it is ignored as in Python (O3 review). Decision
M1 (section 6) is whether the image raises the grace.

### 3.5 Restart and crash recovery

Rule: **Go adds no recovery action Python did not have.** Every "running" state is one of:

| State | Kind | After restart / `kill -9` |
|---|---|---|
| refresh/task busy keys, discovery jobs, Sub-Store update, `_preparing_deployments`, publish leases, rule-sync workers | memory (Python: memory or a flock that dies with the process) | not running; discovery job GET → 404 (T19) |
| router receipt `running` / `queued` | file | `running` → `paused` when the next round or sync touches it; POST 409 `上次同步尚未完成，请稍后重试` until then; orphan `queued` runs normally (X07, T25) |
| Agent deployment `preparing`/`publishing`… | config | `Held(key)` false → the existing recovery (poll the Agent, mark failed) as in Python |
| DNS follow `preparing`/`publishing`, HA pair jobs, gateway session `publishing` | config / files | the owning loop or the next publish request resumes with the recorded IDs |
| Agent upgrade `latest_upgrade` | config | existing A15 logic |
| online status, metrics since last flush, pending WebDAV backup, caches | memory | lost (SIGTERM saves metrics; `kill -9` loses the last segment, as Python) |
| store build temp files | files | removed at open (store, as Python) |

A port that wants an extra recovery action must record it in the O12 ledger and get it reviewed.

### 3.6 Startup order

`instance.Acquire` → logx → `repo.OpenDefault` (migrations, corrupt-file handling) → `Startup` steps in this order:
`load-config` (rule-proxy token rotation), `subscription-cache-prune`, `agent-manager-init` (incl. legacy
`runtime.sqlite3` import), `metrics-load-history` → listeners → `Runtime.Start`
(AtBoot loops; first rounds after their `FirstDelay`). This is Python's import/`__main__` order, omitting the retired node-test DNS migration as clarified in O3-3.

### 3.7 Storage versions (future data changes)

- Additive changes (new fields, new optional files) need **no** version bump: unknown members are preserved (n7-n8
  R2) and missing ones are defaulted on read.
- `meta.storage_version` (now 1) is bumped only when older code could misread or lose data on read-modify-write.
  The newer binary migrates at open under `.config.lock`, before any Startup step, after an online backup
  (`store.BackupTo` → `DATA_DIR/backups/configflow-v<from>-<UTC timestamp>.sqlite3`, kept until the next migration).
  Migrations are an ordered list `[]store.Migration{From, To int; Up func(ctx, *store.Conn) error}` applied in one
  transaction each. An older binary refuses a newer file with the existing `StoreUnsupported` message (Python does
  the same for version > 1, so the first bump also locks Python out — accepted, Python is retired).
- Other SQLite files use `PRAGMA user_version`, JSON stores a top-level `version` member; the same rules apply,
  owned by their package. No migration exists today; none is part of this design's steps.

## 4. Buildable steps

Every step: own branch off `go`, compiles, `gofmt -l` empty, `go vet ./...`, `go test -count=1 ./...` and `-race` on
the touched packages pass, `server/scripts/proddiff.sh` on .32 shows no new difference. Sizes: S ≤ 1 day, M 1–2
days of one Codex agent. Until a step lands, new ports follow the transitional rule at the end.

| Step | What | Size | After | ∥ | Who |
|---|---|---|---|---|---|
| O3-1 | `internal/lockx`: Mutex, Keyed (with clean-up), RWMutex, Flight, level checker + `Violations`; table tests incl. `-race`, try-under-blocking allowed, blocking inversion counted, Keyed map empties after 10k distinct keys. No consumers. | S | – | O3-3 | Codex |
| O3-2 | `lifecycle` v2: Clock (+ fake in `lifecycle/lifecycletest`), Periodic/Startup/ShutdownHook registry, Runtime (ModeRun/ModeManual), Spawn/Stopping/WaitIdle, `RequestSeen`; `Loop`/`OnShutdown` kept as adapters; build-tag files `mode_proddiff.go` and `events_difftest.go`; `tools/proddiff/build.py` drops the source overlay of `lifecycle.go` (keeps the route-manifest part of main or moves it under the same tag). Tests with the fake clock: first delay, interval re-read each round, wake, no overlap, panic recovery, drain/cancel order. | M | O3-1 | – | Codex; **Opus review with O3-3** |
| O3-3 | `internal/instance` + main: Acquire before repo open, Startup steps (3.6), shutdown sequence (3.4) with `CONFIGFLOW_SHUTDOWN_GRACE_SECONDS`, second-signal exit, `httpx` calls `lifecycle.RequestSeen()` after the profile + auth gate. Messages in `messages.go`. Tests: a second process (test binary re-exec) gets `ErrInUse`; a held `.task-scheduler.lock` blocks start; `run()` with an injected signal channel shows flush #1 before drain and #2 after. | M | O3-2 for main | O3-1 for instance | Codex |
| O3-4 | Migrate loops: refresh (lifetime flock removed in O3-3; Periodic 30/30; claims → Keyed; `RunNow` → Spawn), discovery auto (120/600), monitor rankings (60/60), metrics (Periodic + Wake; two-phase shutdown save and `metrics-load-history` Startup **completed in O3-3**), substore idle (0/5, conditional), `subscription-cache-prune` → Startup (**completed in O3-2**); replace `time.Sleep` in these loops by the Clock. Rows' `running` from `Held`. | M | O3-2, O3-3 | O3-5, O3-6 | Codex |
| O3-5 | Detached goroutines → `Spawn`: `tasks.StartBackground`, `discovery/jobs.go`, `webdav.Worker` (+ `WaitIdle` via Runtime), `upgrades.startThread`, `provider.runFlight` (+ `lockx.Flight`), `substore` update, `weblistener.serve` (tracked as a job that ends on Close); `rules/matchercache` timer on the Clock. | M | O3-2 | O3-4, O3-6 | Codex |
| O3-6 | File locks → in-process (2.4): taskstatus, notifications, weblistener, agents deployments (`preparationRunning` = `Held`), monitor dashboard state (5 s limit), installer ×2, substore runtime leases (RWMutex with Python's limits); repo records `LevelRepository` and serves `JobLock` from a `Keyed` owned by `routers` (fail-closed default stays until routers wire it); move handler-level `WithoutCancel` into owners. Python-written lock files are left alone. | M | O3-1, O3-3 | O3-4, O3-5 | Codex |
| O3-7 | Gates as a test in `internal/lifecycle/gate_test.go` (go/parser over non-test files): `go` statements only in `lifecycle` and in an explicit allowlist of fan-outs that wait for their goroutines before returning (each entry `file:func` with a one-line reason); `Flock` only in `repo` and `instance`; `time.Sleep`/`time.AfterFunc` only in an allowlist (lock polling in repo, rate limits) — loops use the Clock. Update AGENT-BRIEF/PORTING pointers if needed. | S | O3-4–O3-6 | – | Codex |
| O3-8 | Fault harness `server/tools/faults/` (runs the built binary on .32 in its own DATA_DIR, loopback only): scenarios in section 5 that exist today; HR ports add their device scenarios to it (A14, A16–A18, A22, R12, R14, R17). | M | O3-3 | O3-4–O3-7 | Codex |

**Historical transitional rule, expired after O3-5** (R05 onward, A11 onward): no new `Flock`; use
`sync.Mutex`/maps for busy keys and locks with the level comments of 2.4; register loops with the existing
`lifecycle.Loop` and implement Python's delays inside; start detached work with a plain `go` wrapped in
`recover` + `context.WithoutCancel` and mark it `// TODO(O3): lifecycle.Spawn`. Steps O3-4–O3-6 migrate them; the
gate in O3-7 finds any that were missed.

O3-2 through O3-5 have now landed: new ports use RegisterPeriodic/RegisterStartup/RegisterShutdown,
Spawn and owner Detach directly. Loop/OnShutdown no longer exist. The remaining old lock and handler
detach sites are migrated in O3-6; the historical plain-go fallback above is not available to new code.

## 5. Acceptance

Unit / race (Go tests, every step):
- `lockx`, `lifecycle`, and every migrated package under `-race`; packages taking more than one lock run their
  tests with `lockx.Violations() == 0` asserted in `TestMain`.
- Fake-clock tests per loop: first round exactly at `FirstDelay`, next one `Interval` after the round *ended*, interval
  change takes effect on the next pause, wake works, AfterFirstRequest loop does not run before `RequestSeen`.
- Concurrent writers: 50 goroutines writing different fields through repo → all kept; same field → Python's conflict
  outcome; commit guard 409 while a router job key is held.

Fault injection (O3-8, black-box binary on .32, separate DATA_DIRs, loopback fakes):
1. Two servers on one DATA_DIR → the second exits with `ErrInUse` within the wait; a Python-style holder of
   `.task-scheduler.lock` (tiny flock script) → Go refuses to start.
2. SIGTERM during a slow refresh/WebDAV upload/heartbeat burst → metrics `shutdown` run recorded (flush #1), flush #2
   only with new data, exit within the grace, instance lock released, restart succeeds.
3. `kill -9` during a refresh and during a metrics flush → restart; task-status, caches and metrics files load; no
   stale busy state (run now works).
4. Lock timeout: `CONFIGFLOW_LOCK_TIMEOUT_SECONDS=1`, an external process holds `.config.lock` → write answers with
   Python's repository-lock timeout response; reads still work.
5. Client disconnect during config writes, subscription fetch, generate, provider render, discovery start (T05,
   T10): final DB/files/task records equal an uninterrupted run.
6. Goroutines: 1000 mixed requests incl. disconnects, then quiescence → goroutine count back to baseline ±5 (read via
   the `configflow_difftest` events file / pprof on loopback in that build).
7. Added by HR ports: kill -9 / SIGTERM mid-publish (router apply, fixed IP, rule sync, Agent push, gateway publish)
   compared with Python's recovery (routers T25, agents T26); lock contention 409-before-400 cases (routers X04).

Difftest timing rules (Flask↔Go, O6 X07/X08):
- Python runs as `python -m backend.app`; Go is the production build plus `configflow_difftest`. Both have the same
  first delays and intervals (table 2.4), so a seeded due item runs in both within `FirstDelay + slack`.
- Never compare when a round ran, only the state after both sides are quiet: Go's events file shows no running
  round/job, Python's thread list (flask_worker) shows no busy worker, and the fakes' transcripts stopped growing.
- Fakes tag each request as request-triggered or loop-triggered; P10 starts only after the first authenticated
  request on both sides (send one no-op request after start).
- `*.lock` files are excluded from file-tree comparisons by exact glob (Go creates none besides the instance and
  `.config.lock`).

Intentionally not kept (to the O12 ledger): per-resource lock files are no longer created (also not for 404s);
waitress queue warning; router operation lock waits are limited to 10 min on write paths; Nginx's 504 at 300 s is
replaced by the browser's timeout; on SIGTERM in-flight work gets up to the grace period instead of being killed at
once; two server processes on one DATA_DIR are refused instead of coordinated.

## 6. Decisions

Taken by the designer (tell the maintainer, no action needed): D1 one process per DATA_DIR enforced by the instance
lock (refuse after a 30 s wait); D2 all per-resource flocks become in-process locks, `.config.lock` stays; D3 Agent
rule-sync loop keeps Python's start on the first request that passes the auth gate (parity; in production Agent
heartbeats start it within seconds); D4 no new crash-recovery actions; D5 two metrics flushes kept, the first right
at the signal.

For the maintainer:

**Decided 2026-11-17 by the maintainer: M1 → keep the default 10 s stop (Go drains ~6 s, then cancels; recovery after restart as designed). Do not raise stopwaitsecs / stop_grace_period in the image.**

**M1 — shutdown grace in the image.** Default Docker/supervisord give 10 s, so Go drains for ~6 s and then cancels
in-flight work (a router apply or Agent publish can be interrupted, like Python, which killed it at once; recovery
on restart is unchanged). Option A (**recommended**): raise it in the official image and compose files to 30 s
(`stopwaitsecs=30` in supervisord / `stop_grace_period: 35s`, `CONFIGFLOW_SHUTDOWN_GRACE_SECONDS=28`), so
publishes usually finish; `docker stop` can then take up to 30 s. Option B: keep 10 s. Users with their own compose
files keep 10 s either way. This only affects packaging (finish domain / O14), not the steps above.

**Clarified in R12 (coordinator, 2026-11-18):** The fourth domain commit exception
(after O3-6c's `repo:DeleteProfile:beginCommitBatch`, `repo:ImportAll:beginCommitBatch`,
`repo:CreateRuleLibraryItem:beginCommitBatch`) is
`routers:saveAbortedPublication:ctxx.Uninterruptible`, in `internal/routers/publish.go`.
Only after `Result.Aborted && Result.Wrote`, one local `SaveRouterPublicationJSON` →
`SaveRouterPublication` transaction must store the failed release, the five explicit
manifest updates, and the required profile revision. This retains the ownership and
recovery evidence for writes already landed on the device. No device I/O, compensation,
or follow-up read is in the uninterruptible region. Cancellation before the first device
write still rejects the transaction. An ordinary publication rejected by the repository's
cancellation guard may enter this one local transaction only if it has not committed.
This does not restore automatic uninterruptibility for ordinary repository writers.
The canonical `TestCommitBatchMarkersMatchDesign` checks both packages and both callees,
resolving ctxx import aliases; each approved package/function/callee appears exactly once.
Runtime tests establish the side-effect condition, one revision, complete evidence, no
post-cancellation device I/O, and lock release; AST alone cannot prove these semantics.

### Clarified in C36 — setup session L2 suborder

`lockx.LevelSetupSession = LevelDevice - 1` 是 L2 的前序：
setup session → device/profile operation → repository。反向阻塞取得 session
锁算违规，try-only 保持既有规则。首次向导 abandon 必须一直持有同一把 keyed
session 操作锁，直到 profile 和 session 都删除完成，避免并发 prepare 重写；
repo.DeleteProfile 自带 profile-operation（LevelDevice）锁，因此不能给两者
同一数值，也不能把跨 repo/I/O 的 session 操作伪装成 L4 叶提交区。C36 的
AdoptOwnedWrite 与自身流程共用该锁；A21 在释放 L3 后才调用它。只读 A20
ProfileSessions 不取得操作锁；未来若需要取得，必须复用 C36 的同一锁与级别，
不得另造一把相同资源、不同级别的锁。
