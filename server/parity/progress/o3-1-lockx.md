# O3-1 — in-process lock primitives

Status: implementation in progress, branch `go-o3-1`. No consumers, sub-agents or pushes.

## Brief

Implement `design/o3-concurrency.md` §2.2: cancellable/limited Mutex and RWMutex, keyed cleanup, runtime
L1–L3 ordering diagnostics, injected-spawner Flight. Conceptual L4/L5 stay sync.Mutex; no file locks or
file changes. Existing callers migrate in O3-6; lifecycle integrates Flight's Spawner in O3-2.

## Done / in progress

- Mutex, Keyed, RWMutex and order checker implemented; .63 gofmt/vet passed and lockx tests passed
  (`ok configflow/internal/lockx 0.121s`). Negative checker cases run in a child process; main tests assert
  Violations() == 0 without resetting shared production state.
- Tests cover increasing order, try under L3, released contexts, isolated-process negative order checks,
  immediate L1 contention, wait limits/context cancellation, 10k-key cleanup, shared/exclusive contention,
  writer cancellation/priority, concurrent stress, retained files and bounded waiter goroutines.
- Coordinator's clarifications are recorded in design §2.2, including explicit TryRLock/TryLock and
  Spawner/NewFlight contract. No implementation contract was changed without confirmation.

## Next

1. Verify/commit the compiling lock primitives on .63; add Flight and its concurrency/lifetime tests.
2. Final .32 gofmt/vet, `go test -race -count=3 ./internal/lockx/`, full suite, proddiff.
3. Record results, rebase go, clean worktree; no push.

## Commands

`RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/lockx -- bash -lc 'cd server && gofmt -w internal/lockx && test -z "$(gofmt -l internal/lockx)" && go vet ./... && go test -count=1 ./internal/lockx/'`
