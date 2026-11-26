# Package #20 agent registry — progress

## Brief
Port `backend/agents/manager.py` (AgentManager), `common/agent_manager.py`, `runtime_state`, `diagnostics`,
`operation_events`, `idle_rounds`, `presence_log`, `agents/version`, `deployment_messages`, `upgrade_messages`
to `server/internal/agents`; routes in `server/internal/api/agents`. Later packages (#21 metrics, #24/#26
publish, #27 rule sync, #28 DNS/address follow, #29 DNS HA, #30 gateway install, #31 IPv6) plug in through
`agents.RegisterHooks`, `OnAgentDeleted`, `OnManagerInit`, `SetRuleSyncRow`/`SetDNSFollowRow`. Agent HTTP
calls go through `agents.Client` (fake in tests). Register only pure-registry routes whose full behaviour
works now; update `parity/routes.tsv`. Wire via `agents.Wire` in `wirePackages` + `apiagents.Register` in
`routePackages` (one line each in `internal/server/server.go`). No sub-agents; tests only via `rt`.

## Done (package complete, waiting for coordinator review)
- Commits ee00305e, 1dd4e700, 277c7c53 and the final `go(agents): differential check ...` commit.
- `internal/agents`: all listed modules ported; doc.go has the API reference. Tests: 91 pass (+1 partial
  skip: GC part of idle-rounds weak-key test), `-race` clean.
- `internal/api/agents`: POST register, POST heartbeat, POST /api/agents, PUT/DELETE /api/agents/<id>.
  Route tests + difftest (`difftest/`): 52 requests + stored Agent records, 0 differences vs Flask.
- routes.tsv: 5 rows done; other /api/agents rows carry the waiting package (21,22,23,24/26,25,26,27,28,
  29,30,31,37,38,11,19); rebind route = #17.
- server.go: apiagents.Register (routePackages), agents.Wire (wirePackages).

## Next
- Nothing; follow-ups only on coordinator request.

## Decisions and findings
- Transport error texts (`Connection error: ...`, `error_details`) carry Go net/http texts, not requests'.
- FailureText: RE2 `\b` is ASCII (Python's is Unicode).
- Hooks default: ReceiptMatches nil → fail closed (OxiDNS activation refused); HA pair paths and
  PublishDeployment → ErrNotPorted; installation registration → InstallationError 501.
- `rebind_profile_agents` route lives in routes/profiles (#17); manager method ported.
- notifications.Complete() only checks ListTasks+Agents; agents.Wire installs the Agents source only when
  tasks.Complete() (else GET /api/notifications would register with a failing task list). Needs the
  coordinator: either list agents.Wire after task-row providers or add tasks.Complete() to the gate.
- GET /api/agents and GET /api/agents/<id> not registered (content_sync #24, upgrade artifacts #26);
  router panics on reusing endpoint "agents.handle_agent_item" with another handler, so #26 must merge
  its GET into the PUT/DELETE handler here.

## Commands
- Diff: `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/agents/difftest/'`
- `rt -- bash -lc 'cd server && go vet ./internal/agents/... ./internal/api/agents/... && go test -count=1 ./internal/agents/... ./internal/api/agents/...'`
- Regenerate tables: `~/program/configflow/.venv/bin/python -I server/internal/agents/testdata/gen_changelog.py $PWD > server/internal/agents/version_changelog.go` and gen_messages.py → messages_data.go (see
  headers of version_changelog.go / messages_data.go; dict → Go literal).
