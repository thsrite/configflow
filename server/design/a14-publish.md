# A14 — Agent publishing chain (A11 bundle → A12 push diff → A13 content sync → A14 publish)

Status: design, 2026-11-17 (Opus). Codex ports A11, A12, A13 and A14 to this design (WORKFLOW "HR modules: Codex
ports, Opus reviews"); a fresh Opus reviews A11 and A14 (HR). Changes to section 2 need the designer's or
coordinator's agreement. Template: WORKFLOW.md "Design docs, hand-offs and escalation". Builds on
`design/o3-concurrency.md` (locks L1–L5, `lifecycle.Spawn`, PORTING 4b, shutdown, crash recovery) and
`design/n7-n8.md` (typed model rules R1–R9, yamlx on yaml.v3, D1–D3 decisions) without changing them.

Inputs read for this design: Python `agents/deployment_bundle.py`, `deployment_locations.py`, `mihomo_resources.py`,
`generated_preview.py`, `push_diff.py` (interface and build function), `content_sync.py`, `manager.py`
(begin/record/get/activate/rollback/recover, `_remember_deployment_content`, `publish_deployment`,
`_publish_deployment`, `push_config_to_agent`), `oxidns_validation_receipt.py`, `rule_sync_store.py`,
`config_generator.py`, `deployment_messages.py`, `oxidns_ha_publish.publish`, `linux_gateway.py` (guards),
`routes/agents.py` (216–245, 885–920, 1340–1748, 1749–2006), `routes/generate.py` (artifact routes); the
**Agent side** `backend/agents/go-agent/routes/{deployment.go,deployment_rollback.go,final_config.go,register.go}`
(the protocol contract — Agents in the field are not changed); the Go registry `internal/agents/{deployments,hooks,
remote,client}.go` (A01, verified); `PROGRESS-agents.md` A01, A11–A20; `difftest-agents.md` (R010, R013–R016,
R023–R025, R063–R065, W04–W09, T05–T09, T26, X01, X06, X12, L2, O6 review); `review-r04.md`,
`review-r05.md` (branch `go-opus-review-r05`) issue classes; A19's public API (branch `go-opus-review-a19`).

## 0. Survey

### 0.1 The chain in Python

```
route push-config ─┬─ (Surge) generate_surge_config → manager.push_config_to_agent → POST /api/config/update
                   └─ begin_deployment (record "preparing", flock key) 
                      → _prepare_transactional_content (generators, provider bundle, router split / gateway
                        guards) → build_deployment_bundle (rule materialization, MosDNS conversion, packing)
                      → commit_provider_bundle → manager.push_config_to_agent → publish_deployment
                        → oxidns_ha_publish.publish (pair) | _publish_deployment:
                           GET /api/capabilities → retarget → (Mihomo) prepare_mihomo_resources
                           → (OxiDNS) validate_bundle → _remember_deployment_content → ipv6 enroll
                           → rule_sync_store.capture → record "uploading" submitted=true
                           → (OxiDNS) publication checkpoint → POST /api/deployments (gzip tar)
                           → record_deployment(upload_receipt)
route GET  deployments/<id>          → get_deployment (HA wrapper) → _get_deployment → GET /api/deployments/<id>
route POST …/activate|rollback|recover → manager (A01) → POST /api/deployments/<id>/<action>
route generate-config → _prepare_transactional_content(store_downloads) → finalize_bundle (capabilities +
                        mihomo resources + POST /api/config/final-preview) → generated_preview.save
GET /api/agents (list/detail)  → content_sync.get_content_sync (2 workers, prepare candidate, fingerprint
                                  vs applied_content) ; push-diff → content_sync.get_push_diff → push_diff.build
```

Data written: the Agent record in the system document (`deployments`, `latest_deployment`, `applied_content`,
`config_version`, `config_revision`, `pushed_at`, `prepared_content`, `oxidns_validation_receipt`), files under
`DATA_DIR/.agent-rule-sync/<sha256(agent)>/<sha256(deployment)>/`, `DATA_DIR/.generated-previews/<id>/`,
`DATA_DIR/.agent-deployment-locks/` (flock files, Python only), subscription/rule caches (C13/C14/C17).

### 0.2 Agent side facts (from `go-agent/routes`, authoritative)

- `GET /api/capabilities` → `deployment_protocols:[1]`, `service_type`, `config_path` (basename),
  `reload_only_protocols`, `final_config_protocols`, `rule_sync_snapshot_protocols`, Mihomo: `mihomo_resources`
  (names of present Geo files), `config_directory`. Older Agents lack some keys.
- `POST /api/deployments`: headers `X-Deployment-ID` (`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,95}$` on the Agent; the panel
  allows `[A-Za-z0-9_-]{1,80}`), `X-Content-SHA256` (lowercased by the Agent, compared with the body),
  `X-Activate` (`"false"` = stage only; anything else activates). Replay of a known ID: same digest → 200 with
  the stored state, other digest → 409 `deployment ID already belongs to different content`. Busy → 409
  `another service operation is in progress`. Blocked by an unrecovered deployment → 409. Accepted → **202**
  `{"success":true,"deployment_id":id,"status":"verifying"}`; the Agent worker then extracts, validates and either
  activates or stops at `ready`.
- Archive contract: gzip; tar members `manifest.json` (≤ 4 MiB, JSON, `reload_only`/`final_config` must be JSON
  booleans when present) and `files/<path>` regular files (`TypeReg`/`TypeRegA`; dirs allowed under `files/`;
  no links). Manifest: `protocol_version==1`, `deployment_id` = header, `service_type`, `config_path` = the
  Agent's basename, `agent_id` = the Agent's own ID when it knows it, every listed file present with equal size
  and **lowercase hex** SHA-256, count equal, config listed. Size ≤ 512 MiB compressed, ≤ 1 GiB expanded,
  ≤ 10000 files.
- Agent states: `receiving, verifying, ready, stopping, backing_up, replacing, reloading, starting, checking,
  rolling_back, succeeded, failed, rolled_back, rollback_failed, recovery_pending`. `GET /api/deployments/<id>`
  returns the durable state (404 when unknown). `POST …/activate` (only from `ready`; `succeeded` → 200 state),
  `…/rollback` (manual; only the active successful deployment with complete backups; replay of a finished manual
  rollback → 200), `…/recover` (states needing recovery; Agents < 1.8.57-go answer 404/405). All accepted actions
  answer 202 and run in an Agent worker holding the Agent's service-operation lock.
- On success the Agent reports `config_version` = full SHA-256 of the installed main file; after a manual
  rollback the first 8 hex of the restored file.
- `POST /api/config/final-preview` `{"content": text}` → 200 `{"success":true,"content":text}` or 400
  `{"success":false,"message":…}` (64 MiB body limit).

### 0.3 Go today

A01 (verified) ports the registry half: `Manager.BeginDeployment`, `RecordDeployment` (all merge rules,
`RecordOptions{OnlyUnsubmitted, UploadReceipt, ActivationReceipt, SkipUnchanged}`), `GetDeployment[Direct]`,
`ActivateDeployment[Direct]`, `RollbackDeployment`, `RecoverDeployment`, `DeploymentRequest` (busy retry,
sanitising, DNS report), `PushConfigToAgent` (legacy `/api/config/update`; transactional branch calls the
`Hooks.PublishDeployment` hook → `ErrNotPorted`), `Hooks.ReceiptMatches` (default false: OxiDNS activation refused),
HA defaults in `compat.go`. All on `*jsonx.Object`. The preparation lock is still a flock file
(O3-6 replaces it by `lockx.Keyed`). Nothing of A11–A14 exists. Config generators (`convert`, `mosdns`,
`oxidns`), `provider` (C17, with `SetLocations` waiting for A11), `rulefetch`, `refresh`, `routersplit`, `gateway`
(A19, ported, under review) exist and take `*jsonx.Object`; `substore.SetContentSyncInvalidator` waits for A13.

## 1. Scope

In scope (Python files → module): A11 `deployment_bundle.py`, `deployment_locations.py`, `mihomo_resources.py`,
`generated_preview.py`; A12 `push_diff.py`; A13 `content_sync.py` + routes 216–245, 1633–1726 and the GET
branches of `handle_agents`/`handle_agent_item`; A14 manager 1013–1157, `oxidns_validation_receipt.py`,
`rule_sync_store.py`, `config_generator.py`, routes 885–920, 1340–1632, 1749–2006. Also the small A01 changes
the hooks require (2.6) and the C34 artifact helpers (`routes/generate.py` uses `_pack` and previews).

Not in scope: the registry merge rules (A01, verified — called, not changed, except 2.6); HA pair publication
(A18 registers into the hooks of 2.6); rule-sync projection (A16), DNS follow (A17), gateway installation (A22),
IPv6 verification (A23) — they consume the API of 2.9; the Agent binary; the black-box Step B difftest (written by a
non-porting Codex from `difftest-agents.md`); N8 model types for the Agent record (N8-4e) — until then the record is
reached through the A01 legacy API and one adapter file (2.2).

**Port order.** A11 → A12 → A13 → A14, steps in section 5. A12 and A13 are non-HR (Codex port, Codex review by a
different agent); A11 and A14 are HR (Codex port, fresh-Opus review, then lab batch L2). A11 may start now (its
config prerequisites C05, C12, C14, C15, C21, C22, C25 and A19 exist); A14-1 needs C17/C18–C23/C26 (exist) and the
interim hooks of 2.6 for A18/A20/A23.

## 2. Interface contract

### 2.1 Package map

```
internal/deploy                (A11 + A14 core; HR)       imports agents, convert, mosdns, oxidns, provider, rulefetch,
                                                            refresh, routersplit, gateway, subs, substore, repo, yamlx,
                                                            lifecycle, lockx (when present), logx
internal/deploy/pushdiff       (A12; pure)                 imports deploy (Bundle, Digests), yamlx
internal/deploy/contentsync    (A13 checker)               imports deploy, pushdiff, lifecycle
internal/agents/agentfake      (test helper package, imported only by _test files and difftests) — section 6.1
internal/api/agents            routes: deploy.go (A14 9 routes), agentlist.go (A13 4 routes + GET branches),
                               public.go (PublicAgent)
```

`agents` never imports `deploy` (deploy imports agents); everything the registry calls in deploy goes through
`agents.Hooks` (2.6). No package below `api` imports `api/agents`. `deploy/adapter.go` is the **only** file of the
three new packages that imports `jsonx` (A19 precedent): it converts the legacy `*jsonx.Object` profile snapshot,
Agent record and Agent answers into the typed values below and builds A01 `RecordDeployment` updates. N8-4e/N8-10
replace the adapter; nothing else changes then.

YAML: the new packages use only the new `yamlx` API (`Parse`, `Marshal`, Node helpers, `Equal`, `FromJSON`,
`ToJSON`) — no `pyyaml`, no legacy `yamlx.Load*`/`SafeDump*`/`Marked`. Every YAML comparison with Python in tests is
structural (`yamlx.Equal` after parsing); rule/provider/custom bytes that Python copies verbatim are compared byte
for byte.

### 2.2 Types (deploy)

```go
// Role of a delivered file. Stored and sent as these exact strings.
type Role string
const (RoleConfig Role = "config"; RoleProvider = "provider"; RoleRule = "rule"; RoleCustom = "custom";
       RoleResource = "resource")   // resource: Mihomo Geo databases, never part of the fingerprint

// File is one manifest entry (Agent: DeploymentFile).
type File struct {
    Path   string `json:"path"`
    Size   int64  `json:"size"`
    SHA256 string `json:"sha256"`          // lowercase hex, always written lowercase
    Role   Role   `json:"role"`
    Name   string `json:"name,omitempty"`  // display name, failure_text(name, 512)
}

// Manifest is manifest.json (Agent: DeploymentManifest). Field order = Python's insertion order.
type Manifest struct {
    ProtocolVersion int    `json:"protocol_version"` // always 1
    DeploymentID    string `json:"deployment_id"`
    AgentID         string `json:"agent_id"`
    ProfileID       string `json:"profile_id"`
    ServiceType     string `json:"service_type"`     // mihomo | mosdns | oxidns (and surge/loon for C34 artifacts)
    ConfigPath      string `json:"config_path"`      // basename
    Files           []File `json:"files"`            // sorted by path when built by Build
    ReloadOnly      bool   `json:"reload_only,omitempty"`
    FinalConfig     bool   `json:"final_config,omitempty"`
}

type Content struct{ Data []byte; Role Role }

// Bundle is an immutable delivery: manifest, file contents, archive packed once on first use.
// After packing, the file map is dropped and Files/Contents read from the archive (Python's memory behaviour).
type Bundle struct{ /* manifest; files map[string]Content; configVersion; comparison *File;
                      once sync.Once; archive []byte; sha256 string; packErr error */ }

func Pack(m Manifest, files map[string]Content, configVersion string, comparison *File) (*Bundle, error) // _pack
func Open(m Manifest, archive []byte, sha256, configVersion string, comparison *File) (*Bundle, error)  // stored archive
func (b *Bundle) Manifest() Manifest                 // deep copy
func (b *Bundle) Archive() ([]byte, error)           // packs on first call; never re-packs
func (b *Bundle) SHA256() (string, error)
func (b *Bundle) ConfigVersion() string              // sha256(config bytes)[:8] for built bundles
func (b *Bundle) Comparison() *File                  // comparison_config (copy) or nil
func (b *Bundle) Files(paths ...string) (map[string][]byte, error)   // bundle.files(paths); all when none given
func (b *Bundle) Contents() (map[string]Content, error)              // rule_sync_store.bundle_files
func (b *Bundle) Equal(o *Bundle) (bool, error)                      // DeploymentBundle.__eq__ (manifest, archive, sha,
                                                                     // config_version, comparison)
```

Archive format (Go-native, deterministic for equal inputs): `compress/gzip` (header ModTime zero, default level),
`archive/tar` `FormatPAX`, members `manifest.json` (`encoding/json` of `Manifest`, `SetEscapeHTML(false)`, no
trailing newline) then `files/<path>` in sorted path order, `Typeflag TypeReg`, `Mode 0600`, `ModTime` Unix 0,
empty uname/gname. **Archive bytes differ from Python's** (separators, tar headers, deflate); only the Agent reads
them, and it checks the digest the panel sends (accepted, 7.1). Go must **read** Python-written archives (receipts
and previews on disk at the switch): extraction uses `archive/tar` on any PAX/ustar input; manifest equality checks
are structural (decoded `Manifest` + `reflect.DeepEqual`), never byte comparisons.

```go
// Profile snapshot captured once per publication. Read-only. Until N8 it wraps the legacy compat config.
type Source struct{ /* legacy *jsonx.Object (adapter.go), revision *int64 */ }
func (s *Source) ProfileID() string; // + the few typed accessors deploy needs (server_domain, rule library entries via
                                     // gateway.Config / repo helpers, oxidns settings node, mihomo.linux_gateway)

// Target is the Agent fields the chain reads, decoded from the registry record (adapter.go).
type Target struct {
    ID, Name, ProfileID, ServiceType, DeploymentMethod, Version, Host string
    Port   json.RawMessage           // passed back to agents.Call unchanged
    Token  string
    DomainDiscovery bool
    Record *agents.Record            // = *jsonx.Object today: what agents.* calls take; opaque to deploy logic
}

// PreparedContent is prepared_content / applied_content (stored in the Agent record). Order as Python.
type PreparedContent struct {
    Fingerprint          string            `json:"fingerprint"`           // "v1:" + 64 hex
    ProfileID            string            `json:"profile_id"`
    ServiceType          string            `json:"service_type"`
    ConfigPath           string            `json:"config_path"`
    BaseURL              string            `json:"base_url"`
    Files                map[string]string `json:"files,omitempty"`       // canonical path → sha256; dropped from
                                                                          // non-latest tasks by BeginDeployment
    DeployedConfigSHA256 string            `json:"deployed_config_sha256,omitempty"` // oxidns only
    SettingsDigest       string            `json:"settings_digest,omitempty"`        // oxidns with a source
}

// ValidationReceipt is oxidns_validation_receipt (server-owned evidence; never taken from an Agent answer).
type ValidationReceipt struct {
    Version      int    `json:"version"`        // 1
    RulesSHA256  string `json:"rules_sha256"`   // RulesFingerprint()
    AgentID      string `json:"agent_id"`
    DeploymentID string `json:"deployment_id"`
    ProfileID    string `json:"profile_id"`
    ServiceType  string `json:"service_type"`   // "oxidns"
    SHA256       string `json:"sha256"`         // archive digest
    Fingerprint  string `json:"fingerprint"`
}

// Receipt is the rule-sync publication receipt (rule_sync_store receipt.json) + its archive.
type Receipt struct {
    Source           json.RawMessage `json:"source"`            // profile snapshot without "agents", opaque here
    Manifest         Manifest        `json:"manifest"`
    SHA256           string          `json:"sha256"`
    ConfigVersion    string          `json:"config_version"`    // config file sha256 (64 hex)
    BaseURL          string          `json:"base_url"`
    ComparisonConfig *File           `json:"comparison_config"`
    Bundle           *Bundle         `json:"-"`                 // set by Load(withBundle)
}

// Update is a panel-originated RecordDeployment update. Nil/zero = key absent (record() copies only present keys).
type Update struct {
    Status, Message, Error, ErrorDetails, FailedStage *string
    Success, Submitted, ManualRollback *bool
    HTTPStatus     int                // 0 = absent
    SHA256, SourceSHA256, ConfigVersion *string
    ConfigRevision *Revision          // present-with-null is a real case (snapshot without revision)
}
type Revision struct{ Value int64; Null bool }
```

`agents.Record` (an Agent record or a task record) and `agents.Result` (an Agent/registry answer) are written
this way in this document; on `go` today both are `*jsonx.Object`. A11-1 adds `type Record = jsonx.Object` and
`type Result = jsonx.Object` to `internal/agents` (aliases, no behaviour) so that deploy's signatures do not name
jsonx and N8-4e retypes them in one place.

Agent answers (upload/status/activate results) stay the A01 type (`agents.Result`); deploy only reads `status`, `http_status`, `message`, `deployment_id`, `success`, `manual_rollback` through
adapter accessors and passes the object back to `RecordDeployment` unchanged.

### 2.3 Encodings and persisted identities

| Value | Where persisted | Go encoding | Across the switch |
|---|---|---|---|
| `Fingerprint(m)` = `"v1:" + sha256(json)` of `{"files":[{"path","role","sha256","size"}…sorted by path],"service_type","version":1}`; main file path canonical `config.yaml`, digests lowercased; role `resource` is **refused** like any unknown role, so callers fingerprint the bundle without Geo resources (Python's `content_bundle`) and with `comparison_config` substituted | `applied_content`, `prepared_content`, receipts, HA jobs | struct with members in alphabetical order, `encoding/json`, `SetEscapeHTML(false)`, U+2028/U+2029 unescaped (A11 review F4), trailing newline trimmed — **byte-identical to Python** for every valid path | unchanged for unchanged file bytes |
| `Digests(m)` canonical path → sha256 | `prepared_content.files`, `applied_content.files` | `map[string]string` (JSON object, sorted keys) | unchanged |
| config_version | task + Agent record | `sha256(config bytes)[:8]`; Agent answers 64 hex | unchanged |
| archive `sha256`, `source_sha256` | task record, receipt dir name `bundle-<sha>.tar.gz` | sha256 of Go archive | **changes**: a same-ID retry of a Python-era deployment answers 409 "Deployment ID already belongs to different content" (7.1) |
| `SettingsDigest(oxidns)` = sha256 of canonical JSON of the stored `oxidns` member | `prepared_content.settings_digest` (A17 compares) | canonical = decode (UseNumber) → `encoding/json` (sorted map keys, `SetEscapeHTML(false)`), D2 = Go-native. One function, used by A17 too | changes once (A17 says "OxiDNS 有其他未发布的配置或规则变化…" until the next publish) |
| `RulesFingerprint()` | `oxidns_validation_receipt.rules_sha256` | sha256 over the embedded OxiDNS schema files and the server build (`debug.ReadBuildInfo` main version + `vcs.revision`; `version.Version` when absent) | changes: staged OxiDNS `ready` candidates from Python are refused at activation (fail closed, 7.1) |
| preview `input_digest`, content-sync input key, agent identity | memory / preview record (TTL 1 h) | canonical JSON as SettingsDigest | irrelevant (transient) |

**Clarified in A11 (coordinator, 2026-11-17):** `oxidns.SchemaFingerprint() string` is a read-only
summary of sorted embedded schema names and bytes (NUL separators). `deploy.RulesFingerprint` combines it
with the native validator build identity as designed; Python's five validator source files are not copied.
This is a server validation receipt identity, separate from the byte-compatible content `Fingerprint`.
Python-era OxiDNS ready candidates fail closed and require a fresh push (7.1/O12). Tests lock content
fingerprint parity separately from schema/build invalidation and the Chinese re-push diagnostic.

### 2.4 Exported API

**A11 (`deploy`)**

```go
const (MaxFileBytes = 64 << 20; MaxTotalBytes = 256 << 20; MaxFiles = 4096
       maxRawCacheBytes = 16 << 20; maxRawCacheEntries = 128; downloadWorkers = 8)

func RelativePath(v string) (string, error)                       // relative_path
func Fingerprint(m Manifest) (string, error)                      // deployment_content_fingerprint (+ its validation)
func Digests(m Manifest) (map[string]string, error)               // deployment_content_digests
func ComparisonManifest(m Manifest, comparison *File) Manifest    // config entry replaced by comparison_config
func Retarget(b *Bundle, configPath string) (*Bundle, error)      // retarget_config_path
func RenderConfig(main *yamlx.Node, service string, localDNSWatchdog bool) ([]byte, error) // render_deployment_config

type BuildInput struct {
    Target            Target
    Source            *Source
    ConfigContent     string
    ProviderDownloads []ProviderFile   // {Name, LocalPath, URL, Content}
    RulesetDownloads  []convert.Download
    CustomFiles       []CustomFile     // {Path, Name, Content, Native}
    PinnedFiles       []PinnedFile     // A16: {Path, Content, Role}
    BaseURL           string
    DeploymentID      string           // "" → random 32 hex
    LocalDNSWatchdog  bool             // Python default true
    Progress          Progress         // nil-safe struct of the three callbacks (2.4 A14 throttling)
}
func Build(ctx context.Context, in BuildInput) (*Bundle, error)   // build_deployment_bundle

func MaterializeRule(ctx context.Context, url string, src *Source, baseURL string, o MaterializeOptions) ([]byte, error)
func LocalRule(url string, src *Source, baseURL string, rulesLocalOnly bool) (*RuleRef, error) // _local_rule (A16)
func ConvertMosDNSRules(content []byte, ruleType string, mixed bool) ([]byte, error)          // + L5 result cache 256/16 MiB
func PrepareMihomoResources(ctx context.Context, b *Bundle, c Capabilities, src *Source, p Progress) (*Bundle, error)
func RequiredGeoResources(doc *yamlx.Node, files map[string]Content) []string

// Locations (deployment_locations): message builders, also registered as provider.SetLocations.
func SourceError(message, text string, at Position, cause error, sourceLabel string) *PreparationError
func PreparationContext(err error, name, path, kind string) *PreparationError
type Position struct{ Line, Column, PayloadIndex *int }  // PayloadIndex -1 = the payload key itself
```

**Clarified in A11 (coordinator, 2026-11-17):** `convert.Download` gains optional `Behavior` and `Format`
fields (`json:"behavior,omitempty"`, `json:"format,omitempty"`). Convert populates them when the final consumer
is known; deploy/adapter.go only reads them. Existing public download encodings remain unchanged (`Object`
and the proddiff download DTO keep their current fields). This preserves classical domain/IP splitting without
changing BuildInput's type; the N8 typed model will own Download later.

Positions come from yaml.v3 Node `Line`/`Column` (`yamlx.Parse` keeps them); the "last duplicate key wins" lookup of
Python is moot under D1 (duplicates are a parse error, whose `*yamlx.SyntaxError` line/column is used). The provider
hook's `SourceError(…, cause *pyyaml.Error, …)` signature is adapted in the step that registers it (and changes with
N7-3 for provider); message texts are byte-identical to Python for the same coordinates.

```go
// Previews (generated_preview): store under DATA_DIR/.generated-previews, TTL 3600 s, 24 previews, 512 MiB.
type Previews struct{ /* repo data dir, L4 mutex, clock */ }
func (p *Previews) Save(ctx context.Context, rec PreviewInput) (*Preview, error)
func (p *Previews) Load(id, agentID string) (*Preview, error)               // *PreviewChanged on any defect
func (p *Previews) RequireCurrent(ctx context.Context, m *agents.Manager, pv *Preview) error
func (pv *Preview) Public() PreviewPublic                                    // generated_preview.public
func (pv *Preview) Zip() ([]byte, error)                                     // download(): ZIP_DEFLATED, files order of Contents
func (pv *Preview) File(path string) ([]byte, bool, error)                   // read_final_agent_file
func FinalizeBundle(ctx context.Context, m *agents.Manager, t Target, b *Bundle, src *Source) (*Bundle, error)
func AgentIdentity(t Target) []byte                                          // canonical JSON of agent_identity
```

**A12 (`deploy/pushdiff`)** — pure, no I/O except reading bundles:

```go
func Build(agent AppliedView, candidate *deploy.Bundle, receipt *deploy.Receipt, lists map[string]*FileEntries) (Diff, error)
func (f *FileEntries) Page(change string, offset, limit int, q Query, find redact.StoredSecretFinder) Page
func SearchQuery(text string) Query
func WithoutStored(v any, find redact.StoredSecretFinder) any   // applied to typed Diff/Page before the response
// Diff/Page are typed structs whose JSON equals Python's dict (field names, null vs absent, display-text unions
// "plain string | list of string and {"secret": text}" as a named type with MarshalJSON). Constants as Python.
```

**A13 (`deploy/contentsync`)**

```go
type Checker struct{ /* clock, 2 slots, entries (≤512), details (≤8, ≤48 MiB), running, generation */ }
func New(opts Options) *Checker      // Options{Clock, Workers 2, SuccessTTL 900 s, ErrorTTL 30 s, MaxEntries 512,
                                     //          MaxDetails 8, MaxListBytes 48 MiB, Prepare PrepareFunc}
type PrepareFunc func(ctx context.Context, t deploy.Target, src *deploy.Source, baseURL, deploymentID string) (*deploy.Bundle, error)
func (c *Checker) Status(ctx, t, src, baseURL) Result            // get_content_sync; never blocks on preparation
func (c *Checker) PushDiff(ctx, t, src, baseURL) any             // get_push_diff
func (c *Checker) Entries(ctx, t, src, baseURL, EntriesQuery) (any, error)   // get_push_diff_entries; ErrNoEntries → 404
func (c *Checker) CompareNow(ctx, t, src, baseURL) Result         // compare_now (A23), runs in the caller
func (c *Checker) Invalidate()                                    // registered with substore.SetContentSyncInvalidator
```

One `Checker` per `*agents.Manager` (created in `deploy.Wire`); Python's "repository identity" checks become this
ownership (tests build their own checker; there is no process-global repository swap in Go). Workers run with
`lifecycle.Spawn("agent-content-sync", …)` bounded by the two slots (O3 P21); the slot is taken before Spawn and
released in the job's defer; results land only if the entry is still the same object (Python's token rule).
`Status` answers within the request; it never waits for a worker.

### Clarified in A13 (coordinator, 2026-11-17)

These decisions supersede the A13 wiring sentence above; no `deploy` → `contentsync` import is added.

- `api/agents` is the composition owner: its routes consume Checker results and it already depends on deploy.
  It holds one Checker per `*agents.Manager`. `WireContentSync` is registered from server.wirePackages;
  `SetContentPrepare(contentsync.PrepareFunc)` is the composition seam A14 will populate. Until A14, preparation
  fails closed with `unknown/check_failed`; tests inject real synthetic bundles through the same seam.
- `deploy.Source.InputKey() (deploy.InputKey, error)` returns an opaque comparable digest, for memory only.
  It removes exactly Python's top-level `agents`, `backup`, `_revision`, `updated_at`, `created_at`, `name`,
  `description`. **profile_name is included**, as are arbitrary nested metadata-named values. The existing
  private preview digest shares the projection implementation but additionally ignores profile_name, as
  Python generated_preview.input_digest explicitly does. The content-sync digest is not D2/persisted identity.
  The combined input key additionally includes Python's Agent identity fields (`id`, `profile_id`,
  `service_type`, `config_version`, complete `applied_content`) and `base_url`. Legacy reads stay inside
  deploy's adapter, exposing only typed comparison state and opaque digests to contentsync.
- A shared Dispatcher broadcasts Sub-Store invalidation to every registered Checker. Registration returns
  an idempotent unregister function. `Manager.OnClose` registers cleanup, `Manager.Close` runs it once;
  singleton replacement closes its old Manager. Shutdown closes composition-owned Checkers and removes
  their registrations; late workers cannot repopulate closed/invalidated entries. Broadcast/unregister
  ordering is synchronized, and tests prove delivery to every live registration and none after unregister.
- Owner entry points detach via lifecycle.Detach. Jobs use lifecycle.Spawn (an injectable Spawner in tests),
  take one of two slots before spawning and a lockx.Keyed LevelJob identity reservation; every rejection,
  panic, close, cancellation and completion releases both exactly once. Cache guards stay memory-only.
  Default clocks come from lifecycle.ClockFrom; fake-clock tests retain the production 900 s / 30 s defaults.
- A13-1 and A13-2 share branch go-port-a13 and are separate buildable commits. A13-1 owns all Checker cache,
  quota, invalidation and synchronous comparison rules; A13-2 owns the four GET routes and consumes A24's
  publicAgent helper by cherry-picking **78b796af**. Do not duplicate that helper. A14 supplies production
  PrepareFunc later; routes and Checker are tested with injected preparation now.

**A14 (`deploy`)**

```go
// PrepareContent is _prepare_transactional_content.
type PrepareOptions struct {
    BaseURL        string
    DeploymentID   string
    PreferCached   bool       // content checks: prefer cached Sub-Store answers
    StoreDownloads bool       // publication / explicit final generation
    Progress       Progress
}
type Prepared struct {
    Bundle              *Bundle
    ConfigContent       string
    Providers           []*provider.Rendered       // for provider.CommitProviderBundle
    SubscriptionHosts, SubscriptionDialers json.RawMessage  // report fields, passed to responses as is
    ProviderDownloads   []ProviderFile
    RulesetDownloads    []convert.Download
    CustomFiles         []CustomFile
}
func PrepareContent(ctx context.Context, m *agents.Manager, t Target, src *Source, o PrepareOptions) (*Prepared, error)
func PrepareContentBundle(ctx context.Context, m *agents.Manager, t Target, src *Source, baseURL, deploymentID string) (*Bundle, error)

// Publish is publish_deployment: the pair publisher (A18) when registered and the Agent is in a pair,
// else PublishDirect.
type PublishOptions struct {
    Activate       bool
    ConfigRevision *Revision
    BaseURL        string
    ResourceConfig *Source     // geo sources; nil → SourceSnapshot → current compat config of the profile
    SourceSnapshot *Source     // nil for callers without a snapshot (Python None)
}
func Publish(ctx context.Context, m *agents.Manager, agentID string, b *Bundle, o PublishOptions) (agents.Result, error)
func PublishDirect(ctx context.Context, m *agents.Manager, agentID string, b *Bundle, o PublishOptions) (agents.Result, error)

// Push is manager.push_config_to_agent for every service: guard, then Publish (bundle) or the legacy Surge call.
type PushInput struct {
    ConfigContent      string
    Bundle             *Bundle          // nil only for Surge/legacy
    Publish            PublishOptions
    RestartAfterUpdate bool             // Surge self-restart flag (extra_data)
    ExpectedProfileID, ExpectedServiceType *string
}
func Push(ctx context.Context, m *agents.Manager, agentID string, in PushInput) (agents.Result, error)

func GenerateAgentConfig(ctx context.Context, src *Source, t Target, baseURL string) (AgentConfig, error) // config_generator
func SettingsDigest(oxidns json.RawMessage) string
func RulesFingerprint() string
func ValidateBundle(agentID string, b *Bundle) (ValidationReceipt, error)
func ReceiptMatches(agent *agents.Record, state *agents.Record) bool        // registered as agents.Hooks.ReceiptMatches
func PublicDeployment(record *agents.Record) *agents.Record                 // _public_deployment (R17, routes)

// Receipts is rule_sync_store (one per data dir; L4 mutex per Agent directory).
type Receipts struct{ /* data dir, per-Agent sync.Mutex map (L4) */ }
func (r *Receipts) Capture(ctx context.Context, agentID string, b *Bundle, src *Source, baseURL string, comparison *File) error
func (r *Receipts) Load(ctx context.Context, agent *agents.Record, withBundle bool) (*Receipt, error) // (nil, nil) = none
func (r *Receipts) Stamp(agent *agents.Record) (Stamp, bool)
func (r *Receipts) AdvanceContent(ctx context.Context, m *agents.Manager, agent *agents.Record, src *Source, b *Bundle) (bool, error)
```

```go
// Route owners: the whole push-config / generate-config orchestration (guards, reservation, preparation, cache
// commit, publish, response mapping of 2.5) lives here; api handlers only bind the request and write Response.
type PushRequest struct {
    Body                   []byte   // raw request body (Python: get_json(silent=True) or {})
    Scheme, Host           string   // request.scheme / request.host
    ForwardedProto, ForwardedHost string // X-Forwarded-Proto / X-Forwarded-Host as received (X06; O9 decides trust)
}
type GenerateRequest struct{ Body []byte; HostURL string } // request.host_url
type Response struct{ Status int; Body any; NoStore bool }
func PushConfig(ctx context.Context, m *agents.Manager, agentID string, r PushRequest) Response
func GenerateFinal(ctx context.Context, m *agents.Manager, agentID string, r GenerateRequest) Response
```

**Detaching (O3 2.5, owner detaches).** Handlers pass `c.Context()`. The long writers — `PushConfig`,
`GenerateFinal`, `Push`, `Publish`, `PublishDirect`, `PrepareContent` (it saves subscription caches with
`StoreDownloads`) — run their body through one helper, `runDetached(ctx, name, fn)`: `lifecycle.Spawn(ctx, name, …)`
and wait for the result. Spawn drops the request's cancellation, tracks the job for the shutdown drain, and cancels
its ctx only at the drain deadline, which is what 3.4 relies on. The helper is re-entrant: when ctx already carries
deploy's job marker (or a lifecycle loop/job ctx of A16–A18/A22) it runs fn inline. `lifecycle.ErrStopping` from Spawn
(after the drain deadline) is returned as is; nothing is recorded. (O3-2 is merged: use `lifecycle.Spawn`
directly; no transitional plain `go`.) Short writers (`Receipts.*`,
`Manager.RememberPreparedContent`, A01 GET/activate/rollback/recover) use `ctx = context.WithoutCancel(ctx)`; each
Agent call there has its own timeout. Reads honour the request ctx.

### 2.5 Errors and their HTTP mapping

```go
type PreparationError struct {          // DeploymentPreparationError (a ValueError)
    Message string                       // final Chinese/English text exactly as Python builds it
    Issues  []oxidns.Issue               // set by _generated_config_failure for OxiDNS validation
    Remedy  *Remedy                      // RuleDownloadError: {"kind":"rule_download","github":bool}
}
type PreviewChanged struct{ Message string }     // 409 code preview_changed
var ErrPairPublisherMissing = errors.New("deploy: DNS HA pair publication is not available (A18)")
var ErrGatewayGuardMissing  = errors.New("deploy: Linux gateway guards are not available (A20)")
```

The push route's Python `except (StrategyReferenceError, DeploymentPreparationError, ValueError)` → 400 is one Go
function, `deploy.IsPreparationFailure(err) bool`, listing **every** Go error type whose Python class is a
`ValueError` subclass reachable from preparation, each with a table test. Starting list (Go names on `go` today):
`*deploy.PreparationError`; `*rules.StrategyReferenceError` and `convert.DialerReferenceError` (StrategyReferenceError
and its subclass); `convert.BaseSettingsError`, `convert.EasyTierSettingsError`; `gwbase.LinuxGatewayError`
(= `gateway.Error`) and `routersplit.RouterSplitError`; `oxidns.SettingsError`, `oxidns.ValidationError`,
`oxidns.ReferenceError`; `rulefetch.RemoteRuleValidationError`; `*repo.Error` of the kinds that are ValueErrors in
Python (ProfileValidationError, ProfileExists, ProfileInUse — **not** NotFound, a KeyError → 500); `agents.ValueError`;
`provider.Error` and the emulated `convert.PyError`/`routersplit.PyError` whose class is `ValueError` or a ValueError
subclass (until N9 removes them); MosDNS archive errors. **Not** in the list: `convert.CustomConfigYAMLError` (Python
`Exception`), `SubscriptionFetchError`, `ProfileNotFound`, transport errors → 500. The porter derives the final list
from the Python class tree (`grep -rn "^class .*(.*Error)" backend`) and the call graph of
`_prepare_transactional_content`; the reviewer re-derives it. Everything else → 500 `Deployment preparation failed`
with `error_details` = `failure_text("<GoType>: <text>")` (texts of non-Chinese internal errors are not chased).

| Route | Error | Status | Body |
|---|---|---|---|
| push-config | Agent missing | 404 | `找不到这台 Agent，请刷新设备列表。` |
| | body not object | 400 | `发布请求格式无效，请重新提交。` |
| | PreviewChanged (also generation/deployment mismatch) | 409 | `{success:false,status:"failed",code:"preview_changed",message}`; recorded only when this request owns the reservation |
| | guard incomplete | 400 | `保存后的发布信息不完整，请重新保存草稿后发布。` |
| | legacy revision invalid/old | 409 | `配置已改变，请重新预览后发布。` |
| | profile unavailable | 409 | `Agent 绑定的配置空间不可用。原始错误：<e>` |
| | guard mismatch | 409 | `草稿已保存，但配置或设备绑定已改变，请重新载入并保存后发布。` |
| | OxiDNS draft revision | 409 | `{code:"draft_changed", message:"配置或设备绑定已改变，请重新载入后发布。"}` |
| | unsupported service | 400 | `当前服务类型不支持配置发布。` |
| | restart not bool | 400 | `发布请求中的重启选项无效，请重新提交。` |
| | reservation refused | its `http_status` (409/404) | public reservation result |
| | binding changed at reservation | 409 | recorded failed `草稿已保存，但设备绑定已改变，请重新选择设备后发布。` |
| | IsPreparationFailure | 400 | `{success:false,status:"failed",failed_stage:"preparing",message: first line ≤300 runes of details, error_details, issues?, events, deployment_id}` |
| | other | 500 | same shape, message `Deployment preparation failed` |
| generate-config | Agent missing 404; guard 409 `目标设备或绑定已改变，请刷新后重新生成。`; PreviewChanged 409 `code:"preview_changed"`; **any** other error 400 `failure_text(str(exc))` (Python catches `Exception`) |
| download / file | PreviewChanged 409; file missing (incl. no `path`) 404 `生成结果中没有这个文件。` |
| GET deployment | status from result; **200 for any of succeeded/failed/rolled_back/rollback_failed** |
| activate/rollback/recover | `_deployment_response`: `http_status` if set; else not success → 409 for rolled_back/rollback_failed, 500 otherwise; success with a non-terminal status → 202; else 200 |
| GET /config | token missing/long → 401 `Token required`; wrong → 401 `Invalid token`; strategy reference 400 `str(e)`; profile 409 `Agent profile unavailable: <e>`; other 500 `str(e)` |

Stored task messages stay the **English protocol texts** Python stores (`Upload outcome unknown; query deployment
status`, …); `agents.PublicDeploymentMessages` localises them on the way out (A02, verified). They are `const`s in
`deploy/messages.go` next to the Chinese ones; tests reference the constants.

### 2.6 Hooks

Registered by deploy (`deploy.Wire`, called from `server.wirePackages`):
- `agents.Hooks.ReceiptMatches = deploy.ReceiptMatches`.
- `provider.SetLocations(deploy.Locations{})` (C30 then removes `deferA11Locations`, see PROGRESS A11).
- `substore.SetContentSyncInvalidator(checker.Invalidate)`.

Provided by deploy for later modules (setters return a restore func; defaults are Python's behaviour when the
module is not involved, else fail closed):

| Setter | Python | Registered by | Default |
|---|---|---|---|
| `SetPairPublisher(func(ctx, m, agentID, b, o) (agents.Result, bool /*handled*/, error))` | `oxidns_ha_publish.publish` | A18 | not handled, unless the Agent record has `oxidns_ha.enabled == true` or an active HA publication → `ErrPairPublisherMissing` (500) |
| `SetGatewayGuards(GatewayGuards{RequireForwardingKept, RequireGatewayOffKept func(ctx, m, Target) error})` | `linux_gateway.require_forwarding_kept/require_gateway_off_kept` (device reads) | A20 | `ErrGatewayGuardMissing` — only reached for full-TUN or legacy-gateway profiles |
| `SetIPv6Enroll(func(ctx, m, agentID, deploymentID string, src *Source) error)` | `ipv6_verification.enroll` | A23 | no-op |

The pure version checks `supports_gateway`, `supports_forwarding`, `require_gateway_agent`,
`require_gateway_bundle_agent` and their constants/messages (`linux_gateway.py` 14–20, 54–75, 200–218) are
ported in A14-1 into `internal/gateway` (`agentversion.go`), where A20 adopts them — no second copy.

A01 changes made in step A14-2 (verified code, so the step's review covers them): delete `Hooks.PublishDeployment`,
`Manager.PublishDeployment`, `PushConfig.Bundle`; `Manager.PushConfigToAgent` keeps only the legacy
`/api/config/update` path (rename `PushLegacyConfig`) and is called by `deploy.Push`; add
`Manager.RememberPreparedContent(ctx, agentID, deploymentID string, prepared, receipt json.RawMessage) error`
(the `_remember_deployment_content` transaction: ownership check `Deployment ownership changed during
preparation` → ValueError class). `BeginDeployment` / `FinishDeploymentPreparation` stay the reservation API
for every publisher (route, A16, A17, A18, A22 call them as Python does).

### 2.7 Agent protocol (panel → Agent), exactly as Python sends it

All calls: `http://<host>:<port>/api/<suffix>`, `Authorization: Bearer <token>` (Latin-1 rules of A15 F2), through
`agents.Manager.DeploymentRequest` / `Call` (A01, verified: environment proxies as requests 2.31, answers parsed and
sanitised there). `Timeout` = requests' (connect, read) pair.

| Call | Request | Timeout | Retry | Panel reading |
|---|---|---|---|---|
| capabilities | `GET capabilities` | (5, 5) | none | object; refuse unless `1 ∈ deployment_protocols` and `service_type` equals the Agent record's; `reload_only` needs `1 ∈ reload_only_protocols`; `final_config` needs `1 ∈ final_config_protocols` and `config_path` (default `config.yaml`) equal to the bundle's; a transport error before `submitted` → failed `Unable to query Agent deployment capabilities` |
| final preview | `POST config/final-preview` JSON `{"content": text}` | (30, 30) | none | `success is true` and `content` string, else `message` or `设备无法生成最终配置，请检查 Agent 状态。` |
| upload | `POST deployments`, body = archive, `Content-Type: application/gzip`, `X-Deployment-ID`, `X-Content-SHA256` (lowercase), `X-Activate: true|false` | (10, 120) | 21 attempts within 2 s, 0.1 s apart, **only** on 409 with message exactly `another service operation is in progress` | `http_status` set → default `status:"failed"`; no string `status` → `{success:true,status:"unknown",message:"Upload accepted; awaiting valid Agent status"}`; recorded with `UploadReceipt` |
| status | `GET deployments/<id>` | (5, 30) | none | 404 → see 3.1 row 6; other HTTP error or non-string status → pending (not stored); else `RecordDeployment(SkipUnchanged)` |
| activate / rollback / recover | `POST deployments/<id>/<action>` (no body) | (5, 30) | busy rule as upload | A01 logic (verified) |
| legacy (Surge, non-transactional) | `POST config/update` UTF-8 JSON `{config, md5, restart_after_update?}` | 10 | none | A01 `PushLegacyConfig` |

The panel's own deployment ID pattern stays `[A-Za-z0-9_-]{1,80}` (stricter than the Agent's); the default ID is
`uuid4().hex` (32 lowercase hex), HA children add their suffixes (A18).

### 2.8 The deployment record (owned by A01; what A14 writes)

Persisted in the system document, Agent record (≤ 30 tasks in `deployments`, insertion order, oldest dropped; failed
tasks kept): `deployments{<id>: task}`, `latest_deployment` (copy of the latest task), and on confirmed success of
the latest task `config_version`, `config_revision` (when the task has one), `pushed_at` (UTC `…Z`),
`applied_content` (= `prepared_content` + `config_version` + `deployment_id`; removed when the task had none or after
a confirmed manual rollback). Task members: `deployment_id, status, success, profile_id, service_type, created_at,
updated_at, agent_updated_at, events[], message, error, error_details, failed_stage, rollback_error,
rollback_error_details, http_status, submitted, sha256, source_sha256, config_version, config_revision,
previous_running, health, manual_rollback, rollback_rejected, rollback_message, prepared_content,
oxidns_validation_receipt`, the DNS-report fields, and `ha_publication` (A18). Public output strips
`prepared_content` and `oxidns_validation_receipt` (`PublicDeployment`) and the Agent's `applied_content`/`token`
(`PublicAgent`).

Revisions: `config_revision` is the profile `_revision` of the publish snapshot (`repo.GetPublishSnapshot`,
or `ConfigRevision` for unguarded pushes; `null` when unavailable) — diagnostic, used by the guarded-publish
check (`expected_config_revision`) and by previews (`config_revision` + `input_digest`). The store generation is
not used by this chain. Files: receipts (2.4 `Receipts`), previews (`Previews`); Go creates no
`.agent-deployment-locks` files (O3-6; Python's leftovers are ignored).

### 2.9 Consumers

| Consumer | Uses |
|---|---|
| A16 rule sync | `Build` (PinnedFiles, LocalDNSWatchdog), `Retarget`, `Pack`, `RelativePath`, `LocalRule`, `Fingerprint`, `Digests`, `Bundle.Contents`, `Receipts.{Load,Stamp,Capture,AdvanceContent}`, `Publish`, limits |
| A17 DNS follow | `PrepareContent`, `Publish`, `Fingerprint`, `Digests`, `RenderConfig`, `SettingsDigest` |
| A18 DNS HA | `SetPairPublisher`, `PublishDirect`, `Pack`, `Bundle.Contents`, `Fingerprint`, `PrepareContent`, A01 `GetDeploymentDirect`/`ActivateDeploymentDirect` |
| A20 gateway | `SetGatewayGuards`; adopts `gateway/agentversion.go` |
| A22 gateway install | `PrepareContent`, `provider.CommitProviderBundle`, `Push` |
| A23 IPv6 | `SetIPv6Enroll`, `Receipts.Load`, `contentsync.Checker.CompareNow` with `PrepareContentBundle` |
| C34 generate | `Pack`, `Previews.Save/Load`, `Preview.Public/Zip/File` (its `/api/generate/artifacts/*` routes) |
| C17 provider, C13 substore | `Locations` hook, content-sync invalidation |
| R17 client devices | `PublicDeployment` |

## 3. State and concurrency

### 3.1 Panel-side task transitions

| # | From | Event (owner) | To | Notes |
|---|---|---|---|---|
| 1 | – | `BeginDeployment` (route, A16/A17/A18/A22) | `preparing` | inside the system transaction; refused 409 by an unfinished task, upgrade busy, DNS follow/HA conflict; L1 key (agent, deployment) taken |
| 2 | `preparing` | progress (`Progress` callbacks) | `preparing` | message only; ≤ 1 write/s, phase boundaries and the first/last rule forced |
| 3 | `preparing` | preparation error, ownership change, capability refusal | `failed` | `failed_stage:"preparing"` except capability refusals (409, no stage); Agent untouched |
| 4 | `preparing` | before upload (`PublishDirect`) | `uploading`, `submitted:true`, `sha256`, `source_sha256`, `config_version`, `config_revision` | **persisted before the POST** |
| 5 | `uploading` | upload answer | Agent status (`verifying`…`ready`) / `failed` / `unknown` | `UploadReceipt`: a late 202 never overwrites an observed later stage |
| 6 | non-finished, submitted | GET poll (route, A16–A18, A23) | Agent status / `failed` (404 and no upload in flight: `Agent has no record of the uploaded deployment`) | 404 while the key is held → pending, not stored |
| 7 | `preparing`, not submitted | GET poll and key not held | `failed` `Preparation interrupted before upload; current service unchanged` | `OnlyUnsubmitted` |
| 8 | `ready` | activate | Agent status / `unknown` (transport) ; OxiDNS without matching receipt → `failed` `failed_stage:"verifying"` 409 | A01 |
| 9 | `succeeded` (latest) | rollback reserve | `rolling_back` `manual_rollback:true` → Agent / `unknown` / back to `succeeded` + `rollback_rejected` | A01 |
| 10 | `rollback_failed` | recover reserve | `rolling_back` → Agent / `unknown` / `rollback_failed` (restored) | A01 |

Status sets (A01 constants, reused): finished = {succeeded, failed, rolled_back} (a new publication may start);
logged results = finished + rollback_failed; HTTP-terminal (route 200 / no 202) = logged results. `succeeded`,
`failed`, `rolled_back` never change status again; `manual_rollback` cannot be cleared by a poll; Agent
`updated_at` older than the stored `agent_updated_at` is ignored.

### 3.2 Locks

| Lock | Level | Held | Busy → |
|---|---|---|---|
| preparation key `(agentID, deploymentID)` | L1 `lockx.Keyed`, try (O3-6) | from `BeginDeployment` until the publishing request ends (route `finally` and `PublishDirect` defer, as Python's two `finish_deployment_preparation` calls; release is idempotent) | 409 `Deployment preparation already running` |
| progress mutex (per publication) | plain `sync.Mutex`, documented "held across one `RecordDeployment`; taken only by progress callbacks, which hold no lock" | one progress write | waits (keeps Python's message order) |
| receipts directory per Agent | L4: a `sync.Mutex` per Agent directory in `Receipts` (lockx levels stop at L3, O3-1 clarification) | file writes and pruning only; the retention set (applied + unfinished deployment IDs) is read from the repository **before** taking it, and `AdvanceContent`'s repository transaction runs **after** releasing it (never L3 inside L4) | waits |
| previews store | L4 mutex (O3 table) | save + eviction | waits |
| MosDNS conversion cache, raw-download cache of one build, content-sync maps | L5 | memory only, never across I/O | – |

**No per-Agent publish lock is added.** Python had none: concurrency is decided by the record (row 1: one unfinished
task per Agent, enforced inside the repository transaction, L3) and by the Agent's own service-operation lock (busy
409, retried 2 s). A double-clicked activate therefore answers like Python (first 202; second retried, then the
Agent's 409 passed through). The L1 key only guards the preparation of one task and answers "is it still being
prepared" for polls (A01's `preparationRunning`, which O3-6 turns into `Keyed.Held` instead of the flock probe —
O3 3.5: process death = not running).

Order of checks before the L1 key in push-config (O3 2.5 lock-before-validation): Agent lookup 404 → body 400 →
generation load 409 → guard validation 400/409 → snapshot 409 → guard compare 409 → OxiDNS draft 409 → Surge branch
→ service 400 → restart 400 → deployment ID → `BeginDeployment` (invalid ID → 400 through the preparation-failure
branch, with `events: []` and the raw `deployment_id` echoed, as Python). Validation first, then the key.

Interaction with other publishers: rule sync (A16), DNS follow (A17), HA (A18), gateway install (A22) all go through
`BeginDeployment` and `Publish`, so they meet the same row-1 refusal; A16's per-Agent worker key and A17/A18's L1
locks are taken *before* they call into deploy (L1 → L1 tries never wait). `BeginDeployment` refuses while an
upgrade is unfinished, a DNS follow job of this Agent or its source is `preparing|publishing`, or an HA pair job
conflicts (`compat.go` default until A18). Rollback refuses for HA members and during DNS follow.

### 3.3 Cancellation (PORTING 4b)

- push-config and generate-config: `deploy.PushConfig` / `GenerateFinal` run as jobs `agent-publish-<id[:12]>` /
  `agent-generate-<id[:12]>` (2.4 "Detaching"); the handler waits for the `Response`. A disconnected browser does not
  stop the job (Python: waitress kept running the handler); the job finishes, records, and releases its key.
- GET deployment, activate, rollback, recover: `context.WithoutCancel` at the deploy/A01 entry; each Agent call has
  its own timeout (2.7). push-diff, entries, list/detail: request ctx for reads; the comparison runs in content-sync
  jobs.
- Rule downloads and provider preparation use the job ctx (cancelled only at the drain deadline).

### 3.4 Shutdown mid-publish (O3 3.4: drain, cancel at grace − 2 s; maintainer kept 10 s)

When the job ctx is cancelled at the drain deadline, deploy **records nothing further**: every error path first
checks `ctx.Err() != nil` and returns `context.Canceled` without `RecordDeployment` (Python was killed at that point
and wrote nothing; recording "failed: Unable to query Agent deployment capabilities" or a download error would be
wrong). The key is released by the process exit.

| Stage when cancelled | Stored record | After restart |
|---|---|---|
| preparing (downloads, generation, Geo, validation) | `preparing`, not submitted | first poll (Agent page tracks `latest_deployment`) → row 7 `Preparation interrupted before upload; current service unchanged`; subscription caches already saved by `StoreDownloads` stay (Python same) |
| capture written, `uploading` persisted, POST in flight | `uploading`, submitted | poll → Agent 404 → `failed` `Agent has no record…` (Agent got nothing or discarded an incomplete body) or the Agent's state (it received everything and continues on its own) |
| upload answered, Agent worker running | Agent stage | poll continues; the Agent's transaction is independent of the panel |
| activate/rollback/recover POST in flight | `ready` / `rolling_back`+`manual_rollback` / `rolling_back` | poll; a lost manual-rollback answer is re-issued only from `unknown` (A01 rule) |
| generate-config | nothing recorded | preview missing → user regenerates |

### 3.5 Crash recovery

No new recovery action (O3 3.5 rule). The rows above apply unchanged to `kill -9`. Panel "verifying/activating"
states are only copies of Agent states; the Agent recovers its own journal (`RecoverDeployments` at Agent start,
`-recover-and-exit`, `POST …/recover`) and the next poll copies the result. A crash between persisting `uploading`
and sending the request is indistinguishable from a lost request and resolves through row 6. Partially written
receipt/preview files are temp files (`.write-*`, renamed atomically): a missing `record.json` makes a preview
`PreviewChanged`, eviction removes the folder; a receipt without its archive makes `Load` return none (Python same).

### 3.6 Status polling — kept as in Python (decision D-A14-1)

The panel task advances only when something asks the Agent (route GET, activate, the owning loops of A16/A17/A18,
A23's verification). Kept because: the Agent page re-tracks every non-terminal `latest_deployment` on load and polls
every 2 s (`useAgentDeployments`, `Agents.vue`), and a 409 "unfinished deployment" answer carries the
`deployment_id`, which the frontend then tracks — so a closed browser self-heals on the next visit; a server-side
poller would be a new recovery action and another concurrent writer of the same record. Cost: a task can sit at
`verifying` in the stored document while nobody looks; no device effect.

### 3.7 Content sync and previews in the background

Content-sync jobs: two slots, results cached 900 s (errors 30 s), entries ≤ 512 LRU (running entries never
dropped), change lists ≤ 8 and ≤ 48 MiB compressed, version token 16 hex per stored list; `Invalidate` drops results
and lists (running jobs keep the slot but their answer lands nowhere). Times from the lifecycle Clock. Previews: TTL,
count and byte bounds checked at every save under the L4 mutex (oldest mtime first, expired first).

## 4. Failure and rollback

| Failure point | Recorded task | HTTP (push) | What the user sees (public message) | Guaranteed on the Agent |
|---|---|---|---|---|
| guard / validation before reservation | none | 400/409 (2.5) | the Chinese guard message | nothing sent |
| reservation refused | none (existing task untouched) | 409 / 404 | e.g. `这台 Agent 还有未结束的发布，请先查看并处理上次发布。` + `原始错误：…` | nothing sent |
| generation / provider / rule download / MosDNS conversion / OxiDNS validation / size limits | `failed`, `failed_stage: preparing`, `error_details` | 400 | first line of the Chinese preparation message, e.g. `无法下载「X」规则集：…本次发布已停止，设备仍使用原配置。`, `remedy` for rule downloads | nothing sent (GET capabilities at most) |
| unexpected error | `failed`, `Deployment preparation failed` | 500 | `准备发布文件失败，请查看 ConfigFlow 日志。` | nothing sent |
| capabilities unreachable | `failed` | 500 | `无法确认 Agent 是否支持发布，请检查设备连接。` | nothing sent |
| old Agent / no reload-only / config path changed for final config | `failed` 409 | 409 | `当前 Agent 不支持完整的校验和回滚发布流程，请先更新 Agent。` / `请升级 Agent 到 1.8.30-go …` / `目标设备配置已改变，请重新生成最终配置后推送。` | nothing sent |
| Geo download (Mihomo), final-archive OxiDNS validation (`ValidateBundle`) — raised inside `PublishDirect` | `failed` preparing | **500** (Python: `_publish_deployment` records and returns the task without `http_status`; `_deployment_response` maps a failed task to 500) | `<label> 下载失败。本次发布已停止，设备仍使用原配置。…` | nothing sent |
| ownership/profile/service changed during preparation (inside `PublishDirect`) | `failed` preparing | 500 (same reason) | `准备文件期间…已改变，请重新发布。` | nothing sent |
| upload transport error / timeout | `unknown` (submitted) | 202 | `暂时无法确认上传结果，请查询发布状态，不要重复提交。` | either nothing, a failed incomplete receive, or the full candidate continuing — the next poll tells which |
| Agent refuses upload (4xx/5xx JSON) | `failed` with Agent message | Agent's status | localised Agent message + `原始错误：…` | nothing applied (Agent journal `failed` or none) |
| Agent verify/stage failure (extract, checksum, DNS validation) | `failed` (from Agent) | polled 200 | e.g. `发布文件校验失败，请重新发布。` | live files untouched (nothing changes before a durable backup) |
| activation failure, Agent rolled back | `rolled_back` | 200 / 409 on action | `配置发布未生效，已回滚到原配置` (log) + Agent reason | previous files restored, service running; `config_version` unchanged |
| rollback failure | `rollback_failed` | 200 / 409 | error + `rollback_error`; new pushes refused until recovered | Agent blocks new deployments (`requires rollback recovery`); `POST recover` or `-recover-and-exit` restores backups |
| recover on Agent < 1.8.57-go | `rollback_failed` (restored) | 404/405 | manual steps message (`此 Agent 版本不支持在 ConfigFlow 中重新恢复…`) | unchanged |
| manual rollback rejected (400/404/405/409 except busy) | `succeeded`, `rollback_rejected`, `rollback_message` | Agent's status | `回滚请求被拒绝，回滚未执行，原配置继续运行` (event) | unchanged |
| OxiDNS candidate without matching receipt | `failed` `failed_stage: verifying` 409 | 409 | `候选配置缺少当前规则的校验证明，已停止激活。请重新推送配置并完成校验。` | candidate staged, never activated; running service untouched |

Rollback on the panel side never touches the device by itself: the only rollbacks are the Agent's automatic
transaction rollback and the user's manual rollback/recover (A01). Provider caches are committed
(`CommitProviderBundle`) only after the whole bundle is ready; subscription downloads saved by `StoreDownloads`
before that are kept even when a later step fails (Python same: they are valid caches).

## 5. Buildable steps

Every step: own branch off `go`; compiles; `gofmt -l` empty; `go vet ./...`; `go test -count=1 ./...` and `-race`
on touched packages; `server/scripts/proddiff.sh` on .32 with no new difference; PROGRESS section updated with
commits, decisions, the Python-test coverage map, and — for HR steps — **one line per issue class** of
`review-r04.md`, `review-r05.md` (and `review-a19.md` when merged) saying how the step avoids it. Sizes: S ≤ 1 day,
M 1–2 days, L 3–4 days of one Codex agent.

| Step | Content | Size | HR | After |
|---|---|---|---|---|
| A11-1 | `deploy` core: `Role/File/Manifest/Content/Bundle`, `Pack/Open`, archive write/read, `RelativePath`, `Fingerprint`, `Digests`, `ComparisonManifest`, `Retarget`, limits; `adapter.go` skeleton (`Source`, `Target`); `agentfake` skeleton (capabilities + upload receiver that extracts and validates exactly like `go-agent extractDeployment`). Tests: Python corpus (`testdata/gen_a11_core.py`: fingerprints/digests/relative paths incl. CJK, emoji, `./`, `..`, control chars, size/role/digest forms, duplicates, 4097 files); Python-written archives read back; Go archives written to `testdata/archives/` for the go-agent conformance test (6.3). | M | yes | – |
| A11-2 | `Locations` (`SourceError`, `PreparationContext`, `source_excerpt` secret hiding) + `provider.SetLocations`; `ConvertMosDNSRules`; `MaterializeRule` (internal callbacks incl. rule-proxy, gateway providers via `gateway.Config.ProviderYAML/SourceYAML` with a snapshot loader, local library, remote sources with `refresh.ReadRuleSource` last-good, recursion limit 3) and `_fetch_bytes` with `RuleDownloadError` + remedy. Corpus vs Python with a loopback rule server (texts exact). Rerun C30 difftest, drop `deferA11Locations`. | M | yes | A11-1 |
| A11-3 | `Build` (plugin file-type map, attachments, pinned files, gateway routing-mark check, provider path rewrite, OxiDNS resource validation + unavailable-ruleset hints, MosDNS reference rewrite, `RenderConfig` + DNS watchdog, 8-worker download fan-out with shared raw cache and single flight per URL, progress), `PrepareMihomoResources`, `RequiredGeoResources`. Corpus: Python builds bundles for synthetic Mihomo/MosDNS/OxiDNS profiles (gateway on/off, inline providers, classical sources, pinned, MRS, failures) → compare manifests (paths, roles, names, sizes, digests of verbatim files), YAML structurally, error texts exactly. | L | yes | A11-2 |
| A11-4 | `Receipts` (capture/load/stamp/advance, pruning rules, L4 lock), `Previews` (+ `FinalizeBundle`, identities, input digest, eviction, ZIP), `ValidateBundle`/`ReceiptMatches`/`RulesFingerprint`, `PublicDeployment`, C34 helpers. Python-written receipt and preview folders load. | M | yes | A11-3 |
| A12-1 | `deploy/pushdiff` (all of push_diff.py: secret marking, YAML key-by-key diff, unified raw diff with markers, node/rule lists, budgets, `FileEntries` compressed paging, search). Corpus: Python `build_push_diff` over bundle pairs from the A11-3 corpus + targeted inputs (secrets in keys/URLs/userinfo/opaque parts, CJK, huge lists, reformatted YAML); JSON compared after decoding. | L | no | A11-4 |
| A13-1 | `deploy/contentsync` Checker (Spawn jobs, slots, TTLs, details, invalidation), wired to substore; fake-clock tests for every cache rule of content_sync.py. | M | no | A12-1 |
| A13-2 | Routes `GET /api/agents`, `GET /api/agents/<id>` (GET branches into the existing handlers), `push-diff`, `push-diff/entries`; `PublicAgent`; deferred A01/A03/A15 list/detail cases; difftest rows R001/R004/R064/R065 (with a temporary `PrepareFunc` from A14-1 when it exists, else a test stub). | M | no | A13-1 |
| A14-1 | `PrepareContent` (all branches of `_prepare_transactional_content`: router-split/gateway version guards, full-TUN checks, gateway-off guard via `SetGatewayGuards`, provider bundle with node-conversion reuse, store-downloads re-render, `_generated_config_failure`), `PrepareContentBundle`, `IsPreparationFailure`, `gateway/agentversion.go`, `GenerateAgentConfig` + route `GET /<id>/config`. | M | yes | A11-4 |
| A14-2 | `PublishDirect`, `Publish` (+ `SetPairPublisher` default), `Push` (+ Surge), `RememberPreparedContent`, `SetIPv6Enroll`, OxiDNS checkpoint hook use; A01 hook removal (2.6); `agentfake` complete (6.1); transcript differential vs Python (6.2) for every row of 3.1 and 4. | L | yes | A14-1 |
| A14-3 | `PushConfig`/`GenerateFinal` owners (`runDetached`) and thin handlers for push-config, GET deployment, activate, rollback, recover, generate-config, download, file; progress throttling with the Clock; deferred route cases of A01/A02/A15; route-level proxy case (A01 decision 5). | M | yes | A14-2, A13-2 |
| A14-4 | Fault scenarios in the O3-8 harness (6.4) and the go-agent conformance run (6.3); PROGRESS coverage map final. | S | yes | A14-3, O3-8 |

Then: fresh-Opus HR review of A11 + A14 (one review, both modules), Codex reviews of A12/A13, black-box Step B rows,
lab batch L2.

## 6. Acceptance

### 6.1 Fake Agent (`internal/agents/agentfake`)

A stateful loopback HTTP server, one instance per server under test (difftest X13), implementing every endpoint of
2.7 with the Agent's real validation (archive rules of 0.2, digest, ID pattern, manifest/target match, state
machine, replay rules, busy lock) and a journal per deployment. It **records every call** (method, path, headers
`Authorization`/`X-*`/`Content-Type`, body digest, decoded manifest and file digests for uploads, JSON bodies,
arrival time, request vs loop origin) and is driven by a script of faults:

- per endpoint: delay, HTTP status + body (JSON object, list, string, `null`, non-JSON, empty), connection reset
  **before** reading the body, close **after** the journal write without answering (lost response), timeout longer
  than the panel's read timeout, `busy` N times then accept;
- upload: digest mismatch, incomplete body, Agent-side extract/validate failure, stage-only (`X-Activate:false`
  → `ready`), activation outcomes `succeeded` / `failed` / `rolled_back` / `rollback_failed` / `recovery_pending`,
  per-stage progression (`receiving → verifying → … → checking → succeeded`) advanced one step per GET;
- capabilities variants: missing protocols, other `service_type`, other `config_path`, no `final_config_protocols`,
  no `reload_only_protocols`, `mihomo_resources` lists, Agent < 1.8.57-go (no recover);
- final-preview rewriting the content (port change) or refusing;
- journal loss (GET 404 after acceptance), stale `updated_at`, wrong `deployment_id` in answers, DNS check reports
  (bounded, with tokens to redact).

### 6.2 Python reference (transcript differential)

`internal/deploy/testdata/gen_a14_publish.py` runs the real Python manager/route functions on the test server
against its own instance of the same fake — `agentfake` also builds as a small test command
(`internal/agents/agentfake/cmd/agentfake -listen 127.0.0.1:0 -script s.json -transcript t.jsonl`), started once per
side with the same script —
for every row of 3.1 and every failure row of 4, plus T06–T09 of `difftest-agents.md`. Compared per scenario:
request transcript (sequence of calls, headers, manifest content and file digests — archive bytes and their sha are
replaced by per-side tokens in order of appearance), the Agent record after the scenario (`deployments`,
`latest_deployment`, `applied_content`, `config_version/revision`, `pushed_at` presence; timestamps, archive digests
and event times normalised), receipts directory listing and `receipt.json` structure, HTTP status and body of the
route. Zero differences except the 7.1 classes.

### 6.3 Real Agent archive conformance

A test in the Agent module, `backend/agents/go-agent/routes/server_archive_conformance_test.go` (test-only file;
skips unless `CONFIGFLOW_SERVER_ARCHIVES` is set), feeds every archive from `server/internal/deploy/testdata/
archives/` (Mihomo/MosDNS/OxiDNS; CJK and emoji names; 4096 files; empty rule file; `final_config`; `reload_only`;
resources) through the Agent's own `extractDeployment` and `DeploymentManifest.UnmarshalJSON`. Run on .32:
`rt -- bash -lc 'cd backend/agents/go-agent && CONFIGFLOW_SERVER_ARCHIVES=$PWD/../../../server/internal/deploy/testdata/archives go test -run ServerArchive ./routes/'`.
This proves today's Agent reads Go archives; L2 adds older Agent versions.

### 6.4 Fault injection (O3-8 harness, .32)

SIGTERM and `kill -9` at each stage of 3.4 (scripted delays in the fake hold the stage), restart, poll: final
records equal the "after restart" column; no stuck L1 key; a second push works. Client disconnect during push and
generate: final state equals an uninterrupted run. Two concurrent pushes to one Agent: one 202/200, one 409 with
Python's message (X12); same `deployment_id` twice: existing task returned.

### 6.5 Black-box differential (Step B, not by the porter)

Rows R010, R013–R016, R023–R025, R063–R065 with their S/F/B cases and W04–W09, T05–T09, T26, X01 (content exemption
of `get_agent_config`/`generate_final_agent_config`), X06 (base URL sources), X12 from `difftest-agents.md`, with the
fake of 6.1 per server.

### 6.6 Python tests → coverage

| Python test file | Covered by |
|---|---|
| `test_agent_content_manifest.py`, `test_agent_content_baseline.py`, `test_deployment_bundle_lazy_archive.py` | A11-1 corpus + `Bundle` laziness tests (pack once, files after pack, equality) |
| `test_agent_deployment_downloads.py`, `test_classical_deployment.py`, `test_deployment_error_locations.py`, `test_agent_content_preparation.py`, `test_mihomo_resources.py`, `test_agent_publish_caches.py` (the libyaml/PyYAML cases are moot under N7; the shared-source-once and cache-touch cases are kept) | A11-2/A11-3 corpora with loopback rule servers |
| `test_generated_preview.py` | A11-4 + A14-3 route tests (6.2 scenarios for previews) |
| `test_agent_push_diff.py` | A12-1 corpus |
| `test_agent_content_sync.py` | A13-1 fake-clock tests (hand-ported: concurrency/caching is not observable by corpus) |
| `test_agent_content_api.py`, `test_agent_push_diff_api.py`, `test_agent_push_diff_paging.py`, `test_agent_first_push_sync.py` | A13-2 route tests + difftest rows |
| `test_oxidns_deployment_receipt.py`, `test_oxidns_rule_source_edit.py`, `test_service_domain_publish.py`, `test_generated_config_error_locations.py` | A14-1/A14-2 scenarios (6.2) |
| `test_save_publish_guard.py`, deferred A01 cases (`test_agent_deployments.py` upload/route cases, failure details, DNS reports, profiles), A02 message routes, `test_profile_generation.py`, `test_agent_log_redaction.py` route halves | A14-3 route tests + 6.2 |
| config-owned deployment-content tests (`test_domain_wildcard_*`, `test_mosdns_rule_fields.py`, `test_mihomo_classical_delivery.py`, `test_ruleset_domain_format.py`, `test_remote_rule_consumers.py`, `test_udp_fallback.py` bundle halves, `test_oxidns_*` bundle/route halves, `test_dns_watchdog_*` bundle halves, `test_proxy_server_dns.py` publish halves, `test_linux_gateway.py` bundle cases) | A11-3 corpus inputs taken from those tests |

### 6.7 L2 lab items (PVE lab, after review; plan for `server/parity/lab/L2.md`)

Only real guests prove: (1) Mihomo, MosDNS and OxiDNS Agents (current release and the oldest release that has
`deployment_protocols:[1]`, plus one < 1.8.30-go and one < 1.8.57-go) load Go-built archives: stage-only → `ready`
→ activate → `succeeded`, the core actually reloads/restarts, DNS answers; (2) forced core check failure →
`rolled_back` with the old service running; (3) forced rollback failure (unwritable backup) → `rollback_failed` →
recover from the UI → `rolled_back`; old Agent shows the manual steps; (4) manual rollback of the latest success,
and rejection when a newer deployment exists; (5) lost responses: drop the panel→Agent link after upload acceptance
(nft on the lab bridge) → `unknown` → reconciles; (6) final-preview on an AIO Agent rewriting DNS ports, then publish
by `generation_id` without re-render; (7) Geo databases: Agent without them gets them in the bundle, Agent with them
reuses; (8) panel SIGTERM and `kill -9` mid-upload and mid-preparation → states of 3.4; (9) Docker Agent publish
(container paths, ownership); (10) Surge legacy path cannot be lab-tested (macOS/iOS only) — fake only, maintainer's
own device at the switch (O6 note).

## 7. Python behaviour intentionally not kept (→ O12 ledger)

### 7.1 One-time effects at the switch

- Archive bytes and sha256 differ from Python's for the same content. A retry with the same `deployment_id` of a
  task started under Python answers 409 `Deployment ID already belongs to different content` (start a new push).
- `rules_sha256` differs: OxiDNS candidates staged (`ready`) under Python are refused at activation with the
  unchecked-candidate message; re-push.
- `settings_digest` differs once (D2): DNS follow shows the generic "unpublished changes" message until the next
  OxiDNS publish.
- Fingerprints change wherever Go-generated file bytes differ (main YAML by N7): every Agent shows 「有变更，待推送」
  once (already accepted: one full publish after the switch).
- Python previews (≤ 1 h) and `.agent-deployment-locks/` files are ignored.

### 7.2 Behaviour

- Generated YAML written by yaml.v3 (`RenderConfig`); duplicate keys in rule payloads/provider YAML are refused (D1)
  where Python took the last value.
- Error texts of non-Chinese internal failures inside `error_details` are Go's.
- Python bug fixed: `receipt_matches` and `rule_sync_store.load` compared the Agent's profile with
  `agent.get('profile_id', 'default')` instead of the resolved default profile; an Agent without `profile_id` bound to
  a default profile not named `default` could never activate an OxiDNS candidate nor use rule-sync receipts. Go uses
  the resolved profile (as `begin_deployment` does). Regression test in A11-4.
- On shutdown, in-flight publishes get up to the drain window instead of being killed at once; records are written
  only by work that completes (3.4).

## 8. Decisions

Taken by the designer (no action needed): new packages `deploy`, `deploy/pushdiff`, `deploy/contentsync`; A01's
`PublishDeployment` hook replaced by `deploy.Publish` + `SetPairPublisher`; no per-Agent publish lock beyond Python's;
records untouched when work is cancelled at shutdown; interim fail-closed defaults on `go` until A18/A20 land (HA
members and full-TUN/legacy-gateway Mihomo profiles cannot be published from Go before then — `go` is not
production); Go-native archive encoding with byte-compatible fingerprints.

For the maintainer:

**D-A14-1 — deployment status advances only when polled.** (A, **recommended**) keep Python's behaviour: the Agent
page and the generate studio poll, a 409 "unfinished deployment" carries the ID the frontend then tracks, the owning
loops poll their own tasks; no extra writer, no new recovery action. (B) add a background reconciler that polls every
non-finished, submitted task every 30 s: stored states stay current without a browser, at the cost of a new loop
writing Agent records concurrently with UI polls and one more Agent request per unfinished task (O12 entry).

**Decided 2026-11-17 by the coordinator (behaviour-preserving default): D-A14-1 → keep Python's poll-driven status advancement; no server-side reconciliation loop.**

**Additions from `a16-a18-sync-follow-ha.md` 1.3 (2026-11-17, additive):** export `RunDetached`, `WriteFileAtomic`,
`Refusal` (counted by `IsPreparationFailure`), `Source` raw accessors and copy-on-write setters, `CanonicalDigest`;
`lifecycle.Tracked`. If the maintainer takes D-A17-1 A, `SettingsDigest` uses `CanonicalDigest` (Python's
`json.dumps(sort_keys=True, ensure_ascii=False)` text) instead of the Go-native encoding of 2.3, and the
"`settings_digest` differs once" bullet of 7.1 no longer applies.
