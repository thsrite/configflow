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
