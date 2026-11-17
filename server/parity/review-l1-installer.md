# L1 installer fixes — independent HR review (D1, D3–D8)

Reviewer: Opus session that did not write the fixes (Codex wrote them on
`go-fix-l1-installer`, final `dc84183f`). Branch `go-opus-review-l1-installer`,
rebased onto `go` `d8f50b00`. Scope: A08 Shell installer (D3, D8), A09 Docker HA
(D5, D6), A10 Compose/download (D1, D7), registration hint (D4). D2/D9 were
excluded from the batch. Inputs: `lab/L1.md`, `progress/l1-installer-fixes.md`,
review-a19/r06/r07 categories, PROGRESS A08–A10, behaviour-changes.

Method: every D3 failure path of both templates (single + AIO, systemd + OpenRC)
was walked line by line against the new journal helper; each suspected defect
became a real-container failure-injection case first (Debian 12 systemd with
dash 0.5.12 and bash, Alpine 3 OpenRC with BusyBox ash). The same test file was
then run against the author's commit `dc84183f` to show each case fails there
(see "Commands"). Only the test servers' owned containers (`--network none`)
were used; no production, PVE or real hostnames.

## Findings

All findings are fixed; **none is open**. Severity: **0 high, 1 medium, 6 low**.

### Medium

- **R1 — An interrupt killed the report shell; compensation ran unseen.**
  `WithInstallOutput` (identical to Python) runs the body in a subshell and has
  only an EXIT trap. Ctrl-C, SSH loss (HUP) or a job-wide TERM reach the whole
  job: the outer shell died immediately (exit −2/−15 in the driver), the prompt
  returned, and the body's D3 cleanup kept restoring in the background. All
  rollback messages, including "Agent 文件或服务配置未能全部恢复…" and the kept
  backup path, went only to the log whose path is printed by the dead outer
  shell — the user saw nothing, and could rerun the installer concurrently with
  a restore still in progress. This defeats D3's "honest report" requirement on
  exactly the interrupt paths. Fix: the single/AIO wrapper adds
  `trap ':' HUP INT TERM` before the body (`withCompensatingInstallOutput`);
  traps run after the foreground body exits, so the outer shell waits for
  compensation, prints the failure details/log path and exits with the body's
  status (130 for Ctrl-C). Core and gateway installers are unchanged. The Python
  baseline comparisons reverse it through a fixed rendered-only delta
  (`l1-rollback-deltas.json` `rendered`), also used by proddiff.
  Tests: `TestL1ReviewFailureInjectionWithRealInitManagers/{systemd,openrc}/sigint-during-start`
  and `/sigterm-during-restore` (real signals to the job's process group).

### Low

- **R2 — Failed fresh AIO left its service directories, which then block the
  single-service installer.** Compensation removed binary/config/unit, but
  `/opt/configflow-agent/mihomo|oxidns` stayed (the real Agent also writes
  `.agent-migration.json` there on start). `install-go.sh` refuses whenever
  those directories exist ("检测到原生同机双 Agent"), although the AIO run had
  just reported a full restore. Fix: `agent_rollback_directory` records a
  directory only if it does not exist before mutation; a successful file
  restore removes exactly those directories (never a pre-existing one, never
  through a symlink). Tests: `TestL1ReviewFailedFreshAIORemovesOnlyCreatedDirectories`
  (unrelated sibling kept), container `aio-new-then-single` (a single install
  afterwards succeeds). On `dc84183f` the directory remained.
- **R3 — IPv4 forwarding hook not journaled.** `enable-ipv4-forwarding.sh`
  (single and AIO Mihomo) is written after mutation starts but was not in the
  journal: a failed fresh install left it, and an upgrade from an older hook
  kept the new one under the restored old unit. Fix: saved with the other
  managed files. Test: `TestL1ReviewFailedFreshInstallRemovesForwardingHook`.
- **R4 — A journal left by a killed run was silently ignored.** After SIGKILL
  or power loss between the file replacement and success, the old binary and
  identity exist only in `.agent-install-backup.*`; a rerun neither restored
  nor mentioned it. Fix: `agent_rollback_begin` warns (`cf_warn`, visible on
  success and failure) and lists every leftover journal with how to use
  `path.N`/`file.N`; it is never applied or deleted automatically (applying a
  half-written journal blindly would be riskier). Tests:
  `TestL1ReviewLeftoverBackupIsReported`, container `sigkill-then-rerun` (old
  identity byte-identical in the leftover journal before and after the rerun).
- **R5 — OpenRC: the restored old Agent could be stopped again by a job-wide
  TERM.** Verified in a container: `supervise-daemon` stays in the caller's
  process group (HUP/INT are harmless, TERM stops its service). During
  compensation a TERM to the job stopped the just-restored old Agent; the script
  honestly reported "服务状态未能全部恢复" but left it stopped. Fix: compensation
  starts restored OpenRC services via `setsid` (when available; BusyBox and
  util-linux both provide it). Test: container `openrc/sigterm-during-restore`.
- **R6 — Ledger incomplete for D3's other visible changes.** Not recorded:
  single install now refuses when the old Agent cannot be stopped and is still
  active (Python `|| true` then overwrote it); AIO now continues if stop failed
  but the unit is inactive; fresh-install failure removes the new files. Added
  to behaviour-changes together with R1–R5.
- **R7 — D5 lacked the "own listener" regression** the task asked for: a rerun
  with the owned, running OxiDNS holding :53 must not be refused; macvlan never
  probes. Code was correct (`haPortCheck` skips a running owned main
  container); test added: `TestL1ReviewOwnRunningOxiDNSIsNotAPortConflict`.

## Checked without findings

- **D3 failure paths (both templates, both managers).** Before arming: only
  `BINARY_TMP`/`CONFIG_FILE.new`/forwarding temp are removed, no service is
  touched. Armed, not mutated (stop failure, snapshot `cp` failure e.g. disk
  full, `-check-deployments`/`-require-clean-deployments`, forwarding ownership
  recheck): previously active services restarted, files untouched. Mutated
  (gate removal, `mv`, unit/logrotate/hook writes, enable/start, health check,
  `persist_ipv4_forwarding`): stop+disable every attempted service first, then
  same-directory temp+rename restore (symlinks via `cp -pP`, verified with GNU
  and BusyBox), reload, re-enable and restart prior states, real health check.
  Snapshots are taken after the old process stopped. Cleanup ignores
  HUP/INT/TERM and children inherit the ignore (systemctl/sleep not killed).
- **Restore failure honesty.** Real read-only bind mount over `/usr/local/bin`
  (container `restore-fails-readonly`): "Agent 文件或服务配置未能全部恢复；服务保持停止",
  backup path printed, failed unit inactive **and** disabled (no retry loop),
  kept journal holds the old binary and identity byte for byte; no "已恢复" claim.
- **No user files removed.** Only journaled paths are touched; absent-before
  paths are deleted, present-before ones replaced from the backup; directories
  only when created by this run. Gate units, drop-ins and OpenRC runlevel
  symlinks restored byte/target-identical, including a drop-in named
  `my $HOME override.conf` (container `gate-restore`, bash and BusyBox ash).
- **Identity/credentials.** Config, binary, unit restored with bytes, mode and
  ownership (`cp -pP`); container snapshots compare sha256+mode.
- **Portability.** dash 0.5.12 (Debian), bash 5, BusyBox ash in real init
  containers; dash 0.5.13.4 / bash / BusyBox with `env -i` (C locale): a journaled
  path with Chinese, space and `$` is recorded and restored exactly. `set -e` is
  disabled inside `agent_rollback_restore || true`, and every step there checks
  its own status. The earlier dash-0.5.13 terminal-text corruption is unrelated
  to the journal (paths are used directly, not re-assigned).
- **D5.** Probe binds 0.0.0.0:53 UDP+TCP in the daemon's host namespace; the
  ConfigFlow image runs as root with python3 (same mechanism as the existing
  enrollment proof). Skipped for a running owned main container; stopped owned
  container is probed (its port is free). Migration does not probe. Readiness
  waits for `RUNNING` (OxiDNS `startsecs=3`), FATAL/EXITED/STOPPED fail fast.
  No false positive found; the image is user-selectable, and a custom image
  without python3 fails loudly (not silently succeeds).
- **D6.** dig → nslookup → BusyBox nslookup, always `@VIP`; exit 127 with
  dnsutils/bind-tools hint. dig exits 0 on SERVFAIL/NXDOMAIN — output is shown,
  as with Python's nslookup command.
- **D8.** Python returns non-JSON errors under `/api` only in
  `download_install_command` (404/503 plain text; checked every `routes/*.py`
  error return). `PreservePlainErrors` is set on that route only and applies
  only to explicit `text/plain` handler responses; unexpected errors stay JSON.
  Body/status/type/no-store/nosniff equal Flask; the attachment header removal
  is the one declared difference, asserted in difftest and proddiff.
- **D7.** Only `werkzeugRangeNotSatisfiable` becomes 416 with
  `Content-Range: bytes */size`; parse_date overflow stays 500.
- **D4.** No component ever sends `registration_key` (repository-wide search);
  policy unchanged, hint only for a missing/empty key, wrong key keeps the old
  text, key never written anywhere.
- **D1.** `GenerateGuide` and `GenerateCompact` both use `guidePath(..., false)`
  before any capability write; long-bind branch removed.
- **Fingerprint and deltas.** `l1-rollback-deltas.json` contains only D3 edits
  plus this review's (one AIO directory-journal line in the template; the wrapper trap and the forwarding-hook journal line as rendered-only deltas, emitted by the Go generators only where forwarding is managed); fingerprint recomputed
  from the three shell artifacts (`9bed9bf8…`), so review-time links are stale.
  HA scripts are not saved as links, so D5/D6 need no fingerprint change.
- **No Python emulation** in production code. Python appears only as test
  fakes (existing harness convention), delta generators, and the D5 probe that
  runs inside the ConfigFlow image.

## Codex-port quality (for the coordinator)

- Good: one compiling commit per defect, strict comparisons kept with narrow,
  generated delta fixtures, real init-manager acceptance, honest failure texts.
- Missed the class "what does the user see": every interrupt path was tested
  only with fakes that call the body directly, never through the report wrapper
  and a real process group, so R1/R5 were invisible. Fresh-install cleanup was
  asserted only for files, not directories or the hook (R2, R3).
- Debt, not findings: (a) single-template branch
  `elif [ "$RESTART_PREVIOUS_AGENT" = 1 ]` is unreachable (the flag is only set
  after arming); harmless, left to keep the delta small. (b) The fingerprint
  covers only the three shell files; a future change in `goagent.go`/`aio.go`/
  `output.go` alone would not invalidate stored links — suggest an explicit
  revision constant in the digest. (c) Insertion deltas are reversed with a
  global `ReplaceAll`; the JSON is self-generated from the diff, so the review
  artifact is the JSON diff itself. (d) A symlinked unit file (`systemctl link`)
  is written through by the template (pre-existing); the journal restores the
  link but not the target's old content. (e) `cf_wait_oxidns` returns without a
  message for a non-64-character ID (unreachable in practice). (f) The progress
  report is hard to read (missing spaces, "Flask239/0").

## L2 / later lab retests

- D3 with real Agent binaries on Debian 12 and Alpine guests: old real Agent,
  new real Agent failing after its startup migration (e.g. registration locked);
  panel has no orphan, old Agent heartbeats again, leftover
  `.agent-migration.json` harmless.
- Ctrl-C over a real SSH pty and an SSH disconnect mid-install (both OS): the
  terminal shows the restore result (R1); after disconnect the restore finishes.
- Hard VM reset mid-install, then rerun: leftover-journal warning (R4).
- D5 with the real ConfigFlow image on a host: fresh host install reaches
  RUNNING; rerun with own running container passes; foreign :53
  (resolved stub/dnsmasq) refused; macvlan unaffected.
- D6 against a real VIP on minimal Debian 12 / Alpine; D1 first compact install
  on Compose 2.20.x; D8 `curl | sh` with an expired link on a guest.
- dash 0.5.13 is not in Debian 12/13; if a distribution ships it, rerun an
  install with `env -i`.

## Commands and results

All on `192.168.0.64` unless noted.

```sh
# focused unit tests + proddiff comparator
RT_HOST=sunbroke@192.168.0.64 rt -- bash -c 'set -u; cd server; gofmt -l internal/installer; go vet ./internal/installer/... || exit 1; (cd .. && PYTHONDONTWRITEBYTECODE=1 "$HOME/cf-test/.venv/bin/python" -m unittest discover -s server/tools/proddiff -p test_proddiff.py 2>&1 | tail -3); go test -count=1 -run "Test(L1Review|ScriptsMatchBackend|GeneratorFingerprint|L1PreRollback|L1Single|L1AIOSecond|L1RollbackIgnores|L1FailedRollback|FailedAgentStart|StandaloneInstall|NativeReinstall|SecondCorePreflight|RenderMatrix|FuzzCorpus)" ./internal/installer; CONFIGFLOW_L1_CONTAINERS=1 go test -count=1 -timeout=30m -v -run TestL1ReviewFailureInjectionWithRealInitManagers ./internal/installer'
```

```text
Ran 37 tests in 0.024s
OK
ok  	configflow/internal/installer	26.443s
--- PASS: TestL1ReviewFailureInjectionWithRealInitManagers (94.29s)
    --- PASS: .../systemd/sigint-during-start/dash (4.50s)        exit 130, restored
    --- PASS: .../systemd/sigterm-during-restore/dash (5.98s)     exit 1, restored
    --- PASS: .../systemd/sigkill-then-rerun/dash (6.32s)
    --- PASS: .../systemd/aio-new-then-single/dash (5.93s)
    --- PASS: .../systemd/gate-restore/bash (5.94s)
    --- PASS: .../systemd/restore-fails-readonly/dash (3.81s)
    --- PASS: .../openrc/sigint-during-start/busybox-ash (4.57s)
    --- PASS: .../openrc/sigterm-during-restore/busybox-ash (14.33s)
    --- PASS: .../openrc/sigkill-then-rerun/busybox-ash (12.75s)
    --- PASS: .../openrc/aio-new-then-single/busybox-ash (14.12s)
    --- PASS: .../openrc/gate-restore/busybox-ash (14.11s)
ok  	configflow/internal/installer	94.292s
```

Same container test file on the author's commit (temporary detached worktree at
`dc84183f`, removed afterwards with `rt clean`):

```text
.../systemd/sigint-during-start/dash      exit -2  (outer shell killed, no result shown)
.../systemd/sigterm-during-restore/dash   exit -15
.../systemd/sigkill-then-rerun/dash       rerun did not report the leftover journal
.../systemd/aio-new-then-single/dash      IsADirectoryError: '/opt/configflow-agent/mihomo' (left behind)
.../openrc/*                              same four failures; gate-restore and restore-fails-readonly PASS
```

Author's real-container acceptance rerun on the review tree:

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -c 'cd server; CONFIGFLOW_L1_CONTAINERS=1 go test -count=1 -timeout=40m -v -run "Test(L1RollbackWithRealInitManagers|L1HAStartupWithRealSocketsAndSupervisor|L1HAVerificationWithRealDNSClients|L1CompactMountsWithRealCompose)" ./internal/installer'
```

```text
--- PASS: TestL1CompactMountsWithRealCompose (1.50s)
--- PASS: TestL1HAStartupWithRealSocketsAndSupervisor (7.29s)
--- PASS: TestL1HAVerificationWithRealDNSClients (2.07s)
--- PASS: TestL1RollbackWithRealInitManagers (56.95s)   six systemd/OpenRC cases
ok  	configflow/internal/installer	67.806s
```

dash 0.5.13 C-locale journal probe (.32, Arch dash 0.5.13.4-1, `env -i`):
dash, bash and BusyBox each restored `units/中文 $HOME override.conf` exactly
and removed the journal.

Final gate: see below.
