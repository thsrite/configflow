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

Next: D3 backup/rollback,
D4 policy clarification, D5 health/port guards, D6 resolver fallback, D7 Range,
D8 route-marker exception. No work has started on D3–D8 implementation yet.
