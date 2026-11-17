# health test performance

Complete; combined Chinese hand-back and evidence: [test-parallel-audit.md](test-parallel-audit.md).
Test-only changes on go-test-parallel-1; no pushes or subagents.

## Changes and preserved coverage

- Parallelize the two existing replay suites and independent pure/private-repository tests.
- Pure functions and private file-store rows do not initialize unused SQLite databases.
  Each replay reuses a read-only default repository; recorded system seeds remain private.
- The Env mutex covers installation, the production call and restoration. All original
  function calls, output/error/file/worker argv/stdin/timeout/env assertions remain.
- Original Test tree retained: 8 top-level passes, no subtests, no fail/skip. All three
  timing pairs have exact equal Test identities/actions and runtime function counters.
- Compared cases: recorded 2280, review 1015 (+2 existing Unicode waivers), reference
  788 (+2 existing Unicode waivers). No new exclusions.

Intentionally sequential (comments in code): ReferenceCorpus (Env/shared store files),
cancelled-request OS-process regression (Env), Agent delete-hook wiring (global hooks).
The real-process 3 x 0.3 s delay and 50 ms cancellation remain; lifecycle fake time cannot
drive ExecRunner's child process. Production timings and unit-test wait bounds unchanged.

## Results

Formal .64 three-pair medians: package wall 217.963 -> 6.324 s; CPU 220.130 -> 5.890 s.
Race count=3: .32 31.248 s / .64 19.324 s (three runs combined).
NormalizeConfig wrong-return-mode mutant was detected by TestReferenceCorpus; control passed.
Full gate on base 0f943623 passed; final rebase base 93fdcc62 and merged target race passed.

## Baseline ranking (all eight tests)

| Test | Seconds |
|---|---:|
| TestRecordedPythonSuite | 159.44 |
| TestReviewCorpus | 62.91 |
| TestReviewCancelledRequestFinishesTheLatencyTest | 1.03 |
| TestReferenceCorpus | 0.16 |
| TestWireRegistersTheAgentDeleteHooksInOrder | 0.07 |
| TestReviewStoreDeleteOfADirectoryFailsLikeOSRemove | 0.06 |
| TestRawAndCachedNodeCredentialsArePreserved | 0.00 |
| TestISOUTCOmitsZeroMicroseconds | 0.00 |

## Commands and evidence

Initial exploratory .32 baseline (each package separately):

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'mkdir -p /tmp/go-test-parallel-1; cd server; for pkg in health refresh oxidns; do go test -race -count=1 -json ./internal/$pkg > /tmp/go-test-parallel-1/before-$pkg.json || exit; tail -1 /tmp/go-test-parallel-1/before-$pkg.json; done'
```

Final paired timing, exact coverage counters, every load snapshot, mutation patches /
assertion-failure outputs, stability commands and gate-base separation are recorded in
test-parallel-audit.md and local ignored .ci-results/test-parallel-1-* artifacts.
All tests/builds/vet/format checks ran on test servers via rt, never on the Mac.
