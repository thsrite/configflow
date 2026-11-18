# PROGRESS — domain `routers`

Domain `routers` (WORKFLOW.md): router core, iKuai, RouterOS core/policy, router DNS/IPv6, router publish/rule sync, clients/split (old packages #32–#38). **52 Python files, 17574 lines, 17 modules** (equal to the `routers` totals in `FILES.md`) — 8 missing, 0 partial, 1 ported, 8 verified (R01–R08). Only reviewers set `verified`.

Planning audit: 2026-11-16, worktree `configflow-go`, branch `go` at 7b1b90b5 (plus uncommitted work of the C24/C25 agent in `internal/oxidns`). The Go tree was searched directly (`grep -ri 'routeros|ikuai|router_split|routers_of|router' server/internal`): **no package ports any routers file**; `internal/server` wires no routers package. What exists is listed under "Go code that already touches this domain" below; nothing from the old `progress/` files covers #32–#38.

## N7-3c — desired/router-split YAML readers (ported)

Branch `go-n7-3c`; **only YAML reading and base graph handling**; no router write
logic changes. Desired fake-IP and router-split base/shape/generation reads use
Parse/Node. Tagged/untagged values and composite-key/key-removed documents produce
identical complete router plans. D1/other policy and both unsafe-alias categories now
return typed Chinese errors with profile/reason before any desired plan or caller
I/O; only actual ErrSyntax retains the default. Three loopback RouterOS cases
assert no plan and zero requests, with a live fallback control. Opaque composite keys are preserved in free-form
base YAML, ignored by string-key desired/TUN schema navigation; no default range
is substituted merely because unrelated metadata has a composite key.

**3438 actual input occurrences**: equal 3168; native diagnostics 141; D1 7;
YAML 1.1/timestamp 43; reader policy 9; tag shape 33; unsafe aliases 6;
复合映射键：Node 保留 8; 未知标签：Python 拒绝 → Go 接受 19; deep ordinary 4;
native grammar/explicit-scalar-native 0. Counts and per-entry comparisons:
[progress/n7-3c-gate.md](progress/n7-3c-gate.md).

Full router-split recorded/corpus/edge comparisons pass on .32/.64. New graph
regressions check depth-40 alias chains, cycles, displaced definitions, and plain
10000/10001-depth documents. Native schema changes use an independent actual
Python converter reference; every unrelated field and collaborator call stays
strict. Final server acceptance and command/output record:
[progress/n7-3c.md](progress/n7-3c.md). HR plan tests use synthetic data only.

Final acceptance: gofmt/vet, scope race, full suite, all 12 live difftest packages,
and eleven 60s fuzz targets pass on .32/.64. Proddiff:
coverage_regressions=0/new=0/stale=0; full command/output record at the link above.

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
| R01 | router models, store, DHCP DNS, desired state | 969 | verified | HR | C04, C05, C21, C32¹ | – | internal/routers, internal/routerdoc, internal/netipx |
| R02 | router split (full TUN marker), rule IP presence | 381 | verified | HR | C04, C12, C14, C18, C21; C36² | – | internal/routersplit, internal/gateway/gwbase, internal/ippresence |
| R03 | RouterOS NAT assessment, iKuai IP groups, drift report | 1499 | verified | HR | R01 | – | internal/routers |
| R04 | RouterOS REST driver, batch writes | 1415 | verified | HR | R01, R03; shared requests client³ | – | internal/routers |
| R05 | iKuai driver | 1179 | verified | HR | R01, R03; shared requests client³ | – | internal/routers |
| R06 | device route editor (RouterOS/iKuai static routes) | 684 | verified | HR | R04, R05 | – | internal/routers |
| R07 | RouterOS NAT address lists, address-list sync, test helpers | 1422 | verified | HR | R03, R04 | – | internal/routers, internal/routers/routerfake |
| R08 | RouterOS policy routing (plan, write, view) | 1349 | verified | HR | R07, C05, C26 (`normalize_policies`, present) | – | internal/routers |
| R09 | forced DNS (DNS guard), IPv4 same-port forward hint | 773 | verified | HR | R04, R07 | – | internal/routers |
| R10 | IPv6 policy, IPv6 fake-IP, IPv6 rule-set routes | 1247 | verified | HR | R04, R09 | – | internal/routers |
| R11 | IPv6 PD guard, IPv6 DNS, RouterOS IPv6 readiness | 1161 | verified | HR | R10 | – | internal/routers |
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

**C32 已接线**（协调者批准）：`ComputeDesired` 和 `RulesetContent` 首参增加 `context.Context`，`ContentLoader` 闭包形状不变。`api/rules.Wire` 复用共享 source helper 的 offline 选项，读取顺序为验证缓存 → inline，绝不联网。缺缓存与 Python 一样形成中文 blocking_warnings/不完整计划，不再返回 ErrNotPorted；真实 Python plan 差分及拒绝所有出站请求的 fake 覆盖此合同。合并顺序 R08 → C32 → R09；desired.go 仅改签名/透传 ctx，R08 字段全部保留。

**Status: `verified`** (adversarial review 2026-11-16, commits 15a3c7b7, a4c56e1d + this section) · HR · prereqs: C04, C05, C21 (ready), C32¹ · Go: internal/routers, internal/routerdoc, internal/netipx

Files:
- `backend/routers/__init__.py` (1), `base.py` (371), `ros_values.py` (27), `lan_clients.py` (93, → routerdoc), `dhcp_dns.py` (158), `store.py` (156), `desired.py` (163); plus the settings validators of `dns_guard.normalize_settings` (R09) and `ipv6_pd_guard.normalize_settings/settings_of` (R11), ported here because `store.normalize/public_view` always call them, and the check of `client_fixed_ip.validate_bindings` (R17) for `repo`.

Commits: 5440b75f/650c8c65/37f783f3/5f89caeb (first, Python-shaped port; superseded), 755f1a9d (reshaped Go-native), 1ea5d840 (PROGRESS); review: **15a3c7b7** (fix + slimming + corpus rows), **a4c56e1d** (helper unit tests).

Review 2026-11-16 (adversarial, every function/branch of the 7 files + the three validators walked against Go):
- **Finding 1 (high, fixed 15a3c7b7):** `FakeIPNetwork` read `custom_config` with `gopkg.in/yaml.v3` directly. yaml.v3 rejects duplicate mapping keys anywhere in the document (PyYAML and the Go Mihomo generator, which reads through `yamlx`, take the last value) and reads the first of several documents (PyYAML: error → default). With a duplicate key the router was told to route 198.18.0.0/16 while Mihomo used the custom range: fake-ip traffic would bypass the gateway. Now `yamlx.LoadWith(KeepNonStringKeys, KeepUnrepresentable)` + `yamlx.Lookup` (follows the facade's native swap together with the generator). Regression: 13 `compute_desired` corpus rows with Python results (duplicate inner/top/`dns` keys, multi-document, int/`yes` keys, `!!set`, merge key, tab, numeric value, quoted, blank) — 4 of them fail on 755f1a9d.
- **Finding 2 (low, fixed 15a3c7b7):** `ProxyGroups` typing introduced during slimming would have failed `compute_desired` on a non-object group; kept tolerant (Python skips non-dicts), covered by the same rows (`proxy_groups: [1, null, …]`).
- Checked and equal (corpus or unit tests): every Chinese message of normalize/guards/DHCP DNS/validate_bindings and their order (table-driven check list = Python's `if` order); defaults (verify_tls, interval 60, guard defaults, password kept on empty); Extra round trip of device state and unknown fields; records written before 2026-10-18 (no auto_rule_sync/rule_sync_interval/ros_policy_rule_sets/manage_return_routing: view and normalize equal; Go writes the reader defaults back — functionally equal, counted in the corpus as `stored: absent setting written with the reader default`); type/endpoint changes pinned by each `*_state`; redaction (password and the 7 state keys dropped, username/endpoint masked, non-object items skipped); DHCP DNS single/HA pairing, offline/publication/state branches, report statuses, readback; static-route selection (sweep rewritten without the edge type: boundaries = target start, network starts, the address after each network inside the target); merge_sources; comparable_values; MAC/IPv4/bindings.
- "Not chased" list re-checked: list/dict sent for a text field (frontend sends strings); IPv6 scope ids (route/rule CIDRs never carry them; scoped link-local is not global anyway); full-width digits/signs in MAC parts (MACs come from RouterOS/iKuai readback or devices the UI lists, all ASCII); non-string rule ids/names (UI writes strings). **Accepted stricter endpoint check** (probed on Python): Go rejects `http://a:abc`, `http://h:-1`, `http://h:+80`, `http://h: 80`, hosts with spaces/backslash/bad `%` escapes and unescaped zone `[fe80::1%eth0]`, which urlparse lets through but no driver can connect to (the zone form is a link-local management address nobody uses); `http://[::1` was a Python 500 (ValueError), Go 400 with the Chinese message. Pinned in `TestR01ReviewEndpoints`.
- Notes for later modules: Python defaulted `DhcpDnsEntry.enabled/dns_explicit` to True, `RouteEntry.table` to main (use `NewRoute`), `RouterInfo.capabilities`/`blockers` to [] — drivers (R04/R05) set them explicitly. Base-class default errors (`不支持 DHCP DNS 下发`/`不支持直接写入`) are dead in Python (both drivers override).

Go layout (`internal/routers`, no jsonx, no `any` parameters):
- `model.go` — `RouterError{Kind: KindRouter|KindConflict|KindRollback}` + `AsRouterError`, `ValidationError` (400 + message), `ErrNotPorted`; `RouterInfo`, `RouteEntry` (`NewRoute`), `PolicyRouteEntry`, `NatBypassEntry`, `DhcpDnsEntry`, `DhcpDnsState`, `RouterState` (only the fields R01 uses; later modules add theirs), `Driver` interface (Python `RouterDriver`; vendor defaults are added by R04/R05), `IsManaged`, `ManagedComment`, policy constants, `PolicyRouteOK`, `PolicyRoute`, `RetirePolicyRoutes`, `StaleRoutes`, `StaleRouteWarning`. JSON tags = Python field names (as_dict is `json.Marshal`).
- `staticindex.go` — `NewStaticRouteIndex(routes).Select(cidr, gateway)` → `StaticRouteSelection{Candidates, Selected, Covered, Conflict}`, `StaticRouteCandidates`. The interval sweep picks, per boundary, the most specific preferred network directly (Python used a heap; same result).
- `router.go` — `RouterSettings` (stored settings the UI sees) embedded in `Router` (+ `Password`, `Extra map[string]json.RawMessage` keeping last_probe/last_release/*_state and any unknown field; `UnmarshalJSON` applies the reader defaults verify_tls=true, rule_sync_interval=60) and in `RouterView` (+ last_probe/last_release/has_password; `View()` = public_view), `State(key)`, `StaticRuleSets()`, `DNSGuard`/`DefaultDNSGuard`/`NormalizeDNSGuard`, `PDGuard`/`NormalizePDGuard`, `Normalize(body, existing)` (merged raw record → typed fields, then one ordered check table with store.normalize's messages), `NewRouterID` (var, tests), `RedactRouters(document) ([]byte, error)`.
- `dhcpdns.go` — `DNSAgent`/`AgentHA` (typed Agent view), `DNSSettings` (`dns_agent_id/dns_server/dns_error/dns_ha`), `DNSAgentSettings`, `DNSHASettings`, `MatchingNetworks`, `BuildDNSReport(dns, gateway, state)` → `DNSReport`, `DNSCheck`, `CheckedDNSTarget`, `VerifyDNS`.
- `desired.go` — `RuleSet`, `LibraryRule`, `RuleSourceIssue`, `FakeIPNetwork(customConfig)` (yamlx facade, like the Mihomo generator), `MergeSources(map[netip.Prefix][]string)`, `Desired` (embeds `*DNSSettings`, set later by the inspection), `ContentLoader`, `RulesetContent` (C32 Wire installs the ctx-aware offline cache/inline reader), `ComputeDesired(ctx, profile JSON, gateway, ruleSets, loader)`.
- `rosvalues.go` — `ComparableValues(path, row map[string]any)` (row = decoded RouterOS REST JSON; R04 may type it).
- `wire.go` — `Wire()` registers `repo.Hooks.RedactRouters` (adapter converts repo's jsonx value at the boundary); listed in `server.wirePackages` (37f783f3).
- `internal/routerdoc` (leaf: `Sighting` (JSON = the UI view; `AddSource` keeps arp/lease/online order), `Reading`/`Covers`, `Better`, `Rank`, `SourceOrder`, `IPv4`, `NormalizeMAC`, `ValidateBindings(document) string` + messages). `repo/compat_oxidns.go::validateBindings` calls it (jsonx → JSON at the boundary). `notifications.RoutersOf` stays its own 4-line jsonx helper (decision 3 switch reverted: routerdoc no longer uses jsonx; notifications moves to typed records in its S pass).
- `internal/netipx` (leaf, on `net/netip`, shared with later modules): `ParseAddr4`, `ParseNetwork(text, strict)` (bare host, leading-zero prefix, IPv4 netmask/hostmask like ip_network), `ParseNetwork4`, `Broadcast`, `ComparePrefix`/`SortPrefixes`, `IsPrivate`/`IsGlobal` (CPython 3.11.17 tables incl. exception blocks), `Collapse` (collapse_addresses). The `pyip` port of Python `ipaddress` was dropped (maintainer: Go-native libraries).

Size (wc -l, non-test): before review routers 1596 + routerdoc 195 = **1791**; after review routers 1327 (desired 237, dhcpdns 242, model 216, rosvalues 34, router 402, staticindex 171, wire 25) + routerdoc 167 = **1494** (code only, no blank/comment lines: 1217). Python accounting: the 7 R01 files are 969 lines; R01 also ports, per its file list, `dns_guard.py` MAX_BYPASS/DEFAULTS/SettingsError/normalize_settings (34), `ipv6_pd_guard.py` DEFAULTS/SettingsError/normalize_settings/settings_of (18) and `client_fixed_ip.py` BINDING_FIELDS/validate_bindings (15) = 67 → **1036 Python lines, 1494/1036 = 1.44×** (1.54× against the 969 of the R01 files alone; coordinator: move those 67 lines from R09/R11/R17 to R01 in the table if you agree). `internal/netipx` 245 counted separately (shared helpers net/netip lacks).

Acceptance (boundary, PORTING rule 4): `TestR01Corpus` (`r01_corpus_test.go`, `testdata/gen_r01.py` → `r01_corpus.jsonl.gz`, real Python on the test server, synthetic values only): **10000 rows, 0 differ** (review added 36 boundary rows) — normalize 2515 (create/update payloads with odd types, guards, policy/static overlap, state-owned type/endpoint changes, URL forms), stored-record round trip 800, public_view 800, redact_routers 60, compute_desired 600 (fake-ip YAML forms, disabled/library sources, non-proxy policies, public/private v4/v6 networks), dns_agent_settings 400, dns_ha_settings 400, build_dns_report 1500, verify_dns 1341, static-route select 1500, comparable_values 36, normalize_mac/ipv4/validate_bindings 27. `internal/netipx` `TestCorpus` 29742 rows vs CPython ipaddress, 0 differ. `wire_test.go` (redact hook). `r01_review_test.go`: ManagedComment, PolicyRouteOK/PolicyRoute/RetirePolicyRoutes, StaleRoutes + warning text, Sighting merge/JSON/Covers, endpoint accept/reject list. `repo`, `notifications`, `server` tests pass.

"No longer chased" (counted as equal, PORTING functional parity): a list/dict sent for a text field (name/type/endpoint/username/agent ids) is stored as JSON text in Go, Python stored its `str()` repr (27 rows); Python-internal exception texts; IPv6 scope ids in networks (Python keeps `%zone`, netipx rejects); full-width digits / signs inside MAC parts (`int(part, 16)`), Unicode case mapping in MACs; urlparse corner cases (Go `net/url` rejects a few hosts urlparse accepts, e.g. with spaces); non-string rule ids/names in profiles (skipped).

Python test files → coverage:
- `test_routers.py`: desired/store/redaction cases → `TestR01Corpus` (compute_desired, normalize, public_view, redact); driver probe/state cases → R04/R05; report cases → R03; API cases → R15 HTTP differential.
- `test_router_cidr_index.py`: merge_sources/compute_desired → `TestR01Corpus` compute_desired + netipx collapse corpus; NAT/policy halves → R03/R08/R12.
- `test_router_source_activation.py`: compute_desired activation → `TestR01Corpus`; policy half → R08.
- `test_router_dhcp_dns.py`: settings/report → `TestR01Corpus` (dns_* and build_dns_report/verify_dns); plan/release/API halves → R12/R15.

Dropped by the rule changes (decided by the maintainer, not left undone): the function-level record plugin (`routers_record.py`, written and run once over the full suite: 45085 distinct calls, plugin-induced failures fixed, then removed — function-level replay is no longer required); the loopback `routerfake` (R01 has no routes; the R15 HTTP differential builds it).

Left for others: C32 has wired `routers.RulesetContent` (offline). R09/R11 reuse `NormalizeDNSGuard`/`NormalizePDGuard`. R04/R05 add driver defaults and type RouterOS rows.

Commands (review: private detached worktree at 3a6e8792 + these files; all ok):
- corpus: `rt --back server/internal/routers/testdata/r01_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/routers/testdata/gen_r01.py server/internal/routers/testdata/r01_corpus.jsonl.gz'`
- netipx corpus: `rt --back server/internal/netipx/testdata/corpus.jsonl.gz -- bash -lc '~/cf-test/.venv/bin/python server/internal/netipx/testdata/gen_corpus.py server/internal/netipx/testdata/corpus.jsonl.gz'`
- tests: `rt -- bash -lc 'cd server && go vet ./internal/routers/ ./internal/routerdoc/ ./internal/netipx/ && go test -count=1 ./internal/netipx/ ./internal/routers/ ./internal/routerdoc/ ./internal/repo/ ./internal/notifications/ ./internal/server/'`

Pitfalls: `json.Unmarshal` into a Go string accepts `null` — use `jsonString` (strict) for values Python checks with `isinstance(str)`; the shared git index may hold other agents' staged deletions — always `git commit -- <paths>`.

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

**Status: `verified`** (review R03, 2026-11-16, zero open findings) · HR · prereqs: R01 · Go: internal/routers (+ `netipx.Summarize` exported)

Files: `backend/routers/routeros_nat.py` (509), `backend/routers/ikuai_groups.py` (684), `backend/routers/report.py` (306); plus `routeros_nat_list.py` `COMMENT/SRC_LIST/DST_LIST`, `references`, `candidate`, `inspect_rule` (58 lines, R07's file) because `assess_bypass` always needs the list-rule inspection (R07 reuses `InspectNATListRule`, `NATListRefs`, `IsNATListCandidate`).

Commits: **23c3f48c** (port, corpus, tests, this section); review **e0695483** (repack bug fixed, `out_fixed` corpus rows), **fd7ffc4f** (typed row accessors), **266bc680** (layout error tests, stdlib helpers), plus this PROGRESS commit.

Go layout (no jsonx, no `any` parameters mirroring Python):
- `rosnat.go` — `Row` (one decoded RouterOS REST row, `map[string]any`), `NatContext` (typed lists; nil = could not be read; JSON = Python keys for R12's fingerprint evidence), `NatAssessment{Status, Detail, RuleIDs}`, `RouteEgressIndex` (`NewRouteEgressIndex`, `GatewayEgress`; caches the static index, gateway egress, parsed NAT address lists and the list rule per read), `Egress(cidr, gateway, ctx, policyRouting, ix)` (Python `_egress`, used by R08/R12), `AssessNAT`, `AssessBypass`, `PreservesSources(rows, sources, destinations, iface, ctx, addressSets, accepted, cache)`, `ClientSourceNetworks`, `BypassExpansion` + `PlannedBypassConflicts` (R12 passes its `nat_update` ops with `add_dst`). `RouterState` gained `NatContext`, `PolicyNATRetiring` (NAT ids the R08 policy plan removes; Python read them from `ros_policy_plan.ops[].owned`) and `IkuaiGroups`.
- `natlist.go` — `InspectNATListRule(rows, others []ListRef) NATListRule{Rule, Problem, OutInterface}`, constants `NATListComment/NATSrcList/NATDstList/PathNAT/PathMangle`.
- `ikuaigroups.go` — names (`GroupName`, `GroupNameRole`), `CanonAddress`, `GroupDigest`, `GroupForm`/`GroupEntry`, `GroupPayload`, `EntryPayload`, `GroupBytes`; readback `GroupFromRow`, `GroupsFromRows` (= group_from_row + mark_owners), `ReadGroupRules` (read_rules + adopt) → `GroupState{Groups, Rules (read order), Rule(key)}`, `ResolveGroup`, `ClassifyRef` (`RefOwned/RefStale/RefForeign`), `ForeignRefs`, `HiddenRefs`; planning `GroupPlanner` (`NewGroupPlanner(state, &warnings, &blockers)` — shares the plan's lists like Python; `Family`, `NeedsRewrite`, `DroppedForeign`, `Migration`, `Refs`, `ReferencedValues`, `Cleanup`, `Check`, `MigrationWarning`; exported maps `Finals/Contents/Rewritten/DeletedRules/StageChecks`, `RouteRules`), `GroupOp` (kind-specific JSON keys like Python's dicts), `GroupRuleCheck`, `GroupStageCheck`, `GroupSpan`, `NATTarget`, `GroupReportSection`, `VerifyGroups`.
- `report.go` — `Networks` (Python `_networks`), `SupernetIndex`, `BuildReport(desired, state) (*Report, error)` with typed sections (`RoutesReport`, `NatReport`, `DNSReport`, `GroupReport`, summary map, in_sync). Later sections (R08 policy routing, R09 DNS guard, R10/R11 IPv6) come in through `ReportParts` (collaborator variable, returns none until those modules set it; Python adds them only when `desired` has `ros_policy`/`dns_guard` or the state holds IPv6 plans, which nothing in Go sets yet): each `ReportPart` gives its key + section, statuses for the summary, its in-sync verdict and the route ids it removes from `routes.extra` (policy migrations). Called before the NAT items, as Python builds the policy plan before `_nat_items` (R08 sets `PolicyNATRetiring` there).

Decisions:
- Group request sizes: the layout keeps Python's measure (spaced separators, non-ASCII as `\uXXXX`) through `wireLen` + two constants (124 bytes of an empty edit request with a 10-digit ID, 27 per entry incl. separator), so groups split exactly where Python split them. It is an upper bound of what `encoding/json` sends **with `SetEscapeHTML(false)`** — R05 must encode iKuai requests that way (`TestGroupBytesBoundsTheRequest`).
- `VerifyGroups` checks groups in plan order (role pa, pm, nat; group number), as Python iterated the check dict.
- Fingerprints (R12) hash `sort_keys` JSON computed by the same Go process at preview and apply; `NatContext` JSON keys equal Python's.

Python bug fixed in Go (review R03; maintainer decision: Python bugs are fixed, not reproduced): when `layout` repacks a fragmented family, Python queued the new CIDRs the first pass had already put into an existing group a second time (`sequence = slot values + pending`), so the repacked group held them twice (5 groups × 10 + 1 new → `cf_pa_1` with 52 entries; the next release then "整理重复条目"). Go queues `pending[position:]` (`TestLayoutRepackQueuesNewEntriesOnce`: 51). **Intended corpus difference:** `gen_r03.py` also runs every `ikuai_plan` script with `FIXED_LAYOUT` (Python's `layout` with exactly that line changed, applied by source replacement); the 80 rows whose result differs carry `out_fixed`, which Go must match (and must not match the buggy `out`).

Not chased (functional parity; RouterOS REST and iKuai send strings/numbers): Python `str()` of non-string row values (None → "None", True → "True" in ids/texts; Go treats them as empty, `Row.flag` accepts a JSON true; `Row.number` takes decimal text or a JSON integer, not Python's `int(True)`/float truncation); Python `str.isspace`/`strip` vs `unicode.IsSpace`/`strings.TrimSpace` (U+001C–U+001F); `int()` forms of `distance` beyond `strconv` (underscores, full-width digits); a truthy non-string literal address (Python `IPv4Network(5)`) → Go "undecidable"; the `$`-before-newline match of Python's group-name regex; IPv4-mapped IPv6 text in `CanonAddress`; an entry too long for any request makes Python's `_minimum` loop forever (unreachable: CIDRs and iKuai notes are short) — Go stops counting and reports the blocker; `planned_bypass_conflicts` with an op id not in the rules (Python StopIteration → 500; R12 builds the ops from the same rows) — Go skips it; `GroupRuleCheck.ID` is omitted when empty (Python `"id": None` for a new NAT rule; R12 compares checks in-process).

Acceptance (boundary): `TestR03Corpus` (`r03_corpus_test.go`, `testdata/gen_r03.py` → `r03_corpus.jsonl.gz`, real Python on the test server, synthetic values) — **18959 rows, 0 differ**: assess_nat 3000 and assess_bypass 3000 (60 % plausible RouterOS reads: proxy route with immediate-gw, connected LAN, WAN masquerade by interface or list, exemptions by address/list, the cf list rule; the rest random: missing chain/action, unreadable lists, mangle actions/jumps, routing rules, VRFs, interface lists with include/all-*, broken address lists, invalid gateway/CIDR; retiring ids), planned 701, preserves 2000, inspect_rule 1500, client_sources 256 (every host of a /24), report 2500 (RouterOS with NAT context, iKuai policy mode with IP groups, static/legacy coverage, retired policy rules, DHCP DNS error/disabled, 300-CIDR batches), ikuai_read 1283 (group/rule readback: dup names, lost markers → adoption, ref_count, odd rows), ikuai_plan 1800 (GroupPlanner scripts as plan.py drives them: families up to 33000 CIDRs → "需要 17 个" blocker, fragmented families → repack, frozen/shared groups, hidden refs, notes incl. CJK/emoji/controls, rewrites/deleted rules, cleanup warnings, checks, migration warning), verify 1283, payload/group_bytes 600, span 400, canon 18, digest 300, networks 318. Every Chinese message is compared byte for byte. Mutation checks: dropping the repack, or the first client address, makes the corpus fail.
Unit tests (`r03_test.go`): group size bound vs `encoding/json`, iKuai encoding, `addressExclude` partition, list-rule cache per read, `ReportParts` (summary, in_sync, extra retirement, JSON key), `NATTarget` order.

Python test files → coverage:
- `test_routeros_nat.py` (25): NAT statuses/details of every case (interface lists and negation, legacy all-* groups, unknown egress, connected-gateway egress, missing policy/interface evidence, mangle actions, chainless rules, NAT jumps, uniform/shadowed routes, exact route with disabled backup) → corpus assess_nat/assess_bypass/report; the driver's context reading (bytes/packets dropped, which tables are read) → R04; plans, release checks and fingerprint invalidation (`test_safe_evidence_changes…`, `test_nat_traffic_counters…`, `test_release_*`) → R12.
- `test_routeros_static_selection.py` (16): report route items (table preference, competing/shadowed routes, policy-named routes not extra) → corpus report (RouteEntry active/distance shapes); RouterOS row parsing → R04; release verification/plans → R12.
- `test_router_route_index.py` (5): report batch and NAT report over 1000 routes, no state mutation → corpus report (300-CIDR rows; Go parses each route once in `NewStaticRouteIndex`/`NewRouteEgressIndex`, assessment never writes state); `_verify` after live route changes → R12.
- `test_ikuai_groups.py` (40): layout (2000-entry cap, byte split of long CIDRs/ranges, touching only changed groups, readback spelling, repack, >16 groups), ownership marker, notes in size and payload, shared/frozen groups, hidden refs, lost markers, unmarked hints, nat target choice, references by gid → corpus ikuai_plan/ikuai_read/payload/verify/report; device writes, rollbacks and release flows → R05/R12.
- `test_ikuai_groups_invariants.py` (4): previous-release plans/writes → R12; `test_routeros_state_has_no_ikuai_groups` → R04.

Size (wc -l, non-test, informational): rosnat 857, natlist 121, ikuaigroups 1287, report 460 = **2725** Go lines for 1557 Python lines (1499 + 58 of routeros_nat_list.py).

Left for others: R04 fills `NatContext`; R05 builds `GroupState` (`GroupsFromRows`, `ReadGroupRules`), sends `GroupPayload`/`EntryPayload` encoded with `ikuaiEncode` and `SetEscapeHTML(false)`; R07 uses `InspectNATListRule`; R08 sets `PolicyNATRetiring` and `ReportParts`; R09–R11 add their `ReportParts`; R12 uses `GroupPlanner`, `PlannedBypassConflicts`, `Egress`, `VerifyGroups`.

Review R03 (2026-11-16, adversarial, zero open findings). Findings and fixes:
1. **Medium (Python bug, fixed in Go — maintainer decision):** repack queued new CIDRs twice (paragraph above) → e0695483, `TestLayoutRepackQueuesNewEntriesOnce`, 80 corpus rows with `out_fixed`; mutation check: restoring `+ pending` fails the corpus ("Go reproduces the Python repack bug").
2. **Low (Python emulation):** `present(any)` (Python truthiness incl. `0`/`[]`/`{}`), `rosTrue(any)`/`valText` on numbers, `rosInt(any)` (`int(True)`, float truncation), `rosInterface(any)`/`selectorMatches(any)`, and `IkuaiGroup.Type any` (copied by Python, never read) → fd7ffc4f: `Row.text/set/flag/number/str` typed accessors; `Type` dropped (the corpus strips `type` from Python's `ikuai_read` view). Corpus unchanged, 0 differ.
3. **Info:** private `sortedKeys`/`isStr` replaced by `slices.Sorted(maps.Keys(…))`/`Row.str` (266bc680).
Other Python bugs of this kind (listed, not changed): (a) index exhaustion error kept from the first pass when the repack would fit (`error` not reset; needs all 9999 `cf_<role>_N` numbers taken — unreachable); (b) `_minimum` never returns for an entry too long for an empty group (request thread hangs; Go stops and reports "过长", `TestLayoutErrors`; unreachable with CIDRs/iKuai-length notes); (c) `payload` overwrites one user note with the marker when every entry has a note (the marker must be the first entry's comment on iKuai 4.0.314; no lossless fix); (d) the `$`-before-newline group-name match makes Python treat `cf_pa_1\n` as ours (Go: the user's, `TestLayoutErrors`); (e) `planned_bypass_conflicts` StopIteration → 500 for an unknown op id (Go skips; unreachable).
Checks done: walked every function/branch of routeros_nat.py, ikuai_groups.py, report.py and the 58 list-rule lines against Go (verdict order, `unknown[0]` precedence, egress caching, mangle/VRF/routing-rule gates, interface lists incl. include/exclude/all-*, address-list bisect = overlap scan, `address_exclude` order, client /24 scope, retiring ids + list rule, clone insertion order in planned conflicts, ownership/adoption/dup names, gid-before-name resolution, frozen/shared groups, notes, byte split constants vs `encode_value`/`json.dumps`, cleanup NAT-target keep rule, warning texts, verify order and messages, report summary/in_sync/ikuai pending); Python probe on the test server for the exhaustion message; "not chased" list checked against real readback (RouterOS REST: strings only; iKuai: numeric ids/ref_count, `decode_value`d strings) — none reachable from a device; foreign groups (no marker, dup name, other role) are never in `Owned`, so never edited/deleted/referenced.
Note: at d5c44acd–266bc680 `internal/rules` did not build (d5c44acd swept in another agent's staged deletions; restored by 0e32335e), so those two commits do not build on their own for reasons outside R03; R03 checks were rerun clean at 7ede5c59 (`go vet ./...` and the test command below: all ok).

Commands (private detached worktree, e.g. `~/program/configflow-go-r03r`; never `rt clean` from the shared tree):
- corpus (~2 min): `rt --back server/internal/routers/testdata/r03_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/routers/testdata/gen_r03.py server/internal/routers/testdata/r03_corpus.jsonl.gz'`
- tests: `rt -- bash -lc 'cd server && gofmt -l internal/routers internal/netipx; go vet ./... && go test -count=1 ./internal/routers/... ./internal/netipx/ ./internal/repo/ ./internal/notifications/ ./internal/server/'`

Pitfalls: decoded rows hold JSON numbers as float64 (iKuai ids, ref_count): read fields through `Row.text`/`set`/`flag`/`number`/`str` (RouterOS REST sends strings, iKuai numbers), `intValue` for iKuai integers; `Row` is a map, so slice identity (not `==`) tells reads apart for the list-rule cache.

## R04 — RouterOS REST driver, batch writes

**Status: `verified`** (O4 HR review, 2026-11-17, `parity/review-r04.md`; zero open findings; L5 lab items listed there) · HR · prereqs: R01, R03 · Go: internal/routers, internal/devhttp

O4 review summary: 10 findings, all fixed with tests — High: F1 extra reported IDs in a route console batch panicked and left routes unrecorded (Python IndexError, same class as the NAT fix below), F3 cancellation could abandon an accepted batch or the rollback (rule 4b; `Apply`/`RestoreMany` now run to completion, `ApplyMany` stops only between proven batches); Medium: F2 rollback message texts/prefix, F7 `maps.Equal` panic on array values and composite values collapsing in ownership snapshots, F9 `dhcp_dns_update` without `expected` wrote without proof; Low: F4 batched cleanup after a failed add batch, F5 dynamic NAT message, F6 stop-on-failure marked removed routes skipped, F8 (Python bug) rollback re-creation ignored the 32 KiB script limit, F10 idiomatic clean-ups. Requirements on R07/R08–R12/R14/R15 and the nine L5 lab items are in the review file. Tests added: `r04_review_test.go`, `r04ReviewCases` in the boundary corpus (Python-compared), `TestOnlyIdempotentRequestsAreRetried` in devhttp; routerfake gained the `PatchCreated` fault.

O4 final authoritative run (`.32`, branch `go-opus-review-r04` rebased on `go` e6c87cc1; commits f9449ccb fixes + tests, 1fb51866 review record): `gofmt -l internal/ tools/` → no output; `go vet ./...` → ok; `go test -race -count=1 ./internal/routers/ ./internal/devhttp/` → `ok configflow/internal/routers 78.870s`, `ok configflow/internal/devhttp 3.481s`; `go test -count=1 ./...` → exit 0, last line `? configflow/tools/proddiff/generator [no test files]`; `server/scripts/proddiff.sh` → self-check PASS, `baseline: new=0 stale=0`, exit 0. (The first proddiff run reported 5 NEW value differences in `GET /api/agents/<agent_id>/metrics/summary` averages and exited 1; an immediate re-run on this branch and a run on plain `go` both gave `new=0` — a time-window flake in the agents metrics summary, unrelated to routers. Flagged for the coordinator.)

- `internal/devhttp` is the shared device HTTP client (coordinator decision: no requests emulation; net/http + small shared helper, reused by R05): `New(Config{VerifyTLS, BasicAuth, Username, Password, Cookies})`, `(*Client).Do(ctx, method, url, body any, timeout) (*Response{Status, Header, Body}, error)`, `Response.JSON`, `DecodeJSON` (UseNumber, exactly one value), `EncodeJSON` (compact, non-ASCII as \uXXXX like json.dumps, no HTML escaping), `*Error{Class}` + `ClassOf(err)` + `Timeout()`, `Close()`. No proxy (`trust_env=False`), redirects not followed, keep-alive, timeout = connect timeout + per-read watchdog (no total limit). Basic auth uses Latin-1 when possible and UTF-8 otherwise. Failure classes were measured on the test server with requests 2.31/urllib3 2.8 and replayed by `devhttp_test.go`: refused/DNS/closed/garbage → ConnectionError, dial timeout → ConnectTimeout, header wait/TLS handshake stall → ReadTimeout, TLS failures → SSLError, short body → ChunkedEncodingError, stalled body → ConnectionError. Note: Go may retry an idempotent GET once on a reused connection the device closed.
- `RouterOS` implements R01 `Driver` over `internal/devhttp`: context-aware probe, ping, route/NAT readback, DHCP DNS evidence/read/update, typed operation/undo records, guarded REST writes, ownership checks, verification and compensation. Cancelled writes still attempt compensation with an uncancelled context. Later R07–R11 kinds use `RegisterRouterOSHandler` until their owning modules register.
- `BatchDriver` preserves route operation grouping/order, 32 KiB console-script limit, 100-operation batches, 200-term queries, 500-ID removals, readback and rollback behavior. Its device I/O takes a context; NAT console writes preserve the original matching fields and restore order. `ScriptString`/`ScriptValue` cover console quoting.
- `internal/routers/routerfake` is a loopback-only RouterOS fake with request recording, parsed `/execute` adds, status/body faults, hidden rows and lost replies. `routeros_boundary_test.go` launches the public Python driver from `~/cf-test/.venv/bin/python` against the same fake and compares method/path/query, parsed REST/console bodies, results, error classes/messages and final tables. It does not replay Python internals.
- Python bugs fixed: (1) `_nat_batch` indexed `values[index]` when RouterOS reported more IDs than the batch contained, leaking `IndexError` and leaving confirmed writes behind. Go fails closed, removes only rows whose ownership it proves, preserves the unowned row and returns a rollback error. (2) The shared Go HTTP helper handles non-Latin-1 Basic Auth credentials with UTF-8; Python requests raised `UnicodeEncodeError` for them.
- Normal boundary corpus behavior: no difference found in REST meaning, write order/grouping, retry/rollback/readback, outcomes or Chinese device errors. The extra-ID malformed reply is the intentional correction above.

Coverage map (Python test file → Go boundary test or deferred module):
- `test_routeros_permissions.py` (4) → `TestRouterOSBoundaryCorpus`: 401/403, permission diagnostics for GET/PATCH at 400/500, unrelated read/write failures, non-JSON HTTP errors, ping permission.
- `test_routeros_route_batch.py` (23) → `TestRouterOSBoundaryCorpus` plus `routeros_batch_unit_test.go`: route add/delete and restore, 205-operation batches, 32 KiB script splitting, 200-term readback chunking, transient read retry, refusal fallback, lost replies and stop-on-failure. Release-stage orchestration, static sync and cross-stage rollback → R12; policy migration/rollback cases → R08.
- `test_routeros_dhcp_dns.py` (18) → `TestRouterOSBoundaryCorpus`: selected-network reads/patches, custom option 6 at network/server/lease/matcher levels, unknown codes, RADIUS, unavailable evidence, explicit nulls, malformed network, target drift/overlap/scope, absent DNS, `dns-none`, failed writes and lost-reply compensation.
- `test_routeros_release.py` (14) → driver-level route/NAT round trips, REST errors, readback and uncertain-write cases in `TestRouterOSBoundaryCorpus`; release planning/stages and cross-stage rollback → R12.

Commands (test server, after rebasing onto `go`):
- Focused boundary and batch unit tests: `rt -- bash -lc 'cd server && go test -count=1 ./internal/routers -run "TestRouterOSBoundaryCorpus|TestRouteBatches|TestConsoleScript|TestRestoreManyStops"'` → `ok configflow/internal/routers 10.392s`.
- Full verification after final rebase: `rt -- bash -lc 'cd server && go vet ./... && go test -count=1 ./...'` → exit 0; output included `ok configflow/internal/routers 42.998s`, `ok configflow/internal/yamlx 1.355s`, and final line `? configflow/tools/proddiff/generator [no test files]`.

Commit: `5d3c48ed` (R04 driver, batching, boundary tests and coverage map).

Files:
- `backend/routers/routeros.py` (816), `backend/routers/routeros_batch.py` (599)
- `internal/routers/model.go`, `internal/routers/messages.go`, `internal/routers/routeros.go`, `internal/routers/routeros_batch.go`, `internal/routers/ros_script.go`
- `internal/routers/routerfake`, `internal/routers/routeros_boundary_test.go`, `internal/routers/routeros_batch_unit_test.go`, `internal/routers/testdata/r04_reference.py`

Routes: none.

Remaining module boundaries: R07 replaces plain address-list reads with digest proofs; R08–R11 register their policy, NAT-list, IPv6 and DNS-guard operation handlers. Those modules are not implemented by R04.

## R05 — iKuai driver

**Status: `verified`** (O4 HR review, 2026-11-17, `parity/review-r05.md`; zero open findings; L5 lab items below) · HR · prereqs: R01, R03; shared device client `internal/devhttp` (R04) · Go: internal/routers, internal/routers/routerfake, internal/devhttp

O4 review summary: 5 findings, all fixed with tests (commit `go(routers): R05 review fixes …`) — Medium: F1 devhttp no longer sent credentials from the management URL as Basic auth after switching to the transport (Python requests and Go before R05 did; login-protected reverse proxy in front of iKuai), F2 `route_delete`/`route_add` refusals were plain English errors, not `RouterError` (a release that rolls back only on `RouterError`, as Python's does, would keep the stage's earlier writes); Low: F3 an add or `add_undo` without a name had no ownership proof (a nameless undo deleted the user's nameless rule; Python raised KeyError before writing), F4 (Python bug) `route_restore`/`nat_restore`/`policy_route_restore` wrote the before-image onto whatever row held the id — now refused unless the row still has the record's `tagname`, F5 the batch read-back compared every row with every new route (now indexed by name). The porter's five intended differences, the absent-add proofs, 1008 re-login, rejected/uncertain split, paging, request size, cancellation and the devhttp transport change were checked and are recorded in the review. Tests added: `r05_review_test.go`, `ikuaiReviewCases` in the corpus (now 307 cases, 6 intended differences), `TestURLUserinfoIsBasicAuth` in devhttp. Final run on `.32` (rebased on `go` aeff9d75): gofmt clean, vet ok, `-race` routers/devhttp ok, `go test ./...` ok except the known load-dependent `internal/importer` resource-bounds timeout (ok alone), 9 difftest packages ok, proddiff `new=0 stale=0`.

Files: `backend/routers/ikuai.py` (1179). Tests: `test_ikuai_connection_errors.py` (7), `test_ikuai_dhcp_dns.py` (16), `test_ikuai_scale.py` (29); driver tests in other files: `test_routers.py` (2 iKuai), `test_router_release.py` (`test_encode_matches_ikuai_ui`, `test_ikuai_error_details_surface_in_message`). Routes: none.

Commits: branch `go-port-r05`, the six `go(routers): R05 …` commits on top of `go` (driver; fake + Python harness + corpus + devhttp redirect fix; Go-only HR tests; labels/encoding cases/PROGRESS; `Write` detaches; deterministic fake) — ids change with each rebase, see `git log go..go-port-r05`.

Go layout (no jsonx/pystr/urlx; device rows stay `Row` because iKuai forms are open-ended and must be written back whole):
- `ikuai.go` — `Ikuai` driver (`NewIkuai`, `NewIkuaiWithHTTP`; one goroutine per driver): transport via devhttp (cookie jar, no proxy/redirects, 10 s or restore timeout), Chinese messages per failure class, `_http_error` (redirect target shown as origin only), login (`md5(pw)`, `base64("salt_11"+pw)`, 4.x `code`, 3.x `Result`), `send` (1008 → one re-login and one resend), exported `Call`, `Write` (refusal = `KindRejected`), `ListAll` (pages of 1000, fail closed on drift/short page/ID problems), `Probe`, `ReadState` (static routes, IP groups → `GroupsFromRows`/`ReadGroupRules`, NAT bypass, 端口分流, 当前路由表), `DecodeIkuaiValue`, `IkuaiRequestBytes`, `IkuaiTagname`, `IkuaiRouteFields`, `IkuaiDHCPStaticFields`.
- `ikuai_dhcp.go` — `ReadDHCPDNS` (complete verified 4.0.120 form, blockers), guarded `dhcp_dns_update`/`dhcp_dns_restore` with full read-back and local restore.
- `ikuai_apply.go` — `Apply` (all kinds of ikuai.py: routes, NAT, 端口分流, IP groups, `add_undo`, DHCP), `Batch(ctx, rollback, fn)` (Python's `batch()` context manager: one static_rt snapshot, new routes proven before the next other write and at the end, one group read until a named group changes), `IkuaiRoutePayload`, `IkuaiOversizedRequests`.
- `model.go` — `RouterError.Undo` (Python's `error.undo`: the undo of a write that may have happened; the release must record it like a finished op's undo) + `UndoOf(err)`; `KindRejected`.
- `messages.go` — every iKuai Chinese message/label; `routerfake/ikuai.go` — loopback iKuai fake; `testdata/r05_reference.py` — Python reference harness.

Design decisions:
- The release-facing API stays JSON at the driver boundary (`Driver.Apply(raw) → raw undo`, like R04); ops decode into one typed `ikuaiOp`, undo records are typed structs whose JSON keys equal Python's dicts.
- Python mutated undo dicts it had already returned (`_confirm_added` and `_cf_refs` set `confirmed=False` when a complete read proved an add never happened). Go returns undo JSON, so the driver remembers such proofs (`absent`, per driver = per release) and `add_undo`/`ikuai_group_undo` consult them; a new add of the same name clears the proof. Corpus cases `policies_kept_until_routes_exist`, `swallowed_add_alone_fails_at_batch_end`, `group_add_swallowed_then_unreadable` compare the resulting rollback with Python.
- `IkuaiRequestBytes` measures the body devhttp sends plus one byte per JSON separator, i.e. exactly Python's `json.dumps` size (checked against Python in `request_bytes`, incl. the 5706/5712 device probe); `GROUP_MAX_BYTES`/96 KiB keep their measured meaning and Go never sends more than that measure.
- devhttp now calls the transport directly (cookie jar applied explicitly): `http.Client` failed on a 3xx with an unparseable `Location` before `ErrUseLastResponse` (Go reported "无法连接 iKuai"); R04's RouterOS corpus still passes; `TestRedirectReturnedAndCookiesKept`. Because `http.Client` is no longer used, `Do` itself sends URL userinfo as Basic auth when the client has no credentials (O4 F1, `TestURLUserinfoIsBasicAuth`).

Python bugs fixed in Go (each an intended corpus difference with an assertion on both sides, `ikuaiFixedCases`):
1. (Medium, HR) `route_comment` whose reply was lost carried no undo: the user's route could stay marked `cf-…` (then treated as ours by later releases). Go attaches the `route_restore` undo.
2. (Medium, HR) `route_delete` deleted first and then crashed computing the undo from an unreadable network, leaving no undo. Go computes the undo first and refuses before deleting.
3. (Medium, HR) `_rebind` kept a gid that now belongs to another (the user's) group when ours was gone, writing a rule that references the wrong group. Go refuses (`iKuai IP 分组 … 不存在，无法恢复规则引用`).
4. (Low) A write reply without a `code` was treated as a refusal (`_Rejected`, no undo) although nothing proves it was refused. Go treats it as a lost reply (`iKuai 调用 … 返回格式无法识别` + undo).
5. (Low) requests raised ValueError (HTTP 500) on a redirect with an unparseable `Location`; Go explains it like any other redirect.

How the R04 review classes are avoided by construction:
- F1 panic on extra IDs / index out of range: no positional indexing of device replies; every device value is read through type assertions (`Row.text`, `jsonInt`, `ikuaiAddressField`) and lists are compared as strings (`nat_update` add_dst no longer uses `==` on interface values). `TestIkuaiMalformedDeviceDataNeverPanics` feeds every JSON shape into every field of every table and runs ReadState/ReadDHCPDNS/Probe and 15 op kinds.
- F3 cancellation abandoning accepted work / rollback: `Apply` and the end-of-`Batch` read-back run on `context.WithoutCancel`; each request keeps its own timeout; the DHCP local restore runs inside `Apply`. `TestIkuaiCancellationFinishesStartedWork` (route add, batch end, DHCP restore after a lost reply, rollback).
- F2/F5 messages and prefixes: every Chinese message is compared byte for byte with Python in the corpus (incl. `；DHCP DNS 局部撤销失败：`, `已提交但读不回新对象：`, group/use messages); a one-character mutation of the 1008 message fails 3 cases.
- F7 array fields compared as empty / `maps.Equal` panics: all row comparisons are structural (`jsonEqual`, `slices.Equal` on typed entries).
- F9 write without proof: `dhcp_dns_update` requires `expected` equal to a fresh read (`dhcp_expected_missing`); group writes require the previewed digest; deletes/undos of adds require the unique name and all recorded fields.
- F8 unbounded scripts: iKuai has no scripts; request size is the analogue — plan blockers use Python's measure (`IkuaiOversizedRequests`), group writes are split by R03, an oversized request is refused by iKuai with an empty reply, which Go reports like Python (an edit then carries its idempotent undo, an add a name-based undo).
- F6/F10: no batched-delete status here; helpers reused (`ikuaiEncode`/`GroupPayload` from R03, `CheckedDNSTarget` from R01, `uniqueStrings`, `netipx`), no private copies.
- Credentials: only the failure class reaches messages; redirect targets are reduced to their origin; `TestIkuaiErrorsNeverCarryCredentials` checks password, its MD5, the salted base64, URL secrets and device bodies.

Not chased (functional parity; iKuai 4.x sends ints/strings): Python truthiness/equality of bools and floats in ints (`True == 1`, `0.0 == 0`, `type(x) is int`), `str(None)` in device-derived texts (missing `prio` in 当前路由表 comments, missing detail fields), IPv4Address(int) for a numeric gateway/DNS, `unquote` replacement granularity for invalid UTF-8, Python crashes on malformed ops (Go returns `iKuai 操作格式无法识别` or a plain error).

Acceptance (boundary): `TestIkuaiBoundaryCorpus` (`ikuai_boundary_test.go`, cases in `ikuai_cases_test.go`) — **305 cases: 300 compared with 0 differences, 5 intended differences** (stable over repeated runs; the fake reports unknown fields in sorted order) — the real Python `IKuaiDriver` and the Go driver against the same loopback fake; compared: outcome (results, undo records as returned, error class/message/carried undo, rollback results), complete request sequence (login and every show/add/edit/del with parsed bodies), final tables. Groups: session/HTTP/redirect/status/body/call-code/1008/write refusal/encoding/size (≈80), paging and read-back (≈45), DHCP (≈95), release flows with rollback incl. lost replies, swallowed writes, hand edits (≈35), IP groups and rebinding (≈35), values/tagnames, oversize blockers, request bytes, transport messages. Mutation check: disabling the confirm-before-next-write rule, the absent-add proof, or changing the 1008 message → 11 failing cases.
Go-only (`ikuai_test.go`): cancellation, malformed data, credentials, request-size bound. `TestRedirectReturnedAndCookiesKept` in devhttp.

Python test files → coverage:
- `test_ikuai_connection_errors.py` (7): https/other redirects, credentials/paths omitted, same-origin/unknown targets, redirect after login → `redirect_*`, `fixed_unparseable_redirect_explained`, `TestIkuaiErrorsNeverCarryCredentials`; HTTP reasons → `http_status_*` (14 statuses); transport classes → `transport_messages`, `transport_restore_timeout_message`, `write_lost_reply`; `test_probe_api_persists_explanation_for_router_list` → R15 (route), its driver message here.
- `test_ikuai_dhcp_dns.py` (16): full read, update+undo of the complete form, blank slots, drop/corrupt/lost/refused writes, local rollback failure, failed read-backs, undo with reads down, preview drift (+ missing `expected`, changed id), scope overlap, unverified fields (22 variants read+write), missing edit field, static binding overrides/evidence (11 variants), incomplete lists, invalid DNS (11), missing/odd scope (8) → `dhcp_*`.
- `test_ikuai_scale.py` (29): every page / page drift / active table → `read_state_every_page`, `read_state_rows_change_between_pages`, `read_state_unreadable_active_table`, `read_state_no_active_table`, `list_all_*`; dhcp_static pages → `list_all_dhcp_static_pages`; constant reads for add/delete/adopt, hand edit during adoption, policies/covered routes kept until new routes exist, rollback after lost re-add, one read before deletes, batched = unbatched writes, rollback of 300 routes, route not read back / reply lost / refused / lost without route / changed after add, policy not read back, NAT reply lost, re-adds not read back → `routes_*`, `policies_kept_until_routes_exist`, `covered_route_kept_until_wider_exists`, `new_routes_read_once_before_deletes`, `mixed_stage_batched`/`_unbatched`, `rollback_*`, `route_*`, `refused_route_needs_no_undo`, `lost_request_without_route_is_nothing_to_undo`, `policy_added_not_read_back_is_rolled_back`, `nat_reply_lost_is_rolled_back`; session expiry → `write_relogin_once`, `write_1008_twice`, `show_*`; empty reply → `reply_body_*`, `write_oversized_body_gets_empty_reply`; oversize/limit → `oversized_blockers`, `request_bytes`. Plan/report/execute parts (`_plan`, `build_report`, `execute`, stage checks, `test_large_destination_sets_publish_in_requests_that_fit`, `test_router_plan_for_routeros_is_never_size_checked`, `test_routes_beyond_the_first_page…` report part) → R12 (and R03 for the report).
- `test_routers.py` iKuai login/probe/state, 3.x → `probe_ok`, `read_state_details`, `probe_3x_version`; `test_router_release.py` encoding/details → `write_encode_matches_web_ui_test`, `write_error_details_surface` (rest of that file → R12).

Requirements on later modules:
- R12: record `UndoOf(err)` of a failed op exactly like a finished op's undo (Python `finished(op, error.undo, ok=False)`); wrap each stage in `Batch(ctx, false, …)` and the rollback in `Batch(ctx, true, …)`; skip empty undo records (`{}`); use `IkuaiOversizedRequests` for plan blockers; use one driver per release (the absent-add proofs are per driver).
- R12 (from the O4 review): treat **every** error from `Apply` as a failed stage and roll back (Python rolled back only on `RouterError`).
- R06/R16/R17: use `Call`, `ListAll`, `Write` (`KindRejected` = `_Rejected`), `DecodeIkuaiValue`, `IkuaiRoutePayload`, `IkuaiRouteFields`, `IkuaiDHCPStaticFields`; the fake (`routerfake.Ikuai`) already serves dhcp_static/dhcp_server and accepts string ids. `Write` detaches from cancellation but `Call`/`ListAll` do not: an entry point that writes and reads back must detach its context itself (o3 §2.5).

L5 lab items (only a real iKuai 4.x VM can prove; difftest L5-A/B/C):
1. Session: login replies of 4.0.3xx (code/Result), 1008 after a router restart or a second login, cookie name/path; account groups without read (1001)/write (1002) per feature and `Label`; funcs a firmware lacks (1003).
2. Paging: `limit=offset,1000` and `TYPE total,data` on static_rt/static_rt_table/nat_rule/stream_ipport/route_object/dhcp_server and `static_total,static_data` on dhcp_static with >1000 rows; whether totals change under concurrent UI edits.
3. Forms and encoding: the web UI encoding (`%`, whitespace, quotes, `$`, backtick, `&`, `;`, backslash, `|`, comment line breaks) round-trips on every form; netmask display suffix; runtime fields (`status`, `ref_count`, `group_id`, termname/hostname/ip_addr_int) accepted or refused in edits; edit = full replacement.
4. Request size: empty 200 above ~98 KB on 4.0.314 (and newer firmware), restore timeout for >1000 literal entries.
5. Names: duplicate tagname/group_name refused (3001), 1–15 characters, IP groups keep entry comments but no group comment, gid reuse after delete, refusal to delete a referenced group (incl. hidden ACL/流控 references).
6. Lost replies: pull the management link after a write is accepted (add/edit/del of routes, NAT, 端口分流, groups, DHCP) — undo by name finds and removes only our object; DHCP before-image restore.
7. 端口分流/static/NAT: next-hop rules with the gateway excluded as source, group references, 当前路由表 leftovers after static route edits (StaleRoutes warning).
8. DHCP DNS: dns1=dns2 written with the complete form, clients renewing get the new DNS, static binding DNS overrides, relay-only/interface-limited services.
9. TLS: HTTPS management with self-signed certificates, verify on/off.
10. (O4 review) Whether iKuai reuses the id of a deleted static_rt/nat_rule/stream_ipport row (and the gid of a deleted route_object) for the next add; if it does, R12 should prove the name before a `*_delete` undo of an added rule.
11. (O4 review) Management address with credentials behind a Basic-auth reverse proxy (`http://user:pass@host`): login, session cookie, 401 message.

Commands (from the worktree root; iterate on .63, final run on .32):
- corpus + Go-only tests: `RT_HOST=sunbroke@192.168.0.63 rt -- bash -lc 'cd server && go test -count=1 -run TestIkuai ./internal/routers/'`
- race: `rt -- bash -lc 'cd server && go test -race -count=1 ./internal/routers/ ./internal/devhttp/'`

## R06 — device route editor

**Status: `verified`** (O4 HR review of the Codex port, 2026-11-17, `parity/review-r06.md`; zero open findings; HTTP/L1 acceptance stays with R15; L5-A items listed there) · HR · prereqs: R04, R05 · Go: internal/routers

O4 review summary: 5 findings, all fixed with failing-first tests — **1 high** (below the ≥ 2 fallback threshold): F1 RouterOS manual writes and compensation passed timeout 0 (= none in devhttp) on a detached context, so an unanswered write hung forever holding the router lock, unreconciled; hidden by the corpus's `r06ShortHTTP` timeout override. Medium: F2 redaction hid every message containing the *username* (a RouterOS user `api` lost the permission help) — only passwords are secrets now; F3 (R04 code) every RouterOS GET/DELETE sent a JSON body `null` from a typed-nil `Row` — no body now, like requests. Low: F4 iKuai non-string `interface` reason; F5 typed `PlannedRoute` instead of JSON-map merging, no shadowed `copy`. Tests: `r06_review_test.go`, corpus case `ikuai/readonly/interface_number` (172 cases). Final .32 run: gofmt empty; `go vet ./...` ok; race routers 264.823s / devhttp 3.488s; `go test -p=4 -count=1 ./...` exit 0 (routers 163.245s); 9 difftest packages ok; proddiff self-check PASS, new=0 stale=0; mutation gate result/request/ownership DETECTED.

Files:
- `backend/routers/route_editor.py` (164), `backend/routers/routeros_routes.py` (283), `backend/routers/ikuai_routes.py` (237)

Tests: `test_route_editor.py` (14), `test_routeros_routes.py` (24), `test_ikuai_routes.py` (24)

Routes: none here (`/routes`, `/routes/plan`, `/routes/apply` handlers are R15).

Implemented: `route_editor.go` (typed intent/preview/result, strict RawMessage request boundary, public snapshot revision, connection/intent fingerprint, detached execute, exported `RouterOperationLock` using lockx L2), `routeros_routes.go` / `ikuai_routes.go` (read-only classification, complete preflight/readback, conservative identity-checked compensation). Implementation commits rebased onto go (O3-1/O3-2 + A19): 984dc0e8, a797a63c, b20c0cc1. No new dependency, jsonx/pystr/urlx, flock or production goroutine.

Design / every R04, R05 and A19 HR issue class: `design/r06-route-editor.md`. Per-Python-file mapping, matrix coverage, accepted differences and L5-only list: `parity/r06-coverage.md`. `TestR06PythonBoundary`: **171** real Python-vs-Go loopback cases; compare result/errors, full requests, empty external undo records and final tables. `r06_safety_test.go`: cancellation after accepted writes and through compensation, cancelled entry, L2 cross-profile identity, fingerprint/secret/runtime stability, credential echo hiding. Three compiled implementation mutations detected result/request/ownership differences.

After rebase: added duplicate-name zero-write proofs (2 explicit safer-than-Python cases), kept recovery/uncertain status visible when redacting secrets; migrated transitional connection lock to shared `lockx.Keyed` (L2), propagated held context, added scope/order assertions. Authoritative .32 gates passed before the final rebase (pre-rebase afdeb804, R06/devhttp/lockx trees identical at 3b6b05c6): gofmt empty; `go vet ./...` exit 0; race routers **241.234s** / devhttp **3.542s**; full `go test -p=4 -count=1 ./...` exit 0 (routers **224.946s**, importer **1.602s**); all 9 tagged/enabled difftest packages passed; proddiff self-check PASS, coverage_regressions=0, baseline **new=0 stale=0**. Exact commands/output tails and added-file sizes: `parity/r06-acceptance.md`. Implementation commits: 984dc0e8, a797a63c, b20c0cc1, 3b6b05c6. Post-final-rebase format/vet and all 171 R06 cases passed again (**33.646s**); R06/devhttp/lockx implementation trees match the full-gate version. Fresh Opus HR review remains (only reviewer may set verified). **HTTP/L1-before-validation acceptance is pending R15**: no `internal/api/routers` or R14 job-lock owner exists on this branch. R020 acquires L1 before lookup/type/body checks (busy ->409 before malformed ->400), then L2; R021 L2 waits; R019 Python read is unlocked. Do not count these as completed HTTP comparisons.

Commands (all loopback/network on .32):
```
rt -- bash -lc 'cd server && go test -count=1 -run TestR06 -v ./internal/routers/'
rt -- bash -lc 'cd server && ~/cf-test/.venv/bin/python internal/routers/testdata/r06_mutations.py'
```
Latest targeted output: `171 real Python boundary cases`, `PASS`, `ok configflow/internal/routers 33.646s`; remote `go vet ./...` passed. Mutation output: result/request/ownership each `DETECTED (compiled, real Python boundary mismatch)`.


## R07 — RouterOS NAT address lists, address-list sync, test helpers

**Status: `verified`** (O4 Opus HR review 2026-11-17, [review-r07.md](review-r07.md): 0 high, 1 medium, 4 low, all fixed; reviewed together with the R06 fixes) · HR · prereqs: R03, R04 · Go: internal/routers, internal/routers/routerfake

O4 review summary: Medium F1 device diagnostics hid the username, used a private redaction copy and, in the shared
`request` path, defeated R06's verified route-edit secret message once combined → password only via `internal/redact`,
`routeSafeError` also hides transport-redacted echoes. Low F2 ranges normalised to CIDR member keys unlike Python `_key`;
F3 quadratic policy-migration overlap (now bisection) and list scans; F4 reuse scope aliased downloaded rows;
F5 member read-back compared lengths, not sets. Tests: `r07_review_test.go` (incl. timeouts + raw bodies of every R07
request, fuzz `FuzzR07DecodeAddressRow`), Python-compared `r07_review_boundary_test.go`. Fake: R07's external-write
helper renamed `routerfake.ExternalWrite`/`ApplyExternal` (R06 owns `routerfake.Mutation`). Codex-port quality: no
high finding, fallback not triggered. Extra L5 items 1–4 in the review file.

Files: `backend/routers/routeros_nat_list.py` (1016), `ros_address_lists.py` (297), `backend/routeros_test_helpers.py` (109).

Commits: 9df951e5 (scoped reads/proofs/fake), 2f1d0ee6 (typed plan, lazy state reads), fa66e0e2 (bounded address writes, NAT migration/compensation), 8ee4afe7 (sync helpers, external-write/restore boundaries), b335a09e (ownership/cancellation/script safety), 8e35b49a (R12 typed contract), bb73aee3 (four mutation classes), 4261591f (strict null fields). No push; fresh Opus must review before `verified`.

Full handoff, R04 F1–F10 prevention, Python test-file mapping, safety corrections and L5 list: [progress/R07-nat-lists.md](progress/R07-nat-lists.md).

Typed API: `PlanNATLists`, `NATRemovalsFirst`, `RestrictNATEgress`, `NATPolicyMigrations`, `VerifyNATLists`; `RouterOS.NATResources`, `NATMemberUpdate`, `NATMigration`; registered `nat_list_sync` / `nat_list_restore` handlers. Shared `ReadAddresses`/`FetchAddresses`/`RememberAddresses`/`ExpectAddresses`/`ReuseAddressReads`/`LocateAddresses`/`OccupiedAddressNames`; address batching `CreateAddresses`/`DeleteAddresses`, `ROSResource`/`NATStep`/`ROSIDs`. No new dependency or jsonx. R08 fills typed `NATPolicyEvidence`; R12/R14 own serialized release/worker orchestration and the existing L1/L2 locks. R07 has no route or loop.

Acceptance: real Python driver against the same loopback fake; compare plans, Chinese errors, undo journals, requests and final ordered tables. Independent Python test-helper interpreter verifies captured scripts. Negative mutation test deliberately changes Go result and must fail. All network fakes and final checks on .32; no real device contacted.

Final .32: gofmt empty, vet pass; race routers 391.762s / devhttp 3.471s; full suite and all 9 CONFIGFLOW_DIFF=1
packages pass; proddiff confirmed exit 0 (`new=0 stale=0`); four deliberate mutations detected. Strict null vs
missing fields are retained in comparisons. Detailed commands/tails and the R12 §2.4 mapping are in the handoff.
No push. L5 and fresh Opus review remain required before production / `verified`.

## R08 — RouterOS policy routing

**Status: `verified`** (O4 Opus HR review 2026-11-17, [review-r08.md](review-r08.md): 0 high, 1 medium, 5 low, all fixed) · HR · prereqs: R07, C05, C26 (`normalize_policies`, present in `internal/oxidns`) · Go: internal/routers

O4 review summary: Medium F1 the porter's moved-member fix covered only R08's preflight; the shared
`restoreDeleteAddresses` (R07 `RestoreNATLists`) still skipped a fresh member moved into another list as
"already deleted" → it now locates missing IDs and keeps/reports them. Low F2 `SubtractNetworks` returned an
error-fallback empty list on mixed families (now per family); F3 `prepare_static` `[]` dropped by omitempty;
F4 view role keyed by an empty list name; F5 vanished console-restored route used the uncertain-write text
instead of Python's read-back mismatch; F6 comments still promised cancellation-proof rollback after the
whole-driver `WithoutCancel` → `lifecycle.Detach` move (behaviour accepted; R12 §3.3 wording to update).
Tests `r08_review_test.go` (all five fail on the hand-back). Codex-port quality: no high finding, fallback
not triggered. R12/R14 requirements, R09 merge overlap and two extra L5 items are in the review file.

Files:
- `backend/routers/ros_policy.py` (520), `backend/routers/ros_policy_write.py` (782), `backend/routers/ros_policy_view.py` (47)

Tests: `test_ros_policy.py` (32), `test_ros_policy_activation.py` (6), `test_ros_policy_advisories.py` (9), `test_ros_mode_transitions.py` (13), `test_ros_table_flags.py` (12), `test_ros_preview_freshness.py` (2), `test_policy_editors.py` (6; not claimed by config — its `/api/rule-library/manual/draft-content` route half → C33)

Routes: none.

Notes for porter: `inspect_desired`, `snapshot`, `owned_rows`, `digest`, `config_row`, `network_overlap`, `subtract`, write steps/journal/rollback (`run_steps`, `_create`, `_delete`, `_restore_step`, `_writable`, `_read`), table/FIB flag normalisation, `read_policy_objects`. `ros_policy.digest` is used by R15's policy workspace.

R07/R12 interface follow-up (required in R08): `plan_model.go` already defines the shared `Op`/`OpHead`/`Stage`
contract and stage keys; extend those definitions rather than redeclaring them. Once `*ROSPolicyReplace`
provides `PrepareStatic []Op`, replace `nestedNATMigration` with typed traversal and **delete
`internal/routers/natlist_policy_adapter.go`** (header: R08 落地后移除). Connect R07's temporary
`NATPolicyEvidence` reads to the same `ROSPolicyPlan.Check` / previous-spec resources R08 owns, so the NAT and
policy planners cannot use different migration evidence. See R12 design §2.4 “Clarified in R07”.


Implemented: `rospolicy_{model,read,plan,write,restore}.go` (plan/inspection/view/guarded write,
shared journal and rollback), plus native typed R07/R12 integration and real-Python loopback tests.
No module feature omitted; no route, lock, goroutine, dependency, push or real-device access added.
The JSON adapter is deleted, the temporary NATPolicy state field is removed, and all NAT evidence
comes from the same ROSPolicyPlan.Check. Shared writes retain 10 s device timeouts and use
lifecycle.Detach. Commits: `6b40ef58` (implementation), `44b38c8b` (full report retirement proof).

Python boundaries cover plans, full report, ordered writes, journals, raw stable manifests, views,
final device tables, 200/201/4096 members, nested NAT-list preparation/undo, legacy hashed names,
FIB present-empty/wire aliases, activation/runtime drift and failed/uncertain writes. The final
nested-NAT case also checks that the recreated policy is a no-op on the next preview.

Final gate evidence (only rt; .64 race/fuzz/proddiff, .32 full/difftest):
- `RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -l internal/ tools/ cmd/ && go vet ./... && go test -race -p=4 -timeout=60m -count=1 ./internal/routers/... ./internal/routersplit/... ./internal/devhttp/...'` → gofmt empty, vet clean; routers `295.892s`, routersplit `4.151s`, devhttp `3.448s`, all ok. After the final report fix: `go test -race -p=4 -timeout=60m -count=1 -run "^TestR08" ./internal/routers` on .64 → `ok 43.055s`.
- `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -l internal/ tools/ cmd/ && go vet ./... && go test -p=4 -timeout=60m -count=1 ./...'` → all pass; routers `241.707s`, routersplit `0.972s`, cmd/configflow `104.650s`, final line `ok configflow/tools/proddiff/generator 0.010s`.
- Final nested-list case: `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -p=4 ./internal/routers -run "TestR08PythonBoundary/static_nat_list_nested_rollback" -count=1'` → `ok 1.741s`.
- `RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && python3 scripts/r08-mutation-gate.py && go test -p=4 -run "^$" -fuzz "^FuzzR08PolicyInput$" -fuzztime=60s -parallel=4 ./internal/routers && go test -p=4 -run "^$" -fuzz "^FuzzR07DecodeAddressRow$" -fuzztime=60s -parallel=4 ./internal/routers'` → result/journal/request/device-table all DETECTED, `4/4 detected; sources restored`; R08 `159951 execs`, `PASS`, `ok 61.019s`; R07 `1330511 execs`, `PASS`, `ok 60.108s`. Mutation-gate result matching accepts the explicit result/write/device comparison diagnostics; build failures never count.
- `RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'GOFLAGS=-p=4 server/scripts/proddiff.sh'` → `self_check: PASS`, `coverage_regressions: 0`, `new: 0`, `stale: 0`; final `baseline: new=0 stale=0`. Existing snapshot only, isolated namespace; no production fetch or data committed.
- All 12 difftest packages passed: `RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/routers -- bash -lc 'cd server && gofmt -w internal/routers && CONFIGFLOW_DIFF=1 go test -p=4 -timeout=60m -count=1 $(go list ./... | rg /difftest)'` → agents `152.535s`, aggregations `50.553s`, subscriptions `122.698s`, devicenotes `1.055s`, discovery `6.281s`, health `15.522s`, httpx `3.550s`, mosdns `4.186s`, refresh `5.747s`, subs `2.902s`, substore `13.001s`, final `ok configflow/internal/webdav/difftest 26.858s`.

Intentional differences / findings for O12:
1. **Medium, Python bug fixed:** rollback could mistake an address ID moved into another list for an already deleted member and report success. R08 rollback preflight locates missing journal IDs globally, checks complete configs before deletion, preserves the external edit and reports incomplete rollback. TestR08PreviewAndOwnershipGuards proves it.
2. **Safety corrections:** malformed/missing/duplicate identity is refused instead of Python's KeyError or accidental match; a failed rollback proof stops later deletions and retains dependent replacement protection (shared R07 rule). Route migration guards the whole batch before deleting any row, rather than deleting an earlier prefix before a later guard fails. Existing R04/R07 fresh-ID, dynamic-row, uncertain-write and script-size safety fixes are reused.
3. **Native opaque revision:** ROSPolicyDigest uses encoding/json (N8 D2), so non-ASCII/number-text hash bytes may differ once; typed fields, user messages, generated device semantics and raw unknown manifest fields are retained. No Python encoder/emulation added.
4. **No open finding.** The port's own medium report/NAT-retirement wiring error was caught and fixed by the complete BuildReport differential below. (Hand-back state; the O4 review above set `verified`.)

Self-audit follow-up: full BuildReport differential `report_policy_nat_retirement` caught a
medium integration defect before hand-back: PolicyNATRetiring must be the NAT resources in the
actual replacement op's Owned list, not NAT migrations from Check. Corrected to Python's rule;
complete report JSON now compares against real Python (including NAT status/summary on withdrawal).
Targeted .32 command: `go test ./internal/routers -run "TestR08PythonBoundary/report_policy_nat_retirement|TestR08NamesGatewayAndReportIntegration" -count=1` → `ok configflow/internal/routers 0.371s`.

### R08 HR issue-class checklist

- R04 F1 malformed/extra reply IDs: uses R07 createResource/CreateAddresses fresh unique ID proofs and R04 bounded consoleAdd; unknown surplus rows are not journaled; FuzzR08PolicyInput rejects malformed manifest/lookup shapes without panic.
- R04 F2 messages: new Chinese text is centralized in messages.go; real-Python boundary compares errors without blanket message stripping. Only pre-existing driver error classes/native internal parse texts differ as documented by R04.
- R04 F3 disconnect: ApplyROSPolicy/RestoreROSPolicy and shared R04/R07 write/restore entry points use lifecycle.Detach; every I/O still passes rosTimeout. Production timeout recorder, real 10 s silent peer and cancelled-request tests use production devhttp.
- R04 F4 batching: reuse CreateAddresses/DeleteAddresses; policy route migration uses 200-term reads/500-ID removes; restore reuses consoleAdd with 100 rows and 32 KiB accounting. 200/201/4096 members and five-route rollback are Python-compared.
- R04 F5 dynamic rows: writableResource rejects dynamic/timed members; all destructive paths guard full raw config; route restores check active state before accepting a non-main route.
- R04 F6 journal placement: append restore before DELETE/remove; operation undos include UndoOf(err); reverse execution restores old path before discarding replacements; remapped IDs refresh the raw previous manifest, including unknown fields.
- R04 F7 composite values: sameRow deep comparisons and cloneAddressRow in configROSRow/owned results; native decoder keeps json.Number. No maps.Equal over arbitrary device values; proof baseline copy tested with nested maps/slices.
- R04 F8 limits: shared 16 KiB/200 member and 500 remove limits retained; route restore splits at 32 KiB UTF-8 bytes. No short size/timeout seam in R08 tests.
- R04 F9 missing proof: missing/empty IDs or config and duplicate manifest/live IDs refuse; all non-route preview dependencies compare before writes, migration IDs guard exact config, each delete re-reads; table/chain/list foreign references retain the bundle.
- R04 F10 helpers: reuse configROSRow, sameRow, payload/writableResource, valuesMatch/ComparableValues, address proof cache, ScriptString, prefixOverlap, addressExclude and consoleAdd; no private redaction/network parser/YAML/JSON compatibility engine.
- R05 class 1 shared clients: no HTTP transport change; production Basic auth/raw Content-Type/no-body and 10 s parameters are checked. Existing devhttp no-proxy/no-redirect/read retry tests run again on the combined tree.
- R05 class 2 error types: write/rollback failures are RouterError (including KindUncertainWrite/KindRollback); native parse failure at the internal driver contract maps to messageOperationFormat.
- R05 class 3 non-empty identity: manifest/undo validation and guardPolicyIn reject missing identity before destructive writes; a name/comment never claims ownership.
- R05 class 4 ID reuse: before-images are used only after full stable equality; missing restore IDs may be recreated, changed IDs are preserved; place-before is remapped and its current existence re-proven.
- R05 class 5 readback indexing: ownership builds path+ID maps once; scoped member reads/batched route reads replace per-member whole-table scans; overlap reuses the R07 binary index and subtraction is a sorted sweep.
- R05 class 6 refused vs uncertain/cancellation: shared PUT logic distinguishes an answered refusal from transport/5xx/invalid-ID uncertainty, journals one provable fresh candidate only, and never retries an uncertain write. create_*_lost_* compares real Python compensation.
- R06 standing timeout/raw checks: TestR08RequestsKeepProductionTimeoutAndRawBodies checks actual body bytes and headers plus all production timeouts through apply and rollback; TestR08DeviceTimeoutActuallyExpires observes the unchanged ten-second watchdog. Existing R04/R06/R07 suites included in final routers race run.
- R06/R07 redaction: no new redaction logic; only existing password/token helpers hide secrets. Username/diagnostic text and ownership rows remain intact; existing R06/R07 password-only tests rerun.
- R07 F2 range identity: NAT migrations call shared NATPolicyMigrations/natKey; an address range remains text, never a CIDR ownership key (explicit range assertion).
- R07 F3 scale: no pairwise prefix re-parsing; ROSPolicyNetworkOverlap delegates to prefixOverlap; SubtractNetworks collapses once then sweeps exclusions; large bundle boundary covers repeated reads.
- R07 F4 aliasing: driver manifests are copied on setter/getter; configROSRow and owned rows deep-copy composite values; address proof caches stay on R07's shared clone implementation.
- R07 F5 set equality: shared NAT Verify keeps set comparison. Policy ownership rejects duplicate IDs first, so count completeness is applied only to a uniquely indexed manifest; rule order is separately compared as an ordered ID sequence.
- R07/R12 interface: extend Op/OpHead/Stage in plan_model.go; typed PrepareStatic traversal deletes natlist_policy_adapter.go; NATPolicy temporary state field is removed, NAT evidence is derived exclusively from ROSPolicyPlan.Check + its raw previous manifest. ManifestHolder/Manifests/CIDRGateway/NATChecks follow the approved R12 design.
- A19 F1 graph expansion/cycles: no new YAML traversal; settings use oxidns.ProfileFromJSON/ReadPolicies/NormalizePolicies and the yamlx facade; dynamic device values originate in bounded JSON trees, not expanded alias graphs.
- A19 F2 document wrappers: no private YAML entry point; shared readers own document/shape validation. Invalid native JSON/manifest input is rejected at the parser boundary.
- A19 F3 Unicode whitespace/content: content extraction remains shared ComputeDesired (R01/C05), with no new content sniffing regex or scanner. The version regex parses only RouterOS version digits.
- O3 locks/Spawn/Detach: R08 introduces no locks/loops/goroutines; one driver per serialized release, caller R12/R14 holds L1/L2. Pure inspection stays cancellable; write owners detach with shutdown cancellation retained.
- N7/N8: encoding/json + typed structs/raw unknown manifest fields, shared yamlx/oxidns; no jsonx import/new dependency.

### R08 coverage and L5 boundary

Every Python named test is mapped in [r08-test-map.md](r08-test-map.md): 32 policy, 6 activation,
9 advisory, 13 transition, 12 table flag, 2 freshness, 2/6 editor tests owned by R08 (four draft-content
route tests belong to C33). R07's 16 bulk tests are also mapped to shared and new boundary coverage.
R12/R15 retain HTTP auth/profile/revision/SQLite/total fingerprint and Agent cross-stage orchestration;
R08 has no HTTP route, so no fake handler is claimed as a Flask differential.

L5-B / L5-I **not run here** (loopback only): CHR 7.22+ policy-rules/unmarked jump/FIB wire forms;
real active/invalid settle and actual gateway/egress; source/direct/gateway packets and LAN/WAN NAT
continuity during static↔policy switch and rollback; FastTrack/raw/VRF/foreign references; real console
16 KiB/32 KiB limits and 10 s hardware budget, ID assignment/reuse, lost replies and 20k member scale.
L5-A transport/permission and batch-order items from R04/R07 remain required. This port touches no PVE
or real device and claims no packet-path/firmware proof.


Hand-back file sizes (lines): rospolicy_model.go 303, rospolicy_plan.go 451,
rospolicy_read.go 405, rospolicy_restore.go 114, rospolicy_write.go 724 (1997 native Go);
rospolicy_boundary_test.go 585, rospolicy_safety_test.go 506, testdata/r08_reference.py 108,
scripts/r08-mutation-gate.py 41. Shared definitions/helpers are extended in place; no copied
Python compatibility layer. No open implementation step in R08; pending independent Opus review
and the L5 lab items above. R12/R15 own their separately listed route/release integration.

## R09 — forced DNS (DNS guard), IPv4 same-port forward hint

**Status: `verified`** (Opus HR review 2026-11-18, [review-r09.md](review-r09.md): 0 high, 1 medium fixed in
`b3e93b5c`, 5 low; Netwatch comment selector ruled: keep it with the existing preflight/inspection block, none of
the three alternatives; gate PASS on `.64 -p 6`) · HR · prereqs: R04, R07 · Go: internal/routers · L5-C items added

Scope: `backend/routers/dns_guard.py` (589), `forward_hint.py` (184), latest upstream
`2a8cb64c` / PR #572 incremental publication. No new route or UI entry. Existing R01
`NormalizeDNSGuard` remains the settings validator. No dependency, production contact, push, or sub-agent.

Implemented: typed guard snapshot/spec/manifest/plan/report/check/replace/undo; scoped address reads and moved-ID
lookup; connected-route and HA VIP/real-host checks; safe object ordering; Netwatch scripts; exact ownership;
incremental keep/delete/create with paused hijacks preserved; preview drift checks; reverse journal, restore
anchors and per-table ID remap; hairpin assessment reuses R04 `stableROSRow`/`ownedHairpinIDs`. Driver registration
for `dns_guard_replace/restore`; `lifecycle.Detach` at both write owners, 10s device calls. `ReportParts` chains its
predecessor. Read-only forward hints use shared three-valued `ipv6_policy_eval.go` (R10 extends it).

Acceptance and per-test mapping, all HR issue-class prevention, intentional Python fixes, interface notes and
L5-only list: [progress/R09-dns-guard.md](progress/R09-dns-guard.md).

Shared edits (minimal fields; gofmt aligns adjacent declarations): `desired.go` adds `DNSGuard`, `model.go` adds
`DNSGuard` / `DNSGuardPlan`, `report.go` adds `RoutesReport.ForwardHint`. R08's DNS manifest set flag/setter
and `Manifests()` are reused without another driver edit. Guard reads `Desired.ROSPolicy.CIDRs` directly;
ForwardHint uses native `ROSPolicyPlan.Items` and prefers the policy filter snapshot over DNS guard's,
including the distinction between empty and missing filters. R12 uses `Manifests().DNSGuard`; the convenience
`DNSGuardPublication()` remains. R12/R15 attach ForwardHint to routes and warnings, never operations/checks/fingerprints.

Rebased onto R08+C32 and the fourth standing item (`go@c14763b3`); no R09 callers of ComputeDesired/RulesetContent
exist, and their C32 ctx signatures and caller contexts are retained. Integrated report/manifest/snapshot tests
and real Runtime.Start/BindRequest/Shutdown boundaries PASS; R09 explicitly checks ctx.Err after Detach and before
device/manifest commits. Final gate **PASS on `.32`**, default package parallelism, as the coordinator
instructed after `.64` remained queued; gofmt/vet/both tagged builds/lifecycle checks PASS.
Runtime Netwatch scripts still select by comment in Python
and Go: preview/apply block existing unproven matching NAT, but the full runtime ownership contract is a
potential high-severity item **deferred to fresh Opus by the coordinator**. See the dedicated Chinese report section.

Current merged-tree `.32` gate: gofmt/vet/both tagged builds/lifecycle checks PASS; proddiff exit0,
all self-checks PASS, coverage_regressions=0, **new=0 stale=0**. **Full race: 93 test packages PASS**
(routers1682.054s, routersplit59.157s, devhttp3.595s). **All 14 live difftest packages PASS**;
three 60s fuzz targets PASS (60.254/61.104/61.116s); four compiled mutations all DETECTED.
Final rt exit0, tail `GATE PASS` / `R09_ALL_GATES_PASS`, gate tree `c14763b3 + 40f419ab`.
Earlier standalone gates passed 84 full-race packages, scoped routers/routersplit/devhttp race, all 12 difftest
packages, three 60s fuzz targets and four compiled mutations. The old aggregation-provider new3/stale0 result
was independently reproduced on unmodified go and fixed separately; it is no longer needed as a gate exception.
Historical evidence is archived at `.32:/tmp/configflow-r09-final-before-r08-c32-20261118/proddiff.log`.
Final merged-tree gate logs: `.32:/tmp/configflow-r09-final`. No baseline changed.

Final `git rebase go`: `895f5e21`, implementation tip `e76e79ba`. Routers/routersplit/devhttp directories
are byte-identical to the full-gate tree (`git diff 40f419ab HEAD -- <those paths>` empty). New base changes only
C32 API review and gate TMPDIR. Post-rebase .32 gofmt/vet PASS; R09 race41.089s, api/rules race74.155s,
api/rules live differential94.837s PASS, rt exit0 / `R09_POST_REBASE_PASS`.
Log `.32:/tmp/configflow-r09-post-rebase.log`; exact commands in the linked progress file.

Chinese hand-back: [progress/R09-handoff-zh.md](progress/R09-handoff-zh.md). Detailed checklist and Python-test
mapping are in the linked R09 progress file. Fresh Opus review and L5 remain required; no push.

## R10 — IPv6 policy, IPv6 fake-IP, IPv6 rule-set routes

**Status: `verified`** (Opus review 2026-11-18; L5-D pending) · HR · prereqs: R04, R09 · Go: internal/routers

Files:
- `backend/routers/ipv6_policy.py` (529), `backend/routers/ipv6_fakeip.py` (348), `backend/routers/ipv6_routes.py` (370)

Tests: `test_ipv6_policy.py` (23), `test_router_ipv6_fakeip.py` (31), `test_router_ipv6_routes.py` (16), `test_router_ipv6_manifest_safety.py` (8)

Routes: none.

求值器已由 R09 落在 `ipv6_policy_eval.go`，R10 扩展它。

Notes for porter: IPv6 address/prefix math against Python `ipaddress` (corpus: ULA, link-local, mapped, zone ids, `strict=False`, prefix lifetimes — RouterOS 7.25.1 lifetime fingerprint fix 4191cf70), manifests `ipv6_fakeip_state`/`ipv6_routes_state`, `relevant`/`settings`/`inspect`/`needs_host`.

实现提交 `55a022ad4`、`99c4b1cc3`、`165cb81fd`、`96e8b4326`；
O3-6a 跟进 `f03376cca`、`151c7b3a0`，已 rebase 到 `go@f90a156eb`。
共享 evaluator 原位扩展为按表返回完整 evidence / NAT translation；R09 继续只取 Decision。
IPv6 Fake-IP / rule-set typed inspect/apply/restore/verify、manifest/BuildReport/driver handler 已接线。
不新增 HTTP route/loop/锁/依赖；R11 复用下一跳/环境转换，R12/R14 负责发布 orchestration。

真实 Python：68 路由/完整报告序列、330 全证据 policy cases、14 settings/原 YAML 字节组；
原始 wire/10s 参数、真实 Runtime shutdown/disconnect、归属/ID reuse/部分恢复回归；
仓库 86 子场景；5/5 编译成功的 boundary/recovery mutations 检出。全部通过 rt 在 .64 跑。
最终 `.64 gate.sh -p 6` 排队超过 15 分钟未获槽，按 brief 已终止；完整门禁由协调者 cf-gate 跑。
O3-6a 合并树 `.32`：全仓 gofmt/vet clean；相关包全部 race 通过（routers 471.428s、
routersplit 17.040s、devhttp 3.596s、repo 214.788s）；R10 / R09 evaluator 两个 60s fuzz PASS。
新增 borrowed active 回归 race 1.067s PASS。全量 race/proddiff/difftest 未宣称通过。
未 push，未访问真实设备/生产/PVE，待新的 Opus 复核和 L5。

详细逐类复核防护、API 契约、Python 测试映射、行为修正、文件行数、精确命令与结果：
[progress/R10-handoff-zh.md](progress/R10-handoff-zh.md)、[r10-test-map.md](r10-test-map.md)。
R06/R07/R08/R09/A24/N8-4ef 问题类别均在交接文件逐项说明，行为修正已登记 behaviour-changes.md。

### R10 独立 Opus 复核（2026-11-18）

[review-r10.md](review-r10.md)：0 high、1 medium（已修）、1 low（已修）、3 low 记录。
F1：补偿 DELETE 回复丢失但设备已删除时，Fake-IP 回滚跳过旧路由恢复，网段失去路由
（Python 会恢复）；改为回读证明 ID 已不存在即按成功处理。F2：dynamic/connect 的正确 Fake-IP
路由阻断文案与 Python 不同；候选判断改用与 Python `_correct` 同义的 `ipv6Routes`。
修复提交 `33df00c2f`，新增 Python 对照探针 `62aac8870`（24 组路由序列、15 组 policy_checks 全部一致）。
`./internal/routers/...` race 在 `.64` 通过（633.612s）。最终 `.64` gate.sh：gofmt/vet/build tags 通过，
全量 race 97 包 ok，proddiff new=0 stale=0，difftest 15 包 ok，7 个 fuzz PASS（排队约 19 分钟，详见 review-r10.md）。

## R11 — IPv6 PD guard, IPv6 DNS, RouterOS IPv6 readiness

**Status: `verified`**（Opus 复核 2026-11-18，L5 待做）· HR · prereqs: R10 · Go: internal/routers

Files:
- `backend/routers/ipv6_pd_guard.py` (635), `backend/routers/ipv6_dns.py` (232), `backend/routers/ipv6_readiness.py` (294)

Tests: `test_router_ipv6_pd_guard.py` (8), `test_router_ipv6_pd_guard_integration.py` (11), `test_router_ipv6_dns.py` (22), `test_router_ipv6_dns_integration.py` (12). `routers/ipv6_readiness.py` has no own test file: its calls are replayed from `test_ipv6_readiness.py` (A23-owned).

Routes: none.

Notes for porter: PD guard script/scheduler on the router (prefix change on reboot — see the maintainer's cf-pd script history), `ipv6_readiness` RouterOS reader (`_list`) used by agents A23 — export it.

移植提交：`1c8e874c2`、`214ed3bc4`、`d214fb515`（基底 R10 review@f368fa1b9；R10尚未合入go，合入后再变基）。
PD/DNS/readiness、typed plan/check/ops、RouterState/report、Manifests/Apply handler均齐；无新HTTP路由。
80个真实Python计划/报告/undo/写序列/最终表序列、3组模板原始字节、raw devhttp/10s透传、18个真实Runtime
边界、部分恢复/外改/原证明/未知字段/SQLite已验。最新.64定向race：routers27.887s、repo25.853s；vet clean。
.32共享包race：routers990.855s、routersplit56.088s、devhttp3.948s、repo448.124s、model48.181s；全仓gofmt/vet
通过。最新产品两个新fuzz各60s通过（219613/257393次）；5类编译变异5/5检出，编译失败不计数。

复核类别逐项：R06 F1=timeout透传，F2=共享脱敏且用户名保留，F3=raw空body/headers，F4=严格接口/ID/flags，
F5=typed DTO；R07 F1=不复制脱敏+共享分支回归，F2=netip身份，F3=snapshot/引用列表与PD scoped状态，
F4=报告/manifest副本，F5=逐对象完整证明/唯一性；R08 F1/F2=失败不当空、禁止猜ID，F3=presence/数组，
F4=无空LAN角色，F5=verify错误保留，F6=关机后补偿停止；R09 F1=部分成功也remap，F2–F4=PD运行时保留
namespace残余记L5，F5=按表nil/empty复用，F6=O14残余；R10 F1=DELETE失败新GET仅404作证明，F2=严格route
helper不改原文案，F3=PD逐步补偿并保留未完成证明，F4=共享默认分支不变，F5=关机残留不声称修复，L5-D
扩到PD端点/响应证明；A24 F1=共享凭据脱敏，F2=Detach后/每个commit前ctx.Err真实Runtime；N8-4ef=raw
proof/R5契约、未知字段与Set/null；O3=无新锁/goroutine/flock，已同6b约定统一上层L1→L2、底层不重入。
详见 [中文交接](progress/R11-handoff-zh.md)、[测试映射](r11-test-map.md)、behaviour-changes 的R11台账。
R12 stage调度/跨阶段回滚、R14自动同步、A23 HTTP/缓存不在本模块冒领验收。最终gate在.64单agent槽位
排队满900s未获槽，自动终止，rt退出124，未执行完整门禁；自身进程已清理。由协调者在batch树跑cf-gate，
不能宣称最新合并树全门禁通过。R10合入go后再变基；待新Opus复核与L5。


### R11 独立 Opus 复核（2026-11-18）

[review-r11.md](review-r11.md)：0 high、1 medium（已修）、1 low（已修）、4 low 记录。
F1：hook 所在 dhcp-client 的整行配置被当成归属证明，用户改 use-peer-dns、改池名、换客户端或清 hook 后，
PD 保护既不能更新也不能撤回（改池名后还会静默失效）；改为按 Python 只认 ID，hook 内容仍由状态把关。
8 个 Python 对照探针修复前全部不同，修复后一致（`9591b838b`）。F2：readiness fingerprint 不再包含设备密码
（`4be368394`）。记录：受管脚本/定时任务外改即阻断（设计）、补偿 PATCH 回复丢失不用 GET 确认、撤回回读时序、
关机残留（O14）。定向检查（gofmt/vet、routers+repo 的 R09–R11 race、R11 Python 对照、FuzzR11PDInput 60s）通过；
按新流程不跑完整 gate，由协调者批量 cf-gate。L5 清单见 review-r11.md。

## R12 — release plan and staged execution

**Status: `missing`** · HR · prereqs: R03–R11, A01 (ready) · Go: internal/routers

Files:
- `backend/routers/plan.py` (807), `backend/routers/release.py` (413)

Tests: `test_router_release.py` (24), `test_routeros_release_safety.py` (12), `test_router_return_routing.py` (20), `test_router_return_allocation.py` (11)

Routes: none.

**Design: `server/design/r12-release.md` (Opus, 2026-11-17) — port to it.** Re-scoped there (D-R12-1): R12 also owns the inspection service of `routes/routers.py` (`_agent`, `_dns_settings`, `_inspect`, `_inspect_state`, `_ipv6_host`, `_return_cleanup`, `_return_routing`), `ros_return_advisory.py` (+ `test_ros_return_advisory.py`) and the apply-route body as `Publish`/`StartPublish`; R12-1…R12-4 can run in parallel with R08–R11, R12-5a/b after them; section 2.4 is the contract R07–R11 porters must follow.

Notes for porter: `build_plan` (stages, ops, blockers, warnings, `STAGE_TITLES`, fingerprint), `execute`/`_execute`/`_verify`/`verify_nat`, `AgentReturnRouting` (Agent allocation contract, hex normalisation), rollback in reverse order. A01 manager provides `ReturnRoutingStatus/Apply/Remove`. Also used by C36 setup completion (`build_plan`) and A18 (`execute` in `test_oxidns_ha.py`).

## R13 — inspection service, return advisory, pending reminders

**Status: `missing`** · HR · prereqs: R12, C05, C07 (ready), A01, A06 (ready); A23 hook · Go: internal/routers

Files:
- `backend/routes/routers.py` (243) — lines 1–36, 63–99, 230–385, 755–768 (inspection service; see table at the top)
- `backend/routers/ros_return_advisory.py` (210)
- `backend/routers/pending.py` (260)

Tests: `test_ros_return_advisory.py` (13), `test_upstream_router_pending.py` (17), `test_router_ipv6_integration.py` (13; rule-sync half → R14)

Routes: none (the `upstream-routers` handler is R15).

**Re-scoped by `design/r12-release.md` (D-R12-1):** the inspection service and `ros_return_advisory.py` (+ its test file) moved to R12; R13 keeps `pending.py` and calls R12's `Inspect` + `BuildPlan` with an explicit profile id (fixes T27).

Notes for porter (pre-design, superseded where they conflict): `Driver(router)`, `Inspect(ctx, config, router, driver, contentLoader)` (returns gateway agent, desired, state, report), `Agent`, `ReturnRouting`, `ReturnCleanup` are exported for R14, R15, R16, C36 (setup completion) — Python imports these private helpers across modules. `_ipv6_host` → hook `IPv6HostRequest` (A23). `pending.summarize` registers `notifications.SetSources{Summarize}`; `agent_routers` reads routers concurrently (ThreadPoolExecutor + `FutureTimeout` → 504 in R15) with a keyed `PendingCache` — hand-port the concurrency/timeout tests. `safely(record_router, …)` after reads: must run with `context.WithoutCancel` (rule 4b).

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
| `routers` | `RulesetContent func(ctx context.Context, profile json.RawMessage, item RuleSet, library *LibraryRule) (string, error)` (offline: `allow_network=False`) | `routes.rules.get_ruleset_content` | C32 — or a direct import if C32 places it in a non-API package (preferred) |
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
