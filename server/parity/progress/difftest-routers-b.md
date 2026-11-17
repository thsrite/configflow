# Routers Step B — independent public HTTP differential

Status: in progress. Branch go-difftest-routers-b, base go 62427c41.
Previous task go-n8-3 is frozen; do not modify that branch. No subagents/push.
Only .32 via rt; .63 unavailable. Synthetic fixtures only, isolated loopback
RouterOS/iKuai/Agent/rule URL/proxy/WebDAV fakes; separate namespaces/fake state
and DATA_DIR per public Python/Go server. Do not read Go implementation in
internal/routers, routersplit, devhttp (or other internal business packages).
Allowed references: difftest harness, public server entry, Python source, routes,
frontend APIs, domain matrix. Findings go to porters; never fix implementation.

Required matrix: parity/difftest-routers.md (2049 lines; O6 approved with
additions). 26 routes, 2301 named design entries, not request/pass counts.
Reads completed: AGENT-BRIEF; WORKFLOW Independent acceptance; full matrix
lines 1–2049, including all 1356 H rows and complete O6 review/additions. Config
Step B reference at 29f9c2cf: README, build.py, harness environment/start/request
outline, compare policies, seed setup, concrete profile-write recipe and findings
format. Borrow patterns only, no edits/copies of that branch. Public Go command
main.go and manifest files read (allowed entry); no business implementations.
Python app.py/client_devices.py/upstream_router_pending.py read completely;
routers handler/vendor helpers still to inspect while writing concrete recipes.

Acceptance rules: strict status/headers/types/keys/values/array order, Chinese
messages, logical SQLite meta/sections/fields+revisions, filesystem and stateful
fake transcripts/tables. Dynamic IDs/times/tokens/hashes must first be validated,
then paired bijectively; never mask plan/report/errors or fingerprints. Later
requests use each endpoint's own previous revision/fingerprint/token. Python is
started with python -m backend.app (waitress, non-DEBUG), real startup/loops.
Missing Go routes detected from actual public registration inventory, never a
business 404; run Python reference and count Go 尚未实现 separately, not failure.
Device JSON bodies compare parsed values. Match every X04 lock ordering, X07
startup timing/receipt, X08 write side effect, X12 accepted/crash classification,
X13 per-side state, X14 memory/restart boundary. Fake prerequisites must exist
before device cases: per-side state, actual Python startup, transcript-gated slow
request to hold locks. Firmware/OS/real packets remain L5/explicit audit scope.

Next:
1. Finish full matrix and config reference read; enumerate actual Python/public
   route API inputs and case audit, including unreachable helper guards.
2. Build test-only tools/routerdiff harness and executable case inventory. Keep
   coverage ledger tied to concrete Python-reachable fixtures, no fake coverage.
3. Implement stateful vendor/Agent fakes from Python/protocol contracts, mutation
   self-checks, actual public servers and independent snapshots/comparison.
4. All route and O6 sequences plus eligible H/D/G/T variants; strict comparison for
   implemented Go, distinct unimplemented/audit/L5 outcomes. Findings document:
   case, request, Python/Go results, severity and responsible module.
5. .32 validation, explicit-path buildable commits go(difftest): routers Step B…;
   final git rebase go, clean tree, counts/commands/output/commits hand-back.

Buildable inventory step: tools/routerdiff/inventory.py parses all 2301 design
entries and 26 unique routes, including shared O6 F90 and non-HTTP fixture/fake
contracts. Entry != executable recipe != request count. Unit tests assert all
category counts, route-table identity, table parsing and O6 sentinels. .32:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server/tools/routerdiff && PYTHONDONTWRITEBYTECODE=1 ~/cf-test/.venv/bin/python -m unittest -v test_inventory'`
→ Ran 3 tests in 0.099s, OK. Initial same lightweight test was accidentally
scheduled to .64 by the updated rt least-load default (also passed); ALL future
commands explicitly pin RT_HOST to .32, especially network/namespace cases.
No HTTP coverage/findings yet; no public server or fake started yet.

Next concrete build step: production Go executable + manifest-only overlay on
public command (no lifecycle build tags), native .32 per-side net/mount namespaces
at identical loopback/synthetic addresses, stdio RPC worker and independent
DATA_DIR binds; real Python -m backend.app. Stateful fakes and transcript-gated
lock holder must be ready before device cases. Need strict comparator mutation
self-checks and physically meaningful Python precondition checks before ledger
entries count as compared/unimplemented. Do not treat inventory as coverage.

Foundation build/seed step completed on .32: production Go public command builds
with only its no-op manifest hook observed (no proddiff tag/lifecycle patch).
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_inventory test_seed; routerdiff_build=$(mktemp -d /tmp/routerdiff-build.XXXXXX); ~/cf-test/.venv/bin/python build.py "$routerdiff_build"'`
→ Ran 4 tests in 0.292s / OK; /tmp/routerdiff-build.AybfCK/configflow.
Read Python routers.py through line 825 (including all policy/workspace/manual
route/rule-sync handlers), routeros.py 1–260 and ikuai.py 1–230. Config Step B
native namespace wrapper read as infrastructure reference only. No HTTP coverage
yet; only the synthetic repository import and build boundary are validated.
