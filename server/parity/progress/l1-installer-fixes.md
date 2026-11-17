# L1 installer defect fixes

Branch `go-fix-l1-installer`, created from latest go=038b52ae after finishing the
separate ETXTBSY fix (`go-fix-installer-etxtbsy`, a05c6047 + 58f1d8b3).
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
  shell-facing installer/script/binary/archive routes. Preserve plain Chinese
  error bodies/status; errors have no attachment disposition. List each route
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

## D3 — failure compensation (implementation complete, acceptance in progress)

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
Latest focused and real-container checks are running on .64. Initial container
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
after the helper (11.746s). No D4–D8 code changed yet.

Next after D3: D4 policy clarification, D5 health/port guards, D6 resolver
fallback, D7 Range, D8 route-marker exception; then the final gates above.

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
