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
