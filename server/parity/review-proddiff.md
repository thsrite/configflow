# O2 review: production-data differential harness

Reviewer: Opus (did not write the harness). Date: 2026-11-17. Scope: `server/tools/proddiff/`,
`server/scripts/proddiff.sh`, `server/parity/proddiff-{baseline,normalize,coverage,generator-coverage}`.
All runs on 192.168.0.32 (network namespaces). Snapshot contents were never printed; this file names
route templates, key paths and counts only.

## Summary

The harness was structurally sound (deep JSON / parsed YAML / line compare, exact-route normalisation,
read-only bind mount, digest check, copies under a 700 directory), but it had one isolation hole, several
comparison blind spots, a weak self-check and a real flake. All are fixed below with tests; one real Go bug
was found (F1 in `proddiff-findings.md`).

| # | Severity | Problem | Fix |
|---|---|---|---|
| 1 | High | **DNS egress from the "offline" namespace.** `/etc/nsswitch.conf` on the test server uses `resolve`; the systemd-resolved varlink socket `/run/systemd/resolve/io.systemd.Resolve` is a filesystem socket, not netns-scoped. Inside the namespace both Python (`getaddrinfo`) and the Go binary (cgo resolver) resolved `example.com` through the host — snapshot hostnames (airport/WebDAV/subscription domains) were resolved on the home network during runs. Interface count = 1 did not prove isolation. | `proddiff.sh` mounts a tmpfs over `/run` (hides resolved, nscd, D-Bus, Docker sockets) and bind-mounts `hosts: files dns` + `nameserver 127.0.0.1`. `run.py` `assert_no_egress()` proves at start: only `lo`; no host IPC sockets visible; `getaddrinfo('example.com')` fails; TCP to 192.0.2.1 and 2001:db8::1 fails with ENETUNREACH. Verified: before the fix the same probe resolved, after it the run proceeds only because resolution fails. |
| 2 | High | **Response headers were never compared** (only `Content-Type`). `X-ConfigFlow-Profile` (Agents), `ETag`, `subscription-userinfo`, `Content-Disposition`, `Cache-Control`, `Location`, `X-Content-Type-Options` could change silently. | Every header except transport framing (`Date`, `Server`, `Content-Length`, `Connection`, `Keep-Alive`, `Transfer-Encoding`) is compared; paths `header.<Name>` (name printed only if it is a backend literal). Current state: headers agree on all 415 compared cases. |
| 3 | Medium | **Both-sided transport failure counted as agreement** (status 0 = 0, no body compare): two hung servers passed. | Reported as `status transport-both`. |
| 4 | Medium | **Duplicate keys swallowed.** `json.loads` / `yaml.safe_load` keep the last duplicate; Mihomo (yaml.v3) and Go JSON consumers reject or differ. | Strict JSON (`object_pairs_hook`) and YAML (`StrictLoader`, merge keys still allowed) → `parse-python`/`parse-go`. Duplicate ZIP members also rejected. |
| 5 | Medium | **int vs float treated as equal** (`53` == `53.0`). Typed consumers (Go Agent structs, yaml.v3 int fields) reject `53.0`. | Separate kind `int-float`. None currently. |
| 6 | Medium | **CRLF/CR normalised away** for text and embedded `content`/`config`; a CRLF install script would pass. ZIP member mode bits (`+x`) and line endings not compared. | Kind `line-endings` (top-level text, embedded strings, ZIP members via `<member>#eol`); ZIP `<member>#mode`. Trailing-newline normalisation kept (harmless). |
| 7 | Medium | **Flaky `GET /api/agents/<agent_id>/metrics/summary`** (seen in 2 of 56 private runs; `metrics/history` once). Root cause: `MetricsHistory.get_metrics` window is `datetime.now() - hours`; the snapshot has a sample every 30 s; Python and Go are asked a few ms apart, and when the cutoff passes a sample in between, `data_points`/means differ. Not a Go bug. Reproduced deterministically by delaying Go 31 s after Python: `metrics/history` differences on every crossing case. | Routes declared `"bracket": true` in `proddiff-normalize.json` (metrics history/summary, traffic stats/trend, traffic-history): on a difference, Python and Go are re-asked alternately (≤3 rounds) and the pair passes only if Go equals the Python reply immediately before or after it. With the same 31 s skew: 6 cases rescued, 0 differences. A constant Go error still fails (unit test). Undeclared routes are never retried. |
| 8 | Medium | **Self-check could pass with a broken comparator**: YAML/text checks accepted *any* difference (a raw-text comparator or one that always reports would pass); JSON check used only a synthetic document. | 23 mutations, each must report its exact path/kind: controls (real reply vs itself, re-encoded real JSON, re-dumped real YAML must be equal); status, Content-Type, real header value/missing, both-sided transport; synthetic deep leaf, int↔float, int↔string, duplicate key, list order; on the largest real Go JSON reply: leaf, missing key, extra key, null vs value, list order; on real Go YAML: leaf at exact path, duplicate key; on real Go text: changed line at exact index, inserted line, CRLF. Unit test proves a comparator that never or always reports fails. |
| 9 | Medium | **Coverage holes.** Every request carried `?profile_id=`, so default-profile resolution was never exercised — and routes that reject any query argument (`oxidns/rules/update`) only ever compared their 400 branch. Routes compared only with synthetic `proddiff-missing` parameters were counted like real coverage. A Go/Python route registered under a different template would be counted "not implemented" forever. | One extra request per route/entity without a profile selector (found F1). Coverage file records `<route> snapshot-parameter`; losing all snapshot-derived parameters is a coverage regression. Summary reports `GET_routes_error_branch_only` (2: `oxidns/queries/<record_id>`, `domain-discovery/probe/<job_id>`). Go router and Flask `url_map` routes must all be in `routes.tsv` (`UNMAPPED-*-ROUTE` fails the gate; currently 0). |
| 10 | Low | **Normalisation broader than needed.** Blind replacement of timestamps, data paths, log lines; three baseline entries hid *any* `body.message` change on Agent log routes (including `Agent not found`). | Each server's own data-copy path is scrubbed to `<DATA_DIR>` (removed `path`, `log_file`, `size`, `size_mb` rules). Timestamps use `tolerance` (same textual shape, within N s). Agent log routes use `pattern` on the literal `Connection error: ` prefix only. Log lines mask only the leading timestamp and compare as a multiset (`unordered`, concurrent writers). Subscription download file names mask only the `YYYYmmdd_HHMMSS` part. Rule file is validated (known fields, exact routes present in `routes.tsv`, one mode). Baseline entries with reason `open` now fail the gate. |
| 11 | Low | **Background asymmetry.** Go disabled 6 lifecycle loops, but `subscription-cache-prune` is one-shot startup work that Python still runs synchronously at import (`prune_orphan_caches`), so the two copies started from different states. Go loop names were captured before lazily wired loops registered. Python disabling was trusted, not observed. | Test lifecycle runs `subscription-cache-prune` synchronously and writes a ready marker after `lifecycle.Start`; the harness waits for it. Loop list written after Start (6 names, 5 disabled + 1 startup-once). `flask_worker.py` audits every `Thread.start`; any disabled loop name starting fails the run (observed: only waitress workers, provider-render pool, one anonymous helper). The remaining asymmetry (Python 10 vs Go 5) is safe: the five extra Python loops (DNS follow, HA publication, IPv6 verification, Agent and router rule sync) have no Go implementation yet, so there is nothing to disable on the Go side. |
| 12 | Low | **Interleaving.** Read-only POSTs that stage artifacts and log readers ran concurrently with GETs; log tail content depended on scheduling. | Phases: GETs (parallel) → POSTs (parallel) → `/api/logs/*` (serial, last). |
| 13 | Low | **Cleanup.** SIGHUP (rt/SSH drop) killed the run without removing copies. | SIGHUP handled like SIGTERM; a shared run lock lets the first run that gets it exclusively delete `copies-*` left by killed runs. |

Checked and found correct: deep JSON compare (dict keys both ways, list length + per index, `bool` distinct
from numbers); parse failures are differences, never equality; generator both-error compares error text;
single-sided transport failures fail the gate; snapshot is a read-only bind mount with digest before/after
and no symlinks; copies live in a temporary directory under the 700 run directory and are removed on normal
exit and exceptions; no auth back door — both servers use the existing `ADMIN_*`/`JWT_SECRET_KEY` env login;
the Go overlay exists only in a temp dir with `//go:build configflow_proddiff`, no repository file carries
the tag, so `go build ./cmd/configflow` cannot include it; console output is templates, schema-safe key
paths and counts; private reports stay in `~/cf-prod-snapshot/runs/run-*` (700).

## Residual limits (not fixed, by design or out of scope)

- **Snapshot ageing.** Windowed routes compare whatever is still inside the window. The snapshot's newest
  metrics sample is ~2.6 h before 2026-11-17 01:00 UTC; the 1 h windows are already empty, the 24 h window
  empties within a day. Those routes then compare empty results. Refresh the snapshot periodically.
- **YAML comments** are not compared (e.g. the `# Generated:` / subscription-detail header of
  `/api/subscriptions/proxies`). Not part of config semantics.
- **JSON object key order** is not compared (Flask sorts keys; frontend does not depend on order).
- Profile selection via `X-ConfigFlow-Profile` header and `?profile=` alias is not exercised (only
  `?profile_id=` and no selector).
- Write routes, device effects and SQLite/file state after writes remain phase two (README).

## Verification

- `rt -- bash -lc 'python server/tools/proddiff/test_proddiff.py'`: 20 tests OK.
- Flake reproduction (temporary, not committed): 31 s Go delay on `metrics/history` without bracketing →
  `NEW … metrics/history body.data.history[9].* value` etc.; with bracketing → `bracket-rescued: 6`, 0 differences.
- Final gate: three consecutive `rt -- server/scripts/proddiff.sh` runs on .32 after `git rebase origin/go`,
  identical summaries (see hand-back).
