# C30 HTTP boundary coverage

Run `CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/subscriptions/difftest/`.
The fixture uses the real Flask blueprints/profile/auth hooks and Go HTTP pipeline on copies of one
synthetic SQLite directory. Only source downloads and URI conversion use controlled fakes; network
requests are forbidden. Each call compares status, JSON/YAML structure or text, content/profile/provider
headers, the exported SQLite document (including revisions/unknown fields), and every JSON/YAML data file.
Native regex/transport diagnostic identities, generated IDs and wall-clock fields are normalized narrowly.
Notification/receipt clocks are injected equally; their evidence and state remain compared.

| Method/path | Success | Failure/edge | Stored effects compared |
|---|---|---|---|
| GET /api/nodes | empty/populated catalog | anonymous 401 | no changes |
| POST /api/nodes | extensible object | null/list, dialer_ref, auth | shared nodes/revision |
| PUT /api/nodes/<node_id> | replacement, stable chain rename | missing, null, changed ID, duplicate/reserved name, disable dependency | document/revisions |
| DELETE /api/nodes/<node_id> | unreferenced node | missing, profile/chain/raw dependency | document/revisions |
| POST /api/nodes/delete-preview | all affected profiles/aggregations, duplicates, empty sources | invalid/missing IDs, protected chain/raw/rule dependencies | preview read-only; confirmation compared exactly |
| POST /api/nodes/delete | reviewed atomic detach | missing/stale confirmation, changed node, blockers | full shared/profile state; unrelated changes preserved |
| POST /api/nodes/parse-lines | converted/empty/unrecognized lines | missing/non-string/null lines | no writes |
| POST /api/nodes/reorder | ID/legacy object order, definitions preserved | unknown/duplicate IDs | shared nodes/revision |
| GET /api/nodes/latency | C16 health/difftest | C16 auth/worker cases | C16 probe inputs/results |
| POST /api/nodes/latency | C16 health/difftest | C16 validation/busy/unavailable | C16 probe inputs/results |
| GET /api/nodes/latency/settings | C16 health/difftest | C16 auth | C16 settings |
| POST /api/nodes/latency/settings | C16 health/difftest | C16 invalid DNS | C16 settings |
| POST /api/nodes/latency/dns/check | C16 health/difftest | C16 validation/unavailable | C16 probe inputs |
| GET /api/subscriptions | catalog/cache/health projection | anonymous 401 | derived fields never stored |
| POST /api/subscriptions | initialized health; derived fields removed; unknown fields retained | null/list, required fields, interval, regex | shared subscriptions/revision |
| PUT /api/subscriptions/<sub_id> | ID forced, derived fields removed | missing, required text | subscriptions/revision/task receipts |
| DELETE /api/subscriptions/<sub_id> | existing and missing (Python idempotent) | selected source 409 | subscriptions, cache, health, task receipts |
| POST /api/subscriptions/reorder | legacy object supplies IDs only | unknown/duplicate IDs | definitions/revision |
| GET /api/subscriptions/health | existing/empty health | anonymous 401 | read-only |
| GET /api/subscriptions/<resource_id>/health-impact | used/missing resource | auth | read-only |
| GET /api/subscriptions/<sub_id>/nodes | imports; matching/legacy/empty/filtered cache | missing sub; wrong URL/invalid/missing cache | repeated preview does not refresh or mutate |
| POST /api/subscriptions/<sub_id>/fetch | preview/import; transport-cache fallback | missing; failed download without cache | imports/dedupe, raw cache, download cache, health/task/notification receipts |
| GET /api/subscriptions/test | message and timestamp | global JWT gate 401 | no writes |
| GET /api/subscriptions/<sub_id>/proxies | fresh/cache; Mihomo/Surge/Loon; text filters | JWT gate, missing, invalid graph/shape, unsupported chains | cache/served artifacts staged after validation |
| GET /api/subscriptions/proxies | combined final graph, cross-feed references, profile selector | JWT gate; cross-feed collision/cycle/reserved names; bad second feed | earlier source caches unchanged on failed final validation |
| GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies | correct URL token without JWT; JWT; other profile | wrong/missing/internal/retired tokens 401; missing source; malformed filter | auth rejection precedes render/cache; profile isolated |

## Dialer and Python-test mapping

| Python file | Boundary evidence / remaining owner |
|---|---|
| test_dialer_activation_shadow.py | TestProviderDeliveryGraphs covers captured managed/raw/URI activation, overlapping names, reserved/duplicate/cyclic/malformed providers; C17 snapshot tests cover direct force_chains calls. Generate/download/push consumers belong to C34/A14. |
| test_dialer_combined_export.py | TestCombinedCrossFeedAndStaging, TestProviderDeliveryGraphs combined branches, TestProfileSelectionIsolation, TestDeliverySnapshotSurvivesConcurrentProfileChange, TestTransportFallbackAndFormats. C17 tests cover all parser shape variants. |
| test_dialer_delivery_snapshot.py | TestProviderDeliveryGraphs single branches and TestDeliverySnapshotSurvivesConcurrentProfileChange. Agent push-specific payload assertions belong to A14. |
| test_dialer_delivery_staging.py | TestCombinedCrossFeedAndStaging checks exact final graph and preservation of all earlier staged caches; C17 tests cover missing feeds/late activation and no-chain duplicate controls. Agent push-specific delivery belongs to A14. |
| test_dialer_proxy.py | TestNodeMutationsProtectChains, TestReviewedDeletion, TestManagementWrites, alternate-format cases; C12/C17 existing conversion/topology tests cover library semantics. Proxy-group/generate/aggregation routes remain owned by C33/C34/C31, not silently implemented here. |
| test_dialer_standalone_activation.py | TestProviderDeliveryGraphs URI branches, TestTransportFallbackAndFormats and concurrent snapshot case; single URI materialization reuses ConversionResults. Aggregation/main-preview counterparts belong to C31/C34. |
| test_node_deletion.py | TestReviewedDeletion, TestDeletionConfirmationSurvivesOnlyUnrelatedWrites, TestDeletionDoesNotReviseUnaffectedOrderGroups, TestDeletionWarnsOnEmptySources, nodes/crud_test. Repository transaction rollback is covered by repo/TestFailedDatabaseCommitKeepsPreviousData; no storage implementation is copied. |
| test_subscription_cache_preview.py | TestCachePreviewNeverRefreshes covers legacy/matching/other URL/missing/null/empty/filtered caches, repeated read-only calls; TestManagementWrites checks imported/missing/auth contracts. |

## Deferred → A11 (maintainer decision)

`provider.SetLocations` intentionally remains unregistered until A11. `deferA11Locations` marks only
individual matching 400 diagnostic envelopes: owning-provider label, file, source label, line/column and
excerpt. It compares the base validation message (including its prefix and field path), status, success
and all stored effects. Cases emit a `DEFERRED A11` line individually; no whole test is skipped.
A11 must register the hook, remove this helper and rerun this suite (also recorded in PROGRESS-agents).

## C30 独立复核补充

- `TestAdversarialProviderURLs`：23 个请求覆盖大小写、空 token、重复参数首值、编码/双编码、路径穿越、尾斜杠、无关 query 及显式 profile 优先级；Flask 响应和存储差分一致。
- `TestCachePreviewNeverRefreshes/non-list`：非数组旧缓存只读预览和列表不报 500，重复调用无写入。
- `TestMissingAggregationDependencyFailsClosed`：Go 缺省 C31 hook 的失败关闭合同（特意不与已实现 C31 的 Python 比较），single/combined 激活图均 500 且完整状态不变。
- `nodes/review_test.go`：删除 DB 提交失败完整回滚、并发引用写使确认失效、取消请求仍完成；在 race 下运行。
- `subscriptions/security_test.go`：过期 JWT、畸形 Bearer、退休能力与公共 token 碰撞、轮换、统一 401，以及日志/响应头/正文/served 文件无请求凭据。
