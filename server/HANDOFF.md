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
| 19338c67-c336-40c7-98a9-b4532a66af68 | C19/C20 Surge/Loon adversarial review | wks_0d8cd06808daac1f, branch `go-review-c19c20` |
| 3112fbeb-e43a-4d16-ad1a-bc88670b1099 | C37 domain discovery port | wks_0dcc037efd16c502, branch `go-port-c37` |
| e46d698f-6345-41b9-8a38-06d61b6622d3 | config black-box difftest, Step A coverage matrix (→ Opus O6 review) | wks_223d2a53f6e7abbb, branch `go-difftest-config` |
## >>> CURRENT STATE — 2026-11-18 ~10:30 CST (read this first; everything below is history) <<<

**Process (all new today, maintainer-approved; see WORKFLOW.md last three sections + AGENT-BRIEF):**
- Merge batching: ready branches are stacked into `batch-N` (rebase each onto the previous; `.md` conflicts keep
  both sides via /tmp/rebase_md.py), one `cf-gate` on the stacked tree, ff-merge on PASS. Docs/test-tooling-only
  commits (parity docs, configdiff) merge after their own checks. A batch gated on an older `go` must be re-gated
  after rebasing if `go` gained Go product code meanwhile (every merged commit passes a full gate on its exact tree).
- Agents never run the full gate: targeted checks only, then hand back. No progress messages (only decision /
  hand-back / blocked). Long tests = one blocking command, never polling. (Token cost was the reason.)
- Leaner verification: reviewers don't re-run the author's suites; no per-module mutation gates; Python
  comparisons = normal paths + one case per error class. Non-HR independent review is KEPT.
- gate.sh: 1 agent gate per server (coordinator cf-gate may add one: CF_GATE_SLOTS=2). Background cf-gate needs
  Bash timeout 7200000 (30-min default killed 4 gates once).
- Proddiff Python-side caching was evaluated and rejected (proddiff cold = 72 s; see progress/proddiff-cache-evaluation.md).
  The gate's time is almost all the full `-race` stage.

**Module status on go (37aad7f6), PROGRESS summary tables now synced with sections:** config 34 verified,
C07 partial, C34/C36 missing; agents 16 verified, 8 missing; routers 9 verified, 8 missing. 59/78 verified.
Routes 228/380 done on go (C34 alone adds 59).

**Gates running at handoff (coordinator background tasks; results in /tmp/*-gate.log on the Mac):**
- batch-1 (worktree ~/program/configflow-go-batch-1, branch batch-1 = go@215ab6b0 + A20 review + C07 review +
  N8-7 batch 1 + go-batch1-fix d7f3a15a [A20 adopts N8-7 redact API]) — /tmp/batch1-gate.log, on .32.
- O3-6c (~/program/configflow-go-verify-o36c, tmp-verify-o36c = go-o3-6c@426f6edd rebased) — /tmp/o36c-gate.log, .32.
- batch-3 PASSED on old base 215ab6b0 (~/program/configflow-go-batch-3: R10 review + R11 review). Not merged.
**Next merge step:** when batch-1 and O3-6c finish, stack on current go: batch-1 + batch-3 + O3-6c (go-o3-6c@426f6edd)
+ N8-5 (a)(b) (go-n8-5-6c@5e08cfb2, already stacked on O3-6c) → one cf-gate → ff-merge. Then notify: O3-6b
(6e5c7760; rebase + 28 subcases), R12/R13 (694df9d0), N8-5 (24d62bfc, start rebasing (c)), N8-7 batch 2 + N8-8a
(097eba4e), A21 (95eb529e; also needs C34 for DNSLoop), C36 (89c495b3; needs A20 setupflow + C34), A16/A17
(0d31fb93/2a90a68e; need A14), A14 reviewer 06515315 (finish A20 integration: drop duplicate reader in deploy.Wire,
register deploy.SetGatewayGuards, un-skip the O3-6c BeforeCommit regression, narrow the A18 baseline row).
**Batch after that:** go-review-c34 (4ad74cf4), go-test-speed-a@6fad06fc, go-test-speed-b@198a1c4e,
go-fix-repo-crosscheck@b4e8a2e7, A14 (after integration), go-gate-speed (6e5c7760, when handed back).

**In flight (agent → branch):**
- 6e5c7760: go-gate-speed (gate.sh: race test cache via PYREF_INPUT_FINGERPRINT, full = `go clean -testcache` then
  normal run on GATE_FULL=1 / 24 h stamp; GATE_ONLY=core|compare split; new synthetic-live stage for PY/REPO/DIFF
  tests the gate never ran — see parity/gate-env-coverage.md). Then O3-6b (go-o3-6b@91c5291f, waits R10+6c).
  Coordinator then updates ~/.local/bin/cf-gate: set GATE_FULL=1 when backend/ or shared core (lockx, repo,
  lifecycle, ctxx, yamlx, model, httpx) changed vs go; optionally split core/compare across .32/.64.
- 24d62bfc (Opus): N8-5 (c) on go-n8-5-6c.
- 097eba4e: N8-8(a) on go-n8-8a (from go-n8-5-6c); N8-7 batch 2 after O3-6c + C07 merge.
- 95eb529e: A21 on go-port-a21 (from batch-1). Clarified: C36 hook missing → 503 only when an unfinished setup
  session exists (reader fail-closed); DNSLoop wired after C34 merges (TODO(C34)).
- 694df9d0: R13 on go-port-r13 (from go-port-r12). Use C07's notifications.TransactionContext (in batch-1).
- 66383b28 (Opus): R12 HR review on go-opus-review-r12. R12 carries r12-batch-allowlist.patch (4th batch exception
  routers:saveAbortedPublication, D-R12-3 kept) to apply after O3-6c merges.
- 89c495b3: C36 port on go-port-c36 (from go-review-c34).
- 0d31fb93: A16 on go-port-a16; 2a90a68e: A17 on go-port-a17 (both from go-opus-review-a14).
- e46d698f / a5567935: test-speed A/B timing (docs-only, only when both servers' 1-min load < 10).
**Not started:** A18, A22, A23, R14–R17 (deps in flight). Then labs L2–L6, O9, O12, O14 switch.

**Decisions today (beyond those recorded in design Clarified sections):** HEAD read-only extended to C06/C34;
non-object JSON bodies 400 except Python's intentional null/missing default (per engine, as Python behaves);
base_url non-string 400; JWT junk chars 401; nonfinite stored numbers read as null; CFG-06 dedupe within one
call only; C33 keeps `.list.bak`; R11 F3 (block on edited owned script) accepted; R12 D-R12-3 kept as 4th batch
exception; O14 follow-up: apply the same pattern to R04–R11 F6; CFG-01 LOW: host aliases / DNS rebinding not
guarded in Python or Go (post-switch item).

**Open question to the maintainer (asked, unanswered):** three speed options — (1) incremental switch: Go serves
ported routes and proxies the rest to Python; (2) risk-tiered verification (lighter for logs/stats/settings);
(3) stop fixing non-security Python bugs during the port (record only). Coordinator recommended all three, (1)
first, with a "minimum module set for switch" estimate.

## (history) CURRENT STATE — 2026-11-17 evening

**First action for a new coordinator session:** completion notifications of running agents go to the session that
started them. Re-route them by sending every running agent below a one-line `mcp__paseo__send_agent_prompt` with
`notifyOnFinish: true` ("协调会话已更换，任务不变，继续做，完成后照常报告。"). Then poll each once with
`get_agent_status`; act on any that already finished.

**Pending coordinator actions** (updated 2026-11-18 ~01:30 by coordinator bb75f5c0)
Verify queue (use `cf-gate` from a temp worktree; RT_HOST pins; .64 uses -p 6):
  .32: go-fix-agg-provider-load (running) → go-n8-6 → go-opus-review-r08 (R08 verified, merge after agg fix)
  .64: go-review-a24 (running) → go-port-c32 → go-n7-4
  Then: go-port-a13 (ported) + go-review-a13 when done; go-port-r09 after R08+C32 (desired.go conflicts: keep one
  dnsGuardManifestSet, keep R08 Manifests(); C32 adds ctx to ComputeDesired/RulesetContent).
Merged since 19:10 (2026-11-17): O3-4, O3-5, N8-3+review, C31+review, N7-3a/b/c, O3 review, R07+review, A12+review,
  A24 (ported), N8-4a–d, installer locale, L1 report, gate.sh. Main repo PR #578 (Agent 1.8.62-go) awaits the user.
Decisions recorded in design "Clarified in …" (N7-3, N8-6, N8-4f, O3-4/5/6, A13, A24, R08 review: device I/O uses
  lifecycle.Detach). 20 proddiff baseline rows "deferred → A14" (A13); A14 must remove them (stale fails the gate).
Infra: gate.sh = 2 slots/server, both tags, race+proddiff parallel, concurrent fuzz; cf-gate wrapper on the Mac.
  .64 = 20 GB static + 20 GB swap. .32 often at load 50–130 (also other projects' rust builds).
Opus: N8-4e/f HR data review running (9669815f). Next Opus: R09 review, A14 review, N8-5 (repo typed API).
User may want faster switch; asked 2026-11-18, decided to keep the full plan (refactors before the switch).

**Planned after O3-6a merges (owner 6e5c7760):** O3-6c "shared commit points honour shutdown" — taskstatus
RecordContext/ForgetContext (lock-held ctx check, old entries kept), repo final commit ctx check, rulefetch
WriteValidatedRuleCache touch/rename ctx check, mosdns BuildMosDNSZip WithoutCancel if still there. C29/C33/A14/A20/R09
only add pre-call ctx.Err() checks in their own owners and switch to the ctx entries afterwards (WORKFLOW 4th item).
Shared C36 read-only reader: A20 (ProfileSessions) and A14 (wizard records) must converge on one package.
A13 deferral baseline is now 24 rows (adds content_sync.checked_at ×4); A14 must remove all 24. A14 also adds one
"deferred → A18" row (HA final generation). R09: Netwatch runtime-script ownership is a mandatory Opus check.

**Running agents** (Paseo project prj_b0033e1859522497; worktrees ~/.paseo/worktrees/302rx1dg/<slug>)
| agent | kind | task | branch |
|---|---|---|---|
| 9669815f-2b5e-4d92-9335-7f3629e9854a | Opus | N8-4e/f HR data review | go-opus-review-n8-4ef |
| 6b0691d3-aa3b-4eed-9b40-f2c9a9d559d8 | Codex | A14 publish (HR) | go-port-a14 |
| 0744d7f4-1e06-4dc0-ba4b-49b17f163bdb | Codex | A13 review | go-review-a13 |
| 5f08d05e-9f29-4c95-bed3-a7ab0efae7e5 | Codex | R09 (waits for R08+C32 merge, then rebase+gate) | go-port-r09 |
| 0d31fb93-a16c-48f8-8e5e-9c2756fb0e6f | Codex | C32 rule routes (final gate) | go-port-c32 |
| 95eb529e-0369-4d40-9205-13e4c4764e1c | Codex | C29 OxiDNS native apply (+ oxidns readers to yamlx) | go-port-c29 |
| 097eba4e-3a43-4bf8-9920-6e970f43faa7 | Codex | N7-4 writers (final gate), then N7-3d | go-n7-4 |
| 6e5c7760-36eb-4e1d-a5f0-1c378a081b14 | Codex | O3-6 locks (routers last, after R08/R09) | go-o3-6 |
| 66833acf-d1e4-45fb-b3bb-3b7d4bae0cbd | Codex | L1 installer defects D1/D3–D8 | go-fix-l1-installer |
| f3bfb2cc-33d9-492b-bb72-fa489c2a872d | Codex | (agg provider fix handed back) | go-fix-agg-provider-load |
| 3586bb0a-c53b-4bd6-8b8a-b69147e9f5a8 | Codex | slow test packages parallel (health/refresh/oxidns) | go-test-parallel-1 |
| e46d698f-6345-41b9-8a38-06d61b6622d3 | Codex | config Step B | go-difftest-config-b |
| c5332a35-f886-4d2d-90e2-f48496728a68 | Codex | routers Step B | go-difftest-routers-b |
Idle and reusable: e4c12b68 (A12/A13 author, ctx 321k), 19338c67 (N7-3b), 74bce4a1, d2f2e2f1 (A24 review), 7d6eb2a0 (A12 review), 12b84914 (proddiff load fix), c5332a35 after N8-3.

**Queue (in order of value)**
- Opus: review O3-1+O3-2+O3-3 together once O3-3 is merged; review N8-3 when handed back; HR reviews of A11, R07 when
  ported; O9 security review after O3 (include the YAML alias-amplification sweep, item 3d).
- Codex: N7-3/N7-4 (migrate YAML readers/writers to new yamlx, delete pyyaml; brief must include item 3d: alias class,
  never `yamlx.Clone` user values; corpus classification gate per design 5.4 "Open for N7-3"); N8-4..N8-11 after N8-3
  review; O3-4..O3-8 after O3-3; R08/R09 after R07 (to r12-release.md §2.4); R12-1..R12-4 (only need R03–R07);
  A12 → A13 → A14 after A11 (design a14-publish.md); A16 → A17 → A18 after A14 (design a16-a18-sync-follow-ha.md);
  A24 (non-HR); remaining config C29/C31–C36 and partials C06/C07/C11; Step B for agents and routers matrices
  (writer must not port in that domain); a fix for the agents `TestProxyDifferentialAgainstRequests`-style stale
  assertions is done (N8-2 review).
- Decisions taken today that porters must follow: D1 reject duplicate YAML keys; D2 Go-native persisted hashes;
  D3 whole-document rewrite; D-A17-1 Go-native (re-bind DNS follow once after switch); D-A18-1 retry when busy;
  D-A14-1 poll-driven deployment status; D-R12-1..3; M1 10 s stop grace; loopback always direct (httpproxy),
  ALL_PROXY fallback kept, CGI protection kept; NaN/±Inf in responses → null; Python 500s become proper 4xx.

**Coordinator rules learned today:** integrate/verify on a temp branch copy, never `git merge` in the shared worktree
before verification, never force-rebase a branch checked out in an agent worktree; always run ALL difftest packages
and the fuzz targets of changed packages in merge verification (they caught two misses today); full `-race` at the default
`-p` (go-fix-importer-bounds merged 2026-11-17; the `-p=4` workaround is no longer needed).

State 2026-11-17 ~15:00: Claude quota hit ~09:00–13:50 (Opus A19/R05 errored, resumed after reset); four Codex sessions were closed and resumed via send_agent_prompt (sessions persist). Merged since 08:00: N7-1, N8-1, and (integration of cherry-picks, verified on .32) C27 review, N8-2, C35 review, proddiff one-sided-retry, go-fix-config-findings (all difftest packages now green incl. api/agents). Known: importer resource-bounds test times out under full parallel -race → 0d31fb93 fixing (branch go-fix-importer-bounds); `internal/agents` TestProxyDifferentialAgainstRequests (CONFIGFLOW_DIFF) reported failing on go by the R05 porter — needs an owner. In progress — Opus: 53661fec R05 HR review (branch go-opus-review-r05 off go-port-r05), b9a2a95d A19 HR review (go-opus-review-a19 off go-port-a19). Codex: 097eba4e N7-2, 6e5c7760 O3-2 (rebasing onto go, conflicts with the lifecycle restart fix), e46d698f config Step B, 19338c67/c5332a35 idle. Coordinator lesson: build integrations in a separate worktree (never `git merge` inside ~/program/configflow-go before verification).
State 2026-11-17 ~08:00: O3 design merged (server/design/o3-concurrency.md; M1 decided: keep 10 s stop). Routers matrix O6 merged (all three matrices reviewed). Opus now: edd7e708 R05 iKuai port, ece3fa7f A19 Linux gateway core port (root of the agents publish chain A11→A12→A13→A14→A16–A18). Codex queue when one frees: O3-1 lockx, O3-2 lifecycle v2 (Opus review O3-2+O3-3 together), Step B for agents/routers (writer e46d698f after config Step B), A24 (non-HR). .32 load ~40 with many authoritative runs; consider a third native Linux test server if it persists.
State 2026-11-17 ~06:30: O2 proddiff review merged (netns DNS leak via host systemd-resolved fixed — isolation is now proven at start; headers compared; 23 self-checks; metrics window flake fixed; F1: A18 not ported → oxidns rules/update 500 for HA members, baselined). Weekly snapshot refresh rule added. Opus now: eed29a36 O6 routers matrix, edd7e708 R05 iKuai port (O5). Next Opus: O3 storage/background-task design, then HR review of R05 (fresh Opus).
State 2026-11-17 ~05:00: pushed go 888bbd53 — merged C05 review, C27 (`ported`), R04 Opus HR review (verified, 10 findings fixed), C30 review (verified), C35 (`ported`), N7+N8 design + maintainer decisions (D1 reject duplicate keys, D2 Go-native encoding, D3 whole rewrite), agents matrix O6. Known pre-existing failure: `internal/api/agents/difftest` TestDockerHADifferentialAgainstFlask counts the documented A09 deliberate difference as a failure → fixed in go-fix-config-findings. Running — Opus: f16937bf O2 proddiff review, eed29a36 O6 routers matrix. Codex: e46d698f config Step B (branch go-difftest-config-b, ~7000 variants, findings file), 27131008 config findings fixes (go-fix-config-findings), 19338c67 C27 review, 0d31fb93 C35 review, c5332a35 N8-2 store raw rows, 097eba4e N8-1, 6e5c7760 N7-1. Coordinator verifies every merge on .32 including ALL difftest packages (`for d in $(go list ./... | grep difftest)`).
State 2026-11-17 ~02:30: O6 config matrix review done (approved with additions, 6a29590d merged); e46d698f now implements config Step B (branch `go-difftest-config-b`, findings → server/parity/difftest-config-findings.md, coordinator dispatches them to porters). Opus now: 3da920fa N7+N8 design (got O6 inputs X01/X04/T29), 0438ff1a R04 HR review (O4). Next Opus: O2 proddiff review, then O6 agents/routers matrices. Codex: C05 review 19338c67, C27 port c5332a35, C37 review 48fb0e89, C30 review 0d31fb93 (new), C35 port 27131008. C30 merged as `ported`.
State 2026-11-17 01:25: Opus window started. Opus agents (Paseo, claude/claude-opus-5-5, thinking high, bypassPermissions): 3da920fa N7+N8 combined design (O1, branch `go-design-n7n8`, deliverable server/design/n7-n8.md); ebc70d81 O6 review of difftest-config.md (branch `go-opus-matrix-config`). Next Opus when one finishes: O4 R04 HR review (fresh, not author), then O2 proddiff review, then O6 for agents/routers matrices. Also merged since 00:40: C22–C26 verified (covered by S-oxidns review), C18 verified. Codex now: 19338c67 C05 review, c5332a35 C27 port, 48fb0e89 C37 review, 27131008 C30 port.
Earlier, 2026-11-17 ~00:40: also merged — C19/C20 (verified), S-oxidns (verified), C37 (`ported`), agents + routers difftest matrices. Three matrices (config/agents/routers) await Opus O6. Codex running: 19338c67 C18 review, c5332a35 C22–C26 status/review, 48fb0e89 C37 review, 27131008 C30 port (new, branch `go-port-c30`). e46d698f (matrix writer) idle on purpose: it writes Step B after O6 and must not port in those domains.
Earlier, 2026-11-17 ~00:00: merged + pushed — N2, proddiff harness (merge gate on .32), S-rules, R04 (`ported`, Opus O4 pending), config difftest matrix (`server/parity/difftest-config.md`, 228 routes, Opus O6 pending before Step B). e46d698f now does the agents matrix (branch `go-difftest-agents`). Session one-shot cron at 01:23 on 11-17 starts Opus: N7+N8 combined design (O1) + O6 matrix review, then O4 R04 and O2 proddiff review.
Earlier state 2026-11-16 evening: N2 and proddiff merged; 954058ca (N2) and 48fb0e89 (proddiff) finished. R04 (5ced41c3) handed back → coordinator verification, then merge as `ported`; Opus HR review (O4) next window. S-rules (245ed359) rebasing onto go after N2; S-oxidns (c5332a35) fixing a fuzz-found YAML block-scalar bug + slow -race test.
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
   A11 clarification: `convert.Download` still has legacy URL/LocalPath values; optional Behavior/Format
   carry deployment consumer metadata while public Object encoding stays unchanged. N8/S-convert must
   move Download into the unified typed model and remove this structural debt.
3c. **Route-pattern conflicts to solve when those routes are ported** (O6 routers review): in Go's ServeMux
   `/api/agents/gateway-installations/{session_id}` and `/api/agents/{agent_id}/upstream-routers` overlap and
   panic at registration; also `/api/agents/i/status` must reach the install short-link handler and
   `POST …/mihomo/rule-targets` the Mihomo proxy route as in Flask. The porter of the second route must
   resolve the precedence explicitly (and test it).
3d. **YAML alias amplification class** (A19 review F1, 2026-11-17): converting YAML with nested/shared aliases
   to jsonx or cloning per occurrence expands exponentially (44 MB at depth 18; self-reference → stack overflow).
   Fixed in internal/gateway; **O9 security review and N7-3 must sweep every YAML→jsonx/clone path** (convert, subs,
   oxidns, provider, importer) for the same class, with depth-40 alias-chain and self-reference tests.
   N7-2 review F1 (high) was the same class inside the editor (alias expansion over budget filled `null`, silently).
   **`yamlx.Clone` must never be used to copy values out of user documents** (no runtime caller left); N7-3 briefs
   must say so.
4. **Blocked until 1 exists:** N7 (YAML → yaml.v3 behind yamlx) and N8 (remove jsonx) Opus designs and their
   implementation, S passes of convert/subs/agents/repo, and any other large refactor of accepted code.
**HR modules since 2026-11-17: Codex ports, fresh Opus reviews** (WORKFLOW last section; Opus designs first for A14, A18, R12).
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

Test servers: `.32` and `.64` are equivalent (since 2026-11-17; `.63` WSL retired). `rt` load-balances between them;
pin with `RT_HOST` when a sequence of commands must hit the same copy. Merge verification + proddiff gate may run
on either; the coordinator usually pins one per verification worktree.

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

- **Never rebase a branch that is checked out in an agent's worktree from another worktree** (`git worktree add -f`):
  the agent's working tree then shows reverse diffs of everything that moved (happened with go-o3-2 on 2026-11-17;
  the agent verified and restored correctly). Ask the agent to rebase, or rebase a **copy** (`git branch tmp <b>`;
  rebase tmp; fast-forward go to tmp) and leave the agent's branch alone.

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
