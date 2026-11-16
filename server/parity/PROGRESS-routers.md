# PROGRESS — domain `routers`

Domain `routers` (WORKFLOW.md): router core, iKuai, RouterOS core/policy, router DNS/IPv6, router publish/rule sync, clients/split (old packages #32–#38). **52 Python files, 17574 lines, 17 modules** (equal to the `routers` totals in `FILES.md`) — 16 missing, 0 partial, 0 ported, 1 verified (R02). Only reviewers set `verified`.

Planning audit: 2026-11-16, worktree `configflow-go`, branch `go` at 7b1b90b5 (plus uncommitted work of the C24/C25 agent in `internal/oxidns`). The Go tree was searched directly (`grep -ri 'routeros|ikuai|router_split|routers_of|router' server/internal`): **no package ports any routers file**; `internal/server` wires no routers package. What exists is listed under "Go code that already touches this domain" below; nothing from the old `progress/` files covers #32–#38.

## Conventions

Same as `PROGRESS-config.md` / `PROGRESS-agents.md`: status `missing` / `partial` (exact list of what is missing) / `ported` (code and tests exist, not reviewed) / `verified` (reviewer only). Test counts are `def test*` per file. Routes are rows of `server/parity/routes.tsv`. A test file is listed under the module that owns its subject; when it also drives later modules, its replay rows for those modules stay *pending* until they are ported (pattern of `internal/oxidns`).

`backend/routes/routers.py` (820 lines) is split by line ranges (contiguous groups, covering the whole file):

| Lines | Functions | Module |
|---|---|---|
| 1–36, 63–99, 230–385, 755–768 | imports, `DRIVERS`, `_label`, `_capture_sync`, `_driver`, `_not_found`, `_save_items`, `_agent`, `_dns_settings`, `_inspect`, `_inspect_state`, `_ipv6_host`, `_return_cleanup`, `_now`, `_return_routing` (243 lines: the inspection service other modules call — `pending`, `rule_sync`, `client_discovery`, `setup_completion` import `_driver`/`_inspect`/`_agent`) | R13 → `internal/routers` |
| 37–62, 100–229, 386–754, 769–820 | `_manual_sync_lock`, `_router_settings_lock`, every `@routers_bp.route` handler, `_proxy_gateways`, `_no_store`, `_log_release`, `_device_route_change` (577 lines) | R15 → `internal/api/routers` |

**HR** = high risk: a defect can break a user's network (RouterOS/iKuai publish, DNS guard, DHCP DNS, IPv6 fake-IP/PD guard, policy routing, rule sync, route editor, fixed IP, Mihomo full-TUN marker). HR modules need a **VM lab test after the maintainer agrees** (PVE lab with RouterOS CHR and iKuai VMs, never production) before the domain is done. 16 of 17 modules are HR; only R16 (client discovery, read-only) is not.

### Go packages (proposed)

| Package | Contents | Imports (must stay acyclic) |
|---|---|---|
| `internal/routerdoc` (new leaf) | `store.routers_of` (+ `find_router`), all of `routers/lan_clients.py`, `client_fixed_ip.validate_bindings` (message only) | jsonx, pystr only — so `repo` and `notifications` can import it and drop their compat copies |
| `internal/routers` | every other `backend/routers/*.py`, the R13 part of `routes/routers.py`, `routes/tasks.py::_router_rows` | repo, notifications, tasks, taskstatus, logx, rulefetch, mosdns, convert, oxidns, agents (manager), agents/monitor, agents/diag, routerdoc, routersplit; **never** gateway, agents/ipv6, setup, api/* |
| `internal/routersplit` (new) | `common/router_split.py` | jsonx, yamlx, convert (DefaultMihomoTemplate), repo, `internal/gateway/gwbase` |
| `internal/gateway/gwbase` (new leaf, see "Cycle") | `LinuxGatewayError`, `TUN_FILTER_KEYS`, `linux_gateway_draft.RESTORE_KEY` | jsonx only |
| `internal/ippresence` (new leaf) | `common/rule_ip_presence.py` | mosdns, convert, rulefetch, repo |
| `internal/clients` (new) | `common/client_devices.py`, `client_discovery.py`, `client_discovery_cache.py`, `client_fixed_ip.py` | routers, routerdoc, oxidns, repo, agents |
| `internal/api/routers` (new) | R15 handlers + `routes/upstream_router_pending.py` (endpoint `agents.upstream_router_pending`, registered from this package) | routers, gateway (A20), agents/monitor |
| `internal/api/clientdevices` (new) | `routes/client_devices.py` | clients |
| `internal/routers/routerfake` (new, test support) | in-memory RouterOS REST + iKuai fake device (port of `routeros_test_helpers.py` and the session fakes), usable as a loopback `httptest.Server` | routers (digest/encode helpers) |

Python's `backend/routers` is one big import cycle (most cross-module imports are lazy, inside functions). In Go it is one package, so each module compiles alone only if forward calls into later modules go through **collaborator variables** in `internal/routers/pending.go` (`var buildGuard = func(...) {... pending("R09", "dns_guard.build") ...}`), replaced by the later module's porter — the pattern `internal/oxidns/pending.go` uses. The replay counts rows that hit a pending variable as *pending*, never as passed.

## Module table

| Id | Module | Py lines | Status | HR | Prereqs | Routes | Go package(s) |
|---|---|---|---|---|---|---|---|
| R01 | router models, store, DHCP DNS, desired state | 969 | missing | HR | C04, C05, C21, C32¹ | – | internal/routers, internal/routerdoc |
| R02 | router split (full TUN marker), rule IP presence | 381 | verified | HR | C04, C12, C14, C18, C21; C36² | – | internal/routersplit, internal/gateway/gwbase, internal/ippresence |
| R03 | RouterOS NAT assessment, iKuai IP groups, drift report | 1499 | missing | HR | R01 | – | internal/routers |
| R04 | RouterOS REST driver, batch writes | 1415 | missing | HR | R01, R03; shared requests client³ | – | internal/routers |
| R05 | iKuai driver | 1179 | missing | HR | R01, R03; shared requests client³ | – | internal/routers |
| R06 | device route editor (RouterOS/iKuai static routes) | 684 | missing | HR | R04, R05 | – | internal/routers |
| R07 | RouterOS NAT address lists, address-list sync, test helpers | 1422 | missing | HR | R03, R04 | – | internal/routers, internal/routers/routerfake |
| R08 | RouterOS policy routing (plan, write, view) | 1349 | missing | HR | R07, C05, C26 (`normalize_policies`, present) | – | internal/routers |
| R09 | forced DNS (DNS guard), IPv4 same-port forward hint | 773 | missing | HR | R04, R07 | – | internal/routers |
| R10 | IPv6 policy, IPv6 fake-IP, IPv6 rule-set routes | 1247 | missing | HR | R04, R09 | – | internal/routers |
| R11 | IPv6 PD guard, IPv6 DNS, RouterOS IPv6 readiness | 1161 | missing | HR | R10 | – | internal/routers |
| R12 | release plan and staged execution | 1220 | missing | HR | R03–R11, A01 (return routing calls, ready) | – | internal/routers |
| R13 | inspection service, return advisory, pending reminders | 713 | missing | HR | R12, C07 (`RecordRouter`, ready), A01, A06 (ready); A23⁴ | – | internal/routers |
| R14 | automatic rule sync | 1043 | missing | HR | R13, C14 (ready), C07 (`tasks`, ready) | – (handlers in R15) | internal/routers |
| R15 | router HTTP routes, policy workspace, upstream-router reminder route | 656 | missing | HR | R14, C06, C22 (ready), C31, C34; A20⁵ | 21 (0 done) | internal/api/routers, internal/routers |
| R16 | client devices and discovery | 786 | missing | | R13, A01, C26, C31 | – | internal/clients, internal/routers |
| R17 | client fixed IP and client-device routes | 1077 | missing | HR | R16, R06, R14, C06 (JWT secret), C36², A13⁶ | 5 (0 done) | internal/clients, internal/routers, internal/api/clientdevices |

¹ `desired.compute_desired` loads rule content through `routes.rules.get_ruleset_content` (C32, a route file). Until C32 exists, a collaborator variable; C32 should export the function from a non-API package (also needed by agents A19). ² `setup_flow.profile_sessions` (C36 imports routers, so routers declares a hook; see contract). ³ Coordinator decision needed before R04/R05 (see questions). ⁴ `_inspect_state` → `agents.ipv6_readiness._request` (A23 imports routers → hook). ⁵ `/api/routers/gateway-forwarding` calls `agents.linux_gateway.gateway_forwarding_entry/persist_gateway_forwarding` (A20). ⁶ `routes.agents._public_deployment` (A13; collaborator variable until then).

Totals: 17 modules, **17574 Python lines, 52 files** (= FILES.md `routers`: 52 files, 17574 lines; checked with `wc -l` on the FILES.md list in this worktree). Routes: **26 rows** of routes.tsv (20 `routes.routers`, 5 `routes.client_devices`, 1 `routes.upstream_router_pending`), 0 done. Python tests owned here: **59 files, 959 tests**.

Module sizes: all within 300–1500 lines (R02 381 is the smallest — kept separate because config C18/C27 and agents A19–A22 need it early; R03 1499 the largest).

## File → module list (all 52 files)

| File | Lines | Module |
|---|---|---|
| `backend/common/client_devices.py` | 63 | R16 |
| `backend/common/client_discovery.py` | 204 | R16 |
| `backend/common/client_discovery_cache.py` | 88 | R16 |
| `backend/common/client_fixed_ip.py` | 281 | R17 |
| `backend/common/router_split.py` | 302 | R02 |
| `backend/common/rule_ip_presence.py` | 79 | R02 |
| `backend/routeros_test_helpers.py` | 109 | R07 (Go test support `routerfake`) |
| `backend/routers/__init__.py` | 1 | R01 |
| `backend/routers/base.py` | 371 | R01 |
| `backend/routers/desired.py` | 163 | R01 |
| `backend/routers/dhcp_dns.py` | 158 | R01 |
| `backend/routers/dns_guard.py` | 589 | R09 |
| `backend/routers/forward_hint.py` | 184 | R09 |
| `backend/routers/ikuai.py` | 1179 | R05 |
| `backend/routers/ikuai_clients.py` | 142 | R16 |
| `backend/routers/ikuai_fixed_ip.py` | 261 | R17 |
| `backend/routers/ikuai_groups.py` | 684 | R03 |
| `backend/routers/ikuai_routes.py` | 237 | R06 |
| `backend/routers/ipv6_dns.py` | 232 | R11 |
| `backend/routers/ipv6_fakeip.py` | 348 | R10 |
| `backend/routers/ipv6_pd_guard.py` | 635 | R11 |
| `backend/routers/ipv6_policy.py` | 529 | R10 |
| `backend/routers/ipv6_readiness.py` | 294 | R11 |
| `backend/routers/ipv6_routes.py` | 370 | R10 |
| `backend/routers/lan_clients.py` | 93 | R01 (→ `internal/routerdoc`) |
| `backend/routers/pending.py` | 260 | R13 |
| `backend/routers/plan.py` | 807 | R12 |
| `backend/routers/policy_workspace.py` | 52 | R15 |
| `backend/routers/release.py` | 413 | R12 |
| `backend/routers/report.py` | 306 | R03 |
| `backend/routers/ros_address_lists.py` | 297 | R07 |
| `backend/routers/ros_clients.py` | 289 | R16 |
| `backend/routers/ros_policy.py` | 520 | R08 |
| `backend/routers/ros_policy_view.py` | 47 | R08 |
| `backend/routers/ros_policy_write.py` | 782 | R08 |
| `backend/routers/ros_return_advisory.py` | 210 | R13 |
| `backend/routers/ros_values.py` | 27 | R01 |
| `backend/routers/route_editor.py` | 164 | R06 |
| `backend/routers/routeros.py` | 816 | R04 |
| `backend/routers/routeros_batch.py` | 599 | R04 |
| `backend/routers/routeros_fixed_ip.py` | 382 | R17 |
| `backend/routers/routeros_nat.py` | 509 | R03 |
| `backend/routers/routeros_nat_list.py` | 1016 | R07 |
| `backend/routers/routeros_routes.py` | 283 | R06 |
| `backend/routers/rule_sync.py` | 474 | R14 |
| `backend/routers/rule_sync_rules.py` | 214 | R14 |
| `backend/routers/rule_sync_store.py` | 90 | R14 |
| `backend/routers/rule_sync_write.py` | 265 | R14 |
| `backend/routers/store.py` | 156 | R01 |
| `backend/routes/client_devices.py` | 153 | R17 |
| `backend/routes/routers.py` | 820 | R13 (243, lines above) + R15 (577) |
| `backend/routes/upstream_router_pending.py` | 27 | R15 |

(`FILES.md`'s module column for these rows is still empty; the coordinator copies this column there.)

## Go code that already touches this domain (audit 2026-11-16)

None of it is a port of a routers file; listed so porters replace or use it instead of duplicating:

- `internal/repo` (C03/C04, verified): the router commit guard (`routerCommitGuard`, `routerRecordsOf`, `RouterIdentityFields`/`RouterConnectionFields`/`RouterManifestFields`), `SaveRouterSettings`, `SaveRouterPublication` + `RouterPublication`, clone drops `routers`. **Use as-is.** Hooks declared for routers: `Hooks.JobLock`, `Hooks.RedactRouters` (contract below). `repo/compat_oxidns.go` holds private copies of `lan_clients.ipv4`, `normalize_mac` (ASCII forms only) and `client_fixed_ip.validate_bindings` → replace with `internal/routerdoc` (R01/R17).
- `internal/notifications` (C07): `RecordRouter`, `RouterSignature`, router release observations, `Sources.Summarize` (= `routers.pending.summarize`, unset → error "routers.pending.summarize is not wired"); `notifications/compat.go::RoutersOf` copy → replace with `routerdoc.RoutersOf` (R01). Router notice tests already ported there (3 of the 4 router cases of `test_notifications.py`).
- `internal/tasks` (C07): slot `SlotRouters = "router_rule_sync"` (unset → `/api/tasks` and `GET /api/notifications` stay unregistered), `*tasks.Conflict` (= RouterError of `start_job`, 409); `api/tasks` already routes category `router_rule_sync` to the runner.
- `internal/convert` (C18): `MihomoHooks.IsFullTun` (unset → false without a `router_split` marker, else `*HookMissingError`), `TakeoverMode`, `ApplyLinuxGateway` (A19's, which must call `router_split.check_generation` first).
- `internal/oxidns/pending.go` (C22): ~~`classicalIPLines` stub~~ — replaced by R02 with the `oxidns.SetClassicalIPLines` hook (`hooks_routers.go`), registered by `ippresence.Wire`.
- `internal/oxidns/devicepolicy.go`: `NormalizeClientRange`, `NormalizePolicies` (used by R08/R16); `oxidns.GenerateOxidnsConfig` (R15 policy preview); `mosdns.ConvertRuleContentForMosDNS` (R01/R02); `rulefetch.RuleCacheReadPath`/`ReadValidatedRuleCache`/`WriteValidatedRuleCache`/`ValidateRemoteRuleContent` (R02/R14); `convert.RulesetFormat`, `convert.DefaultMihomoTemplate` (R02); `agents.Manager.GetAgentByID`, `ReturnRoutingStatus/Apply/Remove` (R12/R13); `agents/monitor` `MonitorError` (R13/R15); `agents/diag.EventMessage` (R15); `substore` requests-like client with requests 2.31 exception classes (`RequestError{Class}`, per-read deadlines) — candidate for the shared router HTTP client (question 1).
- `internal/agents/compat.go`: `HASummary` keeps the `routers` key of an HA job (A18's; no router logic).

## R01 — router models, store, DHCP DNS, desired state

**Status: `missing` (porting in progress, see "Port log" below)** · HR · prereqs: C04, C05, C21 (ready), C32¹ · Go: internal/routers, internal/routerdoc, internal/netipx

Port log (interim, 2026-11-16): code committed — 650c8c65 routerdoc (+ repo/notifications switched), 37f783f3 routers core, 5f89caeb switch to net/netip (pyip dropped, thin `internal/netipx`; corpus 29742 rows 0 differ). Next: record plugin `internal/routers/testdata/routers_record.py` (written, not yet run), Go replay, corpus, routerfake skeleton.

Files:
- `backend/routers/__init__.py` (1), `base.py` (371), `ros_values.py` (27), `lan_clients.py` (93, → routerdoc), `dhcp_dns.py` (158), `store.py` (156), `desired.py` (163)

Tests: `test_routers.py` (16; drivers/report/API cases replay as R03–R05/R15 land), `test_router_cidr_index.py` (8; NAT/policy halves → R03/R08), `test_router_source_activation.py` (3; policy half → R08), `test_router_dhcp_dns.py` (13; plan/release halves → R12)

Routes: none.

Evidence: no Go code (compat copies only: `notifications.RoutersOf`, `repo` lan_clients/bindings — replace them).

Notes for porter: `base.py` dataclasses (`RouterState`, `RouteEntry`, `NatBypassEntry`, `DhcpDnsState`, `RouterInfo`, …), `RouterError`/`RouterConflictError`/`RouterRollbackError`, `StaticRouteIndex`/`SupernetIndex` CIDR indexes (bisect/heap; corpus against Python `ipaddress`), `as_dict`, `managed_comment`/`is_managed`, `retire_policy_routes`. `store.normalize` validation messages, `redact_routers` → registers `repo.Hooks.RedactRouters`; `public_view`, `EDITABLE_FIELDS`, `RouterValidationError` are used by C36. `store` lazily needs `dns_guard.DEFAULTS/normalize_settings` (R09) and IPv6 settings (R10/R11) → collaborator variables. `desired.fake_ip_network`, `merge_sources`, `rule_source_issue`, `compute_desired` (content loader = C32 collaborator; MosDNS IP part conversion is ready). `lan_clients.ipv4`/`normalize_mac` must follow Python `int(part, 16)` exactly (full-width digits, signs, whitespace — the repo copy does not). Build the domain's **record plugin** (`internal/routers/testdata/routers_record.py`, see "Testing approach") here; later modules extend its function list.

## R02 — router split (full TUN marker), rule IP presence

**Status: `verified`** (review R02, 2026-11-16, functional-parity standard; HR lab test still pending) · HR · prereqs: C04 (bindings), C12 (`RulesetFormat`), C14 (rule cache), C18 (`DefaultMihomoTemplate`), C21 (MosDNS IP part); hook for C36 · Go: `internal/routersplit`, `internal/gateway/gwbase`, `internal/ippresence`

Files → Go:
- `backend/common/router_split.py` (302) → `routersplit/routersplit.go` (+ `pyhelpers.go`: Python dict/list/truthiness/AttributeError/TypeError helpers over `*jsonx.Object` and `*yamlx.Mapping`, `copy.deepcopy` with memo): `MarkerKey`, `GatewayMarker`, `FullTun()`, `RouteKeysRemoved`, `Messages` + `MessageCodes` + `Code*`, `LegacyRestoreBlocked`, `FullTunNoRestorePoint`, `FullTunRestoreOverwrites`, `GeneratedTunMissing`, `GeneratedTunFiltered` (`{keys}` placeholder), `RouterSplitError{gwbase.LinuxGatewayError; Code; Status}` (`As` → `**gwbase.LinuxGatewayError`, `NewRouterSplitError(msg, code, 0=409)`), `ReadMarker` (`StateAbsent/Valid/Invalid`), `IsFullTun`, `RouteRuleSetIDs`, `TunFilters`, `FullTunDocument`, `FullTunBaseYAML`, `IsFullTunDocument`, `CheckGeneration`, `RequireFullTunGenerated` (ValueError = `*PyError`), `LegacyContext`, `NeedsContext`, `BaseIsFullTun`, `LegacySource`, `PublishLegacySource`, `LegacyGatewayAgentIDs`, `GatewayWasUsed`, `ModeFromContext`, `RouterSplitState(ctx, r, profileID, config, context any)` (nil or nil `*jsonx.Object` = None). Config/context/doc parameters are `any` so Python's crashes on malformed values are reproduced (`*PyError{Class, Message}`).
- `backend/common/rule_ip_presence.py` (79) → `ippresence/ippresence.go`: `ClassicalIPLines(ctx, item, config any) (nil|bool, error)` (repository from ctx as in rulefetch), memo (1024 entries, content sha256 / cache path+mtime_ns+size, cleared when full; `ClearMemo`).
- `linux_gateway.LinuxGatewayError`, `TUN_FILTER_KEYS`, `linux_gateway_draft.RESTORE_KEY` → `gateway/gwbase/gwbase.go` (decision 2; exactly these three, no logic). A19 adopts it.

Hooks:
- Registered: `ippresence.Wire` (one line in `server.wirePackages`) → `convert.RegisterMihomoHooks(MihomoHooks{IsFullTun})` and `oxidns.SetClassicalIPLines`. Wire lives in `ippresence` (imported by nobody) so `routersplit` — which `internal/gateway` will import — does not import `internal/oxidns`.
- `oxidns` hook (C27 had not declared one): `internal/oxidns/hooks_routers.go` (new file) — `ClassicalIPLinesFunc func(ctx, item, config any) (any, error)`, `SetClassicalIPLines`; `classicalIPLines` calls it with `context.Background()` (Python uses the global repository; `hasIPPart` has no request context) and raises its error; unregistered it stays pending exactly like C22's stub, which was deleted from `pending.go` (minimal edit). `internal/oxidns/routers_hooks_test.go` (package `oxidns_test`, init) registers the real hook with a private temp repository for the oxidns replays. C27 may move the hook declaration into its own file later; keep the signature.
- Declared, fail closed: `routersplit.SetProfileSessions(ProfileSessionsFunc)` = `setup_flow.profile_sessions(repository, profile_id)` for C36; unset → `*routersplit.HookMissingError` (reached only by `LegacyContext`, `PublishLegacySource` with a profile id and an unmarked config, `RouterSplitState` without context).

Effect on config replays (2026-11-16):
- C18 (`convert`): `IsFullTun` is only called by `TakeoverMode`, which neither the C18 corpus nor its recorded replay exercise (only the hand-ported `TestTakeoverMode`, with its own hooks); no C18 row hit `*HookMissingError` for a router_split marker (generation markers go through `ApplyLinuxGateway`, answered by the C18 replay from the record — A19's). Ran `TestMihomoCorpus` (13908 rows) and `TestMihomoRecordedPythonTests` (1566 calls) with `ippresence.Wire()` registered and without: **0 differ both**. `ippresence.TestWireRegistersIsFullTun` shows `TakeoverMode` answering `full_tun` for a marked profile after Wire.
- C22/C23/C24 (`oxidns`): with the hook, the one `routers` pending row of `recorded.jsonl.gz` (a `routing_groups` content-aware call) is compared and identical (routing_groups 6807 → 6808 identical, pending −1, 0 differ); corpus/c23/c24 files have no classical rows (unchanged, 0 differ).

Tests (all on the test server):
- **Record & replay** `routersplit/testdata/r02_record.py` (pytest plugin, R02-specific; R01's shared `routers_record.py` did not exist yet — merge later) → `r02_recorded.jsonl.gz`: every top-level call of the 18 public router_split functions and `classical_ip_lines` during the whole backend suite (12552 passed), with collaborator answers (`profile_sessions`, `get_profile_agent_bindings`, `get_profile` in call order; for classical_ip_lines a snapshot of the rules directory + stored rule library, memo cleared per call). **3119 distinct calls from 1458 tests in 80 files: router_split 2595 identical (1200 tests, 70 files), classical_ip_lines 524 identical (258 tests, 12 files; 211 false, 204 true, 109 None); 0 differ, 0 not expressible, 0 unsupported.** Replays: `routersplit/recorded_test.go` `TestRecordedPythonTests` (answers served through package seams `agentBindings`/`getProfile`/`sessionsOf`; Go must make the same collaborator calls) and `ippresence/recorded_test.go` (fresh repository per row with the snapshot).
- **Random corpus** `routersplit/testdata/gen_r02.py` (real Python through the recorder's dispatcher; synthetic values) → `r02_corpus.jsonl.gz`: 6000 router_split rows (malformed markers incl. bool/float/str versions, 1024/1025 ids, controls, 257-char ids; gateway settings; base YAML with anchors/aliases, non-string keys, timestamps, binary, merge keys, 120-deep nesting, invalid YAML, non-strings; routers topologies incl. unhashable proxy ids; contexts with str/dict/int agent id lists; sessions; bindings incl. ProfileNotFound and OSError answers) + 2000 classical_ip_lines rows (random libraries/declarations, inline and cached contents: payload YAML, scalar payload TypeError, BOM, invalid UTF-8, CR/LF/U+2028/U+0085 separators, mrs, legacy and by-id cache names, malformed `rule_library`): **8000 rows (1155 raising), 0 differ, 0 not expressible** (`TestCorpus` in both packages).
- **Hand-ported** (`routersplit/ported_test.go`, `ippresence/ported_test.go`): errors.As/`Code`/`Status` of RouterSplitError; ProfileSessions fail-closed then registered (real repo); frontend copy of `MESSAGES` + `LEGACY_RESTORE_BLOCKED` (`test_frontend_keeps_the_same_router_split_copy`, router_split half); alias bytes of `full_tun_base_yaml` (reference from Python); cache memo keyed by path+mtime+size (same size+mtime keeps the memoized answer); content memo cleared at 1024; Wire → `TakeoverMode`.

Coverage map (Python test file → how covered):
- `test_router_split.py` (24): replay 21 tests (186 rows); `test_frontend_keeps_the_same_router_split_copy` hand-ported (router_split half; FORWARDING_* constants → A20); `test_rule_set_id_validation_is_shared_with_gateway_selection` → A19 (`linux_gateway.validate_rule_set_ids`); `test_forwarding_agent_version_gate` → A20 (`agents.linux_gateway`). The session-lock aspect of `test_session_reads_never_wait_for_a_wizard_step` → C36 (its `legacy_context` rows are replayed); clone/export/import halves are repo (C04, verified).
- `test_router_split_regression.py` (4): replay 3 tests (23 rows); `test_single_service_scripts_match_the_previous_release` → installers (agents), no router_split call.
- `test_oxidns_ip_probe.py` (9, C27-owned): replay of `classical_ip_lines` (15 rows from 5 tests); the cross-call memo of `test_cache_change_is_noticed_through_the_memo` (the replay clears the memo per call) is hand-ported as `ippresence.TestCacheMemoIsKeyedByPathMtimeAndSize`; `test_native_legacy_empty_ip_part_is_recognised_and_dropped_on_rebuild` → C27; `test_classical_set_without_ip_lines_writes_no_ip_group_or_probe`, `test_ip_groups_that_all_match_the_fallback_skip_the_probe`, `test_ip_group_away_from_fallback_still_does_not_probe`, `test_probe_needed` → C27 (generation/probe logic, not R02 functions).
- Other domains' files, router_split rows replayed (deferred for their own subjects): `test_linux_gateway_draft.py` 413, `test_setup_flow.py` 283, `test_gateway_installation.py` 264, `test_gateway_onboarding.py` 204, `test_dialer_rendered_bundle.py` 132, `test_dialer_activation_shadow.py` 96, `test_dialer_provider_content.py` 82, `test_gateway_publish_compatibility.py` 68, `test_linux_gateway.py` 50, `test_ipv6_verification.py` 50, `test_split_layout_migration.py` 47, `test_dialer_standalone_activation.py` 47, `test_linux_gateway_agent_bridge.py` 19, … (70 files in all; `check_generation` 1294 rows come from generation through `apply_linux_gateway`). classical_ip_lines rows: `test_oxidns_routing_index.py` 439, `test_oxidns_domain_only_native.py` 18, `test_proxy_group_dns_follow.py` 16, `test_oxidns_common_routing.py` 15, `test_profile_rule_routes.py` 11, + 7 files.
- Differential: no routes (none in this module).

Differences from Python / limits (documented, not seen in suite or corpus):
- Base YAML that yamlx cannot hold — recursive aliases, `!!set`/`!!omap`/`!!pairs`: the readers (`_base_document` → `BaseIsFullTun`, `RouterSplitState`, `ModeFromContext`) load them as `*yamlx.Unrepresentable` placeholders and answer like Python (review R02, edge rows); only an *empty* `!!set` as a TUN filter value counts as a filter in use (Python: falsy). `FullTunBaseYAML` (a writer) still returns `*yamlx.UnrepresentableError` for them (Python would dump `!!set`/anchors) — limit of the yamlx helper, which is not extended (PORTING functional-parity block). Non-string keys are supported (`*yamlx.Mapping`, kept through deepcopy and dump).
- `rule_cache_read_path` raising inside Python's legacy-name loop (a library entry that is not a dict → AttributeError; a non-string name → TypeError → None) cannot happen in Go's `rules.RuleCacheReadPath` (Go skips/compute a path). Unreachable with real data: the repository refuses such library entries (`共享规则仓库名称必须非空且唯一`, `rule_library 中每一项必须有 ID`, checked with Python in review R02), so they are left out of the edge corpus. Repository errors are classified: `store.ErrStorage` (OSError) and `repo.IsValueError` → None like Python's except, others propagate.
- A content string with lone surrogates (Python `text.encode()` UnicodeEncodeError) cannot occur in Go strings.
- `ProfileSessions` is `[]*jsonx.Object` (Python's list of dicts); context dicts from callers are `any`.

Commits: 2d72718b (port, gwbase, hooks, wiring), 25b33e7a (record & replay), dd45397d (corpus, hand-ported tests), d60bd076 (oxidns replays with the hook); review be8e03c3.

Review R02 (2026-11-16, adversarial; standard switched to functional parity mid-review, 35817a74):
- Walked every function/branch of `router_split.py` and `rule_ip_presence.py` against Go (`read_marker` key set/`type is int`/id bounds and dedupe, `_gateway_enabled` `is True`, `tun_filters`, `full_tun_document` deepcopy-then-get order, setdefault for `stack` only, key positions, `_load_base` strip/default template/except classes, `_full_tun_shape`, `check_generation`, `require_full_tun_generated` format, `_session_context` (empty latest dict), `legacy_context` evaluation order + KeyErrors, `_base_document` non-str → AttributeError `read`, `_router_ids`, `legacy_source`, `publish_legacy_source`, `legacy_gateway_agent_ids` set hashing, `gateway_was_used` `in` on str/list/dict/number, `_state` issue order, `router_split_state` None handling; `_declaration`, `source_type` default, memo keys/limit/lock, `except (OSError, TypeError, ValueError)` scope, utf-8-sig, `read_validated_rule_cache` outside the try). All Chinese messages byte-identical.
- New edge corpus `testdata/gen_r02_edge.py` → `r02_edge.jsonl.gz` (2522 rows from real Python, 691 raising): marker version True/1.0/"１"/10**20, ids with 256/257 emoji and CJK, C0/DEL/C1/U+2028/U+3000, 1024/1025 ids, dedupe order; filter values 0/""/[]/{}/[0]/" "; documents with null/list/str tun, `stack: null`; base YAML: BOM, U+3000/U+0085-only, NUL, empty-list/date/dict aliases, shared tun alias, invalid dates (ValueError), huge ints, .inf/.nan, YAML 1.1 ints, emoji keys, 200-char lines, multi-doc, merge keys, complex keys, non-string keys, tags, timestamps, 300/600-deep nesting, recursive aliases, !!set/!!omap/!!pairs; router/context/session/binding shapes (1/True/1.0 ids, unhashables, strings, dicts); gateway_was_used over 14 mihomo shapes × 6 agents × 5 sources; classical_ip_lines: legacy-name and by-id caches, library_rule_id 0/False/True/1.0/""/list/dict, url int/bool/list/dict/float/mrs variants, source_type variants, full-width/CJK/emoji/U+2028/FS/GS/RS/VT contents, double BOM, payload YAML shapes, invalid UTF-8 (surrogates, overlong, >U+10FFFF), config/item shapes. **0 differ** (routersplit 2323, ippresence 199); `full_tun_base_yaml` compared structurally (anchors not compared, functional parity).
- Findings, all fixed in be8e03c3: (1, medium) base YAML with a recursive alias or `!!set`/`!!omap`/`!!pairs` made `router_split_state`/`base_is_full_tun`/`mode_from_context` fail where Python answers (`KeepUnrepresentable` in `baseDocument`, placeholder truthy); (2, low) a classical rule set whose stored `url` is a list/dict (the repository accepts it): `convert.RulesetFormat` raised AttributeError where Python's lru_cached `urlsplit` raises TypeError, so `classical_ip_lines` raised (OxiDNS generation error) instead of answering None — `convert/common.go` now returns TypeError for unhashable urls (convert + oxidns tests green).
- Not findings (no functional difference): 330–400-deep base YAML raises RecursionError in Python's deepcopy/dump but works in Go (depends on Python's stack depth; 300 and 600 agree); anchors in `full_tun_base_yaml` output.
- Hooks: `SetProfileSessions` fails closed (`*HookMissingError`) exactly where Python calls `profile_sessions`; `ClassicalIPLines` takes ctx; the oxidns hook passes `context.Background()` because `internal/oxidns` is context-free — never cancelled, so rule 4b holds. Cross-module note for oxidns (not an R02 defect): Python's `get_shared()` inside a repository transaction sees the working copy (thread-local); Go's Background read sees the committed library, which matters only for the legacy/by-id cache migration branch of `rule_cache_read_path` when OxiDNS routing is computed inside a transaction that just added the library entry (e.g. rules add-with-library + DNS assignment); if oxidns ever gains a ctx, pass it to the hook.
- Coverage map re-checked against the Python test files (deferrals to A19 `validate_rule_set_ids`, A20 `FORWARDING_*`, C36 session lock, C27 probe/generation, installers exist as named).

Left: HR lab test (after the maintainer agrees). Later: merge `r02_record.py` into R01's `routers_record.py` if wanted; C36 registers `SetProfileSessions`; A19 imports `gwbase` (and calls `routersplit.CheckGeneration` first in `apply_linux_gateway`); HR lab test of full-TUN generation through C18/A22 (after the maintainer agrees).

Reviewer hints: walk `ReadMarker` (`type(version) is int`, exact key set, `dict.fromkeys`), `FullTunDocument` (setdefault for `stack` only, new keys appended, `tun: null` → new `{}`), `GatewayWasUsed` (`in` on str/list/number), `LegacyGatewayAgentIDs` (set hashability: 1/1.0/True equal, unhashable TypeError order), `baseDocument` (non-str truthy `custom_config` → AttributeError `read`), `ClassicalIPLines` (which errors Python's `except (OSError, TypeError, ValueError)` catches — `caught()`; `rule_library` shapes).

Pitfalls: `rt` serializes runs of this project ("waiting for another run"), so expect delays when other agents test. The Write tool turned a `"\ufeff"` escape into a literal BOM once — gofmt reports "illegal byte order mark".

Commands:
- edge corpus (~20 s): `rt --back server/internal/routersplit/testdata/r02_edge.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/routersplit/testdata ~/cf-test/.venv/bin/python server/internal/routersplit/testdata/gen_r02_edge.py'`
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/routersplit internal/ippresence internal/gateway; go vet ./internal/routersplit/ ./internal/ippresence/ ./internal/gateway/... ./internal/server/ && go test -count=1 -race ./internal/routersplit/ ./internal/ippresence/ ./internal/gateway/... ./internal/server/'` (last: ok; ippresence ~85 s under -race, 6 s without)
- record (~3 min): `rt --back server/internal/routersplit/testdata/r02_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/r02rec; R02_RECORD_DIR=/tmp/r02rec PYTHONPATH=.:server/internal/routersplit/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 --dist=worksteal -p r02_record -p no:cacheprovider; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/routersplit/testdata/r02_record.py merge /tmp/r02rec'`
- corpus (~10 s): `rt --back server/internal/routersplit/testdata/r02_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/routersplit/testdata ~/cf-test/.venv/bin/python server/internal/routersplit/testdata/gen_r02.py 6000'`
- C18/C22 check: `go test -count=1 -v -run "TestMihomoRecordedPythonTests|TestMihomoCorpus" ./internal/convert/` and `go test -count=1 -v ./internal/oxidns/` (for C18 with the hook: a temporary `package convert_test` file with `func init() { ippresence.Wire() }` on a private worktree).

## R03 — RouterOS NAT assessment, iKuai IP groups, drift report

**Status: `missing`** · HR · prereqs: R01 · Go: internal/routers

Files:
- `backend/routers/routeros_nat.py` (509), `backend/routers/ikuai_groups.py` (684), `backend/routers/report.py` (306)

Tests: `test_ikuai_groups.py` (40), `test_ikuai_groups_invariants.py` (4; plan/release halves → R12), `test_routeros_nat.py` (25), `test_router_route_index.py` (5), `test_routeros_static_selection.py` (16)

Routes: none.

Notes for porter: `RouteEgressIndex`, `assess_bypass`, `assess_nat`, `_egress`, `_selector_matches`, `client_source_networks`, `planned_bypass_conflicts`, `preserves_sources`; `GroupPlanner`, `canon`, `span`, `nat_target`, `verify`; `build_report`, `_networks`. `report` lazily calls ros_policy (R08), dns_guard (R09), IPv6 modules (R10/R11) → collaborator variables. The report and plan **fingerprints** are hashes over these structures: key order and number text must match Python byte for byte (they gate every publish).

## R04 — RouterOS REST driver, batch writes

**Status: `missing`** · HR · prereqs: R01, R03; shared requests client (question 1) · Go: internal/routers

Files:
- `backend/routers/routeros.py` (816), `backend/routers/routeros_batch.py` (599)

Tests: `test_routeros_permissions.py` (4), `test_routeros_route_batch.py` (23), `test_routeros_dhcp_dns.py` (18), `test_routeros_release.py` (14; release halves → R12)

Routes: none.

Notes for porter: transport is `requests.Session` with `trust_env=False`, `verify=verify_tls`, Basic auth (requests encodes user/password as latin-1 — check the non-latin-1 case against Python), `allow_redirects=False`, `json=` bodies (bytes = `json.dumps` default separators, ASCII), per-request `timeout` (requests semantics: connect and each read, not a total). Error texts carry the **requests exception class name** (`无法连接 RouterOS：ConnectTimeout`), so the Go client must classify exactly like requests 2.31 (`ConnectionError`, `ConnectTimeout`, `ReadTimeout`, `SSLError`, `ChunkedEncodingError`, …); GET failures → `RouterError`, writes → `UncertainWrite`. Keep the test seams Python tests use as overridable fields on the Go driver: `request(method, path, values, timeout)`, `list`, `transaction`, the session (see "Testing approach"). Console scripts (`/execute`): `_console_add`, `console_safe`, `_script_value`/`_script_string` (R08 file, used here) — escaping is security-relevant (fuzz with `"`, `\`, `$`, `;`, newlines, CJK, emoji, full-width). Duck-typed manifests (`hasattr(driver, 'ros_policy_manifest')`) → explicit optional fields with "set" flags. Lazy calls into R07–R11 → collaborator variables.

## R05 — iKuai driver

**Status: `missing`** · HR · prereqs: R01, R03; shared requests client (question 1) · Go: internal/routers

Files:
- `backend/routers/ikuai.py` (1179)

Tests: `test_ikuai_connection_errors.py` (7), `test_ikuai_dhcp_dns.py` (16), `test_ikuai_scale.py` (29)

Routes: none.

Notes for porter: login (`md5(pw)` + `base64("salt_11"+pw)`), session expiry and re-login, paging of every list, `encode_value`/`decode_value` (must match the iKuai UI, `test_router_release.py::test_encode_matches_ikuai_ui`), `request_bytes`/`oversized_requests`, `_http_error`, `_call_error`; the five transport classes map to five Chinese messages (SSLError, ConnectTimeout, Timeout, ConnectionError, other RequestException) and an empty body → "iKuai 没有返回任何内容…". Version gate 4.x.

## R06 — device route editor

**Status: `missing`** · HR · prereqs: R04, R05 · Go: internal/routers

Files:
- `backend/routers/route_editor.py` (164), `backend/routers/routeros_routes.py` (283), `backend/routers/ikuai_routes.py` (237)

Tests: `test_route_editor.py` (14), `test_routeros_routes.py` (24), `test_ikuai_routes.py` (24)

Routes: none here (`/routes`, `/routes/plan`, `/routes/apply` handlers are R15).

Notes for porter: `router_operation_lock(router)` (per-connection in-process lock; used by R15, R17 and agents A23) — export it. `read_routes`, `public_route`, plan/apply with conflict/rollback.

## R07 — RouterOS NAT address lists, address-list sync, test helpers

**Status: `missing`** · HR · prereqs: R03, R04 · Go: internal/routers, internal/routers/routerfake

Files:
- `backend/routers/routeros_nat_list.py` (1016), `backend/routers/ros_address_lists.py` (297), `backend/routeros_test_helpers.py` (109, test helper → `routerfake`)

Tests: `test_routeros_nat_list.py` (64), `test_nat_list_unchanged.py` (8), `test_ros_address_lists.py` (14), `test_ros_policy_bulk.py` (16)

Routes: none.

Notes for porter: `ros_address_lists.reuse(driver)` (per-preview proof cache), `digest`, `_encode`, paged `cf-rows`/`cf-digest` scripts, batch add with `cf-done:<failed>:<first>`; `nat_resources`, `member_update`, `migration`, `inspect_rule`, `verify`. `routerfake` is the Go port of `routeros_test_helpers.py` (script interpreter for route/NAT adds, address-list digests/pages/batches, `.query` prints) plus the state model of the session fakes; cross-check it against the Python helper on every `/execute` script found in the recordings.

## R08 — RouterOS policy routing

**Status: `missing`** · HR · prereqs: R07, C05, C26 (`normalize_policies`, present in `internal/oxidns`) · Go: internal/routers

Files:
- `backend/routers/ros_policy.py` (520), `backend/routers/ros_policy_write.py` (782), `backend/routers/ros_policy_view.py` (47)

Tests: `test_ros_policy.py` (32), `test_ros_policy_activation.py` (6), `test_ros_policy_advisories.py` (9), `test_ros_mode_transitions.py` (13), `test_ros_table_flags.py` (12), `test_ros_preview_freshness.py` (2), `test_policy_editors.py` (6; not claimed by config — its `/api/rule-library/manual/draft-content` route half → C33)

Routes: none.

Notes for porter: `inspect_desired`, `snapshot`, `owned_rows`, `digest`, `config_row`, `network_overlap`, `subtract`, write steps/journal/rollback (`run_steps`, `_create`, `_delete`, `_restore_step`, `_writable`, `_read`), table/FIB flag normalisation, `read_policy_objects`. `ros_policy.digest` is used by R15's policy workspace.

## R09 — forced DNS (DNS guard), IPv4 same-port forward hint

**Status: `missing`** · HR · prereqs: R04, R07 · Go: internal/routers

Files:
- `backend/routers/dns_guard.py` (589), `backend/routers/forward_hint.py` (184)

Tests: `test_router_dns_guard.py` (27), `test_router_forward_hint.py` (15)

Routes: none.

Notes for porter: `relevant`, `snapshot`, `build`, `report`, `verify`, `owned_hairpins`, `NETWATCH_FIELDS`, `DEFAULTS`, `normalize_settings`/`SettingsError` (also used by `store`), incremental publish (upstream 2a8cb64c, "只增删有变化的对象"); forward hint is read-only and must never enter an operation or fingerprint. Python's `test_oxidns_ha.py` (A18) also drives dns_guard.

## R10 — IPv6 policy, IPv6 fake-IP, IPv6 rule-set routes

**Status: `missing`** · HR · prereqs: R04, R09 · Go: internal/routers

Files:
- `backend/routers/ipv6_policy.py` (529), `backend/routers/ipv6_fakeip.py` (348), `backend/routers/ipv6_routes.py` (370)

Tests: `test_ipv6_policy.py` (23), `test_router_ipv6_fakeip.py` (31), `test_router_ipv6_routes.py` (16), `test_router_ipv6_manifest_safety.py` (8)

Routes: none.

Notes for porter: IPv6 address/prefix math against Python `ipaddress` (corpus: ULA, link-local, mapped, zone ids, `strict=False`, prefix lifetimes — RouterOS 7.25.1 lifetime fingerprint fix 4191cf70), manifests `ipv6_fakeip_state`/`ipv6_routes_state`, `relevant`/`settings`/`inspect`/`needs_host`.

## R11 — IPv6 PD guard, IPv6 DNS, RouterOS IPv6 readiness

**Status: `missing`** · HR · prereqs: R10 · Go: internal/routers

Files:
- `backend/routers/ipv6_pd_guard.py` (635), `backend/routers/ipv6_dns.py` (232), `backend/routers/ipv6_readiness.py` (294)

Tests: `test_router_ipv6_pd_guard.py` (8), `test_router_ipv6_pd_guard_integration.py` (11), `test_router_ipv6_dns.py` (22), `test_router_ipv6_dns_integration.py` (12). `routers/ipv6_readiness.py` has no own test file: its calls are replayed from `test_ipv6_readiness.py` (A23-owned).

Routes: none.

Notes for porter: PD guard script/scheduler on the router (prefix change on reboot — see the maintainer's cf-pd script history), `ipv6_readiness` RouterOS reader (`_list`) used by agents A23 — export it.

## R12 — release plan and staged execution

**Status: `missing`** · HR · prereqs: R03–R11, A01 (ready) · Go: internal/routers

Files:
- `backend/routers/plan.py` (807), `backend/routers/release.py` (413)

Tests: `test_router_release.py` (24), `test_routeros_release_safety.py` (12), `test_router_return_routing.py` (20), `test_router_return_allocation.py` (11)

Routes: none.

Notes for porter: `build_plan` (stages, ops, blockers, warnings, `STAGE_TITLES`, fingerprint), `execute`/`_execute`/`_verify`/`verify_nat`, `AgentReturnRouting` (Agent allocation contract, hex normalisation), rollback in reverse order. A01 manager provides `ReturnRoutingStatus/Apply/Remove`. Also used by C36 setup completion (`build_plan`) and A18 (`execute` in `test_oxidns_ha.py`).

## R13 — inspection service, return advisory, pending reminders

**Status: `missing`** · HR · prereqs: R12, C05, C07 (ready), A01, A06 (ready); A23 hook · Go: internal/routers

Files:
- `backend/routes/routers.py` (243) — lines 1–36, 63–99, 230–385, 755–768 (inspection service; see table at the top)
- `backend/routers/ros_return_advisory.py` (210)
- `backend/routers/pending.py` (260)

Tests: `test_ros_return_advisory.py` (13), `test_upstream_router_pending.py` (17), `test_router_ipv6_integration.py` (13; rule-sync half → R14)

Routes: none (the `upstream-routers` handler is R15).

Notes for porter: `Driver(router)`, `Inspect(ctx, config, router, driver, contentLoader)` (returns gateway agent, desired, state, report), `Agent`, `ReturnRouting`, `ReturnCleanup` are exported for R14, R15, R16, C36 (setup completion) — Python imports these private helpers across modules. `_ipv6_host` → hook `IPv6HostRequest` (A23). `pending.summarize` registers `notifications.SetSources{Summarize}`; `agent_routers` reads routers concurrently (ThreadPoolExecutor + `FutureTimeout` → 504 in R15) with a keyed `PendingCache` — hand-port the concurrency/timeout tests. `safely(record_router, …)` after reads: must run with `context.WithoutCancel` (rule 4b).

## R14 — automatic rule sync

**Status: `missing`** · HR · prereqs: R13, C14, C07 (`tasks`) · Go: internal/routers

Files:
- `backend/routers/rule_sync.py` (474), `rule_sync_rules.py` (214), `rule_sync_store.py` (90), `rule_sync_write.py` (265)
- plus `backend/routes/tasks.py::_router_rows` (C07's file; listed under C07 as "ported by routers")

Tests: `test_router_rule_sync.py` (27), `test_routeros_nat_list_sync.py` (6), `test_router_ipv6_routes_sync.py` (4). Deferred here from other domains: `test_router_repository_safety.py` (C03) `test_rule_sync_persists_rollback…`; `test_scheduled_tasks.py` (C15) router row of `test_router_agent_dns_follow_domain_and_backup_rows`; `test_rule_cache_identity.py` (C08) `test_all_local_consumers_keep_colliding_sources_separate` router consumer; `test_remote_rule_consumers.py` (C28) `test_ros_download_revalidates_shared_transport_response_for_each_declaration`; `test_profile_clone_router_isolation.py` (C04) receipt half; rule-sync half of `test_router_ipv6_integration.py`.

Routes: none here (`GET|PUT|POST /api/routers/<id>/rule-sync` handlers are R15).

Notes for porter: registers `repo.Hooks.JobLock` (= `rule_sync_store.job_lock`: non-blocking in-process lock + `flock` on `DATA_DIR/router-rule-sync/<token>.lock`; keep the lock file, PORTING rule 7), `tasks.SetRows(tasks.SlotRouters, …)`, `tasks.SetRunner("router_rule_sync", …)` (RouterError → `*tasks.Conflict`), `lifecycle.Loop` for `rule_sync.start_loop` (app.py). Receipts `DATA_DIR/router-rule-sync/*.json` byte-identical. `capture_manual`, `needs_baseline`, `start_job`, `paused_message` are used by R13/R15. Idle CPU history (#534–#537): keep the loop's idle gating exactly. Task status through `notifications.TaskStatus`.

## R15 — router HTTP routes, policy workspace, upstream-router reminder route

**Status: `missing`** · HR · prereqs: R14, C06, C22 (ready), C31 (`device_notes`), C34 (`sanitize_external_payload`); A20 for gateway forwarding · Go: internal/api/routers, internal/routers (policy workspace)

Files:
- `backend/routes/routers.py` (577) — lines 37–62, 100–229, 386–754, 769–820 (handlers)
- `backend/routes/upstream_router_pending.py` (27) — on the agents blueprint, endpoint `agents.upstream_router_pending`
- `backend/routers/policy_workspace.py` (52)

Tests: `test_policy_workspace.py` (17), `test_ros_api_safety.py` (7). Deferred here: `test_router_repository_safety.py` (C03) 5 cases (`test_first_publication_blocks_batch_replacement…`, `test_batch_http_mutations_cannot_abandon_published_policy`, `test_batch_http_mutation_during_first_readback…`, `test_router_settings_allow_credential_repair…`, `test_rotated_router_password_can_be_repaired…`); `test_notifications.py` (C07) `test_confirmed_empty_router_release_clears_previous_failure`; `test_operation_logs.py` (C02) router release log cases; `test_profile_clone_router_isolation.py` (C04, route half with C34); `test_router_gateway_forwarding.py` (A20-owned; the route is here); route halves of every R01–R14 test file.

Routes (21, 0 done):

`GET /api/routers`; `POST /api/routers`; `PUT /api/routers/<router_id>`; `DELETE /api/routers/<router_id>`; `POST /api/routers/<router_id>/probe`; `GET /api/routers/<router_id>/report`; `POST /api/routers/<router_id>/plan`; `POST /api/routers/<router_id>/apply`; `GET /api/routers/<router_id>/routes`; `POST /api/routers/<router_id>/routes/plan`; `POST /api/routers/<router_id>/routes/apply`; `GET /api/routers/<router_id>/policy`; `GET /api/routers/<router_id>/policy-settings`; `PUT /api/routers/<router_id>/policy-settings`; `POST /api/routers/<router_id>/policy-settings/preview`; `GET /api/routers/<router_id>/rule-sync`; `PUT /api/routers/<router_id>/rule-sync`; `POST /api/routers/<router_id>/rule-sync`; `GET /api/routers/gateway-forwarding`; `POST /api/routers/gateway-forwarding`; `GET /api/agents/<agent_id>/upstream-routers`

Notes for porter: **rule 4b is critical here**: probe, apply, routes/apply, rule-sync, settings PUT/DELETE must run to completion with `context.WithoutCancel(ctx)` even if the browser disconnects — a half-finished publish on a router is the worst outcome. `_manual_sync_lock` / `_router_settings_lock` decorators take `job_lock` (409 texts). `Cache-Control: no-store` on gateway-forwarding and upstream-routers. `gateway-forwarding` calls A20 (`gateway_forwarding_entry`, `persist_gateway_forwarding`) directly from this API package (no cycle); until A20 exists it fails closed (500 with a "not wired" error), never silently. The `/api/profiles/<id>/…` forms do not exist for routers (profile via `X-ConfigFlow-Profile`/`?profile=`).

## R16 — client devices and discovery

**Status: `missing`** · prereqs: R13 (`Driver`), R04, R05, A01, C26 (`apply_policies`, `read_policies`, `normalize_client_range`), C31 (`device_notes`) · Go: internal/clients, internal/routers

Files:
- `backend/common/client_devices.py` (63), `client_discovery.py` (204), `client_discovery_cache.py` (88)
- `backend/routers/ros_clients.py` (289), `backend/routers/ikuai_clients.py` (142)

Tests: `test_client_devices.py` (11; route half → R17), `test_client_discovery.py` (35), `test_client_discovery_cache.py` (6). Replayed from config: `test_oxidns_device_policy_side_entries.py` (client_devices calls).

Routes: none (the `/api/client-devices*` handlers are R17).

Notes for porter: RouterOS ARP/lease/ping readers (`PingNotPermitted`, `TIMEOUT`), iKuai client readers, `Sighting`/`Reading`/`better` from routerdoc, `DiscoveryCache` snapshot timing (hand-port the time-based cases). `list_devices` is used by agents A23 — export it. Read-only: not HR.

## R17 — client fixed IP and client-device routes

**Status: `missing`** · HR · prereqs: R16, R06 (`router_operation_lock`), R14 (`job_lock`), C06 (JWT secret), C36 hook, A13 collaborator · Go: internal/clients, internal/routers, internal/api/clientdevices, internal/routerdoc (`ValidateBindings`)

Files:
- `backend/common/client_fixed_ip.py` (281), `backend/routers/routeros_fixed_ip.py` (382), `backend/routers/ikuai_fixed_ip.py` (261), `backend/routes/client_devices.py` (153)

Tests: `test_client_fixed_ip.py` (17), `test_ikuai_fixed_ip.py` (14), `test_routeros_fixed_ip.py` (23)

Routes (5, 0 done): `GET /api/client-devices`; `PUT /api/client-devices`; `GET /api/client-devices/discovery`; `POST /api/client-devices/fixed-ip/preview`; `POST /api/client-devices/fixed-ip/apply`

Notes for porter: preview tokens are `itsdangerous.URLSafeTimedSerializer(JWT_SECRET_KEY, salt='client-fixed-ip-v1')` (itsdangerous 2.2.0: compact JSON, zlib only when shorter, base64url, int timestamp, HMAC-SHA1 with django-concat key derivation) — port by hand (no new dependency) and cross-check both ways (a token minted by Python must verify in Go and the reverse, same secret). Writes DHCP static leases (HR). `validate_bindings` moves to `routerdoc.ValidateBindings` so `repo` drops its copy. `routes.agents._public_deployment` (A13) → collaborator variable until A13. Rule 4b for apply.

## Cross-domain contract (coordinator decision 3: interfaces/hooks, no copies)

### Hooks other domains already declared that routers must implement

| Declared in | Go signature | Python | Provided by |
|---|---|---|---|
| `repo.Hooks` (C03) | `JobLock func(profileID, routerID string, router *jsonx.Object) (release func(), acquired bool)` — default *always acquired* | `routers/rule_sync_store.job_lock` | R14 (must be wired before any Go router write route registers, i.e. before R15) |
| `repo.Hooks` (C04/C34) | `RedactRouters func(routers any) any` — default: `ExportAll(desensitize)` refuses | `routers/store.redact_routers` | R01 |
| `notifications.Sources` (C07) | `Summarize func(report, plan *jsonx.Object) *jsonx.Object` — default: error "not wired" | `routers/pending.summarize` | R13 |
| `tasks` (C07) | `SetRows(tasks.SlotRouters, RowsFunc)`; `SetRunner("router_rule_sync", RunFunc)` with `*tasks.Conflict` for RouterError | `routes/tasks._router_rows`, `rule_sync.start_job` | R14 |
| `lifecycle` (C06) | `lifecycle.Loop("router_rule_sync", func(ctx))` | `app.py` → `rule_sync.start_loop` | R14 |
| `convert.MihomoHooks` (C18) | `IsFullTun func(config *jsonx.Object) (bool, error)` | `router_split.is_full_tun` | R02 |
| `oxidns` (C22 stub, C27 owner) | proposed `ClassicalIPLines func(ctx context.Context, item, config any) any` (nil/true/false) | `rule_ip_presence.classical_ip_lines` | R02 (C27 declares the hook when it replaces the stub) |
| `notifications/compat.go`, `repo/compat_oxidns.go` | replace copies by imports of `routerdoc.RoutersOf`, `routerdoc.IPv4`, `NormalizeMAC`, `ValidateBindings` | `store.routers_of`, `lan_clients`, `validate_bindings` | R01, R17 (they edit only their own packages; the coordinator switches repo/notifications) |

### Hooks routers declares (fail closed until registered)

| Declared in | Go signature | Python | Provided by |
|---|---|---|---|
| `routers` | `IPv6HostRequest func(ctx context.Context, agent *jsonx.Object) (*jsonx.Object, error)` (error = MonitorError) | `agents.ipv6_readiness._request` in `_ipv6_host` | A23 (A23 imports routers) |
| `routers` | `RulesetContent func(ctx context.Context, item, libraryRule, config *jsonx.Object) (string, error)` (offline: `allow_network=False`) | `routes.rules.get_ruleset_content` | C32 — or a direct import if C32 places it in a non-API package (preferred) |
| `routersplit` and `clients` | `ProfileSessions func(ctx context.Context, r *repo.Repository, profileID string) ([]*jsonx.Object, error)` | `setup_flow.profile_sessions` | C36 (C36 imports routers/routersplit) |
| `clients` | `PublicDeployment func(record *jsonx.Object) *jsonx.Object` | `routes.agents._public_deployment` | A13 |

### What routers needs from config/agents (direct imports)

- Config ready: repo (`GetConfig`, `SaveConfig`, `ResetConfigContext`, `UpdateConfigTransaction`, `ListProfiles`, `GetProfile`, `DefaultProfileID`, `ResolveProfileID`, `SaveRouterSettings`, `SaveRouterPublication`, `GetProfileAgentBindings`, `DataDir`), httpx/auth (`auth.Require`; the JWT secret accessor for R17 — `auth` has it as config `SecretKey`, may need an exported getter: tell the coordinator), logx, taskstatus, `notifications.RecordRouter`/`Safely`/`RefreshTasks`/`TaskStatus`, tasks, urlx, yamlx, pystr, rulefetch (C14), `convert.RulesetFormat`/`DefaultMihomoTemplate` (C12/C18), `mosdns.ConvertRuleContentForMosDNS` (C21), `oxidns.GenerateOxidnsConfig` (C22), `oxidns.NormalizePolicies`/`NormalizeClientRange`, `oxidns.IsSettingsError` (C05 `OxidnsSettingsError`).
- Config missing: C26 `oxidns_device_policy.apply_policies`/`read_policies` (R16), C31 `device_notes` (`normalize_name`, `read_notes`, `set_note`, `policy_names_revision`, `save_policy_rows`; R15/R16/R17), C34 `config_export.sanitize_external_payload` (R15 policy preview), C32 `get_ruleset_content` (R01, via hook).
- Agents ready: `agents.Manager` (`GetAgentByID`, return routing calls), `agents/monitor.MonitorError`, `agents/diag.EventMessage`.
- Agents missing: A20 `linux_gateway.gateway_forwarding_entry`/`persist_gateway_forwarding` (R15 imports `internal/gateway` directly), A23 (hook above), A13 (hook above).

### What other domains need from routers (export these names)

- Config: C18 `IsFullTun` (R02); C27 `ClassicalIPLines` (R02); C36 setup flow/completion: `routersplit` (`ReadMarker`, `MarkerKey`, `RouteKeysRemoved`, `LoadBase`, `FullTunDocument`, `TunFilters`, `IsFullTun`, `RouteRuleSetIDs`), `routers` (`PublicView`, `Normalize`, `RouterValidationError`, `EditableFields`, `ComputeDesired`, `BuildPlan`, `RouterError`, `Driver`, `Inspect`).
- Agents: A14/A19/A20/A21/A22 `routersplit` (`CheckGeneration`, `RouterSplitState`, `LegacyContext`, `LegacySource`, `PublishLegacySource`, `LegacyGatewayAgentIDs`, `GatewayWasUsed`, `BaseIsFullTun`, `IsFullTunDocument`, `RequireFullTunGenerated`, `FullTunBaseYAML`, `ModeFromContext`, `NeedsContext`, `Messages`, constants); A18/A20/A23 `routerdoc.RoutersOf`; A18 `dhcp_dns` (`DNSHASettings`) and `Execute` (tests); A23 `routers.IPv6Readiness…` (R11), `RouterError` (R01), `RouterOperationLock` (R06), `RouterOSDriver` (R04), `JobLock` (R14), `clients.ListDevices` (R16).

### Cycle `router_split` ↔ `linux_gateway` — proposed interface

Python: `router_split` imports `LinuxGatewayError` and `TUN_FILTER_KEYS` from `common/linux_gateway.py` (and lazily `linux_gateway_draft.RESTORE_KEY`); `linux_gateway.apply_linux_gateway` lazily calls `router_split.check_generation`, and `agents/linux_gateway.py`, `gateway_onboarding`, `gateway_installation`, `routes/linux_gateway.py` call many router_split functions.

Proposal — break the cycle with a leaf package, no hook needed:

```go
// package gwbase (internal/gateway/gwbase): constants and the error type both sides share.
type LinuxGatewayError struct{ Message string }        // common/linux_gateway.LinuxGatewayError (a ValueError)
func (e *LinuxGatewayError) Error() string
func (e *LinuxGatewayError) PythonClass() string        // "LinuxGatewayError"
var TUNFilterKeys = []string{"include-interface", "exclude-interface", "include-uid", "include-uid-range",
	"exclude-uid", "exclude-uid-range", "include-mac-address", "exclude-mac-address",
	"include-android-user", "include-package", "exclude-package"}  // order matters
const RestoreKey = "linux_gateway_restore_point"         // linux_gateway_draft.RESTORE_KEY

// package routersplit (R02) imports gwbase, never internal/gateway.
type RouterSplitError struct {
	gwbase.LinuxGatewayError
	Code   string // ROUTER_SPLIT_*
	Status int    // 409 unless given
}
func (e *RouterSplitError) As(target any) bool          // errors.As(err, **gwbase.LinuxGatewayError) == true
func CheckGeneration(config *jsonx.Object) error        // nil or *RouterSplitError
```

- `internal/gateway` (A19–A22) imports `gwbase` (its own error type and constants) and `routersplit` directly; `apply_linux_gateway` calls `routersplit.CheckGeneration(config)` first, then registers `convert.MihomoHooks.ApplyLinuxGateway` as C18 declared.
- `routersplit` imports `convert` (`DefaultMihomoTemplate`) and `repo`; `convert` reaches it only through `MihomoHooks.IsFullTun` (registered by `routersplit.Wire`), so no cycle.
- `routersplit`'s only remaining upward call, `setup_flow.profile_sessions`, is the `ProfileSessions` hook above (C36 registers; default `*HookMissingError`, reached only for an unmarked profile with the gateway on — `needs_context`).
- Ownership: R02's porter creates `gwbase` (3 symbols, no logic) because R02 can start now; A19 adopts it instead of declaring its own `LinuxGatewayError`. If A19 starts first, it creates `gwbase` with exactly this content. Needs coordinator confirmation (question 2).

## Testing approach (rule 4: record & replay, corpus, differential)

### 1. Record & replay of the Python suite

One pytest plugin for the domain, `server/internal/routers/testdata/routers_record.py` (pattern `internal/convert/testdata/c18_record.py`), built by R01 and extended per module (a per-module list of entry points). Run with the whole backend suite (`-n 6 --dist=worksteal`), merge, gzip to `testdata/<module>_recorded.jsonl.gz`.

What a record holds:
- the test id, the entry point (`module.function` or `Driver.method`), arguments and result (or exception class + `str()`); dataclasses (`RouterState`, `RouteEntry`, `NatBypassEntry`, `RouterInfo`, …) serialised as `{"__dataclass__": name, fields…}`, `ipaddress` objects as text, bytes as hex; arguments mutated in place (`desired.update`, `state.nat_context`) recorded after the call;
- **the device transcript**: every request the code under test sends, in order, per driver instance — `(driver id, method, path, json body as Python sent it (value and `json.dumps` bytes), timeout, allow_redirects)` — and the answer (status, JSON/text/empty body, or the requests exception class); for `/execute` the script text byte for byte; plus the driver attributes read/written around the call (`ros_policy_manifest`, `dns_guard_manifest`, `ipv6_*_manifest`, `_forward_filter_rows`);
- collaborator answers: Agent manager (`get_agent_by_id`, return routing status/apply/remove), A23 IPv6 host request, rule content loader, clock (`datetime.now`, `time.monotonic`), `time.sleep` calls, random ids (`router_…`), repository reads/writes made inside the call.

How the device transcript is captured (Python tests fake the device at five levels; all must be seen):
- real `RouterOSDriver`/`IKuaiDriver` with a fake **session** (`FakeROSSession`, `FakeIKuaiSession`, `StatefulRouterOS`, `PagedIKuai`, `LabSession`, …) — wrap `session` (`get/post/put/patch/delete/request`);
- instance monkeypatches of `_request` (10 tests), `_list` (5), `_transaction` (2), `session.request` (4), `close` — the plugin installs a class-level `__setattr__` on both driver classes so whatever a test assigns to these attributes is wrapped by a recording proxy (monkeypatch uses `setattr`);
- subclasses (`FakeROS(RouterOSDriver)`, `MemoryROS`, `FlagTableROS`, …) overriding `_request`/`_get`/`_post`/`read_state` — record at the overridden method;
- duck-typed drivers (`MemoryDriver(RouterDriver)`, local `class Driver:` in 6 files, `Device`, `RouteDevice`, …) passed to `build_plan`/`execute`/editors — record the **method-level transcript** (method, args, result/exception, attribute reads incl. `hasattr` probes).
Each record names its level; the Go replay injects answers at the same level: the Go driver exposes the same seams as overridable fields (`request`, `list`, `transaction`, HTTP transport) and the planning/release code takes a `Driver` interface (with optional capability interfaces for Python's `hasattr`), so a scripted fake built from the transcript plays the device. The replay **asserts that Go sends the identical request sequence** (method, path, body value and bytes, script text) and stops at the first difference, printing both. Rows that need a level with no Go equivalent are counted and hand-ported (listed in the module section).

Concurrency (pending reader threads, rule-sync loop/jobs, per-router locks, `FutureTimeout`), lock files, and time-based caches are hand-ported, as rule 4 allows.

### 2. Random corpus (real Python on the test server)

Pure functions where edge inputs matter: CIDR/IPv4/IPv6 math and indexes (`StaticRouteIndex`, `SupernetIndex`, `_networks`, `merge_sources`, IPv6 prefix logic) against `ipaddress` (leading zeros, full-width digits, mapped/zone addresses, host bits with `strict=False`); `store.normalize` messages; `lan_clients.ipv4`/`normalize_mac` (`int(x, 16)` on full-width digits, signs, whitespace); `ros_values.comparable_values`; iKuai `encode_value`/`decode_value`; RouterOS console escaping (`_script_value`, `_script_string`, `console_safe`, `_console_add`) with `"`, `\`, `$`, `;`, newlines, controls, CJK, emoji — an escaping bug writes arbitrary console commands to a router; `dns_guard.normalize_settings`; `rule_sync_rules.digest`; `router_split.read_marker`/`full_tun_document`; itsdangerous tokens (fixed timestamp).

### 3. Flask ↔ Go differential with one loopback fake router

`internal/api/routers/difftest` (run with `CONFIGFLOW_DIFF=1`, pattern `internal/httpx/difftest` + `internal/api/agents/difftest`):
- **Fake device**: `internal/routers/routerfake` started as `httptest.Server` on 127.0.0.1 (HTTP and a TLS variant with a self-signed certificate for `verify_tls` true/false and the SSLError path). It implements RouterOS REST (`/rest/<path>` GET with `?key=value`, PUT/PATCH/DELETE, POST `…/print` `.query`, `…/remove`, `/execute` via the ported `routeros_test_helpers` interpreter, `/ping`, `/system/resource`) and iKuai (`/Action/login`, `/Action/call` dispatching `func_name`/`action` over static_rt, route_object, nat_rule, stream_ipport, DHCP static/DNS, sysstat, paging). Control endpoints on a separate path: `reset(seed)` (seeds taken from the Python fixtures), `log` (every request with body bytes), `state` (all tables), fault injection (HTTP 401/403/404/500 with RouterOS bodies, lost reply after a write → UncertainWrite, delay for connect/read timeouts, connection reset, empty body for iKuai oversized requests).
- **Fake Agent** for return routing / IPv6 host / gateway forwarding (extend the api/agents difftest Agent).
- Flask (`python -m backend.app`) and the Go app each get a copy of the same data directory whose router records point at the fake's URL (Python sessions use `trust_env=False`, so no proxy interferes). For each case: reset fake → request to Flask → capture → reset fake + fresh data copy → same request to Go → compare **status, headers, body, `configflow.sqlite3` rows, `DATA_DIR` files (`router-rule-sync/*.json`, `task-status.json`, notification store), the fake's request log (order, bodies, scripts) and its final tables**. Masks only for timestamps (`at`, `generated_at`, `checked_at`, log times) and generated router ids; fingerprints are not masked (they are hashes of state and must be equal).
- Cases: every route with success and failure (unknown router 404, validation 400, router down 502, stale fingerprint 409, blockers 409, lock busy 409, permission errors), multi-step flows (create → probe → report → plan → apply with the plan's fingerprint → report; route editor plan/apply; policy-settings preview/save; rule-sync enable → run → poll; fixed-IP preview → apply with the preview token), a client disconnect during apply (Go must finish exactly like Flask — rule 4b), and two concurrent applies (same outcome set). Fixed-IP tokens are time-dependent: compare structure and cross-verify tokens between the two servers (same `JWT_SECRET_KEY`).

### 4. VM lab (HR, after the maintainer agrees)

Rebuild the PVE lab RouterOS CHR (7.24/7.25.1 like production) and iKuai 4.0.x VMs plus lab Mihomo/OxiDNS Agents. A/B on one snapshot: Python server publishes → dump device (`/export`, REST tables, iKuai lists) → revert snapshot → Go server publishes the same profile → dump → diff; also failure + rollback with a permission-limited user, rule sync loop over several intervals, DNS guard, IPv6 fake-IP/routes, PD guard (needs a DHCPv6-PD lab), route editor, fixed IP. Never production.

## Next steps (coordinator; at most 2 agents in this domain at a time)

Before porting — coordinator decisions (questions below): shared requests-like HTTP client (before R04/R05), `gwbase` ownership (before R02), `routerdoc` leaf (before R01), C32 `get_ruleset_content` placement (R01 uses a collaborator variable meanwhile).

Porting order (one fresh porter per module; slot 2 alternates between a second porter for independent modules and the reviewer of the module just finished):
1. **R01** (builds the record plugin, `pending.go`, routerdoc) ‖ **R02** (independent; unblocks config C18 `IsFullTun`, C27, agents A19–A22 and the cycle).
2. **R03** → **R04** ‖ review R01, R02.
3. **R05** ‖ review R03; **R06** ‖ review R04 (R06 early because A23/R17 need `router_operation_lock`).
4. **R07** → **R08** ‖ reviews R05, R06, R07.
5. **R09** → **R10** → **R11** ‖ reviews.
6. **R12** (all pending variables of R03–R11 resolved; the replay of the release test files becomes complete) ‖ review R08–R11.
7. **R13** → **R14** (registers JobLock, task slot, loop) ‖ review R12.
8. **R15** (after C31/C34 or with fail-closed collaborators; gateway-forwarding after A20) ‖ review R13, R14.
9. **R16** → **R17** (after C26/C31; A13/C36 hooks) ‖ reviews R15, R16, R17.
10. Domain differential (step 4 of WORKFLOW) once all 17 are `verified` and the providers of every hook exist (A13, A20, A23, C31, C32, C34, C36); then the HR lab tests.

Review order: each module right after its porter; R01/R02 first because other domains build on them. Reviewers check the test-file → coverage mapping (WORKFLOW test coverage policy), rule 4b on every handler, and that pending/collaborator rows reached zero for the modules already ported.

HR lab tests (after the maintainer agrees; PVE lab only): after R12 (manual publish RouterOS + iKuai, rollback), R09–R11 (DNS guard, IPv6), R14 (rule sync), R06 (route editor), R17 (fixed IP), R02 (full-TUN generation through C18/A22 with an Agent).

Questions for the coordinator/maintainer:
1. **Shared router HTTP client**: routers needs requests 2.31 semantics (exception class names in user messages, connect/read timeouts, `trust_env=False`, Basic auth latin-1, `json=` bytes, no redirects, `verify` toggle). `internal/substore/httpclient.go` already classifies requests exceptions with per-read deadlines. Extract it into a shared package (with `reqproxy`) before R04, rather than a third copy?
2. **`internal/gateway/gwbase`** created by R02 (proposal above) and adopted by A19 — agree?
3. **`internal/routerdoc`** leaf (routers_of, lan_clients, validate_bindings) so `repo` and `notifications` drop their compat copies — agree? (The switch in repo/notifications is a coordinator edit.)
4. **C32 `get_ruleset_content`**: export from a non-API package (also needed by A19) instead of a routers hook?
5. `test_policy_editors.py` was not assigned to any config module; assigned to R08 with its rule-library route half deferred to C33 — confirm.
6. Agents planned `test_router_gateway_forwarding.py` under A20 while the route lives in R15: keep the file with A20 (A20's porter adds the route cases to R15's differential when both exist)?
7. The lab VMs (CHR, iKuai) were destroyed in October; rebuilding them is part of the HR lab step — confirm when the maintainer wants it.

## Coordinator decisions (2026-11-16)

1. **Shared requests-style HTTP client:** yes. Before R04 writes its driver, extract
   `internal/substore/httpclient.go` (+ `internal/reqproxy`) into a shared package (e.g. `internal/reqhttp`)
   used by substore, rulefetch where it fits, and the router drivers; no third copy. The R04 porter does
   the extraction first, keeping substore's tests and differentials green.
2. **`internal/gateway/gwbase`:** agreed. R02 creates it (only `LinuxGatewayError`, `TUNFilterKeys`,
   `RestoreKey`); A19 adopts it.
3. **`routerdoc` leaf:** agreed. The R01 porter creates it and switches the router compat copies in `repo`
   and `notifications` to it with minimal edits, re-running those packages' tests.
4. **`get_ruleset_content`:** C32 exports it from a non-API package (no routers hook for it); until C32
   lands, R-modules that need it declare the narrowest interface and fail closed.
5. `test_policy_editors.py` under R08 (rule-library route part → C33): confirmed.
   `test_router_gateway_forwarding.py` stays with A20 (route in R15): confirmed.
6. **VM lab rebuild:** only after the maintainer agrees; the coordinator asks when the first HR router
   modules are verified. No production routers are ever touched.
