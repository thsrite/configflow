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
