# R07 — NAT address lists

Status: **verified** after the fresh Opus HR review ([../review-r07.md](../review-r07.md); 5 findings fixed on branch go-opus-review-r07, rebased on go incl. the R06 fixes). Ported on branch go-port-r07.
Scope: routeros_nat_list.py, ros_address_lists.py, routeros_test_helpers.py. No routes, no subagents, no push.

Design follows R03 typed rows and R04 driver/handler registry. Typed list plan, resource/check and journal structs; open RouterOS rows retain unknown fields. No jsonx, new dependencies, locks or goroutines. Driver owned by one operation; outer R12/R14 owns L1/L2 locks. Read context cancellable; exported writes detach and each device request retains 10s timeout. Cache proof lives only in one explicit reuse scope, invalidated before any destructive operation.

R04 issue-class prevention:
- F1: validate indices/counts/IDs; malformed replies fail closed and journal only proven new IDs.
- F2: Chinese messages centralised; compare real Python errors and rollback messages.
- F3: write and rollback entry points detach cancellation, bounded I/O and settle waits.
- F4: address creates/removes and legacy NAT restore use batches, not per-member REST.
- F5: dynamic/timeout/disabled list entries rejected before write; dynamic objects never modified.
- F6: journal before delete; restore checks current table and records all removed rows even after failure.
- F7: deep comparisons via sameRow; no map equality on interface values, no panic paths.
- F8: 200 members/16KiB address scripts; 500-ID removes; R04 NAT 100/32KiB restores.
- F9: every destructive write checks preview resource ID + full stable config; missing proof refuses.
- F10: reuse configROSRow/payload/sameRow/ScriptString/putNAT, no private emulation helpers.

Done: address proof/read layer and stateful fake scripts; 24 Python boundary comparisons (digest/fetch/reuse across 0/1/499/500/501/2000/2001/4500 members), content-mutation check. .32 go vet ./... and existing routers/devhttp tests pass.
Done step 1: 9df951e5. Typed NAT planner/read integration: 135 boundary scenarios compare real Python plans and exact request sequences, all pass on .32. Covers no-list paths, adopt/unknown, merge/stale/retained, foreign/dynamic/disabled/reference/shape/order blockers and grow/shrink/unchanged membership.
Done step 2: 2f1d0ee6. Address batches + NAT apply/restore implemented; .32 vet all and 30 successful apply/roundtrip + 24 NAT failure + 31 address-write boundary cases pass, comparing outcomes/journal/full requests/final tables. Batches cover 200/201/450/3000 members and 560 legacy rules. Digest/fetch/names script text is exact. Address/NAT add parameter order is immaterial: full grammar and all values/address order/anchors are compared, with independent Python interpreter checks. Redact password/URL-encoded password from device diagnostics; accepted security correction vs Python (O9/O12).
Done: helper boundary cases (removals-first, restrict egress, enrolled member-only update, nested migration, policy address split, baseline, locate and occupied-name reservation) and 5 external-mutation/restore fault cases pass on .32. Fake mutations preserve allocated IDs and transcript. No open differential mismatch.
Done: independent Python script-interpreter cross-check; request-cancelled apply and rollback; UTF-8 script byte limits; all-member delete guard; partial address recreation preserves new IDs; deep-copy proof cache; ambiguous/dynamic sweep refusal; wrong-table undo rejection; secret diagnostics; malformed proof/page fallbacks; 600-member pruning/withdrawal and narrowed-rule roundtrips. Deliberately wrong Go result was detected (expected test failure). A fault fixture initially reused *F1 at 200 members; corrected to *F0001, with duplicate-ID behaviour covered explicitly as fail closed.
Next: fresh Opus HR review; R08 typed adapter replacement; R12/R14 integration and L5 lab acceptance.
Commands: `rt -- bash -lc 'cd server && gofmt -w internal/routers/addresslists.go internal/routers/routerfake/addresslists.go internal/routers/routeros.go internal/routers/routerfake/fake.go internal/routers/messages.go && go vet ./internal/routers/... && go test -count=1 ./internal/routers/ ./internal/devhttp/'`.

## Coverage and acceptance

The Go tests call real Python RouterOS entry points against the same loopback fake, reset to identical synthetic tables for each side. Comparisons cover results, Chinese errors, journals, all request methods/paths/query/body values and final ordered tables. Digest/fetch/names scripts compare literally; complete grammar-checked address batches and R04 NAT console add scripts compare parsed commands with every field/order/anchor retained. No broad ID or value normalization; all R07 comparisons retain null-versus-missing fields (the earlier R04 stripNulls helper is not used). Mutation modes cover independently corrupted result, undo journal, request method and final table; each must fail a real Python boundary comparison. A separate Python-helper interpreter runs captured console scripts against seeded tables (including 501 rows, CJK and separator values), independently checking fake output and effects.

| Python test file (test functions) | R07 proof | Deferred integration (not R07 code omitted) |
|---|---|---|
| test_routeros_nat_list.py (64) | natlist_plan_test: new/synced/adopt/unknown/foreign/disabled/dynamic/reference/shape/order/legacy/stale/retained/grow/shrink; natlist_write_test: apply, roundtrip, partial creates/deletes, lost ack, failed restore, anchor loss/foreign refs/edited members; natlist_helpers_test: egress, early removals, policy migrations, exact verification; r07_safety: cancellation and fail-closed ownership | Complete route→NAT→return release and source-policy roundtrips require R08/R12. R07 stage deltas/ordering/compensation are directly compared now. |
| test_nat_list_unchanged.py (8) | plan boundary compares no-NAT/not-required/adopt/unknown reads and active legacy migration; helper compares unchanged non-NAT operations | R08 policy bundle hashes, R05 iKuai dispatch, R12 full plan/fingerprint/release integration; raw Python hashes are not Go identities. |
| test_ros_address_lists.py (14) | addresslists_test: 0/1/499/500/501/2000/2001/4500, digest/runtime/order, exact scripts/cache; r07_safety: separators/changing pages/malformed digest/deep cache ownership; read helpers: occupied names/moved IDs/20-ID boundary | Source-policy and DNS-guard consumers' full preview/withdrawal sequences require R08/R09/R12; the shared read/invalidation APIs are ported. |
| test_ros_policy_bulk.py (16) | AddressWriteBoundary: 1/3/200/201/450/3000, partial/duplicate/lost/read errors/sweep; UTF-8 byte splitting, guarded delete, partial recreate/new IDs, original-order NAT batches | Two subtraction/performance test functions belong to R08 (ros_policy.subtract), not R07. Policy manifest remap/withdrawal activation belongs to R08/R12; R07 address helpers retain partial created IDs. |
| test_routeros_nat_list_sync.py (6; additionally read) | NATResources, NATMemberUpdate, NATMigration, early removals and list-only device deltas are compared at their boundaries | Receipt/worker/route-delta execution and pause messages belong to R14; R07 never performs auto migration. |

R07 route count: zero. No Python functionality dropped. No new dependency, jsonx adapter, flock, goroutine or persisted format. No L1/L2 lock is owned here: the existing driver is single-operation state, R12/R14 serialize a device with their job (L1) and operation (L2) locks; no validation/409 ordering is moved into this driver.

## Safety corrections versus Python (O9/O12, for fresh Opus review)

- Diagnostics redact the router password/username and URL-encoded forms; Python can echo them from device/console text. Device rows remain intact for exact ownership checks.
- Malformed digest fields produce RouterError rather than Python ValueError/crash. Oversize single address scripts/unsafe field keys/composite console fields are refused before I/O rather than exceeding the console cap or concatenating unsafe code.
- A rollback ownership failure on a new NAT rule stops subsequent member deletion, retaining what the changed rule still selects. Python's run_steps passed one delete_safe value across that later deletion. Error remains an explicit incomplete rollback.
- Sweeps never choose between duplicate IDs, dynamic/timed rows or more fresh matching rows than were requested. New IDs must be valid, absent before, uniquely present and match submitted values; ambiguous rows stay untouched. No destination/CIDR-only deletion.
- Runtime keys are excluded from restore payloads via configROSRow (shared R03/R04 rules). Cache rows are deep copies, including composite fields; array/object-valued REST fields always use full reads rather than inventing a RouterOS :tostr proof.

## L5 outstanding (only a CHR proves these)

D07/D08, G08, L5-A/B/I and review-r04 L5 1–9: RouterOS acceptance of 200-member/16KiB scripts and 500-ID removes; associative key/ID ordering and 500-row MD5 chunks with CJK/escaping; 2000-row print pagination and separator fallback; real permission groups and /execute output/ID format; short-lived invalid settling; large-list/legacy migration load; true firmware partial-delete ordering, link loss and restore anchors; original client IP/proxy and WAN packet paths at every transition, including gateway renumbering and policy withdrawal. TLS/credential encodings and real restart persistence stay in the shared R04 L5 checks. No lab or production device was contacted.


## R12 §2.4 对应关系

Coordinator explicitly approved the minimal shared contract on 2026-11-17. `plan_model.go` provides exactly the
§2.2 `Op` / `OpHead` / `Stage` fields and JSON tags, stage keys, and the needed `NATUpdateOp`; R12-1 extends it.

| Contract | R07 implementation |
|---|---|
| PlanNATLists → *NATListPlan, Ops []Op | Concrete *NATListSync / *NATUpdateOp, no raw operation JSON in the plan. NATListSync embeds OpHead. |
| NATRemovalsFirst([]Op, []Op, gateway) | Typed copies split early removals, preserving unrelated op identities and JSON. |
| RestrictNATEgress(*NATListSync, *NATListCheck, iface) | Mutates typed egress, replacement flag, removed ID check and OpHead.Summary. |
| VerifyNATLists(ctx, check, state) | Same signatures and complete reference/member checks. |
| InvalidateAddressReads / ReuseAddressReads | Exported invalidation and nested per-driver reuse scope. R12 invalidates before each op/batch. |
| Stage consumption | NATMemberUpdate([]Stage, baseline), NATMigration([]Stage); NATStage removed. |
| R08 not present yet | Approved read-only adapter in natlist_policy_adapter.go, header “R08 落地后移除”; only nested prepare_static migration counts. Required typed replacement/delete task in R08; clarification added to r12-release.md §2.4. |

§3–4: R07 owns no new lock; the caller's L1→L2 contexts are retained and writes/rollback detach cancellation with
per-request timeouts. One release must retain one driver for the proof cache and newly known IDs. A failed NAT
operation compensates inside Apply; an incomplete compensation is KindRollback, with its explicit failure text
(no duplicate/retried partial undo). R12 owns the stage loop: **every** error fails the stage, any UndoOf(err)
is recorded before failing, and nil/empty/null/{} undo records are skipped. The R07 restore handler returns {}.
R07 NAT lists have no ownership manifest (ownership is the unique rule shape and member snapshots); R12/R08 extend ManifestHolder for their own resources. R07 never suppresses an operation error; its successful undo journal is returned before a subsequent stage can
run. TestR12TypedNATContract checks typed dispatch, JSON field presence and exported invalidation; existing real
Python boundary tests continue to compare the same driver/API JSON after the model change.

R05 checklist: shared devhttp behaviour unchanged (explicit RouterOS auth still wins); every driver refusal is a
RouterError with Chinese constants; resource ID/config must be non-empty before destructive I/O; restore proves
stable identity before overwriting/deleting; row readbacks/positions and next surviving anchors are indexed once
(no per-item JSON scans). Uncertain writes are compensated inside the same detached driver operation.
A19 checklist: no YAML container/alias traversal or payload parser is introduced; address proof caches own deep
JSON copies and composite-valued rows use REST instead of lossy digest coercion; no silent unsupported shape.


## Added artifacts (lines)

| File | Lines |
|---|---:|
| `internal/routers/addresslists.go` | 401 |
| `internal/routers/addresslists_test.go` | 84 |
| `internal/routers/addresswrite.go` | 572 |
| `internal/routers/device_redact.go` | 20 |
| `internal/routers/natlist_helpers_test.go` | 266 |
| `internal/routers/natlist_plan.go` | 690 |
| `internal/routers/natlist_plan_test.go` | 146 |
| `internal/routers/natlist_policy_adapter.go` | 42 |
| `internal/routers/natlist_read.go` | 243 |
| `internal/routers/natlist_write.go` | 738 |
| `internal/routers/natlist_write_test.go` | 324 |
| `internal/routers/plan_model.go` | 68 |
| `internal/routers/r07_safety_test.go` | 300 |
| `internal/routers/r12_nat_contract_test.go` | 42 |
| `internal/routers/routerfake/addresslists.go` | 207 |
| `internal/routers/routerfake/mutation.go` | 40 |
| `internal/routers/routerfake_python_test.go` | 122 |
| `internal/routers/testdata/r07_fake_reference.py` | 36 |
| `internal/routers/testdata/r07_reference.py` | 64 |


## Final authoritative validation (.32)

All gates pass after the N8-2/storage/proxy upstream sync (base 3ae44741; R07 implementation unchanged by the
subsequent final rebase). Exact commands, run from this worktree:

```bash
rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/ tools/)" && go vet ./...'
rt -- bash -lc 'cd server && go test -race -timeout=30m -count=1 ./internal/routers/ ./internal/devhttp/'
rt -- bash -lc 'cd server && go test -p=2 -timeout=30m -count=1 ./...'
rt -- bash -lc 'cd server; mapfile -t r07_difftest_packages < <(go list -tags=configflow_difftest ./... | rg "/difftest$"); CONFIGFLOW_DIFF=1 go test -tags=configflow_difftest -p=2 -timeout=30m -count=1 "${r07_difftest_packages[@]}"'
rt -- server/scripts/proddiff.sh
```

Final tails:

```text
GOFMT_PASS                       (gofmt -l had no output)
VET_PASS
ok configflow/internal/routers   391.762s  (-race)
ok configflow/internal/devhttp   3.471s    (-race)
RACE_PASS
ok configflow/internal/webdav/difftest 0.007s
ok configflow/internal/weblistener 0.011s
ok configflow/internal/weburl 0.005s
ok configflow/internal/yamlx 1.359s
?  configflow/tools/proddiff/generator [no test files]
ALL_TESTS_PASS
ok api/agents/difftest 120.276s; api/subscriptions/difftest 92.282s
ok discovery/difftest 4.276s; health/difftest 14.846s; httpx/difftest 2.027s
ok mosdns/difftest 2.246s; refresh/difftest 3.783s
ok substore/difftest 5.851s; webdav/difftest 20.618s
DIFFTEST_PASS
baseline: new=0 stale=0
PRODDIFF_EXIT_0
MUTATION_DETECTED result
MUTATION_DETECTED journal
MUTATION_DETECTED request
MUTATION_DETECTED table
```

Strict null/missing plan/list comparison + TestR12TypedNATContract additionally passed (`ok routers 26.572s`).
Mutation command (each must exit nonzero **and** report an outcome/wire/table difference, not a build failure):
`CONFIGFLOW_R07_MUTATE=<result|journal|request|table> go test -count=1 -run 'TestNATListWriteBoundary/new/1/nat_apply$' ./internal/routers/` through `rt` on .32. All four were repeated on the final implementation.

The combined final run's local SSH process remained after the remote command disappeared; all test logs were
complete. The stale client was closed after a fresh keepalive-enabled SSH invocation confirmed proddiff again:
`ssh -o BatchMode=yes -o ConnectTimeout=5 -o ServerAliveInterval=15 -o ServerAliveCountMax=4 sunbroke@192.168.0.32 'cd ~/rt/.paseo__worktrees__302rx1dg__go-port-r07 && server/scripts/proddiff.sh > /tmp/r07-proddiff-confirmed.log 2>&1; r07_code=$?; tail -1 /tmp/r07-proddiff-confirmed.log; echo PRODDIFF_EXIT_$r07_code; exit "$r07_code"'`.
This confirmation exited 0 with `baseline: new=0 stale=0`; it was a transport cleanup, not a failed test.

`-p=2` bounds package concurrency without skipping tests. Proddiff has no public router plan exporter yet
(R12/R15); R07 device semantics are proven by its real-Python loopback boundary corpus and independent helper
interpreter. No production or lab device was contacted. No R07 feature is skipped; the integration and lab
items in the coverage map remain with their owning modules.

Final rebase check on .32: gofmt empty, `go vet ./...` pass, strict plan/list + R12 contract tests pass
(`ok configflow/internal/routers 27.750s`, `FINAL_REBASE_CHECK_PASS`). Final rebase also includes the coordinator-verified importer bounds change; R07 implementation files are unchanged.
