# Test speed B — final handoff state

Branch go-test-speed-b; rebased local go@37aad7f65 (containsbatch2/N73perf).
Implementation done, onlytests/goldens/sharedtools. No product/push/subagents/fullgate.
Main Chinese report ../test-speed-b.md has package commits, exactcommands, boundaries.

Current package commits:
- cb9ea7b8c agents initialcheckpointedprivatefilecopies +8pureparallel
- 527d7138a metrics3independentUnicode/key testsparallel
- e94e58c7f monitor cacheddecodedsections/privatecopies +3isolatedparallelseeds
- 3800dc344 apiagents pureA10conditionalgolden
- 7b792e255 installer pureA10deflateZIPgolden +6purecorpusparallel
- bc80253c7 ippresence percorpusrepo +perrowreset/3parallelcorpora
- 108aa2719 repo7purechecksparallel
Shared A recorder/auditor/fingerprint adopted withoutAconsumer code. Bregistry
6c7349487 has28groups/30constructorrefs,5scopes inclzero-entryippresence.
Bhelper-sourceAST6ad7a8cac checksordinary.gohelpers too; handoffpatchtoA.

Final rebased308 COMPLETEexit0 on.64:fmt-lsilent, both realPython check0diff,
sevenchangedpackage targetedrace PASS, sharedtestgolden/tools race PASS,
fullgovet silentPASS. Logs .ci-results/test-speed-b-rebased.
All existing corpusinputs/assertions retained; no coverage/timeouts relaxed.

Other actual controls already completed andsource restored:
- omitippresenceRemoveAll=>stale-fileassertFAIL; wrongseeddispatch=>isolationFAIL.
- unregisteredaliasexec=>ASTFAIL; non-testhelperGo=>ASTFAIL; bothpositiveafterrestore.
- source/input/venv/noaccept/failed-assertion no-write controls insharedtests.
- plaincachemissingfingerprintfails;wrappercached;backendappendfails/no(cached);
  restorecached. Ordinarypureoracle/guardedselectedstrace0Pythonexec.
- actualTestAuditRepository cached=>newplain.gohelper=>unregisteredFAIL/nocache;
  remove=>PASS. .ci-results/test-speed-b-audit-cache logs.
- canonicalinterpreterselectorisnowtestgolden.Python; metadata/Run samevenv.
  Initialrecord plus knownsourcehashupdates:Entries byte-identical. Finalrebase
  didnotchangeeither golden metadata orentries; finalcheckequal.

Authoritative timing instruction: formalbefore/after WAIT bothservers1minload<10.
Newbaseline mustinclude1d554eae/batch2; alloldbase/highloadtimingnumbers discarded.
Oldafter pressure81905 intentionallystopped exit143, nofullpassclaim. Implementation
canhandofffirst; laterpairedmeasurementseparate docscommit. No further loadpolling.

Liveboundaries remainreal, exactregistry notwholepackageexemption. Before oldgo215
and immutable Bsnapshot both liveproxy/API/productPyPASS, sameTHREE repofailures:
Ops+Interchange layout22 Py profiles/.copy.operation.lock; C04layout28 Py
profiles/.ghost.operation.lock. RootCrossCheckLock andcrashPASS. Userassigned6e
triage; exactsyntheticlogs handedoff. Do NOT call liveRepoallgreen.
Primaryignoredlogs .ci-results/test-speed-b-live-before andlive-after preserveboth.
An earlierafter same-rootrt-overlap lostlogs, explicitlyINVALID; rerun293 inown
immutableworktree COMPLETEexit0wrapper withper-jobrepoexit1 samefailures.

No running testcells remain. Allthree own temporaryworktrees removed after
copyinglogs and preservingtrialdiff/untrackedfiles inignored.ci-results.
Final latestgo37aad7f65 change isprogress/proddiffdocs only; source-identical to5264 validated
tree, no duplicaterun. Reportscommitted, treecleanafterfinalamend. Finalhandoff
tocoordinatorandA;6e alreadyreceivedexactlegacyfailureprovenance.

Coordinator changed to f8801d39; implementation68945125c handoff/context sent.
A formalpair isrunning on.64 (beforee0e9d9d45,afterb2cf3e6b4); BmustwaitforA's
explicit release. Bhasnorunningtests/measurement. Preparedmeasure-b.sh withthe
same sequentialuncachedmethodasA, Bpackagearray/original60mtimeout. No syncinside
pair; both immutable snapshots mustbeprepared first, logsoutsidebothroots.
Thispreparationdoesnotstartmeasurement or reserveA'swindow. Formaldata remains
pendingafterAwindow+bothload<10. Next:remote syntaxcheckafterwindow, rebase latestgo,
prepareimmutable before/after snapshotsandrunoneblockingpair, appendreportdocs.

Ahasreleaseditswindow: firstpairinvalidatedbyhigher-prioritygate-speed-accept;
Athenwaited908sandhandedbackunmeasured. Newcoordinatorf880mustscheduleBwindow.
Bsingle-readinesscheckafterrelease: .32load1=4.06/noaccept; .64load1=10.58/accept
active. Conditionsnotmet; noBpairlaunched. Windowrequestsenttof880 withbaseline
e0e9d9d45, Bmeasure-b.shsequentialmethod/logsoutsidebothsnapshots. Awaitexplicit
reservationandaccepttermination,bothload1<10. No backgroundmeasurement/test.
