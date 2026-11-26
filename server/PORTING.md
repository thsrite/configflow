# Porting the ConfigFlow backend from Python to Go

This directory holds the Go rewrite of `backend/` (Flask). The Vue frontend, the Go Agent
(`backend/agents/go-agent`) and the on-disk data format stay as they are. The Python code remains in
the tree as the reference until the Go server has full parity; it is never run next to the Go server.

**Feature parity is a hard requirement.** Nothing is dropped silently. If something looks dead or
not worth porting, list it in your hand-back under "Not ported" with evidence; do not decide alone.

**Parity standard = functional parity (maintainer's decision, 2026-11-16; Python is retired for good,
no rollback to it).** This overrides the stricter wording of the rules below where they conflict.
- **Must match Python:** every feature, route and background task; API status codes, JSON field names,
  types and values; Chinese user-facing messages (the frontend and the Agent match on them); the Agent
  protocol; existing `configflow.sqlite3` and `DATA_DIR` data must be read and written compatibly
  (Python-written data loads, nothing is lost on read-modify-write); generated configs (Mihomo, Surge,
  Loon, MosDNS, OxiDNS, router scripts) must be **semantically** identical — same structure and values
  after parsing, same lines/rules in the same order where order matters; validation must accept/reject
  the same inputs (same status code, same Chinese message).
- **No longer chased:** byte-level formatting where the consumer parses the result (YAML quoting/wrapping,
  JSON whitespace, anchors — a one-time config change at the switch is accepted); text of Python-internal
  exceptions on malformed input (`string indices must be integers`, PyYAML/TypeError texts) — Go still
  rejects with the same status code and its own clear message; hash/set iteration order; CPython-only
  Unicode edge cases (`'ß'.upper()`, superscript `isdigit`) unless a test or real user data depends on them.
- **Go-native libraries first** (maintainer, 2026-11-16: "能用 go 原生库的肯定要用"). New code uses the
  standard library or an established Go library (`net/netip`, `net/url`, `net/http`, `strings`,
  `unicode`, `archive/zip`, `compress/*`, `gopkg.in/yaml.v3`, `regexp`/`regexp2`) instead of emulating
  Python. Do not add new Python-emulation code. Call YAML/JSON through the `yamlx`/`jsonx` facades
  (their implementations are being swapped to native libraries behind the same API — see "Native-library
  swap" in WORKFLOW.md); call `strings`/`unicode` directly instead of `pystr`, and `net/url` /
  `http.ProxyFromEnvironment` instead of `urlx`/`reqproxy` in new code.
- **Comparisons:** replay/corpus/differential compare outputs structurally (parsed YAML/JSON) and
  exceptions by class/status + whether rejected, not by message text, except for Chinese user-facing
  messages, which must match exactly. Rows that differ only in a "no longer chased" way count as equal;
  list such classes once in the module section.

## Layout

```
server/
  go.mod                      module configflow (Go 1.26)
  cmd/configflow/             main: flags/env, wiring, HTTP listener, background loops
  internal/jsonx/             ordered JSON values with Python-compatible encoding (see below)
  internal/store/             SQLite document store (port of common/config_store.py)
  internal/repo/              ProfileRepository (port of common/config_repository.py, common/config.py)
  internal/httpx/             router, request context, auth hooks, JSON responses, redaction
  internal/<area>/            one package per work package (rules, subs, mihomo, oxidns, agents, routers, ...)
  internal/api/<blueprint>/   HTTP handlers, one package per Flask blueprint file
  parity/routes.tsv           every method+path of the Python app; fill in "go package" and "status"
  testdata/                   goldens (copied from backend/testdata or generated from Python)
```

## Rules for every work package

1. **Port features, not code** (2026-11-16, maintainer: idiomatic Go is the goal). Read the Python module
   and its tests to understand *what* it does, then design it the Go way: your own types, function
   boundaries and names; no `any` parameters or emulated Python dynamic typing to mirror Python call
   shapes. Chinese user-facing messages stay byte-identical. **No size budget** (maintainer, 2026-11-16: line counts are
   meaningless as a limit). The hard rule is: **no Python emulation** — no ports of CPython/PyYAML/
   requests/stdlib behaviour, no emulated Python exceptions or texts, no `any` parameters mirroring Python
   call shapes, no private copies of shared helpers. Write clear Go; never shorten code or drop comments
   to hit a number.
2. **Types: Go-native only.** Use typed Go structs with `encoding/json` tags. Fields of a stored
   document the code does not know are kept with native means (e.g. an `Extra map[string]json.RawMessage`
   captured by a custom `UnmarshalJSON`/`MarshalJSON`), so read-modify-write loses nothing. Free-form
   user-edited YAML (custom Mihomo/MosDNS fragments) is held as `*yaml.Node` from `gopkg.in/yaml.v3`
   (keeps order and comments); free-form JSON as `json.RawMessage`. Do not use `jsonx` in new code; it is
   removed in the S passes (N8).
3. **Stored data must stay compatible.** Data written by Python (`configflow.sqlite3`, files under
   `DATA_DIR`) must load in Go, and Go must write data the frontend and Agents read correctly. Byte-level
   identity with Python's formatting is not required.
4. **Acceptance = same external behaviour, proven at the boundary** (changed 2026-11-16). Required per
   module:
   - **Flask↔Go HTTP differential** for every route (success and failure): status, JSON fields/values
     (structural), Chinese messages, and the data written to `configflow.sqlite3`/`DATA_DIR` afterwards
     (compared after parsing).
   - **Generated outputs** (Mihomo/Surge/Loon/MosDNS/OxiDNS configs, router scripts, install scripts,
     Agent payloads) compared structurally against Python for a corpus of realistic and edge inputs,
     driven through the public entry points (not internal helpers).
   - **Agent protocol and background tasks**: same requests/responses and same effects.
   - Hand-written Go unit tests for the module's own logic where useful.
   Function-level record & replay of Python internals is **no longer required**. Existing replay tests
   may be kept while they pass; when code is restructured, replace them by boundary tests instead of
   bending the Go code to Python's call shapes. In the module's PROGRESS section, list every Python test
   file and which boundary test covers its behaviour (or why none is needed).
4b. **Request cancellation must not abort work Python would finish.** In Flask a request thread runs to
   completion even if the browser disconnects. In Go, `c.Context()` is cancelled when the client goes away.
   Pass `lifecycle.Detach(ctx)` (keeping values and shutdown cancellation) to anything that writes state, takes
   repository locks, runs shared/coalesced downloads, spawns workers or records task status; only use the
   cancellable context where Python itself has a timeout. Reviewers check this for every handler (found as
   a high-severity defect in C13, C15 and C16). From O3 on, the owning package's exported entry point detaches,
   not the handler (o3-concurrency.md 2.5).
5. **Device and network I/O goes through interfaces** declared in your package (HTTP client,
   clock, command runner), so tests use fakes instead of patching globals. No test may contact a
   real host; never run anything against production addresses.
6. **No new dependencies** beyond those already in `server/go.mod` without saying so in the
   hand-back. Standard library first.
7. **Concurrency, locks and background tasks follow `server/design/o3-concurrency.md`** (O3, 2026-11-17):
   one server process per DATA_DIR (instance lock); no new `flock` — Python's per-resource file locks become
   in-process locks (`lockx`, levels L1 try-only → L2 → L3 `.config.lock` → L4 → L5); loops, startup work and
   detached jobs go through `lifecycle` (Periodic / Startup / `Spawn`) with Python's first delays and
   intervals (table 2.4 there); a lock Python takes before validation is taken at the same point (409 before
   400). The old O3-2/O3-6 transitional rule has expired; the migrated concurrency boundary applies.
   The AST gate `internal/lifecycle/gate_test.go::TestConcurrencyFacadeBoundary` enforces the boundary.
   Any joined fan-out exemption is an exact `file:func` with a reason; no blanket package exemptions.
   Source-bound owners use `lifecycle.SpawnerFrom(ctx)` so a stopped generation cannot move work
   into the global Runtime. Local HTTP callbacks require an injected tracked Spawner.

8. **HTTP handlers** live in `internal/api/<blueprint>` and only translate HTTP ↔ package calls.
   Register routes in that package's `Register(*httpx.Router)`; never edit another package's
   registration. Update `parity/routes.tsv` for the routes you implemented (`go package`, status
   `done`). Response bodies, status codes, headers and the token redaction described in
   `internal/httpx` must match the Flask app.
9. **Where tests run.** Never run tests, builds or `go vet` on the Mac. Use
   `rt -- bash -lc 'cd server && go test ./internal/<pkg>/...'` from your worktree (it syncs the tree
   to the test server and runs there). Python reference runs:
   `rt -- bash -lc '~/cf-test/.venv/bin/python -m pytest backend/test_x.py -q -p no:cacheprovider'`.
   Only run your own packages; the server is shared.
10. **Git.** Work only inside `server/` (plus `parity/` rows for your routes). Commit on your own
    branch with a message `go(<package>): <what>`; do not push, do not touch `backend/`, `frontend/`,
    `docker/`.

## Progress file (required)

Keep `server/parity/progress/<N>-<short-name>.md` up to date for your package (N = package number) and
commit it **together with every commit you make**. If you are interrupted (usage limit, crash), a fresh
agent continues from this file instead of from your transcript, so it must be enough on its own:

- **Brief**: one paragraph restating your task and scope (including scope decisions the coordinator sent
  you later).
- **Done**: what is ported, committed and passing (with commit ids), per Python module/test file.
- **In progress**: what you were doing when you last committed; files that are half-written.
- **Next**: the remaining steps, in order.
- **Decisions and findings**: Python quirks you reproduced, differences you accepted and why, helper
  copies in `compat.go`, open questions for the coordinator.
- **Commands**: the exact `rt` commands to run your tests and regenerate your corpora.

Keep it short and factual (it is read by an agent, not a person); replace stale content instead of
appending a diary.

## Hand-back format

- Files added, with line counts.
- Python files covered and, per file, anything not ported or deliberately different (with reason).
- Python test files → Go test files, number of cases ported / skipped (with reason).
- Result of the test run on the test server (command and tail of the output).
- Open questions and assumptions about other packages' APIs.

## Reference: packages already available

Read the source for exact signatures; every exported name has a doc comment.

**`configflow/internal/jsonx`** (`doc.go`, `object.go`, `value.go`, `number.go`, `decode.go`, `encode.go`)
- `Object`: `NewObject()`, `Obj("k", v, ...)` literal, `Get/Value/GetOr/Set/SetDefault/Delete/Pop`,
  `Keys/Values/All()` (range in insertion order), `Clone` (shallow), `Update`, `DeepCopy`, typed getters
  `GetString/GetBool/GetObject/GetList/GetNumber/GetInt/GetFloat` (zero value when missing or of another
  type; reads are safe on a nil `*Object`, writes are not).
- Helpers: `AsObject/AsList/AsString/AsBool/AsNumber/AsInt/AsFloat`, `ListOf`, `Strings`, `Path(v, keys...)`,
  `Truthy` (Python `bool(v)`), `Equal` (Python `==`: `1 == 1.0`, dict order ignored), `DeepCopy`,
  `Repr`/`Str` (Python `repr()`/`str()` for messages).
- Numbers: `Int(i)`, `BigInt`, `Float(f)`, `ParseNumber`; `IsInt/IsFloat/Int64/Float64`.
- `Parse([]byte)` is `json.loads`; errors carry Python's message.
- Encoders, byte-identical to CPython 3.11 (verified against a 634-value corpus):
  `Compact` (stored SQLite values), `Indent2` (files; append `"\n"` yourself), `Sorted` (content
  digests), `Default` (`json.dumps(v)`), `Response` (`flask.jsonify` body: sorted keys, ASCII, compact,
  trailing newline), and `Encode(v, Options{EnsureASCII, SortKeys, Compact, Indent})` for the rest.
  `backend/common/json_response.py::jsonify_external` is `Encode(v, Options{EnsureASCII: true, SortKeys: true})`
  without a trailing newline. With `LOG_LEVEL=DEBUG` Flask's `jsonify` indents by 2.
- Not provided: `default=str`, `allow_nan=False`; convert or check before encoding. Request bodies with
  a BOM must be stripped before `Parse`.

**`configflow/internal/store`** (`store.go`, `conn.go`, `rows.go`, `errors.go`) — port of
`common/config_store.py`, interchangeable with it on the same `configflow.sqlite3` (cross-checked both
ways). `New(path, timeout)`, `Load`, `Meta`, `Reading(func(*Conn) error)`, `Commit(base, document, meta)`
→ `(entry, committed, err)`, `Create`, `Check`, `BackupTo`, `PreserveCorrupt`, `Fingerprint`. `Entry.Document`
is shared and must never be mutated: `DeepCopy()` first. Errors are `*store.Error`; use `errors.Is` with
`ErrStore`, `ErrCorrupt`, `ErrUnsupported`, `ErrConflict`, `ErrStorage`, `ErrSQLite`, `ErrDocument`; `Error()`
is Python's `str(exc)`.

**Infra (package #1)** — each has a package doc; read it before use.
- `internal/yamlx`: PyYAML 6.0.1 on the jsonx model (parser `internal/pyyaml`, constructor port).
  `Load`/`SafeLoad(text)` is `yaml.safe_load` (values, `*yamlx.Error{Class}` with PyYAML's text);
  `LoadWith(text, LoadOptions{Schema: SchemaNative, Key, Name, KeepNonStringKeys})` (NativeLoader via
  `Key`; `KeepNonStringKeys` gives `*yamlx.Mapping` for dicts with int/bool/None keys, see `Lookup`);
  `LoadCSafeFirst` is "CSafeLoader, else safe_load". Values jsonx cannot hold: `*NonStringKeyError`,
  `*UnrepresentableError` (recursive aliases, `!!set`/`!!omap`/`!!pairs`).
  `Dump(v, DumpOptions{AllowUnicode, SortKeys, FlowStyle, Indent, Width, IndentSequences, ExplicitStart,
  ExplicitEnd, NoAliases, OnMarked})` is a port of PyYAML's pure-Python emitter, byte-identical for the
  option sets used in the backend (11 sets × 1374 corpus values). Presets: `SafeDump` (`yaml.safe_dump(v)`,
  sorted), `SafeDumpUnicode` (`allow_unicode=True, sort_keys=False`), `IndentDump` (`IndentDumper`).
  `FlowList`, `FlowMap`, `*SharedString` (anchor/alias), `Marked` + `OnMarked` (for `push_diff`).
  CSafeDumper output (used once in `push_diff.py` to measure size) is not reproduced.
- `internal/logx`: `Setup(Config)`, `Get(name)` with `Debug/Info/Warning/Error/Critical` and `…f`
  variants, `Exception(msg, err)`, `LogText(value, limit)` (Python `log_text`, default limit 120),
  `ClientIP(req)`. **Use the Python module path as the logger name** (`backend.routes.agents`): the log
  line format `2026-11-14 12:34:56,789 - name - LEVEL - msg` is what the log page shows.
- `internal/jsonstore`: one JSON file per id under a directory, atomic writes (`utils/json_store.py`).
- `internal/urlx`: `SafeURLForLog`, `SafeExceptionDetails(err)` (uses `PythonClass()` when an error has
  it), `Unquote`, `UnquotePlus`, `QuotePlus`, `Split`, `RequestsPrepareURL`.
- `internal/pystr`: CPython 3.11 string rules on Unicode 14 tables: `Lower` (str.lower), `IsSpace`, `Strip`,
  `Decimal`, `Int(s, base)` (int()), `Float(s)` (float()). Use these instead of Go's unicode tables.
- `internal/taskstatus`: `DATA_DIR/task-status.json` store; `Record`, `Get`, `Load`, `Forget`, `NowISO`,
  `ParseTime`, `FromISOFormat` (CPython `datetime.fromisoformat` semantics); `OnRecord` hook.
- `internal/localhttp`: in-process dispatch of requests addressed to this server (`utils/local_http.py`):
  `Register(handler)`, `Do(ctx, Request)` → `(nil, nil)` means "use the network".
- `internal/resource` (bundled files: `FromEnv().Backend(name)`), `internal/utilx` (`GetLocalIP`,
  `GenerateRandomToken`, `StrToBool`), `internal/version` (`Version`, `Info()`), `internal/jsonresp`
  (`External(payload)` = `jsonify_external` body; `PlainTree`; `Sanitized` marker — HTTP glue is #3's).

**`configflow/internal/rules` (package #4)** — rule syntax, matching, rule cache names, group
candidates, reorder, strategy references, connection rules. Items are `*jsonx.Object`, lists `[]any`.
- **Python regexes: always `rules.CompilePyRegex(pattern, ignoreCase)`** (never `regexp.Compile` on user
  input). It validates with a port of CPython's `re` parser (same error texts; `IsPyRegexError(err)`),
  runs on RE2 when that is exact and on a backtracking engine (regexp2) otherwise, so look-around,
  back references etc. behave as in Python. `PyRegex.Search`; a match on the backtracking engine is cut
  off after 1 s and counts as no match. Only `\N{name}` and repeat counts beyond int32 return
  `*UnsupportedRegexError`. If you need another method (fullmatch, findall, sub), ask in the hand-back.
- Parsing/matching: `ParseRuleLine`, `MatchDomain`, `MatchIP`, `MatchQuery`, `IsValidDomain`, `IsValidIP`,
  `NormalizeMatchQuery`, `NewRuleConfigMatcher(ruleConfigs, ruleLibrary, load, indexKey, logger)` with
  `Match(query)`, `NewMatcherCache(MatcherCacheOptions)` with `LocalMatcher(config, profileID)`.
- Syntax: `RuleValueError(kind, value)` ("" = valid), `DomainValueError`, `CIDRError`, `SplitRulePayload`,
  `RuleTypes`, `IsRuleType`, `LooksLikeIP`.
- Rule cache files: `SanitizeRuleName`, `RuleCacheFilename(rule)`, `RuleCacheReadPath(rule, library, stored,
  rulesDir)`, `TouchUnchangedCache`.
- Groups/policies: `FilterCandidates`, `ValidateFilters`, `ValidateCandidates`, `MergeGroupUpdate`,
  `ManagedMembers`, `ManagedProviders`, `RenderMembers`, `ExclusionFilter`, `LowerGroupProviderFilters`,
  `ReorderByIDs`, `ResolveNewOrder`, `ValidateRulePolicies`; errors `*CandidateValidationError`,
  `*StrategyReferenceError`.
- Connection rules: `IPRuleTargets`, `ParseDestination`, `ApplyIPRule` with `ConnectionDeps` callbacks
  (`RulesetInfo` from #19, `ReadRulePayload`/`AppendRulePayload` from #12).
- Left for other packages: remote rule cache validation and fetching (#12), `find-duplicates` (#16, lives
  in `routes/rules.py`), converter-driven strategy reference tests (#6/#7). `rules/compat.go` holds copies
  of helpers owned by #6, #11, #12; replace them when those packages land.

**`configflow/internal/repo` (package #2)** — `ProfileRepository` and `common/config.py`. Interchangeable
with Python on the same data directory (cross-checked: fresh init, 80 scripted operations, both-way
interchange). Read `repository.go`, `scope.go`, `hooks.go` doc comments before use.
- Open: `OpenDefault(dataDir)` (production), `Open(dataDir, Options{...})`; module level `SetRepository(r)`,
  `GetRepository(ctx)`, `WithRepository(ctx, r)` (tests), `DataDir()`.
- **Every method takes `ctx` first.** Transactions carry their state in the ctx: an updater receives a
  derived ctx; reads with it see the working copy; a write with it fails with `ErrInUse`
  (`不能在配置事务中再次开启写事务`); a write with any *other* ctx inside an updater deadlocks until the lock
  timeout. Always pass the ctx you were given.
  ```go
  saved, revision, err := r.UpdateProfileTransaction(ctx, id,
      func(ctx context.Context, current *jsonx.Object) (*jsonx.Object, error) {
          groups := current.GetList("proxy_groups")
          current.Set("proxy_groups", append(groups, g)) // slices are values: Set the list back
          return nil, nil                                  // nil = keep edited current; error = abort
      })
  ```
- **Request scope** replaces Python's ContextVars (per-request config snapshot and three-way baseline):
  httpx installs `ctx = repo.WithScope(req.Context())`, `repo.SetRequest(ctx, repo.RequestInfo{…})`,
  `defer repo.ResetConfigContext(ctx)`. Handlers then use `repo.GetConfig(ctx, "")` /
  `repo.SaveConfig(ctx, nil, "")` exactly like Python's `get_config()` / `save_config()`. Background loops
  use `repo.WithScope(loopCtx)` per task or no scope (then nothing is cached and saves have no baseline).
- Profiles: `ListProfiles`, `GetProfile`, `ProfileMetadata`, `DefaultProfileID`, `SetDefaultProfile`,
  `CreateProfile`, `CloneProfile`, `ImportClientProfile`, `UpdateProfile`, `DeleteProfile`,
  `UpdateProfileTransaction`, `UpdateProfileFields`/`SaveProfile`, `ImportProfile`, `ExportProfile`,
  `ConfigRevision`, `GetPublishSnapshot(ctx, id, agents)`, `GetCompatConfig`, `GetCompatConfigWithSource`,
  `CompatConfigFrom`, `GetProfileAgentBindings`, `AgentBindingSummary`, `SaveRouterSettings`,
  `SaveRouterPublication(ctx, profileID, expectedRouter, RouterPublication{…})`.
- Shared: `GetShared`, `GetSharedWithSource`, `UpdateSharedTransaction`, `SaveShared`,
  `UpdateSharedAndProfilesTransaction`, `CreateRuleLibraryItem`, `GetResourceUsage`, `PreviewNodeDeletion`,
  `DeleteNodesWithReferences`.
- System: `GetSystem`, `GetSystemSettings`, `GetSystemWithSource`, `UpdateSystemTransaction`, `SaveSystem`,
  `GetAgents(ctx, history)` (with `history=false` the history fields are `AgentHistoryNotRead`; read them only
  through `AgentHistory(agent, field)`), `RuleProxyTokensForSanitization`, `AgentTokensForSanitization`,
  `SanitizationTokensOf(system)`.
- Export/import: `ExportAll(ctx, desensitize)`, `ImportAll`, `ResetAll`.
- Files (paths refuse escapes): `SharedPath`, `CacheDir`, `SharedCacheDir`, `SharedRulesDir`,
  `ReadSharedJSON`, `WriteSharedJSON`, `WriteSharedText`, `ProfileDir`, `ProfilePath`, `ProvidersDir`,
  `RulesDir`, `GeneratedDir`, `WriteGenerated`, `WriteProfileJSON`, `ReadProfileJSON`, `WriteProfileText`.
- `config.py` module functions: `GetConfig`, `GetResourceConfig`, `LoadConfig`, `SaveConfig`,
  `SaveConfigWithPublishGuard`, `UpdateConfigTransaction`, `SafeImportConfig`, `GetSharedConfig`,
  `SaveSharedConfig`, `UpdateSharedConfigTransaction`, `UpdateSharedAndConfigTransaction`,
  `GetSystemConfig`, `SaveSystemConfig`, `ResolveProfile(ctx, explicit, fallback *string)` (exact; use it for
  client-supplied ids), `ResolveProfileID(ctx, id)` (`""` = not given), `IsGlobalRequest`,
  `ProfileAPIPath`, `ConfigAPIPath`.
- Merge/defaults: `DeepMerge`, `ThreeWayMerge`, `GetDefaultConfig`, `GetInitialConfig`,
  `GetDefaultOxidnsSettings`, `ValidateDocument`, `ValidateProfileID`, field lists (`ProfileFields`,
  `SharedFields`, …).
- Errors: `*repo.Error{Kind, Message, Usages}`; `errors.Is` with `ErrRepository`, `ErrValidation`,
  `ErrNotFound`, `ErrExists`, `ErrInUse`, `ErrNodeDeletionChanged`, `ErrRuleLibraryCreationUncertain`,
  `ErrRejected` (validation, exists, in use: answered as a client error); `*MissingError`;
  `NewInUse`, `NewValidationError`, `UsagesOf`. Store failures stay `*store.Error`.
- **Hooks** (`repo.RegisterHooks(repo.Hooks{…})` at wiring time; no-op or fail-closed defaults):
  `JobLock` (router job lock inside the commit, #37), `ConfigurationCommitted` (WebDAV auto-backup, #17),
  `PlanNodeDeletion` (#15), `SanitizeExternalPayload` / `RedactOxidnsSettings` / `RedactRouters` /
  `RedactEasytierSettings` (desensitized export, #17), `DefaultServerDomain` and `InitAgentManager`
  (`LoadConfig`, #3/#20), `SetStructuredProxyStringParser` (#6). The package that owns a hook registers it
  from its own `Register…` function; do not edit `repo`.
- `repo/compat_*.go` hold private copies of validators owned by other packages (dialer references, proxy
  chains, health, OxiDNS, rule cache names). When you port the owning package, export the function from your
  package and tell me in the hand-back; I will switch `repo` over (it must not create an import cycle:
  packages that `repo` imports cannot import `repo`).

**HTTP shell (package #3): `internal/httpx`, `httpxtest`, `auth`, `notifications`, `tasks`, `lifecycle`,
`server`, `cmd/configflow`.** Full docs: `internal/httpx/doc.go`. Verified against the real Flask app
(82 requests, 0 differences).
- **Declare routes** in `internal/api/<blueprint>/` with one `Register(r *httpx.Router)`, then add it to
  `routePackages` in `internal/server/server.go` (that one line is the only edit you make outside your package):
  ```go
  func Register(r *httpx.Router) {
      r.GET("/api/agents", "agents.handle_agents", auth.Require(list))
      r.Handle([]string{"GET", "PUT"}, "/api/agents/<agent_id>/client-notes", "agents.client_notes", auth.Require(notes))
      r.POST("/api/agents/<agent_id>/heartbeat", "agents.agent_heartbeat", heartbeat) // public: no auth.Require
  }
  ```
  The endpoint name is `<Blueprint name>.<function>` (the name passed to `Blueprint(...)`, not the module:
  `routes.client_tools.*` is `agents.*`); auth exemptions and redaction whitelists are keyed by it.
  Converters `<name>`, `<path:name>`; Werkzeug routing rules (404/405/308) are built in.
- **Handlers** get `c *httpx.Ctx`: `c.Context()` (pass to every repo call; carries the request Scope),
  `c.ProfileID`, `Param`, `Arg`/`ArgDefault`/`ArgInt`/`ArgList`, `Header`, `JSON()` (Flask `get_json()`;
  returns the 400/415 error to return as-is), `JSONSilent`, `JSONObject`, `JSONForce`, `Data`, `Form`,
  `RemoteAddr`, `HostURL`, `URLRoot`, `IsInternalCall`, `Set`/`Get` (`flask.g`).
- **Responses**: `httpx.JSON(v)` (= `jsonify`), `JSONStatus(code, v)`, `Message(code, success, msg)`,
  `External(c, v)` (= `jsonify_external`), `Text`, `Bytes`, `RawJSON`, `Empty`,
  `File(FileOptions{Path|Data, MimeType, AsAttachment, DownloadName})` (pass `MimeType` explicitly);
  modifiers `.WithStatus`, `.WithHeader`, `.NoStore()`. Token redaction runs globally; do not redact yourself.
- **Errors**: return them. `*repo.Error` → the standard 400/404/409 bodies; `httpx.BadRequest(msg)`,
  `NotFound()`, `&httpx.HTTPError{Code, Description}` → JSON `{success:false,message}` on `/api` errors (Step B/O12); non-API errors retain HTML. Anything else or a panic → logged
  `Exception on <path> [<method>]` + a generic 500 JSON error on API paths (HTML elsewhere). Where Python returns `jsonify({'success': False, 'message': str(e)}), 500`
  explicitly, return that response explicitly.
- **Auth**: `auth.Require(h)` (= `@require_auth`), `auth.ValidateTokenOrJWT(c, systemDoc)` (config pulls),
  `auth.IsAuthEnabled()`. Public endpoints are listed in `internal/auth` (`gate`); adding one means
  editing that list — tell me in the hand-back instead of editing it.
- **Background work / shutdown**: register `lifecycle.Periodic`, `Startup` and `ShutdownHook` through
  `RegisterPeriodic`, `RegisterStartup` and `RegisterShutdown` from the package's wire function listed in
  `wirePackages` in `internal/server`. Detached jobs use `Spawn`; owners use `Detach` to keep shutdown
  cancellation while ignoring disconnects. The temporary `Loop`/`OnShutdown` adapters are removed in O3-5.
  Timings and hook phases follow `design/o3-concurrency.md`. Request hooks: `app.BeforeRequest`/`AfterRequest`.
- **Repo hooks**: `repo.RegisterHooks` from your wire function.
- **Notifications**: `notifications.RecordAgent`, `RecordRouter`, `RecordRegistrationRejection`,
  `RecordAgentAddress`, `ResolveAgentAddress`, `RecordNotice`; providers via `notifications.SetSources`.
  Any package that records task status must create its store with `notifications.TaskStatus(ctx, dataDir)`.
- **Task list (`/api/tasks`)**: each owner provides its rows with `tasks.SetRows(slot, fn)`, runners with
  `tasks.SetRunner(category, fn)`; `/api/tasks` and `GET /api/notifications` register only when every slot is
  provided (slots: subscription/rule_source #13, router_rule_sync #37, agent #20/#27/#28,
  domain_discovery #19, metrics_history #21).
- **HTTP tests** (external test package `x_test`):
  ```go
  srv := httpxtest.New(t, httpxtest.Options{Auth: true, Register: httpxtest.Routes(mypkg.Register)})
  admin := srv.Login()
  srv.Post("/api/x", httpxtest.JSON(jsonx.Obj("a", 1)), admin).WantStatus(200).WantJSON(want)
  ```
  `srv.Repo`, `srv.Context()`, `srv.URL()` (real loopback server). Auth config is process-wide: tests that
  enable login must not run in parallel with tests that expect it off. For a Flask↔Go comparison of your
  routes, extend the pattern in `internal/httpx/difftest` (loopback only).
- **Web front end**: the Go server serves the SPA and replaces nginx (`httpx.Edge`, enabled by
  `CONFIGFLOW_FRONTEND_DIR`); the web listener port logic lives in `internal/weblistener` (#3, not #14).
- `internal/redact`: response/export secret redaction (`SanitizeWithSecrets`, `ExternalPayloadSecrets`,
  `RedactSecretsInText`, …) — #17 builds `config_export` on it.

**`configflow/internal/subs` (package #5)** — manual node strings, subscription hosts/dialers/filters,
proxy chains, commented Mihomo YAML edits (`subs/commented`), listeners, proxy-server DNS, public suffix.
- Converters (#6/#7): `ParseURIList`, `ParseYAMLObject`, `ValidateProxyChains`, `LowerProxyChains`,
  `LowerChainProviders`, `AddDialerGroups(main, config, DialerDeps)`, `AdaptSubscriptionProxies`,
  `DialerRecord`, `GroupName`, `MergeSubscriptionHosts`, `NewHostsIndex(cache).ForURL`, `ApplyNodeDNS`,
  `ValidateListenerPolicies`, `FilterSubscriptionItems`. `subs/compat.go` holds a copy of
  `dialer_references` (`ValidateDialers`, `DialerReferenceError`) for #6 to replace.
- Sub-Store client (#13): `ExtractSubscriptionHosts`, `ExtractSubscriptionDialers`, `HostsDigest`, `DescribeChange`.
- Routes (#15/#16): `ValidateRegexFilter`, `RelinkGroupListeners` (`*ListenerReferenceError` unwraps to
  `rules.StrategyReferenceError`), `CompileSubscriptionFilter`.
- Deployment/agents (#24/#26): `ReportedCoreVersion`, `OutdatedAgents`, `ParseCoreVersion`, `CoreTooOld`,
  `ListenerAddress`, `FinalTarget`, `Compress`.
- Domain discovery (#19): `ExtractDomain(url, includePrivate)` — tldextract 5.1.2 with its bundled
  snapshot, exactly as the Python code configures it (never `x/net/publicsuffix`).

**`configflow/internal/importer` (C35)** — pure native client-config import; no storage or network I/O.
- `Parse(clientType, content string) (*Plan, error)`: clients `mihomo`, `surge`, `loon`, `shadowrocket`.
  The reusable Plan preserves protocol extras (`json.RawMessage`), custom settings and ordered warnings.
- `Build(plan *Plan, shared Shared, options BuildOptions) (*Result, error)`: resolves against the current
  shared catalog without mutating either input. Options provide default subscription IDs and optional
  ID allocation; production IDs use crypto/rand. Result contains **shared additions only**, profile
  fields, summary and warnings. It is JSON-marshalable into repo.ImportClientProfile's callback;
  do not write the typed existing Shared back, since the repository retains unknown existing fields.
- C34 owns HTTP dry-run/commit and its legacy-repository adapter. Use Parse before the transaction and
  Build against the catalog supplied inside the transaction. Boundary examples:
  `internal/importer/storage_test.go` (commit/rollback/opaque fields) and `corpus_test.go` (Python oracle).

**`configflow/internal/webdav` (C35)** — standard-library WebDAV backup, retention and commit worker.
- `NewService(HTTPDoer)` supplies an injectable transport and clock; the default transport rejects
  cross-origin redirects. `Service.Test(ctx, Settings)` checks/creates directories; `Service.Upload`
  takes an in-memory json.RawMessage full export and returns filename/removed count. Each HTTP operation
  has a 30s timeout. `DescribeError` emits only safe categories/status; never echo raw transport errors.
- `Capture(ctx)` obtains the originating repository's full document Snapshot for a manual backup;
  the caller supplies `snapshot.Settings` from validated request fields. It does not validate ignored
  saved connection fields. Automatic Source snapshots load their own saved settings.
  `Run(ctx, snapshot, automatic)`
  uploads and records task receipts/notifications. Its repository association survives request exit.
- `Wire()` registers ConfigurationCommitted and shutdown handling (already listed by server.NewApp).
  The lazy worker debounces for 1s, retaining only the latest Source reference; snapshots are loaded
  outside commit locks. `WaitIdle(ctx)` fences uploads and their receipt callbacks without cancelling I/O.
- Settings retention defaults/limits and `RetentionValue` are shared with api/settings; invalid imported
  retention uses defaults. `ExpiredBackups` preserves the latest N plus one newest file per recent day.
  Zero latest retention disables deletion; cleanup failures never invalidate a successful upload.

## Helpers from a package that is not ported yet

If you need a small helper that belongs to another work package still marked `todo`/`wip` in
`parity/packages.md`, put a minimal private copy in your package in a file named `compat.go`, mark it
`// TODO(port): replace with <package>` and list it in the hand-back. Do not create or edit another
package's directory.

## Working next to other agents

**Never run `rt clean` from the shared worktree `~/program/configflow-go`**: it deletes the shared remote copy
on the test server (gitignored build outputs and generated test data of every agent). Only run it inside
your own private worktree, right before removing that worktree.

Several agents work in this same worktree at once. Touch only your package directories and your rows of
`parity/routes.tsv`. Stage with explicit paths (`git add server/internal/<yours>`), never `git add -A` or
`git commit -a`. If `git` reports an index lock, wait a few seconds and retry. Commit early and often
(after each coherent, compiling step) so nothing is lost if you are interrupted. Run only your own
packages' tests: `rt -- bash -lc 'cd server && go vet ./internal/<yours>/... && go test -count=1 ./internal/<yours>/...'`.
Another agent's half-written package may sit in the tree; never "fix" it, and do not run `go test ./...`.
Do the work yourself: do not spawn sub-agents or forks (parallel helpers exhaust the shared usage
window within an hour and leave half-written files behind).
`rt --back <path>` copies that path from the server over your local files. Use it only for a single
generated file or a directory inside your own package (`--back server/internal/<yours>/testdata`), never
for `server/internal` or `server`: it would overwrite other agents' newer local edits.
