# O3-7 concurrency source gate

Status: implementation and targeted verification complete; waiting for the coordinator's batch-6 merge notification before rebasing to go.
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

Final follow-up passed:
- Explicit record mode after check, with an independent before/after JSON assertion:
  **5 files, entries/input/dependencies unchanged**. Only the two reviewed Go source
  fingerprints changed; the api/agents update adds exactly the batch-6 dnsfollow_test.go
  hash. No expected response/input bytes were accepted or edited.
- Ordinary race/count1 replay: MosDNS39.233s, api/agents1.497s. All formerly blocked
  oracle regressions now pass, alongside the broad run's remaining successful tests.
- Lifecycle tests under configflow_proddiff and configflow_difftest: both0.536s.
- Actual Flask differentials: mosdns9.345s and httpx29.424s, both pass; JSON validates
  exactly two package passes and zero skip/fail events.
- Final AST scan: **784 non-test Go files**, **8 go + 7 wall-time exemptions**,
  passed0.236s; final line `O37 TARGETED PASS`.

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc '
set -e; cd server
export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(dirname "$PWD") CONFIGFLOW_DIFF_PYTHON=$HOME/cf-test/.venv/bin/python PYTHONDONTWRITEBYTECODE=1
unset CONFIGFLOW_SNAPSHOT CONFIGFLOW_DIFF
selection="^(TestRecordedPythonTests|TestReviewUnholdableYAMLOutsideTheOutputMatchesPython|TestCorpus|TestN73SharedUpstreamMetadataMatchesPython|TestReviewConditionalMatchesWerkzeug)$"
# First run read-only check (expected nonzero for source metadata only).
# PYREF_MODE=check go test -race -count=1 -v -run "$selection" -timeout=15m ./internal/mosdns/ ./internal/api/agents/
# Then, after inspecting zero output differences, record through the original tool.
PYREF_MODE=record PYREF_ACCEPT=1 go test -race -count=1 -v -run "$selection" -timeout=15m ./internal/mosdns/ ./internal/api/agents/
go test -race -count=1 -run "$selection" -timeout=15m ./internal/mosdns/ ./internal/api/agents/
for tag in configflow_proddiff configflow_difftest; do go test -tags "$tag" -count=1 ./internal/lifecycle/; done
CONFIGFLOW_DIFF=1 go test -json -p=2 -count=1 -timeout=20m ./internal/mosdns/difftest/ ./internal/httpx/difftest/
go test -v -count=1 -run "^TestConcurrencyFacadeBoundary$" ./internal/lifecycle/
'
```

The actual foreground wrapper retained `pyref-before.json`, `pyref-check.log`,
`pyref-record.log`, `pyref-replay.log`, both `tag-*.log`, and `diff.json` under
`.32:~/.cache/o37-targeted.wVn4IU/`. Its post-record assertion compares the full
entries maps and version exactly, dependencies/input hashes exactly, and permits
only the two named source keys. All commands remain on .32; no reference output
was invented, no assertion or timeout was relaxed.

## Commits and integration

- 5a0871af4: SpawnerFrom + stale generation rejection, approved design notes/tests.
- 25df64076: localhttp injected tracked callbacks + HTTP production wiring/tests.
- 4b83f2a6b: archive Spawn and Clock-based production retries/tests.
- 13d8b7e0e: canonical AST gate and contributor pointers.
- Final commit: provenance-only oracle refresh and this completed report.

No outstanding product finding. The only failed broad checks were the stale
source fingerprints described above, all now replay-green. No new dependency,
flock, periodic loop, hidden loop switch or unapproved job API was added. Wait for
batch-6 merge notice before rebase; do not rebase onto the still-older go or copy
other branches. Final hand-back names the branch tip; do not push.
