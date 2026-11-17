# O3-5 — tracked detached work

Status: implementation in progress on go-o3-5 from go@8bf1b189; no sub-agents or pushes.
Coordinator approved the four interface/cancellation clarifications, recorded in design Clarified in O3-5.

Order: tasks/metrics manual/discovery jobs, WebDAV, upgrades, Sub-Store update, web listener first.
Clock extraction/Flight.DoChan/provider/matcher wait for the coordinator's O3 review merged notification;
then rebase go before modifying those parts. Do not edit lockx/lifecycle core before that notification.

First step: generic tasks use L1 Keyed + Spawn, retaining busy text and owner error reporting. Refused
admission/panic releases the key; Runtime supplies fresh scope and shutdown cancellation. Metrics manual
jobs already go through StartBackground and acquire these semantics without changing the HTTP contract.
First .32 run passed tasks/API tests, and exposed an existing metrics fixture that reused the
stopped default Runtime across repeated suites. The fixture now reconfigures only after old work
is idle; no production lifecycle core changes. Rerun gofmt/vet passed and race count3 passed:
tasks1.376s, metrics26.376s, api/tasks4.200s. New task regressions cover fresh scope, actual write
after disconnect, busy sharing, panic/refusal cleanup and shutdown cancellation. All heavy commands pinned to .32; .64 is also permitted.

Generic jobs commit: ef3f11f1. Discovery now uses tracked Spawn for async jobs, with the shared L1
admission boundary retained for sync/async and metadata published atomically. Async input retains only
repo/held lock, not request values or Scope; Runtime supplies lifetime and fresh scope. Rejection/panic
finalizes metadata and releases admission. Safe ProbeError text stays unchanged while internal causes
remain inspectable; ordinary sync errors return for caller handling, async failures log at their boundary.
.32 gofmt/vet pass; discovery full race count3 19.913s, nested package1.020s, API4.242s. Further
cause/panic-sentinel and cleanup regressions pass focused race count3 2.445s. C37 scope/weak-reference,
cross-profile privacy, metadata snapshot and bounded-history tests remain green.

Discovery commit: 470a0360. Read go-opus-review-o3:server/parity/review-o3.md section Rules for
O3-5..O3-8 and inspected F1/F2 context changes; awaiting its merge notification before touching core.
Entry guards now Detach and reject already-cancelled Runtime work before claiming new jobs.

WebDAV now uses tracked per-activation workers, injected Clock/Spawner, the production1s automatic-worker
factory and Startup activation. A shared upload semaphore survives all activations, and owner WaitIdle
counts only this owner's jobs across generations. Explicit Close is terminal; runtime hooks stop their
captured activation only, so a late old hook cannot stop a new generation. No request context is stored.
Snapshot/export, Run, Upload and HTTP request paths use Detach to preserve shutdown cancellation.

Tests: existing WebDAV suite first passed race1 3.504s, then all new lifetime tests passed race3 7.388s.
The loopback HTTP cancellation test keeps the production30s timeout; it passed with all WebDAV tests
race3 10.974s and enabled tagged difftest27.918s. Fake-clock startup tests use newAutomaticWorker
(the production1s debounce, not a replacement interval); cover restart, owner-scoped WaitIdle,
refusal/panic cleanup, cross-generation gate and late hooks. Last shared-context guard/factory rerun:
gofmt-l empty, vet ./... green, race3 tasks1.320s/discovery28.089s/metrics24.076s/webdav10.422s.

WebDAV commit: f9660f6b. Agent address follow now uses Spawn, Detach and Runtime Clock for its
production120s throttle; refused admission rolls the throttle back. Existing4s verification timeout
and messages remain unchanged. Shutdown cancellation reaches the Agent client without producing an
unreachable-address notice. Test hooks run accepted work through an isolated Runtime rather than inline
goroutines. Production120s/4s parameters are pinned by tests. .32 gofmt/vet passed; race count3
upgrades77.511s and api/agents416.954s; after adding tracked regressions upgrades113.664s passed.

Agent follow commit: 7df35b31. Sub-Store update now uses injected Spawner and L1 admission. Refusal,
panic and completion all release the update key; update state/messages remain compatible. HTTP and
ExecRunner retain job-aware cancellation and their existing timeouts; leases/polling remain O3-6.
Fixture jobs use isolated Runtime instances, native empty repositories and no production resources.
Regressions cover cancelled-request continuation, actual loopback HTTP cancellation, subprocess
cancellation, refused admission and production500KiB/15s-connect/120s-read bounds. .32 fmt/vet green;
race3 substore32.298s/settings44.573s, then fixture isolation race1 substore22.125s/settings15.724s;
final added cancellation regressions with full substore race3 passed55.844s.

Sub-Store commit: b34dd4b3. Listener Serve now runs as tracked jobs, ends on owner Close or Runtime
cancellation, and closes all newly opened sockets on partial Spawn refusal. Failed admission restores
the prior token and does not publish active state. Existing5s health timeout is pinned; health polling
and file-lock migration remain O3-6. .32 gofmt/vet green; full listener race count3 passed1.109s,
including actual sockets, owner Close, Runtime cancellation and partial admission rollback.

Prefix work is complete. Remaining: after O3 review merge notice, rebase go, shared Clock extraction,
Flight.DoChan/provider/matcher, then all final gates (all enabled difftests and proddiff included).

Prefix .64 complete preflight: gofmt-l empty, vet green, full `go test -race -p=4 -count=1
-timeout=30m ./...` exit0; cmd105.920s, health282.052s, installer330.594s, provider180.245s,
refresh270.129s, repo78.466s, oxidns115.417s, routers230.494s, yamlx12.267s. Difftest packages
in this run were not enabled; their enabled tagged runs remain a final gate.

Coordinator notified O3 review merged; rebased onto go@197c3c3f. Conflicts were only additive design
sections and Sub-Store messages.go; both sides retained. Current prefix commits after rebase:
f7ca67ec / ea0be396 / 181f46db / cabf9a24 / 28db8b31 / 06908f51 / ba7191ac.
New BindRequest and recorded Detach parent remain as integrated; no replacement with older semantics.

Shared Clock/Timer and real implementation now live in internal/clock. Lifecycle exports aliases;
lifecycletest imports only clock, allowing rules tests/owners to use the shared fake without a cycle.
.64 gofmt/vet pass; race count3 clock1.011s, lifecycle7.918s, lifecycletest1.015s, lockx8.495s.

Clock extraction commit: 647b8a97. Flight.DoChan now registers a capacity1 result channel on the same
underlying call as Do, with no waiter goroutine or cancellation of shared work. Finish publishes all
results on normal return, panic/Goexit and rejected Spawn. Tests combine Do/DoChan,1000 channel waiters,
an unread cancelled waiter's buffered result, shared flags, panic and a blocked refusal with joiners.
.64 fmt/vet green; race count3 lockx8.191s, lifecycle7.728s, lifecycletest1.007s.

Flight commit: aee0d01e. Provider rendering now uses Flight.DoChan and tracked Spawn; every waiter
selects its request cancellation, shared result and Runtime Clock timer. The production12s deadline
and8-slot pool are unchanged. Missing fallback keeps waiting on the same result; invalid fallback
keeps the existing read-error boundary without replacing ongoing work. Panic is handled once by
Flight; the pool token is deferred. Preparation detaches through lifecycle; local commits remain
uninterruptible. Fake-clock tests pin12s/8, fallback/no-cache/read-error reuse and cancellation;
the existing HTTP isolation tests retain the production ServeProvider entry. .64 gofmt/vet green;
provider full race count3 passed447.504s, including actual preparation I/O cancellation.

Provider commit: 0baedaef. Matcher cleanup uses the shared Clock and injected Spawner, with private
stop channels and generation checks. A nil Spawner does only synchronous eviction. Discovery injects
a non-nil Spawner, clears housekeeping in PhaseStopSignal, refuses new sweeps after Stopping and
refreshes the cache's Clock in the production autoPeriodic OnStart for each activation. No new
Startup task or phase was added. Tests pin the production wiring,1800s/60s generic limits and
discovery4/1024/300s limits, clear/refusal cleanup and cross-activation clocks. Fake-clock T0 shutdown
finishes without advancing grace. .64 fmt/vet passed; race3 rules13.924s, discovery15.178s;
focused activation/shutdown race3 passed1.024s.

Implementation is complete; remaining gates: final fmt/vet, related race after integration, full p4,
both tags, all10 enabled tagged difftest packages, proddiff, final rebase go and clean workspace.

Second composition rebase: go@d7c481c7 (A12/R07/C31) is now underneath the branch. Final gate setup
caught additive-message alignment and a same-size import whitespace edit missed by rsync's timestamp
copy-back; reformatted both on .64 and forced a later timestamp before copy-back. Full gofmt-l is
empty and vet green. No tests ran in the two early formatting-gate exits. Current integration has
an additional devicenotes differential: enumerate with tagged go list, do not hardcode10 packages.

Final related race on .64 passed: lifecycle3.254s/clock1.007s/lockx3.492s/tasks1.098s,
discovery7.936s/webdav3.311s/upgrades26.628s/metrics5.857s/substore14.556s/listener1.023s,
provider143.423s/rules7.758s/httpx29.545s/cmd117.178s, API tasks1.637s/settings8.349s/
agents103.723s/subscriptions2.367s. Full suite then exposed two newly integrated C31 fixture assumptions:
cancelled Surge waiters still expected200, and provider shutdown closed an isolated Runtime although
DefaultSpawner runs on the configured global Runtime. Updated the tests to the approved cancellable
wait contract (render still starts and finishes, live Surge waiter receives its own format) and actual
Configure/Start/Stop wiring (I/O cancelled, no publication). No lifecycle/Spawner API changed, and no
production assertion was dropped. All other full-suite packages passed. Focused API aggregations
race count3 now passes5.593s, with vet green. Repeat full suite and continue remaining gates.

Second full run passed the updated C31 tests and every other package except one listener fixture:
TestSavedPortSurvivesRestartAndUpdateMovesTheListener reported the old port still accepting. Its two
freePort calls released both ports before use, permitting equal ports (a valid Update no-op). Require
distinct ports and include both ports in failure output; retain the old-port rejection assertion.
No product listener logic changed. Full listener race3 plus100 repetitions of the move test passed
on both hosts: .64 1.033s/0.162s, .32 1.028s/0.096s. Repeat the complete full suite. Remaining gates
can run independently: full on .64, both tags/enabled differentials/proddiff on .32 (both valid hosts).

Acceptance on go@038b52ae + O3-5: .64 full suite exit0/FULL_PASS (cmd104.201s,
API aggregations0.128s, listener0.008s, provider10.290s, routers147.193s). On .32 both tags pass
lifecycle/clock/lockx tests and whole-tree test compilation. All12 enabled tagged difftests passed:
agents115.666s, aggregations41.257s, subscriptions130.962s, devicenotes1.178s, discovery3.983s,
health15.118s, httpx2.447s, mosdns3.040s, refresh5.199s, subs3.079s, substore10.344s, webdav27.929s.
Proddiff exit0:23 self-checks PASS,2831 cases/435 comparisons, coverage_regressions0,
unmapped_routes0,11 existing unique differences,new=0/stale=0. WebDAV worker Startup is visible as
webdav-worker-init, as intended. No baseline or exemption changed.

During the final gates go integrated N7-3b (d3124353), including the provider preparation chain.
Rebase next, retaining its new native-reader error handling and the O3-5 Detach changes, then repeat
composition gates so the latest source is covered rather than reporting old-base results as current.

N7-3b composition on d3124353: related chain race passed (provider136.730s, discovery8.677s,
rules8.665s, refresh197.650s, API aggregations2.763s/subscriptions2.276s, rulefetch18.408s,
repo68.894s); full suite COMPOSE_FULL_PASS. Both tags compile the whole tree and pass core tests.
All12 enabled difftests pass again (agents151.322s, aggregations26.209s, subscriptions86.911s,
devicenotes1.091s, discovery4.146s, health14.864s, httpx2.602s, mosdns3.369s, refresh5.842s,
subs2.987s, substore9.833s, webdav27.357s); final proddiff again new=0/stale=0.

Final design audit found the explicit §2.3 requirement to retire Loop/OnShutdown in O3-5. With zero
production callers remaining, removed both entry points, legacy Registry entries and legacy runner.
Default mode and automatic cross-generation restart tests now use Periodic; the two original named
restart regressions and Configure-old-work regression remain. PORTING's live API guide and the expired
transitional rule now reflect the real API. .64 fmt/vet green, core race3 passes lifecycle7.664s,
lifecycletest1.008s/clock1.008s/lockx8.035s. Repeat cmd/full/tags/final proddiff after this cleanup.
