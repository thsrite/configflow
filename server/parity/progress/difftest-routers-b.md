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
Reads completed: AGENT-BRIEF; WORKFLOW Independent acceptance; matrix lines
1–1020 (all S/F/B/D/G/T/X/O6 additions, H through H0607); O6 review at document
end also read from initial output. Still explicitly read 1021–2049 before code.
Config Step B branch reference: file inventory and tools/configdiff/README.md
read; findings format partly viewed (large output truncated). Need finish format
reference and inspect generic build/compare/seed/harness only. Do not copy/edit
that branch's files or infer expectations from Go implementation.

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

No test/build/lint/dev server run yet. No findings/coverage claimed yet.
