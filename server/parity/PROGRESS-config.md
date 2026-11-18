# PROGRESS — domain `config`

Domain `config` (WORKFLOW.md): foundation (old packages #0–#5) and configuration/generation (#6–#19). **119 Python files, 39628 lines, 37 modules** — 11 missing, 3 partial, 8 ported, 15 verified (C01, C02, C03, C04, C08, C09, C10, C12, C13, C14, C15, C16, C17, C21, C28). Only reviewers set `verified`.

Planning audit: 2026-11-15, worktree `configflow-go`, branch `go` at 9783acd1 (plus uncommitted work of the subs and agents agents). File list and other domains: `server/parity/FILES.md`.

## N7-3c — MosDNS/provider YAML readers (ported)

Branch `go-n7-3c`; call-site and command record:
[progress/n7-3c.md](progress/n7-3c.md), every caller's input counts:
[progress/n7-3c-gate.md](progress/n7-3c-gate.md). No old loaders are deleted.
MosDNS custom base/upstreams/payload/archive references and provider proxies/source
positions now use Parse/Node/Decode. JSON adaptation keeps scalar proxy keys through
shared StringifyScalarKeys; composite proxy keys take the existing Chinese failure
path, opaque root keys are ignored. MosDNS retains ordinary syntax fallback and
explicitly refuses policy/unsafe aliases. Native source tags and free-form keys
are kept; alias sharing survives generated forwarder reuse and displaced anchors.

**20972 actual input occurrences**, one primary category each: equal 15400;
native diagnostics 2965; D1 29; YAML 1.1/timestamp 586; reader policy 64;
non-JSON tag shape 556; unsafe aliases 54; 原生解析器语法范围 1255;
复合映射键：Node 保留 6; 未知标签：Python 拒绝 → Go 接受 57; deep ordinary 0.
The shared 1591/735 core gate has 2326 classified rows (full table at the link).

C21's 47072 rows compare actual Python output structurally with **no blanket
unholdable-value exception**; C28's 87 YAML inputs and C21's 20 review inputs are
included. Every YAML entry has depth-40 amplification/self-reference and native
10000/10001 nesting checks. Final format/vet/race/full, all 12 live difftest
packages and eleven 60s fuzz targets pass on .32/.64; proddiff has
coverage_regressions=0/new=0/stale=0. After N7-3a rebase, C17 corpus parse rows
164–166, 179–181 and 209–211 all pass through public ParseProviderProxies.
Context, source labels and all non-YAML effects remain strict. The five explicitly
authorized aggregation diagnostic comparisons retain exact 400/owner/source/native
diagnosis and strict other fields/storage; no aggregation runtime code changes. No pending integration.

## Conventions

- Status: `missing` (no Go code), `partial` (some code; the section lists exactly what is missing), `ported` (code and tests exist, nothing obviously missing; **not reviewed**), `verified` (set by a reviewer only, with commit ids and checks).
- A test file is listed under the module that owns its subject. Integration tests (`create_app()` + routes) often also need later modules: the porter ports the cases that can run now and lists the **deferred** cases in the section of the module that makes them runnable (as done below for the foundation). `ported` allows deferred cases if they are listed.
- Python test counts are `def test*` per file; "Go n/m" counts cases found in Go tests by name or by the `test_x.py::test_name` comments. Corpus tests (Python-generated rows under `testdata/`) cover behaviour without matching names.
- Routes: rows of `server/parity/routes.tsv`. `/api/profiles/<profile_id>/…` wrappers in `routes/profiles.py` only set the profile and call another blueprint's function; they are listed under the module that owns that function, whose `api` package registers them (PORTING rule 8).
- Python lazily imports in cycles (e.g. converters ↔ provider delivery ↔ route files, the whole OxiDNS cluster). Go needs a DAG: the prerequisite order below breaks the cycles; when a module needs a function of a later module, put a `compat.go` copy (`TODO(port)`) or an interface/hook and list it here.
- Cross-domain calls (config → agents/routers code) are listed per module; they are the main blockers for C18, C27, C34, C36, C37.

## N7-3d — frozen-reader gate performance (ported; batch-2 acceptance)

- Implementation00e741661 selected into batch-2. Eight callers retain all
  original inputs but load only a compact derived physical-audit index. The
  helper alone streams and actually replays every21343 frozen source, unknown0.
- .64 gofmt/vet passed; full helper race113.518s (all-source replay107.22s),
  persistent and actual-file entry-tamper negative controls passed. Nine full
  package race results completed. Coordinator requested stopping the remaining
  author run; final installer/oxidns/provider/routers coverage belongs to the
  coordinator's batch-2 gate. The first3 also naturally completed in this run.
- Timings, historical scope/load caveats and direct discovery comparison:
  [progress/n73-gate-perf.md](progress/n73-gate-perf.md).

## N8-7 — early consumers, first batch (ported; final acceptance running)

## N8-7 — early consumers, first batch (ported; coordinator acceptance pending)

- Branch go-n8-7 from a08c4cc2, finally rebased onto go@c31fbf306. Package
  commits are listed in the full report: [progress/n8-7.md](progress/n8-7.md).
  Original full race had one obsolete Unicode fixture failure, closed by the
  corrected complete api/agents race; first proddiff new=0/stale=0. Final
  all-difftest/proddiff stages never obtained slots within the authorized
  15-minute queue limit. Final merged-tree cf-gate remains pending.
- Native file codec, typed JWT claims, shared bounded native redaction. Real Python
  file interchange, JWT bidirectional interop and HMAC negative controls.
- Coordinator split: taskstatus waits for O3-6c; tasks, notifications (+store),
  weblistener and api/{logs,stats,notifications,tasks} wait for C07. These are
  N8-7 second batch, not claimed migrated here.
- Persisted non-finite values individually become null; raw records survive;
  metrics projections treat them as missing. Native numeric lexemes stay exact.
  Ledger/O14 cover Python rollback arithmetic TypeError and 24-hour retention.

## N7-3d — final reader/legacy cleanup (complete; coordinator verification pending)

Branch go-n7-3d was initially stacked on N7-4; now rebased onto go@28e6e0c12
(includes cb5732ec C29/L1/A13). Full .32 gate PASS:91 race packages,14 difftest
packages,13 fuzz targets60s,proddiff new0/stale0/coverage0,aggregation-provider2/2.
Coordinator requested only fmt/vet/targeted YAML race after this rebase; all
passed on .64, including the new L1 boundary regression. Scope includes convert readers,
importer/rules bare decoders, gateway's remaining legacy readers/Mapping codec,
the proddiff loader and provider/deploy diagnostic interfaces, then removal of
all old loader/pyyaml references and test-helper consolidation/grep gate.
OxiDNS's two readers belong to C29. Chinese contexts stay unchanged; native
positions are audited and any deltas counted separately.

First buildable step: rules matcher now uses Parse with unchanged payload/source
selection; real native-reader comparison equal940/native-diagnostics41,unknown0.
The complete rules tests pass (.64 1.755s), including depth40 and self-alias
rejection through both extraction and public Match. Importer's reader now uses
Parse/ToJSON and preserves retained Node sharing instead of recursive expansion;
real14-input gate equal12/native-diagnostics1/诊断位置1,unknown0. Original importer corpus
and extracted-subtree budget regressions pass (1.499s). Other implementation and
final acceptance remain pending. Provider/deploy diagnostic hooks now carry native
errors/one-based coordinates. A frozen old public boundary reference checks235
complete visible outputs (193 refused,147 positioned),position changes0/unknown0;
provider/A11/A12 full tests and A13 diagnostic tests pass.
A19 readers now use Parse with no generation/plan changes: base3739/payload5161
sources,unknown0. All7494 corpus rows pass;4 approved native-loader differences
have real Python expectations. Editor/enable checks explicitly reject budget
excess, small chains succeed. Full gateway suite PASS8.645s.
Rebased on N7-4 integration at e980d402. Aggregation's typed JSON→YAML bridge
now uses FromJSON/Marshal; surrogate/scalar-type/special-character regressions
and C31 HTTP differential pass (.64 27.400s). Final fuzz/proddiff pending.
See [progress/n7-3d.md](progress/n7-3d.md).

## N7-4 — native YAML writers (complete; coordinator verification pending)

All runtime writer callers now construct Nodes/typed values and call Marshal:
convert, installer, gateway, provider, refresh, routersplit and the proddiff
adapter. MosDNS/rulefetch/pushdiff already used native emission; their shared
adapter and regressions are covered. Removed dump.go/emitter.go/types.go and all
old writer API calls. convert/mosdns share the sole temporary yamlbridge.Encode;
legacy Timestamp/bytes/Mapping loader values remain only until N7-3d.

Core writer coverage is 1,395 independent Python semantic references. Mihomo's
111 old writer type bugs use real Python safe_load graphs (strings56/large
integers22/timestamps33); all other outputs compare Parse/Equal. Installer's
writer-only oracle reruns unchanged Python business code, replacing only
`yaml.dump`/`safe_dump`, with persisted typed input graphs and injection bytes.
8,623 verified source rows: layout8,127/string21/Unicode436/unchanged39,
unknown=0; Unicode row counts NEL345/LS53/PS47 overlap. All three characters
round-trip through generated Compose with exact bind sources. Complete results
remain strict outside YAML, including shell strings and archive members.

Agent push diff has no semantic edit for writer-only changes; later credential
edits stay redacted. The A19 16-level binary graph now explicitly fails the
shared alias budget with empty output; small shared graphs succeed. Approved
Python bug fixes and the single D2/O14 generation byte change are in the ledger.
Old golden files remain unchanged. The .64 gate passed all88 race test packages,
all12 then-existing difftest packages, all9 fuzz targets at60s each, and proddiff
new=0/stale=0/coverage_regressions=0 (2831cases/462compares/23selfchecks).
Rebased cleanly onto go@64815165 (provider load fix and N8-6). Per coordinator,
post-rebase acceptance reran full vet plus all16 changed test packages (.64),
the exact tagged provider manifest regression, and all13 enabled difftest
packages (.32, including new provider/difftest); all passed. See
[progress/n7-4-writers.md](progress/n7-4-writers.md) for exact commands and output.
N7-3d follows after merge; its OxiDNS readers are owned by C29.

## Step B findings — config boundary repairs (ported, 2026-11-17)

The coordinator's twelve decisions are tracked individually in
[config-findings-fixes.md](config-findings-fixes.md). Intentional differences are
listed in [behaviour-changes.md](behaviour-changes.md); existing N8 decisions stay
intact. Every finding was reproduced or confirmed on .63; final .32 validation
includes all changed packages, all differential packages, the complete agents
differential twice, and proddiff. C35 auto-backup activation and C30 BOM handling
were already fixed and have been rechecked. A11 still owns file positions and
excerpts; provider ownership/source labels now compare strictly.

Final `.32`: gofmt/vet pass, changed-package race and full suite pass; all nine
Flask differential packages pass, including agents twice (229.828s). Proddiff
2831 cases / 415 comparisons, coverage_regressions=0, new=0, stale=0. Short-link
HTML-to-JSON errors are explicitly verified; existing A18/F1 remains recorded,
with only its error representation fingerprint updated. No route coverage was removed.


## N7-3a — native subs/substore YAML readers (ported; final gates passed)

All YAML reader sites in subs/parser/compat/hosts/dialers and substore/client use Parse/Get/AllPairs
and the one shared ProjectJSONKeys boundary. No legacy Load*/Mapping/Lookup/Timestamp/Compose in
these packages; no runtime Clone. The bridge retains alias sharing and checks full/subtree budgets;
40-level alias and self-reference regressions cover each entry. N8-9 removes the jsonx transition.

Migration gate: 4845 real caller inputs (parser3664/dialers610/hosts494/substore51/detection26), plus
1591+735 old core texts; unknown=0. Real Python oracle patches only YAML loaders with separately
recorded native values and complete pre-projection tag/alias audit. Business code is unchanged;
expected changes / classified: subs209/209 out of7494, substore14/14 out of146,
convert29/29 out of34697 (authorized corpus-only extension; product code unchanged). Per-class input counts,
all eight shared yamlx commits, approved native behavior, exact commands and final gates are recorded
in [progress/n7-3a.md](progress/n7-3a.md). Non-YAML golden assertions remain strict. Old loaders and
pyyaml stay until a/b/c/d integration cleanup; no public route added. Independent review pending.

Final `.64`: gofmt/vet silent; yamlx/subs/substore/convert race and full `-p=4` pass;
all ten enabled difftest packages pass; six fuzz targets ×60s pass; proddiff 2831 cases/415 comparisons,
23 self-checks PASS, coverage_regressions=0, `new=0 stale=0`. Rebased onto local go; no push.

Per-category input counts (overlap allowed; callers=4845, core=2326, unknown=0):

| Category | Caller sources | Core sources |
|---|---:|---:|
| D1-duplicate | 74 | 138 |
| alias-recursion | 1 | 6 |
| binary-text | 8 | 51 |
| equal | 3960 | 632 |
| explicit-scalar-native | 0 | 5 |
| explicit-scalar-validation | 2 | 131 |
| implicit-schema | 96 | 153 |
| merge-source | 2 | 5 |
| multiple-documents | 7 | 17 |
| native-diagnostics | 573 | 631 |
| native-format | 9 | 49 |
| native-json-key-policy | 18 | 0 |
| native-parser-A | 0 | 6 |
| native-parser-B | 0 | 7 |
| native-parser-C | 0 | 2 |
| native-parser-surrogate | 57 | 0 |
| native-parser-tab | 3 | 3 |
| non-json-tags | 8 | 140 |
| non-string-keys | 9 | 102 |
| nonfinite-text | 10 | 41 |
| tag-shape | 0 | 243 |
| timestamp-text | 10 | 67 |

The separate surrogate syntax class is non-JSON YAML flow only: standard JSON paired UTF-16 escapes
retain emoji through encoding/json first. Read-only production snapshot high-surrogate occurrences=0
across all scanned categories; no stored texts were rewritten. Expected changes / classified remain
subs209/209, substore14/14, convert29/29. API and non-YAML golden behavior remains strict.

## N8-6 — native HTTP request binding (ported; final gates passed)

Ctx.Bind / BindSilent(bool) plus literal/form-preserving httpx.Number, native duplicate whole-value
replacement, media/BOM/empty/invalid handling and DTO rules are implemented. Only R226 tasks interval
and strict POST /api/auth/login migrate; other X04 validators remain for N8-7+. Correct login,
incorrect credentials and wrong credential field types keep their statuses/Chinese messages. Named
bad-login vectors explicitly assert Python500→Go400/415, and migrated non-finite requests reject400.
Legacy JSON helpers stay unchanged; N8-7+ callers record their own non-finite transition. A04/A05
heartbeat/history seeds and traffic ingestion are inventoried, not changed. Detailed design mapping,
DTOs, HTTP-shell scope, exact commands and output: [progress/n8-6-httpx.md](progress/n8-6-httpx.md).


Final `.32`: gofmt/vet silent; changed-package race (httpx/core-auth/API-auth/tasks) and full `-p=4`
pass; all 12 enabled differential packages pass; FuzzBindNativeJSON 60s/632396 exec PASS;
proddiff 23 self-checks PASS, 2831 cases/435 comparisons, coverage_regressions=0, `new=0 stale=0`.
118 shell differential vectors include 17 explicitly asserted migrations. No baseline change.

## N7-1 — native yamlx core (ported, 2026-11-17)

New yaml.v3 Node API lives alongside the untouched legacy API; no caller/oxidns migration or pyyaml
deletion in this step. Commits: `5e0d2f97` core, `885bddde` JSON/differential/fuzz coverage, `bc7948a6`
scalar-text/type-error clarifications. Full handoff,
exact commands and output: [progress/n7-1-yamlx.md](progress/n7-1-yamlx.md).

257 synthetic documents + 15 string round trips compared to live PyYAML; the 1591/735 legacy corpora
remain green. Final .32: gofmt/vet silent, yamlx race 9.054s, full Go suite passes; both fuzz targets 60s
(260513/99450 executions); updated proddiff 23 self-checks PASS, 2831 cases, 415 comparisons,
`baseline: new=0 stale=0`. Non-test yamlx lines 3109 → 4225, no new dependency.

Coordinator-approved boundaries are in [design/n7-n8.md](../design/n7-n8.md) §2.2: D1 duplicate rejection
with key/line; recursive and excessive aliases rejected; merge semantics; timestamp decoding by destination;
non-finite YAML numbers → JSON strings; typed mismatches wrap ErrType and *yaml.TypeError. Fixed native
encoder issues: document-wrapper comments lost and empty null keys changed to strings. No open N7-1
finding; old/new corpus classification and invalid explicit tags (e.g. `!!null [a]`) are explicitly N7-3
gates (§5.4). Editor is N7-2 (Opus-reviewed, verified: review-n7-2.md); consumers/writers migrate in
N7-3/N7-4.

## O3-1 — lockx primitives (verified, 2026-11-17)

Branch `go-o3-1`, code commits `11d187c8` and `9cae8b68`: new Mutex/Keyed/RWMutex/Flight with L1–L3
context order checker; no caller migration or file I/O. L4/L5 remain sync.Mutex. Coordinator's API
clarifications are in `design/o3-concurrency.md` §2.2; full handoff, exact commands and output are in
[progress/o3-1-lockx.md](progress/o3-1-lockx.md). Final .32 gofmt/vet silent, race-count3 **8.067s**,
full Go suite passes (lockx 0.332s); proddiff 23 self-checks PASS, 2831 cases/415 comparisons,
`baseline: new=0 stale=0`. Main tests assert zero violations; negative checker/panic logging tests use
child processes. Includes 10k-key cleanup, concurrent stress, explicit limit/try semantics, cancelled
waiter drainage and shared-work lifetime tests. Non-test lines 0 → 568, no new dependency. No open
O3-1 finding; independent review pending. O3-2 integrates Spawner; O3-6 migrates consumers.

## O3-2 — lifecycle v2 (verified, 2026-11-17)

Branch `go-o3-2` stacks on O3-1: task registry/clock, restartable Runtime, tracked Spawn/scopes/Flight,
manual rounds, first-request gate, phased bounded shutdown and injected HTTP Drain. Legacy adapters
remain; only subscription-cache-prune migrates to Startup, five periodic owners stay for O3-4. Proddiff
uses tagged helpers/mode with no source overlays or work-name whitelist; difftest events are tag-only.
Full handoff: [progress/o3-2-lifecycle.md](progress/o3-2-lifecycle.md). Final rebased .32 gofmt/vet,
three-run lifecycle/clock/lockx race, both tagged builds, `-p=2` full suite and all nine explicitly enabled
`configflow_difftest` packages pass. Proddiff tool tests 31 OK; tagged proddiff 23 self-checks PASS,
2831 cases/415 comparisons, `new=0 stale=0`. Preserves both restart regressions, legacy/periodic
cross-generation exclusion and Configure's ownership of unfinished old jobs. The known importer wall-clock
probe timeout reproduced in the first parallel suite; its original-threshold focused recheck and the
reduced-concurrency full rerun pass. That probe is owned by go-fix-importer-bounds and was not changed here.
Coordinator clarifications are in design §2.3; independent O3-2/O3-3 review is still scheduled.

## O3-3 — instance/startup/shutdown integration (verified, 2026-11-17)

Five-lock instance ownership precedes log/repository initialization; shared ResolveDataDir makes
absolute, home and symlink aliases use the same lock. Ordered synchronous Startup precedes listening;
httpx signals profile/auth-passed requests. Main injects HTTP Drain, splits first/final metrics saves,
bounds shutdown with the default 8s grace, closes resources/releases ownership and supports immediate
second-signal exit. Three old loop lifetime flocks removed now; their timing/Periodic migration stays
O3-4. Production has no loop-disable switch. Implementation and approved boundaries:
[progress/o3-3-startup.md](progress/o3-3-startup.md), [design notes](../design/o3-startup-shutdown.md).

Post-fix .32 fmt/vet, required race plus instance/repo, both tag sets, `-p=4` full suite, all ten
currently enabled tagged differentials (including N7-2 subs), three synthetic corrupt-startup checks
and 31 proddiff tool tests pass; proddiff 23 self-checks/2831cases/415comparisons, `new=0 stale=0`. Actual SIGTERM idle/publishing/
blocked Drain and second-signal cases pass; real Python-compatible and alias ownership rejection pass
with zero scheduled egress. Author status is ported; Opus review with O3-1/O3-2 is still required.
Latest-go composition check also passes with N7-2; no O3-3 production source changed during rebase.
Final upstream-only importer/oxidns test fixes were rechecked on .32 (2.477s/73.814s), with fmt/vet clean.

## O3-4 — periodic owner migration (verified, 2026-11-17)

refresh 30/30 (Keyed L1 claims shared with run now, ≤3 joined workers, rows from `Held`), discovery
120/600, OxiDNS rankings 60/60, metrics 30/30 + Wake (OnStart, `Signal.Clear`), Sub-Store 0/5 when built
in; `ClockFrom`, shutdown-aware `Detach`, `Periodic.OnStart`. Handoff: [progress/o3-4-loops.md](progress/o3-4-loops.md).

## O3 joint review (O3-1..O3-4 verified, 2026-11-17)

Fresh Opus review of O3-1..O3-4 together: [review-o3.md](review-o3.md). 0 high, 2 medium (Detach inside
shutdown hooks returned the cancelled job context; request-detached work never got the shutdown
cancellation → `lifecycle.BindRequest` in httpx), 4 low (second signal during the first save ignored as in
Python; stop signal ends the instance wait; RequestSeen fast path; ranking slot-pause production test).
All fixed with regression tests and mutation-checked; zero open findings, so O3-1..O3-4 are `verified`.
Rules for O3-5..O3-8 are in the review's last section.

## Known accepted differences (do not rediscover)

- jsonx: lone UTF-16 surrogates cannot be represented in Go strings (documented in `jsonx/doc.go`); `test_json_response.py::test_surrogate_strings_keep_normalization_before_redaction` depends on it.
- Regex: `\N{name}` escapes and repeat counts > int32 → `*rules.UnsupportedRegexError` (filters with such patterns are not applied; validation accepts them like Python). Backtracking matches stop after 1 s and count as no match.
- yamlx (since review C02 on the PyYAML port, `internal/pyyaml`): values, exception classes and texts are PyYAML's. Values jsonx cannot hold are reported after construction: non-string keys → `*NonStringKeyError` (or `*yamlx.Mapping` with `LoadOptions.KeepNonStringKeys`), recursive aliases and non-empty `!!set`/`!!omap`/`!!pairs` → `*UnrepresentableError`. CSafeDumper output (size measurement in `push_diff.py`) not reproduced.
- Mihomo YAML editor: N7-2 replaces `subs/commented` with `yamlx.Edit` and output verification; C11 records acceptance and the deferred real-route checks.
- Web listener: the Go server listens itself and serves the SPA (replaces nginx); nginx-specific behaviour has no counterpart (C07).
- Not ported by decision (packages.md): legacy pre-SQLite migration, `models/`, `startup.py`, `http_server.py`, `http_queue.py`, `agents/node_test_dns_cleanup.py`, `metrics_history.LEGACY_DIRS`; `subscription_parser.parse_subscription`/`parse_clash_yaml` (no caller). Tests only about those: `test_split_layout_migration.py`, 25 cases of `test_config_storage_migration.py`, `test_http_server.py`, `test_startup.py`, `test_http_queue.py`, `test_node_test_dns_cleanup.py`.

## Module table

| Id | Module | Py lines | Status | Prereqs | Routes |
|---|---|---|---|---|---|
| C01 | jsonx, store | 564 | verified | – | – |
| C02 | infra helpers | 828 | verified | C01 | – |
| C03 | repo storage core (config_repository.py part A) | 1576 | verified | C01, C02 | – |
| C04 | repo public API (config_repository.py part B), config.py | 1373 | verified | C03 | – |
| C05 | profile context, OxiDNS settings normalizer | 497 | verified | C04 | – |
| C06 | HTTP shell, auth, settings routes | 1028 | verified | C05 | 22 (22 done) |
| C07 | logs, stats, tasks, notifications, web listener | 1393 | partial | C06 | 9 (5 done) |
| C08 | rule syntax, matcher, rule utils | 1101 | verified | C02 | – |
| C09 | matcher cache, group candidates, reorder, strategy refs, connection rules | 708 | verified | C08 | – |
| C10 | manual node parser, subscription hosts/filter/dialers | 1242 | verified | C08 | – |
| C11 | proxy chains, commented Mihomo editor, listeners, proxy-server DNS | 855 | verified | C10 | – |
| C12 | converter common, Mihomo node conversion, dialer references, EasyTier | 1196 | verified | C09, C11 | – |
| C13 | Sub-Store client, runtime, subscription cache, dependencies | 1193 | verified | C12, C10 | 3 (3 done) |
| C14 | remote rule fetch, ruleset content, remote sources, website identity | 1202 | verified | C12, C08 | – |
| C15 | subscription refresh, health, scheduled refresh | 600 | verified | C13, C14, C07 | – |
| C16 | health check, latency, node DNS, region check, domain probe | 1089 | verified | C12, C13 | 5 (5 done) |
| C17 | provider delivery and responses | 540 | verified | C15 | – |
| C18 | Mihomo config generation, Mihomo base settings | 1275 | verified | C12, C15, C16, C17 | – |
| C19 | Surge converter | 1129 | verified | C18 | – |
| C20 | Loon converter | 764 | verified | C19 | – |
| C21 | MosDNS converter | 1261 | verified | C14 | – |
| C22 | OxiDNS converter, native loader, references, match targets | 913 | verified | C05, C21 | – |
| C23 | OxiDNS validation and constraints | 1542 | verified | C22 | – |
| C24 | OxiDNS common settings | 1175 | verified | C23 | – |
| C25 | OxiDNS common routing, DNS watchdog | 1286 | verified | C24 | – |
| C26 | OxiDNS common updates, device policy, editor | 1189 | verified | C25 | – |
| C27 | rule DNS actions | 885 | verified | C26 | – |
| C28 | MosDNS archive and routes | 838 | verified | C21, C15 | 15 (15 done) |
| C29 | OxiDNS native apply, archive, routes | 739 | verified | C27, C28 | 18 (18 done) |
| C30 | subscription and node routes, node deletion | 994 | verified | C17, C16, C19, C20 | 26 (26 done) |
| C31 | aggregation routes, device notes | 722 | verified | C30, C26 | 11 (11 done) |
| C32 | rule routes | 1107 | verified | C09, C14, C27 | 21 (0 done) |
| C33 | rule library and proxy group routes | 976 | verified | C32 | 25 (25 done) |
| C34 | generate, config and profile routes, config export | 1443 | verified | C18, C19, C20, C28, C29, C31, C33 | 59 (0 done) |
| C35 | config importer, WebDAV backup | 1375 | verified | C04, C06 | – |
| C36 | setup flow | 1372 | verified | C34, C35 | 5 (5 done) |
| C37 | domain discovery | 1658 | verified | C14, C16 | 17 (17 done) |

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

Deferred (not open findings): route-level tests → C34 (+ agents registry / routers): `test_profile_agent_bindings.py` 5/8 (rebind/delete-error cases), `test_profile_clone_router_isolation.py` 0/2, `test_save_publish_guard.py`, `test_editor_transactions.py`, `test_qa_integrity.py` (whole-app regressions; run when C30–C34 exist). Hooks still default until their owners land: `PlanNodeDeletion` (C30), redaction hooks (C34, routers, C12), `JobLock` (routers). `repo/compat_*.go` copies to switch: `compat_dialers.go`, `compat_proxystring.go` (C12), `compat_health.go` (C16), `compat_oxidns.go` (C26/C27/C31).

Commands: `rt -- bash -lc 'cd server && go vet ./internal/repo/... && CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(cd .. && pwd) go test -count=1 ./internal/repo/...'`; regenerate: `python3 server/internal/repo/testdata/repo_crosscheck_c04_gen.py server/internal/repo/testdata/repo_crosscheck_c04_ops.json` (local, no Python backend needed) and `rt --back server/internal/repo/testdata/repo_c04_reference.json -- bash -lc 'CONFIGFLOW_REPO=$(pwd) ~/cf-test/.venv/bin/python server/internal/repo/testdata/repo_c04_reference.py server/internal/repo/testdata/repo_c04_reference.json'`.

## C05 — profile context, OxiDNS settings normalizer

**Status: `verified`** (independent adversarial review; zero open functional findings; final `.32` acceptance passed) · prereqs: C04 · Go: internal/repo (scope.go, defaults.go), internal/httpx (app.go profileContext), `internal/oxidns/settings.go` (normalizer), `convert.AppendURLQuery`

Files → Go:
- `backend/common/profile_context.py` (208): `profile_api_path`, `config_api_path`, `_is_global_request`, `resolve_profile_id` → `repo/scope.go`; `install_profile_context` → `httpx/app.go::profileContext` + `logConfigPull`; `append_url_query(url, params)` → **`convert.AppendURLQuery(url, [][2]any)`** (already exported by C21 in `convert/exports_mosdns.go`; reused, no copy; a Mapping is passed as its items in order; error = urlsplit's ValueError as `*convert.PyError`).
- `backend/common/oxidns_settings.py` (289) → `internal/oxidns/settings.go`: native `*yaml.Node` (`Node` alias), `SettingsError`/`ValidationError`/`ReferenceError` with structured issues; `BlockQtypesExec`, `DefaultSettings() *Node`, private `merge`, `SafeRelativePath(value *Node, label string)`, `normalizeRuleDNS`, **`NormalizeSettings(value *Node, validateNative bool) (*Node, error)`**. Nil/null means an empty object; recursive defaults and unknown data are copied. `repo.GetDefaultOxidnsSettings()` remains the independent repository factory and is checked against the native defaults. C23 is now wired: strict native mode calls `NativeDocument` and full expanded-document validation; draft mode only loads safe YAML after the shared attachment/policy/rule-DNS checks. No `PendingError` remains.
- Lazy imports of the normalizer, ported in the same package as the real functions (not copies): `rule_dns.normalize_action`/`ROUTE_LABELS` → `oxidns/ruledns.go` (`NormalizeAction`, `RouteLabels`), `oxidns_device_policy.normalize_policies`/`normalize_client_range` → `oxidns/devicepolicy.go` (`NormalizePolicies`, `NormalizeClientRange`), `oxidns_native.managed_files/native_document/load_native_yaml` → `oxidns/native.go` (C22).

Review: [review-c05.md](review-c05.md). Implementation/coverage commits: `b5b70b3d`, `56b14c21` (plus final acceptance commit). Independent fresh Python references: 1427 profile/HTTP/URL rows (1320 full-pipeline requests, 30 classifications, 32 direct resolutions, 5 path builders, 40 URL calls) and 1778 settings/path rows, including 20 native documents each in strict/draft mode and argument post-state. Every old `normalize` recording now runs without waivers: **1442 checked, no pending C23 rows**. Medium finding fixed: URL appending accepted arbitrary names/IPv4 in brackets; `net/url` zone handling plus `netip.ParseAddr`/`Is6` now rejects unusable bracket hosts. Per coordinator, IPvFuture is also rejected; no CPython URL parser is emulated. Removed the export's redundant exception-class conversion probe. `.63` full repo/httpx/auth/oxidns/convert tests and vet pass; URL-validation-removal and provider-JWT-whitelist mutations are rejected. `.32` default-sharing mutation is also rejected. Read-only, loopback-only snapshot trace confirms both saved OxiDNS profiles are native; template validation, malformed drafts, priority/fallback/header and unauthenticated-provider branches require the synthetic boundaries. See [static coverage](review-c05-production.coverage.json). Final `.32`: gofmt no output, vet pass, five-package race pass (repo 20.424s, httpx 16.615s, auth 1.519s, oxidns 423.003s, convert 56.388s), all-package tests pass; rebased on `go` at `e6c87cc1` and reran vet/all tests/proddiff after C30/C37 integration. Proddiff: four self-checks PASS, 65 GET templates / 282 comparisons, 5 existing global differences, **new=0 stale=0 coverage_regressions=0**. C05 tests rerun after strengthening nested/default-copy assertions: httpx 1.140s, oxidns 3.976s, convert 0.006s. No cache-key collision or secret-log finding. No pushes.

Python test files (rule 4): none of its own. Covered through the replay of every suite call (normalize is reached from 100 test files, among them `test_oxidns_settings_routes.py`, `test_oxidns_editor.py`, `test_rule_dns.py`, `test_oxidns_native.py`, `converters/test_oxidns.py`, `test_gateway_onboarding.py`, `test_oxidns_resource_lifecycle.py`, `test_oxidns_fallback_lifecycle.py`, `test_oxidns_domain_only_native.py`).

Known structure debt (not an open functional finding in this review): **→ S-convert / S-repo / S-httpx, under the [N7/N8 design](../design/n7-n8.md)**. `convert/mihomo_generate_py.go` `appendURLQuery` still takes dynamic key/value pairs and uses shared `fstr`/`raise`/`catch`; the public API is retained. `repo/defaults.go` dynamic document factories and `scope.go` compat snapshots still use `jsonx`; `repo/compat_oxidns.go` `normalizeAction` retains a private dynamic rule-DNS adapter because `oxidns` imports `repo`. `normalizeClientRange` already delegates address logic to the shared native `clientrange` package but retains `pyStrip` at its boundary. Shared extraction/types belong to the designed S passes; no parallel leaf package or public API rewrite was introduced. `httpx/app.go` C05 middleware is already native strings/pointers/context; the remaining request/query emulation and dynamic response sanitizer belong to S-httpx/C06. Settings normalization itself is now native YAML nodes and ordinary Go errors (S-oxidns).

Review coverage includes template short-circuit/first-error order, big integers/bools/floats, native string whitespace/line splitting, stable selector/rule-DNS dedupe, safe paths, native strict/draft behavior and helper invocation. CPython-only Unicode differences retain the existing PORTING/native-library policy; no new corpus waiver was added.

## C06 — HTTP shell, auth, settings routes

**Status: `verified`** (independent review 2026-11-18; 1 medium log-disclosure finding fixed, 0 open; review-c06.md records full gate and final-rebase targeted evidence) · prereqs: C05 · Go: internal/httpx, httpxtest, auth, server, lifecycle, cmd/configflow, api/auth, api/settings, redact

Files:
- `backend/app.py` (206)
- `backend/routes/__init__.py` (113)
- `backend/common/auth.py` (171)
- `backend/routes/auth.py` (83)
- `backend/routes/settings.py` (401)
- `backend/conftest.py` (54)

Tests: `test_bearer_auth.py` (8), `test_login_messages.py` (3), `test_request_exception_redaction.py` (9), `test_rule_fetch_proxy_settings.py` (7), `test_security_regressions.py` (31)

Routes (22, 22 done):

`POST /api/auth/login` [done]; `GET /api/auth/status` [done]; `GET /api/auth/verify` [done]; `GET /api/backup/config` [done]; `POST /api/backup/config` [done]; `POST /api/backup/now` [done C35]; `POST /api/backup/test` [done C35]; `DELETE /api/config-token` [done]; `GET /api/config-token` [done]; `POST /api/config-token` [done]; `GET /api/server-domain` [done]; `POST /api/server-domain` [done]; `GET /api/settings/dependencies` [done C13]; `POST /api/settings/dependencies/sub-store/mode` [done C13]; `POST /api/settings/dependencies/sub-store/update` [done C13]; `GET /api/settings/rule-fetch-proxy` [done]; `POST /api/settings/rule-fetch-proxy` [done]; `GET /api/settings/subscription-aggregation` [done]; `POST /api/settings/subscription-aggregation` [done]; `GET /api/settings/web-port` [done]; `POST /api/settings/web-port` [done]; `GET /api/version` [done]

Evidence: all 22 declared routes exist, including delegated C13 dependency routes and C35 backup I/O. Completion branch adds six-path read-only HEAD dispatch, native Bind/BindSilent request DTOs, per-path complete-file/DB/revision invariance, 132 real Flask response + complete SQLite table/file comparisons (including 27 explicit non-finite rejection vectors), real SQLite save-failure log redaction, direct Python secret/text/finder/error-metadata contracts, and an authentication matrix over every current registration (checkpoint: 144 registrations / 406 method-path rows; C29 registrations included dynamically after rebase), with anonymous, invalid JWT and valid JWT. Existing HTTP differential and changed consumer tests pass on .64; the eight targeted Python source files pass 296 cases. Exact per-function mappings replace the stale blanket 26/9/2 deferrals in [progress/c06.md](progress/c06.md).

Missing / deferred: C06 owner work is complete on O3-6a go@f90a156e: three owner entries detach and check shutdown before settings commits / WebDAV operations; handlers have no Detach or WithoutCancel. Real Runtime HTTP regressions cover eight operations, stopped-runtime state invariance and disconnected-request completion. Shared final commit windows are **已知、已分派**, O3-6c / 6e5c7760 (including WebDAV.Run's context-free receipt after in-flight cancellation). C07 is excluded. MCP (finish), C34 config/export leaf HTTP and C33 proxy-group HTTP retain explicit external owner cases; shared contracts do not claim their leaf acceptance. C33 has since merged with its own independent verified review; C06/C11 do not reassign that owner work.

Clarified in: [design/c06-http-settings.md](../design/c06-http-settings.md). HEAD on all six GET/write settings paths is GET without a body; strict binding follows N8-6 400/415 while silent readers keep their business responses. `compat.go` rule URL validation already delegates to shared rulefetch. `redact` has direct A24 credential-policy tests and new C06 Python text/finder/encoding comparisons; the old “no tests” note is obsolete. Final .64 owner scoped race passed (settings 29.111s, httpx/difftest 69.990s, subs/difftest 21.301s), formatting/vet passed. The author checkpoint gate never acquired a slot for 900s and was terminated (exit75); no stage/fuzz ran in that historical run. The later independent C06/C11 reviews are now complete: C06 log-disclosure fix 8168bc648, C11 evidence/count/regex corrections 118f1084c and cache-invalidation regression 56509f415. The .32 full gate passed (95 race ok packages, 15 enabled difftest packages, proddiff new=0/stale=0, two 60s fuzz targets); after rebase onto a9d6ce86c all relevant targeted checks passed. Final batch gate stays with the coordinator. See [C06 review](review-c06.md), [C11 review](review-c11.md), and the historical [author hand-back](C06-C11-HANDOFF.md).

## C07 — logs, stats, tasks, notifications, web listener

**Status: `verified` — independent C07 review complete; full integration gate remains due (900s queue limit, exit75, no stage ran)** · prereqs: C06 · Go: internal/api/logs, api/stats, api/tasks, api/notifications, tasks, notifications, weblistener

Independent review (2026-11-18): [review-c07.md](review-c07.md), base go@d43cca6e4.
Four findings (0 high / 1 medium / 3 low), all fixed: valid legacy decimal-counter
separators, scoped test providers/runners, listener rollback error contract
(bf8003685), and unknown exact task keys before readiness dispatch (ae816cb07).
e32998805 adds a real Runtime shutdown between saved-state and active-listener
commits. Full changed-package race/live, whole-tree format/vet, C37 manual-task
three-round regressions, 60s counter fuzz (686577 execs), and independent CFG-07
on both servers pass (Python/Go500/500, compared1/diffs0,155 comparator self-tests).
No module-local open findings; existing O3-6c/shared and R14/A16/A17 owners remain
explicit. Reviewer final gate stayed queued through the original900s deadline;
no gate phase/proddiff was run. Coordinator cf-gate on the merged tree is still due.

Scope: backend/routes/{logs,stats,tasks,notifications}.py, backend/notifications.py,
backend/utils/{notification_store,web_listener}.py. All nine routes now done,
including immediate registration of the four formerly wip task/notification
routes. Missing real slots explicitly return 503; no empty production rows.

Commits: 62129a294 tasks readiness/owned interval/stored WebDAV runner/History;
ca6ca9b86 notification context commits/partial observation retention/HEAD;
4d7b232f5 logs/stats owners and stats HEAD; 8f95515dc native listener lifetime and
portable socket/HTTP regression matrix. Acceptance/mapping: 82076dcbb; follow-up CFG-07: 5b6dcd1f3, malformed-history: 20d53d55a, interval display-order: a208172e1. Final foreground gate did not acquire a slot before the15-minute window (exit75); hand back targeted passes per coordinator. Full integration gate remains due.

Clarified in C07 (2026-11-18): [design and external owners](../design/c07-status.md),
[full source-test mapping](progress/c07-mapping.md), [commands/results](progress/c07.md).
Coordinator approved 503 with missing slot names until real rows exist, ready
commands independent of missing rows, 503 for known unported owners and 400 for
unknown keys, no post-commit interval presentation 500, safe partial background
notification refresh, read-only stats/notification HEAD, ctx-aware leaf commits,
BindSilent and real Runtime shutdown/disconnect tests. C37 retains work ctx and
its Runtime generation. All non-nginx listener contracts are retained: existing
seven plus eleven completed; exactly three nginx-only mechanics retired by design.

Acceptance: notifications 15 original contracts plus the successful-release
projection of the sixteenth; actual confirmed-empty /api/routers/r/apply remains
routers-owned. Logs seven original contracts retained. Task run/read malformed,
non-object and non-finite bodies, complete-ready rows/interval writes, nine-route
authentication, DB/JSON/log after-state and combined notification redaction run
against real Flask. Stats adds 48-point/30-minute/corrupt-optional-write/concurrent
and HEAD tests. Runtime tests use real Start/BindRequest/Shutdown, including
cancellation after observation/health and before commit. Fuzz target:
FuzzTaskHistoryCounters (60s PASS,386707 executions). CFG-07 P2 is fixed by5b6dcd1f3: real stat EACCES is not missing/fallback; configdiff R075-F08 PASS with zero diffs, Python/Go500/500 (source evidence direction). No Agent registry copy remains (A01 already wired it).

External owner items are explicit: R14 router_rule_sync; A16/A17 agent rows and
DNS-follow runner; A04 metrics already ready; C06 reviewed web-port handler
HEAD/Bind changes are not duplicated. O3-6c (6e5c7760) owns remaining shared
repo/taskstatus/rulefetch commit windows; adapt RecordContext/ForgetContext on
rebase when available. N8-7 defers overlapping packages until C07 release.
No production device access or production fixtures; no pushes. Only a different
reviewer can mark verified.

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

**Status: `verified`** (independent review 2026-11-18; 1 medium coverage gap and 1 low count error corrected, 0 open; review-c11.md records full gate and final-rebase targeted evidence) · prereqs: C10 · Go: internal/subs, internal/yamlx.Edit

Files:
- `backend/utils/proxy_chains.py` (262)
- `backend/utils/commented_mihomo.py` (112)
- `backend/utils/mihomo_listeners.py` (87)
- `backend/utils/proxy_server_dns.py` (394)

Tests: `test_mihomo_listener_lifecycle.py` (10), `test_proxy_chains.py` (18), `test_proxy_server_dns.py` (22)

Routes: none.

Evidence: `internal/subs` chains.go/listeners.go/proxyserverdns.go and shared `yamlx.Edit`. All 18 chain, 22 DNS and 10 listener source functions now have explicit mappings in [progress/c11.md](progress/c11.md). The old 10-chain/17-DNS blanket deferrals are superseded by verified C18 generation and C19 Surge replay, C15 refresh replay, native helper/corpus tests and the exact public logical-reference regression. C33 proxy-group PUT/DELETE persistence/409/rollback and C34 preview response wiring remain external, coordinator-approved owner entry points; this module does not implement them.

The capture-count TODO is closed: provider backtick sorting compiles each bucket once and uses `rules.Regex.Groups()`. Live Python differential covers conditional/verbose/named/escaped/class parentheses, complete provider lowering and the emitted $N sorting substitution. .64 targeted subs/difftest/convert tests pass. Independent C11 review completed: source count is 18, all 50 source functions are mapped, actual cache invalidation and provider validation are identified separately from metadata/generation replays; 13 live regex vectors apply every emitted $N target. See review-c11.md.

N7-2 implementation: removed the private `subs/commented` parser/emitter
and its generators. The frozen 12,479-row corpus now checks native YAML meaning
and comment bytes, not ruamel formatting. Scalar tokens use in-place edits when
positions/metadata are stable and no alias consumer can change; whole-line block
mapping pair deletions splice when bounds are unambiguous (never first key after
`- `, never leaving an implicit empty map). Flow values inside a removed pair
must parse completely on their starting line; multiline flow values rewrite.
Plain scalars followed by tab-only continuation lines also rewrite. New nodes, shared/merged listener
copies, flow removals, block/multiline scalar changes, tags/anchors or uncertain
ranges use whole-document yaml.v3 encoding. Missing annotations are retained;
undefined/forward aliases expand to their original values. Every result is
reparsed and Equal-checked; failures keep the source and existing unsafe-edit 400.
CRLF splice/rewrite regressions pass. Shared Marshal normalises folded values to
literal style to prevent yaml.v3's extra-newline bug. Non-scalar-key line comments
move above the pair, and Tab-containing strings use double quotes retaining tags
(coordinator-approved shared Marshal corrections).

Splices prove preservation of original comment bytes rather than yaml.v3's
post-edit annotation count (the scanner may leave an orphan note unattached).
Whole rewrites restore missing notes and verify each one. Exact classes,
commands and corpus counts: progress/n7-2.md. Final .32 formatting/vet/race,
three 60s fuzz targets, full Go suite (-p=2), all ten CONFIGFLOW_DIFF packages
and proddiff (new=0 stale=0) PASS. The initial importer 3s resource-probe timeout
passed both isolated and in the reduced-parallelism full rerun; no limit changed.
Opus review N7-2 (2026-11-17, review-n7-2.md): F1 high (alias expansion through `Clone`
silently wrote `null` into relinked listeners/displaced alias consumers), F2 medium (splice failures
refused instead of rewriting; indentless sequences), F3–F6 low (`!!merge <<`, removed-pair comments
moved to the document head, English multi-document reason, unwritable `!!` tag) — all fixed, 0 open; the editor is
verified. C11 completion retains this reviewed editor unchanged; remaining actual proxy-group HTTP transactions belong to C33 and preview response wiring to C34, as mapped above.

## C12 — converter common, Mihomo node conversion, dialer references, EasyTier

**Status: `verified`** (review 2026-11-16, one round; 0 open findings — commits a2ea3673, e5fc9df7, 64a981fa) · prereqs: C09, C11 · Go: `internal/convert`

Files → Go:
- `backend/converters/common.py` (118) → `convert/common.go` (`UDPFallback`, `NTPDirect`, `EmittedRules`, `NTPDirectRule`, `AfterMatchComment`, `NetworkAsProtocol`, `HasPortList`, `SetGeneralOption`, `RulesetFormat`, `ParseHostsPairs`); `compat.go` = `_has_yaml_payload` via `rules.HasYAMLPayload`
- `backend/converters/mihomo.py` node group → `convert/mihomo_nodes.go`: `ApplyGithubProxyDomain`, `MihomoUsedPorts`, `MihomoListenPorts` (+ `pySet`, CPython set order), proxy YAML LRU cache (`loadProxyYAML`, `stringKeys`), `ParseStructuredProxyString`, `InspectStructuredProxyString`, `WithNodeConversions` + `ConversionResults`, `ConvertNodeToMihomo`; region-probe constants
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

**Status: `verified`** (review 2026-11-16, commits ca7a8933, d376585f, 7a570675, 13f50871; table below) · prereqs: C12, C10 · Go: `internal/substore`, routes in `internal/api/settings/dependencies.go`

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

Decisions / differences from Python (porter; see the review below for what changed):
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

### Review C13 (adversarial, 2026-11-16)

Every function of `sub_store_client.py`, `sub_store_runtime.py`, `subscription_cache.py`, `dependencies.py`, `parse_userinfo` and the three routes walked branch by branch against `client.go`, `httpclient.go`, `runtime.go`, `cache.go`, `env.go`, `dependencies.go`, `userinfo.go`, `wire.go`, `api/settings/dependencies.go`.

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | high | Environment proxies: Go used `http.ProxyFromEnvironment` (loopback bypass, ALL_PROXY ignored, read once); requests (`trust_env`, not disabled anywhere in C13) sends the loopback Sub-Store requests through `HTTP_PROXY` unless `NO_PROXY` names them, rereads the environment per request; `proxies=` (rule download proxy) was parsed with `url.Parse` and its credentials sent UTF-8/not at all | shared `internal/reqproxy` (A07's requests port moved out of `agents/proxy.go`, which now binds it to `os.environ`); `RequestsClient` uses `FromEnvironment` / `FromValue`, Proxy-Authorization latin-1 with CONNECT (https) or the request (http) — ca7a8933 | `TestReviewLoopbackSubStoreGoesThroughEnvironmentProxy`, `reqproxy.TestReferenceEnvironmentProxies` (994 requests cases), difftest `TestDifferentialReviewLoopbackProxy` |
| 2 | medium | Redirect hops kept the first hop's proxy decision; requests' `rebuild_proxies` keeps the proxies merged for the first URL and adds the hop scheme's proxy when the hop is not bypassed | `reqproxy.ForRedirect`, per-hop transport — 13f50871 | `TestReviewRedirectProxyLikeRequests` (checked with real requests) |
| 3 | high | A cancelled request context (browser gone) aborted the shared download/command: waiters got the error and a failure cooldown was recorded; Python's thread finished the request | `RequestsClient.Do` and `ExecRunner.Run` use `context.WithoutCancel` — ca7a8933 | `TestReviewCancelledRequestContextFinishesTheFetch` |
| 4 | medium | Decision 10: `ParseProxiesFromYAML` failed on mappings with int/bool/None keys | `yamlx` `KeepNonStringKeys`; root `*yamlx.Mapping` read with `Lookup`; proxies' mappings become objects with json.dumps key text (the cache form); C17 allowlist row removed — ca7a8933 | review corpus `parse_proxies_from_yaml`/`proxies_to_nodes` rows, provider `TestCorpus` |
| 5 | low | `_is_yaml_response` false for documents PyYAML loads but jsonx cannot hold (recursive alias, `!!set`) | root mapping keys read from `pyyaml.Compose` on `UnrepresentableError` — ca7a8933 | review corpus `is_yaml_response` |
| 6 | medium | Malformed `state.json` (JSON that is not an object; `last_used` a string/null/list/huge int) was treated as empty and healed; Python fails: TypeError in `_write`, AttributeError at `state.get` (status → 500, mode switch → 500 after saving the mode), TypeError/OverflowError at `now - last_used` | `read()` returns the JSON value; `write`/`stateGet`/`floatMinus` raise Python's errors — ca7a8933 | `TestReviewRuntimeMalformedState` (32 states × tick/status/use vs Python incl. commands issued and file after), difftest `TestDifferentialReviewMalformedState` |
| 7 | low | `_save_mode`: `reset_config_context()` ran after a failed transaction; missing `system_config` was AttributeError instead of KeyError | reset only after success — ca7a8933 | (code path; route tests) |
| 8 | low | supervisorctl/node output decoded with replacement and raw newlines; Python `text=True` is strict UTF-8 (UnicodeDecodeError, not RuntimeUnavailable) with universal newlines | `ExecRunner` strict decoding + newline translation; `_command` maps only OSError/TimeoutExpired — ca7a8933 | `TestReviewCommandOutputIsStrictUTF8` |
| 9 | low | `Content-Type` parameters: first `charset` won; requests' dict keeps the last | last wins, Python strip/lower — ca7a8933 | `TestReviewCharsetLastWins`, review corpus `response_text` |
| 10 | low | Hosts starting with `*`/`.` connected (ConnectionError) where requests raises InvalidURL "URL has an invalid label." | `checkURL` — ca7a8933 | `TestReviewHostLabelIsInvalidURL` |
| 11 | low | `os.makedirs` failure texts (cache dir, runtime dir): MkdirAll named another path / errno | `pyMakedirs` — ca7a8933 | `TestReviewMakedirsErrorTexts` |
| 12 | low | A falsy first proxy from Sub-Store (`proxies: [0, …]`) was not remembered (Python caches it; Sub-Store asked again each time) | an empty object stands in (every caller only tests truthiness) — ca7a8933 | `TestReviewFalsyConversionIsRemembered` |
| 13 | medium | A panic in the update goroutine would crash the server (Python's thread records the failure) | recover in `installRelease` (→ its `except Exception` path) and `runUpdate` — ca7a8933 | `TestReviewUpdatePanicIsAFailure` |
| 14 | low | `TooManyRedirects` lacked the 31st redirect's status (`safe_exception_details` shows `status=302`) | `CheckRedirect` sets it — 7a570675 | `TestReviewTooManyRedirectsCarriesStatus` (checked with real requests: 31 requests, status=302) |
| 15 | low | flock EINTR returned OSError (Python retries, PEP 475) | retry — ca7a8933 | – |

Decision 8 done here: `substore` uses `pystr.Lower/IsSpace/Strip/Int` (its `pyLower`, digit and int() copies removed) and `urlx.RequestsPrepareURL` / `urlx.Quote` (its `PrepareURL` copy removed; `PrepareURL` is now a wrapper taking `[]Param`). Kept private: `pyIsAlnum`/`isPyWord` (pystr has no isalnum table; Go's Unicode 15 vs CPython's 14 only matters for the conversion log's scheme and userinfo keys), `safeExceptionDetails` (Python inspects only the exception itself; `urlx.SafeExceptionDetails` walks `Unwrap`, which would add the direct error's status to `SubscriptionFetchError`).

Porter's listed differences, decided: no env proxy for loopback / env read once → fixed (#1); no IDNA → accepted (net/http dials the IDNA form; only invalid labels differ, as A01); UTF-8 fallback decoding (other codecs, charset_normalizer) → accepted (no `golang.org/x/text`, as A02); Go transport error texts → accepted (only class and status are ever shown, via `safe_exception_details`); malformed state treated as empty → fixed (#6); download written whole → accepted (Python's partial file is removed on every failure path; same end state); supervisorctl replacement decoding → fixed (#8).

Accepted differences (reviewer):
- `convert_proxy_string` with an invalid `SUB_STORE_BACKEND_API_PORT` raises ValueError in Python (generation/parse-lines fail); Go logs and returns no node — `convert.ProxyStringConverter` (C12, also implemented by the Surge/Loon work) has no error; misconfiguration only.
- Non-list `proxies` values: a mapping/falsy value returns `[]` (Python returns the value itself; `{0: a}` would then be indexed by callers); values yamlx cannot hold (`!!set`, recursive aliases) inside proxies are a YAMLError-class error (Python fails later with AttributeError); `!!binary`/date keys inside a proxy are a TypeError at parse time (Python keeps them until json.dumps refuses them when the cache is written); `{1: a, '1': b}` merges to one key (Python's json.dumps would write the key twice — reading back gives the same).
- `isYAMLResponse` fallback ignores `<<` merge keys at the root of an unrepresentable document.
- Content-Type with a bare `charset` parameter (no `=`): Python raises AttributeError in `resp.text`; Go ignores the parameter. requests' `guess_json_utf` for BOM-less UTF-16/32 JSON bodies not reproduced.
- Non-dict `metadata`/`hosts` in a hand-edited cache: Python raises AttributeError after writing the new cache; Go treats them as empty.
- `prune_orphan_caches` returns names sorted (Python: directory order; the return value is unused).
- Runtime root uses `filepath.Abs` (collapses a leading `//`, which `os.path.abspath` keeps): only the temp directory name differs.
- TLS verification with system roots (requests: certifi / `REQUESTS_CA_BUNDLE`), `.netrc` not read (as A01).

Checks performed (no finding): direct-first logic (`#` fragments, http(s) only via Unicode lower, `clash.meta` UA, YAML check order, `x-powered-by` lower, userinfo from the same response, `traffic or` ≡ `is None` since parse_userinfo never returns `{}`), 403/429 short cut (no Sub-Store), temp names and quoting, `#noFlow`, content passing (`""` included), download URL/target/`noFlow` param, delete on every path incl. failed create, `direct_attempted` conversion-failure error, fallback direct fetch; coalescing (cleanup order, retention 600 s, reuse 300/30/900, `>=` edges, never bypassing in-flight or failures, eviction of completed entries only, untracked owners, failure count cap 8, cooldown 3·2ⁿ⁻¹ ≤ 300, ≥60 s for 403/429 from the direct error, failure purge ≥ 3600 s, success clears failures); node conversion cache (version None/"", LRU 512, deep copies, version re-check); `parse_userinfo` (regex scanning, Unicode `\w`/`\s`, float → int, OverflowError); runtime (lock files/modes/timeouts 45/90/0, state merge order and bytes, `_ensure_running` 20 s/0.2 s loop and failure write, use/maintenance finally replacing errors, set_mode order, tick branches, status table, monitor 5 s, `start_monitor` only with the bundle); cache (paths, `.lock` removal, prune exception set and messages, save order, count/updated_at, previous count log with Python `==`, hosts change log); dependencies (builtin URL validation, version parsing/comparison, VERSION decoding, active bundle choice, running version JSON shapes, release parsing order/errors/notes slicing/digest, 30-min cache keeping the last good release, status dict order, restart/wait/rollback paths and messages, update lock/state); routes (args, 202/409, 400 body, RuntimeUnavailable → 409, other errors 500).

Reference tests added: `testdata/gen_review.py` → `review_reference.json` (CPython 3.11, requests 2.31, PyYAML 6.0.1): 43 userinfo headers (units, signs, Arabic digits, NaN/inf texts, 300/400-digit values, NBSP/U+2028, latin-1), 20 YAML-detection texts, 30 subscription bodies × parse/nodes (non-string keys, merges, base64, JSON, vless fields, multi-document, dates), 14 charset headers, 128 runtime rows (32 malformed `state.json` × tick/status/use), 11 reuse/cooldown clock scenarios, in-flight waiters with mixed flags — asserted by `review_c13_test.go`; `review_c13_fix_test.go` for the HTTP/exec/cache fixes. Differential: +2 tests (loopback environment proxy 3 requests; malformed state.json 3 × 4 requests): **20 (porter) + 3 + 12 = 35 requests, 0 differences**.

Open findings: none.

Commands (review): `rt -- bash -lc 'cd server && go vet ./internal/substore/... ./internal/api/settings/ && go test -count=1 ./internal/substore/... ./internal/api/settings/ ./internal/provider/ ./internal/convert/... ./internal/refresh/... ./internal/server/'` (+ `-race`, + `./internal/reqproxy/ ./internal/agents/...` for the shared proxy code); `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/substore/difftest/'`; reference: `rt --back server/internal/substore/testdata/review_reference.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/substore/testdata/gen_review.py'`.

## C14 — remote rule fetch, ruleset content, remote sources, website identity

**Status: `verified`** (review 2026-11-16, one round, commits 6179d521, bd0a8387, ffc35d9c, 0b080bdf; 0 open findings — see *Review C14* below) · prereqs: C12, C08 · Go: `internal/rulefetch`, `internal/pyyaml` (moved from `rulefetch/pyyaml` by review C02)

Files → Go:
- `backend/utils/ruleset_content.py` (722) → `rulefetch/content.go` (ResultCache, ContentDigest, ClearRuleContentCaches, MetadataError, NormalizeClassicalLines, NormalizeMihomoYAML, NormalizeMihomoRuleset, InspectDomainContent, ValidateDomainContent, RemoteRuleValidationError (+NewRemoteRuleMessage/NewRemoteRuleError; unwraps to repo.ErrValidation), ValidateRemoteRuleContent, ReadRulePayload, AppendRulePayload/AppendRuleLines, RemoveRulePayload, ReadRulePayloadAny/AppendRulePayloadAny for rules.ConnectionDeps), `mrs.go` (_validate_remote_mrs on klauspost zstd). Line helpers `_valid_pattern`, `_classical_line`, `classical_line_error`, `_has_yaml_payload` live in `rules` (exported in `rules/ruleset_lines.go`: rulefetch imports rules); `convert/compat.go` now delegates to `rules.HasYAMLPayload`.
- PyYAML (needed for marks/tokens/error text) → `internal/pyyaml` (Scan, Compose).
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
- Transport failures: `FetchError.Class` and the retry decision follow requests/`_is_transient_disconnect`; message texts are Go's. Since review C14 the transport sends what requests sends (reqproxy proxy, prepared URL, ISO-8859-1 headers, Session.send body reading, urllib3 2.8 decoding: gzip/deflate only) — see *Review C14*.
- `read_rule_source_content`: RequestException and ValueError (incl. UnicodeEncodeError, ProfileValidationError) become `RequestException("Rule download failed (<class>)")`; RemoteRuleValidationError and everything else (storage errors) propagate.
- PyYAML RecursionError = nesting limit 490 (Python's depends on the stack depth); `\U` surrogates → U+FFFD.
- (Review C14) Non-string YAML keys now load through `yamlx` `KeepNonStringKeys` and re-dump; the former allowlist is gone.
- website_identity: `_Meta` is a port of CPython 3.11.17's HTMLParser feed (`pyhtml.go`), `html.unescape` a port with CPython's html5 table; a non-numeric status counts as not 2xx (Python TypeError).
- MRS: zstd framing checked like libzstd 1.5.7 (`mrsFraming`: unknown prefix incl. 1–3 trailing bytes, block limits, reserved bits, dictionary id, 128 KiB window); content decoded by klauspost.
- `refresh_remote_rule` log uses rulefetch's own `safeExceptionDetails` (Python class names via `PythonClass`, status only for an HTTPError itself).
- PyRegex.Sub/SubTemplate/Match always run on regexp2 (CPython must_advance loop via `\G(?:p)(?!\G)`); a backtracking timeout returns ErrMatchTimeout.

Commits: d426421b (pyyaml), c2ba3f6b (PyRegex sub), d3f50ef7 (content/MRS/sources/website/URLs + corpus), d36e5331 (fetch, transport, cache), edf7ac79 (fake transport tests), e773ef34 (integration), (cache tests), 3b9060e2 (settings switch), 7050d97d (local dispatch, adapters).


Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/rulefetch internal/rules internal/convert; go vet ./internal/rulefetch/... ./internal/rules/... ./internal/convert/... && go test -count=1 ./internal/rulefetch/... ./internal/rules/... ./internal/convert/...'` (last: all ok; `-race` ok)
- corpora: `rt --back server/internal/rulefetch/testdata/corpus.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen.py'`; `rt --back server/internal/pyyaml/testdata -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/pyyaml/testdata/gen.py'`; `rt --back server/internal/rules/testdata/sub_reference.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rules/testdata/gen_sub.py'`

### Review C14 (adversarial, 2026-11-16)

Every function and branch of `ruleset_content.py`, `rule_fetch.py`, `remote_rule_sources.py`, `website_identity.py` and the `rule_utils.py` functions finished here walked against `content.go`, `mrs.go`, `fetch.go`, `transport.go`, `urls.go`, `sources.go`, `website.go`, `cache.go`; the transport also against requests 2.31 / urllib3 2.8 source (Session.send, resolve_redirects, prepare_url/prepare_auth, HTTPAdapter, response decoders) and CPython 3.11.17's `html.parser`, `html.unescape`, `urllib.parse`.

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | medium | website identity: `_meta_content` was a regex scan. HTMLParser (3.11.17: new attribute grammar, CDATA/RCDATA modes, comments, declarations) never reports an incomplete tag — the Agent truncates captures at 200 bytes, so a long `og:description` is `''` in Python and was the text in Go; also comments/script/title content, `= 'x` backtracking, duplicate attributes. `html.unescape` was Go's (`&#1;`, `&#x7f;`, `&#xFFFF;` kept, huge numbers wrapped, `&#1` at the end not decoded) | bd0a8387 (`pyhtml.go`: HTMLParser feed port, `pyUnescape` with the html5 table generated from CPython) | review corpus `meta_content` (6084), `unescape` (3132), `clean_text` (1522), `page_identity` |
| 2 | medium | Rule download proxy credentials: `url.Parse` + Go transport sent `Proxy-Authorization` for a user without password (`user:`), UTF-8 instead of latin-1, Go's %-decoding; requests (proxy_headers) sends Basic only with a user and a password, latin-1, literal userinfo when a `%` is not an escape, and raises UnicodeEncodeError (not a RequestException) for non-latin-1 credentials. Decision 14 (shared `reqproxy`) | 0b080bdf (`reqproxy.FromValue`, CONNECT header for https targets, request header for http) | `TestFetchDifferentialAgainstRequests` proxy scenarios |
| 3 | medium | Non-stream requests (`requests.get` path) read the body inside requests: a redirect goes through `resolve_redirects(yield_requests=True)` — non-UTF-8 Location bytes raise UnicodeDecodeError, the body is read (a stalled body is a ConnectionError, a Content-Length cut a transient ChunkedEncodingError and retried; chunked/decoding errors ignored), the target parsed (ValueError). Go skipped the body and decoded Location as UTF-8 (rule_fetch sees ISO-8859-1 text: `/é` → `/%C3%83%C2%A9` next hop) | 0b080bdf (`readUnstreamed`, header values decoded ISO-8859-1, repeated fields joined like HTTPHeaderDict, Transfer-Encoding kept) | differential: `latin1 location*`, `utf8 location`, `two locations`, `redirect body *`, `redirect bad *`, `redirect requote` |
| 4 | medium | `http.Client` parsed a redirect's Location with Go's URL rules before the redirect policy: `%zz` or `http://[::1` became ConnectionError instead of requests' requote / rule_fetch's ValueError | 0b080bdf (`Transport.RoundTrip`; URL credentials as Authorization per prepare_auth: none without a password, latin-1) | `redirect requote`, `redirect bad ipv6`, `url userinfo` |
| 5 | medium | Content-Encoding: Go decoded `zstd` (urllib3 2.8 here has neither zstd nor brotli: left encoded), ignored comma lists (`gzip, zstd` → MultiDecoder with deflate for unknown codings; > 5 codings DecodeError), failed on truncated gzip/deflate streams (zlib returns what it has), on gzip trailing garbage / later members (GzipDecoder swallows), reported a bad gzip header at request time instead of on read | 0b080bdf (`decodeBody`, `pyGzipReader`, `pyDeflateReader`, source vs decoder errors) | 54 decoding scenarios in the differential |
| 6 | low | `_internal_rule_base` swallowed urlsplit/port errors (Python raises `Invalid IPv6 URL`, `Port out of range 0-65535`, … from `request_rule`/`github_download_candidates` before validation); urlsplit texts for bracketed hosts (`An IPv4 address cannot be in brackets`, `IPvFuture address is invalid`, ipaddress text), the NFKC message without the netloc, port repr, hostname lowered with Go tables (`İ`) | 0b080bdf, ffc35d9c (`checkBracketedNetloc`, `rules.IPAddressVersion`, `pystr.Lower`) | review corpus `is_internal_rule_url`/`github_download_candidates`/`rule_request_options` (576 each), `urlsplit_error`, `nfkc_forbidden` (all code points), `page_identity` |
| 7 | low | Decision 8: non-string YAML keys. `read_rule_payload` used a node fallback (no merge keys) and composed before loading; Python loads first and lets KeyError/IndexError/TypeError/RecursionError of the constructors propagate. `normalize_mihomo_yaml` kept flow documents with non-string keys (the porter's accepted case) | ffc35d9c (`yamlx.LoadWith(KeepNonStringKeys)`, `yamlx.Dump` represents `*yamlx.Mapping`; corpus allowlist removed) | review corpus `read_payload`/`append`/`remove`/`normalize_yaml` (27 documents each), `yamlx.TestDumpMappingWithNonStringKeys` |
| 8 | low | MRS: corrupt frames that libzstd rejects as soon as the block header is read (block larger than the frame's block limit, reserved type/bits, dictionary id, window) were "truncated" in Go | ffc35d9c (`mrsFraming`, replaces `trailingGarbage`) | review corpus `validate_remote_bytes` (520: 230 MRS inputs incl. 200 mutations, skippable frames, windows, checksums) |
| 9 | low | Missing CA bundle (`verify=` / REQUESTS_CA_BUNDLE): requests raises OSError only for https URLs (`_request_with_https_proxy`: FileNotFoundError), not a RequestException; Go raised SSLError for every URL (retried/mirrored) | 0b080bdf | `missing ca bundle *` scenarios |
| 10 | low | `read_rule_source_content` converted storage errors (OSError) and passed ValueError-class repository errors through; Python converts exactly RequestException and ValueError | 0b080bdf (`isValueError`, `PythonClass` for repo/store/OS errors) | recorded/corpus + code path |
| 11 | low | `refresh_remote_rule` log: `status=` of the last attempt was printed for a RuleFetchError (Python: it has no response) | 0b080bdf (`safeExceptionDetails`) | code path |
| 12 | low | gofmt: `content.go`, `content_test.go` | 6179d521 | – |

Accepted differences (reviewer):
- Transport error texts are Go's (requests renders urllib3 objects); the class, the retry decision and the RuleFetchError structure match (differential compares classes; `_is_transient_disconnect` cases reset / closed before response / IncompleteRead retried, InvalidChunkLength / decoding / timeouts / refused not). TLS-specific faults (EOF during handshake, certificate errors) are classified by code review only.
- Default trust is the system roots (requests: certifi); IDNA of non-ASCII hosts and `.netrc` are not reproduced (as A01, C13). Go sends `Connection: close` (requests `keep-alive`; every download uses a fresh connection either way).
- Python crashes that Go handles: a first `payload` key that is not a list followed by a payload list (`append_rule_lines` AttributeError; unreachable — `validate_domain_content` refuses documents without a unique payload list before they are stored); constructor KeyError/IndexError inside `normalize_mihomo_yaml` (Go keeps the text with a warning); non-dict/non-string config values (`server_domain`, rule items, markers) where Python raises TypeError/AttributeError.
- `normalize_mihomo_yaml` on a flow document holding `!!set`/`!!omap` values: Python re-dumps them as tagged YAML, Go keeps the text with the yamlx warning (`reviewAccepted` in review_c14_test.go; values a Mihomo rule provider cannot use).
- GzipDecoder holds back a later member's output up to 64 KiB (Python's loss of partial output on a corrupt later member depends on its read chunking); the redirect-body rules of finding 3 were determined empirically against requests (a reset during a redirect body is timing-dependent in Python and not emulated beyond its class); the `NO_PROXY` port check of a redirect hop treats only glibc-exact numeric IPv4 hosts as IPv4.
- PyYAML RecursionError depth (490 nested nodes) as C02/C08.

Coordinator decisions: 8 — rulefetch keeps no `pyLower`/strip copies (wrappers over `pystr`), payload/normalize YAML on `yamlx` `KeepNonStringKeys`; its own `safeExceptionDetails` stays private like C13's (Python looks only at the exception itself, `urlx.SafeExceptionDetails` walks `Unwrap`), now with `PythonClass` covering repository/store/OS errors. 14 — rule downloads choose and authorise their proxy through `internal/reqproxy` (explicit `proxies=` only; redirects are request_rule's own hops, so `ForRedirect` does not apply).

Checks performed (no finding): ResultCache LRU/size bound, content_digest, metadata errors, every inspect/normalize/validate branch with marks in code points (libyaml fast path agrees on `_SIMPLE_PAYLOAD` shapes, confirmed for non-ASCII comments), comment indexing, flow/block rewrites, RemoteRuleValidationError texts (preview, repr, counts), `_remote_text_entries` mark/problem selection, UTF-8 error line numbers, payload append/remove index arithmetic; request_rule candidate order, mirror unwrap, retry delays 1 s/2 s and log lines, size limit (exact size passes), HTTPError text with ISO-8859-1 reason, redirect policy (max_redirects, internal origin, missing Location), local dispatch path; resolve_remote_rule_source branches; known_service/page_identity/site_title.

Coverage map (Python tests of C14; "replay" = calls recorded by `c14_record.py` over the whole suite and replayed by `recorded_test.go`):
- `test_remote_ruleset_validation.py` 12: hand-ported 12/12 + replay (88 calls).
- `test_mihomo_yaml_delivery.py` 7: hand-ported 7/7 + replay (36).
- `test_ruleset_classical_format.py` 15: hand-ported 15/15 + replay (123).
- `test_rule_fetch_retries.py` 10, `test_rule_fetch_fallback.py` 6: hand-ported (fake Transport) + replay of the URL/candidate helpers (16, 30) + transport differential (retry counts per fault).
- `test_rule_fetch_proxy_integration.py` 19: hand-ported over loopback servers + replay (163) + differential (proxy scenarios).
- `test_remote_rule_cache.py` 8: 6 hand-ported (cache_test.go) + replay (32); route halves → C32/C33.
- `test_ruleset_domain_format.py` 13: 5 hand-ported + replay (77); MosDNS conversions → C21, routes → C32/C33/C37.
- `test_website_identity.py` 11: 4 hand-ported + replay (27) + review corpus (HTMLParser/unescape); job/route/region-store cases → C37/C16.
- `test_rule_delivery_proxy.py` 3, `test_rule_proxy_freshness.py` 3: replay of their C14 calls (21, 119); the MosDNS zip / rule-proxy route behaviour → C28.
- C08-deferred: `test_rule_syntax.py` 15 cases hand-ported + replay (219); `test_rule_cache_identity.py` / `test_remote_rule_cache_edges.py` read side hand-ported + replay (16, 31), routes → C32/C33; `test_local_http.py` rule_fetch cases hand-ported (local_test.go) + replay (52).
- Calls from 82 test files in total are replayed (103734 distinct calls), e.g. converters, publication caches, connection rules, domain discovery, router rule sync.

Open findings: none.

Commands (review):
- tests: `rt -- bash -lc 'cd server && go vet ./internal/rulefetch/... && go test -count=1 ./internal/rulefetch/... ./internal/rules/... ./internal/convert/... ./internal/refresh/... ./internal/health/... ./internal/api/settings/'` (+ `-race ./internal/rulefetch/... ./internal/yamlx/`, + `./internal/yamlx/... ./internal/agents/monitor/... ./internal/substore/... ./internal/provider/...` for the shared pieces): all ok.
- record & replay: `rt -- bash -lc 'rm -rf /tmp/c14rec && C14_RECORD_DIR=/tmp/c14rec PYTHONPATH=.:server/internal/rulefetch/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 10 --dist=worksteal -p c14_record -p no:cacheprovider'`, then `rt --back server/internal/rulefetch/testdata/c14_recorded.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/c14_record.py merge /tmp/c14rec'` (copy back in the same rt call as the merge: another agent's sync deletes untracked server files).
- review corpus: `rt --back server/internal/rulefetch/testdata/review_c14.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen_review_c14.py'`; transport differential: `rt --back server/internal/rulefetch/testdata/fetch_c14.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen_fetch_c14.py'`; entity table: `rt --back server/internal/rulefetch/html5_entities.go -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen_html5.py'` then `gofmt -w`.

## C15 — subscription refresh, health, scheduled refresh

**Status: `verified`** (review 2026-11-16, commit 9a85b5c2; see "Review C15") · prereqs: C13, C14, C07 · Go: `internal/refresh` (new; not `internal/substore`: the module also drives rule sources through `rulefetch` and registers task rows)

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

### Review C15 (adversarial, 2026-11-16)

Every function and branch of `subscription_refresh.py`, `subscription_health.py`, `scheduled_refresh.py` and `routes/tasks.py` `_history`/`_host`/`_subscription_rows`/`_rule_rows`/`run_task` (subscription, rule_source)/`set_interval` walked against `interval.go`, `source.go`, `health.go`, `scheduled.go`, `rows.go`, `pyerr.go`, `env.go` and `api/tasks`.

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | high | A cancelled request context (browser gone during a manual refresh, a provider delivery or a rule read) aborted shared work: repository locks taken with a cancelled context fail (at random when uncontended, `select`), so the download cache, the nodes, the health record or the task receipt were dropped after a successful download; Python's request thread always finishes (cf. C13 finding 3) | `RefreshSubscription`, `FetchSubscriptionSource`, `DownloadSubscriptionNodes`, `RememberSourceDownload`, `RefreshRuleSource`, `ReadRuleSource`, `RecordFetch`, `Forget`, `LoadHealth` run on `context.WithoutCancel` (`detached`) — 9a85b5c2 | `TestReviewCancelledRequestContextFinishesTheRefresh` (fails without the fix: `context canceled`) |
| 2 | low | `next_run_at` back-off: `consecutive_failures` below int64 min (hand-edited `task-status.json`) wrapped through `big.Int.Int64()` into a positive exponent (`-18446744073709551611` → 4800 s instead of 300 s) | exponent from the sign / ≥ 11 test without `Int64()` — 9a85b5c2 | corpus `next_run_at` rows (±2⁶⁴, ±2⁶³, `'-18446744073709551611'`, `'٣'`, `-1e19`), differential `subscription:s-future` / `rule_source:r-future` next_run_at compared unmasked |
| 3 | low | `timedelta(seconds=…)` bound checked on `|seconds|`; Python floors into days, so `-999999999·86400-1` overflows with `days=-1000000000; …` (Go reported "date value out of range") | bound on floored days — 9a85b5c2 | corpus intervals `-999999999·86400`, `-999999999·86400-1`, `-10³⁰` |
| 4 | low | `is_library_source` / `find_resource` validated every `rule_library`/`subscriptions` item before searching; Python's `next()` stops at the first match (a non-object item after it does not fail) | `nextObject` — 9a85b5c2 | `TestReviewNextStopsAtTheFirstMatch` |
| 5 | low | `node.setdefault('id', f"node_{uuid4()…}")` draws a uuid for every node; Go only for nodes without an id (visible to recorded uuid sequences only) | always drawn — 9a85b5c2 | replay of `download_subscription_nodes` / `refresh_subscription` with the recorded uuids |
| 6 | low | Decision 8: `fetch_userinfo` lowered with `strings.ToLower` | `pystr.Lower` — 9a85b5c2 | (no ASCII-visible difference) |

Accepted differences (reviewer, in addition to the porter's list above, which stands): the porter's list re-checked — offsets kept by `parseTime` (sub-second offsets cut; instants equal), non-object `traffic` of a hand-edited transport cache dropped, `FetchUserinfo` reading the body (no production caller), TypeError-class errors for non-string `id`/`url`, `host()` of a truthy non-string URL — all hand-edited/unreachable inputs. A panic inside a refresh is recovered by `run` without a task receipt (Python records any Exception; a Go panic is a Go bug). `api/tasks.setInterval` (C07's file) skips non-object list items where Python raises AttributeError (unreachable: repository-validated lists) — noted for C07.

Checks performed (no finding): `validate_refresh_interval` (type is int, bool, big ints, label); `source_cache_metadata` (key order, sources `cache`/`prepared_yaml`, hosts then dialers); `_is_fresh` (Z/offset/naive, future, exact interval boundary, microsecond arithmetic); `_download_cache_path`; `remember_source_download` (stored keys/order, fetched_at default from the clock); `download_subscription_nodes` (order id → url → fetch → remember → parse → empty ValueError → nodes → `sub['name']` KeyError only with nodes → metadata → save); `fetch_subscription_source` branch order (interval validation → StrategyReferenceError, `cache and metadata.get('url') not in (None, url)`, AttributeError for non-dict metadata, usable/current, manual-mode message with `sub.get("name", sub["id"])`, validate_shapes, truthy conversions, safe_dump sort_keys, download cache errors swallowed exactly for OSError/repository/JSON errors, fetch → remember); health (`load_health` swallow set, `list(history or [])[-23:]` for str/dict/None/false/1.5, `int(count or 0)` for str/float/NaN/inf/dict, `if traffic`, `_save_health` logging, `forget` only on non-null); scheduler (TICK/FIRST_DELAY 30 s, flock LOCK_EX|LOCK_NB on `.task-scheduler.lock` held until shutdown, silent return when locked, ≤ 3 workers, claims before running, release in finally, per-job repo scope, `run_now` claim → TaskBusy, background on a detached context); `describe_failure` order and the requests hierarchy (ConnectTimeout → Timeout first, SSLError before ConnectionError, `type(error) is ValueError`); `next_run_at` (`not interval` first, entry AttributeError, `parse_time(last) or parse_time(baseline)`, `int()` of every count form, `min(interval, retry)`); `subscription_cache_time` (`or {}`, nodes truthiness before metadata); `rule_cache_time` (ValueError/OSError swallowed, isfile following symlinks, half-even microsecond rounding of st_mtime); `_due` (`not sub.get('enabled', True)` vs rows' `is not False`, KeyError for a missing id, `due <= now`); rows (field order, history `int()`s, rule `cache_updated_at` fallback, `_host` via urlsplit port); `run_task` mapping (TaskBusy/LookupError strip/ValueError incl. ProfileValidationError → 409/404/409, others 500).

Record & replay: `testdata/c15_record.py` (pytest plugin over the whole Python suite: 12552 passed) records 21 public functions with the files they read and wrote (`subscribes/<id>.json`, the transport cache, `subscription_health.json`, `task-status.json`), the collaborators' answers (`get_subscription_proxies_yaml`, the `fetch=` callable, `convert_proxy_string`, uuid4) and a frozen clock; `recorded_test.go` replays them on a fresh repository each: **3150 distinct calls from 78 test files, 3145 replayed with 0 differences** (outputs, error class + text, file bytes after the call); 5 skipped because the test monkeypatched one of that call's own collaborators (e.g. `save_subscription_nodes` → lambda).

Coverage map (Python tests of C15):
- `test_scheduled_tasks.py` 13: hand-ported 9 (see porter's list) + replay (113 calls) + differential; deferred as listed above (C30/C33/routers/A16/A17/C37).
- `test_subscription_refresh_policy.py` 12: hand-ported 7 + replay (91 calls: `fetch_subscription_source`, `download_subscription_nodes`, `_is_fresh`, …); route halves deferred to C17/C18/C30/C31/C34 as listed.
- `test_flow_ui_apis.py` C15 cases: hand-ported + replay (66 calls incl. `record_fetch`/`forget`/`parse_userinfo`).
- `test_subscription_hosts.py` 2 C15 cases: hand-ported + replay (37 calls of `source_cache_metadata`/`download_subscription_nodes`).
- `test_notifications.py` (118 calls), `test_remote_rule_consumers.py` (41), `test_rule_proxy_freshness.py` (146), `test_subscription_cache_preview.py` (83), `test_subscription_fetch_requests.py` (6), `test_proxy_group_regex_exclusion.py` (70), `test_mihomo_classical_delivery.py` (37), `test_rule_fetch_proxy_routes.py` (19), `test_profile_scoped_artifacts.py` (19), `test_agent_push_diff_api.py` (36), `test_agent_content_*` (106), `test_dynamic_proxy_chains.py` (81), `test_subscription_dialers.py` (1): C15 calls replayed; their route/generation behaviour stays with the modules listed above.
- Not recorded (integration functions over the rule fetcher): `refresh_rule_source`, `read_rule_source`, `run_round`, `run_now`, `_due`, rows — covered by the hand-ported tests, the corpus scenarios (rows, `_due`, rule-cache mtimes) and the differential (rule runs: success, invalid, 503, content source).

Corpus additions (`testdata/gen.py`, now 1210 rows, all equal): back-off counts beyond int64, negative timedelta bounds, health scenario `health review` (23/24-item histories, null/false/1.5/emoji histories, NaN/inf/-1e300/` 7 `/`٧`/`7.0`/dict counts, Unicode ids). Differential: + `s-future`/`r-future` (exact next_run_at), **31 requests, 0 differences**, files equal.

Open findings: none.

Commands (review):
- tests: `rt -- bash -lc 'cd server && go vet ./internal/refresh/... && go test -count=1 ./internal/refresh/... ./internal/tasks/... ./internal/api/tasks/... ./internal/server/'`
- differential: `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/refresh/difftest/'`
- record & replay: `rt -- bash -lc 'rm -rf /tmp/c15rec && C15_RECORD_DIR=/tmp/c15rec PYTHONPATH=.:server/internal/refresh/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 10 --dist=worksteal -p c15_record -p no:cacheprovider'`, then `rt --back server/internal/refresh/testdata/c15_recorded.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/refresh/testdata/c15_record.py merge /tmp/c15rec'`
- corpus: `rt --back server/internal/refresh/testdata/corpus.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/refresh/testdata/gen.py'`

## C16 — health check, latency, node DNS, region check, domain probe

**Status: `verified`** (review 2026-11-16, commit b579c790; see "Review C16") · prereqs: C12, C13 · Go: `internal/health/check` (leaf: no repo import), `internal/health`, routes in `internal/api/nodes`

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
- `repo/compat_health.go` `validateHealth` now calls `check.ValidateHealth` (done in review C16, b579c790). `validateRefreshInterval` there stays (C15's; refresh imports repo).

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

### Review C16 (adversarial, 2026-11-16)

Every function and branch of `health_check.py`, `node_latency.py`, `node_dns.py`, `region_check.py`, `domain_probe.py` and the latency routes of `routes/nodes.py` walked against `check/healthcheck.go`, `check/region.go`, `check/probe.go`, `check/num.go`, `check/pyerr.go`, `latency.go`, `dns.go`, `ipaddr.go`, `runner.go`, `wire.go`, `api/nodes/latency.go`; the Agent delete hooks against `routes/agents.py`.

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | high | A cancelled request context (browser gone during a 20-node test or a DNS check) killed the worker (`ExecRunner` treated `ctx.Done()` as a timeout → 503 "执行超时") and could drop the stored results / the saved DNS setting (repository locks fail under a cancelled context); Python's request thread finishes the subprocess and stores everything (cf. C13 finding 3) | `ExecRunner.Run` ignores the context (only the timeout kills); `RunTests`, `CheckSettings`, `SaveConfig` run on `context.WithoutCancel` — b579c790 | `TestReviewCancelledRequestFinishesTheLatencyTest` (real worker script, request cancelled mid-run, 3 rounds, history stored) |
| 2 | medium | Agent delete hooks: `os.remove` is unlink(2) — a directory where `probes/<id>.json` / `regions/<id>.json` should be fails with `IsADirectoryError` and DELETE /api/agents/<id> answers 500 `[Errno 21] Is a directory: '<path>'`; Go's `os.Remove` silently removed an empty directory and reported Go's `remove …: directory not empty` for a full one | `jsonstore.Remove` = unlink with EINTR retry (shared C02 helper; also fixes A05's traffic store), hooks return Python's OSError text — b579c790 | `TestReviewStoreDeleteOfADirectoryFailsLikeOSRemove` (texts checked against CPython 3.11 on the test server) |
| 3 | low | Crash texts: `"string indices must be integers"` (3.11 adds `, not 'str'`), `[url, *web.get('redirects')]` of a non-iterable (`Value after * must be an iterable, not NoneType`) | b579c790 | review corpus rows `pick bad`, `evaluate` |
| 4 | low | Decision 8: `repo/compat_health.go` kept its own validate_health / urlsplit copy | `validateHealth` delegates to `check.ValidateHealth` (ValueError → repo helper ValueError); the private urlsplit, bracket, port and key tables removed; repo's verified tests and golden rows pass unchanged — b579c790 | `internal/repo` suite, replay of 1640 recorded `validate_health` calls |

Accepted differences (reviewer; the porter's list above stands after re-checking): `validate_health` walks the object's key order (Python's set order is randomised per process: only which message wins with several invalid fields); `statistics.median` over non-number delays is a TypeError in Go (the Agent's probe answer is `[]int` by schema — `go-agent/domain_probe.go`); `domain_suggestion` compares only numbers/strings (lists are ordered in Python; hand-edited probe files only); Python crash texts in `*check.PyError` that render objects remain approximate (classes equal — replay compares text too and found only #3). Not reproduced: a worker that leaves a grandchild holding stdout (Go waits for the pipe, as Python's normal path does). Region/probe store write failures carry Go error texts — only C37 routes can show them (note for C37).

Checks performed (no finding): health defaults/initializers (setdefault order, `tolerance` for url-test, legacy `health_check_url` truthiness), every validate_health branch (bool/int types, big ints, URL whitespace/C0, urlsplit errors incl. bracketed hosts, port 0/65536, credentials incl. `@` with empty user, fragment, expected-status grammar/bounds/order); node DNS (`_ipv4_server` partition/strip/ASCII decimal port/53 dropped/leading zeros, `_server` length in code points, `?#\`, IDNA 2003 ASCII fast path and label lengths, IPv4-only literals, label regex, `%` escapes, `netloc.endswith(':')`, tls path, default ports, urlunsplit), normalize/load/effective/describe/save (managed mode → legacy list, node_test_dns mirror, reset after save); `settings()` (strip, urlsplit, truthy credentials, `.port`, `int()` forms, unified delay lower), candidate keys (`["node",id]`, ensure_ascii False, `is not False`, setdefault first wins), `_proxy_of` exception → None, `_needs_sub_store`, probe binary order (env alone when set, isfile + X_OK), payload bytes (`ensure_ascii=False` for latency, ASCII for the check), 8 MiB bound, budget `ceil(n/20)·t/1000+5`, GOMAXPROCS/GOMEMLIMIT defaults, return code 3 / non-zero (incl. signals), strict UTF-8 of stdout then stderr, result validation order (version → ProbeUnavailable passes through; any Value/Type/KeyError → invalid), `run_tests` order (lock → candidates → names/keys → 20 → identities → same-name check → 5 conversions → settings → binary → conversions → probe → stamp → same_test history → filter → save) and the response shape; region evaluators (markers/None handling, Netflix region from path, statuses all-403/404, YouTube CN), `evaluate_services` by-id hashing (1 == True), `exit_info`, classify (451, block marker with 200/403, region-page redirect only when the original path is not one, `status < 500`), summarize, `pick_targets` (stable preferred-first sort, 4 groups, alive default, 8 regions in rule order), RegionStore (setdefaults, 30-day wall-clock bound, `>=` string compare, newest 1000 websites), ProbeStore (prune `<` bound, 5000 cap with stable reverse sort, load prunes without writing), `latest_probes`, `judge` (exact int/float comparisons, `round` half-even, overflow/zero-division paths), `probe_url`, routes (names/keys validation, 409/503/400 mapping, `dns: null` = saved setting for the check, None → ValueError for save), Wire hook order (traffic → probe → region).

Record & replay: `testdata/c16_record.py` over the whole Python suite (12552 passed) records 34 public functions with the worker call (`_probe_binary` result, argv, stdin text, timeout, GOMAXPROCS/GOMEMLIMIT, answer), the environment and system_config fields read, Sub-Store conversions, store files before/after and the frozen naive clock; `recorded_test.go` replays **2280 distinct calls from 60 test files with 0 differences** (outputs, error class + text, worker stdin bytes/argv/timeout/env, store file bytes). Review corpus `testdata/gen_review_c16.py` → `review_c16.jsonl.gz`, **1017 rows, 0 differences** (`TestReviewCorpus`): 58 health URL forms ×2, int/status/shape edges; 85 DNS server forms through `_server` and `_ipv4_server` (brackets, IDNA/ß/☃/İ/Kelvin, 63/64-char labels, 253/254 names, `%` escapes, ports); 31 env sets for `settings()`; 38 worker outputs for `_run_probe` (versions 4.0/True/"4", latencies 2⁶⁴/1.0/True/0/"1", duplicate/extra/missing/non-string names, NaN, BOM, `-0`) + 4 return codes; 31 DNS-check outputs (latency `-0`/True/2⁷⁰, scoped/mapped/duplicate IPv6, 198.18/15 edges, error codes) + 2 return codes; 39 region names (full-width, NBSP/ideographic space, ſ, İ, Türkiye, emoji); classify statuses/URLs/shapes; 6 target sets for `pick_targets`; 14 result sets ×2 (evaluate/exit); 15 rate stats; 20 judge pairs ×5 options (half-even rounding, 10⁴⁰⁰, bools, float totals, no_path); suggestions; store bounds (exactly 30 days, 5003 tied hosts, bad entries, 1010 websites).

Coverage map (Python tests of C16):
- `test_node_latency.py` 25: hand-ported 25/25 + replay (215 calls) + differential.
- `test_node_dns.py` 13: hand-ported 13/13 + replay (134) + differential.
- `test_region_check.py` 19: 11 hand-ported + replay (53: evaluators, classify, stores); 8 deferred to C18/A14/C37/A05 as listed above.
- `test_domain_probe.py` 16: 5 functions hand-ported + replay (68: judge, stores, suggestion, probe_url); 11 end-to-end cases → C37.
- `test_website_identity.py` (C16 store part): replay (15); job/route cases → C37.
- Health tests `test_health_check_advanced.py` (6 calls), `test_health_https_defaults.py` (2), `test_health_persistence.py` (6), `test_health_review_r1.py` (30), `test_health_review_re1.py` (15), `test_health_review_re2.py` (36), `test_health_validation.py` (29), `test_proxy_group_batch_health.py` (33), `test_qa_integrity.py` (32): their validate_health/initialize/provider_health_check calls replayed; generation/route halves stay with C18/C19/C20/C30/C31/C33/#39.
- `test_flow_ui_apis.py` (11), `test_node_test_dns_cleanup.py` (7: load_config/normalize calls; the cleanup itself is not ported by decision), `test_domain_discovery.py` (25), `test_oxidns_ip_probe.py` (35), `test_dialer_round2.py` (1107 validate_health calls) and 40 more files: C16 calls replayed.
- Not recorded (integration over repository + worker): `run_tests`, `collect_candidates`, `load_results`, `save_config` — covered by the hand-ported tests and the differential (44 requests, worker stdin byte-identical, `node_latency.json` and system_config compared).

Differential: unchanged (44 requests, 0 differences); the directory-at-store-path case of finding 2 is the agents DELETE route's (A01 differential, noted for the agents domain).

Open findings: none.

Commands (review):
- tests: `rt -- bash -lc 'cd server && go vet ./internal/health/... ./internal/api/nodes/ ./internal/repo/ ./internal/jsonstore/ && go test -count=1 ./internal/health/... ./internal/api/nodes/ ./internal/repo/... ./internal/jsonstore/... ./internal/agents/trafficstore/... ./internal/server/'` (+ `-race`)
- differential: `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/health/difftest/'`
- record & replay: `rt --back server/internal/health/testdata/c16_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c16rec && C16_RECORD_DIR=/tmp/c16rec PYTHONPATH=.:server/internal/health/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 10 --dist=worksteal -p c16_record -p no:cacheprovider && PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/health/testdata/c16_record.py merge /tmp/c16rec'`
- review corpus: `rt --back server/internal/health/testdata/review_c16.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/health/testdata ~/cf-test/.venv/bin/python server/internal/health/testdata/gen_review_c16.py'`

## C17 — provider delivery and responses

**Status: `verified`** (review 2026-11-16: 4532957f, 00af00ee; see "Review C17") · prereqs: C15 · Go: `internal/provider` (new)

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
- **C13 `substore.ParseProxiesFromYAML`**: mappings with non-string keys (`proxies: [{1: a, name: A}]`, `1: a` at the root) fail (yamlx NonStringKeyError); Python returns them (key `1`). Corpus allowlist `knownDifference`. **Fixed in review C13 (ca7a8933); allowlist row removed.**
- **C12 `convert.ValidateEmitted`**: with a non-dict provider definition (`proxy-providers: {F: 1}`) and an active chain, Go raises AttributeError where Python first reports the dangling `dialer-proxy` (DialerReferenceError). Hand-made invalid main only; allowlisted.

Commits: cb386206 (code), 2e9a7dfe (corpus, response tests), 430f79bc (ported tests), + this PROGRESS commit.

Reviewer hints: `prepareSubscriptionProvider` try/except boundary (which errors become transport errors; `update` only after validation); `PrepareProviderBundle` render condition order and the `active` rescan; `providerContext` dedup key; `parseMarkedError` for ConstructorError marks; `ServeProvider` timer vs queueing in the 8-slot pool.

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/provider; go vet ./internal/provider/ && go test -count=1 -race ./internal/provider/'` (last: ok, 38.6 s)
- corpus: `rt --back server/internal/provider/testdata/corpus.json.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/provider/testdata/gen.py'`

### Review C17 (adversarial, 2026-11-16)

Every function and branch of `provider_delivery.py` and `provider_response.py` walked against `delivery.go`, `errors.go`, `response.go`, `hooks.go` (try/except boundary of prepare, `update` only after validation, render condition order and the `active` rescan, single shared fetch per (id, url) incl. `prepared` futures, `_provider_context` dedup key, `_provider_source_error` field lookup and marks, `serve_provider` timer vs pool queueing, copies, last-good write/read).

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | high | PORTING 4b: `PrepareSubscriptionProvider`, `PrepareProviderBundle` (shared per-subscription fetches, download-cache writes), `CommitCacheUpdates`, `CommitProviderBundle` used the caller's context; a route passing `c.Context()` (C30 refresh/proxies routes, A14 push) would turn a client disconnect into a transport fallback, a different answer or a half-committed bundle. Python's request thread finishes | all four run on `context.WithoutCancel(ctx)` (repo scope kept) — 4532957f | `TestReviewBundleAndCommitFinishWhenTheRequestIsCancelled` |
| 2 | low | `x['key']` on a str/list item (hand-made main/config) said `'str' object is not subscriptable`; CPython 3.11: `string indices must be integers, not 'str'` / `list indices must be integers or slices, not str` | `subscriptError` — 4532957f | `TestReviewValidateSubscriptTexts` (texts from `testdata/review_c17_ref.py`) |
| 3 | low | eager evaluation where Python is lazy: `source.get('url')` of the provider's own definition checked before the loop (Python: only for other definitions); `resource['id']` of `requested` read before the catalog scan (Python: per item); the `prepared` future key used the raw id/url while the fetches look it up with `str()` (non-string ids never shared) | 4532957f | replay + `TestReviewValidateSubscriptTexts` case 1 |
| 4 | low | decision 10: `flattenMapping` private copy of yamlx's flatten | `yamlx.FlattenMapping` exported, copy removed — 00af00ee | corpus + replay |
| 5 | test | timing flake of `TestAggregationFallsBackToCommittedArtifactBeforeFirstServe` (decision 14): the render released at the end of the test kept running and wrote `providers/served/…` while `t.TempDir` was being removed ("directory not empty" under load); the 0.2 s budget only bounds the fallback from below | `newServeFixture` registers a cleanup (runs first) that waits until no flight is left; nothing the tests check was weakened — 4532957f | the response tests ×15 with `-race` while every CPU of the test server was busy: ok |

Accepted differences (re-checked, unchanged): provider YAML with values jsonx cannot hold (`!!set`/`!!omap`/`!!pairs`, recursive aliases) is a non-YAML load error in Go (transport fallback in prepare) where Python loads it and raises a shape error — never produced by Sub-Store/airport YAML; non-string provider `url` → TypeError when sources are collected; non-string sub ids passed to fetchers as `str()`; a render body that is not UTF-8 is stored as last good unchanged (Python's `get_data(as_text=True)` would raise; every provider render is text). The C12 `ValidateEmitted` allowlist row of the corpus is gone (`knownDifference` empty; fixed in C12's review).

Checks performed (no finding): `capture` (generator order, `enabled` default, frozenset of names), `has_chains` (short-circuit order, `override` truthiness, AttributeErrors), `validate` (shape errors first, chain-exit overlap, same-URL payload aliasing, `{**main, 'proxies': …}` key position), `adapt_delivered` (`getattr(main_json)`, falsy id), `dialer_record` reads (metadata `dialers`, download cache only for a URL, read errors → None), `validate_proxy_shapes` (field tuples, `.strip()` = `pystr.Strip`), `_load_provider_proxies` (YAMLError vs other, root/proxies errors, label/owner), prepare (source_prepared vs refresh policy, which errors become transport errors, `StrategyReferenceError` re-raise, cache fallback, `allow_transport_fallback` only without chains, `failure_text` of the transport error, `convert_node_to_mihomo` truthiness), `validate_rendered_bundle` (monotonic activation, unverified-fallback error, by-URL payload sharing, `require_providers`), bundle (requested catalog/definition injection with deep copies, `opaque` nodes, render condition order, `active` rescan in `rendered` insertion order, `local_path` KeyError, progress texts and `force`), commit (last update per sub id wins at the first position, artifacts after caches), `ServeProvider` (key fields incl. `format or ''`, digest only as identity, fallback only without `format`, the timer covers pool queueing like `future.result(timeout)`, no last good → wait, UnicodeDecodeError propagates, `text/yaml; charset=utf-8` + `X-ConfigFlow-Provider: last-good`, `%d` seconds, last good written only for a 200 `text/yaml` render, per-waiter copies incl. Content-Disposition, Content-Length recomputed, render on `WithoutCancel`), `RequestedFilters` (presence vs value, `format` check before regex validation, 400 messages).

Record & replay (new): `testdata/c17_record.py` (pytest plugin) records every top-level call of `validate_proxy_shapes`, `parse_provider_proxies`, `DeliverySnapshot.capture/.has_chains/.validate`, `validate_rendered_bundle`, `prepare_subscription_provider`, `prepare_provider_bundle`, `commit_provider_bundle`, `commit_cache_updates` during the full suite (12552 passed under the recorder), with the collaborators' answers: A11 `preparation_context`/`source_error` (by arguments), every fetch (label/id/url → result or exception; aggregation fetches run on worker threads and are compared as a multiset), Sub-Store conversions, `generate_aggregation_provider` answers (test doubles included) with the fetches they made, uuid4 node ids of prepare, the subscription/download caches before the call and the files after commits, and the call's wall-clock window (timestamps inside it and cache `updated_at` masked on both sides). `recorded_test.go` `TestRecordedPythonTests`: **1262 distinct calls from 712 tests in 42 files, 0 differences** (bundle 762, capture 196, parse 94, validate 72, commit_updates 58, commit_bundle 55, has_chains 15, prepare_sub 7, rendered_bundle 2); 1 call skipped (a `SimpleNamespace` snapshot in `test_subscription_refresh_policy.py`, hand-ported). A test's fake aggregation provider without `cache_updates` (`test_proxy_chains.py`) and tests that replace `commit_cache_updates` are handled explicitly in the harness.

Coverage map (Python tests touching C17):
- `test_provider_response.py` 9: hand-ported 8 (`response_test.go`); `test_aggregation_endpoint_falls_back_when_generation_is_slow` → C31 (route).
- `test_provider_filter_isolation.py` 4: hand-ported 4.
- `test_provider_error_locations.py` 14: replay (34 calls, real A11 texts) + hand-ported 13; `test_publication_entrypoints_preserve_subscription_and_aggregation_hierarchy` → C30/C31/A14 (routes).
- Replay of their C17 calls (route/generation halves stay with C18/C30/C31): `test_dialer_provider_content.py` 73, `test_dialer_rendered_bundle.py` 114, `test_dialer_activation_shadow.py` 123, `test_dialer_standalone_activation.py` 101, `test_dialer_combined_export.py` 113, `test_dialer_delivery_snapshot.py` 67, `test_dialer_delivery_staging.py` 7 (+ hand-ported half), `test_dialer_review_fixes.py` 34, `test_dialer_round2.py` 36, `test_dialer_proxy.py` 28, `test_dialer_provider_scope.py` 13, `test_dynamic_proxy_chains.py` 37, `test_proxy_chains.py` 22, `test_group_exclusion_generation.py` 18, `test_proxy_group_regex_exclusion.py` 22, `test_subscription_dialers.py` 6, `test_subscription_hosts.py` 8, `test_subscription_refresh_policy.py` 30 (+ hand-ported), `test_subscription_regex_filter.py` 1 (+ hand-ported).
- Replay of the bundle/commit calls of `test_agent_content_api.py` 30, `test_agent_content_preparation.py` 21, `test_agent_first_push_sync.py` 31, `test_agent_push_diff_api.py` 21, `test_generated_preview.py` 48, `test_setup_flow.py` 100, `test_split_layout_migration.py` 47 and 16 more files (profile, security, import, router split, service domain, …); their own subjects belong to A11/A12/A14/C34/C36.

Notes for C30/C31 (routes): call `RequestedFilters` before fetching; `ServeProvider` only after auth/existence checks, with the render closure capturing `c` (the render gets a `WithoutCancel` context — pass it on); every other preparation/commit path is now cancellation-safe by itself. Their differentials must cover `X-ConfigFlow-Provider: last-good` + `text/yaml; charset=utf-8` after `ResponseSeconds`, Content-Disposition copied per waiter, `?format=surge|loon` never falling back, the `regex`/`exclude_regex` 400 texts, the last good file written only for a 200 `text/yaml`, and the aggregation fallback to `providers/<id>.yaml`. C31 registers `SetAggregationProvider` (fail-closed default checked by `TestDefaultHooksFailClosedAndPassThrough`); A11 registers `SetLocations` and should reproduce the A11 texts recorded in the replay rows (`locations`).

Open findings: none.

Commands (review):
- tests: `rt -- bash -lc 'cd server && go vet ./internal/provider/ ./internal/mosdns/ && go test -count=1 ./internal/provider/ ./internal/mosdns/ ./internal/convert/... ./internal/substore/...'` (+ `-race`: ok, provider 62.5 s)
- record & replay: `rt --back server/internal/provider/testdata/recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c17rec; C17_RECORD_DIR=/tmp/c17rec PYTHONPATH=.:server/internal/provider/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 8 --dist=worksteal -p c17_record -p no:cacheprovider; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/provider/testdata/c17_record.py merge /tmp/c17rec'` (~2.5 min)
- references: `rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/provider/testdata/review_c17_ref.py'`

## C18 — Mihomo config generation, Mihomo base settings

**Status: `verified`** (independent functional review 2026-11-17; zero open functional findings) · prereqs: C12, C15, C16, C17 · Go: `internal/convert` (+ `internal/convert/mihomoenv`)

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

Review C18 (2026-11-17, independent reviewer; branch `go-review-c18`):
- High: custom YAML parse errors were copied verbatim into ERROR logs, including source excerpts with secrets/subscription URLs. Python has the same bug (`mihomo.py` parse-error logger). Removed duplicate logging; callers still receive the same `CustomConfigYAMLError` diagnostic and generation aborts. Synthetic controller-secret and token-URL fixtures failed before the fix.
- Medium: `sourceURLText` stringified false/zero/empty legacy library URLs into nonempty format overrides. Python falls back to the rule's URL. Fixed empty-value coalescing; both generated rule-provider format/path and ruleset download extension now match. Python on `.63` confirmed all six null/empty/false/zero/container variants resolve `/fallback.yaml` as YAML. Four non-string variants failed in Go before the fix.
- Baseline `.32` proddiff exit 0: self checks PASS, coverage_regressions=0, new=0/stale=0; main Mihomo 2 successes, Agent Mihomo 1, subscription Mihomo 4, downloads 2 per kind.
- Structural Python types/errors remain **→ S-convert, waiting for N8 design**, per the coordinator's explicit scope. Public APIs and invalid-input acceptance/rejection stay unchanged.
- Review commits: `d1bba127` (parse-log leak and format fallback fixes), `6eab5b5a` (1010 structural boundary cases, coverage matrix and native bounds). `.63` full convert suite passed (6.426s, then 6.698s); final boundary suite passed (0.137s).
- Added independent `review_mihomo_gen.py` → `review_mihomo.jsonl.gz`: **1010 public-boundary rows, 235 expected rejections**, structural generated documents + reports/mutations, ordered download lists, base validation, takeover and Agent context. Earlier 998-row full convert suite passed on `.63` (6.698s); final added host/core-version cases also passed. Source coverage / full branch matrix / all 66 Python test-file mappings are in `parity/review-c18.md`. Production trace confirms base validators and several generation helpers are not visited by the snapshot.
- Local native cleanup: bounded int64 comparisons replace arbitrary-precision temporary allocations; DEBUG lengths use strconv.Itoa. String/digit validation semantics remain intact. Mutation check temporarily restored the broken nonempty format override on the remote copy: config structure and ruleset-download comparisons failed as expected (`format-fallback mutation detected`); fixed source restored. Partial reports on generation errors are also compared.
- Authoritative `.32` acceptance after rebase to current `go`: gofmt internal/tools clean; `go vet ./...` PASS; `go test -race -count=1 -timeout=30m ./internal/convert/` → `ok configflow/internal/convert 56.312s`; `go test -count=1 ./...` all PASS (convert 28.788s, installer 150.618s, oxidns 86.902s; final weburl 0.004s / yamlx 0.831s / proddiff generator no tests). `server/scripts/proddiff.sh` exit 0: self-check status/json-leaf/yaml-leaf/text-line PASS, coverage_regressions=0, new=0/stale=0; main2/Agent1/sub4/provider-download2/rule-download2 success, 5 existing unrelated global differences.
- Complete: no open functional findings; no public API changes or new dependencies. Structural debt stays assigned to S-convert. Final report and exact commands/output: `parity/review-c18.md`. Rebased onto `go` at `869db198`; the last rebase added only HANDOFF/router-matrix documentation, so tested Go sources are unchanged. Clean worktree at hand-back; no push.

## C19 — Surge converter

**Status: `verified`** (independent functional review 2026-11-17; no open functional findings) · prereqs: C18 · Go: `internal/convert` (`surge.go`, shared `clients.go`)

Files → Go:
- `backend/converters/surge.py` (1129) → `convert/surge.go`: `GenerateSurgeConfig(ctx, config, baseURL)` (= generate_surge_config), `ConvertNodeToSurge(node) (line, wireguard, err)` ("" = None), `ConvertProxyGroupToSurge(group, config, baseURL, smartGroups)`, `ConvertProxiesToSurgeText(proxies, toNodes)` (toNodes = `substore.ProxiesToNodes`, passed by the caller because substore imports convert), `GetProxyGroupNodes`, `SurgeDefaultGeneral`, `SurgeBuiltinPolicies`; `split_rules_and_rulesets` is C18's identical `splitRulesAndRulesets`.
- `convert/clients.go` (shared with C20; renamed from `clients_py.go` during review): `pyDict` (hashable-key dict), `joinAny` (str.join TypeError), `strIn`, `nextMatch`, `hasPortListAny` / `networkAsProtocolAny` / `rulesetFormatAny` (C12's common helpers for values of any type; a non-string truthy `source_url` is detected with two sentinel URLs → AttributeError 'decode' exactly when urlsplit would see it), `validateRulePolicies` (unhashable policy → TypeError before `rules.ValidateRulePolicies`), `parseCustomSections` (dict semantics: repeated section keeps first position, last text), `configAPIPath` (repo's copy; repo imports convert), `resolveProxyString` (parse_uri_list step), `clientConverter` (Sub-Store from `MihomoEnv.Converter`; unwired → `*HookMissingError` raised only when a URI node string needs converting).

Collaborators: only Sub-Store `convert_proxy_string` (via `reject_surge_dialers` → `convert_node_to_mihomo`), taken from C18's `MihomoEnv` (`mihomoenv.Wire`). No new hooks; no other domain is called. `generate_aggregation_provider` is only used by the dead `get_aggregation_nodes` (below).

Tests:
- Corpus `testdata/gen_surge_loon.py` (imports `gen_mihomo` for profiles and its fakes) → `surge_loon_corpus.jsonl.gz`, `surge_loon_corpus_test.go` (shared with C20): 6008 profiles each through `generate_surge_config` and `generate_loon_config` (every node type and params shape both read, structured JSON/YAML/YAML-list/flow node strings, URI strings of every scheme incl. tuic/anytls/wireguard/socks5/http/snell that parse_uri_list drops, WireGuard sections, smart groups, custom configs with repeated/odd sections and CRLF, UDP fallback/NTP, Loon-only rule types, blackmatrix7 rule sets, dialers/chains (rejected), 12 % of profiles with malformed values at random places), 6000 nodes each through `convert_node_to_surge`/`convert_node_to_loon`, 2000 proxy lists each through the `*_text` functions, 1662 groups through `convert_proxy_group_to_surge`/`_loon`, 1667 `get_proxy_group_nodes`, 1662 `_build_rules`, 28 `_sanitize_name`. **34697 rows, 0 differences**; 2676 Surge + 2477 Loon configurations byte-identical, the rest equal errors (class + message; crash classes TypeError/KeyError/AttributeError by class) and equal mutated profiles.
- **Recorded replay** (`testdata/c19_record.py`, pytest plugin, → `surge_loon_recorded.jsonl.gz`; `surge_loon_recorded_test.go`): every top-level call of the ten converter functions during the full backend suite (12552 passed under the recorder) with the Sub-Store answers: **128 distinct calls from 92 tests in 17 files, 0 differences, 0 unsupported** (surge 65, loon 36, node_surge 3, node_loon 6, proxies_surge 7, proxies_loon 5, group_surge 4, group_nodes 1, build_rules 1). Files: test_client_fixed_ip, test_config_import, test_converter_review_regressions, test_dialer_proxy, test_dialer_round2, test_generated_preview, test_health_https_defaults, test_loon_generation, test_mihomo_easytier, test_profile_generation, test_proxy_chains, test_proxy_group_regex_exclusion, test_qa_integrity, test_safe_url_queries, test_snell_shadow_tls, test_surge_loon_group_exclusion, test_udp_fallback.
- Hand-ported (`surge_loon_ported_test.go`): `test_snell_shadow_tls.py` export 2/6 (`test_surge_export_snell_*`), `test_surge_loon_group_exclusion.py` 5/5, `test_udp_fallback.py` Surge/Loon halves (disabled MATCH, Surge FINAL/native fallback, Surge base wins, Loon after-MATCH, Loon custom General, Loon port list, NTP first, NTP off ×2); Go-side: Sub-Store unwired fails closed only for URI node strings, `*_text` skip failing nodes / dialer-proxy error.
- C35 now covers `test_snell_shadow_tls.py` 3 import cases with synthetic public Parse/Build corpus rows; `TestSnellShadowTLSRoundTripThroughSurgeExporter` covers the real importer → exporter round trip (the export half is also in the C19 replay). Route/agent halves of the replayed files (config/generate/subscription/aggregation endpoints, agent publishing) → C30/C31/C34/agents.

Not ported (coordinator confirmed during review): `surge.get_aggregation_nodes` is dead code: `rg -n get_aggregation_nodes backend` returns only its definition at `backend/converters/surge.py:15`, no test or production caller. Actual aggregation export is implemented through policy-path. Under the no-caller rule it is not a missing feature. `get_proxy_group_nodes` (also no production caller) is ported because a test uses it.

Decisions / differences from Python: generated config semantics unchanged; sensitive Surge DEBUG line intentionally omitted (Python logging bug fixed). Native strings/regexp use Go Unicode behavior under PORTING's accepted CPython-only Unicode differences; all retained references/production configs match. Debug set lines use insertion order. Non-string YAML keys follow C10/C12's accepted differences. Full branch matrix: `parity/review-c19-c20.md`.

Structural debt **→ S-convert, waiting for N8 design** (coordinator explicitly excluded from this functional review): `clients.go` `pyDict`, `truthy/or2/eq`, dynamic string/list guards, `rulesetFormatAny` sentinel and `validateRulePolicies` malformed-key pre-scan; `surge.go` dynamic generate/node/group/subscription adapters and `key` probes, using `dictGet/dictItem/iterate/fstr/must/raise/catch` from the shared legacy convert layer. Public APIs must be redesigned together, not piecemeal.

Commits: 4f09d3d1 (code), f350a047 (corpus), 84ffae21 (replay + ported tests), + this PROGRESS commit.

Review notes: followed-group missing `url`/`interval` keys retain the existing null rendering; parse errors are skipped; raw objects keep the outer name. Pure `len`/unused node-map probes were removed; the remaining malformed-value rejection guards await S-convert (see debt above).

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/convert; go vet ./internal/convert/... && go test -count=1 ./internal/convert/...'` (last run, detached worktree at HEAD 84ffae21: ok 7.2 s; `-race -run "Surge|Loon"` ok)
- corpus: `rt --back server/internal/convert/testdata/surge_loon_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/convert/testdata ~/cf-test/.venv/bin/python server/internal/convert/testdata/gen_surge_loon.py 6000'` (deterministic; ~30 s)
- recorded replay: `rt --back server/internal/convert/testdata/surge_loon_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c19rec; C19_RECORD_DIR=/tmp/c19rec PYTHONPATH=.:server/internal/convert/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 --dist=worksteal -p c19_record -p no:cacheprovider; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/c19_record.py merge /tmp/c19rec'` (~3 min)

## C20 — Loon converter

**Status: `verified`** (independent functional review 2026-11-17; no open functional findings) · prereqs: C19 · Go: `internal/convert` (`loon.go`, shared `clients.go`)

Files → Go:
- `backend/converters/loon.py` (764) → `convert/loon.go`: `GenerateLoonConfig(ctx, config, baseURL)`, `ConvertNodeToLoon(node) (line, err)` ("" = None), `ConvertProxyGroupToLoon(group, members)`, `ConvertProxiesToLoonText(proxies, toNodes)`, `BuildLoonRules` (= `_build_rules`), `SanitizeLoonName` (= `_sanitize_name`), constants `LoonDefaultGeneral`, `LoonBuiltinPolicies`, `LoonRuleTypes`, `LoonVmessCiphers`, `LoonDefaultTestURL`; private `effectiveLoonGroup`, `loonGroupSources`, `loonGroupMemberRefs`, `nameAllocator`, `resolveLoonNode`, `_quote/_value/_bool/_first/_transport_opts/_tls_opts/_shadow_tls_opts` ports.
- The group-pruning loop iterates in insertion order where Python iterates a set: the result is the greatest fixed point either way; only the order of the debug lines differs.

Collaborators, tests, commands: shared with C19 (same corpus, replay and ported test files; see C19). Loon-specific hand-ported: `test_loon_generation.py` 5/10 (`test_unsupported_transports_are_skipped`, `test_default_general_keeps_fake_ip_range_in_tun`, `test_logical_rules_with_unsupported_subrules_are_commented`, `test_node_formats_follow_loon_docs`, policy half of `test_loon_rejects_unknown_policy_and_chains`; the chain half tests C12's `reject_surge_dialers`); `test_surge_loon_group_exclusion.py` Loon cases 2/2; `test_udp_fallback.py` Loon halves. Every generator call of `test_loon_generation.py` (12) is in the replay. Deferred: `test_loon_public_url_requires_token`, `test_loon_url_names_config_after_profile`, `test_custom_loon_config_round_trip` (routes → C34), `test_subscription_serves_loon_nodes` (route → C30; its `convert_proxies_to_loon_text` call is replayed), generation half of `test_loon_config_sections` replayed, its repository setup is C04's.

Decisions / differences from Python: no generated config semantic differences. Struct keys fix remote-cache collisions; strings/regexp follow the same accepted Go Unicode classes as C19. Structural debt **→ S-convert, waiting for N8 design**: `loon.go` dynamic generate/node/group/subscription wrappers, `key` probes for cipher/strategy/IDs, `must/raise/catch`, and shared `clients.go` maps/guards/sentinel/policy pre-scan. Coordinator confirmed these are excluded from this functional review; exact functions and shared dependencies are listed in `parity/review-c19-c20.md`.

Commits: same as C19.

Review C19/C20 (2026-11-17, independent reviewer):
- Medium: Loon remote cache concatenated `(kind, resource ID, exclusion)` with `\x01`; an ID/exclusion containing that delimiter aliases another source. Fixed with a Go struct map key; `review_clients_test.go::TestLoonRemoteCacheKeepsSourceAndExclusionSeparate` checks both remote lines and group references.
- High: Surge DEBUG logging exposed the complete generated group line, including subscription URLs and `config_token`. Removed content from that log; `TestSurgeDebugLogOmitsSubscriptionURLsAndTokens` verifies the DEBUG path without leaking either. This fixes a Python logging bug; generated config semantics are unchanged.
- Local native cleanup: strings.TrimSpace/ToLower/ToUpper, strconv.Itoa, regexp RE2 with Unicode decimal/space classes; removed emulated len and unused nodesByID exception probe; renamed shared helpers to clients.go. Full convert suite on `.63` passed (6.535 s).
- Scope confirmed by coordinator: this review closes functional findings; package-wide dynamic types and raised/caught Python errors are structural debt **→ S-convert, waiting for N8 design**. No public API rewrite in this review.
- Baseline `.32`: `rt -- server/scripts/proddiff.sh` exit 0, `baseline: new=0 stale=0`; 2 Surge + 2 Loon configs and 4 subscriptions per client succeeded. Production source trace proved the snapshot misses custom sections, follow mode, HTTP/WS transport and ShadowTLS; synthetic references cover those branches (matrix linked below).
- Review commits: `48b40c42` (cache fix), `632b0fb7` (native local cleanup + sensitive log fix), `a4d0fa36` (306 independent boundary references + branch matrix).
- Added independent `testdata/review_clients_gen.py` → `review_clients.jsonl.gz`, public-boundary regression `TestClientReviewBoundaryReferences`: **306 rows, 16 expected rejections, 0 differences** on `.63`. Full source walk / production trace / boundary matrix / remaining S-convert functions: `parity/review-c19-c20.md`. Deliberately restoring delimiter concatenation made the direct regression fail; original fixed file restored.
- Authoritative `.32` acceptance exit 0: `gofmt -l internal/ tools/` clean; `go vet ./...` PASS; `go test -race -count=1 -timeout=30m ./internal/convert/` → `ok configflow/internal/convert 57.529s`; `go test -count=1 ./...` all packages passed (convert 17.335s; final lines `ok configflow/internal/weburl 0.005s`, `ok configflow/internal/yamlx 0.635s`, generator `[no test files]`); `server/scripts/proddiff.sh` → four self-checks PASS, `coverage_regressions=0`, `new=0`, `stale=0`, `baseline: new=0 stale=0`. Surge/Loon each 2 main + 4 subscription success outputs; 5 existing global baseline differences remain unrelated.
- Post-rebase `.32` acceptance also exit 0 after integrating shared rule helper changes: gofmt clean, vet PASS, convert race **62.514s**, full suite all PASS (convert **19.667s**, installer **174.985s**, yamlx **0.821s**), proddiff four self-checks PASS, `coverage_regressions=0`, `new=0 stale=0`; each client still 2 main + 4 subscription successes.
- Complete: no open functional findings; no public API changes/new dependencies; no push. Final branch is rebased onto local `go`; structural debt is assigned to S-convert per coordinator decision.

## C21 — MosDNS converter

**Status: `verified`** (review 2026-11-16: a499fc3c; see "Review C21") · prereqs: C14 · Go: `internal/mosdns` (new)

Files → Go:
- `backend/converters/mosdns.py` (1261) → `mosdns/content.go` (`ConvertRuleContentForMosDNS(content, part...)` — no part = Python None; `DomainPatternForMosDNS` (ok=false = None); `ParseClashRuleValue`; `RulesetParts`; `EmptyRulePart`; `PartDomain`/`PartIP`/`RuleParts`; `DNSRuleTypes`), `upstreams.go` (`ParseDNSUpstreams(any)`), `layout.go` (`SplitRulesAndRulesets`, `RulesetFiles` (= `_ruleset_files`), `BuildRuleProxyURL` (= `_build_rule_proxy_url`; part/sourceID `any`, nil = None), `ParseCustomMatches` (= `_parse_custom_matches`), private tag map / single-rule groups / layout), `generate.go` (`GenerateMosDNSConfig`, `GetMosDNSRulesetDownloads`, `GetMosDNSCustomFiles`, `UnsupportedYAMLError`; YAML via `yamlx.IndentDump` = `yaml.dump(Dumper=IndentDumper, allow_unicode=True, default_flow_style=False, sort_keys=False, indent=2)`, the only dump call), `py.go` (Python semantics: raise/catch into `*convert.PyError{Class, Message}`, dict hashing with 1 == 1.0 == True and "unhashable type" errors, `in` on list/str/dict, `list + list` / `str + str` TypeErrors, `int()`, `'\n'.join` TypeError, deepcopy with memo so aliased upstream mappings keep their YAML anchor in the fallback copy).
- Shared helpers used (no private copies): `pystr` (Strip/Lower/Upper/IsSpace/Int), `yamlx` (`LoadWith{KeepNonStringKeys}`, `Lookup`, `IndentDump`), `rules.ValidPattern`/`HasYAMLPayload`, `convert.ParseHostsPairs`. New exports in `convert/exports_mosdns.go` (wrappers only, no behaviour change): `AppendURLQuery` (urlsplit error → ValueError PyError), `PySplitlines`, `PyReEscape`, `IsIPNetwork` (= ip_network(strict=False) acceptance).
- Not ported: `_sanitize_rule_value_for_tag` — no caller in `backend/` (grep), no test (maintainer's rule for code without callers, decision 12).
- For C22 (oxidns imports): `DNSRuleTypes`, `BuildRuleProxyURL`, `ParseCustomMatches`, `RulesetParts`, `ConvertRuleContentForMosDNS`, `ParseDNSUpstreams`, `SplitRulesAndRulesets` are exported with Python's crash semantics as errors.

Verification:
- **Random corpus** `testdata/gen.py` → `corpus.jsonl.gz`, `rows_test.go` `TestCorpus`: 6005 profiles (fixed + random; 70 % well-formed, 30 % "wild" with malformed values at random places) each through generate / downloads / custom_files / split, 3986 `_ruleset_files` + 3986 `_build_rule_proxy_url`, 6000 rule contents (Clash/list/native/YAML block+flow, CRLF/\r/\v/U+2028/U+0085 separators, BOM, Unicode case (ß, İ, ı), wildcard patterns, IPv4/IPv6/netmask/zone/invalid CIDRs, scalar/dict/binary/date payloads, aliases) × parts None/domain/ip/""/other, 73 domain patterns, 1500 classical lines, 6000 upstream definitions (YAML list/map/strings, line format with bootstrap/enable_pipeline, indentation, comments, aliases, non-string keys, malformed), 1500 custom-match lists, ruleset parts. Settings covered: rule sets of every behavior incl. duplicates, name collisions with builtin/single-rule tags, `ruleset_` ids, shared libraries, relative/absolute/malformed URLs, server_domain/base URL/profile_id, token (missing/empty/non-str), single rules of every type (adjacent grouping), direct/proxy id lists (list/str/dict/None), custom matches head/tail, custom_config (anchors, timestamps, binary, big ints, inf/nan, non-mapping roots, invalid YAML, servers/data_providers), log/api toggles, all cache fields incl. int() edge values, local/remote/fallback DNS, hosts, default_forward, listen_address (dict/list → anchor). **47072 rows, 0 differences**; 492 rows accepted (see differences).
- **Recorded replay** `testdata/c21_record.py` (pytest plugin) → `recorded.jsonl.gz`, `recorded_test.go` `TestRecordedPythonTests`: every top-level call of generate_mosdns_config, get_mosdns_ruleset_downloads, get_mosdns_custom_files, convert_rule_content_for_mosdns, domain_pattern_for_mosdns, parse_clash_rule_value, parse_dns_upstreams, _build_rule_proxy_url, _ruleset_files, split_rules_and_rulesets, _parse_custom_matches, _ruleset_parts during the full backend suite (12552 passed under the recorder): **5436 distinct calls from 1432 tests in 92 files, 0 differences, 0 unsupported, 0 accepted** (proxy_url 2824, split 1088, domain_pattern 569, convert 416, parse_clash 285, downloads 76, generate 74, custom_files 47, upstreams 45, custom_matches 8, ruleset_parts 3, ruleset_files 1). Callers covered include converters/oxidns, OxiDNS common routing/updates, deployment bundle, rule DNS, router desired state, MosDNS archive/routes.
- Hand-written Go test (`unsupported_test.go`): the documented refusal and that Python's own errors still come first.

Python test files (rule 4 coverage):
- `converters/test_mosdns.py` (16): replay — all three classes (MosdnsCacheConfigTest, MosdnsRuleConversionTest, MosdnsIpRuleConfigTest) recorded (32 calls); the tests only assert on these outputs.
- `test_domain_wildcard_oxidns_edges.py` (2): `test_domain_edges_match_page_and_dns_conversion` — converter half replayed (39 calls); `inspect_domain_content` half is C14's, `RuleConfigMatcher` half C08's, `convert_mosdns_rules` half → agents A11 (deployment bundle). `test_native_oxidns_domain_edges` → C29/A11 (runs the native OxiDNS harness).
- `test_ruleset_domain_format.py` MosDNS conversions (deferred here by C14): replay — `test_domain_prefix_errors_preserve_text_and_valid_patterns_keep_scope` and `test_connection_add_saves_correct_domain_syntax_and_preserves_payload` conversions recorded; their route/bundle halves stay with C32/C33/C37/A11.
- Other files whose converter calls are replayed (their own subject belongs elsewhere): test_converter_review_regressions, test_mosdns_rule_fields, test_mosdns_regex_delivery, test_domain_wildcard_delivery/inputs, test_mosdns_archive, test_remote_rule_consumers (`_ruleset_files`), test_profile_generation, test_profile_scoped_artifacts, test_safe_url_queries, test_dns_watchdog_config/core, test_client_fixed_ip, test_agent_deployments, test_agent_deployment_downloads, test_mihomo_classical_delivery, converters/test_oxidns and the OxiDNS/router/agent files listed by the recorder.

Decisions / differences from Python:
- *(Porter's original decision — narrowed in Review C21: only unholdable values that reach the output are refused, non-string-key mappings are dumped.)* YAML that the jsonx model cannot hold (non-string keys, `!!set`/`!!omap`/`!!pairs`, recursive aliases): a custom_config with such values that would reach the output (top-level or outside log/api/plugins/servers/data_providers), or upstream mappings with non-string keys, → `*UnsupportedYAMLError` (Python dumps them with yaml.Dumper); raised only after every error Python would raise first. An unrepresentable custom_config is refused even when Python would drop that part (e.g. `plugins: !!set {a}`). In `convert_rule_content_for_mosdns` and `parse_dns_upstreams` such a document is treated like a YAML parse failure (text / line format) where Python would use it. Corpus: 492 rows, all on inputs flagged `unholdable` by Python; the replay has none.
- Python set iteration order (only in the `!!set` payload case above) is not reproduced.

Commits: 74424f5e (code), 8a88fbec (corpus), 32784c24 (replay, refusal test), + this PROGRESS commit.

Left: nothing for the port. C22 builds on the exports above; C28 (archive/routes) and A11 (deployment bundle) call `ConvertRuleContentForMosDNS`, `RuleParts`, `EmptyRulePart`, `ParseClashRuleValue`, `DomainPatternForMosDNS`.

Reviewer hints: `singleRuleGroups` (short-circuit order before `ident not in selected`, `kind in dict` hashes kind), `rulesetLayout` source-tuple hashing, eager default in `tagDefault`, `parseDNSUpstreams` fall-through cases, `dumpInterval` (OverflowError not caught), identity of `listen_address` (written twice → anchor) and of aliased upstream mappings through the fallback deepcopy.

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/mosdns; go vet ./internal/mosdns/ ./internal/convert/ && go test -count=1 ./internal/mosdns/'` (last: ok 2.2 s; `-race` ok 16.6 s)
- corpus: `rt --back server/internal/mosdns/testdata/corpus.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/mosdns/testdata/gen.py'` (deterministic, 6000 per kind, ~30 s)
- recorded replay: `rt --back server/internal/mosdns/testdata/recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c21rec; C21_RECORD_DIR=/tmp/c21rec PYTHONPATH=.:server/internal/mosdns/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 --dist=worksteal -p c21_record -p no:cacheprovider; PYTHONPATH=.:server/internal/mosdns/testdata ~/cf-test/.venv/bin/python server/internal/mosdns/testdata/c21_record.py merge /tmp/c21rec'` (~3 min)

### Review C21 (adversarial, 2026-11-16)

Python `converters/mosdns.py` walked against `content.go`, `upstreams.go`, `layout.go`, `generate.go`, `py.go` and `convert/exports_mosdns.go`, following the porter's hints (`singleRuleGroups` short-circuit order, source-tuple hashing, eager `tagDefault`, upstream fall-through, `dumpInterval` OverflowError, anchor identity through the fallback deepcopy). The existing replay (5436 calls, 0 differences) and corpus (47072 rows) were re-run after every change.

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | medium | The accepted difference "refuse `custom_config` with unholdable YAML even in sections Python drops" was observable for plausible inputs: a `!!set`/`!!omap`/`!!pairs` value or a recursive alias inside `log`, `api`, `plugins`, `servers` or `data_providers` (replaced or dropped by Python), or as a non-dict root (Python uses the defaults), made Go refuse the whole configuration | yamlx `LoadOptions.KeepUnrepresentable` (opt-in) puts `*yamlx.Unrepresentable` in place of such values; MosDNS refuses only those that reach the output (`checkDumpable`) — a499fc3c | `TestReviewUnholdableYAMLOutsideTheOutputMatchesPython` (Python outputs in `testdata/review_c21_ref.json`, from `review_c21_ref.py`) |
| 2 | medium | Mappings with keys that are not strings (`1: y`, `null:`, `1.5:`, `!!binary` keys) in upstream definitions or in `custom_config` parts that reach the output were refused although yamlx's dumper writes them exactly like PyYAML (`pairMapping`); a custom root with such keys was refused too | the root is kept as `*yamlx.Mapping` with Python's key order (`log`/`api`/`plugins` first, `servers`/`data_providers` dropped, deleted and appended keys as in the dict), nested mappings are dumped — a499fc3c | same test (`generate:` and `generate-nolog:` rows) + corpus: 158 upstream rows and 130 generate rows formerly "accepted" are now equal |
| 3 | medium | (found while fixing 2) the fallback forwarder's deepcopy did not copy `*yamlx.Mapping` upstreams, so a non-string-key upstream used by both forwarders would be dumped as `&id001`/`*id001` where Python's `deepcopy` gives two plain copies | `deepCopy` copies `*yamlx.Mapping` with the memo — a499fc3c | corpus (25 generate rows) |
| 4 | low | `convert_rule_content_for_mosdns` read a whole YAML document as text when anything in it was unholdable (Python uses `payload` and skips non-str items); `parse_dns_upstreams` likewise fell back to the line format | same placeholder load; only a `payload` that is itself a `!!set` is still read as text — a499fc3c | review references (`convert:`/`upstreams:` rows) |

Accepted differences (after the fixes): values jsonx cannot hold that **reach the output** (e.g. `extra: !!set {a}` or `r: &r [*r]` at a kept top-level key, such a value inside an upstream mapping) → `*UnsupportedYAMLError`, raised after every Python error (Python dumps them with `yaml.Dumper`); a `payload` that is itself a `!!set` (iterated by Python in hash order, not reproducible) is read as text. Corpus: **45 accepted rows** (was 492): 25 generate refusals + 20 `!!set` payload conversions; `rows_test.go` now accepts output differences only for `convert` rows — every other unholdable row must be refused or equal. Replay: 0 accepted.

Checks performed (no finding): `_normalize_ruleset_id`, execution order (`known` set, falsy ids), tag map (builtin + reserved single-rule tags, `_ip` suffix space for classical, `_2` counters), single-rule groups (adjacency, `ident not in selected` after the item checks, `kind in dict` hashing), layout source tuples, `_ruleset_files`/local paths/IP tags, rule-proxy URLs (`profile_api_path`, `/api` prefix, query order url/source_id/part/token, token type check), `empty_rule_part`, `domain_pattern_for_mosdns` (all wildcard forms, `re.escape`), content conversion (BOM, MosDNS-format sniffing of 20 lines, YAML payload detection, native prefixes, Clash comma rules incl. DOMAIN-REGEX commas, list IP/CIDR via `ip_network(strict=False)`), `parse_dns_upstreams` (strip, list/dict/str items, line format with indentation/`-`/`#`, `bootstrap`/`enable_pipeline`, a bare second part), generation (custom base, log/api toggles, cache fields and `int()` edges, hosts, forwarders, sequences, custom matches head/tail, `listen_address` written twice → anchor, key order), downloads and custom files. Rule 4b: the module does no I/O and gets no request context. Decision 8: no private copies left (`pystr`, `convert.PySplitlines`/`PyReEscape`/`AppendURLQuery`).

Coverage map: the porter's list above stands (replay of every C21 call in 92 files; `test_native_oxidns_domain_edges` → C29/A11), checked against the recorder's file list. Consumers re-tested after the yamlx/mosdns changes: `internal/oxidns` (C22–C24; on a private worktree of HEAD because the shared tree held C24's unfinished work), `convert`, `substore` (+ difftest), `yamlx`, `provider`.

Open findings: none.

Commands (review): the porter's, plus `rt --back server/internal/mosdns/testdata/review_c21_ref.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/mosdns/testdata/review_c21_ref.py > server/internal/mosdns/testdata/review_c21_ref.json'`; `C21_SHOW_ACCEPTED=1` logs the accepted corpus rows. Last run: `ok configflow/internal/mosdns` (3.6 s; `-race` 18.0 s), corpus "45 accepted, 0 differ", replay "0 differ".

## C22 — OxiDNS converter, native loader, references, match targets

**Status: `verified`** (由 S-oxidns 复核覆盖；覆盖核对及 .32 验证通过，2026-11-17) · prereqs: C05, C21 · Go: `internal/oxidns`

**Coverage audit, 2026-11-17 (verified; final checks passed):** 由 S-oxidns 复核覆盖：`672fc0d0`、`0164424d`、`da0aa993`，协调复验/合并 `dc3453c2`。范围为 converter/native/references/match_targets 的生成、资源/附件、include/限额、引用/目标各分支；当前边界选择 13,245 条录制、41,217 条随机参考。逐文件依据和 HTTP 排除边界见 [review-c22c26.md](review-c22c26.md)。下文为历史移植记录，旧 pending/stub/representation 描述已由 S 重写替换；旧语料总数不代表现行测试行数。

Files → Go (`server/internal/oxidns/`):
- `backend/converters/oxidns.py` (490) → `converter.go`: `GenerateOxidnsConfig` (YAML via `yamlx.SafeDumpUnicode` = `yaml.safe_dump(allow_unicode=True, sort_keys=False)`), `GetOxidnsRulesetDownloads`, `GetOxidnsCustomFiles`, `UnavailableRulesets`, `RulesetParts` (= `ruleset_parts`), `RoutingGroups(config, settings, Legacy{None,All,ContentAware}, items, itemsGiven)` (= `_routing_groups`), `HostsFileContent`, `Forward`, `FallbackUpstreams`, `Items`, `Base`, `TagFor` (= `_tag`), `RulePath`, `LocalDNSDefaults`/`RemoteDNSDefaults`; `_selected` is the lazy `selected(..., yield)` (errors keep Python's generator order).
- `backend/common/oxidns_native.py` (264) → `native.go`: `LoadNativeYAML(text, label)` (yamlx `SchemaNative` + `Key` hook = `NativeLoader.construct_mapping`; Python's default label `DefaultNativeLabel`), `ManagedFiles` (→ `[]ManagedFile{Path, Content []byte}`, `.Object()` = Python dict), `NativeDocument(settings, validate, *NativeSources)` (`sources` → `NativeSources{Plugins, Includes}`), `ResourceReferences`, `RelativeResource` (nil = None), `ValidateResourceFiles`, `Tag` (= `TAG`), limits `Max*`.
- `backend/common/oxidns_references.py` (64) → `references.go`: `ReferenceLocations(document, tags, dynamic)` (tag patterns through `rules.CompilePyRegex`, `re.escape` = `convert.PyReEscape`), `ReferenceReason(references, prefix)` (default `DefaultReferencePrefix`), `NewReferenceError` (= `OxidnsReferenceError`, `Error.References`).
- `backend/common/oxidns_match_targets.py` (61) → `matchtargets.go`: `ReadMatchTargets` (all calls pending: needs `collect_reference_issues`, C23).
- `backend/native_test_helpers.py` (34): test helper of the real-binary Mihomo tests (socket polling); no production code — not applicable.
- Ported early because C05/C22 call them (real functions in the shared package; their modules add the rest to the same files): `rule_dns.py` (C27) → `ruledns.go`: constants (`Actions`, `ListRoutes`, `InlineExec`, `RouteLabels`, `RealWrappers`, prefixes, `BlackholePlugin`), `EntryKey`, `RouteKey`, `RouteExec`, `NormalizeAction`, `_enabled`, `upstream_chain`, `HelperPlugins`, `Convertible`, `has_ip_part`. `oxidns_device_policy.py` (C26) → `devicepolicy.go`: `NormalizeClientRange`, `NormalizePolicies`. `oxidns_validation.py` (C23) → `validation.go`: `Schemas()` (reads `backend/schemas/oxidns/*.json` under `resource.FromEnv()`, once), `ValidateSchema`, `FormatError` (all formats except `timezone`, `dns-record`, `domain-expression` → pending), `FormatPath`, `MaxIssues`, `OxidnsValidationError` (`validationError`), `_endpoint`, `_uint`, `dns_code_valid`, `_is_type`. Needed because template generation validates the live download options (`timeout`/`outbound`/`socks5`) with `validate_schema`.
- `pending.go`: stubs raising `*PendingError` for ~~`validate_expanded_document`, `require_valid_document`, `collect_reference_issues` (C23)~~ (replaced by C23), `prepare_editor` (C26, added by C23), `migrate_domain_only_routing` (C25), `prepare_native_rule_updates`, `patch_document` (C26), `rule_ip_presence.classical_ip_lines` (routers domain; provide through a hook, decision 3). **Each later porter replaces its stub, deletes it from pending.go and re-runs this package's replay; the pending rows must turn into identical rows.**
- Shared helpers used (no private copies): `mosdns` exports (`SplitRulesAndRulesets`, `BuildRuleProxyURL`, `ParseCustomMatches`, `RulesetParts`, `ConvertRuleContentForMosDNS`, `ParseDNSUpstreams`, `DNSRuleTypes`, `UnsupportedYAMLError` (type alias)), `convert` (`PySplitlines`, `PyReEscape`, `IsIPNetwork`, `AppendURLQuery`, new wrapper file `convert/exports_oxidns.go`: `ParseIPAddress` = `ipaddress.ip_address`), `pystr`, `urlx.Split` (upstream/http-url formats), `yamlx`, `rules.CompilePyRegex`, `resource`. Package-local Python glue (`py.go`: raise/catch, get/item/truthy/str, memo deepcopy that keeps YAML aliases) follows the `mosdns`/`convert` pattern.

Verification:
- **Recorded replay** `testdata/c22_record.py` (pytest plugin) → `recorded.jsonl.gz`, `recorded_test.go` `TestRecordedPythonTests`: every top-level call of the C05/C22 functions above plus the early-ported `normalize_action`, `route_key`, `route_exec`, `entry_key`, `helper_plugins`, `normalize_client_range`, `normalize_policies`, `validate_schema` (schema objects recorded as `{"$schema": path}` into `schemas()`), `format_error`, and `OxidnsReferenceError(...)` construction, during the full backend suite (12551 passed; 1 unrelated failure `test_docker_ha_compact.py::test_concurrent_first_starts_share_identity_and_enroll_once`): **29539 distinct calls from 2522 tests in 100 files: 27140 identical, 0 differ, 0 not expressible, 0 unsupported; 2399 pending** (after C23, 2026-11-16: **28485 identical, 0 differ, 1054 pending** — C26 605, C25 448, routers 1; see C23) (validate_expanded_document 1605, collect_reference_issues 762 = every read_match_targets, patch_document 22, plugin formats 7, timezone 2, classical_ip_lines 1). Identical per function: routing_groups 6807, reference_locations 6331, validate_schema 3475, normalize_client_range 2125, items 1580, ruleset_parts 1498, normalize 1417, load_native_yaml 1067, generate 992, resource_references 491, native_document 278, append_url_query 217, validate_resource_files 108, helper_plugins 90, base 81, relative_resource 78, rule_path 68, normalize_policies 67, custom_files 51, downloads 40, entry_key 38, forward 36, route_exec 33, normalize_action 31, reference_reason 30, route_key 25, unavailable_rulesets 19, reference_error 17, format_error 16, managed_files 14, tag 14, hosts 4, fallback 2. (Most downloads/custom_files/generate rows of the suite are native profiles → pending C23.)
- **Random corpus** `testdata/gen.py` → `corpus.jsonl.gz`, `TestCorpus`: 60870 rows, synthetic values only: 6000 profiles × generate/downloads/custom_files (half "valid" profiles toggling every feature: cache/dump/probe, API auth/TLS/WebUI, log file, recorder, IP selector, dynamic set with learn direct/proxy, AdGuard content/URLs, RouterOS address list/route with empty gateways, rule update with valid/invalid timeout/outbound/socks5, custom matches head/tail, hosts, custom_config with aliases/timestamps/binary/runtime/plugins/duplicate tags, upstream texts in every format, rule actions incl. pinned upstreams; half malformed), routing_groups (legacy False/True), items/base, unavailable_rulesets, 1500 hosts files (all Python line separators, comments, legacy aliases, Unicode digits, scoped IPv6), 1442 upstream texts × forward/fallback, 2500 native YAML texts (YAML 1.1 vs 1.2 scalars, 0x/0o/0b/underscores, .inf/.nan, unsafe/duplicate/non-string keys, aliases, recursive aliases, !!set/!!binary/!!timestamp/!!omap, depth 30–200, 50000-node limit, alias blow-up, >2 MiB), 3500 attachment lists (paths, reserved names, conflicts, encodings, 2000 random base64 strings), 2000 include trees (nesting, cycles, `..`, missing, non-UTF-8, globals, sources on/off), 1500 documents × resource_references/validate_resource_files, references/reasons/reference errors, rule DNS parts, 1500 client ranges, policies, tags, 2000 download-option schema checks, 3000 format_error rows: **0 differences**, 105 accepted (Unsupported on rows Python marks unholdable), 159 pending (C23 native validation, C26 patch_document); after C23: 65 pending (C25 32, C26 33), 0 differences.
- Hand-written (`unsupported_test.go`): the documented refusal and that Python's errors still come first; pending is reported, never answered.

Python test files (rule 4 coverage):
- `test_oxidns_native.py` (18): replay — 109 calls from 69 test ids (load_native_yaml, native_document, managed_files, resource files, normalize); rows needing native validation are pending C23.
- `test_oxidns_match_targets.py` (10): replay — 475 calls from 16 test ids; `read_match_targets` rows were pending C23; since C23 all 762 are identical.
- `test_oxidns_domain_only_core.py` (6): not runnable here — skipped without `OXIDNS_TEST_BINARY` (real OxiDNS core); its inputs are `generate_oxidns_config`, which is replayed. Not applicable until a core binary is available (then record it like the rest).
- `test_classical_native.py` (1), `test_domain_wildcard_mihomo_native.py` (1), `test_mihomo_yaml_native.py` (1): real Mihomo binary tests (skipped without `MIHOMO_BINARY`), only linked to C22 through `native_test_helpers.py`; not applicable (Mihomo side belongs to C14/C18).
- `converters/test_oxidns.py` (43, listed under C27): replay — 254 calls from 131 test ids, identical except native/device-policy rows pending C23/C26.
- Other files whose C22 calls are replayed: the 100 files listed by the recorder (OxiDNS common/routing/editor/lifecycle/HA/archive/apply/validation/rule DNS/agents deployment/gateway/router files).

Decisions / differences from Python:
- YAML the jsonx model cannot hold (`custom_config` or upstream texts with non-string keys, `!!set`/`!!omap`/`!!pairs`, recursive aliases) that would reach the output → `*UnsupportedYAMLError` (= `mosdns.UnsupportedYAMLError`): a top-level non-string-key `custom_config` and unrepresentable values are refused when loaded, nested non-string-key mappings just before dumping (so Python's earlier errors come first). Corpus: 105 accepted rows, replay none.
- `LoadNativeYAML`: sets/tuples/recursive aliases are reported with the message Python gives when its traversal reaches them; when the same document would first hit another limit in Python's pre-order walk, the message can differ (no case in replay or corpus).
- `ReadMatchTargets` and every native generation path are pending until C23/C25/C26.

Commits: 924f37da (code + replay), 9c9e2310 (corpus, base64/str-membership fixes), 29c7a74d (refusal/pending tests), + this PROGRESS commit.

Left: nothing else for C22 itself. For later porters: C23 replaces `validateExpandedDocument`, `requireValidDocument`, `collectReferenceIssues`, the `timezone` and `dns-record`/`domain-expression` formats in `validation.go` (keep `ValidateSchema`/`FormatError` as they are verified by 3475 + 2000 rows); C25 `migrateDomainOnlyRouting`; C26 `prepareNativeRuleUpdates`, `patchDocument` (and the rest of `devicepolicy.go`); C27 the rest of `ruledns.go`; routers `classicalIPLines` via a hook. Re-run `TestRecordedPythonTests`/`TestCorpus` after each (the recorded file already has Python's answers for the pending rows; re-record only if Python changed).

Reviewer hints: `selected` membership (`in` on list/dict/str selectors, unhashable identities), aggregation in `routingGroups` (`setdefault(...).extend`), key positions of `runtime`/`log`/`api`/`plugins` in the result (pop vs replace), slice/object identity before dumping (no accidental aliases: every list is freshly allocated), `purePosixPath` (WebUI index, download dir/filename), strict `b64decodeValidate` (CPython 3.11 `a2b_base64(strict_mode=True)`), `durationValid` (re.I `ſ`), `endpoint`, `hostsFileContent` (line endings kept, `rstrip('\r\n')` only).

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/oxidns; go vet ./internal/oxidns/ ./internal/convert/ && go test -count=1 ./internal/oxidns/'` (last: ok 19.2 s; `-race` ok 137 s)
- corpus: `rt --back server/internal/oxidns/testdata/corpus.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/gen.py'` (deterministic, ~40 s; optional scale argument)
- recorded replay: `rt --back server/internal/oxidns/testdata/recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c22rec; C22_RECORD_DIR=/tmp/c22rec PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 --dist=worksteal -p c22_record -p no:cacheprovider; PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/c22_record.py merge /tmp/c22rec'` (~3.5 min)

## C23 — OxiDNS validation and constraints

**Status: `verified`** (由 S-oxidns 复核覆盖；覆盖核对及 .32 验证通过，2026-11-17) · prereqs: C22 · Go: `internal/oxidns`

**Coverage audit, 2026-11-17 (verified; final checks passed):** 由 S-oxidns 复核覆盖：`672fc0d0`、`0164424d`、`da0aa993`，协调复验/合并 `dc3453c2`。范围为 validation/plugin/global/reference constraints 的 schema、格式/跨字段约束、引用图各分支，含 zone/IDN、中文错误；当前选择 1,330 条录制、21,700 条随机参考，global/reference 私有 helper 通过完整 validator 和 ReadMatchTargets 执行。逐文件依据见 [review-c22c26.md](review-c22c26.md)。下文为历史移植记录，旧 stubs/Unicode 表/schema 外部加载等已由 S 重写替换。

Files → Go (`server/internal/oxidns/`):
- `backend/common/oxidns_validation.py` (310) → `validation.go` (C22 had ported `schemas`, `validate_schema`, `format_error`, `format_path`, `OxidnsValidationError`): now `ValidateNativeConfig`, `RequireValidDocument`, `ValidateExpandedDocument(document, sources-dict)` (internal `validateExpandedDocument(*jsonx.Object, *NativeSources)` for `native_document`), `ValidateSettings(settings, config, baseURL)` (calls `prepareEditor`, **pending C26**), `format_error` `timezone` (`zoneinfo.go`) and `dns-record`/`domain-expression` (→ `validatePluginFormat`).
- `backend/common/oxidns_plugin_constraints.py` (676) → `constraints_plugin.go` (`CollectPluginIssues`, `ValidatePluginFormat`, `_tokens`/`_uint`/`_duration`/`_regex_error`/`_domain_error`/`_cron_error`/`_adguard_error`) + `zone.go` (`_zone_records_error`) + `dnstext.go` (the dnspython 2.7.0 subset it needs: zone tokenizer, `dns.name.from_text`/`from_unicode`, `dns.ttl.from_text`, `dns.ipv4`/`dns.ipv6.inet_aton`, `dns.rdata.from_text` for A AAAA CNAME(+aliases) NS PTR DNAME MX RT AFSDB RP HINFO TXT/SPF(+AVC/RESINFO) SOA SRV NAPTR CAA; MINFO has no dnspython class → GenericRdata → always invalid in text form, as in Python) + `idna2003.go` (`encodings.idna.ToASCII`: nameprep on `ucd_3_2_0`, NFKC as CPython computes it, punycode).
- `backend/common/oxidns_global_constraints.py` (188) → `constraints_global.go` (`CollectGlobalIssues`).
- `backend/common/oxidns_reference_validation.py` (368) → `references_validation.go` (`CollectReferenceIssues(document, catalog, graph, &order)`; quick expressions, references, iterative DFS cycle check). Replaced C22's stubs in `pending.go` (`validate_expanded_document`, `require_valid_document`, `collect_reference_issues`, the three formats); added stub `prepareEditor` (C26).
- Helpers: `pyre_c23.go` (`re.split(r'[\s,]+')`, `split(None, 1)`, Unicode `\d`/`\b`, `re.sub(r'\bJAN\b')`, `(?m)^\s*\$(?:INCLUDE|GENERATE)\b` with re.I, template finditer, `re.compile` errors through `rules.CompilePyRegex`), `unicode_c23_tables.go` (generated by `testdata/gen_unicode_c23.py` from the server's CPython 3.11.17: `str.isalnum`, stringprep B.1/B.2/C.*/D.1/D.2, `ucd_3_2_0` NFKD per character, combining classes, primary composites).

Verification:
- **C22's former pending rows**: `TestRecordedPythonTests` **0 differ, 0 pending for C23** (was 2399 pending; now 1054 pending, all later modules: `prepare_native_rule_updates` C26 583, `migrate_domain_only_routing` C25 448, `patch_document` C26 22, `classical_ip_lines` routers 1 — rows that C23 used to stop now run further). `TestCorpus` **0 differ, 0 pending for C23** (was 159; now 65: C25 32, C26 33). Identical incl. 762 `read_match_targets`, 1025 `generate`, 504 `custom_files`.
- **Recorded replay** `testdata/c23_record.py` (pytest plugin, top-level calls of the 8 C23 public functions) → `c23_recorded.jsonl.gz`, `c23_test.go` `TestC23Recorded`: full suite 12552 passed; **2043 distinct calls from 1259 tests in 51 files: 1621 identical, 0 differ; 422 pending** (`validate_settings` → `prepare_editor`, C26). Identical: collect_reference_issues 712, validate_native_config 403, require_valid_document 256, validate_expanded_document 228, validate_plugin_format 15, collect_plugin_issues 6, collect_global_issues 1.
- **Random corpus** `testdata/gen_c23.py` → `c23_corpus.jsonl.gz`, `TestC23Corpus`: **31978 rows, 0 differences**: 2500 validate_native_config + 500 require_valid_document + 600 validate_expanded_document (sources with short origin lists → IndexError, includes) on random documents (every catalog plugin type, schema-driven args with mutations, constraint-targeted args for every rule of collect_plugin_issues, global log/api/network sections, forward upstreams/bootstrap, http_server entries, references/expressions/cycles), 6000 collect_plugin_issues, 1500 collect_global_issues, 1500 collect_reference_issues (half with graph), 1500 format_error (C23 formats) + 1500 validate_plugin_format, 4000 zone texts (single/multi; names with escapes, `\300` → Python's `struct.error`, IDN, long labels, all directives, every RDATA type, quoting/parens/comments, RFC 3597, {qname}/{qclass}), 2000 AdGuard lines, 1500 cron, 800 domain rules, 800 regexes (incl. OverflowError), 800 durations (incl. `5ſ` TypeError), 439 ZoneInfo keys (all of /usr/share/zoneinfo sampled + path tricks), 6039 IDNA labels (3706 punycode successes). Covered issue codes: all 52 codes the modules emit.
- Hand-written: `unsupported_test.go` `TestPendingNativeValidation` now checks native normalization succeeds and `ValidateSettings` reports C26 pending.

Python test files (rule 4 coverage):
- `test_oxidns_validation_api.py` (10): replay — 8 tests with rows (validate_expanded_document 3, validate_plugin_format 1 identical; validate_settings 12 pending C26). `test_unsafe_or_non_object_yaml_does_not_persist`, `test_rule_endpoint_is_authenticated` are route tests → **deferred to C29** (`/api/profiles/<id>/oxidns/settings`, `/api/oxidns/validate`); validate_settings rows → **C26** (then they run with this replay).
- `test_oxidns_validation_global.py` (7): replay — 6 tests (91 validate_native_config + 1 collect_global_issues, identical). `test_global_schema_covers_every_serialized_upstream_field` asserts the schema file's content: the Go code reads the same file — not applicable.
- `test_oxidns_validation_plugins.py` (17): replay — 15 tests (247 validate_native_config, 14 validate_plugin_format, 6 collect_plugin_issues, identical). `test_every_non_server_forward_catalog_type_has_schema_and_legal_fixture` (frontend catalog vs schema file) and `test_schema_audit_follows_nested_union_fields…` (`scripts/audit_oxidns_schema`) test data/scripts, not backend code — not applicable.
- `test_oxidns_validation_references.py` (12): replay — all 12 tests (150 collect_reference_issues, 11 validate_native_config, identical).
- Other files whose C23 calls are replayed: 47 more (OxiDNS native/common/editor/lifecycle/apply/restore/HA, rule DNS, agents, routers, setup).

Decisions / differences from Python:
- Schemas are read from `backend/schemas/oxidns` under the resource root (C22's `Schemas()`, `internal/resource`), not `go:embed` (embedding needs a copy inside `server/`; the resource root is the project's mechanism for bundled backend files). The Go image must ship `backend/schemas/oxidns` (reviewer/finish domain: decide whether to embed a copy plus an equality test).
- `ZoneInfo(key)`: Python's algorithm on the host's zoneinfo (PYTHONTZPATH or the CPython default TZPATH, `_validate_tzfile_path`, regular file, TZif magic); the `tzdata` wheel is not installed in the backend environment, so there is no fallback. Validity therefore depends on the image's `/usr/share/zoneinfo` exactly as for the Python server (the Go image needs tzdata). Only the magic is checked, CPython parses the whole file (a truncated file would differ). The ZoneInfo instance cache is not modelled (a key that loaded once stays valid in Python even if its file is removed).
- `re.compile` in `_regex_error`: `\N{name}` patterns count as compiling (accepted difference of `internal/rules`); OverflowError/RecursionError are raised as in Python (RecursionError text "maximum recursion depth exceeded" may differ from CPython's suffix).
- dnspython's `GenericRdata`/`from_wire` path for a known type given as `\#` inside `rdata.from_text` is unreachable (the caller handles RFC 3597 first) and fails closed.

Pitfalls: another agent's `rt` sync can overwrite a freshly generated corpus in the remote tree before `--back` (seen once): generate with `--back` in its own `rt` call and check the local row count. `rows_test.go`'s `callRow` falls through to `callRowC23` (c23_test.go).

Commits: 84b029c2 (code), 822c1c80 (replay + corpus), + pending test, + this PROGRESS commit.

Left: `validate_settings` rows (422 replay) run when C26 replaces `prepareEditor`. Reviewer: walk `constraints_plugin.go` against Python branch by branch (Python ints vs bools in `== 0`, `is True`, `_duration` TypeError order), dnspython tokenizer edge cases (unget of EOF, DELIMITER tokens), IDNA nameprep/NFKC tables (regenerate with `gen_unicode_c23.py` if the server's Python changes), ZoneInfo decision.

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/oxidns; go vet ./internal/oxidns/ && go test -count=1 ./internal/oxidns/'` (last: ok 20.2 s; `-race` ok 140 s)
- C23 corpus: `rt --back server/internal/oxidns/testdata/c23_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -W ignore server/internal/oxidns/testdata/gen_c23.py'` (deterministic, ~1 min)
- C23 recorded replay: `rt --back server/internal/oxidns/testdata/c23_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c23rec; C23_RECORD_DIR=/tmp/c23rec PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 --dist=worksteal -p c23_record -p no:cacheprovider; PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/c23_record.py merge /tmp/c23rec'` (~3 min)
- Unicode tables: `rt --back server/internal/oxidns/unicode_c23_tables.go -- bash -lc '~/cf-test/.venv/bin/python server/internal/oxidns/testdata/gen_unicode_c23.py'` then `gofmt -w` it on the server.

## C24 — OxiDNS common settings

**Status: `verified`** (由 S-oxidns 复核覆盖；覆盖核对及 .32 验证通过，2026-11-17) · prereqs: C23 · Go: `internal/oxidns`

**Coverage audit, 2026-11-17 (verified; final checks passed):** 由 S-oxidns 复核覆盖：`672fc0d0`、`0164424d`、`da0aa993`，协调复验/合并 `dc3453c2`。范围为 oxidns_common.py 的 ownership/capability、toggle/park/restore、fallback/API/TLS/cache/dynamic/资源/更新各分支。822 条录制、3,746 条随机参考均使用真实 C25/C26 协作函数。逐文件依据见 [review-c22c26.md](review-c22c26.md)。下文为历史移植记录；旧协作 stub/切片别名模型已不存在，不能把旧 replay 的替身结果当作当前执行方式。

Files → Go (`server/internal/oxidns/`):
- `backend/common/oxidns_common.py` (1175) → `common.go` (`ReadCommonSettings`, `ApplyCommonSettings`, `CommonFields()`, `_read` = `readCommon`, `_DIRECT`/`_API`/`COMMON_FIELDS` tables built from `DefaultSettings()` in Python's dict order, `_fallback_state`/`_prepare_fallback`/`_finish_fallback`, `_main_control`, `_dynamic_state`, `_cron_job`, `_resources`, `_adguard_state`, every `_patch_*`, `_validate_value`, `_default_args`) + `common_context.go` (`_Context` = `commonContext` with `allowed`/`ensure`/`park`/`park_plugin`/`call_state`/`toggle_call`/`resource_paths`…, `TagTypes` = `_TAG_TYPES`; Python list/dict glue: `inList`, `listRemove` (identity-or-equality like `list.remove`), `pyIter`, `pyContains`, `setItem`, `popKey`, `setDefault`).
- Early parts of later modules (no further dependencies): `common_routing.go` (C25: `Selectors`, `RuleDNSFields`, `RoutingFields` = `ROUTING_FIELDS`, `_references`, `replace_download_entries`), `editor.go` (C26: `_same` = `sameValue`, `_MISSING` = `missingValue`), `pending.go`: `_relevant` (`ruleUpdateRelevant`, early return of prepare/finish_rule_updates).
- **Collaborators of later modules are now package variables in `pending.go`** (default: `PendingError`): `prepareEditor` (C26; was a func, C23 uses it), `readRouting`, `ownedChains`, `applyRouting`, `resourceDownloads`, `refreshRuleHelpers` (C25), `prepareRuleUpdate`, `finishRuleUpdates` (C26; take `*commonContext`). Their porters assign the real functions (keep the variables or adapt `c24_test.go`'s `installCollaborators`). Until then every production read/apply is pending (all go through `prepare_editor` and `read_routing`).
- YAML aliases: Python mutates lists in place; Go slices are values. Every in-place list mutation inside the document goes through `commonContext.replaceList`, which rebinds every other holder of the same list (same backing array and length) — so aliased sequences/plugins lists behave like Python's shared list. Dicts are shared pointers as in Python.

Verification:
- **Record & replay** `testdata/c24_record.py` → `c24_recorded.jsonl.gz`, `c24_test.go` `TestC24Recorded`: every top-level `read_common_settings`/`apply_common_settings` of the full suite (12552 passed) with the answers of the C25/C26 collaborators recorded per call (sequence + sha256 of the arguments, so Go must ask the same questions in the same order; in-place collaborators also record the state they leave: document incl. alias paths, settings, tag map, parked dict). **822 distinct calls from 397 tests in 20 files: 822 identical, 0 differ, 0 pending, 0 unsupported** (read 280, apply 542).
- **Random corpus** `testdata/gen_c24.py` → `c24_corpus.jsonl.gz`, `TestC24Corpus`: 1600 template drafts with random common inputs, ~80 % mutated natively (renamed/retyped/duplicated/removed plugins with and without `renamed_plugins`, custom/partial feature calls, non-dict args, include + additional files, `${ENV}` references, API shapes incl. bearer auth/webui/str, TTL/qtype/prefer_ipv4/metrics variants incl. 10**20 numbers, fallback graphs and parked plugins, dynamic domains/learners, Hosts/AdGuard files, cron jobs, YAML-aliased args, Unicode, huge ints, junk `_common_disabled`), then read + 1–2 applies with editable fields (typed values) and random fields (malformed values, unknown fields, non-dict changes): **3746 rows (read 1600, apply 2146; 782 OxidnsSettingsError), 0 differ**. Go statement coverage of `common*.go` from both: 94–100 % for every function except Python-crash glue for odd types.
- C22/C23 unchanged: `TestRecordedPythonTests` 0 differ, 1054 pending (C25 448, C26 605, routers 1); `TestCorpus` 0 differ, 65 pending; `TestC23Recorded` 0 differ, 422 pending (C26); `TestC23Corpus` 0 differ.

Python test files (rule 4 coverage; rows from the replay):
- `test_oxidns_feature_lifecycle.py` (8 functions, 21 ids): replay — 68 calls, identical.
- Also replayed (C24 calls only; their own subjects belong to C25/C26/C29/agents): test_oxidns_common 98, test_oxidns_common_lifecycle 65, test_oxidns_fallback_lifecycle 58, test_oxidns_resource_lifecycle 75, test_oxidns_native_apply 125, test_oxidns_native_restore 113, test_oxidns_native_cleanup 42, test_oxidns_hosts_cleanup 33, test_oxidns_match_targets 42, test_gateway_onboarding 27, test_oxidns_withdrawn_downloads 21, test_oxidns_download_aliases 19, test_oxidns_common_api 12, test_profile_rule_routes 8, test_rule_sync_projection 6, test_oxidns_common_routing 4, test_oxidns_device_policy 3, test_oxidns_domain_only_native 2, test_oxidns_settings_routes 1. Nothing hand-ported (no concurrency/timing/process behaviour in this module).

Routes: none (the `/api/…/oxidns/common` routes are C29's). PORTING rule 4b (request cancellation) not applicable: no context, no I/O.

Decisions / differences from Python:
- Collaborator stubs instead of `compat` copies: C24 cannot answer anything in production until C25 (`read_routing`, …) and C26 (`prepare_editor`) land; the replay proves C24's own logic with Python's answers.
- `relinkList` cannot tell aliased *empty* lists apart (Go zero-length slices have no identity): an empty list shared through a YAML anchor and then grown in place is grown only at the edited holder (no case in replay/corpus).
- Aware YAML timestamps compare by fields in `sameValue` (Python compares instants); timestamps never reach C24 rows (recorder marks them unsupported; none in suite).

Pitfalls: replay harness only — the recorded "after" state of in-place collaborators loses YAML aliasing unless restored from the alias paths (`restoreDocument`); first run without it gave 10 false differences in `test_oxidns_download_aliases.py`. `C24_RECORD_ARGS=1` stores full collaborator arguments in the rows so a hash mismatch prints the first differing bytes (debug only; do not commit such a file).

Commits: f8ce1808 (code), 0009eaff (record & replay), 58fad245 (corpus), + this PROGRESS commit.

Left for later porters: C25 assigns `readRouting`, `ownedChains`, `applyRouting`, `resourceDownloads`, `refreshRuleHelpers` (and extends `common_routing.go`); C26 assigns `prepareEditor`, `prepareRuleUpdate`, `finishRuleUpdates` (they get `*commonContext`; use its methods) and extends `editor.go`. After each, `TestC24Recorded`/`TestC24Corpus` must still pass; once all are real, also run the rows with the real functions (drop `installCollaborators`) — the recorded outputs are Python's end-to-end results. Reviewer: walk `toggleCall` position/disabled bookkeeping, `patchResources` (urls list shared with `changes`), `patchAPI` parked-holder paths, evaluation order of Python's eager defaults.

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/oxidns; go vet ./internal/oxidns/ && go test -count=1 ./internal/oxidns/'` (last: ok 36 s)
- C24 replay: `rt --back server/internal/oxidns/testdata/c24_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c24rec; C24_RECORD_DIR=/tmp/c24rec PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 --dist=worksteal -p c24_record -p no:cacheprovider; PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/c24_record.py merge /tmp/c24rec'` (~3 min; check 822 rows locally)
- C24 corpus: `rt --back server/internal/oxidns/testdata/c24_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -W ignore server/internal/oxidns/testdata/gen_c24.py'` (deterministic, ~70 s, 3746 rows)

## C25 — OxiDNS common routing, DNS watchdog

**Status: `verified`** (由 S-oxidns 复核覆盖；覆盖核对及 .32 验证通过，2026-11-17) · prereqs: C24 · Go: `internal/oxidns`

**Coverage audit, 2026-11-17 (verified; final checks passed):** 由 S-oxidns 复核覆盖：`672fc0d0`、`0164424d`、`da0aa993`，协调复验/合并 `dc3453c2`。范围为 common_routing 的识别、迁移、ownership/follow/资源协调，以及 dns_watchdog 的纯变换全部分支。当前选择 3,525 条录制、13,404 条随机参考；本轮补充 41 条真实 Python watchdog 参考。此模块没有后台循环；Agent 注入、probe/restart 归 A01/A14/A16 及独立 Go Agent，未随 C25 标为复核。逐文件依据见 [review-c22c26.md](review-c22c26.md)。下文为历史移植记录；旧 pending/fnmatch Python 模拟已由 S 重写替换。

Files → Go (`server/internal/oxidns/`):
- `backend/common/oxidns_common_routing.py` (1192) → `routing.go` (+ C24's early `common_routing.go`: `Selectors`, `RoutingFields`, `replace_download_entries`; `_references` is now an exact rune scanner instead of a regex per call, plus `_words`/`_Mentions` = `mentions`). Exported: `ReadRouting`, `ApplyRouting(…, ApplyRoutingOptions{BeforeConfig, Notices})`, `MigrateDomainOnlyRouting` (returns the argument objects themselves when nothing is migrated, as Python), `RemoveRule`, `PlaceRoutingGroups`, `ReloadReferences`, `ResourceDownloads`, `ResourceReaders`, `ResourceIsRead`, `OwnedChains`, `RefreshRuleHelpers`, `PrefixEnd` (device policy C26). Internal: `_analyze` = `analyzeRouting` (state struct `routingState`; `full` false = Python's early-return dicts without groups/records), `_recognize`, `_catalog` = `buildCatalog`, `_observed_legacy_groups`, `_aliased_custom_state`, `_apply_aliased_custom`, `_validate_selections`, `_take_over`, `_settle`, `_own_helpers`, `_sync_updates`, `_uses_rule`, `_normalize_custom_matches`.
- `backend/common/dns_watchdog.py` (94) → `watchdog.go`: `EnsureLocalDNSWatchdog` (errors `*convert.PyError{Class: "ValueError"}`), `WatchdogName`, `WatchdogRule()`. Rule is the one the Go Agent checks (`backend/agents/go-agent/routes/dns_watchdog.go`: exec `reject 3`, single match `qname full:configflow-watchdog.invalid`). For agents (deployment_bundle, rule_sync).
- Early parts of other modules: `ruledns_routing.go` (C27 `rule_dns.py`: `Invalid` = `invalidRoute`, `GROUP_TAG_RE` (Unicode `\d`), `chain_features`, `_pinned`, `_source`, `parse_exec`, `pinned_action`, `default_route`, `probe_needed`, `policy_dns_routes` (uses `rules.RenderMembers`), `follow_route`, `NON_PROXY_POLICIES`); `nativeapply.go` (C29: `_MANAGED`, `_WRAPPERS`, `_rename`); `fnmatch.go` (CPython 3.11 `fnmatch.fnmatchcase`: `translate` ported literally incl. range chunks/empty-range removal, classes read like sre_parse).
- `pending.go`: C24's `readRouting`, `ownedChains`, `applyRouting`, `resourceDownloads`, `refreshRuleHelpers` variables now default to the real functions (kept as variables for the C24 replay); C22's `migrateDomainOnlyRouting` stub removed. `migrate_domain_only_routing` calls C26 `finishRuleUpdates` (still pending in production until C26).

Verification:
- **Record & replay** `testdata/c25_record.py` (pytest plugin, reuses c24_record's collaborator wrapper) → `c25_recorded.jsonl.gz`, `c25_test.go` `TestC25Recorded`: every top-level call of read_routing, apply_routing (kwargs before_config/notices), migrate_domain_only_routing (+ returned-object identity), remove_rule, resource_downloads, resource_readers, resource_is_read, owned_chains, refresh_rule_helpers, `_prefix_end`, `_references`, ensure_local_dns_watchdog during the full suite (12552 passed). Rows also carry: YAML alias paths of the arguments (restored in Go), the arguments after the call when changed in place (checked), the C26 collaborators' answers (strict queue: prepare_editor 6, prepare_rule_update 6, finish_rule_updates 12) and classical_ip_lines answers by argument hash (477, via `SetClassicalIPLines`). **15376 distinct calls from 1033 tests in 48 files: 15376 identical, 0 differ, 0 pending, 0 unsupported** (_references 11817, read_routing 984, resource_downloads 701, migrate 613, owned_chains 563, apply_routing 246, resource_readers 122, resource_is_read 117, watchdog 107, refresh 63, _prefix_end 34, remove_rule 9).
- **Random corpus** `testdata/gen_c25.py` → `c25_corpus.jsonl.gz`, `TestC25Corpus`: **18333 rows, 0 differ** — 1500 routing graphs (random rule spaces: every DNS rule type + unconvertible, domain/ipcidr/classical sets with content/URL, disabled/duplicate/non-string ids, Unicode, policies + proxy groups incl. follow chains; templates with selections, rule actions, custom matches, dynamic domains, rule updates via prepare_editor; then mutations: legacy IP groups with/without probe, hand-edited pinned chains/blackhole, renamed/shared/extended providers, reordered/broken sequence_main, YAML aliases, includes, managed renames with renamed_plugins, parked/follow state, odd download entries + glob readers) × read / 2841 apply (random changes, before configs, notices) / migrate / remove_rule / downloads / owned_chains / refresh; 600 default_forward-only cases on locked IP routing; 2000 watchdog documents (all server kinds, `${ENV}` entries, ambiguous tags, wrapper name collisions, aliases); 4000 glob patterns (resource_is_read); 3000 `_references` tags (Unicode digits/marks, `$`); 1929 `_prefix_end`. Errors exercised: OxidnsSettingsError 1706, ValueError 1640, AttributeError 126, TypeError 10, KeyError 3. Go statement coverage from both: ≥ 88 % for every C25 function except Python-crash glue and `mentions.positions`' non-word branch (29 %); exported wrappers are thin `catch` shells.
- C22/C23/C24 re-run: `TestRecordedPythonTests` 0 differ, pending 1054 → **607** (C25 0; C26: prepare_native_rule_updates 583, patch_document 22, finish_rule_updates 2 — rows that stopped at C25 now run until C26; routers rows identical since d60bd076 registers the real hook in tests); `TestCorpus` 0 differ, pending 65 → **33** (C26 only); `TestC23Recorded` 0 differ (422 pending C26); `TestC23Corpus`, `TestC24Recorded` (822), `TestC24Corpus` (3746) 0 differ.

Python test files (rule 4 coverage):
- `test_dns_watchdog_config.py` (8): replay (ensure_local_dns_watchdog rows of this file, identical) + corpus.
- `test_dns_watchdog_core.py` (1): real OxiDNS/MosDNS core test (skipped without a core binary); its input is ensure_local_dns_watchdog, replayed — not applicable otherwise.
- `test_oxidns_common_routing.py` (54): replay (all its C25 calls, identical).
- Also replayed (C25 calls only): the other 45 files the recorder lists (OxiDNS common/lifecycle/editor/native apply/restore/cleanup/device policy/rule DNS/rule sync projection/deployment bundle/gateway onboarding/rules routes…). Nothing hand-ported (no concurrency/timing/process behaviour).

Routes: none (`remove_rule` is used by `routes/rules.py`, C32). Rule 4b not applicable (no context, no I/O).

Decisions / differences from Python:
- `capabilities` key order: Python builds it from a frozenset (hash-seed dependent); Go uses a fixed order (SELECTORS, RULE_DNS_FIELDS, aggregate_rules, default_forward, custom_matches, custom_match_position). Only reaches jsonify (sorted). Replay compares it order-insensitively.
- `fnmatch` "bad character range" (`re.error`) is raised with class `error` but a shorter message (no case in suite/corpus; translate removes empty ranges).
- `place_routing_groups`' `old` groups: Go takes tag/provider type/args (`PlaceRoutingGroups` reads them from objects); recognized groups with `Invalid` routes never reach it (they lock rebuilding first).
- For the agents porter: `rule_sync_projection.py` uses `_analyze` and `_references`; `references`/`analyzeRouting` are unexported — export them when porting it (state struct is `routingState`).

Pitfalls: corpus generation must run with `--back` in its own `rt` call (check row count locally). The C25 replay installs `SetClassicalIPLines` per row and restores the previous hook (the routers' test hook).

Commits: bf844578 (code), 041cdcba (record & replay), 02d766d2 (corpus), + this PROGRESS commit.

Left: nothing for C25 itself. C26 replaces `prepareEditor`/`prepareRuleUpdate`/`finishRuleUpdates`; then re-run C22–C25 replays (C25 rows that consume recorded C26 answers stay valid). Reviewer: `_recognize` branch order (early returns vs. full state), `_settle` follow/parked bookkeeping, `_own_helpers` keep-closure and insertion index, `_sync_updates` known paths + readers, `pinnedChain` shape checks (bool vs int via `sameValue`), `policyDNSRoutes` recursion/memo.

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/oxidns; go vet ./internal/oxidns/ && go test -count=1 ./internal/oxidns/'` (last: ok 41.6 s)
- C25 replay: `rt --back server/internal/oxidns/testdata/c25_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c25rec; C25_RECORD_DIR=/tmp/c25rec PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 --dist=worksteal -p c25_record -p no:cacheprovider; PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/c25_record.py merge /tmp/c25rec'` (~3.5 min; 15376 rows)
- C25 corpus: `rt --back server/internal/oxidns/testdata/c25_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -W ignore server/internal/oxidns/testdata/gen_c25.py'` (~2 min, 18333 rows; Python's set order can vary the row set slightly between runs)

## C26 — OxiDNS common updates, device policy, editor

**Status: `verified`** (由 S-oxidns 复核覆盖；覆盖核对及 .32 验证通过，2026-11-17) · prereqs: C25 · Go: `internal/oxidns`

**Coverage audit, 2026-11-17 (verified; final checks passed):** 由 S-oxidns 复核覆盖：`672fc0d0`、`0164424d`、`da0aa993`，协调复验/合并 `dc3453c2`。范围为 updates/device_policy/editor 的 withdrawal/rebase/pipeline、hook/侧监听/增改撤销、迁移/三方冲突合并全部分支；当前选择 3,072 条录制、4,551 条随机参考，另有 6,304 份存储往返及公开编辑器/revision 回归。本轮补查 ApplyPolicies 返回草稿、同步模板元数据、失败不改输入；它不写数据库。C29/C31/agents 的 HTTP、持久化和并发入口未一并标为复核。逐文件依据见 [review-c22c26.md](review-c22c26.md)。下文为历史移植记录，旧 jsonx/协作 stub/size budget 描述已由 S 重写替换。

Files → Go (`server/internal/oxidns/`):
- `backend/common/oxidns_common_updates.py` (465) → `updates.go`: `ReconcileNativeWithdrawals`, `RebaseNativeRuleSources`, `PrepareNativeRuleUpdates`, `FinishRuleUpdates` (fresh-context wrapper); internal `prepareRuleUpdateImpl`, `finishRuleUpdatesImpl`, `stopPipeline` (= `_stop_pipeline`, for C29 native apply), `removeEmptyReload`, `managedJobs`, `externalReferences`, `updateProviders` (= `_providers`), `restorePipeline`, `downloadPath` (= `_path`), `refreshSteps`/`downloadOnlySteps` (= `_REFRESH`/`_DOWNLOAD_ONLY`), `proxyIdentityOf`, `rulesetSources`, `generatedSource` (net/url), `refreshManagedRuleURLs`, `withDownloads`, `managedDownloads`.
- `backend/common/oxidns_device_policy.py` (386) → `devicepolicy.go` (C22 had `NormalizeClientRange`/`NormalizePolicies`): `PolicyTag`, `DirectTag`, `Revision`, `PatchDocument(document, previous, rows, config)`, `InspectEntries`, `NewEntryContext` (typed `EntryContext`, `.Object()` = Python dict with sorted sets), `ReadPolicies`, `ApplyPolicies`; internal `policyPlugin`, `checkMain`, `listenPort`, `serverEntries`, `reach`, `legacyNodeTest`, `patchPolicies` (= `_patch`).
- `backend/common/oxidns_editor.py` (338) → `editor.go` (C24 had `_same`, `_MISSING`): `PrepareEditor`, `PreviewTemplate`, `TemplateInputs`; internal `prepareEditorImpl`, `renderTemplate` (= `_template`), `restoreParked`, `parkInactive`, `ruleIDs`, `mergeValues` (= `_merge`), `mergePlugins`, `changesOf`, `union`, `keepReferencedContent`, `pointer`, `label`, `pluginMap`.
- Early part of C31: `backend/common/device_notes.py` read side → `devicenotes.go` (`NormalizeName`, `NormalizeNotes`, `StoredNames`, `MaxNotes`); read_policies needs `stored_names`. C31 adds the writers to this file.
- `pending.go`: no stubs left. `prepareEditor`, `prepareRuleUpdate`, `finishRuleUpdates` stay package variables (assigned in `init`, a direct initializer is an initialization cycle) because the C24/C25 replays answer them with recorded results.

Verification (all on the test server, clean detached worktree at df3c8f6d):
- **C22–C25 replays, pending → 0:** `TestRecordedPythonTests` 607 C26-pending → **0 pending, 0 differ** (29539 rows; native `generate` 1630, incl. prepare_native_rule_updates + device-policy patch_document); `TestCorpus` 33 → **0 pending, 0 differ** (105 accepted as before); `TestC23Recorded` 422 (validate_settings → prepare_editor) → **0 pending, 0 differ**; `TestC24*`/`TestC25*` unchanged 0 differ (they keep recorded C26 answers).
- **Record & replay** `testdata/c26_record.py` → `c26_recorded.jsonl.gz`, `c26_test.go` `TestC26Recorded`: top-level calls of every C26 entry point during the full suite (12552 passed), run end to end in Go (no collaborators). Context functions (prepare_rule_update, finish_rule_updates, _stop_pipeline) are rebuilt from the recorded `_Context` state (by_tag, parked live/copy, included tags) and the state after the call is compared. **4048 calls from 1229 tests in 53 files: 4048 identical, 0 differ** (prepare_editor 1766, prepare_native_rule_updates 583, finish_rule_updates 482, prepare_rule_update 481, read_policies 207, reconcile_native_withdrawals 193, stored_names 157, apply_policies 86, revision 34, preview_template 26, inspect_entries 10, _merge 7, _stop_pipeline 6, patch_document 4, template_inputs 3, rebase_native_rule_sources 3).
- **Random corpus** `testdata/gen_c26.py` → `c26_corpus.jsonl.gz`, `TestC26Corpus`: public entry points with random device-policy rows (hosts/CIDRs/IPv6/mapped/invalid/duplicates, names, modes), native drafts with node-test/side/client-port/env/http_server listeners, broken sequence_main/forward_local, includes, duplicate tags, foreign hooks, hand-edited owned chains; template inputs + hand-edited documents through prepare_editor/preview_template with random resolutions; ruleset sources (absolute/relative, base URLs, profiles, retired tokens) through prepare_native_rule_updates, rebase_native_rule_sources, apply_common_settings. **4882 rows (1795 errors, ~40 distinct Chinese messages), 0 differ.**

Python test files (rule 4 coverage):
- `test_oxidns_common.py` (36), `test_oxidns_common_lifecycle.py` (21), `test_oxidns_fallback_lifecycle.py` (14), `test_oxidns_resource_lifecycle.py` (16), `test_oxidns_service_domain.py` (6), `test_oxidns_editor.py` (30), `test_oxidns_device_policy.py` (15), `test_oxidns_device_policy_side_entries.py` (15): replay — every C26 call of these files is in `c26_recorded.jsonl.gz` (identical); their C24/C25/C22 calls are in those modules' replays. Their route cases (`/api/oxidns/common-settings*`, `/api/profiles/<id>/oxidns/editor|template-preview|settings|render`, `/api/client-devices`, agent DNS policy routes) → **deferred to C29/C31/agents** (route owners' Flask↔Go differential).
- `test_oxidns_device_policy_core.py` (2): real OxiDNS core (skipped without `OXIDNS_TEST_BINARY`); its inputs are apply_policies + generate_oxidns_config, both replayed — not applicable otherwise.
- Nothing hand-ported (no concurrency/timing/process behaviour).

Routes: none (routes calling C26 belong to C29 `routes/oxidns.py`, C31 `client_devices`, C32/C33 `rule_library` (rebase), agents/routers (`read_policies`, `revision`)). Rule 4b not applicable (no context, no I/O).

Decisions / differences from Python:
- **Representation (rules changed mid-module, 2026-11-16):** C26 is written on the package's existing jsonx/commonContext model, because its functions edit in place the same native documents and `_Context` that C24/C25 hold (prepare/finish_rule_updates are C24 collaborators). Typed structs / yaml.Node would need the whole package converted — left to the oxidns S pass. New C26 code uses `net/url`, `net/netip`, `strconv`, `strings` (no new urlx/pystr use).
- **Size:** Python 1189 lines (+ ~40 of device_notes) → Go ~2280 non-test lines (updates.go 905, devicepolicy.go +700, editor.go +623, devicenotes.go 83, pending.go −28) ≈ 1.9×, **over the 1.2× budget**: jsonx type assertions and Python-shaped error glue of the existing package; to be slimmed in the S pass.
- No longer chased (functional parity): `_generated_source` uses `net/url` — URLs Go cannot parse (or with bad `%` escapes in the query) count as manual overrides (conservative: not rebased); `_listen_port`/`_endpoint`/`node_test_dns` parsing are ASCII/`netip` (Python's Unicode digits and whitespace not chased); TypeError texts on malformed values. None occur in replay or corpus.
- `EntryContext` is a typed struct (Python returns a dict with sets); the replay compares `.Object()` with sets sorted.

Pitfalls: HEAD at df3c8f6d does not build `internal/mosdns` (C28's ae4c5bbe references `weburl` from an uncommitted file); verification used a private worktree with `internal/mosdns` from 8aa0595b. The shared tree's other uncommitted edits (urlx removal in `convert`/`oxidns/constraints*`) make `TestCorpus` (append_url_query error texts) and one `TestC23Corpus` row differ — not C26 code.

Commits: 5c223f6b (code), df3c8f6d (record & replay + corpus), + this PROGRESS commit.

Left: review. For C29: use `stopPipeline`, `downloadPath`, `refreshSteps`, `downloadOnlySteps`, `reconcileNativeWithdrawals`, `PrepareEditor`, `PreviewTemplate`; C31: extend `devicenotes.go`, use `ApplyPolicies`/`ReadPolicies`/`Revision`; rule_library (C33): `RebaseNativeRuleSources`. `repo/compat_oxidns.go` (`normalizeClientRange`, `normalizeName`, `normalizeNotes`) and `rules/compat.go` device-policy copies can switch to `oxidns.NormalizeClientRange`/`NormalizeName`/`NormalizeNotes` if no import cycle (oxidns imports repo, so **repo cannot import oxidns** — repo keeps its copy).

Commands:
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/oxidns; go vet ./internal/oxidns/ && go test -count=1 ./internal/oxidns/'` (last on clean tree: ok 77.8 s)
- C26 replay: `rt --back server/internal/oxidns/testdata/c26_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c26rec; C26_RECORD_DIR=/tmp/c26rec PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 --dist=worksteal -p c26_record -p no:cacheprovider; PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/c26_record.py merge /tmp/c26rec'` (~6 min, 4048 rows)
- C26 corpus: `rt --back server/internal/oxidns/testdata/c26_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -W ignore server/internal/oxidns/testdata/gen_c26.py'` (~1 min, 4882 rows)

## S-oxidns — slimming pass of `internal/oxidns` (C22–C26 + early C27/C29/C31 pieces)

**Status: verified — coordinator fuzz/race follow-up closed, zero open findings** (2026-11-17). Goal: idiomatic Go, every feature kept; no line budget (Python in scope ≈ 6.1k: converters/oxidns, oxidns_native, _validation, _reference_validation, _references, _plugin_constraints, _global_constraints, _match_targets, _common, _common_routing, _common_updates, _device_policy, _editor, dns_watchdog; plus oxidns_settings 289 and the early rule_dns / device_notes / native_apply pieces). Before: 25.9k non-test lines (17.3k code + 8.6k Unicode tables).

Race-budget follow-up **closed** (`go-fix-importer-bounds`, 2026-11-17): .32 JSON baseline 417.361s; slowest retained suites: generation recordings 80.62s, editor recordings 53.75s, normalization recordings 45.42s, storage 44.99s, common recordings 41.16s. Race-only input-local reference suites now run in parallel after all sequential environment/hook tests clean up. Only four random corpora change from first 8/function/outcome + 1/16 sample to first 4 + 1/128, selected before payload copies: generation 2782→461 / full 41217, common 272→93 / 3746, routing 1028→193 / 13404, editor 387→75 / 4551 (4469→822, −81.6%; normal still runs all 62918 in original order). Dedicated validation, recordings, fresh Python references, storage and all regression/fuzz seeds stay complete. Exact-tree comparison skips redundant YAML parsing; canonical map copies use an index and storage key-order uses strings.Builder, protected by 16 comparator regressions. JSON audit confirms all 3047 original test/subtest names, 59 protected function totals and 6304 storage documents retained. .63 final normal passes 38.293s; .32 natural-load race 115.403s. Coordinator revised the shared-server criterion to two default-p/default-timeout whole-tree race passes, with actual load recorded. Both final .32 rounds pass all 68 tested packages: oxidns **274.075s / 283.762s**, active-window 1m load **21.45–40.57 / 19.73–46.70**; importer 33.977s / 40.717s. Format/vet silent; proddiff 2831 cases, self-check PASS, new=0/stale=0/coverage_regressions=0 (11 existing differences). No production code, timeout flags, baseline or normalization changes; no synthetic load in final acceptance. Source commits: 12ac1095, 39d90065, fdb2a652; exact output/load samples and trial history: [review-c35.md](review-c35.md#后续资源探测与-oxidns-race-预算修复已完成).

Plan (design, committed before the rewrite):
- **Build beside, then swap.** The new code is written in `internal/oxidnsnew` (package `oxidns`), tested at the boundary against the existing Python-recorded rows; when complete, the old package is deleted and the new one moved to `internal/oxidns` in one commit (consumer: only `internal/ippresence` → `SetClassicalIPLines`). Every commit builds.
- **Dynamic values = `*yaml.Node`** (gopkg.in/yaml.v3) for native documents (free-form user YAML, rule 2) *and* for the settings object / profile config the package edits: they carry fragments of native documents (parked plugins, args) and, in native mode, unvalidated template fields that must round-trip unchanged, and the Python logic addresses them by dotted paths. At load every alias becomes a shared node pointer (pointer identity = Python object identity, so in-place edits reach every holder like Python), comments/styles and mapping-key presentation are retained (review fix); aliases with annotations retain those annotations, and every scalar gets its resolved tag (`!!str/!!int/!!float/!!bool/!!null`, NativeLoader rules: YAML 1.2 bools, 0x/0o/0b/underscore ints). `node.go`: get/set/del, typed reads, `M(...)`/`L(...)` builders, memoized deep copy, `equal` (Python `==`) and `same` (type-strict `_same`), order-preserving JSON decode/encode, YAML dump.
- **Typed where the shape is fixed:** `Issue{Path, Message, Code, Severity, Source}` (+ `Path` of key/index elements), `Schema` (decoded from `backend/schemas/oxidns/*.json` with encoding/json), `Capability`, `File`, `RulesetDownload`, `Sources`, `MatchTargets`, `Validation`, errors `*SettingsError{Msg, Issues}` (= OxidnsSettingsError), `*ValidationError{Issues}`, `*ReferenceError{Msg, References}`; Python crashes on malformed input (TypeError/AttributeError/KeyError) become a plain error (`errMalformed`), not emulated exceptions.
- **Boundary adapter** `adapter.go`: the only file that touches `jsonx` — profile config from `repo` (`*jsonx.Object`) ↔ node, and the calls into `mosdns`/`rules` helpers that still take jsonx (SplitRulesAndRulesets, BuildRuleProxyURL, ParseDNSUpstreams, ParseCustomMatches, RenderMembers).
- **Native libraries:** `regexp` (+ small scanners for Python look-behind patterns), `net/netip`, `net/url`, `strconv`, `golang.org/x/net/idna` (replaces the IDNA2003 port and the 8.6k-line Unicode tables), fnmatch as a 20-line translate to `regexp` (`path.Match` does not fit: fnmatch `*` crosses `/`), `time.LoadLocation` for IANA zones; dnspython zone subset rewritten compactly. Deleted: `py.go`, `pyre_c23.go`, `idna2003.go`, `unicode_c23_tables.go`, `fnmatch.go`, `pending.go`, function-level replay tests and their generators.
- **File layout** (one per Python module): `node.go`, `errors.go`, `adapter.go`, `settings.go`, `native.go`, `references.go`, `validation.go` (schema engine), `constraints_plugin.go`, `constraints_global.go`, `zone.go` (dnspython subset), `references_validation.go`, `matchtargets.go`, `converter.go`, `ruledns.go`, `common.go`, `routing.go`, `updates.go`, `devicepolicy.go`, `editor.go`, `watchdog.go`, `devicenotes.go`.
- **Public API** (Python name → Go): generate_oxidns_config → `Generate(p *Profile, baseURL)`; get_oxidns_ruleset_downloads → `RulesetDownloads`; get_oxidns_custom_files → `CustomFiles`; unavailable_rulesets → `UnavailableRulesets`; normalize_oxidns_settings → `NormalizeSettings`; safe_relative_path → `SafeRelativePath`; load_native_yaml/native_document/managed_files/resource_references/relative_resource/validate_resource_files → same names; reference_locations/reference_reason → `ReferenceLocations`/`ReferenceReason`; read_match_targets → `ReadMatchTargets`; validate_native_config/require_valid_document/validate_expanded_document/validate_settings/format_error → same names; read/apply_common_settings → `ReadCommonSettings`/`ApplyCommonSettings`; read_routing/apply_routing/migrate_domain_only_routing/remove_rule/resource_downloads/owned_chains/refresh_rule_helpers/place_routing_groups/reload_references/resource_readers/resource_is_read → same names; ensure_local_dns_watchdog → `EnsureLocalDNSWatchdog`; prepare_editor/preview_template/template_inputs; reconcile_native_withdrawals/rebase_native_rule_sources/prepare_native_rule_updates; normalize_client_range/normalize_policies/revision/patch_document/inspect_entries/read_policies/apply_policies; device notes `NormalizeName/NormalizeNotes/StoredNames`; rule_dns early pieces (`EntryKey`, `RouteKey`, `RouteExec`, `NormalizeAction`, `HelperPlugins`, …); hook `SetClassicalIPLines`.
- **Acceptance (boundary):** `boundary_test.go` drives the public entry points with the recorded Python rows already in `testdata/` (recorded.jsonl.gz, corpus.jsonl.gz, c23–c26 recorded/corpus; public-function rows only) and compares outputs structurally (YAML texts parsed, JSON values with int/float/bool kept apart), errors by rejected-or-not plus the exact Chinese message for OxidnsSettingsError/ValidationError; validation-message helpers (zone records, AdGuard lines, cron, plugin constraints) keep their message corpora as data-driven tests. Storage round-trip: Python-written settings JSON → node → JSON equal.

Result (done, not reviewed; commits 1fa88e9f plan, fe4ac996 + 81b50540 new package beside the old one, d5c44acd swap, 0e32335e restores internal/rules files d5c44acd swept in by mistake — see Pitfalls):
- **Lines:** non-test 25,866 → 14,020 (old: 17.3k code + 8.6k Unicode tables); test 1,846 → 829 (+ the recorded rows in testdata). The maintainer dropped the line budget (be9a3fe8); counts are for information.
- **Deleted:** jsonx documents and `any` signatures; `py.go`, `pyre_c23.go`, PyError glue, `pending.go`; `idna2003.go` + `unicode_c23_tables.go` (→ `golang.org/x/net/idna`; go.mod gains `golang.org/x/text` indirect); `fnmatch.go` (→ `globMatch` in routing.go, a short translate to `regexp`; `path.Match` does not fit because fnmatch `*` crosses `/`); `dnstext.go` (dnspython port → `zone.go`); yamlx/PyYAML loading of template text fields (→ yaml.v3 `safeLoad`); every function-level replay test (`recorded_test.go`, `rows_test.go`, `c23–c26_test.go`, `unsupported_test.go`, `routers_hooks_test.go`) and `testdata/gen_unicode_c23.py`.
- **Review removed the last formatting emulation:** resource identities use native scalar text; `Revision` and `EntryKey` use `encoding/json` with deterministic map ordering and HTML escaping disabled. `pyStr`, `pyFloat`, `sortedJSON` are gone. `spaces` (duration/AdGuard checks) and attachment base64 validation remain feature helpers. Confirmation and switch implications are recorded in the review below.
- **Remaining jsonx:** only `adapter.go` (profile config from repo; `mosdns` SplitRulesAndRulesets/BuildRuleProxyURL/ParseDNSUpstreams/ParseCustomMatches; `rules.RenderMembers`; the routers' classical_ip_lines hook). `_regex_error` still calls `rules.CompilePyRegex` (shared helper; S-rules replaces its implementation).
- **Boundary tests** `boundary_test.go` (public entry points + the message-level validators zone/AdGuard/cron/domain/regex/format, rows of testdata recorded/corpus files): **106,128 rows identical, 0 differ, 484 accepted.** Accepted classes, corpus rows only (every row of the real Python test suite is identical): Python crashed on malformed input (TypeError/AttributeError/KeyError/IndexError/OverflowError/struct.error) and Go answers; YAML values a document cannot hold (sets, non-string keys) refused; CPython-only Unicode rules (isdigit/isspace on superscripts, Arabic-Indic digits, U+001C–U+001F; IDNA 2003 vs UTS 46 for non-ASCII zone names); hosts net/url refuses that urlsplit accepted (`$${X}`). In-place edits (`args_after`) and returned-object identity (migrate) are checked too. Env vars `OXIDNS_SHOW`, `OXIDNS_ARGS`, `OXIDNS_FN`, `OXIDNS_ACCEPTED` show details. `hooks_external_test.go` registers the real ippresence hook on a temporary repository.
- **Storage:** `storage_test.go` — 6,304 Python-written settings/profile documents round-trip FromJSON→ToJSON without loss (key order, int/float/bool kinds); native normalization keeps every stored field.
- **Behaviour changes (functional parity):** editor output uses yaml.v3 while retaining comments, key order, scalar styles and effective merge values; aliases may expand into the same content. Generated configs materialize merge keys so the core receives explicit values. Native YAML now also accepts valid merges (Python NativeLoader rejected them). Template YAML follows yaml.v3 scalar resolution as already agreed. See the review for narrow accepted corpus classes.
- **Python test files (rule 4 coverage):** unchanged from C22–C26: the same recorded rows of every listed Python test file now run through the public entry points (recorded.jsonl.gz: C22 files; c23_recorded: validation files; c24_recorded: common/lifecycle; c25_recorded: common routing, watchdog; c26_recorded: common/editor/device policy). Route cases stay deferred to C29/C31 as listed there.

Pitfalls: the shared index often holds other agents' staged changes — always `git commit -- <paths>`; a bare `git commit` swept internal/rules deletions into d5c44acd (restored by 0e32335e, built on a temporary index so nobody's working tree or index changed). Never `git reset` in the shared tree: HEAD moves under you.

Commands:
- tests: `rt -- bash -lc 'cd server && go vet ./internal/oxidns/ && go test -count=1 ./internal/oxidns/'` (≈60–75 s; last: ok, full `go vet ./... && go test ./...` at 0e32335e in a clean worktree: all ok)
- details: `OXIDNS_ARGS=1 OXIDNS_SHOW=5 OXIDNS_FN=apply_routing go test -run TestBoundaryRoutingCorpus ./internal/oxidns/`
- regenerating rows: the C22–C26 recorder/generator commands above (scripts unchanged in testdata/).

### Review S-oxidns (adversarial, 2026-11-16; follow-up verified 2026-11-17)

Scope: resumed the paused review (6d4babb9) in `go-review-soxidns`; C22–C26 and the existing early C27/C29/C31 pieces only. No production access, no pushes. All Python reference generation, tests, vet and fuzzing run through `rt` on 192.168.0.32. The independent domain/production-snapshot differential and HR/lab acceptance remain coordinator work under WORKFLOW, not a claim of this package review.

Findings and fixes (no unresolved functional finding after package verification):
- **High — YAML edits lost user content/presentation.** `builder.node/fill` and `cloneMemo` dropped comments/key annotations/styles; three-way merge rebuilt mappings and lost key metadata. Preserve presentation throughout common settings, template preview and device-policy writes. Quoted explicit integers/booleans normalize to their declared kinds, rather than becoming strings on dump. Annotated aliases are completed after their source nodes are filled. Public-entry regression tests cover all three editors, unknown fields, order, literal strings, comments on merge aliases, explicit quoted scalars and read→edit→load values.
- **High — merge handling.** Template `safeLoad` retained raw `<<` without interpreting inherited fields; NativeLoader rejected merges. Node access/iteration/equality/counts/references now use effective merge values with first-source and explicit-key precedence, and preserve `<<:` in edited drafts. Deleting an inherited key materializes the affected mapping so it cannot reappear. Generated configs expand merges before delivery, independent of core YAML merge support. Malformed/cyclic merges are rejected; traversal visits shared sources once before the normal size/depth validation. Multi-document template YAML is rejected instead of silently truncating after the first document.
- **Medium — DoH URLs with Unicode credentials were refused.** Shared `weburl.Parse` now encodes userinfo through `net/url.User/UserPassword` before parsing, decoding existing escapes first. Tests prove Chinese credentials and percent-escaped passwords round-trip without double encoding. Installer corpus comparison now recognizes an English IP-parser error containing a CJK host as an internal error (both sides still refuse); Chinese user messages remain exact.
- **Medium — Unicode acceptance hid validation differences.** Removed the blanket “any non-ASCII in a zone row” waiver; normal CJK/IDN/RTL names cannot trigger it. Enabled UTS46 BidiRule, rejecting mixed-direction labels Python rejects. New real-Python references contain 320 zone cases (CJK, Japanese, Cyrillic, Greek, Arabic, Hebrew, emoji, Unicode dots; A/AAAA/CNAME/MX/TXT/SRV/SOA/CAA, origin/relative names, invalid addresses). All match. The six old refused upstream rows are exclusively `$${X}`; that URL waiver is now restricted to that exact value. The Hosts rows use U+001C/other special line separators, and AdGuard differences use U+001C, not normal user names. The remaining invalid-URL bootstrap diagnostic difference is compared by removing only that consequent issue and comparing the complete result.
- **High — validation depended on loose backend schema files and could panic on a request.** Embed the three pinned catalogs, validate them at startup, and test equality against Python’s files. Validation works with an empty CONFIGFLOW_ROOT. Unknown internal format names return a rejection message, not a panic. Nil expanded-document source metadata defaults safely to config.yaml.
- **Style/error safety —** replace zone panic/recover unwinding with explicit parser failure state; propagate YAML encoder errors through editors/generation before changing stored text. Only startup catalog/default programming errors retain panic. New package docs and Chinese merge/format messages follow the root Go style rules.

Formatting-helper confirmation/removal:
- Agent `managedPaths` → `backupDeployment` includes old manifest paths; `replaceDeployment` removes Backups absent from `fresh` except persistent Mihomo Geo assets (`backend/agents/go-agent/routes/deployment.go`). Renamed OxiDNS rule/AdGuard resources are therefore removed on the next successful publish, with backups available to rollback.
- `revision()` consumers were traced in policy_workspace, client_devices, routes/agents, routes/routers and ros_policy. It is a freshly computed editor/publication conflict token; ros_policy’s `source_revision` is only an inspect-result field (no persistence consumer). No current Go consumer recomputes it; only oxidns exports it. Native canonical JSON is deterministic, preserves big integers and distinguishes int/float/bool/string/null; tests check reordered mappings keep a token, changed values invalidate it, clones keep it, and string-upstream EntryKey bytes stay stable. A pre-switch browser token can conflict once and reload; one full republish remains the agreed switch action.
- Existing Chinese null-type diagnostics are complete message constants (including the literal word None); native identity/hash formatting does not change them. Unsupported *internal* validator-format names return a rejection message rather than raising Python RuntimeError. These and newly supported native merges account for additional accepted corpus rows; ordinary supported inputs keep their results.

Coverage / feature walk:
- Rechecked Python `converters/oxidns`, `oxidns_native`, `oxidns_references`, `oxidns_match_targets` against converter/native/reference/target APIs, including include sources, attachments, resources and active-rule selection.
- `oxidns_validation`, `oxidns_plugin_constraints`, `oxidns_global_constraints`, `oxidns_reference_validation` → schema/type/format/range, zone/AdGuard/cron, TLS/outbound/bootstrap, quick expressions/dependency graph and Chinese issues.
- `oxidns_common` → context ownership/park/restore, API/cache/TLS/fallback/dynamic/Hosts/AdGuard/manual update; `oxidns_common_routing` → recognition, legacy IP migration, provider/helper ownership, selectors/actions/follow/parked reconciliation, readers/download updates; `dns_watchdog` → local reply/wrapper and idempotence.
- `oxidns_common_updates`, `oxidns_device_policy`, `oxidns_editor` → resource withdrawal/rebase, editor conflict merge, conservative device hooks and side listeners. Existing settings and early rule_dns/device_notes pieces retain the original S scope; later C27/C29/C31 APIs remain their owners’ work.
- C22–C26 Python test-file→boundary-file mapping above remains intact. `rg 'oxidns\.' server/internal` confirms ippresence is the only implementation consumer of this package; its real-hook test and all consumers pass in the full suite. Storage test still round-trips 6,304 Python-written documents.
- New `testdata/review_s_gen.py` → `review_s.jsonl.gz`: 354 fresh real-Python rows, including 13 complete realistic profiles (synthetic upstreams only): caches/dump/recorder, fallback, IDN Hosts, custom YAML merges, routing aggregation, AdGuard, dynamic learning/IP selection, pinned upstream and scheduled ruleset downloads. 320 zone rows + 13 generated configs + 13 custom-file outputs + 8 URLs. One deliberately refused `$${X}` placeholder; all other results structurally match.
- All converter/validation/common/routing/editor recorded and corpus boundary tests pass with **zero unclassified differences**. The acceptance inventory was inspected by class, including all original 484 rows; additional exceptions are only supported native merges and non-panicking internal format diagnostics described above, never a CJK-name or Chinese-message waiver. Fuzz targets seed zone validation from the Python corpus and exercise native YAML read/write type preservation.

Verification so far:
- `rt -- bash -lc 'cd server && go vet ./... && go test -count=1 ./...'` (wrapped only to retain a /tmp log): all packages passed, final lines `ok configflow/internal/weburl 0.005s`, `ok configflow/internal/yamlx 0.686s`.
- `rt -- bash -lc 'cd server && go test -count=1 -v ./internal/oxidns'`: `PASS`, `ok configflow/internal/oxidns 55.014s`; after error-report diagnostics, 67.728s. Public editor and final fresh-reference checks also pass.
- `rt -- bash -lc 'cd server && go test -run "^$" -fuzz=FuzzNativeYAMLRoundTrip -fuzztime=10s -parallel=2 ./internal/oxidns && go test -run "^$" -fuzz=FuzzZoneValidation -fuzztime=10s -parallel=2 ./internal/oxidns'`: both PASS (40,916 and 105,945 executions). Coordinator can extend each to the required 60s gate.
- Reference regeneration: `rt --back server/internal/oxidns/testdata/review_s.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/review_s_gen.py'`.

Original hand-back before the coordinator return (historical; latest verification follows):
- Branch `go-review-soxidns`, rebased onto local `go` at **229f2abe**. Final code commits: **41cbd981** (review fixes, references/fuzz targets/catalogs/docs), **03ee34e9** (preserve complete Chinese null-type messages; remove their waiver). Earlier intermediate ids were rewritten by rebase. The final upstream increments changed only HANDOFF/WORKFLOW scheduling instructions; the tested Go code, dependencies and tests are identical (confirmed with git diff).
- Final required command: `rt -- bash -lc 'cd server && go vet ./... && go test -count=1 ./...'` → exit 0; relevant/final output: `ok configflow/internal/installer 160.185s`, `ok configflow/internal/ippresence 22.203s`, `ok configflow/internal/oxidns 72.956s`, `ok configflow/internal/weburl 0.018s`, `ok configflow/internal/yamlx 1.360s`.
- Final formatting command: `rt -- bash -lc 'cd server && gofmt -l internal/oxidns internal/weburl internal/installer/docker_ha_corpus_test.go'` → exit 0, no output.
- Fresh reference replay: 320 zone + 13 generated configurations + 13 custom-file results + 7 URL results identical; only the intentionally refused `$${X}` URL is accepted as the documented difference. 6,304 stored-document round trips remain green.
- Lines after review: production Go **14,271**, Go tests **1,273**, plus **3,616** lines of embedded unchanged catalog JSON. No production access or pushes. No open finding or unfinished implementation in this package review; independent domain/production-snapshot, coordinator race/60s fuzz and HR/lab gates remain as assigned in WORKFLOW.
- This final verification-note commit changes only this progress file. All work is committed and the worktree is clean at hand-back.


#### Coordinator return — block-scalar and race follow-up (verified 2026-11-17)

The previous verification was rejected: a 12s coordinator fuzz run found `A: |\n \n 行`, and the full package under -race hit its default 600s limit under load. The follow-up below resolves both returned findings; all requested checks passed.
- Reproduced after the test-server reboot (31 GiB): value `"\n行"` became `"行"`; yaml.v3 emits the value's first break as its block-header break. Fix normalizes only leading-newline and CR/NEL/Unicode-separator string styles to double quotes; ordinary literal/folded styles remain unchanged. This is applied to native/template loading, constructed strings and replacements without adding an encode-time traversal. Regression seed is `testdata/fuzz/FuzzNativeYAMLRoundTrip/leading_blank_unicode`; table tests cover literal/folded, indentation, blank/space-only lines, ASCII/CJK/emoji, CR/NEL/LS/PS and all string construction paths. The original regression and the complete normal package suite passed (125.905s), including all twelve new table cases.
- Unfiltered race baseline after reboot timed out at 600.528s in editor replay. Generation replay took 85.07s, randomized generation 194.97s, common replay 41.49s and randomized common settings 129.10s. CPU profile dominated by race/TSAN instrumentation, stack runnable in JSON decoding (not a blocked lock). Logs/profile: `/tmp/oxidns-race-before.log` / `.cpu` on the test server. No timeout increase.
- Race-only sampling added for the four expensive randomized corpora: first eight rows per function/expected outcome, plus a deterministic 1/16 hash sample from the entire file. Recorded Python test-suite rows, full validator corpus, storage, new regression/fuzz seeds and all normal non-race corpus runs remain complete. Default-timeout full passes and standalone measurements are recorded below.
- Full normal oxidns corpus passed (125.905s). gofmt/vet passed. The first requested whole-server race run completed oxidns in 515.502s despite peak load 43 on 12 CPUs; the only failure was an unrelated installer HTTP test fixture: handler reads status under a mutex while tests wrote it unguarded. Guarded status/location updates and call snapshots with the same existing mutex, changing test code only. `go test -race -count=1 -run "TestBundleBootstrap" ./internal/installer` passed (1.973s).
- Both requested long fuzz runs passed with `-fuzztime=120s -parallel=2`: FuzzNativeYAMLRoundTrip 121.023s / 296,105 executions; FuzzZoneValidation 121.032s / 849,185 executions. No additional failing input; the requested original seed remains committed.
- Per coordinator steering, only one race run is active for this worktree at a time. Standalone `go test -race -count=1 ./internal/oxidns/` passed in **449.527s** without a timeout override. Start uptime: `15:33:14 up 32 min, 3 users, load average: 4.22, 8.76, 12.44`; end: `15:40:45 up 39 min, 6 users, load average: 27.73, 20.23, 16.37` (12 CPUs; other work increased the load during the run). The coordinator is adding another server; this work stays on the authorized test server.
- Second whole-server race run passed oxidns in **440.717s**, but exposed a separate repo lock-test fixture race (`Close` vs unlocking goroutine `Fd`). A synchronized fixture passed 20 race repetitions (6.159s); integration branch already has coordinator fix **8b63fb6d**, retained on rebase. Integration also contains **87e47955**, the overlapping installer status-fixture fix.
- Rebased onto **86700e16**: kept the complete N2 `weburl` implementation (it already handles Unicode credentials), used the upstream repo lock fix, and consolidated installer status/location/call snapshots under its existing mutex. Integrated gofmt had no output, vet passed, and the complete ordinary suite passed (`oxidns 88.928s`, `installer 152.257s`, `repo 7.367s`, `weburl 0.007s`, `yamlx 1.177s`).
- First integrated acceptance command on native .32 passed with exit 0: `gofmt -l . && go vet ./... && go test -count=1 ./... && go test -race -count=1 ./...`, then both fuzz targets with `-run "^$" -fuzztime=120s -parallel=2`. Formatting had no output. Full race: `oxidns 535.249s`, `installer 433.180s`, `repo 49.311s`, `weburl 1.035s`, `yamlx 9.286s`; no race warnings. Load briefly reached 53 while other agents ran. Repeated native fuzz: **121.021s / 768,632 executions**, zone fuzz: **121.028s / 602,980 executions**, both PASS and no new failure seed. Final command uptime: `16:06:11 up 1:05, 4 users, load average: 4.29, 9.55, 14.52`.
- Lower-load standalone retest passed in **328.799s** (default timeout): start `16:06:32 up 1:05, 4 users, load average: 3.64, 9.06, 14.26`; end `16:12:02 up 1:10, 3 users, load average: 4.40, 7.29, 11.94`. Intermediate observed one-minute loads were 3.45, 5.50, 13.65 and 5.72. This is the standalone package timing requested by the coordinator.
- Integration advanced during that measurement: rebased again to **0639ea66**, including S-rules/R04; the only additional oxidns change is the shared `clientrange.Normalize` helper. Installer fixture snapshots use the equivalent upstream `recordedCalls` implementation. Post-integration checks passed below; the leading-scalar fix and both fuzz paths are unchanged.
- Final post-integration acceptance on native .32: `rt -- bash -lc 'cd server && gofmt -l . && go vet ./... && go test -count=1 ./... && go test -race -count=1 ./...'` (wrapped with uptime/log capture) → **exit 0**. Formatting had no output; vet passed. Ordinary full: `oxidns 86.083s`, `installer 156.610s`, `routers 19.775s`, `rules 3.095s`. Full race: `oxidns 379.956s`, `installer 259.891s`, `repo 32.469s`, `routers 109.991s`, `rules 13.889s`; final lines `ok configflow/internal/weburl 1.043s`, `ok configflow/internal/yamlx 6.351s`, `? configflow/tools/proddiff/generator [no test files]`. Start uptime `16:13:03 up 1:11, 3 users, load average: 4.56, 6.63, 11.40`; end `16:22:37 up 1:21, 4 users, load average: 16.64, 11.20, 11.55`.
- Long fuzz reproduction command: `rt -- bash -lc 'cd server && go test -run "^$" -fuzz=FuzzNativeYAMLRoundTrip -fuzztime=120s -parallel=2 ./internal/oxidns && go test -run "^$" -fuzz=FuzzZoneValidation -fuzztime=120s -parallel=2 ./internal/oxidns'`. Both targets passed twice at >=120s; latest final lines were `PASS`, `ok configflow/internal/oxidns 121.021s` and `PASS`, `ok configflow/internal/oxidns 121.028s`. No fuzz assertion was weakened.
- Final integration base **2d542a12**: its only change after the tested **0639ea66** base is HANDOFF documentation; exact git diff confirms identical runtime/test/dependency trees. Branch **go-review-soxidns**. Current code commits: **2504f807** (original review fixes), **b295896e** (Chinese diagnostic constants), **12e30433** (block-scalar regression/style fix and bounded race corpus); intermediate ids above are historical. Installer fixture fixes are now entirely supplied by upstream S-rules, including locked snapshots; the overlap was resolved in favor of upstream.
- No remaining implementation or open review findings. Reboot connection failures were retried; no local tests, production access, timeout increase or pushes. All changes committed; final `git rebase go` and clean worktree checks completed at hand-back.

## S-rules — slimming pass of `internal/rules` (C08 + C09)

**Status: verified** (coordinator follow-up, 2026-11-16). Native-semantics corrections, full race verification and post-rebase checks passed; rebased on `go` at `c53430b4` (N2 integration follow-up below). No open findings; accepted Go-native differences are listed below. Goal: idiomatic Go with every feature kept; the maintainer removed the line budget. Python in scope 1809 lines (rule_syntax 216, rule_matcher 690, rule_utils 195 pure part, domain_matcher_cache 237, group_candidates 303, reorder 57, strategy_references 27, connection_rules 84). Before: 15620 non-test lines (7354 without generated IDNA/case tables), 4096 test lines.

Plan (design, committed before the rewrite):
- **Regex = `regexp` (RE2) first, `regexp2` when needed.** `regex.go`: one linear walk over the Python pattern does three jobs: (1) validity is decided by the engines; Chinese messages keep the same prefix and use Go's engine detail; (2) the minimal syntax shim: groups become plain captures (names kept in a Go map), `(?P=n)`/`\1` → `\k<n>`, `{,n}` → `{0,n}`, `\Z` → `\z`, `\d \w \s` and negations → explicit Unicode classes (Python `str` semantics, also inside `[...]`; ASCII under `(?a)`), `\b`/`\B` and `$` pass through unchanged to the selected engine, `(?x)` whitespace/comments are removed, possessive `x*+` → `(?>x*)`, global flags → engine flags; (3) engine choice: RE2 unless the pattern needs look-around, back references, conditionals, atomic/possessive groups or repeat counts > 1000 → `regexp2` with a 1 s match timeout (timeout = no match / `ErrMatchTimeout`). IGNORECASE uses the engines' Unicode folding (Python's simple-lowercase quirks such as `İ`/`ı` follow Go). API kept for consumers: `Regex` (alias `PyRegex` retained for caller compatibility), `CompilePyRegex`, `Search`, `Match`, `Sub(text, func(*Match) (string, error), count)`, `Match.Group/Groups/NamedGroup`, `UnsupportedRegexError` (`\N{name}`), `ErrRepeatTooLarge` (counts beyond int32), `ErrNegativeSubCount`, `ErrMatchTimeout`, `IsPyRegexError`. Deleted: `pyre_parse.go`, `pyre_re2.go`, `pyre_net.go`, `pyre_icase.go`, `pyre_case_table.go`, `pyre_sub.go` (replacement templates were unused; rulefetch/website.go now uses a callback).
- **IDNA = `golang.org/x/net/idna`** (UTS #46 transitional lookup mapping ≈ IDNA2003 nameprep for real names: full-width, upper case, CJK, `ß`), Python's ASCII fast path and label-length rules kept. Deleted: `idna.go`, `idna_tables.go`.
- **IPs = `net/netip` + `netipx.ParseNetwork`** (bare host, leading-zero prefix, IPv4 netmask/hostmask like `ip_network(strict=False)`). Deleted: `ipaddr.go`.
- **Strings = `strings`/`unicode`** (`TrimSpace`, `ToUpper`, `ToLower`); `pystr` no longer imported. Deleted: `pystr.go`; `difflib.go` shrinks to a 40-line `get_close_matches(n=1)` (same ratio and tie order).
- **Data model:** the package keeps `*jsonx.Object` at its API where consumers hand it profile data (convert: RenderMembers, LowerGroupProviderFilters, FilterCandidates, ValidateRulePolicies; rulefetch/repo/discovery: RuleCacheFilename/RuleCacheReadPath; the matcher and cache take the same profile config that C37 will pass) — N8 replaces jsonx repository-wide later. Python crash emulation (TypeError/KeyError on malformed records, `jsonx.Repr` keys for unhashable ids) is dropped: non-string ids/names/filters are skipped or rejected with the module's Chinese message.
- **File layout:** `regex.go`, `syntax.go` (rule_syntax + close-match), `text.go` (IDNA, IP parsing, CR/LF/CRLF line splitting), `matcher.go`, `matchercache.go` (+ `stamp_*.go`), `groups.go`, `connection.go`, `ruleutils.go`, `strategy.go`, `reorder.go`, `compat.go` (rule-content helpers that cannot be imported without a cycle), plus cycle-free `rulesetfmt` and `clientrange` leaf packages shared by rules, converters, OxiDNS and repo.
- **Acceptance (boundary):** new generator `testdata/boundary_gen.py` (CPython 3.11 on the test server, synthetic values) → `boundary.jsonl.gz`: realistic and edge rule lines of every dialect, rule_value_error for every rule type (Chinese messages exact), DOMAIN-REGEX/filter patterns users write (incl. `(?i)`, named groups, look-around, `\d` with full-width digits, CJK/emoji/full-width node names) with search results, filter_candidates/validate_filters, domains (IDN, full-width, upper case), IPs/CIDRs, match_query, RuleConfigMatcher over text/YAML rule sets, Sub with callbacks. Reused as data: `corpus.jsonl.gz` (gen.py: match/line/direct/syntax/ip/idna/cache/filters/group/lower) and `review_c09.json.gz` (apply/reorder/policies/lower). Compared: outcomes and Chinese messages; Python-internal exception texts only through the messages that embed them. Deleted when covered: fuzz/replay of Python internals (`review_c08.jsonl.gz` str.upper tables, sre fuzz, libyaml token soup; `icase_reference.json.gz`; `sub_reference.jsonl.gz` template internals) with their generators.

Progress: (updated at each milestone)
- Plan committed (f999878f); S rewrite committed (39d01b51). The maintainer removed strict Python regex error text/positions and the line budget; engine validity and details are native Go.
- Rewrite result: rules non-test lines 15620 → 4268 (7354 → 4268 excluding generated tables), tests 4096 → 3659 before this review. Deleted the CPython regex/case/IDNA/IP/string emulation layers and their generated corpora. `regex.go` now uses RE2 or regexp2 with a small syntax shim; `text.go` uses x/net/idna and net/netip; the matcher uses yaml.v3 nodes. C08/C09 behavior remains on the existing typed profile API.
- Consumer updates: rulefetch uses callback-based `Sub`; convert/health/subs tests document Go Unicode folding; existing `PyRegex`/`CompilePyRegex` aliases remain names only.
- Kept review changes: `RulesetFormat` / `NormalizeClientRange` implementations moved into cycle-free `internal/rulesetfmt` and `internal/clientrange`, shared by rules, convert, OxiDNS and repo; public helper APIs remain thin wrappers. Format inference stops at the first matching library entry. Frontend error display was checked: it displays Go regex errors as plain text without parsing their details.
- Coordinator correction (2026-11-16): removed the Python word-boundary expansion (including its empty-string `\B` guard), Python `$` rewrite and Unicode `str.splitlines` separators. Boundaries and anchors now run directly on `regexp` / `regexp2`, consistent with the maintainer's Go-native/Mihomo policy. Rule files split only at LF, CRLF or CR. Negative `Sub` counts now return `ErrNegativeSubCount`; every current production caller passes 0, so this rejects an unused invalid parameter instead of copying CPython's negative-count behavior.
- Safety check: `TestReviewBacktrackingMatchTimesOut` runs a nested-quantifier/look-ahead pattern with a 10 ms regexp2 timeout and verifies it returns `ErrMatchTimeout` within one second; `Search` converts the same timeout to no match and logs once per pattern.
- (low) The shared converter format inference validated every `rule_library` entry before selecting a match, so a `null` after the first matching record raised an error where Python stops at the match. Validation now occurs in the shared ordered scan; the matcher keeps its prior tolerant behavior. Covered by `convert/ruleset_format_review_test.go` and the two `ippresence` edge-corpus rows.
- Corpus update: deleted CPython word-boundary search/Sub expectations from both generators and regenerated their gzip fixtures on the test server. `gen.py` now generates rule-file separators only from LF/CRLF/CR. No runtime skip was added for these changes: native boundary/anchor tests compare Search/Match against direct `regexp` / `regexp2` results (empty input, CJK, combining marks, connector punctuation, realistic English filters and trailing newlines), while line tests assert Unicode separator characters remain content. `go test -count=1 ./internal/rules` passes.
- Accepted differences added by the coordinator: (1) RE2 boundaries are ASCII word boundaries; regexp2 uses its own Unicode categories (including marks/connector punctuation); empty-input `\B` follows the selected engine. These may differ from Python and from each other, by design: use the native libraries Mihomo uses. (2) `$` is each engine's own anchor (RE2 does not match before a final LF outside multiline mode); node names and individual rule lines do not carry line endings, so no normal filter is affected. (3) VT/FF/U+001C–U+001E/NEL/U+2028/U+2029 are content, not file line endings; supporting Python's unusual text splitting is outside the Go-native policy. (4) negative replacement counts are API errors rather than CPython's no-op; no production caller passes them.
- Accepted differences decision: Go's regex engine error details are displayed as plain text by the frontend (no parsing); only the Chinese prefix is stable, and native engine details remain an intentional S-pass change. Go accepts a few syntaxes Python rejected (for example variable-width look-behind and flags after the start); the frontend accepts free-form patterns, so this expands what can be saved, but cannot alter pre-existing Python-valid stored filters and regexp2 can execute those patterns. Keep as the documented Go-native extension. `\N{name}` and repeat counts beyond int32 remain unsupported. The frontend accepts arbitrary regex text, and `\N{name}` is accepted as an unsupported/no-op by validation, so it can be stored; repeat counts beyond int32 are rejected by Go-side filter validation, though hand-edited or pre-existing records can reach runtime. Neither is a realistic airport-node filter. The preview for an unsupported `\N{name}` consequently shows all candidates; this is an explicit residual limitation because Python's Unicode-name table is not embedded. İ/ı/ſ and newer Unicode-table folds remain rare data-dependent edges; revisit if such names occur in production data. IPv6 scoped networks, IDNA-2003-only forms, multi-character upper-case `ß`, non-list legacy group fields and dotted display of IPv4-mapped IPv6 remain documented differences; write validation or unusual hand-edited data bounds their reach.
- Original review commits after the N2 rebase: `96fcf1b5` (fixes) and `4bb32623` (prior non-race verification); native-semantics correction: `6891d63f`. The coordinator requested the native-semantics corrections above after rejecting four Python-emulation changes. Follow-up full race verification passed, then `git rebase go` succeeded onto `af1d64f4`. Since the tested tree, upstream added the proddiff generator/tooling and a test-only repo lock race fix; no server runtime Go code changed. The new Go package/repo race checks and the ordinary full suite passed after rebase. The first race run lost its connection during the coordinator-confirmed test-server reboot (exit 255, server expanded to 31 GiB RAM); it is not counted as validation and the full command was restarted after reconnection.
- Full-race follow-up: `installer.TestBundleBootstrapExpiredFirstAttemptStaysStoppedAndRetries` exposed concurrent writes of the loopback fixture status and reads in its HTTP handler (`docker_ha_compose_shell_test.go:127/237/245`). Fixture response updates and recorded-request snapshots now use the existing mutex; no runtime installer behavior changed. `rt -- bash -lc 'cd server && go test -race -count=3 ./internal/installer -run "^TestBundleBootstrap"'` passes (`ok configflow/internal/installer 3.789s`).
- Race-run timeout: the unchanged OxiDNS corpus reached Go's default 10-minute package limit while executing `TestBoundaryEditorRecorded` in yaml.v3 parsing. The first full run therefore failed for that timeout and the installer fixture race; `rules` passed (`13.444s`). Final full-race command: `rt -- bash -lc 'cd server && gofmt -l . && go vet ./... && go test -race -count=1 -timeout=30m ./...'` → exit 0; gofmt had no output, vet passed, every package passed (`installer 493.913s`, `oxidns 943.550s`, `rules 14.484s`; last line `ok configflow/internal/yamlx 5.893s`). No tests were excluded and race detection remained enabled.
- Post-rebase verification: `rt -- bash -lc 'cd server && gofmt -l . && go vet ./... && go test -race -count=1 ./internal/repo ./tools/proddiff/generator && go test -count=1 ./...'` → exit 0; gofmt had no output, vet passed, repo race passed (`19.077s`), the new generator compiled, and all packages passed (`installer 138.085s`, `oxidns 70.104s`, `rules 3.263s`; final lines `ok configflow/internal/yamlx 0.750s`, `? configflow/tools/proddiff/generator [no test files]`).
- N2 integration follow-up: rebased onto `go` at `86700e16`, including `288394c5`, `87e47955` and `6541a643`. The installer conflict is consolidated into one `setResponse` method using the existing mutex for status/location updates; the locked request snapshot remains, and N2's duplicate `setStatus` is removed. N2's other code is retained. PROGRESS merged automatically; an exact comparison confirmed all content outside S-rules equals upstream (both sides' records retained). All four replayed commits were checked for conflict markers. The final acceptance command below passed on .32; .63 is reserved for iteration per the updated AGENT-BRIEF.
- Final N2-rebase acceptance on .32: `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/ tools/)" && go vet ./... && go test -race -count=1 -timeout=30m ./internal/installer/ ./internal/rules/ ./internal/rulesetfmt/ ./internal/clientrange/ ./internal/convert/ && go test -count=1 ./... && cd .. && server/scripts/proddiff.sh'` → exit 0. gofmt/vet passed; race: installer 263.155s, rules 7.796s, convert 59.935s (rulesetfmt/clientrange compiled); ordinary full suite passed (rules 8.971s, installer 250.491s; final tested package `yamlx 1.808s`, followed by generator `[no test files]`). Proddiff: all four self-checks PASS, 2060 cases, 224 compared, 5 existing unique baseline differences, coverage regressions 0, `baseline: new=0 stale=0`.
- Final rebase: `go` advanced during acceptance by two HANDOFF-only commits (`87ce5bb6`, `c53430b4`). Rebased again to `c53430b4`; an exact git diff confirmed `server/internal`, `tools`, `cmd`, `scripts`, go.mod and go.sum are identical to the tree that passed the .32 command above. No test repeat was needed for eight added HANDOFF lines.
- Current boundary corpus: 140 search records × 57 names, 63 callback Sub records, 60 filters, 115 rule-value checks, 30 rule lines, 144 queries and 20 host names. Native boundary/anchor tests cover 13 patterns × 14 subjects on the actual selected Go engine; CPython boundary expectations were removed rather than tolerated.
- Python coverage map: test_rule_syntax.py → syntax_test.go, boundary_test.go, corpus TestCorpusRuleValueError; test_rule_matcher_behavior.py / test_rule_matcher_index.py → matcher_test.go, corpus TestCorpusMatch, boundary queries; test_rule_cache_identity.py / test_remote_rule_cache_edges.py → ruleutils_test.go, corpus TestCorpusCacheNames and C14; test_domain_matcher_cache.py → matchercache_test.go; test_connection_ip_rules.py → connection_test.go and review_c09_apply_test.go; group_candidates has no Python test file and is covered by groups_test.go, corpus filters/group/lower, review_c09_test.go and the boundary corpus.
- Accepted differences remaining in `differences_test.go`: IPv6 scope ids inside networks (`netip` refuses); Go accepts some patterns Python rejects (variable-width look-behind, flags not at the start, `(?)`, repeated anchors); repeat counts 2^31…2^32-2 rejected (`ErrRepeatTooLarge`); `\N{name}` unsupported; Unicode database / IGNORECASE for İ/ı/ſ; IDNA 2003-only names (fake `xn--` ASCII labels, labels starting with combining marks, rare unassigned code points); multi-character upper-case (`ß`); non-list group source fields (Python iterates strings/dicts); regex error details after Chinese prefixes are Go engine text; `str(network)` of IPv4-mapped IPv6 prints dotted.

## C27 — rule DNS actions

**Status: `verified`** (independent adversarial review; zero open functional findings; final reviewer `.32` acceptance passed) · prereqs: C26 verified · Go: `internal/oxidns`

Files:
- `backend/common/rule_dns.py` (885)

Tests: `converters/test_oxidns.py` (43), `test_oxidns_common_routing.py` (54), `test_oxidns_domain_only_native.py` (9), `test_oxidns_ip_probe.py` (9), `test_oxidns_routing_index.py` (3), `test_oxidns_rule_actions_core.py` (5), `test_proxy_group_dns_follow.py` (6), `test_rule_dns.py` (33), `test_rule_dns_policy_groups.py` (7)

Routes: none.

Scope / implementation plan:
- Read the complete 885-line Python module and all nine listed test files (2,964 lines). Existing C22/C25/S code already implements generation, pinned helper recognition, policy-group defaults and classical-content lookup. Extend it; no second implementation or Python exception/value shim.
- Buildable step 1: typed rule-page state and upstream candidates; public profile-agent filtering, choice parsing, ruleset hydration and explicit-route/read-entry APIs. Free-form upstream/settings documents remain yaml.Node, JSON views use typed structs and encoding/json.
- Buildable step 2: ApplyRuleDNS returns a replacement settings draft plus warnings; it never mutates before/after profiles. C32 installs the returned settings under its transaction only after success. Strict native refusal is a Go RoutingLockedError with typed usage metadata; lenient mode preserves native YAML and returns the existing Chinese warning. Existing common routing owns all rebuilding/parking/resource reconciliation.
- Buildable step 3: synthetic real-Python public-entry reference corpus includes read models, template/native writes, group reorder/follow drift, enable/disable/delete/value/reorder changes, linked/stale upstreams, locked/malformed drafts and final generated YAML. Add boundary assertions on both inputs and generated configurations; compare proddiff public generators on .32, with coverage regressions 0.
- Iteration on .63; final gofmt, vet, oxidns race, ordinary full suite and proddiff on .32 all passed. Each compiling step was committed. Final status is ported; an independent reviewer follows.

Cross-domain contracts:
- The existing classical-IP hook stays the one installed by ippresence; unknown content remains conservative. No private rule_ip_presence copy.
- A16 的 go-port-a16 已提供共享 SettingsFor（待 HR/依赖合入）。The read model decodes the saved library/rules booleans from agent.rule_sync.settings into a typed SyncSettings; read options allow an agent settings adapter when A16 supplies its own type. No workers, HTTP or repository calls are introduced.
- C27 has no routes or repository writes. C32 owns HTTP/auth/transaction/cancellation semantics and converts choice/setting errors to 400 and RoutingLockedError to 409 with usages. C29 owns invalidate-follow at whole-settings replacement; group writers also call the same C27 update API.

Step 1 implemented in `ruledns_state.go` (typed JSON engine/rule/Agent/upstream views):
- `ReadRuleDNS(p *Profile, options RuleDNSReadOptions) (RuleDNSState, error)`; `RuleDNSEntries(p *Profile, ids []string, options RuleDNSReadOptions) (map[string]RuleDNSRule, error)`; `ExplicitRuleDNS(p *Profile, id string) (string, error)`. Empty route means no route; JSON uses null where the Python read model does. Unsupported rows omit route fields. Read options: nil Agents selects ProfileAgents, nonnil empty selects none; optional SyncSettings adapter returns `{Library, Rules bool}`.
- Reviewer API correction approved by coordinator: `RuleDNSAgent.ID`, `.Name`, `.ServiceType` are **`*string`**, with unchanged JSON keys `id`, `name`, `service_type`. Missing/null legacy metadata encodes JSON null; present empty strings remain empty strings. C32 must handle nullable values. Pointers are independent values, not aliases into the caller's YAML nodes.
- `ProfileAgents(p *Profile) []*Node` (copies); `PolicyDNSRoutes(p *Profile) map[string]string`; `FollowRoute(item *Node, p *Profile) string` (empty route = no DNS route).
- `UpstreamCatalog(settings, document *Node, bindings []DNSBinding) ([]UpstreamCandidate, error)`; nil document parses the template lists or saved native YAML. Candidate includes key/source/addr/label/linked and `Entry json.RawMessage`; the read model hides Entry. Bindings contain UpstreamTag and UpstreamIndex.
- `HydrateRuleset(item, library *Node, profileID string) *Node` returns a copy; C32 assigns the returned reference. It clears old library source fields, retains rule-owned fields/unknown metadata and uses a profile content URL for inline sources.
- `.63`: gofmt, `go vet ./internal/oxidns` and `go test -count=1 -run "^TestReviewNativeRevision$" ./internal/oxidns` passed (`0.006s`). No new dependency or jsonx import.

Step 2 implemented in `ruledns_apply.go`:
- `ApplyRuleDNS(before, after *Profile, assignments []DNSAssignment, options RuleDNSApplyOptions) (RuleDNSUpdate, error)`. Assignment has RuleID/Choice strings; slice order is retained, duplicate IDs use the last choice. Options has BaseURL and Lenient (false = strict). Result has Settings *Node, Warnings []string and Changed bool. Both profiles remain unchanged on success/failure; C32 writes the returned Settings to its transaction's after profile only when Changed. Other rule/group changes remain the caller's responsibility.
- `CheckDNSChoice(choice string) error`; invalid choices and ordinary assignment failures are *SettingsError (use IsSettingsError → HTTP 400). Locked native rewrites return *RoutingLockedError (errors.Is(err, ErrRoutingLocked)); Reason/Error and typed Usages `{kind:oxidns, code:oxidns_routing_custom, target:sequence_main, reason}` → HTTP 409. Lenient mode preserves native text, prunes removed-rule metadata and returns the existing Chinese warning.
- `InvalidateFollow(p *Profile, settings *Node) (*Node, error)` returns a copy, clearing intent only when an active rule's actual replacement route changed. C29 uses this before whole-object replacement.
- `Convertible(item *Node) bool`, `IsIPRuleType(kind string) bool`, `HasIPPart(item *Node, p *Profile) bool`, `RuleDNSMetadataFields() []string` are C32-facing helpers. The metadata fields include parked data; the older routing-only RuleDNSFields slice deliberately does not. RouteLabels/Selectors/RouteKey/NormalizeAction remain shared with existing generation.
- Strict/lenient writes reuse ApplyRouting, compare newly introduced validator errors, keep old legacy issues editable, and preserve native YAML comments on edits. Missing/malformed stored actions read as default and are dropped on the next relevant write.
- `.63` compile/vet/hash regression check passed (`0.006s`); public-boundary behavioral verification follows in step 3.

Step 3 implemented:
- `testdata/c27_gen.py` invokes real Python public read/write entry points in a temporary DATA_DIR, with synthetic addresses/domains only. `c27_public.jsonl.gz`: **282 references**, including **148 successful final OxiDNS configurations after applying assignments**. It covers every action together and separately, local/remote/fallback pins, duplicate-pin sharing, cache/lazy TTL/remote TTL/response hooks, template/native modes, group default/follow changes, reordered/deleted/disabled/re-enabled rules, parked explicit choices, changed rule values, malformed stored actions, stale/linked/current pins, manual pin edits, existing/new validator errors, strict/lenient conflicts, Agent subsets/saved sync flags, source hydration and follow invalidation.
- `TestRuleDNSPublicBoundary` compares the full generated YAML structures, read models, replacement settings, warnings, Chinese rejection messages, typed lock usages and argument post-state without difference waivers. It additionally requires byte-exact unchanged input profiles and preservation of native draft comments. `TestRuleDNSAssignmentOrder` covers ordered duplicate assignment semantics and unknown-field/input preservation. All choices are validated before rule lookup, preserving the public error precedence for multi-error requests.
- `.63`: targeted race `go test -race -count=1 -run "^TestRuleDNS" ./internal/oxidns` passed **10.816s**; ordinary full oxidns passed **41.409s** before the final error-precedence assertion/helper additions. No production code outside internal/oxidns changed; no new jsonx/pystr/urlx import, dependency, Python-value or exception shim.

Python test-file → boundary coverage (function counts; parameterized ids expand them):

| Python file | Functions | Go coverage / explicit exclusions |
|---|---:|---|
| `converters/test_oxidns.py` | 43 | Existing `TestBoundaryGenerationRecorded/Corpus`, `TestReviewSReferences`; new C27 final-generation cases cover all rule actions, ordering/aggregation, pinned copies, independent caches, response hooks and domain-only classical sets. Old byte hashes/quote spelling are structural comparisons under the accepted formatting policy. |
| `test_oxidns_common_routing.py` | 54 | Existing `TestBoundaryRoutingRecorded/Corpus`; new C27 native writes, before-profile reconciliation, multiple deletion, explicit/follow park/restore, manual helper conflicts/addresses and generated outputs. S editor tests and the C27 comment assertion protect user edits. |
| `test_oxidns_domain_only_native.py` | 9 | Existing native migration/generation/download boundaries; C27 IP assignment refusal/default and classical-domain outputs do not re-enable probes. Agent rule-sync projection stop/publish assertions remain A16, not a C27 worker implementation. |
| `test_oxidns_ip_probe.py` | 9 | Existing real ippresence hook and routing/generation boundaries cover known/unknown/changed classical metadata; `HasIPPart` uses the same hook. New generated configs retain managed domain-only routing. Cache memo/filesystem behavior itself belongs to ippresence, exercised by full-suite tests. |
| `test_oxidns_routing_index.py` | 3 | Existing public routing graph corpus and new generated-output/ordering comparisons prove the recognizer's result. No private Python rescanning/index algorithm is ported. |
| `test_oxidns_rule_actions_core.py` | 5 | New all-action cache/features/plain/native/classical output cases cover the generator/native-applier side. Optional real-core DNS query/cache/timeout tests require OXIDNS_TEST_BINARY and were not run; no binary or actual DNS responder behavior is claimed here. |
| `test_proxy_group_dns_follow.py` | 6 | New group-default reorder, explicit versus follow, disable/reenable and locked-native update cases exercise the complete C27 snapshot API. HTTP/MCP auth, one-revision saves, selected-profile isolation and repository rollback remain C32/C33/MCP route-owner boundary tests; C27 exposes immutable snapshots and typed conflicts for those transactions. |
| `test_rule_dns.py` | 33 | Existing native action normalization/generation/helper boundaries plus C27 state/default/unsupported/disabled/follow drift, malformed reads/cleanup, catalog links, hydration, assignment vocabulary/error order, pin retention and strict/lenient lock cases. Typed RoutingLockedError replaces the Python exception hierarchy. |
| `test_rule_dns_policy_groups.py` | 7 | Shared native RenderMembers remains the sole membership implementation; C27 synthetic group matrix covers manual/proxy/REJECT, explicit order, strategies-first, source compatibility, follow-group source, non-manual groups, disabled/missing source, buckets/exclusions and follow updates. |

C32 integration: construct **complete resolved snapshots after the rule/group changes**, pass only explicitly supplied dns fields as ordered DNSAssignment entries, call ApplyRuleDNS under its existing transaction, abort on error, and install Settings only if Changed. Persist the caller's other edits and warnings in that same transaction; never save the returned draft before the conflict check. C29 uses InvalidateFollow on complete-form replacement. ProfileFromJSON is the native JSON snapshot entry; NewProfile/ToJSONX remain the existing adapter to the old repository. ToJSON returns settings JSON bytes suitable for json.RawMessage. Engine.InUse has the unchanged JSON name oxidns_in_use. No runtime behavior from those routes is claimed until their modules wire these APIs.

Reviewer C32 transaction contract: detach **before taking repository locks** with `context.WithoutCancel(c.Context())` (Scope is retained). Build before/after from the resolved snapshot supplied by the transaction callback, not a stale snapshot read before the lock. `TestC27ReviewCanceledTransactionWaitsAndCommits` proves a canceled request still waits for a contended lock and preserves the preceding writer; the cancellable negative control returns context.Canceled. `TestC27ReviewConcurrentTransactionsMerge` proves 12 concurrent canceled-but-detached writes retain all rules/DNS choices and exactly 12 revisions, without changing another profile. Strict native conflict rolls back the caller's rule change, DNS settings and revision together. C27 itself remains synchronous snapshot computation, with no workers or repository writes.

Reference command: `RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/oxidns/testdata/c27_public.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/c27_gen.py'`.

Final verification on native .32 (tested integration base **e6c87cc1**):
- Required command: `rt -- bash -lc 'cd server && test -z "$(gofmt -l .)" && go vet ./... && go test -race -count=1 ./internal/oxidns/ && go test -count=1 ./... && cd .. && server/scripts/proddiff.sh'` (wrapped with uptime/log capture to `/tmp/c27-final.log`) → **exit 0**. Formatting had no output; vet passed.
- Oxidns race **340.910s**, default timeout, no race warnings. Ordinary full suite passed: oxidns **89.184s**, installer **155.837s**; final lines `ok configflow/internal/weburl 0.007s`, `ok configflow/internal/yamlx 0.774s`, `? configflow/tools/proddiff/generator [no test files]`.
- Proddiff generator mode: OxiDNS config, agent OxiDNS config, custom files and downloads each had **2 successes / 2 compared / 0 both-error**. Overall 2060 cases / 282 compared, four self-checks PASS, **coverage_regressions=0**, 5 existing unique baseline differences (agents:value 3, config:value 2), **baseline: new=0 stale=0**. No baseline/coverage exception was added. Existing missing HTTP generators and 20 Python/20 Go connection-failed cases are not claimed as successful comparisons. Synthetic C27 writes additionally compare all 148 final generated configurations, without depending on the snapshot using rule actions.
- Uptime start `17:52:35 up 2:51, 3 users, load average: 1.64, 4.28, 5.23`; end `18:01:53 up 3:00, 4 users, load average: 16.34, 8.97, 6.76`.
- Final polish only renamed the Go view field to InUse (wire JSON tag unchanged) and documented Error/Unwrap. Follow-up `rt -- bash -lc 'cd server && test -z "$(gofmt -l .)" && go vet ./... && go test -race -count=1 -run "^TestRuleDNS" ./internal/oxidns/'` → exit 0, **16.902s**. All 282 references / 148 generated results and the ordered-assignment/input/comment checks still pass.

Author hand-back: no implementation remained; no dependency, Python emulation, new jsonx/pystr/urlx import, route registration or repository write was added. Go APIs deliberately return replacement drafts/references rather than mutating caller objects; callers install those results as documented. Module source/test features are retained; optional core execution and route/transaction/auth behavior remain with the owners above. Author branch **go-port-c27**; independent review follows below.

Reviewer acceptance (`go-review-c27`): [review-c27.md](review-c27.md). Implementation/coverage commits `2207a08f`, `857dd389` (plus final acceptance commit). New independent corpus: **505 rows**, **469 strict comparisons / 314 additional final documents**, plus **36 explicitly asserted D2 outcomes** (not blanket waivers); Python has 93 ordinary rejections. Fixed medium helper-node aliasing and low null-Agent JSON bug. Replaced NUL issue keys by a struct; removed the `behaviorOf` artificial NUL type sentinel using native Node type/value predicates without changing classification. Shared snapshot/return-value ownership, canceled lock waits, 12-writer merging and strict rollback pass race. D2 native JSON identities deliberately remain changed; a reproduced medium safety defect (old fallback pin guessed as remote after its hash changed) now fails conservatively: no source heuristic when separator-bearing entry has no native source hash match; locked read/recovery reason, typed 400/409 or lenient warning, unchanged settings/YAML. Matching native hashes remain editable. [D2 recovery](behaviour-changes.md) and C29 note are recorded. Four intentional mutations (helper alias, nullable fields, source guess, missing detachment) all fail the new tests, then pass after restoration. Final `.32`: gofmt no output, vet pass, full oxidns race **818.857s**, all-package suite pass. After rebasing onto N8-1 at `5535931d`, reran vet, C27+author targeted race **97.916s**, and full suite (oxidns **255.812s**, installer **343.139s**), all pass. Final proddiff: **23 self-checks PASS**, 65 GET / 66 method-path / 415 comparisons, 10 existing global differences, **new=0 stale=0 coverage_regressions=0**, unmapped_routes=0. One preceding run had two Python-only domain-discovery transport failures; a source/baseline-unchanged rerun cleared them (30 Python/30 Go failures remain symmetrically counted, not successes). No baseline/normalization change. No push; clean rebased hand-back.

Clarified in C32: `RuleDNSAgent.Name` now uses independently encoded `json.RawMessage`; legacy numeric, array and null display names are preserved exactly rather than silently becoming null. `TestRuleDNSAgentLegacyNames` covers all three classes and returned-byte isolation; C32 `TestDNSAgentReadModelLegacyNames` compares them against real Python HTTP.

## C28 — MosDNS archive and routes

**Status: `verified`** (review C28 below, 2026-11-16; ported under the idiomatic-Go / boundary-acceptance rules) · prereqs: C21, C15 · Go: `internal/mosdns` (`archive.go`, `ruleproxy.go`), `internal/api/mosdns` (routes)

Files → Go:
- `backend/utils/mosdns_archive.py` (244) → `mosdns/archive.go`: `BuildMosDNSZip(ctx, snapshot *jsonx.Object, yamlContent string, []Download, []File, ArchiveOptions{BaseURL, SkipReferenceValidation})` → ZIP bytes or `*ArchiveError` (= MosdnsArchiveError; OxiDNS archive C29 uses the same type), `ArchivePath(string)`, `Download`/`File` + `DecodeList[T]` (converter lists → typed), `MaxWorkers`/`MaxFiles`/`MaxTotalBytes`. Callback resolution reads a typed `snapshot` (profile_id, system_config, rule_configs, rule_library) decoded from the compat config; remote sources go through `rulefetch.ResolveRemoteRuleSource`, `convert.RulesetFormat`, `refresh.ReadRuleSource`; zip via `archive/zip`.
- `backend/routes/mosdns.py` (594) → `mosdns/ruleproxy.go` (`RuleProxy(ctx, RuleProxyRequest)` → `RuleProxyResponse{Status, Text|JSON, NoStore, Stale}`; `fetchRemote` = `_fetch_remote_content`; `authorized` = `_require_rule_proxy_auth`; `localContent` = `_local_content_rule`) and `api/mosdns/mosdns.go` (7 editor routes table-driven, `POST /api/mosdns/settings` in one `UpdateProfileTransaction`, cache normalization `normalizeCache`, rule proxy + profile wrapper). Routes registered in `server.routePackages`; the two rule-proxy paths stay public (already in `auth` gate).
- Not ported: `_load_cached_rule_content_for_url` — no caller in `backend/` (only `test_rule_cache_identity.py` calls it directly); its behaviour (find the validated cache of a URL) is what `refresh.ReadRuleSource` does for the live paths.

Rule 4b: handlers and `RuleProxy`/`BuildMosDNSZip` run on `context.WithoutCancel` (cache writes, task records, archive workers).

Acceptance (boundary):
- **Flask↔Go differential** `internal/mosdns/difftest` (real Flask + Go app on copies of one data dir, loopback fake rule server): all 15 routes, 50 requests — GET/POST of the 6 editor routes (success, unknown rule 409→500 JSON, text body 415→500, non-object body, broken JSON, null bodies, odd cache values), `POST /api/mosdns/settings` (success, `?profile=`, non-object 400, missing rule 409, unknown profile 404), rule proxy (inline via rule-proxy token / config token / JWT on the profile path, declared remote → cache + task status, cached read, classical `part=ip`, invalid content 422, MRS 422, undeclared 503 → 502, changed source 422, no/wrong token 401, no url / bad part / ftp 400, unknown profile 404). Compared: status, JSON (structural), text bodies, Cache-Control/X-ConfigFlow-Rule-Cache, then the stored `mosdns` settings, rule cache files and task-status.json (times masked): **50 requests, 4 data items, 0 differences**.
- **Archive outputs**: `archive_test.go` `TestArchiveMatchesRecordedPythonBuilds` rebuilds every `build_mosdns_zip` call of the whole Python suite (recorded by `testdata/c28_record.py` → `c28_recorded.jsonl.gz`, 12552 passed under the recorder) from the same snapshot, rule caches, task status and download answers: entries (names, order, bytes), Chinese failure messages and rule caches written afterwards — **86 builds, 0 differ** (the racy cancellation test excluded; covered by a hand test). The other recorded rows (proxy/auth/fetch internals) are kept in the file but not replayed (function-level replay no longer required).
- Hand tests: bounded parallel downloads (peak = 8), a failed download returns without waiting for in-flight ones, size/file budgets (file budget checked before any download), unsafe/conflicting paths, missing references, label sanitization. `-race` clean.

Differences from Python (functional parity; not chased): ZIP bytes (Go deflate, extended timestamps; entries/contents equal); Python-internal exception texts in 500 JSON bodies of the editor routes for a JSON body that is not an object (`'list' object has no attribute 'get'` → `request body is not a JSON object`, same status); hand-edited non-string/non-bool values in the snapshot (e.g. numeric rule ids) make the archive/proxy refuse (decode error) where Python compared them loosely; `config.yaml` mappings with non-string keys are refused by the reference check; `cache_dump_file` that is a list/dict is stored as its JSON text (Python: repr); integer settings beyond int64 fall back to the default.

Python test files → coverage:
- `test_mosdns_archive.py` (15): archive outputs via the recorded builds (all cases incl. transport/cache/wrong-profile/unsafe paths/references/conflicts); parallelism, cancellation, budgets also hand-ported; the generate-route cases (`/api/profiles/<id>/generate/mosdns`) → C34.
- `test_converter_review_regressions.py` (7), `test_mosdns_regex_delivery.py` (2), `test_mosdns_rule_fields.py` (5), `test_domain_wildcard_delivery.py` (3): archive halves via recorded builds; rule-proxy conversions = C21 converter (verified) + differential (inline, classical, part); generate halves → C34.
- `test_remote_rule_consumers.py` (16): archive cases via recorded builds; rule-proxy cases (invalid content 422, cache after invalid refresh, MRS, non-UTF-8) via differential; Agent/bundle cases → A11.
- `test_request_exception_redaction.py` (mosdns case: no cache fallback on a failed undeclared fetch) → differential `proxy undeclared down`.
- Also touching C28: `test_security_regressions.py` rule-proxy auth (token, retired token, JWT, malformed bearer, oversized token, URL validation) → differential (token/JWT/401/400) + `rulefetch` (C14) for URL/redirect/size rules; `test_rule_proxy_freshness.py`, `test_rule_fetch_proxy_routes.py`, `test_profile_rule_routes.py`, `test_editor_transactions.py`, `test_save_publish_guard.py` (mosdns parts) → differential; `test_rule_cache_identity.py::_load_cached_rule_content_for_url` → not ported (no caller).

Size: Python 838 lines; Go non-test 1156 (archive.go 568, ruleproxy.go 311, api 277) = 1.38×. Reasons: explicit worker scheduler, Go error returns, URL origin comparison and typed snapshot decoding; the seven near-identical Python editor routes are already one table-driven handler.

Commits: b6f5f77a (first port), ae4c5bbe (replay harness, superseded), the reshape commit (idiomatic Go, boundary tests, weburl), + this commit.

Commands:
- tests: `rt -- bash -lc 'cd server && go vet ./internal/mosdns/... ./internal/api/mosdns/ && go test -count=1 -race ./internal/mosdns/... ./internal/server/'`
- differential: `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/mosdns/difftest/'`
- re-record archive builds (~4 min): `rt --back server/internal/mosdns/testdata/c28_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/c28rec; C28_RECORD_DIR=/tmp/c28rec PYTHONPATH=.:server/internal/mosdns/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 8 --dist=worksteal -p c28_record -p no:cacheprovider; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/mosdns/testdata/c28_record.py merge /tmp/c28rec'` (merge must run in the same `rt` call: a later sync deletes the untracked file on the server)

### Review C28 (adversarial, 2026-11-16)

Walked `mosdns_archive.py` (`_path`, `_label`, `_local_path`, `_source`, `build_mosdns_zip` incl. scheduling, budgets, reference check) and `routes/mosdns.py` (7 editor routes, `_cache_settings`, `save_mosdns_settings`, `_fetch_remote_content`, `_require_rule_proxy_auth`, `_local_content_rule`, `mosdns_rule_proxy` and its exception ladder) against Go.

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | medium | Accepted difference "hand-edited snapshot values with the wrong JSON type make the archive/proxy refuse" was user-visible: one rule configuration anywhere in the profile with e.g. a numeric `library_rule_id` or a string `enabled` (MCP/API writes are not type-checked for non-ruleset items) made **every** rule-proxy download 500 and every MosDNS ZIP fail; `enabled: null` counted as enabled (Python: falsy) | snapshot fields are `text` (string or the JSON text of another value, compared like Python's `==`) and `truth` (Python truthiness, missing = true) | `TestReviewSnapshotToleratesOtherJSONTypes`; differential seeds a numeric `library_rule_id` + `enabled: "yes"` rule and an `enabled: null` selection |
| 2 | medium | Reference check decoded `config.yaml` into `map[string]any` only: a mapping with non-string keys (kept and dumped by the C21 converter since review C21) made the archive refuse ("MosDNS 配置格式无效") where Python packs it; a plugin that is not a mapping **panicked** (single-value type assertion); `plugins: null` / `files: null` were accepted (Python refuses) | `lookup` reads `map[string]any` and `map[any]any`, presence-aware; non-mapping plugin → plain error (Python AttributeError) | `TestReviewReferenceCheck` |
| 3 | low | Rule proxy: a URL Go's `url.Parse` rejects (`http://a b/x`) was a 500 from `localContent` (Python: 400 via the URL check); user info in the URL was ignored when matching the origin (Python compares the netloc); 400/500 choice partly by emulated class names; non-UTF-8 cache was a `FetchError{Class:"UnicodeDecodeError"}`, i.e. a *RequestException* → 500 (Python 400) | unparsable URL = not local; netloc incl. user info compared; own errors are Go values (`errSizeLimit`, `errContentLength`, `errNotUTF8`, `rulefetch.ErrInvalidURL`), repo/store/fs/download errors by type; only rulefetch's untyped errors still fall back to its class (TODO(N9)) | `TestReviewRuleProxyBadRequestErrors`, differential `proxy malformed url` (400) |
| 4 | low | Accepted difference "integer settings beyond int64 fall back to default" was reachable from the editor's number fields (Python stores the exact integer, also `1e30` → its exact float value) | `intOf` returns `*big.Int` with `int()` semantics (bool, truncated float, inf → default, `_` digit groups) | `TestReviewIntOfMatchesPythonInt` (CPython 3.11 references), differential `post cache-settings big ints` (stored values compared exactly with `UseNumber`) |
| 5 | low | Differential harness gap: the Go app did not register the in-process dispatch (`RegisterLocal`, done by `main`), so callback URLs that Python serves through its own API could not be compared; `Content-Type` and the `Authorization` paths were not exercised | harness registers it, compares `Content-Type`, sends custom `Authorization` headers; 14 new requests | – |

Added differential requests (64 total): retired rule-proxy token as bearer (with valid query token) and as query token → 401; non-JWT bearer + valid token → 200; Basic header, rule-proxy token as bearer → 401; empty bearer + config token → 200; 5000-char token → 401; malformed URL → 400; bad `%` escape → 502; relative callback URL → 200; cache big ints; two **deferred to C33** (`deferTo`): a URL with user info and an `enabled: null` inline source are served by Python through an in-process call of `/api/profiles/<id>/rule-library/content/<id>`, which is `todo` in routes.tsv — Go answers 502 until C33 registers it (the harness logs them; they fail if Go answers anything but 502). C33's reviewer: drop the `deferTo` marks.

Accepted differences (re-judged): ZIP bytes; Python exception texts in 500 bodies; `cache_dump_file` list/dict stored as JSON text (Python repr; the editor sends a string); non-string `content`/`url` of a library rule read as "" (repository validates library entries); `;params` in a self-URL's last path segment (Python's `urlparse` splits them off; no generated URL contains `;`); invalid percent escapes in a remote URL refused at preparation in the archive (same failure, "准备失败" instead of "下载失败"); `parse_qs` vs `url.Query` on `;`-separated or badly escaped callback queries; `errors='replace'` per-byte vs per-sequence U+FFFD in undeclared downloads (garbage lines either way).

Checks with no finding: auth ladder (internal call is a secret header token; retired set = persisted minus current; bearer parse/length; JWT failure falls through to the query token; length cap), status codes and Chinese messages of every branch, `Cache-Control`/`X-ConfigFlow-Rule-Cache`, declared/undeclared fetch paths (declared sources are always `source_type: url`, so Python's non-url branch is dead), MRS 422, stale cache warning, editor routes (defaults seeded per route, legacy `api_addr`, cache field back-fill, 415/JSON/null bodies, publish guard), `POST /settings` transaction and key order, `_path`/`_label`, callback resolution (profile prefix, `/api/` strip, rule-proxy nesting depth 3, `part` validation, selection), budgets and file order, bounded workers and early return (`-race`), rule 4b (all handlers and `RuleProxy`/`BuildMosDNSZip` detached). "Not ported" `_load_cached_rule_content_for_url` confirmed: only `test_rule_cache_identity.py` calls it and `test_request_exception_redaction.py` monkeypatches it to assert it is *not* used (no-cache 502 → differential `proxy undeclared down`). `weburl` is N2's package (committed here only so HEAD built); not counted.

Size: before 1156 (1.38×), after 1245 (archive.go 640, ruleproxy.go 318, api 287) = **1.49×** (≤ 1.5; tolerant snapshot types and the reference walker are the growth).

Review commit: see `git log --grep 'review C28'`. Open findings: none.

Commands (review): `rt -- bash -lc 'cd server && go vet ./internal/mosdns/... ./internal/api/mosdns/ && go test -count=1 -race ./internal/mosdns/... ./internal/api/mosdns/ ./internal/server/'` → `ok configflow/internal/mosdns 21.6s`, `ok …/difftest`, `ok …/api/mosdns`, `ok …/server`; differential `CONFIGFLOW_DIFF=1 … ./internal/mosdns/difftest/` → "64 requests, 4 data items, 0 differences" (2 deferred to C33). Run on a private worktree of HEAD 1fa88e9f: the shared tree's uncommitted urlx/rulefetch work changes `rows_test.go` texts.

## C29 — OxiDNS native apply, archive, routes

**Status: `verified`** · prereqs: C27, C28 · Go: internal/oxidns, internal/api/oxidns (new)

C27 reviewer integration note: the shared recognizer now locks a pinned chain when its upstream tag/string metadata contains U+2028/U+2029, no Go-native source hash matches, and the old source would otherwise be guessed by shape. This is intentional D2 safe failure, not a custom-lock condition to bypass. Preserve the recovery message, typed 400/409 or lenient warning and original YAML. Template users refresh/reselect; native users regenerate routing in the editor from current upstreams. Do not add a legacy JSON hash adapter or silently rewrite the pin's source. Details: [behaviour changes](behaviour-changes.md), [C27 review](review-c27.md). C29/C32 still own route auth, request detachment and one-transaction persistence; the snapshot API tests do not claim their HTTP routes are implemented.

Files:
- `backend/common/oxidns_native_apply.py` (520)
- `backend/utils/oxidns_archive.py` (28)
- `backend/routes/oxidns.py` (191)

Tests: `test_oxidns_archive.py` (4), `test_oxidns_common_api.py` (2), `test_oxidns_download_aliases.py` (5), `test_oxidns_hosts_cleanup.py` (9), `test_oxidns_integration.py` (16), `test_oxidns_native_apply.py` (31), `test_oxidns_native_cleanup.py` (11), `test_oxidns_native_restore.py` (20), `test_oxidns_settings_routes.py` (7), `test_oxidns_withdrawn_downloads.py` (10)

Routes (18/18 done):

`POST /api/oxidns/common-settings`; `POST /api/oxidns/common-settings/apply`; `POST /api/oxidns/editor`; `POST /api/oxidns/native-settings/apply`; `POST /api/oxidns/render`; `GET /api/oxidns/settings`; `POST /api/oxidns/settings`; `POST /api/oxidns/template-preview`; `POST /api/oxidns/validate`; `POST /api/profiles/<profile_id>/oxidns/common-settings`; `POST /api/profiles/<profile_id>/oxidns/common-settings/apply`; `POST /api/profiles/<profile_id>/oxidns/editor`; `POST /api/profiles/<profile_id>/oxidns/native-settings/apply`; `POST /api/profiles/<profile_id>/oxidns/render`; `GET /api/profiles/<profile_id>/oxidns/settings`; `POST /api/profiles/<profile_id>/oxidns/settings`; `POST /api/profiles/<profile_id>/oxidns/template-preview`; `POST /api/profiles/<profile_id>/oxidns/validate`

Evidence: `internal/oxidns/native_apply.go`, `native_activate.go`, `native_input.go`, `archive.go`,
`settings_store.go`, `internal/api/oxidns` and server registration. All new handlers use N8-6
BindSilent (Python silent JSON reads). Native apply has 222 actual Python public input/output/post-state
references; archive has 18 actual Python ZIP cases (content, order, permissions); the live Flask
HTTP differential compares responses and complete exported DB/all data files after each request.
Route inventory/dispatch conflict, cancellation and atomic competing If-Match saves have regressions.
Detailed scope, commands, remaining acceptance and N8-5 adapters: [C29 progress](progress/c29.md).

Missing / deferred: no C29 implementation deferred; shared in-flight shutdown windows remain assigned to O3-6c below.
Final .64 gate (-p6) passed: gofmt/vet/tags/full race/all15 difftest, proddiff new=0 stale=0, both specified fuzz60s.
Rebased onto go@0dace4ae (C32 review); C29 runtime code unchanged from the gated version.
Merged C29/C32 API differentials and all new C29 tests under race passed again.
Independent review `go-review-c29`, zero open C29 findings; fix `496b16b5`: exact-case DTO keys,
updater error location, template reader policy/alias cause propagation (three medium). Full core,
API/difftest, scoped race/vet/gofmt and per-input reader gate pass on .64 (both unknown=0);
both oxidns fuzz targets pass 60s on .32. Final .64 gate -p6 waited over15min without starting,
then stopped per coordinator instruction (exit143, no checks started); coordinator runs cf-gate
for full race/all difftest/proddiff new=0 stale=0. [Review evidence and exact commands](review-c29.md).
Author evidence and commands: [C29 hand-back](C29-HANDOFF.md).
The approved Python HEAD save bug is fixed as read-only with an explicit expected differential;
N8-6 non-finite JSON rejection has 48 explicit Flask controls across all 16 POST pairs;
C29 SaveSettings/BuildZIP now check shutdown after Detach and before shared commits; real Runtime
shutdown/disconnect regressions pass under race. Shared in-flight archive/cache/taskstatus/repo final
commit windows are explicitly assigned to 6e5c7760 (go-o3-6), O3-6c; C29 does not duplicate those fixes.
reader policies follow N7-3d §5.7 (unknown=0). No new dependency or Python compatibility hash adapter.

N8-5 follow-up: settings_store.go storedSettings and the transaction assignment bridge, the render
snapshot/NewProfile bridge in api/oxidns, and archive.go's shared MosDNS snapshot argument migrate
when the typed repository API lands. New stored settings already use model.Dynamic.

Python test coverage: native_apply/native_restore/native_cleanup/hosts_cleanup →
TestC29NativePythonBoundary (170 actual pytest cases, 7 API cases deselected from recording and covered
by live HTTP workflow). archive → TestC29ArchivePythonBoundary (18 cases) plus unsafe-YAML refusals;
common_api/settings_routes/validation_api → api/oxidns/difftest workflow/route matrix and
settings_store_test cancellation/concurrent guard checks. download_aliases/withdrawn_downloads and
shared integration logic reuse C25–C27 boundary suites, rerun as part of the complete oxidns package.
Profile clone/backup/import, publication/device delivery/MCP remain owned by their existing modules;
C29 exposes the complete package builder and editor/settings boundaries without duplicating those owners.

Notes for porter: Also registers the 9 `/api/profiles/<id>/oxidns/*` wrapper routes (rule 8: the delegate package registers the profile wrapper). `oxidns_archive` imports `mosdns_archive` (C28).

## C30 — subscription and node routes, node deletion

**Status: `verified`** · prereqs: C17, C16, C19, C20 · Go: internal/api/subscriptions, internal/api/nodes

Files: `backend/routes/subscriptions.py` (696), `backend/routes/nodes.py` (213), `backend/common/node_deletion.py` (85).

Routes (26/26 registered; 5 C16 latency methods retained):

`GET /api/nodes`; `POST /api/nodes`; `DELETE /api/nodes/<node_id>`; `PUT /api/nodes/<node_id>`; `POST /api/nodes/delete`; `POST /api/nodes/delete-preview`; `GET /api/nodes/latency`; `POST /api/nodes/latency`; `POST /api/nodes/latency/dns/check`; `GET /api/nodes/latency/settings`; `POST /api/nodes/latency/settings`; `POST /api/nodes/parse-lines`; `POST /api/nodes/reorder`; `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`; `GET /api/subscriptions`; `POST /api/subscriptions`; `GET /api/subscriptions/<resource_id>/health-impact`; `DELETE /api/subscriptions/<sub_id>`; `PUT /api/subscriptions/<sub_id>`; `POST /api/subscriptions/<sub_id>/fetch`; `GET /api/subscriptions/<sub_id>/nodes`; `GET /api/subscriptions/<sub_id>/proxies`; `GET /api/subscriptions/health`; `GET /api/subscriptions/proxies`; `POST /api/subscriptions/reorder`; `GET /api/subscriptions/test`

Independent review complete (`go-review-c30`), zero open C30 findings. Fixes: `6c80c783` constant-time public/internal capability comparison; `2ef264ae` legacy non-array cache summaries plus deletion rollback/concurrency tests; `37fa3455` malformed fetch/reorder bodies rejected before work; `729c2452` null-cache fallback without panic; `3e088219` UTF-8 BOM decoding. Coverage: `82f78031`, `627bed76` (all 25 global method/path pairs reject URL-token-only access; explicit profile provider tested separately).

Final .32 acceptance at `3e088219` after rebase onto `go` (`e6c87cc1`): gofmt has no output, whole-tree vet/full suite pass; scoped race subscriptions 1.406s, nodes 4.922s, auth 1.430s; live Flask differential 67.413s. Proddiff: 65 GET templates, all self-checks PASS, coverage_regressions=0, new=0, stale=0, no baseline/normalization edits. One earlier transient Agent metrics summary difference disappeared on rerun; paths and limits are recorded in `parity/review-c30.md`. A11 diagnostics were subsequently closed by A11-2; C31 aggregation materialization remains explicitly deferred; missing C31 materialization rejects activated single/combined delivery with unchanged storage. Full findings, boundary coverage, exact commands/output and commit list: `parity/review-c30.md`.

Done: `bf499d37` implements node CRUD/parse/reorder/reviewed deletion and `nodes.Wire` → `repo.Hooks.PlanNodeDeletion`; `f19ee585` implements subscription management/cache preview/refresh/health/reorder and single/combined provider exports. `b063069d` adds Flask↔Go boundary acceptance and fixes found by it (URI conversion-record initialization/reuse, legacy health count adapter, semantic comparison of unaffected group JSON).

Acceptance: `internal/api/subscriptions/difftest` compares each HTTP response and the complete exported SQLite document plus every JSON/YAML data file after each request. Route matrix and detailed mapping: `parity/difftest-c30.md`. Independent review adds URL/JWT/rotation/BOM/malformed body/null-cache cases, direct deletion commit-failure/reopen/concurrent-reference tests and the missing-aggregation fail-closed check. Final independent .32 results are recorded above and in `parity/review-c30.md`; the original porter's acceptance (including C16 live differential and C30 live differential under race) remains in `parity/C30-HANDOFF.md` as historical evidence.

| Python tests | Boundary coverage / remaining owner |
|---|---|
| test_dialer_activation_shadow.py (3) | TestProviderDeliveryGraphs, existing C17 snapshot validation; generate/download/push consumers belong to C34/A14. |
| test_dialer_combined_export.py (9) | TestCombinedCrossFeedAndStaging, combined graph cases, profile isolation and concurrent snapshot cases. |
| test_dialer_delivery_snapshot.py (3) | single graph cases and TestDeliverySnapshotSurvivesConcurrentProfileChange; Agent payloads belong to A14. |
| test_dialer_delivery_staging.py (5) | final cross-feed graph/staged-cache comparison plus existing C17 missing-feed/late-activation tests; push consumer belongs to A14. |
| test_dialer_proxy.py (23) | node CRUD/reference guards/reorder, reviewed deletion and alternate formats; proxy-group/aggregation/generate entry points belong to C33/C31/C34. |
| test_dialer_standalone_activation.py (4) | URI graph/fallback/format/snapshot cases; aggregation/main-preview counterparts belong to C31/C34. |
| test_node_deletion.py (9) | reviewed deletion, all blockers, stale/unrelated writes, empty sources, unaffected groups; nodes/review_test.go directly checks commit failure with reopen, concurrent affected-profile writes and cancellation. |
| test_subscription_cache_preview.py (4) | TestCachePreviewNeverRefreshes (8 states, repeated read-only calls), TestFetchWithNullCacheNodes, imported/missing/auth cases. |

Decisions/differences:
- Native encoding/json and yaml.v3; unknown stored fields retained as json.RawMessage. Production jsonx usage is confined to adapter.go; no new pystr/urlx or dependencies.
- Auth is unchanged: global paths require JWT at the gate; explicit profile provider aliases accept token/JWT in the handler. Correct/wrong/missing/internal/retired token boundaries and token-free logs are tested.
- All writes/shared downloads detach request cancellation. Main graph is captured before fetch; URI conversion is materialized once and reused; provider caches/artifacts are committed only after final graph validation.
- Existing confirmation protocol preserved exactly (sorted UTF-8 JSON with comma/colon spaces, SHA-256). Byte formatting/comments of YAML are not preserved; parsed content is compared. Native regex/transport diagnostic identities are normalized, keeping Chinese validation text unchanged. `/api/subscriptions/test` wall clock is narrowly normalized in proddiff.
- A11-2 closed the deferred diagnostics: deploy.Wire registers provider.SetLocations; the isolated C30 fixture installs/restores the same hook. Removed deferA11Locations; complete file/coordinate/excerpt envelopes compare strictly. `.32` CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/subscriptions/difftest/ → ok 99.756s; status, message and all stored effects match.

Commands:
- Iteration: `RT_HOST=sunbroke@192.168.0.63 rt -- bash -lc 'cd server && go vet ./internal/api/nodes/... ./internal/api/subscriptions/... && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/nodes/... ./internal/api/subscriptions/...'`.
- Proddiff: `rt -- server/scripts/proddiff.sh` → `baseline: new=0 stale=0`; no production access, only the read-only snapshot under isolated network namespace.
- Final .32: `rt -- bash -lc 'cd server && gofmt -l internal/ tools/ && go vet ./... && go test -race -count=1 ./internal/api/nodes/... ./internal/api/subscriptions/... && go test -count=1 ./... && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/subscriptions/difftest/ ./internal/health/difftest/'` (executed as one fail-fast shell with logs/tails; exact invocation and output in `parity/C30-HANDOFF.md`).
- Boundary race: `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/subscriptions/difftest/'` → `ok configflow/internal/api/subscriptions/difftest 73.305s`.
- Complete: rebased onto local `go`, no push. Full hand-back, line counts and final output: `parity/C30-HANDOFF.md`.

Cross-module boundary remaining: a chain graph requiring aggregation materialization still needs C31 to install `provider.SetAggregationProvider`; aggregation/main-preview/proxy-group/Agent-push consumers are owned by C31/C34/C33/A14 respectively. This does not add a C30 fallback or bypass final validation.

## C31 — aggregation routes, device notes


A11 proddiff dependency closed by C31: both workers now independently render aggregation providers. The former frozen provider path is removed from the production-snapshot harness, and that Agent's bundle is promoted from packaging-only partial coverage. Structured main/provider YAML compares parsed values, with each manifest size/SHA256 separately checked against its real delivered bytes; all other artifacts stay byte-exact. A11's branch was not modified.

**Status: `verified`** · prereqs: C30, C26 · Go: internal/api/aggregations, internal/aggregations, internal/devicenotes; repo/oxidns adapters

Files:
- `backend/routes/aggregations.py` (617)
- `backend/common/device_notes.py` (105)

Tests: `test_aggregation_reorder.py` (3), `test_dialer_rendered_bundle.py` (4), `test_shared_resource_routes.py` (8)

Routes (11, 11 done):

`GET /api/aggregations`; `POST /api/aggregations`; `DELETE /api/aggregations/<agg_id>`; `GET /api/aggregations/<agg_id>`; `PUT /api/aggregations/<agg_id>`; `GET /api/aggregations/<agg_id>/count`; `GET /api/aggregations/<agg_id>/preview`; `GET /api/aggregations/<agg_id>/provider`; `GET /api/aggregations/<resource_id>/health-impact`; `POST /api/aggregations/reorder`; `GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider`

All 11 route methods registered and native aggregation renderer wired into provider bundles. Shared notes replace repo/oxidns validation copies; existing Go agents/routers have no private note consumers yet. Real Flask↔Go HTTP and real Python generated-provider/notes structural differentials are implemented; final expanded acceptance passed on .64, with final C31 cache differential race on .32. Restart file: [progress/C31-aggregations.md](progress/C31-aggregations.md).

A11 integration: aggregation generator is now registered in proddiff, **2 production provider outputs compared successfully** (`new=0 stale=0`). The A11 Agent's frozen Python aggregation-provider input has now been removed after rebasing onto integrated A11; the A11 branch is untouched. proddiff's former `aggregations` catalog lookup was corrected to `subscription_aggregations` in both HTTP and direct generator selectors.

Python test mapping (all functions in the three requested files):

| Python tests | C31 boundary coverage / other owner |
|---|---|
| `test_aggregation_reorder.py`: by-ids persistence; unknown-id rejection; legacy-object reorder preserving definitions with stale profile | `difftest.TestCatalogAndReorder`, `TestSharedCatalogSelectionAndProtection`; full document and JSON/YAML files compared after every request. |
| `test_dialer_rendered_bundle.py`: provider-only chain rejects duplicates globally; no-chain keeps legacy duplicates; one materialization for valid targets; reserved names/content errors | `TestGlobalRenderedBundle` (both aggregation orderings, chain on/off, duplicate and relay/DIRECT targets), `TestDryGeneratorCorpus` (immutable supplied main, reserved/duplicate names and malformed YAML), `TestOneOpaqueMaterializationForMainAndAggregation`. C34's main preview/config download and A14 Agent push route wrappers stay with those owners; shared C31 provider bundle is now real, not a frozen stub. |
| `test_shared_resource_routes.py`: catalog ignores unrelated profile; referenced delete preserves all profiles; provider output scoped without source mutation | `TestSharedCatalogSelectionAndProtection` (stale selector, cloned Other, 409 delete, other-profile preview), `TestProviderAndPreview` (provider alias), `TestBoundedCollectionDetachesCancellationAndDoesNotMutate` (owned source snapshot), `TestPublishedSnapshotSurvivesArtifactReplacement`; state comparison includes all profiles/files. |
| Same file: library rename/composition; shared dialer fields/profile-local chains; deleting source referenced by shared aggregation; global stats/local composition; cross-profile raw-dialer rename | Route ownership remains C33 rule-library/proxy-group, C30 nodes/subscriptions, C38 stats respectively. C30 already covers source-dependency and cross-profile guards; no duplicate route implementation here. C31 generation side is covered by the real bundle tests above. |

Notes boundary: `devicenotes/difftest.TestDeviceNotesBoundary` drives all stored/read/set/revision/prepare/save public operations with real Python, including invalid and full-capacity data; name-only saves retain identical settings. `FuzzStoredNotes` and `FuzzAggregationDocument` exercise new parsing paths. Native JSON object key order/YAML indentation are not chased; malformed internal exception wording is native. After integration of A11, real `deploy.Locations` is wired in the C31 differential and its location-only deferral is removed; complete Chinese diagnostics and all state now compare strictly.

Final: gofmt/vet, all changed-package race, full ordinary suite (80 tested packages), all 12 enabled difftest packages, both 60s fuzz targets and proddiff pass. New=0 stale=0, aggregation-provider 2/2, independent Agent bundle 3/3 (no partial). No A11 diagnostic deferral remains. No remaining C31 route/feature work. Independent adversarial review completed on `go-review-c31`: one high (owner shutdown cancellation) and one low (Unicode note revision) finding fixed, zero open. Final .32 gofmt/vet, changed-package/provider race, full suite (80 tested packages), all 12 enabled difftests, both 60s fuzz targets and proddiff passed; aggregation 2/2, Agent bundle 3/3, no partial, new=0 stale=0. Findings, exact commands/output and hand-back: [review-c31.md](review-c31.md). Chinese findings, exact commands/output and commits: [C31-HANDOFF.md](C31-HANDOFF.md).

Load-stability follow-up (`go-fix-agg-provider-load`, based on O3-5): old 65c0e6d4 also reproduces stale/fresh response timing differences under a one-CPU limit; no introduced provider semantic regression. Proddiff now waits for completed provider rendering on both sides, while production keeps the same 12-second last-good behavior. Real Python production/fresh-mode header/body regressions, unchanged strict comparator controls, .64 full gate (`-p 4`, 87 race packages, 13 enabled difftests, two 60s fuzz targets) and single-CPU proddiff pass. Concurrent gate proddiff at load 42.70 to 35.94: new=0 stale=0, aggregation 2/2, bundle 3/3, no partial. Full evidence: [fix-agg-provider-load.md](fix-agg-provider-load.md).

## C32 — rule routes

CFG-11/CFG-02 regression follow-up (2026-11-18, go-fix-cfg-c32): C32 now patches only genuinely changed top-level fields, retaining untouched DNS raw bytes/positions/revisions; owner cancellation checks remain in place, including callback exit. SQL regressions locate the first former false write at DELETE and verify no-op404 versus actual reference cleanup. No repo/store change; O3-6c and N8-5b interfaces coordinated. Delivery warnings retain Python count/first10 line positions once per request, memo hits included, with shared credential redaction and unchanged caches. 20 CFG-11 vectors rerun with zero DNS revision differences; CFG-02/03 temporary combined tree59238b255 passes22/22 exact HTTP/log/DB/file vectors. Producer CFG-03 remains owned by go-fix-cfg-c14. Final .64 gate on d43cca6e4 PASS:98 race packages,16 live difftest packages,proddiff new=0/stale=0/self-check PASS; C32 API56.838s/live94.522s,C37 core586.669s/API7.094s/live11.313s. Rebased onto a9d6ce86c:consumer delta gofmt/vet + enabled C32 race/live PASS63.206s/160.419s; then doc-only rebase215ab6b04. Ready for coordinator batch-2 under the new targeted-only agent workflow. Full-gate evidence, approved strict exceptions and separately assigned C33 backup finding: [fix-cfg-c32.md](fix-cfg-c32.md). N8-8/9 must retain only-changed-field semantics when replacing this temporary adapter.

最终 go 更新到 0f943623（provider慢渲染修正），新增第14个 live differential 已追加 race：provider/difftest51.543s；proddiff tag回归0.070s，API1.414s/server0.035s及相关vet均PASS。完整 gate 的13个既有包和此新增包均已执行，不改baseline；报告中区分完整gate内容与追加验证。

**Status: `verified`**（2026-11-18 独立复核完成，0 未决） · prereqs: C09, C14, C27 · Go: internal/api/rules。分支 go-port-c32，最终实现 ccab8632，基于 go 0f943623。无新依赖、不 push，A24 分支未动。

独立复核 `go-review-c32`（起点 c3e4e9ac，非作者）完成，0 未决：F1 高（Runtime 停止后仍可写入；owner/事务/下载/缓存均检查关机取消，普通断开仍完成）、F2 中（batch null 元素被当空字符串；在语法校验前严格拒绝）。补充九类写入取消矩阵、SQLite 提交失败重开验证、30 秒/镜像/422/缓存归属，以及 19 管理方法与两个匿名 provider 的 token/JWT 矩阵。完整记录：[review-c32.md](review-c32.md)。

独立 .64 最终 `gate.sh -p 6 FuzzPastedRules:./internal/api/rules` PASS：全树 gofmt/vet/双 tag、全量 race（91 个包命令）、全部 14 个 live difftest、60 秒 fuzz（369,572 execs）和 proddiff（23 自检 PASS，new=0 stale=0 coverage_regressions=0 unmapped_routes=0）。C32 race49.239s/live diff33.880s；日志 `/tmp/c32-review-integrated-gate`。原版/R08 已以 ported 合入 76f65828 后，仅保留两个独立复核提交；再变基 601a8121 后追加启用差分的 API/model/repo/server race 全过（50.714s/166.861s/20.533s/125.254s/1.759s），最后变基 e980d402（N7 writer）后全树格式/vet、C32全部测试/差分及整应用接线再通过（4.002s/56.612s/0.053s）。完整 gate 与末次增量范围分开记录，未改 baseline，无 push。

Files: backend/routes/rules.py 全部方法及 profiles.py 两类别名。核心使用 native DTO/RawMessage，legacy存储/引擎转换仅在adapter；复用C09 syntax/reorder/lazy matcher、C14 validation/fetch/cache/delivery、refresh、C27 DNS/native routing。共享库+profile+DNS一个事务；owner lifecycle.Detach；缓存失败保留保存。全应用注册无冲突。

Routes (21, 21 done):

`GET /api/profiles/<profile_id>/rules`; `POST /api/profiles/<profile_id>/rules`; `GET /api/profiles/<profile_id>/rules/local/<name>`; `GET /api/rule-sets`; `POST /api/rule-sets`; `DELETE /api/rule-sets/<rule_set_id>`; `PUT /api/rule-sets/<rule_set_id>`; `GET /api/rules`; `POST /api/rules`; `DELETE /api/rules/<rule_id>`; `PUT /api/rules/<rule_id>`; `POST /api/rules/batch`; `POST /api/rules/compose`; `GET /api/rules/dns`; `PUT /api/rules/dns`; `POST /api/rules/find-duplicates`; `POST /api/rules/group-name`; `GET /api/rules/local/<name>`; `POST /api/rules/match-test`; `PUT /api/rules/policy`; `POST /api/rules/reorder`

**原延期 deferred → A16，已由 A16 接线**：go-port-a16 的 Wire 注册 agents.Hooks.PinnedWithheld，随 A16 合并恢复该字段。原降级合同保留：无符合条件的auto-sync OxiDNS确定false；需要projection但hook nil/error，单条脱敏warning、省略字段、不回滚保存。nil/error/true/false/no-device都有测试，不豁免其他字段。PROGRESS-agents A16 已明确合同。

问题已修：中等 C27 legacy Agent 数字/数组名称丢为null → 独立json.RawMessage（数字/数组/null真实Python及字节隔离回归）；中等 Python HEAD DNS意外写入 → 仅PUT写、HEAD只读（真实Python旧写入/Go全状态不变断言及behaviour-changes记录）。C32 reorder保持旧对象数组/异常ID和position的Python合同；11种已接受文本行分隔通过C14 RuleLines适配matcher，不改raw缓存。

A19 gateway和R01 reader均已接线。协调者批准ComputeDesired/RulesetContent首参ctx、ContentLoader形状不变；shared sourceOptions.offline固定cache→inline、零网络。R01缺缓存完整plan/中文blocking warning与真实Python一致；拒绝所有出站的fake断言0次。PROGRESS-routers R01已写C32已接线。合并顺序R08→C32→R09；desired.go只改ctx签名/透传，R08字段全部保留。

验收：[C32-HANDOFF.md](C32-HANDOFF.md)，[21方法及7个Python文件映射](difftest-c32.md)。Python文件5/13/29/24/7/7/8个原始测试函数的归属均有说明；C33/C18/C28/A11之外的路由不复制。每请求比较status/JSON/headers、SQLite sections/fields全行、export及JSON/YAML/list文件。Mihomo YAML与native_config按结构，raw/cache保留字节；仅归一化计时/有效时间及一一配对随机ID派生引用，不改生产baseline。

完整 .64 gate：gofmt、vet、双tag、全量race（88个包测试命令）、全部13个live difftest、proddiff、60秒FuzzPastedRules均PASS。API rules race16.886s、oxidns95.383s、routers442.437s；C32 live diff74.075s；fuzz90,402 execs。proddiff23自检PASS、81GET/82method-path、693实际比较（planned cases2831）、aggregation-provider2/2、agent-deployment-bundle3/3，new=0 stale=0 coverage_regressions=0；baseline未改。提交gate load18.06/15.16/16.48，结束25.59/24.32/21.56。

R08 integration follow-up: rebased `go-port-c32` onto `go=d8f50b00`; preserved every R08 desired-state field. The only newly stale production call was `InspectROSPolicyDesired` → `ComputeDesired`; it now passes the existing caller ctx. `TestROSPolicyDesiredPassesContextToDefaultContentLoader` checks the exact context, request value and deadline reach the default source loader. Repository-wide `rg` of both ComputeDesired and RulesetContent found no other stale callers. .64 gofmt/vet + `CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/routers/... ./internal/routersplit/... ./internal/api/rules/...` PASS: routers404.956s (includes R04/R06/R07/R08 live Python boundaries), routersplit13.863s, API23.183s, API difftest113.050s. Load start22.83/13.33/11.87; end11.41/13.64/12.76. No full gate repeated here (coordinator owns it).

最后R01补丁按协调者裁定只追加相关检查，.32 gofmt/vet + CONFIGFLOW_DIFF=1相关全包race均PASS：API37.281s/diff172.626s/routers399.537s/routersplit27.638s/server1.480s/gateway29.774s；load开始21.67/23.75/25.79、结束3.08/12.39/20.44。完整gate内容、最后最小补丁及0f943623增量验证的范围/命令见交回报告。没有其他待移植的C32功能。

## C33 — rule library and proxy group routes

**Status: `verified` — C33范围无未结发现；最终gate排队超15分钟，交协调者执行** · prereq: C32 · Go: `internal/api/rulelibrary`、`internal/api/proxygroups`

25/25 method/path 已实现且均为 done，无重复键；逐方法成功/失败、落盘比较和 Python 测试归属见 [difftest-c33.md](difftest-c33.md)。作者交回为 `go-port-c33@a4445071`；历史移植及旧 gate 记录见 [C33-HANDOFF.md](C33-HANDOFF.md)，不能代替本轮验收。

Python：完整两路由 `backend/routes/rule_library.py`（540行）、`proxy_groups.py`（436行）；三项主测试（batch health / regex exclusion / create）及 policy editor 内容半边、DNS follow、scheduled rule source/cache 相关行为均有边界映射。RouterOS editor 和 MCP 自有入口仍属各自 owner，无新增路由或 UI 操作入口。

独立复核分支 `go-review-c33`（复核者不是作者）。已从895f基底依次 rebase 到 A13、L1、C29（含复核）及最新本地 `go@f90a156eb`（含R09及O3-6a）。合并保留 C29 的 server 注册、18条 done 路由和共享 reader。完整结论见 [review-c33.md](review-c33.md)。

8项发现均已修：4 high（失效标记权限错误复活 legacy；旧 inline writer 越过缓存提交协议；发布/撤销未与其他owner的DB提交串行化；O3-6a合并后回调丢失owner取消），2 medium（reorder 真值非对象404/500边界；严格输入的非有限 JSON 内部500），2 low（live D2 接受大写hex；预览来源选择默认/混合数组与非数组验证）。前四个修复提交 `c0e64a248`、`479e947c4`、`17735a0fa`、`30e25285e`；协议串行化提交 `23d2eff82`；回调恢复owner context的合并修复 `c5efcc5ca`。

缓存：暂存/Sync → 持久 marker → DB → L3内核对来源声明并rename/目录Sync/清marker；marker只有明确不存在才允许回退。撤销也在L3核对原baseline后才清新marker，原有marker不清。Create在下载后、实际提交前加入同ID编辑/清理lease，保留迟到下载竞争语义。共享 rulefetch inline writer 在repo锁内核对当前声明/字节，过期调用不发布；文件副作用后以内部sentinel中止未变DB候选，不增加API/revision/hook。C32 cold cache、cache→inline优先级与零网络断言保留，无新增后台恢复。

真实 SIGKILL/reopen 覆盖 staged/committed/renamed/complete 四点及手动修复；真实 Runtime 测试继续覆盖15类 owner（关机拒写、请求断开完成）。共享 taskstatus/repo/rulefetch 的**最终关机取消提交/锁等待窗口**仍归 `O3-6c / 6e5c7760`，不是 C33 发现；声明一致性守卫不代表那项已完成。O3-6c尚未在验收基底f90a156eb中；后合入方需把C33 adapter.forget改为ForgetContext(owner ctx, key)，Record经refresh.recordStatus切换；并补真实Runtime关机后taskstatus不变回归，见review-c33衔接段。

合并 C29 后 .32 全仓格式/vet、定向 oxidns reader race42.127s，C33 API111.073s/live140.268s、代理14.034s、C32 API120.945s/live254.575s、C29 API4.038s/live83.430s、rules22.663s/rulefetch54.203s、C28live19.904s/C11live25.970s/server2.037s全部通过。新 Bind 后 .64 C33/C29/server race+live通过；最终预览修复后 .32 C33 API92.565s/live144.693s/代理11.583s/server2.832s全绿。

协议最终增量：.32 C33 API120.722s/live191.633s、代理14.051s、C32 API127.732s/live325.488s、rulefetch57.092s、server2.774s全部通过；发布/撤销/独立metadata/真实创建lease及慢GET与inline赢家控制的补跑2.928s通过。C37/A24自己DB-first修改来源的崩溃窗口另需协调者分派，不应与O3-6c取消任务混同；本轮只声明C33协议完成，详见报告。

最终冻结树c5efcc5ca（f90a156eb基底）：C33 API80.471s/live101.195s、代理7.290s、C32 API80.216s/live238.734s、C29 API2.151s/live40.952s、rulefetch28.623s、server2.050s均通过race/live，所有原用例及新增控制启用；F8真实回调关机两包2.818s/1.922s、缓存mtime连续3轮live8.588s通过。

60秒独立FuzzEditableContent PASS（39412次）；最终冻结树全仓gofmt/vet通过，proddiff harness direct/discover各42测试 PASS。旧树门禁均已取消，不计完整通过；最终树.64 gate在06:01:39入队，06:16:53仍无slot，15分14秒后按约定终止自身进程组，退出143，未开始任何检查。完整门禁由协调者在合并树运行cf-gate；精确命令、日志位置、已完成定向结果见报告。只允许读测试服务器离线快照，报告只记录new/stale/self-check；不接触生产，不复制快照内容，不push、不使用子agent。

备份合同衔接（2026-11-18）：协调者在C33接通后的configdiff T25发现PUT/DELETE缺少Python保留的旧主缓存 `.list.bak`。新分支 `go-fix-c33-bak` 从go@d43cca6e4起步，最终变基到go@a9d6ce86c（c31源码及一条仅WORKFLOW的门禁说明）；修复代码0ddafc602→a0bbc3d6a（C33 Go代码未再修改）：L3声明守卫通过后捕获旧主字节，主rename后以0600 temp尽力刷新备份，备份失败不失败主写；最后一次owner取消检查在主rename前，成功后本地收尾不可取消。该备份没有自动恢复读取方，但实际参与unchanged-cache和孤儿ID保护，协调者已明确要求保留。O3-6c已收到Paseo文件修改通知；不改它的repo/taskstatus/rulefetch文件。

新增备份/权限/空旧内容、unchanged-cache、孤儿ID、备份失败、主rename后Runtime关机回归；F7拒绝验证备份字节/inode/mtime不变；四点真实SIGKILL/reopen也核对备份和恢复后轮转。定向race9.198s通过。T25两个目标library/operation-2及operation-3均200/compared/diffs0；整组仍退出1（另有rules/operation-5的MosDNS revision1/10差异，preview未实现），不能称整组通过。没有改比较器或复制其他CFG票。变基前.32全仓格式/vet及C33/C32/rulefetch/rules/server race/live均通过；最终c31基底复验格式/vet、AST门1.117s、C33 API92.060s/live145.000s、rules19.379s、server2.451s通过，T25两目标再次compared/diffs0，整组仍保留上述其他差异。最终.64 gate于08:15:16排队，08:30:21仍无slot，按15分钟上限核对自身PID/PGID/cwd后终止，退出143，未开始检查、不计完整通过；协调者按WORKFLOW在批次合并树运行cf-gate。完整命令及结果见 [review-c33-bak.md](review-c33-bak.md)。

## C34 — generate, config and profile routes, config export

**独立复核（2026-11-18，修复 `da3883a33`）**：2 high / 2 medium 全部修复，56个常规方法通过；3个artifact保持已约定503/wip。逐引擎null/空body裁定、凭据出口、dry_run及请求默认修复，59方法认证与18个外部别名分派证据见 [review-c34.md](review-c34.md)。A14/Previews/O3-6c外部分工不变；只跑定向race/vet/真实Flask，无完整gate。

**Status: `partial`** · prereqs: C18, C19, C20, C28, C29, C31, C33 · Go: internal/api/generate, api/config, api/profiles, internal/configexport (new; internal/redact exists)


**Clarified in C34 (2026-11-18):** HEAD uses the GET read-only branch for profile collections/items and all custom-config routes: no DB, business-file, revision or generation-cache changes. These fix Python dispatch bugs and have explicit expected differences. Artifact routes are registered before A14 integration and return 503 「等待 Agent 发布模块」; unknown services remain 400 and unknown actions 404. Frozen-artifact success coverage awaits A14, without copying its implementation. `repo.Hooks.SanitizeExternalPayload` gains a first `context.Context` parameter; desensitized ExportAll reads credentials from the same repository snapshot as the exported document. N8-5b was notified; N8-5c retains this contract. A11 Previews gets a separate shared-leaf-lock/shutdown fix **after O3-6c merges**, with real Runtime reproductions; the A14 reviewer has been notified. Commit sequence: export wiring, profiles, custom config, generation/live config, artifact integration. Shared commit points remain known and assigned to O3-6c; final gate queue limit is 15 minutes.

Files:
- `backend/routes/generate.py` (320)
- `backend/routes/config.py` (497)
- `backend/routes/profiles.py` (336)
- `backend/common/config_export.py` (290)

Tests: `test_agent_log_redaction.py` (2), `test_flow_ui_apis.py` (9), `test_profile_api.py` (19), `test_profile_generation.py` (10), `test_profile_scoped_artifacts.py` (16), `test_sensitive_config_exports.py` (25)

Routes (59, 56 done, 3 registered wip pending A14):

`GET /api/config/<profile_id>/loon`; `GET /api/config/<profile_id>/loon/<filename>`; `GET /api/config/<profile_id>/mihomo`; `GET /api/config/<profile_id>/mosdns`; `GET /api/config/<profile_id>/oxidns`; `GET /api/config/<profile_id>/surge`; `GET /api/config/export`; `POST /api/config/import`; `GET /api/config/loon`; `GET /api/config/mihomo`; `GET /api/config/mosdns`; `GET /api/config/oxidns`; `POST /api/config/reset`; `GET /api/config/surge`; `GET /api/custom-config/loon`; `POST /api/custom-config/loon`; `GET /api/custom-config/mihomo`; `POST /api/custom-config/mihomo`; `GET /api/custom-config/mosdns`; `POST /api/custom-config/mosdns`; `GET /api/custom-config/oxidns`; `POST /api/custom-config/oxidns`; `GET /api/custom-config/surge`; `POST /api/custom-config/surge`; `POST /api/generate/<service>/artifact`; `GET /api/generate/artifacts/<preview_id>/<action>`; `POST /api/generate/loon`; `POST /api/generate/loon/preview`; `POST /api/generate/mihomo`; `POST /api/generate/mihomo/preview`; `POST /api/generate/mosdns`; `POST /api/generate/mosdns/preview`; `POST /api/generate/oxidns`; `POST /api/generate/oxidns/preview`; `POST /api/generate/surge`; `POST /api/generate/surge/preview`; `GET /api/profiles`; `POST /api/profiles`; `DELETE /api/profiles/<profile_id>`; `GET /api/profiles/<profile_id>`; `PUT /api/profiles/<profile_id>`; `GET /api/profiles/<profile_id>/agents`; `POST /api/profiles/<profile_id>/agents/rebind`; `POST /api/profiles/<profile_id>/clone`; `POST /api/profiles/<profile_id>/default`; `GET /api/profiles/<profile_id>/export`; `POST /api/profiles/<profile_id>/generate/<service>/artifact`; `POST /api/profiles/<profile_id>/generate/loon`; `POST /api/profiles/<profile_id>/generate/loon/preview`; `POST /api/profiles/<profile_id>/generate/mihomo`; `POST /api/profiles/<profile_id>/generate/mihomo/preview`; `POST /api/profiles/<profile_id>/generate/mosdns`; `POST /api/profiles/<profile_id>/generate/mosdns/preview`; `POST /api/profiles/<profile_id>/generate/oxidns`; `POST /api/profiles/<profile_id>/generate/oxidns/preview`; `POST /api/profiles/<profile_id>/generate/surge`; `POST /api/profiles/<profile_id>/generate/surge/preview`; `POST /api/profiles/<profile_id>/import`; `POST /api/profiles/import-client`

Evidence: all 59 methods registered; HANDOFF 3c ownership/precedence and authentication matrix cover the complete app. Implementation, source mapping and validation: [progress/c34.md](progress/c34.md).

Missing / deferred: A14 frozen artifact success wiring (three explicit 503 routes); A11 shared Previews shutdown/keyed-lock fix waits for O3-6c merge notification. C34 ordinary endpoints implemented.

Notes for porter: Own routes: `routes/config.py` (24), `routes/generate.py` (12), `routes/profiles.py` CRUD/import/export/agents/generate wrappers (23). `routes/generate.py` calls `agents.deployment_bundle._pack` and `routes.agents.download_final_agent_config/read_final_agent_file` (agents domain) for artifacts; `routes/profiles.py` uses `agents.manager.AgentDeploymentConflict` and the agent manager. `config_export` → `internal/redact` already has the response-redaction part; this module adds `sanitize_config_for_output`, `redact_oxidns_settings` and registers the repo redaction hooks.

C35 supplies native `importer.Parse/Build` for `/api/profiles/import-client` (API reference in PORTING.md). Run `test_config_import.py::test_import_route_dry_run_and_commit` at the HTTP boundary when C34 lands. C35 already compares all four client parse/build outputs and real repository commit/rollback; the route itself remains C34's responsibility.


**Clarified in C34 JSON shape follow-up:** the five custom-config routes reject non-null non-object JSON with 400/no writes. Null/missing body follows each real Flask engine: Surge/Loon JSON null is an empty update (200, unchanged fields, revision+1); Mihomo/MosDNS JSON null is 500/no writes; empty bodies with or without JSON media are 500/no writes for all four engines; OxiDNS null/empty is 400/no writes. Explicit {} separately clears Mihomo/MosDNS custom_config (200, revision+1), leaves Surge/Loon fields unchanged (200, revision+1), and is 400/no writes for OxiDNS. This supersedes the author's unified null/missing update decision, per the maintainer’s review clarification. Ordinary object values stay with existing business validators. Explicit Python-200/no-field and Python-500 shape classes are separately recorded in behaviour-changes and asserted by real Flask tests. No Python container-membership emulation.


**C34 hand-back (2026-11-18, go-port-c34 rebased on go@215ab6b04):** 56 implemented methods + 3 registered artifact503 methods. Final .64 targeted API/configgen/configexport/server race, real Flask DB/files differential, full vet and clean formatting PASS; shared DNS-loop direct Python corpus509 + targeted race PASS; fuzz60s227,427 executions PASS. Repo race PASS233s. Entire gateway race hit default10m in the pre-existing N7 frozen-history archive decoder; targeted C34 gateway checks pass, the broader timeout remains explicit. Per the latest coordinator instruction, no agent full gate; batch gate belongs to the coordinator. Previews keyed-lock/shutdown delta is deliberately the next step after O3-6c notification. Detailed source/test mapping, exact commands, output and commit list: [progress/c34.md](progress/c34.md).

**Clarified in C34 base_url:** missing/null means no override; strings including empty stay unchanged; other field types return400 with no DB/file/cache commit. This fixes invalid Python string-coerced URL prefixes and OxiDNS500; other fields and profile/generate empty-body defaults are unchanged. Explicit Flask expectations are in TestC34BaseURLInputAlternatives.

## C35 — config importer, WebDAV backup

**Status: `verified`** · prereqs: C04, C06 · Go: internal/importer, internal/webdav, internal/api/settings

Files: `backend/utils/config_importer.py` (1148), `backend/utils/webdav_backup.py` (227); backup handlers from `backend/routes/settings.py`.

Routes: `POST /api/backup/now`, `POST /api/backup/test` [done; C06 accounting now 22/22]. No new GET routes; existing production coverage file needs no additions.

Independent adversarial review complete (`go-review-c35`, 2026-11-17), no open C35 finding. Fixes: URL-userinfo/encoded credential leakage; blank/masked password preservation per coordinator; valid manual request no longer blocked by ignored malformed saved settings; explicit empty WebSocket Host; quadratic warning/section scans; duplicate-slash paths; native per-operation DAV cookies. Coordinator-approved dot-path rejection covers save/test/manual/automatic before network with a clear task error. Security decisions and migration advice: `parity/behaviour-changes.md`; severity-ranked findings, branch matrix and command outputs: [review-c35.md](review-c35.md). Review commits: `6ca39f71`, `762cdc56`, `801ed02e`.

Review acceptance: .63 importer 97 Python structural cases, deep/alias/32 MiB resource probes (~108 MiB normal peak RSS), repository path isolation, 60s fuzz (1,201,454 executions) pass. Agent non-runtime changes exercise real repo→worker→loopback DAV; TLS compared with real Python, weak references prove completed snapshot release. Final .32: gofmt/vet silent; scoped importer/webdav/settings/server race passes (importer 26.674s); full tree all 64 tested packages pass; enabled real Flask DAV race differential 39.009s, settings 8.592s, httpx differential 1.845s. Proddiff: 65 GET routes, 2831 cases, self-check PASS, coverage_regressions=0, new=0, stale=0 (10 existing differences); no baseline/coverage changes. Rebased on go `5535931d`; no push. C34 retains import HTTP integration; O3-5 retains the existing lifecycle migration.

Follow-up `go-fix-importer-bounds` complete: child RLIMIT_CPU bounds actual CPU (3s normal/30s race), wall fallback 30s/5min; all input sizes and RSS/stack/soft-heap checks unchanged. Infinite-compute child verifies the CPU guard. Combined with the S-oxidns race budget fix, .32 two consecutive `go test -race -count=1 ./...` runs with default -p/default timeout pass all 68 packages; importer 33.977s/40.717s, oxidns 274.075s/283.762s under active loads 21.45–40.57/19.73–46.70. Final proddiff new=0/stale=0. Source commits 12ac1095, 39d90065, fdb2a652; full evidence in review-c35.md follow-up.

Implementation:
- Pure typed importer `Parse → Plan → Build`: Mihomo/Clash, Surge, Loon, Shadowrocket; resource reuse/collisions, protocol/TLS/transport/WireGuard/ShadowTLS options, providers, group/rule ordering, warnings and custom settings. Protocol extras use json.RawMessage; free-form settings use yaml.Node. Native alias/merge expansion avoids dangling anchors after generated sections are removed. Node signatures retain JSON numbers, avoiding false reuse of adjacent integers above 2^53 (two dedicated Python-comparison corpus cases). Existing shared data is never rewritten by Build.
- WebDAV uses net/http + encoding/xml; no dependency added. Full exports are encoded in memory, eliminating temporary-file cleanup paths. HEAD/MKCOL/PUT/PROPFIND/DELETE, recursive directories, timestamped filenames and latest+daily retention are implemented. Cleanup failures preserve successful uploads.
- `webdav.Wire` installs `repo.Hooks.ConfigurationCommitted`. One lazy worker, one pending repository reference, one debounce deadline (1s), fresh snapshot outside repository locks, serialized uploads. Slow uploads do not block saves; intervening commits coalesce. Disable before snapshot cancels pending upload; failure never retries without a new commit or rolls back saved data. Shutdown drops pending work and waits for current timed I/O. Request/wait cancellation does not cancel committed backup I/O (PORTING 4b).
- Manual/automatic task receipts and failure notifications retain the originating repository. Connection/password handling is confined to request construction; safe errors omit URL, username, password and remote body. Directory paths in diagnostics/logs redact known credentials. Existing backup/config response contract (including masked password) is preserved. Native default transport refuses cross-origin redirects.

Boundary coverage and Python test mapping:
- `test_config_import.py` (7): `importer.TestSyntheticPythonImportCorpus` compares Python's public parse/build results structurally across **92 synthetic cases**, including four original client fixtures, all supported protocols, invalid formats, reuse/collisions, YAML aliases/merges and custom settings. `TestNativeImportsCommitThroughRepositoryAndPreserveExistingFields` commits all four clients through C04, verifies unknown stored fields survive, and verifies complete rollback on invalid imports. The HTTP dry-run/commit case `test_import_route_dry_run_and_commit` remains **deferred → C34**, owner of `/api/profiles/import-client`; no import HTTP route is in C35 scope.
- `test_snell_shadow_tls.py` deferred C19 import cases: three synthetic corpus rows plus `TestSnellShadowTLSRoundTripThroughSurgeExporter` through the real Go exporter; closed.
- `test_webdav_auto_backup.py` (24 definitions): `webdav/difftest` invokes real Flask and Go HTTP boundaries against separate authenticated loopback DAV fakes. Config/test/now successes and validation/JWT/remote failures compare status, complete JSON body, full exported storage, JSON sidecars (receipts/notifications) and remote directories/files/upload counts after every request. Includes masked credentials, partial config, recursive mkdir, retention/default fallback, post-commit auto success/failure, coalescing and pending disable. Unit tests cover boundary-invisible timing: slow upload, disabled/read/failed commits, agent runtime suppression vs add/edit/delete/config, snapshot failure/no retry/restart, lock release and shutdown/wait cancellation. Retention helper cases cover same-second ordering, daily windows, zero retention and foreign names. CPython thread-start mocking has no Go equivalent; native lazy-goroutine lifecycle/restart is tested. Serialization failures are rejected before PUT; no temporary file exists to leak.
- Normalizations are limited to independent fake URLs, nondeterministic receipt clocks/durations, Python exception-class labels around retained HTTP codes, YAML formatting, and one malformed boolean node-name warning (`False` → native `false`). Chinese message prefixes, status/success, all data/side effects are compared. yaml.v3 parser diagnostics and duplicate-key rejection replace PyYAML internal syntax diagnostics; no exception/stdlib emulation was added.

Scoped validation on .63: importer vet/race `ok configflow/internal/importer 1.659s`; webdav vet/race `ok configflow/internal/webdav 1.721s`; `CONFIGFLOW_DIFF=1` race differential `ok configflow/internal/webdav/difftest 13.946s`; settings scoped race previously passed (5.263s). Early .32 proddiff: GET routes 65, cases 2060, `coverage_regressions=0`, `new=0`, `stale=0` (5 existing unique differences).

Commits: `ed13691f` core/worker; `45a044f8` typed importer; `decafb67` production hook/routes; `cb09668e` aliases/storage; `90866647` Snell; `cb0c641e` DAV differential/lifecycle; `531b88b2` number precision; `632bb763` full-app route expectation; `3c946518` YAML error-prefix assertion. No pending implementation in C35. C34 owns the deferred import HTTP integration.

Final acceptance on .32 (all exit 0): `gofmt -l internal/ tools/` and `go vet ./...` have no output; scoped `go test -race -count=1` importer/webdav/settings/server passes (latest importer after test-strengthening: 1.416s). `go test -count=1 ./...` passes (installer 141.490s; server 0.015s; webdav 0.225s). `CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/webdav/difftest/` → 14.075s; existing httpx/settings differential → 0.876s. `server/scripts/proddiff.sh`: 65 GET routes, 2060 cases, all self-checks PASS, `coverage_regressions=0`, `new=0`, `stale=0`, 5 existing baseline differences. No coverage/baseline changes needed. The first full run exposed stale “backup endpoints must be absent” assertions in server_test.go; these are fixed and the full rerun passes.

Exact commands/tails, native differences and added-file counts: [C35-HANDOFF.md](C35-HANDOFF.md). Rebased on local go; no push; final documentation commit closes the clean worktree.

## C36 — setup flow

A20/A14 已提供共享只读 reader：`internal/setupflow.ProfileSessions(ctx, dataDir, profileID)`（首版来自 `go-port-a14`，A20 复用并补强类型错误的 fail-closed 中文提示，服务器 `setupflow.Wire` 注入 routersplit）。C36 在此基础上实现写入与流程，不另建第二套 reader。会话是 `setup-sessions/*.json` 的原子替换文件；读取在 repo 写事务外，绑定 Agent/配置取 repo 一致快照。

**Status: `verified` (independent Codex review, 2026-11-18; C36-owned scope)** · prereqs: C34, C35 · Go: `internal/setup`, `internal/api/setup`

Sources: `setup_flow.py`, `setup_presets.py`, `setup_completion.py`, `setup_abandon.py`,
`routes/setup.py` (1372 lines); Python `test_setup_flow.py` 76 families mapped in
[交接](progress/C36-handoff-zh.md). Five methods registered (5/5); implicit HEAD is read-only.
Native session records, ownership adoption, presets, prepare/base, check/finish,
router draft checkpoints, default summary and abandon implemented and tested.

**Clarified in C36:** Native external owner callbacks are `setup.ExternalOwners`.
Unregistered required callbacks fail closed503「等待首次使用向导模块」; no substitute
installation/router state. C32 public `ReadRulesetContent` is a thin sourceContent
entry, wired from server; existing defaults/signatures remain unchanged. A21 last
merger wires `onboarding.SetAdoptOwnedWrite(setup.AdoptOwnedWrite)` and the DNSOwner
callbacks directly to LocalUpstreams/PrepareMihomoDNS/defaults and C34 shared DNSLoop.
A21 owns the real revision+1 precondition test; C36 owns full/without-base digests and
session operation serialization. `lockx.LevelSetupSession = LevelDevice-1` orders
setup session → device/profile operation → repo (abandon retains the session lock
through both deletions). A20 ProfileSessions remains a read-only leaf; no copied files.

A20 reader integration is complete: setupflow.ReadSession is the single bounded byte
reader used by both ProfileSessions and C36, with no session lock in the read-only leaf.
Rebased onto go@fbbe382ec; targeted .32 vet/race/Flask checks pass.
Deferred external integration: real A21 server hook/DNS registration and real A22 latest/has-installation/
gateway status; real router Driver/Inspect/BuildPlan callback. Formal A21 server test
must prove registered callbacks remove503. Current tests prove callback dispatch,
independent orchestration and genuine Python boundary parity with synthetic data.
See [进度与精确命令](progress/c36.md) and [接线与验证映射](progress/C36-handoff-zh.md).

Independent review: [review-c36.md](review-c36.md). High 0, medium 1 (fixed), low 0;
no open C36-owned findings. Complete responses and public failures now retain private-session
credential seeds through final redaction; usernames/password-presence flags, stored credentials,
HTTP statuses and owner error chains are preserved. Added explicit revision+1/idempotence/digest,
post-external-read Runtime shutdown and exact503/zero-write regressions; targeted .32 race,
normal/error Flask boundary, reader/lock/C32 timeout checks and vet pass. Approved external owner
registration and real A21 embedded-save precondition integration remain deferred as listed above.
Fixes: `1fe46a78f`, `3defa2895`; exact commands and fail-before/pass-after evidence are in the review.

## C37 — domain discovery

CFG-06 follow-up (2026-11-18): InitRulesets now deduplicates targets within each call, preserving first occurrence order, before any allocation/write. One library/reference/cache per selected target; no orphan allocations or empty backups. Invalid targets still fail before writes; empty targets still mean direct+proxy. Across separate calls it deliberately keeps Python's new allocations and name suffixes. Future decision (not part of this fix): consider reusing an existing binding on repeated calls; define validity for renamed/deleted libraries and manually edited references first. The author favors that reuse once its validity/recovery contract is decided. Explicit real-Flask difference assertions: TestCFG06DuplicateTargetDifference; external HTTP ordering/cache/repeat/invalid-input regression: TestInitTargetsDeduplicateInFirstOccurrenceOrder.

A24 independent review (`c106af0d`, [report](review-a24.md)) confirms QueryRuleset reuse and BeforeWrite rejection with no repository/cache/history writes, and guard-pass equivalence. `.64` queued full gate passed (12 difftest, proddiff new=0/stale=0); after O3-5 rebase onto `go@9a208c6c`, discovery and enabled difftest race passed again (11.131s / 11.133s), with A24 binding/default/revision and shutdown regressions passing. No C37 runtime change or open finding was added by the review.

**Clarified in A24:** `discovery.ApplyOptions.BeforeWrite` provides Python's `before_write` guard inside the shared transaction, before library mutation. An error aborts the transaction and prevents cache/history writes. Its transaction context supports fresh binding/default reads. `discovery.QueryRuleset(json.RawMessage, id)` returns the existing concrete `*RulesetInfo` for a supplied profile snapshot, reusing C37 reference/eligibility logic. Existing ApplyDomains callers remain compatible. A24's entry point uses `lifecycle.Detach`. `TestApplyDomainsTransactionGuard` verifies rejection leaves the repository/caches unchanged and acceptance commits. .64: `go test -count=1 ./internal/api/agents ./internal/discovery` PASS (5.676s / 0.680s). Follow-up: the two remaining adapter WithoutCancel calls (profile history and rule cache) also use lifecycle.Detach, retaining the O3 shutdown cancellation parent. Guard-pass/no-option equivalence and unrelated-settings query regressions pass; final delta race: discovery 10.329s, enabled difftest 9.822s (.64). Final A24 gate: C37 race 9.062s / enabled difftest 8.663s; rollback proof checks complete cache contents, paths and mtimes (focused race 1.581s); all full/diff/proddiff gates green on .64. Final N7-3b integration on go d3124353: C37 race 8.590s / enabled difftest 8.126s; complete gate and proddiff new=0 stale=0 PASS (.64), including byte/path/mtime rollback assertions.

**Status: `verified`** (independent review 2026-11-17; 0 open findings) · prereqs: C14, C16 · Go: `internal/discovery`, `internal/api/domaindiscovery`

Scope: all of `backend/utils/domain_discovery.py` (175), `domain_discovery_service.py` (1309), `routes/domain_discovery.py` (174). All **17/17 routes registered** and routes.tsv marked done, including settings, domains/status, apply/undo/history/ignore, probe polling, region checks/routing, website identification/notes. Server changes are imports plus routePackages/wirePackages only.

Implementation: native typed settings, library/reference metadata, traffic aggregation, history, Agent protocol and jobs. Preserves public messages, defaults, rule ordering, cache-file provider digests, domain/classical/YAML payload handling, dedupe, shadow warnings, policy rulesets, website candidate ordering, region batching, automatic probing/adoption/rechecks and task receipts. Reuses C14 rule content/cache, C16 evaluation/stores, A01 Manager transport/version and A05 traffic/status storage. No new dependencies or Python exception emulation. Removed the old A05 discovery emulation helpers (~290 lines). `bridge.go` and `taskbridge.go` are explicit adapters to the **existing jsonx-only repo/matcher/health/tasks APIs**; native business code does not import jsonx/pystr/urlx. They must disappear with those APIs' N8 conversion. No private copy of used-port validation: calls convert.MihomoUsedPorts.

Memory model:
- No process-wide traffic/report/summary/probe-result cache. Traffic and merged probes live only for a request/round; a selected Agent loads only its traffic, while other statuses use A05's bounded 128-entry status cache.
- One process-wide C08 MatcherCache, max **4** matchers, **1024** host results each (Python: 10000), idle eviction **300s** (Python: 1800s), validates URL cache content through C14, clears at shutdown. Index memory scales with the referenced rules in those four slots; there is no fixed byte claim for arbitrarily large rule libraries.
- At most **21** metadata-only jobs (20 previous finished + current/latest); bounded input/batch sizes. Results are persisted in C16 stores, returned directly only to synchronous auto rechecks, never saved in job history. Workers retain a repository handle and a fresh Scope, not the request context/Scope. Weak-pointer/GC tests prove request values and completed result buffers are released.

Lifecycle/O3: lifecycle.Loop(domain-discovery-auto), existing tasks providers/StartBackground for run-now, same probes/.auto.lock across processes, cancellable waits (120s first delay, 600s interval), fresh/reset Scope per profile. HTTP mutations detach cancellation before any reads/writes; probe jobs also start from a fresh repository-only context. Existing Manager I/O timeouts bound device calls. Exclusivity/job polling remain process-local, while the auto scheduler has flock, matching Python. Manual workers are not newly added to a global shutdown/drain framework; that policy remains O3's shared responsibility.

Boundary evidence (.63):
- `server/scripts/proddiff.sh`: all six GET templates in C37 compare with **zero C37 differences**; missing probe job uses framework's synthetic missing parameter (no custom parameter patch needed). The .63 whole-run gate reported Agent metrics/history window drift outside C37; the required .32 final run passed with new=0 (no baseline changes). Never changed the baseline or accessed live production/devices.
- `internal/discovery/difftest/TestHTTPAndStorageParity`: **65** sequential synthetic HTTP requests + six missing-profile GET cases; real Flask and Go on copied SQLite directories, same loopback fake Agent. Every write compares stored rule_library, rule_configs, domain_discovery, cache contents and per-Agent probes/regions. Every Agent request payload is compared; generated IDs are mapped by persisted library name/reference position so relationships and cross-target isolation remain checked. Covers all 17 routes with success/failure, async completion/user failure, partial website batches, domain/classical dedupe, Chinese validation, unknown profile, shadow warnings, region results, history/evidence/undo. Generic malformed-object 500 pages retained using plain Go errors.
- `api/domaindiscovery/routes_test.go`: all 17 routes reject anonymous callers; cancelled HTTP mutations still commit; unknown settings/history fields survive.
- `discovery/lifecycle_test.go`: fresh worker Scope, request cancellation, cross-profile exclusivity/privacy, status progress/errors/failure/panic, immutable metadata, 21-job bound, weak-pointer release, auto disabled/skipped receipts, passive-failure adoption threshold, recent-probe skip, 30-day recheck and no-result recheck recorded only once.

Python coverage map (no wholesale unit-test translation):
- `test_domain_discovery.py` (27): clean-report/store/agent report/token/switch/reregistration/priority behaviours already belong to A05/A01/C08 (their existing tests retained); public suffix test retained here. Group/tag/day/format/init/apply/settings/ignore/selected-Agent/status behaviours covered by the HTTP differential and production GET comparison. No duplicate copies of A05 tests added. Status-only loading still calls LoadStatus rather than Load or matcher; selected history loading is explicit in reports().
- `test_domain_background_lifecycle.py` (5 functions, parametrized): synchronous result/non-retention, async exclusivity, failure cleanup → lifecycle tests above (unobservable GC/Scope ownership needs hand-written tests); automatic per-profile cleanup → fresh per-profile scopes + defer ResetConfigContext, and auto round tests; application startup/request cleanup stays covered by existing repo/httpx/server tests (not reimplemented).
- C16/C14 deferred `test_domain_probe.py` 11 orchestration functions → HTTP Agent protocol/storage differential + automatic round/lifecycle tests; pure judge/store/URL/suggestion functions stay C16-covered. `test_region_check.py` six C37 cases → settings validation, region checks/matrix/routing/shadow warnings in differential; device config injection remains C18. `test_website_identity.py` jobs/candidates/notes/profile isolation → website differential (partial/missing/other profile), known-service/page parsing/store bounds stay C14/C16.

Differences: JSON whitespace/native errors follow functional parity. Mixed-target added/skipped/history ordering follows first target occurrence instead of Python's randomized set order; differential sorts only those value+target collections. Stable traffic ranking/port ties preserve source object order via encoding/json.Decoder (GET proddiff passes without normalization). Go also preserves unknown discovery settings/history fields which Python read_settings/history updates could discard (data-loss fix). No deliberate feature omission.

Commits: 22e5c9b4 (native operations/jobs), 99d97932 (17 routes, automatic loop/tasks), b541f5b2 (boundary/GC fixes), bd7c856a (final validation and relationship-preserving differential IDs); rebased on go 2487dac3. All .32 checks below passed; final documentation/test-identity commit completes the hand-back.

Commands:
- `.63`: `RT_HOST=sunbroke@192.168.0.63 rt -- bash -lc 'cd server && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/discovery/... ./internal/api/domaindiscovery/'` — all pass.
- `.63` GET regression: `RT_HOST=sunbroke@192.168.0.63 rt -- server/scripts/proddiff.sh` — C37 zero differences; unrelated metrics window drift described above.
- `.32` final: `rt -- bash -lc 'cd server && gofmt -l internal/ tools/ && go vet ./... && go test -race -count=1 -timeout=30m ./internal/discovery/ ./internal/api/... && go test -count=1 ./... && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/discovery/difftest/'` — exit 0. gofmt/vet silent; race tail: discovery 2.197s, api/domaindiscovery 1.634s, api/tasks 1.613s (all API packages pass); full-suite tail: weburl 0.034s, yamlx 0.655s, tools/proddiff/generator no test files; enabled Flask differential 1.461s.
- `.32` final ID-relationship assertion: `rt --back server/internal/discovery -- bash -lc 'cd server && gofmt -w internal/discovery/ && gofmt -l internal/ tools/ && go vet ./... && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/discovery/difftest/ | tail -8'` — explicit PASS, differential 2.874s. No production-code changes after the full suite (comments/test identity assertions only).
- `.32` post-rebase: `rt -- bash -lc 'cd server && gofmt -l internal/ tools/ && go vet ./... && go test -count=1 ./internal/discovery/... ./internal/api/domaindiscovery/ ./internal/convert/... && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/discovery/difftest/'` — exit 0; discovery 0.088s, api/domaindiscovery 0.039s, convert 8.561s, enabled differential 1.790s. The rebase brought only reviewed Surge/Loon changes outside C37.
- `.32` final production snapshot (after rebase): `rt -- server/scripts/proddiff.sh` — exit 0. Final output: `self-check: status=PASS json-leaf=PASS yaml-leaf=PASS text-line=PASS`; `cases=2060`, `compared=236`, `coverage_regressions=0`, `new=0`, `stale=0`, `self_check=PASS`; `baseline: new=0 stale=0`. All six C37 GET templates covered. Existing five baseline keys remain; no baseline edits. Saved inputs stay in the framework's private server directory.

File inventory (lines; before hand-back documentation commit): existing discovery.go 310 → 45; discovery_test.go 24 retained. Added under discovery/: auto.go 277, bridge.go 633, jobs.go 183, lifecycle_test.go 293, messages.go 60, model.go 339, probe.go 237, region.go 333, settings.go 307, summary.go 575, taskbridge.go 59, website.go 145, write.go 384, difftest/diff_test.go 428, difftest/testdata/reference.py 67. Added api/domaindiscovery/: routes.go 236, messages.go 4, routes_test.go 70. Server wiring imports/registry lines and 17 routes.tsv rows updated. No backend/frontend/doc UI changes, generated corpora, dependencies, pushes or sub-agents.


Independent adversarial review (go-review-c37; fixes c524c1b7, f4d99211, ccf7c627):
- **medium**: null note silently removed annotations; explicit null apply rule type was defaulted.
  Both now return the Flask validation failure without changing stored state.
- **medium**: legacy numeric confidence, invalid stored port, mixed history/ignore entries and
  empty JSON options caused rejected settings or different request defaults. Native JSON decoding
  now applies the documented field defaults; differential covers these persisted/request shapes.
- **medium**: metadata slice length hid an additional retained job; tail pointers are now cleared.
  Manual task runner also retained request values; worker contexts now carry only the repository.
- **medium**: region aggregation sorted target names instead of preserving Agent preference, and
  omitted Agent marker maps became null. Typed region aggregation preserves order and empty defaults.
- **low**: idle matcher expiry could wait another sweep; cleanup timer honors the earliest deadline.
- **low**: failed jobs exposed partial error metadata; completion polling shared its mutable timestamp
  pointer. Failure metadata and returned snapshots now match the Flask/immutable ownership contract.
- **test reliability**: Flask applies target groups from a set; comparator now normalizes only explicit
  apply/history lists (same-time groups), preserving distinct chronology and all ordered region arrays.

Review scope/evidence: every branch of the three Python source files, all 17 routes' success,
validation/auth/missing-profile paths; 78 sequential Flask requests + six missing-profile requests +
live same/cross-profile conflicts and isolated polling, with Agent protocol and persisted side effects.
Fresh-process restart sees no phantom jobs and retains probes. Cancellation/exclusivity stress (60
rounds), automatic-loop cancellation/flock release, weak-pointer tests of 80 job results and 8 rounds
of 80 Agents × 150 hosts all pass scoped race. Production cache options are asserted at 4/1024/300s;
60 profiles × 1400 host lookups prove caps and collection of evicted/expired objects. Referenced rule
indexes scale with rule input within four matcher slots; no absolute byte ceiling is claimed.
Only bridge.go/taskbridge.go import jsonx in production discovery code; region orchestration is native.
Six GET templates are now enforced in proddiff-coverage.txt. Detailed 17-route matrix and execution
record: parity/progress/review-c37.md.

Final .32 acceptance at integration 77df305c + review f6a61e52:
- gofmt (internal/tools/cmd) silent; go vet ./... passes.
- go test -race -count=1 ./... passes; discovery 8.172s, API 1.819s, rules passes.
- CONFIGFLOW_DIFF=1 go test ./internal/discovery/difftest/ passes, 1.980s.
- rt -- server/scripts/proddiff.sh passes: 56 GET templates, 2060 cases/236 comparisons,
  coverage_regressions=0, new=0, stale=0, all four self-checks PASS; existing baseline five keys
  (agents:value=3, config:value=2), no C37 differences and no baseline edits.
- Integration advanced with independent C30 changes during final race; rebase and focused
  integration/proddiff recheck passed, without repeating the full race suite.
- Post-rebase f45e0cce: gofmt silent, vet and focused node/subscription/server/discovery tests pass;
  enabled Flask differential 1.780s. proddiff now 65 GET templates/282 comparisons, no new/stale
  baseline or coverage regressions, four self-checks PASS; all six C37 GET templates enforced.
- Final rebase onto 6a29590d adds only parity/difftest-config.md review documentation. Clean worktree,
  no push. Exact commands and final output recorded in parity/progress/review-c37.md.


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
   `agents`, `agents/metrics`; `subs.pyIntStr`; `repo.pythonFloat`; `substore.PrepareURL`; `repo/compat_health.go` → `health/check.ValidateHealth` (no cycle; done in review C16); C14's accepted
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
    from `yamlx` and switch (item 8). **Done in review C17**: both allowlist rows are gone, `yamlx.FlattenMapping` is used.
11. **Leftovers from the C12 review (2026-11-16):** (a) `convert.InspectStructuredProxyString` keeps the
    yaml.v3 loose loader only as a fallback for node strings with a recursive alias (needed by repo's
    verified `TestCompatProxyStringTimestamp`); removing it needs a `yamlx` option for recursive aliases
    (owner C02 — next C02 touch). (b) `convert/pyhelpers.urlsplit` stays a private copy until `urlx.Split`
    matches Python for bracketed non-IP hosts (`https://[github.com]/`); switch when that `urlx` fix lands.
    (c) `repo`'s `TestWriteWaitsForReleasedFileLock` showed a data race in test code once under `-race`
    (goroutine vs deferred close); fix in the next repo touch.
12. **`converters/surge.py::get_aggregation_nodes`** has no caller in `backend/` and no test: not ported,
    under the maintainer's rule for code without callers (2026-11-16).
13. **EasyTier URL check (C12)**: deliberate fix of a Python validation bug — see PROGRESS-agents.md
    "Coordinator decisions" item 6. The Go version rejects a URL with a single trailing "\n"; follow-up by
    the next agent touching `convert/easytier.go`.
14. **From the C13 review (2026-11-16):** `internal/reqproxy` is now the shared requests-2.31 proxy logic
    (A07's port moved there; `agents/proxy.go` is a shim). Every package that makes requests-style calls
    uses it; `reqproxy.ForRedirect` should also replace Go's redirect proxy rules in `agents.HTTPClient`
    (A01 follow-up for the next agent touching `agents/client.go`). C17 review: check the timing flake in
    `TestAggregationFallsBackToCommittedArtifactBeforeFirstServe` (0.2 s budget, failed once under load) — done in review C17 (a background render raced the temp-dir cleanup; the fixture now drains flights).
15. **Open items for C06 (httpx), found by the A10 porter (2026-11-16):** `httpx.File` differs from
    Werkzeug 3.1 `send_file` on conditional/range requests: 304 should keep `Content-Disposition` and drop
    `Last-Modified`; Range is evaluated before the 304 check; `If-Range: W/"tag"` matches; a failed
    `If-Match` is 412; `Range: bytes = 1 - 2` (spaces) is accepted; Range on an empty file is ignored;
    multi-range or malformed Range is 416; ETag mtime rounding differs. Affects every send_file route; fix in
    the C06 review and remove the "known httpx differences" from the A10 download differential. Added by the A10 review (2026-11-16): httpx's Range parsing uses int64 (`bytes=0-99999999999999999999` is 206 in Werkzeug), does not strip Latin-1 white space (U+00A0/U+0085) around Range items, and parses If-Range/If-Modified-Since with `http.ParseTime` instead of `email.utils.parsedate_to_datetime` (no weekday, numeric zones, RFC 850/asctime variants); the A10 route's own 500 decision now follows Werkzeug (`internal/api/agents/downloads_date.go`, `werkzeugConditionalError` — reusable for httpx).
16. **No runtime reads from `backend/` (2026-11-16):** the Go server must run without the Python tree.
    Every data file the Python code reads at runtime (`backend/schemas/oxidns/*.json`, templates, scripts,
    `config_template.json`, …) is embedded with `go:embed` from a copy under `server/`, guarded by a test
    that fails when the copy drifts from `backend/` (pattern: `repo.TestConfigTemplateMatchesBackend`,
    `installer.TestScriptsMatchBackend`). C22/C23 currently read the OxiDNS schemas through the resource
    root: the C22/C23 reviewer switches them to an embedded copy. Finish (#40): the image must contain
    tzdata (`/usr/share/zoneinfo`), because OxiDNS timezone validation follows the host's zoneinfo like the
    Python server.

## N7-3b — rulefetch / refresh / structured proxy YAML readers

Status: verified migration boundaries; pinned `.32` acceptance and N7-3a integration gates passed.
Branch `go-n7-3b`; detailed inventory/results: [progress/N7-3b-readers.md](progress/N7-3b-readers.md).
Scope added by coordinator: convert structured proxy reader and api/subscriptions'
program-generated JSON→YAML helper; minimal convert error-return caller adapters
in repo/provider/health and rule append-error caller adapters are separate commits. Shared yamlx validation
comes from N7-3a's eight shared commits, without private policy/merge implementations.

Migration gate replays actual caller inputs from the existing Python corpora and
whole-suite recordings, comparing exact response fields/rule values; YAML edited
text is compared structurally plus separate exact splice/no-op/comment tests.
Native diagnostic comparisons keep refusal channels; D1/policy cases assert the
native diagnostic and retain the source. Special cases have exact native output
fingerprints keyed to the original suite/function/input (no copied private data).
No skip. Counts below are changed boundary rows, not all corpus rows:

| §5.7 / decision | rulefetch corpus | rulefetch whole-suite | C14 review |
|---|---:|---:|---:|
| D1 duplicate refusal | 63 | 20 | 17 |
| §5.4 tag/merge/document validation | 21 | 0 | 18 |
| Native diagnostics/positions | 6417 | 32 | 11 |
| D3 layout, equal Node semantics | 2327 | 68 | 36 |
| D3 quoted scalar folding | 9 | 2 | 0 |
| Alias refusal diagnostic collation | 4 | 2 | 0 |
| Native Node mapping keys | 0 | 0 | 5 |
| Binary scalar text | 0 | 0 | 1 |
| Python empty-sequence bug fixed | 25 | 0 | 0 |
| Python alias-append order bug fixed | 1 | 0 | 0 |

Convert: 48 distinct changed corpus/review inputs: native scalar schema 28,
D1 4, §5.4 7, recursive alias refusal 5, native JSON number spelling 2,
map member order 2. Cached repetitions assert those same exact expectations.
Repo caller golden: 30 explicit native expectations: scalar schema 18, D1 3,
§5.4 6, JSON complex-key refusal 2, member order 1. All remaining rows retain
strict existing comparisons and earlier accepted differences.

Alias coverage: depth-40 shared diamond, linear chain and self-reference across
rule inspection/normalization/read/edit/remote validation and both proxy readers;
no expansion/null substitution. Proxy policy/alias errors never reach Sub-Store
or its local cache. Refresh metadata/remember/download/preview entry tests reject
unsafe graphs before writing either cache. Native malformed input uses existing
validation channels. Runtime `yamlx.Clone` callers: zero.

Integration dependency closed: rebased to go@8303397c after N7-3a merged; the
eight equivalent shared commits were automatically dropped, with no yamlx diff
against go. Refresh's delegated hosts/dialer/proxy-list reads pass -race278.151s;
all12 CONFIGFLOW_DIFF difftest packages pass, including refresh/subs/substore.
Proddiff2831 cases/435 compared, self_check PASS, new=0 stale=0. Full p4 suite
also passes on this integrated source (subs8.019s/substore2.734s/yamlx1.948s).
Remaining bare decoders in
importer/rules/oxidns are assigned to N7-3d; legacy loaders/pyyaml remain until
that follow-up, as instructed.

Final supplement after O3 merged at go@197c3c3f: pinned .32 format/vet empty,
refresh -race393.973s PASS; all12 CONFIGFLOW_DIFF difftest packages PASS;
proddiff2831 cases/435 compared, self_check PASS, new=0 stale=0. No new b code
changes or production baseline adjustments. Commands/tails: review-n7-3b.md.

C09 consumer follow-up: `rules.TestReviewC09ApplyIPRule` replays1536 rows;
198 changed YAML layouts compare structurally and36 malformed-YAML rows retain
the Chinese context with the exact native parser suffix. All result/status/rule
fields remain exact, and all other comparisons/previous allowances remain.
Only this consumer test changed; rules' runtime reader remains N7-3d scope.

Final shared integration: all8 yamlx fixes are present (writer uses09740db3,
logical equivalent of ae109f82). Proxy keys use shared ProjectJSONKeys; container
and immutable-scalar alias sharing is retained. Native large decimal precision,
1MiB scalar ×100 alias backing-data identity, tagged metadata re-encoding, and
all splice/no-op checks pass. The shared HasYAMLPayload hint now recognizes
comments/BOM/CRLF/--- before a flow payload; nil-payload append re-reads hinted
YAML, and opaque Classical text is preserved. Deploy's two A11 review tests plus
a new nonempty exact-output regression pass with race. This is the same
format-hint bug in Python/old Go, recorded in behaviour-changes.md. Production
baseline remains unchanged. Lenient cached proxy policy/alias skips now emit
index/category-only warnings and a local skipped=N summary; strict returns400.

N7-3d convert reader iteration:7487+474 real Python reader-relevant rows,
expected changes276+2=classified278,unknown0;YAML1.1/timestamp182,
native-diagnostics34,nonfinite text31,binary/tagged31;3837 document-mode sources.
Diagnostic position subclass:32 primary lines/34 missing native columns,
per-row record in convert/testdata/n73d_diagnostic_positions.json. Native
Node→jsonx preserves physical sharing; N8-8 replaces this temporary boundary.

N7-3d historical test merge: yamltest→yamlxtest.21,343 unique actual old/new
reader comparisons frozen before implementation deletion, complete native audits
and old graphs/errors retained,unknown0; mode-specific history7,174 inputs.
All ten helper-dependent package suites passed; .32 full vet and changed API/
facade-helper tests passed. New AST gate will enforce the final cleanup.

N7-3d cleanup implementation complete; final gate pending. All old loaders/types
and internal/pyyaml removed; yamlbridge retains only jsonx/native Node support.
AST gate rejects bypass readers/runtime Clone and migration-helper imports.
C29 shared bb1dac8d consumed as e10291f5. Native old/new reader gate on .32:
native53364 (equal52544/D1547/policy177/alias96), template324455
(equal324190/D1132/alias133),unknown0. Shared old native decoder is test-only,
centralized within yamlxtest; the empty-stream contract has a regression test.

## N8-8a — typed convert foundations (2026-11-18)

独立复核（`go-review-n8-8a`，作者基线 `3f328827`）已完成，1 medium、1 low 均已修，0 open：
MaterializeAliases 在原定义移到首次 alias 后面时正确撤掉旧 Anchor，公开 Edit 可闭合；
字节门新增三种引擎精确数量和 source/row 去重检查，防止缩成 10 条 Mihomo 而虚假通过。
旧基线/当前树各抽三引擎首中末共 9 样本，均匹配冻结原始字节 SHA；31 路径副作用夹具无额外放宽。
.32 gofmt/vet、复核新增测试与风险代表定向 race 全绿；未重跑作者整套 corpus/fuzz/4548 输出。
详细结论、命令和剩余迁移边界：[review-n8-8a.md](review-n8-8a.md)。

Completed on `go-n8-8a`, rebased onto `go@fbbe382ec` (batch-4 included).
Common rules, node conversion/caches, dialer resource graphs/Provider topology and
EasyTier consume typed model records and native Nodes; old signatures are codecs
in `convert/legacy.go`. Core AST checks forbid jsonx/any/dynamic exceptions and
reverse calls into legacy. N8-5(c) is not required. Normal generated output remains
byte-identical: 4548 original Go results (2353 Mihomo/1064 Loon/1131 Surge).
Old Python goldens remain unchanged; approved R5/validation, Unicode ordinals and
31 input-immutability rows have narrow explicit assertions and ledger entries.

N8-9 prerequisite: the single typed rulesetfmt implementation landed early in
`a633babfe`; N8-9 migrates remaining consumers and deletes its thin legacy adapter.
Shared yamlx-only fixes: `2aee129ab` rejects unparseable tagged scalar output;
`e11becd23` statically validates manual kinds/anchor bindings and keeps Parse/Equal
closure checks in tests. No runtime output Parse/Equal pass was added.

Targeted .64 checks passed: convert full non-race, changed-entry race, yamlx and
rulesetfmt race, repo consumers, gofmt/vet, all converter archives/306 client
boundaries and nine 60-second fuzz targets. Coordinator runs full gate/proddiff
on its batch tree under Leaner verification. Exact commands, output, commit map,
class counts and remaining (b)/(c)/(d)/N8-9 adapters:
[`progress/n8-8a.md`](progress/n8-8a.md).
