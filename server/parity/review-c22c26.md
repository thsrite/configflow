# C22–C26 coverage audit (2026-11-17)

Branch: `go-review-c22c26`, starting at `go` / `dc3453c2`.
Decision: the Python files assigned to all five modules were covered by the
S-oxidns source/branch review. This audit makes that scope explicit, checks the
current call sites and adds fresh strict boundary references. All requested .32
checks passed. All five modules are verified in their assigned library scope;
no new runtime defect or open library finding remains.

Evidence commits on the integration branch: `672fc0d0` (S-oxidns adversarial
fixes/reference corpus), `0164424d` (Chinese diagnostic constants), `da0aa993`
(leading-newline YAML fix and race workload), `dc3453c2` (accepted follow-up).
The older hashes in the S-oxidns diary were rewritten by integration rebases.

## Source and branch crosswalk

This table records the actual feature walk, rather than inferring coverage
from a shared package name or the existence of passing tests. The Python file
names below are relative to `backend/`; Go names to `server/internal/oxidns/`.

| Module / Python source | Current Go | Branches checked in S-oxidns and confirmed in this audit |
|---|---|---|
| C22 `converters/oxidns.py` | `converter.go`, shared `ruledns.go` | Template/native mode; active/disabled/unknown rules; domain/IP/classical grouping, aggregation and pinned actions; upstream/fallback formats; custom fragments and Hosts; AdGuard/attachment resources; downloads; validation before delivery and no mutation of profile inputs. |
| C22 `common/oxidns_native.py` | `native.go`, `node.go` | Root/key/type/duplicate/size/depth/node-count limits; aliases and merges; include expansion, origins, cycles/missing/non-UTF-8 files; relative/reserved/duplicate paths, text/base64 attachments; resource existence and native configuration validation. The S review fixed lost comments/metadata, merge precedence and encoder error propagation. |
| C22 `common/oxidns_references.py` | `references.go` | Tag references in every scalar/container path; escaped/similar tags, expression boundaries, dynamic references, reason formatting. Effective merge fields are traversed, overridden inherited fields are not live references. |
| C22 `common/oxidns_match_targets.py` | `matchtargets.go`, `references_validation.go` | Known provider types, malformed/unknown/duplicate tags, reference issue filtering and target/tag result ordering. |
| C23 `common/oxidns_validation.py` | `validation.go`, `editor.go` | Schema type/enum/union/required/range/pattern/formats; native load failure versus structured issues; expanded include source paths; max issue count; incomplete legacy drafts; public ValidateSettings through PrepareEditor. Embedded catalog equality and absent source metadata have S regression tests. |
| C23 `common/oxidns_plugin_constraints.py` | `constraints_plugin.go`, `zone.go` | Every plugin constraint family: default/null/deferred values, integer/bool distinctions, lower/upper relations, durations and timeouts, upstream/bootstrap/TLS/outbound, domains/regex/cron, zone/RDATA/directives and AdGuard. Unicode zone names were freshly compared to real Python; broad CJK acceptance was removed. |
| C23 `common/oxidns_global_constraints.py` | `constraints_global.go` | Global log/API/runtime/network limits, optional/null/deferred fields, TLS and credential combinations; checks are reached through ValidateNativeConfig/ValidateExpandedDocument, not a replacement copy of the old private helper. |
| C23 `common/oxidns_reference_validation.py` | `references_validation.go` | Provider/executor kinds, quick expressions, match/exec paths, dynamic and absent references, disabled versus active graphs, DFS cycles and source ordering; invoked by native validation and ReadMatchTargets. |
| C24 `common/oxidns_common.py` | `common.go` | Read/apply value validation and capabilities; renamed/included/shared/foreign ownership, call insertion and disabled-feature parking/restoration; fallback snapshots; API/TLS/cache dump; dynamic domain learning; Hosts/AdGuard resources; manual/scheduled rule updates and preservation of unrelated native content. All collaborators are now real Go calls, with no pending stubs or recorded collaborator substitution. |
| C25 `common/oxidns_common_routing.py` | `routing.go`, shared `ruledns.go` | Recognition early exits and complete graph state; catalog grouping/legacy migration; provider/helper ownership, custom/aliased chains, selectors/action/follow/parked reconciliation; before-profile/notices handling; remove-rule; readers/globs/download updates, helper keep-closure and insertion order. |
| C25 `common/dns_watchdog.py` | `watchdog.go` | Deep-copy/no draft mutation; unresolved includes/environment entries; plugin/list/tag validation; all UDP/TCP/QUIC/HTTP listeners; exact guard dedupe/move, wrapper collision/reuse/order and repeated invocation. The added 41 real-Python boundary cases exercise these branches directly. |
| C26 `common/oxidns_common_updates.py` | `updates.go` | Withdrawal versus live/custom/glob readers; malformed/custom downloaders; owned provider/reload/job pipeline stop/restore; known/manual/generated sources; URL/profile/token rebasing; enabled/disabled rule update and conservative retention. The private context helpers run through complete public common/editor/update operations. |
| C26 `common/oxidns_device_policy.py` | `devicepolicy.go`, shared `clientrange.Normalize` | Address/range canonicalization, limits/names/modes/duplicates; host-only versus prefix specificity; recognizable main/local upstream; owned-hook position/content/extra references; all listener/side-port/indirect/env refusals; saved node-test context and isolated legacy chains; read-only editability reports; apply/replace/revoke with metadata and draft preservation. New strict references compare return values and every input after success/failure. |
| C26 `common/oxidns_editor.py` | `editor.go` | Native open without inactive-template validation; template conversion and legacy backup; inactive rules/parked choices; old parsed/string/absent baselines; type-sensitive scalar/mapping/plugin/atomic-array three-way merge; explicit conflict choices and escaped paths; resource unions/referenced-content retention; proposed/baseline/action/follow metadata; no state write. |

The early C26 address/policy normalizers assigned originally to C22 and early
C27/C29 pieces used internally are included above. S-oxidns also reviewed
`oxidns_settings.py` and the read side of `device_notes.py`; this does not transfer
ownership of the remaining C27/C29/C31 modules.

## What the current tests actually execute

`boundary_test.go` selects public entry points from the original Python recordings.
The files also contain private-helper rows that are intentionally not replayed
after the native rewrite. They are not counted as current public-boundary checks.

| Module | Selected recorded rows | Selected randomized rows (ordinary tests) | Current tests / additional evidence |
|---|---:|---:|---|
| C22 | 13,245 / 29,539 | 41,217 / 60,870 | `TestBoundaryGenerationRecorded/Corpus`, `TestReviewSReferences`, YAML/merge/limits/schema regressions in `review_s_test.go`. Generation 1,630, includes native_document 341, custom_files 504, downloads 488; real ippresence hook is installed by `hooks_external_test.go`. |
| C23 | 1,330 / 2,043 | 21,700 / 31,978 | `TestBoundaryValidationRecorded/Corpus`; global/reference constraints also run through the complete validator, C22's 762 ReadMatchTargets calls and native generation. `TestReviewUnicodeWaiver`, embedded schemas, expanded-source and fresh IDN-zone references. |
| C24 | 822 / 822 | 3,746 / 3,746 | `TestBoundaryCommonRecorded/Corpus` (280 reads + 542 applies), using complete real C25/C26 implementations; editor preservation tests cover common-field writes. |
| C25 | 3,525 / 15,376 | 13,404 / 18,333 | `TestBoundaryRoutingRecorded/Corpus`: 984 reads, 246 applies, 613 migrations, 107 watchdog transformations, ownership/resources/removal; private `_references` and `_prefix_end` rows are excluded, their behavior is reached through public graph/migration/policy operations. |
| C26 | 3,072 / 4,048 | 4,551 / 4,882 | `TestBoundaryEditorRecorded/Corpus`: 1,766 prepares, 583 rule updates, 207 policy reads, 86 writes, previews/withdrawals/rebases/inspection; `_merge` and context-helper rows are excluded, exercised via public editor/common/update calls. |

Existing per-module Python-test-file lists in PROGRESS remain the coverage index;
their route-only assertions are not claimed as library acceptance. The optional
real-core tests (`test_oxidns_domain_only_core.py`,
`test_oxidns_device_policy_core.py`, `test_dns_watchdog_core.py`) require isolated
OxiDNS/MosDNS binaries and are not claimed as executed here. Core/Agent runtime
and lab acceptance remain separate gates.

The race workload samples only four randomized corpora (per function/outcome
first eight plus stable 1/16); ordinary tests run all selected rows. Recorded
rows, the full selected validator corpus, 6,304 storage round trips and all new
94 references remain complete under race.

`review_c22c26_gen.py` invokes real Python public entry points under a temporary
DATA_DIR. `review_c22c26.jsonl.gz` has 94 rows (26 rejections): 7 native loads,
7 native validations, 41 watchdog cases, 10 common read/apply, 22 policy
read/apply, 6 conflict previews and 1 native-open case. `TestC22C26ReviewReferences`
checks structural results, Chinese errors and every argument's post-state,
including rejected calls; input strings (including native YAML) compare byte
for byte, without YAML canonicalization. It has no accepted-difference waiver. It supplements
the S source review rather than claiming branch completeness from 94 examples.

## Explicit ownership boundaries

- C22–C26 contain **no HTTP route registration, repository writes or background
  goroutines**. `ApplyPolicies` produces a new settings object and template
  metadata; its “stores rows” comment refers to fields of that returned draft.
  Python's function has the same contract. It does not save a profile or enforce
  a transaction token. `Revision` is covered as a deterministic ephemeral token;
  the actual write-lock/If-Match check is not claimed here.
- `backend/routes/oxidns.py` and its 18 routes belong to **C29**, still missing:
  there is no `internal/api/oxidns`, no registered route and no runtime import of
  `internal/oxidns` by existing API packages. C29 auth, headers, ETag/If-Match,
  cancellation, saved effects and profile wrappers need its own HTTP differential.
- Client-device/policy-workspace writes belong to **C31** and agent/router policy
  handlers to their route owners. No absent Go write path is marked reviewed.
  Existing repo compatibility normalization is covered by repository tests; it
  does not implement those route transactions.
- Python `common/dns_watchdog.py` is a 94-line pure configuration transformation,
  not a monitoring loop. Bundle injection and rule-sync eligibility are Agent
  **A01/A14/A16**; probe/restart behavior is in the separate Go Agent's
  `routes/dns_watchdog.go`. Neither their runtime nor live DNS behavior becomes
  verified by setting **C25** verified. C25 verifies the generated guard contract
  only (name, exact rule, local short-circuit, wrapper order and idempotence).
- Proddiff compares snapshot public generators (`oxidns`, agent-oxidns, downloads
  and custom files) and implemented routes. Its skip of absent C29/C31 routes
  cannot establish coverage for those missing endpoints. Snapshot contents stay
  on the server; only schema-level summaries are reported.

## Commands / result

Fresh synthetic references:
`rt --back server/internal/oxidns/testdata/review_c22c26.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/review_c22c26_gen.py'`

Targeted first run: 94 references, `ok configflow/internal/oxidns 0.201s`;
complete post-state checks: `0.160s`. After strengthening input text comparison
to exact strings, gofmt/vet and the targeted race test passed (`1.976s`).
This last change only strengthens the test assertion; no runtime code changed.

Final .32 gate (exit 0, shell also captured uptime and `/tmp/c22c26-final.log`):
`rt -- bash -lc 'cd server && gofmt -l . && go vet ./... && go test -race -count=1 ./internal/oxidns/ ./internal/api/... && go test -count=1 ./... && cd .. && server/scripts/proddiff.sh'`

- gofmt had no output (the shell additionally asserted an empty listing); vet passed.
- Target race: `ok configflow/internal/oxidns 298.543s`, `ok configflow/internal/api/agents 15.466s`, `ok configflow/internal/api/domaindiscovery 1.434s`, all other API packages passed (last `ok configflow/internal/api/tasks 1.314s`).
- Ordinary full suite: `ok configflow/internal/oxidns 98.168s`, `ok configflow/internal/installer 156.057s`; final lines `ok configflow/internal/weburl 0.015s`, `ok configflow/internal/yamlx 1.200s`, `? configflow/tools/proddiff/generator [no test files]`.
- Proddiff: all four self-checks PASS, 2060 cases / 236 compared, 5 existing unique baseline differences (`agents:value` 3, `config:value` 2), coverage regressions 0, `baseline: new=0 stale=0`. OxiDNS public config, agent config, files and downloads each had 2 successful comparisons. The existing 20 Python and 20 Go connection-failed cases are reported by the harness, not treated here as successful comparisons; no new baseline or coverage exception was added.
- Start uptime `16:42:49 up 1:41, 2 users, load average: 0.88, 3.66, 7.16`; end `16:50:57 up 1:49, 3 users, load average: 5.24, 6.88, 7.16`.
- Final exact-input assertion check: `rt -- bash -lc 'cd server && test -z "$(gofmt -l .)" && go vet ./internal/oxidns/ && go test -race -count=1 -run "^TestC22C26ReviewReferences$" ./internal/oxidns/'` → exit 0, `ok configflow/internal/oxidns 1.976s`.
- Final integration base `869db198`: only HANDOFF and the routers differential matrix changed after the tested base `b498951c`. Runtime, dependency and prior tests remain identical; the only local test change after the main gate is the stronger assertion, verified by the targeted race check above.

No new dependency, Python emulation, runtime behavior change or production access.
No pushes. C29/C31 and Agent runtime gates retain their original owners/statuses.
