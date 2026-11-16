# Go rewrite — handoff for the coordinating session

**Since 2026-11-15 the work follows `server/WORKFLOW.md` (domains, module split, adversarial review,
domain differential, `PROGRESS-<domain>.md`). Where this file's "How a package is run" differs, WORKFLOW.md wins.**

Read this first when you pick up the Go rewrite in a new session. It is written for the coordinator
(the session that briefs, verifies and records work packages), not for the porting agents; they read
`PORTING.md`.

## Goal and standing decisions (maintainer, 2026-11-14)

- Rewrite the Flask backend (`backend/`, ≈86k lines without tests) in Go, on branch `go` of the fork
  (sunbroke555/configflow), worktree `~/program/configflow-go`. One full rewrite, no Python/Go
  coexistence. The maintainer is the project's maintainer; no upstream consent needed.
- **No lost features.** Every route in `parity/routes.tsv` (380 method+path pairs) and every background
  task must exist in Go. A gap found in a hand-back is sent back to the agent, not accepted.
- Not ported (decided): `backend/models/`, Python runtime tuning (`startup.py`, `http_server.py`,
  `http_queue.py` — Go-native tuning instead), pre-SQLite data migration (Go refuses legacy-only data
  dirs and asks to upgrade via the last Python release), the one-time managed-DNS cleanup, and any code
  without a caller. Details at the end of `parity/packages.md`.
- Subscriptions are parsed by the bundled Sub-Store; Go keeps Sub-Store and ports only its client (#13).
- Follow-up after parity (not during the port): Surge/Loon manual nodes via Sub-Store; listed in
  `parity/packages.md`.
- **All subagents use Opus 5.5** (`model: "opus"`). No per-risk model mix.
- Production (NAS panel, routers, DNS hosts) is never touched. High-risk (HR) packages get code and tests
  only; lab tests on PVE VMs happen after the maintainer agrees.

## Where things are

| File | What |
|---|---|
| `server/PORTING.md` | Rules every porting agent follows + API reference of every finished package. Update it after each verified package. |
| `server/parity/packages.md` | 41 work packages, dependency order, status (`todo`/`wip`/`done`/`lab`), not-ported list, follow-ups. |
| `server/parity/routes.tsv` | Every Flask route; agents fill `go package` + `status` for their rows. |
| `server/parity/progress/<N>-*.md` | Per-package progress file kept by the porting agent at every commit; the restart point after an interruption. |
| `server/internal/...` | Go packages. `cmd/configflow` is the binary. |

## Status at handoff (2026-11-15 01:15)

Done and independently re-verified by the coordinator: #0 jsonx+store, #1 infra (yamlx, logx, …),
#2 repo, #3 httpx/auth/settings (+ the Go server replaces nginx), #4 rules (regex: RE2 + regexp2
fallback). Routes done: 22 of 380.

In progress when this file was written (the agents die if the session that started them ends; their
uncommitted files stay on disk):
- **#5 subs**: functionally done and verified; follow-up running to reduce refusals of the
  comment-preserving Mihomo YAML editor (realistic corpus of 128 repo configs already 0 refused /
  0 mismatches, commit f233dcb6; random corpora still refused 335/336 rows). Uncommitted at handoff:
  `subs/commented/*.go`, `numbers.go`, `testdata/commented_scalars.jsonl.gz`, `testdata/gen.py`, and a
  debug file `subs/zz_debug_test.go` that must not be committed.
- **#20 agent registry**: started 00:55, uncommitted `server/internal/agents/` at handoff.

If those agents are gone when you resume: inspect `git status`, run the package tests (below), and brief a
new Opus agent to finish from the files on disk (tell it what is committed, what is not, and what its brief
was — the briefs are reproduced in spirit by the template below).

## How a package is run

1. Pick the next `todo` package whose prerequisites are `done` (`parity/packages.md`). Run **at most 2
   agents at a time** (more burns the 5-hour usage window within an hour).
2. Brief a `general-purpose` agent with `model: "opus"` using the template below; mark the package `wip`.
3. When it hands back: re-run its tests yourself on the test server (commands below), including its
   Python differential/cross-check; read its "not ported / differences" list critically (lost features →
   send it back with `SendMessage`, which resumes the same agent).
4. When verified: paste its API reference into `PORTING.md` (above "Helpers from a package that is not
   ported yet"), set `done` in `packages.md`, commit (`go: …`, explicit paths) and `git push origin go`.
5. If an agent is cut off by the usage limit: after the reset, **always resume the same agent with
   `SendMessage`** (maintainer's choice, 2026-11-15: continuity over usage cost). Start a fresh Opus agent
   from `server/parity/progress/<N>-*.md` only when the old agent can no longer be resumed (e.g. the
   session that started it has ended). Agents keep the progress file anyway, for that case.

Brief template (adapt the package part; the agents already know the rest from PORTING.md):

> You are porting one work package of a Python (Flask) backend to Go. Worktree `~/program/configflow-go`
> (branch `go`). **Read `server/PORTING.md` completely first** (rules + API reference of the packages you
> build on), then `server/parity/packages.md` (your package is #N) and `server/parity/routes.tsv`.
> Package #N: <name>, Python sources <files> (read completely before writing). Scope rules: <what to
> port, what belongs to later packages behind hooks/interfaces, which routes to register>. Tests: port
> every Python test whose subject is in scope (<known files>), list the rest with the package they belong
> to; add a differential check against real Python on the test server (corpus via
> `PYTHONPATH=. ~/cf-test/.venv/bin/python`, or the Flask↔Go HTTP pattern in `internal/httpx/difftest`;
> synthetic values only, loopback only). Process: commit early and often with explicit paths, `go(<pkg>): …`,
> no push, no sub-agents, tests only via `rt`, only your packages; only edit `routePackages`/`wirePackages`
> in `internal/server/server.go` outside your package. Hand back: PORTING.md format + API reference,
> routes done/left, differences from Python, exact server commands and final output lines.

## Verification commands (run from the worktree; never on the Mac)

```
rt -- bash -lc 'cd server && gofmt -l internal/<pkg>; go vet ./internal/<pkg>/... && go test -count=1 ./internal/<pkg>/...'
# Python cross-checks that are env-gated:
rt -- bash -lc 'cd server && CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(cd .. && pwd) go test -count=1 -v ./internal/store/ ./internal/repo/'
rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Differential ./internal/httpx/difftest/'
```

Use `rt --back <path inside one package>` only; never `--back server/internal` (it overwrote other agents'
files once). After `cf-test`, use `rt --no-sync`.

## Lessons so far

- Usage: each package agent costs 300–500k tokens; parallel agents and agents that fork helpers exhausted
  the window twice. A very long coordinator context makes every wake-up expensive: start a fresh
  coordinator session from this file rather than continuing an old one.
- Agents on Sonnet once shipped "never matches" for unsupported regexes (a lost feature) — always read the
  differences list.
- Agents' hand-backs overstate nothing so far, but run their tests yourself anyway; they sometimes leave
  gofmt or debug files behind.
- Event-driven is enough: hand-backs and completions wake the coordinator. A recurring cron is only a
  fallback for resuming agents after a usage-limit reset (session-only, expires after 7 days).

## Coordinator habits (keep them)

- **Size gate (2026-11-16):** on every hand-back, measure Go non-test lines of the touched packages
  yourself (`find <pkgs> -name '*.go' ! -name '*_test.go' | xargs cat | wc -l`) against the module's Python
  lines; over 1.5× → send it back (target 1.2×). Also grep the diff for Python-shaped code
  (`any` parameters mirroring Python, `PyError`/emulated exceptions, new `jsonx`/`pystr`/`urlx` uses,
  private helper copies). Track the whole-tree total after each push; target at the end ≈ 100–130k lines.

- After each hand-back, re-run the module's tests and differentials yourself on a throwaway detached
  worktree of HEAD (others' uncommitted work often breaks the shared tree):
  `git worktree add -q --detach ~/program/configflow-go-verify HEAD && cd ~/program/configflow-go-verify &&
  rt -- bash -lc 'cd server && go vet ./... && go test -count=1 ./...'`, then `rt clean`,
  `git worktree remove --force …` **and** `rm -rf ~/program/configflow-go-verify` (macOS leaves `.DS_Store`),
  `git worktree prune`.
- After a module changes shared packages (httpx, urlx, yamlx, pystr, repo, agents client), run the whole
  project (`go test ./...`), not just the module.
- Recount the PROGRESS header from the module table after status changes (porters often leave it stale).
- `git push origin go` sometimes fails on the network: retry (`for i in 1 2 3; do git push -q origin go && break; sleep 5; done`).
- The git index is shared by all agents: commit with explicit paths only (`git commit -- <paths>`); check
  `git diff --cached --stat` first. Agents have accidentally committed each other's staged files three times.
- Usage limit: about 6–7 agent runs per 5-hour window with 4 agents in parallel. When agents fail with the
  limit, resume each with `SendMessage` after the reset time (the user usually says "继续"); the session
  cron fallback has not been reliable.
- All agents `model: "opus"`; briefs forbid sub-agents and background jobs; reviewers set `verified` only
  with zero open findings; the coordinator only confirms by re-running.

## Non-rewrite items still open (2026-11-16)

- Upstream PRs from 2026-11-14 that were open when last checked: #573 (install script preview scroll),
  #574 (Agent address follow), #575 (draft, stacked on #574: write-back + 修改地址 dialog). Check their
  state before touching them.
- DNS01 (192.168.0.11) still has `/opt/configflow-agent/config-oxidns.json.bak-before-ip-change-20261114`;
  delete only after the maintainer confirms.
- VM lab tests proposed by reviewers, all waiting for the maintainer's go-ahead: installers (A08 plan),
  Agent web update/rollback and address follow on real DHCP renumbering (A15 plan), Docker HA (A09/A10),
  and later publish/HA/router modules.
- Decided not to fix in Python (being retired): trailing-newline `re.match` validation bugs — fixed in Go only
  (PROGRESS-agents decision 6).
