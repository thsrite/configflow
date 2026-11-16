# R04 adversarial HR review (O4)

Reviewer: fresh Opus session (did not write the port). Scope: RouterOS REST
driver and batch writes — `backend/routers/routeros.py` (816 lines) and
`backend/routers/routeros_batch.py` (599 lines), both read completely, against
`internal/routers/routeros.go`, `routeros_batch.go`, `ros_script.go`,
`messages.go`, `internal/routers/routerfake` and the shared device client
`internal/devhttp`. Black-box reference: `parity/difftest-routers.md` section
D (D01–D06, D10) and L5. No production device was contacted; every test uses
loopback fakes and synthetic values.

Method: every branch of both Python files was walked against Go (request
method/path/body, ordering, batching limits, read-back, ownership proof before
delete, compensation, messages). Each suspected difference was first turned
into a failing test — a Python-compared case in `TestRouterOSBoundaryCorpus`
(real Python driver and the Go driver against the same stateful fake, compared
on outcome, request sequence and final tables) where Python is the reference,
or a Go-only test in `r04_review_test.go` where Python crashes or cannot be
observed (malformed replies, cancellation, panics). All of them failed on the
ported code (output recorded below) and pass after the fixes.

## Findings

All findings are fixed; **none is open**.

### High

- **F1 — extra reported IDs in a route console batch panicked and left routes
  unrecorded.** `consoleAdd` indexed `values[index]` for every `cf-id` line and
  sliced `values[len(ids):]`; a reply reporting more adds than the script held
  (`cf-id:5:*FA` after five adds) panicked with `index out of range [5] with
  length 5`. The five routes the router had already written were never
  recorded, so no release rollback could remove them. Python raised
  `IndexError` at the same place — the porter fixed this class for `_nat_batch`
  but not for the route path. Go now treats IDs beyond the script as a
  malformed reply: the proven routes before them are recorded, the reported
  foreign row is never touched, and the batch fails as an uncertain write
  (`RouterOS /ip/route 已提交但没有返回可信的新对象 ID；…已保留现场，请检查路由器`).
  In a rollback re-creation every requested route is still proven
  individually; the extra line is ignored there.
  Test: `TestRouteBatchExtraReportedIDFailsClosed`.
- **F3 — cancellation could abandon an accepted batch (PORTING rule 4b).**
  The driver used the caller's context for the read-backs that prove a batch.
  With a request context (client disconnect) after `/execute` was accepted:
  the route batch read-back failed (`context canceled；批量新增的路由无法回读…`)
  and the NAT settle wait returned a bare `context canceled` — written rows
  stayed on the router, nothing was recorded, and in the NAT case the error was
  not even a router error. `RestoreMany` aborted the rollback itself
  (`无法连接 RouterOS：ConnectionError`). Python finishes in all three cases.
  Fix: `Apply` and `RestoreMany` run to completion
  (`context.WithoutCancel`; each request keeps its own 10 s timeout);
  `ApplyMany` checks cancellation only between batches, where everything
  written is recorded, and returns `ctx.Err()` there. The settle wait error is
  wrapped as the NAT read-back rollback error in case a later caller passes a
  cancellable context. Tests: `TestApplyFinishesNATBatchAfterCancellation`,
  `TestApplyManyStopsOnlyAtProvenBatchBoundary`,
  `TestRestoreManyIgnoresCancellation`.

### Medium

- **F2 — rollback messages differed from Python.** Batched rollback records
  (`_restore_deleted` / `_restore_removed`) lacked the
  `RouterOS 回滚未完成：` prefix of Python's `_rollback_failed`; the batched
  delete and the single-path DELETE/PATCH snapshot check used
  `回滚对象已发生外部变化，保留现有配置` where Python says
  `对象在操作期间发生外部变化，保留现有配置` (new constant
  `messageChangedDuringWrite`); a RESTORE whose object had become dynamic said
  "changed" instead of `RouterOS 动态对象不能接管、修改或删除`. Boundary cases
  `review_batch_delete_rollback_changed_route_message`,
  `review_batch_delete_rollback_dynamic_route_message`,
  `review_batch_recreate_rollback_conflict_message`,
  `review_restore_present_dynamic_route_message`.
- **F7 — row comparisons could panic; composite values collapsed.**
  `maps.Equal` on decoded rows (`nat_update` expected vs live rule, DHCP
  expected attributes, policy mangle config) panics when both sides hold a JSON
  array (`comparing uncomparable type []interface {}`) — a request-path panic.
  RouterOS sends strings today, but the DHCP reader explicitly accepts list
  values. Also `comparable` turned every array/object into `""`, so ownership
  snapshots could not tell `["lan"]` from `["guest"]`. Fix: `sameRow`
  (`reflect.DeepEqual`) for raw rows; composite values compare by their JSON
  text. Tests: `TestNATUpdateWithArrayValuesDoesNotPanic`,
  `TestComparableDistinguishesCompositeValues`.
- **F9 — `dhcp_dns_update` without previewed attributes wrote anyway.** Python
  compares `entry.attributes != op.get('expected')`, so a missing value refuses
  the write (`RouterOS DHCP 网段在预览后发生变化，请重新预览`). Go skipped the
  check and patched the LAN's DNS without proof. The plan always sets
  `expected`, so this was latent, but it was fail-open on a whole-LAN DNS write.
  Boundary case `review_dhcp_missing_expected_rejected_before_write`.

### Low

- **F4 — cleanup after a failed add batch went one REST round trip per route**
  (GET, scoped GET, DELETE, GET for each of up to 100 routes) where Python uses
  its batched rollback (one query, one remove, one ID read). Same outcome and
  messages; now uses `RestoreMany` like Python's `restore_routes`. Boundary case
  `review_batch_readback_mismatch_cleans_up_in_batch` (needs the new fake
  fault `PatchCreated`).
- **F5 — a dynamic NAT rule in `nat_update`** was refused as preview drift
  instead of `RouterOS 动态对象不能接管、修改或删除` (Python raises in
  `_payload` first). No write happened in either version. Boundary case
  `review_nat_update_dynamic_rule_message`.
- **F6 — stop-on-failure reported removed routes as skipped.** In a batched
  DELETE rollback with a failure, Go marked every later pending record
  `skipped` even when the batched remove had deleted it; Python reports those
  as restored and skips only rows still present. Callers ignore `skipped`, so
  the visible effect was limited to status. Test:
  `TestRestoreDeletedReportsRemovedRowsAfterFailure` (a router that processes
  the remove IDs in its own order).
- **F8 — Python bug: rollback re-creation ignored the 32 KiB script limit.**
  `_restore_removed` put up to 100 adds in one script regardless of size
  (64 015 bytes in the test; the RouterOS console caps a script at 64 kB). Go
  now flushes before 32 KiB like the add path. Test:
  `TestRestoreRemovedRespectsScriptByteLimit`.
- **F10 — idiomatic Go clean-ups.** `collapseRouterNetworks` was a private copy
  of `Networks` (now `netipx.Collapse(Networks(values))`); `verifyChange`
  duplicated `checkWritten`; dead `json.Marshal` call and unused parameters
  removed; the `RouterOS` type documents that only `Ping` may run concurrently
  (Python's client probes ping one driver from a thread pool; `Ping` touches no
  driver state).

## Checked and found correct

- REST semantics: methods, paths (`*` kept, everything else escaped like
  `quote(safe="*")`), JSON bodies, `?dst-address=`/`?list=` scoping, 401/403/404
  handling (404 GET → missing, 404 DELETE → already absent), 4xx refusal vs 5xx
  and transport failure → uncertain write, permission diagnostics, non-JSON.
- Ownership: a PUT is owned only with a fresh `*hex` ID absent before and in
  scope; no trusted ID → nothing deleted, `已保留现场`. DELETE/PATCH undo is
  recorded before the request, so a lost reply is compensated by snapshot.
  RESTORE refuses unknown same-destination routes and a vanished NAT neighbour.
  Batched adds record only the proven prefix; deletes record a RESTORE for
  every row the ID read shows gone.
- Batching limits: min run 4, 100 adds, 32 KiB scripts, 200-term OR queries,
  500-ID removes, read retries 3 × 1 s (reads only), NAT settle 8 × 0.25 s;
  duplicate destination/ID ends a batch; refused `/execute` (4xx or
  `not enough permissions`) falls back to single writes; any other failure is
  uncertain and preserved.
- The porter's `_nat_batch` IndexError fix: extra IDs are marked unproven; only
  rows with fresh, unique, in-table IDs and a snapshot are removed; the
  unowned row stays; the rollback error is returned (boundary case
  `nat_batch_extra_id_python_index_bug`).
- Transport: no proxy, no redirects (a `Location` never leaks), keep-alive,
  connect timeout + per-read watchdog, TLS verify per router (HTTP/1.1 only),
  Basic auth Latin-1 else UTF-8, credentials only in the header and never in
  messages (only the failure class is shown). Go's transparent retry on a
  reused connection applies to GET only: `TestOnlyIdempotentRequestsAreRetried`
  shows POST/PUT/PATCH/DELETE reach the device once and fail as
  `ConnectionError` (uncertain write) when the connection drops after the
  request was read.
- Concurrency: `go test -race` on `internal/routers` and `internal/devhttp`;
  the handler registry is locked; the watchdog stops its timer.
- `rosTruthy` is RouterOS's flag reading (`true`/`yes`, any case, or JSON true)
  as Python's `_truthy`; `rosText` gives RouterOS strings/numbers as text.
  Device rows stay `map[string]any` (RouterOS objects are open-ended and every
  unknown NAT matcher must be preserved); operations, undo records, checks and
  scopes are typed structs.
- ReadState's DNS-guard hairpin and policy-mangle filters match Python's
  `dns_guard.stable`/`owned_hairpins` and `ros_policy.config_row`; R08/R09
  must reuse them, not add second copies.

## Accepted differences (not defects)

- Read-back reports the first mismatching field in sorted key order; Python
  used insertion order. Differs only when two or more fields mismatch.
- A PUT whose returned row is dynamic: Python raised without an undo record;
  Go records the DELETE without a snapshot, so the rollback refuses
  (`回滚缺少本次写入快照，无法确认对象归属`) — fail closed and reported.
- Request bodies are compact JSON (requests used `", "`/`": "`); Python
  `str(None)`/`str(True)` in comparisons and the Unicode `\b` of the permission
  regex are not reproduced — RouterOS REST sends strings and ASCII messages.
- Verified TLS uses the system roots (Python: certifi); the Docker image needs
  `ca-certificates` (O14 note).

## Requirements on later modules

- R12/R14/R15: the release passes `checked_dhcp_dns` only from its own fresh
  read, never from client input; `ApplyMany` may return `ctx.Err()` at a batch
  boundary — treat it as a failed stage and roll back the recorded undos.
- R07: replaces ReadState's plain address-list reads with digest proofs.
- R08–R11: register their operation kinds; reuse `stableROSRow`,
  `configROSRow`, `payload`, `ownershipSnapshot`, `sameRow`.

## L5 lab items (do not block `verified`)

Only a real RouterOS CHR (7.24/7.25.1) can prove these (difftest L5-A/L5-B):

1. REST over HTTP 7.9+ and HTTPS 7.1+ with a self-signed certificate, verify on
   and off; Go's TLS 1.2+ handshake and system-root verification.
2. Account groups: read-only, read+write without `api`/`rest-api`, without
   `test` (ping), without the policy `/execute` needs — the permission
   messages and the single-write fallback.
3. Console batches: 100 route adds, 32 KiB scripts with CJK/emoji/quote/`$`
   comments, the `cf-id`/`cf-err`/`cf-done` output and ID format; the
   `ret` shape of `/execute` with `as-string`; scripts near the 64 kB limit.
4. `POST …/print` with 200 OR terms, `…/remove` with 500 comma IDs, the order in
   which RouterOS processes those IDs and what a partial failure returns.
5. NAT batches: `place-before` order, the `invalid=true` settle window after
   300+ rules, `disabled`/`log` yes/no flags, unknown matchers preserved.
6. Lost replies: a write accepted but its reply dropped (pull the management
   link mid-batch) — rows preserved, rollback refuses unknown owners.
7. Static routes: defaults RouterOS fills (`distance`, `scope`,
   `target-scope`, `check-gateway`), dynamic/ECMP/VRF/recursive rows refused,
   restore of a deleted route with the same destination.
8. DHCP DNS: PATCH `dns-server`/`dns-none` on 7.24/7.25.1, option 6 at
   network/server/lease/matcher levels, RADIUS, read-back after renewals.
9. Non-Latin-1 credentials (UTF-8 Basic auth) accepted or refused by RouterOS.

## Commands and results

Before the fixes (`.63`, new tests added to the ported code):

```
--- FAIL: TestRouteBatchExtraReportedIDFailsClosed
    ApplyMany error = panic: runtime error: index out of range [5] with length 5
--- FAIL: TestApplyFinishesNATBatchAfterCancellation
    Apply after cancellation = context canceled；批量新增的 NAT 规则无法回读…
--- FAIL: TestApplyManyStopsOnlyAtProvenBatchBoundary
--- FAIL: TestRestoreManyIgnoresCancellation
--- FAIL: TestRestoreDeletedReportsRemovedRowsAfterFailure  [error skipped skipped skipped]
--- FAIL: TestNATUpdateWithArrayValuesDoesNotPanic  comparing uncomparable type []interface {}
--- FAIL: TestComparableDistinguishesCompositeValues
--- FAIL: TestRestoreRemovedRespectsScriptByteLimit  console script of 64015 bytes exceeds 32768
--- FAIL: TestRouterOSBoundaryCorpus/review_batch_delete_rollback_changed_route_message
    Python="RouterOS 回滚未完成：对象在操作期间发生外部变化，保留现有配置" Go="回滚对象已发生外部变化，保留现有配置"
--- FAIL: …/review_batch_delete_rollback_dynamic_route_message
--- FAIL: …/review_batch_recreate_rollback_conflict_message
--- FAIL: …/review_restore_present_dynamic_route_message
--- FAIL: …/review_nat_update_dynamic_rule_message
--- FAIL: …/review_dhcp_missing_expected_rejected_before_write  (Go patched the network)
--- FAIL: …/review_batch_readback_mismatch_cleans_up_in_batch  (per-route GET/DELETE vs batched remove)
```

After the fixes, final authoritative run on `.32` (rebased on `go` e6c87cc1):

```
gofmt -l internal/ tools/                                   (no output)
go vet ./...                                                ok
go test -race -count=1 ./internal/routers/ ./internal/devhttp/
  ok  configflow/internal/routers  78.870s
  ok  configflow/internal/devhttp  3.481s
go test -count=1 ./...                                      exit 0
  ?   configflow/tools/proddiff/generator  [no test files]
server/scripts/proddiff.sh
  self-check: status=PASS json-leaf=PASS yaml-leaf=PASS text-line=PASS
  baseline: new=0 stale=0                                   exit 0
```

The first proddiff run reported five NEW value differences in
`GET /api/agents/<agent_id>/metrics/summary` (averages) and exited 1; a re-run
on this branch and a run on plain `go` both gave `new=0`. It is a time-window
flake of the agents metrics summary, unrelated to routers.
