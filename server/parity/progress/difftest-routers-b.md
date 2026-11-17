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

Native foundation and first concrete HTTP group completed:
- namespace.sh gives each side independent net/mount/PID namespaces, same owned
  DATA_DIR path, lo-only synthetic LAN/ULA, private resolver, normal-user server.
- Python starts via -m backend.app with INFO/waitress and normal startup loops;
  Go public command uses normal lifecycle and manifest-only observer. The observer
  TSV delimiter bug was caught by the boundary test and corrected in test code.
- Stateful ROS/iKuai baseline fakes + six owned HTTP peer listeners per side.
  Contract tests prove own Basic/login hashes/cookie, state read/write, duplicate
  tags, 1008 rejection without writes, applied write with empty acknowledgement,
  independent tables. Transcript gate proves Python probe holds the job lock and
  an invalid concurrent update gets 409 before body validation. Full /execute,
  all Agent endpoints and detailed vendor edge contracts remain to implement.
- Strict typed response/database/files/device comparison and mutation tests:
  status, Chinese text, fingerprint, ordered operations, profile headers, foreign
  objects, revisions and receipts cannot be silently ignored. Only HTTP transport
  branding/framing and explicitly accepted device JSON whitespace are excluded.
- Actual public manifest registers NONE of the 26 domain routes: R15/R17 are
  still missing (consistent with PROGRESS-routers). R01–R06 are lower-level code,
  not registered public routes. Go device HTTP tests activate when registration
  appears; Python prerequisites still execute now.

.32 commands:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build.AybfCK/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_inventory test_seed test_compare test_boundary'`
→ Ran 9 tests in 9.674s / OK.
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python run.py --binary /tmp/routerdiff-build.AybfCK/configflow --output /tmp/routerdiff-auth-shapes.json'`
→ authentication 182; profile_selection_failures 485 cumulative; body_guards 549
cumulative; matrix_entries=2301, matrix_routes=26, implemented_routes=0,
go-unimplemented=549, referenced_entries=268. This is partial coverage: each
referenced entry remains variants-in-progress; 2033 entries still pending. No
product differences can yet be claimed (zero implemented HTTP domain routes).

Field guard group now executes 1162 concrete HTTP recipes (321 referenced design
entries; all references still require final variant audit). Added exact Chinese
message preconditions so an earlier 400 cannot masquerade as the intended guard.
Router create/update cover each required field, bool/int distinctions, falsy vs
non-list rule sets, DNS guard/PD switches and nested lists; client draft covers
IP/name/mode/previous values; fixed-IP covers each outer/row condition, duplicate
IP across vendors, duplicate MAC per router and 1,000,000/1,000,001 token boundary;
manual route intent covers both plan/apply and every scalar/shape guard;
workspace and rule-sync outer/key/type guards have independent variants.
Python oracle corrected one fixture assumption: all-zero/broadcast-shaped MACs
are syntactically accepted by normalize_mac, so they cannot be used to assert
the invalid-MAC 400 guard (the request reached source-evidence 409 instead).
No implementation changed; valid unusual MAC cases remain separate follow-ups.

.32 command:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python run.py --binary /tmp/routerdiff-build.AybfCK/configflow --output /tmp/routerdiff-field-guards.json'`
→ route_edit_shape 1106 cumulative; workspace_shape 1142; rule_sync_shape 1162;
{"matrix_entries":2301,"matrix_routes":26,"implemented_routes":0,
"recipe_counts":{"go-unimplemented":1162},"referenced_entries":321}.
Detailed ledger stays in the synthetic .32 artifact; 1980 matrix entries still
have no attached executed recipe. No final coverage or completion claimed.

Successful draft/manual sequence step (.32):
- Same-side Ref objects resolve revision/fingerprint from each server's own prior
  HTTP response; missing references fail closed. Integrity tests reject duplicate
  recipe IDs or references to nonexistent design entries.
- Client name/smart/direct/clear sequences assert actual returned device rows;
  workspace modern/legacy/no-op save and real stale revision sequences; router
  rename/empty-password/no-op saves. Every profile write asserts exactly one
  _revision increment and identical foreign profiles/shared/system rows (including
  storage field revision/position); draft writes cannot mutate device tables.
- Manual add/update/no-op/delete for ROS AND iKuai uses actual preview hash and
  returned entry revision on that same side. Device-only operations must leave
  all configuration DB rows unchanged, preserve a seeded foreign route, and
  pass exact editable/normalized values/readback checks. iKuai fake presents the
  real netmask display suffix/runtime status and retains full underlying forms.
- Closed RouterOS console grammar now executes route/NAT/address-member adds,
  digest/page/name reads, OR queries and comma-ID removes against real fake
  tables. Unsupported syntax fails the fixture; it cannot report invented
  success. Added tests prove script mutation, escaped Unicode values, IDs and
  parse-before-execute rejection. No Python production helper called by fake.
- Read review-r06 Accepted differences: manual route revision/fingerprint hashes
  are intentionally Go-native opaque tokens. Future comparison must use shape,
  stability/content-binding/stale rejection and scoped pairing, not assume the
  Python digest algorithm. Do not silently mask these fields. Full dynamic
  pairing and all accepted-difference classifications remain to implement.

`.32: RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build.AybfCK/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_inventory test_seed test_compare test_recipes test_console test_boundary; ~/cf-test/.venv/bin/python run.py --binary "$ROUTERDIFF_BINARY" --output /tmp/routerdiff-draft-runtime.json'`
→ Ran 13 tests in 8.220s / OK; baseline sequence 1203 recipes.
Latest manual sequence command:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes; ~/cf-test/.venv/bin/python run.py --binary /tmp/routerdiff-build.AybfCK/configflow --output /tmp/routerdiff-manual-routes.json'`
→ Ran 2 tests in 0.080s / OK; manual_routes 1221 cumulative recipes;
implemented_routes=0, go-unimplemented=1221, referenced_entries=373 of 2301.
1928 matrix entries remain unattached; none is silently called covered. Task
continues: full protocol/failure/rollback, generated forms, runtime/concurrency,
all H reachability/variant audits, findings renderer, final full validation.

Coordinator update: .64 is now an equal native Hyper-V Arch host, .63 retired;
.32/.64 both allowed for final gates. Keep this run pinned .32 for continuity.
N8-3 is merged ported into go; its old branch remains frozen. Final rebase pending.

Stored/error and cross-profile job-lock step:
- 1288 actual recipes now pass Python preconditions; 397 design entries have
  attached variants (all 2301 still require final per-entry completion audit).
- Persistent synthetic receipt seeds at exact required SHA256 filenames cover
  JSON array/scalar fallback, invalid JSON paused result, version truncation,
  missing version HTML500, stale running rejection before the real first round,
  paused message. Source-proven HTML500 cases exclude only HTML prose (X03),
  retaining status/kind/Content-Type and complete storage/device state.
- Five persisted manifest types each protect DELETE and both connection/type
  edits; legacy overlap/default/public-private-field cases, missing-router and
  wrong-vendor branches, malformed IPv6 URL HTML500, nonempty array/string
  release body HTML500. No broad exception/HTML normalizer.
- ROS and iKuai probes hold L1 at a current, named fake request gate; all seven
  mutating endpoints reject invalid bodies with 409 before handler/type checks.
  Same-connection alpha/beta contention is exercised; release finishes the
  actual probe and checks revision+1, including the failed iKuai probe.
- Fixed a harness race found by these cases: matching an old transcript could
  falsely claim the current probe had reached its gate. Gates now record an
  individual entered event and wait for THAT fault activation; no sleeps or
  historical request counts. A future partial Go HTTP registration skips the
  complete dependency chain as Go 尚未实现 rather than joining an unstarted job.
- Findings renderer now produces parity/difftest-routers-findings.md with exact
  requests/Python/Go/differences and severity/responsible modules. At zero
  implemented domain routes it explicitly gives no product-defect conclusion.
- Runner defaults to exit 2 while matrix audit is incomplete. Iteration must
  explicitly pass --allow-incomplete; the ledger always records complete:false.
  Atomic per-group reports and errors prevent an old run being mistaken for new.

.32 latest command:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --output /tmp/routerdiff-stored-locks.json --findings /tmp/routerdiff-findings-current.md'`
→ Ran 2 tests in 0.172s / OK; base/job_locks 1238 cumulative;
stored/receipts 1245; stored/stored_guards 1288; implemented_routes=0,
go-unimplemented=1288, referenced_entries=397, complete=false,
remaining_audits=2301. Findings retrieved from this synthetic owned artifact.
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server; gofmt -l $(git ls-files "*.go" 2>/dev/null || find . -name "*.go"); go vet ./...'`
→ exit 0, no output. This is an intermediate formatting/vet check, not the final
rebase/full acceptance. Work continues; 1904 entries have no executed recipe yet.

Creation/inspection/Agent scope step:
- 40 real creates followed by selected-record deletion: coercion, literal-false
  TLS, falsy rule sets, stable deduplication, unvalidated port/query-fragment,
  unknown input ignored; returned ID format/uniqueness, exact stored fields,
  retained password and absence from public view. Creates append exactly one
  record; deletes remove exactly the selected record; all foreign rows/device
  state remain unchanged and each write bumps revision once.
- Successful unlinked report/plan/policy and target-empty preview. Early blocked
  plan explicitly has empty fingerprint/script/stages (not a valid 16-char hash).
- Each cohesive write sequence now gets a NEW pair/database: a missing Go R17
  write cannot contaminate a later implemented R15 scenario's revisions/state.
  --world selects isolated iteration groups; default still runs every world.
- Actual alpha/beta OxiDNS query pinning, request-profile response headers,
  deleted-header-before-agent lookup, HEAD ignoring invalid agent on R002,
  empty-profile discovery, all-profile Mihomo gateways vs profile-local sources.
- Real public heartbeats establish online status; authenticated independent
  Agent fakes GET forwarding state, POST persist, re-read after persist and
  idempotent persist. Shared-router refs deduplicate in source order; an Agent
  bound to beta is authorized by an explicit router link in alpha. No OS/L5
  forwarding/reboot claim. Fake validates the correct host-specific bearer.
- Auth A7 now uses a token that actually belongs to a seeded Agent; a real
  heartbeat first proves it is valid before all 26 admin endpoints reject it.
- Added exact Agent missing-profile audit: actual public import rejects it;
  stopping the real Python process, deleting only that field in owned synthetic
  SQLite and restarting also fails before handlers. No Python helper patched.
  R002-B02 stays pending (proposal in difftest-routers-audit.md); no bogus HTTP
  success counted. Failed fixture roots are now cleaned even before side startup.
- Fake protocol refinements: catch unexpected fake errors as fixture errors;
  nested JSON matchers target iKuai actions; IDs advance beyond seeded foreign
  IDs; correct DHCP static_ vs lease view; group IDs/ref_count with hidden refs;
  mutable Agent forwarding/return state and basic owned WebDAV writes. Deeper
  vendor branches still require their own cases, not claimed covered here.

Full .32 phase command:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build.AybfCK/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_inventory test_seed test_compare test_recipes test_console test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --output /tmp/routerdiff-phase-agent.json --findings /tmp/routerdiff-phase-agent-findings.md; cd ../..; gofmt -l $(rg --files -g "*.go"); go vet ./...'`
→ Ran 16 tests in 14.650s / OK; forwarding 1348; receipts 1355; stored guards
1398; implemented_routes=0, go-unimplemented=1398, referenced_entries=426,
complete=false, remaining_audits=2301. gofmt/vet exit 0, no output.
After the ID/ref-count helper refinement, targeted fresh .32 validation:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build.AybfCK/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --world manual-routes --world router-creations --world forwarding --binary "$ROUTERDIFF_BINARY" --output /tmp/routerdiff-peer-counter.json'`
→ Ran 6 tests in 11.629s / OK; manual 18, creations 98, forwarding 105 cumulative;
0 implemented, 105 Go 尚未实现. This filtered run is not the full coverage count.
Still 1875 design entries without an attached executed recipe; ALL entries need
final variant/reachability audit. Work continues; no final hand-back/rebase yet.

Intermediate integration refresh: committed work rebased cleanly onto go
fa237d77 (8 test-only commits; new HEAD fb00da2c before this probe step).
Rebuilt the real public Go command and re-ran 16 infrastructure/boundary checks
plus all 1398 then-current recipes. .32 command:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python build.py /tmp/routerdiff-build.AybfCK; ROUTERDIFF_BINARY=/tmp/routerdiff-build.AybfCK/configflow ~/cf-test/.venv/bin/python -m unittest -v test_inventory test_seed test_compare test_recipes test_console test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --output /tmp/routerdiff-rebased-agent.json'`
→ Ran 16 tests in 14.141s / OK; 1398 Go 尚未实现 recipes, 426 referenced entries,
0 implemented domain routes. Still incomplete; final rebase remains required.

Probe error/capability step: 94 additional real HTTP recipes. RouterOS covers
HTTP auth/refusal/details/permissions, HTML/non-JSON, JSON null/scalars/arrays,
resource/version bounds, Chinese model/version, each optional capability error,
connection close, actual 10-second read timeout, device password echoes vs
protected global-token redaction. iKuai covers status/redirect origin mapping
(without following), empty/non-JSON/wrong shapes, Result10000 plus refused code,
legacy response rejection, individual call errors, one session re-login and
persistent expiry (exact login request counts), versions and each capability.
Every success/failure probe asserts profile revision +1, stored ok/message and
foreign rows/device configuration unchanged. Real waits use current request gates
and an elapsed lower bound, not mocked exceptions or clock jumps.
Fake sessions now require an actual successful login and rotate cookies on each
login; their generation/current cookie are observable state. Fixed JSON-null
fault ambiguity caught by the new source guard: null now emits literal JSON
null, while only explicit disconnect closes the socket. Direct fake regression
and actual R017 null guards both lock this distinction.

Fresh .32 validation after these changes:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build.AybfCK/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_seed test_compare test_recipes test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --world probe-errors --world manual-routes --world forwarding --binary "$ROUTERDIFF_BINARY" --output /tmp/routerdiff-probe-session-verified.json; cd ../..; gofmt -l $(rg --files -g "*.go"); go vet ./...'`
→ Ran 11 tests in 11.498s / OK; manual18, forwarding25, ROS probes69, iKuai
probes119 cumulative; 119 Go 尚未实现, 46 referenced entries; formatting/vet
exit 0, no output. Filtered iteration only. The next full run must include all
new probes before the total is treated as verified composition.

Release and confirmed Python bug step (maintainer decision 2026-11-17):
- Default iKuai fake retains real static_rt netmask display suffix. Actual public
  Python publish returns 502, fails _confirm_added, and _revert_add refuses its
  own newly added row because _matches sees the suffix. One new Fake-IP route
  remains. Recorded Python bug IKUAI_NETMASK_SUFFIX_ROLLBACK, severity medium,
  responsible R05; not a Go finding while R011 is unregistered. Full actual
  response and remaining row are in difftest-routers-findings.md.
- User explicitly required BOTH variants: default suffix bug + independent plain
  netmask success, not a global fake relaxation. Added separate worlds and
  matrix execution table; runtime status remains in both variants. Plain variant
  successfully publishes static/NAT/group state and confirms empty publication;
  ROS basic/static and empty publications also pass actual readback.
- L5-A checklist now explicitly checks real firmware raw static_rt netmask suffix
  format and clean compensation on failure. No real-device/PVE operation done.
- Go same-case observation records actual status and newly created static rows
  remaining. Mirroring Python's failure residue is an explicit safety failure,
  even if both endpoints otherwise match. A different, cleaner outcome is kept
  as reference-bug-observation for review (neither an automatic pass nor failure).
  No-Go-route result stays Go 尚未实现, never a private R05 helper call.
- Hardened dependency handling: missing Go responses never borrow Python values;
  they block only dependent steps as prior Go differences. A skipped missing Go
  write marks that world's subsequent state-dependent cases Go 尚未实现 instead
  of comparing polluted Python-only state to empty Go state. Four deliberate
  runner self-checks prove these paths and the unsafe “both reproduce residue”
  false-pass trap. Cases/worlds remain isolated.
- Fresh login tokens now validate JWT shape, known-key HMAC, numeric dates/lifetime,
  admin identity and same-side /auth/verify. Random-key/auth-off worlds have
  explicit support without substituting another side's credentials.
- Matrix attachments audited against actual S/F/B meanings: corrected vendor
  action IDs, empty vs editable reads, R001 superseded global P1/P2, fixed-IP
  duplicate/profile guards, sync paused/missing-baseline and update unknown-field
  vs no-op IDs. Five manifest/id unknown-input preservation writes added. See
  difftest-routers-audit.md; these remain partial variant audits, not completion.

Full fresh .32 composition:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build.AybfCK/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_inventory test_seed test_compare test_recipes test_console test_boundary test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --output /tmp/routerdiff-release-composition.json --findings /tmp/routerdiff-release-composition-findings.md; cd ../..; gofmt -l $(rg --files -g "*.go"); go vet ./...'`
→ Ran 20 tests in 19.004s / OK; ROS probe1395, iKuai probe1445, ROS release1451,
suffix1453, plain1459, receipts1466, stored guards1514 cumulative;
matrix_entries=2301, matrix_routes=26, implemented_routes=0,
go-unimplemented=1514, referenced_entries=474, python_bugs includes
IKUAI_NETMASK_SUFFIX_ROLLBACK, complete=false, remaining_audits=2301.
Formatting/vet exit 0 with no output. Findings retrieved from this full run.
1827 entries still lack an executed recipe; every entry still needs its final
variant/reachability audit. Task continues; no final coverage/hand-back claim.

Client discovery and fixed-IP iteration (2026-11-17, task continues):
- Added real public HTTP preview/apply/delete for RouterOS dynamic make-static,
  ARP-only new lease, and iKuai static allocation; own-side signed token refs,
  preview-name distinct from saved name, foreign reservations/custom DHCP field
  preservation, unchanged saved notes, binding visibility/deletion and revision
  invariants. 13 transaction/read recipes.
- Added 5 discovery/cache/rename recipes and 9 outage/degradation recipes: dual
  vendor reads, literal refresh=1 vs refresh=true, names not cached, last useful
  snapshot retained after all sources fail, independent partial/failed states,
  optional table failures and reservation-enrichment failure containment.
- Added 49 exact-guard previews: source-router evidence, foreign static deletion,
  disabled/invalid/relayed/ambiguous scopes, malformed masks/addresses, dynamic
  identity conflicts, protected RADIUS leases, duplicate/bad IDs and strict iKuai
  static form shapes. Every expected refusal checks the actual Chinese message;
  these are partial variants, not claims that whole matrix entries are complete.
- Found/fixed a fixture protocol defect, not a product finding: captured iKuai
  dhcp_static del sends a decimal STRING id while show/persisted IDs are integers
  (backend/test_ikuai_fixed_ip.py:67,84 and its checked-in 4.0.314 capture). Fake
  now implements that narrow protocol conversion and rejects non-string forms;
  actual HTTP boundary regression checks both. D26's abbreviated `{id:int}` is
  a matrix documentation inconsistency; see audit, not an excuse to change Python.
- No Go business implementation read or changed. Current observed manifest still
  registers zero of the 26 domain routes, so all these cases are Go 尚未实现.

Exact .32 validation:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build.AybfCK/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world fixed-ip --world discovery --output /tmp/routerdiff-clients.json'`
→ Ran 6 tests in 17.659s / OK; fixed-ip13, discovery18 cumulative; fixture_error
absent, go-unimplemented18, complete=false.
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world fixed-ip-guards --world discovery-failures --output /tmp/routerdiff-client-guards.json'`
→ Ran 2 tests in 0.171s / OK; fixed-ip-guards49, discovery-failures58 cumulative;
go-unimplemented58, referenced_entries16, complete=false, remaining_audits2301.

Fixed-IP uncertainty/batch iteration (2026-11-17, task continues):
- Added 12 real token/staleness recipes: public-preview token tampering,
  cross-profile replay, public router edit, changed DHCP gateway, changed source
  MAC and a discoverable device without a saved name/policy. Exact reached
  conflict messages and unchanged storage/device configuration are asserted.
- Added 8 actual lost-ack recipes: fake receives and commits ROS make-static,
  PATCH, new PUT or iKuai static add, then closes the TCP reply. Python reads back
  its own operation identity and compensates; dynamic undo removes the new
  static reservation rather than fabricating a restored dynamic row. Foreign
  leases survive and no binding is saved. Ordered outbound requests are checked.
- Added a three-device mixed-vendor transaction: third write refused, then
  iKuai second item is undone before ROS first item; exact operation counts,
  reverse order, residual tables and unchanged configuration all checked. Added
  an intentionally wrong forward-undo runner self-check proving order assertions
  can fail; it is a harness test, not product simulation.
- Added 64-device alternating ROS/iKuai preview/add/delete success through HTTP,
  actions preserve input order, all 64 MAC bindings are persisted, notes survive,
  then all bindings and owned reservations are removed. Each phase increments
  revision exactly once. Both foreign reservations remain. No device address is
  accessed outside the private namespace.

Exact .32 command:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world fixed-ip-stale --world fixed-ip-uncertain --world fixed-ip-batch-rollback --world fixed-ip-64 --output /tmp/routerdiff-fixed-complete-iteration.json'`
→ Ran 7 tests in 0.243s / OK; stale12, uncertain20, mixed rollback22,
64-device26 cumulative (last group49.82s); go-unimplemented26,
referenced_entries24, complete=false, remaining_audits2301. No Go defect claim.

Public restart and detached rule-sync iteration (2026-11-17):
- Rebased onto local go 038b52ae (clean rebase), rebuilt the normal public Go
  binary with the route-observer-only overlay. No internal Go source was read.
- Public process restart tests now prove same-key fixed-IP preview survives,
  random startup signing key invalidates the old preview, and each restarted
  side obtains/verifies its own fresh login. No clock/token/product patch.
- Enrolment uses real plan→manual publish→receipt. Three detached HTTP sync
  jobs cover remote IPv4 delta, unchanged bytes and domain-only edit. They poll
  the public receipt to its terminal state, check SHA-256 source versions,
  exact cache bytes, device tables, route counts and unchanged config revisions.
- Added repeat-GET/after-job polling. Go failure remains a compared observation,
  not a fixture exception; a negative runner self-check proves this. Captured
  terminal reads are part of the differential, not silently discarded. The
  initial 202 response stays exact, including possible phase differences.
- Runtime global-classification networks are generated only on the test server,
  never used as network destinations. Exact addresses drive all checks and
  differences. Report-only aliases redact textual/key/base64 occurrences after
  comparison; a negative address comparison proves privacy does not turn a
  changed address into a pass. No generated address is committed/printed.
- Fake now supplies the actual singleton /routing/settings shape with 7.24.5
  policy-rules order (D09), required by sync's evidence snapshot; a missing
  singleton initially caused Python to pause and was a fixture precondition
  failure, not a product finding. All new workflows now pass real Python.

Exact .32 commands:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python build.py /tmp/routerdiff-build.AybfCK; ~/cf-test/.venv/bin/python -m unittest -v test_addresses test_recipes test_runner'`
→ /tmp/routerdiff-build.AybfCK/configflow; Ran 8 tests in 0.511s / OK.
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_addresses test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world rule-sync --world fixed-ip-restart-stable --world fixed-ip-restart-random --output /tmp/routerdiff-sync-restart.json'`
→ Ran 9 tests in 0.417s / OK; stable2, random4, rule-sync13 cumulative;
go-unimplemented13, referenced_entries7, complete=false, remaining_audits2301.
These are iteration subsets, not final combined coverage; task continues.

Fresh combined run on .32 for 2f41fcc3:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build.AybfCK/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_inventory test_seed test_compare test_recipes test_console test_boundary test_runner test_addresses; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --output /tmp/routerdiff-client-sync-composition.json --findings /tmp/routerdiff-client-sync-findings.md; cd ../..; gofmt -l $(rg --files -g "*.go"); go vet ./...'`
→ Ran 23 tests in 17.612s / OK; fixed64 group53.14s, rule-sync1574,
receipts1581, stored1629 cumulative. matrix_entries2301, routes26,
implemented0, go-unimplemented1629, referenced_entries533, complete=false,
remaining_audits2301; known Python suffix rollback bug retained. Formatting/vet
exit0/no output. Full findings retrieved from this run. 1768 design entries still
lack an attached executed recipe, and every entry's final audit remains pending.

Download-failure / proxy / linked reminder iteration:
- 41 actual HTTP recipes enrol through manual publish before exercising 503,
  invalid UTF-8, HTML/JSON/broken-YAML, comments-only, domain-only IPv4 collapse,
  >4MiB download, 100001 lines and 20001 IPv4 entries. Failed vs paused states,
  exact reached diagnostics, unchanged caches/device configurations, retry after
  failed download and manual re-enrolment after pause are checked. Large rule
  addresses are generated only in-server, remain text (never destinations), and
  report aliases also cover individual members/base64 within runtime parents.
- Explicit authenticated HTTP proxy job proves absolute-form request and Basic
  proxy credential, zero direct-source requests, successful same-byte cache.
  Fake read-only sources reject mutation methods and proxy credential mismatch.
- 11 linked R001 recipes cover alpha/beta ordering, cache reuse, both refresh
  spellings, partial single-router unknown, never reusing unknown, public-edit
  signature invalidation, ignored sidebar header and deleted linked Agent without
  a fake 404. All use live real reports; notifications are not disabled.
- Still no Go business source read/changed. Current actual manifest has no domain
  HTTP route; these are Go 尚未实现, not successful comparisons or completed audits.

Exact .32 subset commands:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_addresses test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world rule-sync-download-failures --output /tmp/routerdiff-sync-downloads.json'`
→ Ran 9 tests in 0.477s / OK; download_failures41 in8.34s;
go-unimplemented41, referenced_entries6, complete=false, remaining_audits2301.
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_addresses test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world rule-sync-proxy --world upstream-linked --output /tmp/routerdiff-proxy-reminder.json'`
→ Ran 9 tests in 0.357s / OK; proxy3, upstream-linked14 cumulative;
go-unimplemented14, referenced_entries16, complete=false, remaining_audits2301.
No test processes remain after these commands. Task continues.
