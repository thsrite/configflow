# R07 adversarial HR review (O4)

Reviewer: fresh Opus session (did not write the port; Codex ported R07 under the
"Codex ports, Opus reviews" rule). Scope: `backend/routers/routeros_nat_list.py`
(1016), `ros_address_lists.py` (297) and `backend/routeros_test_helpers.py`
(109), read completely together with the `ros_policy_write.py` helpers R07
calls (`_create`/`_put`/`_confirm`, `_delete`, `_create_addresses`,
`_run_address_batches`, `_settled_address_rows`, `_delete_addresses`,
`_forget`, `_restore_*`, `run_steps`) and `RouterOSDriver.read_state`, against
`internal/routers/addresslists.go`, `addresswrite.go`, `device_redact.go`,
`natlist_plan.go`, `natlist_read.go`, `natlist_write.go`,
`natlist_policy_adapter.go`, `plan_model.go`, the R07 parts of `routeros.go`,
`messages.go`, `model.go` and `routerfake`. Reviewed **together with the R06
review fixes** (bd8a6a92, on `go` since 62427c41): the branch was rebased onto
them first, which surfaced finding F1(c). Black-box reference:
`parity/difftest-routers.md` D07, D08, G08, L5-A/B/I; design
`design/r12-release.md` §2.2/§2.4 and "Clarified in R07"; `design/o3-concurrency.md`.
No production device or lab guest was contacted; every test uses loopback
fakes and synthetic values.

Method: every branch of the three files and the called write helpers was
walked against Go (request method/path/body/timeout, proof before every
destructive write, journal order, refused vs uncertain, compensation order and
the `kept` rule, cancellation, cache scope). Each suspected defect became a
failing test first (output below) — Go-only tests in `r07_review_test.go`
where Python cannot see the behaviour, a Python-compared plan corpus in
`r07_review_boundary_test.go` where Python is the reference.

## Findings

All findings are fixed; **none is open**. Severity count: **0 high, 1 medium,
4 low** (below the "≥ 2 high" fallback threshold of WORKFLOW's last section).

### Medium

- **F1 — device diagnostics: username treated as a secret, private
  redaction helper, and a shared-path rewrite that defeated R06's verified
  secret message.** (a) `NewRouterOS` stored `secrets = {password,
  username}` and `safeDeviceText` replaced every occurrence in place with
  `[REDACTED]` — the R06 F2 class: a RouterOS user named `api` turns
  `rest-api` in a device detail into `rest-[REDACTED]`, and short usernames
  mangle any console output. Python hides nothing; usernames are shown by the
  router form. (b) The helper was a private copy of `internal/redact`
  (literal + two encodings only; PORTING rule 1 "no private copies of shared
  helpers"). (c) R07 put this redaction into the shared R04 `request` path;
  combined with R06, a route edit whose device reply echoes the password
  returned `RouterOS PUT /ip/route 返回 HTTP 400（[REDACTED]）` instead of
  R06's verified `路由操作失败：设备回复包含凭据，已隐藏详情，请检查路由器`
  (`TestR06RedactionHidesOnlySecrets/password_echoed_by_device`,
  `TestR06DeviceCredentialEchoIsHidden/routeros` failed on the combined tree).
  Fix: only the password is a secret; `safeDeviceText` uses
  `redact.ContainsEncodedSecret` (any URL-encoding depth) and replaces the
  whole device text by `redact.Redacted`; `routeSafeError` also treats an
  already transport-redacted error as a credential echo, so route edits keep
  their Chinese message. Tests: `TestR07RedactionHidesOnlyPassword` (REST
  detail and console output; username `api` visible, plain and
  `%40`/`+`-encoded password hidden) and the two R06 tests above (pass again).
  The rebase onto R06 already dropped the username; the remaining parts are in
  the review commit.

### Low

- **F2 — address ranges became CIDR member keys (identity ≠ Python).**
  `natKey` used `Networks`, which summarises `a-b` ranges; Python's `_key`
  uses `IPv4Network(text, strict=False)` and keeps a range as text. A
  ConfigFlow member edited into `198.51.100.0-198.51.100.0` therefore counted
  as the planned `/32` in Go (kept, nothing added) where Python replaces it;
  the legacy-shape proof could take a range-form `cf-mihomo` rule as one the
  per-destination writer produced (it never wrote ranges); `VerifyNATLists`
  accepted the range as the planned member. Fix: `natKey` uses
  `netipx.ParseNetwork4(text, false)`, the legacy group parse
  `netipx.ParseNetwork4(key, true)` (Python `IPv4Network(key)`). Tests:
  `TestR07NATKeyKeepsRanges`, Python-compared `TestR07RangeKeysMatchPython`
  (`range_member` failed before the fix with different `dst_add`/`dst_remove`/
  `exempt`; `range_legacy` already agreed and guards the legacy path).
- **F3 — large policy migrations were quadratic with re-parsing.**
  `NATPolicyMigrations` re-parsed every exclusion for every member
  (`Networks(exclusions)` inside the member loop): thousands × thousands
  parses; Python bisects over collapsed networks (`network_overlap`). Also
  `slices.Contains` scans in `LocateAddresses` (> 20 IDs), the sweep rollback
  read-back, `VerifyNATLists` removed IDs, and `NATRemovalsFirst`'s legacy
  keys. Fix: `prefixOverlap` (collapse + binary search; R08 may replace it by
  `ROSPolicyNetworkOverlap`), sets elsewhere. Test:
  `TestR07PrefixOverlapMatchesPairwise` (10 000 randomized comparisons
  against the pairwise scan). Outcomes unchanged (all corpora pass).
- **F4 — the reuse scope aliased downloaded rows with the caller.**
  `ReadAddresses` stored the downloaded slice in the scope and returned the
  same row maps; a caller modifying a returned row changed what the next read
  in the same scope "proved" (the digest-proven path already cloned). Python
  has the same aliasing, but the hand-back claimed deep-copied proof caches.
  Fix: the scope keeps its own copies (download and digest-proven rows);
  `forgetAddresses` now mirrors Python's `_forget` per list name (it updated
  the expected hint of a known list on the second item). Test:
  `TestR07ReuseScopeOwnsDownloadedRows`.
- **F5 — the member read-back compared lengths.** `VerifyNATLists` checked
  `len(ours) == len(expected)` and inclusion; Python compares sets, so a
  repeated planned key failed a correct read-back. Fix: set equality. Test:
  `TestR07VerifyComparesMemberSets`.

## Checked and found correct

- **Ownership (never touch user entries).** The list rule is ours only by its
  exact shape (comment, both list names, no other matchers, unique, ahead of
  every translating rule, no foreign reference in NAT/Mangle/filter/raw);
  members only by comment `cf-mihomo-nat`, dynamic/timed/disabled members
  block the plan; foreign members are kept and block when no rule exists.
  Every destructive write re-proves preview ID + full stable config
  (`checkNATUnchanged`, `guardResource`, `DeleteAddresses` against one fresh
  read, `DeleteNATRules` against one table read, and again per ID in its
  DELETE fallback — stricter than Python, which deleted unguarded there).
  Withdraw without a rule is refused (Python crashed). Created rows are ours
  only with a fresh unique `*hex` ID absent before and matching every
  submitted value; ambiguous/surplus fresh rows are never journaled.
- **Refused vs uncertain.** Transport errors and 5xx on writes are
  `KindUncertainWrite`; the NAT PUT then accepts exactly one fresh matching
  candidate (else `RouterRollbackError`, nothing deleted); an address batch
  waits for the list to settle (10 × 1 s) before journaling; 4xx refusals read
  once and journal what exists. Missing completion marker = refused, proven by
  read-back.
- **Rollback and compensation.** Journal entries are written before each
  destructive request; the sweep step stands in for an unread batch; restore
  runs in reverse, old rules come back in original order (sibling/anchor rule
  of `restore_rules`, remapped IDs, console batches ≥ 4), new objects are
  deleted only when nothing failed and no foreign reference exists (`kept`
  message), and Go additionally stops later deletions after any failure
  (author's correction, safer than Python's per-group flag).
- **Timeouts (R06 F1 class).** Every R07 request — reads, `/execute`
  batches, NAT PUT/DELETE, `/remove`, settle reads, compensation — passes
  `rosTimeout` (10 s); detached contexts therefore always end. Proven per
  request by `TestR07RequestsKeepTimeoutAndRawBodies` (apply, restore, legacy
  delete with failed batch remove and per-ID fallback, batched restore).
- **Cancellation (PORTING 4b).** `ApplyNATLists`, `RestoreNATLists`,
  `CreateAddresses`, `DeleteAddresses`, `DeleteNATRules` detach; plan/state
  reads keep cancellation and only update caches after a successful read;
  `TestNATListWriteAndRollbackFinishAfterCancellation` passes.
- **Locks (O3).** R07 owns no lock, goroutine or loop; the driver is
  single-operation state; R12/R14 serialise with L1/L2. Nothing to order.
- **Pagination and large lists.** Console fetch pages of 2000 with total
  consistency and REST fallback; digest 500-row chunks; batches 200 members /
  16 KiB UTF-8 bytes (oversize single member refused); removes 500 IDs;
  > 20 IDs locate via one table read.
- **Raw requests (R06 standing item 2).** GET/DELETE carry no body or
  Content-Type (R06 F3 fix applies to all R07 reads); `/execute` is exactly
  `{"script","as-string":""}`, `/remove` exactly `{".id"}`, the NAT PUT
  exactly the seven rule keys incl. `place-before`.
- **Test doubles (R06 standing item 1).** Every R07 test builds the driver
  with the production constructor and devhttp client; no seam overrides a
  timeout or batch size.
- **Console injection.** Names/addresses/values go through `ScriptString`;
  field keys are restricted to `[A-Za-z0-9_.-]`; a value containing
  separators or newlines can only make a decoded row fail its field count
  (→ REST fallback), never forge one. New fuzz target
  `FuzzR07DecodeAddressRow`.
- **Credentials.** Only the password (any URL-encoding) is hidden (F1); rows
  are never redacted, so ownership comparisons stay exact.
- **No Python emulation.** Typed ops/journal/plan; no exception emulation; the
  console encoder/decoder model RouterOS, not Python.
- **Temporary JSON adapter.** `natlist_policy_adapter.go` only marshals an
  `Op` of kind `ros_policy_replace` and counts nested `prepare_static`
  `nat_list_sync` legacy/stale lengths; it builds no op and writes nothing;
  header says "R08 落地后移除"; the R08 task is recorded.
- **Shared types vs r12-release.md §2.2.** `Op`, `OpHead` (+ methods),
  `Stage` (field names, types, tags incl. `omitempty` on the three pointers)
  and the ten stage keys match the design and Python's `plan.py`;
  `NATUpdateOp` has the §2.2 keys with exact conditional presence. The R07
  function signatures match §2.4.
- **R04 ten-class prevention claims** (author's list re-checked):
  F1 malformed replies — true (count-checked decoding, guarded digest/total
  parsing, no index panics; fuzz added). F2 messages — true (all constants,
  rollback texts Python-compared). F3 cancellation — true (detach + 10 s per
  request, tested). F4 batching — true. F5 dynamic — true. F6 journal before
  delete — true. F7 deep comparisons — true (`sameRow`); the proof-cache
  claim was incomplete (F4 here). F8 limits — true. F9 proof before
  destructive writes — true. F10 shared helpers — partly false: private
  redaction copy (F1) and a pairwise overlap scan instead of the Python
  bisection (F3).

## Accepted differences (porter's list, re-checked)

Password echoes in device diagnostics are hidden (Python shows them; O9/O12) —
now password only, via the shared helper. Malformed digests are `RouterError`;
oversize single members and unsafe keys refused before I/O; Go stops later
deletions after any rollback failure; surplus/duplicate/dynamic fresh rows are
never journaled or swept; the per-ID DELETE fallback re-proves each rule;
composite-valued rows always use REST reads instead of a digest proof. All
are fail-closed corrections and listed for O12.

## Requirements on later modules

- R08: replace `natlist_policy_adapter.go` by typed `PrepareStatic` traversal
  and delete it; supply `NATPolicyEvidence`; may export
  `ROSPolicyNetworkOverlap` and use it in `NATPolicyMigrations` instead of
  `prefixOverlap`.
- R12: open `ReuseAddressReads` per preview/release and call
  `InvalidateAddressReads` before each op/batch; any undo record is recorded
  before failing a stage; pass `remapped` into `nat_list_restore` like
  Python's `run_steps`.
- Any later redaction: password only, through `internal/redact`; route-edit
  errors keep R06's message (F1c).

## L5 lab items (added to D07/D08/L5-B/L5-I; do not block `verified`)

The porter's list (200-member/16 KiB scripts, 500-ID removes, associative
key/ID order and 500-row MD5 chunks with CJK/escaping, 2000-row print pages,
`/execute` output/ID format and permissions, invalid settle, legacy migration
load, partial-delete order, link loss, restore anchors, packet paths) plus:

1. An address-list member written as a range (`a-b`) on 7.24/7.25.1: does the
   firmware keep the range text or normalise it to a CIDR (F2 keys)?
2. `:tostr` of a member whose comment holds `;`, `=`, newline or `cf-row:`:
   confirm the fetch falls back to REST and the digest still matches.
3. A device reply (REST detail, console error) with a user named `api`: the
   text stays readable (F1).
4. A management link cut during an address batch and during the NAT PUT: the
   write ends after 10 s as uncertain and reconciles by read-back.

## Codex-port quality (for the coordinator)

**High findings: 0.** Medium 1 (F1, credential-redaction class — the same
class as R06 F2, plus a cross-module effect on R06 once combined), low 4
(F2 identity drift from a shared parser, F3 performance on large lists, F4
cache aliasing, F5 set comparison). The fallback rule (≥ 2 high) is **not**
triggered.

Right: the hard HR classes — exact-shape ownership, fresh-ID proofs,
refused vs uncertain, journal-before-write with sweep stand-ins, ordered
legacy restore with anchors, the `kept` rule, detachment on every write entry
point, 10 s on every request, typed R12 contract, a Python-compared corpus
(plans, journals, full wire, final tables) with four deliberate-mutation
checks and an independent script interpreter. Wrong: (1) a self-introduced
safety feature (redaction) designed too broadly and placed in a shared path
without checking the neighbouring module's contract — the second time this
class appears (R06 F2); (2) reaching for a convenient shared parser
(`Networks`) whose semantics (ranges) differ from the Python key function;
(3) pairwise scans where Python bisects. Suggested checklist addition:
**"a change to a shared driver path (request, redaction, parsing) needs the
other modules' tests on the combined tree"**.

## Commands and results

Before the fixes (`.32`, new tests against the ported code rebased on R06):

```
--- FAIL: TestR06RedactionHidesOnlySecrets/password_echoed_by_device
    message = "RouterOS PUT /ip/route 返回 HTTP 400（[REDACTED]）", want "路由操作失败：设备回复包含凭据，已隐藏详情，请检查路由器"
--- FAIL: TestR06DeviceCredentialEchoIsHidden/routeros
    device echo leaked: RouterOS PUT /ip/route 返回 HTTP 400（[REDACTED]）
--- FAIL: TestR07ReuseScopeOwnsDownloadedRows
    reused rows were modified through an earlier result: comment "changed by caller"
--- FAIL: TestR07VerifyComparesMemberSets
    回读校验失败：地址列表 cf-mihomo-nat-dst 的 ConfigFlow 条目与计划不一致
--- FAIL: TestR07NATKeyKeepsRanges
    natKey("198.51.100.0-198.51.100.255") = "198.51.100.0/24", want "198.51.100.0-198.51.100.255"
--- FAIL: TestR07RangeKeysMatchPython/range_member/{1,5}
    plan $.ops[0].dst_add[1]: lengths 2 vs 1; $.ops[0].dst_remove[0]: lengths 1 vs 0; $.exempt…
```

(The username part of F1 was removed while resolving the rebase onto R06, so
no separate "before" run exists for it; `TestR07RedactionHidesOnlyPassword`
covers it.)

After the fixes, final authoritative run on `.32`: see the R07 section of
`PROGRESS-routers.md` and `progress/R07-nat-lists.md` (exact commands and tails).
