# O3-1..O3-4 joint adversarial concurrency/lifecycle review

Reviewer: fresh Opus session (did not write the steps; Codex implemented O3-1 lockx, O3-2 lifecycle v2,
O3-3 instance/startup/shutdown and O3-4 loop migration). Scope: `internal/lockx` (all files),
`internal/lifecycle` (all files incl. `lifecycletest`, both build-tag modes), `internal/instance`,
`cmd/configflow/{main,service,messages}.go`, the httpx first-request hook, and the five migrated owners
(`refresh` scheduled/rows/env, `discovery` auto/jobs/taskbridge, `agents/monitor` rankings/wire,
`agents/metrics` flush/wire, `substore` wire/runtime idle) against `design/o3-concurrency.md` (2.1–2.5 incl.
every "Clarified in" block, 3.1–3.6, table 2.4), `design/o3-startup-shutdown.md`, PORTING rule 4b, the
authors' progress files `progress/o3-{1,2,3,4}-*.md`, and the Python reference
(`utils/scheduled_refresh.py`, `utils/domain_discovery_service.py`, `agents/oxidns_ranking_history.py`,
`agents/metrics_flush.py`, `utils/sub_store_runtime.py`, `routers/rule_sync.py`, `app.py`).
Standing checks from review-r06: test seams must not hide production parameters; compare raw requests at
least once. No production host, device or lab guest was contacted; every test uses loopback fakes, temporary
data directories and synthetic values. No new dependency (the goroutine-leak check uses
`runtime.NumGoroutine` with a stack dump, no goleak).

Method: every lock/lifecycle path was walked for cancellation, panic, rejection, restart and shutdown
interleavings; every suspected defect was turned into a failing test first, then fixed. The fixes were
mutation-checked by reverting each one on the test server (output below).

## Findings

All findings are fixed; **none is open**. Severity count: **0 high, 2 medium, 4 low**.

### Medium

- **F1 — `Detach` inside shutdown hooks and Drain returned an already cancelled context.** Shutdown passed
  hooks a context carrying the generation, and `Detach` always chose that generation's *job* context as the
  cancellation parent. PhaseAfterDrain and PhaseClose hooks run after `stopJobs` (grace−2 s), so any owner
  entry point that follows design 2.5 (`ctx = lifecycle.Detach(ctx)`) and is called from the final metrics
  save (task-status receipt, traffic store, later O3-6 owners) or a close-phase owner would get a cancelled
  context and fail or skip its I/O — the final save of a graceful stop would be lost silently. No current
  production caller detaches on that path (latent), but O3-5/O3-6 move exactly these entry points to
  `Detach`. Fix: Detach keeps a recorded cancellation parent (`cancelParentKey`): the generation's job
  context for rounds, jobs, Startup, OnStart, legacy loops and bound requests; the overall shutdown grace
  for hooks and Drain. Test: `TestDetachInsideShutdownHooksIsBoundedByGrace` (all three phases and Drain see
  a live detached context; it ends with the overall grace).
- **F2 — work detached from a request never received the shutdown cancellation.** Request contexts had no
  generation, so `Detach(c.Context())` was plain `WithoutCancel`. A handler-path owner that detaches (refresh
  `RunNow` preparation, discovery synchronous paths, every 2.5 owner after O3-6) kept running past the
  grace−2 s cancellation (design 3.4: "cancel every job/round context; device ops stop at their next ctx
  check") until the process exited at the end of shutdown, so it could never record its own failure. Fix:
  `lifecycle.BindRequest` (+ `(*Runtime).BindRequest`); `httpx.App.ServeHTTP` binds every request to the
  live generation. Client disconnects still do not reach detached work (4b unchanged); contexts already
  inside a Runtime (localhttp from a job) and processes without a live generation are unchanged. Tests:
  `TestBindRequestKeepsShutdownCancellation`; real-SIGTERM process scenario `detached-handler` (handler
  detaches, blocks on the detached context; it is cancelled at 6 s, before the final save).

### Low

- **F3 — a second signal during the first shutdown save discarded unsaved monitoring history.** Python's
  `handle_stop_signal` ignores a repeated signal while `flush_on_shutdown` runs and force-quits only after
  it (documented for double Ctrl+C). Go's main called `os.Exit(0)` on any second signal, also during flush
  #1. Fix: main registers a last PhaseStopSignal marker hook; a second signal before it is logged
  (`msgSignalDuringSave`) and ignored, after it exits at once as before. Test: real-process scenario
  `signal-during-save` (SIGINT while a slow stop-signal hook runs → full shutdown incl. final save, exit 0);
  existing `second-signal` (after the save) still exits within 3 s.
- **F4 — SIGTERM during the ≤30 s instance wait was ignored.** main installs `signal.Notify` before `run`,
  but read the channel only after startup, so `docker stop` of a second server on the same volume waited
  for Docker's SIGKILL. Fix: `acquireInstance` watches the signal channel during `instance.Acquire` (joined
  goroutine), ends the wait and exits 0 before logging/storage are opened. Test:
  `TestStopSignalDuringInstanceWaitExitsAtOnce` (held Python lock, SIGTERM after 1 s → exit 0 < 3 s,
  diagnostic logged, no `configflow.sqlite3`, all locks free).
- **F5 — every request re-ran loop eligibility under the Runtime operation lock.** `RequestSeen` called
  `startEligible` on every admitted request: a registry snapshot (slice copies) under `opMu`, serialising
  all concurrent requests behind it. Only the first request of a generation can make loops eligible
  (later registrations and Start trigger eligibility themselves). Fix: fast path. Test:
  `TestRequestSeenStartsOncePerGeneration` (request before Start counts; 50 concurrent requests activate
  once; next generation needs its own first request).
- **F6 — a test seam hid the production slot pause of the ranking collector (standing check 1).** The
  ranking tests replace `rankingSleep` with a no-op; nothing pinned the production collector's 0.2 s pause
  (Python `pause=0.2`) or that `rankingPause` uses the Runtime clock and ends at Stopping. Tests only:
  `TestReviewO3ProductionCollectorPausesBetweenSlots`, `TestReviewO3SlotPauseUsesRuntimeClockAndStopping`.

## Checked and found correct

**O3-1 lockx.**
- Order checker: blocking Mutex/Keyed/RWMutex acquisitions walk every *active* ancestor record; equal or
  higher level → counted once per attempt, logged once per name pair, never blocks or panics. Released
  records are skipped but their ancestors still count. Same-level blocking nesting (two L2 keys) is a
  violation, as it must be. LevelJob `Lock`/`RLock` are tries (ErrBusy) and never check order; `TryLock`
  records its level, so a later *blocking* lower/equal acquisition is still reported. L4/L5 stay
  `sync.Mutex` by design; repo does not record L3 yet (O3-6).
- Try cannot deadlock or violate order; a cancelled ctx fails a try (owners only map that to 409 during
  shutdown).
- Mutex token channel: ctx/timer/token races return the token when ctx ended concurrently; `held` flag
  cleared before the token returns. Keyed pins/unpins per attempt, removes entries at zero refs on every
  failure path (try miss, wait cancel, limit), `Held` allocates nothing. Release is idempotent.
- RWMutex: writer preference, writer cancellation/timeout de-registers and wakes readers, readers never
  register; no upgrade (documented). Recursive RLock while a writer waits deadlocks like `sync.RWMutex`
  (note for O3-6).
- Flight: entry registered before Spawn, so joiners before a Spawner rejection are woken with the wrapped
  error; panics/`Goexit` complete the call (ErrPanic, payload not logged) and remove the entry; every
  caller waits with its own ctx; the worker gets `WithoutCancel(first ctx)` → Spawner context.
**O3-2 lifecycle.**
- Periodic: first round exactly after FirstDelay; pause measured after the round; Interval re-read with
  the generation context; Wake ends one pause (coalesced, pending survives a round as Python's
  `_wake.set()` + `wait` + `clear`). Rounds of one name never overlap, also across generations (`busy`
  map); a restarted loop skips while an old uncooperative round runs. Panics in Round/OnStart/Interval
  are recovered and logged.
- ModeManual: no drivers, no OnStart, so metrics `startedAt` stays nil and `next_run_at` is null until a
  run records `last_run_at` — identical to Python with the loop not started. RunRound works in both modes.
- Start/Stop generations: Start after Shutdown creates a fresh generation (new jobs/loops contexts,
  counters, `requested` reset); Shutdown/Start serialise on `opMu`; `pending` spans generations so
  Configure cannot orphan old work. Signal state is shared by spec and is cleared only by metrics
  OnStart/ResetFlushState (approved O3-4 contract).
- Spawn/WaitIdle: admission and the idle check are under one mutex; the drain seal sets `admit=false`
  in the same critical section that observes idleness, so no job can slip in between; a job spawning a
  follow-up before it ends keeps the generation busy. Spawned work gets a fresh repo Scope, panics are
  recovered, the job context is cancelled at grace−2 s, not by the request.
- Shutdown: Stopping closed and loop timers stopped first; PhaseStopSignal hooks; drain deadline
  relative to the shutdown start; jobs cancelled, ≤0.5 s wait; after-drain and close hooks bounded by the
  overall grace; no fake-clock timer leaks. Hooks that ignore ctx cannot block past the grace.
- Detach without a Runtime equals WithoutCancel; inside one it creates no goroutine (cancelCtx parent
  registration through the embedded job context; checked with 1000 children).
**O3-3 instance and main.**
- Five locks in a fixed order, each retried every 500 ms under the 30 s deadline; any failure closes all
  opened descriptors (rollback); `O_CLOEXEC` (Go default) keeps Sub-Store/child processes from inheriting
  the locks; Python's four lock paths match (`data_dir` for rankings/scheduler, raw `DATA_DIR` joins for
  `probes/` and `router-rule-sync/` resolve to the same inodes). Home and symlink aliases share the lock
  through `repo.ResolveDataDir`.
- Startup order 3.6 (Acquire → logx → OpenDefault → load-config, prune, agent-manager-init,
  metrics-load-history → listen → Start); Wire re-registration keeps positions; requests between listen
  and Start count for the generation's first request.
- Shutdown: flush #1 before HTTP Shutdown (Python order), Drain joins its workers, grace default 8 s with
  strict validation, PhaseClose closes connections, flushes logs and releases the instance; real SIGTERM
  idle/publish/blocked-drain/second-signal plus the two new scenarios pass under `-race`.
**O3-4 loops.**
- Timings equal table 2.4 and Python constants: refresh 30/30 (Workers 3), discovery 120/600, rankings
  60/60 (24 slots, 0.2 s pause), metrics 30/30 + Wake, Sub-Store 0/5 only with built-in Sub-Store; each
  owner test drives the production spec constructor (`periodic()`, `autoPeriodic()`, `rankingPeriodic()`,
  `flushPeriodic()`, `idlePeriodic()`) on a fake clock, so the clock does not hide production timing.
- refresh: due keys claimed before the pool (Python `_claim`), ≤3 workers with a slot channel and
  `wg.Wait`; on Stopping the unstarted claims are released and running ones joined; claims are released
  in the worker's defer; `RunNow` looks up before the claim (404/400 before 409) and releases on Spawn
  rejection; rows read `Held`. The worker contexts carry the round's detached job cancellation.
- discovery: per-profile fresh scope, Stopping checked between profiles and before probe jobs; metrics:
  Round skips once stopping, OnStart resets state once per activation; Sub-Store: Tick only scheduling
  moved, busy texts unchanged.
- No `time.Sleep` or bare `go` left in the five loops' scheduling (remaining ones are listed below and
  belong to O3-5/O3-6).
- Raw requests (standing check 2): O3 changes no request construction; the instance-boundary tests record
  every refresh/userinfo/rule transport call at the transport boundary (zero scheduled egress), and the
  ten difftest packages pass unchanged.
- No Python emulation introduced or found in the reviewed code.

## Accepted / noted (no change)

- metrics `runMu` is held across disk I/O and the task-status write, unlike the "L5 memory" label in table
  2.4; it is Python's blocking `_run_lock` and nothing that holds a lockx/L4 lock calls `metrics.Run`, so no
  deadlock path exists. O3-6 should treat it as an operation lock, not L5.
- Spawn keeps the request's `held` lock records in the job context (needed for refresh's L1 key released
  by the job). A job that does a blocking L2 wait while its spawner still holds an L2 lock is reported by
  the checker even though it runs on another goroutine — conservative, keep.
- A second signal exits with status 0 (Python died by the re-raised signal); harmless for Docker/supervisord.

## Rules for O3-5..O3-8 (problem classes to avoid)

1. **Detach, never `WithoutCancel`, in owners.** `context.WithoutCancel` drops the shutdown cancellation
   F1/F2 now provide; use `lifecycle.Detach` at the owner's exported entry point. Do not re-detach inside a
   shutdown hook expecting job cancellation — hooks are bounded by the overall grace.
2. **Every detached goroutine through `Spawn`** (O3-5: `tasks.StartBackground`, `discovery/jobs.go:176`,
   `webdav.Worker`, `upgrades.startThread` (`agents/upgrades/address.go:64`), `provider.runFlight`,
   `substore` update (`dependencies.go:70`), `weblistener.serve`). Release busy keys on every path: Spawn
   rejection (`ErrStopping` during shutdown), panic (defer inside the job), stopping before start.
3. **Test seams must not hide production parameters** (F6): whenever a test replaces a sleep/timeout/limit
   seam, add one test on the production value; drive fake-clock tests through the production spec
   constructor, not a hand-written Periodic.
4. **Wake carry-over:** any new Periodic with a `Wake` must `Clear` it in `OnStart`, or a previous
   generation's notification shortens the first delay.
5. **No blocking waits without ctx in rounds** (O3-6): Sub-Store `lock()` polls with `time.Sleep` up to 45/90 s
   and ignores ctx, so a Tick can hold shutdown to the grace; dashboard state polling
   (`agents/monitor/state.go:283`) likewise. Migrate to `lockx` with ctx and Python's limits; do not nest
   `RLock` on the Sub-Store RWMutex (writer preference deadlocks).
6. **repo must record `LevelRepository`** in its held context (O3-6) — until then the checker cannot see a
   blocking L2 wait inside an L3 transaction.
7. **Gate allowlist (O3-7)**, current joined fan-outs outside lifecycle: `refresh/scheduled.go:RunRound`
   (WaitGroup), `cmd/configflow/service.go` serve goroutine (joined by `<-served`), stop goroutine,
   `drainServers` (joined), `acquireInstance` watcher (joined); per-call fan-outs `health/latency.go`,
   `health/runner.go`, `mosdns/archive.go`, `localhttp`, `rulefetch/transport.go` need a wait check each.
   Flock only in `repo/lock.go` and `instance`; today also `substore/runtime.go`, `agents/deployments.go`,
   `agents/monitor/state.go`, `taskstatus`, `installer/docker_ha_enrollment.go`, `notifications/store.go`,
   `weblistener` (O3-6 removes them).
8. **Fault harness (O3-8):** reuse the real-process scenarios of `cmd/configflow/service_test.go`
   (idle, publish, blocked-drain, second-signal, signal-during-save, detached-handler, instance-wait
   signal); add goroutine baseline ±5 after 1000 mixed requests via the difftest events/pprof build, and
   for every HR port a SIGTERM mid-publish case that checks the owner recorded its cancellation.
9. **Hooks must honour ctx**; anything slow in PhaseStopSignal delays the HTTP drain start (Python
   order) and is bounded only by the overall grace.

## Commands and results

Mutation check (fixes reverted one by one on `.64`, new tests against the old behaviour):

```
--- FAIL: TestDetachInsideShutdownHooksIsBoundedByGrace
    review_o3_test.go:58: after hook detached into a cancelled context: context canceled
--- FAIL: TestRealSIGTERMServiceShutdown/signal-during-save
    service_test.go:339: second signal during the first save was not ignored
--- FAIL: TestRealSIGTERMServiceShutdown/detached-handler
    service_test.go:113: shutdown cancellation did not reach work detached from a request
```

Final acceptance on **`.64`** (`RT_HOST=sunbroke@192.168.0.64`) with the review fixes on `go@8bf1b189`
(O3-4 included); the branch was then rebased onto `go@6b50151c` (adds the N8-3 review: `model`, `repo`,
HANDOFF) and re-checked there (composition run at the end of this section):

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/ tools/ cmd/)" && go vet ./... \
  && go test -race -p=4 -timeout=40m -count=1 ./...'
RT_HOST=sunbroke@192.168.0.64 rt --no-sync -- bash -lc 'cd server && go test -p=4 -timeout=30m -count=1 ./... \
  && for tag in configflow_proddiff configflow_difftest; do go test -tags=$tag -count=1 ./internal/lifecycle/... \
       && go test -tags=$tag -count=1 -run "^$" ./cmd/configflow; done'
RT_HOST=sunbroke@192.168.0.64 rt --no-sync -- bash -lc 'cd server && for d in $(go list ./... | grep difftest); do \
  CONFIGFLOW_DIFF=1 go test -count=1 -timeout=30m $d; done'
RT_HOST=sunbroke@192.168.0.64 rt --no-sync -- server/scripts/proddiff.sh
```

```
gofmt -l internal/ tools/ cmd/                      (no output)  GOFMT_PASS
go vet ./...                                        VET_PASS
go test -race -p=4 -count=1 ./...                   exit 0, 74 ok, no FAIL
  ok configflow/cmd/configflow 116.837s   (real SIGTERM: idle, publish, blocked-drain, second-signal,
                                           signal-during-save, detached-handler; instance-wait signal)
  ok configflow/internal/lifecycle 3.242s  ok .../lifecycletest 1.016s  ok configflow/internal/lockx 3.422s
  ok configflow/internal/instance 7.745s   ok configflow/internal/httpx 27.167s
  ok configflow/internal/refresh 215.929s  ok configflow/internal/discovery 7.300s
  ok configflow/internal/agents/monitor 221.068s  ok configflow/internal/agents/metrics 5.287s
  ok configflow/internal/substore 6.722s
  ok configflow/internal/yamlx 7.117s
  ?  configflow/tools/proddiff/generator [no test files]
go test -p=4 -count=1 ./...                         FULL_EXIT=0, 74 ok
  ok configflow/internal/yamlx 0.984s
  ?  configflow/tools/proddiff/generator [no test files]
-tags=configflow_proddiff  lifecycle 0.199s, lifecycletest 0.005s, cmd/configflow builds   TAG_PASS
-tags=configflow_difftest  lifecycle 0.198s, lifecycletest 0.004s, cmd/configflow builds   TAG_PASS
CONFIGFLOW_DIFF=1 difftest packages (10 × ok): api/agents 122.771s, api/subscriptions 65.431s,
  discovery 1.706s, health 13.506s, httpx 1.018s, mosdns 1.237s, refresh 2.980s, subs 1.032s,
  substore 4.400s, webdav 13.785s
server/scripts/proddiff.sh   PRODDIFF_EXIT=0, 23 self-checks PASS, 2831 cases / 415 comparisons,
  coverage_regressions 0, unmapped_routes 0, go_startup_once [agent-manager-init, load-config,
  metrics-load-history, subscription-cache-prune], 11 existing unique differences,
  baseline: new=0 stale=0
```

Startup/shutdown changed (F3, F4), so the real-SIGTERM process scenarios ran in both the race and the
plain full suite (all pass; e.g. race: idle 1.04 s, publish 1.06 s, blocked-drain 7.01 s, second-signal
1.03 s, signal-during-save 1.54 s, detached-handler 7.01 s; instance-wait SIGTERM exits in < 3 s).

Composition check after `git rebase go` (`go@6b50151c`, N8-3 review merged), same host:

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/ tools/ cmd/)" && go vet ./... \
  && go test -p=4 -timeout=30m -count=1 ./... \
  && go test -race -p=4 -count=1 ./internal/lifecycle/... ./internal/lockx/ ./internal/httpx/ ./internal/model/ ./internal/repo/ ./internal/instance/'
```

```
GOFMT_PASS  VET_PASS  FULL_EXIT=0 (74 ok; cmd/configflow 104.192s, model 0.008s, repo 5.496s,
  last line ok configflow/internal/yamlx 0.783s)
race: lifecycle 3.225s, lifecycletest 1.013s, lockx 3.330s, httpx 21.609s, model 1.039s, repo 49.523s,
  instance 7.727s
```
