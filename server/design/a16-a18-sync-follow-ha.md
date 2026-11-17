# A16–A18 — Agent rule sync, DNS follow, DNS HA (keepalived VIP pair)

Status: design, 2026-11-17 (Opus). Codex ports A16, A17 and A18 to this design (WORKFLOW "HR modules: Codex ports,
Opus reviews"); a fresh Opus reviews each module (HR), then lab batch L3. Changes to section 2 need the designer's or
coordinator's agreement. Template: WORKFLOW.md "Design docs, hand-offs and escalation". Builds on
`design/o3-concurrency.md` (L1–L5, `lifecycle.Spawn`/`Periodic`, PORTING 4b, shutdown, crash recovery),
`design/n7-n8.md` (model rules R1–R9, yamlx on yaml.v3, D1–D3) and **`design/a14-publish.md`** (A16–A18 publish through
`deploy.Publish`; A18 registers `deploy.SetPairPublisher`) without changing them, except the additive A14 exports
listed in 1.3.

Inputs read completely: Python `agents/rule_sync.py`, `rule_sync_projection.py`, `rule_sync_snapshot.py`,
`rule_sync_store.py`, `dns_follow.py`, `oxidns_ha.py`, `oxidns_ha_handoff.py`, `oxidns_ha_publish.py`,
`idle_rounds.py`, the HA/follow parts of `manager.py` (`begin_deployment`, `_deployment_request`, `_get_deployment`,
`get/activate/publish_deployment`, `service_operation` callers, `_oxidns_ha_request` and the five HA calls),
`routes/agents.py` 162–215, 1233–1269, 1727–1748, `routes/tasks.py` (`_agent_rows`, `run_task`), `routers/dhcp_dns.py`
(`dns_ha_settings`), `routers/rule_sync.py` (HA digest), `common/rule_dns.py` (`settings_for`, `_bindings`),
`routes/rules.py` (`pinned_withheld`), `app.py` (`start_agent_rule_sync`); the test files of 6.6; the Agent routes in
`backend/agents/go-agent/routes/{oxidns_ha*.go,rule_sync_snapshot.go,dns_follow.go,register.go}` (protocol facts); on
`go`: `internal/agents/{compat.go,hooks.go,idle_rounds.go,singleton.go,presence_log.go,client.go,remote.go,
deployments.go}`, `internal/agents/monitor/ruleupdate.go` (`SetHARules`), `internal/lifecycle`, `internal/lockx`,
`internal/oxidns` exports, `internal/routers/dhcpdns.go`; `PROGRESS-agents.md` A06, A07, A11–A18; `difftest-agents.md`
(R017–R020, R048–R051, R062, R067, R068, W20, T10–T12, T17, X08, X12, L3, O6 review); `proddiff-findings.md` F1;
issue classes of `review-r04.md`, `review-r05.md`, `review-a19.md` and the N7-2 review (HANDOFF 3d).

## 0. Survey

### 0.1 The three modules in Python

```
A16 rule sync (opt-in per device: settings {library, rules})
  loop agent-rule-sync-loop (started by the first admitted request; 5 s; ≤2 workers; round-robin cursor;
    presence_log.observe every round)
  └ RuleSyncWorker.run(agent)  [flock .agent-rule-sync/<h>/worker.lock, non-blocking]
      HA journal active → oxidns_ha_publish.advance; HA pair → only the leader runs
      offline → pending; version < 1.8.30-go → blocked; unfinished latest deployment → pending/running
      remembered comparison (_Comparison: inputs = repo head, receipt stamp, applied_content, profile/service/
        config_version, rule-cache stamp, settings; RECHECK 600 s) or full comparison:
        receipt (rule_sync_store.load) + publish snapshot → rule_changes() → none | content | structure (+ withheld)
      debounce: new key → pending (noticed_at) → ≥5 s later act
      content  → _refresh: Mihomo PUT providers/rule per changed provider | OxiDNS POST rules/update (paths)
                 → native task polled by later rounds → fetch_snapshot → rule_sync_store.advance_content
      structure / HA / non-HTTP providers → _publish: begin_deployment → fetch_snapshot → prepare_candidate
                 (pinned non-rule bytes, isolate_rule_bundle, reload_only) → manager.publish_deployment
  routes GET|PUT /api/agents/<id>/rule-sync; tasks row agent_rule_sync:<id>; rules routes → pinned_withheld

A17 DNS follow (OxiDNS target follows the DNS listener of a Mihomo source)
  loop dns-follow (import; 5 s; IdleRounds) → tick → _round [flock .dns-follow.lock for the whole round]
  └ per bound target: running state → _resume; else source latest succeeded & new → _run:
      state preparing (child id dns-<digest>) → baseline check → publish snapshot → bound entry signature
      → Mihomo GET dns/listener → OxiDNS GET dns/active → unchanged? _finish_unchanged
      → prepare (current draft) must equal baseline (else drift message) → candidate with new addr
      → resources identical → _check_ownership → begin_deployment → job saved → state publishing
      → manager.publish_deployment → _resume → _finalize (draft CAS write, signature) → succeeded
  routes GET|PUT|DELETE /api/agents/<id>/dns-follow, POST …/retry (also tasks run "dns_follow:<id>")

A18 DNS HA (two OxiDNS Agents share a VIP through keepalived on each Agent)
  routes GET|PUT|DELETE /api/agents/<id>/oxidns-ha, GET …/oxidns-ha-environment
  PUT  → oxidns_ha.configure [flock .oxidns-ha.lock]: normalize → upstream_findings → capabilities/env proofs
         → preview both → oxidns_ha_publish.start_enable (journal on both records, payload on disk) → 202
  loop oxidns-ha-publish (import; 2 s; IdleRounds) → reconcile → advance(job) [pair + member flocks, try]
  └ _step: payload/bundle (prepare once) → listener check → handoff prepare → order (standby first)
      → per member: stage/publish child → wait → standby health → (handoff: VIP to standby, confirmed twice)
      → old holder → both healthy → handoff finish → (enable) _configure_keepalived (apply secondary, primary,
        undo on failure) → succeeded
  DELETE → disable: pair locks → HA lock → remove keepalived secondary first → delete fields (force: always)
  publish (A14 hook) / activate / deployment GET / rules (A07 hook) / service_operation (A15 hook) route
  through the journal for paired Agents.
```

Data written: Agent record members `rule_sync{settings,state}`, `dns_follow`, `dns_follow_state`, `dns_follow_job`,
`oxidns_ha`, `oxidns_ha_state`, `oxidns_ha_publication` (+ `ha_publication` summaries inside deployment tasks), the
OxiDNS draft `oxidns` of a profile (DNS follow finalisation only), files `DATA_DIR/.oxidns-ha-publications/<sha256(
sorted member ids joined by "\n")>/{payload.json,bundle.tar.gz}`, the A14 receipts under `.agent-rule-sync/` (through
`Receipts`), task-status rows `agent_rule_sync:<id>`, `dns_follow:<id>` (→ notifications), Python lock files
`.dns-follow.lock`, `.oxidns-ha.lock`, `.agent-rule-sync/<h>/worker.lock`, `.oxidns-ha-publications/<h>/lock`.

### 0.2 Agent side facts (authoritative: `go-agent/routes`)

- `GET /api/rule-sync/snapshot` (monitor auth): gzip tar of `manifest.json` + `files/<path>` of the managed live
  files; the panel never extracts to disk.
- `GET /api/dns/listener?deployment_id=` (Mihomo): `{agent_id, deployment_id, enabled, host, port}`;
  `GET /api/dns/active?deployment_id=` (OxiDNS): `{agent_id, deployment_id}`; `GET /api/dns/peer` (OxiDNS):
  `{peer_agent_id, service_type}` — proves both Agents share one network namespace. Old Agents answer 404.
- `/api/oxidns-ha`: `GET` → `{managed:{active,pending,spec,…}|null, runtime:{vip_present, keepalived_running,
  healthy, vip_healthy,…}, error?}` (no `success` field on success); `POST /preview` (spec) → `{ok, blockers[],
  cidr, interface}`; `PUT` (spec) applies keepalived and restores on its own failure; `DELETE` removes only
  ownership-proven config and restores the service's previous enabled/running state; `/handoff` `GET` →
  `{handoff:{operation_id, active,…}|null}`, `POST`/`DELETE` body `{operation_id}` yields / restores the VIP
  without interrupting DNS. 404 = Agent without HA. Spec: `{safety_version:1, vip, vrid, priority 150|100,
  self_ip, peer_ip}`.
- Capabilities add `oxidns_ha_protocols`, `oxidns_ha_container_protocols`, `oxidns_ha_handoff_protocols`,
  `oxidns_ha_environment{kind, ready, network, host_id, host_id_source, host_boot_id, interface, blockers}`.
- Deployments, rules update (`/api/oxidns/rules/update`) and Mihomo panel calls: as in A14 0.2 / A07.

### 0.3 Go today (`go` at `9e4bbc3d`)

- `internal/agents/compat.go`: pure HA pieces for the registry (`haActive`, `haConflict`, `HASummary`,
  `HAAggregate`, default `service_operation`/`deployment`/`activate` without pair locks; pair deployments →
  `ErrNotPorted`). `Hooks.HAServiceOperation/HADeployment/HAActivate/PublishDeployment` (the last one is deleted by
  A14-2 in favour of `deploy.SetPairPublisher`).
- `agents.IdleRounds` (`NewIdleRounds`, `Due`, `Done`), `agents.ObservePresence`, `agents.SetRuleSyncRow` /
  `SetDNSFollowRow` (the `agent` task slot registers when both exist), `agents.OnManagerInit`,
  `agents.FailureText`, `AppendEvent`, `CompareVersions`, the Agent HTTP client with `NoProxy`/`NoRedirects`/
  `MaxBody` (A06) and `Call`/`DeploymentRequest`/`GetDeploymentDirect`/`ActivateDeploymentDirect` (A01).
- `monitor.SetHARules` (A07 hook; default `ErrHARulesNotPorted` → proddiff F1), `monitor.RuleUpdateCall`,
  `monitor.SupportsPaths`, `monitor.PanelCall`, `RuleUpdateMessages`, `PanelMessages`, `monitor.AgentOrigin`.
- `oxidns`: `PrepareEditor`, `LoadNativeYAML`, `ApplyRouting`, `NormalizeAction`, `EntryKey`, `UpstreamCatalog`,
  `HydrateRuleset`, `ProfileAgents`, `ResourceReferences`, `RelativeResource`, `RulePath`, `RulesetDownloads`,
  `Generate`, `BlackholeTag`, `UpstreamPrefix`; **unexported**: `_analyze`, `_references`, `parse_exec`,
  `pinned_action`, `_ruleset_parts`, `BLACKHOLE_PLUGIN` (A16-0 exports them).
- `lifecycle` v2 (O3-2 merged): `RegisterPeriodic{AtBoot|AfterFirstRequest}`, `Spawn`, `Stopping`, `RequestSeen`,
  `RunRound`; `lockx` (O3-1): `Mutex`, `Keyed`, levels, `Violations`.
- R01 `routers.DNSAgent`/`AgentHA` decode `oxidns_ha`, `oxidns_ha_state.status`, `oxidns_ha_publication.status`
  themselves (routers read; A18 writes).
- A11–A14 are **missing** (designed in `a14-publish.md`); N8-3 (`internal/model` codec) is next on the N8 path.

## 1. Scope

### 1.1 In scope (Python → module)

| Module | Python | Go |
|---|---|---|
| A16 (1371) | `rule_sync.py`, `rule_sync_projection.py`, `rule_sync_snapshot.py`, routes 1727–1748, tasks row `agent_rule_sync`, `pinned_withheld` for the rules routes | `internal/agents/rulesync` |
| A17 (760) | `dns_follow.py`, routes 162–191, tasks row + runner `dns_follow` | `internal/agents/dnsfollow` |
| A18 (1529) | `oxidns_ha.py`, `oxidns_ha_handoff.py`, `oxidns_ha_publish.py`, routes 192–215, 1233–1269, manager HA calls (1732–1780), A01 hook bodies of `compat.go` | `internal/agents/dnsha` |

Also: the stored types of the seven Agent members (2.3) in `internal/model`; deletion of `agents/compat.go`; the A07
(`SetHARules`), A15 (`HAServiceOperation`) and A01 (`HADeployment`, `HAActivate`) hooks; proddiff F1.

Not in scope: A11–A14 internals (called, not changed, beyond 1.3); router-side DHCP/guard logic that reads the HA
fields (R01 `DNSHASettings`, R10 guard, R14 digest — they keep their own typed views, 2.3 is their read contract);
the Agent binary; Docker HA installers (A09/A10, verified); the black-box Step B difftest.

### 1.2 Porting order

A16 → A17 → A18, steps in section 5. A18 imports A16 (`PrepareCandidate`, `FetchSnapshot`, `SettingsFor`); A16
reaches A18 only through one hook (2.6). A17 is independent of A16 and A18 at the Go level (it reads the HA journal
through `agents` predicates) and may run in parallel with A16 once A16-0 is merged.

### 1.3 What must exist first

| Prerequisite | Needed by | State |
|---|---|---|
| A11 (`Pack`, `Open`, `Bundle.Contents/Files`, `Fingerprint`, `Digests`, `RelativePath`, `Retarget`, `Build` with `PinnedFiles`/`LocalDNSWatchdog`, `LocalRule`, limits) | A16, A17, A18 | design done |
| A14 (`PrepareContent`, `Publish`, `PublishDirect`, `SetPairPublisher`, `Receipts.{Load,Stamp,Capture,AdvanceContent}`, `SettingsDigest`, `RenderConfig`, `IsPreparationFailure`, `agentfake`) | all | design done |
| N8-3 `internal/model` record codec (`DecodeRecord`/`EncodeRecord`, `Map`, `Fields`, `Int`) | stored types 2.3 | next on N8 path |
| A07 `RuleUpdateCall`, `PanelCall`, `SetHARules` | A16, A18 | verified |
| C25–C27 OxiDNS routing/rule-DNS functions | A16 | verified |

**Additive A14/A11 exports** (contract additions, agreed by this design; the porter of whichever step lands later
adds them): `deploy.RunDetached(ctx, name string, fn func(context.Context) error) error` (A14 2.4 "Detaching" helper,
exported); `deploy.WriteFileAtomic(path string, data []byte) error` (the `Receipts` writer: `.write-*` temp, fsync,
rename; mode 0600, dirs 0700); `deploy.Refusal` (2.8); `deploy.Source` raw accessors and copy-on-write setters
(2.5.1); `deploy.CanonicalDigest` (2.4, decision D-A17-1). And in `lifecycle`: `Tracked(ctx) bool` (true inside a
Periodic round or Spawn job), which `RunDetached` uses for re-entrancy.

## 2. Interface contract

### 2.1 Package map and import graph

```
internal/model               stored types of 2.3 (leaf; files agentrulesync.go, agentfollow.go, agentha.go)
internal/agents              + ha.go: pure HA predicates over records (replaces compat.go), DNS-follow busy predicate
internal/agents/rulesync     A16  imports agents, monitor, deploy, oxidns, convert(mihomo generator), repo, model,
                                  lifecycle, lockx, taskstatus, yamlx
internal/agents/dnsfollow    A17  imports agents, deploy, oxidns, repo, model, lifecycle, lockx, taskstatus, yamlx
internal/agents/dnsha        A18  imports agents, monitor, deploy, rulesync, oxidns, routerdoc, repo, model,
                                  lifecycle, lockx, yamlx
internal/api/agents          routes: rulesync.go (2), dnsfollow.go (4), dnsha.go (4)
internal/agents/agentfake    + HA, snapshot, dns/*, rule-update and provider endpoints (6.1)
```

Cycles avoided: `deploy` never imports the three packages (pair publication through `SetPairPublisher`, their user
errors through `deploy.Refusal`); `rulesync` never imports `dnsha` (pair advance through `rulesync.SetPairAdvance`,
2.6; pair membership through `agents.HAPair`); `dnsfollow` imports neither. `agents` imports none of them. Each new
package has exactly one file that touches `jsonx` (`adapter.go`, A14 precedent) until N8-5/N8-10 remove the legacy
record API: it decodes the Agent record members into the `model` types of 2.3 and writes them back inside
`UpdateAgents` callbacks.

### 2.2 Shared predicates in `internal/agents` (`ha.go`, replaces `compat.go`)

```go
// HAEnabled is oxidns_ha.enabled / manager._oxidns_ha_enabled: oxidns_ha.enabled is JSON true.
func HAEnabled(agent *Record) bool
// HAActive is oxidns_ha_publish.active: a non-empty journal whose status is not succeeded/failed/rollback_failed.
func HAActive(agent *Record) bool
// HAConflict is oxidns_ha_publish.conflict(agent, deployment_id): nil when free; deploymentID "" = Python None.
func HAConflict(agent *Record, deploymentID string) Result
// HASummary, HAAggregate: as today (compat.go), moved here.
// HAPair is oxidns_ha_publish.pair: (nil, nil) when HA is not enabled; ErrPairIncomplete
// ("DNS 高可用配对不完整，请先修复主备关系", a deploy.Refusal) when the records do not pair.
func (m *Manager) HAPair(ctx context.Context, agentID string) ([]*Record, error)
// DNSFollowBusy is manager._dns_follow_busy (already used by A01; exported).
```

`compat.go` keeps its default hooks until A18-2 registers the real ones; A18-2 deletes it (and `ErrNotPorted` uses for
HA). The predicates read through the `model` types of 2.3 (via the record adapter) so that every reader agrees on
truthiness: `enabled is True` means JSON `true` only (R4/R5: a stored `"true"` or `1` is not enabled — Python
`is True`).

### 2.3 Stored data (Agent record members) and their writers

All live in the system document's Agent records. Types are written in `internal/model` with N8-3's codec (R1 unknown
members kept, R2 unchanged members byte-identical, R3 presence, R5 wrong types kept and read as zero, R8 numbers).
"replace" = Python assigns a new dict (unknown members of the old value are dropped — Go does the same: build a fresh
value); "update" = Python `dict.update`/`setdefault` (unknown members kept).

| Member | Shape | Written by (how) | Read by |
|---|---|---|---|
| `rule_sync` | `{settings, state}` (object kept, `setdefault`) | A16 | A16, A18, C27 `rule_dns_state` (`SyncSettings`), tasks rows, A01 re-registration copy |
| `rule_sync.settings` | `{library: bool, rules: bool}` | A16 `configure` (replace, both members of a pair in one transaction), A18 `_configure_keepalived` (replace on both) | `SettingsFor` = `settings.get(k) is True` |
| `rule_sync.state` | `{status, message, updated_at, deployment_id?, key?, noticed_at? (float s), retry_at? (float s), force_refresh?, native_task?}` | A16 `_update` (replace with `updated_at` when it differs ignoring `updated_at`) | A16, tasks row, status route |
| `dns_follow` | `{source_agent_id, upstream_tag, upstream_index, connection, address?, id, source_profile_id, target_profile_id, upstream_signature, after_deployment_id}` | A17 `configure` (replace), `_finalize` (update `upstream_signature`), `disable` (pop) | A17, A18 `upstream_findings`, C27 `_bindings`, A01 busy checks |
| `dns_follow_state` | `{status, message, updated_at (ISO …Z), events[], source_deployment_id?, target_deployment_id?, attempt?, retry_requested?, address?, draft_synced?}` | A17 `_save_state` (update + events), `configure`/`retry` (replace) | A17, A01 (`begin_deployment`, rollback, re-registration), A14 `Publish` (pair check), tasks row |
| `dns_follow_job` | `{binding_id, profile_id, original_settings_digest, candidate_settings (whole OxiDNS settings, free-form → model.Dynamic/RawMessage), candidate_signature}` | A17 `_set_job` (replace), `configure`/`retry`/`disable` (pop) | A17 `_resume`/`_finalize`; never returned by routes |
| `oxidns_ha` | `{enabled, group_id, peer_id, vip, vrid, role}` | A18 `_configure_keepalived` (replace on both), `_disable` (pop) | A18, A01, A07, A15, A16, routers R01/R10/R14 |
| `oxidns_ha_state` | `{status, error, updated_at (int s), members? []}` | A18 save (replace), `_record_error` (copy + update), `_record_leftover` (replace, only on non-enabled records), `_disable` (pop) | A18, routers R01 (`status == succeeded`) |
| `oxidns_ha_publication` | the journal (below) | A18 `_reserve`, `_save` (replace with a deep copy of the job on every child), `_disable` (pop) | A18, A01/A07/A15/A16/A17 (`HAActive`, `HAConflict`), routers R01 (`status`) |
| `deployments[<child>].ha_publication`, `latest_deployment` | `summary(job)` | A18 `_save` | A01 public views, A14 GET |

Journal (`model.HAJournal`): `id, owner_agent_id, owner_deployment_id, children (Map[string]: agent → deployment id,
order = members order), identities (Map[string]{host, port, profile_id, service_type, enabled} — raw values compared
with Python `==` semantics: numbers by value, so `8080 == 8080.0`), status, phase, message, active_agent_id (null
kept), activate, order [], enable ({peer_id, vip, vrid} | null — always present), updated_at (int), handoff_protocol,
warnings [], routers [{id, name, profile_id}], rule_sync_settings?, kind? ("rules"), members [{agent_id, name,
deployment_id, status}], handoff? {operation_id, source_agent_id, target_agent_id, requested_at (float s), confirmed,
released, confirmations, released_at?}, handoff_required? (**presence matters**: `'handoff_required' in job`),
handoff_failure? {message, status}`. Python-written journals of jobs in flight at the switch must load and resume
(6.2 scenario "resume Python journal").

Files: `payload.json` = `{source_snapshot, resource_config, config_revision, base_url, baseline_deployment_id?,
bundle?{manifest, sha256, config_version, comparison_config}, id}` (sources are whole profile configs, carried as
`deploy.Source` raw JSON); `bundle.tar.gz` read with `deploy.Open` (Python-written archives must load, A14 2.2).
Written with `deploy.WriteFileAtomic`, folder mode 0700. Go creates **no** lock files or lock-only directories
(O3 D2); Python's leftovers are ignored. (This supersedes the A15 review note that A18 "must create" them; the A15
upgrades difftest keeps its exclusion of `.oxidns-ha-publications/*/lock`.)

### 2.4 Persisted identities and encodings

| Value | Python | Go |
|---|---|---|
| HA child deployment ID | owner: its own ID; others `"ha-" + sha256(owner + "/" + deploymentID + "/" + member)[:48]` | same bytes |
| HA enable owner ID / rules owner ID | `"ha-" + uuid4().hex` / `"rules-" + uuid4().hex`; job id `uuid4().hex` | same forms (`crypto/rand` 16 bytes, lowercase hex) |
| HA payload folder | `sha256("\n".join(sorted(children)))` hex | same |
| HA group id | `uuid4().hex`, kept on reconfiguration | same |
| handoff `operation_id` | job id | same |
| DNS follow child ID | `"dns-" + _digest([binding_id, source_deployment_id, attempt])[:48]` | `"dns-" + CanonicalDigest(...)[:48]` |
| `dns_follow.upstream_signature`, `candidate_signature` | `_digest(entry)` of the bound upstream entry | `CanonicalDigest` of the entry (YAML node → JSON via `yamlx.ToJSON`) |
| `dns_follow_job.original_settings_digest`, `prepared_content.settings_digest` | `_digest(config['oxidns'])` | `CanonicalDigest` (= `deploy.SettingsDigest`) |
| `dns_follow.id` | `uuid4().hex` | same form |
| `rule_sync.state.key` | `sha256(json.dumps([source, settings, applied deployment id], sort_keys=True, ensure_ascii=False))` | `CanonicalDigest` of the same triple |
| in-memory only: comparison inputs, `_state_hash` (rule signatures), `_rule_cache_stamp` | various `json.dumps` | Go-native canonical JSON (never persisted, both sides computed in Go) |

`_digest` is `sha256(json.dumps(value, sort_keys=True, ensure_ascii=False))` with Python's **default separators**
(`", "`, `": "`). Its results are persisted and compared on every DNS-follow round: a different encoding makes every
existing binding fail after the switch with `绑定的上游已被编辑或移动，请重新选择绑定，避免覆盖自定义内容` until the user
re-binds. **Decision D-A17-1 (8)**: `deploy.CanonicalDigest(v json.RawMessage) string` writes that exact text for JSON
values (object keys sorted by code point — Python sorts `str` by code point, Go by UTF-8 bytes: identical order;
strings escaped like `ensure_ascii=False`: `\"`, `\\`, `\n`, `\r`, `\t`, `\b`, `\f`, other < U+0020 as `\u00XX`,
nothing else escaped; integers as their decimal text; non-integral or exponent numbers as Python `repr(float)`
(shortest round-trip digits, fixed notation for decimal exponents −4…15 with `.0` for integral values, else
`d.ddde±XX`); `true/false/null`). It replaces A14's Go-native `SettingsDigest` encoding (same function), so A14 7.1's
"settings_digest differs once" disappears. Corpus: 6.2 (Python `json.dumps` on random JSON incl. CJK, emoji,
control characters, U+2028, floats 1e-5…1e22, −0.0, big integers).

Timestamps: `rule_sync.state.{noticed_at,retry_at}` and `handoff.{requested_at,released_at}` are wall-clock Unix
seconds as JSON floats (Python `time.time()`); `oxidns_ha_state.updated_at`, journal `updated_at` are ints;
`dns_follow_state.updated_at` is `datetime.now(utc).isoformat()` with `Z` (microseconds kept), rule sync
`updated_at` the same with `timespec='seconds'`. Go reads ints and floats for all of them (R8: fractional members are
`float64`) and writes the Python form.

### 2.5 Exported API

#### 2.5.1 Additions to `deploy.Source` (A14 adapter, additive)

```go
func (s *Source) JSON() json.RawMessage                 // whole config, without "agents" when built by GetPublishSnapshot(agents=false)
func (s *Source) Member(name string) json.RawMessage     // nil when absent (rule_configs, rule_library, oxidns, system_config, profile_id)
func (s *Source) With(name string, v json.RawMessage) *Source  // copy-on-write; nil v deletes; never mutates s
func SourceFromJSON(data []byte, revision *int64) (*Source, error)
```

Python's "neither function mutates its inputs" becomes structural: projections build new `Source` values.

#### 2.5.2 A16 `rulesync`

```go
const (MinAgentVersion = "1.8.30-go"; PollInterval = 5 * time.Second; RetryAfter = 60 * time.Second;
       RecheckAfter = 600 * time.Second; PinnedWithheldNotice = "指定上游需手动发布")

type Settings struct{ Library, Rules bool }               // model.RuleSyncSettings view
func SettingsFor(agent *agents.Record) Settings            // settings.get(k) is True
func (s Settings) Any() bool

// Service owns the worker state of one Manager (Python: per-repository module globals).
type Service struct{ /* m, receipts, clock, now func() float64 (wall seconds), prepare PrepareFunc,
                         comparisons map[string]*comparison (L5), busy *lockx.Keyed[string] (L1),
                         pending set + 2 slots */ }
func New(m *agents.Manager, r *deploy.Receipts, o Options) *Service
func (s *Service) Status(ctx context.Context, agentID string) (StatusView, error)      // GET; ErrAgentNotFound → 404
func (s *Service) Configure(ctx context.Context, agentID string, body json.RawMessage) (StatusView, error)
func (s *Service) Round(ctx context.Context) error          // one loop round (Periodic "agent-rule-sync")
func (s *Service) Run(ctx context.Context, agentID string)   // one worker (tests; the loop spawns it)
func PinnedWithheld(ctx context.Context, repo *repo.Repository, r *deploy.Receipts, profileID string, ruleIDs []string) (bool, error)

// Used by A18 for rule-only pair publications:
type PrepareFunc func(ctx context.Context, agent deploy.Target, source *deploy.Source, baseline Baseline,
                      settings Settings, deploymentID string, forceRefresh bool) (*deploy.Bundle, error)
func PrepareCandidate(ctx context.Context, agent deploy.Target, source *deploy.Source, baseline Baseline,
                      settings Settings, deploymentID string, forceRefresh bool) (*deploy.Bundle, error)
func FetchSnapshot(ctx context.Context, m *agents.Manager, agent *agents.Record, baseline Baseline) (*deploy.Bundle, error)
type Baseline struct{ Receipt *deploy.Receipt; Bundle *deploy.Bundle } // bundle replaced by the live snapshot

// Pure projection (no I/O; inputs never mutated):
type Kind string // "none" | "content" | "structure"
func ProjectRuleChanges(base, current *deploy.Source, s Settings, service string, withheld map[string]bool) (*deploy.Source, error)
func RuleChanges(base, current *deploy.Source, service string, s Settings) (Kind, *deploy.Source, []string, error)
func RuleSignatures(config *deploy.Source, service string) (withContent, bare string, err error)
func RuleSourceFingerprint(url string, config *deploy.Source, baseURL string) []string      // nil = unidentifiable
func UnchangedRulePaths(oldEntries, newEntries []RuleEntry, base, current *deploy.Source, baseURL string) map[string]bool
func MihomoRuleEntries(doc *yamlx.Node) []RuleEntry
func IsolateRuleBundle(baseline, candidate *deploy.Bundle, service string) (*deploy.Bundle, error)
func ReadBundleFiles(b *deploy.Bundle) (map[string]deploy.Content, error)                  // verified read
func PublishedUpstreams(source *deploy.Source) map[string]bool

func SetPairAdvance(fn func(ctx context.Context, m *agents.Manager, agentID string)) (restore func())
func Wire(m *agents.Manager, r *deploy.Receipts)  // Periodic, task row, routes' service
```

`ProjectionError` (Python `RuleSyncProjectionError`, a ValueError) carries the exact Chinese text.

#### 2.5.3 A17 `dnsfollow`

```go
const UnpublishedChanges = "OxiDNS 有其他未发布的配置或规则变化，请先手动发布这些修改，再重试联动"
const WaitingForTarget   = "Mihomo 已更新，等待 OxiDNS 当前发布完成后再同步"

type Service struct{ /* m, lock *lockx.Mutex (L1 "dns-follow"), rounds *agents.IdleRounds, now */ }
func New(m *agents.Manager, o Options) *Service
func (s *Service) Options(ctx context.Context, targetID string) (OptionsView, error)       // GET
func (s *Service) Configure(ctx context.Context, targetID string, body json.RawMessage) (OptionsView, error) // PUT
func (s *Service) Disable(ctx context.Context, targetID string) (DisabledView, error)      // DELETE
func (s *Service) Retry(ctx context.Context, targetID string) (RetryView, error)           // POST retry, tasks run
func (s *Service) Tick(ctx context.Context) error                                          // Periodic "dns-follow"
func Wire(m *agents.Manager)
```

`Error` (Python `FollowError`) carries the Chinese text. Views are typed structs whose JSON equals Python's
(`binding`, `state` passed through as the stored member — **`dns_follow_job` is never part of a view**).

#### 2.5.4 A18 `dnsha`

```go
const (Protocol = 1; SettleTime = 10 * time.Second; HandoffTimeout = 30 * time.Second)
var Priority = map[string]int{"primary": 150, "secondary": 100}

type Service struct{ /* m, client, haLock *lockx.Mutex (L1 "oxidns-ha"), pairKeys *lockx.Keyed[string] (L1),
                       rounds *agents.IdleRounds, now func() float64, sleep (Clock), prepareContent, prepareRules */ }
func New(m *agents.Manager, o Options) *Service

// Routes
func (s *Service) Status(ctx context.Context, agentID string) (StatusView, error)                     // GET
func (s *Service) Configure(ctx context.Context, agentID string, body json.RawMessage, baseURL string) (ConfigureView, error) // PUT
func (s *Service) Disable(ctx context.Context, agentID string, force bool) (DisableView, error)       // DELETE
func (s *Service) Environment(ctx context.Context, agentID string) (EnvironmentView, int, error)       // GET environment

// Journal (registered as hooks, 2.6)
func (s *Service) Publish(ctx context.Context, agentID string, b *deploy.Bundle, o deploy.PublishOptions) (agents.Result, bool, error)
func (s *Service) Deployment(ctx context.Context, agentID, deploymentID string) (agents.Result, error)
func (s *Service) Activate(ctx context.Context, agentID, deploymentID string) (agents.Result, error)
func (s *Service) ServiceOperation(ctx context.Context, agentID string, op func() (agents.Result, error), uninstall bool) (agents.Result, error)
func (s *Service) Rules(ctx context.Context, agentID, method string) (agents.Result, int, error)
func (s *Service) Advance(ctx context.Context, agentID string) *JournalSummary
func (s *Service) Reconcile(ctx context.Context) error                     // Periodic "oxidns-ha-publish"

// Agent HA protocol (2.7)
type Client struct{ m *agents.Manager }
func (c Client) Status(ctx, agent) (*AgentHAStatus, error); Preview(ctx, agent, Spec) (*PreviewResult, error)
func (c Client) Apply(ctx, agent, Spec) error; Remove(ctx, agent) error
func (c Client) Handoff(ctx, agent, method, operationID string) (*HandoffResult, error)

func Wire(m *agents.Manager)
```

`Error{Message string; Blockers []string; CanForce bool}` (Python `HAError` + `details`); `HandoffPending{Message
string; Unknown bool}`; `ErrStatusUnavailable` (Python `HAStatusUnavailable`, message `无法确认主备的 DNS 健康和虚拟 IP
状态`). Errors returned by `Publish` that Python raised as `ValueError` implement `deploy.Refusal`.

### 2.6 Hooks

| Hook | Python | Registered by | Default until then |
|---|---|---|---|
| `deploy.SetPairPublisher` | `oxidns_ha_publish.publish` | A18 `Wire` | A14 default (fail closed for HA records) |
| `agents.Hooks.HADeployment`, `HAActivate`, `HAServiceOperation` | `deployment`, `activate`, `service_operation` | A18 | `compat.go` (deleted by A18-2) |
| `monitor.SetHARules` | `oxidns_ha_publish.rules` | A18 | `ErrHARulesNotPorted` (proddiff F1) |
| `rulesync.SetPairAdvance` | `publications.advance` from the rule-sync worker | A18 | no-op (the HA loop advances the journal anyway; no device effect) |
| `agents.SetRuleSyncRow` / `SetDNSFollowRow` | `routes/tasks._agent_rows` halves | A16 / A17 | `agent` task slot missing (tasks routes unregistered) |
| `tasks.SetRunner("dns_follow", …)` | `run_task` branch → `dns_follow.retry` | A17 | category unknown |
| rules routes `pinned_withheld` | `rule_sync.pinned_withheld` | caller imports `rulesync.PinnedWithheld` directly (whichever of C32/A16 lands second wires it) | – |
| `lifecycle.RegisterPeriodic` | the three loops | each `Wire` | – |
| `oxidns.SyncSettings` read option (C27) | `rule_sync.settings_for` | A16 passes `SettingsFor` | C27 decodes `rule_sync.settings` itself (same rule) |

Registration order (Python `init_agent_manager`): DNS follow loop, then HA publication loop, then IPv6 (A23); the
rule-sync loop is `AfterFirstRequest`. `Wire` functions are called from `server.wirePackages` in that order.

### 2.7 Agent protocol (panel → Agent), exactly as Python sends it

| Call | By | Request | Timeout (connect, read) | Transport | Panel reading |
|---|---|---|---|---|---|
| rule-sync snapshot | A16 | `GET <AgentOrigin>/api/rule-sync/snapshot`, `Authorization: Bearer`, `Accept: application/gzip` | (3, 90) | **no env proxy, no redirects**, body ≤ `MaxTotalBytes + 8 MiB` (A06 client options) | ≠200 → `无法读取设备当前规则快照，请确认 Agent 已升级且没有其他操作正在进行`; then 6.x checks; transport/tar/JSON/decode errors → `无法验证设备当前规则快照，未继续自动应用` |
| OxiDNS rules update | A16 | `monitor.RuleUpdateCall` GET / POST (+`paths`) | A07 | A07 | `task{id,status,message}`; `RULE_UPDATE_TARGET_UNKNOWN` with paths → retry without paths |
| Mihomo provider refresh | A16 | `monitor.PanelCall(…, "providers/rule", "PUT", {name})` per provider | A07 | A07 | errors carry A07 codes → blocked/failed state |
| dns/listener | A17 | `deploymentRequest GET dns/listener?deployment_id=<source id>` | (5, 30) | A01 `DeploymentRequest` (env proxies) | `success` false or `http_status` → `<name>：<message>`, 404 → `请升级 Agent 至 1.8.24-go 或更高版本后重试`; transport → `<name> 无法连接，请检查网络后重试` |
| dns/active | A17 | `GET dns/active?deployment_id=<baseline id>`; **no query when the id is absent** (requests drops `None` params) | (5, 30) | same | same |
| dns/peer | A17 | `GET dns/peer` | (5, 30) | same | `peer_agent_id`, `service_type` |
| capabilities | A18 | `GET capabilities` | 5 | A01 `DeploymentRequest` | `oxidns_ha_*` fields (2.3 of route R051, 4.1) |
| HA status | A18 | `GET /api/oxidns-ha` | (5, 15) | `agents.Call` (env proxies, Bearer, latin-1 rules) | 404 → `Agent <name> 不支持 DNS 高可用，请升级`; non-JSON or non-object → `{}`; ≠200 or `success:false` → `error` or `message` or `Agent 返回 HTTP <n>` (redacted) |
| HA preview | A18 | `POST /api/oxidns-ha/preview` JSON spec | (5, 30) | same | `ok`, `blockers`, `cidr` |
| HA apply / remove | A18 | `PUT /api/oxidns-ha` JSON spec / `DELETE` | (5, 90) | same | success = no error |
| handoff | A18 | `GET /api/oxidns-ha/handoff` (no body); `POST`/`DELETE` JSON `{"operation_id": id}` | (5, 15) | same | `handoff` record |
| deployments, activate, final preview | A16–A18 | through `deploy.Publish/PublishDirect` and A01 | A14 2.7 | A14 | A14 |

`_call` mapping (A18): a transport error → `<name> 无法连接，请检查网络后重试`; an Agent-reported error (Python
`RuntimeError`) → `<name>：<redacted text>`; anything else (missing host/port/token = `*agents.CallError`) propagates
(500), as Python's KeyError did. In handoff calls **every** error is `HandoffPending{unknown: true}`.

### 2.8 Errors and HTTP mapping

`deploy.Refusal` (`interface{ error; Refusal() }`) marks errors Python raised as `ValueError` on the publish path:
`dnsha.Error`, `dnsha` publish refusals, `agents.ErrPairIncomplete`, `rulesync.ProjectionError`, `dnsfollow.Error`.
`deploy.IsPreparationFailure` returns true for any `Refusal` (push-config → 400, as Python's `except ValueError`).

Each package has `IsUserError(err) bool` = "Python would have caught it as `ValueError`": its own error types,
`deploy.IsPreparationFailure`, `oxidns.SettingsError`/`ValidationError`/`ReferenceError`, repo ValueError kinds,
`agents.ValueError`. Everything else → Flask's 500 (HTML page, as Python; difftest compares status only).

| Route | Case | Status | Body |
|---|---|---|---|
| `GET /<id>/rule-sync` | ok | 200, `Cache-Control: no-store` | `{success, settings, state{status,message,updated_at?}, baseline_ready}` |
| | Agent missing | 404 | `{success:false, message:"未找到这台 Agent"}` |
| | other service | 400 | `规则自动同步仅支持 Mihomo 和 OxiDNS` |
| `PUT /<id>/rule-sync` | any query arg, or Content-Length > 1024 | 400 | `规则同步设置无效` (checked **first**) |
| | body not `{library: bool, rules: bool}` exactly | 400 | `library 和 rules 必须分别为布尔值` (after the 404/service checks) |
| | HA enabling | 400 | `DNS 高可用正在开启，请完成后再修改主备规则自动同步设置` |
| | pair changed during save | 400 | `主备关系已改变，请刷新后重试` |
| | incomplete pair | 400 | `DNS 高可用配对不完整，请先修复主备关系` |
| `GET/PUT/DELETE /<id>/dns-follow`, `POST …/retry` | ok | 200 (retry **202**) | views of 2.5.3 |
| | any `IsUserError` | 409 | `{success:false, message: failure_text(str(e), every Agent token)}` |
| `GET/PUT/DELETE /<id>/oxidns-ha` | ok | PUT: 202 unless `publication.status ∈ {succeeded, failed, rollback_failed}`; else 200 | view |
| | any `IsUserError` | 409 | `{success:false, message: failure_text(…), blockers?: [failure_text each], can_force?: true}` |
| `GET /<id>/oxidns-ha-environment` | not found / not OxiDNS | 404 | `OxiDNS Agent not found` |
| | capabilities raise (any exception) | 502 | `无法连接 Agent，请先检查容器日志和网络` |
| | non-object or `success:false` | 502 | `Agent 能力检查失败，请确认 Agent 已上线` |
| `…/oxidns/rules/update` (A07) for a pair | via `Rules` | 200 / 202 | `{success, task, ha_publication?}`; Rules' refusals → A07's `SERVICE_BUSY` 409 (`*monitor.HABusyError`) |

Request details kept: HA PUT passes `request.host_url` without the trailing `/` (Host header only, difftest R050 B90)
as the payload `base_url` when non-empty, else the owner's `applied_content.base_url`; DELETE `force` is true only for
`?force=1|true` (case-insensitive); DNS-follow and HA bodies are `get_json(silent=True)` (invalid JSON = `None` →
the "参数无效" texts); the Agent-side 409 `another service operation is in progress` on HA calls is an ordinary
`<name>：<message>` refusal (no busy retry, Python same).

All Chinese texts are constants in each package's `messages.go` (byte-identical to Python; tests reference the
constants). Logged lines keep Python's levels and texts (`Agent %s 规则自动同步：%s`, `OxiDNS %s DNS 联动：%s`, `OxiDNS
pair publication stopped (%s)`, …) through `logx`, never with tokens or URLs.

## 3. State and concurrency

### 3.1 A16 rule sync

**Loop** (`Periodic{Name:"agent-rule-sync", Start:AfterFirstRequest, FirstDelay:0, Interval:5s}`): each round reads
all Agents (no history), calls `agents.ObservePresence` (errors logged at DEBUG), rotates by the cursor, and for each
Mihomo/OxiDNS Agent with `SettingsFor(a).Any()` not already pending, while fewer than 2 are pending, spawns
`lifecycle.Spawn(ctx, "agent-rule-sync-worker", s.Run(id))`; `cursor += 1` per Agent visited (Python increments for
every scanned Agent, including skipped ones, until the break). The pending set lives in the Service and survives
rounds (Python's `pending` dict of futures); a worker removes itself when done. Round errors are logged
`Agent rule sync scan failed (<type>)`. `ErrStopping` from Spawn ends the round.

**Worker** (`Run`): `busy.TryLock(agentID)` (L1, Python's per-Agent flock) → held: return. Then `_run`; any error
except cancellation → `_state(blocked|failed, …, retry_at=now+60, force_refresh=true, native_task/key kept)`, where
`blocked` for the codes `RULE_UPDATE_NOT_CONFIGURED, RULE_UPDATE_AMBIGUOUS, API_NOT_CONFIGURED, CONFIG_UNSUPPORTED,
TLS_UNSUPPORTED, CONTROLLER_UNAVAILABLE, UPGRADE_REQUIRED, CORE_UPDATE_UNSUPPORTED` and the message from A07's
`RuleUpdateMessages` (OxiDNS) or `PanelMessages` (Mihomo) by the error's code, else
`规则自动同步失败；将稍后重试，可查看设备发布状态`.

`state.status` transitions (each written only when it differs ignoring `updated_at`; a transition of
`(status, message)` logs once; `succeeded`/`failed` reached from another status records one task-status run
`agent_rule_sync:<id>` with trigger `event`):

| From | Condition (in `_run` order) | To |
|---|---|---|
| any | Agent gone / not Mihomo/OxiDNS; journal active → `PairAdvance`; HA pair and not leader; no scope on; `retry_at` in the future | (unchanged) |
| any | disabled or offline | `pending 等待设备上线后同步规则` (keeps native_task/key/force_refresh; running → force_refresh) |
| any | version < 1.8.30-go or unparsable | `blocked 请先将 Agent 升级到 1.8.30-go 或更新版本` |
| any | latest deployment unfinished (or HA-published) and still unfinished after `GetDeployment` | `running` (own deployment) / `pending 等待配置发布完成，再检查规则变化` |
| running(own deployment) | deployment finished not succeeded | `failed 规则热加载未完成…` retry 60 s |
| running(own deployment) | succeeded | `succeeded 规则已通过原生接口热加载` (re-read) |
| any | remembered unsafe | `blocked 当前原生规则结构无法安全自动合并，请手动检查并发布配置` |
| any | no receipt baseline | `blocked 先成功发布一次配置，建立自动同步基线` |
| any | projection error | `blocked` unsafe (remembered) |
| any | kind none, not resuming | `idle 已引用的规则没有待同步变化` (unless succeeded with the same notice) |
| any | native task tracking (OxiDNS) | running → wait; library off → snapshot + advance → `idle 此前受理的规则更新已结束，继续按当前开关检查`; succeeded+same key → snapshot + advance → `succeeded 已引用的规则集已下载并热重载` / `pending 设备配置已改变，重新检查规则变化`; failed → `failed <native message>` retry; else `pending 继续检查最新规则内容` |
| any | new key | `pending 检测到规则变化，等待连续编辑完成` (noticed_at) |
| pending | < 5 s since noticed_at | (unchanged) |
| pending | content → `_refresh` | `running 正在下载并热重载…` → succeeded / pending / failed (see 0.1) |
| pending | structure → `_publish` | `pending 设备正在执行其他操作，稍后同步规则` (reservation refused) or `running 正在校验规则变化并准备热加载` (deployment_id, key) → `succeeded`/`failed` |

The notice `；指定上游需手动发布` is appended to every message while the comparison withheld a pinned upstream
(OxiDNS). The remembered comparison (`comparison` per Agent, L5 map in the Service, Python's per-repository
`_comparisons`) keeps `inputs`, `outcome` (`none`/`unsafe`/nil) and `withheld`; inputs are `repo.Head`,
`Receipts.Stamp`, canonical `applied_content`, `profile_id`, `service_type`, `config_version`, the rule-cache stamp
(walk of `SharedRulesDir`: path, size, mtime ns; unreadable folders skipped), sorted settings. An entry older than
600 s (or with `now < at`, clock stepped back) starts a new comparison.

`_unchanged(agent, settings, deploymentID)` = same identity tuple (`id, profile_id, service_type, host, port, token,
config_version, applied_content.deployment_id`), same settings, and the latest deployment finished, absent, or the
given ID. It is re-checked before every device call in `_refresh` and before advancing the receipt.

### 3.2 A17 DNS follow

**Loop** (`Periodic{"dns-follow", AtBoot, 0, 5s}`): `Tick` = `IdleRounds.Due` → `round` → `IdleRounds.Done(idle)`.
The round takes `lock.TryLock` (L1; busy → round not idle, no work) and holds it for the whole round including
device calls and preparation (Python's flock around `_round`).

`dns_follow_state.status`: `waiting → preparing → publishing → succeeded | failed`; `failed → waiting` by retry;
configure resets to `waiting 等待 Mihomo 下一次成功发布`. Running set = `{preparing, publishing}`.

Round per bound target (Python `_round`, same order and `idle` bookkeeping): running → `_resume` (not idle); target
and source listed (`_listed`), profiles equal the binding's (else `Agent 的配置绑定已改变，请重新设置 DNS 联动`);
source latest unfinished → `GetDeployment` (not idle); skip unless the latest is `succeeded`, not a manual rollback,
not `after_deployment_id`, and new (or `retry_requested`); target release in flight → `waiting` WaitingForTarget
(written once); re-read target and source; source moved on → skip; else `_run`.

Error handling per target (Python's `except Exception`): message = `str(e)` for user errors, else
`联动处理异常，请检查 Agent 连接后重试`; if the child is submitted and unfinished → `publishing Mihomo 已更新，OxiDNS
发布结果待确认：<msg>`; else an unfinished unsubmitted child is recorded failed (`DNS 联动准备失败`,
`OnlyUnsubmitted`) and the state becomes `failed` with prefix `Mihomo 已更新，OxiDNS 同步失败：` (source id known) or
`DNS 联动暂不可用：`. The round stays idle only when the failure was already recorded, came from stored data alone
(`stored_only`) and was a `dnsfollow.Error` — the IdleRounds rule that kept prod idle CPU low; tests 6.6 pin it.

`_save_state`: no write when status, redacted message, given fields and events are unchanged (and no
`before_save`); `before_save` (`_check_ownership`) runs inside the same `UpdateAgents` transaction (pure, L3-safe).
A `succeeded`/`failed` transition records one task-status run `dns_follow:<id>`; a `(status, message)` change logs
once.

### 3.3 A18 pair journal and enable/disable

Journal `status`: `preparing → applying | checking | unknown → … → ready (activate=false) | succeeded | failed |
rollback_failed`; terminal for `active()` = `{succeeded, failed, rollback_failed}`; `advance` skips
`{succeeded, failed, ready}`; `rollback_failed` → `_reconcile_recovery` (poll children; all resolved →
`failed 设备回滚恢复已确认；原主备发布未完成，可重新发布`). `phase`: `preparing, standby, primary, handoff, keepalived,
done`. Member view statuses (in `_save`): the child's task status; `waiting` (not submitted and not active) or
`skipped` (failed before submission). Every `_save` writes the journal copy to both records, the child task's
`ha_publication` summary and the refreshed `latest_deployment` in one transaction, and fails with
`主备发布记录已经改变，已停止继续操作` when a member's journal id differs.

`_step` (one pass, returns after any wait): handoff failure recorded → `_fail`; members and identities (else
`主备设备信息在发布期间改变，已停止继续操作`); payload (`主备发布资料与当前任务不匹配，请重新发布` /
`主备发布资料校验失败，请重新发布`); bundle: rules job → receipt baseline (`主备规则更新基线已改变，请重新发布配置`) +
`FetchSnapshot` + `PrepareCandidate(library on, rules off, forceRefresh)`; enable → `deploy.PrepareContent` from the
snapshot in the payload; payload rewritten with the bundle; `_require_listener` (wildcard UDP 53, else
`主备候选配置必须保留通配地址的 UDP 53 监听（如 0.0.0.0:53），未更新任何核心`); `handoff.prepare`; order (enable before
both enabled: `[peer, owner]`; else the unique VIP holder last, `无法确认唯一持有虚拟 IP 的主机，未更新任何核心`); then
for each member in order exactly as Python lines 418–481 (standby success + `pending` check, standby health, container
environment re-check, handoff `ensure` before the old holder's upload or activation, stage the old holder when
handoff is required, activation of `ready` children, 400/404/405 activation refusal → `_fail`); not activating →
`ready 两台候选配置已准备…`; both healthy; `handoff.finish`; enable → `keepalived` phase → `configureKeepalived`;
`succeeded 主备配置与规则均已发布成功`.

`advance` error mapping: `HandoffPending` → `unknown|checking`, phase `handoff`, its message; transport errors and
`ErrStatusUnavailable` → `unknown 主备设备连接暂不可用，正在等待确认，未继续更新下一台`; user errors → `_fail(redacted
str)`; other errors → `_fail(主备发布处理失败，请查看配置与设备状态后重试)` with the warning log; **cancellation →
nothing written** (3.7).

`_fail(job, message, status)`: a submitted child still running (not terminal, not `ready`) → `unknown …；已提交的设备结果
仍待确认，未继续另一台`; a job with a handoff → record `handoff_failure`, `release` + `settle_failure`
(`HandoffPending` → `unknown`); otherwise unsubmitted or `ready` children are recorded failed (`主备发布未继续：<msg>`,
`OnlyUnsubmitted` for unsubmitted), preparation keys released, job `status`.

Handoff sub-machine (`handoff` member, all wall-clock): `ensure` creates `{operation_id: job id, source: old holder,
target: standby, requested_at, confirmed:false, released:false, confirmations:0}`, **persists before any POST**,
refuses after `released` (`虚拟 IP 迁移已结束，未继续激活旧候选配置`) and after 30 s unconfirmed (`虚拟 IP 迁移或解析确认超时，
原主机未更新`); GET then POST the yield on the source; the record must be active with this operation id; both members'
views must show the target as the only VIP holder with keepalived running, healthy and `vip_healthy`; two
consecutive confirmations (`confirmations ≥ 2`) before the old holder is touched; a failed view resets the count.
`release` deletes the yield and confirms its absence before `released:true`; `finish` waits ≤30 s after release for the
preferred (primary) member to hold the VIP healthily; `settle_failure` waits for normal election after a failed
handoff when both cores are healthy.

**Enable** (`Configure`): HA lock → `normalize` (2.8 messages; order of checks as Python 87–136) → `upstream_findings`
(blockers: same-namespace DNS follow on either member; loopback upstream addresses in forward plugins of the shared
draft; warnings: unreadable draft, `cleanup_on_shutdown` of `ros_address_list`/`ros_route`) → capabilities + env
proofs of both (`validate_pair_environments`: container readiness, identity sources, boot ids, host ids) → preview
both (blockers, same CIDR, VIP inside) → `startEnable` (same intent while active → current summary; else publish
snapshot, pair keys, `_reserve`, payload, `BeginDeployment` + `FinishDeploymentPreparation` per child, `_save`) →
release HA lock → 202. `configureKeepalived` (journal phase `keepalived`, inside `advance`): HA lock → the same
validation again → apply secondary then primary (undo in reverse on failure: previous spec re-applied, or removal)
→ save `oxidns_ha` + `oxidns_ha_state{succeeded}` + shared rule-sync settings on both (identity/group re-check,
any save failure undoes both devices) → settle (`Status` polled every 1 s up to 10 s while health is `split`/`down`).

**Disable**: pair keys over `{agent, peer}` (busy → `主备配置正在处理，请等待完成后再停用高可用`) → HA lock → active journal →
`主备配置发布尚未结束，请确认两台结果后再停用高可用`; Agent missing → `Agent 不存在`; nothing stored → success;
enabled → members = agent + peer when they still pair by group or peer id; not enabled → leftover members; remove
secondary first; errors without force → `_record_error(failed=false)` + `停用未完成：…。设备离线时可以强制清除记录，之后请在设备上
手动删除 keepalived 中的 ConfigFlow 配置` (`can_force`); else pop `oxidns_ha`, `oxidns_ha_state`,
`oxidns_ha_publication` (a leftover marker never clears a member that joined a working pair since). Deployment history
(`ha_publication` inside tasks) is kept.

**Status** (GET): `Advance(agent)` first (Python calls the loop starter, a no-op in Go, and `advance`), then members
(`_member`: offline → `Agent 离线` without a device call; status errors and unmanaged/pending configs as messages),
`health` (`split` | `down` | `degraded` | `ok`, Python `_health`).

### 3.4 Locks

| Lock | Level | Key | Held | Busy → |
|---|---|---|---|---|
| rule-sync worker | L1 `Keyed[string]` | agent id | one `Run` | skip (no write) |
| DNS follow | L1 `Mutex` "dns-follow" | – | a whole round; `Configure`/`Disable`/`Retry` bodies | 409 `DNS 联动正在处理，请稍后重试` (routes); round not idle |
| HA | L1 `Mutex` "oxidns-ha" | – | `Configure` (validation → reservation), `configureKeepalived`, `disable` body | 409 `DNS 高可用正在处理，请稍后重试`; in `configureKeepalived` see D-A18-1 |
| pair / member | L1 `Keyed[string]` "oxidns-ha-pair" | `"pair:" + sorted ids joined "\n"`, then `"member:" + id` for sorted ids | `advance`, `startEnable`, `publish` reservation, `activate` order write, `disable`, `ServiceOperation` (single child) | per caller: summary returned / `DNS 主备正在处理，请稍后重试` / `主备配置正在处理，请等待完成后再停用高可用` / `主备发布正在处理，请稍后确认` / 409 `DNS 主备正在处理，请等待两台确认完成` |
| deployment preparation | L1 (A14/O3-6) | (agent, deployment) | `BeginDeployment` → `FinishDeploymentPreparation` | A14 |
| repo | L3 | – | each `UpdateAgents`/`UpdateProfileTransaction` callback (pure: no I/O) | lock timeout |
| receipts per Agent | L4 (A14) | – | file writes | waits |
| comparisons, pending set, IdleRounds | L5 | – | memory only | – |

All L1 acquisitions are try-only, so the orders that differ between paths (HA `Configure`: HA lock → pair keys;
`Disable`: pair keys → HA lock; HA loop: pair keys → preparation keys → HA lock in `configureKeepalived`; DNS follow
round: follow lock → preparation keys → pair keys via `deploy.Publish`; rule sync: worker key → preparation key → pair
keys) cannot deadlock (O3 3.2 rule 1). Multi-key acquisition is all-or-nothing: on the first busy key every key taken
so far is released before returning (Python's `ExitStack`). Locks are not reentrant: a nested `advance` of the same
pair inside the loop (`configureKeepalived` → settle → `Status` → `Advance`) finds the keys busy and returns the
summary, exactly as Python's second `flock` in one process did. `lockx.Violations()` stays 0 (asserted in
`TestMain` of the three packages).

**Lock before validation** (O3 2.5; Python order kept, reviewers check):
- rule-sync PUT: query/length 400 → Agent 404 → service 400 → body 400 → HA-enabling 400 → pair save. No L1 lock.
- dns-follow PUT/DELETE/retry: **follow lock first** (409 even for a malformed body) → validation.
- oxidns-ha PUT: **HA lock first** → normalize. DELETE: Agent read → pair keys → HA lock → checks. GET: `Advance`
  (pair keys, try) → Agent missing 409.
- tasks run `dns_follow:<id>` → `Retry` (follow lock first).

### 3.5 Interplay with A14 publishes and router releases

- Every automatic publication reserves through `BeginDeployment`, so A01's row-1 refusals apply: an unfinished task,
  an upgrade, a DNS-follow job of the Agent or of its bound source (`绑定的 OxiDNS 正在同步…`), an HA conflict
  (`HAConflict`: a journal owns another child id). A user's push-config during a rule-sync `_publish` gets the
  ordinary 409 and vice versa.
- `deploy.Publish` calls the pair publisher first; `Service.Publish` returns `handled=false` when `HAPair` is nil
  (single Agent → `PublishDirect`), so rule sync, DNS follow and push-config share one path. Pair publications from
  DNS follow and rule-only bundles require equal `applied_content.fingerprint` on both members (`主备配置基线不同，请先
  完整发布一次主备配置，再自动同步规则`).
- `PublishDirect` is called only by the HA journal for pair children (never `Publish`, which would re-enter).
- Rollback (A01) refuses HA members and pair tasks; activation of a pair child goes through `Activate` (journal
  order); restart/update/uninstall go through `ServiceOperation` (member key + `HAConflict`; uninstall of an enabled
  member → `请先停用 DNS 高可用，再卸载成员`).
- Routers never call into A16–A18. R01 `DNSHASettings` reads the stored members at plan time: the VIP is handed out
  only while both `oxidns_ha_state.status == "succeeded"` and both journals are absent or succeeded; R14's rule-sync
  digest includes `{group_id, vip, peer{id, host}}` only while enabled. A18 must keep these shapes (2.3) and never
  write `oxidns_ha_state.status = succeeded` before both keepalived applies succeeded. `routers_using` (profiles ×
  `routerdoc.RoutersOf`, routers whose `dns_agent_id` is a member) is reported, never published.

### 3.6 Cancellation and detaching (PORTING 4b)

| Entry point | Context |
|---|---|
| Periodic rounds, rule-sync workers | lifecycle round/job ctx (cancelled only at the drain deadline) |
| HA `Status`, `Configure`, `Disable`; `Publish`/`Deployment`/`Activate`/`Rules` hooks when called from a request | `deploy.RunDetached(ctx, "oxidns-ha-…", fn)`: Spawn + wait; inline when `lifecycle.Tracked(ctx)` |
| DNS follow `Configure`, `Disable`, `Retry`; rule-sync `Configure` | `context.WithoutCancel` at the entry (short: one or two device calls with their own timeouts, then a repo write) |
| rule-sync `Status`, DNS follow `Options`, HA `Environment` | request ctx (reads; `Options` performs no device call) |

Every Agent call has its own timeout from 2.7 derived from the detached/job ctx. Python's synchronous `time.sleep`
waits (settle 1 s steps, busy retries) wait on the lifecycle Clock and return at `Stopping`.

### 3.7 Shutdown and crash recovery

Rule (A14 3.4, O3 3.5): **when the job ctx is cancelled, nothing further is recorded** — every catch-all in the three
packages (`Run`'s `except`, `_publish`'s failed record, DNS follow's per-target `except`, `advance`'s `_fail`,
`configureKeepalived`'s undo-and-record) first checks `ctx.Err() != nil` and returns `context.Canceled` unwritten.
Python was killed at that point; writing "failed" or retiring staged HA candidates would be wrong. **No recovery
action is added**; the existing Python recovery paths resume from stored state:

| Stage when stopped (SIGTERM drain end or `kill -9`) | Stored | After restart |
|---|---|---|
| rule sync `running` + own deployment preparing (unsubmitted) | state running, task preparing | next round: `GetDeployment` → A01 row 7 `Preparation interrupted…` → `failed 规则热加载未完成…` retry 60 s |
| rule sync native task accepted | state running + `native_task` | next round polls the task by id (`native_accepted_task_survives_process_restart_without_reposting`) |
| rule sync between refresh and advance | state running | `resuming` → re-fetch snapshot / refresh again (Python same) |
| DNS follow `preparing`, child not reserved / reserved unsubmitted | state preparing | `_resume`: no job → `联动准备被中断，请重试` → failed; reserved → A01 row 7 → failed via the child |
| DNS follow `publishing`, child submitted | state publishing + job | `_resume` polls the child; succeeded → `_finalize` (CAS draft write) |
| DNS follow finalised draft write done, state not yet succeeded | draft written, state publishing | `_resume` → `_finalize` again: draft equals candidate → no write; signature rebind idempotent → succeeded |
| HA journal at any step | journal + payload on disk | loop `Reconcile` resumes `_step`; submitted children polled, never re-uploaded (`_publish_deployment` replay rule of A14 for same ID + digest) |
| HA handoff yield POSTed, response lost | `handoff` persisted before the POST | `ensure` GETs the record first (`lost_handoff_reply_is_reconciled_after_restart…`) |
| HA keepalived apply in flight (`configureKeepalived`) | journal phase `keepalived`, records not yet enabled | next round repeats `configureKeepalived` from validation (Agent `PUT` is idempotent per spec) |
| HA enable route between reservation and response | journal `preparing` | the loop continues; a repeated PUT with the same intent returns the summary |
| HA disable between device removal and record deletion | records still enabled, keepalived removed | status shows members `设备上没有 DNS 高可用配置，请重新开启`; a second DELETE removes (Agent DELETE of absent config succeeds) and clears |

In-memory losses (Python same): comparisons, IdleRounds, pending set, busy keys (L1 keys die with the process, as
flocks did; `Held` is false after restart).

## 4. Failure and rollback

### 4.1 HA enable / reconfigure / disable

| Step | Failure | Devices | Records | User sees |
|---|---|---|---|---|
| validation, findings, protocols, environment proofs, preview | any | untouched | untouched | 409 with message (+ `blockers`) |
| reservation (`startEnable`) | identity changed, other journal, disabled, upgrading, unfinished deployment | untouched | untouched (journal not written) or `_fail` cleans children | 409 |
| content preparation, listener check | user error | untouched | journal `failed`, children failed `主备发布未继续：…` | HA card publication failed |
| standby (peer) publish | Agent refuses / rolls back | standby keeps old config (Agent transaction) | journal `failed`/`rollback_failed` | `备机发布失败：<msg>` |
| standby unhealthy after success | – | standby has new config; primary untouched | `failed 备机发布后的 DNS 健康检查未通过，主机未更新` | same |
| primary publish fails | Agent rolls back primary | standby new, primary old (configs differ until next publish; both answer — no keepalived yet on first enable) | `failed 主机发布失败：…` | same |
| keepalived apply secondary fails | – | nothing applied (Agent restores itself) | journal failed with `<name>：<msg>` | message |
| keepalived apply primary fails | – | secondary undone (`DELETE` or previous spec) | undo ok: first enable → no record; reconfigure → `_record_error(failed=false)` (status kept) | `<cause>` |
| undo fails | – | keepalived may remain on the undone member | first enable → `_record_leftover` (state only, `members`); reconfigure → `_record_error(failed=true)` → routers stop handing out the VIP | `<cause>；回滚未完成：<errors>`; disable cleans leftovers |
| record save fails after both applies | – | both undone | as above | `保存 DNS 高可用记录失败，请重试` or the HAError |
| disable: a member unreachable | – | others removed | `_record_error(failed=false)` | 409 + `can_force` |
| disable with force | – | unreachable member keeps keepalived (VIP may stay there) | fields deleted | warnings list the errors; the person removes keepalived on the device |

Guarantees: `oxidns_ha.enabled` is written only after both devices applied keepalived; a record is never left
half-enabled; routers hand out the VIP only after `succeeded` on both; the secondary is applied first and removed
first so the VIP does not move on the way in or out.

### 4.2 Pair publication

| Situation | Behaviour |
|---|---|
| exactly one VIP holder | standby (non-holder) first; holder last |
| no holder / both hold (split brain) | `failed 无法确认唯一持有虚拟 IP 的主机，未更新任何核心` — no core touched |
| status unreadable | `unknown` (retried every round, nothing published) |
| standby result unknown / pending | `unknown 正在确认备机发布结果，暂不继续`; primary untouched |
| standby succeeded, health unknown | `unknown` (transport) — primary untouched |
| handoff required (restarting bundle on a paired member) | standby activated → VIP moved to standby (yield on old holder), confirmed twice from both views → old holder staged then activated → yield released → normal election confirmed |
| handoff timeout (30 s unconfirmed) | `_fail` → release yield → settle → `failed`; old holder untouched |
| lost yield/release reply | `HandoffPending{unknown}` → `unknown`; next round reads the record (GET) and continues |
| reload-only (rules) bundle | no handoff (`handoff_required:false`); standby first, then holder, both hot-reload |
| old Agents without handoff protocol | refused before any core change (`<name> 的 Agent 不支持发布前迁移虚拟 IP，请先升级两台 Agent`) |
| legacy journal (no `handoff_protocol`) | `handoff_required:false` (never strands a Python-era job) |
| staged pair (`activate:false`) | both `ready`; `Activate` recomputes the order from live holders at activation |
| child `rollback_failed` | journal `rollback_failed` → `_reconcile_recovery` polls until the user recovers the child (A01 recover) |

Split-brain detection is reported (`health: split`) by `Status`; the panel never "fixes" a split by itself (no new
recovery action). Partial updates (one member new, one old) are visible as member statuses and in the journal message;
the next manual publication from either member republishes both.

### 4.3 DNS guard and routers

A18 changes what DHCP DNS should be, never router state. After enable, `routers` in the response/journal lists the
routers whose DHCP DNS uses a member (manual release needed); the router publish (R12) picks the VIP up through R01.
The R10 DNS guard targets the VIP and exempts both real hosts (tests `test_guard_*` in `test_oxidns_ha.py` belong to
routers R10/R12; A18 only guarantees the stored fields).

### 4.4 DNS follow

| Failure | Writes | Device |
|---|---|---|
| validation / reachability / pinned upstream / profile already bound | none (409) | none |
| no baseline / drift (`_drift_message`) / bound entry edited / listener disabled or loopback / active mismatch | state failed (message) | none |
| resources changed between the two preparations | failed `OxiDNS 规则文件在准备期间发生变化，请先手动发布后重试` | none |
| reservation refused | failed `<A01 message>` | none |
| child upload/activation failure | failed from the child (`error_details`/`error`/`message`) | Agent transaction rolled back by the Agent |
| child `rollback_failed` | state `publishing OxiDNS 回滚失败，请先在发布记录中恢复服务并确认结果` (no retry until recovered) | per A14 |
| finalisation: draft edited meanwhile | `succeeded … ；发布期间 OxiDNS 草稿有新修改，已保留草稿；请核对后再发布`, `draft_synced:false` | new config live; draft untouched |
| finalisation: ownership changed | failed `OxiDNS 当前发布或绑定已改变，未覆盖草稿，请核对设备状态后重试` | live config stays |

The source (Mihomo) is never rolled back by DNS follow; its success stands (`Mihomo 已更新，…`).

### 4.5 Rule sync

Rule-only bundles carry the deployed non-rule bytes (pinned files, isolated main config, `reload_only:true`); the
Agent hot-reloads rule data without restarting the core. Failures leave the baseline receipt unchanged; the next
attempt waits 60 s (`retry_at`). Projection refusals block until the inputs change. A native task that cannot be
verified by a snapshot never advances the baseline (`successful_native_call_without_verified_snapshot…`).

## 5. Buildable steps

Every step: own branch off `go`; compiles; `gofmt -l` empty; `go vet ./...`; `go test -count=1 ./...` and `-race`
on touched packages; `server/scripts/proddiff.sh` on .32 shows no new difference (A18-4: F1 entries turn stale and
the baseline is re-recorded); PROGRESS section updated with commits, decisions, the Python-test coverage map, and
**one line per issue class** of 6.8 saying how the step avoids it. Sizes: S ≤ 1 day, M 1–2 days, L 3–4 days.

| Step | Content | Size | HR | After |
|---|---|---|---|---|
| A16-0 | Prerequisite exports: `oxidns.Analyze`, `References`, `ParseExec`, `PinnedAction`, `RulesetParts`, `BlackholePlugin` (thin exports, no copies); `lifecycle.Tracked`; `deploy.RunDetached`, `WriteFileAtomic`, `Refusal`, `Source` accessors (2.5.1), `CanonicalDigest` (+ corpus vs Python `json.dumps`, D-A17-1) and `SettingsDigest` switched to it; `internal/model` types of 2.3; `agents/ha.go` predicates + `HAPair` + `ErrPairIncomplete` (compat.go delegates to them). | M | yes | A14-4, N8-3 |
| A16-1 | `rulesync` projection (pure): `ProjectRuleChanges`, `RuleChanges`, `RuleSignatures`, `RuleSourceFingerprint`, `UnchangedRulePaths`, `MihomoRuleEntries`, `IsolateRuleBundle` (Mihomo + OxiDNS document isolation, helper checks, download/reload lists), `ReadBundleFiles`, `PublishedUpstreams`. Corpus `testdata/gen_a16_projection.py`: real Python on synthetic profiles (library/rules combinations, native graphs, pinned upstreams, custom plugins, offline sources) → projected sources compared structurally, kinds/withheld exactly, error texts exactly; isolation over Python-built bundle pairs; alias-chain/self-reference main files (6.8). | L | yes | A16-0 |
| A16-2 | `FetchSnapshot` (A06 client options, decompression budget, every check of `rule_sync_snapshot.py`), `PrepareCandidate`, reusable/changed rule paths. Corpus: Python-built snapshot archives incl. every tampering case of `test_rule_sync_snapshot.py`; candidate bundles vs Python (manifest paths/roles/digests of pinned bytes exact, YAML structural). | M | yes | A16-1 |
| A16-3 | Worker (`Run`, `_run`, `_refresh`, `_publish`, state writes, comparisons), loop (`Round`, presence, ≤2 Spawn, cursor), `Status`/`Configure` + routes R067/R068, task row, `PinnedWithheld`, `SetPairAdvance`; fake-clock tests for every timing rule; transcript differential 6.2 (rule-sync scenarios). | L | yes | A16-2 |
| A17-1 | `dnsfollow` binding side: `Options`, `Configure`, `Disable`, `Retry`, entries/scheme/network-host/pinned checks, digests, routes R017–R020, task row + runner. | M | yes | A16-0 |
| A17-2 | Round: `Tick` (IdleRounds), `_run`, `_resume`, `_finalize`, `_check_ownership`, unchanged paths, drift messages; transcript differential 6.2 (follow scenarios) + every idle-round rule of `test_dns_follow.py`. | L | yes | A17-1 |
| A18-1 | `dnsha` client (2.7), `normalize`, `validate_pair_environments`, `upstream_findings`, preview, `routers_using`, `Status` (members, health), `Environment` + route R051 (and the deferred A09 test), GET R049. | M | yes | A16-0 |
| A18-2 | Journal: `_reserve`, `_save`, payload, `_fail`, `_members`, `_order`, `_require_listener`, `_child_bundle`, `_step`, handoff (`prepare/ensure/release/finish/settle_failure`), `advance`, `Reconcile` loop, `Deployment`, `Activate`, `Publish` (`SetPairPublisher`), `Rules` (`monitor.SetHARules`), `ServiceOperation`; register A01/A15 hooks; delete `compat.go`; `SetPairAdvance`. Transcript differential 6.2 (pair scenarios). | L | yes | A18-1, A16-3 |
| A18-3 | Enable/disable: `Configure` → `startEnable`, `configureKeepalived` (undo, leftover, record error, settle), `Disable`/force, PUT/DELETE R050/R048; D-A18-1. | M | yes | A18-2 |
| A18-4 | Fault scenarios (6.5) in the O3-8 harness; proddiff F1 closure (6.4); A15/A07 difftests re-run with the real hooks; PROGRESS coverage map final; L3 plan file. | M | yes | A18-3, A17-2, O3-8 |

Then: fresh-Opus HR reviews (A16, A17, A18 — one each, or A17+A18 together), black-box Step B rows, lab batch L3.

## 6. Acceptance

### 6.1 Fake Agents (`agentfake`, extended)

Per server under test one fake set (difftest X13); a **pair** is two stateful fakes sharing a simulated L2 segment
object that models the VIP: each fake has `keepalived_running`, `managed{active,pending,spec}`, `healthy`,
`vip_healthy`, a priority from its spec; the segment elects the holder (highest priority among running members, a
yield record lowers its owner) after a scripted delay (default 4 s, Python's measured value; 0 in unit tests), and can
be scripted to split (both hold / none holds), drop a member, or make health checks fail. Endpoints: everything of
0.2 with the Agent's validation (spec shape, 404 for "old Agent", handoff record per operation id, idempotent PUT/
DELETE, environment proofs incl. host/boot ids), the rule-sync snapshot built from the fake's live files (with
tampering switches for every case of `test_rule_sync_snapshot.py`), `dns/listener|active|peer`, native rule update
tasks (running → succeeded/failed per GET), Mihomo provider PUTs, deployments (A14 fake). Fault script per endpoint as
A14 6.1 (delay, status/body, reset before body, lost response after the state change, timeout). Transcripts tag
request- vs loop-originated calls.

### 6.2 Python reference (transcript differential)

`testdata/gen_a16.py`, `gen_a17.py`, `gen_a18.py` run the real Python modules (loops driven by explicit `tick`/
`reconcile`/worker calls, clocks injected) against their own `agentfake` instances (`cmd/agentfake` with the same
script); Go runs the same scenarios with `RunRound`/`Run`. Compared per scenario: request transcripts (method, path,
headers, JSON bodies, manifests/file digests; archive bytes tokenised as in A14 6.2), the seven Agent members after
each step (timestamps normalised; `events` times normalised), `deployments`/`latest_deployment`, profile `oxidns`
draft, payload/receipt files (structure), task-status rows, HTTP status and body of routes, and *which writes
happened* (commit count per round — the idle-round rules). Scenarios: every test name of 6.6 that drives state, plus:
resume of a Python-written journal/payload/state at every step (switch in flight), canonical digests of Python-made
bindings (D-A17-1), and all 4.x rows. Zero differences except section 7.

### 6.3 Black-box difftest rows (Step B, not by the porter)

R017–R020, R048–R051, R062 (HA pair update), R067, R068, R061 (HA GET), R066/R077/R078 (HA service operations),
R063 (push-config of an HA member), W20, T10–T12, T17, T26 (restart in the middle of follow/HA/rule sync), X08 (loop
start rules: rule sync only after an admitted request; DNS follow and HA at boot), X10 (auto-backup on state writes),
X12 (double-click enable, rule-sync loop vs manual push). Harness rule from O3 5: compare only after both sides are
quiet (events file, fake transcripts stopped growing).

### 6.4 Proddiff F1 closure

After A18-2 registers `monitor.SetHARules`, `GET /api/agents/<HA member>/oxidns/rules/update` answers like Python:
200 `{success:true, task:{id:"", status:"idle", phase:"idle", …}}` when the member's last journal is not a rules job,
or the task view of the last rules job (no device call for a terminal journal). Run `server/scripts/proddiff.sh` on
.32: the F1 baseline entries (route + `GET /api/logs/info|tail` consequences) report `stale`; re-record the baseline
with the reason `F1 closed by A18-2`, set F1 `closed` in `proddiff-findings.md`. If the snapshot ever holds an active
journal, Python's import-time loop advances it while Go's proddiff build runs loops manually — record it as a harness
difference, not a Go finding.

### 6.5 Fault injection (O3-8 harness, .32, loopback fakes)

SIGTERM and `kill -9` at each row of 3.7 (fake delays hold the stage), restart, let the loops run: final records equal
the "after restart" column; no duplicate upload (fake transcript); no stuck L1 key. Concurrency: two HA PUTs (one 202,
one same summary or 409), PUT while the journal is in `keepalived` phase (D-A18-1), DNS-follow PUT during a round
(409), rule-sync loop vs manual push to the same Agent (one 409), HA status GET while the loop advances (summary,
no second device call).

### 6.6 Python tests → coverage

| Python test file | Covered by |
|---|---|
| `test_rule_sync_projection.py` (35) | A16-1 corpus (inputs taken from each test) + `IsolateRuleBundle` cases |
| `test_rule_sync_snapshot.py` (23) | A16-2 snapshot corpus (one archive per test) |
| `test_agent_rule_sync.py` (51) | receipt cases → A14 `Receipts` tests (already A14's); route cases → A16-3 route tests + R067/R068; worker/loop/comparison cases → A16-3 fake-clock tests (hand-ported: timing/memory not observable by corpus) + 6.2 scenarios |
| `test_dns_follow.py` (70) | A17-1 binding/route tests; A17-2 6.2 scenarios; idle-round cases hand-ported with commit counting |
| `test_idle_rounds.py` (5) | already `agents.IdleRounds` (A02) |
| `test_oxidns_ha.py` (37) | A18-1/A18-3 (enable/disable/status/health/findings/http); DHCP/guard/router cases → routers R01/R10/R12 (their PROGRESS lists them) |
| `test_oxidns_ha_publish.py` (28), `test_oxidns_vip_handoff.py` (13) | A18-2 6.2 pair scenarios (one per test) |
| `test_oxidns_ha_docker.py` (9), `test_oxidns_ha_identity.py` (8 — companion cases → A09/A10 installer), `test_oxidns_ha_compact_identity.py` (7 — companion cases → A10) | A18-1 `validate_pair_environments` table tests + 6.2 |
| `test_oxidns_ha_unchanged.py` (6) | routers (guard/DHCP/inspection/digest) + A01 re-registration; A18 asserts the stored shapes |
| `test_oxidns_ha_guards.py` (3) | A18-2 (rules envelope, disable keeps history) + C26/C27 native-mutation guard |
| config deferred: `test_udp_fallback.py`, `test_oxidns_domain_only_native.py`, `test_operation_logs.py` (rule-sync/follow logs), `test_rule_compose_route.py` (`pinned_withheld`), `test_scheduled_tasks.py::test_router_agent_dns_follow_domain_and_backup_rows` (agent rows) | A16-1 signatures, A16-3/A17-1 logs and rows |

### 6.7 L3 lab items (PVE lab, after the reviews; plan in `server/parity/lab/L3.md`)

Only real guests prove: (1) two native OxiDNS Agents (Debian/systemd and Alpine/OpenRC) on the `cfgt` bridge: enable
HA → keepalived on both, VIP held by the primary, routers' view `succeeded`; (2) kill the primary guest → VIP on the
secondary within keepalived's timing, DNS answered on the VIP throughout (dig loop, count failures); bring it back →
preemption; (3) a full publish of the pair with handoff: VIP moves to the standby before the old holder restarts, no
DNS gap beyond the measured handoff; (4) lost replies (nft drop on the lab bridge) during yield and release → `unknown`
→ reconciles; (5) forced keepalived apply failure on the primary → secondary undone; undo failure → leftover →
disable cleans it; (6) disable with one member offline → `can_force` → force → manual cleanup instructions hold; (7)
Docker HA pair on two hosts (host and macvlan), same-host pair refused by boot id; (8) rule sync on real Mihomo and
OxiDNS: content-only refresh hot-reloads without a core restart, structure change publishes reload-only, offline
device catches up; (9) DNS follow: Mihomo republish with a changed `dns.listen` → OxiDNS upstream follows without a
resolution gap; same-namespace (AIO) connection; (10) panel SIGTERM/`kill -9` mid-handoff and mid-follow → 3.7 rows.
Safety rules of WORKFLOW "VM lab tests" apply (new guests only, recorded owner file, never touch prod VMs).

### 6.8 Issue classes (R04, R05, A19, N7-2) — what each means here

| Class | Here |
|---|---|
| R05-1 shared client behaviour | snapshot fetch must keep `NoProxy` + `NoRedirects` (others use env proxies); HA calls `agents.Call` latin-1 rules; params with absent values dropped; test each |
| R05-2 error type on write paths | every refusal is a package error type with a `messages.go` constant; `IsUserError` decides 409/400 vs 500; never `fmt.Errorf` text to users |
| R05-3 identity proofs never match on "missing" | where a stored id proves that a write or delete targets *our* object — handoff `operation_id` on the device, journal `id` in `_save`, `dns_follow.id == job.binding_id` in `_finalize`, the child id in `_resume`, the payload `id` — an absent or empty value is a mismatch, never `"" == ""`. "Unchanged" comparisons of the same record before and after (`_identity`, `_unchanged`, journal `identities`) keep Python's equality (absent before and after = unchanged); a test shows that a record *losing* its host or token stops the journal |
| R05-4 id-addressed undo | keepalived undo re-applies the *previous spec* only to the member it was applied to; handoff release deletes only its own `operation_id` |
| R05-5 read-back loops index once | rule-cache stamp walk, `_rule_cache_stamp`, member lookups by id map, not repeated scans |
| R04-F3 cancellation abandoning accepted work | 3.6/3.7: detached entries, nothing recorded on cancel, rounds finish their current device step |
| R04-F1 malformed replies panic | every Agent answer decoded into typed structs with R5 tolerance; no indexing of unchecked lists (`blockers`, `members`, manifest `files`) |
| A19-F1 / N7-2-F1 alias amplification | YAML read from devices or drafts (snapshot main file, native drafts, candidate documents) is compared with `yamlx.Equal` and copied by node moves, never `yamlx.Clone` or YAML→JSON expansion; tests with depth-40 alias chains and self-references in the baseline main file, a native draft and a snapshot (must finish fast or refuse with a message, never hang or crash) |
| A19-F2 document wrappers | isolation/listener checks treat a `DocumentNode` as its content and refuse non-mappings |
| A19-F3 Unicode white space | `_require_listener`'s `listen.strip()` and `rpartition(':')`, network-host checks: Python `str.strip()` strips Unicode white space; test NBSP/ideographic space |

## 7. Python behaviour intentionally not kept (→ O12 ledger)

- Lock files `.dns-follow.lock`, `.oxidns-ha.lock`, `.agent-rule-sync/<h>/worker.lock`, `.oxidns-ha-publications/<h>/
  lock` and their lock-only directories are not created (O3 D2; one process per DATA_DIR).
- On shutdown, rounds and pair steps get the drain window; when cancelled they record nothing (Python was killed).
- **D-A18-1 (if accepted):** a busy HA lock in the `keepalived` phase keeps the journal `applying` and retries next
  round instead of failing an enable whose cores are already published (Python: `_fail` with
  `DNS 高可用正在处理，请稍后重试`).
- Python bug fixed: rule sync `_publish` recorded `failed` on any exception, even after the upload was submitted
  (a late repository error could mark a deployment failed that the Agent then activated; A01 never moves a failed
  task again). Go records with `OnlyUnsubmitted` (unsubmitted → failed; submitted → left to the poll).
- Python bug fixed (A14 7.2, inherited): receipts and `pinned_withheld` resolve the Agent's default profile instead of
  the literal `"default"`.
- If D-A17-1 is decided B: every DNS-follow binding must be re-selected once after the switch.

## 8. Decisions

Taken by the designer (tell the maintainer, no action needed): three packages `rulesync`, `dnsfollow`, `dnsha`; stored
types in `internal/model`; `agents/ha.go` predicates replace `compat.go`; `rulesync.SetPairAdvance` hook breaks the
Python import cycle; no lock files (O3 D2, supersedes the A15 review note); loops keep Python's start modes and timings
(rule sync after the first admitted request); synchronous `advance` in HA GET/publish/activate/deployment routes kept
(parity: a status GET may run one pair step; it runs detached and tracked); no new recovery actions; the HA payload
cache may keep the decoded bundle in memory per (job id, archive sha) — an optimisation without behaviour change.

For the maintainer:

**D-A17-1 — keep DNS-follow bindings valid across the switch.** Python persists digests of `json.dumps(…,
sort_keys=True, ensure_ascii=False)` (upstream signatures, child IDs, the settings digest) and compares them on every
round. (A, **recommended**) Go writes that exact canonical text in one shared function (`deploy.CanonicalDigest`,
~80 lines, corpus-checked against Python): existing bindings, in-flight jobs and `settings_digest` keep working; it also
removes A14's one-time "unpublished changes" message (A14 7.1 bullet 3 goes). (B) Go-native canonical JSON as A14 D2
chose: after the switch every binding answers `绑定的上游已被编辑或移动…` on the next Mihomo publish and must be
re-selected by hand, once.

**D-A18-1 — HA lock busy during the keepalived phase.** Python fails the whole enable (both cores already published)
when a concurrent HA request (e.g. a repeated enable click doing its device preview) holds the HA lock at that moment.
(A, **recommended**) Go treats it as busy: journal stays `applying`, next round (2 s) retries; ledger entry. (B) keep
Python's failure; the person enables again.

No other decision is open; everything else follows Python.

**Decided 2026-11-17:** **D-A17-1 → B (Go-native canonical JSON)**, by the maintainer: after the switch every DNS-follow
binding reports "绑定的上游已被编辑或移动" once at the next Mihomo publish and must be re-bound once by hand; it is stable
afterwards. This must appear in the O12 ledger, the release notes and the O14 switch plan (step: re-bind DNS-follow
upstreams after the first Mihomo publish). The A14 note "`settings_digest` changes once at the switch" stays.
**D-A18-1 → A (retry next round)**, by the coordinator: when keepalived-phase enablement finds the HA lock busy, Go retries
in the next 2 s round instead of failing the whole enablement; record in O12.

