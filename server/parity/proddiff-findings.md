# Production-data differential: Go findings

Differences found by `server/scripts/proddiff.sh` that are Go behaviour, not harness artefacts.
Reported by route template, key path and type only (no snapshot values). Each open finding has
matching entries in `proddiff-baseline.txt` whose reason names it; fixing the finding turns those
entries `stale` and the baseline must then be re-recorded.

| ID | Route template | Key path / type | Status |
|---|---|---|---|
| F1 | `GET /api/agents/<agent_id>/oxidns/rules/update` | `status` value, `Content-Type` value, `body` type, `header.Cache-Control` missing-in-go | open |

## F1 — OxiDNS rule update returns 500 for HA members (A18 not ported)

- Found by: O2 review (2026-11-17), via the new default-profile case (request without `profile_id`).
  Every earlier run sent `?profile_id=…`; the route rejects any query argument with
  `400 INVALID_REQUEST` on both sides, so its working branch had never been compared.
- Python: `200 application/json`, `{"success": true, "task": {…}}` (starts the rule-update task).
- Go: `500 text/html` (generic error page, no `Cache-Control`) for the snapshot's OxiDNS Agents that are
  HA members (2 of the snapshot's OxiDNS Agents). Go log: `oxidns_ha_publish.rules is not ported yet (A18)`
  (`monitor.ErrHARulesNotPorted`, `internal/agents/monitor/ruleupdate.go`).
- Type: missing dependency behind a registered route. The route is registered in the Go router and counted
  as implemented, but its HA branch depends on module A18 (DNS HA), which is still `missing`.
- Consequence entries: `GET /api/logs/info body.size` and `GET /api/logs/tail` (`total_lines`,
  `filtered_lines`, `logs` length, `logs[0]`, `logs[1]`) differ because Go logs the two request exceptions.
- Owner: agents domain (A18). Until A18 lands, the route should not be treated as accepted.
