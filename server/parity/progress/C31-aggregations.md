# C31 — aggregations and device notes

Branch `go-port-c31`, private worktree `go-review-c30`. Base `go` at `0b367ae5` includes A11 and N8-3. Do not touch `go-fix-importer-bounds` or `go-port-a11`. No subagents/pushes. Chinese hand-back.

## Current state

- All 11 methods implemented and registered in `internal/api/aggregations`; native generator in `internal/aggregations` uses immutable main capture, bounded eight-way fetch, staged cache updates and all-or-nothing provider publication. Opaque node conversion materialized once across main/provider. Every caller-owned JSON document remains owned.
- `internal/devicenotes` owns complete validation, legacy fallback, hidden/removed names, revision and policy edit plans. Repo and oxidns delegate; name-only saves retain the settings object. No existing agents/routers private note consumers remain (their later client/policy route owners can use this shared API).
- Real Flask HTTP response/storage/file comparison covers all methods, errors, auth, HEAD body/no-body, reorder/clone guards, preview/provider formats/filters, source errors and global bundle activation. Real Python dry provider and device-notes comparisons pass. Scalars/unknown protocol fields and full-capacity notes included. Python test mapping is in PROGRESS-config C31.
- Post-A11 integration, full error diagnostics compare strictly with real deploy.Locations; the old location-only deferral is deleted.
- Proddiff independently generates all providers: aggregation 2/2 and Agent bundles 3/3, no frozen/partial inputs. Manifest size/hash verified against each side's real bytes before structured main/provider YAML comparison; other artifacts byte-exact. Raised only observed output/route coverage floors; no difference baseline or normalization-rule changes.
- Current feature work complete. Remaining: final combined .64 gates, report, final rebase/clean check. Status must become ported, never self-verified.

## Findings and decisions

- Fixed dangerous method fallthrough found during port audit: implicit HEAD with JSON could reach POST/PUT. Explicit gates preserve Python 500/no-catalog-write; all side effects compared. Common Step B #3 JSON error media type migration remains asserted.
- Python preview re-reads the artifact after publication; Go returns the validated snapshot so concurrent artifact replacement cannot mix preview nodes and counts. Regression and behaviour-changes entry present.
- Python dry generator treats a scalar subscription string as character IDs. Native selector decoding rejects incorrect types; one precise named oracle difference asserted with unchanged state. Normal public save contract already requires arrays.
- Proddiff's old `aggregations` catalog lookup silently reported no aggregation samples; corrected to `subscription_aggregations` in both direct and HTTP selectors, regression proves enumeration.
- Native map key order required changing only repo normalize_notes golden output to structural comparison. Values/types and all Chinese errors remain strict; other helper goldens untouched.
- Aggregation fuzz first found a test-only nil-vs-empty bytes false positive, not mutation. Switched to bytes.Equal and seeded empty input. Notes sustained 60s fuzz passed; repeat both in final gate.
- Differential normalization now only touches validated timestamp strings and generated UUIDs in identity/order fields; native regex diagnostic suffix alone is normalized. Negative control preserves arbitrary payload values/types. No A11 diagnostic masks.

## Validation history and next gate

Initial .63 extraction gofmt/vet/selected C05/C22-C26 references passed; .63 disappeared and was later retired. .32 old-base scoped race/full ordinary suite/all 12 enabled differentials passed; first full run's key-order golden failure was corrected and rerun. Changed collaborator race passed including oxidns 116.014s, convert 94.936s, provider 104.302s, repo 39.815s; C31 Flask race 66.022s. New user policy makes .32/.64 equivalent; final pinned host is **192.168.0.64**.

Post-rebase .64 strict C31 Flask differential passes 15.791s, then tightened normalization/scalar corpus passes 15.269s. Proddiff passes new=0 stale=0, self-check PASS, aggregation 2/2, bundle 3/3. Synthetic proddiff comparator/inventory suite 34/34; metadata corruption checks pass. Final command must run gofmt, whole vet, every changed package race, full go test, every CONFIGFLOW_DIFF difftest package, both 60s fuzz targets and proddiff. Record exact commands/tails in C31-HANDOFF.md, then final rebase go and clean worktree.
