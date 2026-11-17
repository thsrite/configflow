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
