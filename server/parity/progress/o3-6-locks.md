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

ctxx implementation and provider local-batch replacement are being tested on .64: ctxx/lifecycle
race3 already passed; provider full race3 is still running in foreground session27514.
Do not sync production changes onto that copy until it completes. Uncommitted ctxx files are not
dependencies of the lock primitive commit. After completion, commit ctxx separately.

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
