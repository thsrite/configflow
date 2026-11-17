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
