# R06 hand-back — 2026-11-17

Branch `go-port-r06`. Full gates ran at pre-final-rebase `afdeb804`, based on
`go` `045f2485` (O3-1/O3-2 and verified A19 included), as the required workflow
runs acceptance before the final rebase. Final implementation commit is
`3b6b05c6`, rebased onto `go` `f0fb3815`; R06, devhttp and lockx code trees are
identical to that tested implementation. Other integration changes include the
reviewed N8-2 store/proxy fixes and design notes. Post-rebase format/vet and all
171 R06 boundary cases are checked again below. Status is **ported**, never
verified by the author; fresh Opus HR review remains required. No push, production
access or lab access. No new dependencies.

Source coverage: all three R06 production files (684 Python lines) are ported.
`parity/r06-coverage.md` maps the 14/24/24 Python test functions, names the seven
R15 HTTP tests still pending, lists intended differences, and gives the L5 list.
`design/r06-route-editor.md` gives the interface/state/failure design and every
R04/R05/A19 issue-class disposition.

Core safety: complete stable snapshot before write; trustworthy fresh unique ID
or tagname plus every recorded field for ownership; ID reuse or any external
change blocks destructive compensation; iKuai names must be unique before editing
or deleting. Every write-path failure is a Chinese RouterError. Every edit detaches
before its first read, so Call/ListAll/readback and compensation survive request
cancellation. Each network request keeps its device timeout (after review-r06 F1). Password echoes
are hidden while keeping uncertain-write/failed-compensation status explicit.
L2 uses shared lockx.Keyed and passes its held context; no new goroutine or flock.

## Files added (lines)

| File | Lines |
|---|---:|
| `internal/routers/route_editor.go` | 379 |
| `internal/routers/routeros_routes.go` | 459 |
| `internal/routers/ikuai_routes.go` | 416 |
| `internal/routers/r06_boundary_test.go` | 591 |
| `internal/routers/r06_safety_test.go` | 291 |
| `internal/routers/routerfake/mutation.go` | 38 |
| `internal/routers/testdata/r06_reference.py` | 49 |
| `internal/routers/testdata/r06_mutations.py` | 27 |
| `design/r06-route-editor.md` | 102 |
| `parity/r06-coverage.md` | 87 |
| `parity/r06-acceptance.md` | 154 |

Modified: routers/messages.go (centralized diagnostics), routeros.go (private
credential metadata for R06 errors, no transport change), both device fakes
(external mutations, reply delay/override), PROGRESS-routers.md. No backend,
frontend or other package implementation changes.

## Authoritative checks on .32

Targeted acceptance and compiled implementation mutation gate:

```
rt -- bash -lc 'cd server && go test -count=1 -run TestR06 -v ./internal/routers/'
rt -- bash -lc 'cd server && ~/cf-test/.venv/bin/python internal/routers/testdata/r06_mutations.py'
```

Last output (after the O3 rebase and ownership hardening):

```
171 real Python boundary cases
PASS
ok  configflow/internal/routers 32.827s
result: DETECTED (compiled, real Python boundary mismatch)
request: DETECTED (compiled, real Python boundary mismatch)
ownership: DETECTED (compiled, real Python boundary mismatch)
```

171 comprises 169 equal-behaviour/explicit display-normalization cases and two
explicit safety differences: duplicate iKuai names cause Python to attempt a write;
Go returns a Chinese conflict, sends zero writes and preserves the full table.
Snapshots, outcomes, all requests and final tables are compared; editor-owned
compensation requests are compared instead of fabricating release undo records.

Format, vet and race (exact invocation):

```
rt -- bash -lc 'cd server && gofmt -l internal/ tools/ > /tmp/r06-gofmt.log && cat /tmp/r06-gofmt.log && test ! -s /tmp/r06-gofmt.log && go vet ./... && go test -race -count=1 ./internal/routers/ ./internal/devhttp/'
```

```
gofmt: no output
go vet ./...: exit 0
ok  configflow/internal/routers 241.234s
ok  configflow/internal/devhttp 3.542s
```

Full suite (exact invocation):

```
rt -- bash -lc 'cd server && go test -p=4 -count=1 ./... > /tmp/r06-full-final.log 2>&1; result=$?; tail -25 /tmp/r06-full-final.log; exit $result'
```

```
exit 0 (all packages)
ok  configflow/internal/routers 224.946s
ok  configflow/internal/importer 1.602s
ok  configflow/internal/webdav/difftest 0.010s
ok  configflow/internal/weblistener 0.008s
ok  configflow/internal/weburl 0.004s
ok  configflow/internal/yamlx 2.487s
?   configflow/tools/proddiff/generator [no test files]
```

All difftest packages and proddiff (exact invocation):

```
rt -- bash -lc 'set -e; cd server; mapfile -t r06_diff_packages < <(go list -tags=configflow_difftest ./... | rg "/difftest$"); CONFIGFLOW_DIFF=1 go test -tags=configflow_difftest -p=4 -count=1 "${r06_diff_packages[@]}" > /tmp/r06-diff-final.log 2>&1 || { tail -80 /tmp/r06-diff-final.log; exit 1; }; cat /tmp/r06-diff-final.log; cd ..; server/scripts/proddiff.sh > /tmp/r06-proddiff-final.log 2>&1 || { tail -30 /tmp/r06-proddiff-final.log; exit 1; }; tail -12 /tmp/r06-proddiff-final.log'
```

```
ok  configflow/internal/api/agents/difftest 122.106s
ok  configflow/internal/api/subscriptions/difftest 86.574s
ok  configflow/internal/discovery/difftest 2.367s
ok  configflow/internal/health/difftest 13.985s
ok  configflow/internal/httpx/difftest 1.210s
ok  configflow/internal/mosdns/difftest 1.774s
ok  configflow/internal/refresh/difftest 3.503s
ok  configflow/internal/substore/difftest 6.341s
ok  configflow/internal/webdav/difftest 20.853s
baseline: new=0 stale=0
```

Proddiff JSON summary: `self_check: PASS`, `coverage_regressions: 0`,
`unmapped_routes: 0`, `new: 0`, `stale: 0`; exit 0. Existing baseline differences
remain baselined, and unimplemented routers routes remain unimplemented; this is
not evidence for the R15 HTTP cases listed below.

## Final rebase check

```
rt -- bash -lc 'cd server && gofmt -l internal/ tools/ > /tmp/r06-rebased-gofmt.log && cat /tmp/r06-rebased-gofmt.log && test ! -s /tmp/r06-rebased-gofmt.log && go vet ./... && go test -count=1 -run TestR06 -v ./internal/routers/ > /tmp/r06-rebased-boundary.log 2>&1; result=$?; rg "real Python|^PASS|^ok|FAIL|mismatch" /tmp/r06-rebased-boundary.log; exit $result'
```

```
gofmt: no output
go vet ./...: exit 0
171 real Python boundary cases
PASS
ok  configflow/internal/routers 33.646s
```

## Remaining acceptance

- Fresh Opus HR review. Only the reviewer may set verified.
- R15 owns R019–R021: production Flask↔Go HTTP differential, auth/profile,
  fingerprint confirmation, no DB/manifest/Agent side effects, and **L1 before
  validation (busy 409 before otherwise-invalid 400/404)** are pending R15. No
  routers HTTP package or R14 job-lock owner exists on this branch. These are
  explicitly not counted as passes in this hand-back.
- L5-A only: firmware acceptance/encoding/defaults/readonly fields; real
  ID allocation/reuse, pagination under firmware churn and active-route convergence;
  dropped physical management link/recreation/restart evidence; actual packet
  forwarding and reboot persistence, including dynamic/ECMP/VRF/recursive routes.
