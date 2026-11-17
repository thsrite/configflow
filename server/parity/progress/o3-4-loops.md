# O3-4 — periodic owner migration

Status: in progress, go-o3-4 from go@fb43c371 after O3-3 merge. No sub-agents/pushes.
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
