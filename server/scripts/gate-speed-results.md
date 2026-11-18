# Gate speed acceptance

`go-gate-speed` is based on `go@5264ba80e`, rebased to `go@e0e9d9d45` (the
additional upstream changes are handoff/docs). Only `server/scripts/` is changed.
No product code, crosscheck repair, pyref implementation or cf-gate wrapper is
carried by this branch. No push.

## Result

All three core calls passed on **192.168.0.64**, using one unchanged integration
tree, `-p 6`, the same three 60s fuzz targets, and a genuine successful-full stamp.

| Call | Mode | Wall seconds | Race result cache hits | Result |
|---|---|---:|---:|---|
| first-full | `GATE_FULL=1`, clean test results then populate | 1047.89 | 0/100 | PASS |
| second-cache | `GATE_FULL=0`, first-run successful stamp | 68.07 | 100/100 | PASS |
| forced-full | `GATE_FULL=1`, clean again then populate | 995.24 | 0/100 | PASS |

Second-cache is **93.50% less wall time / 15.39x faster** than first-full, including
fresh lifecycle/tag checks and the same 60s fuzz runs. No-test-file packages are
excluded from the denominator. The complete list is in
[gate-speed-cache-packages.txt](gate-speed-cache-packages.txt); the gate also prints
it and writes `race-cached.txt`. Some core packages have optional live tests;
cache hits do not claim those live branches ran in core. Compare covers the
required live boundaries separately, always fresh.

Each core completed formatting, whole-repository vet, both tag builds/scoped
vet/lifecycle tests, full race package selection and all three fuzz targets.
All nine fuzz executions passed (~61s each, concurrent within each call):
`FuzzNativeRoundTrip`, `FuzzNativeNoPanic`, `FuzzClientImport`.

Compare: still running at the time of this checkpoint. Final acceptance requires
live proddiff, all 16 difftest packages, then the 23-entry synthetic-live manifest
with no missing/skip event. No unrelated heavy task is started during acceptance.

## Exact setup and commands

Temporary integration branch `go-gate-speed-check@33f69dec2`, directory
`/tmp/go-gate-speed-check`: final script implementation + completed speed-a
`b2cf3e6b4` + crosscheck repair `f01d464ef`. Main submission's executable scripts
were diffed against this tree: no differences. Only docs changed afterwards.
Neither dependency is imported into the submitted branch.

Run through rt with `RT_HOST=sunbroke@192.168.0.64`, from this integration tree.
The remote command runs in `server/`:

```sh
for task_mode in retry-full retry-cache retry-forced; do
  task_logs="$HOME/rt/gate-speed-accept/$task_mode"
  mkdir -p "$task_logs"
  task_full=1
  if [ "$task_mode" = retry-cache ]; then task_full=0; fi
  /usr/bin/time -p -o "$task_logs/time.txt" \
    env GATE_ONLY=core GATE_FULL=$task_full GATE_LOGS="$task_logs" \
    scripts/gate.sh -p 6 \
    FuzzNativeRoundTrip:./internal/yamlx \
    FuzzNativeNoPanic:./internal/yamlx \
    FuzzClientImport:./internal/importer >"$task_logs/console.log" 2>&1 || exit 1
done
```

All three final lines: `GATE STAGE PASS: core`. First/forced updated the real
`~/rt/.gate-last-full`; cache mode did not fabricate or refresh that timestamp.
Timestamp after forced-full: `1794971011`.

Compare is serialized by rt after core, on the same tree/host:

```sh
task_logs="$HOME/rt/gate-speed-accept/compare"
mkdir -p "$task_logs"
/usr/bin/time -p -o "$task_logs/time.txt" \
  env GATE_ONLY=compare GATE_LOGS="$task_logs" \
  scripts/gate.sh -p 6 >"$task_logs/console.log" 2>&1
```

## Script and evidence controls

On .64, after the final executable-script change:

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && bash -n scripts/gate.sh scripts/gate-test-exec.sh && PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-routing.py && PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-live.py'
```

Exit0: **10 routing/cache/umask controls + 6 execution-evidence controls**, all OK.
These verify forced/stale/future/missing full stamps, stage selection, failed-run
stamp protection, fingerprint propagation/failure/fallback, inherited umask and
private trace permissions, missing tests, skip/fail, and silent Python branches.

Real pyref smoke, in separate detached speed-a tree `6fad06fcf`:

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && export PYREF_INPUT_FINGERPRINT="$(go run ./tools/pyref -fingerprint)" && test -n "$PYREF_INPUT_FINGERPRINT" && printf "PYREF_INPUT_FINGERPRINT=%s\n" "$PYREF_INPUT_FINGERPRINT"'
```

Exit0. All real core calls also exported
`58bce4ae7134edf772a041c2d28b29d54a50d6972c564b2beb47ff3d008d7d65`.
Older trees without tools/pyref explicitly report the conditional fallback.

Concurrency confirmation inspected installed `go1.27.4-X:nodwarf5` sources:
`clean.go` advances expiration under `lockedfile.Edit`; `test.go` reads via
`lockedfile.Read` and treats expired entries as misses. It neither kills another
gate nor deletes its test binaries/work directory. Host-wide invalidation may
cause extra work and was approved. Raw excerpts on .64:
`~/rt/gate-speed-accept/go-cache-source.txt`. No failure was injected into
someone else's gate. See [gate.md](gate.md) for details and coverage boundaries.

## Initial failed trial, fixed and retained

The earlier `first-full/` trial failed after1809.50s (including initial queue).
It did not create a success stamp. Global `umask 077` in the first script version
incorrectly changed installer expected0644→0600 and store expected0640→0600.
Fixed by preserving inherited umask and restricting only the log directory to
0700 and trace file to0600. Two real shell/strace regression controls now guard
this. The temporary earlier speed-a snapshot also lacked TestEdgeCorpus's gzip;
the completed dependency contains it. No product or permission assertions were
relaxed. Targeted retries passed: installer31.209s, store1.009s, routersplit2.024s.

An early compare wrapper waiting on rt's project lock was cancelled before any
compare test began when the response-cache proposal was withdrawn. It is not an
acceptance result. Final proddiff is always live, with no new flag/dependency.
