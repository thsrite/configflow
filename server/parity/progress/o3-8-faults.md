# O3-8 fault harness

Status: implementation and 16/16 binary scenarios passed; handed back with the
explicit batch-6 integration todo below. No further tests running.
No push, no sub-agents. Branch go-o3-8 from
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

## Final binary run before batch-6 integration

Commits: `79fb01f90` (instrumentation), `b4b7be2b3` (harness and guide).
On .32:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- server/tools/faults/run.sh
```

Last line: `FAULTS PASS: 16/16`.
Private report: `~/.cache/configflow-faults.9kPvPP/results/report.json`.
The report records checks and observations per scenario; no token/configuration values.

| Scenario group | Evidence |
| --- | --- |
| Instance/Python-holder exclusion | Both real waits refuse with the native Chinese diagnostic; rejected process leaves all config rows unchanged; restart succeeds. Startup-exclusion 60.976s total. |
| SIGTERM | Burst metrics, HTTP body drain + second save, slow refresh and WebDAV all complete and reload. |
| SIGKILL | Actual refresh/metrics/pre-COMMIT boundaries, exact row checks, valid saved prefix, next task succeeds. |
| Lock/disk faults | Actual external flock timeout and cancellation; read-only database/cache failures visible, prior state intact, retry and restart succeed. |
| Disconnects | All five config/subscription/generate/provider/discovery controls have equal complete normalized snapshots, 2.003s. |
| Goroutines | 1000 mixed requests, 100 disconnects; baseline 11 → 11, within ±5. |
| Orphaned controller | Actual 30s bound releases the request, timeout event recorded, commit succeeds; scenario 30.797s. |
| stopJobs deadline | Actual 30s periodic refresh starts at 30.097s; in-flight I/O survives drain, EOF cancellation at 6.005s after SIGTERM; config unchanged after restart. |
| Production isolation | Actual production binary has no route, ignores pause file, emits no probe events, writes and restarts successfully. |

Controls: `go test -race -count=1 ./tools/faults/fixture/` passed (1.446s);
`PYTHONDONTWRITEBYTECODE=1 python test_core.py` passed 8/8, `bash -n run.sh` passed.

## Targeted Go checks (.32)

Commands were run from `server/` through
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc '…'`:

```sh
test -z "$(gofmt -l internal/faultprobe cmd/configflow tools/faults/fixture internal/store/store.go internal/agents/metrics/flush.go internal/configgen/generate.go internal/provider/response.go)"
go vet ./...
go test -race -count=1 ./internal/faultprobe/ ./internal/store/ ./internal/agents/metrics/ ./internal/configgen/ ./internal/provider/ ./tools/faults/fixture/
go test -race -count=1 ./cmd/configflow/ -run 'Fault|ProductionHasNoFaultRoutes'
go test -race -count=1 -tags configflow_difftest ./internal/faultprobe/ ./cmd/configflow/ -run 'Fault|Point|Token'
go test -count=1 ./internal/lifecycle/ -run ConcurrencyFacadeBoundary
go test -run '^$' -bench BenchmarkProductionPoint -benchmem ./internal/faultprobe/
go build -gcflags=-m ./internal/faultprobe/ 2> "$HOME/.cache/o38-inline-final.log"
grep 'can inline Point' "$HOME/.cache/o38-inline-final.log"
CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/config/difftest/ ./internal/api/subscriptions/difftest/
```

gofmt had no output; whole-repository vet passed (run again after the concurrent
observer test was added). Positive last package lines:

```text
ok configflow/internal/faultprobe       1.008s
ok configflow/internal/store           13.953s
ok configflow/internal/agents/metrics  16.603s
ok configflow/internal/configgen       4.238s
ok configflow/tools/faults/fixture      1.365s
ok configflow/cmd/configflow           1.041s
ok configflow/internal/faultprobe      1.023s  # difftest, includes 32 concurrent observers
ok configflow/cmd/configflow           1.274s  # difftest loopback/token guard
ok configflow/internal/lifecycle       0.224s  # source gate, no exemptions added
BenchmarkProductionPoint-12 1000000000 0.6132 ns/op 0 B/op 0 allocs/op
internal/faultprobe/probe_default.go:8:6: can inline Point
ok configflow/internal/api/config/difftest 72.039s
```

### Existing provider failure (not an O3-8 change)

`provider.TestCorpus` failed on 4 snapshot rows. The same command on a detached
**unmodified O3-7@a1d3e1b78**, before either O3-8 commit, has the same four failures:

```sh
go test -race -count=1 -run '^TestCorpus$' ./internal/provider/
```

```text
--- FAIL: TestCorpus (124.46s)
corpus rows: map[N7-3 native boundary:99 bundle:430 parse:224 prepare_sub:745 rendered_bundle:15 shapes:32 snapshot:135], failures 4
FAIL configflow/internal/provider 124.502s
```

The branch's whole provider package failed only `TestCorpus` (213.192s overall).
One representative mismatch is snapshot row 366 with malformed `proxy-providers.F=1`:
reference AttributeError versus native null output. Expectations and product logic
were not changed. Coordinator assigns the expected topology/diagnostic repair to
`go-fix-n8-8a-provider`; this is an existing acceptance failure, not a green result.
Raw baseline log: `~/.cache/o38-provider-baseline.log` on .32.

### Existing subscriptions diagnostic failure

Baseline verification is complete; stopped without extending tests as requested.
Both this branch and detached unmodified O3-7@a1d3e1b78 ran the complete package:

```sh
CONFIGFLOW_DIFF=1 go test -json -count=1 ./internal/api/subscriptions/difftest/
```

Both fail exactly these four children of `TestProviderDeliveryGraphs`, with no
additional failing test:

```text
none/single/malformed
managed/single/malformed
raw/single/malformed
uri/single/malformed
```

For all four, HTTP status is 400 on both sides; the Chinese diagnostic interpolates
the malformed array dialer-proxy as `['relay']` in Python and an empty string in Go.
The exact same diagnostic discrepancy occurs before O3-8. Branch package elapsed
172.081s; baseline 171.170s. Raw complete JSONL logs on .32:
`~/.cache/o38-subscriptions-current.jsonl` and
`~/.cache/o38-subscriptions-baseline.jsonl`. No diagnostic expectation was relaxed,
and neither the corpus nor the subscriptions failure is reported as passing.

## Integration todo

Coordinator 94c212e6 confirmed batch-6 is still waiting on the 8a repair, review and
gate, several hours away; requested hand-back now after the subscriptions baseline
check, without extending verification. **batch-6 合入后 rebase，按 264ffbb5 重跑关机场景**.
The current successful binary report is explicitly pre-integration evidence and
does not claim to have executed the R14 M1 fix. No full gate was run; the coordinator
runs the merged-tree gate. No new fuzz target in this step.

HR device scenarios A14/A16–A18/A22/R12/R14/R17 are explicitly future registry extensions,
not placeholder passes or silently skipped tests.
