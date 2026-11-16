# Production snapshot differential

Scope: offline, snapshot-only acceptance framework. No business changes, subagents, production
access or push. Documentation: [tool README](../../tools/proddiff/README.md).

Original runner: 16c0292f / f31b9ff7 / 48567672. Acceptance follow-up worker commit e49d8306.
Go cmd/configflow uses test-tagged temporary overlays solely for loop suppression/route export.
Source is read-only mounted and hashed, all workers run in the same loopback-only namespace,
all data copies are disposable, auth uses existing env-account login. Real values stay only in
private test-server reports. Python 10 named background starters, Go 5 loops disabled.

Follow-up complete implementation:
- Direct Go command tools/proddiff/generator reads copies via repo and invokes public generator APIs;
  paired Python worker uses the same selectors/options. Separate generator copies avoid HTTP side effects.
- Five main config generators; six attachment/download metadata generators; three subscription formats;
  four Agent service variants. Mihomo preflight disabled equally; provider Fetch fails offline equally
  and the business implementation renders saved node caches. No generated caches/config are committed.
- Automatic HTTP ownership when the corresponding method/template is registered; subscription requests
  include all three formats. Zero-instance and missing implementations remain explicit.
- Four mandatory mutations: HTTP status, deeply nested JSON leaf, real Go YAML leaf, real Go text line.
- coverage.txt keeps generator/success markers; generator-coverage.json records all counts and success
  floors. Losing even one formerly successful output fails. Added takeover/floor/mutation regressions.
- Developer documentation moved from doc/testing to tools/proddiff/README.md.

Current coverage (all generated outputs compared successfully):
- Main Mihomo, Surge, Loon, MosDNS, OxiDNS: 2 each.
- MosDNS files/downloads, OxiDNS files/downloads, Mihomo providers/rules: 2 each.
- Subscription Mihomo/Surge/Loon: 4 each.
- Agent Mihomo 1, OxiDNS 2; Agent MosDNS/Surge 0 (no saved instances).
- Total 37 direct outputs, zero generator differences or errors.
- Missing Go generators: router scripts (1 input), full Agent deployment bundle/final-device render
  (3 inputs), aggregation provider (0 inputs). Do not count these as passed.
- HTTP: 156 GET (50 compared/106 missing), 205 selected templates (51 compared/154 missing),
  2060 cases enumerated, 224 compared; 30 no-snapshot-parameter error cases.
- Existing unique baseline: agents/value 3, config/value 2; routers 0 verified differences because
  script generation is missing. new=0, stale=0, coverage_regressions=0. Both sides 20 offline HTTP
  connection failures. Four self-checks PASS. No new business issues.

Commands:
- `rt -- server/scripts/proddiff.sh`
- `rt -- bash -lc '~/cf-test/.venv/bin/python -m unittest discover -s server/tools/proddiff -p "test_*.py" -v && server/scripts/proddiff.sh && cd server && test -z "$(gofmt -l tools/proddiff)" && go vet ./...'`
- Explicit coverage refresh:
  `rt --back server/parity/proddiff-baseline.txt --back server/parity/proddiff-coverage.txt --back server/parity/proddiff-generator-coverage.json -- server/scripts/proddiff.sh --record-baseline`

The test server reboot interrupted two commands; they were re-run after connectivity recovered.
Validation complete, both required commands exit 0. Final tail:
`Ran 12 tests ... OK`
`self-check: status=PASS json-leaf=PASS yaml-leaf=PASS text-line=PASS`
`baseline: new=0 stale=0` (coverage_regressions=0; source hash unchanged, all copies removed).
No business code changes; baseline remains exactly the original five value entries.
Rebase target: local go HEAD 6e59ce26e6966be9277833edc7f56ab2e3583a22. No push.
