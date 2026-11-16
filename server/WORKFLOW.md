# Module-level workflow (from 2026-11-15)

Replaces the package-by-package fan-out described earlier in `HANDOFF.md`. Agents that port or review a
module read `PORTING.md` (rules, API reference) and their domain's `PROGRESS-<domain>.md`; nothing else is
required to start.

## Domains

| Domain | File | Contents (old package numbers) |
|---|---|---|
| `config` | `server/parity/PROGRESS-config.md` | foundation already ported (#0 jsonx/store, #1 infra, #2 repo, #3 httpx/auth/settings, #4 rules, #5 subs) and configuration/generation: converters (Mihomo, Surge/Loon, MosDNS, OxiDNS), remote rules/providers, Sub-Store client, health/latency, resource routes, generate/profiles/import-export, setup flow, domain discovery (#6–#19) |
| `agents` | `server/parity/PROGRESS-agents.md` | registry/heartbeat, metrics/traffic, installer rendering, Docker HA rendering, deployment bundle, push diff, agent routes/upgrades, agent rule sync, DNS follow, DNS HA, Linux gateway, IPv6 readiness (#20–#31) |
| `routers` | `server/parity/PROGRESS-routers.md` | router core, iKuai, RouterOS core/policy, router DNS/IPv6, router publish/rule sync, clients/split (#32–#38) |
| `finish` | `server/parity/PROGRESS-finish.md` | MCP (#39), image/entrypoint (#40), whole-server differential |

Dependencies: `agents` publishing needs `config` generation; `finish` needs everything. `config` and
`routers` can run side by side; `agents` modules that do not need generation (registry, metrics,
installers) can run early.

## Steps per domain

1. **Split (planning agent).** Assign every non-test Python file of the domain to exactly one module of
   300–1500 Python lines (a file larger than 1500 lines may be its own module or be split by function
   groups, named explicitly). No file may be left out: the plan lists the domain's files with line counts
   and the module each belongs to, and states the total. For every module, inspect the existing Go code
   yourself — do not trust earlier hand-backs — and mark it `missing`, `partial` (list what is missing),
   or `ported` (code and tests exist; not yet reviewed). Also list the module's Python tests, its rows in
   `routes.tsv`, and its prerequisites. Result goes to `PROGRESS-<domain>.md`.
2. **Port (one fresh agent per module).** For each `missing`/`partial` module: the agent reads only
   `PORTING.md`, the domain's `PROGRESS` file, its module section, the module's Python files and tests,
   and the Go APIs it depends on. It writes the code and tests, builds and tests on the test server via
   `rt`, updates the module section (status `ported`, commits, decisions, pitfalls) and commits.
3. **Adversarial review (a different fresh agent per module), for every module, old or new.** Premise:
   *the Go version is wrong until proven otherwise.*
   - Walk every Python function and every branch side by side with Go: each `if`, exception,
     early return, default value, boundary, each SQL statement (same SQL and same results in SQLite), each
     Chinese message byte for byte, each log line that a user or test can see.
   - For non-trivial logic, run the Python function on the test server with edge inputs (empty/None,
     Unicode and full-width digits, CJK, emoji, time zones and DST edges, huge integers, floats, malformed
     input) to produce reference results, and add them as Go tests.
   - Fix every defect found and add a regression test. If not everything is fixed in one round, the
     module gets another review round by a fresh reviewer.
   - Only with **zero open findings** is the module marked `verified` in `PROGRESS`, with the review's
     commit ids and the list of checks done.
4. **Domain differential.** When every module of the domain is `verified`: run all Go tests of the domain
   and a Flask↔Go differential (pattern: `internal/httpx/difftest`) covering **every route of the domain
   with at least one success and one failure case**; for write operations compare the data written
   (`configflow.sqlite3` rows and files under `DATA_DIR`) after the request. Then walk the domain's Python
   files and test files once more and confirm each maps to a `verified` module. Only then is the domain
   marked done.

## PROGRESS files

`server/parity/PROGRESS-<domain>.md` is the handoff for the domain. Every agent updates it **when it
finishes a step and when it notices its context is running out**: what is done (with commit ids), what is
left, decisions, pitfalls; then commits it with explicit paths. An agent continuing someone else's work
reads it first instead of exploring the code base. Keep it factual and current; replace stale lines.

Per-package files in `server/parity/progress/` from the earlier workflow are inputs for the planning
agent; they are not maintained further.

## Concurrency, models, interruptions

- All agents `model: "opus"`. No sub-agents spawned by agents.
- At most **2 domains** active at a time, at most **2 agents per domain** at a time (e.g. one porter and
  one reviewer working on different modules).
- Interruption by the usage limit: after the reset, **resume the same agent** (`SendMessage`) — the
  maintainer wants continuity. A fresh agent continues from `PROGRESS` only if the original cannot be
  resumed. Each new module gets a new agent, so no agent's history grows across modules.
- Agents commit WIP before risky steps and when their context gets long.

## Coordinator duties

- Launch planning, porting and review agents per the steps above; keep `PROGRESS` statuses honest (only a
  reviewer sets `verified`).
- Re-run tests of anything marked `verified` before the domain differential.
- Push `go` after each verified module or domain step. Never touch production; high-risk router/Agent
  behaviour additionally gets a VM lab test after the maintainer agrees.

## Parity standard (changed 2026-11-16: functional parity)

On 2026-11-15 the maintainer kept strict byte-level parity, partly to allow rollback to Python. On
2026-11-16 Python was retired for good, and the standard became **functional parity** as defined at the
top of `PORTING.md`: features, routes, API fields/values, Chinese messages, Agent protocol, stored-data
compatibility and the semantics of generated configs must match; byte formatting, Python-internal error
texts, hash order and CPython-only Unicode edge cases are no longer chased. Reviews still walk every
branch against Python, but a finding must name a functional difference. Modules verified under the
strict standard stay verified. A one-time change of generated config bytes at the switch is accepted
(the maintainer is told before the switch, since Agents will receive one push).

## Test coverage policy (2026-11-16)

Python tests are no longer translated one by one by default. Equivalence is proven by recording the whole
Python test suite and replaying the recorded calls in Go, plus random corpora and Flask↔Go differentials;
only behaviour those cannot observe is hand-ported (rule 4 in PORTING.md). Every module's PROGRESS section
maps each Python test file to how it is covered. Reviewers check that mapping as part of step 3.

## Native-library swap (2026-11-16)

The maintainer's goal for the rewrite is Go-native code: wherever the feature is kept, Go libraries
replace the Python-emulation layers built so far. Each swap is its own module (port + adversarial
review like any other), run alongside the domains, verified by re-running the replays/corpora of every
consumer with structural comparison (functional parity). Planned order (cheapest/most contained first):

| Id | Layer | Replace with | Keep only |
|---|---|---|---|
| N1 | `pyip` (Python `ipaddress`) | `net/netip` — done inside R01 | helpers netip lacks (collapse/summarise) |
| N2 | `urlx`, `reqproxy` (requests URL/proxy rules) | `net/url`, `http.ProxyFromEnvironment` | – |
| N3 | `pystr` (CPython Unicode/str/int/float rules) | `strings`, `unicode`, `strconv` | none, unless a real user-data case needs it |
| N4 | installer zlib/zipfile port, `oxidns/fnmatch.go` | `archive/zip`, `compress/flate`, `path.Match` (+ `[!x]`→`[^x]`) | – |
| N5 | httpx Werkzeug emulation (send_file, conditional requests, routing quirks) | `http.ServeContent`, `net/http` | auth/route semantics the frontend uses |
| N6 | `rules` Python-`re` shims (icase tables, `re.sub` templates, `\N{}`) | `regexp` (RE2), `regexp2` for lookaround/backrefs (same as Mihomo's own filters) | – |
| N7 | `pyyaml` + `yamlx` loader/dumper, `subs/commented` editor | `gopkg.in/yaml.v3` (Node API keeps comments; Mihomo itself uses yaml.v3) | `yamlx` facade API |
| N8 | `jsonx` Python encoding (repr floats, ensure_ascii, separators) | `encoding/json`-style output | ordered `*jsonx.Object` type (unknown fields and key order survive) |
| N9 | Python exception emulation: `PyError`/class-name/message types in `health/check/pyerr.go`, `refresh/pyerr.go`, `routersplit/pyhelpers.go`, `rulefetch/pyerrors.go`, `subs/pyconv.go`, `substore/pystr.go` | plain Go errors | Chinese messages and HTTP status codes |
| N10 | per-package Python value glue: `convert/pyhelpers.go`, `mosdns/py.go`, `oxidns/py.go`, `oxidns/pyre_c23.go`, `routers/pyhelpers.go`, `httpx/pyconv.go`, `installer/py*.go`, `agents/metrics/pyvalue.go`, `agents/monitor/py*.go`, `agents/metrics/traffic_compat.go`, `repo/pyvalue.go`, `rules/pystr.go`, `rulefetch/pystr.go`, `rules/ipaddr.go` (do together with N3) | plain Go, `strconv`, `net/netip` | what a feature needs (e.g. JSON number int/float handling) |
| N11 | other stdlib ports: IDNA (`rules/idna.go`, `oxidns/idna2003.go`), `rulefetch/pyhtml.go` (html.unescape/HTMLParser), `agents/pydecode.go` (requests text decoding), `agents/pytime.go` (Python time formatting) | `golang.org/x/net/idna`, `html.UnescapeString` / `golang.org/x/net/html`, UTF-8 + `golang.org/x/text`, `time.Format` | API time formats the frontend shows/parses |
| N6+ | `rules/pyre_parse.go` (port of CPython's sre parser) + `pyre_case_table.go`, `pyre_icase.go`, `pyre_net.go`, `pyre_sub.go` | `regexp` / `regexp2` directly, small syntax shim (`(?P<name>`, `(?P=name)`) only | – |

The inventory above came from a name/comment sweep (2026-11-16) and may miss unmarked emulation. Every
swap agent sweeps its consumers again, every adversarial review checks "no Python emulation where a Go
library does the job", and the `finish` domain greps for leftovers before the switch.

Consequences accepted by the maintainer: YAML 1.1 implicit types (`yes/no/on/off`, `NO`, sexagesimal,
`0777`) follow yaml.v3 (= Mihomo's own reading); generated config bytes and stored JSON formatting
change once at the switch (Agents get one push; tell the maintainer before switching); CPython-only
Unicode edge cases follow Go. Functional checks that must still hold after every swap: the domain
replays/corpora/differentials with structural comparison, existing `configflow.sqlite3` loads and
round-trips without loss.
