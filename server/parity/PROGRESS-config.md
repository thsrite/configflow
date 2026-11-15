# PROGRESS — domain `config`

Domain `config` (WORKFLOW.md): foundation (old packages #0–#5) and configuration/generation (#6–#19). **119 Python files, 39628 lines, 37 modules** — 26 missing, 5 partial, 5 ported, 1 verified (C01). Only reviewers set `verified`.

Planning audit: 2026-11-15, worktree `configflow-go`, branch `go` at 9783acd1 (plus uncommitted work of the subs and agents agents). File list and other domains: `server/parity/FILES.md`.

## Conventions

- Status: `missing` (no Go code), `partial` (some code; the section lists exactly what is missing), `ported` (code and tests exist, nothing obviously missing; **not reviewed**), `verified` (set by a reviewer only, with commit ids and checks).
- A test file is listed under the module that owns its subject. Integration tests (`create_app()` + routes) often also need later modules: the porter ports the cases that can run now and lists the **deferred** cases in the section of the module that makes them runnable (as done below for the foundation). `ported` allows deferred cases if they are listed.
- Python test counts are `def test*` per file; "Go n/m" counts cases found in Go tests by name or by the `test_x.py::test_name` comments. Corpus tests (Python-generated rows under `testdata/`) cover behaviour without matching names.
- Routes: rows of `server/parity/routes.tsv`. `/api/profiles/<profile_id>/…` wrappers in `routes/profiles.py` only set the profile and call another blueprint's function; they are listed under the module that owns that function, whose `api` package registers them (PORTING rule 8).
- Python lazily imports in cycles (e.g. converters ↔ provider delivery ↔ route files, the whole OxiDNS cluster). Go needs a DAG: the prerequisite order below breaks the cycles; when a module needs a function of a later module, put a `compat.go` copy (`TODO(port)`) or an interface/hook and list it here.
- Cross-domain calls (config → agents/routers code) are listed per module; they are the main blockers for C18, C27, C34, C36, C37.

## Known accepted differences (do not rediscover)

- jsonx: lone UTF-16 surrogates cannot be represented in Go strings (documented in `jsonx/doc.go`); `test_json_response.py::test_surrogate_strings_keep_normalization_before_redaction` depends on it.
- Regex: `\N{name}` escapes and repeat counts > int32 → `*rules.UnsupportedRegexError` (filters with such patterns are not applied; validation accepts them like Python). Backtracking matches stop after 1 s and count as no match.
- yamlx: syntax error texts are yaml.v3's; non-string mapping keys → `*NonStringKeyError`; CSafeDumper output (size measurement in `push_diff.py`) not reproduced.
- Commented Mihomo YAML editor (`subs/commented`): refuses (400 with Python's message, original text kept) instead of writing different bytes; open categories are listed in C11 and are still being worked on.
- Web listener: the Go server listens itself and serves the SPA (replaces nginx); nginx-specific behaviour has no counterpart (C07).
- Not ported by decision (packages.md): legacy pre-SQLite migration, `models/`, `startup.py`, `http_server.py`, `http_queue.py`, `agents/node_test_dns_cleanup.py`, `metrics_history.LEGACY_DIRS`; `subscription_parser.parse_subscription`/`parse_clash_yaml` (no caller). Tests only about those: `test_split_layout_migration.py`, 25 cases of `test_config_storage_migration.py`, `test_http_server.py`, `test_startup.py`, `test_http_queue.py`, `test_node_test_dns_cleanup.py`.

## Module table

| Id | Module | Py lines | Status | Prereqs | Routes |
|---|---|---|---|---|---|
| C01 | jsonx, store | 564 | verified | – | – |
| C02 | infra helpers | 828 | ported | C01 | – |
| C03 | repo storage core (config_repository.py part A) | 1576 | ported | C01, C02 | – |
| C04 | repo public API (config_repository.py part B), config.py | 1373 | ported | C03 | – |
| C05 | profile context, OxiDNS settings normalizer | 497 | partial | C04 | – |
| C06 | HTTP shell, auth, settings routes | 1028 | partial | C05 | 22 (17 done) |
| C07 | logs, stats, tasks, notifications, web listener | 1393 | partial | C06 | 9 (5 done) |
| C08 | rule syntax, matcher, rule utils | 1101 | partial | C02 | – |
| C09 | matcher cache, group candidates, reorder, strategy refs, connection rules | 708 | ported | C08 | – |
| C10 | manual node parser, subscription hosts/filter/dialers | 1242 | ported | C08 | – |
| C11 | proxy chains, commented Mihomo editor, listeners, proxy-server DNS | 855 | partial | C10 | – |
| C12 | converter common, Mihomo node conversion, dialer references, EasyTier | 1196 | ported | C09, C11 | – |
| C13 | Sub-Store client, runtime, subscription cache, dependencies | 1193 | missing | C12, C10 | – |
| C14 | remote rule fetch, ruleset content, remote sources, website identity | 1202 | missing | C12, C08 | – |
| C15 | subscription refresh, health, scheduled refresh | 600 | missing | C13, C14, C07 | – |
| C16 | health check, latency, node DNS, region check, domain probe | 1089 | missing | C12, C13 | – |
| C17 | provider delivery and responses | 540 | missing | C15 | – |
| C18 | Mihomo config generation, Mihomo base settings | 1275 | missing | C12, C15, C16, C17 | – |
| C19 | Surge converter | 1129 | missing | C18 | – |
| C20 | Loon converter | 764 | missing | C19 | – |
| C21 | MosDNS converter | 1261 | missing | C14 | – |
| C22 | OxiDNS converter, native loader, references, match targets | 913 | missing | C05, C21 | – |
| C23 | OxiDNS validation and constraints | 1542 | missing | C22 | – |
| C24 | OxiDNS common settings | 1175 | missing | C23 | – |
| C25 | OxiDNS common routing, DNS watchdog | 1286 | missing | C24 | – |
| C26 | OxiDNS common updates, device policy, editor | 1189 | missing | C25 | – |
| C27 | rule DNS actions | 885 | missing | C26 | – |
| C28 | MosDNS archive and routes | 838 | missing | C21, C15 | 15 (0 done) |
| C29 | OxiDNS native apply, archive, routes | 739 | missing | C27, C28 | 18 (0 done) |
| C30 | subscription and node routes, node deletion | 994 | missing | C17, C16, C19, C20 | 26 (0 done) |
| C31 | aggregation routes, device notes | 722 | missing | C30, C26 | 11 (0 done) |
| C32 | rule routes | 1107 | missing | C09, C14, C27 | 21 (0 done) |
| C33 | rule library and proxy group routes | 976 | missing | C32 | 25 (0 done) |
| C34 | generate, config and profile routes, config export | 1443 | missing | C18, C19, C20, C28, C29, C31, C33 | 59 (0 done) |
| C35 | config importer, WebDAV backup | 1375 | missing | C04, C06 | – |
| C36 | setup flow | 1372 | missing | C34, C35 | 5 (0 done) |
| C37 | domain discovery | 1658 | missing | C14, C16 | 17 (0 done) |

Routes in config modules: 228 of the 380 rows in routes.tsv.

## C01 — jsonx, store

**Status: `verified`** (review 2026-11-15, one round; 0 open findings) · prereqs: – · Go: internal/jsonx, internal/store

Files:
- `backend/common/config_store.py` (564)

Tests: `test_config_store.py` (11)

Routes: none.

Evidence: `internal/store` (store.go, conn.go, rows.go, errors.go, atime_*.go) + `internal/jsonx`. All 40 defs of `config_store.py` have counterparts (the exception classes are `store.Err*` kinds, `_fsync_file` is inlined). `test_config_store.py` 11/11 cases ported (`store_test.go`) plus a both-way Python cross-check (`crosscheck_test.go`) and the 634-value jsonx encoder corpus.

Review findings (all fixed; regression tests in `store/review_test.go`, `store/backup_linux_test.go`, `store/crosscheck_test.go`):
1. **medium** — `BackupTo` failures carried the driver's *source*-connection errmsg (`配置数据库访问失败：not an error` for a disk write failure); CPython raises with the destination's errmsg, i.e. `sqlite3_errstr(rc)` (`…：disk I/O error`). Fix 26140751; `TestBackupWriteFailureMessage` (RLIMIT_FSIZE, reference from CPython).
2. **low** — a TEXT row that is not UTF-8: Python's sqlite3 raises `OperationalError: Could not decode to UTF-8 column '<col>' with text '<one U+FFFD per bad byte>'` (reads → `ConfigStorageError 配置数据库访问失败：…`, `check()` → raw, salvage → None); Go returned StoreCorrupt for values and silently read bad owners/keys. Fix 039f961d (`checkText` in every row scan, `decodeError`); `TestInvalidUTF8TextIsAStorageError` + cross-check scenarios `value-bad-utf8`, `meta-bad-utf8`, `owner-bad-utf8`.
3. **low (latent)** — `translate`/`untranslated` found a SQLite error through `errors.As` inside an already classified `*store.Error` and reclassified it (Python's `except sqlite3.Error` never sees a StoreError). Fix 039f961d; `TestTranslateKeepsClassifiedErrors`.
4. **low** — `build_temporary` quick_check failure text is `f'{result}'` (`[('…',), …]`), Go joined rows with `; `. Fix 66da000f; `TestQuickCheckFailureMessage`.
5. **low** — `preserve_corrupt` no-hard-link fallback: `shutil.copy2` stats the source *after* copying and copies atime+mtime, then the mode; Go set atime to now. Fix 66da000f; `TestCopyFileKeepsTimes`.

Checks performed:
- Line-by-line walk of every function/branch of `config_store.py` vs Go: SQL text of all statements and the schema, pragmas (`foreign_keys`, `synchronous=EXTRA`, `journal_mode=DELETE`, `application_id`, busy timeout = `sqlite3.connect(timeout)`), `BEGIN`/`BEGIN IMMEDIATE`, rollback paths (`in_transaction`), head/meta validation, `document_rows`/`build_document` consistency rules and messages, `_diff` (incl. cascade, root-order compare), commit cache reuse, `_apply` meta order, temp naming/mode 0600/cleanup, install/fsync, check, salvage, corrupt target, backup (busy retry, O_EXCL 0600), stale temporaries glob semantics; modernc driver source (error formatting, URI open flags, backup API).
- jsonx vs CPython 3.11 `_json.c` (scanstring, scan_once, match_number, object/array parsing, error positions) and `encoder.py`/C encoder (escaping, float repr rule, separators, sort order), Flask 3.0 `DefaultJSONProvider.response`.
- New Python reference data: `jsonx/testdata/review_gen.py` → `review.jsonl` (73 cases: exact float ties beyond 800 digits, extreme exponents, astral/BMP sort order, U+2028/2029 and format characters, full-width and other non-ASCII digits, nested duplicate keys, 600-deep and wide documents, error positions after CRLF/astral/deep nesting, 4301-digit ints) — passed without code changes (`TestReviewCorpus`).
- Store cross-check extended: 7 new tampered-row scenarios (BLOB/integer/bad-UTF-8 values, bad-UTF-8 meta/owner, negative generation, failing trigger) and a commit outcome per scenario; odd document sequences (empty documents/sections/keys, profiles without fields, reordering) compared row by row with Python (`TestCrossCheckPythonOddDocuments`); Go↔Python concurrent writers (busy wait → conflict both ways, short timeout → `database is locked`) and 8 concurrent Go writers (`-race`, 3 runs); permission/directory/read-only failures; paths with URI special characters.
- Dependent packages re-run after the fixes: `internal/repo`, `internal/api/settings`, `internal/agents` pass.

Accepted differences (reviewer's judgement, documented; not open findings — overrule if needed):
- Nesting: CPython `json.loads`/`dumps` hit RecursionError at depth 993 (less inside Flask); Go parses up to `jsonx.MaxDepth` 10000 and encodes any depth (`jsonx/doc.go`).
- jsonx lone surrogates (domain-level known difference).
- Rows only manual SQL can produce: a `generation` beyond int64 (Python reads it but cannot commit: OverflowError; Go reports StoreCorrupt), non-integer `position` values (Python loads; Go fails with a scan error), a BLOB meta/field value in `Fingerprint` (Python TypeError; Go digests it — test helper only).
- `DiscardTemporary`/`DiscardStaleTemporaries` ignore unlink errors other than "missing" (Python would raise e.g. PermissionError from cleanup); `copyFile` does not copy xattrs/file flags (copy2 does, best effort).
- Integers over 4300 digits are not refused by the encoders (unreachable: `Parse` and yamlx refuse them on input, as CPython does).
- `default=str` / `allow_nan=False` are not provided by the encoders (callers convert first). The store-level C01 cases of `test_config_storage_migration.py` are in C03 (`repo/open_test.go`).

Commands: `rt -- bash -lc 'cd server && CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(cd .. && pwd) go test -count=1 -v ./internal/store/ ./internal/jsonx/'`; regenerate references: `rt --back server/internal/jsonx/testdata/review.jsonl -- bash -lc '~/cf-test/.venv/bin/python server/internal/jsonx/testdata/review_gen.py'`.

## C02 — infra helpers

**Status: `ported`** · prereqs: C01 · Go: internal/logx, jsonstore, yamlx, localhttp, urlx, taskstatus, resource, utilx, jsonresp, version; httpx/internal.go

Files:
- `backend/__init__.py` (1)
- `backend/common/__init__.py` (1)
- `backend/converters/__init__.py` (1)
- `backend/utils/__init__.py` (1)
- `backend/utils/logger.py` (58)
- `backend/utils/json_store.py` (77)
- `backend/utils/fast_yaml.py` (77)
- `backend/utils/local_http.py` (140)
- `backend/utils/url_utils.py` (123)
- `backend/utils/task_status.py` (126)
- `backend/common/resource.py` (63)
- `backend/common/utils.py` (33)
- `backend/common/json_response.py` (94)
- `backend/version.py` (11)
- `backend/common/internal_call.py` (22)

Tests: `test_fast_yaml.py` (15), `test_json_response.py` (10), `test_local_http.py` (10), `test_operation_logs.py` (12), `test_safe_url_queries.py` (11)

Routes: none.

Evidence: Go: `logx`, `jsonstore`, `yamlx`, `localhttp`, `urlx`, `taskstatus`, `resource`, `utilx`, `version`, `jsonresp`, `httpx/internal.go` (internal_call). Every def matched by name or documented rename (`get_logger`→`logx.Get`, `JsonFileStore`→`jsonstore`, `get_resource_path`/`get_backend_resource`→`resource.FromEnv().Backend`, `get_version_info`→`version.Info`, `jsonify_external`→`jsonresp.External`/`httpx.External`). Tests: `test_json_response.py` 7/10, `test_local_http.py` 1/10 by name but the file header in `localhttp_test.go` says the local_http behaviour is ported against a plain handler; `test_fast_yaml.py` is about the libyaml verification cache that has no Go counterpart, its observable behaviour is in `yamlx_test.go` + the 1374-value corpus.

Missing / deferred: Deferred test cases (run them when the named module exists): `test_local_http.py` cases through rule_fetch (redirect policy, server_domain host compare, `test_real_rule_url_check_works_with_one_http_worker`) → C14; `test_safe_url_queries.py` 7 cases through generation/installers (`append_url_query`, Mihomo/Surge/MosDNS URL round-trips → C18/C19/C28; `test_generated_go_installer_does_not_print_binary_download_url` → agents domain); `test_operation_logs.py` is mostly agents/routers (assigned here only for the logger part).

Reviewer notes: Open item from the subs follow-up (coordinator, 2026-11-15): **yamlx cannot parse `1,000` inside a flow collection, nor ruamel's `!!int '0x-1F'` (negative hex/binary)**; `internal/subs` works around the latter only in its reload check. This module cannot become `verified` until fixed or accepted by the maintainer. Known limits already documented in PORTING.md: non-string mapping keys → `*NonStringKeyError`; YAML syntax error texts are yaml.v3's, not PyYAML's; CSafeDumper output not reproduced. `test_json_response.py` missing: surrogate normalisation before redaction (jsonx surrogate limit), and two Flask JSON-provider hook tests (Python mechanics). Reviewer: `utils/local_http.py::_check_read_timeout` / slow-response timeout case has no named Go test — check `localhttp_test.go`.

## C03 — repo storage core (config_repository.py part A)

**Status: `ported`** · prereqs: C01, C02 · Go: internal/repo

Files:
- `backend/common/config_repository.py` (1576) — lines 1–1058 and 2087–2604: exceptions, `_now`, `_content_digest`, `_deep_merge`, `_three_way_merge`, `_default_profile_config`, `_items_by_id`, `_id_list`, `_prune_rule_dns`, `_collect_resource_refs`, `_AgentHistoryNotRead`; `ProfileRepository.__init__` … `_write_locked` (open, legacy migration [not ported], corrupt rebuild, cache, router commit guard, document/transaction core); `_scope_legacy_dialers`, `_convert_import`, `_resource_usage`, `_validate_document`, `_validate_profile`

Tests: `test_config_repository.py` (15), `test_config_repository_cache.py` (12), `test_config_storage_migration.py` (41), `test_multi_profile_repository.py` (35), `test_router_repository_safety.py` (17), `test_split_layout_migration.py` (32)

Routes: none.

Evidence: `internal/repo` repository.go, lock.go, merge.go, defaults.go, validate.go, importexport.go, errors.go, files.go (8.3k Go lines, 4.2k test lines). `test_config_repository_cache.py` 12/12, `test_multi_profile_repository.py` 35/35, `test_config_repository.py` 14/15 (missing one is legacy single-file migration), Python↔Go cross-check of 80 scripted operations (`crosscheck_test.go`). `test_router_repository_safety.py` 11/17.

Missing / deferred: Not ported by decision: legacy migration functions (see FILES.md note) and 25 of 41 `test_config_storage_migration.py` cases + all 32 `test_split_layout_migration.py` cases (legacy import; `_convert_import`/`_scope_legacy_dialers` themselves are ported and cross-checked). Deferred: 6 `test_router_repository_safety.py` cases (`test_rule_sync_persists_rollback…`, `test_first_publication_blocks_batch_replacement…`, `test_batch_http_mutations_cannot_abandon_published_policy`, `test_batch_http_mutation_during_first_readback…`, `test_router_settings_allow_credential_repair…`, `test_rotated_router_password_can_be_repaired…`) → routers domain (they need router routes/jobs).

Reviewer notes: **Maintainer question:** `_rebuild_corrupt_locked` (rebuild a damaged `configflow.sqlite3` from a matching `config.json`) is not ported; Go refuses to start with the restore hint (`repository.go::openLocked`). This is pre-SQLite-file based, but it is not literally on the maintainer's not-ported list — confirm. Spot-check notes: `DeleteProfile` uses `lexists(directory)` where Python uses `Path.exists()` (follows symlinks: a dangling symlink profile dir is renamed in Go, a fresh tombstone dir is made in Python); agent `name` that is not a string is joined with `jsonx.Str` in Go where Python's `'、'.join` raises TypeError (500). `validateProfile` mosdns/oxidns branch: Python `profile.get(engine, {})` of a non-dict raises AttributeError; Go treats it as empty — check whether an earlier check already rejects non-dict `mosdns`. Python lock uses `fcntl.flock` + thread lock; Go `withLock` — check timeout message `_lock_timeout_seconds` text.

## C04 — repo public API (config_repository.py part B), config.py

**Status: `ported`** · prereqs: C03 · Go: internal/repo

Files:
- `backend/common/config_repository.py` (1028) — lines 1059–2086: `_profile` … `write_profile_text` (profiles, bindings, publish snapshot, create/clone/import-client/update/delete, transactions, router settings/publication, shared, rule library, node deletion, system/agents, compat config, export/import/reset, shared/profile paths, sanitization tokens, locks, atomic writes)
- `backend/common/config.py` (345)

Tests: `test_editor_transactions.py` (9), `test_profile_agent_bindings.py` (8), `test_profile_clone_router_isolation.py` (2), `test_qa_integrity.py` (12), `test_request_config_snapshots.py` (16), `test_save_publish_guard.py` (18)

Routes: none.

Evidence: `internal/repo` profiles.go, shared.go, system.go, scope.go, hooks.go, files.go; `config.py` module functions in scope.go/repository.go (24/26 defs matched; `_cache`/`_LazyBaseline` are scope internals). `test_request_config_snapshots.py` 16/16.

Missing / deferred: Deferred (route-level, need `/api/profiles` etc.): `test_profile_agent_bindings.py` 5/8 missing (rebind/delete-error cases → C34 route + agents registry), `test_profile_clone_router_isolation.py` 0/2 (→ C34 + routers), `test_save_publish_guard.py`, `test_editor_transactions.py`, `test_qa_integrity.py` (whole-app regressions; run when C30–C34 exist).

Reviewer notes: Hooks still default/no-op until their owners land: `ConfigurationCommitted` (WebDAV, C35), `PlanNodeDeletion` (C30), `SanitizeExternalPayload`/`RedactOxidnsSettings`/`RedactRouters`/`RedactEasytierSettings` (C34, routers), `SetStructuredProxyStringParser` (C12), `JobLock` (routers), `InitAgentManager` (agents). `repo/compat_*.go` copies to switch: `compat_rules.go` (rules is ported and does not import repo — switch now), `compat_dialers.go` (C12), `compat_proxystring.go` (C12), `compat_health.go` (C16), `compat_oxidns.go` (C26/C27/C31).

## C05 — profile context, OxiDNS settings normalizer

**Status: `partial`** · prereqs: C04 · Go: internal/repo (scope.go, defaults.go), internal/httpx (app.go profileContext); normalizer: internal/oxidns (new)

Files:
- `backend/common/profile_context.py` (208)
- `backend/common/oxidns_settings.py` (289)

Tests: none of its own (covered through the tests of the modules that use it).

Routes: none.

Evidence: `profile_context.py`: `profile_api_path`, `config_api_path`, `_is_global_request`, `resolve_profile_id` → `repo/scope.go`; `install_profile_context` → `httpx/app.go::profileContext` + `logConfigPull`. `oxidns_settings.py`: only `get_default_oxidns_settings` → `repo/defaults.go::GetDefaultOxidnsSettings`.

Missing / deferred: Missing: `profile_context.append_url_query` (used by converters and routes: port with C12/C18); `oxidns_settings.OxidnsSettingsError`, `_merge`, `safe_relative_path`, `_normalize_rule_dns`, `normalize_oxidns_settings` (174 lines; lazily imports `rule_dns.normalize_action`, `oxidns_device_policy.normalize_policies`, `oxidns_native.managed_files/native_document/load_native_yaml`). Private copies of `normalize_action`/`normalize_client_range`/notes in `repo/compat_oxidns.go`. No dedicated Python test file; covered by OxiDNS tests (`test_oxidns_*`, `converters/test_oxidns.py`).

Reviewer notes: Port the normalizer together with C22 (it needs `oxidns_native`) and finish it when C26/C27 land; until then C05 stays `partial`. Python callers of the missing functions span 15 files (converters, routes/config, routes/oxidns, routes/rules, agents deployment_bundle, linux_gateway, gateway_onboarding).

## C06 — HTTP shell, auth, settings routes

**Status: `partial`** · prereqs: C05 · Go: internal/httpx, httpxtest, auth, server, lifecycle, cmd/configflow, api/auth, api/settings, redact

Files:
- `backend/app.py` (206)
- `backend/routes/__init__.py` (113)
- `backend/common/auth.py` (171)
- `backend/routes/auth.py` (83)
- `backend/routes/settings.py` (401)
- `backend/conftest.py` (54)

Tests: `test_bearer_auth.py` (8), `test_login_messages.py` (3), `test_request_exception_redaction.py` (9), `test_rule_fetch_proxy_settings.py` (7), `test_security_regressions.py` (31)

Routes (22, 17 done):

`POST /api/auth/login` [done]; `GET /api/auth/status` [done]; `GET /api/auth/verify` [done]; `GET /api/backup/config` [done]; `POST /api/backup/config` [done]; `POST /api/backup/now`; `POST /api/backup/test`; `DELETE /api/config-token` [done]; `GET /api/config-token` [done]; `POST /api/config-token` [done]; `GET /api/server-domain` [done]; `POST /api/server-domain` [done]; `GET /api/settings/dependencies`; `POST /api/settings/dependencies/sub-store/mode`; `POST /api/settings/dependencies/sub-store/update`; `GET /api/settings/rule-fetch-proxy` [done]; `POST /api/settings/rule-fetch-proxy` [done]; `GET /api/settings/subscription-aggregation` [done]; `POST /api/settings/subscription-aggregation` [done]; `GET /api/settings/web-port` [done]; `POST /api/settings/web-port` [done]; `GET /api/version` [done]

Evidence: Go: `httpx` (router, ctx, responses, sanitize, edge/SPA, CORS), `httpxtest`, `auth` (auth.go, jwt.go), `server`, `lifecycle`, `cmd/configflow` (`validate_required_env_vars` → `printNoAuthBanner`), `api/auth` (3/3 routes), `api/settings` (14/19 routes), `redact`. Verified earlier against Flask with 82 requests (`httpx/difftest`). Tests: `test_login_messages.py` 3/3, `test_bearer_auth.py` 4/8 (missing 4 are MCP → finish), `test_rule_fetch_proxy_settings.py` 5/7, `test_security_regressions.py` 5/31, `test_request_exception_redaction.py` 0/9.

Missing / deferred: Missing routes (in `routes/settings.py`): `POST /api/backup/now`, `POST /api/backup/test` (WebDAV → port with C35); `GET /api/settings/dependencies`, `POST /api/settings/dependencies/sub-store/mode`, `POST /api/settings/dependencies/sub-store/update` (→ port with C13). Deferred tests: `test_security_regressions.py` 26 cases are config-pull/rule-proxy/provider endpoints (→ C28 rule proxy, C30/C31 providers, C34 `/api/config/*`); `test_request_exception_redaction.py` 9 cases go through Sub-Store/rules/subscriptions/aggregations/mosdns routes (→ C13, C30, C31, C32, C28); `test_rule_fetch_proxy_settings.py` 2 cases (save failure echo, full backup vs shared export → C34). `app.py::start_agent_rule_sync` and the agents/routers loops are wired by those domains. `internal/redact` (421 lines) has **no tests** of its own.

Reviewer notes: Reviewer: add direct tests for `internal/redact` (variants/URL-encoded forms, `RedactSecretsInText`, `StoredSecretFinder`) against Python `config_export` helpers. Public endpoint list lives in `internal/auth` (`gate`): check it against `routes/auth.py::setup_before_request` once all route packages exist. `api/settings/compat.go` holds `rule_fetch.validate_rule_url` (switch with C14).

## C07 — logs, stats, tasks, notifications, web listener

**Status: `partial`** · prereqs: C06 · Go: internal/api/logs, api/stats, api/tasks, api/notifications, tasks, notifications, weblistener

Files:
- `backend/routes/logs.py` (179)
- `backend/routes/stats.py` (109)
- `backend/routes/tasks.py` (365)
- `backend/routes/notifications.py` (33)
- `backend/notifications.py` (296)
- `backend/utils/notification_store.py` (103)
- `backend/utils/web_listener.py` (308)

Tests: `test_logs_tail.py` (7), `test_notifications.py` (16), `test_web_listener.py` (21)

Routes (9, 5 done):

`POST /api/logs/clear` [done]; `GET /api/logs/info` [done]; `GET /api/logs/tail` [done]; `GET /api/notifications` [wip]; `POST /api/notifications/read` [done]; `GET /api/stats/overview` [done]; `GET /api/tasks` [wip]; `PUT /api/tasks/interval` [wip]; `POST /api/tasks/run` [wip]

Evidence: Go: `api/logs` (3/3 routes, `test_logs_tail.py` 7/7), `api/stats` (1/1), `notifications` + `api/notifications` (`test_notifications.py` 15/16), `tasks` + `api/tasks`, `weblistener` (redesign: Go serves the SPA itself instead of rewriting nginx; `test_web_listener.py` 7/21).

Missing / deferred: Missing: `GET /api/notifications` and the three `/api/tasks` routes are `wip` — they register only when every `tasks` slot is provided: subscription + rule_source (C15), domain_discovery (C37), router_rule_sync (routers), agent + metrics_history (agents); backup row is here. The row builders `_subscription_rows`, `_rule_rows`, `_domain_rows` of `routes/tasks.py` are ported by C15/C37, `_router_rows`/`_metrics_row` by routers/agents. `test_notifications.py::test_confirmed_empty_router_release_clears_previous_failure` → routers. `internal/tasks` (315 lines) and `api/notifications` have no tests of their own. `notifications/compat.go` holds an agent-registry copy (switch when agents #20 lands).

Reviewer notes: Web listener: 14 Python cases are nginx-specific (reload failure restore, nginx validation, docker backend address unreachable by nginx, source update template) or route-level (`test_get_and_post_require_authentication`, `test_success_api_returns_active_settings`, `test_missing_port_is_rejected`, custom Sub-Store/backend port reservation, internal services sharing a port, IPv4-only kernel). Reviewer must classify each as obsolete-by-design (document) or missing (port); the reserved-port and IPv4-only cases look portable.

## C08 — rule syntax, matcher, rule utils

**Status: `partial`** · prereqs: C02 · Go: internal/rules

Files:
- `backend/utils/rule_syntax.py` (216)
- `backend/utils/rule_matcher.py` (690)
- `backend/utils/rule_utils.py` (195)

Tests: `test_remote_rule_cache_edges.py` (7), `test_rule_cache_identity.py` (7), `test_rule_matcher_behavior.py` (15), `test_rule_matcher_index.py` (28), `test_rule_syntax.py` (24)

Routes: none.

Evidence: `internal/rules` syntax.go, matcher.go, ruleutils.go, pyre_*.go (Python `re` parser + RE2/regexp2), idna.go. `test_rule_matcher_behavior.py` 15/15, `test_rule_matcher_index.py` 18/28 (missing 10 are memory-packing internals `_PackedSourceLines`/`_SourceLinesBuilder` — Python mechanics), `test_rule_syntax.py` 9/24 + corpus.

Missing / deferred: Missing in `rule_utils.py`: `get_rules_dir`, `read_validated_rule_cache`, `write_validated_rule_cache`, `refresh_remote_rule`, `read_rule_source_content`, `save_rule_to_local` (they need rule fetch/ruleset content; port with C14). Deferred tests: `test_rule_syntax.py` 15 cases through `ruleset_content.inspect/validate_domain_content` and rule routes (→ C14/C32); `test_rule_cache_identity.py` 0/7 and `test_remote_rule_cache_edges.py` 0/7 (cache identity/legacy-name cache → C14, route cases → C32/C33).

Reviewer notes: Accepted: `\N{name}` and repeat counts > int32 give `*UnsupportedRegexError`; backtracking matches cut off after 1 s count as no match. `rules/compat.go` still holds copies of already-ported helpers: `safeExceptionDetails` returns `exception_type=%T` (Go type name, e.g. `*fs.PathError`) while `urlx.SafeExceptionDetails` exists — switch and check the log text; `Logger` interface instead of `logx`; `ProfileValidationError` instead of `repo.Error` (rules cannot import repo? it can — rules imports only jsonx; check for a cycle via repo→rules first). Remaining compat copies → C14 (ruleset_content), C12 (converters/common), C26 (device policy).

## C09 — matcher cache, group candidates, reorder, strategy refs, connection rules

**Status: `ported`** · prereqs: C08 · Go: internal/rules

Files:
- `backend/utils/domain_matcher_cache.py` (237)
- `backend/utils/group_candidates.py` (303)
- `backend/utils/reorder.py` (57)
- `backend/utils/strategy_references.py` (27)
- `backend/utils/connection_rules.py` (84)

Tests: `test_connection_ip_rules.py` (8), `test_domain_matcher_cache.py` (17)

Routes: none.

Evidence: `internal/rules` matchercache.go, groups.go, reorder.go, strategy.go, connection.go. All defs matched (`apply_ip`→`ApplyIPRule`, `_destination_network`→`ParseDestination`; `clear_cache` → check `MatcherCache` reset). `test_domain_matcher_cache.py` 16/17, `test_connection_ip_rules.py` 5/8.

Missing / deferred: Deferred: `test_connection_ip_rules.py` 3 cases (`test_single_address_append_preserves_content_and_cache`, `test_yaml_payload_append_remains_valid_and_keeps_comments`, `test_under_lock_changes_cannot_redirect_ip_writes`) need `ConnectionDeps.ReadRulePayload/AppendRulePayload` from C14 and `RulesetInfo` from C37. `test_domain_matcher_cache.py::test_replaced_file_during_lazy_read_is_retried_on_same_handle` not ported — reviewer: port or justify.

Reviewer notes: Reviewer: confirm `domain_matcher_cache.clear_cache` has a Go equivalent (no name match).

## C10 — manual node parser, subscription hosts/filter/dialers

**Status: `ported`** · prereqs: C08 · Go: internal/subs

Files:
- `backend/utils/subscription_parser.py` (428)
- `backend/utils/subscription_hosts.py` (465)
- `backend/utils/subscription_filter.py` (50)
- `backend/utils/subscription_dialers.py` (299)

Tests: `test_node_library_subscription_filter.py` (3), `test_subscription_dialers.py` (12), `test_subscription_hosts.py` (22), `test_subscription_regex_filter.py` (4)

Routes: none.

Evidence: `internal/subs` parser.go, hosts.go, filter.go, dialers.go, psl.go (+ corpora parser/hosts/filter/dialers/psl generated from Python by `testdata/gen.py`). Pure-function cases of `test_subscription_hosts.py` (8/22), `test_subscription_dialers.py` (2/12), `test_subscription_regex_filter.py` (2/4) in `ported_test.go`.

Missing / deferred: Not ported (no caller): `subscription_parser.parse_subscription`, `parse_clash_yaml`. Deferred: the remaining `test_subscription_hosts.py` cases (scheduled download, provider endpoint, preview route, agent content invalidation → C15/C17/C30/agents), `test_subscription_dialers.py` 10 cases (generation/delivery → C17/C18), `test_subscription_regex_filter.py` 2 (provider + save route → C17/C30), `test_node_library_subscription_filter.py` 0/3 (preview routes → C30/C33).

Reviewer notes: Accepted: `\N{name}` regex filter → filter not applied (documented in progress/5-subs.md). Spot-checked `filter.go` vs `subscription_filter.py`: same branches and messages. `subs/compat.go` = dialer_references copy; it stays (C12's `internal/convert` imports subs for `ValidateProxyChains` → cycle) and `subs/compat_drift_test.go` checks it against convert's Python corpus. C12 fixed two copy defects there: str-valued `manual_nodes`/`include_groups`/`proxies` iterate characters; an empty `!!binary` type is falsy.

## C11 — proxy chains, commented Mihomo editor, listeners, proxy-server DNS

**Status: `partial`** · prereqs: C10 · Go: internal/subs, internal/subs/commented

Files:
- `backend/utils/proxy_chains.py` (262)
- `backend/utils/commented_mihomo.py` (112)
- `backend/utils/mihomo_listeners.py` (87)
- `backend/utils/proxy_server_dns.py` (394)

Tests: `test_mihomo_listener_lifecycle.py` (10), `test_proxy_chains.py` (18), `test_proxy_server_dns.py` (22)

Routes: none.

Evidence: `internal/subs` chains.go, listeners.go, proxyserverdns.go, `subs/commented` (3.4k lines: ruamel round-trip port) + corpora chains/dns/commented* (random 2411 rows, anchors 1884, realistic 4911, scalars 2080, blocks 1092). `test_mihomo_listener_lifecycle.py` 10/10 (listener-level), `test_proxy_chains.py` 8/18, `test_proxy_server_dns.py` 5/22 (helpers; `apply_node_dns` corpus-tested). Latest subs commits 17611a91, b7817902.

Missing / deferred: **Open items (coordinator, 2026-11-15) — module cannot become `verified` until closed or accepted by the maintainer:** (1) CRLF documents with comments/blank lines between entries are refused (118 random-corpus rows; ruamel writes mixed `\r\r\n`/`\n`; est. 1–2 days, needs a port of ruamel's scanner comment handling); (2) a commented block collection expanded inside a flow collection is refused (33 anchor rows; est. half a day); (3) small: lone `-` plain scalar, comment on a null item of a nested compact sequence, `!!binary`/`!!map`/`!!seq`/`!!set` tags (≈20 rows). (The yamlx `1,000`/`!!int '0x-1F'` item is recorded under C02.) Realistic corpus of 128 repo configs: 0 refused, 0 mismatches. Deferred tests: `test_proxy_chains.py` 10 cases through generation/Surge/provider (→ C17/C18/C19), `test_proxy_server_dns.py` 17 cases through `generate` (→ C18) and agent core versions (→ agents).

Reviewer notes: Refusals return Python's failure message and the route answers 400 with the original text kept (refusing beats different bytes). 24 Python-crash anchor rows are documented as is. `chains.go:489` TODO: needs a `PyRegex.Groups()` method in rules.

## C12 — converter common, Mihomo node conversion, dialer references, EasyTier

**Status: `ported`** (not reviewed) · prereqs: C09, C11 · Go: `internal/convert`

Files → Go:
- `backend/converters/common.py` (118) → `convert/common.go` (`UDPFallback`, `NTPDirect`, `EmittedRules`, `NTPDirectRule`, `AfterMatchComment`, `NetworkAsProtocol`, `HasPortList`, `SetGeneralOption`, `RulesetFormat`, `ParseHostsPairs`); `compat.go` = `_has_yaml_payload` copy (TODO C14)
- `backend/converters/mihomo.py` node group → `convert/mihomo_nodes.go`: `IndentDumperOptions` (IndentDumper), `ApplyGithubProxyDomain`, `MihomoUsedPorts`, `MihomoListenPorts`, proxy YAML LRU cache (`loadProxyYAML`), `ParseStructuredProxyString`, `InspectStructuredProxyString` (see decisions), `WithNodeConversions` + `ConversionResults` (node_conversions), `ConvertNodeToMihomo`; region-probe constants
- `backend/utils/proxy_utils.py` → `FixProxyFields`
- `backend/utils/dialer_references.py` → `convert/dialers.go` (derived from the old faithful `repo/compat_dialers.go`), `convert/dialers_api.go`: `ValidateShapes`, `ValidateDialers`, `DependencyGraph` (`*Graph`, `Ref{Kind, ID}`, `Ref.IDValue`), `GroupMembers`, `GroupDependencies`, `RawDialer`, `DialerTarget`, `CheckCycles`, `IncomingDialers`, `ResolveDialer`, `OverlayDialer`, `FilterProviderCandidates`, `ProviderProxyName`, `ValidateEmitted` (`EmitOptions`), `RejectSurgeDialers`, `DependencyNodeIDs`
- `backend/common/mihomo_easytier.py` → `convert/easytier.go` (+ `ipaddr.go`: ipaddress.ip_network/ip_interface/ip_address rules): `NormalizeEasyTierSettings`, `EasyTierSettings`, `EasyTierDefaultSettings`, `EasyTierApplies`, `EasyTierKeepaliveGroupName`, `ApplyEasyTier`, `RedactEasyTierSettings`, `EasyTierFields`, constants
- `convert/proxyyaml_loose.go`: the yaml.v3-based loose loader moved from `repo/compat_proxystring.go`

Tests (`convert/ported_test.go`, `corpus_test.go`):
- `test_proxy_yaml_cache.py` 14/14 (semantic cases via the corpus rows + a second cached call; special types/aliases/cycles adapted: `!!set`, non-string keys and recursive nodes give nil in Go — yamlx limit; deepcopy-lock test replaced by a lock-free check)
- `test_mihomo_easytier.py` 11/13 as pure `ApplyEasyTier`/`NormalizeEasyTierSettings` tests (the generate round trip of each → C18); deferred: `test_surge_and_loon_ignore_the_setting` (C19/C20), `test_api_round_trip_rejection_and_desensitized_export` (C34 + export hook)
- `test_dialer_provider_scope.py` 3/4 (deferred: `test_provider_membership_cycle_rejected_before_side_effects`, routes → C17/C18/C30/agents)
- `test_dialer_round2.py` 2/10 (deferred 8 route/generation cases → C17/C18/C30/C33)
- `test_dialer_review_fixes.py` 1/16 (deferred 15 route cases → C17/C18/C30/C33)
- Log lines of `convert_node_to_mihomo`: `TestConversionLogLines`.
- `test_node_conversion_cache.py` 1/12 (`test_rendering_again_does_not_start_sub_store` with a caching fake; the other 11 test sub_store_client's own cache → C13) + `TestNodeConversionsReuse`
- Differential corpus `convert/testdata/corpus.jsonl.gz`: 37577 Python rows over 33 functions (common, nodes, dialers incl. 2500 random profiles and 2500 emitted configs, EasyTier 1500×3), all equal (results, mutated arguments, intentional error messages; unintended exceptions must be errors).

Compat switch-over:
- `repo`: switched. `repo/compat_dialers.go` keeps only the shared Python helpers and wraps `convert` (errors → `HelperError` classes); `repo/compat_proxystring.go` deleted (its loader moved to convert); `parseStructuredProxyString` = `convert.InspectStructuredProxyString`; `compat_test.go` golden passes (adapter `goldenDefault` for timestamps/bytes). repo → convert → subs → rules has no cycle.
- `subs`: cycle (convert needs `subs.ValidateProxyChains`, subs' chains/dialers need dialer refs) → copy kept, `subs/compat_drift_test.go` runs it over convert's corpus (2 copy defects fixed, see C10).
- `rules/compat.go` `RulesetFormat`: cycle (convert imports rules) → copy kept, `rules/compat_drift_test.go` (294 rows equal).
- Not done (outside C12's packages): `repo.Hooks.RedactEasytierSettings` can now be `convert.RedactEasyTierSettings` (register in `server.wireCore`, convert cannot import repo).

Decisions / differences from Python:
- `validate_proxy_chains` = `subs.ValidateProxyChains` plus the unhashable `follow_group` TypeError subs lacks.
- Sub-Store is injected: `ProxyStringConverter.ConvertProxyString(ctx, s) *jsonx.Object` (nil = None); `node_conversions` is per-context, not thread-local.
- `provider_proxy_name` re.sub: validated by `rules.CompilePyRegex`, run on regexp2 with `(?P<n>)`/`(?P=n)` rewritten (`netPattern`); ask C08 for a `PyRegex` replace/submatch API to drop this.
- YAML values jsonx cannot hold (non-string keys, recursive containers, `!!set`): `ParseStructuredProxyString` → nil (string goes to Sub-Store); `InspectStructuredProxyString` (raw_dialer, repo import rewrite) falls back to the loose loader (keys as json.dumps writes them) — what those Python callers observe.
- Ports: bools count as ports 1/0 (Python keeps `True`); `MihomoListenPorts` lists listener ports ascending (Python: set order). Equal set members keep the first (`7890.0` hides `7890`), as in Python.
- Ids that are not strings live in `Ref.ID` as `"\x00"+repr`; `Ref.IDValue()` decodes numbers/None.
- Proxy YAML cache budget uses an approximation of `sys.getsizeof` (only decides caching).
- Python crashes on malformed input (KeyError/TypeError/AttributeError/StopIteration…) are `*PyError` with Python's class; messages are approximate.

Commits: 520b8bf9 (WIP code), bcfa7ee2 (corpus), a6d1be35 (ported tests), dae8afba (compat switch-over; the deletion of `repo/compat_proxystring.go` landed in 8507a6f4 through the shared index), + final commit (log-line test, status). Last run: vet clean, `go test -race` ok for convert, repo, subs, rules.

Reviewer hints: compare `dialers.go` with `dialer_references.py` (it started as the repo copy, extended); `pySub`/`netPattern`; the yamlx-limit decisions above; `MihomoListenPorts` order.

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/convert; go vet ./internal/convert/... && go test -count=1 ./internal/convert/... ./internal/repo/... ./internal/subs/... ./internal/rules/...'`
- corpus: `rt --back server/internal/convert/testdata -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/gen.py'` (deterministic; gzip mtime 0)

## C13 — Sub-Store client, runtime, subscription cache, dependencies

**Status: `missing`** · prereqs: C12, C10 · Go: internal/substore (new)

Files:
- `backend/utils/sub_store_client.py` (453)
- `backend/utils/sub_store_runtime.py` (233)
- `backend/utils/subscription_cache.py` (149)
- `backend/utils/dependencies.py` (358)

Tests: `test_dependencies.py` (12), `test_sub_store_no_flow.py` (4), `test_sub_store_runtime.py` (12), `test_subscription_cache_cleanup.py` (4), `test_subscription_fetch_requests.py` (15)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Also implements the 3 dependency routes of `routes/settings.py` (C06 missing list) in `internal/api/settings`. `subscription_cache` calls `agents.content_sync.invalidate_content_sync` (agents domain): use a hook/interface. Sub-Store itself is kept (only its HTTP client is ported). Provides nothing to `tasks` yet (that is C15).

## C14 — remote rule fetch, ruleset content, remote sources, website identity

**Status: `missing`** · prereqs: C12, C08 · Go: internal/rulefetch (new)

Files:
- `backend/utils/rule_fetch.py` (346)
- `backend/utils/ruleset_content.py` (722)
- `backend/utils/remote_rule_sources.py` (45)
- `backend/utils/website_identity.py` (89)

Tests: `test_mihomo_yaml_delivery.py` (7), `test_remote_rule_cache.py` (8), `test_remote_ruleset_validation.py` (12), `test_rule_delivery_proxy.py` (3), `test_rule_fetch_fallback.py` (6), `test_rule_fetch_proxy_integration.py` (19), `test_rule_fetch_retries.py` (10), `test_rule_proxy_freshness.py` (3), `test_ruleset_classical_format.py` (15), `test_ruleset_domain_format.py` (13), `test_website_identity.py` (11)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Also completes C08's missing `rule_utils` functions and the deferred rule_syntax/local_http/remote-cache tests; replaces `rules/compat.go` ruleset_content copies and `api/settings/compat.go` (`validate_rule_url`). `rule_fetch` calls `agents.diagnostics.failure_text` (agents) — copy or interface. `website_identity` imports `region_check` (C16): small compat or move order.

## C15 — subscription refresh, health, scheduled refresh

**Status: `missing`** · prereqs: C13, C14, C07 · Go: internal/substore (new)

Files:
- `backend/utils/subscription_refresh.py` (139)
- `backend/utils/subscription_health.py` (114)
- `backend/utils/scheduled_refresh.py` (347)

Tests: `test_scheduled_tasks.py` (13), `test_subscription_refresh_policy.py` (12)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Provides `tasks.SetRows` for slots `subscription` and `rule_source` (+ runners) — ports `routes/tasks.py::_subscription_rows`/`_rule_rows`. Background loop via `lifecycle.Loop`.

## C16 — health check, latency, node DNS, region check, domain probe

**Status: `missing`** · prereqs: C12, C13 · Go: internal/health (new)

Files:
- `backend/utils/health_check.py` (82)
- `backend/utils/node_latency.py` (254)
- `backend/utils/node_dns.py` (189)
- `backend/utils/region_check.py` (367)
- `backend/utils/domain_probe.py` (197)

Tests: `test_domain_probe.py` (16), `test_health_check_advanced.py` (4), `test_health_https_defaults.py` (6), `test_health_persistence.py` (2), `test_health_review_r1.py` (8), `test_health_review_re1.py` (10), `test_health_review_re2.py` (8), `test_health_validation.py` (9), `test_node_dns.py` (13), `test_node_latency.py` (25), `test_region_check.py` (19)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Replaces `repo/compat_health.go`. `node_latency` needs `convert_node_to_mihomo` (C12). `web_listener` is not here (done in C07).

## C17 — provider delivery and responses

**Status: `missing`** · prereqs: C15 · Go: internal/provider (new)

Files:
- `backend/utils/provider_delivery.py` (424)
- `backend/utils/provider_response.py` (116)

Tests: `test_dialer_provider_content.py` (3), `test_provider_error_locations.py` (14), `test_provider_filter_isolation.py` (1), `test_provider_response.py` (9)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: `provider_delivery` lazily imports `routes.aggregations.generate_aggregation_provider` / `get_subscription_proxies_yaml` and `routes.subscriptions.get_subscription_proxies_yaml` (logic living in route files, C30/C31) and `agents.deployment_locations`/`agents.diagnostics`. Port the provider-YAML builders of those route files as package functions here or define interfaces that C30/C31 fill; say which in this section.

## C18 — Mihomo config generation, Mihomo base settings

**Status: `missing`** · prereqs: C12, C15, C16, C17 · Go: internal/convert (new)

Files:
- `backend/converters/mihomo.py` (905) — generation group (see module notes)
- `backend/common/mihomo_base_settings.py` (370)

Tests: `test_dynamic_proxy_chains.py` (18), `test_group_exclusion_generation.py` (8), `test_mihomo_base_settings.py` (21)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Rest of `converters/mihomo.py`: `CustomConfigYAMLError`, `validate_ipv6_flags`, `_default_mihomo_template`, `split_rules_and_rulesets`, `build_provider_health_check`, `apply_no_resolve_to_logic_rule`, `normalize_find_process_mode`, `parse_mosdns_custom_hosts`, `sync_mosdns_hosts`, `inject_region_probe`, `generate_mihomo_config` (585 lines alone), `get_mihomo_provider_downloads`, `get_mihomo_ruleset_downloads` (≈905 lines). **Cross-domain:** `generate_mihomo_config` calls `common.linux_gateway.apply_linux_gateway`/`gateway_settings` (agents domain); `mihomo_base_settings` calls `gateway_onboarding.mihomo_dns_loop`, `linux_gateway.fake_ip_network`/`gateway_settings`, `router_split.is_full_tun`, `setup_flow`. Port those leaf functions as `compat.go` copies (TODO(port)) or coordinate with the agents/routers domains first. High-value generation goldens: generate from Python on the test server for the 128 repository base configs.

## C19 — Surge converter

**Status: `missing`** · prereqs: C18 · Go: internal/convert (new)

Files:
- `backend/converters/surge.py` (1129)

Tests: `test_snell_shadow_tls.py` (6)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: `surge.py` imports `routes.aggregations.generate_aggregation_provider` (C31) — use the C17 decision. Manual node strings keep `subs.ParseURIList` (follow-up after parity: switch Surge/Loon to Sub-Store).

## C20 — Loon converter

**Status: `missing`** · prereqs: C19 · Go: internal/convert (new)

Files:
- `backend/converters/loon.py` (764)

Tests: `test_loon_generation.py` (10), `test_surge_loon_group_exclusion.py` (5), `test_udp_fallback.py` (15)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Loon shares helpers with Surge; port after C19.

## C21 — MosDNS converter

**Status: `missing`** · prereqs: C14 · Go: internal/mosdns (new)

Files:
- `backend/converters/mosdns.py` (1261)

Tests: `converters/test_mosdns.py` (16), `test_domain_wildcard_oxidns_edges.py` (2)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: `converters/oxidns.py` imports this converter, so it comes before the OxiDNS cluster.

## C22 — OxiDNS converter, native loader, references, match targets

**Status: `missing`** · prereqs: C05, C21 · Go: internal/oxidns (new)

Files:
- `backend/converters/oxidns.py` (490)
- `backend/common/oxidns_native.py` (264)
- `backend/common/oxidns_references.py` (64)
- `backend/common/oxidns_match_targets.py` (61)
- `backend/native_test_helpers.py` (34)

Tests: `test_classical_native.py` (1), `test_domain_wildcard_mihomo_native.py` (1), `test_mihomo_yaml_native.py` (1), `test_oxidns_domain_only_core.py` (6), `test_oxidns_match_targets.py` (10), `test_oxidns_native.py` (18)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: OxiDNS cluster C22–C27 + C29 is one strongly connected component in Python (lazy imports in both directions between converters/oxidns, oxidns_common, _routing, _updates, editor, native_apply, device_policy, rule_dns, oxidns_settings). Plan it as one Go package `internal/oxidns`; each module adds its files there. Finish C05's normalizer here. `test_fast_yaml.py` native-loader cases already pass via `yamlx.LoadWith(SchemaNative)`.

## C23 — OxiDNS validation and constraints

**Status: `missing`** · prereqs: C22 · Go: internal/oxidns (new)

Files:
- `backend/common/oxidns_validation.py` (310)
- `backend/common/oxidns_reference_validation.py` (368)
- `backend/common/oxidns_plugin_constraints.py` (676)
- `backend/common/oxidns_global_constraints.py` (188)

Tests: `test_oxidns_validation_api.py` (10), `test_oxidns_validation_global.py` (7), `test_oxidns_validation_plugins.py` (17), `test_oxidns_validation_references.py` (12)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Pure validation; good candidate for a Python-generated corpus (inputs → error text).

## C24 — OxiDNS common settings

**Status: `missing`** · prereqs: C23 · Go: internal/oxidns (new)

Files:
- `backend/common/oxidns_common.py` (1175)

Tests: `test_oxidns_feature_lifecycle.py` (8)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

## C25 — OxiDNS common routing, DNS watchdog

**Status: `missing`** · prereqs: C24 · Go: internal/oxidns (new)

Files:
- `backend/common/oxidns_common_routing.py` (1192)
- `backend/common/dns_watchdog.py` (94)

Tests: `test_dns_watchdog_config.py` (8), `test_dns_watchdog_core.py` (1)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: `dns_watchdog` is also used by agents (deployment_bundle, rule_sync): export `EnsureLocalDNSWatchdog` etc.

## C26 — OxiDNS common updates, device policy, editor

**Status: `missing`** · prereqs: C25 · Go: internal/oxidns (new)

Files:
- `backend/common/oxidns_common_updates.py` (465)
- `backend/common/oxidns_device_policy.py` (386)
- `backend/common/oxidns_editor.py` (338)

Tests: `test_oxidns_common.py` (36), `test_oxidns_common_lifecycle.py` (21), `test_oxidns_device_policy.py` (15), `test_oxidns_device_policy_core.py` (2), `test_oxidns_device_policy_side_entries.py` (15), `test_oxidns_editor.py` (30), `test_oxidns_fallback_lifecycle.py` (14), `test_oxidns_resource_lifecycle.py` (16), `test_oxidns_service_domain.py` (6)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Replaces `repo/compat_oxidns.go` (device notes, client range, bindings, `normalize_action`) partly and `rules/compat.go` device-policy copies.

## C27 — rule DNS actions

**Status: `missing`** · prereqs: C26 · Go: internal/oxidns (new)

Files:
- `backend/common/rule_dns.py` (885)

Tests: `converters/test_oxidns.py` (43), `test_oxidns_common_routing.py` (54), `test_oxidns_domain_only_native.py` (9), `test_oxidns_ip_probe.py` (9), `test_oxidns_routing_index.py` (3), `test_oxidns_rule_actions_core.py` (5), `test_proxy_group_dns_follow.py` (6), `test_rule_dns.py` (33), `test_rule_dns_policy_groups.py` (7)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: **Cross-domain:** `rule_dns` calls `agents.rule_sync.settings_for` (agents) and `common.rule_ip_presence.classical_ip_lines` (routers): compat copies or interface.

## C28 — MosDNS archive and routes

**Status: `missing`** · prereqs: C21, C15 · Go: internal/mosdns, internal/api/mosdns (new)

Files:
- `backend/utils/mosdns_archive.py` (244)
- `backend/routes/mosdns.py` (594)

Tests: `test_converter_review_regressions.py` (7), `test_domain_wildcard_delivery.py` (3), `test_mosdns_archive.py` (15), `test_mosdns_regex_delivery.py` (2), `test_mosdns_rule_fields.py` (5), `test_remote_rule_consumers.py` (16)

Routes (15, 0 done):

`GET /api/mosdns/api-settings`; `POST /api/mosdns/api-settings`; `GET /api/mosdns/cache-settings`; `POST /api/mosdns/cache-settings`; `GET /api/mosdns/custom-matches`; `POST /api/mosdns/custom-matches`; `GET /api/mosdns/dns-servers`; `POST /api/mosdns/dns-servers`; `GET /api/mosdns/log-settings`; `POST /api/mosdns/log-settings`; `GET /api/mosdns/rule-proxy`; `GET /api/mosdns/rulesets`; `POST /api/mosdns/rulesets`; `POST /api/mosdns/settings`; `GET /api/profiles/<profile_id>/mosdns/rule-proxy`

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: `mosdns_archive` imports `routes.mosdns._fetch_remote_content` and `scheduled_refresh` (C15). Rule-proxy endpoint is public with config token/JWT — covers deferred `test_security_regressions.py` cases.

## C29 — OxiDNS native apply, archive, routes

**Status: `missing`** · prereqs: C27, C28 · Go: internal/oxidns, internal/api/oxidns (new)

Files:
- `backend/common/oxidns_native_apply.py` (520)
- `backend/utils/oxidns_archive.py` (28)
- `backend/routes/oxidns.py` (191)

Tests: `test_oxidns_archive.py` (4), `test_oxidns_common_api.py` (2), `test_oxidns_download_aliases.py` (5), `test_oxidns_hosts_cleanup.py` (9), `test_oxidns_integration.py` (16), `test_oxidns_native_apply.py` (31), `test_oxidns_native_cleanup.py` (11), `test_oxidns_native_restore.py` (20), `test_oxidns_settings_routes.py` (7), `test_oxidns_withdrawn_downloads.py` (10)

Routes (18, 0 done):

`POST /api/oxidns/common-settings`; `POST /api/oxidns/common-settings/apply`; `POST /api/oxidns/editor`; `POST /api/oxidns/native-settings/apply`; `POST /api/oxidns/render`; `GET /api/oxidns/settings`; `POST /api/oxidns/settings`; `POST /api/oxidns/template-preview`; `POST /api/oxidns/validate`; `POST /api/profiles/<profile_id>/oxidns/common-settings`; `POST /api/profiles/<profile_id>/oxidns/common-settings/apply`; `POST /api/profiles/<profile_id>/oxidns/editor`; `POST /api/profiles/<profile_id>/oxidns/native-settings/apply`; `POST /api/profiles/<profile_id>/oxidns/render`; `GET /api/profiles/<profile_id>/oxidns/settings`; `POST /api/profiles/<profile_id>/oxidns/settings`; `POST /api/profiles/<profile_id>/oxidns/template-preview`; `POST /api/profiles/<profile_id>/oxidns/validate`

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Also registers the 9 `/api/profiles/<id>/oxidns/*` wrapper routes (rule 8: the delegate package registers the profile wrapper). `oxidns_archive` imports `mosdns_archive` (C28).

## C30 — subscription and node routes, node deletion

**Status: `missing`** · prereqs: C17, C16, C19, C20 · Go: internal/api/subscriptions, api/nodes (new)

Files:
- `backend/routes/subscriptions.py` (696)
- `backend/routes/nodes.py` (213)
- `backend/common/node_deletion.py` (85)

Tests: `test_dialer_activation_shadow.py` (3), `test_dialer_combined_export.py` (9), `test_dialer_delivery_snapshot.py` (3), `test_dialer_delivery_staging.py` (5), `test_dialer_proxy.py` (23), `test_dialer_standalone_activation.py` (4), `test_node_deletion.py` (9), `test_subscription_cache_preview.py` (4)

Routes (26, 0 done):

`GET /api/nodes`; `POST /api/nodes`; `DELETE /api/nodes/<node_id>`; `PUT /api/nodes/<node_id>`; `POST /api/nodes/delete`; `POST /api/nodes/delete-preview`; `GET /api/nodes/latency`; `POST /api/nodes/latency`; `POST /api/nodes/latency/dns/check`; `GET /api/nodes/latency/settings`; `POST /api/nodes/latency/settings`; `POST /api/nodes/parse-lines`; `POST /api/nodes/reorder`; `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`; `GET /api/subscriptions`; `POST /api/subscriptions`; `GET /api/subscriptions/<resource_id>/health-impact`; `DELETE /api/subscriptions/<sub_id>`; `PUT /api/subscriptions/<sub_id>`; `POST /api/subscriptions/<sub_id>/fetch`; `GET /api/subscriptions/<sub_id>/nodes`; `GET /api/subscriptions/<sub_id>/proxies`; `GET /api/subscriptions/health`; `GET /api/subscriptions/proxies`; `POST /api/subscriptions/reorder`; `GET /api/subscriptions/test`

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Registers `/api/profiles/<id>/subscriptions/<sub_id>/proxies` too. Fills `repo.Hooks.PlanNodeDeletion`. Subscription provider endpoints are public (token) — deferred security cases.

## C31 — aggregation routes, device notes

**Status: `missing`** · prereqs: C30, C26 · Go: internal/api/aggregations (new); device notes in internal/oxidns or own package

Files:
- `backend/routes/aggregations.py` (617)
- `backend/common/device_notes.py` (105)

Tests: `test_aggregation_reorder.py` (3), `test_dialer_rendered_bundle.py` (4), `test_shared_resource_routes.py` (8)

Routes (11, 0 done):

`GET /api/aggregations`; `POST /api/aggregations`; `DELETE /api/aggregations/<agg_id>`; `GET /api/aggregations/<agg_id>`; `PUT /api/aggregations/<agg_id>`; `GET /api/aggregations/<agg_id>/count`; `GET /api/aggregations/<agg_id>/preview`; `GET /api/aggregations/<agg_id>/provider`; `GET /api/aggregations/<resource_id>/health-impact`; `POST /api/aggregations/reorder`; `GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider`

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Registers `/api/profiles/<id>/aggregations/<agg_id>/provider`. `device_notes` is used by repo validation (copy in `repo/compat_oxidns.go` to switch), agents and routers.

## C32 — rule routes

**Status: `missing`** · prereqs: C09, C14, C27 · Go: internal/api/rules (new)

Files:
- `backend/routes/rules.py` (1107)

Tests: `test_domain_wildcard_inputs.py` (5), `test_mihomo_classical_delivery.py` (13), `test_profile_rule_routes.py` (29), `test_rule_compose_route.py` (24), `test_rule_duplicates.py` (7), `test_rule_fetch_proxy_routes.py` (7), `test_rule_validation_routes.py` (8)

Routes (21, 0 done):

`GET /api/profiles/<profile_id>/rules`; `POST /api/profiles/<profile_id>/rules`; `GET /api/profiles/<profile_id>/rules/local/<name>`; `GET /api/rule-sets`; `POST /api/rule-sets`; `DELETE /api/rule-sets/<rule_set_id>`; `PUT /api/rule-sets/<rule_set_id>`; `GET /api/rules`; `POST /api/rules`; `DELETE /api/rules/<rule_id>`; `PUT /api/rules/<rule_id>`; `POST /api/rules/batch`; `POST /api/rules/compose`; `GET /api/rules/dns`; `PUT /api/rules/dns`; `POST /api/rules/find-duplicates`; `POST /api/rules/group-name`; `GET /api/rules/local/<name>`; `POST /api/rules/match-test`; `PUT /api/rules/policy`; `POST /api/rules/reorder`

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Registers `/api/profiles/<id>/rules` and `/rules/local/<name>`. `routes/rules.py` imports `agents.rule_sync.pinned_withheld` (agents) and the OxiDNS cluster; `find-duplicates` lives here (noted in PORTING).

## C33 — rule library and proxy group routes

**Status: `missing`** · prereqs: C32 · Go: internal/api/rulelibrary, api/proxygroups (new)

Files:
- `backend/routes/rule_library.py` (540)
- `backend/routes/proxy_groups.py` (436)

Tests: `test_proxy_group_batch_health.py` (6), `test_proxy_group_regex_exclusion.py` (9), `test_rule_library_create.py` (9)

Routes (25, 0 done):

`GET /api/profiles/<profile_id>/proxy-groups`; `POST /api/profiles/<profile_id>/proxy-groups`; `GET /api/profiles/<profile_id>/rule-library/content/<rule_id>`; `GET /api/proxy-groups`; `POST /api/proxy-groups`; `DELETE /api/proxy-groups/<group_id>`; `PUT /api/proxy-groups/<group_id>`; `PUT /api/proxy-groups/batch-health-url`; `GET /api/proxy-groups/health-defaults`; `POST /api/proxy-groups/preview-regex`; `POST /api/proxy-groups/reorder`; `GET /api/rule-library`; `POST /api/rule-library`; `DELETE /api/rule-library/<rule_id>`; `PUT /api/rule-library/<rule_id>`; `GET /api/rule-library/<rule_id>/draft-content`; `PUT /api/rule-library/<rule_id>/draft-content`; `POST /api/rule-library/cache`; `POST /api/rule-library/check-content`; `GET /api/rule-library/content/<rule_id>`; `GET /api/rule-library/proxy-domains`; `POST /api/rule-library/proxy-domains`; `POST /api/rule-library/reorder`; `POST /api/rule-library/test`; `POST /api/rule-library/test-single`

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Registers `/api/profiles/<id>/rule-library/content/<rule_id>` and `/proxy-groups`. Proxy-group routes run `subs.RelinkGroupListeners` (deferred listener-lifecycle cases of C11).

## C34 — generate, config and profile routes, config export

**Status: `missing`** · prereqs: C18, C19, C20, C28, C29, C31, C33 · Go: internal/api/generate, api/config, api/profiles, internal/configexport (new; internal/redact exists)

Files:
- `backend/routes/generate.py` (320)
- `backend/routes/config.py` (497)
- `backend/routes/profiles.py` (336)
- `backend/common/config_export.py` (290)

Tests: `test_agent_log_redaction.py` (2), `test_flow_ui_apis.py` (9), `test_profile_api.py` (19), `test_profile_generation.py` (10), `test_profile_scoped_artifacts.py` (16), `test_sensitive_config_exports.py` (25)

Routes (59, 0 done):

`GET /api/config/<profile_id>/loon`; `GET /api/config/<profile_id>/loon/<filename>`; `GET /api/config/<profile_id>/mihomo`; `GET /api/config/<profile_id>/mosdns`; `GET /api/config/<profile_id>/oxidns`; `GET /api/config/<profile_id>/surge`; `GET /api/config/export`; `POST /api/config/import`; `GET /api/config/loon`; `GET /api/config/mihomo`; `GET /api/config/mosdns`; `GET /api/config/oxidns`; `POST /api/config/reset`; `GET /api/config/surge`; `GET /api/custom-config/loon`; `POST /api/custom-config/loon`; `GET /api/custom-config/mihomo`; `POST /api/custom-config/mihomo`; `GET /api/custom-config/mosdns`; `POST /api/custom-config/mosdns`; `GET /api/custom-config/oxidns`; `POST /api/custom-config/oxidns`; `GET /api/custom-config/surge`; `POST /api/custom-config/surge`; `POST /api/generate/<service>/artifact`; `GET /api/generate/artifacts/<preview_id>/<action>`; `POST /api/generate/loon`; `POST /api/generate/loon/preview`; `POST /api/generate/mihomo`; `POST /api/generate/mihomo/preview`; `POST /api/generate/mosdns`; `POST /api/generate/mosdns/preview`; `POST /api/generate/oxidns`; `POST /api/generate/oxidns/preview`; `POST /api/generate/surge`; `POST /api/generate/surge/preview`; `GET /api/profiles`; `POST /api/profiles`; `DELETE /api/profiles/<profile_id>`; `GET /api/profiles/<profile_id>`; `PUT /api/profiles/<profile_id>`; `GET /api/profiles/<profile_id>/agents`; `POST /api/profiles/<profile_id>/agents/rebind`; `POST /api/profiles/<profile_id>/clone`; `POST /api/profiles/<profile_id>/default`; `GET /api/profiles/<profile_id>/export`; `POST /api/profiles/<profile_id>/generate/<service>/artifact`; `POST /api/profiles/<profile_id>/generate/loon`; `POST /api/profiles/<profile_id>/generate/loon/preview`; `POST /api/profiles/<profile_id>/generate/mihomo`; `POST /api/profiles/<profile_id>/generate/mihomo/preview`; `POST /api/profiles/<profile_id>/generate/mosdns`; `POST /api/profiles/<profile_id>/generate/mosdns/preview`; `POST /api/profiles/<profile_id>/generate/oxidns`; `POST /api/profiles/<profile_id>/generate/oxidns/preview`; `POST /api/profiles/<profile_id>/generate/surge`; `POST /api/profiles/<profile_id>/generate/surge/preview`; `POST /api/profiles/<profile_id>/import`; `POST /api/profiles/import-client`

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Own routes: `routes/config.py` (24), `routes/generate.py` (12), `routes/profiles.py` CRUD/import/export/agents/generate wrappers (23). `routes/generate.py` calls `agents.deployment_bundle._pack` and `routes.agents.download_final_agent_config/read_final_agent_file` (agents domain) for artifacts; `routes/profiles.py` uses `agents.manager.AgentDeploymentConflict` and the agent manager. `config_export` → `internal/redact` already has the response-redaction part; this module adds `sanitize_config_for_output`, `redact_oxidns_settings` and registers the repo redaction hooks.

## C35 — config importer, WebDAV backup

**Status: `missing`** · prereqs: C04, C06 · Go: internal/importer, internal/webdav (new)

Files:
- `backend/utils/config_importer.py` (1148)
- `backend/utils/webdav_backup.py` (227)

Tests: `test_config_import.py` (7), `test_webdav_auto_backup.py` (24)

Routes: none.

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Fills `repo.Hooks.ConfigurationCommitted` (auto backup) and the two `/api/backup/*` routes of `routes/settings.py` (C06 missing list). `config_importer` (client config import: Clash/Surge/…) has no backend imports — self-contained, can start early.

## C36 — setup flow

**Status: `missing`** · prereqs: C34, C35 · Go: internal/setup, internal/api/setup (new)

Files:
- `backend/common/setup_flow.py` (600)
- `backend/common/setup_presets.py` (319)
- `backend/common/setup_completion.py` (236)
- `backend/common/setup_abandon.py` (90)
- `backend/routes/setup.py` (127)

Tests: `test_setup_flow.py` (76)

Routes (5, 0 done):

`GET /api/setup`; `POST /api/setup`; `GET /api/setup/<session_id>`; `PATCH /api/setup/<session_id>`; `POST /api/setup/<session_id>/<operation>`

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: **Cross-domain heavy:** setup flow uses `linux_gateway`, `gateway_onboarding`, `gateway_installation` (agents), `router_split`, `routers.store/plan/desired/base`, `routes.routers` (routers), `agents.oxidns_monitor`. Schedule after the routers and agents domains have those pieces, or with compat copies.

## C37 — domain discovery

**Status: `missing`** · prereqs: C14, C16 · Go: internal/discovery, internal/api/domaindiscovery (new)

Files:
- `backend/utils/domain_discovery.py` (175)
- `backend/utils/domain_discovery_service.py` (1309)
- `backend/routes/domain_discovery.py` (174)

Tests: `test_domain_background_lifecycle.py` (5), `test_domain_discovery.py` (27)

Routes (17, 0 done):

`GET /api/domain-discovery/agents`; `POST /api/domain-discovery/apply`; `GET /api/domain-discovery/domains`; `GET /api/domain-discovery/history`; `DELETE /api/domain-discovery/ignore`; `POST /api/domain-discovery/ignore`; `POST /api/domain-discovery/probe`; `GET /api/domain-discovery/probe/<job_id>`; `GET /api/domain-discovery/region`; `POST /api/domain-discovery/region/check`; `POST /api/domain-discovery/region/route`; `POST /api/domain-discovery/rulesets/init`; `GET /api/domain-discovery/settings`; `PUT /api/domain-discovery/settings`; `POST /api/domain-discovery/undo`; `POST /api/domain-discovery/website/identify`; `PUT /api/domain-discovery/website/note`

Evidence: No Go package; no route registered.

Missing / deferred: Everything in the files below.

Notes for porter: Provides `tasks.SetRows(domain_discovery)` (ports `routes/tasks.py::_domain_rows`) and `ConnectionDeps.RulesetInfo` for rules. Calls `agents.traffic_store`, `agents.version.compare_versions`, agent manager (agents domain). `subs.ExtractDomain` (tldextract) is ready.
## Next steps (coordinator; at most 2 config agents at a time: one porter + one reviewer)

Porting order (one fresh porter per module; each updates its section and commits):
1. **C12** converter common + Mihomo node conversion + dialer refs + EasyTier (unblocks C13, C14, C16, C17 and replaces three compat copies).
2. **C14** remote rules (also finishes C08's `rule_utils` functions and deferred rule tests; replaces `rules/compat.go` ruleset_content and `api/settings/compat.go`).
3. **C13** Sub-Store client (+ the 3 dependency routes of C06).
4. **C15** refresh/scheduled refresh (+ `tasks` slots subscription/rule_source).
5. **C16** health/latency → **C17** provider delivery (decide where aggregation/subscription provider YAML builders live).
6. **C35** config importer + WebDAV (self-contained importer; + 2 backup routes of C06, `ConfigurationCommitted` hook) — may be swapped earlier whenever the porter slot is free.
7. **C21** MosDNS converter → **C22** … **C27** OxiDNS cluster (one Go package `internal/oxidns`; finish C05's normalizer in C22, complete it by C27) → **C28** MosDNS archive/routes → **C29** OxiDNS apply/routes.
8. **C18** Mihomo generation — first agree with the coordinator how `linux_gateway`/`gateway_onboarding`/`router_split` leaf functions are provided (compat copies vs. waiting for the agents/routers domains); then **C19** Surge → **C20** Loon.
9. **C30** → **C31** → **C32** → **C33** resource routes; **C34** generate/config/profiles (needs agents deployment bundle pieces for artifacts: compat or agents domain first).
10. **C36** setup flow and **C37** domain discovery last (heaviest cross-domain dependencies).

Review order (one fresh reviewer per module, alongside the porter):
1. Now (status `ported`): **C01**, **C03** (also switch `repo/compat_rules.go` to `internal/rules`), **C04**, **C09**, **C10**.
2. **C02** after the yamlx open item (`1,000` in flow collections, `!!int '0x-1F'`) is fixed or accepted.
3. **C08** after C14 has added the missing `rule_utils` functions (also switch `rules/compat.go` copies of `urlx`/`logx`/`repo` helpers).
4. **C11** after the maintainer decides on the commented-editor open items (1)–(3).
5. **C06** after C13 and C35 added their settings routes (add `internal/redact` tests); **C07** after every `tasks` slot exists (C15, C37, agents, routers).
6. **C05** after C27.
7. Every newly ported module right after its porter finishes.

Questions for the maintainer (from this audit):
- `_rebuild_corrupt_locked` (rebuild a damaged database from a matching `config.json`) is not ported; Go refuses with a restore hint. Accept as part of the legacy decision?
- Commented editor open items (C11) and the yamlx limits (C02): implement or accept?
- Cross-domain order: C18 (Mihomo generation), C27 (rule DNS), C34 (artifacts), C36 (setup), C37 (domain discovery) call agents/routers code. Compat copies in config, or let those agents/routers modules go first?

## Coordinator decisions (2026-11-15)

1. **Corrupt-database rebuild from `config.json`** (`_rebuild_corrupt_locked`) is part of the legacy
   decision: `config.json` only exists in data dirs migrated from the pre-SQLite format, which the Go
   server does not support. Not ported; C03 keeps Python's "no safe recovery source" behaviour.
2. **C11 editor refusals / C02 yamlx limits**: the small items (lone `-`, null item comments,
   `!!binary`/`!!map`/`!!seq`/`!!set`, block collection expanded in a flow collection, yamlx `1,000` in flow
   collections and `!!int '0x-1F'`) are fixed during the modules' adversarial review. The CRLF case is
   deferred by the maintainer (2026-11-15): recorded as a to-do, decided near the end of the rewrite; it
   does not block C11 from becoming `verified` (listed as an accepted, deferred difference).
3. **Config modules that call agents/routers code** (C18, C27, C34, C36, C37): do not copy those
   functions. Declare a small interface or hook in the config package for exactly what is called (same
   pattern as `repo.Hooks`), with a fail-closed or explicitly-documented default, and list it under the
   module as "provided by <domain>". The agents/routers module that ports the Python function registers
   the implementation. The domain differential runs only after the providers exist.
4. Edge differences found in C03 spot checks (dangling symlink `lexists` vs `exists`, non-string agent
   name, non-object `mosdns`) go to the C03 reviewer: match Python's observable behaviour, except that a
   Python crash (500) may become a handled error — document each.
