# R05 adversarial HR review (O4)

Reviewer: fresh Opus session (did not write the port). Scope: the iKuai 4.x
driver — `backend/routers/ikuai.py` (1179 lines, read completely) and the
parts of `backend/routers/release.py` that consume it (`_batch`,
`_apply_stage`, `_rollback`, `execute`) — against `internal/routers/ikuai.go`,
`ikuai_apply.go`, `ikuai_dhcp.go`, the iKuai parts of `model.go` and
`messages.go`, `internal/routers/routerfake/ikuai.go`, the Python reference
harness `testdata/r05_reference.py`, and the shared device client change in
`internal/devhttp`. Black-box reference: `parity/difftest-routers.md` D18–D24
(incl. the O6 additions) and its L5 items. No production device was contacted;
every test uses loopback fakes and synthetic values.

Method: every branch of `ikuai.py` was walked against Go (request
func/action/param, write order, batch snapshot and read-back rules,
ownership proof before delete/edit, undo records and the undo a failed write
carries, messages). Each suspected difference was turned into a failing test
first: a Python-compared case in `TestIkuaiBoundaryCorpus`
(`ikuaiReviewCases`) where Python is the reference, or a Go-only test in
`r05_review_test.go` / `devhttp_test.go` where Python crashes or the behaviour
is not observable in the corpus. All of them failed on the ported code (output
below) and pass after the fixes.

## Findings

All findings are fixed; **none is open**.

### Medium

- **F1 — credentials in the management address were no longer sent
  (devhttp).** R05 changed `devhttp.Client.Do` to call `http.Transport`
  directly (so a 3xx with an unparseable `Location` is returned instead of
  failing). `http.Client` used to turn URL userinfo into a Basic
  `Authorization` header when none was set; the transport does not. For iKuai
  (no device Basic auth) an endpoint such as `http://user:pass@host` — the way
  to pass a login-protected reverse proxy in front of the web UI, which the
  401 message explicitly points at (「管理入口是否另有登录验证」) — worked with
  Python requests (`prepare_auth` uses URL credentials when no auth is given)
  and with Go before R05, and now got 401. The store accepts such endpoints
  (`urlparse(...).hostname` only). Fix: `Do` sets Basic auth from the URL
  userinfo when the client has no credentials of its own (Latin-1 else UTF-8
  like the explicit credentials); explicit RouterOS credentials still win.
  RouterOS is unaffected (it always sets its own header). Test:
  `TestURLUserinfoIsBasicAuth` (userinfo, explicit wins, none, non-Latin-1).
- **F2 — refusals on the release path were plain English errors.**
  `route_delete` of a row whose network cannot be read (porter's fix 2) and
  `route_add` with an invalid IPv4 network returned
  `fmt.Errorf("routers: iKuai static_rt id=… has no readable network; not deleted")`
  and `…invalid IPv4 network …`. They are shown to the user (English text,
  not in `messages.go`), and they are not `*RouterError`: Python's
  `release.execute` rolls a stage back only for `RouterError` (any other
  exception skips the rollback and becomes HTTP 500), so a release ported with
  the same rule would leave the stage's earlier writes on the router. Fix:
  `RouterError`s with Chinese constants (`messageIkuaiRouteNetwork`,
  `messageIkuaiOperationFormat`); no write happens in either case. Tests:
  `TestIkuaiRouteRefusalsAreRouterErrors`; the corpus case
  `fixed_route_delete_unreadable_network_not_deleted` now asserts the class and
  message.

### Low

- **F3 — an add or add-undo without a name had no ownership proof.** Objects
  ConfigFlow adds are found again only by their unique `tagname`. `add` took
  `payload["tagname"]` whatever it was: a `nat_readd`/`policy_route_readd`
  whose record lacked `tagname` (or a missing/`null` record, which `rebind`
  turned into an empty form) was written; and `revertAdd` (`add_undo`) with a
  match without `tagname` compared rows by `jsonEqual(row["tagname"], nil)`, so
  a row **without** a name matching the other fields was taken for ours —
  the test shows the user's nameless NAT rule being deleted. Python raised
  `KeyError` before writing in all these cases. Records normally come from the
  driver itself and iKuai rows always carry a name (the device refuses a
  missing one), so this needed a malformed record, but it was fail-open on a
  delete. Fix: `add` and `revertAdd` require a non-empty string name, `rebind`
  refuses a record that is not an object (`iKuai 操作格式无法识别`, nothing
  written). Test: `TestIkuaiAddAndUndoNeedAName` (5 cases).
- **F4 — a restore wrote its before-image onto whatever row held the id
  (Python bug).** `route_restore`, `nat_restore` and `policy_route_restore`
  read the row by the record's id and wrote the record over it, without
  checking it was still the same object. If the rule was deleted by hand and
  iKuai gave its id to a new rule (SQLite row ids are reused when the highest
  row is deleted), or the rule was renamed, the rollback overwrote the user's
  rule with ConfigFlow's old one. ConfigFlow's own edits never change a
  `tagname`. Fix: the restore is refused unless the current row's `tagname`
  equals the record's (`iKuai <func> 中 id=… 的对象已被替换或改名，未自动恢复，请检查`);
  other hand edits are still overwritten as in Python (the record is the whole
  before-image). Tests: corpus `fixed_restore_refuses_replaced_object`
  (Python overwrites the user's rule; Go refuses without writing) and the
  control `review_restore_same_object_runs` (compared, equal);
  `TestIkuaiRestoreRefusesReplacedObject` (route, NAT, 端口分流).
- **F5 — the batch read-back was quadratic.** `confirmAdded` scanned every
  row for every new route with a JSON round trip per comparison
  (O(adds × rows) marshals; 300 adds against a few thousand routes is
  seconds). It now indexes the read once by name, as Python's
  `rows.setdefault(tagname)` did; same results (corpus unchanged).

## Porter's claims checked

- **Absent-add proofs (`absent`, replacing Python's mutation of returned undo
  dicts).** Set only from a complete read without the name (batch read-back of
  routes; `cfRefs` for a group this batch *added*, not updated); consulted only
  by `add_undo`/`ikuai_group_undo` when the current read again finds no object
  of that name; cleared by any new add of the name. It can therefore only turn
  "missing object → error" into "missing object → nothing to undo", never skip
  or delete a real object. Differences to Python's per-dict flag are benign:
  a proof also covers an older undo of the same name in the same driver
  (whose object is equally absent). It is per driver: R12 must use one driver
  per release (a new driver fails loud, not silent).
- **1008 re-login.** `send` re-logs in and resends exactly once, only after a
  JSON reply with code 1008; a second 1008 on a write is `KindRejected`; a
  failed re-login or a lost resend reply is an uncertain write (the caller
  attaches its undo). POST is never retried by the transport (not
  replayable). Corpus: `show_relogin_once`, `show_1008_twice`,
  `write_relogin_once`, `write_1008_twice`, `write_session_lost_relogin_fails`.
- **Rejected vs uncertain.** Only HTTP 200 + JSON object + integer `code ≠ 0`
  is a refusal; status ≠ 200 (incl. 4xx), non-JSON, empty 200, transport
  errors and a reply without an integer code are uncertain. Every add/edit
  carries its undo when uncertain (adds: by name, `confirmed:false`; edits:
  the whole old row; group writes: digest-checked records).
- **Pagination.** `limit=offset,1000`, total must be a non-negative integer and
  constant across pages, every page exactly as long as due, IDs positive,
  integer and unique when required; `static_` prefix only for dhcp_static.
- **Request size.** `IkuaiRequestBytes` = compact body + one byte per
  separator outside strings = Python's `json.dumps` size (corpus
  `request_bytes`, 5706/5712 probe); devhttp sends the compact body, never more
  than the measure. 92 KiB group splitting is R03's (unchanged).
- **Cancellation (PORTING 4b / o3 §2.5).** `Apply` and `Write` detach;
  `Batch` proves its routes on a detached context; reads (`Probe`,
  `ReadState`, `ReadDHCPDNS`, `ListAll`, `Call`) use the caller's context.
- **devhttp transport change** (besides F1): no proxy (transport has none),
  redirects never followed, cookies applied/stored on every response like
  `http.Client` (incl. 3xx), RouterOS has no jar; errors carry only the class;
  `TestOnlyIdempotentRequestsAreRetried` and R04's corpus pass.
- **Credentials.** Messages carry only failure classes, device texts and the
  redirect origin; the URL-parse error of devhttp (which may contain userinfo)
  never reaches a message (`messageIkuaiTransport`).
- **Concurrency.** One goroutine per driver (documented); `-race` on routers and
  devhttp passes.
- **The five intended differences** are justified: (1) `route_comment` lost
  reply carries `route_restore` — otherwise the user's route stays marked
  `cf-` and later releases treat it as ours; (2) `route_delete` refuses before
  deleting (now a `RouterError`, F2); (3) `rebind` refuses when our group is
  not found by name — Python pointed the rule at whatever group now held the
  gid (the user's); (4) a write reply without a code is uncertain, not a
  refusal — nothing proves it was refused, the undo is harmless; (5) an
  unparseable redirect is explained instead of crashing.

## Accepted (Python semantics kept, not defects)

- Deletes by id (`route_delete`, `nat_delete`, `policy_route_delete`, and the
  `*_delete` undo of a rule ConfigFlow added) prove nothing but the id; a
  delete whose reply is lost carries no undo (Python's behaviour; the object
  is then possibly gone and is not re-added on rollback). Only a reused id
  could make the undo delete a different rule — see L5 item 10.
- Restores overwrite hand edits of other fields of the same object (the record
  is the whole before-image).
- Python truthiness/equality of bools and floats in ints, `str(None)` in
  device-derived texts, Python crashes on malformed ops (porter's list).

## Issue classes (checklist for later HR ports)

State for each how the port avoids it (WORKFLOW "HR modules: Codex ports, Opus reviews"):

1. **Shared client behaviour.** When a shared helper changes how requests are
   sent (e.g. `http.Client` → `Transport.RoundTrip`), list everything the old
   path did implicitly (URL userinfo → Basic auth, cookies on every response,
   no redirects, no proxy) and test each (F1).
2. **Error type on write paths.** Every refusal or failure an operation can
   return is a `*RouterError` with a Chinese constant from `messages.go`, never
   `fmt.Errorf` text: the release decides about rollback and the user reads the
   message (F2).
3. **Identity proofs never match on "missing".** A name/ID used as ownership
   proof must be present and non-empty before anything is written or deleted;
   comparing `nil == nil` (or `"" == ""`) turns a malformed record into a
   match against foreign rows (F3).
4. **Id-addressed undo proves the id still names the same object** (name or
   recorded fields) before writing a before-image over it; device ids may be
   reused (F4, L5 item 10).
5. **Read-back loops index once.** No per-item scan with JSON round trips over
   whole tables (F5).
6. Patterns R05 got right and later drivers should copy: refused (device
   answered, code ≠ 0) vs uncertain (everything else) writes; the undo of an
   uncertain write travels on the error (`RouterError.Undo` / `UndoOf`);
   driver-remembered proofs may only relax "missing → error", never skip or
   remove a real object; `Apply`/`Write` detach from cancellation, reads do not.

## Requirements on later modules

- R12: as in PROGRESS (record `UndoOf(err)`, `Batch`, skip `{}`, one driver
  per release, `IkuaiOversizedRequests`); additionally **treat every error from
  `Apply` as a failed stage and roll back** (Python only did so for
  `RouterError`; the driver now returns `RouterError` for every refusal, but a
  rollback must not depend on the error type).
- R06/R16/R17 call `ListAll`/`Call` (not detached) and `Write` (detached)
  directly: their entry points that write must detach the context themselves so
  a read-back after a write is not cancelled (o3 §2.5).

## L5 lab items (added to PROGRESS R05; do not block `verified`)

10. Whether iKuai 4.x reuses the id of a deleted static_rt/nat_rule/
    stream_ipport row (and gid of route_object) for the next add; if it does,
    R12 should prove the name before a `*_delete` undo.
11. Management address with credentials behind a Basic-auth reverse proxy
    (F1): login, session cookie and 401 message.

## Commands and results

Before the fixes (`.63`, new tests against the ported code):

```
--- FAIL: TestURLUserinfoIsBasicAuth/userinfo     Authorization = "", want "Basic cHJveHkgdXNlcjpwQHNz"
--- FAIL: TestURLUserinfoIsBasicAuth/non-latin-1  Authorization = "", want "Basic 55SoOuWvhg=="
--- FAIL: TestIkuaiBoundaryCorpus/fixed_route_delete_unreadable_network_not_deleted
    {"class":"crash","message":"routers: iKuai static_rt id=12 has no readable network; not deleted"}
--- FAIL: TestIkuaiBoundaryCorpus/fixed_restore_refuses_replaced_object   (Go overwrote USER_NEW)
--- FAIL: TestIkuaiAddAndUndoNeedAName/nat_readd_without_tagname   iKuai nat_rule add 失败：请求参数不合法（tagname: 名称长度为1-15）
--- FAIL: TestIkuaiAddAndUndoNeedAName/nat_readd_without_record
--- FAIL: TestIkuaiAddAndUndoNeedAName/policy_route_readd_null_record
--- FAIL: TestIkuaiAddAndUndoNeedAName/add_undo_without_tagname    error = <nil> (the user's nameless rule deleted)
--- FAIL: TestIkuaiAddAndUndoNeedAName/add_undo_with_numeric_tagname
--- FAIL: TestIkuaiRestoreRefusesReplacedObject/{route,nat,policy}    error = <nil>
--- FAIL: TestIkuaiRouteRefusalsAreRouterErrors/{route_delete_unreadable_network,route_add_invalid_network,route_add_IPv6}
    error = &errors.errorString{…}
```

After the fixes, final authoritative run on `.32` (branch rebased on `go`
aeff9d75; fix commit `go(routers): R05 review fixes …`; afterwards rebased onto
`go` 0efddadf, which changes only `HANDOFF.md`/`WORKFLOW.md`):

```
gofmt -l internal/ tools/                                   (no output)
go vet ./...                                                ok
go test -race -count=1 ./internal/routers/ ./internal/devhttp/
  ok  configflow/internal/routers  247.704s
  ok  configflow/internal/devhttp  3.573s
go test -count=1 ./...        64 ok; one failure: internal/importer
  TestAdversarialImporterResourceBounds/wide (import process killed by its
  timeout under full parallel load — the known issue in HANDOFF, being fixed on
  go-fix-importer-bounds); re-run alone: ok configflow/internal/importer 1.867s
TestIkuaiBoundaryCorpus: 307 cases, 6 intended differences (Python bugs fixed in Go)
CONFIGFLOW_DIFF=1 go test -count=1 <every difftest package>  9 × ok
  (api/agents, api/subscriptions, discovery, health, httpx, mosdns, refresh, substore, webdav)
server/scripts/proddiff.sh    self-check PASS, baseline: new=0 stale=0, exit 0
```
