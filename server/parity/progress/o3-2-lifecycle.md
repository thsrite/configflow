# O3-2 — lifecycle v2 and tagged differential modes

Status: ported; final rebased `.32` acceptance complete. Branch `go-o3-2`, stacked on
O3-1 and rebased onto integration `0efddadf` (includes config findings `885d1f7c`). No sub-agents or pushes.

## Scope and implementation

- Implement design o3-concurrency §2.3: Clock/Timer, coalescing Signal, ordered Registry,
  Periodic/Startup/ShutdownHook, restartable Runtime, ModeRun/ModeManual, tracked detached Spawn,
  fresh repository scopes, RequestSeen, Stopping, WaitIdle and phased shutdown.
- Inject HTTP `Options.Drain`: drain handlers and jobs/rounds concurrently, accept finishing-handler Spawn
  through grace−2s, cancel work then wait at most 0.5s, run after-drain/close hooks within total grace.
  Idle standalone runtimes finish early. Hooks/Drain must obey ctx; ignored ctx cannot be forcibly killed.
- Keep Loop/OnShutdown/Start/Stop adapters. Only subscription-cache-prune moved to synchronous Startup;
  the five periodic consumers remain for O3-4. DefaultSpawner and *Runtime implement lockx.Spawner;
  Flight integration proves request cancellation does not cancel shared work, but shutdown does.
- Configure replaces the idle/stopped default, retains Wire registrations when Registry is nil,
  and routes global Spawn to the replacement. Production defaults to ModeRun regardless of options/env;
  proddiff defaults to ModeManual. Coordinator's approved contracts are recorded in design §2.3.
- Build-tag mode/helper files replace source overlays completely; removed tools/proddiff/build.py.
  Tagged main helper exports actual routes; Startup completion exports Names/startups/ready.
  Startup metadata comes from Registry, with no task-name whitelist. Difftest-only JSONL events
  record paired round/job start/end. README describes both modes.
- Non-test lifecycle lines: 152 → 1020 (includes fake clock); no new dependency.

## Rebase and restart regressions

Rebased onto the integration containing the old restart fix and its test isolation change. Resolved
lifecycle.go in favour of v2, preserving the old tests' semantics using independent Registry/Runtime:

- `TestStartStopStartRunsTheLoopAgain`: the registered callback actually runs in both generations;
  it cannot retain callbacks from an earlier testing.T under `-count=3`.
- `TestRestartCannotOverlapAnUnfinishedOldRound`: an uncooperative old round retains its same-name
  gate after forced shutdown and restart, then releases it on completion (fake-clock driven).
- `TestLegacyRestartWaitsForOldLoop`: the Loop adapter also retains its gate across generations;
  a restarted generation starts it only after the old invocation exits.
- Default-runtime process test keeps Configure's instance-routing/registry checks and rejects
  replacement while old work remains, including after an additional Start→Stop cycle. Runtime counts
  pending work across all generations so replacement cannot orphan an earlier generation's job.

Other tests cover the Python P1–P10 first-delay/interval table, re-reading interval after round end,
wake, first-request gating, no overlap, panic recovery/continuation, scopes/detachment/Flight,
startup order/error continuation, shutdown phases/deadlines/late Spawn, early idle shutdown,
uncooperative-hook waiting bounds and goroutine cleanup. Default/global tests use subprocess isolation.

## Commits

O3-2 code (O3-1 remains the explicit stack dependency):

- `df11eb0d` task specifications and deterministic clock.
- `f8107f53` tracked jobs and restartable generations.
- `01e97ed4` default adapters and tagged differential builds; preserve incoming restart regression.
- `38154c7a` cross-generation periodic round gates and Configure protection.
- `94e371de` legacy-loop gates and ownership of unfinished work across further restarts.

## Final acceptance on .32

The pre-integration agents difftest differences were repaired by config findings on `go`; they are
not waived here. Every enabled difftest must pass after rebase.

First foreground command (no overlay/background job; stopped at the known full-suite probe failure):

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail
cd server
test -z "$(gofmt -l .)"
go vet ./...
go test -race -count=3 ./internal/lifecycle/... ./internal/lockx/...
go test -tags=configflow_proddiff -count=1 ./internal/lifecycle/... ./cmd/configflow
go test -tags=configflow_difftest -count=1 ./internal/lifecycle/... ./cmd/configflow
go test -count=1 ./...
mapfile -t o3_diff_packages < <(go list -tags=configflow_difftest ./... | rg "/difftest$")
CONFIGFLOW_DIFF=1 go test -tags=configflow_difftest -timeout=30m -count=1 "${o3_diff_packages[@]}"
cd ..
~/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py
server/scripts/proddiff.sh'
```

- gofmt/vet: pass, silent.
- Race count3: lifecycle 4.251s, lifecycletest clock 1.047s, lockx 8.491s; all pass.
- Proddiff tag: lifecycle 0.064s, clock 0.028s; cmd/configflow compiles.
- Difftest tag: lifecycle 0.086s, clock 0.051s; cmd/configflow compiles.
- First full-suite attempt hit `TestAdversarialImporterResourceBounds/wide`'s unchanged 3s child
  deadline (`timed_out=true`) while the server load average was 36.14. Coordinator confirms this is
  the known wall-clock probe issue owned by `go-fix-importer-bounds`, unrelated to O3-2; requested
  original-threshold focused recheck followed by `-p=2` full-suite rerun. No C35 code or probe changed.
- Unchanged-threshold focused wide probe: PASS, 0.874s, peak_RSS_KiB=72976, rejected=false.
- Reduced-concurrency full suite: PASS. Importer 1.283s, lifecycle 0.058s, clock 0.019s,
  lockx 0.278s, installer 217.199s, oxidns 136.092s; final package output
  `ok configflow/internal/yamlx 1.223s`, generator has no tests.
- All nine explicitly enabled `configflow_difftest` packages: PASS, no exemptions:
  agents 139.882s, subscriptions 132.281s, discovery 4.664s, health 14.728s,
  httpx 2.220s, mosdns 2.882s, refresh 5.455s, substore 11.161s, webdav 31.663s.
- Python proddiff tool tests: `Ran 31 tests in 0.035s`, `OK`.
- Tagged production-data proddiff: PASS, exit 0; all 23 self-checks PASS, 2831 cases /
  415 comparisons, coverage_regressions=0, unmapped_routes=0, baseline `new=0 stale=0`.
  Registry metadata confirms five suppressed loops and exactly one synchronous Startup,
  `subscription-cache-prune`. Existing integration baseline has 11 unique differences; none added.
  Isolation remained enabled; no snapshot contents are included in this report.
- The entire continuation foreground command exited 0. No difftest waiver and no comparator/business
  changes were made for this task's acceptance.

Coordinator-approved low-concurrency continuation after the known probe timeout:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail
cd server
go test -count=1 -run="^TestAdversarialImporterResourceBounds$/^wide$" -v ./internal/importer
go test -p=2 -timeout=30m -count=1 ./...
mapfile -t o3_diff_packages < <(go list -tags=configflow_difftest ./... | rg "/difftest$")
CONFIGFLOW_DIFF=1 go test -tags=configflow_difftest -p=2 -timeout=30m -count=1 "${o3_diff_packages[@]}"
cd ..
~/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py
server/scripts/proddiff.sh'
```

Independent review is not claimed; O3-2/O3-3 remain scheduled for Opus. No periodic-owner migration,
HTTP main Drain injection, or instance lock is part of O3-2 (O3-4/O3-3 respectively).

Final `git rebase go` includes only the later HANDOFF/WORKFLOW policy documentation beyond the tested
integration. Compared the tested code tree to the final tree under internal/cmd/scripts/tools: identical.
`git diff --check` passes; final worktree is clean; no push.
