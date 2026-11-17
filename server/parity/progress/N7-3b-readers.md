# N7-3b — rulefetch / refresh YAML readers

Status: in progress; branch go-n7-3b from go@6090e113. No worktree changes
were present at start. Scope: only rulefetch/refresh; shared yamlx changes
come from N7-3a. No agents spawned, no push.

Read AGENT-BRIEF, n7-n8 §2.2/§5 (5.4/5.7), N7-3 step, reviews N7-2 and A19,
PORTING/WORKFLOW. Shared changes cherry-picked: 0b98b42e (tag shape) and
dc3f339c (AllPairs); no private implementation of either.

Inventory: rulefetch/content.go Compose+Scan in NormalizeMihomoYAML,
domainEntries, remoteEntries, ReadRulePayload; LoadWith in flow-root
normalization and ReadRulePayload; old-node append/remove and recorded test
fingerprints depend on marks. pyerrors.go imports pyyaml solely for recursion
classification. refresh has no direct runtime Load calls: it delegates YAML
reading to subs/substore (N7-3a); SafeLoad in two tests must migrate.

Coordinator approved Edit/Marshal adaptation instead of scanner/span copies:
first Edit/splice, untouched lines byte-identical; only fallback D3 rewrite
when splice cannot apply. No-op/reads return original text. Append/remove
sequence fallback formatting change must be explicit in tests + ledger;
rule order, semantics and comments preserved. No yamlx.Clone on user input.

Next: migrate content readers and edit adapters to Node, replace old node
fingerprints by boundary assertions; migrate refresh test readers; add actual
input per-row old/new classification + depth40/shared/self alias entry tests;
add fuzz; .63 iterate, final .32 format/vet/race/full p4/all difftests/fuzz60/
proddiff. Check 3a merge before refresh delegated-input acceptance. No tests run
yet in this step. Existing legacy loaders/pyyaml stay for other migrations.

First buildable reader step: runtime rulefetch has no old Compose/Scan/Load,
Mapping/Lookup/Timestamp/Unrepresentable. Read payload operates on Node scalars;
edit callbacks use shared Edit (no private scanner/span). Removed old recursion
exception probe and payloadDocument null placeholder. Replay node fingerprints
retain kind/style only (old Python offsets are internal); full boundary corpus
classification still pending. Refresh's two test reads use Parse/typed Unmarshal.
Targeted .32 tests for flow/block, quoted keys, BOM, anchors, empty/refill and
refresh cache reads pass (rulefetch .027s / refresh .025s). .63 went offline
(SSH2222 timeout), so iterations continue on .32 per BRIEF.

Scope extended by coordinator: convert/proxyyaml_loose.go and its structured
proxy read path; remove private constructor/resolver/null-sentinel via native
Nodes. discovery/customPorts assigned to N7-3c. api/subscriptions/providers.go
reads program-generated JSON; replaced that YAML parser by FromJSON+Marshal.
Shared third cherry-pick 23196ddd (merge/scalar validation) applied as44f86041.

Native proxy reader step: removed the private resolver/constructor and recursive
null-sentinel bridge. Parse + AllPairs own policy/merge handling; a memoized Node
boundary preserves shared containers without repeated JSON serialization. Only
scalars cross ToJSON. Shared graphs bypass the old cache's copying path. Native
scalar/tag/alias tests updated; compilation of convert/repo/provider/health passes
on .32. Error-return API adaptation follows as a separate commit per coordinator.
Fourth shared pick a7acc788 applied as476adc97 (syntax/policy/alias identities).

convert API error-return caller adaptation (separate commit): both public proxy
readers return (*jsonx.Object,error). Syntax/no structured object retains fallback;
policy/alias errors propagate from ConvertNodeToMihomo, convert/repo rawDialer,
repo import rewrite, and provider delivery through existing error channels.
health.needsSubStore no longer counts a refused document as a remote conversion;
proxyOf uses its existing failed-probe path. No bulk import/skip loop at these
call sites, so no new silent skipping. Dedicated .32 tests prove policies/alias40
never call the fake Sub-Store and never populate the proxy cache; syntax still
falls back. Changed callers compile. Legacy scalar replay classification pending.
