# O3-7 concurrency source gate

Status: implementation complete, final targeted verification in progress.
Branch `go-o3-7`, created from the then-current `batch-6@cdca79def`; no push.
The coordinator will notify when batch-6 is merged before the final rebase to go.

## Implementation and decisions

- `internal/lifecycle/gate_test.go` uses go/parser on all server-owned non-test
  Go files, irrespective of build tags. It skips only hidden artifacts and
  dependency directories (`vendor`, `node_modules`), not test helper packages.
  `go` is restricted to lifecycle or exact joined `file:func` entries; Flock
  references are restricted to repo/instance; time.Sleep/AfterFunc references
  require exact exemptions. Import aliases, function values, and relevant dot
  imports are covered. Script/comment text is not a Go call. Stale entries fail.
- **Exemptions: 15 entries total, 8 joined fan-outs + 7 wall-time primitives.**
  The canonical maps contain a one-line reason per entry. The 7 wall-time
  entries comprise 4 deliberate fault delays in loopback fake servers and 3
  production primitives: the per-request devhttp inactivity watchdog, synchronous
  SQLite backup busy polling (250ms), and Sub-Store's existing injected real-clock
  Sleep adapter. No periodic scheduling or production retry gained an exemption.
  Flock has no per-function exemptions; only the two specified packages qualify.
- Fixed actual non-joined work instead of exempting it: MosDNS archive workers
  now use Spawn while preserving fail-fast return and MaxWorkers. Every admitted
  worker remains tracked; panic still delivers a result and Runtime logs it.
- Per the coordinator's two approvals, `SpawnerFrom(ctx)` returns the originating
  Runtime or DefaultSpawner. Stale/stopped generations reject admission, even
  after restart. Source-bound owners use this instead of new Options fields.
- localhttp has an injected lockx.Spawner because importing lifecycle would
  create lifecycle→repo→rules→rulefetch→localhttp→lifecycle. Missing spawner fails
  closed; production App.RegisterLocal injects DefaultSpawner. Handlers retain
  a fresh context, with only shutdown cancellation bridged in (also into Detach),
  and no caller repository transaction/authentication/value inheritance.
  Stream Close still cancels its handler; shutdown also unblocks a pipe writer.
- Agent busy retry uses Runtime Clock; rulefetch retries use the shared Clock
  interface (nil means real). Existing optional sleep test seams remain; production
  no longer assigns time.Sleep. Tests use production 100ms and 1s/2s intervals,
  not replacement timings, and verify cancellation/timer cleanup.
- AGENT-BRIEF and PORTING point at the AST boundary and source-bound SpawnerFrom.
  Approved interfaces and context rules are recorded in Clarified in O3-7.

## Regressions

- Gate negative controls: bare go, path/package spoofing, aliased imports,
  function-value references, dot imports, nested repo package, method ownership;
  positive controls for the exact owning packages, context.AfterFunc, shadowing,
  and embedded script strings.
- SpawnerFrom: bound/unbound/stopped generation, plus stale generation after
  restart; owning Runtime.WaitIdle observes its job.
- localhttp: missing/refused spawner never invokes the handler; actual streaming
  handler Runtime shutdown cancellation, WaitIdle waits through delayed cleanup,
  request disconnect still completes, caller values absent, nested Detach receives
  shutdown cancellation. Production RegisterLocal wiring tested in an isolated
  child process against the configured default Runtime.
- MosDNS early failure: all MaxWorkers start; the failed build returns while
  remaining work is still visible to WaitIdle, and shutdown cancels that I/O.
- Existing archive fail-fast/bounds, streaming, retries and caller suites retained.

## Targeted verification (.32)

No full gate, full-repository race or proddiff; no new fuzz targets.
Focused new regressions passed under race (after correcting two test compile
mistakes: a package-level clock name collision and existing HTTP/lifecycle API names).

The first broad targeted command did gofmt, whole-repository vet and:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server; export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(dirname "$PWD") CONFIGFLOW_DIFF_PYTHON=$HOME/cf-test/.venv/bin/python PYTHONDONTWRITEBYTECODE=1; unset CONFIGFLOW_SNAPSHOT CONFIGFLOW_DIFF; go test -race -p=4 -count=1 -timeout=45m ./internal/lifecycle/ ./internal/localhttp/ ./internal/httpx/ ./internal/rulefetch/ ./internal/agents/ ./internal/mosdns/ ./internal/refresh/ ./internal/api/agents/ ./internal/api/mosdns/ ./internal/server/'
```

Gofmt and vet passed. Race passed: lifecycle6.145s, localhttp1.096s,
httpx80.360s, rulefetch73.448s, agents1365.648s, refresh371.324s,
api/mosdns1.041s, server7.613s. MosDNS and api/agents failed **only** 5 golden
source fingerprints; no race report or semantic assertion failed. Original log:
`.32:~/.cache/o37-targeted.wVn4IU/race.log`.

Read-only `PYREF_MODE=check`, exact five-test selection, race/count1:
MosDNS TestCorpus had 47072 rows, 3706 structural comparisons and 2863 oracle
inputs; TestRecordedPythonTests had 5436 recorded calls from 1432 Python tests.
API conditional reference covered 3000 dates/3000 requests (919 Werkzeug refusals).
**All output comparisons had 0 differ; no PYREF DIFF.** Changed provenance only:
`server/internal/mosdns/archive_test.go` (our regression) and the newly added
`server/internal/api/agents/dnsfollow_test.go` (pre-existing batch-6 input).
Log `pyref-check.log` in the same directory. Re-record only after this review;
independently assert entries/input/dependencies unchanged, then ordinary replay.
Remaining tag and two real Flask differentials are running in the same foreground command.
