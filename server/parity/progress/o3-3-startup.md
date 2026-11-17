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
