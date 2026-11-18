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
