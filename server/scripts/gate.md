# Split merge gate

Run from a private worktree on a test server. `gate.sh` holds one machine-local
slot for its entire invocation (`GATE_SLOTS`, default 1); core and compare can
run on different hosts with independent slots. `GATE_LOGS` selects a persistent
log directory. Exit 0 means all selected work passed; it does not mean unselected
stages ran.

```sh
GATE_ONLY=core server/scripts/gate.sh -p 6 FuzzNativeRoundTrip:./internal/yamlx
GATE_ONLY=compare server/scripts/gate.sh -p 6
GATE_ONLY=agent server/scripts/gate.sh -p 6   # Agent module only
GATE_ONLY=frontend server/scripts/gate.sh      # frontend only
# No GATE_ONLY: core followed by compare, under one slot.
```

Core: gofmt, whole-repository vet, both lifecycle build modes (build, scoped vet,
lifecycle tests), whole-repository race, then the Agent module
(`backend/agents/go-agent`, a separate Go module: gofmt, vet, vet with the
container-only `halab nftlab` tags, race with the same `-p` and test cache;
"GATE FAIL: agent-module" / "gate: agent module PASS ..."), supplied fuzz targets
(60s by default). `GATE_ONLY=agent` runs only the Agent module and, like compare,
never refreshes the full-core stamp.
Compare: observer self-checks + proddiff, every `/difftest` package, discovered synthetic live,
then the frontend. All four groups run even if an earlier one fails; the final status
remains failed and logs retain results for the other groups.

Frontend (since 2026-11-22, in compare because it uses no Go caches or race
workers; `GATE_ONLY=frontend` runs it alone): the same commands as `cf-test
typecheck` and `cf-test frontend` — `npm run -s typecheck` (vue-tsc) and `npx
vitest run` in `frontend/`. Dependencies come from the cf-test cache:
`npm ci` runs once per `package-lock.json` hash into
`~/cf-test/node_modules/<hash>` (flock-guarded) and `frontend/node_modules`
is linked to it. Logs: `frontend-deps.log`, `frontend-typecheck.log`,
`frontend-vitest.log`. Prints `gate: frontend PASS (typecheck; vitest Tests …)`
or `GATE FAIL: frontend (typecheck|vitest, see <log>)` with the failing test
lines. Before this stage no gate ran the frontend (a stale vitest test stayed
red on `go` from 2026-11-19 to 2026-11-22).
Legacy `GATE_ONLY=proddiff` and `difftest` keep their narrow meaning.
proddiff reference (WP9): `PRODDIFF_MODE=python` (default until WP11) compares Flask + Python
generators with this tree; `PRODDIFF_MODE=go PRODDIFF_BASE_TREE=<tree with server/>` compares the
baseline tree's Go binary and generator with this tree (`parity/proddiff-baseline-go.txt`, backend-free
tools venv from `tools/requirements-tools.txt`). An unknown mode or a missing base tree exits 2 before
any work. See `tools/proddiff/README.md`.

## Fuzz temporary space and concurrency

Fuzz runs use a fresh `~/.cache/configflow-gate/fuzz.XXXXXX` directory on the
home disk, with separate `go/` and `tmp/` children exported as `GOTMPDIR` and
`TMPDIR`. This overrides a caller's `/tmp` settings for the fuzz stage only;
race/build/compare keep their existing environment. The private directory is
removed when the fuzz stage finishes, including a failing target. Persistent
`GATE_LOGS/fuzz-<name>.log` files remain available for diagnosis. Go's build cache
and failing-input corpus are not placed in this temporary directory.

At most **8 fuzz targets** run simultaneously by default. Set
`GATE_FUZZ_JOBS` to a positive integer to override that target limit. Every target
still runs; a failed target is reported after the other targets finish, and a
failed fuzz stage does not refresh the full-core timestamp. This limit is separate
from `GATE_FUZZ_PARALLEL` (workers per target, default 2) and `-p` (package build/test
parallelism). Each target's fuzz budget remains `GATE_FUZZTIME` (default 60s).

```sh
GATE_ONLY=core GATE_FUZZ_JOBS=4 server/scripts/gate.sh -p 6 FuzzNativeRoundTrip:./internal/yamlx
```

The routing controls use the real gate shell and fake Go commands to prove the
8-target default, overrides, disk paths even with caller `/tmp`, per-run isolation,
success/failure cleanup, invalid limits and retention of every target's execution.
They do not run a full gate or alter product tests.

## Race result cache

Core computes and exports `PYREF_INPUT_FINGERPRINT` via `go run ./tools/pyref
-fingerprint` when that entry exists. A failed/empty fingerprint fails core; an
older tree without the entry explicitly reports the fallback. The fingerprint
entry must remain paired with the tests that consume it when integrating speed-a.

`GATE_FULL=1`, a missing `~/rt/.gate-last-full`, an age of at least 24h, or a
future-dated stamp selects FULL. Full runs `go clean -testcache` immediately
before the same cache-enabled race command. This intentionally invalidates the
host-wide Go **test result** cache; build cache remains intact. It repopulates
results, unlike `-count=1`, so the next run can hit them. A concurrent gate can
lose cache hits and do more work; cache cleaning is not exclusion of other Go
processes. A shared timestamp describes a successful full core on that host;
Go's own keys still validate source, toolchain, flags, and tracked inputs per
package. Only a fully successful core (including supplied fuzz targets) refreshes
the timestamp. Compare-only never updates it.

`race.log` retains every package line. `race-cached.txt` and console output list
all `(cached)` packages and the hit count. Changes to tests, flags, Python input
fingerprint, or observed inputs legitimately reduce hits. Difftest, observer and
synthetic live tests use `-count=1`; proddiff always runs its oracle live. The proposed Python response cache was
withdrawn by the coordinator; no `--no-cache` interface or dependency is needed.

## Synthetic live: required execution evidence

`gate-live.py` invokes `go run ./tools/gatediscover` to build the manifest from
`go list -json ./...` active test files and Go ASTs. All `/difftest` tests remain
in the non-race diff group. Ordinary tests referencing CONFIGFLOW_DIFF,
CONFIGFLOW_DIFF_PYTHON, CONFIGFLOW_PY, CONFIGFLOW_REPO or CONFIGFLOW_YAML_PYTHON,
including transitive callers of helpers in other files, enter synthetic live
with race. Package-scope references conservatively select every test. Both
Go discovery and the Python runner reject an environment-bearing test package
missing from the compare manifest. New packages/tests need no whitelist edit.

Both groups set absolute CONFIGFLOW_PY and CONFIGFLOW_REPO,
CONFIGFLOW_DIFF_PYTHON, CONFIGFLOW_YAML_PYTHON, CONFIGFLOW_DIFF=1 and disable
bytecode writes. They unset CONFIGFLOW_SNAPSHOT, PYREF_MODE, PYREF_ACCEPT and
PYREF_INPUT_FINGERPRINT; fresh tests do not inherit a stale fingerprint or record
goldens. Three snapshot-only tests have explicit, recorded exclusions in the
Go discovery tool; private-snapshot subtests are excluded from synthetic live.
Missing interpreters or required imports fail the stage. `*-discovery.json`
records source files and exclusions; `*-manifest.json` records required tests;
`*-results.json` retains terminal per-test actions even after a test failure.
The current inventory and baseline coverage gaps are in
[gate-env-coverage.md](../parity/gate-env-coverage.md).

`diff.json` and `live.json` must contain run **and** pass for every required
entry, and no ordinary test/subtest skip/fail event. Invalid difftest fuzz seeds
may skip; they are not required ordinary tests. The live runner selects the
discovered top-level names with `-run`; diff retains its full package execution. `live-manifest.json` records the precise expectation. In
particular repo's `TestStoredRoundTrip/python-written-rows` must exist and pass.
The legacy WebDAV test has no Python-specific subtest/log event and ignores the
configured interpreter path. `gate-test-exec.sh` therefore runs only its test
binary under `strace -f -e execve`; the verifier requires a successful exec of
the actual inline `webdav3.client` TLS oracle, plus the test's run/pass. A
preflight import cannot satisfy this check. `strace` and the existing
`~/cf-test/.venv/bin/python` are mandatory. Only synthetic arguments enter this
mode-0600 trace. No production test or Python behavior is patched.

Private snapshot audits, A13 namespace proof, container/init tests, archive
producer/consumer conformance and shellcheck retain their separate prerequisites;
this synthetic stage does not claim to cover them.

Validator negative controls: run on a test server:

```sh
cd server
go test -race ./tools/gatediscover
go vet ./tools/gatediscover
PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-live.py
bash -n scripts/gate.sh scripts/gate-test-exec.sh
```

## Cache concurrency verification

The installed server toolchain (`go1.27.4-X:nodwarf5 linux/amd64`) was inspected:
`src/cmd/go/internal/clean/clean.go` uses `lockedfile.Edit(testexpire.txt)` and
only advances its nanosecond expiration value. `src/cmd/go/internal/test/test.go`
uses `lockedfile.Read` of that file and treats older entries as misses. Cleaning
neither removes test binaries nor mutates another test's working directory.
This confirms the intended concurrency effect: extra misses/re-execution, not a
corrupted result or cancellation of another gate. The shared-cache consequence
is deliberate and approved; slots still bound expensive gate execution.

Routing regression controls (fake go/npm/npx; the fixture includes an empty
Agent module and frontend) additionally cover the frontend stage (one `npm ci`
per lockfile, typecheck/vitest failures, compare and default include it, core
does not), missing/stale/future timestamps,
forced full, real shell stage selection, no timestamp on failure, fingerprint
propagation/failure/fallback, compare's live proddiff invocation, and invalid stages:

```sh
PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-routing.py
```

## Fuzz resource regression verification (2026-11-18)

On `.32`, the following targeted command passed (no full gate or product change):

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; bash -n scripts/gate.sh scripts/gate-test-exec.sh; PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-routing.py; PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-live.py'
```

Shell syntax passed; routing controls: **14 tests, OK (7.655s)**; live validator
controls: **6 tests, OK**. The default-cap case submits 41 fake fuzz targets:
all 41 start and finish, observed concurrency never exceeds 8, both temporary
environment paths are under the per-run home-cache directory, and that directory
is gone afterwards. Other cases prove an override of 2 with a failing target,
a limit of 1, distinct directories on repeated runs, unchanged per-target worker
settings, no leaked active commands, and rejection of zero/negative/noninteger
limits. The earlier core environment and full-run timestamp rules stay covered.
