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
