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

## Current state (2026-11-16 night — read this first)

Rules changed a lot on 2026-11-16; the authoritative versions are the top block of `PORTING.md`, `AGENT-BRIEF.md`
and the last sections of `WORKFLOW.md` ("Native-library swap", "Idiomatic Go", "Switch checklist",
"Division of work"). In short: Python is retired; idiomatic Go; Go-native types/libraries; acceptance at the
boundary; **no size budget; the hard rule is no Python emulation**; Python bugs are fixed in Go; one full
re-publish at the switch is accepted.

**Division of work:** Codex via Paseo (`codex/gpt-6.1-sol`, thinking `high`, mode `full-access`) does most work
in its own Paseo worktree branched off local `go` (ends with `git rebase go`); Claude Opus does all structural
design, HR reviews, concurrency problems and escalations (max 1 Opus agent). Coordinator = a Claude session:
dispatch, verify (detached worktree + `rt`, emulation gate), fast-forward `go`, push.

**Running Codex agents (Paseo, project prj_b0033e1859522497):** check with `mcp__paseo__list_agents` /
`get_agent_status`; their finish notifications go to the session that last prompted them (the old coordinator). On takeover,
first `get_agent_status` each one; then re-attach: `send_agent_prompt` to each still-running agent with
`notifyOnFinish: true` and a harmless steer ("协调会话已更换，任务不变，继续做，完成后照常报告。") so its finish
notification comes to you. Agents already idle: read their final message (`get_agent_activity`) and handle
the hand-back. If no notification arrives within ~3 h, check status once more.
| agentId | task | workspace / branch |
|---|---|---|
| 954058ca-002e-424d-b246-6447e66a3c3b | N2 review finish (uncommitted fixes in the shared worktree) | wks_b5cc6440618e29f7, `~/program/configflow-go` (branch `go`) |
| 245ed359-780e-49cd-b5df-7fb79c356bf6 | S-rules adversarial review | wks_d2891b3794d213be, branch `go-review-srules` |
| c5332a35-f886-4d2d-90e2-f48496728a68 | S-oxidns adversarial review (first: non-ASCII zone rows) | wks_b086971f263e9706, branch `go-review-soxidns` |
| 5ced41c3-a748-42b3-8aa9-9350ae2be0ef | R04 RouterOS driver port (on `internal/devhttp`) | wks_1b9579c1f761857d, branch `go-port-r04` |
| 48fb0e89-9128-4cc3-8c65-c1eaac084f03 | Production-data differential harness (queue item 1; snapshot on test server `~/cf-prod-snapshot/20261116/`, copied read-only 2026-11-16) | wks_66a1e12807e372cb, branch `go-proddiff` |
Worktrees of the branch agents: `~/.paseo/worktrees/302rx1dg/<branch>`.

**When one finishes:** create a detached worktree of its branch (or of `go` after fast-forward), run
`rt -- bash -lc 'cd server && gofmt -l internal/; go vet ./... && go test -race -count=1 ./...'`, grep the diff
for Python emulation, then fast-forward `go` (`git merge --ff-only <branch>` in `~/program/configflow-go`
when its tree is clean, otherwise rebase the branch again) and push. R04 then needs an HR review (Opus, or
Codex first + "Opus review pending" if Claude quota is out).

**Queue (reordered 2026-11-16 by the maintainer: boundary oracle first):**
1. **Production-data differential harness** — next free slot, before anything else; once it exists it is a
   merge gate for every change (WORKFLOW.md "Independent acceptance" 2). Needs a read-only snapshot copy
   from the NAS onto the test server.
2. **Black-box difftest for the config domain** (coverage matrix → Opus matrix review → tests → non-porting
   Codex review), to protect the refactors that follow.
3. Allowed meanwhile (do not touch accepted code): new module ports (R05 iKuai, C27 after S-oxidns review,
   other `missing` modules whose prerequisites are verified) and reviews.
3b. **Known emulation debt for the S passes** (found at the N2 merge, 2026-11-16): `internal/provider`
   (`pyError` type with Python exception classes in errors.go/delivery.go/response.go — N2 added two more
   `ValueError` uses in delivery.go) and `internal/convert` (mihomo_generate.go, exports_mosdns.go). The S pass
   of those packages removes them (plain Go errors with the Chinese messages the API returns).
4. **Blocked until 1 exists:** N7 (YAML → yaml.v3 behind yamlx) and N8 (remove jsonx) Opus designs and their
   implementation, S passes of convert/subs/agents/repo, and any other large refactor of accepted code.
**Opus task list O1–O15 with triggers:** WORKFLOW.md "Complete Opus task list" — start each on its trigger without asking.
**Fixed assignment by risk:** see WORKFLOW.md last section — Opus always does designs, HR ports, HR reviews,
matrix/spot/refactor reviews; Codex everything else; quota only sets when Opus tasks run (never handed to Codex). Max 2 Opus agents.
5. S-rules and S-oxidns (already running) merge normally, then get re-checked with the production-data
   differential as soon as it exists.
Claude subagents of the old session (S-rules, S-oxidns, N2 review, R04 Claude porters) are finished/paused —
don't resume them; their work continues in the Codex agents above.

**Decided 2026-11-16 (maintainer accepted all 3):** additions — (1) a Go style section in the `go` branch's
root `AGENTS.md` (naming, `%w` errors, no panics, ctx first, doc comments, table tests, Chinese messages as
constants, `logx`, gofmt/vet); (2) a fixed design-doc template (interface contract, state transitions,
buildable steps, acceptance) for Opus designs, and no handoff of half-finished uncommitted code; (3)
escalation packet when Codex fails 2–3 rounds: failing output + file paths + one-line list of failed
attempts, to a fresh Opus session. Applied: root `AGENTS.md` style section, WORKFLOW.md last section, AGENT-BRIEF.md pointer.

**Other:** the 2-hourly fallback cron was deleted (pause). An older coordinator session ("configflow-40")
was told not to resume anything. Last pushed `go`: 39d01b51 (later local commits: 73d39c55 AGENT-BRIEF,
bd0c9735 + e62eb5c7 division of work, this HANDOFF update).

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

Test servers: `.63` (WSL2 on the maintainer's PC, port 2222 via `~/.ssh/config`) for agents' iterative runs;
`.32` (native Linux) for every authoritative run — agents' final hand-back run and **the coordinator's merge
verification + proddiff gate always on .32** (WSL can be interrupted and differs from production Linux).

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

- **Emulation gate (2026-11-16):** on every hand-back, grep the diff for Python emulation (ports of
  CPython/PyYAML/requests/stdlib behaviour, `PyError`/emulated exception texts, `any` parameters mirroring
  Python call shapes, new `jsonx`/`pystr`/`urlx` uses, private helper copies) → send back. No size budget
  (maintainer: line limits are meaningless); line counts are for information only.
- After each hand-back, re-run the module's tests and differentials yourself on a throwaway detached
  worktree of HEAD (others' uncommitted work often breaks the shared tree):
  `git worktree add -q --detach ~/program/configflow-go-verify HEAD && cd ~/program/configflow-go-verify &&
  rt -- bash -lc 'cd server && go vet ./... && go test -race -count=1 ./...'`, then `rt clean`,
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
- **VM lab tests approved by the maintainer on 2026-11-16** (batches L1–L6, Opus, WORKFLOW.md "VM lab tests"); originally proposed: installers (A08 plan),
  Agent web update/rollback and address follow on real DHCP renumbering (A15 plan), Docker HA (A09/A10),
  and later publish/HA/router modules.
- Decided not to fix in Python (being retired): trailing-newline `re.match` validation bugs — fixed in Go only
  (PROGRESS-agents decision 6).
