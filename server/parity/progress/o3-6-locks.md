# O3-6 — in-process locks and owner cancellation

Status: implementing on go-o3-6 from go@9a208c6c; O3-5 is frozen. No sub-agents or pushes.
First commit619e4412 records all five coordinator clarifications in design/o3-concurrency.md.
Final acceptance is server/scripts/gate.sh -p 6, on .32 or .64; no separate full race/proddiff.
Routers writes and JobLock wiring wait for the R08/R09 merge notification, then rebase go first.

Order: ctxx/batch lifetime + lock primitives; repo L3/read snapshot/fail-closed; leaf stores,
listener, deployments, monitor, installer issue lock; Sub-Store leases; handler→owner detach;
routers last; full gate. Leave all Python lock files and the target-side compose bootstrap unchanged.

Foundations: NewLeafKeyed reuses keyed admission, cancellation/limit and reference cleanup without
recording a level or hiding ancestor records. Ordinary L4 stays sync.Mutex. LevelDeviceControl is
the second L2 suborder (after work/device, before repository); reverse acquisition still reports.
.32 gofmt/vet passed; full lockx race count3 passed11.939s. Tests cover bounded/cancelled leaf waits,
serialization/cleanup, forward suborder and subprocess-isolated reverse/ancestor violations.

Lock primitive commit: b4dbc67a. ctxx and provider local-batch replacement passed .64 gofmt/vet and
race3: ctxx1.030s, lifecycle7.876s, lifecycletest1.011s, full provider570.205s. The shared context
package has only the context dependency; tests cover detached disconnects, shutdown cancellation,
masked parent across nested Detach, preserved values and child-owned cancellation.

Additional coordinator ruling: repo's generic withLock also covered profile operation and file locks,
so do not blindly label all of them L3. Migrate profile operation locks to L2 Keyed by profile id and
cache/generated files to bounded leaf locks; retain only config's flock. Add concurrency/legacy-file
sentinels; this ruling is now in Clarified in O3-6. Repo migration is now being implemented; validation is pending.

Important agreed interfaces/boundaries:
- ctxx depends only on context; WithParent/Detach share the existing lifecycle semantics.
  Uninterruptible masks the parent through downstream Detach, only for local atomic/batch work.
- repo records real L3 via held contexts and exposes a minimal locked-read snapshot; missing JobLock
  fails closed (old code actually allowed nil). JobLock gets ctx. Keep .config.lock/flock and timeout.
- monitor reads/verifies at L3 then acquires L4 CAS, never an unlocked verify gap. Notifications gather
  observations outside L4 and version-fence their merge so stale observation work cannot win later.
- Sub-Store Use/Maintenance callbacks receive the held ctx; work45s/read,90s/maintenance,
  control45s; Tick/SetMode use explicit TryLock. Shared locks must cover Runtime copies of one Root.
  No nested RLock while a writer waits. Scheduling waits use Clock/ctx; persisted Monotonic stays.
- weblistener/StateStore blocking APIs gain ctx. Preserve validation/409 precedence.
- retain target bootstrap.py's cross-process compose-enrollment flock; only server .issue.lock moves.
- owned L1 leases must be released on every failure path, and Held replaces preparation flags.

Preflight-only facts to preserve in tests: multiple store/manager/runtime objects may share one
physical directory; file-flock replacement must not weaken their mutual exclusion. Local commits
must remain whole even when a downstream owner detaches after job cancellation. Legacy leaf code
currently calls external repo/task readers while holding its file lock; fix the boundary, not merely
the syscall. CAS/publication/target-generation assertions are mandatory, not optional cleanup.

ctxx commit: 3fcc53ec. Task-status now keeps its existing process-wide leaf mutex and atomic writes,
without opening/creating the legacy lock. New tests exercise100 concurrent updates from independent
Store objects and unchanged legacy bytes/inode/mtime, plus no new lock file. The old test expecting
a new lock file now asserts its absence; content/key order/mode assertions are unchanged.
.32 full taskstatus race3 passed2.613s. Notification leaf removal is pending its own commit; its
full suite currently hits the newly fail-closed repo guard in a router-removal fixture, which will
receive an explicit mock gate in the repo-contract step rather than weakening production defaults.

Repo resource migration now passes .64 gofmt/vet ./... and full touched-package race1:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go vet ./... && go test -race -p=6 -count=1 -timeout=20m ./internal/repo ./internal/notifications'`
Tail: repo90.651s, notifications4.924s. Config retains flock with real L3, profiles have L2 keyed
operations, generated/cache files bounded leaf admission; old resource files are not opened.
ReadSnapshot supplies a coherent locked read, rejects writes, and expires with its callback.
Missing JobLock rejects router-changing commits. Fixtures now explicitly supply the gate when
required and C04 probes the actual leaf admission instead of creating a historical file lock.
Regressions cover cancellation/batches, snapshot expiry and writer exclusion, profile parallelism,
delete/write ordering, timeout/cancellation and legacy bytes/inode/mtime. Intentional order errors
run in subprocesses; normal repository suite requires zero Violations.
One fixture-edit script aborted before saving, then its shell ran stale tests; corrected with
atomic apply_patch, unused import removed, and the full package run above passed. This was reported
to the coordinator; no product assertions were removed.
Notification store lock removal and staged observations/version fencing are currently uncommitted,
with new concurrency regressions being validated next. Routers remain untouched.

Notification leaf migration is complete: no flock/open of notifications.lock. Snapshot detaches at
its owner, reserves an observation ticket, collects repo/task/Agent data outside the leaf, then
merges only if no newer snapshot or committed notification mutation won. Successful unchanged
snapshots also fence older work; failed callbacks/commits do not advance the applied version.
Concurrent regressions hold a source open and prove newer snapshots, unchanged snapshots, notices,
and MarkRead finish immediately and survive the old source completing. Legacy sentinel plus100
concurrent store mutations also pass. .64 gofmt/vet ./... and full notifications race3:15.574s.
Next: monitor L3 read/verify→leaf CAS, listener, deployment Held, installer, Sub-Store; owner/handler
migration and routers after the coordinator's merge notification. Repo commit88422be8.

Notifications committedf24daeb1. Monitor now passes .64 gofmt/vet ./... and full monitor race1
173.713s. StateStore Read/CAS take ctx and use per-physical-file NewLeafKeyed with the unchanged5s
production bound (pinned by test); no file locks or Sleep polling. Manager requires WithReadSnapshot;
FromAgents delegates to its repository. Target/publication verification occurs in L3 and remains
held through leaf CAS. Real-repo concurrent tests hold the leaf and prove target/deployment writers
cannot cross this boundary; after their changes, stale target/publication checks refuse CAS and
preserve bytes. Normal Violations=0, timeout/cancel and legacy bytes/inode/mtime tests pass. The
old four-process reset test is replaced by four independent Store objects in one process (instance
cross-process exclusion already tested in O3-3); the single-winner CAS assertion remains intact.
Listener changes (ctx API, shared L1 try, Clock health wait) are in progress on .32. Routers untouched.

Monitor committed1473d985. Listener migration passes .32 gofmt/vet ./... and race3: weblistener1.255s,
api/settings47.382s. Start/Status/Update now take ctx and detach in the owner; cmd/settings pass
original contexts. L1 Try admission is keyed by physical dataDir across Manager copies, preserving
validation-before-busy precedence. Serve Spawn receives the owner context; health I/O keeps300ms
probes and5s default, waits via Clock and stops on shutdown. Tests pin5s, drive the production wait
with a fake Clock, prove disconnect survival/shutdown cancellation and timer cleanup, and verify
legacy bytes/inode/mtime/no new locks. No router changes.

Listener committed6992840e. Installer server issuance now uses a process-wide sync.Mutex over its
local file batch; IssueEnrollment takes ctx and detaches in the owner (API passes original ctx).
Target-side compose bootstrap text is unchanged. .64 gofmt/vet ./... and installer touched regression/
DockerHA corpus race1 passed36.490s. Twenty simultaneous issues per legacy file/directory/missing
case retain one identity; old lock metadata/content stay unchanged and missing lock stays missing.
Corpus still compares all state files; only .issue.lock is omitted from after-tree comparison.
One obsolete Python row(review/issue/197: .issue.lock is a directory) is explicitly classified as
accepted and replaced by the successful boundary test above; no corpus is regenerated. First vet
caught a syscall import also used for non-lock errno handling; retained it, then checks passed.
Agents deployment Held leases and cleanup are now being validated on .32; Sub-Store and remaining
owner/handler migration follow. Routers wait for R08/R09.

Sub-Store runtime migration passes .64 gofmt/vet ./... and full substore race1:25.380s. Shared physical
root owners use work RWMutex L2 and control L2 suborder, reference-counted through holders/waiters.
Use/Maintenance receive held ctx callbacks; try-only Tick/SetMode have explicit entry points. Nested
Use reuses its active lease so queued writers cannot deadlock a recursive RLock; read→write upgrades
return the existing busy response. Tests pin45/90/45s and20s/200ms, check writer preference/nested
Use, Runtime copies, ctx cancellation, short-limit error, forward0/reverse1 violations and legacy
sentinels. Startup waits use the scheduler Clock; persisted env.Clock.Monotonic remains unchanged;
the owner fake-clock seam remains available. The obsolete cross-process work test now exercises
independent Runtime objects, with cross-process exclusion covered by O3-3 instance tests.
Cache cleanup no longer deletes Python .lock files. Both explicit deletion/pruning have sentinel
regressions. Cache file-list fixtures expect no newly created lock. The first run surfaced those
fixtures plus an independent-request test accidentally inheriting the held callback ctx; fixed the
fixture and asserted startup elapsed time in its scheduler domain, then the complete suite passed.
Agent deployment full race is still running on .32 (no failure output). Owner/handler migration and
routers remain; wait for R08/R09 before routers. No full gate has been run yet.

Sub-Store committed8cc5994a. Agent deployment migration passes .32 gofmt/vet ./... and full agents
race1:522.971s. Preparation admission is a shared directory/Agent/deployment L1 key; running uses
Held without accessing legacy files. Manager's short release-handle mutex is not held over repo
transactions. Owned leases release on failed commit, panic unwind, Finish, and generation shutdown
(after commit finishes); rejection never releases a foreign lease. Existing busy/validation order
is preserved inside BeginDeployment. Tests cover Manager copies, interrupted preparation recovery,
disconnected request, synthetic failed/panicked/cancelled commits, no new legacy file and unchanged
legacy bytes/inode/mtime. Normal paths require Violations=0. Removed obsolete lock-path I/O-error
expectation: that path is now an ignored sentinel. Next: owner/handler cancellation and routers.

Agent leases committedbac4d3f6. Non-router owner cancellation migration is complete and validated:
HTTP handlers now pass original contexts; discovery edits/probes/jobs, health workers, MosDNS,
Sub-Store shared fetch/conversion, WebDAV and native adapter owners detach themselves. Repo public
write entry points also detach before any preparation reads; the existing update transaction uses
Uninterruptible for its local commit. Deploy preview/receipt file batches explicitly mask the parent
while holding their leaf lock. The review's metrics note is implemented: its save mutex is recorded
as L2, waiting with ctx, not held as an L5 memory lock over I/O. Its running flag retains its previous
meaning (actual admitted Run); no new lockx API. New DNS-I/O/metrics-wait regressions prove disconnect
survival and shutdown cancellation. No changes to routers or its handlers.

Validation (remote only, gofmt on explicit paths + go vet ./... passed in both):
- .64 `go test -race -p=6 -count=1 -timeout=20m ./internal/agents/metrics ./internal/api/domaindiscovery ./internal/api/mosdns ./internal/api/nodes ./internal/api/settings ./internal/api/subscriptions ./internal/deploy ./internal/discovery ./internal/health ./internal/mosdns ./internal/substore ./internal/webdav`
  PASS:10.386s,3.211s,1.037s,22.664s,14.992s,3.791s,59.800s,18.020s,310.735s,181.116s,24.563s,4.966s.
  Log /tmp/o36-owners.log in this rt copy. Vet first caught an assumed Mutex.Held API; retained the
  existing running atomic flag and used only the already available Mutex.Lock/ctx contract.
- .32 `go test -race -p=6 -count=1 -timeout=15m ./internal/repo`: PASS133.022s,
  log /tmp/o36-repo-owners.log. Earlier repo/resource90.651s remains recorded above.

Remaining dependency: coordinator has not yet notified R08/R09 integration. Latest local go observed
5680fbb3 (HANDOFF, after4d8a665a R08-release Detach), not the integrated R08/R09 boundary. DO NOT edit
routers or rebase until the explicit notification. All independent steps are committed; no tests are
running. When notified: git rebase go, reconcile to integrated routers, reuse its physical connection
L1 Keyed for repo.JobLock, finish router write/rollback Detach including R04–R07/R09, tests then
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'server/scripts/gate.sh -p 6'` for both tags/full race/
all difftest/proddiff. Rebase go again if advanced, verify clean; no push/subagents. This is not final
acceptance and no gate/proddiff has been run for O3-6 yet.
