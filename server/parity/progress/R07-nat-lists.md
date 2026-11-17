# R07 — NAT address lists

Status: in progress; HR, must remain `ported` until fresh Opus review. Branch go-port-r07 from go.
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
Done step 1: cb64b567. Typed NAT planner/read integration: 135 boundary scenarios compare real Python plans and exact request sequences, all pass on .32. Covers no-list paths, adopt/unknown, merge/stale/retained, foreign/dynamic/disabled/reference/shape/order blockers and grow/shrink/unchanged membership.
Done step 2: d6302eb1. Address batches + NAT apply/restore implemented; .32 vet all and 30 successful apply/roundtrip + 24 NAT failure + 31 address-write boundary cases pass, comparing outcomes/journal/full requests/final tables. Batches cover 200/201/450/3000 members and 560 legacy rules. Digest/fetch/names script text is exact. Address/NAT add parameter order is immaterial: full grammar and all values/address order/anchors are compared, with independent Python interpreter checks. Redact password/URL-encoded password from device diagnostics; accepted security correction vs Python (O9/O12).
Done: helper boundary cases (removals-first, restrict egress, enrolled member-only update, nested migration, policy address split, baseline, locate and occupied-name reservation) and 5 external-mutation/restore fault cases pass on .32. Fake mutations preserve allocated IDs and transcript. No open differential mismatch.
Done: independent Python script-interpreter cross-check; request-cancelled apply and rollback; UTF-8 script byte limits; all-member delete guard; partial address recreation preserves new IDs; deep-copy proof cache; ambiguous/dynamic sweep refusal; wrong-table undo rejection; secret diagnostics; malformed proof/page fallbacks; 600-member pruning/withdrawal and narrowed-rule roundtrips. Deliberately wrong Go result was detected (expected test failure). A fault fixture initially reused *F1 at 200 members; corrected to *F0001, with duplicate-ID behaviour covered explicitly as fail closed.
Next: authoritative .32 race/all/difftest/proddiff on rebased go, final evidence and clean-tree rebase.
Commands: `rt -- bash -lc 'cd server && gofmt -w internal/routers/addresslists.go internal/routers/routerfake/addresslists.go internal/routers/routeros.go internal/routers/routerfake/fake.go internal/routers/messages.go && go vet ./internal/routers/... && go test -count=1 ./internal/routers/ ./internal/devhttp/'`.

## Coverage and acceptance

The Go tests call real Python RouterOS entry points against the same loopback fake, reset to identical synthetic tables for each side. Comparisons cover results, Chinese errors, journals, all request methods/paths/query/body values and final ordered tables. Digest/fetch/names scripts compare literally; complete grammar-checked address batches and R04 NAT console add scripts compare parsed commands with every field/order/anchor retained. No broad ID or value normalization. A separate Python-helper interpreter runs captured console scripts against seeded tables (including 501 rows, CJK and separator values), independently checking fake output and effects.

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
