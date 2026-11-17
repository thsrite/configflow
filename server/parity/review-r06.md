# R06 adversarial HR review (O4)

Reviewer: fresh Opus session (did not write the port; Codex ported R06 under the
"Codex ports, Opus reviews" rule). Scope: `backend/routers/route_editor.py`
(164), `routeros_routes.py` (283) and `ikuai_routes.py` (237), each read
completely together with the Flask handlers that call them
(`routes/routers.py` `_manual_sync_lock`, `_device_route_change`) and the
R04/R05 driver helpers they use (`_request`, `_payload`, `_list_all`, `_write`,
`_route_payload`), against `internal/routers/route_editor.go`,
`routeros_routes.go`, `ikuai_routes.go`, the R06 parts of `messages.go`,
`routeros.go`, `routerfake`, `internal/devhttp` and `internal/lockx`.
Black-box reference: `parity/difftest-routers.md` R019–R021, T16, X01, X04,
X10 and L5-A. No production device or lab guest was contacted; every test uses
loopback fakes and synthetic values.

Method: every branch of the three files was walked against Go for both
drivers (request method/path/body/timeout, preflight, readback, ownership proof
before every compensating write or delete, ID reuse, error kind and Chinese
text, cancellation). Each suspected defect was turned into a failing test
first — Go-only tests in `r06_review_test.go` where the behaviour is not
visible to the Python corpus (timeouts, wire bodies, redaction), and a
Python-compared case in `TestR06PythonBoundary` where Python is the reference.
All failed on the ported code (output below) and pass after the fixes.

## Findings

All findings are fixed; **none is open**. Severity count: **1 high, 2 medium,
2 low** (below the "≥ 2 high" fallback threshold of WORKFLOW's last section).

### High

- **F1 — RouterOS manual writes and their compensation ran without any device
  timeout.** `routeros_routes.go` passed `timeout 0` to `d.request` for the
  PUT/PATCH/DELETE of the edit and for every compensating write
  (`editROSRestore`). In `devhttp.Client.Do` 0 means *no* dial timeout and no
  read watchdog, and `MutateRoute` deliberately runs on
  `context.WithoutCancel`, so nothing could end the request: a router that
  accepts the write and never answers (the dropped management link of L5-A
  item 3) blocked the edit forever — no readback, no compensation, the write
  never reconciled — while the caller held the L2 `RouterOperationLock` (and
  in R15 the L1 job lock). Every later publish, rule sync or route edit of that
  connection would wait ten minutes and get 409 until the process restarted.
  Python's `_request` uses `TIMEOUT = 10` for these calls; the design and
  hand-back claimed "device I/O still has its 10 s timeout". The corpus missed
  it because its timeout cases replace the transport with `r06ShortHTTP`, which
  forces 200 ms whatever the driver asks for. Fix: the three calls use
  `rosTimeout` (iKuai was not affected: `Write` maps 0 to its 10 s default).
  Tests: `TestR06RouterOSRequestsKeepDeviceTimeout` (records the timeout of
  every request for add/update/delete and the three compensation paths),
  `TestR06RouterOSUnansweredWriteTimesOut` (real devhttp client, a PATCH that
  is never answered: now an uncertain write after reconciliation within ~10 s,
  table unchanged; before: still hanging after 30 s).

### Medium

- **F2 — redaction treated the username as a secret and hid real
  diagnostics.** `routeSafeError` replaced the whole error with
  `路由操作失败：设备回复包含凭据，已隐藏详情…` whenever the text contained the
  username, the password or the URL userinfo username. RouterOS REST users are
  commonly named `api`; `permissionHelp` contains `api`/`rest-api`/`read`/
  `write`, so with such a user every 403 (and every refusal that quotes the
  help) lost the actionable permission guidance; a device text that merely
  named the user was hidden too. Usernames are not credentials here (the router
  form and `public_view` show them; Python's X01 hides neither). Fix: only the
  password and a URL-userinfo password are secrets; the RouterOS driver keeps
  just the password for this purpose (`editSecrets` → `password`). Test:
  `TestR06RedactionHidesOnlySecrets` (user `api` keeps
  `messageROSForbidden+permissionHelp`, a username echo is shown, a password
  echo is still hidden).
- **F3 — every RouterOS GET and DELETE sent a JSON body `null`
  (shared-client class 1; R04 code, exposed by R06).** `RouterOS.request`
  passed its `values Row` straight to `DeviceHTTP.Do(…, body any, …)`; a nil
  `Row` is a non-nil interface, so devhttp encoded it as `null` with
  `Content-Type: application/json`. Python sends a body only when `values is not
  None`. The fakes decode `null` and "no body" to the same nil, so neither the
  R04 nor the R06 corpus could see it; real firmware behaviour with a body on
  GET/DELETE is unproven (L5). Fix: `request` passes a nil interface when
  `values` is nil. Test: `TestR06RouterOSBodylessRequests` (raw bodies and
  Content-Type of the GETs of ReadRoutes/MutateRoute and of the DELETE).
  R04's full corpus passes unchanged.

### Low

- **F4 — a non-string iKuai `interface` lacked the `路由接口不完整` reason.**
  Go tested `snapshot.text("interface") == ""`, which renders a number as
  text; Python requires a non-empty string. The row was read-only either way
  (type reason), only the displayed reason differed. Fix: string assertion.
  Python-compared corpus case `ikuai/readonly/interface_number`.
- **F5 — idiomatic Go.** `BuildRoutePlan` built `after` by marshalling the
  public route and the values into `map[string]json.RawMessage` and merging the
  maps — an emulation of Python's `{**before, **values}`. Now a typed
  `PlannedRoute` (`editable`/`reason`/`revision` present only when there is a
  current route, exactly the keys Python produces; the plan corpus is
  unchanged). Also: the iKuai editor no longer shadows the builtin `copy`, and
  `route_editor.go` imports are grouped.

## Checked and found correct

- **Ownership before destructive compensation (both drivers).** RouterOS: a
  create is ours only with a fresh, unique `*hex` ID absent before; without one
  the compensation only accepts an unchanged table and otherwise preserves the
  scene (no guess by destination); delete of an added row and PATCH back of an
  update require the live row to equal the full canonical expected snapshot;
  recreation of a deleted row requires a free ID and destination and verifies a
  single fresh `*hex` candidate (and the returned ID, when present). iKuai: the
  random 15-char `RT…` name must be unique, new, editable and every form field
  equal; restore of an update requires the live row to equal our written image
  (incl. `tagname`) and the name to stay unique; recreation requires free name
  and destination. Unchanged tables need no compensation; lost undo replies are
  resolved by readback, never resent.
- **ID reuse / wrong-object writes.** Every edit re-reads and compares the full
  stable snapshot (RouterOS: whole entry; iKuai: ENTRY_IDENTITY incl.
  snapshot) immediately before writing; duplicate IDs make rows read-only and
  the generic `ReadRoutes` refuses the list; Go's extra duplicate-tagname
  refusal (Python writes) is a justified safety difference with zero-write
  proofs.
- **Errors.** Every vendor failure path returns a `*RouterError` with a
  `messages.go` constant (conflict / router / uncertain / rollback kinds as
  Python's classes); validation alone returns `ValidationError`; rollback
  messages carry the original and the compensation error.
- **Cancellation (PORTING 4b, R05 requirement).** `MutateRoute` (both) and
  `ExecuteRoutePlan` detach before the preflight read; readbacks and
  compensation use the detached context; reads (`ReadRoutes`, preview) keep
  cancellation. With F1 fixed each request is again bounded by its 10 s device
  timeout.
- **Locks (O3).** `RouterOperationLock` is `lockx.Keyed` at `LevelDevice`, key
  = type + endpoint without trailing slashes (Python's key), writes detach and
  wait ≤ 10 min → Chinese 409 conflict, reads keep cancellation, held context
  propagated, no order violation. Python's RLock re-entrancy is not used by any
  Python caller (all `with router_operation_lock` blocks are flat), so the
  non-reentrant L2 is safe; callers must not nest it (documented).
- **Readback cost.** One linear pass per proof; no per-item rescans.
- **Credentials.** No logging in R06; transport errors carry only the class;
  fingerprints exclude the password; with F2 the password (plain or encoded)
  is still never shown.
- **Deferral to R15 is honest.** The L1 job lock (R14 `job_lock`) and the
  routers HTTP package do not exist; the 409-before-400 order (X04), router
  lookup/`current != router`, fingerprint confirmation, auth/profile and the
  Flask↔Go differential of R019–R021 all live in the handler. Nothing in the
  three ported files that is testable without them was skipped; the porter did
  not register fake handlers.
- `-race` on routers/devhttp passes (TestR06ConnectionLock exercises the lock
  from two goroutines).

## Accepted differences (porter's list, re-checked)

Native opaque revision/fingerprint hashes; malformed RouterOS composite comment
shown empty (read-only); add identity fields accept absent/null/"" only; RouterOS
recreation needs a valid fresh `*hex` ID; L2 write wait ≤ 10 min; duplicate iKuai
tagnames refused before writing. Changed by this review: **password** echoes (not
username echoes) are hidden (F2) — still an O9/O12 behaviour difference to Python.

## Requirements on later modules

- R15: map `RouterOperationLock` errors (write: `KindConflict` 409; read: the
  context error of a disconnected client); take L1 before lookup/body checks for
  R020; rebuild the plan from rows read under L2 and compare the fingerprint
  before `ExecuteRoutePlan`; never pass a client-supplied plan.
- Any test seam that overrides a production parameter (timeouts, sizes) must be
  complemented by at least one test on the production value (F1 was hidden by
  `r06ShortHTTP`).

## L5 lab items (added to L5-A; do not block `verified`)

5. A RouterOS CHR whose management link is cut after it accepted a manual
   PATCH/PUT/DELETE: the edit ends after the 10 s timeout as an uncertain write,
   reconciles by readback and releases the router lock (F1).
6. RouterOS 7.24/7.25.1 REST GET/DELETE without a body (now identical to
   Python's requests) on HTTP and HTTPS (F3).

## Codex-port quality (for the coordinator)

Right: the hard HR classes — ownership proofs (fresh ID / unique random name +
all fields), ID reuse, refused vs uncertain, restore verified by identity,
unchanged-table short cut, detachment on every write entry point, linear
readback, Chinese `RouterError` on every failure path, deterministic name seam
instead of a global hook, an honest R15 deferral, and a Python-compared corpus
with mutation checks. Wrong: (1) a production parameter (device timeout) that
its own test seam overrode, so the corpus could never see it — high; (2) a
self-introduced safety deviation (redaction) designed too broadly, without
checking it against the fixed message texts; (3) an implicit shared-client
behaviour (typed nil → JSON `null`) inherited from R04 and invisible to fakes
that normalise bodies — checklist class 1 asks to look for exactly this; (4)
Python dict-merge emulation through JSON maps. Classes (1) and (3) are new
checklist items: **"test seams must not hide production parameters"** and
**"compare raw wire bodies/headers at least once, not only decoded bodies"**.

## Commands and results

Before the fixes (`.63`, new tests against the ported code):

```
--- FAIL: TestR06PythonBoundary/ikuai/readonly/interface_number
    Python reason "路由字段 interface 类型无法识别；路由接口不完整"  Go "路由字段 interface 类型无法识别"
--- FAIL: TestR06RouterOSRequestsKeepDeviceTimeout/{add,update,delete,update_compensation,delete_compensation,add_compensation}
    PUT …/rest/ip/route used timeout 0s, want 10s   (PATCH/DELETE likewise)
--- FAIL: TestR06RouterOSUnansweredWriteTimesOut (30.03s)
    manual RouterOS write without a device answer did not time out
--- FAIL: TestR06RouterOSBodylessRequests
    GET sent body/content type "null|application/json", want none
    DELETE sent body/content type "null|application/json", want none
--- FAIL: TestR06RedactionHidesOnlySecrets/username_in_permission_help
    message = "路由操作失败：设备回复包含凭据，已隐藏详情，请检查路由器", want "RouterOS 拒绝访问，…read、write、api、rest-api…"
--- FAIL: TestR06RedactionHidesOnlySecrets/username_echoed_by_device
```

After the fixes, final authoritative run on `.32`:

```
gofmt -l internal/ tools/                                   (no output)
go vet ./...                                                ok
go test -race -count=1 ./internal/routers/ ./internal/devhttp/
  ok  configflow/internal/routers  264.823s
  ok  configflow/internal/devhttp  3.488s
go test -p=4 -count=1 ./...                                 exit 0 (68 ok; routers 163.245s)
CONFIGFLOW_DIFF=1 go test -tags=configflow_difftest -p=4 -count=1 <9 difftest packages>   9 × ok
server/scripts/proddiff.sh    self_check PASS, coverage_regressions 0, baseline: new=0 stale=0
python internal/routers/testdata/r06_mutations.py           result/request/ownership DETECTED
go test -run TestR06 -v ./internal/routers/                 172 real Python boundary cases, PASS (33.250s)
```
