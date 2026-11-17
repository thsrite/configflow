# O3-2 — lifecycle v2 and tagged differential modes

Status: implementation in progress, branch `go-o3-2` stacked on `go-o3-1`. No sub-agents or pushes.

## Scope / decisions

Implement design o3-concurrency §2.3: Clock/Signal/Registry, periodic and startup work, tracked detached jobs,
phased shutdown and restartable Runtime; retain Loop/OnShutdown/Start/Stop adapters. Wire Flight's Spawner.
Replace proddiff source rewriting with build-tag helpers/modes; production has no loop-disable switch.
Coordinator permits migrating subscription-cache-prune only to RegisterStartup; five periodic loops remain
legacy until O3-4. Preserve the concurrent agent's repeated Start/Stop regression when rebasing.

## Done / next

- Added task specifications, registry, signal and deterministic lifecycletest clock; .63 full vet and
  lifecycle tests pass (clock 0.003s). Options.Drain and tag-helper/one-shot scope approved by coordinator,
  recorded in design §2.3.
- Next: Runtime + scopes/drain/restart tests; default adapters and Flight wiring; tagged modes, main helpers,
  remove overlay builder and update proddiff metadata/docs. Final .32 race (lifecycle/lockx), full suite,
  proddiff new=0, enabled difftests, rebase go and clean worktree.
