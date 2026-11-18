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
- **Two equivalent test servers** (same setup: Arch, Go, `~/cf-test/.venv` Python 3.11 with identical packages,
  prod snapshot for proddiff, `ssh pve` to the lab): `192.168.0.32` (12 cores) and `192.168.0.64` (Hyper-V VM with
  its own kernel and TUN, 16 cores; replaced the retired WSL `.63` on 2026-11-17). Without `RT_HOST`, `rt` picks the
  less-loaded one for every run that syncs and prints `rt: running on <host>`; `--no-sync`/`--back`-only reruns reuse
  the host it last picked for this worktree (`rt host` shows it). Both are valid for iteration **and** for the final
  hand-back run (full suite, `-race`, proddiff); the hand-back report names the host. When several commands must hit
  the same copy (e.g. `rt -- …` then `rt --no-sync -- …` while another agent shares the worktree), pin it with
  `RT_HOST=sunbroke@192.168.0.32` (or `.64`). A timing-sensitive failure seen on one host only is re-checked on the
  other before you call it a bug.
- **You do not run the full gate (since 2026-11-18).** The coordinator runs `gate.sh` / `cf-gate` on batch trees
  (WORKFLOW.md, "Merge batching"). You run only targeted checks for what you changed: `gofmt`, `go vet ./...`,
  `go test -race` on the packages you touched and their direct callers, the related difftest/configdiff groups,
  and your new fuzz targets for 60 s. Then hand back with the exact commands and last output lines. Waiting on a
  full gate burns many tokens per poll and was the largest cost of the rewrite so far.
- **Full gate script:** `server/scripts/gate.sh [-p N] [FuzzName:./pkg ...]` runs gofmt+vet, full `-race` and proddiff in
  parallel, all difftest packages in one `go test`, and the given fuzz targets concurrently (60 s each). Agents no longer use it for hand-back (see above); for reference: `rt -- bash -lc 'server/scripts/gate.sh -p 6 FuzzX:./internal/x'` (`-p 6` on .64). The separate
  non-race full run is no longer required (the race run executes every test).
  gate.sh runs at most 1 agent gate per server at once (the coordinator may add one more) and queues the rest. Never run a full `-race` or proddiff
  outside gate.sh; while iterating, test only the packages you changed. A proddiff difference seen only under high
  load (check `/proc/loadavg`) is re-run at a quieter time before it is reported as a finding.
  **gate.sh reading the prod snapshot is always allowed**, even when your task says "synthetic data only": proddiff
  reads the offline `~/cf-prod-snapshot` on the test server and never connects to production. That rule is about
  what *you* write: your fixtures and test cases stay synthetic, and nothing from the snapshot is copied into code,
  tests, docs or reports (report only the proddiff summary: new / stale / self-check).
  **Queue limit:** if your final gate waits for a slot more than 15 minutes, stop it and hand back with the targeted
  runs you did; the coordinator runs the full gate (`cf-gate`) on the merged tree. Long gates may be moved to the
  background by your tool; keep following them in the foreground (poll the log) and do not end the turn early.
- **Temp space:** `/tmp` on both servers is a small tmpfs (7.8 GB on .64) that concurrent Go builds filled
  ("disk quota exceeded"). `rt` now sources `~/.rt-env`, which sets `TMPDIR`/`GOTMPDIR` to `~/.cache/rt-tmp` on the
  home disk; do not write large files to `/tmp` yourself.
- **Memory:** `.64` has 20 GB + 20 GB swap since 2026-11-18 (`.32` 31 GB). Before that, parallel full `-race` runs
  got test binaries OOM-killed on .64 (`signal: killed` = OOM, not a test failure — check `sudo dmesg -T | grep -i oom`).
  On .64 run full `-race` / gate.sh with `-p 6`; if a package dies with `signal: killed`, re-run it before reporting.
- **Real network stack** (Docker/Compose host/bridge/macvlan, iptables/nftables, policy routing, TUN, keepalived/VIPs,
  network namespaces, installer scripts in containers) runs on either server; never on the Mac. Real multi-host
  network behaviour (macvlan HA, VIP failover) belongs to the PVE lab batches, driven from either server (`ssh pve`).
  Run full `-race` (`go test -race ./...`) only once at the end; while iterating use `-race` only on the
  packages you changed.
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

## Concurrency

Locks, background loops, detached jobs, cancellation and shutdown: `server/design/o3-concurrency.md` (no new
`flock`, no bare `go` for work that outlives its caller, loop timings from its table 2.4).
`internal/lifecycle/gate_test.go::TestConcurrencyFacadeBoundary` checks all non-test Go sources:
Flock only in repo/instance, explicit file:function exemptions for joined fan-outs and wall-clock primitives.
Use `SpawnerFrom(ctx)` to preserve an originating Runtime; do not add broad exemptions to bypass Spawn/Clock.

## Style, hand-off, escalation

Follow the Go style section of the root `AGENTS.md` ("Go 后端（`server/`）代码风格"). Never stop with
uncommitted or non-compiling code: commit what compiles, revert the rest, write the remaining steps into your
PROGRESS file. If you fail the same problem 2–3 rounds, stop and report an escalation packet: failing command
+ trimmed output, file paths, one line per attempt and why it failed, branch/commit (WORKFLOW.md, last section).

## Verification scope (2026-11-18)

Follow WORKFLOW.md "Leaner verification": reviewers do not re-run the author's whole suites, no per-module
mutation gates, and Python comparisons cover normal paths plus one representative case per error class.

## Messages to the coordinator

Send a message only when (a) you need a decision, (b) you hand back, or (c) you are blocked. Do not send
progress updates ("still running", "package X passed") — each one wakes the coordinator with a long context.
Put progress into your PROGRESS/report file instead.

## Never end your turn while waiting

**Concretely: run every long test as ONE blocking foreground command with a long timeout (e.g. 3600000 ms) and
read its result when it returns. Do not poll a log in a loop (each poll is a full model turn), do not start it in
the background, and do not use a Monitor/until-loop and then end your turn — two Opus reviewers stalled that way
on 2026-11-18.** If a tool moves a long command to the background anyway, wait for it with a single blocking
`wait`/`tail --pid` style command, not repeated checks.

Run tests and other long commands in the **foreground** with a long timeout (the test servers can be slow
under load). Background-job notifications do not reliably wake an agent in this setup: if you end your turn
saying "waiting for the run", nothing resumes you and the task stalls. Only end your turn when the task is
finished (or you are blocked on a question to the coordinator).

## Reporting

At the end (and when your context gets long) update your PROGRESS section: status, commits, decisions,
findings, what is left. Final report: what you did, findings with severity, functional differences (should
be none), exact `rt` commands with their final output lines, branch and commit ids.
