# Split merge gate

Run from a private worktree on a test server. `gate.sh` holds one machine-local
slot for its entire invocation (`GATE_SLOTS`, default 1); core and compare can
run on different hosts with independent slots. `GATE_LOGS` selects a persistent
log directory. Exit 0 means all selected work passed; it does not mean unselected
stages ran.

```sh
GATE_ONLY=core server/scripts/gate.sh -p 6 FuzzNativeRoundTrip:./internal/yamlx
GATE_ONLY=compare server/scripts/gate.sh -p 6
# No GATE_ONLY: core followed by compare, under one slot.
```

Core: gofmt, whole-repository vet, both lifecycle build modes (build, scoped vet,
lifecycle tests), whole-repository race, supplied fuzz targets (60s by default).
Compare: observer self-checks + proddiff, every `/difftest` package, synthetic live.
Legacy `GATE_ONLY=proddiff` and `difftest` keep their narrow meaning.

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

`gate-live.py` contains the exact six-package/23-entry manifest (21 top-level
tests and two Python-specific subtests). It sets absolute CONFIGFLOW_PY and
CONFIGFLOW_REPO, CONFIGFLOW_DIFF_PYTHON, CONFIGFLOW_DIFF=1 and disables bytecode
writes. It unsets CONFIGFLOW_SNAPSHOT and excludes private-snapshot subtests.
Missing interpreters or required imports fail the stage; they never count as
successful skips. This stage uses race and fresh execution.

`live.json` must contain run **and** pass for each manifest entry, and no selected
skip/fail event. `live-manifest.json` records the precise expectation. In
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

Routing regression controls additionally cover missing/stale/future timestamps,
forced full, real shell stage selection, no timestamp on failure, fingerprint
propagation/failure/fallback, compare's live proddiff invocation, and invalid stages:

```sh
PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-routing.py
```
