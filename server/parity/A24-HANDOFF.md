# A24 — client tools, OxiDNS policies and discovery switches

Branch: `go-port-a24`. Based on local `go`; no push. Final acceptance host: **192.168.0.64**.

## Scope and behavior

All eight method/path pairs are implemented and marked done in routes.tsv:

| Methods | Agent suffix |
|---|---|
| GET, PUT | client-notes |
| GET | mihomo/rule-targets |
| POST | mihomo/rule-targets/apply |
| PUT | domain-discovery |
| POST | domain-discovery/clear |
| GET, PUT | oxidns/device-policies |

Every route requires admin JWT. Agent bearer credentials are rejected. Bound profile selection ignores the global UI profile. GET/HEAD never writes. Notes use previous-name compare-and-set; policy writes check the latest binding, settings revision and policy-name revision in the repository transaction. Name-only changes preserve OxiDNS settings, native draft and settings revision. IP/domain writes reuse C09/C37 and actual local-cache owners. Cleanup deletes traffic, probes and regions in Python's order and retains traffic_history. Toggle responses use the live heartbeat overlay and the shared public Agent adapter.

C37 clarification (coordinator approved without a separate design): `ApplyOptions.BeforeWrite` is called under the shared transaction before library mutation, cache writes or history writes; errors abort. It receives the transaction context for fresh uncached binding/default queries. `QueryRuleset(json.RawMessage, id)` returns concrete `*RulesetInfo` using the original C37 logic. Existing calls remain compatible. PROGRESS-config C37 records both interfaces.

The same-package `publicAgent(*agents.Record) *agents.Record` adapter delegates deployment/upgrade/HA sanitization, removes credentials/private comparison data and unread-history sentinels, and preserves future public fields. The helper was separately committed as **78b796af** and communicated to the coordinator for A13 before rebase. A13 must reuse it; PROGRESS-agents A13 states this.

All new write entry points use `lifecycle.Detach` in the owner. No handler calls WithoutCancel, no background goroutines or new dependencies were added. Legacy JSONx access is confined to adapters.

## Boundary evidence and Python test mapping

`TestA24FlaskBoundaries` runs real Flask on a copied synthetic SQLite/data directory, compares status, JSON, no-store headers, complete database documents and every JSON/YAML file after **every** request. The actual Python libraries write rule caches and native policy drafts; no implementation result is frozen as an expectation. A07's existing catch-all is registered alongside the literal rule-target routes. All fixtures are synthetic; Python network calls are explicitly forbidden.

`TestPublicAgentPython` compares ten full/unread record combinations with actual Python `_public_agent`, including empty/nonempty HA and private deployment content/receipt, token, DNS follow job, HA private children, upgrade diagnostics, future fields and empty/null cases; source records remain unchanged and secret markers never appear publicly.

`TestClientToolsConcurrentEditors` proves exactly one of two simultaneous edits commits, with the other receiving a conflict; settings revisions remain unchanged for name-only policy updates. Cancellation owners finish all write families, including domain/IP adoption. Stale binding/default guards reject without writes; toggle preserves runtime metrics/version. C37 tests prove guard rejection leaves the entire repository/cache unchanged and a passing guard produces the same response, library and history as an unguarded call. The parser fuzz target checks accepted connection entries remain stable across the JSON boundary.

All 15 `backend/test_client_tools.py` functions are mapped individually by behavior in PROGRESS-agents A24. HTTP cases are exercised here; shared legacy fallback/removal/capacity cases retain C31's real Python `TestDeviceNotesBoundary` and native `TestLegacyNamesAndPolicyPlan`; clone/export retains the C31 repository roundtrip checks. Additional mappings: device-policy API cases from `test_oxidns_device_policy.py`; discovery live-state case from `test_agent_runtime_state.py`; existing A05 discovery reporting tests remain unchanged. No route or feature remains unported.

## Findings and differences

No unresolved findings. Integration defects caught and fixed while porting:

- Validation reliability: translate C09's legacy validation error into the repository error recognized by the HTTP layer, preserving Python's 400 instead of returning 500.
- Write safety: fresh binding/default/dual-revision checks run inside transactions; a rejecting C37 guard prevents all writes. Registry discovery switch now detaches in its owner. Two existing C37 adapter calls were also changed from WithoutCancel to lifecycle.Detach so downstream history/cache calls retain the generation shutdown signal.
- Public serialization (medium): the shared deployment serializer treated an empty HA object as an active publication, adding/changing status/message/success fields. Require a nonempty HA object, matching Python; the real `_public_agent` reference now includes empty HA child/latest records.
- Read consistency: rule-target summaries use one captured profile snapshot; write guards deliberately bypass request caches.

No intentional functional difference or newly fixed Python bug. The existing accepted formatting class applies to generated native YAML (`native_config`, `template_base_config`): compare parsed structure and values, not YAML indentation. Since settings revisions hash those exact serialized strings, each side's response revision is separately verified against its actual stored settings and used for that side's subsequent request. No semantic field is dropped. Only nondeterministic created_at/updated_at/applied_at strings are normalized.

## Final commands and results

Final acceptance start: local go `ac1f2377`. Initial `/proc/loadavg`: `13.02 13.83 14.29`; later observed `30.89 21.37 17.07` while other agents shared the host. No artificial pressure experiment.

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -o pipefail; cd server && cat /proc/loadavg && gofmt -l internal/api/agents/{public_agent_adapter,public_agent_test,client_tools,client_tools_adapter,client_tools_diff_test,client_tools_messages,agents}.go internal/agents/manager.go internal/discovery/{write,bridge,query,a24_test}.go && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/agents/... ./internal/discovery/... ./internal/agents'
```

Exit 0. gofmt: no output; full vet: no output. Race tail:

```text
ok  configflow/internal/api/agents             162.725s
ok  configflow/internal/api/agents/difftest     267.586s
ok  configflow/internal/discovery                9.924s
ok  configflow/internal/discovery/difftest        9.582s
ok  configflow/internal/agents                252.748s
```

First full ordinary suite passed (93 package lines); all 12 enabled difftest packages passed. Parser fuzz: PASS, 502,767 executions in 61.020s with 2 workers.

Final delta was separately formatted/vetted and race-checked on .64 (loadavg `21.50 21.08 19.43`):

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api/agents/client_tools_diff_test.go --back server/internal/api/agents/public_agent_test.go --back server/internal/api/agents/messages.go --back server/internal/discovery/bridge.go --back server/internal/deploy/adapter.go -- bash -lc 'set -euo pipefail; cat /proc/loadavg; cd server; gofmt -w internal/api/agents/{client_tools_diff_test,public_agent_test,messages}.go internal/discovery/bridge.go internal/deploy/adapter.go; go vet ./...; CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/agents -run "TestPublicAgent|TestA24|TestClient"; CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/discovery/... ./internal/deploy'
```

```text
ok  configflow/internal/api/agents          13.705s
ok  configflow/internal/discovery           10.329s
ok  configflow/internal/discovery/difftest    9.822s
ok  configflow/internal/deploy              22.951s
```

Exit 0. Final rebase/full ordinary suite/all enabled difftests/proddiff: pending.
