# O3-1 — in-process lock primitives

Status: implemented, final acceptance pending, branch `go-o3-1`. No consumers, sub-agents or pushes.

## Brief

Implement `design/o3-concurrency.md` §2.2: cancellable/limited Mutex and RWMutex, keyed cleanup, runtime
L1–L3 ordering diagnostics, injected-spawner Flight. Conceptual L4/L5 stay sync.Mutex; no file locks or
file changes. Existing callers migrate in O3-6; lifecycle integrates Flight's Spawner in O3-2.

## Done / in progress

- Mutex, Keyed, RWMutex and order checker implemented; .63 gofmt/vet passed and lockx tests passed
  (`ok configflow/internal/lockx 0.121s`). Negative checker cases run in a child process; main tests assert
  Violations() == 0 without resetting shared production state.
- Primitive commit `64bbb2db`; focused .63 race passed (`2.284s`). Flight's first race -count=3 passed
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

1. Rebase current go, then final .32 gofmt/vet, `go test -race -count=3 ./internal/lockx/`, full suite, proddiff.
2. Record results, rebase go, clean worktree; no push.

## Commands

`RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/lockx -- bash -lc 'cd server && gofmt -w internal/lockx && test -z "$(gofmt -l internal/lockx)" && go vet ./... && go test -count=1 ./internal/lockx/'`
