# O3-3 — instance ownership, startup and shutdown

Status: ported; all final `.32` checks complete. Branch `go-o3-3`, rebased onto `go@a44b343b`.
Required race/tag candidate `00126bf9` and N7-2 composition candidate `bcccb1d9` share the same O3-3
source as the final branch. No sub-agents, pushes or production-device access.
Coordinator successor: `bb75f5c0-8c87-410c-84cd-20801944f7e0`.

## Implementation and approved boundaries

- New `internal/instance` holds `.configflow-server.lock` and all four Python lifetime locks until
  Release. Busy locks retry at 500ms under main's 30s deadline. Failure rolls back partial ownership;
  ErrInUse retains the context cause and emits the centralized Chinese directory diagnostic.
  Server-file diagnostics contain pid/version/start-time; old Python lock bytes and files are preserved.
- `repo.ResolveDataDir` shares Open's existing home expansion/path resolution without opening storage.
  Main resolves before Acquire, then initializes logging and opens the repository. This fixes the
  high-severity home-alias ownership bypass found during implementation: raw `~/data` must never be
  locked while the repository opens the expanded home path. Absolute/home/symlink aliases share locks.
- `repo.PrepareConfig` prepares configuration without initializing the Agent manager; LoadConfig keeps
  its prior hook behaviour. Main explicitly registers Startup in order: load-config, existing prune,
  agent-manager-init, metrics-load-history; RunStartup completes before any listener is opened.
  Repeated Wire registration replaces specifications in place, preserving this order.
- Remove the three old lifetime flocks now (refresh, discovery auto, ranking history), as approved;
  otherwise instance ownership would exclude the Go process's own loops. Existing Loop timing remains
  30/30, 120/600 and 60/60. Periodic migration remains O3-4. Metrics startup/history loading and the two
  shutdown-save phases are already complete here; the O3-4 row records that early work.
- httpx notifies lifecycle after profile resolution and the auth gate. Rejected profiles/credentials
  do not count; authenticated requests and allowed public heartbeats do. This supplies the D3 trigger;
  it does not port the future Agent rule-sync worker itself.
- Configure injects concurrent API/web http.Server.Shutdown as Drain. First metrics save precedes drain;
  handlers/jobs drain together, remaining jobs cancel at grace−2s, cancellation waits at most 0.5s,
  then the final save runs. PhaseClose closes remaining HTTP connections, flushes logx and releases
  instance ownership. Native Shutdown workers and API Serve are joined. Second SIGTERM/SIGINT exits 0.
- Grace defaults to 8s under the unchanged 10s image stop budget; validate finite positive durations.
  Production has no loop-disable flag or env switch. Test callbacks and helper envs live in `_test.go`.
- Store, traffic and ranking SQLite handles are operation-scoped and already closed by their owners;
  there is no persistent process-wide sql.DB or repository Close API to fake.
- Preserve N8-2 safe corrupt-storage diagnostics in file+stderr at the main error boundary. Busy
  ownership failures only log to console. Both error-log and service paths use the shared resolver.
- Do not revive node-test-dns-migrate: users upgrade/start the final Python version first. This existing
  PORTING decision now has an explicit O12 entry. Coordinator clarifications are in design §2.3;
  developer notes are [o3-startup-shutdown.md](../../design/o3-startup-shutdown.md), outside user doc/.

## Regression evidence

- Real cross-process flock holders cover each of the five locks and all four Python locks together,
  context timeout, Chinese error text, retry, partial rollback, persistent file contents, diagnostics
  and concurrent idempotent Release. Real main refuses the Python-compatible holder at 30s before DB open.
- PrepareConfig defers manager initialization; LoadConfig still initializes it. Configure cannot
  replace a Runtime while its synchronous RunStartup callback is active.
- Isolated HTTP child tests prove profile/auth rejection does not trigger work, public heartbeats count,
  and later accepted requests do not repeat the first-request trigger.
- Every old loop executes under instance ownership with injected waits proving unchanged delays/intervals.
- Real `.32` main process race passes: idle SIGTERM 1.048s, publishing 1.087s, blocked HTTP drain 7.010s,
  second-signal exit 1.060s. Publishing writes a second metric after flush #1; flush #2 persists it.
  Every scenario permits ownership reacquisition after exit.
- Real home/symlink server-vs-server rejection passes at 31.43s/31.44s, each with outbound count 0.
  The clock and loop delays remain real; a recording/refusing refresh.Env covers every subscription,
  rule-source and userinfo I/O boundary of the only scheduler eligible in this 30s window. A positive
  test calls all three boundaries and proves the observer records/refuses them.
- Fixtures are legitimate empty stores created with repo.Open(Options{}), explicit empty resources and
  a synthetic Agent; real child main still uses OpenDefault. No template resources, product guard
  bypass, legacy-name whitelist or loop-disabling mode is used.

## Findings and integration

Fixed: raw-home path could lock a different directory from the DB; old three lifetime flocks would
skip Go loops under instance ownership; per-call HTTP drain must join its control goroutines.
No open production finding is known; independent Opus review is still required with O3-1/O3-2.

Fixed test setup errors: proddiff-tag child needed its own manifest path; editing/deleting referenced
production-template resources correctly failed repository guards. Two failed resource-edit attempts
were escalated under AGENT-BRIEF; coordinator approved the empty native-store fixture above.
Neither product reference validation nor loop behaviour was changed to silence those tests.

Rebase preserved new N8-2 main recovery logging and both behaviour-change ledger sections. The
integrated synthetic startup script passed all three corrupt JSON/UTF-8/large-value cases: exit=1,
safe file+stderr diagnostics and original DB bytes unchanged. Previous pre-alias candidate also passed
full/differential/proddiff gates; those results are superseded by the post-fix final run below.

## Commits

- `f1c2aa54`: instance core and cross-process locks.
- `f5811a36`: ordered startup, main drain/shutdown, request notification, early owner wiring.
- `2768a2f0`: real signal tests, grace validation and startup/Configure regression.
- `e936d427`: developer notes routed into server/design.
- `772ec753`: joined drain workers and private tagged process fixtures.
- `05987b68`: shared data-directory resolver and real alias regressions.
- `e8e3eeb5`: preserve integrated safe startup diagnostics.
- `67be1bf1`: approved empty fixtures and zero scheduled egress assertions.

## Final `.32` acceptance

Foreground, no background test job; all gates must complete before hand-back:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail
cd server
test -z "$(gofmt -l .)"
go vet ./...
go test -race -p=4 -timeout=30m -count=1 ./internal/lifecycle/... ./internal/lockx/... ./internal/httpx/... ./cmd/configflow ./internal/instance/... ./internal/repo
go test -tags=configflow_proddiff -p=4 -timeout=5m -count=1 ./cmd/configflow ./internal/lifecycle/...
go test -tags=configflow_difftest -p=4 -timeout=5m -count=1 ./cmd/configflow ./internal/lifecycle/...
go test -p=4 -timeout=30m -count=1 ./...
mapfile -t o3_diff_packages < <(go list -tags=configflow_difftest ./... | rg "/difftest$")
CONFIGFLOW_DIFF=1 go test -tags=configflow_difftest -p=4 -timeout=30m -count=1 "${o3_diff_packages[@]}"
o3_binary=$(mktemp)
trap "rm -f \"$o3_binary\"" EXIT
go build -o "$o3_binary" ./cmd/configflow
cd ..
PYTHONPATH=. ~/cf-test/.venv/bin/python server/scripts/review-n8-2-startup.py "$o3_binary"
~/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py
server/scripts/proddiff.sh'
```

Final candidate command exited 0:

- gofmt/vet: silent, pass.
- Required + instance/repo race: lifecycle3.139s, clock1.022s, lockx3.550s, httpx40.003s,
  cmd105.628s, instance7.779s, repo36.563s; all pass.
- Proddiff-tag main96.745s/lifecycle.064s/clock.010s; difftest-tag main96.563s/lifecycle.050s/
  clock.006s; all pass, including real signal and path-alias process cases.
- Full `-p=4`: PASS (cmd96.746s, importer1.502s, installer167.092s, oxidns89.398s,
  repo3.729s, yamlx1.049s); final package output `ok configflow/internal/yamlx 1.049s`.
- All nine then-present enabled tagged differentials pass: agents152.501s, subscriptions130.037s,
  discovery3.185s, health14.601s, httpx1.580s, mosdns2.344s, refresh5.055s,
  substore11.006s, webdav38.987s.
- Production binary startup script: `3 synthetic corrupt startup cases: exit=1, safe log+stderr,
  original database bytes unchanged`.
- Python proddiff tool tests: `Ran 31 tests in 0.024s`, `OK`.
- Tagged proddiff: all 23 self-checks PASS, 2831 cases/415 comparisons,
  coverage_regressions=0, unmapped_routes=0, baseline `new=0 stale=0`; 4 Startup names and 5 suppressed
  periodic loops. No comparator waiver or baseline rewrite. Existing 11 baseline differences remain.

Earlier all-touched-package race also passed metrics/refresh/discovery/monitor/substore. Full oxidns
race is not claimed here; the earlier slow-test issue was subsequently repaired upstream.
Non-test lines: instance0→121, cmd233→393. No dependency added.

## Latest-go composition acceptance

Rebased onto `go@fd731be1`, retaining all N7-2 and O3 ledger/progress entries. Only documentation
conflicted; compared every O3-3 source directory against the full-tested candidate and found no code
or test differences. N7-2 adds a tenth enabled differential package.

The composition command exited 0 on .32:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail
cd server
test -z "$(gofmt -l .)"
go vet ./...
go test -p=4 -timeout=30m -count=1 ./...
mapfile -t o3_diff_packages < <(go list -tags=configflow_difftest ./... | rg "/difftest$")
CONFIGFLOW_DIFF=1 go test -tags=configflow_difftest -p=4 -timeout=30m -count=1 "${o3_diff_packages[@]}"
o3_binary=$(mktemp)
trap "rm -f \"$o3_binary\"" EXIT
go build -o "$o3_binary" ./cmd/configflow
cd ..
PYTHONPATH=. ~/cf-test/.venv/bin/python server/scripts/review-n8-2-startup.py "$o3_binary"
~/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py
server/scripts/proddiff.sh'
```

- gofmt/vet: silent pass.
- Full `go test -p=4 -timeout=30m -count=1 ./...`: all pass, including main96.608s,
  importer1.455s, installer218.506s, oxidns106.586s, repo4.226s, yamlx1.684s.
- All ten explicitly enabled configflow_difftest packages: agents157.811s, subscriptions145.899s,
  discovery5.680s, health16.447s, httpx2.658s, mosdns3.194s, refresh6.742s,
  subs3.169s, substore11.701s, webdav34.478s; every package passes.
- Production binary synthetic corrupt startup script: all 3 cases pass, safe file+stderr,
  original database bytes unchanged.
- Proddiff tool tests: `Ran 31 tests in 0.030s`, `OK`.
- Tagged proddiff: 23 self-checks PASS, 2831 cases/415 comparisons,
  coverage_regressions=0, unmapped_routes=0, baseline `new=0 stale=0`; unchanged 11 baseline
  differences, 4 synchronous Startup names, 5 disabled periodic loops. Existing bracket tolerance
  rescued 2 comparisons after N7-2; no rule, waiver or baseline was added by O3-3.

Final rebase onto go@a44b343b adds only importer/oxidns test-harness repairs and handoff notes.
Checked the upstream delta: no production Go file changed. Compared all O3-3 source/test directories
against bcccb1d9: identical. Final focused .32 check exits 0:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail
cd server
test -z "$(gofmt -l .)"
go vet ./...
go test -p=4 -timeout=30m -count=1 ./internal/importer ./internal/oxidns'
```

Output: `ok configflow/internal/importer 2.477s`, `ok configflow/internal/oxidns 73.814s`.
Final diff check passes, worktree is clean, no push. Nothing remains for implementation acceptance;
independent O3-1/O3-2/O3-3 Opus review remains with the coordinator. Author status is ported.
