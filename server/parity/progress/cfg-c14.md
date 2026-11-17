# CFG-01 / CFG-03 / CFG-09

Branch go-fix-cfg-c14, starts local go1439e503d in the existing private worktree.
Three tickets must have separate commits; no push/subagents; tests only rt,
synthetic data. Read AGENT-BRIEF, prior PORTING/WORKFLOW + C14 progress, triage.
User priority: CFG01 security first, CFG03 physical YAML preservation second,
CFG09 I/O classification last. H120 and rule-delivery-yaml configdiff groups.

CFG01 in progress: added20 real-Python loopback controls (testdata/gen_cfg01.py,
cfg01.json). Old code fails4 native expectations: raw dot traversal and3 encoded/
backslash redirect targets. Python itself accepts those3 target ambiguities plus encoded API-prefix dot traversal;
Go security policy deliberately refuses them (explicitly identified in test,
not a blanket expected-diff exemption). Proposed fix classifies raw API URLs
before dot-normalization, creates sticky origin/API-prefix boundary, checks
normalized URL and every redirect target with same native path/URL guard.
Normalization escaping API is rejected before first I/O; H120 fixture will need
precise allowance for earlier refusal (Python1 source request vs Go0), keeping
status400 and forbidden target0 strict. No rewrite of URL parser.
Targeted old-code controls failed4/18; fixed targeted controls +vet pass. Full
rulefetch suite passes15.246s (20 Python controls) on.64. Final 20-control +local-callback regressions pass0.028s, global go vet ./...
passes. Committing CFG01 before public H120 validation; amend evidence after run.

Coordination via Paseo skill explicitly authorized:
- O3-6c6e5c7760-36eb-4e1d-a5f0-1c378a081b14 replied: go-o3-6c@cf14fe3c2,
  API commit7a9b43e98 may rebase. RecordContext/ForgetContext final rename remains
  native LinkError/PathError, context errors unchanged. May fix CFG09 classification
  now ongo1439 without copying lifecycle code. Later jointly verify real Runtime
  shutdown no receipt, cancellation classification, disconnect still completes.
- C32 CFG020d31fb93-a16c-48f8-8e5e-9c2756fb0e6f confirmed our CFG03 owns shared
  rulefetch/content.go physical multiline detection, original bytes +line0 warning;
  they own read_adapter.local warning consumer/log once/name/count/lines/redaction.
  Send CFG03 hash/tests when done; H133 mixed finding only closes after both merge.

Next CFG03 use existing yamlx source-span/lexical helper rather than PyYAML copy.
CFG09 native IO errors must500, URL failures400, preserve errors.As, coordinate6c.
Final full gate foreground, queue cutoff15min, rebase latestgo, Chinese handback.

CFG01 H120 public blackbox passed4/4 compared/open0/design0 in22.49s on.64;
150 comparator selfchecks + real mutation check pass. First attempt stopped at old
observer selfcheck, no business requests; exact Py1/Go0 observer/negative controls
updated, no global trace exemption. Amend CFG01 evidence, then implementCFG03.
CFG03 added14 real-Python source controls; old Go fails10 physical-multiline cases,
4 ordinary singleline controls pass. New tests/fixtures remain untracked untilCFG03
commit. Actual group name from C32: rule-delivery-yaml-helpers.

CFG01 final/amended commit1d0a791a8 (H1204/4 +150selfchecks). CFG03 implementation
now uses shared yamlx.Source physical line/column index +editor quote lexer, no
PyYAML scanner copy. 16 real Python controls pass; original14 failed10 before.
One-line Unicode20k scalar positions check52.6ms in isolated15s. Fullrulefetch
12.689s/yamlx4.782s and globalvet pass. Native reader folding/matching unchanged;
delivery normalizer now original bytes+one warning. Final overflow guard in new
indexed offset path needs targeted fmt/test beforecommitCFG03. C32 commit9fe6a851
ready; afterCFG03 commit ask them joint22/22 on temp merge branch, noC32 sourcecopy.
Then implementCFG09 (confirmed6c interface unchanged/nativeIO errors).
