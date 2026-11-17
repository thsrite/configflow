# O3-3 — instance ownership, startup and shutdown

Status: in progress, branch `go-o3-3` from integrated O3-1/O3-2 on `go`. No sub-agents or pushes.

Scope: five lifetime instance locks, ordered synchronous startup before listening, profile/auth-passed
request notification, injected HTTP drain, two metrics flush phases, bounded signal shutdown and cleanup.
Tests/builds run only on servers; real cross-process/SIGTERM acceptance runs on .32.

Coordinator approved in O3-3: remove the three old loop lifetime flocks now without changing timing;
introduce repo.PrepareConfig while preserving LoadConfig callers; explicitly order configuration, prune,
Agent init and metrics history startup; do not resurrect the deliberately retired node DNS migration.
Record these clarifications in design §2 and mark the early metrics startup work done under O3-4.

Instance core and cross-process tests pass on .32: gofmt/vet clean, race 7.726s. Six cases cover each Go/Python lock and all four Python lifetime locks together; diagnostics, preserved legacy bytes, retry, cancellation, rollback and idempotent release also pass. Next: startup/config wiring,
request gate notification, main signal/drain/close phases, real SIGTERM tests, final .32 acceptance,
rebase and clean tree. The existing slow oxidns full-race issue is owned by a separate agent.

Startup preparation and request-gate step implemented: PrepareConfig preserves LoadConfig's hook
behaviour; global RunStartup permits listening only after startup and blocks Configure while running.
Metrics history loading now registers as Startup, with separate first/last shutdown flush hooks;
substore exposes the existing prune registration for main ordering. Three old loop flocks removed,
timing unchanged; per-loop regressions drive the existing waits under real instance ownership.
.63 vet and focused tests pass (repo .101s, metrics .536s, refresh .177s, discovery .100s,
monitor .073s, lifecycle .012s). Initial focused build found two obsolete test imports; removed them.
HTTP profile/auth/public heartbeat regression passes under .63 race (httpx3.340s, lifecycle2.037s). Main now acquires before repository open, registers ordered startup, injects concurrent HTTP drain, and uses phased flush/close/release with second-signal force exit; main vet/build and focused httpx/lifecycle tests pass. Real SIGTERM process coverage and final full acceptance pending.

Real `.32` process race validation passes (cmd/configflow 42.943s): actual SIGTERM idle 1.237s,
publishing 1.481s, blocked HTTP drain 7.009s, second-signal force exit 1.245s; each reacquires the
instance after exit. First save is observed before work completion; second saves its additional point.
A separate process blocked by all four Python-compatible locks exits 1 at 30.05s, emits the same
centralized Chinese diagnostic, and never creates configflow.sqlite3. New finite/range grace parsing
and Configure-during-synchronous-startup tests are being checked before the final acceptance run.

Finite/range grace validation and Configure-during-startup regression pass on .32 race;
cmd12.346s, lifecycle2.031s, fake clock1.015s. The real signal scenarios also reran after the managed
shutdown panic boundary was added. The retired migration was listed in PORTING but not explicitly
in the behaviour-change table; an O12 entry now makes that operator prerequisite explicit.

Per coordinator routing correction, developer operation notes live at
[design/o3-startup-shutdown.md](../../design/o3-startup-shutdown.md), not doc/.
Rebased onto go@045f2485 before final .32 acceptance; three code steps now
6be4e9f6 / feb5e869 / c09be36e. Final acceptance in progress.
