# CFG-01 / CFG-03 / CFG-09

Branch go-fix-cfg-c14, base local go1439e503d. No push/subagents; all tests via
rt on.64, only synthetic fixtures. See ../review-cfg-c14.md for exact evidence.

- CFG01 committed1d0a791a8: sticky internal API boundary before/after normalization
  and each redirect;20 real Python controls plus pre-I/O callback checks; H120
  public4/4compared,0open,0design; fullrulefetch+globalvet pass. Four explicitly
  named Python unsafe/ambiguous accepts now refused by Go; independent safety
  review requested by coordinator after delivery.
- CFG03 committedd8e6cacff: shared Source/editor lexical spans preserve physically
  multiline bytes+line0warning;16 real Python controls;20kUnicode positions52.6ms;
  fullrulefetch/yamlx+globalvet pass. C32 owner joint tree59238b255549 with CFG02
  9fe6a851: real rule-delivery-yaml-helpers22compared,0different,0open,design_errors[]
  in135.88s; H133 both prefixes bytes+warning strictly equal. Joint tree is not in
  either task branch; A12 listing unrelated.
- CFG09 separately committed (product-equivalent3475bdd82 before final evidence amend): preserve native
  LinkError and cancellation as500, PathError existing500, invalidURL400/valid200.
  Real receipt rename-directory+permission failures preserve chain and successful
  cache, no temp leaks. Targetedmosdns3.861s+vet pass. Real public configdiff2/2compared,0open/design in73.05s.
  Confirmed O3-6c API with owner before edit. Default runtime regressions explicitly
  skip base missingRecordContext. Separate joint4d3dfa7a2 with stable7c9f2a9bc
  executed every Runtime child withoutskip;mosdns23.276s/taskstatus2.237s
  racecount3+vet PASS. Runtime shutdown/parent-path/rename/disconnect verified.
  Product remains isolated, no lifecycle implementation copied. JointHTTP2/2compared/open0/design0 in72.24s;154selfchecks+mutationcontrolpass.

Rebased latestgo@d43cca6e4; currentCFG010d543f53e,CFG0347881419c.
Fullmosdns221.145s/rulefetch17.816s/yamlx9.977s + globalvet PASS on rebased tree.
Finalforegroundgate single-slot queue .64 07:47:04—08:02:04 reached900s;
watchdog stopped it with exit124, no gate stages ran. PerAGENT-BRIEF handback
with targeted results; coordinator runs merged fullgate. Report finalized in
CFG09 commit (gitlog subject: classify receipt IO failures as server errors).
CFG01 independent safety review remains coordinator's next step. Three ticket
implementations complete, latestgo@d43cca6e4, no production/push/subagents.
Jointlogs copied to local ignored .ci-results; clean temporary worktree removed.
