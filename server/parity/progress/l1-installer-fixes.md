# L1 installer defect fixes

Branch `go-fix-l1-installer`, created from latest go=038b52ae after finishing the
separate ETXTBSY fix (`go-fix-installer-etxtbsy`, a05c6047 + 58f1d8b3).
**Final status (2026-11-18): implementation and acceptance complete; ported, independent HR review pending. Final base `go@0f943623`; no push.**

Source report: [lab/L1.md](../lab/L1.md). No subagents, pushes or production access.
Use .32/.64 only. Container method reference: installer-container-lab memory
(Alpine with OpenRC softlevel; jrei/systemd-debian:12 with real systemd).

## Scope and coordinator decisions

Each defect gets its own compiling commit with regression tests.

- D1: **reject** compact data dirs containing `:` with existing guidePath false
  message. Remove the compact colon branch. Do not attempt long-format mounts.
  Record explicit differences in four recording comparisons and live Flask diff;
  leave original Python/recordings unchanged. Real Compose >=2.20.3 validation.
- D3: single and AIO shell install failure restores backed-up old binaries,
  configs/identities and prior service state. Stop/disable failed new services;
  restore before restarting old services. Preserve backups if recovery fails.
  Update generator fingerprint and explicit A08 byte-delta accounting. Real
  Debian/systemd and Alpine/OpenRC failure-injection acceptance required.
- D4: establish original registration-key policy; fix defect if needed, otherwise
  clarify errors/documentation. Source inspection so far: Python and Go both
  enforce registration_key unless installation_profile is present; the bundled
  Agent does not submit a registration_key. Preserve authentication enforcement.
- D5: HA host install checks :53 and cannot succeed with OxiDNS FATAL.
- D6: verification uses available tools or gives an explicit installation hint;
  a VIP check must actually query that VIP, not the system default resolver.
- D7: out-of-bounds binary Range ->416, not inherited Python500; ledger + tests.
- D8: coordinator explicitly approved an exception to Step B #3 after the old
  all-API-JSON intent was confirmed. Use an explicit **route-level marker** for
  the saved-install-link route only: final audit/refinement preserves all of
  Python's original JSON errors, including binary downloads. Preserve original
  plain Chinese error bodies/status; errors have no attachment disposition. List each route
  beneath Step B #3. Other API errors stay JSON. Test curl -fsSL and curl|sh,
  generic error-boundary tests and Flask differential.
- Excluded: D2 custom-lifecycle Agent migration and D9 Agent MosDNS text.

Final gates: all Go tests; all CONFIGFLOW_DIFF=1 packages; proddiff with
new=0/stale=0; affected-package race; gofmt/vet; final rebase go and clean tree.

## D1 implemented

GenerateGuide and exported GenerateCompact both validate data dirs with
guidePath(..., false); all invalid input returns no compose/info. Removed the
colon/long-bind branch. API test proves no capability directory or registry
mutation. Live Flask comparison asserts the deliberate Python200/unsafe mount
plus capability vs Go400/no capability, including Python's issue-lock file.

All four unchanged recording files now use exact D1 refusal classifications:
compose recorded:1, compose corpus:152, guide compose review:204, guide recorded:1
colon cases, plus 1 exact path-message wording update. Earlier unrelated errors
remain strict. No partial output is accepted alongside the new refusal.

On .32, real Compose **5.6.1** successfully runs the first three generated mount
specifications for both agent and companion (space and literal $ in the path),
with an inert Alpine image/network none. Reusing old colon/create_host_path=true
specifications fails with Docker's exact invalid-volume-specification error.
The probe exercises mounts only, not enrollment or the full HA application.
All uniquely named probe containers are removed by test cleanup.

Commands/results so far (all on .32):

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -c 'set -eu; cd server; go test -count=1 -run "Test(L1Compact|ComposeCorpus|ComposeRecorded|ComposeReview|DockerHARecorded)" ./internal/installer ./internal/api/agents; CONFIGFLOW_DIFF=1 go test -count=1 -v -run TestL1CompactColonDifferentialAgainstFlask ./internal/api/agents/difftest; CONFIGFLOW_L1_CONTAINERS=1 go test -count=1 -v -run TestL1CompactMountsWithRealCompose ./internal/installer'
```

```text
ok configflow/internal/installer 9.458s
ok configflow/internal/api/agents 0.064s
EXPECTED L1 D1: Python 200 + unsafe mount/capability, Go 400 + no writes
ok configflow/internal/api/agents/difftest 1.863s
real Docker Compose 5.6.1
L1 D1: generated normal mounts ran; legacy colon/create_host_path mounts failed as expected
ok configflow/internal/installer 3.326s
```

Final D1 formatting and full vet passed; focused rerun: installer 11.366s,
api/agents 0.190s, D1_READY_TO_COMMIT. D1 is complete in this commit.

D1 committed as 4c58d0cf.

## D3 — failure compensation (targeted acceptance complete)

Added shared embedded POSIX agent-install-rollback.sh and inserted it into both
single/AIO templates. It snapshots service active/enabled state, then stops old
processes before saving final binary/config/identity bytes. Saves old unit,
logrotate and any recovery-gate/core dependency files before editing them;
deduplicates paths. ARMED tracks stopped services, MUTATED tracks actual writes.
Before mutation, cleanup resumes original services without touching their files.
After mutation, stops/disables all attempted new services, restores all files
via same-directory temp+rename, reloads manager, then reenables/restarts only
prior states. Failed restore keeps private backups and services stopped. Cleanup
ignores further HUP/INT/TERM; staged new JSON is removed only if this run created it.

New l1_rollback_test.go has stateful Python manager fakes with real filesystem
unit/config snapshots, and a flag proving the candidate-start fault was reached:
single old active/inactive × systemd/OpenRC; AIO existing/new × both managers,
with successful first new AIO process before second fails; additional termination
during restore; forced restore-copy failure retains original config backup and
leaves failed service stopped/disabled. Initial main+signal cases passed .32
(7.123s); all ten strengthened cases, including copy-failure, passed (6.971s).

Fingerprint is f08339646f7d4db4d1cfe14ad314942c, verified from the original
release identity plus the three owned shell artifacts. Old unsafe links become
stale. Fixed exact source-delta fixture covers only approved D3 edits; helper
content, both templates and all remaining rendered bytes remain strict. A08
20,000 fuzz records + 949 matrix records + templates passed (4.873s), online
Flask comparison: 293 equal, 0 differ (4.707s), before the final restored-service
health check (which now verifies OpenRC supervisor has a living child too).
Latest focused and real-container checks passed on .64. Initial container
fixture defects were corrected: AIO executable permissions must be 0755;
OpenRC needs a reaping PID 1 (Docker --init). The hung old fixture was terminated
and its uniquely named containers removed; no aborted run counts as acceptance.
All six latest Debian/systemd + Alpine/OpenRC cases passed: single-existing,
single-new and AIO-existing on each manager (134.163s). Every case proves
candidate startup was reached; AIO proves both candidates started before failure.
Old service preconditions and restored bytes/modes/identities/active/enabled
states are checked against real init managers. No owned containers remain.
Latest focused regression + fmt/vet passed on .64 (20.375s).
Docker CLI execution now has bounded pipe-drain time and forwards termination
through sudo, so a leaked daemon descriptor cannot hang the harness forever. Existing focused old native/AIO tests passed .32
after the helper (11.746s). D4–D8 are now complete as detailed below.

D3 container acceptance uses a controlled candidate executable with actual init
managers: it exercises installation/identity/file/service compensation, not Agent
D2 lifecycle behavior (explicitly excluded from this batch).

D3 acceptance commands (.64):

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -c 'set -eu; cd server; CONFIGFLOW_L1_CONTAINERS=1 go test -count=1 -timeout=25m -v -run TestL1RollbackWithRealInitManagers ./internal/installer > /tmp/configflow-l1-d3-containers.log 2>&1; tail -40 /tmp/configflow-l1-d3-containers.log'
RT_HOST=sunbroke@192.168.0.64 rt --no-sync -- bash -c 'set -eu; cd server; gofmt -l internal/installer internal/api/agents/difftest/installer_test.go; go vet ./internal/installer/... ./internal/api/agents/...; go test -count=1 -run "Test(ScriptsMatchBackend|GeneratorFingerprint|L1PreRollback|RenderMatrix|FuzzCorpus|L1Single|L1AIOSecond|L1RollbackIgnores|L1FailedRollback)" ./internal/installer; CONFIGFLOW_DIFF=1 go test -count=1 -run TestInstallerDifferentialAgainstFlask ./internal/api/agents/difftest'
```

```text
--- PASS: TestL1RollbackWithRealInitManagers (134.14s)
PASS
ok configflow/internal/installer 134.163s
ok configflow/internal/installer 20.375s
ok configflow/internal/api/agents/difftest 4.736s
```

## D4 — registration-policy clarification

Python routes/agents.py already requires registration_key for ordinary register;
ordinary Shell/Docker Agent has no client option for it. HA enrollment instead
uses a scoped installation capability. Authentication is unchanged. Missing/empty
key 403 now explains this and how to choose enrollment/contact the administrator;
wrong keys retain the original refusal. No global secret is placed in an URL,
script, response or persisted Agent. Updated agent-install documentation.

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api/agents/messages.go --back server/internal/api/agents/agents_test.go -- bash -c 'set -eu; cd server; gofmt -w internal/api/agents/messages.go internal/api/agents/agents_test.go; go vet ./internal/api/agents; go test -count=1 -v -run "Test(L1Locked|RegistrationKey|DockerHAEnrollmentLocked)" ./internal/api/agents'
```

All three tests pass; `ok configflow/internal/api/agents 0.151s`.
D3 full package check (331.726s, .64) reports only the older A09 compact corpus's
13 undeclared D1 cases; API agents passed 28.736s. No D3 failures. The additional
A09 corpus must receive the same narrow D1 declaration before final acceptance.

D1 additional A09 recording: docker_ha.jsonl.gz has 1 compact-colon and 12 exact
old path-message cases. Explicitly classified using the same narrow helpers;
all other cases remain strict. On .64:
`RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/installer/docker_ha_corpus_test.go -- bash -c 'set -eu; cd server; gofmt -w internal/installer/docker_ha_corpus_test.go; go test -count=1 -v -run TestDockerHACorpusMatchesPython ./internal/installer'`
→ 3279 cases, 0 differ; `ok configflow/internal/installer 0.511s`.

## D7 — unsatisfiable download ranges

Return 416 (not the Python catch-all 500), with Content-Range bytes */actual-size,
no attachment header and no server ERROR log. Preserve JSON error text and HEAD
empty-body handling. Regression covers boundary/huge/multiple ranges + valid206;
Flask differential explicitly accepts only the exact range error's status/header
correction. All other downloads remain strict.

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api/agents/l1_downloads_test.go --back server/internal/api/agents/difftest/downloads_diff_test.go -- bash -c 'set -eu; cd server; gofmt -w internal/api/agents/l1_downloads_test.go internal/api/agents/difftest/downloads_diff_test.go; go vet ./internal/api/agents/...; go test -count=1 -v -run TestL1UnsatisfiableBinaryRangeIs416 ./internal/api/agents; CONFIGFLOW_DIFF=1 go test -count=1 -v -run TestDownloadDifferentialAgainstFlask ./internal/api/agents/difftest > /tmp/configflow-l1-d7-diff.log 2>&1; tail -5 /tmp/configflow-l1-d7-diff.log'
```

`ok configflow/internal/api/agents 0.038s`; downloads 120 comparisons equal,
0 differ, 12 pre-existing httpx differences; `ok .../difftest 3.315s`.

## D8 — explicit plain-text download exception

Confirmed coordinator's refinement: preserve Python JSON where it originally
existed, including Agent binary missing/invalid-name404. Only the saved install
link has explicit plain-text404/503, so only agents.download_install_command gets
Route.PreservePlainErrors. No URL inference. All other API boundaries remain JSON.
Remove attachment header on these errors per earlier explicit instruction;
Python has that header, so the differential declares and asserts this one header
correction separately rather than claiming identical headers. Body (including
newline), status, content-type and remaining route headers match Python exactly.
Audited core/probe/install/Docker snippets and binary/config archive download
errors; no extra plain-error markers warranted. Step B #3 lists the sole route.

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/httpx/router.go --back server/internal/httpx/app.go --back server/internal/httpx/error_boundary_test.go --back server/internal/api/agents/l1_shell_download_test.go --back server/internal/api/agents/difftest/l1_download_errors_test.go -- bash -c 'set -eu; cd server; gofmt -w internal/httpx/router.go internal/httpx/app.go internal/httpx/error_boundary_test.go internal/api/agents/l1_shell_download_test.go internal/api/agents/difftest/l1_download_errors_test.go; go vet ./internal/httpx/... ./internal/api/agents/...; go test -count=1 -v -run "Test(ConfigFindingsEveryAPIErrorUsesJSON|L1ShellDownload|StorageFailures)" ./internal/httpx ./internal/api/agents; CONFIGFLOW_DIFF=1 go test -count=1 -v -run "Test(L1DownloadErrorFormats|InstallerDifferentialAgainstFlask)" ./internal/api/agents/difftest > /tmp/configflow-l1-d8-diff.log 2>&1; tail -10 /tmp/configflow-l1-d8-diff.log'
```

httpx0.016s, api/agents0.061s; installer293 comparisons equal/0 differ; new Flask
cases both install404/malformed-token and binary404/invalid-filename pass;
difftest2.829s. Curl-fsSL returns22 without a body, curl|sh exposes the original
Chinese refusal rather than treating JSON as a script. No real network outside
loopback; no production access.

## D5 — check host DNS port and core health

Before new/stopped Host installation, a temporary Python socket probe uses the
Docker daemon's host network namespace and binds UDP/TCP53 together. Already
running, ownership-proven installation skips this probe (its own listener is not
a foreign conflict). New and migration script success requires actual supervisor
oxidns RUNNING, with bounded30 polls; FATAL/EXITED/STOPPED or timeout is failure.
Existing state/CIDs/backup and explicit retry/rollback mechanisms are preserved.
Pure shell tests cover busy port and FATAL through the full generated install
command. Frozen source module/calls are reversed only at strict historical
comparison boundaries, including duplicated install command block. All other
fields, Compose, capabilities and rollback commands remain strict.

Real .64 containers use Supervisor and actual bound sockets: free succeeds,
UDP53 and TCP53 each refuse, RUNNING succeeds and real FATAL cannot print success.
The test substitutes only --network host → owned-container namespace for the
probe, so the test server host's53 is never bound. All owned containers removed.

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/installer/l1_ha_container_test.go --back server/internal/installer/installertest/ha.go -- bash -c 'set -eu; cd server; gofmt -w internal/installer/l1_ha_container_test.go internal/installer/installertest/ha.go; go test -count=1 -run "Test(L1HA|NewRetry|Migration|DockerHA.*(Corpus|Recorded))" ./internal/installer > /tmp/configflow-l1-d5-focus.log 2>&1; tail -10 /tmp/configflow-l1-d5-focus.log; CONFIGFLOW_L1_CONTAINERS=1 go test -count=1 -v -run TestL1HAStartupWithRealSocketsAndSupervisor ./internal/installer > /tmp/configflow-l1-d5-real.log 2>&1; tail -15 /tmp/configflow-l1-d5-real.log'
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api/agents/difftest/docker_ha_compose_diff_test.go -- bash -c 'set -eu; cd server; gofmt -w internal/api/agents/difftest/docker_ha_compose_diff_test.go; CONFIGFLOW_DIFF=1 go test -count=1 -v -run TestDockerHADifferentialAgainstFlask ./internal/api/agents/difftest > /tmp/configflow-l1-d5-diff.log 2>&1; tail -6 /tmp/configflow-l1-d5-diff.log; go vet ./internal/installer/... ./internal/api/agents/...'
```

```text
ok configflow/internal/installer 11.134s
--- PASS: TestL1HAStartupWithRealSocketsAndSupervisor (21.22s)
ok configflow/internal/installer 21.221s
docker HA routes: 239 comparisons equal, 0 differ
ok configflow/internal/api/agents/difftest 4.412s
```
Initial comparison failures exposed duplicate command_blocks and a separate
Python→Go Compose-generator comparison path; both now apply the same fixed
approved delta. They were comparison coverage gaps, not runtime failures, and
are not counted as successful acceptance runs. Full gate remains outstanding.

## D6 — portable VIP verification

Generated command selects dig → nslookup → BusyBox nslookup, explicitly queries
the validated VIP and propagates client errors. Missing clients exit127 with
Debian/Ubuntu dnsutils and Alpine bind-tools instructions. getent is deliberately
not used because it cannot choose a DNS server. Tests include all branches,
missing clients despite getent being present, and failing selected client.
Frozen verification source is compared exactly and inverse-normalized only when
complete command + canonical IPv4 match the approved template.

Real .64 Alpine image: each real dig/nslookup/BusyBox client resolved example.com
through synthetic VIP192.0.2.53 to fixture A192.0.2.123, entirely inside an owned
network-none container's loopback interface. Actual minimal debian:12 produced
the clear missing-client hint. No server host network changes or public DNS.

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/installer/installertest/ha.go --back server/internal/installer/l1_ha_verify_test.go -- bash -c 'set -eu; cd server; gofmt -w internal/installer/installertest/ha.go internal/installer/l1_ha_verify_test.go; go test -count=1 -v -run "Test(L1HAVerification|Verification|DockerHA.*(Corpus|Recorded))" ./internal/installer > /tmp/configflow-l1-d6-focus.log 2>&1; tail -12 /tmp/configflow-l1-d6-focus.log; CONFIGFLOW_DIFF=1 go test -count=1 -v -run TestDockerHADifferentialAgainstFlask ./internal/api/agents/difftest > /tmp/configflow-l1-d6-diff.log 2>&1; tail -5 /tmp/configflow-l1-d6-diff.log'
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/installer/l1_ha_verify_container_test.go -- bash -c 'set -eu; cd server; gofmt -w internal/installer/l1_ha_verify_container_test.go; CONFIGFLOW_L1_CONTAINERS=1 go test -count=1 -timeout=10m -v -run TestL1HAVerificationWithRealDNSClients ./internal/installer > /tmp/configflow-l1-d6-real.log 2>&1; tail -14 /tmp/configflow-l1-d6-real.log; go vet ./internal/installer/... ./internal/api/agents/...'
```

```text
ok configflow/internal/installer 5.705s
docker HA routes: 239 comparisons equal, 0 differ
ok configflow/internal/api/agents/difftest 4.442s
--- PASS: TestL1HAVerificationWithRealDNSClients (7.06s)
ok configflow/internal/installer 7.067s
```

D8 gate-audit follow-up: the proddiff comparator still explicitly accepted the
old short-link plain/HTML→JSON migration. Removed that obsolete waiver; original
404/503 Chinese body+newline, status and type must now match exactly. The only
narrow exception asserts Python's exact configflow-install.sh attachment header
and Go's absence, keeping every other header strict. Mutations back to JSON,
wrong body/status/type/header and unrelated routes fail. No baseline diff entry
was added or hidden. Updated comparator README and synthetic regression.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -c 'set -eu; PYTHONDONTWRITEBYTECODE=1 "$HOME/cf-test/.venv/bin/python" -m unittest discover -s server/tools/proddiff -p test_proddiff.py'`
→ 34 tests in0.020s, OK. Stopped only our still-queued gate before changing the
remote snapshot; final gate will restart with the updated comparator.

## Final hand-back map (acceptance complete)

All fixes are ported; no independent-review status is claimed. Latest integrated
base for final proddiff acceptance: go0f943623; branch go-fix-l1-installer, no push.

| Defect | Commits after rebase | Targeted evidence |
|---|---|---|
| D1 | 7d536406 + da6d9ea7 | five old recording sets, no capability writes on refusal, real Compose5.6.1 old/new mount behavior |
| D3 | 7e9f1726 + 31b5f2c3 | stateful failure/cancellation/copy-failure regressions, six real systemd/OpenRC scenarios, A08 matrix/fuzz and Flask293/0 |
| D4 | c822dc47 | old policy confirmed, missing-key hint, registry unchanged, existing-token and locked enrollment security |
| D5 | 2bcf0a86 + 2784efbe | real UDP/TCP53 busy/free and Supervisor RUNNING/FATAL; Flask239/0 |
| D6 | 07630d7c | three real DNS clients resolve isolated VIP; actual minimal Debian12 missing-client hint |
| D7 | ecd18ef1 | 416 actual-length headers/HEAD/valid206; download Flask120/0 (12 inherited httpx classes) |
| D8 | 204c11e1 + ba7ddee7 | plain404/503 vs JSON binary404/invalid name; curl cases; strict proddiff comparator34 tests |

D2/D9 remain outside this batch. No multi-host HA/VIP failover claim: that remains
L3/lab work. All D3/D5/D6 containers are owned and removed; no PVE/production access.
Final proddiff-only gate passed on .32, logs
/tmp/configflow-l1-installer-proddiff-final (new0/stale0). All test commands have
finished. Shared gate-stage helper: c66a38cb. Exact final command/output below.

Historical first full-gate state (2026-11-18): .64's queue remained occupied, so
stopped only our waiting gate and moved the full gate to .32, which had a free
slot. GATE_LOGS=/tmp/configflow-l1-installer-gate-r3, gate.sh -p4, acquired slot2.
Gofmt/full vet and both tagged builds/vet/lifecycle tests passed; full race is
still running. Initial proddiff's new785/stale0 consists of D3 approved installer
line shifts (old comparator lacked the shared frozen delta) and the coordinator's
three known aggregation-provider differences. Added exact D3 delta reversal from
installertest/l1-rollback-deltas.json to proddiff too; source/module/route/status
mutations stay visible. Comparator35 tests passed (.64,0.023s), bash -n gate.sh
passed. No aggregation allowance/baseline entry was added.

Coordinator confirmed 72d4b2c0 on go-fix-agg-provider-load fixes the three provider
artifacts: only proddiff waits for both completed renders; production12s last-good
policy stays unchanged. Wait for coordinator notification/merge, then rebase go
and rerun ONLY proddiff (explicitly authorized); do not repeat the full race.

Added GATE_ONLY=difftest|proddiff to gate.sh: both reuse its existing slot lock,
report only their own stage, propagate failure, and leave ordinary full gates
unchanged. This lets the remaining difftests run despite known proddiff failure
and lets the final approved proddiff-only rerun obey the host's gate-slot limit.
All difftests subsequently passed on .64 through this stage mode, logs
/tmp/configflow-l1-installer-diff-final. Exact commands/results follow.

## Race and differential acceptance (completed before provider integration)

Full .32 gate finished its race stage: **85 packages passed**; only installer
failed on TestN73ComposeHashOnlyReviewCases's three explicit old A10 semantic
rows. They bypassed the general comparison path and therefore had not applied
D5's fixed source delta. Applied l1GuideBaseline to these three assertions;
semantic guide/bundle checks stay strict. No production change. Whole installer
race then passed on .64,579.035s. No other race failures or ETXTBSY occurred.

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -c 'set -eu; GATE_LOGS=/tmp/configflow-l1-installer-gate-r3 server/scripts/gate.sh -p 4'
RT_HOST=sunbroke@192.168.0.64 rt -- bash -c 'set -eu; GATE_ONLY=difftest GATE_LOGS=/tmp/configflow-l1-installer-diff-final server/scripts/gate.sh -p 4'
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/installer/n73_review_test.go -- bash -c 'set -eu; cd server; gofmt -w internal/installer/n73_review_test.go; go vet ./internal/installer/...; go test -race -p 4 -count=1 -timeout=30m ./internal/installer > /tmp/configflow-l1-installer-race-final.log 2>&1; tail -12 /tmp/configflow-l1-installer-race-final.log'
```

```text
# full gate: gofmt/full vet, tagged builds/vet/lifecycle PASS
ok configflow/internal/api/agents 477.345s
ok configflow/internal/devhttp 3.916s
ok configflow/internal/httpx 35.624s
ok configflow/internal/routers 510.899s
# only failure: three old installer semantic assertions; fixed and rerun:
ok configflow/internal/installer 579.035s
# all enabled differentials:
ok configflow/internal/api/agents/difftest 118.949s
ok configflow/internal/api/aggregations/difftest 21.175s
ok configflow/internal/api/subscriptions/difftest 61.335s
ok configflow/internal/devicenotes/difftest 0.506s
ok configflow/internal/discovery/difftest 1.999s
ok configflow/internal/health/difftest 13.711s
ok configflow/internal/httpx/difftest 1.389s
ok configflow/internal/mosdns/difftest 1.796s
ok configflow/internal/refresh/difftest 3.462s
ok configflow/internal/subs/difftest 1.472s
ok configflow/internal/substore/difftest 5.180s
ok configflow/internal/webdav/difftest 15.693s
GATE STAGE PASS: difftest
```

The original full gate exit1 is retained, not called GATE PASS: it contains the
three pre-fix installer assertions and the pre-update proddiff snapshot. The
corrected installer package and all independent difftest packages passed as above.
Per explicit coordinator instruction, after provider fix merge/rebase only
proddiff must rerun; no redundant whole race. No active test commands remain.

## Final provider integration and proddiff PASS

Coordinator notified that provider fix was merged as go0f943623. Rebased
go-fix-l1-installer onto that exact integration head without conflicts. As
explicitly instructed, reran ONLY proddiff after rebase, using the same gate
slots; .64 was occupied, so the actual run used .32's free slot2. No extra
whole race/full-suite run. No production behavior or baseline was weakened.

```sh
git rebase go
RT_HOST=sunbroke@192.168.0.32 rt -- bash -c 'set -eu; GATE_ONLY=proddiff GATE_LOGS=/tmp/configflow-l1-installer-proddiff-final server/scripts/gate.sh -p 4'
```

```text
gate: slot 2 of 2
== proddiff (only; other stages are not run)
self-check: ... text-line=PASS text-insert=PASS text-line-endings=PASS
coverage_regressions=0 unmapped_routes=0 unique_differences=11 self_check=PASS
baseline: new=0 stale=0
GATE STAGE PASS: proddiff
```

Compared75 GET routes/2831 cases, with462 comparisons in the detailed counter.
All23 comparator self-checks passed. The existing11 schema-only baseline
differences are unchanged; no aggregation allowance was added. Original
production12s provider policy is unchanged; only proddiff waits for both
completed artifacts, per the coordinator's merged fix.

All implementation commits are separate by defect; mapping above uses final
rebased IDs. Reports and behaviour ledger contain approved Python bug fixes,
D4's original registration limitation, D8's sole route exception plus explicit
attachment-header correction, commands and retained initial-failure evidence.
Status remains ported pending independent review. D2/D9 and multi-host L3
failover remain outside this batch. No active commands or owned containers;
final git rebase go/clean-tree check follows the report commit. No push.
