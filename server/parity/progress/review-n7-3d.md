# N7-3d independent review progress

Branch go-review-n7-3d, base a08c4cc27; no push/sub-agent/production. All tests via rt.
Review report: ../review-n7-3d.md. F1 commit117f785f0 restores provider/gateway typed reader causes,
with unchanged public messages and native diagnostic/position inspection. Seven
synthetic failures per boundary failed before and pass after; provider235 frozen
visible diagnostics pass exactly. F2 architecture audit gaps and F3 complete
21343-source frozen replay are implemented and targeted tests pass; committing as a separate gate-hardening change.
User confirmed matcher/content-list public API contracts stay unchanged: verify real
Python, add bounded alias-bomb regressions, keep helper causes, add design Clarified.
Matcher internal helper now returns its Parse cause. Added rules/pushdiff/contentsync
regressions, awaiting rt runs queued behind full changed-package iteration on .64.
Next: finish those runs, commit separately, complete call-site/category audit, run
final gate foreground (stop if queue >15m), finish report. Temporary review scripts
under server/tools/review_n73d_* must be removed before final commit.

F2/F3 committed02afae340. F4 direct deploy.SourceError zero coordinates failed
before (native NUL/no position; malformed flow/line only) and passes after;
provider235 exact reference still passes. Changed rules/deploy full packages and
vet pass. Full gateway/provider/routersplit/yamlx/yamlxtest iteration passes;
provider411.365s under shared-host load25-28 (no failure). User now explicitly
requires A12 RuleEntries to use lexical scanning like Python, not YAML parsing.
14 synthetic Python controls captured; old implementation fails9 controls including
valid block/flow, malformed, indentation, alias, BOM and 200-line head boundary.
Implement one shared line iterator and lexical listing, update safety tests and
Clarified accordingly. Keep provider/matcher public behavior unchanged.

F4 committed2d027e716. F5 A12 lexical listing now matches all14 real Python controls,
including valid/flow/malformed/indentation/alias/1MiB/empty/BOM/head200/Unicode.
F6 matcher helper preserves original cause with unchanged public no-match. New
10s-isolated alias regressions pass (matcher~10ms, provider/listing~15ms,
contentsync~39ms). rules/pushdiff/contentsync full suites +vet pass after lexical
change. Native migration references pass: convert3837/278, gateway4 changes,
provider235 exact messages/0positions, importer14, matcher981. Temporary tools
removed; permanent oracle + contract checker live in pushdiff/testdata.
Next: format final test cleanup remotely, commit F5/F6 + Clarified, run foreground
full gate (queue limit15min), complete final detailed review report.
