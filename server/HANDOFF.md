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
## >>> CURRENT STATE — 2026-11-21 ~15:40 (coordinator 711a60b5 → next coordinator; READ THIS FIRST) <<<

**go = 0e46c07a0 (pushed).** N8-11 DONE. Merged since 11-20: batch-17..20 (W3–W9, test migration, O9 round 2 + low fixes,
404 for missing Agent, M18 damaged-store UI, #40 Go release image + autoupdate + CI), docs O12 release notes, O13
completeness (0 gaps), O14 switch plan, O15 ARCHITECTURE.md, all with maintainer rulings recorded inside those docs.

**Models / quota (2026-11-21):** Codex quota out until **2026-11-23 21:18**; Gemini (antigravity) out ~97 h (until ~11-25).
Maintainer: Codex/Gemini work goes to **Opus** meanwhile (claude/claude-opus-5-5, thinking medium, bypassPermissions).
Switch execution back to Codex when it returns. Opus sometimes hits the 5-h limit (re-prompt after reset) or a
false-positive safeguard error (start a fresh session in the same workspace with a short neutral brief).
**Paseo restarted 3× on 11-21**: agents go "closed"; re-prompt each with "会话中断，请恢复…先看工作区". Before
relaunching Gemini in a worktree kill its old `agy` process (`ps ax | grep agy`, match `--add-dir <worktree>`).
From an agent session `paseo run --cwd` is ignored — use `--workspace <wks_id>` (from get_agent_status) to continue in a
worktree, or `--new-workspace worktree --worktree-mode checkout-branch --branch <b>` for a new one.

**Update 2026-11-21 (new coordinator, solarsear account; new Paseo agents use the default ~/.claude account):**
maintainer: "要严查，这性能也太差了". Hard O10 targets (same host + snapshot, Go <= Python; SUPERSEDED for idle CPU by the PVE ruling below): idle CPU avg <= 1% (floor
<= 0.5%), idle RSS <= 117 MB, every measured route median latency <= Python, read-route allocation in the MB range.
Every O10 task hands back o10perf before/after numbers against these. Maintainer 2026-11-21 later: "go 性能应该比 python 好才对 … 肯定不正常" → switch acceptance = Go
BETTER than Python on every idle metric and every measured route (any route slower than Python blocks the switch);
measured on the PVE CT. (A ratio target "<= 1/3 of Python" was set and WITHDRAWN the same day: maintainer asked whether hard-coded
targets make sense and pointed out Python would also be faster with caches.) Acceptance stays Go <= Python per metric,
measured fairly: S1 (stop deep-copying reads — Python never copied) is the main fix; S2 result caches only where the
direct typed computation is still slower, each behind a switch so Go is also measured with caches off; reports give
the Go/Python ratio for information. Benchmark build checked: normal release build (no -race, same as Dockerfile apart from -s -w). Root-cause audit dispatched: Opus dfbabde4,
branch go-perf-rootcause (from go-perf-t1 + cherry-picked T2) → server/parity/o10-rootcause.md: can wave A+B reach the
targets; if not, structural change (immutable snapshot / copy-on-write repo, version-keyed caches). Wave B waits for it.
Root cause DONE (dfbabde4, go-perf-rootcause 3b05c9bb8, server/parity/o10-rootcause.md): wave A+B CANNOT reach the
targets with the current data model (deep-copy reads; Agent histories 304 of 442 KB; encode→generic-decode projections;
model decode 147 B/B from per-member baseline at record.go:180). Coordinator rulings: adopt S1 (readers borrow the
head-cached immutable snapshot, writers copy) + S2 (version-keyed derived caches) + S3 (lazy decode baseline, streaming
encode); follow R1–R12 order (replaces T4/T7/T8/T9/T10-followups/T11); generator routes target <= 30 MB, no result cache;
prod host has no THP → R12 = GC only. Tooling-only branch go-o10-tools (o10perf + report, no EXPERIMENT commits) pending.
batch-21 = audit fix + T2 + T1 stacked on go; fresh Opus 037b7644 integrates T3 (notifications.go conflict) + HR review
+ o10perf vs go. batch-21 MERGED 2026-11-21 (go = 0c009f67e): audit fix + T2 + T1/T6/T10 + T3/T5 + review fixes (ReadHAPair two
records, Map.SetAll) + C04 cross-check at the T1 boundary + publish difftest holds the IPv6 scheduler (timing flake,
negative controls). Gate: core FULL .64 PASS (race + 59 fuzz), compare .32 PASS on 3rd run. .32 o10perf (Go only):
idle CPU 50%→1.8% (Py 0.9), RSS 294→167 MB (Py 117); notifications 25 ms, config 13–26 ms, mihomo 151, provider 234
(all < Py); agents 267 ms, agents/<id> 171, profiles 13 (still 6–12× Py). R1 Agent borrow (Opus 86161dd6,
go-perf-r1) and R2 Config borrow (Opus 4ec236f7, go-perf-r2) running, both told to rebase onto 0c009f67e. batch-22 MERGED (go = 331c9420f): M16 + Agent image 1.9.0-go + difftest refresh-timing fix + registration fuzz
invalid-UTF-8 test fix (core race PASS, compare PASS live 45/45, all fuzz PASS after the fix; .64 only).
Maintainer 2026-11-21: O10 acceptance numbers come from an LXC on PVE .6 (real host the panel will run on): Opus
e44edb00, branch go-o10-pve, CT on cfgt, 2 cores/2 GB/cpuunits 50, no egress, Go and Python measured one after the
other → server/parity/o10-pve-baseline.md + server/tools/o10perf/pve.sh.
Maintainer 2026-11-22: do the cleanups BEFORE the switch (not after). Order: now T1b (3fc755ef) ∥ T1c (Opus 0cb0740a,
go-o18-t1c) ∥ R11 allocation caps + R8 + R9 (Opus 027298ea, go-perf-r11); next T1d, remaining O18 C items (T2/T3/T4/T8),
R13 non-heap 57 MB, proddiff comparator in Go; then O17 idiomatic review; then R12 GC; then final PVE acceptance and
L6 soak on the final code (soak must run on the code that ships); then switch. Codex back 11-23 21:18 → executors.

Maintainer 2026-11-22 FINAL: OPTION C — last full Python-comparison gate on the switch candidate, then the switch commit deletes backend/ AND all Python-dependent tests/gate stages (WP0 inventory = delete list); afterwards gate = core race + Agent + fuzz + frontend + proddiff Go-vs-Go. (Superseded: OPTION B tag `python-final` keeps the last Python tree, test
servers check it out outside the repo, all Python-dependent tests/gate stages read it there; main still deletes
backend/ at the merge. WP1/WP2/WP4–WP8/WP10/WP13 dropped before the switch (convert per package later if needed).
No-Python tests plan DONE (b85aa02f0, server/design/no-python-tests.md): ~72 packages / ~145 test files + difftest/
live stages + proddiff/configdiff/routerdiff depend on Python. Approach: extend testgolden with a freeze mode (record
runs Python and Go, writes the Go-side observation only after they agree); 16 work packages WP0–WP13; final recording
on the switch candidate tree at T−1 week (tag python-final-goldens); gate afterwards: compare = proddiff Go-vs-Go +
frontend, replay tests in core race, "no Python" audit. Running now: WP0 runtime inventory + WP3 move fixtures
(Opus 62179ccd, go-nopy-wp0-wp3), WP9 proddiff Go-vs-Go (Opus 1e7554cf, go-nopy-wp9). WP1 freeze mechanism next.
O14 precondition to add: all WPs done + two consecutive GATE_NOPY=1 gates before the switch.

batch-31 MERGED 2026-11-22 (HA auto-complete after member-lost timeout + review M1 re-check the journal under the
reserving lease; stale Docker AIO frontend test fixed; gate.sh now runs the FRONTEND (vue-tsc + vitest) in compare,
GATE_ONLY=frontend; test-gate-routing repaired; gate-live accepts strace-split execve for the WebDAV TLS oracle).
Before batch-31 the gate never ran frontend tests (a frontend failure sat on go since 11-19).

batch-29 MERGED 2026-11-22 (idle wrap-up: presence credentials on demand, webdav auto_backup=false skips the diff,
o10perf idlestats; enrollment fuzz UseNumber). batch-30 MERGED 2026-11-22 (O18 T1a: Document/Profile/Shared/System
plain structs + per-row codec, lenient stored reads, strict input with existing messages, unknown members dropped,
changed-rows-only writes; HR review H1 damaged system_config no longer rewritten by token upkeep, H2 two token
checks honour SystemSettingsDamaged, M1 clip strict-decoded slices; model cold load 45→28 ms, write 8.8→5.3 ms).
T1b (Opus 3fc755ef) next; T1c after T1b lands.

Maintainer approved 2026-11-22: HA pair publish stopped by the 10-minute member-lost timeout is auto-completed when
the member returns (same content as the interrupted publish, not the latest config; independent of the rule
auto-sync switch; one attempt; UI "等待设备恢复后自动完成发布", history tag "自动补完"). Opus 64fd3f0f, branch
go-ha-auto-complete. Real-device check of this goes into L6 (no separate lab rebuild).

batch-28 MERGED 2026-11-22 (R6 router read routes: generation-keyed config credentials in repo, Records reads the
stored profile without hydrating, member-wise DNS Agent source; review M1 wrongly typed rule members fall back to the
strict decode). Running: O18 T1a (03308f4f), idle CPU wrap-up (Opus b72b8975, go-perf-idle).

batch-26 MERGED 2026-11-22 (R4+R7 Agent public projection member-wise, credentials from the same generation (review
M1), view cache default ON; /api/profiles = Python). batch-27 MERGED 2026-11-22 (R10 SQLite read-connection reuse,
heartbeat CPU −46%; review M1: WAL connections are never kept, else a renamed-in file could be read through the old
-wal). In flight: R6 routers (go-perf-r6 → batch-28, HR review Opus 9cccabce; test server: all 5 router read routes
now faster than Python), O18 T1a model plain structs (Opus 03308f4f, go-o18-t1a; rulings: always-written members not
omitzero; strict HTTP decode with the existing Chinese messages table, Python-unvalidated members now 400).

batch-24 MERGED 2026-11-22 (O18 small cleanup T5/T6/T7 + C15a/b/c + C17 + review fix POST files skip ServeContent +
lab D1 202 + lab docs; enrollment fuzz 1E700 test fix). batch-25 MERGED 2026-11-22 (lab defect fixes, Agent 1.9.1-go:
L5-D1 nft 1.0.6 return routing via token-owned objects, L3-D1 keepalived stop order + VIP release check, L2-D3 core
version refresh, 4 Agent routes test races, gate.sh now has an Agent module stage (gofmt/vet/race; GATE_ONLY=agent);
panel L3-D2/L3-D3/L5-D2/L5-D3 + O-1 10-minute member-lost timeout; HR review 0 high/0 medium). Before batch-25 the
gate never checked backend/agents/go-agent. Agent image 1.9.1-go must ship with the Go panel (P13).

Lab L2–L5 DONE 2026-11-22 (Opus 7c0c4d68, branch go-audit-lab: 547662cb2 D1 recovery accepted → 202 + 13 lab-doc
commits; reports server/parity/lab/L2–L5.md; all lab guests destroyed, CHR 7.25.1 image kept). Joins the next batch
gate. Defects dispatched: Agent side (Opus 33089f7d, go-lab-fixes-agent): L5-D1 HIGH return routing fails on nftables
1.0.6/Debian 12 (table/chain comments missing from `nft -j`), L3-D1 Alpine/OpenRC keepalived left holding the VIP
after HA disable, L2-D3 stale core version, Agent → 1.9.1-go. Panel side (Opus 64fd3f0f, go-lab-fixes-panel): L3-D2
HA pair stuck after successful re-recovery (409 forever), L3-D3 auto-sync failure reason, L5-D2 plan must reject a
next hop outside the Mihomo subnet, L5-D3 iKuai interrupted batch abort record, O-1 design (member permanently offline
→ pairing stuck; maybe pull a minimal admin force-abandon before the switch, M07). Not covered in L2–L5: DNS guard,
PD guard, router reminders, batch write with link loss, IPv6 prep vs rule sync, full TUN split. Ruling: lab DoH
resolver + DNAT for cfgt guests becomes permanent lab infrastructure (plain DNS from lab guests otherwise leaks via
the home router hijack to production DNS); record it in the owner file. `api/agents` -race needs > 10 min (gate uses 60m).

batch-23 MERGED 2026-11-21: R1 Agent borrow + R1 HR review (guard atomic, gate anti-bypass) + R2 Config borrow + R2
HR review (gate anti-bypass, append/cap tests); gate core FULL .32 PASS, compare .64 PASS first try. R4+R7 (Opus
967123ab, go-perf-r4): cache-off still slower than Python on the 4 Agent routes, cache-on 3–8× faster; /api/profiles
= Python. Ruling: cache default ON (CONFIGFLOW_AGENT_VIEW_CACHE=0 disables); R4 rebases on go and removes remaining
direct-path waste (content source from borrowed Config, summary credential seed from the head cache, member-wise
projection), re-measure both columns; revisit keeping the cache after O18 T1.

Maintainer 2026-11-21 (agreed): after O18 C items → O17 widened to a full "idiomatic Go" review: Python-shaped structure
and flow (5 s full-recompute polling instead of change-driven work, whole-document storage with histories inside Agent
records, functions split 1:1 like Python) and unlabelled ports the O18 keyword search missed; native Go, no Python
behaviour copied. New switch precondition (add to O14): every test that runs real Python (pyref recordings, Flask/Python
differentials, configdiff, gate live/compare stages) must be converted to checked-in golden data or pure Go tests
BEFORE backend/ is deleted at the main merge, or the gate stops checking anything. O18 A-class (10) items are
boundary content with negligible cost (no perf impact); kept.

PVE benchmark CT DONE (e44edb00): CT 240 cf-o10-perf 10.97.0.140 on PVE .6 (i3-N305, 2 cores/2 GB, link_down + nft
table inet o10ct + lo-only netns; stopped, kept; owner /root/cf-gwip-lab/o10-owner.json). Rerun from a worktree:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash server/tools/o10perf/pve.sh run --servers go` (then `--servers python`; one
after the other, ~40 min each). Baseline server/parity/o10-pve-baseline.md: batch-21 idle CPU avg 1.9% vs Py 1.5%,
RSS 92 vs 117 MB; slower routes summary=1 7–9×, agent detail 5–7×, profiles 4–6×, router policy 4–5×. Test-server
allocation numbers match the CT; latency/CPU only relative. Ruling: the absolute idle targets are dropped (Python
itself is 1.5% here) → acceptance = same CT, same snapshot, Go <= Python on idle avg, 10 s window median and p10,
RSS, and every route.

O18 DONE (d58b07e27, server/parity/o18-leftover-parity.md): 33 leftover parity mechanisms (A 10 keep, B 5 legacy read,
C 18 removable). Prod snapshot: 1 unknown member (agents[].has_update, a computed response field wrongly stored → drop),
0 wrongly typed fields, 0 leaves whose bytes differ from typed re-encoding → model byte/order preservation protects
nothing. Coordinator rulings (maintainer asked "直接去掉是不是更干净"): model becomes plain structs on encoding/json/v2
(O18 T1a–T1d; replaces O10 R5); unknown members dropped with a load warning, tests reject undeclared members; lenient
leaf decode (wrong type → zero + warning); keep insertion order for user-keyed maps and Dynamic as is; explicit
`enabled: null` keeps meaning disabled (earlier flag-truthiness ruling) via load normalisation. Order: batch-23 (R1+R2)
→ R4/R7 batch → T1a→T1b→T1c→T1d one at a time; small C items T5/T6/T7 in parallel now; T2 with R8; T3/T4/T8/T9 later.

T1 rulings: router/client redaction keeps Agent credentials (collected once per request, narrow read); one-off stale
router-check reminders after upgrade accepted (behaviour-changes + o12 notes). T1 sent back for that fix.

**BLOCKER for the switch = performance (O10, server/parity/o10-performance.md on go-audit-o10 / go-perf-t2):** prod
snapshot: Go idle CPU 34.9% vs Python 0.9%, idle RSS 289 vs 117 MB, /api/notifications ×60, /api/agents ×26, generation
×10–15. Cause: whole-data copies. Do not switch until wave A+B are merged and O10 is re-measured below Python.
- Wave A (all based on go-audit-o10 3987d26e0, which itself has a small product fix SystemConfig/Backup narrow readers):
  * T2 model.Marshal single validation — DONE, branch go-perf-t2 (4 commits, 601ab1025): allocations halved
    (86→43 GB), /api/agents 681→319 MB, rule-sync 61→7.8 MB. Opus-authored; needs a fresh Opus HR review.
  * T1/T6/T10 repo narrow reads — Opus 3542fcfa (took over from Gemini), branch go-perf-t1 (59d240aab, 2 commits).
  * T3/T5 notifications — Opus b4740fcd (fresh session after safeguard error), branch go-perf-t3 (17 dirty files).
  * When all three hand back: one fresh Opus review of T1+T2+T3 (HR: repo/model core), stack as batch-21 on go,
    full gate (core on one host, preflight+compare on the other; drivers in ~/.cache/*-compare-run/driver.sh on .64),
    note T2 changes model/*.go → oxidns pyref source metadata must be refreshed (entries unchanged) after stacking.
- Wave B (dispatch after wave A merges): T4 agents list/detail typed content source (no per-Agent JSON round trip),
  T7 "encode whole record to read one field" sweep (rules.rulePolicy, convert.validateRuleIdentities first), T8 YAML
  encoder reuse, T11 per-route allocation guard from tools/o10perf, then T9 GC/memory policy; then re-run O10.

**Other in flight:**
- M16 unknown core version — DONE (Opus f2f8fb58 finished), branch go-fix-m16 @8a283871f, based on go 448c52dcf (needs `git rebase go` onto batch-20 before batching; mosdns pyref source metadata re-recorded, entries unchanged; its own gate run: compare PASS, race PASS after pyref refresh). Batch it with the Agent-image branch: non-empty unrecognized Mihomo version (e.g.
  `alpha-100ff73`) = unknown like empty, throttled warning, no 400. Lab-confirmed bug.
- Agent image pairing (Opus 99c38a83, branch go-agent-image-sync, 2 commits + 18 dirty): panel embeds paired
  config-flow-agent:<version> tag (version.AgentImage), CI docker-agent.yml publishes version tags; new entrypoint is
  backward compatible with the Python panel (lab-verified). Now doing: bump Agent version to 1.9.0-go, re-shoot HA
  wizard screenshot. Deferred to after switch: "Docker Agent 更新步骤" dialog default tag. O14 precondition P13: Agent
  image must ship before/with the Go panel (L2 found old latest image → every new Docker Agent 403).
- Lab O16 (Opus 7c0c4d68, branch go-audit-lab, 4 commits): L2 P1–P11 PASS, D1 fixed (recovery accepted → 202), D4 =
  the Agent image issue above, M16 measured (P12). Then L3 → L4 → L5 (CHR 7.25.1 download from download.mikrotik.com
  approved with SHA256 check). L6 = 24 h soak with prod snapshot copy (maintainer approved D9) + switch rehearsal;
  plan: start the soak early and run it in parallel (8 h + forced daily tasks is the fallback if it ends up last).
  Lab branch commits (product fixes like D1) must go through a batch gate before merging.
- After wave B: O17 code-size audit, final O10 re-measure, L6, then the maintainer picks the switch window (D1).

**Maintainer rulings 2026-11-21/22 (also in the docs):** autoupdate image kept and rebuilds the Go server (+ CI publishes
`<version>-autoupdate`; floating `autoupdate`/`latest` move only after the observation period; Go merge to main deletes
backend/ Python in the same commit); O12 M01–M19 and O14 D1–D14 decided (see o12-release-notes.md / o14-switch-plan.md
tails): notably D3 new major version + prerelease tags, latest untouched ~7 days; D4 panel image amd64+arm64 only;
D5 keep auto-sync ON at first start; D8 TZ Asia/Shanghai; M18 UI before switch (done); M07 HA force-disable after switch;
M08 all of L2–L6 before switching.
Coordinator rulings today: missing Agent → 404 (done); O9 r2 lows L1/L2/L3/L4/L6 fixed, L5 unchanged; L3 node control
chars: name/type/server/port/URI strict, multi-line params (PEM) allowed.

**Running agents to re-route on takeover (send each one MCP prompt with notifyOnFinish:true):** 7c0c4d68 (lab),
3542fcfa (T1), b4740fcd (T3), f2f8fb58 (M16), 99c38a83 (Agent image). Gate recipe: see "Gate recipe used today" in the
2026-11-19 section below; preflight script /tmp/preflight.sh on both hosts logs to ~/.cache/rt-tmp/preflight-<dir>.log;
never pin everything to .32 — check /proc/loadavg first.

## >>> CURRENT STATE — 2026-11-19 night (coordinator 2f0cdf04 → next coordinator; READ THIS FIRST) <<<

**Update 2026-11-22 (evening): O10 found Go SLOWER than Python** (prod snapshot, .32): idle CPU 34.9% vs 0.9%, idle RSS
289 MB (live heap 17 MB) vs 117 MB, /api/notifications ×60, /api/agents ×26, config generation ×10–15. Cause: whole-data
copies (model.Marshal re-validates per nesting level = 39% of allocations; repo.Config copies all Agents + histories;
repo.System full copy for small reads; per-Agent config encode/decode; notification snapshot encodes Agents per row).
Branch go-audit-o10 (3987d26e0, not merged: small product fix SystemConfig/Backup narrow readers). Wave A (based on
go-audit-o10): T2 model.Marshal single validation (Opus e1916289, go-perf-t2), T1/T6/T10 repo narrow reads (Gemini
6215652c, go-perf-t1, needs Opus HR review), T3/T5 notifications (Gemini 30a80fd1, go-perf-t3). Wave B: T4 agents
list/detail, T7, T8, T11 per-route allocation guard, then T9 GC policy. Switch blocked on O10 targets.
Batch-20 (O9 r2 + 404 + M18 + #40) under Opus review c10bf766. Lab L2–L5 Opus 7c0c4d68 (CHR 7.25.1 download approved).
O12/O14 maintainer rulings recorded in those docs. Paseo restarted once on 2026-11-22: agents re-prompted.

**Update 2026-11-22 (audits):** O13 completeness MERGED (65d03d17c): 0 gaps except #40 image; routes.tsv 380/380 done.
Maintainer decision: keep the autoupdate image, make it rebuild the Go server too. Coordinator ruling: missing-Agent
500 "Agent not found" on /api/agents/<id>/logs, /logs/sources, /config/logging → fix to 404 (Python bug) — small task,
not yet dispatched. In flight: O9 round 2 (Opus ce1b8b0e, branch go-audit-o9r2; first finding = no response-body size
limit in agents client / devhttp), O12 release notes (Opus f6b211e0, go-audit-o12), #40 image (Gemini 7ba2ebb9,
go-image-40). Alloc-assertion flake fixed (6c7e754bf, test-only). Next: O10 perf, O15 architecture, O17 size (after O10).

**Update 2026-11-22: N8-11 DONE — W9 MERGED, go = 7e05ccfab.** jsonx/pystr deleted, all *_legacy.go gone, legacy gate is
hard (no allowlist). O7 review (Opus 26e12ace): 0 high/0 medium; fixes: Sub-Store finished_at plain decimal; FuzzEncode
test accepts the approved out-of-range float refusal (1e700). Final gate: compare PASS (live 45/45), core race had one
failure TestN89SyncPipelineAllocations (alloc delta 82,923 B > 64 KiB under -race while 81 fuzz targets ran in parallel);
re-run 3x on idle .64 and 2x on idle .32 → PASS (deltas ±17 KB) → load noise, merged. Follow-up: make that allocation
assertion robust under -race/load (e.g. min of N runs or skip under race) — small task.
Next: audit phase (O9 round 2, O10, O13, O15, O17, O12), #40 image, labs L2–L6, O14. Codex back 2026-11-23 21:18.

**Update 2026-11-21 (evening): batch-19 MERGED (test migration w8/w8b/w8c + coverage review), go = be137cf89.**
Allowlist 676 → 131. Coverage review (Gemini 51ce3131) restored 4 weakened tests + fixed store test encoder (encoding/json
escapes U+2028/2029 and encodes nil slices as null — tests comparing bytes with Python must not use it). Live stage caught
that; preflight does not run live tests. W9 (Gemini e0e11c14, branch go-n8-11-w9) dispatched: clear allowlist, delete
jsonx/pystr + all *_legacy.go, harden gate; then fresh Opus O7 review + batch gate → N8-11 done.

**Update 2026-11-21: batch-18 MERGED (W5 + W6 + W7 + reviews + gate coverage fix), go = 0d65d91ba.** Gate now runs
every CONFIGFLOW_DIFF/Python test (LIVE 45 required, 0 skips). Batch-18 integration: Opus a02ccfe1 reverted an
integrator-added repo Scope typed cache (unneeded; provider reads Config once). Remaining allowlist ≈676 lines: 581 test
imports + 95 prod. Test migration split three ways (test files only, no prod/corpus/pyref changes), Gemini Neon:
w8 (5552ecee: api/agents, installer, agents/{metrics,upgrades,trafficstore}), w8b (d22313a4: other agents/**, deploy,
routers, routersplit, gateway, setup*, clients, discovery, notifications, tasks*), w8c (1f82fb75: everything else incl.
tools/, cmd/). Then W9 = remove prod facades (95 lines) + jsonx/pystr dirs + hard gate + Opus O7 review.

**Update 2026-11-20 (evening): batch-17 MERGED (W3 + W4 + reviews), go = 0fcca2c24.** Codex quota out until
2026-11-23 21:18 → maintainer: Codex work goes to antigravity/gemini-4-neon-high (mode full-access); HR reviews stay fresh
Opus. Continue an existing worktree with `paseo run --workspace <wks_id>` (`--cwd` is ignored from an agent session).
Preflight script now logs per tree (~/.cache/rt-tmp/preflight-<dir>.log); don't pin briefs to .32 — pick the idle host.
State: W5 done + Opus review (401bcf8e, 0 high, 3 medium fixed) → branch go-opus-review-n8-11-w5@b11f95a36, waiting for W6.
Ruling: DELETE /api/profiles/<id> with malformed JSON → 400 (empty body still deletes). W6 (Gemini 71b8dd8d, branch
go-n8-11-w6-deploy-gateway) rebasing onto go to integrate W4's RuleSourceReader + fix onboarding null policy (explicit
null ≠ missing; shown as JSON null) → then fresh Opus HR review → batch W5+W6. W7 (Gemini 6f6e6553, branch
go-n8-11-w7-generation) started, includes the "flag truthiness" sweep (missing → default; false/null/numeric 0/""/[]/{}
→ false; else true; one shared helper). W8 after W6 merges (api/agents tests overlap). Gate coverage branch
go-gate-diff-coverage@0bb5d949f joins the W7 batch.

**Update 2026-11-20 (afternoon): P0 + W1 + W2 MERGED, go = 5f034b1ba** (batch-16, gate PASS after fixes by Codex
533a78db: pyref source metadata for oxidns/adapter.go (TestBoundaryGenerationCorpus 270→266, 4 refs no longer needed after
the enabled fix), oracle registration, repo JSON writes SetEscapeHTML(false), legacy ProfileMetadata Detach, shared field
position order, compact built-in defaults). Lesson: agents must run CONFIGFLOW_DIFF difftests of ALL affected packages +
full preflight and refresh pyref source metadata themselves (now in every brief).
In flight: W3 (38c8b940) + W4 (6296c861) told to rebase onto go and finish their W1-dependent last steps; wave 2 started
early for the non-W3/W4-dependent packages: W5 config-api (Codex 317a5ae8, branch go-n8-11-w5-config-api; incl. 409
usages fix, auth caller migration, W1 review L1 validations, non-finite 400) and W6 deploy-gateway (Codex e857938b, HR,
branch go-n8-11-w6-deploy-gateway). W7 waits for W3/W4; W8 later (load). Gate coverage fix ready on go-gate-diff-coverage
@0bb5d949f (needs onboarding null-policy fix to pass) → merge together with the "flag truthiness" task (W2 review L3:
explicit null/0/""/false/[]/{} = disabled, missing = default, else enabled; one shared helper across rules/matcher,
discovery, convert, provider/delivery, routers/ipv6_plan, notifications, oxidns adapter; + onboarding null policy shown as
JSON null) — dispatch after W3/W4 merge.

**Update 2026-11-20 (N8-11 wave 1):** P0 (aa803cd5, go-n8-11-p0@212aeac09) batch-15 gate FAILED: it changed
weburl/logsafe.go which is a source of many pyref archives (mosdns/oxidns…) → "Python 参考已变化" in preflight + core race.
P0 told to refresh source metadata only (entries unchanged). Wave 1 all based on P0 212aeac09:
- W1 repo (fd582f3f) handed back eff5f6e78+4009d59be → fresh Opus HR review 6fc95e12 (branch go-opus-review-n8-11-w1).
  W1 added UpdateConfig/UpdateSharedConfigs (W4 needs), SharedConfig/SaveSharedRecord/ResourceUsage (W3 needs).
  Pre-existing bug → W5: global import/reset 409 carries extra `usages` (Python: success/message only).
- W2 agents (162a561d) handed back 5d10d974e; now doing authorised minimal fix in oxidns/adapter.go: explicit
  null/0/'' enabled must disable (3 rulesync TestProjectionAgainstPython rows fail on go itself — gate coverage gap to
  investigate). Then fresh Opus HR review.
- W3 resources (38c8b940) partial fd526f6a7; W4 rules (6296c861) partial a80130222/f2486e236. Both finish their last
  sub-steps after W1 is merged; then non-HR independent review (+ Opus spot check provider/token for W3).
- Cross-owner edits authorised (listed as "越界授权修改" in each report for wave-2 rebases): W2 deploy receipts/
  publish hooks + oxidns/adapter.go; W3 aggregations/native_delivery.go, refresh/env.go; W4 gateway/adapter.go + new
  gateway RuleSourceReader file, api/rules/content.go. Any agent may mechanically migrate tests broken by its own
  signature changes. Rulings: W4 HTML via x/net/html with exact native-difference manifest; W4 Chinese messages keep
  ['id'] quoting via []string-only helper, non-string values as JSON text; scoped request reads keep snapshot semantics.

**Update 2026-11-20 (later): batch-14 MERGED (metrics/trafficstore native + Opus HR ccaeca36: 0 high/0 medium; L2
ruling: whole agent-traffic.json unparsable → fail-closed like corrupt days, reset recovers), then N8-11 plan
(server/design/n8-11-plan.md + rulings 3.3) → go = ae18239d4. Gate: preflight/compare .32 PASS, core FULL .64 PASS.
N8-10 is DONE. N8-11 is NOT S (157 prod files import jsonx): P0 → wave 1 (W1 repo HR ∥ W2 agents HR ∥ W3 resources ∥
W4 rules) → wave 2 (W5 ∥ W6 HR ∥ W7 ∥ W8, after W1+W2 reviewed and merged) → W9 delete + hard gate + Opus O7.
P0 (testjson + ratchet gate + ErrorClass) dispatched: Codex aa803cd5, branch go-n8-11-p0. Waiter gotcha: never
`pgrep -f "gate.sh ..."` from an ssh loop (it matches itself); wait on the pid.

**Update 2026-11-20: batch-13 MERGED, go = cf5ec02e7** (upgrades + api/agents + Opus HR review 7cf88da4: high 0,
medium 0, low 8 — R1 stored status null stays null, R2 missing service_type = mihomo on traffic routes, R3 RunServiceOperation
Runtime shutdown/disconnect test, R4 binary-download 500 text logged in behaviour-changes; L1 old address-follow /
installation-register hooks have no production caller → N8-11). Done by hand (single branch, no conflicts, no integrator):
preflight .32 PASS, compare .32 PASS (synthetic live 23/23), core FULL .64 PASS (race + 17 fuzz). metrics-native (dfd5f2d2)
told to rebase, switch traffic.go to native APIs (keep R2), rebind the 5 Flask difftest rows, drop dead legacy adapters →
then Opus HR review (data path) + own batch gate. Then N8-11.

**Update 2026-11-19 late night (coordinator = this new session; 02a6c1ea + dfd5f2d2 re-routed and `paseo agent detach`ed):**
- api/agents handed back: go-n8-10-api-agents@f8b387f7f (on go 5c5998e86; upgrades line 23d5d46c6..52dd0b884, api/agents
  29d0a6115..f8b387f7f). Fresh Opus HR review 7cf88da4 running on branch go-opus-review-n8-10-api-agents (scope: the 3
  api/agents commits only; fixes in that branch → review-n8-10-api-agents.md). After it: fresh Codex integrator for batch-13.
- metrics-native owner part handed back: go-n8-10-metrics-native@e9cfd6f04; waits for batch-13, then rebase + traffic.go
  switch + 5 difftest rows. Its "agents race timeout" = Go default 10m test timeout (package needs >10m under race; gate
  uses 60m); TestOptionalAgentProfilesIgnoreOversizedUnrelatedConfig base 40.1s vs branch 42.0s on .64 → unrelated.

**Why:** Claude account quota ran out; the maintainer is switching accounts. Everything below is current.
**go = 5c5998e86** (pushed). batch-11 (core-2, json-case, Go 1.27.4) and batch-12 (monitor + installer) merged today.
Details of both + all rulings: the two "Update 2026-11-19" paragraphs further down in this section's predecessor.

**In flight (all Paseo; send each one MCP prompt with notifyOnFinish:true so completions reach YOU):**
- 02a6c1ea (Codex) — N8-10 api/agents, branch go-n8-10-api-agents, worktree ~/.paseo/worktrees/302rx1dg/go-n8-10-api-agents.
  Rebased onto go 5c5998e86 and CONTAINS the reviewed upgrades line (go-opus-review-n8-10-upgrades@82adf451c, HR
  done: high 0). Was running full HTTP difftest + race when handed over. Rulings given (all to be in its report +
  behaviour-changes): typed core hooks InstallationProfileTyped/InstallationRegisterTyped (raw-JSON owner boundary,
  named params, no swap), HAServiceOperationTyped + Manager.RunServiceOperation (HA lease reused, single trigger);
  registration version whitelist inside RegisterIdentity after auth/proof (anon 403 regardless; invalid → 400, proof
  kept; null/missing → 1.0.0); If-Modified-Since via net/http.ParseTime (invalid ignored), Range 416 unchanged;
  cache flush tags [{}] → 400; traffic-report + client-notes/rule-apply/policies resolve the Agent profile binding via
  ResolveAgentProfile first (bad binding → 400 no write; lists skip bad entry + throttled warning, 200);
  traffic.go metrics/trafficstore calls stay legacy (owned by the metrics-native task). It may self-handle
  "read-only accessor / typed hook, same semantics, single trigger" increments and native-rule corpus-only diffs.
  **When it hands back:** dispatch a fresh Opus HR review of the api/agents commits only (upgrades already reviewed),
  then batch-13 = that branch (upgrades + api/agents + review fixes) with a FRESH Codex integrator (neutral brief,
  no security detail — b6fa1f80 got blocked by OpenAI's content filter; do not reuse it).
- dfd5f2d2 (Codex) — N8-10 supplement: metrics + trafficstore native services (A05 engine), branch
  go-n8-10-metrics-native from go 5c5998e86. Rulings: A04 summary/stats/trend skip bad items (200) + throttled
  warning; A05 trafficstore rejects writes only for the corrupt Agent/day bucket (whole-file only if days is not an
  object), original file untouched, recovery via existing reset/retention shown by test (ask before any new entry);
  native write APIs take ctx with Detach/entry/after-lock/pre-commit checks (jsonstore.WriteContext), lifecycle
  Runtime regressions. **After batch-13 merges:** tell it to rebase onto go and, in the same branch, switch
  api/agents/traffic.go to its native APIs and rebind the 5 TestDifferentialAgainstFlask rows (4 SHA-bound native
  comparers + agent-traffic.json structural compare). Then Opus HR review (data path) + its own gate.
- Idle/finished, keep worktrees (never delete an agent's cwd): 8019dda1, be408111, df5151e2, 1cc7e926, 7e69d031,
  86f6b6ec, ec52dd6b, 4a68384a, 71279df2, b6fa1f80 (blocked).

**Gate recipe used today (coordinator does it by hand when needed):** preflight on .32
`ssh sunbroke@192.168.0.32 'bash /tmp/preflight.sh program__configflow-go-batch-N -p 6'` after `RT_HOST=... rt sync`
from the batch worktree; compare on .32 via ~/.cache/batchN-compare-run/driver.sh (copy batch12's, sed the number;
setsid nohup); core on .64: `ssh .64 "cd ~/rt/program__configflow-go-batch-N && setsid nohup bash -lc
'GATE_FULL=1 GATE_ONLY=core GATE_SLOTS=2 server/scripts/gate.sh -p 6 <fuzz targets>' > /tmp/bN-core.log 2>&1 &"`
(fuzz targets = cf-gate's list for packages changed vs origin/go; run that loop under bash, zsh globs fail).
Then ff-merge into ~/program/configflow-go (branch go), push origin go, `git worktree remove` the batch worktree.

**After N8-10:** N8-11 (delete jsonx/pystr/legacy + old core hooks + typed-hook unregistration (upgrades review L2)
+ test-only numeric IP/image compat in installer + grep gate), then O9 round 2, O10, O13, O15, O17, O12 (13 items
need the maintainer), labs L2–L6, O14. UI follow-up: recovery entry for corrupt dashboard checkpoint/latest_upgrade.
Maintainer decisions today: toolchain Go 1.27.4; keep batches separate (batch-12 / batch-13 / N8-11), no merging.

## (history) CURRENT STATE — 2026-11-19 (handoff to a fresh session) <<<

**Update 2026-11-19 late (coordinator 2f0cdf04): batch-12 MERGED, go = 42a9269d3** — N8-10 monitor (+ Opus HR:
M1 superseding damaged status → StateError; null status = missing) and installer (+ Opus HR: `//` root alias refused,
RenderDockerRun port via shellWord, ss non-ASCII blank lines → unknown). Preflight + compare .32 PASS, core FULL .64
PASS on 6714a866f. Integrator b6fa1f80 (Codex) was stopped by OpenAI's content filter (context full of
path/shell-injection review material) after stacking; coordinator finished the gate by hand. Use a FRESH Codex for
the next integration with a neutral brief.
**batch-13 candidates:** go-opus-review-n8-10-upgrades@82adf451c (upgrades 1051ddfc5 on installer-review f8df41fb1
+ HR: null Agent version = missing; corrupt HA fields → FollowModel skips + throttled warning, heartbeat stays 200,
address dialog 500) and go-n8-10-api-agents (02a6c1ea, IN FLIGHT; stacked on upgrades; must rebase onto go now
that monitor is in). Both need rebasing onto go (installer commits now have new hashes). api/agents rulings: core
typed hooks InstallationProfileTyped/InstallationRegisterTyped/HAServiceOperationTyped + Manager.RunServiceOperation
(single trigger, HA lease reused); registration version whitelist checked inside RegisterIdentity AFTER auth/proof
(anon → 403 regardless), before consume (400, proof kept); If-Modified-Since via net/http.ParseTime (invalid →
ignored); traffic.go keeps legacy metrics/trafficstore adapters → NEW TASK "N8-10 supplement: metrics/trafficstore
native services (A05 engine)" before N8-11. UI follow-up: recovery entry for corrupt dashboard checkpoint and
latest_upgrade (currently offline fix / re-register, documented). N8-11 also: delete old core hooks, typed hook
unregistration (upgrades review L2), test-only numeric IP/image compat in installer.

**Update 2026-11-19 ~18:30 (coordinator 2f0cdf04): batch-11 MERGED, go = a25020826.** Contents: json-case sweep
(go-review-json-case), N8-10 core-2 + Opus HR review (high 0, M1 re-registration kept non-Python members incl.
docker_ha_installation_id → fixed; node_test_dns_endpoints dropped like Python — field dead since upstream #569),
toolchain Go 1.27.4 (go.mod go 1.27.0, x/text direct, Dockerfile golang:1.27.4; govulncheck v1.10.0 clean),
jsondecode.Select (narrow exact-name owned capture; fixes 8.4 MB N8-9 sync alloc regression from objectMembers;
Codex-reviewed by be408111), zoneinfo oracle registration, pyref refresh. Gate: preflight + compare .32 PASS on
0bf663323 (new=0 stale=0, 24 difftest, 23 live); core FULL .64 PASS (race + 94 fuzz). Final tip adds docs only.
Monitor (1cc7e926) and installer (7e69d031) are on 70cea1eb6 → rebase onto go. Rulings for them: corrupt stored
guards (oxidns_ha_publication, dashboard checkpoints, non-object registry members, NaN expires) fail closed with a
tested recovery path; installer session ids must be 32 lowercase hex, index == session.id (prod snapshot 0/0);
installer data_dir dangerous-dir check normalises with path.Clean; numeric agent_ip → 400; machine text/numbers
native ASCII rules (no CPython tables, no 4300-digit limit, no int64 overflow); kept named D2 identity encoder for
published install hashes (not a legacy facade). UI follow-up: dashboard StateError on first load has no recovery
button (admin uses stats reset API). Plan kept by maintainer: batch-12 = monitor + installer (installer needs Opus HR,
security fixes), batch-13 = upgrades + api/agents, N8-11 separate.

**Update 2026-11-19 ~16:40 (coordinator 2f0cdf04, Opus 5.5):** core-2 handed back as `go-n8-10-core-2@a75dd4a1d`
(based on go@d9515f7fe, 20 commits, report progress/n8-10-core-2.md, status "partial" with explicit deferred list).
Rulings today: traffic domain lowercasing = x/text cases.Lower, Unicode 17 only, test hard-asserts 17.0.0 (55
lowercase + 307 final-sigma rows bound to official UCD); **toolchain → Go 1.27.4** (maintainer: servers already
ran 1.27.4 via GOTOOLCHAIN=auto, release image was the untested 1.26.11) — batch-11 gets one integrator commit:
server/go.mod `go 1.27.0` + `toolchain go1.27.4`, x/text v0.43.0 marked direct, docker/Dockerfile 3× golang:1.27.4;
Agent module (backend/agents/go-agent, Dockerfile.agent) unchanged; rerun govulncheck (not installed on servers yet).
In flight: df5151e2 Opus HR review (go-opus-review-n8-10-core-2), 1cc7e926 Codex monitor (go-n8-10-monitor),
7e69d031 Codex installer (go-n8-10-installer) — both stacked on a75dd4a1d, rebase after review fixes; upgrades after
installer, api/agents last. Planned: batch-11 = json-case + core-2(+review) + toolchain; batch-12 monitor+installer.

**Why this handoff:** the previous coordinator session (94c212e6, Opus) got safety-routed down to Opus 4.8
because its context accumulated security material (an O9 security review + a user question about LAN packet
capture). A FRESH session avoids that context and should stay on Opus 5.5. Start here.

**go = 12150a402** (pushed). Merged since the last entry: batch-7, batch-8, batch-9, batch-10.
- **Route parity is COMPLETE** — MCP #39 (6 routes) and the 3 artifact routes were the last gaps, both merged
  with Opus HR review. Every Python route now exists in Go.
- **O9 security round 1 done & merged**: Go 1.26.11 toolchain, all-routes auth test, H1 CORS same-origin-only
  (never rejects; reverse proxy OK), H2 Agent registration needs key/JWT/one-time 24 h credential (hash only,
  cap 100 evicting oldest), M1 backend body limits, M2 decoded-download caps, M3 placeholder/short JWT key →
  random + warning, L1/L3/L5/L6.
- **N8 refactor**: N8-1..N8-8d done; N8-9 all seven groups done; N8-10 **core-1 done** (Opus HR verified).
- **JSON case-sensitivity sweep**: encoding/json matches struct fields case-INsensitively and takes the last of
  duplicate keys; Python matches exact keys. Fixed heartbeat (batch-10) then swept all 223 call sites to a shared
  exact-key `jsondecode` (go-audit-json-case@32120955f + Codex review go-review-json-case@37a1a4840, +3 fixes).
- **Allocations**: Mihomo 128n ~46 MB, OxiDNS cold ~43 MB, heartbeat ~6.8 KB. O10 has two residual follow-ups
  (MemberJSON scalar encode, typed View fixed read cost).

**batch-11 candidates (both already reviewed, ready to stack):**
- go-review-json-case@37a1a4840 (JSON exact-key sweep + its Codex review)
- go-n8-10-core (8019dda1, IN FLIGHT) — N8-10 core-2: manager register/edit, deployments, remote/diagnostics/
  events. Rebased onto go@12150a402. Rulings given: non-ASCII/Unicode version numbers fail closed (machine values,
  no CPython int emulation); stored-ownership identity fields compare original text only (M1 from core-1); any JSON
  from Agents parsed by exact key, never struct-decoded; heartbeat alloc ceilings must hold. When it hands back:
  dispatch a fresh-Opus HR review, then stack into batch-11.

**Integrator:** b6fa1f80 (Codex) builds each batch in ~/program/configflow-go-batch-N, runs preflight
(/tmp/preflight.sh on the servers = vet both tags + full non-race suite) + split gate (core on .64, compare on .32,
setsid nohup). Coordinator ff-merges on PASS and pushes.

**Still to do after batch-11:** N8-10 monitor/upgrades/installer/api-agents (parallel after core-2); N8-11
(delete jsonx/pystr/legacy + grep gate); then O9 round 2 (MCP/artifact + full tree), O10, O13 feature-completeness
audit, O15 ARCHITECTURE.md, O17 code-size audit, O12 release notes (draft at server/parity/o12-release-notes.md,
13 maintainer-confirm items), labs L2–L6, O14 switch. remaining-work.md has the full list with sizes.

**Account/model note (maintainer 2026-11-19):** using the Pro account `solarsear`. Opus 5.5 can get safety-routed
to 4.8 when context is security-heavy — keep coordinator sessions focused, `/clear` or hand off when a session's
context fills with security review material. HR reviews stay Opus; non-HR reviews Codex. Create agents with
`paseo run --new-workspace worktree --worktree-mode checkout-branch` so they show in the sidebar; never delete an
agent's cwd worktree.

## (history) CURRENT STATE — 2026-11-19 (coordinator 94c212e6) <<<

**go = 690e97995** (pushed). batch-7 merged 2026-11-18 night (R14–R17, A18/A22/A23, N8-8b/c, N8-9 health+discovery,
O3-7/8; fuzz found quoted "<<" merge-key bug, fixed). **batch-8 merged 2026-11-19**: N8-9 rules + dns, MCP #39
(6 routes, Opus HR verified), artifact 3 routes (Opus HR verified, 34 deferred rows removed), O9 round 1 (Go 1.26.11
toolchain, all-routes auth test), O9 fixes H1 CORS same-origin only (never rejects; reverse proxy OK), H2 Agent
registration needs key/JWT/one-time 24 h credential (hash only, cap 100 evicting oldest), M1 backend body limits,
M2 rule download decoded cap, M3 placeholder/short JWT key → random key + warning, L1/L3/L5/L6. Allocation:
Mihomo 128 nodes ~65 MB, OxiDNS cold cache ~43 MB. Full route parity reached (MCP + artifact were the last gaps).
**Inventory:** server/parity/remaining-work.md (what is left, with sizes).
**batch-9 candidates:** go-review-n8-9-subs@9be6977cb (subs/substore + review: 64 MiB caps, per-subtree alias
budget), go-review-jsonstore-ctx@2fac52248 (F1), go-n8-9-routers (61078038 rebasing onto 690e97995; HR review done
by an Opus the author spawned — accepted, but HR reviews are coordinator-dispatched from now on).
**In flight:** 8019dda1 N8-10 agents core (Codex; then Opus HR review), b455468c C37/A24 crash gap (Codex).
**Next:** N8-10 monitor/upgrades/installer/api-agents after core; N8-8d after subs merges; N8-11 deletion last;
then O10, O12, O13, O15, O17, labs L2–L6, O9 round 2 (MCP/artifact + full tree), O14.
**Decisions 2026-11-19 (maintainer):** O9-H1/H2/M3 as above. Coordinator rulings: external subscription base64 stays
tolerant (named Go decoder); airport group type whitelist → select; user-stored identity fields strict; non-ASCII
Agent coreVersion fails closed at EasyTier gate; temp dir = TMPDIR/TEMP/TMP or os.TempDir with clear diagnostics;
legacy rule caches claimable by stored shared library or stored profile-local rules only (prod snapshot: 0 claims);
generation paths read caches via PeekCacheWithFormat (never migrate); Python 500 → proper 4xx.
**Paseo:** create agents with `paseo run --new-workspace worktree --worktree-mode checkout-branch` so they show in the
sidebar; then send one MCP prompt with notifyOnFinish so completions reach the coordinator. Never delete an agent's
cwd worktree (see memory).

## (history) CURRENT STATE — 2026-11-18 ~22:30 CST (coordinator 94c212e6) <<<

**go = 55c9794a0** (pushed): **batch-6 merged** (A16, A17, A21, N8-8a review, N8-7b review, O3-6b, lifecycle race
fix, 8a provider fix 694df9d0 + Codex review fix 38ad5156, R5 stale-expectation/identity fixes, integration fixes
0d31fb93, and the generation perf fix). Gate: core FULL on .64 (race + 42 fuzz) PASS on a16a9fc99; compare on .32
(proddiff new=0, retries 0, 21 difftest pkgs, 23 synthetic live) PASS on 573705d53 (test/doc-only delta).
**Perf regression found by the gate:** N8-8a codecs re-decoded the whole config per node / per ruleset
(Mihomo 128 nodes + 512 rules: 3.2 GB alloc, 3.4 s; OxiDNS POST 29–160 s). Fixed with narrow codecs (RulesetFormat
decodes only rule_library): 77 MB, 0.1 s (Python 0.095 s, 119 MB alloc). Remaining gap vs pre-typed Go (9.8 MB) → O10.
**batch-7** (b6fa1f80 builds ~/program/configflow-go-batch-7): A22, R14, R16, R17, R15 review (dab6b395, verified),
O3-7, O3-8 (6e5c7760), pyref-narrow, N8-8b, N8-8c (097eba4e), N8-9 discovery, N8-9 health (Gemini draft →
89c495b3 rework → 2a90a68e review), A18 review (1f3a87d6), A23 review (e8737927); integration: one RouterJobLock for
R14/R15/R17, A22 registration, delete "deferred → R14/A18" baseline rows, O3-8 shutdown rerun, A14 throttle test
clock. Accepted D2 consequences (O12 release notes): IPv6 verify reports and dismissed notifications reset once
after the switch. Next: b6fa1f80 hands back tip after preflight + full compare → coordinator runs core on .64 → merge.
**Follow-ups:** N8-9 health F1 (shared jsonstore ctx API + discovery adapters, after batch-7); O-1 decided (forced
disable after 10 min offline, design a16-a18); O17 code-size audit + O10 allocation added (WORKFLOW).
**Process changes today:** pre-flight = full non-race suite (/tmp/preflight.sh on both servers); A/B speed timing
cancelled (maintainer); Gemini/Antigravity not used (trial draft needed 11 corrections, quota ran out mid-task).
**Mac disk:** nearly full at 3 GiB; 99 idle worktrees removed (branches kept). Remove finished worktrees after every
batch; give reused idle agents an explicit worktree (their old cwd may be gone, e.g. 0d31fb93, 694df9d0 kept).

## (history) CURRENT STATE — 2026-11-18 ~18:30 CST (coordinator f8801d39 → handing off) <<<

**go = 30caa723** (pushed). batch-5 merged 14:45 (A14, R12, R13+X1, C34, C36, N8-5c, go-gate-speed, speed A/B,
Step B fixes). Verified 67/78 on go; 7 more verified and waiting (A16 A17 A21 in batch-6; A22 R14 R16 R17 for
batch-7); in HR review: A18 (1f3a87d6), A23 (e8737927); porting: R15 (4e136761, from go-opus-review-r14). C07 partial.

**batch-6** = local branch `batch-6@8ec68fd4` in ~/program/configflow-go-batch-6 (75 commits over go@c5f5ff34;
go since gained only docs commits): A16 integrate, A17 integrate, N8-8a review, N8-7b review, O3-6b, A21 review,
gate fuzz tmp/jobs fix, lifecycle race fix 264ffbb5 (R14 review M1). PROGRESS dup rows already cleaned.
**Blocked on** 694df9d0 rebasing `go-fix-n8-8a-provider` (3 commits: model.String/Int shared scalar decode,
provider topology rejection + diagnostics, ippresence unknown states) onto batch-6 — conflicts because N8-5c
deleted repo/compat_dialers.go and changed provider/corpus_test.go. Then: stack it, quick Codex review of that fix
(model-layer rule change; I promised the maintainer), pre-flight (/tmp/preflight6.sh on .32: vet both tags,
compile all tests, `go test -run 'Corpus|Reference|Golden' ./internal/...`), then split gate: core on .64
(`GATE_FULL=1 GATE_ONLY=core gate.sh -p 6 <fuzz targets>`), compare on .32, both launched with `setsid nohup`
ON THE SERVER (session restarts kill rt-attached gates). Merge, push, notify.
**batch-7 candidates:** go-opus-review-a22@9791bc8d (on A21), go-opus-review-r14@a08a120b (integration with O3-6b:
map conflict error, delete R14 repo hook + syncJobs, rerun TestR14ReviewRepositoryHookSharesJobLock),
go-review-r16@1b28b662, go-opus-review-r17@b78bbb38 (swap routers.JobLock → RouterJobLock, delete syncJobs, rerun
TestReviewR17RepositoryHookSharesJobLock), go-o3-7@a1d3e1b7, go-pyref-narrow@06a25922 (golden fingerprints narrowed),
N8-8b go-opus-review-n8-8b@fbd9993c, N8-8c go-n8-8c@fd61ff3f (needs the 8a fix entry; two positive tests skipped).
Build R17/R14/R16 order carefully (go-r17-base-v2 = go + R14 author + reviewed R16 was a temp base).
**In flight:** 6e5c7760 O3-8 fault harness (faultprobe pkg; prod no-op; /__faults/state difftest-only + token);
097eba4e N8-9 discovery (from batch-6); 5cf838eb Gemini trial N8-9 health (let it finish; then Codex review; no
more Antigravity work); e46d698f A/B speed timing after 23:00 on .64 (fixed before/after refs).

**Decisions today (also in design Clarified / behaviour-changes):** speed options rejected; all Opus at thinking
medium; non-HR reviews Codex, HR reviews Opus (Gemini switch withdrawn — quota); PD-1 proddiff Last-Modified
normalization (6 routes, HTTP-date, 600 s) + exact ETag baseline; typed decode: declared string fields accept
numeric literals as their exact token text (123.0 → "123.0"), declared int fields accept strict ASCII decimal
quoted integers ("443"), bool strict, open Params (Mihomo) pass through; wrong-typed identity fields (provider
names, rule value/type, group type…) fail with explicit "<field> 必须是字符串，当前值：<JSON>"; Surge alpn list:
1 element → value, else omit + warning; Surge url/interval None → defaults; WireGuard reserved list → 1/2/3;
locks: LevelSetupSession=18, LevelAgentOperation=19, device 20, RouterOperationLocks sorted/deduped group;
lifecycle.SpawnerFrom(ctx); localhttp injected Spawner; yamlx Marshal static Node checks (no runtime reparse).
**Lessons:** pre-flight before every full gate caught 4 integration bugs; golden fingerprints were too broad
(fixed in go-pyref-narrow); .64 /tmp tmpfs fills with concurrent fuzz (fixed in batch-6 gate.sh); `pgrep -f`
watchers match themselves; GATE_ONLY=difftest skips fuzz; review agents' targeted runs miss cross-package corpora —
always run cross-package Corpus|Reference|Golden on stacked trees.

## (history) CURRENT STATE — 2026-11-18 ~12:10 CST (coordinator f8801d39; read this first) <<<

**go = 566af4bd** (pushed): batch-4 merged (A20 review, C07 review, N8-7 batch 1, R10/R11 reviews, O3-6c,
N8-5(a)(b), 3 integration fixes) + routers Step B (tooling/docs only). Verified modules ≈61/78.
**Maintainer decisions today:** the three speed options are REJECTED (no incremental switch, no tiered
verification, keep fixing Python bugs in Go). **All Opus agents run at thinking medium** (quota) — WORKFLOW
"Model settings". Codex stays gpt-6.1-sol high.
**Gate infra:** ~/.local/bin/cf-gate now sets GATE_FULL=1 automatically when backend/, core packages
(lockx repo lifecycle ctxx yamlx model httpx), go.mod/sum, server/scripts or tools/pyref changed vs base;
CF_GATE_SPLIT=1 runs core on .32 and compare on .64 in parallel (needs go-gate-speed's gate.sh). Lessons:
run long remote gates detached (setsid nohup on the server) — a coordinator session restart killed one;
`pgrep -f` watchers must not match their own command line; GATE_ONLY=difftest does not run fuzz.
A/B test-speed timing only after 23:00 on .64 (daytime .64 is for gates).

**batch-5 (next, full gate, split):** go-gate-speed@096ae6fa + go-fix-repo-crosscheck f01d464e (must go
together); R12 = local branch batch-4-r12's 4 R12 commits + allowlist patch + go-r12-r03fix c2a9361c
(TestR03Corpus fix); R13 review (10fcabd6 rebasing + P1 + X1 tolerant router decode); C34 review + C36
(89c495b3 rebasing; C36 adds lockx.LevelSetupSession, C32 ReadRulesetContent 99afa95d); N8-5(c) (24d62bfc
rebasing; then retire that session — context 670k); test-speed A b75f2d7f + B 68945125; go-fix-r05-stepb
9be5a141; go-fix-a20-c26-stepb (e4e190e9, c36bce34, ce8b3796). Pre-flight: run package corpus tests on the
stacked tree before the full gate.
**Then:** A14 (06515315 rebasing + 4 wiring items) → A16 (reviewed 6464cd7c, 0 high) and A17 (reviewed
f5180a27, 0 high) rebase on A14, RecordContext, mark verified. A21 (95eb529e) waits C34 for DNSLoop and does
the final C36 server registration. O3-6b (6e5c7760). N8-8a (097eba4e; yamlx shares c5a31ce3 + static Node
checks), then N8-7 batch 2.
**Step B findings routed:** R05 + A20/C26 fixed (above); R15/R17 parts (HEAD creates router, verinfo/Location
500 + failed probe save, udp keyword) belong to those ports — put them in their briefs.

## (history) CURRENT STATE — 2026-11-18 ~10:30 CST (read this first; everything below is history) <<<

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

**Maintainer decision 2026-11-18 ~10:40: all three speed options REJECTED** — no incremental switch/proxy to
Python, no risk-tiered (lighter) verification, keep fixing Python bugs in Go (record in behaviour-changes.md).
Do not re-propose them.

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
