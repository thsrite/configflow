# R12 — Router release: inspection, plan, staged execution, publication record

Status: design, 2026-11-17 (Opus). Codex ports R12 to this design (WORKFLOW "HR modules: Codex ports, Opus
reviews"); a fresh Opus reviews it (HR). Changes to section 2 need the designer's or coordinator's agreement.
Template: WORKFLOW.md "Design docs, hand-offs and escalation". Builds on `design/o3-concurrency.md` (L1–L5,
`lifecycle.Spawn`, PORTING 4b, shutdown, crash recovery) and `design/n7-n8.md` (typed model rules), and does not
change them. Depth model: `design/a14-publish.md`.

Inputs read completely: Python `backend/routers/plan.py` (807), `release.py` (413), `ros_return_advisory.py` (210),
`routes/routers.py` (the inspection service 230–385 and the handlers `router_plan`, `router_apply`, `_log_release`,
`_capture_sync`, `_manual_sync_lock`, `_return_routing`), the consumers `pending.py::_read_router`,
`rule_sync.py::sync`, `rule_sync_write.py` (`restrict_plan`, `_apply_delta`), `ros_policy_write.py::apply/verify`,
`setup_completion.py` (managed router check), `test_oxidns_ha.py` (uses `execute`), `config_repository.py::
save_router_publication`, `notifications.py` (router release observation), `agents/manager.py` return-routing calls;
Go `internal/routers` (R01–R05 verified; R06 branch `go-port-r06`, R07 branch `go-port-r07`), `internal/repo`
(`SaveRouterPublication`), `internal/agents/remote.go` (return routing), `internal/lockx`, `internal/lifecycle`;
`parity/PROGRESS-routers.md` R03–R17, `parity/review-r04.md`, `parity/review-r05.md`, `parity/difftest-routers.md`
(T09, T13, T14, T22, T25, X03, X04, X08, X10, X12, K29, K31, L5, O6 review).

## 0. Survey

### 0.1 The chain in Python

```
POST /plan   (R012) ── find_router ── _driver ── _inspect ─────────────── build_plan ── safely(record_router) ── 200
GET  /report (R018) ── find_router ── _driver ── _inspect ── safely(record_router) ── 200
POST /apply  (R011) ── job_lock (try, before 404) ── find_router ── router_operation_lock (blocking)
                       ── reset_config_context, find_router again: changed → 409
                       ── _driver ── _inspect ── build_plan ── blockers → 409 ── fingerprint ≠ → 409
                       ── empty: _capture_sync, save_router_publication(last_release ok), refresh_tasks → 200
                       ── AgentReturnRouting ── execute(plan, driver, agent) ── _log_release
                       ── save_router_publication(last_release, manifests from driver attributes)
                       ── refresh_tasks ── ok: _capture_sync ── 200 / 502
_inspect = ros_address_lists.reuse(driver) { gateway Agent, compute_desired, DHCP DNS settings (single / HA VIP),
           driver manifests (policy, DNS guard), read_state, retire_policy_routes, DNS guard snapshot, ROS policy
           desired+snapshot+NAT context, IPv6 Fake-IP / IPv6 routes inspect (one Agent host request), DHCP DNS read,
           IPv6 DNS inspect, PD guard inspect, build_report (R08 policy plan, R09 guard plan as report parts),
           forward_hint, return routing status or cleanup }
execute  = ros_address_lists.reuse(driver) { for each stage: skip/verify-only/manual/pre-checks →
           batch { apply ops (runs, single, undo on error) } → verify → log;  on error: log failed,
           rollback(done) in reverse inside batch(rollback=True) }
```

Other callers: `pending._read_router` (R13, read-only reminders: `_inspect` + `build_plan`), `rule_sync.sync` (R14:
`_inspect` with an IP loader, `build_plan`, `restrict_plan`, `_apply_delta` which calls `release._verify`),
`ros_policy_write.apply` (R08: `release.verify_nat`), `setup_completion` (C36: `_inspect` + `build_plan`, requires
`empty` and no blockers), `test_oxidns_ha.py` (A18 tests: `execute`), `rule_sync_write.restrict_plan`
(`STAGE_TITLES`).

Data written: the router record (`last_release`, `ros_policy_state`, `dns_guard_state`, `ipv6_fakeip_state`,
`ipv6_routes_state`, `ipv6_pd_guard_state`) through `save_router_publication` (bumps `_revision`, so X08 applies);
the R14 receipt baseline through `_capture_sync`; notifications/tasks through `refresh_tasks` and `record_router`.
Nothing else: there is no release journal on disk — the undo log lives in memory for one `execute` call.

### 0.2 Go today

- R01: `Router`, `RouterState`, `Driver`, `RouterError{Kind, Message, Undo}` + `UndoOf`, `Desired`, `ComputeDesired`,
  DNS settings/report/`CheckedDNSTarget`/`VerifyDNS`, `StaticRouteIndex`, `PolicyRoute(OK)`, `RetirePolicyRoutes`,
  `StaleRoutes`/`StaleRouteWarning`, `ManagedComment`/`IsManaged`.
- R03: `BuildReport` + `ReportParts`, `Egress`, `AssessBypass`, `RouteEgressIndex`, `PlannedBypassConflicts`,
  `Networks`, `SupernetIndex`, `GroupPlanner` (+ `GroupOp`, `GroupStageCheck`, `VerifyGroups`, `GroupSpan`,
  `NATTarget`), `CanonAddress`.
- R04: `RouterOS` (`Apply` detaches, `BatchKinds`/`ApplyMany`/`BatchRestorable`/`RestoreMany`,
  `SetPolicyManifest`/`SetDNSGuardManifest`, `RegisterRouterOSHandler` for R07–R11 kinds).
- R05: `Ikuai` (`Apply` detaches, `Batch(ctx, rollback, fn)`, undo on uncertain errors, absent-add proofs per driver,
  `IkuaiOversizedRequests`).
- R06 (branch): `RouterOperationLock(ctx, router, write) (ctx, release, err)` — L2, write paths detach and wait ≤ 10
  min, then `KindConflict` `此路由器正在发布或同步，请稍后重试`.
- R07 (branch): `ReuseAddressReads() func()`, private `invalidateAddresses`, `PlanNATLists` (`NATListPlan` with
  `Ops []json.RawMessage`, `Check *NATListCheck`, `Evidence`, `Exempt`), `NATRemovalsFirst`, `RestrictNATEgress`,
  `VerifyNATLists`, `NATStage` (a stand-in for R12's stage type), `NATMemberUpdate`, `NATMigration`.
- `repo.SaveRouterPublication(ctx, profileID, expectedRouter, RouterPublication{Manifest, DNSGuard, IPv6FakeIP,
  IPv6Routes, IPv6PDGuard, LastRelease Optional})` (jsonx today; N8 replaces jsonx — use whatever typed API exists
  at port time, same semantics).
- `agents.Manager.ReturnRoutingStatus/Apply/Remove(ctx, agent, …)` (A01; errors are `*agents.RemoteError` with the
  Chinese message, or transport errors from `Call`).
- `notifications.RefreshTasks`, `notifications.Safely`, `agents/diag.EventMessage`, `lockx`, `lifecycle.Spawn`.
- Nothing of plan/release/inspection exists. `C32` (`get_ruleset_content`) is missing: `routers.RulesetContent`
  fails closed, so tests inject a `ContentLoader` (Python `_inspect` accepts one too).

## 1. Scope

### 1.1 What R12 covers (decision D-R12-1: re-scoped)

R12 owns everything between "a caller holds the router" and "the publication is recorded":

1. **Inspection** — `routes/routers.py` `_agent`, `_dns_settings`, `_inspect`, `_inspect_state`, `_ipv6_host`,
   `_return_cleanup`, `_return_routing` (≈150 lines; were R13).
2. **Plan** — all of `plan.py` (807), plus `ros_return_advisory.py` (210; was R13; only `build_plan` uses it).
3. **Execution** — all of `release.py` (413).
4. **Publication** — the body of `router_apply` after the job lock (≈80 lines of `routes/routers.py`, was R15),
   `_log_release`, `_capture_sync` call site, as the owner function `Publish`/`StartPublish`.

Why moved: R12 cannot be accepted at the boundary without the inspection (every plan input — desired, state with
the R08–R11 sub-plans, report, return-routing status — is produced by `_inspect`, and `build_plan` itself calls
`ros_return_advisory`), and the publish sequence (lock, re-read, fingerprint, execute, record, detach) is where the
O6 findings X04/X10/X12/T13/T25 live; keeping it in the HTTP package would leave it testable only by the black-box
difftest. R13 keeps `pending.py` (reminders; consumes `Inspect` + `BuildPlan`); R15 keeps HTTP mapping only.
Python lines: R12 1220 → **≈1660**; R13 713 → **260**. Test files moved: `test_ros_return_advisory.py` (13) → R12.
The coordinator updates the module table (this commit adds pointers in the R12/R13 sections of PROGRESS).

### 1.2 Not in scope

- R15 handlers (HTTP parsing, job-lock-before-404, response writing, `safely(record_router)` after plan/report),
  R13 pending, R14 rule sync (it *calls* `Inspect`, `BuildPlan`, `Verify`, `StageTitles`).
- The per-feature sub-plans and their device operations (R07 NAT lists, R08 policy, R09 DNS guard + forward hint,
  R10 IPv6 Fake-IP/routes, R11 IPv6 DNS/PD guard): R12 consumes their contract (2.4) and calls their verifiers.
- Driver internals (R04/R05). Repository internals (n7-n8).

### 1.3 Porting order and what R06–R11 must expose

R12-1 … R12-4 (section 5) need only R03–R07 and can run **in parallel with the R08–R11 ports** (they touch
`plan.go`/`release.go`/`inspect.go`/`publish.go`, which no other module writes). R12-5 integrates R08–R11 and runs
after R11 is on `go`. The coordinator passes **section 2.4** to every R07–R11 porter still working, so their types
already match; whatever they named differently, the R12-5 porter adapts in their files with a minimal edit.

| Module | Must expose for R12 (names are the contract; signatures in 2.4) |
|---|---|
| R06 | `RouterOperationLock` (exists on the branch). |
| R07 | Ops as typed values implementing `Op` (2.2) — `PlanNATLists` returns `[]Op`; `NATRemovalsFirst([]Op, []Op, gateway)`; exported `(*RouterOS).InvalidateAddressReads()`; `NATStage` replaced by R12's `Stage` (R14 then calls `NATMemberUpdate(plan.Stages, …)`). |
| R08 | `ROSPolicyPlan` on `RouterState`, `ROSPolicyDesired` on `Desired`, op type `*ROSPolicyReplace` with the mutable fields of 2.4, `ROSPolicyNetworkOverlap`, `SubtractNetworks`, `VerifyROSPolicy`, `InspectROSPolicyDesired`, `SnapshotROSPolicy`, `OwnedROSPolicyRows`; manifest tracking. |
| R09 | `DNSGuardRelevant`, `SnapshotDNSGuard`, `DNSGuardPlan` on `RouterState` (built in its `ReportPart`), `VerifyDNSGuard`, `ForwardHint` (read-only), manifest tracking. |
| R10 | `IPv6FakeIPRelevant/Settings/Inspect`, `IPv6RoutesRelevant/NeedsHost/Inspect`, plans on `RouterState`, `VerifyIPv6FakeIP`, `VerifyIPv6Routes`, manifests. |
| R11 | `IPv6DNSInspect`, `IPv6PDGuardRelevant/Inspect`, plans on `RouterState`, `VerifyIPv6DNS`, `VerifyIPv6PDGuard`, manifest. |
| R14 | registers `CaptureManual` (2.10); exports `NeedsBaseline` for R15's plan route. |
| A23 | registers `IPv6HostRequest` (2.10). |

## 2. Interface contract

### 2.1 Files (all in `internal/routers`, package `routers`)

| File | Contents |
|---|---|
| `plan_model.go` | `Op`, `OpHead`, R12-owned op types, `Stage`, `Checks` (+ sub-checks), `Plan`, `StageTitles`, stage keys. |
| `plan.go` | `BuildPlan` and its private helpers (port of `plan.py`), `Fingerprint`. |
| `plan_return.go` | return-routing contract: `ReturnStatus`, `ReturnSpec`, `ReturnAllocation`, `ReturnProtocol`, `ReturnSafetyBlockers`, `returnScope`, `returnOp`, cleanup ops. |
| `return_advisory.go` | port of `ros_return_advisory.py` (`ReturnAdvisoryWarnings`). |
| `inspect.go` | `Inspection`, `InspectRequest`, `Inspect`, gateway Agent view, DNS settings, return routing / cleanup, IPv6 host (hook). |
| `release.go` | `Result`, `LogEntry`, `ReturnAgent`, `Execute`, `Verify`, `VerifyNAT`, stage loop, rollback. |
| `publish.go` | `PublishRequest`, `PublishOutcome`, `Publish`, `StartPublish`, `LastRelease`, manifests capture, release logging. |
| `messages.go` | every new Chinese message as a constant (2.12). |
| `hooks_release.go` | `IPv6HostRequest`, `CaptureManual` collaborator variables (fail closed). |

### 2.2 Plan model

**Operations.** An operation is a typed value whose JSON is exactly Python's op dict (keys, presence, value
types). That JSON is (a) the driver contract (`Driver.Apply(json.Marshal(op))`), (b) part of the API response and
(c) part of the fingerprint input. Plans are never decoded from client input (apply rebuilds the plan), so there is
no polymorphic `UnmarshalJSON`.

```go
// Op is one release operation. The JSON of the concrete type is the driver contract.
type Op interface {
	OpKind() string
	OpSummary() string
}

// OpHead is embedded by every op type: {"kind": …, "summary": …}.
type OpHead struct {
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}
func (h OpHead) OpKind() string    { return h.Kind }
func (h OpHead) OpSummary() string { return h.Summary }
```

R12-owned op types (constructors set `Kind`; field order irrelevant, presence exact):

| Kind | Go type | JSON keys (besides kind, summary) | Conditional keys |
|---|---|---|---|
| `route_add` | `*RouteAddOp` | `cidr`, `gateway`, `comment` | – |
| `route_comment` | `*RouteCommentOp` | `id`, `cidr`, `comment` | – |
| `route_delete` | `*RouteDeleteOp` | `id`, `cidr` | – |
| `policy_route_add` | `*PolicyRouteOp` | `gateway`, `dst`, `comment` | `groups` (planner), `keep_foreign: true` (only when true) |
| `policy_route_update` | `*PolicyRouteOp` | `id`, `gateway`, `dst`, `comment` | as above |
| `policy_route_delete` | `*PolicyRouteDeleteOp` | `id` | – |
| `nat_update` | `*NATUpdateOp` | `id`, `add_dst` (list, may be `[]`), `comment` | `groups`, `keep_foreign: true`; `expected` (RouterOS per-rule plan: the rule's raw attributes); `out_interface` (set by the policy-replacement branch) |
| `nat_add` | `*NATAddOp` | `src`, `dst`, `comment` | `groups`; `out_interface` |
| `dhcp_dns_update` | `*DHCPDNSUpdateOp` | `id`, `dns`, `expected` (raw), `scope` `{id, network, gateway}` | `checked_dhcp_dns` — **only** on the copy `Execute` sends (RouterOS, first op of the stage), never in plan JSON or fingerprint |
| `return_routing_apply` | `*ReturnApplyOp` | `spec` (2.5), `previous` (Agent's spec verbatim or `null`) | – |
| `return_routing_remove` | `*ReturnRemoveOp` | `restore_legacy` (false), `previous` (Agent spec verbatim) | – |
| `ikuai_group_*` | R03 `GroupOp` (add `OpKind/OpSummary`) | R03's JSON | – |

`nat_update.expected`, `dhcp_dns_update.expected` and `previous` are `json.RawMessage` (device/Agent data kept
verbatim). Lists that Python always emits are never nil (`[]`). Ops of R07–R11 are their own types implementing
`Op` (2.4). Undo records stay `json.RawMessage` (the driver's contract, R04/R05).

**Stages.**

```go
type Stage struct {
	Key     string  `json:"key"`               // StageReturnRouting … StageReturnCleanup
	Title   string  `json:"title"`             // StageTitles[Key]
	Manual  bool    `json:"manual"`
	Ops     []Op    `json:"ops"`               // never nil
	Enabled *bool   `json:"enabled,omitempty"` // present only where Python sets it (return_routing, dhcp_dns, ipv6_routes, ipv6_dns)
	Skipped *bool   `json:"skipped,omitempty"` // ipv6_routes, ipv6_dns, ipv6_pd_guard
	Note    *string `json:"note,omitempty"`    // routes: only in policy mode
}

const (
	StageReturnRouting = "return_routing"; StageRoutes = "routes"; StageIPv6FakeIP = "ipv6_fakeip"
	StageIPv6Routes = "ipv6_routes"; StageNATBypass = "nat_bypass"; StageDHCPDNS = "dhcp_dns"
	StageIPv6DNS = "ipv6_dns"; StageDNSGuard = "dns_guard"; StageIPv6PDGuard = "ipv6_pd_guard"
	StageReturnCleanup = "return_cleanup"
)
var StageTitles = map[string]string{ /* exactly plan.STAGE_TITLES */ }
```

Order (positions computed exactly like `build_plan`: `ipv6_fakeip` inserted at index 2, `ipv6_routes` before
`nat_bypass`, then appended `ipv6_dns`, `dns_guard`, `ipv6_pd_guard`, `return_cleanup`), giving
return_routing → routes → ipv6_fakeip → ipv6_routes → nat_bypass → dhcp_dns → ipv6_dns → dns_guard → ipv6_pd_guard →
return_cleanup; only applicable stages appear (difftest T09).

**Checks** — part of the API response (`plan.checks`) and the verify input. Typed; presence rules are Python's:

| JSON key | Go field | Present when | Content |
|---|---|---|---|
| `gateway` | `Gateway string` | always | proxy gateway |
| `routes` | `Routes []CIDRGateway` | always (`[]` in policy mode) | static-route checks |
| `removed_routes` | `RemovedRoutes []string` | always | ids of `route_delete` ops, including those inside a policy op's `prepare_static` |
| `nat_bypass` | `NATBypass []string` | always | CIDRs whose NAT status ∉ {not_required, unknown} |
| `nat_not_required` | `NATNotRequired []CIDRGateway` | always | |
| `ipv6_fakeip` | `IPv6FakeIP` (R10 check type, pointer) | R10 plan present | R10's check |
| `ipv6_routes` | `IPv6Routes` (R10, pointer) | R10 routes plan present and `skip` empty | |
| `nat_list` | `NATList *NATListCheck` | R07 list plan present | |
| `policy_routes` | `PolicyRoutes []PolicyRouteCheck{comment, gateway, dst}` | policy mode | |
| `restored_routes` | `RestoredRoutes []CIDRGateway` | `retired_policy_routes` non-empty | |
| `removed_policy_routes` | `RemovedPolicyRoutes []string` | same | |
| `ros_policy` | R08 check type (pointer) | R08 plan present | |
| `return_routing` | `ReturnRouting *ReturnSpec` | `manage_return_routing` | copy of the spec |
| `dns_guard` | `DNSGuard *DNSGuardCheck{Roles}` | R09 plan present | `{"roles": …}` |
| `ikuai_groups` | `IkuaiGroups map[string]GroupStageCheck` | GroupPlanner used | `planner.StageChecks` |
| `dhcp_dns` | `DHCPDNS *DNSCheck` | DNS Agent selected and DNS status ∈ {synced, pending} | `{id, network, gateway, dns}` |
| `ipv6_dns` | R11 check (pointer) | R11 DNS plan present | |
| `ipv6_pd_guard` | `IPv6PDGuard` slot | R11 PD plan present | `{}` when the stage is skipped, else R11's check (which carries `enabled`) |

Execution reads "present" as "pointer non-nil" and Python's truthiness tests exactly where Python uses them
(`checks.get('return_routing')` truthy, `checks['dns_guard']['roles']` non-empty, `checks.get('nat_not_required')`
non-empty, `checks['ipv6_pd_guard'].get('enabled')`).

**Plan.**

```go
type Plan struct {
	Stages       []Stage  `json:"stages"`
	Checks       *Checks  `json:"checks,omitempty"` // absent in the early-blocker plan (Python returns no "checks")
	Blockers     []string `json:"blockers"`
	Warnings     []string `json:"warnings"`
	Script       string   `json:"script"`
	Fingerprint  string   `json:"fingerprint"`
	Empty        bool     `json:"empty"`
	SyncBaseline *bool    `json:"sync_baseline,omitempty"` // set by R15's plan route only
}
```

Early-blocker plan (no gateway / bad gateway / return routing unreadable or unsafe): `{"stages": [], "blockers":
[…], "warnings": […], "script": "", "fingerprint": "", "empty": true}`.

**Fingerprint** (decision D-R12-2). `Fingerprint(stages, natEvidence, dhcpEvidence, returnEvidence)` = first 16 hex
of SHA-256 over the **canonical JSON** of `{"stages", "nat_evidence", "dhcp_evidence", "return_evidence"}`:
marshal with `SetEscapeHTML(false)`, decode with `UseNumber` into a generic tree, re-marshal (all object keys sorted
at every depth, like `sort_keys=True`). Inputs are exactly Python's: the stages as the API shows them (no
`checked_dhcp_dns`), the nested evidence built in Python's order (base `{context, address_sets[, nat_list]}` →
`{nat, ros_policy, request}` → `{nat, ipv6_fakeip, ipv6_check}` → `{v4, ipv6_routes, ipv6_routes_check,
ipv6_routes_skip}` → `{routing, ipv6_dns, ipv6_dns_check}` → `{base, ipv6_pd_guard, ipv6_pd_guard_status}`, each
layer a small typed struct whose inner layer is a `json.RawMessage`), `dhcp_evidence` `{agent_id, target_dns,
gateway, state[, ha]}`, `return_evidence` = the Agent's raw `safety` object (only with `manage_return_routing`).
**Not** required to equal Python's value (Go never checks a Python fingerprint: the plan is rebuilt by the same
process; `last_release.fingerprint` is never compared later). Required: equal for equal device/Agent/config state
within one process run and across runs; different whenever any stage field or evidence leaf differs; unchanged by
runtime counters (the R03/R08 evidence already strips them). Checks are not hashed (Python).

### 2.3 Inspection

```go
// InspectRequest is one read-only inspection. Config is the profile's compat document (what get_config returns);
// ProfileID replaces Python's resolve_profile_id() (no request context in Go).
type InspectRequest struct {
	ProfileID string
	Config    json.RawMessage
	Router    *Router
	Driver    Driver        // one driver per preview / release (R05 absent-add proofs, R07 proof cache)
	Loader    ContentLoader // nil = RulesetContent (C32); R14 passes its IP loader
	Agents    AgentSource   // GetAgentByID; production: agents.Manager adapter
	Repo      *repo.Repository
}

type Inspection struct {
	Gateway *GatewayAgent  // nil when unlinked or not found
	Desired *Desired
	State   *RouterState
	Report  *Report        // Report.ReturnRouting set (omitempty keeps R03's corpus unchanged)
}

// Inspect is _inspect: reads the device and the gateway Agent, never writes. Context = the caller's (request ctx
// for previews; Publish passes its job ctx). Errors: *RouterError (device) → caller's 502; others propagate.
func Inspect(ctx context.Context, req InspectRequest) (*Inspection, error)
```

Port of `_inspect_state`, step for step (Python lines 241–357), with these Go specifics:
- RouterOS: wrap the whole inspection in `d.ReuseAddressReads()` (R07); iKuai has none.
- `GatewayAgent`: typed view decoded at the boundary from the Agent record (`id`, `name`, `host`, `port`, `status`,
  `profile_id`, `service_type`, `enabled`, `oxidns_ha{enabled, peer_id}`) plus the raw record handle the
  `agents.Manager` calls need. DNS settings: R01 `DNSAgentSettings` / `DNSHASettings` (`_dns_settings`).
- Manifest attributes: Python sets `driver.ros_policy_manifest = router.get('ros_policy_state')` (also to None) only
  when relevant; Go calls `SetPolicyManifest` / `SetDNSGuardManifest` / R10/R11 setters under the same conditions,
  and the driver records that the manifest was **set** (hasattr semantics, 2.9).
- `_ipv6_host`: profile check uses `Config.profile_id` or `req.ProfileID`; service/enabled checks; then the
  `IPv6HostRequest` hook (A23) → `MonitorError` text becomes `{error: text}`; one request per inspection (shared by
  Fake-IP and routes, Python's `hosts` cache).
- DHCP DNS read error: a `*RouterError` becomes `dns_error = 无法读取 DHCP DNS 配置：<msg>`; other errors propagate.
- `report.routes.forward_hint` from R09 `ForwardHint` (read-only; never in an op, check or fingerprint): R03's
  `RoutesReport` gains `ForwardHint *ForwardHintResult` (`omitempty`, so the R03 corpus is unchanged).
- Return routing: `manage_return_routing == true` → `returnStatus(ctx, agent)`; else `{status: disabled, message}`
  plus `cleanup` from `_return_cleanup`, which lists **all profiles** through `req.Repo` and excludes
  `(req.ProfileID, router.ID)`. Python used `resolve_profile_id()`, which in R13's worker threads fell back to the
  default profile (difftest T27); Go passes the real profile — Python bug fixed (O12, F-R12-2).

`ReturnStatus` is the Agent's body kept whole: typed `Status`, `Message`, `Cleanup *ReturnCleanup`, typed views of
`managed{active, table_present, spec}`, `safety{version, ok, blockers, allocation, protocols}`, `fwmark_rules`, plus
`Rest map[string]json.RawMessage` so every Agent key reaches the API unchanged (`MarshalJSON` merges; `success`
dropped as Python does). `status` values: `unlinked`, `offline`, `unavailable` (Agent error text, or the error class
name when empty), `ok`, `disabled`.

### 2.4 Contract with R07–R11 (state fields, desired fields, verifiers, manifests)

`RouterState` gains (owners fill them; R12 reads them):

```go
ROSPolicy        *ROSPolicySnapshot // R08 snapshot (inspection)
ROSPolicyPlan    *ROSPolicyPlan     // R08, set in its ReportPart (Python report.py:215)
DNSGuard         *DNSGuardSnapshot  // R09
DNSGuardPlan     *DNSGuardPlan      // R09, set in its ReportPart (report.py:255)
IPv6FakeIPPlan   *IPv6FakeIPPlan    // R10 (inspection)
IPv6RoutesPlan   *IPv6RoutesPlan    // R10
IPv6DNSPlan      *IPv6DNSPlan       // R11
IPv6PDGuardPlan  *IPv6PDGuardPlan   // R11
```

Minimum fields R12 reads (types are the owner's; JSON names = Python keys):

| Plan | Fields |
|---|---|
| `ROSPolicyPlan` | `Blockers, Warnings []string`; `Ops []Op` (each `*ROSPolicyReplace`); `Check *ROSPolicyCheck` (has `Migrations []…{Path, ID, Config}` and `Replacements []…{Path, Values}`); `Evidence`, `Request json.RawMessage` (fingerprint) |
| `*ROSPolicyReplace` (op) | mutable by `BuildPlan`: `OpHead.Summary` (suffix `；先准备 NAT 保护，再建立并回读替代静态路径`), `StaticChecks []CIDRGateway` (`static_checks`, always present on every policy op, may be `[]`), `PrepareStatic []Op` (`prepare_static`, only when static checks exist), `StaticNATChecks *NATChecks` (`static_nat_checks` `{gateway, nat_bypass, nat_not_required}`); R08's handler applies `PrepareStatic` through `ApplyMany` and calls `VerifyNAT(ctx, *StaticNATChecks, state)` |
| `DNSGuardPlan` | `Blockers, Warnings`; `Ops []Op`; `Spec json.RawMessage` (null = guard off); `Roles` (for `checks.dns_guard.roles`) |
| `IPv6FakeIPPlan` | `Blockers, Warnings, Ops`; `Check` (pointer), `Evidence` |
| `IPv6RoutesPlan` | `Skip []string`, `Warnings`, `Enabled bool`, `Ops`, `Check`, `Evidence` |
| `IPv6DNSPlan` | `Blockers, Warnings, Enabled, Ops, Check, Evidence` |
| `IPv6PDGuardPlan` | `Blockers, Warnings, Enabled, Ops, Check` (with `Enabled`), `Evidence`, `Status` |
| `Desired` | `DNSGuard *DesiredDNSGuard{Settings, Previous}` (R09), `ROSPolicy *ROSPolicyDesired{Previous{Spec{Destinations}}, CIDRs []{CIDR}}` (R08), `IPv6CIDRs` (R01, exists), `DNSHA` (R01 settings) |

Functions R12 calls (all take `ctx` first when they do I/O; device verifiers take `*RouterOS` — these stages exist
only on RouterOS; any other driver → `*RouterError` `messageStageNeedsRouterOS`):

```go
// R07
func PlanNATLists(ctx, report *Report, state *RouterState, gateway string, skipped []string) *NATListPlan // Ops []Op
func NATRemovalsFirst(natOps, routeOps []Op, gateway string) ([]Op, []Op)
func RestrictNATEgress(op *NATListSync, check *NATListCheck, iface string) string
func VerifyNATLists(ctx, check *NATListCheck, state *RouterState) error
func (d *RouterOS) InvalidateAddressReads()
func (d *RouterOS) ReuseAddressReads() func()
// R08
func InspectROSPolicyDesired(ctx, config json.RawMessage, router *Router, desired *Desired, loader ContentLoader) (*ROSPolicyDesired, error)
func SnapshotROSPolicy(ctx, d *RouterOS) (*ROSPolicySnapshot, error)
func OwnedROSPolicyRows(manifest json.RawMessage, snap *ROSPolicySnapshot) ([]ROSResource, error)
func ROSPolicyNetworkOverlap(replacements []netip.Prefix) func(netip.Prefix) bool
func SubtractNetworks(from, minus []netip.Prefix) []netip.Prefix
func VerifyROSPolicy(ctx, d *RouterOS, check *ROSPolicyCheck) error
// R09
func DNSGuardRelevant(r *Router) bool
func SnapshotDNSGuard(ctx, d *RouterOS) (*DNSGuardSnapshot, error)
func VerifyDNSGuard(ctx, d *RouterOS, check *DNSGuardCheck) error
func ForwardHint(ctx, r *Router, desired *Desired, state *RouterState, report *Report, d *RouterOS) (*ForwardHintResult, error)
// R10
func IPv6FakeIPRelevant(config json.RawMessage, r *Router) bool; func IPv6FakeIPEnabled(config json.RawMessage) bool
func InspectIPv6FakeIP(ctx, config json.RawMessage, r *Router, gw *GatewayAgent, host IPv6Host, d *RouterOS) (*IPv6FakeIPPlan, error)
func IPv6RoutesRelevant(config json.RawMessage, r *Router, desired *Desired) bool
func IPv6RoutesNeedsHost(config json.RawMessage, r *Router, desired *Desired) bool
func InspectIPv6Routes(ctx, config json.RawMessage, r *Router, desired *Desired, gw *GatewayAgent, host IPv6Host, d *RouterOS) (*IPv6RoutesPlan, error)
func VerifyIPv6FakeIP(ctx, d *RouterOS, check *IPv6FakeIPCheck) error
func VerifyIPv6Routes(ctx, d *RouterOS, check *IPv6RoutesCheck) error
// R11
func InspectIPv6DNS(ctx, desired *Desired, scope DNSScope, d *RouterOS) (*IPv6DNSPlan, error)
func IPv6PDGuardRelevant(r *Router) bool
func InspectIPv6PDGuard(ctx, r *Router, gatewayHost string, d *RouterOS) (*IPv6PDGuardPlan, error)
func VerifyIPv6DNS(ctx, d *RouterOS, check *IPv6DNSCheck) error
func VerifyIPv6PDGuard(ctx, d *RouterOS, check *IPv6PDGuardCheck) error
```

**Manifests** (Python's driver attributes `ros_policy_manifest`, `dns_guard_manifest`, `ipv6_fakeip_manifest`,
`ipv6_routes_manifest`, `ipv6_pd_guard_manifest`): `RouterOS` keeps, per manifest, `set bool` + `value
json.RawMessage`; the R04 setters and every R08–R11 handler that assigns the attribute in Python call the matching
setter (each setter marks `set`). R12 reads them through:

```go
type Manifest struct { Set bool; Value json.RawMessage } // Set=false: Python had no attribute → field untouched
type Manifests struct { Policy, DNSGuard, IPv6FakeIP, IPv6Routes, IPv6PDGuard Manifest }
type ManifestHolder interface { Manifests() Manifests } // RouterOS implements; iKuai does not (all unset)
```

### 2.5 Return routing (Agent) contract

```go
type ReturnAllocation struct { Version int; Reused bool; Table, Priority int; Mark string } // Mark normalised 0x%x
type ReturnSpec struct {                                  // JSON = the Agent PUT body (Agent protocol)
	RouterIP      string   `json:"router_ip"`
	Table         int      `json:"table"`
	Priority      int      `json:"priority"`
	Mark          string   `json:"mark"`
	SafetyVersion int      `json:"safety_version"`
	ProxyDst      []string `json:"proxy_dst,omitempty"`   // only protocol 1 (key absent for protocol 2)
}
func ReturnAllocationOf(status *ReturnStatus) (*ReturnAllocation, bool) // return_allocation, nil → false
func ReturnProtocol(status *ReturnStatus) int                            // 1 or 2
func ReturnSafetyBlockers(status *ReturnStatus, requireSafe bool) []string
const ManagedReturnTable = "configflow_return"; ReturnSafetyVersion = 1; ReturnAllocationVersion = 1
```

Validation is Python's **type-exact** reading of Agent JSON: `version`, `table`, `priority` must be JSON integers
(an integer literal; `1.0`, `true`, strings refuse), `reused` a JSON bool, table 1–252, priority 1–32765, mark
`^0[xX][0-9a-fA-F]{1,8}$`, non-zero power of two, normalised `0x%x`; safety `version == 1`, `ok` bool, `blockers`
list of non-blank strings. `previous` specs come from the Agent and are sent back verbatim (`json.RawMessage`); the
re-apply check reads `safety_version` type-exactly and refuses `adopt_legacy` truthy. Allocation comparison
(`_check_return_allocation`) compares table/priority by numeric value and mark by string, as Python's `!=`.
`_return_rule_matches`: `priority`, `table` = JSON integer or a non-empty string parsed decimal or `0x` hex;
`mark` text `"<mark>/<mask>"`, both equal to the expected mark's number; anything else → no match (Python's
`int()` accepts `_`/whitespace/`+`: not chased, iproute2 prints neither).

```go
// ReturnAgent is AgentReturnRouting: return-routing calls on the proxy gateway's Agent.
type ReturnAgent interface {
	Status(ctx context.Context) (*ReturnStatus, error)
	Apply(ctx context.Context, spec json.RawMessage) error
	Remove(ctx context.Context, restoreLegacy bool) error
}
func NewReturnAgent(manager *agents.Manager, gateway *GatewayAgent) ReturnAgent
```

### 2.6 Plan

```go
// BuildPlan is build_plan: pure (no device or Agent I/O) except ReturnAdvisoryWarnings, which reads only the
// state it is given. It never fails on device data: problems become blockers/warnings. An error means a
// programming or contract failure (e.g. an op that cannot be marshalled) → caller's 500.
func BuildPlan(ctx context.Context, router *Router, insp *Inspection, driver Driver) (*Plan, error)
```

Port of `build_plan` and its helpers line by line (`router_ip`, `_collapse`, `_lan_except`, `_route_ops`,
`_retired_policy_ops`, `_return_cleanup_ops`, `_policy_op`, `_grouped_policy_op`, `_policy_route_ops`,
`_grouped_nat_ops`, `_nat_ops`, `_return_op`, `fingerprint`). Points Codex must not guess:

1. Order of blockers and warnings is Python's append/insert order (several `warnings.insert(0, …)`; ipv6_routes
   warnings de-duplicated against the list so far). Messages byte-identical (corpus).
2. `skipped` is a set in Python; every place it is iterated for output (`_retired_policy_ops` `lost`, sorted) or
   membership keeps Python's result; `planner.deleted_rules` likewise.
3. `_nat_ops` RouterOS with `nat_context`: R07 `PlanNATLists` first (nil → per-rule plan). Per-rule plan: `rules`
   excludes R08 migrations, target selection (`managed[0]` or first adopt id), RouterOS fragment grouping by
   attributes minus `src-address` (compare attribute maps structurally, R04's `sameRow`), `expected` = rule
   attributes on every RouterOS `nat_update`; iKuai grouped branch via `NATTarget`.
4. iKuai: `IkuaiOversizedRequests(marshal(route_ops + nat_ops))` blockers (R05 requirement).
5. RouterOS with `nat_context`: `PlannedBypassConflicts(nat_ops …)` blockers.
6. Policy replacement block (Python 614–641): mutates R08 ops and the NAT ops in place, then `route_ops = [op]`
   (the **last** policy op with static checks wins, as Python's loop rebinds) and `nat_ops = []`.
7. `removals_first` only when the list check exists and NAT ops remain.
8. `manual = driver != nil && !driver.SupportsWrite()`; script via `RenderScript` of manual ops (dead for both
   drivers today, kept).
9. Advisory: RouterOS without managed return → forward hint `drop` → its warning only; else
   `ReturnAdvisoryWarnings(desired, state, driver)` + hint warning if any.
10. `empty = no stage has ops`.

`ReturnAdvisoryWarnings` is `ros_return_advisory.warnings_for` (port with R03 helpers `selectorMatches`,
`rosTruthy`, `RouteEgressIndex`; no private copies).

### 2.7 Execution, verification, undo log

```go
type LogEntry struct {
	Stage   string `json:"stage"`
	Summary string `json:"summary"`
	Status  string `json:"status"` // ok, verified, warning, manual, failed
}
type Result struct {
	OK          bool       `json:"ok"`
	FailedStage *string    `json:"failed_stage,omitempty"` // present only when !OK (may be "")
	Log         []LogEntry `json:"log"`                    // never nil
	Rollback    []LogEntry `json:"rollback"`               // never nil; statuses ok/failed
	Aborted     bool       `json:"-"`                      // stopped by shutdown (3.4), no rollback ran
}

// Execute is release.execute. ctx must be a job or background context: its cancellation means "the server is
// stopping" (3.4). All device and Agent I/O runs on context.WithoutCancel(ctx). agent may be nil.
func Execute(ctx context.Context, plan *Plan, driver Driver, agent ReturnAgent) *Result

// Verify is release._verify for one stage; state reuses a read with no write since (routes/nat_bypass return the
// state they checked, other stages nil). Every failure is a *RouterError (Python's ReleaseError included).
func Verify(ctx context.Context, stage string, checks *Checks, driver Driver, agent ReturnAgent, state *RouterState) (*RouterState, error)

// VerifyNAT is release.verify_nat (also called by R08's replacement handler).
func VerifyNAT(ctx context.Context, checks NATChecks, state *RouterState) error
```

**Undo log** (in memory, one per `Execute`; Python's `done`):

```go
type doneEntry struct { stage string; op Op; undo json.RawMessage }
```

Rules (from `release.py` and the R04/R05 reviews):
- An entry is appended **as soon as** an op is proven (`ApplyMany`'s `record` callback, or `Apply` returning), and
  for a failed op whose error carries `UndoOf(err)` (R05: uncertain iKuai writes) — appended *before* the stage
  fails, without an `ok` log line.
- `record(op, undo)` from `ApplyMany` names the op by its raw JSON: R12 matches it to the first not-yet-recorded op
  of the run with equal bytes. If `ApplyMany` returns an error carrying `UndoOf` (R04 never does today; a test pins
  it), R12 records it against the first unrecorded op of the run.
- Undo records with no effect are skipped at rollback: nil, empty, `null`, `{}` (trimmed). Python's falsy dict.
- Return-routing undo: apply with `previous` → `{"kind":"return_routing_apply","spec":previous,"previous":null}`;
  apply without → `{"kind":"return_routing_remove","restore_legacy":false}`; remove with `previous` → the apply
  record; remove without → `{}`.
- **F-R12-1 (Python bug fixed):** a return-routing apply/remove whose Agent call fails with a **transport** error
  (not `*agents.RemoteError`, i.e. the request may have been applied) records the same undo with no `ok` line, like
  R05's uncertain writes. Python recorded nothing, so a rule the Agent did apply stayed after the rollback. The undo
  is safe when nothing was applied (ownership-aware remove of nothing; re-apply of the previous spec).

**Stage loop** — port `_execute` exactly; the points that decide the device transcript:
1. Blockers → failure entry `发布计划存在阻止项：…` (stage `""`), no writes. `enabled == false` with ops →
   `未启用的发布阶段包含写入操作，请重新预览`.
2. Verify-only branches for empty stages, in Python's `if`/`elif` chain and with the log texts of 2.12; the
   `unchanged` state reuse (set after a routes/NAT verify, cleared before any write, reused by the next NAT verify)
   is ported exactly — it decides how many `ReadState` calls happen.
3. Manual stage → `{status: manual, summary: 需要在路由器上手动导入脚本}`.
4. Pre-checks before `dhcp_dns` (routes verify → optional ipv6_fakeip verify → NAT verify on the same state →
   `ReadDHCPDNS` → `CheckedDNSTarget`; RouterOS: first op is a `dhcp_dns_update` copy carrying `checked_dhcp_dns` =
   that read), before `ipv6_dns` (`checks.dhcp_dns` required, then dhcp_dns verify), before `dns_guard` with roles
   (routes → ipv6_fakeip → NAT).
5. Apply: `invalidate` address-list proofs before every op and every `ApplyMany` run; consecutive ops of a kind in
   `BatchDriver.BatchKinds()` go to `ApplyMany` as one run; the stage's apply runs inside
   `StageBatcher.Batch(ctx, false, …)` when the driver implements it (iKuai); verify runs after the batch.
6. After the routes stage verifies: `StaleRoutes` warning (not a failure).
7. Return ops: `return_routing_apply` checks `safety_version` (type-exact int in {1,2}) and `adopt_legacy`, then
   `_check_return_allocation(agent.Status)`, then `Apply`; `return_routing_remove` checks
   `ReturnSafetyBlockers(status, requireSafe=false)` then `Remove`. Agent nil → `未启用 Agent 回流配置，无法执行回流操作`.
8. **Any** error from apply, `ApplyMany`, a pre-check, a read, a verify, or an Agent call is a stage failure (review
   R04/R05 requirement: not only `RouterError`). Failure entry summary = `err.Error()`, or `messageUnknownError`
   when empty. A `KindRollback` error adds `{stage, err.Error(), failed}` as the first rollback entry (Python's
   `RouterRollbackError`).

**Rollback** — port `_rollback`:
- Inside `StageBatcher.Batch(ctx, true, …)` when available; entries = reversed `done`, effect-less skipped.
- Consecutive entries with `BatchDriver.BatchRestorable(undo)` and run length > 1 → `RestoreMany(ctx, undos,
  false)` (one result per record; `""` = restored); else one `apply(undo)` each (driver `Apply`, or the
  return-routing branch for return kinds). Results: `已撤销：<op summary>` ok / `撤销失败：<op summary>（<error>）`
  failed, stage = the entry's stage.
- An error from the batch itself (iKuai end-of-rollback proof) → `撤销失败：<error>` with the stage of `done[0]`
  (Go guards an empty `done`).
- Every rollback step runs to completion on `WithoutCancel` (R04 F3); rollback is never skipped because of the
  error type.

### 2.8 Publish (owner of the apply route body)

```go
type PublishRequest struct {
	ProfileID   string
	RouterID    string
	Expected    string // fingerprint from the body: str(body.get('fingerprint') or ''), parsed by R15
	Router      *Router // the record R15 found before taking the operation lock (404 already answered)
	Repo        *repo.Repository
	Agents      AgentSource
	NewDriver   func(*Router) Driver // DRIVERS[type](endpoint, username, password, verify_tls)
}

type PublishStatus int
const (
	PublishOK PublishStatus = iota // 200: executed and verified, or nothing to write
	PublishFailed                  // 502: executed, failed, rolled back (Result says how far)
	PublishRouterChanged           // 409 messageRouterChangedWhileWaiting, no plan
	PublishBusy                    // 409 messageEditBusy (operation-lock wait limit)
	PublishBlocked                 // 409 joined blockers + plan
	PublishStale                   // 409 messageStaleFingerprint + plan
	PublishDeviceError             // 502 {success:false, message}: *RouterError before execute
)

type PublishOutcome struct {
	Status  PublishStatus
	Message string
	Plan    *Plan   // nil for RouterChanged/Busy/DeviceError
	Result  *Result // set for OK (empty plan: {ok:true, log:[], rollback:[]}) and Failed
	Err     error   // non-RouterError failures after the device work (repo errors): R15 maps like httpx (X03)
}

// Publish runs one release while the caller holds the router's job lock. Never cancelled by the caller: ctx is the
// job context of StartPublish.
func Publish(ctx context.Context, req PublishRequest) PublishOutcome

// StartPublish runs Publish as the tracked job "router-publish-<router id>" (lifecycle.Spawn) and owns unlock (the
// job lock R15 took): it runs when the job ends, or immediately if Spawn refuses (lifecycle.ErrStopping returned).
// The channel delivers exactly one outcome; R15 waits for it or for the request context (client gone).
func StartPublish(ctx context.Context, req PublishRequest, unlock func()) (<-chan PublishOutcome, error)
```

Sequence (Python `router_apply`), every step on the job context (detached from the request):
1. `RouterOperationLock(ctx, router, write=true)` (R06; waits ≤ 10 min → `PublishBusy`).
2. Fresh read of the profile config (`ResetConfigContext` + `GetConfig`); find the router; if absent or not equal to
   `req.Router` (canonical JSON of the whole record incl. unknown fields) → `PublishRouterChanged`.
3. `driver := req.NewDriver(router)`; `defer driver.Close()`; one driver for inspect and execute.
4. `Inspect` → `BuildPlan`. `*RouterError` → `PublishDeviceError` (log `主路由 %s 发布中止：%s`).
5. Blockers → `PublishBlocked` (log warning `主路由 %s 发布被阻止：…`). Empty or unequal fingerprint →
   `PublishStale` (log warning).
6. Empty plan → `CaptureManual` (errors logged, swallowed) → `SaveRouterPublication(last_release ok)` →
   `ResetConfigContext` → `Safely(RefreshTasks)` → log → `PublishOK` with the empty result.
7. Agent client only when `manage_return_routing` or any stage has a `return_routing_remove` op.
8. Log `开始发布到主路由 %s：%d 项变更`; `Execute`; `logRelease` (Python `_log_release`, levels and texts exact).
9. Publication: `last_release = {at, ok, fingerprint, failed_stage ("" when ok), message}` where message =
   `diag.EventMessage(join('；', summaries of failed entries in log + rollback), [password])`; plus every manifest
   with `Set` (Value falsy → field removed, repo semantics). `SaveRouterPublication` (repo error → `Err`, the
   device result is still logged; Python raised `ProfileInUse` → 409 through the errorhandler).
10. `ResetConfigContext`, `Safely(RefreshTasks)`; if `Result.OK` → `CaptureManual(published config, router, driver,
    desired)` (errors logged, swallowed).
11. Outcome: OK → `PublishOK` message `""`; failed → `PublishFailed` with message `阶段「<failed_stage>」失败，
    回滚未完成，请检查发布记录` if any rollback entry failed, else `…，已按逆序撤销已完成的变更`. Aborted (3.4) →
    `PublishFailed` with `messageReleaseAborted`.

`at` = local time `2006-01-02T15:04:05` (Python `isoformat(timespec='seconds')`), from the lifecycle Clock.

HTTP mapping (R15 implements; listed here so the owner/handler split is unambiguous):

| Outcome | Status | Body |
|---|---|---|
| job lock busy (R15, before the 404) | 409 | `{success:false, message:"此路由器正在发布或同步，请稍后重试"}` |
| router not found (R15, after the lock) | 404 | `{success:false, message:"路由器不存在"}` |
| `ErrStopping` from `StartPublish` | 503 | `{success:false, message: messageServerStopping}` |
| `PublishBusy` / `PublishRouterChanged` | 409 | `{success:false, message}` |
| `PublishDeviceError` | 502 | `{success:false, message}` |
| `PublishBlocked` / `PublishStale` | 409 | `{success:false, message, plan}` |
| `PublishOK` (empty) | 200 | `{success:true, result:{ok:true,log:[],rollback:[]}, plan}` |
| `PublishOK` / `PublishFailed` | 200 / 502 | `{success, result, plan, message}` |
| `Err` (repo) | httpx repo-error mapping (409 `usages`, 400, lock timeout) | as Python's errorhandler |
| client gone before the outcome | – | handler returns; the job finishes and records (X10) |

### 2.9 Publication record and manifests

`last_release` type:

```go
type LastRelease struct {
	At          string `json:"at"`
	OK          bool   `json:"ok"`
	Fingerprint string `json:"fingerprint"`
	FailedStage string `json:"failed_stage"`
	Message     string `json:"message"`
}
```

Stored in the router record through `SaveRouterPublication` (the only writer allowed to replace/clear manifests,
under the caller's held job lock; repo's commit guard knows it is held via `lockedRouters`). Manifest mapping:
`Policy → ros_policy_state`, `DNSGuard → dns_guard_state`, `IPv6FakeIP → ipv6_fakeip_state`, `IPv6Routes →
ipv6_routes_state`, `IPv6PDGuard → ipv6_pd_guard_state`. Unset manifests are not passed (field untouched). The
manifests are recorded for failed releases too (rollback handlers set them to the restored ownership — Python).
Every `SaveRouterPublication` (empty plan, success, failure, shutdown abort) is a profile write: `_revision` + 1,
`config_revision` consumers go stale and, with auto-backup on, one WebDAV upload is scheduled after the commit
(repo's `ConfigurationCommitted`, outside L3) — difftest X08; R12 adds nothing of its own. Previews (R012/R018)
never write the profile.

### 2.10 Hooks (collaborator variables, `hooks_release.go`)

```go
// IPv6HostRequest is agents.ipv6_readiness._request (A23 registers). Unset → the inspection reports
// {error: messageIPv6HostUnavailable} for the IPv6 features (fail closed: no IPv6 ops planned).
var IPv6HostRequest func(ctx context.Context, agent *GatewayAgent) (IPv6Host, error)

// CaptureManual is rule_sync.capture_manual (R14 registers). Unset → ErrNotPorted, logged like any capture error.
var CaptureManual func(ctx context.Context, profileID string, config json.RawMessage, router *Router, driver Driver, desired *Desired) error
```

`RouterOperationLock` (R06) and the job lock (R14 `JobLock`, taken by R15) are direct calls.

### 2.11 Consumers

| Consumer | Uses |
|---|---|
| R13 `pending` | `Inspect` (request-independent ctx with its 25 s budget) + `BuildPlan`; never `Execute`. |
| R14 rule sync | `Inspect` (IP loader), `BuildPlan`, `Verify` (routes, nat_bypass, ipv6_fakeip, ipv6_dns, ipv6_routes), `StageTitles`, `Plan.Stages` for R07 `NATMemberUpdate`/`NATMigration`, `ROSPolicyReplace` fields. **R14 must roll back on any `Verify` error** — Python's `_apply_delta` caught only `RouterError`/`RuntimeError`, so a `ReleaseError` from `_verify` skipped compensation (Python bug; Go's `Verify` returns `*RouterError`, fixed by construction — F-R12-3, R14 test). |
| R15 | `StartPublish`, `PublishOutcome`, `Inspect` + `BuildPlan` for plan/report/policy preview, `Plan.SyncBaseline`. |
| R08 | `VerifyNAT` inside its replacement handler. |
| C36 setup completion | `Inspect` + `BuildPlan` (`empty && no blockers`). |
| A18 tests | `Execute(plan, driver, nil)`. |

### 2.12 Messages (constants in `messages.go`, byte-identical to Python)

Plan: every blocker/warning/summary/note literal of `plan.py` and `ros_return_advisory.py` (the corpus checks
each). Execution (`release.py`): `发布计划存在阻止项：`, `未启用的发布阶段包含写入操作，请重新预览`,
`IPv6 DNS 下发缺少已确认的 OxiDNS DHCP 目标，请重新预览`, `未启用 Agent 回流配置，无法执行回流操作`,
`回流操作缺少安全握手或包含手工规则接管，请重新预览`, `回流资源分配在预览后发生变化，请重新预览后发布`,
`mihomo Agent 不支持按来源标记的回流规则，请重新预览后发布`, the verify texts (`回读校验失败：…`, `Agent 上没有找到
configflow_return 表`, `Agent 上的回流网段与计划不一致`, `Agent 上的回流路由或安全握手与计划不一致`,
`没有找到与计划一致的回流策略路由（ip rule）`, `回读校验失败：Agent 上仍有受管的回流规则`), log texts (`回读校验通过`,
`需要在路由器上手动导入脚本`, the nine `回读确认…` and three `…本次保留现状` texts), rollback formats.
Publish: `路由器设置在等待发布期间发生变化，请重新预览`, `路由器、Agent 或期望状态在预览后发生了变化，请重新预览`,
`阶段「%s」失败，%s`, `回滚未完成，请检查发布记录`, `已按逆序撤销已完成的变更`, the five log formats of
`router_apply`/`_log_release`. New (Go only, O12): `messageReleaseAborted` = `服务正在停止，发布在当前操作完成后中止；
已完成的变更保留，请重新预览后发布`, `messageServerStopping` = `服务正在停止，请稍后重试`, `messageUnknownError` =
`发布失败（未知错误）`, `messageStageNeedsRouterOS`, `messageIPv6HostUnavailable` = `无法读取代理网关的 IPv6
状态（功能未就绪）`.

## 3. State and concurrency

### 3.1 Release states

There is no persistent "release in progress" state (Python had none). In memory:

| State | Entered | Left |
|---|---|---|
| queued for the operation lock | `StartPublish` → job running, waiting on L2 | lock acquired / 10 min → `PublishBusy` |
| inspecting / planning | lock held | blocked / stale / empty / executing |
| executing stage *k* | before the stage's first pre-check | verified → stage *k+1*; error → rolling back; shutdown → aborted |
| rolling back | stage error | rollback list complete (each undo attempted once) |
| recording | after execute | `SaveRouterPublication` returned |
| done | outcome sent; job lock and L2 released by defers | – |

### 3.2 Locks (O3 levels)

| Lock | Level | Taken by | Held | Busy → |
|---|---|---|---|---|
| router job lock (`repo.Hooks.JobLock`, R14, key = type + trimmed endpoint, or profile + router id) | L1 try | R15 handler, **before** the 404 / body checks (X04) | until the job ends (`StartPublish` owns `unlock`) | 409 `此路由器正在发布或同步，请稍后重试` |
| `RouterOperationLock` (R06) | L2, write: detached wait ≤ 10 min | `Publish` | inspect → plan → execute → record | `PublishBusy` 409 (O12: Python waited forever) |
| `.config.lock` | L3 | repo (`GetConfig`, `SaveRouterPublication`) | the transaction only; **no device or Agent I/O under L3** | repo timeout |
| L4/L5 | – | notifications, taskstatus | – | – |

Order L1 → L2 → L3, never reversed. `Publish` passes the context returned by each acquisition down (lockx order
checker). The repo commit guard *tries* router job locks inside L3 (allowed: try never waits); `SaveRouterPublication`
declares this router as locked by the caller.

Interactions (same outcome as Python unless noted):
- **Auto rule sync (R14)** shares the job lock: a round finding it busy skips the router; manual sync POST → 409.
  R14 then takes L2 itself. A publish never waits for a sync and vice versa (L1 is try-only).
- **Probe, settings PUT/DELETE, route editor apply, fixed-IP apply, rule-sync settings PUT** take the job lock →
  409 while a publish runs (X04). **Policy GET (R013), policy preview (R021)** wait on L2 with the request context
  (they block until the publish finishes, then answer 409 if the record changed).
- **Plan / report previews (R012/R018)** take no lock (Python): they may read intermediate device state during a
  publish; harmless (fingerprint mismatch at apply).
- **Profile delete / config import** (repo commit guard): 409 while the job lock is held; with manifests 409 always.
- **DNS guard** is a stage of this release, not a separate writer. **DNS HA (A18)** only changes what DHCP DNS
  should be (the VIP); it never publishes routers.
- **Agent publishes (A14/A16/A17)** to the same Mihomo Agent run concurrently with return-routing calls; the Agent's
  own service-operation lock answers 409 → `RemoteError` → stage failure → rollback (Python same).
- **Driver per goroutine:** one `Publish` owns its driver; drivers are not shared (R04/R05 rule: one goroutine per
  driver; R05 proofs per driver).

### 3.3 Cancellation (PORTING 4b)

- R15 never passes the request context into the release: `StartPublish` spawns the job with
  `context.WithoutCancel` semantics (lifecycle.Spawn). A disconnected browser (or the old Nginx 504 at 300 s, X10)
  does not stop it; the job finishes, records, refreshes tasks, captures the baseline and releases both locks.
  R15's handler returns when the request ends without writing.
- Inside `Execute` all I/O uses `WithoutCancel(ctx)`; `ctx` itself is consulted only at operation boundaries (3.4).
- `Inspect` for previews uses the request ctx (pure reads; a cancelled preview returns its error, nothing written).
- `Verify` called by R14 runs under R14's detached job context.

### 3.4 Shutdown mid-release (O3 3.4; maintainer kept the 10 s stop)

At SIGTERM the drain waits for the publish job like any job (most releases finish within the ~6 s drain). At the
drain deadline the job context is cancelled. Decision **D-R12-3**: `Execute` checks `ctx.Err()` **only at operation
boundaries** — before each stage's pre-checks, before each op or `ApplyMany` run, and `ApplyMany` itself stops
between proven batches (R04) — and then:
1. appends `{stage: current, summary: messageReleaseAborted, status: failed}`;
2. does **not** run the rollback (`Aborted = true`, `Rollback = []`);
3. returns; `Publish` records `last_release` (ok false, `failed_stage`, message) **and the manifests** on a detached
   context, then refreshes tasks; no `CaptureManual`.

Why not roll back: after the cancel the runtime waits only ≈ 0.5 s before exit; a rollback would usually be cut
halfway and leave neither the old nor the new state *and* no record. Recording first-and-only is one SQLite write
(tens of ms) and preserves what matters most: the ownership manifests of objects this release created (R08–R11),
without which the next preview treats them as foreign (T25). Every op that ran completed with its own read-back,
and the stage order guarantees each prefix is a safe intermediate state (routes before NAT before DNS before guard;
DHCP DNS and DNS guard re-verify routes/NAT immediately before they write). This supersedes, for this one cause
only, the R04 review note "ApplyMany ctx error → roll back"; any other error rolls back. Python was killed at once
and recorded nothing (O12 entry). If the process dies before the record, the result is Python's (3.5).

### 3.5 Crash recovery (`kill -9`, power loss)

No new recovery action (O3 3.5). After a crash mid-release: the device holds the completed ops; `last_release` and
manifests keep their previous values; the R14 receipt is untouched (capture only after success). The next preview
re-inspects: static routes, NAT rules and iKuai groups are recognised by their `cf` markers and planned normally
(adopt / keep / delete); R08–R11 objects created after the last recorded manifest appear as foreign and block or
are reported by those modules, exactly as in Python (difftest T25 compares this).

## 4. Failure and rollback

| Failure point | What runs | Rolled back | Recorded | HTTP | User sees |
|---|---|---|---|---|---|
| job lock busy | nothing | – | – | 409 | `此路由器正在发布或同步，请稍后重试` |
| op lock wait > 10 min | nothing | – | – | 409 | same text (O12) |
| router changed while waiting | lock released | – | – | 409 | `路由器设置在等待发布期间发生变化，请重新预览` |
| inspection/plan device error (`*RouterError`) | reads only | – | – | 502 | the driver's Chinese message |
| blockers | reads only | – | – | 409 | `；`-joined blockers + plan |
| fingerprint empty/stale | reads only | – | – | 409 | `路由器、Agent 或期望状态在预览后发生了变化，请重新预览` + plan |
| empty plan | capture + record | – | `last_release.ok=true` | 200 | (nothing to write) |
| return_routing stage: capability/allocation changed, Agent refusal | Agent status read (+ apply) | earlier entries (none: first stage) | failed release | 502 | the check's message, `阶段「return_routing」失败，已按逆序撤销已完成的变更` |
| op refused (`KindRejected`, RouterOS 4xx) | nothing written for that op | all earlier ops of all stages, reverse order | failed + manifests | 502 | device message |
| op result unknown (lost reply, 5xx, iKuai uncertain) | RouterOS compensates inside `Apply` (R04) and reports; iKuai returns the undo on the error → recorded → run in rollback | as above, incl. the uncertain op's undo | failed | 502 | e.g. `RouterOS /ip/route 已提交但没有返回可信的新对象 ID；…已保留现场，请检查路由器` |
| Agent transport error on return apply/remove | – | earlier ops + this op's undo (F-R12-1) | failed | 502 | the Agent error text |
| batch partially proven (`ApplyMany`) | proven prefix recorded | prefix + earlier | failed | 502 | batch message |
| stage verify fails (incl. pre-checks before DHCP DNS / guard) | – | every recorded op | failed | 502 | `回读校验失败：…` |
| non-RouterError anywhere in execute (malformed device JSON etc.) | – | every recorded op | failed | 502 | `err.Error()` (Python: HTML 500, no rollback, no record — X12, O12) |
| rollback step fails | other steps still attempted | partial | failed; message includes `撤销失败：…（…）` | 502 | `阶段「X」失败，回滚未完成，请检查发布记录` |
| `RouterRollbackError` from a handler (its own compensation failed) | – | the rest | first rollback entry = its text | 502 | as above |
| iKuai end-of-rollback proof fails | – | – | `撤销失败：<error>` | 502 | as above |
| `SaveRouterPublication` fails | device work done and logged | – | **not recorded** (Python same) | repo mapping (409 `路由器连接已变化，无法保存发布回读记录` / lock timeout) | that message; the release log is in the server log |
| `CaptureManual` fails | release succeeded | – | release recorded; receipt not | 200 | (logged `Could not record remote rule synchronization baseline`; R14 shows waiting) |
| shutdown at drain deadline | stops at op boundary | **nothing** (3.4) | failed + manifests, if the write completes | client gone | after restart: `上次发布失败：服务正在停止…` |
| `kill -9` | – | – | nothing new | – | 3.5 |

**Guarantees on the device** (what the reviewer checks):
1. No write without a fresh read in the same driver: every op's preconditions are re-proven by the driver (R04/R05)
   and the plan is rebuilt under the operation lock from a fresh inspection; the fingerprint ties it to what the
   user previewed.
2. Every write that may have happened is either proven and recorded, or compensated inside the driver, or carries
   its undo on the error — and is recorded before the stage fails.
3. Rollback runs in exact reverse order across stages, attempts every effective undo once, never re-runs a batch
   whose restore partly failed (Python comment), and never deletes an object whose ownership it cannot prove
   (drivers refuse; the refusal is a reported rollback failure, not a silent skip).
4. DHCP DNS is advertised and DNS hijack enabled only after routes and NAT protection are re-verified in the same
   read; IPv6 RA DNS is withdrawn only after DHCP DNS is verified.
5. The publication record reflects the device after the release (or after its rollback), including ownership
   manifests, whenever the process lives long enough to write it.

## 5. Buildable steps

Every step: own branch off `go`; compiles; `gofmt -l` empty; `go vet ./...`; `go test -count=1 ./...` and `-race`
on `internal/routers`; `server/scripts/proddiff.sh` on .32 with no new difference; PROGRESS R12 updated (commits,
decisions, Python-test coverage map) and, for HR steps, **one line per issue class** of `review-r04.md`,
`review-r05.md` (and `review-a19.md`) saying how the step avoids it. Sizes: S ≤ 1 day, M 1–2 days, L 3–4 days.

| Step | Content | Size | HR | After |
|---|---|---|---|---|
| R12-1 | `plan_model.go`, `plan_return.go`, `plan.go` without the R07 list / R08–R11 branches (they return "not present"), `return_advisory.go`, `Fingerprint`; R03 `GroupOp` implements `Op`. Corpus `testdata/gen_r12_plan.py`: real Python `build_plan` over synthetic `(router, desired, state, report, return_routing)` built by the generator (RouterState from JSON via the dataclasses; report from real `build_report`) — static/policy/retired/iKuai-group/NAT per-rule/DHCP/return/cleanup topologies, malformed Agent safety/allocation values (type-exact cases), conflicts; compare plan JSON structurally **excluding the fingerprint value**, plus fingerprint *relations* (equal for equal inputs, different for each single-leaf mutation of stages/evidence). Advisory: `test_ros_return_advisory.py` scenarios as corpus rows. | L | yes | R03–R05 |
| R12-2 | `inspect.go` (incl. `GatewayAgent`, `ReturnStatus`, `_return_cleanup` across profiles, hooks), R07 integration in `_nat_ops` (`PlanNATLists`, `NATRemovalsFirst`, `RestrictNATEgress` via typed ops). Python reference harness `testdata/r12_reference.py` mode `inspect+plan`: seeds a temp `DATA_DIR` (profile, router, Agents), runs Python `_inspect(config, router, driver, content_loader)` + `build_plan` against `routerfake` (RouterOS / iKuai) and `agentfake` (return routing), outputs report + plan JSON + device transcript; Go `Inspect` + `BuildPlan` against fresh fakes with the same seed. | M | yes | R12-1, R07 |
| R12-3 | `release.go`: `Execute`, `Verify` (routes, nat_bypass + `VerifyNAT` + nat_list, dhcp_dns, return_routing, return_cleanup), undo log, rollback, `ReturnAgent`, F-R12-1, shutdown abort (3.4). Harness mode `execute`: Python `execute(plan, driver, agent)` vs Go `Execute` on the same seeded fakes with fault scripts (6.1, 6.2): compare `Result` exactly, transcript, final tables. Go-only tests: cancellation at each boundary kind (before stage / before op / inside `ApplyMany` between batches) → aborted, no rollback, all completed ops proven; every error class rolls back; effect-less undos skipped; `ApplyMany` error with `UndoOf` attributed; `lockx.Violations()==0`. | L | yes | R12-2 |
| R12-4 | `publish.go`: `Publish`, `StartPublish`, `LastRelease`, manifests capture (R04 setters get the `set` flag), `logRelease`, `CaptureManual` hook. Harness mode `apply`: Python `POST /api/routers/<id>/apply` through the Flask test client on the seeded data dir vs Go `Publish` outcome (status per 2.8 table, body JSON, stored router record incl. `last_release` minus `at`, manifests, `_revision` bump, task-status/notification files), for blockers, stale/empty fingerprint, empty plan, success, failure+rollback, router changed while waiting. Go-only: job lock released by the job (not the handler) after a simulated client disconnect; second publish 409 while the first runs; op-lock limit; `SaveRouterPublication` failure → `Err`; shutdown abort records manifests; `-race`. | M | yes | R12-3, R06 |
| R12-5a | R08 + R07 policy integration: ROS policy ops/evidence/static checks/`prepare_static`/`static_nat_checks`, `network_overlap` blocker, checks `ros_policy`, `Verify` routes→`VerifyROSPolicy`, empty-routes-stage verify branch, policy manifest; R14's `NATStage` → `Stage`. Harness rows from `test_ros_policy*.py`, `test_ros_mode_transitions.py` flows. | M | yes | R12-4, R08 |
| R12-5b | R09–R11 integration: DNS guard stage + pre-checks + verify + forward hint, IPv6 Fake-IP / routes / DNS / PD guard stages (insertion positions, skipped flags, `{}` PD check, warnings de-dup), evidence nesting, manifests for all five, verify-only branches for empty IPv6 stages. Harness rows from the R09–R11 integration test files. | M | yes | R12-5a, R09–R11 |
| R12-6 | Consumer readiness and faults: `Verify` export contract test for R14 (F-R12-3 case), A18 `Execute` use, C36 `empty` check; O3-8 fault scenarios (6.4); PROGRESS coverage map final; L5 plan entries. | S | yes | R12-5b, O3-8 |

Then: fresh-Opus HR review of R12 (one review over R12-1…6); difftest Step B rows for R011/R012/R018 run when R15
lands; lab batch L5.

## 6. Acceptance

### 6.1 Fakes

- `routerfake` RouterOS and iKuai (R04/R05; R07 adds address lists/scripts) with the O6 fake-contract facts and
  fault injection per request: HTTP status/body, lost reply after the write is applied, refusal, delay beyond the
  driver timeout, connection reset, hidden/dynamic rows, external writes between requests (a callback after request
  *n*), ID reuse, iKuai 1008 mid-batch, empty 200 for oversized bodies, RouterOS `invalid=true` settle.
- `agentfake` (A14's fake, return-routing endpoints added): `GET/PUT/DELETE /api/return-routing` with a state
  machine (managed spec, table/rule presence, safety status, allocation, protocols), faults: refusal 409 with
  message, 404 (old Agent), transport error **after** applying (F-R12-1), allocation change after N calls,
  `fwmark_rules` in decimal / padded hex / wrong mask.
- One fake set per side (difftest X13); seeds identical.

### 6.2 Python reference (boundary differential, not function replay)

`testdata/r12_reference.py`, run on the test server with `~/cf-test/.venv/bin/python`, driven by
`r12_boundary_test.go` like R04/R05: each case = seed (profile document, router record, Agents, fake tables,
fault script) + mode (`inspect+plan`, `execute` with a plan produced by the same side, `apply` via Flask test
client). Compared: plan JSON (structural; fingerprint by relations), `Result` (exact, Chinese byte for byte),
the device transcript (method, path, query, body as JSON value, console script text), the Agent transcript, final
fake tables, stored router record (minus `at`), manifests. Intended differences carry an `intended` tag with both
sides asserted (Python bug fixes F-R12-1/2, X12 crash class, shutdown abort is Go-only).

Case groups (minimum; each with RouterOS and iKuai where applicable):
1. Plans: home setup adopt + fill gaps; conflicts skipped + warnings; no Agent / bad gateway / non-IPv4 endpoint
   with return routing; return routing unsafe/old/incomplete allocation (each type-exact variant); policy mode
   migrate/repair/raise priority; retired policy back to static with conflicts; iKuai groups (add/extend/repack/
   cleanup/migration warning); NAT per-rule adopt/extend/fragments; RouterOS NAT list; DHCP DNS pending/synced/
   blocked; stale routes; forward hint drop; advisory warnings; oversized iKuai requests.
2. Execute happy paths: every stage combination of T09 including verify-only empty stages and `unchanged` reuse
   (transcript shows the number of reads).
3. Faults per stage at first/middle/last op (T09): refusal, lost reply, external write, ID reuse, verify mismatch;
   rollback across stages; rollback failure (external change during rollback, `review_*` messages); iKuai rollback
   proof failure; RouterRollbackError.
4. Return routing: apply/readback/allocation change/protocol change/cleanup last/cleanup skipped shared/unsafe;
   F-R12-1.
5. Apply flow: blockers, stale/empty fingerprint, wrong Content-Type (empty fingerprint after a full inspection —
   difftest R011 B92), empty plan records, router changed while waiting, save failure.

Mutation checks (reviewer runs; each must fail ≥ 1 case): drop one rollback entry kind; skip the DHCP pre-check;
reverse rollback order; treat `{}` undo as effective; reuse `unchanged` after a write; omit `checked_dhcp_dns`;
roll back only on `RouterError`.

### 6.3 Go-only tests

Cancellation/shutdown (3.3/3.4), lock interplay (job lock held by the job, L2 wait limit with a fake clock, order
checker), `-race` with concurrent previews during a publish, malformed device/Agent JSON never panics (R04 F1/F7
class: every field of `ReturnStatus`, `fwmark_rules`, R08–R11 plans fed odd shapes), credentials never in
`last_release.message` (password via `EventMessage`), fingerprint determinism across process restarts (same input →
same value from two test binaries).

### 6.4 Fault injection (O3-8 harness, .32, black-box binary)

SIGTERM during a long publish (slow fake): within the drain → completes and records; at the drain deadline →
aborted entry recorded with manifests, no rollback requests in the fake transcript after the cancel. `kill -9`
mid-stage → restart → next preview's plan compared with Python's after the same kill (T25). Client disconnect at
each stage → final state equals an uninterrupted run (X10).

### 6.5 Black-box differential (Step B, not the porter)

Rows R011, R012, R018 (and R016's router plan) of `difftest-routers.md`, T09, T13, T14, T22, T25, X03, X04, X08,
X10, X12 — run when R15 is ported. Fingerprints: each side self-consistent (O6 rule), never compared across sides.

### 6.6 Python tests → coverage

| File | Coverage |
|---|---|
| `test_router_release.py` (24) | plan cases → R12-1 corpus / R12-2 harness; execute order/rollback/agent-first/kernel leftovers/policy verification/return cleanup → R12-3 harness; `test_router_plan_and_apply_api` → R12-4 harness `apply`; `test_encode_matches_ikuai_ui`, `test_ikuai_error_details_surface_in_message` → R05 (done) |
| `test_routeros_release_safety.py` (12) | NAT/route safety plans → R12-1/2; rollback with external writer, lost create, later-stage failure, delete rollback without duplicates → R12-3 harness (driver halves already R04) |
| `test_router_return_routing.py` (20) | plan/blocker cases → R12-1; apply/readback/cleanup → R12-3; API cases (setting persists, offline Agent not contacted, switching off) → R12-4 harness `apply` + R15 differential |
| `test_router_return_allocation.py` (11) | allocation validation boundaries → R12-1 corpus (type-exact rows); change after preview, rollback of owned tuple → R12-3 |
| `test_ros_return_advisory.py` (13) | R12-1 corpus |
| deferred rows: `test_router_dhcp_dns.py` plan/release halves, `test_router_cidr_index.py`/`test_router_source_activation.py` policy halves, `test_routeros_nat.py` plan/release/fingerprint cases, `test_routeros_static_selection.py` release verification, `test_router_route_index.py` `_verify`, `test_ikuai_groups_invariants.py` previous-release flows, `test_ikuai_scale.py` plan/execute parts, R08–R11 integration files' release halves, `test_oxidns_ha.py` guard execute | R12-1…5 harness rows, one row per named test, listed in PROGRESS |

### 6.7 L5 lab items (PVE lab, after review; `server/parity/lab/L5.md`)

1. Full publish RouterOS CHR 7.24 / 7.25.1 and iKuai 4.0.x from a lab panel: all stages, read-back, then a second
   preview is empty (`last_release.ok`, manifests stored).
2. A/B with Python on one snapshot: same profile → device dumps equal (REST tables / `/export`, iKuai lists).
3. Failure at each stage by revoking a permission mid-release (RouterOS group without write on one menu; iKuai
   account) → rollback leaves the pre-release device; record shows the failed stage.
4. Pull the management link during a stage (lost reply) → compensation/rollback; reconnect → preview consistent.
5. Return routing on a lab Mihomo Agent: apply → traffic marked and returned; switch off → cleanup last;
   Agent restart mid-release (transport error, F-R12-1 undo).
6. SIGTERM the lab panel during a long release (300+ routes): aborted record with manifests; next preview withdraws
   or completes cleanly.
7. Concurrent auto rule sync round and a manual publish on one router: one 409, no interleaved writes.

## 7. Python behaviour intentionally not kept (→ O12 ledger)

| # | Python | Go | Why |
|---|---|---|---|
| X12 | non-RouterError inside `execute` → HTML 500, no rollback, no `last_release`, lock released | stage failure, rollback, record, 502 | fail closed (R04 F1/F7 class) |
| F-R12-1 | Agent transport error on return apply/remove → no undo recorded | undo recorded, run on rollback | uncertain-write class (R05) |
| F-R12-2 | `_return_cleanup` in worker threads used the default profile (T27) | explicit profile | correctness |
| F-R12-3 | R14 skipped compensation for `ReleaseError` from `_verify` | `Verify` returns `*RouterError`; R14 compensates | correctness (R14 test) |
| D-R12-3 | SIGTERM killed the release at once, nothing recorded | drain waits (~6 s); at the deadline stop at an op boundary, record failure + manifests, no rollback | device consistency + ownership records |
| O3 | operation lock waited forever | write paths wait ≤ 10 min → 409 | O3 table 2.4 |
| D-R12-2 | fingerprint = Python `json.dumps(sort_keys)` bytes | canonical Go JSON; value differs from Python's | one-time: a preview opened before the switch needs a re-preview |
| X03 | `Content-Type` / malformed body handling, HTML 500 for odd bodies | R15 decides per the difftest | R15 |

## 8. Decisions

Taken by the designer (tell the maintainer, no action needed):
- **D-R12-1** R12 absorbs the inspection service and the return advisory (moved from R13) and the apply-route body
  (from R15) as owner functions; R13 keeps pending reminders, R15 keeps HTTP mapping (1.1). The coordinator updates
  the module table.
- **D-R12-2** Go-native fingerprint encoding (2.2); not equal to Python's value, same sensitivity.
- **D-R12-3** Shutdown at the drain deadline: stop at an operation boundary, record, no rollback (3.4).
- F-R12-1/2/3 Python bugs fixed (section 7).
- R12-1…R12-4 may run in parallel with the R08–R11 ports; R12-5a/b after them (1.3).

For the maintainer: **none.** Every user-visible difference above is a failure-path or shutdown-path improvement
recorded for O12; the success path, stage order, messages, plan/API shapes and device writes are Python's.
