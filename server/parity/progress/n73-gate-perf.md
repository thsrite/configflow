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

## Timings (in progress)

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
