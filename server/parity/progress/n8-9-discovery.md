# N8-9 discovery

Base: batch-6 `8ec68fd4d`, rebased onto the approved shared-model/provider fix
`0a0aa27f8`; branch `go-n8-9-discovery`.
Targeted verification only, fixed `RT_HOST=sunbroke@192.168.0.32`.

## Changes

- `repository.go`: typed Repository.Config/Shared/View and
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
| publicServices, probeControls, pickTargets, exitRequest/serviceRequests/domainRequest, exitInfo, serviceMatrix, classifyRegion, summarizeRegion, serviceDomains | N8-9 health/check |
| registryAgents (retains runtime heartbeat overlay), managerClient Probe/Targets/Region, supported version comparison | N8-10 agents |
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
- apply/probe non-object roots now produce explicit Chinese 400 rather than
  Python's AttributeError/500 (two exact existing HTTP cases). The website
  endpoint retains its existing R5/default-domains behavior for such roots.
- 11 HTTP write entries reject malformed/empty/nonfinite JSON with Chinese 400
  before writes. Local HTTP table: 11 x 5 = 55 cases. Flask differential:
  11 x 3 nonfinite words = 33 cases, asserting Python's exact status and Go's
  error envelope. No global legacy parser switch or unrelated route change.
- Shared model positive regression covers numeric group names and quoted region
  ports. It is active after integrating `0a0aa27f8`; no coercion is copied.
  Ignored-domain members use model.String followed by domain validation, so null,
  numeric non-domain values and malformed host names cannot become ignore rules.
- Input model immutability fuzz; existing custom-port YAML/alias fuzz retained.
  Original corpus/golden files and baselines are not edited.

## Verification

Commands, final output and commits are filled below after targeted checks.

### Exact commands and results (.32)

All commands used `RT_HOST=sunbroke@192.168.0.32`.

```sh
rt -- bash -lc 'set -e; cd server; gofmt -w internal/discovery/*.go internal/discovery/difftest/*.go internal/api/domaindiscovery/*.go; test -z "$(gofmt -l internal/ tools/)"; go vet ./...; go test -race -count=1 ./internal/discovery ./internal/api/domaindiscovery ./internal/agents/metrics ./internal/tasks; go test -race -count=1 -timeout=15m -run "Client|TrafficReport" ./internal/api/agents; go test -race -count=1 -run "Corpus|Reference|Golden" ./internal/rules ./internal/rulefetch ./internal/health ./internal/health/check ./internal/agents/trafficstore; CONFIGFLOW_DIFF=1 go test -count=1 ./internal/discovery/difftest'
```

Exit 0. gofmt and vet silent. Final output:

```text
ok configflow/internal/discovery 21.348s
ok configflow/internal/api/domaindiscovery 10.265s
ok configflow/internal/agents/metrics 14.705s
ok configflow/internal/tasks 1.473s
ok configflow/internal/api/agents 27.315s
ok configflow/internal/rules 8.924s
ok configflow/internal/rulefetch 29.150s
ok configflow/internal/health 2.585s
ok configflow/internal/health/check 1.030s [no tests to run]
ok configflow/internal/agents/trafficstore 1.041s [no tests to run]
ok configflow/internal/discovery/difftest 14.865s
```

The enabled Flask run includes the existing 78 request/storage snapshots and 30
Agent calls, the existing CFG-06 duplicate-target cases, and the new 33 exact
nonfinite expectations. No skip/normalization of arbitrary fields or cases.

```sh
rt -- bash -lc 'cd server && go test -count=1 -run "^$" -fuzz "^FuzzNativeConfigProjection$" -fuzztime=60s -parallel=2 ./internal/discovery && go test -count=1 -run "^$" -fuzz "^FuzzCustomPortYAML$" -fuzztime=60s -parallel=2 ./internal/discovery'
```

Exit 0: native model immutability 669225 executions, PASS 60.111s; custom-port
YAML 16833 executions, PASS 61.021s. The existing alias/depth/merge/unknown-tag
regressions and new shared-scalar and runtime-heartbeat regressions also pass.

An earlier, broader command used the same full API package without a longer
package timeout. Its unrelated upgrade fixtures exhausted the default cumulative
10m at TestUpgradeExemptionsDoNotOpenAgentAdministration (the individual test had
run 20s; stack in model/repo decode during Agent registration). It reported
`FAIL configflow/internal/api/agents 600.074s`; it had already passed discovery,
HTTP, metrics and tasks. The narrowed direct-caller race above passed all Client
and TrafficReport cases in 27.315s. This is a verification scope/timeout limit,
not a claimed full api/agents race pass. The chained corpus/Flask stages did not
run in that aborted command and were executed explicitly in the successful run.

No full gate, production snapshot read, baseline change, push or sub-agent.
