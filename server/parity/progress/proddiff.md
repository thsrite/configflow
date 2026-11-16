# Production snapshot differential

Task: test-only, offline production-data acceptance framework. No business changes, no subagents,
no push. Source snapshot stays on the test server; all reports/commits contain schema only.
Design and command documentation: [doc/testing/go-proddiff.md](../../../doc/testing/go-proddiff.md).

Done:
- Initial runner commit af90c23d (rebased from 493df48c): real cmd/configflow build, isolated
  user/net/mount namespace, read-only snapshot mount/hash guard, separate disposable data copies,
  existing environment-account login, actual registered-route manifest, automatic mutation self-check.
- Hardened startup equivalence with metrics_flush.load_history; removed false history differences.
- Build-tagged ephemeral Go overlays suppress lifecycle loops; named Flask starters suppressed.
  PID namespace kills descendants. All real values stay in private server reports.
- Explicit read-only POST inventory; entity enumeration includes routers.items, saved deployments,
  previews, sessions and SQLite installer capabilities. Installer output uses saved Agent parameters.
- Exact normalization list; structural JSON/YAML/ZIP and text comparison; schema-only baseline and
  coverage inventory. Missing Go routes are counted, previously compared route removals fail.
- Five synthetic comparator/enumeration tests passed; full server go vet passed on test server.

Current remote result (integration Go source plus framework overlays):
156 GET templates: 50 compared, 106 unimplemented. 205 selected method/templates: 51 compared,
154 unimplemented. 2040 cases enumerated, 224 compared, 30 have no saved path parameter.
Python 10 named starters and Go 5 registered loops disabled. Both sides 20 connection-failure
responses, no transport timeout. Self-check PASS. Baseline: agents/value 3, config/value 2,
routers 0 compared differences (unimplemented). new=0, stale=0, coverage_regressions=0.
Source content unchanged; disposable copies deleted. Unrecoverable in-memory/device records are
explicit missing-parameter error cases; unimplemented outputs are not claimed as verified.

Open findings: three Agent log/logging failure message differences and two runtime log-line
value differences; exact schema paths in proddiff-baseline.txt. No business fixes attempted.

Commands:
- `rt -- server/scripts/proddiff.sh`
- `rt --back server/parity/proddiff-baseline.txt --back server/parity/proddiff-coverage.txt -- server/scripts/proddiff.sh --record-baseline`
- `rt -- bash -lc '~/cf-test/.venv/bin/python -m unittest discover -s server/tools/proddiff -p "test_*.py" -v && cd server && test -z "$(gofmt -l tools/proddiff)" && go vet ./...'`

Remaining: final integration rebase, ordinary baseline recheck on rebased sources, clean worktree.
