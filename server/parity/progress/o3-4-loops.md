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
