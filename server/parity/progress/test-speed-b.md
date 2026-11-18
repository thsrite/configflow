# Test speed B

Branch go-test-speed-b starts local go@a9d6ce86c; shared testgolden base
05ccbb133 fast-forwarded from A. No product code/push/subagents. Test only rt.
Latest process go@215ab6b0: no full agent gate; targeted checks only, no coordinator
progress messages, block tests rather than polling. Final rebase latest local go.

Assigned agents tree, api/agents, installer, ippresence, repo. Each changed package
separate commit. A14/A20/C34/N8-5 in flight: inspect latestgo before each package,
no product edits. A owns shared testgolden and tools/pyref audit/registration format.
B exceptions go tools/pyref/live-exceptions-b.json (not yet created).

Baseline launched .32 initial load1.26/6.22/16.39, GOMAXPROCS4 command:
  go test -race -p=4 -count=1 -timeout=60m -json ./internal/agents/... ./internal/api/agents ./internal/installer ./internal/ippresence ./internal/repo
All output .ci-results/test-speed-b-before/race.jsonl + load-before/after; rt session
7703 still running, do NOT resync this root until it completes. Baseline snapshot
is a9d6ce86c. Shared frozen YAML performance is owned by097eba4e/go-n73-gate-perf,
not B. Installer may need fresh baseline after it merges (currently untouched except
pure golden deflate conversion); other B slow corpus tests do not call Classify.

Independent local validation worktree (same agent, no subagent):
../test-speed-b-check detached shared05ccbb133 plus B uncommitted test files.
Test server .64 via rt. First trial of all original 3 monitor seeds parallel and
all ippresence corpora passed race: monitor82.584s, ippresence8.030s (NOT before/after
same-host performance evidence; only validation). New state leakage tests added.
Latest validation59348 failed to compile because loader replacement accidentally
removed `type failures`; primary has restored it. root agents fixture2.376s and
ippresence isolation1.407s passed; re-copy fixed primary to trial, rerun.

WIP changes:
- ippresence per-corpus repo init; replace library/reset cache files per row, same
  ClearMemo/each-row assertions;3corpora t.Parallel. Synthetic reset isolation test.
- monitor decoded gz corps cached once, deep-copy only explicit original sections;
  independent mutable inputs. original 3 large seeds share parent request dispatcher,
  each child own store/core/call log and t.Parallel; all original asserts remain.
  Added small parallel seed isolation regression. Need fault-injection negatives:
  omit file reset => stale-files test FAIL; dispatch all seeds to first => FAIL.
- root agents checkpointed initial repo files cached once, copy private files per
  newEnv then still OpenDefault. Pure event/message tests t.Parallel. Independent
  clone regression passes. No global hook-reset test parallelization.
- metrics3pure Unicode/key reference tests t.Parallel; no execPython baseline.
- api/agents A10 conditional3000 pure oracle -> shared testgolden, exact input payload.
- installer A10 deflate/ZIP -> shared testgolden exact input+manifest fingerprint.
  Need first record + read-only check zero diff after source edits/gofmt complete.
- repo7pure reference tests t.Parallel; no global clock/hook/env tests parallelized.

Coordinator explicitly approved keeping Python live per individual registered call:
installer product bootstrap.py/shell Python stubs; repo CONFIGFLOW_PY liveSQLite/
locks/crash; api/agents CONFIGFLOW_DIFF liveHTTP. root agents proxy_diff also has
CONFIGFLOW_DIFF explicit liveHTTP (default skip), retained. No package-wide exemption.
Record-only exec in pure Run callbacks registered too. AST tool and shared registration
format owner A; B writes same schema. Ordinary runtime strace no oracle Python, except
individually registered product scripts. Need separately time live suites.

Pending: sync fixed tests to trial; fmt/quickrace + record accepted(first recording
already authorized)+check; negative controls; copy formatted files/goldens primary;
per-package commits; finish/rebase latestgo; after same-host same-command race JSON
with load; standalone live timings; shared AST negative + hash drift/no-write negatives
via A shared tool; fullgo vet, final report evidence, clean temp worktree. No fullgate.

## Continuation checkpoint (2026-11-18)

Six package commits now done (will rebase/fixup final):
5af1035fe ippresence,800f562c1 agents,2b48a11af metrics,c92de1855 api-agents,
961456dbd installer,bc7232ffa repo,d937ae31b monitor (7 actual packages incl child).
Shared A patches adopted: initial05ccbb133,d96f216cc;8613ceca7->7c4bac730;
f8eff80b0->028393879 (only appended its new shared progress text, not private A
package claims);b38298d28->778c1d85d wrapper/fingerprint guard. No A consumer code.

Primary .32 baseline7703 COMPLETED ALL PASS:
load /proc start1.13/5.95/16.14 ->end41.04/19.86/13.11, cross-load large.
metrics10.668,upgrades44.174,apiagents274.604,agents421.339,monitor450.228,
repo133.224,ippresence788.105,installer1158.076 sec. Installer slowN73 gate668.70,
WriterOracleProvenance91.58 (external shared YAML perf owner not yet merged).
Baseline artifact .ci-results/test-speed-b-before/{race.jsonl,load-before,load-after}.
Primary after SAME COMMAND .32 session81905 RUNNING. Do not resync .32 root.

Trial ../test-speed-b-check validation completed:
- isolation targeted race root agents2.576,monitor31.487,ippresence1.431.
- actual source fault controls40128 PASS: omit RemoveAll =>previous row files
  leaked; dispatch allseeds to first =>parallel seed received another fixture;
  aliased exec unregistered =>AST rejection. All source restored.
- puregolden firstrecord+separatecheck output0diff;
  apiA10 3000dates+3000requests(919raise)0different; installer233inputs42MB+60ZIP.
- final check63041 .64 passedapi0.227/installer1.984; tools/testgolden race4.081,
  tools/pyref1.009;actualB CLI28groups/5scopesPASS;monitor allReference/Review47.107,
  repoReference/Review3.481 racePASS. Logs copied primary .ci-results/test-speed-b-check.
- installer six pure corpus tests parallel added; issue/enroll/globalclock serial.

NEW two consumer edits PRIMARY UNCOMMITTED: remove private reviewPython311 and
use testgolden.Python() in both Run callbacks so interpreter and dependency
fingerprint use same canonical path. This needs re-record metadata (outputs same).
A10 files and oldgoldens temporarily mismatched until queued validation completes.
Use fixups to original package commits after copying back finalrecord.

Shared wrappercache issue approved bycoordinator: plaincachedgotest without
PYREF_INPUT_FINGERPRINTfails;explicit-countpositive allowed (gate unchanged).
`go run ./tools/pyref -test -- args` computes nativebackend+venv digest;Getenv in
New includesit inGo cachekey. Fingerprint driftnegative proof fromA done;B queued
same actual cache=>backend edit=>fail=>restore=>cached proof.

Primary FINAL validation session99127 .64 is QUEUED on primary project rt lock
behind .32after81905 (rt project serialization crosses hosts). It will automatically
sync current primary and run: gofmt2A10;acceptedrecord;readonlycheck;sharedhelper+
actualrepositoryASTtests;plaincachefailure;wrapperfirst/secondcached;temporary
backend/utils/oxidns_archive.py modification rejected(no cached),restorecached;
straceordinaryselected pure/guardedtests ZERO Pythonexec;globalvet. No need
anothercommand/relaunch. --back final logs .ci-results/test-speed-b-final plus2Go
files/goldens toPRIMARY. Keep following tests to completion; queue limit15min if
it actually stays queued (start roughlyafter afterbenchmark had run~15min).

Live timing wrapper functions exec CELL280 still RUNNING. It blocksbaseline live
session45461 .64 (no polling loop atmodel), then auto runs same commandafter using
immutable committed snapshot ../test-speed-b-live-after@028393879 (command in
functions store live_b_command was updated with rt-C this snapshot). Cell280's
workdirarg stillprimary butrt-C overridescorrectly. This avoids concurrent primary
source updates while live snapshots sync. Resultsbackinbase/snapshot respectively.
Before base ../test-speed-b-live-base@go215ab6b04.
Jobs:proxyDIFF;api sixexplicitHTTP tests;repo ^Test(CrossCheck|StoredRoundTrip)
withCONFIGFLOW_PY/REPO (no snapshot);installer actualBundleBootstrap/Migration.
Baselineproxy21.03wall(exit0),api50.35(exit0),repo stillactiveC04Operations.
Existing baseline FAILS TestCrossCheckOperations20.22s +Interchange21.24s,
xcMaskedLayout line22. Sentcoorddecision/handoff (not progress), preservingall
assertions; no productfix. C04 has254ops vsbasic80; may bevery slow. Do NOT falsely
claimalllivegreen. Latest diagnosticactiveC04@09:13:38+08;no further logpolling.
Awaitcoordresponseif stoppinginvalidbaseline authorized;do not shorten timeout.
Currentlive args -race-count1-timeout60m-json. ExplicitPythonproduct/live calls
stayreal, registrycategorizationapproved.

Untrackedregistry files ready: live-exceptions-b.json28exactconstructor groups
(includesdifftestchildren),scopes-b.json5roots incl zero-entryippresence. Shared
TestAuditRepository(f8)automatically readsmanifest/registry; future exec must fail.
Needcommit registry+report separately. No localtests/build. No fullgateallowed.
Next: resultsbefore/after+live+queuedfinal;handle actual failures,finish perpackage
fixups if needed,vet/AST/hash/cache trace evidence,latestgo rebase,reportfull table
withloads/noise(no fakecausalspeedclaims),remove clean tempworktrees,finalhandback.
