# N7-3d independent review — complete

Branch `go-review-n7-3d`; original base `a08c4cc27`, rebased cleanly onto local
`go@97580425b`. Detailed Chinese report: `server/parity/review-n7-3d.md`.
No push, sub-agents, production access or non-synthetic new fixtures.

## Findings fixed (high0 / medium3 / low3 / open0)

- F1 medium `8b52d4442`: provider/gateway typed YAML reader causes survive context
  formatting (14 synthetic cases failed before). Existing visible messages kept.
- F2 medium + F3 low `b5605451c`: AST helper-import/subpackage bypasses closed;
  permanent11 positive/negative controls, plus live replay of every21343 frozen
  source. Both bypasses passed the old real gate and fail the fixed gate.
- F4 low `26191ee0f`: direct deploy formatter omits unavailable native coordinates;
  native NUL/line-only controls failed before (line0/column0), pass after.
- F5 medium + F6 low `c0b12ef2d`: A12 RuleEntries restores Python lexical display
  rather than parsing YAML;14 real-Python controls (9 fail before), 1MiB line,
  valid depth40 graph42 literal entries, aliases/empty/malformed/indentation/BOM/
  Unicode/head200, complete raw entries/counts/display. Reuses rules/text.go line
  splitting; no second scanner. Matcher helper retains cause; public no-match
  unchanged. Native provider helper cause/size fallback unchanged. Design Clarified.

## Verification

All testing ran through rt on192.168.0.64. Changed-package full suites and vet:
gateway8.500s/provider411.365s/routersplit9.378s/yamlx5.381s/yamlxtest7.032s;
rules3.607s/deploy24.485s/contentsync0.642s/pushdiff7.947s before lexical change;
final lexical rules2.946s/pushdiff6.663s/contentsync0.399s. Provider long duration
was shared-host load25-28; passed without waiver or timeout changes.

Native gates: convert3837 sources/278 changes=182+34+31+31, provider235 complete
visible messages/rejected193/position0, gateway4/4, importer14, matcher981;
unknown0. Independent successful-output audit finds only approved native typing
(including real port fields) and metadata scalar changes. Diagnostic34/line
changes32/missing columns34 individually verified. Frozen replay21343 actually
executes every source (not only archive hashes). New10s isolated alias regressions
complete~10ms matcher/~15ms provider+listing/~39ms public contentsync.

Final fallback globalgofmt empty + go vet ./... passed; full convert15.826s,
importer2.632s, aggregations0.761s, proddiff-generator0.016s; permanent Python
contract checks passed. Logs `.ci-results/review-n73d-final/`.

After rebase: upstream158 configdiff tool/doc commits had no Go changes. Global
fmt/vet and all review regressions/AST controls/provider235/all21343 frozen sources
pass again: gateway0.006s/provider0.038s/deploy0.009s/rules0.018s/pushdiff0.055s/
contentsync0.021s/yamlx0.337s/yamlxtest2.999s. Final marker
`INTEGRATED PASS; gofmt empty; vet passed`, logs `.ci-results/review-n73d-integrated/`.
No new dependency, runtime Clone, legacy loader reference or temporary poison file.

## Full gate queue limit — coordinator handback required

The foreground .64 gate on original equivalent code `cf23b3fd7` (rebased fix
`c0b12ef2d`) waited900s without a slot and exited124 by the queue-limit wrapper.
No gate stage started. Logs `.ci-results/review-n73d-gate/gate.out`:
`QUEUE LIMIT: stopped after 900 seconds without a gate slot; no gate stages ran.`
Full race/build-tag/difftest/proddiff/timed fuzz are NOT claimed passed. No
second gate was started; coordinator runs full gate on merged tree per brief.
Requested fuzz: FuzzMatcherYAML, FuzzRuleEntries, FuzzNodeItems, FuzzCustomConfigYAML.
All foreground test sessions have completed; no pending background task.
