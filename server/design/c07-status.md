# C07 — status surfaces and write ownership

## Clarified in C07 (2026-11-18)

Coordinator decisions apply to tasks, notifications, logs, stats and native web
listeners. All four formerly wip task/notification routes register immediately.
The two list GETs return 503 plus Chinese text and `missing_slots` until every
real provider is wired; production never registers empty placeholder providers.
Ready task owners remain callable, known missing owners return 503 and unknown
keys return 400. Interval writes only query their corresponding ready provider;
a presentation failure after commit cannot turn the save into a 500.

Background notification refresh still records observed ready-task failures.
While any task slot is missing it cannot retire unseen receipts. List HEAD uses
the same projection and error status/headers without migrating or replacing the
store. Stats HEAD similarly computes the GET projection without sampling to
disk. Native binding uses BindSilent for run/read, with exact JSON keys and
finite JSON; the Python parser's ignored NaN/Infinity acceptance is explicitly
covered as a native-parser difference.

Write owners Detach and check their original Runtime before each owned commit.
Notification collection is outside the leaf lock; only read/merge/atomic rename
is inside it. Observation tickets and MarkRead ordering remain intact. Leaf
waits are cancellable and bounded. Optional stats sampling uses the repository's
existing profile-operation/file replacement locks, retaining Python's
last-commit-wins behavior without a leaf-to-repository lock inversion.

WebDAV run-now uses the existing C35 stored snapshot and upload owner. C37 manual
tasks retain work values/Runtime generation and receive a fresh repository scope.
Listener update keeps old sockets until real token health succeeds and state is
saved, checks shutdown before rename and active switch, and restores saved state
if cancelled between those commits. Native IPv4-only/reserved-port behavior is
retained. Only nginx validation, nginx-to-backend Docker reachability and old
nginx image-template replacement are retired.

## External owner boundaries

| Boundary | Current provider / remaining owner | Acceptance |
|---|---|---|
| subscription/rule-source rows and fetch | C15 refresh | real rows and interval HTTP/storage differential; run admission supplied owner; C15 owns fetch transport |
| domain-discovery rows/run | C37 discovery | manual task real Runtime value/generation regression |
| metrics-history row/run/interval | A04 metrics | ready independently of A16/A17; real interval differential |
| WebDAV backup run | C07 → C35 webdav | loopback DAV upload/full document/receipt + disconnect/shutdown |
| router_rule_sync row/run | R14 | missing row/runner → 503 until real wiring; no device operation here |
| aggregate agent row, DNS-follow run | A16/A17 | SetRuleSyncRow + SetDNSFollowRow; both required for real agent slot |
| Agent registry in notifications | A01 agents.Wire | already unconditional; no registry copy remains |
| confirmed empty router release | routers release owner | existing notification projection contract tested in C07; real device release/publication remains routers |
| web-port handler HEAD/Bind | reviewed C06 branch | no duplicate edit here; rebase when integrated |
| final repo/taskstatus/rulefetch commits | O3-6c / 6e5c7760 | known, assigned: owner checks do not close a shutdown race inside older shared I/O; adopt RecordContext/ForgetContext once integrated |

No Agent deployment, ownership proof or router state machine is implemented or
relaxed by C07. No production data or device access. Independent review is still
required; implementation acceptance does not mark this module verified.
