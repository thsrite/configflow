# Test speed A — Python reference recording and slow package analysis

Branch `go-test-speed-a`, based on go `a9d6ce86c`. Scope: mosdns, oxidns,
provider, deploy and children, discovery, routersplit. Routers excluded. No tests
removed, timeouts shortened or assertions relaxed. Ordinary references become
recorded outputs; difftest/proddiff and genuine cross-language interactions stay
live. B group consumes the same `internal/testgolden` helper, no private copies.

## Shared helper foundation

`internal/testgolden`: exact input/output transcripts; source and venv metadata
fingerprints; ordinary replay calls no reference callback. `PYREF_MODE=check`
reports changed outputs and metadata without writing. `record` writes only with
`PYREF_ACCEPT=1`, and only when the test has not failed. JSON-lines process starts
lazily in live modes, closes input and is reaped before recording cleanup.
`tools/pyref/README.md` documents both APIs and the review workflow.

Foundation validation on .32:

```
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server; gofmt -w internal/testgolden; go test -race -count=1 ./internal/testgolden; go vet ./internal/testgolden'
ok configflow/internal/testgolden 1.008s
```

The .64 baseline is running with the command below; after changes, use the exact
same command on that machine and retain JSON timings and /proc/loadavg samples:

```
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'mkdir -p server/parity/test-speed; cat /proc/loadavg > server/parity/test-speed/a-before.load; cd server; go test -race -count=1 -timeout=30m -json ./internal/mosdns ./internal/oxidns ./internal/provider ./internal/deploy/... ./internal/discovery ./internal/routersplit > parity/test-speed/a-before.jsonl; result=$?; cat /proc/loadavg >> parity/test-speed/a-before.load; tail -4 parity/test-speed/a-before.jsonl; exit $result'
```

Pending: baseline results/hotspots; integrate and record each package; zero-diff
live checks; ordinary-test process audit; after timings; final gate/rebase.

Shared-tool follow-up: compressed transcripts supported; JSON-lines process
cleanup belongs to the creating test (so multiple child subtests share one live
oracle). Actual child-Go negative controls verify source/input/venv drift,
changed expected outputs, unaccepted first recordings and failed original
assertions all fail without overwriting. Alias-aware AST inventory checks exact
file/function/callee/count exceptions; new or stale process sites fail.

The original .64 baseline was stopped after coordinator identified concurrent
`go-n73-gate-perf` ownership of the frozen YAML hotspot. It is **not** a valid
before/after measurement. Re-measure after that branch is integrated. No frozen
archive, index, Classify implementation or yamlxtest file is modified here.

Latest process policy: targeted checks only; coordinator runs batch full gates.

Shared follow-up targeted validation (.64):
`go test -race -count=1 ./internal/testgolden ./tools/pyref` passed
(`4.129s`, `1.012s`); targeted vet passed. Negative controls use child Go test
processes and a synthetic venv directory; they execute no Python themselves.

Additional shared audit controls (B-group review): package-level constructors,
constructor function values and aliased imports cannot bypass registration.
Dot imports of process packages are rejected. Literal exec.Cmd construction is
also inventoried. A registered callback is still verified by runtime tracing.

## mosdns — recorded pure references

Converted the four policy-reference consumers to shared `testgolden.NewLines`;
all old corpus rows, structural/native comparisons and assertions remain. The
four input-local converter suites now run in parallel after sequential archive
hook tests. No archive globals are parallelized. Generated transcript JSON is
compressed (about 1.8 MiB total, rather than 13 MiB uncompressed).

On .64, first recording passed (`40.430s`). A separate live read-only check then
passed, **zero transcript/metadata diffs**: 2863 corpus inputs, 5 review inputs,
1 upstream metadata input and an empty recorded-test fallback transcript (the
original static recorded rows still all execute). Selected-package `41.333s` is
live-reference validation, **not** the final race speed measurement.

```
PYREF_MODE=check go test -count=1 -timeout=30m -v ./internal/mosdns ./internal/oxidns ./internal/routersplit -run '^(TestRecordedPythonTests|TestCorpus|TestN73SharedUpstreamMetadataMatchesPython|TestReviewUnholdableYAMLOutsideTheOutputMatchesPython|TestC29ArchivePythonBoundary)$'
PYREF equal: testdata/pyref/TestCorpus.json.gz (2863 exact inputs)
ok configflow/internal/mosdns 41.333s
```

The public Flask differential stays live and unchanged. Final race timings and
ordinary-path process tracing follow after shared N7-3 gate optimization lands.

## oxidns — archive reference

Converted the 18-case C29 archive oracle to a shared exact transcript. Python
still uses its real archive builder and requests fetcher when recording/checking.
Its isolated fake HTTP endpoint serves the same responses as the Go endpoint;
only these two allocated loopback base URLs become a token in the input key.
Output bytes, archive order/modes, input immutability and Chinese refusals retain
all original assertions. The original combined `/raw == 2` assertion is now two
independent `Go == 1 / Python reference == 1` assertions plus equality of the
entire fetch-path/count map; a duplicate or missing Go fetch still fails.

.64 live `PYREF_MODE=check` passed: **18 exact inputs, diff 0**, `0.527s`.
Existing seven-way boundary workers and parallel suites are preserved; no unsafe
shared repository/hook parallelism is added. Full package race timing pending
integration of the independent YAML fixture optimization.

## routersplit — normalized-input oracle

Replaced only the pure JSON-lines Python oracle with the common transcript API;
all recorded/corpus/edge replays, collaborator call ordering, invariants and
error/structural comparisons are unchanged. Replay suites still run sequentially
because they install process-global collaborators; t.Parallel there would race.
No routers business or tests are edited.

.64 separate live check: **5 corpus reference inputs, diff 0**, plus an empty
fallback transcript for the complete original recorded replay. Selected package
passed in `2.806s`. Race timing follows the shared fixture integration.

## provider — independent seed copies

The existing corpora already use recorded Python outputs, so there is no pure
runtime Python oracle to replace. The expensive setup was `repo.Open` on an empty
directory for every stateful row. Each replay/corpus now creates its initial
repository once, copies that closed-on-return filesystem into a **new directory
per row**, then opens a fresh repository object. All original calls, cache
writes, durable SQL writes, revision checks and file assertions still run.
Process-global Locations/Env/aggregation hooks keep these replays sequential.

`TestCorpusRepositoryCopiesCannotLeakState` deliberately writes both shared SQL
state and a cache file to one copy, then verifies an earlier and a later copy
against the full clean export (including revisions) and file absence. Targeted
race control passed in `1.393s`. Complete recorded/corpus rows and the control
passed on .64 in a selected-test race run (`90.000s`; this is not the final paired
measurement and does not include the N7-3 gate test).

```
go test -race -count=1 -timeout=30m -json ./internal/provider ./internal/deploy/... -run '^(TestCorpus|TestRecordedPythonTests|TestCorpusRepositoryCopiesCannotLeakState|TestPythonBuild|TestPythonCorpus|TestPythonCheckerHistories)$'
configflow/internal/provider pass 90
```

## deploy — parallel build corpus

`TestPythonBuild` and each independent row now use t.Parallel. Each row keeps its
own repository, input Source, downloads and all original manifest/file/YAML/error
assertions. The read-only HTTP response table is shared once; server shutdown is
a parent cleanup, after all child subtests finish. Existing recorded references
already require no Python process. .64 selected-test race run passed `5.628s`.

## deploy/pushdiff — parallel recorded corpus

`TestPythonCorpus` and each row now use t.Parallel. Row-local bundles, receipts,
detail budgets and paging lists remain independent; all 32 recorded Python test
identities, minimum-row guard and exact comparison/paging assertions remain.
No runtime Python reference existed here. .64 selected-test race run passed
`17.209s` (complete package timing pending paired measurement).

## deploy/contentsync — parallel histories

The nine recorded histories now run as parallel independent subtests. Each
history constructs its own checker, clock, fake worker queue, limits, config,
agent, preparation failures and version map. Every action and expected job/call/
entry/detail count still executes in its original within-history order.
No runtime Python reference existed. .64 selected-test race run passed `1.630s`.

Shared actual-repository audit is enforced by ordinary `go test ./tools/pyref`,
using per-group `scopes-*.json` plus exact `live-exceptions-*.json`. Missing pairs,
new constructors and stale registrations fail. .64 race audit passed `1.560s`.

Cache boundary identified from Go 1.26 cmd/go source: backend and venv lie outside
server's Go module root, so bare cached `go test` does not recheck those reads.
Actual executions (including current gate's -count=1) do validate hashes. Asked
coordinator to approve a shared native-fingerprint test wrapper for cache-enabled
daily runs; no gate script changes proposed.

## discovery — parallel pure port parsing

No ordinary Python oracle exists here; the explicit HTTP/storage differential
stays live. Pure port-YAML graph/depth/schema/migration tests and registrable-
domain checks are input-local and now use t.Parallel. Job histories/restart/
matcher tests retain sequential ordering because jobFixture resets shared
jobState, some tests replace the default repository and several assert GC liveness.
No timeout, depth, case count or assertion changes. .64 targeted race of the
four non-history parser/domain suites passed `2.049s`; complete paired race run
will include the migration check after the independent frozen index lands.

A process inventory registers exactly nine existing sites: three Go self-process
isolation/restart tests and six unchanged live differential/copy sites. There are
no whole-package exemptions. Normal MosDNS/OxiDNS/routersplit references only
call shared testgolden; ordinary process tracing remains a separate check.

## Cache-safe reference testing (coordinator approved)

Shared `testgolden.Fingerprint` and `tools/pyref -fingerprint` read backend Python
sources and venv metadata natively. `tools/pyref -test -- <go test args>` exports
`PYREF_INPUT_FINGERPRINT` and forwards Go's exact exit code and standard streams.
`New` always reads that env (Go cache input), checks it against the current native
fingerprint and retains its original per-golden hash validation. A missing env
is allowed only for an explicitly supplied positive `-count`, which disables
Go caching; bare cache-enabled tests fail with the wrapper instructions.

Gate integration (coordinator changes gate.sh later, **not edited here**):

```
cd server
export PYREF_INPUT_FINGERPRINT="$(go run ./tools/pyref -fingerprint)"
go test -race -p 6 ./...
```

Compute the export anew before each test batch. The reusable Go function is
`testgolden.Fingerprint() (string, error)`; it starts no Python.

Actual cache negative control on .64 passed. In one foreground command, the
wrapper ran the C29 archive test with race, the identical second invocation hit
cache, a temporary comment was appended to its recorded backend reference file
`backend/utils/oxidns_archive.py`, and the identical wrapper invocation reran and
failed the metadata check. Restoring the original file (trap-protected, preserved
mtime) restored the valid cache hit. No golden or local backend source was edited.

```
go run ./tools/pyref -test -- -race -run '^TestC29ArchivePythonBoundary$' ./internal/oxidns
ok configflow/internal/oxidns (cached)
archive_test.go:66: ... Python 参考已变化，请重新录制 (source/input/venv hash mismatch)
ok configflow/internal/oxidns (cached)
CACHE CONTROL PASS: cached -> backend mutation reruns and fails -> restored cached
```

Evidence logs on .64: `~/.cache/pyref-cache-control/{first,cached,mutated,restored}.log`.
Shared race selfchecks/vet passed (`testgolden 4.359s`, `tools/pyref 1.296s`).

## Ordinary-path process tracing

.64 actual `strace -f -e execve` of all three transformed reference suites passed
(count=1, no record/check mode). Observed **zero Python exec**; the deliberately
executed Python control was detected by the exact same exec filter. No ordinary
Go reference callbacks were allowed through. Logs:
`~/.cache/speed-a-ordinary-exec.log`, `~/.cache/speed-a-exec-negative.log`.

```
strace -f -e execve -s 4096 -o "$HOME/.cache/speed-a-ordinary-exec.log" go test -count=1 -timeout=30m ./internal/mosdns ./internal/oxidns ./internal/routersplit -run '^(TestRecordedPythonTests|TestCorpus|TestN73SharedUpstreamMetadataMatchesPython|TestReviewUnholdableYAMLOutsideTheOutputMatchesPython|TestC29ArchivePythonBoundary)$'
ok configflow/internal/mosdns 19.600s
ok configflow/internal/oxidns 3.113s
ok configflow/internal/routersplit 11.156s
EXEC TRACE PASS: ordinary tests start zero Python; deliberate Python negative control detected
```

Those elapsed values include ptrace overhead and are **not** speed comparisons.

Latest .64 final targeted harness checks passed: changed-path gofmt clean,
`bash -n tools/pyref/measure-a.sh`, `go vet ./...`, and
`go test -race -count=1 ./internal/testgolden ./tools/pyref`
(`4.259s`, `1.323s`). Full gate is coordinator-owned under the updated workflow.

Waiting dependency for the required unbiased paired measurement: go is still
`215ab6b04`; the independently owned gate-performance branch has handed back
at `019204508`. Coordinator asked to remeasure only after integrating it. The
first high-load/pre-index baseline was explicitly discarded, not used as evidence.

Actual guard for unwrapped cache-enabled tests also passed on .64: bare
`go test -race -run '^TestC29ArchivePythonBoundary$' ./internal/oxidns` failed with
`PYREF_INPUT_FINGERPRINT is missing`, rather than allowing an unsafe cached run.

Live process lifecycle control passed under
`PYREF_MODE=check strace -f -e trace=execve,wait4,waitid ...` for the 18 C29 archive
inputs: diff 0; the real Python child exited 0 and its PID no longer existed
under /proc after the test (no live or zombie process). Evidence:
`~/.cache/speed-a-live-reap.log` and `~/.cache/speed-a-live-reap-test.log`.

Resume after N73 integration: rebase go, review changed consumer test hashes,
rerun real recording/check if metadata changed (do not waive the check); prepare
a pristine latest-go baseline tree on the same server; execute
`server/tools/pyref/measure-a.sh BASELINE_TREE LOG_DIRECTORY` once to run before
and after sequentially with the identical race command. Summarize each package's
Elapsed and slowest tests from JSON and both load samples. No valid before/after
numbers are claimed yet; old high-load/pre-index baseline remains discarded.
