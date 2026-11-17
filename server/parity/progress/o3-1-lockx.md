# O3-1 — in-process lock primitives

Status: ported; final .32 acceptance passed, ready for independent review. Branch `go-o3-1`, based on
`go` at `e5903ffb`. No consumers, sub-agents or pushes.

## Brief

Implement `design/o3-concurrency.md` §2.2: cancellable/limited Mutex and RWMutex, keyed cleanup, runtime
L1–L3 ordering diagnostics, injected-spawner Flight. Conceptual L4/L5 stay sync.Mutex; no file locks or
file changes. Existing callers migrate in O3-6; lifecycle integrates Flight's Spawner in O3-2.

## Done / in progress

- Mutex, Keyed, RWMutex and order checker implemented; .63 gofmt/vet passed and lockx tests passed
  (`ok configflow/internal/lockx 0.121s`). Negative checker cases run in a child process; main tests assert
  Violations() == 0 without resetting shared production state.
- Commits after rebase: `11d187c8` primitives/checker, `9cae8b68` Flight and full concurrency coverage.
  Primitive .63 race passed (`2.284s`). Flight's first race -count=3 passed
  (`7.832s`); explicit RW nonpositive-limit and cross-type order regressions then passed (`7.992s`),
  together with full vet and silent gofmt.
- Flight uses injected Spawner only. Cancellation/deadlines detach with values retained; every caller
  waits independently; completion/rejection/panic cleans the entry and broadcasts. Tests cover 50-way
  coalescing, per-key independence, 100 cancelled waiters, ordinary error chains, rejection fan-out,
  panic wake-up/log-once/payload privacy, reuse, zero value and shutdown context forwarding.
- Tests cover increasing order, try under L3, released contexts, isolated-process negative order checks,
  immediate L1 contention, wait limits/context cancellation, 10k-key cleanup, shared/exclusive contention,
  writer cancellation/priority, concurrent stress, retained files and bounded waiter goroutines.
- Coordinator's clarifications are recorded in design §2.2, including explicit TryRLock/TryLock and
  Spawner/NewFlight contract. No implementation contract was changed without confirmation.

## Next

No O3-1 implementation work remains. Coordinator reviews/integrates this branch. O3-2 supplies the
lifecycle Spawner adapter; O3-6 migrates lock consumers and removes their per-resource flocks.

## Commands

`RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/lockx -- bash -lc 'cd server && gofmt -w internal/lockx && test -z "$(gofmt -l internal/lockx)" && go vet ./... && go test -count=1 ./internal/lockx/'`

Final .32 run (2026-11-17), exit **0**:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l .)" && go vet ./... && go test -race -count=3 ./internal/lockx/ && go test -count=1 ./... && cd .. && server/scripts/proddiff.sh'
```

- gofmt and vet: silent success.
- Race: `ok configflow/internal/lockx 8.067s` (three runs, main-process Violations == 0).
- Full suite: `ok configflow/internal/lockx 0.332s`; tail `ok configflow/internal/yamlx 2.653s`,
  `? configflow/tools/proddiff/generator [no test files]`. Installer 199.236s, oxidns 102.508s, no failures.
- Proddiff: all 23 self-checks PASS, 2831 cases, 415 comparisons, 10 existing unique baseline differences,
  coverage regressions 0, unmapped routes 0; final line **`baseline: new=0 stale=0`**.

## Decisions / handoff

- No new dependency and no existing Python feature/test removed or changed; this step adds infrastructure
  only. Ordinary worker errors retain their error chain without logging. Panic logs omit the payload.
- The checker counts each offending blocking attempt once, while logging each name pair once. Negative
  tests use isolated child processes so production diagnostic state needs no test reset API.
- File behaviour is deliberate: lockx performs no file I/O, creates no resource locks and leaves Python's
  leftovers untouched. Instance and repo continue to own file locks; no consumer is migrated here.
- Added non-test lines: **0 → 568** (doc 12, messages 7, order 80, Mutex 102, Keyed 87, RWMutex 159,
  Flight 121). Tests: locks 571 + Flight 337 = 908 lines. No goroutine is launched directly in production
  lockx; waits use channels/timers, Flight delegates to its tracked Spawner.
- `git diff --check` clean; final `git rebase go` reports up to date and the worktree is clean.
  No code changed after final acceptance. No open O3-1 functional findings; independent review pending,
  no push.
