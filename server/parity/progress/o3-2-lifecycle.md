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
- `b0a235c1`: first compiling spec/clock step. Runtime now implements periodic/manual rounds, fresh
  scopes, tracked detached Spawn/Flight, request gate, restartable generations and bounded phased
  shutdown with concurrent HTTP Drain. .63 full vet and lifecycle race pass (1.036s, fake clock 1.015s).
- Tests cover the Python timing table, post-round interval re-read, wake, no overlap, recover/continue,
  first-request timing, repeated starts/stops, manual mode, detached cancellation/scopes, Flight,
  phase/drain/deadline order, late-handler Spawn, idle early finish, uncooperative hook bounds and leaks.
- Default adapters now route registrations and Spawn to a configurable runtime, with reverse legacy
  shutdown hook order and repeated Start/Stop support. Configure rejects active replacement and retains
  registry on nil. DefaultSpawner and *Runtime satisfy lockx.Spawner.
- Tagged modes/main helpers replace source overlays; deleted tools/proddiff/build.py. Proddiff derives
  startup metadata from Registry, not a name whitelist. Only subscription-cache-prune moved to Startup.
  .63 full vet/race for lifecycle/lockx/substore/metrics passed; both proddiff/difftest tag builds/tests pass.
- Default integration tests run in child processes, including the required Configure→global Spawn
  instance-routing regression and production-mode no-disable behaviour. Difftest event pairs are tested.
- Final focused .63 recheck passed: lifecycle race 2.055s, clock 1.017s, proddiff-tag lifecycle 0.028s,
  cmd/configflow compiled, difftest-tag lifecycle 0.022s. Admission seals atomically at idle drain completion;
  hooks receive their generation's closed Stopping signal. Final .32 and rebase still pending.
- Next: Runtime + scopes/drain/restart tests; default adapters and Flight wiring; tagged modes, main helpers,
  remove overlay builder and update proddiff metadata/docs. Final .32 race (lifecycle/lockx), full suite,
  proddiff new=0, enabled difftests, rebase go and clean worktree.
