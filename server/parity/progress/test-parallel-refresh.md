# refresh test performance

Complete; combined Chinese hand-back and evidence: [test-parallel-audit.md](test-parallel-audit.md).
Test-only changes on go-test-parallel-1; no pushes or subagents.

## Changes and preserved coverage

- The original replay remains sequential because Env is process-wide. Pure/clock-only
  rows do not initialize unused DBs; one normally initialized default database/directory
  image is copied per stateful row and opened normally. No writable files, Repository
  objects or scopes are shared between rows. All seeds and before/after assertions remain.
- Independent pure-function tests run in parallel. The original Test tree is retained:
  41 top / 7 child passes, zero fail/skip, exact names/actions in every timing pair.
- Recorded replay: 3150 visited, the same 5 pre-existing collaborator exclusions,
  3145 compared. Reference: 1210 outer cases, including 8 scenarios / 43 output comparisons.
  Runtime counters and all existing acceptance classifications match before/after.

Intentionally sequential: setup users (Env, task claims/lifecycle), FetchUserinfo HTTP
Env tests, ReferenceCorpus/SourceCacheMetadata clock users, route helpers (task slots/Env).
Reasons are documented in helpers/tests. No production timings or unit-test wait bounds changed.

## Results

Formal .64 three-pair medians: package wall 218.721 -> 85.995 s; CPU 223.090 -> 84.680 s.
Race count=3: .32 354.053 s / .64 259.024 s (three runs combined).
DownloadCachePath wrong-prefix mutant was detected by TestReferenceCorpus; control passed.
Full gate on base 0f943623 passed. After final rebase to 93fdcc62, target race and refresh
difftest passed (3.929 s). The older 963-case progress note was stale; actual corpus is 1210.

## Baseline slowest 20 tests

| Test | Seconds |
|---|---:|
| TestRecordedPythonSuite | 257.69 |
| TestReferenceCorpus | 0.69 |
| TestReviewCancelledRequestContextFinishesTheRefresh | 0.37 |
| TestUnsafeSourceYAMLNeverWritesCaches | 0.35 |
| TestUnsafeDownloadedCacheRejectedBeforeReuse | 0.32 |
| TestAutoDeliveryReadsCacheWithoutUpstreamOrExtendingAge | 0.31 |
| TestIntervalEditValidatesOnTheRealRows | 0.30 |
| TestExpiredCacheIsServedAndTheScheduledTaskRefreshesIt | 0.23 |
| TestFailedScheduledRefreshKeepsNodesAndRetriesSooner | 0.22 |
| TestRunNowClaimsTheTask | 0.21 |
| TestDryPreviewsShareDownloadAcrossRestartWithoutChangingValidatedNodes | 0.20 |
| TestDeviceReadsUseTheCacheAndOnlyAMissingCacheDownloadsNow | 0.19 |
| TestSchedulerSkipsDisabledSourcesAndSourcesWithoutAutomaticUpdates | 0.19 |
| TestRuleSourceDueOnFirstRoundWithoutCache | 0.19 |
| TestLoopRunsUnderTheInstanceLock | 0.19 |
| TestFailedRuleRefreshSurvivesAReloadWithReasonAndRetainedCache | 0.18 |
| TestRoundJoinsThreeWorkersAndStopsAdmission | 0.17 |
| TestRecordCountsRunsFailuresAndSkips | 0.16 |
| TestDeviceReadOfAFailedSourceIsStale | 0.16 |
| TestManualOnlyWithoutCacheExplainsActionWithoutFetching | 0.16 |

## Commands and evidence

Initial exploratory .32 baseline (each package separately):

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'mkdir -p /tmp/go-test-parallel-1; cd server; for pkg in health refresh oxidns; do go test -race -count=1 -json ./internal/$pkg > /tmp/go-test-parallel-1/before-$pkg.json || exit; tail -1 /tmp/go-test-parallel-1/before-$pkg.json; done'
```

Final paired timing, exact coverage counters, every load snapshot, mutation patches /
assertion-failure outputs, stability commands and gate-base separation are recorded in
test-parallel-audit.md and local ignored .ci-results/test-parallel-1-* artifacts.
All tests/builds/vet/format checks ran on test servers via rt, never on the Mac.
