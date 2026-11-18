# Gate speed implementation / acceptance

Branch `go-gate-speed`, based on `go@5264ba80e`. Only `server/scripts/` is changed.
No product code, test consumer code, gate wrapper, Python response cache or pyref
implementation is carried by this branch. No push.

## Implemented

- `core`: formatting, vet, both build modes, all race packages, supplied fuzz.
- `compare`: live proddiff + observer checks, all difftest packages, exact synthetic
  live manifest. Default runs both. Each invocation holds one host-local slot.
- Cache-enabled race exports the external-input fingerprint when the pyref entry
  exists. Missing/stale/future full stamp or `GATE_FULL=1` cleans only test results,
  then executes the same race command and repopulates results. A successful core
  refreshes the stamp. Cached package names and count are retained separately.
- Synthetic live uses explicit interpreter/root/DIFF, unsets SNAPSHOT, uses fresh
  JSON execution, and requires all 23 manifest entries (21 roots, 2 Python-specific
  subtests) run/pass without any skip. WebDAV's silent branch additionally needs
  a successful traced exec of its real inline TLS oracle. No product-test patch.
- The proddiff response-cache proposal was withdrawn during this task; the final
  script always calls the existing live proddiff with no new flag/dependency.

See [gate.md](gate.md) for the exact stages, cache/concurrency rationale, evidence
files and separate opt-in coverage that is not claimed by synthetic live.

## Completed targeted checks (.64)

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && bash -n scripts/gate.sh scripts/gate-test-exec.sh && PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-routing.py && PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-live.py'
```

Final script version after cache-interface withdrawal: exit 0; 8 shell-routing /
cache-boundary controls + 6 JSON/branch negative controls, all OK. These do not
stand in for the real merge-test matrix.

Real pyref smoke (separate detached `go-test-speed-a@6fad06fcf` tree):

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && export PYREF_INPUT_FINGERPRINT="$(go run ./tools/pyref -fingerprint)" && test -n "$PYREF_INPUT_FINGERPRINT" && printf "PYREF_INPUT_FINGERPRINT=%s\n" "$PYREF_INPUT_FINGERPRINT"'
```

Exit 0, fingerprint `58bce4ae7134edf772a041c2d28b29d54a50d6972c564b2beb47ff3d008d7d65`.
The shell routing control also verifies that the exported value reaches the race
process and that failed/empty fingerprint generation cannot pass.

Concurrency confirmation used the installed `go1.27.4-X:nodwarf5` Go sources:
`clean.go` writes a monotonic expiration under `lockedfile.Edit`; `test.go` uses
`lockedfile.Read` and converts expired entries into misses. No other test process
or work directory is removed. Raw source excerpts retained on .64 at
`~/rt/gate-speed-accept/go-cache-source.txt`. No disruption was injected into
someone else's gate; the approved shared-cache expiry may cause extra work.

## Real acceptance matrix (in progress)

Temporary integration tree `/tmp/go-gate-speed-check`, branch `go-gate-speed-check`
at `898feb752`: script implementation `803878bb1` + speed-a `e908630ca` +
crosscheck repair `f01d464ef` cherry-picked as `898feb752`. Those dependency files
are not brought into the submitted branch. Core has no further behavior changes
in later script commits; final compare will use the script with the withdrawn
response-cache interface removed.

.64 logs: `~/rt/gate-speed-accept/first-full`, `second-cache`, `forced-full`.
Each call uses `GATE_ONLY=core`, `-p 6`, and these three 60s fuzz targets:
`FuzzNativeRoundTrip:./internal/yamlx`, `FuzzNativeNoPanic:./internal/yamlx`,
`FuzzClientImport:./internal/importer`. First and third explicitly set GATE_FULL=1;
second sets 0 and uses the genuine successful full timestamp.
`/usr/bin/time -p` records each wall time; first includes initial slot waiting.
The first slot was acquired before the 15-minute queue limit; formatting, vet,
both tags and real fingerprint export completed, and full race is in progress.

An early compare invocation queued on rt's project lock was cancelled before any
compare tests started when the response-cache proposal was withdrawn. It is not
counted as an acceptance run. After core matrix completion, replay the final
scripts into the temporary tree and run `GATE_ONLY=compare -p 6` once. It includes
the real crosscheck repair and must report 23/23 required entries with zero skip.

Pending: exact three core results/durations/cache-hit lists, final compare,
final rebase/clean check and hand-back to new coordinator
`f8801d39-a5bb-4c3f-bb7d-4b9eca9cc4b5`.

### First trial and correction

The initial real FULL attempt exited 1 (wall 1809.50s, including queue), so it did
**not** create the full-success stamp. Three package failures:

- installer DockerHA corpus expected mode 0644, got 0600;
- store TestCopyFileKeepsTimes expected 0640, got 0600;
- routersplit TestEdgeCorpus could not find its gzip golden in the earlier
  speed-a snapshot used by the temporary tree.

The first two exposed an implementation error in the gate: global `umask 077`
changed the test processes' permissions. Fixed by preserving inherited umask,
restricting only the log directory to 0700, and creating the WebDAV trace as 0600
inside a subshell. Two new real-shell/strace controls guard inherited 0022 and
private trace permissions. All **10 routing + 6 evidence** controls now pass.
The missing edge corpus exists in the completed speed-a branch; update only the
temporary dependency tree before restarting the unchanged-tree timing matrix.
No product code or assertions were changed to hide these failures.
