# Python reference goldens

Shared Go test helper: `configflow/internal/testgolden`. These goldens replace only
pure, deterministic Python reference calculations. Explicit Flask differentials,
proddiff, product-script tests and live cross-language SQLite/locking interactions
remain live.

- `New(t, Spec{Name, Sources, Input, Run}).Output(t)` records stdout or a caller's
  captured result. `Run` owns args, stdin, environment and any side-effect capture.
- `Reference.Call(t, exactInput, run)` records a map of exact input/output byte
  pairs. Different inputs cannot accidentally reuse an expected answer.
- `NewLines(t, repositoryRelativeScript).Call(t, input, nil)` supports existing
  JSON-lines oracles. A live process starts lazily only in explicit reference mode
  and closes stdin, waits and reaps on cleanup. All original comparisons remain.
- For dynamic loopback addresses, callers may pass a keyInput containing a token
  in place of **only that fixture's allocated port**. Assertions still compare
  the actual Go request/side effects; never normalize arbitrary paths or values.

Normal `go test` starts no Python for these references. Every golden hashes its
backend sources, oracle script, consuming package's test source and declared
input, plus the venv's sorted dist-info METADATA/RECORD/WHEEL files. Dependency
checking reads files directly; it does not execute Python or pip. `NewLines`
conservatively fingerprints all backend Python sources (including transitive
imports). A changed hash fails with `Python 参考已变化，请重新录制`.

Run on the test server with the same interpreter/dependencies as the original
reference (`CONFIGFLOW_DIFF_PYTHON`, default `~/cf-test/.venv/bin/python`):

```sh
# Read-only live equivalence check. Diff must be zero before handoff.
PYREF_MODE=check go test -count=1 -v ./internal/<package>
# Explicitly accept a reviewed change / first recording. Failed tests never write.
PYREF_MODE=record PYREF_ACCEPT=1 go test -count=1 -v ./internal/<package>
```

`check` prints differing exact inputs and old/new output bytes and fails, including
metadata-only or removed-input changes. It never writes. `record` without accept
also never writes. New recordings require the same explicit acceptance. Updates
are atomic; goldens are normal tracked `testdata/pyref/*.json` files. Never set the
accept flag in gate/CI. Record, review the git diff, and run `check` once more.

## Process audit and explicit live exceptions

`go run ./tools/pyref` audits A scopes by default. `-scopes` selects migrated
packages; it also inspects all Go sources in their children, including ordinary
`.go` test helpers. Every process constructor (including
aliased imports and calls in callbacks) and old `yamlxtest.NewOracle` call must
have an exact file/function/callee registration in `live-exceptions-a.json` or
`live-exceptions-b.json`. `count` defaults to one; an added call or stale entry
fails the audit. Each registration states its guard, kind and reason. No whole
package exemption exists. Product scripts, Go self-process tests and explicit
cross-language integration tests are kept and identified separately.

This AST inventory complements a runtime exec trace of **ordinary** tests. A
registration alone is not proof of a recording guard: ordinary runs must show no
Python oracle exec, and live modes must reap all child processes. `NewLines`
transcripts use gzip-compressed JSON; one-shot `Spec` may also set `Compress`.

The audit scans constructor references in package initializers and function
values as well as direct calls. Dot imports of process packages are rejected;
literal `exec.Cmd{...}` construction is inventoried too. Runtime exec tracing
still verifies the ordinary path; the registry is not a reflection/shell proof.

## Cache-safe daily tests and gate integration

Go's cache does not watch files outside the module root (`backend/` and the
venv). Use the shared native wrapper for cached tests:

```sh
cd server
# Backend+venv SHA becomes a logged environment input of every reference test.
go run ./tools/pyref -test -- -race ./internal/mosdns
```

The wrapper propagates stdin/stdout/stderr and the exact `go test` exit code.
It starts Go, never Python. For gate.sh or another driver, the equivalent is:

```sh
export PYREF_INPUT_FINGERPRINT="$(go run ./tools/pyref -fingerprint)"
go test -race -p 6 ./...
```

Compute the export afresh for every invocation/batch; never store a fixed value.
The actual test also verifies it against the native fingerprint and retains all
per-golden source/input/dependency checks. Bare cache-enabled `go test` without
the variable fails with instructions. An explicitly supplied positive `-count`
disables Go caching and is allowed without a fingerprint (including the current
gate's `-count=1`). This branch does **not** edit gate.sh.

`measure-a.sh BASELINE_REPOSITORY_ROOT OUTPUT_DIRECTORY` runs the exact same
uncached race command sequentially on A's eight packages in the baseline and
current trees. It records JSON timings and host/load/start/end metadata. Run only
on a test server, after the independently owned N7-3 fixture optimization has
been integrated into the baseline. It does not run full gate/proddiff.
