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
| N8 | `jsonx` (ordered dynamic documents + Python encoding) | typed structs + `encoding/json`, unknown fields via `json.RawMessage`, free-form YAML as `yaml.Node` | nothing |
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

Size check (2026-11-16): Go non-test code is ~150k lines for roughly half of the 86k-line Python
backend. About 25k lines are Python-emulation layers (table above); much of the rest is code that
reproduces Python's failure modes on malformed input (type checks raising emulated TypeError/KeyError,
exception texts) and private per-package copies of shared helpers. Under functional parity, every swap
module and every review from now on also **deletes code that only existed for strict parity** and
**merges private helper copies into one shared package**, and reports lines before/after.

**N2 (done, commit 6afb0554).** `internal/urlx` and `internal/reqproxy` deleted, plus
`installer/pyurl.go`, the urlsplit copy in `convert/pyhelpers.go`, agents' requote/proxy emulation and
the 994-row requests proxy reference. Callers use `net/url` / `net/http` through `internal/weburl`
(608 lines): `Parse` (url.Parse after the typed-URL clean-up; bare `%zone` accepted; unbalanced
brackets, unbracketed IPv6 and NFKC-delimiter hosts refused), `Hostname`/`Port`/`RawPath`/
`HasCredentials`, lenient `Unquote` (redaction), `Prepare` (RFC 3986 normalisation before a request),
`ParseProxy` + `FromEnvironment` (lower-case first, ALL_PROXY counts, NO_PROXY domain/IP/CIDR/port/`*`,
**loopback/localhost always go direct** — 2026-11-17 coordinator decision replaces A07 decision 5 with native `httpproxy` semantics — chosen per redirect hop by
http.Transport), `SafeForLog`/`SafeErrorDetails`. Accepted classes (tests compare outcome):
URLs net/url refuses that urllib accepted (control chars, malformed `%` escapes or ports, IPvFuture,
`{`/space in hosts, non-ASCII or unencoded credentials) are rejected; non-Chinese exception texts
compared by class only; requests' all-`%` requoting when one escape is malformed and other byte-level
quoting; redirect hops pick their own proxy (requests kept the first URL's); SOCKS proxies now work;
invalid UTF-8 in query values becomes U+FFFD (Werkzeug re-quoted); upstream labels for odd inputs;
CPython full case-folding (`İ`, final sigma). The adversarial review below replaced the remaining
private urlsplit copies in `rulefetch/urls.go` (SplitURL/URLJoin) and `subs/proxyserverdns.go`
(urlSplitHost). Lines (non-test, urlx+reqproxy+weburl+agents+substore+
rulefetch+localhttp+health+installer+convert+provider+redact+httpx): 59602 → 58285.

**N2 proxy follow-up (2026-11-17).** Shared `weburl.FromEnvironment` now uses
`golang.org/x/net/http/httpproxy.FromEnvironment().ProxyFunc()` with ALL_PROXY /
all_proxy as fallback only when the corresponding native HTTP(S) selection is
empty. CGI is never bypassed by the fallback. This replaces the former custom
NO_PROXY matcher. Localhost and loopback always go direct; use a routable Agent
address when an environment proxy is required. Empty lowercase values fall back
to uppercase; NO_PROXY `:port` alone is not a host match; `.example.com` covers
subdomains only. CGI returns an error without sending any request. These native
outcomes are explicitly asserted in the real requests differential and Agent /
Sub-Store client boundaries; [ledger](parity/behaviour-changes.md).

**N2 review (adversarial, 2026-11-16) — VERIFIED; zero open functional findings.** Commits:
`288394c5` (N2 changes) and `87e47955` (installer test-fixture race found during coordinator verification).
F1–F4 are fixed: userinfo bytes rejected by `net/url` are escaped before parsing; `%2F` stays in
`RawPath` alongside non-ASCII; log redaction cuts userinfo before decoding; rule query appending no
longer parses a rule name as a URL. The URL/proxy Python emulation was removed, including SplitURL,
URLJoin and `urlSplitHost`; `agents/client.go` now uses UTF-8 Basic auth and the Go resolver instead of
Latin-1 and glibc `inet_aton` emulation. Scoped IPv6 Agent hosts remain supported.

Accepted differences are narrow and checked at consumer boundaries:

- The RFC 3986 `urljoin` references `http:g`, `..;x` and `//[v1.x]/p` occur only in the synthetic
  URL conformance table in `rulefetch/testdata/gen.py`; the real rule-fetch redirect fixtures contain
  none of them. IPvFuture is not a requestable host. These rows are classified in `urlAccepted`.
- `url_requote` keeps valid unreserved escapes such as `%7e` in the request target instead of decoding
  them. The loopback server receives the equivalent target and returns HTTP 200 with the same body;
  the comparator decodes only letters, digits, `-`, `_` and `~`, never dot or reserved escapes.
- NFKC delimiter authorities are accepted as Go-native URL parsing differences in synthetic inputs;
  they cannot form a valid urllib request. `refresh.host` also accepts its two explicitly invalid,
  non-requestable inputs (`http://host.test:abc/`, `http://%41.test/`) returning an empty Go host.
- Invalid DNS percent escapes remain rejected at the DNS configuration boundary. Invalid UTF-8 SNI
  uses `strings.ToValidUTF8`; no valid hostname depends on per-byte replacement.
- URL host case is compared case-insensitively for requests preparation. Local API dispatch checks the
  decoded path, so an escaped unreserved `/api/` prefix still reaches the same local endpoint.

Verification was isolated from other agents' uncommitted files: detached worktree at `229f2abe` plus
N2-owned files ran `rt -- bash -lc 'cd server && go vet ./... && go test -count=1 ./...'` with exit 0;
final lines were `ok configflow/internal/weburl 0.010s` and `ok configflow/internal/yamlx 1.546s`.
`go test -race -count=1 ./...` was rerun after the test-server restart: installer passed after the
fixture setter was mutex-protected; all packages passed except `internal/oxidns`, whose recorded editor
test exceeded Go's default 10-minute timeout. A focused
`rt -- bash -lc 'cd server && go test -race -count=1 -timeout=20m ./internal/oxidns'` passed with
`ok configflow/internal/oxidns 794.000s`. The race suite's remaining packages all passed in the full run.
The private worktree was cleaned with `rt clean` and removed. The shared worktree still has other
agents' uncommitted changes in `rulefetch/website.go` (Unicode whitespace hunk), `subs/ported_test.go`
and `parity/PROGRESS-config.md`; these were left untouched and unstaged.

## Idiomatic Go (2026-11-16, maintainer: "用 go 原生库")

PORTING rules 1–4 now say: port features, not code; typed structs; acceptance at the external boundary
(HTTP differential, generated outputs, stored data, Agent protocol), function-level replay of Python
internals no longer required; no size budget (removed 2026-11-16) — the hard rule is no Python emulation. Consequences:
- New modules (and modules still in progress) follow the new rules immediately.
- Already written code is slimmed in **S passes**, one per package group, done together with the
  matching N swap where one exists (e.g. S for `subs` + N7 YAML). An S pass rewrites the package the Go
  way, replaces function-level replay tests by boundary tests, merges duplicated helpers, and reports
  lines before/after. A module verified earlier stays verified only if its boundary tests pass after the
  S pass; the S pass gets its own adversarial review (checks: no lost feature, boundary tests cover every
  Python test file's behaviour, no Python emulation left).
- Reviewers of new modules reject Python-shaped code (`any` parameters for
  Python call shapes, emulated exceptions, private copies of shared helpers).

Note for N7 (from review R01, 2026-11-16): every reader of the same user YAML must agree. `yaml.v3`
rejects duplicate mapping keys and multi-document input differently from the PyYAML port; R01's fake-ip
range reader once used yaml.v3 directly while the Mihomo generator used yamlx, so a duplicate key made
them disagree (fake-ip traffic routed past the gateway). When N7 swaps yamlx to yaml.v3, decide duplicate
keys / multi-document handling once in the yamlx facade and keep all readers on it.
Note (2026-11-16): R01 also ports 67 validator lines of dns_guard/ipv6_pd_guard/client_fixed_ip; R09/R11/R17
reuse them. Size budgets were dropped the same day: line counts are reported for information only.

## Switch checklist (collected while porting; do these when Go replaces Python in production)

- Frontend: delete the unused `generateDockerCompose` / `generateDockerRun` helpers in `frontend/src/api/index.ts` (they call `/agents/docker-compose` / `/agents/docker-run`, which never existed; the UI uses `docker-agent-compose/run`). Found by the agents difftest matrix, 2026-11-17.

- One full publish to every Agent and router right after the switch (generated config bytes, OxiDNS
  resource tags and device-policy revision hashes change once; accepted by the maintainer 2026-11-16).
- Stale resource files under old names: removed by the Agent/deployment cleanup, or cleaned once.
- Tell the maintainer before the switch that Agents will receive one push.

## Division of work (maintainer, 2026-11-16)

> Who does what (HR ports, Opus limit, quota fallback) is superseded by "Fixed assignment by risk" at the end.

- **Codex (Paseo, `codex/gpt-6.1-sol`, thinking `high`, mode `full-access`)** does most work: ports,
  S passes, native swaps, Python reference corpora, Flask↔Go differentials, and adversarial reviews of
  non-HR modules (by a different Codex agent than the author). Each Codex agent gets its own Paseo worktree
  branched off local `go` and ends with `git rebase go`; shared rules in `server/AGENT-BRIEF.md`.
- **Claude Opus** for: **all structural design** — before Codex implements any native swap, S pass of a
  package, new package or cross-package interface (types, package boundaries, public API, hooks, how
  stored data/unknown fields are held, background tasks/wiring), Opus writes a short design section in
  PROGRESS/WORKFLOW and commits it; Codex then implements to that design and may not change it without
  asking. Single-module ports that only follow existing patterns need no separate design. Also Opus:
  adversarial review of HR modules (routers writing to devices R04, R05, R07–R12, R14; agents
  publish/rule sync/DNS follow/HA), concurrency/state problems, and escalation when Codex fails the same
  problem twice. At most 1 Opus agent at a time.
- ~~**When Claude quota is exhausted:** HR modules get a Codex review first … `verified (codex)` …~~
  **Superseded** by "Fixed assignment by risk" (last section): Opus tasks wait for quota instead. `go` is not production; the switch additionally requires the PVE lab test.
- **Coordinator** (dispatch, verify on a detached worktree via `rt`, emulation gate, fast-forward + push
  `go`): this Claude session; if Claude quota is out, Codex agents finish their task and stop with their
  branch ready; the coordinator merges when back. A fresh coordinator session starts from `HANDOFF.md`.

## Design docs, hand-offs and escalation (maintainer, 2026-11-16)

- **Go style:** all Go code follows the "Go 后端（`server/`）代码风格" section of the root `AGENTS.md`
  (naming, `%w` errors, no panics, ctx first, doc comments, table tests, Chinese messages as constants in
  `messages.go`, `logx`, gofmt/vet). Reviewers check it; violations in new code are findings.
- **Opus design template** (every structural design, committed as a section of PROGRESS/WORKFLOW or
  `server/design/<topic>.md`):
  1. *Scope* — what the design covers and what it explicitly leaves out.
  2. *Interface contract* — Go types, package boundaries, exported functions/methods with signatures,
     error values, which package owns which data; how unknown stored fields are kept.
  3. *State and concurrency* — state transitions (table or list), who mutates what, locks/goroutines,
     cancellation behaviour (PORTING rule 4b).
  4. *Buildable steps* — an ordered list where every step compiles and passes `go test ./...` on its own,
     so Codex can commit after each.
  5. *Acceptance* — boundary checks that prove it (routes/differential, generated outputs, stored data,
     Agent protocol), and which Python behaviour is intentionally not kept.
- **No half-finished hand-offs:** an agent (Opus or Codex) never hands over uncommitted or non-compiling
  code. Before stopping it commits what compiles (or reverts what doesn't) and records the remaining steps
  in its PROGRESS file. The coordinator rejects a hand-back with a dirty tree.
- **Escalation packet:** when Codex fails the same problem 2–3 rounds, stop it and give a **fresh** Opus
  session only: (a) the failing command and its output (trimmed to the relevant part), (b) the file paths
  involved, (c) one line per attempt already made and why it failed, (d) the branch/commit to start from.
  No transcript dump. The Opus result goes back to Codex as a design/fix note, or Opus commits the fix
  itself if it is small.

## Independent acceptance (maintainer, 2026-11-16)

Agent-written unit tests share the author's understanding, and Codex reviewing Codex shares blind spots.
Acceptance therefore rests on checks whose expected values come from Python and whose authors did not write
the Go code.

1. **Domain differential, black-box.** Per domain, a **fresh Codex agent that does not port in that domain**
   writes the Flask↔Go differential (pattern: `internal/httpx/difftest`). Its brief forbids reading
   `server/internal/**` implementation code (it may use only the difftest harness and the public server
   entry); cases are derived from the Python source, `parity/routes.tsv` and the frontend API calls.
   Its commits may only touch difftest/testdata paths (coordinator checks the diff).
   - **Step A — coverage matrix first:** `server/parity/difftest-<domain>.md`, one row per route: success
     case(s), failure cases (auth, validation, missing/empty values, conflicting writes), edge cases, and what
     is compared after writes (DB rows, generated files, Agent pushes). **Opus reviews the matrix against the
     Python source** (cheap; finds missed branches, background-task triggers, permissions) before any test
     code is written.
   - **Step B — tests:** implemented to the reviewed matrix; another non-porting Codex agent reviews the test
     code, mainly for comparisons that can never fail (over-broad normalisation, ignored fields, swallowed
     errors).
   - **Mechanical gate (coordinator):** every route in `routes.tsv` has ≥1 success and ≥1 failure case, or a
     written reason; a differential that never reported a difference on purpose-broken input is suspect
     (run once with a deliberately wrong Go response).
   - Differences found go back to the **porting** agent; the test agent never edits Go code.
2. **Production-data differential.** A copy of the production `configflow.sqlite3` (+ data dir) is loaded
   into Python and Go on the test server; every generated output (Mihomo/Surge/Loon/OxiDNS/router configs,
   subscription outputs, Agent payloads) and every GET API response is compared structurally. The snapshot
   stays on the test server (`~/cf-prod-snapshot/`, mode 700), is never committed, and nothing from it
   (hostnames, IPs, tokens, airport domains) goes into commits, PRs, briefs or reports — differences are
   reported by path/key only. Copying is read-only from the NAS; production is never written to.
3. **Coordinator verification adds `-race`** (`go test -race -count=1 ./...`) for every merge, and parsers
   (YAML, subscriptions, rules, node strings) get Go fuzz targets seeded from the Python test corpora; the
   coordinator runs each fuzz target for 60 s at verification.
4. **Opus spot review:** per domain 1–2 non-HR modules picked by the coordinator (prefer the largest or most
   stateful). A finding that is a *class* of error (not a one-off) triggers a sweep of that class across the
   whole domain by Codex.
5. **Lab end-to-end before the switch** (unchanged, needs the maintainer's go-ahead): real Agents,
   RouterOS/iKuai, HA on PVE VMs, Go and Python side by side for a period.

Even with all of this, the guarantee is "no difference on known paths and on the real data", not "no bugs".

## Model settings (2026-11-17)

- Codex: `codex/gpt-6.1-sol`, thinking `high`, mode `full-access` (maintainer's choice).
- Opus agents: `claude/claude-opus-5-5`, thinking **`medium`** since 2026-11-18 (maintainer: Claude quota too
  small; applies to every Opus task incl. HR reviews and designs, and to the coordinator), mode `bypassPermissions`.
  Was `high` 2026-11-17 → 2026-11-18.
- Non-HR independent reviews stay on Codex. (A switch to Gemini on 2026-11-18 was withdrawn the same day:
  Antigravity quota is also too small. Antigravity is not used for regular work.)

## Fixed assignment by risk (maintainer, 2026-11-16; supersedes the quota-based rules in "Division of work")

Who does a task depends only on its risk, never on remaining quota (quota cannot be observed and would make
the level of scrutiny depend on timing).

- **Always Opus:** structural designs (N7, N8, new packages/interfaces); **ports of HR modules** (R05,
  R07–R12, R14; agents publish / rule sync / DNS follow / DNS HA / Linux gateway / installers / upgrade);
  **reviews of HR modules** by a different, fresh Opus session than the author (R04 included, although
  Codex ported it); coverage-matrix reviews of the black-box difftests; spot reviews (1–2 non-HR modules
  per domain); reviews of core-engine refactors (S-rules, S-oxidns, later S passes); escalations.
- **Always Codex:** everything else — non-HR ports, S passes and native swaps per Opus design, difftests,
  non-HR reviews (never the author), the production-data differential.
- **Quota only sets the schedule.** Opus tasks wait in the Opus queue until a window has quota; they are
  never handed to Codex instead, and there is no `verified (codex)` / "Opus review pending" state any more.
  Meanwhile Codex keeps working on non-HR tasks. Max 2 Opus agents at a time, no nested sub-agents.
- **Opus queue order:** whatever blocks other work first (N7/N8 designs, matrix review of the next domain),
  then R04 HR review, then HR ports in dependency order, then spot/refactor reviews.
- When a window runs out mid-task, Opus agents stop with committed work + PROGRESS file (no half-finished
  hand-offs) and are resumed in the next window.

## Complete Opus task list with triggers (maintainer, 2026-11-16 — the full list; no per-phase asking)

The coordinator starts each item when its trigger fires, in the Opus queue order of "Fixed assignment by
risk" (blockers first). Every item is done by an Opus agent that did not author the work under review.

| # | Task | Trigger |
|---|---|---|
| O1 | N7 (YAML → yaml.v3) and N8 (remove jsonx) designs; later any new package/cross-package interface design | now (next window) / when a design is needed |
| O2 | Review of the production-data differential harness itself: comparison logic, ignore/normalise list, auth hook, netns isolation, self-test — a false pass here lets every later merge through | when `go-proddiff` hands back, before it becomes the merge gate |
| O3 | Storage + background-task design: SQLite write locking, task scheduler (22 Python threads → goroutines), cancellation and shutdown order, schema/version handling for future data changes | before the bulk of the agents domain is ported |
| O4 | HR reviews (fresh Opus ≠ author), first R04 | each HR module hand-back |
| O5 | HR module ports (R05, R07–R12, R14; agents publish / rule sync / DNS follow / DNS HA / Linux gateway / installers / upgrade) | prerequisites verified |
| O6 | Coverage-matrix review of each domain's black-box difftest, incl. streaming/long-running endpoints (live logs, SSE/WebSocket, progress polling, uploads/downloads) that request/response diffs miss | each matrix hand-back |
| O7 | Core-engine refactor reviews (S-rules, S-oxidns, later S passes / N swaps) | after merge |
| O8 | Spot reviews: 1–2 non-HR modules per domain; a class of error → Codex sweep | per domain, after half its modules are verified |
| O9 | Security review: auth/sessions/CSRF, static file serving (Go replaces nginx), path traversal, **injection into generated RouterOS scripts / install scripts / shell commands from user-entered names and addresses**, secrets in logs, third-party Go dependencies | after O3; again on the full tree before the switch |
| O10 | Performance and memory against the rewrite's purpose (prod Python: 146 MB RSS, idle CPU): idle RSS/CPU with the prod snapshot, `/api/agents` and generation load, goroutine/fd/memory over a 24 h soak, pprof findings → Codex tasks | when the config + agents domains run end to end; soak in the lab |
| O11 | Escalations (Codex failed 2–3 rounds) | when they happen |
| O12 | Behaviour-change ledger: every place Go intentionally differs from Python (fixed Python bugs, dropped features, format changes) collected from all PROGRESS files and checked for user-visible impact → release notes | before the switch |
| O13 | Feature-completeness final audit: `routes.tsv`, background tasks, every frontend API call, settings keys vs Go — by an Opus that ported nothing | before the switch |
| O14 | Switch and release plan: the maintainer's production cutover (checks, data compatibility, Agents' one full re-publish and how to verify it, stop-loss without Python rollback), **and the public release for other users of thsrite/configflow** (legacy-data refusal path, Docker image/arch/tzdata, upgrade notes), plus the lab test plan (needs the maintainer's go-ahead to run) | when O13 is clean |
| O16 | VM lab batches L1–L6 (section "VM lab tests" below) | each batch's trigger |
| O15 | Architecture overview for future contributors (`server/ARCHITECTURE.md`): packages, data flow, background tasks, how to add a route/generator | before the switch |

The maintainer is only asked for decisions that are theirs: lab test go-ahead, switch timing, user-visible
behaviour changes from O12, and anything touching production.

## Branch per agent, always (2026-11-16)

No agent works directly in the shared worktree `~/program/configflow-go` / on `go` any more: it blocks
fast-forward merges whenever another branch touches the same files (S-oxidns vs the N2 finish, 2026-11-16).
Every agent gets its own Paseo worktree branch off `go`; only the coordinator moves `go`.

## VM lab tests — approved by the maintainer (2026-11-16)

The maintainer approved running the HR lab tests on the PVE lab without asking per test. Executed by **Opus**
(added to the Opus list as **O16**; PVE .6 also hosts production guests, so this is not Codex work), after
each batch's modules are ported and verified, in this order:

| Batch | Covers | Needs |
|---|---|---|
| L1 | A08–A10 installers (Debian 12/systemd + Alpine 3.24/OpenRC guests, Docker host/bridge/macvlan, Compose bundle/direct/compact, stale links), A15 web upgrade/rollback, legacy protocol-1 update, address follow on a real DHCP renumbering — plans in PROGRESS-agents A08/A09/A10/A15 | now (Go serves those routes) |
| L2 | A01+A14 publish / activate / rollback / recover; A11 bundle on real guests | A14 ported + verified |
| L3 | A16 rule sync, A17 DNS follow, A18 DNS HA (keepalived VIP pair) | those modules verified |
| L4 | A19–A22 Linux gateway modes, A23 IPv6 readiness against a lab RouterOS | those modules verified |
| L5 | routers R04/R05/R07–R14 against a lab RouterOS CHR and a lab iKuai VM (publish, policy routing, return routing, DNS, IPv6) | those modules verified |
| L6 | O10 24 h soak (RSS/CPU/goroutines/fds with the prod snapshot imported and devices repointed to lab guests) and the O14 switch rehearsal | before the switch |

Safety rules for every batch (from the lab notes in memory and earlier labs):
- PVE .6 only, isolated bridge `cfgt` (10.97.0.0/24, nft `inet cfgt` drops lab→private LANs); new guests only,
  recorded in an owner file under `/root/cf-gwip-lab/` before they are started; **never touch** VM 101
  (ros-chr), VM 107 (oxidns), CT 105 (mihomo) or any guest not recorded as a lab guest by this work.
- If the prod snapshot is imported into a lab panel, repoint or disable every router, Agent, backup and
  subscription before anything can publish; the lab panel must not reach production devices.
- Each batch ends with a short report in `server/parity/lab/L<n>.md` (what ran, results, defects → tasks)
  and the guests are destroyed afterwards unless the next batch reuses them.

## Production snapshot refresh (from the O2 review, 2026-11-17)

Time-window routes (metrics/traffic history: 1 h, 24 h, 7 d) compare empty results once the snapshot ages, so
the coordinator refreshes `~/cf-prod-snapshot/` on the test servers (read-only copy from the NAS: tar of the data
dir minus logs + SQLite online backup via the container's Python, mode 700, never committed) **weekly** and before
the switch. After a refresh, new differences are expected to surface; triage them like any finding (fix Go or
baseline with a reason). Keep the previous snapshot until the new baseline is settled, then delete it. The
harness proves egress isolation at start (only lo, no host sockets, DNS fails, TEST-NET unreachable) — never
weaken that check.

## HR modules: Codex ports, Opus reviews (maintainer, 2026-11-17; supersedes "HR module ports by Opus" in "Fixed assignment by risk" and O5)

Reason: R04 (Codex port + Opus review) showed the review catches what Codex misses (10 findings, 2 high, all fixed);
the issue classes are now written down; porting is the most token-heavy step and HR ports were the bottleneck.

- **Codex ports HR modules.** The brief must include: the issue-class checklists from `server/parity/review-r04.md`,
  `review-r05.md`, `review-a19.md`, `review-r06.md` and later `review-<module>.md` files (when present), `server/design/o3-concurrency.md` (locks, Spawn, cancellation /
  PORTING 4b) and `server/design/n7-n8.md` (typed model rules), the relevant rows and lab items of the domain's
  difftest matrix, and the requirement to state for each checklist class how it is avoided.
- **Opus first writes a short design** (interface, state transitions, failure/rollback, buildable steps, acceptance;
  template in "Design docs, hand-offs and escalation") for the most complex HR modules: **A14 publishing, A18 DNS HA,
  R12 router publish**. Codex implements to it.
- **Every HR module gets a fresh-Opus review** (never the author); it stays `ported` until that review sets `verified`.
- **Fallback:** if a review finds **two or more high-severity findings** in a module, the remaining HR modules of that
  area go back to Opus porting until the coordinator decides otherwise.
- R05 and A19 (already ported by Opus) finish their Opus reviews as started.
- **Two standing checklist items (R06 review, 2026-11-17):** (1) a test double must never override a production
  parameter it is meant to exercise (R06's fake client forced a 200 ms timeout and hid a missing 10 s write timeout);
  (2) at least one test per device/HTTP client compares the raw request (body bytes and headers), not only the decoded
  content (R04's client sent a JSON `null` body on every GET/DELETE and both fakes accepted it).
- **Third standing item (R07 review, 2026-11-17):** a change to a shared driver path (request, redaction, parsing) is
  tested on the *merged* tree with the other modules' tests of that driver (R07's private redaction in R04's shared
  `request` broke two verified R06 tests). Credential redaction hides only passwords/tokens, never usernames, and uses
  `internal/redact` — this class was found twice (R06 F2, R07 F1).
- **Fourth standing item (A24 F2, C32 review high, 2026-11-18):** `lifecycle.Detach` keeps the shutdown cancellation,
  but a fast owner path that never looks at it still commits after shutdown. Every write owner checks `ctx.Err()`
  after Detach and before each commit point (repo transaction, file replace, device write), and leaf stores without a
  ctx get one. Test with a real `Runtime.Start/BindRequest/Shutdown`: after shutdown no write lands; after a request
  disconnect every write still completes.

## Merge batching and risk tiers (coordinator, since 2026-11-18)

Approved by the maintainer to cut gate time without reducing what is verified.

- **Batch merges.** Branches that are ready (reviewed, targeted tests green) are stacked on top of `go` in
  one batch tree (`batch-N`, each branch rebased in turn; `.md` conflicts keep both sides) and the full gate
  (`cf-gate`) runs once on that tree. On PASS the whole batch fast-forwards into `go`. On FAIL the failing
  package normally names the branch; that branch is dropped from the batch and handed back, and the rest is
  re-gated (or gated separately when the failure cannot be attributed). Every commit that reaches `go`
  therefore still passed a full gate on the exact tree it lands in.
- **Risk tiers.** Commits that change only documentation or test tooling (`server/parity/**`, `*.md`,
  `server/tools/configdiff/**` and similar, no Go product code and no `server/scripts/gate.sh` logic that the
  gate itself depends on) may be fast-forwarded after their own checks (e.g. configdiff self-tests).
  Anything that touches Go product code, even one package, goes through a batch full gate.
- Agents keep handing back with targeted evidence when the gate queue exceeds 15 minutes; the coordinator
  decides which batch a branch joins.

## Leaner verification (maintainer, 2026-11-18)

The rewrite was spending most of its time and tokens re-verifying the same things. Product behaviour rules
are unchanged (Python parity, approved behaviour changes, HR review, non-HR independent review, full batch
gate before every merge). What changes is how much each agent re-runs:

1. **No duplicate runs.** A reviewer does not re-run the author's full comparison suites. It reads them, runs
   the tests for its own fixes, and spot-checks: a few author cases per risk area plus any case it doubts.
   The batch gate re-runs everything anyway.
2. **No per-module mutation gates.** Do not build "N/N mutations detected" scripts per module. A negative
   control is still required where a test could pass vacuously (an index or golden that might be ignored, a
   guard that might never fire) — one targeted control, not a mutation suite.
3. **Python comparisons cover behaviour boundaries, not exhaustive input matrices.** Compare the normal
   paths plus one representative case per error class (malformed JSON, non-object body, wrong type, missing
   field, conflict, cancellation). Do not enumerate every shape x every engine. When an error class has a
   per-engine or per-route difference in Python, cover each distinct behaviour once.
