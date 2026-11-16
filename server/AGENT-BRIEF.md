# Brief for every agent on the Go rewrite (Claude or Codex)

Read this first, then `server/PORTING.md` (rules), `server/WORKFLOW.md` (process, native swaps, notes) and
the `server/parity/PROGRESS-<domain>.md` section of your task. Reply/report in the language you are
asked in; the maintainer reads Chinese.

## Environment (mandatory)

- **Never run tests, builds, `go vet`, linters or dev servers on this Mac.** Run them on the test server
  `192.168.0.32` with `rt`: `rt -- bash -lc 'cd server && go vet ./... && go test -count=1 ./...'`.
  `rt` syncs the git root of the current directory (respecting .gitignore) to `~/rt/<path>` on the server,
  runs the command there and returns its exit code. `rt --back <path>` copies a file back. Python for
  reference outputs on the server: `~/cf-test/.venv/bin/python` (run with `PYTHONPATH=.` from the repo root).
- Only light work locally: editing, git, reading files, grep, scripts that finish in seconds.
- **Never run `rt clean` in `~/program/configflow-go`** (the shared worktree); only inside your own
  private worktree right before deleting it.
- **Never touch production**: no SSH or HTTP to 192.168.0.105 (NAS panel), routers, DNS hosts
  (192.168.0.2/.11/.12), PVE hosts, or any home device. Tests use loopback fakes only.
- **No real hostnames, IPs, tokens or airport/subscription domains** in code, tests, corpora or commit
  messages. Synthetic values only (example.com, 192.0.2.0/24, 198.51.100.0/24, 2001:db8::/32).
- No pushes (`git push`) and no PRs: the coordinator verifies and pushes.

## Git

- Branch `go` is the integration branch. Codex agents work in their **own worktree on their own branch**
  created from `go`; finish by rebasing onto the latest local `go` (`git rebase go`), make sure every
  commit builds, and report the branch name and commit ids. The coordinator verifies and fast-forwards `go`.
- If you must work in the shared worktree `~/program/configflow-go`: commit with explicit paths only
  (`git diff --cached --stat`, then `git commit -- <paths>`), never sweep others' staged files, never move
  HEAD with `reset`/`update-ref`.
- Commit messages: `go(<area>): …`, English, ending with
  `Co-Authored-By: <your model name> <noreply@…>` as your tool normally does.

## Rules in one paragraph (details in PORTING.md)

Python is retired; the goal is idiomatic Go with every feature kept. Port features, not code. Go-native
types and libraries only (typed structs + `encoding/json`, `yaml.v3`, `net/url`, `net/netip`, `regexp`/
`regexp2`, `x/net/idna`). **Hard rule: no Python emulation** (no ports of CPython/PyYAML/requests/stdlib
behaviour, no emulated exception classes or texts, no `any` parameters mirroring Python call shapes, no
private copies of shared helpers). No size budget. Must match Python: features, routes, status codes,
JSON fields/values, Chinese user-facing messages byte for byte, stored-data compatibility, Agent protocol,
semantics of generated configs and of what is written to routers. Python bugs are fixed in Go (list them).
Acceptance is at the external boundary: Flask↔Go HTTP differentials, generated outputs compared
structurally against real Python, storage round trips, loopback fakes for devices.

## Style, hand-off, escalation

Follow the Go style section of the root `AGENTS.md` ("Go 后端（`server/`）代码风格"). Never stop with
uncommitted or non-compiling code: commit what compiles, revert the rest, write the remaining steps into your
PROGRESS file. If you fail the same problem 2–3 rounds, stop and report an escalation packet: failing command
+ trimmed output, file paths, one line per attempt and why it failed, branch/commit (WORKFLOW.md, last section).

## Reporting

At the end (and when your context gets long) update your PROGRESS section: status, commits, decisions,
findings, what is left. Final report: what you did, findings with severity, functional differences (should
be none), exact `rt` commands with their final output lines, branch and commit ids.
