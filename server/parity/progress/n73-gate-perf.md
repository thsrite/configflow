# N7-3d frozen-reader gate performance

Branch `go-n73-gate-perf`, based on clean `go@c31fbf306`, independent of the
handed-back `go-n8-7@1985337ad`. No product code changes, pushes or subagents.

## Cause and coverage inventory

`Classify` previously decoded the complete 26 MiB compressed `n73_frozen`
archive into all 21,343 source/native-graph records on its first call in each
process. Callers did **not** all re-parse every source: they selected their own
inputs, but still paid that full archive decode and retained all graphs. The
actual all-source Parse replay already lives in
`yamlxtest.TestAllFrozenReaderSources` (added by the independent review).

Eight caller packages trigger the full decode: discovery, gateway, installer,
mosdns, oxidns, provider, routers and routersplit. The helper package itself is
the ninth test process. Direct/indirect calls are `Check` and `Classify`;
`CheckReaderFixtures`/`HistoricalReader` use the separate 625 KiB mode archive
and do not trigger this archive decode. Offline `tools/yamlreaderoracle` also
uses Classify, but its command has no tests and does not execute it in a gate.

## Change

- Embed the original full archive only in `history_test.go`; stream one record
  at a time in the sole complete replay, rather than retaining all graph rows.
- Callers load a 1.5 MiB compressed derived index: exact source SHA256, original
  class, exact native error, SHA256 of the canonical full physical ParseAudit.
  ParseAudit includes tags, values, line/column, ordered child IDs and aliases.
  Each caller still parses and verifies **all of its original selected inputs**.
- Every gate runs all 21,343 sources in the helper test and independently checks
  every index entry against the unchanged full archive. Original archive SHA,
  revision, unique source IDs, source SHA, cardinality and unknown=0 remain
  mandatory. No test skipping, sampling, input removal or changed expectation.
- The generated index has its own pinned SHA. `go generate` in yamlxtest reads
  only the unchanged archive and records metadata/physical-audit hashes; no
  production business functions or new reference outcomes are involved.

## Timings and approved measurement scope (in progress)

Measure the same selected reader migration tests, serial package scheduling
(`-p=1`), `GOMAXPROCS=2`, `-race -count=1`, on .64. The 40-minute timeout is
measurement protection for the known old slow path, not a changed gate default.
Commands and package elapsed times will be filled after both runs. Shared-host
load is reported; timings are observations, never wall-clock assertions.

Baseline exact command:

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=2 --back .ci-results/n73-gate-perf-before -- bash -lc 'set -euo pipefail; mkdir -p .ci-results/n73-gate-perf-before; cd server; go test -json -race -p=1 -count=1 -timeout=40m -run "^Test(AllFrozenReaderSources|N73.*Reader(Migration|Inputs))" ./internal/discovery ./internal/gateway ./internal/installer ./internal/mosdns ./internal/oxidns ./internal/provider ./internal/routers ./internal/routersplit ./internal/yamlx/yamlxtest > ../.ci-results/n73-gate-perf-before/tests.jsonl'
```

Baseline starts at 2026-11-18 00:22:56 UTC on .64 (initial load21.85).
The original code/fixtures remain untouched on that server until it finishes.
A separate .32 early validation runs the new all-source replay and compact-index
negative control after gofmt/vet.

The actual baseline command also prints pass/fail package/test elapsed fields
from this JSONL using Python after go test; the test invocation above is exact.

Coordinator approved avoiding nine old full-decode repetitions (2026-11-18):
use the original N8-7 complete gate as historical before data, explicitly state
load/scope differences, retain discovery's direct before/after observation, and
run every caller and center package's complete race suite on the new tree.
Do not claim a strict speedup multiple. The old process was stopped after an
observation at12m43s elapsed/11m31s CPU/1,327,596KiB RSS with no test result.
This is a censored measurement (>763s), not a passed test or timeout failure.
Only our verified go-test PID and its discovery test child were stopped; no
other gate or process was changed. The old rt command exited143.

.32 preliminary validation: full replay21343, original category counts,
unknown0, race126.629s PASS. Complete helper suite128.618s and discovery24.790s
PASS; discovery's7-row migration test1.50s. The first YAML facade package run
found a forbidden generator import of the test helper; fixed by giving the
offline generator its JSON wire-schema DTO, without widening the AST allowlist.
The full replay checks that wire representation against actual ParseAudit.
Corrected complete yamlx race18.637s PASS, all AST controls retained.
`go generate` output remains byte-identical, SHA
`47e7732d7e632eacae626e6a99ecc2e136a5873c8d2297a68c0c8e5f9c45134b`.

## Highest-priority final acceptance

Coordinator made this task highest priority after the R10 merge gate timed out
installer/provider at60min (installer's old migration gate alone43m22s).
No additional work is being added. The remaining duplicate focused measurement
was stopped after discovery's direct comparison completed, to run the required
complete suites immediately. Its partial gateway/installer/mosdns results are
not substituted for full-package timings.

Same-host direct comparison (.64, same reader test/filter, GOMAXPROCS2, -p1,
-race -count1 -timeout40m): discovery's7-row test1.58s/package2.647s PASS.
Old test had not completed at763s (CPU691s, RSS1,327,596KiB); this lower bound
is retained without estimating a finished old duration or claiming a multiple.

Persistent negative control `TestFrozenIndexRejectsChangedEntry` mutates one
record's class in otherwise valid index JSON and requires rejection by the
actual index decoder. An end-to-end experiment also changed exactly one
category in the real on-disk fixture, ran the full-replay test, verified a
nonzero exit and its exact integrity diagnostic, then restored the original
fixture before any green full-package run. Output:

```text
tampered one index category; metadata and record count unchanged
--- FAIL: TestAllFrozenReaderSources (0.01s)
    history_test.go:37: historical YAML comparison index changed
FAIL configflow/internal/yamlx/yamlxtest 0.021s
47e7732d7e632eacae626e6a99ecc2e136a5873c8d2297a68c0c8e5f9c45134b  internal/yamlx/yamlxtest/testdata/n73_index.json.gz
```

.64 final gofmt empty and full vet PASS; complete caller/helper/facade race
currently running with -p6. Full output and exact commands follow at completion.
