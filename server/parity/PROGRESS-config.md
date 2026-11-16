# PROGRESS — domain `config`

Domain `config` (WORKFLOW.md): foundation (old packages #0–#5) and configuration/generation (#6–#19). **119 Python files, 39628 lines, 37 modules** — 19 missing, 4 partial, 6 ported, 8 verified (C01, C02, C03, C04, C08, C09, C10, C12). Only reviewers set `verified`.

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
- yamlx (since review C02 on the PyYAML port, `internal/pyyaml`): values, exception classes and texts are PyYAML's. Values jsonx cannot hold are reported after construction: non-string keys → `*NonStringKeyError` (or `*yamlx.Mapping` with `LoadOptions.KeepNonStringKeys`), recursive aliases and non-empty `!!set`/`!!omap`/`!!pairs` → `*UnrepresentableError`. CSafeDumper output (size measurement in `push_diff.py`) not reproduced.
- Commented Mihomo YAML editor (`subs/commented`): refuses (400 with Python's message, original text kept) instead of writing different bytes; open categories are listed in C11 and are still being worked on.
- Web listener: the Go server listens itself and serves the SPA (replaces nginx); nginx-specific behaviour has no counterpart (C07).
- Not ported by decision (packages.md): legacy pre-SQLite migration, `models/`, `startup.py`, `http_server.py`, `http_queue.py`, `agents/node_test_dns_cleanup.py`, `metrics_history.LEGACY_DIRS`; `subscription_parser.parse_subscription`/`parse_clash_yaml` (no caller). Tests only about those: `test_split_layout_migration.py`, 25 cases of `test_config_storage_migration.py`, `test_http_server.py`, `test_startup.py`, `test_http_queue.py`, `test_node_test_dns_cleanup.py`.

## Module table

| Id | Module | Py lines | Status | Prereqs | Routes |
|---|---|---|---|---|---|
| C01 | jsonx, store | 564 | verified | – | – |
| C02 | infra helpers | 828 | verified | C01 | – |
| C03 | repo storage core (config_repository.py part A) | 1576 | verified | C01, C02 | – |
| C04 | repo public API (config_repository.py part B), config.py | 1373 | verified | C03 | – |
| C05 | profile context, OxiDNS settings normalizer | 497 | partial | C04 | – |
| C06 | HTTP shell, auth, settings routes | 1028 | partial | C05 | 22 (17 done) |
| C07 | logs, stats, tasks, notifications, web listener | 1393 | partial | C06 | 9 (5 done) |
| C08 | rule syntax, matcher, rule utils | 1101 | verified | C02 | – |
| C09 | matcher cache, group candidates, reorder, strategy refs, connection rules | 708 | verified | C08 | – |
| C10 | manual node parser, subscription hosts/filter/dialers | 1242 | verified | C08 | – |
| C11 | proxy chains, commented Mihomo editor, listeners, proxy-server DNS | 855 | partial | C10 | – |
| C12 | converter common, Mihomo node conversion, dialer references, EasyTier | 1196 | verified | C09, C11 | – |
| C13 | Sub-Store client, runtime, subscription cache, dependencies | 1193 | ported | C12, C10 | 3 (3 done) |
| C14 | remote rule fetch, ruleset content, remote sources, website identity | 1202 | ported | C12, C08 | – |
| C15 | subscription refresh, health, scheduled refresh | 600 | ported | C13, C14, C07 | – |
| C16 | health check, latency, node DNS, region check, domain probe | 1089 | ported | C12, C13 | 5 (5 done) |
| C17 | provider delivery and responses | 540 | ported | C15 | – |
| C18 | Mihomo config generation, Mihomo base settings | 1275 | ported | C12, C15, C16, C17 | – |
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

**Status: `verified`** (review 2026-11-15/16, one round; 0 open findings) · prereqs: C01 · Go: internal/logx, jsonstore, yamlx, pyyaml, pystr, localhttp, urlx, taskstatus, resource, utilx, jsonresp, version; httpx/internal.go

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

Evidence: every def matched by name or documented rename (`get_logger`→`logx.Get`, `JsonFileStore`→`jsonstore`, `get_resource_path`/`get_backend_resource`→`resource.FromEnv().Backend`, `get_version_info`→`version.Info`, `jsonify_external`→`jsonresp.External`/`httpx.External`, `is_internal_call`→`httpx.Ctx.IsInternalCall`). Tests: `test_json_response.py` 7/10 (surrogate normalisation: jsonx surrogate limit; two Flask JSON-provider hook tests: Python mechanics), `test_local_http.py` behaviour against a plain handler (`localhttp_test.go`, incl. `TestSlowLocalResponseReportsTimeoutAndReleasesTheStream` — the reviewer note is closed), `test_fast_yaml.py` is the libyaml verification cache (no Go counterpart; `SafeLoad` always gives PyYAML's result, which is what the cache guarantees), `test_safe_url_queries.py` 4/11 + corpus.

**YAML decision (coordinator decision 5): `yamlx.Load*` now run on the PyYAML port.** `rulefetch/pyyaml` moved to `internal/pyyaml` (f9d7eafd; `ComposeWith` adds a stream name and a resolver for NativeLoader). `yamlx/construct.go` ports BaseConstructor/SafeConstructor and NativeLoader.construct_mapping on its nodes (8c432221): dispatch by tag alone (scalar tags on collections reach `construct_scalar`; `=` keys), collections as generators run in rounds like `construct_document` (decides which error wins), `flatten_mapping` mutating shared nodes, Python dict key equality (1/1.0/true, `.nan` singleton, aware datetimes by instant), Python exception classes (`*yamlx.Error{Class}`: ConstructorError/ScannerError/…, ValueError from `int()`/`float()`/dates, KeyError for `!!bool maybe`, IndexError for `!!int ""` — yaml.v3-based code panicked there —, TypeError, AttributeError, RecursionError). Evidence: yamlx load corpus 1591 texts × {safe_load, NativeLoader}: **all equal, now compared strictly** (value, or exception class and message; before only 'some error' was checked and 0 messages were required); new `review.jsonl` 735 texts × {safe_load, NativeLoader, CSafeLoader-first}: 735/735/733 (+2 accepted, below); pyyaml corpus 6065 rows; consumers' corpora unchanged and green: subs C10 (1905 rows, the six `yamlxDiffers` rows now compared and equal, list empty), C11 commented editor, C12 convert, C13 substore, C14 rulefetch (72849 rows), repo, rules. yaml.v3 remains only behind `LoadCSafeFirst` (the "CSafeLoader, else safe_load" pattern of `subscription_dialers._load` and `push_diff._load_yaml`): yaml.v3 nodes → pyyaml nodes → same constructor, any failure → PyYAML. Non-string keys: `LoadOptions.KeepNonStringKeys` returns `*yamlx.Mapping` (Pairs with typed keys) with `yamlx.Lookup`/`IsMapping`; without it `*NonStringKeyError` (only where Python's dict cannot be a jsonx.Object). Callers switched: `subs.ExtractSubscriptionHosts` (str(key) rule), `subs.ExtractSubscriptionDialers` (CSafe-first, non-string keys dropped — only string keys are read), `substore.isYAMLResponse`; the editor reload check no longer rewrites ruamel's `!!int '0x-1F'`. `Dump` unchanged (corpus still byte-identical).

Review findings (all fixed; regression tests named):
1. **medium** — yamlx (yaml.v3 based): `1,000` in flow collections, `!!int '0x-1F'`, non-string keys, `name:\tx` accepted, yaml.v3 error texts, `!!binary` (padding stops input, run positions in UnicodeEncodeError), `!!bool maybe` → false (Python KeyError), `!!int ""`/`!!float ""` → **Go panic**, `!!float abc` → 0, `!!int "١٢"`/`"0x 1f"`/`0o17` refused, wrong constructor messages for tags on other node kinds, depth-first error order, timestamp errors in the wrong order and without the timedelta. Fix 8c432221, fa67e510; `TestLoadMatchesPyYAML` (strict), `TestReviewC02References`.
2. **low** — non-string YAML keys in `subs` hosts/dialers and `substore._is_yaml_response` (C10 rows). Fix ad577e64, 0cb26856; `TestReviewC10References` (yamlxDiffers empty), `TestIsYAMLResponseWithNonStringKeys`.
3. **low** — `jsonx.Repr` (C01) and `logx.IsPrintable` decided printability with Go's newer Unicode tables in Repr; the CPython 3.11 table moved to jsonx and both use it (commit 9780c272, which also finishes the table move that cc108f97 picked up from the shared index). `TestReprUsesCPythonPrintableTable`.
4. **low** — new `internal/pystr` (Python 3.11 / Unicode 14 tables: `Lower` with full mapping + Final_Sigma, `IsSpace`, `Decimal`, `Int(s, base)` incl. base 0/prefixes/underscores/4300-digit limit/messages, `Float`); 612 int, 74 float, 443 lower references (`pystr/testdata/ref.json`, `TestMatchesCPython`).
5. **low** — `urlx`: hostname lower-casing used Go tables and a partial case-ignorable/cased approximation (`ⒶΣ.test` → σ instead of ς; Unicode 15/16 capitals lowered); `[fe80::1%a%b]` accepted (ipaddress refuses). Fix 9fa5af44 (`pystr.Lower`, scope-id check); `urlx.TestReviewC02References`. Also: `SafeExceptionDetails` prints `PythonClass()` of the first error in the chain that has one (`TestSafeExceptionDetailsUsesPythonClass`; `yamlx.Error` has it).
6. **low** — `logx.ClientIP`: X-Real-IP was read as UTF-8; WSGI gives latin-1 text (`"\xc3\xa9\x85"` → `Ã©?`). Fix 9fa5af44; `TestClientIPHeaderIsLatin1`.
7. **low** — `jsonresp.PlainTree`: an empty list deeper than 128 counted as plain (Python checks the depth first). Fix 9fa5af44; `TestPlainTreeRefusesDeepEmptyList`.
8. **low** — `localhttp.Do`: URLs went through Go's `url.Parse` (a stray `%` failed the request; mixed escapes kept; query not encoded; host case/port kept; fragment dropped from the response URL). Now `urlx.RequestsPrepareURL` (copy of C13's verified requests/urllib3 port) + `urlsplit`. Fix 79282840; `TestPreparedURLLikeRequests`, requests references in `urlx/testdata/review.json`.
9. **low** (reported by the C15 port) — `taskstatus.Record`: hand-edited counters `"4"` read as 0; Python `int(x or 0)` → 5 (also `" 7 "`, `"０３"`, `"١٢"`, `"1_0"`, floats truncated, big ints kept; `"x"`/`"1.5"`/`[1]`/inf/nan raise and leave the file). Fix d7547ad6; `TestReviewRecordCountersLikePython` (168 Python references).
10. **low** (reported by the C15 port) — `taskstatus.ParseTime`/`FromISOFormat` returned UTC and dropped the offset; now the time is in the parsed zone, named like `timezone.tzname()` (`UTC+08:00`; zero offset and naive → `time.UTC`). Callers re-tested: refresh (+difftest), agents, agents/metrics, api/stats, notifications (their own offset reconstruction keeps working and could now be dropped). Fix d7547ad6; `TestReviewParseTimeKeepsOffset`.

Checks performed: walk of every function/branch of logger.py, json_store.py (text-mode read: invalid UTF-8/BOM → None like jsonx.Parse; write bytes/verify path), fast_yaml.py, local_http.py, url_utils.py (urlsplit/_check_bracketed_netloc/_checknetloc/parse_qsl/urlencode/unquote per ASCII run), task_status.py, resource.py, utils.py (`str(s).lower()` — no non-ASCII letter lowers to t/r/u/e), json_response.py, version.py, internal_call.py; PyYAML 6.0.1 constructor.py read from the test server's venv; new Python references listed above; full test run of every consumer (command below).

Accepted differences (reviewer's judgement; not open findings):
- YAML: values jsonx cannot hold are errors after construction (non-string keys without KeepNonStringKeys, recursive aliases, non-empty `!!set`/`!!omap`/`!!pairs`); e.g. `parse_yaml_object` with an `1:` key gives nil where Python returns a dict with an int key. Ints over 4300 digits from hex/binary YAML load (Python too) but jsonx encoders do not refuse them (json.dumps does). RecursionError depth is a fixed limit (pyyaml 490 nested collections, 490 `=` levels). `LoadCSafeFirst`: yaml.v3 is not libyaml — an empty node tagged `!` is str in libyaml, null here (yaml.v3 drops the tag); only reachable in dialers/push_diff with that exact shape.
- Callers that catch only `yaml.YAMLError` in Python crash (500) on ValueError/KeyError from constructors; Go callers treat every `*yamlx.Error` as a parse failure (classes are available via `IsYAMLError()`/`PythonClass()`).
- taskstatus: a sub-second UTC offset cannot be a `time.Location` offset (instant and zone name exact, wall clock off by the fraction); `Result.Trigger == ""` means the default "schedule" (Python's `trigger=''` would store ''; no Python caller passes '').
- `LoadBytes` (unused) decodes UTF-16 without Python's error texts. `resource` resolves under one root instead of `__file__`/`_MEIPASS` (deployment layout); logx.ClientIP takes the first X-Real-IP header (WSGI servers join duplicates differently).

Notes for other modules (not C02 findings): `pyLower`/`isCased` copies in subs, substore, rules, convert, agents use Go tables (same defect class as finding 5) — switch to `pystr.Lower`; `subs` `pyIntStr`/`repo` `pythonFloat` can switch to `pystr.Int`/`Float` (Unicode-15 digits differ); C14's accepted flow-root non-string-key case and C12's loose loader can now use `KeepNonStringKeys`; `substore.PrepareURL` can switch to `urlx.RequestsPrepareURL`; `substore.ParseProxiesFromYAML` no longer needs to re-compose for the PyYAML class/text.

Commits: 9780c272, f9d7eafd, 8c432221, d7547ad6, fa67e510, ad577e64, 0cb26856, 9fa5af44, 79282840 (+ this PROGRESS update).

Commands: `rt -- bash -lc 'cd server && go vet ./internal/... 2>&1 | head; go test -count=1 ./internal/yamlx/... ./internal/pyyaml/... ./internal/pystr/... ./internal/jsonx/... ./internal/logx/... ./internal/jsonstore/... ./internal/urlx/... ./internal/taskstatus/... ./internal/localhttp/... ./internal/jsonresp/... ./internal/subs/... ./internal/convert/... ./internal/rulefetch/... ./internal/repo/... ./internal/rules/... ./internal/substore/... ./internal/refresh/... ./internal/agents/... ./internal/api/... ./internal/notifications/... ./internal/httpx/...'`. Regenerate references (each with `rt --back <file> -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python <gen>'`): `server/internal/yamlx/testdata/gen_review.py` → `review.jsonl`; `server/internal/pystr/testdata/gen_tables.py` → `../tables.go`, `gen_ref.py` → `ref.json`; `server/internal/urlx/testdata/gen_review.py` → `review.json`; `server/internal/taskstatus/testdata/gen_review.py` → `review.json`.

## C03 — repo storage core (config_repository.py part A)

**Status: `verified`** (review 2026-11-15, one round; 0 open findings) · prereqs: C01, C02 · Go: internal/repo

Files:
- `backend/common/config_repository.py` (1576) — lines 1–1058 and 2087–2604: exceptions, `_now`, `_content_digest`, `_deep_merge`, `_three_way_merge`, `_default_profile_config`, `_items_by_id`, `_id_list`, `_prune_rule_dns`, `_collect_resource_refs`, `_AgentHistoryNotRead`; `ProfileRepository.__init__` … `_write_locked` (open, legacy migration [not ported], corrupt rebuild, cache, router commit guard, document/transaction core); `_scope_legacy_dialers`, `_convert_import`, `_resource_usage`, `_validate_document`, `_validate_profile`

Tests: `test_config_repository.py` (9 defs), `test_config_repository_cache.py` (12), `test_config_storage_migration.py` (41), `test_multi_profile_repository.py` (35), `test_router_repository_safety.py` (17), `test_split_layout_migration.py` (32)

Routes: none.

Evidence: `internal/repo` repository.go, lock.go, merge.go, defaults.go, validate.go, importexport.go, errors.go, files.go, pyvalue.go. By name: `test_config_repository.py` 9/9, `test_config_repository_cache.py` 12/12, `test_multi_profile_repository.py` 35/35, `test_config_storage_migration.py` 19/41 (16 + 3 added by the review; the other 22 are legacy migration / rebuild from config.json, not ported by decision), `test_router_repository_safety.py` 11/17. Python reference corpus `testdata/repo_reference.py` → `repo_reference.json` (`reference_test.go`): 30 three-way merges, 5 deep merges, 48 lock-timeout strings, legacy defaults (plain + factories), 258 `_validate_document` mutations of a full document, 16 `_collect_resource_refs` cases (roots, closures, dialers, chains), 4 `_prune_rule_dns`, 9 `_resource_usage`, 19 `_router_commit_guard` cases (job lock calls, policy router, same device once), 55 `_convert_import` sources incl. schema-2 dialer scoping, with deterministic uuid/time on both sides. Plus the porter's 80-operation cross-check.

Review findings (all fixed):
1. **low** — `LockTimeout` parsed `CONFIGFLOW_LOCK_TIMEOUT_SECONDS` with `strconv.ParseFloat`: full-width/Arabic-Indic digits and `1_0` fell back to 300 s, `0x1p3` gave 8 s (Python: 10 s / 10 s / 300 s). Fix 1c649ea8 (`pythonFloat`: PyFloat_FromString transform + grammar); `TestReferenceLockTimeout`.
2. **low** — `_prune_rule_dns` kept list entries only for string rule ids; Python's set keeps entries equal to non-string ids (`5`, `None`) and raises TypeError for unhashable ones. Fix cae54e1f (`pyHashKey`); `TestReferencePruneRuleDNS`.
3. **low** — `_convert_import`: a `RULE-SET` rule without `value` gets `"value": null` in Python (stored bytes differed); rule names / group ids / smart-group ids are dict/set keys with Python equality; a truthy non-string rule-library name (`sanitize_rule_name` → TypeError) and unhashable names/ids/values (TypeError) were imported by Go. Fix 91f6f296; `TestReferenceConvertImport` (`resources`, `rule configs kept`, `rule name int`, `library name int`, `subscription name dict`, `rule value list`).
4. **low** — `Path.resolve()` semantics: Go's realpath ended a symlink loop at a parity-dependent link and never raised; Python 3.11 raises `RuntimeError("Symlink loop from '<path>'")` (500) from `profile_dir`/`profile_path`/`shared_path`/`_lock`/`__init__`. `DeleteProfile`, the orphan-journal check and rule-cache creation used `lexists` where Python uses `Path.exists()` (coordinator item 4: with `profile_dir` resolving first, the only observable case was a looping profile link — Go renamed it into the tombstone and failed, Python raises before any change; a dangling journal link was renamed by Go, kept by Python). The missing-database log line had Go-specific text; Python's text is back. Fix 614b6d4c; `TestDeleteProfileThroughSymlinkLoopFailsLikePython`, `TestDeleteProfileWithDanglingDirectoryLink`, `TestDanglingJournalLinkIsNotSetAside`, `TestRealpathSymlinkLoopStopsAtFirstRepeatedLink` (expectations from Python runs). The `rule cache` `.bak` check (`shared.go`, part B) got the same `exists` fix.
5. **low** — data dir `~name/...` was not expanded and an undeterminable home directory was used literally; Python's `expanduser()` expands it / raises `RuntimeError("Could not determine home directory.")`. Fix b0f5373b; `TestDataDirExpandUserLikePython`.
6. **test gap** — three non-legacy `test_config_storage_migration.py` cases were counted as legacy: `test_two_threads_on_one_instance_lose_no_update`, `test_write_from_another_process_is_seen` (Python as the other process), `test_cache_follows_the_generation`. Ported in f50db59b (`review_c03_test.go`).
- Also done (plan item): `repo/compat_rules.go` removed, `rules.SanitizeRuleName` / `rules.RuleCacheFilename` used (1c649ea8, 91f6f296); compat golden cases still pass through them.

Coordinator item 4 (edge cases):
- Dangling symlink `lexists` vs `exists` in `DeleteProfile`: now `Path.exists()` (finding 4).
- Non-string Agent name in `DeleteProfile`: Python `'、'.join` raises TypeError (500, nothing deleted); Go returns `ProfileInUse` `配置「…」仍被以下 Agent 使用：5` (409, nothing deleted). Accepted (handled error for a crash); `TestDeleteProfileBoundToAgentWithNonStringName`. In `_validate_document` (`Agent {name} 绑定的配置不存在`) Python formats any name with `str()`, Go matches (`agent name int/none/dict/float/bool/emoji` reference cases).
- Non-object `mosdns`: unreachable in Python — `_validate_profile` refuses `mosdns 参数必须是对象` before the DNS loop; Go has the same order (`mosdns str/none/missing/list with bad dns` reference cases, `TestNonObjectMosdnsIsRefusedBeforeRuleReferences`). Non-object `oxidns` likewise (`oxidns 参数必须是对象`).

Accepted differences (reviewer's judgement, documented; not open findings — overrule if needed):
- Python crashes (TypeError/AttributeError/KeyError → 500) on malformed values become handled errors in Go, e.g. unhashable `agent.profile_id`/`node.subscription_id`/`library_rule_id` → the neighbouring `ProfileValidationError`; non-list `surge.smart_groups`, unknown resource-root kind, missing rule-library `name`, non-list `rules`/`proxy_groups`/`agents` in imports → `*HelperError` (message not Python's). Logged per case by `refOutcome`.
- Where Python's first error depends on set iteration order (hash-randomised per process: several invalid aggregations/nodes in `_collect_resource_refs`, several invalid health-check keys), Go reports the first in document order.
- `_read_entry` retries a failed read under `.config.lock` for every error (Python: only store/repository/Value/TypeErrors); the retry is idempotent.
- Python's read block (`_document()`) also sets the thread-local document, so a write nested in a read raises `ProfileInUse`; Go reads do not mark the context (no repository method writes inside a read).
- `_router_commit_guard` de-duplicates devices by `(repr(type), endpoint)`; Python's tuple equality also merges types `1`/`True`/`1.0` and crashes on an unhashable type.
- Legacy refusal (Go-only by decision): refuses on `config.json`, `config.json.bak`, `system.json(.bak)`, `profiles/*/config.json(.bak)` found with `lexists` (also dangling links, where Python would initialise defaults or refuse with its own message). Before refusing, Go creates only what Python's constructor also creates (`profiles/`, `.config.lock`) and discards stale `.configflow.sqlite3.*` temporaries; legacy files, database, `shared/`, `migrations/` untouched (`TestLegacyFilesWithoutDatabaseAreRefused`). `_import_changed_legacy_locked` (config.json next to a database), `_migrate_raw_cache`, `_rebuild_corrupt_locked` (coordinator decision 1) not ported.
- An `EACCES` on `lstat(configflow.sqlite3)` would be treated as "missing" (Python raises PermissionError); unreachable because `.config.lock` in the same directory was just opened.
- `AgentHistoryNotRead`: typed jsonx getters return zero values instead of raising (documented in system.go).

Checks performed:
- Line-by-line walk of part A vs Go: exception classes and every message, `_items_by_id`/`_id_list`, `_collect_resource_refs` (chain shapes, builtin handling, roots, aggregation closure, raw-dialer walk), `_validate_document`/`_validate_profile` branch order, `_three_way_merge` (list keys incl. bool/float ids, duplicates, reorder rule), `_deep_merge`, `_default_profile_config`/oxidns defaults, `_empty_profile`, `_open_locked` (symlink/non-regular db, orphan journal, earlier copies log, `check()` error mapping, content-validation wrap, startup token write), `_load_entry`/`_read_entry` (cache by head, retry under lock, conflict/store error texts), `_set_cache` size rule, `_write_locked` (nested write, token, validation, guard through COMMIT, cache drop on failure, commit_count only when committed), `_document(write=True)` hook after lock release, `_router_commit_guard`, `_scope_legacy_dialers`, `_convert_import`, `_resource_usage`; lock: `_lock_timeout_seconds` bounds and text, poll/deadline, contended errnos, re-entry waits for itself until timeout, `"0"` written into empty lock files.
- Python reference corpus (above) on CPython 3.11.17; Python probes for delete through a looping/dangling link, dangling journal link, non-string Agent name.
- Cross-language: Go and Python exclude each other on `.config.lock` and time out with the same message both ways (`TestCrossCheckLockExcludesPythonAndGo`); a Python writer killed right before COMMIT leaves the previous data readable and writable by Go (`TestCrossCheckPythonWriterKilledBeforeCommit`); existing fresh-init / 80-operation / interchange cross-checks pass.
- `go test -race` of `internal/repo` (all tests incl. cross-checks); dependent packages `internal/api/...`, `internal/agents/...`, `internal/subs/...`, `internal/rules/...` pass.

Deferred (not open findings): 6 `test_router_repository_safety.py` cases (`test_rule_sync_persists_rollback…`, `test_first_publication_blocks_batch_replacement…`, `test_batch_http_mutations_cannot_abandon_published_policy`, `test_batch_http_mutation_during_first_readback…`, `test_router_settings_allow_credential_repair…`, `test_rotated_router_password_can_be_repaired…`) → routers domain (need router routes/jobs); `test_split_layout_migration.py` and the 22 legacy `test_config_storage_migration.py` cases are not ported by decision.

Commands: `rt -- bash -lc 'cd server && go vet ./internal/repo/... && CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(cd .. && pwd) go test -count=1 ./internal/repo/...'`; regenerate references: `rt --back server/internal/repo/testdata/repo_reference.json -- bash -lc 'CONFIGFLOW_REPO=$(pwd) ~/cf-test/.venv/bin/python server/internal/repo/testdata/repo_reference.py server/internal/repo/testdata/repo_reference.json'`.

## C04 — repo public API (config_repository.py part B), config.py

**Status: `verified`** (review 2026-11-15, one round; 0 open findings) · prereqs: C03 · Go: internal/repo

Files:
- `backend/common/config_repository.py` (1028) — lines 1059–2086: `_profile` … `write_profile_text` (profiles, bindings, publish snapshot, create/clone/import-client/update/delete, transactions, router settings/publication, shared, rule library, node deletion, system/agents, compat config, export/import/reset, shared/profile paths, sanitization tokens, locks, atomic writes)
- `backend/common/config.py` (345)

Tests: `test_editor_transactions.py` (9), `test_profile_agent_bindings.py` (8), `test_profile_clone_router_isolation.py` (2), `test_qa_integrity.py` (12), `test_request_config_snapshots.py` (16), `test_save_publish_guard.py` (18)

Routes: none.

Evidence: `internal/repo` profiles.go, shared.go, system.go, scope.go, hooks.go, files.go, importexport.go, lock.go; `config.py` module functions in scope.go/defaults.go (24/26 defs matched; `_cache`/`_LazyBaseline` are scope internals). `test_request_config_snapshots.py` 16/16, `test_profile_agent_bindings.py` 3/8. Review tests: `review_c04_test.go`, `review_c04_crosscheck_test.go`.

Review findings (all fixed):
1. **low** — `SaveRouterSettings`/`SaveRouterPublication` returned `KeyError 'id'` for any non-string `expected_router['id']`; Python finds no record for a hashable non-string id (`ProfileInUse 路由器设置已变化…` / `路由器连接已变化…`, after the profile lookup) and raises `TypeError unhashable type: 'dict'|'list'` for an unhashable one. Fix 6f2efc98; `TestSaveRouterWithNonStringExpectedID`, cross-check steps 5–10.
2. **low** — `ImportClientProfile`: a non-object `profile` result failed with `'list' object has no attribute 'get'`; Python's `.items()` gives `… 'items'`. Fix 6f2efc98; `TestImportClientProfileNonObjectProfileResult`.
3. **low** — `_read_json` (`ReadProfileJSON`, `ReadSharedJSON`): invalid UTF-8 gave a Go text instead of CPython's `UnicodeDecodeError` (`'utf-8' codec can't decode byte 0xff in position 0: invalid start byte`, `bytes in position 6-7: unexpected end of data`, …), and Python reads in text mode with universal newlines, so JSON error positions after `\r\n`/`\r` differed. Fix 47f3e25e (`pyUTF8DecodeError`, port of CPython's strict decoder; newline translation); `TestReferenceReadJSON` (613 Python reference files: random byte sequences, BOM, CRLF, NaN, duplicates).
4. **low** — `SaveConfigWithPublishGuard` refused a nil config before resolving the profile: an unknown profile gave `ProfileValidationError` (400) instead of `ProfileNotFound` (404). Fix 88d75284; `TestSaveConfigWithPublishGuardResolvesBeforeRefusingNil`, cross-check step `cfg_save_with_guard ghost`.
5. **low** — `ResolveProfile` memo: "no explicit id" was keyed `"\x00"`, so an explicit `"\x00"` after a resolution without id returned the request profile instead of `Invalid profile id`; and the result was stored into the memo re-read after validation, so a commit in between (another request deleting the profile) left a stale id trusted (Python stores into the memo it took, which the next lookup discards). Fix 446d110f (`resolvedKey`, `Scope.remember`); `TestResolveProfileMemoKeepsNoneApartFromExplicitIDs` (Python probe in a Flask request context), `TestResolveProfileResultValidatedBeforeACommitIsNotTrusted`.
6. **low** — `CreateRuleLibraryItem` released the cache lock when the transaction body returned and removed a failed creation's cache without it; Python holds it (ExitStack) through COMMIT, the commit hook and the clean-up. Fix 171cbd21 (`acquireLock`, `withLock` built on it); `TestCreateRuleLibraryItemHoldsCacheLockThroughCommit` (ConfigurationCommitted hook finds the lock held), `TestCreateRuleLibraryItemFailedCommitRemovesCache`.

Accepted differences (reviewer's judgement, documented; not open findings — overrule if needed):
- Python crashes (500) that are handled errors in Go: unhashable group `name`/rule `policy` in `_update_profile_locked` (TypeError; Go goes on to the validation error), non-string usage names in `'、'.join` of `_apply_shared_locked` (Go joins `str()`), non-string `server_domain` in `load_config` (AttributeError at startup; Go leaves it).
- OS-level failures that Python raises as raw `OSError` (500, text only in the log) keep Go's os error text, e.g. `write_shared_json('.')` onto a directory (`IsADirectoryError` vs `*os.LinkError`); `Path.exists()` raising on EACCES is `false` in Go; an existing non-directory `migrations` fails at the rename instead of at `mkdir`.
- `delete_nodes_with_references` updates the changed profiles in first-seen order; Python iterates a hash-randomised set (first error / timestamp order nondeterministic).
- API conventions (PORTING): `""` stands for `None` in `GetConfig`/`SaveConfig`/`UpdateConfigTransaction`/`SafeImportConfig`/`ResolveProfileID`/`CacheDir`/`CreateProfile(cloneFrom)`; client-supplied ids must use `ResolveProfile` (exact). Without a Scope nothing is cached (Python threads keep their ContextVars until `reset_config_context`); background loops use `WithScope` per task.
- Hooks fail closed until their owners register them: `ExportAll(desensitize)` refuses (redaction: C34, routers, C12), `PlanNodeDeletion` errors (C30). The structure `export_all(desensitize=True)` builds around the redaction functions is cross-checked with marker hooks on both sides.
- `_get_initial_config` reads `config_template.json` at runtime; Go embeds a copy (`TestConfigTemplateMatchesBackend`).

Checks performed:
- Line-by-line walk of part B and config.py against Go: every method's argument checks, exception classes and messages (incl. str/repr of ids, `{**result, 'profile': …}` key order, `dict(metadata, revision, is_default, bound_agents)`), transactions (updater under `.config.lock`, `_revision` bumps once per section, no-op writes, nested write refusal, router locked/policy keys, delete-profile tombstone/rollback/guard release order), three-way merges with baselines and the `_revision` check (`True`/`1.0`/`"1"`), create/clone/import-client/update/delete, router settings/publication, shared/profile combined transactions, rule-library creation, node deletion plan wrapper, system/agents (history sentinel order), token normalisation/rotation/retirement, export/import/reset, file helpers (`shared_path`, `profile_path`, atomic writes + `.bak`, 0600 temporaries), config.py scope (snapshot cache, lazy/eager baselines, invalidation by commit count, behaviour in transactions, `reset_config_context` points), `resolve_profile_id` order (explicit, path, `?profile=`, `?profile_id=`, header, fallback, default) and memo.
- Python cross-check `TestCrossCheckC04Operations`: 254 operations on both sides from one seed directory, comparing results/errors (class, message, usages), all SQLite rows after each step and the final layout: router settings/publication incl. published-manifest guards, `import_client_profile` (14 result shapes), `create_profile` with 11 description types, 10 Unicode/edge names (`　`, `\x1c`, `\x85`, `​`, `᠎`), 14 ids at the regex boundary (64/65 chars, full-width, Arabic-Indic digit, `é`, newline, non-strings), `update_profile`, revision conflicts, updater replacements (removed policy groups, ruleset source fields dropped), `import_profile` (wrapper, conflicts, rule-DNS pruning), agents/bindings/tokens, `save_system` merges, file helpers (generated, profile JSON/text, shared JSON/text, escapes, 11 malformed JSON files), rule library, desensitized export structure, `import_all` token retirement, and 40 config.py operations (snapshot edits saved with concurrent edits merged, stale baselines, publish guard, shared/system baselines, combined transactions, resource configs, safe import, reset, reopen). Generator: `testdata/repo_crosscheck_c04_gen.py`.
- Python references `testdata/repo_c04_reference.json`: `_read_json` corpus (613 files), `get_default_config`, `_get_initial_config`, factory `_empty_profile`; Flask request-context probe for the resolve memo.
- `go test -race` of `internal/repo` (incl. both cross-checks); dependent packages `internal/api/...`, `internal/subs/...`, `internal/convert/...`, `internal/httpx/...`, `internal/agents/...` pass except `internal/agents/monitor` (empty `pydecimal_table.go`, another agent's unfinished work).

Deferred (not open findings): route-level tests → C34 (+ agents registry / routers): `test_profile_agent_bindings.py` 5/8 (rebind/delete-error cases), `test_profile_clone_router_isolation.py` 0/2, `test_save_publish_guard.py`, `test_editor_transactions.py`, `test_qa_integrity.py` (whole-app regressions; run when C30–C34 exist). Hooks still default until their owners land: `ConfigurationCommitted` (C35), `PlanNodeDeletion` (C30), redaction hooks (C34, routers, C12), `JobLock` (routers). `repo/compat_*.go` copies to switch: `compat_dialers.go`, `compat_proxystring.go` (C12), `compat_health.go` (C16), `compat_oxidns.go` (C26/C27/C31).

Commands: `rt -- bash -lc 'cd server && go vet ./internal/repo/... && CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(cd .. && pwd) go test -count=1 ./internal/repo/...'`; regenerate: `python3 server/internal/repo/testdata/repo_crosscheck_c04_gen.py server/internal/repo/testdata/repo_crosscheck_c04_ops.json` (local, no Python backend needed) and `rt --back server/internal/repo/testdata/repo_c04_reference.json -- bash -lc 'CONFIGFLOW_REPO=$(pwd) ~/cf-test/.venv/bin/python server/internal/repo/testdata/repo_c04_reference.py server/internal/repo/testdata/repo_c04_reference.json'`.

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

**Status: `verified`** (review 2026-11-16, one round; 0 open findings) · prereqs: C02 · Go: internal/rules, internal/rulefetch/cache.go (C08 functions)

Files:
- `backend/utils/rule_syntax.py` (216)
- `backend/utils/rule_matcher.py` (690)
- `backend/utils/rule_utils.py` (195)

Tests: `test_remote_rule_cache_edges.py` (7), `test_rule_cache_identity.py` (7), `test_rule_matcher_behavior.py` (15), `test_rule_matcher_index.py` (28), `test_rule_syntax.py` (24)

Routes: none.

Evidence: `internal/rules` syntax.go, matcher.go, ruleutils.go, pyre_*.go (Python `re` parser + RE2/regexp2), idna.go. `test_rule_matcher_behavior.py` 15/15, `test_rule_matcher_index.py` 18/28 (missing 10 are memory-packing internals `_PackedSourceLines`/`_SourceLinesBuilder` — Python mechanics), `test_rule_syntax.py` 9/24 + corpus.

Missing / deferred: the repository-backed `rule_utils.py` functions are ported in C14 (`internal/rulefetch/cache.go`); `test_rule_syntax.py`'s 15 ruleset_content cases are ported there (TestRuleSyntaxThroughRulesetContent) and the legacy-cache read side (TestLegacyCacheReadSide). Still deferred: the route cases of `test_rule_cache_identity.py` / `test_remote_rule_cache_edges.py` (→ C32/C33). Line helpers of ruleset_content are exported from `rules/ruleset_lines.go` for rulefetch.

Reviewer notes: Accepted: `\N{name}` and repeat counts > int32 give `*UnsupportedRegexError`; backtracking matches cut off after 1 s count as no match. `rules/compat.go` still holds copies of already-ported helpers: `safeExceptionDetails` returns `exception_type=%T` (Go type name, e.g. `*fs.PathError`) while `urlx.SafeExceptionDetails` exists — switch and check the log text; `Logger` interface instead of `logx`; `ProfileValidationError` instead of `repo.Error` (rules cannot import repo? it can — rules imports only jsonx; check for a cycle via repo→rules first). Remaining compat copies → C14 (ruleset_content), C12 (converters/common), C26 (device policy).

Review C08 (2026-11-16, one round) — findings (all fixed, each with a regression test):
1. **high** — YAML rule sets in the matcher (`_iter_ruleset_lines`): gopkg.in/yaml.v3 plus a hand-written resolver, and the displayed source taken from the start line only. Python composes with CSafeLoader (libyaml) and slices from the start mark's line to the line holding the *end* mark. Diverged on `%YAML 1.2` documents, `:`/`?` inside flow plain scalars, tabs inside plain scalars and after tokens/tags/directives/block indicators, a comment right after a block scalar header or `%YAML` version, byte order marks (stream start: marks exclude it; any line start: skipped), surrogate escapes (`"\ud83d"` is a libyaml error), the empty `! ` node (str), multi-line scalars (source spans to the end line). Fix a06e7bab + the final review commit (`pyyaml.ComposeOptions.Libyaml`: libyaml's scanner rules in the PyYAML port) + fd9a48dc (matcher on it, Python's rfind/find over end marks). Tests: `TestReviewC08YAMLPayload` (80000 fuzzed documents vs CSafeLoader), `TestReviewC08Matcher`.
2. **medium** — `ip_address`/`ip_network` were net/netip: any white space refused, so scope ids Python keeps verbatim (`fe80::1%eth 0`, `::%a `, `x\n`) made parse_rule_line return DOMAIN instead of IP/IP-CIDR, cidr_error the wrong message, normalize_match_query strip the dot, matching fail. Fix 17bb4d03 (port of CPython 3.11 `_ip_int_from_string`/`_split_scope_id`). Tests: `TestReviewC08IP`, `TestReviewC08Lines`, `TestReviewC08Domains`.
3. **medium** — regex group names (`(?P<n>`, `(?P=n)`, `(?(n)`, `\g<n>`) used a letter/digit approximation of `str.isidentifier`: `℘`, `a·`, `ⅷ`, `x\u0301`… valid in Python, rejected in Go (and the reverse for some letters), with different error texts. Fix c19aef3f (`pystr.IsIdentifier`, XID tables from CPython) + fd9a48dc. Tests: `TestReviewC08Regex`, `TestReviewC08Case` (every code point).
4. **low** — `str.upper()` (split_rule_payload, parse_rule_line, `_parse_ruleset_line`) was Go's simple mapping plus 10 special cases: `ΐ`, `ᾳ`, `և`… and Unicode 15+ letters gave a different kind in `未知规则类型 …`. Fix c19aef3f (`pystr.Upper`, full mapping) + fd9a48dc. Tests: split records, `TestReviewC08Case`.
5. **low** — regex error texts quoted with a minimal repr; Python escapes non-printables (`\u200b`, `\u2028`, `\u3000`). Fix fd9a48dc (`jsonx.Repr`). Test: rx records.
6. **low** — the matcher's log lines were dropped without an injected logger, and `exception_type` was the Go type; Python logs under `backend.utils.rule_matcher` with the Python class. The matcher cache now defaults to `backend.utils.domain_matcher_cache` and lets its matchers log under rule_matcher. Fix 44028412 (`urlx.SafeExceptionDetails`, logx). Test: `TestReviewC08MatcherLogLines`.
7. **low** — `rulefetch.RuleCacheReadPath` read the shared document before checking the current cache file; Python returns an existing file first (a repository failure does not matter then). Fix a0fe241c. Test: `rulefetch/TestReviewC08ReadPathPrefersExistingCacheWithoutRepositoryRead`.

Coordinator item 8: done for `rules` — the private `pyLower`/`pyUpper`/`pyStrip`/`pyIsSpace`/`pyIsIdentifier` copies are gone; call sites use `internal/pystr` (Upper and IsIdentifier added there, generated by `pystr/testdata/gen_tables.py`). All corpora re-run.

Fuzzing (testdata/review_c08_gen.py → review_c08.jsonl.gz, CPython 3.11.17, synthetic values): regex grammar fuzzer (groups, named groups/refs, conditionals, look-around, atomic, possessive, scoped/global flags incl. a/u/L/x, classes with categories and Unicode case pairs, quantifiers incl. invalid ones, stray metacharacters) — 6000 random + 5000 Python-valid patterns (13120 compiled pattern/flag pairs: 8342 on RE2, 4778 on the backtracking engine; 124952 searches with spans) × IGNORECASE on/off, error text or search span + every group span on 8–10 subjects each, plus rule_value_error('DOMAIN-REGEX') for each; 4000+ rule_value_error on mutated kinds (case, typos, ı/ſ/K, SpecialCasing letters) and values (domains with IDN/full-width/bidi/ZW chars, IPs with zones, ports/DSCP/UID ranges, logical rules, regexes); 1000 split_rule_payload with Unicode spaces; 3000 each of parse_rule_line (all dialects, quotes, comments, no-resolve variants), ip_address/ip_network/cidr_error, domain_value_error/is_valid_domain/normalize_match_query, match_query/match_domain; 800 RuleConfigMatcher cases (text and YAML bodies); 80000 YAML token-soup documents (5909 with entries) through `_iter_ruleset_lines`; str.upper/lower/isidentifier of all code points. All equal.

Accepted differences (not open findings):
- Python exceptions handled in Go: OverflowError for repeat counts ≥ MAXREPEAT escapes `_regex_error`, `match_domain` and the matcher index in Python (HTTP 500), Go reports `正则表达式格式无效：…` / no match; RecursionError depth is fixed at 490 nested groups (Python's depends on the call stack); non-dict rule items, non-string `rule_type`/`value` (Python's `ip_network(5)` would even accept an int value), unhashable behaviors — values every write path validates as strings.
- From C09: Go's Unicode tables (newer than 14.0) for `\w`/`\d`/case folding on code points unassigned in Python 3.11; backtracking timeout (1 s, no match); `\N{name}`.
- libyaml emulation covers the differences the fuzzing found; nesting deeper than the PyYAML port's limit (490) yields nothing where libyaml (no Python recursion) might parse.
- `rule_cache_read_path` joins with filepath.Join (cleaned path text, same file); `refresh_remote_rule` with a non-string url passes its str() (Python passes the object; validated urls only).

Checks: line-by-line walk of rule_syntax.py, rule_matcher.py (incl. `_RuleIndex` lookup order, IP index by prefix, behaviors, key/slot, logs), rule_utils.py vs rules/syntax.go, matcher.go, ipaddr.go, difflib.go, ruleutils.go and rulefetch/cache.go; regex engine selection (RE2 only without look-around/refs/atomic/possessive/conditionals/`\b`, `$` only at the tail), category classes, flags, `$`/`\Z`/`\b`/`\B` translations; existing corpora (27000 match pairs, regex, idna, syntax, cache names) and C09/C14 references pass.
Not gofmt-clean but not C08's: `rulefetch/content.go`, `rulefetch/content_test.go` (C14).

Commands: `rt -- bash -lc 'cd server && go vet ./internal/rules/... ./internal/rulefetch/... && go test -count=1 ./internal/rules/... ./internal/rulefetch/... ./internal/convert/... ./internal/subs/... ./internal/repo/...'`; regenerate: `rt --back server/internal/rules/testdata/review_c08.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python -W ignore server/internal/rules/testdata/review_c08_gen.py server/internal/rules/testdata/review_c08.jsonl.gz 6000 80000 5000'`; pystr tables: `rt --back server/internal/pystr/tables.go -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/pystr/testdata/gen_tables.py'`.

## C09 — matcher cache, group candidates, reorder, strategy refs, connection rules

**Status: `verified`** (review 2026-11-15, one round; 0 open findings) · prereqs: C08 · Go: internal/rules

Files:
- `backend/utils/domain_matcher_cache.py` (237)
- `backend/utils/group_candidates.py` (303)
- `backend/utils/reorder.py` (57)
- `backend/utils/strategy_references.py` (27)
- `backend/utils/connection_rules.py` (84)

Tests: `test_connection_ip_rules.py` (8), `test_domain_matcher_cache.py` (17)

Routes: none.

Evidence: `internal/rules` matchercache.go, groups.go, reorder.go, strategy.go, connection.go. All defs matched (`apply_ip`→`ApplyIPRule` = the body of its `append(shared)` closure, `_destination_network`→`ParseDestination`, `clear_cache`→`MatcherCache.Clear`, `_cleanup_idle`/`_evict_idle_locked`/`_schedule_cleanup_locked`/`_signature`/`_LocalSource`/`_CachedMatcher` → same-named private Go code). `test_domain_matcher_cache.py` 17/17 (the review ported `test_replaced_file_during_lazy_read_is_retried_on_same_handle` with a test hook between the handle's last fstat and the path stat), `test_connection_ip_rules.py` 5/8 + the library-level part of 2 more through the reference corpus (below).

Review findings (all fixed):
1. **medium** — IGNORECASE (engine shared with C08; hit by every `(?i)` group/subscription filter and by DOMAIN-REGEX, which Python compiles with `re.IGNORECASE`): Go used RE2's/regexp2's Unicode case folding; sre matches a cased literal by simple lowercase (`_sre.unicode_tolower`) plus `re._casefix._EXTRA_CASES` and tests sets with the lowercased character. `(?i)istanbul` did not match `İstanbul`, `(?i)ı`/`(?i)İ` matched only themselves, `(?ai)k` matched the Kelvin sign. Fix 21fe760e: `pyre_icase.go` rewrites literals and sets under IGNORECASE into explicit classes after parsing (tables generated from CPython 3.11.17: `pyre_case_table.go` by `testdata/gen_case.py`; fast exact construction, brute force only as fallback), `flagLetters` no longer emits `i`, back references keep the flag. Regression: `TestICaseLiteralsLikePython` (every cased code point as a literal, Unicode and ASCII mode, both engines), `TestICaseSetsLikePython` (57 sets incl. negation, categories, non-BMP ranges, scoped flags, every code point, both engines), `TestICaseSetFoldIsCheap`.
2. **low** — `ApplyIPRule`: `subnet_of` ignored IPv6 scope ids; Python's `IPv6Address.__eq__` compares them, so `fe80::/64` is not covered by an existing `IP-CIDR6,fe80::%eth0/64` and is appended. Fix 661e5a7b (`ipNet.subnetOf`); `TestSubnetOfHonoursScopeIDLikePython`, apply references.
3. **low** — `FilterCandidates` refused patterns only Python can run (`\N{name}`, repeat counts > int32) with a Go-only message ("…环视、反向引用等…", also stale since look-around runs on regexp2); the domain decision is "not applied, accepted like Python". Fix 661e5a7b; `TestFilterCandidatesUnsupportedPatternIsNotApplied`.
4. **test gap** — `test_replaced_file_during_lazy_read_is_retried_on_same_handle` ported (86157174); concurrent stress test of handles/edits/eviction/Clear/file replacement under `-race` (b3362482); gofmt of the generated table and the icase test's time limit under `-race` (e1f9e123, ef2e9337).

New Python references (`testdata/review_c09_gen.py` → `review_c09.json.gz`, `review_c09_test.go`, `review_c09_apply_test.go`): `filter_candidates` 300 cases (İ/ı/ß/ﬃ/Kelvin/ſ/Ω, full-width and Arabic-Indic digits, CJK, emoji, 300-char names, look-around, back references, `\b`, `(?a)`/`(?u)`/`(?x)`, invalid patterns, non-string values, `　`/U+2028 strip), `validate_filters` (types incl. `1`/`1.0`/`True` legacy values, invalid exclude patterns), `lower_group_provider_filters` with 22 odd provider names (quotes, backslash, NUL, DEL, U+3000, ZWSP, bidi, astral, unassigned — the clone path digest is `sha256(repr(key))`), `resolve_new_order` (NFC/NFD, full-width ids, None ids, raw entries, positions), `validate_rule_policies` (all formats, non-string ids/policies), `_destination_network` (54 values × 4 rule types: whitespace, U+3000, `/024`, netmask forms, mapped IPv6 prefixes, multicast/unspecified, zones, non-strings) and `apply_ip` with the repository/discovery stubbed against `ApplyIPRule` + the real `rulefetch` payload port (2 behaviours × 32 contents incl. the YAML payloads of `test_yaml_payload_append_remains_valid_and_keeps_comments` × 6 values × 4 policies).

Checks performed:
- Line-by-line walk of all five files: matcher cache (signature inputs and snapshot, `_read_key`/`_library_key`, path-change retry, stamp race rules, FileNotFoundError vs other OSError/UnicodeError/validation errors, inline fallback, retry timer, host result LRU and deep copies, registry LRU/eviction/cleanup timer generation, `active`/`last_used`), group candidates (every branch of `filter_candidates`, `managed_members`, `managed_providers`, `render_members`, `validate_filters`, `_embedded`, `_literal_backticks`, `exclusion_filter`, `lower_group_provider_filters` incl. `sources.get(None)` for records without `follow_group`, `validate_candidates`, `merge_group_update` key order), reorder, strategy references (messages via `str()`), connection rules (`ip_rule_targets` `active` = raw `enabled` value, target rules, `normalize_client_range` copy, canonical CIDR text, warnings also for skipped).
- Existing corpora still pass (27000 match pairs, 16128 regex searches, 1498 validate_candidates, lower cases); dependent packages `subs`, `convert`, `rulefetch`, `repo`, `api/logs` pass after the IGNORECASE change.

Accepted differences (reviewer's judgement, documented; not open findings — overrule if needed):
- Python crashes (500) that are handled in Go: non-dict list entries, unhashable ids/policies/names (TypeError), missing `name`/`id` keys (KeyError), non-string node names in `filter_candidates`, RecursionError/OverflowError from compiling a filter (Go: `正则表达式无效`).
- Dict/set keys for library ids, group ids and reorder ids are `jsonx.Repr` strings, not Python hash equality (`1`/`1.0`/`True`); unreachable: stored ids are validated non-empty strings and reorder ids must be strings. Likewise an empty-dict library entry (`source or item`) cannot be referenced by a stored rule set.
- `LocalMatcher`: a rules directory that does not exist is made absolute without resolving symlinks in its existing prefix (`os.path.realpath` would); only the slot key/paths change, not results.
- `MatcherCacheOptions.StoredLibrary` is called eagerly and cannot fail; Python reads the repository only when the new cache name is missing, and a repository error there would propagate (empty index + error log, or an exception from `local_matcher`).
- Unicode database: Go's tables are newer than Python 3.11's Unicode 14.0, so characters assigned later are letters/digits for `\w`/`\d` (and cased if new case pairs exist) in Go; the references skip Python-unassigned code points.
- The cache clock is process-relative (`time.monotonic` is boot-relative); only differences are used.

Deferred (not open findings) → **C37** (the only caller, `domain_discovery_service.py`, and the agent route): wire one process-wide `MatcherCache` with `RulesDir`, `StoredLibrary` (shared `rule_library`), **`ValidateRemote` = `rulefetch.ValidateRemoteRuleContent(content, rule, nil, "")` (nil accepts every cache file, Python always validates url sources)**, `Clear` on shutdown; port `apply_ip`'s orchestration around `ApplyIPRule` (`before_write`, `get_compat_config(profile_id)`, `update_shared_config_transaction`, `save_rule_to_local` for `Result.Updated`, warnings) and the route-level remainder of `test_single_address_append_preserves_content_and_cache` (cache file, profile unchanged, no discovery history) and `test_under_lock_changes_cannot_redirect_ip_writes`.

Commands: `rt -- bash -lc 'cd server && go vet ./internal/rules/... && go test -count=1 ./internal/rules/...'`; regenerate: `rt --back server/internal/rules/testdata/review_c09.json.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rules/testdata/review_c09_gen.py server/internal/rules/testdata/review_c09.json.gz'`, `rt --back server/internal/rules/pyre_case_table.go --back server/internal/rules/testdata/icase_reference.json.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rules/testdata/gen_case.py server/internal/rules/pyre_case_table.go server/internal/rules/testdata/icase_reference.json.gz'`.

## C10 — manual node parser, subscription hosts/filter/dialers

**Status: `verified`** (review 2026-11-15, one round; 0 open findings — the remaining YAML differences belong to `internal/yamlx`, see below) · prereqs: C08 · Go: internal/subs

Files:
- `backend/utils/subscription_parser.py` (428)
- `backend/utils/subscription_hosts.py` (465)
- `backend/utils/subscription_filter.py` (50)
- `backend/utils/subscription_dialers.py` (299)

Tests: `test_node_library_subscription_filter.py` (3), `test_subscription_dialers.py` (12), `test_subscription_hosts.py` (22), `test_subscription_regex_filter.py` (4)

Routes: none.

Evidence: `internal/subs` parser.go, hosts.go, filter.go, dialers.go, pyconv.go (+ corpora parser/hosts/filter/dialers generated from Python by `testdata/gen.py`). Pure-function cases of `test_subscription_hosts.py` (8/22), `test_subscription_dialers.py` (2/12), `test_subscription_regex_filter.py` (2/4) in `ported_test.go`. `subs/compat.go` = dialer_references copy (stays: cycle with `internal/convert`; `compat_drift_test.go` checks it).

Review findings (all fixed, e88c0698; regression tests in `review_c10_test.go`):
1. **low** — `int()` of a port: `digitValue` limited the walk back to ten code points, so digits after the first run of a contiguous Nd block (mathematical digits U+1D7D8…U+1D7FF) had value 0 (`trojan://…:𝟙𝟙` → port 0; Python 11). Reference: every Unicode decimal digit as a port; `TestPyIntStrAndMappedAddresses`.
2. **low** — `_address` (hosts values/keys, also used by C11's proxy-server DNS): CPython 3.11.17 judges IPv4-mapped addresses by their IPv4 part, so `::ffff:224.0.0.1` (multicast) and `::ffff:0.0.0.0` (unspecified) are refused; Go kept them. Reference rows + `TestPyIntStrAndMappedAddresses`.
3. **low** — `AdaptSubscriptionProxies`: a proxy without `name` added `"None"` (and a numeric name `"5"`) to the defined names, so a dialer target literally named `None`/`5` was kept; Python's set holds `None`/`5`, which never equal a string target (node dropped). `TestAdaptSubscriptionProxiesIgnoresNonStringNames` (Python probe).
4. **low** — hosts used-domain filters: a provider-query or aggregation pattern only Python can run (`\N{name}`) dropped the whole query filter / skipped the aggregation; per the domain decision it is now just not applied (a pattern Python rejects still behaves as in Python). `TestQueryFilterUnsupportedPatternIsNotApplied`.

New Python references (`testdata/review_c10_gen.py` → `review_c10.jsonl.gz`, 1905 rows, `TestReviewC10References`): all 660 Unicode decimal digits as port digits; 29 odd port forms (sign, `0443`, underscores, U+3000/\x1c/U+2028/NBSP/NEL around, 4300/4301 digits, `0x1bb`, `4.43e2`, full-width, mathematical, superscripts) × ss/trojan/vless/hysteria2; 14 names (empty, CJK, emoji/ZWJ, İ, percent-encoding kept raw, `#` in fragment, 500 chars, NUL, RTL); standard/URL-safe × padded/unpadded base64 for ss user info, whole ss, ss with inner fragment, ssr (remarks/protoparam/obfsparam/empty values) and vmess; vmess value types (float/bool/str/None/list/1e400/NaN ports and aids, non-object JSON, BOM, duplicate keys, invalid UTF-8, trailing NUL, bad padding); 27 query strings × vless/trojan/hysteria/hysteria2 (repeated keys, `+`/`%2B`/`%zz`/`%ff`, `;`, blank values, case); IPv6 literals and zones, IDN hosts; 8 representative links and JSON/YAML objects truncated at every position; 90 YAML/JSON documents (duplicate keys, YAML 1.1 ints/floats/bools/sexagesimal/timestamps, anchors/merge keys, tags, multi-document, CRLF, NaN/big numbers) through `parse_uri_list` and `parse_yaml_object`; 36 hosts bodies (duplicate and non-string keys, Kelvin K/İ/ß domains, mapped/zoned/upper-case IPv6 values, lists with duplicates and domains, CRLF, tab indentation, quoted keys, flow mappings, comments/blank lines inside the block, 1005 entries, 25 wildcards, label length limits, 300-char key, NEL/U+2028 line breaks, timestamps/binary values); 17 dialer bodies (duplicate groups/proxies, cycles, settings type filter, non-list/str members, non-iterable `proxies`/`proxy-groups`, 60 targets, 120 members, unparsable bodies).

Checks performed:
- Line-by-line walk of all four files: dispatch order and bare `except` → skip semantics of `parse_uri_list`, every scheme parser (split order, `@`/`#`/`?` handling, `rsplit` for vless, defaults and their types, `params` key order, `parse_qs` semantics incl. blank values and `errors='replace'`), `base64.b64decode` (non-alphabet skipping, padding), `int()` of str/float/bool; hosts (`_domain`, `_address`, `_normalize_value`, `_hosts_block` incl. `splitlines` separators, `extract_subscription_hosts`, digest/describe, index building, `_matches`, `_provider_sources` incl. `urlsplit`, `_query_filter`, `_used_domains`, `_creates_cycle`, merge report/order/conflict text, `log_report` digest and eviction); filter (messages, `str(name)`); dialers (`extract_subscription_dialers`, `dialer_record`, `_name_pattern` = `re.escape`, `add_dialer_groups` incl. partial state on a claim conflict, `adapt_subscription_proxies`).
- Existing corpora (parser 3700+ rows, filter, hosts/merge, dialers, chains) still pass; `go test -race` of rules/subs/convert/rulefetch/repo.

Accepted differences (reviewer's judgement, documented; not open findings — overrule if needed):
- **YAML**: the six rows the C10 review attributed to yamlx (non-string keys, tab before a value) were fixed by the C02 review (8c432221, ad577e64: yamlx on the PyYAML port, `KeepNonStringKeys` in hosts/dialers); `yamlxDiffers` is empty and all 1905 rows are compared except 35 whose Python result JSON cannot carry (datetime, bytes, set).
- Python crashes (exceptions escaping to 500) that are handled in Go: non-YAMLError exceptions from PyYAML constructors (invalid dates), RecursionError in deep dialer group nesting, `int()` of a malformed stored `total`, `item['name']` KeyErrors, a non-string truthy `regex_filter`, malformed bracketed provider URLs (`urlsplit` ValueError).
- `subscription_hosts_index`'s file-stamp memo lives with the cache package (C13); `NewHostsIndex(cache).ForURL(sub)` is the computation.
- Ids are compared as strings (`objectsByID`); non-string ids are unreachable in validated documents.

Deferred (not open findings): Not ported (no caller): `subscription_parser.parse_subscription`, `parse_clash_yaml`. The remaining `test_subscription_hosts.py` cases (scheduled download, provider endpoint, preview route, agent content invalidation → C15/C17/C30/agents), `test_subscription_dialers.py` 10 cases (generation/delivery → C17/C18), `test_subscription_regex_filter.py` 2 (provider + save route → C17/C30), `test_node_library_subscription_filter.py` 0/3 (preview routes → C30/C33).

Commands: `rt -- bash -lc 'cd server && go vet ./internal/subs/... && go test -count=1 ./internal/subs/...'`; regenerate: `rt --back server/internal/subs/testdata/review_c10.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/subs/testdata/review_c10_gen.py server/internal/subs/testdata/review_c10.jsonl.gz'`.

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

**Status: `verified`** (review 2026-11-16, one round; 0 open findings — commits a2ea3673, e5fc9df7, 64a981fa) · prereqs: C09, C11 · Go: `internal/convert`

Files → Go:
- `backend/converters/common.py` (118) → `convert/common.go` (`UDPFallback`, `NTPDirect`, `EmittedRules`, `NTPDirectRule`, `AfterMatchComment`, `NetworkAsProtocol`, `HasPortList`, `SetGeneralOption`, `RulesetFormat`, `ParseHostsPairs`); `compat.go` = `_has_yaml_payload` via `rules.HasYAMLPayload`
- `backend/converters/mihomo.py` node group → `convert/mihomo_nodes.go`: `IndentDumperOptions`, `ApplyGithubProxyDomain`, `MihomoUsedPorts`, `MihomoListenPorts` (+ `pySet`, CPython set order), proxy YAML LRU cache (`loadProxyYAML`, `stringKeys`), `ParseStructuredProxyString`, `InspectStructuredProxyString`, `WithNodeConversions` + `ConversionResults`, `ConvertNodeToMihomo`; region-probe constants
- `backend/utils/proxy_utils.py` → `FixProxyFields`
- `backend/utils/dialer_references.py` → `convert/dialers.go`, `convert/dialers_api.go` (`ValidateShapes`, `ValidateDialers`, `DependencyGraph`, `GroupMembers`, `GroupDependencies`, `RawDialer`, `DialerTarget`, `CheckCycles`, `IncomingDialers`, `ResolveDialer`, `OverlayDialer`, `FilterProviderCandidates`, `ProviderProxyName`, `ValidateEmitted`, `RejectSurgeDialers`, `DependencyNodeIDs`)
- `backend/common/mihomo_easytier.py` → `convert/easytier.go` (+ `ipaddr.go`)
- `convert/proxyyaml_loose.go`: yaml.v3 loader, now only the fallback of `InspectStructuredProxyString` for a document with a recursive alias (see accepted differences)

Tests: `ported_test.go` (porter: `test_proxy_yaml_cache.py` 14/14, `test_mihomo_easytier.py` 11/13, `test_dialer_provider_scope.py` 3/4, `test_dialer_round2.py` 2/10, `test_dialer_review_fixes.py` 1/16, `test_node_conversion_cache.py` 1/12 + log lines; deferred cases listed with C17/C18/C19/C20/C30/C33/C34), `corpus_test.go` (37577 Python rows), `review_c12_test.go` (2130 review rows + exception messages of the porter corpus).

Review findings (all fixed; regression rows in `testdata/review_c12.jsonl.gz` → `TestReviewC12References`, unless noted):
1. **medium** — `MihomoListenPorts` listed listener ports ascending; Python's dict follows the CPython set order of `mihomo_used_ports` (setup_flow names the *first* conflicting port). Now `pySet` reproduces setobject.c (hashes of int/bool/float, linear probes, perturbation, resizes, `set_merge`): 1191 of 1200 rows (600 random documents × used/listen ports) give Python's exact order; the rest are the accepted None-slot cases below. a2ea3673.
2. **medium** — `port: true` became port `1` in `MihomoListenPorts`; Python keeps `True` (setup message `使用端口 True`). `PortOwner.Value` holds the key as Python does (first key object kept). a2ea3673.
3. **low** — `has_port_list`/`network_as_protocol` ran on RE2: Python's `re.IGNORECASE` also matches `ı` for I and `ſ` for S (`(ıN-PORT,80/443)` is a port list in Python). Now `rules.CompilePyRegex` (+ `Sub` for findall); `str.upper` used a partial SpecialCasing table (`ᾳ`, `ᾀ`, `ﬓ` wrong) → `pystr.Upper`. a2ea3673.
4. **low** — external-controller port: `isdigit`/`int()` on Go's Unicode 15 tables accepted Kawi/Nag Mundari digits that Python 3.11 refuses; now `pystr.Decimal` + `pystr.Int` (also the 4300-digit limit). a2ea3673.
5. **low** — `convert_node_to_mihomo` structured `params` lists: Python's `dict.update` takes any length-2 sequence (`"ab"`, a two-key dict) and keys that are not strings; Go raised TypeError. a2ea3673.
6. **low** — `group_dependencies` `sorted(ids)` sorted non-string ids as `"\x00"+repr` text (`10` before `9`) and never raised for mixed str/int; now numeric order and Python's TypeError. The `use` match tuple is hashed only when added, as in Python. a2ea3673.
7. **low** — `validate_proxy_chains`: the unhashable-`follow_group` TypeError was raised before every other check; Python raises it in its first loop over groups-by-id (an earlier group's DialerReferenceError wins; a group replaced by a later duplicate id is never checked). a2ea3673.
8. **low** — `filter_provider_candidates`/`provider_proxy_name`: OverflowError/RecursionError of `re.compile` became DialerReferenceError; `re.sub` compiles before it checks the name type; a non-string name with no rules returned `""` instead of TypeError; `rules[0]` of a dict is KeyError(0); `\d` in the `$n` expansion used Unicode 15 digits. a2ea3673.
9. **low** — `resolve_dialer` read `ref['id']` before iterating (KeyError instead of StopIteration for an empty list); `reject_surge_dialers` and `validate_emitted` validated whole lists up front where Python's `any()`/loops stop early — a later malformed group/node/provider definition/payload entry raised AttributeError/TypeError before Python's DialerReferenceError (this is coordinator item 10's C17 row; its allowlist entry in `provider/corpus_test.go` is removed and the row now matches). a2ea3673.
10. **low** — crash messages: `obj['k']` on a str/list said `'str' object is not subscriptable`; Python says `string indices must be integers, not 'str'` / `list indices must be integers or slices, not str`. Now every unintended exception of both corpora carries Python's text (`TestCorpusExceptionMessages`). e5fc9df7.
11. **low** — YAML node strings with keys that are not strings (`1: a`, `yes: x`, `~: y`) were not parsed locally (`ParseStructuredProxyString` nil → Sub-Store; `health` `needs_sub_store` true); Python parses them. Now `yamlx` `KeepNonStringKeys` (coordinator item 8) and the keys are stored as json.dumps writes them (accepted difference below); such results bypass the cache as in Python. The ported cache test helper bypassed the real loader; fixed (64a981fa).

Coordinator items: **8** — `convert` uses `pystr` (`Lower`, `Upper`, `Strip`, `IsSpace`, `Decimal`, `Int`) and `rules.CompilePyRegex`; the private `pyLower`/`pyIsSpace`/`decimalValue`/Go-table helpers are gone; the loose YAML loader is replaced by `yamlx` `KeepNonStringKeys` except for documents with a recursive alias (no `yamlx` counterpart; deleting it needs a yamlx option such as a recursion placeholder — owner C02). The private `urlsplit` copy stays until the bracketed-host fix of `urlx` that is uncommitted in the shared tree lands (`urlx.Split` at HEAD reports netip's text for `https://[github.com]/`; with the fix, switch `pyhelpers.urlsplit` to it). **10** — `ValidateEmitted` fixed (finding 9); C17's allowlist row removed.

Accepted differences (reviewer's judgement, documented; overrule if needed):
- **None in the port set**: `hash(None)` is the address of `None`, so its slot in `mihomo_used_ports`' intermediate set depends on the interpreter binary (reference build `…dd`, python:3.11-slim `…36`; the ASLR-randomised bits decide its probe sequence after a collision). `pySet` counts None (and str members, whose hash is randomised per process) for resizes but gives them no slot; the order differs from a given Python build only when None displaced an int (9 of 1200 random-document rows on the reference build; same members).
- **Keys that are not strings** in proxy YAML / `params` pairs are stored as json.dumps text (`1` → `"1"`, `true` → `"true"`, `null` → `"null"`): jsonx objects have string keys. Generated YAML writes `'1': a` where Python writes `1: a`; keys that only differ by type (`1` and `"1"`) collapse.
- **Recursive aliases and non-empty `!!set`/`!!omap`/`!!pairs`** in a node string: not parsed locally (`ParseStructuredProxyString` nil → Sub-Store), the domain's yamlx/jsonx limit. `InspectStructuredProxyString` (raw_dialer, provider delivery, repo import rewrite) reads recursive documents with the loose loader, as before.
- **Regex**: `\N{name}` and repeat counts beyond int32 in provider filters/name rules are refused with the Provider message (`…无效或不受支持`); a 1 s backtracking timeout in a name rule is `Provider 名称转换规则无效` (domain regex decision).
- **Ids that are not strings** live in `Ref.ID` as `"\x00"+repr`; not observable (graph membership/cycles, `incoming_dialers` only for truthiness, `dependency_node_ids` against string node ids); `Ref.IDValue()` turns them back (a bool id comes back as 1/0).
- **Proxy YAML cache budget** approximates `sys.getsizeof`; it only decides whether a result is cached (results are equal either way).
- Python crashes on values the code does not check are `*PyError` with Python's class and, for every corpus row, Python's message; other crash texts may differ.

Checks performed:
- Line-by-line walk of all five Python files against Go: every branch, short-circuit order of `any()`/generators/comprehensions, dict/set hashing points, exception classes and messages, Chinese messages, defaults and key order (common.py, node group of mihomo.py incl. the YAML cache and `node_conversions`, proxy_utils, dialer_references incl. `validate_proxy_chains`' interaction, mihomo_easytier incl. `ipaddress` rules for networks/netmasks/hostmasks/interfaces/scope ids).
- New Python references (`testdata/review_c12_gen.py`, 2130 rows): IGNORECASE ı/İ/ſ/K, Unicode spaces (U+2028, U+3000, \x1c–\x1f, U+180E, U+200B) and SpecialCasing in common.py; hosts pairs with zones, mapped/embedded IPv4, full-width/mathematical digits; `ruleset_format` URLs (İ, Kelvin, NFKC netloc, brackets); GitHub proxy hosts (İ, trailing dot, userinfo, brackets, ℀); port documents (bools, floats, 10**30, full-width/Arabic-Indic/Kawi/circled digits, 600 random documents for order); 76 node strings (non-string keys, recursion, !!set/!!binary/timestamps, aliases into reality-opts, YAML 1.1 ints/floats, JSON NaN/1e400/duplicates/surrogates, URIs vs `key: value`, BOM, NEL/U+2028, multi-document, tabs, merge keys, tags) through parse/convert/raw_dialer; 32 structured nodes (`İPSEC`, Kelvin, ẞ, Σ, every `params` shape); 17 dialer profiles (3000-deep include chain, 500-group cycle, self-include, mixed/numeric/str id sorting, unhashable follow_group order, raw dialer names/YAML/recursion, Unicode names, aggregation ids, unhashable `use` ids); lazy `resolve_dialer`/`reject_surge_dialers`/`validate_emitted`; filter/provider-name edge patterns and targets; 97 EasyTier settings (code point limits with emoji, every netmask form, IPv6/zone, mtu/routing-mark bounds and types, scheme case/Kelvin, URL whitespace, TLD dots) and 23 `apply_easytier` documents (core versions incl. full-width, non-list proxies/groups/rules, nameserver-policy shapes, dialer targets). Reference generated twice with different PYTHONHASHSEED: only the TypeError text of a mixed-type sort varies (class compared).
- Sub-Store converter end to end: `substore` `TestRenderingAgainDoesNotStartSubStore` drives `convert.ConvertNodeToMihomo` through the real client (passes).
- Runs on the test server: `go vet`, `go test -count=1` of convert, subs, repo, rules, provider, substore, health; `-race` of convert, subs, rules, provider, substore (repo's `TestWriteWaitsForReleasedFileLock` showed an intermittent data race inside `repo/lock_test.go`'s own goroutine/defer — not C12 code; passes on rerun).

Not done here (outside C12's packages): register `convert.RedactEasyTierSettings` as `repo.Hooks.RedactEasytierSettings` in the server wiring; `subs/compat.go` dialer copy and `rules/compat.go` `RulesetFormat` copy stay (import cycles; drift tests run them over the corpus).

Commands:
- tests: `rt -- bash -lc 'cd server && go vet ./internal/convert/... && go test -count=1 ./internal/convert/... ./internal/subs/... ./internal/repo/... ./internal/rules/... ./internal/provider/... ./internal/substore/...'`
- porter corpus: `rt --back server/internal/convert/testdata -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/gen.py'`
- review references: `rt --back server/internal/convert/testdata/review_c12.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/review_c12_gen.py'`

## C13 — Sub-Store client, runtime, subscription cache, dependencies

**Status: `ported`** (not reviewed) · prereqs: C12, C10 · Go: `internal/substore`, routes in `internal/api/settings/dependencies.go`

Files → Go:
- `backend/utils/sub_store_client.py` (453) → `client.go`: `Client` (`GetSubscriptionProxiesYAML` with `FetchOptions{Refresh, PreferCached}` = coalescing, 300/30/900 s reuse, 128-entry bound, failure cooldown 3 s doubling to 300 s, ≥60 s for direct 403/429; `fetchSubscription` = `_get_subscription_proxies_yaml`; `ConvertProxyString`/`ConvertProxyStringTarget` with the per-bundled-version LRU of 512 = `convert.ProxyStringConverter`), `SubscriptionResult`, `SubscriptionFetchError` (Unwrap = `__cause__` = direct error), `ParseProxiesFromYAML`, `ProxiesToNodes`, `isYAMLResponse`; `httpclient.go` = requests-like client (`RequestsClient`, `Request{Params, JSON, Timeout, Proxy}`, `Response.Text/JSON/RaiseForStatus/HeaderValue`, `RequestError{Class, Status}`, `PrepareURL` = prepare_url + urllib3 2.8 parse_url normalisation + requote_uri, `QuotePath`); `omap.go` (dict order for the bookkeeping).
- `backend/utils/sub_store_runtime.py` (233) → `runtime.go`: `Runtime` (`Use`, `Maintenance`, `SetMode`, `Tick`, `Status`, `Monitor`, flock `work`/`control` locks, `state.json`, default root `<tempdir>/configflow-sub-store-<sha256(abspath(DATA_DIR))[:12]>`), `GetMode`, `RuntimeUnavailable`, `IdleSeconds`.
- `backend/utils/subscription_cache.py` (149) → `cache.go`: `CachePath`, `DeleteSubscriptionCache`, `PruneOrphanCaches`, `SaveSubscriptionNodes`, `LoadSubscriptionCache` (returns `(obj, err)`: err only where Python raises outside its try), `SetContentSyncInvalidator` (hook for agents A13 `invalidate_content_sync`; nil default = only logged).
- `backend/utils/dependencies.py` (358) → `env.go` (`Env{LookupEnv, DataDir, HTTP, Commands, Clock, RandomHex}`, `BuiltinSubStoreURL`, `BaseURL` = `_get_base_url`, `ActiveBundle`, `IsBuiltinAvailable`, `FetchRunningVersion`, `ParseVersion`, `IsNewer`, `CommandRunner`/`ExecRunner`, `Clock`/`SystemClock`) + `dependencies.go` (`Dependencies`: `FetchLatestRelease`, `GetSubStoreStatus`, `ListDependencies`, `StartSubStoreUpdate`, `runUpdate`/`installRelease` with rollback, `GetUpdateState`).
- `subscription_health.parse_userinfo` (C15's file, needed by the client) → `userinfo.go` `ParseUserinfo`; C15 should use it from here.
- `wire.go`: `Service{Env, Runtime, Client, Dependencies}`, `NewService`, `Default()`, `SetDefault()`, `Converter()` (pass to `convert.ConvertNodeToMihomo` / `RejectSurgeDialers`), `Wire()`.

Interface for #40 (process control): `substore.CommandRunner.Run(ctx, argv, timeout) (CommandResult{Stdout, Stderr, ReturnCode}, error)`. Python's exact calls go through it: `supervisorctl -c /etc/supervisor/conf.d/supervisord.conf <status|start|stop> sub-store` (timeout 20 s), `… restart sub-store` (60 s), `node --version` (5 s), `node --check <file>` (60 s). Errors must be `*substore.PyError` with Class `FileNotFoundError`/`PermissionError`/`OSError`/`TimeoutExpired` (→ RuntimeUnavailable 「无法管理内置 Sub-Store…」). Default `ExecRunner` (os/exec). #40 installs another by setting `substore.Default().Env.Commands` (or `SetDefault`).

Wiring: `substore.Wire` in `server.wirePackages` registers `lifecycle.Loop("sub-store-idle")` (tick every 5 s, only with the bundle) and a one-shot `lifecycle.Loop("subscription-cache-prune")` (prune_orphan_caches at startup; failure logged by `backend.app` 「清理订阅缓存失败: …」). Routes (in `api/settings.Register`): `GET /api/settings/dependencies`, `POST /api/settings/dependencies/sub-store/update`, `POST /api/settings/dependencies/sub-store/mode` (routes.tsv rows done; `server_test` expects them). Tasks slot: none — subscription/rule_source rows are `scheduled_refresh` (C15).

Tests (`internal/substore/*_test.go`, fake Sub-Store + fake airport on loopback, fake `CommandRunner` driving the fake, fake clock):
- `test_sub_store_runtime.py` 12/12 (`runtime_test.go`, `client_test.go`; the other-process case re-execs the test binary as the lock holder; the route case in `api/settings/dependencies_test.go`).
- `test_dependencies.py` 12/12 (+8 port params) (`dependencies_test.go`; `_wait_for_version`/restart are real against the fake Sub-Store instead of patched).
- `test_subscription_fetch_requests.py` 14/15 (deferred: `test_manual_route_reuses_traffic_without_userinfo_get` → C30 subscriptions route; `requests.Timeout` cases use a refused/closed connection).
- `test_sub_store_no_flow.py` 3/4 (deferred `test_parse_lines_route_converts_each_line` → C30 nodes route).
- `test_subscription_cache_cleanup.py` 3/4 at cache level (delete via `DeleteSubscriptionCache`; deferred: the DELETE `/api/subscriptions` route halves and `test_a_subscription_still_in_use_keeps_its_cache` → C30).
- `test_node_conversion_cache.py` 11/12 here (+ C12's case now through the real client: `TestRenderingAgainDoesNotStartSubStore`); skipped `test_a_value_that_is_not_text_is_passed_through` (Go API takes strings only; ConvertNodeToMihomo never passes anything else).
- `test_request_exception_redaction.py` 3 sub_store_client cases; `test_subscription_hosts.py::test_hosts_change_invalidates_agent_content_checks_once` (metadata built with `subs.ExtractSubscriptionHosts`).
- Extra: state.json bytes, command protocol, 20-s start timeout, cache file bytes, legacy/broken caches, prune error paths, release parsing/caching/errors, status card, update failure paths, sweep.
- Corpus `testdata/gen.py` → `corpus.json` (237 rows: parse_userinfo, parse_version, is_newer, builtin_sub_store_url, _is_yaml_response, parse_proxies_from_yaml, proxies_to_nodes, conversion log line, requests prepare_url, response.text/json) all equal (`corpus_test.go`), except no-charset non-UTF-8 bodies (charset_normalizer guess, skipped).
- Differential `internal/substore/difftest` (real Flask vs Go, same fake Sub-Store, GitHub via a dead HTTPS_PROXY → ProxyError on both): bundled 16 requests + external 4 requests, 0 differences, stored `sub_store_mode` equal.

Decisions / differences from Python:
- `convert.ProxyStringConverter` has no error: an invalid `SUB_STORE_BACKEND_API_PORT` makes Python's convert_proxy_string raise ValueError; Go logs and returns nil. A non-object first proxy (0, "", []) is cached by Python, returned as nil and not cached by Go (same ConvertNodeToMihomo outcome).
- HTTP: Go never sends loopback requests through an environment proxy (requests would unless NO_PROXY); env proxies read once per process (http.ProxyFromEnvironment); IDNA of non-ASCII hosts not done; charset guessing without a charset = BOM else UTF-8; codecs beyond UTF-8/16, Latin-1, ASCII decode as UTF-8; transport error texts are Go's (class + status match).
- `ParseProxiesFromYAML`: non-list falsy `proxies` ('' / {}) → empty list (Python returns that value); YAML errors use the PyYAML port (`rulefetch/pyyaml`) for class/text. yamlx limits apply to `_is_yaml_response`'s fallback parse (non-string keys → not YAML).
- Runtime: a non-object state.json or non-number `last_used` counts as empty/0 (Python TypeError); lock polling on wall time; Monitor recovers panics. supervisorctl output decoded with replacement (Python strict).
- Cache: non-dict `metadata`/`hosts`/`entries` treated as empty in the hosts-change check (Python AttributeError after the write). `ConfigStorageError` (store.ErrStorage) counts as OSError, like Python.
- Update: the bundle is written after the whole download (Python streams); same end state.

Commits: 29df3c0c (code), a7d8f03f (tests), 37f1237f (route tests), fbc859c4 (interim), a55ccd34 (corpus, URL normalisation, PyYAML errors), + final commit (differential, this section).

Reviewer hints: compare `GetSubscriptionProxiesYAML` dict-order bookkeeping (omap) with the Python dicts; `fetchSubscription` branch order (direct → 403/429 → Sub-Store lease → fallback); `installRelease` rollback; `httpclient.go` PrepareURL vs urllib3 2.8 `parse_url`; runtime lock/state order in `Use`/`Maintenance`/`Tick`.

Commands:
- tests: `rt -- bash -lc 'cd server && go vet ./internal/substore/... ./internal/api/settings/ ./internal/server/ && go test -count=1 -race ./internal/substore/... ./internal/api/settings/ ./internal/server/ ./internal/convert/'`
- differential: `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/substore/difftest/'`
- corpus: `rt --back server/internal/substore/testdata/corpus.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/substore/testdata/gen.py'`

## C14 — remote rule fetch, ruleset content, remote sources, website identity

**Status: `ported`** (not reviewed) · prereqs: C12, C08 · Go: `internal/rulefetch`, `internal/pyyaml` (moved from `rulefetch/pyyaml` by review C02)

Files → Go:
- `backend/utils/ruleset_content.py` (722) → `rulefetch/content.go` (ResultCache, ContentDigest, ClearRuleContentCaches, MetadataError, NormalizeClassicalLines, NormalizeMihomoYAML, NormalizeMihomoRuleset, InspectDomainContent, ValidateDomainContent, RemoteRuleValidationError (+NewRemoteRuleMessage/NewRemoteRuleError; unwraps to repo.ErrValidation), ValidateRemoteRuleContent, ReadRulePayload, AppendRulePayload/AppendRuleLines, RemoveRulePayload, ReadRulePayloadAny/AppendRulePayloadAny for rules.ConnectionDeps), `mrs.go` (_validate_remote_mrs on klauspost zstd). Line helpers `_valid_pattern`, `_classical_line`, `classical_line_error`, `_has_yaml_payload` live in `rules` (exported in `rules/ruleset_lines.go`: rulefetch imports rules); `convert/compat.go` now delegates to `rules.HasYAMLPayload`.
- PyYAML (needed for marks/tokens/error text) → `rulefetch/pyyaml` (Scan, Compose).
- `backend/utils/rule_fetch.py` (346) → `rulefetch/fetch.go` (Client{Transport, Local, Sleep}, DefaultClient, Request, RequestRule, RuleFetchSettings, IsInternalRuleURL, GetRuleFetchProxy, RuleRequestProxy, IsGithubURL, GithubDownloadCandidates, RuleFetchError), `transport.go` (Transport interface, HTTPTransport, Response{Content, Read, Close, RaiseForStatus}, FetchError{Class, Transient, Status}, IsTimeout, IsRequestException, PythonClass), `urls.go` (SplitURL = urlsplit, URLJoin = urljoin, ValidateRuleURL, NormalizeRuleFetchProxy).
- `backend/utils/remote_rule_sources.py` (45) → `sources.go` ResolveRemoteRuleSource.
- `backend/utils/website_identity.py` (89) → `website.go` (PageMarkers, SiteTitle, CleanText, KnownService, PageIdentity; `Services` = region_check.SERVICES copy, TODO C16).
- C08's missing `rule_utils.py` functions → `cache.go` (GetRulesDir, RuleCacheReadPath(ctx), ReadValidatedRuleCache, WriteValidatedRuleCache, RefreshRemoteRule (RefreshOptions), ReadRuleSourceContent, SaveRuleToLocal).
- `rules.PyRegex` substitution API (C12 request): `Sub`, `SubTemplate`, `ParseTemplate`/`Template.Expand`, `Match`, `Groups`, `GroupIndex`, `ErrMatchTimeout`, `TemplateIndexError` (`rules/pyre_sub.go`); `convert.pySub` uses it.
- Switched: `api/settings/compat.go` normalize_rule_fetch_proxy → rulefetch (urlsplit copy removed). `rules/compat.go` copies stay (rulefetch→rules; rules cannot import rulefetch) and are exported via `ruleset_lines.go`; the corpus exercises them through rulefetch.

Tests:
- Corpora (real Python, synthetic values): `pyyaml/testdata/gen.py` 6065 rows (tokens, nodes, marks, error text) all equal; `rulefetch/testdata/gen.py` 72849 rows (inspect, normalize ×3, validate_remote str/bytes/MRS, read/append/remove payload, validate_domain, website identity, resolve_remote_rule_source, validate_rule_url, normalize proxy, urljoin) all equal except 3 rows of one accepted difference (below); `rules/testdata/gen_sub.py` 9312 rows (sub with template/function, search spans) all equal.
- Ported: `test_remote_ruleset_validation.py` 12/12, `test_mihomo_yaml_delivery.py` 7/7, `test_ruleset_classical_format.py` 15/15, `test_rule_fetch_retries.py` 10/10 and `test_rule_fetch_fallback.py` 6/6 (fake Transport returning FetchError classes), `test_rule_fetch_proxy_integration.py` 19/19 functions over loopback httptest servers + a test forward proxy (the "untrusted origin behind a trusted HTTPS proxy" variant skipped: httptest servers share one certificate), `test_remote_rule_cache.py` 6/8 (rule_utils halves; route halves and `test_failed_edit_preserves_declaration_and_previous_cache`, `test_invalid_create_does_not_commit_catalog_entry` → C32/C33), `test_ruleset_domain_format.py` 5/13 (inspection parts; MosDNS conversions → C21; route cases → C32/C33/C37), `test_website_identity.py` 4/11 (job/route/region-store cases → C37/C16), C08-deferred `test_rule_syntax.py` 15 cases (content_test.go TestRuleSyntaxThroughRulesetContent), `test_rule_cache_identity.py`/`test_remote_rule_cache_edges.py` legacy read side (TestLegacyCacheReadSide; the rest are routes → C32/C33), `test_local_http.py` deferred rule_fetch cases (local_test.go; the one-worker case has no Go counterpart).
- Deferred: `test_rule_delivery_proxy.py` 3, `test_rule_proxy_freshness.py` 3 (MosDNS zip / rule-proxy routes → C28).

Decisions / differences from Python:
- Transport failures: `FetchError.Class` and the retry decision follow requests/`_is_transient_disconnect` (TLS EOF, reset/abort/EPIPE, server or proxy closing early, body cut short and chunked EOF retried; certificate, timeout, refused, DNS, HTTP status, decoding and invalid chunk length not); message texts are Go's (requests renders urllib3 objects). RuleFetchError text = diag.FailureText (imports the leaf package `agents/diag`, no cycle). `response.url` = request URL with "/" for an empty path. Requests' headers kept (`User-Agent: python-requests/2.31.0`, `Accept-Encoding: gzip, deflate`); gzip/deflate/zstd decoded. Default trust = Go system roots (requests: certifi); REQUESTS_CA_BUNDLE/CURL_CA_BUNDLE honoured for default verify; environment proxies never used.
- `read_rule_source_content`: every error except RemoteRuleValidationError and repo errors becomes `RequestException("Rule download failed (<class>)")`.
- PyYAML RecursionError = nesting limit 490 (Python's depends on the stack depth); `\U` surrogates → U+FFFD.
- Accepted (corpus allowlist): a flow-style YAML root with a non-string mapping key (`{payload: [...], 1: x}`) cannot be loaded into jsonx: NormalizeMihomoYAML keeps the text with the yamlx error as warning where Python rewrites it. ReadRulePayload falls back to a node-based document for such keys (merge keys not applied in the fallback).
- website_identity: `_Meta` (HTMLParser) is a regex start-tag/attribute scan; `html.unescape` = Go `html.UnescapeString`; a non-numeric status counts as not 2xx (Python TypeError).
- MRS: python-zstandard's "unknown frame prefix" for 1–3 trailing bytes after complete frames is emulated (trailingGarbage).
- `refresh_remote_rule` log uses rulefetch's own safe_exception_details with Python class names. Suggestion for C02: let `urlx.SafeExceptionDetails` honour a `PythonClass() string` interface (it prints Go type names today).
- PyRegex.Sub/SubTemplate/Match always run on regexp2 (CPython must_advance loop via `\G(?:p)(?!\G)`); a backtracking timeout returns ErrMatchTimeout.

Commits: d426421b (pyyaml), c2ba3f6b (PyRegex sub), d3f50ef7 (content/MRS/sources/website/URLs + corpus), d36e5331 (fetch, transport, cache), edf7ac79 (fake transport tests), e773ef34 (integration), (cache tests), 3b9060e2 (settings switch), 7050d97d (local dispatch, adapters).

Reviewer hints: compare content.go branch by branch with ruleset_content.py (rune indices everywhere marks are used); transport classification (`classifyRequestError`, `classifyBodyError`) against urllib3/requests exception mapping; urls.go urlsplit/urljoin against CPython 3.11; website.go metaContent vs HTMLParser.

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/rulefetch internal/rules internal/convert; go vet ./internal/rulefetch/... ./internal/rules/... ./internal/convert/... && go test -count=1 ./internal/rulefetch/... ./internal/rules/... ./internal/convert/...'` (last: all ok; `-race` ok)
- corpora: `rt --back server/internal/rulefetch/testdata/corpus.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen.py'`; `rt --back server/internal/pyyaml/testdata -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/pyyaml/testdata/gen.py'`; `rt --back server/internal/rules/testdata/sub_reference.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rules/testdata/gen_sub.py'`

## C15 — subscription refresh, health, scheduled refresh

**Status: `ported`** (not reviewed) · prereqs: C13, C14, C07 · Go: `internal/refresh` (new; not `internal/substore`: the module also drives rule sources through `rulefetch` and registers task rows)

Files → Go:
- `backend/utils/subscription_refresh.py` (139) → `interval.go` (`ValidateRefreshInterval(value, label)` → `*IntervalError` (ValueError), `DefaultSourceInterval`/`DefaultProviderInterval`/`MaxInterval`, `DefaultIntervalLabel`), `source.go` (`SourceCacheMetadata`, `isFresh`, `DownloadCachePath`, `RememberSourceDownload`, `DownloadSubscriptionNodes`, `FetchSubscriptionSource(ctx, sub, fetch SourceFetch)` → `*rules.StrategyReferenceError` for interval/manual-mode refusals).
- `backend/utils/subscription_health.py` (114) → `health.go` (`HealthFile`, `HistorySize`, `UserinfoTimeout`, `UserinfoUserAgent`, `ParseUserinfo` = `substore.ParseUserinfo` (C13's port, reused), `FetchUserinfo` (no production caller in Python; through `Env.HTTP`), `LoadHealth`, `RecordFetch(ctx, id, FetchRecord{OK, Count, FromCache, Traffic})`, `Forget`).
- `backend/utils/scheduled_refresh.py` (347) → `scheduled.go` (constants, `TaskBusy`, `StaleRuleCache`, `LookupError`, `SubscriptionKey`/`RuleSourceKey`, `IsRunning`/claim/release, `DescribeFailure`, `NextRunAt(entry any, interval jsonx.Number, baseline, now)`, `SubscriptionCacheTime`, `RuleCacheTime`, `IsLibrarySource`, `RefreshSubscription`, `RefreshRuleSource(ctx, rule, trigger, RuleOptions{Config, Fetch, Timeout, AllowStale})`, `ReadRuleSource`, `due`, `run`, `RunRound`, `FindResource`, `RunNow`), `rows.go` (`SubscriptionRows`/`RuleRows` = `routes/tasks._subscription_rows`/`_rule_rows`, `runTask` (run_task's subscription/rule_source branch), `Loop` = start_loop's thread, `Wire`), `pyerr.go` (Python class of an error, requests hierarchy for isinstance, `safe_exception_details`, `int()`), `env.go` (`Env{Clock, FetchSubscription, RuleClient, HTTP, RandomHex, Converter}`, `DefaultEnv`, `SetEnv`).
- Callers still to come use: `FetchSubscriptionSource` + `SourceCacheMetadata` (C17 provider delivery, C31 aggregations), `DownloadCachePath` (subscription_dialers, C17/C18), `ReadRuleSource` + `DescribeFailure` (C28 MosDNS, C32 rules, A11 deployment bundle), `RefreshRuleSource` + `RuleSourceKey` (C33 rule library), `RefreshSubscription`, `SubscriptionKey`, `LoadHealth`/`RecordFetch`/`Forget`/`HistorySize` (C30 subscriptions, C36 setup_abandon), `ValidateRefreshInterval` (C18 Mihomo provider interval, C34 config route).

Wiring: `refresh.Wire` in `server.wirePackages`: `tasks.SetRows` for `subscription` and `rule_source`, `tasks.SetRunner("subscription"|"rule_source")` (TaskBusy → 409, LookupError → 404, ValueError → 409), `lifecycle.Loop("scheduled-refresh")` (flock `DATA_DIR/.task-scheduler.lock` LOCK_EX|LOCK_NB, held until shutdown; first round after 30 s, then every 30 s; round failures logged `Scheduled refresh round failed`; ≤3 workers; each job in its own repo scope). Task status through `notifications.TaskStatus(ctx, dataDir)` with `Env.Clock`. `internal/tasks.ValidateRefreshInterval` removed; `api/tasks` uses `refresh.ValidateRefreshInterval`. `repo/compat_health.go` keeps its private copy (repo cannot import refresh); `TestValidateRefreshIntervalMatchesTheRepoCopyGolden` checks this package against repo's golden rows.

**/api/tasks and GET /api/notifications still cannot register**: missing task slots `router_rule_sync` (routers), `agent` (A16/A17 rule sync + DNS follow rows), `domain_discovery` (C37) (`internal/server/refresh_wire_test.go` logs `tasks.Missing()`).

Tests (`internal/refresh/*_test.go`; fakes: `Env.FetchSubscription` for get_subscription_proxies_yaml, a fake `rulefetch.Transport`/`Fetch` for request_rule, fake clock, fake `substore.HTTPClient`):
- `test_scheduled_tasks.py` 9/13 here: record counts, next_run back-off, failed rule refresh survives reload (rows), failure reasons, rule without valid cache, device reads, run now (route), interval edit (route part), scheduler skips disabled, rule due on first round. Deferred: `test_source_changes_and_deletions_forget_the_previous_history` → C30/C33 (subscription / rule-library PUT, DELETE); the rule-library PUT half of `test_interval_edit_validates_and_rule_source_edits_keep_it` → C33; `test_router_agent_dns_follow_domain_and_backup_rows` → routers/A16/A17/C37 (backup row is C07's); `test_domain_discovery_records_a_skipped_round_with_its_reason` → C37.
- `test_subscription_refresh_policy.py` 7/12 at fetch_subscription_source / scheduler level (auto delivery reads cache ×2 intervals, manual-only refusal, expired cache served + scheduled refresh, failed refresh keeps nodes, dry previews share the download across a restart, expired download refetched, changed URL in manual mode) + the validator for the invalid-interval cases. Deferred: provider endpoints (`/api/subscriptions/<id>/proxies`, `/api/subscriptions/proxies`, `/api/aggregations/<id>/provider`) → C17/C30/C31; `test_manual_refresh_is_allowed_and_next_poll_uses_new_cache` → C30; `test_publish_preparation_uses_cache_and_stages_no_fake_refresh` and the prepare_subscription_provider halves → C17; `test_mihomo_sync_setting_is_independent_and_applies_to_aggregations` / `test_invalid_mihomo_sync_setting_does_not_save_yaml` → C18/C34; `test_invalid_source_intervals_are_rejected_without_saving` route → C30.
- Other files: `test_flow_ui_apis.py` `test_parse_userinfo`, `test_fetch_userinfo_skips_non_http`, record_fetch/load_health/forget half of `test_subscription_health_history_and_derived_fields` (routes → C30); `test_subscription_hosts.py` `test_only_downloaded_bodies_carry_hosts_into_the_cache_metadata`, `test_scheduled_download_stores_hosts_next_to_nodes`; refresh half of `test_remote_rule_consumers.py::test_rule_proxy_uses_valid_cache_after_invalid_refresh` (rule proxy → C28). Still for later modules (they call C15 functions through routes): `test_rule_fetch_proxy_routes.py::test_local_refresh_fetches_legacy_entry_without_source_type` (C32), `test_rule_proxy_freshness.py` (C28), `test_mihomo_classical_delivery.py::test_url_source_and_fallback_keep_raw_cache` (C17/C32), `test_proxy_group_regex_exclusion.py::test_provider_refresh_filters_response_without_removing_source_cache` (C17), `test_agent_push_diff_api.py` / `test_agent_content_*` / `test_dynamic_proxy_chains.py` / `test_subscription_dialers.py` cases (agents, C17), `test_profile_scoped_artifacts.py::test_rule_cache_workers_use_shared_rule_sources` (C33), `test_notifications.py` two is_running cases (C07, monkeypatch of is_running).
- Extra: health file edges (non-object, broken, string/object/int history), userinfo request shape/404/request error, refresh records (fetch failure without cache, success with traffic, download cache), run-now busy/lookup/no-URL, loop lock + sleeps, row key order, stale device read.
- Corpus `testdata/gen.py` → `corpus.json` (963 rows, CPython 3.11.17): validate_refresh_interval, subscription/rule_interval, 775 next_run_at (offsets incl. week dates, compact, `,` fractions, ±23:59, year 1/9999 overflow, timedelta bound, failures as str/full-width/float/inf/nan/list/huge), _is_fresh, _download_cache_path, 59 describe_failure (every requests class with/without response, SubscriptionFetchError combinations, RuleFetchError, repo/OS/ValueError/StrategyReferenceError/DialerReferenceError), fromtimestamp(st_mtime) rounding, _host, and repository scenarios (record_fetch/forget sequences on fresh/edited/broken files; full subscription + rule rows and `_due` with caches, statuses and rule-cache mtimes). All equal except 18 accepted rows (sub-second UTC offsets; instants equal).
- Differential `internal/refresh/difftest` (real Flask vs Go on copies of one data dir; fake airport + rule server on loopback, external Sub-Store on a closed loopback port): GET /api/tasks ×3, POST /api/tasks/run ×19 (successes, empty subscription, 403, invalid rule, 503 rule, content source, no URL, gone, malformed bodies), PUT /api/tasks/interval ×12 — **31 requests, 0 differences**; files equal after the run (times masked): task-status.json, subscription_health.json, subscribes/s-ok.json, both subscription-downloads, rule cache + .bak, stored subscriptions/rule_library. Go side: empty fakes for the three missing slots, `metrics.SetStartedAt` (Flask's flush loop is running).

Decisions / differences from Python:
- `parseTime` keeps the parsed UTC offset (`taskstatus.ParseTime` returns UTC; Python keeps tzinfo, so `next_run_at`/`_iso(due)` print the stored offset, and year-1 local times stay valid). Sub-second offsets are cut to seconds (FixedZone).
- Python class names come from the error itself (`pythonClass`: own types, repo/store kinds, OS errnos, `PythonClass()`); Go errors without a Python counterpart are named by their Go type in `更新失败（…）` (urlx convention).
- `SubscriptionResult.Traffic` is an object or nil: a hand-edited non-object `traffic` in a transport cache is dropped (Python passes it on). `cache_updated_at` of a hand-edited non-string `updated_at` is not recorded (taskstatus.Result takes strings).
- `FetchUserinfo` reads the body (Go client), Python streamed and closed after the headers.
- A non-string `id`/`url` where Python indexes `sub['id']`/`sub['url']` fails with a TypeError-class error (Python fails later with its own class); missing keys are KeyError like Python.
- `host()` of a truthy non-string URL is "" (Python AttributeError → 500). The loop's open/lock errors are logged (Python's thread died with a traceback on stderr).

Findings in other modules (not fixed here; for their reviewers):
- **C02 `taskstatus.Record`**: `pyInt` reads a string `run_count`/`failure_count`/`consecutive_failures` (hand-edited task-status.json) as 0; Python's `int(entry.get(...) or 0)` converts `"4"` → 4 (and raises on `"x"`). Seen in the differential (seed with string counts: run_count 5 vs 1). `taskstatus.ParseTime` drops the offset (see above; A04's metrics next_run_at has the same display difference).
- **C07 `tasks.History`**: `pyIntOrZero` keeps a string run_count/failure_count as is; Python `int()`s it. C15's rows re-convert with `pyIntValue` (`historyOf`); the other rows (backup, metrics, agents) still show the string.

Commits: db825e78 (code, wiring), 81b02e4e (tests), 9c15d0a4 (health/route/server tests), d53dd44c (corpus, offsets, run counts), 2157ade9 (differential), + this PROGRESS commit.

Reviewer hints: `DescribeFailure` order and the requests hierarchy (`requestsBases`); `NextRunAt` int()/min/timedelta overflow; `FetchSubscriptionSource` branch order (`cache.get('metadata', {})` AttributeError, `in (None, url)`, interval 0 vs current); `RefreshSubscription`/`RefreshRuleSource` record order and what is recorded on each path; `_due` truthiness (`not sub.get('enabled', True)`) vs rows (`is not False`).

Commands:
- tests: `rt -- bash -lc 'cd server && go vet ./internal/refresh/... ./internal/tasks/ ./internal/api/tasks/ ./internal/server/ && go test -count=1 -race ./internal/refresh/ ./internal/tasks/ ./internal/api/tasks/ ./internal/server/'`
- differential: `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/refresh/difftest/'`
- corpus: `rt --back server/internal/refresh/testdata/corpus.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/refresh/testdata/gen.py'`

## C16 — health check, latency, node DNS, region check, domain probe

**Status: `ported`** (not reviewed) · prereqs: C12, C13 · Go: `internal/health/check` (leaf: no repo import), `internal/health`, routes in `internal/api/nodes`

Files → Go:
- `backend/utils/health_check.py` (82) → `check/healthcheck.go`: `ProviderDefaults()`, `GroupDefaults()`, `GroupHealthTypes`/`IsGroupHealthType`, `InitializeGroupHealth`, `InitializeProviderHealth`, `ValidateHealth(values, provider)` (`*check.ValueError`), `ProviderHealthCheck` (missing key → `*check.KeyError`), `UserInfo`.
- `backend/utils/region_check.py` (367) → `check/region.go`: constants, `ExitRequest`, `GroupTypes`/`NonNodeTypes`/`ExcludedTargets`, `RegionRules`, `RegionOfName`/`RegionOfNameValue`, `RegionLabel`, `Services` (+`ServiceByID`, `Service.Requests()`, `Service.Evaluate(byID)`), `ServiceRequests`, `EvaluateServices`, `ExitInfo`, `PublicServices`, `BlockPattern`, `DomainRequest`, `ClassifyDomainResult`, `SummarizeDomain`, `PickTargets`, `RegionStore` (`NewRegionStore(dir)`, `Update(id, mutate, now)`, `Load`, `Delete`, `Path`), `NaiveISOSeconds`. Python regexes via `rules.CompilePyRegex` (look-behind, Unicode IGNORECASE).
- `backend/utils/domain_probe.py` (197) → `check/probe.go`: constants, `ControlTargets`, `VerdictOrder`/`VerdictLabels`, `ProbeURL`, `PathHealthy`, `Judge(direct, proxy, JudgeOptions)` (`DefaultJudgeOptions`), `ProbeStore` (`Save`, `Load`, `Delete`), `LatestProbes`, `DomainSuggestion`. `check/num.go` = Python int/float arithmetic (exact int/float comparison, correctly rounded true division, round-half-even); `check/pyerr.go` = `*check.PyError{Class}` for malformed Agent answers / hand-edited files (raised via panic+recover inside each exported function).
- `backend/utils/node_dns.py` (189) → `health/dns.go`: `ConfigKey`, `DirectDoH`, `DNSError`, `BootstrapError`, `CheckMessages`, `NormalizeDNSServers`, `NormalizeConfig`, `LoadConfig`, `EffectiveSettings(ctx, value)` / `DescribeSettings` (value nil = saved setting, as `None`), `SaveConfig`, `CheckSettings`. `health/ipaddr.go` = `ipaddress.IPv4Address` / `ip_address` / `str()` of Python 3.11.17 (IPv4-mapped printed dotted).
- `backend/utils/node_latency.py` (254) → `health/latency.go`: constants, `Settings`, `DNSSettings`, `CollectCandidates`, `proxyOf`, `needsSubStore`, `runProbe`/`parseResults`, `LoadResults`, `RunTests(ctx, names, keys)` (nil = None; process-wide TryLock → `*ProbeBusy`; `LockForTest`), `isoUTC`. Errors: `*health.ProbeUnavailable` (503), `*health.ProbeBusy` (409), `*health.ValueError` (400; also the UnicodeDecodeError of non-UTF-8 worker output, CPython message).
- `health/runner.go`: `Runner`/`Command`/`Result`, `ExecRunner`, `ErrTimeout`, `*OSError`, `Env{LookupEnv, Environ, Runner, Now, ProbeBinary, Converter}`, `DefaultEnv`/`SetEnv`/`CurrentEnv`, `FindProbeBinary`.
- `health/wire.go`: `GetProbeStore(ctx)` (DATA_DIR/probes), `GetRegionStore(ctx)` (DATA_DIR/regions), `Wire()`.
- Routes (`routes/nodes.py` latency part, which only wraps C16): `internal/api/nodes/latency.go` `Register` — `GET|POST /api/nodes/latency` (`nodes.node_latency`), `GET|POST /api/nodes/latency/settings`, `POST /api/nodes/latency/dns/check`; registered in `server.routePackages`; routes.tsv rows done. **C30 adds the node CRUD routes as its own file in `api/nodes` and lines to the same `Register`.**

**node-probe interface for #40** (doc comment in `latency.go`): argv `[<binary>]` only; stdin = `json.dumps({url, timeout_ms, dns_servers, dns_bootstrap, unified_delay, nodes}, ensure_ascii=False)` (latency; node `name` = source key) or `json.dumps({"operation": "dns_check", "timeout_ms": 5000, dns_servers, dns_bootstrap})` (ASCII); env = process env + `GOMAXPROCS=2`, `GOMEMLIMIT=128MiB` unless set; timeout ceil(n/20)·timeout_ms/1000+5 s (latency) / 10 s (check), killed on expiry; stdout `{"version": 4, ...}`; exit code 3 = invalid DNS settings; stderr never shown. Binary lookup: `CONFIGFLOW_NODE_PROBE_BIN` alone if set, else `/opt/configflow/node-probe/configflow-node-probe`, `resource.FromEnv().Backend("node-probe/configflow-node-probe")`, PATH. #40 only has to ship the binary there (or set the env var); `health.ExecRunner` runs it, `health.SetEnv` replaces the runner.

Wiring: `health.Wire` in `server.wirePackages` registers `agents.OnAgentDeleted("domain_probe")` then `("region_check")` (A01 table). No loops, no task rows (Python has none for C16). `internal/server.TestDeleteAgentClearsEveryPerAgentStoreInAFullyWiredServer` unskipped and passing.

Compat switch-over:
- `rulefetch/website.go`: the `Services` copy (TODO C16) removed; `KnownService` iterates `check.Services`.
- `rules/idna_export.go` (new file in rules): `rules.IDNAEncode` exports the existing `pyIDNAEncode` (needed by `node_dns._server`).
- **For the coordinator:** `repo/compat_health.go` `validateHealth` can now call `check.ValidateHealth` (leaf package; repo → health/check → rules/urlx/pystr/jsonstore has no cycle); wrap `*check.ValueError` into repo's helper ValueError. Not done here (repo is verified, not my file). `validateRefreshInterval` there stays (C15's).

Tests:
- Reference corpus `health/testdata/gen.py` → `corpus.json` (CPython 3.11.17, 790 rows): validate_health group/provider (95×2 incl. 30 URL forms), initialize_*_health, provider_health_check, 77 DNS server forms ×2 (servers/bootstrap), lists, normalize_config, check_settings over 40 worker outputs (address canonicalisation, fake-IP, latency types, error codes, version, exit codes), 11 UTF-8 decode-error messages, `_run_probe` over 20 outputs + 5 exit codes, settings() over 22 env sets, region_of_name (42 incl. Kelvin sign, İ, full-width), region_label, 45 evaluate cases ×3 (service, evaluate_services, exit_info) incl. malformed Agent answers, classify_domain_result (26), summarize, pick_targets (15 incl. malformed), judge (37 incl. big ints, floats, NaN, string delays), probe_url, domain_suggestion, path_healthy, probe/region store file bytes and odd files — **all equal** (`corpus_test.go`).
- Ported: `test_node_latency.py` 25/25 (`api/nodes/latency_test.go` + `health/latency_internal_test.go` for `_proxy_of`); `test_node_dns.py` 13/13 (`api/nodes/latency_test.go`); `test_region_check.py` 11/19 here (evaluations, service limits, classify, summarize, region_of_name, pick_targets, corrupt stores probe+region; `check/check_test.go`); `test_domain_probe.py` 5/16 functions (judge ×12, missing path, probe_url, store expiry, suggestion); C14-deferred `test_website_identity.py::test_cache_expiry_and_bound`. Extra: 5000-host bound, Wire hook order + files removed, isoUTC.
- Deferred: `test_region_check.py` `test_probe_entry_injected_only_when_enabled_for_agents` (C18), `test_agent_config_injects_probe_only_with_domain_discovery` (C18 + A14 config_generator), `test_region_settings_validate_port`, `test_region_check_requires_enable`, `test_region_check_builds_matrix_and_domain_verdicts`, `test_route_service_to_policy_creates_ruleset_and_undo`, `test_route_places_ruleset_before_competing_user_rule`, `test_apply_warns_when_an_earlier_rule_still_matches` (C37), traffic-store case of `test_stores_treat_corrupt_files_as_empty` (A05); `test_domain_probe.py` 11 end-to-end cases (fake Agent, jobs, auto rounds, history/undo) → C37; `test_website_identity.py` job/route cases → C37. Health tests: `test_health_check_advanced.py` 4, `test_health_https_defaults.py` 6, `test_health_persistence.py` 2, `test_health_review_r1.py` 8, `test_health_review_re1.py` 10, `test_health_review_re2.py` 8, `test_health_validation.py` 9 all go through generation (`build_provider_health_check`, Mihomo/Surge/Loon → C18/C19/C20), `/api/proxy-groups` (C33), `/api/subscriptions`/`/api/aggregations` (C30/C31), MCP (#39) or repository validation (still `repo/compat_health.go`); validate_health itself is covered by the corpus. Seeding per-Agent `probes/`/`regions/` files in `api/agents/difftest` (A01 suggestion) not done: that package is the agents agents'.
- Differential `health/difftest` (real Flask vs Go, copies of one data dir, one fake worker script via `CONFIGFLOW_NODE_PROBE_BIN` recording its stdin, Sub-Store on a closed port): 44 requests (all candidates, names/keys, missing/disabled/filtered, URI conversion failure, old worker, exit 3/1, garbage, wrong names, bool latency, invalid UTF-8 → 400, timeout, missing (non-executable) worker, 7 body errors, settings save/invalid/managed/custom/bootstrap, history reset, 10 DNS check cases incl. `{"dns": null}`) — **0 differences**; worker stdin byte-identical per request; `shared/node_latency.json` (tested_at masked) and `system_config` DNS fields equal.

Decisions / differences from Python:
- `validate_health` walks the object's key order; Python walks `allowed & values.keys()` (hash order, randomised per process) — only which message wins when several fields are invalid.
- `statistics.median` on non-number delays raises TypeError in Go; Python sorts strings and fails only later (or not, when the other path has no delays) — hand-crafted Agent answers only.
- Crash texts (`*check.PyError`, KeyError, TypeError) are approximate; classes match the corpus.
- Python's `_run_probe`/`check_settings` raise UnicodeDecodeError from `subprocess.run(text=True)` (400 with the codec message); reproduced with CPython's message (locale assumed UTF-8, as in the image).
- Python's `ThreadPoolExecutor` conversions → 5 goroutines; log line order is nondeterministic on both sides.
- `subscription` entries / `nodes` lists that are not dicts/lists (repository-validated) give TypeError-class errors; non-list truthy `nodes` in a cache ignored unless a number (TypeError), as Python's iteration.

Pitfalls: tests share `health.SetEnv` and the process-wide test lock — restore with `t.Cleanup(health.SetEnv(...))`. The shared worktree held another agent's non-compiling `internal/agents/proxy.go` (missing go.sum for x/net/idna) during the differential: run on a private detached worktree of HEAD.

Commits: 8e17b7f9 (code, wiring, routes, rulefetch switch), d0d974e4 (corpus), c7541241 (ported tests), 2c327410 (differential, routes.tsv), + this PROGRESS commit.

Reviewer hints: `encryptedServer` vs `_server` (urlsplit/IDNA/port/`netloc.endswith(':')` order); `parseCheck`/`parseResults` exception order (which errors become ProbeUnavailable vs pass through); `runTests` ordering of `measured`/`stored` (file bytes) and the `same_test` history rule; region evaluators' malformed-input branches; `num.go` comparisons.

Commands:
- tests: `rt -- bash -lc 'cd server && go vet ./internal/health/... ./internal/api/nodes/ ./internal/server/ ./internal/rulefetch/ && go test -count=1 -race ./internal/health/... ./internal/api/nodes/ ./internal/server/ ./internal/rulefetch/ ./internal/rules/'` (last: all ok)
- differential: `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/health/difftest/'`
- corpus: `rt --back server/internal/health/testdata/corpus.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/health/testdata/gen.py'`

## C17 — provider delivery and responses

**Status: `ported`** (not reviewed) · prereqs: C15 · Go: `internal/provider` (new)

Files → Go:
- `backend/utils/provider_delivery.py` (424) → `delivery.go`: `Snapshot` (= DeliverySnapshot: `Capture`, `Main` = json.loads(main_json), `HasChains`, `Adapt`, `Validate(proxies, forceChains, providerName *string)`, `ProviderName(sub['name'])`), `AdaptDelivered` (+ `dialerRecord` = subscription_dialers.dialer_record(sub) with its cache / download-cache reads), `ValidateProxyShapes`, `loadProviderProxies` (= `_load_provider_proxies`), `ParseProviderProxies`, `PrepareSubscriptionProvider` (`PrepareOptions{Fetch, AllowTransportFallback, SourcePath, SourcePrepared}`), `ValidateRenderedBundle`, `CommitCacheUpdates`, `PrepareProviderBundle` (`BundleOptions{Requested, RenderAll, AllowTransportFallback, Prepared, SubscriptionFetch, Progress}`; per-(id,url) single-flight fetch like the Python futures), `CommitProviderBundle`. Result dicts are `*Rendered{Name, HasName, Content, CacheUpdates []CacheUpdate{SubID, Nodes, Metadata}, AggregationID, URL, LocalPath, Extra}`. `errors.go`: `*provider.Error{Class, Message}` (re-wrapped StrategyReferenceError / DialerReferenceError / ListenerReferenceError with `_provider_contexts` and `provider_field`; `errors.As` finds the convert/subs/rules types), `providerContext`, `shapeError`, `providerSourceError` (field lookup on `pyyaml.Compose` + `flattenMapping`, last duplicate key wins), `yamlCause` (the yaml.YAMLError as `*pyyaml.Error`: compose errors re-composed with marks; ConstructorError parsed back from str(exc)).
- `backend/utils/provider_response.py` (116) → `response.go`: `ServeProvider(c, kind, itemID, profileID, configData, render Render)` (single flight keyed by kind/profile/item/format/regex/exclude_regex/sha256 of sorted config JSON; pool of `PoolWorkers`=8; `ResponseSeconds`=12 (`SetResponseSeconds` for tests); only no-`format` requests fall back; a 200 `text/yaml` render is written to `providers/served/<kind>-<id>.yaml`; last good → aggregation artifact `providers/<id>.yaml`; fallback answer `text/yaml; charset=utf-8` + `X-ConfigFlow-Provider: last-good`, read with read_text's universal newlines; every waiter gets a copy of status/headers/body; warning logged on `backend.utils.provider_response`), `RequestedFilters(c)` (`*rules.CandidateValidationError` → 400).

Interfaces for other modules (decision 3):
- **provided by A11 (agents)**: `provider.SetLocations(Locations)` — `PreparationContext(message, name, path, kind string) string` = str(deployment_locations.preparation_context(...)) (name/path already `str(x) if x else ''`), `SourceError(message, text, line, column *int, cause *pyyaml.Error, sourceLabel) string` = str(source_error(...)) (cause nil = None; ReaderError has empty fields). Default `PassthroughLocations` returns the message unchanged (texts lack location/excerpt, error classes and decisions unchanged). `diagnostics.failure_text` = `agents/diag.FailureText` (leaf, already shared).
- **provided by C31**: `provider.SetAggregationProvider(fn)` — `generate_aggregation_provider(agg, config=, main_config=main, persist=False, subscription_fetch=fetch, source_prepared=True)` returning `*Rendered` (counts in `Extra`). Default fails closed with `ErrAggregationProviderMissing`.
- `Env{Fetch (sub_store_client.get_subscription_proxies_yaml), SubscriptionFetch / AggregationFetch (the two route-module names Python tests patch), Converter, NodeID}`; `SetEnv`, `SequentialNodeIDs`.
- Routes: none (the provider endpoints are C30/C31: they call `RequestedFilters`, `ServeProvider` with a render closure capturing `c`, `Capture`, `ParseProviderProxies`, `PrepareProviderBundle`, `CommitProviderBundle`; A14/A22 call `PrepareProviderBundle`/`CommitProviderBundle`/`CommitCacheUpdates`). So no Flask↔Go differential here; the routes' differential (headers incl. Content-Disposition, Content-Type, last-good marker) belongs to C30/C31.

Tests (`internal/provider`):
- Corpus `testdata/gen.py` → `corpus.json.gz` (CPython, synthetic values, fake A11 on both sides + 'real' A11 texts recorded for the A11 porter): 1581 rows — validate_proxy_shapes 32, parse_provider_proxies 224 (YAML errors incl. constructor errors, merges, duplicate keys, secrets, flow/block, CRLF), DeliverySnapshot capture/has_chains/validate 135 rows × 151 calls, validate_rendered_bundle 15, prepare_subscription_provider 745 scenarios (fresh repo, seeded caches/download caches, fake fetchers, fixed clocks, sequential node ids; result, fetch calls and files after), prepare_provider_bundle + commit_provider_bundle 430 scenarios (requested/render_all/fallback/prepared, aggregation fake, progress messages, arguments not mutated, files written). **All equal** except 2 accepted rows of other modules (below).
- Ported: `test_provider_response.py` 8/9 (`response_test.go`; the immediate-render test checks no flight is left; deferred `test_aggregation_endpoint_falls_back_when_generation_is_slow` → C31), `test_provider_filter_isolation.py` 4/4, `test_provider_error_locations.py` 13/14 against the fake A11 (what C17 passes: field, line/column, label, owner, path; secrets/excerpt redaction is A11's — corpus 'real' texts; deferred `test_publication_entrypoints_preserve_subscription_and_aggregation_hierarchy` → C30/C31/A14), `test_dialer_provider_content.py` 1/3 (`test_empty_provider_is_valid`; the two route/push cases → C30/C31/A14), `test_subscription_refresh_policy.py` `test_publish_preparation_uses_cache_and_stages_no_fake_refresh`, `test_dry_previews_share_download_across_restart_without_changing_validated_nodes` (prepare halves), `test_subscription_regex_filter.py::test_provider_delivers_filtered_proxies_but_caches_raw_nodes`, provider half of `test_dialer_delivery_staging.py::test_no_chain_direct_subscription_preserves_duplicate_legacy_names`, extra: headers copied / last-good newline translation, RequestedFilters cases, default hooks.
- Still deferred (need generation C18 or routes C30/C31/A14): `test_proxy_chains.py`, `test_dynamic_proxy_chains.py`, `test_group_exclusion_generation.py`, `test_subscription_dialers.py` provider cases (their `main` comes from generate_mihomo_config — scenario shapes are covered by the corpus), `test_dialer_*` route cases listed under C12, `test_subscription_refresh_policy.py` endpoint cases, `test_agent_content_*` / `test_agent_push_diff_api.py` / `test_generated_preview.py` (patch commit_provider_bundle; A14).

Decisions / differences from Python:
- Provider YAML is loaded with `yamlx` `KeepNonStringKeys` (dicts with int keys are dicts). Values yamlx cannot hold (recursive aliases, `!!set`/`!!omap`/`!!pairs`) give a non-YAML error (Python loads them, then a shape error) — treated like other non-YAML load errors (transport fallback in prepare).
- `ServeProvider`'s render gets `context.WithoutCancel(request ctx)` (same repo scope as the request; Python's pool thread had fresh config ContextVars but re-read the same request). A panic in render becomes an error (500), as an exception in the future did.
- Non-string provider `url` in a definition → TypeError-class error when sources are collected (Python fails later, only if materialized). Non-string sub ids/urls passed to fetchers as `str()`.
- `flattenMapping` is a private copy of yamlx's unexported `constructor.flatten` (TODO: switch when C02 exports it).

Findings in other modules (not fixed here):
- **C13 `substore.ParseProxiesFromYAML`**: mappings with non-string keys (`proxies: [{1: a, name: A}]`, `1: a` at the root) fail (yamlx NonStringKeyError); Python returns them (key `1`). Corpus allowlist `knownDifference`.
- **C12 `convert.ValidateEmitted`**: with a non-dict provider definition (`proxy-providers: {F: 1}`) and an active chain, Go raises AttributeError where Python first reports the dangling `dialer-proxy` (DialerReferenceError). Hand-made invalid main only; allowlisted.

Commits: cb386206 (code), 2e9a7dfe (corpus, response tests), 430f79bc (ported tests), + this PROGRESS commit.

Reviewer hints: `prepareSubscriptionProvider` try/except boundary (which errors become transport errors; `update` only after validation); `PrepareProviderBundle` render condition order and the `active` rescan; `providerContext` dedup key; `parseMarkedError` for ConstructorError marks; `ServeProvider` timer vs queueing in the 8-slot pool.

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/provider; go vet ./internal/provider/ && go test -count=1 -race ./internal/provider/'` (last: ok, 38.6 s)
- corpus: `rt --back server/internal/provider/testdata/corpus.json.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/provider/testdata/gen.py'`

## C18 — Mihomo config generation, Mihomo base settings

**Status: `ported`** (not reviewed) · prereqs: C12, C15, C16, C17 · Go: `internal/convert` (+ `internal/convert/mihomoenv`)

Files → Go:
- `backend/converters/mihomo.py` generation group → `convert/mihomo_generate.go`: `CustomConfigYAMLError`, `ValidateIPv6Flags`, `DefaultMihomoTemplate`, `SplitRulesAndRulesets`, `DefaultHealthCheckURL`, `BuildProviderHealthCheck` (structured `health_check` via `health/check.ProviderHealthCheck`), `LogicRuleTypes`, `ApplyNoResolveToLogicRule`, `NormalizeFindProcessMode`, `ParseMosdnsCustomHosts`, `SyncMosdnsHosts`, `InjectRegionProbe`, `GenerateOptions`, `GenerateMihomoConfig` (= generate_mihomo_config; YAML via `yamlx.IndentDump`), `GenerateMihomoDocument` (= `yaml.safe_load(generate_mihomo_config(...))`, used by many callers); `convert/mihomo_downloads.go`: `Download`, `GetMihomoProviderDownloads`, `GetMihomoRulesetDownloads`; `convert/mihomo_generate_py.go`: Python-semantics helpers (raise/catch panics → returned errors, `dictGet`/`dictItem`/`iterate`, ordered sets, `appendURLQuery` = profile_context.append_url_query, `pyIntOf`).
- `backend/common/mihomo_base_settings.py` → `convert/base_settings.go`: constants, `BaseSettingsError`, `SplitHostPort`, `ControllerScope`, `SniffPortError`, `ValidateBaseSettings(document, previous, reserved map[int]string, localAddresses []any)`, `TakeoverMode`, `AgentSource` + `AgentContext` (repo bindings + agent manager behind an interface: convert cannot import repo), `controllerSecretError` = setup_flow.controller_secret_error copy (TODO(port) C36).

Collaborators (`convert/mihomo_generate_env.go`):
- `MihomoEnv` (config domain; installed by `mihomoenv.Wire`, listed in `server.wirePackages`): `Converter` (substore, resolved at call time), `ProviderInterval` (refresh.ValidateRefreshInterval with `Mihomo 订阅同步间隔`), `LoadSubscriptionCache` (substore), `DownloadedText` (repo shared JSON at refresh.DownloadCachePath), `PrepareProviderBundle` (provider). Unset fields fail closed with `*HookMissingError`.
- `MihomoHooks` (other domains; `RegisterMihomoHooks`, register nothing from config):
  - `ApplyLinuxGateway` — **provided by agents A19/A21** (`linux_gateway.apply_linux_gateway`, which first calls **routers** `router_split.check_generation`). Default: no-op when `mihomo.linux_gateway` is absent/null and there is no `mihomo.router_split` marker (exactly Python's no-op), else `*HookMissingError`.
  - `GatewayEnabled` — agents A19/A21 (`gateway_settings(config)['enabled']`; get_mihomo_ruleset_downloads, takeover_mode). Default: false when `linux_gateway` absent/null, else `*HookMissingError`.
  - `FakeIPNetworkError` — agents A19/A21 (`fake_ip_network`, base settings `dns.fake-ip-range`). Default `*HookMissingError` when the value changed.
  - `MihomoDNSLoop` — agents gateway onboarding (`gateway_onboarding.mihomo_dns_loop`). Default `*HookMissingError` when a DNS upstream changed.
  - `IsFullTun` — routers clients/split (`router_split.is_full_tun`). Default false without a `router_split` marker, else `*HookMissingError`.
  - Call order as Python: generation → `ApplyLinuxGateway` after rule-providers, before rules; base settings → fake range check after the type checks, DNS loop last.

Tests:
- Differential corpus `testdata/gen_mihomo.py` → `mihomo_corpus.jsonl.gz` (`mihomo_corpus_test.go`, external package): 300 agent_context rows, 6006 random + fixed profiles (config_template.json, default config, empty) → **4076 generated YAML byte-identical**, 1930 error rows equal (class + message; crash classes by class), reports and mutated profiles equal; 908 ruleset + 570 provider download lists; 6000 `validate_base_settings` (random docs/previous/reserved/local addresses, fake A19/onboarding hooks mirrored); helper rows. **13908 rows, 0 differences.** Covered: all group types, follow groups, filters incl. look-around, chains/dialers (valid relay topologies), all rule types, rule sets (library/url/content/mrs/github proxy), subscriptions + aggregations with caches (hosts metadata incl. domain mappings, dialer records, download-cache fallback), EasyTier, region probe, MosDNS hosts sync, custom base YAML (anchors/aliases, timestamps, binary, invalid YAML, non-mapping roots, ipv6 flags, find-process-mode), provider intervals, config tokens, server domains. Not in the corpus: preflight (provider), Linux gateway enabled (hook), non-string-key base YAML (yamlx limit).
- **Recorded replay of the Python suite** (`testdata/c18_record.py`, a pytest plugin, → `mihomo_recorded.jsonl.gz`; `mihomo_recorded_test.go`): every `generate_mihomo_config` / `get_mihomo_ruleset_downloads` / `validate_base_settings` call made while the whole backend suite runs (12552 passed under the recorder) is stored with its arguments, what the collaborators answered (Sub-Store conversions incl. reused node_conversions, subscription caches, download-cache reads, provider preflight result, Linux gateway before/after, gateway setting, fake_ip_network, mihomo_dns_loop), the result/exception, mutated profile and report; Go replays it with those answers and compares bytes. **1566 distinct calls from 1115 Python tests in 66 files, 0 differences**; 14 calls skipped because their tests monkeypatch `convert_node_to_mihomo` (ported by hand). This covers the generation halves of every C18 test file and of the deferred cases recorded elsewhere: `test_dynamic_proxy_chains.py` 17/18 tests, `test_group_exclusion_generation.py` 7/8, `test_proxy_chains.py` 12/18, `test_mihomo_base_settings.py` 20/21, `test_proxy_server_dns.py` 16/22, `test_subscription_dialers.py` 9/12, `test_subscription_hosts.py` 13/22, `test_mihomo_easytier.py` 7/13 (round trips), `test_dialer_*` (combined export 9/9, snapshot 3/3, staging 4/5, proxy 12/23, review fixes 9/16, round2 8/10, rendered bundle 4/4, standalone 4/4), health generation (`test_health_review_r1/re1/re2`, `https_defaults`, `persistence`), `test_udp_fallback.py` 4/15, `test_safe_url_queries.py` 2/11, `test_region_check.py` probe entry, `test_subscription_refresh_policy.py` mihomo interval, `test_converter_review_regressions.py` 3/7, plus route/agent tests that generate (setup flow 43, gateway installation 54, gateway onboarding 23, linux gateway 11 + draft 22, …). The remaining tests of those files test other modules' functions (C09/C11/C12 helpers, Surge/Loon C19/C20, MosDNS C21, routes C34, agents) — their route/HTTP halves stay deferred to C30/C31/C33/C34/agents.
- Hand-ported (`mihomo_ported_test.go`): `test_proxy_chains.py::test_invalid_chain_metadata_is_rejected_before_conversion` (12 cases; counting Sub-Store instead of the patched converter), `::test_conversion_failure_cannot_silently_remove_chain_topology` (A, Chain), `test_dynamic_proxy_chains.py::test_native_override_cycle_is_not_hidden_by_original_provider_graph`, `test_group_exclusion_generation.py::test_candidate_filter_preserves_records_order_and_empty_include`; Go-side: hook defaults (exact no-op vs fail closed), `TakeoverMode` with hooks, preflight receives the logical profile and the finished document (errors abort; unwired fails closed), `mihomoenv` provider interval, `GenerateMihomoDocument`, template freshness.
- Not ported here: `test_mihomo_base_settings.py::test_dns_loop_helper_keeps_the_gateway_wizard_rules` (tests gateway_onboarding.mihomo_dns_loop → agents); `test_agent_ports_and_takeover_are_reported_and_enforced`, `test_save_route_rejects_and_keeps_the_saved_base` (routes → C34; their validate calls are in the replay; `AgentContext` is covered by 300 corpus rows).

Decisions / differences from Python:
- Debug log lines with Python sets print them in insertion/sorted order (Python's set order is hash-seed dependent).
- Base YAML with non-string mapping keys (`1: x`) cannot be loaded into jsonx (yamlx `NonStringKeyError` returned); Python would generate. Provider/rule-set names that are not strings → TypeError-class error (Python writes a non-string key).
- `subscription_hosts_index` without the file-stamp memo (reads the cache each time; same result).
- `add_dialer_groups`' node conversion errors cross `subs.AddDialerGroups` as a panic recovered by the generator (subs' `ConvertNode` callback has no error result).
- `AgentContext` keeps int ports that fit int64.

Commits: 7b0fb0ea (code), e879ebf9 (corpus), a7969b4c (recorded replay), 1d839e60 + bcaa6e83 (hand-ported tests, agent_context rows), cbaeef91 (`mihomoenv.Wire` in server.wirePackages).

Left: nothing for the port. For other modules: C34 registers the generation routes (C18 owns none) and ports the route halves listed above; agents A19/A21 + routers register `ApplyLinuxGateway`/`GatewayEnabled`/`FakeIPNetworkError`/`IsFullTun`, agents gateway onboarding registers `MihomoDNSLoop`; C36 replaces `controllerSecretError` with its export.

Reviewer hints: walk `generateMihomoDocument` against generate_mihomo_config (set semantics via `orderedSet`/`hashID`, order of `.get`/`[]` crash points, debug lines evaluated only when DEBUG is on — the KeyErrors they could raise are raised later at the same place); `mihomoRulesetURL` (shared by downloads); `appendURLQuery` vs parse_qsl/urlencode(quote_via=quote); `same()` vs `_same` (type identity, dict key sets, NaN); `ControllerScope` (3.11.17 IPv4-mapped loopback); `AgentContext` order. The recorded replay trusts the recorded collaborator answers — the collaborators themselves are verified in their own modules.

Pitfalls: the shared tree's `internal/convert` may not build while the C12 reviewer edits it — verify on a private detached worktree (`git worktree add --detach ~/program/configflow-go-c18 HEAD`, copy the C18 files, `rt` from there).

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/convert; go vet ./internal/convert/... && go test -count=1 ./internal/convert/...'`
- corpus: `rt --back server/internal/convert/testdata/mihomo_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/gen_mihomo.py 6000'` (deterministic seed; ~20 s)
- recorded replay: `rt --back server/internal/convert/testdata/mihomo_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c18rec; C18_RECORD_DIR=/tmp/c18rec PYTHONPATH=.:server/internal/convert/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 --dist=worksteal -p c18_record -p no:cacheprovider; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/c18_record.py merge /tmp/c18rec'` (~3 min)
- last run (HEAD bcaa6e83, clean detached worktree): `go vet` clean; `go test -count=1 ./internal/convert/... ./internal/server/ ./internal/provider/` ok; `-race` ok for convert.

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

Evidence: `internal/discovery` exists with two functions **ported early by A05** (2026-11-15, commit 5fb71710; see PROGRESS-agents A05): `RegistrableDomain` (= `domain_discovery.registrable_domain`, on `subs.ExtractDomain` without private suffixes; test `discovery_test.go` = `test_registrable_domain_uses_public_suffix_list`, plus 45 `_target_key` reference inputs in `agents/metrics`) and `ProviderRevisions` (= `domain_discovery_service.provider_revisions`, with private partial copies of `read_settings` (only the keys it uses, plus the TypeError of a non-iterable `ignored`/`history`), `ruleset_problem`, `_references`/`ruleset_info` (`active` only) and `_served_rule_content`; used by the Agent traffic-report route; covered by `api/agents.TestTrafficReportReturnsProviderRevisions` and the agents differential). No route registered. **Reviewed with A05 (2026-11-15, verified):** both functions walked against Python; `RegistrableDomain` checked through 158 `_target_key` reference inputs (`agents/metrics/a05_reference.json` + `traffic_reference.json`), no finding; `ProviderRevisions` no finding — the porter's sort_keys/unhashable-id differences are unreachable because `_validate_document` forces str ids and unique non-empty str names in `rule_library`. C37 still has to review the rest of the package when it lands.

Missing / deferred: Everything else in the files below. When porting `read_settings`/`ruleset_info` fully, make `ProviderRevisions` use them (same behaviour). Already ported by A05 from `test_domain_discovery.py`: `test_clean_report_*`, `test_store_merges_reports_and_expires_old_days`, `test_store_caps_hosts_per_day_keeping_heaviest`, `test_registrable_domain_uses_public_suffix_list`, `test_report_requires_token_and_enabled_switch`, `test_report_with_login_enabled_rejects_invalid_agent_credentials`, `…_retains_switch_and_payload_checks`, `…_rejects_unknown_agent` (do not port again). The traffic store is `internal/agents/trafficstore` (`Get`, `Load`, `LoadStatus`/`StatusOf`, `SplitRouteKey`); `internal/discovery` may import it but must never import `internal/agents/metrics` (which imports discovery).

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
2. ~~C02~~ verified 2026-11-16 (yamlx on the PyYAML port).
3. ~~C08~~ verified 2026-11-16.
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
5. **Two YAML loaders** (noted 2026-11-15 after C14): `internal/yamlx` (yaml.v3 + PyYAML resolver) and
   `internal/rulefetch/pyyaml` (a port of PyYAML 6.0.1's pure reader/scanner/parser/composer/resolver,
   needed for PyYAML's exact error texts and marks). The C02 reviewer evaluates moving the PyYAML port to a
   shared package and building `yamlx.Load*` on it, which would also close C02's open items (`1,000` in
   flow collections, error texts) and the non-string-key limits; if adopted, re-run every yamlx consumer's
   tests and corpora.
6. **C10 verified with six yamlx-dependent rows** (2026-11-15): confirmed by the coordinator — they are
   C02's (yamlx) defects, not C10's: non-string mapping keys (`123:`, `1.5:`, `true:`, `~:`) make yamlx fail
   where PyYAML skips/keeps them, and `name:\tx` (tab before a value) is accepted by yamlx but rejected by
   PyYAML (new). `subs` lists them in `yamlxDiffers`; when C02 fixes yamlx (see item 5), that test flags
   the change and the rows must then match Python.
7. **Open item for C07 (found by the C15 porter, 2026-11-15):** `tasks.History` passes a string
   `run_count` (hand-edited `task-status.json`) through unchanged where Python converts it with `int()`;
   C15's rows convert it themselves, but the backup, metrics and agent rows still show the string. Fix
   in the C07 review. (The related `taskstatus.Record` / `ParseTime` defects were sent to the C02 reviewer.)
8. **Shared Python-semantics helpers (after the C02 review, 2026-11-16):** `internal/pystr` (`Lower`,
   `IsSpace`, `Int`, `Float` on CPython 3.11 tables), `urlx.RequestsPrepareURL`, and `yamlx` built on
   `internal/pyyaml` (`LoadOptions.KeepNonStringKeys`, `LoadCSafeFirst`). Private copies elsewhere must be
   switched to them so behaviour cannot drift: `pyLower` copies in `subs`, `substore`, `rules`, `convert`,
   `agents`, `agents/metrics`; `subs.pyIntStr`; `repo.pythonFloat`; `substore.PrepareURL`; `repo/compat_health.go` → `health/check.ValidateHealth` (no cycle); C14's accepted
   non-string-key case and C12's loose YAML loader (`KeepNonStringKeys`). Each module's reviewer does the
   switch for its own package (or the next porter touching it), re-runs that package's corpora, and notes
   it in the section. Before any domain differential, `grep` for remaining copies must come back empty.
9. **`internal/api/nodes`** (created by the C16 porter for the latency routes): C30 adds its node routes
   to the same package's `Register` instead of creating another one.
10. **Defects found by the C17 porter in other modules (2026-11-16)** — fix in those modules' reviews:
    C13 `substore.ParseProxiesFromYAML` rejects mappings with non-string keys where Python accepts them
    (use `yamlx` `KeepNonStringKeys`); C12 `convert.ValidateEmitted` raises a different error than Python
    for a non-dict provider definition. C17's corpus allowlists these two rows; remove the allowlist when
    fixed. Also `provider.flattenMapping` is a private copy of an unexported `yamlx` helper — export it
    from `yamlx` and switch (item 8).
11. **Leftovers from the C12 review (2026-11-16):** (a) `convert.InspectStructuredProxyString` keeps the
    yaml.v3 loose loader only as a fallback for node strings with a recursive alias (needed by repo's
    verified `TestCompatProxyStringTimestamp`); removing it needs a `yamlx` option for recursive aliases
    (owner C02 — next C02 touch). (b) `convert/pyhelpers.urlsplit` stays a private copy until `urlx.Split`
    matches Python for bracketed non-IP hosts (`https://[github.com]/`); switch when that `urlx` fix lands.
    (c) `repo`'s `TestWriteWaitsForReleasedFileLock` showed a data race in test code once under `-race`
    (goroutine vs deferred close); fix in the next repo touch.
