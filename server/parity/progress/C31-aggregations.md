# C31 — aggregations and device notes

Branch: `go-port-c31`, private worktree `go-review-c30`. Do not move or edit `go-fix-importer-bounds`; coordinator owns its integration. No subagents/pushes. Chinese hand-back.

## Brief and scope

Port all 11 routes in backend/routes/aggregations.py (including explicit-profile public provider), complete backend/common/device_notes.py, replace repo/oxidns name normalization copies with a shared leaf implementation, switch existing agents/routers consumers where present. Register aggregation generation in proddiff and note that A11's frozen aggregation input can be removed; do not edit go-port-a11. Python sources fully read before implementation. C30/C26 are integrated prerequisites.

## Implementation and remaining work

- First compiling step: native `internal/devicenotes` handles stored notes, legacy policy fallback, empty-name hiding, policy edit plans and revisions; repo/oxidns delegate to it. Policy-row parsing also shared so no second name-validation algorithm remains. Oxidns keeps adapters and existing policy application; rename-only saves do not regenerate native settings.
- Existing Go agents/routers currently have no device_notes consumers; Python policy_workspace/client_devices consumers belong to modules not yet ported. Shared APIs are provided for them; no private note copies will remain. Recheck call sites at final rebase.
- Parse/fuzz and boundary tests being added. First .63 gofmt/vet and existing C05/C22-C26 reference tests pass (oxidns 0.566s). Need real Python device-notes differential including mutations and revision semantics.
- Remaining: aggregation native renderer and provider hook, 11 routes, full Flask↔Go success/error/write-state differential, generator structural comparison/fuzz, registration conflict tests, proddiff inventory fixes, Python test mapping and final .32 checks.
- Important discovery: proddiff currently enumerates `aggregations` instead of `subscription_aggregations`; fix generator and HTTP selector enumeration, not just the missing hook label.
- Generator must use existing provider immutable capture/bundle/commit, preflight before fetch, serialize/conversion once, bounded 8-way source collection, deferred cache updates and all-artifact validation before publication. URL token auth remains profile-only public bypass, global path JWT first.

## Acceptance to complete

Iterate pure/unit on .63; real loopback sockets and final verification .32. Final: gofmt, go vet ./..., changed-package race, full go test ./..., every enabled difftest package, proddiff new=0 stale=0. Map test_aggregation_reorder.py, test_dialer_rendered_bundle.py, test_shared_resource_routes.py in PROGRESS-config. Commit explicit paths, `go(aggregations): ...`, update this file in every commit; final rebase go, clean worktree, no push. Report route completion, differences/Python bugs, severity-ranked issues, exact commands/tails and commit ids.


## Validation so far

- .63 first extraction: gofmt and whole-tree `go vet ./...` pass; selected C05/C22–C26 reference tests pass (oxidns 0.566s). .63 then became unreachable; use .32 until it returns.
- .32 `go test -race -count=1 ./internal/devicenotes/` passes 1.017s; `go test -count=1 ./internal/oxidns/ -run "TestBoundaryEditorRecorded|TestBoundaryCommonRecorded|TestC22C26ReviewReferences|TestC05ReviewSettingsReferences"` passes 23.380s.
- Notes table cases cover bad roots/addresses/types/control characters/canonical collisions, legacy DEL repair, empty-name hiding, removal carryover, owned snapshots and zero-value safety. `FuzzStoredNotes` seeds added; sustained fuzz and real-Python notes corpus pending.
- Current go has no device_notes call sites in agents/routers yet (Python policy_workspace/client_devices are later owners); no existing Go private notes copy there. The shared API and oxidns wrappers cover their future integration. Existing repo and oxidns copies are replaced, not retained.

## Second compiling step

All eleven routes and a native aggregation renderer now compile and register in the full server; renderer wired to immutable provider capture/bundle/atomic commit. Node conversions retain one delivery scope across main and aggregation rendering. Shared notes fixes now preserve Python's lazy legacy validation for nonempty edits; typed policy edits no longer encode/decode themselves for validation.

- .32 `go vet ./...` passed with route integration; full convert/provider tests passed 16.934s/12.646s.
- Real-Python notes differential passes (1.055s): valid/invalid notes and policies, normalization, empty-name hiding, removed-row carryover, capacity, revisions, route edits and exact settings identity for name-only saves. Generated native drafts compared after YAML parsing.
- Flask↔Go differential currently covers catalog/reorder/provider/preview/auth, statistics stripping, stale-profile catalog access, protected delete, synthetic opaque conversion and global bundle validation. Basic paths pass; finishing broader bundle corpus and source-clock fixture alignment.
- First actual .32 proddiff passes `new=0 stale=0`, all self-checks pass, aggregation-provider now HTTP-compared **2 cases, 2 successes** (previous inventory used the wrong `aggregations` key and incorrectly reported zero). Both direct worker and HTTP inventory use `subscription_aggregations`; synthetic inventory regression added. A11 can remove its frozen provider byte input after C31 integration; do not alter A11 branch here.

## Third compiling step

- Flask↔Go catalog/provider/generator corpus now passes (13.826s). Added true full-server registration test (11 methods, static reorder path and profile alias), immutable-main chain activation and duplicate rejection across all aggregations in both orderings. Dry provider corpus compares real Python YAML structures, order, source statistics and all persistent effects; Unicode regex, invalid regex/type, chained nodes, reserved/duplicate names and malformed provider YAML included.
- Core collection test checks bounded 8-worker fanout, stable subscription order, cancelled caller completion and no input mutation. Main+provider public generation test proves one opaque URI conversion across a delivery (real existing Sub-Store adapter, fake loopback transport).
- Route fallback test proves authenticated last-good artifact fallback, singleflight for repeated requests, rejection before render, no YAML fallback for Surge, completion despite request cancellation and background artifact publication. Existing provider/convert suites remain green.
- Python preview reads the provider file after generation and can observe a concurrent replacement inconsistent with its counts. Go preview uses the validated result's proxies directly; document this reliability fix in behaviour-changes and final handoff.

The renderer/routes now pass whole-tree vet and the native registration/delivery/core tests; proddiff's synthetic comparator/inventory suite passes all 32 tests. Route state updated to 11/11 in routes.tsv; C31 stays wip until final acceptance and independent review (porter never self-verifies). Added deterministic route regression: zero fallback deadline, one shared upstream fetch, old aggregation artifact fallback, rejected token never reaches rendering, cancelled Surge waiter completes and background last-good publication is awaited. Main+provider conversion occurs once.
