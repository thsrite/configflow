# C31 — aggregations and device notes

Status **verified**, branch `go-review-c31`. Fresh adversarial reviewer, not the port author. Original port start `go-port-c31@1aa5c0cf`; latest `go` now contains the same original C31 runtime tree. No subagents, no pushes, no production access. Tests only through `rt`, synthetic data only; proddiff reads the isolated server snapshot.

## Complete

- All 11 route methods, immutable main/provider rendering, bounded eight-worker collection, staged cache updates, bundle validation/publication, shared device notes and real Python boundary comparisons are complete. Original port scope/test mapping and accepted Python fixes remain in PROGRESS-config and C31-HANDOFF.
- Independent review found and fixed **F1 high**: three handler `WithoutCancel` calls and the generator lost Runtime shutdown cancellation. Handlers now pass context unchanged; catalog owner APIs and Generate use lifecycle.Detach. Joined workers and prepared bundles check cancellation before publication. Four catalog write methods complete after browser cancellation and reject closed-Runtime work; dry and HTTP preview/provider tests prove Runtime cancellation reaches I/O and preserves last-good artifacts.
- Fixed **F2 low**: U+2028/U+2029 changed device-name revision digests. Preserve UTF-8 in the persisted digest without changing literal backslash-u text. Real Python six-operation corpus expanded; notes fuzz also checks stable revisions across JSON round trips.
- No stale private device-note copies in repo/oxidns/agents/routers/deploy. Shared helpers, auth/profile ordering, route registration, YAML D1/alias policy and native-only boundary rules checked.
- Final acceptance on **192.168.0.32**: gofmt empty, whole vet, changed-package/provider race, full ordinary suite (80 tested packages), all 12 CONFIGFLOW_DIFF packages, both 60s fuzz targets, proddiff harness 34 tests and snapshot differential pass. Notes fuzz 111701 execs; aggregation fuzz 74987 execs. Proddiff aggregation 2/2 and Agent bundle 3/3, neither has partial; new=0 stale=0, coverage_regressions=0, unmapped_routes=0, self_check=PASS.

## Hand-back

Zero open findings or remaining feature work. Review/runtime/tests/progress committed together with explicit paths; final `git rebase go`, no push. Chinese findings, exact rt commands/output, original failing-test evidence and validation limits: [../review-c31.md](../review-c31.md). Port author's historical handoff remains [../C31-HANDOFF.md](../C31-HANDOFF.md). Review commit ID is the branch HEAD reported to the coordinator.
