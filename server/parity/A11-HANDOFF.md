# A11 — deployment preparation hand-back

Branch `go-port-a11`. Scope: `design/a14-publish.md` A11-1–4 only. HR status is **ported**, never verified until a fresh Opus review. No subagents, pushes or production access. Final acceptance results are recorded in PROGRESS-agents A11.

## Design mapping

| Step | Implementation |
|---|---|
| A11-1 | `deploy/types.go`, `core.go`, `bundle.go`, `adapter.go`; `agents/record.go`; `agents/agentfake`; Agent-side `server_archive_conformance_test.go`. Typed immutable manifest/files, lazy deterministic native PAX/gzip, strict bounded reads, frozen Python v1 fingerprint/digests, comparison main entry and retargeting. |
| A11-2 | `locations.go`, `convert.go`, `materialize.go`, `adapter.go`, `messages.go`; provider hook in `Wire`. Snapshot callbacks (library/rule-proxy/gateway), recursion ≤3, shared refresh last-good source reader, download errors/remedies, 256-entry/16 MiB conversion cache. C30 diagnostic deferral removed. |
| A11-3 | `build.go`, `rawcache.go`, `resources.go`: complete file graph, pins, provider paths/inline behavior, gateway guard, OxiDNS resources/hints, DNS watchdog, eight joined workers, per-build raw URL coalescing + 128-entry/16 MiB LRU, supplemental Geo resources. Production bundle comparison and coverage floors. |
| A11-4 | `storage.go`, `receipts.go`, `previews.go`, `validation.go`, adapter bridges; read-only `oxidns.SchemaFingerprint`. L4 atomic private stores and pruning, preview identities/input key/quotas/expiry/ZIP/final-preview, exact-candidate validation receipts, public task projection. |

Only deploy/adapter.go imports jsonx in runtime code. It retains unknown snapshot fields, bridges existing provider parser marks until N7-3, and uses the existing agents/repo APIs. Dynamic YAML uses the new yamlx API. No dependency added; no Python implementation copied.

Coordinator-approved clarifications:

- convert.Download gains optional Behavior/Format, populated by convert when known and read by deploy's adapter. Existing Object/public download DTO unchanged; downloads generators retain success and zero differences. N8/S-convert ownership debt is in HANDOFF 3b; contract note in a14-publish.md.
- oxidns exposes a read-only SchemaFingerprint, never schemaFS. RulesFingerprint hashes schema summary + native validator build, intentionally invalidating Python-era OxiDNS ready candidates. It is separate from the unchanged-byte content Fingerprint; Chinese re-push refusal and no activation POST are tested.
- C31 is absent. One production Agent's real Python aggregation provider bytes are frozen in mode 0600 private temporary test-server files and supplied as the same A11 Build input. Summary/coverage label `partial: A11 packaging only, aggregation provider frozen (C31 pending)`; this case is excluded from independent generation success. The separate aggregation-provider generator stays Go 尚未实现. C31 has an explicit unfreeze task in PROGRESS-config. No snapshot values/frozen bytes are committed.

## HR issue-class checklist

Each cell states the step's treatment; no device activation, rollback or network policy change is implemented in A11.

| Issue class | A11-1 | A11-2 | A11-3 | A11-4 |
|---|---|---|---|---|
| R04 F1 extra/malformed results, indexing | Manifest/member/count/type checks | Payload shape/index check before lookup | Typed worker results and node kind checks | Corrupt metadata/archive/identity rejected before use |
| R04 F2 rollback messages | No rollback | Shared deployment messages + Python context corpus | Preparation messages/error positions compared | Existing Agent activation gate supplies exact Chinese re-push message |
| R04 F3 cancellation after accepted work | Pure memory | Rule I/O timeout + caller job ctx, refresh owns cache writes | All eight workers joined; no upload | Short durable store writes detach; no repo lock under file lock |
| R04 F4 compensation batching | No device writes | No device writes | No device writes | No device rollback; immutable receipt baselines |
| R04 F5 validation order/messages | Python fingerprint check order | Coordinates from parser nodes, exact context oracle | Artifact ownership/refusal order | Binding/archive/input checks precede use |
| R04 F6 partial outcomes reported incorrectly | No batch write | No batch write | First error retained; all workers joined | Capture then guarded CAS; failed ownership returns false |
| R04 F7 composite comparison panic | Typed manifests + DeepEqual | Nodes/strings, no interface comparability | Typed file metadata, bytes and structural YAML | Typed metadata, exact receipt object equality and canonical identities |
| R04 F8 resource/script bound | File/count/aggregate/manifest/tar bounds | Result cache bounded | Raw cache/worker/file limits; per-destination accounting | Source metadata counts in preview quota; bounded reads/archives |
| R04 F9 absent expected proof | Required file size/path/digest | Missing local callback/source fails | Every referenced file/provider must be bundled | Nonempty Agent/task/profile/archive/fingerprint, exact current proof |
| R04 F10 duplicate helpers | Standard archives/JSON | Shared diag/mosdns/rulefetch/refresh | Shared gateway/oxidns/watchdog helpers | Existing Agent HA projection/localization; one atomic store helper |
| R05 C1 implicit shared-client behavior | No shared client change | Existing rulefetch transport reused | Existing transport and callbacks retained | Existing Manager.DeploymentRequest for capabilities/final-preview |
| R05 C2 write-path error classification | Typed preparation failures | Causes/remedies retained | Typed preparation/download failures | Corrupt read fails closed; write errors wrapped, no partial publish |
| R05 C3 missing identity matches missing | Required metadata | Snapshot path matching | Safe canonical file keys | Empty identity refused; extra receipt fields refused |
| R05 C4 reused ID during undo | Same-ID/different-digest fake refusal | No undo | Pins are caller's trusted receipt bytes | Applied pointer compared in transaction; target identity change requires regeneration |
| R05 C5 repeated table scan/JSON loops | Indexed member lookup | Bounded conversion lookup | File/cache maps + one final sort | Indexed retained IDs; file pruning under L4, repo read once outside |
| R05 C6 refused vs uncertain and cancellation | Receiver journals validated bytes only | HTTP failure has no network remedy; connectivity failures do | Preparation failure stops before Agent upload | Durable files first, current pointer acknowledgement separately; no fabricated activation proof |
| A19 F1 alias amplification/cycles | Typed acyclic manifests | yamlx bounded parsing | Hostile providers stay Nodes; A19 traversal sees each container once; depth-64/cycle tests | Manifest/archive checks, typed metadata; no expansion of third-party YAML |
| A19 F2 wrappers/wrong shape silently ignored | Structural manifest | Explicit payload shape | Generated root must be a mapping | Native OxiDNS document validator on actual archive main bytes |
| A19 F3 whitespace → empty capture | No conversion | Unicode whitespace payload detection | Exact empty proxy-part bytes; invalid source fails | Frozen bytes never silently re-rendered |

## Python test coverage mapping

| Python file / deferred behavior | Go boundary evidence or later owner |
|---|---|
| test_agent_content_manifest.py | core_test.go + gen_a11_core.py: real Python identity/digests/path outcomes; aggregate/file/count bounds, canonical collisions, roles/digests/nulls, names/retarget invariance. |
| test_deployment_bundle_lazy_archive.py | TestBundleImmutableLazyConcurrent; pack once/drop raw bytes, copies, concurrent files/archive, Python PAX readback. |
| test_agent_content_baseline.py | Content encoding invariants in A11 core; manager baseline promotion/late status/restart/binding behavior remains existing A01 and A14 publication acceptance. A11 does not claim to implement those manager writes. |
| test_agent_content_preparation.py | TestPythonBuild and TestUnusedSourceChangesDoNotChangeBundle: complete selected graph, ignored management/unused sources, custom/provider/rule byte changes. Full generation wrappers, StoreDownloads and named route progress are A14. |
| test_agent_deployment_downloads.py | TestPythonBuild DNS split/empty/error cases, TestBuildSharedSourcesAndProgress (one URL per build, two builds, eight-worker bound), TestRawCacheBoundsAndCoalescing, TestRawCacheFailureDoesNotPoisonRetry (entry/byte LRU, oversized, empty bodies, failed retry), TestLoopbackRuleDownload; route/history responses A14. |
| test_classical_deployment.py | gen_a11_build.py + TestPythonBuild: text/YAML classical marking/normalization, DNS split, native attachments, exact verbatim bytes; rules_test.go conversion corpus includes classical regex commas and IPv6. Shared normalizer's own corpus retains wildcard/ambiguous-line semantics. |
| test_deployment_error_locations.py | gen_a11_rules.py + TestPythonRulesAndLocations: 64 conversion cases, 23 sensitive/indented excerpts, 9 contexts, 7 callbacks. Full C30 Flask differential now compares file/line/column/excerpt without deferA11Locations. Publishing responses/history A14. |
| test_mihomo_resources.py | TestPythonBuild Geo selections + TestGeoPreparation; alias reuse, old Agent, required resource, progress without source URL, empty/HTML refusal, resources excluded from content identity. |
| test_agent_publish_caches.py | Build per-publication sharing + existing rulefetch.TestUnchangedRefreshOnlyTouches, refresh.TestDeviceReadsUseTheCacheAndOnlyAMissingCacheDownloadsNow / TestAutoDeliveryReadsCacheWithoutUpstreamOrExtendingAge / TestInvalidRefreshKeepsTheValidCache. Python loader-choice tests moot under N7; preparation generating main once is A14. |
| test_generated_preview.py | TestPythonPrivateStoresLoad, TestPreviewsFrozenIdentityExpiryAndQuota, TestLegacyPreviewFreezesSurgeText, TestFinalizeBundleUsesFrozenDeviceBytes: Python disk load/public view, exact ZIP/file bytes, expiry/tamper/quota (including source metadata), frozen Surge, binding/input change, target-side adaptation and comparison baseline. HTTP download/push/generation_id/HA orchestration remains A14. |
| test_oxidns_deployment_receipt.py | TestRulesProofAndOldCandidateRefusal: actual A01 activation gate, current proof, changed schema/build proof, no POST, Chinese diagnostic; TestReceiptsCaptureAdvanceAndDefaultProfile: resolved non-default-named profile and extra-proof rejection. Upload/replay/late status route scenarios remain A14. |
| test_oxidns_rule_source_edit.py, test_service_domain_publish.py, test_generated_config_error_locations.py | A11 graph/source error primitives covered; full preparation/publication scenarios explicitly A14 per design 6.6. |
| test_agent_deployments.py A01-deferred bundle cases | Python Build/Geo/rule corpora, real Agent archive test, retarget and failure tests. Upload/route reservation/manager transitions remain A14. |
| test_domain_wildcard_*.py, test_mosdns_rule_fields.py, test_mihomo_classical_delivery.py | Shared MosDNS conversion and Python domain/IP/wildcard/classical Build corpus; full generated profile behavior remains converter corpora and A14 preparation. |
| test_ruleset_domain_format.py, test_remote_rule_consumers.py | Real rulefetch/refresh validation invoked by Build; Python corpus validates final consumer and exact rule bytes. Existing rulefetch/refresh boundary suites cover last-good cache behavior. |
| test_udp_fallback.py, test_oxidns_* bundle/resource cases | Public native OxiDNS resource validator/relative-resource helpers reused by Build; native-vs-converted and missing-resource corpus. Full generated configs remain existing oxidns corpus; publication/rule-sync projections A14/A16. |
| test_dns_watchdog_config.py, test_dns_watchdog_core.py | TestPythonBuild watchdog on/off for MosDNS/OxiDNS through shared EnsureLocalDNSWatchdog; existing oxidns watchdog tests. Receipt/publication ownership A14. |
| test_proxy_server_dns.py, test_linux_gateway.py bundle cases | Shared gateway config/provider loader and routing-mark guard; hostile provider tests + production bundle comparison. Core-version-specific generation and forwarding/version guards A14/A20; real capture L4. |

Corpora contain synthetic domains/addresses only. Production-data outputs remain private on the test server.

## Only L2 can prove

After fresh Opus review and A14, real guests must prove actual core load/reload/restart/DNS health, stage-only→ready→activate on current and oldest protocol-1 Agents (including <1.8.30-go/<1.8.57-go); forced activation failure and previous-service restoration; rollback failure→recover; manual rollback/newer-publication refusal; link loss after upload; AIO final-preview DNS-port adaptation and publish without rerender; missing/reused Geo databases and file permissions; process death mid-preparation/upload; Docker paths/ownership. The real Agent extractor test proves current code reads the archives, not that a running core loads them. Surge has no L2 guest (macOS/iOS only): fake coverage and maintainer device at O14.

## Commands

All heavy commands run on test servers. `.63` was used for iteration; `.32` is authoritative. No unfinished command is handed back. Exact final commands and output tails are in PROGRESS-agents A11.
