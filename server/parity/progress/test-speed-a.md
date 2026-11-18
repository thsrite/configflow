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
