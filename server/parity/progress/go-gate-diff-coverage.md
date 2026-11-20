# Go gate environment discovery

Brief: audit tests gated by Python/DIFF environments, automatically include ordinary
packages in compare, fail on omitted execution or skips, run compare only on .32.
No product fixes, full gate, push or PR.

Done: AST discovery of active test files and transitive helper callers; shared
Python preflight/environment for diff and live; JSON required manifests/results;
compare continues remaining groups after failures. Existing WebDAV exec and
repo/store Python subtest evidence remain required. Three private-snapshot tests
retain explicit exclusions. Full inventory/results: ../gate-env-coverage.md.

Verification (.32): Go discovery race test/vet and shell syntax passed; Python
execution validator 9 tests OK; routing controls 15 tests OK. Final discovery matches
executed manifest; full gofmt check and go vet ./... PASS.

Compare: proddiff new=0 stale=0/self-check PASS. Live 40/42 top-level PASS, 2 FAIL,
0 SKIP; both Python-only subtests and WebDAV exec evidence PASS. New entries:
19/21 PASS, rulesync projection (3 falsy enabled cases; W2) and onboarding
null-policy (1 Chinese error-message mismatch, explicit null becomes PROXY) FAIL.
No product code changed. First diff JSON was removed by overlapping rt sync;
recovered main log, retained intact live JSON, reran difftest alone to safe logs:
24 packages/197 required tests run+pass, 0 skips. Logs on .32:
~/rt/go-gate-diff-coverage-20261120/{gate-first.log,live/,diff-retry/}.

Files added: tools/gatediscover/main.go (179 lines), main_test.go (54 lines),
this progress file. All implementation changes are gate tooling; product behavior
is unchanged. Complete inventory, pass/fail names and root causes are in the
coverage document. No unfinished implementation or required product fix in scope.

Next: coordinator integrates gate tooling; W2 owns the known rulesync fix; assign/decide the onboarding null-policy mismatch. No push or PR. Branch: go-gate-diff-coverage; commit: this changeset.

Commands:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; gofmt -l tools/gatediscover; go test -race ./tools/gatediscover; go vet ./tools/gatediscover; bash -n scripts/gate.sh scripts/gate-test-exec.sh; PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-live.py; PYTHONDONTWRITEBYTECODE=1 python scripts/test-gate-routing.py'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'GATE_ONLY=compare GATE_LOGS="$PWD/server/.gate-diff-coverage" server/scripts/gate.sh -p 4'
```

Final checks / recovery run:

```sh
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc 'GATE_ONLY=difftest GATE_LOGS="$HOME/rt/go-gate-diff-coverage-20261120/diff-retry" server/scripts/gate.sh -p 4'
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc 'set -e; cd server; files=$(gofmt -l internal tools cmd); test -z "$files" || { printf "%s\n" "$files"; exit 1; }; go vet ./...; echo "gofmt clean; go vet ./... PASS"'
```
