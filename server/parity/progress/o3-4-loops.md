# O3-4 — periodic owner migration

Status: partial implementation, awaiting Signal reset interface decision; not ready for O3-4 merge.
go-o3-4 from go@fb43c371 after O3-3 merge. No sub-agents/pushes.
Only .32 is available; all tests/builds/formatting/vet run there.

Scope: refresh 30/30 (L1 Keyed, RunNow→Spawn, rows Held, ≤3 joined workers), discovery120/600,
ranking60/60, metrics30/30+Signal and per-generation activation state, conditional Sub-Store0/5.
Existing metrics Startup/two flush phases and prune Startup are already done.

Coordinator-approved API additions/boundaries are recorded in design Clarified in O3-4:
ClockFrom returns Runtime or real Clock; Periodic.OnStart runs before first wait in ModeRun only;
Detach drops request cancellation but preserves shutdown cancellation. Business clocks stay unchanged.
Only listed refresh/discovery detach paths change now; async jobs/general StartBackground remain
O3-5; Sub-Store leases/lock polling and remaining handler detach moves remain O3-6.

Framework step: ClockFrom/Detach/OnStart implemented with fake-clock/lifetime tests.
.32: lifecycle gofmt and vet pass; `go test -race -count=3 ./internal/lifecycle/...` passes
(7.286s/1.015s); `go vet ./...` passes; full suite is running before owner sync.
Next: owner migrations and tests,
complete .32 gates (all enabled difftests/proddiff), rebase go and clean hand-back.

Framework commit: 798f18d7. Its .32 full suite completed successfully (p=4, count=1).
Second step: refresh/discovery/ranking owners registered as Periodic with actual first/after-round delays.
Refresh owns an L1 Keyed shared by RunNow and due rounds; lookup precedes busy checking, rejection
releases the key, queued claims release on stopping, <=3 workers are joined. Rows read Held.
Refresh detached work and synchronous discovery jobs keep Runtime cancellation while ignoring disconnects.
Discovery preserves per-profile fresh scopes/failure continuation. Ranking slot pauses use ClockFrom;
business timestamps and existing seams remain unchanged.
.32 gofmt and `go vet ./...` pass. Focused migration regressions under `-race -count=3`:
refresh 1.509s, discovery 1.272s, monitor 1.246s. Full package race is deferred to the final run with
an adequate timeout: early broad count=3 run was interrupted to avoid repeating the large unchanged
SQLite corpora three times; a diagnostic 45s refresh run reached the unchanged recorded corpus and
expired while building a SQLite fixture, not while waiting on a lock. One new cancellation test's
receipt assertion was corrected: at the cancellation deadline storage may abort, while the tracked
worker must exit and release its key. Request cancellation still permits completion (existing C15 tests).
Next: metrics/Signal activation and conditional Sub-Store idle, followed by complete final gates.

Owner migration commit: f86d4de8. Sub-Store next step replaces Monitor with conditional Periodic0/5,
Round delegates to Tick; work/control leases, readiness polling and persisted Monotonic stay unchanged.
Fake-clock test executes Tick at0 and5s after round completion and checks state.json still uses1234.5
from the existing owner clock, independent of the Runtime wall clock. Builtin/external Wire registration
is tested with independent registries. .32 gofmt/vet pass; focused race count3 passes metrics1.173s,
substore1.043s. Metrics Manual no-activation check is also green. Metrics restart/wake finalization awaits
the coordinator's Signal.Clear API decision (pending old wake must not shorten the new first30s wait).

Broader .32 race completed with count1/p4/timeout30m: lifecycle3.093s, lifecycletest1.014s,
lockx3.369s, refresh130.141s, discovery9.501s, monitor252.189s, substore7.106s; their ordinary
(non-enabled) difftest packages also pass. The large recorded/SQLite corpora require normal timeouts.
Enabled configflow_difftest run is in progress (agents passed125.847s).
Pending Signal clarification has a concrete regression: .32 `go test -race -count=1 -timeout=30s
-run ^TestFlushPeriodicActivationWakeAndRestart$ ./internal/agents/metrics` fails at
`old wake interrupted new first delay` without pending-signal cleanup. No reset of sync.Once or
production timer disabling is used as a workaround. Coordinator session is currently quota-blocked;
the API question was also presented to the maintainer.

All10 enabled difftest packages passed on .32 with the draft metrics migration (no Signal.Clear
implemented yet): agents125.847s, subscriptions85.197s, discovery2.417s, health14.643s,
httpx1.633s, mosdns2.090s, refresh3.983s, subs1.647s, substore5.456s, webdav18.154s.
Sub-Store commit:9682b55b. Further regression checks (activation panic cleanup, real RunNow
completion after request disconnect) pass race count3: lifecycle1.017s/refresh1.207s;
whole-tree gofmt-l is empty and go vet ./... passes.

To leave a compiling, passing source tree while awaiting the required contract decision, the metrics
migration draft (including failing restart regression and passing manual/wake tests) is preserved as:
`$(git rev-parse --git-path o3-4-metrics-pending.patch)` (worktree git metadata, not pushed).
`git apply --check` succeeds. After approval, apply that patch, add Signal.Clear with a nonblocking
channel drain, use it in metrics OnStart and ResetFlushState, add a Signal regression, document the
approved extension and run metrics/lifecycle tests. Then repeat the complete final gate on the complete
O3-4 tree (race, both tags, full, all enabled difftests, proddiff), rebase go and hand back clean.
The current checked-in metrics loop remains legacy until that decision; this is not an O3-4 completion claim.
