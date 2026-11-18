# N8-9 discovery

Base: batch-6 `8ec68fd4d`; branch `go-n8-9-discovery`.
Targeted verification only, fixed `RT_HOST=sunbroke@192.168.0.32`.

## Changes

- `repository.go`: typed Repository.Config/Shared/System/View and
  UpdateProfile/UpdateShared replace legacy repository object reads/writes.
  Discovery keeps owned, typed operational projections. Model owns string/int
  coercion, original record provenance and field issues. No local lexer or
  alternative coercion rule is introduced.
- `ports.go`: Parse -> selected physical Nodes -> convert.UsedPorts; removes
  the YAML -> JSON -> jsonx -> YAML detour. Original parser policy/alias limits,
  native-depth syntax fallback, unknown-tag validation and listener exclusion
  are preserved.
- `taskbridge.go`: native tasks.SetRowBuilder, taskstatus.Snapshot/Receipt,
  ProjectHistory and Task. Same next-run calculation, context detach, owner
  repository Scope and background task admission; no lock/rename implementation
  is changed. Existing O3/C37 lifecycle and transaction-guard tests run unchanged.
- Response DTOs for settings, agents, busy and region; history adds its name to
  raw JSON member values, retaining unknown history members and number spelling.
- HTTP requestDTO keeps exact field names and raw field shape for business
  validators. Native BindSilent validates every write before a handler runs;
  case-sensitive keys and existing Chinese field errors remain.
- Core AST test rejects jsonx/pystr imports, dynamic parameter shapes and calls
  back into any codec/helper defined in legacy.go. A wiring test checks all
  typed collaborator fields and the Agent client are installed.

## Temporary adapter inventory

Only `legacy.go` imports jsonx. Core callbacks have typed signatures and no
business implementation is duplicated. The old ProviderRevisions JSON-object
export is a one-call codec of the native result.

| Adapter/callback | Owning removal step |
| --- | --- |
| saveRule, validateContent, cachedContent | N8-9 rules/rulefetch |
| matcherOptions, localMatcher (native lifecycle/cache ownership stays here) | N8-9 rules |
| probes, saveProbes, judge | N8-9 health/check |
| regionData, editRegion | N8-9 health |
| knownService, pageIdentity, websiteMarkers | N8-9 rulefetch |
| publicServices, probeControls, pickTargets, regionRequests, exitInfo, serviceMatrix, classifyRegion, summarizeRegion, serviceDomains | N8-9 health/check |
| managerClient Probe/Targets/Region, supported version comparison | N8-10 agents |
| loadTraffic | N8-10 Agent traffic report core (the native stored-file boundary already exists) |
| ProviderRevisions old export | N8-10 api/agents |

The compatibility callback implementations call actual owner APIs; they do not
copy matcher, probe verdict, page classification or Agent request business code.
Native repository reading is used even inside matcher adapters. All consumers of
legacy rule-library APIs receive the consumed model-decoded string members, not
R2's original numeric token reinterpreted by the old API.

## Explicit behavior changes and coverage

- Stored wrong-typed bools use R5. The real HTTP fixture starts with
  auto_probe: 1; Python reads true, Go false, and the first settings write
  persists those values. The existing 78-request differential explicitly checks
  this one field in settings responses and saved snapshots; all siblings and
  all 30 Agent requests remain strictly compared. Python fixture/business code
  and existing normal-path expectations are unchanged.
- 13 table cases cover wrong-typed consumed rule-library/rule/group fields.
  Only model.Issues triggers rejection; absent/null/empty values are not rejected
  merely for decoding to zero. Errors include resource, owning ID, field and
  exact stored JSON. Historical/Agent identity fields use the same validator.
- 11 HTTP write entries reject malformed/empty/nonfinite JSON with Chinese 400
  before writes. Local HTTP table: 11 x 5 = 55 cases. Flask differential:
  11 x 3 nonfinite words = 33 cases, asserting Python's exact status and Go's
  error envelope. No global legacy parser switch or unrelated route change.
- Shared model positive regression covers numeric group names and quoted region
  ports. While the approved 8a shared decoder is pending, this test explicitly
  skips rather than approving fallback zero output. No coercion is copied.
- Input model immutability fuzz; existing custom-port YAML/alias fuzz retained.
  Original corpus/golden files and baselines are not edited.

## Verification

Commands, final output and commits are filled below after targeted checks.
