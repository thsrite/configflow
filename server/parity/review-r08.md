# R08 adversarial HR review (O4)

Reviewer: fresh Opus session (did not write the port; Codex ported R08 under the
"Codex ports, Opus reviews" rule). Scope: `backend/routers/ros_policy.py` (520),
`ros_policy_write.py` (782) and `ros_policy_view.py` (47), read completely, plus the
Python callers that consume R08 values (`report.py` build_report, `plan.py` policy
block, `release.verify_nat`, `routeros_nat.assess_bypass` retiring, `routeros_nat_list`
`still_routed`/`plan`, `routeros_batch._console_add`, `ros_values`), against
`internal/routers/rospolicy_{model,read,plan,write,restore}.go` and every shared file the
port changed (`addresswrite.go`, `natlist_write.go`, `natlist_plan.go`, `routeros.go`,
`routeros_batch.go`, `plan_model.go`, `report.go`, `model.go`, `desired.go`, `messages.go`,
`routerfake`). The branch was rebased onto the current `go` (N8-4a…4d) before the gates.
Black-box reference: `parity/difftest-routers.md` D09, G09/G10, T11, H0612/H0613,
H1297, L5-B; design `design/r12-release.md` §2.2/§2.4/§3.3/§3.4 and "Clarified in R07";
`design/o3-concurrency.md` (Detach, BindRequest, Clarified sections, O3 review rules).
N8-4f is not on `go` yet; its constraint was applied as briefed (a nil read is never a
clear command; an error-fallback empty value is never ownership or "nothing to prove").
No production device or lab guest was contacted; loopback fakes and synthetic values only.

Method: every branch of the three Python files was walked against Go (request
method/path/body/timeout, proof before every destructive write, journal placement,
refused vs uncertain, compensation order, preflight, manifest remap, cancellation),
then every shared helper the port touched was re-read for side effects on R04–R07.
All Chinese literals of the three Python files were checked mechanically against
`messages.go` (all present). Each suspected defect became a failing test first
(`r08_review_test.go`); all five new tests were confirmed to FAIL on the handed-back
sources and pass after the fixes (output below).

## Findings

All findings are fixed; **none is open**. Severity count: **0 high, 1 medium,
5 low** (below the "≥ 2 high" fallback threshold of WORKFLOW's last section).

### Medium

- **F1 — the "moved member looks deleted" class survived in the shared rollback
  helper.** The porter found and fixed this Python bug for R08 by adding a preflight in
  `RestoreROSPolicy` (locate missing journal IDs globally, compare configs). The same
  class remained in the *shared* `restoreDeleteAddresses`, which R07's
  `RestoreNATLists` uses directly: a fresh NAT-list member moved by another writer into
  a different list between write and rollback was absent from the list-scoped read, was
  skipped as "already deleted", and the rollback reported success while a ConfigFlow
  member survived elsewhere. Fix: `restoreDeleteAddresses` locates IDs missing from
  their own list (`LocateAddresses`, only for missing IDs, so the normal path costs
  nothing) and treats a located row as an external edit — kept, reported as
  `策略路由对象在操作期间发生变化，保留现场：…` (incomplete rollback). R08's own preflight
  stays (it also blocks all later deletions, the documented R07 rule). Test:
  `TestR08ReviewNATListRollbackKeepsMovedMember` (fails before: "rollback reported
  success while a fresh member survived in another list"). Python behaviour intentionally
  not kept (O12, same entry as the porter's finding 1).
  Swept the rest of the class: `snapshotPolicy` already locates manifest members;
  `restoreAddresses` (recreate) only sees IDs this release itself deleted and proved
  gone; `restoreAddressSweep` only removes rows of the swept list (safe direction);
  `DeleteAddresses` guards with a scoped read before removal (a moved member fails the
  guard); `VerifyROSPolicy` withdrawal checks rows this release deleted under guard.

### Low

- **F2 — `SubtractNetworks` returned `nil` ("nothing remains") on mixed-family or
  invalid input.** `netipx.Collapse` errors on mixed families; the error branch returned
  nil. Today every caller passes IPv4-only prefixes, but the function is the exported
  R12 contract (§2.4) for `withdrawn = subtract(previous, current)`, where an
  error-fallback empty result would mean "nothing withdrawn → no static replacement
  proof" (N8-4f class). Fix: subtraction per address family (an IPv6 exclusion never
  removes IPv4 space), invalid prefixes ignored, never an error-fallback empty list.
  Test: `TestR08ReviewSubtractNetworksFamilies` (table-driven, 6 cases).
- **F3 — `prepare_static` presence.** `ROSPolicyReplace.PrepareStatic` used
  `omitempty`, so a planner-set empty list (static checks whose replacement routes
  already exist: Python `op['prepare_static'] = nat_ops + []`) vanished from the plan
  JSON and fingerprint input. Fix: `MarshalJSON` emits the key whenever the slice is
  non-nil, `[]` included; nil stays absent. Test: `TestR08ReviewPrepareStaticPresence`
  (also decodes the driver contract back).
- **F4 — policy view roles from empty list names.** `ReadPolicyObjects` recorded
  `roles[""]` for a saved jump rule without `src-/dst-address-list`, so an address entry
  without a list showed editor `destination`; Python only records truthy names. Fix and
  test `TestR08ReviewViewRolesIgnoreEmptyLists`.
- **F5 — vanished console-restored route reported with the wrong message.** In
  `restorePolicyRoutes` a route the console batch created under a proven fresh ID that is
  gone on read-back (`Rows[i] == nil`) was reported as the generic uncertain write;
  Python passes `None` to `_confirm` → `策略路由回读与提交内容不一致：/ip/route（对象不存在）`.
  Fix: confirm with the nil row. Test: `TestR08ReviewRestoredRouteVanished` (exact
  message; the other three routes are restored).
- **F6 — doc comments contradicted the shared cancellation change.** The port moved the
  whole RouterOS write/rollback path (`Apply`, `ApplyMany` work ctx, `RestoreMany`,
  `compensate`, `natBatch`, `CreateAddresses`, `DeleteAddresses`, `DeleteNATRules`,
  `ApplyNATLists`, `RestoreNATLists`, R08 entry points) from `context.WithoutCancel` to
  `lifecycle.Detach` — the change the R07 review deferred to O3-6. Behaviour accepted:
  it follows O3 review rule 1 ("Detach, never WithoutCancel, in owners"), request
  cancellation is still dropped (4b, tested), and at the drain deadline an aborted op
  returns quickly so R12's D-R12-3 path can still record manifests (with WithoutCancel
  the job would sit in the op until process exit and record nothing). But four comments
  still promised "runs to the end even if ctx is cancelled"; corrected. **Coordinator:**
  `r12-release.md` §3.3 says "Inside `Execute` all I/O uses `WithoutCancel(ctx)`"; that
  sentence should now read `lifecycle.Detach` (shutdown deadline cuts an in-flight op;
  §3.4 then records without rollback).

## Checked and found correct

- **Ownership.** Every destructive write is preceded by an exact ID + complete stable
  config proof (`configROSRow` drops exactly Python's RUNTIME set; deep equality, no
  normalisation): `deleteResource`/`guardResource`, `DeleteAddresses` (scoped read, all
  members proven before any journal/remove), `deletePolicyRoutes` (whole batch guarded
  before any removal — stricter than Python, which deleted a prefix first), restore
  deletes (guard + dynamic refusal + table reference check). Migrations touch only
  `cf`-managed main routes to the proxy gateway and managed NAT accept rules; names and
  comments never adopt (`OwnedROSPolicyRows`, `ReadPolicyObjects`). Malformed/duplicate
  manifest entries refuse (`messageOperationFormat`/`messagePolicyDuplicateID`) instead of
  Python's KeyError or accidental match. Foreign references to the bundle's table,
  lists or private chain block (`checkPolicyReferences`, also before deleting the old
  bundle and in rollback).
- **Refused vs uncertain.** `createResource`: a refusal journals nothing; an uncertain
  PUT (transport/5xx/missing or reused ID) journals exactly one provable fresh candidate
  or fails as rollback error, never retried. Remove fallbacks re-prove every ID.
- **Journal and rollback.** Restore records precede DELETE/remove; operation undos carry
  `UndoOf(err)`; reverse order restores the old bundle (jump last) before deleting the new
  one (jump first); once any restore fails, later deletions are skipped and the new
  protection kept (shared R07 rule); NAT-list undos get the remap list; recreated IDs
  remap `place-before` and the raw previous manifest (unknown fields kept). The porter's
  moved-member fix (preflight locate + config comparison) is correct; F1 closes the
  shared helper.
- **Timeouts and raw requests.** Every device request carries `rosTimeout` (10 s),
  checked through apply and rollback by `TestR08RequestsKeepProductionTimeoutAndRawBodies`
  (production devhttp client, body bytes, Content-Type, Basic auth, no body on
  GET/DELETE) and the real silent-peer `TestR08DeviceTimeoutActuallyExpires`. No test
  double overrides a production timeout or limit.
- **Cancellation (4b).** Write entry points detach; inspection, snapshot, view and verify
  use the caller ctx (verify waits with `waitContext`). No locks, goroutines, loops or
  `time.Sleep` in R08; the caller (R12/R14) holds L1/L2.
- **Scale.** Snapshot reads only NAT-referenced, known and manifest lists, locates
  missing manifest members, reserves names with one console scan; overlap uses the
  shared bisection index; subtraction is the sorted sweep; route migration removes in
  500-ID batches and restores in bounded console batches (32 KiB accounting). Python
  cases with 200/201/4096 members compare equal.
- **Table/FIB and wire forms.** `ComparableValues` (fib present-empty, passthrough,
  `gateway@main`) is used for read-back matching and the view; ownership baselines stay
  raw. The fake's `PolicyWire` mode reproduces these wire forms for both sides.
- **network_overlap / subtract / ranges (R07 F2 class).** NAT migrations go through the
  shared `NATPolicyMigrations`/`natKey`; a range stays text as an identity; ranges are
  summarised only where Python's `_networks` does (replacement CIDRs), identically.
- **Redaction.** No new redaction; password-only shared helpers unchanged.
- **Reuse.** `configROSRow`, `sameRow`, `writableResource`, `valuesMatch`,
  `ComparableValues`, `createResource`/`confirmResource`, address batch/proof cache,
  `consoleAdd`, `prefixOverlap`, `addressExclude`, `editROSEntry`, `Egress`/`AssessNAT`/
  `PreservesSources`, `oxidns.ReadPolicies/NormalizePolicies/Revision`, `ComputeDesired`.
  No private parser, YAML/JSON engine or redaction copy; no Python emulation (no jsonx).
- **Shared types (R12 §2.4).** `Op`/`OpHead`/`Stage` extended in place; `CIDRGateway`,
  `NATChecks`, `Manifest(s)`, `ManifestHolder` as designed; `ROSPolicyReplace` has
  `StaticChecks` (always present), typed `PrepareStatic []Op`, `StaticNATChecks`; the
  R07 JSON adapter `natlist_policy_adapter.go` and the temporary `RouterState.NATPolicy`
  are gone (grep clean); NAT-list evidence derives only from `ROSPolicyPlan.Check` +
  its raw previous manifest (`natPolicyEvidence`); typed `nestedNATMigration` recursion.
- **N8-4f.** Manifests carry `Set`; apply/restore always set explicitly (`null` for
  none — a JSON `null` previous decodes to the `null` raw value, never to an unset
  attribute). Error fallbacks found: `natPolicyEvidence` on an unreadable previous
  manifest drops previous resources → `StillRoutedNAT` counts the policy route as
  0.0.0.0/0 (conservative, keeps exemptions; and the policy plan already blocks);
  `jsonValuesEqual` decode failure → "not current" (re-plan); `/system/resource` read
  failure only drops the version hint (Python). `SubtractNetworks` was the one unsafe
  fallback (F2).
- **Report integration.** `policy_routing` section, summary counts (items, blockers →
  unknown, ops without items → pending), `in_sync`, route-extra retirement by migration
  IDs and NAT retiring by `op.owned` NAT IDs match Python (`report_policy_nat_retirement`
  compares the complete report against real Python).
- **Porter's issue-class statements** (R04 F1–F10, R05 classes 1–6, R06/R07 standing
  items, R07 F2–F5, A19 F1–F3, O3, N7/N8) were checked against the code; all hold except
  the R07 F5/"moved member" sweep (F1 above) and the cancellation wording (F6).

## Accepted differences (porter's list, re-checked)

1. Moved-member rollback reports incomplete (Python bug fixed) — now in the shared helper too (F1).
2. Whole-batch guard before route migration deletes; failed rollback proof stops later deletions.
3. `ROSPolicyDigest`/owner digest via `encoding/json` (opaque revision; router IDs are ASCII).
4. Python crashed (ValueError/KeyError/TypeError → HTML 500, no rollback) on malformed
   manifests, CIDRs or gateways; Go refuses with a Chinese RouterError and rolls back.
5. Read-back mismatch message lists fields in sorted order (Python: submitted-dict order;
   Go rows are maps). Only on a device that rewrites several submitted fields at once.

## Requirements on later modules

- **R12:** set the policy manifest on the driver during inspection exactly where Python
  sets `driver.ros_policy_manifest` (snapshot locates manifest members through it); map a
  snapshot `RouterError` to `state.ROSPolicy = nil` → blocker `无法读取 ROS 策略路由配置`
  (H1297); `VerifyNAT`'s `NATChecks` has no `nat_list` — the release-level verify must
  add `checks.nat_list` (`VerifyNATLists`) as Python's `verify_nat` does; set
  `PrepareStatic` to a non-nil slice whenever Python sets `prepare_static` (F3); persist
  `Manifests().Policy` when `Set` (value `null` = clear).
- **R14:** `restrict_plan` reads `migrations`, `previous`, `spec` of the policy op (K49).
- **R09 overlap (parallel branch `go-port-r09`):** textual conflicts expected in
  `desired.go` (`Desired` field block: R08 adds `ROSPolicy`, R09 `DNSGuard`), `model.go`
  (`RouterState` field block) and `routeros.go` (driver fields; both add
  `dnsGuardManifestSet` and the same `SetDNSGuardManifest` body — keep one). Semantics
  are compatible: R09 sets `ReportParts`, R08's policy part is appended after it; Python
  builds the policy plan before the DNS guard, but nothing in R09 reads
  `ROSPolicyPlan`/`PolicyNATRetiring`, so the order is safe. R09 must keep R08's
  `Manifests()` (it returns the DNS guard manifest too).

## L5 lab items (unchanged from the hand-back; do not block `verified`)

CHR 7.22+ `policy-rules` wire forms and missing-field versions; real `invalid` settle
time after creating the private chain; actual `immediate-gw`/egress of the private
table; source/direct/gateway packet paths and LAN/WAN NAT continuity during static ↔
policy switch and rollback; FastTrack/raw/VRF/foreign references; real console
16 KiB/32 KiB limits and the 10 s budget on slow boards; ID assignment/reuse and lost
replies; 20k-member scale. Added by this review: (a) a member moved to another list
between apply and rollback on a real CHR (F1: incomplete rollback, member kept);
(b) shutdown (SIGTERM) during a policy release with the detached driver (F6): the
in-flight op aborts at the drain deadline and the release record/manifests are saved.

## Codex-port quality (for the coordinator)

**High findings: 0.** Medium: 1 (F1 — class "ownership proof via scoped read": the porter
found the class but fixed only its own call site, not the shared helper). Low: 5 (F2
error-fallback empty value / N8-4f; F3 JSON key presence; F4 truthiness of a role key;
F5 message on a rare failure path; F6 stale comments after a shared-path change). The
fallback rule (≥ 2 high → remaining routers HR ports back to Opus) is **not triggered**.
The port is careful: Python-compared boundary corpus over results, errors, journals,
write sequences and final device tables, real production timeouts, raw-request checks,
a working mutation gate, and safety deviations that are all in the conservative
direction and documented.

## Commands and results

Targeted iteration (.64): `rt -- bash -lc 'cd server && go vet ./internal/routers/... && go test -count=1 -run "TestR08|TestR07|TestNATList|TestAddress" ./internal/routers'`
→ `ok configflow/internal/routers 134.185s`.

Detection proof (.64, review fixes reverted to the hand-back sources, tests kept):
`go test -count=1 -run TestR08Review ./internal/routers` → all five FAIL, e.g.
`rollback reported success while a fresh member survived in another list`;
`got [], want [10.128.0.0/9]`; `prepare_static=null present=false`;
`editors=map[*A1:settings *A2:destination *A3:source]`;
`error "策略路由回滚未完成：RouterOS /ip/route 已提交但没有返回可信的新对象 ID；…", want "策略路由回滚未完成：策略路由回读与提交内容不一致：/ip/route（对象不存在）"`.

Final gates on the branch rebased onto `go` (ae2e0f59):

- `.32`: `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'GATE_LOGS=/tmp/r08gate/logs server/scripts/gate.sh -p 4 FuzzR08PolicyInput:./internal/routers FuzzR07DecodeAddressRow:./internal/routers FuzzFakeIPYAML:./internal/routers'`
  (load 50–70 on 12 cores): gofmt empty, `go vet ./...` clean; full `go test -race -p=4 -count=1 ./...`
  passed — 84 packages `ok`, no FAIL (`ok configflow/internal/routers 626.017s`,
  `ok configflow/internal/routersplit 20.826s`, `ok configflow/internal/devhttp 3.826s`); proddiff
  `self_check: PASS`, `coverage_regressions: 0`, `baseline: new=2 stale=0` — both NEW rows are
  `GET /api/aggregations/<agg_id>/provider` (`body.proxies length`, `header.X-ConfigFlow-Provider
  missing-in-go`), the known aggregation-provider differences already on `go` (coordinator: under
  investigation in f3bfb2cc); nothing in routers. gate.sh stops after proddiff, so the rest ran separately:
- `.64`: `CONFIGFLOW_DIFF=1 go test -p=4 -count=1 -timeout=40m $(go list ./... | grep difftest)` → rc 0, all 12
  packages ok (agents 122.470s, aggregations 25.735s, subscriptions 69.569s, devicenotes, discovery, health,
  httpx, mosdns, refresh, subs, substore, final `ok configflow/internal/webdav/difftest 17.368s`).
- `.64` fuzz, 60 s each, `-parallel=2`: FuzzR08PolicyInput 185356 execs `ok 61.089s`; FuzzR07DecodeAddressRow
  507231 execs `ok 60.058s`; FuzzFakeIPYAML 222505 execs `ok 61.041s` (all fuzz targets of the changed package).
- `.64` mutation gate `python3 scripts/r08-mutation-gate.py` → result/journal/request/device-table `DETECTED`,
  `R08 mutation gate: 4/4 detected; sources restored`.

After the final `git rebase go` (onto 9a208c6c, O3-5), changed packages re-checked on `.64`:
gofmt empty; `go vet ./...` clean; `go build -tags configflow_proddiff ./...` and `-tags configflow_difftest` ok;
`go test -race -p=4 -count=1 ./internal/routers/... ./internal/routersplit/... ./internal/devhttp/...` →
`ok configflow/internal/routers 391.845s`, `ok configflow/internal/routersplit 14.196s`,
`ok configflow/internal/devhttp 3.492s`.
