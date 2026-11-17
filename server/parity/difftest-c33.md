# C33 boundary coverage

Scope: 25 methods in `routes/rule_library.py` and `routes/proxy_groups.py`; public aliases are included. Synthetic stores and loopback HTTP only. Python source and the three named Python suites were read in full. Actual Go/Flask requests compare status, JSON, Content-Type/Cache-Control/profile headers, full exported document, SQLite sections/fields including positions, JSON/YAML/list files after every call. Native YAML and known Mihomo custom YAML compare structurally; user-comment preservation is asserted separately. No storage or configuration fields are skipped.

| Method/path | Success | Failure/edge | Writes compared |
|---|---|---|---|
| GET /api/proxy-groups | default list | JWT 401 | unchanged DB/files |
| POST /api/proxy-groups | health defaults + future fields | array 400 | complete selected profile |
| GET /api/profiles/<id>/proxy-groups | other profile | JWT 401 | unchanged |
| POST /api/profiles/<id>/proxy-groups | other profile creation | array 400 | selected versus untouched profile |
| PUT /api/proxy-groups/<id> | patch filters, rename, delete health field; template/native DNS follow; listener relink | ID mutation409, invalid filters400, locked native409 | profile+DNS once, shared unchanged, listener YAML |
| DELETE /api/proxy-groups/<id> | unreferenced group | missing404; referenced409 (Go boundary) | exact profile; atomic refusal |
| PUT /api/proxy-groups/batch-health-url | selected health groups | invalid/duplicate IDs400, missing404, ordinary group400, credential URL400 | only selected URLs; profile isolation |
| GET /api/proxy-groups/health-defaults | shared defaults | JWT401 (no domain failure) | unchanged |
| POST /api/proxy-groups/preview-regex | subscription/aggregation, exclusion, backtick buckets, source dedupe | invalid source400, wrong filter type400 | unchanged source/cache |
| POST /api/proxy-groups/reorder | IDs and legacy display objects | unknown404 | stored definitions, never display fields |
| GET /api/rule-library | annotated shared sources | JWT401 | unchanged |
| POST /api/rule-library | inline, downloaded, future data | malformed400, missing ID400, duplicate409, unavailable URL400 | catalog+stable/raw cache atomically |
| PUT /api/rule-library/<id> | metadata rename, content change, URL metadata, interval retention, native disable/restore/retype | invalid content400, missing404, failed fetch502, JWT401 | shared library+selected native profiles+cache+task history |
| DELETE /api/rule-library/<id> | unreferenced source | missing404, referenced409, JWT401 | catalog+cache; atomic refusal |
| GET /api/rule-library/<id>/draft-content | selected editor fields, usage, no-store | missing404, JWT401 | unchanged |
| PUT /api/rule-library/<id>/draft-content | exact content-only update | stale409, extra keys400, missing404, JWT401; concurrent200/409 | metadata/profiles untouched; stable cache follows DB |
| POST /api/rule-library/cache | inline + URL | no selection400, missing404, JWT401 | raw bytes/task rows; preferences unchanged |
| POST /api/rule-library/check-content | append YAML payload, classical raw edit | wrong content/append type400, diagnostic content issues | unchanged |
| GET /api/rule-library/content/<id> | public inline bytes | missing404, URL source400 | unchanged |
| GET /api/profiles/<id>/rule-library/content/<id> | public alias | missing404, URL source400 | unchanged |
| GET /api/rule-library/proxy-domains | current setting | JWT401 | unchanged |
| POST /api/rule-library/proxy-domains | trimmed setting | wrong type500 | unrelated settings retained |
| POST /api/rule-library/reorder | legacy display object/IDs | missing404 | only order, content retained |
| POST /api/rule-library/test | bounded HEAD, successful source | JWT401; failed source/concurrent edit (Go boundary) | fresh guarded disable; native parking |
| POST /api/rule-library/test-single | loopback200/404 status | empty URL400; JWT401 | unchanged |

## Python test ownership

| Python suite | C33 boundary evidence | Other ownership |
|---|---|---|
| test_proxy_group_batch_health.py (6 functions, parameterized) | `proxygroups/TestBatchRollbackAndProfileIsolation`, mutation/isolation tests, `TestProxyGroupHTTPParity`: malformed/partial selection, explicit URL, duplicate IDs, untouched fields/profile | common health validator corpus C16 covers remaining URL lexical vectors; no private validator |
| test_proxy_group_regex_exclusion.py (9) | `TestPreviewBucketOrderingAndSourceFilters`, `TestProxyGroupHTTPParity`: two source types, subscription prefilter, bucket order/literal exclusion, persisted patches; source-dedupe and raw cache preservation | provider refresh/generation routes belong to verified C17/C30/C31 and run in gate; not reimplemented |
| test_rule_library_create.py (9) | `TestCreateContentAndPublicDelivery`, rejection/loopback/cache tests and `TestRuleLibraryReadCreateHTTPParity`, `TestRuleLibraryEditAndChecksHTTPParity` | repository's existing `CreateRuleLibraryItem` owns cache+DB rollback, existing-ID and orphan/.bak protection and uncertain result; C33 reuses it |
| test_policy_editors.py — draft half only | `TestDraftNativeRevisionContract`, `TestBothSidesRejectConcurrentDraftSubmissions`, `TestDraftRevisionAndConcurrentSubmission`, fault/unknown-commit tests: metadata preserved, stale edits, auth, body shape, version contract | RouterOS authoring/manifest half remains R08/R09/R10; no router writes added |
| test_proxy_group_dns_follow.py (6) | `TestProxyGroupFollowDNSHTTPParity`: template/native, nested/follow source, rename, newly selected shared node, locked-native conflict; exact DB/profile effects | MCP owner remains MCP; shared C27 validator covers inactive/explicit selectors |
| C11/N7-2 listener lifecycle | `TestGroupListenerRelinkingHTTPParity`, `TestListenerRelinkingPreservesUserComments`; C11 real Python relink differential rerun | delegates to subs.RelinkGroupListeners |
| test_scheduled_tasks.py rule-library halves | interval retained in `TestReplaceAndDeletePreserveIndependentState`; task history forgotten on changed source/deletion by actual routes | refresh cache task rows compare Flask in C33 HTTP tests; scheduler C15 owns timings |
| test_rule_cache_identity.py / test_remote_rule_cache_edges.py route halves | failed URL PUT keeps prior declaration/cache; content edit staging/unknown-commit/rename failures and legacy fallback denial | identity validation/retry parser corpus remains C14 |
| C28 previously deferred two callbacks | `mosdns/difftest`: `proxy userinfo inline`, `proxy inline disabled by null` now unconditional comparisons, both pass | no C28 production logic changed |

## Approved comparison classes

- D1: parsed YAML values for native_config and known Mihomo custom_config; check-content's YAML content field only. Raw rule cache bytes stay exact.
- D2: only draft response revision hashes are compared by per-side validity/stability/invalidation, not cross-language bytes; every other field remains exact. A Unicode/HTML fixture proves the native hash differs. Concurrent real Flask/Go requests submit the same text with each side's token and require 200/409.
- Python future completion order: sort only `results` on the two bulk endpoints by ID. All other arrays retain order.
- Native internal English diagnostics: malformed proxy-domain scalar, non-object cache request and caught malformed JSON retain status500 and compare native descriptions by the specifically observed error class. Unchanged state and all other envelope fields stay exact; Chinese diagnostics stay exact. Empty falsey previews, malformed probe/cache statuses and mixed unknown IDs have explicit Flask cases.
- Python cache's erroneous string/container ID membership is fixed by requiring an array (400); a dedicated real Flask regression proves `manual` selected both `a` and `manual`, while Go rejects without any durable effects. This is an explicit validation bug fix, never a broad comparator skip.
- Production comparison has one approved D2 normalize: exact draft route, top-level revision, anchored lowercase64hex. Harness shape/type/missing-field/other-field/other-route negative controls stay live; baseline is unchanged.
- Fixed clocks for existing refresh/notification rows and timestamps only; one-to-one generated-ID correspondence inherited from C32's harness (no C33 generated IDs in ordinary source CRUD).

Fuzz target: `FuzzEditableContent` in api/rulelibrary. Registration is exercised with rules, aggregations and all server routes; no ServeMux ambiguity is tolerated.

## Independent C33 review additions

- `TestReviewAuthenticationAllowlist`: all 23 JWT-only method/path pairs versus absent/config/internal/retired/invalid capabilities; both anonymous aliases remain anonymous. Real Flask/Go response and complete stored state comparisons.
- `TestReviewStrictAndSilentBinding`: 14 write bindings × JSON/text/plain/+json × valid/malformed input. Explicit Content-Type is preserved by both harness sides, rather than overwritten by the fixture.
- `TestReviewReorderMalformedHTTPParity`: both reorder methods × falsey defaults and truthy non-object errors; fixes Go404/Python500. Only those internal English diagnostics are class-compared, with exact status, remaining envelope and storage.
- `TestReviewDraftRevisionShape`: live D2 requires exactly64 lowercasehex, matching production's already narrow route/top-level rule. Production direct/discover comparator controls run all40 tests, including wrong shape/type/missing/nested/unrelated-field/unrelated-route negative controls.
- Cache safety has actual SIGKILL/reopen coverage at four durable boundaries, marker-permission denial, old scoped reader versus actual draft rename, cold population from current DB and late shared inline writers. Those intentional reliability corrections are asserted independently; ordinary C32 cache files remain fully compared.
- 27 actual HEAD probes prove ten-worker bound, join and raw method/body/headers. Native DNS diffs check the tracked lock-order counter. Existing 15-class real Runtime shutdown/disconnection tests remain active; shared final cancellation windows are still O3-6c ownership.
- `TestReviewNonFiniteBindingUsesMalformedBoundary`: all14 binding entry points × NaN/Infinity/-Infinity use each route's real Flask malformed-response control; strict/silent400 and explicitly caught500 stay distinct. `TestReviewPythonNonFiniteAcceptanceIsExplicit` proves Python's unused:NaN200 versus native Go400 with unchanged state, as the registered N8 JSON class.
- `TestReviewPreviewSelectionShapes`: both source types × eight empty/mixed selection forms compare exact Flask JSON/state; four explicit Python200/Go400 controls document rejection of accidental string/object-key iteration. Valid ID arrays and unknown-ID skipping remain unchanged.
