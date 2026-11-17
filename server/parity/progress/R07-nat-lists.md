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
Done step 2: d6302eb1. Address batches + NAT apply/restore implemented; .32 vet all and 30 successful apply/roundtrip + 24 NAT failure + 31 address-write boundary cases pass, comparing outcomes/journal/full requests/final tables. Batches cover 200/201/450/3000 members and 560 legacy rules. Script text is exact except R04 NAT console-add field order (parsed to commands, all parameters retained). Redact password/URL-encoded password from device diagnostics; accepted security correction vs Python (O9/O12).
Next: remaining helper/rollback/adversarial coverage, mutation gate, .32 full gates/proddiff and rebase go.
Commands: `rt -- bash -lc 'cd server && gofmt -w internal/routers/addresslists.go internal/routers/routerfake/addresslists.go internal/routers/routeros.go internal/routers/routerfake/fake.go internal/routers/messages.go && go vet ./internal/routers/... && go test -count=1 ./internal/routers/ ./internal/devhttp/'`.
