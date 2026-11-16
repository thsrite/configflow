# C37 independent adversarial review

Scope: all three Python discovery files vs native Go implementation and 17 HTTP routes;
request/background cancellation, exclusive jobs, persistence, memory ownership and production GET gate.
No subagents. Iterate pure tests on .63; real TCP/Flask differential and final gates on .32.

Audit in progress: route decoder null/default differences, permissive persisted setting defaults,
21-job physical ownership (not just len), 4 matcher/1024 host/300s idle limits and repeated large traffic.
Added red regression tests before fixes. Native business code uses jsonx only in bridge.go and taskbridge.go.
No status change to verified until findings are closed and required final gates pass.

First fixes validated:
- Null website note deleted stored annotations; explicit null rule_type was silently defaulted.
- Empty JSON option values had different init/settings/region defaults; confidence 80.0,
  bad stored port and filtered persisted lists caused full-page decode errors.
- The 21-job slice retained a 22nd job behind its slice start; weak-pointer regression exposed it.
- Matcher cleanup could wait one full extra sweep after the 300s deadline; timer now honors earliest expiry.
- Native cache stress: 60 profiles × 1400 hosts, cap 4/1024, 299s kept/300s released, evicted objects collected.
- Eight rounds of 80 Agents × 150 hosts release their response objects; 60 cancellation/exclusivity rounds pass.
- Added six C37 GET templates to proddiff coverage inventory, no response baseline edits.
Validation: .63 scoped race tests discovery, domaindiscovery, rules all pass; .32 enabled HTTP/storage
Flask differential now 77 cases plus six missing-profile cases passes. Still reviewing task runner context
ownership and region result ordering; final full-suite run not yet done.

Second findings closed in working tree:
- Manual automatic-task runner retained cancelled request values through shared StartBackground.
  It now detaches before reads and gives the worker a repository-only context; weak-pointer test passes.
- Region usable/restricted arrays were alphabetized instead of preserving target preference order.
  Native aggregation now lives in region.go; bridge.go only adapts C16 legacy calls.
- Absent Agent marker maps became explicit null and failed C16 evaluation; omission is preserved.
- Overall failed jobs exposed partial errors unlike Flask; failure metadata is cleared, panic logs once.
- Added fresh-process restart test: no phantom running metadata, persisted probe survives restart.
- Expanded HTTP differential with nonalphabetical targets, omitted markers, live same-profile/cross-profile
  conflicts and isolated job polling. 78 sequential cases + six missing-profile cases + live conflict cases pass.
Production jsonx imports remain exactly bridge.go/taskbridge.go; no new Python runtime/type emulation.
- FinishedAt in returned job snapshots shared a mutable pointer; completion snapshots now deep-copy it.
- Legacy read-time bool/map defaults use one native JSON-token option decoder shared with HTTP;
  no recursive dynamic-value emulation or unbounded container decode.
- Auto loop test proves first-delay cancellation, competing loop refusal and file-lock release.

Python branch walk completed:
- domain_discovery.py: suffix extraction, counters/day filtering, selected statuses, ignored/failing/pending
  tags, stable host/port/route ties, first/last timestamps, coverage rounding, views and line formatting.
- service settings/rules: normalization, candidate/reference positioning, defaults/validation, cache-byte
  revisions, direct/proxy/policy targets, YAML/domain/classical dedupe, shadow warnings, history/undo/ignore.
- service jobs: Agent/version selection, 3-host and 18+2 batching, partial failures, exclusivity, polling,
  fresh scopes, panic/failure cleanup, 20/3-day auto selection, 30-day/10-entry rechecks and task receipts.
- service website/region: real collected-domain checks, candidate preference, 30/10 input caps, 12-request
  batches, ordered target results, preserved omitted fields, latest-store merge and policy-rule insertion.
- source automatic loop: flock/first delay/interval, per-profile scope cleanup, error continuation and stop.

17-route acceptance map (all also have administrator-auth failure coverage):
| method/path suffix | success and functional failures |
|---|---|
| GET settings | defaults/candidates; missing profile |
| PUT settings | partial update/empty selectors; object/type/confidence/port/occupied-port validation |
| POST rulesets/init | direct/both/empty-default targets; bad targets/proxy policy |
| GET domains | views/day/selection/probes/website; missing profile/invalid view fallback |
| GET agents | collection-only statuses; missing profile |
| POST apply | classical/domain/YAML dedupe/evidence/shadow/history; bad item/domain/null type |
| POST undo | original/policy target and absent domain; invalid domain/target |
| GET history | sorted names/evidence/future fields; missing profile |
| POST ignore | add/normalize/dedupe; invalid domain |
| DELETE ignore | remove; empty values |
| POST probe | successful/partial/failed async jobs; invalid domains/busy/cross-profile busy |
| GET probe/<job_id> | live/done; missing/cross-profile/missing-profile |
| GET region | result/policies/domain merge; missing profile |
| POST region/check | services/domain/omitted-marker paths; empty options/busy/no suitable Agent |
| POST region/route | domain/service policy library routing; bad kind/service/policy/domain |
| POST website/identify | known/partial batches; no Agent/all missing/invalid domains |
| PUT website/note | save/delete; bad object/domain/type/null annotation |

Latest .63 scoped race/vet run passes (discovery, domaindiscovery, rules). .32 boundary rerun required
on rebased integration source, followed by the single final full -race suite and proddiff.
