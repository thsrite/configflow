# C30 hand-back

Branch: `go-port-c30`. Implementation commits after final rebase:
- `bf499d37` — nodes routes and reviewed deletion hook.
- `f19ee585` — subscription management and provider exports.
- `b063069d` — Flask↔Go acceptance and fixes found by it.

Status: `ported`; 26/26 method/path pairs registered (21 C30 methods plus 5 retained C16 latency methods).
`nodes.Wire` registers `repo.Hooks.PlanNodeDeletion`. No pushes, no new dependencies, no production/device
access. Only synthetic data/fakes and the network-isolated read-only proddiff snapshot were used.

## Behaviour and safety

- `auth.go` is unchanged. Anonymous global provider requests return 401 even with a URL token.
  The explicit profile alias accepts the correct token (200), rejects wrong/missing/internal/retired tokens
  (401), and checks authentication before source existence/rendering. JWT and profile isolation also match.
  Token strings are asserted absent from provider logs.
- Snapshot capture precedes downloads, URI materialization is reused, the final cross-feed graph is
  validated before provider node caches/artifacts are committed. Failed later feeds preserve earlier caches.
  Storage comparison includes revisions, unknown fields, task/health/notification receipts and served YAML.
- Reviewed deletion preserves the exact interoperable confirmation hash, detaches ordinary references in
  one repository transaction, blocks chain/raw/rule dependencies, rejects stale confirmations and preserves
  unrelated writes. Unaffected proxy-order groups do not acquire spurious revisions.
- No deliberate C30 feature difference. Accepted formatting differences: YAML comments/formatting and
  native English regex/transport diagnostics; status and Chinese validation text remain compared.
- Deferred → A11, as directed: only individually annotated 400 diagnostic ownership/location envelopes
  (`deferA11Locations`); status/success/base validation prefix/field paths and stored effects are compared.
  Both PROGRESS-config C30 and PROGRESS-agents A11 require the owner to wire SetLocations, remove the
  helper and rerun C30. No compat implementation was added.
- Cross-module consumers retain their owners: C31 aggregation-provider hook, C33 proxy-group routes,
  C34 generate/download routes, A14 Agent push. A profile that requires aggregation materialization still
  fails closed until C31 wires its provider; no C30 bypass is introduced.

Python files: routes/subscriptions.py, routes/nodes.py and common/node_deletion.py. Per-test-file mapping
and each route's success/failure/storage assertions: [difftest-c30.md](difftest-c30.md) and PROGRESS-config C30.
The C30 differential has 111 synthetic fixture scenarios; it compares against live Flask blueprints rather
than replaying implementation functions. C16's existing differential supplies the retained latency methods.

## Added source/test files

| File | Lines |
|---|---:|
| `internal/api/nodes/adapter.go` | 177 |
| `internal/api/nodes/crud.go` | 254 |
| `internal/api/nodes/crud_test.go` | 54 |
| `internal/api/nodes/deletion.go` | 306 |
| `internal/api/nodes/messages.go` | 19 |
| `internal/api/subscriptions/adapter.go` | 480 |
| `internal/api/subscriptions/difftest/cases_test.go` | 401 |
| `internal/api/subscriptions/difftest/diff_test.go` | 499 |
| `internal/api/subscriptions/difftest/testdata/reference.py` | 91 |
| `internal/api/subscriptions/messages.go` | 14 |
| `internal/api/subscriptions/models.go` | 78 |
| `internal/api/subscriptions/providers.go` | 282 |
| `internal/api/subscriptions/routes.go` | 376 |
| `internal/api/subscriptions/security_test.go` | 65 |

## Final .32 validation

The exact fail-fast command (shell logs remain on the test server):

```sh
rt -- bash -lc 'set -e
cd server
gofmt -l internal/ tools/ > /tmp/c30-format-final.log
cat /tmp/c30-format-final.log
test ! -s /tmp/c30-format-final.log
go vet ./... > /tmp/c30-vet-final.log 2>&1
cat /tmp/c30-vet-final.log
go test -race -count=1 ./internal/api/nodes/... ./internal/api/subscriptions/... > /tmp/c30-race-final.log 2>&1
tail -5 /tmp/c30-race-final.log
go test -count=1 ./... > /tmp/c30-suite-final.log 2>&1
tail -8 /tmp/c30-suite-final.log
CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/subscriptions/difftest/ ./internal/health/difftest/ > /tmp/c30-diff-final.log 2>&1
tail -5 /tmp/c30-diff-final.log
cd ..
server/scripts/proddiff.sh > /tmp/c30-proddiff-final.log 2>&1
tail -3 /tmp/c30-proddiff-final.log'
```

Exit 0. gofmt and vet produced no output. Final lines:

```text
ok  configflow/internal/api/nodes                       4.136s
ok  configflow/internal/api/subscriptions               1.172s
ok  configflow/internal/api/subscriptions/difftest      1.022s  (env-gated differential skipped in this scoped race command)
...
ok  configflow/internal/weblistener                    0.006s
ok  configflow/internal/weburl                         0.006s
ok  configflow/internal/yamlx                          0.492s
?   configflow/tools/proddiff/generator                 [no test files]
ok  configflow/internal/api/subscriptions/difftest     59.159s
ok  configflow/internal/health/difftest                13.418s
self-check: status=PASS json-leaf=PASS yaml-leaf=PASS text-line=PASS
coverage_regressions=0 new=0 stale=0 self_check=PASS
baseline: new=0 stale=0
```

The production comparison covers 65 GET templates overall, including the 9 new C30 templates added to
proddiff-coverage.txt. Existing baseline differences remain unchanged. No baseline entries were added.
Only the diagnostic route's independent timestamp was added to the narrowly scoped normalization file.

Actual C30 differentials were then executed under race, also exit 0:

```sh
rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/subscriptions/difftest/ > /tmp/c30-diff-race-final.log 2>&1; result=$?; tail -20 /tmp/c30-diff-race-final.log; exit $result'
```

```text
ok  configflow/internal/api/subscriptions/difftest 73.305s
```

Final `git rebase go` brought only the coordinator's HANDOFF.md update, so the tested code is unchanged.
