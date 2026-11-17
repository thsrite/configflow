# Aggregation provider load follow-up

Branch `go-fix-agg-provider-load`, originally cut from `go@df6928ae`; rebased onto O3-5 `go@9a208c6c`. No subagents, no pushes, no production access. All tests through rt, synthetic delayed providers; snapshot private data remains on test servers.

## Findings and fix

The unmodified baseline 65c0e6d4 and current df6928ae each pass proddiff on .64 under load ~40. Restricting both to one CPU reproduces the provider marker mismatch on the baseline (new=1) and all three reported differences on current (new=3). Their provider owner/harness source is identical; no introduced provider semantic regression identified. Python returned last-good after 12 seconds while Go had already rendered fresh content. The differing Python body exactly matches an original snapshot provider artifact. A fresh reply correctly has no fallback header; the older artifact contains more nodes.

Fix only the comparison response policy: Flask launcher uses future timeout None; configflow_proddiff Go startup uses SetResponseSeconds(-1). Successful provider storage remains enabled; production default remains 12 seconds. No comparison, normalization, baseline, snapshot or generated content changes.

## Tests/status

- Before fix, real Python fresh-mode boundary tests fail on both provider paths (Go old body + last-good, Python fresh body + no marker).
- On .32, real Python provider response race passes 54.867s; tagged startup hook test passes 0.112s.
- Deterministic production barrier tests use the actual 12-second deadline; both public provider paths and fresh mode pass on .64 with race (55.758s), tagged startup hook test 0.113s.
- Added a strict comparator negative control for both reported fields. Final gate and load acceptance complete. Post-O3-5 .64 validation: provider real-Python race 55.085s, tagged hook 0.152s, O3 wait/cancel/fallback tests 1.030s; all pass.

O3-5 integration retains Flight.DoChan, Runtime-owned work, lifecycle Clock and cancellable waiter loop. Only fallback timer creation skips negative comparison budgets. The .64 single-CPU proddiff passes new=0 stale=0 (load 31.19 to 26.02, transport retries 1/0); all five successful aggregation reply pairs have equal fresh YAML and no marker, aggregation 2/2 and bundle 3/3.

Final **gate.sh -p 4 on .64 PASS**: gofmt/vet, both build tags, full race (87 tested packages), all 13 enabled difftests, FuzzProviderYAML 60s/26289 executions and FuzzAggregationDocument 60s/22350 executions. Concurrent proddiff at load 42.70 to 35.94 has new=0 stale=0, no coverage/unmapped regressions, self-check PASS; aggregation 2/2 and bundle 3/3, no partial. No pending work. Final commit/rebase/clean check, no push. Commands and output: [report](../fix-agg-provider-load.md).
