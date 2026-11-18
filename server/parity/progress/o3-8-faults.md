# O3-8 fault harness

Status: implementation in progress; no push, no sub-agents. Branch go-o3-8 from
O3-7@a1d3e1b78. Wait for the coordinator's batch-6 merge notice before rebasing;
then repeat black-box scenarios on the integrated R14 M1 lifecycle (264ffbb5).

## Approved design

Coordinator approved the implementation in Clarified in O3-8:
- shared internal/faultprobe, production empty inlineable Point; no allocations;
- difftest-only, loopback + random private-file token state endpoint, counts/phases only;
- ctx-aware Clock polling with a real independent 30s maximum and timeout record;
- StoreBeforeCommit after existing hooks, immediately before existing ctx.Err/COMMIT;
- MetricsFlush before history file writes; GenerateBuild after owner Detach and
  ProviderRender after pool admission (needed even on cache hits);
- names are typed constants in a single compiled Catalog; harness refuses unknown names;
- production route-table + build-tag/AST guards; O3-7 scans tagged observer source too.

Real server binaries, synthetic marked DATA_DIRs, loopback-only user/network/mount/PID
namespace; mask host resolver sockets, drop capabilities; external flock and read-only
DB/cache faults. Python stdlib drives the binary; Go fixture helper seeds via repo.Open.
No overlay, snapshot/prod data, device access, loop-disable switch or production delay change.

## Current evidence (.32)

Foundation unit/race passes for faultprobe (production/difftest), production route
absence and difftest loopback/token guards. O3-7 gate passes with no extra exemptions.
Production benchmark: 0.6268 ns/op, 0 B/op, 0 allocs/op; compiler: `can inline Point`.

Actual binary scenarios already passed during iteration:
- production API/barrier absence (real production binary);
- SIGTERM heartbeat burst, first shutdown record and silent unchanged second save;
- drain-time partial heartbeat (100-continue proves handler reading body), slow refresh,
  exactly two saves only after new data, all metrics reload;
- in-flight automatic WebDAV upload during drain;
- SIGKILL during refresh and registered metrics-flush barrier, restart + next run;
- SIGTERM/SIGKILL at the actual SQLite pre-COMMIT boundary: exact row snapshot unchanged,
  restart and new writer successful;
- native 1s external config-lock timeout response (400 + exact English native message),
  reads still work; shutdown interrupts a longer real lock wait;
- read-only database and cache directory: failure visible/receipted, prior rows/files intact,
  restore permissions, retry succeeds;
- actual second-process / Python-style lifetime lock refusal (30s each), and signal during
  startup instance wait; all restart;
- 1000 mixed requests including 100 disconnects → goroutine baseline ±5;
- orphaned barrier: actual 30s timeout recorded and work released (no substituted budget);
- five deterministic client-disconnect controls: config/subscription/generate/provider/discovery,
  exact complete DB/files/task snapshot equality with an uninterrupted clone. Last run:
  `~/.cache/configflow-faults.QPeVFO/results/report.json`, 1/1 passed, 2.873s.

Fixture fixes, not product changes: seeder port uses native int, backup settings belong in
system.backup, wait for tracked task-end after receipt (receipt precedes lease release),
provider needs the synthetic config token. Comparison policy excludes only app.log,
normalizes registered wall-clock fields/known provider timestamp header, instance
PID diagnostic, exact panel origin and consistent generated subscription node IDs.
It preserves JSON types, revisions, task counters/statuses, paths/modes, bytes and references.
No business assertion was dropped to clear a difference. Malformed clock values stay visible.

Current remaining work:
- run/verify the new actual periodic refresh stopJobs-deadline scenario (30s production spec);
- add/finish control tests and the one-page extension guide;
- final all-scenario run, scoped Go race/vet/gofmt and production inlining checks;
- await batch-6 merge notification, rebase and repeat scenarios on R14 M1;
- commit final harness and complete exact commands/results report; no full gate.

HR device scenarios A14/A16–A18/A22/R12/R14/R17 are explicitly future registry extensions,
not placeholder passes or silently skipped tests.
