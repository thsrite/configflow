# oxidns test performance

Complete; combined Chinese hand-back and evidence: [test-parallel-audit.md](test-parallel-audit.md).
Test-only changes on go-test-parallel-1; no pushes or subagents.

## Changes and preserved coverage

- Remove pre-existing race-only random sampling and its two constant-only build-tag files.
- One producer reads/decompresses each boundary file once; a bounded queue feeds seven
  joined internal workers. Storage and strict normalization also use joined workers.
  Worker errors return to the owner or use Errorf, never Fatal; no new Test nodes.
- Existing independent reference subtests run in parallel. Original generated/D2 totals
  remain checked after children finish; original storage threshold remains after join.
- Exact original test identities/actions: 40 top / 3025 child passes, zero fail/skip.
  Every pair compares the same 106966 boundary rows, 1442 strict normalizations and
  6304 storage documents; all other strict/generated/D2 counts are preserved.
- Normal/error/Chinese-message/argument-post-state comparisons and existing accepted
  difference classifications are unchanged. No corpus files or function selectors changed.

Intentionally sequential: TestReviewEmbeddedSchemas uses t.Setenv(CONFIGFLOW_ROOT).
The classicalIPLines hook is installed once in init and reads its private read-only repo.

## Results

Formal .64 equal-work three-pair medians: package wall 210.552 -> 132.558 s; CPU
828.090 -> 948.540 s; RSS 4882644 -> 2330044 KiB. Report CPU +14.5% alongside the
wall reduction (~37%) and RSS reduction (~52%), not as a CPU improvement.
The baseline disables only its existing sampling, so both sides execute full corpora.
The original sampled .32 147.434 s is historical, not an equal-work comparison.
Race count=3: .32 1225.492 s / .64 426.881 s (three complete runs each).
NormalizeSettings wrong-return-object mutant was detected by TestC05ReviewRecordedNormalization;
control passed. Full gate on 0f943623 and target race after final rebase to 93fdcc62 passed.

## Baseline slowest 20 tests (including subtests)

| Test | Seconds |
|---|---:|
| TestBoundaryGenerationRecorded | 142.80 |
| TestStorageRoundTrip | 129.66 |
| TestBoundaryCommonRecorded | 124.08 |
| TestBoundaryEditorRecorded | 120.81 |
| TestC27ReviewPublicBoundaries | 97.30 |
| TestC05ReviewRecordedNormalization | 83.79 |
| TestBoundaryCommonCorpus | 80.98 |
| TestBoundaryRoutingRecorded | 76.82 |
| TestBoundaryGenerationCorpus | 73.59 |
| TestRuleDNSPublicBoundary | 62.58 |
| TestBoundaryValidationRecorded | 51.22 |
| TestBoundaryRoutingCorpus | 51.09 |
| TestBoundaryEditorCorpus | 24.68 |
| TestBoundaryValidationCorpus | 20.54 |
| TestC05ReviewSettingsReferences | 20.34 |
| TestC22C26ReviewReferences | 4.88 |
| TestReviewSReferences | 1.68 |
| TestC27ReviewPublicBoundaries/apply_rule_dns_boundary/transition/native/False/disable/implicit | 1.43 |
| TestC27ReviewPublicBoundaries/apply_rule_dns_boundary/transition/native/False/policy-reject/proxy | 1.31 |
| TestRuleDNSPublicBoundary/apply_rule_dns_boundary/choice_native/False/refused | 1.24 |

Parent/child elapsed times overlap and must not be summed.

## Commands and evidence

Initial exploratory .32 baseline (each package separately):

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'mkdir -p /tmp/go-test-parallel-1; cd server; for pkg in health refresh oxidns; do go test -race -count=1 -json ./internal/$pkg > /tmp/go-test-parallel-1/before-$pkg.json || exit; tail -1 /tmp/go-test-parallel-1/before-$pkg.json; done'
```

Final paired timing, exact coverage counters, every load snapshot, mutation patches /
assertion-failure outputs, stability commands and gate-base separation are recorded in
test-parallel-audit.md and local ignored .ci-results/test-parallel-1-* artifacts.
All tests/builds/vet/format checks ran on test servers via rt, never on the Mac.
