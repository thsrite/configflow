# O3-4 — periodic owner migration

Status: **verified** — joint Opus O3 review, zero open findings ([review-o3.md](../review-o3.md)).
Author status before review: implementation complete; author acceptance passed on .32. Ready for coordinator verification
and the planned joint Opus review of O3-1–O3-4.
Branch: go-o3-4. No sub-agents, pushes or new dependencies.
Both .32 and .64 are accepted servers; this task pins .32 for every test/build/vet/format command.

## Implementation

- refresh: Periodic30/30, L1 Keyed shared by scheduled/manual work, RunNow uses tracked Spawn,
  rows read Held. Resource lookup still precedes busy checking and the Chinese409 message is unchanged.
  A round claims due keys, joins at most3 workers and releases unstarted claims when stopping.
- discovery: Periodic120/600, fresh scope per profile, per-profile failures continue to the next profile.
  RunAutoTask/automatic rounds and synchronous startJob retain Runtime shutdown cancellation.
- OxiDNS rankings: Periodic60/60. Slot pauses use ClockFrom, and stopping prevents new Agent/slot work.
  Business timestamps and existing owner clock seams remain unchanged.
- metrics: Periodic30/30+Signal. OnStart resets startedAt/attemptedAt/stopping once per ModeRun activation,
  before the first wait. Manual/proddiff mode does not initialize a schedule. Interval edits wake and reread
  the configured due interval. Existing history Startup and first/final shutdown saves remain wired.
- Sub-Store: Periodic0/5 only when builtin exists; Round calls Tick. Persisted state.json monotonic time
  remains in the owner clock's kernel-uptime domain. Prune Startup remains unconditional.
- lifecycle: ClockFrom, shutdown-aware Detach, Periodic.OnStart and Signal.Clear. Detach preserves values
  and ignores disconnects while respecting the originating Runtime's grace−2s cancellation. Clear is a
  nonblocking receive of at most one pending notification; its only production consumers are metrics
  OnStart and ResetFlushState. Notify and normal waiting semantics are unchanged.

All interface additions/boundaries are coordinator-approved and recorded under Clarified in O3-4 in
[the design](../../design/o3-concurrency.md). Signal.Clear was explicitly approved after a fake-clock
regression demonstrated that a previous shutdown wake shortened the next generation's first30s wait.
The saved metrics draft has been applied; no pending API decision remains.

Remaining O3 work is outside this step: tasks.StartBackground/metrics manual/discovery async jobs(O3-5),
Sub-Store work/control leases and polling plus other handler-level detach moves(O3-6).
No production environment/flag switch was added to disable loops.

## Tests and findings

Fake-clock tests cover actual owner activation, first delay, pauses after round completion, metrics wake,
restart reset and Manual no-activation/next-run behavior. Instance ownership regressions drive the real
refresh/discovery/ranking rounds. Further assertions check the scheduled refresh receipt, discovery's
prerequisite receipt and retired-Agent ranking cleanup, not only callback execution.

Refresh regressions cover <=3 joined workers, stopping admission/release, shared running rows, lookup-before-
busy, request-disconnect completion, cancellation reaching download I/O and release after rejected Spawn.
Discovery's synchronous job test checks a fresh scope and cancellation reaching I/O. Lifecycle tests cover
old-generation cancellation, no per-detach watcher leak, OnStart panic cleanup and startup registration.
Signal tests check empty/pending/coalesced Clear and effective subsequent Notify.

No unresolved product finding or external API/message difference. A new metrics test originally passed
Background to its independent Runtime shutdown hook, opening a default test repository in the package
folder. The fixture now passes its bound repository context and asserts the shutdown receipt's location;
synthetic artifacts were removed and no package artifacts remain. No product fallback contract changed.

Focused race count3 before the final batch passed: lifecycle7.376s/lifecycletest1.024s/metrics19.219s;
the strengthened metrics timing/repository assertions passed metrics1.560s/lifecycle1.032s.

## Final acceptance

Pinned runner: `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc '…'`.
These commands run sequentially in that shell, with failure preserving the failing log and exit status:

```sh
set -euo pipefail
cd server
test -z "$(gofmt -l internal/ tools/ cmd/)"
go vet ./...
go test -race -p=4 -timeout=30m -count=1 ./internal/lifecycle/... ./internal/lockx/... ./internal/refresh/... ./internal/discovery/... ./internal/agents/monitor/... ./internal/agents/metrics/... ./internal/substore/... ./internal/httpx/... ./cmd/... > /tmp/o34-final-race.log 2>&1
go test -p=4 -timeout=30m -count=1 ./... > /tmp/o34-final-full.log 2>&1
for tag in configflow_proddiff configflow_difftest; do
 go test -tags="$tag" -count=1 ./internal/lifecycle/...
 go test -tags="$tag" -run "^$" ./cmd/configflow
done
for pkg in $(go list ./... | rg "/difftest$"); do
 CONFIGFLOW_DIFF=1 go test -tags=configflow_difftest -p=4 -count=1 -timeout=30m "$pkg"
done > /tmp/o34-final-difftest.log 2>&1
cd ..
server/scripts/proddiff.sh > /tmp/o34-final-proddiff.log 2>&1
```

Current output: GOFMT_PASS, VET_PASS, RACE_PASS, FULL_PASS; both TAG_PASS markers.
Final race: lifecycle3.115s/lifecycletest1.023s, lockx3.432s, refresh252.626s, discovery9.070s,
monitor199.662s, metrics7.878s, substore8.811s, httpx33.585s, cmd/configflow105.981s; nested packages pass.
Full suite exited0. Both tags build main and pass lifecycle tests (0.047s/0.049s, helper0.004s).
All10 enabled difftest packages passed: agents119.798s, subscriptions74.262s, discovery2.140s,
health13.560s, httpx1.340s, mosdns1.514s, refresh3.318s, subs1.647s, substore6.004s,
webdav17.544s. DIFFTEST_PASS.
Proddiff: all23 self-checks PASS,2831 cases, coverage_regressions0, unmapped_routes0,
new=0/stale=0; final runner exited0 with ALL_PASS. go_disabled_background4 is expected because
this image has no builtin Sub-Store, so sub-store-idle is not registered. No exemption was added.

The final test-only instance-boundary assertions are checked afterward with:

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/refresh/refresh_test.go --back server/internal/discovery/review_c37_test.go --back server/internal/agents/monitor/a07_review_test.go -- bash -lc 'cd server && gofmt -w internal/refresh/refresh_test.go internal/discovery/review_c37_test.go internal/agents/monitor/a07_review_test.go && test -z "$(gofmt -l internal/ tools/ cmd/)" && go vet ./... && go test -race -count=3 -timeout=5m -run "TestLoopRunsUnderTheInstanceLock|TestReviewAutoLoopRunsUnderInstanceLock|TestReviewRankingLoopRunsUnderInstanceLock" ./internal/refresh ./internal/discovery ./internal/agents/monitor'
```

Before the batch, rebase onto go@0b367ae5 completed without conflicts (N8-3/A11 included).
Implementation commits after that rebase:
- b96e0a3b: framework clock/detach/activation.
- b24fd2d5: refresh/discovery/ranking migrations.
- 8dc55ecf: conditional Sub-Store scheduling.
- 405c2ec1: activation panic and disconnect regressions.
- c097c97e: earlier partial acceptance record (superseded by this complete report).
- d19d1a94: metrics migration and approved Signal.Clear.

The final test-only boundary assertions passed race count3: refresh2.173s, discovery2.044s,
monitor2.286s; whole-tree gofmt-l stayed empty and go vet ./... passed afterward.
Only tests/report changed after the complete production-code gate. No package-local test artifacts remain.
Final rebase/clean-worktree confirmation and final commit id are included in the hand-back message.
There is no remaining O3-4 work; O3-5/O3-6 scope listed above is intentionally left to those steps.
