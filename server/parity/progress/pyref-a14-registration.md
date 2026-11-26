# Batch-5 process registration follow-up

Branch `go-fix-pyref-a14`, based on frozen batch-5 `627f09ac8`.
Register two A-group process sites, then five additional B-group sites exposed
when the repository audit advanced past A. The coordinator explicitly authorized
the B additions and confirmed that its author would not edit that registry in
parallel. All entries use existing scopes and categories with exact constructor
count one. No audit rule, scope, product implementation, test assertion, execution
guard or golden is changed.

## Classifications

| Registry ID | File/function | Kind | Boundary and guard |
|---|---|---|---|
| A-GO-4 | deploy/crash_test.go:TestPublishProcessRecovery | go-subprocess | `os.Args[0]`, exact `TestPublishChild` filter and A14 child environment; SIGTERM/SIGKILL across preparing/uploading/accepted; inspect durable journal and recovery, Wait/reap child |
| A-DIFF-8 | deploy/publish_oracle_test.go:TestPythonPublishTranscripts | differential | Always-on, eleven scenarios, independent loopback fake Agents and temporary repositories; real Python publication/poll/activation/rollback/recovery |
| B-029 | agents/metrics/native_files_test.go:TestPythonLoadsAndSavesNativeNullFilesButLegacyMathFails | cross-language | Always-on synthetic Go-written metric files; Python loads/flushes them, checks None arithmetic rejection, then Go reads actual Python-written JSONL |
| B-030 | agents/trafficstore/native_files_test.go:TestTrafficFilesInteroperateWithPython | cross-language | Always-on temporary Go→Python→Go traffic-file round trip, including unknown/null members |
| B-031 | repo/review_n85_test.go:TestReviewN85PythonFullStorageDifferential | cross-language | Explicit `CONFIGFLOW_PY` and `CONFIGFLOW_REPO`; Python/Go mutations compare every SQLite table, complete files and exported documents |
| B-032 | repo/sections_roundtrip_test.go:TestSectionWritesStoredRoundTrip | cross-language | Python audit callback requires both environment variables; Go section/row-preservation assertions always run; Python opens actual Go-written SQLite state |
| B-033 | repo/typed_roundtrip_test.go:TestTypedUpdateStoredRoundTrip | cross-language | Same explicit Python guard; native typed-update/generation/revision assertions always run; Python verifies actual committed document |

All paths in the table are relative to `server/internal/`. The existing A deploy
scope includes both A14 files; existing B agents/repo scopes include all five B
sites. Counts describe AST constructor sites, not runtime invocation counts.

The publishing test is not a deterministic stdout-only calculation: Python must
generate real fake-Agent HTTP calls so the test can compare request order,
authorization/content/deployment headers, uploaded manifest/file digests, durable
Agent state and receipt files. A static golden would omit that interaction. The
five B calls likewise prove real storage interoperability. They stay live, and
no testgolden recording wrapper or new skip is introduced. Registry guards
explicitly identify the always-on cases rather than inventing an environment
guard that does not exist in their tests.

## Validation

Fixed host `.32`; no full gate:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -count=1 -v ./tools/pyref/'
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc 'cd server && go test -race -count=1 -timeout=15m -v -run "^(TestPublishProcessRecovery|TestPythonPublishTranscripts)$" ./internal/deploy'
```

The first audit run (with only A's two registrations) passed A's twelve sites,
then correctly rejected the five unregistered B constructors. Existing bypass,
new/stale-registration and helper-source controls passed; the audit was not
relaxed to hide this failure.

Final checks passed on `.32`:

```text
pyref audit: 12 registered call sites, 6 scopes; no unregistered process constructors/live oracles
pyref audit: 33 registered call sites, 5 scopes; no unregistered process constructors/live oracles
--- PASS: TestAuditRepository (1.33s)
ok   configflow/tools/pyref  2.339s
--- PASS: TestPublishProcessRecovery (89.72s)
--- PASS: TestPythonPublishTranscripts (107.66s)
ok   configflow/internal/deploy  198.416s
```

Both actual repository scopes and all existing audit controls pass. Deploy
executes all six crash/recovery signal-stage combinations and all eleven real
Python publishing scenarios; none were skipped. The five additional B tests were
read to establish their live storage boundaries; no claim of rerunning their
author suites is made. Registry-only edits require no golden re-recording.

Logs are in `.32:~/.cache/pyref-a14-registration-627f09ac/`: `audit.log` is the
initial failure, `audit-final.log` is the passing complete audit, and `deploy.log`
is the passing targeted deploy run. `git diff --check` passes. No Go files or
audit implementation changed, and no full gate ran.
