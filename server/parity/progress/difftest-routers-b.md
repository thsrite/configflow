# Routers Step B — independent public HTTP differential

Status: coordinator-requested close-out; PARTIAL coverage hand-back, NOT acceptance verified. Branch go-difftest-routers-b.
Latest clean integration rebase: goe0e9d9d45; normal public binary built from it.
Current declarations:556 executed-variant audits plus14 explicit source-unreachable
proofs plus2 O6 review-superseded entries;1729 remaining if all increments and
controls are composed. Last FULL composition still37937739:3054
recipes/301 audits. Newer subsets do not replace that full evidence; latest
large subset2470 recipes/327 audits. All observed26 domain routes remain absent
from the actual Go public manifest, so no native domain parity is claimed.
No further audit directions or full composition per coordinator. Final targeted checks and clean rebase only; coordinator runs the batch gate. History follows below.
Previous task go-n8-3 is frozen; do not modify that branch. No subagents/push.
Latest coordinator instruction: pin all new runs to .32 through rt; .64 is reserved for timing. Domain
fixtures are synthetic only. Author does not run gate.sh or proddiff; coordinator owns the batch gate. No private snapshot content was read/copied by these domain runs.
Isolated loopback
RouterOS/iKuai/Agent/rule URL/proxy/WebDAV fakes; separate namespaces/fake state
and DATA_DIR per public Python/Go server. Do not read Go implementation in
internal/routers, routersplit, devhttp (or other internal business packages).
Allowed references: difftest harness, public server entry, Python source, routes,
frontend APIs, domain matrix. Findings go to porters; never fix implementation.

Required matrix: parity/difftest-routers.md (2062 current lines; O6 approved with
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

Native TLS and IPv6 device transport iteration:
- Optional fake TLS listeners use independently generated synthetic self-signed
  certificates inside each owned namespace root, outside DATA_DIR. Real TLS1.2+
  handshake, no patched requests/crypto, no host CA/network changes. Probe rejects
  untrusted certificates with validation on, succeeds with validation off, then
  rejects again after re-enabling it. Device HTTP transcript stays empty for
  failed handshakes; failed and successful probe commits are both asserted.
- Optional IPv6-only listeners on the same private namespace ports prove actual
  RouterOS/iKuai HTTP and HTTPS communication to synthetic fd42:cf::1. IPv4 and
  IPv6 sockets have separate V6ONLY binding, each side retains its own table and
  session world. No home IPv6 device/DNS/PVE is accessed. Synthetic key files are
  deleted with the owned Pair root.
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world probe-tls --world probe-ipv6 --output /tmp/routerdiff-probe-tls-ipv6.json'`
→ Ran 2 tests in0.234s / OK; TLS6 in0.79s, IPv6 transport10 cumulative in1.00s;
go-unimplemented10, referenced_entries7, complete=false, remaining_audits2301.

Real-time token boundary / DNS publication / storage-refusal iteration:
- Fixed-IP token uses real elapsed time, no clock/signature/product monkeypatch:
  public preview → wait598s → successful add → wait6s → exact expired-token409,
  no second device request/storage write. Foreground test elapsed604.85s. Default
  composition now includes this intentionally slow world; do not call the live
  boundary complete after merely parsing/generating its recipe.
- Reference preview payloads now inspect with the actual itsdangerous library:
  known-key signature, exact scope/member shape, operation count/order, 64hex
  connection digest and no router password. Random-key world inspects unverified
  payload only; own-side HTTP apply/restart authenticates behaviour. This is a
  Python reference privacy assertion, not Go-token format emulation or cross-side
  token substitution. Negative protocol tests deliberately sign leaked-password,
  wrong-scope/digest/identity payloads and prove rejection.
- DNS guard all four block_dot/failover combinations publish/update/withdraw via
  real HTTP, preserve unchanged owned hairpin/hijack IDs and foreign redirect/
  connected route, retain DHCP domain/NTP and IPv6 RA lifetime. DHCP DNS single
  OxiDNS and IPv6 advertise-dns disable are exercised. No real scripts execute;
  Netwatch/RA traffic and failover remain L5, scripts stay exact compared content.
- Fake implements actual place-before insertion without persisting that command
  argument; actual TCP boundary test verifies device order and readback. Empty
  Netwatch table is explicit; its missing snapshot initially failed a postcondition
  (fixture only), now all workflows pass without relaxing expected device results.
- Separate plain-mask iKuai DHCP variant verifies the COMPLETE DHCP form and all
  unrelated fields survive, dns1/dns2 both explicit target; default suffix remains
  enabled everywhere else. HA fixture with primary offline and peer online proves
  VIP DHCP target + guard/Netwatch target while preserving both host exemptions.
- H0032/H0033 are NOT convenient N/A: public add after a valid2048-binding import
  hits record2049 validation; oversized real fake device ID hits binding value
  length validation. Both actual successful device writes are compensated after
  storage rejects, DB remains byte/logically unchanged, foreign rows survive.
  Dynamic undo removes the static reservation and warns of renewal impact.

Exact .32 commands/tails:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world fixed-ip-expiry --output /tmp/routerdiff-fixed-expiry.json'`
→ Ran 2 tests in0.180s / OK; real-time ageing598/598s,6/6s;
fixed_expiry3 in604.85s; go-unimplemented3, complete=false, remaining_audits2301.
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build.AybfCK/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world dns-guard --output /tmp/routerdiff-dns-guard.json'`
→ Ran 6 tests in10.325s / OK; later world failed only because missing empty fake
Netwatch table (fixed). Authoritative rerun:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world dns-guard --world dhcp-ikuai-plain --world dns-ha --output /tmp/routerdiff-dns-success.json'`
→ Ran 2 tests in0.272s / OK; guard19 in26.70s, iKuai22, HA26 cumulative;
go-unimplemented26, referenced_entries13, complete=false, remaining_audits2301.
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_tokens test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world fixed-ip-binding-cap --world fixed-ip-long-id --world fixed-ip-restart-random --world fixed-ip --output /tmp/routerdiff-binding-validation.json'`
→ Ran 9 tests in0.492s / OK; fixed13, random-key15, binding-cap17, long-id19;
go-unimplemented19, referenced_entries21, complete=false, remaining_audits2301.
No test sessions remain. Task continues; full latest composition not yet rerun.

Authentication cross-cut iteration:
- Added156 real unauthorized requests across all26 routes: a configured long
  download config token in either query spelling, Agent query token, a valid
  signed admin JWT in query, device password as Bearer and config-token cookie.
  None is an admin Authorization header; all are401 before device/storage writes.
  Guard fixture really persists the config token, not a made-up unmatched value.
- Six auth-off worlds replay198 ordinary recipes, including a successful request
  on EVERY one of the26 routes: editors/manual route add/edit/delete, real fixed
  IP and discovery, vendor publications, Agent forwarding and detached sync.
  Each public process starts normally with empty ADMIN_USERNAME/ADMIN_PASSWORD;
  Side observes authEnabled=false and sends no admin credential. Device/Agent
  credentials remain enforced by their own fakes; they are not bypassed.
- Case references remain side/world local. Ledger and findings now identify a
  recipe as world:case because the same legal recipe is intentionally replayed
  with a different auth mode. This does not increase matrix-entry denominator or
  turn a missing Go HTTP route into a pass.

Exact .32 command:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_tokens test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world guards --world auth-off-editors --world auth-off-release --world auth-off-fixed --world auth-off-discovery --world auth-off-forwarding --world auth-off-sync --output /tmp/routerdiff-auth-modes.json'`
→ Ran9 tests in0.366s / OK; guard1319, auth-off-editors1471, release1483,
fixed1496, discovery1501, forwarding1508, sync1517 cumulative;
go-unimplemented1517, referenced_entries435, complete=false,
remaining_audits2301. A separate read-only remote JSON audit of this report:
`{"auth_off_recipes":198,"auth_off_routes_with_success":26,"missing_success_routes":[]}`.
The198 auth-off recipes and156 new guards passed; this is a subset run, not the
full latest composition or final matrix audit. No pending test process.

Forwarding failure/display metadata iteration:
- Added46 public Agent boundary recipes: HTTP400/409/422/5xx status/code/message
  mapping,401/403/404, malformed/null/nonobject JSON, lost connection, Unicode
  diagnostic1000-codepoint limit, supported/blocked/persisted/system-persisted/
  notice states, bool-vs-string/int-vs-bool, exact512KiB/+1 response limits.
  Healthy second Agent survives ordinary first-Agent errors. Reads/refused writes
  assert storage and Agent state unchanged; HTTP headers no-store stay checked.
- Found a second actual Python bug, medium: unhashable forward_filter state or
  source kind/remedy source causes uncaught TypeError and whole-list HTML500.
  Four real HTTP cases and current-request log-offset checks prove the source
  path; no monkeypatch or private helper. A20/R15 owner, recorded separately in
  difftest-routers-findings.md. Go domain routes remain unregistered, so no claim
  that native Go has or fixes it. Same Go500/missing healthy entry will explicitly
  fail; safe changed outcome is reference-bug-observation for review, never pass.
  Runner negative checks cover both same-crash false-pass and malformed Go data.
- Fixed overly verbose integrity failure output: missing matrix IDs now report
  only that ID, not the entire2301-entry dictionary. Earlier incorrect F labels
  were caught before any HTTP recipe ran and corrected to reviewed F11/F13.
- Only difftest/docs changed; no Python or Go implementation repair.

Exact .32 command:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build.AybfCK/configflow --world forwarding-failures --output /tmp/routerdiff-forwarding-filter.json --findings /tmp/routerdiff-forwarding-filter-findings.md'`
→ Ran9 tests in0.624s / OK; reads31, writes44, byte limits46, malformed filter50
cumulative; go-unimplemented50, referenced_entries7,
python_bugs=[AGENT_FORWARD_FILTER_UNHASHABLE], complete=false,
remaining_audits2301. No pending process. Task continues.

Server-load rule update and new cancellation/Agent checks (2026-11-17):
- Rebased cleanly again onto then-local go65c0e6d4 after coordinator's load notice.
  Read server/scripts/gate.sh: final full race/full server tests/proddiff must use
  that two-slot gate; on .64 pass -p4. No independent full-race/proddiff started.
  Iteration is restricted to relevant routerdiff modules/worlds. Test build
  observer now defaults to Go -p4, still only overlays the public manifest hook.
- Rebuilt normal public Go binary on .64 at /tmp/routerdiff-build-302rx1dg/configflow.
  Uptime load31.83/37.13/25.92 for the build; source/public hook unchanged. Targeted
  recipe/runner tests9 passed0.369s. Domain manifest remains zero registered.
- Real TCP abandonment infrastructure sends a public request without reading its
  response, waits for a current device write gate, shuts down that exact client
  socket, checks public write guard409 while held, releases the REAL fixture
  operation/actual acknowledgement, then waits for guard400 validation refusal.
  Retry only deliberately invalid[] while409; accepted writes are never retried.
  No handler, context, Python function or Go implementation is patched.
- Six real preview/write/read cases prove R011 still saves last_release/revision
  and receipt, R005 saves binding/revision, R020 leaves the new manual route after
  disconnect. Final snapshots wait for public completion/guard and R013 L2 read;
  they are not transient mid-write samples. Own-side Ref applies equally to flow
  preparation; missing Go refs block and never borrow Python tokens. Flow guard
  responses are compared, including wrong Go early-unlock status.
- Pass-through gate executes the independent fake and returns its actual dynamic
  ID/HTTP ack; it does not fabricate a success. Real TCP fake boundary regression
  verifies the201 and new row. Two negative runner checks prove preparation-ref
  isolation and no retry of an incorrectly accepted invalid write.
- Added9 untrusted forward_filter display variants: malformed-but-hashable values
  discarded, exact primitives, stable enum ordering/dedup, source/remedy bounds,
  Unicode control/format removal and safe command whitelist. These are returned
  strings only; no displayed command is executed. Added two lost-forwarding-reply
  outcomes and real45s GET/POST Agent timeouts; query confirms actual persisted
  vs untouched state and no duplicate POST. The two timeouts took90.38s total.

Exact new .64 commands and tails:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; uptime; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python build.py /tmp/routerdiff-build-302rx1dg --parallel 4; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner'`
→ load31.83/37.13/25.92; binary path above; Ran9 tests in0.369s / OK.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; uptime; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world disconnect-release --world disconnect-fixed --world disconnect-manual --output /tmp/routerdiff-disconnect.json'`
→ load40.04/32.95/26.63; Ran13 tests in35.629s / OK; disconnect6 cumulative,
go-unimplemented6, complete=false. Quiescent refinement's authoritative rerun:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build-302rx1dg/configflow --world disconnect-release --world disconnect-fixed --world disconnect-manual --output /tmp/routerdiff-disconnect-quiescent.json'`
→ Ran11 tests in0.738s / OK; release2 in0.78s, fixed4 in1.03s, manual6 in0.35s;
go-unimplemented6, referenced_entries3, complete=false, remaining_audits2301.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; uptime; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build-302rx1dg/configflow --world forwarding-failures --world forwarding-uncertain --output /tmp/routerdiff-forwarding-composition.json --findings /tmp/routerdiff-forwarding-composition-findings.md'`
→ load34.83/33.89/28.02; Ran9 tests in0.506s / OK; failure59, uncertain61,
timeouts63 cumulative in90.38s; go-unimplemented63, referenced_entries9,
python_bugs includes AGENT_FORWARD_FILTER_UNHASHABLE; complete=false,
remaining_audits2301. No pending processes; latest full composition still pending.

HEAD dispatch iteration (2026-11-18):
- Added four independent real HTTP worlds: collection and forwarding HEAD, each
  with absent vs valid body. Two medium Python bugs: HEAD_CREATES_ROUTER and
  HEAD_PERSISTS_FORWARDING, owners R15 / R15+A20. DB revision and Agent POST
  assertions prove side effects. Original matrix retained; audit corrects R025
  HEAD read-only prerequisite. No product implementation changed.
- Named reference-bug registry renders every bug with its own owner/description.
  Safe Go differences are review observations; same unsafe mutation is failure.
  Negative runner regression exercises both DB and device mutation.
- Report now prints executed worlds, so subset statistics cannot look like a
  fresh full composition. Latest FULL composition remains the older1629 run.

Exact .64 command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner test_compare test_tokens; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build-302rx1dg/configflow --world release-ik-suffix --world release-ik-plain --world forwarding-failures --world head-collection-empty --world head-collection-body --world head-forwarding-empty --world head-forwarding-body --output /tmp/routerdiff-reference-bugs.json --findings /tmp/routerdiff-reference-bugs-findings.md'`
Remote final report exists, completed2026-11-18 00:18:07+08:00:
matrix_entries2301, routes26, implemented0, go-unimplemented71, referenced_entries15,
python_bugs=[AGENT_FORWARD_FILTER_UNHASHABLE,HEAD_CREATES_ROUTER,
HEAD_PERSISTS_FORWARDING,IKUAI_NETMASK_SUFFIX_ROLLBACK], complete=false,
remaining_audits2301. Earlier HEAD-only run: Ran12 tests0.538s/OK; 4 HTTP recipes
passed. Combined command output was truncated by the tool; final report verified
read-only on remote. Final full matrix audit and latest composition still pending.

Method dispatch and recorded policy-read iteration:
- 170 OPTIONS/dispatch recipes,5 local/live HEADs,2 discovery HEAD/cache,2 upstream
  HEAD/cache,8 recorded-policy reads/HEAD/failure. Actual Python401 OPTIONS guard
  and six-method forwarding Allow corrected fixture assumptions; not product bugs.
- Read policy_view/ros_values/config_row Python sources;6 synthetic manifest
  resources publicly imported, live table order differs and foreign same-name row
  preserved. Unknown config change affects status without leaking private fields.
- Source-exact Allow order normalization retains duplicates/membership/case,
  selftest deliberately breaks each. Fixed token inspection to apply to R006 POST
  only; OPTIONS200 has no fixed-IP token. HEAD runner selfchecks now model real
  empty HEAD bodies; no business implementation changed.

Exact .64 command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build-302rx1dg/configflow --world policy-view --world routing-methods --world head-discovery --world head-upstream --output /tmp/routerdiff-head-policy.json'`
→ policy8(1.16s), methods178(1.06s), localHEAD183(.29s), discovery185(.53s),
upstream187(.38s); matrix2301/routes26/implemented0/go-unimplemented187,
referenced_entries22, complete=false, remaining_audits2301. Earlier relevant
`python -m unittest -v test_recipes test_runner test_compare`: Ran16 tests0.447s/OK.
No pending test process; full current composition and audit remain unfinished.

Overlap and no-follow filesystem snapshots:
- Six public R001 reserved-name variants actually reach installation-state,
  install-token and allowlisted binary handlers (auth vs public capability paths
  retained). Additional required route signatures come from Python decorators
  and routes.tsv; missing external registration is separately unimplemented,
  never inferred from a business404. Negative runner check locks this oracle.
- File snapshots now include permissions, empty directories, literal symlink
  targets and special-file kinds; regular bytes remain exact. Directory-fd
  traversal, O_NOFOLLOW, opened-inode identity checks avoid traversing links or
  reading FIFOs. Only reviewed exact root DB/log names and *.lock remain excluded.
  Synthetic regression proves nested similarly named files stay visible, external
  links are not read, and bytes/mode/directory/target mutations all differ. This
  strengthens future T21 evidence; does NOT claim T21 fault coverage yet.

Exact .64 command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_filesystem test_compare test_runner test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build-302rx1dg/configflow --world routing-methods --world policy-view --world disconnect-release --world disconnect-fixed --world disconnect-manual --output /tmp/routerdiff-filesystem.json'`
→ Ran18 tests0.372s/OK; release2(.70s), fixed4(.95s), manual6(.28s),
policy14(1.17s), methods184(1.05s), localHEAD189(.28s), reserved181-world cumulative
195(.15s). Final matrix2301/routes26/implemented0/go-unimplemented195,
referenced_entries22, complete=false, remaining_audits2301. No pending process.

Return routing, actual OS faults and explicit auth audit (2026-11-18):
- Rebased cleanly onto then-go4d8a665a. HEAD now13d1b194 after rewrite; old
  pre-rebase hashes above remain historical evidence, not final branch IDs.
  Rebuilt /tmp/routerdiff-build-302rx1dg/configflow on .64 with -p4. Native
  public manifest still has0 domain registrations. No Go business code read.
- Return protocol1 and2 each run public preview/publish/noop/disable/cleanup/noop.
  Agent fake independently checks its bearer/safety/allocation/protocol; actually
  creates managed table/ip-rule/spec, preserves foreign rules and removes only
  owned objects. Real TCP boundary regression proves safety409/no mutation,
  successful PUT, refused legacy-restore, DELETE cleanup and cross-side isolation.
  Added offline +55 malformed safety/allocation cases,8 accepted allocation limits/
  padded mark normalization. Known HTTP400 refusal versus HTTP500 uncertain PUT
  have separate rollback expectations:500 records uncertain routes rollback failed
  while cleaning the known completed Agent return stage. No fake relaxation.
- Real private mount namespace faults: bind/remount only owned DATA_DIR read-only,
  public valid client draft500/unchanged, restore rw then same draft200; actual
  receipt destination-directory collision forces os.replace failure after device
  publication, response stays200, last_release ok persists, no usable baseline.
  OS state is verified; no Python helper patched and no host/production mount used.
- Full synthetic frozen-source attempt13d1b194, after real boundary4 tests23.514s,
  passed through1810 completed recipes and605.85s actual token ageing. It STOPPED
  on a fixture quiescence error: a terminal rule-sync receipt precedes task-status/
  notification persistence. Do not count it as a full pass or product finding.
  Read rule_sync.sync/task_status.record: same L1 lock covers receipt and later
  notification. Runner now waits through public invalid PUT[]409→400 after terminal
  R023 polling. It never retries accepted writes; full device/files still compared.
  Download-failure41 cases then passed repeatedly (9.20s/7.92s/6.86s).
- Go fake protocol violations now remain actual compared observations, not fixture
  aborts. Python fixture violations still abort. Negative regression proves both.
- Added explicit variant audit declarations, initially182 A1–A7 entries only; A1
  requires all6 query/header/cookie bypass attempts as well as missing header.
  Every audit names source/rationale and exact world:recipe/status prerequisites.
  Parsing IDs, another world, omitted cases or wrong outcomes cannot satisfy it;
  negative tests prove this. Tests-complete and Go-comparison remain distinct.
  This does not mark the whole fixture, route, X06 or any H completed.

Exact .64 command (20 unit/boundary tests all passed22.360s before HTTP loop;
that first HTTP attempt exposed a wrong cleanup-log index, fixed below):
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world return-protocol1 --world return-protocol2 --world return-safety --world return-rollback --world persistence-readonly --world persistence-receipt --world rule-sync-download-failures --output /tmp/routerdiff-return-os-sync.json'`
Authoritative corrected combination:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build-302rx1dg/configflow --world return-protocol1 --world return-protocol2 --world return-safety --world return-rollback --world return-uncertain --world persistence-readonly --world persistence-receipt --world rule-sync-download-failures --output /tmp/routerdiff-return-os-sync.json'`
→ Ran15 tests0.668s/OK; downloads41(6.86s), ro44(.22s), receipt47(.68s),
proto1 54(1.24s), proto2 61(1.25s), known rollback63(.58s), uncertain65(.71s),
safety121(8.09s), limits129(1.52s). matrix2301/routes26/implemented0/
go-unimplemented129/referenced23/audited0/remaining2301/complete=false.

Still open in test code: expiry's common pause starts after both sequential
previews. Once Go implements this route, slow Go preview can age the Python
token past the two-second margin. Replace with own-side request-window anchors
and validate the measurement window, then repeat the real expiry run. This is
a fixture risk, not a product finding. Dynamic identity normalization remains
fail-closed/raw, not yet integrated. Full current composition, all H reachability
and remaining G/T/D branches still unfinished; no final gate or hand-back yet.

The guard+tail combination completed on .64 (26 worlds, NOT a full run):
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build-302rx1dg/configflow --world guards --world rule-sync --world rule-sync-download-failures --world rule-sync-proxy --world upstream-linked --world dns-guard --world dhcp-ikuai-plain --world dns-ha --world disconnect-release --world disconnect-fixed --world disconnect-manual --world policy-view --world routing-methods --world head-discovery --world head-upstream --world head-collection-empty --world head-collection-body --world head-forwarding-empty --world head-forwarding-body --world auth-off-editors --world auth-off-release --world auth-off-fixed --world auth-off-discovery --world auth-off-forwarding --world auth-off-sync --world stored --output /tmp/routerdiff-audited-tail.json --findings /tmp/routerdiff-audited-tail-findings.md'`
=> stored guards1865 cumulative(1.88s), matrix2301/routes26/implemented0,
go-unimplemented1865/referenced507/audited182/remaining2119/complete=false.
Python bugs in this subset: HEAD_CREATES_ROUTER/HEAD_PERSISTS_FORWARDING.
No current test process from this command. The full frozen attempt failed earlier;
this successful subset must not replace it with a false full-pass statement.

Own-side age-window refinement (supersedes the fixture risk above):
- Record monotonic request start/end on each side. Near-expiry apply waits until
  598s after its OWN preview start and requires the whole response before599.5s.
  Expired apply waits604s after its OWN preview response end. No clock patch or
  Python token is supplied to Go. Missing anchors/missed windows are explicit
  fixture failures; a negative runner test proves no Python-window fallback.
- Independent fake listener shutdowns now run together, keeping full cleanup
  joins while avoiding six sequential poll delays; no application lifecycle change.
- Exact .64 selftest command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_runner test_recipes test_boundary test_filesystem test_compare'`
=> Ran26 tests15.557s / OK. The revised real604s expiry still needs execution,
and the latest full composition/final rebase/gate remain outstanding.

Fresh complete CURRENT-RECIPE composition, source cb95c456 (.64):
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; uptime; frozen_root=$(mktemp -d /tmp/routerdiff-composition.XXXXXX); rsync -a --exclude .git ./ "$frozen_root/"; printf "frozen synthetic source: %s\n" "$frozen_root"; trap '\''rm -rf "$frozen_root"'\'' EXIT; cd "$frozen_root/server/tools/routerdiff"; ~/cf-test/.venv/bin/python -m unittest -v test_inventory test_seed test_console test_addresses test_tokens; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --output /tmp/routerdiff-composition-cb95c456.json --findings /tmp/routerdiff-composition-cb95c456-findings.md'`
=> load28.26/28.39/24.39; Ran9 tests0.836s/OK; own-side real expiry605.27s
passed. Final stored guards2435 cumulative1.48s. Matrix2301/routes26/implemented0,
go-unimplemented2435/referenced611/audited182/remaining2119/complete=false.
All four known Python bugs reproduced; actual Go HTTP comparisons0. This is a
successful composition of the recipes THEN WRITTEN, not a completed matrix.
Frozen synthetic copy was deleted by EXIT trap; remote reports stay under /tmp.
Main findings replaced with this fresh full report (all4 classes, no old subset
totals presented as fresh full totals). No final Go gate was run.

IPv6 Fake-IP / notification / additional OS iterations after cb95c456:
- Read all ipv6_fakeip.py, its _lan/_gateway/_same_gateway helpers and real Agent
  request boundary, public custom-Mihomo save route and publication manifest write.
  28 initial recipes prove LLA creation/manifest, owned synced state, lifetime/
  counters leaving the old fingerprint valid, DNS IPv6 mode switch without route
  replacement, owned-only withdrawal, borrowed LLA and existing ULA retained,
  nine route-conflict flags and five unusable host flags. Actual public config
  POST supplies mode changes; no private product helper performs mutations.
- Added2 stronger R018 HEAD notification cases: a real IPv6 blocker creates a
  pending notification, then recovery removes that exact key while retaining
  the separate unpublished-changes notification. The earlier unlinked empty
  report HEAD proves device I/O only, not a pending notification. File JSON
  path prerequisites/absence are asserted WITHOUT masking raw file comparison;
  negative runner tests reject missing/wrong/stale notification evidence.
- External SQLite BEGIN IMMEDIATE lock is an OS fixture, with no data updates.
  Both normal public processes receive the documented
  CONFIGFLOW_LOCK_TIMEOUT_SECONDS=0.25 in dedicated worlds (default300s would
  exceed the harness transport window). Draft500 leaves DB unchanged; release
 500 occurs AFTER device writes, leaves one new route, no last_release/baseline;
  releasing the external writer restores normal saves. No simulated SQLite error.
- Fake ROS now refuses identity/runtime fields in PUT/PATCH and connected-row
  mutation instead of accepting them silently. Closed console writes use the
  same device guard. Real TCP negative fixture checks six bad payload/row cases
  all400, state unchanged and no fixture errors. Unknown writable matchers stay.
- Missing mutating HTTP fixture setup blocks later comparisons even when its
  main request was a GET/plan. Explicit external route requirements prevent a
  missing custom-config route from being treated as a business response.

Initial IPv6 .64 command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build-302rx1dg/configflow --world ipv6-fakeip-owned --world ipv6-fakeip-borrow-lla --world ipv6-fakeip-borrow-ula --world ipv6-fakeip-conflicts --output /tmp/routerdiff-ipv6-fakeip.json'`
=> Ran16 tests0.459s/OK; owned10(4.96s), LLA12(1.26s), ULA14(1.28s),
conflicts28(4.78s); referenced3, go-unimplemented28, no new Python bugs.

Authoritative current combination on .64 (including tightened ROS fake):
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world ipv6-fakeip-owned --world ipv6-fakeip-borrow-lla --world ipv6-fakeip-borrow-ula --world ipv6-fakeip-conflicts --world head-report-notifications --world persistence-sqlite-busy --world persistence-publication-busy --world dns-guard --world fixed-ip --world fixed-ip-batch-rollback --world fixed-ip-uncertain --world return-protocol1 --world rule-sync --world policy-view --output /tmp/routerdiff-ipv6-os-readonly-regression.json'`
=> Ran23 tests13.458s/OK; fixed13(3.06s), uncertain21(3.22s), rollback23(2.34s),
sync32(3.38s), DNS51(25.47s), owned61(5.02s), LLA63(1.31s), ULA65(1.30s),
conflicts79(4.76s), HEAD81(.77s), SQLite84(.48s), publication87(.93s), return94(1.27s),
policy102(1.26s). Matrix2301/routes26/implemented0/go-unimplemented102/
referenced62/audited0/remaining2301/complete=false. No pending test process.

Still open: audit the remaining2119 design entries and write their remaining
variants, native dynamic identity/time/token handling, concurrent R001 read
transcript interleavings (do NOT sort mutation order), startup failure vs fixture
failure classification, policy bundle and NAT-list scale/migration/rollback,
IPv6 public rule routes/RA/PD scripts and failures, background scheduler rounds,
backup/proxy, SIGTERM/KILL mid-write and Nginx ingress boundaries. Then fresh full
composition, final gate through gate.sh (never standalone full race/proddiff),
latest git rebase go/build/recheck/clean. Task is actively continuing, no hand-back.

Explicit profile-guard audits:
- Re-read profile_context resolution and before-request hooks, global auth and
  upstream explicit-profile handling. Added80 auditable entries: non-global
  F08/F09 three selectors each and F90 six unauthenticated combinations; global
  F13/F14/F90/B90 plus F91's two empty-linked and one real alpha/beta-linked case.
  Every requirement names its exact world/case/status and source. Selector
  precedence as a whole remains X02 pending. Original R001-F08/F09 stay pending
  their explicit supersession audit, not silently counted from the wrong guard.
- Negative audit test requires both failure kinds/all three selectors and keeps
  global/non-global contracts separate. No product or fake implementation change.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary /tmp/routerdiff-build-302rx1dg/configflow --world guards --world runtime-reads --world upstream-linked --output /tmp/routerdiff-profile-audit.json'`
=> Ran4 tests0.662s/OK; guards1319, runtime1330(.92s), upstream1341(3.04s).
Matrix2301/routes26/implemented0/go-unimplemented1341/referenced341/
audited262/remaining2039/complete=false. No Python bugs in this selected subset.
The last full composition remains cb95c4562435 recipes/611 referenced/182 then
audited; this new audit subset proves80 more, not a new full composition.

Settings/caller audit and legacy credential repair:
- Source-call search corrected H0154-H0161/H0491-H0492 scope: only store.normalize
  calls these validators, so actual public R008/R010 cases provide their error
  evidence; report/plan cannot be assumed to reach them. Added exact H attachments,
  missing JSON member types/blank/CJK/fullwidth input, iKuai unsupported enabling,
  actual existing-iKuai edits and shared field guards on both vendors. iKuai stops
  at its preceding vendor check before overlap/missing-DNS guards; explicit HTTP
  counterexamples retain that source proof. No product implementation changed.
- Added8 successful settings controls: trim/mask/stable dedup,64 duplicate vs64
  unique items and whole-field null default restoration on both vendors. Removed
  an over-broad G21 attachment: a settings reset does not prove PD event scripts.
- 17 real legacy cases load four empty/missing-password records through the public
  Python import fixture; GET opens them, rename/null/empty credential edits refuse
  with the correct password message, public repair succeeds and preserves name/
  unknown stored fields. This differs from the un-loadable missing Agent profile
  fixture; never bypassed validation or injected Go private state.
- Three readbacks after the rejection batch guard against an implementation that
  mutates in-memory router/device/workspace state before returning400; DB-only
  snapshots would miss it. They are extra controls, no fake coverage ID added.
- Removed the auth batch's preparatory heartbeat: Agent capabilities are valid in
  the registry while offline. A separate real public HTTP boundary test proves
  valid/wrong heartbeat tokens and admin refusal; Go checked only for routes in its
  actual public manifest. This avoids unmatched heartbeat timestamps or a missing
  R001 cascading into skipped otherwise-implemented auth routes. The auth world
  now has no preparatory mutation; updated audit rationale accordingly.
- Explicit14 child/parent settings audits require all named shape/loop/vendor
  variants. Integrity selftest checks every requirement exists with the intended
  H attachment/status; not derived by simply selecting whatever claims that H.

Earlier legacy/default control subset on .64: Ran4 tests0.448s/OK, legacy17(.26s)
and defaults25 cumulative(.19s), report /tmp/routerdiff-legacy-settings.json.
Authoritative expanded run:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world guards --world legacy-credentials --world router-guard-normalization --world runtime-reads --world upstream-linked --output /tmp/routerdiff-settings-guard-audit.json'`
=> Ran12 tests45.048s/OK; guards1573 including readbacks(.94s), legacy1590(1.23s),
defaults1598(1.39s), runtime1609(1.18s), upstream1620(3.16s). Matrix2301/routes26/
implemented0/go-unimplemented1620/referenced353/audited276/remaining2025/
complete=false, no new Python bugs. No pending test process. Last full composition
is still cb95c4562435 recipes/611 references, before these additions. Final full
composition, gate and rebase remain pending; task is not ready for hand-back.

Client normalizer branch audit (next incremental step):
- Read full client_devices.py/device_notes.py/client_discovery_cache.py and range/
  policy normalization in oxidns_device_policy.py. Added explicit12 H audits:
  H0023/H0024/H0068/H0071-H0079. Every requirement names the exact guard world,
  case and HTTP status, with integrity checks on attachment. No H was closed by
  collecting arbitrary cases with a matching label.
- R003 guard batch now146 cases: every non-string address family, malformed
  multiple slash/zone/CJK/fullwidth address, empty/signed/mixed/overlong/space/
  fullwidth prefix, v4/v6 overflow, mapped prefix<96, host/network multicast,
  unspecified hosts and actual address parse errors. Name and previous_name each
  cover non-string,81 ASCII/Unicode/whitespace chars, every C0 codepoint and DEL.
  Both stale previous-name and previous-mode409 are separate; corrected the
  latter's route attachment from F13 to F14.
-12 independent accepted public controls cover canonical host/CIDR/mapped forms,
  mapped /96 and IPv6 /0, zero-padded prefix, exact80 Unicode chars (>80 bytes),
  quotes/trim, normalized previous name/address removal and beta isolation.
  Notes-only controls currently assert exact response/stored note/no device I/O;
  an explicit unrelated-OxiDNS-field retention oracle remains to add.
- No product files or Go implementation read/changed. Reports remain incomplete;
  latest full composition is still cb95c456, not this subset.

`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world guards --world client-canonical-names --world client-drafts --output /tmp/routerdiff-client-normalizer-audits.json'`
=> Ran6 tests0.894s/OK; guards1650, client-drafts1661(1.34s), canonical1673(1.21s).
Matrix2301/routes26/implemented0/go-unimplemented1673/referenced360/
audited287/remaining2014/complete=false. This subset lacks R001-F91's linked
world; all existing+new declarations total288, so the full remaining count is2013
if those already-proven worlds are composed. No new Python bug in this subset.

Notes persistence and resource retention controls:
- Added retained_fields assertions for source-proven no-change profile fields;
  deliberate DNS-rewrite negative runner test proves the assertion can fail.
  Canonical notes cases now retain full oxidns/router/fixed-IP settings, rather
  than inferring retention from device silence.
-4 notes-capacity requests prove2049 refusal/no partial write, existing2048 edit,
  removal and freed-slot acceptance.7 mode-note requests prove original native/
  legacy names unchanged, stale names_revision409 vs modern editor old-revision
  success, empty-note override and final hidden-name cleanup.4 real preview/
  apply/rename/remove requests prove local binding removal keeps actual DHCP
  reservation and makes no device requests.
- Added8 real import rejection probes for invalid notes types/canonical duplicate
  aliases/2049 cap. Their test has not yet been run; H0069/H0070 remain unclosed,
  distinguishing load barrier from reachable normal write overflow.

.64 exact controls:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world client-canonical-names --world notes-capacity --output /tmp/routerdiff-notes-retention.json'`
=> Ran18 tests0.733s/OK; canonical12(.45s), capacity16 cumulative(1.01s), Go missing16.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world client-mode-notes --world notes-keep-reservation --output /tmp/routerdiff-notes-policy-reservation.json'`
=> Ran18 tests0.690s/OK; mode-note7(.44s), reservation11 cumulative(1.12s), Go missing11.
Both reports remain complete=false, no new Python bugs; they do not replace the
last full composition report. No test process pending. Import rejection probes,
remaining2013 audits, full composition, final gate/rebase remain pending.

Stored-policy and prerequisite-oracle increment:
- Added device_policy.py:65 invalid public-import worlds ×3 actual public HTTP
  callers=195 requests, plus3 legacy/capacity positives. H0080-H0084 explicitly
  require all source-reviewed variants. Full pass on .64; declared audits now293,
  remaining2008 when composed, not a completed matrix.
- Real notes import rejection probes now passed (8 invalid variants). Invalid
  notes load barriers remain pending, not conveniently counted as HTTP coverage.
- Fixed another test-oracle gap: auxiliary public setup responses were ignored on
  success, and an actual bad Go response aborted as a fixture failure. They are
  now recorded/compared; a failed Go setup blocks the dependent main request and
  later state-dependent writes, with the actual response retained. Python setup
  failure remains a genuine fixture error. Public-manifest matching separately
  labels missing auxiliary HTTP routes Go-unimplemented (never business404).
  Deliberate502,200/success:false and scalar-body negatives prove no abort/no
  invented main response/no dependent mutation. Existing side-local-reference
  negative updated with an explicit available auxiliary route, keeping the
  original no-cross-side-value proof distinct from missing implementation.
- Findings renderer now includes blocked prerequisites with actual differences,
  rather than silently omitting them. Full findings file not overwritten with a
  subset. Auxiliary native timestamps still need the planned scoped comparison;
  this change deliberately does not mask those differences.

Actual .64 commands:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_seed test_recipes test_audits; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix stored-policy- --output /tmp/routerdiff-stored-policy-audits.json'`
=> Ran11 tests5.071s/OK;198 recipes, Go missing198, referenced9, audited5,
remaining2296 for this isolated subset; complete=false, no new Python bugs.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_runner test_recipes test_audits; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world stored-policy-legacy --world discovery-failures --world client-mode-notes --world notes-keep-reservation --output /tmp/routerdiff-prerequisite-guard.json'`
=> Ran26 tests1.657s/OK; mode7(.55s), legacy9(.29s), reservation13(1.19s),
discovery22(3.29s), Go missing22, complete=false. No pending process. Final full
composition still pending; this is not ready for hand-back. Still2008 audits,
dynamic comparison integration, startup-failure classification and deeper device/
background cases remain. All final full/race/proddiff commands must use gate.sh;
latest explicit coordinator instruction says -p4 on .64 (overrides older docs-p6).

Rebased onto go f73983fb; latest native public binary rebuilt at
/tmp/routerdiff-build-302rx1dg-latest/configflow. Prior commit IDs above are
historical command/source provenance (rebase rewrote branch IDs). .64 load at
rebuild27.98/29.03/27.06. Exact smoke command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; uptime; export PYTHONDONTWRITEBYTECODE=1; ~/cf-test/.venv/bin/python server/tools/routerdiff/build.py /tmp/routerdiff-build-302rx1dg-latest --parallel 4; cd server/tools/routerdiff; export ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world client-mode-notes --world stored-policy-legacy --world discovery-failures --world guards --output /tmp/routerdiff-rebase-f73983fb-smoke.json'`
=> native build OK; Ran7 tests23.377s/OK; guards1650, mode1657, legacy1659,
discovery1668(5.02s). Matrix2301/Go routes0/missing1668/audited287/remaining2014
in this subset. Source at a66ce734 after rebase, before next additions.

Native startup and client-success audit increment:
- Namespace RPC errors now carry operation context. If Python public startup
  succeeds but Go's actual public child process exits during start, preserve the
  real exit code as a process difference, with no fabricated HTTP500 or missing-
  implementation classification. Live/slow listeners and RPC/environment errors
  remain fixture errors, not product conclusions. Unknown route inventory is
  null/native_inventory_observed=false rather than falsely asserting zero routes.
- Real negative launches /usr/bin/false in the owned Go worker (test-oracle probe
  only, excluded from product reports), confirms exit1, and independently serves
  actual Python HTTP200. Runner negative confirms actual process difference and
  no invented Go response. This is not a finding against the real Go binary.
- Added8 explicit R003 success audits: ordinary new/mode/name transitions plus
  native retention, hidden legacy-name cleanup,2048-note boundary and actual
  DHCP binding retention. Added a pure smart→default transition with unchanged
  name to complement prior combined name+mode edits. All required42 requests
  completed against actual Python; Go public routers remain unregistered.
- Explicit declarations now301, remaining2000 when all proven worlds compose.
  Complete=false. Main findings remain the old full composition; no hand-back.

`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_runner test_boundary test_inventory; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world client-mode-notes --world release-ik-suffix --output /tmp/routerdiff-startup-classification.json --findings /tmp/routerdiff-startup-classification-findings.md'`
=> Ran30 tests28.401s/OK;9 recipes; native_inventory_observed=true, routes0,
Go missing9, known IKUAI_NETMASK_SUFFIX_ROLLBACK reproduced; complete=false.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world client-drafts --world client-canonical-names --world notes-capacity --world client-mode-notes --world stored-policy-legacy --world notes-keep-reservation --output /tmp/routerdiff-client-success-audits.json'`
=> Ran9 tests0.789s/OK;42 requests, references24, audits8 in this subset,
Go missing42. No process pending. Full composition/gate/final rebase still pending.

FULL current composition completed, immutable source37937739 on go f73983fb:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; uptime; frozen_root=$(mktemp -d /tmp/routerdiff-composition.XXXXXX); rsync -a --exclude .git ./ "$frozen_root/"; printf "frozen synthetic source: %s\n" "$frozen_root"; trap '\''rm -rf "$frozen_root"'\'' EXIT; cd "$frozen_root/server/tools/routerdiff"; ~/cf-test/.venv/bin/python -m unittest discover -v; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --output /tmp/routerdiff-composition-37937739.json --findings /tmp/routerdiff-composition-37937739-findings.md'`
=> Load17.33/29.02/28.16; Ran51 tests23.266s/OK.3054 recipes; all actual Python
prerequisites passed; Go routes0/compared0/go-unimplemented3054; references649,
audited301/remaining2000/complete=false. All4 registered Python bug classes
reproduced. Real token expiry605.22s, real gateway timeouts91.41s. Frozen directory
/tmp/routerdiff-composition.vQ3A2O removed by EXIT trap. Full findings file fetched
from this report; it supersedes the old2435 recipe composition. This is not the
complete2301-entry matrix and not a Go parity pass.

Next source increment also passed, .64 load10.24/12.71/18.66:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; uptime; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest discover -v; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world guards --world router-guard-normalization --world empty-manifests --world stored --world legacy-credentials --world client-mode-notes --world discovery-coalescing --world discovery-lru --world discovery-staleness --output /tmp/routerdiff-cache-settings-audits.json'`
=> Ran54 tests20.951s/OK; guards1666, mode1675, empty1690, legacy1707,
normalization1743, coalescing1746(2.04s), LRU1880(72.66s), stale1885(66.29s),
receipts1892, stored1940(2.72s). Go routes0/missing1940/referenced387/
audited310/remaining1991 in this selected subset.23 new declarations proven;
combined declared total324, remaining1977. No new Python bug. All queued/running
commands completed; fetched full report with:
`RT_HOST=sunbroke@192.168.0.64 rt --no-sync --back server/parity/difftest-routers-findings.md -- bash -lc 'set -euo pipefail; cp /tmp/routerdiff-composition-37937739-findings.md server/parity/difftest-routers-findings.md'`

Changes since that full composition:16 extra non-object R010 bodies,28 interval/
boolean/ignored-secret positive controls,15 truly empty manifest writes/deletes,
134 real profile LRU requests,3 simultaneous refresh controls and5 real stale/
restart controls. RPC start-exit classification also covers failed restarts;
selftest proves no fabricated Go HTTP response. Fixed stale names_revision's
attachment to R015-F16 (was incorrectly F15; HTTP expectation409 unchanged).
All changes are tests/docs, no product code or Go business implementation read.
T03 still pending its explicit integrated audit, as are native policy graph
cases, deeper device/backgound branches, scoped dynamic comparison and external
G23 dispatch. Final full composition/gate/final rebase remain pending; no hand-back.

T03 explicit audit completed (helper OS-thread failures remain separate):
-161 named requirements integrate default/repeated/forced/literal-true reads,
  current note join, good-snapshot outage retention, partial replacement/unknown
  saved rows, real61s stale boundary,129-profile128-capacity LRU, simultaneous
  refresh sharing and restart. Added9 credential/TLS/no-op-signature requests:
  wrong locally saved password really gets a device401/new failed snapshot,
  restoration and TLS-setting change re-read, same router fields with a new
  profile revision reuse the good cache. Fake credentials were not relaxed.
- New cache_options fixture returns the same single-router synthetic options as
  the preceding inline expressions; no app state/clock helper is imported.
- All161 requirements' exact world/case/status/entry attachments have integrity
  checks. T03 does not imply R004 is complete, does not close H0030/H0031 resource
  failures, and does not claim physical presence or unregistered Go route parity.
  Declarations now325/remaining1976 if composed; last full3054 report still301.

`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world discovery --world discovery-failures --world discovery-signature --world discovery-coalescing --world discovery-lru --world discovery-staleness --output /tmp/routerdiff-cache-complete-audit.json'`
=> Ran11 tests2.407s/OK; discovery5(1.58s), signature14 cumulative(2.20s),
coalesce17(1.56s), LRU151(86.03s), stale156(63.66s), failures165(2.96s).
Go routes0/missing165/references17/audited1/remaining2300 in this subset;
complete=false, no new Python bug. No active or queued command remains.
Next: native device-policy graph read-only/atomic rejection and reachable H0085-
H0101, remaining wrapper audits, deeper device/generator/background branches,
scoped dynamic identities/timestamps/hashes and G23 external manifest dispatch.

Native policy graph main-chain increment:
- New policy_graph.py uses literal native documents, public repository import,
  and only HTTP for every operation.18 malformed/unrecognized main graphs each
  perform R002200 editable=false/exact reason, R003 mode-change400 with complete
  state unchanged, R003 name-only200 retaining full native/settings bytes, and
  R014200 with the same read-only reason. Includes include, missing/duplicate tags,
  missing/wrong main/args/exec/tail and missing/wrong/empty local upstream.
-3 ordinary native controls read recognized main, insert direct policy, then
  rename without rewriting native settings. No helper is called to manufacture
  expected application output; expected diagnostic fragments are literal data.
- Helper H0086/H0100/H0101 attached only to reached branches, but not closed:
  listener/ownership/side-entry families remain to implement. R002-F12/R003-F15
  also remain pending their complete graph variants. No new declared audit yet.

.64 pilot: Ran4 recipe integrity tests0.400s/OK;19 requests passed.
Authoritative full current graph batch:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix native-policy- --output /tmp/routerdiff-native-policy-main-guards.json'`
=>75 requests, Go routes0/missing75/references8, complete=false, no new bug.
All actual Python assertions passed. No active/queued command. Declarations still
325, remaining1976; last full composition remains379377393054 recipes/301 audits.
Task remains in progress; final full/gate/rebase pending.

Native listener graph increment:
-26 independent public-import worlds ×4 HTTP operations=104 requests. Covers
  no DNS listener, non-object args, invalid entry values, HTTP entries outer/member/
  exec shapes, no listener directly entering main, unset/env/zero/overflow/bad/
  client-port side listeners, missing/dynamic reach targets and indirect main.
  Every world proves safe read-only diagnostics, atomic refused policy change,
  usable note-only editing with exact native retention, and workspace readback.
- H0087/H0088/H0089 attached only to the helper guards actually reached, but no
  audit is closed yet. Main custom listener ports, inner dynamic references,
  legitimate side/node-test listeners and policy ownership still to add.

`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix native-policy-listener --output /tmp/routerdiff-native-policy-listeners.json'`
=> Ran4 tests0.360s/OK;104 requests, routes0/missing104/references9,
complete=false, no new Python bug. No pending process; declarations325/remaining
1976 unchanged. Last full composition remains3054/301. Task not ready to hand back.

Native main/listener helper audit increment:
- Added custom main5353 port conflict and dynamic references inside a side chain,
  plus boolean/number args and HTTP entry shapes.50 literal negative graphs now
  instantiate all _refuse caller families through R002200/R003400; every graph
  also preserves native bytes through note-only editing and workspace readback.
- H0085-H0089 explicit5 declarations completed. H0087's non-string operand is
  source-unreachable at public call sites (inspect_entries first normalizes it
  to text); the actual ${PORT} operand is exercised. No helper-call fake used.
- Added stored_contains prerequisite (full state comparison unchanged) to prove
  valid direct insertion really writes both managed plugins, client matcher and
  hook. Missing-hook/empty/null/object negative runner controls fail as intended.
  Empty-policy vs512 smart-only vs direct insertion provide H0085's controls.

.64 pilot: Ran25 tests0.842s/OK,20 selected requests passed.
Authoritative batch:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix native-policy- --world stored-policy-capacity --output /tmp/routerdiff-native-policy-helper-audits.json'`
=> Ran34 tests1.842s/OK;204 requests, routes0/missing204/references12,
audits5 in this subset/remaining2296/complete=false. Declarations330/remaining1971
if composed. No new Python bug. No pending command. Untracked policy_owned.py
is the next input-fixture step, not yet wired/tested/claimed. Main findings and
last full composition remain3054/301; final gate and rebase pending.

Native policy ownership audit increment:
- policy_owned.py supplies literal recorded direct rows and managed native
  plugins/hook through public import.16 negative worlds ×4 public operations
  prove missing/altered plugins, absent/duplicate/conditional hook, misplaced
  hook before built-ins/after custom actions, each unrecorded name conflict and
  extra goto/jump reference. These are direct ownership errors, never relabelled
  _refuse. Name-only updates retain complete native bytes even while readonly.
-3 valid controls recognize old ownership, change direct→smart and remove both
  managed markers from actual stored native text, then rename without rewriting
  native. New stored_excludes and deliberate residual/null/object negatives
  prove withdrawal cannot pass with stale managed output.
- H0096-H0099 explicit4 declarations require all guard variants plus positive
  ownership/actual cleanup. Declared334, remaining1967 if composed. H0100/H0101
  parent and native node-test side-listener classification still pending.

.64 pilot: Ran27 tests0.595s/OK;67 requests passed.
Authoritative command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix native-owned- --world native-policy-valid --output /tmp/routerdiff-native-owned-audits.json'`
=> Ran37 tests1.727s/OK;70 requests, Go routes0/missing70/references13/
audits4 in this subset/remaining2297/complete=false, no new Python bug.
No pending command. Last FULL composition remains379377393054 recipes/301 audits;
current source has more cases/audits, no new full composition yet. Final gate/
rebase still pending; task not ready to hand back.

Maintainer clarified final gate scope this turn: run gate.sh normally, including
read-only private-snapshot proddiff. No new synthetic-gate entry is needed. The
handwritten domain cases/fixtures remain synthetic only. Never copy snapshot
contents into code, tests, docs or reports; report only proddiff new/stale/
self-check totals. Keep raw private gate/proddiff logs on the test server.

Native side/legacy listener audit and fifth reference bug:
-23 valid literal side graphs ×3 public operations=69 requests, plus2 independent
  collision bug requests. Public system DNS contexts, exact/direct and all3
  wildcard forms, profile-bound disabled OxiDNS registry, grouped UDP/TCP,
  HTTP/TLS/multi-entry, nonsequence/nested/foreign/no-forward/shared-forward
  classification. Actual direct save retains opaque side upstream settings;
  note-only saves retain complete native bytes. H0090-H0095 six audits proven.
  Declarations340, remaining1961 if composed; whole matrix still incomplete.
- First batch failed an honest fixture precondition at editable=false. Real
  public HTTP probe exposed a source defect, not a fake protocol failure:
  reference_locations scans URL strings for known plugin names, so main tag udp
  collides with side forward udp://, falsely reaching main. Neutral-tag counterpart
  succeeds. Original input remains in a named low Python bug world with actual
  GET200 readonly and PUT400. Owner C26/R17; no implementation fix by this agent.
  Matching Go refusal is deliberately a difference; correct Go editor output
  remains reference-bug-observation requiring review, never a false pass.
- The19 neutral-tag pilot worlds57 requests passed. Extended authoritative run:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix native-side- --output /tmp/routerdiff-native-side-audits.json --findings /tmp/routerdiff-native-side-findings.md'`
=> Ran40 tests1.650s/OK;71 requests, Go routes0/missing71/references9/
audits6 in this subset/remaining2295/complete=false, new reference bug
NATIVE_SIDE_REFERENCE_PROTOCOL_COLLISION. No process pending. New bug evidence
fetched as synthetic temporary report, appended to main findings with an explicit
separate-batch note, then temporary local file deleted. Existing3054/301 full
statistics unchanged; they are still the last full composition.

Next work: native normalization/parent diagnostics, remaining HTTP wrapper guard
closure, G23 external-public-route dispatch, deeper policy/NAT/IPv6/background/
restart branches and strict scoped dynamic comparison. Final gate/rebase pending.

Native normalization and parent/API diagnostics:
-54 public-import malformed settings worlds ×4 HTTP operations=216 new requests:
  mode/native text types, empty/scalar/malformed/duplicate/unsafe-key/cyclic/
  nonfinite/binary YAML, attachment outer/row/count257, bad/unsafe/reserved paths,
  duplicate and both file-directory orders, content/encoding/base64 errors.
  No attachment is materialized. Each safely reads readonly, atomically refuses
  mode change, allows notes with complete settings retention and reads workspace.
- H0100/H0101 and R002-F12/R003-F15 explicit4 declarations require120 negative
  worlds from normalization+main/listener+ownership, plus ordinary parent-success
  controls. Invalid raw policy rows fail before the read_policies try and remain
  their own H0080-H0084/API400 family, not incorrectly classified readonly200.
  No claim of exhaustive imported C22 parser internals outside routers inventory.
- Declarations344, remaining1957 if composed. Full matrix remains incomplete.

.64 pilot: Ran7 tests0.398s/OK;20 selected requests passed.
Authoritative command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix native-policy- --world-prefix native-owned- --world-prefix native-invalid- --world stored-policy-capacity --output /tmp/routerdiff-native-parents.json'`
=> All selected selftests OK,487 requests passed; routes0/missing487/references17/
audits13 in this subset/remaining2288/complete=false, no new Python bug. Confirmed
with a read-only JSON summary afterward (no test rerun). No active command.
Last full remains379377393054 recipes/301 audits; newer independent batches do
not replace it. Next priority G23's real external-public-route oracle and simple
config/Agent generation guards, alongside remaining wrapper/device/background
coverage and dynamic-value comparison. Final gate/rebase still pending.

G23 external boundary oracle foundation:
- Case.boundary declares an actual external public method/template with empty
  routers route ID and explicit owner. Registered concrete requests match the
  real Go manifest (including a generic service segment), independent of missing
  R012/etc. Contradictory metadata is a fixture error. No business404 inference.
- Results/report separate routers and cross-domain recipe counts. Cross-domain
  comparison cannot inflate26 routers routes or claim a domain route covered.
  Findings carry the explicit external owner. Main26 inventory unchanged.
- Deliberately wrong500 external response produces an actual difference even
  with every router route absent; absent external manifest skips without sending
  Go HTTP. Existing runtime-read regression still uses the original domain oracle.

`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_runner test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world runtime-reads --output /tmp/routerdiff-external-oracle-regression.json'`
=> Ran31 tests0.635s/OK;11 runtime requests, domain missing11, cross-domain{},
complete=false. No process pending. Actual external G23 input recipes are next;
no new comparison/coverage claim yet. Declarations344/remaining1957 unchanged.

G23 marker / final Agent TUN public guard increment:
- Public manifest was re-read through the real Go process:181 method/path rows;
  it contains neither these config preview nor Agent final-generation routes.
  Missing external routes are independently observed, not inferred from404 or
  from the absence of26 routers routes. No Go business implementation was read.
-103 real Python HTTP recipes:57 invalid marker inputs, gateway conflict,
  30 missing/modified full-TUN shapes,11 filter keys plus combined ordered
  diagnostic, and3 accepted marker boundary controls. All100 refusals check
  exact message, complete state unchanged; Agent refusals also require zero
  Agent requests. Positive previews check the real content-bearing schema.
- First whole increment stopped after100 successes because I incorrectly
  assumed preview success included success=true. Actual public Python source
  returns content/subscription_hosts/subscription_dialers without that field.
  Fixed only the test prerequisite; reran all3 positive controls. Previous100
  observations retained in the incomplete report, not represented as a clean
  whole run. The two actual observation files satisfy all4 explicit H0110–13
  requirement sets when evaluated together. No implementation or fake changed.
- Declarations348/remaining1953 if composed. These4 are source/HTTP guard audits,
  not a claim that all G23 helpers/generation or Go parity is complete. H0102–09
  and H0114–18 still require their actual callers and branch variants. No new
  Python bug. All103 external requests are Go尚未实现; domain count unchanged.

Exact .64 commands:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix split- --output /tmp/routerdiff-split-all.json --findings /tmp/routerdiff-split-all-findings.md'`
=> Ran8 tests1.391s/OK;100 real rejection recipes; then fixture_error 'success'.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix split-valid- --output /tmp/routerdiff-split-valid.json'`
=> Ran21 tests2.376s/OK;3 valid previews, missing3/complete=false. In the same
foreground shell a read-only Python evaluation joined results from the two JSON
reports and called audits.evaluate: real_http_recipes103, H0110/H0111/H0112/H0113
all variants-audited, go_statuses[go-unimplemented]. No product requests mocked.

No command pending. Last FULL composition remains37937739,3054 recipes/301 audits;
the above is a separate increment, not a replacement full report. Main findings
full statistics remain unchanged. Final composition/gate/rebase still pending.

G23 actual wizard preparation and read-only state increment:
-31 real gateway-onboarding previews reach full_tun_document/_load_base, unlike
  ordinary config preview. Nonobject tun, all11 filters, five nontext base types,
  real syntax/unsafe-constructor/1500-level recursion errors and five nonobject
  roots. Three controls (empty base, null tun, custom stack/mtu) pass both service
  render/validation phases and stop at the later no-proxy prerequisite. All
  reports remain200/ready=false with exact sole blocker; never claim an install.
-27 real Linux gateway editor GETs cover root/old marker/TUN shape, actual YAML
  fallback, full-TUN positive and11 separate filter diagnostics. They preserve
  complete stored bytes/device state and no-store. No Python exception patched.
-H0102–H0109/H0114/H0115 ten explicit audits proven from58 actual observations;
  declarations358, remaining1943 if composed. All58 Go external routes missing,
  zero new reference bugs. Legacy setup/topology and publishing early exits
  remain pending; no convenient unreachable classification was added.

.64 pilot6 requests: Ran8 tests0.796s/OK; missing6/complete=false.
Full preparation command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix split-onboarding- --output /tmp/routerdiff-split-onboarding-all.json'`
=>31 requests, cross-domain missing31/references7/audits0 for that run's earlier
loaded declarations/complete=false. All exact Python prerequisites passed.
State/self-check command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix split-state- --output /tmp/routerdiff-split-state-all.json'`
=> Ran46 tests3.020s/OK;27 state requests, cross-domain missing27/references5/
audits4/remaining2297 in this subset/complete=false. Same foreground shell then
read the two synthetic JSON observation reports and evaluated explicit audit
requirements: real_http_recipes58; all ten listed helpers variants-audited.

No active command. Last FULL composition is still37937739,3054 recipes/301 audits;
new increments not composed, main findings full header unchanged. Final gate and
final rebase pending. Next priority remaining wrapper/helper coverage, scoped
dynamic identities/times/generated content, device failure/compensation and
background/crash paths. Final gate private snapshot scope was already authorized.

Generated router-ID comparison increment:
- RouterIDs is now used by the actual execute comparator for successful R008
  POSTs only. Establishes a per-profile historical injective pair only after
  each side returns a nonempty URL-safe path segment and appends exactly one
  stored router with that ID, preserving all previous routers. Go ID format
  need not copy Python's router_12hex format; existing Python prerequisite keeps
  checking its actual format. Own-side DELETE references remain raw/unchanged.
- Projection touches only the explicit R007 routers[].id / R008,R010 router.id
  response slots and the selected owner's stored routers.items[].id slots.
  Unknown fields, names, nested last_probe IDs, messages, revision/fingerprint/
  token, device IDs/scripts and all receipt/file paths stay strict. No regex
  substitution in strings. Raw result responses and side-local Ref values are
  kept; projection happens on deep copies for comparison only.
- Negative controls prove a different native ID with correct persistence can
  compare, while wrong stored ID, missing/extra/modified records, unsafe/reused
  identities and altered names fail the real execute comparison. Cross-profile
  values cannot borrow mappings. Other dynamic families (times/editor revisions/
  generated configs/device identities) remain pending; no blanket normalizer.
- Initial comparator self-check fixture omitted the sections table required by
  the existing profile_write invariant; corrected that fixture, not the invariant.
  No product request ran after that failed self-check. Then ran the command below.

`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY=/tmp/routerdiff-build-302rx1dg-latest/configflow; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_identities test_compare test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world router-creations --output /tmp/routerdiff-create-identities-regression.json'`
=> Ran32 tests0.749s/OK;80 real Python create/delete HTTP recipes6.15s,
domain missing80/references4/audits0 in this subset/complete=false. No new bug.
This is comparator negative-control proof plus Python prerequisite regression;
native routes remain unregistered, so no actual Go creation parity is claimed.

No active command. Declarations358/remaining1943 unchanged. Last full composition
37937739 remains3054/301, not replaced by subsets. Final composition/gate/rebase
still pending. Next work continues remaining helper/wrapper/device/background
coverage and other scoped dynamic comparisons; task remains incomplete.

Integration refresh before forwarding increment:
- Clean git rebase go succeeded43 commits, onto15173872f. Prior router ID commit
  is nowfe7347d19, wizard/state2bec6538d, split guardsf9e76cef7. Historical IDs in
  older command evidence remain source provenance, not current commit IDs.
- Read updated AGENT-BRIEF in full: gate private snapshot allowance is explicit;
  final slot queue limit15 minutes, then targeted hand-back/coordinator cf-gate.
  Temp/build/report files now use home disk, not large /tmp allocations.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1; uptime; mkdir -p "$HOME/.cache/routerdiff-reports/302rx1dg"; ~/cf-test/.venv/bin/python server/tools/routerdiff/build.py "$HOME/.cache/routerdiff-build-302rx1dg-current" --parallel 4; export ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest discover -v; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world router-creations --world runtime-reads --world split-valid-empty --output "$HOME/.cache/routerdiff-reports/302rx1dg/rebase-foundation.json"'`
=> uptime load15.56/27.52/29.02; public build succeeds; Ran74 tests24.148s/OK;
92 HTTP recipes, domain missing91/cross-domain missing1/routes0/complete=false.

Forwarding real preconditions and transport increment:
-56 requests across32 independent registry/version/layout worlds: absent Agent,
  OxiDNS wrong service, Docker, genuinely offline,17 malformed version classes,
  old major/minor/patch, exact1.8.40 AIO and1.8.41 standalone boundaries,
  suffixless/leading-zero/new-major/new-minor controls. Fresh online status comes
  only from actual authenticated public heartbeat, never a mocked clock/status.
  Rejected preconditions make zero Agent requests; layout guards read once but
  do not POST; modern versions short-circuit the old-layout read.
-109 response/error/filter requests, including invalid UTF-8,301/302/303/307/308
  HTML redirects with explicit zero redirected requests, absent/nontext/falsy
  error text and priority/Unicode1000 cap, all JSON root types,524288/524289-byte
  GET/POST boundaries, both truly-written/not-written overlarge replies. The
  byte-limit cases confirm terminal state by own public GET and one POST only.
  Four valid forward_filter states,11 dropped type/state variants, typed/capped
  safe display, and all6 existing unhashable bug variants retain exact evidence.
-4 actual uncertainty/timeout cases: before/after real write with lost TCP reply,
  real45-second status timeout and real45-second forwarding timeout.90.37s, no
  simulated Python timeout. Devices, storage, complete transcript remain compared.
-24 explicit audits: H0001/H0004/H0005–H0022 and R025-F10/F11/R026-F12/F13.
  H0002/H0003 exception catches remain pending reachability analysis, not closed
  just because a malformed version was refused before them. GET prechecks are
  not falsely labelled as reaching persist's H0018–H0022 lines.
  Declarations382/remaining1919 if composed. All169 Go routes unregistered.
  No new reference bug class; two extra dict variants of the known sanitizer
  crash added to findings separately, not folded into the last full header.

Pilot16 requests: Ran9 tests0.446s/OK, missing16. First response100 requests:
Ran9 tests0.432s/OK, missing100, existing unhashable class reproduced. Then
extended controls and ran the complete increment:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner test_identities; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix forwarding- --output "$HOME/.cache/routerdiff-reports/302rx1dg/forwarding-all-audits.json"'`
=> Ran52 tests4.824s/OK;169 requests, routes0/domain missing169/references28/
audits24/remaining2277 in this subset/complete=false; known bug class reproduced.

No active command. Last FULL composition remains37937739,3054 recipes/301 audits;
current source increments have NOT been composed. Main findings statistics stay
at that full run. Final gate and final rebase remain pending. Next gaps include
scoped other dynamic families, helpers/wrappers, device compensation/background/
OS failures, and reviewing unnecessary future/sibling route dependencies in
recipe eligibility (creation requires future DELETE; job-lock world requires
every sibling endpoint). Those must not hide an already implemented handler.

Eligibility false-skip fixes (test infrastructure only):
- R008 creation no longer requires future R009 cleanup. Every world owns a
  disposable DATA_DIR, so a missing DELETE cannot justify skipping implemented
  POST. Deletion still requires the actual prior creation and own raw ID.
- manifest_requires now uses the same concrete/template matcher as the actual
  HTTP prerequisite oracle. A registered generic service handler satisfies the
  concrete Mihomo prerequisite; wrong method, a Surge-only path and no handler
  remain missing. Existing token-overlap missing-handler check stays strict.
- Purpose-broken negative controls cover absence/wrong method/wrong concrete
  endpoint; real execute uses only R008 registration for an ID-bound successful
  creation. This fixes false unimplemented counts, not a product behaviour.

`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_runner test_identities; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world router-creations --output "$HOME/.cache/routerdiff-reports/302rx1dg/create-independent-eligibility.json"'`
=> Ran39 tests0.740s/OK;80 real create/delete recipes3.80s, missing80/complete=false.
No active command. Declarations382/remaining1919 unchanged; full composition/
final gate/final rebase pending. The shared job-lock world's sibling dependencies
are still a separate open test-infrastructure issue, not silently accepted.

Asynchronous gate prerequisite observation:
- New worker await_gate observes the actual pending HTTP future and actual fake
  event together. A real early HTTP completion is retained with status/headers/
  body; a harness-client transport error or bounded gate deadline retains its
  category/process exit without inventing HTTP500. Unknown/unregistered gates
  remain fixture errors. No app state, clocks or Python helpers are patched.
- execute records both prerequisite observations. Python must reach the device;
  Go early completion/transport/deadline blocks main/dependent requests and is a
  compared difference, never an unexplained fake wait error or parity pass.
  Findings now include the prior blocked-reference reason. Raw bearer headers
  are not included in async prerequisite metadata.
- Negative controls exercise early200/503, transport exit7 and live deadline in
  execute. Real public auth-status HTTP on BOTH servers proves early completion
  carries its real200; the real gated RouterOS probe proves the ready path.
  Initial negative test forgot to register its deliberately unentered gate;
  added a real unused fake fault, keeping the worker's unknown-gate check strict.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_runner test_boundary'`
=> Ran36 tests22.630s/OK after the negative fixture correction. No product domain
comparison or new finding claimed by these harness checks. No active command.
Declarations382/remaining1919 unchanged; next split job-lock dependency worlds.

Independent lock-contention worlds:
- Replaced one shared18-request lock world with15 isolated target/vendor worlds,
  30 requests. Each starts its own real R017 probe, awaits the actual device gate,
  compares one malformed-body409, then releases/joins the SAME probe and checks
  exactly one profile revision/last_probe write. Explicit before-release snapshot
  keeps the write phase unambiguous. RouterOS success/iKuai intentional probe
  refusal remain their real200/502 results.
- All seven actual _manual_sync_lock/_router_settings_lock callers, both vendors,
  plus alpha/beta sharing one physical connection. Busy request only depends on
  itself and the automatic public begin-probe prerequisite. Joining depends on
  that target, so no missing sibling handler hides an implemented endpoint.
  Removed the unrelated R019 login prerequisite. No device/Go implementation read.
- H1278 declaration requires all15 busy results AND all15 real probe completions,
  not just one409. Original shorthand R012/R018 scope does not make those readonly
  handlers call this decorator. OS flock/scheduler/crash behaviour still separate.
  Declarations383, remaining1918 if composed. No new bug.

.64 pilot6 requests: Ran38 tests0.842s/OK, missing6.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix job-lock- --output "$HOME/.cache/routerdiff-reports/302rx1dg/job-lock-independent-all.json"'`
=> Ran54 tests2.299s/OK;30 requests, domain missing30/routes0/references3/
audits1/remaining2300 in this subset/complete=false. No active command.
Last full composition still37937739,3054/301, unchanged main findings header.
Current full assembly, final gate and final rebase remain pending; do not hand
this incomplete matrix back as completed Step B acceptance.

Client wrapper / Agent pin increment:
- H1256 actual scope is R002 GET and R004 GET/HEAD; R002 HEAD and all PUT/fixed-IP
  writes do not pin by Agent. Added the distinguishing R004 HEAD missing-Agent
  404 alongside R002 HEAD200. Two real PUTs with foreign beta/missing Agent query
  still write alpha notes only, retain DNS/bindings/router fields and all prior
  notes, foreign profile unchanged. Original alpha/beta read pins remain real.
- Exact shape guard:8 parse/root failures, all7 missing keys, extra key and5
  wrong base_url types. Profile mismatch11 variants include every JSON type and
  case/whitespace, all409. Stale name/mode are actual DeviceChanged responses;
  fresh row write is a control. All127 reviewed name/mode/network error variants
  are explicit endpoint requirements, not just one400 or a harvested ID list.
-13 new audits: H1256–H1260, R003-F10–F14, R002-S03/R004-S04/R003-B06.
  Declarations396/remaining1905 if composed. H1261/H1262 broad settings-error
  translation still pending a complete transitive-error family check, including
  actual growth beyond note/policy capacity and malformed stored-IP contexts.
  No convenient parent closure from a single child failure. No new Python bug.

`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world guards --world client-drafts --world agent-scope --world client-agent-query-writes --output "$HOME/.cache/routerdiff-reports/302rx1dg/client-wrapper-audits.json"'`
=> Ran55 tests4.574s/OK;1700 real HTTP requests, routes0/domain missing1700/
references372/audits310/remaining1991 in this subset/complete=false. This also
revalidates the existing large guards world after new profile/body metadata.

No active command. Last full composition remains37937739,3054 recipes/301 audits;
current increments are not assembled. Final gate/final rebase pending. Next
continue stored/capacity failure contexts, remaining public route/helper/device/
background coverage and other scoped dynamic values. Step B remains incomplete.

Client settings-error transitive contexts / atomic capacity:
-32 new stored-IP worlds ×3 actual callers=96 requests: missing member and all
  31 type/parse/prefix/mapped/multicast/unspecified families, through R002 read,
  R003 update and R014 workspace. These malformed rows pass real public import;
  the API errors are not a synthetic loading bypass or a mocked helper.
- Actual512→513 policy write refuses atomically, not just an imported513-row
  rejection. Notes2048→2049 tested both name-only and simultaneous mode/direct
  rewrite; the latter failure happens after policy preparation and must roll
  DNS and name changes back together. Complete DB/files/devices remain identical.
  H0080 now explicitly requires the real growth write, so omitting it loses proof.
- H1261/H1262 each require348 exact inputs/controls:127 request normalizer,
  65 stored policy shape/name/mode/duplicate,32 stored IP,120 native graph/
  settings/ownership,3 growth refusals and a fresh success. Scope is routers
  inventory errors; not all shared C22 normalizer internals or repository/SQL
  errors. Invalid stored notes have their already-proven public loading barrier
  and are not forged into an HTTP fixture. Declarations398/remaining1903 if composed.

.64 pilot19 requests: Ran11 tests0.379s/OK, missing19, no new bug.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world guards --world client-drafts --world notes-capacity --world-prefix stored-policy- --world-prefix native-policy- --world-prefix native-owned- --world-prefix native-invalid- --output "$HOME/.cache/routerdiff-reports/302rx1dg/client-settings-error-parents.json"'`
=> Ran56 tests3.266s/OK;2470 real HTTP requests, routes0/domain missing2470/
references384/audits327/remaining1974 in this subset/complete=false. No new bug.
No active command. Last full composition3054/301 remains unchanged. Final
composition/gate/rebase pending; still1903 matrix entries without complete audits.

Fresh HTTP clock comparison increment:
- Case.times is an explicit response-member allowlist, not a key/regex mask.
  Each Go value must be an ISO date+time inside that side's actual HTTP send/
  response window (one-second source precision allowance), then only that member
  is paired on deep comparison copies. Python uses the same stricter shape/
  response-end window. Polling after the response does not extend its clock bound.
- Raw report bodies and own-side Ref observations remain untouched. Wrong type,
  missing/malformed/stale/future Go values produce clock differences rather than
  fixture aborts. A bad Python prerequisite still aborts. Adjacent array members,
  escaped-key siblings, unlisted ISO-like text,64hex revision/fingerprint and
  opaque tokens remain strict. Real execute negative controls exercise the path.
- This deliberately does not normalize cached clocks, persisted last_probe/
  receipt/task timestamps or general dynamic strings. Those need their own
  content/provenance proof; no claim of complete dynamic comparison yet.

`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_temporal test_compare test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world runtime-reads --world unlinked-inspection --world discovery --output "$HOME/.cache/routerdiff-reports/302rx1dg/fresh-clock-regression.json"'`
=> Ran34 tests1.492s/OK;25 real Python HTTP prerequisites, domain missing25/
references28/audits0/complete=false. Then added escaped array-pointer control and
strengthened the64hex/token negative values:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_temporal test_runner'`
=> Ran31 tests0.688s/OK. No actual Go domain handler comparison or new bug claimed.
No active command; declarations398/remaining1903 unchanged. Full composition,
final gate/rebase and all remaining matrix/dynamic work still pending.

Fixed-IP outer shape / profile guards:
- H0036 exact object/operation/list guard covers every parse/root class, all
  missing/extra members, all operation types, empty/65 lists and NONEMPTY wrong
  string/object device collections. H0037 covers wrong row types, empty/extra
  rows and all four missing fields. Actual one-device plus64-device add/delete
  previews are the successful schema/cap controls; their real signed payloads
  and device effects are not mocked.
- Added exact two-member apply shape (missing profile/token and extra) plus11
  preview and11 apply profile mismatches: all JSON types, case and whitespace.
  They stop before device I/O/cache invalidation, status/message exact and all
  state unchanged. Root messages now prove the intended guard instead of only400.
  Device/row variant labels became stable readable names; refreshed guard results
  are used, not stale labels from the first report.
-12 audits: H0036/H0037/H1263/H1270/H1271, R005-F10/F11,
  R006-F10/F11/F12/F13/F16. Declarations410/remaining1891 if composed. Remaining
  MAC/IP/name, source checks, setup, tokens and all compensation parents are NOT
  implied by these outer checks. No new bug; all native domain routes still absent.

.64 initial real positive/control command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world guards --world fixed-ip --world fixed-ip-64 --output "$HOME/.cache/routerdiff-reports/302rx1dg/fixed-wrapper-pilot.json"'`
=> Ran11 tests0.422s/OK;1715 requests, including real64-device add/delete52.42s;
routes0/missing1715/references374/audits301 for earlier loaded declarations.
After stable label/declaration changes, reran changed guards (positive/control
recipes were unchanged, so did not repeat52-second batch):
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world guards --output "$HOME/.cache/routerdiff-reports/302rx1dg/fixed-wrapper-guards.json"'`
=> Ran58 tests5.241s/OK;1698 requests, routes0/missing1698/references356/
audits311/remaining1990 in this subset/complete=false. The same foreground shell
then evaluated ONLY the unchanged fixed-ip/fixed-ip-64 positive records plus the
new guards file:1715 actual observations, all12 explicit audit sets variants-audited.

No active command. Last full composition3054/301 still not replaced. Full current
composition/final gate/rebase pending; native parity and1900-ish remaining source
paths are not complete. Next work: deeper fixed-IP fields/setup and reachability,
remaining device/background/OS paths and content-bound dynamic comparison.


Fixed-IP IP/MAC/name normalization increment:
- Added97 rejected plus12 positive actual HTTP previews. Positive cases include
  AF hex letters in lower/mixed case (a digits-only MAC cannot prove uppercasing),
  short octets, mixed separators, outer whitespace, empty/blank/80-character
  names and a same-MAC/different-vendor control with actual sightings on both.
  Rejects cover every JSON type, malformed IPv4/MAC, normalized duplicates,
  all C0/DEL and81 ASCII/Unicode/space names. Preview is DB/device read-only.
- Nine named audits: H0038/H0039/H0592/H0593, R006-F14/F15/B01/B02/B03.
  Current419 declarations/1882 pending if composed; no native parity claim.
  Ownership-marker aliasing remains pending and is documented in the audit file.
- Harness construction mistakes were caught by self-tests: an unclosed tuple,
  then a missing inner generator loop/count assertion. Fixed before reference
  rerun; they are test authoring failures, not product findings.

.64 commands:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world guards --world fixed-ip-normalization --output "$HOME/.cache/routerdiff-reports/302rx1dg/fixed-normalization-audits.json"'`
=> Ran32 tests4.311s/OK;1805 recipes, allGo-unimplemented, references361,
320 audits/1981 pending in this subset, no Python bug, complete=false.
Then strengthened the success set with actual AF hex case variants and reran only
that changed world, keeping the unchanged guard observations:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world fixed-ip-normalization --output "$HOME/.cache/routerdiff-reports/302rx1dg/fixed-normalization-letters.json"'`
=> Ran32 tests13.250s/OK;109 recipes,97 refusals/12 successes, allGo-unimplemented,
7 audits in this single-world subset (two require the unchanged guard control).
A foreground `rt --no-sync` Python evaluation joined only the unchanged guards
from the first report with the109 current observations: observed_recipes1807,
fixed_normalization_audits9, verdict=variants-audited, native_compared0.
Last FULL report3054/301 remains unchanged; final composition/gate/rebase pending.

Cached clocks: added explicit same-side freshness provenance instead of a broad
clock mask. ResponseClocks retains each successful fresh source's raw clock and
route/profile; cached entries must equal their own source exactly. Native bad
fresh clocks never become valid just because later cache replies repeat them.
Raw reports/Refs remain untouched. Python cache provenance is also checked when
the native route is absent. Only named response members are projected; persisted
clocks, hashes/revisions/tokens, ISO-like adjacent text stay strict/pending.
- Comparator helper and actual execute negative controls cover changed/missing/
  wrong type, source not declared, wrong route/profile, changed text/precision,
  invalid original clock and unimplemented-native fixture refusal.
- Applied to known discovery and upstream cache recipes, including actual
  129-profile LRU, own61-second age, failed refresh and rename/no-op preservation.
- A first iteration command used nonexistent world names; runner rejected them
  before starting product processes, as designed. Corrected names, no product bug.

.64 actual boundary command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world discovery --world discovery-signature --world discovery-lru --world discovery-failures --world discovery-staleness --world upstream-linked --output "$HOME/.cache/routerdiff-reports/302rx1dg/cache-clock-binding.json"'`
=>173 recipes/allGo-unimplemented; LRU134 requests62.47s and real-age sequence
63.42s;30 referenced entries; no Python bug; complete=false. No new audit
completion inferred from this subset. Current419 declarations remain unchanged.
Focused self-check command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_temporal test_runner test_recipes'`
=> Ran46 tests2.215s/OK. No command pending. Full composition, remaining1882
source audits, dynamic ownership/token/hash/YAML binding, final gate/rebase still
pending; this increment is not acceptance hand-back.


Fixed-IP setup guard increment:
- Added real0600 session JSON/0700 directory fixtures under canonical
  setup_<20hex> profiles (public repository import). Actual HTTP POST /api/setup
  and POST /api/setup/<id>/abandon prove the base shape/file modes and the
  retained-router abandoned state. All seeded active/abandoned/completed records
  pass the actual GET /api/setup/<id> without any DB/file rewrite. The completed
  fixture exercises an archived stored flag, not successful device installation
  or completion; that config-domain workflow is not claimed here.
- Active selected session blocks preview409 before device I/O; completed,
  abandoned and foreign active sessions permit device-backed preview200.
  A separate workflow previews while abandoned, uses public PATCH to reopen,
  then presents its own valid token and receives apply409 without device I/O.
  No monkeypatch/profile_sessions fake and no manually constructed signed token.
- Generic HTTP prerequisites now accept explicit nonempty schema checks and
  absent members, retaining the success=true default for existing APIs. The
  setup API has no success member: it is not invented. Negative controls prove
  wrong ID/type/present-abandoned and actual bad native prerequisites block main
  mutations and remain differences; all actual response members remain compared.
- Fixture file paths are resolved before excluding SQLite: ./ and nested/../
  aliases cannot overwrite the test DB. File/directory permission options are
  restricted to explicitly seeded owned paths. Six negative controls prove
  DB integrity, outside bytes/mode and parent-directory modes stay untouched.
- Three audits H0035/R006-F17/R005-F14:422 declarations/1879 pending if composed.
  No new reference bug. Current full3054/301 report still not replaced.

.64 command proving public create/read/abandon and fixtures:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_runner test_recipes test_seed test_boundary.BoundaryTests.test_setup_file_fixture_is_accepted_by_real_public_reader_and_matches_creation_contract; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix fixed-setup- --output "$HOME/.cache/routerdiff-reports/302rx1dg/fixed-setup.json"'`
=> Ran48 tests22.951s/OK;6 recipes allGo-unimplemented, no bug.
After adding declarations and path/mode safety controls:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_runner test_recipes test_seed; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix fixed-setup- --output "$HOME/.cache/routerdiff-reports/302rx1dg/fixed-setup-audits.json"'`
=> Ran69 tests23.436s/OK;6 recipes, routes0/missing6,3 references/3 audits,
2298 pending in this subset, complete=false. No command pending. Full composition,
remaining source/device/background checks, dynamic binding, gate/rebase pending.


Fixed-IP source/binding guards increment:
-26 no-I/O409 variants: both add/delete against missing/case/space router IDs,
  missing type and every wrong legacy stored type (including nonempty array/map).
  These dictionaries genuinely pass public repository import; no forged driver.
- Both vendors: unseen MAC, unseen IP, empty sighting tables, seen positive
  controls; wrong-vendor source and changed-source-at-apply from existing real
  workflows. Exact source-specific409; readonly fake tables and full DB/files.
- Each vendor refuses a stored binding for a different router or MAC before
  any I/O. Foreign device reservations with no local binding remain untouched.
- Recovered ownership: no local binding, but an actual static device row has a
  cf-client marker. Each vendor previews and confirms deletion using its own
  signed token, retains foreign reservations and all saved names/DNS/router,
  and leaves local binding empty. Covers R005-S07 without adding any reservation.
- Corrected the cross-profile stale-token recipe's Case.profile metadata to beta
  (its request/body already selected beta); matters for future scoped bindings.
- Eight audits H0040/H0042/H0043, R006-F18/F19/F22/S04 and R005-S07.
  Current430 declarations/1871 pending if composed. No new reference bug.

.64 final command for this increment:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix fixed-source- --world fixed-ip-stale --world fixed-ip-guards --world fixed-ip --output "$HOME/.cache/routerdiff-reports/302rx1dg/fixed-sources-audits.json"'`
=> Ran66 tests10.461s/OK;116 actual recipes, routes0/missing116,35 references,
8 audits/2293 pending in this subset, no Python bug, complete=false. Last FULL
composition3054/301 unchanged; no command pending. Remaining branch/device/
background work, scoped dynamics, final composition, gate/rebase pending.


Explicit source reachability ledger (separate from executed coverage):
- H0034/H0041/H0061 cannot be triggered through the current real Python public
  graph. _router rejects non-routeros/ikuai before _open; discovery filters the
  same two types before _read; fixed DRIVERS constructs only the two concrete
  classes with supports_write=True and vendor constants. All three adapter
  callsites are type-guarded or use that constant vendor. No production property
  or dispatch registration changes this. Device account permissions raise at
  HTTP and do not change this class flag. Unit monkeypatches are outside Step B.
- reachability.py records full source/call-chain proof plus required real public
  controls:26 legacy/missing source refusals, both real seen-device positives,
  real dual-vendor discovery and actual enrichment failure. Source control
  requests do NOT gain the unreachable H execution tag.
- evaluate emits source-unreachable only after all named controls have correct
  real Python outcomes, otherwise source-proof-controls-pending. Every proof
  has execution_claim=false. Reports separately count audited_entries (executed)
  and source_unreachable_entries; a proof is never a test pass. Test oracle
  counterexamples prove missing/bad controls retain pending, and proofs cannot
  inflate execution counts/referenced entries or imply native comparison.
-430 executed-variant declarations plus3 source proofs;1868 still pending if
  current increments are composed. Original R001-F08/F09 O6-superseded items and
  other defensive branches remain uncompleted until separately proved.

.64 exact command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_reachability test_audits test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix fixed-source-router- --world fixed-source-missing-router --world fixed-source-unseen --world discovery --world discovery-failures --output "$HOME/.cache/routerdiff-reports/302rx1dg/source-reachability.json"'`
=> Ran54 tests8.360s/OK;48 real recipes/allGo-unimplemented,20 references,
2 executed audits,3 source-unreachable proofs,2296 pending in this subset,
complete=false. No new reference bug. Full report3054/301 still historical;
final full composition/gate/rebase and remaining matrix work pending.


O6 supersession ledger:
- The approved matrix itself strikes out original R001-F08/F09 (global route
  cannot use header/profile_id/default P1/P2). Their replacements F14/F13 remain
  executed404/400 audits, while original IDs are review-superseded, not passed.
- Added invalid as well as missing profile header/profile_id controls; both
  ignored by global R001. With explicit ?profile= the matching invalid/missing
  space still returns400/404. A genuinely linked alpha/beta result with invalid
  header prevents an empty-list-only fixture from faking the scope proof.
- Each supersession needs all named actual controls, otherwise
  review-proof-controls-pending. execution_claim=false; separate
  review_superseded_entries counter. No matrix row/denominator removed; no
  native response masked or no-op500 treated as success. R001-F91 now requires
  all four ignored-selector examples plus the linked control, not just three.
-430 executed declarations +3 source proofs +2 explicit O6 supersessions;
  1866 pending if composed. This subset does not replace the last full report.

.64 exact command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-current/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_reachability test_audits test_runner test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world guards --world runtime-reads --world upstream-linked --output "$HOME/.cache/routerdiff-reports/302rx1dg/review-supersession.json"'`
=> Ran68 tests12.623s/OK;1722 actual recipes/allGo-unimplemented,374 references,
312 executed audits/0 source exclusions/2 O6 supersessions/1987 pending in this
subset; no Python bug, complete=false. No command pending. Full composition,
remaining device/background/source/dynamic work, final gate/rebase still pending.


Intermediate integration refresh (57 commits rebased cleanly onto goa08c4cc27):
- No changes to backend Python or AGENT-BRIEF/WORKFLOW/PORTING/rootAGENTS versus
  earlier base15173872f, checked with git diff. Existing Python source proofs and
  fixture contracts remain based on unchanged reference code. No Go business
  implementation read during integration.
- New ordinary public binary (same observation-only manifest overlay):
  $HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow on .64.
- Working source f6a069d95 before the subsequent local version-proof increment.
  Build/self-check/foundation command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1; uptime; ~/cf-test/.venv/bin/python server/tools/routerdiff/build.py "$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27" --parallel 4; export ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest discover -v; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world router-creations --world runtime-reads --world split-valid-empty --output "$HOME/.cache/routerdiff-reports/302rx1dg/rebase-a08c4cc27-foundation.json"'`
=> load16.35/18.97/20.47; build success; Ran103 tests66.722s/OK;94 real
recipes,93 domain missing +1 cross-domain missing, actual domain routes0,
16 references/0 audits/complete=false. No reference bug. This is an intermediate
refresh, not final gate/hand-back. Old commit IDs above remain source provenance.


H0002/H0003 outer version catch source proof:
- Read _at_least and all three constant-minimum callers plus the full real
  compare_versions function. Regex/type guard leaves only built-in strings with
  exactly three ASCII numeric parts and optional-go. String operations cannot
  throw TypeError/AttributeError; integer ValueError (including digit limits) is
  caught INSIDE compare_versions, which always returns an int. Current public
  input cannot reach the outer ValueError/TypeError/AttributeError catch/False.
- Added a literal5000-zero major .0.0-go refusal through GET/POST, actual empty
  public heartbeat preserving the stored version. No helper/exception/output
  simulation; no claim of observing an inner exception through HTTP. The source
  proof requires48 real named controls: all malformed types/forms, older parts,
  exact minimums, suffixless/leading-zero/new versions and the oversized input.
- Source exclusions now5, kept separate from430 executed-variant declarations
  and2 O6 corrections;1864 remain pending if composed. These are author proofs
  for Step B's independent review, not Go module verified statuses.

.64 on new goa08c4cc27 binary:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_reachability test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix forwarding-version- --output "$HOME/.cache/routerdiff-reports/302rx1dg/version-outer-catch-reachability.json"'`
=> Ran38 tests10.972s/OK;48 actual recipes/allGo-unimplemented,5 references,
2 executed audits/2 source exclusions/0 O6 corrections/2297 pending in this
subset, no Python bug, complete=false. No command pending. Full report still
historical3054/301. Next: scoped persisted probe clocks, other dynamic families,
remaining source/device/background matrix, final composition/gate/rebase.


Persisted probe clock binding + invalidation:
- ProbeClocks first validates an explicitly declared R017 probe/at against its
  own actual HTTP window, then requires exact equal last_probe.at on exactly the
  requested router/profile in real storage. Native missing/wrong/duplicate/
  stale storage produces clock-storage differences, never an alias. Python
  fixture validation remains active even while the native route is absent.
- Pairing affects comparison copies only: DB routers.items[].last_probe.at and
  known public_view slots R007/R008/R010 plus R014/R015 workspace.router.
  Alias only when raw time still equals the proven own source. No message,
  raw file, other timestamp, foreign profile, digest, revision, fingerprint or
  future Ref is changed. Latest valid probe replaces that router's prior clock
  binding. Invalid native evidence cannot establish a binding.
- Fully read Python policy_workspace.py: its public_view reuse needs these same
  narrow slots. Generated RouterIDs mapping now also covers workspace.router.id
  at R014/R015, leaving source_agents IDs and arbitrary fields untouched.
- New real10-step roundtrip: fresh workspace→ROS probe→list/workspace→name
  edit→stale editor409→fresh editor save→iKuai probe→both stored public probes.
  A probe preserves workspace revision; a name edit invalidates it. Publication
  invalidation remains pending, so R015-B04 is NOT called complete.
-17 independent fresh-probe/edit worlds (34 requests) cover all five connection
  members clearing last_probe, plus name/same type/normalized same username/
  endpoint/TLS/noop and six ignored empty/non-string passwords retaining it.
  Clearing means absent stored member and explicit public null. All edits bump
  profile revision once, preserve unrelated sections and perform zero device I/O.
  R010-B03 is one new explicit audit;431 variants+5 proofs+2 supersessions,
  1863 still pending if composed. No new reference bug.

.64 reference probe regression (before adding readback/invalidation recipes):
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix probe- --output "$HOME/.cache/routerdiff-reports/302rx1dg/persisted-probe-clocks.json"'`
=>104 real requests across probe-errors/TLS/IPv6, both successful and failed
probe clocks validated against actual storage; allGo-unimplemented,32 references,
no bug, complete=false. Wrong initial --world probes was rejected before process
start; corrected to the actual --world-prefix probe-.
Final command for changed comparison and all invalidation controls:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_probe_clocks test_temporal test_identities test_runner test_recipes test_audits; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix probe-edit- --world probe-clock-roundtrip --output "$HOME/.cache/routerdiff-reports/302rx1dg/probe-clock-invalidation.json"'`
=> Ran80 tests12.902s/OK;44 real recipes/allGo-unimplemented,7 references,
1 executed audit/2300 pending in this subset, complete=false, no bug. Raw clock
mismatch and corrupted stored message are purpose-broken oracle controls, not
native findings. No command pending. Full historical3054/301 remains unchanged.
Next: main native request transport failure classification, remaining dynamic
ownership/token/hash/YAML/receipt clocks, device/background matrix; full
composition, final gate/rebase/clean hand-back still pending.

Main-request native transport observations:
- Worker RPC now returns actual exception class as structured metadata, separate
  from its prose/traceback. Request/join connection/read/protocol failures are
  retained with actual child running/exit state; other RPC/fixture exceptions
  remain fixture errors. No prose parsing and no manufactured HTTP status/body.
- Native failure returns different with go=null (no complete HTTP observation),
  actual go_process transport details, and blocks dependent world requests. It
  cannot be miscounted as Go-unimplemented or a matching500. Python transport
  precondition failures still abort as reference fixture errors.
- Immediate state capture/differences are retained separately and explicitly
  quiescence_proven=false. Capture failure stores its actual error class. Neither
  snapshot availability nor child exit alone claims successful rollback or
  completed jobs. Markdown findings include this limited observation.
- Keep the actual exit code after an explicit stop until the next start, so
  later observations do not lose real -9/other child evidence. Same value feeds
  existing async-gate outcomes. Purpose-broken own Go process test uses only the
  actually registered /api/auth/status boundary, never an invented routers route.
  Native is killed only inside its disposable namespace; Python remains ready.
- Unit controls cover refused/disconnected/timeout/malformed/incomplete response,
  a still-running child, unavailable state snapshot and misleading error prose;
  unknown classes/fixture RPC/Python failures are not converted into Go findings.
  This closes MAIN request/poll/join observation. Auxiliary setup/settling request
  transport failures still need the same distinction later (bad HTTP statuses
  and async device-gate outcomes already handled). No matrix audit count added.

.64 actual own-process fault/self-check command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_runner test_boundary'`
=> Ran43 tests67.265s/OK. Actual killed native produced ConnectionRefusedError,
exit_code=-9/running=false, no HTTP status/response; reference HTTP200 remained.
After strengthening live-child/snapshot-failure controls and findings rendering:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world runtime-reads --output "$HOME/.cache/routerdiff-reports/302rx1dg/transport-normal-regression.json"'`
=> Ran32 tests2.418s/OK;13 real normal recipes/allGo-unimplemented,9 references,
0 audits/exclusions/corrections in this subset, complete=false, no bug. All
purpose-broken observations above are oracle tests, not native product findings.
No active command. Counts431 executed declarations+5 source proofs+2 O6 entries,
1863 pending if composed. Current full report remains historical3054/301.
Remaining matrix/dynamics/aux transport, full composition, final gate/rebase pending.

Probe resource/version/capability branch increment:
- Read both drivers' construction/request/probe functions and R017 handler.
  Fatal system reads return502 and persist failed probes; optional capability
  errors return200/probe.ok=true, omitting ONLY the failed capability.
-3 ROS paths each:401/403/404/500, bad JSON, disconnect, two list-shape guards,
  actual10s timeout.4 iKuai capabilities each: permission,503, bad JSON,
  disconnect, two1008 replies/one re-login, actual10s timeout.1008 controls are
  response faults, not real-firmware session acceptance; no refused write applies.
- Added late null after valid dict on3 ROS lists, eight non-object resource roots,
  per-vendor JSON-kind version fields and seven min/current/new-major/rc/zeroes/
  Chinese version/model successes. Messages are literal reference preconditions
  verified by real Python; no generic Python formatter or helper substitution.
-12 audits H0835/H0841/H0842/H0843/H0241/H0242/H1296,
  R017-S01/S02/S03/F13/B04. H1296 requires110 fatal/healthy/TLS observations.
  Broad R017-F12, DB-save failure, unhandled nested schema/O12 crashes, real
  readonly/apply permissions and T15 remain open. No new product finding.
- Fixed elapsed_min only checking Python: both sides now enforce declared minimum
  on actual monotonic HTTP duration; too-fast native equal-body reply differs.
  Async begin keeps original monotonic start. No raw RTT equality or clock patch;
  oracle-only counterexample uses actual short waits. Upper bounds/in-budget
  slow peers and load-sensitive timing review remain later D/T work.

.64 long reference matrix:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_recipes test_probe_clocks; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world probe-errors --output "$HOME/.cache/routerdiff-reports/302rx1dg/probe-capability-failures.json"'`
=> Ran18 tests0.947s/OK;136 requests, ROS49.39s/iKuai66.96s including7 child
and2 fatal10s waits; allGo-unimplemented,34 references, no bug. Unchanged named
fatal/capability records reused below; no output/evidence rows fabricated.
Final changed controls/TLS/comparator:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_runner test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world probe-guard-controls --world probe-tls --output "$HOME/.cache/routerdiff-reports/302rx1dg/probe-guard-audits-controls.json"'`
=> Ran73 tests22.601s/OK;34 requests/allGo-unimplemented,17 references,
2 audits in this subset. Foreground rt --no-sync evaluation joined only the
unchanged136 and current34 observations against all12 named declarations:
observed_recipes170/probe_guard_audits12/verdict=variants-audited/native_compared0.
443 executed declarations+5 source proofs+2 O6 entries;1851 pending if composed.
Full report remains historical3054/301. No command pending. Remaining source/
device/background/dynamics/aux transport, full composition and gate/rebase pending.

Auxiliary HTTP transport and healthy public-process oracle increment:
- Actual registered GET /api/auth/status completed normal two-process comparison
  with zero HTTP/DB/file/fake differences. Formal boundary self-check added; it
  is an oracle control, never counted as a routers route or matrix audit.
- Preparatory http/assert_http/wait_guard and post-response settling reads now
  retain concrete native request transport errors and actual process state.
  Unknown classes, namespace faults and Python fixture errors still abort.
  A completed main response is retained when a later settling request fails;
  no incomplete HTTP response/status is fabricated. Dependent requests block.
- Immediate state evidence is explicitly not a fence for background work or a
  proof of successful compensation. Setup and main/post-response failures use
  the same bounded snapshot helper. No product implementation changed.

.64 command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_runner test_boundary'`
=> Ran46 tests40.555s/OK, including real healthy native HTTP and owned-process
kill negative control. Native product findings/routers compared counts unchanged.
443 executed declarations+5 source proofs+2 O6 entries;1851 pending if composed.
Full composition, remaining matrix, final gate/rebase still pending.

DHCP DNS evidence and abstract-driver reachability increment:
- New dhcp_guards.py drives real HTTP report/plan/apply from full synthetic
  RouterOS tables. Unknown evidence must remain visible as report200 unknown,
  plan200 exact blockers and confirmation409 without any device/storage write.
-22 new named audits H0849–H0863/H0904–H0910. Cases separate server/RADIUS,
  missing/duplicate ID, network parsing and dynamic flags, DNS suppression/list
  types, all9 names call positions, four option6 layers, incomplete/ambiguous
  references, every option-code type/range and actual5000-digit integer limit.
  Accepted defaults/lists/canonical networks/vendor-specific option43 and ignored
  out-of-scope/disabled/ordinary lease/matcher controls prevent blanket refusal.
- H0128–H0131 are four NEW source-unreachable proofs, separate from executions:
  production constructs only the fixed two concrete classes; both override
  probe/read_state/read_dhcp_dns/apply, with no super/base call or method/map
  mutation. Real both-vendor operation controls are required before proof status.
- First390 real HTTP recipes passed Python preconditions (361.74s). The initial
  attempted no-store header assertion was a fixture assumption, corrected after
  reading the actual report handler; no Cache-Control normalization was added.
- Integrity self-test initially caught its old443 declaration-count expectation;
  updated explicit count465. Current targeted run has44 self-tests passing
  (16.270s) and is executing the expanded DHCP vectors plus all four proofs'
  concrete controls. Final output/commit is recorded only after it completes.

Expanded final .64 command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes test_reachability; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world dhcp-evidence-guards --world probe-guard-controls --world release-ros --world release-ik-plain --world dhcp-ikuai-plain --output "$HOME/.cache/routerdiff-reports/302rx1dg/dhcp-evidence-audits.json"'`
=> Ran44 tests16.270s/OK; DHCP group441 requests429.91s; total484 actual
recipes/allGo-unimplemented,47 references,24 executed audits (22 new DHCP plus2
existing probe controls),4 new source proofs,2273 pending in this subset,
complete=false/no bug. All four new source proofs' both-vendor controls executed.
No command pending.465 declarations+9 source proofs+2 O6 corrections;1825 remain
if all increments/controls are composed. Historical full3054/301 still stands;
this subset does not replace it. Next: remaining route/driver/schema/dynamic/
background matrix, full composition, final gate/rebase/clean hand-back.

Nested iKuai schema / new reference finding:
- Read real iKuai _post/_login/call/probe and R017 again. code0/results object
  does not guarantee verinfo is an object. Nonempty bool/int/float/string/list
  reaches .get and raises actual AttributeError; no failed probe is saved.
-16 real HTTP requests:6 falsy controls return502 and persist failed probe,
 5 truthy malformed kinds return HTML500 with own-request source traceback,
 5 subsequent valid probes succeed. Crashes leave DB and device tables unchanged.
  New low bug IKUAI_PROBE_NESTED_SCHEMA_CRASH belongs R05/R15, recorded in main
  findings as a separate increment. Six reference defect classes are now known.
- Native safe structured refusal is O12 review, not an equality pass. Matching
  HTML failure, accepted response, device writes/mutation, other settings/foreign
  state/file mutation fail safety checks. A fix can persist only its own fresh
  failed probe with one profile revision/generation and new commit_id; stable
  metadata/configuration stay unchanged. All physical diffs remain visible.
- Oracle controls include broken response types, bool-vs-number, stale clock,
  extra/duplicate malformed router rows, foreign DB/config/files, device write
  intent, wrong store identity/commit/generation. No product helper patched.

.64 regression command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_probe_schemas test_runner test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world probe-nested-schema --output "$HOME/.cache/routerdiff-reports/302rx1dg/probe-nested-schema.json" --findings "$HOME/.cache/routerdiff-reports/302rx1dg/probe-nested-schema-findings.md"'`
=> Ran54 tests3.411s/OK;16 actual requests2.82s/allGo-unimplemented,4 references,
0 new audits/proofs, new named Python bug, complete=false. Initial purpose-broken
transcript lacked required raw/headers fixture members; fixed the oracle fixture.
After tightening the own failed-probe metadata scope, exact same rt command with
`python -m unittest -v test_probe_schemas`=>Ran3 tests0.225s/OK;16 actual requests
3.31s/allGo-unimplemented with same verified reference bug. No active command.
Counts465 declarations+9 source proofs+2 O6 entries/1825 remaining unchanged.
Final full composition/gate/rebase and the remaining matrix are still pending.

Content-bound store commit identities:
- Real POST /api/server-domain matched HTTP/DB except independently generated
  meta.commit_id. Added StoreHeads: only this exact meta member can be projected,
  after full candidate DB equality and independent raw store/generation/config
  provenance checks. Reused token, rotated known generation, changed store,
  backward/invalid generation and unchanged-generation config changes fail.
- Raw config provenance includes fields/sections/root_order and all physical
  revisions/positions. Projection uses separately proven RouterIDs/ProbeClocks,
  but history always retains each side's raw digest. No file/message/field UUID
  text or other metadata is rewritten. Real Pairs always enable reference checks;
  existing small purpose-built runner stubs opt into it only when testing heads.
- Read actual config_store.head/commit/_apply. Metadata-only transactions keep
  generation/commit and are valid; unrelated metadata remains strict. Legacy
  missing/null/empty commit IDs stay literal and never earn an alias; normal
  config write thereafter has a new opaque token. No Python UUID encoding rule
  is imposed on Go's token. No product implementation was read or changed.

.64 initial oracle command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_store_heads test_boundary'`
=> Ran19 tests101.894s/OK; real native writes/readbacks compare successfully.
Final corruption control and reference corpus:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a08c4cc27/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_store_heads test_boundary.BoundaryTests.test_real_native_config_write_pairs_only_content_bound_store_commit_ids; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world router-creations --world probe-clock-roundtrip --world probe-nested-schema --world fixed-ip --world workspace-drafts --world client-drafts --output "$HOME/.cache/routerdiff-reports/302rx1dg/store-head-binding.json"'`
=> Ran7 tests6.479s/OK; actual token-only tamper in stopped OWN native DB survives
restart/publicHTTP200 but is rejected by comparator.140 real domain recipes,
allGo-unimplemented,58 references,1 existing audit,complete=false; existing named
schema bug retained. No domain native parity claim. After adding legacy-null/
empty/missing controls, same rt command with only the unittest portion=>Ran8
 tests4.986s/OK. No command active, all tests/builds remained on .64.
Counts465 declarations+9 source proofs+2 O6 entries/1825 remaining unchanged.
Remaining: ownership nonces/comments/tags, opaque tokens/fingerprints/revisions,
YAML structure/retention, runtime receipt/setup clocks/read transcript order,
remaining route/device/background matrix, full composition/final gate/rebase.

Periodic integration refresh (not final hand-back):
- Clean git rebase go succeeded across65 commits onto a9d6ce86c. Python source,
  PORTING and root AGENTS unchanged; reread the brief/workflow changes: one agent
  gate/server (coordinator can add one), coordinator batch/risk-tier rules. Final
  work still follows the task's explicit gate instruction; no gate run yet.
- Current rebased commits:1fc410b93 store heads,84e850094 nested-schema bug,
 3ad1779e9 DHCP evidence,a2f227708 auxiliary HTTP. Older IDs above are provenance.
- Built normal public binary with observer at
  ~/.cache/routerdiff-build-302rx1dg-a9d6ce86c/configflow. Use this binary now;
  older a08c4cc27 paths in historical commands remain historical observations.

.64 refresh command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1; uptime; ~/cf-test/.venv/bin/python server/tools/routerdiff/build.py "$HOME/.cache/routerdiff-build-302rx1dg-a9d6ce86c" --parallel 4; export ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a9d6ce86c/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest discover -v; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world runtime-reads --world router-creations --world probe-clock-roundtrip --world probe-nested-schema --world fixed-ip --world workspace-drafts --world client-drafts --output "$HOME/.cache/routerdiff-reports/302rx1dg/rebase-a9d6ce86c-foundation.json"'`
=> Load15.90/19.85/23.38; build success; Ran129 tests119.753s/OK;153 actual
recipes/allGo-unimplemented,67 references,1 existing audit,complete=false;
known nested-schema reference bug reproduced. Actual domain manifest still0/26.
465 declarations+9 source proofs+2 O6 corrections/1825 pending if composed.
No command pending. Full composition and remaining matrix still open; no final
Go gate, no private snapshot read, no push. Continue remaining domain coverage.

Router record lifecycle increment:
- Read R009/R010 handlers, settings/job-lock wrapper, store.normalize and relevant
  profile middleware/repository contract.45 new real HTTP recipes across5 worlds:
  default alpha, profile beta, profile_id beta, header beta and query profile alpha
  winning over both beta selectors. Same IDs/endpoints have distinct profile names;
  the foreign ROS record has an active manifest and remains untouched.
- Both vendors: initial missing404, delete200, repeat404, ordered remaining row,
  empty target and intact foreign list. Deletion changes exactly one record and
  one profile revision, retains DNS/names/fixed bindings and performs zero device
  I/O, even while fake tables contain managed and foreign resources. Header world
  independently proves five simultaneous empty manifests cannot block deletion.
- All5 protected manifest fields: existing409 delete/400 endpoint/type refusals
  paired with empty-manifest successes. Added10 actual draft writes attempting
  object/list manifest replacement and forged id/probe/release/unknown metadata;
  original private data survives and forged fields stay absent. Existing null
  injection controls retained, giving15 R010-B01 inputs.
- Reused5 actual entered-device-gate busy worlds, now with explicit R009/R010
  route audits. Each must observe409 before malformed-body validation, then the
  own held probe must complete after explicit release. Includes same endpoint
  across profiles; corrected the busy beta Case.profile metadata. No sleeps/
  private locks. Waiting-router-lock/repository-expected race audits stay open.
-16 new audits: R009 S01/F10/F11/F13–F17/B01/B02/B03/B05, R010 F11/F14/S04/B01.
  These are named-reference coverage declarations, not native-route passes.

.64 final command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a9d6ce86c/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix router-delete- --world stored --world empty-manifests --world job-lock-routeros-R009 --world job-lock-ikuai-R009 --world job-lock-routeros-R010 --world job-lock-ikuai-R010 --world job-lock-routeros-shared-connection --output "$HOME/.cache/routerdiff-reports/302rx1dg/router-lifecycle-audits.json"'`
=> Ran44 tests15.229s/OK;135 actual recipes/allGo-unimplemented,40 references,
26 audits in this subset (16 new+10 prior guard audits),2275 pending/complete=false,
no new reference finding. Source exception cases already in stored remain
unchanged; their broader O12 handling/classification remains pending.
481 declarations+9 source proofs+2 O6 corrections/1809 pending if composed.
Historical full3054/301 stays unchanged. No command pending. Continue remaining
matrix/dynamic binding/background work before final composition/gate/rebase.

Client-device deterministic OxiDNS revision binding:
- Read common/client_devices.py, routes/client_devices.py, common/device_notes.py,
  common/oxidns_device_policy revision/read/apply and full policy_workspace.py.
  S-oxidns's approved native canonical hash must be treated as a content token.
- New PolicyRevisions narrowly covers R002 GET/R003 PUT oxidns_revision. Validate
  lowercase SHA256 shape and own stored oxidns dependency stability/collision
  freedom; alias only when complete dependency and other response members match.
  Never compute/emulate Python's or Go's returned hash. Raw observations/Refs,
  all config fields, unknown settings and response values remain intact.
- Names/bindings/router/probe/profile counters are excluded exactly as the source
  contract. Actual effective response profile handles query-Agent pinning. Own
  dependency keys preserve signed-zero/numeric distinctions. Purpose-broken
  native token/constant/reuse/cross-scope/content/body controls fail; native
  implementation-format differences alone pair. Bad Python contract aborts even
  with absent Go. Initial source check wrongly included HEAD; fixed to only
  GET/PUT because HEAD/OPTIONS contain no observable body token, with regression.

.64 final command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a9d6ce86c/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_policy_revisions test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world client-drafts --world client-mode-notes --world client-canonical-names --world stored-policy-legacy --world agent-scope --world fixed-ip --world fixed-ip-normalization --output "$HOME/.cache/routerdiff-reports/302rx1dg/client-policy-revisions.json"'`
=> Ran40 tests1.562s/OK;170 real recipes/allGo-unimplemented,63 references,
14 existing audits,complete=false/no new bug. Native encoding negatives are
oracle tests only; no native domain handler exists to claim product parity.
Counts481 declarations+9 source proofs+2 O6 entries/1809 pending unchanged.
No command pending. Next: extend content binding to all3 R014/R015 workspace
hash paths together (router excluding last_probe plus full oxidns; names token
from actual ordered public IP/name rows), then other dynamics/matrix/background.
Current client-only binder does NOT claim to close those workspace hashes.
Full composition, final gate/rebase and remaining matrix are still pending.

Workspace hashes joined to content-bound revision comparator:
- Extended PolicyRevisions to R014 GET/R015 PUT's3 explicit paths, validated and
  projected together. OxiDNS dependency/token is shared with client API; workspace
  depends on the entire own stored router except last_probe and full settings;
  names depends on actual ordered public policy IP/name rows. Never recompute
  either algorithm or replace arbitrary SHA-looking strings.
- Raw own dependency history is distinct from independently projected router/
  probe copies. Wrong owner/duplicate target/missing or malformed token/private
  setting mismatch cannot establish pairing. Probe changes leave workspace hash
  stable; note-only changes update names hash; router edits update workspace hash.
- New integrated execute oracle confirms optimistic PUT receives each side's own
  raw revision/names token, not the projection. An over-eager native new token
  for unchanged input is a difference. No native handler/product pass inferred.
- Overlapping writer linearization/storage provenance remains a separate open
  concern for future T02/T08. Current controlled workflows settle their writes;
  this does not assert every post-response snapshot is its request's exact state.

.64 actual reference command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a9d6ce86c/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_policy_revisions test_runner test_probe_clocks; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world workspace-drafts --world client-mode-notes --world probe-clock-roundtrip --world native-policy-valid --world native-owned-valid --world-prefix probe-edit- --output "$HOME/.cache/routerdiff-reports/302rx1dg/workspace-policy-revisions.json"'`
=> Ran46 tests2.189s/OK;69 actual recipes/allGo-unimplemented,34 references,
1 existing audit,complete=false/no bug. After adding integrated own-token PUT
oracle, same rt command with only unittest portion=>Ran47 tests3.041s/OK.
No active command.481 declarations+9 source proofs+2 O6 corrections/1809 pending
unchanged. Remaining signatures/fingerprints/YAML/ownership/runtime clocks and
matrix/background/concurrency, full composition and final gate/rebase still open.

Complete iKuai DHCP read-evidence increment:
- Read IKuaiDriver._dhcp_list/read_dhcp_dns, full firmware field contracts,
  decoder, shared matching/report and public inspection/plan/apply boundaries.
  New ikuai_dhcp_guards.py uses complete synthetic4.x forms and default fake
  netmask presentation; it does not relax the known static-route suffix bug.
-43 fields each absent and null;17 integer fields each bool; every JSON kind on
  representative int/string fields. Missing/unknown CSV ordering, exact enabled/
  runtime status/relay/physical restrictions, pool parser/disjuncts and both DNS
  slots have separate evidence. Explicit-vs-inherited DNS targets differ correctly.
- Static binding invalid-address types/text, every reachable missing/incorrect
  member, independent DNS slots and unknown fields are exercised. Disabled/
  outside/runtime-key controls remain usable. Read-only static id type is not
  validated by this reader, unlike the fixed-IP writer; its valid-reader control
  is explicit and makes no write/firmware-acceptance claim.
- Shared matching tests missing/duplicate enabled IDs, unknown subnets with full
  underlying diagnostics, disabled duplicates and distinct outside subnets.
  Both real drivers construct network strings: real iKuai empty-string network
  causes ValueError; no private DhcpDnsState or TypeError is manufactured.
-20 new declarations H0247–H0263/H0147–H0149, each requiring actual report200,
  plan200 and blocked confirmation409 plus independent positive controls.
  First609 real requests passed in512.83s; no new bug and native0/26. Added28
  boundary requests (end-address parsing, masks, singleton, both-DNS invalid,
  whitespace/leading-zero binding address); expanded637 run currently active.
- Targeted integrity checks passed46 tests28.981s before the expanded run.
  Record final command/tail and commit only after the active foreground run ends.

Final .64 command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a9d6ce86c/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -v test_audits test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world dhcp-ikuai-evidence --output "$HOME/.cache/routerdiff-reports/302rx1dg/ikuai-dhcp-evidence-audits.json"'`
=> Ran46 tests28.981s/OK;637 actual recipes596.74s/allGo-unimplemented,
20 references/20 new audits in this subset,2281 pending/complete=false/no bug.
All expanded vectors passed real Python; no record composition or invented
observation was used. This is a subset, not a replacement full-suite report.
501 declarations+9 source proofs+2 O6 corrections/1789 pending if composed.
No active command. Remaining write/readback/undo (H0264+), signatures/ownership/
fingerprints/YAML/runtime clocks, other matrix and background/concurrency,
full composition and final gate/rebase remain open. Next check fake transport's
avoidable packet delays before further large real-reference batches.

Owned fake HTTP transport adjustment:
- Handler now opts into socketserver's TCP_NODELAY flag. Small loopback JSON
  headers/body are sent promptly; explicit peer gates, real timeout waits and
  TLS verification remain unchanged. No product server flags or replies changed.
- .64 foreground chain completed its full harness unittest discovery before
  running the reference batch (set -e); the original verbose unittest tail was
  lost to tool-output truncation, so no exact test-count/timing is claimed here.
  Exact command:
  `RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a9d6ce86c/configflow"; uptime; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest discover -v; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world dhcp-ikuai-evidence --world probe-errors --world probe-tls --output "$HOME/.cache/routerdiff-reports/302rx1dg/peer-tcp-nodelay.json"'`
- Completed synthetic report:779 recipes,allGo-unimplemented,0/26 native routes,
  56 references,20 audits,2281 pending in this subset,complete=false,error=null,
  no Python bug observation. This includes all637 iKuai evidence vectors and
  142 real fatal/capability/TLS probe requests, including their timeout paths.
  Original process has exited; no duplicate batch was launched. No new matrix
  coverage or native parity is inferred from this transport-only rerun.

DHCP write/readback/compensation increment:
- New ikuai_dhcp_writes.py drives18 independent worlds through actual public
  previews/confirmations.43-field DHCP before-images and the unrelated static
  binding survive; owned route/NAT/group state is clean after compensation.
- Real peer gates pause edit/read requests, never product helpers. Readback zero/
  duplicate IDs, changed attributes vs independent runtime blocker, unavailable
  primary/recovery reads, lost applied acknowledgement, failed restore and a
  later stage-verification failure have separate actual outcomes. The later
  failure exercises the ordinary outer undo of a completed DHCP operation.
- An external DHCP domain edit while the fresh scope read is gated is retained;
  the final expected-attributes guard refuses before any DHCP edit. All four
  authorization/session codes and generic rejection detail/fallback forms are
  explicit;1008 makes two edit attempts. This is not firmware expiry acceptance.
- Async begin accepts a structured JSON document so each side's own fingerprint
  is resolved/encoded independently. An adversarial runner oracle confirms own
  token/body/header handling; conflicting raw+structured body is refused.
-10 new audits H0264/H0265/H0268/H0273–H0279 require every named failed branch
  AND an independent successful full edit. O6 default mask suffix stays unchanged;
  this write family explicitly uses the independent plain-mask variant.
- Initial28 and expanded34 actual reference requests passed. Full harness
  discovery then found ONLY its frozen declaration count501 vs510; corrected
  to511 after the added external-edit audit. It was a test inventory assertion,
  not a product difference. Targeted rerun passed83 tests15.197s/OK.

Exact .64 verification:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a9d6ce86c/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -q test_audits test_recipes test_runner; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix dhcp-ikuai-write- --world dhcp-ikuai-plain --output "$HOME/.cache/routerdiff-reports/302rx1dg/ikuai-dhcp-write-audits.json"'`
=> Ran83 tests15.197s/OK;39 actual recipes/allGo-unimplemented,0/26 native,
14 references/10 new audits,2291 pending in this subset,complete=false/no bug.
No active command.511 declarations+9 proofs+2 corrections/1779 pending if all
increments compose. This does not replace historical full composition or mark
any Go module verified. Remaining corrupt-operation reachability proofs require
complete producer analysis (do not assume legacy Agent host types are strings),
then signatures/ownership/fingerprints/YAML/runtime clocks, other matrix and
background/concurrency, full composition and final gate/rebase remain open.

DNS target/scope follow-on (in progress, not committed/hand-back yet):
- Initial112 single/HA address worlds completed304 real requests with no
  fixture error or Python bug,allGo-unimplemented. Stored wrong JSON types,
  malformed/IPv6/mapped addresses and exact IPv4 excluded classes are exercised
  independently for single DNS host, HA VIP, primary host and peer host. Eight
  boundary-adjacent valid controls per position remain pending without writes.
- Eleven legacy gateway forms completed33 report/plan/refusal requests. The
  report preserves the DHCP scope failure while plan/refusal independently use
  the earlier missing/invalid gateway diagnostic. Public repository import,
  ordinary server and real ipaddress are used; no helper or exception patch.
- Added five producer source proofs: malformed DHCP restore before-image and
  malformed/invalid update target cannot originate from current public plan/
  in-memory undo producers. They require actual named positive/negative public
  controls and remain separate execution_claim=false outcomes. H0267 is still
  open: do not casually assume legacy stored Agent host values are strings.
- Added later DHCP verification variants (scope cardinality/id/network,
  blockers, different DNS vs inherited slot), to be verified next with their
  complete outer compensation. No passing claim for these unrun additions yet.
- Prior committed state passed full harness discovery146 tests61.194s/OK.
  Target/proof integrity then passed52 tests23.074s/OK and its initial33 gateway
  requests. Current foreground .64 chain passed full discovery149 tests50.333s
  and is executing354 target/gateway/matching/positive/undo observations in one
  report (dns-target-audits.json). Follow to completion; no background hand-back.
- Latest local go now adds WORKFLOW Leaner verification: future Python cases
  cover normal paths and distinct error classes without multiplying every shape
  by every engine. Existing synthetic vectors remain valid; avoid duplicate full
  re-verification of unchanged areas. Coordinator handles batch gates per newer
  brief, while this task's explicit user final-gate instruction remains recorded.

DNS target/scope follow-on completed as an increment (overall Step B still open):
-354 actual observations completed: allGo-unimplemented,28 references,8 new
  executed audits plus5 source-unreachable proofs,2288 pending in that subset,
  no Python bugs/error. Exact .64 command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a9d6ce86c/configflow"; uptime; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest discover -q; routerdiff_log="$HOME/.cache/routerdiff-reports/302rx1dg/dns-target-audits.log"; if ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix dns-target- --world-prefix dns-gateway- --world dns-matching-failures --world dhcp-ikuai-plain --world dhcp-ikuai-write-late-verification-failure --output "$HOME/.cache/routerdiff-reports/302rx1dg/dns-target-audits.json" > "$routerdiff_log" 2>&1; then tail -n 2 "$routerdiff_log"; else tail -n 12 "$routerdiff_log"; exit 1; fi'`
=> load17.69/14.23/16.26;Ran149 tests50.333s/OK;354 cumulative recipes,
implemented_routes=0,audited_entries=8,source_unreachable_entries=5,
remaining_audits=2288,complete=false,python_bugs=[].
- Seven later-scope verification variants then joined the existing DHCP write
  family. All disjuncts of target cardinality/ID/network, a fresh blocker and both
  wrong-DNS/inherited-slot cases performed a verified full outer restore.53
  actual requests completed,13 execution audits (10 prior+3 new) plus1 source
  proof,20 references,2287 pending in this subset,no bug/error.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-a9d6ce86c/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -q test_recipes test_audits test_reachability; routerdiff_log="$HOME/.cache/routerdiff-reports/302rx1dg/dhcp-scope-audits.log"; if ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix dhcp-ikuai-write- --world dhcp-ikuai-plain --output "$HOME/.cache/routerdiff-reports/302rx1dg/dhcp-scope-audits.json" > "$routerdiff_log" 2>&1; then tail -n 2 "$routerdiff_log"; else tail -n 12 "$routerdiff_log"; exit 1; fi'`
=> Ran53 tests17.614s/OK;53 cumulative recipes,implemented_routes=0,
audited_entries=13,source_unreachable_entries=1,remaining_audits=2287,
complete=false,python_bugs=[]. No active command, no implementation changes.
-11 new execution declarations H0140–H0146/H0150–H0153,5 new source proofs
  H0266/H0269–H0272. Current522+14+2/1763 pending if all increments compose.
  These are independently executed focused reports; historical full composition
  is unchanged. No native routers parity/ported-module verification is claimed.
Next synchronize latest go and rebuild only the normal public observer binary;
then remaining matrix, dynamic ownership/runtime/YAML comparisons and background/
concurrency. Keep opaque signatures/fingerprints literal until their exact
protocol/compatibility obligation is established; never blanket-mask them.
Final composition, gate/rebase/clean hand-back remain pending.

Clean periodic integration refresh onto goe0e9d9d45 completed73 commits without
conflicts. Latest increment commit after rebase:f03fae0a6; prior DHCP-write
commit3da66fa19; transport commit5f1017b48. Python sources unchanged sincea9.
Read latest brief/WORKFLOW Leaner verification and coordinator messaging rules;
no progress messages to coordinator, no subagents/push. Targeted new-base check:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1; uptime; ~/cf-test/.venv/bin/python server/tools/routerdiff/build.py "$HOME/.cache/routerdiff-build-302rx1dg-e0e9d9d45" --parallel 4; export ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-e0e9d9d45/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -q test_boundary.BoundaryTests.test_real_native_config_write_pairs_only_content_bound_store_commit_ids; routerdiff_log="$HOME/.cache/routerdiff-reports/302rx1dg/rebase-e0e9d9d45-smoke.log"; if ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world runtime-reads --world client-drafts --world dhcp-ikuai-plain --world dhcp-ikuai-write-late-scope-network --world dns-target-ha-vip-documentation --world dns-target-single-null --output "$HOME/.cache/routerdiff-reports/302rx1dg/rebase-e0e9d9d45-smoke.json" > "$routerdiff_log" 2>&1; then tail -n 2 "$routerdiff_log"; else tail -n 12 "$routerdiff_log"; exit 1; fi'`
=> load11.10/12.51/14.10;normal binary build success;Ran1 native store-head
boundary test2.378s/OK;34 actual recipes/allGo-unimplemented,50 references,
1 existing audit,2300 pending in subset,complete=false/no bug. Still0/26 domain
routes in actual native public manifest; no broad duplicate suite was rerun.
No active command; work remains (1763 matrix entries pending if increments
compose), full composition/final verification/rebase still not done.

RouterOS HTTP error-class increment:
- Read actual RouterOSDriver._request and fixed-IP recovery/rollback producers.
  New ros_http_guards.py selects11 real probe controls and18 preview/confirm
  requests, plus an independent ordinary successful preview/new lease write.
- Nine new audits H0826–H0834 distinguish GET refusal from uncertain non-GET
  writes. Closed TCP and real read timeout,401/403,GET404 vs ordinary PUT404,
  error JSON/HTML/non-object content, permission detail and2xx malformed JSON
  have exact public outcomes. Not every status is multiplied by every method.
- Applied ambiguous replies must recover and remove the uniquely owned new
  lease; definite4xx refusals issue no DELETE. Both original leases and the
  unrelated iKuai binding survive, no local fixed-IP binding is committed.
  The ordinary success proves the fixture is writable; full snapshots remain
  strict. Does not claim all higher-level undo, cancellation or firmware paths.
- Initial29 requests passed. Final focused .64 command:
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-e0e9d9d45/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -q test_recipes test_audits; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world-prefix ros-http- --output "$HOME/.cache/routerdiff-reports/302rx1dg/ros-http-audits.json"'`
=> Ran53 tests12.325s/OK;31 cumulative actual recipes/allGo-unimplemented,
20 references/9 new audits,2292 pending in this subset,complete=false/no bug,
0/26 actual native domain registrations. No active command.
Current531+14+2/1754 remaining if increments compose. No full composition or
native parity claim. Continue pending helper/route/background boundaries using
one representative per distinct behaviour class, retaining literal unproven
signatures/fingerprints and full raw evidence. No private snapshot has been read
by these synthetic domain runs; final verification/rebase/clean hand-back open.

iKuai HTTP/authentication/TLS diagnostic increment:
- Read the real iKuai origin/http/call/login/post implementations. New focused
  ikuai_http_guards.py selects existing public probes plus normal/error fallback
  controls; no helpers or exceptions are patched. Distinct HTTP reason messages,
 201 refusal, same/other-origin redirect without following, empty/non-JSON and
  non-object body, login code/legacy Result/ErrMsg, missing/null call code,
  missing message, malformed successful results and1008 retry are all explicit.
- Real self-signed TLS True/False/True, actual10s read timeout and closed TCP
  retain ordinary probe persistence/profile revision and complete snapshots.
 1008 requires two real logins for both recovery and repeated expiry; fake
  response injection still does not claim real firmware session expiry.
-23 audits H0211–H0219/H0220–H0221/H0224–H0227/H0230–H0237, with ordinary
  healthy controls. H0209/H0210 malformed redirect catches, H0222/H0223 actual
  connect-SYN timeout and H0228/H0229 generic RequestException stay OPEN; do not
  invent library exceptions to obtain coverage. First40 real requests passed.
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-e0e9d9d45/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -q test_recipes test_audits; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world ikuai-http-reads --world ikuai-http-tls --output "$HOME/.cache/routerdiff-reports/302rx1dg/ikuai-http-audits.json"'`
=> Ran55 tests17.408s/OK;40 actual recipes/allGo-unimplemented,26 references,
23 new audits,2278 pending in subset,complete=false/no bug,0/26 actual native
registrations. No active command. Current554+14+2/1731 pending if increments
compose. Remaining matrix/ownership/runtime/YAML/background/concurrency and
full composition/final verification/rebase/clean hand-back remain open.

Coordinator changed again; task unchanged. Latest explicit instruction supersedes
older final-gate plans: ALL new rt commands pin sunbroke@192.168.0.32; no agent
full gate. Coordinator runs batch gate after hand-back. Final findings group by
suggested owner and include ONLY items requiring dispatch (no accepted changes).

Actual connect-SYN timeout increment H0222/H0223:
- New network_faults.py permits only a fixed SYN-drop rule in a distinct
  loopback-only namespace, checked against its parent network identity saved
  before unshare. No caller-controlled host/port/command/nft syntax. Worker
  cleanup removes its table; namespace destruction is an additional boundary.
- Baseline public probe succeeds. Public draft edit switches to unused owned
 10.44.0.3:18082 before the rule is installed, so legitimate native connection
  pooling cannot bypass the fault. Kernel counter proves a SYN was attempted,
  fake HTTP count stays0, actual10s ConnectTimeout gets the exact public error
  and saved failed probe. Rule removal is followed by successful public probe.
- Packet-presence booleans are retained as flow observations, not fabricated
  device replies. A native response with no SYN is a comparison difference;
  an oracle keeps its completed HTTP visible. Host namespace is actually refused
  before privileged commands. Retransmission counts are not an API contract.
- First .64 three-request smoke passed; a94-test targeted run20.662s/OK plus
 3 observations/2 audits10.51s was already running before the host switch and
  finished. No further .64 commands were started after the instruction.
- Fresh-endpoint variant verified on .32 with normal public observer rebuilt:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1; uptime; ~/cf-test/.venv/bin/python server/tools/routerdiff/build.py "$HOME/.cache/routerdiff-build-302rx1dg-e0e9d9d45" --parallel 4; export ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-e0e9d9d45/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -q test_network_faults test_audits.AuditTests.test_connect_timeout_audits_require_both_recovery_controls_and_real_packet_evidence test_boundary.BoundaryTests.test_real_healthy_native_boundary_compares_complete_observations; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world ikuai-connect-timeout --output "$HOME/.cache/routerdiff-reports/302rx1dg/connect-timeout-audits-32.json"'`
=> load3.66/7.62/9.40;normal public binary build success;Ran4 tests2.787s/OK;
3 actual recipes10.55s/allGo-unimplemented,5 references/2 new audits,
remaining_audits=2299 in subset,complete=false/no bug,0/26 native registrations.
No active command. Current556+14+2/1729 pending if increments compose. Next
investigate actual malformed-redirect transport outcomes (H0209/H0210/H0228/
H0229), then remaining matrix/ownership/runtime/YAML/background/concurrency.
No gate or snapshot read was performed; coordinator batch gate at hand-back.

COORDINATOR CLOSE-OUT (latest instruction, supersedes all historical next steps):
Stop expanding audits. Commit the pending redirect code if targeted .32 checks
pass; otherwise WIP. List all uncompleted matrix rows and reasons in findings.
Hand back branch/last commit and only dispatch-needed findings grouped by owner.
No complete gate/full composition; coordinator runs the merge batch gate.

Pending redirect code passed, so it is a normal tested commit, not WIP:
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-e0e9d9d45/configflow"; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -q test_probe_schemas test_runner test_recipes; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world ikuai-redirect-errors --output "$HOME/.cache/routerdiff-reports/302rx1dg/redirect-errors-closeout.json"'`
=> Ran61 tests4.238s/OK;6 actual requests0.88s/allGo-unimplemented,7 references,
no new formal audit declarations,complete=false. Actual Python bug registry adds
IKUAI_PROBE_REDIRECT_PARSE_CRASH: real traceback from requests/sessions.py,
ValueError Invalid IPv6 URL,HTML500,no DB/device change,no failed probe saved;
subsequent healthy recovery200. O12 safe/unsafe native outcome oracle added to
existing test_probe_schemas; rerun of that module=>Ran3 tests0.235s/OK.
This extends an existing probe failure family; no new audit direction opened.

Findings close-out:
- Seven deduplicated dispatch classes grouped by R05/R15/A20/C26/R17 at the
  top of parity/difftest-routers-findings.md, each with severity. All are Python
  reference defects; native domain handlers absent, so no proven Go regression.
- Exact complement of audits.declared + reachability.declared lists1729 IDs:
  route517,helper1112,device30,generated24,sequence26,crosscutting14,fixture3,
  fake-contract3. Every row has a matrix source link and explicit shared reason
  code. H0209/H0228/H0229 have new actual observations but no completed audit
  definition, so stay in this conservative uncovered list.
-556 executed-variant definitions+14 source proofs+2 explicit O6 supersessions
  are a distributed evidence inventory, not a freshly composed pass count.
  Last complete historical run remains3054 requests/301 audits; no report
  concatenation or native parity claim. All observed26 native routes absent.
- Unclosed token/fingerprint/ownership/YAML/runtime-clock/concurrency/firmware
  comparison contracts are separately disclosed as test limitations, not product
  findings. No product or Go business implementation was read/changed.
- Latest target host .32 only. No full gate/proddiff/private snapshot, no push.
Next ONLY commit, git rebase go, final bounded targeted checks, record tails and
clean status, then hand back. Do not resume the historical expansion plans.

## Final close-out verification

Coordinator-directed partial hand-back; no new audit directions. Tested source
commit236c9cacc, rebased integration basee0e9d9d45. Exact .32 command:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; export PYTHONDONTWRITEBYTECODE=1 ROUTERDIFF_BINARY="$HOME/.cache/routerdiff-build-302rx1dg-e0e9d9d45/configflow"; uptime; cd server/tools/routerdiff; ~/cf-test/.venv/bin/python -m unittest -q test_probe_schemas test_runner test_recipes test_audits test_inventory test_reachability test_network_faults test_boundary.BoundaryTests.test_real_healthy_native_boundary_compares_complete_observations; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$ROUTERDIFF_BINARY" --world ikuai-redirect-errors --world release-ik-suffix --output "$HOME/.cache/routerdiff-reports/302rx1dg/final-closeout.json"; ~/cf-test/.venv/bin/python - <<'"'"'PY'"'"'
import re
from pathlib import Path
import audits, inventory, reachability
known=set(audits.declared())|set(reachability.declared())
pending=set(inventory.load()[1])-known
text=Path("../../parity/difftest-routers-findings.md").read_text()
listed=re.findall(r"^\| ([A-Za-z0-9-]+) \| \[矩阵 L", text, re.M)
if len(listed)!=len(set(listed)) or set(listed)!=pending:
    raise SystemExit("uncovered ledger differs from exact matrix complement")
print("uncovered ledger: "+str(len(listed))+"/"+str(len(pending))+" exact, no duplicates")
PY'
```

Final output:

```text
load average:20.67,18.03,13.88
Ran 102 tests in 33.949s
OK
release-ik-suffix/ikuai_suffix_bug: 2 cumulative recipes, 0.54s
ikuai-redirect-errors/cases: 8 cumulative recipes, 0.80s
implemented_routes=0; recipe_counts={go-unimplemented:8}; referenced_entries=9
python_bugs=[IKUAI_NETMASK_SUFFIX_ROLLBACK,IKUAI_PROBE_REDIRECT_PARSE_CRASH]
complete=false; remaining_audits=2301 in this focused subset
uncovered ledger: 1729/1729 exact, no duplicates
```

The102 targeted tests include the new redirect safe/unsafe native outcome
oracle, coverage-ledger integrity, parent-namespace isolation/packet evidence,
runner contracts and one actual healthy native public-boundary comparison.
The8 real domain requests remain Go-unimplemented, not false passes. This is
not a full Go suite, full matrix rerun, gate or proddiff. Coordinator owns the
merge gate. Findings contain seven deduplicated reference-defect dispatch
classes and exact1729 uncovered IDs with reasons; source code was not fixed.

Hand-back scope is now closed as requested. No active command, no further
audit expansion. All changes committed; final git rebase go and clean status
are checked below/by the hand-back response. No push, no private snapshot data.
